package conversation

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"

	anthropic "ccgo/internal/api/anthropic"
	"ccgo/internal/contracts"
	"ccgo/internal/messages"
	"ccgo/internal/tool"
)

// TestStopFailureHookFiredWhenAPIReturnsError verifies that when send() fails
// (API error), the runner fires StopFailure hooks and returns the error (HOOK-32).
// We test runStopFailureHooks directly since it is a package-internal method.
func TestStopFailureHookFiredWhenAPIReturnsError(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("shell hooks require /bin/sh; skipping on Windows")
	}
	dir := t.TempDir()
	capture := filepath.Join(dir, "captured.json")
	script := filepath.Join(dir, "capture.sh")
	if err := os.WriteFile(script, []byte("#!/bin/sh\ncat > "+shellQuoteConv(capture)+"\n"), 0755); err != nil {
		t.Fatal(err)
	}

	r := Runner{
		WorkingDirectory: dir,
		SessionID:        "sess_sf",
		settingsOverride: &contracts.Settings{
			Hooks: map[string]any{
				"StopFailure": []any{map[string]any{
					"hooks": []any{map[string]any{
						"type":    "command",
						"command": script,
					}},
				}},
			},
		},
	}

	// Fire StopFailure directly.
	r.runStopFailureHooks(context.Background(), "rate_limit_error")

	data, err := os.ReadFile(capture)
	if err != nil {
		t.Fatalf("hook was not executed or capture file missing: %v", err)
	}
	var payload map[string]any
	if err := json.Unmarshal(data, &payload); err != nil {
		t.Fatalf("captured payload not valid JSON: %v\ndata: %s", err, data)
	}
	if payload["hook_event_name"] != tool.HookStopFailure {
		t.Fatalf("hook_event_name = %q, want %q", payload["hook_event_name"], tool.HookStopFailure)
	}
	errField, _ := payload["error"].(string)
	if errField == "" {
		t.Fatalf("StopFailure hook payload missing 'error' field; payload: %v", payload)
	}
	if !strings.Contains(errField, "rate_limit_error") {
		t.Fatalf("StopFailure 'error' field = %q, want it to contain the error message", errField)
	}
}

// TestStopFailureFiredFromRunTurnOnAPIError verifies that when the API client
// returns an error during RunTurn, the StopFailure hook fires automatically
// without the caller needing to do anything special (HOOK-32 production path).
func TestStopFailureFiredFromRunTurnOnAPIError(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("shell hooks require /bin/sh; skipping on Windows")
	}
	dir := t.TempDir()
	capture := filepath.Join(dir, "stop_failure.json")
	script := filepath.Join(dir, "capture.sh")
	if err := os.WriteFile(script, []byte("#!/bin/sh\ncat > "+shellQuoteConv(capture)+"\n"), 0755); err != nil {
		t.Fatal(err)
	}

	apiErr := anthropic.APIError{StatusCode: 500, Type: "server_error", Message: "internal error"}
	client := &fakeClient{calls: []fakeCall{{err: apiErr}}}
	runner := Runner{
		Client:           client,
		Model:            "sonnet",
		MaxTokens:        128,
		SessionID:        "sess_sf_run",
		WorkingDirectory: dir,
		settingsOverride: &contracts.Settings{
			Hooks: map[string]any{
				"StopFailure": []any{map[string]any{
					"hooks": []any{map[string]any{
						"type":    "command",
						"command": script,
					}},
				}},
			},
		},
	}

	_, err := runner.RunTurn(context.Background(), nil, messages.UserText("hello"))
	if err == nil {
		t.Fatal("expected error from RunTurn when API fails")
	}

	// Give the hook time to write (it's synchronous in our path).
	_ = time.Sleep // no sleep needed — runStopFailureHooks is synchronous.

	data, readErr := os.ReadFile(capture)
	if readErr != nil {
		t.Fatalf("StopFailure hook did not execute (capture file missing): %v", readErr)
	}
	var payload map[string]any
	if jsonErr := json.Unmarshal(data, &payload); jsonErr != nil {
		t.Fatalf("captured payload not valid JSON: %v\ndata: %s", jsonErr, data)
	}
	if payload["hook_event_name"] != tool.HookStopFailure {
		t.Fatalf("hook_event_name = %q, want %q", payload["hook_event_name"], tool.HookStopFailure)
	}
}

// TestStopHookContinuationRepromptsModel verifies that when a Stop hook blocks
// (exit 2 / Block=true), the model is re-prompted (HOOK-31 Stop continuation).
// The test uses two sequential fakeClient calls: first the normal end_turn,
// then the re-prompted call also returns end_turn so the loop terminates.
func TestStopHookContinuationRepromptsModel(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("shell hooks require /bin/sh; skipping on Windows")
	}

	// Hook script: blocks the first call only.
	dir := t.TempDir()
	counterFile := filepath.Join(dir, "count.txt")
	script := filepath.Join(dir, "maybe_block.sh")
	// Write 0 to counter; on first invocation, increment and return block JSON;
	// on second invocation, return exit 0.
	scriptContent := `#!/bin/sh
COUNT=$(cat ` + shellQuoteConv(counterFile) + ` 2>/dev/null || echo 0)
echo $((COUNT+1)) > ` + shellQuoteConv(counterFile) + `
if [ "$COUNT" -eq "0" ]; then
  echo '{"continue":false,"stopReason":"stop_hook_block_test"}'
fi
`
	if err := os.WriteFile(counterFile, []byte("0"), 0644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(script, []byte(scriptContent), 0755); err != nil {
		t.Fatal(err)
	}

	// Two API calls: first triggers Stop hook (which blocks), second is the
	// continuation call after the hook forced re-prompting.
	endTurnReply := func(n int) *anthropic.Response {
		return &anthropic.Response{
			Type:       "message",
			Role:       "assistant",
			Model:      "sonnet",
			StopReason: "end_turn",
			Content:    []contracts.ContentBlock{contracts.NewTextBlock("reply")},
		}
	}
	client := &fakeClient{calls: []fakeCall{
		{response: endTurnReply(1)},
		{response: endTurnReply(2)},
	}}

	runner := Runner{
		Client:           client,
		Model:            "sonnet",
		MaxTokens:        128,
		SessionID:        "sess_stop_cont",
		WorkingDirectory: dir,
		settingsOverride: &contracts.Settings{
			Hooks: map[string]any{
				"Stop": []any{map[string]any{
					"hooks": []any{map[string]any{
						"type":    "command",
						"command": script,
					}},
				}},
			},
		},
	}

	result, err := runner.RunTurn(context.Background(), nil, messages.UserText("go"))
	if err != nil {
		t.Fatalf("RunTurn error: %v", err)
	}
	_ = result

	// Verify the hook ran (at least twice — once to block, once to allow).
	data, err := os.ReadFile(counterFile)
	if err != nil {
		t.Fatal(err)
	}
	countStr := strings.TrimSpace(string(data))
	if countStr != "2" {
		t.Fatalf("Stop hook should have run 2 times (block then allow), counter = %q", countStr)
	}

	// Verify the API was called twice (first call → hook blocks → re-prompt → second call).
	if len(client.requests) < 2 {
		t.Fatalf("expected at least 2 API requests (stop hook continuation), got %d", len(client.requests))
	}
}
