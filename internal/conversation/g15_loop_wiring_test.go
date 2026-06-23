package conversation

import (
	"context"
	"net/http"
	"testing"
	"time"

	"ccgo/internal/api/anthropic"
	"ccgo/internal/contracts"
)

// ---------------------------------------------------------------------------
// LOOP-49: buildRequest auto-calls StripExcessMediaItems
// CC ref: services/api/claude.ts:956-1015 (stripExcessMediaItems).
// ---------------------------------------------------------------------------

// mediaCapturingClient wraps CreateMessage to capture the API messages sent.
type mediaCapturingClient struct {
	captured []contracts.APIMessage
	response *anthropic.Response
}

func (m *mediaCapturingClient) CreateMessage(_ context.Context, req anthropic.Request) (*anthropic.Response, error) {
	m.captured = req.Messages
	return m.response, nil
}

// buildAPIHistoryWithNImages constructs a slice of APIMessages that each contain
// one image block. The resulting slice is used as pre-built messages in a
// stub runner so we can verify StripExcessMediaItems is applied.
func buildAPIHistoryWithNImages(n int) []contracts.Message {
	msgs := make([]contracts.Message, 0, n)
	for i := 0; i < n; i++ {
		// Each message is a user turn carrying one image block.
		// Use ImageSource so NormalizeForAPI produces valid APIMessages.
		msgs = append(msgs, contracts.Message{
			Type: contracts.MessageUser,
			UUID: contracts.NewID(),
			Content: []contracts.ContentBlock{
				{
					Type: contracts.ContentImage,
					Source: &contracts.ImageSource{
						Type:      "base64",
						MediaType: "image/png",
						Data:      "iVBORw0KGgo=",
					},
				},
			},
		})
	}
	return msgs
}

func TestBuildRequestStripsExcessMedia_ExceedsLimit(t *testing.T) {
	// Build 102 images in history; limit is 100, so 2 should be stripped.
	const numImages = 102
	history := buildAPIHistoryWithNImages(numImages)

	client := &mediaCapturingClient{
		response: &anthropic.Response{
			ID:      "msg_ok",
			Type:    "message",
			Role:    "assistant",
			Model:   "claude-opus-4-5",
			Content: []contracts.ContentBlock{contracts.NewTextBlock("ok")},
		},
	}
	runner := Runner{
		Client:    client,
		Model:     "claude-opus-4-5",
		MaxTokens: 64,
	}

	// history is passed as prior turns; RunTurn adds the new user message.
	_, err := runner.RunTurn(context.Background(), history, contracts.Message{
		Type:    contracts.MessageUser,
		UUID:    contracts.NewID(),
		Content: []contracts.ContentBlock{{Type: contracts.ContentText, Text: "done"}},
	})
	if err != nil {
		t.Fatalf("RunTurn: %v", err)
	}
	if client.captured == nil {
		t.Fatal("client.captured is nil — no request was captured")
	}

	// Count images in the outgoing API request.
	count := 0
	for _, msg := range client.captured {
		for _, block := range msg.Content {
			if block.Type == contracts.ContentImage {
				count++
			}
		}
	}
	if count > anthropic.APIMaxMediaPerRequest {
		t.Fatalf("media count = %d, want ≤ %d (StripExcessMediaItems not applied)", count, anthropic.APIMaxMediaPerRequest)
	}
}

func TestBuildRequestStripsExcessMedia_WithinLimit(t *testing.T) {
	// 3 images — well within the 100 limit; all should be preserved.
	history := buildAPIHistoryWithNImages(3)

	client := &mediaCapturingClient{
		response: &anthropic.Response{
			ID:      "msg_ok",
			Type:    "message",
			Role:    "assistant",
			Model:   "claude-opus-4-5",
			Content: []contracts.ContentBlock{contracts.NewTextBlock("ok")},
		},
	}
	runner := Runner{
		Client:    client,
		Model:     "claude-opus-4-5",
		MaxTokens: 64,
	}
	_, err := runner.RunTurn(context.Background(), history, contracts.Message{
		Type:    contracts.MessageUser,
		UUID:    contracts.NewID(),
		Content: []contracts.ContentBlock{{Type: contracts.ContentText, Text: "done"}},
	})
	if err != nil {
		t.Fatalf("RunTurn: %v", err)
	}
	count := 0
	for _, msg := range client.captured {
		for _, block := range msg.Content {
			if block.Type == contracts.ContentImage {
				count++
			}
		}
	}
	if count != 3 {
		t.Fatalf("media count = %d, want 3 (images should be preserved)", count)
	}
}

// ---------------------------------------------------------------------------
// LOOP-50: non-streaming fallback path applies NonstreamingFallbackTimeout
// CC ref: services/api/claude.ts:807-811 (getNonstreamingFallbackTimeoutMs).
// ---------------------------------------------------------------------------

// timeoutCapturingClient records whether the context passed to CreateMessage
// has a deadline set.
type timeoutCapturingClient struct {
	streamErrs   []error
	calls        []fakeCall
	requests     []anthropic.Request
	lastDeadline time.Time
	hadDeadline  bool
}

func (c *timeoutCapturingClient) CreateMessage(ctx context.Context, req anthropic.Request) (*anthropic.Response, error) {
	dl, ok := ctx.Deadline()
	c.hadDeadline = ok
	if ok {
		c.lastDeadline = dl
	}
	c.requests = append(c.requests, req)
	if len(c.calls) == 0 {
		return nil, anthropic.APIError{StatusCode: http.StatusInternalServerError, Type: "test_error", Message: "no fake call"}
	}
	call := c.calls[0]
	c.calls = c.calls[1:]
	return call.response, call.err
}

func (c *timeoutCapturingClient) StreamMessages(ctx context.Context, req anthropic.Request, handle func(anthropic.StreamEvent) error) error {
	c.requests = append(c.requests, req)
	if len(c.streamErrs) == 0 {
		return anthropic.APIError{StatusCode: http.StatusInternalServerError, Type: "test_error", Message: "no stream err configured"}
	}
	err := c.streamErrs[0]
	c.streamErrs = c.streamErrs[1:]
	return err
}

func TestNonstreamingFallbackAppliesTimeout(t *testing.T) {
	// Streaming fails before any event → fallback to non-streaming.
	// Verify the fallback CreateMessage context has a deadline.
	tc := &timeoutCapturingClient{
		streamErrs: []error{
			anthropic.APIError{StatusCode: http.StatusServiceUnavailable, Type: "overloaded_error", Message: "stream unavailable"},
		},
		calls: []fakeCall{{
			response: &anthropic.Response{
				ID:      "msg_fallback",
				Type:    "message",
				Role:    "assistant",
				Model:   "claude-opus-4-5",
				Content: []contracts.ContentBlock{contracts.NewTextBlock("fallback ok")},
			},
		}},
	}

	runner := Runner{
		Client:       tc,
		Model:        "claude-opus-4-5",
		MaxTokens:    64,
		UseStreaming:  true,
	}

	result, err := runner.RunTurn(context.Background(), nil, contracts.Message{
		Type:    contracts.MessageUser,
		UUID:    contracts.NewID(),
		Content: []contracts.ContentBlock{{Type: contracts.ContentText, Text: "hello"}},
	})
	if err != nil {
		t.Fatalf("RunTurn: %v", err)
	}
	if got := result.Assistant.Content[0].Text; got != "fallback ok" {
		t.Fatalf("assistant text = %q, want 'fallback ok'", got)
	}
	if !tc.hadDeadline {
		t.Fatal("fallback CreateMessage context had no deadline — NonstreamingFallbackTimeout not applied")
	}
	// The deadline should be within ~5 minutes in the future (local mode = 300s).
	untilDeadline := time.Until(tc.lastDeadline)
	if untilDeadline <= 0 {
		t.Fatalf("deadline is already in the past: %v", tc.lastDeadline)
	}
	if untilDeadline > 305*time.Second {
		t.Fatalf("deadline too far in future (%v) — timeout not capped", untilDeadline)
	}
}
