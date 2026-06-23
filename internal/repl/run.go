package repl

import (
	"context"
	"encoding/json"
	"strings"

	"ccgo/internal/config"
	"ccgo/internal/contracts"
	"ccgo/internal/conversation"
	"ccgo/internal/mcp"
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
		// Apply pending model switch from /fast or /model picker (CMD-FAST-01).
		// modelRef is set by RunInteractiveWithOptions when onModelChange fires.
		if loop.modelRef != nil && *loop.modelRef != "" {
			base.Model = *loop.modelRef
		}
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
	// AgentRegistry is the shared background-task registry. When non-nil the
	// loop polls it between turns and surfaces finished agents as system messages.
	// The production wiring in main.go creates one shared instance and passes the
	// same pointer to both the task tool (via runner.AgentRegistry) and the REPL.
	AgentRegistry agentRegistryHarvester

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

	// OnModelChange, when non-nil, is called each time the user switches model
	// (via /fast or /model picker). The caller can use this to keep any
	// out-of-REPL bookkeeping (e.g. runner.Model in main.go) in sync.
	// The REPL loop itself handles within-session model switching via modelRef.
	// CMD-FAST-01.
	OnModelChange func(string)

	// OnTurnResult, when non-nil, is called after each successful turn with the
	// full turn Result. Production code wires this to accumulate cost
	// (result.Usage) into runner.AccumulatedUsage for COST-02 persistence.
	OnTurnResult func(conversation.Result)

	// MCPApprovalPath is the local settings file path where project-scope MCP
	// trust decisions (from MCPServerApprovalDialog) are persisted.
	// When non-empty, overlay submissions matching "mcp:yes_all:*",
	// "mcp:yes:*", and "mcp:no:*" are written to this file.
	// CC ref: src/services/mcpServerApproval.tsx (F8-C04).
	MCPApprovalPath string

	// MCPManager, when non-nil, provides live connection status for the /mcp
	// slash panel. The panel shows connected/failed/disabled statuses from the
	// Manager instead of static configured-only data.
	// G11: live MCP connection manager wiring.
	MCPManager *mcp.Manager

	// ExtendedKeys, when true, enables the Kitty keyboard protocol
	// (ESC[>4m) so Shift+Enter and other composite key sequences are
	// recognised unambiguously. Sourced from mergedSettings at startup; should
	// only be enabled on terminals that advertise Kitty support.
	// REPL-60. CC ref: src/ink/ink.tsx:1430.
	ExtendedKeys bool

	// SyntaxHighlightingDisabled mirrors settings.SyntaxHighlightingDisabled.
	// When true, diff output is rendered without ANSI color codes.
	// CC ref: utils/settings/types.ts syntaxHighlightingDisabled.
	SyntaxHighlightingDisabled bool

	// SpinnerConfig carries render-affecting spinner settings (tips, verb).
	// Sourced from mergedSettings.SpinnerTipsEnabled / SpinnerVerbs at startup.
	// CC ref: src/services/tips/tipScheduler.ts spinnerTipsEnabled.
	SpinnerConfig SpinnerConfig

	// TerminalTitleFromRename, when true, causes /rename to also update the
	// terminal tab title via an OSC-0 sequence.
	// CC ref: utils/settings/types.ts terminalTitleFromRename.
	TerminalTitleFromRename bool

	// Theme selects the ANSI colour scheme for the status bar.
	// Mirrors settings.Theme: "dark" (default), "light", "dark-daltonism", "light-daltonism".
	// CC ref: utils/settings/types.ts theme.
	Theme string

	// StatusLineCommand is the shell command from settings.StatusLine.Command.
	// When non-empty, its stdout is used as the status bar content.
	// CC ref: utils/settings/types.ts statusLine:{type:"command",command:string}.
	StatusLineCommand string
}

// buildOverlaySubmitHandler composes a single overlay-submit handler that
// (a) persists MCP trust decisions to mcpApprovalPath when non-empty, and
// (b) delegates to the caller-supplied onOverlay hook (may be nil).
//
// MCP approval format: "mcp:yes_all:<serverName>", "mcp:yes:<serverName>",
// "mcp:no:<serverName>".  CC ref: src/services/mcpServerApproval.tsx.
func buildOverlaySubmitHandler(onOverlay func(string), mcpApprovalPath string) func(string) {
	if onOverlay == nil && mcpApprovalPath == "" {
		return nil
	}
	return func(submit string) {
		// Persist MCP trust decisions when we have a path to write to.
		if mcpApprovalPath != "" {
			tryApplyMCPApproval(mcpApprovalPath, submit)
		}
		if onOverlay != nil {
			onOverlay(submit)
		}
	}
}

// tryApplyMCPApproval parses an overlay submit string for mcp:* prefixes and
// calls config.ApplyMCPApproval.  Errors are silently discarded (best-effort).
func tryApplyMCPApproval(path string, submit string) {
	// Accepted formats:
	//   mcp:yes_all:<serverName>
	//   mcp:yes:<serverName>
	//   mcp:no:<serverName>
	for _, prefix := range []string{"mcp:yes_all:", "mcp:yes:", "mcp:no:"} {
		if strings.HasPrefix(submit, prefix) {
			serverName := submit[len(prefix):]
			if serverName == "" {
				return
			}
			var action config.MCPApprovalAction
			switch prefix {
			case "mcp:yes_all:":
				action = config.MCPApprovalYesAll
			case "mcp:yes:":
				action = config.MCPApprovalYes
			case "mcp:no:":
				action = config.MCPApprovalNo
			}
			_ = config.ApplyMCPApproval(path, action, serverName) // best-effort
			return
		}
	}
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
		// Apply pending model switch from /fast or /model picker (CMD-FAST-01).
		if loop.modelRef != nil && *loop.modelRef != "" {
			base.Model = *loop.modelRef
		}
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
	return newProductionRouterWithRegistry(cwd, registry, nil)
}

// newProductionRouterWithRegistry is like newProductionRouter but also wires an
// AgentRegistry so that /tasks and /bashes read live background-task state.
func newProductionRouterWithRegistry(cwd string, registry []contracts.Command, agReg agentRegistrySnapshotter) *CommandRouter {
	return newProductionRouterFull(cwd, registry, agReg, nil)
}

// newProductionRouterFull is the full production router that wires an
// AgentRegistry and optionally a live MCPManager for the /mcp panel.
func newProductionRouterFull(cwd string, registry []contracts.Command, agReg agentRegistrySnapshotter, mcpMgr *mcp.Manager) *CommandRouter {
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
	// CMD-CONFIG-01: register /config on the REPL router so it doesn't fall
	// through to the model. Returns a text summary of key settings.
	router.Register("config", configHandler(func() contracts.Settings {
		s, _ := config.LoadSettingsFile(config.UserSettingsPath())
		return s
	}, cwd))
	// CMD-PLUGIN-01: register /plugin on the REPL router so it doesn't fall
	// through to the model. Returns a text summary of configured plugins.
	router.Register("plugin", pluginHandler(func() contracts.Settings {
		s, _ := config.LoadSettingsFile(config.UserSettingsPath())
		return s
	}))
	router.Register("plugins", pluginHandler(func() contracts.Settings {
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
	// /tasks and /bashes: backed by the shared AgentRegistry when available.
	router.Register("tasks", tasksHandlerWithRegistry(agReg))
	router.Register("bashes", tasksHandlerWithRegistry(agReg))
	router.Register("keybindings", keybindingsHandler(""))
	router.Register("reload-plugins", reloadPluginsHandler(cwd))
	router.Register("color", colorHandler())
	router.Register("statusline", statusLineHandler())
	// AUTH-LOGIN-01/02: /login and /logout run the OAuth flow / clear creds.
	router.Register("login", loginHandler())
	router.Register("logout", logoutHandler())
	// /mcp: shows live MCP server status from the Manager when available.
	// G11: live connection manager wired here.
	router.Register("mcp", mcpHandlerWith(mcpMgr))
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
	loop.onOverlaySubmit = buildOverlaySubmitHandler(opts.OnOverlay, opts.MCPApprovalPath)
	if opts.CustomKeymap != nil {
		loop.screen.Keymap = *opts.CustomKeymap
	}
	if opts.EditorMode == "vim" {
		loop.screen.SetVimEnabled(true)
		loop.refreshBaseStatus()
	}
	// REPL-60: enable Kitty keyboard protocol when the caller requests it.
	if opts.ExtendedKeys {
		loop.SetExtendedKeys(true)
	}
	// CFG-35: wire syntax highlighting preference. Color is enabled by default;
	// disabled when settings.SyntaxHighlightingDisabled=true.
	loop.SetSyntaxHighlightColor(!opts.SyntaxHighlightingDisabled)
	// CFG-37: wire spinner tips/verb settings from mergedSettings.
	loop.SetSpinnerConfig(opts.SpinnerConfig)
	// CFG-51: wire theme so the status bar uses the appropriate colour scheme.
	if opts.Theme != "" {
		loop.screen.Theme = opts.Theme
	}
	// CFG-19: wire statusLine command so its stdout populates the status bar.
	if opts.StatusLineCommand != "" {
		loop.SetStatusLineCommand(opts.StatusLineCommand)
	}
	if opts.Trust != nil {
		loop.activeOverlay = NewTrustDialog(*opts.Trust)
	}

	// OVL-05/06: Wire the working directory so the QuickOpen overlay can walk
	// project files when the user types "@" in the prompt.
	if base.WorkingDirectory != "" {
		loop.SetCWD(base.WorkingDirectory)
	}

	// OVL-07: Wire prompt history entries (display strings) so the HistorySearch
	// overlay (Ctrl+Q) can fuzzy-search prior prompts.
	if len(opts.PromptHistory) > 0 {
		displays := make([]string, 0, len(opts.PromptHistory))
		for _, e := range opts.PromptHistory {
			if e.Display != "" {
				displays = append(displays, e.Display)
			}
		}
		loop.SetPromptHistory(displays)
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

	// CMD-FAST-01: wire model-switch seam so /fast and /model picker actually
	// update the runner model for subsequent turns. A shared modelVal is set by
	// onModelChange and applied by StartTurn before each r := base copy.
	var modelVal string
	loop.modelRef = &modelVal
	loop.onModelChange = func(m string) {
		modelVal = m
		if opts.OnModelChange != nil {
			opts.OnModelChange(m)
		}
	}

	// Wire the AgentRegistry (if provided) into the loop so it can poll for
	// completed background agents between turns and surface them to the user.
	if opts.AgentRegistry != nil {
		loop.SetAgentRegistry(opts.AgentRegistry)
	}

	// Wire the command router so /resume (and future live-effect commands) are
	// handled without falling through to the model.
	// G11: pass MCPManager so /mcp shows live connection status.
	router := newProductionRouterFull(base.WorkingDirectory, opts.Registry, opts.AgentRegistry, opts.MCPManager)

	// CMD-FAST-01: re-register /fast with a real model setter that calls
	// onModelChange so the switch actually takes effect on subsequent turns
	// (production wiring — overrides the nil-setter stub in the default router).
	router.Register("fast", fastHandlerWith(func(m string) error {
		loop.onModelChange(m)
		return nil
	}))

	// CMD-BRANCH-01: re-register /branch with a real forker that forks the
	// current session transcript. When the session ID or working dir is empty,
	// branchHandlerWith falls back to the nil-forker info message.
	if base.SessionID != "" && base.WorkingDirectory != "" {
		srcID := base.SessionID
		root := base.WorkingDirectory
		router.Register("branch", branchHandlerWith(func(title string) (sessionForkerResult, error) {
			result, err := session.ForkSession(srcID, root, title)
			if err != nil {
				return sessionForkerResult{}, err
			}
			return sessionForkerResult{SessionID: result.SessionID, Title: result.Title}, nil
		}, srcID))
	}

	// CFG-36: re-register /rename with the terminal-title seam when
	// terminalTitleFromRename is enabled. The loop's titleWriter writes an
	// OSC-0 sequence to the terminal. Production wiring below.
	var titleSetterForRename func(string)
	if opts.TerminalTitleFromRename {
		titleSetterForRename = func(name string) {
			loop.setTerminalTitle(name)
		}
	}
	router.Register("rename", renameHandlerWithTitle(
		writeCustomTitle,
		base.SessionID,
		base.WorkingDirectory,
		opts.TerminalTitleFromRename,
		titleSetterForRename,
	))

	// CMD-FAST-01: extend onOverlaySubmit to handle "model:<name>" from the
	// /model picker overlay. The loop already routes model: to onOverlaySubmit;
	// we intercept it here to call onModelChange so the switch takes effect.
	prevOverlaySubmit := loop.onOverlaySubmit
	loop.onOverlaySubmit = func(submit string) {
		if strings.HasPrefix(submit, "model:") {
			name := strings.TrimPrefix(submit, "model:")
			if name != "" {
				loop.onModelChange(name)
			}
		}
		if prevOverlaySubmit != nil {
			prevOverlaySubmit(submit)
		}
	}

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

	// COST-02: wire turn-result callback so each successful turn's usage is
	// forwarded to the caller (main.go accumulates into runner.AccumulatedUsage).
	if opts.OnTurnResult != nil {
		loop.onTurnResult = opts.OnTurnResult
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
