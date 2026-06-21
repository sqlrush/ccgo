# Commands Coverage (Phase 6b) Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Bring in-scope slash- and CLI-command coverage from ~22% toward functional parity. Today ccgo has 18 builtin slash commands, but every "interactive" one (`/resume`, `/config`, `/mcp`, `/memory`) only produces a **text summary** rendered into the transcript — none performs a live effect, and the REPL loop has **zero slash-command awareness** (it passes `/foo` verbatim to `RunTurn`, which parses it but cannot drive the live screen). Many CC commands are entirely absent (`/agents`, `/permissions`, `/context`, `/export`, `/init`, `/review`, `/doctor`, `/theme`, `/effort`, `/vim`, `/hooks`, `/ide`) and the CLI subcommands `doctor`, `update`, `agents`, `completion` do not exist. This phase implements the in-scope set with **real behavior** and wires a REPL-side command dispatcher so interactive commands take effect live.

**Architecture:** Slash commands fall into three behavior classes, and we keep them cleanly separated:

1. **Prompt commands** (`/init`, `/review`) — already supported by the registry's `CommandPrompt` path; they only need a builtin definition + prompt template that expands to model-bound text. No new dispatch.
2. **Pure-data / formatting commands** (`/context`, `/doctor`, `/hooks`) — produce a deterministic text/ANSI report from local state. These add a new `commands.LocalCommandResult` type plus a `conversation.Runner` formatter (mirroring the existing `formatCostSummary`/`formatMCPCommandSummary` pattern at `internal/conversation/run.go:116-158`). They run identically headless and interactive.
3. **Live-effect commands** (`/resume`, `/permissions`, `/agents`, `/theme`, `/effort`, `/vim`, `/export`, `/ide`) — must change runtime/persisted state. We add a new `internal/repl/commands.go` dispatcher: a small `CommandRouter` consulted by the REPL loop **before** a prompt is sent to the model. It parses the slash input, and for live-effect commands invokes a typed handler (resume picker, settings writer, etc.) that mutates the loop's `history`/screen or writes a settings file. Non-live-effect slash input falls through to the normal `StartTurn → RunTurn` path unchanged (so headless `--print /foo` keeps working). This is the "library built, glue missing" pattern: the registry, `ExecuteSlashCommand`, session list, and `permissions.Engine.ApplyUpdate` all exist; we add the dispatcher glue + the few missing formatters/writers.

The persistence primitives we reuse: `config.WriteUserSettingsDocument(map[string]any)` / `config.WriteSettingsDocument(path, map[string]any)` (`internal/config/user_settings.go:30,34`), `config.ReadSettingsDocument` (`:17`), and `permissions.Engine.ApplyUpdate` (returns a **new** Engine). No typed `WriteSettings(contracts.Settings)` exists; we round-trip through the `map[string]any` document API, which is exactly how plugin enable/disable already persists (`config.SetPluginEnabledInSettingsFile`).

**Tech Stack:** Go 1.26; existing packages `internal/commands`, `internal/conversation`, `internal/repl`, `internal/tui`, `internal/contracts`, `internal/config`, `internal/permissions`, `internal/session`, `internal/compact`, `internal/plugins`, `internal/bootstrap`, `internal/messages`; `cmd/claude/main.go`. **No new third-party dependencies.**

## Global Constraints

Copied verbatim from the master roadmap (`docs/superpowers/plans/2026-06-21-00-master-roadmap.md` §6):

- **Module/toolchain:** `ccgo`, `go 1.26` (from `go.mod`).
- **Immutability (CRITICAL):** never mutate shared structs in place; return new copies. Copy the `conversation.Runner` value per turn before setting `OnEvent`/`Tools.Asker` (existing pattern). `permissions.Engine.ApplyUpdate` already returns a **new** engine — honor that.
- **Many small files:** one responsibility per file; target 150–350 lines (800 hard max).
- **Errors handled explicitly at every level; never swallow.** Terminal raw-mode `restore` and any acquired resource MUST be released on every exit path (`defer`).
- **Input validation at boundaries:** validate all external data (API responses, user input, file content, MCP server output); fail fast with clear messages.
- **No new third-party deps** unless the plan justifies it explicitly. Phase 1 added only `golang.org/x/term`. No bubbletea/tcell/charm.
- **Non-TTY safety:** interactive paths MUST NOT call `term.MakeRaw` when stdin/stdout isn't a tty; fall back to line mode. Tests MUST NOT depend on a real tty.
- **TDD:** every task writes a failing test first, then minimal code. Commit after each task. Run package tests with `go test ./internal/<pkg>/ -run TestName -v`; full suite `go test ./...`.
- **Verify against real code, distrust roadmap docs:** every assumed type name, field, constant, or CC behavior MUST be confirmed with `go doc`/`grep` (ccgo side) or by reading `/Users/sqlrush/agent/claude-code/src` (CC side) before writing the test — flag the exact command at the point of use.
- **Security:** no hardcoded secrets; tokens in keychain not plaintext (Phase 4); sandbox flag must actually enforce (Phase 7); never leak sensitive data in errors.

## Scope: IN vs DEFERRED

**IN this phase (Phase 6b):**
- Slash: `/resume` (real interactive resume), `/agents` editor, `/permissions` editor, `/context`, `/export`, `/init`, `/review`, `/doctor`, `/theme`, `/effort`, `/vim`, `/hooks`, `/ide` (CLI-side detect/connect only).
- CLI subcommands: `claude doctor`, `claude update`, `claude agents` (list), `claude completion`.

**DEFERRED / EXCLUDED:**
- `/login` `/logout` and `claude auth` → **Phase 4** (auth/OAuth). Do NOT implement here.
- Debug-only commands (`ant-trace`, `heapdump`, `mock-limits`, `reset-limits`, `thinkback*`, `debug-tool-call`, `perf-issue`, `ctx_viz`, `break-cache`, `backfill-sessions`, `good-claude`, `btw`, `passes`, `stickers`) → **OUT of scope** (gap-audit §6). Never implement.
- Cloud/remote/companion commands → OUT of scope.
- `/agents` and `claude agents` **create/edit** beyond the local `.claude/agents/*.md` file format: CC's full wizard mounts React/Ink; we implement the **file read/write + a non-interactive editor model** (parse, list, create, delete) and a minimal interactive list/create flow. We do NOT port the React wizard UI verbatim.
- `claude completion` for ant-only shells: CC's external build ships **no** completion handler (`cli/handlers/ant.js` is absent; the command is `hidden` and ant-gated — verified in CC `src/main.tsx:4439-4492`). We provide a **clean bash/zsh/fish static-script generator** (greenfield, justified below) since shell completion is genuinely useful and entirely local; it is small and dependency-free.
- `/hooks` and `/ide`: `/hooks` is **VIEW-ONLY** in CC (`HooksConfigMenu.tsx:3-12` docstring); we match that (read-only summary, no editor). `/ide` CLI side detects IDEs and connects to the `ide` MCP server; the actual extension is OUT of scope. We implement detection + an MCP-config toggle stub guarded so tests need no IDE/network.

## Current command inventory (code-verified 2026-06-21)

`internal/commands/registry.go:286-307` `BuiltinCommands()` registers 18 builtins:
`help, config, mcp, plugin, skills, memory, native, resume, clear, compact, cost, summary, release-notes, files, issue, status, model, output-style`.

| Command | Status today | Anchor |
|---|---|---|
| `/help /clear /compact /cost /summary /status /model /config /mcp /plugin /memory /skills /native /resume /files /issue /release-notes /output-style` | Present | `registry.go:286-307` |
| `/resume` | **text-only** — lists sessions, never resumes live | `slash.go:229`, `run.go:152,7099` |
| `/config /mcp /memory` | **text-only** summaries | `run.go:134-148` |
| `/agents /permissions` | **MISSING** | — |
| `/context /export /init /review /doctor /theme /effort /vim /hooks /ide` | **MISSING** | — |
| CLI `doctor update agents completion` | **MISSING** (only `plugin` CLI exists, `main.go:363`) | `main.go:197` |
| theme settings field | **MISSING** in `contracts.Settings` | — |
| effort settings field | **PRESENT** `Settings.EffortLevel` | `contracts/settings.go:55` |
| vim settings field | **MISSING** (runtime-only `tui.REPLScreen.VimEnabled`) | `tui/screen.go:54` |
| `.claude/agents/*.md` loader | **MISSING** (agents load via plugins only → `tool.AgentInfo`) | `tool/types.go:16`, `plugins/loader.go:1539` |

**Gap-audit discrepancies found:**
- Gap-audit §1/§4.25 says "17/~78 commands"; code shows **18** builtins. Minor.
- Gap-audit §4.25 implies `/resume` simply "doesn't resume". More precisely: it lists sessions as text via `formatResumeSummary` (`run.go:7099`) — the read path exists; only the **live-resume effect** is missing, and only in the REPL.
- Gap-audit §5 lists `/effort` among "missing"; the **settings field** `EffortLevel` already exists, so `/effort` only needs a writer + command, not schema work.

## File Structure

**New files:**
- `internal/commands/local_types.go` — new `LocalCommandResultType` constants (`Context`, `Doctor`, `Hooks`) + builtin registration helpers (or extend `slash.go` if small). *(May instead edit `slash.go`/`registry.go` directly; keep additions cohesive.)*
- `internal/repl/commands.go` — `CommandRouter`: REPL-side dispatch of live-effect slash commands.
- `internal/repl/commands_resume.go` — resume picker/loader bridging `session.ListProjectSessions` + `BuildResumeConversation` into the loop history.
- `internal/repl/commands_settings.go` — `/theme`, `/effort`, `/vim`, `/permissions` settings mutations via the document writer.
- `internal/repl/commands_export.go` — `/export` transcript renderer + file writer.
- `internal/agentfile/agentfile.go` — `.claude/agents/*.md` parse/format/list/save/delete (greenfield, mirrors CC `agentFileUtils.ts`).
- `internal/doctor/doctor.go` — health-check diagnostics shared by `/doctor` and `claude doctor`.
- `internal/contextreport/contextreport.go` — `/context` usage report (token breakdown).
- `cmd/claude/cli_doctor.go`, `cli_update.go`, `cli_agents.go`, `cli_completion.go` — CLI subcommand handlers *(or add functions to `main.go`; prefer separate files per the 800-line cap — `main.go` is already 4337 lines).*

**Modified files:**
- `internal/commands/registry.go` — add builtin definitions for the new commands.
- `internal/commands/slash.go` — register new `ExecuteBuiltinLocalCommand` cases + result types.
- `internal/conversation/run.go` — add formatters for new pure-data local result types in the `!shouldQuery` switch (`:116-158`).
- `internal/repl/loop.go` / `internal/repl/run.go` — consult `CommandRouter` before `StartTurn`.
- `internal/contracts/settings.go` — add `Theme` and `VimMode`/`EditorMode` fields.
- `cmd/claude/main.go` — top-level subcommand dispatch for `doctor`/`update`/`agents`/`completion` (mirror the `plugin` dispatch at `:197`).

---

## Task 1: REPL command-dispatch harness (router + loop seam)

**Why first:** every live-effect command needs a place to run inside the REPL. The loop currently has no slash awareness (verified: `grep -rn "ExecuteSlashCommand\|HasPrefix.*\"/\"" internal/repl/` → 0 hits). Build the seam + a trivial command (`/clear` live-effect) to prove the harness, then later tasks register handlers into it.

**Files:**
- Create: `internal/repl/commands.go`
- Modify: `internal/repl/loop.go` (consult router in `handleKey`'s `ScreenEventPromptSubmitted` branch)
- Test: `internal/repl/commands_test.go`

**Interfaces:**
- Produces:
  - `type CommandContext struct { Args string; Screen *tui.REPLScreen; History []contracts.Message; CWD string }`
  - `type CommandOutcome struct { Handled bool; NewHistory []contracts.Message; ReplaceHistory bool; Status string; SendToModel bool }`
  - `type CommandHandler func(ctx context.Context, cc CommandContext) (CommandOutcome, error)`
  - `type CommandRouter struct { handlers map[string]CommandHandler }`
  - `func NewCommandRouter() *CommandRouter`
  - `func (r *CommandRouter) Register(name string, h CommandHandler)`
  - `func (r *CommandRouter) Dispatch(ctx context.Context, input string, cc CommandContext) (CommandOutcome, error)` — parses the slash name via `commands.ParseSlashCommand`; if a handler is registered, runs it; else returns `{Handled:false}` so the loop falls through to the model.

> CONFIRM before writing: the exact `tui.REPLScreen` mutation methods. Run `go doc ./internal/tui REPLScreen` — expected `ClearConversation()`, `SetMessages([]Message)`, `AppendMessage(Message)`. The agent map reported these exist. Confirm `commands.ParseSlashCommand` signature with `go doc ./internal/commands ParseSlashCommand` (expected `(SlashCommand, bool)` with `.CommandName`/`.Args`).

- [ ] **Step 1: Write the failing test**

Create `internal/repl/commands_test.go`:
```go
package repl

import (
	"context"
	"testing"

	"ccgo/internal/contracts"
)

func TestCommandRouterDispatchHandled(t *testing.T) {
	router := NewCommandRouter()
	var gotArgs string
	router.Register("clear", func(ctx context.Context, cc CommandContext) (CommandOutcome, error) {
		gotArgs = cc.Args
		return CommandOutcome{Handled: true, ReplaceHistory: true, NewHistory: nil, Status: "cleared"}, nil
	})

	out, err := router.Dispatch(context.Background(), "/clear all", CommandContext{Args: "", History: []contracts.Message{{}}})
	if err != nil {
		t.Fatalf("Dispatch err: %v", err)
	}
	if !out.Handled {
		t.Fatal("expected /clear to be handled")
	}
	if gotArgs != "all" {
		t.Fatalf("Args = %q want %q", gotArgs, "all")
	}
	if !out.ReplaceHistory || out.NewHistory != nil {
		t.Fatalf("expected history replaced with nil, got %+v", out)
	}
}

func TestCommandRouterUnregisteredFallsThrough(t *testing.T) {
	router := NewCommandRouter()
	out, err := router.Dispatch(context.Background(), "/unknownxyz", CommandContext{})
	if err != nil {
		t.Fatalf("Dispatch err: %v", err)
	}
	if out.Handled {
		t.Fatal("unregistered command must fall through (Handled=false)")
	}
}

func TestCommandRouterNonSlashFallsThrough(t *testing.T) {
	router := NewCommandRouter()
	router.Register("clear", func(ctx context.Context, cc CommandContext) (CommandOutcome, error) {
		return CommandOutcome{Handled: true}, nil
	})
	out, _ := router.Dispatch(context.Background(), "hello world", CommandContext{})
	if out.Handled {
		t.Fatal("plain prompt text must not be handled by the router")
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/repl/ -run TestCommandRouter -v`
Expected: FAIL — `undefined: NewCommandRouter`.

- [ ] **Step 3: Write minimal implementation**

Create `internal/repl/commands.go`:
```go
package repl

import (
	"context"
	"strings"

	"ccgo/internal/commands"
	"ccgo/internal/contracts"
	"ccgo/internal/tui"
)

// CommandContext is the live state a REPL command handler may read/mutate.
type CommandContext struct {
	Args    string
	Screen  *tui.REPLScreen
	History []contracts.Message
	CWD     string
}

// CommandOutcome reports what a handler did. Handled=false means the input was
// not a registered live-effect command and must fall through to the model.
type CommandOutcome struct {
	Handled        bool
	ReplaceHistory bool
	NewHistory     []contracts.Message
	Status         string
	SendToModel    bool
}

// CommandHandler runs a single live-effect slash command.
type CommandHandler func(ctx context.Context, cc CommandContext) (CommandOutcome, error)

// CommandRouter maps slash command names to live-effect handlers.
type CommandRouter struct {
	handlers map[string]CommandHandler
}

func NewCommandRouter() *CommandRouter {
	return &CommandRouter{handlers: map[string]CommandHandler{}}
}

func (r *CommandRouter) Register(name string, h CommandHandler) {
	r.handlers[strings.TrimSpace(name)] = h
}

// Dispatch routes a raw input line. If it is a slash command with a registered
// handler, the handler runs with cc.Args set to the parsed arguments.
func (r *CommandRouter) Dispatch(ctx context.Context, input string, cc CommandContext) (CommandOutcome, error) {
	parsed, ok := commands.ParseSlashCommand(input)
	if !ok {
		return CommandOutcome{Handled: false}, nil
	}
	handler, found := r.handlers[parsed.CommandName]
	if !found {
		return CommandOutcome{Handled: false}, nil
	}
	cc.Args = parsed.Args
	return handler(ctx, cc)
}
```

- [ ] **Step 4: Run tests to verify they pass**

Run: `go test ./internal/repl/ -run TestCommandRouter -v`
Expected: PASS.

- [ ] **Step 5: Wire the router into the loop**

Add a `router *CommandRouter` field to `Loop` (in `loop.go`, alongside the other fields ~`:30-62`) and an `OnCommand func(input string) (CommandOutcome, bool)` injection seam set by `run.go`. In `handleKey`'s `ScreenEventPromptSubmitted` branch (`loop.go:210-216`), before calling `StartTurn`, consult the router:
```go
	case tui.ScreenEventPromptSubmitted:
		if l.StartTurn == nil || l.running || strings.TrimSpace(event.Value) == "" {
			return false
		}
		if l.onCommand != nil {
			if outcome, handled := l.onCommand(event.Value); handled {
				l.applyCommandOutcome(outcome)
				return false
			}
		}
		l.running = true
		l.StartTurn(event.Value)
	}
```
Add `onCommand func(input string) (CommandOutcome, bool)` field and:
```go
// applyCommandOutcome applies a handled live-effect command's result to the
// screen and history without sending anything to the model.
func (l *Loop) applyCommandOutcome(outcome CommandOutcome) {
	if outcome.ReplaceHistory {
		l.history = outcome.NewHistory
		l.screen.SetMessages(messagesToScreen(l.history))
	}
	if outcome.Status != "" {
		l.screen.AppendMessage(tui.Message{Role: tui.RoleSystem, Text: outcome.Status})
	}
}
```
`messagesToScreen` converts `[]contracts.Message` → `[]tui.Message` (reuse existing mapping; if none exists, write a small mapper in `render.go` mirroring `messageFromEvent`). Add a loop test driving a `/clear`-style command end-to-end with a `FakeTerminal` proving the screen is cleared and nothing is sent to the model:
```go
func TestLoopRouterClearsHistory(t *testing.T) {
	ft := NewFakeTerminal("/clear\r\x04\x04", 80, 24)
	l := NewLoop(ft, nil)
	l.history = []contracts.Message{{Type: contracts.MessageUser}}
	sent := 0
	l.StartTurn = func(string) { sent++ }
	l.onCommand = func(input string) (CommandOutcome, bool) {
		return CommandOutcome{Handled: true, ReplaceHistory: true, NewHistory: nil, Status: "cleared"}, true
	}
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	if err := l.Run(ctx); err != nil {
		t.Fatalf("Run err: %v", err)
	}
	if sent != 0 {
		t.Fatalf("StartTurn called %d times; live command must not hit the model", sent)
	}
	if len(l.history) != 0 {
		t.Fatalf("history not cleared: %d msgs", len(l.history))
	}
}
```
Confirm the `tui.RoleSystem` constant exists: `go doc ./internal/tui Role` (verified — `RoleSystem` present).

- [ ] **Step 6: Run all repl tests + commit**

Run: `go test ./internal/repl/ -v`
Expected: PASS.
```bash
git add internal/repl/commands.go internal/repl/commands_test.go internal/repl/loop.go internal/repl/loop_test.go internal/repl/render.go
git commit -m "feat(repl): add CommandRouter seam for live-effect slash commands"
```

---

## Task 2: `/resume` — real interactive session resume

**Behavior (CC `resume.tsx:194-243`):** no-arg → show a picker of same-repo sessions; selecting one loads its full transcript into the live conversation via `context.resume(sessionId, log)`. With an arg → resolve by id/title/search and resume directly. ccgo's read side exists (`session.ListProjectSessions`, `session.BuildResumeConversation`, `formatResumeSummary`); the **live load** is missing.

**Files:**
- Create: `internal/repl/commands_resume.go`
- Modify: `internal/repl/run.go` (register the resume handler on the router)
- Test: `internal/repl/commands_resume_test.go`

**Interfaces:**
- Produces:
  - `func resumeHandler(cwd string, loadConversation func(path string, id contracts.ID) ([]contracts.Message, error)) CommandHandler`
  - For no-arg: render a numbered session list to the screen and set the loop into a "pick-a-number" sub-mode — to avoid a new modal state machine in this task, accept the session **id or index** as the arg: `/resume <id|N|search term>`. (The picker dialog UI is Phase 2's job; here we deliver the functional resume with arg-or-numbered-list.)
  - On a resolvable target → `CommandOutcome{Handled:true, ReplaceHistory:true, NewHistory:<loaded>, Status:"Resumed <id> (<n> messages)"}`.

> CONFIRM: `session.SessionInfo` fields (`go doc ./internal/session SessionInfo` → `ID, Path, Title, ProjectPath, GitBranch, Modified, Size`) and `session.ListProjectSessions(root) ([]SessionInfo, error)`, `session.BuildResumeConversation(path, leaf contracts.ID) (ResumeConversation, error)` with `.Found`/`.Messages` (verified). `session.SearchProjectSessions(cwd, query, limit)` exists (`run.go:7124`).

- [ ] **Step 1: Write the failing test**

Create `internal/repl/commands_resume_test.go`. Inject a fake loader and a fake session-list so the test needs no disk:
```go
package repl

import (
	"context"
	"testing"

	"ccgo/internal/contracts"
)

func TestResumeHandlerLoadsByID(t *testing.T) {
	listed := []resumeEntry{
		{ID: "sess-a", Path: "/x/sess-a.jsonl", Title: "first"},
		{ID: "sess-b", Path: "/x/sess-b.jsonl", Title: "second"},
	}
	loaded := []contracts.Message{
		{Type: contracts.MessageUser, Content: []contracts.ContentBlock{contracts.NewTextBlock("hi")}},
	}
	h := resumeHandlerWith(
		func() ([]resumeEntry, error) { return listed, nil },
		func(path string, id contracts.ID) ([]contracts.Message, error) {
			if id != "sess-b" {
				t.Fatalf("loaded wrong session %q", id)
			}
			return loaded, nil
		},
	)
	out, err := h(context.Background(), CommandContext{Args: "sess-b"})
	if err != nil {
		t.Fatalf("handler err: %v", err)
	}
	if !out.Handled || !out.ReplaceHistory || len(out.NewHistory) != 1 {
		t.Fatalf("unexpected outcome %+v", out)
	}
}

func TestResumeHandlerNoArgListsSessions(t *testing.T) {
	listed := []resumeEntry{{ID: "sess-a", Path: "/x/sess-a.jsonl", Title: "first"}}
	h := resumeHandlerWith(
		func() ([]resumeEntry, error) { return listed, nil },
		func(string, contracts.ID) ([]contracts.Message, error) { t.Fatal("should not load"); return nil, nil },
	)
	out, err := h(context.Background(), CommandContext{Args: ""})
	if err != nil {
		t.Fatalf("handler err: %v", err)
	}
	if out.ReplaceHistory {
		t.Fatal("no-arg resume must not replace history; it lists")
	}
	if out.Status == "" {
		t.Fatal("expected a listing in Status")
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/repl/ -run TestResumeHandler -v`
Expected: FAIL — `undefined: resumeEntry` / `resumeHandlerWith`.

- [ ] **Step 3: Write minimal implementation**

Create `internal/repl/commands_resume.go`:
```go
package repl

import (
	"context"
	"fmt"
	"strconv"
	"strings"

	"ccgo/internal/contracts"
	"ccgo/internal/session"
)

// resumeEntry is the minimal session-listing row the resume handler needs.
type resumeEntry struct {
	ID    contracts.ID
	Path  string
	Title string
}

type resumeLister func() ([]resumeEntry, error)
type resumeLoader func(path string, id contracts.ID) ([]contracts.Message, error)

// resumeHandlerWith is the dependency-injected core (testable without disk).
func resumeHandlerWith(list resumeLister, load resumeLoader) CommandHandler {
	return func(ctx context.Context, cc CommandContext) (CommandOutcome, error) {
		entries, err := list()
		if err != nil {
			return CommandOutcome{}, fmt.Errorf("list sessions: %w", err)
		}
		arg := strings.TrimSpace(cc.Args)
		if arg == "" {
			return CommandOutcome{Handled: true, Status: formatResumeList(entries)}, nil
		}
		entry, ok := resolveResumeTarget(entries, arg)
		if !ok {
			return CommandOutcome{Handled: true, Status: fmt.Sprintf("No session matched %q.", arg)}, nil
		}
		msgs, err := load(entry.Path, entry.ID)
		if err != nil {
			return CommandOutcome{}, fmt.Errorf("load session %s: %w", entry.ID, err)
		}
		return CommandOutcome{
			Handled:        true,
			ReplaceHistory: true,
			NewHistory:     msgs,
			Status:         fmt.Sprintf("Resumed %s (%d messages)", entry.ID, len(msgs)),
		}, nil
	}
}

// resumeHandler builds the production handler over the real session store.
func resumeHandler(cwd string) CommandHandler {
	return resumeHandlerWith(
		func() ([]resumeEntry, error) {
			infos, err := session.ListProjectSessions(cwd)
			if err != nil {
				return nil, err
			}
			out := make([]resumeEntry, 0, len(infos))
			for _, info := range infos {
				out = append(out, resumeEntry{ID: info.ID, Path: info.Path, Title: info.Title})
			}
			return out, nil
		},
		func(path string, id contracts.ID) ([]contracts.Message, error) {
			resumed, err := session.BuildResumeConversation(path, "")
			if err != nil {
				return nil, err
			}
			if !resumed.Found {
				return nil, fmt.Errorf("session %s has no resumable messages", id)
			}
			return resumed.Messages, nil
		},
	)
}

func resolveResumeTarget(entries []resumeEntry, arg string) (resumeEntry, bool) {
	// Exact id.
	for _, e := range entries {
		if string(e.ID) == arg {
			return e, true
		}
	}
	// 1-based index.
	if n, err := strconv.Atoi(arg); err == nil && n >= 1 && n <= len(entries) {
		return entries[n-1], true
	}
	// Title / id substring.
	lower := strings.ToLower(arg)
	for _, e := range entries {
		if strings.Contains(strings.ToLower(e.Title), lower) || strings.Contains(strings.ToLower(string(e.ID)), lower) {
			return e, true
		}
	}
	return resumeEntry{}, false
}

func formatResumeList(entries []resumeEntry) string {
	if len(entries) == 0 {
		return "No previous sessions found."
	}
	lines := []string{"Resumable sessions (use /resume <number|id|search>):"}
	for i, e := range entries {
		title := e.Title
		if strings.TrimSpace(title) == "" {
			title = string(e.ID)
		}
		lines = append(lines, fmt.Sprintf("  %d. %s  (%s)", i+1, title, e.ID))
	}
	return strings.Join(lines, "\n")
}
```

- [ ] **Step 4: Register on the router**

In `run.go`'s `newTurnLoop`, build a `CommandRouter`, register `resumeHandler(base.WorkingDirectory)` under `"resume"` (and alias `"continue"` if desired — confirm `base.WorkingDirectory` field with `go doc ./internal/conversation Runner | grep WorkingDirectory`), set `loop.router` + `loop.onCommand` to a closure that calls `router.Dispatch` with a `CommandContext{Screen:&loop.screen, History:loop.history, CWD:base.WorkingDirectory}`.

- [ ] **Step 5: Run tests + commit**

Run: `go test ./internal/repl/ -v`
Expected: PASS.
```bash
git add internal/repl/commands_resume.go internal/repl/commands_resume_test.go internal/repl/run.go
git commit -m "feat(repl): /resume actually loads a prior session into the live REPL"
```

---

## Task 3: `.claude/agents/*.md` file model (parse/format/list/save/delete)

**Behavior (CC `agentFileUtils.ts`):** agents are markdown files with YAML frontmatter (`name, description, tools, model, effort, color, memory`) + a body (system prompt). User scope → `~/.claude/agents/`, project/local → `<cwd>/.claude/agents/`. `saveAgentToFile` uses `wx` (no overwrite), `deleteAgentFromFile` unlinks. ccgo has **no** non-plugin agent loader today (verified: agents flow only through `internal/plugins`). This task builds the file model; Task 4 wires `/agents` + `claude agents` onto it.

**Files:**
- Create: `internal/agentfile/agentfile.go`
- Create: `internal/agentfile/agentfile_test.go`

**Interfaces:**
- Produces:
  - `type AgentFile struct { Name string; Description string; Tools []string; Model string; Effort string; Color string; Memory string; Prompt string; Path string }`
  - `func Parse(name string, content []byte) (AgentFile, error)`
  - `func Format(a AgentFile) string`
  - `func ProjectDir(cwd string) string` / `func UserDir() (string, error)`
  - `func List(dirs ...string) ([]AgentFile, error)`
  - `func Save(dir string, a AgentFile) error` (no overwrite; clear error if exists)
  - `func Delete(dir, name string) error`

> CONFIRM: reuse the existing frontmatter parser if one exists rather than writing a new YAML parser. Run `grep -rn "frontmatter\|ParseFrontmatter\|yaml\|---" internal/skills/*.go internal/plugins/*.go | grep -iv test | head`. ccgo skills already parse `name:`/`description:` frontmatter (`internal/skills`); reuse that helper if exported, else write a **minimal line-based** parser (the agent format is `key: value` scalars + string lists, not arbitrary YAML — do NOT add a yaml dependency). Confirm no yaml dep: `grep -rn "gopkg.in/yaml\|yaml.v3" go.mod` (expected: none — keep it that way).

- [ ] **Step 1: Write the failing test**

Create `internal/agentfile/agentfile_test.go`:
```go
package agentfile

import (
	"os"
	"path/filepath"
	"testing"
)

const sample = `---
name: reviewer
description: Reviews Go code for idioms
tools: Read, Grep, Bash
model: sonnet
color: blue
---
You are a meticulous Go reviewer. Focus on idiomatic patterns.
`

func TestParseRoundTrip(t *testing.T) {
	a, err := Parse("reviewer", []byte(sample))
	if err != nil {
		t.Fatalf("Parse err: %v", err)
	}
	if a.Name != "reviewer" || a.Description != "Reviews Go code for idioms" {
		t.Fatalf("bad metadata: %+v", a)
	}
	if len(a.Tools) != 3 || a.Tools[0] != "Read" || a.Tools[2] != "Bash" {
		t.Fatalf("bad tools: %v", a.Tools)
	}
	if a.Model != "sonnet" || a.Color != "blue" {
		t.Fatalf("bad model/color: %+v", a)
	}
	if a.Prompt == "" || a.Prompt[:7] != "You are" {
		t.Fatalf("bad prompt: %q", a.Prompt)
	}
	// Format must reproduce a parseable file.
	again, err := Parse("reviewer", []byte(Format(a)))
	if err != nil {
		t.Fatalf("re-parse err: %v", err)
	}
	if again.Description != a.Description || len(again.Tools) != len(a.Tools) {
		t.Fatalf("round-trip mismatch: %+v vs %+v", again, a)
	}
}

func TestSaveListDelete(t *testing.T) {
	dir := t.TempDir()
	a := AgentFile{Name: "helper", Description: "d", Prompt: "p"}
	if err := Save(dir, a); err != nil {
		t.Fatalf("Save err: %v", err)
	}
	if err := Save(dir, a); err == nil {
		t.Fatal("second Save must fail (no overwrite)")
	}
	if _, statErr := os.Stat(filepath.Join(dir, "helper.md")); statErr != nil {
		t.Fatalf("file not written: %v", statErr)
	}
	list, err := List(dir)
	if err != nil || len(list) != 1 || list[0].Name != "helper" {
		t.Fatalf("List = %v, %v", list, err)
	}
	if err := Delete(dir, "helper"); err != nil {
		t.Fatalf("Delete err: %v", err)
	}
	if _, statErr := os.Stat(filepath.Join(dir, "helper.md")); !os.IsNotExist(statErr) {
		t.Fatal("file not deleted")
	}
}

func TestParseRejectsEmptyName(t *testing.T) {
	if _, err := Parse("", []byte("---\n---\nbody")); err == nil {
		t.Fatal("empty name must error")
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/agentfile/ -v`
Expected: FAIL — package does not exist / undefined symbols.

- [ ] **Step 3: Write minimal implementation**

Create `internal/agentfile/agentfile.go`. Implement: a small frontmatter splitter (lines between leading `---` and the next `---`), `key: value` scalar parsing, comma-split for `tools`, validation (non-empty name; name matches `[a-zA-Z0-9_-]+` to make a safe filename), `Format` re-emitting frontmatter (omit empty fields, omit `tools` when empty), `Save` with `os.OpenFile(..., O_WRONLY|O_CREATE|O_EXCL, 0o644)` for no-overwrite semantics, `List` globbing `*.md`, `Delete` via `os.Remove`. `ProjectDir(cwd)` = `filepath.Join(cwd, ".claude", "agents")`; `UserDir()` uses `os.UserHomeDir()` → `.claude/agents`. Validate the name in `Save`/`Delete` to prevent path traversal (reject `/`, `\`, `..`). Keep it ~200–280 lines, no third-party deps.

- [ ] **Step 4: Run tests + commit**

Run: `go test ./internal/agentfile/ -v`
Expected: PASS.
```bash
git add internal/agentfile/
git commit -m "feat(agentfile): parse/format/list/save/delete .claude/agents markdown"
```

---

## Task 4: `/agents` slash + `claude agents` CLI on the file model

**Behavior:** `/agents` with no arg lists agents grouped by scope (user/project); `/agents create <name>` / `/agents delete <name>` mutate files; `/agents show <name>` prints detail. `claude agents` (CC `cli/handlers/agents.ts:32`) is **list-only**.

**Files:**
- Create: `cmd/claude/cli_agents.go`
- Modify: `internal/repl/commands_settings.go` (or a new `commands_agents.go`) for the `/agents` handler; register on the router in `run.go`
- Modify: `cmd/claude/main.go` (dispatch `agents` subcommand near `:197`)
- Test: `internal/repl/commands_agents_test.go`, `cmd/claude/cli_agents_test.go`

- [ ] **Step 1: Write failing tests**

`internal/repl/commands_agents_test.go` — drive the `/agents` handler against a temp project dir:
```go
package repl

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestAgentsHandlerCreateAndList(t *testing.T) {
	cwd := t.TempDir()
	h := agentsHandler(cwd)

	out, err := h(context.Background(), CommandContext{Args: "create reviewer", CWD: cwd})
	if err != nil || !out.Handled {
		t.Fatalf("create: %v %+v", err, out)
	}
	if _, statErr := os.Stat(filepath.Join(cwd, ".claude", "agents", "reviewer.md")); statErr != nil {
		t.Fatalf("agent file not created: %v", statErr)
	}
	out, err = h(context.Background(), CommandContext{Args: "", CWD: cwd})
	if err != nil {
		t.Fatalf("list err: %v", err)
	}
	if !strings.Contains(out.Status, "reviewer") {
		t.Fatalf("list missing reviewer: %q", out.Status)
	}
}
```
`cmd/claude/cli_agents_test.go` — exercise `runAgentsCLI` writing to a buffer (list-only, no tty):
```go
package main

import (
	"bytes"
	"strings"
	"testing"
)

func TestRunAgentsCLIListsEmpty(t *testing.T) {
	var out, errOut bytes.Buffer
	code := runAgentsCLI(t.TempDir(), nil, &out, &errOut)
	if code != 0 {
		t.Fatalf("exit code %d, stderr=%s", code, errOut.String())
	}
	if !strings.Contains(out.String(), "agents") && out.Len() == 0 {
		t.Fatalf("expected some listing output, got %q", out.String())
	}
}
```

- [ ] **Step 2: Run to verify failure**

Run: `go test ./internal/repl/ -run TestAgentsHandler -v && go test ./cmd/claude/ -run TestRunAgentsCLI -v`
Expected: FAIL — undefined `agentsHandler` / `runAgentsCLI`.

- [ ] **Step 3: Implement**

`agentsHandler(cwd)` parses the first arg word as a subcommand (`create`/`delete`/`show`/`list`/empty), defaulting to list. `create <name>` builds an `agentfile.AgentFile{Name:name, Description:"", Prompt:"# "+name+"\n"}` and `agentfile.Save(agentfile.ProjectDir(cwd), a)`; `delete <name>` → `agentfile.Delete`; list → enumerate `agentfile.List(agentfile.ProjectDir(cwd))` + `agentfile.UserDir()`, returning a grouped `Status`. All paths return `CommandOutcome{Handled:true, Status:...}` (no model send). Add `runAgentsCLI(cwd string, args []string, stdout, stderr io.Writer) int` that lists project+user agents and prints them; in `main.go`, before the print/interactive branch, add:
```go
	if !*printMode && len(flags.Args()) > 0 && strings.EqualFold(flags.Args()[0], "agents") {
		return runAgentsCLI(state.CWD(), flags.Args()[1:], stdout, stderr)
	}
```
(Mirror the existing `plugin` dispatch at `main.go:197`. Confirm `state.CWD()` exists: `grep -n "func (s \*State) CWD" internal/bootstrap/*.go`.) Register `agentsHandler` on the router in `run.go`.

- [ ] **Step 4: Run tests + commit**

Run: `go test ./internal/repl/ ./cmd/claude/ -v`
```bash
git add internal/repl/commands_agents.go internal/repl/commands_agents_test.go cmd/claude/cli_agents.go cmd/claude/cli_agents_test.go cmd/claude/main.go internal/repl/run.go
git commit -m "feat(commands): /agents editor + claude agents CLI over .claude/agents files"
```

---

## Task 5: settings schema for theme + vim, and the settings document writer helper

**Why:** `/theme` and `/vim` need persisted settings keys that do not exist (`Theme`, `EditorMode`/`VimMode`); `/permissions` and `/effort` need a reusable round-trip writer. `EffortLevel` already exists (`contracts/settings.go:55`).

**Files:**
- Modify: `internal/contracts/settings.go` (+ clone in `internal/config/settings.go` + JSON allowlist `internal/config/settings_json.go`)
- Create: `internal/config/settings_mutate.go` — `SetUserSettingsValue(key string, value any) error` (read doc → set key → write doc)
- Test: `internal/config/settings_mutate_test.go`

> CONFIRM the exact clone list and JSON allowlist before editing: `grep -n "EffortLevel\|OutputStyle" internal/config/settings.go internal/config/settings_json.go`. The agent reported `EffortLevel` is cloned at `config/settings.go:187-188` and allow-listed at `settings_json.go:91`. Mirror that for the new `Theme`/`EditorMode` fields. Confirm `WriteUserSettingsDocument`/`ReadUserSettingsDocument` signatures: `go doc ./internal/config WriteUserSettingsDocument`.

- [ ] **Step 1: Write the failing test**

Create `internal/config/settings_mutate_test.go`:
```go
package config

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
)

func TestSetSettingsValueInDocument(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "settings.json")
	if err := os.WriteFile(path, []byte(`{"model":"sonnet"}`), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := SetSettingsValue(path, "theme", "dark"); err != nil {
		t.Fatalf("SetSettingsValue err: %v", err)
	}
	raw, _ := os.ReadFile(path)
	var got map[string]any
	if err := json.Unmarshal(raw, &got); err != nil {
		t.Fatal(err)
	}
	if got["theme"] != "dark" || got["model"] != "sonnet" {
		t.Fatalf("merged doc = %v; want theme=dark, model preserved", got)
	}
}

func TestSetSettingsValueCreatesFile(t *testing.T) {
	path := filepath.Join(t.TempDir(), "nested", "settings.json")
	if err := SetSettingsValue(path, "editorMode", "vim"); err != nil {
		t.Fatalf("err: %v", err)
	}
	if _, err := os.Stat(path); err != nil {
		t.Fatalf("file not created: %v", err)
	}
}
```

- [ ] **Step 2: Run to verify failure**

Run: `go test ./internal/config/ -run TestSetSettingsValue -v`
Expected: FAIL — `undefined: SetSettingsValue`.

- [ ] **Step 3: Implement**

Create `internal/config/settings_mutate.go`:
```go
package config

import (
	"fmt"
	"strings"
)

// SetSettingsValue read-modify-writes a single top-level key in the settings
// document at path, preserving all other keys. It creates the file (and parent
// dir) if missing. value of nil deletes the key.
func SetSettingsValue(path string, key string, value any) error {
	key = strings.TrimSpace(key)
	if key == "" {
		return fmt.Errorf("settings key must be non-empty")
	}
	doc, err := ReadSettingsDocument(path)
	if err != nil {
		return fmt.Errorf("read settings %s: %w", path, err)
	}
	if doc == nil {
		doc = map[string]any{}
	}
	if value == nil {
		delete(doc, key)
	} else {
		doc[key] = value
	}
	if err := WriteSettingsDocument(path, doc); err != nil {
		return fmt.Errorf("write settings %s: %w", path, err)
	}
	return nil
}
```
> CONFIRM `ReadSettingsDocument` returns `(map[string]any, nil)` for a missing file rather than an error; check its body. If it errors on ENOENT, treat `os.IsNotExist` as empty doc here. Also confirm `WriteSettingsDocument` creates parent dirs; if not, add `os.MkdirAll(filepath.Dir(path), 0o755)`.

Then add `Theme string json:"theme,omitempty"` and `EditorMode string json:"editorMode,omitempty"` to `contracts.Settings` (with a brief test in `internal/contracts` if that package has settings tests — `grep -n "func Test" internal/contracts/settings_test.go`). Add them to the config clone + JSON allowlist mirroring `EffortLevel`. Add a contracts/config test asserting a JSON round-trip preserves `theme`/`editorMode`.

- [ ] **Step 4: Run tests + commit**

Run: `go test ./internal/config/ ./internal/contracts/ -v`
```bash
git add internal/config/settings_mutate.go internal/config/settings_mutate_test.go internal/contracts/settings.go internal/config/settings.go internal/config/settings_json.go
git commit -m "feat(config): settings document key writer + theme/editorMode fields"
```

---

## Task 6: `/theme`, `/effort`, `/vim` live settings commands

**Behavior:** `/theme <name>` writes `theme`; `/effort <low|medium|high|max|auto>` writes `effortLevel` (auto clears it; CC `effort.tsx:19,76`); `/vim` toggles `editorMode` between `vim`/`normal` (CC `vim.ts:8-19`) and flips `tui.REPLScreen.SetVimEnabled` live.

**Files:**
- Create: `internal/repl/commands_settings.go` (if not already created in Task 4; add the three handlers)
- Modify: `internal/repl/run.go` (register handlers)
- Modify: `internal/commands/registry.go` (add builtin defs for `theme`, `effort`, `vim`)
- Test: `internal/repl/commands_settings_test.go`

> CONFIRM the valid effort values from CC: `EffortLevel` ∈ {low, medium, high, max, auto} (`utils/effort.ts:14`). Confirm `tui.REPLScreen.SetVimEnabled(bool)` exists (verified). Pass a `settingsPath` + a `setValue func(key string, v any) error` into the handlers (DI) so tests don't touch the real `~/.claude/settings.json`.

- [ ] **Step 1: Write the failing test**

Create `internal/repl/commands_settings_test.go`:
```go
package repl

import (
	"context"
	"testing"
)

func TestEffortHandlerValidatesAndWrites(t *testing.T) {
	var key string
	var val any
	set := func(k string, v any) error { key, val = k, v; return nil }
	h := effortHandlerWith(set)

	if out, err := h(context.Background(), CommandContext{Args: "high"}); err != nil || !out.Handled {
		t.Fatalf("high: %v %+v", err, out)
	}
	if key != "effortLevel" || val != "high" {
		t.Fatalf("wrote %q=%v want effortLevel=high", key, val)
	}
	// auto clears (nil value).
	if _, err := h(context.Background(), CommandContext{Args: "auto"}); err != nil {
		t.Fatalf("auto: %v", err)
	}
	if val != nil {
		t.Fatalf("auto must clear effortLevel, got %v", val)
	}
	// invalid value is rejected without writing.
	key = ""
	if out, _ := h(context.Background(), CommandContext{Args: "turbo"}); !out.Handled || key != "" {
		t.Fatalf("invalid effort should report but not write; key=%q out=%+v", key, out)
	}
}

func TestThemeHandlerWrites(t *testing.T) {
	var key string
	set := func(k string, v any) error { key = k; return nil }
	h := themeHandlerWith(set)
	if out, err := h(context.Background(), CommandContext{Args: "dark"}); err != nil || !out.Handled {
		t.Fatalf("theme: %v %+v", err, out)
	}
	if key != "theme" {
		t.Fatalf("wrote %q want theme", key)
	}
}
```

- [ ] **Step 2: Run to verify failure**

Run: `go test ./internal/repl/ -run 'TestEffortHandler|TestThemeHandler' -v`
Expected: FAIL — undefined handlers.

- [ ] **Step 3: Implement**

In `internal/repl/commands_settings.go` add `effortHandlerWith(set func(string, any) error)`, `themeHandlerWith(...)`, and a `vimHandler` that toggles `cc.Screen.SetVimEnabled` and persists `editorMode`. Validate effort against the fixed set; `auto` → `set("effortLevel", nil)`. Empty arg → report current/usage in `Status` without writing. Production constructors wrap `config.SetSettingsValue(config.UserSettingsPath(), ...)` — confirm `config.UserSettingsPath()` exists: `go doc ./internal/config UserSettingsPath`. Add the three builtins to `registry.go` `BuiltinCommands()` (`CommandLocalJSX` for theme/vim, with `ArgumentHint`). Register handlers in `run.go`.

- [ ] **Step 4: Run tests + commit**

Run: `go test ./internal/repl/ ./internal/commands/ -v`
```bash
git add internal/repl/commands_settings.go internal/repl/commands_settings_test.go internal/repl/run.go internal/commands/registry.go
git commit -m "feat(commands): /theme /effort /vim live settings commands"
```

---

## Task 7: `/permissions` editor (list + add/remove rules, persisted)

**Behavior (CC `permissions.tsx` + `PermissionUpdate.ts`):** show allow/ask/deny rules; add/remove `Tool(arg)` rules persisted to `settings.json` `permissions.{allow,deny,ask}`. Editable scopes: user/project/local. ccgo has `permissions.Engine.ApplyUpdate` (returns new engine) + `PermissionsSetting` (`contracts/settings.go:138`) but no writer caller.

**Files:**
- Create: `internal/config/permissions_write.go` — `AddPermissionRule(path, behavior, rule string) error`, `RemovePermissionRule(path, behavior, rule string) error` (operate on the doc's `permissions` sub-object)
- Create: `internal/repl/commands_permissions.go` — `/permissions` handler (list/allow/deny/ask/remove subcommands)
- Modify: `internal/repl/run.go`, `internal/commands/registry.go`
- Test: `internal/config/permissions_write_test.go`, `internal/repl/commands_permissions_test.go`

> CONFIRM `PermissionsSetting` field JSON tags (`allow`/`deny`/`ask` — verified `contracts/settings.go:138-146`). The doc-level key is `"permissions"` (confirm with `grep -n "\"permissions\"\|json:\"permissions" internal/contracts/settings.go`). Confirm valid behaviors map to those three arrays.

- [ ] **Step 1: Write failing tests**

`internal/config/permissions_write_test.go`:
```go
package config

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
)

func readPerms(t *testing.T, path string) map[string]any {
	t.Helper()
	raw, _ := os.ReadFile(path)
	var doc map[string]any
	_ = json.Unmarshal(raw, &doc)
	perms, _ := doc["permissions"].(map[string]any)
	return perms
}

func TestAddRemovePermissionRule(t *testing.T) {
	path := filepath.Join(t.TempDir(), "settings.json")
	if err := AddPermissionRule(path, "allow", "Bash(ls:*)"); err != nil {
		t.Fatalf("add err: %v", err)
	}
	if err := AddPermissionRule(path, "allow", "Bash(ls:*)"); err != nil { // idempotent
		t.Fatalf("re-add err: %v", err)
	}
	perms := readPerms(t, path)
	allow, _ := perms["allow"].([]any)
	if len(allow) != 1 || allow[0] != "Bash(ls:*)" {
		t.Fatalf("allow = %v want one Bash(ls:*)", allow)
	}
	if err := RemovePermissionRule(path, "allow", "Bash(ls:*)"); err != nil {
		t.Fatalf("remove err: %v", err)
	}
	perms = readPerms(t, path)
	if allow, _ := perms["allow"].([]any); len(allow) != 0 {
		t.Fatalf("rule not removed: %v", allow)
	}
}

func TestAddPermissionRuleRejectsBadBehavior(t *testing.T) {
	if err := AddPermissionRule(filepath.Join(t.TempDir(), "s.json"), "maybe", "Bash(x)"); err == nil {
		t.Fatal("invalid behavior must error")
	}
}
```
`internal/repl/commands_permissions_test.go` drives the handler with an injected mutator and asserts list/allow/deny outcomes.

- [ ] **Step 2: Run to verify failure**

Run: `go test ./internal/config/ -run TestAddRemovePermissionRule -v`
Expected: FAIL — undefined.

- [ ] **Step 3: Implement**

`permissions_write.go`: read doc, ensure `permissions` map, validate behavior ∈ {allow, deny, ask}, append rule if not present (idempotent), or remove it; write back via `WriteSettingsDocument`. Preserve other permission keys (`defaultMode`, `additionalDirectories`). `/permissions` handler: no-arg → list current rules grouped by behavior (read via `config.LoadSettingsFile` or the doc); `allow|deny|ask <rule>` → add; `remove <rule>` → remove from all behaviors; return `CommandOutcome{Handled:true, Status:...}`. Register on router + add builtin def (`CommandLocalJSX`, alias `allowed-tools` like CC).

- [ ] **Step 4: Run tests + commit**

Run: `go test ./internal/config/ ./internal/repl/ -v`
```bash
git add internal/config/permissions_write.go internal/config/permissions_write_test.go internal/repl/commands_permissions.go internal/repl/commands_permissions_test.go internal/repl/run.go internal/commands/registry.go
git commit -m "feat(commands): /permissions editor persists allow/deny/ask rules"
```

---

## Task 8: `/context` usage report (shared formatter)

**Behavior (CC `context.tsx` + `analyzeContext`):** report token usage of the conversation vs the model's context window, broken down. ccgo has `compact.EstimateTokens([]Message) int` (`internal/compact/estimate.go:10`), `compact.EffectiveContextWindow(WindowConfig)` (`threshold.go:33`), and `model.Model.ContextWindowTokens` (`model.go:25`). Build a deterministic text report from these.

**Files:**
- Create: `internal/contextreport/contextreport.go`
- Create: `internal/contextreport/contextreport_test.go`
- Modify: `internal/commands/slash.go` + `internal/conversation/run.go` (new `LocalCommandResultContext` + formatter, so it runs headless too) and register `/context` builtin in `registry.go`

> CONFIRM signatures: `go doc ./internal/compact EstimateTokens`, `go doc ./internal/compact WindowConfig`, `go doc ./internal/compact EffectiveContextWindow`, `go doc ./internal/model Model` (field `ContextWindowTokens`). Confirm how `conversation.Runner` exposes the active model/window for the formatter — `grep -n "func (r.*Runner) model(\|ContextWindow\|maybeEmitTokenWarning" internal/conversation/run.go`.

- [ ] **Step 1: Write the failing test**

Create `internal/contextreport/contextreport_test.go`:
```go
package contextreport

import (
	"strings"
	"testing"
)

func TestReportBreakdown(t *testing.T) {
	r := Report{
		ModelName:     "claude-sonnet",
		WindowTokens:  200000,
		PromptTokens:  50000,
		SystemTokens:  2000,
		ToolTokens:    1000,
	}
	out := Format(r)
	if !strings.Contains(out, "claude-sonnet") {
		t.Fatalf("missing model name: %q", out)
	}
	if !strings.Contains(out, "200000") && !strings.Contains(out, "200,000") {
		t.Fatalf("missing window size: %q", out)
	}
	// Used = prompt+system+tool = 53000; ~26.5%.
	if !strings.Contains(out, "53000") && !strings.Contains(out, "53,000") {
		t.Fatalf("missing used total: %q", out)
	}
	if !strings.Contains(out, "%") {
		t.Fatalf("expected a percentage: %q", out)
	}
}

func TestReportZeroWindowSafe(t *testing.T) {
	out := Format(Report{ModelName: "x", WindowTokens: 0, PromptTokens: 10})
	if out == "" || strings.Contains(out, "NaN") || strings.Contains(out, "+Inf") {
		t.Fatalf("zero window must not divide by zero: %q", out)
	}
}
```

- [ ] **Step 2: Run to verify failure**

Run: `go test ./internal/contextreport/ -v`
Expected: FAIL — package missing.

- [ ] **Step 3: Implement**

`Report` struct + `Format(Report) string` with safe percentage (guard `WindowTokens<=0`). Then in `conversation/run.go`, add `LocalCommandResultContext` handling that builds a `Report` from `compact.EstimateTokens(originalHistory)`, the runner's model window, and the system prompt size, and routes through `appendLocalTextResult` (matching the `Cost`/`MCP` pattern at `:116-144`). Add the `commands.LocalCommandResultContext` constant + the `ExecuteBuiltinLocalCommand` case + a `context` builtin (`CommandLocal`, `SupportsNonInteractive:true`). No REPL router entry needed (it works headless and interactive via the existing local-result path).

- [ ] **Step 4: Run tests + commit**

Run: `go test ./internal/contextreport/ ./internal/conversation/ ./internal/commands/ -v`
```bash
git add internal/contextreport/ internal/conversation/run.go internal/commands/slash.go internal/commands/registry.go
git commit -m "feat(commands): /context token-usage report (headless + interactive)"
```

---

## Task 9: `/export` conversation export (file or transcript text)

**Behavior (CC `export.tsx:53-67`):** render the conversation to plain text; with a filename arg write `<cwd>/<name>.txt`; no arg → offer file/clipboard (we do file by default; clipboard is a Phase-2/native concern). Live-effect command (writes a file).

**Files:**
- Create: `internal/repl/commands_export.go`
- Modify: `internal/repl/run.go`, `internal/commands/registry.go`
- Test: `internal/repl/commands_export_test.go`

> CONFIRM a transcript renderer exists to reuse. Run `grep -rn "func.*PlainText\|RenderTranscript\|TextContent\|func.*Transcript.*string" internal/messages/*.go internal/session/*.go | grep -iv test`. Reuse `messages.TextContent(msg)` per message (verified used in `run.go`); build a simple `User: ... / Assistant: ...` text export. Do NOT add a clipboard dep.

- [ ] **Step 1: Write the failing test**

Create `internal/repl/commands_export_test.go`:
```go
package repl

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"ccgo/internal/contracts"
)

func TestExportHandlerWritesFile(t *testing.T) {
	cwd := t.TempDir()
	history := []contracts.Message{
		{Type: contracts.MessageUser, Content: []contracts.ContentBlock{contracts.NewTextBlock("hello")}},
		{Type: contracts.MessageAssistant, Content: []contracts.ContentBlock{contracts.NewTextBlock("hi there")}},
	}
	h := exportHandler(cwd)
	out, err := h(context.Background(), CommandContext{Args: "convo", CWD: cwd, History: history})
	if err != nil || !out.Handled {
		t.Fatalf("export: %v %+v", err, out)
	}
	path := filepath.Join(cwd, "convo.txt")
	raw, statErr := os.ReadFile(path)
	if statErr != nil {
		t.Fatalf("export file missing: %v", statErr)
	}
	body := string(raw)
	if !strings.Contains(body, "hello") || !strings.Contains(body, "hi there") {
		t.Fatalf("export body incomplete: %q", body)
	}
}

func TestExportHandlerDefaultFilename(t *testing.T) {
	cwd := t.TempDir()
	h := exportHandler(cwd)
	out, err := h(context.Background(), CommandContext{Args: "", CWD: cwd, History: nil})
	if err != nil || !out.Handled {
		t.Fatalf("export default: %v %+v", err, out)
	}
	if !strings.Contains(out.Status, ".txt") {
		t.Fatalf("expected a filename in status: %q", out.Status)
	}
}
```

- [ ] **Step 2: Run to verify failure**

Run: `go test ./internal/repl/ -run TestExportHandler -v`
Expected: FAIL — undefined `exportHandler`.

- [ ] **Step 3: Implement**

`exportHandler(cwd)` reads `cc.History`, renders each message as `Role: text` lines via `messages.TextContent`, derives a filename (arg sanitized → force `.txt`; empty → timestamp-based `claude-export-<unix>.txt`), validates against path traversal, writes with `0o644`, returns `Status: "Exported N messages to <path>"`. Register on router + add `export` builtin (`CommandLocalJSX`, `ArgumentHint:"[filename]"`).

- [ ] **Step 4: Run tests + commit**

Run: `go test ./internal/repl/ -v`
```bash
git add internal/repl/commands_export.go internal/repl/commands_export_test.go internal/repl/run.go internal/commands/registry.go
git commit -m "feat(commands): /export writes the conversation transcript to a file"
```

---

## Task 10: `/init` and `/review` prompt commands

**Behavior:** both are **prompt** commands in CC (`init.ts:6`, `review.ts:14`) — they expand to a fixed instruction sent to the model. `/init` → analyze the codebase and write `CLAUDE.md`. `/review` → `gh pr` workflow review. ccgo's registry already supports `CommandPrompt` with a `PromptTemplate` (`internal/commands/prompt.go`). This task adds the two builtin prompt definitions + their template content, no new dispatch.

**Files:**
- Modify: `internal/commands/registry.go` (builtin defs) + a builtin prompt source (where bundled prompt templates are registered — confirm location)
- Test: `internal/commands/registry_test.go` (or a focused new test)

> CONFIRM how builtin **prompt** templates are sourced today. Builtins are `CommandLocal`/`CommandLocalJSX` (`registry.go:286-307`) — none are `CommandPrompt`. Run `grep -rn "CommandPrompt\|PromptTemplate{" internal/commands/*.go | grep -iv test` and read `internal/commands/prompt.go` to see how `ExpandPrompt` resolves a template (`registry.go:142` calls `registry.ExpandPrompt`). You must register a `PromptTemplate` for `init`/`review` in `Sources` (likely a new `BundledSkillPrompts`-style entry, or a dedicated builtin-prompts slice). Read the CC prompt text at `commands/init.ts:6` (`OLD_INIT_PROMPT`) and `commands/review.ts:14` (`LOCAL_REVIEW_PROMPT`) and port them faithfully (they are plain instruction strings — `$ARGUMENTS` substitution for review's PR number maps to ccgo's existing arg interpolation; confirm the interpolation token with `grep -n "ARGUMENTS\|\\$1\|argument" internal/commands/prompt.go`).

- [ ] **Step 1: Write the failing test**

Add to `internal/commands/registry_test.go` (or new `builtin_prompts_test.go`):
```go
func TestInitAndReviewAreExpandablePromptCommands(t *testing.T) {
	reg := Load(Options{}) // builtins included by default
	for _, name := range []string{"init", "review"} {
		cmd, ok := reg.Find(name)
		if !ok {
			t.Fatalf("/%s not registered", name)
		}
		if cmd.Type != contracts.CommandPrompt {
			t.Fatalf("/%s type = %q want prompt", name, cmd.Type)
		}
		expanded, err := reg.ExpandPrompt(name, "", "")
		if err != nil {
			t.Fatalf("ExpandPrompt(%s) err: %v", name, err)
		}
		if len(expanded.Message.Content) == 0 {
			t.Fatalf("/%s expanded to empty content", name)
		}
	}
}

func TestReviewInterpolatesArgs(t *testing.T) {
	reg := Load(Options{})
	expanded, err := reg.ExpandPrompt("review", "123", "")
	if err != nil {
		t.Fatal(err)
	}
	text := expanded.Message.Content[0].Text
	if !strings.Contains(text, "123") {
		t.Fatalf("review prompt did not interpolate PR arg: %q", text)
	}
}
```
> CONFIRM `ExpandPrompt` signature: `go doc ./internal/commands Registry` → expected `ExpandPrompt(name, args string, sessionID contracts.ID) (Expanded, error)` with `.Message`. Adjust the test to the real return type/field.

- [ ] **Step 2: Run to verify failure**

Run: `go test ./internal/commands/ -run 'TestInitAndReview|TestReviewInterpolates' -v`
Expected: FAIL — commands not found.

- [ ] **Step 3: Implement**

Add `init` and `review` as `CommandPrompt` builtins (with `Source: CommandSourceBuiltin`, `Description`, `ArgumentHint` for review `[pr-number]`) and register matching `PromptTemplate`s carrying the ported CC prompt text in the builtin-prompt source. Ensure `ExpandPrompt` finds them (the template map is keyed by command name — `registry.go:433-447`). Keep the prompt strings in a small dedicated file `internal/commands/builtin_prompts.go` (they are long; keep under the 800-line cap).

- [ ] **Step 4: Run tests + commit**

Run: `go test ./internal/commands/ -v`
```bash
git add internal/commands/registry.go internal/commands/builtin_prompts.go internal/commands/registry_test.go
git commit -m "feat(commands): /init and /review builtin prompt commands"
```

---

## Task 11: `/doctor` + `claude doctor` health checks (shared engine)

**Behavior (CC `doctorDiagnostic.ts:514` + `screens/Doctor.tsx`):** report install type/version, config sanity, settings parse errors, MCP/keybinding warnings, ripgrep mode, sandbox notes. CC `/doctor` and `claude doctor` share the same engine. We implement a deterministic, **local-only, network-free** diagnostic.

**Files:**
- Create: `internal/doctor/doctor.go`, `internal/doctor/doctor_test.go`
- Create: `cmd/claude/cli_doctor.go`, `cmd/claude/cli_doctor_test.go`
- Modify: `cmd/claude/main.go` (dispatch `doctor`), `internal/commands/slash.go`+`run.go`+`registry.go` (`/doctor` local result + formatter)

> CONFIRM available signals without network: version (`grep -n "version =" cmd/claude/main.go`), settings load errors (`config.LoadSettingsFile`/`ParseSettingsJSON` returning errors), ripgrep detection (`grep -rn "ripgrep\|rg\b\|exec.LookPath" internal/ | grep -iv test | head`). Keep checks to things resolvable from the filesystem + `exec.LookPath`; do NOT call any API or check auth (auth is Phase 4).

- [ ] **Step 1: Write the failing test**

Create `internal/doctor/doctor_test.go`:
```go
package doctor

import (
	"strings"
	"testing"
)

func TestRunChecksReturnsResults(t *testing.T) {
	report := Run(Input{Version: "0.1.0", CWD: t.TempDir()})
	if len(report.Checks) == 0 {
		t.Fatal("expected at least one check")
	}
	var sawVersion bool
	for _, c := range report.Checks {
		if c.Name == "" || (c.Status != StatusOK && c.Status != StatusWarn && c.Status != StatusError) {
			t.Fatalf("malformed check: %+v", c)
		}
		if strings.Contains(strings.ToLower(c.Name), "version") {
			sawVersion = true
		}
	}
	if !sawVersion {
		t.Fatal("expected a version check")
	}
}

func TestFormatReportDeterministic(t *testing.T) {
	report := Report{Checks: []Check{{Name: "Version", Status: StatusOK, Detail: "0.1.0"}}}
	out := Format(report)
	if !strings.Contains(out, "Version") || !strings.Contains(out, "0.1.0") {
		t.Fatalf("format missing content: %q", out)
	}
}
```

- [ ] **Step 2: Run to verify failure**

Run: `go test ./internal/doctor/ -v`
Expected: FAIL — package missing.

- [ ] **Step 3: Implement**

`Run(Input) Report` performs checks: Go runtime/version line, ripgrep availability (`exec.LookPath("rg")` → OK/Warn), settings.json parse status (load user+project settings, report parse errors as Error), `.claude` dir presence, working-dir writability. `Format(Report) string` renders aligned `[OK]/[WARN]/[ERR] Name — Detail` lines. `runDoctorCLI(input, stdout, stderr) int` prints `Format(Run(...))`, exit 1 if any `StatusError`. In `main.go` add the `doctor` dispatch (mirror `plugin` at `:197`). Add `/doctor` as a `LocalCommandResultDoctor` local result + `conversation` formatter calling `doctor.Format(doctor.Run(...))` so it works in the REPL/headless transcript too. Add the `doctor` builtin (`CommandLocalJSX`, `Immediate:true`).

- [ ] **Step 4: Run tests + commit**

Run: `go test ./internal/doctor/ ./cmd/claude/ ./internal/conversation/ -v`
```bash
git add internal/doctor/ cmd/claude/cli_doctor.go cmd/claude/cli_doctor_test.go cmd/claude/main.go internal/commands/slash.go internal/commands/registry.go internal/conversation/run.go
git commit -m "feat(commands): /doctor + claude doctor health checks"
```

---

## Task 12: `claude update` and `claude completion` CLI subcommands

**Behavior:** `claude update` (CC `cli/update.ts:30`) prints current version + reports update status. Since real self-update needs the npm/native installer + network (and our distribution differs), implement a **safe, network-free default**: print current version, install method detection (best-effort), and a clear "checking for updates is not configured / run via your package manager" message — with a `--check` that is a no-op stub returning current version. (Justified: full self-update is distribution-specific and out of the functional-parity core; we provide the command surface + version reporting, deferring network update to a follow-up.) `claude completion <shell>` generates a static bash/zsh/fish completion script (greenfield; CC's external build ships none — verified `main.tsx:4439-4492` ant-gated, `cli/handlers/ant.js` absent).

**Files:**
- Create: `cmd/claude/cli_update.go`, `cmd/claude/cli_completion.go` + tests
- Modify: `cmd/claude/main.go` (dispatch both)

> CONFIRM the version variable name/source: `grep -n "var version\|version =" cmd/claude/main.go`. The completion script should reference the binary name `claude` and the top-level flags/subcommands actually supported (enumerate from the flagset in `run()` + the subcommand dispatch). Keep scripts as static templated strings per shell.

- [ ] **Step 1: Write the failing tests**

`cmd/claude/cli_completion_test.go`:
```go
package main

import (
	"bytes"
	"strings"
	"testing"
)

func TestCompletionBash(t *testing.T) {
	var out, errOut bytes.Buffer
	if code := runCompletionCLI([]string{"bash"}, &out, &errOut); code != 0 {
		t.Fatalf("exit %d stderr=%s", code, errOut.String())
	}
	s := out.String()
	if !strings.Contains(s, "complete") || !strings.Contains(s, "claude") {
		t.Fatalf("bash completion malformed: %q", s)
	}
}

func TestCompletionUnknownShell(t *testing.T) {
	var out, errOut bytes.Buffer
	if code := runCompletionCLI([]string{"powershell-xyz"}, &out, &errOut); code == 0 {
		t.Fatal("unknown shell should be a non-zero exit")
	}
}

func TestCompletionRequiresShellArg(t *testing.T) {
	var out, errOut bytes.Buffer
	if code := runCompletionCLI(nil, &out, &errOut); code == 0 {
		t.Fatal("missing shell arg should error")
	}
}
```
`cmd/claude/cli_update_test.go`:
```go
package main

import (
	"bytes"
	"strings"
	"testing"
)

func TestUpdatePrintsVersion(t *testing.T) {
	var out, errOut bytes.Buffer
	if code := runUpdateCLI(nil, "0.1.0", &out, &errOut); code != 0 {
		t.Fatalf("exit %d stderr=%s", code, errOut.String())
	}
	if !strings.Contains(out.String(), "0.1.0") {
		t.Fatalf("update output missing version: %q", out.String())
	}
}
```

- [ ] **Step 2: Run to verify failure**

Run: `go test ./cmd/claude/ -run 'TestCompletion|TestUpdate' -v`
Expected: FAIL — undefined.

- [ ] **Step 3: Implement**

`runCompletionCLI(args, stdout, stderr) int`: require `args[0]` ∈ {bash, zsh, fish}; print the matching static script; error otherwise. `runUpdateCLI(args, version, stdout, stderr) int`: print current version + status. Dispatch both in `main.go` (mirror `plugin`/`agents`/`doctor`). Keep completion scripts in `cli_completion.go` as `const`s.

- [ ] **Step 4: Run tests + commit**

Run: `go test ./cmd/claude/ -v`
```bash
git add cmd/claude/cli_update.go cmd/claude/cli_update_test.go cmd/claude/cli_completion.go cmd/claude/cli_completion_test.go cmd/claude/main.go
git commit -m "feat(cli): claude update (version status) + claude completion scripts"
```

---

## Task 13: `/hooks` (read-only view) and `/ide` (detect/connect stub); register everything + integration sweep

**Behavior:** `/hooks` is **VIEW-ONLY** (CC `HooksConfigMenu.tsx:3-12`) — summarize configured hooks per event from merged settings. `/ide` CLI side detects IDEs and toggles the `ide` MCP server; the extension itself is OUT of scope, so we implement detection + a guarded connect message (no network/IDE in tests). This task also does the final registration sweep + a non-tty end-to-end smoke test confirming every new command is dispatchable.

**Files:**
- Create: `internal/repl/commands_hooks.go`, `internal/repl/commands_ide.go` (or fold `/hooks` into a `conversation` formatter since it's read-only — prefer the local-result formatter path so it works headless too)
- Modify: `internal/commands/slash.go`+`run.go`+`registry.go`, `internal/repl/run.go`
- Test: `internal/repl/commands_hooks_test.go`, `internal/repl/run_commands_test.go` (integration)

> CONFIRM how hooks config is read: `grep -rn "Hooks\b\|HookConfig\|Settings.Hooks\|getSettings" internal/contracts/settings.go internal/hooks/*.go | grep -iv test | head`. Summarize from `contracts.Settings.Hooks` (confirm field exists). For `/ide`, confirm IDE detection helpers exist or stub detection behind an injected func; ensure no real process spawn in tests.

- [ ] **Step 1: Write the failing test**

`internal/repl/commands_hooks_test.go` — handler returns a read-only summary; with no hooks configured it says so. `internal/repl/run_commands_test.go` — build a loop via the production wiring with a `FakeTerminal` feeding `/theme dark\r`, `/effort high\r`, `/doctor\r`, then `\x04\x04`, with settings writes redirected to a temp dir; assert no panic, the model is never hit (inject a `StartTurn` recorder), and the screen shows status lines. Example skeleton:
```go
func TestREPLDispatchesNewCommandsWithoutModel(t *testing.T) {
	ft := NewFakeTerminal("/doctor\r/effort high\r\x04\x04", 80, 24)
	l := NewLoop(ft, nil)
	router := NewCommandRouter()
	router.Register("doctor", func(ctx context.Context, cc CommandContext) (CommandOutcome, error) {
		return CommandOutcome{Handled: true, Status: "doctor ok"}, nil
	})
	router.Register("effort", func(ctx context.Context, cc CommandContext) (CommandOutcome, error) {
		return CommandOutcome{Handled: true, Status: "effort set"}, nil
	})
	l.onCommand = func(input string) (CommandOutcome, bool) {
		out, err := router.Dispatch(context.Background(), input, CommandContext{Screen: &l.screen})
		if err != nil { return CommandOutcome{}, false }
		return out, out.Handled
	}
	hit := 0
	l.StartTurn = func(string) { hit++ }
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	if err := l.Run(ctx); err != nil { t.Fatalf("Run: %v", err) }
	if hit != 0 { t.Fatalf("commands must not hit the model; hit=%d", hit) }
	if !strings.Contains(ft.Out.String(), "doctor ok") || !strings.Contains(ft.Out.String(), "effort set") {
		t.Fatalf("status lines missing: %q", ft.Out.String())
	}
}
```

- [ ] **Step 2: Run to verify failure**

Run: `go test ./internal/repl/ -run 'TestREPLDispatchesNewCommands|TestHooks' -v`
Expected: FAIL.

- [ ] **Step 3: Implement**

`/hooks` formatter: read `contracts.Settings.Hooks`, list each event → matcher → hook type, or "No hooks configured." `/ide`: detection behind an injected `detect func() []string`; no-arg lists detected IDEs or "No IDE detected."; `open` returns a guarded message (do not spawn in tests). Register all Phase-6b builtins in `registry.go` `BuiltinCommands()` and all live-effect handlers in `run.go`'s router construction (resume, agents, theme, effort, vim, permissions, export, ide). Confirm the production `newTurnLoop` builds the router and sets `loop.onCommand` once, passing a fresh `CommandContext` (Screen, current History, CWD) per dispatch.

- [ ] **Step 4: Full build, vet, suite, smoke**

Run:
```bash
go build ./... && go vet ./... && go test ./internal/repl/ ./internal/commands/ ./internal/config/ ./internal/contracts/ ./internal/conversation/ ./internal/doctor/ ./internal/agentfile/ ./internal/contextreport/ ./cmd/claude/ -v
go test ./...
```
Expected: build OK, vet clean, all green.

Non-tty regression (must not hang, must not enter raw mode):
```bash
echo "/doctor" | go run ./cmd/claude   # line-mode fallback dispatches /doctor and exits
go run ./cmd/claude doctor             # CLI doctor
go run ./cmd/claude completion bash    # prints a completion script
go run ./cmd/claude agents             # lists agents
```

- [ ] **Step 5: Commit**

```bash
git add internal/repl/commands_hooks.go internal/repl/commands_ide.go internal/repl/commands_hooks_test.go internal/repl/run_commands_test.go internal/repl/run.go internal/commands/slash.go internal/commands/registry.go internal/conversation/run.go
git commit -m "feat(commands): /hooks view + /ide detect; register full Phase 6b command set"
```

---

## Self-Review

**Spec coverage (Phase 6b gate = in-scope command coverage ~full; `/resume` actually resumes):**
- REPL command-dispatch harness → Task 1. ✓
- `/resume` real live resume → Task 2. ✓
- `.claude/agents` file model → Task 3; `/agents` + `claude agents` → Task 4. ✓
- settings writer + theme/vim schema → Task 5; `/theme /effort /vim` → Task 6. ✓
- `/permissions` editor (persisted) → Task 7. ✓
- `/context` → Task 8. ✓
- `/export` → Task 9. ✓
- `/init` `/review` prompt commands → Task 10. ✓
- `/doctor` + `claude doctor` → Task 11. ✓
- `claude update` + `claude completion` → Task 12. ✓
- `/hooks` (view) + `/ide` (detect) + registration/integration sweep → Task 13. ✓

**Explicitly DEFERRED / EXCLUDED (by design, restated):** `/login` `/logout` `claude auth` → Phase 4; all debug-only commands → out of scope (never implement); cloud/remote/companion commands → out of scope; the full React/Ink `/agents` wizard, `/permissions` rule-list UI, and `/resume` modal picker → Phase 2 polishes the UI (this phase delivers the functional behavior via arg-or-numbered-list + settings writers); real network self-update → follow-up (we ship the `claude update` surface + version reporting); clipboard export → native/Phase 2 (we ship file export).

**Cross-phase dependencies & risks:**
- **Depends on Phase 1** (the REPL loop, `CommandRouter` seam attaches to `handleKey`'s submit branch — verified present at `loop.go:210-216`).
- **`/permissions` persistence** uses the same `config.WriteSettingsDocument` path that Phase 2's "Allow Session" will use; coordinate to avoid two writers diverging. `permissions.Engine.ApplyUpdate` is the typed alternative — this phase persists via the document writer for simplicity; Phase 2 may unify them.
- **`/context`** token math reuses `compact.EstimateTokens` (Phase 3 wires micro-compact; numbers stay consistent because both read the same estimator).
- **`/agents`** file model is greenfield and independent of the plugin agent loader; a future task can teach `conversation.Runner.toolAvailableAgents` to also read `.claude/agents/*.md` (out of scope here — this phase only provides the file model + editor).
- **Collision risk with Phase 2:** both touch `internal/repl` and `internal/tui`. The `CommandRouter` is additive (new files + one `handleKey` insertion); sequence Phase 2's screen/dialog work to land after this, or rebase carefully.

**Verification-before-completion:** every assumed ccgo symbol is flagged with the exact `go doc`/`grep` to confirm at point of use: `tui.REPLScreen` methods (Task 1), `commands.ParseSlashCommand` (Task 1), `session.SessionInfo`/`ListProjectSessions`/`BuildResumeConversation` (Task 2), frontmatter parser reuse + no-yaml-dep (Task 3), `state.CWD()` (Task 4), settings clone/allowlist + `ReadSettingsDocument`/`WriteSettingsDocument` ENOENT/mkdir behavior + `UserSettingsPath` (Tasks 5–6), `PermissionsSetting` tags (Task 7), `compact.EstimateTokens`/`WindowConfig`/`model.ContextWindowTokens` + runner model accessor (Task 8), transcript renderer reuse (Task 9), `ExpandPrompt` signature + builtin-prompt source + arg interpolation token + CC prompt text at `init.ts:6`/`review.ts:14` (Task 10), version variable + ripgrep/settings signals (Tasks 11–12), `contracts.Settings.Hooks` + IDE detection (Task 13). CC behaviors cited: resume `resume.tsx:194-243`, agents `agentFileUtils.ts`, permissions `PermissionUpdate.ts:208`, context `analyzeContext`, export `export.tsx:53-67`, init `init.ts:6`, review `review.ts:14`, doctor `doctorDiagnostic.ts:514`, theme `theme.tsx`, effort `effort.tsx:19,76`+`effort.ts:14`, vim `vim.ts:8-19`, hooks `HooksConfigMenu.tsx:3-12`, ide `ide.tsx:419-556`, CLI `main.tsx:4278-4492`.

**Tests never require a tty or network:** every handler test injects fakes (session lister/loader, settings mutator, IDE detector) and uses `t.TempDir()`; the loop integration tests use `FakeTerminal`; doctor/completion/update CLI tests write to `bytes.Buffer`. No test calls the Anthropic API or `term.MakeRaw`.
