package repository

import (
	"context"
	"fmt"
	"strings"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/service/ecs"
	ecstypes "github.com/aws/aws-sdk-go-v2/service/ecs/types"
)

type TaskDefinitionDescription struct {
	TaskDefinition *ecstypes.TaskDefinition
	Tags           []ecstypes.Tag
}

type ECSRepository interface {
	DescribeTaskDefinition(ctx context.Context, family string) (*TaskDefinitionDescription, error)
	RegisterTaskDefinition(ctx context.Context, input *ecs.RegisterTaskDefinitionInput) (string, error)
	DescribeServiceNetworkConfig(ctx context.Context, cluster, service string) (*ecstypes.AwsVpcConfiguration, error)
	DescribeServiceStatus(ctx context.Context, cluster, service string) (string, error)
	CreateService(ctx context.Context, input *ecs.CreateServiceInput) error
	UpdateServiceTaskDef(ctx context.Context, cluster, service, taskDefARN string) error
	DrainAndDeleteService(ctx context.Context, cluster, service string) error
	ListTaskDefinitionsByFamily(ctx context.Context, family string) ([]string, error)
	DeregisterTaskDefinition(ctx context.Context, arn string) error
}

type ecsRepository struct {
	client *ecs.Client
}

func NewECSRepository(client *ecs.Client) ECSRepository {
	return &ecsRepository{client: client}
}

func (r *ecsRepository) DescribeTaskDefinition(ctx context.Context, family string) (*TaskDefinitionDescription, error) {
	out, err := r.client.DescribeTaskDefinition(ctx, &ecs.DescribeTaskDefinitionInput{
		TaskDefinition: aws.String(family),
		Include:        []ecstypes.TaskDefinitionField{ecstypes.TaskDefinitionFieldTags},
	})
	if err != nil {
		return nil, fmt.Errorf("describe task definition %s: %w", family, err)
	}
	return &TaskDefinitionDescription{
		TaskDefinition: out.TaskDefinition,
		Tags:           out.Tags,
	}, nil
}

func (r *ecsRepository) RegisterTaskDefinition(ctx context.Context, input *ecs.RegisterTaskDefinitionInput) (string, error) {
	out, err := r.client.RegisterTaskDefinition(ctx, input)
	if err != nil {
		return "", fmt.Errorf("register task definition: %w", err)
	}
	return aws.ToString(out.TaskDefinition.TaskDefinitionArn), nil
}

func (r *ecsRepository) DescribeServiceNetworkConfig(ctx context.Context, cluster, service string) (*ecstypes.AwsVpcConfiguration, error) {
	out, err := r.client.DescribeServices(ctx, &ecs.DescribeServicesInput{
		Cluster:  aws.String(cluster),
		Services: []string{service},
	})
	if err != nil || len(out.Services) == 0 {
		return nil, fmt.Errorf("describe service %s/%s: %w", cluster, service, err)
	}
	return out.Services[0].NetworkConfiguration.AwsvpcConfiguration, nil
}

func (r *ecsRepository) DescribeServiceStatus(ctx context.Context, cluster, service string) (string, error) {
	out, err := r.client.DescribeServices(ctx, &ecs.DescribeServicesInput{
		Cluster:  aws.String(cluster),
		Services: []string{service},
	})
	if err != nil || len(out.Services) == 0 {
		return "", nil
	}
	return aws.ToString(out.Services[0].Status), nil
}

func (r *ecsRepository) CreateService(ctx context.Context, input *ecs.CreateServiceInput) error {
	_, err := r.client.CreateService(ctx, input)
	return err
}

func (r *ecsRepository) UpdateServiceTaskDef(ctx context.Context, cluster, service, taskDefARN string) error {
	_, err := r.client.UpdateService(ctx, &ecs.UpdateServiceInput{
		Cluster:        aws.String(cluster),
		Service:        aws.String(service),
		TaskDefinition: aws.String(taskDefARN),
	})
	return err
}

func (r *ecsRepository) DrainAndDeleteService(ctx context.Context, cluster, service string) error {
	_, _ = r.client.UpdateService(ctx, &ecs.UpdateServiceInput{
		Cluster:      aws.String(cluster),
		Service:      aws.String(service),
		DesiredCount: aws.Int32(0),
	})
	_, err := r.client.DeleteService(ctx, &ecs.DeleteServiceInput{
		Cluster: aws.String(cluster),
		Service: aws.String(service),
		Force:   aws.Bool(true),
	})
	return err
}

func (r *ecsRepository) ListTaskDefinitionsByFamily(ctx context.Context, family string) ([]string, error) {
	var arns []string
	var nextToken *string
	for {
		out, err := r.client.ListTaskDefinitions(ctx, &ecs.ListTaskDefinitionsInput{
			FamilyPrefix: aws.String(family),
			NextToken:    nextToken,
		})
		if err != nil {
			return arns, err
		}
		for _, arn := range out.TaskDefinitionArns {
			if taskDefinitionFamilyFromARN(arn) == family {
				arns = append(arns, arn)
			}
		}
		if out.NextToken == nil {
			break
		}
		nextToken = out.NextToken
	}
	return arns, nil
}

func taskDefinitionFamilyFromARN(arn string) string {
	name := arn
	if slash := strings.LastIndex(name, "/"); slash >= 0 {
		name = name[slash+1:]
	}
	if colon := strings.LastIndex(name, ":"); colon >= 0 {
		name = name[:colon]
	}
	return name
}

func (r *ecsRepository) DeregisterTaskDefinition(ctx context.Context, arn string) error {
	_, err := r.client.DeregisterTaskDefinition(ctx, &ecs.DeregisterTaskDefinitionInput{
		TaskDefinition: aws.String(arn),
	})
	return err
}
