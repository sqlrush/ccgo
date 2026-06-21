package hooks

import (
	"encoding/json"
	"testing"

	"ccgo/internal/contracts"
	"ccgo/internal/tool"
)

func TestBuildInputBaseFields(t *testing.T) {
	ctx := tool.Context{
		WorkingDirectory: "/work",
		SessionID:        "sess_1",
		Metadata: map[string]any{
			tool.MetadataSessionPathKey: "/tmp/t.jsonl",
		},
	}
	event := tool.HookEvent{
		Phase:   tool.HookSessionStart,
		Payload: map[string]any{"source": "startup"},
	}
	raw, err := BuildInput(ctx, event)
	if err != nil {
		t.Fatal(err)
	}
	var got map[string]any
	if err := json.Unmarshal([]byte(raw), &got); err != nil {
		t.Fatal(err)
	}
	want := map[string]string{
		"session_id":      "sess_1",
		"transcript_path": "/tmp/t.jsonl",
		"cwd":             "/work",
		"hook_event_name": "SessionStart",
		"source":          "startup",
	}
	for k, v := range want {
		if got[k] != v {
			t.Fatalf("field %s = %v want %v", k, got[k], v)
		}
	}
}

func TestApplyHookSpecificOutputSessionStartContext(t *testing.T) {
	raw := `{"hookSpecificOutput":{"hookEventName":"SessionStart","additionalContext":"extra ctx"}}`
	result, ok := hookResultFromJSON(tool.HookSessionStart, raw)
	if !ok {
		t.Fatal("parse failed")
	}
	if result.Message != "extra ctx" {
		t.Fatalf("message = %q want %q", result.Message, "extra ctx")
	}
}

func TestBuildInputRejectsInvalidPayload(t *testing.T) {
	// Channel values are not JSON-serializable; BuildInput must error, not panic.
	ctx := tool.Context{WorkingDirectory: "/w"}
	event := tool.HookEvent{Phase: tool.HookNotification, Payload: map[string]any{"bad": make(chan int)}}
	if _, err := BuildInput(ctx, event); err == nil {
		t.Fatal("expected error for non-serializable payload")
	}
	_ = contracts.PermissionAllow // keep import
}
