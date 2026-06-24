package repl

// G29: MCP-34/35 elicitation wiring test.
//
// Verifies that loopElicitationPrompt + showElicitationOverlay correctly bridge
// between a blocking MCP prompt goroutine and the REPL loop:
//   1. The prompt goroutine calls showElicitationOverlay → sends to elicitationCh.
//   2. The loop receives it, sets activeOverlay, calls onElicitationShown.
//   3. The test writes a key to the terminal (after overlay is shown).
//   4. The overlay submits "elicitation:<action>".
//   5. handleOverlaySubmit sends to activeElicitation.reply.
//   6. The prompt goroutine receives the action.

import (
	"context"
	"testing"
	"time"

	"ccgo/internal/mcp"
)

// TestLoopElicitationPromptBridge verifies the full bridge:
// loopElicitationPrompt sends a request → the REPL shows the overlay →
// user presses Enter (accept) → the goroutine receives "accept".
func TestLoopElicitationPromptBridge(t *testing.T) {
	ft := NewFakeTerminal("", 80, 24)
	gate := make(chan struct{})
	gt := &gatedTerminal{FakeTerminal: ft, gate: gate}
	loop := NewLoop(gt, nil)

	// Seam: write Enter (accept) + Ctrl-D exit after overlay appears.
	loop.onElicitationShown = func() {
		// Enter selects "accept" (cursor=0), double Ctrl-D exits.
		_, _ = gt.FakeTerminal.In.WriteString("\r\x04\x04")
		close(gate)
	}

	// Build the production loopElicitationPrompt using the loop's showElicitationOverlay.
	prompt := loopElicitationPrompt(loop.showElicitationOverlay)
	loop.StartTurn = func(_ string) {}

	// Invoke the prompt in a goroutine (it blocks until the overlay replies).
	resultCh := make(chan string, 1)
	go func() {
		action, _, err := prompt(context.Background(), mcp.ElicitationRequest{Message: "Pick an action"})
		if err != nil {
			resultCh <- "err:" + err.Error()
			return
		}
		resultCh <- action
	}()

	// Run the loop (blocks until loop exits).
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	_ = loop.Run(ctx)

	// Collect the reply.
	select {
	case action := <-resultCh:
		if action != "accept" {
			t.Fatalf("got action = %q, want accept", action)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("timeout waiting for elicitation reply")
	}
}

// TestLoopElicitationPromptEscCancels verifies that pressing Esc in the
// elicitation overlay delivers "cancel" back to the blocking prompt goroutine.
func TestLoopElicitationPromptEscCancels(t *testing.T) {
	ft := NewFakeTerminal("", 80, 24)
	gate := make(chan struct{})
	gt := &gatedTerminal{FakeTerminal: ft, gate: gate}
	loop := NewLoop(gt, nil)

	loop.onElicitationShown = func() {
		// Esc cancels, then double Ctrl-D exits.
		_, _ = gt.FakeTerminal.In.WriteString("\x1b\x04\x04")
		close(gate)
	}

	prompt := loopElicitationPrompt(loop.showElicitationOverlay)
	loop.StartTurn = func(_ string) {}

	resultCh := make(chan string, 1)
	go func() {
		action, _, err := prompt(context.Background(), mcp.ElicitationRequest{Message: "Choose"})
		if err != nil {
			resultCh <- "err:" + err.Error()
			return
		}
		resultCh <- action
	}()

	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	_ = loop.Run(ctx)

	select {
	case action := <-resultCh:
		if action != "cancel" {
			t.Fatalf("got action = %q, want cancel", action)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("timeout waiting for elicitation cancel reply")
	}
}

// TestLoopElicitationDenyOnExit verifies that a pending elicitation is
// cancelled (with "cancel" reply) when the loop exits.
func TestLoopElicitationDenyOnExit(t *testing.T) {
	// Empty FakeTerminal → EOF → loop exits immediately.
	ft := NewFakeTerminal("", 80, 24)
	loop := NewLoop(ft, nil)
	loop.StartTurn = func(_ string) {}

	// Build prompt and invoke immediately — it will block until loop exits.
	prompt := loopElicitationPrompt(loop.showElicitationOverlay)

	resultCh := make(chan string, 1)
	go func() {
		// The elicitationCh has capacity 4, so the send doesn't block.
		action, _, _ := prompt(context.Background(), mcp.ElicitationRequest{Message: "x"})
		resultCh <- action
	}()

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	_ = loop.Run(ctx)

	select {
	case action := <-resultCh:
		if action != "cancel" {
			t.Fatalf("deny-on-exit: got %q want cancel", action)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("deny-on-exit: pending elicitation not cancelled on loop exit")
	}
}
