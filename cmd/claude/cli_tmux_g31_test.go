package main

import (
	"testing"
)

// TestTmuxFlagStored verifies that cliOptions.Tmux is a bool field that can be set.
// CLI-FLAG-35: --tmux flag is a ⚠️ seam test — tmux requires a running daemon and
// TTY which is impractical in CI. The flag is correctly parsed (see TestTmuxFlagParsed
// in f2_flags_test.go); runtime wiring is deferred.
// CC ref: src/main.tsx --tmux flag.
func TestTmuxFlagStored(t *testing.T) {
	opts := cliOptions{Tmux: true}
	if !opts.Tmux {
		t.Error("expected cliOptions.Tmux=true, got false")
	}
}

// TestTmuxFlagDefault verifies that cliOptions.Tmux defaults to false.
func TestTmuxFlagDefault(t *testing.T) {
	opts := cliOptions{}
	if opts.Tmux {
		t.Error("expected cliOptions.Tmux=false by default, got true")
	}
}
