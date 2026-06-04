# Claude Code 100% Go Rewrite Roadmap

目标：用 Go 100% 还原 Claude Code 的外部行为，而不是按源码文件逐行翻译。验收标准以 CLI、TUI、SDK、API 请求、工具执行、权限、session、MCP、plugins、skills、agents 等用户可观察行为为准。

## Completion Definition

100% 还原必须满足：

- CLI 参数、环境变量、退出码、stdout/stderr、日志格式兼容。
- 交互 TUI 的消息流、输入行为、权限弹窗、任务面板、状态栏、快捷键、resume/history 兼容。
- SDK/headless 输出事件、JSON/NDJSON、control protocol 兼容。
- Anthropic API 请求语义兼容，包括 tools schema、beta headers、thinking、effort、prompt cache、retry/fallback、usage/cost。
- Tool 行为兼容，包括成功、失败、权限拒绝、取消、并发、输出截断、落盘和 UI 展示。
- settings、MCP、plugins、skills、hooks、session JSONL、memory、compact、bridge、remote、agent/task 行为兼容。
- 能读取旧 Claude Code 配置和 transcript，并能生成旧版本可理解的数据。

## Current Status

| 模块 | 状态 |
| --- | --- |
| Go 工程骨架 | 已完成：`go.mod`、`cmd/claude`、`cmd/claude-mcp`、`Makefile` |
| Source audit | 已完成：`tools/sourceaudit`、`docs/sourceaudit.json` |
| 契约层 | 已完成第一版：messages、tools、commands、permissions、settings、session |
| Parity/golden 框架 | 已完成基础框架 |
| Bootstrap/platform/config/auth/model/messages/session | 已完成第一批地基模块 |
| Permissions/tool/api/conversation | 已完成第二批运行时核心 |
| Anthropic API | 已有 streaming、retry、usage/cost、beta header、dump、prompt cache 基础 |
| Conversation loop | 已有 tool loop、fallback、stream aggregation、transcript append |
| Tool runtime | 已有 registry、executor、hooks 框架、并发分区、权限判定、结果截断 |
| 文件工具 M5 初版 | 已完成文本版 `Read`、`Write`、`Edit`，含读前写、mtime stale guard、`replace_all`、Read 去重 |
| M6 初始上下文层 | 已完成 CLAUDE.md/memdir 扫描、memory manifest、team-memory secret guard、compact threshold/prompt/runner/boundary plan、conversation auto-compact 接入、失败熔断、microcompact/cache 初版、persistent cached microcompact 初版、cache digest structural/rich-content metadata 覆盖、cache version/TTL/prune、in-memory micro cache prune、memory-cache write-through 到磁盘、atomic cache write、坏缓存默认 fail-open、session memory summary/frontmatter aliases/recall 初版、model-ranked recall session-id selection 和 invalid-selection fallback 初版、recall agent alternate/camel response keys/fenced-prose JSON extraction/scalar id parsing/nested/wrapped/collection-alias selection parsing、resume context model-assisted recall 接入、session memory rollup/prune compaction、rollup archive exclusion/merge、rune-safe rollup truncation、resume context + session memory recall、conversation recall 注入、deterministic/model-backed memory extraction 初版、extraction agent fenced-prose JSON/wrapped facts/alternate/structured field/nested source object/nested response/fact kind alias parsing、turn-end memory extraction 落盘、prompt history lock/buffered flush/field aliases、official subagent transcript layout、agent metadata sidecar/field aliases、sidechain runtime start/append/finish/cancel/fail 和 parent-chain append/finish 初版、sidechain manager orchestration 初版、sidechain manifest 聚合、sidechain state/list/resume/content-field aliases 初版、sidechain resume context builder、sidechain conversation/content-replacement reconstruction、transcript tail/window/metadata/index loaders、byte-budget transcript tail loader、agent-scoped content replacement metadata/record field-alias loading、session-scoped metadata reappend including AI-title/last-prompt/task-summary、transcript line offset index/window/byte-budget-window/parent-chain/resume/tail/byte-budget-tail loaders、extended transcript metadata entries/type/field aliases、transcript message/session UUID field aliases including top-level `messageUuid`/`messageId`/`id` record IDs and `role`/`entry_type`/`messageType`/`createdAt` timestamp aliases、transcript tombstone metadata delete/relink、transcript resume conversation builder、index 文本预览和 AI-title/last-prompt/task metadata 字段、流式 transcript 搜索、session list pagination/search/title、remote history token refresh、remote history 全量分页抓取/page-field/event-list/records/entries/eventList/sessionEvents/last-id/cursor/event-id/has-next alias/wrapped-data/links/paging/bare-array/connection/eventConnection/sessionEventsConnection wrapper response/edge-cursor fallback/max-pages 截断状态/before_id 续抓、remote event transcript materialization/fallback field fill/去重追加/duplicate parent guard、remote history 一步 sync 到 transcript |
| M7 初始 TUI 层 | 已完成轻量 terminal frame renderer、PromptInput 状态机、history 导航、ctrl-p/ctrl-n history navigation、shift-enter 多行输入、多行 prompt 行内 ctrl-a/ctrl-e/ctrl-u/ctrl-k 和 wrap/render/cursor、共享 kill ring、ctrl-b/ctrl-f/ctrl-u/ctrl-k/ctrl-w 行编辑、alt-b/alt-f/alt-d/alt-backspace word 编辑、ctrl-left/ctrl-right/alt-left/alt-right word motion、ctrl-y yank 和 alt-y yank-pop 初版、reverse-search cursor/word 编辑/kill/yank/yank-pop 初版、ctrl-c interrupt/双击退出事件、ctrl-d delete-forward/空输入双击退出事件、ctrl-l 重绘事件、ctrl-o/ctrl-t 全局切换事件、ctrl-g/ctrl-s/ctrl-x chord chat 事件、reverse-search 状态/渲染/脚本断言/空结果/选择回填/cursor 断言、paste/image hint 输入和 OSC ST/base64 filename 兼容、text/image pasted-content 引用/metadata 脚本断言/提交展开/history entry restoration、SGR mouse 解析、alternate terminal navigation key sequences including modified Home/End/Delete/PageUp/PageDown、滚轮滚动、修饰键滚轮/左键、左键拖动选择、viewport 半页/顶部/底部可配置滚动、viewport 点击选择和 dialog action 点击、focus/blur 事件、resize 视口保持、keybinding resolver/config/chord pending/null-unbind/key/action camelCase alias、JSON config loader 和 focus/mouse/paste/image key name 覆盖、vim insert/normal/j/k/word/WORD/ge/gE/line-local ^/$/0/I/A/D/quote/bracket text-object/yank/register/paste/delete/count/replace/undo/find/till/repeat/dot-repeat/G/gg/toggle/join/open-line/indent/substitute 动作、normal-mode arrow/backspace/delete 映射和 operator linewise/字符范围、REPL screen、permission/task dialog builder、dialog kind/id routing/runtime/status line、runtime 到 REPL screen 的 dialog/status 同步、runtime-aware interaction script runner、prompt text/cursor/expanded/vim mode/register/task state/dialog result/runtime mutation/task bulk-cancel/permission cancel/keybinding mutation/status negative/snapshot negative/screen size/event-sequence/event-count/no-event/dialog-result-count/no-dialog-result 脚本断言、viewport 脚本断言、named-key 脚本输入、script JSON/JSONL/wrapper loader、script file runner 和 runtime/task camel field aliases、stale dialog race guard、cancel active、permission id/all cancellation、queued permission promotion、active task dialog refresh、task lifecycle/bulk-cancel 初版、idempotent alternate screen lifecycle/reset/reassert interactive 初版、mouse/focus/bracketed-paste terminal mode lifecycle/reconciliation、ANSI snapshot 基础、snapshot corpus write/compare/script-file compare/missing-baseline/diff/batch/strict unexpected-baseline 状态、scripted interaction runner/assertions/multi-key/text/paste/image/pasted-content metadata 初版、status/dialog/message components、viewport/selection |
| 全量测试 | 当前 `go test ./...` 通过 |

M6 补充：transcript resume 的嵌套 content block 现在接受 `toolUseId`/`toolUseID`、`isError`、`cacheControl`、`cacheReference` 字段别名，并保留 cache edit 的 `cacheReference`。

M6 补充：transcript resume 的 nested content block `id`/`tool_use_id`/`toolUseId` 现在接受 JSON number，并保留为字符串 tool-use ID。

M6 补充：嵌套 contract message 现在接受 `parentUUID`、`parentId`/`parentID`/`parent_id`、`parentMessageId`/`parentMessageID`/`parent_message_id` 和 parent-message UUID 别名，transcript/remote history payload 自带 parent alias 时不会丢失嵌套 parent。

M6 补充：嵌套 contract message 现在接受 `messageId`/`messageID`/`message_id` 和 `messageUuid`/`messageUUID`/`message_uuid` 作为自身 ID/UUID 别名，indexed resume 会保留 payload 自带的 nested message id。

M6 补充：嵌套 contract message 的 primary `id` 现在接受 JSON number，`LoadTranscript` 和 indexed resume 会保留为字符串 message id。

M6 补充：基础 `SessionEntry` JSONL loader 现在接受 `role`/`entryType`/`messageType`、message ID/UUID、parent ID/UUID 和 `sessionID`/`session`/session UUID 别名，旧 entry 文件可通过 `session.Load` 保留类型、parent 和 session。

M6 补充：tombstone metadata target/parent 现在接受 `targetId`/`deletedId`/`messageId` 和 `parentId`/`parentMessageId` 系列 ID/UUID 别名，删除/重连 replay 不会因旧字段拼写丢失 tombstone 目标或 parent。

M6 补充：transcript metadata 现在接受 summary `leafID`/message ID、content-replacement `agentID`/`toolUseID`/`blockID` 和 context-collapse `collapseID`/`summaryID`/archived ID 别名，metadata loader 与 full loader 保持同一兼容面。

M6 补充：worktree-state metadata 现在除 `worktreeSession`/`worktree_session` 外，也接受 `worktreeState`/`worktree_state`/`worktree`/`workspace` wrapper，full loader 和 lightweight metadata loader 都会保留旧 worktree payload。

M6 补充：PR link metadata 现在接受 `pullRequestNumber`/`pull_request_number`、`pullRequestURL`/`pull_request_url` 和 `repoFullName`/`repositoryFullName` 别名，full loader 和 lightweight metadata loader 都能恢复旧 PR metadata。

M6 补充：task-summary metadata 现在接受 `taskSummary`/`task_summary`/`content`/`text` 摘要别名和 `createdAt`/`created_at` timestamp 别名，旧任务摘要记录不会只保留 session id。

M6 补充：summary/custom-title/ai-title/last-prompt metadata 现在接受 `content`/`text`/`title`/`name`/`prompt` 等值字段别名，full loader、metadata loader 和 transcript index 的标题/摘要恢复保持一致。

M6 补充：tag、agent-name、agent-color、agent-setting 和 mode metadata 现在接受 `label`/`name`/`color`/`setting`/`status` 等值字段别名，full loader、metadata loader 和 transcript index 的 agent/session 状态恢复保持一致。

M6 补充：content-replacement metadata 现在接受 `records`/`contentReplacements` 等 record wrapper，以及 record 内 `type`/`content`/`hash` 等字段别名，full loader、metadata loader 和 transcript index 的 replacement 恢复保持一致。

M6 补充：content-replacement metadata 的 `agentId`、record `toolUseId` 和 `blockId` 现在也接受 JSON number，并在 full/lightweight loader 中保留为字符串 ID。

M6 补充：remote history GraphQL/connection 分页现在接受 `hasPrevious`/`hasPreviousPage`、`hasOlder`/`more` 继续分页标记，以及 `previousCursor`/`prevCursor`/`beforeCursor`/`olderCursor` 等 before-id cursor 别名，避免只返回第一页历史。

M6 补充：remote history pagination bool 字段现在除 JSON bool 和 `true`/`false` 字符串外，也接受 `1`/`0`、`yes`/`no`、`on`/`off` 等数值/字符串布尔形态，避免 wrapper/pageInfo 中的非严格布尔值中断分页。

M6 补充：remote history pagination cursor/id 字段现在接受 JSON number 并原样转成字符串，覆盖 `next_cursor` 等 page 字段和 `edges[].cursor` 的数字形态。

M6 补充：contract `ID` JSON 读取现在接受 JSON number/null，remote history event/message/session/parent ID alias 可继承数字 ID 兼容面并在 transcript materialization 中保留为字符串。

M6 补充：remote history response parser 现在会递归解包 `data.session.events`、`data.projectSession.eventConnection`、`conversation`、`remoteHistory` 等 GraphQL/session wrapper，继续复用 `nodes`/`edges[].node` 和 `pageInfo` pagination 解析。

M6 补充：remote history REST/link 风格分页现在接受 `links.next`/`links.previous`/`links.prev`/`links.older` 的字符串 URL 或 `{href,url,uri,link}` 对象，并从 `before_id`、`beforeId`、`cursor`、`pageCursor`、`previousCursor`、`prevCursor`、`beforeCursor`、`olderCursor`、`startCursor`、`endCursor` 等 query 参数提取下一页 before-id。

M6 补充：remote history 现在也接受 HTTP `Link` header 中 `rel="previous"`/`prev`/`older`/`next` 的分页 URL，并从同一组 before/cursor query 参数中提取续抓 before-id。

M6 补充：sidechain/subagent state loader 现在接受 `subagent_start`/`agent_start`/`task_start` 和 `sidechain_end`/`subagent_finish`/`agent_finish`/`task_summary` 等 subtype 别名，并归一化 `active`/`success`/`canceled`/`error` 等运行状态别名，同时读取 `subagentId`/`agentID`、`subagentType`、`finalSummary` 等 content 字段。

M6 补充：sidechain agent metadata sidecar 读取现在接受 `type`/`subagentType`/`agentName`/`name`、`workspacePath`/`workspace`/`path`/`directory`、`taskDescription`/`prompt`/`input`/`command`/`title` 等字段别名，兼容历史或第三方生成的 subagent metadata。

M6 补充：transcript metadata loader 现在会按 `messageId` 建立 file-history snapshot 和 attribution snapshot 索引，并接受 `message_id`/`messageUuid`/`id` 等字段别名，和官方按消息恢复 snapshot 的语义对齐。

M6 补充：transcript/index/session list 现在读取消息上的 `gitBranch`，接受 `git_branch`/`branch` 别名，并允许 session search 按分支名命中，补齐官方 lite metadata 中的 branch 恢复/检索语义。

M6 补充：full transcript 的 `TitleFromTranscript` 标题优先级现在和 indexed/lite 路径一致，按 custom title、AI title、首个用户 prompt、last-prompt、summary 顺序兜底，避免 resume/search/list 标题分叉。

M6 补充：lightweight transcript index 的 `content-replacement` 计数现在和其它 session metadata 一样按请求的 session id 过滤，避免 session list/search 摘要被同文件其它 session 污染。

M6 补充：transcript/index/session list 现在读取消息上的 `cwd` 作为 project path，接受 `projectPath`/`project_path`/`workingDirectory`/`working_directory` 等别名，并允许 session search 按项目路径命中，贴近官方 lite metadata 的 projectPath 恢复语义。

M6 补充：TranscriptMessage 现在结构化读取官方 SerializedMessage 元数据 `userType`、`entrypoint`、`version`、`slug`，并兼容 `user_type`/`userKind`、`entry_point`/`client`、`appVersion`/`claudeCodeVersion`、`sessionSlug`/`planSlug` 等别名。

M6 补充：model-backed session memory recall prompt 现在显式写入 requested limit 和 excluded current session id，减少模型返回超量或当前 session 后再 fallback 的概率。

M6 补充：lightweight transcript metadata loader 现在和 full loader 一样在 `system`/`compact_boundary` 后清空旧 `marble-origami-commit`/`marble-origami-snapshot` 状态，避免 metadata-only resume/inspect 路径保留 compact 前的过期 context-collapse 记录。

M6 补充：memory 层现在提供官方 `memoryAge`/freshness note 语义，`ReadDocumentsWithOptions` 可为超过 1 天的 memory 文档前缀 system-reminder，提示模型把 memory 视为 point-in-time 并验证当前代码。

M6 补充：memory 层新增官方 `relevant_memories` attachment 基础结构，支持 stable memory header、system-reminder 渲染、已 surfaced memory path/byte 扫描、按 200 行/4096 bytes 读取并附截断提示的 surfacing reader、mark-after-filter 的 duplicate memory attachment 过滤、最后非 meta user prompt/单词 prompt/60KB session cap 的 prefetch gating、多目录结果排除 read-state/surfaced 后取前 5 个候选的选择逻辑，以及 recent successful tools 窗口收集并排除 pending/failed/同名失败工具。

M6 补充：conversation request 构建现在会把 history 中的 `relevant_memories` attachment 展开成 user/meta system-reminder 后再进入 Anthropic request，避免 attachment message 在 NormalizeForAPI 路径被丢弃。

M6 补充：Runner 现在支持显式 `RelevantMemoryDir` runtime 接线：按最后非 meta user prompt 扫描 memory dir、deterministic 选择相关 md memory、读取成 `relevant_memories` attachment 并注入 request；默认不开启。

M6 补充：relevant memory prefetch 现在有可取消 runtime，并在 `MemoryAgentClient` 可用时先走 model-backed sideQuery selector：向模型提供候选 memory manifest，接受 `memory_paths`/`memoryPaths`/`filePath`/`matches`/嵌套 selection 等路径别名，按模型顺序读取附件；模型错误或无效路径 fail-open 回落 deterministic selector。完整官方 prompt/telemetry parity 仍需继续补。

M6 补充：model-backed relevant memory selector prompt 现在包含 recent successful tools 和 already-surfaced memory paths 的有界上下文，模型侧选择与 deterministic prefilter 的 tool/surfaced 约束更一致。

M6/M7 补充：Runner 会把 `RelevantMemoryDir` 透传到 tool metadata 的 internal auto-memory path context，让 Read tool freshness prefix 和 permission internal-path policy 在同一 memory dir 配置下生效。

M6 补充：transcript resume 在 fallback 转换 attachment message 时会保留 raw attachment payload，确保恢复出的 `relevant_memories` attachment 仍可被 request 构建路径展开为 system-reminder。

M6/M7 补充：Read tool 现在在 metadata 提供 auto-memory 目录时，为读取到的旧 auto-memory 文件前缀 freshness system-reminder，贴近官方 FileReadTool 的 memory freshness prefix。

M7 补充：keybinding resolver/config 和脚本 named-key 输入已覆盖 `ctrl-h`/`ctrl-i`/`ctrl-j`/`ctrl-m`、`ctrl-[`、`ctrl-?` 及 `control-*` 终端别名；terminal parser 支持 CSI-u/kitty keyboard protocol 的 ctrl/alt/shift-enter/shift-tab 序列；image hint parser 支持 OSC ST terminator 和 base64 `name=` filename；keybinding JSON loader 支持 wrapper object-map、`shortcuts`、object action 字段、string-array key sequence/chord 和 `null`/`false` unbind；mouse parser 支持 legacy X10/normal tracking 序列；interaction script 支持结构化 mouse/mouse_event 步骤，button 接受 `buttonMask`/`button_mask`/`btn`/`code`/`mask`，坐标接受 `mouseX`/`mouse_x`/`clientX`/`screenX` 和对应 Y/row/line 别名，release 接受 `mouseUp`/`isRelease`/`mouseRelease`/`releaseEvent` 等字段别名；interaction script 还支持字符串 `keys` 和 `input`/`input_text`/`keys_text`/`raw_key`/`paste_text` 字段别名，status/snapshot/viewport/pasted-content contains 断言接受单字符串或字符串数组，且 `keybindings`、`expectEvents`、`expectDialogResults`、`expectPrompt.pastedContents`、`expectTasks.contains` 接受单对象或对象数组。

M7 补充：keybinding config 的 page navigation key name 现在接受 `pgup`/`pg-up`/`prior` 和 `pgdn`/`pg-down`/`next`，覆盖常见终端键名/配置别名。

M7 补充：interaction script 的 `message` step 现在接受 chat/transcript 风格的 `type`/`speaker` role 别名和 `content`/`body`/`message` text 别名；`image` step 接受 `fileName`/`file_name`/`name`、`mimeType`/`mime_type`/`contentType` 和 `data`/`base64` 内容别名；permission request step 接受 request/permission/tool-use ID、path、description 和 action 字段别名，并允许 `actions` 使用单字符串；`expectPrompt` 接受 `value`/`input`/`content`/`message`、`expandedText`/`fullText`、`cursorIndex`/`cursorPosition`、`isEmpty`/`blank` 等字段别名，且 `pastedContents` 断言接受 `pastedId`/`pastedContentId`、`kind`/`pastedType`、`value`/`data`/`base64`、`contentType`/`mimeType`、`fileName`/`name` 和 `contains` 等字段别名；`expectVim` 接受 `vimEnabled`/`isEnabled`、`vimMode`/`modeName`/`currentMode`、`vimRegister`/`registerValue`/`yankRegister`、`registerLinewise`/`linewise` 等字段别名；`expectTasks` 接受 `taskCount`/`total`/`size`/`length` 和 `statusCounts`/`countsByState` 等字段别名；`expectScreen`/`expectViewport` 接受 `columns`/`rows`、`screenWidth`/`screenHeight`、`scrollOffset`/`viewportOffset`、`visibleRows`/`lineCount` 等字段别名；`expectReverseSearch` 接受 `isActive`/`visible`/`open`、`search`/`term`/`pattern`、`cursorIndex`、`currentResult`、`matchCount`/`matches`、`noMatches` 等字段别名；`expectDialog` 可断言 body contains/not-contains、actions/action contains/not-contains、action count 和 focused action，runtime-aware scripts 会在步骤间保留 dialog focused action，且接受 `isActive`/`visible`、`dialogId`/`dialogID`、`dialogKind`、`heading`/`header`、`content`/`text`/`message` 等字段别名；`expectEvent`/`expectDialogResult` 接受 `eventType`/`event`/`name`、`payload`/`text`/`message`、`dialogId`/`dialogID`/`dialogKind`、`actionValue`/`resultStatus`/`exists`/`isStale` 等字段别名。

M7 补充：interaction script 现在接受 `messages`、`append_messages`/`appendMessages`、`transcript_messages`/`transcriptMessages` 批量消息注入字段，且这些字段既可用单个对象也可用对象数组，消息对象沿用 chat/transcript role/text 别名。

M7 补充：interaction script 的 direct `dialog` step 现在接受 `dialogId`/`dialogID`、`dialogKind`、`heading`/`header`、`content`/`text`/`message`、`options`/`choices`/`buttons` 和 `focusedIndex`/`selectedIndex` 等字段别名，并允许 actions/options 使用单字符串。

M7 补充：interaction script loader 现在接受 `scriptSteps`/`script_steps`、`interactionSteps`/`interaction_steps` wrapper 字段，并能从一层 `scenario`/`test`/`case`/`fixture`/`interaction` 对象里继续解析脚本步骤。

M7 补充：ANSI snapshot corpus 比对现在在 `.txt` baseline 缺失时可读取 `.ansi` baseline 并 strip ANSI 后比较，strict unexpected-baseline 检查也会纳入 `.ansi` 文件。

M7 补充：interaction script step 现在接受 `resize`/`terminalSize`/`screenSize` 对象或 `[width,height]` 数组，以及顶层 `columns`/`rows` resize 别名；`focus`/`focused`/`blur`/`focusIn`/`focusOut` 会走正常 focus event 路径；snapshot capture 接受 `snapshot`/`snapshotId`/`snapshotLabel` 等名称别名；runtime-aware scripts 接受 `permission`/`permissionRequest`、`task`/`taskStatus`、`removeTask`/`deleteTask`、`cancelPermission`、`cancelTasks`/`cancelReason`、`openTasks`/`showTasks` 等 mutation 别名。

M7 补充：interaction script JSONL loader 单行上限提升到 50MiB，和 transcript/session 大记录读取容忍度对齐，避免大型 paste、image metadata 或 snapshot fixture 脚本行触发 scanner token limit。

M7 补充：terminal CSI-u/kitty keyboard parser 现在接受 codepoint alternate 和 modifier event-type 的冒号字段（如 `CSI 97:65;5:1u`），按主 codepoint/modifier 解析 ctrl/alt/shift/rune 键，避免 kitty progressive keyboard protocol 变体被判为 unknown。

M7 补充：terminal CSI-u/kitty keyboard parser 现在也接受无修饰/base 序列（如 `CSI 97u`、`CSI 13;1u`），映射 printable rune、Enter、Tab、Esc 和 Backspace，避免启用 extended keyboard 后普通按键掉入 unknown。

M7 补充：terminal CSI parser 现在把 DA/device attributes (`CSI c`、`CSI >c`、`CSI =c`) 解析成 report action，保留 primary/secondary/tertiary private marker 和 code，避免终端能力查询序列落入 generic unknown。

M7 补充：terminal CSI parser 现在接受 ECMA/xterm cursor alias final bytes：`CSI a` 映射 cursor-forward、`CSI e` 映射 cursor-down、`CSI \`` 映射 cursor-column，避免常见终端输出别名落入 unknown。

M7 补充：terminal CSI parser 现在接受 DEC private mode `?1047h/l` alternate-screen buffer 和 `?1048h/l` save/restore cursor，补齐常见 alternate-screen lifecycle 序列变体。

M7 补充：terminal CSI parser 现在把 DECREQTPARM terminal-parameters (`CSI x`) 解析成 report action，保留 code 和 private marker，避免终端参数查询序列落入 generic unknown。

M7 补充：terminal CSI parser 现在把 xterm window manipulation/report (`CSI t`，如 `CSI 14t`/`CSI 18t`) 解析成 report action，保留 code 和 private marker，避免窗口/文本区尺寸查询序列落入 generic unknown。

M7 补充：terminal CSI parser 现在把 TBC tab-clear (`CSI g`/`CSI 3g`) 解析成 cursor action，保留 clear-current/all code，避免制表位清理序列落入 generic unknown。

M7 补充：terminal ESC parser 现在把 HTS horizontal-tab-set (`ESC H`) 解析成 cursor/tab-set action，和 CSI tab-clear 控制序列形成闭环。

M7 补充：terminal sequence dispatcher 现在把 SS3 application cursor (`ESC OA`/`OB`/`OC`/`OD`) 解析成结构化 cursor move action，避免 application cursor mode 序列落入 unknown。

M7 补充：terminal CSI parser 现在把 DEC `?1h/l` application cursor mode 解析成独立 mode action，和 SS3 application cursor key 解析闭环。

M7 补充：terminal CSI parser 现在把 DEC `?3h/l` 132/80-column mode 解析成结构化 `columnMode` action，覆盖常见列宽状态切换序列。

M7 补充：terminal CSI parser 现在把 DEC `?5h/l` reverse video/screen mode 解析成结构化 `reverseVideo` mode action，继续减少终端显示状态序列的 unknown fallback。

M7 补充：terminal CSI parser 现在把普通 `CSI 4h/l` insert/replace mode 解析成 `insertMode` action，避免 ECMA mode set/reset 序列落入 unknown。

M7 补充：terminal CSI parser 现在把普通 `CSI 20h/l` line-feed/new-line mode 解析成 `lineFeedMode` action，继续覆盖 ECMA mode set/reset 序列。

M7 补充：terminal CSI parser 现在把 DEC `?6h/l` origin mode 和 `?7h/l` auto-wrap mode 解析成结构化 mode action，继续减少终端状态序列的 unknown fallback。

M7 补充：terminal CSI parser 现在把 DEC `?12h/l` cursor blink mode 解析成结构化 `cursorBlink` mode action，补齐 cursor visibility/style 相邻的终端状态序列。

M7 补充：terminal CSI parser 现在把 xterm/DEC `?45h/l` reverse-wraparound mode 解析成结构化 `reverseWrap` mode action，补齐 auto-wrap 相邻的 wrap 状态序列。

M7 补充：terminal CSI parser 现在把 DEC `?66h/l` application keypad mode 解析成结构化 `applicationKeypad` mode action，补齐 application cursor mode 相邻的输入状态序列。

M7 补充：terminal CSI parser 现在把 REP repeat-preceding-character (`CSI b`/`CSI 4b`) 解析成 edit action，visible-text/snapshot pipeline 和 ANSI message wrapping/trim 会按重复次数展开前一个可重复 grapheme。

M7 补充：terminal CSI parser 现在按 ANSI 默认参数解析 scroll-region (`CSI r`/`CSI ;10r`)，缺失 top 默认为 1，缺失 bottom 保持为 0 表示 reset/full-height，避免把 reset 误判为单行区域。

M7 补充：terminal CSI parser 现在把 DECSTR soft reset (`CSI !p`) 解析成 reset action，terminal parser 会复用现有 reset 流程清理 SGR/link 状态，避免软复位序列落入 generic unknown。

M7 补充：prompt history 写入现在按官方 `history.ts` 过滤 image pasted content，不再把 image base64/filename/mediaType 写入 `history.jsonl`；历史读取仍兼容旧 image metadata。

M7 补充：paste-cache 现在提供按 cutoff mtime 清理旧 `.txt` paste 文件的 best-effort 入口，忽略不存在的 cache 目录、非 `.txt` 文件和单文件清理错误，贴近官方 `cleanupOldPastes` 行为。

M7 补充：Buffered prompt history writer 现在支持撤销最近 pending entry 的 fast path，给中断/自动恢复场景接入官方 `removeLastFromHistory` 的 pending-buffer 语义留下可测试入口。

M7 补充：Buffered prompt history writer 现在也支持撤销已 flush entry 的 slow path：记录最近 add 的 timestamp，并在同一 writer 的 up-arrow/ctrl-r 历史读取中按当前 session 跳过，保持 `history.jsonl` append-only。

M7 补充：image-cache 现在有 session-scoped 存取基础：图片 paste 可按官方 `image-cache/<session>/<id>.<ext>` 路径缓存、base64 落盘为 0600 文件、批量只存 image 内容、查询内存路径，并清理非当前 session 的旧 image-cache 目录。

M7 补充：PromptInput/REPL screen 现在可启用 session-scoped image-cache；image hint paste 进入 prompt 时会缓存 `[Image #N]` 对应文件路径并把 base64 图片写入 `image-cache/<session>`，贴近官方 PromptInput 的 `cacheImagePath` + `storeImage` 行为。

M7 补充：prompt submit event 现在会保留 display 文本和 pasted-content metadata；session 层 `PromptMessages` 可把 text paste refs 展开、把 image paste refs 转成 Anthropic `image` content block 的 `source` 字段，并追加 image-cache source-path meta message。

M7 补充：pasted image metadata 现在保留 `dimensions` 和 `sourcePath`，读取接受 `source_path`/snake-case dimension aliases；image meta message 会按官方 `createImageMetadataText` 规则输出 source path、原始尺寸、显示尺寸和坐标换算倍率。

M7 补充：image hint parser 现在从 iTerm2 OSC File metadata 解析 `width`/`height`、original/display dimension 别名和 `sourcePath`/`source_path`/`path`，`KeyImageHint` 与 PromptInput pasted image metadata 会保留这些字段。

M7 补充：PromptInput paste 现在先 strip ANSI、把 `\r` 归一化为换行并把 tab 展开为 4 个空格；REPL screen 按官方 `PASTE_THRESHOLD=800` 和 `min(rows-10, 2)` 可见行阈值决定短 paste 内联还是折叠为 `[Pasted text #N]`。

M7 补充：PromptInput 现在会在输入编辑后清理已删除 `[Image #N]` pill 对应的 orphan image pasted-content，并且 session `PromptMessages` 提交构造会再次过滤未引用图片，避免孤儿图片进入 Anthropic image block 或 metadata。

M7 补充：image paste pill 现在匹配官方 lazy-space 行为：连续粘贴图片会自动写成 `[Image #1] [Image #2]`，图片后直接输入非空白字符会补一个空格，显式空格或换行不会重复补空格。

M7 补充：REPL message metadata 现在保留 `imagePasteIds`，并在 `SetMessages`/`AppendMessage` 时扫描用户消息里的 pasted refs 与 image ids 来推进 `NextPastedID`，避免 resume/continue 后新 paste ID 和历史消息冲突。

M7 补充：reverse-search 现在基于完整 `HistoryEntry` 匹配和选中历史项，选择后会恢复 text/image pasted-content metadata，并让随后的提交继续携带 display 与图片元数据。

M7 补充：REPL message restore 现在可从用户消息的 content blocks、`imagePasteIds` 和 pasted-content metadata 恢复 prompt，重建 `[Image #N]` 引用和 base64 image pasted contents，贴近官方 message selector restore 路径。

M7 补充：Ctrl-S prompt stash 现在保存并恢复 prompt text、cursor 和 pasted-content metadata，空 prompt 时可 unstash，贴近官方 `chat:stash` 行为。

M7 补充：prompt history pasted-content 读取现在接受 `mimeType`/`mime_type`/`contentType`、`fileName`/`file_name`/`name`、`filePath`/`file_path`/`path` 等 text/image metadata 别名，历史恢复路径和 image hint/parser metadata 兼容面保持一致。

M7 补充：prompt history `LogEntry` 读取现在接受 `sessionID`/`session`/`sessionUuid`/`sessionUUID`/`session_uuid` 作为 session id 别名，current-session-first 历史排序不会因 session 字段拼写不同而失效。

当前状态不是 100% 还原，而是“核心地基 + 运行时框架 + 第一批具体工具”的可编译阶段。

## Milestones

### M0: Evidence Closure

目标：固化源快照、缺失契约和官方 CLI 黑盒证据。

需要完成：

- 源快照 inventory 固化。
- 缺失 import 目标清单。
- 官方 CLI 黑盒采样脚本。
- 功能矩阵：external/internal/gated/enterprise。
- 每个缺失契约标注状态：恢复源码、反推 golden、暂不可证。

当前状态：基础 source audit 已完成，但官方 CLI golden corpus 仍需扩充。

### M1: Contract Layer

目标：稳定所有 JSON、stream、schema 契约。

需要完成：

- Message union。
- SDK message/event/control types。
- Tool schema/result/progress。
- Command type。
- Permission result/update。
- Settings/MCP config schema。
- Session JSONL entries。

当前状态：第一版已完成，仍需补 SDK/control protocol、完整 MCP config、schema generation。

### M2: CLI, Bootstrap, Config, Auth, Model

目标：兼容启动、配置和认证行为。

需要完成：

- CLI args/mode dispatch。
- `--version`、`--help`、`--print`、resume/continue 等入口。
- settings merge、managed policy、migrations、live reload。
- API key、OAuth、secure storage。
- model aliases/capabilities/cost/provider registry。

当前状态：bootstrap/config/auth/model 基础已完成；CLI 仍是 scaffold，未完整兼容 CC。

### M3: API Client And Conversation Loop

目标：跑通无工具和有工具 headless loop。

需要完成：

- Anthropic streaming/non-streaming client。
- query loop、tool_use/tool_result pairing。
- retry/fallback、stop hooks、rate-limit handling。
- usage/cost/token accounting。
- prompt cache lifecycle。

当前状态：streaming、retry、fallback、tool loop、usage/cost 基础已完成；完整 provider/gateway/cache/stop hook 仍缺。

### M4: Tool Runtime, Permissions, Sandbox

目标：完整工具执行和权限系统。

需要完成：

- Tool interface/registry/executor/orchestrator。
- permission rules/modes/decision engine。
- `PreToolUse`、`PermissionRequest`、`PostToolUse` hook flow。
- interactive permission prompt。
- auto mode / YOLO classifier。
- sandbox adapter 和 allowlist。

当前状态：runtime、rules、path checks、hooks 框架已完成；交互 permission、classifier、真实 sandbox 仍缺。

### M5: Core Built-In Tools

目标：还原内置工具行为。

优先顺序：

1. `Read` / `Edit` / `Write`
2. `Bash`
3. `Glob` / `Grep`
4. `TodoWrite`
5. `WebFetch` / `WebSearch`
6. Notebook / PDF / image
7. PowerShell
8. MCP wrapper tools

当前状态：

- 文本版 `Read`、`Write`、`Edit` 初版已完成。
- 已覆盖读前写、mtime stale guard、唯一匹配、`replace_all`、Read 去重、跨 tool round read-state。

仍需完成：

- `Read` 的 image/PDF/notebook、大文件 token budget、binary edge cases。
- `Edit/Write` 的 structured diff、git diff、LSP/IDE notify、file history、settings validation、secret guard。
- `Bash` parser、sandbox、timeout、background、interrupt、read-only/destructive validation。
- `Glob/Grep` ripgrep 行为、排序、分页、忽略规则。
- `TodoWrite` 状态和 tool result 兼容。
- Web、PowerShell、Notebook、MCP concrete tool semantics。

### M6: Session, Memory, Compact

目标：还原上下文、历史和压缩。

需要完成：

- JSONL transcript 读写。
- resume/search/title。
- sidechain/subagent transcript。
- prompt history、remote history。
- CLAUDE.md、session memory、team memory、auto memory。
- compact、auto compact、microcompact、token warning。
- content replacement、compact boundary、tombstone。

当前状态：session/history 有大量基础能力；memory/compact 初始包、compact runner、conversation auto-compact 接入、compact 失败熔断、microcompact/cache 初版、persistent cached microcompact 初版、cache digest structural/rich-content metadata 覆盖、cache version/TTL/prune、in-memory micro cache prune、memory-cache write-through 到磁盘、atomic cache write、坏缓存默认 fail-open、session memory summary/frontmatter aliases/recall 初版、model-ranked recall session-id selection 和 invalid-selection fallback 初版、recall agent alternate/camel response keys/fenced-prose JSON extraction/scalar id parsing/nested/wrapped/collection-alias selection parsing、resume context model-assisted recall 接入、session memory rollup/prune compaction、rollup archive exclusion/merge、rune-safe rollup truncation、resume context + session memory recall、conversation recall 注入、deterministic/model-backed memory extraction 初版、extraction agent fenced-prose JSON/wrapped facts/alternate/structured field/nested source object/nested response/fact kind alias parsing、turn-end memory extraction 落盘、prompt history lock/buffered flush/field aliases、official subagent transcript layout、agent metadata sidecar/field aliases、sidechain runtime start/append/finish/cancel/fail 和 parent-chain append/finish 初版、sidechain manager orchestration 初版、sidechain manifest 聚合、sidechain state/list/resume/content-field aliases 初版、sidechain resume context builder、sidechain conversation/content-replacement reconstruction、transcript tail/window/metadata/index loaders、byte-budget transcript tail loader、agent-scoped content replacement metadata/record field-alias loading、session-scoped metadata reappend including AI-title/last-prompt/task-summary、transcript line offset index/window/byte-budget-window/parent-chain/resume/tail/byte-budget-tail loaders、extended transcript metadata entries/type/field aliases、transcript message/session UUID field aliases including top-level `messageUuid`/`messageId`/`id` record IDs and `role`/`entry_type`/`messageType`/`createdAt` timestamp aliases、transcript tombstone metadata delete/relink、transcript resume conversation builder、index 文本预览和 AI-title/last-prompt/task metadata 字段、流式 transcript 搜索、session list pagination、remote history token refresh、remote history 全量分页抓取/page-field/event-list/records/entries/last-id/cursor/event-id/has-next alias/wrapped-data/links/paging/bare-array/connection wrapper response/edge-cursor fallback/max-pages 截断状态/before_id 续抓、remote event transcript materialization/fallback field fill/去重追加/duplicate parent guard、remote history 一步 sync 到 transcript 和 session resume/search/title 支撑已落地；完整 subagent runtime、官方 cached microcompact parity、官方 session memory compaction 策略、完整 memory recall agent 策略仍缺。

本轮补充：remote history connection/pageInfo 解析接受 `hasPrevious`/`hasPreviousPage`、`hasOlder`/`more` 继续分页标记，以及 `previousCursor`/`prevCursor`/`beforeCursor`/`olderCursor` before-id cursor 别名。

本轮补充：remote history response parser 会递归解包 `data.session.events`、`data.projectSession.eventConnection`、`conversation`、`remoteHistory` 等 GraphQL/session wrapper，继续复用 `nodes`/`edges[].node` 和 `pageInfo` pagination 解析。

本轮补充：remote history REST/link 风格分页接受 `links.next`/`links.previous`/`links.prev`/`links.older` 的字符串 URL 或 `{href,url,uri,link}` 对象，并从 before/cursor query 参数提取续抓 before-id。

本轮补充：remote history HTTP `Link` header 分页接受 `previous`/`prev`/`older`/`next` rel URL，并以 body cursor 优先、header cursor fallback 的方式继续抓取。

本轮补充：transcript metadata loader 现在接受 `sessionID` 和 `session` 作为 session-scoped metadata ID 别名，并容忍 `prNumber`、`timeSavedMs`、`lastSpawnTokens` 等计数字段使用数字字符串。

本轮补充：transcript metadata ID helper 现在复用 contract `ID` JSON 解码，`messageID`/`sessionID` 等 metadata ID 字段可接受 JSON number 并保留为字符串。

本轮补充：context-collapse commit metadata 的 collapse/summary/archived ID 字段现在也走 metadata ID helper，支持 JSON number ID 并保持 full/lightweight loader 一致。

本轮补充：context-collapse snapshot metadata 接受 `isArmed`/`enabled` bool 别名、`spawnTokens`/`tokenCount` 计数字段别名，以及 `stagedMessages`/`items` staged payload wrapper，full loader 和 metadata loader 保持一致。

本轮补充：transcript message 和嵌套 contract message 现在接受 `sessionID` 顶层别名，`LoadTranscript`、`LoadTranscriptIndex` 和 indexed resume 会保留该 session id（覆盖测试：`TestLoadTranscriptAcceptsSessionIDUpperAlias`）。

本轮补充：remote history `SDKEvent` 解码现在也接受 `sessionID` 作为事件 session id 别名，materialize 成 transcript message 时会同步填充 record 和嵌套 message 的 session id（覆盖测试：`TestRemoteHistoryTranscriptMessagesAcceptsSessionIDUpperAlias`）。

本轮补充：remote history `SDKEvent` 解码现在接受 `parentUUID`、`parentId`/`parentID`/`parent_id` 和 `parentMessageId`/`parentMessageID`/`parent_message_id` 作为 parent alias，materialize transcript 时会保留 parent chain（覆盖测试：`TestRemoteHistoryTranscriptMessagesAcceptsParentIDAliases`）。

本轮补充：sidechain/subagent state loader 接受 legacy/fork 命名的 start/finish subtype、ID/type/summary 字段别名和常见状态别名，提升旧 subagent transcript resume/list 的恢复率。

本轮补充：conversation runner 现在会在用户消息入队后计算 compact token warning state，并在触发 warning/error/auto-compact/blocking 阈值时发出 `token_warning` event；warning state 接入 blocking-limit override，auto-compact threshold 判断接入 `CLAUDE_AUTOCOMPACT_PCT_OVERRIDE`，使 runtime warning 和自动压缩使用同一套 window 输入。

本轮补充：microcompact disk cache loader 现在读取 Go 默认、camelCase 和 snake_case 字段别名，并容忍计数字段的数字字符串，提升 cached microcompact 文件在不同实现/版本间的恢复率。

### M7: TUI Renderer And Interaction

目标：还原交互式 Claude Code 体验。

需要完成：

- Terminal renderer、layout、event、input、scroll、selection、alternate screen。
- REPL screen、PromptInput、Messages、StatusLine。
- permission dialogs、task dialogs。
- keybindings、vim mode、history/search。
- ANSI snapshots 和交互脚本。

当前状态：轻量 terminal frame renderer、PromptInput 状态机、history 导航、ctrl-p/ctrl-n history navigation、shift-enter 多行输入、多行 prompt 行内 ctrl-a/ctrl-e/ctrl-u/ctrl-k 和 wrap/render/cursor、共享 kill ring、ctrl-b/ctrl-f/ctrl-u/ctrl-k/ctrl-w 行编辑、alt-b/alt-f/alt-d/alt-backspace word 编辑、ctrl-left/ctrl-right/alt-left/alt-right word motion、ctrl-y yank 和 alt-y yank-pop 初版、reverse-search cursor/word 编辑/kill/yank/yank-pop 初版、ctrl-c interrupt/双击退出事件、ctrl-d delete-forward/空输入双击退出事件、ctrl-l 重绘事件、ctrl-o/ctrl-t 全局切换事件、ctrl-g/ctrl-s/ctrl-x chord chat 事件、reverse-search 状态/渲染/脚本断言/空结果/选择回填/cursor 断言、paste/image hint 输入和 OSC ST/base64 filename 兼容、text/image pasted-content 引用/metadata 脚本断言/提交展开/history entry restoration、SGR mouse 解析、alternate terminal navigation key sequences including modified Home/End/Delete/PageUp/PageDown、滚轮滚动、修饰键滚轮/左键、左键拖动选择、viewport 半页/顶部/底部可配置滚动、viewport 点击选择和 dialog action 点击、focus/blur 事件、resize 视口保持、keybinding resolver/config/chord pending/null-unbind/key/action camelCase alias、JSON config loader 和 focus/mouse/paste/image key name 覆盖、vim insert/normal/j/k/word/WORD/ge/gE/line-local ^/$/0/I/A/D/quote/bracket text-object/yank/register/paste/delete/count/replace/undo/find/till/repeat/dot-repeat/G/gg/toggle/join/open-line/indent/substitute 动作、normal-mode arrow/backspace/delete 映射和 operator linewise/字符范围、REPL screen 模型、permission/task dialog builder、dialog kind/id routing/runtime/status line、runtime 到 REPL screen 的 dialog/status 同步、runtime-aware interaction script runner、prompt text/cursor/expanded/vim mode/register/task state/dialog result/runtime mutation/task bulk-cancel/permission cancel/keybinding mutation/status negative/snapshot negative/screen size/event-sequence/event-count/no-event/dialog-result-count/no-dialog-result 脚本断言、viewport 脚本断言、named-key 脚本输入、script JSON/JSONL/wrapper loader、script file runner 和 runtime/task camel field aliases、stale dialog race guard、cancel active、permission id/all cancellation、queued permission promotion、active task dialog refresh、task lifecycle/bulk-cancel 初版、idempotent alternate screen lifecycle/reset/reassert interactive 初版、mouse/focus/bracketed-paste terminal mode lifecycle/reconciliation、ANSI snapshot 基础、snapshot corpus write/compare/script-file compare/missing-baseline/diff/batch/strict unexpected-baseline 状态、scripted interaction runner/assertions/multi-key/text/paste/image/pasted-content metadata 初版、status/dialog/message components、viewport/selection 已落地；完整 ANSI parity、真实 permission/task runtime race/cancel 行为、完整 vim/keybinding 系统、完整 alternate screen lifecycle 和官方交互脚本仍缺。

本轮补充：keybinding config、keymap 解析和 interaction script named-key 输入接受 `ctrl-h`/`ctrl-i`/`ctrl-j`/`ctrl-m`、`ctrl-[`、`ctrl-?`、对应 `control-*` 以及 compact/camel 形式；terminal parser 支持 CSI-u/kitty keyboard protocol 的 ctrl/alt/shift-enter/shift-tab 序列；image hint parser 支持 OSC ST terminator 和 base64 `name=` filename；keybinding JSON loader 支持 wrapper object-map、`shortcuts`、object action 字段、string-array key sequence/chord 和 `null`/`false` unbind；mouse parser 支持 legacy X10/normal tracking 序列；interaction script 支持结构化 mouse/mouse_event 步骤，button 接受 `buttonMask`/`button_mask`/`btn`/`code`/`mask`，坐标接受 `mouseX`/`mouse_x`/`clientX`/`screenX` 和对应 Y/row/line 别名，release 接受 `mouseUp`/`isRelease`/`mouseRelease`/`releaseEvent` 等字段别名；interaction script 还支持字符串 `keys` 和 `input`/`input_text`/`keys_text`/`raw_key`/`paste_text` 字段别名，并允许 status/snapshot/viewport/pasted-content contains 断言使用单字符串或字符串数组，`keybindings`、`expectEvents`、`expectDialogResults`、`expectPrompt.pastedContents` 和 `expectTasks.contains` 使用单对象或对象数组。

本轮补充：interaction script step 接受 `resize`/`terminalSize`/`screenSize` 对象或 `[width,height]` 数组、顶层 `columns`/`rows` resize 别名、`focus`/`focused`/`blur`/`focusIn`/`focusOut` focus event 别名、`snapshot`/`snapshotId`/`snapshotLabel` capture 名称别名，以及 runtime-aware mutation 别名如 `permission`/`permissionRequest`、`task`/`taskStatus`、`removeTask`/`deleteTask`、`cancelPermission`、`cancelTasks`/`cancelReason`、`openTasks`/`showTasks`。

本轮补充：interaction script step 可通过 `status`/`setStatus`/`statusLine`/`baseStatus` 设置状态行；runtime-aware scripts 会把它作为 base status，并继续叠加 permission/task 计数，便于复用带状态栏的 ANSI/interaction fixture。

本轮补充：interaction script 批量消息注入接受 `messages`、`appendMessages` 和 `transcriptMessages` 等字段，并允许单对象自动转数组，便于把 transcript/chat fixture 直接迁入脚本。

本轮补充：interaction script direct `dialog` step 接受和 dialog expectation 对齐的 ID/kind/title/body/actions/focused aliases，减少自定义 dialog fixture 的手工改写。

本轮补充：interaction script loader 接受更多 steps wrapper aliases 和一层 scenario/fixture 嵌套对象，减少把 golden fixture 改写成本地专用格式的需求。

本轮补充：snapshot corpus 支持 `.ansi` only baselines，方便复用真实终端输出 corpus，而不必预先生成 `.txt` companion 文件。

本轮补充：terminal lifecycle 增加可选 extended-key mode，按官方 `CSI >1u`/`CSI >4;2m` 启用 kitty keyboard protocol 和 modifyOtherKeys，退出时重置 modifyOtherKeys 并 pop kitty stack，reassert 时先 pop 再 push，避免长期会话 stack 泄漏。

本轮补充：terminal CSI-u/kitty keyboard parser 接受无 modifier 字段或 modifier `1` 的 base key 序列，覆盖 printable rune、Enter、Tab、Esc 和 Backspace，避免 extended-key 模式下普通键序列被解析成 unknown。

本轮补充：terminal CSI parser 把 DA/device attributes (`CSI c`、`CSI >c`、`CSI =c`) 归入 report action，并在 terminal parser dispatcher 中作为 `TerminalActionReport` 暴露。

本轮补充：terminal CSI parser 接受 `CSI a`/`CSI e`/`CSI \`` cursor alias final bytes，并映射到已有 cursor-forward/cursor-down/cursor-column actions。

本轮补充：terminal CSI parser 接受 DEC private mode `?1047h/l` alternate-screen buffer 和 `?1048h/l` save/restore cursor，复用已有 mode/cursor actions。

本轮补充：terminal CSI parser 把 DECREQTPARM terminal-parameters (`CSI x`) 归入 report action，保留 code/private marker。

本轮补充：terminal CSI parser 把 xterm window manipulation/report (`CSI t`) 归入 report action，覆盖常见 `CSI 14t`/`CSI 18t` 窗口/文本区尺寸查询。

本轮补充：terminal CSI parser 把 TBC tab-clear (`CSI g`/`CSI 3g`) 归入 cursor action，保留 clear-current/all code。

本轮补充：terminal CSI parser 把 REP repeat-preceding-character (`CSI b`) 归入 edit action，visible-text/snapshot pipeline 和 ANSI message wrapping/trim 会按重复次数展开前一个可重复 grapheme。

本轮补充：terminal CSI parser 把 DECSTR soft reset (`CSI !p`) 归入 reset action，并在 terminal parser 中清理 SGR/link 状态。

本轮补充：renderer/snapshot 增加 opt-in DEC 2026 synchronized output 包裹入口，可用官方 BSU/ESU (`CSI ?2026h`/`CSI ?2026l`) 生成整帧 ANSI fixture，同时默认渲染保持不变。

本轮补充：terminal OSC helper 增加 OSC 0 title/icon 序列生成，输入会先 strip ANSI；`StripANSI` 现在会完整跳过 OSC/DCS/APC/PM/SOS payload，避免 title/snapshot 可见文本被终端控制串污染。

本轮补充：terminal OSC helper 增加 OSC 21337 tab status 序列、清理序列和 tmux/screen passthrough 包裹，status 文本按官方规则转义分号和反斜杠。

本轮补充：terminal OSC helper 增加 OSC 8 hyperlink 开始/结束序列，按官方 rolling hash 为 URL 自动生成 `id=`，并允许显式 params 覆盖。

本轮补充：terminal OSC helper 增加 OSC 9;4 progress 序列，覆盖 clear/set/error/indeterminate，running/error 百分比按官方规则 clamp 到 0..100。

本轮补充：terminal OSC helper 增加 iTerm2、Kitty、Ghostty notification 序列和 raw BEL helper，调用方可按环境选择是否 wrap multiplexer。

本轮补充：terminal OSC helper 增加 OSC 52 clipboard 序列生成，固定 clipboard selection `c` 并按 UTF-8 base64 编码 payload；native clipboard/tmux buffer runtime 仍未接入。

本轮补充：terminal OSC helper 增加显式 ST (`ESC \\`) terminator 入口，可按官方 Kitty 避免 BEL 的路径生成 OSC 序列，同时默认 `OSCSequence` 仍保持 BEL terminator。

本轮补充：terminal OSC helper 增加 OSC color parser，支持 `#RRGGBB` 和 XParseColor 风格 `rgb:R/G/B`，按官方规则把 1-4 位 hex component 缩放到 8-bit RGB。

本轮补充：terminal OSC helper 增加 OSC 21337 tab-status payload parser，支持 `\;`/`\\` 转义、bare key 或空值清理、unknown key ignore，并复用 OSC color parser 解析 indicator/status-color。

本轮补充：terminal OSC helper 增加 OSC 8 hyperlink payload parser，按官方规则解析 params、保留 URL 内部分号，并把空 URL 识别为 hyperlink end。

本轮补充：terminal OSC helper 增加轻量 `ParseOSCContent`，覆盖官方 title(0/1/2)、OSC 8 hyperlink、OSC 21337 tab status 和 unknown action 分支。

本轮补充：terminal OSC helper 增加完整 OSC sequence parser，可从带 `ESC ]` 前缀且以 BEL 或 ST 终止的序列解析出 `ParseOSCContent` action。

本轮补充：terminal OSC parser 现在把 OSC 52 clipboard、iTerm2 progress/notification、Kitty notification 和 Ghostty notification 解析为结构化 terminal actions，snapshot/visible-text replay 会继续剥离这些控制串。

本轮补充：terminal renderer constants 增加官方 clear scrollback (`CSI 3J`) 和 legacy Windows home (`CSI 0f`) 序列 helper，支持现代 clear-screen+scrollback 和 legacy Windows clear 组合；平台自动探测仍留给调用方。

本轮补充：terminal CSI helper 增加通用 `CSISequence`、cursor up/down/forward/back/position/move 和 line/screen erase 序列，按官方 helper 的零移动返回空串与 horizontal-first cursorMove 行为生成 ANSI。

本轮补充：terminal CSI helper 增加 scroll up/down、set scroll region 和 reset scroll region 序列，scroll 零值返回空串，便于后续补齐官方 viewport/scroll-region 输出路径。

本轮补充：terminal CSI helper 增加 DECSCUSR cursor-style 序列，覆盖 block/underline/bar 的 blinking 与 non-blinking code，并保留 unknown style 的默认 cursor fallback。

本轮补充：terminal CSI helper 增加 bracketed paste start/end 和 focus in/out 输入 marker 常量，并用现有 parser 验证 focus marker 映射，方便官方交互 fixture 复用原始 CSI marker。

本轮补充：terminal CSI helper 增加 `EraseLinesSequence(n)`，按官方 `eraseLines` 语义逐行 `CSI 2K`、行间上移并以 `CSI G` 回到列 1，`n<=0` 返回空串。

本轮补充：terminal CSI helper 增加官方 CSI param/intermediate/final byte range 常量和判定函数，为后续更完整 CSI parser/action tests 提供基础。

本轮补充：terminal CSI helper 增加官方 CSI final-byte/DEC mode 常量和 `ParseCSISequence` 动作解析，覆盖 cursor move/position/save/restore/show/hide/style、erase display/line/chars、scroll up/down/region、SGR params、alternate-screen/bracketed-paste/mouse/focus mode 和 unknown sequence fallback。

本轮补充：terminal CSI parser 补齐 insert/delete chars、insert/delete lines、forward tab/back tab action，`CSI M` 在 output parser 中按 delete-lines 处理，同时 input tokenizer 仍保留 X10 mouse payload 边界消费。

本轮补充：terminal CSI parser 现在把 DSR (`CSI n`) 解析成 report action，覆盖 device-status、cursor-position 和 private-mode unknown report，避免 terminal status query/response 序列继续落入 generic unknown。

本轮补充：terminal CSI parser 现在把 DEC `?1006h/l` SGR mouse mode 解析成 mouseTracking action，和 lifecycle 发出的 SGR mouse enable/disable 序列闭环。

本轮补充：terminal CSI parser 现在把 DEC `?9h/l` X10 mouse tracking mode 解析成 mouseTracking `x10` action，和 input tokenizer/parser 的 X10 mouse payload 支撑闭环。

本轮补充：terminal CSI parser 现在也把 DEC `?1001h/l` highlight、`?1005h/l` UTF-8 mouse mode 和 `?1015h/l` urxvt numeric mouse mode 解析成 mouseTracking action，和输入侧 numeric mouse 兼容面闭环。

本轮补充：terminal CSI parser 现在把 xterm `?1007h/l` alternate scroll mode 解析成独立 mode action，避免 alternate-screen wheel 兼容序列落入 unknown。

本轮补充：terminal CSI parser 现在把 DEC `?2026h/l` synchronized output mode 解析成 mode action，和 renderer/snapshot 的 BSU/ESU 包裹路径闭环。

本轮补充：terminal ESC helper 增加官方 ESC final-byte 判定和 `ParseESCSequence`/`ParseESCContent`，覆盖 RIS reset、DECSC/DECRC save/restore、IND/RI/NEL cursor action、HTS/charset ignored 分支和 unknown sequence fallback。

本轮补充：terminal SGR helper 增加官方 `TextStyle` 状态解析基础，覆盖 reset、bold/dim/italic/underline/blink/inverse/hidden/strikethrough/overline、普通/亮色命名色、256 色、RGB 色、underline color、分号和冒号参数格式；完整 ANSI parser/render style parity 仍继续推进。

本轮补充：terminal sequence dispatcher 增加官方 `identifySequence` 等价分流，按 CSI/OSC/ESC/SS3/unknown 识别并委派现有 parser，SS3 按官方 output parser 作为 unknown action；streaming tokenizer 和 text grapheme action 仍继续推进。

本轮补充：terminal tokenizer 增加官方 streaming escape boundary 状态机，支持跨 chunk buffer/flush/reset、CSI/SS3/OSC/DCS/APC 序列边界、OSC BEL/ST terminator、ESC intermediate charset 序列、invalid CSI text fallback 和 opt-in X10 mouse payload 消费；完整 text grapheme action parser 仍继续推进。

本轮补充：terminal parser 增加轻量 ANSI action pipeline，串接 tokenizer、CSI/OSC/ESC dispatcher 和 SGR style state，输出 text/bell/cursor/erase/scroll/mode/title/link/tabStatus/reset/unknown action，文本宽度覆盖 ASCII、emoji 和 East Asian wide；完整 grapheme cluster segmentation 和 renderer style parity 仍继续推进。

本轮补充：terminal parser 跟踪 OSC 8 hyperlink start/end 状态，暴露当前 `inLink` 和 `linkUrl`，reset 时清空链接状态，贴近官方 parser 的 link range 状态语义。

本轮补充：terminal parser 的 text grapheme 基础分段补齐 combining mark、variation selector、emoji modifier、ZWJ emoji 序列和 regional indicator flag pair，并按官方多 codepoint grapheme 宽度为 2 的规则计算宽度；完整 Unicode UAX #29 分段仍未宣称完成。

本轮补充：terminal parser 的 text grapheme 分段继续补齐 emoji tag sequence，把 subdivision flag 这类 base emoji + tag chars + cancel tag 作为单个宽 grapheme，避免 wrap/pad/cursor pipeline 拆分视觉 emoji。

本轮补充：terminal CSI parser 现在对 tokenizer flush 出来的非 final-byte incomplete CSI 返回 unknown action，而不是丢弃，贴近官方 `parseCSI` 对 flushed partial sequence 的 fallback 行为。

本轮补充：terminal sequence dispatcher 对 tokenizer flush 出来的 OSC partial sequence 使用 `ParseOSCContent` fallback，允许无 BEL/ST terminator 的 title/link/tab-status content 按官方 parser 语义产出 action。

本轮补充：terminal tokenizer 增加明确的 output/input 构造器，output 路径默认不吞 `CSI M` 后续字节，input 路径默认开启 X10 mouse payload 边界消费，避免调用方误用布尔选项导致 output parser 吞文本或 stdin mouse payload 泄漏。

本轮补充：mouse parser 现在接受 urxvt/xterm 1015 numeric mouse `CSI button;x;yM`，按 legacy offset 还原 button code，左键、释放和滚轮语义与 SGR/X10 mouse 保持一致。

本轮补充：terminal tokenizer 补齐 PM (`ESC ^`) 和 SOS (`ESC X`) string-control 状态，和 OSC/DCS/APC 一样支持 BEL 或 ST terminator，避免这些控制串 payload 泄漏为 text token。

本轮补充：terminal sequence dispatcher/parser 现在把 DCS/APC/PM/SOS string-control 序列分类为 `stringControl` action，保留 payload、terminator 和 incomplete flush 状态，同时 visible text 继续忽略这些不可见控制串。

本轮补充：message renderer 增加 ANSI-aware wrapping/padding，带 SGR 的 message text 会通过 terminal parser 按 grapheme 可见宽度换行，并把 `TextStyle` action 重新渲染为 SGR 序列，避免 escape bytes 参与 layout 宽度计算；普通文本路径保持不变。

本轮补充：基础 wrap/pad/trim 改为按 terminal grapheme 可见宽度计算，普通 message、status/dialog/viewport/prompt 的 CJK/emoji 宽字符不再按单 rune 宽度参与布局，继续向 terminal column parity 收口。

本轮补充：prompt layout 的 chunking 和 cursor column 映射改为按 terminal grapheme 可见宽度计算，宽字符输入换行和 cursor CSI 定位不再按 rune index 误算列宽。

本轮补充：reverse-search footer 的 cursor CSI 定位改为按 query 光标前 terminal grapheme visible width 计算，宽字符历史搜索输入不再按 rune index 误算列宽。

### M8: Commands, Skills, Plugins

目标：还原 slash commands、skills 和 plugin 系统。

需要完成：

- `/help`、`/config`、`/mcp`、`/plugin`、`/skills`、`/memory`、`/resume` 等命令。
- local commands、local-jsx command abstraction。
- bundled/user/plugin/MCP skills discovery。
- plugin manifest、marketplace、install/cache/update。
- plugin hooks/agents/MCP。

当前状态：未开始完整实现。

### M9: MCP Platform

目标：完整 MCP client/server 平台。

需要完成：

- stdio/SSE/HTTP/WebSocket/sdk/claudeai-proxy transport。
- server config merge、policy allow/deny、OAuth。
- resources/prompts/tools/list/call/read。
- MCP tool result truncation/persist。
- elicitation、channel notifications、session-expired。
- 内置工具 MCP server。

当前状态：`cmd/claude-mcp` 仍是占位。

### M10: Agents, Tasks, Worktree, Remote

目标：还原多 agent、后台任务和远端协作。

需要完成：

- AgentTool、built-in/custom agents、frontmatter MCP。
- local agent、async/background task、task output。
- worktree isolation、cleanup、resume。
- remote CCR agent、team/swarm/coordinator。
- SendMessage、TeamCreate、TeamDelete、Task*。

当前状态：未开始完整实现。

### M11: Bridge, LSP, Telemetry, Advanced Integrations

目标：补齐高级集成能力。

需要完成：

- repl bridge、remote-control、session websocket、direct connect。
- LSP manager、diagnostics、LSP tool。
- telemetry/analytics/diagnostics/tracing。
- Chrome/computer-use/voice/native integrations。
- enterprise/gated/platform-specific behavior。

当前状态：未开始完整实现。

## Recommended Next Steps

1. 完成 M5 基础可用工具面：优先 `Glob/Grep`、`TodoWrite`、`Bash`。
2. 补强 `Read/Edit/Write` 高级分支：PDF/image/notebook/diff/LSP/history。
3. 实现 CLI `--print` 和 SDK JSON/NDJSON，用 golden 对齐官方行为。
4. 推进 session resume、compact、memory。
5. 再进入 TUI、MCP、plugins、skills、agents、remote。

## Verification Strategy

每个模块都需要至少三层验证：

- Unit tests：验证 Go 内部逻辑。
- Golden/parity tests：验证 JSON、stdout/stderr、transcript、tool result 等稳定输出。
- Official CLI black-box tests：无法从源码确定的行为，必须用官方 CLI 采样反推。

`go test ./...` 只能说明当前实现内部一致，不能证明 Claude Code 100% parity。
