package conversation

import (
	"context"
	"encoding/json"
	"path/filepath"
	"strings"
	"testing"

	"ccgo/internal/api/anthropic"
	compactpkg "ccgo/internal/compact"
	"ccgo/internal/contracts"
	msgs "ccgo/internal/messages"
	"ccgo/internal/tool"
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

// TestRunTurnContextWindowExceededRecoversViaCompact verifies that a
// model_context_window_exceeded stop_reason triggers a forced compaction and retries
// the API call once, ultimately returning the end_turn result.
func TestRunTurnContextWindowExceededRecoversViaCompact(t *testing.T) {
	// First call: ctx-window exceeded (triggers compaction).
	// Second call: compact summary.
	// Third call: retry after compaction succeeds.
	client := &fakeClient{calls: []fakeCall{
		{response: &anthropic.Response{
			ID:         "msg_ctx_exceeded",
			Type:       "message",
			Role:       "assistant",
			Model:      "sonnet",
			StopReason: "model_context_window_exceeded",
			Content:    nil,
		}},
		{response: &anthropic.Response{
			ID:         "msg_summary",
			Type:       "message",
			Role:       "assistant",
			Model:      "sonnet",
			StopReason: "end_turn",
			Content:    []contracts.ContentBlock{contracts.NewTextBlock("compacted summary")},
		}},
		{response: &anthropic.Response{
			ID:         "msg_recovered",
			Type:       "message",
			Role:       "assistant",
			Model:      "sonnet",
			StopReason: "end_turn",
			Content:    []contracts.ContentBlock{contracts.NewTextBlock("recovered")},
		}},
	}}
	transcriptPath := filepath.Join(t.TempDir(), "session.jsonl")
	r := Runner{
		Client:        client,
		CompactClient: client,
		Model:         "sonnet",
		MaxTokens:     128,
		SessionID:     "sess_ctx_window_recovery",
		SessionPath:   transcriptPath,
		// Enabled but NOT Force — the initial maybeAutoCompact at RunTurn entry
		// won't fire (token usage is low), but forceCompact sets Force=true internally
		// to trigger compaction on ctx-window recovery.
		AutoCompact: &compactpkg.AutoConfig{
			Enabled:  true,
			Force:    false,
			KeepLast: 1,
		},
	}
	// Provide some history to compact.
	history := []contracts.Message{
		msgs.UserText("old message one"),
		msgs.AssistantText("old reply one", "sonnet", nil),
		msgs.UserText("old message two"),
		msgs.AssistantText("old reply two", "sonnet", nil),
	}
	res, err := r.RunTurn(context.Background(), history, msgs.UserText("continue"))
	if err != nil {
		t.Fatalf("RunTurn err: %v", err)
	}
	if !res.Compacted {
		t.Fatalf("expected compaction to be triggered for ctx-window recovery; result = %#v", res)
	}
	// 3 API calls: main request → compact summary → retry after compact.
	if len(client.calls) != 0 {
		t.Fatalf("expected all 3 calls consumed; remaining = %d", len(client.calls))
	}
	if res.StopReason != "end_turn" {
		t.Fatalf("final StopReason = %q want end_turn", res.StopReason)
	}
}

// TestRunTurnContextWindowExceededIsBounded verifies that if compaction cannot reduce
// history (ok==false from forceCompact), the error message is surfaced and the loop stops
// without retrying — no infinite loop.
func TestRunTurnContextWindowExceededIsBounded(t *testing.T) {
	// Single call: ctx-window exceeded. No compact client configured, so
	// forceCompact will fail to reduce history (ShouldRun returns false without
	// a proper AutoCompact config that enables Force). We test by NOT setting
	// AutoCompact, so forceCompact's internal forced runner still has no client
	// that returns a summary — and we just get the fallback surface+stop.
	client := &fakeClient{calls: []fakeCall{
		{response: &anthropic.Response{
			ID:         "msg_ctx_exceeded",
			Type:       "message",
			Role:       "assistant",
			Model:      "sonnet",
			StopReason: "model_context_window_exceeded",
			Content:    nil,
		}},
	}}
	// Runner with NO AutoCompact set — forceCompact will see no config and return ok=false.
	r := newMinimalRunner(t, client)
	res, err := r.RunTurn(context.Background(), nil, msgs.UserText("hi"))
	if err != nil {
		t.Fatalf("RunTurn err: %v", err)
	}
	// No compaction should have occurred.
	if res.Compacted {
		t.Fatalf("should not have compacted without AutoCompact config; result = %#v", res)
	}
	// Only one API call was made (no retry).
	if len(client.calls) != 0 {
		t.Fatalf("should have consumed exactly 1 call; remaining = %d", len(client.calls))
	}
	// The context window error message must be surfaced.
	if !containsText(res.Messages, "context window") {
		t.Fatalf("expected ctx-window error message surfaced; messages = %v", res.Messages)
	}
	// StopReason stays as model_context_window_exceeded.
	if res.StopReason != "model_context_window_exceeded" {
		t.Fatalf("StopReason = %q want model_context_window_exceeded", res.StopReason)
	}
}

// TestRunTurnContextWindowRetryHasNoEmptyAssistant asserts that the history sent
// on the retry request (3rd API call) after ctx-window compaction does NOT contain
// an assistant message with empty content. Before the fix, the loop appended an
// empty-content assistant message to history before detecting the stop_reason, and
// that empty turn was forwarded to the retry — which the real Anthropic API rejects.
func TestRunTurnContextWindowRetryHasNoEmptyAssistant(t *testing.T) {
	client := &fakeClient{calls: []fakeCall{
		// Call 1: ctx-window exceeded — response.Content is nil.
		{response: &anthropic.Response{
			ID:         "msg_ctx_exceeded",
			Type:       "message",
			Role:       "assistant",
			Model:      "sonnet",
			StopReason: "model_context_window_exceeded",
			Content:    nil,
		}},
		// Call 2: compact summary.
		{response: &anthropic.Response{
			ID:         "msg_summary",
			Type:       "message",
			Role:       "assistant",
			Model:      "sonnet",
			StopReason: "end_turn",
			Content:    []contracts.ContentBlock{contracts.NewTextBlock("compacted summary")},
		}},
		// Call 3: retry after compaction.
		{response: &anthropic.Response{
			ID:         "msg_recovered",
			Type:       "message",
			Role:       "assistant",
			Model:      "sonnet",
			StopReason: "end_turn",
			Content:    []contracts.ContentBlock{contracts.NewTextBlock("recovered")},
		}},
	}}
	transcriptPath := filepath.Join(t.TempDir(), "session.jsonl")
	r := Runner{
		Client:        client,
		CompactClient: client,
		Model:         "sonnet",
		MaxTokens:     128,
		SessionID:     "sess_no_empty_assistant",
		SessionPath:   transcriptPath,
		AutoCompact: &compactpkg.AutoConfig{
			Enabled:  true,
			Force:    false,
			KeepLast: 1,
		},
	}
	history := []contracts.Message{
		msgs.UserText("old message one"),
		msgs.AssistantText("old reply one", "sonnet", nil),
		msgs.UserText("old message two"),
		msgs.AssistantText("old reply two", "sonnet", nil),
	}
	_, err := r.RunTurn(context.Background(), history, msgs.UserText("continue"))
	if err != nil {
		t.Fatalf("RunTurn err: %v", err)
	}
	// There must be exactly 3 recorded requests (initial, compact summary, retry).
	if len(client.requests) != 3 {
		t.Fatalf("expected 3 API requests; got %d", len(client.requests))
	}
	// Inspect the 3rd (retry) request — no message may be an assistant with empty content.
	retryReq := client.requests[2]
	for i, m := range retryReq.Messages {
		if m.Role == "assistant" && len(m.Content) == 0 {
			t.Fatalf("retry request message[%d] is an assistant message with empty content — empty assistant turn not stripped before compaction retry", i)
		}
	}
}

// TestRunTurnPauseTurnCapReachableWithoutToolRoundError verifies that N pause_turn responses
// where N > maxToolRounds default (8) hit the graceful pauseTurnLimitMessage, NOT the hard
// "maximum tool rounds exceeded" error. This was previously broken because each pause_turn
// resume consumed the shared tool-round budget.
func TestRunTurnPauseTurnCapReachableWithoutToolRoundError(t *testing.T) {
	// Build maxPauseTurnResumes+1 pause_turn responses (11 total: 1 initial + 10 resumes),
	// followed by a final end_turn that must never be reached because the cap fires first.
	// We need maxPauseTurnResumes+1 pause_turns: the first sends, then resumes 1..10,
	// then on the 11th send the cap check fires before sending.
	calls := make([]fakeCall, maxPauseTurnResumes+1)
	for i := range calls {
		calls[i] = fakeCall{response: &anthropic.Response{
			ID:         "msg_pause",
			Type:       "message",
			Role:       "assistant",
			Model:      "sonnet",
			StopReason: "pause_turn",
			Content:    []contracts.ContentBlock{contracts.NewTextBlock("pausing")},
		}}
	}
	// Append one more end_turn that is never reached (cap fires first).
	calls = append(calls, fakeCall{response: &anthropic.Response{
		ID:         "msg_end",
		Type:       "message",
		Role:       "assistant",
		Model:      "sonnet",
		StopReason: "end_turn",
		Content:    []contracts.ContentBlock{contracts.NewTextBlock("never reached")},
	}})
	client := &fakeClient{calls: calls}
	// Use default MaxToolRounds (8) — the pause cap (10) exceeds it; with the fix,
	// the graceful cap message is reachable without hitting the tool-round error.
	r := newMinimalRunner(t, client)
	res, err := r.RunTurn(context.Background(), nil, msgs.UserText("go"))
	// Must not return an error (the hard tool-round error must NOT fire).
	if err != nil {
		t.Fatalf("RunTurn returned unexpected error (tool-round budget must not starve pause_turn cap): %v", err)
	}
	// The graceful pause cap message must have been surfaced.
	if !containsText(res.Messages, pauseTurnLimitText) {
		t.Fatalf("expected pause-cap message %q surfaced; messages = %v", pauseTurnLimitText, res.Messages)
	}
	// Exactly maxPauseTurnResumes+1 calls consumed (initial + 10 resumes; cap fires
	// before the 12th send so the end_turn call is never consumed).
	consumed := len(calls) - len(client.calls)
	if consumed != maxPauseTurnResumes+1 {
		t.Fatalf("expected %d API calls consumed (initial+%d resumes); got %d", maxPauseTurnResumes+1, maxPauseTurnResumes, consumed)
	}
}

// TestRunTurnGenuineToolRoundsBounded verifies that genuine tool-execution rounds
// are still bounded by maxToolRounds. This ensures the fix to I1 (excluding recoveries
// from the budget) does not inadvertently remove the tool-loop safety backstop.
func TestRunTurnGenuineToolRoundsBounded(t *testing.T) {
	// Register a trivial no-op tool so tool calls are actually executed.
	registry, err := tool.NewRegistry(tool.FuncTool{
		DefinitionValue: contracts.ToolDefinition{
			Name:        "Noop",
			Description: "does nothing",
			ReadOnly:    true,
			InputSchema: contracts.JSONSchema{"type": "object"},
		},
		CallFunc: func(ctx tool.Context, raw json.RawMessage, sink tool.ProgressSink) (contracts.ToolResult, error) {
			return contracts.ToolResult{Content: "ok"}, nil
		},
	})
	if err != nil {
		t.Fatal(err)
	}

	// Build maxToolRounds+1 tool_use responses so the loop exhausts the budget.
	maxRounds := 3 // small value to keep test fast
	calls := make([]fakeCall, maxRounds+1)
	for i := range calls {
		calls[i] = fakeCall{response: &anthropic.Response{
			ID:         "msg_tool",
			Type:       "message",
			Role:       "assistant",
			Model:      "sonnet",
			StopReason: "tool_use",
			Content: []contracts.ContentBlock{{
				Type:  contracts.ContentToolUse,
				ID:    "toolu_loop",
				Name:  "Noop",
				Input: json.RawMessage(`{}`),
			}},
		}}
	}
	client := &fakeClient{calls: calls}
	r := Runner{
		Client:        client,
		Tools:         tool.NewExecutor(registry),
		Model:         "sonnet",
		MaxTokens:     128,
		MaxToolRounds: maxRounds,
		SessionID:     "sess_tool_round_limit",
	}
	_, err = r.RunTurn(context.Background(), nil, msgs.UserText("loop"))
	if err == nil {
		t.Fatal("expected 'maximum tool rounds exceeded' error but got nil")
	}
	if !strings.Contains(err.Error(), "maximum tool rounds exceeded") {
		t.Fatalf("expected tool-round limit error; got %v", err)
	}
}
