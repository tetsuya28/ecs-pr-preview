package domain

import (
	"fmt"
	"os"
	"strconv"
	"strings"
)

// Config holds all configuration for the ECS PR Preview tool.
// Each field is populated from environment variables.
type Config struct {
	// AWS resource names
	ClusterName string // ECS_CLUSTER_NAME
	ALBName     string // ALB_NAME
	HostedZone  string // HOSTED_ZONE
	BaseTaskDef string // BASE_TASK_DEF: Task Definition family used as the copy source
	BaseService string // BASE_SERVICE: ECS Service name used to inherit network configuration

	// PR resource naming
	// Resources are named as: PRResourcePrefix + "-" + prNumber
	PRResourcePrefix string // PR_RESOURCE_PREFIX (e.g. "myapp-pr")

	// Domain
	// PR domain is built as: PRSubdomainPrefix + "-" + prNumber + "." + BaseDomain
	BaseDomain        string // BASE_DOMAIN         (e.g. "example.com")
	PRSubdomainPrefix string // PR_SUBDOMAIN_PREFIX (e.g. "pr" → "pr-123.example.com")

	// Container settings
	AppContainerName string // APP_CONTAINER_NAME (e.g. "app")
	AppECRRepository string // APP_ECR_REPOSITORY (e.g. "myapp-app")
	LBContainerName  string // LB_CONTAINER_NAME  (e.g. "nginx")
	LBContainerPort  int32  // LB_CONTAINER_PORT  (e.g. "80")

	// ALB health check
	HealthCheckPath string // HEALTH_CHECK_PATH (e.g. "/healthz")

	// Environment variable override rules in the Task Definition
	AppURLEnvKey  string   // ENV_KEY_APP_URL  (e.g. "APP_URL")
	DomainEnvKeys []string // ENV_KEYS_DOMAIN  comma-separated (e.g. "SESSION_DOMAIN,SANCTUM_STATEFUL_DOMAINS")
}

// ConfigFromEnv loads Config from environment variables.
// Returns an error if any required variable is missing.
func ConfigFromEnv() (Config, error) {
	cfg := Config{
		ClusterName:       os.Getenv("ECS_CLUSTER_NAME"),
		ALBName:           os.Getenv("ALB_NAME"),
		HostedZone:        os.Getenv("HOSTED_ZONE"),
		BaseTaskDef:       os.Getenv("BASE_TASK_DEF"),
		PRResourcePrefix:  os.Getenv("PR_RESOURCE_PREFIX"),
		BaseDomain:        os.Getenv("BASE_DOMAIN"),
		AppECRRepository:  os.Getenv("APP_ECR_REPOSITORY"),
		BaseService:       getEnvDefault("BASE_SERVICE", os.Getenv("ECS_CLUSTER_NAME")),
		PRSubdomainPrefix: getEnvDefault("PR_SUBDOMAIN_PREFIX", "pr"),
		AppContainerName:  getEnvDefault("APP_CONTAINER_NAME", "app"),
		LBContainerName:   getEnvDefault("LB_CONTAINER_NAME", "nginx"),
		LBContainerPort:   int32(getEnvInt("LB_CONTAINER_PORT", 80)),
		HealthCheckPath:   getEnvDefault("HEALTH_CHECK_PATH", "/healthz"),
		AppURLEnvKey:      getEnvDefault("ENV_KEY_APP_URL", "APP_URL"),
		DomainEnvKeys:     getEnvSlice("ENV_KEYS_DOMAIN", "SESSION_DOMAIN,SANCTUM_STATEFUL_DOMAINS"),
	}

	var missing []string
	for _, pair := range []struct{ key, val string }{
		{"ECS_CLUSTER_NAME", cfg.ClusterName},
		{"ALB_NAME", cfg.ALBName},
		{"HOSTED_ZONE", cfg.HostedZone},
		{"BASE_TASK_DEF", cfg.BaseTaskDef},
		{"PR_RESOURCE_PREFIX", cfg.PRResourcePrefix},
		{"BASE_DOMAIN", cfg.BaseDomain},
		{"APP_ECR_REPOSITORY", cfg.AppECRRepository},
	} {
		if pair.val == "" {
			missing = append(missing, pair.key)
		}
	}
	if len(missing) > 0 {
		return Config{}, fmt.Errorf("missing required environment variables: %s", strings.Join(missing, ", "))
	}
	return cfg, nil
}

func getEnvDefault(key, defaultVal string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return defaultVal
}

func getEnvInt(key string, defaultVal int) int {
	v := os.Getenv(key)
	if v == "" {
		return defaultVal
	}
	n, err := strconv.Atoi(v)
	if err != nil {
		return defaultVal
	}
	return n
}

func getEnvSlice(key, defaultVal string) []string {
	v := getEnvDefault(key, defaultVal)
	if v == "" {
		return nil
	}
	return strings.Split(v, ",")
}
