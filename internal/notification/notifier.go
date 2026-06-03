package notification

import "context"

// Notifier sends real-time progress notifications (e.g. Slack).
type Notifier interface {
	Notify(ctx context.Context, msg string) error
}

// Commenter posts or updates a comment on a PR when an operation completes.
type Commenter interface {
	UpsertComment(ctx context.Context, marker, body string) error
}

// MultiNotifier bundles multiple Notifiers. Failures are silently ignored so
// one broken notifier does not affect the others.
type MultiNotifier []Notifier

func (ms MultiNotifier) Notify(ctx context.Context, msg string) error {
	for _, n := range ms {
		_ = n.Notify(ctx, msg)
	}
	return nil
}
