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

// TestMCPStatusPanelEmpty verifies the empty state message (MCP-53, F8-C03).
func TestMCPStatusPanelEmpty(t *testing.T) {
	out := mcpStatusPanel(nil)
	if !strings.Contains(out, "No MCP servers") {
		t.Fatalf("empty panel = %q", out)
	}
}

// TestMCPStatusPanelShowsEntries verifies that server entries are shown with
// name, transport, target, and status (MCP-53, F8-C03).
func TestMCPStatusPanelShowsEntries(t *testing.T) {
	entries := []MCPServerEntry{
		{Name: "git-mcp", Transport: "stdio", Target: "/usr/bin/git-mcp", Status: "connected"},
		{Name: "remote-x", Transport: "http", Target: "https://x.example/mcp", Status: "error", Error: "connection refused"},
	}
	out := mcpStatusPanel(entries)
	if !strings.Contains(out, "git-mcp") {
		t.Fatalf("panel missing git-mcp: %q", out)
	}
	if !strings.Contains(out, "remote-x") {
		t.Fatalf("panel missing remote-x: %q", out)
	}
	if !strings.Contains(out, "connected") {
		t.Fatalf("panel missing 'connected' status: %q", out)
	}
	if !strings.Contains(out, "connection refused") {
		t.Fatalf("panel missing error detail: %q", out)
	}
}
