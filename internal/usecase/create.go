package usecase

import (
	"context"
	"fmt"
	"log"
	"sort"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/service/ecs"
	ecstypes "github.com/aws/aws-sdk-go-v2/service/ecs/types"
	"github.com/tetsuya28/ecs-pr-preview/internal/domain"
	"github.com/tetsuya28/ecs-pr-preview/internal/notification"
	"github.com/tetsuya28/ecs-pr-preview/internal/repository"
)

// CreateUsecase orchestrates the creation (or update) of a PR preview environment.
type CreateUsecase struct {
	cfg       domain.Config
	ecs       repository.ECSRepository
	elbv2     repository.ELBV2Repository
	r53       repository.Route53Repository
	notifier  notification.Notifier
	commenter notification.Commenter
}

func NewCreateUsecase(
	cfg domain.Config,
	ecs repository.ECSRepository,
	elbv2 repository.ELBV2Repository,
	r53 repository.Route53Repository,
	notifier notification.Notifier,
	commenter notification.Commenter,
) *CreateUsecase {
	return &CreateUsecase{
		cfg:       cfg,
		ecs:       ecs,
		elbv2:     elbv2,
		r53:       r53,
		notifier:  notifier,
		commenter: commenter,
	}
}

// Execute creates or updates a PR preview environment.
func (u *CreateUsecase) Execute(ctx context.Context, prNumber int, imageTag, ecrRegistry string) (err error) {
	preview := domain.NewPRPreview(prNumber, u.cfg)
	appImage := fmt.Sprintf("%s/%s:%s", ecrRegistry, u.cfg.AppECRRepository, imageTag)

	_ = u.notifier.Notify(ctx, fmt.Sprintf(":rocket: Starting environment setup for PR #%d", prNumber))
	defer func() {
		if err != nil {
			log.Printf("ERROR: create PR #%d failed: %v", prNumber, err)
			_ = u.notifier.Notify(ctx, fmt.Sprintf(":x: [PR #%d] Environment setup failed: %v", prNumber, err))
		}
	}()

	alb, err := u.elbv2.DescribeALB(ctx, u.cfg.ALBName)
	if err != nil {
		return err
	}
	listenerARN, err := u.elbv2.FindHTTPSListenerARN(ctx, alb.ARN)
	if err != nil {
		return err
	}
	netCfg, err := u.ecs.DescribeServiceNetworkConfig(ctx, u.cfg.ClusterName, u.cfg.BaseService)
	if err != nil {
		return err
	}

	// 1. Register Task Definition
	log.Printf("==> Registering TaskDef: %s", preview.Family)
	_ = u.notifier.Notify(ctx, fmt.Sprintf(":gear: [PR #%d] Registering Task Definition...", prNumber))
	taskDefARN, err := u.registerTaskDef(ctx, preview, appImage)
	if err != nil {
		return err
	}
	_ = u.notifier.Notify(ctx, fmt.Sprintf(":white_check_mark: [PR #%d] Task Definition registered: %s", prNumber, taskDefARN))

	// 2. Ensure Target Group
	log.Printf("==> Ensuring Target Group: %s", preview.TGName)
	_ = u.notifier.Notify(ctx, fmt.Sprintf(":gear: [PR #%d] Ensuring Target Group...", prNumber))
	tgARN, err := u.elbv2.EnsureTargetGroup(ctx, preview.TGName, alb.VpcID, u.cfg.HealthCheckPath)
	if err != nil {
		return err
	}
	_ = u.notifier.Notify(ctx, fmt.Sprintf(":white_check_mark: [PR #%d] Target Group ready", prNumber))

	// 3. Ensure Listener Rule
	log.Printf("==> Ensuring Listener Rule: %s", preview.Domain)
	_ = u.notifier.Notify(ctx, fmt.Sprintf(":gear: [PR #%d] Ensuring Listener Rule...", prNumber))
	if err := u.elbv2.EnsureListenerRule(ctx, listenerARN, preview.Domain, tgARN, prNumber); err != nil {
		return err
	}
	_ = u.notifier.Notify(ctx, fmt.Sprintf(":white_check_mark: [PR #%d] Listener Rule ready", prNumber))

	// 4. Create or update ECS Service
	log.Printf("==> Ensuring ECS Service: %s", preview.ServiceName)
	_ = u.notifier.Notify(ctx, fmt.Sprintf(":gear: [PR #%d] Ensuring ECS Service...", prNumber))
	if err := u.ensureService(ctx, preview, taskDefARN, tgARN, netCfg); err != nil {
		return err
	}
	_ = u.notifier.Notify(ctx, fmt.Sprintf(":white_check_mark: [PR #%d] ECS Service ready", prNumber))

	// 5. Upsert Route53 A record
	log.Printf("==> Upserting Route53: %s", preview.Domain)
	_ = u.notifier.Notify(ctx, fmt.Sprintf(":gear: [PR #%d] Configuring Route53 record...", prNumber))
	if err := u.r53.UpsertAliasRecord(ctx, u.cfg.HostedZoneID, preview.Domain, alb.DNSName, alb.CanonicalZoneID); err != nil {
		return fmt.Errorf("upsert Route53 record: %w", err)
	}

	_ = u.notifier.Notify(ctx, fmt.Sprintf(":tada: [PR #%d] Environment ready: %s", prNumber, preview.AppURL))

	if u.commenter != nil {
		_ = u.commenter.UpsertComment(ctx, "preview",
			fmt.Sprintf("## PR Preview\n\n%s\n\n`image_tag: %s`", preview.AppURL, imageTag),
		)
	}

	log.Printf("==> Done: %s", preview.AppURL)
	return nil
}

func (u *CreateUsecase) registerTaskDef(ctx context.Context, preview domain.PRPreview, appImage string) (string, error) {
	desc, err := u.ecs.DescribeTaskDefinition(ctx, u.cfg.BaseTaskDef)
	if err != nil {
		return "", err
	}
	input, err := buildRegisterTaskDefinitionInput(u.cfg, preview, desc, appImage)
	if err != nil {
		return "", err
	}

	return u.ecs.RegisterTaskDefinition(ctx, input)
}

func buildRegisterTaskDefinitionInput(cfg domain.Config, preview domain.PRPreview, desc *repository.TaskDefinitionDescription, appImage string) (*ecs.RegisterTaskDefinitionInput, error) {
	if desc == nil || desc.TaskDefinition == nil {
		return nil, fmt.Errorf("describe task definition %s: empty task definition", cfg.BaseTaskDef)
	}
	td := desc.TaskDefinition

	// Pre-resolve all override values once.
	resolved := make(map[string]string, len(cfg.EnvOverrides))
	for key := range cfg.EnvOverrides {
		if val, ok := cfg.EnvOverrides.Resolve(key, preview); ok {
			resolved[key] = val
		}
	}

	containers := make([]ecstypes.ContainerDefinition, len(td.ContainerDefinitions))
	copy(containers, td.ContainerDefinitions)
	for i, c := range containers {
		containers[i].Environment = append([]ecstypes.KeyValuePair(nil), c.Environment...)

		isApp := aws.ToString(c.Name) == cfg.AppContainerName
		if isApp {
			containers[i].Image = aws.String(appImage)
		}

		// Update env vars that already exist in this container.
		applied := make(map[string]bool, len(resolved))
		for j, env := range containers[i].Environment {
			key := aws.ToString(env.Name)
			if val, ok := resolved[key]; ok {
				containers[i].Environment[j].Value = aws.String(val)
				applied[key] = true
			}
		}

		// Append env vars that don't exist yet; only add to the app container.
		if isApp {
			for _, key := range sortedKeys(resolved) {
				val := resolved[key]
				if !applied[key] {
					containers[i].Environment = append(containers[i].Environment, ecstypes.KeyValuePair{
						Name:  aws.String(key),
						Value: aws.String(val),
					})
				}
			}
		}
	}

	return &ecs.RegisterTaskDefinitionInput{
		Family:                  aws.String(preview.Family),
		ContainerDefinitions:    containers,
		Cpu:                     td.Cpu,
		EphemeralStorage:        td.EphemeralStorage,
		Memory:                  td.Memory,
		NetworkMode:             td.NetworkMode,
		IpcMode:                 td.IpcMode,
		PidMode:                 td.PidMode,
		RequiresCompatibilities: td.RequiresCompatibilities,
		ExecutionRoleArn:        td.ExecutionRoleArn,
		InferenceAccelerators:   td.InferenceAccelerators,
		PlacementConstraints:    td.PlacementConstraints,
		ProxyConfiguration:      td.ProxyConfiguration,
		RuntimePlatform:         td.RuntimePlatform,
		Tags:                    desc.Tags,
		TaskRoleArn:             td.TaskRoleArn,
		Volumes:                 td.Volumes,
	}, nil
}

func sortedKeys(values map[string]string) []string {
	keys := make([]string, 0, len(values))
	for key := range values {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	return keys
}

func (u *CreateUsecase) ensureService(ctx context.Context, preview domain.PRPreview, taskDefARN, tgARN string, netCfg *ecstypes.AwsVpcConfiguration) error {
	status, _ := u.ecs.DescribeServiceStatus(ctx, u.cfg.ClusterName, preview.ServiceName)
	if status == "ACTIVE" {
		if err := u.ecs.UpdateServiceTaskDef(ctx, u.cfg.ClusterName, preview.ServiceName, taskDefARN); err != nil {
			return fmt.Errorf("update service: %w", err)
		}
		log.Printf("  Service updated")
		return nil
	}

	if err := u.ecs.CreateService(ctx, &ecs.CreateServiceInput{
		Cluster:        aws.String(u.cfg.ClusterName),
		ServiceName:    aws.String(preview.ServiceName),
		TaskDefinition: aws.String(taskDefARN),
		DesiredCount:   aws.Int32(1),
		LaunchType:     ecstypes.LaunchTypeFargate,
		NetworkConfiguration: &ecstypes.NetworkConfiguration{
			AwsvpcConfiguration: &ecstypes.AwsVpcConfiguration{
				Subnets:        netCfg.Subnets,
				SecurityGroups: netCfg.SecurityGroups,
				AssignPublicIp: netCfg.AssignPublicIp,
			},
		},
		LoadBalancers: []ecstypes.LoadBalancer{{
			TargetGroupArn: aws.String(tgARN),
			ContainerName:  aws.String(u.cfg.LBContainerName),
			ContainerPort:  aws.Int32(u.cfg.LBContainerPort),
		}},
	}); err != nil {
		return fmt.Errorf("create service: %w", err)
	}
	log.Printf("  Service created")
	return nil
}
