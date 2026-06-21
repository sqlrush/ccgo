package repl

import (
	"context"
	"strings"
	"testing"
	"time"
)

// TestREPLDispatchesNewCommandsWithoutModel verifies that Phase-6b slash
// commands are routed through the production wiring without ever hitting the
// model (StartTurn must not be called) and that the expected status lines
// appear in the terminal output.
func TestREPLDispatchesNewCommandsWithoutModel(t *testing.T) {
	// Feed /theme dark, then /effort high, then double-EOF to exit.
	ft := NewFakeTerminal("/theme dark\r/effort high\r\x04\x04", 80, 24)
	l := NewLoop(ft, nil)

	router := NewCommandRouter()
	router.Register("theme", func(ctx context.Context, cc CommandContext) (CommandOutcome, error) {
		return CommandOutcome{Handled: true, Status: "theme ok"}, nil
	})
	router.Register("effort", func(ctx context.Context, cc CommandContext) (CommandOutcome, error) {
		return CommandOutcome{Handled: true, Status: "effort ok"}, nil
	})
	l.onCommand = func(input string) (CommandOutcome, bool) {
		out, err := router.Dispatch(context.Background(), input, CommandContext{Screen: &l.screen})
		if err != nil {
			return CommandOutcome{}, false
		}
		return out, out.Handled
	}

	hit := 0
	l.StartTurn = func(string) { hit++ }

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()

	if err := l.Run(ctx); err != nil {
		t.Fatalf("Run: %v", err)
	}
	if hit != 0 {
		t.Fatalf("commands must not hit the model; StartTurn called %d times", hit)
	}
	got := ft.Out.String()
	if !strings.Contains(got, "theme ok") {
		t.Fatalf("expected 'theme ok' in output; got: %q", got)
	}
	if !strings.Contains(got, "effort ok") {
		t.Fatalf("expected 'effort ok' in output; got: %q", got)
	}
}

// TestREPLDispatchesHooksAndIdeCommandsWithoutModel verifies that /hooks and
// /ide are routed correctly and never reach the model.
func TestREPLDispatchesHooksAndIdeCommandsWithoutModel(t *testing.T) {
	ft := NewFakeTerminal("/hooks\r/ide\r\x04\x04", 80, 24)
	l := NewLoop(ft, nil)

	router := NewCommandRouter()
	router.Register("hooks", func(ctx context.Context, cc CommandContext) (CommandOutcome, error) {
		return CommandOutcome{Handled: true, Status: "hooks ok"}, nil
	})
	router.Register("ide", func(ctx context.Context, cc CommandContext) (CommandOutcome, error) {
		return CommandOutcome{Handled: true, Status: "ide ok"}, nil
	})
	l.onCommand = func(input string) (CommandOutcome, bool) {
		out, err := router.Dispatch(context.Background(), input, CommandContext{Screen: &l.screen})
		if err != nil {
			return CommandOutcome{}, false
		}
		return out, out.Handled
	}

	hit := 0
	l.StartTurn = func(string) { hit++ }

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()

	if err := l.Run(ctx); err != nil {
		t.Fatalf("Run: %v", err)
	}
	if hit != 0 {
		t.Fatalf("commands must not hit the model; StartTurn called %d times", hit)
	}
	got := ft.Out.String()
	if !strings.Contains(got, "hooks ok") {
		t.Fatalf("expected 'hooks ok' in output; got: %q", got)
	}
	if !strings.Contains(got, "ide ok") {
		t.Fatalf("expected 'ide ok' in output; got: %q", got)
	}
}

// TestREPLDispatchesDoctorWithoutModel verifies that /doctor is dispatched
// without reaching the model.
func TestREPLDispatchesDoctorWithoutModel(t *testing.T) {
	ft := NewFakeTerminal("/doctor\r\x04\x04", 80, 24)
	l := NewLoop(ft, nil)

	router := NewCommandRouter()
	router.Register("doctor", func(ctx context.Context, cc CommandContext) (CommandOutcome, error) {
		return CommandOutcome{Handled: true, Status: "doctor ok"}, nil
	})
	l.onCommand = func(input string) (CommandOutcome, bool) {
		out, err := router.Dispatch(context.Background(), input, CommandContext{Screen: &l.screen})
		if err != nil {
			return CommandOutcome{}, false
		}
		return out, out.Handled
	}

	hit := 0
	l.StartTurn = func(string) { hit++ }

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()

	if err := l.Run(ctx); err != nil {
		t.Fatalf("Run: %v", err)
	}
	if hit != 0 {
		t.Fatalf("doctor command must not hit the model; StartTurn called %d times", hit)
	}
	got := ft.Out.String()
	if !strings.Contains(got, "doctor ok") {
		t.Fatalf("expected 'doctor ok' in output; got: %q", got)
	}
}
