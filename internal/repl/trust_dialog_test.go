package repl

import (
	"strings"
	"testing"

	"ccgo/internal/tui"
)

func TestTrustDialogListsSources(t *testing.T) {
	d := NewTrustDialog(TrustInfo{FolderPath: "/proj", HasBashRules: true, HasMCPServers: true})
	out := strings.Join(d.Render(80, 24), "\n")
	if !strings.Contains(out, "/proj") {
		t.Fatalf("trust dialog should show folder path: %q", out)
	}
	if !strings.Contains(strings.ToLower(out), "bash") || !strings.Contains(strings.ToLower(out), "mcp") {
		t.Fatalf("trust dialog should list detected sources: %q", out)
	}
}

func TestTrustDialogYes(t *testing.T) {
	d := NewTrustDialog(TrustInfo{FolderPath: "/proj"})
	res, _ := d.ApplyKey(tui.Key{Type: tui.KeyEnter})
	if res.Submit != "trust:yes" {
		t.Fatalf("default Enter should trust, got %q", res.Submit)
	}
}

func TestTrustDialogEscDeclines(t *testing.T) {
	d := NewTrustDialog(TrustInfo{FolderPath: "/proj"})
	res, _ := d.ApplyKey(tui.Key{Type: tui.KeyEsc})
	if res.Submit != "trust:no" {
		t.Fatalf("Esc should decline, got %q (dismissed=%v)", res.Submit, res.Dismissed)
	}
}

// TestTrustDialogShowsMCPServerNames verifies that when MCPServerNames are set,
// the render output contains the specific server names (OVL-18).
func TestTrustDialogShowsMCPServerNames(t *testing.T) {
	d := NewTrustDialog(TrustInfo{
		FolderPath:     "/proj",
		HasMCPServers:  true,
		MCPServerNames: []string{"myserver", "otherserver"},
	})
	out := strings.Join(d.Render(80, 24), "\n")
	if !strings.Contains(out, "myserver") {
		t.Fatalf("trust dialog should show MCP server name 'myserver': %q", out)
	}
	if !strings.Contains(out, "otherserver") {
		t.Fatalf("trust dialog should show MCP server name 'otherserver': %q", out)
	}
}

// TestTrustDialogShowsHookSources verifies that when HookSources are set,
// the render output contains the source paths (OVL-18).
func TestTrustDialogShowsHookSources(t *testing.T) {
	d := NewTrustDialog(TrustInfo{
		FolderPath:  "/proj",
		HasHooks:    true,
		HookSources: []string{"/path/to/hook"},
	})
	out := strings.Join(d.Render(80, 24), "\n")
	if !strings.Contains(out, "hook") {
		t.Fatalf("trust dialog should show hook source path: %q", out)
	}
	if !strings.Contains(out, "/path/to/hook") {
		t.Fatalf("trust dialog should show full hook source path: %q", out)
	}
}

// TestTrustDialogFallbackToGenericMCPLabel verifies that when HasMCPServers is
// true but MCPServerNames is empty, the generic "MCP servers" label is shown.
func TestTrustDialogFallbackToGenericMCPLabel(t *testing.T) {
	d := NewTrustDialog(TrustInfo{
		FolderPath:    "/proj",
		HasMCPServers: true,
	})
	out := strings.Join(d.Render(80, 24), "\n")
	if !strings.Contains(strings.ToLower(out), "mcp servers") {
		t.Fatalf("trust dialog should show generic 'MCP servers' label: %q", out)
	}
}

// TestTrustDialogFallbackToGenericHooksLabel verifies that when HasHooks is true
// but HookSources is empty, the generic "Hooks" label is shown.
func TestTrustDialogFallbackToGenericHooksLabel(t *testing.T) {
	d := NewTrustDialog(TrustInfo{
		FolderPath: "/proj",
		HasHooks:   true,
	})
	out := strings.Join(d.Render(80, 24), "\n")
	if !strings.Contains(out, "Hooks") {
		t.Fatalf("trust dialog should show generic 'Hooks' label: %q", out)
	}
}
