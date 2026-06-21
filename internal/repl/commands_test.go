package repl

import (
	"context"
	"testing"
	"time"

	"ccgo/internal/contracts"
)

func TestCommandRouterDispatchHandled(t *testing.T) {
	router := NewCommandRouter()
	var gotArgs string
	router.Register("clear", func(ctx context.Context, cc CommandContext) (CommandOutcome, error) {
		gotArgs = cc.Args
		return CommandOutcome{Handled: true, ReplaceHistory: true, NewHistory: nil, Status: "cleared"}, nil
	})

	out, err := router.Dispatch(context.Background(), "/clear all", CommandContext{Args: "", History: []contracts.Message{{}}})
	if err != nil {
		t.Fatalf("Dispatch err: %v", err)
	}
	if !out.Handled {
		t.Fatal("expected /clear to be handled")
	}
	if gotArgs != "all" {
		t.Fatalf("Args = %q want %q", gotArgs, "all")
	}
	if !out.ReplaceHistory || out.NewHistory != nil {
		t.Fatalf("expected history replaced with nil, got %+v", out)
	}
}

func TestCommandRouterUnregisteredFallsThrough(t *testing.T) {
	router := NewCommandRouter()
	out, err := router.Dispatch(context.Background(), "/unknownxyz", CommandContext{})
	if err != nil {
		t.Fatalf("Dispatch err: %v", err)
	}
	if out.Handled {
		t.Fatal("unregistered command must fall through (Handled=false)")
	}
}

func TestCommandRouterNonSlashFallsThrough(t *testing.T) {
	router := NewCommandRouter()
	router.Register("clear", func(ctx context.Context, cc CommandContext) (CommandOutcome, error) {
		return CommandOutcome{Handled: true}, nil
	})
	out, _ := router.Dispatch(context.Background(), "hello world", CommandContext{})
	if out.Handled {
		t.Fatal("plain prompt text must not be handled by the router")
	}
}

func TestLoopRouterClearsHistory(t *testing.T) {
	ft := NewFakeTerminal("/clear\r\x04\x04", 80, 24)
	l := NewLoop(ft, nil)
	l.history = []contracts.Message{{Type: contracts.MessageUser}}
	sent := 0
	l.StartTurn = func(string) { sent++ }
	l.onCommand = func(input string) (CommandOutcome, bool) {
		return CommandOutcome{Handled: true, ReplaceHistory: true, NewHistory: nil, Status: "cleared"}, true
	}
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	if err := l.Run(ctx); err != nil {
		t.Fatalf("Run err: %v", err)
	}
	if sent != 0 {
		t.Fatalf("StartTurn called %d times; live command must not hit the model", sent)
	}
	if len(l.history) != 0 {
		t.Fatalf("history not cleared: %d msgs", len(l.history))
	}
}
