package repl

import (
	"bufio"
	"context"
	"io"
	"strings"

	"ccgo/internal/contracts"
	"ccgo/internal/conversation"
	"ccgo/internal/tui"
)

// ExitAlternateMarker is the leading bytes of the alt-screen exit sequence;
// used by tests to confirm clean teardown.
const ExitAlternateMarker = "\x1b[?1049l"

// PermissionAskRequest is a placeholder for the Task 6 permission dialog wire-up.
type PermissionAskRequest struct{}

type askRequest struct {
	req   PermissionAskRequest
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
	defer restore() //nolint:errcheck

	opts := tui.TerminalModeOptions{BracketedPaste: true, FocusEvents: true}
	if err := l.term.WriteString(l.life.EnterInteractive(opts)); err != nil {
		return err
	}
	defer l.term.WriteString(l.life.ExitInteractive()) //nolint:errcheck

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
		}
	}
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
	switch event.Type {
	case tui.ScreenEventExit:
		return true
	case tui.ScreenEventPromptSubmitted:
		if l.StartTurn != nil && strings.TrimSpace(event.Value) != "" {
			l.running = true
			l.StartTurn(event.Value)
		}
	}
	return false
}

func (l *Loop) render() error {
	return l.term.WriteString(l.screen.Render())
}

// runLineMode is the non-tty fallback: read lines, submit each as a prompt.
func (l *Loop) runLineMode(ctx context.Context) error {
	reader := bufio.NewReader(readerFunc(l.term.Read))
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
