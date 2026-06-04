package notification

import (
	"context"
	"fmt"
	"sync"

	"github.com/slack-go/slack"
)

type slackPoster interface {
	PostMessageContext(ctx context.Context, channelID string, options ...slack.MsgOption) (string, string, error)
}

// SlackNotifier sends the first notification as a parent message and posts all
// subsequent notifications in that message's Slack thread.
type SlackNotifier struct {
	channelID string
	threadTS  string
	client    slackPoster
	mu        sync.Mutex
}

func NewSlackNotifier(token, channelID string) *SlackNotifier {
	return newSlackNotifier(channelID, slack.New(token))
}

func newSlackNotifier(channelID string, client slackPoster) *SlackNotifier {
	return &SlackNotifier{
		channelID: channelID,
		client:    client,
	}
}

func (s *SlackNotifier) Notify(ctx context.Context, msg string) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	options := []slack.MsgOption{slack.MsgOptionText(msg, false)}
	if s.threadTS != "" {
		options = append(options, slack.MsgOptionTS(s.threadTS))
	}

	_, ts, err := s.client.PostMessageContext(ctx, s.channelID, options...)
	if err != nil {
		return err
	}
	if s.threadTS == "" {
		if ts == "" {
			return fmt.Errorf("slack chat.postMessage response missing ts")
		}
		s.threadTS = ts
	}
	return nil
}
