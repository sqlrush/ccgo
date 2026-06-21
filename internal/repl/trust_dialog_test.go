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
