package usecase

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/aws/aws-sdk-go-v2/service/ecs"
	ecstypes "github.com/aws/aws-sdk-go-v2/service/ecs/types"
	"github.com/tetsuya28/ecs-pr-preview/internal/domain"
	"github.com/tetsuya28/ecs-pr-preview/internal/notification"
	"github.com/tetsuya28/ecs-pr-preview/internal/repository"
)

func TestDeleteUsecaseContinuesWhenRoute53RecordIsMissing(t *testing.T) {
	notifier := &collectingNotifier{}
	err := NewDeleteUsecase(
		deleteTestConfig(),
		&deleteTestECS{serviceStatus: "ACTIVE"},
		&deleteTestELBV2{},
		&deleteTestRoute53{recordExists: false},
		notifier,
		nil,
	).Execute(context.Background(), 1319)
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}
	if !notifier.contains("delete Route53 record skipped") {
		t.Fatalf("expected Route53 warning notification, got %q", notifier.messages)
	}
	if !notifier.contains("Environment teardown complete") {
		t.Fatalf("expected completion notification, got %q", notifier.messages)
	}
	if notifier.contains("Environment teardown failed") {
		t.Fatalf("did not expect failure notification, got %q", notifier.messages)
	}
}

func TestDeleteUsecaseFailsOnOtherRoute53Error(t *testing.T) {
	notifier := &collectingNotifier{}
	err := NewDeleteUsecase(
		deleteTestConfig(),
		&deleteTestECS{serviceStatus: "ACTIVE"},
		&deleteTestELBV2{},
		&deleteTestRoute53{recordExists: true, deleteErr: errors.New("route53 throttled")},
		notifier,
		nil,
	).Execute(context.Background(), 1319)
	if err == nil {
		t.Fatal("expected error")
	}
	if !notifier.contains("delete Route53 record failed") {
		t.Fatalf("expected Route53 failure notification, got %q", notifier.messages)
	}
	if !notifier.contains("Environment teardown failed") {
		t.Fatalf("expected failure notification, got %q", notifier.messages)
	}
	if notifier.contains("Environment teardown complete") {
		t.Fatalf("did not expect completion notification, got %q", notifier.messages)
	}
}

func TestDeleteUsecaseSkipsMissingECSService(t *testing.T) {
	notifier := &collectingNotifier{}
	ecsRepo := &deleteTestECS{}
	err := NewDeleteUsecase(
		deleteTestConfig(),
		ecsRepo,
		&deleteTestELBV2{},
		&deleteTestRoute53{recordExists: true},
		notifier,
		nil,
	).Execute(context.Background(), 1319)
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}
	if ecsRepo.drainCalled {
		t.Fatal("DrainAndDeleteService should not be called")
	}
	if !notifier.contains("delete ECS Service skipped") {
		t.Fatalf("expected ECS warning notification, got %q", notifier.messages)
	}
	if !notifier.contains("Environment teardown complete") {
		t.Fatalf("expected completion notification, got %q", notifier.messages)
	}
}

func deleteTestConfig() domain.Config {
	return domain.Config{
		ClusterName:       "cluster",
		HostedZoneID:      "Z1234567890ABCDEF",
		PRResourcePrefix:  "luna-pr",
		BaseDomain:        "luna-matching.net",
		PRSubdomainPrefix: "pr",
		AppECRRepository:  "app",
		AppContainerName:  "app",
		LBContainerName:   "nginx",
		LBContainerPort:   80,
		HealthCheckPath:   "/healthz",
		BaseTaskDef:       "base-task",
		BaseService:       "service",
		EnvOverrides:      domain.EnvOverrides{},
		ALBName:           "alb",
	}
}

type collectingNotifier struct {
	messages []string
}

var _ notification.Notifier = (*collectingNotifier)(nil)

func (n *collectingNotifier) Notify(_ context.Context, msg string) error {
	n.messages = append(n.messages, msg)
	return nil
}

func (n *collectingNotifier) contains(needle string) bool {
	for _, msg := range n.messages {
		if strings.Contains(msg, needle) {
			return true
		}
	}
	return false
}

type deleteTestECS struct {
	serviceStatus string
	drainCalled   bool
}

var _ repository.ECSRepository = (*deleteTestECS)(nil)

func (e *deleteTestECS) DescribeTaskDefinition(context.Context, string) (*repository.TaskDefinitionDescription, error) {
	return nil, nil
}

func (e *deleteTestECS) RegisterTaskDefinition(context.Context, *ecs.RegisterTaskDefinitionInput) (string, error) {
	return "", nil
}

func (e *deleteTestECS) DescribeServiceNetworkConfig(context.Context, string, string) (*ecstypes.AwsVpcConfiguration, error) {
	return nil, nil
}

func (e *deleteTestECS) DescribeServiceStatus(context.Context, string, string) (string, error) {
	return e.serviceStatus, nil
}

func (e *deleteTestECS) CreateService(context.Context, *ecs.CreateServiceInput) error {
	return nil
}

func (e *deleteTestECS) UpdateServiceTaskDef(context.Context, string, string, string) error {
	return nil
}

func (e *deleteTestECS) DrainAndDeleteService(context.Context, string, string) error {
	e.drainCalled = true
	return nil
}

func (e *deleteTestECS) ListTaskDefinitionsByFamily(context.Context, string) ([]string, error) {
	return nil, nil
}

func (e *deleteTestECS) DeregisterTaskDefinition(context.Context, string) error {
	return nil
}

type deleteTestELBV2 struct{}

var _ repository.ELBV2Repository = (*deleteTestELBV2)(nil)

func (e *deleteTestELBV2) DescribeALB(context.Context, string) (*repository.ALBInfo, error) {
	return &repository.ALBInfo{
		ARN:             "alb-arn",
		DNSName:         "alb.example.com",
		CanonicalZoneID: "ZALB",
		VpcID:           "vpc-1",
	}, nil
}

func (e *deleteTestELBV2) FindHTTPSListenerARN(context.Context, string) (string, error) {
	return "listener-arn", nil
}

func (e *deleteTestELBV2) EnsureTargetGroup(context.Context, string, string, string) (string, error) {
	return "", nil
}

func (e *deleteTestELBV2) EnsureListenerRule(context.Context, string, string, string, int) error {
	return nil
}

func (e *deleteTestELBV2) FindListenerRuleARN(context.Context, string, string) (string, error) {
	return "rule-arn", nil
}

func (e *deleteTestELBV2) DeleteListenerRule(context.Context, string) error {
	return nil
}

func (e *deleteTestELBV2) DeleteTargetGroup(context.Context, string) error {
	return nil
}

type deleteTestRoute53 struct {
	recordExists bool
	existsErr    error
	deleteErr    error
}

var _ repository.Route53Repository = (*deleteTestRoute53)(nil)

func (r *deleteTestRoute53) HostedZoneDomain(context.Context, string) (string, error) {
	return "luna-matching.net", nil
}

func (r *deleteTestRoute53) AliasRecordExists(context.Context, string, string) (bool, error) {
	return r.recordExists, r.existsErr
}

func (r *deleteTestRoute53) UpsertAliasRecord(context.Context, string, string, string, string) error {
	return nil
}

func (r *deleteTestRoute53) DeleteAliasRecord(context.Context, string, string, string, string) error {
	return r.deleteErr
}
