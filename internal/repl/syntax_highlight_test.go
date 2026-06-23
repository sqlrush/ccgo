package repl

import (
	"encoding/json"
	"strings"
	"testing"

	"ccgo/internal/contracts"
)

// TestRenderToolResultNoColorWhenSyntaxHighlightingDisabled verifies that when
// SyntaxHighlightingDisabled=true the diff output contains no ANSI color codes.
// CC ref: utils/settings/types.ts syntaxHighlightingDisabled — disable diff highlighting.
func TestRenderToolResultNoColorWhenSyntaxHighlightingDisabled(t *testing.T) {
	input, _ := json.Marshal(map[string]string{
		"file_path":  "hello.go",
		"old_string": "old text\n",
		"new_string": "new text\n",
	})
	tu := &contracts.ToolUse{Name: "Edit", Input: input}
	tr := &contracts.ToolResult{}

	// With syntax highlighting disabled, output must not contain ANSI escape codes.
	out := RenderToolResultTextWithColorOpt(tu, tr, false)
	if strings.Contains(out, "\x1b[") {
		t.Errorf("expected no ANSI codes when syntaxHighlightingDisabled=true; got %q", out)
	}
	// The diff should still contain the changed line content.
	if !strings.Contains(out, "new text") && !strings.Contains(out, "old text") {
		t.Errorf("expected diff content in output; got %q", out)
	}
}

// TestRenderToolResultColorWhenSyntaxHighlightingEnabled verifies that when
// SyntaxHighlightingDisabled=false (the default) the diff output contains ANSI codes.
func TestRenderToolResultColorWhenSyntaxHighlightingEnabled(t *testing.T) {
	input, _ := json.Marshal(map[string]string{
		"file_path":  "hello.go",
		"old_string": "old text\n",
		"new_string": "new text\n",
	})
	tu := &contracts.ToolUse{Name: "Edit", Input: input}
	tr := &contracts.ToolResult{}

	out := RenderToolResultTextWithColorOpt(tu, tr, true)
	if !strings.Contains(out, "\x1b[") {
		t.Errorf("expected ANSI codes when syntaxHighlightingDisabled=false; got %q", out)
	}
}
