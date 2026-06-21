package repl

import (
	"context"

	"ccgo/internal/contracts"
	"ccgo/internal/conversation"
	"ccgo/internal/messages"
	"ccgo/internal/permissions"
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

// InteractiveOptions carries everything the REPL needs beyond a turn runner to
// reach CC parity: the live permission engine (for in-session rule updates), a
// settings writer (for persisted rules), the command registry (slash menu), the
// initial mode, and the data backing the resume/theme/memory overlays.
type InteractiveOptions struct {
	// Engine is the live permission engine used for in-session rule updates.
	// May be nil — persistence via Settings still works without it.
	Engine *permissions.Engine

	// Settings persists "allow always" rules to the appropriate settings file.
	// May be nil in tests that don't exercise persistence.
	Settings ruleWriter

	// Registry is the slash-command list used to populate the slash menu.
	// May be nil to disable the slash menu.
	Registry []contracts.Command

	// Mode is the initial permission mode (cycled by Shift+Tab in the REPL).
	Mode contracts.PermissionMode

	// ResumeEntries backs the resume picker overlay.
	ResumeEntries []ResumeEntry

	// Themes backs the theme picker overlay.
	Themes []string

	// MemoryFiles backs the memory file selector overlay.
	MemoryFiles []string

	// Trust, when non-nil, shows the trust dialog at startup.
	Trust *TrustInfo

	// OnOverlay is called when an overlay submission is handled internally
	// (resume:/theme:/memory:/trust: prefixes). Nil is fine.
	OnOverlay func(string)
}

// RunInteractive launches the interactive REPL against a fully-wired runner.
// base must already have Client/Tools/Permissions/Model set (see interactiveRunner).
// history seeds prior turns.
//
// This is a thin wrapper around RunInteractiveWithOptions with zero options,
// retained for backward compatibility with existing callers and tests.
func RunInteractive(ctx context.Context, term Terminal, base conversation.Runner, history []contracts.Message) error {
	return RunInteractiveWithOptions(ctx, term, base, history, InteractiveOptions{})
}

// RunInteractiveWithOptions launches the interactive REPL with the given options.
// A cancelable child context is derived so that when Run returns (on user exit,
// EOF, or error) the cancel fires, causing any in-flight turn goroutine's
// RunTurn call and its ctx.Done() guards on eventCh/doneCh to unwind promptly
// instead of leaking the goroutine and the underlying HTTP request.
func RunInteractiveWithOptions(ctx context.Context, term Terminal, base conversation.Runner, history []contracts.Message, opts InteractiveOptions) error {
	ctx, cancel := context.WithCancel(ctx)
	defer cancel()

	loop := newTurnLoop(ctx, term, base, history)
	if opts.Settings != nil {
		loop.SetSettingsWriter(opts.Settings)
	}
	if opts.Registry != nil {
		loop.SetRegistry(opts.Registry)
	}
	loop.SetMode(opts.Mode)
	loop.onOverlaySubmit = opts.OnOverlay
	if opts.Trust != nil {
		loop.activeOverlay = NewTrustDialog(*opts.Trust)
	}

	// Wire the command router so /resume (and future live-effect commands) are
	// handled without falling through to the model.
	router := NewCommandRouter()
	router.Register("resume", resumeHandler(base.WorkingDirectory))
	router.Register("continue", resumeHandler(base.WorkingDirectory))
	loop.onCommand = func(input string) (CommandOutcome, bool) {
		cc := CommandContext{
			Screen:  &loop.screen,
			History: loop.history,
			CWD:     base.WorkingDirectory,
		}
		outcome, err := router.Dispatch(ctx, input, cc)
		if err != nil {
			return CommandOutcome{Handled: true, Status: "Error: " + err.Error()}, true
		}
		return outcome, outcome.Handled
	}

	return loop.Run(ctx)
}
