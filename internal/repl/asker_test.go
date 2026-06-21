package repl

import (
	"context"
	"testing"
	"time"

	"ccgo/internal/contracts"
	"ccgo/internal/tool"
)

// gatedTerminal wraps FakeTerminal and blocks Read until gate is closed.
// This ensures the Enter keypress is only consumed after the dialog is shown,
// preventing the race where the key is read before showPermission runs.
type gatedTerminal struct {
	*FakeTerminal
	gate chan struct{}
}

func (g *gatedTerminal) Read(p []byte) (int, error) {
	<-g.gate
	return g.FakeTerminal.Read(p)
}

func TestLoopAskerAllow(t *testing.T) {
	ft := NewFakeTerminal("\r", 80, 24)
	gate := make(chan struct{})
	gt := &gatedTerminal{FakeTerminal: ft, gate: gate}
	l := NewLoop(gt, nil)
	// release input only after dialog is shown — test seam
	l.onPermissionShown = func() { close(gate) }

	asker := loopAsker{askCh: l.askCh}
	decisionCh := make(chan contracts.PermissionDecision, 1)
	go func() {
		d, err := asker.Ask(context.Background(), tool.PermissionAskRequest{
			ToolUseID:   "u1",
			ToolName:    "Bash",
			Description: "run ls",
		})
		if err == nil {
			decisionCh <- d
		}
	}()

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	_ = l.Run(ctx)

	select {
	case d := <-decisionCh:
		if d.Behavior != contracts.PermissionAllow {
			t.Fatalf("decision = %v want allow", d.Behavior)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("asker never received a decision")
	}
}

func TestLoopAskerDeny(t *testing.T) {
	// Esc produces ScreenEventCancelled which resolves to PermissionDeny.
	esc := "\x1b"
	ft := NewFakeTerminal(esc, 80, 24)
	gate := make(chan struct{})
	gt := &gatedTerminal{FakeTerminal: ft, gate: gate}
	l := NewLoop(gt, nil)
	l.onPermissionShown = func() { close(gate) }

	asker := loopAsker{askCh: l.askCh}
	decisionCh := make(chan contracts.PermissionDecision, 1)
	go func() {
		d, err := asker.Ask(context.Background(), tool.PermissionAskRequest{
			ToolUseID:   "u2",
			ToolName:    "Bash",
			Description: "run rm -rf",
		})
		if err == nil {
			decisionCh <- d
		}
	}()

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	_ = l.Run(ctx)

	select {
	case d := <-decisionCh:
		if d.Behavior != contracts.PermissionDeny {
			t.Fatalf("decision = %v want deny", d.Behavior)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("asker never received a decision")
	}
}

func TestLoopDenyPendingOnExit(t *testing.T) {
	// Empty input -> immediate EOF -> Run exits -> denyPendingAsks fires.
	ft := NewFakeTerminal("", 80, 24)
	l := NewLoop(ft, nil)

	reply := make(chan contracts.PermissionDecision, 1)
	l.askCh <- askRequest{
		req:   tool.PermissionAskRequest{ToolUseID: "u1", ToolName: "Bash"},
		reply: reply,
	}

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	_ = l.Run(ctx)

	select {
	case d := <-reply:
		if d.Behavior != contracts.PermissionDeny {
			t.Fatalf("want deny, got %v", d.Behavior)
		}
	case <-time.After(time.Second):
		t.Fatal("asker not unblocked on exit")
	}
}

func TestDecisionFromAction(t *testing.T) {
	tests := []struct {
		action string
		want   contracts.PermissionBehavior
	}{
		{"Allow", contracts.PermissionAllow},
		{"Allow Session", contracts.PermissionAllow},
		{"Deny", contracts.PermissionDeny},
		{"anything", contracts.PermissionDeny},
	}
	for _, tc := range tests {
		got := decisionFromAction(tc.action)
		if got != tc.want {
			t.Errorf("decisionFromAction(%q) = %v, want %v", tc.action, got, tc.want)
		}
	}
}
