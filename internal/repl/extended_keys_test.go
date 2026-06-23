package repl

import (
	"strings"
	"testing"

	"ccgo/internal/tui"
)

// TestSetExtendedKeysEnablesKittyProtocol verifies that when ExtendedKeys is
// set on a Loop, the Kitty keyboard protocol sequence (tui.EnableKittyKeyboard)
// is written to the terminal during EnterInteractive.
// REPL-60. CC ref: src/ink/ink.tsx:1430.
func TestSetExtendedKeysEnablesKittyProtocol(t *testing.T) {
	// Input: just Ctrl-D twice to exit immediately so we can inspect output.
	ft := NewFakeTerminal("\x04\x04", 80, 24)
	l := NewLoop(ft, nil)
	l.SetExtendedKeys(true)

	if err := runLoop(t, l); err != nil {
		t.Fatalf("Run: %v", err)
	}

	out := ft.Out.String()
	if !strings.Contains(out, tui.EnableKittyKeyboard) {
		t.Fatalf("expected Kitty keyboard sequence %q in output, got: %q", tui.EnableKittyKeyboard, out)
	}
}

// TestSetExtendedKeysOffDoesNotSendKittySequence verifies that when
// ExtendedKeys is not set, the Kitty keyboard sequence is NOT written.
func TestSetExtendedKeysOffDoesNotSendKittySequence(t *testing.T) {
	ft := NewFakeTerminal("\x04\x04", 80, 24)
	l := NewLoop(ft, nil)
	// extendedKeys defaults to false

	if err := runLoop(t, l); err != nil {
		t.Fatalf("Run: %v", err)
	}

	out := ft.Out.String()
	if strings.Contains(out, tui.EnableKittyKeyboard) {
		t.Fatalf("unexpected Kitty keyboard sequence in output when ExtendedKeys=false")
	}
}
