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

- 已落地 JSONL/session 基础、resume/search/title 支撑、prompt history lock/buffered flush/field aliases、official subagent transcript layout、agent metadata sidecar/field aliases、sidechain runtime start/append/finish/cancel/fail 和 parent-chain append/finish 初版、sidechain manager orchestration 初版、sidechain manifest 聚合、sidechain state/list/resume/content-field aliases 初版、sidechain resume context builder、sidechain conversation/content-replacement reconstruction、transcript tail/window/metadata/index loaders、byte-budget transcript tail loader、agent-scoped content replacement metadata/record field-alias loading、session-scoped metadata reappend including AI-title/last-prompt/task-summary、transcript line offset index/window/byte-budget-window/parent-chain/resume/tail/byte-budget-tail loaders、extended transcript metadata entries/type/field aliases、transcript message/session UUID field aliases including top-level `messageUuid`/`messageId`/`id` record IDs and `role`/`entry_type`/`messageType`/`createdAt` timestamp aliases、transcript tombstone metadata delete/relink、transcript resume conversation builder、index 文本预览和 AI-title/last-prompt/task metadata 字段、流式 transcript 搜索、session list pagination、remote history token refresh、remote history 全量分页抓取/page-field/event-list/records/entries/eventList/sessionEvents/last-id/cursor/event-id/has-next alias/wrapped-data/links/paging/bare-array/connection/eventConnection/sessionEventsConnection wrapper response/edge-cursor fallback/max-pages 截断状态/before_id 续抓、remote event transcript materialization/fallback field fill/去重追加/duplicate parent guard、remote history 一步 sync 到 transcript、CLAUDE.md/memdir 扫描、team-memory secret guard、compact runner/boundary plan、conversation auto-compact、失败熔断、microcompact/cache 初版、persistent cached microcompact 初版、cache digest structural/rich-content metadata 覆盖、cache version/TTL/prune、in-memory micro cache prune、memory-cache write-through 到磁盘、atomic cache write、坏缓存默认 fail-open、session memory summary/frontmatter aliases/recall 初版、model-ranked recall session-id selection 和 invalid-selection fallback 初版、recall agent alternate/camel response keys/fenced-prose JSON extraction/scalar id parsing/nested/wrapped/collection-alias selection parsing、resume context model-assisted recall 接入、session memory rollup/prune compaction、rollup archive exclusion/merge、rune-safe rollup truncation、resume context + session memory recall、conversation recall 注入、deterministic/model-backed memory extraction 初版、extraction agent fenced-prose JSON/wrapped facts/provider-style response wrapper/alternate/structured field/nested source object/nested response/fact kind alias parsing 和 turn-end memory extraction 落盘。
- 本轮补充：sidechain agent metadata sidecar 读取接受 `type`/`subagentType`/`agentName`/`name`、`workspaceRoot`/`workspacePath`/`workspace`/`path`/`directory`、`taskDescription`/`operationName`/`commandName`/`displayTitle`/`prompt`/`input`/`command`/`title` 等字段别名，支持 JSON:API/resource-style wrapper，并避免把 `resource.type` 误当成 agent 类型，避免历史或第三方 subagent sidecar 在 resume/list 时丢失 agent 类型、worktree 路径或任务描述。
- 本轮补充：sidechain/subagent lifecycle content 读取会递归解包 `payload`/`data`/`body`/`result`/`response`/`metadata` 等 wrapper，嵌套的 subagent ID、status/outcome、summary、agent type、workspace path 和 task description 都可参与 state/list/resume 恢复。
- 本轮补充：sidechain/subagent lifecycle content 读取现在也接受 JSON:API/resource-style 的 `resource`/`attributes`/`properties` wrapper；外层 resource `id` 可作为 sidechain ID fallback，内层 metadata、status/outcome 和 summary 仍会参与 state/list/resume 恢复。
- 本轮补充：sidechain/subagent lifecycle content 读取现在也递归解包 GraphQL/JSON:API 风格的 `edge`/`node`/`attrs` wrapper，wrapped start/summary event 可继续恢复 ID、status、summary 和 agent metadata。
- 本轮补充：sidechain/subagent lifecycle 字段提取现在也会穿透 `edges`/`nodes`/`included` 等 collection wrapper 和数组元素，GraphQL connection 或 JSON:API included 风格的 start/summary payload 可继续恢复 state/list/resume 所需字段。
- 本轮补充：sidechain/subagent lifecycle 的 ID 等 string-like 字段接受 JSON number 和 Go 数字标量，numeric subagent ID 会保留为字符串并可用于 state/list/resume 查找。
- 本轮补充：sidechain/subagent lifecycle subtype 现在接受 `subagent_started`、`agentStarted`、`task_failed`、`sidechainCompleted` 等相邻事件名，并从 `taskID`/`workerId`/`runId`、`agentName`/`kind`、`resultText`/`finalMessage` 等字段恢复 state/list/resume 信息；`*_failed`/`*_cancelled` subtype 无显式 status 时会归一到 failed/cancelled。
- 本轮补充：sidechain/subagent lifecycle status 归一化现在接受 compact/camel aliases，包括 `inProgress`、`completedSuccessfully`、`cancelledByUser`/`canceledByUser`、`failedError`/`failedWithError` 和 `timedOut`，并继续写回 canonical running/completed/cancelled/failed 状态。
- 本轮补充：transcript metadata loader 为 file-history snapshot 和 attribution snapshot 建立 `messageId` 索引，并接受 `message_id`/`messageUuid`/`id` 等字段别名，保留 raw list 的同时支持按消息恢复 snapshot。
- 本轮补充：transcript message/index/session list 读取 `gitBranch`，兼容 `git_branch`/`branch` 别名，并让 session search 可以按分支名命中，贴近官方 lite metadata 的 branch 展示和检索行为。
- 本轮补充：full transcript `TitleFromTranscript` 的标题优先级和 indexed/lite 路径对齐，按 custom title、AI title、首个用户 prompt、last-prompt、summary 顺序兜底。
- 本轮补充：lightweight transcript index 的 `content-replacement` 计数按请求的 session id 过滤，避免 session list/search 摘要被同文件其它 session 污染。
- 本轮补充：transcript message/index/session list 读取 `cwd` 作为 project path，兼容 `projectPath`/`project_path`/`workingDirectory`/`working_directory` 等别名，并让 session search 可以按项目路径命中，贴近官方 lite metadata 的 projectPath 恢复行为。
- 本轮补充：TranscriptMessage 结构化读取官方 SerializedMessage 元数据 `userType`、`entrypoint`、`version`、`slug`，并兼容 user/entrypoint/version/slug 的 snake/camel/旧字段别名，减少旧 transcript 只能靠 raw JSON 保留元数据的情况。
- 本轮补充：model-backed session memory recall prompt 现在显式写入 requested limit 和 excluded current session id，减少模型返回超量或当前 session 后再 fallback 的概率。
- 本轮补充：remote history connection/pageInfo 解析接受 `hasPrevious`/`hasPreviousPage`、`hasOlder`/`more` 继续分页标记，以及 `previousCursor`/`prevCursor`/`beforeCursor`/`olderCursor` before-id cursor 别名，覆盖 GraphQL 向更旧事件翻页的响应形态。
- 本轮补充：remote history pagination bool 字段除 JSON bool 和 `true`/`false` 字符串外，也接受 `1`/`0`、`yes`/`no`、`on`/`off` 等数值/字符串布尔形态，避免 wrapper/pageInfo 中的非严格布尔值中断分页。
- 本轮补充：remote history pagination cursor/id 字段现在接受 JSON number 并原样转成字符串，覆盖 `next_cursor` 等 page 字段和 `edges[].cursor` 的数字形态。
- 本轮补充：remote history pagination 现在接受 `nextPageToken`/`nextToken`/`pageToken`/`continuationToken` 及 snake_case 形式，响应字段和 link URL query 参数都会归一到续抓 before-id。
- 本轮补充：remote history pagination 现在也接受 `previousPageToken`/`prevPageToken`/`olderPageToken`、`previousToken`/`prevToken`/`olderToken` 及 snake_case 形式，响应字段、link object 和 link URL query 参数都会归一到续抓 before-id。
- 本轮补充：remote history pagination 现在也接受相邻 before-cursor aliases，包括 `before`、`beforeID`、`olderThan`、`endingBefore` 和 `untilId`，响应字段、link object 和 link URL query 参数都会归一到续抓 before-id。
- 本轮补充：remote history pagination 现在也接受 OData next-link 字段 `@odata.nextLink`、`odata.nextLink` 和 `__next`，并从 `$skiptoken`/`skipToken` link query 参数提取续抓 cursor。
- 本轮补充：remote history 普通事件数组现在也接受 JSON:API/resource-style 元素，事件 payload 可放在 `attributes` 或 `properties` 里，并使用外层 resource `id` 作为 SDK event ID fallback。
- 本轮补充：remote history fetch 把 HTTP 204 和 200 空 body 视为空的终止页，避免空历史响应被标成 incomplete 或触发 JSON EOF。
- 本轮补充：remote history fetch 现在把 HTTP 404/410 missing/deleted session response 视为空的终止页；其它非 OK 响应仍保持 nil page/incomplete，避免把临时服务错误误报为完整空历史。
- 本轮补充：contract `ID` JSON 读取现在接受 JSON number/null，remote history event/message/session/parent ID alias 可继承数字 ID 兼容面并在 transcript materialization 中保留为字符串。
- 本轮补充：remote history response parser 会递归解包 `data.session.events`、`data.projectSession.eventConnection`、`data.viewer.session.events`、`data.node.eventConnection`、`conversation`、`remoteHistory`、`_embedded` 等 GraphQL/session/HAL wrapper，继续复用 `nodes`/`edges[].node` 和 `pageInfo` pagination 解析。
- 本轮补充：remote history event-list 接受 `value`/`values`/`resources`/`collection` 别名，connection edge 也接受 `resource`/`value` 作为 node payload，覆盖 OData/HAL/resource collection 风格响应。
- 本轮补充：remote history response parser 现在也递归解包页级 JSON:API/resource `attributes`/`properties` wrapper，event-list 接受 `list`/`object`/`objects` aliases，并能把单个 `data.attributes` resource event 作为一条 SDK event 恢复。
- 本轮补充：remote history response parser 现在也递归解包 JSON:API `relationships` wrapper，并接受 `children`、`resultsConnection`/`results_connection` 和 `childrenConnection`/`children_connection` 事件集合别名，relationship 内的 `pageInfo` pagination 仍可驱动续抓。
- 本轮补充：remote history 在 JSON:API `relationships.events.data` 只有 resource identifier 时，会继续使用 top-level `included` 中的真实事件 resource，避免把 `{type,id}` 标识符误当作空事件遮蔽完整 payload。
- 本轮补充：remote history 顶层事件数组现在会展开混合 JSON:API `data` 资源中的 session relationship event page，合并嵌套 pagination，并跳过 tool/task 等明确非事件资源，避免空 SDK event 污染历史。
- 本轮补充：remote history response parser 现在也接受 JSON:API `included` collection，会过滤非事件资源，并递归解包 `resource`/`attributes`/`properties` 后保留外层 resource id 作为事件 ID fallback。
- 本轮补充：remote history response parser 会解包 `payload`/`response`/`result`/`body` 等通用响应外壳，外壳内的 event list、pagination、links 会继续递归解析。
- 本轮补充：remote history response parser 现在也接受 provider-style `choices`/`outputs`/`candidates`/`generations` wrapper 以及顶层 `message`/`content`/`text` envelope，可从 `message.content`、content-block array 和 `content.parts[].text` 中恢复 event page JSON（包括 fenced `json` code block），并保留 pagination 继续驱动 `before_id` 续抓。
- 本轮补充：remote history pagination 现在接受 `starting_after`/`startingAfter`/`after*` cursor aliases，page 字段和 link URL query 都可驱动下一页 `before_id` 续抓。
- 本轮补充：remote history `SDKEvent` 本体接受 `eventType`/`event_type`/`role` 类型别名、`createdAt`/`created_at` 时间戳别名，以及 `payload`/`data`/`body`/`metadata`/`meta`/`attributes`/`properties`/`serializedMessage` message payload 别名；payload 只有 `role`/`content` 时也能 materialize 成 transcript message。
- 本轮补充：sidechain lifecycle 读取现在接受运行时相邻字段别名，包括 `jobId`/`threadId`/`workflowId`/`operationId`/`requestId`、`workerType`/`taskType`、`workspaceRoot`/`projectPath`、`instructions`/`operationName`/`commandName`/`displayTitle`、`jobStatus`、`resultState`、`outputText` 和毫秒级 start/end time 字段，并会递归解包 `runtime`/`context`/`state` 容器。
- 本轮补充：sidechain lifecycle start/end time 现在接受 `startTimestamp`/`startTimestampMs`/`startedAtUnix` 以及 `endTimestamp`/`completedTimestamp`/`completedAtUnixMs` 等相邻时间别名，恢复第三方/旧 runtime transcript 时不再只依赖 `startedAt`/`endedAt`。
- 本轮补充：sidechain lifecycle 和 metadata sidecar 的 task description/summary 读取现在接受 visible text payload，包括 text content block、message object、content block array、provider-style `message.content`/`parts`/`outputText` wrapper，并继续跳过 thinking/tool/image 等非可见块。
- 本轮补充：transcript resume 的嵌套 content block 接受 `toolUseId`/`toolUseID`、`isError`、`cacheControl`、`cacheReference` 字段别名，并保留 cache edit 的 `cacheReference`。
- 本轮补充：transcript resume 的 nested content block `id`/`tool_use_id`/`toolUseId` 现在接受 JSON number，并保留为字符串 tool-use ID。
- 本轮补充：嵌套 contract message 的 `content` 接受字符串、单个 content-block 对象，以及混合字符串/content-block 数组；字符串会归一为 text block，并接受 `text`/`body`/`message`/`value`/`output` 正文字段和 `role`/`messageType` 类型别名，提升 transcript/remote history payload 恢复率。
- 本轮补充：lightweight transcript metadata loader 在 `system`/`compact_boundary` 后清空旧 `marble-origami-commit`/`marble-origami-snapshot` 状态，和 full loader/官方 sessionStorage compact-boundary 语义一致。
- 本轮补充：transcript metadata loader 接受 `sessionID`/`session` 作为 session-scoped metadata ID 别名，并容忍 `prNumber`、`timeSavedMs`、`lastSpawnTokens` 等计数字段使用数字字符串。
- 本轮补充：transcript metadata ID helper 现在复用 contract `ID` JSON 解码，`messageID`/`sessionID` 等 metadata ID 字段可接受 JSON number 并保留为字符串。
- 本轮补充：context-collapse commit metadata 的 collapse/summary/archived ID 字段现在也走 metadata ID helper，支持 JSON number ID 并保持 full/lightweight loader 一致。
- 本轮补充：content-replacement metadata 的 `agentId`、record `toolUseId` 和 `blockId` 现在也接受 JSON number，并在 full/lightweight loader 中保留为字符串 ID。
- 本轮补充：context-collapse snapshot metadata 接受 `isArmed`/`enabled` bool 别名、`spawnTokens`/`tokenCount` 计数字段别名，以及 `stagedMessages`/`items` staged payload wrapper，full loader 和 metadata loader 保持一致。
- 本轮补充：transcript metadata 读取现在会先解包 JSON:API/resource、GraphQL edge/node、`included` 以及 collection/list/values wrapper，再做 metadata type 分类；full loader、lightweight metadata loader 和 transcript index 都可恢复 wrapped title/task/tag/worktree/content-replacement/context-collapse 条目。
- 本轮补充：transcript metadata type 现在接受 compact/camel aliases，例如 `aiTitle`、`lastPrompt`、`taskSummary`、`contentReplacement`、`fileHistorySnapshot`、`speculationAccept` 和 `contextCollapseSnapshot`；full loader、lightweight metadata loader 和 transcript index 使用同一归一化。
- 本轮补充：transcript message 和嵌套 contract message 接受顶层 `sessionID` 作为 session id 别名，`LoadTranscript`、`LoadTranscriptIndex` 和 indexed resume 会保留该 session id（覆盖测试：`TestLoadTranscriptAcceptsSessionIDUpperAlias`）。
- 本轮补充：嵌套 contract message 接受 `parentUUID`、`parentId`/`parentID`/`parent_id`、`parentMessageId`/`parentMessageID`/`parent_message_id` 和 parent-message UUID 别名，transcript/remote history payload 自带 parent alias 时不会丢失嵌套 parent。
- 本轮补充：indexed resume chain 现在区分 byte budget 截断掉的 parent 和 transcript 里真实缺失的 parent，bounded resume 会分别暴露 `TruncatedParent` 与 `MissingParent`。
- 本轮补充：contract message、session entry 和 transcript loader 现在会把 message-type aliases 归一化为 canonical 类型，包括 `assistant_message`、`userMessage`、`system-event`、`attachmentMessage`、`progress_update` 和 `tombstone_event`；full loader、line index 和 indexed resume 统一使用 canonical user/assistant/system/attachment，并保留 progress bridge 语义。
- 本轮补充：tail、byte-tail、window 和 metadata-only transcript loader 也改用 canonical message type 处理 progress bridge 与 compact-boundary，`progress_update` 和 `system-event` 等别名不再只在 full loader 路径生效。
- 本轮补充：tail、byte-tail、window 和 streaming transcript search 现在复用 full/index loader 的 wrapped record 展开逻辑，可从 JSON:API/resource、GraphQL edge/node 和 collection/list wrapper 中恢复 transcript 批次，并保留 progress bridge。
- 本轮补充：TUI Vim prompt editing 增加基础 visual/visual-line 模式，支持 `v`/`V` 进入选择、motion 扩展 selection、visual `o` 切换 active end、visual `<`/`>` 行缩进/反缩进、visual `~` 大小写切换、visual `u`/`U` 小写/大写转换、`y`/`d`/`c` 以及常用 visual `x`/`s` aliases 作用于选择范围、Esc 回到 normal，并让 interaction script 可用 `visual`/`visualLine` 断言当前 mode。
- 本轮补充：TUI Vim prompt editing 增加 prompt-local `H`/`M`/`L` screen-line motions，normal/visual/operator 路径都可用，`H`/`L` 支持 count 定位首/末附近行，operator line-motion 可 dot-repeat。
- 本轮补充：TUI Vim prompt editing 支持 normal-mode `gv` 重新进入上一次 characterwise/linewise visual selection，后续 visual operator 会复用恢复出的选择范围。
- 本轮补充：TUI Vim prompt editing 增加 `gu`/`gU`/`g~` case-conversion operator，复用 motion、linewise、find/till、text-object 和 dot-repeat operator 管线，并保持大小写转换不写入 yank register。
- 本轮补充：TUI Vim prompt editing 增加 normal-mode `gJ` raw line join，不插入/规范化空格，并接入 dot-repeat。
- 本轮补充：TUI Vim prompt editing 增加 visual/visual-line `J`/`gJ` 行拼接，支持选择范围内的 whitespace-normalized join 和 raw join，并沿用 undo、`gv` selection 记忆和 dot-repeat change 记录。
- 本轮补充：TUI Vim prompt editing 增加 visual/visual-line `p`/`P` 粘贴替换 selection，支持 characterwise 和 linewise register，替换出的文本会回写 unnamed register，并避免行选择替换到末尾时留下额外空行。
- 本轮补充：TUI Vim prompt editing 增加 visual/visual-line `r{char}` selection 替换，按选区把非换行字符替换为目标字符，保留行结构、接入 undo，并让 `gv` 能重选替换前的 visual range。
- 本轮补充：TUI Vim prompt editing 增加 normal-mode `R` replace mode，输入会从当前 cursor 开始覆盖现有字符、超过文本尾部时追加，并接入 undo 与 dot-repeat。
- 本轮补充：TUI Vim prompt editing 增加 prompt-local marks，支持 `m{mark}` 设置位置、`` `{mark}` 精确跳转、`'{mark}` 跳到 mark 所在行首，并支持 `d`/`c`/`y` 等 operator 以 mark 作为 motion。
- 本轮补充：TUI Vim prompt editing 增加基础 macro 录制和回放，支持 `q{reg}` 开始录制、normal-mode `q` 停止、`@{reg}` 按 count 回放，以及 `@@` 重放上一 macro。
- 本轮补充：TUI Vim prompt editing 增加 prompt-local `/` 和 `?` 搜索模式，支持 Enter 执行、Esc 取消、Backspace 编辑查询、wraparound 匹配，以及 `n`/`N` 重复上一搜索方向或反向搜索。
- 本轮补充：TUI Vim prompt editing 将 `/` 和 `?` 搜索接入 operator motion，支持 `d/search`、`c?search`、搜索 count、取消清理 pending 状态，以及 search operator 的 dot-repeat 记录。
- 本轮补充：TUI Vim prompt editing 将 `/`、`?`、`n` 和 `N` 接入 visual/visual-line 模式，搜索会临时进入 search prompt、Enter 后恢复 selection 并移动 active end，Esc 取消时保留原 visual selection。
- 本轮补充：TUI Vim prompt editing 增加 named register 前缀，支持 normal/operator/visual 路径里的 `"{reg}` yank/delete/paste、uppercase register append、black-hole register no-op，以及普通移动命令后清理未使用的 register selection。
- 本轮补充：TUI Vim prompt editing 的 normal-mode `x`/`X` 现在会把删除字符写入 unnamed 或 selected named register，并保持 `.` dot-repeat 删除路径继续可用。
- 本轮补充：TUI Vim prompt editing 现在支持 visual/visual-line `Y`/`D`/`C` linewise aliases，字符 visual 选区也会按所在整行 yank/delete/change，并保持 unnamed/named register 的 linewise 内容一致。
- 本轮补充：prompt history 写入现在保留 image pasted-content 的 media type、filename、dimensions 和 image-cache source path 元数据，同时继续不把 inline base64 image bytes 或 text-paste hash 写进图片历史记录。
- 本轮补充：prompt history 读取旧 image pasted-content 记录时，如果缺少 source path 但对应 session 的 image-cache 文件仍存在，会自动补回 source path 并刷新内存 image path cache。
- 本轮补充：interaction script key 字段现在接受 DOM-style key event object，可从 `key`/`code`（包括 `Numpad*`、扩展数字区括号/hash/backspace 和标点 key code）、旧式 `keyIdentifier`、数字 `keyCode`/`which`/`charCode`（包括标点和数字区运算符）、`keypress.which` 字符码、`ctrlKey`/`altKey`/`metaKey`/`shiftKey` 和 `modifiers` 数组还原现有 key 名，wrapper payload 中的 key event 也可驱动脚本回放。
- 本轮补充：interaction script mouse payload 现在可从 `mouseup`、`pointerUp`、`touchend` 等 event type 推导 release 状态；显式 release bool 仍优先生效。
- 本轮补充：interaction script mouse payload 现在可从 `wheel`/`mousewheel`、`scrollUp`/`scrollDown`、`direction`、`deltaY` 和旧式 `wheelDelta` 推导 SGR wheel button，录制的 DOM/compact 滚轮事件可直接驱动 viewport 滚动。
- 本轮补充：interaction script mouse payload 现在接受 DOM `which` 和 `buttons`/`buttonState` bitmask，并映射到 SGR left/middle/right button，避免录制脚本的右键/中键被误当成 primary click。
- 本轮补充：interaction script mouse payload 现在会把 DOM `mousemove`/`pointermove` 的 buttonless motion 映射成 SGR `35`，把带 `buttons`/`which` 的 move/drag 映射成 SGR motion button，避免 hover/move 录制事件误触发 dialog/viewport primary click。
- 本轮补充：interaction script touch payload 现在可从 `touches`/`targetTouches`/`changedTouches` 的首个 touch point 恢复坐标，`touchmove` 映射为 SGR drag motion，`touchcancel` 映射为 release，减少 DOM touch 录制 fixture 的手工改写。
- 本轮补充：REPL dialog 鼠标处理现在忽略 SGR motion/drag button，只响应实际 press/click，避免 pointer/touch move 回放关闭 permission/task dialog。
- 本轮补充：interaction script paste payload 现在接受 DOM `clipboardData`/`dataTransfer` 对象，可从 `text/plain`、`plainText` 和 `items[].text` 恢复 pasted text，减少 ClipboardEvent 录制 fixture 的手工改写。
- 本轮补充：interaction script paste payload 现在也可从 DOM `clipboardData.items` 与 `dataTransfer.files` 中恢复 `image/*` file item 为 image paste，并避免把 image file 的 `data`/`base64` 内容误当成普通 pasted text。
- 本轮补充：interaction script clipboard/dataTransfer image paste 现在优先读取 `items[].file`、`items[].blob`、`items[].getAsFile` 等嵌套 file payload，保留真实 filename、media type、base64 和 source path。
- 本轮补充：interaction script resize payload 现在接受 DOM/window 尺寸别名 `innerWidth`/`innerHeight`、`clientWidth`/`clientHeight`、`offsetWidth`/`offsetHeight` 和 ResizeObserver 风格 `contentRect`/`target` wrapper。
- 本轮补充：interaction script resize payload 现在接受 ResizeObserver `contentBoxSize`/`borderBoxSize` 数组里的 `inlineSize`/`blockSize` 字段，覆盖现代浏览器 box-size 事件形态。
- 本轮补充：嵌套 contract message 接受 `messageId`/`messageID`/`message_id` 和 `messageUuid`/`messageUUID`/`message_uuid` 作为自身 ID/UUID 别名，indexed resume 会保留 payload 自带的 nested message id。
- 本轮补充：嵌套 contract message 的 primary `id` 现在接受 JSON number，`LoadTranscript` 和 indexed resume 会保留为字符串 message id。
- 本轮补充：基础 `SessionEntry` JSONL loader 接受 `role`/`entryType`/`messageType`、message ID/UUID、parent ID/UUID 和 `sessionID`/`session`/session UUID 别名，旧 entry 文件可通过 `session.Load` 保留类型、parent 和 session。
- 本轮补充：tombstone metadata target/parent 加宽到 `targetId`/`deletedId`/`messageId` 和 `parentId`/`parentMessageId` ID/UUID 别名，delete/relink replay 可兼容旧字段拼写。
- 本轮补充：transcript metadata 加宽 summary `leafID`/message ID、content-replacement `agentID`/`toolUseID`/`blockID` 和 context-collapse `collapseID`/`summaryID`/archived ID 别名，full loader 和 lightweight metadata loader 保持一致。
- 本轮补充：worktree-state metadata 接受 `worktreeState`/`worktree_state`/`worktree`/`workspace` wrapper 别名，full loader 和 lightweight metadata loader 都能保留旧 worktree payload。
- 本轮补充：PR link metadata 接受 `pullRequestNumber`/`pull_request_number`、`pullRequestURL`/`pull_request_url` 和 `repoFullName`/`repositoryFullName` 别名，full loader 和 lightweight metadata loader 都能恢复旧 PR metadata。
- 本轮补充：task-summary metadata 接受 `taskSummary`/`task_summary`/`content`/`text` 摘要别名和 `createdAt`/`created_at` timestamp 别名，旧任务摘要记录不会只保留 session id。
- 本轮补充：summary/custom-title/ai-title/last-prompt metadata 接受 `content`/`text`/`title`/`name`/`prompt` 等值字段别名，full loader、metadata loader 和 transcript index 的标题/摘要恢复保持一致。
- 本轮补充：tag、agent-name、agent-color、agent-setting 和 mode metadata 接受 `label`/`name`/`color`/`setting`/`status` 等值字段别名，full loader、metadata loader 和 transcript index 的 agent/session 状态恢复保持一致。
- 本轮补充：content-replacement metadata 接受 `records`/`contentReplacements` 等 record wrapper，以及 record 内 `type`/`content`/`hash` 等字段别名，full loader、metadata loader 和 transcript index 的 replacement 恢复保持一致。
- 本轮补充：remote history `SDKEvent` 解码接受顶层 `sessionID` 作为事件 session id 别名，materialize transcript 时会填充 record 与嵌套 message 的 session id（覆盖测试：`TestRemoteHistoryTranscriptMessagesAcceptsSessionIDUpperAlias`）。
- 本轮补充：remote history `SDKEvent` 解码接受 `parentUUID`、`parentId`/`parentID`/`parent_id`、`parentMessageId`/`parentMessageID`/`parent_message_id` parent aliases，materialize transcript 时会保留 parent chain（覆盖测试：`TestRemoteHistoryTranscriptMessagesAcceptsParentIDAliases`）。
- 本轮补充：remote history `SDKEvent` 解码接受 `eventID`、`messageId`/`messageID`/`message_id` 和 `messageUuid`/`messageUUID`/`message_uuid` 作为事件 ID aliases；只有 `uuid` 时也会作为 event ID fallback，materialize transcript 时能稳定生成 message UUID 与 parent chain（覆盖测试：`TestRemoteHistoryTranscriptMessagesAcceptsEventMessageIDAliases`）。
- 本轮补充：remote history `SDKEvent` timestamp 解码接受 `created`/`createdTime`/`created_time`、`date_time`、`eventTime`/`event_time`、`occurredAt`/`occurred_at` 等相邻字段，并容忍 `timestamp`/`createdAt` 等时间字段为 JSON number，materialize transcript 时继续填充 record 与嵌套 message timestamp（覆盖测试：`TestSDKEventUnmarshalAcceptsTimestampAliases`、`TestRemoteHistoryTranscriptMessagesAcceptsEventTimestampAliases`）。
- 本轮补充：remote history `SDKEvent.message` 现在接受顶层字符串和 content-block 数组形态，会先包装成 message content 再解码，避免 provider 把 assistant/user event message 直接写成 `"message":"text"` 或 `message:[...]` 时 JSON 解码失败（覆盖测试：`TestSDKEventUnmarshalAcceptsScalarMessagePayload`、`TestRemoteHistoryTranscriptMessagesAcceptsScalarEventMessagePayload`）。
- 本轮补充：remote history `SDKEvent` 的 status/error/result 内容字段现在接受 `statusMessage`/`progress_message`、`errorMessage`/`failure_reason`、`resultText`/`outputText`/`completionText` 以及 result object aliases `output`/`response`/`value`/`completion`，仅在对应 canonical event type 下补值，避免和 assistant/user message payload 混淆（覆盖测试：`TestSDKEventUnmarshalAcceptsStatusErrorResultAliases`、`TestRemoteHistoryEventsAcceptStatusErrorResultAliases`）。
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
- 本轮补充：session-memory recall agent 和 relevant-memory selector 现在递归解包 `data`/`payload`/`body`、JSON:API `resource`/`attributes`/`properties`/`attrs`、`included`，以及 GraphQL `viewer`/`edge`/`node`/`nodes`/`edges`、`collection`/`list`/`children`/`values` selection wrapper；带明确非 memory/session `type` 的 resource 不再用裸 `id` 污染选择顺序，API-shaped model response 中的 session IDs 和 memory paths 会按模型顺序保留。
- 本轮补充：session-memory recall agent 和 relevant-memory selector 现在也接受 provider-style `choices`/`outputs`/`candidates`/`generations` response wrapper 以及顶层 `message`/`content`/`text` envelope，可从 `message.content`、content-block array、`content.parts[].text` 和 fenced `json` code block 里递归恢复 JSON selection payload。
- 本轮补充：session-memory recall agent 和 relevant-memory selector 的 query 解析现在接受 `user_query`/`userQuery`、`question`、`prompt`、`input`、`search`、`search_text`/`searchText` 等相邻别名，模型返回非 canonical query key 时仍能保留改写后的检索语义。
- 本轮补充：relevant-memory selector 现在接受 `uri`/`url`/`href`、`fileUri`/`fileUrl` 等 memory path aliases，并能从 `file://` URI 或 API URL path basename 恢复候选 memory；顶层 `memories`/`matches`/`filesList` 集合在同时带 `query` 时也会参与 selection 解析，避免 query 快路径丢掉模型选择。
- 本轮补充：session-memory recall agent 现在接受 `summaries`、`selectedSummaries`、`relevantSummaries`、`candidateSummaries` 等 summary collection aliases，并会继续从嵌套 `summary.sessionId`/`summaryId` 恢复模型排序的 session IDs。
- 本轮补充：session-memory recall agent 现在也接受 `sessionUri`/`sessionUrl`、`uri`/`url`/`href` 等 summary link aliases，能从 `file://.../summary.md` 或 API URL path 中恢复 session ID 并按模型顺序匹配 summary。
- 本轮补充：model-backed memory fact extraction 的正文解析现在接受 `fact`、`statement`、`insight`、`result`、`output` 等相邻 text aliases，模型不用 canonical `text`/`content`/`summary` 字段时也能恢复事实内容。
- 本轮补充：session-memory summary frontmatter 的 `updatedAt`/`createdAt` 时间现在接受 Unix 秒、Unix 毫秒和 `updatedAtMs`/`timestampMs` 等相邻字段别名，recall 排序不再只依赖 RFC3339 字符串。
- 本轮补充：session-memory summary frontmatter 的 session/message ID 现在接受 `sessionUUID`、`conversationId`、`threadId`、`transcriptId`、`messageID` 和 `leafID` 等相邻别名。
- 本轮补充：session-memory summary 正文现在在 markdown body 为空时接受 frontmatter `summaryText`、`summary`、`content`、`text`、`resultSummary`、`finalSummary` 等字段兜底，同时保持 body 优先。
- 本轮补充：model-backed memory fact extraction 现在也接受 provider-style `choices`/`outputs`/`candidates`/`generations` response wrapper 以及顶层 `message`/`content`/`text` envelope，可从 `message.content`、content-block array、`content.parts[].text` 和 fenced `json` code block 里递归恢复 JSON facts payload。
- 本轮补充：model-backed memory fact extraction 现在接受 `sourceMessageUUID`/`source_message_uuid`、`sourceEventId`/`source_event_id`、`originId` 和 `turn`/`event` source object 等来源别名，并把 numeric source ID 保留为字符串。
- 本轮补充：compact runner 的 summary 响应现在也接受 provider-style `choices`/`outputs`/`candidates`/`generations` wrapper，可在构建 compact plan 前从 `message.content`、content-block array、`content.parts[].text` 和 fenced `json` code block 中恢复 visible summary text。
- 本轮补充：remote history `SDKEvent` type 现在会把 provider-style aliases 归一化为现有 canonical 事件类型，包括 `assistant_message`/`assistant_delta`、`userMessage`/`humanMessage`、`system-event`、`result_event`/`finalResult`/`response.completed`、`errorEvent`/`failureEvent` 和 `status_update`/`statusMessage`/`progress`，single-object page 与 transcript materialization 不再因事件类型拼写相邻而丢消息。
- 本轮补充：conversation runner 会在用户消息入队后基于 compact window 计算 token warning state，并在达到 warning/error/auto-compact/blocking 阈值时发出 `token_warning` event；warning state 接入 blocking-limit env override，auto-compact 判断接入既有 `CLAUDE_AUTOCOMPACT_PCT_OVERRIDE`，避免 warning 和 compact 阈值来源分叉。
- 本轮补充：microcompact disk cache loader 现在读取 Go 默认、camelCase 和 snake_case 字段别名，并容忍计数字段的数字字符串，以及 RFC3339、Unix 秒、Unix 毫秒时间字段，提升 cached microcompact 文件在不同实现/版本间的恢复率。
- 本轮补充：microcompact disk cache loader 现在接受 `cacheEntry`/`cache_entry`、`micro_compact`/`micro_compact_result` wrapper，以及 `summaryMarkdown`/`resultSummary`/`compressedText`、`cacheDigest`/`digestHash`/`fingerprint`、`summarizedCount`/`retainedCount`、`formatVersion` 和 `ttlMilliseconds`/`expiresInMilliseconds`/`maxAgeMilliseconds` 等相邻实现字段别名。
- 本轮补充：microcompact disk cache loader 继续接受相邻 timestamp/expiry aliases，包括 `cachedAt`、`cacheCreatedAt`、`storedAt`、`generatedAt`、`updatedAt`、`timestamp`、`expiry`、`expirationTime`、`validUntil`、`notAfter`，以及 `timeToLiveSeconds`、`validForMs` 等相对 TTL 字段。
- 本轮补充：microcompact disk cache loader 的相对 TTL 现在也接受分钟、小时和天级别名，例如 `ttlMinutes`、`expiresInHours`、`validForDays` 及 snake/camel 相邻形式，恢复 cached microcompact 过期时间时不再局限于秒/毫秒。
- 本轮补充：microcompact disk cache loader 的相对 TTL 字符串现在也接受固定单位 ISO-8601 duration，例如 `PT1H30M`、`P1D`、`P1DT2H`，并继续拒绝年/月这类不定长 duration。
- 本轮补充：microcompact disk cache loader 和 prune 现在接受 digest 缺失但文件名已 keyed 的 cache entry，会用 `<digest>.json` 文件名作为 digest fallback，同时仍保留显式 digest mismatch 的 invalid-cache guard。
- 本轮补充：microcompact disk cache loader 的 `cached`/`fromCache`/`cacheHit`/`isCached` 布尔字段现在接受 JSON bool、`true`/`false`、`yes`/`no`、`on`/`off` 和 `1`/`0` 数字/字符串形态。
- 本轮补充：microcompact disk cache loader 现在会从 `metadata`/`meta`/`cacheInfo`/`cacheDetails`/`cacheEntry`/`entry`/`record`/`cache` 等 sidecar object 中补缺失的 digest、version、cache-hit、timestamp、TTL 和 count aliases；主 summary payload 字段仍保持优先。
- 本轮补充：microcompact disk cache loader 现在也接受 JSON:API/resource-style `resource`/`attributes`/`properties` wrapper，summary payload 可放在 attributes/properties 内，外层 resource `id` 可作为 digest fallback。
- 本轮补充：microcompact disk cache loader 现在也递归解包 GraphQL-style `viewer`/`edge`/`node`/`attrs` wrapper，node `id` 可作为 digest fallback，attrs/properties 内的 summary、version、timestamp 和 TTL aliases 都会恢复。
- 本轮补充：microcompact disk cache loader 现在也会穿透 `edges`/`nodes`/`included` 等 collection wrapper 和数组元素，跳过无 summary 的非 cache resource，并恢复 GraphQL connection 或 JSON:API included 风格 cache entry。
- 本轮补充：microcompact disk cache loader 的 summary-like payload 现在接受 text content-block object、text content-block array 和 string array，会把可见 text block 合并为 summary，并会继续解包 text block 内嵌的 JSON/fenced summary payload，兼容官方/SDK 响应内容块形态的 cached microcompact 文件。
- 本轮补充：microcompact disk cache loader 的 summary array item 现在也复用 provider-style `parts`/`content.parts`/`output` 文本恢复路径，批量候选或 provider cache item 不再因数组元素不是标准 text block 而失效。
- 本轮补充：microcompact disk cache loader 的 summary-like payload 现在也接受完整 contract message object，并会递归解包 `message`/`assistantMessage`/`resultMessage`/`outputMessage`/`completionMessage` wrapper，从 message content 中恢复 visible text summary。
- 本轮补充：microcompact disk cache loader 的 summary array 元素现在也接受完整 contract message object，可把 message list 与 content-block 混合数组恢复成可见摘要文本。
- 本轮补充：microcompact disk cache loader 现在会把 `value` 字段中的 text content-block object 识别为 direct summary payload，同时继续从同一 `value` object 中补 digest、version、timestamp 等 cache metadata，避免 `value` 作为 summary/cache wrapper 双义字段时丢摘要或 sidecar 信息。
- 本轮补充：microcompact disk cache loader 现在也接受 provider-style `choices`/`outputs`/`candidates`/`generations`/`results` 等响应数组 wrapper，并可从 `message.content`、`output.content`、`content.parts[].text` 和 fenced `json` code block 中恢复 summary，同时保留外层 cache metadata。
- 本轮补充：contract content block 与 microcompact cache loader 现在把 provider `output_text`/`outputText` content block 归一为可见 text，OpenAI Responses 风格 `output[].content[].type="output_text"` cache payload 可恢复 summary，而 reasoning/tool/image blocks 继续跳过。
- 本轮补充：microcompact disk cache loader 的 summary-like 字段现在也接受 `body`、`markdown`、`description`/`details`、`finalSummary`、`summaryContent`、`resultText`、`completionText`、`responseText`、`messageText` 等 direct/provider aliases，并在 nested cache entry、JSON:API/resource 和 provider-style response wrapper 中统一恢复可见摘要文本。

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
- 本轮补充：task runtime 在状态行、任务面板排序/渲染、批量取消和 scripted task expectation 前会把 `active`/`inProgress`/`in_progress`、`success`/`done`/`completedSuccessfully`、`error`/`failedWithError`、`canceled`/`cancelledByUser` 等 task state 别名归一为 canonical 状态。
- 本轮补充：scripted task runtime payload 和 task expectation 现在接受 `taskID`、`jobId`、`runId`、`label`、`displayName`/`displayTitle`、`operationName`/`commandName`、`phase`/`taskState`/`jobStatus`/`resultState`、`message`/`currentStep`/`statusMessage`/`outputText`/`resultText`、`percent`/`percentage`/`pct`/`completionPercent` 等相邻字段，并支持数字 task ID 与数字字符串 progress。
- 本轮补充：scripted task runtime 的 task payload、expectation 和 remove-task mutation 现在共享 `operationId`、`requestId`、`threadId`、`workflowId`、`toolUseId` 等相邻 task ID aliases，录制脚本可用 operation/request 风格 ID 追踪和移除任务。
- 本轮补充：permission runtime 会把 `Reject`/`deny`/`decline`/`disallow`/`no` 等 permission action 归一为 denied 结果，把 `Cancel`/`abort` 归一为 cancelled 结果，并让 scripted dialog-result status 断言接受 `rejected`/`approved` 等状态别名。
- 本轮补充：scripted permission payload、dialog expectation、event、cancel-permission 和 dialog-result expectation 现在接受 `ID`/`ToolName`/`Actions`、`permissionID`、`requestID`、`toolUseID`、`operationID`、`operation`、`commandName`、`resourcePath`、`body`、`reasonText`、`allowedActions`、`buttons` 等相邻字段，并支持数字 request ID。
- 本轮补充：configurable keybinding name parser 现在接受 macOS/DOM 风格 `cmd`/`command`/`super` modifier aliases，和现有 `meta`/`option` 一样映射到 prompt word motion、yank-pop、delete-word-back 与 modified left/right arrow key。
- 本轮补充：scripted runtime 的 permission/task payload 现在会递归解包 `value`/`payload`/`data`/`resource`/`attributes`/`properties`/`attrs`/`edge`/`node`、`included`/`collection`/`list`/`values` 等 JSON:API/GraphQL wrapper；resource/node 顶层 ID 会回填到内层 permission/task，带明确非 permission/task `type` 的 included resource 会被跳过，并避免仅因 wrapper 顶层 ID 生成半空 runtime 对象。
- 本轮补充：scripted runtime mutation 的 `removeTask`/`cancelPermission`/`cancelTasks` 现在也会从 wrapped `value`/`payload`/`data`/`resource`/`edge.node` 中递归读取 task/permission ID 和 cancellation detail，兼容 resource/action fixture 直接驱动 runtime mutation。
- 本轮补充：scripted runtime mutation 的直接 alias 字段现在也接受 object payload，例如 `removeTask: {resource:{id}}`、`cancelPermission: {edge:{node:{id}}}` 和 `cancelTasks: {resource:{attributes:{reasonText}}}` 不再因 string/bool 强类型字段提前解析失败。
- 本轮补充：scripted runtime action 的 boolean payload 现在同样递归解包 `resource`/`attributes`/`edge.node`/`attrs`，`cancelTasks`/`openTasks` 等动作可用 wrapped `enabled:false`/`open:false` 明确禁用，避免 wrapper object 默认 fallback 成 true。
- 本轮补充：scripted interaction 的直接 key alias 字段现在也接受 wrapped object，例如 `key: {resource:{attributes:{value}}}`、`keyPress: {resource:{attributes:{key}}}` 和 `keyPresses: {edge:{node:{attrs:{sequence}}}}` 不再因 string/list 强类型字段提前解析失败。
- 本轮补充：scripted interaction 的布尔字段现在接受非严格 bool payload，包括 `"true"`/`"false"`、`yes`/`no`、`on`/`off` 和数字 `1`/`0`；覆盖 mouse release、dialog visible/result、prompt empty、vim state、reverse-search，以及 focus/cancel/openTasks/expectNoEvent/expectFocused 等顶层 step 控制字段。
- 本轮补充：scripted interaction step 现在接受 `expect`/`expected`/`assertions`/`checks`/`verify`/`then`/`after` 等 expectation wrapper object，可把嵌套的 prompt/event/dialog/snapshot/screen/task/vim/viewport 断言映射到已有 `expect*` 字段。
- 本轮补充：scripted interaction expectation wrapper 现在也接受 assertion/check 数组；数组元素可用 `type`/`kind`/`name`/`target` 等 discriminator 加 `value`/`payload` 载荷声明 prompt/event/dialog/snapshot/screen/task/vim/viewport 断言，覆盖官方 fixture 常见的分列 checks 形态。
- 本轮补充：scripted interaction assertion/check 数组元素的载荷现在也接受 `resource`/`node`/`attributes`/`properties`/`result`/`response`/`output`，让 JSON:API/resource-style 断言体可直接映射到 prompt/event/dialog/snapshot/screen/task/vim/viewport expectation。
- 本轮补充：keybinding config、keymap 解析和 interaction script named-key 输入接受 `ctrl-h`/`ctrl-i`/`ctrl-j`/`ctrl-m`、`ctrl-[`、`ctrl-?`、对应 `control-*` 以及 compact/camel 形式；terminal parser 支持 CSI-u/kitty keyboard protocol 的 ctrl/alt/shift-enter/shift-tab 序列；image hint parser 支持 OSC ST terminator 和 base64 `name=` filename；keybinding JSON loader 支持 wrapper object-map、`shortcuts`、object action 字段、string-array key sequence/chord 和 `null`/`false` unbind；mouse parser 支持 legacy X10/normal tracking 序列；interaction script 支持结构化 mouse/mouse_event 步骤，button 接受 `buttonMask`/`button_mask`/`btn`/`code`/`mask`，坐标接受 `mouseX`/`mouse_x`/`clientX`/`screenX`/`pageX`/`offsetX`/`viewportX` 和对应 Y/row/line 别名，release 接受 `mouseUp`/`isRelease`/`mouseRelease`/`releaseEvent` 等字段别名；interaction script 还支持字符串 `keys` 和 `input`/`input_text`/`keys_text`/`raw_key`/`paste_text` 字段别名，并允许 status/snapshot/viewport/pasted-content contains 断言使用单字符串或字符串数组，`keybindings`、`expectEvents`、`expectDialogResults`、`expectPrompt.pastedContents` 和 `expectTasks.contains` 使用单对象或对象数组。
- 本轮补充：keybinding config 的 page/navigation/editing key name 现在接受 `pgup`/`pg-up`/`prior`/`pageUpKey`、`pgdn`/`pg-down`/`next`/`pageDownKey`、`homeKey`/`endKey`、`deleteForward`/`forwardDelete` 和 `deleteBackward`，覆盖常见终端、DOM 和配置别名。
- 本轮补充：keybinding config 和脚本 named-key 输入现在接受 DOM/fixture 风格的 `enterKey`/`returnKey`/`numpadEnter`、`escapeKey`/`escKey`、`tabKey`、`shiftEnterKey`/`shiftNumpadEnter`、`shiftTabKey`/`backtabKey`，减少配置导入时对基础控制键名称的手工归一。
- 本轮补充：keybinding config 和脚本 named-key 输入接受 DOM-style arrow key aliases，包括 `arrowLeft`/`arrowRight`/`arrowUp`/`arrowDown` 以及 ctrl/alt/meta/option arrow variants。
- 本轮补充：keybinding action parser 接受更多 editor/global-style action aliases，包括 `cursorLeft`/`cursorRight`、`previousWord`/`nextWord`、`lineStart`/`lineEnd`、`moveToBeginningOfLine`/`moveToEndOfLine`、`deletePreviousChar`/`deleteNextChar`、`backwardDelete`/`forwardDelete`、`deleteToStartOfLine`/`deleteToEndOfLine`、`killLine`、`pasteKillRing`/`yankPrevious`、`clearScreen`、`openExternalEditor`、`toggleTasks`、`cancelAgents`、`focusPrev`、`acceptSelection` 和 `search`。
- 本轮补充：keybinding config 和脚本 named-key 输入接受短修饰符别名，包括 `c-`/`m-`/`a-`/`opt-`/`s-` 以及 compact/camel 形式，可覆盖 control、meta、alt、option 和 shift key names。
- 本轮补充：keybinding config 和脚本 named-key 输入现在接受 `backtab`/`back-tab`/`btab` 等 Shift-Tab terminfo/fixture 别名，并映射到既有 focus-previous key surface。
- 本轮补充：keybinding JSON loader 现在递归解包 `data`/`payload`/`settings`/`config`/`keyboard`/`keymap` 等外层 wrapper，嵌套 preference export 中的 `bindings`/`shortcuts` 不需要手工扁平化。
- 本轮补充：keybinding JSON loader 现在也递归解包 JSON:API/resource-style `resource`/`attributes`/`properties`/`attrs` wrapper，API/preferences envelope 内的 `keybindings`/`keymap` 可直接加载。
- 本轮补充：keybinding JSON loader 现在把 `data`/`payload`/`body`/`result`/`response`、`resources`、`included`、`collection`/`list`/`children`/`values`、`nodes` 和 `items` 下的数组视为 binding list，数组元素也可直接使用 JSON:API/resource-style `resource`/`node`/`attributes`/`properties` wrapper。
- 本轮补充：keybinding JSON loader 现在也接受 GraphQL connection 风格的 `edges` binding list，binding item 可用 `edges[].node` 或 `edge.node` wrapper，外层可递归解包 `viewer`/`node`/`*Connection` wrapper。
- 本轮补充：keybinding JSON loader 现在接受 `keymap`/`keymaps`、`keyboardShortcuts`、`hotkeys`、`userKeybindings`、`customKeybindings` 等集合字段别名，并同时支持直接 object-map 和嵌套 `bindings` wrapper；单条 binding 的 key 字段也接受 `accelerator`、`keystroke`、`hotKey`、`keyCombo` 和 `keyChord` aliases。
- 本轮补充：keybinding JSON loader 现在也接受 provider-style `choices`/`outputs`/`candidates`/`generations` response wrapper，可从 `message.content`、content-block array 和 `content.parts[].text` 里恢复 binding array 或 object map。
- 本轮补充：interaction script 的 per-step keybinding mutation 复用同一套 collection alias、object-map 和 resource wrapper 解析，脚本步骤可直接使用 `keymap`、`keyboardShortcuts`、`hotkeys`、`keyboard`、`preferences` 或 `keybindingConfig` 临时改键位。
- 本轮补充：interaction script 的 `keys` 字段支持 printable text chunk 和空格分隔 named-key sequence，例如 `ctrl-x ctrl-k`，减少官方脚本把连续输入拆成数组的改写成本。
- 本轮补充：interaction script key input 接受 press-style aliases，包括 `press`、`keyPress`、`keypress`、`shortcutKey`、`presses`、`keyPresses` 和 `shortcuts`。
- 本轮补充：interaction script loader 现在会扁平化 `cases`/`tests`/`testCases`/`scenarios`/`fixtures` 等 suite array，每个 case 内的 `steps`/`timeline`/`scriptSteps` 会按顺序展开，顶层数组也可直接混入 case object。
- 本轮补充：interaction script loader 现在也接受 provider-style `choices`/`outputs`/`candidates`/`generations` response wrapper，可从 `message.content`、content-block array 和 `content.parts[].text` 里递归恢复 script JSON。
- 本轮补充：interaction script 和 keybinding provider response 现在会剥离 fenced `json` code block，模型/SDK 返回 code-fenced 脚本或 keybinding 配置时不再需要手工去 fence。
- 本轮补充：interaction script key/keySequence action payload 现在递归解包 JSON:API/GraphQL-style wrapper，`payload.resource.attributes.key` 和 `edge.node.attrs.sequence` 可直接驱动按键与组合键序列。
- 本轮补充：interaction script 的直接字符串 alias 字段现在也接受 wrapped object；`text`、`pasteText`、`setStatus`、`snapshotName` 等字段可从 `resource.attributes` 或 `edge.node.attrs` 中恢复正文、paste、status 和 snapshot 名称，避免 direct field fixture 在 scalar decode 阶段失败。
- 本轮补充：interaction script action/type/kind/name/operation 动作判别字段接受 compact/camel fixture aliases，包括 `typeText`、`inputText`、`insertText`、`keyPress`、`pressKey`、`keySequence`、`pasteText`、`pastedText`、`clipboardText`、`setStatus`、`statusLine`、`terminalSize` 和 `screenSize`。
- 本轮补充：interaction script 的 typeText/pasteText/setStatus/snapshot 字符串 action payload 现在递归解包 JSON:API/GraphQL-style wrapper，`payload.resource.attributes.text`、`edge.node.attrs.content`、`resource.attributes.message` 和 wrapped snapshot `name` 可直接驱动 prompt、paste、status 与 snapshot capture。
- 本轮补充：interaction script resize/terminalSize/screenSize action payload 现在递归解包 `value`/`payload`/`data`/`resource`/`attributes`/`properties`/`attrs`/`edge.node` 等 wrapper，官方/API fixture 可把 columns/rows 放在 JSON:API 或 GraphQL envelope 内。
- 本轮补充：interaction script 的 direct resize 数字 alias 字段现在也递归解包 wrapper；`resizeWidth`/`resizeHeight`、`screenWidth`/`screenHeight` 和 terminal width/height 相邻别名可从 wrapped `value`、`columns`、`rows` 中恢复尺寸。
- 本轮补充：interaction script 的 direct focus bool alias 字段现在也递归解包 wrapper；`focus`、`focused`、`focusIn`、`focusOut`、`blur`/`blurred` 可从 wrapped `enabled`、`value`、`selected` 等字段恢复焦点事件控制。
- 本轮补充：interaction script 的 focus/blur action-discriminator 现在也接受 `value`/`payload`/`data` 中的 wrapped bool；`action:"focus"`、`kind:"blur"`、`operation:"focusState"` 和 `name:"setFocus"` 可用 `focused:false`、`blurred:false` 或非严格 bool payload 明确发出 focus-out/focus-in。
- 本轮补充：interaction script 的 direct expectation bool alias 字段现在也递归解包 wrapper；`expectNoEvent`、`expectNoDialogResult(s)`、`expectFocused` 可从 wrapped `value`/`enabled`/`selected` 恢复断言控制。
- 本轮补充：interaction script 的 direct expectation count alias 字段现在也递归解包 wrapper；`expectEventCount`、`expectTotalEventCount`、`expectDialogResultCount`、`expectTotalDialogResultCount` 可从 wrapped `value`/`count`/`total` 恢复计数断言。
- 本轮补充：interaction script 的 direct expectation string-list alias 字段现在也递归解包 wrapper；`expectStatusContains`/`NotContains` 和 `expectSnapshotContains`/`NotContains` 可从 wrapped `value`/`values`/`contains`/`items` 恢复断言列表。
- 本轮补充：interaction script 的 direct expectation collection alias 字段现在也递归解包 wrapper；`expectEvents`、`expectDialogResults` 可从 wrapped `events`/`results`/`items`/`nodes` 中恢复结构化断言列表。
- 本轮补充：interaction script 的 direct single expectation alias 字段现在也递归解包 wrapper；`expectEvent`、`expectDialogResult` 可从 wrapped `event`/`result`/`expected` 中恢复结构化单项断言。
- 本轮补充：interaction script 的 direct single expectation alias 字段现在改为 raw payload 解析；`expectEvent`、`expectDialogResult` singular 字段也可接受单元素数组并取首项，避免基础 step unmarshal 提前失败。
- 本轮补充：interaction script 的 direct prompt expectation 现在也递归解包 JSON:API/GraphQL-style wrapper；`expectPrompt`/`expect_prompt` 可从 `resource.attributes`、`edge.node.attrs` 等外壳恢复 prompt text、cursor、empty、pasted-content count 和 next pasted ID 断言。
- 本轮补充：interaction script 的 direct vim expectation 现在也递归解包 JSON:API/GraphQL-style wrapper；`expectVim`/`expect_vim` 可从 `resource.attributes`、`edge.node.attrs` 等外壳恢复 enabled、mode、register 和 register-linewise 断言。
- 本轮补充：interaction script 的 direct screen/viewport expectation 现在也递归解包 JSON:API/GraphQL-style wrapper；`expectScreen`/`expect_screen` 和 `expectViewport`/`expect_viewport` 可从 `resource.attributes`、`edge.node.attrs` 等外壳恢复 columns/rows、scroll offset、visible line count 和 visible contains/not-contains 断言。
- 本轮补充：interaction script 的 direct task/reverse-search expectation 现在也递归解包 JSON:API/GraphQL-style wrapper；`expectTasks`/`expect_tasks` 和 `expectReverseSearch`/`expect_reverse_search` 可从 `resource.attributes`、`edge.node.attrs` 等外壳恢复 task count/stateCounts/contains、reverse active/query/cursor/current/result-count 断言，并保留 wrapped `active:false` 与 `taskCount:0`。
- 本轮补充：interaction script 的 direct dialog expectation 现在也递归解包 JSON:API/GraphQL-style wrapper；`expectDialog`/`expect_dialog` 可从 `resource.attributes`、`edge.node.attrs` 等外壳恢复 active、ID/kind、title/body、body/action contains、action count 和 focused index 断言，并保留 wrapped `active:false`。
- 本轮补充：interaction script action/type/kind/name/operation 动作判别字段继续接受 compact/camel event/media aliases，包括 `focusIn`、`focusOut`、`mouseEvent`、`pasteImage` 和 `imagePaste`。
- 本轮补充：interaction script mouseEvent/pasteImage action payload 现在递归解包 JSON:API/GraphQL-style `resource`/`attributes`/`properties`/`attrs`/`edge.node` wrapper，mouse payload 的 Button/X/Y/Release 可接受数字字符串/非严格 bool，wrapped mouse 坐标/按钮和 image filename/media/content 可直接驱动 dialog click 与 image paste。
- 本轮补充：interaction script action/type/kind/name/operation 动作判别字段也可驱动 runtime/dialog mutation，支持 `requestPermission`、`taskStatus`、`showTasks`、`cancelTasks`、`removeTask` 和 `showDialog` 等动作，并从 `value`/`payload`/`data`/`body` 载荷解析对象、ID 或取消原因。
- 本轮补充：interaction script 的 direct runtime mutation 字段现在改为 raw payload 解析；`requestPermission`/`request_permission` 和 `upsertTask`/`upsert_task` singular 字段也可接受单元素数组并取首项，避免基础 step unmarshal 提前失败。
- 本轮补充：interaction script 的 direct mouse 字段现在改为 raw payload 解析；`mouse`/`mouse_event`/`mouseEvent` singular 字段可接受单元素数组并递归解包 `resource.attributes`、`edge.node.attrs` 等 wrapper，避免录制脚本把鼠标事件包成 API payload 时提前失败。
- 本轮补充：interaction script 的 direct message list 字段现在改为 raw payload 解析；`messages`/`appendMessages`/`transcriptMessages` 可递归解包 `resource.attributes`、`data[]`、`edge.node.attrs` 等 API/GraphQL wrapper，并保留 image paste id 等 message metadata。
- 本轮补充：interaction script 的 direct single message 字段现在也复用 raw message parser；`message`/`Message` 可递归解包 `resource.attributes`、`edge.node.attrs`，并在数组形态下回退为多条 message 追加，避免单条消息 wrapper 被基础解码成空消息。
- 本轮补充：interaction script 的 direct dialog 字段现在改为 raw payload 解析；`dialog`/`Dialog` 可递归解包 `resource.attributes`、`edge.node.attrs` 和单元素数组，`showDialog` action payload 也复用同一解析路径，dialog payload 的 `ID`/`Focused` 可接受数字和数字字符串，避免 wrapper-only 或 Go 默认字段名 dialog 被解码失败/空弹窗。
- 本轮补充：interaction script 的 direct image 字段现在改为 raw payload 解析；`image`/`Image` 可复用 pasteImage action 的 wrapper 解析，递归解包 `resource.attributes`、`edge.node.attrs` 和单元素数组，保留 filename/media/content/source-path 后再生成 prompt pasted-image。
- 本轮补充：interaction script 的 direct keybinding mutation 字段现在改为 raw-only 解析；`keybindings`/`key_bindings`/`keyBindings`/`keybindingSpecs`/`Keybindings` 不再先走 `[]BindingSpec` 基础解码，统一复用 keybinding loader 的 object-map、resource wrapper 和 edge/node collection parser。
- 本轮补充：interaction script 的 UpperCamel step 字段现在也进入 raw/wrapper 解析路径；`UpsertTask`、`CancelAllTasks`、`OpenTasksDialog`、`ExpectEvent(s)`、`ExpectDialog`、`ExpectDialogResult(s)`、`ExpectPrompt`、`ExpectVim`、`ExpectScreen`、`ExpectViewport`、`ExpectReverseSearch`、`ExpectTasks` 等 Go 默认字段名可直接接受 JSON:API/GraphQL-style wrapper，task status/expectation 的 ID/title/state/detail/progress、tasks expectation 的 Count/StateCounts、event expectation 的 Type/Value/DialogID、dialog expectation 的 Active/ActionCount/FocusedIndex、dialog-result expectation 的 ID/Found/Stale、prompt expectation 的 Cursor/PastedContentCount/NextPastedID/Empty、pasted-content expectation 的 ID/MediaType/Filename/ContentContains、vim expectation 的 Enabled/Mode/Register/RegisterLinewise、screen expectation 的 Width/Height、viewport expectation 的 Offset/VisibleLineCount 和 reverse-search expectation 的 Cursor/ResultCount/NoResults 改为 raw-first 解码并继续接受数字字符串/非严格 bool。
- 本轮补充：terminal CSI-u/kitty keyboard parser 接受 `codepoint:alternate` 和 `modifier:event-type` 冒号字段，按主 codepoint/modifier 解析 ctrl/alt/shift/rune 键，覆盖 kitty progressive keyboard protocol 的常见变体。
- 本轮补充：terminal CSI-u/kitty keyboard parser 接受无 modifier 字段或 modifier `1` 的 base key 序列，覆盖 printable rune、Enter、Tab、Esc 和 Backspace，避免 extended-key 模式下普通键序列被解析成 unknown。
- 本轮补充：terminal CSI-u/kitty keyboard parser 现在把 shift-only Backspace (`CSI 8;2u`/`CSI 127;2u`) 仍映射到 Backspace，避免 kitty extended-key 模式下退格被误当作 DEL rune 或 unknown。
- 本轮补充：terminal input parser 接受 xterm modified arrow 序列（如 `CSI 1;2D`、`CSI 1;6C`、`CSI 1;7D`），把 shift-arrow 降级为方向键、alt-arrow 映射到 word-motion key、ctrl/ctrl+alt-arrow 映射到 ctrl word-motion key，避免 extended navigation 序列落入 unknown。
- 本轮补充：terminal input parser 现在把 xterm modified navigation modifier 范围扩展到 `2..16`，覆盖 meta/shift+meta/ctrl+meta 组合（如 `CSI 1;10D`、`CSI 1;16C`）以及对应 Home/End/Delete/PageUp/PageDown 序列。
- 本轮补充：terminal CSI-u/kitty keyboard parser 现在按 modifier bitfield 解码 `9..16` 扩展组合，把 meta/shift+meta 映射到现有 alt key surface，把 ctrl+meta 组合保留为 ctrl key，覆盖 `CSI 98;9u`、`CSI 97;13u` 等序列。
- 本轮补充：terminal CSI parser 把 DA/device attributes (`CSI c`、`CSI >c`、`CSI =c`) 归入 report action，并在 terminal parser dispatcher 中作为 `TerminalActionReport` 暴露。
- 本轮补充：terminal CSI parser 现在保留多参数 DA/device-attributes response 的完整 code list，例如 `CSI ?62;1;2;6c` 不再只留下首个 terminal type code。
- 本轮补充：terminal CSI parser 接受 `CSI a`/`CSI e`/`CSI \`` cursor alias final bytes，并映射到已有 cursor-forward/cursor-down/cursor-column actions。
- 本轮补充：terminal CSI parser 接受 ECMA `CSI Ps j` / `CSI Ps k` HPB/VPB backward cursor final bytes，并映射到已有 cursor-back/cursor-up actions。
- 本轮补充：terminal CSI parser 接受 DEC private mode `?1047h/l` alternate-screen buffer 和 `?1048h/l` save/restore cursor，复用已有 mode/cursor actions。
- 本轮补充：terminal CSI parser 把 DECREQTPARM terminal-parameters (`CSI x`) 归入 report action，保留 code/private marker。
- 本轮补充：terminal CSI parser 现在保留 DECREPTPARM/terminal-parameters response 的完整参数列表，例如 `CSI 2;1;1;112;112;1;0x` 不再只留下 report code。
- 本轮补充：terminal CSI parser 把 DECRQM mode request (`CSI Ps $ p` / `CSI ? Ps $ p`) 归入 report action，保留 mode code 和 DEC private marker。
- 本轮补充：terminal CSI parser 现在保留 DECRQM mode-request 的完整参数列表，例如 `CSI ?25;1000$p` 会同时暴露首个 mode code 和原始 params。
- 本轮补充：terminal CSI parser 把 CPR cursor-position responses (`CSI row;col R` / DEC private `CSI ? row;col R`) 归入 report action，结构化暴露 row/column 并保持 visible-text stripping。
- 本轮补充：terminal CSI parser 继续补齐 DEC 私有 DSR/CPR，`CSI ?6n` 现在归入 cursor-position report query，`CSI ?row;col;page R` 会保留 page 元数据。
- 本轮补充：terminal CSI parser 现在保留 CPR cursor-position response 的完整参数列表，例如 `CSI ?12;34;2R` 会同时暴露 row/column/page 和原始 params。
- 本轮补充：terminal CSI parser 现在保留 DSR/device-status report 的完整参数列表，例如 `CSI ?6;1n` 会同时暴露首个 report code 和原始 params。
- 本轮补充：terminal CSI parser 把 xterm window manipulation/report (`CSI t`) 归入 report action，覆盖常见 `CSI 14t`/`CSI 18t` 查询，并把 `CSI 4;height;width t` 与 `CSI 8;rows;cols t` 的 pixel/text-area 尺寸参数结构化暴露。
- 本轮补充：terminal CSI parser 现在保留 xterm window report 的完整参数列表，例如 `CSI 3;x;y t`、`CSI 4;height;width t`、`CSI 8;rows;cols t` 不再丢失 report code 后面的原始字段。
- 本轮补充：terminal CSI parser 把 DECRPM mode status report (`CSI Ps;Ps $ y` / `CSI ? Ps;Ps $ y`) 归入 report action，保留 mode code、status 和 DEC private marker。
- 本轮补充：terminal CSI parser 现在保留 DECRPM mode-status response 的完整参数列表，例如 `CSI ?25;2$y` 会同时暴露 code/status 和原始 params。
- 本轮补充：terminal CSI parser 把 TBC tab-clear (`CSI g`/`CSI 3g`) 归入 cursor action，保留 clear-current/all code。
- 本轮补充：terminal ESC parser 把 HTS horizontal-tab-set (`ESC H`) 归入 cursor/tab-set action，和 CSI tab-clear 控制序列形成闭环。
- 本轮补充：terminal sequence dispatcher 把 SS3 application cursor (`ESC OA`/`OB`/`OC`/`OD`) 归入结构化 cursor move action，避免 application cursor mode 序列落入 unknown。
- 本轮补充：terminal CSI parser 把 DEC `?1h/l` application cursor mode 解析成独立 mode action，和 SS3 application cursor key 解析闭环。
- 本轮补充：terminal CSI parser 把 DEC `?3h/l` 132/80-column mode 解析成结构化 `columnMode` action，覆盖常见列宽状态切换序列。
- 本轮补充：terminal CSI parser 把 DEC `?40h/l` allow column switching mode 解析成结构化 `allowColumnSwitch` mode action，补齐 `?3h/l` 列宽切换相邻的许可状态序列。
- 本轮补充：terminal CSI parser 把 DEC `?95h/l` no-clear-on-column-switch mode 解析成结构化 `noClearOnColumnSwitch` mode action，补齐列宽切换时是否清屏的状态序列。
- 本轮补充：terminal CSI parser 把 DEC `?5h/l` reverse video/screen mode 解析成结构化 `reverseVideo` mode action，继续减少终端显示状态序列的 unknown fallback。
- 本轮补充：terminal CSI parser 把普通 `CSI 4h/l` insert/replace mode 解析成 `insertMode` action，避免 ECMA mode set/reset 序列落入 unknown。
- 本轮补充：terminal CSI parser 把普通 `CSI 20h/l` line-feed/new-line mode 解析成 `lineFeedMode` action，继续覆盖 ECMA mode set/reset 序列。
- 本轮补充：terminal CSI parser 把 DEC `?6h/l` origin mode 和 `?7h/l` auto-wrap mode 解析成结构化 mode action，继续减少终端状态序列的 unknown fallback。
- 本轮补充：terminal CSI parser 把 DEC `?8h/l` auto-repeat mode 解析成结构化 `autoRepeat` mode action，补齐键盘重复状态序列。
- 本轮补充：terminal CSI parser 把 DEC `?12h/l` cursor blink mode 解析成结构化 `cursorBlink` mode action，补齐 cursor visibility/style 相邻的终端状态序列。
- 本轮补充：terminal CSI parser 把 DEC `?44h/l` margin bell mode 解析成结构化 `marginBell` mode action，补齐 wrap/margin 相邻的响铃状态序列。
- 本轮补充：terminal CSI parser 把 xterm/DEC `?45h/l` reverse-wraparound mode 解析成结构化 `reverseWrap` mode action，补齐 auto-wrap 相邻的 wrap 状态序列。
- 本轮补充：terminal CSI parser 把 DEC `?46h/l` logging mode 解析成结构化 `logging` mode action，避免日志状态序列落入 unknown fallback。
- 本轮补充：terminal CSI parser 把 DEC `?66h/l` application keypad mode 解析成结构化 `applicationKeypad` mode action，补齐 application cursor mode 相邻的输入状态序列。
- 本轮补充：terminal ESC parser 现在把 VT100 `ESC =`/`ESC >` application/numeric keypad 模式也归一成 `applicationKeypad` mode action，和 CSI `?66h/l` 输出保持一致。
- 本轮补充：terminal CSI parser 把 DEC `?67h/l` backarrow key mode 解析成结构化 `backarrowKey` mode action，补齐键盘输入状态序列。
- 本轮补充：terminal CSI parser 把 DEC `?69h/l` left/right margin mode 解析成结构化 `leftRightMarginMode` mode action，补齐 scroll-region 相邻的 margin 状态序列。
- 本轮补充：terminal CSI parser 现在把带参数的 `CSI Pl;Pr s` 解析成 left/right horizontal margin region action，同时保留无参数 `CSI s` save-cursor 语义，和 DEC `?69h/l` margin mode 闭环。
- 本轮补充：terminal CSI parser 现在识别带 intermediate space 的 `CSI Ps SP @` / `CSI Ps SP A` scroll-left/right 序列，避免误解析成 insert-characters 或 cursor-up。
- 本轮补充：terminal ESC parser 现在把 charset selection (`ESC ( B` / `ESC ) 0` / `ESC * B` / `ESC / A` / `ESC % G` 等) 解析成结构化 charset action，并在 terminal parser 可见文本管线中消费，避免常见终端 charset 选择序列残留为 unknown。
- 本轮补充：terminal ESC parser 现在也把 ISO-2022 charset shift 控制 (`ESC N`、`ESC n`、`ESC o`、`ESC |`、`ESC }`、`ESC ~`) 解析成结构化 charset-shift action，并在可见文本管线中消费，继续减少真实终端输出里的 unknown 控制序列。
- 本轮补充：terminal CSI parser 现在把 DEC selective erase `CSI ? Ps J` / `CSI ? Ps K` 标记为 selective display/line erase，和普通 ED/EL 区分开。
- 本轮补充：terminal CSI parser 现在把 ECMA `CSI Ps N` / `CSI Ps O` 解析成 erase-in-field / erase-in-area action，覆盖 to-end/to-start/all 三种 region。
- 本轮补充：terminal CSI parser 把 DEC insert/delete columns (`CSI Ps ' }` / `CSI Ps ' ~`) 归入 edit action，避免列编辑控制序列落入 unknown fallback。
- 本轮补充：terminal CSI parser 把 REP repeat-preceding-character (`CSI b`) 归入 edit action，visible-text/snapshot pipeline 和 ANSI message wrapping/trim 会按重复次数展开前一个可重复 grapheme。
- 本轮补充：terminal CSI parser 按 ANSI 默认参数解析 scroll-region (`CSI r`/`CSI ;10r`)，缺失 top 默认为 1，缺失 bottom 保持 0 表示 reset/full-height，避免 reset scroll region 被误判为单行区域。
- 本轮补充：terminal CSI parser 现在按 ANSI 默认参数处理显式 `0` 计数/位置参数，cursor movement/position/column、insert/repeat/erase chars 和 scroll up/down 这类动作会把 `CSI 0...` 解析为默认 1，同时保留 mode/report/erase selector 的原始 0 语义。
- 本轮补充：terminal CSI parser 把 DECSTR soft reset (`CSI !p`) 归入 reset action，并在 terminal parser 中清理 SGR/link 状态。
- 本轮补充：terminal parser 的 text grapheme 分段继续补齐 emoji keycap sequence，`1️⃣`/`2⃣` 这类 keycap 在完整输入和跨 `Feed()` 边界切在 base 或 variation selector 后时都会保持单个宽 grapheme；完整 Unicode UAX #29 分段仍未宣称完成。
- 本轮补充：terminal parser 的 text grapheme 分段继续补齐 Hangul L/V/T jamo 连接规则，decomposed `한` 这类音节在完整输入以及跨 `Feed()` 边界切在 leading/vowel jamo 后时都会保持单个宽 grapheme；完整 Unicode UAX #29 分段仍未宣称完成。
- 本轮补充：terminal parser 的 text grapheme 分段继续补齐 CRLF line-break cluster，完整输入和跨 `Feed()` 边界切在 `\r`/`\n` 中间时都会保持单个零宽 line-break grapheme，wrap/trim/render 宽度路径也复用同一 line-break 判断；完整 Unicode UAX #29 分段仍未宣称完成。
- 本轮补充：terminal parser 的 text grapheme 分段继续补齐 Unicode mark category 和 Prepend 规则，nonspacing/enclosing/spacing mark 会归入前一 cluster，`का` 这类 spacing mark cluster 不再被拆宽，Arabic prepend mark 加 base text 会保持同一 grapheme，单独 prepend mark flush 时按零宽处理；完整 Unicode UAX #29 分段仍未宣称完成。
- 本轮补充：prompt history 写入按官方 `history.ts` 跳过 image pasted content，避免把 image base64/filename/mediaType 存入 `history.jsonl`，读取路径仍兼容旧 image metadata。
- 本轮补充：paste-cache 增加按 cutoff mtime 清理旧 `.txt` paste 文件的 best-effort 入口，忽略缺失目录、非 `.txt` 文件和单文件错误，和官方 `cleanupOldPastes` 语义对齐。
- 本轮补充：Buffered prompt history writer 支持撤销最近 pending entry，覆盖官方 `removeLastFromHistory` 在异步 flush 前直接从 pending buffer 移除的 fast path。
- 本轮补充：Buffered prompt history writer 支持撤销已 flush entry 的 slow path：最近 entry 已落盘时记录 timestamp，并让同一 writer 的 up-arrow/ctrl-r 历史读取按当前 session 跳过该 entry。
- 本轮补充：image-cache 增加 session-scoped 图片路径缓存、base64 图片落盘、批量 image 存储、内存路径查询和旧 session cache 清理，贴近官方 `imageStore.ts` 的存取/清理基础。
- 本轮补充：PromptInput/REPL screen 可显式启用 image-cache session，image hint paste 会在插入 `[Image #N]` 时同步缓存路径并写入图片文件，补齐官方 PromptInput 侧的 `cacheImagePath`/`storeImage` 接入点。
- 本轮补充：image paste cache 现在会在没有原始 source path 时把生成的缓存路径写回 pasted-content 的 `SourcePath`，prompt image metadata/history 恢复不再只依赖全局 image-id 路径缓存。
- 本轮补充：prompt submit event 保留 display 和 pasted-content metadata，session 层提供 `PromptMessages` 将 text paste 展开、image paste 转成 Anthropic image source block，并为已缓存图片追加 source-path meta message。
- 本轮补充：pasted image metadata 保留 `dimensions` 和 `sourcePath`，支持 source/dimension 字段别名，并按官方 `createImageMetadataText` 格式生成 source path、原始/显示尺寸和坐标换算倍率 meta text。
- 本轮补充：image hint parser 现在从 iTerm2 OSC File metadata 解析 `width`/`height`、original/display dimension 别名和 `sourcePath`/`source_path`/`path`，并把这些字段传入 prompt pasted image metadata。
- 本轮补充：PromptInput paste 按官方路径清理 ANSI/CR/tab，并用 `PASTE_THRESHOLD=800` 和 `min(rows-10, 2)` 行阈值决定正常高度短 paste 内联、小窗口/长 paste 折叠为 pasted-content ref。
- 本轮补充：PromptInput 编辑后会移除未被 `[Image #N]` 引用的 image pasted-content，session `PromptMessages` 也会在请求构造前过滤 orphan image，避免删除的图片继续作为 image block/meta 发送。
- 本轮补充：image paste pill 支持官方 lazy-space 语义，连续图片自动分隔，图片后直接输入非空白字符时补一个空格，显式空格/换行不会重复补空格。
- 本轮补充：REPL message metadata 保留 `imagePasteIds`，并从已有用户消息的 image ids 和 pasted refs 初始化/推进 `NextPastedID`，贴近官方 resume/continue 避免 paste ID 重用的逻辑。
- 本轮补充：reverse-search 现在按完整 `HistoryEntry` 匹配和选择历史，选中后恢复 text/image pasted-content metadata，后续 submit event 仍携带 display 与图片元数据。
- 本轮补充：prompt history pasted-content 读取接受 `mimeType`/`mime_type`/`contentType`、`fileName`/`file_name`/`name`、`filePath`/`file_path`/`path` 等 text/image metadata 别名，使历史恢复路径和 image hint/parser metadata 兼容面一致。
- 本轮补充：prompt history pasted-content 正文字段接受 `text`/`body`/`message`/`raw`/`base64Data` 等别名，stored pasted-content hash 接受 `digest`/`checksum`/`sha256` 等别名，attachment/cache 风格历史记录可以恢复正文或命中 paste-cache。
- 本轮补充：prompt history `LogEntry` 读取接受 `sessionID`/`session`/`sessionUuid`/`sessionUUID`/`session_uuid` 作为 session id 别名，current-session-first 历史排序不会因 session 字段拼写不同而失效。
- 本轮补充：prompt history `HistoryEntry`/`LogEntry` 和 pasted-content item 现在接受 `entry`/`record`/`item`/`payload` 等 wrapper，stored history 可从 nested payload 读取显示文本/附件并用外层 project/session/timestamp metadata 补齐。
- 本轮补充：prompt history `HistoryEntry`/`LogEntry` 和 pasted-content item 现在也递归解包 `edge`/`node`/`resource`/`attributes`/`properties`/`attrs` wrapper，GraphQL/JSON:API history exports 可直接恢复 prompt 与附件 metadata。
- 本轮补充：REPL message restore 现在可从用户消息 content blocks、`imagePasteIds` 和 pasted-content metadata 重建 prompt text、`[Image #N]` 引用和 base64 image pasted contents。
- 本轮补充：Ctrl-S prompt stash 现在保存/恢复 prompt text、cursor 和 pasted-content metadata，空 prompt 下再次触发会恢复 stash，贴近官方 `chat:stash`。
- 本轮补充：remote history REST/link 风格分页接受 `links`/`_links` 的 `next`/`previous`/`prev`/`older` 字符串 URL 或 `{href,url,uri,link}` 对象，并从 `before_id`、`beforeId`、`cursor`、`pageCursor`、`previousCursor`、`prevCursor`、`beforeCursor`、`olderCursor`、`startCursor`、`endCursor` 等 query 参数提取续抓 before-id。
- 本轮补充：remote history REST/link 风格分页也接受 RFC/JSON:API 风格 `links` 数组，按 `rel`/`relation`/`name`/`type`/`kind`/`label` 中的 `previous`/`prev`/`older`/`next` 选择 continuation URL。
- 本轮补充：remote history HTTP `Link` header 分页接受 `rel="previous"`/`prev`/`older`/`next` URL，并按 RFC Link 结构处理 `<...>` URL 和 quoted 参数里的逗号，再从同一组 before/cursor query 参数提取续抓 before-id。
- 本轮补充：sidechain/subagent state loader 接受 `subagent_start`/`agent_start`/`task_start` 和 `sidechain_end`/`subagent_finish`/`agent_finish`/`task_summary` 等 subtype 别名，并归一化 `active`/`success`/`canceled`/`error` 等状态别名。
- 本轮补充：sidechain runtime finish 在写入 summary 前会把 `success`/`error`/`canceled` 等状态别名归一为 `completed`/`failed`/`cancelled`，sidechain transcript 和主 transcript 的 lifecycle 输出保持 canonical。
- 本轮补充：sidechain runtime 现在会拒绝同一 sidechain ID 的 running 状态重复 start；已完成后重新 start 会被视为新 lifecycle，state loader 会清空上一轮 summary/endedAt 并使用新的 startedAt。
- 本轮补充：sidechain/subagent lifecycle content 读取现在递归解包 `payload`/`data`/`body`/`result`/`response`/`metadata` 等 wrapper，并从嵌套 start event 中恢复 agent type、workspace path 和 task description。
- 本轮补充：sidechain/subagent lifecycle content 读取现在也递归解包 GraphQL/JSON:API 风格的 `edge`/`node`/`attrs` wrapper，wrapped start/summary event 可继续恢复 ID、status、summary 和 agent metadata。
- 本轮补充：sidechain/subagent lifecycle content 的 ID 等 string-like 字段现在接受 JSON number/数字标量，numeric subagent ID 会保留为字符串并能通过 resume fallback 找回对应 sidechain。
- 本轮补充：interaction script 的 `message` step 接受 `type`/`speaker` role 别名和 `content`/`body`/`message` text 别名；`image` step 和 iTerm2 image hint 接受 `fileName`/`file_name`/`name`、`mimeType`/`mime_type`/`contentType`、source path/URL 和 `data`/`base64` 内容别名；permission request step 接受 request/permission/tool-use ID、path、description 和 action 字段别名，并允许 `actions` 使用单字符串；`expectPrompt` 接受 `value`/`input`/`content`/`message`、`expandedText`/`fullText`、`cursorIndex`/`cursorPosition`、`isEmpty`/`blank` 等字段别名，且 `pastedContents` 断言接受 `pastedId`/`pastedContentId`、`kind`/`pastedType`、`value`/`data`/`base64`、`contentType`/`mimeType`、`fileName`/`name` 和 `contains` 等字段别名；`expectVim` 接受 `vimEnabled`/`isEnabled`、`vimMode`/`modeName`/`currentMode`、`vimRegister`/`registerValue`/`yankRegister`、`registerLinewise`/`linewise` 等字段别名；`expectTasks` 接受 `taskCount`/`total`/`size`/`length` 和 `statusCounts`/`countsByState` 等字段别名；`expectScreen`/`expectViewport` 接受 `columns`/`rows`、`screenWidth`/`screenHeight`、`scrollOffset`/`viewportOffset`、`visibleRows`/`lineCount` 等字段别名；`expectReverseSearch` 接受 `isActive`/`visible`/`open`、`search`/`term`/`pattern`、`cursorIndex`、`currentResult`、`matchCount`/`matches`、`noMatches` 等字段别名；`expectDialog` 可断言 body contains/not-contains、actions/action contains/not-contains、action count 和 focused action，runtime-aware scripts 会在步骤间保留 dialog focused action，且接受 `isActive`/`visible`、`dialogId`/`dialogID`、`dialogKind`、`heading`/`header`、`content`/`text`/`message` 等字段别名；`expectEvent`/`expectDialogResult` 接受 `eventType`/`event`/`name`、`payload`/`text`/`message`、`dialogId`/`dialogID`/`dialogKind`、`actionValue`/`resultStatus`/`exists`/`isStale` 等字段别名。
- 本轮补充：interaction script step 接受 `resize`/`terminalSize`/`screenSize` 对象或 `[width,height]` 数组、顶层 `columns`/`rows` resize 别名、`focus`/`focused`/`blur`/`focusIn`/`focusOut` focus event 别名、`snapshot`/`snapshotId`/`snapshotLabel` capture 名称别名，以及 runtime-aware mutation 别名如 `permission`/`permissionRequest`、`task`/`taskStatus`、`removeTask`/`deleteTask`、`cancelPermission`、`cancelTasks`/`cancelReason`、`openTasks`/`showTasks`。
- 本轮补充：interaction script step 可通过 `status`/`setStatus`/`statusLine`/`baseStatus` 设置状态行；runtime-aware scripts 会把它作为 base status，并继续叠加 permission/task 计数，方便复用带 status line 的 ANSI/interaction fixture。
- 本轮补充：interaction script 批量消息注入接受 `messages`、`append_messages`/`appendMessages`、`transcript_messages`/`transcriptMessages` 字段，且这些字段可用单对象或对象数组，复用 chat/transcript role/text 别名；message 注入也接受 `pastedContent`/`attachments` 粘贴内容别名、单数 `imagePasteId` 图片引用别名，以及 pasted-content 的 `kind`/`value`/`data` 内容字段别名。
- 本轮补充：interaction script direct `dialog` step 接受 `dialogId`/`dialogKind`、`heading`/`header`、`content`/`text`/`message`、`options`/`choices`/`buttons`、`focusedIndex`/`selectedIndex` 等字段别名，且 actions/options 可用单字符串。
- 本轮补充：interaction script loader 接受 `scriptSteps`/`script_steps`、`interactionSteps`/`interaction_steps` wrapper 字段，并支持一层 `scenario`/`test`/`case`/`fixture`/`interaction` 嵌套对象。
- 本轮补充：interaction script loader 现在也接受 JSON:API/resource-style `resource`/`attributes`/`properties` wrapper，可从 attributes/properties 内继续解析 `steps`/`records`/`timeline`，减少官方 fixture API envelope 的改写成本。
- 本轮补充：interaction script 的单个 step item 现在也接受 JSON:API/resource-style `resource`/`node`/`attributes`/`properties` wrapper，数组元素和 JSONL 行可直接使用 API fixture 的 step resource 形态。
- 本轮补充：interaction script loader 现在把 `data`/`payload`/`body`/`result`/`response`、`resources` 和 `nodes` 中的数组也视为 step list，可直接加载 API/GraphQL collection envelope，同时保留单步 `data` 载荷兼容。
- 本轮补充：interaction script loader 现在接受 GraphQL connection 风格的 `edges` step list 和 JSON:API/HAL collection 风格的 `included`、`collection`/`list`/`children`/`values` step list，数组元素可用 `edges[].node`、`edge.node`、`resource.attributes` 或 `resource.properties` wrapper，外层也可递归解包 `viewer`/`node`/`*Connection` wrapper 来加载录制脚本。
- 本轮补充：interaction script loader 现在也接受 provider-style `choices`/`outputs`/`candidates`/`generations` response wrapper，可从 `message.content`、content-block array 和 `content.parts[].text` 里恢复 script JSON，减少模型/SDK 录制脚本响应的手工拆包。
- 本轮补充：ANSI snapshot corpus 比对支持 `.ansi` only baseline fallback，strict stale-baseline 检查同时覆盖 `.txt` 和 `.ansi`。
- 本轮补充：interaction script JSONL loader 单行上限提升到 50MiB，和 transcript/session 大记录读取容忍度对齐，覆盖大型 paste、image metadata 或 snapshot fixture 脚本行。
- 本轮补充：terminal lifecycle 增加可选 extended-key mode，按官方 `CSI >1u`/`CSI >4;2m` 启用 kitty keyboard protocol 和 modifyOtherKeys，退出时重置 modifyOtherKeys 并 pop kitty stack，reassert 时先 pop 再 push 以避免长期会话 stack 泄漏。
- 本轮补充：renderer/snapshot 增加 opt-in DEC 2026 synchronized output 包裹入口，可用官方 BSU/ESU (`CSI ?2026h`/`CSI ?2026l`) 生成整帧 ANSI fixture，同时默认渲染保持不变。
- 本轮补充：terminal OSC helper 增加 OSC 0 title/icon 序列生成，输入会先 strip ANSI；`StripANSI` 现在会完整跳过 OSC/DCS/APC/PM/SOS payload，避免 title/snapshot 可见文本被终端控制串污染。
- 本轮补充：terminal OSC helper 增加 OSC 21337 tab status 序列、清理序列和 tmux/screen passthrough 包裹，status 文本按官方规则转义分号和反斜杠。
- 本轮补充：terminal OSC helper 增加 OSC 8 hyperlink 开始/结束序列，按官方 rolling hash 为 URL 自动生成 `id=`，并允许显式 params 覆盖。
- 本轮补充：terminal OSC helper 增加 OSC 9;4 progress 序列，覆盖 clear/set/error/indeterminate，running/error 百分比按官方规则 clamp 到 0..100。
- 本轮补充：terminal OSC helper 增加 iTerm2、Kitty、Ghostty notification 序列和 raw BEL helper，调用方可按环境选择是否 wrap multiplexer。
- 本轮补充：terminal OSC helper 增加 OSC 52 clipboard 序列生成，支持默认 `c` selection、显式 clipboard selection 和 clear 序列，并按 UTF-8 base64 编码 payload；native clipboard/tmux buffer runtime 仍未接入。
- 本轮补充：terminal OSC helper 增加显式 ST (`ESC \\`) terminator 入口，可按官方 Kitty 避免 BEL 的路径生成 OSC 序列，同时默认 `OSCSequence` 仍保持 BEL terminator。
- 本轮补充：terminal OSC helper 增加 OSC color parser，支持 `#RRGGBB` 和 XParseColor 风格 `rgb:R/G/B`，按官方规则把 1-4 位 hex component 缩放到 8-bit RGB。
- 本轮补充：terminal OSC parser 把 OSC 10-19 dynamic color 设置/查询解析为结构化 color action，复用既有 OSC color parser，并支持同一序列内按官方递增规则连续设置多个 target；visible text/snapshot 继续剥离这些控制串。
- 本轮补充：terminal OSC parser 把 OSC 110-119 dynamic color reset 序列解析为结构化 color reset action，覆盖前景/背景/光标、pointer、Tektronix 和 highlight 动态色 reset。
- 本轮补充：terminal OSC parser 把 OSC 4 palette color 设置/查询和 OSC 104 palette reset 解析为结构化 palette action，支持同一序列内多组 index/color、index/? 和按 index reset。
- 本轮补充：terminal OSC parser 把 OSC 5 special color 设置/查询和 OSC 105 reset 解析为结构化 specialColor action，限制 special index 为 0-4，并保持 visible text/snapshot 清洁。
- 本轮补充：terminal OSC helper 增加 OSC 21337 tab-status payload parser，支持 `\;`/`\\` 转义、bare key 或空值清理、unknown key ignore，并复用 OSC color parser 解析 indicator/status-color。
- 本轮补充：terminal OSC helper 增加 OSC 8 hyperlink payload parser，按官方规则解析 params、保留 URL 内部分号，并把空 URL 识别为 hyperlink end。
- 本轮补充：terminal OSC helper 增加轻量 `ParseOSCContent`，覆盖官方 title(0/1/2)、OSC 8 hyperlink、OSC 21337 tab status 和 unknown action 分支。
- 本轮补充：terminal OSC helper 增加完整 OSC sequence parser，可从带 `ESC ]` 前缀且以 BEL 或 ST 终止的序列解析出 `ParseOSCContent` action。
- 本轮补充：terminal OSC parser 把 OSC 52 clipboard、iTerm2 progress/notification、Kitty notification 和 Ghostty notification 作为结构化 terminal actions 暴露，visible text/snapshot 继续正确剥离这些控制串。
- 本轮补充：terminal OSC parser 把 OSC 133/633 shell integration marks (`A`/`B`/`C`/`D`) 作为结构化 shellIntegration action 暴露，保留 command-end exit code，visible text/snapshot 继续剥离这些 shell 标记。
- 本轮补充：terminal OSC parser 现在识别 VS Code OSC 633 `E` command-line 和 `P` property 记录，保留 raw value，并把 semicolon property payload 解析成结构化 metadata，visible text/snapshot 继续剥离这些 shell 标记。
- 本轮补充：terminal OSC parser 现在解析 OSC 7 current-directory URI，保留 raw URI 并暴露 scheme/host/path，TerminalParser 会输出 directory action，visible text/snapshot replay 继续剥离该控制串。
- 本轮补充：terminal renderer constants 增加官方 clear scrollback (`CSI 3J`) 和 legacy Windows home (`CSI 0f`) 序列 helper，支持现代 clear-screen+scrollback 和 legacy Windows clear 组合；平台自动探测仍留给调用方。
- 本轮补充：terminal CSI helper 增加通用 `CSISequence`、cursor up/down/forward/back/position/move 和 line/screen erase 序列，按官方 helper 的零移动返回空串与 horizontal-first cursorMove 行为生成 ANSI。
- 本轮补充：terminal CSI helper 增加 scroll up/down、set scroll region 和 reset scroll region 序列，scroll 零值返回空串，便于后续补齐官方 viewport/scroll-region 输出路径。
- 本轮补充：terminal CSI helper 增加 DECSCUSR cursor-style 序列，覆盖 block/underline/bar 的 blinking 与 non-blinking code，并保留 unknown style 的默认 cursor fallback。
- 本轮补充：terminal CSI helper 增加 bracketed paste start/end 和 focus in/out 输入 marker 常量，并用现有 parser 验证 focus marker 映射，方便官方交互 fixture 复用原始 CSI marker。
- 本轮补充：terminal CSI helper 增加 `EraseLinesSequence(n)`，按官方 `eraseLines` 语义逐行 `CSI 2K`、行间上移并以 `CSI G` 回到列 1，`n<=0` 返回空串。
- 本轮补充：terminal CSI helper 增加官方 CSI param/intermediate/final byte range 常量和判定函数，为后续更完整 CSI parser/action tests 提供基础。
- 本轮补充：terminal CSI helper 增加官方 CSI final-byte/DEC mode 常量和 `ParseCSISequence` 动作解析，覆盖 cursor move/position/save/restore/show/hide/style、erase display/line/chars、scroll up/down/region、SGR params、alternate-screen/bracketed-paste/mouse/focus mode 和 unknown sequence fallback。
- 本轮补充：terminal CSI parser 现在支持多参数 mode set/reset 序列，例如 `CSI ?1000;1006;2004h` 和 `CSI 4;20l`，在保持单 `Mode` 兼容字段的同时通过 `Modes` 暴露完整 mode 列表。
- 本轮补充：terminal CSI parser 现在对混合 cursor visibility 和 mode 的多参数序列（如 `CSI ?25;1000h`）保留完整 mode list，避免真实终端初始化/恢复序列只暴露 cursor action 而丢失后续 mode。
- 本轮补充：terminal CSI parser 补齐 insert/delete chars、insert/delete lines、forward tab/back tab action，`CSI M` 在 output parser 中按 delete-lines 处理，同时 input tokenizer 仍保留 X10 mouse payload 边界消费。
- 本轮补充：terminal CSI parser 现在把 DSR (`CSI n`) 解析成 report action，覆盖 device-status、cursor-position 和 private-mode unknown report，避免 terminal status query/response 序列继续落入 generic unknown。
- 本轮补充：terminal CSI parser 现在把 DEC `?1006h/l` SGR mouse mode 解析成 mouseTracking action，和 lifecycle 发出的 SGR mouse enable/disable 序列闭环。
- 本轮补充：terminal CSI parser 现在把 DEC `?9h/l` X10 mouse tracking mode 解析成 mouseTracking `x10` action，和 input tokenizer/parser 的 X10 mouse payload 支撑闭环。
- 本轮补充：terminal CSI parser 现在也把 DEC `?1001h/l` highlight、`?1005h/l` UTF-8 mouse mode、`?1015h/l` urxvt numeric mouse mode 和 xterm `?1016h/l` SGR-pixels mouse mode 解析成 mouseTracking action，和输入侧 numeric/SGR mouse 兼容面闭环。
- 本轮补充：terminal CSI parser 现在把 xterm `?1007h/l` alternate scroll mode 解析成独立 mode action，避免 alternate-screen wheel 兼容序列落入 unknown。
- 本轮补充：terminal CSI parser 现在把 DEC `?2026h/l` synchronized output mode 解析成 mode action，和 renderer/snapshot 的 BSU/ESU 包裹路径闭环。
- 本轮补充：terminal ESC helper 增加官方 ESC final-byte 判定和 `ParseESCSequence`/`ParseESCContent`，覆盖 RIS reset、DECSC/DECRC save/restore、IND/RI/NEL cursor action、HTS、charset selection 和 unknown sequence fallback。
- 本轮补充：terminal SGR helper 增加官方 `TextStyle` 状态解析基础，覆盖 reset、bold/dim/italic/underline/blink/inverse/hidden/strikethrough/overline、普通/亮色命名色、256 色、RGB 色、underline color、分号和冒号参数格式；完整 ANSI parser/render style 应用仍继续推进。
- 本轮补充：terminal sequence dispatcher 增加官方 `identifySequence` 等价分流，按 CSI/OSC/ESC/SS3/unknown 识别并委派现有 parser，SS3 按官方 output parser 作为 unknown action；streaming tokenizer 和 text grapheme action 仍继续推进。
- 本轮补充：terminal tokenizer 增加官方 streaming escape boundary 状态机，支持跨 chunk buffer/flush/reset、CSI/SS3/OSC/DCS/APC 序列边界、OSC BEL/ST terminator、ESC intermediate charset 序列、invalid CSI text fallback 和 opt-in X10 mouse payload 消费；完整 text grapheme action parser 仍继续推进。
- 本轮补充：terminal parser 增加轻量 ANSI action pipeline，串接 tokenizer、CSI/OSC/ESC dispatcher 和 SGR style state，输出 text/bell/cursor/erase/scroll/mode/title/link/tabStatus/reset/unknown action，文本宽度覆盖 ASCII、emoji 和 East Asian wide；完整 grapheme cluster segmentation 与 renderer style 应用仍继续推进。
- 本轮补充：terminal parser 跟踪 OSC 8 hyperlink start/end 状态，暴露当前 `inLink` 和 `linkUrl`，reset 时清空链接状态，贴近官方 parser 的 link range 状态语义。
- 本轮补充：terminal parser 的 text grapheme 基础分段补齐 combining mark、variation selector、emoji modifier、ZWJ emoji 序列和 regional indicator flag pair；宽度计算现在让 base+combining-mark cluster 保持 base glyph 宽度，emoji presentation/ZWJ/flag 仍按宽 grapheme 处理，完整 Unicode UAX #29 分段仍未宣称完成。
- 本轮补充：terminal parser 的 streaming text action 现在会暂存可能跨 chunk 延续的末尾 grapheme；ZWJ emoji、regional indicator flag 和未完成 emoji tag sequence 跨 `Feed()` 边界时不会被拆成两个宽字符，遇到控制序列或 `Flush()` 会先落地 pending text。
- 本轮补充：terminal parser 的 text grapheme 分段继续补齐 emoji tag sequence，把 subdivision flag 这类 base emoji + tag chars + cancel tag 作为单个宽 grapheme，避免 wrap/pad/cursor pipeline 拆分视觉 emoji。
- 本轮补充：terminal CSI parser 现在对 tokenizer flush 出来的非 final-byte incomplete CSI 返回 unknown action，而不是丢弃，贴近官方 `parseCSI` 对 flushed partial sequence 的 fallback 行为。
- 本轮补充：terminal sequence dispatcher 对 tokenizer flush 出来的 OSC partial sequence 使用 `ParseOSCContent` fallback，允许无 BEL/ST terminator 的 title/link/tab-status content 按官方 parser 语义产出 action。
- 本轮补充：terminal tokenizer 增加明确的 output/input 构造器，output 路径默认不吞 `CSI M` 后续字节，input 路径默认开启 X10 mouse payload 边界消费，避免调用方误用布尔选项导致 output parser 吞文本或 stdin mouse payload 泄漏。
- 本轮补充：mouse parser 接受 urxvt/xterm 1015 numeric mouse `CSI button;x;yM`，按 legacy offset 还原 button code，左键、释放和滚轮语义与 SGR/X10 mouse 保持一致。
- 本轮补充：SGR mouse parser 现在拒绝负 button 和 0/负坐标，和 URXVT/X10 parser 的坐标下界 guard 对齐，避免无效 terminal mouse packet 触发点击/滚动事件。
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
