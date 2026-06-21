package repl

import (
	"context"
	"strings"
	"testing"
	"time"
)

func runLoop(t *testing.T, l *Loop) error {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	return l.Run(ctx)
}

func TestLoopSubmitThenExit(t *testing.T) {
	// Type "hi", press Enter (submit), then Ctrl-D twice (exit).
	ft := NewFakeTerminal("hi\r\x04\x04", 80, 24)
	l := NewLoop(ft, nil)

	var submitted []string
	l.StartTurn = func(input string) { submitted = append(submitted, input) }

	if err := runLoop(t, l); err != nil {
		t.Fatalf("Run err: %v", err)
	}
	if len(submitted) != 1 || submitted[0] != "hi" {
		t.Fatalf("submitted = %v want [hi]", submitted)
	}
	if ft.Raw {
		t.Fatal("terminal raw mode not restored on exit")
	}
	// Lifecycle should have left the alternate screen on exit.
	if !strings.Contains(ft.Out.String(), ExitAlternateMarker) {
		t.Fatal("expected alternate-screen exit sequence in output")
	}
}

func TestLoopNonTTYFallback(t *testing.T) {
	ft := NewFakeTerminal("hello\n", 80, 24)
	ft.TTY = false
	l := NewLoop(ft, nil)
	var submitted []string
	l.StartTurn = func(input string) { submitted = append(submitted, input) }
	if err := runLoop(t, l); err != nil {
		t.Fatalf("Run err: %v", err)
	}
	if len(submitted) != 1 || submitted[0] != "hello" {
		t.Fatalf("submitted = %v want [hello]", submitted)
	}
	if ft.Raw {
		t.Fatal("non-tty path must not enter raw mode")
	}
}
