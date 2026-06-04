package notification

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"testing"

	"github.com/slack-go/slack"
)

func TestSlackNotifierPostsSubsequentMessagesInThread(t *testing.T) {
	var requests []url.Values
	client := &http.Client{Transport: roundTripFunc(func(r *http.Request) (*http.Response, error) {
		if r.Method != http.MethodPost {
			t.Fatalf("method = %s, want %s", r.Method, http.MethodPost)
		}
		if got := r.URL.String(); got != "https://slack.test/api/chat.postMessage" {
			t.Fatalf("url = %q, want %q", got, "https://slack.test/api/chat.postMessage")
		}

		payload := readFormPayload(t, r)
		if got := payload.Get("token"); got != "xoxb-test" {
			t.Fatalf("token = %q, want %q", got, "xoxb-test")
		}
		requests = append(requests, payload)
		return jsonResponse(fmt.Sprintf(`{"ok":true,"channel":"C123456","ts":"1710000000.%06d"}`, len(requests))), nil
	})}

	slackClient := slack.New("xoxb-test", slack.OptionHTTPClient(client), slack.OptionAPIURL("https://slack.test/api/"))
	notifier := newSlackNotifier("C123456", slackClient)
	if err := notifier.Notify(context.Background(), "first"); err != nil {
		t.Fatalf("first notify: %v", err)
	}
	if err := notifier.Notify(context.Background(), "second"); err != nil {
		t.Fatalf("second notify: %v", err)
	}

	if len(requests) != 2 {
		t.Fatalf("request count = %d, want 2", len(requests))
	}
	if requests[0].Get("channel") != "C123456" || requests[0].Get("text") != "first" || requests[0].Get("thread_ts") != "" {
		t.Fatalf("first payload = %+v", requests[0])
	}
	if requests[1].Get("channel") != "C123456" || requests[1].Get("text") != "second" || requests[1].Get("thread_ts") != "1710000000.000001" {
		t.Fatalf("second payload = %+v", requests[1])
	}
}

func TestSlackNotifierReturnsSlackAPIError(t *testing.T) {
	client := &http.Client{Transport: roundTripFunc(func(r *http.Request) (*http.Response, error) {
		return jsonResponse(`{"ok":false,"error":"not_in_channel"}`), nil
	})}

	slackClient := slack.New("xoxb-test", slack.OptionHTTPClient(client), slack.OptionAPIURL("https://slack.test/api/"))
	notifier := newSlackNotifier("C123456", slackClient)
	err := notifier.Notify(context.Background(), "first")
	if err == nil {
		t.Fatal("expected error")
	}
	if !strings.Contains(err.Error(), "not_in_channel") {
		t.Fatalf("error = %q, want not_in_channel", err)
	}
}

func TestSlackNotifierRequiresFirstMessageTimestamp(t *testing.T) {
	client := &http.Client{Transport: roundTripFunc(func(r *http.Request) (*http.Response, error) {
		return jsonResponse(`{"ok":true}`), nil
	})}

	slackClient := slack.New("xoxb-test", slack.OptionHTTPClient(client), slack.OptionAPIURL("https://slack.test/api/"))
	notifier := newSlackNotifier("C123456", slackClient)
	err := notifier.Notify(context.Background(), "first")
	if err == nil {
		t.Fatal("expected error")
	}
	if !strings.Contains(err.Error(), "missing ts") {
		t.Fatalf("error = %q, want missing ts", err)
	}
}

func readFormPayload(t *testing.T, r *http.Request) url.Values {
	t.Helper()
	body, err := io.ReadAll(r.Body)
	if err != nil {
		t.Fatalf("read body: %v", err)
	}
	payload, err := url.ParseQuery(string(body))
	if err != nil {
		t.Fatalf("parse body: %v", err)
	}
	return payload
}

type roundTripFunc func(*http.Request) (*http.Response, error)

func (f roundTripFunc) RoundTrip(r *http.Request) (*http.Response, error) {
	return f(r)
}

func jsonResponse(body string) *http.Response {
	return &http.Response{
		StatusCode: http.StatusOK,
		Header:     make(http.Header),
		Body:       io.NopCloser(strings.NewReader(body)),
	}
}
