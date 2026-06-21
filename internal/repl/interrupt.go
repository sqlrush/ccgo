package repl

import (
	"context"

	"ccgo/internal/tui"
)

// SetTurnCancel registers the cancel func for the currently launching turn.
// The runner (run.go) calls this before starting RunTurn so an ESC/Ctrl-C
// can abort the in-flight HTTP request and tool execution.
func (l *Loop) SetTurnCancel(cancel context.CancelFunc) {
	l.turnCancel = cancel
}

// interruptTurn aborts the running turn: it cancels the turn context, surfaces
// an "Interrupted" line, and resets running/spinner state. No-op when idle.
func (l *Loop) interruptTurn() {
	if !l.running {
		return
	}
	if l.turnCancel != nil {
		l.turnCancel()
		l.turnCancel = nil
	}
	l.running = false
	l.stopSpinner()
	l.screen.AppendMessage(tui.Message{Role: tui.RoleSystem, Text: "Interrupted by user."})
}
