# First And Second Batch Parity Audit

本文件记录第一批地基模块和第二批运行时核心对 Claude Code 源快照的重新扫描结果。

目标标准是 100% 行为兼容；当前状态不是完成态，而是持续补齐过程中的审计清单。

## Scope

第一批：

- `internal/contracts`
- `internal/bootstrap`
- `internal/platform`
- `internal/config`
- `internal/auth`
- `internal/model`
- `internal/messages`
- `internal/session`

第二批：

- `internal/permissions`
- `internal/tool`
- `internal/api/anthropic`
- `internal/conversation`

## Source Areas Rescanned

权限：

- `src/types/permissions.ts`
- `src/utils/permissions/*`
- `src/hooks/toolPermission/*`
- `src/components/permissions/*`

工具运行时：

- `src/Tool.ts`
- `src/tools.ts`
- `src/services/tools/toolExecution.ts`
- `src/services/tools/toolOrchestration.ts`
- `src/services/tools/StreamingToolExecutor.ts`
- `src/services/tools/toolHooks.ts`

Anthropic API 和 conversation：

- `src/services/api/client.ts`
- `src/services/api/claude.ts`
- `src/services/api/errors.ts`
- `src/services/api/errorUtils.ts`
- `src/services/api/promptCacheBreakDetection.ts`
- `src/query/*`
- `src/cli/print.ts`
- `src/entrypoints/sdk/*`

地基：

- `src/utils/settings/*`
- `src/utils/model/*`
- `src/services/oauth/*`
- `src/history.ts`
- `src/assistant/sessionHistory.ts`
- `src/bootstrap/state.ts`
- `src/constants/*`

## Implemented During This Rescan

- Permission rule parser now supports escaped parentheses, escaped wildcards, legacy tool names, legacy `:*` shell prefix rules, `addRules`/`replaceRules`/`removeRules`/`setMode`/directory updates, and rule validation.
- Permission path checks now cover working-directory scope, additional working directories, dangerous root paths for mutating operations, symlink-resolved permission paths, symlink escape prevention for new children, sensitive `.git`/`.claude`/IDE/shell-config write targets, suspicious Windows path-pattern blocking, internal readable paths such as session memory/tool-results/tasks, and internal editable paths such as scratchpad/plan/job/agent-memory paths with job symlink escape guards. The permission engine now applies sensitive-path safety before broad allow rules while still allowing explicit internal harness paths.
- Settings schema coverage was expanded for major Claude Code settings keys: auth helpers, model allowlists/overrides, MCP policy, hooks policy, worktree, shell, output style, language, thinking/effort, plugins, remote, spinner, sandbox, and related flags. Settings parsing now also coerces `env` values to strings, filters invalid permission rules into warnings, validates key permission fields, applies WebSearch/WebFetch-specific permission-rule validation, and honors `allowManagedPermissionRulesOnly` for permission-rule merging.
- Model registry now uses current Claude model IDs and aliases from the source snapshot, including Sonnet 4.6, Opus 4.6, Haiku 4.5, canonical-name rendering, and `[1m]` context variants.
- OAuth support now includes production OAuth config, scope parsing, Claude.ai scope detection, auth URL construction, PKCE verifier/challenge, state generation, and expiry checks.
- Session/history support now includes CC-compatible prompt history references, pasted text/image placeholder parsing, paste-cache hashing and retrieval, `history.jsonl` append/load, current-session-first up-arrow ordering, ctrl+r-style deduped timestamped history, `CLAUDE_CODE_SKIP_PROMPT_HISTORY`, remote session event pagination helpers, lenient transcript loading, legacy progress parent-bridge recovery, compact-boundary pruning, snip removal/relink replay, metadata entry collection, leaf UUID calculation, conversation-chain reconstruction, orphaned parallel tool-result recovery, content-replacement record loading/reconstruction, and tombstone-style transcript message removal with a size guard.
- Anthropic API layer now covers streaming accumulation, usage update/accumulation semantics, non-streaming max token cap, thinking-budget adjustment, retry/backoff with `Retry-After` and `x-should-retry`, context-overflow `max_tokens` retry adjustment, beta-header dedupe, custom request headers, basic prompt cache breakpoint/cache-reference/cache-edits placement, prompt dump JSONL capture for init/new user messages/non-streaming responses/stream chunks, and CC-compatible USD cost calculation for known Claude models including cache read/write, web search requests, and Opus 4.6 fast-tier pricing.
- Tool runtime now includes concurrency partitioning, ordered concurrent execution, interrupt behavior/defaults, max result size metadata, oversized result persistence, pre/post/permission-denied hook dispatch, hook-driven input updates/blocking, lifecycle progress events, and pre-call cancellation checks.
- Conversation runner can now use streaming clients, aggregate stream events into assistant messages, run tool calls through the orchestrator, preserve transcript append behavior, and apply CC-style per-message aggregate tool-result budget replacement before API requests with persisted replacement records for resume.

## Still Missing For 100% Compatibility

The following items remain incomplete and must not be treated as done:

- Full permission hook flow: `PreToolUse`, `PermissionRequest`, `PostToolUse`, blocking hook results, hook progress, hook telemetry.
- Auto mode / YOLO classifier: transcript construction, two-stage classifier, XML/tool-use parsing, prompt dump, denial circuit breaker, model gating, and fallback behavior.
- Interactive permission prompt flow: REPL dialogs, bridge/channel/swarm permission relays, user feedback images, prompt race handling, cancellation.
- Full filesystem permission parity gaps that remain: skill-scope allow suggestions, bundled skill extraction/read allowlist, sandbox write allowlist integration, complete auto-memory override policy, and deeper platform-specific Windows/WSL bypass handling.
- Full tool execution parity gaps that remain: full hook command execution/runtime policy, MCP elicitation, complete SDK progress/control event surface, mid-call cancellation for concrete tools, background task behavior, telemetry, schema-not-sent hints, and concrete tool-specific semantics.
- Complete Anthropic API parity gaps that remain: full dynamic beta-header latching, ant-only dump gating and `/issue` integration, cost tracker/session restore integration, streaming-to-non-streaming fallback, gateway/proxy-specific headers, first-party/Bedrock/Vertex/Foundry client setup, OAuth refresh in retry loop, fast-mode retry/cooldown semantics, persistent unattended retry heartbeats, full prompt-cache editing lifecycle, and provider-specific cache behavior.
- Full conversation/query loop: stop hooks, compact/auto-compact, token budget escalation, resume, SDK JSON/NDJSON control events, status updates, rate-limit handling, model switch breadcrumbs, side questions.
- Full settings parity gaps that remain: complete Zod-equivalent validation messages, MDM/HKCU settings, managed-settings drop-in loading, settings cache/change detector, schema generation, plugin-only customization enforcement, all strict marketplace validations, and live reload/app-state sync.
- Full session/history parity gaps that remain: large-file optimized transcript loading, preserved-segment edge cases beyond current relink/prune support, content-replacement feature-flag/runtime override and inherited subagent gap-fill details, async prompt-history locking/pending-flush race behavior, image-cache integration, remote-history auth token refresh, sidechain/subagent transcript layout, and all session metadata entry types.

## Current Verification

The current Go implementation compiles and passes:

```sh
go test ./...
```

This is evidence that the current strengthened implementation is internally consistent. It is not proof of 100% Claude Code parity.

## M5 Initial Progress

After the first/second batch hardening, the Go rewrite now includes an initial `internal/tools/file` package with text-file `Read`, `Write`, and `Edit` tools.

Covered behavior:

- `Read` line-number formatting, offset/limit slicing, mtime-based same-range dedup, text/binary/device guards, and read-state recording.
- `Write` create/update behavior, read-before-write validation for existing files, mtime stale detection, and post-write read-state refresh.
- `Edit` exact replacement, nonexistent-file creation with empty `old_string`, unique-match enforcement, `replace_all`, quote-style preservation for curly quotes, CRLF preservation, and post-edit read-state refresh.
- `conversation.Runner` now preserves tool metadata across tool rounds so a `Read` in one tool round can authorize a later `Edit`/`Write`.

Still missing from full M5 parity:

- Image, PDF, notebook, and large-file token-budget behavior in `Read`.
- Structured diff hunks/git diff/LSP and IDE notifications/file-history integration for `Write`/`Edit`.
- Settings-file validation, team-memory secret guard, skill activation, and full permission prompt rendering.
- `Bash`, `Glob`, `Grep`, `TodoWrite`, web, notebook, PowerShell, and MCP concrete tool semantics.

## M6/M7 Initial Progress

M6 progress now includes:

- `internal/memory`: recursive `.md` memory scanning, frontmatter parsing, newest-first capped manifests, root-to-leaf `CLAUDE.md` discovery/loading, and team-memory secret detection.
- `internal/compact`: effective context window calculations, auto-compact thresholds, token warning state, compact summary prompt construction, compact API runner, compact boundary/summary plan generation, microcompact/cache primitives, persistent cached microcompact storage, cache version/TTL/prune handling, atomic cache writes, corrupt-cache fail-open behavior, and filename/digest mismatch pruning.
- `internal/conversation`: optional auto-compact can now run before the main request, fail open with a consecutive-failure circuit breaker, persist compact boundary metadata to transcript, write a session-memory summary, inject recalled session-memory snippets into API request context when enabled, and optionally extract turn-end memory facts into session memory.
- `internal/memory`: session-memory summaries can now be loaded, rolled up/pruned into an archive summary, recalled by query with deterministic scoring and recency ordering, and ranked through a model-assisted candidate/session-id selection path; deterministic and model-backed memory fact extraction can summarize user preferences, requests, decisions, and tool-use facts.
- `internal/session`: project session listing and pagination, prompt-history lock and buffered flush, lightweight transcript index/title/text-preview inference, line-offset transcript indexing/window/tail loading, AI-title/last-prompt/task/agent/PR/worktree transcript metadata loading, agent-scoped content replacement metadata loading, session-scoped metadata reappend, streaming transcript search snippets for resume/search UI, official `subagents/agent-*.jsonl` transcript layout with legacy sidechain listing, agent metadata sidecar read/write, sidechain runtime start/append/finish summary bridging, sidechain manager orchestration for spawn/append/finish/list/resume, sidechain state/list/resume support, sidechain resume context construction, sidechain conversation and agent-scoped content-replacement reconstruction, transcript tail/window loading and byte-budget tail loading for bounded-memory resume/UI paths, lightweight transcript metadata loading, and remote-history token refresh retry.
- `internal/tools/file`: `Write`/`Edit` now call the memory secret guard for team-memory paths.

M7 progress now includes:

- `internal/tui`: lightweight terminal frame renderer using ANSI clear/home/cursor control, message/status/prompt/dialog components, prompt input editing including shared kill-ring state, ctrl-b/ctrl-f/ctrl-u/ctrl-k/ctrl-w line editing, alt-b/alt-f/alt-d/alt-backspace word editing, ctrl-y yank, and alt-y yank-pop initial support, reverse-search cursor/word editing/kill/yank/yank-pop initial support, ctrl-c interrupt/double-press exit events, ctrl-d delete-forward and empty-input double-press exit events, ctrl-l redraw events, ctrl-o/ctrl-t global toggle events, ctrl-g/ctrl-s/ctrl-x chord chat events, prompt history navigation, reverse-search state/rendering/selection/script assertions including empty/cancel paths, paste/image hint input handling, pasted-content references with expanded submit text, SGR mouse parsing, wheel scrolling, configurable viewport half-page/top/bottom scrolling, viewport line selection and dialog action clicks, focus/blur events, resize scroll preservation, default and configurable keybinding resolver with chord pending/null-unbind behavior and focus/mouse/paste/image key names, vim insert/normal word, WORD, ge/gE, quote, bracket, and j/k linewise motions/text objects, persistent yank/register/paste paths, last-change dot-repeat, G/gg line navigation, toggle-case, join, open-line, indent, substitute, delete/count/replace/undo/find/till/repeat actions, normal-mode arrow/backspace/delete mappings, and operator character ranges, permission/task dialog builders with kind/id routing/runtime resolution/status line, stale dialog race guards, active dialog cancellation, permission id/bulk cancellation, task lifecycle state transitions, idempotent alternate screen lifecycle/reset sequences, mouse/focus/bracketed-paste terminal mode lifecycle and reassertion sequences, ANSI snapshot capture/stripping, snapshot corpus write/compare, scripted interaction runner with assertions including vim mode/register/task state and multi-key input, REPL screen model, viewport scrolling, and selection focus model.

Still missing for full M6/M7 parity:

- M6: full official microcompact/cached microcompact parity, official session memory compaction/recall policy beyond current rollup, complete memory recall agent strategy, complete sidechain/subagent runtime beyond current storage/metadata/resume context bridge, richer large transcript indexed loading beyond current offset windows/tails, remote-history token refresh, and remaining niche metadata entry semantics.
- M7: custom Ink-compatible layout/reconciler parity, complete configurable keybinding/vim systems beyond current normal/operator motions and repeat find/till, full permission/task runtime parity beyond current stale/cancel/lifecycle guards, full alternate screen lifecycle management beyond current idempotent reset and terminal mode paths, complete mouse behavior beyond wheel/action-click/viewport-select paths, full focus/resize parity beyond current events, full paste/image cache/history integration beyond current pasted-content refs, official ANSI snapshot corpus, and official scripted interaction parity tests.
