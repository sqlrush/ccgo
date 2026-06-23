package repl

import (
	"testing"
)

// TestRunInteractiveWithOptionsVimEnabled verifies the seam that
// RunInteractiveWithOptions uses when EditorMode is "vim": SetVimEnabled(true)
// flips the screen's VimEnabled flag.
func TestRunInteractiveWithOptionsVimEnabled(t *testing.T) {
	ft := NewFakeTerminal("", 80, 24)
	l := NewLoop(ft, nil)
	// Simulate what RunInteractiveWithOptions does with EditorMode="vim":
	l.screen.SetVimEnabled(true)
	if !l.screen.VimEnabled {
		t.Fatal("SetVimEnabled(true) must set VimEnabled=true on the screen")
	}
}
