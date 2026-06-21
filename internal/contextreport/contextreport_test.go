package contextreport

import (
	"strings"
	"testing"
)

func TestReportBreakdown(t *testing.T) {
	r := Report{
		ModelName:    "claude-sonnet",
		WindowTokens: 200000,
		PromptTokens: 50000,
		SystemTokens: 2000,
		ToolTokens:   1000,
	}
	out := Format(r)
	if !strings.Contains(out, "claude-sonnet") {
		t.Fatalf("missing model name: %q", out)
	}
	if !strings.Contains(out, "200000") && !strings.Contains(out, "200,000") {
		t.Fatalf("missing window size: %q", out)
	}
	// Used = prompt+system+tool = 53000; ~26.5%.
	if !strings.Contains(out, "53000") && !strings.Contains(out, "53,000") {
		t.Fatalf("missing used total: %q", out)
	}
	if !strings.Contains(out, "%") {
		t.Fatalf("expected a percentage: %q", out)
	}
}

func TestReportZeroWindowSafe(t *testing.T) {
	out := Format(Report{ModelName: "x", WindowTokens: 0, PromptTokens: 10})
	if out == "" || strings.Contains(out, "NaN") || strings.Contains(out, "+Inf") {
		t.Fatalf("zero window must not divide by zero: %q", out)
	}
}
