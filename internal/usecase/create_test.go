package usecase

import (
	"reflect"
	"testing"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/service/ecs"
	ecstypes "github.com/aws/aws-sdk-go-v2/service/ecs/types"
	"github.com/tetsuya28/ecs-pr-preview/internal/domain"
	"github.com/tetsuya28/ecs-pr-preview/internal/repository"
)

func TestBuildRegisterTaskDefinitionInputCopiesBaseFields(t *testing.T) {
	cfg := domain.Config{
		BaseTaskDef:      "base-task",
		AppContainerName: "app",
		EnvOverrides: domain.EnvOverrides{
			"APP_URL": "{pr_url}",
			"NEW_ENV": "{pr_domain}",
		},
	}
	preview := domain.PRPreview{
		Domain: "pr-1.example.com",
		AppURL: "https://pr-1.example.com",
		Family: "myapp-pr-1",
	}
	base := &ecstypes.TaskDefinition{
		ContainerDefinitions: []ecstypes.ContainerDefinition{
			{
				Name:  aws.String("app"),
				Image: aws.String("old-app-image"),
				Environment: []ecstypes.KeyValuePair{
					{Name: aws.String("APP_URL"), Value: aws.String("https://old.example.com")},
					{Name: aws.String("KEEP"), Value: aws.String("1")},
				},
			},
			{
				Name:  aws.String("sidecar"),
				Image: aws.String("sidecar-image"),
			},
		},
		Cpu:                     aws.String("512"),
		EphemeralStorage:        &ecstypes.EphemeralStorage{SizeInGiB: 50},
		ExecutionRoleArn:        aws.String("arn:aws:iam::123456789012:role/execution"),
		InferenceAccelerators:   []ecstypes.InferenceAccelerator{{DeviceName: aws.String("device"), DeviceType: aws.String("eia1.medium")}},
		IpcMode:                 ecstypes.IpcModeTask,
		Memory:                  aws.String("1024"),
		NetworkMode:             ecstypes.NetworkModeAwsvpc,
		PidMode:                 ecstypes.PidModeTask,
		PlacementConstraints:    []ecstypes.TaskDefinitionPlacementConstraint{{Type: ecstypes.TaskDefinitionPlacementConstraintTypeMemberOf, Expression: aws.String("attribute:ecs.availability-zone == ap-northeast-1a")}},
		ProxyConfiguration:      &ecstypes.ProxyConfiguration{Type: ecstypes.ProxyConfigurationTypeAppmesh, ContainerName: aws.String("envoy"), Properties: []ecstypes.KeyValuePair{{Name: aws.String("IgnoredUID"), Value: aws.String("1337")}}},
		RequiresCompatibilities: []ecstypes.Compatibility{ecstypes.CompatibilityFargate},
		RuntimePlatform:         &ecstypes.RuntimePlatform{CpuArchitecture: ecstypes.CPUArchitectureArm64, OperatingSystemFamily: ecstypes.OSFamilyLinux},
		TaskRoleArn:             aws.String("arn:aws:iam::123456789012:role/task"),
		Volumes:                 []ecstypes.Volume{{Name: aws.String("cache")}},
	}
	tags := []ecstypes.Tag{{Key: aws.String("team"), Value: aws.String("platform")}}

	input, err := buildRegisterTaskDefinitionInput(cfg, preview, &repository.TaskDefinitionDescription{
		TaskDefinition: base,
		Tags:           tags,
	}, "new-app-image")
	if err != nil {
		t.Fatalf("buildRegisterTaskDefinitionInput: %v", err)
	}

	if aws.ToString(input.Family) != preview.Family {
		t.Fatalf("Family = %q, want %q", aws.ToString(input.Family), preview.Family)
	}
	app := input.ContainerDefinitions[0]
	if aws.ToString(app.Image) != "new-app-image" {
		t.Fatalf("app image = %q, want new-app-image", aws.ToString(app.Image))
	}
	appEnv := envMap(app.Environment)
	if appEnv["APP_URL"] != preview.AppURL {
		t.Fatalf("APP_URL = %q, want %q", appEnv["APP_URL"], preview.AppURL)
	}
	if appEnv["NEW_ENV"] != preview.Domain {
		t.Fatalf("NEW_ENV = %q, want %q", appEnv["NEW_ENV"], preview.Domain)
	}
	if appEnv["KEEP"] != "1" {
		t.Fatalf("KEEP = %q, want 1", appEnv["KEEP"])
	}
	sidecar := input.ContainerDefinitions[1]
	if aws.ToString(sidecar.Image) != "sidecar-image" {
		t.Fatalf("sidecar image = %q, want sidecar-image", aws.ToString(sidecar.Image))
	}
	if _, ok := envMap(sidecar.Environment)["NEW_ENV"]; ok {
		t.Fatal("NEW_ENV should not be appended to sidecar")
	}
	if aws.ToString(base.ContainerDefinitions[0].Image) != "old-app-image" {
		t.Fatal("base container definition was mutated")
	}
	if envMap(base.ContainerDefinitions[0].Environment)["APP_URL"] != "https://old.example.com" {
		t.Fatal("base container environment was mutated")
	}

	want := &ecs.RegisterTaskDefinitionInput{
		Family:                  input.Family,
		ContainerDefinitions:    input.ContainerDefinitions,
		Cpu:                     base.Cpu,
		EphemeralStorage:        base.EphemeralStorage,
		ExecutionRoleArn:        base.ExecutionRoleArn,
		InferenceAccelerators:   base.InferenceAccelerators,
		IpcMode:                 base.IpcMode,
		Memory:                  base.Memory,
		NetworkMode:             base.NetworkMode,
		PidMode:                 base.PidMode,
		PlacementConstraints:    base.PlacementConstraints,
		ProxyConfiguration:      base.ProxyConfiguration,
		RequiresCompatibilities: base.RequiresCompatibilities,
		RuntimePlatform:         base.RuntimePlatform,
		Tags:                    tags,
		TaskRoleArn:             base.TaskRoleArn,
		Volumes:                 base.Volumes,
	}
	if !reflect.DeepEqual(input, want) {
		t.Fatalf("input did not preserve base registerable fields\ninput=%#v\nwant=%#v", input, want)
	}
}

func TestBuildRegisterTaskDefinitionInputRejectsEmptyDescription(t *testing.T) {
	_, err := buildRegisterTaskDefinitionInput(domain.Config{BaseTaskDef: "base"}, domain.PRPreview{}, nil, "image")
	if err == nil {
		t.Fatal("expected error")
	}
}

func envMap(values []ecstypes.KeyValuePair) map[string]string {
	result := make(map[string]string, len(values))
	for _, kv := range values {
		result[aws.ToString(kv.Name)] = aws.ToString(kv.Value)
	}
	return result
}
