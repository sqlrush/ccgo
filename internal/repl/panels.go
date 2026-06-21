package repl

import (
	"fmt"
	"strings"
	"time"

	"ccgo/internal/contracts"
)

// SessionStats is the snapshot of usage shown in cost/context/status panels.
type SessionStats struct {
	Model        string
	InputTokens  int
	OutputTokens int
	CostUSD      float64
	ContextUsed  int
	ContextMax   int
	APIDuration  time.Duration
}

func costPanel(s SessionStats) string {
	return fmt.Sprintf(
		"Total cost: $%.2f\nAPI duration: %s\nTokens: %d in / %d out",
		s.CostUSD, s.APIDuration.Round(time.Millisecond), s.InputTokens, s.OutputTokens,
	)
}

func contextPanel(s SessionStats) string {
	if s.ContextMax <= 0 {
		return fmt.Sprintf("Context: %d tokens used (limit unknown)", s.ContextUsed)
	}
	pct := s.ContextUsed * 100 / s.ContextMax
	return fmt.Sprintf("Context: %d / %d tokens (%d%%)", s.ContextUsed, s.ContextMax, pct)
}

func statusPanel(s SessionStats, mode contracts.PermissionMode) string {
	var b strings.Builder
	fmt.Fprintf(&b, "Model: %s\n", s.Model)
	fmt.Fprintf(&b, "Mode: %s\n", modeLabel(mode))
	b.WriteString(contextPanel(s))
	b.WriteString("\n")
	b.WriteString(costPanel(s))
	return b.String()
}

func modeLabel(mode contracts.PermissionMode) string {
	switch mode {
	case contracts.PermissionAcceptEdits:
		return "accept edits"
	case contracts.PermissionPlan:
		return "plan mode"
	case contracts.PermissionBypassPermissions:
		return "bypass permissions"
	default:
		return "default"
	}
}

// DoctorCheck is one diagnostic line in the /doctor report.
type DoctorCheck struct {
	Name   string
	Status string
	Detail string
}

func doctorReport(checks []DoctorCheck) string {
	var b strings.Builder
	b.WriteString("Claude Code Doctor\n")
	for _, c := range checks {
		mark := statusMark(c.Status)
		fmt.Fprintf(&b, "%s %s: %s\n", mark, c.Name, c.Detail)
	}
	return strings.TrimRight(b.String(), "\n")
}

func statusMark(status string) string {
	switch strings.ToLower(status) {
	case "ok", "pass":
		return "✓"
	case "warn", "warning":
		return "!"
	case "fail", "error":
		return "✗"
	default:
		return "·"
	}
}
