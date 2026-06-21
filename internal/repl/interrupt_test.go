package repl

import (
	"context"
	"testing"
)

func TestInterruptTurnCancelsContext(t *testing.T) {
	ft := NewFakeTerminal("", 80, 24)
	l := NewLoop(ft, nil)
	_, cancel := context.WithCancel(context.Background())
	cancelled := false
	l.SetTurnCancel(func() { cancelled = true; cancel() })
	l.running = true

	l.interruptTurn()

	if !cancelled {
		t.Fatal("interruptTurn did not invoke the per-turn cancel")
	}
	if l.running {
		t.Fatal("running flag should clear after interrupt")
	}
	if l.turnCancel != nil {
		t.Fatal("turnCancel should be reset to nil after interrupt")
	}
}

func TestInterruptTurnNoopWhenIdle(t *testing.T) {
	ft := NewFakeTerminal("", 80, 24)
	l := NewLoop(ft, nil)
	// No turn running, no cancel set: must not panic.
	l.interruptTurn()
	if l.running {
		t.Fatal("running should stay false")
	}
}
