package repl

import (
	"context"
	"strings"
	"testing"
	"time"

	"ccgo/internal/api/anthropic"
	"ccgo/internal/contracts"
	"ccgo/internal/conversation"
	"ccgo/internal/tool"
	"ccgo/internal/tui"
)

// blockingClient blocks in CreateMessage until ctx is cancelled, then signals
// via clientReturned (buffered-1) that it has returned. Used to prove that
// RunInteractive's internal cancel propagates to an in-flight turn goroutine.
type blockingClient struct {
	clientReturned chan struct{}
}

func (c blockingClient) CreateMessage(ctx context.Context, _ anthropic.Request) (*anthropic.Response, error) {
	<-ctx.Done()
	// Non-blocking send: buffered channel ensures the signal is never lost even
	// if nobody is waiting (RunInteractive has already returned).
	select {
	case c.clientReturned <- struct{}{}:
	default:
	}
	return nil, ctx.Err()
}

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

	loop := newTurnLoop(ctx, term, base, nil, HistoryRecorder{Skip: true})
	loop.onTurnDone = func() { close(gate) }

	if err := loop.Run(ctx); err != nil {
		t.Fatalf("loop.Run error: %v", err)
	}

	visible := tui.TerminalVisibleText(ft.Out.String())
	if !strings.Contains(visible, "assistant-reply") {
		t.Fatalf("assistant reply not rendered; got: %q", visible)
	}
}

// TestRunInteractiveCancelsTurnOnExit proves that when RunInteractive returns
// (e.g. the user exits while a turn is in flight) the internal cancel propagates
// to the turn goroutine's RunTurn context, unblocking any in-flight API call.
// Without the ctx, cancel := context.WithCancel / defer cancel() fix in
// RunInteractive, the blockingClient would never receive ctx.Done() and the
// goroutine would leak.
func TestRunInteractivePersistsAllowAlways(t *testing.T) {
	// Drive: a permission ask arrives, user picks "always" (action index 1).
	// Assert that the recording ruleWriter receives exactly one update.
	//
	// We use a gatedTerminal so that the input bytes (\x1b[B = Down, \r = Enter)
	// are not read before the dialog is shown. Once onPermissionShown fires, the
	// gate is closed and the Read proceeds.  After the two-byte key sequence is
	// consumed, the next Read returns EOF which causes the loop to exit cleanly.
	ft := NewFakeTerminal("\x1b[B\r", 80, 24) // Down + Enter selects action[1] = "Allow always"
	gate := make(chan struct{})
	gt := &gatedTerminal{FakeTerminal: ft, gate: gate}
	l := NewLoop(gt, nil)

	var persisted []contracts.PermissionUpdate
	l.SetSettingsWriter(recordingWriter{onApply: func(u contracts.PermissionUpdate) error {
		persisted = append(persisted, u)
		return nil
	}})

	// Release input only after the dialog is rendered (test seam).
	l.onPermissionShown = func() { close(gate) }

	asker := loopAsker{askCh: l.askCh}
	decisionCh := make(chan contracts.PermissionDecision, 1)
	go func() {
		d, err := asker.Ask(context.Background(), tool.PermissionAskRequest{
			ToolUseID: "u1", ToolName: "Read", Path: "/tmp/x",
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
		t.Fatal("asker never received a decision within 2s")
	}
	if len(persisted) != 1 {
		t.Fatalf("expected 1 persisted rule, got %d", len(persisted))
	}
}

type recordingWriter struct{ onApply func(contracts.PermissionUpdate) error }

func (w recordingWriter) Apply(u contracts.PermissionUpdate) error { return w.onApply(u) }

func TestRunInteractiveCancelsTurnOnExit(t *testing.T) {
	clientReturned := make(chan struct{}, 1)
	base := conversation.Runner{
		Client:    blockingClient{clientReturned: clientReturned},
		Model:     "x",
		MaxTokens: 8,
	}

	// FakeTerminal with "hello\r" followed by immediate EOF.  The loop submits
	// the prompt (launching the blocking turn goroutine), then exits because the
	// input stream closes — while the client is still blocked in CreateMessage.
	term := NewFakeTerminal("hello\r", 80, 24)

	if err := RunInteractive(context.Background(), term, base, nil); err != nil {
		t.Fatalf("RunInteractive error: %v", err)
	}

	// After RunInteractive returns, the deferred cancel() must have fired,
	// causing blockingClient.CreateMessage to unblock and signal clientReturned.
	select {
	case <-clientReturned:
		// pass: turn goroutine was cancelled and unblocked
	case <-time.After(2 * time.Second):
		t.Fatal("turn goroutine leaked: CreateMessage was not cancelled within 2s")
	}
}

// TestCwdFromWorktreeResults verifies that cwdFromWorktreeResults correctly
// extracts the new working directory from EnterWorktree and ExitWorktree
// StructuredContent payloads (WORKTREE-CWD-01).
func TestCwdFromWorktreeResults(t *testing.T) {
	t.Parallel()
	cases := []struct {
		name    string
		results []contracts.ToolResult
		want    string
	}{
		{
			name:    "empty results",
			results: nil,
			want:    "",
		},
		{
			name: "unrelated tool result ignored",
			results: []contracts.ToolResult{
				{Content: "some output", StructuredContent: map[string]any{"other": "data"}},
			},
			want: "",
		},
		{
			name: "EnterWorktree result returns worktree_path",
			results: []contracts.ToolResult{
				{
					StructuredContent: map[string]any{
						"worktree_path":   "/tmp/.ccgo-worktrees/sess-myfeature",
						"worktree_branch": "HEAD",
						"original_cwd":    "/tmp/repo",
						"message":         "Created worktree.",
					},
				},
			},
			want: "/tmp/.ccgo-worktrees/sess-myfeature",
		},
		{
			name: "ExitWorktree result returns original_cwd",
			results: []contracts.ToolResult{
				{
					StructuredContent: map[string]any{
						"original_cwd":  "/tmp/repo",
						"worktree_path": "/tmp/.ccgo-worktrees/sess-myfeature",
						"action":        "keep",
						"message":       "Exited worktree.",
					},
				},
			},
			want: "/tmp/repo",
		},
		{
			name: "error result is skipped",
			results: []contracts.ToolResult{
				{
					IsError: true,
					StructuredContent: map[string]any{
						"worktree_path":   "/tmp/.ccgo-worktrees/sess-err",
						"worktree_branch": "HEAD",
					},
				},
			},
			want: "",
		},
		{
			name: "last worktree result wins when multiple present",
			results: []contracts.ToolResult{
				{
					StructuredContent: map[string]any{
						"worktree_path":   "/tmp/.ccgo-worktrees/sess-first",
						"worktree_branch": "HEAD",
						"original_cwd":    "/tmp/repo",
					},
				},
				{
					StructuredContent: map[string]any{
						"original_cwd":  "/tmp/repo",
						"worktree_path": "/tmp/.ccgo-worktrees/sess-first",
						"action":        "remove",
					},
				},
			},
			want: "/tmp/repo",
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			got := cwdFromWorktreeResults(tc.results)
			if got != tc.want {
				t.Errorf("cwdFromWorktreeResults() = %q, want %q", got, tc.want)
			}
		})
	}
}
