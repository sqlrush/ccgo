# Claude Code Go Rewrite Plan

目标：把 `/Users/sqlrush/agent/claude-code` 当前 Claude Code TypeScript 源码快照，用 Go 重写为行为兼容实现，并以“100% 功能还原”为最终验收标准。

## 1. 完成定义

“100% 还原”不能按“源码目录都写了 Go 文件”定义，必须按外部行为定义：

- CLI 参数、环境变量、退出码、stdout/stderr、日志格式兼容。
- 交互 TUI 的消息流、输入行为、权限弹窗、任务面板、状态栏、快捷键、resume/history 兼容。
- SDK/headless 输出事件、JSON/NDJSON、control protocol 兼容。
- Anthropic API 请求语义兼容，包括 tools schema、beta headers、thinking、effort、prompt cache、retry/fallback、usage/cost。
- Tool 行为兼容，包括成功、失败、权限拒绝、取消、并发、输出截断、落盘和 UI 展示。
- settings、MCP、plugins、skills、hooks、session JSONL、memory、compact、bridge、remote、agent/task 行为兼容。
- 能读取旧 Claude Code 配置和 transcript，并能生成旧版本可理解的数据。

当前源快照缺少部分核心类型/模块，因此最终完成还要求：

- 补齐缺失源码，或
- 用官方 CLI 黑盒行为测试反推出等价契约，且每个反推点有 golden 证据。

## 2. 总体策略

采用“契约优先 + Golden 回放 + 模块替换”的方式。

不建议逐文件翻译，原因：

- TypeScript 源中 React/Ink/TUI、Zod schema、feature gate、dynamic import 和循环依赖非常重。
- 当前快照缺少 `types/message`、`types/tools` 等关键类型。
- Go 需要重建 runtime 边界：context cancellation、goroutine concurrency、terminal renderer、JSON schema、MCP transport、shell sandbox。

推荐节奏：

1. 建立契约层和 parity 测试框架。
2. 实现最小 headless query loop 和核心工具。
3. 扩展到完整 CLI/TUI。
4. 逐步接入 MCP、plugins、skills、hooks、agents、bridge 等平台能力。
5. 用 golden corpus 和官方 CLI 对比收敛细节。

## 3. 目标仓库结构

```text
cmd/claude/                  # 主 CLI
cmd/claude-mcp/              # 内置工具 MCP server，可选
pkg/sdk/                     # Go SDK/public control/event contract
pkg/mcpserver/               # 对外 MCP server adapter
internal/bootstrap/          # 启动、feature matrix、global session state
internal/cli/                # 参数、headless、structured IO
internal/config/             # paths、settings、managed policy、migrations
internal/auth/               # API key、OAuth、secure storage
internal/model/              # model aliases/capabilities/providers/cost
internal/api/anthropic/      # API streaming、headers、retry、usage
internal/contracts/          # JSON/schema/message/session/tool contracts
internal/messages/           # message builders/normalizers/mappers
internal/session/            # transcript、resume、search、sidechains
internal/conversation/       # query loop、tool loop、compact、stop hooks
internal/tool/               # Tool interface、registry、schema、render adapter
internal/tools/...           # built-in tools by domain
internal/permissions/        # permission modes/rules/decisions
internal/sandbox/            # sandbox adapter and config
internal/tui/                # terminal renderer and UI components
internal/commands/           # slash/local commands
internal/skills/             # skills loading and invocation
internal/plugins/            # plugin/marketplace/cache
internal/mcp/                # MCP client/transports/config/OAuth
internal/hooks/              # lifecycle hooks
internal/tasks/              # local/remote/background task lifecycle
internal/agent/              # subagents/swarm/worktree/remote agent
internal/memory/             # CLAUDE.md/session/team/auto memory
internal/compact/            # compact/microcompact/context management
internal/bridge/             # repl bridge / remote control
internal/remote/             # CCR sessions / websocket
internal/lsp/                # LSP manager and diagnostics
internal/platform/           # shell/fs/git/keychain/os helpers
internal/telemetry/          # analytics/diagnostics/tracing
test/parity/                 # golden tests against TS/official behavior
```

## 4. 里程碑

### M0: 证据和缺口闭环

产出：

- 源快照 inventory 固化。
- 缺失 import 目标清单。
- 官方 CLI 黑盒采样脚本。
- 功能矩阵：external/internal/gated/enterprise。

验收：

- 每个缺失契约都有“恢复源码 / 反推 golden / 暂不可证”的状态。
- 不再把缺失源码误判为已实现范围。

### M1: Go scaffold 和契约层

产出：

- Go module、目录结构、lint/test/golden test 框架。
- `internal/contracts`：Message、Tool、Command、Permission、Settings、MCP、Session、SDK event。
- JSON schema 生成/校验工具。

验收：

- 能解析现有 settings 片段、MCP config、session JSONL 样本。
- SDK/headless event 结构有 golden fixtures。

### M2: CLI/bootstrap/config/auth/model

产出：

- `cmd/claude` 基本参数和 mode dispatch。
- 配置路径、settings merge、managed policy、migrations。
- API key/OAuth/secure store abstraction。
- model aliases/capabilities/cost。

验收：

- `--version`、基础 help、settings 读取、auth 状态输出与基线一致。
- settings 后向兼容测试覆盖未知字段和旧字段。

### M3: API client 和 conversation loop

产出：

- Anthropic streaming client。
- query loop、tool_use/tool_result pairing、retry/fallback、stop hooks 框架。
- usage/cost/token accounting。

验收：

- 不启用工具时的 prompt -> streaming -> assistant 输出可跑通。
- API request golden 能比对 headers/body 中关键字段。

### M4: Tool framework、permissions、sandbox

产出：

- Go Tool interface/registry。
- permission rules/modes/decision engine。
- hook/classifier/permission prompt 接入点。
- sandbox adapter 接口。

验收：

- allow/deny/ask、path rule、MCP rule、agent rule、mode rule 有覆盖。
- 并发安全工具并发，非安全工具串行。

### M5: 核心工具

优先顺序：

1. Read/Edit/Write
2. Bash
3. Glob/Grep
4. TodoWrite
5. WebFetch/WebSearch
6. Notebook/PDF/image
7. PowerShell

验收：

- 每个工具具备输入校验、权限、执行、结果、错误、截断、session 记录 golden。
- Read-before-write、mtime changed、UNC 防护、team memory secret guard 覆盖。
- Bash parser/sandbox/timeout/background/interrupt 有回放测试。

### M6: Session、memory、compact

产出：

- JSONL transcript 读写。
- resume/search/title。
- sidechain/subagent transcript。
- CLAUDE.md/memdir/session memory。
- compact/auto compact/token warning。

验收：

- 能读取旧 transcript 并恢复对话。
- compact 前后消息链和 API 请求保持 golden。

当前进度：

- 已落地 JSONL/session 基础、resume/search/title 支撑、prompt history lock/buffered flush/field aliases、official subagent transcript layout、agent metadata sidecar/field aliases、sidechain runtime start/append/finish/cancel/fail 和 parent-chain append/finish 初版、sidechain manager orchestration 初版、sidechain manifest 聚合、sidechain state/list/resume/content-field aliases 初版、sidechain resume context builder、sidechain conversation/content-replacement reconstruction、transcript tail/window/metadata/index loaders、byte-budget transcript tail loader、agent-scoped content replacement metadata/record field-alias loading、session-scoped metadata reappend including AI-title/last-prompt/task-summary、transcript line offset index/window/byte-budget-window/parent-chain/resume/tail/byte-budget-tail loaders、extended transcript metadata entries/type/field aliases、transcript message/session UUID field aliases including top-level `messageUuid`/`messageId`/`id` record IDs and `role`/`entry_type`/`messageType`/`createdAt` timestamp aliases、transcript tombstone metadata delete/relink、transcript resume conversation builder、index 文本预览和 AI-title/last-prompt/task metadata 字段、流式 transcript 搜索、session list pagination、remote history token refresh、remote history 全量分页抓取/page-field/event-list/records/entries/eventList/sessionEvents/last-id/cursor/event-id/has-next alias/wrapped-data/links/paging/bare-array/connection/eventConnection/sessionEventsConnection wrapper response/edge-cursor fallback/max-pages 截断状态/before_id 续抓、remote event transcript materialization/fallback field fill/去重追加/duplicate parent guard、remote history 一步 sync 到 transcript、CLAUDE.md/memdir 扫描、team-memory secret guard、compact runner/boundary plan、conversation auto-compact、失败熔断、microcompact/cache 初版、persistent cached microcompact 初版、cache digest structural/rich-content metadata 覆盖、cache version/TTL/prune、in-memory micro cache prune、memory-cache write-through 到磁盘、atomic cache write、坏缓存默认 fail-open、session memory summary/frontmatter aliases/recall 初版、model-ranked recall session-id selection 和 invalid-selection fallback 初版、recall agent alternate/camel response keys/fenced-prose JSON extraction/scalar id parsing/nested/wrapped/collection-alias selection parsing、resume context model-assisted recall 接入、session memory rollup/prune compaction、rollup archive exclusion/merge、rune-safe rollup truncation、resume context + session memory recall、conversation recall 注入、deterministic/model-backed memory extraction 初版、extraction agent fenced-prose JSON/wrapped facts/alternate/structured field/nested source object/nested response/fact kind alias parsing 和 turn-end memory extraction 落盘。
- 本轮补充：sidechain agent metadata sidecar 读取接受 `type`/`subagentType`/`agentName`/`name`、`workspacePath`/`workspace`/`path`/`directory`、`taskDescription`/`prompt`/`input`/`command`/`title` 等字段别名，避免历史或第三方 subagent sidecar 在 resume/list 时丢失 agent 类型、worktree 路径或任务描述。
- 本轮补充：transcript metadata loader 为 file-history snapshot 和 attribution snapshot 建立 `messageId` 索引，并接受 `message_id`/`messageUuid`/`id` 等字段别名，保留 raw list 的同时支持按消息恢复 snapshot。
- 本轮补充：transcript message/index/session list 读取 `gitBranch`，兼容 `git_branch`/`branch` 别名，并让 session search 可以按分支名命中，贴近官方 lite metadata 的 branch 展示和检索行为。
- 本轮补充：full transcript `TitleFromTranscript` 的标题优先级和 indexed/lite 路径对齐，按 custom title、AI title、首个用户 prompt、last-prompt、summary 顺序兜底。
- 本轮补充：lightweight transcript index 的 `content-replacement` 计数按请求的 session id 过滤，避免 session list/search 摘要被同文件其它 session 污染。
- 本轮补充：transcript message/index/session list 读取 `cwd` 作为 project path，兼容 `projectPath`/`project_path`/`workingDirectory`/`working_directory` 等别名，并让 session search 可以按项目路径命中，贴近官方 lite metadata 的 projectPath 恢复行为。
- 本轮补充：TranscriptMessage 结构化读取官方 SerializedMessage 元数据 `userType`、`entrypoint`、`version`、`slug`，并兼容 user/entrypoint/version/slug 的 snake/camel/旧字段别名，减少旧 transcript 只能靠 raw JSON 保留元数据的情况。
- 本轮补充：model-backed session memory recall prompt 现在显式写入 requested limit 和 excluded current session id，减少模型返回超量或当前 session 后再 fallback 的概率。
- 本轮补充：remote history connection/pageInfo 解析接受 `hasPrevious`/`hasPreviousPage`、`hasOlder`/`more` 继续分页标记，以及 `previousCursor`/`prevCursor`/`beforeCursor`/`olderCursor` before-id cursor 别名，覆盖 GraphQL 向更旧事件翻页的响应形态。
- 本轮补充：remote history response parser 会递归解包 `data.session.events`、`data.projectSession.eventConnection`、`conversation`、`remoteHistory` 等 GraphQL/session wrapper，继续复用 `nodes`/`edges[].node` 和 `pageInfo` pagination 解析。
- 本轮补充：transcript resume 的嵌套 content block 接受 `toolUseId`/`toolUseID`、`isError`、`cacheControl`、`cacheReference` 字段别名，并保留 cache edit 的 `cacheReference`。
- 本轮补充：lightweight transcript metadata loader 在 `system`/`compact_boundary` 后清空旧 `marble-origami-commit`/`marble-origami-snapshot` 状态，和 full loader/官方 sessionStorage compact-boundary 语义一致。
- 本轮补充：transcript metadata loader 接受 `sessionID`/`session` 作为 session-scoped metadata ID 别名，并容忍 `prNumber`、`timeSavedMs`、`lastSpawnTokens` 等计数字段使用数字字符串。
- 本轮补充：transcript message 和嵌套 contract message 接受顶层 `sessionID` 作为 session id 别名，`LoadTranscript`、`LoadTranscriptIndex` 和 indexed resume 会保留该 session id（覆盖测试：`TestLoadTranscriptAcceptsSessionIDUpperAlias`）。
- 本轮补充：嵌套 contract message 接受 `parentUUID`、`parentId`/`parentID`/`parent_id`、`parentMessageId`/`parentMessageID`/`parent_message_id` 和 parent-message UUID 别名，transcript/remote history payload 自带 parent alias 时不会丢失嵌套 parent。
- 本轮补充：嵌套 contract message 接受 `messageId`/`messageID`/`message_id` 和 `messageUuid`/`messageUUID`/`message_uuid` 作为自身 ID/UUID 别名，indexed resume 会保留 payload 自带的 nested message id。
- 本轮补充：基础 `SessionEntry` JSONL loader 接受 `role`/`entryType`/`messageType`、message ID/UUID、parent ID/UUID 和 `sessionID`/`session`/session UUID 别名，旧 entry 文件可通过 `session.Load` 保留类型、parent 和 session。
- 本轮补充：tombstone metadata target/parent 加宽到 `targetId`/`deletedId`/`messageId` 和 `parentId`/`parentMessageId` ID/UUID 别名，delete/relink replay 可兼容旧字段拼写。
- 本轮补充：transcript metadata 加宽 summary `leafID`/message ID、content-replacement `agentID`/`toolUseID`/`blockID` 和 context-collapse `collapseID`/`summaryID`/archived ID 别名，full loader 和 lightweight metadata loader 保持一致。
- 本轮补充：remote history `SDKEvent` 解码接受顶层 `sessionID` 作为事件 session id 别名，materialize transcript 时会填充 record 与嵌套 message 的 session id（覆盖测试：`TestRemoteHistoryTranscriptMessagesAcceptsSessionIDUpperAlias`）。
- 本轮补充：remote history `SDKEvent` 解码接受 `parentUUID`、`parentId`/`parentID`/`parent_id`、`parentMessageId`/`parentMessageID`/`parent_message_id` parent aliases，materialize transcript 时会保留 parent chain（覆盖测试：`TestRemoteHistoryTranscriptMessagesAcceptsParentIDAliases`）。
- 本轮补充：memory 层补齐官方 `memoryAge`/freshness note 语义，`ReadDocumentsWithOptions` 可为超过 1 天的 memory 文档前缀 system-reminder，提示模型把 memory 当作 point-in-time observation 并核对当前代码。
- 本轮补充：Read tool 在 metadata 提供 auto-memory 目录时，会为读取旧 auto-memory 文件的 tool result 前缀 freshness system-reminder，和官方 FileReadTool 的 memory freshness prefix 对齐。
- 本轮补充：memory 层补齐官方 `relevant_memories` attachment 基础，包含 stable memory header、system-reminder 渲染、surfaced path/byte 扫描、按 200 行/4096 bytes 读取并附截断提示的 surfacing reader、mark-after-filter 的 duplicate memory attachment 过滤、最后非 meta user prompt/单词 prompt/60KB session cap 的 prefetch gating、多目录结果排除 read-state/surfaced 后取前 5 个候选的选择逻辑，以及 recent successful tools 窗口收集并排除 pending/failed/同名失败工具。
- 本轮补充：conversation `BuildRequest` 会把 history 里的 `relevant_memories` attachment 展开为 user/meta system-reminder 后再 NormalizeForAPI，补齐 official messages.ts attachment 渲染路径的基础 runtime 接线。
- 本轮补充：Runner 增加显式 `RelevantMemoryDir` runtime：配置后会扫描 memory dir、默认用 deterministic selector 选出相关 md memory，若配置 `MemoryAgentClient` 则优先用 model-backed selector，读取为 `relevant_memories` attachment 并注入 request；默认关闭。
- 本轮补充：Runner 会把 `RelevantMemoryDir` 放入 tool metadata 的 internal auto-memory path context，使 Read tool 的 stale-memory freshness prefix 和 permission internal-path policy 与同一配置对齐。
- 本轮补充：transcript resume fallback 转换 attachment message 时保留 raw attachment payload，恢复后的 `relevant_memories` attachment 仍可进入 conversation request 的 system-reminder 展开路径。
- 本轮补充：memory 层增加可取消 `PrefetchRelevantMemories` runtime，复用现有 gating/selector/surfacing 逻辑返回 plan、selection 和 attachments；conversation `RunTurn` 会在用户消息进入后启动 relevant memory prefetch，并在第一轮 model request 消费结果，预取文件系统错误 fail-open 且不阻断主请求。
- 本轮补充：relevant memory prefetch 接入 model-backed sideQuery selector：当 `MemoryAgentClient` 可用时，先向模型提供候选 memory manifest，请模型返回 `memory_paths`/`memoryPaths`/`filePath`/`matches`/嵌套 selection 等路径别名，按模型顺序读取附件；模型错误或无效路径会 fail-open 回落 deterministic selector。完整官方 prompt/telemetry parity 仍继续推进。
- 本轮补充：model-backed relevant memory selector prompt 现在包含 recent successful tools 和 already-surfaced memory paths 的有界上下文，模型侧选择与 deterministic prefilter 的 tool/surfaced 约束更一致。
- 本轮补充：conversation runner 会在用户消息入队后基于 compact window 计算 token warning state，并在达到 warning/error/auto-compact/blocking 阈值时发出 `token_warning` event；warning state 接入 blocking-limit env override，auto-compact 判断接入既有 `CLAUDE_AUTOCOMPACT_PCT_OVERRIDE`，避免 warning 和 compact 阈值来源分叉。
- 本轮补充：microcompact disk cache loader 现在读取 Go 默认、camelCase 和 snake_case 字段别名，并容忍计数字段的数字字符串，提升 cached microcompact 文件在不同实现/版本间的恢复率。

### M7: TUI renderer 和交互体验

产出：

- terminal renderer、layout、event、input、scroll、selection、alternate screen。
- REPL screen、PromptInput、Messages、StatusLine、permission dialogs、task dialogs。
- keybindings/vim/history/search。

验收：

- ANSI snapshot 和交互脚本覆盖主路径。
- 窗口尺寸变化、Ctrl-C/Esc/Enter、paste/image hint、permission ask/deny/allow 都有测试。

当前进度：

- 已落地轻量 terminal frame renderer、PromptInput/history、ctrl-p/ctrl-n history navigation、shift-enter 多行输入、多行 prompt 行内 ctrl-a/ctrl-e/ctrl-u/ctrl-k 和 wrap/render/cursor、共享 kill ring、ctrl-b/ctrl-f/ctrl-u/ctrl-k/ctrl-w 行编辑、alt-b/alt-f/alt-d/alt-backspace word 编辑、ctrl-left/ctrl-right/alt-left/alt-right word motion、ctrl-y yank 和 alt-y yank-pop 初版、reverse-search cursor/word 编辑/kill/yank/yank-pop 初版、ctrl-c interrupt/双击退出事件、ctrl-d delete-forward/空输入双击退出事件、ctrl-l 重绘事件、ctrl-o/ctrl-t 全局切换事件、ctrl-g/ctrl-s/ctrl-x chord chat 事件、reverse-search 状态/渲染/脚本断言/空结果/选择回填/cursor 断言、paste/image hint 输入和 OSC ST/base64 filename 兼容、text/image pasted-content 引用/metadata 脚本断言/提交展开/history entry restoration、SGR mouse 解析、alternate terminal navigation key sequences including modified Home/End/Delete/PageUp/PageDown、滚轮滚动、修饰键滚轮/左键、左键拖动选择、viewport 半页/顶部/底部可配置滚动、viewport 点击选择和 dialog action 点击、focus/blur 事件、resize 视口保持、keybinding resolver/config/chord pending/null-unbind/key/action camelCase alias、JSON config loader 和 focus/mouse/paste/image key name 覆盖、vim insert/normal/j/k/word/WORD/ge/gE/line-local ^/$/0/I/A/D/quote/bracket text-object/yank/register/paste/delete/count/replace/undo/find/till/repeat/dot-repeat/G/gg/toggle/join/open-line/indent/substitute 动作、normal-mode arrow/backspace/delete 映射和 operator linewise/字符范围、REPL screen、permission/task dialog builder、dialog kind/id routing/runtime/status line、runtime 到 REPL screen 的 dialog/status 同步、runtime-aware interaction script runner、prompt text/cursor/expanded/vim mode/register/task state/dialog result/runtime mutation/task bulk-cancel/permission cancel/keybinding mutation/status negative/snapshot negative/screen size/event-sequence/event-count/no-event/dialog-result-count/no-dialog-result 脚本断言、viewport 脚本断言、named-key 脚本输入、script JSON/JSONL/wrapper loader、script file runner 和 runtime/task camel field aliases、stale dialog race guard、cancel active、permission id/all cancellation、queued permission promotion、active task dialog refresh、task lifecycle/bulk-cancel 初版、idempotent alternate screen lifecycle/reset/reassert interactive 初版、mouse/focus/bracketed-paste terminal mode lifecycle/reconciliation、ANSI snapshot 基础、snapshot corpus write/compare/script-file compare/missing-baseline/diff/batch/strict unexpected-baseline 状态、scripted interaction runner/assertions/multi-key/text/paste/image/pasted-content metadata 初版、viewport/selection；完整 Ink/layout parity 和官方交互脚本仍缺。
- 本轮补充：keybinding config、keymap 解析和 interaction script named-key 输入接受 `ctrl-h`/`ctrl-i`/`ctrl-j`/`ctrl-m`、`ctrl-[`、`ctrl-?`、对应 `control-*` 以及 compact/camel 形式；terminal parser 支持 CSI-u/kitty keyboard protocol 的 ctrl/alt/shift-enter/shift-tab 序列；image hint parser 支持 OSC ST terminator 和 base64 `name=` filename；keybinding JSON loader 支持 wrapper object-map、`shortcuts`、object action 字段、string-array key sequence/chord 和 `null`/`false` unbind；mouse parser 支持 legacy X10/normal tracking 序列；interaction script 支持结构化 mouse/mouse_event 步骤，button 接受 `buttonMask`/`button_mask`/`btn`/`code`/`mask`，坐标接受 `mouseX`/`mouse_x`/`clientX`/`screenX` 和对应 Y/row/line 别名，release 接受 `mouseUp`/`isRelease`/`mouseRelease`/`releaseEvent` 等字段别名；interaction script 还支持字符串 `keys` 和 `input`/`input_text`/`keys_text`/`raw_key`/`paste_text` 字段别名，并允许 status/snapshot/viewport/pasted-content contains 断言使用单字符串或字符串数组，`keybindings`、`expectEvents`、`expectDialogResults`、`expectPrompt.pastedContents` 和 `expectTasks.contains` 使用单对象或对象数组。
- 本轮补充：terminal CSI-u/kitty keyboard parser 接受 `codepoint:alternate` 和 `modifier:event-type` 冒号字段，按主 codepoint/modifier 解析 ctrl/alt/shift/rune 键，覆盖 kitty progressive keyboard protocol 的常见变体。
- 本轮补充：terminal CSI-u/kitty keyboard parser 接受无 modifier 字段或 modifier `1` 的 base key 序列，覆盖 printable rune、Enter、Tab、Esc 和 Backspace，避免 extended-key 模式下普通键序列被解析成 unknown。
- 本轮补充：terminal CSI parser 把 DA/device attributes (`CSI c`、`CSI >c`、`CSI =c`) 归入 report action，并在 terminal parser dispatcher 中作为 `TerminalActionReport` 暴露。
- 本轮补充：terminal CSI parser 接受 `CSI a`/`CSI e`/`CSI \`` cursor alias final bytes，并映射到已有 cursor-forward/cursor-down/cursor-column actions。
- 本轮补充：terminal CSI parser 接受 DEC private mode `?1047h/l` alternate-screen buffer 和 `?1048h/l` save/restore cursor，复用已有 mode/cursor actions。
- 本轮补充：terminal CSI parser 把 DECREQTPARM terminal-parameters (`CSI x`) 归入 report action，保留 code/private marker。
- 本轮补充：terminal CSI parser 把 xterm window manipulation/report (`CSI t`) 归入 report action，覆盖常见 `CSI 14t`/`CSI 18t` 窗口/文本区尺寸查询。
- 本轮补充：terminal CSI parser 把 TBC tab-clear (`CSI g`/`CSI 3g`) 归入 cursor action，保留 clear-current/all code。
- 本轮补充：terminal CSI parser 把 REP repeat-preceding-character (`CSI b`) 归入 edit action，visible-text/snapshot pipeline 和 ANSI message wrapping/trim 会按重复次数展开前一个可重复 grapheme。
- 本轮补充：terminal CSI parser 把 DECSTR soft reset (`CSI !p`) 归入 reset action，并在 terminal parser 中清理 SGR/link 状态。
- 本轮补充：prompt history 写入按官方 `history.ts` 跳过 image pasted content，避免把 image base64/filename/mediaType 存入 `history.jsonl`，读取路径仍兼容旧 image metadata。
- 本轮补充：paste-cache 增加按 cutoff mtime 清理旧 `.txt` paste 文件的 best-effort 入口，忽略缺失目录、非 `.txt` 文件和单文件错误，和官方 `cleanupOldPastes` 语义对齐。
- 本轮补充：Buffered prompt history writer 支持撤销最近 pending entry，覆盖官方 `removeLastFromHistory` 在异步 flush 前直接从 pending buffer 移除的 fast path。
- 本轮补充：Buffered prompt history writer 支持撤销已 flush entry 的 slow path：最近 entry 已落盘时记录 timestamp，并让同一 writer 的 up-arrow/ctrl-r 历史读取按当前 session 跳过该 entry。
- 本轮补充：image-cache 增加 session-scoped 图片路径缓存、base64 图片落盘、批量 image 存储、内存路径查询和旧 session cache 清理，贴近官方 `imageStore.ts` 的存取/清理基础。
- 本轮补充：PromptInput/REPL screen 可显式启用 image-cache session，image hint paste 会在插入 `[Image #N]` 时同步缓存路径并写入图片文件，补齐官方 PromptInput 侧的 `cacheImagePath`/`storeImage` 接入点。
- 本轮补充：prompt submit event 保留 display 和 pasted-content metadata，session 层提供 `PromptMessages` 将 text paste 展开、image paste 转成 Anthropic image source block，并为已缓存图片追加 source-path meta message。
- 本轮补充：pasted image metadata 保留 `dimensions` 和 `sourcePath`，支持 source/dimension 字段别名，并按官方 `createImageMetadataText` 格式生成 source path、原始/显示尺寸和坐标换算倍率 meta text。
- 本轮补充：image hint parser 现在从 iTerm2 OSC File metadata 解析 `width`/`height`、original/display dimension 别名和 `sourcePath`/`source_path`/`path`，并把这些字段传入 prompt pasted image metadata。
- 本轮补充：PromptInput paste 按官方路径清理 ANSI/CR/tab，并用 `PASTE_THRESHOLD=800` 和 `min(rows-10, 2)` 行阈值决定正常高度短 paste 内联、小窗口/长 paste 折叠为 pasted-content ref。
- 本轮补充：PromptInput 编辑后会移除未被 `[Image #N]` 引用的 image pasted-content，session `PromptMessages` 也会在请求构造前过滤 orphan image，避免删除的图片继续作为 image block/meta 发送。
- 本轮补充：image paste pill 支持官方 lazy-space 语义，连续图片自动分隔，图片后直接输入非空白字符时补一个空格，显式空格/换行不会重复补空格。
- 本轮补充：REPL message metadata 保留 `imagePasteIds`，并从已有用户消息的 image ids 和 pasted refs 初始化/推进 `NextPastedID`，贴近官方 resume/continue 避免 paste ID 重用的逻辑。
- 本轮补充：reverse-search 现在按完整 `HistoryEntry` 匹配和选择历史，选中后恢复 text/image pasted-content metadata，后续 submit event 仍携带 display 与图片元数据。
- 本轮补充：prompt history pasted-content 读取接受 `mimeType`/`mime_type`/`contentType`、`fileName`/`file_name`/`name`、`filePath`/`file_path`/`path` 等 text/image metadata 别名，使历史恢复路径和 image hint/parser metadata 兼容面一致。
- 本轮补充：prompt history `LogEntry` 读取接受 `sessionID`/`session`/`sessionUuid`/`sessionUUID`/`session_uuid` 作为 session id 别名，current-session-first 历史排序不会因 session 字段拼写不同而失效。
- 本轮补充：REPL message restore 现在可从用户消息 content blocks、`imagePasteIds` 和 pasted-content metadata 重建 prompt text、`[Image #N]` 引用和 base64 image pasted contents。
- 本轮补充：Ctrl-S prompt stash 现在保存/恢复 prompt text、cursor 和 pasted-content metadata，空 prompt 下再次触发会恢复 stash，贴近官方 `chat:stash`。
- 本轮补充：remote history REST/link 风格分页接受 `links.next`/`links.previous`/`links.prev`/`links.older` 的字符串 URL 或 `{href,url,uri,link}` 对象，并从 `before_id`、`beforeId`、`cursor`、`pageCursor`、`previousCursor`、`prevCursor`、`beforeCursor`、`olderCursor`、`startCursor`、`endCursor` 等 query 参数提取续抓 before-id。
- 本轮补充：remote history HTTP `Link` header 分页接受 `rel="previous"`/`prev`/`older`/`next` URL，并从同一组 before/cursor query 参数提取续抓 before-id。
- 本轮补充：sidechain/subagent state loader 接受 `subagent_start`/`agent_start`/`task_start` 和 `sidechain_end`/`subagent_finish`/`agent_finish`/`task_summary` 等 subtype 别名，并归一化 `active`/`success`/`canceled`/`error` 等状态别名。
- 本轮补充：interaction script 的 `message` step 接受 `type`/`speaker` role 别名和 `content`/`body`/`message` text 别名；`image` step 接受 `fileName`/`file_name`/`name`、`mimeType`/`mime_type`/`contentType` 和 `data`/`base64` 内容别名；permission request step 接受 request/permission/tool-use ID、path、description 和 action 字段别名，并允许 `actions` 使用单字符串；`expectPrompt` 接受 `value`/`input`/`content`/`message`、`expandedText`/`fullText`、`cursorIndex`/`cursorPosition`、`isEmpty`/`blank` 等字段别名，且 `pastedContents` 断言接受 `pastedId`/`pastedContentId`、`kind`/`pastedType`、`value`/`data`/`base64`、`contentType`/`mimeType`、`fileName`/`name` 和 `contains` 等字段别名；`expectVim` 接受 `vimEnabled`/`isEnabled`、`vimMode`/`modeName`/`currentMode`、`vimRegister`/`registerValue`/`yankRegister`、`registerLinewise`/`linewise` 等字段别名；`expectTasks` 接受 `taskCount`/`total`/`size`/`length` 和 `statusCounts`/`countsByState` 等字段别名；`expectScreen`/`expectViewport` 接受 `columns`/`rows`、`screenWidth`/`screenHeight`、`scrollOffset`/`viewportOffset`、`visibleRows`/`lineCount` 等字段别名；`expectReverseSearch` 接受 `isActive`/`visible`/`open`、`search`/`term`/`pattern`、`cursorIndex`、`currentResult`、`matchCount`/`matches`、`noMatches` 等字段别名；`expectDialog` 可断言 body contains/not-contains、actions/action contains/not-contains、action count 和 focused action，runtime-aware scripts 会在步骤间保留 dialog focused action，且接受 `isActive`/`visible`、`dialogId`/`dialogID`、`dialogKind`、`heading`/`header`、`content`/`text`/`message` 等字段别名；`expectEvent`/`expectDialogResult` 接受 `eventType`/`event`/`name`、`payload`/`text`/`message`、`dialogId`/`dialogID`/`dialogKind`、`actionValue`/`resultStatus`/`exists`/`isStale` 等字段别名。
- 本轮补充：interaction script step 接受 `resize`/`terminalSize`/`screenSize` 对象或 `[width,height]` 数组、顶层 `columns`/`rows` resize 别名、`focus`/`focused`/`blur`/`focusIn`/`focusOut` focus event 别名、`snapshot`/`snapshotId`/`snapshotLabel` capture 名称别名，以及 runtime-aware mutation 别名如 `permission`/`permissionRequest`、`task`/`taskStatus`、`removeTask`/`deleteTask`、`cancelPermission`、`cancelTasks`/`cancelReason`、`openTasks`/`showTasks`。
- 本轮补充：interaction script step 可通过 `status`/`setStatus`/`statusLine`/`baseStatus` 设置状态行；runtime-aware scripts 会把它作为 base status，并继续叠加 permission/task 计数，方便复用带 status line 的 ANSI/interaction fixture。
- 本轮补充：interaction script 批量消息注入接受 `messages`、`append_messages`/`appendMessages`、`transcript_messages`/`transcriptMessages` 字段，且这些字段可用单对象或对象数组，复用 chat/transcript role/text 别名。
- 本轮补充：interaction script direct `dialog` step 接受 `dialogId`/`dialogKind`、`heading`/`header`、`content`/`text`/`message`、`options`/`choices`/`buttons`、`focusedIndex`/`selectedIndex` 等字段别名，且 actions/options 可用单字符串。
- 本轮补充：interaction script loader 接受 `scriptSteps`/`script_steps`、`interactionSteps`/`interaction_steps` wrapper 字段，并支持一层 `scenario`/`test`/`case`/`fixture`/`interaction` 嵌套对象。
- 本轮补充：ANSI snapshot corpus 比对支持 `.ansi` only baseline fallback，strict stale-baseline 检查同时覆盖 `.txt` 和 `.ansi`。
- 本轮补充：interaction script JSONL loader 单行上限提升到 50MiB，和 transcript/session 大记录读取容忍度对齐，覆盖大型 paste、image metadata 或 snapshot fixture 脚本行。
- 本轮补充：terminal lifecycle 增加可选 extended-key mode，按官方 `CSI >1u`/`CSI >4;2m` 启用 kitty keyboard protocol 和 modifyOtherKeys，退出时重置 modifyOtherKeys 并 pop kitty stack，reassert 时先 pop 再 push 以避免长期会话 stack 泄漏。
- 本轮补充：renderer/snapshot 增加 opt-in DEC 2026 synchronized output 包裹入口，可用官方 BSU/ESU (`CSI ?2026h`/`CSI ?2026l`) 生成整帧 ANSI fixture，同时默认渲染保持不变。
- 本轮补充：terminal OSC helper 增加 OSC 0 title/icon 序列生成，输入会先 strip ANSI；`StripANSI` 现在会完整跳过 OSC/DCS/APC/PM/SOS payload，避免 title/snapshot 可见文本被终端控制串污染。
- 本轮补充：terminal OSC helper 增加 OSC 21337 tab status 序列、清理序列和 tmux/screen passthrough 包裹，status 文本按官方规则转义分号和反斜杠。
- 本轮补充：terminal OSC helper 增加 OSC 8 hyperlink 开始/结束序列，按官方 rolling hash 为 URL 自动生成 `id=`，并允许显式 params 覆盖。
- 本轮补充：terminal OSC helper 增加 OSC 9;4 progress 序列，覆盖 clear/set/error/indeterminate，running/error 百分比按官方规则 clamp 到 0..100。
- 本轮补充：terminal OSC helper 增加 iTerm2、Kitty、Ghostty notification 序列和 raw BEL helper，调用方可按环境选择是否 wrap multiplexer。
- 本轮补充：terminal OSC helper 增加 OSC 52 clipboard 序列生成，固定 clipboard selection `c` 并按 UTF-8 base64 编码 payload；native clipboard/tmux buffer runtime 仍未接入。
- 本轮补充：terminal OSC helper 增加显式 ST (`ESC \\`) terminator 入口，可按官方 Kitty 避免 BEL 的路径生成 OSC 序列，同时默认 `OSCSequence` 仍保持 BEL terminator。
- 本轮补充：terminal OSC helper 增加 OSC color parser，支持 `#RRGGBB` 和 XParseColor 风格 `rgb:R/G/B`，按官方规则把 1-4 位 hex component 缩放到 8-bit RGB。
- 本轮补充：terminal OSC helper 增加 OSC 21337 tab-status payload parser，支持 `\;`/`\\` 转义、bare key 或空值清理、unknown key ignore，并复用 OSC color parser 解析 indicator/status-color。
- 本轮补充：terminal OSC helper 增加 OSC 8 hyperlink payload parser，按官方规则解析 params、保留 URL 内部分号，并把空 URL 识别为 hyperlink end。
- 本轮补充：terminal OSC helper 增加轻量 `ParseOSCContent`，覆盖官方 title(0/1/2)、OSC 8 hyperlink、OSC 21337 tab status 和 unknown action 分支。
- 本轮补充：terminal OSC helper 增加完整 OSC sequence parser，可从带 `ESC ]` 前缀且以 BEL 或 ST 终止的序列解析出 `ParseOSCContent` action。
- 本轮补充：terminal OSC parser 把 OSC 52 clipboard、iTerm2 progress/notification、Kitty notification 和 Ghostty notification 作为结构化 terminal actions 暴露，visible text/snapshot 继续正确剥离这些控制串。
- 本轮补充：terminal renderer constants 增加官方 clear scrollback (`CSI 3J`) 和 legacy Windows home (`CSI 0f`) 序列 helper，支持现代 clear-screen+scrollback 和 legacy Windows clear 组合；平台自动探测仍留给调用方。
- 本轮补充：terminal CSI helper 增加通用 `CSISequence`、cursor up/down/forward/back/position/move 和 line/screen erase 序列，按官方 helper 的零移动返回空串与 horizontal-first cursorMove 行为生成 ANSI。
- 本轮补充：terminal CSI helper 增加 scroll up/down、set scroll region 和 reset scroll region 序列，scroll 零值返回空串，便于后续补齐官方 viewport/scroll-region 输出路径。
- 本轮补充：terminal CSI helper 增加 DECSCUSR cursor-style 序列，覆盖 block/underline/bar 的 blinking 与 non-blinking code，并保留 unknown style 的默认 cursor fallback。
- 本轮补充：terminal CSI helper 增加 bracketed paste start/end 和 focus in/out 输入 marker 常量，并用现有 parser 验证 focus marker 映射，方便官方交互 fixture 复用原始 CSI marker。
- 本轮补充：terminal CSI helper 增加 `EraseLinesSequence(n)`，按官方 `eraseLines` 语义逐行 `CSI 2K`、行间上移并以 `CSI G` 回到列 1，`n<=0` 返回空串。
- 本轮补充：terminal CSI helper 增加官方 CSI param/intermediate/final byte range 常量和判定函数，为后续更完整 CSI parser/action tests 提供基础。
- 本轮补充：terminal CSI helper 增加官方 CSI final-byte/DEC mode 常量和 `ParseCSISequence` 动作解析，覆盖 cursor move/position/save/restore/show/hide/style、erase display/line/chars、scroll up/down/region、SGR params、alternate-screen/bracketed-paste/mouse/focus mode 和 unknown sequence fallback。
- 本轮补充：terminal CSI parser 补齐 insert/delete chars、insert/delete lines、forward tab/back tab action，`CSI M` 在 output parser 中按 delete-lines 处理，同时 input tokenizer 仍保留 X10 mouse payload 边界消费。
- 本轮补充：terminal CSI parser 现在把 DSR (`CSI n`) 解析成 report action，覆盖 device-status、cursor-position 和 private-mode unknown report，避免 terminal status query/response 序列继续落入 generic unknown。
- 本轮补充：terminal CSI parser 现在把 DEC `?1006h/l` SGR mouse mode 解析成 mouseTracking action，和 lifecycle 发出的 SGR mouse enable/disable 序列闭环。
- 本轮补充：terminal CSI parser 现在把 DEC `?2026h/l` synchronized output mode 解析成 mode action，和 renderer/snapshot 的 BSU/ESU 包裹路径闭环。
- 本轮补充：terminal ESC helper 增加官方 ESC final-byte 判定和 `ParseESCSequence`/`ParseESCContent`，覆盖 RIS reset、DECSC/DECRC save/restore、IND/RI/NEL cursor action、HTS/charset ignored 分支和 unknown sequence fallback。
- 本轮补充：terminal SGR helper 增加官方 `TextStyle` 状态解析基础，覆盖 reset、bold/dim/italic/underline/blink/inverse/hidden/strikethrough/overline、普通/亮色命名色、256 色、RGB 色、underline color、分号和冒号参数格式；完整 ANSI parser/render style 应用仍继续推进。
- 本轮补充：terminal sequence dispatcher 增加官方 `identifySequence` 等价分流，按 CSI/OSC/ESC/SS3/unknown 识别并委派现有 parser，SS3 按官方 output parser 作为 unknown action；streaming tokenizer 和 text grapheme action 仍继续推进。
- 本轮补充：terminal tokenizer 增加官方 streaming escape boundary 状态机，支持跨 chunk buffer/flush/reset、CSI/SS3/OSC/DCS/APC 序列边界、OSC BEL/ST terminator、ESC intermediate charset 序列、invalid CSI text fallback 和 opt-in X10 mouse payload 消费；完整 text grapheme action parser 仍继续推进。
- 本轮补充：terminal parser 增加轻量 ANSI action pipeline，串接 tokenizer、CSI/OSC/ESC dispatcher 和 SGR style state，输出 text/bell/cursor/erase/scroll/mode/title/link/tabStatus/reset/unknown action，文本宽度覆盖 ASCII、emoji 和 East Asian wide；完整 grapheme cluster segmentation 与 renderer style 应用仍继续推进。
- 本轮补充：terminal parser 跟踪 OSC 8 hyperlink start/end 状态，暴露当前 `inLink` 和 `linkUrl`，reset 时清空链接状态，贴近官方 parser 的 link range 状态语义。
- 本轮补充：terminal parser 的 text grapheme 基础分段补齐 combining mark、variation selector、emoji modifier、ZWJ emoji 序列和 regional indicator flag pair，并按官方多 codepoint grapheme 宽度为 2 的规则计算宽度；完整 Unicode UAX #29 分段仍未宣称完成。
- 本轮补充：terminal parser 的 text grapheme 分段继续补齐 emoji tag sequence，把 subdivision flag 这类 base emoji + tag chars + cancel tag 作为单个宽 grapheme，避免 wrap/pad/cursor pipeline 拆分视觉 emoji。
- 本轮补充：terminal CSI parser 现在对 tokenizer flush 出来的非 final-byte incomplete CSI 返回 unknown action，而不是丢弃，贴近官方 `parseCSI` 对 flushed partial sequence 的 fallback 行为。
- 本轮补充：terminal sequence dispatcher 对 tokenizer flush 出来的 OSC partial sequence 使用 `ParseOSCContent` fallback，允许无 BEL/ST terminator 的 title/link/tab-status content 按官方 parser 语义产出 action。
- 本轮补充：terminal tokenizer 增加明确的 output/input 构造器，output 路径默认不吞 `CSI M` 后续字节，input 路径默认开启 X10 mouse payload 边界消费，避免调用方误用布尔选项导致 output parser 吞文本或 stdin mouse payload 泄漏。
- 本轮补充：terminal tokenizer 补齐 PM (`ESC ^`) 和 SOS (`ESC X`) string-control 状态，和 OSC/DCS/APC 一样支持 BEL 或 ST terminator，避免这些控制串 payload 泄漏为 text token。
- 本轮补充：terminal sequence dispatcher/parser 现在把 DCS/APC/PM/SOS string-control 序列分类为 `stringControl` action，保留 payload、terminator 和 incomplete flush 状态，同时 visible text 继续忽略这些不可见控制串。
- 本轮补充：snapshot/OSC 复用 terminal parser 的 visible-text pipeline，`StripANSI` 不再维护独立手写 scanner；可见文本提取统一覆盖 CSI/OSC/DCS/APC/PM/SOS、flushed partial OSC 和 raw BEL 兼容行为，为后续 ANSI parser 与 renderer/snapshot parity 收口。
- 本轮补充：message renderer 增加 ANSI-aware wrapping/padding，带 SGR 的 message text 会通过 terminal parser 按 grapheme 可见宽度换行，并把 `TextStyle` action 重新渲染为 SGR 序列，避免 escape bytes 参与 layout 宽度计算；普通文本路径保持不变。
- 本轮补充：基础 wrap/pad/trim 改为按 terminal grapheme 可见宽度计算，普通 message、status/dialog/viewport/prompt 的 CJK/emoji 宽字符不再按单 rune 宽度参与布局，继续向 terminal column parity 收口。
- 本轮补充：prompt layout 的 chunking 和 cursor column 映射改为按 terminal grapheme 可见宽度计算，宽字符输入换行和 cursor CSI 定位不再按 rune index 误算列宽。
- 本轮补充：reverse-search footer 的 cursor CSI 定位改为按 query 光标前 terminal grapheme visible width 计算，宽字符历史搜索输入不再按 rune index 误算列宽。

### M8: Commands、skills、plugins

产出：

- slash/local/local-jsx 等价抽象。
- commands registry 和内置命令迁移。
- skills discovery/bundled/user/plugin/MCP skills。
- plugin manifest、marketplace、install/cache/update、plugin hooks/agents/MCP。

验收：

- `/help`、`/config`、`/mcp`、`/plugin`、`/skills`、`/memory`、`/resume` 等关键命令 golden。
- plugin 加载顺序、冲突、缓存、错误展示兼容。

### M9: MCP 完整平台

产出：

- stdio/SSE/HTTP/WebSocket/sdk/claudeai-proxy transport。
- server config 合并、policy allow/deny、OAuth、resources/prompts/tools。
- MCP tool result truncation/persist、elicitation、channel notifications。
- 内置工具 MCP server。

验收：

- 用 MCP fixture server 回放 list/call/resource/prompt/auth/session-expired。
- 与 settings/plugin/policy 组合测试。

### M10: Agents、tasks、worktree、remote

产出：

- AgentTool、built-in/custom agents、frontmatter MCP。
- local agent、async/background task、task output。
- worktree isolation、remote CCR agent、team/swarm/coordinator。
- SendMessage/TeamCreate/TeamDelete/Task*。

验收：

- subagent transcript、permission propagation、task progress、kill/resume、worktree cleanup 有 golden。

### M11: Bridge 和高级集成

产出：

- repl bridge、remote-control、session websocket、direct connect。
- LSP、IDE integration、Chrome native host、voice、computer-use、buddy、ultraplan。

验收：

- 每个 gated feature 独立开关测试。
- 不启用 feature 时二进制行为和可见 schema 不泄露 gated 工具/命令。

### M12: Parity hardening

产出：

- 全功能矩阵。
- 回归 golden corpus。
- 性能和资源上限测试。
- release packaging。

验收：

- CLI/TUI/SDK/MCP/session/tool/settings/plugin/agent 全部 golden 通过。
- 与官方 CLI 的黑盒差异清单清零或有明确版本差异说明。

## 5. 测试和验证方案

### Golden corpus

必须收集：

- CLI stdout/stderr/exit code。
- SDK JSON/NDJSON stream。
- API request body/header 红acted snapshot。
- Tool input/output/error。
- Session JSONL。
- TUI ANSI frames。
- Settings/MCP/plugin parse result。

### 测试层级

- Unit：schema、parser、permission matcher、message normalization。
- Integration：query loop + fake Anthropic server + fake tools。
- Fixture：Bash/File/MCP/plugin/settings/session golden。
- TUI：pty 脚本 + ANSI snapshot。
- Black-box：同一命令分别跑官方 CLI/Go CLI，比对稳定字段。
- Fuzz：shell parser、permission rules、settings parser、JSONL loader。

### 不可用真实 API 的处理

用 fake Anthropic streaming server 固定输出：

- text delta
- thinking/redacted thinking
- tool_use partial JSON
- tool_result continuation
- max_output_tokens
- rate limit
- prompt too long
- retryable 5xx/529

真实 API 只用于少量手工 smoke，不作为确定性 CI。

## 6. 关键实现决策

### TUI

早期可以用简化 renderer 提升速度，但最终若要 100% 还原，需要 Go 版 terminal renderer，至少覆盖：

- flex/yoga-like layout
- text wrap/width/ANSI
- alternate screen
- keyboard/mouse/focus events
- selection
- scroll viewport
- raw ANSI blocks
- stable render diff

### Schema

TypeScript 用 Zod。Go 侧建议：

- 合约结构体 + JSON tags 作为主源。
- 生成 JSON Schema 给 Anthropic/MCP/SDK。
- 对 settings/MCP/config 保留 unknown fields。
- 对 tool input 使用 typed struct + raw JSON fallback，避免 schema 漂移。

### Concurrency

用 context + errgroup/semaphore：

- concurrency-safe tool 可并发。
- 非安全工具独占。
- sibling error 可取消并发 sibling。
- user interrupt 根据 tool interrupt behavior 决定 cancel/block。
- tool progress 独立 channel，结果按 tool_use 顺序提交。

### Session compatibility

不能改变 JSONL 语义：

- progress 不参与 parent chain。
- legacy progress entry 要桥接。
- 大 transcript 不能整文件读写。
- subagent sidechain 路径和 metadata 要兼容。

### Feature gates

建立统一 feature registry：

- build tag：完全裁剪内部功能。
- runtime flag：保留但默认关闭。
- environment override：兼容 `CLAUDE_CODE_*`。
- schema visibility：关闭时工具/命令/schema 不可见。

## 7. 风险清单

| 风险 | 影响 | 处理 |
| --- | --- | --- |
| 源快照缺失核心类型/模块 | 无法仅凭源码证明 100% | M0 建缺口表，官方 CLI golden 反推 |
| TUI 自研 Ink 行为复杂 | 视觉/交互偏差大 | 单独建 renderer parity，不依赖普通 CLI UI 库作为最终实现 |
| Bash/PowerShell parser 复杂 | 权限绕过或误拒绝 | parser fixture + fuzz + destructive/read-only golden |
| API beta/header/cache 行为易漂移 | 模型行为差异 | request snapshot + fake server + real smoke |
| settings/plugin/MCP 合并优先级复杂 | 企业/项目配置不兼容 | schema/merge table tests |
| session JSONL 大文件和旧格式 | resume 失败或 OOM | head/tail loader、legacy bridge、size guard |
| feature-gated internal code多 | schema 泄漏或功能缺失 | feature matrix + build/runtime 双测试 |
| 法律/授权边界 | 项目可发布性风险 | 只做行为兼容和独立实现，不复制源码文本；确认授权范围 |

## 8. 当前状态

已完成：

- 扫描源目录规模、入口、核心模块、工具/命令体系。
- 建立 TypeScript 到 Go 模块映射。
- 识别快照缺口和高风险契约。
- 保存本地计划文档。

未完成：

- 尚未初始化 Go module。
- 尚未恢复缺失 TS 文件。
- 尚未建立官方 CLI black-box golden corpus。
- 尚未开始实现 Go 代码。

## 9. 建议的下一步

1. 初始化 Go 工程和 CI：`go mod init`、`cmd/claude`、`internal/contracts`、`test/parity`。
2. 写一个 `tools/sourceaudit` 小工具，把 import 缺口、目录统计、feature gate 自动导出为 JSON，避免扫描结果手工失真。
3. 建立第一批 golden：`--version`、`--help`、headless 简单 prompt、Read/Edit/Write/Bash、settings parse、session JSONL。
4. 先实现 contracts/messages/session/settings/tool，再写 query loop。
5. 每实现一个模块，都用 golden 标记 parity 状态，不允许“看起来能跑”直接算完成。
