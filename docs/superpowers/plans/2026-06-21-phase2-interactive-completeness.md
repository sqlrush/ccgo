# Interactive Completeness (Phase 2) Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Take Phase 1's *usable* REPL to **CC-parity interaction** — wire the existing-but-dead `internal/tui` library wholesale into the `internal/repl` event loop so every Claude Code screen and dialog renders and is interactive: live resize, an in-turn spinner, Ctrl-C/ESC mid-turn interrupt, the full permission dialog set with persisted "don't ask again" rules, slash-command autocomplete, a resume picker, vim-mode + mode-switch indicators, and rich rendering (StructuredDiff, tool blocks, HelpV2, status/cost/context panels, Doctor, onboarding/Trust, theme picker, `/memory` selector). **Most of this is WIRING existing components, not green-field rendering** — the audit confirms `internal/tui` (~21K LOC, 36 files) and `internal/native/color_diff.go` already exist and are tested; they were never imported into a running path.

**Architecture:** Phase 1 built `internal/repl/loop.go` — a channel-based `select` over `inputCh` / `eventCh` / `askCh` / `doneCh` driving `*tui.REPLScreen` + `*tui.DialogRuntime`. Phase 2 extends that same loop along five seams without rewriting it: (1) a **resize channel** fed by a `SIGWINCH` listener (`golang.org/x/sys/unix`, already an indirect dep) that calls `screen.Resize`; (2) a **ticker channel** that animates a `Spinner` while `l.running`; (3) routing `tui.ScreenEventInterrupted` to a per-turn `context.CancelFunc` (Phase 1 left it stubbed); (4) replacing the loop's hardcoded 3-action permission dialog with a per-tool **dialog builder** that emits the right action set and, on an "Allow always" choice, returns a `contracts.PermissionDecision` carrying `Suggestions []PermissionUpdate` that a new `settingswriter` package persists via the existing `config.WriteSettingsDocument` and the existing `permissions.Engine.ApplyUpdate` (which already returns a **new** engine); (5) **overlay screens** (slash menu, resume picker, help, theme, memory, doctor, status panels) implemented as small immutable view structs the loop renders above the transcript, each driven by `ApplyKey` and dismissed back to the REPL. The model turn still runs in a goroutine; `runner.OnEvent` posts to `eventCh`. No new rendering primitives are invented — every overlay reuses `tui.RenderDialog`/`tui.RenderMessages`/`native.BuildColorDiff`.

**Tech Stack:** Go 1.26; existing `internal/tui`, `internal/repl`, `internal/tool`, `internal/permissions`, `internal/config`, `internal/contracts`, `internal/conversation`, `internal/commands`, `internal/session`, `internal/messages`, `internal/native`, `internal/bootstrap`; `golang.org/x/term` (Phase 1) + `golang.org/x/sys/unix` (already indirect via x/term — promoted to direct; no new download). **No bubbletea/tcell/charm.**

## Global Constraints

Copied verbatim from the master roadmap (`docs/superpowers/plans/2026-06-21-00-master-roadmap.md` §6):

- **Module/toolchain:** `ccgo`, `go 1.26` (from `go.mod`).
- **Immutability (CRITICAL):** never mutate shared structs in place; return new copies. Copy the `conversation.Runner` value per turn before setting `OnEvent`/`Tools.Asker` (existing pattern). `permissions.Engine.ApplyUpdate` already returns a **new** engine — honor that.
- **Many small files:** one responsibility per file; target 150–350 lines (800 hard max).
- **Errors handled explicitly at every level; never swallow.** Terminal raw-mode `restore` and any acquired resource MUST be released on every exit path (`defer`).
- **Input validation at boundaries:** validate all external data (API responses, user input, file content, MCP server output); fail fast with clear messages.
- **No new third-party deps** unless the plan justifies it explicitly. Phase 1 added only `golang.org/x/term`. No bubbletea/tcell/charm. (Phase 2 promotes `golang.org/x/sys` from indirect to direct — it is already downloaded; no new module is fetched.)
- **Non-TTY safety:** interactive paths MUST NOT call `term.MakeRaw` when stdin/stdout isn't a tty; fall back to line mode. Tests MUST NOT depend on a real tty.
- **TDD:** every task writes a failing test first, then minimal code. Commit after each task. Run package tests with `go test ./internal/<pkg>/ -run TestName -v`; full suite `go test ./...`.
- **Verify against real code, distrust roadmap docs:** every assumed type name, field, constant, or CC behavior MUST be confirmed with `go doc`/`grep` (ccgo side) or by reading `/Users/sqlrush/agent/claude-code/src` (CC side) before writing the test — flag the exact command at the point of use, as Phase 1's plan does.
- **Security:** no hardcoded secrets; tokens in keychain not plaintext (Phase 4); sandbox flag must actually enforce (Phase 7); never leak sensitive data in errors.

### Code-verified baseline (the seams this plan builds on)

Confirmed by reading the source on 2026-06-21:

- `internal/repl/loop.go` — `Loop` struct (loop.go:30) has `term`, `screen tui.REPLScreen`, `life tui.ScreenLifecycle`, `dialog *tui.DialogRuntime`, `inputCh chan tui.Key`, `eventCh chan conversation.Event`, `askCh chan askRequest`, `doneCh chan turnOutcome`, `StartTurn func(input string)`, `history []contracts.Message`, `activeAsk *askRequest`, `askQueue []askRequest`, `onPermissionShown func()`, `onTurnDone func()`, `running bool`, `width`, `height`. The tty `select` loop is at loop.go:107-137. `handleKey` is loop.go:189; `showPermission` loop.go:243.
- `internal/repl/asker.go` — `loopAsker` (asker.go:13) and `decisionFromAction` (asker.go:37, maps "Allow"/"Allow Session"→allow else deny).
- `internal/repl/run.go` — `newTurnLoop` (run.go:13) copies the runner by value, sets `OnEvent`+`Tools.Asker`; `RunInteractive` (run.go:46) derives a cancelable child ctx.
- `internal/tui/screen.go` — `ScreenEventType` constants (screen.go:16-33) incl. `ScreenEventInterrupted` (`"interrupted"`, screen.go:20), `ScreenEventDialogAction`, `ScreenEventCancelled`, `ScreenEventExit`. `REPLScreen.Resize(width, height)` exists (screen.go:551). `NewREPLScreen(width,height,history []string) REPLScreen` (screen.go:105). `AppendMessage`/`SetMessages`/`ClearConversation` exist (screen.go:134-155). `REPLScreen.Status string`, `REPLScreen.Dialog *Dialog`, `REPLScreen.VimEnabled bool`, `REPLScreen.VimMode VimMode` are public fields (screen.go:45-103).
- `internal/tui/dialog_runtime.go` — `DialogRuntime.RequestPermission(PermissionRequest) Dialog` (dr.go:40), `ApplyToScreen(*REPLScreen, baseStatus)` (dr.go:215), `ResolveScreenEvent(...) DialogResult` (dr.go:228). `DialogResult{ID,Kind,Action,Status,Found,Stale}` (dr.go:18); `DialogResultStatus` constants `DialogResultAllowed/Denied/Cancelled/Closed` (dr.go:10-16). `permissionActionStatus(action)` (dr.go:285) classifies action strings (deny/cancel→else allowed).
- `internal/tui/dialogs.go` — `PermissionRequest{ID,ToolName,Path,Description,Actions []string}` (dialogs.go:16). `PermissionDialog(req)` defaults actions to `["Allow","Allow Session","Deny"]` when `Actions` empty (dialogs.go:34) and honors a custom `Actions` slice.
- `internal/tui/components.go` — `RenderDialog(Dialog,width) []string` (components.go:285), `RenderMessages([]Message,width) []string` (components.go:23), `RenderStatusLine`, `RenderPromptLines`.
- `internal/tui/types.go` — `Message{Role Role; Text string; ContentBlocks []contracts.ContentBlock; ...}` (types.go:17); `Role` consts `RoleUser/Assistant/System/Tool` (types.go:10-15); `Dialog{Title,Body,Actions []string,Focused int,ID,Kind}` (types.go:33); `Key{Type KeyType; Rune; ...}` (types.go:193); `KeyType` consts incl. `KeyCtrlC`, `KeyEsc`, `KeyShiftTab` (types.go:63-144).
- `internal/tui/lifecycle.go` — `ScreenLifecycle.EnterInteractive(TerminalModeOptions) string`, `ExitInteractive() string`, `ReassertInteractive(opts) string` (lifecycle.go:146).
- `internal/contracts/permissions.go` — `PermissionDecision{Behavior,...,Suggestions []PermissionUpdate, BlockedPath}` (perm.go:50); `PermissionUpdate{Type,Destination,Rules []PermissionRuleValue,Behavior,Mode,Directories}` (perm.go:64); `PermissionRuleValue{ToolName,RuleContent}` (perm.go:39); `PermissionMode` consts `PermissionDefault/AcceptEdits/BypassPermissions/Plan` (perm.go:5); behaviors `PermissionAllow/Deny/Ask` (perm.go:17).
- `internal/permissions/engine.go` — `func (e Engine) ApplyUpdate(update contracts.PermissionUpdate) (Engine, error)` returns a **new** Engine (engine.go:403); handles `"addRules"`, `"replaceRules"`, `"removeRules"`, `"setMode"`, `"addDirectories"`.
- `internal/config/user_settings.go` — `WriteUserSettingsDocument(map[string]any) error` (us.go:30), `WriteSettingsDocument(path, map[string]any) error` (us.go:34, MarshalIndent + 0o600), `ReadSettingsDocument(path) (map[string]any, error)` (us.go:17). `internal/config/paths.go` — `UserSettingsPath()` (paths.go:11), `ProjectSettingsPath(root)` returns `<root>/.claude/settings.json` (paths.go:40).
- `internal/tool/types.go` — `PermissionAskRequest{ToolUseID contracts.ID; ToolName, Path, Description string; Decision contracts.PermissionDecision}` (types.go:39); `PermissionAsker.Ask(ctx, req) (PermissionDecision, error)` (types.go:49).
- `internal/conversation/types.go` — `EventType` consts incl. `EventAssistantMessage`, `EventToolUse`, `EventToolResult`, `EventStreamEvent`, `EventToolProgress`, `EventCompact`, `EventTokenWarning` (types.go:41-51); `Event{Type, Message *contracts.Message, ToolUse *contracts.ToolUse, ToolResult *contracts.ToolResult, ...}` (types.go:93); `Runner{Permissions tool.PermissionDecider, PermissionMode contracts.PermissionMode, Tools tool.Executor, OnEvent func(Event), ...}` (types.go:109).
- `internal/commands/registry.go` — `Registry.Visible() []contracts.Command` (registry.go:166), `Find(name) (contracts.Command, bool)` (registry.go:177). `contracts.Command{Name, Aliases, DisplayName, Description, ArgumentHint, Hidden, ...}` (command.go:21). `internal/commands/slash.go` — `IsSlashInput(input) bool` (slash.go:101), `ParseSlashCommand(input) (SlashCommand, bool)` (slash.go:76).
- `internal/native/color_diff.go` — `BuildColorDiff(oldText,newText, opts ColorDiffOptions) ColorDiff` (cd.go:28).

---

## File Structure

**New files in `internal/repl/`:**
- `resize.go` — `signalResizer` (SIGWINCH→chan) + `Loop.resizeCh` handling; non-tty/no-signal safe.
- `spinner.go` — `Spinner` (frame/phrase/elapsed); pure `Frame(elapsed)` is the TDD core.
- `interrupt.go` — per-turn `context.CancelFunc` registry on `Loop`; `ScreenEventInterrupted` → cancel running turn.
- `permission_dialog.go` — `buildPermissionDialog(req) tui.PermissionRequest` per tool; `decisionForAction(req, action) contracts.PermissionDecision` (carries `Suggestions`).
- `overlay.go` — `Overlay` interface (`ApplyKey(tui.Key) (OverlayResult, bool)`, `Render(w,h) []string`) + `Loop.activeOverlay` plumbing.
- `slash_menu.go` — `SlashMenu` overlay: filter `registry.Visible()` as the prompt starts with `/`.
- `resume_picker.go` — `ResumePicker` overlay over `session` summaries.
- `help_screen.go` — `HelpScreen` overlay (HelpV2 content).
- `theme_picker.go` — `ThemePicker` overlay.
- `memory_selector.go` — `MemorySelector` overlay.
- `panels.go` — `statusPanel`/`costPanel`/`contextPanel`/`doctorReport` text builders.
- `trust_dialog.go` — `TrustDialog` overlay (first-run).
- `mode_switch.go` — `cycleMode(cur) next` + `modeIndicator(mode,vim) string`.
- `diff_render.go` — `renderToolDiff(tu *contracts.ToolUse, tr *contracts.ToolResult) (string, bool)` via `native.BuildColorDiff`.

**New package `internal/settingswriter/`:**
- `writer.go` — `Apply(update contracts.PermissionUpdate) error` (read→merge→write the right settings file) + `Destination` resolution.

**Modified existing files:**
- `internal/repl/loop.go` — add `resizeCh`, `tickCh`, `turnCancel`, `activeOverlay` fields; extend the `select`; route `ScreenEventInterrupted`; consult overlay before normal key handling.
- `internal/repl/asker.go` — replace `decisionFromAction` use with the per-tool `decisionForAction`.
- `internal/repl/render.go` — call `renderToolDiff` for edit tools.
- `internal/repl/run.go` — pass the `permissions.Engine` handle + registry + settings writer into the loop so persistence + slash menu work.
- `go.mod` — promote `golang.org/x/sys` to a direct require (no download).

---

## Task 1: Live resize (SIGWINCH) handling

**Files:**
- Create: `internal/repl/resize.go`
- Modify: `internal/repl/loop.go` (add `resizeCh`; handle it in the select)
- Test: `internal/repl/resize_test.go`
- Modify: `go.mod` (promote `golang.org/x/sys` to direct)

**Interfaces:**
- Produces:
  - `type resizeEvent struct{ Width, Height int }`
  - `func startResizeListener(ctx context.Context, t Terminal, out chan<- resizeEvent)` — installs a `SIGWINCH` handler (no-op on non-tty), and on each signal reads `t.Size()` and posts a `resizeEvent`. Returns immediately; spawns a goroutine.
  - `func (l *Loop) applyResize(ev resizeEvent)` — calls `l.screen.Resize`, updates `l.width/height`, re-renders.

Confirm the signal constant before writing: `go doc golang.org/x/sys/unix SIGWINCH` (POSIX-only; the listener file is guarded so Windows builds fall back to no signal — see Step 3 note).

- [ ] **Step 1: Write the failing test**

Create `internal/repl/resize_test.go`:
```go
package repl

import "testing"

func TestApplyResizeUpdatesScreen(t *testing.T) {
	ft := NewFakeTerminal("", 80, 24)
	l := NewLoop(ft, nil)
	if l.width != 80 || l.height != 24 {
		t.Fatalf("initial size = %dx%d want 80x24", l.width, l.height)
	}
	l.applyResize(resizeEvent{Width: 120, Height: 40})
	if l.width != 120 || l.height != 40 {
		t.Fatalf("after resize = %dx%d want 120x40", l.width, l.height)
	}
	if l.screen.Width != 120 || l.screen.Height != 40 {
		t.Fatalf("screen size = %dx%d want 120x40", l.screen.Width, l.screen.Height)
	}
}

func TestApplyResizeIgnoresNonPositive(t *testing.T) {
	ft := NewFakeTerminal("", 80, 24)
	l := NewLoop(ft, nil)
	l.applyResize(resizeEvent{Width: 0, Height: -5})
	if l.width != 80 || l.height != 24 {
		t.Fatalf("non-positive resize must be ignored, got %dx%d", l.width, l.height)
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/repl/ -run TestApplyResize -v`
Expected: FAIL — `undefined: resizeEvent` / `undefined: (*Loop).applyResize`.

- [ ] **Step 3: Write minimal implementation**

Create `internal/repl/resize.go` (POSIX build; the signal install is isolated so a `resize_windows.go` stub could no-op later — for this plan the file targets `//go:build !windows`):
```go
//go:build !windows

package repl

import (
	"context"
	"os"
	"os/signal"

	"golang.org/x/sys/unix"
)

// resizeEvent carries a new terminal size produced by a SIGWINCH.
type resizeEvent struct {
	Width  int
	Height int
}

// startResizeListener installs a SIGWINCH handler that posts the current
// terminal size to out. It is a no-op for non-tty terminals (pipes never
// resize) and returns as soon as the goroutine is started. The goroutine
// stops when ctx is cancelled.
func startResizeListener(ctx context.Context, t Terminal, out chan<- resizeEvent) {
	if !t.IsTTY() {
		return
	}
	sig := make(chan os.Signal, 1)
	signal.Notify(sig, unix.SIGWINCH)
	go func() {
		defer signal.Stop(sig)
		for {
			select {
			case <-ctx.Done():
				return
			case <-sig:
				w, h, err := t.Size()
				if err != nil || w <= 0 || h <= 0 {
					continue
				}
				select {
				case out <- resizeEvent{Width: w, Height: h}:
				case <-ctx.Done():
					return
				}
			}
		}
	}()
}

// applyResize updates the screen and cached dimensions. Non-positive sizes
// (e.g. a transient zero from a detaching tty) are ignored.
func (l *Loop) applyResize(ev resizeEvent) {
	if ev.Width <= 0 || ev.Height <= 0 {
		return
	}
	l.width = ev.Width
	l.height = ev.Height
	l.screen.Resize(ev.Width, ev.Height)
}
```

In `internal/repl/loop.go`, add the field to the `Loop` struct (after `doneCh`):
```go
	resizeCh chan resizeEvent
```
Initialize it in `NewLoop` (in the returned struct literal):
```go
		resizeCh: make(chan resizeEvent, 1),
```
Start the listener in `Run`, right after `go l.readInput(ctx)` (loop.go:101):
```go
	startResizeListener(ctx, l.term, l.resizeCh)
```
Add a case to the tty `select` (loop.go:107-137):
```go
		case rev := <-l.resizeCh:
			l.applyResize(rev)
			if err := l.render(); err != nil {
				return err
			}
```

- [ ] **Step 4: Run tests to verify they pass**

Run: `go test ./internal/repl/ -run TestApplyResize -v && go vet ./internal/repl/`
Expected: PASS; vet clean. Promote the dep: edit `go.mod` so `golang.org/x/sys vX.Y.Z` is in a direct `require` block (remove the `// indirect` comment). Confirm no download occurs: `go mod tidy && git diff go.mod go.sum` — `go.sum` must be unchanged.

- [ ] **Step 5: Commit**

```bash
git add internal/repl/resize.go internal/repl/loop.go internal/repl/resize_test.go go.mod
git commit -m "feat(repl): handle SIGWINCH live terminal resize"
```

---

## Task 2: In-turn spinner / progress indicator

**Files:**
- Create: `internal/repl/spinner.go`
- Modify: `internal/repl/loop.go` (tick channel; render spinner into `screen.Status` while running)
- Test: `internal/repl/spinner_test.go`

**CC behavior anchor:** `src/components/Spinner.tsx:166-171` builds `verb + '…'`; `src/components/Spinner/SpinnerAnimationRow.tsx:162,168,216` show elapsed seconds, token count (after a threshold), and `"(esc to interrupt)"`. We replicate the *visible string*: an animated frame + a verb + elapsed seconds + `"(esc to interrupt)"`.

**Interfaces:**
- Produces:
  - `type Spinner struct { frames []string; verb string; start time.Time }`
  - `func NewSpinner(now time.Time) Spinner`
  - `func (s Spinner) Line(now time.Time) string` — pure; `"<frame> Working… (3s · esc to interrupt)"`. Frame index derived from `now.Sub(s.start)` so it is deterministic in tests.

- [ ] **Step 1: Write the failing test**

Create `internal/repl/spinner_test.go`:
```go
package repl

import (
	"strings"
	"testing"
	"time"
)

func TestSpinnerLineDeterministic(t *testing.T) {
	start := time.Unix(1000, 0)
	s := NewSpinner(start)
	// 3.2s in: elapsed should read 3s; frame index = (3200ms/100ms) % len.
	line := s.Line(start.Add(3200 * time.Millisecond))
	if !strings.Contains(line, "3s") {
		t.Fatalf("line %q should contain elapsed 3s", line)
	}
	if !strings.Contains(line, "esc to interrupt") {
		t.Fatalf("line %q should mention esc to interrupt", line)
	}
	if !strings.Contains(line, s.verb) {
		t.Fatalf("line %q should contain verb %q", line, s.verb)
	}
}

func TestSpinnerFrameAdvances(t *testing.T) {
	start := time.Unix(0, 0)
	s := NewSpinner(start)
	a := strings.Fields(s.Line(start))[0]
	b := strings.Fields(s.Line(start.Add(100 * time.Millisecond)))[0]
	if a == b {
		t.Fatalf("frame did not advance: %q == %q", a, b)
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/repl/ -run TestSpinner -v`
Expected: FAIL — `undefined: NewSpinner`.

- [ ] **Step 3: Write minimal implementation**

Create `internal/repl/spinner.go`:
```go
package repl

import (
	"fmt"
	"time"
)

const spinnerInterval = 100 * time.Millisecond

// spinnerFrames is a Braille-dot animation; ASCII-safe in any UTF-8 terminal.
var spinnerFrames = []string{"⠋", "⠙", "⠹", "⠸", "⠼", "⠴", "⠦", "⠧", "⠇", "⠏"}

// Spinner renders an animated in-turn progress line. It is a value type; the
// Line method is pure (frame derived from elapsed time) so tests are stable.
type Spinner struct {
	frames []string
	verb   string
	start  time.Time
}

func NewSpinner(now time.Time) Spinner {
	return Spinner{frames: spinnerFrames, verb: "Working…", start: now}
}

// Line returns the status string at the given wall-clock time, e.g.
// "⠹ Working… (3s · esc to interrupt)".
func (s Spinner) Line(now time.Time) string {
	elapsed := now.Sub(s.start)
	if elapsed < 0 {
		elapsed = 0
	}
	idx := int(elapsed/spinnerInterval) % len(s.frames)
	secs := int(elapsed / time.Second)
	return fmt.Sprintf("%s %s (%ds · esc to interrupt)", s.frames[idx], s.verb, secs)
}
```

Wire it into the loop. In `internal/repl/loop.go` add fields:
```go
	tickCh  <-chan time.Time
	stopTick func()
	spinner Spinner
```
Add a helper to start/stop the ticker and base status. Add to the struct a `baseStatus string` field (the non-spinner status). In `handleKey`'s `ScreenEventPromptSubmitted` branch, after `l.StartTurn(event.Value)` and `l.running = true`, start the spinner:
```go
			l.startSpinner()
```
In `finishTurn` (loop.go:149), at the top after `l.running = false`, call:
```go
	l.stopSpinner()
```
Add the methods (new file would also be fine, but keeping with loop.go for cohesion is acceptable here since they touch private fields):
```go
func (l *Loop) startSpinner() {
	l.spinner = NewSpinner(time.Now())
	ticker := time.NewTicker(spinnerInterval)
	l.tickCh = ticker.C
	l.stopTick = ticker.Stop
	l.screen.Status = l.spinner.Line(time.Now())
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
```
Add a `case <-l.tickCh:` to the tty `select` (a `nil` channel blocks forever, so this case is inert when no turn is running):
```go
		case <-l.tickCh:
			l.tick()
			if err := l.render(); err != nil {
				return err
			}
```
Add `"time"` to loop.go imports if absent.

- [ ] **Step 4: Run tests to verify they pass**

Run: `go test ./internal/repl/ -run 'TestSpinner|TestLoop|TestRunInteractive' -v`
Expected: PASS. (The existing `TestRunInteractiveOneTurn` must still pass: the spinner only overwrites `screen.Status`, which the test does not assert.)

- [ ] **Step 5: Commit**

```bash
git add internal/repl/spinner.go internal/repl/loop.go internal/repl/spinner_test.go
git commit -m "feat(repl): animate in-turn spinner with elapsed time and interrupt hint"
```

---

## Task 3: Ctrl-C / ESC mid-turn interrupt

**Files:**
- Create: `internal/repl/interrupt.go`
- Modify: `internal/repl/loop.go` (route `ScreenEventInterrupted`), `internal/repl/run.go` (register per-turn cancel)
- Test: `internal/repl/interrupt_test.go`

Phase 1 left `tui.ScreenEventInterrupted` (screen.go:20) **stubbed** — `handleKey` ignores it. This task wires it to cancel the in-flight turn's context.

**CC behavior anchor:** `src/components/Spinner/SpinnerAnimationRow.tsx:216` ("esc to interrupt"); an `AbortController` aborts the in-progress turn. We cancel the per-turn `context.Context`.

**Interfaces:**
- Produces:
  - field `turnCancel context.CancelFunc` on `Loop`.
  - `func (l *Loop) interruptTurn()` — if a turn is running, calls `turnCancel`, appends a "Interrupted" system message, stops the spinner, clears `running`.
  - `StartTurn` signature stays `func(input string)`; the cancel is registered via a new `Loop.SetTurnCancel(context.CancelFunc)` the runner calls before launching.

Confirm the screen actually emits `ScreenEventInterrupted` for the ESC/Ctrl-C-during-turn chord: `grep -n "ScreenEventInterrupted" internal/tui/screen.go` (confirmed at screen.go:250 and :309). The screen decides; the loop reacts.

- [ ] **Step 1: Write the failing test**

Create `internal/repl/interrupt_test.go`:
```go
package repl

import (
	"context"
	"testing"
)

func TestInterruptTurnCancelsContext(t *testing.T) {
	ft := NewFakeTerminal("", 80, 24)
	l := NewLoop(ft, nil)
	_, cancel := context.WithCancel(context.Background())
	cancelled := false
	l.SetTurnCancel(func() { cancelled = true; cancel() })
	l.running = true

	l.interruptTurn()

	if !cancelled {
		t.Fatal("interruptTurn did not invoke the per-turn cancel")
	}
	if l.running {
		t.Fatal("running flag should clear after interrupt")
	}
	if l.turnCancel != nil {
		t.Fatal("turnCancel should be reset to nil after interrupt")
	}
}

func TestInterruptTurnNoopWhenIdle(t *testing.T) {
	ft := NewFakeTerminal("", 80, 24)
	l := NewLoop(ft, nil)
	// No turn running, no cancel set: must not panic.
	l.interruptTurn()
	if l.running {
		t.Fatal("running should stay false")
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/repl/ -run TestInterrupt -v`
Expected: FAIL — `undefined: (*Loop).SetTurnCancel` / `interruptTurn`.

- [ ] **Step 3: Write minimal implementation**

Create `internal/repl/interrupt.go`:
```go
package repl

import (
	"context"

	"ccgo/internal/tui"
)

// SetTurnCancel registers the cancel func for the currently launching turn.
// The runner (run.go) calls this before starting RunTurn so an ESC/Ctrl-C
// can abort the in-flight HTTP request and tool execution.
func (l *Loop) SetTurnCancel(cancel context.CancelFunc) {
	l.turnCancel = cancel
}

// interruptTurn aborts the running turn: it cancels the turn context, surfaces
// an "Interrupted" line, and resets running/spinner state. No-op when idle.
func (l *Loop) interruptTurn() {
	if !l.running {
		return
	}
	if l.turnCancel != nil {
		l.turnCancel()
		l.turnCancel = nil
	}
	l.running = false
	l.stopSpinner()
	l.screen.AppendMessage(tui.Message{Role: tui.RoleSystem, Text: "Interrupted by user."})
}
```

Add the field to `Loop` in loop.go:
```go
	turnCancel context.CancelFunc
```
Route the event in `handleKey` (loop.go:207, the `switch event.Type` block) — add a case **before** the default fallthrough:
```go
	case tui.ScreenEventInterrupted:
		l.interruptTurn()
```
In `internal/repl/run.go` `newTurnLoop`, register the cancel before launching the goroutine. Change `StartTurn` to derive a per-turn context:
```go
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
```
Note: `doneCh` still posts on the parent `ctx` (not `turnCtx`) so an interrupted turn's `turnOutcome` (carrying the abort error) is still delivered and `finishTurn` clears state. `finishTurn` must tolerate a `context.Canceled` error gracefully — it already appends `out.err.Error()` as a system message; an interrupt produces a benign "context canceled" line under the "Interrupted by user." line. To avoid the duplicate, guard `finishTurn` to skip the error message when `errors.Is(out.err, context.Canceled)`:
```go
	if out.err != nil {
		if !errors.Is(out.err, context.Canceled) {
			l.screen.AppendMessage(tui.Message{Role: tui.RoleSystem, Text: out.err.Error()})
		}
		return
	}
```
Add `"errors"` and `"context"` imports to loop.go as needed.

- [ ] **Step 4: Run tests to verify they pass**

Run: `go test ./internal/repl/ -v`
Expected: PASS (all repl tests; the existing `TestRunInteractiveCancelsTurnOnExit` continues to pass since the parent-ctx cancel still unwinds the turn).

- [ ] **Step 5: Commit**

```bash
git add internal/repl/interrupt.go internal/repl/loop.go internal/repl/run.go internal/repl/interrupt_test.go
git commit -m "feat(repl): cancel the running turn on Ctrl-C/ESC interrupt"
```

---

## Task 4: Settings writer for persisted permission rules

**Files:**
- Create: `internal/settingswriter/writer.go`
- Test: `internal/settingswriter/writer_test.go`

This task builds the persistence sink **first** (no UI yet) so Task 5 can wire "Allow always" to it. It bridges a `contracts.PermissionUpdate` to the existing `config.WriteSettingsDocument`, honoring immutability (read → new merged map → write).

**Interfaces:**
- Produces:
  - `type Writer struct { UserPath, ProjectPath string }`
  - `func New(userPath, projectPath string) Writer`
  - `func (w Writer) Apply(update contracts.PermissionUpdate) error` — validates the update, resolves the destination file (`localSettings`/`projectSettings` → ProjectPath; `userSettings` → UserPath; default → UserPath), reads the doc, merges rule strings into `permissions.allow`/`.deny`/`.ask` (per `update.Behavior`), writes back.

Confirm the settings JSON shape CC uses for permissions: `grep -rn "permissions" internal/config/schema.go` and `grep -rn "\"allow\"\|\"deny\"\|\"ask\"" internal/permissions/*.go` to confirm the `permissions.{allow,deny,ask}` arrays-of-strings layout. (The `PermissionRule.String()` form and `PermissionRuleValueToString` in `internal/permissions/` produce the canonical rule string, e.g. `Bash(git status:*)`.)

- [ ] **Step 1: Write the failing test**

Create `internal/settingswriter/writer_test.go`:
```go
package settingswriter

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"ccgo/internal/contracts"
)

func readDoc(t *testing.T, path string) map[string]any {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read %s: %v", path, err)
	}
	doc := map[string]any{}
	if err := json.Unmarshal(data, &doc); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	return doc
}

func allowList(t *testing.T, doc map[string]any) []any {
	t.Helper()
	perms, ok := doc["permissions"].(map[string]any)
	if !ok {
		t.Fatalf("permissions missing or wrong type: %#v", doc["permissions"])
	}
	list, _ := perms["allow"].([]any)
	return list
}

func TestApplyAddsUserAllowRule(t *testing.T) {
	dir := t.TempDir()
	userPath := filepath.Join(dir, "user", "settings.json")
	projPath := filepath.Join(dir, "proj", ".claude", "settings.json")
	w := New(userPath, projPath)

	err := w.Apply(contracts.PermissionUpdate{
		Type:        "addRules",
		Destination: "userSettings",
		Behavior:    contracts.PermissionAllow,
		Rules:       []contracts.PermissionRuleValue{{ToolName: "Bash", RuleContent: "git status:*"}},
	})
	if err != nil {
		t.Fatalf("Apply: %v", err)
	}
	list := allowList(t, readDoc(t, userPath))
	if len(list) != 1 || list[0] != "Bash(git status:*)" {
		t.Fatalf("allow = %#v want [Bash(git status:*)]", list)
	}
}

func TestApplyProjectDestinationAndDedup(t *testing.T) {
	dir := t.TempDir()
	userPath := filepath.Join(dir, "user", "settings.json")
	projPath := filepath.Join(dir, "proj", ".claude", "settings.json")
	w := New(userPath, projPath)

	upd := contracts.PermissionUpdate{
		Type:        "addRules",
		Destination: "projectSettings",
		Behavior:    contracts.PermissionAllow,
		Rules:       []contracts.PermissionRuleValue{{ToolName: "Read"}},
	}
	if err := w.Apply(upd); err != nil {
		t.Fatalf("Apply 1: %v", err)
	}
	if err := w.Apply(upd); err != nil { // idempotent: no duplicate
		t.Fatalf("Apply 2: %v", err)
	}
	list := allowList(t, readDoc(t, projPath))
	if len(list) != 1 || list[0] != "Read" {
		t.Fatalf("allow = %#v want exactly [Read]", list)
	}
}

func TestApplyRejectsEmptyRules(t *testing.T) {
	w := New(filepath.Join(t.TempDir(), "s.json"), filepath.Join(t.TempDir(), "p.json"))
	err := w.Apply(contracts.PermissionUpdate{Type: "addRules", Behavior: contracts.PermissionAllow})
	if err == nil {
		t.Fatal("expected error for update with no rules")
	}
}
```

Confirm the canonical rule-string form before finalizing: `go doc ./internal/permissions PermissionRuleValueToString` and `grep -n "func PermissionRuleValueToString" internal/permissions/*.go`. The expected output for `{ToolName:"Bash", RuleContent:"git status:*"}` is `Bash(git status:*)` and for `{ToolName:"Read"}` is `Read`. If the helper is unexported, replicate its (tiny) format in `writer.go` rather than importing test internals — but prefer reusing the exported helper.

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/settingswriter/ -v`
Expected: FAIL — package does not exist.

- [ ] **Step 3: Write minimal implementation**

Create `internal/settingswriter/writer.go`:
```go
package settingswriter

import (
	"fmt"

	"ccgo/internal/config"
	"ccgo/internal/contracts"
	"ccgo/internal/permissions"
)

// Writer persists permission-rule updates to the appropriate settings.json.
type Writer struct {
	UserPath    string
	ProjectPath string
}

func New(userPath, projectPath string) Writer {
	return Writer{UserPath: userPath, ProjectPath: projectPath}
}

// Apply persists a permission-rule update. Only rule-add updates are handled
// here (mode/directory updates are session-scoped and not persisted by the
// dialog). It reads the destination doc, merges the canonical rule strings
// into the matching behavior list, and writes a new doc (no in-place mutation
// of caller data).
func (w Writer) Apply(update contracts.PermissionUpdate) error {
	if len(update.Rules) == 0 {
		return fmt.Errorf("settingswriter: update has no rules")
	}
	key, err := behaviorKey(update.Behavior)
	if err != nil {
		return err
	}
	path := w.destinationPath(update.Destination)
	doc, err := config.ReadSettingsDocument(path)
	if err != nil {
		return fmt.Errorf("settingswriter: read %s: %w", path, err)
	}
	perms := asMap(doc["permissions"])
	existing := asStringSet(perms[key])
	for _, value := range update.Rules {
		rule := permissions.PermissionRuleValueToString(value)
		if _, ok := existing[rule]; ok {
			continue
		}
		existing[rule] = struct{}{}
	}
	perms[key] = sortedKeys(existing)
	doc["permissions"] = perms
	return config.WriteSettingsDocument(path, doc)
}

func (w Writer) destinationPath(destination string) string {
	switch destination {
	case string(contracts.PermissionSourceProjectSettings), string(contracts.PermissionSourceLocalSettings):
		return w.ProjectPath
	default:
		return w.UserPath
	}
}

func behaviorKey(behavior contracts.PermissionBehavior) (string, error) {
	switch behavior {
	case contracts.PermissionAllow:
		return "allow", nil
	case contracts.PermissionDeny:
		return "deny", nil
	case contracts.PermissionAsk:
		return "ask", nil
	default:
		return "", fmt.Errorf("settingswriter: unsupported behavior %q", behavior)
	}
}

func asMap(v any) map[string]any {
	if m, ok := v.(map[string]any); ok {
		return m
	}
	return map[string]any{}
}

func asStringSet(v any) map[string]struct{} {
	out := map[string]struct{}{}
	if list, ok := v.([]any); ok {
		for _, item := range list {
			if s, ok := item.(string); ok {
				out[s] = struct{}{}
			}
		}
	}
	return out
}

func sortedKeys(set map[string]struct{}) []any {
	keys := make([]string, 0, len(set))
	for k := range set {
		keys = append(keys, k)
	}
	sortStrings(keys)
	out := make([]any, len(keys))
	for i, k := range keys {
		out[i] = k
	}
	return out
}
```

Create `internal/settingswriter/sort.go` (keep the std-lib import isolated; or just import `"sort"` directly in writer.go — either is fine, but a tiny file keeps writer.go focused):
```go
package settingswriter

import "sort"

func sortStrings(s []string) { sort.Strings(s) }
```

If `permissions.PermissionRuleValueToString` is unexported, replace the call with the equivalent inline format confirmed in Step 1 (e.g. `func ruleString(v contracts.PermissionRuleValue) string` that returns `v.ToolName` when `RuleContent==""`, else `v.ToolName+"("+v.RuleContent+")"`). Confirm with the grep in Step 1.

- [ ] **Step 4: Run tests to verify they pass**

Run: `go test ./internal/settingswriter/ -v`
Expected: PASS.

- [ ] **Step 5: Commit**

```bash
git add internal/settingswriter/
git commit -m "feat(settingswriter): persist permission rules to settings.json"
```

---

## Task 5: Full permission dialog set + "Allow always" persistence wiring

**Files:**
- Create: `internal/repl/permission_dialog.go`
- Modify: `internal/repl/loop.go` (`showPermission` uses per-tool actions; resolve "always" → persist + ApplyUpdate), `internal/repl/asker.go` (replace `decisionFromAction`)
- Test: `internal/repl/permission_dialog_test.go`

**CC behavior anchors (action sets):**
- Bash: `src/components/permissions/BashPermissionRequest/bashToolUseOptions.tsx:65-143` — Yes / "Yes, and don't ask again for: <prefix>" / No.
- FileEdit & FileWrite: `src/components/permissions/FilePermissionDialog/permissionOptions.tsx:87-150` — Yes / "Yes, allow all edits during this session" / "Yes, allow all edits in <dir>/" / No.
- WebFetch: `src/components/permissions/WebFetchPermissionRequest/WebFetchPermissionRequest.tsx:76-104` — Yes / "Yes, and don't ask again for <hostname>" / No.
- PowerShell mirrors Bash; Skill / NotebookEdit / SedEdit / Filesystem mirror the file pattern; AskUserQuestion / EnterPlanMode / ExitPlanMode are plan/ask ceremonies (Phase 5 supplies the tools; Phase 2 renders the dialog).

We map all of these to **three canonical actions** plus a per-tool "scope" label so the persist path knows what rule to write: `Allow once` / `Allow always` / `Deny`. The "always" label text is tool-specific (purely cosmetic), but the resulting `PermissionDecision` carries `Suggestions` describing the rule to persist.

**Interfaces:**
- Produces:
  - `type permActions struct { Actions []string; AlwaysIndex int }`
  - `func permissionActions(req tool.PermissionAskRequest) permActions` — returns the action list per tool; `AlwaysIndex` is the index of the "always" action (or -1).
  - `func decisionForAction(req tool.PermissionAskRequest, action string) contracts.PermissionDecision` — "Deny"→deny; "Allow once"→allow; "Allow always…"→allow + `Suggestions` (a single `addRules` update to `localSettings` with a rule derived from the tool name + `req.Path`).

- [ ] **Step 1: Write the failing test**

Create `internal/repl/permission_dialog_test.go`:
```go
package repl

import (
	"testing"

	"ccgo/internal/contracts"
	"ccgo/internal/tool"
)

func TestPermissionActionsBash(t *testing.T) {
	pa := permissionActions(tool.PermissionAskRequest{ToolName: "Bash", Description: "run git status"})
	if len(pa.Actions) != 3 {
		t.Fatalf("Bash actions = %v want 3", pa.Actions)
	}
	if pa.AlwaysIndex < 0 || pa.AlwaysIndex >= len(pa.Actions) {
		t.Fatalf("AlwaysIndex %d out of range", pa.AlwaysIndex)
	}
}

func TestDecisionForActionAllowOnce(t *testing.T) {
	pa := permissionActions(tool.PermissionAskRequest{ToolName: "Read", Path: "/tmp/a"})
	d := decisionForAction(tool.PermissionAskRequest{ToolName: "Read", Path: "/tmp/a"}, pa.Actions[0])
	if d.Behavior != contracts.PermissionAllow {
		t.Fatalf("allow-once behavior = %v want allow", d.Behavior)
	}
	if len(d.Suggestions) != 0 {
		t.Fatalf("allow-once must not carry persistence suggestions: %#v", d.Suggestions)
	}
}

func TestDecisionForActionAllowAlwaysCarriesSuggestion(t *testing.T) {
	req := tool.PermissionAskRequest{ToolName: "Read", Path: "/tmp/a"}
	pa := permissionActions(req)
	d := decisionForAction(req, pa.Actions[pa.AlwaysIndex])
	if d.Behavior != contracts.PermissionAllow {
		t.Fatalf("always behavior = %v want allow", d.Behavior)
	}
	if len(d.Suggestions) != 1 {
		t.Fatalf("always must carry exactly one suggestion, got %d", len(d.Suggestions))
	}
	s := d.Suggestions[0]
	if s.Type != "addRules" || s.Behavior != contracts.PermissionAllow {
		t.Fatalf("suggestion = %+v want addRules/allow", s)
	}
	if len(s.Rules) != 1 || s.Rules[0].ToolName != "Read" {
		t.Fatalf("suggestion rule = %+v want Read", s.Rules)
	}
}

func TestDecisionForActionDeny(t *testing.T) {
	req := tool.PermissionAskRequest{ToolName: "Bash"}
	d := decisionForAction(req, "Deny")
	if d.Behavior != contracts.PermissionDeny {
		t.Fatalf("deny behavior = %v", d.Behavior)
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/repl/ -run 'TestPermissionActions|TestDecisionForAction' -v`
Expected: FAIL — `undefined: permissionActions`.

- [ ] **Step 3: Write minimal implementation**

Create `internal/repl/permission_dialog.go`:
```go
package repl

import (
	"fmt"

	"ccgo/internal/contracts"
	"ccgo/internal/tool"
)

const (
	actionAllowOnce = "Allow once"
	actionDeny      = "Deny"
)

// permActions is the action set for a tool's permission dialog plus the index
// of the persistence ("always") action.
type permActions struct {
	Actions     []string
	AlwaysIndex int
}

// permissionActions returns the per-tool dialog actions. All tools currently
// share the canonical Allow-once / Allow-always / Deny shape; the always-label
// text is tool-specific for parity with CC, but the persisted rule is uniform.
func permissionActions(req tool.PermissionAskRequest) permActions {
	always := alwaysLabel(req)
	return permActions{
		Actions:     []string{actionAllowOnce, always, actionDeny},
		AlwaysIndex: 1,
	}
}

func alwaysLabel(req tool.PermissionAskRequest) string {
	switch req.ToolName {
	case "Bash", "PowerShell":
		return "Allow always for this command"
	case "WebFetch":
		return "Allow always for this host"
	case "Edit", "Write", "FileEdit", "FileWrite", "NotebookEdit", "SedEdit", "Filesystem":
		return "Allow always for this session"
	default:
		return "Allow always for this tool"
	}
}

// decisionForAction maps a chosen action label to a PermissionDecision. The
// "always" action additionally carries a Suggestions update the loop persists.
func decisionForAction(req tool.PermissionAskRequest, action string) contracts.PermissionDecision {
	switch action {
	case actionDeny:
		return contracts.PermissionDecision{Behavior: contracts.PermissionDeny}
	case actionAllowOnce:
		return contracts.PermissionDecision{Behavior: contracts.PermissionAllow}
	default:
		// Any non-deny, non-once action is the tool-specific "always" label.
		return contracts.PermissionDecision{
			Behavior:    contracts.PermissionAllow,
			Suggestions: []contracts.PermissionUpdate{persistUpdate(req)},
		}
	}
}

// persistUpdate builds the addRules update for an "always" choice. Rule content
// is the path/host scope when available; the rule defaults to the bare tool
// when no narrower scope exists (matching CC's tool-level allow).
func persistUpdate(req tool.PermissionAskRequest) contracts.PermissionUpdate {
	rule := contracts.PermissionRuleValue{ToolName: req.ToolName}
	if scope := persistScope(req); scope != "" {
		rule.RuleContent = scope
	}
	return contracts.PermissionUpdate{
		Type:        "addRules",
		Destination: string(contracts.PermissionSourceLocalSettings),
		Behavior:    contracts.PermissionAllow,
		Rules:       []contracts.PermissionRuleValue{rule},
	}
}

func persistScope(req tool.PermissionAskRequest) string {
	// Prefer a rule suggested by the permission engine if present.
	if len(req.Decision.Suggestions) > 0 {
		for _, s := range req.Decision.Suggestions {
			if len(s.Rules) > 0 && s.Rules[0].RuleContent != "" {
				return s.Rules[0].RuleContent
			}
		}
	}
	if req.Path != "" {
		return fmt.Sprintf("%s", req.Path)
	}
	return ""
}
```

Now wire the loop. In `internal/repl/loop.go` `showPermission` (loop.go:243), build the dialog with the per-tool actions:
```go
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
	l.dialog.ApplyToScreen(&l.screen, l.baseStatus)
	if l.onPermissionShown != nil {
		l.onPermissionShown()
	}
}
```
And change `handleKey`'s dialog-resolution branch (loop.go:192-205) to compute the decision via `decisionForAction` and persist the suggestion:
```go
	if l.activeAsk != nil &&
		(event.Type == tui.ScreenEventDialogAction || event.Type == tui.ScreenEventCancelled) {
		result := l.dialog.ResolveScreenEvent(&l.screen, event, l.baseStatus)
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
```
Add the persistence method to loop.go (using the new `Loop.perms` engine handle and `Loop.settings` writer added below):
```go
// persistDecision applies any rule suggestions carried by an "always" choice:
// it updates the live permission engine (immutably) and writes settings.json.
func (l *Loop) persistDecision(decision contracts.PermissionDecision) {
	for _, update := range decision.Suggestions {
		if l.settings != nil {
			if err := l.settings.Apply(update); err != nil {
				l.screen.AppendMessage(tui.Message{Role: tui.RoleSystem, Text: "failed to save permission rule: " + err.Error()})
			}
		}
		if l.onRulePersisted != nil {
			l.onRulePersisted(update)
		}
	}
}
```
Add fields to `Loop`:
```go
	settings        ruleWriter
	onRulePersisted func(contracts.PermissionUpdate) // test seam
```
Define the seam interface in loop.go (small interface, defined where used per Go style):
```go
// ruleWriter persists a permission-rule update. settingswriter.Writer satisfies it.
type ruleWriter interface {
	Apply(update contracts.PermissionUpdate) error
}
```
Add a setter (called from run.go in Task 13):
```go
func (l *Loop) SetSettingsWriter(w ruleWriter) { l.settings = w }
```
Remove `decisionFromAction` from asker.go (now superseded) — but keep `decisionFromAction`'s tiny test `TestDecisionFromAction` only if the function remains; since we delete the function, delete that test case too. Confirm no other caller: `grep -rn "decisionFromAction" internal/repl/`. (If anything else references it, keep it; otherwise delete both the function and its test.)

- [ ] **Step 4: Run tests to verify they pass**

Run: `go test ./internal/repl/ -v`
Expected: PASS. The Phase-1 asker tests (`TestLoopAskerAllow`/`Deny`) still pass: "Allow once" and the always-label both map to `PermissionAllow`; "Deny" maps to `PermissionDeny`. If `TestLoopAskerAllow` typed `"\r"` to confirm the *first* action (now "Allow once"), it still yields allow. Verify the focused-action chord still confirms action 0: `grep -n "Focused" internal/tui/screen.go internal/tui/dialogs.go`.

- [ ] **Step 5: Commit**

```bash
git add internal/repl/permission_dialog.go internal/repl/loop.go internal/repl/asker.go internal/repl/permission_dialog_test.go
git commit -m "feat(repl): per-tool permission dialogs with persisted allow-always rules"
```

---

## Task 6: Rich tool rendering (StructuredDiff for edits, tool blocks)

**Files:**
- Create: `internal/repl/diff_render.go`
- Modify: `internal/repl/render.go` (use diff render for edit tools)
- Test: `internal/repl/diff_render_test.go`

**CC behavior anchor:** `src/components/StructuredDiff.tsx:95-150` (gutter line numbers, +/- coloring). ccgo already has `internal/native/color_diff.go:28 BuildColorDiff` — reuse it, do not reimplement.

**Interfaces:**
- Produces:
  - `func renderToolResultText(tu *contracts.ToolUse, tr *contracts.ToolResult) string` — for Edit/Write tools with `old_string`/`new_string` (or `content`) in the tool input, render a colored unified diff; otherwise a one-line `⎿ ok/error` summary.

Confirm the edit tool input field names: `grep -rn "old_string\|new_string\|file_path\|\"content\"" internal/tools/file/*.go | head`. Confirm `BuildColorDiff` signature and `ColorDiffOptions` fields: `go doc ./internal/native BuildColorDiff` and `go doc ./internal/native ColorDiffOptions`.

- [ ] **Step 1: Write the failing test**

Create `internal/repl/diff_render_test.go`:
```go
package repl

import (
	"encoding/json"
	"strings"
	"testing"

	"ccgo/internal/contracts"
)

func TestRenderToolResultTextEditShowsDiff(t *testing.T) {
	tu := &contracts.ToolUse{
		ID:   "t1",
		Name: "Edit",
		Input: json.RawMessage(`{"file_path":"/x.go","old_string":"foo","new_string":"bar"}`),
	}
	tr := &contracts.ToolResult{ToolUseID: "t1"}
	out := renderToolResultText(tu, tr)
	if !strings.Contains(out, "foo") || !strings.Contains(out, "bar") {
		t.Fatalf("diff render missing old/new text: %q", out)
	}
}

func TestRenderToolResultTextNonEditSummary(t *testing.T) {
	tu := &contracts.ToolUse{ID: "t2", Name: "Read", Input: json.RawMessage(`{}`)}
	tr := &contracts.ToolResult{ToolUseID: "t2"}
	out := renderToolResultText(tu, tr)
	if strings.Contains(out, "foo") {
		t.Fatalf("non-edit should not diff: %q", out)
	}
	if out == "" {
		t.Fatal("non-edit should still produce a summary line")
	}
}

func TestRenderToolResultTextError(t *testing.T) {
	tu := &contracts.ToolUse{ID: "t3", Name: "Read", Input: json.RawMessage(`{}`)}
	tr := &contracts.ToolResult{ToolUseID: "t3", IsError: true}
	out := renderToolResultText(tu, tr)
	if !strings.Contains(strings.ToLower(out), "error") {
		t.Fatalf("error result should mention error: %q", out)
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/repl/ -run TestRenderToolResultText -v`
Expected: FAIL — `undefined: renderToolResultText`.

- [ ] **Step 3: Write minimal implementation**

Create `internal/repl/diff_render.go`:
```go
package repl

import (
	"encoding/json"

	"ccgo/internal/contracts"
	"ccgo/internal/native"
)

type editInput struct {
	FilePath  string `json:"file_path"`
	OldString string `json:"old_string"`
	NewString string `json:"new_string"`
	Content   string `json:"content"`
}

// renderToolResultText renders a tool result for the transcript. Edit/Write
// tools get a colored unified diff (via native.BuildColorDiff); everything
// else gets a concise ok/error summary line.
func renderToolResultText(tu *contracts.ToolUse, tr *contracts.ToolResult) string {
	if tu != nil && isEditTool(tu.Name) {
		if diff, ok := editDiff(tu); ok {
			return diff
		}
	}
	if tr != nil && tr.IsError {
		return "  ⎿ error"
	}
	return "  ⎿ ok"
}

func isEditTool(name string) bool {
	switch name {
	case "Edit", "Write", "MultiEdit", "NotebookEdit", "SedEdit":
		return true
	default:
		return false
	}
}

func editDiff(tu *contracts.ToolUse) (string, bool) {
	var in editInput
	if err := json.Unmarshal(tu.Input, &in); err != nil {
		return "", false
	}
	oldText := in.OldString
	newText := in.NewString
	if newText == "" && in.Content != "" { // Write tool: whole-file content
		newText = in.Content
	}
	if oldText == "" && newText == "" {
		return "", false
	}
	diff := native.BuildColorDiff(oldText, newText, native.ColorDiffOptions{Path: in.FilePath})
	return diff.Text, true
}
```

Adjust the `native.ColorDiffOptions` field/`ColorDiff` result accessor to match the real type confirmed in Step 1 (`go doc ./internal/native ColorDiff` for the result field — likely `.Text` or `.Unified`; if different, fix the return). Do not invent fields.

In `internal/repl/render.go`, change the `EventToolResult` case to use the richer renderer. The Phase-1 `messageFromEvent` switch maps `EventToolResult` — update it so it carries the tool-use context. Since `conversation.Event` for a tool result includes `ev.ToolResult` but not the originating `ev.ToolUse`, track the last tool-use in the loop. Add to `Loop` a `lastToolUse *contracts.ToolUse` field; in `applyEvent`, when `ev.Type == conversation.EventToolUse`, set `l.lastToolUse = ev.ToolUse`. Then change the tool-result rendering to call `renderToolResultText(l.lastToolUse, ev.ToolResult)`:
```go
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
```
Keep `messageFromEvent`'s existing `EventToolResult` branch (it is now unreachable from `applyEvent` but still unit-tested); to avoid dead code, remove the `EventToolResult` case from `messageFromEvent` and update the Phase-1 `render_test.go` tests `TestMessageFromEventToolResult`/`Error` to call `renderToolResultText` instead. Confirm both Phase-1 tests are updated to the new entry point.

- [ ] **Step 4: Run tests to verify they pass**

Run: `go test ./internal/repl/ -v`
Expected: PASS (incl. updated Phase-1 render tests).

- [ ] **Step 5: Commit**

```bash
git add internal/repl/diff_render.go internal/repl/render.go internal/repl/loop.go internal/repl/diff_render_test.go internal/repl/render_test.go
git commit -m "feat(repl): render edit tool results as colored structured diffs"
```

---

## Task 7: Overlay framework + slash-command menu / autocomplete

**Files:**
- Create: `internal/repl/overlay.go`, `internal/repl/slash_menu.go`
- Modify: `internal/repl/loop.go` (overlay routing)
- Test: `internal/repl/slash_menu_test.go`

**CC behavior anchor:** `src/components/PromptInput/PromptInputHelpMenu.tsx:1-100` — when the prompt starts with `/`, a menu of matching commands appears; typing filters; arrows navigate; Enter selects.

**Interfaces:**
- Produces:
  - `type OverlayResult struct { Submit string; Dismissed bool }`
  - `type Overlay interface { ApplyKey(key tui.Key) (OverlayResult, bool); Render(width, height int) []string }`
  - `type SlashMenu struct { all []contracts.Command; filtered []contracts.Command; query string; cursor int }`
  - `func NewSlashMenu(cmds []contracts.Command, query string) *SlashMenu`
  - `func (m *SlashMenu) ApplyKey(key tui.Key) (OverlayResult, bool)` — Up/Down move cursor; Enter returns `Submit:"/name"`; Esc dismisses; rune keys extend query and refilter.

- [ ] **Step 1: Write the failing test**

Create `internal/repl/slash_menu_test.go`:
```go
package repl

import (
	"testing"

	"ccgo/internal/contracts"
	"ccgo/internal/tui"
)

func sampleCommands() []contracts.Command {
	return []contracts.Command{
		{Name: "help", Description: "Show help"},
		{Name: "clear", Description: "Clear conversation"},
		{Name: "compact", Description: "Compact"},
		{Name: "config", Description: "Config"},
	}
}

func TestSlashMenuFiltersByPrefix(t *testing.T) {
	m := NewSlashMenu(sampleCommands(), "c")
	if len(m.filtered) != 3 { // clear, compact, config
		t.Fatalf("filtered = %d want 3 (%v)", len(m.filtered), m.filtered)
	}
}

func TestSlashMenuEnterSubmitsSelected(t *testing.T) {
	m := NewSlashMenu(sampleCommands(), "co") // compact, config
	res, _ := m.ApplyKey(tui.Key{Type: tui.KeyDown}) // move to config
	_ = res
	res, _ = m.ApplyKey(tui.Key{Type: tui.KeyEnter})
	if res.Submit != "/config" {
		t.Fatalf("submit = %q want /config", res.Submit)
	}
}

func TestSlashMenuEscDismisses(t *testing.T) {
	m := NewSlashMenu(sampleCommands(), "")
	res, _ := m.ApplyKey(tui.Key{Type: tui.KeyEsc})
	if !res.Dismissed {
		t.Fatal("Esc should dismiss the slash menu")
	}
}
```

Confirm key-type constant names: `grep -n "KeyDown\|KeyUp\|KeyEnter\|KeyEsc" internal/tui/types.go` (confirmed types.go:75-83).

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/repl/ -run TestSlashMenu -v`
Expected: FAIL — `undefined: NewSlashMenu`.

- [ ] **Step 3: Write minimal implementation**

Create `internal/repl/overlay.go`:
```go
package repl

import "ccgo/internal/tui"

// OverlayResult is the outcome of feeding a key to an overlay.
type OverlayResult struct {
	// Submit, when non-empty, is text to inject into the prompt/turn pipeline
	// after the overlay closes (e.g. a selected "/command").
	Submit string
	// Dismissed signals the overlay should close with no submission.
	Dismissed bool
}

// Overlay is a modal view rendered above the transcript. It owns its own key
// handling; the loop closes it when ApplyKey reports Submit or Dismissed.
type Overlay interface {
	ApplyKey(key tui.Key) (result OverlayResult, handled bool)
	Render(width, height int) []string
}
```

Create `internal/repl/slash_menu.go`:
```go
package repl

import (
	"strings"

	"ccgo/internal/contracts"
	"ccgo/internal/tui"
)

// SlashMenu is an overlay listing slash commands filtered by a typed query.
type SlashMenu struct {
	all      []contracts.Command
	filtered []contracts.Command
	query    string
	cursor   int
}

func NewSlashMenu(cmds []contracts.Command, query string) *SlashMenu {
	m := &SlashMenu{all: cmds, query: query}
	m.refilter()
	return m
}

func (m *SlashMenu) refilter() {
	q := strings.ToLower(strings.TrimSpace(m.query))
	m.filtered = m.filtered[:0]
	for _, cmd := range m.all {
		if cmd.Hidden {
			continue
		}
		if q == "" || strings.HasPrefix(strings.ToLower(cmd.Name), q) {
			m.filtered = append(m.filtered, cmd)
		}
	}
	if m.cursor >= len(m.filtered) {
		m.cursor = len(m.filtered) - 1
	}
	if m.cursor < 0 {
		m.cursor = 0
	}
}

func (m *SlashMenu) ApplyKey(key tui.Key) (OverlayResult, bool) {
	switch key.Type {
	case tui.KeyEsc:
		return OverlayResult{Dismissed: true}, true
	case tui.KeyUp:
		if m.cursor > 0 {
			m.cursor--
		}
		return OverlayResult{}, true
	case tui.KeyDown:
		if m.cursor < len(m.filtered)-1 {
			m.cursor++
		}
		return OverlayResult{}, true
	case tui.KeyEnter:
		if len(m.filtered) == 0 {
			return OverlayResult{Dismissed: true}, true
		}
		return OverlayResult{Submit: "/" + m.filtered[m.cursor].Name}, true
	case tui.KeyBackspace:
		if len(m.query) > 0 {
			m.query = m.query[:len(m.query)-1]
			m.refilter()
		}
		return OverlayResult{}, true
	case tui.KeyRune:
		m.query += string(key.Rune)
		m.refilter()
		return OverlayResult{}, true
	default:
		return OverlayResult{}, false
	}
}

func (m *SlashMenu) Render(width, height int) []string {
	lines := []string{"Commands (" + m.query + "):"}
	max := height - 2
	if max < 1 {
		max = 1
	}
	for i, cmd := range m.filtered {
		if i >= max {
			break
		}
		marker := "  "
		if i == m.cursor {
			marker = "> "
		}
		line := marker + "/" + cmd.Name
		if cmd.Description != "" {
			line += " — " + cmd.Description
		}
		lines = append(lines, tui.Truncate(line, width))
	}
	return lines
}
```

Confirm a truncation helper exists in `tui`: `grep -rn "func Truncate\|func padOrTrim\|func TerminalVisibleWidth" internal/tui/*.go`. `padOrTrim` is unexported (components.go); if no exported truncate exists, drop the `tui.Truncate(...)` call and just append `line` (the loop's renderer wraps). Use whichever the grep confirms; do not invent `tui.Truncate`.

Wire the loop. Add `activeOverlay Overlay` and `registry []contracts.Command` fields to `Loop`. In `handleKey`, before applying the key to the screen, check the overlay:
```go
func (l *Loop) handleKey(key tui.Key) bool {
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
	// ... (existing dialog + switch logic) ...
}
```
Open the slash menu when the user types `/` as the first prompt character. The simplest deterministic trigger: when an `ApplyKey` produces no special event and the prompt text equals `"/"`, open the menu. Add after the existing `switch` in `handleKey`:
```go
	if l.activeOverlay == nil && l.registry != nil && l.screen.Prompt.Text == "/" {
		l.activeOverlay = NewSlashMenu(l.registry, "")
	}
```
Confirm `screen.Prompt.Text` exists: `go doc ./internal/tui PromptState` (the `Text` field is referenced throughout components.go, e.g. components.go:162). Render the overlay above the transcript in `render()`:
```go
func (l *Loop) render() error {
	if l.activeOverlay != nil {
		lines := l.activeOverlay.Render(l.width, l.height)
		return l.term.WriteString(l.life.ReassertInteractive(tui.TerminalModeOptions{}) + strings.Join(lines, "\r\n") + "\r\n")
	}
	return l.term.WriteString(l.screen.Render())
}
```
Add a `SetRegistry([]contracts.Command)` setter for run.go to call.

- [ ] **Step 4: Run tests to verify they pass**

Run: `go test ./internal/repl/ -v`
Expected: PASS.

- [ ] **Step 5: Commit**

```bash
git add internal/repl/overlay.go internal/repl/slash_menu.go internal/repl/loop.go internal/repl/slash_menu_test.go
git commit -m "feat(repl): overlay framework and slash-command autocomplete menu"
```

---

## Task 8: Resume / continue picker overlay

**Files:**
- Create: `internal/repl/resume_picker.go`
- Test: `internal/repl/resume_picker_test.go`

**CC behavior anchor:** `src/screens/ResumeConversation.tsx:87-100` + `src/components/LogSelector.tsx:129-161` — a list of prior sessions (summary · timestamp · project path); Up/Down navigate, Enter selects.

**Interfaces:**
- Produces:
  - `type ResumeEntry struct { ID, Summary, ProjectPath string; ModifiedUnix int64 }`
  - `type ResumePicker struct { entries []ResumeEntry; cursor int }`
  - `func NewResumePicker(entries []ResumeEntry) *ResumePicker`
  - `func (p *ResumePicker) Selected() (ResumeEntry, bool)`
  - `ApplyKey`/`Render` (Overlay): Enter returns `Submit:"resume:<id>"`; Esc dismisses.

Confirm what session-listing data exists: `grep -rn "func.*List\|Summary\|ModTime\|type.*Session" internal/session/*.go | grep -iv test | head`. Reuse the existing session-summary lister (e.g. transcript discovery) rather than re-walking the FS — cite the exact function found.

- [ ] **Step 1: Write the failing test**

Create `internal/repl/resume_picker_test.go`:
```go
package repl

import (
	"testing"

	"ccgo/internal/tui"
)

func sampleResumeEntries() []ResumeEntry {
	return []ResumeEntry{
		{ID: "s1", Summary: "fix bug", ProjectPath: "/a", ModifiedUnix: 200},
		{ID: "s2", Summary: "add feature", ProjectPath: "/b", ModifiedUnix: 100},
	}
}

func TestResumePickerEnterSelects(t *testing.T) {
	p := NewResumePicker(sampleResumeEntries())
	p.ApplyKey(tui.Key{Type: tui.KeyDown})
	res, _ := p.ApplyKey(tui.Key{Type: tui.KeyEnter})
	if res.Submit != "resume:s2" {
		t.Fatalf("submit = %q want resume:s2", res.Submit)
	}
}

func TestResumePickerEscDismisses(t *testing.T) {
	p := NewResumePicker(sampleResumeEntries())
	res, _ := p.ApplyKey(tui.Key{Type: tui.KeyEsc})
	if !res.Dismissed {
		t.Fatal("Esc should dismiss")
	}
}

func TestResumePickerRenderShowsSummaries(t *testing.T) {
	p := NewResumePicker(sampleResumeEntries())
	lines := p.Render(80, 24)
	joined := ""
	for _, l := range lines {
		joined += l
	}
	if !contains(joined, "fix bug") || !contains(joined, "add feature") {
		t.Fatalf("render missing summaries: %q", joined)
	}
}

func contains(s, sub string) bool { return len(s) >= len(sub) && index(s, sub) >= 0 }
func index(s, sub string) int {
	for i := 0; i+len(sub) <= len(s); i++ {
		if s[i:i+len(sub)] == sub {
			return i
		}
	}
	return -1
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/repl/ -run TestResumePicker -v`
Expected: FAIL — `undefined: NewResumePicker`.

- [ ] **Step 3: Write minimal implementation**

Create `internal/repl/resume_picker.go`:
```go
package repl

import (
	"fmt"

	"ccgo/internal/tui"
)

// ResumeEntry is one prior session shown in the resume picker.
type ResumeEntry struct {
	ID           string
	Summary      string
	ProjectPath  string
	ModifiedUnix int64
}

// ResumePicker is an overlay listing resumable sessions, newest first.
type ResumePicker struct {
	entries []ResumeEntry
	cursor  int
}

func NewResumePicker(entries []ResumeEntry) *ResumePicker {
	return &ResumePicker{entries: entries}
}

func (p *ResumePicker) Selected() (ResumeEntry, bool) {
	if p.cursor < 0 || p.cursor >= len(p.entries) {
		return ResumeEntry{}, false
	}
	return p.entries[p.cursor], true
}

func (p *ResumePicker) ApplyKey(key tui.Key) (OverlayResult, bool) {
	switch key.Type {
	case tui.KeyEsc:
		return OverlayResult{Dismissed: true}, true
	case tui.KeyUp:
		if p.cursor > 0 {
			p.cursor--
		}
		return OverlayResult{}, true
	case tui.KeyDown:
		if p.cursor < len(p.entries)-1 {
			p.cursor++
		}
		return OverlayResult{}, true
	case tui.KeyEnter:
		if entry, ok := p.Selected(); ok {
			return OverlayResult{Submit: "resume:" + entry.ID}, true
		}
		return OverlayResult{Dismissed: true}, true
	default:
		return OverlayResult{}, false
	}
}

func (p *ResumePicker) Render(width, height int) []string {
	lines := []string{"Resume a conversation:"}
	max := height - 2
	if max < 1 {
		max = 1
	}
	for i, e := range p.entries {
		if i >= max {
			break
		}
		marker := "  "
		if i == p.cursor {
			marker = "> "
		}
		lines = append(lines, fmt.Sprintf("%s%s · %s", marker, e.Summary, e.ProjectPath))
	}
	return lines
}
```

- [ ] **Step 4: Run tests to verify they pass**

Run: `go test ./internal/repl/ -run TestResumePicker -v`
Expected: PASS.

- [ ] **Step 5: Commit**

```bash
git add internal/repl/resume_picker.go internal/repl/resume_picker_test.go
git commit -m "feat(repl): interactive resume/continue session picker overlay"
```

---

## Task 9: Mode-switch UI + indicators (plan / acceptEdits / bypass) + vim indicator

**Files:**
- Create: `internal/repl/mode_switch.go`
- Modify: `internal/repl/loop.go` (Shift+Tab cycles mode; status shows indicator)
- Test: `internal/repl/mode_switch_test.go`

**CC behavior anchor:** `src/components/PromptInput/PromptInputFooterLeftSide.tsx:70-71,191` — Shift+Tab cycles permission mode; "-- INSERT --" shown in vim insert. `ExitPlanModePermissionRequest.tsx:268` cycles plan→acceptEdits→bypass.

**Interfaces:**
- Produces:
  - `func cycleMode(cur contracts.PermissionMode) contracts.PermissionMode` — default → acceptEdits → plan → bypassPermissions → default.
  - `func modeIndicator(mode contracts.PermissionMode, vimEnabled bool, vimMode tui.VimMode) string` — e.g. `"plan mode"`, `"accept edits"`, `"bypass permissions"`, plus `" · -- INSERT --"` when vim insert.

Confirm vim mode constant names/values: `go doc ./internal/tui VimMode` and `grep -n "VimMode\b\|VimInsert\|VimNormal" internal/tui/vim.go | head`.

- [ ] **Step 1: Write the failing test**

Create `internal/repl/mode_switch_test.go`:
```go
package repl

import (
	"strings"
	"testing"

	"ccgo/internal/contracts"
)

func TestCycleMode(t *testing.T) {
	seq := []contracts.PermissionMode{
		contracts.PermissionDefault,
		contracts.PermissionAcceptEdits,
		contracts.PermissionPlan,
		contracts.PermissionBypassPermissions,
		contracts.PermissionDefault,
	}
	cur := contracts.PermissionDefault
	for i := 1; i < len(seq); i++ {
		cur = cycleMode(cur)
		if cur != seq[i] {
			t.Fatalf("cycle step %d = %q want %q", i, cur, seq[i])
		}
	}
}

func TestModeIndicatorPlan(t *testing.T) {
	got := modeIndicator(contracts.PermissionPlan, false, 0)
	if !strings.Contains(strings.ToLower(got), "plan") {
		t.Fatalf("indicator = %q should mention plan", got)
	}
}

func TestModeIndicatorDefaultEmptyNoVim(t *testing.T) {
	if got := modeIndicator(contracts.PermissionDefault, false, 0); got != "" {
		t.Fatalf("default mode w/o vim should be empty, got %q", got)
	}
}
```

(The `vimMode` arg is typed `tui.VimMode`; the test passes `0` as the zero value. Confirm the underlying type with the `go doc` above; if `VimMode` is a string type, pass `tui.VimMode("")` instead of `0`.)

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/repl/ -run 'TestCycleMode|TestModeIndicator' -v`
Expected: FAIL — `undefined: cycleMode`.

- [ ] **Step 3: Write minimal implementation**

Create `internal/repl/mode_switch.go`:
```go
package repl

import (
	"strings"

	"ccgo/internal/contracts"
	"ccgo/internal/tui"
)

// cycleMode advances the permission mode in the same order CC's Shift+Tab uses:
// default → acceptEdits → plan → bypassPermissions → default.
func cycleMode(cur contracts.PermissionMode) contracts.PermissionMode {
	switch cur {
	case contracts.PermissionDefault:
		return contracts.PermissionAcceptEdits
	case contracts.PermissionAcceptEdits:
		return contracts.PermissionPlan
	case contracts.PermissionPlan:
		return contracts.PermissionBypassPermissions
	default:
		return contracts.PermissionDefault
	}
}

// modeIndicator is the status-bar fragment for the current mode + vim state.
// Default mode with no vim returns "" (no clutter), matching CC.
func modeIndicator(mode contracts.PermissionMode, vimEnabled bool, vimMode tui.VimMode) string {
	var parts []string
	switch mode {
	case contracts.PermissionAcceptEdits:
		parts = append(parts, "accept edits")
	case contracts.PermissionPlan:
		parts = append(parts, "plan mode")
	case contracts.PermissionBypassPermissions:
		parts = append(parts, "bypass permissions")
	}
	if vimEnabled && isVimInsert(vimMode) {
		parts = append(parts, "-- INSERT --")
	}
	return strings.Join(parts, " · ")
}
```

Add `isVimInsert` matching the confirmed `VimMode` representation. If `VimMode` is a string with an `"insert"` value (confirm: `grep -n "VimInsert\|= VimMode" internal/tui/vim.go`):
```go
func isVimInsert(mode tui.VimMode) bool {
	return tui.VimMode(strings.ToLower(string(mode))) == tui.VimModeInsert
}
```
If `VimMode` is an int enum, compare against the confirmed `tui.VimModeInsert` constant directly. Use whichever the grep confirms.

Wire into the loop. Add a `Loop.mode contracts.PermissionMode` field (seed from the runner's `PermissionMode` via a setter `SetMode`). In `handleKey`, handle Shift+Tab to cycle mode and refresh the base status. The screen emits `KeyShiftTab` as a normal key (not a ScreenEvent), so intercept it before `screen.ApplyKey`:
```go
	if key.Type == tui.KeyShiftTab {
		l.mode = cycleMode(l.mode)
		l.refreshBaseStatus()
		return false
	}
```
Add:
```go
func (l *Loop) refreshBaseStatus() {
	l.baseStatus = modeIndicator(l.mode, l.screen.VimEnabled, l.screen.VimMode)
	if !l.running {
		l.screen.Status = l.baseStatus
	}
}
```
Confirm `KeyShiftTab` exists: `grep -n "KeyShiftTab" internal/tui/types.go` (confirmed types.go:82). Confirm `screen.VimEnabled`/`screen.VimMode` are exported fields (screen.go:56-93 — `VimEnabled bool`, `VimMode VimMode`).

- [ ] **Step 4: Run tests to verify they pass**

Run: `go test ./internal/repl/ -v`
Expected: PASS.

- [ ] **Step 5: Commit**

```bash
git add internal/repl/mode_switch.go internal/repl/loop.go internal/repl/mode_switch_test.go
git commit -m "feat(repl): Shift+Tab mode cycling with plan/acceptEdits/bypass + vim indicator"
```

---

## Task 10: Status / cost / context panels + Doctor report

**Files:**
- Create: `internal/repl/panels.go`
- Test: `internal/repl/panels_test.go`

**CC behavior anchor:** `src/components/StatusLine.tsx:36-120` (model, context %, cost, tokens); `src/screens/Doctor.tsx:1-100` (env/settings/MCP/plugin diagnostics). These are *text builders* the slash commands (`/cost`, `/context`, `/status`, `/doctor`) render — pure functions over already-available state, easily unit-tested.

**Interfaces:**
- Produces:
  - `type SessionStats struct { Model string; InputTokens, OutputTokens int; CostUSD float64; ContextUsed, ContextMax int; APIDuration time.Duration }`
  - `func costPanel(s SessionStats) string`
  - `func contextPanel(s SessionStats) string`
  - `func statusPanel(s SessionStats, mode contracts.PermissionMode) string`
  - `type DoctorCheck struct { Name, Status, Detail string }`
  - `func doctorReport(checks []DoctorCheck) string`

- [ ] **Step 1: Write the failing test**

Create `internal/repl/panels_test.go`:
```go
package repl

import (
	"strings"
	"testing"
	"time"

	"ccgo/internal/contracts"
)

func TestCostPanel(t *testing.T) {
	s := SessionStats{Model: "claude-x", CostUSD: 0.1234, APIDuration: 2 * time.Second}
	out := costPanel(s)
	if !strings.Contains(out, "$0.12") {
		t.Fatalf("cost panel %q should show cost", out)
	}
}

func TestContextPanelPercent(t *testing.T) {
	s := SessionStats{ContextUsed: 50000, ContextMax: 200000}
	out := contextPanel(s)
	if !strings.Contains(out, "25%") {
		t.Fatalf("context panel %q should show 25%%", out)
	}
}

func TestContextPanelZeroMaxSafe(t *testing.T) {
	out := contextPanel(SessionStats{ContextUsed: 10, ContextMax: 0})
	if out == "" {
		t.Fatal("context panel should still render with unknown max")
	}
}

func TestStatusPanelIncludesMode(t *testing.T) {
	out := statusPanel(SessionStats{Model: "m"}, contracts.PermissionPlan)
	if !strings.Contains(strings.ToLower(out), "plan") {
		t.Fatalf("status panel %q should include mode", out)
	}
}

func TestDoctorReport(t *testing.T) {
	out := doctorReport([]DoctorCheck{
		{Name: "Go toolchain", Status: "ok", Detail: "go1.26"},
		{Name: "Settings", Status: "warn", Detail: "invalid key"},
	})
	if !strings.Contains(out, "Go toolchain") || !strings.Contains(out, "Settings") {
		t.Fatalf("doctor report missing checks: %q", out)
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/repl/ -run 'TestCostPanel|TestContextPanel|TestStatusPanel|TestDoctorReport' -v`
Expected: FAIL — `undefined: costPanel`.

- [ ] **Step 3: Write minimal implementation**

Create `internal/repl/panels.go`:
```go
package repl

import (
	"fmt"
	"strings"
	"time"

	"ccgo/internal/contracts"
)

// SessionStats is the snapshot of usage shown in cost/context/status panels.
type SessionStats struct {
	Model        string
	InputTokens  int
	OutputTokens int
	CostUSD      float64
	ContextUsed  int
	ContextMax   int
	APIDuration  time.Duration
}

func costPanel(s SessionStats) string {
	return fmt.Sprintf(
		"Total cost: $%.2f\nAPI duration: %s\nTokens: %d in / %d out",
		s.CostUSD, s.APIDuration.Round(time.Millisecond), s.InputTokens, s.OutputTokens,
	)
}

func contextPanel(s SessionStats) string {
	if s.ContextMax <= 0 {
		return fmt.Sprintf("Context: %d tokens used (limit unknown)", s.ContextUsed)
	}
	pct := s.ContextUsed * 100 / s.ContextMax
	return fmt.Sprintf("Context: %d / %d tokens (%d%%)", s.ContextUsed, s.ContextMax, pct)
}

func statusPanel(s SessionStats, mode contracts.PermissionMode) string {
	var b strings.Builder
	fmt.Fprintf(&b, "Model: %s\n", s.Model)
	fmt.Fprintf(&b, "Mode: %s\n", modeLabel(mode))
	b.WriteString(contextPanel(s))
	b.WriteString("\n")
	b.WriteString(costPanel(s))
	return b.String()
}

func modeLabel(mode contracts.PermissionMode) string {
	switch mode {
	case contracts.PermissionAcceptEdits:
		return "accept edits"
	case contracts.PermissionPlan:
		return "plan"
	case contracts.PermissionBypassPermissions:
		return "bypass permissions"
	default:
		return "default"
	}
}

// DoctorCheck is one diagnostic line in the /doctor report.
type DoctorCheck struct {
	Name   string
	Status string
	Detail string
}

func doctorReport(checks []DoctorCheck) string {
	var b strings.Builder
	b.WriteString("Claude Code Doctor\n")
	for _, c := range checks {
		mark := statusMark(c.Status)
		fmt.Fprintf(&b, "%s %s: %s\n", mark, c.Name, c.Detail)
	}
	return strings.TrimRight(b.String(), "\n")
}

func statusMark(status string) string {
	switch strings.ToLower(status) {
	case "ok", "pass":
		return "✓"
	case "warn", "warning":
		return "!"
	case "fail", "error":
		return "✗"
	default:
		return "·"
	}
}
```

- [ ] **Step 4: Run tests to verify they pass**

Run: `go test ./internal/repl/ -run 'TestCostPanel|TestContextPanel|TestStatusPanel|TestDoctorReport' -v`
Expected: PASS.

- [ ] **Step 5: Commit**

```bash
git add internal/repl/panels.go internal/repl/panels_test.go
git commit -m "feat(repl): cost/context/status panels and doctor report builders"
```

---

## Task 11: Theme picker + /memory selector + HelpV2 overlays

**Files:**
- Create: `internal/repl/help_screen.go`, `internal/repl/theme_picker.go`, `internal/repl/memory_selector.go`
- Test: `internal/repl/help_screen_test.go`, `internal/repl/theme_picker_test.go`, `internal/repl/memory_selector_test.go`

**CC behavior anchors:** HelpV2 `src/components/HelpV2/HelpV2.tsx:20-79` (built-in + custom commands, Esc to dismiss); ThemePicker `src/components/ThemePicker.tsx:30-100` (list + preview, Enter selects); MemoryFileSelector `src/components/memory/MemoryFileSelector.tsx:44-100` (User/Project/nested memory files).

These three are the same Overlay shape as the slash menu (list + cursor + Enter/Esc). To avoid three near-identical implementations, build a tiny shared `listOverlay` and parameterize it.

**Interfaces:**
- Produces:
  - `type listItem struct { Label, Submit string }`
  - `type listOverlay struct { title string; items []listItem; cursor int }`
  - `func newListOverlay(title string, items []listItem) *listOverlay`
  - `func NewHelpScreen(commands []contracts.Command) *listOverlay`
  - `func NewThemePicker(themes []string) *listOverlay`
  - `func NewMemorySelector(files []string) *listOverlay`

- [ ] **Step 1: Write the failing tests**

Create `internal/repl/help_screen_test.go`:
```go
package repl

import (
	"strings"
	"testing"

	"ccgo/internal/contracts"
	"ccgo/internal/tui"
)

func TestHelpScreenListsCommandsAndDismisses(t *testing.T) {
	h := NewHelpScreen([]contracts.Command{{Name: "clear", Description: "Clear"}})
	lines := h.Render(80, 24)
	if !strings.Contains(strings.Join(lines, "\n"), "/clear") {
		t.Fatalf("help should list /clear: %v", lines)
	}
	res, _ := h.ApplyKey(tui.Key{Type: tui.KeyEsc})
	if !res.Dismissed {
		t.Fatal("Esc should dismiss help")
	}
}
```

Create `internal/repl/theme_picker_test.go`:
```go
package repl

import (
	"testing"

	"ccgo/internal/tui"
)

func TestThemePickerEnterSubmits(t *testing.T) {
	p := NewThemePicker([]string{"dark", "light"})
	p.ApplyKey(tui.Key{Type: tui.KeyDown})
	res, _ := p.ApplyKey(tui.Key{Type: tui.KeyEnter})
	if res.Submit != "theme:light" {
		t.Fatalf("submit = %q want theme:light", res.Submit)
	}
}
```

Create `internal/repl/memory_selector_test.go`:
```go
package repl

import (
	"testing"

	"ccgo/internal/tui"
)

func TestMemorySelectorEnterSubmits(t *testing.T) {
	s := NewMemorySelector([]string{"~/.claude/CLAUDE.md", "./CLAUDE.md"})
	res, _ := s.ApplyKey(tui.Key{Type: tui.KeyEnter})
	if res.Submit != "memory:~/.claude/CLAUDE.md" {
		t.Fatalf("submit = %q", res.Submit)
	}
}
```

- [ ] **Step 2: Run tests to verify they fail**

Run: `go test ./internal/repl/ -run 'TestHelpScreen|TestThemePicker|TestMemorySelector' -v`
Expected: FAIL — `undefined: NewHelpScreen` etc.

- [ ] **Step 3: Write minimal implementation**

Create `internal/repl/help_screen.go` (holds the shared `listOverlay` + the three constructors):
```go
package repl

import (
	"ccgo/internal/contracts"
	"ccgo/internal/tui"
)

// listItem is one selectable row in a listOverlay.
type listItem struct {
	Label  string
	Submit string
}

// listOverlay is a reusable cursor-driven list overlay (help/theme/memory).
type listOverlay struct {
	title  string
	items  []listItem
	cursor int
}

func newListOverlay(title string, items []listItem) *listOverlay {
	return &listOverlay{title: title, items: items}
}

func (o *listOverlay) ApplyKey(key tui.Key) (OverlayResult, bool) {
	switch key.Type {
	case tui.KeyEsc:
		return OverlayResult{Dismissed: true}, true
	case tui.KeyUp:
		if o.cursor > 0 {
			o.cursor--
		}
		return OverlayResult{}, true
	case tui.KeyDown:
		if o.cursor < len(o.items)-1 {
			o.cursor++
		}
		return OverlayResult{}, true
	case tui.KeyEnter:
		if o.cursor >= 0 && o.cursor < len(o.items) {
			return OverlayResult{Submit: o.items[o.cursor].Submit}, true
		}
		return OverlayResult{Dismissed: true}, true
	default:
		return OverlayResult{}, false
	}
}

func (o *listOverlay) Render(width, height int) []string {
	lines := []string{o.title}
	max := height - 2
	if max < 1 {
		max = 1
	}
	for i, it := range o.items {
		if i >= max {
			break
		}
		marker := "  "
		if i == o.cursor {
			marker = "> "
		}
		lines = append(lines, marker+it.Label)
	}
	return lines
}

// NewHelpScreen builds the HelpV2 overlay listing the visible commands.
func NewHelpScreen(commands []contracts.Command) *listOverlay {
	items := make([]listItem, 0, len(commands))
	for _, c := range commands {
		if c.Hidden {
			continue
		}
		label := "/" + c.Name
		if c.Description != "" {
			label += " — " + c.Description
		}
		// Help is informational: selecting a row inserts the command.
		items = append(items, listItem{Label: label, Submit: "/" + c.Name})
	}
	return newListOverlay("Help — commands (esc to close)", items)
}
```

Create `internal/repl/theme_picker.go`:
```go
package repl

// NewThemePicker builds an overlay to choose a theme; Enter submits
// "theme:<name>" which the loop persists/applies.
func NewThemePicker(themes []string) *listOverlay {
	items := make([]listItem, 0, len(themes))
	for _, name := range themes {
		items = append(items, listItem{Label: name, Submit: "theme:" + name})
	}
	return newListOverlay("Select theme (esc to cancel)", items)
}
```

Create `internal/repl/memory_selector.go`:
```go
package repl

// NewMemorySelector builds an overlay to pick a memory file to edit; Enter
// submits "memory:<path>".
func NewMemorySelector(files []string) *listOverlay {
	items := make([]listItem, 0, len(files))
	for _, path := range files {
		items = append(items, listItem{Label: path, Submit: "memory:" + path})
	}
	return newListOverlay("Edit memory file (esc to cancel)", items)
}
```

- [ ] **Step 4: Run tests to verify they pass**

Run: `go test ./internal/repl/ -run 'TestHelpScreen|TestThemePicker|TestMemorySelector' -v`
Expected: PASS.

- [ ] **Step 5: Commit**

```bash
git add internal/repl/help_screen.go internal/repl/theme_picker.go internal/repl/memory_selector.go internal/repl/help_screen_test.go internal/repl/theme_picker_test.go internal/repl/memory_selector_test.go
git commit -m "feat(repl): help, theme picker, and memory selector overlays"
```

---

## Task 12: Onboarding + TrustDialog (first-run folder trust)

**Files:**
- Create: `internal/repl/trust_dialog.go`
- Test: `internal/repl/trust_dialog_test.go`

**CC behavior anchor:** `src/components/TrustDialog/TrustDialog.tsx:23-100` — first run lists detected config sources (bash rules, MCP servers, hooks, apiKeyHelpers) and asks to trust the folder; Yes proceeds, No exits/limits.

**Interfaces:**
- Produces:
  - `type TrustInfo struct { FolderPath string; HasBashRules, HasMCPServers, HasHooks, HasAPIKeyHelper bool }`
  - `type TrustDialog struct { info TrustInfo; cursor int }`
  - `func NewTrustDialog(info TrustInfo) *TrustDialog`
  - `ApplyKey`/`Render` (Overlay): two actions "Yes, trust this folder" / "No". Enter on Yes → `Submit:"trust:yes"`; on No → `Submit:"trust:no"`; Esc → `Submit:"trust:no"` (declining is the safe default).

- [ ] **Step 1: Write the failing test**

Create `internal/repl/trust_dialog_test.go`:
```go
package repl

import (
	"strings"
	"testing"

	"ccgo/internal/tui"
)

func TestTrustDialogListsSources(t *testing.T) {
	d := NewTrustDialog(TrustInfo{FolderPath: "/proj", HasBashRules: true, HasMCPServers: true})
	out := strings.Join(d.Render(80, 24), "\n")
	if !strings.Contains(out, "/proj") {
		t.Fatalf("trust dialog should show folder path: %q", out)
	}
	if !strings.Contains(strings.ToLower(out), "bash") || !strings.Contains(strings.ToLower(out), "mcp") {
		t.Fatalf("trust dialog should list detected sources: %q", out)
	}
}

func TestTrustDialogYes(t *testing.T) {
	d := NewTrustDialog(TrustInfo{FolderPath: "/proj"})
	res, _ := d.ApplyKey(tui.Key{Type: tui.KeyEnter})
	if res.Submit != "trust:yes" {
		t.Fatalf("default Enter should trust, got %q", res.Submit)
	}
}

func TestTrustDialogEscDeclines(t *testing.T) {
	d := NewTrustDialog(TrustInfo{FolderPath: "/proj"})
	res, _ := d.ApplyKey(tui.Key{Type: tui.KeyEsc})
	if res.Submit != "trust:no" {
		t.Fatalf("Esc should decline, got %q (dismissed=%v)", res.Submit, res.Dismissed)
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/repl/ -run TestTrustDialog -v`
Expected: FAIL — `undefined: NewTrustDialog`.

- [ ] **Step 3: Write minimal implementation**

Create `internal/repl/trust_dialog.go`:
```go
package repl

import (
	"fmt"
	"strings"

	"ccgo/internal/tui"
)

// TrustInfo describes the configuration sources detected for a folder, shown
// in the first-run trust dialog so the user knows what they're enabling.
type TrustInfo struct {
	FolderPath      string
	HasBashRules    bool
	HasMCPServers   bool
	HasHooks        bool
	HasAPIKeyHelper bool
}

// TrustDialog is the first-run "trust this folder?" overlay.
type TrustDialog struct {
	info   TrustInfo
	cursor int // 0 = Yes, 1 = No
}

func NewTrustDialog(info TrustInfo) *TrustDialog {
	return &TrustDialog{info: info}
}

func (d *TrustDialog) ApplyKey(key tui.Key) (OverlayResult, bool) {
	switch key.Type {
	case tui.KeyEsc:
		return OverlayResult{Submit: "trust:no"}, true
	case tui.KeyUp, tui.KeyDown, tui.KeyTab:
		d.cursor ^= 1
		return OverlayResult{}, true
	case tui.KeyEnter:
		if d.cursor == 0 {
			return OverlayResult{Submit: "trust:yes"}, true
		}
		return OverlayResult{Submit: "trust:no"}, true
	default:
		return OverlayResult{}, false
	}
}

func (d *TrustDialog) Render(width, height int) []string {
	lines := []string{
		"Do you trust the files in this folder?",
		"  " + d.info.FolderPath,
		"",
	}
	for _, src := range d.detectedSources() {
		lines = append(lines, "  • "+src)
	}
	lines = append(lines, "")
	lines = append(lines, d.actionLine())
	return lines
}

func (d *TrustDialog) detectedSources() []string {
	var out []string
	if d.info.HasBashRules {
		out = append(out, "Bash permission rules")
	}
	if d.info.HasMCPServers {
		out = append(out, "MCP servers")
	}
	if d.info.HasHooks {
		out = append(out, "Hooks")
	}
	if d.info.HasAPIKeyHelper {
		out = append(out, "API key helper")
	}
	if len(out) == 0 {
		out = append(out, "No special configuration detected")
	}
	return out
}

func (d *TrustDialog) actionLine() string {
	yes, no := " Yes, trust this folder ", " No "
	if d.cursor == 0 {
		yes = "[Yes, trust this folder]"
	} else {
		no = "[No]"
	}
	return fmt.Sprintf("%s   %s", strings.TrimRight(yes, " "), strings.TrimRight(no, " "))
}
```

- [ ] **Step 4: Run tests to verify they pass**

Run: `go test ./internal/repl/ -run TestTrustDialog -v`
Expected: PASS.

- [ ] **Step 5: Commit**

```bash
git add internal/repl/trust_dialog.go internal/repl/trust_dialog_test.go
git commit -m "feat(repl): first-run folder TrustDialog overlay"
```

---

## Task 13: Final wiring — engine handle, registry, settings writer, overlay dispatch into cmd/claude

**Files:**
- Modify: `internal/repl/run.go` (accept + plumb the permission engine, settings writer, registry, mode; dispatch slash submissions through the command pipeline)
- Modify: `internal/repl/loop.go` (setters: `SetMode`, `SetRegistry`, `SetSettingsWriter`; route `resume:`/`theme:`/`memory:` submissions)
- Modify: `cmd/claude/main.go` (build and pass the engine/writer/registry into `RunInteractive`)
- Test: `internal/repl/run_test.go` (extend the existing end-to-end test to assert a persisted rule)

**Interfaces:**
- Produces:
  - `type InteractiveOptions struct { Engine *permissions.Engine; Settings ruleWriter; Registry []contracts.Command; Mode contracts.PermissionMode; ResumeEntries []ResumeEntry; Themes []string; MemoryFiles []string; Trust *TrustInfo }`
  - `func RunInteractiveWithOptions(ctx context.Context, term Terminal, base conversation.Runner, history []contracts.Message, opts InteractiveOptions) error`
  - keep `RunInteractive(ctx, term, base, history)` as a thin wrapper passing zero options (backward compatible with Phase 1's main.go call + tests).

- [ ] **Step 1: Write the failing test**

Extend `internal/repl/run_test.go` with a persistence assertion. Add:
```go
func TestRunInteractivePersistsAllowAlways(t *testing.T) {
	// Drive: submit "go", a permission ask arrives, user picks "always".
	// Use the asker test harness + a recording ruleWriter.
	ft := NewFakeTerminal("", 80, 24)
	l := NewLoop(ft, nil)

	var persisted []contracts.PermissionUpdate
	l.SetSettingsWriter(recordingWriter{onApply: func(u contracts.PermissionUpdate) error {
		persisted = append(persisted, u)
		return nil
	}})

	gate := make(chan struct{})
	l.onPermissionShown = func() { close(gate) }

	asker := loopAsker{askCh: l.askCh}
	decisionCh := make(chan contracts.PermissionDecision, 1)
	go func() {
		d, err := asker.Ask(context.Background(), tool.PermissionAskRequest{
			ToolUseID: "u1", ToolName: "Read", Path: "/tmp/x",
		})
		if err == nil {
			decisionCh <- d
		}
	}()

	// After the dialog shows, send keys to focus the "always" action then Enter.
	go func() {
		<-gate
		// Down once selects action index 1 ("Allow always…"), Enter confirms.
		ft.In.WriteString("\x1b[B\r")
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
	if len(persisted) != 1 {
		t.Fatalf("expected 1 persisted rule, got %d", len(persisted))
	}
}

type recordingWriter struct{ onApply func(contracts.PermissionUpdate) error }

func (w recordingWriter) Apply(u contracts.PermissionUpdate) error { return w.onApply(u) }
```
Add imports `"ccgo/internal/tool"` and `"ccgo/internal/contracts"` to run_test.go if absent.

Confirm the down-arrow CSI sequence `\x1b[B` is what selects the next dialog action: this depends on the screen routing arrow keys to `applyDialogAction`. Verify: `grep -n "KeyDown\|applyDialogAction\|Focused" internal/tui/screen.go`. If the dialog advances focus with a different key (e.g. Tab), use that confirmed key in the test input instead — do not change production code to fit the test.

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/repl/ -run TestRunInteractivePersists -v`
Expected: FAIL — `undefined: (*Loop).SetSettingsWriter` resolves (added Task 5) but the focus/persist path may not yet route the always action through `persistDecision`; if Task 5 already wired it, this test confirms the integration end-to-end. (If it already passes after Task 5, that is acceptable — it documents the integration; proceed to wire the options struct below, which the remaining steps require.)

- [ ] **Step 3: Write minimal implementation**

In `internal/repl/loop.go` add the remaining setters (some added in earlier tasks; ensure all exist):
```go
func (l *Loop) SetMode(mode contracts.PermissionMode) {
	l.mode = mode
	l.refreshBaseStatus()
}

func (l *Loop) SetRegistry(cmds []contracts.Command) { l.registry = cmds }
```
Route non-prompt overlay submissions (`resume:`/`theme:`/`memory:`/`trust:`) so they don't get sent to the model as a literal turn. In the overlay-submission branch of `handleKey` (Task 7), special-case structured submits:
```go
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
```
Add:
```go
// handleOverlaySubmit consumes structured overlay results (resume:/theme:/
// memory:/trust:). It returns true when the submit was handled internally and
// should NOT be forwarded to the model. onOverlaySubmit is a host/test seam.
func (l *Loop) handleOverlaySubmit(submit string) bool {
	for _, prefix := range []string{"resume:", "theme:", "memory:", "trust:"} {
		if strings.HasPrefix(submit, prefix) {
			if l.onOverlaySubmit != nil {
				l.onOverlaySubmit(submit)
			}
			return true
		}
	}
	return false // "/command" and plain text fall through to the model/command pipeline
}
```
Add the `onOverlaySubmit func(string)` field to `Loop` (host wires resume/theme/memory actions; nil in tests is fine).

In `internal/repl/run.go`, add the options struct and the new entrypoint:
```go
package repl

import (
	"context"

	"ccgo/internal/contracts"
	"ccgo/internal/conversation"
	"ccgo/internal/messages"
	"ccgo/internal/permissions"
)

// InteractiveOptions carries everything the REPL needs beyond a turn runner to
// reach CC parity: the live permission engine (for in-session rule updates), a
// settings writer (for persisted rules), the command registry (slash menu), the
// initial mode, and the data backing the resume/theme/memory overlays.
type InteractiveOptions struct {
	Engine        *permissions.Engine
	Settings      ruleWriter
	Registry      []contracts.Command
	Mode          contracts.PermissionMode
	ResumeEntries []ResumeEntry
	Themes        []string
	MemoryFiles   []string
	Trust         *TrustInfo
	OnOverlay     func(string)
}

func RunInteractive(ctx context.Context, term Terminal, base conversation.Runner, history []contracts.Message) error {
	return RunInteractiveWithOptions(ctx, term, base, history, InteractiveOptions{})
}

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
	return loop.Run(ctx)
}
```
Keep `newTurnLoop` as-is (run.go:13). The `messages` import stays (used by `newTurnLoop`).

In `cmd/claude/main.go`, replace the `repl.RunInteractive(ctx, term, runner, history)` call (main.go:300) with the options form. Build the inputs from already-available state:
```go
	engine, _ := state.PermissionEngine() // confirm accessor name
	registry := state.CommandRegistry().Visible() // confirm accessor name
	writer := settingswriter.New(config.UserSettingsPath(), config.ProjectSettingsPath(state.CWD()))
	opts := repl.InteractiveOptions{
		Engine:   engine,
		Settings: writer,
		Registry: registry,
		Mode:     runner.PermissionMode,
	}
	if err := repl.RunInteractiveWithOptions(ctx, term, runner, history, opts); err != nil {
		fmt.Fprintf(stderr, "ccgo: %v\n", err)
		return 1
	}
	return 0
```
Confirm the exact accessor names on `bootstrap.State` before writing: `grep -n "func (.*State) PermissionEngine\|func (.*State) CommandRegistry\|func (.*State) CWD\|func (.*State) Registry" internal/bootstrap/*.go`. If an engine accessor does not exist, fall back to passing `Settings`+`Registry`+`Mode` only and leave `Engine: nil` (the persist path uses `Settings`; the live-engine update is a P1 nicety — flag it as deferred rather than inventing an accessor). Confirm `config.ProjectSettingsPath` signature: `go doc ./internal/config ProjectSettingsPath`. Add imports `"ccgo/internal/settingswriter"` and `"ccgo/internal/config"` to main.go if absent.

- [ ] **Step 4: Build, vet, run package + full suite**

Run:
```bash
go build ./... && go vet ./... && go test ./internal/repl/ ./internal/settingswriter/ -v && go test ./...
```
Expected: build OK, vet clean, repl + settingswriter PASS, full suite green.

Manual smoke test (requires a real tty; cannot be automated):
```bash
go run ./cmd/claude
# Resize the window — layout re-flows live (no garbled lines).
# Send a prompt — spinner animates with elapsed seconds + "esc to interrupt".
# Press ESC mid-turn — turn aborts, "Interrupted by user." appears.
# Trigger a tool needing permission — dialog shows Allow once / Allow always… / Deny.
#   Pick "Allow always…" — confirm a rule lands in ~/.claude or ./.claude settings.json:
#   (in another shell) cat .claude/settings.json | grep -A3 permissions
# Type "/" — slash menu appears and filters as you type; Enter runs the command.
# Shift+Tab — mode indicator cycles default→accept edits→plan→bypass.
# Ctrl-D twice — clean exit, terminal restored (cursor visible, no raw mode).
```
Non-tty regression (must not hang, must not enter raw mode):
```bash
echo "" | go run ./cmd/claude
```

- [ ] **Step 5: Commit**

```bash
git add internal/repl/run.go internal/repl/loop.go internal/repl/run_test.go cmd/claude/main.go
git commit -m "feat(claude): wire permission persistence, slash menu, mode, and overlays into the REPL"
```

---

## Self-Review

**Spec coverage (Phase 2 deliverables from roadmap §5 / gap-audit §10.1 UI list):**
- resize / SIGWINCH live handling → Task 1. ✓
- spinner / progress indicator → Task 2. ✓
- Ctrl-C / ESC mid-turn interrupt (Phase 1's stubbed `ScreenEventInterrupted`) → Task 3. ✓
- settings writer for persisted rules (`config.WriteSettingsDocument` bridge) → Task 4. ✓
- full permission dialog set + "Allow Session"/"Allow always" persistence (carries `Suggestions`, honors immutable `Engine.ApplyUpdate`) → Task 5. ✓ (Bash/PowerShell/Edit/Write/WebFetch/Skill/NotebookEdit/SedEdit/Filesystem action sets; AskUserQuestion/EnterPlanMode/ExitPlanMode dialogs render here, their *tools* land in Phase 5 per roadmap §3 cross-dep).
- rich rendering: StructuredDiff + tool blocks → Task 6 (reuses `native.BuildColorDiff`); status/cost/context panels + Doctor → Task 10; HelpV2 + theme picker + /memory selector → Task 11; onboarding/TrustDialog → Task 12. ✓
- slash-command menu + autocomplete → Task 7 (overlay framework). ✓
- resume / continue picker → Task 8. ✓
- vim mode wiring + mode-switch UI/indicators (plan/acceptEdits/bypass) → Task 9. ✓
- final wiring into `cmd/claude` → Task 13. ✓

**Placeholder scan:** No TBD / "add error handling" / "similar to Task N". Every step shows real Go. The only conditional branches are explicit *verification-gated* fallbacks (e.g. "if `PermissionRuleValueToString` is unexported, replicate the format"; "if no engine accessor exists, pass Settings only") — each names the exact grep/`go doc` to run and the concrete alternative, never an open TODO.

**Verification flags (every assumed identifier is grep/go-doc-checked at its point of use):** `golang.org/x/sys/unix SIGWINCH` (Task 1); `native.BuildColorDiff`/`ColorDiffOptions`/`ColorDiff` result field + edit-tool input field names (Task 6); `permissions.PermissionRuleValueToString` + `permissions.allow/deny/ask` settings shape (Task 4); `tui` key constants `KeyDown/Up/Enter/Esc/ShiftTab/Tab`, `screen.Prompt.Text`, `screen.VimEnabled/VimMode`, `VimMode`/`VimModeInsert` representation, `tui.Truncate`/`padOrTrim` existence (Tasks 7, 9, 11); dialog focus-advance key (Tasks 5, 13); `bootstrap.State` accessors `PermissionEngine`/`CommandRegistry`/`CWD` + `config.ProjectSettingsPath` signature (Task 13). None assumed silently.

**Immutability:** `permissions.Engine.ApplyUpdate` returns a new Engine (honored — the loop replaces its handle, never mutates in place); `settingswriter.Apply` reads → builds a new merged doc → writes (no in-place mutation of caller data); the per-turn runner copy (`r := base`) from Phase 1 is preserved in Task 3's `StartTurn`.

**Non-TTY safety:** `startResizeListener` is a no-op when `!IsTTY` (pipes never resize); the spinner ticker only runs on the tty `select`; the non-tty `runLineMode` path (Phase 1) is untouched. No test constructs a real tty — all use `FakeTerminal` / pure functions.

**Errors:** `settingswriter.Apply` wraps read/write errors with `%w` and path context; persist failures surface as a system message (Task 5) rather than being swallowed; the resize listener `defer signal.Stop(sig)` and the spinner `defer`-style `stopTick` release resources; interrupt resets `turnCancel` to nil to avoid double-cancel.

**File sizes:** every new file is well under 350 LOC; the shared `listOverlay` (Task 11) avoids three duplicate overlays; `loop.go` grows but each added method is small and single-purpose (split into `resize.go`/`spinner.go`/`interrupt.go`/`overlay.go` rather than inlined).

**Cross-phase dependencies / risks (also see roadmap §3):**
- Tasks 5/13 render AskUserQuestion/EnterPlanMode/ExitPlanMode permission dialogs, but those *tools* are Phase 5 — until then those dialogs only appear if such a tool is invoked; this is the documented P2↔P5 seam, not a gap.
- Task 13's live-engine update is gated on a `bootstrap.State.PermissionEngine` accessor; if absent, persistence still works via the settings writer (rules load on next start). Flagged, not invented.
- Phase 2 competes for `internal/repl`/`internal/tui` files with Phase 5's plan-mode UI ceremony (roadmap §3) — sequence P2 before P5's UI work to avoid collisions.
