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
	ClusterName  string // ECS_CLUSTER_NAME
	ALBName      string // ALB_NAME
	HostedZoneID string // HOSTED_ZONE_ID: Route53 hosted zone ID (e.g. "Z1234567890ABCDEF")
	BaseTaskDef  string // BASE_TASK_DEF: Task Definition family used as the copy source
	BaseService  string // BASE_SERVICE: ECS Service name used to inherit network configuration

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

	// EnvOverrides maps Task Definition env keys to value templates.
	// Parsed from ENV_OVERRIDES (e.g. "APP_URL={pr_url},COOKIE_DOMAIN={pr_domain},DEBUG=false").
	// Placeholders: {pr_url} → PR URL with scheme, {pr_domain} → PR hostname only.
	// Literal values (no placeholder) are passed through as-is.
	EnvOverrides EnvOverrides // ENV_OVERRIDES
}

// EnvOverrides maps Task Definition environment variable keys to value templates.
// Use {pr_url} and {pr_domain} as placeholders; any other text is used literally.
type EnvOverrides map[string]string

// Resolve returns the resolved value for the given key, substituting PR-specific
// placeholders. Returns ("", false) if the key is not in the overrides map.
func (o EnvOverrides) Resolve(key string, preview PRPreview) (string, bool) {
	tmpl, ok := o[key]
	if !ok {
		return "", false
	}
	val := strings.NewReplacer(
		"{pr_url}",    preview.AppURL,
		"{pr_domain}", preview.Domain,
	).Replace(tmpl)
	return val, true
}

// ConfigFromEnv loads Config from environment variables.
// Returns an error if any required variable is missing.
func ConfigFromEnv() (Config, error) {
	cfg := Config{
		ClusterName:       os.Getenv("ECS_CLUSTER_NAME"),
		ALBName:           os.Getenv("ALB_NAME"),
		HostedZoneID:      os.Getenv("HOSTED_ZONE_ID"),
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
	}

	overrides, err := parseEnvOverrides(os.Getenv("ENV_OVERRIDES"))
	if err != nil {
		return Config{}, fmt.Errorf("ENV_OVERRIDES: %w", err)
	}
	cfg.EnvOverrides = overrides

	var missing []string
	for _, pair := range []struct{ key, val string }{
		{"ECS_CLUSTER_NAME", cfg.ClusterName},
		{"ALB_NAME", cfg.ALBName},
		{"HOSTED_ZONE_ID", cfg.HostedZoneID},
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

// parseEnvOverrides parses "KEY=template,KEY=template,..." into an EnvOverrides map.
func parseEnvOverrides(s string) (EnvOverrides, error) {
	result := make(EnvOverrides)
	if s == "" {
		return result, nil
	}
	for _, pair := range strings.Split(s, ",") {
		kv := strings.SplitN(strings.TrimSpace(pair), "=", 2)
		if len(kv) != 2 || kv[0] == "" {
			return nil, fmt.Errorf("invalid entry %q (expected KEY=value)", pair)
		}
		result[kv[0]] = kv[1]
	}
	return result, nil
}
