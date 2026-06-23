package repl

import (
	"strings"
	"testing"
	"time"

	"ccgo/internal/tui"
)

// TestSetTerminalTitleCallsWriter verifies that startSpinner triggers
// titleWriter with a string containing "Claude".
func TestSetTerminalTitleCallsWriter(t *testing.T) {
	ft := NewFakeTerminal("", 80, 24)
	l := NewLoop(ft, nil)

	var written []string
	l.titleWriter = func(title string) {
		written = append(written, title)
	}

	// startSpinner sets status + fires setTerminalTitle("Claude — thinking…")
	l.startSpinner()

	if len(written) == 0 {
		t.Fatal("expected titleWriter to be called by startSpinner")
	}
	combined := strings.Join(written, "")
	if !strings.Contains(combined, "Claude") {
		t.Fatalf("expected 'Claude' in terminal title sequence, got %q", combined)
	}
}

// TestSetTerminalTitleEmptyOnFinish verifies that finishTurn calls titleWriter
// with the clear-title sequence.
func TestSetTerminalTitleEmptyOnFinish(t *testing.T) {
	ft := NewFakeTerminal("", 80, 24)
	l := NewLoop(ft, nil)

	var written []string
	l.titleWriter = func(title string) {
		written = append(written, title)
	}

	// Start a spinner to simulate an in-flight turn.
	l.startSpinner()
	written = nil // reset; we only care about finishTurn

	// Finish with a successful empty turn.
	l.finishTurn(turnOutcome{})

	if len(written) == 0 {
		t.Fatal("expected titleWriter to be called by finishTurn")
	}
	// The clear sequence should match ClearTerminalTitleSequence().
	clearSeq := tui.ClearTerminalTitleSequence()
	combined := strings.Join(written, "")
	if combined != clearSeq {
		t.Fatalf("expected clear sequence %q, got %q", clearSeq, combined)
	}
}

// TestSetTerminalTitleDirectlyOnLoop verifies setTerminalTitle calls writer with
// the correct OSC sequence for a non-empty title.
func TestSetTerminalTitleDirectlyOnLoop(t *testing.T) {
	ft := NewFakeTerminal("", 80, 24)
	l := NewLoop(ft, nil)

	var written []string
	l.titleWriter = func(title string) {
		written = append(written, title)
	}

	l.setTerminalTitle("My Title")
	if len(written) == 0 {
		t.Fatal("expected titleWriter to be called")
	}
	expected := tui.TerminalTitleSequence("My Title")
	if written[0] != expected {
		t.Fatalf("expected %q, got %q", expected, written[0])
	}
}

// TestSpinnerThinkingIntegration verifies that applyStreamingDelta switches
// the spinner to thinking mode on a thinking_delta event.
func TestSpinnerThinkingIntegration(t *testing.T) {
	ft := NewFakeTerminal("", 80, 24)
	l := NewLoop(ft, nil)
	now := time.Now()
	l.spinner = NewSpinner(now)
	l.running = true

	// Simulate a thinking_delta event using existing helper.
	l.applyStreamingDelta(makeThinkingDeltaEvent("<thinking>hello</thinking>"))

	if !l.thinkingActive {
		t.Fatal("expected thinkingActive=true after thinking_delta")
	}
	if !strings.Contains(l.spinner.verb, "Thinking") {
		t.Fatalf("expected spinner verb to contain 'Thinking', got %q", l.spinner.verb)
	}

	// Simulate a text_delta — should exit thinking mode.
	l.applyStreamingDelta(makeTextDeltaEvent("hello text"))

	if l.thinkingActive {
		t.Fatal("expected thinkingActive=false after text_delta")
	}
	if !strings.Contains(l.spinner.verb, "Working") {
		t.Fatalf("expected spinner verb to contain 'Working' after text_delta, got %q", l.spinner.verb)
	}
}
