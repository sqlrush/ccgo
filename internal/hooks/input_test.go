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

// TestBuildInputAgentIDAndType verifies that agent_id and agent_type from the
// event payload are included in the base hook input (HOOK-54).
func TestBuildInputAgentIDAndType(t *testing.T) {
	ctx := tool.Context{
		WorkingDirectory: "/work",
		SessionID:        "sess_agent",
	}
	event := tool.HookEvent{
		Phase: tool.HookSubagentStop,
		Payload: map[string]any{
			"agent_id":   "subagent-42",
			"agent_type": "task",
		},
	}
	raw, err := BuildInput(ctx, event)
	if err != nil {
		t.Fatal(err)
	}
	var got map[string]any
	if err := json.Unmarshal([]byte(raw), &got); err != nil {
		t.Fatal(err)
	}
	if got["agent_id"] != "subagent-42" {
		t.Fatalf("agent_id = %v want %q", got["agent_id"], "subagent-42")
	}
	if got["agent_type"] != "task" {
		t.Fatalf("agent_type = %v want %q", got["agent_type"], "task")
	}
}

// TestBuildInputAgentFieldsAbsentWhenNotInPayload verifies that agent_id and
// agent_type are omitted when not present in the event payload (main thread hooks).
func TestBuildInputAgentFieldsAbsentWhenNotInPayload(t *testing.T) {
	ctx := tool.Context{
		WorkingDirectory: "/work",
		SessionID:        "sess_main",
	}
	event := tool.HookEvent{
		Phase:   tool.HookStop,
		Payload: map[string]any{},
	}
	raw, err := BuildInput(ctx, event)
	if err != nil {
		t.Fatal(err)
	}
	var got map[string]any
	if err := json.Unmarshal([]byte(raw), &got); err != nil {
		t.Fatal(err)
	}
	if _, ok := got["agent_id"]; ok {
		t.Fatal("agent_id must not be present for main-thread hooks")
	}
	if _, ok := got["agent_type"]; ok {
		t.Fatal("agent_type must not be present when not in payload")
	}
}
