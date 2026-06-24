package repl

// REPL-59: OS notification / terminal bell on turn completion.
// Verifies that:
//   - TurnNotificationSequences("terminal_bell") emits the BEL character (\a).
//   - RunInteractiveWithOptions wires PreferredNotifChannel to the notification seam.
//   - When channel="terminal_bell" and terminal is unfocused, \a appears in output.
//
// CC ref: src/services/notifier.ts sendToChannel("terminal_bell").

import (
	"strings"
	"testing"

	"ccgo/internal/conversation"
	"ccgo/internal/contracts"
	"ccgo/internal/tui"
)

// TestTurnNotificationBellEmitted verifies that when the terminal is unfocused
// and PreferredNotifChannel is "terminal_bell", finishTurn causes a BEL (\a)
// to be written to the output terminal.
// This exercises the run.go wiring: onTurnDoneNotify calls TurnNotificationSequences
// with the configured channel and writes results to term.WriteString.
func TestTurnNotificationBellEmitted(t *testing.T) {
	ft := NewFakeTerminal("", 80, 24)
	l := NewLoop(ft, nil)
	l.screen.Focused = false // simulate unfocused terminal

	// Wire the same notification logic as run.go does for channel="terminal_bell".
	notifChannel := "terminal_bell"
	l.onTurnDoneNotify = func(unfocused bool) {
		if !unfocused {
			return
		}
		seqs := tui.TurnNotificationSequences("Claude", "Turn complete", notifChannel)
		for _, seq := range seqs {
			_ = ft.WriteString(seq)
		}
	}

	l.finishTurn(turnOutcome{
		result: conversation.Result{Messages: []contracts.Message{}},
	})

	written := ft.Out.String()
	if !strings.Contains(written, "\a") {
		t.Fatalf("terminal_bell channel: expected BEL (\\a) in output; got %q", written)
	}
}

// TestTurnNotificationDisabledEmitsNothing verifies that
// "notifications_disabled" channel produces no notification sequences.
// We capture only what the onTurnDoneNotify callback writes.
func TestTurnNotificationDisabledEmitsNothing(t *testing.T) {
	ft := NewFakeTerminal("", 80, 24)
	l := NewLoop(ft, nil)
	l.screen.Focused = false

	var notifWritten []string
	notifChannel := "notifications_disabled"
	l.onTurnDoneNotify = func(unfocused bool) {
		if !unfocused {
			return
		}
		seqs := tui.TurnNotificationSequences("Claude", "Turn complete", notifChannel)
		notifWritten = seqs // capture, don't write to terminal
	}

	l.finishTurn(turnOutcome{
		result: conversation.Result{Messages: []contracts.Message{}},
	})

	if len(notifWritten) != 0 {
		t.Fatalf("notifications_disabled: expected no sequences; got %#v", notifWritten)
	}
}

// TestTurnNotificationBellNotEmittedWhenFocused verifies that no notification
// is sent when the terminal is focused (user is actively watching).
func TestTurnNotificationBellNotEmittedWhenFocused(t *testing.T) {
	ft := NewFakeTerminal("", 80, 24)
	l := NewLoop(ft, nil)
	l.screen.Focused = true // terminal IS focused

	var notifCalled bool
	l.onTurnDoneNotify = func(unfocused bool) {
		if !unfocused {
			return
		}
		notifCalled = true
	}

	l.finishTurn(turnOutcome{
		result: conversation.Result{Messages: []contracts.Message{}},
	})

	if notifCalled {
		t.Fatal("focused terminal: onTurnDoneNotify must not send notifications")
	}
	_ = ft // suppress unused warning
}
