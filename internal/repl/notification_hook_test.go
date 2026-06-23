package repl

// TestLoopPermissionAskNotifyFires verifies that onPermissionAskNotify is called
// when showPermission is invoked (HOOK-35: Notification hook trigger wiring).
// This is the REPL-level seam test; the conversation-level test is in
// conversation/stop_hook_fields_test.go.

import (
	"testing"

	"ccgo/internal/contracts"
	"ccgo/internal/tool"
)

func TestLoopPermissionAskNotifyFires(t *testing.T) {
	term := NewFakeTerminal("", 80, 24)
	loop := NewLoop(term, nil)

	var notifiedTool string
	done := make(chan struct{})
	loop.onPermissionAskNotify = func(toolName string) {
		notifiedTool = toolName
		close(done)
	}

	ar := askRequest{
		req: tool.PermissionAskRequest{
			ToolUseID: contracts.NewID(),
			ToolName:  "Write",
			Path:      "/tmp/test.go",
		},
		reply: make(chan contracts.PermissionDecision, 1),
	}

	loop.showPermission(ar)

	// Wait for async notification callback (it runs in a goroutine).
	select {
	case <-done:
	case <-make(chan struct{}): // never, just to show the pattern
	}

	// Since onPermissionAskNotify is called in a goroutine, we need to give it
	// a moment; the select on done ensures we don't race.
	_ = notifiedTool // accessed after done is closed (goroutine completed)
	if notifiedTool != "Write" {
		t.Fatalf("notifiedTool = %q; want %q", notifiedTool, "Write")
	}
}
