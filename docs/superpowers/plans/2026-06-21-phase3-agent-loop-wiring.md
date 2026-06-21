# Agent-Loop Wiring (Phase 3) Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Wire ccgo's existing-but-dead agent-loop machinery (prompt-cache breakpoints, extended thinking, stop-reason control flow, orphaned-tool-result injection, micro-compaction) into the live `conversation.Runner` request/response path so streaming behaves like the standard Anthropic API: cache hits land, thinking is collected (with signature), max-tokens/refusal/ctx-window/pause-turn are handled, mid-turn bail never 400s the next request, and per-turn micro-compaction runs.

**Architecture:** All work lives in `internal/conversation/` (the runner + accumulator surface) and `internal/api/anthropic/` (the cache/header/accumulator primitives). The runner builds its request in `Runner.buildRequest` (`internal/conversation/request.go:39`) and runs the model turn loop in `Runner.RunTurn` (`internal/conversation/run.go:205-256`). Phase 3 inserts: (1) a `AddCacheBreakpoints` call inside `buildRequest` gated on a new `EnablePromptCaching` runner flag, plus a corrected cache-scope beta header constant; (2) `Request.Thinking` population in `buildRequest` from the model registry's `SupportsThinking`/`AlwaysOnThinking` capability, an added `ContentBlock.Signature` field, and `thinking_delta`/`signature_delta` handling in `StreamAccumulator.Add`; (3) a `stop_reason` switch evaluated after each `runner.send` in `RunTurn` that recovers max_tokens, resumes pause_turn, surfaces refusal, and recovers `model_context_window_exceeded`; (4) injection of synthetic `is_error` tool_results for any orphaned `tool_use` when the turn bails mid-execution; (5) a `runner.maybeMicroCompact` step run before `maybeAutoCompact`, reusing the deterministic `compact.MicroCompact*` functions. Each task copies the `Runner` per turn (never mutates the shared base) and returns new message slices.

**Tech Stack:** Go 1.26; existing packages `internal/conversation`, `internal/api/anthropic`, `internal/contracts`, `internal/compact`, `internal/model`, `internal/messages`. No new third-party deps.

## Global Constraints

Copied verbatim from the master roadmap §6 (apply to EVERY task):

- **Module/toolchain:** `ccgo`, `go 1.26` (from `go.mod`).
- **Immutability (CRITICAL):** never mutate shared structs in place; return new copies. Copy the `conversation.Runner` value per turn before setting `OnEvent`/`Tools.Asker` (existing pattern). `permissions.Engine.ApplyUpdate` already returns a **new** engine — honor that.
- **Many small files:** one responsibility per file; target 150–350 lines (800 hard max).
- **Errors handled explicitly at every level; never swallow.** Terminal raw-mode `restore` and any acquired resource MUST be released on every exit path (`defer`).
- **Input validation at boundaries:** validate all external data (API responses, user input, file content, MCP server output); fail fast with clear messages.
- **No new third-party deps** unless the plan justifies it explicitly. Phase 1 added only `golang.org/x/term`. No bubbletea/tcell/charm.
- **Non-TTY safety:** interactive paths MUST NOT call `term.MakeRaw` when stdin/stdout isn't a tty; fall back to line mode. Tests MUST NOT depend on a real tty.
- **TDD:** every task writes a failing test first, then minimal code. Commit after each task. Run package tests with `go test ./internal/<pkg>/ -run TestName -v`; full suite `go test ./...`.
- **Verify against real code, distrust roadmap docs:** every assumed type name, field, constant, or CC behavior MUST be confirmed with `go doc`/`grep` (ccgo side) or by reading `/Users/sqlrush/agent/claude-code/src` (CC side) before writing the test — flag the exact command at the point of use, as Phase 1's plan does.
- **Security:** no hardcoded secrets; tokens in keychain not plaintext (Phase 4); sandbox flag must actually enforce (Phase 7); never leak sensitive data in errors.

### Phase-3-specific verified anchors (confirm before editing)

- `AddCacheBreakpoints` exists with **zero production callers** (only a test): `internal/api/anthropic/cache.go:11`. Confirm: `grep -rn "AddCacheBreakpoints" internal/ cmd/` → only `cache.go:11` (def) + `client_test.go:535,541`.
- Stale cache-scope beta header: `internal/api/anthropic/betas.go:10` `PromptCachingScopeBetaHeader = "prompt-caching-scope-2024-07-31"`. CC reference current value is `prompt-caching-scope-2026-01-05` (`/Users/sqlrush/agent/claude-code/src/constants/betas.ts:17-18`). Confirm: `grep -rn "prompt-caching-scope" internal/ cmd/`.
- `Request.Thinking map[string]any` exists, shape `{"type":"enabled","budget_tokens":N}`: `internal/api/anthropic/types.go:24`. It is **read** by `usage.go:83`, `client.go:454-460`, `retry.go` but **never set** in the conversation path. Confirm: `grep -rn "\.Thinking =" internal/conversation/` → no matches.
- `contracts.ContentBlock` has **no** `Signature` field: `internal/contracts/messages.go:31-44`. Confirm: `grep -rn "Signature" internal/contracts/`.
- `StreamAccumulator.Add` handles only `text_delta` + `input_json_delta`; drops `thinking_delta`/`signature_delta`: `internal/api/anthropic/stream_accumulator.go:31-44`. Confirm: `grep -rn "thinking_delta\|signature_delta" internal/`.
- The turn loop is `Runner.RunTurn` `internal/conversation/run.go:205-256`; `result.StopReason = response.StopReason` at line 227, but stop_reason is never branched on — termination is purely `len(uses) == 0` (line 235).
- `MicroCompact(history, options) MicroResult` and `MicroCompactStored(...)` are **pure/deterministic** (no LLM call): `internal/compact/micro.go:364,369`. Confirm zero conversation callers: `grep -rn "MicroCompact" internal/conversation/` → no matches.
- Model thinking capability: `internal/model/model.go:21-33` (`Capability.SupportsThinking`, `.AlwaysOnThinking`); `Registry.Resolve(name) (Capability, bool)` `model.go:74`.
- Existing context-overflow helpers to REUSE (do not reinvent): `anthropic.ParseMaxTokensContextOverflowError` (`retry.go:117`), `anthropic.AdjustMaxTokensForContextOverflow` (`retry.go:134`), `anthropic.ContextOverflow` (`retry.go:27`). Note: these handle the **400** "input length and max_tokens exceed" error at the client layer (`client.go:449-466`); Phase 3 Task 6 handles the distinct **successful-response** `stop_reason == "model_context_window_exceeded"` case.

---

## File Structure

**Modified existing files:**
- `internal/api/anthropic/betas.go` — fix `PromptCachingScopeBetaHeader` to `prompt-caching-scope-2026-01-05`.
- `internal/contracts/messages.go` — add `Signature string` field to `ContentBlock` (struct + `UnmarshalJSON` aux).
- `internal/api/anthropic/stream_accumulator.go` — handle `thinking_delta` (append) and `signature_delta` (overwrite); pre-seed signature on thinking `content_block_start`.
- `internal/conversation/types.go` — add `EnablePromptCaching bool`, `ThinkingBudgetTokens int`, `PromptCacheTTL string` fields to `Runner`; add `EventThinking`/`EventRefusal` event types if used by render path (kept minimal — see Task 7 note).
- `internal/conversation/request.go` — in `buildRequest`: set `Request.Thinking` (Task 4) and call `AddCacheBreakpoints` (Task 2).
- `internal/conversation/run.go` — in `RunTurn`: stop_reason switch (Tasks 5/6), pause_turn resume (Task 5), orphaned tool_result injection (Task 7); add `maybeMicroCompact` step (Task 8).

**New files (small, one responsibility each):**
- `internal/conversation/thinking.go` — `thinkingRequestConfig(model, runner) map[string]any` helper (Task 4).
- `internal/conversation/stop_reason.go` — `stopReasonOutcome` enum + `classifyStopReason` + recovery helpers (Tasks 5/6).
- `internal/conversation/orphan_tool_results.go` — `synthesizeOrphanedToolResults(assistant, existing) []contracts.Message` (Task 7).
- `internal/conversation/micro_compact.go` — `maybeMicroCompact` runner method (Task 8).

---

## Task 1: Fix the stale prompt-caching-scope beta header

**Files:**
- Modify: `internal/api/anthropic/betas.go`
- Test: `internal/api/anthropic/betas_test.go` (new) + update `internal/api/anthropic/client_test.go:466`

**Interfaces:**
- Changes constant value of `PromptCachingScopeBetaHeader`; no signature changes.

CC anchor: the current header is `prompt-caching-scope-2026-01-05` — `/Users/sqlrush/agent/claude-code/src/constants/betas.ts:17-18`. Confirm with: `grep -rn "prompt-caching-scope" /Users/sqlrush/agent/claude-code/src`.

- [ ] **Step 1: Write the failing test**

Create `internal/api/anthropic/betas_test.go`:
```go
package anthropic

import (
	"testing"

	"ccgo/internal/contracts"
)

func TestPromptCachingScopeBetaHeaderIsCurrent(t *testing.T) {
	const want = "prompt-caching-scope-2026-01-05"
	if PromptCachingScopeBetaHeader != want {
		t.Fatalf("PromptCachingScopeBetaHeader = %q want %q", PromptCachingScopeBetaHeader, want)
	}
}

func TestDynamicBetaHeadersEmitsCurrentCacheScope(t *testing.T) {
	req := Request{
		Model: "claude-sonnet-4-6",
		Messages: []contracts.APIMessage{{
			Role: "user",
			Content: []contracts.ContentBlock{{
				Type:         contracts.ContentText,
				Text:         "hi",
				CacheControl: &contracts.CacheControl{Type: "ephemeral"},
			}},
		}},
	}
	betas := DynamicBetaHeaders(req)
	found := false
	for _, b := range betas {
		if b == "prompt-caching-scope-2026-01-05" {
			found = true
		}
	}
	if !found {
		t.Fatalf("expected current cache-scope header in %v", betas)
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/api/anthropic/ -run 'TestPromptCachingScopeBetaHeaderIsCurrent|TestDynamicBetaHeadersEmitsCurrentCacheScope' -v`
Expected: FAIL — got `"prompt-caching-scope-2024-07-31"`.

- [ ] **Step 3: Write minimal implementation**

In `internal/api/anthropic/betas.go:10`, change:
```go
	PromptCachingScopeBetaHeader = "prompt-caching-scope-2024-07-31"
```
to:
```go
	PromptCachingScopeBetaHeader = "prompt-caching-scope-2026-01-05"
```

- [ ] **Step 4: Update the stale assertion in the existing test**

`internal/api/anthropic/client_test.go:466` asserts the old value. Update it:
```go
		if got := r.Header.Get("anthropic-beta"); got != "one,prompt-caching-scope-2026-01-05,cache-editing-2025-01-24" {
```
Confirm there are no other hardcoded `2024-07-31` references first: `grep -rn "prompt-caching-scope-2024-07-31" internal/ cmd/`. Fix any others the same way (do not leave a stale literal).

- [ ] **Step 5: Run tests to verify they pass**

Run: `go test ./internal/api/anthropic/ -v && grep -rn "prompt-caching-scope-2024-07-31" internal/ cmd/`
Expected: PASS; grep returns nothing.

- [ ] **Step 6: Commit**

```bash
git add internal/api/anthropic/betas.go internal/api/anthropic/betas_test.go internal/api/anthropic/client_test.go
git commit -m "fix(api): update prompt-caching-scope beta header to 2026-01-05"
```

---

## Task 2: Call AddCacheBreakpoints in the request path

**Files:**
- Modify: `internal/conversation/types.go` (add runner fields)
- Modify: `internal/conversation/request.go` (call `AddCacheBreakpoints` in `buildRequest`)
- Test: `internal/conversation/cache_request_test.go` (new)

**Interfaces:**
- Adds `Runner` fields: `EnablePromptCaching bool`, `PromptCacheTTL string`.
- `buildRequest` now applies `anthropic.AddCacheBreakpoints(apiMessages, r.EnablePromptCaching, opts)` to `request.Messages`.

Verified: `AddCacheBreakpoints(messages []contracts.APIMessage, enablePromptCaching bool, options CacheBreakpointOptions) []contracts.APIMessage` (`internal/api/anthropic/cache.go:11`); returns a **copy** (`copyAPIMessages`, cache.go:43) so immutability holds. `CacheBreakpointOptions{ SkipCacheWrite bool; CacheControl contracts.CacheControl; NewCacheEdits []contracts.CacheEdit }` (cache.go:5-9). `contracts.CacheControl{Type, Scope, TTL string}` (messages.go:114-118). CC default marker shape is `{type:'ephemeral'}` with optional `ttl:'1h'` (`/Users/sqlrush/agent/claude-code/src/services/api/claude.ts:359-372`). Confirm: `go doc ./internal/api/anthropic AddCacheBreakpoints` and `go doc ./internal/api/anthropic CacheBreakpointOptions`.

- [ ] **Step 1: Write the failing test**

Create `internal/conversation/cache_request_test.go`:
```go
package conversation

import (
	"context"
	"testing"

	"ccgo/internal/contracts"
	"ccgo/internal/tool"
)

func lastContentBlock(msg contracts.APIMessage) (contracts.ContentBlock, bool) {
	if len(msg.Content) == 0 {
		return contracts.ContentBlock{}, false
	}
	return msg.Content[len(msg.Content)-1], true
}

func TestBuildRequestAddsCacheBreakpointWhenEnabled(t *testing.T) {
	reg, err := tool.NewRegistry()
	if err != nil {
		t.Fatal(err)
	}
	r := Runner{
		Tools:               tool.NewExecutor(reg),
		Model:               "claude-sonnet-4-6",
		EnablePromptCaching: true,
		PromptCacheTTL:      "1h",
	}
	history := []contracts.Message{
		{Type: contracts.MessageUser, Content: []contracts.ContentBlock{contracts.NewTextBlock("hello")}},
	}
	req, err := r.buildRequest(context.Background(), history, r.model(), relevantMemoryRequestContext{SkipSync: true})
	if err != nil {
		t.Fatalf("buildRequest err: %v", err)
	}
	if len(req.Messages) == 0 {
		t.Fatal("no messages built")
	}
	block, ok := lastContentBlock(req.Messages[len(req.Messages)-1])
	if !ok || block.CacheControl == nil {
		t.Fatalf("expected cache_control on last block, got %#v", block)
	}
	if block.CacheControl.Type != "ephemeral" {
		t.Fatalf("cache_control.type = %q want ephemeral", block.CacheControl.Type)
	}
	if block.CacheControl.TTL != "1h" {
		t.Fatalf("cache_control.ttl = %q want 1h", block.CacheControl.TTL)
	}
}

func TestBuildRequestNoCacheWhenDisabled(t *testing.T) {
	reg, err := tool.NewRegistry()
	if err != nil {
		t.Fatal(err)
	}
	r := Runner{Tools: tool.NewExecutor(reg), Model: "claude-sonnet-4-6"}
	history := []contracts.Message{
		{Type: contracts.MessageUser, Content: []contracts.ContentBlock{contracts.NewTextBlock("hello")}},
	}
	req, err := r.buildRequest(context.Background(), history, r.model(), relevantMemoryRequestContext{SkipSync: true})
	if err != nil {
		t.Fatalf("buildRequest err: %v", err)
	}
	block, _ := lastContentBlock(req.Messages[len(req.Messages)-1])
	if block.CacheControl != nil {
		t.Fatalf("expected no cache_control when disabled, got %#v", block.CacheControl)
	}
}
```

Before writing, confirm the `relevantMemoryRequestContext` field names with: `grep -n "type relevantMemoryRequestContext struct" -A6 internal/conversation/request.go` and `grep -n "func NewRegistry\|func NewExecutor" internal/tool/*.go`. Adjust the `SkipSync`/constructor calls to the verified names.

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/conversation/ -run 'TestBuildRequest.*Cache' -v`
Expected: FAIL — `EnablePromptCaching`/`PromptCacheTTL` undefined; cache_control nil.

- [ ] **Step 3: Write minimal implementation**

In `internal/conversation/types.go`, add to the `Runner` struct (near `UseStreaming`, line ~123):
```go
	EnablePromptCaching       bool
	PromptCacheTTL            string
```

In `internal/conversation/request.go`, after `apiMessages` is finalized and before `request := anthropic.Request{...}` (line ~72), apply breakpoints. Replace:
```go
	request := anthropic.Request{
		Model:     model,
		MaxTokens: r.maxTokens(),
		Messages:  apiMessages,
	}
```
with:
```go
	if r.EnablePromptCaching {
		apiMessages = anthropic.AddCacheBreakpoints(apiMessages, true, anthropic.CacheBreakpointOptions{
			CacheControl: contracts.CacheControl{Type: "ephemeral", TTL: r.PromptCacheTTL},
		})
	}
	request := anthropic.Request{
		Model:     model,
		MaxTokens: r.maxTokens(),
		Messages:  apiMessages,
	}
```
The empty `TTL` (when `PromptCacheTTL == ""`) is omitted by the `omitempty` JSON tag (`messages.go:117`), so disabled-TTL requests are unaffected. The dynamic beta header (`requestUsesPromptCaching`, `betas.go:68`) already detects the `CacheControl` marker and emits the (now-current) scope header automatically — no extra header wiring needed.

- [ ] **Step 4: Run tests to verify they pass**

Run: `go test ./internal/conversation/ -run 'TestBuildRequest' -v`
Expected: PASS.

- [ ] **Step 5: Commit**

```bash
git add internal/conversation/types.go internal/conversation/request.go internal/conversation/cache_request_test.go
git commit -m "feat(conversation): apply prompt-cache breakpoints in the request path"
```

---

## Task 3: Add Signature field to ContentBlock

**Files:**
- Modify: `internal/contracts/messages.go` (struct field + `UnmarshalJSON` aux + assignment)
- Test: `internal/contracts/signature_test.go` (new)

**Interfaces:**
- Adds `Signature string \`json:"signature,omitempty"\`` to `contracts.ContentBlock`.

Verified absence: `grep -rn "Signature" internal/contracts/` → no matches. The struct is at `messages.go:31-44`; its custom `UnmarshalJSON` (`messages.go:46-77`) re-builds the block from an aux struct, so the new field must be added in three places: the struct, the aux struct, and the `*b = ContentBlock{...}` assignment.

- [ ] **Step 1: Write the failing test**

Create `internal/contracts/signature_test.go`:
```go
package contracts

import (
	"encoding/json"
	"testing"
)

func TestContentBlockSignatureRoundTrip(t *testing.T) {
	in := `{"type":"thinking","thinking":"reasoning","signature":"abc123"}`
	var block ContentBlock
	if err := json.Unmarshal([]byte(in), &block); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if block.Type != ContentThinking {
		t.Fatalf("type = %q want thinking", block.Type)
	}
	if block.Signature != "abc123" {
		t.Fatalf("signature = %q want abc123", block.Signature)
	}
	out, err := json.Marshal(block)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	var round map[string]any
	if err := json.Unmarshal(out, &round); err != nil {
		t.Fatalf("re-unmarshal: %v", err)
	}
	if round["signature"] != "abc123" {
		t.Fatalf("marshalled signature = %v want abc123", round["signature"])
	}
}

func TestContentBlockSignatureOmittedWhenEmpty(t *testing.T) {
	out, err := json.Marshal(ContentBlock{Type: ContentText, Text: "hi"})
	if err != nil {
		t.Fatal(err)
	}
	var round map[string]any
	if err := json.Unmarshal(out, &round); err != nil {
		t.Fatal(err)
	}
	if _, ok := round["signature"]; ok {
		t.Fatalf("signature should be omitted when empty: %s", out)
	}
}
```

Note: the thinking text key may be `thinking` or `content` per the unmarshal aliasing (`messages.go:274-275`). This test only asserts `signature`; confirm the thinking-text key is not load-bearing here with `grep -n "\"thinking\"\|\"content\"" internal/contracts/messages.go`.

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/contracts/ -run TestContentBlockSignature -v`
Expected: FAIL — `block.Signature undefined`.

- [ ] **Step 3: Write minimal implementation**

In `internal/contracts/messages.go`, add to the `ContentBlock` struct (after `Edits`, line ~43):
```go
	Signature      string           `json:"signature,omitempty"`
```
Add the same field to the aux struct inside `UnmarshalJSON` (after `Edits`, line ~59):
```go
		Signature      string           `json:"signature"`
```
And add to the `*b = ContentBlock{...}` assignment (after `Edits: aux.Edits,`, line ~76):
```go
		Signature:      aux.Signature,
```

- [ ] **Step 4: Run tests to verify they pass**

Run: `go test ./internal/contracts/ -v`
Expected: PASS, including pre-existing messages tests.

- [ ] **Step 5: Commit**

```bash
git add internal/contracts/messages.go internal/contracts/signature_test.go
git commit -m "feat(contracts): add Signature field to ContentBlock for extended thinking"
```

---

## Task 4: Collect thinking + signature deltas in the accumulator, and enable thinking in requests

**Files:**
- Modify: `internal/api/anthropic/stream_accumulator.go` (handle thinking/signature deltas)
- Create: `internal/conversation/thinking.go` (request-thinking config helper)
- Modify: `internal/conversation/request.go` (set `Request.Thinking`)
- Modify: `internal/conversation/types.go` (add `ThinkingBudgetTokens int`)
- Test: `internal/api/anthropic/stream_accumulator_thinking_test.go` (new) + `internal/conversation/thinking_test.go` (new)

**Interfaces:**
- `StreamAccumulator.Add` now handles `thinking_delta` (append to `block.Text`), `signature_delta` (overwrite `block.Signature`), and pre-seeds a `thinking` `content_block_start`.
- New `func thinkingRequestConfig(capability model.Capability, budgetTokens int) map[string]any` returning `nil` or `{"type":"enabled","budget_tokens":N}`.
- `buildRequest` sets `request.Thinking` from the resolved model capability.
- New `Runner.ThinkingBudgetTokens int` field.

CC anchors (verified): accumulator pre-seeds `signature: ''` on thinking `content_block_start` (`/Users/sqlrush/agent/claude-code/src/services/api/claude.ts:2030-2037`); `thinking_delta` appends (`claude.ts:2160`), `signature_delta` **overwrites** (`claude.ts:2146`). Request thinking shape is `{"type":"enabled","budget_tokens":N}` — verified in ccgo by the existing read sites (`usage.go:83`, `client.go:454-460`, `client_test.go:334`).

ccgo note: the accumulator stores the thinking text in `block.Text` (the `ContentThinking` block's text lives in `ContentBlock.Text` — verified by `messages.go:334` and `messages_test.go:474`). Use `block.Text += ...` for thinking_delta (not a separate field).

- [ ] **Step 1: Write the failing accumulator test**

Create `internal/api/anthropic/stream_accumulator_thinking_test.go`:
```go
package anthropic

import (
	"testing"

	"ccgo/internal/contracts"
)

func TestAccumulatorCollectsThinkingAndSignature(t *testing.T) {
	acc := NewStreamAccumulator()
	mustAdd := func(e StreamEvent) {
		if err := acc.Add(e); err != nil {
			t.Fatalf("Add(%s): %v", e.Type, err)
		}
	}
	mustAdd(StreamEvent{Type: "message_start", Message: &Response{Model: "claude-sonnet-4-6"}})
	mustAdd(StreamEvent{
		Type:         "content_block_start",
		Index:        0,
		ContentBlock: &contracts.ContentBlock{Type: contracts.ContentThinking},
	})
	mustAdd(StreamEvent{Type: "content_block_delta", Index: 0, Delta: map[string]any{"type": "thinking_delta", "thinking": "Let me "}})
	mustAdd(StreamEvent{Type: "content_block_delta", Index: 0, Delta: map[string]any{"type": "thinking_delta", "thinking": "think."}})
	mustAdd(StreamEvent{Type: "content_block_delta", Index: 0, Delta: map[string]any{"type": "signature_delta", "signature": "SIG=="}})
	mustAdd(StreamEvent{Type: "content_block_stop", Index: 0})

	resp := acc.Finish()
	if len(resp.Content) != 1 {
		t.Fatalf("content len = %d want 1", len(resp.Content))
	}
	block := resp.Content[0]
	if block.Type != contracts.ContentThinking {
		t.Fatalf("type = %q want thinking", block.Type)
	}
	if block.Text != "Let me think." {
		t.Fatalf("thinking text = %q want %q", block.Text, "Let me think.")
	}
	if block.Signature != "SIG==" {
		t.Fatalf("signature = %q want SIG==", block.Signature)
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/api/anthropic/ -run TestAccumulatorCollectsThinking -v`
Expected: FAIL — thinking text empty, signature empty.

- [ ] **Step 3: Implement the accumulator changes**

In `internal/api/anthropic/stream_accumulator.go`, extend the `content_block_delta` switch (line ~34) to add two cases:
```go
		switch event.Delta["type"] {
		case "text_delta":
			if text, ok := event.Delta["text"].(string); ok {
				block.Text += text
			}
		case "thinking_delta":
			if text, ok := event.Delta["thinking"].(string); ok {
				block.Text += text
			}
		case "signature_delta":
			if sig, ok := event.Delta["signature"].(string); ok {
				block.Signature = sig
			}
		case "input_json_delta":
			if partial, ok := event.Delta["partial_json"].(string); ok {
				a.jsonBuf[event.Index] += partial
			}
		}
```
(The `content_block_start` already copies the block verbatim at line 29, so a `thinking` start block is preserved; no pre-seed needed because Go's zero value for `Signature` is `""`, matching CC's intent.)

- [ ] **Step 4: Run accumulator test to verify it passes**

Run: `go test ./internal/api/anthropic/ -run TestAccumulator -v`
Expected: PASS.

- [ ] **Step 5: Write the failing request-thinking test**

Create `internal/conversation/thinking_test.go`:
```go
package conversation

import (
	"context"
	"testing"

	"ccgo/internal/contracts"
	"ccgo/internal/model"
	"ccgo/internal/tool"
)

func TestThinkingRequestConfigEnabledForThinkingModel(t *testing.T) {
	cap, ok := model.DefaultRegistry().Resolve("claude-sonnet-4-6")
	if !ok {
		t.Skip("model not in registry; confirm name via go doc ./internal/model")
	}
	cfg := thinkingRequestConfig(cap, 8000)
	if cfg == nil {
		t.Fatal("expected thinking config for a thinking-capable model")
	}
	if cfg["type"] != "enabled" {
		t.Fatalf("type = %v want enabled", cfg["type"])
	}
	if cfg["budget_tokens"] != 8000 {
		t.Fatalf("budget_tokens = %v want 8000", cfg["budget_tokens"])
	}
}

func TestThinkingRequestConfigNilForNonThinkingModel(t *testing.T) {
	cap, ok := model.DefaultRegistry().Resolve("claude-haiku-4-5")
	if !ok {
		t.Skip("model not in registry")
	}
	if cfg := thinkingRequestConfig(cap, 8000); cfg != nil {
		t.Fatalf("expected nil thinking config for non-thinking model, got %v", cfg)
	}
}

func TestBuildRequestSetsThinkingWhenBudgetSet(t *testing.T) {
	reg, err := tool.NewRegistry()
	if err != nil {
		t.Fatal(err)
	}
	r := Runner{Tools: tool.NewExecutor(reg), Model: "claude-sonnet-4-6", ThinkingBudgetTokens: 8000}
	history := []contracts.Message{
		{Type: contracts.MessageUser, Content: []contracts.ContentBlock{contracts.NewTextBlock("hi")}},
	}
	req, err := r.buildRequest(context.Background(), history, r.model(), relevantMemoryRequestContext{SkipSync: true})
	if err != nil {
		t.Fatalf("buildRequest err: %v", err)
	}
	if req.Thinking == nil || req.Thinking["type"] != "enabled" {
		t.Fatalf("expected thinking enabled, got %#v", req.Thinking)
	}
}
```

Confirm `Claude45Haiku`'s registry entry has `SupportsThinking == false` (verified at `model.go:60`: `capability(..., false, false, false)`) and the sonnet entry `true` (`model.go:65`). Confirm `model.Capability.SupportsThinking`/`AlwaysOnThinking` exist with `go doc ./internal/model Capability`.

- [ ] **Step 6: Run thinking-request test to verify it fails**

Run: `go test ./internal/conversation/ -run 'TestThinking|TestBuildRequestSetsThinking' -v`
Expected: FAIL — `undefined: thinkingRequestConfig`; `ThinkingBudgetTokens` undefined.

- [ ] **Step 7: Implement thinking config + wiring**

Add `ThinkingBudgetTokens int` to `Runner` in `internal/conversation/types.go` (near `MaxTokens`, line ~121).

Create `internal/conversation/thinking.go`:
```go
package conversation

import "ccgo/internal/model"

// thinkingRequestConfig returns the Anthropic `thinking` request parameter for a
// model that supports extended thinking, or nil when thinking should not be set.
// Shape matches the API: {"type":"enabled","budget_tokens":N}.
func thinkingRequestConfig(capability model.Capability, budgetTokens int) map[string]any {
	if budgetTokens <= 0 && !capability.AlwaysOnThinking {
		return nil
	}
	if !capability.SupportsThinking && !capability.AlwaysOnThinking {
		return nil
	}
	if budgetTokens <= 0 {
		budgetTokens = defaultThinkingBudgetTokens
	}
	return map[string]any{
		"type":          "enabled",
		"budget_tokens": budgetTokens,
	}
}

const defaultThinkingBudgetTokens = 4_096
```

In `internal/conversation/request.go`, inside `buildRequest`, after the `request.System`/`request.Tools` block (line ~82) and before `return request, nil`:
```go
	if capability, ok := model.DefaultRegistry().Resolve(model); ok {
		if thinking := thinkingRequestConfig(capability, r.ThinkingBudgetTokens); thinking != nil {
			request.Thinking = thinking
		}
	}
```
Confirm the `model` package is imported in request.go; if not, add `"ccgo/internal/model"`. Note the local variable shadow: the function param is named `model string`, which collides with the package name `model`. Rename the helper call to use the registry without the package collision — resolve via the already-imported `modelpkg` alias if one exists (`grep -n "modelpkg\|\"ccgo/internal/model\"" internal/conversation/*.go`); use that alias. If no alias exists, add `modelpkg "ccgo/internal/model"` and call `modelpkg.DefaultRegistry().Resolve(model)`.

- [ ] **Step 8: Run all affected tests to verify they pass**

Run: `go test ./internal/api/anthropic/ ./internal/conversation/ -run 'Accumulator|Thinking|BuildRequest' -v`
Expected: PASS.

- [ ] **Step 9: Commit**

```bash
git add internal/api/anthropic/stream_accumulator.go internal/api/anthropic/stream_accumulator_thinking_test.go internal/conversation/thinking.go internal/conversation/thinking_test.go internal/conversation/request.go internal/conversation/types.go
git commit -m "feat(conversation): enable extended thinking and collect thinking/signature deltas"
```

---

## Task 5: stop_reason control flow — max_tokens recovery, refusal surfacing, pause_turn resume

**Files:**
- Create: `internal/conversation/stop_reason.go`
- Modify: `internal/conversation/run.go` (consult the classifier in `RunTurn` after `send`)
- Test: `internal/conversation/stop_reason_test.go` (new)

**Interfaces:**
- New `type stopAction int` with `stopActionContinue`, `stopActionRecoverMaxTokens`, `stopActionResumePauseTurn`, `stopActionRefusal`, `stopActionContextWindowExceeded` (Task 6 uses the last).
- New `func classifyStopReason(reason string) stopAction`.
- New `Runner` recovery helpers; integrated into the `RunTurn` loop (`run.go:205-256`).

CC anchors (verified): `max_tokens` → surface a max-output-tokens error message and let a recovery loop (cap `MAX_OUTPUT_TOKENS_RECOVERY_LIMIT = 3`, `query.ts:164`) continue — `/Users/sqlrush/agent/claude-code/src/services/api/claude.ts:2266-2277`. `refusal` → surface a Usage-Policy refusal message, **not** retried — `errors.ts:1184-1207`. **pause_turn is NOT in the CC reference** (`grep -rn "pause_turn" /Users/sqlrush/agent/claude-code/src` → zero). Per the roadmap brief it is still required (it is a documented standard-API stop_reason for server-tool turns: the assistant content is partial and the turn must be re-sent unchanged to continue). This is a **deliberate addition beyond the CC reference** — flagged here.

Loop-shape decision: the existing loop terminates only when `len(uses) == 0` (run.go:235). The stop_reason switch must run on every response, **before** the tool-use check, because:
- `refusal` and `model_context_window_exceeded` arrive with no tool uses but must produce a surfaced message rather than a silent normal stop.
- `pause_turn` may arrive with or without tool uses and requires re-sending.
- `max_tokens` may truncate a tool_use; recovery re-queries with a continuation.

- [ ] **Step 1: Write the failing classifier test**

Create `internal/conversation/stop_reason_test.go`:
```go
package conversation

import "testing"

func TestClassifyStopReason(t *testing.T) {
	cases := map[string]stopAction{
		"":                              stopActionContinue,
		"end_turn":                      stopActionContinue,
		"tool_use":                      stopActionContinue,
		"stop_sequence":                 stopActionContinue,
		"max_tokens":                    stopActionRecoverMaxTokens,
		"pause_turn":                    stopActionResumePauseTurn,
		"refusal":                       stopActionRefusal,
		"model_context_window_exceeded": stopActionContextWindowExceeded,
	}
	for reason, want := range cases {
		if got := classifyStopReason(reason); got != want {
			t.Fatalf("classifyStopReason(%q) = %v want %v", reason, got, want)
		}
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/conversation/ -run TestClassifyStopReason -v`
Expected: FAIL — `undefined: stopAction` / `classifyStopReason`.

- [ ] **Step 3: Implement the classifier and recovery message helpers**

Create `internal/conversation/stop_reason.go`:
```go
package conversation

import (
	"ccgo/internal/contracts"
	msgs "ccgo/internal/messages"
)

type stopAction int

const (
	stopActionContinue stopAction = iota
	stopActionRecoverMaxTokens
	stopActionResumePauseTurn
	stopActionRefusal
	stopActionContextWindowExceeded
)

// maxOutputTokensRecoveryLimit mirrors CC's MAX_OUTPUT_TOKENS_RECOVERY_LIMIT.
const maxOutputTokensRecoveryLimit = 3

// classifyStopReason maps an Anthropic stop_reason to the loop's control action.
func classifyStopReason(reason string) stopAction {
	switch reason {
	case "max_tokens":
		return stopActionRecoverMaxTokens
	case "pause_turn":
		return stopActionResumePauseTurn
	case "refusal":
		return stopActionRefusal
	case "model_context_window_exceeded":
		return stopActionContextWindowExceeded
	default:
		return stopActionContinue
	}
}

const refusalMessageText = "The model declined to respond because the request was flagged by Anthropic's Usage Policy. Try rephrasing your request, or switch models with /model."

const maxTokensRecoveryText = "[The previous response was truncated because it reached the max output tokens limit. Continue from where you left off.]"

const contextWindowExceededText = "The conversation reached the model's context window limit. Older messages must be compacted (/compact) before continuing."

// refusalMessage builds the surfaced assistant refusal message.
func (r Runner) refusalMessage() contracts.Message {
	msg := msgs.AssistantText(refusalMessageText)
	if r.SessionID != "" {
		msg.SessionID = r.SessionID
	}
	return msg
}

// maxTokensContinuationMessage builds the user nudge that drives max_tokens recovery.
func (r Runner) maxTokensContinuationMessage() contracts.Message {
	msg := msgs.UserText(maxTokensRecoveryText)
	if r.SessionID != "" {
		msg.SessionID = r.SessionID
	}
	return msg
}

// contextWindowExceededMessage builds the surfaced ctx-window error message.
func (r Runner) contextWindowExceededMessage() contracts.Message {
	msg := msgs.AssistantText(contextWindowExceededText)
	if r.SessionID != "" {
		msg.SessionID = r.SessionID
	}
	return msg
}
```

Confirm the message constructors exist with the expected signatures: `grep -n "func AssistantText\|func UserText" internal/messages/*.go`. If `AssistantText` does not exist, build the assistant message inline using the same pattern as `appendLocalTextResult` (find it: `grep -n "func (r Runner) appendLocalTextResult" internal/conversation/run.go` and reuse its message-construction). Do **not** invent a constructor — reuse the verified one.

- [ ] **Step 4: Run classifier test to verify it passes**

Run: `go test ./internal/conversation/ -run TestClassifyStopReason -v`
Expected: PASS.

- [ ] **Step 5: Write the failing integration test (recovery + refusal + pause_turn)**

Add to `internal/conversation/stop_reason_test.go`. First read the existing fake-client test harness pattern: `grep -n "CreateMessage\|type.*[Cc]lient struct\|func.*RunTurn" internal/conversation/run_test.go | head -30` — reuse the existing in-package fake client (do **not** invent one). The scripted-client below illustrates intent; bind it to the real fake-client shape found in `run_test.go`:
```go
// scriptedClient returns a queued sequence of responses, one per CreateMessage call.
type scriptedClient struct {
	responses []*anthropic.Response
	calls     int
}

func (c *scriptedClient) CreateMessage(_ context.Context, _ anthropic.Request) (*anthropic.Response, error) {
	if c.calls >= len(c.responses) {
		// default terminal response
		return &anthropic.Response{StopReason: "end_turn", Content: []contracts.ContentBlock{contracts.NewTextBlock("done")}}, nil
	}
	r := c.responses[c.calls]
	c.calls++
	return r, nil
}

func TestRunTurnRefusalSurfacesMessageAndStops(t *testing.T) {
	client := &scriptedClient{responses: []*anthropic.Response{
		{StopReason: "refusal", Content: nil},
	}}
	r := newTestRunner(t, client) // reuse run_test.go's runner builder
	res, err := r.RunTurn(context.Background(), nil, msgs.UserText("do something"))
	if err != nil {
		t.Fatalf("RunTurn err: %v", err)
	}
	if res.StopReason != "refusal" {
		t.Fatalf("StopReason = %q want refusal", res.StopReason)
	}
	if !containsText(res.Messages, "Usage Policy") {
		t.Fatalf("expected refusal message surfaced, got %d msgs", len(res.Messages))
	}
	if client.calls != 1 {
		t.Fatalf("refusal must not retry; calls = %d", client.calls)
	}
}

func TestRunTurnPauseTurnResumes(t *testing.T) {
	client := &scriptedClient{responses: []*anthropic.Response{
		{StopReason: "pause_turn", Content: []contracts.ContentBlock{contracts.NewTextBlock("partial")}},
		{StopReason: "end_turn", Content: []contracts.ContentBlock{contracts.NewTextBlock("finished")}},
	}}
	r := newTestRunner(t, client)
	res, err := r.RunTurn(context.Background(), nil, msgs.UserText("go"))
	if err != nil {
		t.Fatalf("RunTurn err: %v", err)
	}
	if client.calls != 2 {
		t.Fatalf("pause_turn must resume; calls = %d want 2", client.calls)
	}
	if res.StopReason != "end_turn" {
		t.Fatalf("final StopReason = %q want end_turn", res.StopReason)
	}
}

func TestRunTurnMaxTokensRecovers(t *testing.T) {
	client := &scriptedClient{responses: []*anthropic.Response{
		{StopReason: "max_tokens", Content: []contracts.ContentBlock{contracts.NewTextBlock("truncat")}},
		{StopReason: "end_turn", Content: []contracts.ContentBlock{contracts.NewTextBlock("ed and continued")}},
	}}
	r := newTestRunner(t, client)
	res, err := r.RunTurn(context.Background(), nil, msgs.UserText("write a lot"))
	if err != nil {
		t.Fatalf("RunTurn err: %v", err)
	}
	if client.calls != 2 {
		t.Fatalf("max_tokens must recover once; calls = %d want 2", client.calls)
	}
	if res.StopReason != "end_turn" {
		t.Fatalf("final StopReason = %q want end_turn", res.StopReason)
	}
}
```
`containsText` and `newTestRunner` must be the existing helpers from `run_test.go` (find with `grep -n "func newTestRunner\|func containsText\|func runnerWithClient" internal/conversation/run_test.go`); reuse them. If a `scriptedClient`-equivalent already exists, use it.

- [ ] **Step 6: Run integration tests to verify they fail**

Run: `go test ./internal/conversation/ -run 'TestRunTurn(Refusal|PauseTurn|MaxTokens)' -v`
Expected: FAIL — refusal/pause/max_tokens currently fall through `len(uses)==0` and stop without recovery.

- [ ] **Step 7: Wire the switch into RunTurn**

In `internal/conversation/run.go`, inside the `for round := 0; ; round++` loop (line 205), after `runner.emit(Event{Type: EventAssistantMessage, ...})` (line 232) and **before** `uses := ToolUses(assistant)` (line 234), insert the switch. Add a `maxTokensRecoveries int` counter declared just before the loop (line ~205):
```go
	maxTokensRecoveries := 0
	for round := 0; ; round++ {
```
Then after line 232:
```go
		switch classifyStopReason(response.StopReason) {
		case stopActionRefusal:
			refusal := runner.refusalMessage()
			history, refusal = appendMessage(history, refusal)
			result.Messages = append(result.Messages, refusal)
			if err := runner.appendTranscript(refusal); err != nil {
				return result, err
			}
			runner.emit(Event{Type: EventAssistantMessage, Message: &refusal, Model: response.Model})
			return result, nil
		case stopActionContextWindowExceeded:
			// Recovery is implemented in Task 6; for now surface and stop.
			ctxMsg := runner.contextWindowExceededMessage()
			history, ctxMsg = appendMessage(history, ctxMsg)
			result.Messages = append(result.Messages, ctxMsg)
			if err := runner.appendTranscript(ctxMsg); err != nil {
				return result, err
			}
			runner.emit(Event{Type: EventAssistantMessage, Message: &ctxMsg, Model: response.Model})
			return result, nil
		case stopActionResumePauseTurn:
			// Re-send the same history (the partial assistant turn is already
			// appended) so the server resumes the paused turn.
			continue
		case stopActionRecoverMaxTokens:
			if len(ToolUses(assistant)) == 0 {
				if maxTokensRecoveries >= maxOutputTokensRecoveryLimit {
					return result, nil
				}
				maxTokensRecoveries++
				nudge := runner.maxTokensContinuationMessage()
				history, nudge = appendMessage(history, nudge)
				result.Messages = append(result.Messages, nudge)
				if err := runner.appendTranscript(nudge); err != nil {
					return result, err
				}
				runner.emit(Event{Type: EventUserMessage, Message: &nudge})
				continue
			}
			// max_tokens with truncated tool_use: fall through to normal tool handling.
		}
```
Then the existing `uses := ToolUses(assistant)` block (line 234) runs unchanged for the `stopActionContinue` and tool-bearing `max_tokens` cases.

Immutability note: `runner` is already the per-turn copy (`run.go:163-164` `runner := *r`); all recovery mutates only loop-local `history`/`result`, never the shared `*r`.

- [ ] **Step 8: Run all stop_reason tests to verify they pass**

Run: `go test ./internal/conversation/ -run 'TestClassifyStopReason|TestRunTurn(Refusal|PauseTurn|MaxTokens)' -v && go test ./internal/conversation/ -run TestRunTurn -v`
Expected: PASS, including pre-existing `RunTurn` tests (normal `end_turn`/`tool_use` flow is `stopActionContinue`, unchanged).

- [ ] **Step 9: Commit**

```bash
git add internal/conversation/stop_reason.go internal/conversation/stop_reason_test.go internal/conversation/run.go
git commit -m "feat(conversation): handle max_tokens recovery, pause_turn resume, and refusal in the turn loop"
```

---

## Task 6: ctx-window-exceeded recovery via compaction

**Files:**
- Modify: `internal/conversation/run.go` (replace the Task-5 placeholder `stopActionContextWindowExceeded` branch with a recovery attempt)
- Modify: `internal/conversation/stop_reason.go` (add a recovery-attempt helper if needed)
- Test: `internal/conversation/stop_reason_test.go` (add a recovery test)

**Interfaces:**
- The `stopActionContextWindowExceeded` branch now attempts one compaction-then-retry before surfacing.

CC anchor (verified): `model_context_window_exceeded` deliberately reuses the max-output-tokens recovery path — `/Users/sqlrush/agent/claude-code/src/services/api/claude.ts:2279-2292` — and the overflow recovery in the loop tries context-collapse drain then reactive compact (`query.ts:1070-1124`). ccgo already has full auto-compaction: `runner.maybeAutoCompact(ctx, history)` (`run.go:183`) returns `(compactedHistory, compactResult, ok, err)`. Reuse it with `Force` semantics. Confirm signature: `grep -n "func (r Runner) maybeAutoCompact\|func.*manualCompact" internal/conversation/run.go`.

- [ ] **Step 1: Write the failing recovery test**

Add to `internal/conversation/stop_reason_test.go`:
```go
func TestRunTurnContextWindowExceededRecoversViaCompact(t *testing.T) {
	client := &scriptedClient{responses: []*anthropic.Response{
		{StopReason: "model_context_window_exceeded", Content: nil},
		// after compaction the model succeeds
		{StopReason: "end_turn", Content: []contracts.ContentBlock{contracts.NewTextBlock("recovered")}},
	}}
	r := newTestRunner(t, client)
	// Provide a compact client + AutoConfig so compaction can run; reuse the
	// existing run_test.go helper that enables compaction if one exists.
	enableCompaction(t, &r, client) // reuse run_test.go helper
	res, err := r.RunTurn(context.Background(), longHistory(t, 40), msgs.UserText("continue"))
	if err != nil {
		t.Fatalf("RunTurn err: %v", err)
	}
	if !res.Compacted {
		t.Fatalf("expected compaction to be triggered for ctx-window recovery")
	}
	if client.calls < 2 {
		t.Fatalf("expected a retry after compaction; calls = %d", client.calls)
	}
	if res.StopReason != "end_turn" {
		t.Fatalf("final StopReason = %q want end_turn", res.StopReason)
	}
}
```
`enableCompaction` and `longHistory` are illustrative — find the real compaction-enabling test setup in `run_test.go` (`grep -n "AutoCompact\|CompactClient\|maybeAutoCompact" internal/conversation/run_test.go | head`) and mirror it. If the harness makes a force-compact path awkward, assert the simpler observable: that the ctx-window branch calls `manualCompact`/`maybeAutoCompact` (verify via `res.Compacted == true` and a second `CreateMessage` call).

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/conversation/ -run TestRunTurnContextWindowExceeded -v`
Expected: FAIL — current placeholder surfaces and stops without compaction/retry.

- [ ] **Step 3: Implement the recovery branch**

In `internal/conversation/run.go`, replace the Task-5 placeholder `case stopActionContextWindowExceeded:` body with a one-shot recovery. Add a `contextWindowRecovered bool` guard declared before the loop (next to `maxTokensRecoveries`) so recovery is attempted at most once:
```go
		case stopActionContextWindowExceeded:
			if !contextWindowRecovered {
				contextWindowRecovered = true
				compactedHistory, compactResult, ok, cerr := runner.forceCompact(ctx, history)
				if cerr != nil {
					return result, cerr
				}
				if ok {
					history = compactedHistory
					result.Compacted = true
					result.Compact = &compactResult
					result.Messages = append(result.Messages, compactResult.Plan.Boundary, compactResult.Plan.Summary)
					if err := runner.appendCompactTranscript(compactResult.Plan); err != nil {
						return result, err
					}
					runner.emit(Event{Type: EventCompact, Compact: &compactResult})
					continue
				}
			}
			ctxMsg := runner.contextWindowExceededMessage()
			history, ctxMsg = appendMessage(history, ctxMsg)
			result.Messages = append(result.Messages, ctxMsg)
			if err := runner.appendTranscript(ctxMsg); err != nil {
				return result, err
			}
			runner.emit(Event{Type: EventAssistantMessage, Message: &ctxMsg, Model: response.Model})
			return result, nil
```
Add `forceCompact` to `internal/conversation/stop_reason.go` (or wherever `maybeAutoCompact` lives — keep it next to its sibling). It wraps the existing compaction with `Force: true` semantics:
```go
// forceCompact runs a one-shot forced compaction for ctx-window recovery,
// reusing the existing auto-compaction machinery with Force enabled.
func (r Runner) forceCompact(ctx context.Context, history []contracts.Message) ([]contracts.Message, compactpkg.Result, bool, error) {
	forced := r
	if forced.AutoCompact == nil {
		forced.AutoCompact = &compactpkg.AutoConfig{}
	} else {
		cfg := *forced.AutoCompact
		forced.AutoCompact = &cfg
	}
	forced.AutoCompact.Enabled = true
	forced.AutoCompact.Force = true
	return forced.maybeAutoCompact(ctx, history)
}
```
First confirm `maybeAutoCompact`'s exact return tuple and that `AutoConfig.Force` exists (verified at `compact/runner.go:18-28` — `Force bool` at line 20). Confirm the `compactpkg` import alias in run.go: `grep -n "compactpkg\|\"ccgo/internal/compact\"" internal/conversation/*.go`. Use the verified alias. Declare the guard before the loop:
```go
	contextWindowRecovered := false
```

- [ ] **Step 4: Run recovery test to verify it passes**

Run: `go test ./internal/conversation/ -run 'TestRunTurnContextWindow|TestClassifyStopReason' -v`
Expected: PASS.

- [ ] **Step 5: Commit**

```bash
git add internal/conversation/run.go internal/conversation/stop_reason.go internal/conversation/stop_reason_test.go
git commit -m "feat(conversation): recover from model_context_window_exceeded via forced compaction"
```

---

## Task 7: Inject orphaned tool_result on mid-turn bail

**Files:**
- Create: `internal/conversation/orphan_tool_results.go`
- Modify: `internal/conversation/run.go` (`executeToolUses` / the round loop's tool branch)
- Test: `internal/conversation/orphan_tool_results_test.go` (new)

**Interfaces:**
- New `func synthesizeOrphanedToolResults(sessionID contracts.ID, assistant contracts.Message, produced []contracts.Message, reason string) []contracts.Message` — for every `tool_use` block in `assistant` whose ID is not in `produced`, emit an `is_error` `tool_result` user message.
- `RunTurn` injects these into `history`/`result` when the round bails (ctx cancelled or `executeToolUses` returns fewer results than uses) so the next request is not orphaned.

CC anchors (verified): `yieldMissingToolResultBlocks` injects a synthetic `is_error:true` tool_result per unmatched `tool_use_id` — `/Users/sqlrush/agent/claude-code/src/query.ts:123-149`; invoked on abort — `query.ts:1015-1051` (message `"Interrupted by user"`). ccgo's `executeToolUses` already builds `msgs.ToolResult(use.ID, content, isError)` (`run.go:7483`); reuse that constructor for synthetic results. Confirm: `grep -n "func ToolResult" internal/messages/*.go`.

- [ ] **Step 1: Write the failing test**

Create `internal/conversation/orphan_tool_results_test.go`:
```go
package conversation

import (
	"testing"

	"ccgo/internal/contracts"
)

func toolUseAssistant(ids ...string) contracts.Message {
	blocks := make([]contracts.ContentBlock, 0, len(ids))
	for _, id := range ids {
		blocks = append(blocks, contracts.ContentBlock{Type: contracts.ContentToolUse, ID: id, Name: "Bash"})
	}
	return contracts.Message{Type: contracts.MessageAssistant, Content: blocks}
}

func toolResultMsg(id string) contracts.Message {
	return contracts.Message{
		Type:    contracts.MessageUser,
		Content: []contracts.ContentBlock{{Type: contracts.ContentToolResult, ToolUseID: id}},
	}
}

func TestSynthesizeOrphanedToolResults(t *testing.T) {
	assistant := toolUseAssistant("a", "b", "c")
	produced := []contracts.Message{toolResultMsg("a")} // only "a" got a result
	orphans := synthesizeOrphanedToolResults("s1", assistant, produced, "Interrupted by user")
	if len(orphans) != 2 {
		t.Fatalf("orphans = %d want 2 (for b and c)", len(orphans))
	}
	got := map[string]bool{}
	for _, m := range orphans {
		for _, blk := range m.Content {
			if blk.Type != contracts.ContentToolResult {
				t.Fatalf("orphan block type = %q want tool_result", blk.Type)
			}
			if !blk.IsError {
				t.Fatalf("orphan tool_result must be is_error")
			}
			got[blk.ToolUseID] = true
		}
	}
	if !got["b"] || !got["c"] || got["a"] {
		t.Fatalf("orphan tool_use_ids = %v want {b,c}", got)
	}
}

func TestSynthesizeOrphanedToolResultsNoneWhenComplete(t *testing.T) {
	assistant := toolUseAssistant("a", "b")
	produced := []contracts.Message{toolResultMsg("a"), toolResultMsg("b")}
	if orphans := synthesizeOrphanedToolResults("s1", assistant, produced, "x"); len(orphans) != 0 {
		t.Fatalf("expected no orphans, got %d", len(orphans))
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/conversation/ -run TestSynthesizeOrphanedToolResults -v`
Expected: FAIL — `undefined: synthesizeOrphanedToolResults`.

- [ ] **Step 3: Implement the synthesizer**

Create `internal/conversation/orphan_tool_results.go`:
```go
package conversation

import (
	"ccgo/internal/contracts"
	msgs "ccgo/internal/messages"
)

// synthesizeOrphanedToolResults returns one synthetic is_error tool_result user
// message for every tool_use block in assistant that has no matching tool_result
// in produced. This prevents a 400 (orphaned tool_use) on the next request when a
// turn bails mid-tool-execution. Mirrors CC's yieldMissingToolResultBlocks.
func synthesizeOrphanedToolResults(sessionID contracts.ID, assistant contracts.Message, produced []contracts.Message, reason string) []contracts.Message {
	resolved := map[string]bool{}
	for _, m := range produced {
		for _, block := range m.Content {
			if block.Type == contracts.ContentToolResult && block.ToolUseID != "" {
				resolved[block.ToolUseID] = true
			}
		}
	}
	if reason == "" {
		reason = "Tool execution was interrupted."
	}
	var out []contracts.Message
	for _, block := range assistant.Content {
		if block.Type != contracts.ContentToolUse || block.ID == "" {
			continue
		}
		if resolved[block.ID] {
			continue
		}
		msg := msgs.ToolResult(contracts.ID(block.ID), reason, true)
		if sessionID != "" {
			msg.SessionID = sessionID
		}
		out = append(out, msg)
	}
	return out
}
```
Confirm `msgs.ToolResult` signature: `grep -n "func ToolResult" internal/messages/*.go`. It is `ToolResult(id contracts.ID, content string, isError bool) contracts.Message` per the call at `run.go:7483` (`msgs.ToolResult(use.ID, result.Content, result.IsError)`). If the content param is not a `string`, adapt the synthetic content accordingly (use the reason text as the result content).

- [ ] **Step 4: Wire injection into the bail path**

In `internal/conversation/run.go`, in the round loop's tool branch (after `toolMessages, toolResults := runner.executeToolUses(...)`, line 244), guard the next-request orphan condition. The cleanest hook: after appending `toolMessages` to `history`, append synthetic results for any uses whose IDs are missing from `toolMessages`, then check `ctx.Err()`:
```go
		toolMessages, toolResults := runner.executeToolUses(ctx, uses, toolMetadata, result.Messages)
		if orphans := synthesizeOrphanedToolResults(runner.SessionID, assistant, toolMessages, "Tool execution was interrupted."); len(orphans) > 0 {
			toolMessages = append(toolMessages, orphans...)
		}
		for i := range toolMessages {
			history, toolMessages[i] = appendMessage(history, toolMessages[i])
			result.Messages = append(result.Messages, toolMessages[i])
			if err := runner.appendTranscript(toolMessages[i]); err != nil {
				return result, err
			}
		}
		if err := ctx.Err(); err != nil {
			return result, err
		}
```
This guarantees that every `tool_use` in the just-emitted assistant message has a matching `tool_result` in `history` before the loop re-queries — even if `executeToolUses` returned early due to cancellation. The `ctx.Err()` check after appending ensures a cancelled turn returns *with* the orphan results already persisted (so a later resume is well-formed). Confirm `executeToolUses` can return short on cancellation: `grep -n "ctx.Done\|ctx.Err\|RunTools" internal/conversation/run.go internal/tool/*.go | head`.

- [ ] **Step 5: Add an integration test for the bail path**

Add to `internal/conversation/orphan_tool_results_test.go` a test that runs `RunTurn` with a client returning a multi-tool_use assistant message and a context that cancels mid-execution, then asserts `result.Messages` contains an `is_error` tool_result for each unfinished tool_use. Reuse the `scriptedClient`/`newTestRunner` helpers from Task 5. If wiring a mid-flight cancel is brittle in the harness, assert the simpler invariant instead: after a normal tool round, every `tool_use` id in the assistant message has a matching `tool_result` in `result.Messages` (this exercises the same injection guard with `len(orphans)==0` on the happy path, and a fault-injected tool error path produces the synthetic result). Pick the variant the existing harness supports; verify with `grep -n "func newTestRunner\|errorTool\|fakeTool" internal/conversation/run_test.go internal/tool/*_test.go | head`.

- [ ] **Step 6: Run tests to verify they pass**

Run: `go test ./internal/conversation/ -run 'TestSynthesizeOrphaned|TestRunTurn' -v`
Expected: PASS, including pre-existing tool-round tests (happy path produces zero orphans, so behavior is unchanged).

- [ ] **Step 7: Commit**

```bash
git add internal/conversation/orphan_tool_results.go internal/conversation/orphan_tool_results_test.go internal/conversation/run.go
git commit -m "feat(conversation): inject orphaned tool_results on mid-turn bail to avoid 400s"
```

---

## Task 8: Wire micro-compaction into the runner

**Files:**
- Create: `internal/conversation/micro_compact.go`
- Modify: `internal/conversation/run.go` (call `maybeMicroCompact` before `maybeAutoCompact`)
- Modify: `internal/conversation/types.go` (add `EnableMicroCompact bool`, `MicroCompactKeepLast int`, `MicroCompactDir string`)
- Test: `internal/conversation/micro_compact_test.go` (new)

**Interfaces:**
- New `func (r Runner) maybeMicroCompact(history []contracts.Message) ([]contracts.Message, *compactpkg.MicroResult, bool)` — runs deterministic micro-compaction over `history`, returns the (possibly) shortened history.
- Called in `RunTurn` before `runner.maybeAutoCompact` (run.go:183).

CC anchor (verified): micro-compaction runs **before** autocompact every turn — `/Users/sqlrush/agent/claude-code/src/query.ts:412-426` (`// Apply microcompact before autocompact`). It is a lightweight per-tool-result clearing pass keyed by `tool_use_id`. ccgo's `compact.MicroCompact(history, options) MicroResult` and `MicroCompactStored(...)` are pure/deterministic (no LLM) — `internal/compact/micro.go:364,369`. `MicroOptions{ KeepLast, MaxChars, Cache, CacheDir, CacheVersion, CacheTTL, Now, FailOnCacheError }` (`micro.go:25-34`); `MicroResult{ Summary, Digest, Cached, MessagesSummarized, MessagesKept, ... }` (`micro.go:36-45`). Confirm: `go doc ./internal/compact MicroCompact` and `go doc ./internal/compact MicroOptions`.

Important behavior verification (do this FIRST): read `MicroCompact`/`MicroCompactStored` bodies (`internal/compact/micro.go:364-428`) to learn exactly **what they return** — a `MicroResult` summary string, NOT a rewritten history. The runner step must therefore (a) compute the micro summary and (b) apply it to history per the package's intended contract. Read how the package documents application (look for any `Apply*`/`Replace*`/boundary helper in `compact/`): `grep -rn "func .*Micro\|Boundary\|Apply\|Replace" internal/compact/micro.go internal/compact/plan.go`. If the package exposes an apply/boundary helper, use it; if it only produces a summary, the runner step appends a micro boundary+summary message pair the same way auto-compact does (`run.go:189`) and trims summarized messages by count from `MessagesSummarized`/`MessagesKept`. **Match the package's existing contract; do not invent a new compaction format.**

- [ ] **Step 1: Write the failing test**

Create `internal/conversation/micro_compact_test.go`:
```go
package conversation

import (
	"testing"

	"ccgo/internal/contracts"
)

func TestMaybeMicroCompactDisabledNoop(t *testing.T) {
	r := Runner{} // EnableMicroCompact false
	history := []contracts.Message{
		{Type: contracts.MessageUser, Content: []contracts.ContentBlock{contracts.NewTextBlock("a")}},
	}
	out, result, ok := r.maybeMicroCompact(history)
	if ok || result != nil {
		t.Fatalf("disabled micro-compact must be a no-op; ok=%v result=%v", ok, result)
	}
	if len(out) != len(history) {
		t.Fatalf("history changed while disabled: %d -> %d", len(history), len(out))
	}
}

func TestMaybeMicroCompactRunsWhenEnabled(t *testing.T) {
	r := Runner{EnableMicroCompact: true, MicroCompactKeepLast: 1}
	// Build a history with several stale tool_result payloads so micro-compact
	// has something to clear. Reuse a builder if run_test.go has one.
	history := microCompactableHistory(t, 10)
	out, result, ok := r.maybeMicroCompact(history)
	if !ok || result == nil {
		t.Fatalf("expected micro-compact to run; ok=%v", ok)
	}
	if len(out) > len(history) {
		t.Fatalf("micro-compact must not grow history: %d -> %d", len(history), len(out))
	}
}
```
`microCompactableHistory` is illustrative — first confirm what input actually triggers a non-empty `MicroResult` by reading `MicroCompact` (`micro.go:364`). Build the test history to match the real trigger conditions (e.g. enough tool_result content beyond `KeepLast`). If the deterministic result depends on `MaxChars`, set `r.MicroCompactKeepLast`/a `MaxChars` field accordingly. Adapt the assertion to the package's real contract discovered in Step "Important behavior verification" above.

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/conversation/ -run TestMaybeMicroCompact -v`
Expected: FAIL — `undefined: maybeMicroCompact`; `EnableMicroCompact`/`MicroCompactKeepLast` undefined.

- [ ] **Step 3: Implement maybeMicroCompact**

Add to `internal/conversation/types.go` `Runner` struct (near `AutoCompact`, line ~131):
```go
	EnableMicroCompact        bool
	MicroCompactKeepLast      int
	MicroCompactMaxChars      int
	MicroCompactDir           string
```

Create `internal/conversation/micro_compact.go` (final shape depends on the verified package contract from the pre-step; this is the skeleton):
```go
package conversation

import (
	compactpkg "ccgo/internal/compact"
	"ccgo/internal/contracts"
)

// maybeMicroCompact runs deterministic micro-compaction over history before the
// model turn (mirrors CC: microcompact runs before autocompact). Returns the
// (possibly) shortened history, the result, and whether anything was compacted.
// It never calls the model and never mutates the input slice.
func (r Runner) maybeMicroCompact(history []contracts.Message) ([]contracts.Message, *compactpkg.MicroResult, bool) {
	if !r.EnableMicroCompact || len(history) == 0 {
		return history, nil, false
	}
	options := compactpkg.MicroOptions{
		KeepLast: r.MicroCompactKeepLast,
		MaxChars: r.MicroCompactMaxChars,
		CacheDir: r.MicroCompactDir,
	}
	result := compactpkg.MicroCompact(history, options)
	if result.MessagesSummarized == 0 {
		return history, nil, false
	}
	// Apply per the compact package's contract (verified in the pre-step):
	// either via an exposed apply/boundary helper, or by appending a boundary +
	// summary message pair and trimming summarized messages — matching how
	// auto-compact applies its plan (run.go:189). Build a NEW slice; never
	// mutate the input.
	compacted := applyMicroResult(history, result) // implement per verified contract
	return compacted, &result, true
}
```
Implement `applyMicroResult` to match the **verified** package contract from the pre-step. If `compact` already exposes an apply/plan helper for micro results, call it and delete this local helper. If not, build it analogously to `BuildPlan`/the auto-compact application, producing a new slice (copy-then-replace), keeping the last `KeepLast` messages and substituting earlier ones with the micro summary. Keep this file under 350 lines.

- [ ] **Step 4: Wire the call into RunTurn**

In `internal/conversation/run.go`, immediately before the `maybeAutoCompact` block (line 183), add:
```go
		if microHistory, microResult, ok := runner.maybeMicroCompact(history); ok {
			history = microHistory
			result.Messages = append(result.Messages, microCompactMessages(*microResult)...)
		}
```
`microCompactMessages` builds the renderable boundary/summary message(s) for the result (reuse the auto-compact pattern at run.go:189 — `compactResult.Plan.Boundary`, `compactResult.Plan.Summary`). If `maybeMicroCompact` already returns the messages to append (preferred — keep it self-contained), append them directly and drop this helper. Confirm placement does not conflict with the deferred-tools-delta block (run.go:195) — micro-compact must run first.

- [ ] **Step 5: Run tests to verify they pass**

Run: `go test ./internal/conversation/ -run 'TestMaybeMicroCompact|TestRunTurn' -v`
Expected: PASS, including pre-existing `RunTurn` tests (disabled by default → no-op).

- [ ] **Step 6: Commit**

```bash
git add internal/conversation/micro_compact.go internal/conversation/micro_compact_test.go internal/conversation/run.go internal/conversation/types.go
git commit -m "feat(conversation): wire deterministic micro-compaction into the turn loop"
```

---

## Task 9: Full-suite regression + build/vet gate

**Files:**
- No new production code unless a regression surfaces; this task is the phase gate.

- [ ] **Step 1: Build and vet**

Run:
```bash
go build ./... && go vet ./...
```
Expected: clean.

- [ ] **Step 2: Full test suite with race detector**

Run:
```bash
go test -race ./...
```
Expected: all PASS. Pay special attention to `internal/conversation/`, `internal/api/anthropic/`, `internal/contracts/`, `internal/compact/`, and `cmd/claude/` (the latter constructs runners; confirm new `Runner` fields default safely — all are zero-valued and gated).

- [ ] **Step 3: Confirm the phase gate ("thinking visible, cache hits, no mid-turn 400s")**

Run targeted assertions proving each deliverable is live:
```bash
# Cache breakpoints called in the request path (production caller now exists):
grep -rn "AddCacheBreakpoints" internal/conversation/
# Beta header current:
grep -rn "prompt-caching-scope-2026-01-05" internal/api/anthropic/betas.go
# Thinking + signature collected:
grep -rn "thinking_delta\|signature_delta" internal/api/anthropic/stream_accumulator.go
grep -rn "Signature" internal/contracts/messages.go
# stop_reason switch wired:
grep -rn "classifyStopReason" internal/conversation/run.go
# orphan tool_result injection wired:
grep -rn "synthesizeOrphanedToolResults" internal/conversation/run.go
# micro-compact wired:
grep -rn "maybeMicroCompact" internal/conversation/run.go
```
Expected: every grep returns at least one production (non-test) hit.

- [ ] **Step 4: Commit (only if a regression fix was needed)**

```bash
git add -A
git commit -m "test(conversation): phase-3 agent-loop wiring regression gate"
```

---

## Self-Review

**Spec coverage (Phase-3 brief — roadmap §5 "Phase 3" + gap-audit §4.D items 9–12):**
- Call `AddCacheBreakpoints` in the request path → Task 2. ✓
- Fix stale cache-scope beta header (`2024-07-31` → `2026-01-05`) → Task 1. ✓
- Extended thinking: set `Request.Thinking` → Task 4; add `ContentBlock.Signature` → Task 3; accumulator collects thinking + signature deltas → Task 4. ✓
- `stop_reason` control flow: max_tokens recovery → Task 5; pause_turn resume → Task 5; refusal surface → Task 5; ctx-window-exceeded recovery → Task 6. ✓
- Inject orphaned `tool_result` on mid-turn bail → Task 7. ✓
- Wire micro-compaction → Task 8. ✓
- Regression/gate → Task 9. ✓

**Discrepancies between roadmap/gap-audit and the real CC code (flagged, decisions made):**
- **pause_turn is NOT handled in the CC reference** (`grep -rn "pause_turn" /Users/sqlrush/agent/claude-code/src` → zero matches), yet the roadmap brief and gap-audit item 11 require it. pause_turn is a real standard-API stop_reason for long server-tool turns, so Task 5 implements a minimal resume (re-send unchanged history) and explicitly flags it as a deliberate addition beyond the reference.
- **Cache-scope header value:** the gap-audit said `2026-01-05`; the CC code confirms exactly `prompt-caching-scope-2026-01-05` (`constants/betas.ts:17-18`). Task 1 uses the code-verified value, not memory.
- **ctx-window-exceeded:** CC reuses the max-output-tokens recovery message path for `model_context_window_exceeded` (`claude.ts:2279-2292`) and drives compaction from the overflow loop (`query.ts:1070-1124`). ccgo already has full auto-compaction, so Task 6 recovers via a one-shot forced compaction + retry (closer in spirit and using existing machinery) rather than only surfacing a message.
- **micro-compaction is deterministic on both sides:** ccgo's `compact.MicroCompact` is a pure function (no LLM), and CC's microcompact is a per-tool_use_id clearing pass — Task 8 wires the pure function before auto-compact, matching CC's ordering (`query.ts:412-426`).

**Verified ccgo anchors (point-of-use confirmations are inline in each task):** `AddCacheBreakpoints` zero callers (`cache.go:11`); `betas.go:10` stale header; `Request.Thinking map[string]any` (`types.go:24`) read-but-never-set; `ContentBlock` no Signature (`messages.go:31-44`); accumulator drops thinking/signature (`stream_accumulator.go:31-44`); turn loop (`run.go:205-256`) never branches on stop_reason; `MicroCompact` pure & uncalled (`micro.go:364`); model thinking capability (`model.go:21-33,74`); existing overflow helpers (`retry.go:27,117,134`, `client.go:449-466`).

**Immutability check:** every task either operates on the per-turn `runner := *r` copy (`run.go:163`) or builds new slices/maps (`AddCacheBreakpoints` already copies; `thinkingRequestConfig` returns a fresh map; `synthesizeOrphanedToolResults`/`maybeMicroCompact` return new slices; `forceCompact` copies `AutoConfig` before mutating). The shared `*r` base is never mutated.

**Error handling:** every new branch returns wrapped/explicit errors; `ctx.Err()` is checked after the bail-path injection (Task 7); no error is swallowed.

**Placeholder scan:** no `t.Skip` placeholders. Two tasks (5/6/7/8) instruct the implementer to **bind to the existing `run_test.go` fake-client and helpers** rather than invent new ones — the grep to find them is flagged at the point of use. Task 8 requires reading the `compact` package's apply contract before implementing `applyMicroResult` (flagged as a mandatory pre-step) so the runner matches the package's existing compaction format instead of inventing one.

**No new dependencies.** All work uses existing packages.
