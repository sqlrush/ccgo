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
	if tu != nil && isEditTool(tu.Name) {
		if diff, ok := editDiff(tu); ok {
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
		Color: true,
	})
	// Prefer ANSI-colored output; fall back to plain unified diff.
	text := cd.Colored
	if text == "" {
		text = cd.Unified
	}
	if text == "" {
		return "", false
	}
	return text, true
}
