// Package contextreport provides a deterministic text formatter for context
// window token-usage reports, used by the /context slash command.
package contextreport

import (
	"fmt"
	"strings"
)

// Report holds the token counts used to render a context usage summary.
// All fields are plain integers; the formatter is side-effect-free.
type Report struct {
	// ModelName is the human-readable model identifier (e.g. "claude-sonnet-4-6").
	ModelName string
	// WindowTokens is the effective context window size for the model.
	// Must be ≥ 0; zero disables percentage output.
	WindowTokens int
	// PromptTokens is the estimated tokens consumed by the conversation turns.
	PromptTokens int
	// SystemTokens is the estimated tokens consumed by the system prompt.
	SystemTokens int
	// ToolTokens is the estimated tokens consumed by tool definitions.
	ToolTokens int
}

// Format returns a deterministic, human-readable context usage report.
// It is safe to call with a zero WindowTokens (no divide-by-zero).
func Format(r Report) string {
	used := r.PromptTokens + r.SystemTokens + r.ToolTokens
	remaining := r.WindowTokens - used

	var sb strings.Builder

	sb.WriteString("Context window usage\n")
	sb.WriteString(strings.Repeat("─", 40) + "\n")
	sb.WriteString(fmt.Sprintf("  Model:         %s\n", r.ModelName))
	sb.WriteString(fmt.Sprintf("  Window:        %d tokens\n", r.WindowTokens))
	sb.WriteString(fmt.Sprintf("  Used:          %d tokens\n", used))
	if r.SystemTokens > 0 {
		sb.WriteString(fmt.Sprintf("    System:      %d tokens\n", r.SystemTokens))
	}
	if r.ToolTokens > 0 {
		sb.WriteString(fmt.Sprintf("    Tools:       %d tokens\n", r.ToolTokens))
	}
	if r.PromptTokens > 0 {
		sb.WriteString(fmt.Sprintf("    Messages:    %d tokens\n", r.PromptTokens))
	}
	if r.WindowTokens > 0 {
		pct := float64(used) * 100.0 / float64(r.WindowTokens)
		sb.WriteString(fmt.Sprintf("  Used %%:        %.1f%%\n", pct))
		sb.WriteString(fmt.Sprintf("  Remaining:     %d tokens\n", remaining))
	} else {
		sb.WriteString("  Used %:        (unknown window size)\n")
	}

	return sb.String()
}
