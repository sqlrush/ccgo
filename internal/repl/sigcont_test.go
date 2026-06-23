//go:build !windows

package repl

// REPL-56: SIGCONT signal handling.
// After Ctrl+Z (SIGTSTP) suspends the process and `fg` resumes it, the REPL
// must redraw by posting a resizeEvent. This tests that startSIGCONTListener
// posts a resize event on SIGCONT.
// CC ref: src/ink/ink.tsx:960 (SIGCONT → force repaint).

import (
	"os"
	"syscall"
	"testing"
	"time"

	"context"
)

func TestSIGCONTListenerPostsResizeEvent(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()

	out := make(chan resizeEvent, 1)
	term := NewFakeTerminal("", 80, 24)
	startSIGCONTListener(ctx, term, out)

	// Send SIGCONT to ourselves.
	if err := syscall.Kill(os.Getpid(), syscall.SIGCONT); err != nil {
		t.Fatalf("kill SIGCONT: %v", err)
	}

	select {
	case ev := <-out:
		if ev.Width <= 0 || ev.Height <= 0 {
			t.Errorf("expected positive dimensions, got %+v", ev)
		}
	case <-ctx.Done():
		t.Fatal("timeout waiting for resize event after SIGCONT")
	}
}

func TestSIGCONTListenerStopsOnContextCancel(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	out := make(chan resizeEvent, 1)
	term := NewFakeTerminal("", 80, 24)
	startSIGCONTListener(ctx, term, out)
	cancel()
	// No panic or goroutine leak after cancel.
}

func TestSIGCONTListenerNopForNonTTY(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	out := make(chan resizeEvent, 1)
	term := &FakeTerminal{W: 80, H: 24, TTY: false}
	// Must not panic when terminal is not a TTY.
	startSIGCONTListener(ctx, term, out)
	// nothing should be posted
	select {
	case ev := <-out:
		t.Errorf("unexpected resize event for non-TTY: %+v", ev)
	default:
	}
}
