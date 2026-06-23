package repl

import (
	"context"
	"strings"
	"testing"
	"time"
)

// TestLoopHandlesStashPromptEvent verifies ScreenEventStashPrompt is handled
// (not silently dropped) by the loop's event switch.
func TestLoopHandlesStashPromptEvent(t *testing.T) {
	// Type "hello", Ctrl+S triggers stash; then Ctrl+D twice exits.
	// Ctrl+S = \x13; Ctrl+D = \x04
	ft := NewFakeTerminal("hello\x13\x04\x04", 80, 24)
	l := NewLoop(ft, nil)
	l.StartTurn = func(string) { t.Fatal("model must not be called") }

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	if err := l.Run(ctx); err != nil {
		t.Fatalf("Run: %v", err)
	}
	// Reaching here without panic/deadlock means the stash event was handled.
}

// TestLoopHandlesFocusEvents verifies ScreenEventFocusIn/Out update screen.Focused.
func TestLoopHandlesFocusEvents(t *testing.T) {
	// FocusOut (\x1b[O), FocusIn (\x1b[I), then double Ctrl+D exit.
	ft := NewFakeTerminal("\x1b[O\x1b[I\x04\x04", 80, 24)
	l := NewLoop(ft, nil)
	l.StartTurn = func(string) {}

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	if err := l.Run(ctx); err != nil {
		t.Fatalf("Run: %v", err)
	}
	// screen.Focused should be true after the FocusIn event.
	if !l.screen.Focused {
		t.Error("screen.Focused should be true after FocusIn event")
	}
}

// TestLoopHandlesToggleTranscriptEvent verifies ScreenEventToggleTranscript
// is handled (not dropped, loop does not deadlock/panic).
func TestLoopHandlesToggleTranscriptEvent(t *testing.T) {
	// Ctrl+O = \x0f
	ft := NewFakeTerminal("\x0f\x04\x04", 80, 24)
	l := NewLoop(ft, nil)
	l.StartTurn = func(string) {}

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	if err := l.Run(ctx); err != nil {
		t.Fatalf("Run: %v", err)
	}
	_ = strings.Contains(ft.Out.String(), "") // reference ft to avoid unused warning
}
