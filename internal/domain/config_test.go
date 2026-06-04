package domain

import (
	"strings"
	"testing"
)

func TestConfigFromEnvLeavesBaseDomainEmpty(t *testing.T) {
	clearConfigEnv(t)
	t.Setenv("ECS_CLUSTER_NAME", "cluster")
	t.Setenv("ALB_NAME", "alb")
	t.Setenv("HOSTED_ZONE_ID", "Z1234567890ABCDEF")
	t.Setenv("BASE_TASK_DEF", "base-task")
	t.Setenv("PR_RESOURCE_PREFIX", "myapp-pr")
	t.Setenv("APP_ECR_REPOSITORY", "myapp-app")

	cfg, err := ConfigFromEnv()
	if err != nil {
		t.Fatalf("ConfigFromEnv: %v", err)
	}
	if cfg.BaseDomain != "" {
		t.Fatalf("BaseDomain = %q, want empty before Route53 resolution", cfg.BaseDomain)
	}
}

func TestConfigFromEnvRequiresHostedZoneID(t *testing.T) {
	clearConfigEnv(t)
	t.Setenv("ECS_CLUSTER_NAME", "cluster")
	t.Setenv("ALB_NAME", "alb")
	t.Setenv("BASE_TASK_DEF", "base-task")
	t.Setenv("PR_RESOURCE_PREFIX", "myapp-pr")
	t.Setenv("APP_ECR_REPOSITORY", "myapp-app")

	_, err := ConfigFromEnv()
	if err == nil {
		t.Fatal("expected error")
	}
	if !strings.Contains(err.Error(), "HOSTED_ZONE_ID") {
		t.Fatalf("error = %q, want HOSTED_ZONE_ID", err)
	}
}

func clearConfigEnv(t *testing.T) {
	t.Helper()
	for _, key := range []string{
		"ECS_CLUSTER_NAME",
		"ALB_NAME",
		"HOSTED_ZONE_ID",
		"BASE_TASK_DEF",
		"PR_RESOURCE_PREFIX",
		"APP_ECR_REPOSITORY",
		"BASE_SERVICE",
		"PR_SUBDOMAIN_PREFIX",
		"APP_CONTAINER_NAME",
		"LB_CONTAINER_NAME",
		"LB_CONTAINER_PORT",
		"HEALTH_CHECK_PATH",
		"ENV_OVERRIDES",
	} {
		t.Setenv(key, "")
	}
}
