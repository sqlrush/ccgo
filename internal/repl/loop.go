package repl

import (
	"bufio"
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"strings"
	"time"

	"ccgo/internal/contracts"
	"ccgo/internal/conversation"
	"ccgo/internal/orchestration"
	"ccgo/internal/session"
	"ccgo/internal/tool"
	"ccgo/internal/tui"
)

// ExitAlternateMarker is the leading bytes of the alt-screen exit sequence;
// used by tests to confirm clean teardown.
const ExitAlternateMarker = "\x1b[?1049l"

type askRequest struct {
	req   tool.PermissionAskRequest
	reply chan contracts.PermissionDecision
}

type turnOutcome struct {
	result conversation.Result
	err    error
}

// ruleWriter persists a permission-rule update. settingswriter.Writer satisfies it.
type ruleWriter interface {
	Apply(update contracts.PermissionUpdate) error
}

// Loop is the terminal runtime that drives the existing tui.REPLScreen.
type Loop struct {
	term   Terminal
	screen tui.REPLScreen
	life   tui.ScreenLifecycle
	dialog *tui.DialogRuntime

	inputCh        chan tui.Key
	eventCh        chan conversation.Event
	askCh          chan askRequest
	questionCh     chan questionRequest
	elicitationCh  chan elicitationPending
	doneCh         chan turnOutcome
	resizeCh       chan resizeEvent
	tickCh     <-chan time.Time
	stopTick   func()
	spinner    Spinner
	baseStatus string

	// StartTurn is invoked when the user submits a prompt. It runs the model
	// turn (typically in a goroutine) and posts to eventCh/askCh/doneCh.
	StartTurn func(input string)

	history   []contracts.Message
	activeAsk *askRequest
	askQueue  []askRequest

	// activeQuestion, when non-nil, holds the pending question whose reply
	// channel is waiting. It is set by showQuestion and cleared when the
	// overlay submits or is dismissed.
	activeQuestion *questionRequest

	// activeElicitation, when non-nil, holds the pending elicitation whose reply
	// channel is waiting. Cleared when the overlay submits or is dismissed.
	// G29: MCP-34/35 elicitation bridge.
	activeElicitation *elicitationPending

	// activeOverlay, when non-nil, receives all key events before normal
	// prompt handling.  Cleared when the overlay submits or is dismissed.
	activeOverlay Overlay

	// registry holds the slash-command list used to populate the slash menu.
	// Set via SetRegistry; nil means slash-menu is disabled.
	registry []contracts.Command

	// lastToolUse tracks the most recent EventToolUse so that the subsequent
	// EventToolResult can be rendered with the richer diff output.
	lastToolUse *contracts.ToolUse

	// settings is the optional writer for persisting "allow always" rules.
	// Set via SetSettingsWriter; nil in tests that don't exercise persistence.
	settings ruleWriter

	// onPermissionShown is a test seam; nil in production. Called at the end of
	// showPermission so tests can synchronize input delivery after the dialog is
	// rendered.
	onPermissionShown func()

	// onQuestionShown is a test seam; nil in production. Called at the end of
	// showQuestion so tests can synchronize input delivery after the overlay is
	// rendered.
	onQuestionShown func()

	// onElicitationShown is a test seam; nil in production. Called at the end of
	// showElicitation so tests can synchronize input delivery after the overlay
	// is rendered. G29: MCP-34/35 elicitation bridge.
	onElicitationShown func()

	// onPermissionAskNotify, when non-nil, is called in a goroutine each time a
	// permission dialog is displayed. Production code wires this to
	// runner.RunNotificationHooks (HOOK-35: Notification fires when idle/awaiting
	// permission). Errors are intentionally discarded to keep the REPL alive.
	onPermissionAskNotify func(toolName string)

	// onTurnDone is a test seam; nil in production. Called at the end of
	// finishTurn so tests can synchronize after the turn completes and history
	// is updated (mirrors onPermissionShown).
	onTurnDone func()

	// onTurnResult, when non-nil, is called with the successful turn result
	// after history is updated. Used by RunInteractiveWithOptions to accumulate
	// cost across turns for COST-02 persistence.
	onTurnResult func(conversation.Result)

	// onTurnDoneNotify, when non-nil, is called after a successful turn when
	// the terminal is unfocused (screen.Focused == false). Production wires
	// this to emit OSC 9/99/777 terminal notification sequences (OVL-42).
	// Tests may wire a recorder to verify the seam fires.
	onTurnDoneNotify func(unfocused bool)

	// onRulePersisted is called for each PermissionUpdate successfully written
	// by an "allow always" choice. In production RunInteractiveWithOptions wires
	// this to refresh the live engine; it also serves as a test seam.
	onRulePersisted func(contracts.PermissionUpdate)

	// onModeChange is called after Shift+Tab cycles the permission mode.
	// RunInteractiveWithOptions wires this to apply a "setMode" update to the
	// live engine pointer so subsequent turns use the new mode.
	onModeChange func(contracts.PermissionMode)

	// onModelChange is a seam called when a command outcome requests a model
	// switch (e.g. /fast switches to Haiku). Nil in production until wired.
	onModelChange func(model string)

	// modelRef, when non-nil, holds the current model override. StartTurn reads
	// *modelRef before copying base so model switches (via /fast or /model picker)
	// take effect on the next turn without rebuilding the runner (CMD-FAST-01).
	modelRef *string

	// titleWriter is a seam for writing OSC-0 terminal title sequences.
	// When non-nil it is called instead of writing directly to the terminal.
	// Nil in tests that don't exercise title writes.
	titleWriter func(title string)

	// thinkingActive tracks whether a thinking_delta streaming sequence is
	// currently in progress. Reset in finishTurn.
	thinkingActive bool

	// onOverlaySubmit is a host/test seam called when an overlay submission is
	// handled internally (resume:/theme:/memory:/trust: prefixes). Nil in tests
	// that don't exercise the overlay action routing.
	onOverlaySubmit func(string)

	// onElicitationReply, when non-nil, is called with the "elicitation:<action>"
	// submit token so the blocking ElicitationPrompt goroutine can receive the reply.
	// Set by the production wiring that bridges the overlay to the MCP client.
	onElicitationReply func(string)

	// onCommand is a test/host seam for routing live-effect slash commands.
	// When non-nil, it is called before StartTurn for every prompt submission.
	// If it returns (outcome, true), the outcome is applied and the model is
	// not called. Production code wires a CommandRouter.Dispatch closure here.
	onCommand func(input string) (CommandOutcome, bool)

	running    bool
	turnCancel context.CancelFunc
	width      int
	height     int

	// mode is the current permission mode, cycled by Shift+Tab.
	mode contracts.PermissionMode

	// streamingBuf accumulates assistant text from text_delta / thinking_delta
	// EventStreamEvent events so the screen shows incremental output (REPL-21).
	// Cleared when EventAssistantMessage arrives (the full message supersedes it).
	streamingBuf    string
	streamingActive bool // true while a streaming assistant message is live on screen

	// agentReg is the shared AgentRegistry for background task tracking.
	// Set via SetAgentRegistry; nil when background tasks are not in use.
	agentReg agentRegistryHarvester

	// bgCheckCh carries polling ticks that prompt the loop to check whether any
	// background agents have finished. Sent from a goroutine when the registry
	// transitions an agent to done/failed.
	bgCheckCh chan struct{}

	// onBGNotice is a test seam called (in the loop goroutine) each time a
	// background-agent completion notice is written to the screen. Nil in production.
	onBGNotice func(msg string)

	// cwd is the working directory used by QuickOpenOverlay when the user presses
	// "@" in the prompt. Set via SetCWD; defaults to "" (overlay disabled).
	cwd string

	// searchRoot is the root directory for GlobalSearchOverlay (OVL-08).
	// Defaults to cwd when set via SetCWD; overridable via SetSearchRoot.
	// Empty string disables the global search overlay.
	searchRoot string

	// promptHistoryEntries is the flattened list of prior prompt entries (display
	// text) used to seed HistorySearchOverlay (OVL-07). Set via SetPromptHistory;
	// nil when history search is disabled.
	promptHistoryEntries []string

	// extendedKeys, when true, enables the Kitty keyboard protocol (REPL-60).
	// Set via SetExtendedKeys before Run; controls whether ExtendedKeys=true is
	// passed to tui.ScreenLifecycle.EnterInteractive.
	// CC ref: src/ink/ink.tsx:1430.
	extendedKeys bool

	// syntaxHighlightColor mirrors the inverse of settings.SyntaxHighlightingDisabled.
	// When false, diff output is rendered without ANSI color codes.
	// CC ref: utils/settings/types.ts syntaxHighlightingDisabled.
	syntaxHighlightColor bool

	// spinnerCfg holds render-affecting spinner settings (tips, verb).
	// Set via SetSpinnerConfig; zero value disables tips.
	spinnerCfg SpinnerConfig

	// statusLineCmd, when non-empty, is the shell command whose stdout is
	// used as the status bar content. Set via SetStatusLineCommand.
	// CC ref: utils/settings/types.ts statusLine:{type:"command",command:string}.
	statusLineCmd string

	// statusLineCmdRunner executes statusLineCmd and returns its output.
	// Defaults to RunStatusLineCommand; can be overridden in tests.
	statusLineCmdRunner func(cmd string) (string, error)

	// fileSuggestionCmd, when non-empty, is the shell command whose stdout (one
	// path per line) populates the QuickOpen overlay instead of walking the
	// filesystem. Set via SetFileSuggestionCmd.
	// CFG-40: CC ref: utils/settings/types.ts fileSuggestion:{type:"command",...}.
	fileSuggestionCmd string

	// lastEscTime records when the most recent Esc key was pressed.
	// Used by the ESC double-press detector (REPL-30) to open MessageSelector.
	lastEscTime time.Time
}

// escDoublePressWindow is the maximum interval between two ESC presses that
// triggers the MessageSelector overlay (REPL-30).
// CC ref: src/components/PromptInput/PromptInput.tsx:1254.
const escDoublePressWindow = 800 * time.Millisecond

// SetAgentRegistry wires the shared AgentRegistry for background task tracking.
// Once set, the loop polls the registry between turns and surfaces completed
// agents as system messages. Call before Run.
func (l *Loop) SetAgentRegistry(reg agentRegistryHarvester) {
	l.agentReg = reg
	if reg != nil {
		l.bgCheckCh = make(chan struct{}, 1)
	}
}

// AgentRegistry returns the registry wired via SetAgentRegistry (may be nil).
func (l *Loop) AgentRegistry() agentRegistryHarvester { return l.agentReg }

// SetSettingsWriter wires the settings writer used to persist "allow always"
// permission rules. Called from run.go during Task 13 wiring.
func (l *Loop) SetSettingsWriter(w ruleWriter) { l.settings = w }

// SetRegistry sets the command list used to populate the slash-command overlay.
// Call this from run.go once the command registry is loaded.
func (l *Loop) SetRegistry(cmds []contracts.Command) { l.registry = cmds }

// SetCWD sets the working directory used by QuickOpenOverlay when the user
// types "@" in the prompt (OVL-05/06). Must be called before Run.
// SetCWD sets the current working directory for QuickOpenOverlay and, when
// searchRoot has not been explicitly set, also seeds the global search root.
func (l *Loop) SetCWD(cwd string) {
	l.cwd = cwd
	if l.searchRoot == "" {
		l.searchRoot = cwd
	}
}

// SetSearchRoot overrides the root directory used by GlobalSearchOverlay
// (OVL-08). When empty the global search overlay is disabled.
func (l *Loop) SetSearchRoot(root string) { l.searchRoot = root }

// SetSyntaxHighlightColor sets whether diff output is rendered with ANSI color.
// Pass false when settings.SyntaxHighlightingDisabled=true.
// CC ref: utils/settings/types.ts syntaxHighlightingDisabled.
func (l *Loop) SetSyntaxHighlightColor(enabled bool) { l.syntaxHighlightColor = enabled }

// SetSpinnerConfig wires spinner tip/verb settings from mergedSettings.
// CC ref: src/services/tips/tipScheduler.ts spinnerTipsEnabled.
func (l *Loop) SetSpinnerConfig(cfg SpinnerConfig) { l.spinnerCfg = cfg }

// SetStatusLineCommand wires the shell command whose stdout is used as the
// status bar content. Set from mergedSettings.StatusLine.Command.
// CC ref: utils/settings/types.ts statusLine:{type:"command",command:string}.
func (l *Loop) SetStatusLineCommand(cmd string) { l.statusLineCmd = cmd }

// SetExtendedKeys enables or disables the Kitty keyboard protocol (REPL-60).
// When enabled, tui.TerminalModeOptions.ExtendedKeys=true is passed to
// EnterInteractive so the terminal receives ESC[>4m. Must be called before Run.
func (l *Loop) SetExtendedKeys(enabled bool) { l.extendedKeys = enabled }

// SetPromptHistory seeds the HistorySearchOverlay (OVL-07) with previously
// submitted prompt display strings (newest-first). Must be called before Run.
func (l *Loop) SetPromptHistory(entries []string) {
	copied := append([]string(nil), entries...)
	l.promptHistoryEntries = copied
}

func NewLoop(t Terminal, history []string) *Loop {
	w, h, err := t.Size()
	if err != nil || w <= 0 || h <= 0 {
		w, h = 80, 24
	}
	return &Loop{
		term:          t,
		screen:        tui.NewREPLScreen(w, h, history),
		dialog:        tui.NewDialogRuntime(),
		inputCh:       make(chan tui.Key, 64),
		eventCh:       make(chan conversation.Event, 256),
		askCh:         make(chan askRequest, 4),
		questionCh:    make(chan questionRequest, 4),
		elicitationCh: make(chan elicitationPending, 4),
		doneCh:        make(chan turnOutcome, 1),
		resizeCh:      make(chan resizeEvent, 1),
		width:         w,
		height:        h,
	}
}

// NewLoopFromHistoryEntries creates a Loop seeded with persisted prompt history
// entries. The entries back Up-arrow / Ctrl+R navigation from the first keystroke.
func NewLoopFromHistoryEntries(t Terminal, entries []session.HistoryEntry) *Loop {
	w, h, err := t.Size()
	if err != nil || w <= 0 || h <= 0 {
		w, h = 80, 24
	}
	return &Loop{
		term:          t,
		screen:        tui.NewREPLScreenFromHistoryEntries(w, h, entries),
		dialog:        tui.NewDialogRuntime(),
		inputCh:       make(chan tui.Key, 64),
		eventCh:       make(chan conversation.Event, 256),
		askCh:         make(chan askRequest, 4),
		questionCh:    make(chan questionRequest, 4),
		elicitationCh: make(chan elicitationPending, 4),
		doneCh:        make(chan turnOutcome, 1),
		resizeCh:      make(chan resizeEvent, 1),
		width:         w,
		height:        h,
	}
}

// Run blocks until the user exits, the stream ends, or ctx is cancelled.
func (l *Loop) Run(ctx context.Context) error {
	if !l.term.IsTTY() {
		return l.runLineMode(ctx)
	}

	restore, err := l.term.MakeRaw()
	if err != nil {
		return err
	}
	defer restore()
	defer l.denyPendingAsks()
	defer l.denyPendingQuestions()
	defer l.denyPendingElicitations()

	// REPL-60: ExtendedKeys enables the Kitty keyboard protocol (ESC[>4m) when
	// the terminal supports it. Sourced from InteractiveOptions.ExtendedKeys.
	opts := tui.TerminalModeOptions{BracketedPaste: true, FocusEvents: true, ExtendedKeys: l.extendedKeys}
	if err := l.term.WriteString(l.life.EnterInteractive(opts)); err != nil {
		return err
	}
	defer func() { _ = l.term.WriteString(l.life.ExitInteractive()) }()

	go l.readInput(ctx)
	startResizeListener(ctx, l.term, l.resizeCh)
	// REPL-56: SIGCONT → force redraw after process is resumed from Ctrl+Z.
	// CC ref: src/ink/ink.tsx:960.
	startSIGCONTListener(ctx, l.term, l.resizeCh)

	// When a background registry is wired, start a watcher goroutine that polls
	// every 500 ms and signals bgCheckCh when any agent finishes. The signal is
	// non-blocking (channel is buffered-1) so the watcher never blocks even if
	// the loop is busy.
	if l.agentReg != nil {
		go l.watchBackgroundAgents(ctx)
	}

	if err := l.render(); err != nil {
		return err
	}

	for {
		select {
		case <-ctx.Done():
			return nil
		case key, ok := <-l.inputCh:
			if !ok {
				return nil // input stream closed (EOF)
			}
			if l.handleKey(key) {
				return nil // exit requested
			}
			if err := l.render(); err != nil {
				return err
			}
		case ar := <-l.askCh:
			l.enqueueAsk(ar)
			if err := l.render(); err != nil {
				return err
			}
		case qr := <-l.questionCh:
			l.showQuestion(qr)
			if err := l.render(); err != nil {
				return err
			}
		case ep := <-l.elicitationCh:
			l.showElicitation(ep)
			if err := l.render(); err != nil {
				return err
			}
		case ev := <-l.eventCh:
			l.applyEvent(ev)
			if err := l.render(); err != nil {
				return err
			}
		case out := <-l.doneCh:
			l.finishTurn(out)
			if err := l.render(); err != nil {
				return err
			}
		case rev := <-l.resizeCh:
			l.applyResize(rev)
			if err := l.render(); err != nil {
				return err
			}
		case <-l.tickCh:
			l.tick()
			if err := l.render(); err != nil {
				return err
			}
		case <-l.bgCheckCh:
			l.drainFinishedAgents()
			if err := l.render(); err != nil {
				return err
			}
		}
	}
}

// watchBackgroundAgents polls the AgentRegistry at ~500 ms intervals and sends
// a non-blocking signal on bgCheckCh whenever at least one agent has finished.
// It runs in its own goroutine and exits when ctx is cancelled.
func (l *Loop) watchBackgroundAgents(ctx context.Context) {
	ticker := time.NewTicker(500 * time.Millisecond)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			if l.agentReg == nil {
				return
			}
			for _, s := range l.agentReg.Snapshot() {
				if s.State == orchestration.AgentDone || s.State == orchestration.AgentFailed {
					// Signal the main loop (non-blocking; one pending signal is enough).
					select {
					case l.bgCheckCh <- struct{}{}:
					default:
					}
					break
				}
			}
		}
	}
}

// drainFinishedAgents harvests every finished background agent from the registry
// and appends a system-message notice to the screen. Called in the loop goroutine
// so it is data-race free with other loop state.
func (l *Loop) drainFinishedAgents() {
	if l.agentReg == nil {
		return
	}
	for _, s := range l.agentReg.Snapshot() {
		if s.State != orchestration.AgentDone && s.State != orchestration.AgentFailed {
			continue
		}
		outcome, ok := l.agentReg.Harvest(s.ID)
		if !ok {
			continue
		}
		var msg string
		if s.State == orchestration.AgentFailed || outcome.Err != nil {
			errStr := "unknown error"
			if outcome.Err != nil {
				errStr = outcome.Err.Error()
			}
			msg = fmt.Sprintf("background task %s failed: %s", s.ID, errStr)
		} else {
			msg = fmt.Sprintf("background task %s completed", s.ID)
			if outcome.Summary != "" {
				msg += ": " + outcome.Summary
			}
		}
		l.screen.AppendMessage(tui.Message{Role: tui.RoleSystem, Text: msg})
		if l.onBGNotice != nil {
			l.onBGNotice(msg)
		}
	}
}

// applyEvent renders a single conversation event to the screen transcript.
func (l *Loop) applyEvent(ev conversation.Event) {
	if ev.Type == conversation.EventToolUse {
		l.lastToolUse = ev.ToolUse
	}
	if ev.Type == conversation.EventToolResult {
		text := RenderToolResultTextWithColorOpt(l.lastToolUse, ev.ToolResult, l.syntaxHighlightColor)
		l.screen.AppendMessage(tui.Message{Role: tui.RoleTool, Text: text})
		return
	}
	// REPL-21: accumulate text_delta / thinking_delta events into a live
	// streaming assistant message rather than waiting for the full
	// EventAssistantMessage. The screen message is updated in-place on each
	// delta so the user sees incremental output as tokens arrive.
	if ev.Type == conversation.EventStreamEvent {
		l.applyStreamingDelta(ev)
		return
	}
	// When the final EventAssistantMessage arrives, clear the streaming state
	// so the completed message replaces the live placeholder cleanly.
	if ev.Type == conversation.EventAssistantMessage {
		if l.streamingActive {
			l.streamingBuf = ""
			l.streamingActive = false
		}
	}
	if msg, ok := messageFromEvent(ev); ok {
		l.screen.AppendMessage(msg)
	}
}

// applyStreamingDelta handles an EventStreamEvent by extracting any text or
// thinking delta and appending it to the live streaming buffer. On the first
// delta it appends a new assistant message; on subsequent deltas it updates
// that message in place. Non-delta events (message_start, content_block_stop,
// etc.) are silently ignored at the render layer (REPL-21).
func (l *Loop) applyStreamingDelta(ev conversation.Event) {
	if ev.StreamEvent == nil {
		return
	}
	se := ev.StreamEvent
	if se.Type != "content_block_delta" || se.Delta == nil {
		return
	}
	var chunk string
	switch se.Delta["type"] {
	case "text_delta":
		chunk, _ = se.Delta["text"].(string)
		// Exiting thinking mode when text tokens arrive (REPL-23).
		if l.thinkingActive {
			l.exitThinkingMode()
		}
	case "thinking_delta":
		chunk, _ = se.Delta["thinking"].(string)
		// Entering thinking mode on first thinking delta (REPL-23).
		if !l.thinkingActive {
			l.enterThinkingMode()
		}
	}
	if chunk == "" {
		return
	}
	l.streamingBuf += chunk
	msg := tui.Message{Role: tui.RoleAssistant, Text: l.streamingBuf}
	if !l.streamingActive {
		l.screen.AppendMessage(msg)
		l.streamingActive = true
	} else {
		l.screen.UpdateLastMessage(msg)
	}
}

// finishTurn handles turn completion: updates history on success or shows an
// error message on failure, then clears the running flag.
func (l *Loop) finishTurn(out turnOutcome) {
	l.running = false
	l.thinkingActive = false
	l.stopSpinner()
	l.setTerminalTitle("")
	if out.err != nil {
		if !errors.Is(out.err, context.Canceled) {
			l.screen.AppendMessage(tui.Message{Role: tui.RoleSystem, Text: out.err.Error()})
		}
		return
	}
	newHistory := make([]contracts.Message, len(l.history)+len(out.result.Messages))
	copy(newHistory, l.history)
	copy(newHistory[len(l.history):], out.result.Messages)
	l.history = newHistory
	if l.onTurnResult != nil {
		l.onTurnResult(out.result)
	}
	if l.onTurnDoneNotify != nil {
		// OVL-42: fire the notification seam; pass whether the terminal is
		// unfocused so production can send OSC notification only when needed.
		// Called synchronously here; the production lambda in RunInteractiveWithOptions
		// is cheap (string concat + WriteString) so no goroutine is needed.
		l.onTurnDoneNotify(!l.screen.Focused)
	}
	if l.onTurnDone != nil {
		l.onTurnDone()
	}
}

// readInput segments the terminal byte stream into keys and posts them.
// NOTE: when the tty is closed this goroutine may remain blocked inside
// OSTerminal.Read / os.Stdin.Read, which is a blocking syscall not preemptable
// by ctx cancellation. This is benign for cmd/claude (the process exits
// immediately after Run returns), but a long-lived host embedding RunInteractive
// would leak this goroutine — mirrors the cancel-limitation noted in
// runLineMode above.
func (l *Loop) readInput(ctx context.Context) {
	defer close(l.inputCh)
	scanner := NewSequenceScanner(readerFunc(l.term.Read))
	for {
		seq, err := scanner.Next()
		if err != nil {
			return
		}
		select {
		case l.inputCh <- tui.ParseKey(seq):
		case <-ctx.Done():
			return
		}
	}
}

// handleKey applies one key to the screen and acts on the resulting event.
// It returns true when the loop should exit.
func (l *Loop) handleKey(key tui.Key) bool {
	// Route keys to the active overlay before any other handling.
	if l.activeOverlay != nil {
		res, handled := l.activeOverlay.ApplyKey(key)
		if handled {
			if res.Dismissed {
				l.activeOverlay = nil
				// If a question overlay was dismissed (Esc), cancel the pending
				// question so the asker goroutine is not left blocked.
				if l.activeQuestion != nil {
					l.activeQuestion.reply <- questionReply{err: fmt.Errorf("question dismissed")}
					l.activeQuestion = nil
				}
			} else if res.Submit != "" {
				l.activeOverlay = nil
				if handled := l.handleOverlaySubmit(res.Submit); !handled {
					if l.StartTurn != nil && !l.running {
						l.running = true
						l.startSpinner()
						l.StartTurn(res.Submit)
					}
				}
			}
			return false
		}
	}

	// OVL-07: Intercept Ctrl+Q before the screen sees it to open the
	// HistorySearchOverlay (fuzzy history search dialog). Ctrl+Q is parsed by the
	// input layer (KeyCtrlQ) but not bound in the default keymap, making it a safe
	// trigger. Only opens when prompt history is loaded.
	if key.Type == tui.KeyCtrlQ && l.activeOverlay == nil && len(l.promptHistoryEntries) > 0 {
		l.activeOverlay = NewHistorySearchOverlay(l.promptHistoryEntries)
		return false
	}

	// Shift+Tab cycles the permission mode and refreshes the status line.
	// Intercept before screen.ApplyKey so the screen does not consume the key.
	// Security gate: switching to BypassPermissions or Auto requires explicit
	// confirmation via a dialog; the mode change is deferred until the user
	// accepts (handleOverlaySubmit routes "bypass:accept" and "auto:accept").
	if key.Type == tui.KeyShiftTab {
		next := cycleMode(l.mode)
		switch next {
		case contracts.PermissionBypassPermissions:
			l.activeOverlay = NewBypassConfirmDialog()
		case contracts.PermissionAuto:
			l.activeOverlay = NewAutoModeOptInDialog()
		default:
			l.applyModeChange(next)
		}
		return false
	}

	// REPL-30: ESC double-press on empty prompt opens MessageSelector overlay.
	// CC ref: src/components/PromptInput/PromptInput.tsx:1254.
	if key.Type == tui.KeyEsc && l.activeOverlay == nil && l.screen.Prompt.Text == "" {
		now := time.Now()
		if !l.lastEscTime.IsZero() && now.Sub(l.lastEscTime) <= escDoublePressWindow && len(l.history) > 0 {
			// Build text entries from history messages.
			screenMsgs := historyToScreen(l.history)
			entries := make([]string, 0, len(screenMsgs))
			for _, msg := range screenMsgs {
				if msg.Text != "" {
					entries = append(entries, msg.Text)
				}
			}
			if len(entries) > 0 {
				l.activeOverlay = NewMessageSelectorOverlay(entries)
				l.lastEscTime = time.Time{}
				return false
			}
		}
		l.lastEscTime = now
	}

	event := l.screen.ApplyKey(key)

	// Open the slash menu when the prompt text is exactly "/" (first keystroke).
	if l.activeOverlay == nil && l.registry != nil && l.screen.Prompt.Text == "/" {
		l.activeOverlay = NewSlashMenu(l.registry, "")
	}

	// OVL-05/06: Open the QuickOpen file picker when the prompt text is exactly "@".
	// The overlay inserts the selected path back into the prompt with an @ prefix
	// (mention) or as a plain path (Tab). Requires cwd to be set.
	// CFG-40: when fileSuggestionCmd is set, populate the overlay with command output
	// instead of walking the filesystem. Falls back to filesystem walk on error.
	if l.activeOverlay == nil && l.cwd != "" && l.screen.Prompt.Text == "@" {
		if paths := l.fileSuggestionFiles(); paths != nil {
			l.activeOverlay = newQuickOpenOverlayWithFiles(paths)
		} else {
			l.activeOverlay = NewQuickOpenOverlay(l.cwd)
		}
		// Clear the "@" trigger character so the overlay starts clean.
		l.screen.Prompt.Text = ""
	}

	if l.activeAsk != nil &&
		(event.Type == tui.ScreenEventDialogAction || event.Type == tui.ScreenEventCancelled) {
		result := l.dialog.ResolveScreenEvent(&l.screen, event, l.screen.Status)
		if result.Found {
			var decision contracts.PermissionDecision
			if result.Status == tui.DialogResultCancelled || result.Status == tui.DialogResultDenied {
				decision = contracts.PermissionDecision{Behavior: contracts.PermissionDeny}
			} else {
				decision = decisionForAction(l.activeAsk.req, result.Action)
				l.persistDecision(decision)
			}
			l.activeAsk.reply <- decision
			l.activeAsk = nil
			l.showNext()
		}
		return false
	}

	switch event.Type {
	case tui.ScreenEventExit:
		return true
	case tui.ScreenEventInterrupted:
		l.interruptTurn()
	case tui.ScreenEventPromptSubmitted:
		// Ignore empty/whitespace-only submissions and in-flight turns silently.
		// l.running is only accessed in the loop goroutine, so no lock is needed.
		if l.StartTurn == nil || l.running || strings.TrimSpace(event.Value) == "" {
			break
		}
		if l.onCommand != nil {
			if outcome, handled := l.onCommand(event.Value); handled {
				if l.applyCommandOutcome(outcome) {
					return true // /exit or /quit requested clean loop exit
				}
				break
			}
		}
		// REPL-49: "!" prefix mode — treat the rest as a bash command to run.
		// CC ref: src/components/PromptInput/inputModes.ts (bash prefix).
		input := event.Value
		if strings.HasPrefix(input, "!") {
			cmd := strings.TrimSpace(strings.TrimPrefix(input, "!"))
			if cmd != "" {
				input = "Run the following bash command: " + cmd
			}
		}
		l.running = true
		l.StartTurn(input)
		l.startSpinner()

	case tui.ScreenEventStashPrompt:
		// Stash/unstash was already applied by screen.ApplyKey (applyStashPrompt).
		// This case exists so the event is acknowledged, not silently dropped.

	case tui.ScreenEventToggleTranscript:
		// The screen manages its own transcript-visibility flag; this case exists
		// so the event is handled rather than dropped.

	case tui.ScreenEventFocusIn:
		l.screen.Focused = true

	case tui.ScreenEventFocusOut:
		l.screen.Focused = false

	case tui.ScreenEventExternalEditor:
		l.launchExternalEditor(event.Value)

	case tui.ScreenEventGlobalSearch:
		// OVL-08: open GlobalSearchOverlay (Ctrl+\). Only opens when
		// searchRoot is set and no other overlay is active.
		if l.activeOverlay == nil && l.searchRoot != "" {
			l.activeOverlay = NewGlobalSearchOverlay(l.searchRoot)
		}
	}
	return false
}

// launchExternalEditor opens $EDITOR (falling back to $VISUAL, then "vi") with a
// temp file seeded with draft. On success the prompt text is replaced with the
// edited content. Errors are surfaced as a system message; they never abort the
// loop.
func (l *Loop) launchExternalEditor(draft string) {
	editor := os.Getenv("EDITOR")
	if editor == "" {
		editor = os.Getenv("VISUAL")
	}
	if editor == "" {
		editor = "vi"
	}
	tmp, err := os.CreateTemp("", "ccgo-prompt-*.txt")
	if err != nil {
		l.screen.AppendMessage(tui.Message{Role: tui.RoleSystem, Text: "external editor: " + err.Error()})
		return
	}
	defer func() { _ = os.Remove(tmp.Name()) }()
	if _, err := tmp.WriteString(draft); err != nil {
		_ = tmp.Close()
		l.screen.AppendMessage(tui.Message{Role: tui.RoleSystem, Text: "external editor write: " + err.Error()})
		return
	}
	_ = tmp.Close()

	// Restore terminal before handing off; re-enter interactive after.
	_ = l.term.WriteString(l.life.ExitInteractive())
	cmd := exec.Command(editor, tmp.Name())
	cmd.Stdin = os.Stdin
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	runErr := cmd.Run()
	opts := tui.TerminalModeOptions{BracketedPaste: true, FocusEvents: true, ExtendedKeys: l.extendedKeys}
	_ = l.term.WriteString(l.life.EnterInteractive(opts))
	if runErr != nil {
		l.screen.AppendMessage(tui.Message{Role: tui.RoleSystem, Text: "external editor: " + runErr.Error()})
		return
	}
	content, err := os.ReadFile(tmp.Name())
	if err != nil {
		l.screen.AppendMessage(tui.Message{Role: tui.RoleSystem, Text: "external editor read: " + err.Error()})
		return
	}
	l.screen.Prompt.Text = strings.TrimRight(string(content), "\n")
}

// applyCommandOutcome applies a handled live-effect command's result to the
// screen and history without sending anything to the model. It returns true
// when the loop should exit immediately (outcome.Exit is set).
func (l *Loop) applyCommandOutcome(outcome CommandOutcome) bool {
	if outcome.ReplaceHistory {
		l.history = outcome.NewHistory
		l.screen.SetMessages(historyToScreen(l.history))
	}
	if outcome.Status != "" {
		l.screen.AppendMessage(tui.Message{Role: tui.RoleSystem, Text: outcome.Status})
	}
	if outcome.Overlay != nil {
		l.activeOverlay = outcome.Overlay
	}
	if outcome.NewMode != "" {
		// slash commands that set a mode (e.g. /plan) bypass the Shift+Tab
		// confirmation dialogs intentionally — they are explicit user commands.
		l.applyModeChange(outcome.NewMode)
	}
	if outcome.NewModel != "" && l.onModelChange != nil {
		l.onModelChange(outcome.NewModel)
	}
	return outcome.Exit
}

// setTerminalTitle writes an OSC-0 terminal title sequence. If titleWriter is
// wired it is called; otherwise the sequence is written directly to the terminal
// via WriteString. Empty title clears the title bar.
func (l *Loop) setTerminalTitle(title string) {
	var seq string
	if title == "" {
		seq = tui.ClearTerminalTitleSequence()
	} else {
		seq = tui.TerminalTitleSequence(title)
	}
	if l.titleWriter != nil {
		l.titleWriter(seq)
		return
	}
	_ = l.term.WriteString(seq)
}

// enterThinkingMode switches the spinner to thinking mode.
func (l *Loop) enterThinkingMode() {
	l.spinner = l.spinner.WithThinkingMode(time.Now())
	l.thinkingActive = true
}

// exitThinkingMode resets the spinner to normal working mode.
func (l *Loop) exitThinkingMode() {
	l.spinner = NewSpinner(l.spinner.start)
	l.thinkingActive = false
}

// enqueueAsk adds an ask to the active slot if empty, otherwise to the backlog.
func (l *Loop) enqueueAsk(ar askRequest) {
	if l.activeAsk == nil {
		l.showPermission(ar)
		return
	}
	l.askQueue = append(l.askQueue, ar)
}

// showNext promotes the next queued ask (if any) to active.
func (l *Loop) showNext() {
	if l.activeAsk != nil || len(l.askQueue) == 0 {
		return
	}
	next := l.askQueue[0]
	l.askQueue = l.askQueue[1:]
	l.showPermission(next)
}

// showPermission registers a permission dialog with the dialog runtime and
// applies it to the screen. onPermissionShown (if set) is called last so tests
// can gate input delivery until the dialog is visible.
func (l *Loop) showPermission(ar askRequest) {
	l.activeAsk = &ar
	actions := permissionActions(ar.req)
	// PERM-TOOL-02 (G24): enrich Description with tool-specific content so the
	// dialog shows the full command (Bash), URL domain (WebFetch), or path (Edit).
	l.dialog.RequestPermission(tui.PermissionRequest{
		ID:          string(ar.req.ToolUseID),
		ToolName:    ar.req.ToolName,
		Path:        ar.req.Path,
		Description: toolSpecificDialogContent(ar.req),
		Actions:     actions.Actions,
	})
	l.dialog.ApplyToScreen(&l.screen, l.screen.Status)
	// Fire Notification hook in background (HOOK-35: notification when idle/
	// awaiting permission). Errors are discarded to keep the REPL alive.
	if l.onPermissionAskNotify != nil {
		go l.onPermissionAskNotify(ar.req.ToolName)
	}
	if l.onPermissionShown != nil {
		l.onPermissionShown()
	}
}

// persistDecision applies any rule suggestions carried by an "always" choice:
// it writes the update via the settings writer and, only on a successful write,
// notifies the test seam. With no writer configured nothing is persisted and the
// seam does not fire, so onRulePersisted is an honest "rule was persisted" signal.
func (l *Loop) persistDecision(decision contracts.PermissionDecision) {
	for _, update := range decision.Suggestions {
		if l.settings == nil {
			continue
		}
		if err := l.settings.Apply(update); err != nil {
			l.screen.AppendMessage(tui.Message{
				Role: tui.RoleSystem,
				Text: "failed to save permission rule: " + err.Error(),
			})
			continue
		}
		if l.onRulePersisted != nil {
			l.onRulePersisted(update)
		}
	}
}

// denyPendingAsks unblocks every asker still waiting when the loop exits,
// so executor goroutines never hang. Drains the active ask, the queue, and
// anything still buffered in askCh, replying Deny to each.
func (l *Loop) denyPendingAsks() {
	deny := contracts.PermissionDecision{Behavior: contracts.PermissionDeny}
	if l.activeAsk != nil {
		l.activeAsk.reply <- deny
		l.activeAsk = nil
	}
	for _, ar := range l.askQueue {
		ar.reply <- deny
	}
	l.askQueue = nil
	for {
		select {
		case ar := <-l.askCh:
			ar.reply <- deny
		default:
			return
		}
	}
}

func (l *Loop) render() error {
	if l.activeOverlay != nil {
		lines := l.activeOverlay.Render(l.width, l.height)
		prefix := l.life.ReassertInteractive(tui.TerminalModeOptions{})
		return l.term.WriteString(prefix + strings.Join(lines, "\r\n") + "\r\n")
	}
	return l.term.WriteString(l.screen.Render())
}

// runLineMode is the non-tty fallback: read lines, submit each as a prompt.
func (l *Loop) runLineMode(ctx context.Context) error {
	reader := bufio.NewReader(readerFunc(l.term.Read))
	// NOTE: bufio ReadString blocks on the underlying reader; a ctx cancel mid-read is not preempted until the next newline or EOF. Acceptable for the non-tty fallback; the tty path (readInput) honors ctx.Done() promptly.
	for {
		select {
		case <-ctx.Done():
			return nil
		default:
		}
		line, err := reader.ReadString('\n')
		line = strings.TrimRight(line, "\r\n")
		if line != "" && l.StartTurn != nil {
			// REPL-49: "!" prefix routes input as a bash command.
			if strings.HasPrefix(line, "!") {
				if cmd := strings.TrimSpace(strings.TrimPrefix(line, "!")); cmd != "" {
					line = "Run the following bash command: " + cmd
				}
			}
			l.StartTurn(line)
		}
		if err == io.EOF {
			return nil
		}
		if err != nil {
			return err
		}
	}
}

// readerFunc adapts Terminal.Read to io.Reader.
type readerFunc func(p []byte) (int, error)

func (f readerFunc) Read(p []byte) (int, error) { return f(p) }

// SetMode sets the initial permission mode (called from run.go if needed).
func (l *Loop) SetMode(mode contracts.PermissionMode) {
	l.mode = mode
	l.refreshBaseStatus()
}

// applyModeChange sets the new mode, refreshes the status bar, and fires
// onModeChange. It is the single path through which all mode switches happen
// so that the security confirmation gate (bypass/auto dialogs) is the only
// other code path that can reach this.
func (l *Loop) applyModeChange(mode contracts.PermissionMode) {
	l.mode = mode
	l.refreshBaseStatus()
	if l.onModeChange != nil {
		l.onModeChange(l.mode)
	}
}

// handleOverlaySubmit consumes structured overlay results (resume:/theme:/
// memory:/trust:/question:/bypass:/auto:/mcp:/cost:/tokenwarn:/compact:/
// ctx:/notices:/idle:/worktree:/onboard:).
// It returns true when the submit was handled internally and should NOT be
// forwarded to the model. onOverlaySubmit is a host/test seam.
func (l *Loop) handleOverlaySubmit(submit string) bool {
	// Route question answers back to the waiting question asker.
	if selected, ok := decodeQuestionAnswer(submit); ok {
		if l.activeQuestion != nil {
			l.activeQuestion.reply <- questionReply{selected: selected}
			l.activeQuestion = nil
		}
		return true
	}

	// Security: bypass-permissions confirmation gate.
	// "bypass:accept" → actually perform the mode switch to bypassPermissions.
	// "bypass:decline" → silently discard; stay on current mode.
	if submit == "bypass:accept" {
		l.applyModeChange(contracts.PermissionBypassPermissions)
		return true
	}
	if submit == "bypass:decline" {
		// Mode switch was cancelled; nothing to do.
		return true
	}

	// Auto-mode opt-in gate.
	// "auto:accept" → switch to auto mode.
	// "auto:decline" → stay on current mode.
	if submit == "auto:accept" {
		l.applyModeChange(contracts.PermissionAuto)
		return true
	}
	if submit == "auto:decline" {
		return true
	}

	// OVL-05/06: QuickOpen file picker inserts a path into the prompt buffer.
	// "quickopen:<path>"        → "@<path> " (@ mention)
	// "quickopen-insert:<path>" → "<path> " (plain path)
	if handleQuickOpenSubmit(&l.screen.Prompt.Text, submit) {
		// Position cursor at the end of the inserted text.
		l.screen.Prompt.Cursor = len([]rune(l.screen.Prompt.Text))
		return true
	}

	// OVL-07: HistorySearch inserts the selected prompt into the prompt buffer.
	// "historysearch:<display>" → first-line of display text
	if handleHistorySearchSubmit(&l.screen.Prompt.Text, submit) {
		l.screen.Prompt.Cursor = len([]rune(l.screen.Prompt.Text))
		return true
	}

	// OVL-08: GlobalSearch submits "globalsearch:<file>:<line>". Insert the
	// file path into the prompt (as a @mention) for the user to act on.
	if handleGlobalSearchSubmit(&l.screen.Prompt.Text, submit) {
		l.screen.Prompt.Cursor = len([]rune(l.screen.Prompt.Text))
		return true
	}

	// OVL-52: ExportDialog submits "export:<filename>". Write the transcript
	// to <cwd>/<filename>.txt directly (side-effect in production; test seam
	// via l.onOverlaySubmit which is called with the status message).
	if strings.HasPrefix(submit, "export:") {
		name := strings.TrimPrefix(submit, "export:")
		cc := CommandContext{History: l.history}
		out, err := writeExport(cc, l.cwd, name)
		if err != nil {
			l.screen.AppendMessage(tui.Message{Role: tui.RoleSystem, Text: err.Error()})
		} else if out.Status != "" {
			l.screen.AppendMessage(tui.Message{Role: tui.RoleSystem, Text: out.Status})
		}
		if l.onOverlaySubmit != nil {
			l.onOverlaySubmit(submit)
		}
		return true
	}

	// Informational dismissals — all handled internally with no model call.
	for _, prefix := range []string{
		"cost:", "tokenwarn:", "compact:", "ctx:", "notices:", "idle:",
	} {
		if strings.HasPrefix(submit, prefix) {
			if l.onOverlaySubmit != nil {
				l.onOverlaySubmit(submit)
			}
			return true
		}
	}

	// G24: elicitation, config, perm overlay results are handled internally.
	// Route to onOverlaySubmit seam for test observation; never fall to model.
	for _, prefix := range []string{
		"elicitation:", "config:", "perm:",
	} {
		if strings.HasPrefix(submit, prefix) {
			if strings.HasPrefix(submit, "elicitation:") {
				// G29: deliver to the blocking loopElicitationPrompt goroutine.
				if l.activeElicitation != nil {
					l.activeElicitation.reply <- submit
					l.activeElicitation = nil
				}
				if l.onElicitationReply != nil {
					l.onElicitationReply(submit)
				}
			}
			if l.onOverlaySubmit != nil {
				l.onOverlaySubmit(submit)
			}
			return true
		}
	}

	// Overlay-action prefixes forwarded to the host seam (resume:/theme:/
	// memory:/trust:/model:/mcp:/worktree:/onboard:).
	for _, prefix := range []string{
		"resume:", "theme:", "memory:", "trust:", "model:",
		"mcp:", "worktree:", "onboard:",
	} {
		if strings.HasPrefix(submit, prefix) {
			if l.onOverlaySubmit != nil {
				l.onOverlaySubmit(submit)
			}
			return true
		}
	}
	return false // "/command" and plain text fall through to the model/command pipeline
}

// showQuestion sets activeOverlay to a questionOverlay and records the
// pending request so that the overlay submission can return the answer.
// onQuestionShown (if set) is called last so tests can gate input delivery.
func (l *Loop) showQuestion(qr questionRequest) {
	l.activeQuestion = &qr
	l.activeOverlay = newQuestionOverlay(qr.q)
	if l.onQuestionShown != nil {
		l.onQuestionShown()
	}
}

// denyPendingQuestions unblocks any waiting question asker goroutines when
// the loop exits. It cancels activeQuestion and drains questionCh.
func (l *Loop) denyPendingQuestions() {
	errVal := fmt.Errorf("question asker: loop exited")
	if l.activeQuestion != nil {
		l.activeQuestion.reply <- questionReply{err: errVal}
		l.activeQuestion = nil
	}
	for {
		select {
		case qr := <-l.questionCh:
			qr.reply <- questionReply{err: errVal}
		default:
			return
		}
	}
}

// elicitationPending holds a pending MCP elicitation/create request and the
// channel used to deliver the user's reply back to the blocking prompt goroutine.
// G29: MCP-34/35 elicitation bridge.
type elicitationPending struct {
	ov    *elicitationOverlay
	reply chan string // receives "elicitation:<action>" submit token
}

// showElicitation sets activeOverlay to the elicitation overlay and records the
// pending request so the submit handler can unblock the waiting goroutine.
// onElicitationShown (if set) is called last so tests can gate input delivery.
func (l *Loop) showElicitation(ep elicitationPending) {
	l.activeElicitation = &ep
	l.activeOverlay = ep.ov
	if l.onElicitationShown != nil {
		l.onElicitationShown()
	}
}

// showElicitationOverlay is the production bridge function passed to
// loopElicitationPrompt. It sends the overlay to elicitationCh (non-blocking
// for the caller goroutine) and returns a channel that receives the reply
// from handleOverlaySubmit once the user makes a choice.
func (l *Loop) showElicitationOverlay(ov *elicitationOverlay) <-chan string {
	reply := make(chan string, 1)
	l.elicitationCh <- elicitationPending{ov: ov, reply: reply}
	return reply
}

// denyPendingElicitations cancels any blocked elicitation goroutine when the
// loop exits, sending a "elicitation:cancel" reply so the caller does not leak.
func (l *Loop) denyPendingElicitations() {
	if l.activeElicitation != nil {
		l.activeElicitation.reply <- "elicitation:cancel"
		l.activeElicitation = nil
	}
	for {
		select {
		case ep := <-l.elicitationCh:
			ep.reply <- "elicitation:cancel"
		default:
			return
		}
	}
}

// refreshBaseStatus recomputes baseStatus from the current mode + vim state,
// incorporating statusLine command output when configured.
// When the spinner is not running, propagates immediately to screen.Status.
func (l *Loop) refreshBaseStatus() {
	base := modeIndicator(l.mode, l.screen.VimEnabled, l.screen.VimMode)
	if l.statusLineCmd != "" {
		runner := l.statusLineCmdRunner
		if runner == nil {
			runner = RunStatusLineCommand
		}
		if out, err := runner(l.statusLineCmd); err == nil && out != "" {
			base = out
		}
	}
	l.baseStatus = base
	if !l.running {
		l.screen.Status = l.baseStatus
	}
}

func (l *Loop) startSpinner() {
	now := time.Now()
	l.baseStatus = l.screen.Status
	l.spinner = NewSpinnerWithConfig(now, l.spinnerCfg)
	ticker := time.NewTicker(spinnerInterval)
	l.tickCh = ticker.C
	l.stopTick = ticker.Stop
	l.screen.Status = l.spinner.Line(now)
	l.setTerminalTitle("Claude — thinking…")
}

func (l *Loop) stopSpinner() {
	if l.stopTick != nil {
		l.stopTick()
		l.stopTick = nil
	}
	l.tickCh = nil
	l.screen.Status = l.baseStatus
}

func (l *Loop) tick() {
	// REPL-24: suppress spinner updates while streaming text is visible.
	// CC ref: src/screens/REPL.tsx:1683 — spinner hidden when text streaming.
	if l.running && !l.streamingActive {
		l.screen.Status = l.spinner.Line(time.Now())
	}
}
