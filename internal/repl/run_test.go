package repl

import (
	"context"
	"strings"
	"testing"
	"time"

	"ccgo/internal/api/anthropic"
	"ccgo/internal/contracts"
	"ccgo/internal/conversation"
	"ccgo/internal/tui"
)

type fakeClient struct{}

func (fakeClient) CreateMessage(_ context.Context, req anthropic.Request) (*anthropic.Response, error) {
	return &anthropic.Response{
		ID:         "msg_test",
		Type:       "message",
		Role:       "assistant",
		Model:      req.Model,
		Content:    []contracts.ContentBlock{contracts.NewTextBlock("assistant-reply")},
		StopReason: "end_turn",
	}, nil
}

// turnGateTerminal wraps FakeTerminal: the first Read returns the buffered
// input; subsequent Reads block on gate (closed by onTurnDone), then drain
// the buffer which is empty so they return io.EOF, causing a clean loop exit.
type turnGateTerminal struct {
	*FakeTerminal
	gate chan struct{}
	sent bool
}

func (g *turnGateTerminal) Read(p []byte) (int, error) {
	if !g.sent {
		g.sent = true
		return g.FakeTerminal.Read(p)
	}
	// Wait for the turn to complete (gate is closed by onTurnDone), then
	// drain the buffer (empty → io.EOF) so the loop exits cleanly.
	<-g.gate
	return g.FakeTerminal.Read(p)
}

func TestRunInteractiveOneTurn(t *testing.T) {
	ft := NewFakeTerminal("hello\r", 80, 24)
	gate := make(chan struct{})
	term := &turnGateTerminal{FakeTerminal: ft, gate: gate}

	base := conversation.Runner{
		Client:    fakeClient{},
		Model:     "claude-test",
		MaxTokens: 256,
	}

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	loop := newTurnLoop(ctx, term, base, nil)
	loop.onTurnDone = func() { close(gate) }

	if err := loop.Run(ctx); err != nil {
		t.Fatalf("loop.Run error: %v", err)
	}

	visible := tui.TerminalVisibleText(ft.Out.String())
	if !strings.Contains(visible, "assistant-reply") {
		t.Fatalf("assistant reply not rendered; got: %q", visible)
	}
}
