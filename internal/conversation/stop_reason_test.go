package conversation

import (
	"context"
	"strings"
	"testing"

	"ccgo/internal/api/anthropic"
	"ccgo/internal/contracts"
	msgs "ccgo/internal/messages"
)

func TestClassifyStopReason(t *testing.T) {
	cases := map[string]stopAction{
		"":                              stopActionContinue,
		"end_turn":                      stopActionContinue,
		"tool_use":                      stopActionContinue,
		"stop_sequence":                 stopActionContinue,
		"max_tokens":                    stopActionRecoverMaxTokens,
		"pause_turn":                    stopActionResumePauseTurn,
		"refusal":                       stopActionRefusal,
		"model_context_window_exceeded": stopActionContextWindowExceeded,
	}
	for reason, want := range cases {
		if got := classifyStopReason(reason); got != want {
			t.Fatalf("classifyStopReason(%q) = %v want %v", reason, got, want)
		}
	}
}

// containsText reports whether any message in msgs contains substr in its text content.
func containsText(messages []contracts.Message, substr string) bool {
	for _, m := range messages {
		for _, block := range m.Content {
			if block.Type == contracts.ContentText && strings.Contains(block.Text, substr) {
				return true
			}
		}
	}
	return false
}

// newMinimalRunner creates a minimal Runner for stop_reason integration tests.
func newMinimalRunner(t *testing.T, client MessageClient) Runner {
	t.Helper()
	return Runner{
		Client:           client,
		Model:            "sonnet",
		MaxTokens:        128,
		SessionID:        "sess_stop_reason_test",
		SessionPath:      "",
		WorkingDirectory: t.TempDir(),
	}
}

func TestRunTurnRefusalSurfacesMessageAndStops(t *testing.T) {
	client := &fakeClient{calls: []fakeCall{
		{response: &anthropic.Response{
			ID:         "msg_refusal",
			Type:       "message",
			Role:       "assistant",
			Model:      "sonnet",
			StopReason: "refusal",
			Content:    nil,
		}},
	}}
	r := newMinimalRunner(t, client)
	res, err := r.RunTurn(context.Background(), nil, msgs.UserText("do something"))
	if err != nil {
		t.Fatalf("RunTurn err: %v", err)
	}
	if res.StopReason != "refusal" {
		t.Fatalf("StopReason = %q want refusal", res.StopReason)
	}
	if !containsText(res.Messages, "Usage Policy") {
		t.Fatalf("expected refusal message surfaced, got %d msgs with content: %v", len(res.Messages), res.Messages)
	}
	if len(client.calls) != 0 {
		t.Fatalf("refusal must not retry; remaining queued calls = %d (expected 0 remaining = 1 consumed)", len(client.calls))
	}
}

func TestRunTurnPauseTurnResumes(t *testing.T) {
	client := &fakeClient{calls: []fakeCall{
		{response: &anthropic.Response{
			ID:         "msg_pause",
			Type:       "message",
			Role:       "assistant",
			Model:      "sonnet",
			StopReason: "pause_turn",
			Content:    []contracts.ContentBlock{contracts.NewTextBlock("partial")},
		}},
		{response: &anthropic.Response{
			ID:         "msg_done",
			Type:       "message",
			Role:       "assistant",
			Model:      "sonnet",
			StopReason: "end_turn",
			Content:    []contracts.ContentBlock{contracts.NewTextBlock("finished")},
		}},
	}}
	r := newMinimalRunner(t, client)
	res, err := r.RunTurn(context.Background(), nil, msgs.UserText("go"))
	if err != nil {
		t.Fatalf("RunTurn err: %v", err)
	}
	// Both calls consumed means 2 API calls were made.
	if len(client.calls) != 0 {
		t.Fatalf("pause_turn must resume; remaining calls = %d (expected 0, meaning 2 consumed)", len(client.calls))
	}
	if res.StopReason != "end_turn" {
		t.Fatalf("final StopReason = %q want end_turn", res.StopReason)
	}
}

func TestRunTurnMaxTokensRecovers(t *testing.T) {
	client := &fakeClient{calls: []fakeCall{
		{response: &anthropic.Response{
			ID:         "msg_trunc",
			Type:       "message",
			Role:       "assistant",
			Model:      "sonnet",
			StopReason: "max_tokens",
			Content:    []contracts.ContentBlock{contracts.NewTextBlock("truncat")},
		}},
		{response: &anthropic.Response{
			ID:         "msg_cont",
			Type:       "message",
			Role:       "assistant",
			Model:      "sonnet",
			StopReason: "end_turn",
			Content:    []contracts.ContentBlock{contracts.NewTextBlock("ed and continued")},
		}},
	}}
	r := newMinimalRunner(t, client)
	res, err := r.RunTurn(context.Background(), nil, msgs.UserText("write a lot"))
	if err != nil {
		t.Fatalf("RunTurn err: %v", err)
	}
	// Both calls consumed means 2 API calls were made.
	if len(client.calls) != 0 {
		t.Fatalf("max_tokens must recover once; remaining calls = %d (expected 0, meaning 2 consumed)", len(client.calls))
	}
	if res.StopReason != "end_turn" {
		t.Fatalf("final StopReason = %q want end_turn", res.StopReason)
	}
}

func TestRunTurnMaxTokensRecoveryIsBounded(t *testing.T) {
	// Provide 4 max_tokens responses (exceeds the cap of 3) — the loop must stop at 3 recoveries.
	calls := make([]fakeCall, maxOutputTokensRecoveryLimit+1)
	for i := range calls {
		calls[i] = fakeCall{response: &anthropic.Response{
			ID:         "msg_trunc",
			Type:       "message",
			Role:       "assistant",
			Model:      "sonnet",
			StopReason: "max_tokens",
			Content:    []contracts.ContentBlock{contracts.NewTextBlock("truncated")},
		}}
	}
	client := &fakeClient{calls: calls}
	r := newMinimalRunner(t, client)
	res, err := r.RunTurn(context.Background(), nil, msgs.UserText("write forever"))
	if err != nil {
		t.Fatalf("RunTurn err: %v", err)
	}
	// Should have consumed exactly maxOutputTokensRecoveryLimit+1 calls (initial + 3 recoveries).
	consumed := (maxOutputTokensRecoveryLimit + 1) - len(client.calls)
	if consumed != maxOutputTokensRecoveryLimit+1 {
		t.Fatalf("expected %d calls consumed, got %d", maxOutputTokensRecoveryLimit+1, consumed)
	}
	// After cap, stop_reason should still be max_tokens.
	if res.StopReason != "max_tokens" {
		t.Fatalf("StopReason = %q want max_tokens after recovery cap", res.StopReason)
	}
}
