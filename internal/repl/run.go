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
		turnCtx, turnCancel := context.WithCancel(ctx)
		loop.SetTurnCancel(turnCancel)
		go func() {
			defer turnCancel()
			r := base // copy by value; do not mutate the shared base
			r.OnEvent = func(ev conversation.Event) {
				select {
				case loop.eventCh <- ev:
				case <-turnCtx.Done():
				}
			}
			r.Tools.Asker = loopAsker{askCh: loop.askCh}
			result, err := r.RunTurn(turnCtx, turnHistory, user)
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
//
// A cancelable child context is derived so that when Run returns (on user exit,
// EOF, or error) the cancel fires, causing any in-flight turn goroutine's
// RunTurn call and its ctx.Done() guards on eventCh/doneCh to unwind promptly
// instead of leaking the goroutine and the underlying HTTP request.
func RunInteractive(ctx context.Context, term Terminal, base conversation.Runner, history []contracts.Message) error {
	ctx, cancel := context.WithCancel(ctx)
	defer cancel()
	return newTurnLoop(ctx, term, base, history).Run(ctx)
}
