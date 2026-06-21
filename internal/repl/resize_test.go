package repl

import (
	"context"
	"testing"
	"time"
)

func TestApplyResizeUpdatesScreen(t *testing.T) {
	ft := NewFakeTerminal("", 80, 24)
	l := NewLoop(ft, nil)
	if l.width != 80 || l.height != 24 {
		t.Fatalf("initial size = %dx%d want 80x24", l.width, l.height)
	}
	l.applyResize(resizeEvent{Width: 120, Height: 40})
	if l.width != 120 || l.height != 40 {
		t.Fatalf("after resize = %dx%d want 120x40", l.width, l.height)
	}
	if l.screen.Width != 120 || l.screen.Height != 40 {
		t.Fatalf("screen size = %dx%d want 120x40", l.screen.Width, l.screen.Height)
	}
}

func TestApplyResizeIgnoresNonPositive(t *testing.T) {
	ft := NewFakeTerminal("", 80, 24)
	l := NewLoop(ft, nil)
	l.applyResize(resizeEvent{Width: 0, Height: -5})
	if l.width != 80 || l.height != 24 {
		t.Fatalf("non-positive resize must be ignored, got %dx%d", l.width, l.height)
	}
}

func TestStartResizeListenerNoOpForNonTTY(t *testing.T) {
	ft := NewFakeTerminal("", 80, 24)
	ft.TTY = false
	ctx := context.Background()
	out := make(chan resizeEvent, 1)

	startResizeListener(ctx, ft, out)

	select {
	case <-out:
		t.Fatal("expected no event on channel for non-tty")
	case <-time.After(50 * time.Millisecond):
		// Correct: no-op confirmed
	}
}
