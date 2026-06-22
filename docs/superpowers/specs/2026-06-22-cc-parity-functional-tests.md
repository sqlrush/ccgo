# CC-Parity Functional Test Checklist — Design Spec

**Date:** 2026-06-22
**Goal:** A functional test checklist that, referencing Claude Code's *actual* feature surface,
aims to cover **100% of CC's functionality**. Each item is a pass/fail functional check with a
current-status verdict for ccgo. The checklist is then the source for (a) a wiring + optimization
worklist (drive the ⚠️/❌ rows to ✅) and (b) a regression test suite (the AUTO rows → Go `e2e/`).

**Source of truth:** CC reference source `/Users/sqlrush/agent/claude-code/src` (audited directly,
NOT self-reported roadmap docs), cross-checked against ccgo code for current status.

**Locked scope reference:** `docs/gap-audit-2026-06-21.md` §10 (IN/OUT). OUT-of-scope CC features
are still ENUMERATED here (marked `N/A`) so the checklist reflects the full CC surface.

---

## 1. Decisions (confirmed with user 2026-06-22)

1. **Scope:** enumerate ALL CC features; OUT-of-scope items listed and marked `N/A` + reason.
2. **Execution model:** layered — `AUTO` (headless `claude --print` / programmatic, machine-assertable)
   + `MANUAL` (interactive TUI / dialogs, human-run with expected result).
3. **Output:** checklist first (Markdown), then sediment the `AUTO` rows into a runnable Go suite.

---

## 2. Deliverable & row format

One master Markdown checklist: `docs/cc-parity/functional-tests.md` (+ this design spec).
Organized into chapters by feature area (§4). Every test item is one table row:

| Column | Meaning |
|---|---|
| **ID** | Stable id, `AREA-FEATURE-NN` (e.g. `TOOL-BASH-03`, `MCP-OAUTH-02`). Referenced by the worklist + suite. |
| **Feature** | The specific CC behavior under test. |
| **Layer** | `AUTO` (headless-assertable) / `MANUAL` (interactive TUI) / `N/A` (out-of-scope). |
| **Test (given → when → then)** | Precondition → action → **expected result** (the pass criterion). |
| **CC ref** | `src/…:line` proving CC actually does this (anti-hallucination). |
| **Status** | `✅ pass` / `⚠️ built-not-wired` (code+tests exist, unreachable in the running app) / `❌ missing` / `N/A`. |

The **Status** column IS the gap analysis. Roll-ups at the end of each chapter + a top summary
(counts per status). `⚠️` rows → wiring work; `❌` rows → true gaps; together = the worklist.

### Example rows (so the format is concrete)

```
| TOOL-BASH-01 | Bash runs a shell command, returns stdout/stderr/exit | AUTO | given a repo; when `claude --print "run: echo hi"` triggers Bash `echo hi`; then result contains "hi", exit 0 | src/tools/BashTool/*.ts | ✅ pass |
| PERM-MODE-03 | Shift+Tab cycles permission mode (default→acceptEdits→plan→bypass) | MANUAL | given the REPL; when Shift+Tab pressed 4×; then status line indicator cycles and returns to default | src/components/PromptInput/PromptInputFooterLeftSide.tsx:70 | ✅ pass |
| CMD-RESUME-02 | `/resume` opens an interactive session picker | MANUAL | given prior sessions; when `/resume` (no arg); then a picker dialog lists same-repo sessions, arrow+enter loads one | src/screens/ResumeConversation.tsx | ⚠️ built-not-wired (functional resume works via `/resume <id|N>`; picker DIALOG needs P2 trigger) |
| SDK-QUERY-01 | `claude` exposes an importable SDK query() entrypoint over the CLI | AUTO | given the binary; when invoked in SDK/control mode; then control_request/response NDJSON drives a turn | src/entrypoints/agentSdkTypes.ts:112 | ⚠️ built-not-wired (sdk.Query exists+tested; no cmd/claude subcommand) |
| REMOTE-TELEPORT-01 | Teleport to a cloud remote agent session | N/A | — | src/… | N/A (cloud stack OUT of scope §10) |
```

---

## 3. How the checklist is produced (Approach A — confirmed)

**Hybrid + real-source audit.** Per feature area (§4), a dedicated pass:
1. **Enumerate** CC's features for the area by reading `/Users/sqlrush/agent/claude-code/src`
   (cite file:line), seeded by the gap audit / phase plans as a checklist of where to look — but the
   CC source is authoritative for "what exists".
2. **Write** one functional test row per feature (given→when→then + expected), tagging the Layer.
3. **Determine ccgo status** by checking ccgo code (`grep`/`go doc`): is it implemented + wired
   (`✅`), built-but-unreachable (`⚠️`), missing (`❌`), or out-of-scope (`N/A`)?

Production is fanned out: one agent per feature area (parallel), each returning its area's rows; the
controller assembles the master doc. (Same method as the original gap audit — `dispatching-parallel-agents`.)
This is a research/enumeration deliverable, not a code build, so it does not go through writing-plans;
the writing-plans step applies later, to the wiring worklist the checklist produces.

---

## 4. Feature-area chapters (CC full surface)

1. **CLI & entrypoints** — `claude`, `--print`, all flags, subcommands (`mcp`/`auth`/`agents`/`doctor`/`update`/`completion`/`config`/`plugin`/`mcp serve`).
2. **Interactive REPL / TUI** — raw input + editing, live streaming render, spinner/progress, resize, Ctrl-C/ESC interrupt, vim mode, mode-switch UI, bracketed paste, alt-screen.
3. **Overlays & dialogs** — slash menu + autocomplete, resume picker, theme picker, `/memory` selector, HelpV2, Doctor screen, onboarding/TrustDialog, all permission dialogs (Bash/Edit/Write/WebFetch/Skill/NotebookEdit/AskUserQuestion/Plan…), status/cost/context panels, notifications.
4. **Agent loop / API** — streaming, extended thinking + signature, prompt caching, model fallback, `stop_reason` control flow (max_tokens/pause_turn/refusal/ctx-window), orphaned tool_result, micro + auto compaction, token budget.
5. **Tools** — Read/Write/Edit/MultiEdit/NotebookEdit/Bash/BashOutput/KillShell/Glob/Grep/WebFetch/WebSearch/Task/TodoWrite/AskUserQuestion/EnterPlanMode/ExitPlanMode/LSP/Skill/StructuredOutput/Worktree/Config — schema, behavior, prompts.
6. **Permissions** — rule matching, 4 modes (default/acceptEdits/plan/bypass), interactive ask, allow-once/allow-always persistence, `/permissions`, per-tool dialogs.
7. **Slash commands** — the full ~78-command registry, each: dispatch + effect.
8. **CLI subcommands** — `doctor`/`update`/`agents`/`completion`/`mcp …`/`auth …`/`config`/`plugin`.
9. **MCP** — stdio/SSE/HTTP/WS transports, `claude mcp add/add-json/list/get/remove/serve`, remote OAuth (RFC 8414/9728/7591) + token cache/refresh, elicitation, reconnect/backoff, `.mcp.json` + settings scopes.
10. **Hooks** — full event taxonomy (SessionStart/End/PreToolUse/PostToolUse/UserPromptSubmit/Stop/SubagentStart/Stop/Notification/PreCompact/PostCompact/…), matchers, parallel deny>ask>allow, input/output schema.
11. **Sessions / memory / compact** — resume/continue, rewind/checkpoint, CLAUDE.md scope hierarchy + `@import`, `history.jsonl`, cost persistence, post-compact file restoration.
12. **Config / plugins / skills / output-styles** — settings hierarchy (user/project/local/managed), plugins, skills (bundled + discovery + activation), output styles.
13. **Auth** — interactive OAuth login (callback/browser/exchange), `/login`·`/logout`·`claude auth`, keychain, apiKeyHelper, API-key/env precedence.
14. **Orchestration** — sync subagents, async/background (`run_in_background`), real local Team (dispatch/coordinate), git-worktree isolation, Task `model`/`isolation`.
15. **Sandbox** — macOS seatbelt, Linux landlock+seccomp, `dangerouslyDisableSandbox` semantics, fail-closed.
16. **SDK** — control protocol (control_request/response), `canUseTool`/interrupt/set_model, importable `Query` entrypoint.
17. **OUT-of-scope appendix** (`N/A`) — cloud stack (teleport/RemoteAgentTask/CCR/cloud cron/remote CLIs), GitHub/Slack apps + session share, IDE/desktop/Chrome/mobile/voice companions, server-driven feature-flags/AB (statsig), internal telemetry + debug-only commands.

---

## 5. Follow-on (after the checklist)

1. User reviews the checklist.
2. Roll up `⚠️ built-not-wired` + `❌ missing` → a **wiring & optimization worklist**, dependency-ordered
   (e.g. real Team execution, SDK CLI entrypoint, rewind snapshot-write trigger, overlay slash-triggers,
   remote-MCP-OAuth `cmd/claude` wiring, PowerShell sandbox parity, …).
3. With "make these test rows go ✅" as the goal, implement the worklist (TDD micro-commits, per
   `writing-plans` + `subagent-driven-development` — that's when writing-plans is invoked).
4. Sediment the `AUTO` rows into `e2e/` Go tests as they pass (CI-runnable regression suite).
