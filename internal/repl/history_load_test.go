package repl

import (
	"testing"

	"ccgo/internal/session"
	"ccgo/internal/tui"
)

// TestNewLoopFromHistoryEntriesSeesHistory verifies that Up-arrow in a loop
// created with history entries surfaces those entries (not an empty history).
func TestNewLoopFromHistoryEntriesSeesHistory(t *testing.T) {
	entries := []session.HistoryEntry{
		{Display: "first command"},
		{Display: "second command"},
	}
	ft := NewFakeTerminal("", 80, 24)
	l := NewLoopFromHistoryEntries(ft, entries)

	screen := &l.screen
	upKey := tui.Key{Type: tui.KeyUp}
	_ = screen.ApplyKey(upKey)
	// After one Up, the prompt should show the last entry ("second command").
	if screen.Prompt.Text != "second command" {
		t.Fatalf("after Up, prompt should be 'second command', got %q", screen.Prompt.Text)
	}
	_ = screen.ApplyKey(upKey)
	// After two Ups, the prompt should show "first command".
	if screen.Prompt.Text != "first command" {
		t.Fatalf("after two Ups, prompt should be 'first command', got %q", screen.Prompt.Text)
	}
}

// TestInteractiveOptionsPromptHistorySeedsLoop verifies that PromptHistory in
// InteractiveOptions is passed through to the loop's screen prompt history.
func TestInteractiveOptionsPromptHistorySeedsLoop(t *testing.T) {
	entries := []session.HistoryEntry{
		{Display: "stored prompt"},
	}
	opts := InteractiveOptions{
		PromptHistory: entries,
	}
	ft := NewFakeTerminal("", 80, 24)
	l := NewLoopFromHistoryEntries(ft, opts.PromptHistory)
	screen := &l.screen
	upKey := tui.Key{Type: tui.KeyUp}
	_ = screen.ApplyKey(upKey)
	if screen.Prompt.Text != "stored prompt" {
		t.Fatalf("PromptHistory not seeded into loop: got %q", screen.Prompt.Text)
	}
}
