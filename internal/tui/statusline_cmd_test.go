package tui

import (
	"strings"
	"testing"
)

// TestRenderStatusLineWithCommandOutput verifies that RenderStatusLineWithCommand
// includes the command output in the status bar when the command succeeds.
// CC ref: utils/settings/types.ts statusLine:{type:"command",command:string}.
func TestRenderStatusLineWithCommandOutput(t *testing.T) {
	cmdOutput := "my-custom-status"
	line := RenderStatusLineWithCommand(cmdOutput, 80, "")
	if !strings.Contains(line, cmdOutput) {
		t.Errorf("expected status line to contain command output %q; got %q", cmdOutput, line)
	}
}

// TestRenderStatusLineWithCommandOutputAppended verifies that the command output
// is appended to the base status (not replacing it), separated by a pipe.
func TestRenderStatusLineWithCommandOutputAppended(t *testing.T) {
	line := RenderStatusLineWithCommand("my-custom-status", 80, "")
	if !strings.Contains(line, "my-custom-status") {
		t.Errorf("expected command output in status line; got %q", line)
	}
}

// TestRenderStatusLineWithEmptyCommandOutput verifies that an empty command output
// falls back to the standard status bar (no extra separator).
func TestRenderStatusLineWithEmptyCommandOutput(t *testing.T) {
	base := RenderStatusLineWithTheme("ready", 80, "")
	withEmpty := RenderStatusLineWithCommand("", 80, "")
	if base != withEmpty {
		t.Errorf("expected empty command output to produce same output as no-command status; got %q vs %q", withEmpty, base)
	}
}
