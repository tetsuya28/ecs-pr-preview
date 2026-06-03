package usecase

import (
	"context"
	"fmt"
	"log"

	"github.com/tetsuya28/ecs-pr-preview/internal/domain"
	"github.com/tetsuya28/ecs-pr-preview/internal/notification"
	"github.com/tetsuya28/ecs-pr-preview/internal/repository"
	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/service/ecs"
	ecstypes "github.com/aws/aws-sdk-go-v2/service/ecs/types"
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
func (u *CreateUsecase) Execute(ctx context.Context, prNumber int, imageTag, ecrRegistry string) error {
	preview := domain.NewPRPreview(prNumber, u.cfg)
	appImage := fmt.Sprintf("%s/%s:%s", ecrRegistry, u.cfg.AppECRRepository, imageTag)

	_ = u.notifier.Notify(ctx, fmt.Sprintf(":rocket: Starting environment setup for PR #%d", prNumber))

	alb, err := u.elbv2.DescribeALB(ctx, u.cfg.ALBName)
	if err != nil {
		return err
	}
	listenerARN, err := u.elbv2.FindHTTPSListenerARN(ctx, alb.ARN)
	if err != nil {
		return err
	}
	hostedZoneID, err := u.r53.FindHostedZoneID(ctx, u.cfg.HostedZone)
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
	if err := u.r53.UpsertAliasRecord(ctx, hostedZoneID, preview.Domain, alb.DNSName, alb.CanonicalZoneID); err != nil {
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
	td, err := u.ecs.DescribeTaskDefinition(ctx, u.cfg.BaseTaskDef)
	if err != nil {
		return "", err
	}

	containers := td.ContainerDefinitions
	for i, c := range containers {
		if aws.ToString(c.Name) == u.cfg.AppContainerName {
			containers[i].Image = aws.String(appImage)
		}
		for j, env := range containers[i].Environment {
			key := aws.ToString(env.Name)
			if key == u.cfg.AppURLEnvKey {
				containers[i].Environment[j].Value = aws.String(preview.AppURL)
			} else {
				for _, domainKey := range u.cfg.DomainEnvKeys {
					if key == domainKey {
						containers[i].Environment[j].Value = aws.String(preview.Domain)
						break
					}
				}
			}
		}
	}

	return u.ecs.RegisterTaskDefinition(ctx, &ecs.RegisterTaskDefinitionInput{
		Family:                  aws.String(preview.Family),
		ContainerDefinitions:    containers,
		Cpu:                     td.Cpu,
		Memory:                  td.Memory,
		NetworkMode:             td.NetworkMode,
		RequiresCompatibilities: td.RequiresCompatibilities,
		ExecutionRoleArn:        td.ExecutionRoleArn,
		TaskRoleArn:             td.TaskRoleArn,
		Volumes:                 td.Volumes,
	})
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
