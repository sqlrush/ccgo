package repl

import (
	"encoding/json"

	"ccgo/internal/contracts"
	"ccgo/internal/native"
)

// editToolInput is the union of field names used by Edit and Write tools.
type editToolInput struct {
	FilePath  string `json:"file_path"`
	OldString string `json:"old_string"`
	NewString string `json:"new_string"`
	Content   string `json:"content"`
}

// renderToolResultText renders a tool result for the transcript. Edit/Write
// tools get a colored unified diff (via native.BuildColorDiff); everything
// else gets a concise ok/error summary line. Never panics on malformed input.
func renderToolResultText(tu *contracts.ToolUse, tr *contracts.ToolResult) string {
	return RenderToolResultTextWithColorOpt(tu, tr, true)
}

// RenderToolResultTextWithColorOpt is like renderToolResultText but allows the
// caller to disable ANSI syntax highlighting in the diff output.
// When color is false, the unified diff is returned without ANSI escape codes.
// CC ref: utils/settings/types.ts syntaxHighlightingDisabled — disable diff highlighting.
func RenderToolResultTextWithColorOpt(tu *contracts.ToolUse, tr *contracts.ToolResult, color bool) string {
	if tu != nil && isEditTool(tu.Name) {
		if diff, ok := editDiffWithColor(tu, color); ok {
			return diff
		}
	}
	if tr != nil && tr.IsError {
		return "  ⎿ error"
	}
	return "  ⎿ ok"
}

// isEditTool reports whether a tool name is one that modifies file content.
func isEditTool(name string) bool {
	switch name {
	case "Edit", "Write", "MultiEdit", "NotebookEdit", "SedEdit":
		return true
	default:
		return false
	}
}

// editDiff parses the tool-use input and returns a colored unified diff string.
// Returns ("", false) when the input cannot be parsed or carries no diff content.
func editDiff(tu *contracts.ToolUse) (string, bool) {
	return editDiffWithColor(tu, true)
}

// editDiffWithColor is like editDiff but honours the color parameter.
// When color is false, only the plain unified diff is returned (no ANSI codes).
// CC ref: utils/settings/types.ts syntaxHighlightingDisabled.
func editDiffWithColor(tu *contracts.ToolUse, color bool) (string, bool) {
	var in editToolInput
	if err := json.Unmarshal(tu.Input, &in); err != nil {
		return "", false
	}

	oldText := in.OldString
	newText := in.NewString
	// Write tool: no old_string; entire new content supplied via "content".
	if newText == "" && in.Content != "" {
		newText = in.Content
	}
	if oldText == "" && newText == "" {
		return "", false
	}

	cd := native.BuildColorDiff(oldText, newText, native.ColorDiffOptions{
		Path:  in.FilePath,
		Color: color,
	})
	// When color requested, prefer ANSI-colored output; fall back to plain diff.
	// When color disabled, use the plain unified diff only.
	var text string
	if color {
		text = cd.Colored
	}
	if text == "" {
		text = cd.Unified
	}
	if text == "" {
		return "", false
	}
	return text, true
}
