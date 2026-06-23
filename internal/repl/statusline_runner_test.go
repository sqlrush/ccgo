package repl

import (
	"strings"
	"testing"
)

// TestStatusLineRunnerWithCommand verifies that RunStatusLineCommand executes the
// given shell command and returns its trimmed stdout.
// CC ref: src/bridge/bridgeUI.ts renderStatusLine — executes statusLine.command.
func TestStatusLineRunnerWithCommand(t *testing.T) {
	out, err := RunStatusLineCommand("echo hello-status")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !strings.Contains(out, "hello-status") {
		t.Errorf("expected output to contain 'hello-status'; got %q", out)
	}
}

// TestStatusLineRunnerEmptyCommand returns empty string without error for blank command.
func TestStatusLineRunnerEmptyCommand(t *testing.T) {
	out, err := RunStatusLineCommand("")
	if err != nil {
		t.Fatalf("unexpected error on empty command: %v", err)
	}
	if out != "" {
		t.Errorf("expected empty output for empty command; got %q", out)
	}
}
