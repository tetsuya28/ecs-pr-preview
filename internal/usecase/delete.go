package usecase

import (
	"context"
	"fmt"
	"log"

	"github.com/tetsuya28/ecs-pr-preview/internal/domain"
	"github.com/tetsuya28/ecs-pr-preview/internal/notification"
	"github.com/tetsuya28/ecs-pr-preview/internal/repository"
)

// DeleteUsecase orchestrates the deletion of a PR preview environment.
type DeleteUsecase struct {
	cfg       domain.Config
	ecs       repository.ECSRepository
	elbv2     repository.ELBV2Repository
	r53       repository.Route53Repository
	notifier  notification.Notifier
	commenter notification.Commenter
}

func NewDeleteUsecase(
	cfg domain.Config,
	ecs repository.ECSRepository,
	elbv2 repository.ELBV2Repository,
	r53 repository.Route53Repository,
	notifier notification.Notifier,
	commenter notification.Commenter,
) *DeleteUsecase {
	return &DeleteUsecase{
		cfg:       cfg,
		ecs:       ecs,
		elbv2:     elbv2,
		r53:       r53,
		notifier:  notifier,
		commenter: commenter,
	}
}

// Execute deletes all AWS resources associated with the PR preview environment.
func (u *DeleteUsecase) Execute(ctx context.Context, prNumber int) (err error) {
	preview := domain.NewPRPreview(prNumber, u.cfg)

	_ = u.notifier.Notify(ctx, fmt.Sprintf(":broom: Starting environment teardown for PR #%d", prNumber))
	defer func() {
		if err != nil {
			log.Printf("ERROR: delete PR #%d failed: %v", prNumber, err)
			_ = u.notifier.Notify(ctx, fmt.Sprintf(":x: [PR #%d] Environment teardown failed: %v", prNumber, err))
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
	var firstErr error
	warn := func(step string, err error) {
		log.Printf("WARN: %s: %v", step, err)
		_ = u.notifier.Notify(ctx, fmt.Sprintf(":warning: [PR #%d] %s failed: %v", prNumber, step, err))
		if firstErr == nil {
			firstErr = err
		}
	}

	// 1. Drain and delete ECS Service
	log.Printf("==> Deleting ECS Service: %s", preview.ServiceName)
	_ = u.notifier.Notify(ctx, fmt.Sprintf(":gear: [PR #%d] Deleting ECS Service...", prNumber))
	if err := u.ecs.DrainAndDeleteService(ctx, u.cfg.ClusterName, preview.ServiceName); err != nil {
		warn("delete ECS Service", err)
	} else {
		_ = u.notifier.Notify(ctx, fmt.Sprintf(":white_check_mark: [PR #%d] ECS Service deleted", prNumber))
	}

	// 2. Delete Listener Rule
	log.Printf("==> Deleting Listener Rule: %s", preview.Domain)
	_ = u.notifier.Notify(ctx, fmt.Sprintf(":gear: [PR #%d] Deleting Listener Rule...", prNumber))
	ruleARN, err := u.elbv2.FindListenerRuleARN(ctx, listenerARN, preview.Domain)
	if err != nil {
		warn("find Listener Rule", err)
	} else if ruleARN != "" {
		if err := u.elbv2.DeleteListenerRule(ctx, ruleARN); err != nil {
			warn("delete Listener Rule", err)
		} else {
			_ = u.notifier.Notify(ctx, fmt.Sprintf(":white_check_mark: [PR #%d] Listener Rule deleted", prNumber))
		}
	}

	// 3. Delete Target Group (waits for target deregistration)
	log.Printf("==> Deleting Target Group: %s", preview.TGName)
	_ = u.notifier.Notify(ctx, fmt.Sprintf(":gear: [PR #%d] Deleting Target Group...", prNumber))
	if err := u.elbv2.DeleteTargetGroup(ctx, preview.TGName); err != nil {
		warn("delete Target Group", err)
	} else {
		_ = u.notifier.Notify(ctx, fmt.Sprintf(":white_check_mark: [PR #%d] Target Group deleted", prNumber))
	}

	// 4. Deregister all Task Definition revisions
	log.Printf("==> Deregistering TaskDefs: %s", preview.Family)
	_ = u.notifier.Notify(ctx, fmt.Sprintf(":gear: [PR #%d] Deregistering Task Definitions...", prNumber))
	if arns, err := u.ecs.ListTaskDefinitionsByFamily(ctx, preview.Family); err != nil {
		warn("list Task Definitions", err)
	} else {
		deregisterFailed := false
		for _, arn := range arns {
			if err := u.ecs.DeregisterTaskDefinition(ctx, arn); err != nil {
				deregisterFailed = true
				warn(fmt.Sprintf("deregister Task Definition %s", arn), err)
			}
		}
		if !deregisterFailed {
			_ = u.notifier.Notify(ctx, fmt.Sprintf(":white_check_mark: [PR #%d] Task Definitions deregistered", prNumber))
		}
	}

	// 5. Delete Route53 A record
	log.Printf("==> Deleting Route53: %s", preview.Domain)
	_ = u.notifier.Notify(ctx, fmt.Sprintf(":gear: [PR #%d] Deleting Route53 record...", prNumber))
	if err := u.r53.DeleteAliasRecord(ctx, u.cfg.HostedZoneID, preview.Domain, alb.DNSName, alb.CanonicalZoneID); err != nil {
		warn("delete Route53 record", err)
	} else {
		_ = u.notifier.Notify(ctx, fmt.Sprintf(":white_check_mark: [PR #%d] Route53 record deleted", prNumber))
	}

	if firstErr != nil {
		return firstErr
	}

	_ = u.notifier.Notify(ctx, fmt.Sprintf(":recycle: [PR #%d] Environment teardown complete", prNumber))

	if u.commenter != nil {
		_ = u.commenter.UpsertComment(ctx, "preview",
			fmt.Sprintf("## PR Preview\n\nEnvironment for PR #%d has been deleted.", prNumber),
		)
	}

	log.Printf("==> Done")
	return nil
}
