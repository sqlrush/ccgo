package repl

import (
	"strings"
	"testing"
	"time"

	"ccgo/internal/contracts"
)

func TestCostPanel(t *testing.T) {
	s := SessionStats{Model: "claude-x", CostUSD: 0.1234, APIDuration: 2 * time.Second}
	out := costPanel(s)
	if !strings.Contains(out, "$0.12") {
		t.Fatalf("cost panel %q should show cost", out)
	}
}

func TestContextPanelPercent(t *testing.T) {
	s := SessionStats{ContextUsed: 50000, ContextMax: 200000}
	out := contextPanel(s)
	if !strings.Contains(out, "25%") {
		t.Fatalf("context panel %q should show 25%%", out)
	}
}

func TestContextPanelZeroMaxSafe(t *testing.T) {
	out := contextPanel(SessionStats{ContextUsed: 10, ContextMax: 0})
	if out == "" {
		t.Fatal("context panel should still render with unknown max")
	}
}

func TestStatusPanelIncludesMode(t *testing.T) {
	out := statusPanel(SessionStats{Model: "m"}, contracts.PermissionPlan)
	if !strings.Contains(strings.ToLower(out), "plan") {
		t.Fatalf("status panel %q should include mode", out)
	}
}

func TestDoctorReport(t *testing.T) {
	out := doctorReport([]DoctorCheck{
		{Name: "Go toolchain", Status: "ok", Detail: "go1.26"},
		{Name: "Settings", Status: "warn", Detail: "invalid key"},
	})
	if !strings.Contains(out, "Go toolchain") || !strings.Contains(out, "Settings") {
		t.Fatalf("doctor report missing checks: %q", out)
	}
}
