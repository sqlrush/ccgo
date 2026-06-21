package repl

import (
	"bufio"
	"context"
	"io"
	"strings"

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

// Loop is the terminal runtime that drives the existing tui.REPLScreen.
type Loop struct {
	term   Terminal
	screen tui.REPLScreen
	life   tui.ScreenLifecycle
	dialog *tui.DialogRuntime

	inputCh chan tui.Key
	eventCh chan conversation.Event
	askCh   chan askRequest
	doneCh  chan turnOutcome

	// StartTurn is invoked when the user submits a prompt. It runs the model
	// turn (typically in a goroutine) and posts to eventCh/askCh/doneCh.
	StartTurn func(input string)

	history   []contracts.Message
	activeAsk *askRequest
	askQueue  []askRequest

	// onPermissionShown is a test seam; nil in production. Called at the end of
	// showPermission so tests can synchronize input delivery after the dialog is
	// rendered.
	onPermissionShown func()

	running bool
	width   int
	height  int
}

func NewLoop(t Terminal, history []string) *Loop {
	w, h, err := t.Size()
	if err != nil || w <= 0 || h <= 0 {
		w, h = 80, 24
	}
	return &Loop{
		term:    t,
		screen:  tui.NewREPLScreen(w, h, history),
		dialog:  tui.NewDialogRuntime(),
		inputCh: make(chan tui.Key, 64),
		eventCh: make(chan conversation.Event, 256),
		askCh:   make(chan askRequest, 4),
		doneCh:  make(chan turnOutcome, 1),
		width:   w,
		height:  h,
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
		}
	}
}

// applyEvent renders a single conversation event to the screen transcript.
func (l *Loop) applyEvent(ev conversation.Event) {
	if msg, ok := messageFromEvent(ev); ok {
		l.screen.AppendMessage(msg)
	}
}

// finishTurn handles turn completion: updates history on success or shows an
// error message on failure, then clears the running flag.
func (l *Loop) finishTurn(out turnOutcome) {
	l.running = false
	if out.err != nil {
		l.screen.AppendMessage(tui.Message{Role: tui.RoleSystem, Text: out.err.Error()})
		return
	}
	newHistory := make([]contracts.Message, len(l.history)+len(out.result.Messages))
	copy(newHistory, l.history)
	copy(newHistory[len(l.history):], out.result.Messages)
	l.history = newHistory
}

// readInput segments the terminal byte stream into keys and posts them.
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
	event := l.screen.ApplyKey(key)

	if l.activeAsk != nil &&
		(event.Type == tui.ScreenEventDialogAction || event.Type == tui.ScreenEventCancelled) {
		result := l.dialog.ResolveScreenEvent(&l.screen, event, l.screen.Status)
		if result.Found {
			behavior := decisionFromAction(result.Action)
			if result.Status == tui.DialogResultCancelled || result.Status == tui.DialogResultDenied {
				behavior = contracts.PermissionDeny
			}
			l.activeAsk.reply <- contracts.PermissionDecision{Behavior: behavior}
			l.activeAsk = nil
			l.showNext()
		}
		return false
	}

	switch event.Type {
	case tui.ScreenEventExit:
		return true
	case tui.ScreenEventPromptSubmitted:
		// Ignore empty/whitespace-only submissions silently.
		if l.StartTurn != nil && strings.TrimSpace(event.Value) != "" {
			l.running = true
			l.StartTurn(event.Value)
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
	l.dialog.RequestPermission(tui.PermissionRequest{
		ID:          string(ar.req.ToolUseID),
		ToolName:    ar.req.ToolName,
		Path:        ar.req.Path,
		Description: ar.req.Description,
	})
	l.dialog.ApplyToScreen(&l.screen, l.screen.Status)
	if l.onPermissionShown != nil {
		l.onPermissionShown()
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
