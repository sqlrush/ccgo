package repl

import (
	"strings"
	"testing"

	"ccgo/internal/tui"
)

func TestMCPApprovalDialogDefaultIsYesAll(t *testing.T) {
	d := NewMCPServerApprovalDialog("my-server")
	// cursor 0 = yes_all
	res, handled := d.ApplyKey(tui.Key{Type: tui.KeyEnter})
	if !handled {
		t.Fatal("Enter should be handled")
	}
	if !strings.HasPrefix(res.Submit, "mcp:yes_all:") {
		t.Fatalf("default Enter = %q want mcp:yes_all:...", res.Submit)
	}
	if !strings.HasSuffix(res.Submit, "my-server") {
		t.Fatalf("submit missing server name: %q", res.Submit)
	}
}

func TestMCPApprovalDialogSelectYes(t *testing.T) {
	d := NewMCPServerApprovalDialog("srv")
	d.ApplyKey(tui.Key{Type: tui.KeyDown}) // cursor → 1 = yes
	res, _ := d.ApplyKey(tui.Key{Type: tui.KeyEnter})
	if !strings.HasPrefix(res.Submit, "mcp:yes:") {
		t.Fatalf("yes select = %q want mcp:yes:...", res.Submit)
	}
}

func TestMCPApprovalDialogSelectNo(t *testing.T) {
	d := NewMCPServerApprovalDialog("srv")
	d.ApplyKey(tui.Key{Type: tui.KeyDown}) // cursor 0→1
	d.ApplyKey(tui.Key{Type: tui.KeyDown}) // cursor 1→2 = no
	res, _ := d.ApplyKey(tui.Key{Type: tui.KeyEnter})
	if !strings.HasPrefix(res.Submit, "mcp:no:") {
		t.Fatalf("no select = %q want mcp:no:...", res.Submit)
	}
}

func TestMCPApprovalDialogEscDeclines(t *testing.T) {
	d := NewMCPServerApprovalDialog("srv")
	res, handled := d.ApplyKey(tui.Key{Type: tui.KeyEsc})
	if !handled {
		t.Fatal("Esc should be handled")
	}
	if !strings.HasPrefix(res.Submit, "mcp:no:") {
		t.Fatalf("Esc = %q want mcp:no:...", res.Submit)
	}
}

func TestMCPApprovalDialogRenderShowsServerName(t *testing.T) {
	d := NewMCPServerApprovalDialog("my-server")
	out := strings.Join(d.Render(80, 24), "\n")
	if !strings.Contains(out, "my-server") {
		t.Fatalf("Render missing server name: %q", out)
	}
}

func TestMCPApprovalDialogDownAtBottomNoOp(t *testing.T) {
	d := NewMCPServerApprovalDialog("s")
	d.cursor = len(mcpOptions) - 1
	_, _ = d.ApplyKey(tui.Key{Type: tui.KeyDown})
	if d.cursor != len(mcpOptions)-1 {
		t.Fatalf("cursor past bottom = %d want %d", d.cursor, len(mcpOptions)-1)
	}
}
