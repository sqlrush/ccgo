package repl

// REPL-24: BriefIdleStatus — spinner hides when streaming text is visible.
// CC ref: src/screens/REPL.tsx:1683.

import (
	"testing"
	"time"
)

// TestTickSuppressedWhileStreaming verifies that tick() does NOT update
// screen.Status when streamingActive is true, even though running is also true.
func TestTickSuppressedWhileStreaming(t *testing.T) {
	ft := NewFakeTerminal("", 80, 24)
	l := NewLoop(ft, nil)

	// Set a known status before the tick.
	const sentinel = "sentinel-status"
	l.screen.Status = sentinel

	// Simulate a running turn with active streaming.
	l.running = true
	l.streamingActive = true
	// Seed the spinner so it would produce a non-empty line if called.
	l.spinner = NewSpinner(time.Now())

	l.tick()

	// Status must be unchanged because streaming suppresses the spinner.
	if l.screen.Status != sentinel {
		t.Fatalf("tick() updated Status to %q while streaming; want sentinel %q", l.screen.Status, sentinel)
	}
}

// TestTickUpdatesStatusWhenNotStreaming verifies that tick() DOES update
// screen.Status when running is true and streamingActive is false.
func TestTickUpdatesStatusWhenNotStreaming(t *testing.T) {
	ft := NewFakeTerminal("", 80, 24)
	l := NewLoop(ft, nil)

	const sentinel = "sentinel-status"
	l.screen.Status = sentinel

	l.running = true
	l.streamingActive = false
	l.spinner = NewSpinner(time.Now())

	l.tick()

	// The spinner must have overwritten the sentinel.
	if l.screen.Status == sentinel {
		t.Fatalf("tick() did not update Status when not streaming; Status still %q", l.screen.Status)
	}
}
