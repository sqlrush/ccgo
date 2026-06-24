package main

import (
	"strings"
	"testing"
)

// TestCreateTmuxSessionCalledFromProdPath_WhenTmuxFlagSet verifies the production
// call site: when options.Tmux is true and a worktree path is available,
// createTmuxSessionIfRequested builds the correct argv and invokes the runner.
// CLI-FLAG-35: prod call site test with injected runner (no real tmux binary needed).
func TestCreateTmuxSessionCalledFromProdPath_WhenTmuxFlagSet(t *testing.T) {
	var capturedArgs []string
	stubRunner := func(name string, args ...string) error {
		capturedArgs = append([]string{name}, args...)
		return nil
	}

	opts := cliOptions{Tmux: true}
	worktreePath := "/workspace/.ccgo-worktrees/feat-x"

	if err := createTmuxSessionIfRequested(opts, "feat-x", worktreePath, stubRunner); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// The runner must be called with tmux as the command.
	if len(capturedArgs) == 0 {
		t.Fatal("runner was not called")
	}
	if capturedArgs[0] != "tmux" {
		t.Errorf("expected command 'tmux', got %q", capturedArgs[0])
	}
	joined := strings.Join(capturedArgs, " ")
	if !strings.Contains(joined, "new-session") {
		t.Errorf("expected 'new-session' in argv: %v", capturedArgs)
	}
	if !strings.Contains(joined, "feat-x") {
		t.Errorf("expected session name 'feat-x' in argv: %v", capturedArgs)
	}
	if !strings.Contains(joined, worktreePath) {
		t.Errorf("expected worktree path in argv: %v", capturedArgs)
	}
}

// TestCreateTmuxSessionCalledFromProdPath_WhenTmuxFlagOff verifies the prod path is a no-op.
func TestCreateTmuxSessionCalledFromProdPath_WhenTmuxFlagOff(t *testing.T) {
	called := false
	stubRunner := func(name string, args ...string) error {
		called = true
		return nil
	}

	opts := cliOptions{Tmux: false}
	if err := createTmuxSessionIfRequested(opts, "sess", "/tmp", stubRunner); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if called {
		t.Error("runner must not be called when Tmux=false")
	}
}

// TestCreateTmuxSessionCalledFromProdPath_DegracesMissingBinary verifies that when
// the runner returns a "tmux binary not found" style error, the error propagates.
// CLI-FLAG-35: graceful degrade when tmux absent.
func TestCreateTmuxSessionCalledFromProdPath_DegracesMissingBinary(t *testing.T) {
	stubRunner := func(name string, args ...string) error {
		return &tmuxNotFoundError{}
	}

	opts := cliOptions{Tmux: true}
	err := createTmuxSessionIfRequested(opts, "sess", "/tmp", stubRunner)
	if err == nil {
		t.Fatal("expected error when tmux absent, got nil")
	}
}
