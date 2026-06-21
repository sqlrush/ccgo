# Interactive Runtime (Phase 1) Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Make `claude` (no `--print`) launch a real interactive REPL — read keystrokes, run a model turn with live streaming render, execute tools, and interactively approve/deny permission prompts — by wiring ccgo's existing-but-dead TUI library to a real terminal.

**Architecture:** A new thin glue package `internal/repl/` owns the terminal I/O runtime that the existing `internal/tui` state machine was designed for but never got. It (1) puts the tty in raw mode via `golang.org/x/term`, (2) segments the stdin byte stream into escape sequences and feeds the existing `tui.ParseKey`, (3) runs a channel-based event loop (`select` over input keys / turn events / permission asks / turn completion) that drives `screen.ApplyKey` → `screen.Render`, and (4) adds a `PermissionAsker` seam to `tool.Executor` so the dead `Ask` branch can surface an interactive dialog and block the tool until the user answers. The model turn runs in a goroutine; `runner.OnEvent` posts events back to the loop for rendering. This reuses the 21K-LOC TUI library wholesale and adds ~no new rendering logic.

**Tech Stack:** Go 1.26; new deps `golang.org/x/term` (+ transitive `golang.org/x/sys`); existing packages `internal/tui`, `internal/tool`, `internal/permissions`, `internal/conversation`, `internal/bootstrap`, `internal/session`, `internal/messages`, `internal/contracts`.

## Global Constraints

- Module `ccgo`, `go 1.26` — copied verbatim from `go.mod`.
- Immutability: never mutate shared structs in place; copy the `conversation.Runner` value per turn before setting `OnEvent`/`Tools.Asker` (matches existing `headlessRunner`/`attachStreamJSON` pattern). `permissions.Engine.ApplyUpdate` already returns a **new** engine — honor that.
- Many small files: each new file in `internal/repl/` has one responsibility; target 150–350 lines.
- Errors handled explicitly at every level; never swallow. Terminal raw-mode `restore` MUST run on every exit path (use `defer`).
- No new third-party deps beyond `golang.org/x/term` (and its required `golang.org/x/sys`). No bubbletea/tcell/charm.
- Non-TTY safety: when stdin/stdout is **not** a terminal (CI, pipes), the interactive path MUST NOT call `term.MakeRaw`; it falls back to a line-buffered loop. Tests MUST never depend on a real tty.
- TDD: every task writes a failing test first, then minimal code. Commit after each task.
- Run all tests with `go test ./...`; package tests with `go test ./internal/repl/ -run TestName -v`.

---

## File Structure

**New package `internal/repl/`:**
- `terminal.go` — `Terminal` interface; `OSTerminal` (real, `x/term`); raw-mode + size + tty detection.
- `terminal_fake.go` — `FakeTerminal` for tests (buffer-backed, no tty needed). (Non-`_test.go` so other packages' tests could reuse it; small.)
- `sequence.go` — `SequenceScanner`: stdin bytes → complete escape sequences/runes/paste blocks. Pure `segment()` helper is the TDD core.
- `loop.go` — `Loop`: the channel-based event loop tying `Terminal` + `*tui.REPLScreen` + `tui.ScreenLifecycle` + `tui.DialogRuntime`; key handling, render, exit, submit, resize.
- `render.go` — maps `conversation.Event` → screen `Message`s (live streaming) and `conversation.Result` → final messages.
- `asker.go` — `loopAsker` implementing `tool.PermissionAsker` via the loop's ask channel; dialog resolution mapping.
- `run.go` — `RunInteractive(ctx, term, base, history)`: builds the loop, wires `StartTurn` to a real `RunTurn` goroutine, runs.

**Modified existing files:**
- `internal/tool/types.go` — add `PermissionAsker` interface + `PermissionAskRequest`.
- `internal/tool/executor.go` — add `Asker` field; consult it in the `Ask` branch (line ~106).
- `cmd/claude/main.go` — replace the scaffold stub (lines 269–275) with `interactive` dispatch; add `interactiveRunner` helper.
- `go.mod` / `go.sum` — add `golang.org/x/term`.

---

## Task 1: Terminal abstraction + real `x/term` implementation

**Files:**
- Create: `internal/repl/terminal.go`
- Create: `internal/repl/terminal_fake.go`
- Test: `internal/repl/terminal_test.go`
- Modify: `go.mod`, `go.sum` (via `go get`)

**Interfaces:**
- Produces:
  - `type Terminal interface { IsTTY() bool; MakeRaw() (restore func() error, err error); Read(p []byte) (int, error); WriteString(s string) error; Size() (width, height int, err error) }`
  - `func NewOSTerminal(in *os.File, out *os.File) *OSTerminal`
  - `type FakeTerminal struct { In *bytes.Buffer; Out *bytes.Buffer; W, H int; Raw bool; TTY bool }` with `func NewFakeTerminal(input string, w, h int) *FakeTerminal`

- [ ] **Step 1: Add the dependency**

Run:
```bash
cd /Users/sqlrush/ccgo && go get golang.org/x/term@latest
```
Expected: `go.mod` gains `require golang.org/x/term vX.Y.Z` (and `golang.org/x/sys` indirect); `go.sum` updated.

- [ ] **Step 2: Write the failing test**

Create `internal/repl/terminal_test.go`:
```go
package repl

import "testing"

func TestFakeTerminalReadWrite(t *testing.T) {
	ft := NewFakeTerminal("ab", 80, 24)
	if !ft.IsTTY() {
		t.Fatal("FakeTerminal should report IsTTY true by default")
	}
	w, h, err := ft.Size()
	if err != nil || w != 80 || h != 24 {
		t.Fatalf("Size() = %d,%d,%v want 80,24,nil", w, h, err)
	}
	buf := make([]byte, 1)
	n, err := ft.Read(buf)
	if err != nil || n != 1 || buf[0] != 'a' {
		t.Fatalf("Read() = %d,%q,%v want 1,'a',nil", n, buf[:n], err)
	}
	if err := ft.WriteString("XY"); err != nil {
		t.Fatalf("WriteString err: %v", err)
	}
	if got := ft.Out.String(); got != "XY" {
		t.Fatalf("Out = %q want %q", got, "XY")
	}
	restore, err := ft.MakeRaw()
	if err != nil || !ft.Raw {
		t.Fatalf("MakeRaw should set Raw; err=%v", err)
	}
	if err := restore(); err != nil || ft.Raw {
		t.Fatalf("restore should clear Raw; err=%v", err)
	}
}

func TestOSTerminalIsTTYFalseForPipe(t *testing.T) {
	// os.Pipe() endpoints are never TTYs; guards against raw-mode in CI.
	r, w, err := osPipe()
	if err != nil {
		t.Fatal(err)
	}
	defer r.Close()
	defer w.Close()
	term := NewOSTerminal(r, w)
	if term.IsTTY() {
		t.Fatal("pipe should not be a TTY")
	}
}
```

- [ ] **Step 3: Run test to verify it fails**

Run: `go test ./internal/repl/ -run TestFakeTerminal -v`
Expected: FAIL — `undefined: NewFakeTerminal` (package doesn't compile).

- [ ] **Step 4: Write minimal implementation**

Create `internal/repl/terminal.go`:
```go
package repl

import (
	"io"
	"os"

	"golang.org/x/term"
)

// Terminal abstracts the raw tty I/O the REPL needs. OSTerminal is the real
// implementation; FakeTerminal (terminal_fake.go) backs tests without a tty.
type Terminal interface {
	IsTTY() bool
	MakeRaw() (restore func() error, err error)
	Read(p []byte) (int, error)
	WriteString(s string) error
	Size() (width, height int, err error)
}

// OSTerminal drives a real terminal via golang.org/x/term.
type OSTerminal struct {
	in  *os.File
	out *os.File
}

func NewOSTerminal(in *os.File, out *os.File) *OSTerminal {
	return &OSTerminal{in: in, out: out}
}

func (t *OSTerminal) IsTTY() bool {
	return term.IsTerminal(int(t.in.Fd())) && term.IsTerminal(int(t.out.Fd()))
}

func (t *OSTerminal) MakeRaw() (func() error, error) {
	fd := int(t.in.Fd())
	state, err := term.MakeRaw(fd)
	if err != nil {
		return nil, err
	}
	return func() error { return term.Restore(fd, state) }, nil
}

func (t *OSTerminal) Read(p []byte) (int, error) { return t.in.Read(p) }

func (t *OSTerminal) WriteString(s string) error {
	_, err := io.WriteString(t.out, s)
	return err
}

func (t *OSTerminal) Size() (int, int, error) {
	w, h, err := term.GetSize(int(t.out.Fd()))
	if err != nil {
		return 0, 0, err
	}
	return w, h, nil
}

// osPipe is a tiny seam so tests can construct OSTerminal over an os.Pipe.
func osPipe() (*os.File, *os.File, error) { return os.Pipe() }
```

Create `internal/repl/terminal_fake.go`:
```go
package repl

import "bytes"

// FakeTerminal is a buffer-backed Terminal for tests. Read drains In; once
// empty it returns io.EOF (the loop treats EOF as a clean exit).
type FakeTerminal struct {
	In  *bytes.Buffer
	Out *bytes.Buffer
	W   int
	H   int
	Raw bool
	TTY bool
}

func NewFakeTerminal(input string, w, h int) *FakeTerminal {
	return &FakeTerminal{
		In:  bytes.NewBufferString(input),
		Out: &bytes.Buffer{},
		W:   w,
		H:   h,
		TTY: true,
	}
}

func (f *FakeTerminal) IsTTY() bool { return f.TTY }

func (f *FakeTerminal) MakeRaw() (func() error, error) {
	f.Raw = true
	return func() error { f.Raw = false; return nil }, nil
}

func (f *FakeTerminal) Read(p []byte) (int, error) { return f.In.Read(p) }

func (f *FakeTerminal) WriteString(s string) error {
	_, err := f.Out.WriteString(s)
	return err
}

func (f *FakeTerminal) Size() (int, int, error) { return f.W, f.H, nil }
```

- [ ] **Step 5: Run tests to verify they pass**

Run: `go test ./internal/repl/ -v`
Expected: PASS (both tests).

- [ ] **Step 6: Commit**

```bash
git add go.mod go.sum internal/repl/terminal.go internal/repl/terminal_fake.go internal/repl/terminal_test.go
git commit -m "feat(repl): add Terminal abstraction with x/term OSTerminal and FakeTerminal"
```

---

## Task 2: Stdin byte-stream → escape-sequence segmenter

**Files:**
- Create: `internal/repl/sequence.go`
- Test: `internal/repl/sequence_test.go`

**Interfaces:**
- Consumes: `Terminal.Read` (any `io.Reader`).
- Produces:
  - `func segment(buf []byte, atEOF bool) (seq string, consumed int, complete bool)` — pure; the TDD core.
  - `type SequenceScanner struct{ ... }`; `func NewSequenceScanner(r io.Reader) *SequenceScanner`; `func (s *SequenceScanner) Next() (string, error)` — returns one complete sequence; `io.EOF` when stream ends.

Each returned `seq` is exactly what `tui.ParseKey(seq) tui.Key` expects (one rune, one control byte, or one complete escape/paste sequence).

- [ ] **Step 1: Write the failing test**

Create `internal/repl/sequence_test.go`:
```go
package repl

import (
	"bytes"
	"errors"
	"io"
	"testing"
)

func TestSegment(t *testing.T) {
	cases := []struct {
		name     string
		in       string
		atEOF    bool
		wantSeq  string
		wantN    int
		wantDone bool
	}{
		{"ascii", "a", false, "a", 1, true},
		{"ctrl-c", "\x03", false, "\x03", 1, true},
		{"enter", "\r", false, "\r", 1, true},
		{"csi-left", "\x1b[D", false, "\x1b[D", 3, true},
		{"csi-incomplete", "\x1b[", false, "", 0, false},
		{"ss3-f1", "\x1bOP", false, "\x1bOP", 3, true},
		{"alt-key", "\x1bx", false, "\x1bx", 2, true},
		{"lone-esc-eof", "\x1b", true, "\x1b", 1, true},
		{"lone-esc-need-more", "\x1b", false, "", 0, false},
		{"utf8-2byte", "é", false, "é", 2, true}, // é
		{"utf8-split", "\xc3", false, "", 0, false},        // first byte of é, need more
		{"paste", "\x1b[200~hi\x1b[201~", false, "\x1b[200~hi\x1b[201~", 14, true},
		{"paste-incomplete", "\x1b[200~hi", false, "", 0, false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			seq, n, done := segment([]byte(tc.in), tc.atEOF)
			if seq != tc.wantSeq || n != tc.wantN || done != tc.wantDone {
				t.Fatalf("segment(%q,%v) = %q,%d,%v want %q,%d,%v",
					tc.in, tc.atEOF, seq, n, done, tc.wantSeq, tc.wantN, tc.wantDone)
			}
		})
	}
}

func TestSequenceScannerNext(t *testing.T) {
	// "a", left-arrow, enter, then EOF.
	sc := NewSequenceScanner(bytes.NewReader([]byte("a\x1b[D\r")))
	want := []string{"a", "\x1b[D", "\r"}
	for _, w := range want {
		got, err := sc.Next()
		if err != nil {
			t.Fatalf("Next() err: %v", err)
		}
		if got != w {
			t.Fatalf("Next() = %q want %q", got, w)
		}
	}
	if _, err := sc.Next(); !errors.Is(err, io.EOF) {
		t.Fatalf("expected io.EOF, got %v", err)
	}
}

func TestSequenceScannerSplitReads(t *testing.T) {
	// Escape sequence split across two reads must reassemble.
	sc := NewSequenceScanner(&chunkReader{chunks: []string{"\x1b[", "D"}})
	got, err := sc.Next()
	if err != nil || got != "\x1b[D" {
		t.Fatalf("Next() = %q,%v want %q,nil", got, err, "\x1b[D")
	}
}

type chunkReader struct {
	chunks []string
	i      int
}

func (c *chunkReader) Read(p []byte) (int, error) {
	if c.i >= len(c.chunks) {
		return 0, io.EOF
	}
	n := copy(p, c.chunks[c.i])
	c.i++
	return n, nil
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/repl/ -run TestSegment -v`
Expected: FAIL — `undefined: segment`.

- [ ] **Step 3: Write minimal implementation**

Create `internal/repl/sequence.go`:
```go
package repl

import (
	"io"
	"unicode/utf8"
)

const esc = 0x1b

// segment inspects buf and returns the first complete input sequence, the
// number of bytes it consumed, and whether a complete sequence was found.
// atEOF=true forces a trailing lone ESC to be emitted as KeyEsc.
func segment(buf []byte, atEOF bool) (string, int, bool) {
	if len(buf) == 0 {
		return "", 0, false
	}
	b0 := buf[0]

	if b0 == esc {
		if len(buf) == 1 {
			if atEOF {
				return "\x1b", 1, true // lone Escape
			}
			return "", 0, false // wait for the rest of the sequence
		}
		switch buf[1] {
		case '[':
			return segmentCSI(buf)
		case 'O':
			if len(buf) >= 3 {
				return string(buf[:3]), 3, true // SS3, e.g. ESC O P (F1)
			}
			return "", 0, false
		default:
			return string(buf[:2]), 2, true // Alt+<key>, ESC <byte>
		}
	}

	if b0 < utf8.RuneSelf {
		return string(buf[:1]), 1, true // ASCII / control byte
	}

	// Multi-byte UTF-8 rune.
	n := runeLen(b0)
	if n == 0 {
		return string(buf[:1]), 1, true // invalid lead byte; consume one
	}
	if len(buf) < n {
		return "", 0, false
	}
	return string(buf[:n]), n, true
}

// segmentCSI handles ESC [ ... sequences, including bracketed paste blocks
// (ESC[200~ ... ESC[201~) which must be consumed whole.
func segmentCSI(buf []byte) (string, int, bool) {
	const pasteStart = "\x1b[200~"
	const pasteEnd = "\x1b[201~"
	if hasPrefix(buf, pasteStart) {
		end := indexOf(buf, []byte(pasteEnd))
		if end < 0 {
			return "", 0, false // paste not finished
		}
		total := end + len(pasteEnd)
		return string(buf[:total]), total, true
	}
	// Generic CSI: ESC [ params... final byte in 0x40..0x7E.
	for i := 2; i < len(buf); i++ {
		if buf[i] >= 0x40 && buf[i] <= 0x7e {
			return string(buf[:i+1]), i + 1, true
		}
	}
	return "", 0, false
}

func runeLen(b byte) int {
	switch {
	case b&0xe0 == 0xc0:
		return 2
	case b&0xf0 == 0xe0:
		return 3
	case b&0xf8 == 0xf0:
		return 4
	default:
		return 0
	}
}

func hasPrefix(b []byte, p string) bool {
	if len(b) < len(p) {
		return false
	}
	for i := 0; i < len(p); i++ {
		if b[i] != p[i] {
			return false
		}
	}
	return true
}

func indexOf(b, sub []byte) int {
	for i := 0; i+len(sub) <= len(b); i++ {
		match := true
		for j := 0; j < len(sub); j++ {
			if b[i+j] != sub[j] {
				match = false
				break
			}
		}
		if match {
			return i
		}
	}
	return -1
}

// SequenceScanner reads raw bytes from r and yields complete input sequences.
type SequenceScanner struct {
	r   io.Reader
	buf []byte
	eof bool
}

func NewSequenceScanner(r io.Reader) *SequenceScanner {
	return &SequenceScanner{r: r}
}

// Next returns the next complete input sequence. It returns io.EOF only once
// the buffer is fully drained and the underlying reader is exhausted.
func (s *SequenceScanner) Next() (string, error) {
	for {
		if seq, n, ok := segment(s.buf, s.eof); ok {
			s.buf = s.buf[n:]
			return seq, nil
		}
		if s.eof {
			if len(s.buf) > 0 {
				// Undecodable trailing bytes: emit one byte to make progress.
				b := string(s.buf[:1])
				s.buf = s.buf[1:]
				return b, nil
			}
			return "", io.EOF
		}
		chunk := make([]byte, 1024)
		n, err := s.r.Read(chunk)
		if n > 0 {
			s.buf = append(s.buf, chunk[:n]...)
		}
		if err == io.EOF {
			s.eof = true
		} else if err != nil {
			return "", err
		}
	}
}
```

- [ ] **Step 4: Run tests to verify they pass**

Run: `go test ./internal/repl/ -run 'TestSegment|TestSequenceScanner' -v`
Expected: PASS (all subtests).

- [ ] **Step 5: Commit**

```bash
git add internal/repl/sequence.go internal/repl/sequence_test.go
git commit -m "feat(repl): add stdin byte-stream to escape-sequence segmenter"
```

---

## Task 3: Event-loop skeleton (input → ApplyKey → render → exit/submit)

**Files:**
- Create: `internal/repl/loop.go`
- Test: `internal/repl/loop_test.go`

**Interfaces:**
- Consumes: `Terminal` (Task 1), `SequenceScanner`+`segment` (Task 2), `tui.NewREPLScreen`, `tui.ParseKey`, `(*tui.REPLScreen).ApplyKey/Render/Resize`, `tui.ScreenEvent*`, `tui.ScreenLifecycle` (`EnterInteractive`/`ExitInteractive`/`TerminalModeOptions`).
- Produces:
  - `type Loop struct { ... StartTurn func(input string); ... }`
  - `func NewLoop(t Terminal, history []string) *Loop`
  - `func (l *Loop) Run(ctx context.Context) error`
  - internal channels: `inputCh chan tui.Key`, `askCh chan askRequest` (defined Task 6), `eventCh chan conversation.Event` (used Task 4), `doneCh chan turnOutcome` (used Task 4).

For this task `StartTurn` is just invoked (no turn machinery yet); a test injects a recorder.

- [ ] **Step 1: Write the failing test**

Create `internal/repl/loop_test.go`:
```go
package repl

import (
	"context"
	"strings"
	"testing"
	"time"
)

func runLoop(t *testing.T, l *Loop) error {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	return l.Run(ctx)
}

func TestLoopSubmitThenExit(t *testing.T) {
	// Type "hi", press Enter (submit), then Ctrl-D twice (exit).
	ft := NewFakeTerminal("hi\r\x04\x04", 80, 24)
	l := NewLoop(ft, nil)

	var submitted []string
	l.StartTurn = func(input string) { submitted = append(submitted, input) }

	if err := runLoop(t, l); err != nil {
		t.Fatalf("Run err: %v", err)
	}
	if len(submitted) != 1 || submitted[0] != "hi" {
		t.Fatalf("submitted = %v want [hi]", submitted)
	}
	if ft.Raw {
		t.Fatal("terminal raw mode not restored on exit")
	}
	// Lifecycle should have left the alternate screen on exit.
	if !strings.Contains(ft.Out.String(), ExitAlternateMarker) {
		t.Fatal("expected alternate-screen exit sequence in output")
	}
}

func TestLoopNonTTYFallback(t *testing.T) {
	ft := NewFakeTerminal("hello\n", 80, 24)
	ft.TTY = false
	l := NewLoop(ft, nil)
	var submitted []string
	l.StartTurn = func(input string) { submitted = append(submitted, input) }
	if err := runLoop(t, l); err != nil {
		t.Fatalf("Run err: %v", err)
	}
	if len(submitted) != 1 || submitted[0] != "hello" {
		t.Fatalf("submitted = %v want [hello]", submitted)
	}
	if ft.Raw {
		t.Fatal("non-tty path must not enter raw mode")
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/repl/ -run TestLoop -v`
Expected: FAIL — `undefined: NewLoop` / `undefined: ExitAlternateMarker`.

- [ ] **Step 3: Write minimal implementation**

Create `internal/repl/loop.go`:
```go
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
	defer restore()

	opts := tui.TerminalModeOptions{BracketedPaste: true, FocusEvents: true}
	if err := l.term.WriteString(l.life.EnterInteractive(opts)); err != nil {
		return err
	}
	defer l.term.WriteString(l.life.ExitInteractive())

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
```

Note: `ScreenEventExit` is produced by the screen itself (e.g. Ctrl-D twice; see `tui` exit-pending logic). `ScreenEventInterrupted`/`ScreenEventCancelled` are handled in Task 4 (turn abort). The unused `eventCh`/`doneCh`/`askCh`/`dialog`/`running` are wired in Tasks 4 and 6.

- [ ] **Step 4: Run tests to verify they pass**

Run: `go test ./internal/repl/ -run TestLoop -v`
Expected: PASS. (FakeTerminal's `Read` returns io.EOF when drained → `readInput` closes `inputCh` → `Run` returns. The `\x04\x04` exits before EOF in the tty test; the non-tty test exits on EOF.)

If `ScreenEventExit` is not emitted by `\x04\x04` in the current `tui` build, adjust the test input to the screen's actual exit chord — verify with: `go doc ./internal/tui | grep -i exit` and `grep -rn "ScreenEventExit" internal/tui/`. Use the confirmed chord; do not change production logic to fit the test.

- [ ] **Step 5: Commit**

```bash
git add internal/repl/loop.go internal/repl/loop_test.go
git commit -m "feat(repl): add terminal event loop with submit, exit, and non-tty fallback"
```

---

## Task 4: Live turn rendering (conversation.Event → screen messages)

**Files:**
- Create: `internal/repl/render.go`
- Modify: `internal/repl/loop.go` (handle `eventCh`/`doneCh` in the select)
- Test: `internal/repl/render_test.go`

**Interfaces:**
- Consumes: `conversation.Event`, `conversation.EventType*`, `conversation.Result`, `contracts.Message`, `messages.TextContent`, `(*tui.REPLScreen).AppendMessage/SetMessages`, `tui.Message`.
- Produces:
  - `func messageFromEvent(ev conversation.Event) (tui.Message, bool)` — maps an event to a renderable message; `false` to skip.
  - `func (l *Loop) applyEvent(ev conversation.Event)` and `func (l *Loop) finishTurn(out turnOutcome)`.

You MUST first confirm the exact `tui.Message` struct shape: run `go doc ./internal/tui Message`. The code below assumes `tui.Message{ Role string; Text string }`; if fields differ (e.g. `Kind`/`Content`), adjust the literals accordingly — keep the mapping, fix the field names.

- [ ] **Step 1: Write the failing test**

Create `internal/repl/render_test.go`:
```go
package repl

import (
	"testing"

	"ccgo/internal/contracts"
	"ccgo/internal/conversation"
	"ccgo/internal/messages"
)

func TestMessageFromEventAssistant(t *testing.T) {
	asst := messages.UserText("") // placeholder; build assistant message:
	asst.Type = contracts.MessageAssistant
	asst.Content = []contracts.ContentBlock{contracts.NewTextBlock("hello there")}

	ev := conversation.Event{Type: conversation.EventAssistantMessage, Message: &asst}
	msg, ok := messageFromEvent(ev)
	if !ok {
		t.Fatal("expected a renderable message for assistant event")
	}
	if msg.Text != "hello there" {
		t.Fatalf("msg.Text = %q want %q", msg.Text, "hello there")
	}
}

func TestMessageFromEventSkipsInternal(t *testing.T) {
	ev := conversation.Event{Type: conversation.EventToolSearchDecision}
	if _, ok := messageFromEvent(ev); ok {
		t.Fatal("internal event should not render")
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/repl/ -run TestMessageFromEvent -v`
Expected: FAIL — `undefined: messageFromEvent`.

- [ ] **Step 3: Write minimal implementation**

Create `internal/repl/render.go`:
```go
package repl

import (
	"fmt"

	"ccgo/internal/conversation"
	"ccgo/internal/messages"
	"ccgo/internal/tui"
)

// messageFromEvent maps a conversation event to a renderable screen message.
// Returns false for events that should not appear in the transcript view.
func messageFromEvent(ev conversation.Event) (tui.Message, bool) {
	switch ev.Type {
	case conversation.EventAssistantMessage:
		if ev.Message == nil {
			return tui.Message{}, false
		}
		text := messages.TextContent(*ev.Message)
		if text == "" {
			return tui.Message{}, false
		}
		return tui.Message{Role: "assistant", Text: text}, true
	case conversation.EventToolUse:
		if ev.ToolUse == nil {
			return tui.Message{}, false
		}
		return tui.Message{Role: "tool", Text: fmt.Sprintf("⏺ %s", ev.ToolUse.Name)}, true
	case conversation.EventToolResult:
		if ev.ToolResult == nil {
			return tui.Message{}, false
		}
		return tui.Message{Role: "tool", Text: toolResultLine(*ev.ToolResult)}, true
	default:
		return tui.Message{}, false
	}
}

func toolResultLine(r conversation.Result) tui.Message { panic("unused") } // removed below
```

Replace the dangling helper — the real `toolResultLine` takes `contracts.ToolResult`:
```go
// (Place this instead of the panic stub above.)
func toolResultLine(r contractsToolResult) string {
	if r.IsError {
		return "  ⎿ error"
	}
	return "  ⎿ ok"
}
```
…but to avoid an import alias, write it directly with the real type. Final `render.go` `toolResultLine`:
```go
func toolResultLine(r contracts.ToolResult) string {
	if r.IsError {
		return "  ⎿ error"
	}
	return "  ⎿ ok"
}
```
(Add `"ccgo/internal/contracts"` to the import block and delete the placeholder lines. The `EventToolResult` case calls `tui.Message{Role: "tool", Text: toolResultLine(*ev.ToolResult)}`.)

Now wire the loop. In `internal/repl/loop.go`, add `applyEvent`/`finishTurn` and extend the `select`:
```go
func (l *Loop) applyEvent(ev conversation.Event) {
	if msg, ok := messageFromEvent(ev); ok {
		l.screen.AppendMessage(msg)
	}
}

func (l *Loop) finishTurn(out turnOutcome) {
	l.running = false
	if out.err != nil {
		l.screen.AppendMessage(tui.Message{Role: "error", Text: out.err.Error()})
		return
	}
	for _, m := range out.result.Messages {
		l.history = append(l.history, m)
	}
}
```
Add `history []contracts.Message` to the `Loop` struct. Then extend the `Run` select loop (the tty branch) to also handle turn channels:
```go
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
```

- [ ] **Step 4: Run tests to verify they pass**

Run: `go test ./internal/repl/ -v`
Expected: PASS. Fix any `tui.Message` field-name mismatch flagged by the compiler per the Step-1 note.

- [ ] **Step 5: Commit**

```bash
git add internal/repl/render.go internal/repl/loop.go internal/repl/render_test.go
git commit -m "feat(repl): render live turn events into the screen transcript"
```

---

## Task 5: `PermissionAsker` seam in the tool executor

**Files:**
- Modify: `internal/tool/types.go` (add interface + request type)
- Modify: `internal/tool/executor.go` (add `Asker` field; consult it in the Ask branch)
- Test: `internal/tool/executor_asker_test.go`

**Interfaces:**
- Produces:
  - `type PermissionAskRequest struct { ToolUseID contracts.ID; ToolName string; Path string; Description string; Decision contracts.PermissionDecision }`
  - `type PermissionAsker interface { Ask(ctx context.Context, req PermissionAskRequest) (contracts.PermissionDecision, error) }`
  - new field `Asker PermissionAsker` on `Executor`.
- Behavior: in the `Ask` branch (executor.go:106), when hooks don't resolve and `e.Asker != nil`, call `e.Asker.Ask(...)`. `PermissionAllow` → fall through and run the tool; `PermissionDeny` → mirror the deny path; anything else / nil asker → preserve today's `permission_requested` behavior.

- [ ] **Step 1: Write the failing test**

Create `internal/tool/executor_asker_test.go`:
```go
package tool

import (
	"context"
	"encoding/json"
	"testing"

	"ccgo/internal/contracts"
)

type fakeAsker struct {
	behavior contracts.PermissionBehavior
	called   bool
}

func (f *fakeAsker) Ask(ctx context.Context, req PermissionAskRequest) (contracts.PermissionDecision, error) {
	f.called = true
	return contracts.PermissionDecision{Behavior: f.behavior}, nil
}

// askDecider always returns Ask, forcing the asker path.
type askDecider struct{}

func (askDecider) DecideTool(t Tool, input json.RawMessage, ctx Context) (contracts.PermissionDecision, error) {
	return contracts.PermissionDecision{Behavior: contracts.PermissionAsk}, nil
}

func newAskExecutor(t *testing.T, asker PermissionAsker) (Executor, contracts.ToolUse, Context) {
	t.Helper()
	reg, err := NewRegistry(EchoTestTool{})
	if err != nil {
		t.Fatal(err)
	}
	exec := NewExecutor(reg)
	exec.Asker = asker
	use := contracts.ToolUse{ID: "u1", Name: "echo", Input: json.RawMessage(`{"text":"hi"}`)}
	ctx := Context{Context: context.Background(), Permissions: askDecider{}}
	return exec, use, ctx
}

func TestExecutorAskerAllowRunsTool(t *testing.T) {
	asker := &fakeAsker{behavior: contracts.PermissionAllow}
	exec, use, ctx := newAskExecutor(t, asker)
	res, err := exec.Execute(ctx, use, NopProgressSink())
	if err != nil {
		t.Fatalf("Execute err: %v", err)
	}
	if !asker.called {
		t.Fatal("asker not consulted")
	}
	if res.IsError {
		t.Fatalf("expected tool to run, got error result: %q", res.Content)
	}
}

func TestExecutorAskerDenyBlocksTool(t *testing.T) {
	asker := &fakeAsker{behavior: contracts.PermissionDeny}
	exec, use, ctx := newAskExecutor(t, asker)
	res, err := exec.Execute(ctx, use, NopProgressSink())
	if _, ok := err.(PermissionError); !ok {
		t.Fatalf("expected PermissionError, got %v", err)
	}
	if !res.IsError {
		t.Fatal("expected error result on deny")
	}
}

func TestExecutorNilAskerPreservesOldBehavior(t *testing.T) {
	exec, use, ctx := newAskExecutor(t, nil)
	_, err := exec.Execute(ctx, use, NopProgressSink())
	if _, ok := err.(PermissionError); !ok {
		t.Fatalf("nil asker should still return PermissionError, got %v", err)
	}
}
```

If no minimal in-package test tool exists, first check: `grep -rn "TestTool\|EchoTool\|stubTool" internal/tool/*_test.go`. Reuse the existing test tool name in the registry call instead of `EchoTestTool{}` / `"echo"`. Do **not** add a production tool just for the test; use an existing test helper or add one in a `_test.go` file.

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/tool/ -run TestExecutorAsker -v`
Expected: FAIL — `exec.Asker undefined` / `undefined: PermissionAsker`.

- [ ] **Step 3: Write minimal implementation**

In `internal/tool/types.go`, add (near the other interfaces; ensure `"context"` is imported):
```go
// PermissionAskRequest describes a tool call awaiting an interactive decision.
type PermissionAskRequest struct {
	ToolUseID   contracts.ID
	ToolName    string
	Path        string
	Description string
	Decision    contracts.PermissionDecision
}

// PermissionAsker resolves an "ask" permission decision interactively.
// Implementations block until the user answers (or ctx is cancelled).
type PermissionAsker interface {
	Ask(ctx context.Context, req PermissionAskRequest) (contracts.PermissionDecision, error)
}
```

In `internal/tool/executor.go`, add the field to the struct (executor.go:31-35):
```go
type Executor struct {
	Registry       *Registry
	ResultStoreDir string
	Hooks          []Hook
	Asker          PermissionAsker
}
```

Then change the Ask branch. Replace the early-return block at executor.go:106-109:
```go
		if hookDecision == nil || hookDecision.Behavior == contracts.PermissionAsk {
			_ = SendProgress(sink, use.ID, "permission_requested", map[string]any{"tool": t.Name(), "behavior": string(decision.Behavior)})
			return result, permissionErr
		}
```
with:
```go
		if hookDecision == nil || hookDecision.Behavior == contracts.PermissionAsk {
			if e.Asker != nil {
				askReq := PermissionAskRequest{
					ToolUseID:   use.ID,
					ToolName:    t.Name(),
					Path:        decision.BlockedPath,
					Description: decision.Message,
					Decision:    decision,
				}
				asked, askErr := e.Asker.Ask(ctx.Context, askReq)
				if askErr != nil {
					return ErrorResult(use, askErr), askErr
				}
				switch asked.Behavior {
				case contracts.PermissionAllow:
					if asked.UpdatedInput != nil {
						if merged, mErr := mergeUpdatedInput(raw, asked.UpdatedInput); mErr == nil {
							raw = merged
						}
					}
					_ = SendProgress(sink, use.ID, "permission_allowed", map[string]any{"tool": t.Name(), "behavior": string(asked.Behavior)})
					// fall through to validation + Call below
				case contracts.PermissionDeny:
					if asked.Message != "" {
						result.Content = asked.Message
					}
					result.Meta["permission"] = asked
					_ = SendProgress(sink, use.ID, "permission_denied", map[string]any{"tool": t.Name(), "behavior": string(asked.Behavior)})
					return result, PermissionError{Decision: asked}
				default:
					_ = SendProgress(sink, use.ID, "permission_requested", map[string]any{"tool": t.Name(), "behavior": string(asked.Behavior)})
					return result, permissionErr
				}
			} else {
				_ = SendProgress(sink, use.ID, "permission_requested", map[string]any{"tool": t.Name(), "behavior": string(decision.Behavior)})
				return result, permissionErr
			}
		}
```

If a `mergeUpdatedInput(raw json.RawMessage, updates map[string]any) (json.RawMessage, error)` helper does not already exist, drop the `UpdatedInput` block entirely for Phase 1 (do not invent it) — interactive Allow in Phase 1 runs the original input. Verify with `grep -rn "UpdatedInput" internal/tool/`; reuse the existing merge helper if present, else remove those four lines.

- [ ] **Step 4: Run tests to verify they pass**

Run: `go test ./internal/tool/ -run TestExecutor -v && go test ./internal/tool/ -v`
Expected: PASS, including pre-existing executor tests (the nil-asker path is unchanged).

- [ ] **Step 5: Commit**

```bash
git add internal/tool/types.go internal/tool/executor.go internal/tool/executor_asker_test.go
git commit -m "feat(tool): add PermissionAsker seam so the Ask branch can prompt interactively"
```

---

## Task 6: Interactive permission dialog bridge in the loop

**Files:**
- Create: `internal/repl/asker.go`
- Modify: `internal/repl/loop.go` (handle `askCh`; resolve dialog from key events)
- Test: `internal/repl/asker_test.go`

**Interfaces:**
- Consumes: `tool.PermissionAsker`/`PermissionAskRequest` (Task 5), `tui.DialogRuntime.RequestPermission/ApplyToScreen/ResolveScreenEvent`, `tui.PermissionRequest`, `tui.DialogResult`/`DialogResultStatus`, `tui.ScreenEventDialogAction`/`ScreenEventCancelled`.
- Produces:
  - `type loopAsker struct { askCh chan askRequest }` implementing `tool.PermissionAsker`.
  - loop handling: on `askRequest`, show dialog; on a dialog-resolving key, send the decision to `reply`.

- [ ] **Step 1: Write the failing test**

Create `internal/repl/asker_test.go`:
```go
package repl

import (
	"context"
	"testing"
	"time"

	"ccgo/internal/contracts"
	"ccgo/internal/tool"
)

func TestLoopAskerAllow(t *testing.T) {
	// User presses Enter on the default-focused "Allow" action.
	ft := NewFakeTerminal("\r", 80, 24)
	l := NewLoop(ft, nil)

	asker := loopAsker{askCh: l.askCh}
	decisionCh := make(chan contracts.PermissionDecision, 1)

	// Kick off an Ask concurrently with the loop.
	go func() {
		d, err := asker.Ask(context.Background(), tool.PermissionAskRequest{
			ToolUseID:   "u1",
			ToolName:    "Bash",
			Description: "run ls",
		})
		if err == nil {
			decisionCh <- d
		}
	}()

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	_ = l.Run(ctx)

	select {
	case d := <-decisionCh:
		if d.Behavior != contracts.PermissionAllow {
			t.Fatalf("decision = %v want allow", d.Behavior)
		}
	default:
		t.Fatal("asker never received a decision")
	}
}
```

Confirm the default-focused action label of `tui.PermissionDialog`. The extractor reported default actions `["Allow","Allow Session","Deny"]` with focus index 0 ("Allow"). If the focused action or the Enter-to-confirm chord differs, set the test input to the verified confirming key (check `grep -rn "ScreenEventDialogAction" internal/tui/` and the dialog key handling). Map "Allow" and "Allow Session" → `PermissionAllow`, "Deny" → `PermissionDeny` (see Step 3).

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/repl/ -run TestLoopAsker -v`
Expected: FAIL — `undefined: loopAsker`.

- [ ] **Step 3: Write minimal implementation**

Create `internal/repl/asker.go`:
```go
package repl

import (
	"context"

	"ccgo/internal/contracts"
	"ccgo/internal/tool"
)

// loopAsker implements tool.PermissionAsker by handing the request to the
// event loop (which renders a dialog) and blocking on the loop's reply.
type loopAsker struct {
	askCh chan askRequest
}

func (a loopAsker) Ask(ctx context.Context, req tool.PermissionAskRequest) (contracts.PermissionDecision, error) {
	reply := make(chan contracts.PermissionDecision, 1)
	select {
	case a.askCh <- askRequest{req: req, reply: reply}:
	case <-ctx.Done():
		return contracts.PermissionDecision{}, ctx.Err()
	}
	select {
	case d := <-reply:
		return d, nil
	case <-ctx.Done():
		return contracts.PermissionDecision{}, ctx.Err()
	}
}

// decisionFromAction maps a dialog action label to a permission behavior.
func decisionFromAction(action string) contracts.PermissionBehavior {
	switch action {
	case "Allow", "Allow Session":
		return contracts.PermissionAllow
	default:
		return contracts.PermissionDeny
	}
}
```

Update `internal/repl/loop.go`. Add a pending-ask field to `Loop`:
```go
	pendingAsk *askRequest
```
Handle `askCh` in the tty select loop (add a case):
```go
		case ar := <-l.askCh:
			l.showPermission(ar)
			if err := l.render(); err != nil {
				return err
			}
```
Add the dialog methods:
```go
func (l *Loop) showPermission(ar askRequest) {
	l.pendingAsk = &ar
	request := tui.PermissionRequest{
		ID:          string(ar.req.ToolUseID),
		ToolName:    ar.req.ToolName,
		Path:        ar.req.Path,
		Description: ar.req.Description,
	}
	l.dialog.RequestPermission(request)
	l.dialog.ApplyToScreen(&l.screen, l.screen.Status)
}
```
And resolve the dialog inside `handleKey` — when a dialog is active, route the event through `DialogRuntime` instead of treating it as a normal submit. Change `handleKey` to:
```go
func (l *Loop) handleKey(key tui.Key) bool {
	event := l.screen.ApplyKey(key)

	if l.pendingAsk != nil &&
		(event.Type == tui.ScreenEventDialogAction || event.Type == tui.ScreenEventCancelled) {
		result := l.dialog.ResolveScreenEvent(&l.screen, event, l.screen.Status)
		if result.Found {
			behavior := decisionFromAction(result.Action)
			if result.Status == tui.DialogResultCancelled || result.Status == tui.DialogResultDenied {
				behavior = contracts.PermissionDeny
			}
			l.pendingAsk.reply <- contracts.PermissionDecision{Behavior: behavior}
			l.pendingAsk = nil
		}
		return false
	}

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
```
Add the `"ccgo/internal/contracts"` import to loop.go if not present. Confirm the `DialogResultStatus` constant names: the extractor reported the type `DialogResultStatus` with values `""`/`"allowed"`/`"denied"`/`"cancelled"`/`"closed"`. Use the exported Go identifiers (run `go doc ./internal/tui DialogResultStatus` — likely `tui.DialogResultDenied`, `tui.DialogResultCancelled`). If they are unexported, compare against the string values instead (`string(result.Status) == "denied"`).

- [ ] **Step 4: Run tests to verify they pass**

Run: `go test ./internal/repl/ -v`
Expected: PASS. The Ask goroutine sends an `askRequest`; the loop shows the dialog; the `"\r"` key resolves it to "Allow"; `decisionFromAction` → allow; reply delivered; then FakeTerminal EOF ends the loop.

- [ ] **Step 5: Commit**

```bash
git add internal/repl/asker.go internal/repl/loop.go internal/repl/asker_test.go
git commit -m "feat(repl): bridge interactive permission dialogs to the executor Asker"
```

---

## Task 7: Wire the real runner and replace the `claude` scaffold stub

**Files:**
- Create: `internal/repl/run.go`
- Modify: `cmd/claude/main.go` (replace lines 269–275; add `interactiveRunner` helper)
- Test: `internal/repl/run_test.go`

**Interfaces:**
- Consumes: `conversation.Runner` (value), `(*conversation.Runner).RunTurn`, `messages.UserText`, `tool.Executor.Asker`, the `Loop` (Tasks 3–6).
- Produces:
  - `func RunInteractive(ctx context.Context, term Terminal, base conversation.Runner, history []contracts.Message) error` — builds the loop, sets `StartTurn` to run a real turn in a goroutine, runs the loop.
  - In main.go: `func interactiveRunner(ctx, state, cliOptions) (conversation.Runner, error)` (mirrors `headlessRunner`), and the dispatch replacing the stub.

- [ ] **Step 1: Write the failing test**

Create `internal/repl/run_test.go`:
```go
package repl

import (
	"context"
	"strings"
	"testing"
	"time"

	"ccgo/internal/contracts"
	"ccgo/internal/conversation"
)

// fakeClient is a minimal conversation.MessageClient that returns one
// assistant text message and no tool calls.
type fakeClient struct{}

func (fakeClient) /* method set per conversation.MessageClient */ {}

func TestRunInteractiveOneTurn(t *testing.T) {
	t.Skip("enable after binding fakeClient to the real conversation.MessageClient interface")

	ft := NewFakeTerminal("hello\r\x04\x04", 80, 24)
	base := conversation.Runner{ /* Client: fakeClient{}, SessionID: "s1" */ }

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	if err := RunInteractive(ctx, ft, base, nil); err != nil {
		t.Fatalf("RunInteractive err: %v", err)
	}
	if !strings.Contains(ft.Out.String(), "assistant-reply") {
		t.Fatal("expected assistant reply rendered")
	}
	_ = contracts.MessageAssistant
}
```

The exact `conversation.MessageClient` interface must be read before fleshing out `fakeClient`: run `go doc ./internal/conversation MessageClient`. Implement its methods to return a fixed assistant message, then remove the `t.Skip`. This is the one task whose full end-to-end test needs the real client interface; keep the skip until the interface is bound, but the implementation below must compile.

- [ ] **Step 2: Run test to verify it fails (compile-only)**

Run: `go test ./internal/repl/ -run TestRunInteractive -v`
Expected: FAIL to compile — `undefined: RunInteractive`.

- [ ] **Step 3: Write minimal implementation**

Create `internal/repl/run.go`:
```go
package repl

import (
	"context"

	"ccgo/internal/contracts"
	"ccgo/internal/conversation"
	"ccgo/internal/messages"
)

// RunInteractive launches the interactive REPL against a fully-wired runner.
// base must already have Client/Tools/Permissions/Model/SessionPath set
// (see interactiveRunner in cmd/claude). history seeds prior turns.
func RunInteractive(ctx context.Context, term Terminal, base conversation.Runner, history []contracts.Message) error {
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

	return loop.Run(ctx)
}
```

In `cmd/claude/main.go`, add `interactiveRunner` right after `headlessRunner` (it is identical except it does not need streaming flags; reuse the same wiring). Implement by delegating:
```go
// interactiveRunner builds a fully-wired runner for the interactive REPL.
func interactiveRunner(ctx context.Context, state *bootstrap.State, options cliOptions) (conversation.Runner, error) {
	return headlessRunner(ctx, state, options)
}
```
(`headlessRunner` already sets Client, Tools, Permissions, Model, SessionPath, BetaHeaders — everything `RunTurn` needs. A distinct function is kept as the seam for future interactive-only wiring, e.g. interactive permission mode defaults.)

Replace the scaffold stub at `cmd/claude/main.go:269-275`:
```go
	if _, err := state.ConversationRunner(); err != nil {
		fmt.Fprintf(stderr, "ccgo: %v\n", err)
		return 1
	}

	fmt.Fprintf(stdout, "ccgo scaffold ready\nsession_id=%s\ncwd=%s\n", state.SessionID(), state.CWD())
	return 0
}
```
with:
```go
	ctx := context.Background()
	runner, err := interactiveRunner(ctx, state, cliOptionsFromFlags(flags, resume, continueMode, model /* etc. */))
	if err != nil {
		fmt.Fprintf(stderr, "ccgo: %v\n", err)
		return 1
	}

	history, err := resumeHistory(state, &runner, cliOptions{Resume: *resume, Continue: *continueMode})
	if err != nil {
		fmt.Fprintf(stderr, "ccgo: %v\n", err)
		return 1
	}

	term := repl.NewOSTerminal(os.Stdin.(*os.File), os.Stdout.(*os.File))
	if err := repl.RunInteractive(ctx, term, runner, history); err != nil {
		fmt.Fprintf(stderr, "ccgo: %v\n", err)
		return 1
	}
	return 0
}
```
Notes for the implementer:
- `run()`'s signature uses `stdin io.Reader, stdout io.Writer`. For the terminal you need the concrete `*os.File`. Use `os.Stdin`/`os.Stdout` directly here (raw mode requires real fds), not the abstract `stdin`/`stdout` params. Guard with a type assertion or just reference `os.Stdin`/`os.Stdout`.
- Build the `cliOptions` the same way the `--print` branch does (reuse the existing options-construction code path around main.go:224; factor a small helper if needed rather than duplicating). Confirm the exact `cliOptions` field names with `grep -n "cliOptions{" cmd/claude/main.go`.
- Add imports: `"context"` (if not present) and `"ccgo/internal/repl"`.

- [ ] **Step 4: Build, run package tests, and smoke-test**

Run:
```bash
go build ./... && go vet ./... && go test ./internal/repl/ ./internal/tool/ -v
```
Expected: build OK, vet clean, package tests PASS.

Manual smoke test (cannot be automated — requires a real tty):
```bash
go run ./cmd/claude
# Expect: an interactive screen (not "ccgo scaffold ready"). Type a prompt,
# press Enter, see a streamed reply. Trigger a tool that needs permission,
# confirm the dialog appears and Allow/Deny works. Ctrl-D twice to exit;
# terminal must be restored (no stuck raw mode, cursor visible).
```
Non-tty regression (must not hang):
```bash
echo "" | go run ./cmd/claude   # line-mode fallback path; should not enter raw mode
```

- [ ] **Step 5: Commit**

```bash
git add internal/repl/run.go cmd/claude/main.go internal/repl/run_test.go
git commit -m "feat(claude): launch interactive REPL instead of the scaffold stub"
```

---

## Self-Review

**Spec coverage (Phase-1 goal = working interactive `claude` with live render + interactive permissions):**
- Terminal raw I/O → Task 1. ✓
- stdin→key segmentation → Task 2. ✓
- event loop + exit + non-tty fallback → Task 3. ✓
- live streaming render of a turn → Task 4. ✓
- executor Asker seam → Task 5. ✓
- interactive permission dialog → Task 6. ✓
- real runner wired + stub replaced → Task 7. ✓

**Deferred to later phases (explicitly NOT in Phase 1, by design):** "Allow Session"/persisted permission rules (needs the engine handle on the runner + settings write — see roadmap Phase 2); resize/SIGWINCH live handling; spinner/in-progress indicator; vim mode; resume/slash-command menus; rich diff/tool rendering; mid-turn interrupt (Ctrl-C abort of a running turn) — `ScreenEventInterrupted` handling is stubbed (returns to loop) and wired in Phase 2.

**Placeholder scan:** the only intentional `t.Skip` is Task 7's end-to-end test, gated on reading the real `conversation.MessageClient` interface (instructed inline). All production code is complete. The `toolResultLine` placeholder in Task 4 Step 3 is explicitly corrected within the same step.

**Type consistency:** `Loop.history`, `eventCh`, `doneCh`, `askCh`, `pendingAsk` are introduced across Tasks 3/4/6 — ensure the struct definition in Task 3 includes all fields referenced later (add `history []contracts.Message` and `pendingAsk *askRequest` when first referenced; the implementer should keep the struct definition cumulative). `PermissionAsker.Ask` signature is identical in Tasks 5, 6, 7.

**Verification-before-completion:** the assumed `tui.Message` field names (`Role`,`Text`), the dialog action labels, the `DialogResultStatus` constant identifiers, the `ScreenEventExit` chord, and the `cliOptions` field names are flagged at their point of use with the exact `go doc`/`grep` command to confirm them before writing. None are assumed silently.

---

## Phase roadmap (subsequent plans — one per subsystem, written when reached)

Phase 1 above delivers a *usable* interactive `claude`. The remaining locked scope (docs/gap-audit-2026-06-21.md §10) becomes its own plans, in dependency order:

1. **Phase 2 — Interactive completeness:** resize/SIGWINCH, spinner, Ctrl-C mid-turn interrupt, "Allow Session" + persisted rules (`Engine.ApplyUpdate` → settings write), slash-command menu, resume picker, vim wiring, rich diff/tool rendering. (~14K LOC; the bulk of "UI 全部复刻".)
2. **Phase 3 — Agent-loop wiring:** prompt-cache breakpoints, extended thinking (+`ContentBlock.Signature`), stop-reason control flow, orphaned tool_results, micro-compact wiring.
3. **Phase 4 — Auth:** OAuth callback+browser+code-exchange, `/login` `/logout`, keychain.
4. **Phase 5 — Tools:** Bash/PS prompts, WebFetch/WebSearch real impl, AskUserQuestion/EnterPlanMode/ExitPlanMode, LSPTool, cwd persistence.
5. **Phase 6 — MCP CLI + remote OAuth; commands; CLAUDE.md hierarchy + @import; rewind; hooks lifecycle.**
6. **Phase 7 — Sandbox, real local Team execution, local SDK.**
