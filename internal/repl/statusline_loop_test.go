package repl

import (
	"strings"
	"testing"
)

// TestStatusLineCommandWiredToLoop verifies that when a statusLine command is
// wired, refreshBaseStatus() uses its output as the screen status.
// CC ref: utils/settings/types.ts statusLine:{type:"command",command:string}.
func TestStatusLineCommandWiredToLoop(t *testing.T) {
	term := NewFakeTerminal("", 80, 24)
	l := NewLoop(term, nil)

	// Wire a fake runner that returns a fixed status string.
	l.statusLineCmdRunner = func(_ string) (string, error) {
		return "custom-status-output", nil
	}
	l.SetStatusLineCommand("echo custom-status-output")

	l.refreshBaseStatus()

	if !strings.Contains(l.screen.Status, "custom-status-output") {
		t.Errorf("expected screen status to contain 'custom-status-output'; got %q", l.screen.Status)
	}
}

// TestStatusLineCommandEmptyFallsBack verifies that when no command is set, the
// status line falls back to the mode indicator (standard behaviour).
// The default mode (PermissionDefault + no vim) produces an empty indicator,
// which is then rendered as "ready" by RenderStatusLine. We just verify that
// the statusLineCmd field is not consulted.
func TestStatusLineCommandEmptyFallsBack(t *testing.T) {
	term := NewFakeTerminal("", 80, 24)
	l := NewLoop(term, nil)
	// Ensure no command runner is registered.
	l.statusLineCmdRunner = func(_ string) (string, error) {
		t.Error("runner should not be called when statusLineCmd is empty")
		return "", nil
	}
	l.refreshBaseStatus()
	// baseStatus should be the mode indicator string (empty for default mode).
	// No panic is the success criterion; the runner should not have been called.
}
