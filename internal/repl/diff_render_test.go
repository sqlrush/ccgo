package repl

import (
	"encoding/json"
	"strings"
	"testing"

	"ccgo/internal/contracts"
)

func TestRenderToolResultTextEditShowsDiff(t *testing.T) {
	tu := &contracts.ToolUse{
		ID:   "t1",
		Name: "Edit",
		Input: json.RawMessage(`{"file_path":"/x.go","old_string":"foo","new_string":"bar"}`),
	}
	tr := &contracts.ToolResult{ToolUseID: "t1"}
	out := renderToolResultText(tu, tr)
	if !strings.Contains(out, "foo") || !strings.Contains(out, "bar") {
		t.Fatalf("diff render missing old/new text: %q", out)
	}
}

func TestRenderToolResultTextNonEditSummary(t *testing.T) {
	tu := &contracts.ToolUse{ID: "t2", Name: "Read", Input: json.RawMessage(`{}`)}
	tr := &contracts.ToolResult{ToolUseID: "t2"}
	out := renderToolResultText(tu, tr)
	if strings.Contains(out, "foo") {
		t.Fatalf("non-edit should not diff: %q", out)
	}
	if out == "" {
		t.Fatal("non-edit should still produce a summary line")
	}
}

func TestRenderToolResultTextError(t *testing.T) {
	tu := &contracts.ToolUse{ID: "t3", Name: "Read", Input: json.RawMessage(`{}`)}
	tr := &contracts.ToolResult{ToolUseID: "t3", IsError: true}
	out := renderToolResultText(tu, tr)
	if !strings.Contains(strings.ToLower(out), "error") {
		t.Fatalf("error result should mention error: %q", out)
	}
}

func TestRenderToolResultTextWriteShowsDiff(t *testing.T) {
	tu := &contracts.ToolUse{
		ID:    "t4",
		Name:  "Write",
		Input: json.RawMessage(`{"file_path":"/y.go","content":"hello world"}`),
	}
	tr := &contracts.ToolResult{ToolUseID: "t4"}
	out := renderToolResultText(tu, tr)
	if !strings.Contains(out, "hello") {
		t.Fatalf("write diff should show new content: %q", out)
	}
}

func TestRenderToolResultTextBadJSON(t *testing.T) {
	tu := &contracts.ToolUse{
		ID:    "t5",
		Name:  "Edit",
		Input: json.RawMessage(`not valid json`),
	}
	tr := &contracts.ToolResult{ToolUseID: "t5"}
	out := renderToolResultText(tu, tr)
	// Should fall back to summary, not panic.
	if out == "" {
		t.Fatal("bad JSON input should still produce a summary line")
	}
}

func TestRenderToolResultTextNilToolUse(t *testing.T) {
	tr := &contracts.ToolResult{ToolUseID: "t6"}
	out := renderToolResultText(nil, tr)
	if out == "" {
		t.Fatal("nil ToolUse should still produce a summary line")
	}
}

func TestRenderToolResultTextEmptyOldNew(t *testing.T) {
	// Edit with empty old_string and new_string falls back to summary.
	tu := &contracts.ToolUse{
		ID:    "t7",
		Name:  "Edit",
		Input: json.RawMessage(`{"file_path":"/z.go","old_string":"","new_string":""}`),
	}
	tr := &contracts.ToolResult{ToolUseID: "t7"}
	out := renderToolResultText(tu, tr)
	if out == "" {
		t.Fatal("empty old/new should still produce a summary line")
	}
}
