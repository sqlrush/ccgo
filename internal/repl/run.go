package repl

import (
	"context"
	"encoding/json"
	"strings"

	"ccgo/internal/config"
	"ccgo/internal/contracts"
	"ccgo/internal/conversation"
	"ccgo/internal/messages"
	"ccgo/internal/permissions"
	"ccgo/internal/session"
	"ccgo/internal/tool"
	asktools "ccgo/internal/tools/ask"
	"ccgo/internal/tui"
)

// newTurnLoop builds a Loop wired to run real conversation turns. Callers may
// set loop.onTurnDone before calling loop.Run for test synchronization.
// recorder is called best-effort on each submitted prompt; history failures
// must not abort the turn.
func newTurnLoop(ctx context.Context, term Terminal, base conversation.Runner, history []contracts.Message, recorder HistoryRecorder) *Loop {
	loop := NewLoop(term, nil)
	loop.history = history
	loop.StartTurn = func(input string) {
		// Record submitted prompt to ~/.claude/history.jsonl (best-effort).
		_ = recorder.Record(input)
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
			// Inject the interactive question asker so AskUserQuestion can
			// present its overlay via the REPL loop (TOOL-ASK-01/03).
			r.ExtraToolMetadata = mergeQuestionAsker(r.ExtraToolMetadata, loop.questionCh)
			result, err := r.RunTurn(turnCtx, turnHistory, user)
			// Propagate cwd changes from EnterWorktree/ExitWorktree so subsequent
			// turns see the updated working directory (WORKTREE-CWD-01).
			if newCWD := cwdFromWorktreeResults(result.ToolResults); newCWD != "" {
				base.WorkingDirectory = newCWD
			}
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

	// PromptHistory seeds Up-arrow / Ctrl+R navigation with previously submitted
	// prompts loaded from ~/.claude/history.jsonl. May be nil; nil means only
	// in-session history is available.
	PromptHistory []session.HistoryEntry

	// EditorMode, when set to "vim", enables vim keybindings in the prompt input.
	// Sourced from mergedSettings.EditorMode at startup.
	EditorMode string

	// CustomKeymap, when non-nil, overrides specific bindings on top of
	// DefaultKeymap. Loaded from ~/.claude/keybindings.json at startup; nil if
	// the file is absent.
	CustomKeymap *tui.Keymap

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

// newTurnLoopForRunner creates a turn loop with a HistoryRecorder derived from
// the runner's WorkingDirectory and SessionID.
func newTurnLoopForRunner(ctx context.Context, term Terminal, base conversation.Runner, history []contracts.Message) *Loop {
	recorder := NewHistoryRecorder(base.WorkingDirectory, base.SessionID)
	return newTurnLoop(ctx, term, base, history, recorder)
}

// newTurnLoopForRunnerWithHistory is like newTurnLoopForRunner but seeds the
// loop's prompt input with persisted history entries so Up-arrow / Ctrl+R
// navigation surfaces prior prompts from the first keystroke.
func newTurnLoopForRunnerWithHistory(ctx context.Context, term Terminal, base conversation.Runner, history []contracts.Message, promptHistory []session.HistoryEntry) *Loop {
	recorder := NewHistoryRecorder(base.WorkingDirectory, base.SessionID)
	loop := NewLoopFromHistoryEntries(term, promptHistory)
	loop.history = history
	loop.StartTurn = func(input string) {
		_ = recorder.Record(input)
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
			// Inject the interactive question asker so AskUserQuestion can
			// present its overlay via the REPL loop (TOOL-ASK-01/03).
			r.ExtraToolMetadata = mergeQuestionAsker(r.ExtraToolMetadata, loop.questionCh)
			result, err := r.RunTurn(turnCtx, turnHistory, user)
			// Propagate cwd changes from EnterWorktree/ExitWorktree so subsequent
			// turns see the updated working directory (WORKTREE-CWD-01).
			if newCWD := cwdFromWorktreeResults(result.ToolResults); newCWD != "" {
				base.WorkingDirectory = newCWD
			}
			select {
			case loop.doneCh <- turnOutcome{result: result, err: err}:
			case <-ctx.Done():
			}
		}()
	}
	return loop
}

// newProductionRouter builds the canonical CommandRouter wired by RunInteractiveWithOptions.
// It is extracted so that the parity test can enumerate registered names without
// duplicating the registration list.
func newProductionRouter(cwd string, registry []contracts.Command) *CommandRouter {
	router := NewCommandRouter()
	router.Register("resume", resumeHandler(cwd))
	router.Register("continue", resumeHandler(cwd))
	router.Register("agents", agentsHandler(cwd))
	router.Register("theme", themeHandler())
	router.Register("effort", effortHandler())
	router.Register("vim", vimHandler())
	router.Register("permissions", permissionsHandler())
	router.Register("allowed-tools", permissionsHandler())
	router.Register("export", exportHandler(cwd))
	router.Register("hooks", hooksHandler(func() contracts.Settings {
		s, _ := config.LoadSettingsFile(config.UserSettingsPath())
		return s
	}))
	router.Register("ide", ideHandler(nil)) // nil → defaultIDEDetect
	router.Register("memory", memoryHandler(cwd))
	router.Register("help", helpHandler(registry))
	router.Register("doctor", doctorHandler(cwd, ""))
	router.Register("model", modelHandler())

	// F4 commands: slash command completion.
	router.Register("add-dir", addDirHandler(cwd))
	// /plan: starts in default mode; the loop updates its own mode via CommandOutcome.NewMode.
	// newProductionRouterWithMode is used when the caller knows the initial mode.
	router.Register("plan", planHandlerWith(contracts.PermissionDefault))
	router.Register("terminal-setup", terminalSetupHandler())
	router.Register("branch", branchHandler())
	router.Register("rename", renameHandler("", cwd))
	router.Register("diff", diffHandler(cwd))
	router.Register("copy", copyHandler(nil))
	router.Register("exit", exitHandler())
	router.Register("quit", exitHandler())
	router.Register("fast", fastHandler())
	router.Register("stats", statsHandler())
	router.Register("tag", tagHandler("", cwd))
	router.Register("tasks", tasksHandler())
	router.Register("keybindings", keybindingsHandler(""))
	router.Register("reload-plugins", reloadPluginsHandler(cwd))
	router.Register("color", colorHandler())
	router.Register("statusline", statusLineHandler())
	router.Register("bashes", tasksHandler())
	// AUTH-LOGIN-01/02: /login and /logout run the OAuth flow / clear creds.
	router.Register("login", loginHandler())
	router.Register("logout", logoutHandler())
	return router
}

// RunInteractiveWithOptions launches the interactive REPL with the given options.
// A cancelable child context is derived so that when Run returns (on user exit,
// EOF, or error) the cancel fires, causing any in-flight turn goroutine's
// RunTurn call and its ctx.Done() guards on eventCh/doneCh to unwind promptly
// instead of leaking the goroutine and the underlying HTTP request.
func RunInteractiveWithOptions(ctx context.Context, term Terminal, base conversation.Runner, history []contracts.Message, opts InteractiveOptions) error {
	ctx, cancel := context.WithCancel(ctx)
	defer cancel()

	// Fire SessionStart once at the session boundary. Source is "startup" for a
	// fresh session (no prior history) and "resume" when seeded with history.
	// If the hook injects additionalContext, prepend it as a user message.
	source := conversation.SessionStartStartup
	if len(history) > 0 {
		source = conversation.SessionStartResume
	}
	if injected, err := base.RunSessionStartHooks(ctx, source); err != nil {
		return err
	} else if injected != "" {
		history = append([]contracts.Message(nil), history...)
		history = append(history, messages.UserText(injected))
	}

	// SessionEnd fires when RunInteractiveWithOptions returns (before cancel, due
	// to LIFO ordering with defer cancel() declared above). Use context.Background()
	// so the call is not affected by the parent ctx cancellation that may have
	// triggered the exit.
	defer func() {
		_ = base.RunSessionEndHooks(context.Background(), conversation.SessionEndPromptInputExit)
	}()

	// W-C05: when a live engine pointer is provided, replace base.Permissions
	// with a thin wrapper that delegates DecideTool to *eng on every call.
	// This means every StartTurn closure's "r := base" copy still reads from
	// the pointer, so Shift+Tab mode changes and allow-always persists take
	// effect on subsequent turns without re-creating the runner.
	if opts.Engine != nil {
		base.Permissions = ptrEngineDecider{eng: opts.Engine}
	}

	var loop *Loop
	if len(opts.PromptHistory) > 0 {
		loop = newTurnLoopForRunnerWithHistory(ctx, term, base, history, opts.PromptHistory)
	} else {
		loop = newTurnLoopForRunner(ctx, term, base, history)
	}
	if opts.Settings != nil {
		loop.SetSettingsWriter(opts.Settings)
	}
	if opts.Registry != nil {
		loop.SetRegistry(opts.Registry)
	}
	loop.SetMode(opts.Mode)
	loop.onOverlaySubmit = opts.OnOverlay
	if opts.CustomKeymap != nil {
		loop.screen.Keymap = *opts.CustomKeymap
	}
	if opts.EditorMode == "vim" {
		loop.screen.SetVimEnabled(true)
		loop.refreshBaseStatus()
	}
	if opts.Trust != nil {
		loop.activeOverlay = NewTrustDialog(*opts.Trust)
	}

	// W-C05: wire Shift+Tab mode changes and allow-always persist into the live
	// engine pointer so every subsequent StartTurn uses the updated mode/rules.
	if opts.Engine != nil {
		eng := opts.Engine
		loop.onModeChange = func(mode contracts.PermissionMode) {
			next, err := eng.ApplyUpdate(contracts.PermissionUpdate{
				Type: "setMode",
				Mode: mode,
			})
			if err == nil {
				*eng = next
			}
		}
		loop.onRulePersisted = func(update contracts.PermissionUpdate) {
			next, err := eng.ApplyUpdate(update)
			if err == nil {
				*eng = next
			}
		}
	}

	// Wire the command router so /resume (and future live-effect commands) are
	// handled without falling through to the model.
	router := newProductionRouter(base.WorkingDirectory, opts.Registry)

	// When the host supplied prebuilt resume entries, prefer them for the no-arg
	// picker so the overlay reflects exactly what main.go discovered at startup.
	// Arg-based lookups still fall through to the disk-backed resume handler.
	if len(opts.ResumeEntries) > 0 {
		entries := opts.ResumeEntries
		fallback := resumeHandler(base.WorkingDirectory)
		pickerOrFallback := func(ctx context.Context, cc CommandContext) (CommandOutcome, error) {
			if strings.TrimSpace(cc.Args) == "" {
				return CommandOutcome{Handled: true, Overlay: NewResumePicker(entries)}, nil
			}
			return fallback(ctx, cc)
		}
		router.Register("resume", pickerOrFallback)
		router.Register("continue", pickerOrFallback)
	}

	// When the host supplied prebuilt memory file paths, prefer them for the
	// /memory selector overlay instead of rediscovering them on disk.
	if len(opts.MemoryFiles) > 0 {
		files := opts.MemoryFiles
		router.Register("memory", memoryHandlerWith(func() ([]string, error) {
			return files, nil
		}))
	}

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

	// Wire Notification hooks: fire when a permission dialog is shown (the
	// system is idle, awaiting the user's permission decision). HOOK-35.
	loop.onPermissionAskNotify = func(toolName string) {
		_ = base.RunNotificationHooks(context.Background(), "permission_requested",
			"Awaiting permission for "+toolName, "")
	}

	return loop.Run(ctx)
}

// mergeQuestionAsker returns a copy of extra (which may be nil) with the
// loopQuestionAsker injected under MetadataQuestionAskerKey. This is called
// once per turn so each turn's r.ExtraToolMetadata is independent and
// immutable after construction (no mutation of the shared base map).
func mergeQuestionAsker(extra map[string]any, qCh chan questionRequest) map[string]any {
	out := make(map[string]any, len(extra)+1)
	for k, v := range extra {
		out[k] = v
	}
	out[asktools.MetadataQuestionAskerKey] = loopQuestionAsker{questionCh: qCh}
	return out
}

// ptrEngineDecider is a thin tool.PermissionDecider that delegates every
// DecideTool call to the engine stored behind the pointer. Because it holds
// a pointer (not a value), copying it via "r := base" in the StartTurn
// closure still reads from the live engine — so Shift+Tab mode changes and
// allow-always persists take effect on the next turn without recreating the
// runner.
type ptrEngineDecider struct {
	eng *permissions.Engine
}

func (d ptrEngineDecider) DecideTool(t tool.Tool, raw json.RawMessage, ctx tool.Context) (contracts.PermissionDecision, error) {
	return tool.NewEnginePermissionDecider(*d.eng).DecideTool(t, raw, ctx)
}

// cwdFromWorktreeResults scans tool results for EnterWorktree/ExitWorktree
// outcomes and returns the new working directory that should apply to subsequent
// turns. EnterWorktree results carry "worktree_path" (the new cwd); ExitWorktree
// results carry "original_cwd" (the restored cwd). The last matching result wins
// so that a turn with both tools produces the correct final directory.
// Returns "" when no worktree tool ran or no cwd change is needed (WORKTREE-CWD-01).
func cwdFromWorktreeResults(results []contracts.ToolResult) string {
	newCWD := ""
	for _, r := range results {
		if r.IsError || r.StructuredContent == nil {
			continue
		}
		sc := r.StructuredContent
		// ExitWorktree: action key is present, new cwd is original_cwd.
		if _, hasAction := sc["action"]; hasAction {
			if cwd, ok := sc["original_cwd"].(string); ok && cwd != "" {
				newCWD = cwd
			}
			continue
		}
		// EnterWorktree: worktree_branch key is present, new cwd is worktree_path.
		if _, hasBranch := sc["worktree_branch"]; hasBranch {
			if path, ok := sc["worktree_path"].(string); ok && path != "" {
				newCWD = path
			}
		}
	}
	return newCWD
}
