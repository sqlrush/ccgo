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
- Session/history support now includes CC-compatible prompt history references, pasted text/image placeholder parsing, paste-cache hashing and retrieval, `history.jsonl` append/load, current-session-first up-arrow ordering, ctrl+r-style deduped timestamped history, `CLAUDE_CODE_SKIP_PROMPT_HISTORY`, remote session event pagination helpers, lenient transcript loading, legacy progress parent-bridge recovery, compact-boundary pruning, snip removal/relink replay, metadata entry collection, leaf UUID calculation, conversation-chain reconstruction, orphaned parallel tool-result recovery, content-replacement record loading/reconstruction, tombstone metadata delete/relink replay, and tombstone-style transcript message removal with a size guard.
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
- Full session/history parity gaps that remain: large-file optimized transcript loading, preserved-segment edge cases beyond current relink/prune support, content-replacement feature-flag/runtime override and inherited subagent gap-fill details, complete async prompt-history lifecycle parity beyond current lock/buffer/undo paths, full pasted-image processing/runtime integration beyond current prompt image-cache/image-block/metadata path, remaining remote-history edge cases, sidechain/subagent transcript layout, and all session metadata entry types.

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
- `internal/compact`: effective context window calculations, auto-compact thresholds, token warning state, compact summary prompt construction, compact API runner, compact boundary/summary plan generation, microcompact/cache primitives, persistent cached microcompact storage, structural/rich-content microcompact cache digests, cache version/TTL/prune handling for disk and in-memory caches, memory-cache write-through to disk, atomic cache writes, corrupt-cache fail-open behavior, and filename/digest mismatch pruning.
- `internal/conversation`: optional auto-compact can now run before the main request, fail open with a consecutive-failure circuit breaker, persist compact boundary metadata to transcript, write a session-memory summary, inject recalled session-memory snippets into API request context when enabled, and optionally extract turn-end memory facts into session memory.
- `internal/memory`: session-memory summaries can now be loaded with frontmatter field aliases, rolled up/pruned into an archive summary with prior rollup archives excluded from candidate compaction and merged across archive IDs, rune-safe rollup truncation, recalled by query with deterministic scoring and recency ordering, ranked through a model-assisted candidate/session-id selection path with fallback when selected IDs are invalid or excluded, alternate/camel response-key parsing, fenced/prose JSON extraction, scalar session-id parsing, nested/wrapped/collection-alias selection parsing, and nested selected-memory item parsing, and injected into resume context through the same optional recall agent path; deterministic and model-backed memory fact extraction can summarize user preferences, requests, decisions, and tool-use facts, including fenced/prose JSON, wrapped facts responses, alternate and structured fact field names, nested source objects, nested fact response shapes, and fact kind aliases.
- `internal/session`: project session listing and pagination, prompt-history lock/buffered flush/field aliases, lightweight transcript index/title/text-preview inference, line-offset transcript indexing/window/byte-budget-window/parent-chain/resume/tail/byte-budget-tail loading, AI-title/last-prompt/task/agent/PR/worktree transcript metadata loading and type/field aliases, transcript message/session UUID field aliases, tombstone metadata loading with target/session/reason aliases and delete/relink replay, agent-scoped content replacement metadata/record field-alias loading, session-scoped metadata reappend including AI-title/last-prompt/task-summary, streaming transcript search snippets for resume/search UI, official `subagents/agent-*.jsonl` transcript layout with legacy sidechain listing, agent metadata sidecar read/write/field aliases, sidechain runtime start/append/finish/cancel/fail summary bridging plus parent-chain append/finish, sidechain manager orchestration for spawn/append/finish/cancel/fail/list/resume, sidechain state/list/resume support with content-field aliases, sidechain resume context construction, sidechain conversation and agent-scoped content-replacement reconstruction, transcript tail/window loading and byte-budget tail loading for bounded-memory resume/UI paths, lightweight transcript metadata loading, remote-history token refresh retry, page-field/event-list/records/entries/last-id/cursor/event-id/has-next aliases plus wrapped-data/links/paging/bare-array responses, before_id resume, fallback field fill, and duplicate-aware parent linking during transcript materialization.
- `internal/session`: transcript full loader and line-offset index now accept top-level record UUID aliases such as `messageUuid`, `messageUUID`, `message_id`, `messageId`, and `id`, plus parent message UUID/ID aliases for progress bridge and indexed resume paths.
- `internal/session`: transcript full loader and line-offset index now accept entry type aliases such as `role`, `entry_type`, and `messageType`, plus `createdAt`/`created_at` timestamp aliases.
- `internal/contracts`/`internal/session`: transcript resume now accepts nested content block aliases for `toolUseId`/`toolUseID`, `isError`, `cacheControl`, `cacheReference`, and cache edit `cacheReference`.
- `internal/session`: remote-history parsing now also accepts connection-style `history`/`messages` wrappers with `nodes`/`edges[].node` event lists and `pageInfo`/`page_info` `hasNextPage`/`endCursor`/`startCursor` pagination aliases.
- `internal/session`: remote-history connection edges now use `edges[].cursor` as the event cursor when the nested node lacks an event ID, preserving pagination even when pageInfo omits a cursor.
- `internal/session`: remote-history parsing now also accepts `eventList`/`event_list`, `sessionEvents`/`session_events`, and connection aliases such as `connection`, `eventConnection`, and `sessionEventsConnection`.
- `internal/session`: remote-history pageInfo parsing now accepts previous/older pagination signals such as `hasPrevious`/`hasPreviousPage`, `hasOlder`/`more`, and before-id cursors such as `previousCursor`/`prevCursor`/`beforeCursor`/`olderCursor`.
- `internal/session`: remote-history link pagination now accepts `links.next`/`links.previous`/`links.prev`/`links.older` string URLs or `{href,url,uri,link}` objects and extracts before/cursor query parameters for continuation.
- `internal/session`: remote-history pagination now also accepts HTTP `Link` header URLs with `previous`/`prev`/`older`/`next` rels as continuation cursor fallbacks.
- `internal/session`: sidechain state loading now accepts legacy subagent/agent/task start and finish subtype aliases, broader sidechain/subagent ID and type fields, summary aliases, and common running/completed/cancelled/failed status aliases.
- `internal/session`: sidechain agent metadata sidecar loading now accepts broader agent type, workspace/worktree path, and task-description field aliases such as `subagentType`, `agentName`, `workspacePath`, `taskDescription`, `prompt`, and `title`.
- `internal/session`: transcript metadata loading now indexes file-history and attribution snapshots by message ID, with aliases such as `message_id`, `messageUuid`, and `id`, while preserving raw snapshot lists.
- `internal/session`: transcript indexes and session search now recover message `gitBranch` values, accept `git_branch`/`branch` aliases, and can match sessions by branch name.
- `internal/session`: full transcript title derivation now matches indexed/lite fallback order: custom title, AI title, first user prompt, last-prompt metadata, then summary.
- `internal/session`: transcript indexes and session search now recover message `cwd` as project path, accept project/working-directory aliases, and can match sessions by project path.
- `internal/session`: transcript message loading now preserves structured SerializedMessage metadata such as `userType`, `entrypoint`, `version`, and `slug`, including common alias spellings.
- `internal/session`: lightweight transcript metadata loading now clears stale context-collapse commit/snapshot state after compact-boundary messages, matching the full loader and official sessionStorage restore semantics.
- `internal/memory`: memory age/freshness helpers now match official stale-memory guidance, and document loading can prefix old memory files with a system-reminder that they are point-in-time observations.
- `internal/memory`: relevant-memory attachment primitives now match the official `relevant_memories` shape for stable headers, system-reminder rendering, surfaced path/byte scanning, 200-line/4096-byte surfacing reads with truncation notices, mark-after-filter duplicate attachment handling, last-non-meta-user/single-word/session-byte-cap prefetch gating, top-5 candidate filtering after read-state/surfaced de-dup, and recent successful tools collection excluding pending/failed/same-name-failed tools.
- `internal/conversation`: request building now expands `relevant_memories` attachment messages into user/meta system-reminders before Anthropic API normalization.
- `internal/conversation`: runners can now opt into a configured relevant-memory directory, deterministically select matching memory files, surface them as `relevant_memories`, and inject them into the next request while leaving default behavior unchanged.
- `internal/conversation`: configured relevant-memory directories are now propagated to tool metadata as internal auto-memory paths so file-tool freshness and permission checks share the same context.
- `internal/session`: transcript resume now preserves raw attachment payloads for fallback attachment messages, keeping resumed `relevant_memories` expandable in request construction.
- `internal/tools/file`: `Read` now prefixes old auto-memory file reads with the same freshness system-reminder when internal auto-memory directory metadata is available.
- `internal/tools/file`: `Write`/`Edit` now call the memory secret guard for team-memory paths.

M7 progress now includes:

- `internal/tui`: lightweight terminal frame renderer using ANSI clear/home/cursor control, message/status/prompt/dialog components, prompt input editing including shared kill-ring state, shift-enter multiline prompt input, line-local multiline prompt ctrl-a/ctrl-e/ctrl-u/ctrl-k editing plus multiline prompt wrap/render/cursor placement, ctrl-b/ctrl-f/ctrl-u/ctrl-k/ctrl-w line editing, ctrl-p/ctrl-n prompt and reverse-search navigation, alt-b/alt-f/alt-d/alt-backspace word editing, ctrl-left/ctrl-right/alt-left/alt-right word motion, ctrl-y yank, and alt-y yank-pop initial support, reverse-search cursor/word editing/kill/yank/yank-pop initial support, ctrl-c interrupt/double-press exit events, ctrl-d delete-forward and empty-input double-press exit events, ctrl-l redraw events, ctrl-o/ctrl-t global toggle events, ctrl-g/ctrl-s/ctrl-x chord chat events, prompt history navigation, reverse-search state/rendering/selection/script assertions including empty/cancel paths and cursor position, paste/image hint input handling with OSC ST terminators and base64 filename decoding, text/image pasted-content references with expanded submit text, metadata script assertions, and history entry restoration, SGR mouse parsing, alternate terminal navigation key sequences including modified Home/End/Delete/PageUp/PageDown, wheel scrolling with modifiers, primary clicks with modifiers, primary drag viewport selection, configurable viewport half-page/top/bottom scrolling, viewport line selection and dialog action clicks, focus/blur events, resize scroll preservation, default and configurable keybinding resolver with chord pending/null-unbind behavior, key/action camelCase aliases, keybinding JSON config loading, and focus/mouse/paste/image key names, vim insert/normal word, WORD, ge/gE, operator ge/gE, line-local `^`/`$`/`0`/`I`/`A`/`D`, quote, bracket, and j/k linewise motions/text objects, persistent yank/register/paste paths, last-change dot-repeat, G/gg line navigation, toggle-case, join, open-line, indent, substitute, delete/count/replace/undo/find/till/repeat actions, normal-mode arrow/backspace/delete mappings, and operator character ranges, permission/task dialog builders with kind/id routing/runtime resolution/status line, stale dialog race guards, active dialog cancellation, permission id/bulk cancellation, queued permission promotion, active task dialog refresh, task lifecycle state transitions and bulk cancellation, idempotent alternate screen lifecycle/reset/reassert-interactive sequences, mouse/focus/bracketed-paste terminal mode lifecycle/reconciliation and reassertion sequences, ANSI snapshot capture/stripping, snapshot corpus write/compare/script-file compare/missing-baseline/diff/batch/strict unexpected-baseline status, scripted interaction runner with JSON/JSONL/wrapper loading and file-runner entrypoints, JSON/runtime/task camel field aliases, keybinding mutations, permission-cancel runtime mutations, and assertions including vim mode/register/task state/dialog result/runtime mutation/task bulk-cancel/status negative/snapshot negative/screen size/event-sequence/event-count/no-event/dialog-result-count/no-dialog-result plus multi-key/text/paste/image/pasted-content metadata and named-key input, REPL screen model, viewport scrolling, and selection focus model.
- `internal/tui`: interaction scripts can now set status/baseStatus via `status`, `setStatus`, `statusLine`, or `baseStatus`, and runtime-aware scripts retain that base while layering permission/task status counts.
- `internal/tui`: terminal lifecycle can now manage extended-key reporting with Kitty keyboard protocol plus xterm modifyOtherKeys, including disable ordering and pop-before-push reassertion to avoid Kitty stack leaks.
- `internal/tui`: renderer and ANSI snapshots now have an opt-in DEC 2026 synchronized-output wrapper for official BSU/ESU frame fixtures without changing default rendering.
- `internal/tui`: terminal OSC helpers can generate sanitized OSC 0 title/icon sequences, and ANSI stripping now skips OSC/DCS-style payloads so invisible terminal controls do not leak into visible snapshot text.
- `internal/tui`: terminal OSC helpers now cover OSC 21337 tab-status generation/clear sequences and tmux/screen passthrough wrapping with official status-text escaping.
- `internal/tui`: terminal OSC helpers now generate OSC 8 hyperlink start/end sequences with official URL-derived id parameters and explicit param overrides.
- `internal/tui`: terminal OSC helpers now generate OSC 9;4 progress clear/set/error/indeterminate sequences with official 0..100 percentage clamping.
- `internal/tui`: terminal OSC helpers now generate iTerm2, Kitty, and Ghostty notification sequences plus raw BEL notifications for caller-managed emission.
- `internal/tui`: terminal OSC helpers now generate OSC 52 clipboard sequences using clipboard selection `c` and UTF-8 base64 payloads; native clipboard and tmux buffer runtime remain future work.
- `internal/tui`: terminal OSC helpers now support explicit ST (`ESC \\`) terminators for Kitty-style no-BEL OSC output while preserving BEL as the default.
- `internal/tui`: terminal OSC helpers now parse `#RRGGBB` and XParseColor-style `rgb:R/G/B` colors, scaling 1-4 digit hex components to 8-bit RGB like the official parser.
- `internal/tui`: terminal OSC helpers now parse OSC 21337 tab-status payloads with escaped separators, clear/null semantics, unknown-key ignore behavior, and parsed indicator/status colors.
- `internal/tui`: terminal OSC helpers now parse OSC 8 hyperlink payloads, including params, semicolon-containing URLs, and empty-URL link-end sequences.
- `internal/tui`: terminal OSC helpers now expose a lightweight `ParseOSCContent` covering title, hyperlink, tab-status, and unknown action branches.
- `internal/tui`: terminal OSC helpers now parse complete BEL- or ST-terminated OSC sequences into `ParseOSCContent` actions.
- `internal/tui`: terminal renderer constants now include clear-scrollback and legacy Windows cursor-home helpers for official clear-terminal sequence parity without platform auto-detection.
- `internal/tui`: terminal CSI helpers now generate cursor movement/position and erase sequences with official zero-move and horizontal-first cursorMove semantics.
- `internal/tui`: terminal CSI helpers now generate scroll up/down and scroll-region sequences with official zero-scroll behavior.
- `internal/tui`: terminal CSI helpers now generate DECSCUSR cursor-style sequences for block, underline, and bar cursors with blinking variants.
- `internal/tui`: terminal CSI helpers now expose bracketed-paste and focus input marker constants aligned with the existing key parser.
- `internal/tui`: terminal CSI helpers now generate official `eraseLines(n)`-style multi-line erase sequences ending at column 1.
- `internal/tui`: keybinding config, keymap resolution, and interaction script named-key input now accept terminal aliases for `ctrl-h`/`ctrl-i`/`ctrl-j`/`ctrl-m`, `ctrl-[`, and `ctrl-?`, including `control-*` and compact/camel variants.
- `internal/tui`: terminal key parsing now accepts CSI-u/kitty keyboard protocol sequences for existing ctrl/alt editing keys, shift-enter, shift-tab, and printable shift-only runes.
- `internal/tui`: terminal CSI-u/kitty keyboard parsing now also accepts colon-suffixed alternate codepoint and modifier event-type fields such as `CSI 97:65;5:1u`.
- `internal/tui`: image hint parsing now accepts OSC ST terminators and base64 `name=` filenames while preserving prompt pasted-content metadata.
- `internal/session`: prompt history writing now skips image pasted-content records like official `history.ts`, while the reader still accepts older image metadata entries.
- `internal/session`: paste-cache now has a best-effort cutoff-mtime cleanup helper for old `.txt` paste files, matching official `cleanupOldPastes` behavior.
- `internal/session`: buffered prompt-history writing now supports removing the latest pending entry before flush and skipping the latest flushed entry by timestamp in the same writer, matching the fast and slow paths of official `removeLastFromHistory`.
- `internal/session`: image-cache now supports session-scoped path caching, base64 image writes, image-only bulk storage, stored-path lookup, cache clearing, and cleanup of non-current session cache directories.
- `internal/session`/`internal/contracts`: prompt pasted-content conversion can now build Anthropic image `source` content blocks from image paste metadata, expand text paste refs, and append source-path meta messages for cached images.
- `internal/session`/`internal/tui`: pasted image metadata now carries `dimensions` and `sourcePath`, accepts common source/dimension aliases, and renders official-style image metadata text with source path plus original/display dimensions and coordinate scale.
- `internal/tui`: PromptInput/REPL screen can enable a session-scoped image-cache so image hint paste caches the image path and writes the base64 image file while inserting the `[Image #N]` prompt reference.
- `internal/tui`: PromptInput paste now strips ANSI, normalizes carriage returns, expands tabs, and uses the official 800-character plus `min(rows-10, 2)` visible-line threshold to decide whether pasted text stays inline or becomes a pasted-content reference.
- `internal/session`/`internal/tui`: orphan image pasted-content entries are now pruned after prompt edits and filtered again during prompt-message construction, so deleted image pills do not leak into Anthropic image blocks or image metadata.
- `internal/tui`: image paste pills now follow the official lazy-space behavior for consecutive image pastes and image-then-text input without adding duplicate spaces before explicit whitespace/newlines.
- `internal/tui`: REPL messages now preserve `imagePasteIds`, and prompt state advances `NextPastedID` from existing user-message image ids and pasted refs so resumed screens do not reuse paste IDs.
- `internal/tui`: reverse-search now matches full history entries and restores text/image pasted-content metadata on selection, so the next submit still carries display text and image metadata.
- `internal/tui`: REPL message restore can now rebuild prompt text and image pasted contents from user-message content blocks, `imagePasteIds`, and pasted-content metadata.
- `internal/tui`: Ctrl-S prompt stash now preserves and restores prompt text, cursor position, and pasted-content metadata.
- `internal/tui`: prompt submitted events now retain display text and pasted-content metadata, so downstream runtime code can build text/image content-block messages instead of receiving only the expanded prompt string.
- `internal/tui`: keybinding JSON loading now accepts wrapper object maps, `shortcuts`/`shortcutBindings`, object action fields such as `commandName`/`commandId`, string-array key sequences/chords, and `null`/`false` unbind entries.
- `internal/tui`: mouse parsing now accepts legacy X10/normal tracking `ESC[M...` press/release/wheel sequences in addition to SGR mouse events.
- `internal/tui`: interaction scripts now accept structured `mouse`/`mouse_event` steps with expanded button aliases such as `buttonMask`/`btn`/`code`, coordinate aliases such as `mouseX`/`clientX`/`screenX` and Y/row/line variants, release aliases such as `mouseUp`/`isRelease`/`mouseRelease`/`releaseEvent`, and dispatch them through the normal screen event path.
- `internal/tui`: interaction script steps now accept resize aliases such as `resize`/`terminalSize` object or `[width,height]` array forms, focus/blur aliases such as `focus`/`focused`/`blur`/`focusIn`/`focusOut`, and snapshot name aliases such as `snapshot`/`snapshotId`/`snapshotLabel`.
- `internal/tui`: runtime-aware interaction scripts now accept mutation aliases such as `permission`/`permissionRequest`, `task`/`taskStatus`, `removeTask`/`deleteTask`, `cancelPermission`, `cancelTasks`/`cancelReason`, and `openTasks`/`showTasks`.
- `internal/tui`: interaction script JSON now accepts string `keys` plus `input`/`input_text`/`keys_text`/`raw_key`/`paste_text` aliases for text entry, raw key sequences, and pasted text.
- `internal/tui`: interaction script contains assertions now accept a single string as well as string arrays for status, snapshot, viewport, and pasted-content checks, including camelCase viewport aliases.
- `internal/tui`: interaction script `keybindings`, `expectEvents`, and `expectDialogResults` fields now accept a single object as well as object arrays.
- `internal/tui`: prompt pasted-content expectations now accept a single `pastedContents` object as well as an array.
- `internal/tui`: interaction script task expectations now accept a single `contains` object as well as an array for expected task matches.
- `internal/tui`: interaction script message steps now accept chat/transcript-style `type`/`speaker` role aliases and `content`/`body`/`message` text aliases.
- `internal/tui`: interaction script message injection now accepts `messages`, `append_messages`/`appendMessages`, and `transcript_messages`/`transcriptMessages` object or object-array aliases.
- `internal/tui`: interaction script image steps now accept filename aliases such as `fileName`/`file_name`/`name`, media-type aliases such as `mimeType`/`mime_type`/`contentType`, and content aliases such as `data`/`base64`.
- `internal/tui`: interaction script direct dialog steps now accept ID, kind, title/body, action-list, and focused-index aliases aligned with dialog expectations.
- `internal/tui`: interaction script permission request steps now accept request/permission/tool-use ID aliases, path aliases, description aliases, action-list aliases, and single-string `actions`.
- `internal/tui`: interaction script prompt expectations now accept text, expanded text, cursor, and empty aliases such as `value`/`input`/`content`/`message`, `expandedText`/`fullText`, `cursorIndex`/`cursorPosition`, and `isEmpty`/`blank`.
- `internal/tui`: interaction script Vim expectations now accept enabled, mode, register, and linewise aliases such as `vimEnabled`/`isEnabled`, `vimMode`/`modeName`/`currentMode`, `vimRegister`/`registerValue`/`yankRegister`, and `registerLinewise`/`linewise`.
- `internal/tui`: interaction script task expectations now accept count and state-count aliases such as `taskCount`/`total`/`size`/`length` and `statusCounts`/`countsByState`.
- `internal/tui`: interaction script screen and viewport expectations now accept size and scroll aliases such as `columns`/`rows`, `screenWidth`/`screenHeight`, `scrollOffset`/`viewportOffset`, and `visibleRows`/`lineCount`.
- `internal/tui`: interaction script reverse-search expectations now accept state, query, cursor, current result, result-count, and no-match aliases such as `isActive`/`visible`/`open`, `search`/`term`/`pattern`, `currentResult`, `matchCount`, and `noMatches`.
- `internal/tui`: interaction script pasted-content expectations now accept ID, kind, content, media-type, filename, and contains aliases such as `pastedId`/`pastedContentId`, `kind`/`pastedType`, `value`/`data`/`base64`, `contentType`/`mimeType`, `fileName`/`name`, and `contains`.
- `internal/tui`: interaction script dialog expectations now support body contains/not-contains, exact actions, action contains/not-contains, action count, and focused action assertions, with runtime-aware scripts preserving dialog focused action across steps.
- `internal/tui`: interaction script dialog expectations now accept active, ID, kind, title, and body aliases such as `isActive`/`visible`, `dialogId`/`dialogID`, `dialogKind`, `heading`/`header`, and `content`/`text`/`message`.
- `internal/tui`: interaction script event and dialog-result expectations now accept aliases such as `eventType`/`event`/`name`, `payload`/`text`/`message`, `dialogId`/`dialogID`/`dialogKind`, `actionValue`/`resultStatus`, and found/stale aliases.
- `internal/tui`: interaction script loading now accepts `scriptSteps`/`script_steps`, `interactionSteps`/`interaction_steps`, and nested `scenario`/`test`/`case`/`fixture`/`interaction` wrapper objects.
- `internal/tui`: interaction script JSONL loading now allows 50MiB records so large paste/image/snapshot fixture lines do not hit scanner token limits.
- `internal/tui`: snapshot corpus comparison now accepts `.ansi`-only baselines by stripping ANSI text on load, and strict unexpected-baseline checks include both `.txt` and `.ansi`.

Still missing for full M6/M7 parity:

- M6: full official microcompact/cached microcompact parity, official session memory compaction/recall policy beyond current rollup/archive merge, complete memory recall agent strategy, complete sidechain/subagent runtime beyond current storage/metadata/resume context bridge, richer large transcript indexed loading beyond current offset/byte-budget windows, parent chains, and tails, remaining remote-history edge cases, and remaining niche metadata entry semantics.
- M7: custom Ink-compatible layout/reconciler parity, complete configurable keybinding/vim systems beyond current normal/operator motions and repeat find/till, full permission/task runtime parity beyond current stale/cancel/queue/dialog-refresh/lifecycle guards, full alternate screen lifecycle management beyond current idempotent reset and terminal mode paths, complete mouse behavior beyond current wheel/action-click/viewport-select/modifier/drag paths, full focus/resize parity beyond current events, full paste/image cache/history integration beyond current text refs and image metadata refs, official ANSI snapshot corpus, and official scripted interaction parity tests.
