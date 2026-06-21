package repl

import (
	"context"

	"ccgo/internal/contracts"
	"ccgo/internal/conversation"
	"ccgo/internal/messages"
)

// newTurnLoop builds a Loop wired to run real conversation turns. Callers may
// set loop.onTurnDone before calling loop.Run for test synchronization.
func newTurnLoop(ctx context.Context, term Terminal, base conversation.Runner, history []contracts.Message) *Loop {
	loop := NewLoop(term, nil)
	loop.history = history
	loop.StartTurn = func(input string) {
		user := messages.UserText(input)
		turnHistory := append([]contracts.Message(nil), loop.history...)
		go func() {
			r := base // copy by value; do not mutate the shared base
			r.OnEvent = func(ev conversation.Event) {
				select {
				case loop.eventCh <- ev:
				case <-ctx.Done():
				}
			}
			r.Tools.Asker = loopAsker{askCh: loop.askCh}
			result, err := r.RunTurn(ctx, turnHistory, user)
			select {
			case loop.doneCh <- turnOutcome{result: result, err: err}:
			case <-ctx.Done():
			}
		}()
	}
	return loop
}

// RunInteractive launches the interactive REPL against a fully-wired runner.
// base must already have Client/Tools/Permissions/Model set (see interactiveRunner).
// history seeds prior turns.
func RunInteractive(ctx context.Context, term Terminal, base conversation.Runner, history []contracts.Message) error {
	return newTurnLoop(ctx, term, base, history).Run(ctx)
}
