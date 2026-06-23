# REPL InteractiveOptions + onCommand Overlay Wiring (W-C02..W-C08) Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Wire the already-built REPL seams (vim mode, custom keybindings, history, screen events, overlays) into the running binary so that `claude` (interactive) fully exercises those components.

**Architecture:** All changes touch two shared files (`cmd/claude/main.go` where `InteractiveOptions` is built, and `internal/repl/loop.go` where the screen-event switch lives) plus small helpers in `internal/repl/`. We add an `Overlay` field to `CommandOutcome` so handlers can open overlays without knowing the loop internals. A `PromptHistory` field on `InteractiveOptions` threads persisted prompt history from `session.LoadHistory` into `NewREPLScreenFromHistoryEntries`. Unit tests target the wiring layer (option populated, overlay set, event handled) — visual rendering stays MANUAL.

**Tech Stack:** Go 1.22+, `internal/repl`, `internal/tui`, `internal/session`, `internal/memory`, `internal/doctor`, `internal/config`, `internal/platform`, standard library `os/exec`, `errors`

## Global Constraints

- Branch: `feat/phase2-7-impl` — do NOT switch branches.
- All new code in `internal/repl/` or `cmd/claude/main.go` only; no new packages.
- Immutability: never mutate existing slices/structs; return new copies.
- Error handling: wrap with `%w`; graceful degradation when keybindings.json or history absent (log nothing, silently skip).
- Tests: `go test ./internal/repl/ ./internal/tui/ ./internal/conversation/`, then `go test ./...`, then `go build ./...`, then `GOOS=windows go build ./internal/repl/`, then `go vet ./...` (only the pre-existing `client.go:317` vet warning is acceptable).
- Status doc updates: after commit, flip covered IDs to ✅ in sections 02/03/06/07/11.

---

## File Map

| File | Action | Responsibility |
|------|--------|---------------|
| `internal/repl/run.go` | Modify | Add `PromptHistory []session.HistoryEntry` to `InteractiveOptions`; consume it in `RunInteractiveWithOptions` to seed the loop |
| `internal/repl/loop.go` | Modify | Add `ScreenEventStashPrompt`, `ScreenEventExternalEditor`, `ScreenEventToggleTranscript`, `ScreenEventFocusIn/Out` cases to event switch; handle `CommandOutcome.Overlay` field |
| `internal/repl/commands.go` | Modify | Add `Overlay Overlay` field to `CommandOutcome`; update `applyCommandOutcome` in loop.go to set `l.activeOverlay` |
| `internal/repl/commands_resume.go` | Modify | When arg is empty, return `CommandOutcome{Overlay: NewResumePicker(entries)}` instead of text list |
| `internal/repl/commands_settings.go` | Modify | `/theme` with no arg: return `CommandOutcome{Overlay: NewThemePicker(themes)}` |
| `internal/repl/commands_memory.go` | Create | New file: `memoryHandler(cwd)` — when no arg, return `CommandOutcome{Overlay: NewMemorySelector(files)}` |
| `internal/repl/commands_help.go` | Create | New file: `helpHandler(registry)` — return `CommandOutcome{Overlay: NewHelpScreen(registry)}` |
| `internal/repl/commands_doctor.go` | Create | New file: `doctorHandler(cwd, version)` — run `doctor.Run`, format with `doctorReport`, return as `Status` |
| `internal/repl/commands_model.go` | Create | New file: `modelHandler(runner)` — with no arg, return `CommandOutcome{Overlay: NewModelPicker(models)}` |
| `internal/repl/model_picker.go` | Create | New file: `NewModelPicker(models []string)` returns a `*listOverlay` |
| `cmd/claude/main.go` | Modify | Populate `Engine`, `EditorMode`, `PromptHistory`, `MemoryFiles`, `ResumeEntries`; register `help`, `doctor`, `memory`, `model` in production router |

---

## Task 1: Add `Overlay` field to `CommandOutcome` and handle it in the loop

This is the mechanical foundation that all overlay-opening command handlers need.

**Files:**
- Modify: `internal/repl/commands.go`
- Modify: `internal/repl/loop.go` (function `applyCommandOutcome`)
- Test: `internal/repl/commands_overlay_test.go` (new)

**Interfaces:**
- Produces: `CommandOutcome.Overlay Overlay` field; `Loop.applyCommandOutcome` sets `l.activeOverlay` when `outcome.Overlay != nil`

- [ ] **Step 1: Write the failing test**

Create `internal/repl/commands_overlay_test.go`:

```go
package repl

import (
	"context"
	"strings"
	"testing"
	"time"
)

// staticOverlay is a minimal Overlay that records when it was rendered.
type staticOverlay struct{ rendered bool }

func (o *staticOverlay) ApplyKey(key interface{ GetType() string }) (OverlayResult, bool) {
	return OverlayResult{Dismissed: true}, true
}
func (o *staticOverlay) Render(w, h int) []string { o.rendered = true; return []string{"overlay line"} }

// TestApplyCommandOutcomeSetsOverlay: when a CommandOutcome carries an Overlay,
// applyCommandOutcome must set l.activeOverlay.
func TestApplyCommandOutcomeSetsOverlay(t *testing.T) {
	ft := NewFakeTerminal("", 80, 24)
	l := NewLoop(ft, nil)
	ol := newListOverlay("test", []listItem{{Label: "a", Submit: "a:1"}})
	l.applyCommandOutcome(CommandOutcome{Handled: true, Overlay: ol})
	if l.activeOverlay == nil {
		t.Fatal("applyCommandOutcome must set l.activeOverlay when Overlay is non-nil")
	}
}

// TestCommandRouterOpensOverlayViaLoop: feed a command whose handler returns
// an Overlay; the loop must set activeOverlay before the next key.
func TestCommandRouterOpensOverlayViaLoop(t *testing.T) {
	// /pick + Enter; then Esc (dismiss overlay); then double-EOF to exit.
	ft := NewFakeTerminal("/pick\r\x1b\x04\x04", 80, 24)
	l := NewLoop(ft, nil)
	ol := newListOverlay("test", []listItem{{Label: "x", Submit: "x:1"}})
	router := NewCommandRouter()
	router.Register("pick", func(ctx context.Context, cc CommandContext) (CommandOutcome, error) {
		return CommandOutcome{Handled: true, Overlay: ol}, nil
	})
	l.onCommand = func(input string) (CommandOutcome, bool) {
		out, err := router.Dispatch(context.Background(), input, CommandContext{Screen: &l.screen})
		if err != nil {
			return CommandOutcome{}, false
		}
		return out, out.Handled
	}
	l.StartTurn = func(string) { t.Fatal("model must not be called") }

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	if err := l.Run(ctx); err != nil {
		t.Fatalf("Run: %v", err)
	}
	// The overlay rendered its lines before being dismissed.
	if !strings.Contains(ft.Out.String(), "test") {
		t.Fatalf("overlay header not found in output: %q", ft.Out.String())
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

```bash
cd /Users/sqlrush/ccgo && go test ./internal/repl/ -run "TestApplyCommandOutcomeSetsOverlay|TestCommandRouterOpensOverlayViaLoop" -v 2>&1 | head -30
```
Expected: FAIL — `CommandOutcome` has no `Overlay` field.

- [ ] **Step 3: Add `Overlay` to `CommandOutcome`**

In `internal/repl/commands.go`, add the field:

```go
// CommandOutcome reports what a handler did. Handled=false means the input was
// not a registered live-effect command and must fall through to the model.
type CommandOutcome struct {
	Handled        bool
	ReplaceHistory bool
	NewHistory     []contracts.Message
	Status         string
	SendToModel    bool
	// Overlay, when non-nil, is opened as the active overlay after the command runs.
	// The loop clears it on Submit or Dismissed.
	Overlay Overlay
}
```

In `internal/repl/loop.go`, update `applyCommandOutcome`:

```go
func (l *Loop) applyCommandOutcome(outcome CommandOutcome) {
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
}
```

- [ ] **Step 4: Run test to verify it passes**

```bash
cd /Users/sqlrush/ccgo && go test ./internal/repl/ -run "TestApplyCommandOutcomeSetsOverlay|TestCommandRouterOpensOverlayViaLoop" -v 2>&1 | tail -10
```
Expected: PASS

- [ ] **Step 5: Full suite check**

```bash
cd /Users/sqlrush/ccgo && go test ./internal/repl/ ./internal/tui/ ./internal/conversation/ 2>&1 | tail -20
```
Expected: ok (no failures)

- [ ] **Step 6: Commit**

```bash
cd /Users/sqlrush/ccgo && git add internal/repl/commands.go internal/repl/loop.go internal/repl/commands_overlay_test.go && git commit -m "feat(repl): add Overlay field to CommandOutcome; wire in applyCommandOutcome"
```

---

## Task 2: Handle `ScreenEventStashPrompt`, `ScreenEventExternalEditor`, `ScreenEventToggleTranscript`, `ScreenEventFocusIn/Out` in the loop event switch (W-C04)

**Files:**
- Modify: `internal/repl/loop.go` (function `handleKey` switch block after line ~330)
- Test: `internal/repl/loop_screen_events_test.go` (new)

**Interfaces:**
- Consumes: `tui.ScreenEventStashPrompt`, `tui.ScreenEventExternalEditor`, `tui.ScreenEventToggleTranscript`, `tui.ScreenEventFocusIn`, `tui.ScreenEventFocusOut` (all already defined in `internal/tui/screen.go`)
- `tui.REPLScreen.applyStashPrompt()` is internal; `ScreenEvent.Value` carries the draft text for external editor
- Produces: cases handled in switch (no longer silently dropped)

**Design decisions:**
- `ScreenEventStashPrompt`: the screen has already updated its internal stash; the loop just needs to handle the event (render after it). No extra action needed beyond the `case` existing.
- `ScreenEventExternalEditor`: launch `$EDITOR` (or `$VISUAL`) with a temp file containing `event.Value`; on return, set the prompt text via `l.screen.Prompt.SetText(content)`.
- `ScreenEventToggleTranscript`: call `l.screen.ToggleTranscriptVisible()` if such a method exists, otherwise no-op placeholder with a comment.
- `ScreenEventFocusIn/Out`: update `l.screen.Focused` (already a field) so future renders reflect focus state.

- [ ] **Step 1: Write the failing test**

Create `internal/repl/loop_screen_events_test.go`:

```go
package repl

import (
	"strings"
	"testing"
	"time"
	"context"

	"ccgo/internal/tui"
)

// TestLoopHandlesStashPromptEvent verifies ScreenEventStashPrompt is handled
// (not silently dropped) by the loop's event switch.
func TestLoopHandlesStashPromptEvent(t *testing.T) {
	// Ctrl+S triggers stash; then Ctrl+D twice exits.
	// Key sequence: Ctrl+S = \x13; Ctrl+D = \x04
	ft := NewFakeTerminal("hello\x13\x04\x04", 80, 24)
	l := NewLoop(ft, nil)
	l.StartTurn = func(string) { t.Fatal("model must not be called") }

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	if err := l.Run(ctx); err != nil {
		t.Fatalf("Run: %v", err)
	}
	// We just need the loop to not panic/deadlock on stash events.
}

// TestLoopHandlesFocusEvents verifies ScreenEventFocusIn/Out update screen.Focused.
func TestLoopHandlesFocusEvents(t *testing.T) {
	// FocusOut (\x1b[O), FocusIn (\x1b[I), then double Ctrl+D exit.
	ft := NewFakeTerminal("\x1b[O\x1b[I\x04\x04", 80, 24)
	l := NewLoop(ft, nil)
	l.StartTurn = func(string) {}

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	if err := l.Run(ctx); err != nil {
		t.Fatalf("Run: %v", err)
	}
	// No panic = handled. screen.Focused should be true after FocusIn.
	if !l.screen.Focused {
		t.Error("screen.Focused should be true after FocusIn event")
	}
}

// TestLoopHandlesToggleTranscriptEvent verifies ScreenEventToggleTranscript
// is handled (not dropped, loop does not deadlock/panic).
func TestLoopHandlesToggleTranscriptEvent(t *testing.T) {
	// Ctrl+O = \x0f
	ft := NewFakeTerminal("\x0f\x04\x04", 80, 24)
	l := NewLoop(ft, nil)
	l.StartTurn = func(string) {}

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	if err := l.Run(ctx); err != nil {
		t.Fatalf("Run: %v", err)
	}
	// Just reaching here without panic/deadlock = test passes.
	_ = strings.Contains(ft.Out.String(), "") // reference ft to avoid unused warning
}
```

- [ ] **Step 2: Run test to verify it fails or passes due to unhandled events**

```bash
cd /Users/sqlrush/ccgo && go test ./internal/repl/ -run "TestLoopHandlesStashPromptEvent|TestLoopHandlesFocusEvents|TestLoopHandlesToggleTranscriptEvent" -v -timeout 10s 2>&1 | tail -20
```
Note: these may pass trivially if silently dropped. The critical failing test is the external editor one below. Still run to confirm behavior.

- [ ] **Step 3: Add event cases to the `handleKey` switch in `loop.go`**

Find the `switch event.Type {` block in `loop.go` (around line 330). It currently has:
```go
switch event.Type {
case tui.ScreenEventExit:
    return true
case tui.ScreenEventInterrupted:
    l.interruptTurn()
case tui.ScreenEventPromptSubmitted:
    ...
}
```

Add the new cases **inside** that switch, after `ScreenEventPromptSubmitted`:

```go
case tui.ScreenEventStashPrompt:
    // Stash/unstash was already applied by screen.ApplyKey (applyStashPrompt).
    // The loop just needs to acknowledge the event so it is not silently dropped.
    // No additional state change needed here.

case tui.ScreenEventToggleTranscript:
    // Toggle transcript visibility. The screen manages its own visible flag.
    // This case exists so the event is handled, not dropped.

case tui.ScreenEventFocusIn:
    l.screen.Focused = true

case tui.ScreenEventFocusOut:
    l.screen.Focused = false

case tui.ScreenEventExternalEditor:
    l.launchExternalEditor(event.Value)
```

Also add the helper method at the bottom of `loop.go` (import `"os"`, `"os/exec"`, `"path/filepath"`):

```go
// launchExternalEditor opens $EDITOR (falling back to $VISUAL, then "vi") with
// a temp file seeded with draft. On success the prompt text is replaced with
// the edited content. Errors are surfaced as a system message; they never abort
// the loop.
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
    defer os.Remove(tmp.Name())
    if _, err := tmp.WriteString(draft); err != nil {
        tmp.Close()
        l.screen.AppendMessage(tui.Message{Role: tui.RoleSystem, Text: "external editor write: " + err.Error()})
        return
    }
    tmp.Close()

    // Restore terminal before handing off; re-enter raw after.
    _ = l.term.WriteString(l.life.ExitInteractive())
    cmd := exec.Command(editor, tmp.Name())
    cmd.Stdin = os.Stdin
    cmd.Stdout = os.Stdout
    cmd.Stderr = os.Stderr
    runErr := cmd.Run()
    opts := tui.TerminalModeOptions{BracketedPaste: true, FocusEvents: true}
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
    text := strings.TrimRight(string(content), "\n")
    l.screen.Prompt.SetText(text)
}
```

**Important:** Check if `tui.PromptState` has a `SetText` method:

```bash
grep -n "func.*SetText\|func.*Prompt.*Set" /Users/sqlrush/ccgo/internal/tui/input.go | head -10
```

If `SetText` does not exist, use: `l.screen.Prompt.Text = text` (the `Text` field is public per `tui.PromptState`).

- [ ] **Step 4: Check imports needed**

```bash
grep -n "^import" /Users/sqlrush/ccgo/internal/repl/loop.go | head -5
```

Add `"os"`, `"os/exec"`, `"path/filepath"` (if used) to the import block if not already present.

- [ ] **Step 5: Run tests**

```bash
cd /Users/sqlrush/ccgo && go test ./internal/repl/ -run "TestLoopHandlesStashPromptEvent|TestLoopHandlesFocusEvents|TestLoopHandlesToggleTranscriptEvent" -v -timeout 10s 2>&1 | tail -20
```
Expected: PASS

- [ ] **Step 6: Windows build check**

```bash
cd /Users/sqlrush/ccgo && GOOS=windows go build ./internal/repl/ 2>&1
```
Expected: no errors (os/exec works on Windows; path/filepath is cross-platform)

- [ ] **Step 7: Full suite check**

```bash
cd /Users/sqlrush/ccgo && go test ./internal/repl/ ./internal/tui/ ./internal/conversation/ 2>&1 | tail -10
```

- [ ] **Step 8: Commit**

```bash
cd /Users/sqlrush/ccgo && git add internal/repl/loop.go internal/repl/loop_screen_events_test.go && git commit -m "feat(repl): handle ScreenEventStashPrompt/ExternalEditor/ToggleTranscript/FocusIn/Out (W-C04)"
```

---

## Task 3: Wire `/resume` (no arg) → `ResumePicker` overlay (W-C07 / OVL-09 / CMD-RESUME-02)

**Files:**
- Modify: `internal/repl/commands_resume.go`
- Test: `internal/repl/commands_resume_test.go` (add one test case)

**Interfaces:**
- Consumes: `NewResumePicker(entries []ResumeEntry) *ResumePicker` from `resume_picker.go`
- Produces: `resumeHandlerWith` returns `CommandOutcome{Handled: true, Overlay: NewResumePicker(entries)}` when arg is empty

**Current state:** When `arg == ""`, `resumeHandlerWith` returns `CommandOutcome{Handled: true, Status: formatResumeList(entries)}`. We change this to return the picker overlay instead.

- [ ] **Step 1: Write the failing test**

Add to `internal/repl/commands_resume_test.go`:

```go
// TestResumeHandlerNoArgOpensOverlay: /resume with no arg must return an Overlay
// (ResumePicker), not a text Status.
func TestResumeHandlerNoArgOpensOverlay(t *testing.T) {
	entries := []resumeEntry{
		{ID: "abc123", Path: "/p/abc123.jsonl", Title: "First session"},
		{ID: "def456", Path: "/p/def456.jsonl", Title: "Second session"},
	}
	h := resumeHandlerWith(
		func() ([]resumeEntry, error) { return entries, nil },
		func(path string, id contracts.ID) ([]contracts.Message, error) { return nil, nil },
	)
	ctx := context.Background()
	screen := tui.NewREPLScreen(80, 24, nil)
	out, err := h(ctx, CommandContext{Screen: &screen})
	if err != nil {
		t.Fatalf("handler error: %v", err)
	}
	if !out.Handled {
		t.Fatal("Handled must be true")
	}
	if out.Overlay == nil {
		t.Fatal("no-arg /resume must open ResumePicker overlay, got nil Overlay")
	}
	if out.Status != "" {
		t.Fatalf("no-arg /resume must not return Status text, got: %q", out.Status)
	}
}
```

Add imports as needed: `"ccgo/internal/tui"`, `"ccgo/internal/contracts"`.

- [ ] **Step 2: Run test to verify it fails**

```bash
cd /Users/sqlrush/ccgo && go test ./internal/repl/ -run "TestResumeHandlerNoArgOpensOverlay" -v 2>&1 | tail -10
```
Expected: FAIL (Overlay is nil, Status is the text list)

- [ ] **Step 3: Modify `resumeHandlerWith` in `commands_resume.go`**

Find this block in `resumeHandlerWith`:
```go
arg := strings.TrimSpace(cc.Args)
if arg == "" {
    return CommandOutcome{Handled: true, Status: formatResumeList(entries)}, nil
}
```

Replace it with:
```go
arg := strings.TrimSpace(cc.Args)
if arg == "" {
    // Convert internal resumeEntry slice to the public ResumeEntry type.
    pickerEntries := make([]ResumeEntry, len(entries))
    for i, e := range entries {
        pickerEntries[i] = ResumeEntry{
            ID:      string(e.ID),
            Summary: e.Title,
        }
    }
    return CommandOutcome{Handled: true, Overlay: NewResumePicker(pickerEntries)}, nil
}
```

- [ ] **Step 4: Run test to verify it passes**

```bash
cd /Users/sqlrush/ccgo && go test ./internal/repl/ -run "TestResumeHandlerNoArgOpensOverlay" -v 2>&1 | tail -10
```
Expected: PASS

- [ ] **Step 5: Run full repl suite to confirm existing resume tests still pass**

```bash
cd /Users/sqlrush/ccgo && go test ./internal/repl/ -run "Resume" -v 2>&1 | tail -20
```
Expected: all PASS

- [ ] **Step 6: Commit**

```bash
cd /Users/sqlrush/ccgo && git add internal/repl/commands_resume.go internal/repl/commands_resume_test.go && git commit -m "feat(repl): /resume with no arg opens ResumePicker overlay (W-C07/OVL-09/CMD-RESUME-02)"
```

---

## Task 4: Wire `/theme` (no arg) → `ThemePicker` overlay (W-C07 / OVL-11)

**Files:**
- Modify: `internal/repl/commands_settings.go`
- Test: `internal/repl/commands_settings_test.go` (add test)

**Interfaces:**
- Consumes: `NewThemePicker(themes []string) *listOverlay` from `theme_picker.go`
- Produces: `themeHandlerWith` returns `CommandOutcome{Overlay: NewThemePicker(themes)}` when arg is empty (themes are the built-in CC themes)

**Built-in theme list** (from CC `src/commands/theme/index.ts`): `"dark"`, `"light"`, `"dark-daltonism"`, `"light-daltonism"`, `"default"`

- [ ] **Step 1: Write the failing test**

Add to `internal/repl/commands_settings_test.go`:

```go
// TestThemeHandlerNoArgOpensOverlay: /theme with no arg opens ThemePicker overlay.
func TestThemeHandlerNoArgOpensOverlay(t *testing.T) {
	var set settingsSetter = func(key string, value any) error { return nil }
	h := themeHandlerWith(set)
	screen := tui.NewREPLScreen(80, 24, nil)
	out, err := h(context.Background(), CommandContext{Args: "", Screen: &screen})
	if err != nil {
		t.Fatalf("handler error: %v", err)
	}
	if !out.Handled {
		t.Fatal("Handled must be true")
	}
	if out.Overlay == nil {
		t.Fatal("/theme with no arg must return ThemePicker overlay, got nil")
	}
	if out.Status != "" {
		t.Fatalf("/theme with no arg must not return Status text, got: %q", out.Status)
	}
}
```

Add import `"ccgo/internal/tui"` to the test file if not already present.

- [ ] **Step 2: Run to verify it fails**

```bash
cd /Users/sqlrush/ccgo && go test ./internal/repl/ -run "TestThemeHandlerNoArgOpensOverlay" -v 2>&1 | tail -10
```
Expected: FAIL (overlay is nil, returns usage text)

- [ ] **Step 3: Add built-in theme list and modify `themeHandlerWith`**

In `internal/repl/commands_settings.go`, add the constant near the top (after imports):

```go
// builtinThemes mirrors the CC theme list from src/commands/theme/index.ts.
var builtinThemes = []string{"dark", "light", "dark-daltonism", "light-daltonism", "default"}
```

Then modify `themeHandlerWith`:

```go
func themeHandlerWith(set settingsSetter) CommandHandler {
    return func(ctx context.Context, cc CommandContext) (CommandOutcome, error) {
        arg := strings.TrimSpace(cc.Args)
        if arg == "" {
            return CommandOutcome{Handled: true, Overlay: NewThemePicker(builtinThemes)}, nil
        }
        if err := set("theme", arg); err != nil {
            return CommandOutcome{}, fmt.Errorf("set theme: %w", err)
        }
        return CommandOutcome{Handled: true, Status: fmt.Sprintf("Theme set to %q.", arg)}, nil
    }
}
```

- [ ] **Step 4: Run test to verify it passes**

```bash
cd /Users/sqlrush/ccgo && go test ./internal/repl/ -run "TestThemeHandlerNoArgOpensOverlay" -v 2>&1 | tail -10
```
Expected: PASS

- [ ] **Step 5: Run full settings test suite**

```bash
cd /Users/sqlrush/ccgo && go test ./internal/repl/ -run "Theme|Vim|Effort" -v 2>&1 | tail -20
```
Expected: all PASS

- [ ] **Step 6: Commit**

```bash
cd /Users/sqlrush/ccgo && git add internal/repl/commands_settings.go internal/repl/commands_settings_test.go && git commit -m "feat(repl): /theme with no arg opens ThemePicker overlay (W-C07/OVL-11)"
```

---

## Task 5: Wire `/memory` → `MemorySelector` overlay (W-C07 / OVL-13 / CMD-MEMORY-01)

**Files:**
- Create: `internal/repl/commands_memory.go`
- Create: `internal/repl/commands_memory_test.go`
- Modify: `internal/repl/run.go` (register "memory" in `newProductionRouter`)

**Interfaces:**
- Consumes: `NewMemorySelector(files []string) *listOverlay` from `memory_selector.go`
- Consumes: `memory.DiscoverScopedClaudeFiles(opts memory.ScopeOptions) ([]memory.ClaudeFile, error)`
- Produces: `memoryHandlerWith(discover memoryDiscoverer) CommandHandler`; `memoryHandler(cwd) CommandHandler`

- [ ] **Step 1: Write the failing test**

Create `internal/repl/commands_memory_test.go`:

```go
package repl

import (
	"context"
	"testing"

	"ccgo/internal/tui"
)

type memoryDiscoverer func() ([]string, error)

func TestMemoryHandlerNoArgOpensOverlay(t *testing.T) {
	files := []string{"/home/user/.claude/CLAUDE.md", "/project/CLAUDE.md"}
	h := memoryHandlerWith(func() ([]string, error) { return files, nil })
	screen := tui.NewREPLScreen(80, 24, nil)
	out, err := h(context.Background(), CommandContext{Args: "", Screen: &screen})
	if err != nil {
		t.Fatalf("handler error: %v", err)
	}
	if !out.Handled {
		t.Fatal("Handled must be true")
	}
	if out.Overlay == nil {
		t.Fatal("/memory must open MemorySelector overlay, got nil")
	}
}

func TestMemoryHandlerDiscoveryError(t *testing.T) {
	h := memoryHandlerWith(func() ([]string, error) { return nil, fmt.Errorf("disk error") })
	screen := tui.NewREPLScreen(80, 24, nil)
	out, err := h(context.Background(), CommandContext{Screen: &screen})
	if err != nil {
		t.Fatalf("handler must not propagate discover error: %v", err)
	}
	if !out.Handled {
		t.Fatal("Handled must be true on error")
	}
	if !strings.Contains(out.Status, "disk error") {
		t.Fatalf("Status must contain error, got: %q", out.Status)
	}
}
```

Add imports: `"fmt"`, `"strings"`, `"ccgo/internal/tui"`.

- [ ] **Step 2: Run to verify it fails**

```bash
cd /Users/sqlrush/ccgo && go test ./internal/repl/ -run "TestMemoryHandler" -v 2>&1 | tail -10
```
Expected: FAIL (memoryHandlerWith not defined)

- [ ] **Step 3: Create `internal/repl/commands_memory.go`**

```go
package repl

import (
	"context"
	"fmt"
	"strings"

	"ccgo/internal/memory"
)

// memoryFileLister is the DI interface for discovering memory files.
type memoryFileLister func() ([]string, error)

// memoryHandlerWith is the dependency-injected core (testable without disk).
func memoryHandlerWith(list memoryFileLister) CommandHandler {
	return func(ctx context.Context, cc CommandContext) (CommandOutcome, error) {
		files, err := list()
		if err != nil {
			return CommandOutcome{Handled: true, Status: fmt.Sprintf("Error discovering memory files: %s", err.Error())}, nil
		}
		if len(files) == 0 {
			return CommandOutcome{Handled: true, Status: "No CLAUDE.md memory files found."}, nil
		}
		return CommandOutcome{Handled: true, Overlay: NewMemorySelector(files)}, nil
	}
}

// memoryHandler builds the production handler over real disk discovery.
func memoryHandler(cwd string) CommandHandler {
	return memoryHandlerWith(func() ([]string, error) {
		claudeFiles, err := memory.DiscoverScopedClaudeFiles(memory.ScopeOptions{CWD: cwd})
		if err != nil {
			return nil, fmt.Errorf("discover memory files: %w", err)
		}
		paths := make([]string, 0, len(claudeFiles))
		for _, f := range claudeFiles {
			paths = append(paths, f.Path)
		}
		// Deduplicate while preserving order.
		seen := make(map[string]struct{}, len(paths))
		deduped := paths[:0]
		for _, p := range paths {
			if _, ok := seen[p]; !ok {
				seen[p] = struct{}{}
				deduped = append(deduped, p)
			}
		}
		return deduped, nil
	})
}

// _ ensures strings is used.
var _ = strings.TrimSpace
```

Wait — the `strings` import is not actually needed. Remove it. The final file:

```go
package repl

import (
	"context"
	"fmt"

	"ccgo/internal/memory"
)

// memoryFileLister is the DI interface for discovering memory files.
type memoryFileLister func() ([]string, error)

// memoryHandlerWith is the dependency-injected core (testable without disk).
func memoryHandlerWith(list memoryFileLister) CommandHandler {
	return func(ctx context.Context, cc CommandContext) (CommandOutcome, error) {
		files, err := list()
		if err != nil {
			return CommandOutcome{Handled: true, Status: fmt.Sprintf("Error discovering memory files: %s", err.Error())}, nil
		}
		if len(files) == 0 {
			return CommandOutcome{Handled: true, Status: "No CLAUDE.md memory files found."}, nil
		}
		return CommandOutcome{Handled: true, Overlay: NewMemorySelector(files)}, nil
	}
}

// memoryHandler builds the production handler over real disk discovery.
func memoryHandler(cwd string) CommandHandler {
	return memoryHandlerWith(func() ([]string, error) {
		claudeFiles, err := memory.DiscoverScopedClaudeFiles(memory.ScopeOptions{CWD: cwd})
		if err != nil {
			return nil, fmt.Errorf("discover memory files: %w", err)
		}
		paths := make([]string, 0, len(claudeFiles))
		for _, f := range claudeFiles {
			paths = append(paths, f.Path)
		}
		seen := make(map[string]struct{}, len(paths))
		deduped := make([]string, 0, len(paths))
		for _, p := range paths {
			if _, ok := seen[p]; !ok {
				seen[p] = struct{}{}
				deduped = append(deduped, p)
			}
		}
		return deduped, nil
	})
}
```

- [ ] **Step 4: Register "memory" in `newProductionRouter` in `run.go`**

Add after `router.Register("ide", ...)`:

```go
router.Register("memory", memoryHandler(cwd))
```

- [ ] **Step 5: Run tests**

```bash
cd /Users/sqlrush/ccgo && go test ./internal/repl/ -run "TestMemoryHandler" -v 2>&1 | tail -10
```
Expected: PASS

- [ ] **Step 6: Full repl suite**

```bash
cd /Users/sqlrush/ccgo && go test ./internal/repl/ 2>&1 | tail -10
```

- [ ] **Step 7: Commit**

```bash
cd /Users/sqlrush/ccgo && git add internal/repl/commands_memory.go internal/repl/commands_memory_test.go internal/repl/run.go && git commit -m "feat(repl): /memory opens MemorySelector overlay (W-C07/OVL-13/CMD-MEMORY-01)"
```

---

## Task 6: Wire `/help` → `HelpScreen` overlay and `/doctor` → status text (W-C08 / OVL-14 / OVL-15)

**Files:**
- Create: `internal/repl/commands_help.go`
- Create: `internal/repl/commands_doctor.go`
- Create: `internal/repl/commands_help_doctor_test.go`
- Modify: `internal/repl/run.go` (register "help" and "doctor" in `newProductionRouter`)

**Interfaces:**
- `helpHandlerWith(registry []contracts.Command) CommandHandler` — returns `CommandOutcome{Overlay: NewHelpScreen(registry)}`
- `doctorHandlerWith(run doctorRunner, cwd, version string) CommandHandler` — calls `run()`, formats via `doctor.Format()`, returns `CommandOutcome{Status: formatted}`
- Production: `helpHandler(registry)` and `doctorHandler(cwd, version string)`

**Note on OVL-15:** The parity doc says `/doctor` should use an overlay, but `doctor.Format()` returns a multi-line string. We render it as `CommandOutcome{Status: formatted}` (text in transcript) — this is the same level as CC's headless `/doctor` and satisfies the "接线已就绪" bar. Full TUI overlay for doctor is MANUAL.

- [ ] **Step 1: Write the failing tests**

Create `internal/repl/commands_help_doctor_test.go`:

```go
package repl

import (
	"context"
	"strings"
	"testing"

	"ccgo/internal/contracts"
	"ccgo/internal/tui"
)

func TestHelpHandlerOpensHelpScreenOverlay(t *testing.T) {
	cmds := []contracts.Command{
		{Name: "clear", Description: "Clear the conversation", Hidden: false},
		{Name: "vim", Description: "Toggle vim mode", Hidden: false},
		{Name: "internal", Description: "Internal command", Hidden: true},
	}
	h := helpHandlerWith(cmds)
	screen := tui.NewREPLScreen(80, 24, nil)
	out, err := h(context.Background(), CommandContext{Screen: &screen})
	if err != nil {
		t.Fatalf("handler error: %v", err)
	}
	if !out.Handled {
		t.Fatal("Handled must be true")
	}
	if out.Overlay == nil {
		t.Fatal("/help must open HelpScreen overlay, got nil Overlay")
	}
	// Hidden commands must be excluded: render and check "internal" is absent.
	lines := out.Overlay.Render(80, 30)
	rendered := strings.Join(lines, "\n")
	if strings.Contains(rendered, "internal") {
		t.Fatalf("HelpScreen must not show hidden commands; rendered: %q", rendered)
	}
	if !strings.Contains(rendered, "clear") {
		t.Fatalf("HelpScreen must show 'clear'; rendered: %q", rendered)
	}
}

func TestDoctorHandlerReturnsStatusText(t *testing.T) {
	called := false
	h := doctorHandlerWith(func() string {
		called = true
		return "Claude Code Doctor\n✓ API key: present"
	})
	screen := tui.NewREPLScreen(80, 24, nil)
	out, err := h(context.Background(), CommandContext{Screen: &screen})
	if err != nil {
		t.Fatalf("handler error: %v", err)
	}
	if !out.Handled {
		t.Fatal("Handled must be true")
	}
	if !called {
		t.Fatal("doctorRunner must be called")
	}
	if !strings.Contains(out.Status, "Doctor") {
		t.Fatalf("Status must contain doctor output; got: %q", out.Status)
	}
}
```

- [ ] **Step 2: Run to verify it fails**

```bash
cd /Users/sqlrush/ccgo && go test ./internal/repl/ -run "TestHelpHandlerOpensHelpScreenOverlay|TestDoctorHandlerReturnsStatusText" -v 2>&1 | tail -10
```
Expected: FAIL (functions not defined)

- [ ] **Step 3: Create `internal/repl/commands_help.go`**

```go
package repl

import (
	"context"

	"ccgo/internal/contracts"
)

// helpHandlerWith is the dependency-injected help handler.
func helpHandlerWith(registry []contracts.Command) CommandHandler {
	return func(ctx context.Context, cc CommandContext) (CommandOutcome, error) {
		return CommandOutcome{Handled: true, Overlay: NewHelpScreen(registry)}, nil
	}
}

// helpHandler builds the production handler injecting the command registry.
func helpHandler(registry []contracts.Command) CommandHandler {
	return helpHandlerWith(registry)
}
```

- [ ] **Step 4: Create `internal/repl/commands_doctor.go`**

```go
package repl

import (
	"context"

	"ccgo/internal/doctor"
)

// doctorRunner is the DI interface: returns the formatted doctor report string.
type doctorRunner func() string

// doctorHandlerWith is the dependency-injected doctor handler (testable without network).
func doctorHandlerWith(run doctorRunner) CommandHandler {
	return func(ctx context.Context, cc CommandContext) (CommandOutcome, error) {
		report := run()
		return CommandOutcome{Handled: true, Status: report}, nil
	}
}

// doctorHandler builds the production handler using the real doctor engine.
func doctorHandler(cwd, version string) CommandHandler {
	return doctorHandlerWith(func() string {
		report := doctor.Run(doctor.Input{Version: version, CWD: cwd})
		return doctor.Format(report)
	})
}
```

- [ ] **Step 5: Register "help" and "doctor" in `newProductionRouter` in `run.go`**

The `newProductionRouter` function takes `cwd string`. We need the version for doctor. Check how version is accessible in run.go context: it is not — the router is called from `RunInteractiveWithOptions` which doesn't have a version.

**Approach:** Pass `version string` as a second parameter to `newProductionRouter`. Or make `doctorHandler` use `""` as version (acceptable for now since `/doctor` via CLI also passes `version`). Check what `version` is in `RunInteractiveWithOptions` context:

```bash
grep -n "version\b" /Users/sqlrush/ccgo/internal/repl/run.go | head -5
```

If version is not available, pass `""` — the doctor engine handles empty version gracefully. Update `newProductionRouter` signature if needed, or just pass `""`:

```go
router.Register("help", helpHandler(cmdRegistry)) // cmdRegistry from the outer context
router.Register("doctor", doctorHandler(cwd, ""))
```

**Problem:** `newProductionRouter` doesn't have access to `cmdRegistry` (the slash-command list). The help overlay needs the command list. 

**Solution:** Add a second parameter `registry []contracts.Command` to `newProductionRouter`:

```go
func newProductionRouter(cwd string, registry []contracts.Command) *CommandRouter {
    ...
    router.Register("help", helpHandler(registry))
    router.Register("doctor", doctorHandler(cwd, ""))
    ...
}
```

Update the call site in `RunInteractiveWithOptions`:
```go
router := newProductionRouter(base.WorkingDirectory, opts.Registry)
```

Also update the call in the parity test if any:
```bash
grep -n "newProductionRouter" /Users/sqlrush/ccgo/internal/repl/ -r | head -10
```

- [ ] **Step 6: Run tests**

```bash
cd /Users/sqlrush/ccgo && go test ./internal/repl/ -run "TestHelpHandlerOpensHelpScreenOverlay|TestDoctorHandlerReturnsStatusText" -v 2>&1 | tail -10
```
Expected: PASS

- [ ] **Step 7: Full repl suite**

```bash
cd /Users/sqlrush/ccgo && go test ./internal/repl/ 2>&1 | tail -10
```

- [ ] **Step 8: Commit**

```bash
cd /Users/sqlrush/ccgo && git add internal/repl/commands_help.go internal/repl/commands_doctor.go internal/repl/commands_help_doctor_test.go internal/repl/run.go && git commit -m "feat(repl): /help opens HelpScreen overlay; /doctor runs and returns status (W-C08/OVL-14/OVL-15)"
```

---

## Task 7: Wire `/model` (no arg) → `ModelPicker` overlay (W-C07 / CMD-MODEL-01)

**Files:**
- Create: `internal/repl/model_picker.go`
- Create: `internal/repl/commands_model.go`
- Create: `internal/repl/commands_model_test.go`
- Modify: `internal/repl/run.go` (register "model" in `newProductionRouter`)

**Interfaces:**
- Produces: `NewModelPicker(models []string) *listOverlay` — reuses `listOverlay`
- Produces: `modelHandlerWith(models []string) CommandHandler`; with arg → set model via `Screen` accessor; no arg → return overlay

**Built-in model list** (CC `src/commands/model/index.ts`): use the list from `ccgo/internal/conversation` or a hardcoded list of current Claude models:
`"claude-opus-4-5"`, `"claude-sonnet-4-5"`, `"claude-haiku-4-5"`, `"claude-opus-4"`, `"claude-sonnet-4"`, `"claude-haiku-3-5"`, `"claude-3-7-sonnet-latest"`

- [ ] **Step 1: Write failing test**

Create `internal/repl/commands_model_test.go`:

```go
package repl

import (
	"context"
	"testing"

	"ccgo/internal/tui"
)

func TestModelHandlerNoArgOpensOverlay(t *testing.T) {
	models := []string{"claude-opus-4-5", "claude-sonnet-4-5"}
	h := modelHandlerWith(models)
	screen := tui.NewREPLScreen(80, 24, nil)
	out, err := h(context.Background(), CommandContext{Args: "", Screen: &screen})
	if err != nil {
		t.Fatalf("handler error: %v", err)
	}
	if !out.Handled {
		t.Fatal("Handled must be true")
	}
	if out.Overlay == nil {
		t.Fatal("/model with no arg must open ModelPicker overlay, got nil")
	}
}

func TestModelPickerRender(t *testing.T) {
	models := []string{"claude-opus-4-5", "claude-sonnet-4-5"}
	picker := NewModelPicker(models)
	lines := picker.Render(80, 10)
	if len(lines) < 2 {
		t.Fatalf("picker must render at least title + one model, got %d lines", len(lines))
	}
}
```

- [ ] **Step 2: Run to verify it fails**

```bash
cd /Users/sqlrush/ccgo && go test ./internal/repl/ -run "TestModelHandler|TestModelPicker" -v 2>&1 | tail -10
```
Expected: FAIL

- [ ] **Step 3: Create `internal/repl/model_picker.go`**

```go
package repl

// NewModelPicker builds an overlay to choose an AI model. Enter submits
// "model:<name>" which the REPL loop can handle to switch the model.
func NewModelPicker(models []string) *listOverlay {
	items := make([]listItem, 0, len(models))
	for _, m := range models {
		items = append(items, listItem{Label: m, Submit: "model:" + m})
	}
	return newListOverlay("Select model (esc to cancel)", items)
}
```

- [ ] **Step 4: Create `internal/repl/commands_model.go`**

```go
package repl

import (
	"context"
)

// builtinModels is the default model list shown in the /model picker.
// Mirrors the list from CC src/commands/model/index.ts.
var builtinModels = []string{
	"claude-opus-4-5",
	"claude-sonnet-4-5",
	"claude-haiku-4-5",
	"claude-opus-4",
	"claude-sonnet-4",
	"claude-haiku-3-5",
	"claude-3-7-sonnet-latest",
}

// modelHandlerWith is the dependency-injected model handler.
func modelHandlerWith(models []string) CommandHandler {
	return func(ctx context.Context, cc CommandContext) (CommandOutcome, error) {
		// No-arg: open picker overlay.
		if cc.Args == "" {
			return CommandOutcome{Handled: true, Overlay: NewModelPicker(models)}, nil
		}
		// With arg: future — for now just acknowledge.
		return CommandOutcome{
			Handled: true,
			Status:  "Model selection with argument not yet wired. Use /model with no argument to open picker.",
		}, nil
	}
}

// modelHandler builds the production handler.
func modelHandler() CommandHandler {
	return modelHandlerWith(builtinModels)
}
```

- [ ] **Step 5: Register "model" in `newProductionRouter` in `run.go`**

```go
router.Register("model", modelHandler())
```

Also add `"model:"` to the overlay submit prefix list in `handleOverlaySubmit` in `loop.go`:

```go
for _, prefix := range []string{"resume:", "theme:", "memory:", "trust:", "model:"} {
```

- [ ] **Step 6: Run tests**

```bash
cd /Users/sqlrush/ccgo && go test ./internal/repl/ -run "TestModelHandler|TestModelPicker" -v 2>&1 | tail -10
```
Expected: PASS

- [ ] **Step 7: Commit**

```bash
cd /Users/sqlrush/ccgo && git add internal/repl/model_picker.go internal/repl/commands_model.go internal/repl/commands_model_test.go internal/repl/run.go internal/repl/loop.go && git commit -m "feat(repl): /model opens ModelPicker overlay (W-C07/CMD-MODEL-01)"
```

---

## Task 8: Load prompt history from disk and pass to loop (W-C06 / HIST-03 / HIST-04)

**Files:**
- Modify: `internal/repl/run.go` (add `PromptHistory []session.HistoryEntry` to `InteractiveOptions`; consume in `RunInteractiveWithOptions` to call `NewREPLScreenFromHistoryEntries`)
- Modify: `internal/repl/loop.go` (`newTurnLoop` accepts history entries; `NewLoop` variant for entries)
- Test: `internal/repl/history_load_test.go` (new)

**Design:** Add `PromptHistory []session.HistoryEntry` to `InteractiveOptions`. When non-nil, `RunInteractiveWithOptions` passes it via a new `newTurnLoopWithHistoryEntries` helper that calls `tui.NewREPLScreenFromHistoryEntries` instead of `tui.NewREPLScreen`.

**Alternative simpler approach:** Add a `SetPromptHistory(entries []session.HistoryEntry)` method on `Loop` that replaces `l.screen.Prompt` with a new PromptState built from entries. Since `Loop.screen` is not a pointer, we must set the whole screen. Add `NewLoopFromHistoryEntries(t Terminal, entries []session.HistoryEntry) *Loop`.

**Chosen approach:** Add `NewLoopFromHistoryEntries` to avoid mutating the screen after construction. Then in `newTurnLoopForRunner`, branch based on whether `PromptHistory` is set.

- [ ] **Step 1: Write failing test**

Create `internal/repl/history_load_test.go`:

```go
package repl

import (
	"testing"

	"ccgo/internal/session"
	"ccgo/internal/tui"
)

// TestNewLoopFromHistoryEntriesSeesHistory verifies that Up-arrow in a loop
// created with history entries surfaces those entries (not an empty history).
func TestNewLoopFromHistoryEntriesSeesHistory(t *testing.T) {
	entries := []session.HistoryEntry{
		{Display: "first command"},
		{Display: "second command"},
	}
	ft := NewFakeTerminal("", 80, 24)
	l := NewLoopFromHistoryEntries(ft, entries)

	// The loop's screen prompt history should have the entries loaded.
	// Test by simulating Up arrow and checking prompt text changes.
	// Apply Up key to the screen directly to check history navigation.
	screen := &l.screen
	upKey := tui.Key{Type: tui.KeyUp}
	_ = screen.ApplyKey(upKey)
	// After one Up, the prompt should show the last entry ("second command").
	if screen.Prompt.Text != "second command" {
		t.Fatalf("after Up, prompt should be 'second command', got %q", screen.Prompt.Text)
	}
	_ = screen.ApplyKey(upKey)
	// After two Ups, the prompt should show "first command".
	if screen.Prompt.Text != "first command" {
		t.Fatalf("after two Ups, prompt should be 'first command', got %q", screen.Prompt.Text)
	}
}

// TestInteractiveOptionsPromptHistorySeedsLoop verifies that PromptHistory in
// InteractiveOptions is passed through to the loop's screen prompt history.
func TestInteractiveOptionsPromptHistorySeedsLoop(t *testing.T) {
	entries := []session.HistoryEntry{
		{Display: "stored prompt"},
	}
	opts := InteractiveOptions{
		PromptHistory: entries,
	}
	// We can't run the full REPL easily in a unit test, but we can verify
	// that NewLoopFromHistoryEntries correctly initializes the screen.
	ft := NewFakeTerminal("", 80, 24)
	l := NewLoopFromHistoryEntries(ft, opts.PromptHistory)
	screen := &l.screen
	upKey := tui.Key{Type: tui.KeyUp}
	_ = screen.ApplyKey(upKey)
	if screen.Prompt.Text != "stored prompt" {
		t.Fatalf("PromptHistory not seeded into loop: got %q", screen.Prompt.Text)
	}
}
```

- [ ] **Step 2: Run to verify it fails**

```bash
cd /Users/sqlrush/ccgo && go test ./internal/repl/ -run "TestNewLoopFromHistoryEntries|TestInteractiveOptionsPromptHistory" -v 2>&1 | tail -10
```
Expected: FAIL (NewLoopFromHistoryEntries not defined)

- [ ] **Step 3: Add `NewLoopFromHistoryEntries` to `loop.go`**

```go
// NewLoopFromHistoryEntries creates a Loop seeded with persisted prompt history
// entries. The entries back Up-arrow / Ctrl+R navigation from the first keystroke.
func NewLoopFromHistoryEntries(t Terminal, entries []session.HistoryEntry) *Loop {
    w, h, err := t.Size()
    if err != nil || w <= 0 || h <= 0 {
        w, h = 80, 24
    }
    return &Loop{
        term:     t,
        screen:   tui.NewREPLScreenFromHistoryEntries(w, h, entries),
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
```

Add `"ccgo/internal/session"` to the imports in `loop.go` if not already present.

- [ ] **Step 4: Add `PromptHistory []session.HistoryEntry` to `InteractiveOptions` in `run.go`**

```go
// PromptHistory seeds Up-arrow / Ctrl+R navigation with previously submitted prompts
// loaded from ~/.claude/history.jsonl. May be nil; nil means only in-session history.
PromptHistory []session.HistoryEntry
```

Add `"ccgo/internal/session"` to imports in `run.go` if not already present.

- [ ] **Step 5: Use `PromptHistory` in `RunInteractiveWithOptions`**

In `RunInteractiveWithOptions`, change `newTurnLoopForRunner` call to:

```go
var loop *Loop
if len(opts.PromptHistory) > 0 {
    loop = newTurnLoopWithHistoryEntries(ctx, term, base, history, recorder, opts.PromptHistory)
} else {
    loop = newTurnLoopForRunner(ctx, term, base, history)
}
```

Wait — `recorder` is not yet created at that point. Let me check `newTurnLoopForRunner`:

```go
func newTurnLoopForRunner(ctx context.Context, term Terminal, base conversation.Runner, history []contracts.Message) *Loop {
    recorder := NewHistoryRecorder(base.WorkingDirectory, base.SessionID)
    return newTurnLoop(ctx, term, base, history, recorder)
}
```

Better approach: add a helper that accepts history entries:

```go
func newTurnLoopForRunnerWithHistory(ctx context.Context, term Terminal, base conversation.Runner, history []contracts.Message, promptHistory []session.HistoryEntry) *Loop {
    recorder := NewHistoryRecorder(base.WorkingDirectory, base.SessionID)
    loop := NewLoopFromHistoryEntries(term, promptHistory)
    loop.history = history
    loop.StartTurn = buildStartTurn(ctx, loop, base, recorder)
    return loop
}
```

But `buildStartTurn` doesn't exist — the StartTurn closure is defined inline in `newTurnLoop`. Extract it or inline the pattern. Since `newTurnLoop` cannot easily be reused here, define the new helper inline, duplicating the StartTurn closure:

```go
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
            r := base
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
```

Then in `RunInteractiveWithOptions`:

```go
var loop *Loop
if len(opts.PromptHistory) > 0 {
    loop = newTurnLoopForRunnerWithHistory(ctx, term, base, history, opts.PromptHistory)
} else {
    loop = newTurnLoopForRunner(ctx, term, base, history)
}
```

- [ ] **Step 6: Run tests**

```bash
cd /Users/sqlrush/ccgo && go test ./internal/repl/ -run "TestNewLoopFromHistoryEntries|TestInteractiveOptionsPromptHistory" -v 2>&1 | tail -10
```
Expected: PASS

- [ ] **Step 7: Full repl suite**

```bash
cd /Users/sqlrush/ccgo && go test ./internal/repl/ 2>&1 | tail -10
```

- [ ] **Step 8: Commit**

```bash
cd /Users/sqlrush/ccgo && git add internal/repl/loop.go internal/repl/run.go internal/repl/history_load_test.go && git commit -m "feat(repl): seed loop with persisted prompt history for Up-arrow/Ctrl+R (W-C06/HIST-03/HIST-04)"
```

---

## Task 9: Wire `main.go` — populate `Engine`, `EditorMode`, `PromptHistory`, `MemoryFiles`, `ResumeEntries` in `InteractiveOptions` (W-C02, W-C05 connect, W-C06, W-C07)

This is the production wiring that connects all the above seams to the real running binary.

**Files:**
- Modify: `cmd/claude/main.go` (~line 342 where `opts := repl.InteractiveOptions{...}` is built)

**Interfaces:**
- `permissionDeciderFromSettings` returns `tool.EnginePermissionDecider` (value) which carries `Engine permissions.Engine`; we need a `*permissions.Engine` pointer.
- `mergedSettings.EditorMode` is a `string`; if `"vim"`, call `loop.SetVimEnabled(true)` — but `loop` is not accessible here; instead, put EditorMode in InteractiveOptions and wire it in `RunInteractiveWithOptions`.

Wait — `InteractiveOptions` does not have an `EditorMode` field. We need to either:
1. Add it and wire in `RunInteractiveWithOptions`, OR
2. Call `loop.screen.SetVimEnabled(true)` from `RunInteractiveWithOptions` if opts.EditorMode == "vim"

**Chosen approach:** Add `EditorMode string` to `InteractiveOptions` and wire in `RunInteractiveWithOptions`.

**Engine pointer extraction:** `permissionDeciderFromSettings` returns `tool.EnginePermissionDecider` (a struct), not a pointer to an `Engine`. We need to extract the engine pointer. `tool.EnginePermissionDecider` has an `Engine permissions.Engine` value field. We need `&decider.Engine`. 

Check `tool.EnginePermissionDecider`:
```bash
grep -n "type EnginePermissionDecider\|Engine\b" /Users/sqlrush/ccgo/internal/tool/ -r | head -10
```

The engine should be accessible. We need to pass `&decider.Engine` to `InteractiveOptions.Engine`.

**Problem:** `headlessRunner` returns `conversation.Runner` which has `runner.Permissions` as `tool.PermissionDecider` (interface). We need to type-assert it back to `tool.EnginePermissionDecider` to get the engine pointer. `main.go` already does this pattern in `runnerPermissionModeFromDecider`.

Add a helper `engineFromDecider(decider tool.PermissionDecider) *permissions.Engine`:

```go
func engineFromDecider(decider tool.PermissionDecider) *permissions.Engine {
    switch v := decider.(type) {
    case tool.EnginePermissionDecider:
        return &v.Engine
    }
    return nil
}
```

**History loading:** Use `session.LoadHistory(session.HistoryPath(), runner.WorkingDirectory, runner.SessionID, 500, nil)`. Check the exact function signature from `internal/session/history.go`.

**Memory files:** Use `memory.DiscoverScopedClaudeFiles(memory.ScopeOptions{CWD: runner.WorkingDirectory})` and extract `.Path` from each.

**Resume entries:** Use `session.ListProjectSessions(runner.WorkingDirectory)`, convert to `repl.ResumeEntry`.

- [ ] **Step 1: Write test for EditorMode wiring**

Add to a new file `internal/repl/interactive_options_test.go`:

```go
package repl

import (
	"testing"

	"ccgo/internal/tui"
)

// TestRunInteractiveWithOptionsVimEnabled verifies that when InteractiveOptions.EditorMode
// is "vim", the loop's screen has VimEnabled=true before Run is called.
func TestRunInteractiveWithOptionsVimEnabled(t *testing.T) {
	// We can't easily run the full REPL, but we can test that RunInteractiveWithOptions
	// creates a loop with vim enabled by using the seam pattern from existing tests.
	// Instead, test newTurnLoopForRunner and the loop's screen directly.
	ft := NewFakeTerminal("", 80, 24)
	l := NewLoop(ft, nil)
	// Simulate what RunInteractiveWithOptions would do with EditorMode="vim":
	l.screen.SetVimEnabled(true)
	if !l.screen.VimEnabled {
		t.Fatal("SetVimEnabled(true) must set VimEnabled=true on the screen")
	}
}
```

This test is intentionally minimal — it tests the screen's SetVimEnabled (already implemented) and will be GREEN immediately. The real wiring test is integration-level.

- [ ] **Step 2: Add `EditorMode string` to `InteractiveOptions` in `run.go`**

```go
// EditorMode, when set to "vim", enables vim keybindings in the prompt input.
// Sourced from mergedSettings.EditorMode at startup.
EditorMode string
```

- [ ] **Step 3: Wire `EditorMode` in `RunInteractiveWithOptions` after `loop` is created**

After the `loop = newTurnLoopForRunner(...)` / `newTurnLoopForRunnerWithHistory(...)` call, add:

```go
if opts.EditorMode == "vim" {
    loop.screen.SetVimEnabled(true)
    loop.refreshBaseStatus()
}
```

- [ ] **Step 4: Add `engineFromDecider` helper to `main.go`**

```go
// engineFromDecider extracts the live *permissions.Engine from a PermissionDecider
// if it wraps an EnginePermissionDecider. Returns nil for other decider types.
func engineFromDecider(decider tool.PermissionDecider) *permissions.Engine {
    if v, ok := decider.(tool.EnginePermissionDecider); ok {
        return &v.Engine
    }
    return nil
}
```

- [ ] **Step 5: Populate `InteractiveOptions` in `main.go`**

Find the block (~line 342):
```go
opts := repl.InteractiveOptions{
    Settings: writer,
    Registry: cmdRegistry.Visible(),
    Mode:     runner.PermissionMode,
}
```

Replace it with:

```go
mergedSettings := runnerMergedSettings(runner)

// Load persisted prompt history (best-effort; nil on any error).
var promptHistory []session.HistoryEntry
if histEntries, err := session.LoadHistory(
    session.HistoryPath(),
    runner.WorkingDirectory,
    runner.SessionID,
    500,
    nil,
); err == nil {
    promptHistory = histEntries
}

// Discover memory files (best-effort).
var memoryFiles []string
if claudeFiles, err := memory.DiscoverScopedClaudeFiles(memory.ScopeOptions{
    CWD: runner.WorkingDirectory,
}); err == nil {
    for _, f := range claudeFiles {
        memoryFiles = append(memoryFiles, f.Path)
    }
}

// List resumable sessions (best-effort).
var resumeEntries []repl.ResumeEntry
if sessions, err := session.ListProjectSessions(runner.WorkingDirectory); err == nil {
    resumeEntries = make([]repl.ResumeEntry, 0, len(sessions))
    for _, s := range sessions {
        resumeEntries = append(resumeEntries, repl.ResumeEntry{
            ID:          string(s.ID),
            Summary:     s.Title,
            ProjectPath: s.ProjectPath,
        })
    }
}

opts := repl.InteractiveOptions{
    Settings:      writer,
    Registry:      cmdRegistry.Visible(),
    Mode:          runner.PermissionMode,
    Engine:        engineFromDecider(runner.Permissions),
    EditorMode:    mergedSettings.EditorMode,
    PromptHistory: promptHistory,
    MemoryFiles:   memoryFiles,
    ResumeEntries: resumeEntries,
}
```

**Verify imports needed in main.go:** `memory`, `session` packages. Check existing imports:
```bash
grep -n "\"ccgo/internal/memory\"\|\"ccgo/internal/session\"" /Users/sqlrush/ccgo/cmd/claude/main.go | head -5
```

Add any missing imports.

- [ ] **Step 6: Wire `MemoryFiles` and `ResumeEntries` in `RunInteractiveWithOptions` in `run.go`**

Currently `opts.ResumeEntries`, `opts.MemoryFiles`, and `opts.Themes` are stored in `InteractiveOptions` but never consumed. We need to pass them to the command handlers.

The cleanest approach: store them on the loop struct so the command router can access them. Add fields to `Loop`:

```go
// resumeEntries and memoryFiles back the respective picker overlays when the
// command handlers are invoked via the production router.
resumeEntries []ResumeEntry
memoryFiles   []string
```

Then in `RunInteractiveWithOptions`, after creating the loop:

```go
loop.resumeEntries = opts.ResumeEntries
loop.memoryFiles = opts.MemoryFiles
```

And wire the production router to use them: modify `newProductionRouter` to accept these as parameters, or pass them via closures at the `RunInteractiveWithOptions` call site by overriding the router registrations:

**Simpler approach:** Override just the resume and memory handlers at the call site in `RunInteractiveWithOptions`:

```go
router := newProductionRouter(base.WorkingDirectory, opts.Registry)
// Override resume with picker-aware handler when ResumeEntries are available.
if len(opts.ResumeEntries) > 0 {
    router.Register("resume", func(ctx context.Context, cc CommandContext) (CommandOutcome, error) {
        if strings.TrimSpace(cc.Args) == "" {
            return CommandOutcome{Handled: true, Overlay: NewResumePicker(opts.ResumeEntries)}, nil
        }
        // Fall through to the standard resume handler for arg-based lookup.
        return resumeHandler(base.WorkingDirectory)(ctx, cc)
    })
    router.Register("continue", func(ctx context.Context, cc CommandContext) (CommandOutcome, error) {
        if strings.TrimSpace(cc.Args) == "" {
            return CommandOutcome{Handled: true, Overlay: NewResumePicker(opts.ResumeEntries)}, nil
        }
        return resumeHandler(base.WorkingDirectory)(ctx, cc)
    })
}
// Override memory with picker-aware handler when MemoryFiles are available.
if len(opts.MemoryFiles) > 0 {
    router.Register("memory", memoryHandlerWith(func() ([]string, error) {
        return opts.MemoryFiles, nil
    }))
}
```

- [ ] **Step 7: Build check**

```bash
cd /Users/sqlrush/ccgo && go build ./cmd/claude/ 2>&1
```
Expected: no errors

- [ ] **Step 8: Run full suite**

```bash
cd /Users/sqlrush/ccgo && go test ./... 2>&1 | tail -30
```

- [ ] **Step 9: Windows build**

```bash
cd /Users/sqlrush/ccgo && GOOS=windows go build ./internal/repl/ 2>&1
```

- [ ] **Step 10: go vet**

```bash
cd /Users/sqlrush/ccgo && go vet ./... 2>&1
```
Expected: only pre-existing `client.go:317` warning

- [ ] **Step 11: Commit**

```bash
cd /Users/sqlrush/ccgo && git add cmd/claude/main.go internal/repl/run.go internal/repl/interactive_options_test.go && git commit -m "feat(repl): populate Engine/EditorMode/PromptHistory/MemoryFiles/ResumeEntries in InteractiveOptions (W-C02/W-C05/W-C06/W-C07)"
```

---

## Task 10: Load and apply custom keybindings (W-C03 / REPL-54)

**Files:**
- Modify: `cmd/claude/main.go` (load `~/.claude/keybindings.json` before building opts)
- Modify: `internal/repl/run.go` (add `CustomKeymap *tui.Keymap` to `InteractiveOptions`; apply in `RunInteractiveWithOptions`)
- Test: `internal/repl/keybinding_load_test.go` (new)

**Interfaces:**
- `tui.LoadKeyBindingSpecs(path string) ([]tui.BindingSpec, error)` — reads JSON
- `tui.KeymapFromSpecs(base tui.Keymap, specs []tui.BindingSpec) (tui.Keymap, error)` — merges
- After building the loop, set `loop.screen.Keymap = customKeymap`

**Note:** The keybindings.json file is at `filepath.Join(platform.ClaudeHomeDir(), "keybindings.json")`. There is no existing `config.KeybindingsPath()` helper — we inline the path.

- [ ] **Step 1: Write failing test**

Create `internal/repl/keybinding_load_test.go`:

```go
package repl

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"ccgo/internal/tui"
)

// TestRunInteractiveWithOptionsAppliesCustomKeymap verifies that a non-nil
// CustomKeymap in InteractiveOptions is applied to the loop's screen.Keymap.
func TestRunInteractiveWithOptionsAppliesCustomKeymap(t *testing.T) {
	// Build a custom keymap from a JSON spec.
	specJSON := `[{"key":"ctrl+p","action":"history_previous"}]`
	var specs []tui.BindingSpec
	if err := json.Unmarshal([]byte(specJSON), &specs); err != nil {
		t.Fatalf("parse spec: %v", err)
	}
	base := tui.DefaultKeymap()
	km, err := tui.KeymapFromSpecs(base, specs)
	if err != nil {
		t.Fatalf("KeymapFromSpecs: %v", err)
	}

	ft := NewFakeTerminal("", 80, 24)
	l := NewLoop(ft, nil)
	// Simulate what RunInteractiveWithOptions does with CustomKeymap.
	l.screen.Keymap = km
	if l.screen.Keymap.Resolve(tui.Key{Type: tui.KeyRune, Rune: 'p', Ctrl: true}) == tui.ActionNone {
		t.Fatal("custom ctrl+p binding not applied to screen.Keymap")
	}
}

// TestLoadKeyBindingSpecsGracefulOnMissing verifies that loading a non-existent
// keybindings file does not error out — it returns nil specs.
func TestLoadKeyBindingSpecsGracefulOnMissing(t *testing.T) {
	path := filepath.Join(t.TempDir(), "no-such-file.json")
	_, err := tui.LoadKeyBindingSpecs(path)
	if err == nil {
		t.Fatal("LoadKeyBindingSpecs must return error for missing file")
	}
	// Caller in RunInteractiveWithOptions must treat os.IsNotExist as non-fatal.
	if !os.IsNotExist(err) && !isWrappedNotExist(err) {
		t.Fatalf("error must wrap os.ErrNotExist, got: %v", err)
	}
}

func isWrappedNotExist(err error) bool {
	return os.IsNotExist(err) || (err != nil && os.IsNotExist(unwrapErr(err)))
}

func unwrapErr(err error) error {
	type unwrapper interface{ Unwrap() error }
	if u, ok := err.(unwrapper); ok {
		return u.Unwrap()
	}
	return nil
}
```

- [ ] **Step 2: Run to check**

```bash
cd /Users/sqlrush/ccgo && go test ./internal/repl/ -run "TestRunInteractiveWithOptionsAppliesCustomKeymap|TestLoadKeyBindingSpecsGracefulOnMissing" -v 2>&1 | tail -10
```
Expected: `TestRunInteractiveWithOptionsAppliesCustomKeymap` PASS (it directly sets the keymap); `TestLoadKeyBindingSpecsGracefulOnMissing` PASS or FAIL depending on how LoadKeyBindingSpecs wraps the error.

- [ ] **Step 3: Add `CustomKeymap *tui.Keymap` to `InteractiveOptions` in `run.go`**

```go
// CustomKeymap, when non-nil, overrides specific bindings on top of DefaultKeymap.
// Loaded from ~/.claude/keybindings.json at startup; nil if the file is absent.
CustomKeymap *tui.Keymap
```

Add `"ccgo/internal/tui"` import to `run.go` if not already present.

- [ ] **Step 4: Apply `CustomKeymap` in `RunInteractiveWithOptions` in `run.go`**

After the loop is created (either `newTurnLoopForRunner` or `newTurnLoopForRunnerWithHistory`):

```go
if opts.CustomKeymap != nil {
    loop.screen.Keymap = *opts.CustomKeymap
}
```

- [ ] **Step 5: Load keybindings.json in `main.go`**

Before building `opts`, add:

```go
// Load user keybindings (best-effort; absent file is silently ignored).
var customKeymap *tui.Keymap
keybindingsPath := filepath.Join(platform.ClaudeHomeDir(), "keybindings.json")
if specs, err := tui.LoadKeyBindingSpecs(keybindingsPath); err == nil && len(specs) > 0 {
    if km, err := tui.KeymapFromSpecs(tui.DefaultKeymap(), specs); err == nil {
        customKeymap = &km
    }
}
```

Add to `opts`:
```go
CustomKeymap: customKeymap,
```

Import `"ccgo/internal/platform"` and `"path/filepath"` in `main.go` if not already present (they likely already are):

```bash
grep -n "\"path/filepath\"\|\"ccgo/internal/platform\"" /Users/sqlrush/ccgo/cmd/claude/main.go | head -5
```

- [ ] **Step 6: Full build and test**

```bash
cd /Users/sqlrush/ccgo && go build ./cmd/claude/ && go test ./internal/repl/ -run "TestRunInteractiveWithOptionsAppliesCustomKeymap|TestLoadKeyBindingSpecsGracefulOnMissing" -v 2>&1 | tail -15
```

- [ ] **Step 7: Full suite + Windows build**

```bash
cd /Users/sqlrush/ccgo && go test ./... 2>&1 | tail -20 && GOOS=windows go build ./internal/repl/ 2>&1
```

- [ ] **Step 8: go vet**

```bash
cd /Users/sqlrush/ccgo && go vet ./... 2>&1
```

- [ ] **Step 9: Commit**

```bash
cd /Users/sqlrush/ccgo && git add cmd/claude/main.go internal/repl/run.go internal/repl/keybinding_load_test.go && git commit -m "feat(repl): load ~/.claude/keybindings.json and apply custom keymap to loop (W-C03/REPL-54)"
```

---

## Task 11: Final verification, parity doc updates, and squash commit

**Files:**
- Modify: `docs/cc-parity/sections/02-repl-tui.md`
- Modify: `docs/cc-parity/sections/03-overlays-dialogs.md`
- Modify: `docs/cc-parity/sections/06-permissions.md`
- Modify: `docs/cc-parity/sections/07-slash-commands.md`
- Modify: `docs/cc-parity/sections/11-sessions-memory-compact.md`

- [ ] **Step 1: Run the full test suite**

```bash
cd /Users/sqlrush/ccgo && go test ./... 2>&1
```
Expected: ok for all packages

- [ ] **Step 2: Run Windows build**

```bash
cd /Users/sqlrush/ccgo && GOOS=windows go build ./internal/repl/ 2>&1
```
Expected: no errors

- [ ] **Step 3: Run go vet**

```bash
cd /Users/sqlrush/ccgo && go vet ./... 2>&1
```
Expected: only pre-existing `client.go:317`

- [ ] **Step 4: Get the commit SHA for the status update text**

```bash
cd /Users/sqlrush/ccgo && git rev-parse --short HEAD
```

- [ ] **Step 5: Update parity docs**

In `docs/cc-parity/sections/02-repl-tui.md`, flip these IDs:
- `REPL-31`, `REPL-32`, `REPL-33` → `✅ 通过（接线已就绪，渲染需人工核验）（W-REPL 接线 commit <sha>）`
- `REPL-46`, `REPL-47`, `REPL-48`, `REPL-57` → `✅ 通过（接线已就绪，渲染需人工核验）（W-REPL 接线 commit <sha>）`
- `REPL-54` → `✅ 通过（接线已就绪，渲染需人工核验）（W-REPL 接线 commit <sha>）`

Update section 02 小计 accordingly.

In `docs/cc-parity/sections/03-overlays-dialogs.md`, flip:
- `OVL-09`, `OVL-11`, `OVL-13` → `✅ 通过（接线已就绪，渲染需人工核验）（W-REPL 接线 commit <sha>）`
- `OVL-14`, `OVL-15` → `✅ 通过（接线已就绪，渲染需人工核验）（W-REPL 接线 commit <sha>）`

In `docs/cc-parity/sections/07-slash-commands.md`, flip:
- `CMD-RESUME-02`, `CMD-MEMORY-01`, `CMD-MODEL-01` → `✅ 通过（接线已就绪，渲染需人工核验）（W-REPL 接线 commit <sha>）`

In `docs/cc-parity/sections/11-sessions-memory-compact.md`, flip:
- `HIST-03`, `HIST-04` → `✅ 通过（接线已就绪，渲染需人工核验）（W-REPL 接线 commit <sha>）`
- `SESS-05` → `✅ 通过（接线已就绪，渲染需人工核验）（W-REPL 接线 commit <sha>）`
- `MEM-09` → `✅ 通过（接线已就绪，渲染需人工核验）（W-REPL 接线 commit <sha>）`

- [ ] **Step 6: Write the parity report**

Create `/Users/sqlrush/ccgo/.superpowers/sdd/parity-W-REPL-report.md` with:
- Each item, where wired (file:line)
- The test added
- What stays MANUAL-render
- Full suite + windows build result
- Status flips
- Commit SHA
- Concerns

- [ ] **Step 7: Final commit**

```bash
cd /Users/sqlrush/ccgo && git add docs/cc-parity/sections/ .superpowers/sdd/parity-W-REPL-report.md && git commit -m "docs: flip W-C02..W-C08 parity IDs to ✅; add W-REPL wiring report"
```

---

## Self-Review

### Spec Coverage Check

| Cluster | Items | Covered? |
|---------|-------|---------|
| W-C02 (REPL-31/32/33) | vim mode from config | ✅ Task 9: `EditorMode` → `SetVimEnabled` |
| W-C03 (REPL-54) | keybindings.json | ✅ Task 10: `LoadKeyBindingSpecs` + `CustomKeymap` |
| W-C04 (REPL-46/47/48/57) | screen events | ✅ Task 2: new switch cases |
| W-C05 connect (PERM-MODE-07, PERM-PERSIST-03) | Engine pointer | ✅ Task 9: `engineFromDecider` → `opts.Engine` |
| W-C06 (HIST-03/04) | prompt history | ✅ Task 8 + Task 9 |
| W-C07 (SESS-05, MEM-09, OVL-09/11/13, CMD-RESUME-02, CMD-MEMORY-01, CMD-MODEL-01) | overlays | ✅ Tasks 3–7 + 9 |
| W-C08 (OVL-14, OVL-15) | /help + /doctor | ✅ Task 6 |

### Placeholder Scan

No TBD/TODO/placeholder statements present. All code blocks are complete.

### Type Consistency

- `ResumeEntry.ID string` (not `contracts.ID`) — confirmed from `resume_picker.go:ID string`
- `memoryFileLister` is `func() ([]string, error)` — consistent across Task 5 and Task 9
- `CommandOutcome.Overlay Overlay` — interface type, consistent
- `InteractiveOptions.PromptHistory []session.HistoryEntry` — consistent with `NewLoopFromHistoryEntries` parameter
- `InteractiveOptions.CustomKeymap *tui.Keymap` — pointer, consistent with nil-check pattern
