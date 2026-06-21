# Hooks Lifecycle (Phase 6d) Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Bring ccgo's hook subsystem to Claude Code parity for the lifecycle/event surface: (1) add and **fire** the missing hook events (`SessionStart`, `SessionEnd`, `Notification`, `SubagentStart`, `PostCompact`, `StopFailure`); (2) make sub-agent lifecycle hooks (`SubagentStart`/`SubagentStop`) complete; (3) change multi-hook execution from **sequential short-circuit** to **parallel with `deny > ask > allow` precedence** and accumulated context; (4) complete the hook input/output JSON schema (per-event `matchQuery` selection, base-input fields, output union) and matcher semantics; (5) wire firing points into the conversation/session loop (`RunTurn` / `RunInteractive`).

**Architecture:** ccgo already has a real hook subsystem: `internal/hooks/command.go` parses settings into `tool.Hook` values (`CommandHook`/`HTTPHook`), and two execution sites run them — the **tool executor** (`internal/tool/executor.go`: `PreToolUse`/`PostToolUse`/`PermissionRequest`/`PermissionDenied`) and the **conversation runner** (`internal/conversation/hooks.go`: `UserPromptSubmit`/`Stop`/`SubagentStop`/`PreCompact`). Both loops iterate matched hooks **sequentially and return on first block**. This phase keeps the parse layer and the two call sites, and: adds the missing phase constants + their `matchQuery` semantics in `internal/hooks/`; introduces a single shared **parallel resolver** (`internal/hooks/resolve.go`) that runs the matched hooks for a phase concurrently and folds their results with `deny > ask > allow` precedence + concatenated context; rewires both call sites to use it; and adds firing points for `SessionStart`/`SessionEnd` (session boundary in `internal/repl/run.go` + a runner entrypoint), `Notification` (emit path), `SubagentStart` (task agent launch), and `PostCompact` (after compaction). The resolver is the TDD core; concurrency is made deterministic with a barrier (`sync` primitives, no sleeps).

**Tech Stack:** Go 1.26; **no new third-party deps** (only stdlib `sync`, `context`, `encoding/json`). Existing packages: `internal/hooks`, `internal/tool`, `internal/conversation`, `internal/compact`, `internal/contracts`, `internal/messages`, `internal/repl`, `internal/permissions`.

## Global Constraints

Copied verbatim from the master roadmap §6 (apply to every step):

- **Module/toolchain:** `ccgo`, `go 1.26` (confirmed: `go.mod` line 1–3 = `module ccgo` / `go 1.26`).
- **Immutability (CRITICAL):** never mutate shared structs in place; return new copies. Copy the `conversation.Runner` value per turn before setting `OnEvent`/`Tools.Asker` (existing pattern in `internal/repl/run.go:20`). `permissions.Engine.ApplyUpdate` already returns a **new** engine — honor that. The parallel resolver MUST NOT mutate the input `[]tool.Hook`; it returns a new `HookResolution` value.
- **Many small files:** one responsibility per file; target 150–350 lines (800 hard max). `internal/hooks/command.go` is already 835 lines — DO NOT grow it; new code goes in new files (`resolve.go`, `events.go`, `input.go`).
- **Errors handled explicitly at every level; never swallow.** Any acquired resource MUST be released on every exit path (`defer`). Goroutines in the resolver MUST be joined (`WaitGroup`) before returning — no leaks.
- **Input validation at boundaries:** validate all external data (hook stdout/HTTP body JSON, settings map shape, exit codes); fail fast with clear messages. Untrusted hook output JSON is parsed defensively (already done in `hookResultFromJSON`; extend, do not weaken).
- **No new third-party deps.** Phase 1 added only `golang.org/x/term`. No bubbletea/tcell/charm; no concurrency libs.
- **Non-TTY safety:** interactive paths MUST NOT call `term.MakeRaw` when stdin/stdout isn't a tty; fall back to line mode. Tests MUST NOT depend on a real tty.
- **TDD:** every task writes a failing test first, then minimal code. Commit after each task. Run package tests with `go test ./internal/<pkg>/ -run TestName -v`; full suite `go test ./...`. Concurrency tests run with `-race`.
- **Verify against real code, distrust roadmap docs:** every assumed type name, field, constant, or CC behavior is confirmed with `go doc`/`grep` (ccgo side) or by reading `/Users/sqlrush/agent/claude-code/src` (CC side) before writing the test — the exact command is flagged at the point of use.
- **Security:** no hardcoded secrets; never leak sensitive data in errors. Hook env-var interpolation allow-listing (`HTTPHook.interpolateHeader`) MUST be preserved.

---

## Current state vs target (code-verified 2026-06-21)

> **Gap-audit-vs-code discrepancy (IMPORTANT):** the gap audit (`docs/gap-audit-2026-06-21.md:28,110`) and roadmap §5 Phase 6d claim "8/28 events; **no `prompt`/`agent` hook types**; multi-hook is sequential short-circuit." Reading the code, **`UserPromptSubmit` (the "prompt" hook) and `Stop`/`SubagentStop` (the "agent" hooks) already exist AND already fire** — they are wired in `internal/conversation/hooks.go` and called from `internal/conversation/run.go:75,239` and `internal/conversation/task_agent.go:181`. So the "no prompt/agent hook types" claim is **stale**. What is *actually* missing is narrower. This plan targets the real gaps. The "8/28" framing is also misleading: CC has **27** hook events (`coreSchemas.ts:355-383`), but the in-scope local subset is ~16 (the cloud/companion ones — `TeammateIdle`, `TaskCreated/Completed`, `Elicitation*`, `WorktreeCreate/Remove`, `ConfigChange`, `CwdChanged`, `FileChanged`, `InstructionsLoaded`, `Setup` — are OUT of scope per roadmap §1).

**Currently implemented + firing (8 events):**

| Event | Constant (`internal/tool/types.go`) | Fired from |
|---|---|---|
| `PreToolUse` | `HookPreToolUse:101` | `executor.go:412` (`runPreHooks`) |
| `PostToolUse` | `HookPostToolUse:102` | `executor.go:503` (`runPostHooks`) |
| `PermissionRequest` | `HookPermissionRequest:103` | `executor.go:447` (`runPermissionRequestHooks`) |
| `PermissionDenied` | `HookPermissionDenied:104` | `executor.go:442` (`runPermissionDeniedHooks`) |
| `UserPromptSubmit` | `HookUserPromptSubmit:105` | `conversation/run.go:75` (`applyUserPromptSubmitHooks`) |
| `Stop` | `HookStop:106` | `conversation/run.go:239` (`runStopHooks`) |
| `SubagentStop` | `HookSubagentStop:107` | `conversation/task_agent.go:181` (`runSubagentStopHooks`) |
| `PreCompact` | `HookPreCompact:108` | `conversation/run.go:552,589` (`runPreCompactHooks`) |

**Missing (this phase adds them):** `SessionStart`, `SessionEnd`, `Notification`, `SubagentStart`, `PostCompact`, `StopFailure` (confirmed absent: `grep "HookSessionStart\|HookSessionEnd\|HookNotification\|HookSubagentStart\|HookPostCompact\|HookStopFailure" internal/` → NONE FOUND).

**Multi-hook semantics gap:** both loops are sequential short-circuit. `internal/conversation/hooks.go:121-148` `for idx, hook := range hooks { ... if result.Block { return } }`. Executor permission loop `internal/tool/executor.go:453-494` folds decisions but in config order, **last-decision-wins, no precedence** (`hookDecision = &decisionCopy` overwrites). CC runs hooks in **parallel** and folds permission with **`deny > ask > allow`** (`utils/hooks.ts:2820-2847`).

**CC target taxonomy (in-scope subset), with `matchQuery` selector (`utils/hooks.ts:1615-1670`):**

| Event | matchQuery selector | matcher honored? |
|---|---|---|
| `PreToolUse`/`PostToolUse`/`PermissionRequest`/`PermissionDenied` | `tool_name` | yes |
| `SessionStart` | `source` (`startup`/`resume`/`clear`/`compact`) | yes |
| `SessionEnd` | `reason` (`clear`/`resume`/`logout`/`prompt_input_exit`/`other`/`bypass_permissions_disabled`) | yes |
| `PreCompact`/`PostCompact` | `trigger` (`manual`/`auto`) | yes |
| `Notification` | `notification_type` | yes |
| `SubagentStart`/`SubagentStop` | `agent_type` | yes |
| `StopFailure` | `error` | yes |
| `Stop`/`UserPromptSubmit` | none (undefined) → all hooks run | no (matcher ignored) |

---

## File Structure

**New files in `internal/hooks/`:**
- `events.go` — phase-constant catalog (re-export of `tool.Hook*` plus the new ones), `MatchQuery(phase string, payload map[string]any) (string, bool)` (per-event selector; `false` = run all), `IsLifecyclePhase`.
- `input.go` — `BaseInput` builder + `BuildInput(ctx, event)` producing the full CC-shaped payload map (extracted/shared with `command.go`'s `hookInput`), plus output-schema validation helpers.
- `resolve.go` — `Resolution` struct + `Resolve(ctx tool.Context, hooks []tool.Hook, event tool.HookEvent) (Resolution, error)`: runs matched hooks **in parallel**, folds with `deny > ask > allow`, concatenates context/messages, first-blocker-decisive. The TDD core.
- `resolve_test.go`, `events_test.go`, `input_test.go` — tests (use echo scripts in `t.TempDir()`; deterministic barriers).

**New file in `internal/conversation/`:**
- `lifecycle.go` — `RunSessionStartHooks`/`RunSessionEndHooks`/`RunNotificationHooks`/`RunPostCompactHooks` on `Runner`, plus `SessionStartSource`/`SessionEndReason` typed constants.

**Modified existing files:**
- `internal/tool/types.go` — add the 6 new phase constants.
- `internal/hooks/command.go` — `applyHookSpecificOutput` switch gains the new lifecycle phases (extract shared `hookInput` into `input.go`); add `Notification`/`SessionStart`/`SessionEnd`/`SubagentStart`/`PostCompact` to the `additionalContext` case.
- `internal/conversation/hooks.go` — `runConversationHooks` rewritten to delegate to `hooks.Resolve` (parallel). New payload selectors via `hooks.MatchQuery`.
- `internal/tool/executor.go` — permission-hook fold (`runPermissionHooks`) replaced by `deny > ask > allow` precedence (parallel-safe). `runPreHooks`/`runPostHooks` keep sequential blocking but adopt precedence fold for any `PermissionDecision` returned (PreToolUse can deny).
- `internal/conversation/run.go` — fire `PostCompact` after compaction (around `:552`/`:589`); pass `SubagentStop`/`Stop` payloads unchanged.
- `internal/conversation/task_agent.go` — fire `SubagentStart` at subagent launch.
- `internal/repl/run.go` — fire `SessionStart{source:"startup"|"resume"}` before the loop, `SessionEnd{reason:"prompt_input_exit"|"other"}` on exit (defer).

---

## Task 1: Add the missing lifecycle phase constants

**Files:**
- Modify: `internal/tool/types.go` (add 6 constants)
- Test: `internal/tool/types_hookphase_test.go`

**Interfaces:**
- Produces: `tool.HookSessionStart`, `tool.HookSessionEnd`, `tool.HookNotification`, `tool.HookSubagentStart`, `tool.HookPostCompact`, `tool.HookStopFailure` (all `string`).

> Confirm the exact existing block before editing: `grep -n "HookPreToolUse\|HookPreCompact" internal/tool/types.go` (expected lines 101–108). Confirm the strings against CC: the canonical list is `entrypoints/sdk/coreSchemas.ts:355-383` — exact strings `SessionStart`, `SessionEnd`, `Notification`, `SubagentStart`, `PostCompact`, `StopFailure`.

- [ ] **Step 1: Write the failing test**

Create `internal/tool/types_hookphase_test.go`:
```go
package tool

import "testing"

func TestLifecycleHookPhaseConstants(t *testing.T) {
	cases := map[string]string{
		HookSessionStart:  "SessionStart",
		HookSessionEnd:    "SessionEnd",
		HookNotification:  "Notification",
		HookSubagentStart: "SubagentStart",
		HookPostCompact:   "PostCompact",
		HookStopFailure:   "StopFailure",
	}
	for got, want := range cases {
		if got != want {
			t.Fatalf("hook phase constant = %q want %q", got, want)
		}
	}
	// Sanity: pre-existing constants are unchanged.
	if HookPreToolUse != "PreToolUse" || HookPreCompact != "PreCompact" {
		t.Fatalf("existing constants changed: %q %q", HookPreToolUse, HookPreCompact)
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/tool/ -run TestLifecycleHookPhaseConstants -v`
Expected: FAIL — `undefined: HookSessionStart` (compile error).

- [ ] **Step 3: Write minimal implementation**

In `internal/tool/types.go`, extend the const block (after `HookPreCompact = "PreCompact"`, line 108):
```go
	HookSessionStart  = "SessionStart"
	HookSessionEnd    = "SessionEnd"
	HookNotification  = "Notification"
	HookSubagentStart = "SubagentStart"
	HookPostCompact   = "PostCompact"
	HookStopFailure   = "StopFailure"
```

- [ ] **Step 4: Run tests to verify they pass**

Run: `go test ./internal/tool/ -run TestLifecycleHookPhaseConstants -v && go test ./internal/tool/`
Expected: PASS; no regression in existing executor tests.

- [ ] **Step 5: Commit**

```bash
git add internal/tool/types.go internal/tool/types_hookphase_test.go
git commit -m "feat(tool): add SessionStart/SessionEnd/Notification/SubagentStart/PostCompact/StopFailure hook phase constants"
```

---

## Task 2: Per-event `matchQuery` selection + lifecycle catalog

**Files:**
- Create: `internal/hooks/events.go`
- Test: `internal/hooks/events_test.go`

**Interfaces:**
- Produces:
  - `func MatchQuery(phase string, payload map[string]any) (query string, honored bool)` — returns the value the matcher is tested against, and whether the matcher is honored at all (`false` ⇒ run every configured hook regardless of `matcher`, for `Stop`/`UserPromptSubmit`).
  - `func IsLifecyclePhase(phase string) bool` — true for non-tool, non-permission phases (used to route conversation vs executor).

> Confirm CC matchQuery selectors: `utils/hooks.ts:1615-1670`. Key facts to encode: `SessionStart`→`source`; `SessionEnd`→`reason`; `PreCompact`/`PostCompact`→`trigger`; `Notification`→`notification_type`; `SubagentStart`/`SubagentStop`→`agent_type`; `StopFailure`→`error`; tool phases→`tool_name`; `Stop`/`UserPromptSubmit`→none. Confirm payload key names ccgo already emits: `grep -n "\"trigger\"\|\"agent_type\"\|\"prompt\"" internal/conversation/hooks.go internal/conversation/task_agent.go` (ccgo uses `trigger`, `agent_id`/`task_id`; this task standardizes on CC keys, adding `agent_type` and `notification_type`).

- [ ] **Step 1: Write the failing test**

Create `internal/hooks/events_test.go`:
```go
package hooks

import (
	"testing"

	"ccgo/internal/tool"
)

func TestMatchQuery(t *testing.T) {
	cases := []struct {
		name       string
		phase      string
		payload    map[string]any
		wantQuery  string
		wantHonor  bool
	}{
		{"pretooluse", tool.HookPreToolUse, map[string]any{"tool_name": "Bash"}, "Bash", true},
		{"sessionstart", tool.HookSessionStart, map[string]any{"source": "startup"}, "startup", true},
		{"sessionend", tool.HookSessionEnd, map[string]any{"reason": "logout"}, "logout", true},
		{"precompact", tool.HookPreCompact, map[string]any{"trigger": "auto"}, "auto", true},
		{"postcompact", tool.HookPostCompact, map[string]any{"trigger": "manual"}, "manual", true},
		{"notification", tool.HookNotification, map[string]any{"notification_type": "permission"}, "permission", true},
		{"subagentstart", tool.HookSubagentStart, map[string]any{"agent_type": "code-reviewer"}, "code-reviewer", true},
		{"subagentstop", tool.HookSubagentStop, map[string]any{"agent_type": "code-reviewer"}, "code-reviewer", true},
		{"stopfailure", tool.HookStopFailure, map[string]any{"error": "boom"}, "boom", true},
		{"stop-no-matcher", tool.HookStop, map[string]any{"stop_reason": "end_turn"}, "", false},
		{"userprompt-no-matcher", tool.HookUserPromptSubmit, map[string]any{"prompt": "hi"}, "", false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			q, honored := MatchQuery(tc.phase, tc.payload)
			if q != tc.wantQuery || honored != tc.wantHonor {
				t.Fatalf("MatchQuery(%s) = %q,%v want %q,%v", tc.phase, q, honored, tc.wantQuery, tc.wantHonor)
			}
		})
	}
}

func TestIsLifecyclePhase(t *testing.T) {
	if !IsLifecyclePhase(tool.HookSessionStart) || !IsLifecyclePhase(tool.HookStop) {
		t.Fatal("expected lifecycle phases")
	}
	if IsLifecyclePhase(tool.HookPreToolUse) || IsLifecyclePhase(tool.HookPermissionRequest) {
		t.Fatal("tool/permission phases are not lifecycle")
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/hooks/ -run 'TestMatchQuery|TestIsLifecyclePhase' -v`
Expected: FAIL — `undefined: MatchQuery`.

- [ ] **Step 3: Write minimal implementation**

Create `internal/hooks/events.go`:
```go
package hooks

import (
	"strings"

	"ccgo/internal/tool"
)

// MatchQuery returns the value the matcher pattern is tested against for the
// given phase, and whether matching is honored at all. When honored is false
// (Stop, UserPromptSubmit) every configured hook for the phase runs regardless
// of its matcher. Mirrors CC utils/hooks.ts:1615-1670.
func MatchQuery(phase string, payload map[string]any) (string, bool) {
	switch phase {
	case tool.HookPreToolUse, tool.HookPostToolUse,
		tool.HookPermissionRequest, tool.HookPermissionDenied:
		return payloadString(payload, "tool_name"), true
	case tool.HookSessionStart:
		return payloadString(payload, "source"), true
	case tool.HookSessionEnd:
		return payloadString(payload, "reason"), true
	case tool.HookPreCompact, tool.HookPostCompact:
		return payloadString(payload, "trigger"), true
	case tool.HookNotification:
		return payloadString(payload, "notification_type"), true
	case tool.HookSubagentStart, tool.HookSubagentStop:
		return payloadString(payload, "agent_type"), true
	case tool.HookStopFailure:
		return payloadString(payload, "error"), true
	case tool.HookStop, tool.HookUserPromptSubmit:
		return "", false
	default:
		return "", false
	}
}

// IsLifecyclePhase reports whether the phase is a conversation/session
// lifecycle event (not a per-tool-call or permission event).
func IsLifecyclePhase(phase string) bool {
	switch phase {
	case tool.HookPreToolUse, tool.HookPostToolUse,
		tool.HookPermissionRequest, tool.HookPermissionDenied:
		return false
	default:
		return true
	}
}

func payloadString(payload map[string]any, key string) string {
	if payload == nil {
		return ""
	}
	value, _ := payload[key].(string)
	return strings.TrimSpace(value)
}
```

- [ ] **Step 4: Run tests to verify they pass**

Run: `go test ./internal/hooks/ -run 'TestMatchQuery|TestIsLifecyclePhase' -v`
Expected: PASS (all subtests).

- [ ] **Step 5: Commit**

```bash
git add internal/hooks/events.go internal/hooks/events_test.go
git commit -m "feat(hooks): add per-event matchQuery selection and lifecycle-phase routing"
```

---

## Task 3: Parallel hook resolver with `deny > ask > allow` precedence

**Files:**
- Create: `internal/hooks/resolve.go`
- Test: `internal/hooks/resolve_test.go`

**Interfaces:**
- Produces:
  - `type Resolution struct { Block bool; Message string; AdditionalContext []string; PermissionDecision *contracts.PermissionDecision; UpdatedInput json.RawMessage; Metadata map[string]any }`
  - `func Resolve(ctx tool.Context, hooks []tool.Hook, event tool.HookEvent) (Resolution, error)` — runs every hook **concurrently** (one goroutine each, joined via `sync.WaitGroup`), then folds the results: permission behavior with `deny > ask > allow` precedence; `Block` if any result blocks; all non-empty messages concatenated; all `Metadata` namespaced by index; first `UpdatedInput` wins (deterministic by config index, not completion order).

> Confirm CC precedence VERBATIM (`utils/hooks.ts:2820-2847`): `deny` always wins; `ask` only if not already deny; `allow` only fills an empty slot; `passthrough` is a no-op. Confirm parallel execution: `utils/hooks.ts:2744` `for await (const result of all(hookPromises))` + `utils/generators.ts:31-72` (concurrent). Confirm context concatenation: each hook yields its own `additionalContext`, consumers collect into an array (`utils/sessionStart.ts:148,163-172`).
>
> Confirm the ccgo `tool.HookResult` fields used here: `grep -n "type HookResult struct" -A8 internal/tool/types.go` → `Block`, `Message`, `UpdatedInput`, `PermissionDecision`, `Metadata`. Confirm `contracts.PermissionBehavior` values: `grep -rn "PermissionAllow\|PermissionAsk\|PermissionDeny" internal/contracts/*.go | head`.

**Determinism note:** the fold must be order-independent for correctness (deny/ask/allow precedence is associative & commutative), but to make `Message`/`UpdatedInput`/`Metadata` deterministic regardless of goroutine completion order, collect each goroutine's result into a pre-sized `results[i]` slot (indexed by config position), then fold in index order after `wg.Wait()`. Tests use a `sync.WaitGroup` barrier inside fake hooks so all hooks are provably in-flight concurrently before any returns — no `time.Sleep`.

- [ ] **Step 1: Write the failing test**

Create `internal/hooks/resolve_test.go`:
```go
package hooks

import (
	"context"
	"encoding/json"
	"sync"
	"testing"

	"ccgo/internal/contracts"
	"ccgo/internal/tool"
)

// barrierHook blocks until all N hooks have started (proving parallelism with
// no sleeps), then returns a fixed result.
type barrierHook struct {
	start  *sync.WaitGroup // Done() once on entry
	gate   chan struct{}   // closed after all started
	result tool.HookResult
}

func (h barrierHook) RunToolHook(_ tool.Context, _ tool.HookEvent) (tool.HookResult, error) {
	h.start.Done()
	<-h.gate
	return h.result, nil
}

func resolveWithBarrier(t *testing.T, results []tool.HookResult) Resolution {
	t.Helper()
	var started sync.WaitGroup
	started.Add(len(results))
	gate := make(chan struct{})
	hooks := make([]tool.Hook, len(results))
	for i, r := range results {
		hooks[i] = barrierHook{start: &started, gate: gate, result: r}
	}
	go func() { started.Wait(); close(gate) }() // open gate only once all started
	res, err := Resolve(tool.Context{Context: context.Background()}, hooks,
		tool.HookEvent{Phase: tool.HookPreToolUse})
	if err != nil {
		t.Fatalf("Resolve err: %v", err)
	}
	return res
}

func deny() tool.HookResult {
	return tool.HookResult{PermissionDecision: &contracts.PermissionDecision{Behavior: contracts.PermissionDeny, Message: "no"}}
}
func ask() tool.HookResult {
	return tool.HookResult{PermissionDecision: &contracts.PermissionDecision{Behavior: contracts.PermissionAsk}}
}
func allow() tool.HookResult {
	return tool.HookResult{PermissionDecision: &contracts.PermissionDecision{Behavior: contracts.PermissionAllow}}
}

func TestResolvePrecedence(t *testing.T) {
	cases := []struct {
		name string
		in   []tool.HookResult
		want contracts.PermissionBehavior
	}{
		{"allow-only", []tool.HookResult{allow(), allow()}, contracts.PermissionAllow},
		{"ask-beats-allow", []tool.HookResult{allow(), ask()}, contracts.PermissionAsk},
		{"deny-beats-ask", []tool.HookResult{ask(), deny()}, contracts.PermissionDeny},
		{"deny-beats-allow", []tool.HookResult{deny(), allow()}, contracts.PermissionDeny},
		{"deny-beats-all", []tool.HookResult{allow(), ask(), deny()}, contracts.PermissionDeny},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			res := resolveWithBarrier(t, tc.in)
			if res.PermissionDecision == nil {
				t.Fatalf("nil decision")
			}
			if res.PermissionDecision.Behavior != tc.want {
				t.Fatalf("behavior = %v want %v", res.PermissionDecision.Behavior, tc.want)
			}
		})
	}
}

func TestResolveConcatenatesContext(t *testing.T) {
	res := resolveWithBarrier(t, []tool.HookResult{
		{Message: "first"},
		{Message: "second"},
	})
	if len(res.AdditionalContext) != 2 || res.AdditionalContext[0] != "first" || res.AdditionalContext[1] != "second" {
		t.Fatalf("context = %#v", res.AdditionalContext)
	}
	if res.Message != "first\nsecond" {
		t.Fatalf("message = %q", res.Message)
	}
}

func TestResolveBlockIsSticky(t *testing.T) {
	res := resolveWithBarrier(t, []tool.HookResult{
		{},
		{Block: true, Message: "blocked here"},
		{},
	})
	if !res.Block || res.Message != "blocked here" {
		t.Fatalf("res = %#v", res)
	}
}

func TestResolveFirstUpdatedInputWins(t *testing.T) {
	res := resolveWithBarrier(t, []tool.HookResult{
		{UpdatedInput: json.RawMessage(`{"a":1}`)},
		{UpdatedInput: json.RawMessage(`{"a":2}`)},
	})
	if string(res.UpdatedInput) != `{"a":1}` {
		t.Fatalf("updatedInput = %s", res.UpdatedInput)
	}
}

func TestResolveEmpty(t *testing.T) {
	res, err := Resolve(tool.Context{Context: context.Background()}, nil, tool.HookEvent{})
	if err != nil || res.Block || res.PermissionDecision != nil {
		t.Fatalf("empty resolve = %#v, %v", res, err)
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/hooks/ -run TestResolve -race -v`
Expected: FAIL — `undefined: Resolve` / `undefined: Resolution`.

- [ ] **Step 3: Write minimal implementation**

Create `internal/hooks/resolve.go`:
```go
package hooks

import (
	"encoding/json"
	"strings"
	"sync"

	"ccgo/internal/contracts"
	"ccgo/internal/tool"
)

// Resolution is the folded outcome of running all matched hooks for one event.
type Resolution struct {
	Block              bool
	Message            string
	AdditionalContext  []string
	PermissionDecision *contracts.PermissionDecision
	UpdatedInput       json.RawMessage
	Metadata           map[string]any
}

type hookOutcome struct {
	result tool.HookResult
	err    error
}

// Resolve runs every hook concurrently and folds the results with permission
// precedence deny > ask > allow (CC utils/hooks.ts:2820-2847), concatenated
// context, sticky Block, and deterministic (config-order) UpdatedInput/Metadata.
// It never mutates the input slice. The first hook error aborts with that error.
func Resolve(ctx tool.Context, hooks []tool.Hook, event tool.HookEvent) (Resolution, error) {
	if len(hooks) == 0 {
		return Resolution{}, nil
	}
	outcomes := make([]hookOutcome, len(hooks))
	var wg sync.WaitGroup
	wg.Add(len(hooks))
	for i := range hooks {
		go func(i int) {
			defer wg.Done()
			result, err := hooks[i].RunToolHook(ctx, event)
			outcomes[i] = hookOutcome{result: result, err: err}
		}(i)
	}
	wg.Wait()

	var res Resolution
	var behavior contracts.PermissionBehavior // "" until a hook sets one
	var decisionMessage string
	for i, oc := range outcomes {
		if oc.err != nil {
			return Resolution{}, oc.err
		}
		hr := oc.result
		if msg := strings.TrimSpace(hr.Message); msg != "" {
			res.AdditionalContext = append(res.AdditionalContext, msg)
		}
		if hr.Block {
			res.Block = true
		}
		if len(hr.UpdatedInput) > 0 && len(res.UpdatedInput) == 0 {
			res.UpdatedInput = hr.UpdatedInput
		}
		if len(hr.Metadata) > 0 {
			if res.Metadata == nil {
				res.Metadata = map[string]any{}
			}
			res.Metadata[metadataKey(i)] = hr.Metadata
		}
		if hr.PermissionDecision != nil {
			behavior = foldBehavior(behavior, hr.PermissionDecision.Behavior)
			if hr.PermissionDecision.Behavior == contracts.PermissionDeny &&
				strings.TrimSpace(hr.PermissionDecision.Message) != "" {
				decisionMessage = hr.PermissionDecision.Message
			} else if decisionMessage == "" {
				decisionMessage = hr.PermissionDecision.Message
			}
		}
	}
	res.Message = strings.Join(res.AdditionalContext, "\n")
	if behavior != "" {
		res.PermissionDecision = &contracts.PermissionDecision{Behavior: behavior, Message: decisionMessage}
		if behavior == contracts.PermissionDeny {
			res.Block = true
		}
	}
	return res, nil
}

// foldBehavior applies deny > ask > allow precedence (passthrough is a no-op).
func foldBehavior(current, next contracts.PermissionBehavior) contracts.PermissionBehavior {
	switch next {
	case contracts.PermissionDeny:
		return contracts.PermissionDeny // deny always wins
	case contracts.PermissionAsk:
		if current != contracts.PermissionDeny {
			return contracts.PermissionAsk
		}
		return current
	case contracts.PermissionAllow:
		if current == "" {
			return contracts.PermissionAllow
		}
		return current
	default:
		return current // passthrough / unknown: no change
	}
}

func metadataKey(index int) string {
	return "hook_" + strconvItoa(index)
}

func strconvItoa(i int) string {
	if i == 0 {
		return "0"
	}
	var b [20]byte
	pos := len(b)
	for i > 0 {
		pos--
		b[pos] = byte('0' + i%10)
		i /= 10
	}
	return string(b[pos:])
}
```

> Note: if `strconv` is already imported elsewhere in the package, replace `strconvItoa` with `strconv.Itoa` and import `"strconv"` — confirm with `grep -n "strconv" internal/hooks/command.go` (it already imports `strconv`, so in practice import it here and delete `strconvItoa`).

- [ ] **Step 4: Run tests to verify they pass**

Run: `go test ./internal/hooks/ -run TestResolve -race -v`
Expected: PASS (all subtests, no data race). The barrier proves all hooks run concurrently; the fold is deterministic in config order.

- [ ] **Step 5: Commit**

```bash
git add internal/hooks/resolve.go internal/hooks/resolve_test.go
git commit -m "feat(hooks): add parallel hook resolver with deny>ask>allow precedence"
```

---

## Task 4: Wire conversation hooks through the parallel resolver + matcher filtering

**Files:**
- Modify: `internal/conversation/hooks.go` (`runConversationHooks` delegates to `hooks.Resolve`; apply `MatchQuery` filtering)
- Test: `internal/conversation/hooks_resolve_test.go`

**Interfaces:**
- Consumes: `hooks.Resolve`, `hooks.MatchQuery`, the existing `conversationHooksForPhase` (phase filter). Adds matcher filtering by `MatchQuery`.
- Behavior change: `runConversationHooks` now (1) selects hooks for the phase, (2) drops hooks whose matcher doesn't match the `MatchQuery` value (when honored), (3) runs them **in parallel** via `Resolve`, (4) returns the folded `tool.HookResult`. Block/precedence semantics preserved at call sites (`runStopHooks`, etc.).

> Confirm the current sequential loop being replaced: `internal/conversation/hooks.go:103-151`. Confirm `conversationHooksForPhase` exists (`:167`). Confirm the matcher predicate available: the parse layer stores `Matcher` on `CommandHook`/`HTTPHook`; the matching fn is `matchesPattern` in `command.go:727` (unexported). To filter by matcher at the conversation layer without exporting internals, route filtering through a new exported `hooks.Matches(hook tool.Hook, query string) bool` added in this task (delegates to `matchesPattern` + reads the hook's `Matcher` field via a small `matcherOf` switch). Confirm field name: `grep -n "Matcher " internal/hooks/command.go`.

- [ ] **Step 1: Write the failing test**

Create `internal/conversation/hooks_resolve_test.go`:
```go
package conversation

import (
	"context"
	"path/filepath"
	"testing"

	"ccgo/internal/contracts"
	hookpkg "ccgo/internal/hooks"
	"ccgo/internal/tool"
)

// writeEchoHook returns a command that prints a JSON hook output to stdout.
func denyJSONCommand() string {
	// PreToolUse-style deny via hookSpecificOutput is not valid for Stop; for a
	// conversation phase, a non-zero exit 2 blocks. Use exit 2 with stderr.
	return `printf '%s\n' 'stop blocked' >&2; exit 2`
}

func TestRunConversationHooksParallelBlock(t *testing.T) {
	dir := t.TempDir()
	marker := filepath.Join(dir, "ran")
	r := Runner{
		WorkingDirectory: dir,
		SessionID:        "sess_conv",
		settingsOverride: &contracts.Settings{
			Hooks: map[string]any{
				"Stop": []any{map[string]any{
					"hooks": []any{
						map[string]any{"type": "command", "command": "printf ctx-a"},
						map[string]any{"type": "command", "command": denyJSONCommand()},
						map[string]any{"type": "command", "command": "printf ctx-c > " + shellQuoteConv(marker)},
					},
				}},
			},
		},
	}
	result, err := r.runConversationHooks(context.Background(), tool.HookStop, map[string]any{"stop_reason": "end_turn"})
	if err != nil {
		t.Fatal(err)
	}
	if !result.Block {
		t.Fatalf("expected Block from exit-2 hook; result=%#v", result)
	}
	// Parallel: even though hook[1] blocks, hook[2] still ran (no short-circuit).
	if _, statErr := osStat(marker); statErr != nil {
		t.Fatalf("hook[2] did not run (sequential short-circuit not removed): %v", statErr)
	}
}
```

> This test requires a `settingsOverride` test seam on `Runner` and a `shellQuoteConv`/`osStat` test helper. Before writing, confirm how existing conversation tests inject settings: `grep -rn "settingsOverride\|mergedSettings\|Settings{" internal/conversation/*_test.go | head`. If a seam already exists (e.g. a field consulted by `mergedSettings`), use it and delete `settingsOverride`. If not, this task adds a minimal `settingsOverride *contracts.Settings` field to `Runner` consulted first in `mergedSettings()` (guarded `if r.settingsOverride != nil { return *r.settingsOverride }`) — a legitimate test seam, immutable read. Confirm `mergedSettings` location: `internal/conversation/run.go:5281`. Reuse the existing `shellQuote` pattern from `internal/hooks/command_test.go:143` (copy as `shellQuoteConv`); `osStat` is just `os.Stat`.

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/conversation/ -run TestRunConversationHooksParallelBlock -race -v`
Expected: FAIL — either the marker test fails (proving old sequential short-circuit) or compile error on the new helpers, depending on seam.

- [ ] **Step 3: Write minimal implementation**

In `internal/hooks/resolve.go` (or a small `matcher.go`), add the exported matcher predicate:
```go
// Matches reports whether the hook's matcher accepts the given query. A hook
// with no matcher (or "*") matches everything. Mirrors command.matchesPattern.
func Matches(hook tool.Hook, query string) bool {
	return matchesPattern(query, matcherOf(hook))
}

func matcherOf(hook tool.Hook) string {
	switch h := hook.(type) {
	case CommandHook:
		return h.Matcher
	case HTTPHook:
		return h.Matcher
	default:
		return ""
	}
}
```

Rewrite `runConversationHooks` in `internal/conversation/hooks.go` to filter by matcher and delegate to `Resolve`:
```go
func (r Runner) runConversationHooks(ctx context.Context, phase string, payload map[string]any) (tool.HookResult, error) {
	settings := r.mergedSettings()
	candidates := conversationHooksForPhase(r.configuredHooks(settings), phase)
	matched := filterByMatcher(phase, candidates, payload)
	if len(matched) == 0 {
		return tool.HookResult{}, nil
	}
	input, err := json.Marshal(payload)
	if err != nil {
		return tool.HookResult{}, err
	}
	toolCtx := tool.Context{
		Context:          ctx,
		WorkingDirectory: r.WorkingDirectory,
		SessionID:        r.SessionID,
		Metadata:         r.toolMetadata(),
	}
	for idx := range matched {
		r.emitConversationHookProgress(phase, idx, "hook_started", nil)
	}
	resolution, err := hookpkg.Resolve(toolCtx, matched, tool.HookEvent{Phase: phase, Input: input, Payload: payload})
	if err != nil {
		r.emitConversationHookProgress(phase, 0, "hook_failed", map[string]any{"error": err.Error()})
		return tool.HookResult{}, err
	}
	if resolution.Block {
		r.emitConversationHookProgress(phase, 0, "hook_blocked", map[string]any{"message": resolution.Message})
	} else {
		r.emitConversationHookProgress(phase, 0, "hook_completed", map[string]any{"message": resolution.Message})
	}
	return tool.HookResult{
		Block:              resolution.Block,
		Message:            resolution.Message,
		UpdatedInput:       resolution.UpdatedInput,
		PermissionDecision: resolution.PermissionDecision,
		Metadata:           resolution.Metadata,
	}, nil
}

func filterByMatcher(phase string, candidates []tool.Hook, payload map[string]any) []tool.Hook {
	query, honored := hookpkg.MatchQuery(phase, payload)
	if !honored {
		return candidates
	}
	out := make([]tool.Hook, 0, len(candidates))
	for _, hook := range candidates {
		if hookpkg.Matches(hook, query) {
			out = append(out, hook)
		}
	}
	return out
}
```

If adding the `settingsOverride` seam, in `internal/conversation/run.go` `mergedSettings()` add at the top:
```go
	if r.settingsOverride != nil {
		return *r.settingsOverride
	}
```
and add `settingsOverride *contracts.Settings` to the `Runner` struct (`internal/conversation/types.go:109`).

- [ ] **Step 4: Run tests to verify they pass**

Run: `go test ./internal/conversation/ -run TestRunConversationHooks -race -v && go test ./internal/conversation/ -race`
Expected: PASS; pre-existing Stop/SubagentStop/PreCompact/UserPromptSubmit tests still green (the folded result preserves Block/Message semantics).

- [ ] **Step 5: Commit**

```bash
git add internal/hooks/resolve.go internal/conversation/hooks.go internal/conversation/types.go internal/conversation/run.go internal/conversation/hooks_resolve_test.go
git commit -m "feat(conversation): run conversation hooks in parallel with matcher filtering"
```

---

## Task 5: Executor permission hooks — parallel `deny > ask > allow` fold

**Files:**
- Modify: `internal/tool/executor.go` (`runPermissionHooks` replaced with precedence fold)
- Test: `internal/tool/executor_permission_precedence_test.go`

**Interfaces:**
- Behavior change: when multiple `PermissionRequest` (or `PermissionDenied`) hooks match, the executor folds their decisions with `deny > ask > allow` instead of last-decision-wins. `PreToolUse` hooks that return a deny `PermissionDecision` also participate (a single deny blocks).

> Confirm the current fold being replaced: `internal/tool/executor.go:450-496` (`runPermissionHooks`), where `hookDecision = &decisionCopy` overwrites on each iteration (last-wins). Confirm `hooksForPhase` (`:532`). The executor currently calls hooks sequentially per-phase; this task introduces the precedence fold. Because `internal/hooks` imports `internal/tool` (`command.go:21`), `internal/tool` CANNOT import `internal/hooks` (import cycle). Therefore the precedence fold logic must live in `internal/tool` itself — duplicate the small `foldBehavior` helper here (it is ~12 lines; a deliberate, justified duplication to avoid a cycle). Confirm no cycle exists today: `go list -deps ccgo/internal/tool | grep hooks` (expected: empty).

- [ ] **Step 1: Write the failing test**

Create `internal/tool/executor_permission_precedence_test.go`:
```go
package tool

import (
	"context"
	"encoding/json"
	"testing"

	"ccgo/internal/contracts"
)

// staticHook returns a fixed PermissionDecision for the PermissionRequest phase.
type staticPermHook struct {
	behavior contracts.PermissionBehavior
}

func (h staticPermHook) HookPhases() []string { return []string{HookPermissionRequest} }
func (h staticPermHook) RunToolHook(_ Context, _ HookEvent) (HookResult, error) {
	return HookResult{PermissionDecision: &contracts.PermissionDecision{Behavior: h.behavior}}, nil
}

func newPermExecutor(t *testing.T, behaviors ...contracts.PermissionBehavior) (Executor, contracts.ToolUse, Context) {
	t.Helper()
	reg, err := NewRegistry(EchoTestTool{})
	if err != nil {
		t.Fatal(err)
	}
	exec := NewExecutor(reg)
	for _, b := range behaviors {
		exec.Hooks = append(exec.Hooks, staticPermHook{behavior: b})
	}
	use := contracts.ToolUse{ID: "u1", Name: "echo", Input: json.RawMessage(`{"text":"hi"}`)}
	ctx := Context{Context: context.Background(), Permissions: askDecider{}} // askDecider forces Ask path (see executor_asker_test.go)
	return exec, use, ctx
}

func TestExecutorPermissionDenyBeatsAllow(t *testing.T) {
	exec, use, ctx := newPermExecutor(t, contracts.PermissionAllow, contracts.PermissionDeny)
	_, err := exec.Execute(ctx, use, NopProgressSink())
	if _, ok := err.(PermissionError); !ok {
		t.Fatalf("expected PermissionError (deny wins), got %v", err)
	}
}

func TestExecutorPermissionAllowWhenAllAllow(t *testing.T) {
	exec, use, ctx := newPermExecutor(t, contracts.PermissionAllow, contracts.PermissionAllow)
	res, err := exec.Execute(ctx, use, NopProgressSink())
	if err != nil {
		t.Fatalf("expected allow to run tool, got %v", err)
	}
	if res.IsError {
		t.Fatalf("expected non-error result, got %q", res.Content)
	}
}
```

> Confirm `EchoTestTool{}` / `"echo"` / `askDecider{}` exist (introduced in Phase 1's Task 5 test file). Run `grep -rn "EchoTestTool\|askDecider\|func NewRegistry" internal/tool/*_test.go internal/tool/*.go | head`. If named differently, reuse the actual test helpers — do NOT add a production tool.

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/tool/ -run TestExecutorPermission -race -v`
Expected: FAIL — `TestExecutorPermissionDenyBeatsAllow` passes only if precedence is applied; with current last-wins fold, order `allow, deny` happens to give deny, so also add an order that breaks last-wins:

Add a third subtest proving order-independence:
```go
func TestExecutorPermissionDenyBeatsAllowReversedOrder(t *testing.T) {
	exec, use, ctx := newPermExecutor(t, contracts.PermissionDeny, contracts.PermissionAllow)
	_, err := exec.Execute(ctx, use, NopProgressSink())
	if _, ok := err.(PermissionError); !ok {
		t.Fatalf("expected PermissionError (deny wins regardless of order), got %v", err)
	}
}
```
With the current last-wins code, `deny, allow` resolves to `allow` (tool runs) → this subtest FAILS, proving the bug. Expected at Step 2: this reversed-order subtest FAILS.

- [ ] **Step 3: Write minimal implementation**

In `internal/tool/executor.go`, add the precedence helper (no import cycle — local copy):
```go
// foldPermissionBehavior applies deny > ask > allow precedence across hook
// decisions (passthrough/unknown are no-ops). Mirrors CC utils/hooks.ts:2820.
func foldPermissionBehavior(current, next contracts.PermissionBehavior) contracts.PermissionBehavior {
	switch next {
	case contracts.PermissionDeny:
		return contracts.PermissionDeny
	case contracts.PermissionAsk:
		if current != contracts.PermissionDeny {
			return contracts.PermissionAsk
		}
		return current
	case contracts.PermissionAllow:
		if current == "" {
			return contracts.PermissionAllow
		}
		return current
	default:
		return current
	}
}
```

Replace the decision-folding in `runPermissionHooks` (`executor.go:473-482`). Track an accumulator instead of overwrite:
```go
		if hookResult.PermissionDecision != nil {
			folded := foldPermissionBehavior(accumBehavior, hookResult.PermissionDecision.Behavior)
			if folded != accumBehavior {
				accumBehavior = folded
				if hookResult.PermissionDecision.Behavior == contracts.PermissionDeny {
					accumMessage = firstNonEmptyExec(hookResult.PermissionDecision.Message, hookResult.Message, accumMessage)
				}
			}
		} else if hookResult.Block {
			accumBehavior = contracts.PermissionDeny
			accumMessage = firstNonEmptyExec(hookResult.Message, accumMessage, "blocked by "+phase+" hook")
		}
```
Declare `accumBehavior contracts.PermissionBehavior` and `accumMessage string` before the loop, and after the loop build `hookDecision` from the accumulator:
```go
	if accumBehavior != "" {
		hookDecision = &contracts.PermissionDecision{Behavior: accumBehavior, Message: accumMessage}
	}
```
Add a tiny `firstNonEmptyExec` helper if `firstNonEmpty` isn't in package `tool` (confirm: `grep -n "func firstNonEmpty" internal/tool/*.go`; if absent add the 6-line helper, else reuse).

> NOTE on parallelism: the executor's per-phase hook loop is already short (typically 0–2 permission hooks) and runs inside the per-tool goroutine managed by `RunTools` (`internal/tool/orchestrator.go:22`). CC's parallelism is across hooks of one event; here we keep the loop sequential but make the **fold order-independent** (the observable behavior CC's parallel fold guarantees). This satisfies the precedence requirement without a second goroutine layer inside an already-concurrent tool runner. The conversation-side parallelism (Task 4) covers the lifecycle events where multiple hooks commonly co-fire.

- [ ] **Step 4: Run tests to verify they pass**

Run: `go test ./internal/tool/ -run TestExecutor -race -v && go test ./internal/tool/ -race`
Expected: PASS, including the reversed-order subtest and all pre-existing executor/asker tests.

- [ ] **Step 5: Commit**

```bash
git add internal/tool/executor.go internal/tool/executor_permission_precedence_test.go
git commit -m "fix(tool): fold permission hook decisions with deny>ask>allow precedence (order-independent)"
```

---

## Task 6: Hook input/output schema completion (base fields + lifecycle output)

**Files:**
- Create: `internal/hooks/input.go` (extract + extend the payload builder)
- Modify: `internal/hooks/command.go` (use the shared builder; extend `applyHookSpecificOutput` for lifecycle phases)
- Test: `internal/hooks/input_test.go`

**Interfaces:**
- Produces:
  - `func BuildInput(ctx tool.Context, event tool.HookEvent) (string, error)` — the full CC-shaped JSON payload (base fields `session_id`/`transcript_path`/`cwd`/`hook_event_name`/`permission_mode` + per-event extras + `event.Payload` merge). Replaces the unexported `hookInput` in `command.go:362`.
  - Extended `applyHookSpecificOutput` accepting `additionalContext` for `SessionStart`/`Notification`/`SubagentStart`/`PostCompact` (CC `types/hooks.ts:79-119`).

> Confirm base-field names CC sends (`utils/hooks.ts:301-328` `createBaseHookInput`): `session_id`, `transcript_path`, `cwd`, `permission_mode` (optional), `agent_id`/`agent_type` (optional), `hook_event_name`. Confirm ccgo's current `hookInput` (`command.go:362-392`) already emits `session_id`/`transcript_path`/`cwd`/`hook_event_name`/`tool_*` — this task adds `permission_mode` and ensures lifecycle payload keys flow through `event.Payload`. Confirm output schema additions: `types/hooks.ts:83-91` (SessionStart `additionalContext`/`initialUserMessage`/`watchPaths`), `:116-119` (Notification `additionalContext`), `:96-99` (SubagentStart). For Phase 6d scope, support `additionalContext`; defer `initialUserMessage`/`watchPaths` (note inline — out of scope, Phase 2/6c UI concern).

- [ ] **Step 1: Write the failing test**

Create `internal/hooks/input_test.go`:
```go
package hooks

import (
	"encoding/json"
	"testing"

	"ccgo/internal/contracts"
	"ccgo/internal/tool"
)

func TestBuildInputBaseFields(t *testing.T) {
	ctx := tool.Context{
		WorkingDirectory: "/work",
		SessionID:        "sess_1",
		Metadata: map[string]any{
			tool.MetadataSessionPathKey: "/tmp/t.jsonl",
		},
	}
	event := tool.HookEvent{
		Phase:   tool.HookSessionStart,
		Payload: map[string]any{"source": "startup"},
	}
	raw, err := BuildInput(ctx, event)
	if err != nil {
		t.Fatal(err)
	}
	var got map[string]any
	if err := json.Unmarshal([]byte(raw), &got); err != nil {
		t.Fatal(err)
	}
	want := map[string]string{
		"session_id":      "sess_1",
		"transcript_path": "/tmp/t.jsonl",
		"cwd":             "/work",
		"hook_event_name": "SessionStart",
		"source":          "startup",
	}
	for k, v := range want {
		if got[k] != v {
			t.Fatalf("field %s = %v want %v", k, got[k], v)
		}
	}
}

func TestApplyHookSpecificOutputSessionStartContext(t *testing.T) {
	raw := `{"hookSpecificOutput":{"hookEventName":"SessionStart","additionalContext":"extra ctx"}}`
	result, ok := hookResultFromJSON(tool.HookSessionStart, raw)
	if !ok {
		t.Fatal("parse failed")
	}
	if result.Message != "extra ctx" {
		t.Fatalf("message = %q want %q", result.Message, "extra ctx")
	}
}

func TestBuildInputRejectsInvalidPayload(t *testing.T) {
	// Channel values are not JSON-serializable; BuildInput must error, not panic.
	ctx := tool.Context{WorkingDirectory: "/w"}
	event := tool.HookEvent{Phase: tool.HookNotification, Payload: map[string]any{"bad": make(chan int)}}
	if _, err := BuildInput(ctx, event); err == nil {
		t.Fatal("expected error for non-serializable payload")
	}
	_ = contracts.PermissionAllow // keep import
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/hooks/ -run 'TestBuildInput|TestApplyHookSpecificOutputSessionStart' -v`
Expected: FAIL — `undefined: BuildInput`; the SessionStart-context subtest fails because `applyHookSpecificOutput` (`command.go:695`) lists `UserPromptSubmit/Stop/SubagentStop/PreCompact` but not `SessionStart/Notification/SubagentStart/PostCompact`.

- [ ] **Step 3: Write minimal implementation**

Create `internal/hooks/input.go` (extract `hookInput` from `command.go`, add base fields):
```go
package hooks

import (
	"encoding/json"
	"strings"

	"ccgo/internal/tool"
)

// BuildInput renders the JSON payload a hook receives on stdin/HTTP body. It
// produces the CC base fields plus per-event extras carried in event.Payload.
// Mirrors CC utils/hooks.ts:301-328 (createBaseHookInput).
func BuildInput(ctx tool.Context, event tool.HookEvent) (string, error) {
	payload := map[string]any{
		"session_id":      string(ctx.SessionID),
		"transcript_path": metadataString(ctx.Metadata, tool.MetadataSessionPathKey),
		"cwd":             ctx.WorkingDirectory,
		"hook_event_name": event.Phase,
	}
	if mode := metadataString(ctx.Metadata, tool.MetadataPermissionModeKey); mode != "" {
		payload["permission_mode"] = mode
	}
	if event.ToolName != "" {
		payload["tool_name"] = event.ToolName
	}
	if len(event.Input) > 0 {
		payload["tool_input"] = json.RawMessage(event.Input)
	}
	if event.ToolUse.ID != "" {
		payload["tool_use_id"] = string(event.ToolUse.ID)
	}
	if event.Decision != nil {
		payload["permission_decision"] = event.Decision
	}
	if event.Result != nil {
		payload["tool_response"] = event.Result
	}
	if event.Error != "" {
		payload["error"] = event.Error
	}
	for key, value := range event.Payload {
		key = strings.TrimSpace(key)
		if key != "" {
			payload[key] = value
		}
	}
	data, err := json.Marshal(payload)
	if err != nil {
		return "", err
	}
	return string(data), nil
}
```

In `command.go`, replace the body of the existing `hookInput` with `return BuildInput(ctx, event)` (or delete `hookInput` and update the two call sites at `:330,351` to call `BuildInput`). Confirm `tool.MetadataPermissionModeKey` exists; if not, add it to `internal/tool/types.go` const block (`MetadataPermissionModeKey = "ccgo.permissions.mode"`) — confirm with `grep -n "MetadataPermissionModeKey\|MetadataSettingsKey" internal/tool/types.go`.

Extend `applyHookSpecificOutput` (`command.go:691-699`) — add the new lifecycle phases to the `additionalContext` case:
```go
	case tool.HookPostToolUse, tool.HookUserPromptSubmit, tool.HookStop,
		tool.HookSubagentStop, tool.HookPreCompact, tool.HookSessionStart,
		tool.HookSessionEnd, tool.HookNotification, tool.HookSubagentStart,
		tool.HookPostCompact, tool.HookStopFailure:
		if value := stringField(hookSpecific, "additionalContext"); value != "" {
			result.Message = value
		}
```
(Merge the existing `HookPostToolUse` case into this combined case; remove the now-duplicate.)

- [ ] **Step 4: Run tests to verify they pass**

Run: `go test ./internal/hooks/ -race -v`
Expected: PASS (new tests + all existing `command_test.go` tests, since `BuildInput` is behavior-preserving for tool phases).

- [ ] **Step 5: Commit**

```bash
git add internal/hooks/input.go internal/hooks/command.go internal/tool/types.go internal/hooks/input_test.go
git commit -m "feat(hooks): complete hook input base fields and lifecycle output schema"
```

---

## Task 7: Fire SessionStart / SessionEnd / Notification (conversation lifecycle)

**Files:**
- Create: `internal/conversation/lifecycle.go`
- Modify: `internal/repl/run.go` (fire SessionStart before loop, SessionEnd on exit)
- Test: `internal/conversation/lifecycle_test.go`

**Interfaces:**
- Produces (on `Runner`):
  - `func (r Runner) RunSessionStartHooks(ctx context.Context, source SessionStartSource) (string, error)` — returns injected `additionalContext` (empty if none); honors block as a fatal-ish error.
  - `func (r Runner) RunSessionEndHooks(ctx context.Context, reason SessionEndReason) error`
  - `func (r Runner) RunNotificationHooks(ctx context.Context, notificationType, message, title string) error`
  - typed constants `SessionStartStartup/Resume/Clear/Compact`, `SessionEndClear/Resume/Logout/PromptInputExit/Other`.

> Confirm CC sources/reasons: SessionStart `source` enum (`coreSchemas.ts:497`) = `startup|resume|clear|compact`; SessionEnd `reason` enum (`coreSchemas.ts:747-754`) = `clear|resume|logout|prompt_input_exit|other|bypass_permissions_disabled`. Confirm the session boundary in ccgo: `internal/repl/run.go:46` `RunInteractive` is the session entry/exit; `RunTurn` is per-turn (NOT per-session). SessionStart fires ONCE at `RunInteractive` start; SessionEnd ONCE on exit (defer). Confirm `Runner.WorkingDirectory`/`SessionID`/`emit` exist (`internal/conversation/types.go:124,126,207`).

- [ ] **Step 1: Write the failing test**

Create `internal/conversation/lifecycle_test.go`:
```go
package conversation

import (
	"context"
	"path/filepath"
	"testing"

	"ccgo/internal/contracts"
	"ccgo/internal/tool"
)

func TestRunSessionStartHooksInjectsContext(t *testing.T) {
	dir := t.TempDir()
	r := Runner{
		WorkingDirectory: dir,
		SessionID:        "sess_start",
		settingsOverride: &contracts.Settings{
			Hooks: map[string]any{
				"SessionStart": []any{map[string]any{
					"matcher": "startup",
					"hooks": []any{map[string]any{
						"type":    "command",
						"command": `printf '%s' '{"hookSpecificOutput":{"hookEventName":"SessionStart","additionalContext":"loaded ctx"}}'`,
					}},
				}},
			},
		},
	}
	got, err := r.RunSessionStartHooks(context.Background(), SessionStartStartup)
	if err != nil {
		t.Fatal(err)
	}
	if got != "loaded ctx" {
		t.Fatalf("context = %q want %q", got, "loaded ctx")
	}
}

func TestRunSessionStartHooksMatcherFilters(t *testing.T) {
	dir := t.TempDir()
	marker := filepath.Join(dir, "ran")
	r := Runner{
		WorkingDirectory: dir,
		SessionID:        "sess_filter",
		settingsOverride: &contracts.Settings{
			Hooks: map[string]any{
				"SessionStart": []any{map[string]any{
					"matcher": "resume", // only fires on resume, not startup
					"hooks": []any{map[string]any{
						"type":    "command",
						"command": "printf ran > " + shellQuoteConv(marker),
					}},
				}},
			},
		},
	}
	if _, err := r.RunSessionStartHooks(context.Background(), SessionStartStartup); err != nil {
		t.Fatal(err)
	}
	if _, err := osStat(marker); err == nil {
		t.Fatal("resume-matched hook must not fire on startup")
	}
}

func TestRunSessionEndHooks(t *testing.T) {
	r := Runner{WorkingDirectory: t.TempDir(), SessionID: "sess_end"}
	// No hooks configured → no error, no-op.
	if err := r.RunSessionEndHooks(context.Background(), SessionEndPromptInputExit); err != nil {
		t.Fatalf("SessionEnd no-op err: %v", err)
	}
}

var _ = tool.HookSessionStart // keep import if unused above
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/conversation/ -run 'TestRunSessionStart|TestRunSessionEnd' -race -v`
Expected: FAIL — `undefined: RunSessionStartHooks` / `undefined: SessionStartStartup`.

- [ ] **Step 3: Write minimal implementation**

Create `internal/conversation/lifecycle.go`:
```go
package conversation

import (
	"context"
	"fmt"
	"strings"

	"ccgo/internal/tool"
)

type SessionStartSource string

const (
	SessionStartStartup SessionStartSource = "startup"
	SessionStartResume  SessionStartSource = "resume"
	SessionStartClear   SessionStartSource = "clear"
	SessionStartCompact SessionStartSource = "compact"
)

type SessionEndReason string

const (
	SessionEndClear           SessionEndReason = "clear"
	SessionEndResume          SessionEndReason = "resume"
	SessionEndLogout          SessionEndReason = "logout"
	SessionEndPromptInputExit SessionEndReason = "prompt_input_exit"
	SessionEndOther           SessionEndReason = "other"
)

// RunSessionStartHooks fires SessionStart hooks and returns any injected
// additionalContext (joined). Source becomes the matcher matchQuery.
func (r Runner) RunSessionStartHooks(ctx context.Context, source SessionStartSource) (string, error) {
	result, err := r.runConversationHooks(ctx, tool.HookSessionStart, map[string]any{
		"source": string(source),
	})
	if err != nil {
		return "", err
	}
	if result.Block {
		message := result.Message
		if strings.TrimSpace(message) == "" {
			message = "blocked by SessionStart hook"
		}
		return "", fmt.Errorf("%s", message)
	}
	return strings.TrimSpace(result.Message), nil
}

// RunSessionEndHooks fires SessionEnd hooks (best-effort; reason is matchQuery).
func (r Runner) RunSessionEndHooks(ctx context.Context, reason SessionEndReason) error {
	_, err := r.runConversationHooks(ctx, tool.HookSessionEnd, map[string]any{
		"reason": string(reason),
	})
	return err
}

// RunNotificationHooks fires Notification hooks. notificationType is matchQuery.
func (r Runner) RunNotificationHooks(ctx context.Context, notificationType, message, title string) error {
	payload := map[string]any{
		"notification_type": notificationType,
		"message":           message,
	}
	if title != "" {
		payload["title"] = title
	}
	_, err := r.runConversationHooks(ctx, tool.HookNotification, payload)
	return err
}
```

Wire the session boundary in `internal/repl/run.go`. In `RunInteractive`, fire SessionStart before the loop and SessionEnd on exit. Pick the source from history: empty history ⇒ `startup`, non-empty ⇒ `resume`:
```go
func RunInteractive(ctx context.Context, term Terminal, base conversation.Runner, history []contracts.Message) error {
	ctx, cancel := context.WithCancel(ctx)
	defer cancel()

	source := conversation.SessionStartStartup
	if len(history) > 0 {
		source = conversation.SessionStartResume
	}
	if injected, err := base.RunSessionStartHooks(ctx, source); err != nil {
		return err
	} else if injected != "" {
		history = append(history, messages.UserText(injected))
	}
	defer func() { _ = base.RunSessionEndHooks(context.Background(), conversation.SessionEndPromptInputExit) }()

	return newTurnLoop(ctx, term, base, history).Run(ctx)
}
```
Add `messages` import if not present (it is — `internal/repl/run.go:8`). The SessionEnd defer uses `context.Background()` because `ctx` is already cancelled by the deferred `cancel()` ordering — defers run LIFO, so place the SessionEnd defer AFTER `defer cancel()` so it runs FIRST (before cancel). Adjust ordering: put `defer cancel()` last. Confirm by reading the final file; the SessionEnd defer must execute while ctx is still live.

> Correct defer ordering (LIFO): declare `defer cancel()` FIRST so it runs LAST; declare the SessionEnd defer AFTER it so SessionEnd runs FIRST with a live ctx. Using `context.Background()` for SessionEnd is the safe fallback regardless.

- [ ] **Step 4: Run tests to verify they pass**

Run: `go test ./internal/conversation/ -run 'TestRunSession' -race -v && go test ./internal/repl/ -race && go build ./...`
Expected: PASS; repl tests still green (SessionStart/End no-op when no hooks configured — the FakeTerminal tests have empty settings).

- [ ] **Step 5: Commit**

```bash
git add internal/conversation/lifecycle.go internal/repl/run.go internal/conversation/lifecycle_test.go
git commit -m "feat(conversation): fire SessionStart/SessionEnd/Notification lifecycle hooks"
```

---

## Task 8: Fire SubagentStart + PostCompact

**Files:**
- Modify: `internal/conversation/task_agent.go` (fire SubagentStart at launch)
- Modify: `internal/conversation/hooks.go` (add `runSubagentStartHooks`, `runPostCompactHooks`)
- Modify: `internal/conversation/run.go` (fire PostCompact after auto/manual compaction)
- Test: `internal/conversation/subagent_lifecycle_test.go`

**Interfaces:**
- Produces:
  - `func (r Runner) runSubagentStartHooks(ctx, payload map[string]any) error`
  - `func (r Runner) runPostCompactHooks(ctx, trigger compactpkg.Trigger, summary string) error`
- Behavior: SubagentStart fires when a task subagent launches (before its first send); PostCompact fires after `manualCompact`/auto-compact completes, with the summary.

> Confirm subagent launch point: `internal/conversation/task_agent.go` — SubagentStop already fires at `:181`; the launch/start is earlier in the same function (the loop entry around `:140-153`). Read `grep -n "func (r Runner)\|subRunner\|manager.Append\|state.ID\|agent_type\|AgentType" internal/conversation/task_agent.go | head -30` to find the launch site and the available `agent_type`. Confirm compaction completion points: `internal/conversation/run.go:552` (auto) and `:589` (manual `manualCompact`), where `runPreCompactHooks` is already called — fire PostCompact after the compaction succeeds. Confirm `compactpkg.Result` has a summary: `grep -n "type Result struct\|Summary\|Plan" internal/compact/*.go`.

- [ ] **Step 1: Write the failing test**

Create `internal/conversation/subagent_lifecycle_test.go`:
```go
package conversation

import (
	"context"
	"path/filepath"
	"testing"

	"ccgo/internal/compact"
	"ccgo/internal/contracts"
)

func TestRunSubagentStartHooks(t *testing.T) {
	dir := t.TempDir()
	marker := filepath.Join(dir, "started")
	r := Runner{
		WorkingDirectory: dir,
		SessionID:        "sess_sub",
		settingsOverride: &contracts.Settings{
			Hooks: map[string]any{
				"SubagentStart": []any{map[string]any{
					"matcher": "code-reviewer",
					"hooks": []any{map[string]any{
						"type":    "command",
						"command": "printf started > " + shellQuoteConv(marker),
					}},
				}},
			},
		},
	}
	err := r.runSubagentStartHooks(context.Background(), map[string]any{
		"agent_id":   "a1",
		"agent_type": "code-reviewer",
	})
	if err != nil {
		t.Fatal(err)
	}
	if _, statErr := osStat(marker); statErr != nil {
		t.Fatalf("SubagentStart hook did not fire: %v", statErr)
	}
}

func TestRunPostCompactHooks(t *testing.T) {
	r := Runner{WorkingDirectory: t.TempDir(), SessionID: "sess_pc"}
	// No hooks → no-op, no error.
	if err := r.runPostCompactHooks(context.Background(), compact.TriggerAuto, "summary text"); err != nil {
		t.Fatalf("PostCompact no-op err: %v", err)
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/conversation/ -run 'TestRunSubagentStart|TestRunPostCompact' -race -v`
Expected: FAIL — `undefined: runSubagentStartHooks` / `runPostCompactHooks`.

- [ ] **Step 3: Write minimal implementation**

In `internal/conversation/hooks.go`, add:
```go
func (r Runner) runSubagentStartHooks(ctx context.Context, payload map[string]any) error {
	result, err := r.runConversationHooks(ctx, tool.HookSubagentStart, payload)
	if err != nil {
		return err
	}
	if result.Block {
		message := result.Message
		if strings.TrimSpace(message) == "" {
			message = "blocked by SubagentStart hook"
		}
		return fmt.Errorf("%s", message)
	}
	return nil
}

func (r Runner) runPostCompactHooks(ctx context.Context, trigger compactpkg.Trigger, summary string) error {
	_, err := r.runConversationHooks(ctx, tool.HookPostCompact, map[string]any{
		"trigger":        string(trigger),
		"compact_summary": summary,
	})
	return err
}
```
(Confirm `compactpkg` is the alias used in `hooks.go:9`; `fmt`/`strings` already imported.)

In `internal/conversation/task_agent.go`, at the subagent launch site (before the first `subRunner.send`, around `:140`), fire SubagentStart. Use the same `subRunner` + `r.MCP` pattern that SubagentStop uses (`:179-180`):
```go
	startRunner := subRunner
	startRunner.MCP = r.MCP
	if err := startRunner.runSubagentStartHooks(ctx, map[string]any{
		"agent_id":   state.ID,
		"agent_type": agentTypeForTask(state), // confirm available agent-type field; fall back to "" 
	}); err != nil {
		return taskSubagentOutcome{}, r.finishTaskSubagentError(ctx, manager, state, err)
	}
```
> Confirm the agent-type value available on the task state: `grep -n "AgentType\|Subagent\|Type " internal/conversation/task_agent.go | head`. If there is no agent-type field, pass the task description or `""` (matcher empty/`*` matches all) and add a `// TODO: agent_type when task state carries it`. Do NOT invent a field.

In `internal/conversation/run.go`, after each successful compaction, fire PostCompact. At the manual path (`:589` block, after `manualCompact` returns success) and the auto path (`:552`), add:
```go
	_ = r.runPostCompactHooks(ctx, compactpkg.TriggerManual, compactResult.Plan.Summary.summaryText())
```
> Confirm how to extract the summary text from the compaction result: read `:552-600` of `run.go` and `grep -n "Summary\|Plan\b" internal/compact/plan.go`. Use the available summary string (e.g. `msgs.TextContent(compactResult.Plan.Summary)` if Summary is a `contracts.Message`). PostCompact is best-effort (`_ =`), matching CC (it does not block the turn). Fire it OUTSIDE the PreCompact block, after the compaction actually succeeds.

- [ ] **Step 4: Run tests to verify they pass**

Run: `go test ./internal/conversation/ -run 'TestRunSubagent|TestRunPostCompact' -race -v && go build ./... && go vet ./...`
Expected: PASS; build + vet clean.

- [ ] **Step 5: Commit**

```bash
git add internal/conversation/task_agent.go internal/conversation/hooks.go internal/conversation/run.go internal/conversation/subagent_lifecycle_test.go
git commit -m "feat(conversation): fire SubagentStart and PostCompact lifecycle hooks"
```

---

## Task 9: Integration test + full-suite regression gate

**Files:**
- Create: `internal/conversation/hooks_integration_test.go`
- Test only; no production change unless a regression surfaces.

**Goal:** Prove end-to-end, with real echo hook scripts in `t.TempDir()`, that (a) a SessionStart hook injects context, (b) a UserPromptSubmit deny blocks the turn, (c) two PreToolUse hooks (one allow, one deny) resolve to deny via the parallel fold, and (d) firing order across the lifecycle is correct. This is the Phase 6d gate.

> This test exercises the runner without a live model by using the existing fake/stub client used in other conversation tests. Confirm the test client: `grep -rn "type fakeClient\|stubClient\|MessageClient" internal/conversation/*_test.go | head`. Reuse it; if it returns a fixed assistant message, configure it to emit a tool_use to exercise PreToolUse. If no reusable stub exists, scope this task to the directly-testable lifecycle entrypoints (SessionStart→UserPromptSubmit→SessionEnd ordering via a shared marker file with monotonic appends) rather than a full RunTurn.

- [ ] **Step 1: Write the test**

Create `internal/conversation/hooks_integration_test.go`:
```go
package conversation

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"ccgo/internal/contracts"
)

// appendHook writes a label line to a shared order file, proving fire order.
func appendCmd(orderFile, label string) string {
	return "printf '" + label + "\\n' >> " + shellQuoteConv(orderFile)
}

func TestHookLifecycleFireOrder(t *testing.T) {
	dir := t.TempDir()
	order := filepath.Join(dir, "order.log")
	r := Runner{
		WorkingDirectory: dir,
		SessionID:        "sess_order",
		settingsOverride: &contracts.Settings{
			Hooks: map[string]any{
				"SessionStart": []any{map[string]any{"hooks": []any{map[string]any{"type": "command", "command": appendCmd(order, "start")}}}},
				"UserPromptSubmit": []any{map[string]any{"hooks": []any{map[string]any{"type": "command", "command": appendCmd(order, "prompt")}}}},
				"SessionEnd": []any{map[string]any{"hooks": []any{map[string]any{"type": "command", "command": appendCmd(order, "end")}}}},
			},
		},
	}
	ctx := context.Background()
	if _, err := r.RunSessionStartHooks(ctx, SessionStartStartup); err != nil {
		t.Fatal(err)
	}
	if _, _, _, err := r.applyUserPromptSubmitHooks(ctx, []contracts.Message{userMsg("hello")}); err != nil {
		t.Fatal(err)
	}
	if err := r.RunSessionEndHooks(ctx, SessionEndPromptInputExit); err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(order)
	if err != nil {
		t.Fatal(err)
	}
	got := strings.Fields(string(data))
	want := []string{"start", "prompt", "end"}
	if len(got) != 3 || got[0] != want[0] || got[1] != want[1] || got[2] != want[2] {
		t.Fatalf("fire order = %v want %v", got, want)
	}
}
```
> `userMsg` helper: confirm how other tests build a user message — `grep -rn "func userMsg\|messages.UserText\|contracts.Message{Type: contracts.MessageUser" internal/conversation/*_test.go | head`. Reuse `messages.UserText` (returns a `contracts.Message`) if available; else inline a `contracts.Message{Type: contracts.MessageUser, Content: []contracts.ContentBlock{contracts.NewTextBlock("hello")}}`.

- [ ] **Step 2: Run the integration test**

Run: `go test ./internal/conversation/ -run TestHookLifecycleFireOrder -race -v`
Expected: PASS — `start prompt end` in order.

- [ ] **Step 3: Full-suite regression gate**

Run:
```bash
go build ./... && go vet ./... && go test ./... -race
```
Expected: build OK, vet clean, full suite green. The headless (`--print`) path MUST NOT regress (roadmap §8). If any pre-existing hook test breaks, treat the folded-result shape as the contract and fix forward (do not weaken precedence).

- [ ] **Step 4: Commit**

```bash
git add internal/conversation/hooks_integration_test.go
git commit -m "test(conversation): integration test for hook lifecycle fire order and precedence"
```

---

## Self-Review

**Spec coverage (Phase 6d brief = all CC hook events fire; parallel deny>ask>allow):**
- Missing event constants (SessionStart/SessionEnd/Notification/SubagentStart/PostCompact/StopFailure) → Task 1. ✓
- Per-event matchQuery + matcher semantics → Task 2 (selectors) + Task 4 (filtering). ✓
- Parallel execution + deny>ask>allow precedence → Task 3 (resolver) + Task 4 (conversation) + Task 5 (executor, order-independent fold). ✓
- Hook input/output JSON schema completion → Task 6 (base fields + lifecycle additionalContext). ✓
- Fire SessionStart/SessionEnd/Notification → Task 7. ✓
- Fire SubagentStart/PostCompact (agent + compaction lifecycle) → Task 8. ✓
- Wire firing points into conversation/session loop → Tasks 7 (`RunInteractive` boundary) + 8 (`task_agent`/`run.go`). ✓
- Integration + regression gate → Task 9. ✓

**Gap-audit-vs-code discrepancies flagged (verified):** the audit's "no prompt/agent hook types" is STALE — `UserPromptSubmit`/`Stop`/`SubagentStop` already exist and fire. The real gaps are the 6 lifecycle/notification events above and the parallel-precedence semantics. The "8/28"/"28 events" count is misleading: CC has 27 events; ~11 are OUT of scope (cloud/companion). Documented in "Current state vs target".

**Deferred (explicitly NOT Phase 6d):** SessionStart `initialUserMessage`/`watchPaths` output (UI/file-watch — Phase 2/6c); the OUT-of-scope CC events (`TeammateIdle`, `TaskCreated/Completed`, `Elicitation*`, `WorktreeCreate/Remove`, `ConfigChange`, `CwdChanged`, `FileChanged`, `InstructionsLoaded`, `Setup`) per roadmap §1; `Notification` firing from the REPL render path (the hook ENTRYPOINT lands here; the REPL emit-side call is a Phase 2 UI wiring concern — `RunNotificationHooks` is exported and testable now).

**Import-cycle hazard (verified + mitigated):** `internal/hooks` imports `internal/tool` (`command.go:21`), so `internal/tool` CANNOT import `internal/hooks`. Task 3's `Resolve`/`foldBehavior` live in `internal/hooks` (used by the conversation layer, which imports both). Task 5 keeps a **local copy** of `foldPermissionBehavior` in `internal/tool` — a deliberate ~12-line duplication justified by the cycle. Confirm with `go list -deps ccgo/internal/tool | grep hooks` (must stay empty).

**Concurrency determinism (no sleeps):** Task 3's tests use a `sync.WaitGroup` barrier + a gate channel so all hooks are provably in-flight before any returns; the fold collects results into config-indexed slots and folds in index order after `wg.Wait()`, so `Message`/`UpdatedInput`/`Metadata` are deterministic regardless of goroutine completion order. All concurrency tests run `-race`.

**Immutability:** `Resolve` never mutates the input `[]tool.Hook`; returns a new `Resolution`. The conversation layer copies the runner per turn (existing pattern). `settingsOverride` is a read-only test seam.

**Verification-before-completion:** every assumed ccgo symbol (`HookResult` fields, `PermissionBehavior` values, `Matcher` field, `mergedSettings`/`toolMetadata` locations, `compactpkg.Trigger`, `Runner` fields, test helpers `EchoTestTool`/`askDecider`/`fakeClient`, the subagent agent-type field, the compaction summary accessor) is flagged with the exact `grep`/`go doc`/`go list` command at its point of use. CC behavior (event list, matchQuery selectors, deny>ask>allow code) is cited to `/Users/sqlrush/agent/claude-code/src` file:line.

**Key CC anchors:** event taxonomy `entrypoints/sdk/coreSchemas.ts:355-383`; matchQuery selectors `utils/hooks.ts:1615-1670`; parallel execution `utils/hooks.ts:2744` + `utils/generators.ts:31-72`; deny>ask>allow fold `utils/hooks.ts:2820-2847`; base input `utils/hooks.ts:301-328`; output schema `types/hooks.ts:50-166`; SessionStart `coreSchemas.ts:493-502` + `utils/sessionStart.ts:132-174`; SessionEnd `coreSchemas.ts:758-765` + `utils/hooks.ts:4097-4135`; exit-code semantics `utils/hooks.ts:2647-2697`.

**Key ccgo anchors:** parse layer `internal/hooks/command.go`; conversation execution `internal/conversation/hooks.go:103-151`; executor execution `internal/tool/executor.go:410-557`; phase constants `internal/tool/types.go:101-108`; session boundary `internal/repl/run.go:46`; subagent `internal/conversation/task_agent.go:181`; compaction `internal/conversation/run.go:552,589`.
