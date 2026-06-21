package todotools

import (
	"context"
	"encoding/json"
	"testing"

	"ccgo/internal/tool"
)

func TestTodoWriteRequiresActiveForm(t *testing.T) {
	toolImpl := NewTodoWriteTool()
	ctx := tool.Context{Context: context.Background()}

	// New schema: content/status/activeForm, no id/priority.
	good, _ := json.Marshal(map[string]any{"todos": []any{
		map[string]any{"content": "Write the parser", "status": "in_progress", "activeForm": "Writing the parser"},
	}})
	if err := toolImpl.Validate(ctx, good); err != nil {
		t.Fatalf("valid activeForm todo failed: %v", err)
	}

	// Missing activeForm → error.
	noForm, _ := json.Marshal(map[string]any{"todos": []any{
		map[string]any{"content": "x", "status": "pending"},
	}})
	if err := toolImpl.Validate(ctx, noForm); err == nil {
		t.Fatal("expected error when activeForm missing")
	}

	// Legacy priority field → rejected as not allowed.
	legacy, _ := json.Marshal(map[string]any{"todos": []any{
		map[string]any{"content": "x", "status": "pending", "activeForm": "Doing x", "priority": "high"},
	}})
	if err := toolImpl.Validate(ctx, legacy); err == nil {
		t.Fatal("expected error for legacy priority field")
	}
}
