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

M6 补充：remote history GraphQL/connection 分页现在接受 `hasPrevious`/`hasPreviousPage`、`hasOlder`/`more` 继续分页标记，以及 `previousCursor`/`prevCursor`/`beforeCursor`/`olderCursor` 等 before-id cursor 别名，避免只返回第一页历史。

M6 补充：remote history REST/link 风格分页现在接受 `links.next`/`links.previous`/`links.prev`/`links.older` 的字符串 URL 或 `{href,url,uri,link}` 对象，并从 `before_id`、`beforeId`、`cursor`、`pageCursor`、`previousCursor`、`prevCursor`、`beforeCursor`、`olderCursor`、`startCursor`、`endCursor` 等 query 参数提取下一页 before-id。

M6 补充：remote history 现在也接受 HTTP `Link` header 中 `rel="previous"`/`prev`/`older`/`next` 的分页 URL，并从同一组 before/cursor query 参数中提取续抓 before-id。

M6 补充：sidechain/subagent state loader 现在接受 `subagent_start`/`agent_start`/`task_start` 和 `sidechain_end`/`subagent_finish`/`agent_finish`/`task_summary` 等 subtype 别名，并归一化 `active`/`success`/`canceled`/`error` 等运行状态别名，同时读取 `subagentId`/`agentID`、`subagentType`、`finalSummary` 等 content 字段。

M6 补充：sidechain agent metadata sidecar 读取现在接受 `type`/`subagentType`/`agentName`/`name`、`workspacePath`/`workspace`/`path`/`directory`、`taskDescription`/`prompt`/`input`/`command`/`title` 等字段别名，兼容历史或第三方生成的 subagent metadata。

M6 补充：transcript metadata loader 现在会按 `messageId` 建立 file-history snapshot 和 attribution snapshot 索引，并接受 `message_id`/`messageUuid`/`id` 等字段别名，和官方按消息恢复 snapshot 的语义对齐。

M6 补充：transcript/index/session list 现在读取消息上的 `gitBranch`，接受 `git_branch`/`branch` 别名，并允许 session search 按分支名命中，补齐官方 lite metadata 中的 branch 恢复/检索语义。

M6 补充：full transcript 的 `TitleFromTranscript` 标题优先级现在和 indexed/lite 路径一致，按 custom title、AI title、首个用户 prompt、last-prompt、summary 顺序兜底，避免 resume/search/list 标题分叉。

M6 补充：transcript/index/session list 现在读取消息上的 `cwd` 作为 project path，接受 `projectPath`/`project_path`/`workingDirectory`/`working_directory` 等别名，并允许 session search 按项目路径命中，贴近官方 lite metadata 的 projectPath 恢复语义。

M6 补充：TranscriptMessage 现在结构化读取官方 SerializedMessage 元数据 `userType`、`entrypoint`、`version`、`slug`，并兼容 `user_type`/`userKind`、`entry_point`/`client`、`appVersion`/`claudeCodeVersion`、`sessionSlug`/`planSlug` 等别名。

M6 补充：lightweight transcript metadata loader 现在和 full loader 一样在 `system`/`compact_boundary` 后清空旧 `marble-origami-commit`/`marble-origami-snapshot` 状态，避免 metadata-only resume/inspect 路径保留 compact 前的过期 context-collapse 记录。

M6 补充：memory 层现在提供官方 `memoryAge`/freshness note 语义，`ReadDocumentsWithOptions` 可为超过 1 天的 memory 文档前缀 system-reminder，提示模型把 memory 视为 point-in-time 并验证当前代码。

M6 补充：memory 层新增官方 `relevant_memories` attachment 基础结构，支持 stable memory header、system-reminder 渲染、已 surfaced memory path/byte 扫描、按 200 行/4096 bytes 读取并附截断提示的 surfacing reader、mark-after-filter 的 duplicate memory attachment 过滤、最后非 meta user prompt/单词 prompt/60KB session cap 的 prefetch gating、多目录结果排除 read-state/surfaced 后取前 5 个候选的选择逻辑，以及 recent successful tools 窗口收集并排除 pending/failed/同名失败工具，为后续 selector/prefetch runtime 接入留好纯函数边界。

M6 补充：conversation request 构建现在会把 history 中的 `relevant_memories` attachment 展开成 user/meta system-reminder 后再进入 Anthropic request，避免 attachment message 在 NormalizeForAPI 路径被丢弃。

M6 补充：Runner 现在支持显式 `RelevantMemoryDir` runtime 接线：按最后非 meta user prompt 扫描 memory dir、deterministic 选择相关 md memory、读取成 `relevant_memories` attachment 并注入 request；默认不开启，完整官方 async sideQuery selector/prefetch 仍后续推进。

M6/M7 补充：Runner 会把 `RelevantMemoryDir` 透传到 tool metadata 的 internal auto-memory path context，让 Read tool freshness prefix 和 permission internal-path policy 在同一 memory dir 配置下生效。

M6 补充：transcript resume 在 fallback 转换 attachment message 时会保留 raw attachment payload，确保恢复出的 `relevant_memories` attachment 仍可被 request 构建路径展开为 system-reminder。

M6/M7 补充：Read tool 现在在 metadata 提供 auto-memory 目录时，为读取到的旧 auto-memory 文件前缀 freshness system-reminder，贴近官方 FileReadTool 的 memory freshness prefix。

M7 补充：keybinding resolver/config 和脚本 named-key 输入已覆盖 `ctrl-h`/`ctrl-i`/`ctrl-j`/`ctrl-m`、`ctrl-[`、`ctrl-?` 及 `control-*` 终端别名；terminal parser 支持 CSI-u/kitty keyboard protocol 的 ctrl/alt/shift-enter/shift-tab 序列；image hint parser 支持 OSC ST terminator 和 base64 `name=` filename；keybinding JSON loader 支持 wrapper object-map、`shortcuts`、object action 字段、string-array key sequence/chord 和 `null`/`false` unbind；mouse parser 支持 legacy X10/normal tracking 序列；interaction script 支持结构化 mouse/mouse_event 步骤，button 接受 `buttonMask`/`button_mask`/`btn`/`code`/`mask`，坐标接受 `mouseX`/`mouse_x`/`clientX`/`screenX` 和对应 Y/row/line 别名，release 接受 `mouseUp`/`isRelease`/`mouseRelease`/`releaseEvent` 等字段别名；interaction script 还支持字符串 `keys` 和 `input`/`input_text`/`keys_text`/`raw_key`/`paste_text` 字段别名，status/snapshot/viewport/pasted-content contains 断言接受单字符串或字符串数组，且 `keybindings`、`expectEvents`、`expectDialogResults`、`expectPrompt.pastedContents`、`expectTasks.contains` 接受单对象或对象数组。

M7 补充：interaction script 的 `message` step 现在接受 chat/transcript 风格的 `type`/`speaker` role 别名和 `content`/`body`/`message` text 别名；`image` step 接受 `fileName`/`file_name`/`name`、`mimeType`/`mime_type`/`contentType` 和 `data`/`base64` 内容别名；permission request step 接受 request/permission/tool-use ID、path、description 和 action 字段别名，并允许 `actions` 使用单字符串；`expectPrompt` 接受 `value`/`input`/`content`/`message`、`expandedText`/`fullText`、`cursorIndex`/`cursorPosition`、`isEmpty`/`blank` 等字段别名，且 `pastedContents` 断言接受 `pastedId`/`pastedContentId`、`kind`/`pastedType`、`value`/`data`/`base64`、`contentType`/`mimeType`、`fileName`/`name` 和 `contains` 等字段别名；`expectVim` 接受 `vimEnabled`/`isEnabled`、`vimMode`/`modeName`/`currentMode`、`vimRegister`/`registerValue`/`yankRegister`、`registerLinewise`/`linewise` 等字段别名；`expectTasks` 接受 `taskCount`/`total`/`size`/`length` 和 `statusCounts`/`countsByState` 等字段别名；`expectScreen`/`expectViewport` 接受 `columns`/`rows`、`screenWidth`/`screenHeight`、`scrollOffset`/`viewportOffset`、`visibleRows`/`lineCount` 等字段别名；`expectReverseSearch` 接受 `isActive`/`visible`/`open`、`search`/`term`/`pattern`、`cursorIndex`、`currentResult`、`matchCount`/`matches`、`noMatches` 等字段别名；`expectDialog` 可断言 body contains/not-contains、actions/action contains/not-contains、action count 和 focused action，runtime-aware scripts 会在步骤间保留 dialog focused action，且接受 `isActive`/`visible`、`dialogId`/`dialogID`、`dialogKind`、`heading`/`header`、`content`/`text`/`message` 等字段别名；`expectEvent`/`expectDialogResult` 接受 `eventType`/`event`/`name`、`payload`/`text`/`message`、`dialogId`/`dialogID`/`dialogKind`、`actionValue`/`resultStatus`/`exists`/`isStale` 等字段别名。

M7 补充：interaction script 现在接受 `messages`、`append_messages`/`appendMessages`、`transcript_messages`/`transcriptMessages` 批量消息注入字段，且这些字段既可用单个对象也可用对象数组，消息对象沿用 chat/transcript role/text 别名。

M7 补充：interaction script 的 direct `dialog` step 现在接受 `dialogId`/`dialogID`、`dialogKind`、`heading`/`header`、`content`/`text`/`message`、`options`/`choices`/`buttons` 和 `focusedIndex`/`selectedIndex` 等字段别名，并允许 actions/options 使用单字符串。

M7 补充：interaction script loader 现在接受 `scriptSteps`/`script_steps`、`interactionSteps`/`interaction_steps` wrapper 字段，并能从一层 `scenario`/`test`/`case`/`fixture`/`interaction` 对象里继续解析脚本步骤。

M7 补充：ANSI snapshot corpus 比对现在在 `.txt` baseline 缺失时可读取 `.ansi` baseline 并 strip ANSI 后比较，strict unexpected-baseline 检查也会纳入 `.ansi` 文件。

M7 补充：interaction script step 现在接受 `resize`/`terminalSize`/`screenSize` 对象或 `[width,height]` 数组，以及顶层 `columns`/`rows` resize 别名；`focus`/`focused`/`blur`/`focusIn`/`focusOut` 会走正常 focus event 路径；snapshot capture 接受 `snapshot`/`snapshotId`/`snapshotLabel` 等名称别名；runtime-aware scripts 接受 `permission`/`permissionRequest`、`task`/`taskStatus`、`removeTask`/`deleteTask`、`cancelPermission`、`cancelTasks`/`cancelReason`、`openTasks`/`showTasks` 等 mutation 别名。

M7 补充：interaction script JSONL loader 单行上限提升到 50MiB，和 transcript/session 大记录读取容忍度对齐，避免大型 paste、image metadata 或 snapshot fixture 脚本行触发 scanner token limit。

M7 补充：terminal CSI-u/kitty keyboard parser 现在接受 codepoint alternate 和 modifier event-type 的冒号字段（如 `CSI 97:65;5:1u`），按主 codepoint/modifier 解析 ctrl/alt/shift/rune 键，避免 kitty progressive keyboard protocol 变体被判为 unknown。

M7 补充：prompt history 写入现在按官方 `history.ts` 过滤 image pasted content，不再把 image base64/filename/mediaType 写入 `history.jsonl`；历史读取仍兼容旧 image metadata。

M7 补充：paste-cache 现在提供按 cutoff mtime 清理旧 `.txt` paste 文件的 best-effort 入口，忽略不存在的 cache 目录、非 `.txt` 文件和单文件清理错误，贴近官方 `cleanupOldPastes` 行为。

M7 补充：Buffered prompt history writer 现在支持撤销最近 pending entry 的 fast path，给中断/自动恢复场景接入官方 `removeLastFromHistory` 的 pending-buffer 语义留下可测试入口。

M7 补充：Buffered prompt history writer 现在也支持撤销已 flush entry 的 slow path：记录最近 add 的 timestamp，并在同一 writer 的 up-arrow/ctrl-r 历史读取中按当前 session 跳过，保持 `history.jsonl` append-only。

M7 补充：image-cache 现在有 session-scoped 存取基础：图片 paste 可按官方 `image-cache/<session>/<id>.<ext>` 路径缓存、base64 落盘为 0600 文件、批量只存 image 内容、查询内存路径，并清理非当前 session 的旧 image-cache 目录。

M7 补充：PromptInput/REPL screen 现在可启用 session-scoped image-cache；image hint paste 进入 prompt 时会缓存 `[Image #N]` 对应文件路径并把 base64 图片写入 `image-cache/<session>`，贴近官方 PromptInput 的 `cacheImagePath` + `storeImage` 行为。

M7 补充：prompt submit event 现在会保留 display 文本和 pasted-content metadata；session 层 `PromptMessages` 可把 text paste refs 展开、把 image paste refs 转成 Anthropic `image` content block 的 `source` 字段，并追加 image-cache source-path meta message。

M7 补充：pasted image metadata 现在保留 `dimensions` 和 `sourcePath`，读取接受 `source_path`/snake-case dimension aliases；image meta message 会按官方 `createImageMetadataText` 规则输出 source path、原始尺寸、显示尺寸和坐标换算倍率。

M7 补充：PromptInput paste 现在先 strip ANSI、把 `\r` 归一化为换行并把 tab 展开为 4 个空格；REPL screen 按官方 `PASTE_THRESHOLD=800` 和 `min(rows-10, 2)` 可见行阈值决定短 paste 内联还是折叠为 `[Pasted text #N]`。

M7 补充：PromptInput 现在会在输入编辑后清理已删除 `[Image #N]` pill 对应的 orphan image pasted-content，并且 session `PromptMessages` 提交构造会再次过滤未引用图片，避免孤儿图片进入 Anthropic image block 或 metadata。

M7 补充：image paste pill 现在匹配官方 lazy-space 行为：连续粘贴图片会自动写成 `[Image #1] [Image #2]`，图片后直接输入非空白字符会补一个空格，显式空格或换行不会重复补空格。

M7 补充：REPL message metadata 现在保留 `imagePasteIds`，并在 `SetMessages`/`AppendMessage` 时扫描用户消息里的 pasted refs 与 image ids 来推进 `NextPastedID`，避免 resume/continue 后新 paste ID 和历史消息冲突。

M7 补充：reverse-search 现在基于完整 `HistoryEntry` 匹配和选中历史项，选择后会恢复 text/image pasted-content metadata，并让随后的提交继续携带 display 与图片元数据。

M7 补充：REPL message restore 现在可从用户消息的 content blocks、`imagePasteIds` 和 pasted-content metadata 恢复 prompt，重建 `[Image #N]` 引用和 base64 image pasted contents，贴近官方 message selector restore 路径。

M7 补充：Ctrl-S prompt stash 现在保存并恢复 prompt text、cursor 和 pasted-content metadata，空 prompt 时可 unstash，贴近官方 `chat:stash` 行为。

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

本轮补充：remote history REST/link 风格分页接受 `links.next`/`links.previous`/`links.prev`/`links.older` 的字符串 URL 或 `{href,url,uri,link}` 对象，并从 before/cursor query 参数提取续抓 before-id。

本轮补充：remote history HTTP `Link` header 分页接受 `previous`/`prev`/`older`/`next` rel URL，并以 body cursor 优先、header cursor fallback 的方式继续抓取。

本轮补充：sidechain/subagent state loader 接受 legacy/fork 命名的 start/finish subtype、ID/type/summary 字段别名和常见状态别名，提升旧 subagent transcript resume/list 的恢复率。

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

本轮补充：interaction script 批量消息注入接受 `messages`、`appendMessages` 和 `transcriptMessages` 等字段，并允许单对象自动转数组，便于把 transcript/chat fixture 直接迁入脚本。

本轮补充：interaction script direct `dialog` step 接受和 dialog expectation 对齐的 ID/kind/title/body/actions/focused aliases，减少自定义 dialog fixture 的手工改写。

本轮补充：interaction script loader 接受更多 steps wrapper aliases 和一层 scenario/fixture 嵌套对象，减少把 golden fixture 改写成本地专用格式的需求。

本轮补充：snapshot corpus 支持 `.ansi` only baselines，方便复用真实终端输出 corpus，而不必预先生成 `.txt` companion 文件。

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
