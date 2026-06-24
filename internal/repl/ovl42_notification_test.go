package repl

// G26: OVL-42 terminal notification tests.
// Verifies that:
//   - onTurnDoneNotify fires after finishTurn on a successful turn.
//   - onTurnDoneNotify is called with unfocused=true when screen.Focused is false.
//   - onTurnDoneNotify is called with unfocused=false when screen.Focused is true.
//   - onTurnDoneNotify does NOT fire on error turns.
//   - TerminalNotificationSequences returns non-empty OSC sequences.
//
// CC ref: src/services/notifier.ts — OSC 9/99/777 terminal notifications.

import (
	"errors"
	"strings"
	"testing"

	"ccgo/internal/conversation"
	"ccgo/internal/contracts"
	"ccgo/internal/tui"
)

func TestTerminalNotificationSequencesNonEmpty(t *testing.T) {
	seqs := tui.TerminalNotificationSequences("Claude", "Turn complete")
	if len(seqs) == 0 {
		t.Fatal("TerminalNotificationSequences should return at least one sequence")
	}
	for _, s := range seqs {
		if s == "" {
			t.Fatal("TerminalNotificationSequences must not return empty sequences")
		}
	}
}

func TestTerminalNotificationSequencesContainOSC(t *testing.T) {
	seqs := tui.TerminalNotificationSequences("Claude", "Turn complete")
	// At least one sequence should contain an OSC prefix (\x1b]).
	found := false
	for _, s := range seqs {
		if strings.Contains(s, "\x1b]") {
			found = true
			break
		}
	}
	if !found {
		t.Fatal("TerminalNotificationSequences should contain an OSC sequence \\x1b]")
	}
}

func TestOnTurnDoneNotifyFiresOnSuccess(t *testing.T) {
	ft := NewFakeTerminal("", 80, 24)
	l := NewLoop(ft, nil)
	l.screen.Focused = false // simulate unfocused terminal

	var notifyCalled bool
	var notifyUnfocused bool
	l.onTurnDoneNotify = func(unfocused bool) {
		notifyCalled = true
		notifyUnfocused = unfocused
	}

	l.finishTurn(turnOutcome{
		result: conversation.Result{Messages: []contracts.Message{}},
	})

	if !notifyCalled {
		t.Fatal("onTurnDoneNotify should be called on successful finishTurn")
	}
	if !notifyUnfocused {
		t.Fatal("onTurnDoneNotify unfocused=true when screen.Focused is false")
	}
}

func TestOnTurnDoneNotifyFocusedState(t *testing.T) {
	ft := NewFakeTerminal("", 80, 24)
	l := NewLoop(ft, nil)
	l.screen.Focused = true // terminal is focused

	var notifyUnfocused bool
	var notifyCalled bool
	l.onTurnDoneNotify = func(unfocused bool) {
		notifyCalled = true
		notifyUnfocused = unfocused
	}

	l.finishTurn(turnOutcome{
		result: conversation.Result{Messages: []contracts.Message{}},
	})

	if !notifyCalled {
		t.Fatal("onTurnDoneNotify should be called even when focused")
	}
	if notifyUnfocused {
		t.Fatal("onTurnDoneNotify unfocused=false when screen.Focused is true")
	}
}

func TestOnTurnDoneNotifyNotFiredOnError(t *testing.T) {
	ft := NewFakeTerminal("", 80, 24)
	l := NewLoop(ft, nil)

	var notifyCalled bool
	l.onTurnDoneNotify = func(unfocused bool) {
		notifyCalled = true
	}

	// Error turn — should NOT fire notify.
	l.finishTurn(turnOutcome{err: errors.New("turn failed")})

	if notifyCalled {
		t.Fatal("onTurnDoneNotify must not be called on error turn")
	}
}
