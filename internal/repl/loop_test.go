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

// TestLoopBashPrefixTTY verifies that "!cmd" is rewritten to
// "Run the following bash command: cmd" before StartTurn is called (REPL-49).
func TestLoopBashPrefixTTY(t *testing.T) {
	// Submit "!ls -la" then exit via Ctrl-D.
	ft := NewFakeTerminal("!ls -la\r\x04\x04", 80, 24)
	l := NewLoop(ft, nil)
	var submitted []string
	l.StartTurn = func(input string) { submitted = append(submitted, input) }
	if err := runLoop(t, l); err != nil {
		t.Fatalf("Run err: %v", err)
	}
	if len(submitted) != 1 {
		t.Fatalf("submitted = %v, want 1 entry", submitted)
	}
	want := "Run the following bash command: ls -la"
	if submitted[0] != want {
		t.Fatalf("submitted[0] = %q, want %q", submitted[0], want)
	}
}

// TestLoopBashPrefixNonTTY verifies "!" prefix rewriting in the non-TTY
// line-mode fallback path (REPL-49).
func TestLoopBashPrefixNonTTY(t *testing.T) {
	ft := NewFakeTerminal("!echo hello\n", 80, 24)
	ft.TTY = false
	l := NewLoop(ft, nil)
	var submitted []string
	l.StartTurn = func(input string) { submitted = append(submitted, input) }
	if err := runLoop(t, l); err != nil {
		t.Fatalf("Run err: %v", err)
	}
	want := "Run the following bash command: echo hello"
	if len(submitted) != 1 || submitted[0] != want {
		t.Fatalf("submitted = %v, want [%q]", submitted, want)
	}
}

// TestLoopBashPrefixEmptyCommandPassedThrough verifies that "!" with only
// spaces after it is forwarded to StartTurn unchanged (no rewrite), since
// there is no command to inject (REPL-49).
func TestLoopBashPrefixEmptyCommandPassedThrough(t *testing.T) {
	// "!   " — stripping "!" leaves blank; we skip the rewrite and pass input as-is.
	ft := NewFakeTerminal("!   \r\x04\x04", 80, 24)
	l := NewLoop(ft, nil)
	var submitted []string
	l.StartTurn = func(input string) { submitted = append(submitted, input) }
	if err := runLoop(t, l); err != nil {
		t.Fatalf("Run err: %v", err)
	}
	// "!   " is not whitespace-only overall so it passes the guard and reaches StartTurn.
	if len(submitted) != 1 {
		t.Fatalf("expected 1 submission, got %v", submitted)
	}
	// Input should be passed unchanged (no rewrite since cmd was empty).
	if submitted[0] != "!   " {
		t.Fatalf("submitted[0] = %q, want %q", submitted[0], "!   ")
	}
}
