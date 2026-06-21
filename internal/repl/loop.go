package repl

import (
	"bufio"
	"context"
	"errors"
	"io"
	"strings"
	"time"

	"ccgo/internal/contracts"
	"ccgo/internal/conversation"
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

	inputCh    chan tui.Key
	eventCh    chan conversation.Event
	askCh      chan askRequest
	doneCh     chan turnOutcome
	resizeCh   chan resizeEvent
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

	// onTurnDone is a test seam; nil in production. Called at the end of
	// finishTurn so tests can synchronize after the turn completes and history
	// is updated (mirrors onPermissionShown).
	onTurnDone func()

	// onRulePersisted is a test seam; nil in production. Called for each
	// PermissionUpdate that would be persisted by an "allow always" choice.
	onRulePersisted func(contracts.PermissionUpdate)

	running    bool
	turnCancel context.CancelFunc
	width      int
	height     int
}

// SetSettingsWriter wires the settings writer used to persist "allow always"
// permission rules. Called from run.go during Task 13 wiring.
func (l *Loop) SetSettingsWriter(w ruleWriter) { l.settings = w }

// SetRegistry sets the command list used to populate the slash-command overlay.
// Call this from run.go once the command registry is loaded.
func (l *Loop) SetRegistry(cmds []contracts.Command) { l.registry = cmds }

func NewLoop(t Terminal, history []string) *Loop {
	w, h, err := t.Size()
	if err != nil || w <= 0 || h <= 0 {
		w, h = 80, 24
	}
	return &Loop{
		term:     t,
		screen:   tui.NewREPLScreen(w, h, history),
		dialog:   tui.NewDialogRuntime(),
		inputCh:  make(chan tui.Key, 64),
		eventCh:  make(chan conversation.Event, 256),
		askCh:    make(chan askRequest, 4),
		doneCh:   make(chan turnOutcome, 1),
		resizeCh: make(chan resizeEvent, 1),
		width:    w,
		height:   h,
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

	opts := tui.TerminalModeOptions{BracketedPaste: true, FocusEvents: true}
	if err := l.term.WriteString(l.life.EnterInteractive(opts)); err != nil {
		return err
	}
	defer func() { _ = l.term.WriteString(l.life.ExitInteractive()) }()

	go l.readInput(ctx)
	startResizeListener(ctx, l.term, l.resizeCh)

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
		}
	}
}

// applyEvent renders a single conversation event to the screen transcript.
func (l *Loop) applyEvent(ev conversation.Event) {
	if ev.Type == conversation.EventToolUse {
		l.lastToolUse = ev.ToolUse
	}
	if ev.Type == conversation.EventToolResult {
		text := renderToolResultText(l.lastToolUse, ev.ToolResult)
		l.screen.AppendMessage(tui.Message{Role: tui.RoleTool, Text: text})
		return
	}
	if msg, ok := messageFromEvent(ev); ok {
		l.screen.AppendMessage(msg)
	}
}

// finishTurn handles turn completion: updates history on success or shows an
// error message on failure, then clears the running flag.
func (l *Loop) finishTurn(out turnOutcome) {
	l.running = false
	l.stopSpinner()
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
			} else if res.Submit != "" {
				l.activeOverlay = nil
				if l.StartTurn != nil && !l.running {
					l.running = true
					l.startSpinner()
					l.StartTurn(res.Submit)
				}
			}
			return false
		}
	}

	event := l.screen.ApplyKey(key)

	// Open the slash menu when the prompt text is exactly "/" (first keystroke).
	if l.activeOverlay == nil && l.registry != nil && l.screen.Prompt.Text == "/" {
		l.activeOverlay = NewSlashMenu(l.registry, "")
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
		if l.StartTurn != nil && !l.running && strings.TrimSpace(event.Value) != "" {
			l.running = true
			l.StartTurn(event.Value)
			l.startSpinner()
		}
	}
	return false
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
	l.dialog.RequestPermission(tui.PermissionRequest{
		ID:          string(ar.req.ToolUseID),
		ToolName:    ar.req.ToolName,
		Path:        ar.req.Path,
		Description: ar.req.Description,
		Actions:     actions.Actions,
	})
	l.dialog.ApplyToScreen(&l.screen, l.screen.Status)
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

func (l *Loop) startSpinner() {
	now := time.Now()
	l.baseStatus = l.screen.Status
	l.spinner = NewSpinner(now)
	ticker := time.NewTicker(spinnerInterval)
	l.tickCh = ticker.C
	l.stopTick = ticker.Stop
	l.screen.Status = l.spinner.Line(now)
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
	if l.running {
		l.screen.Status = l.spinner.Line(time.Now())
	}
}
