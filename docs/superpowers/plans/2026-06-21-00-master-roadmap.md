# ccgo → 100% Functional Parity — Master Roadmap

> **Status:** Phase 1 (interactive runtime) DONE & merged to `main` (verified in code, 2026-06-21).
> **This is the parent document** for all per-phase implementation plans. Each phase below has
> its own TDD plan file in this directory; this doc owns the scope, dependency order, shared
> engineering constraints, milestone gates, and the plan index.

**Source of truth:** code on both sides, not self-reported roadmap docs.
- ccgo (Go): `/Users/sqlrush/ccgo`
- Claude Code reference (TypeScript): `/Users/sqlrush/agent/claude-code/src`
- Gap audit (code-verified, locked scope §10): `docs/gap-audit-2026-06-21.md`

---

## 1. What "100%" means (locked 2026-06-21)

Not a literal 1:1 of CC's ~511K TS. The committed target is **three pillars**:

> **本地可运行 + 走标准 Anthropic API 的全部功能集 + UI 交互全部复刻**
> (Locally-runnable + complete standard-Anthropic-API feature set + full UI/interaction replication.)

**IN scope (the committed 100%):** all local logic (tools, agent loop, permissions, hooks,
sessions/memory/compact, rewind, CLAUDE.md hierarchy + @import, plugins, skills, output styles,
config); everything over the standard Anthropic API (streaming, extended thinking, prompt caching,
model fallback, official WebFetch/WebSearch server tools, cost accounting); MCP over open protocols
(stdio/SSE/HTTP/WS, remote OAuth RFC 8414/9728/7591, `claude mcp` CLI, server mode); local
orchestration (real sync + async subagents, real local Team, git-worktree isolation); OS sandbox
(seatbelt / landlock+seccomp); local SDK control protocol; **full TUI/dialog replication (first-class).**

**OUT of scope (control is outside this codebase — NOT a Go capability gap):** cloud remote stack
(teleport, RemoteAgentTask, CCR relay, session server, cloud cron, remote-setup/env CLIs);
GitHub/Slack apps + session share; companion-app surfaces (IDE handshake, desktop, Chrome, mobile
pairing, voice STT); server-driven feature flags / A-B (statsig); internal telemetry + debug-only
commands.

**Gray zone (IN, flagged risk):** interactive OAuth login uses the official client's
credentials/endpoints — technically reproducible, ToS/account-policy gray area.

**Size & pace (this definition):** ~65–70K new prod Go LOC (~110–115K incl. tests). At the
adjusted integration-heavy pace (~5K total LOC/active day) ≈ **4–6 weeks**; conservative ≈ 7–9 weeks.
**UI full-replication (~14K prod LOC) is the single largest line item and on the critical path.**

---

## 2. Where we are (code-verified)

**Phase 1 shipped:** `cmd/claude/main.go` imports `internal/repl`; `go.mod` has
`golang.org/x/term v0.44.0`; `internal/repl/` has the terminal driver, stdin segmenter, event loop,
live render, executor `PermissionAsker` seam, and interactive permission dialog bridge. `claude`
(no `--print`) is a real REPL, not the old `scaffold ready` stub.

**The structural insight that makes the rest cheap — "library built, glue missing":**

| Built (tested) | Where | Wired into running path? |
|---|---|---|
| Full TUI (~21K LOC) | `internal/tui/` | ⚠️ Phase 1 wired the core REPL screen; most screens/dialogs still unrendered |
| Micro-compaction | `internal/compact/micro.go` | ❌ runner never calls it (Phase 3) |
| Prompt-cache breakpoints | `internal/api/anthropic/cache.go` | ❌ zero callers (Phase 3) |
| Permission decision persistence | `permissions.Engine.ApplyUpdate` | ❌ no caller writes settings.json (Phase 2) |
| OAuth PKCE primitives | `internal/auth/oauth.go` | ⚠️ no callback + no code exchange (Phase 4) |

→ Much of the remaining work is **wiring**, far cheaper than green-field. Each phase plan must
**verify the current wiring state in code first** (grep for callers) before assuming work is needed.

---

## 3. Dependency graph & phase order

```
Phase 1 (DONE) ── interactive runtime + executor Asker
   │
   ├─► Phase 2  Interactive completeness (UI 复刻主体)   [critical path, ~14K]
   │
   ├─► Phase 3  Agent-loop wiring (cache/thinking/stop-reason)   [~4K]
   │
   ├─► Phase 4  Auth / OAuth login                        [~3K]
   │
   ├─► Phase 5  Tools (prompts, web*, plan/ask, LSP)       [~7K]
   │
   ├─► Phase 6  (split — see below)                        [~15K]
   │     ├ 6a MCP CLI + remote OAuth
   │     ├ 6b Commands coverage
   │     ├ 6c Memory: CLAUDE.md hierarchy + @import + rewind
   │     └ 6d Hooks lifecycle
   │
   └─► Phase 7  Sandbox + real local Team + local SDK      [~6K, can trail]
```

**Hard dependencies (must precede):**
- Everything depends on **Phase 1** (done).
- **Phase 2's** "Allow Session" persistence depends on `permissions.Engine.ApplyUpdate` (exists) +
  a settings writer — independent of other phases.
- **Phase 5's** `EnterPlanMode`/`ExitPlanMode`/`AskUserQuestion` tools need **Phase 2's** dialog
  rendering to be user-visible (the tool can land first behind the seam; the UI ceremony lands in P2).
- **Phase 6b** (`/login` `/logout`) overlaps **Phase 4**; keep auth commands in Phase 4, the rest in 6b.
- **Phase 6c rewind** depends on session transcript writers (exists, parse-only today).
- **Phase 3, 4, 5, 6a–d, 7 are mutually independent** and can be planned/executed in parallel after
  Phase 1. Phase 2 is the only one that competes for the same `internal/tui`/`internal/repl` files,
  so sequence Phase 2 work to avoid colliding with Phase 5's plan-mode UI ceremony.

**Recommended execution sequence** (gap-audit §9): 2 → 3 → 4 → 5 → 6a/6b/6c/6d → 7. Phase 2 is
biggest and on the critical path, so it can run alongside the smaller 3/4 if a second worker exists.

---

## 4. Phase table

| Phase | Plan doc | Subsystem | Prod LOC (est) | Gate (done when…) |
|---|---|---|---:|---|
| 1 ✅ | `2026-06-21-interactive-runtime-phase1.md` | Interactive runtime | ~8.5K | `claude` is a real REPL (DONE) |
| 2 | `…-phase2-interactive-completeness.md` | UI/TUI full replication | ~14K | every CC screen/dialog rendered & interactive; perms persist |
| 3 | `…-phase3-agent-loop-wiring.md` | cache/thinking/stop-reason | ~4K | thinking visible, cache hits, no mid-turn 400s |
| 4 | `…-phase4-auth-oauth.md` | OAuth login + keychain | ~3K | new user logs in from zero; token in keychain |
| 5 | `…-phase5-tools.md` | tool prompts + web + plan/ask + LSP | ~7K | tool behavior matches CC |
| 6a | `…-phase6a-mcp-cli-remote-oauth.md` | `claude mcp` CLI + remote OAuth | ~6K | add/list/remove servers via CLI; remote OAuth flow works |
| 6b | `…-phase6b-commands.md` | slash + CLI command coverage | ~7.5K | command coverage ~full; `/resume` actually resumes |
| 6c | `…-phase6c-memory-claudemd-rewind.md` | CLAUDE.md hierarchy + @import + rewind | ~5.5K | full memory hierarchy + @import + rewind/checkpoint |
| 6d | `…-phase6d-hooks-lifecycle.md` | hooks lifecycle + types | ~3.5K | all CC hook events fire; parallel deny>ask>allow |
| 7 | `…-phase7-sandbox-team-sdk.md` | OS sandbox + local Team + SDK | ~6K | sandbox enforces; Team runs real teammates; SDK importable |

(LOC are order-of-magnitude from gap-audit §7; each phase plan refines its own estimate.)

---

## 5. Per-phase briefs (the spec each phase plan elaborates)

> Each brief lists: **target behavior**, **CC reference anchors** (where to read the real impl),
> **ccgo current state** (what exists / what's unwired), and **deliverables**. The phase plan turns
> these into Task-by-Task TDD steps with real test code and exact file:line.

### Phase 2 — Interactive completeness (UI 复刻主体)
- **Target:** "usable REPL" → CC-parity interaction.
- **CC anchors:** `src/components/`, `src/screens/`, `src/ink/` (React/Ink — map behavior, not code).
- **ccgo state:** `internal/tui/` (~21K, mostly unwired beyond Phase 1's core screen);
  `permissions.Engine.ApplyUpdate` exists, no settings-writer caller.
- **Deliverables:** resize/SIGWINCH live; spinner/progress; Ctrl-C mid-turn interrupt; **"Allow
  Session" + persisted rules** (`Engine.ApplyUpdate` → settings.json writer); slash-command menu +
  autocomplete; resume/continue picker; vim mode wiring; rich rendering (StructuredDiff, tool-use/
  tool-result, HelpV2, status/cost/context panels, Doctor, onboarding + TrustDialog, theme picker,
  `/memory` selector, notifications, keybindings); full permission dialog set (Bash, FileEdit,
  FileWrite, AskUserQuestion, EnterPlanMode, ExitPlanMode, PowerShell, Skill, WebFetch, Filesystem,
  NotebookEdit, SedEdit). Mode-switch UI + indicators (plan/acceptEdits/bypass).

### Phase 3 — Agent-loop wiring
- **Target:** correct streaming control-flow + caching + thinking, matching the standard API.
- **CC anchors:** the conversation/query loop in `src/` (stream handling, stop_reason switch,
  cache breakpoint insertion, thinking deltas + signature).
- **ccgo state:** `internal/api/anthropic/cache.go` (`AddCacheBreakpoints`, zero callers);
  `internal/conversation/` runner; `internal/compact/micro.go` (unwired); `ContentBlock` lacks
  `Signature`; accumulator drops thinking/signature deltas; cache-scope beta header stale.
- **Deliverables:** call `AddCacheBreakpoints` in the request path + fix beta header; extended
  thinking (`Request.Thinking` set, `ContentBlock.Signature` field, accumulator collects thinking +
  signature); `stop_reason` control flow (max_tokens recovery, pause_turn resume, refusal surface,
  ctx-window-exceeded recovery); inject orphaned `tool_result` on mid-turn bail; wire micro-compact.

### Phase 4 — Auth / OAuth
- **Target:** first-time interactive login from zero.
- **CC anchors:** OAuth login flow (PKCE, callback listener, browser open, code exchange), keychain.
- **ccgo state:** `internal/auth/oauth.go` (PKCE primitives + refresh only; no callback, no code
  exchange); token stored plaintext.
- **Deliverables:** local callback HTTP listener; open browser; `authorization_code` exchange;
  `/login` `/logout` + `claude auth` CLI; token keychain (macOS/Linux/Windows) replacing plaintext;
  `apiKeyHelper` support. **Flag the ToS gray-zone in the plan.**

### Phase 5 — Tools
- **Target:** tool behavior/prompts match CC.
- **CC anchors:** Bash/PowerShell tool prompts (~370 lines: git-commit/PR workflow, quoting, tool
  preference), WebFetch (secondary-model summarization), WebSearch (`web_search_20250305` server
  tool), `AskUserQuestion`/`EnterPlanMode`/`ExitPlanMode` tools, LSPTool 9-op.
- **ccgo state:** Bash/PS prompts are one-line stubs; WebFetch returns raw text; WebSearch scrapes
  DuckDuckGo; no plan/ask interactive tools; only `LSPDiagnostics`; Bash cwd not persisted across
  calls; TodoWrite old schema.
- **Deliverables:** full Bash/PS prompts; WebFetch secondary-model summarize; WebSearch official
  server tool; `AskUserQuestion`/`EnterPlanMode`/`ExitPlanMode`; LSPTool 9-op; Bash cwd persistence;
  TodoWrite `activeForm` schema; `StructuredOutput`; Enter/ExitWorktree; Config tool.

### Phase 6a — MCP CLI + remote OAuth
- **Target:** manage MCP servers from CLI; connect to OAuth-protected remote servers.
- **CC anchors:** `claude mcp` subcommand group; MCP OAuth discovery (RFC 8414/9728) + DCR (RFC 7591).
- **ccgo state:** MCP client core strong (4 transports); no `claude mcp` CLI; no remote OAuth/DCR;
  no `claude mcp serve` full tool set; no claudeai-proxy/ide transports; no auto-reconnect/backoff.
- **Deliverables:** `claude mcp add/list/remove/get` CLI; remote-server OAuth discovery + DCR +
  token cache; `claude mcp serve` full tool set; elicitation UI hook; reconnect/backoff.

### Phase 6b — Commands coverage
- **Target:** slash + CLI command coverage from ~22% to ~full (excluding OUT-of-scope/debug cmds).
- **CC anchors:** the command registry in `src/` (each slash command + each `claude <subcmd>`).
- **ccgo state:** 17/~78 commands, most text-only; `/agents` `/permissions` missing; `/resume`
  doesn't resume; missing `/theme /effort /context /export /init /review /ide /doctor /vim /hooks`;
  CLI `doctor/update/agents/completion` missing.
- **Deliverables:** implement the in-scope command set with real behavior; make `/resume` resume;
  `/agents` `/permissions` editors; `/context` `/export` `/init` `/review` `/doctor`; CLI
  `doctor/update/agents/completion`. (Exclude `/login` `/logout` → Phase 4; exclude debug cmds.)

### Phase 6c — Memory: CLAUDE.md hierarchy + @import + rewind
- **Target:** full memory hierarchy + import resolution + rewind/checkpoint.
- **CC anchors:** CLAUDE.md discovery (User/Managed/`.claude`/rules/`*.local` scopes), `@import`
  resolver, rewind/checkpoint snapshot writer + restore.
- **ccgo state:** CLAUDE.md walks parent bare files only; `@import` not resolved; rewind absent
  (transcript *parses* snapshot lines but nobody *writes* them); no cost persistence/restore;
  no post-compact file restoration; no `~/.claude/history.jsonl`.
- **Deliverables:** full CLAUDE.md scope hierarchy; `@import` resolution (with cycle guard);
  rewind/checkpoint snapshot write + restore + `/rewind`-style UI hook; cost persist/restore on
  resume; post-compact file restoration; `history.jsonl`.

### Phase 6d — Hooks lifecycle
- **Target:** all CC hook events fire; correct multi-hook semantics.
- **CC anchors:** hook event taxonomy (28 events incl. SessionStart/lifecycle, prompt, agent),
  parallel deny>ask>allow resolution.
- **ccgo state:** 8/28 events; SessionStart & lifecycle never fire; no prompt/agent hook types;
  multi-hook is sequential short-circuit, not parallel deny>ask>allow.
- **Deliverables:** fire SessionStart + lifecycle; add prompt/agent hook types; parallel hook
  execution with deny>ask>allow precedence; complete the event taxonomy.

### Phase 7 — Sandbox + local Team + local SDK
- **Target:** real OS sandbox, real local Team execution, importable local SDK.
- **CC anchors:** seatbelt profile (macOS), landlock+seccomp (Linux); Team dispatch/coordinate;
  SDK control protocol (`control_request/response`, `canUseTool`, interrupt, set_model).
- **ccgo state:** `dangerouslyDisableSandbox` is a flag with zero enforcement (security regression);
  `callTeamDispatch/Coordinate/Schedule` only append messages (no teammate runs); no SDK control
  protocol / importable entrypoint.
- **Deliverables:** seatbelt (macOS) + landlock/seccomp (Linux) enforcement honoring the flag; real
  in-process Team runner (real teammates, real coordination); async/background agents
  (`run_in_background`); Task schema `model`/`isolation`; local SDK control protocol + importable
  entrypoint.

---

## 6. Shared engineering constraints (apply to EVERY phase plan)

Copied into each phase plan's "Global Constraints". Verbatim values:

- **Module/toolchain:** `ccgo`, `go 1.26` (from `go.mod`).
- **Immutability (CRITICAL):** never mutate shared structs in place; return new copies. Copy the
  `conversation.Runner` value per turn before setting `OnEvent`/`Tools.Asker` (existing pattern).
  `permissions.Engine.ApplyUpdate` already returns a **new** engine — honor that.
- **Many small files:** one responsibility per file; target 150–350 lines (800 hard max).
- **Errors handled explicitly at every level; never swallow.** Terminal raw-mode `restore` and any
  acquired resource MUST be released on every exit path (`defer`).
- **Input validation at boundaries:** validate all external data (API responses, user input, file
  content, MCP server output); fail fast with clear messages.
- **No new third-party deps** unless the plan justifies it explicitly. Phase 1 added only
  `golang.org/x/term`. No bubbletea/tcell/charm.
- **Non-TTY safety:** interactive paths MUST NOT call `term.MakeRaw` when stdin/stdout isn't a tty;
  fall back to line mode. Tests MUST NOT depend on a real tty.
- **TDD:** every task writes a failing test first, then minimal code. Commit after each task.
  Run package tests with `go test ./internal/<pkg>/ -run TestName -v`; full suite `go test ./...`.
- **Verify against real code, distrust roadmap docs:** every assumed type name, field, constant, or
  CC behavior MUST be confirmed with `go doc`/`grep` (ccgo side) or by reading
  `/Users/sqlrush/agent/claude-code/src` (CC side) before writing the test — flag the exact command
  at the point of use, as Phase 1's plan does.
- **Security:** no hardcoded secrets; tokens in keychain not plaintext (Phase 4); sandbox flag must
  actually enforce (Phase 7); never leak sensitive data in errors.

---

## 7. Risks & open decision points

1. **UI (Phase 2) is the biggest single item and on the critical path.** Its completion gates "a
   demo-able complete product". Decision: run it first/alone, or interleave with smaller 3/4.
2. **OAuth (Phase 4) is a ToS gray zone.** Decision (policy, not technical): do it, scope it, or
   ship API-key-only and leave OAuth behind a flag.
3. **Pace assumption:** 4–6 weeks assumes ~5K LOC/active day. Remaining work is integration glue
   (slower than early mechanical table-filling) — the largest schedule risk.
4. **Phase 7 cloud-adjacent items are OUT of scope** — keep Team/SDK strictly local; do not creep
   into RemoteAgentTask/teleport.

---

## 8. Verification strategy & milestone gates

- **Per-task:** failing test → minimal impl → green → commit (TDD, enforced in each plan).
- **Per-phase gate:** the "Gate" column in §4. A phase is done only when its gate is demonstrable
  (a test or a documented manual smoke test), `go build ./...` + `go vet ./...` clean, full
  `go test ./...` green.
- **Milestones:**
  - **M-T1 (usable interactive product):** Phase 2 core + Phase 3 + Phase 4 → a person can log in,
    chat, stream, see thinking, approve tools interactively. (~2–3 weeks)
  - **M-T2 (solid functional parity):** + Phase 5 + Phase 6a–d → tools/commands/MCP/memory/hooks at
    parity. (~4–6 weeks)
  - **M-T3 (near-100% local):** + Phase 7 → sandbox, real Team, SDK. (~6–8 weeks)
- **Cross-phase regression:** after integrating each phase, run the full suite; the non-TTY headless
  path (`--print`) must never regress.

---

## 9. Plan index

| Order | File | Status | Tasks |
|---|---|---|---|
| — | `2026-06-21-00-master-roadmap.md` (this doc) | living | — |
| 1 | `2026-06-21-interactive-runtime-phase1.md` | ✅ implemented | 7 |
| 2 | `2026-06-21-phase2-interactive-completeness.md` | ✅ implemented | 13 |
| 3 | `2026-06-21-phase3-agent-loop-wiring.md` | ✅ implemented | 9 |
| 4 | `2026-06-21-phase4-auth-oauth.md` | ✅ implemented | 7 |
| 5 | `2026-06-21-phase5-tools.md` | ✅ implemented | 10 |
| 6a | `2026-06-21-phase6a-mcp-cli-remote-oauth.md` | ✅ implemented | 10 |
| 6b | `2026-06-21-phase6b-commands.md` | ✅ implemented | 13 |
| 6c | `2026-06-21-phase6c-memory-claudemd-rewind.md` | ✅ implemented | 8 |
| 6d | `2026-06-21-phase6d-hooks-lifecycle.md` | ✅ implemented | 9 |
| 7 | `2026-06-21-phase7-sandbox-team-sdk.md` | ✅ implemented | 9 |

**All phase plans written 2026-06-21** (~18K lines total; 88 TDD tasks across P2–P7). Each was
authored by reading the real code on both sides (ccgo + CC reference), not the roadmap docs.

Each plan is self-contained and produces working, testable software on its own. Execute via
`superpowers:subagent-driven-development` (fresh subagent per task + review) or
`superpowers:executing-plans` (batched with checkpoints).

---

## 10. Code-verified corrections to the gap audit (found while writing the plans)

Writing each plan against the **real code on both sides** surfaced places where the gap audit
(written from a faster survey) was stale or imprecise. These refine — not replace — the locked
scope. Net effect: several subsystems are **cheaper** than audited; Phase 7 is **slightly larger**.

| Phase | Audit said | Code actually shows | Effect |
|---|---|---|---|
| 2 | UI incl. StructuredDiff + settings-writer to build | `native.BuildColorDiff` + `config.WriteSettingsDocument`/`ProjectSettingsPath` already exist | smaller; P2 is wiring + a thin `PermissionUpdate`→doc bridge |
| 3 | needs `pause_turn` resume (item 11) | `pause_turn` is **absent** from the CC reference (0 grep hits) | implement minimal resume; flagged as a deliberate addition |
| 4 | "refresh only", ~2K LOC | `BuildAuthURL`, all PKCE primitives, exact endpoints + scopes already present + tested | cheaper; mostly callback+exchange+storage swap |
| 4 | (keychain) | CC just shells to `/usr/bin/security`; Linux/Win has a TODO file-store gap | **no new dep**; macOS via `os/exec`, others reuse chmod-0600 file store |
| 5 | LSP 9-op = completion/rename/format; ExitPlanMode takes a `plan` param | 9 ops are navigation+call-hierarchy; ExitPlanMode reads plan from **disk** | follow code over audit |
| 6a | `claude mcp serve` + elicitation missing; ~6K LOC | serve server + elicitation **protocol** already complete; net-new ≈ 1.8–2.5K | much smaller; CLI wiring + remote-OAuth front-half only |
| 6b | 17 commands; `/effort` missing; `/resume` absent | 18 builtins; `EffortLevel` exists in `contracts.Settings`; `/resume` has working read/list (only live-resume missing); `claude completion` ships in no external CC build | re-scoped to live-effect wiring + greenfield completion |
| 6c | "no `~/.claude/history.jsonl`" | the store is **fully implemented** (byte-matches CC) — real gap is **zero callers** | Task wires it, doesn't build it |
| 6d | "no prompt/agent hook types"; 8/28 events | `UserPromptSubmit`/`Stop`/`SubagentStop` already fire; CC has **27** events, ~11 OUT of scope → ~16 in-scope, 8 already work | gap narrows to 6 events + sequential→parallel `deny>ask>allow` |
| 7 | "one real local subagent" | the single subagent is **also record-only** (no model loop); seatbelt/landlock profiles live in an external CC package, not `src` | slightly larger; profiles implemented natively |
| 7 | (sandbox dep) | `x/sys` lacks typed Landlock wrappers | promote `x/sys` to direct + **one new dep** `github.com/landlock-lsm/go-landlock`; seccomp hand-rolled; macOS uses OS `sandbox-exec` |

**Recurring confirmation of the §2 thesis:** repeatedly the *library is built and the glue is
missing* (history.jsonl, mcp serve, elicitation protocol, OAuth primitives, settings writer,
StructuredDiff). The remaining work skews even further toward **wiring** than the audit implied.

**New dependencies introduced by the plans (only where justified, per §6):**
- Phase 4: none (keychain via `os/exec` → `/usr/bin/security`).
- Phase 7: `github.com/landlock-lsm/go-landlock` + promote `golang.org/x/sys` to a direct dep.

**Cross-phase coupling to manage during execution (flagged by multiple plans):**
- Phase 4 OAuth callback/exchange machinery is **shared** by Phase 6a remote-MCP OAuth — build the
  canonical version in Phase 4; 6a reuses it (6a gates on it with an injected `Authorizer` so it
  stays testable if built first).
- A **settings/permission writer** is touched by Phase 2 ("Allow Session"), Phase 6b
  (`/permissions`), and Phase 6d (hook `settingsOverride`) — keep one writer, coordinate callers.
- Phase 2 competes with Phase 5 (plan-mode UI) and Phase 6b (command router) for
  `internal/repl`/`internal/tui` files — sequence Phase 2 first or isolate in a worktree.
- **Phase 2 → Phase 6b (implemented 2026-06-21):** Phase 2 BUILT + TESTED the help / resume /
  theme / memory overlays and the TrustDialog, plus the slash-command *menu* (opens on `/`). But
  *opening* the help/resume/theme/memory overlays and *executing* a selected `/command` need the
  slash-command **dispatch** (the command router), which is Phase 6b's deliverable. Until 6b lands,
  a selected `/command` is sent to the model as literal text and those four overlays have no trigger.
  This is a **conscious, documented deferral** (Phase-2 opus whole-branch review, finding #2), not a
  gap: the components exist and pass tests; 6b wires `InteractiveOptions.{OnOverlay,ResumeEntries,
  Themes,MemoryFiles,Trust}` + a slash dispatcher in `cmd/claude`. The TrustDialog also needs
  `cmd/claude` to set `opts.Trust` from a first-run folder scan (6b/onboarding).
