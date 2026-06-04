package main

import (
	"fmt"
	"log"
	"os"

	"github.com/aws/aws-sdk-go-v2/config"
	awsecs "github.com/aws/aws-sdk-go-v2/service/ecs"
	"github.com/aws/aws-sdk-go-v2/service/elasticloadbalancingv2"
	"github.com/aws/aws-sdk-go-v2/service/route53"
	"github.com/spf13/cobra"
	"github.com/tetsuya28/ecs-pr-preview/internal/domain"
	"github.com/tetsuya28/ecs-pr-preview/internal/notification"
	"github.com/tetsuya28/ecs-pr-preview/internal/repository"
	"github.com/tetsuya28/ecs-pr-preview/internal/usecase"
)

func main() {
	root := &cobra.Command{
		Use:   "pr-preview",
		Short: "Manage PR preview environments on AWS ECS",
	}
	root.AddCommand(newCreateCmd(), newDeleteCmd())

	if err := root.Execute(); err != nil {
		log.Fatal(err)
	}
}

func initDeps(cmd *cobra.Command, prNumber int) (
	domain.Config,
	repository.ECSRepository,
	repository.ELBV2Repository,
	repository.Route53Repository,
	notification.Notifier,
	notification.Commenter,
	error,
) {
	cfg, err := domain.ConfigFromEnv()
	if err != nil {
		return domain.Config{}, nil, nil, nil, nil, nil, err
	}

	awsCfg, err := config.LoadDefaultConfig(cmd.Context())
	if err != nil {
		return domain.Config{}, nil, nil, nil, nil, nil, fmt.Errorf("load aws config: %w", err)
	}

	ecsRepo := repository.NewECSRepository(awsecs.NewFromConfig(awsCfg))
	elbv2Repo := repository.NewELBV2Repository(elasticloadbalancingv2.NewFromConfig(awsCfg))
	r53Repo := repository.NewRoute53Repository(route53.NewFromConfig(awsCfg))

	var notifiers notification.MultiNotifier
	slackBotToken := os.Getenv("SLACK_BOT_TOKEN")
	slackChannelID := os.Getenv("SLACK_CHANNEL_ID")
	if slackBotToken != "" && slackChannelID != "" {
		notifiers = append(notifiers, notification.NewSlackNotifier(slackBotToken, slackChannelID))
	} else if slackBotToken != "" || slackChannelID != "" {
		log.Printf("WARN: set both SLACK_BOT_TOKEN and SLACK_CHANNEL_ID to enable Slack threaded notifications")
	}

	commenter := buildGitHubCommenter(os.Getenv("GITHUB_TOKEN"), os.Getenv("GITHUB_REPOSITORY"), prNumber)

	return cfg, ecsRepo, elbv2Repo, r53Repo, notifiers, commenter, nil
}

func buildGitHubCommenter(token, repository string, prNumber int) notification.Commenter {
	if token == "" || repository == "" {
		return nil
	}
	if prNumber < 0 {
		log.Printf("WARN: --pr-number must be >= 0 for GitHub commenter")
		return nil
	}
	commenter, err := notification.NewGitHubCommenter(token, repository, prNumber)
	if err != nil {
		log.Printf("WARN: failed to init GitHub commenter: %v", err)
		return nil
	}
	return commenter
}

func newCreateCmd() *cobra.Command {
	var prNumber int
	var imageTag string
	var ecrRegistry string

	cmd := &cobra.Command{
		Use:   "create",
		Short: "Create or update a PR preview environment",
		RunE: func(cmd *cobra.Command, args []string) error {
			if err := validatePRNumber(prNumber); err != nil {
				return err
			}
			cfg, ecsRepo, elbv2Repo, r53Repo, notifier, commenter, err := initDeps(cmd, prNumber)
			if err != nil {
				return err
			}
			return usecase.NewCreateUsecase(cfg, ecsRepo, elbv2Repo, r53Repo, notifier, commenter).
				Execute(cmd.Context(), prNumber, imageTag, ecrRegistry)
		},
	}
	cmd.Flags().IntVar(&prNumber, "pr-number", 0, "Pull request number (required)")
	cmd.Flags().StringVar(&imageTag, "image-tag", "", "App container image tag (required)")
	cmd.Flags().StringVar(&ecrRegistry, "ecr-registry", "", "ECR registry URL (required)")
	_ = cmd.MarkFlagRequired("pr-number")
	_ = cmd.MarkFlagRequired("image-tag")
	_ = cmd.MarkFlagRequired("ecr-registry")
	return cmd
}

func newDeleteCmd() *cobra.Command {
	var prNumber int

	cmd := &cobra.Command{
		Use:   "delete",
		Short: "Delete a PR preview environment",
		RunE: func(cmd *cobra.Command, args []string) error {
			if err := validatePRNumber(prNumber); err != nil {
				return err
			}
			cfg, ecsRepo, elbv2Repo, r53Repo, notifier, commenter, err := initDeps(cmd, prNumber)
			if err != nil {
				return err
			}
			return usecase.NewDeleteUsecase(cfg, ecsRepo, elbv2Repo, r53Repo, notifier, commenter).
				Execute(cmd.Context(), prNumber)
		},
	}
	cmd.Flags().IntVar(&prNumber, "pr-number", 0, "Pull request number (required)")
	_ = cmd.MarkFlagRequired("pr-number")
	return cmd
}

func validatePRNumber(prNumber int) error {
	if prNumber < 0 {
		return fmt.Errorf("--pr-number must be >= 0")
	}
	return nil
}
