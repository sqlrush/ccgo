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
| 文件工具 M5 初版 | 已完成文本版 `Read`、image Read 初版、notebook Read 初版、Read 大文本预算截断/落盘、`Write`、`Edit`，含读前写、mtime stale guard、`replace_all`、structured diff、`.claude/settings*.json` 写前校验、team-memory secret guard、Read 去重 |
| M6 初始上下文层 | 已完成 CLAUDE.md/memdir 扫描、memory manifest、team-memory secret guard、compact threshold/prompt/runner/boundary plan、conversation auto-compact 接入、失败熔断、microcompact/cache 初版、persistent cached microcompact 初版、cache digest structural/rich-content metadata 覆盖、cache version/TTL/prune、in-memory micro cache prune、memory-cache write-through 到磁盘、atomic cache write、坏缓存默认 fail-open、session memory summary/frontmatter aliases/recall 初版、model-ranked recall session-id selection 和 invalid-selection fallback 初版、recall agent alternate/camel response keys/fenced-prose JSON extraction/scalar id parsing/nested/wrapped/collection-alias selection parsing、resume context model-assisted recall 接入、session memory rollup/prune compaction、rollup archive exclusion/merge、rune-safe rollup truncation、resume context + session memory recall、conversation recall 注入、deterministic/model-backed memory extraction 初版、extraction agent fenced-prose JSON/wrapped facts/provider-style response wrapper/alternate/structured field/nested source object/nested response/fact kind alias parsing、turn-end memory extraction 落盘、prompt history lock/buffered flush/field aliases、official subagent transcript layout、agent metadata sidecar/field aliases、sidechain runtime start/append/finish/cancel/fail 和 parent-chain append/finish 初版、sidechain manager orchestration 初版、sidechain manifest 聚合、sidechain state/list/resume/content-field aliases 初版、sidechain resume context builder、sidechain conversation/content-replacement reconstruction、transcript tail/window/metadata/index loaders、byte-budget transcript tail loader、agent-scoped content replacement metadata/record field-alias loading、session-scoped metadata reappend including AI-title/last-prompt/task-summary、transcript line offset index/window/byte-budget-window/parent-chain/resume/tail/byte-budget-tail loaders、extended transcript metadata entries/type/field aliases、transcript message/session UUID field aliases including top-level `messageUuid`/`messageId`/`id` record IDs and `role`/`entry_type`/`messageType`/`createdAt` timestamp aliases、transcript tombstone metadata delete/relink、transcript resume conversation builder、index 文本预览和 AI-title/last-prompt/task metadata 字段、流式 transcript 搜索、session list pagination/search/title、remote history token refresh、remote history 全量分页抓取/page-field/event-list/records/entries/eventList/sessionEvents/last-id/cursor/event-id/has-next alias/wrapped-data/links/paging/bare-array/keyed event map/connection/eventConnection/sessionEventsConnection wrapper response/edge-cursor fallback/max-pages 截断状态/before_id 续抓、remote event transcript materialization/fallback field fill/去重追加/duplicate parent guard、remote history 一步 sync 到 transcript |
| M7 初始 TUI 层 | 已完成轻量 terminal frame renderer、PromptInput 状态机、history 导航、ctrl-p/ctrl-n history navigation、shift-enter 多行输入、多行 prompt 行内 ctrl-a/ctrl-e/ctrl-u/ctrl-k 和 wrap/render/cursor、共享 kill ring、ctrl-b/ctrl-f/ctrl-u/ctrl-k/ctrl-w 行编辑、alt-b/alt-f/alt-d/alt-backspace word 编辑、ctrl-left/ctrl-right/alt-left/alt-right word motion、ctrl-y yank 和 alt-y yank-pop 初版、reverse-search cursor/word 编辑/kill/yank/yank-pop 初版、ctrl-c interrupt/双击退出事件、ctrl-d delete-forward/空输入双击退出事件、ctrl-l 重绘事件、ctrl-o/ctrl-t 全局切换事件、ctrl-g/ctrl-s/ctrl-x chord chat 事件、reverse-search 状态/渲染/脚本断言/空结果/选择回填/cursor 断言、paste/image hint 输入和 OSC ST/base64 filename 兼容、text/image pasted-content 引用/metadata 脚本断言/提交展开/history entry restoration、SGR mouse 解析、alternate terminal navigation key sequences including modified Home/End/Delete/PageUp/PageDown、滚轮滚动、修饰键滚轮/左键、左键拖动选择、viewport 半页/顶部/底部可配置滚动、viewport 点击选择和 dialog action 点击、focus/blur 事件、resize 视口保持、keybinding resolver/config/chord pending/null-unbind/key/action camelCase alias、JSON config loader 和 focus/mouse/paste/image key name 覆盖、vim insert/normal/j/k/word/WORD/ge/gE/line-local ^/$/0/|/I/A/D/quote/bracket text-object/yank/register/paste/delete/count/replace/undo/find/till/repeat/matching-pair %/dot-repeat/G/gg/toggle/join/open-line/indent/substitute 动作、normal-mode arrow/backspace/delete 映射和 operator linewise/字符范围、REPL screen、permission/task dialog builder、dialog kind/id routing/runtime/status line、runtime 到 REPL screen 的 dialog/status 同步、runtime-aware interaction script runner、prompt text/cursor/expanded/vim mode/register/task state/dialog result/runtime mutation/task bulk-cancel/permission cancel/keybinding mutation/status negative/snapshot negative/screen size/event-sequence/event-count/no-event/dialog-result-count/no-dialog-result 脚本断言、viewport 脚本断言、named-key 脚本输入、script JSON/JSONL/wrapper loader、script file runner 和 runtime/task camel field aliases、stale dialog race guard、cancel active、permission id/all cancellation、queued permission promotion、active task dialog refresh、task lifecycle/bulk-cancel 初版、idempotent alternate screen lifecycle/reset/reassert interactive 初版、mouse/focus/bracketed-paste terminal mode lifecycle/reconciliation、ANSI snapshot 基础、snapshot corpus write/compare/script-file compare/missing-baseline/diff/batch/strict unexpected-baseline 状态、scripted interaction runner/assertions/multi-key/text/paste/image/pasted-content metadata 初版、status/dialog/message components、viewport/selection |
| 全量测试 | 当前 `go test ./...` 通过 |

M7 补充：interaction script paste payload 现在接受 ClipboardItem 风格的 `items[].getAsString`/`get_as_string` 以及 `stringData`/`textData` 文本字段，DOM clipboard 录制脚本可直接恢复 pasted text。

M7 补充：scripted task runtime payload 和 task expectation 现在接受 `taskID`、`jobId`、`runId`、`label`、`displayName`、`phase`、`taskState`、`message`、`currentStep`、`percent`/`percentage`/`pct` 等相邻字段，并支持数字 task ID 与数字字符串 progress。

M5 补充：Bash/BashOutput 现在接受 `timeout`、`run_in_background`/`runInBackground`、`tail_lines`/`tailLines` 的 quoted semantic string 输入，和官方 SDK 常见的 number/boolean 宽松输入保持一致。

M5 补充：PowerShell/PowerShellOutput 现在接受 `timeout`、`run_in_background`/`runInBackground`、`tail_lines`/`tailLines` 的 quoted semantic string 输入，和官方 PowerShell tool schema 的 semantic number/boolean 行为对齐。

M5 补充：Read/Edit 现在接受 `offset`/`limit` 和 `replace_all` 的 quoted semantic string 输入；whole-decimal 数字字符串如 `"2.0"` 会按官方 `semanticNumber(...int())` 语义归一为整数，fractional 数字仍会被拒绝。

M5 补充：Bash/PowerShell 现在会在前台模式阻断首个语句中的长 `sleep`/`Start-Sleep`（2 秒及以上）并提示使用 `run_in_background`；短 sleep、浮点 sleep、`Start-Sleep -Milliseconds` 和显式后台执行保持允许。

M5 补充：Bash/PowerShell 现在接受官方 `dangerouslyDisableSandbox` semantic boolean 输入，并在 structured content 中记录该请求；真实 sandbox adapter/override 行为仍按 sandbox parity 项继续推进。

M5 补充：Bash/PowerShell 的 `dangerouslyDisableSandbox` 现在会进入权限引擎；除可用的 `bypassPermissions` 模式外，sandbox override 会要求确认，`dontAsk` 模式下会拒绝，避免显式 allow rule 或 read-only 分类静默放行 sandbox override。

M5 补充：settings 中的 `sandbox.allowUnsandboxedCommands` 现在会进入 permission context；该值为 `false` 时会拒绝 `dangerouslyDisableSandbox`，即使处于 `bypassPermissions` 模式也不会放行，同时 settings validation 会标记该 sandbox 布尔字段的非 bool 值。

M5 补充：settings 中的 `sandbox.filesystem.allowWrite`/`denyWrite`/`denyRead`/`allowRead` 现在会合并到 permission context 并参与路径权限判定；`denyRead` 可被更窄的 `allowRead` 放行，`denyWrite` 会阻断写入，`allowWrite` 可在危险根路径和敏感文件安全检查之后放行额外写目录，同时修正 request cwd-relative path 展开顺序。

M5/M8 补充：permission internal path context 现在支持 `SkillDirs`，Runner 会把配置的 skill/bundled-skill 目录透传到工具 metadata，使这些目录下的 `SKILL.md` 和资源文件可作为内部路径读取；写入仍不被该 allowlist 放行，完整 skill discovery/activation/SkillTool 仍按 M8 缺口推进。

M8 补充：新增 `internal/skills` discovery 基础模块，支持从工作目录向上到 git root/home 发现项目 `.claude/skills/<skill>/SKILL.md` 目录，并支持按文件路径动态发现 cwd 以下更深层 `.claude/skills`；Runner 现在会把工作目录发现到的项目 skill roots 自动加入 tool metadata 的只读 allowlist。

M8 补充：Read/Write/Edit/NotebookEdit 现在会在处理文件路径时触发嵌套 skill directory discovery，并把新发现的 skill roots 追加到共享 tool metadata 的内部只读路径上下文，后续工具可读取对应 `SKILL.md` 和资源文件；完整 skill activation、SkillTool 调用和 UI 展示仍未完成。

M8 补充：`internal/skills` 现在可以加载目录式 `<skill>/SKILL.md`，解析基础 frontmatter 并生成 prompt command 元数据，包括 display name、description fallback、allowed-tools、argument-hint、arguments、when_to_use/when-to-use、version、model（含 inherit 归一为无覆盖）、context: fork、agent、effort、paths、content length、user-invocable hidden 状态和 disable-model-invocation；项目 skill commands 可按现有 discovery 顺序导出，但 slash command 注册、SkillTool 调用和 UI 激活仍未完成。

M8 补充：新增 `internal/commands` registry 基础层，按官方 `getCommands(cwd)` 的来源顺序合并 bundled/builtin-plugin/project-skill/workflow/plugin/dynamic/builtin command metadata，支持 dynamic skill 去重、display-name/alias 查找、hidden 可见性过滤、SkillTool model-invocable command 过滤、slash-command skill 过滤和 bridge-safe command 判定；当前仍是 metadata/registry 层，local/local-jsx 实际执行、`/help`/`/skills` UI、plugin/MCP/workflow 来源接入仍未完成。

M8 补充：command registry 现在保存本地 skill prompt template，并提供 `ExpandPrompt` 基础调用入口，可按 name/display-name/alias 展开 prompt command，执行 `$ARGUMENTS`、`$ARGUMENTS[n]`、`$n` 和 frontmatter `arguments` named placeholder 替换，注入 `${CLAUDE_SESSION_ID}`，对非 MCP skill 替换 `${CLAUDE_SKILL_DIR}`，并生成 official shape 的 meta user message；shell command injection、SkillTool wrapper、local/local-jsx 执行和 REPL/UI wiring 仍未完成。

M8 补充：新增基础 `Skill` tool wrapper，已注册到默认内置工具集，可按官方 `skill`/`args` 输入调用项目目录发现到的本地 prompt skill，并兼容 `commandName`/`arguments` 别名；tool result 会返回 `Launching skill: ...`、structured command metadata 和 prompt expansion 生成的 meta user message，conversation runner 现在会把 `ToolResult.NewMessages` 追加进后续模型请求和 transcript。forked skill、remote/MCP/plugin skill、skill prompt shell injection、slash/local command UI wiring 仍未完成。

M8 补充：新增基础 slash command parser/executor，按官方 `/command args` 与 `/mcp:tool (MCP) args` 形态解析，并把本地项目 prompt skill slash 调用接入 conversation runner：`/skill args` 会生成 `<command-name>/<command-message>/<command-args>` metadata user message 和展开后的 meta prompt message，写入 transcript/parent chain 后再请求模型；skill frontmatter `model` 可覆盖本轮请求模型。local/local-jsx 命令目前只返回未实现输出，不会误发给模型；command permissions attachment、forked skill、MCP/plugin/bundled slash 来源和 UI 仍未完成。

M8 补充：本地 prompt skill 的 slash 调用和 `Skill` tool 现在都会生成 `command_permissions` attachment，按官方 `allowed-tools` 解析 comma/space 分隔且保留括号内模式；conversation runner 会在当前 turn 内把这些 `PermissionSourceCommand` allow rules 合并进 engine permission decider，让 skill frontmatter 授权的后续工具调用可在同一轮放行，并继续保留 model override attachment metadata。完整权限 UI 展示、SDK event surface、forked/MCP/plugin/bundled skill 权限继承仍未完成。

M8 补充：skill frontmatter 标量兼容继续补齐，`allowed_tools`/`argument_hint`/`disable_model_invocation`/`user_invocable`/`when-to-use` 等相邻字段会映射到 canonical command metadata；`model: inherit` 不再误触发模型覆盖，`context: fork`、`agent`、`effort` 会保留在 command contract 中，为后续 forked skill/agent 执行接线提供 metadata。

M8 补充：project legacy `.claude/commands/**/*.md` 现在会加载为 `commands_DEPRECATED` prompt command，并支持目录式 `SKILL.md` 命名空间（例如 `team/deploy/SKILL.md` -> `team:deploy`）、普通 markdown 命名空间、frontmatter metadata、SkillTool 可见性过滤和 prompt expansion；目录式 legacy command 会保留 base directory 前缀和 `${CLAUDE_SKILL_DIR}` 替换，完整 user/managed commands、plugin commands、local/local-jsx 执行仍未完成。

M8 补充：现有 Go 内置 slash command metadata 继续贴近官方源快照，补齐 `config`/`resume`/`clear` 的 aliases（`settings`、`continue`、`reset`、`new`），以及 `mcp`/`resume`/`model` 的 argument hint、`mcp`/`status`/`model` 的 immediate 标记和部分官方描述；大量内置 command 的真实 local/local-jsx UI 执行仍未完成。

M8 补充：slash command 现在有基础 local command result 抽象，`/clear` 不再落入 unsupported 分支，会生成 local text result、保留 command metadata message，并且不会请求模型；完整 REPL conversation reset、local command text/compact/skip 全语义、`/cost`/`/status`/`/compact` 和 local-jsx UI 执行仍未完成。

M5 补充：WebFetch/WebSearch 现在接受本地数值参数的 quoted semantic string 输入，包括 `timeout`、`max_bytes`/`maxBytes` 和 `max_results`/`maxResults`；WebSearch 现在也按官方行为拒绝同一请求同时设置 `allowed_domains` 和 `blocked_domains`。

M7 补充：scripted permission payload、dialog expectation、event、cancel-permission 和 dialog-result expectation 现在接受 `ID`/`ToolName`/`Actions`、`permissionID`、`requestID`、`toolUseID`、`operationID`、`operation`、`commandName`、`resourcePath`、`body`、`reasonText`、`allowedActions`、`buttons` 等相邻字段，并支持数字 request ID。

M6 补充：microcompact disk cache loader 和 prune 现在接受 digest 缺失但文件名已 keyed 的 cache entry，会用 `<digest>.json` 文件名作为 digest fallback，同时保留显式 digest mismatch 的 invalid-cache guard。

M6 补充：microcompact disk cache loader 的 `cached`/`fromCache`/`cacheHit`/`isCached` 布尔字段现在接受 JSON bool、`true`/`false`、`yes`/`no`、`on`/`off`、`1`/`0` 数字/字符串形态，以及 whole-number 数字字符串如 `"1.0"`/`"0.0"`。

M6 补充：microcompact disk cache loader 现在接受 JSON:API/resource-style `resource`/`attributes`/`properties` wrapper，summary payload 可放在 attributes/properties 内，外层 resource `id` 可作为 digest fallback。

M6 补充：microcompact disk cache loader 现在也递归解包 GraphQL-style `viewer`/`edge`/`node`/`attrs` wrapper，node `id` 可作为 digest fallback，attrs/properties 内的 summary、version、timestamp 和 TTL aliases 都会恢复。

M6 补充：microcompact disk cache loader 的 summary-like payload 现在接受 text content-block object、text content-block array 和 string array，会把可见 text block 合并为 summary，兼容官方/SDK 响应内容块形态的 cached microcompact 文件。

M6 补充：microcompact disk cache loader 的 summary-like payload 现在也接受完整 contract message object，并会递归解包 `message`/`assistantMessage`/`resultMessage`/`outputMessage`/`completionMessage` wrapper，从 message content 中恢复 visible text summary。

M6 补充：microcompact disk cache loader 的 summary array 元素现在也接受完整 contract message object，可把 message list 与 content-block 混合数组恢复成可见摘要文本。

M6 补充：microcompact disk cache loader 现在会把 `value` 字段中的 text content-block object 识别为 direct summary payload，同时继续从同一 `value` object 中补 digest、version、timestamp 等 cache metadata，避免 `value` 作为 summary/cache wrapper 双义字段时丢摘要或 sidecar 信息。

M6 补充：microcompact disk cache loader 的 relative TTL 现在接受分钟、小时和天级字段别名，包括 `ttlMinutes`、`expiresInHours`、`validForDays` 及 snake/camel 相邻形式，恢复 cached microcompact 过期时间时不再只依赖秒/毫秒字段。

M6 补充：microcompact disk cache loader 的 relative TTL 字符串现在接受固定单位 ISO-8601 duration，例如 `PT1H30M`、`P1D`、`P1DT2H`，同时仍拒绝年/月这类长度不固定的 duration，避免 cache expiry 产生歧义。

M6 补充：sidechain/subagent lifecycle state loader 现在接受 `subagent_started`、`agentStarted`、`task_failed`、`sidechainCompleted` 等相邻 subtype aliases，支持 `taskID`/`workerId`/`runId`、`agentName`/`kind`、`resultText`/`finalMessage` 等字段，并在 failed/cancelled subtype 没有显式 status 时自动归一状态。

M6 补充：sidechain/subagent lifecycle content 读取现在接受 JSON:API/resource-style `resource`/`attributes`/`properties` wrapper，外层 resource `id` 可作为 sidechain ID fallback，内层 agent metadata、status/outcome 和 summary 字段仍能恢复到 state/list/resume。

M6 补充：sidechain/subagent lifecycle content 读取现在也递归解包 GraphQL/JSON:API 风格的 `edge`/`node`/`attrs` wrapper，wrapped start/summary event 可继续恢复 ID、status、summary 和 agent metadata。

M6 补充：sidechain/subagent lifecycle status 归一化现在接受 compact/camel aliases，包括 `inProgress`、`completedSuccessfully`、`cancelledByUser`/`canceledByUser`、`failedError`/`failedWithError` 和 `timedOut`，并保持 transcript/runtime 输出为 canonical running/completed/cancelled/failed。

M6 补充：transcript resume 的嵌套 content block 现在接受 `toolUseId`/`toolUseID`、`isError`、`cacheControl`、`cacheReference` 字段别名，并保留 cache edit 的 `cacheReference`。

M6 补充：transcript resume 的 nested content block `id`/`tool_use_id`/`toolUseId` 现在接受 JSON number，并保留为字符串 tool-use ID。

M6 补充：嵌套 contract message 的 `content` 现在接受字符串、单个 content-block 对象，以及混合字符串/content-block 数组；字符串会归一为 text block，并接受 `text`/`body`/`message`/`value`/`output` 正文字段和 `role`/`messageType` 类型别名，提升 transcript/remote history payload 恢复率。

M6 补充：嵌套 contract message 现在接受 `parentUUID`、`parentId`/`parentID`/`parent_id`、`parentMessageId`/`parentMessageID`/`parent_message_id` 和 parent-message UUID 别名，transcript/remote history payload 自带 parent alias 时不会丢失嵌套 parent。

M6 补充：indexed resume chain 现在区分 byte budget 截断掉的 parent 和 transcript 里真实缺失的 parent，bounded resume 可以暴露 `TruncatedParent` 与 `MissingParent` 两种断点。

M6 补充：嵌套 contract message 现在接受 `messageId`/`messageID`/`message_id` 和 `messageUuid`/`messageUUID`/`message_uuid` 作为自身 ID/UUID 别名，indexed resume 会保留 payload 自带的 nested message id。

M6 补充：嵌套 contract message 的 primary `id` 现在接受 JSON number，`LoadTranscript` 和 indexed resume 会保留为字符串 message id。

M6 补充：基础 `SessionEntry` JSONL loader 现在接受 `role`/`entryType`/`messageType`、message ID/UUID、parent ID/UUID 和 `sessionID`/`session`/session UUID 别名，旧 entry 文件可通过 `session.Load` 保留类型、parent 和 session。

M6 补充：tombstone metadata target/parent 现在接受 `targetId`/`deletedId`/`messageId` 和 `parentId`/`parentMessageId` 系列 ID/UUID 别名，删除/重连 replay 不会因旧字段拼写丢失 tombstone 目标或 parent。

M6 补充：transcript metadata 现在接受 summary `leafID`/message ID、content-replacement `agentID`/`toolUseID`/`blockID` 和 context-collapse `collapseID`/`summaryID`/archived ID 别名，metadata loader 与 full loader 保持同一兼容面。

M6 补充：transcript metadata 字段查找现在接受大小写、snake_case、kebab-case 和空格分隔形式归一，`Session-ID`、`Custom-Title`、`Pull-Request-Number` 等相邻字段可在 full loader、lightweight metadata 和 transcript index 中恢复同一 metadata。
M6 补充：transcript message/envelope 和嵌套 contract message 字段查找也复用大小写、snake_case、kebab-case 和空格分隔形式归一，`Message-Type`、`Message ID`、`Parent-Message-ID`、`Session-ID`、`Git-Branch`、`Message Text` 等字段可贯通 full loader、progress bridge、line index 和 indexed resume。
M6 补充：legacy session JSONL 的 `SessionEntry` 读取也复用同一 normalized 字段查找，`Entry Type`、`Message-ID`、`Parent Message ID`、`Session-ID`、`Created At` 等字段可经 `session.Load` 恢复旧 entry 与嵌套 message。
M6 补充：remote-history `SDKEvent` 读取也复用 normalized 字段查找，`Event Type`、`Event-ID`、`Parent Message ID`、`Created At`、`Message-Payload`、`Status Message`/`Failure Reason`/`Final Output` 等字段可恢复事件类型、ID、parent、时间戳、状态/错误/结果和 transcript materialization 所需 message。

M6 补充：worktree-state metadata 现在除 `worktreeSession`/`worktree_session` 外，也接受 `worktreeState`/`worktree_state`/`worktree`/`workspace` wrapper，full loader 和 lightweight metadata loader 都会保留旧 worktree payload。

M6 补充：PR link metadata 现在接受 `pullRequestNumber`/`pull_request_number`、`pullRequestURL`/`pull_request_url` 和 `repoFullName`/`repositoryFullName` 别名，full loader 和 lightweight metadata loader 都能恢复旧 PR metadata。

M6 补充：task-summary metadata 现在接受 `taskSummary`/`task_summary`/`content`/`text` 摘要别名和 `createdAt`/`created_at` timestamp 别名，旧任务摘要记录不会只保留 session id。

M6 补充：summary/custom-title/ai-title/last-prompt metadata 现在接受 `content`/`text`/`title`/`name`/`prompt` 等值字段别名，full loader、metadata loader 和 transcript index 的标题/摘要恢复保持一致。

M6 补充：tag、agent-name、agent-color、agent-setting 和 mode metadata 现在接受 `label`/`name`/`color`/`setting`/`status` 等值字段别名，full loader、metadata loader 和 transcript index 的 agent/session 状态恢复保持一致。

M6 补充：content-replacement metadata 现在接受 `records`/`contentReplacements` 等 record wrapper，以及 record 内 `type`/`content`/`hash` 等字段别名，full loader、metadata loader 和 transcript index 的 replacement 恢复保持一致。

M6 补充：content-replacement metadata 的 `agentId`、record `toolUseId` 和 `blockId` 现在也接受 JSON number，并在 full/lightweight loader 中保留为字符串 ID。

M6 补充：remote history GraphQL/connection 分页现在接受 `hasPrevious`/`hasPreviousPage`、`hasOlder`/`more` 继续分页标记，以及 `previousCursor`/`prevCursor`/`beforeCursor`/`olderCursor` 等 before-id cursor 别名，避免只返回第一页历史。

M6 补充：remote history pagination bool 字段现在除 JSON bool 和 `true`/`false` 字符串外，也接受 `1`/`0`、`yes`/`no`、`on`/`off` 等数值/字符串布尔形态，以及 whole-number JSON number 或数字字符串如 `1.0`/`"0.0"`，避免 wrapper/pageInfo 中的非严格布尔值中断分页。

M6 补充：remote history pagination cursor/id 字段现在接受 JSON number 并原样转成字符串，覆盖 `next_cursor` 等 page 字段和 `edges[].cursor` 的数字形态。

M6 补充：remote history pagination 现在接受 `nextPageToken`/`nextToken`/`pageToken`/`continuationToken` 及 snake_case 形式，响应字段和 link URL query 参数都会归一到续抓 before-id。

M6 补充：remote history pagination 现在也接受通用 `paginationToken`、`cursorToken` 和 `token` continuation aliases，覆盖响应字段、link object 和 link URL query 参数。

M6 补充：remote history pagination 现在也接受 `previousPageToken`/`prevPageToken`/`olderPageToken`、`previousToken`/`prevToken`/`olderToken` 及 snake_case 形式，响应字段、link object 和 link URL query 参数都会归一到续抓 before-id。

M6 补充：remote history pagination 现在也接受相邻 before-cursor aliases，包括 `before`、`beforeID`、`olderThan`、`endingBefore` 和 `untilId`，响应字段、link object 和 link URL query 参数都会归一到续抓 before-id。

M6 补充：remote history pagination 现在也接受 `hasMoreResults`/`hasMoreItems`/`hasMorePages`、`isTruncated`/`truncated` 等继续分页标记，以及 `nextKey`/`lastEvaluatedKey`/`lastKey` cursor 别名；响应字段和 link URL query 参数都会归一到续抓 before-id，覆盖 keyset/token 风格分页响应。

M6 补充：remote history pagination 现在也接受 OData next-link 字段 `@odata.nextLink`、`odata.nextLink` 和 `__next`，并从 `$skiptoken`/`skipToken` link query 参数提取续抓 cursor。

M6 补充：remote history fetch 现在把 HTTP 204 和 200 空 body 视为空的终止页，避免空历史响应被标成 incomplete 或触发 JSON EOF。

M6 补充：remote history fetch 现在把 HTTP 404/410 missing/deleted session response 视为空的终止页；5xx 等其它非 OK 响应仍保持 nil page/incomplete，用来区分“没有远端历史”和“暂时无法取证”。

M6 补充：contract `ID` JSON 读取现在接受 JSON number/null，remote history event/message/session/parent ID alias 可继承数字 ID 兼容面并在 transcript materialization 中保留为字符串。

M6 补充：remote history response parser 现在会递归解包 `data.session.events`、`data.projectSession.eventConnection`、`data.viewer.session.events`、`data.node.eventConnection`、`conversation`、`remoteHistory`、`_embedded` 等 GraphQL/session/HAL wrapper，继续复用 `nodes`/`edges[].node` 和 `pageInfo` pagination 解析。

M6 补充：remote history event-list 现在接受 `value`/`values`/`resources`/`collection` 别名，connection edge 也接受 `resource`/`value` 作为 node payload，覆盖 OData/HAL/resource collection 风格响应。

M6 补充：remote history response parser 现在会解包 `payload`/`response`/`result`/`body` 等通用响应外壳，外壳内的 event list、pagination、links 会继续递归解析。

M6 补充：remote history response parser 现在也接受 provider-style `choices`/`outputs`/`candidates`/`generations` wrapper，可从 `message.content`、content-block array 和 `content.parts[].text` 中恢复 event page JSON（包括 fenced `json` code block），并保留 pagination 继续驱动 `before_id` 续抓。

M6 补充：remote history provider wrapper 的 fenced JSON 提取现在接受 inline/glued fence 形态，例如语言标记后同一行直接跟 JSON 对象，模型输出不换行时仍能恢复 event page。

M6 补充：remote history pagination 现在接受 `starting_after`/`startingAfter`/`after*` cursor aliases，page 字段和 link URL query 都可驱动下一页 `before_id` 续抓。

M6 补充：remote history response parser 现在接受 JSON:API `included` collection，会过滤非事件资源，并递归解包 `resource`/`attributes`/`properties` 后用外层 resource id 作为事件 ID fallback。

M6 补充：remote history `data`/`payload`/`response`/`result`/`body` 等 event-list 字段现在也接受单个 SDK event 对象，避免非数组单事件页被当作普通 wrapper 后丢失。

M6 补充：remote history `SDKEvent` 本体现在接受 `eventType`/`event_type`/`role` 类型别名、`createdAt`/`created_at` 时间戳别名，以及 `payload`/`data`/`body`/`serializedMessage` message payload 别名；payload 只有 `role`/`content` 时也能 materialize 成 transcript message。

M6 补充：remote history `SDKEvent` 的 status/error/result 正文字段现在继续接受 `stateMessage`/`updateText`/`messageText`、`failureMessage`/`exceptionMessage`/`diagnosticMessage`、`summaryText`/`finalOutput`/`responseText` 等 provider/export aliases，并把 `summary`/`final` 作为 result object fallback。

M6 补充：remote history REST/link 风格分页现在接受 `links`/`_links` 的 `next`/`previous`/`prev`/`older` 字符串 URL、`{href,url,uri,link}` 对象，或直接携带 `cursor`/`beforeId`/`lastEvaluatedKey` 等 cursor 字段的 link object，并从 `before_id`、`beforeId`、`cursor`、`pageCursor`、`previousCursor`、`prevCursor`、`beforeCursor`、`olderCursor`、`startCursor`、`endCursor` 等 query 参数提取下一页 before-id。

M6 补充：remote history REST/link 风格分页现在也接受 RFC/JSON:API 风格的 `links` 数组，按 `rel`/`relation`/`name`/`type` 中的 `previous`/`prev`/`older`/`next` 选择续抓 URL 或 direct cursor item，并从同一组 before/cursor query 参数或 cursor 字段提取 before-id。

M6 补充：remote history 现在也接受 HTTP `Link` header 中 `rel="previous"`/`prev`/`older`/`next` 的分页 URL，并按 RFC Link 结构处理 `<...>` URL 和 quoted 参数里的逗号，再从同一组 before/cursor query 参数中提取续抓 before-id。

M6 补充：sidechain/subagent state loader 现在接受 `subagent_start`/`agent_start`/`task_start` 和 `sidechain_end`/`subagent_finish`/`agent_finish`/`task_summary` 等 subtype 别名，并归一化 `active`/`success`/`canceled`/`error` 等运行状态别名，同时读取 `subagentId`/`agentID`、`subagentType`、`finalSummary` 等 content 字段。

M6 补充：sidechain/subagent lifecycle content 读取现在会递归解包 `payload`/`data`/`body`/`result`/`response`/`metadata` 等 wrapper，嵌套的 subagent ID、status/outcome、summary、agent type、workspace path 和 task description 都可参与 state/list/resume 恢复。

M6 补充：sidechain/subagent lifecycle content 读取现在也递归解包 GraphQL/JSON:API 风格的 `edge`/`node`/`attrs` wrapper，wrapped start/summary event 可继续恢复 ID、status、summary 和 agent metadata。

M6 补充：sidechain/subagent lifecycle 的 ID 等 string-like 字段现在接受 JSON number 和 Go 数字标量，numeric subagent ID 会保留为字符串并可用于 state/list/resume 查找。

M6 补充：sidechain runtime finish 现在会在写入 summary 前把 `success`/`error`/`canceled` 等状态别名归一为 `completed`/`failed`/`cancelled`，让 sidechain transcript 与主 transcript 的 lifecycle 输出保持 canonical。

M6 补充：sidechain lifecycle start/end time 现在接受 `startTimestamp`/`startTimestampMs`/`startedAtUnix` 以及 `endTimestamp`/`completedTimestamp`/`completedAtUnixMs` 等相邻时间别名，恢复第三方/旧 runtime transcript 时不再只依赖 `startedAt`/`endedAt`。

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

M6 补充：session-memory recall agent 和 relevant-memory selector 现在递归解包 `data`/`payload`/`body`、JSON:API `resource`/`attributes`/`properties`/`attrs`、`included`，以及 GraphQL `viewer`/`edge`/`node`/`nodes`/`edges`、`collection`/`list`/`children`/`values` selection wrapper；带明确非 memory/session `type` 的 resource 不再用裸 `id` 污染选择顺序，API-shaped model response 中的 session IDs 和 memory paths 会按模型顺序保留。

M6 补充：session-memory recall agent 和 relevant-memory selector 现在也接受 provider-style `choices`/`outputs`/`candidates`/`generations` response wrapper，可从 `message.content`、content-block array、`content.parts[].text` 和 fenced `json` code block 里递归恢复 JSON selection payload。

M6 补充：session-memory recall agent、relevant-memory selector 和 model-backed fact extraction 的 fenced JSON 提取现在接受 inline/glued fence 形态，模型输出语言标记后同一行直接跟 JSON 时仍能恢复 selection/facts。

M6 补充：session-memory recall agent 和 relevant-memory selector 的 query 解析现在接受 `user_query`/`userQuery`、`question`、`prompt`、`input`、`search`、`search_text`/`searchText` 等相邻别名，模型返回非 canonical query key 时仍能保留改写后的检索语义。

M6 补充：relevant-memory selector 现在也接受 `sourcePath`/`source_path` 和 `documentPath`/`document_path` 等 memory path aliases，模型/API 以 source/document 语义返回候选文件路径时仍能匹配本地 memory 文件。

M6 补充：session-memory recall agent 现在也接受 `sessionPath`/`session_path`、`sessionSummaryPath`、`summaryPath`/`summary_path`、`sessionFilePath` 和 `transcriptPath` selection aliases，模型/API 直接返回 summary 或 transcript/session JSONL 文件路径时可复用现有 path lookup 找回 session。

M6 补充：model-backed memory fact extraction 现在也接受 provider-style `choices`/`outputs`/`candidates`/`generations` response wrapper 以及顶层 `message`/`content`/`text` envelope，可从 `message.content`、content-block array、`content.parts[].text` 和 fenced `json` code block 里递归恢复 JSON facts payload。

M6 补充：model-backed memory fact extraction 的正文解析现在接受 `fact`、`statement`、`insight`、`result`、`output` 等相邻 text aliases，模型不用 canonical `text`/`content`/`summary` 字段时也能恢复事实内容。

M6 补充：model-backed memory fact extraction 的 kind 归一化现在接受 `constraint`、`user_rule`、`guideline`、`standing_instruction`、`policy` 等 instruction-like aliases，并归入现有 preference 事实类型。

M6 补充：model-backed memory fact extraction 现在接受更多 fact source aliases，包括 `sourceMessageUUID`/`source_message_uuid`、`sourceEventId`/`source_event_id`、`originId` 以及 `turn`/`event` source object，并保留 numeric source IDs 为字符串。

M6 补充：compact runner 的 summary 响应现在也接受 provider-style `choices`/`outputs`/`candidates`/`generations` wrapper，可在构建 compact plan 前从 `message.content`、content-block array、`content.parts[].text` 和 fenced `json` code block 中恢复 visible summary text。

M6/M7 补充：Runner 会把 `RelevantMemoryDir` 透传到 tool metadata 的 internal auto-memory path context，让 Read tool freshness prefix 和 permission internal-path policy 在同一 memory dir 配置下生效。

M6 补充：transcript resume 在 fallback 转换 attachment message 时会保留 raw attachment payload，确保恢复出的 `relevant_memories` attachment 仍可被 request 构建路径展开为 system-reminder。

M6/M7 补充：Read tool 现在在 metadata 提供 auto-memory 目录时，为读取到的旧 auto-memory 文件前缀 freshness system-reminder，贴近官方 FileReadTool 的 memory freshness prefix。

M6 补充：microcompact disk cache loader 现在接受更多相邻 timestamp/expiry aliases，包括 `cachedAt`、`cacheCreatedAt`、`storedAt`、`generatedAt`、`updatedAt`、`timestamp`、`expiry`、`expirationTime`、`validUntil`、`notAfter`，以及 `timeToLiveSeconds`、`validForMs` 等相对 TTL 字段。

M7 补充：keybinding resolver/config 和脚本 named-key 输入已覆盖 `ctrl-h`/`ctrl-i`/`ctrl-j`/`ctrl-m`、`ctrl-[`、`ctrl-?` 及 `control-*` 终端别名；terminal parser 支持 CSI-u/kitty keyboard protocol 的 ctrl/alt/shift-enter/shift-tab 序列；image hint parser 支持 OSC ST terminator 和 base64 `name=` filename；keybinding JSON loader 支持 wrapper object-map、`shortcuts`、object action 字段、string-array key sequence/chord 和 `null`/`false` unbind；mouse parser 支持 legacy X10/normal tracking 序列；interaction script 支持结构化 mouse/mouse_event 步骤，button 接受 `buttonMask`/`button_mask`/`btn`/`code`/`mask`，坐标接受 `mouseX`/`mouse_x`/`clientX`/`screenX`/`pageX`/`offsetX`/`viewportX` 和对应 Y/row/line 别名，release 接受 `mouseUp`/`isRelease`/`mouseRelease`/`releaseEvent` 等字段别名；interaction script 还支持字符串 `keys` 和 `input`/`input_text`/`keys_text`/`raw_key`/`paste_text` 字段别名，status/snapshot/viewport/pasted-content contains 断言接受单字符串或字符串数组，且 `keybindings`、`expectEvents`、`expectDialogResults`、`expectPrompt.pastedContents`、`expectTasks.contains` 接受单对象或对象数组。

M7 补充：terminal input parser 现在把 modified SS3 function-key 序列（如 `ESC O 1;2P`、`ESC O 1;5Q`、`ESC O 1;16S`）归一为现有 F1-F4 key surface，补齐 xterm/kitty 兼容模式下的 F-key 输入形态。

M7 补充：terminal input parser 现在也接受 modified SS3 application-cursor navigation 序列（如 `ESC O 1;2A`、`ESC O 1;5D`、`ESC O 1;16C`），和 CSI modified navigation 一样把 shift 降级为方向键、alt/meta 映射到 word-motion key、ctrl 组合映射到 ctrl word-motion key。

M7 补充：terminal input parser 现在把显式默认参数的 CSI navigation key 序列（如 `ESC [ 1 A`、`ESC [ 1 D`、`ESC [ 1 H`、`ESC [ 1 F`）归一为现有 arrow/Home/End key surface，同时继续让 `ESC [ 2 A` 这类 cursor-count 控制保持 unknown。

M7 补充：task runtime 现在会在状态行、任务面板排序/渲染、批量取消和 scripted task expectation 前把 `active`/`in_progress`、`success`/`done`、`error`、`canceled` 等 task state 别名归一为 canonical 状态。

M7 补充：permission runtime 现在会把 `Reject`/`deny`/`decline`/`disallow`/`no` 等 permission action 归一为 denied 结果，把 `Cancel`/`abort` 归一为 cancelled 结果，并让 scripted dialog-result status 断言接受 `rejected`/`approved` 等状态别名。

M7 补充：keybinding config 的 page navigation key name 现在接受 `pgup`/`pg-up`/`prior` 和 `pgdn`/`pg-down`/`next`，覆盖常见终端键名/配置别名。

M7 补充：keybinding config 和脚本 named-key 输入现在接受 DOM-style arrow key aliases，包括 `arrowLeft`/`arrowRight`/`arrowUp`/`arrowDown` 以及 ctrl/alt/meta/option arrow variants。

M7 补充：keybinding action parser 现在接受更多 editor/global-style action aliases，包括 `cursorLeft`/`cursorRight`、`lineStart`/`lineEnd`、`deletePreviousChar`/`deleteNextChar`、`killLine`、`pasteKillRing`/`yankPrevious`、`clearScreen`、`openExternalEditor`、`toggleTasks`、`cancelAgents`、`focusPrev`、`acceptSelection` 和 `search`。

M7 补充：keybinding config 和脚本 named-key 输入现在接受短修饰符别名，包括 `c-`/`m-`/`a-`/`opt-`/`s-` 以及 compact/camel 形式，可覆盖 control、meta、alt、option 和 shift key names。

M7 补充：keybinding config 和脚本 named-key 输入现在接受 `backtab`/`back-tab`/`btab` 等 Shift-Tab terminfo/fixture 别名，并映射到既有 focus-previous key surface。

M7 补充：keybinding JSON loader 现在递归解包 `data`/`payload`/`settings`/`config`/`keyboard`/`keymap` 等外层 wrapper，嵌套 preference export 中的 `bindings`/`shortcuts` 不需要手工扁平化。

M7 补充：keybinding JSON loader 现在也递归解包 JSON:API/resource-style `resource`/`attributes`/`properties`/`attrs` wrapper，API/preferences envelope 内的 `keybindings`/`keymap` 可直接加载。

M7 补充：keybinding JSON loader 现在也接受 GraphQL connection 风格的 `edges` binding list，binding item 可用 `edges[].node` 或 `edge.node` wrapper，外层可递归解包 `viewer`/`node`/`*Connection` wrapper。

M7 补充：keybinding JSON loader 现在接受 `keymap`/`keymaps`、`keyboardShortcuts`、`hotkeys`、`userKeybindings`、`customKeybindings` 等集合字段别名，并同时支持直接 object-map 和嵌套 `bindings` wrapper。

M7 补充：keybinding JSON loader 现在也接受 provider-style `choices`/`outputs`/`candidates`/`generations` response wrapper，可从 `message.content`、content-block array 和 `content.parts[].text` 里恢复 binding array 或 object map。

M7 补充：interaction script 和 keybinding provider response 现在会剥离 fenced `json` code block，模型/SDK 返回 code-fenced 脚本或 keybinding 配置时不再需要手工去 fence。

M7 补充：interaction script 和 keybinding provider response 的 fenced JSON 提取现在接受 inline/glued fence 形态，例如语言标记后同一行直接跟脚本数组或 keybinding map，模型输出不换行时仍能加载配置。

M7 补充：interaction script 的 per-step keybinding mutation 现在复用同一套 collection alias、object-map 和 JSON:API/resource wrapper 解析，脚本步骤可直接使用 `keymap`、`keyboardShortcuts`、`hotkeys`、`keyboard`、`preferences` 或 `keybindingConfig` 临时改键位。

M7 补充：interaction script 的 `keys` 字段现在支持 printable text chunk 和空格分隔 named-key sequence，例如 `ctrl-x ctrl-k`，减少官方脚本把连续输入拆成数组的改写成本。

M7 补充：interaction script key input 现在接受 press-style aliases，包括 `press`、`keyPress`、`keypress`、`shortcutKey`、`presses`、`keyPresses` 和 `shortcuts`。

M7 补充：interaction script 的 `message` step 现在接受 chat/transcript 风格的 `type`/`speaker` role 别名和 `content`/`body`/`message` text 别名；`image` step 和 iTerm2 image hint 接受 `fileName`/`file_name`/`name`、`mimeType`/`mime_type`/`contentType`、source path/URL 和 `data`/`base64` 内容别名；permission request step 接受 request/permission/tool-use ID、path、description 和 action 字段别名，并允许 `actions` 使用单字符串；`expectPrompt` 接受 `value`/`input`/`content`/`message`、`expandedText`/`fullText`、`cursorIndex`/`cursorPosition`、`isEmpty`/`blank` 等字段别名，且 `pastedContents` 断言接受 `pastedId`/`pastedContentId`、`kind`/`pastedType`、`value`/`data`/`base64`、`contentType`/`mimeType`、`fileName`/`name` 和 `contains` 等字段别名；`expectVim` 接受 `vimEnabled`/`isEnabled`、`vimMode`/`modeName`/`currentMode`、`vimRegister`/`registerValue`/`yankRegister`、`registerLinewise`/`linewise` 等字段别名；`expectTasks` 接受 `taskCount`/`total`/`size`/`length` 和 `statusCounts`/`countsByState` 等字段别名；`expectScreen`/`expectViewport` 接受 `columns`/`rows`、`screenWidth`/`screenHeight`、`scrollOffset`/`viewportOffset`、`visibleRows`/`lineCount` 等字段别名；`expectReverseSearch` 接受 `isActive`/`visible`/`open`、`search`/`term`/`pattern`、`cursorIndex`、`currentResult`、`matchCount`/`matches`、`noMatches` 等字段别名；`expectDialog` 可断言 body contains/not-contains、actions/action contains/not-contains、action count 和 focused action，runtime-aware scripts 会在步骤间保留 dialog focused action，且接受 `isActive`/`visible`、`dialogId`/`dialogID`、`dialogKind`、`heading`/`header`、`content`/`text`/`message` 等字段别名；`expectEvent`/`expectDialogResult` 接受 `eventType`/`event`/`name`、`payload`/`text`/`message`、`dialogId`/`dialogID`/`dialogKind`、`actionValue`/`resultStatus`/`exists`/`isStale` 等字段别名。

M7 补充：interaction script 现在接受 `messages`、`append_messages`/`appendMessages`、`transcript_messages`/`transcriptMessages` 批量消息注入字段，且这些字段既可用单个对象也可用对象数组，消息对象沿用 chat/transcript role/text 别名；message 注入也接受 `pastedContent`/`attachments` 粘贴内容别名、单数 `imagePasteId` 图片引用别名，以及 pasted-content 的 `kind`/`value`/`data` 内容字段别名。

M7 补充：interaction script 的 direct `dialog` step 现在接受 `dialogId`/`dialogID`、`dialogKind`、`heading`/`header`、`content`/`text`/`message`、`options`/`choices`/`buttons` 和 `focusedIndex`/`selectedIndex` 等字段别名，并允许 actions/options 使用单字符串。

M7 补充：interaction script loader 现在接受 `scriptSteps`/`script_steps`、`interactionSteps`/`interaction_steps` wrapper 字段，并能从一层 `scenario`/`test`/`case`/`fixture`/`interaction` 对象里继续解析脚本步骤。

M7 补充：ANSI snapshot corpus 比对现在在 `.txt` baseline 缺失时可读取 `.ansi` baseline 并 strip ANSI 后比较，strict unexpected-baseline 检查也会纳入 `.ansi` 文件。

M7 补充：interaction script step 现在接受 `resize`/`terminalSize`/`screenSize` 对象或 `[width,height]` 数组，以及顶层 `columns`/`rows` resize 别名；`focus`/`focused`/`blur`/`focusIn`/`focusOut` 会走正常 focus event 路径；snapshot capture 接受 `snapshot`/`snapshotId`/`snapshotLabel` 等名称别名；runtime-aware scripts 接受 `permission`/`permissionRequest`、`task`/`taskStatus`、`removeTask`/`deleteTask`、`cancelPermission`、`cancelTasks`/`cancelReason`、`openTasks`/`showTasks` 等 mutation 别名。

M7 补充：interaction script action/type/kind/name/operation 动作判别字段现在接受 compact/camel event/media aliases，包括 `focusIn`、`focusOut`、`mouseEvent`、`pasteImage` 和 `imagePaste`。

M7 补充：interaction script action/type/kind/name/operation 动作判别字段现在可驱动 runtime/dialog mutation，支持 `requestPermission`、`taskStatus`、`showTasks`、`cancelTasks`、`removeTask` 和 `showDialog` 等动作，并从 `value`/`payload`/`data`/`body` 载荷解析对象、ID 或取消原因。

M7 补充：interaction script JSONL loader 单行上限提升到 50MiB，和 transcript/session 大记录读取容忍度对齐，避免大型 paste、image metadata 或 snapshot fixture 脚本行触发 scanner token limit。

M7 补充：terminal CSI-u/kitty keyboard parser 现在接受 codepoint alternate 和 modifier event-type 的冒号字段（如 `CSI 97:65;5:1u`），按主 codepoint/modifier 解析 ctrl/alt/shift/rune 键，避免 kitty progressive keyboard protocol 变体被判为 unknown。

M7 补充：terminal CSI-u/kitty keyboard parser 现在也接受无修饰/base 序列（如 `CSI 97u`、`CSI 13;1u`），映射 printable rune、Enter、Tab、Esc 和 Backspace，避免启用 extended keyboard 后普通按键掉入 unknown。

M7 补充：terminal input parser 现在接受 xterm modified arrow 序列（如 `CSI 1;2D`、`CSI 1;6C`、`CSI 1;7D`），把 shift-arrow 降级为方向键、alt-arrow 映射到 word-motion key、ctrl/ctrl+alt-arrow 映射到 ctrl word-motion key，避免 extended navigation 序列落入 unknown。

M7 补充：terminal input parser 现在把 xterm modified navigation modifier 范围扩展到 `2..16`，覆盖 meta/shift+meta/ctrl+meta 组合（如 `CSI 1;10D`、`CSI 1;16C`）以及对应 Home/End/Delete/PageUp/PageDown 序列。

M7 补充：terminal CSI-u/kitty keyboard parser 现在按 modifier bitfield 解码 `9..16` 扩展组合，把 meta/shift+meta 映射到现有 alt key surface，把 ctrl+meta 组合保留为 ctrl key，覆盖 `CSI 98;9u`、`CSI 97;13u` 等序列。

M7 补充：terminal CSI parser 现在把 DA/device attributes (`CSI c`、`CSI >c`、`CSI =c`) 解析成 report action，保留 primary/secondary/tertiary private marker 和 code，避免终端能力查询序列落入 generic unknown。

M7 补充：terminal CSI parser 现在接受 ECMA/xterm cursor alias final bytes：`CSI a` 映射 cursor-forward、`CSI e` 映射 cursor-down、`CSI \`` 映射 cursor-column，避免常见终端输出别名落入 unknown。

M7 补充：terminal CSI parser 现在接受 DEC private mode `?1046h/l` alternate-screen switching mode、`?1047h/l` alternate-screen buffer 和 `?1048h/l` save/restore cursor，补齐常见 alternate-screen lifecycle 序列变体。

M7 补充：terminal CSI parser 现在把 DECREQTPARM terminal-parameters (`CSI x`) 解析成 report action，保留 code 和 private marker，避免终端参数查询序列落入 generic unknown。

M7 补充：terminal CSI parser 现在把 xterm window manipulation/report (`CSI t`，如 `CSI 14t`/`CSI 18t`) 解析成 report action，保留 code/private marker，并为 `CSI 4;height;width t` 与 `CSI 8;rows;cols t` 暴露结构化尺寸字段，避免窗口/文本区尺寸序列落入 generic unknown。

M7 补充：terminal CSI parser 现在把 DECRPM mode status report (`CSI Ps;Ps $ y` / `CSI ? Ps;Ps $ y`) 解析成 report action，保留 mode code、status 和 DEC private marker，和 DECRQM mode request 形成闭环。

M7 补充：terminal CSI parser 现在把 TBC tab-clear (`CSI g`/`CSI 3g`) 解析成 cursor action，保留 clear-current/all code，避免制表位清理序列落入 generic unknown。

M7 补充：terminal ESC parser 现在把 HTS horizontal-tab-set (`ESC H`) 解析成 cursor/tab-set action，和 CSI tab-clear 控制序列形成闭环。

M7 补充：terminal sequence dispatcher 现在把 SS3 application cursor (`ESC OA`/`OB`/`OC`/`OD`) 解析成结构化 cursor move action，避免 application cursor mode 序列落入 unknown。

M7 补充：terminal sequence dispatcher 现在也把 modified SS3 application cursor (`ESC O 1;2A`/`1;5B`/`1;16D`) 解析成结构化 cursor move action，和 input parser 的 modified SS3 navigation 支持保持一致。

M7 补充：terminal CSI parser 现在把 DEC `?1h/l` application cursor mode 解析成独立 mode action，和 SS3 application cursor key 解析闭环。

M7 补充：terminal CSI parser 现在把 DEC `?3h/l` 132/80-column mode 解析成结构化 `columnMode` action，覆盖常见列宽状态切换序列。

M7 补充：terminal CSI parser 现在把 DEC `?40h/l` allow column switching mode 解析成结构化 `allowColumnSwitch` mode action，补齐 `?3h/l` 列宽切换相邻的许可状态序列。

M7 补充：terminal CSI parser 现在把 DEC `?95h/l` no-clear-on-column-switch mode 解析成结构化 `noClearOnColumnSwitch` mode action，补齐列宽切换时是否清屏的状态序列。

M7 补充：terminal CSI parser 现在把 DEC `?5h/l` reverse video/screen mode 解析成结构化 `reverseVideo` mode action，继续减少终端显示状态序列的 unknown fallback。

M7 补充：terminal CSI parser 现在把普通 `CSI 4h/l` insert/replace mode 解析成 `insertMode` action，避免 ECMA mode set/reset 序列落入 unknown。

M7 补充：terminal CSI parser 现在把普通 `CSI 20h/l` line-feed/new-line mode 解析成 `lineFeedMode` action，继续覆盖 ECMA mode set/reset 序列。

M7 补充：terminal CSI parser 现在把 DEC `?6h/l` origin mode 和 `?7h/l` auto-wrap mode 解析成结构化 mode action，继续减少终端状态序列的 unknown fallback。

M7 补充：terminal CSI parser 现在把 DEC `?8h/l` auto-repeat mode 解析成结构化 `autoRepeat` mode action，补齐键盘重复状态序列。

M7 补充：terminal CSI parser 现在把 DEC `?12h/l` cursor blink mode 解析成结构化 `cursorBlink` mode action，补齐 cursor visibility/style 相邻的终端状态序列。

M7 补充：terminal CSI parser 现在把 DEC `?44h/l` margin bell mode 解析成结构化 `marginBell` mode action，补齐 wrap/margin 相邻的响铃状态序列。

M7 补充：terminal CSI parser 现在把 xterm/DEC `?45h/l` reverse-wraparound mode 解析成结构化 `reverseWrap` mode action，补齐 auto-wrap 相邻的 wrap 状态序列。

M7 补充：terminal CSI parser 现在把 DEC `?46h/l` logging mode 解析成结构化 `logging` mode action，避免日志状态序列落入 unknown fallback。

M7 补充：terminal CSI parser 现在把 DEC `?66h/l` application keypad mode 解析成结构化 `applicationKeypad` mode action，补齐 application cursor mode 相邻的输入状态序列。

M7 补充：terminal ESC parser 现在把 VT100 `ESC =`/`ESC >` application/numeric keypad 模式也归一成 `applicationKeypad` mode action，和 CSI `?66h/l` 输出保持一致。

M7 补充：terminal CSI parser 现在把 DEC `?67h/l` backarrow key mode 解析成结构化 `backarrowKey` mode action，补齐键盘输入状态序列。

M7 补充：terminal CSI parser 现在把 DEC `?69h/l` left/right margin mode 解析成结构化 `leftRightMarginMode` mode action，补齐 scroll-region 相邻的 margin 状态序列。

M7 补充：terminal CSI parser 现在把带参数的 `CSI Pl;Pr s` 解析成 left/right horizontal margin region action，同时保留无参数 `CSI s` save-cursor 语义，和 DEC `?69h/l` margin mode 闭环。

M7 补充：terminal CSI parser 现在识别带 intermediate space 的 `CSI Ps SP @` / `CSI Ps SP A` scroll-left/right 序列，避免误解析成 insert-characters 或 cursor-up。

M7 补充：terminal ESC parser 现在把 charset selection (`ESC ( B` / `ESC ) 0` / `ESC * B` / `ESC / A` / `ESC % G` 等) 解析成结构化 charset action，并在 terminal parser 可见文本管线中消费，避免常见终端 charset 选择序列残留为 unknown。

M7 补充：terminal ESC parser 现在也把 ISO-2022 charset shift 控制 (`ESC N`、`ESC n`、`ESC o`、`ESC |`、`ESC }`、`ESC ~`) 解析成结构化 charset-shift action，并在可见文本管线中消费，继续减少真实终端输出里的 unknown 控制序列。

M7 补充：terminal ESC parser 现在把 DEC line/screen attribute 序列 (`ESC # 3/4/5/6/8`) 解析成结构化 screen action，并在 terminal parser 中透传 alignment-test/line-size 控制，避免 ANSI snapshot 管线落入 unknown。

M7 补充：terminal ESC parser 现在把 DECID identify-terminal (`ESC Z`) 归入现有 device-attributes report action，和 `CSI c` 查询路径保持一致。

M7 补充：terminal CSI parser 现在把 DEC selective erase `CSI ? Ps J` / `CSI ? Ps K` 标记为 selective display/line erase，和普通 ED/EL 区分开。

M7 补充：terminal CSI parser 现在把 ECMA `CSI Ps N` / `CSI Ps O` 解析成 erase-in-field / erase-in-area action，覆盖 to-end/to-start/all 三种 region。

M7 补充：terminal CSI parser 现在把 DEC insert/delete columns (`CSI Ps ' }` / `CSI Ps ' ~`) 解析成 edit action，避免列编辑控制序列落入 unknown fallback。

M7 补充：terminal CSI parser 现在把 REP repeat-preceding-character (`CSI b`/`CSI 4b`) 解析成 edit action，visible-text/snapshot pipeline 和 ANSI message wrapping/trim 会按重复次数展开前一个可重复 grapheme。

M7 补充：terminal CSI parser 现在按 ANSI 默认参数解析 scroll-region (`CSI r`/`CSI ;10r`)，缺失 top 默认为 1，缺失 bottom 保持为 0 表示 reset/full-height，避免把 reset 误判为单行区域。

M7 补充：terminal CSI parser 现在把 DECSTR soft reset (`CSI !p`) 解析成 reset action，terminal parser 会复用现有 reset 流程清理 SGR/link 状态，避免软复位序列落入 generic unknown。

M7 补充：prompt history 写入现在按官方 `history.ts` 过滤 image pasted content，不再把 image base64/filename/mediaType 写入 `history.jsonl`；历史读取仍兼容旧 image metadata。

M7 补充：paste-cache 现在提供按 cutoff mtime 清理旧 `.txt` paste 文件的 best-effort 入口，忽略不存在的 cache 目录、非 `.txt` 文件和单文件清理错误，贴近官方 `cleanupOldPastes` 行为。

M7 补充：Buffered prompt history writer 现在支持撤销最近 pending entry 的 fast path，给中断/自动恢复场景接入官方 `removeLastFromHistory` 的 pending-buffer 语义留下可测试入口。

M7 补充：Buffered prompt history writer 现在也支持撤销已 flush entry 的 slow path：记录最近 add 的 timestamp，并在同一 writer 的 up-arrow/ctrl-r 历史读取中按当前 session 跳过，保持 `history.jsonl` append-only。

M7 补充：image-cache 现在有 session-scoped 存取基础：图片 paste 可按官方 `image-cache/<session>/<id>.<ext>` 路径缓存、base64 落盘为 0600 文件、批量只存 image 内容、查询内存路径，并清理非当前 session 的旧 image-cache 目录。

M7 补充：PromptInput/REPL screen 现在可启用 session-scoped image-cache；image hint paste 进入 prompt 时会缓存 `[Image #N]` 对应文件路径并把 base64 图片写入 `image-cache/<session>`，贴近官方 PromptInput 的 `cacheImagePath` + `storeImage` 行为。

M7 补充：image paste cache 现在会在没有原始 source path 时把生成的缓存路径写回 pasted-content 的 `SourcePath`，prompt image metadata/history 恢复不再只依赖全局 image-id 路径缓存。

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

M7 补充：prompt history pasted-content 正文字段现在接受 `text`/`body`/`message`/`raw`/`base64Data` 等别名，stored pasted-content hash 也接受 `digest`/`checksum`/`sha256` 等别名，减少 attachment/cache 风格历史记录恢复时丢失正文或 paste-cache 命中的情况。

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

本轮补充：`cmd/claude --help` 现在沿用 Go flag usage 输出并成功退出；`cmd/claude --print/-p` 现在接入真实 `conversation.Runner.RunTurn` 单轮 headless 路径，可从参数或 stdin 读取 prompt，读取 `ANTHROPIC_API_KEY`/`ANTHROPIC_BASE_URL`/`ANTHROPIC_MODEL`/`CLAUDE_MODEL`/settings model，解析模型别名，装配 builtin tools、settings-derived permission engine、settings-derived MCP config，并把最终 assistant text 写到 stdout；`--output-format json` 现在会输出基础 result envelope，`--output-format stream-json` 会输出基础 NDJSON event stream 并以 result 行收尾，包含 result text、session id、assistant message、stop reason、model、usage 和 tool results；`--stream --output-format stream-json` 现在还会透出 raw Anthropic streaming events，包括 text delta；headless `--resume <session-id-or-jsonl>` 和 `--continue` 现在会加载当前项目 transcript chain 作为 history，并把新回合追加回同一个 transcript；`--system-prompt`/`--append-system-prompt` 现在会进入 Anthropic request `system` 字段；`--allowedTools`/`--allowed-tools` 和 `--disallowedTools`/`--disallowed-tools` 现在会作为 CLI permission rules 合入 headless permission engine；`--add-dir`/`--addDir` 现在会作为 CLI additional working directory 合入 headless permission context。

当前状态：bootstrap/config/auth/model 基础已完成；CLI 已有 `--version`、`--help` 成功退出、scaffold settings 校验、基础 `--print` headless 单轮执行路径、基础 JSON result 输出、基础 stream-json event 输出、raw streaming event 透传、headless resume/continue transcript 接线、system prompt flags、CLI allow/deny tool rules 和 CLI add-dir additional working directory context，但完整参数矩阵、交互 TUI 主循环、resume picker/UI、官方 SDK NDJSON/control protocol 和官方 stdout/stderr/exit-code parity 仍未完整兼容 CC。

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

- 文本版 `Read`、PDF text/page-selection 初版（含常见 Page/Contents 间接对象、Pages/Kids 页序、FlateDecode 文本流和 UTF-16 BOM 字符串）、PNG/JPEG/GIF/WebP image Read、Jupyter notebook cell 渲染初版、Read 大文本 tool-result budget 截断/落盘、`Write`、`Edit` 初版已完成。
- 已覆盖读前写、mtime stale guard、唯一匹配、`replace_all`、Write/Edit structured diff hunks、`.claude/settings.json`/`settings.local.json` 写前 JSON/语义校验、team-memory secret guard、Read 去重、跨 tool round read-state。
- `NotebookEdit` 初版已完成，支持官方 `notebook_path`/`cell_id`/`new_source`/`cell_type`/`edit_mode` schema、replace/insert/delete 主路径、真实 cell id 和 `cell-N` 索引、code cell 修改后清空 outputs/execution_count、read-before-edit/stale guard、read-state 刷新、`notebook_path` 权限路径识别、结构化结果和 cell-level diff/hunks。
- `Bash` 初版已完成，支持 command/timeout/description 输入校验、`/bin/sh -c` 执行、stdout/stderr/exit code/timeout 结构化结果、动态 read-only/concurrency-safe/destructive 分类、Git diff/log/show/status/ls-files/grep/rev-parse/branch/tag/ls-remote safe-flag 校验，Git remote/push/reflog/stash/worktree/merge-base/describe/cat-file/for-each-ref/rev-list/blame/shortlog/config-get 参数级安全分类、`git remote show/get-url` 参数收紧、`git ls-remote` URL/SSH/server-option guard、branch/tag 裸 positional 创建防护、`git reflog expire/delete`、`git stash drop/pop/clear` 和 `git worktree remove/prune` 破坏性分类、`find -delete/-exec rm` 与 `xargs rm` 破坏性分类、safe wrapper/env 前缀归一化（`time`/`nohup`/`timeout`/`nice`/`stdbuf`/`env`）后的只读/破坏性分类、临时环境赋值前缀后的破坏性命令识别、权限规则接入、后台启动、同会话 `BashOutput` 输出读取和 `KillBash` 取消。
- `Glob`/`Grep` 纯 Go 初版已完成，支持 `**` 递归 glob、Glob 绝对 pattern base-dir 提取、Glob 官方 pattern/path-only strict schema、Glob/Grep 输出工作目录相对路径、Glob 默认 no-ignore/hidden 搜索及 `CLAUDE_CODE_GLOB_NO_IGNORE`/`CLAUDE_CODE_GLOB_HIDDEN` env 切换、Grep 官方 VCS metadata 目录排除（`.git`/`.svn`/`.hg`/`.bzr`/`.jj`/`.sl`）、Grep 层级 `.gitignore`/`.ignore`、Glob oldest-first modified/path 排序、Glob 截断 tool-result 提示、Grep regex/fixed string (`fixed_strings`/`-F`)、multiline 跨行 dotall 搜索、glob/type 过滤、Grep glob 空白/逗号多 pattern 与 brace alternation、Glob/Grep path 存在性校验和 Glob directory-only path 校验、`output_mode`/`outputMode` 的 `files_with_matches`/`content`/`count` 输出模式、Grep files_with_matches file-count summary、Grep count-mode occurrence/file summary、Grep `--max-columns 500` 长匹配/上下文行省略占位、`context`/`before_context`/`after_context` 和 `-C`/`-B`/`-A` 上下文行及官方 precedence（非 content 模式忽略）、`line_numbers`/`lineNumbers`/`-n` line-number 控制、`max_count`/`maxCount`/`-m` per-file match limiting、`offset`/`head_limit` 分页和 content-mode pagination tool-result 提示、默认 250 条 Grep head limit、`head_limit=0` unlimited、`ignore_case`/`case_insensitive`/`caseInsensitive`/`-i` 大小写不敏感搜索，以及 Grep 数字/布尔参数的 quoted semantic string 兼容。
- `TodoWrite` 初版已完成，支持完整 todo list 写入、状态/优先级校验、重复 id 拒绝、单个 `in_progress` 约束、结构化结果、tool metadata 状态保存和 session-scoped 本地持久化/恢复。
- `WebFetch` 初版已完成，支持 URL/timeout/max_bytes 输入校验、HTTP GET、HEAD preflight、metadata/raw `skipWebFetchPreflight` skip-preflight、二进制 preflight 跳过 GET、文本/二进制判定、截断、非 2xx error 标记、结构化结果、HTML-to-text rendering、prompt-focused excerpt、prompt phrase scoring/metadata 和 `WebFetch(domain:...)` 权限规则适配。
- `WebSearch` HTML/JSON 搜索适配初版已完成，支持 query/max_results/timeout/domain filters 输入校验、可注入搜索 endpoint、DuckDuckGo HTML 链接解析、DuckDuckGo subdomain redirect unwrap、常见 JSON result shapes、DuckDuckGo result snippet 抽取、domain allow/block 过滤、结构化结果和 query 权限规则匹配。
- `PowerShell` 初版已完成，支持 command/timeout/description/run_in_background 输入校验、`pwsh`/`powershell` 前台执行、后台启动、`PowerShellOutput` 输出读取、`KillPowerShell` 取消、stdout/stderr/exit code/timeout/cancel 结构化结果、前台/后台输出 tool-result 截断/落盘测试覆盖、缺失可执行文件结构化错误、动态 read-only/concurrency-safe/destructive 分类、常见 mutating alias canonicalization、path-free `git`/`git.exe`/`git.cmd` 等外部 Git 命令复用 Bash Git safety 分类、Docker `ps`/`images`/`logs`/`inspect` 只读外部命令分类和变量/未知 flag guard、只读 PowerShell cmdlet safe-flag allowlist 和路径参数 guard、数据转换/对象检查/系统信息类 cmdlet 只读 allowlist、pipeline-tail 格式化/对象选择类 cmdlet 只读 allowlist 和变量/hashtable/scriptblock guard、网络/事件/CIM 元数据类 cmdlet 只读 allowlist 和远程/XML/hashtable 风险参数排除、native/external 原生命令只读 allowlist（`ipconfig`/`netstat`/`systeminfo`/`tasklist`/`where.exe`/`hostname`/`whoami`/`route print`/`file`/`findstr`/`dotnet` 等）和写操作形态拒绝、文件读取类命令基础相对路径 guard 和默认工具注册。

本轮补充：`WebFetch` HEAD preflight 现在记录 `Content-Disposition`，并会按 attachment filename 的常见二进制扩展名（PDF/image/archive/office/media 等）跳过 GET，覆盖服务端未给 `Content-Type` 但通过下载文件名暴露类型的二进制响应。

本轮补充：`WebFetch` HTML-to-text rendering 现在会保留 anchor `href` 作为链接上下文，并把 `img` 的 `alt`/`title`/`aria-label` 和 `src` 渲染成可见图片说明；prompt-focused excerpt 可以命中图片说明文本，同时避免重复 URL 链接文本和 `javascript:` href。

本轮补充：`WebFetch` GET 会记录 redirect 后的 `final_url`，HTML rendering 会按 final URL 解析相对 anchor/image URL，确保重定向页面中的相对链接和图片说明指向浏览器实际可见的目标地址。

本轮补充：`WebFetch` 文本 body 现在会按 BOM、`Content-Type` charset 或 HTML `<meta charset>`/`http-equiv` charset 解码常见网页编码，包括 UTF-8/UTF-16LE/UTF-16BE、Latin-1 和 Windows-1252，并在 structured content 暴露归一化 `charset`。

本轮补充：`WebSearch` JSON parser 现在会递归解包 `web`、`response`、`search`、`hits`、`documents`、`records`、`entries` 等常见搜索后端 wrapper，并继续保留 URL 去重和 allowed/blocked domain filter。

本轮补充：`WebSearch` JSON result parser 现在支持 `pageUrl`/`targetUrl`/`source_url`/`formattedUrl` 等 URL aliases、`htmlTitle`/`htmlSnippet` 等 HTML 标记字段清理、嵌套 URL object，以及 `deepLinks`/`siteLinks` 子结果递归解析。

本轮补充：`Grep` 现在支持 whole-word 搜索参数 `word_regexp`/`wordRegexp`/`word-regexp`/`-w`，在 regex 和 fixed-string 模式下都会按词边界过滤匹配，并兼容 quoted boolean 输入。

本轮补充：`Grep` 现在支持反向匹配参数 `invert_match`/`invertMatch`/`invert-match`/`-v`，`files_with_matches`、`content`、`count` 和 multiline 模式都会按非匹配行/未覆盖行输出，并兼容 quoted boolean 输入。

本轮补充：`Grep` 的 `files_with_matches` 输出现在按官方行为使用文件修改时间倒序排序，mtime 相同再按路径排序；分页和 `head_limit` 会在排序后应用。

本轮补充：`Glob`/`Grep` 搜索遍历现在会读取 permission context 中的 `Read(...)` deny 规则，并把对应 basename/path/directory pattern 作为额外 ignore rule，避免被禁止读取的文件出现在搜索结果中。

本轮补充：`Bash` 和 `PowerShell` read-only 分类现在会拒绝 tokenizer 视角未闭合的 quote 以及末尾 escape/line-continuation 命令，避免不完整 shell input 被误判为只读。

本轮补充：`Bash`/`PowerShell` 轻量 tokenizer 的 escape 处理现在尊重 single-quoted literal 语义，Bash 单引号内 `\` 和 PowerShell 单引号内 backtick 不再被当作 escape，从而减少合法只读命令的误拒绝。

本轮补充：`Bash`/`PowerShell` read-only/destructive 分类现在会在未引用 newline 处分段，并剥离未引用的行注释；注释后的文本不会误触发拒绝，下一行真实命令仍会被独立分类。

本轮补充：`Bash` destructive 分类现在也把未引用单个 `&` 后台分隔符作为命令边界，`pwd & rm -rf build` 这类后台后续破坏性命令不再漏过 destructive 标记。

本轮补充：`Bash` destructive 分类现在会递归检查未被 single-quote 保护的 command substitution/backtick/subshell 内容，例如 `$(rm -rf build)`、`` `rm -rf build` `` 和 `(rm -rf build)`，避免嵌套破坏性命令只被判为非只读却没有 destructive 标记。

本轮补充：`PowerShell` destructive 分类现在会递归检查未被 single-quote 保护的括号表达式、`$()` 子表达式和 scriptblock `{...}` 内容，`Write-Output (Remove-Item out.txt)`、`"$(Remove-Item out.txt)"` 和 `& { Remove-Item out.txt }` 这类嵌套破坏性命令会触发 destructive 标记。

本轮补充：`Bash` 常见文件读取/搜索类只读命令（`ls`/`cat`/`head`/`tail`/`wc`/`grep`/`rg`/`find`/`stat`/`file`/`du`/`df`）现在增加基础相对路径 guard，绝对路径、home、`..`、变量/命令替换路径、Windows drive、UNC 和 URI/provider-like 路径不再自动进入 read-only fast path。

本轮补充：`Bash` `rg` read-only 分类现在拒绝 `--pre`/`--pre=...` 外部预处理命令，避免 ripgrep 执行任意预处理器时仍被自动归为只读。

本轮补充：`Bash` `go list` read-only 分类现在从“子命令即只读”收敛到参数级 allowlist，允许 `-json`、`-deps`、`-f`、`-m`、`-versions`、`-tags` 和 `-mod=readonly/vendor` 等查询形态，拒绝 `-mod=mod`、`-modfile`、`-overlay`、未知 flag、缺值 flag 和非本地 package pattern。

本轮补充：`Bash` `find` read-only 分类现在拒绝 `-delete`、`-fprint`、`-fprint0`、`-fprintf` 和 `-fls` 等删除/写文件 action，避免 `find . -delete` 或 `find . -fprint out.txt` 这类命令进入只读快路径。

本轮补充：`Bash` safety 分类现在把 `find -exec`/`-execdir`/`-ok`/`-okdir` 排除出 read-only fast path，并会识别 `find ... -exec sh -c 'rm ...'` 与 `xargs sh -c 'rm ...'` 这类 shell wrapper 内嵌破坏性脚本；`find -exec*` 和 `xargs` 的子命令还会复用 safe wrapper/env/assignment 归一化，覆盖 `env rm`、`timeout rm`、`xargs -I{} env sh -c ...` 等形态。

本轮补充：`PowerShell` native/external 文件读取/搜索只读命令（`where.exe`/`file`/`tree`/`findstr`）现在对路径型 positional、`where.exe /R`、`file -f`、`findstr /G`/`/D` 和 `/C:` pattern 后的文件参数执行相对路径 guard，Windows drive、UNC、URI/provider-like、`..` 以及缺值 path flag 不再进入 read-only fast path；`where.exe` flag allowlist 收敛到 `/R`/`/Q`/`/F`/`/T`。

仍需完成：

- `Read` 的完整 PDF parity、完整 notebook render parity、完整 token-budget parity、full media parity、binary edge cases。
- `Edit/Write`/`NotebookEdit` 的完整 git/notebook diff parity、LSP/IDE notify、file history、NotebookEdit UI/golden 和更广义 secret guard。
- `Bash` 完整 shell parser、真实 sandbox、interrupt、后台任务完整生命周期、更细 read-only/destructive validation 和官方 golden 兼容。
- `Glob/Grep` 完整 ripgrep parity 和剩余输出参数。
- `TodoWrite` TUI 同步和官方 golden 兼容。
- `WebFetch` browser 渲染、完整 prompt-aware summarization 和官方 golden；`WebSearch` 官方搜索后端、ranking parity 和 golden。
- `PowerShell` 完整 parser、完整权限/path validation、后台生命周期 edge cases、前台截断 golden、session 记录和官方 golden；Notebook fuller parity、MCP concrete tool semantics。

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

当前状态：session/history 有大量基础能力；memory/compact 初始包、compact runner、conversation auto-compact 接入、compact 失败熔断、microcompact/cache 初版、persistent cached microcompact 初版、cache digest structural/rich-content metadata 覆盖、cache version/TTL/prune、in-memory micro cache prune、memory-cache write-through 到磁盘、atomic cache write、坏缓存默认 fail-open、session memory summary/frontmatter aliases/recall 初版、model-ranked recall session-id selection 和 invalid-selection fallback 初版、recall agent alternate/camel response keys/fenced-prose JSON extraction/scalar id parsing/nested/wrapped/collection-alias selection parsing、resume context model-assisted recall 接入、session memory rollup/prune compaction、rollup archive exclusion/merge、rune-safe rollup truncation、resume context + session memory recall、conversation recall 注入、deterministic/model-backed memory extraction 初版、extraction agent fenced-prose JSON/wrapped facts/provider-style response wrapper/alternate/structured field/nested source object/nested response/fact kind alias parsing、turn-end memory extraction 落盘、prompt history lock/buffered flush/field aliases、official subagent transcript layout、agent metadata sidecar/field aliases、sidechain runtime start/append/finish/cancel/fail 和 parent-chain append/finish 初版、sidechain manager orchestration 初版、sidechain manifest 聚合、sidechain state/list/resume/content-field aliases 初版、sidechain resume context builder、sidechain conversation/content-replacement reconstruction、transcript tail/window/metadata/index loaders、byte-budget transcript tail loader、agent-scoped content replacement metadata/record field-alias loading、session-scoped metadata reappend including AI-title/last-prompt/task-summary、transcript line offset index/window/byte-budget-window/parent-chain/resume/tail/byte-budget-tail loaders、extended transcript metadata entries/type/field aliases、transcript message/session UUID field aliases including top-level `messageUuid`/`messageId`/`id` record IDs and `role`/`entry_type`/`messageType`/`createdAt` timestamp aliases、transcript tombstone metadata delete/relink、transcript resume conversation builder、index 文本预览和 AI-title/last-prompt/task metadata 字段、流式 transcript 搜索、session list pagination、remote history token refresh、remote history 全量分页抓取/page-field/event-list/records/entries/last-id/cursor/event-id/has-next alias/wrapped-data/links/paging/bare-array/keyed event map/connection wrapper response/edge-cursor fallback/max-pages 截断状态/before_id 续抓、remote event transcript materialization/fallback field fill/去重追加/duplicate parent guard、remote history 一步 sync 到 transcript 和 session resume/search/title 支撑已落地；完整 subagent runtime、官方 cached microcompact parity、官方 session memory compaction 策略、完整 memory recall agent 策略仍缺。

本轮补充：session-memory summary frontmatter 的 `updatedAt`/`createdAt` 时间现在接受 Unix 秒、Unix 毫秒和 `updatedAtMs`/`timestampMs` 等相邻字段别名，recall 排序不再只依赖 RFC3339 字符串。

本轮补充：session-memory summary frontmatter 的 session/message ID 现在接受 `sessionUUID`、`conversationId`、`threadId`、`transcriptId`、`messageID` 和 `leafID` 等相邻别名，当前摘要和 recall candidate 恢复不再只依赖 `session_id`/`last_message_uuid`。

本轮补充：session-memory summary 正文现在在 markdown body 为空时接受 frontmatter `summaryText`、`summary`、`content`、`text`、`resultSummary`、`finalSummary` 等字段兜底，同时保持 body 优先。

本轮补充：remote history connection/pageInfo 解析接受 `hasPrevious`/`hasPreviousPage`、`hasOlder`/`more` 继续分页标记，以及 `previousCursor`/`prevCursor`/`beforeCursor`/`olderCursor` before-id cursor 别名。

本轮补充：remote history response parser 会递归解包 `data.session.events`、`data.projectSession.eventConnection`、`data.viewer.session.events`、`data.node.eventConnection`、`conversation`、`remoteHistory` 等 GraphQL/session wrapper，继续复用 `nodes`/`edges[].node` 和 `pageInfo` pagination 解析。

本轮补充：remote history response parser 现在也递归解包 JSON:API `relationships` wrapper，并接受 `children`、`resultsConnection`/`results_connection` 和 `childrenConnection`/`children_connection` 事件集合别名，relationship 内的 `pageInfo` pagination 仍可驱动续抓。

本轮补充：remote history 在 JSON:API `relationships.events.data` 只有 resource identifier 时，会继续使用 top-level `included` 中的真实事件 resource，避免把 `{type,id}` 标识符误当作空事件遮蔽完整 payload。

本轮补充：remote history event-list 字段现在可直接承载单个 SDK event 对象；`data`/`result` 等字段不再必须是数组或 wrapper 才能进入分页结果。

本轮补充：remote history REST/link 风格分页接受 `links.next`/`links.previous`/`links.prev`/`links.older` 的字符串 URL、`{href,url,uri,link}` 对象，或直接携带 cursor 字段的 link object，并从 before/cursor query 参数或 direct cursor fields 提取续抓 before-id。

本轮补充：remote history HTTP `Link` header 分页接受 `previous`/`prev`/`older`/`next` rel URL，并以 body cursor 优先、header cursor fallback 的方式继续抓取。

本轮补充：transcript metadata loader 现在接受 `sessionID` 和 `session` 作为 session-scoped metadata ID 别名，并容忍 `prNumber`、`timeSavedMs`、`lastSpawnTokens` 等计数字段使用数字字符串。

本轮补充：transcript metadata ID helper 现在复用 contract `ID` JSON 解码，`messageID`/`sessionID` 等 metadata ID 字段可接受 JSON number 并保留为字符串。

本轮补充：context-collapse commit metadata 的 collapse/summary/archived ID 字段现在也走 metadata ID helper，支持 JSON number ID 并保持 full/lightweight loader 一致。

本轮补充：context-collapse snapshot metadata 接受 `isArmed`/`enabled` bool 别名、`spawnTokens`/`tokenCount` 计数字段别名，以及 `stagedMessages`/`items` staged payload wrapper，full loader 和 metadata loader 保持一致。

本轮补充：transcript message 和嵌套 contract message 现在接受 `sessionID` 顶层别名，`LoadTranscript`、`LoadTranscriptIndex` 和 indexed resume 会保留该 session id（覆盖测试：`TestLoadTranscriptAcceptsSessionIDUpperAlias`）。

本轮补充：contract message、session entry 和 transcript loader 现在会把 message-type aliases 归一化为 canonical 类型，包括 `assistant_message`、`userMessage`、`system-event`、`attachmentMessage`、`progress_update` 和 `tombstone_event`；full loader、line index 和 indexed resume 统一使用 canonical user/assistant/system/attachment，并保留 progress bridge 语义。

本轮补充：tail、byte-tail、window 和 metadata-only transcript loader 也改用 canonical message type 处理 progress bridge 与 compact-boundary，`progress_update` 和 `system-event` 等别名不再只在 full loader 路径生效。

本轮补充：tail、byte-tail、window 和 streaming transcript search 现在复用 full/index loader 的 wrapped record 展开逻辑，可从 JSON:API/resource、GraphQL edge/node 和 collection/list wrapper 中恢复 transcript 批次，并保留 progress bridge。

本轮补充：remote history `SDKEvent` 解码现在也接受 `sessionID` 作为事件 session id 别名，materialize 成 transcript message 时会同步填充 record 和嵌套 message 的 session id（覆盖测试：`TestRemoteHistoryTranscriptMessagesAcceptsSessionIDUpperAlias`）。

本轮补充：remote history `SDKEvent` 解码现在接受 `parentUUID`、`parentId`/`parentID`/`parent_id` 和 `parentMessageId`/`parentMessageID`/`parent_message_id` 作为 parent alias，materialize transcript 时会保留 parent chain（覆盖测试：`TestRemoteHistoryTranscriptMessagesAcceptsParentIDAliases`）。

本轮补充：sidechain/subagent state loader 接受 legacy/fork 命名的 start/finish subtype、ID/type/summary 字段别名和常见状态别名，提升旧 subagent transcript resume/list 的恢复率。

本轮补充：sidechain/subagent lifecycle content 读取递归解包常见 wrapper，并从嵌套 start/summary event 中恢复 subagent ID、status、summary、agent type、workspace path 和 task description，减少 fork/第三方 transcript 需要手工扁平化字段的情况。

本轮补充：sidechain/subagent lifecycle content 读取现在也递归解包 GraphQL/JSON:API 风格的 `edge`/`node`/`attrs` wrapper，wrapped start/summary event 可继续恢复 ID、status、summary 和 agent metadata。

本轮补充：sidechain/subagent lifecycle 字段提取现在也会穿透 `edges`/`nodes`/`included` 等 collection wrapper 和数组元素，GraphQL connection 或 JSON:API included 风格的 start/summary payload 可继续恢复 state/list/resume 所需字段。

本轮补充：sidechain/subagent lifecycle content 的 ID 等 string-like 字段现在接受 JSON number/数字标量，numeric subagent ID 会保留为字符串并能通过 resume fallback 找回对应 sidechain。

本轮补充：sidechain runtime 现在会拒绝同一 sidechain ID 的 running 状态重复 start；已完成后重新 start 会被视为新 lifecycle，state loader 会清空上一轮 summary/endedAt 并使用新的 startedAt。

本轮补充：conversation runner 现在会在用户消息入队后计算 compact token warning state，并在触发 warning/error/auto-compact/blocking 阈值时发出 `token_warning` event；warning state 接入 blocking-limit override，auto-compact threshold 判断接入 `CLAUDE_AUTOCOMPACT_PCT_OVERRIDE`，使 runtime warning 和自动压缩使用同一套 window 输入。

本轮补充：session-memory recall agent 和 relevant-memory selector 现在递归解包 `data`/`payload`/`body`、JSON:API `resource`/`attributes`/`properties`/`attrs`、`included`，以及 GraphQL `viewer`/`edge`/`node`/`nodes`/`edges`、`collection`/`list`/`children`/`values` selection wrapper；带明确非 memory/session `type` 的 resource 不再用裸 `id` 污染选择顺序，API-shaped model response 中的 session IDs 和 memory paths 会按模型顺序保留。

本轮补充：session-memory recall agent 和 relevant-memory selector 现在也接受 provider-style `choices`/`outputs`/`candidates`/`generations` response wrapper 以及顶层 `message`/`content`/`text` envelope，可从 `message.content`、content-block array、`content.parts[].text` 和 fenced `json` code block 里递归恢复 JSON selection payload。

本轮补充：session-memory recall agent、relevant-memory selector 和 model-backed fact extraction 的 fenced JSON 提取现在接受 inline/glued fence 形态，模型输出语言标记后同一行直接跟 JSON 时仍能恢复 selection/facts。

本轮补充：model-backed memory fact extraction 现在也接受 provider-style `choices`/`outputs`/`candidates`/`generations` response wrapper 以及顶层 `message`/`content`/`text` envelope，可从 `message.content`、content-block array、`content.parts[].text` 和 fenced `json` code block 里递归恢复 JSON facts payload。

本轮补充：model-backed memory fact extraction 现在会穿透 `observations`/`notes`/`findings`/`records`、`data.resource.attributes` 和 `edge.node` 这类 API 包装集合，并接受 `note`/`description`/`body`/`message`/`observation`/`finding` 作为 fact 正文字段别名。

本轮补充：model-backed memory fact extraction 现在接受更多 kind aliases，包括 `user_pref`、`requirement`、`action_item`、`outcome`、`conclusion`、`tool_usage` 和 `command_run`，并归一到 preference/request/decision/tool。

本轮补充：model-backed memory fact extraction 的 kind 归一化现在也接受 `constraint`、`user_rule`、`guideline`、`standing_instruction`、`policy` 等 instruction-like aliases，并归入现有 preference 事实类型。

本轮补充：compact runner 的 summary 响应现在也接受 provider-style `choices`/`outputs`/`candidates`/`generations` wrapper，可在构建 compact plan 前从 `message.content`、content-block array、`content.parts[].text` 和 fenced `json` code block 中恢复 visible summary text。

本轮补充：remote history response parser 现在也接受 provider-style `choices`/`outputs`/`candidates`/`generations` wrapper 以及顶层 `message`/`content`/`text` envelope，可从 `message.content`、content-block array 和 `content.parts[].text` 中恢复 event page JSON（包括 fenced `json` code block），并保留 pagination 继续驱动 `before_id` 续抓。

本轮补充：microcompact disk cache loader 现在读取 Go 默认、camelCase、snake_case 和相邻实现常见字段别名/包装形态，包括 `result`/`data`/`cache`/`value` wrapper、`content`/`text` summary、`cacheKey`/`key`/`hash` digest（JSON number 会原样转为字符串）、cache-hit 别名、计数字段别名/数字字符串/whole-number JSON number、RFC3339/Unix 秒/Unix 毫秒时间字段，以及 `createdAt` + `ttlSeconds`/`ttlMs`/`expiresIn`/`maxAge` 等相对 TTL 推导，提升 cached microcompact 文件在不同实现/版本间的恢复率；fractional count 仍会被拒绝。

本轮补充：microcompact disk cache loader 现在也接受 `cacheEntry`/`cache_entry`、`micro_compact`/`micro_compact_result` wrapper，以及 `summaryMarkdown`/`resultSummary`/`compressedText`、`cacheDigest`/`digestHash`/`fingerprint`、`summarizedCount`/`retainedCount`、`formatVersion` 和 `ttlMilliseconds`/`expiresInMilliseconds`/`maxAgeMilliseconds` 等相邻实现字段别名。

本轮补充：microcompact disk cache loader 现在会从 `metadata`/`meta`/`cacheInfo`/`cacheDetails`/`cacheEntry`/`entry`/`record`/`cache` 等 sidecar object 中补缺失的 digest、version、cache-hit、timestamp、TTL 和 count aliases；主 summary payload 字段仍保持优先。

本轮补充：microcompact disk cache loader 现在也递归解包 GraphQL-style `viewer`/`edge`/`node`/`attrs` wrapper，node `id` 可作为 digest fallback，attrs/properties 内的 summary、version、timestamp 和 TTL aliases 都会恢复。

本轮补充：microcompact disk cache loader 现在也会穿透 `edges`/`nodes`/`included` 等 collection wrapper 和数组元素，跳过无 summary 的非 cache resource，并恢复 GraphQL connection 或 JSON:API included 风格 cache entry。

本轮补充：microcompact disk cache loader 的字段和 wrapper 查找现在接受大小写、snake_case 和 kebab-case 相邻形式归一，例如 `cache-entry` 内的 `summary-text`、`cache-key`、`cache-version`、`created-at` 和 `ttl-seconds` 可恢复同一 cache entry。

本轮补充：microcompact disk cache loader 的 direct payload 判定也复用归一化 summary 别名，顶层 `summary-text`、snake_case 或 kebab-case summary 字段搭配 `data`/`payload` sidecar 时仍会优先恢复顶层摘要。

本轮补充：microcompact disk cache loader 的 summary-like payload 现在接受 text content-block object、text content-block array 和 string array，会把可见 text block 合并为 summary，并会继续解包 text block 内嵌的 JSON/fenced summary payload，兼容官方/SDK 响应内容块形态的 cached microcompact 文件。

本轮补充：microcompact disk cache loader 的 summary array item 现在也复用 provider-style `parts`/`content.parts`/`output` 文本恢复路径，且 provider summary/wrapper 字段同样接受大小写、snake_case 和 kebab-case 相邻形式归一，批量候选或 provider cache item 不再因数组元素不是标准 text block 或字段拼写相邻而失效。

本轮补充：microcompact disk cache loader 的 summary-like payload 现在也接受完整 contract message object，并会递归解包 `message`/`assistantMessage`/`resultMessage`/`outputMessage`/`completionMessage` wrapper，从 message content 中恢复 visible text summary。

本轮补充：microcompact disk cache loader 现在会把 `value` 字段中的 text content-block object 识别为 direct summary payload，同时继续从同一 `value` object 中补 digest、version、timestamp 等 cache metadata，避免 `value` 作为 summary/cache wrapper 双义字段时丢摘要或 sidecar 信息。

本轮补充：microcompact disk cache loader 现在也接受 provider-style `choices`/`outputs`/`candidates`/`generations`/`results` 等响应数组 wrapper，并可从 `message.content`、`output.content`、`content.parts[].text` 和 fenced `json` code block 中恢复 summary，同时保留外层 cache metadata。

本轮补充：microcompact disk cache loader 现在也会解包一行内 fenced JSON summary payload，例如 `json {...}` 或 `json{...}` 与 opening fence 在同一行的 provider/SDK 输出，不再把整段 code fence 当作可见摘要文本。

本轮补充：transcript metadata loader 现在先递归解包 JSON:API/resource、GraphQL edge/node、`included`、collection/list/values 等 wrapper，再做 metadata type 分类；full transcript、lightweight metadata 和 transcript index 都能恢复 wrapped title/task/tag/worktree/content-replacement/context-collapse metadata。

本轮补充：transcript metadata type 现在接受 compact/camel aliases，例如 `aiTitle`、`lastPrompt`、`taskSummary`、`contentReplacement`、`fileHistorySnapshot`、`speculationAccept` 和 `contextCollapseSnapshot`；full transcript、lightweight metadata 和 transcript index 共用同一归一化。

本轮补充：transcript metadata 字段查找现在也接受大小写、snake_case、kebab-case 和空格分隔形式归一，`Session-ID`、`Custom-Title`、`Pull-Request-Number` 等相邻字段可在 full loader、lightweight metadata 和 transcript index 中恢复同一 metadata。
本轮补充：transcript message/envelope 和嵌套 contract message 字段查找现在也复用大小写、snake_case、kebab-case 和空格分隔形式归一，`Message-Type`、`Message ID`、`Parent-Message-ID`、`Session-ID`、`Git-Branch`、`Message Text` 等相邻字段可贯通 full loader、progress bridge、line index 和 indexed resume。
本轮补充：legacy session JSONL 的 `SessionEntry` 读取现在也复用同一 normalized 字段查找，`Entry Type`、`Message-ID`、`Parent Message ID`、`Session-ID`、`Created At` 等字段可经 `session.Load` 恢复旧 entry 与嵌套 message。
本轮补充：remote-history `SDKEvent` 读取现在也复用 normalized 字段查找，`Event Type`、`Event-ID`、`Parent Message ID`、`Created At`、`Message-Payload`、`Status Message`/`Failure Reason`/`Final Output` 等字段可恢复事件类型、ID、parent、时间戳、状态/错误/结果和 transcript materialization 所需 message。

本轮补充：contract content block 解码现在接受文本块字段别名 `body`/`message`/`value`/`output`/`contentText`/`content_text`，并在 `text`/`thinking` block 中从字符串 `content` 回填文本，transcript resume 可恢复这些嵌套文本块格式。

本轮补充：contract image source 解码现在接受 `mediaType`/`mimeType`/`contentType`、`base64`/`payload` 等 source 字段别名，并支持顶层 image block 直接携带媒体类型和 base64 数据，transcript resume 会保留为规范 `ImageSource`。

本轮补充：remote history `SDKEvent` payload materialization 现在会递归解包 `payload`/`data`/`body`/`metadata`/`meta`/`attributes`/`properties` 内的 `record`/`entry`/`item`/`event`/`result`/`response`/`output` wrapper，减少远端事件多层包装导致的消息丢失。

本轮补充：remote history `SDKEvent` type 现在会把 provider-style aliases 归一化为现有 canonical 事件类型，包括 `assistant_message`、`userMessage`、`system-event`、`result_event`、`errorEvent` 和 `status_update`/`progress`，single-object page 与 transcript materialization 不再因事件类型拼写相邻而丢消息。

本轮补充：remote history `SDKEvent` status/error/result 内容字段现在也接受 provider/export 风格正文别名，包括 `stateMessage`/`updateText`/`messageText`、`failureMessage`/`exceptionMessage`/`diagnosticMessage`、`summaryText`/`finalOutput`/`responseText`，并把 `summary`/`final` 作为 result object fallback；这些字段仍只在对应 canonical event type 下补值。

本轮补充：remote history 普通事件数组现在也会解包元素级 `event`/`record`/`entry`/`item`/`resource`/`value` 以及无事件本体字段时的 `data`/`payload`/`body` wrapper，并用元素 `cursor` 作为事件 ID fallback，覆盖非 GraphQL edges 的 wrapper item 响应。

本轮补充：remote history 普通事件数组现在也接受 JSON:API/resource-style 元素，事件 payload 可放在 `attributes` 或 `properties` 里，并使用外层 resource `id` 作为 SDK event ID fallback。

本轮补充：remote history response parser 现在也递归解包页级 JSON:API/resource `attributes`/`properties` wrapper，event-list 接受 `list`/`object`/`objects` aliases，并能把单个 `data.attributes` resource event 作为一条 SDK event 恢复。

本轮补充：remote history event-list 字段现在接受 keyed event map，例如 `events: {"evt_1": {...}}`，会按 key 稳定排序并在事件缺 ID 时用 map key 作为 fallback event ID。

本轮补充：contract content block `type` 解码现在会归一化 `toolUse`/`tool-result`/`cacheEdits`/`inputImage`/`chain-of-thought` 等 camel/kebab/compact 别名，transcript resume 可保留为规范 block type。

### M7: TUI Renderer And Interaction

目标：还原交互式 Claude Code 体验。

需要完成：

- Terminal renderer、layout、event、input、scroll、selection、alternate screen。
- REPL screen、PromptInput、Messages、StatusLine。
- permission dialogs、task dialogs。
- keybindings、vim mode、history/search。
- ANSI snapshots 和交互脚本。

当前状态：轻量 terminal frame renderer、PromptInput 状态机、history 导航、ctrl-p/ctrl-n history navigation、shift-enter 多行输入、多行 prompt 行内 ctrl-a/ctrl-e/ctrl-u/ctrl-k 和 wrap/render/cursor、共享 kill ring、ctrl-b/ctrl-f/ctrl-u/ctrl-k/ctrl-w 行编辑、alt-b/alt-f/alt-d/alt-backspace word 编辑、ctrl-left/ctrl-right/alt-left/alt-right word motion、ctrl-y yank 和 alt-y yank-pop 初版、reverse-search cursor/word 编辑/kill/yank/yank-pop 初版、ctrl-c interrupt/双击退出事件、ctrl-d delete-forward/空输入双击退出事件、ctrl-l 重绘事件、ctrl-o/ctrl-t 全局切换事件、ctrl-g/ctrl-s/ctrl-x chord chat 事件、reverse-search 状态/渲染/脚本断言/空结果/选择回填/cursor 断言、paste/image hint 输入和 OSC ST/base64 filename 兼容、text/image pasted-content 引用/metadata 脚本断言/提交展开/history entry restoration、SGR mouse 解析、alternate terminal navigation key sequences including modified Home/End/Delete/PageUp/PageDown、滚轮滚动、修饰键滚轮/左键、左键拖动选择、viewport 半页/顶部/底部可配置滚动、viewport 点击选择和 dialog action 点击、focus/blur 事件、resize 视口保持、keybinding resolver/config/chord pending/null-unbind/key/action camelCase alias、JSON config loader 和 focus/mouse/paste/image key name 覆盖、vim insert/normal/j/k/word/WORD/ge/gE/line-local ^/$/0/|/I/A/D/quote/bracket text-object/yank/register/paste/delete/count/replace/undo/find/till/repeat/matching-pair %/dot-repeat/G/gg/toggle/join/open-line/indent/substitute 动作、normal-mode arrow/backspace/delete 映射和 operator linewise/字符范围、REPL screen 模型、permission/task dialog builder、dialog kind/id routing/runtime/status line、runtime 到 REPL screen 的 dialog/status 同步、runtime-aware interaction script runner、prompt text/cursor/expanded/vim mode/register/task state/dialog result/runtime mutation/task bulk-cancel/permission cancel/keybinding mutation/status negative/snapshot negative/screen size/event-sequence/event-count/no-event/dialog-result-count/no-dialog-result 脚本断言、viewport 脚本断言、named-key 脚本输入、script JSON/JSONL/wrapper loader、script file runner 和 runtime/task camel field aliases、stale dialog race guard、cancel active、permission id/all cancellation、queued permission promotion、active task dialog refresh、task lifecycle/bulk-cancel 初版、idempotent alternate screen lifecycle/reset/reassert interactive 初版、mouse/focus/bracketed-paste terminal mode lifecycle/reconciliation、ANSI snapshot 基础、snapshot corpus write/compare/script-file compare/missing-baseline/diff/batch/strict unexpected-baseline 状态、scripted interaction runner/assertions/multi-key/text/paste/image/pasted-content metadata 初版、status/dialog/message components、viewport/selection 已落地；完整 ANSI parity、真实 permission/task runtime race/cancel 行为、完整 vim/keybinding 系统、完整 alternate screen lifecycle 和官方交互脚本仍缺。

本轮补充：Vim normal/operator motion 现在支持 `|` 1-based column motion，`5|` 可跳到当前逻辑行第 5 列，`d3|`/`c3|`/`y3|` 等 operator motion 会复用同一列范围并保留 register/dot-repeat 路径。

本轮补充：Vim normal/operator motion 现在支持 `%` matching-pair motion，可在当前逻辑行从下一个括号跳到匹配括号，并让 `d%`/`c%`/`y%` 使用 charwise inclusive 匹配范围。

本轮补充：scripted interaction 的布尔字段现在接受非严格 bool payload，包括 `"true"`/`"false"`、`yes`/`no`、`on`/`off` 和数字 `1`/`0`；覆盖 mouse release、dialog visible/result、prompt empty、vim state、reverse-search，以及 focus/cancel/openTasks/expectNoEvent/expectFocused 等顶层 step 控制字段，减少官方/外部交互脚本 fixture 因 bool 表示差异失败的情况。

本轮补充：scripted interaction 的 DOM key event replay 现在会把 `keyup`/`keyUp`/`key-release` 这类 release payload 当作 no-op，避免浏览器/Playwright 录制同时包含 keydown 和 keyup 时重复插入 prompt 输入。

本轮补充：scripted interaction 的 DOM key event replay 也会跳过 `Dead`/`Process`/IME key names、`isComposing` payload 和 composition event type，避免 IME/dead-key 录制 artifact 被当成普通 prompt 文本插入。

本轮补充：scripted interaction action replay 现在接受 DOM `beforeinput`/`input` event 的 `data` payload，`insertText` 等文本输入进入 prompt typing，`insertFromPaste`/drop variants 进入现有 pasted-content 路径。

本轮补充：scripted interaction DOM input replay 现在会把 `deleteContentBackward`、`deleteWordBackward`、`deleteHardLineForward`、`insertLineBreak` 等 `inputType` 映射到已有 prompt key action，覆盖浏览器录制的删除和换行事件。

本轮补充：scripted interaction key event object 现在接受 `repeatCount`/`count`/`times` 等数字重复次数字段，可把压缩后的连续 keydown 录制展开为多次按键回放，并设置上限避免异常 fixture 放大。

本轮补充：scripted interaction step 现在接受 `expect`/`expected`/`assertions`/`checks`/`verify`/`then`/`after` 等 expectation wrapper object，可把嵌套的 prompt/event/dialog/snapshot/screen/task/vim/viewport 断言映射到已有 `expect*` 字段。

本轮补充：scripted interaction expectation wrapper 现在也接受 assertion/check 数组，数组元素可用 `type`/`kind`/`name`/`target` 等 discriminator 搭配 `value`/`payload` 声明 prompt/event/dialog/snapshot/screen/task/vim/viewport 断言，减少官方脚本 fixture 的结构改写成本。

M7 补充：terminal input parser 和 configurable keybinding name parser 现在接受 xterm 扩展功能键 F13-F20，包括 `ESC [25~`、`ESC [26~`、`ESC [28~`、`ESC [29~`、`ESC [31~` 到 `ESC [34~`，以及 `f13`/`function-key-20` 等配置名。

本轮补充：scripted interaction assertion/check 数组元素的载荷现在也接受 `resource`/`node`/`attributes`/`properties`/`result`/`response`/`output`，让 JSON:API/resource-style 断言体可直接映射到 prompt/event/dialog/snapshot/screen/task/vim/viewport expectation。

本轮补充：keybinding config、keymap 解析和 interaction script named-key 输入接受 `ctrl-h`/`ctrl-i`/`ctrl-j`/`ctrl-m`、`ctrl-[`、`ctrl-?`、对应 `control-*` 以及 compact/camel 形式；terminal parser 支持 CSI-u/kitty keyboard protocol 的 ctrl/alt/shift-enter/shift-tab 序列；image hint parser 支持 OSC ST terminator 和 base64 `name=` filename；keybinding JSON loader 支持 wrapper object-map、`shortcuts`、object action 字段、string-array key sequence/chord 和 `null`/`false` unbind；mouse parser 支持 legacy X10/normal tracking 序列；interaction script 支持结构化 mouse/mouse_event 步骤，button 接受 `buttonMask`/`button_mask`/`btn`/`code`/`mask`，坐标接受 `mouseX`/`mouse_x`/`clientX`/`screenX`/`pageX`/`offsetX`/`viewportX` 和对应 Y/row/line 别名，release 接受 `mouseUp`/`isRelease`/`mouseRelease`/`releaseEvent` 等字段别名；interaction script 还支持字符串 `keys` 和 `input`/`input_text`/`keys_text`/`raw_key`/`paste_text` 字段别名，并允许 status/snapshot/viewport/pasted-content contains 断言使用单字符串或字符串数组，`keybindings`、`expectEvents`、`expectDialogResults`、`expectPrompt.pastedContents` 和 `expectTasks.contains` 使用单对象或对象数组。

本轮补充：keybinding config 和脚本 named-key 输入现在接受短修饰符别名，包括 `c-`/`m-`/`a-`/`opt-`/`s-` 以及 compact/camel 形式，可覆盖 control、meta、alt、option 和 shift key names。

本轮补充：keybinding config 和脚本 named-key 输入现在接受 `backtab`/`back-tab`/`btab` 等 Shift-Tab terminfo/fixture 别名，并映射到既有 focus-previous key surface。

本轮补充：keybinding JSON loader 现在递归解包 `data`/`payload`/`settings`/`config`/`keyboard`/`keymap` 等外层 wrapper，嵌套 preference export 中的 `bindings`/`shortcuts` 不需要手工扁平化。

本轮补充：keybinding JSON loader 现在也递归解包 JSON:API/resource-style `resource`/`attributes`/`properties`/`attrs` wrapper，API/preferences envelope 内的 `keybindings`/`keymap` 可直接加载。

本轮补充：keybinding JSON loader 现在把 `data`/`payload`/`body`/`result`/`response`、`resources`、`included`、`collection`/`list`/`children`/`values`、`nodes` 和 `items` 下的数组视为 binding list，数组元素也可直接使用 JSON:API/resource-style `resource`/`node`/`attributes`/`properties` wrapper。

本轮补充：keybinding JSON loader 现在也接受 GraphQL connection 风格的 `edges` binding list，binding item 可用 `edges[].node` 或 `edge.node` wrapper，外层可递归解包 `viewer`/`node`/`*Connection` wrapper。

本轮补充：keybinding JSON loader 现在接受 `keymap`/`keymaps`、`keyboardShortcuts`、`hotkeys`、`userKeybindings`、`customKeybindings` 等集合字段别名，并同时支持直接 object-map 和嵌套 `bindings` wrapper。

本轮补充：keybinding JSON loader 现在也接受 provider-style `choices`/`outputs`/`candidates`/`generations` response wrapper，可从 `message.content`、content-block array 和 `content.parts[].text` 里恢复 binding array 或 object map。

本轮补充：interaction script 的 per-step keybinding mutation 现在复用同一套 collection alias、object-map 和 JSON:API/resource wrapper 解析，脚本步骤可直接使用 `keymap`、`keyboardShortcuts`、`hotkeys`、`keyboard`、`preferences` 或 `keybindingConfig` 临时改键位。

本轮补充：interaction script 的 `keys` 字段现在支持 printable text chunk 和空格分隔 named-key sequence，例如 `ctrl-x ctrl-k`，减少官方脚本把连续输入拆成数组的改写成本。

本轮补充：interaction script key input 现在接受 press-style aliases，包括 `press`、`keyPress`、`keypress`、`shortcutKey`、`presses`、`keyPresses` 和 `shortcuts`。

本轮补充：interaction script loader 现在会扁平化 `cases`/`tests`/`testCases`/`scenarios`/`fixtures` 等 suite array，每个 case 内的 `steps`/`timeline`/`scriptSteps` 会按顺序展开，顶层数组也可直接混入 case object。

本轮补充：interaction script loader 现在也接受 provider-style `choices`/`outputs`/`candidates`/`generations` response wrapper，可从 `message.content`、content-block array 和 `content.parts[].text` 里递归恢复 script JSON。

本轮补充：interaction script 和 keybinding provider response 现在会剥离 fenced `json` code block，模型/SDK 返回 code-fenced 脚本或 keybinding 配置时不再需要手工去 fence。

本轮补充：interaction script 和 keybinding provider response 的 fenced JSON 提取现在接受 inline/glued fence 形态，例如语言标记后同一行直接跟脚本数组或 keybinding map，模型输出不换行时仍能加载配置。

本轮补充：interaction script 的 key/keySequence action payload 现在递归解包 JSON:API/GraphQL-style wrapper，wrapped single key 和 key sequence array 可直接驱动按键输入与组合键事件。

本轮补充：interaction script 的 direct key alias 字段现在也接受 wrapped object，`key`、`keyPress`、`keyPresses` 等字段可从 `resource.attributes` 或 `edge.node.attrs` 中恢复单键和 key sequence，避免官方 fixture 直接字段形态在 string/list decode 阶段失败。

本轮补充：interaction script 的 direct string alias 字段现在也接受 wrapped object，`text`、`pasteText`、`setStatus`、`snapshotName` 等字段可从 `resource.attributes` 或 `edge.node.attrs` 中恢复正文、paste、status 和 snapshot 名称，避免官方 fixture 直接字段形态在 scalar decode 阶段失败。

本轮补充：interaction script step 现在接受 `action`/`type`/`kind`/`name`/`operation` 动作判别别名，并可用 `value`/`payload`/`data` 等载荷字段驱动 key press、key sequence、text input、paste、status、resize、mouse/image 和 focus/blur 动作。

本轮补充：interaction script action/type/kind/name/operation 动作判别字段现在接受 compact/camel fixture aliases，例如 `typeText`、`inputText`、`insertText`、`keyPress`、`pressKey`、`keySequence`、`pasteText`、`pastedText`、`clipboardText`、`setStatus`、`statusLine`、`terminalSize` 和 `screenSize`。

本轮补充：interaction script 的 typeText/pasteText/setStatus/snapshot 字符串 action payload 现在递归解包 JSON:API/GraphQL-style wrapper，wrapped prompt text、paste text、status text 和 snapshot name 可直接驱动对应脚本步骤。

本轮补充：interaction script 的 resize/terminalSize/screenSize action payload 现在递归解包 `value`/`payload`/`data`/`resource`/`attributes`/`properties`/`attrs`/`edge.node` 等 wrapper，JSON:API/GraphQL fixture 中的 columns/rows 可直接驱动 screen resize。

本轮补充：interaction script 的 direct resize 数字 alias 字段现在也递归解包 wrapper；`resizeWidth`/`resizeHeight`、`screenWidth`/`screenHeight` 和 terminal width/height 相邻别名可从 wrapped `value`、`columns`、`rows` 中恢复尺寸，避免 direct field fixture 在 int decode 阶段失败。

本轮补充：interaction script 的 direct focus bool alias 字段现在也递归解包 wrapper；`focus`、`focused`、`focusIn`、`focusOut`、`blur`/`blurred` 可从 wrapped `enabled`、`value`、`selected` 等字段恢复焦点事件控制，避免 direct field fixture 在 bool decode 阶段失败。

本轮补充：interaction script 的 focus/blur action-discriminator 现在也接受 `value`/`payload`/`data` 中的 wrapped bool；`action:"focus"`、`kind:"blur"`、`operation:"focusState"` 和 `name:"setFocus"` 可用 `focused:false`、`blurred:false` 或非严格 bool payload 明确发出 focus-out/focus-in。

本轮补充：interaction script 的 direct expectation bool alias 字段现在也递归解包 wrapper；`expectNoEvent`、`expectNoDialogResult(s)`、`expectFocused` 可从 wrapped `value`/`enabled`/`selected` 恢复断言控制，避免 direct expectation fixture 在 bool decode 阶段失败。

本轮补充：interaction script 的 direct expectation count alias 字段现在也递归解包 wrapper；`expectEventCount`、`expectTotalEventCount`、`expectDialogResultCount`、`expectTotalDialogResultCount` 可从 wrapped `value`/`count`/`total` 恢复计数断言，避免 direct expectation fixture 在 int decode 阶段失败。

本轮补充：interaction script 的 UpperCamel step 字段现在也复用 raw/wrapper 解析；`UpsertTask`、`CancelAllTasks`、`OpenTasksDialog`、`ExpectEvent(s)`、`ExpectDialog`、`ExpectDialogResult(s)`、`ExpectPrompt`、`ExpectVim`、`ExpectScreen`、`ExpectViewport`、`ExpectReverseSearch`、`ExpectTasks` 等 Go 默认字段名可直接接受 JSON:API/GraphQL-style wrapper，task status/expectation 的 ID/title/state/detail/progress、tasks expectation 的 Count/StateCounts、event expectation 的 Type/Value/DialogID、dialog expectation 的 Active/ActionCount/FocusedIndex、dialog-result expectation 的 ID/Found/Stale、prompt expectation 的 Cursor/PastedContentCount/NextPastedID/Empty、pasted-content expectation 的 ID/MediaType/Filename/ContentContains、vim expectation 的 Enabled/Mode/Register/RegisterLinewise、screen expectation 的 Width/Height、viewport expectation 的 Offset/VisibleLineCount 和 reverse-search expectation 的 Cursor/ResultCount/NoResults 改为 raw-first 解码并继续接受数字字符串/非严格 bool。

本轮补充：interaction script 的 direct expectation string-list alias 字段现在也递归解包 wrapper；`expectStatusContains`/`NotContains` 和 `expectSnapshotContains`/`NotContains` 可从 wrapped `value`/`values`/`contains`/`items` 恢复断言列表，避免 direct expectation fixture 在 list decode 阶段失败。

本轮补充：interaction script 的 direct expectation collection alias 字段现在也递归解包 wrapper；`expectEvents`、`expectDialogResults` 可从 wrapped `events`/`results`/`items`/`nodes` 中恢复结构化断言列表，避免 wrapper collection fixture 被当成空对象断言。

本轮补充：interaction script 的 direct single expectation alias 字段现在也递归解包 wrapper；`expectEvent`、`expectDialogResult` 可从 wrapped `event`/`result`/`expected` 中恢复结构化单项断言，避免 wrapper fixture 被当成空 expectation。

本轮补充：interaction script 的 direct single expectation alias 字段现在改为 raw payload 解析；`expectEvent`、`expectDialogResult` singular 字段也可接受单元素数组并取首项，避免 API/fixture 把 singular expectation 包成数组时在基础 step unmarshal 阶段提前失败。

本轮补充：interaction script 的 direct prompt expectation 现在也递归解包 JSON:API/GraphQL-style wrapper；`expectPrompt`/`expect_prompt` 可从 `resource.attributes`、`edge.node.attrs` 等外壳中恢复 prompt text、cursor、empty、pasted-content count 和 next pasted ID 断言，避免 wrapper fixture 静默变成空 prompt expectation。

本轮补充：interaction script 的 direct vim expectation 现在也递归解包 JSON:API/GraphQL-style wrapper；`expectVim`/`expect_vim` 可从 `resource.attributes`、`edge.node.attrs` 等外壳中恢复 enabled、mode、register 和 register-linewise 断言，避免 wrapper fixture 静默变成空 Vim expectation。

本轮补充：interaction script 的 direct screen/viewport expectation 现在也递归解包 JSON:API/GraphQL-style wrapper；`expectScreen`/`expect_screen` 和 `expectViewport`/`expect_viewport` 可从 `resource.attributes`、`edge.node.attrs` 等外壳中恢复 columns/rows、scroll offset、visible line count 和 visible contains/not-contains 断言，避免 wrapper fixture 静默变成空 screen/viewport expectation。

本轮补充：interaction script 的 direct task/reverse-search expectation 现在也递归解包 JSON:API/GraphQL-style wrapper；`expectTasks`/`expect_tasks` 和 `expectReverseSearch`/`expect_reverse_search` 可从 `resource.attributes`、`edge.node.attrs` 等外壳中恢复 task count/stateCounts/contains、reverse active/query/cursor/current/result-count 断言，并保留 wrapped `active:false` 与 `taskCount:0`。

本轮补充：interaction script 的 direct dialog expectation 现在也递归解包 JSON:API/GraphQL-style wrapper；`expectDialog`/`expect_dialog` 可从 `resource.attributes`、`edge.node.attrs` 等外壳中恢复 active、ID/kind、title/body、body/action contains、action count 和 focused index 断言，并保留 wrapped `active:false`。

本轮补充：interaction script action/type/kind/name/operation 动作判别字段现在也接受 compact/camel event/media aliases，包括 `focusIn`、`focusOut`、`mouseEvent`、`pasteImage` 和 `imagePaste`。

本轮补充：interaction script 的 mouseEvent/pasteImage action payload 现在递归解包 JSON:API/GraphQL-style `resource`/`attributes`/`properties`/`attrs`/`edge.node` wrapper，mouse payload 的 Button/X/Y/Release 可接受数字字符串/非严格 bool，wrapped mouse 坐标/按钮和 image filename/media/content 可直接驱动 dialog click 与 image paste。

本轮补充：interaction script action/type/kind/name/operation 动作判别字段现在也能驱动 runtime/dialog mutation，支持 `requestPermission`、`taskStatus`、`showTasks`、`cancelTasks`、`removeTask` 和 `showDialog` 等动作，并从 `value`/`payload`/`data`/`body` 载荷解析 permission/task/dialog 对象、task/permission ID 或取消原因。

本轮补充：interaction script 的 runtime permission/task payload 现在递归解包 `value`/`payload`/`data`/`resource`/`attributes`/`properties`/`attrs`/`edge`/`node`、`included`/`collection`/`list`/`values` 等 JSON:API/GraphQL wrapper，并把 resource/node 顶层 ID 回填到内层 runtime 对象；带明确非 permission/task `type` 的 included resource 会被跳过，避免 wrapper-only payload 或 metadata resource 被误解析成 runtime 对象。

本轮补充：interaction script 的 direct runtime mutation 字段现在改为 raw payload 解析；`requestPermission`/`request_permission` 和 `upsertTask`/`upsert_task` singular 字段也可接受单元素数组并取首项，避免 API/fixture 把 mutation payload 包成数组时在基础 step unmarshal 阶段提前失败。

本轮补充：interaction script 的 direct mouse 字段现在改为 raw payload 解析；`mouse`/`mouse_event`/`mouseEvent` singular 字段可接受单元素数组并递归解包 `resource.attributes`、`edge.node.attrs` 等 wrapper，避免录制脚本把鼠标事件包成 API payload 时提前解析失败。

本轮补充：interaction script 的 direct message list 字段现在改为 raw payload 解析；`messages`/`appendMessages`/`transcriptMessages` 可递归解包 `resource.attributes`、`data[]`、`edge.node.attrs` 等 API/GraphQL wrapper，并保留 image paste id 等 message metadata。

本轮补充：interaction script 的 direct single message 字段现在也复用 raw message parser；`message`/`Message` 可递归解包 `resource.attributes`、`edge.node.attrs`，并在数组形态下回退为多条 message 追加，避免单条消息 wrapper 被基础解码成空消息。

本轮补充：interaction script 的 direct dialog 字段现在改为 raw payload 解析；`dialog`/`Dialog` 可递归解包 `resource.attributes`、`edge.node.attrs` 和单元素数组，`showDialog` action payload 也复用同一解析路径，dialog payload 的 `ID`/`Focused` 可接受数字和数字字符串，避免 wrapper-only 或 Go 默认字段名 dialog 被解码失败/空弹窗。

本轮补充：interaction script 的 direct image 字段现在改为 raw payload 解析；`image`/`Image` 可复用 pasteImage action 的 wrapper 解析，递归解包 `resource.attributes`、`edge.node.attrs` 和单元素数组，保留 filename/media/content/source-path 后再生成 prompt pasted-image。

本轮补充：interaction script 的 direct keybinding mutation 字段现在改为 raw-only 解析；`keybindings`/`key_bindings`/`keyBindings`/`keybindingSpecs`/`Keybindings` 不再先走 `[]BindingSpec` 基础解码，统一复用 keybinding loader 的 object-map、resource wrapper 和 edge/node collection parser。

本轮补充：interaction script 的 runtime mutation action 现在也递归解包 wrapped `removeTask`/`cancelPermission` ID 和 `cancelTasks` cancellation detail；`payload.resource.attributes`、`edge.node` 和相邻 API envelope 可直接驱动 task removal、permission cancellation 与 task bulk-cancel。

本轮补充：interaction script 的 direct runtime mutation alias 字段现在接受 object payload；`removeTask: {resource:{id}}`、`cancelPermission: {edge:{node:{id}}}`、`cancelTasks: {resource:{attributes:{reasonText}}}` 会走同一递归解析路径，不再被 string/bool alias 字段提前拒绝。

本轮补充：interaction script action 的 boolean payload 现在也递归解包 JSON:API/GraphQL wrapper；`cancelTasks`/`openTasks` 等动作会尊重 `payload.resource.attributes.enabled:false`、`edge.node.attrs.open:false` 这类 wrapped false，而不是因为 object payload fallback 成 true。

本轮补充：interaction script step 接受 `resize`/`terminalSize`/`screenSize` 对象或 `[width,height]` 数组、顶层 `columns`/`rows` resize 别名、`focus`/`focused`/`blur`/`focusIn`/`focusOut` focus event 别名、`snapshot`/`snapshotId`/`snapshotLabel` capture 名称别名，以及 runtime-aware mutation 别名如 `permission`/`permissionRequest`、`task`/`taskStatus`、`removeTask`/`deleteTask`、`cancelPermission`、`cancelTasks`/`cancelReason`、`openTasks`/`showTasks`。

本轮补充：interaction script step 可通过 `status`/`setStatus`/`statusLine`/`baseStatus` 设置状态行；runtime-aware scripts 会把它作为 base status，并继续叠加 permission/task 计数，便于复用带状态栏的 ANSI/interaction fixture。

本轮补充：interaction script 批量消息注入接受 `messages`、`appendMessages` 和 `transcriptMessages` 等字段，并允许单对象自动转数组，便于把 transcript/chat fixture 直接迁入脚本。

本轮补充：interaction script direct `dialog` step 接受和 dialog expectation 对齐的 ID/kind/title/body/actions/focused aliases，减少自定义 dialog fixture 的手工改写。

本轮补充：interaction script loader 接受更多 steps wrapper aliases 和一层 scenario/fixture 嵌套对象，减少把 golden fixture 改写成本地专用格式的需求。

本轮补充：interaction script 单步 JSON 现在接受 `step`/`scriptStep`/`interactionStep`/`record`/`entry`/`item`/`event` wrapper，可用于数组元素、JSONL 行和 wrapper object 中的 steps item，减少录制脚本逐行改写成本。

本轮补充：interaction script 单步 JSON 现在也接受 JSON:API/resource-style `resource`/`node`/`attributes`/`properties` wrapper，数组元素和 JSONL 行可直接使用 API fixture 的 step resource 形态。

本轮补充：interaction script 顶层 wrapper 现在接受 `records`/`recordedSteps`/`events`/`entries`/`items`/`actions`/`timeline` 数组入口，并复用单步 wrapper 拆包逻辑。

本轮补充：interaction script loader 现在接受 `data`/`payload`/`body`/`result`/`response`/`recording`/`session`/`run` 等外层对象 wrapper，可继续递归查找 steps/records/events/timeline。

本轮补充：interaction script loader 现在也接受 JSON:API/resource-style `resource`/`attributes`/`properties` wrapper，可从 attributes/properties 内继续解析 `steps`/`records`/`timeline`。

本轮补充：interaction script loader 现在把 `data`/`payload`/`body`/`result`/`response`、`resources` 和 `nodes` 下的数组也视为 step list，可直接加载 API/GraphQL collection envelope，同时保留单步 `data` 载荷兼容。

本轮补充：interaction script loader 现在接受 GraphQL connection 风格的 `edges` step list 和 JSON:API/HAL collection 风格的 `included`、`collection`/`list`/`children`/`values` step list，数组元素可用 `edges[].node`、`edge.node`、`resource.attributes` 或 `resource.properties` wrapper，外层也可递归解包 `viewer`/`node`/`*Connection` wrapper 来加载录制脚本。

本轮补充：interaction script loader 现在也接受 provider-style `choices`/`outputs`/`candidates`/`generations` response wrapper，可从 `message.content`、content-block array 和 `content.parts[].text` 里恢复 script JSON，减少模型/SDK 录制脚本响应的手工拆包。

本轮补充：TUI Vim prompt editing 增加基础 visual/visual-line 模式，支持 `v`/`V` 进入选择、motion 扩展 selection、visual `o` 切换 active end、visual `<`/`>` 行缩进/反缩进、visual `~` 大小写切换、visual `u`/`U` 小写/大写转换、`y`/`d`/`c` 以及常用 visual `x`/`s` aliases 作用于选择范围、Esc 回到 normal，并让 interaction script 可用 `visual`/`visualLine` 断言当前 mode。

本轮补充：TUI Vim prompt editing 支持 normal-mode `gv` 重新进入上一次 characterwise/linewise visual selection，后续 visual operator 会复用恢复出的选择范围。

本轮补充：TUI Vim prompt editing 增加 `gu`/`gU`/`g~` case-conversion operator，复用 motion、linewise、find/till、text-object 和 dot-repeat operator 管线，并保持大小写转换不写入 yank register。

本轮补充：TUI Vim prompt editing 增加 normal-mode `gJ` raw line join，不插入/规范化空格，并接入 dot-repeat。

本轮补充：TUI Vim prompt editing 增加 visual/visual-line `J`/`gJ` 行拼接，支持选择范围内的 whitespace-normalized join 和 raw join，并沿用 undo、`gv` selection 记忆和 dot-repeat change 记录。

本轮补充：TUI Vim prompt editing 增加 visual/visual-line `p`/`P` 粘贴替换 selection，支持 characterwise 和 linewise register，替换出的文本会回写 unnamed register，并避免行选择替换到末尾时留下额外空行。

本轮补充：TUI Vim prompt editing 增加 visual/visual-line `r{char}` selection 替换，按选区把非换行字符替换为目标字符，保留行结构、接入 undo，并让 `gv` 能重选替换前的 visual range。

本轮补充：TUI Vim prompt editing 增加 normal-mode `R` replace mode，输入会从当前 cursor 开始覆盖现有字符、超过文本尾部时追加，并接入 undo 与 dot-repeat。

本轮补充：TUI Vim prompt editing 增加 prompt-local marks，支持 `m{mark}` 设置位置、`` `{mark}` 精确跳转、`'{mark}` 跳到 mark 所在行首，并支持 `d`/`c`/`y` 等 operator 以 mark 作为 motion。

本轮补充：TUI Vim prompt editing 增加基础 macro 录制和回放，支持 `q{reg}` 开始录制、normal-mode `q` 停止、`@{reg}` 按 count 回放，以及 `@@` 重放上一 macro。

本轮补充：TUI Vim prompt editing 增加 prompt-local `/` 和 `?` 搜索模式，支持 Enter 执行、Esc 取消、Backspace 编辑查询、wraparound 匹配，以及 `n`/`N` 重复上一搜索方向或反向搜索。

本轮补充：TUI Vim prompt editing 将 `/` 和 `?` 搜索接入 operator motion，支持 `d/search`、`c?search`、搜索 count、取消清理 pending 状态，以及 search operator 的 dot-repeat 记录。

本轮补充：TUI Vim prompt editing 将 `/`、`?`、`n` 和 `N` 接入 visual/visual-line 模式，搜索会临时进入 search prompt、Enter 后恢复 selection 并移动 active end，Esc 取消时保留原 visual selection。

本轮补充：TUI Vim prompt editing 增加 named register 前缀，支持 normal/operator/visual 路径里的 `"{reg}` yank/delete/paste、uppercase register append、black-hole register no-op，以及普通移动命令后清理未使用的 register selection。

本轮补充：TUI Vim prompt editing 的 normal-mode `x`/`X` 现在会把删除字符写入 unnamed 或 selected named register，并保持 `.` dot-repeat 删除路径继续可用。

本轮补充：TUI Vim prompt editing 现在支持 visual/visual-line `Y`/`D`/`C` linewise aliases，字符 visual 选区也会按所在整行 yank/delete/change，并保持 unnamed/named register 的 linewise 内容一致。

本轮补充：prompt history 写入现在保留 image pasted-content 的 media type、filename、dimensions 和 image-cache source path 元数据，同时继续不把 inline base64 image bytes 或 text-paste hash 写进图片历史记录。

本轮补充：prompt history 读取旧 image pasted-content 记录时，如果缺少 source path 但对应 session 的 image-cache 文件仍存在，会自动补回 source path 并刷新内存 image path cache。

本轮补充：interaction script key 字段现在接受 DOM-style key event object，可从 `key`/`code`（包括 `Numpad*`、扩展数字区括号/hash/backspace 和标点 key code）、旧式 `keyIdentifier`、数字 `keyCode`/`which`/`charCode`（包括标点和数字区运算符）、`keypress.which` 字符码、`ctrlKey`/`altKey`/`metaKey`/`shiftKey` 和 `modifiers` 数组还原现有 key 名，wrapper payload 中的 key event 也可驱动脚本回放。

本轮补充：interaction script mouse payload 现在可从 `mouseup`、`pointerUp`、`touchend` 等 event type 推导 release 状态；显式 release bool 仍优先生效。

本轮补充：interaction script mouse payload 现在可从 `wheel`/`mousewheel`、`scrollUp`/`scrollDown`、`direction`、`deltaY` 和旧式 `wheelDelta` 推导 SGR wheel button，录制的 DOM/compact 滚轮事件可直接驱动 viewport 滚动。

本轮补充：interaction script mouse payload 现在接受 DOM `which` 和 `buttons`/`buttonState` bitmask，并映射到 SGR left/middle/right button，避免录制脚本的右键/中键被误当成 primary click。

本轮补充：interaction script mouse payload 现在会把 DOM `mousemove`/`pointermove` 的 buttonless motion 映射成 SGR `35`，把带 `buttons`/`which` 的 move/drag 映射成 SGR motion button，避免 hover/move 录制事件误触发 dialog/viewport primary click。

本轮补充：interaction script touch payload 现在可从 `touches`/`targetTouches`/`changedTouches` 的首个 touch point 恢复坐标，`touchmove` 映射为 SGR drag motion，`touchcancel` 映射为 release，减少 DOM touch 录制 fixture 的手工改写。

本轮补充：REPL dialog 鼠标处理现在忽略 SGR motion/drag button，只响应实际 press/click，避免 pointer/touch move 回放关闭 permission/task dialog。

本轮补充：interaction script paste payload 现在接受 DOM `clipboardData`/`dataTransfer` 对象，可从 `text/plain`、`plainText` 和 `items[].text` 恢复 pasted text，减少 ClipboardEvent 录制 fixture 的手工改写。

本轮补充：interaction script paste payload 现在也可从 DOM `clipboardData.items` 和 `dataTransfer.files` 中恢复 `image/*` file item，映射为已有 image paste，并避免把 image file 的 `data`/`base64` 内容误当成普通 pasted text。

本轮补充：interaction script clipboard/dataTransfer image paste 现在也优先读取 `items[].file`、`items[].blob`、`items[].getAsFile` 等嵌套 file payload，保留真实 filename、media type、base64 和 source path，而不是只保留外层 MIME。

本轮补充：interaction script resize payload 现在接受 DOM/window 尺寸别名 `innerWidth`/`innerHeight`、`clientWidth`/`clientHeight`、`offsetWidth`/`offsetHeight` 和 ResizeObserver 风格 `contentRect`/`target` wrapper。

本轮补充：interaction script resize payload 现在接受 ResizeObserver `contentBoxSize`/`borderBoxSize` 数组里的 `inlineSize`/`blockSize` 字段，覆盖现代浏览器 box-size 事件形态。

本轮补充：prompt/image history 的 `ImageDimensions` 读取 `width`/`height` 或仅 original 尺寸时，会默认 display 尺寸等于 original 尺寸，避免只有单尺寸字段的 image fixture 丢失 source metadata。

本轮补充：prompt history 的 pasted-content 类型现在会归一化 `inputImage`/`pasted-image`/`input_text`/`pasted-text` 等别名，runtime history 和 stored history 恢复都会映射到规范 `image`/`text`。

本轮补充：prompt history 与 interaction script 的 pasted-content ID 现在接受 `pastedContentId`/`attachmentID`/`contentID`/`imageID` 等别名，并容忍数字字符串，数组和单对象 attachment fixture 可保留原始 pasted-content ID。

本轮补充：prompt history 的 `HistoryEntry`/`LogEntry` 以及 `pastedContents`/`pasted_contents` item 现在接受 `entry`/`record`/`item`/`payload` 等 wrapper；pasted contents 除 map 外也接受数组和单对象，runtime history 与 stored history 都会按内容内 ID/ID 别名重建 map。

本轮补充：prompt history 的 `HistoryEntry`/`LogEntry` 和 pasted-content item 现在也递归解包 `edge`/`node`/`resource`/`attributes`/`properties`/`attrs` wrapper，GraphQL/JSON:API history exports 可直接恢复 prompt 与附件 metadata。

本轮补充：prompt/history pasted-content 引用解析现在接受大小写差异和 `pasted image`/`input-image`/`input_text` 等占位符别名，文本展开、图片引用过滤和 next pasted ID seed 共用同一识别面。

本轮补充：prompt history `LogEntry` 读取现在接受 `projectPath`/`cwd`/`workingDirectory`/`workspacePath` 等 project 别名，以及 `createdAt`/`unixTimestamp` 等 timestamp 别名；RFC3339 时间会归一为毫秒时间戳，避免旧 history 因字段名不同被 project/session 过滤漏掉。

本轮补充：prompt history 的显示文本读取现在接受 `prompt`/`text`/`input`/`content`/`value` 等 display 别名，runtime history 和 stored history 都不会因旧字段名把 prompt 恢复成空字符串。

本轮补充：prompt history 的 pasted-content 容器字段现在和 interaction script 对齐，接受 `pastedContent`/`pasted_content`、`pasteContents`/`paste_contents`、`pastes`、`attachments`/`attachment` 等别名，runtime history 和 stored history 都可复用 attachment 风格 fixture。

本轮补充：snapshot corpus 支持 `.ansi` only baselines，方便复用真实终端输出 corpus，而不必预先生成 `.txt` companion 文件。

本轮补充：terminal lifecycle 增加可选 extended-key mode，按官方 `CSI >1u`/`CSI >4;2m` 启用 kitty keyboard protocol 和 modifyOtherKeys，退出时重置 modifyOtherKeys 并 pop kitty stack，reassert 时先 pop 再 push，避免长期会话 stack 泄漏。

本轮补充：terminal CSI-u/kitty keyboard parser 接受无 modifier 字段或 modifier `1` 的 base key 序列，覆盖 printable rune、Enter、Tab、Esc 和 Backspace，避免 extended-key 模式下普通键序列被解析成 unknown。

本轮补充：terminal CSI-u/kitty keyboard parser 现在把 shift-only Backspace (`CSI 8;2u`/`CSI 127;2u`) 仍映射到 Backspace，避免 kitty extended-key 模式下退格被误当作 DEL rune 或 unknown。

本轮补充：terminal CSI parser 把 DA/device attributes (`CSI c`、`CSI >c`、`CSI =c`) 归入 report action，并在 terminal parser dispatcher 中作为 `TerminalActionReport` 暴露。

本轮补充：terminal CSI parser 接受 `CSI a`/`CSI e`/`CSI \`` cursor alias final bytes，并映射到已有 cursor-forward/cursor-down/cursor-column actions。

本轮补充：terminal CSI parser 接受 ECMA `CSI Ps j` / `CSI Ps k` HPB/VPB backward cursor final bytes，并映射到已有 cursor-back/cursor-up actions。

本轮补充：terminal CSI parser 接受 DEC private mode `?1047h/l` alternate-screen buffer 和 `?1048h/l` save/restore cursor，复用已有 mode/cursor actions。

本轮补充：terminal CSI parser 把 DECREQTPARM terminal-parameters (`CSI x`) 归入 report action，保留 code/private marker。

本轮补充：terminal CSI parser 把 DECRQM mode request (`CSI Ps $ p` / `CSI ? Ps $ p`) 归入 report action，保留 mode code 和 DEC private marker。

本轮补充：terminal CSI parser 把 xterm window manipulation/report (`CSI t`) 归入 report action，覆盖常见 `CSI 14t`/`CSI 18t` 查询，并把 `CSI 4;height;width t` 与 `CSI 8;rows;cols t` 的 pixel/text-area 尺寸参数结构化暴露。

本轮补充：terminal CSI parser 把 TBC tab-clear (`CSI g`/`CSI 3g`) 归入 cursor action，保留 clear-current/all code。

本轮补充：terminal CSI parser 把 REP repeat-preceding-character (`CSI b`) 归入 edit action，visible-text/snapshot pipeline 和 ANSI message wrapping/trim 会按重复次数展开前一个可重复 grapheme。

本轮补充：terminal CSI parser 把 DECSTR soft reset (`CSI !p`) 归入 reset action，并在 terminal parser 中清理 SGR/link 状态。

本轮补充：terminal ESC parser 把 DEC line/screen attribute (`ESC # 3/4/5/6/8`) 归入 screen action，terminal parser 会结构化透传 double-height top/bottom、single/double-width 和 alignment-test 控制，继续减少真实 ANSI 输出里的 unknown fallback。

本轮补充：terminal ESC parser 把 DECID identify-terminal (`ESC Z`) 归入 report action，terminal parser 会像 `CSI c` 一样暴露为 device-attributes report，避免老式终端识别查询落入 unknown。

本轮补充：renderer/snapshot 增加 opt-in DEC 2026 synchronized output 包裹入口，可用官方 BSU/ESU (`CSI ?2026h`/`CSI ?2026l`) 生成整帧 ANSI fixture，同时默认渲染保持不变。

本轮补充：terminal OSC helper 增加 OSC 0 title/icon 序列生成，输入会先 strip ANSI；`StripANSI` 现在会完整跳过 OSC/DCS/APC/PM/SOS payload，避免 title/snapshot 可见文本被终端控制串污染。

本轮补充：terminal OSC helper 增加 OSC 21337 tab status 序列、清理序列和 tmux/screen passthrough 包裹，status 文本按官方规则转义分号和反斜杠。

本轮补充：terminal OSC helper 增加 OSC 8 hyperlink 开始/结束序列，按官方 rolling hash 为 URL 自动生成 `id=`，并允许显式 params 覆盖。

本轮补充：terminal OSC helper 增加 OSC 9;4 progress 序列，覆盖 clear/set/error/indeterminate，running/error 百分比按官方规则 clamp 到 0..100。

本轮补充：terminal OSC helper 增加 iTerm2、Kitty、Ghostty notification 序列和 raw BEL helper，调用方可按环境选择是否 wrap multiplexer。

本轮补充：terminal OSC helper 增加 OSC 52 clipboard 序列生成，支持默认 `c` selection、显式 clipboard selection 和 clear 序列，并按 UTF-8 base64 编码 payload；native clipboard/tmux buffer runtime 仍未接入。

本轮补充：terminal OSC helper 增加显式 ST (`ESC \\`) terminator 入口，可按官方 Kitty 避免 BEL 的路径生成 OSC 序列，同时默认 `OSCSequence` 仍保持 BEL terminator。

本轮补充：terminal OSC helper 增加 OSC color parser，支持 `#RRGGBB` 和 XParseColor 风格 `rgb:R/G/B`，按官方规则把 1-4 位 hex component 缩放到 8-bit RGB。

本轮补充：terminal OSC parser 把 OSC 10-19 dynamic color 设置/查询解析为结构化 color action，复用既有 OSC color parser，并支持同一序列内按官方递增规则连续设置多个 target；visible text/snapshot 继续剥离这些控制串。

本轮补充：terminal OSC parser 把 OSC 110-119 dynamic color reset 序列解析为结构化 color reset action，覆盖前景/背景/光标、pointer、Tektronix 和 highlight 动态色 reset。

本轮补充：terminal OSC parser 把 OSC 4 palette color 设置/查询和 OSC 104 palette reset 解析为结构化 palette action，支持同一序列内多组 index/color、index/? 和按 index reset。

本轮补充：terminal OSC parser 把 OSC 5 special color 设置/查询和 OSC 105 reset 解析为结构化 specialColor action，限制 special index 为 0-4，并保持 visible text/snapshot 清洁。

本轮补充：terminal OSC helper 增加 OSC 21337 tab-status payload parser，支持 `\;`/`\\` 转义、bare key 或空值清理、unknown key ignore，并复用 OSC color parser 解析 indicator/status-color。

本轮补充：terminal OSC helper 增加 OSC 8 hyperlink payload parser，按官方规则解析 params、保留 URL 内部分号，并把空 URL 识别为 hyperlink end。

本轮补充：terminal OSC helper 增加轻量 `ParseOSCContent`，覆盖官方 title(0/1/2)、OSC 8 hyperlink、OSC 21337 tab status 和 unknown action 分支。

本轮补充：terminal OSC helper 增加完整 OSC sequence parser，可从带 `ESC ]` 前缀且以 BEL 或 ST 终止的序列解析出 `ParseOSCContent` action。

本轮补充：terminal OSC parser 现在把 OSC 52 clipboard、iTerm2 progress/notification、Kitty notification 和 Ghostty notification 解析为结构化 terminal actions，snapshot/visible-text replay 会继续剥离这些控制串。

本轮补充：terminal OSC parser 现在把 OSC 133/633 shell integration marks (`A`/`B`/`C`/`D`) 解析成结构化 shellIntegration action，并保留 command-end exit code，真实 shell 输出的 prompt/command 标记不会落入 unknown。

本轮补充：terminal OSC parser 现在识别 VS Code OSC 633 `E` command-line 和 `P` property 记录，保留 raw value，并把 semicolon property payload 解析成结构化 metadata，visible text/snapshot replay 继续剥离这些 shell 标记。

本轮补充：terminal OSC parser 现在解析 OSC 7 current-directory URI，保留 raw URI 并暴露 scheme/host/path，TerminalParser 会输出 directory action，snapshot/visible-text replay 继续剥离该控制串。

本轮补充：terminal renderer constants 增加官方 clear scrollback (`CSI 3J`) 和 legacy Windows home (`CSI 0f`) 序列 helper，支持现代 clear-screen+scrollback 和 legacy Windows clear 组合；平台自动探测仍留给调用方。

本轮补充：terminal CSI helper 增加通用 `CSISequence`、cursor up/down/forward/back/position/move 和 line/screen erase 序列，按官方 helper 的零移动返回空串与 horizontal-first cursorMove 行为生成 ANSI。

本轮补充：terminal CSI helper 增加 scroll up/down、set scroll region 和 reset scroll region 序列，scroll 零值返回空串，便于后续补齐官方 viewport/scroll-region 输出路径。

本轮补充：terminal CSI helper 增加 DECSCUSR cursor-style 序列，覆盖 block/underline/bar 的 blinking 与 non-blinking code，并保留 unknown style 的默认 cursor fallback。

本轮补充：terminal CSI helper 增加 bracketed paste start/end 和 focus in/out 输入 marker 常量，并用现有 parser 验证 focus marker 映射，方便官方交互 fixture 复用原始 CSI marker。

本轮补充：terminal CSI helper 增加 `EraseLinesSequence(n)`，按官方 `eraseLines` 语义逐行 `CSI 2K`、行间上移并以 `CSI G` 回到列 1，`n<=0` 返回空串。

本轮补充：terminal CSI helper 增加官方 CSI param/intermediate/final byte range 常量和判定函数，为后续更完整 CSI parser/action tests 提供基础。

本轮补充：terminal CSI helper 增加官方 CSI final-byte/DEC mode 常量和 `ParseCSISequence` 动作解析，覆盖 cursor move/position/save/restore/show/hide/style、erase display/line/chars、scroll up/down/region、SGR params、alternate-screen/bracketed-paste/mouse/focus mode 和 unknown sequence fallback。

本轮补充：terminal CSI parser 现在支持多参数 mode set/reset 序列，例如 `CSI ?1000;1006;2004h` 和 `CSI 4;20l`，在保持单 `Mode` 兼容字段的同时通过 `Modes` 暴露完整 mode 列表。

本轮补充：terminal CSI parser 现在对混合 cursor visibility 和 mode 的多参数序列（如 `CSI ?25;1000h`）保留完整 mode list，避免真实终端初始化/恢复序列只暴露 cursor action 而丢失后续 mode。

本轮补充：terminal CSI parser 补齐 insert/delete chars、insert/delete lines、forward tab/back tab action，`CSI M` 在 output parser 中按 delete-lines 处理，同时 input tokenizer 仍保留 X10 mouse payload 边界消费。

本轮补充：terminal CSI parser 现在把 DSR (`CSI n`) 解析成 report action，覆盖 device-status、cursor-position 和 private-mode unknown report，避免 terminal status query/response 序列继续落入 generic unknown。

本轮补充：terminal CSI parser 现在把 DEC `?1006h/l` SGR mouse mode 解析成 mouseTracking action，和 lifecycle 发出的 SGR mouse enable/disable 序列闭环。

本轮补充：terminal CSI parser 现在把 DEC `?9h/l` X10 mouse tracking mode 解析成 mouseTracking `x10` action，和 input tokenizer/parser 的 X10 mouse payload 支撑闭环。

本轮补充：terminal CSI parser 现在也把 DEC `?1001h/l` highlight、`?1005h/l` UTF-8 mouse mode、`?1015h/l` urxvt numeric mouse mode 和 xterm `?1016h/l` SGR-pixels mouse mode 解析成 mouseTracking action，和输入侧 numeric/SGR mouse 兼容面闭环。

本轮补充：terminal CSI parser 现在把 xterm `?1007h/l` alternate scroll mode 解析成独立 mode action，避免 alternate-screen wheel 兼容序列落入 unknown。

本轮补充：terminal CSI parser 现在把 DEC `?1046h/l` alternate-screen switching mode 解析成独立 `alternateScreenSwitching` action，和 `?1047/?1049` 的实际 alternate-screen buffer 切换区分开。

本轮补充：terminal CSI parser 现在把 DEC `?2026h/l` synchronized output mode 解析成 mode action，和 renderer/snapshot 的 BSU/ESU 包裹路径闭环。

本轮补充：terminal ESC helper 增加官方 ESC final-byte 判定和 `ParseESCSequence`/`ParseESCContent`，覆盖 RIS reset、DECSC/DECRC save/restore、IND/RI/NEL cursor action、HTS、charset selection 和 unknown sequence fallback。

本轮补充：terminal SGR helper 增加官方 `TextStyle` 状态解析基础，覆盖 reset、bold/dim/italic/underline/blink/inverse/hidden/strikethrough/overline、普通/亮色命名色、256 色、RGB 色、underline color、分号和冒号参数格式；完整 ANSI parser/render style parity 仍继续推进。

本轮补充：terminal sequence dispatcher 增加官方 `identifySequence` 等价分流，按 CSI/OSC/ESC/SS3/unknown 识别并委派现有 parser，SS3 按官方 output parser 作为 unknown action；streaming tokenizer 和 text grapheme action 仍继续推进。

本轮补充：terminal tokenizer 增加官方 streaming escape boundary 状态机，支持跨 chunk buffer/flush/reset、CSI/SS3/OSC/DCS/APC 序列边界、OSC BEL/ST terminator、ESC intermediate charset 序列、invalid CSI text fallback 和 opt-in X10 mouse payload 消费；完整 text grapheme action parser 仍继续推进。

本轮补充：terminal tokenizer 的 SS3 状态现在会消费参数字节后再等待 final byte，`ESC O 1;5D` 这类 modified SS3 cursor 序列可跨 chunk 作为完整 sequence token 进入 dispatcher。

本轮补充：terminal parser 增加轻量 ANSI action pipeline，串接 tokenizer、CSI/OSC/ESC dispatcher 和 SGR style state，输出 text/bell/cursor/erase/scroll/mode/title/link/tabStatus/reset/unknown action，文本宽度覆盖 ASCII、emoji 和 East Asian wide；完整 grapheme cluster segmentation 和 renderer style parity 仍继续推进。

本轮补充：terminal parser 跟踪 OSC 8 hyperlink start/end 状态，暴露当前 `inLink` 和 `linkUrl`，reset 时清空链接状态，贴近官方 parser 的 link range 状态语义。

本轮补充：terminal parser 的 text grapheme 基础分段补齐 combining mark、variation selector、emoji modifier、ZWJ emoji 序列和 regional indicator flag pair；宽度计算现在让 base+combining-mark cluster 保持 base glyph 宽度，emoji presentation/ZWJ/flag 仍按宽 grapheme 处理，完整 Unicode UAX #29 分段仍未宣称完成。

本轮补充：terminal parser 的 streaming text action 现在会暂存可能跨 chunk 延续的末尾 grapheme；ZWJ emoji、VS16 emoji ZWJ sequence、emoji modifier sequence、regional indicator flag 和未完成 emoji tag sequence 跨 `Feed()` 边界时不会被拆成两个宽字符，遇到控制序列或 `Flush()` 会先落地 pending text。

本轮补充：terminal parser 的 text grapheme 分段继续补齐 emoji tag sequence，把 subdivision flag 这类 black-flag base + tag chars + cancel tag 作为单个宽 grapheme，完整输入以及跨 `Feed()` 边界切在 base emoji 或 tag char 后时都不会拆分视觉 emoji。

本轮补充：terminal parser 的 text grapheme 分段继续补齐 emoji keycap sequence，`1️⃣`/`2⃣` 这类 keycap 在完整输入和跨 `Feed()` 边界切在 base 或 variation selector 后时都会保持单个宽 grapheme；完整 Unicode UAX #29 分段仍未宣称完成。

本轮补充：terminal parser 的 text grapheme 分段继续补齐 Hangul L/V/T jamo 连接规则，decomposed `한` 这类音节在完整输入以及跨 `Feed()` 边界切在 leading/vowel jamo 后时都会保持单个宽 grapheme；完整 Unicode UAX #29 分段仍未宣称完成。

本轮补充：terminal parser 的 text grapheme 分段继续补齐 CRLF line-break cluster，完整输入和跨 `Feed()` 边界切在 `\r`/`\n` 中间时都会保持单个零宽 line-break grapheme，wrap/trim/render 宽度路径也复用同一 line-break 判断；完整 Unicode UAX #29 分段仍未宣称完成。

本轮补充：terminal parser 的 text grapheme 分段继续补齐 Unicode mark category 和 Prepend 规则，nonspacing/enclosing/spacing mark 会归入前一 cluster，`का` 这类 spacing mark cluster 不再被拆宽，Arabic prepend mark 加 base text 会保持同一 grapheme，单独 prepend mark flush 时按零宽处理；完整 Unicode UAX #29 分段仍未宣称完成。

本轮补充：terminal parser 的 text grapheme 分段继续补齐常见 Indic virama conjunct，`क्ष` 这类 consonant + virama + consonant cluster 在完整输入和跨 `Feed()` 边界切在 virama 后时都会保持单个窄 grapheme；完整 Unicode UAX #29 分段仍未宣称完成。

本轮补充：terminal CSI parser 现在对 tokenizer flush 出来的非 final-byte incomplete CSI 返回 unknown action，而不是丢弃，贴近官方 `parseCSI` 对 flushed partial sequence 的 fallback 行为。

本轮补充：terminal sequence dispatcher 对 tokenizer flush 出来的 OSC partial sequence 使用 `ParseOSCContent` fallback，允许无 BEL/ST terminator 的 title/link/tab-status content 按官方 parser 语义产出 action。

本轮补充：terminal tokenizer 增加明确的 output/input 构造器，output 路径默认不吞 `CSI M` 后续字节，input 路径默认开启 X10 mouse payload 边界消费，避免调用方误用布尔选项导致 output parser 吞文本或 stdin mouse payload 泄漏。

本轮补充：mouse parser 现在接受 urxvt/xterm 1015 numeric mouse `CSI button;x;yM`，按 legacy offset 还原 button code，左键、释放和滚轮语义与 SGR/X10 mouse 保持一致。

本轮补充：terminal tokenizer 补齐 PM (`ESC ^`) 和 SOS (`ESC X`) string-control 状态，和 OSC/DCS/APC 一样支持 BEL 或 ST terminator，避免这些控制串 payload 泄漏为 text token。

本轮补充：terminal sequence dispatcher/parser 现在把 DCS/APC/PM/SOS string-control 序列分类为 `stringControl` action，保留 payload、terminator 和 incomplete flush 状态，同时 visible text 继续忽略这些不可见控制串。

本轮补充：terminal tokenizer、sequence dispatcher、CSI parser 和 visible-text stripping 现在接受 8-bit C1 CSI (`0x9b`) 序列，覆盖分块 SGR 输入以及 input tokenizer 的 X10 mouse payload 边界。

本轮补充：terminal key parser 现在接受 8-bit C1 CSI (`0x9b`) 输入形态，覆盖 bracketed paste、focus、direct/numbered/modified navigation、function-key、CSI-u/Kitty key、SGR/URXVT mouse 和 X10 mouse。

本轮补充：terminal tokenizer、SS3 parser 和 key parser 现在接受 8-bit C1 SS3 (`0x8f`) 序列，覆盖 application cursor、modified SS3 navigation 和 F1-F4 function-key 输入。

本轮补充：terminal tokenizer、OSC parser、string-control dispatcher 和 visible-text stripping 现在接受 8-bit C1 OSC/DCS/APC/PM/SOS 以及 C1 ST (`0x9c`) 终止符，同时保留合法 UTF-8 continuation byte，不会把 emoji/CJK 文本误切成控制串。

本轮补充：terminal tokenizer、sequence dispatcher、ESC parser 和 visible-text stripping 现在接受 C1 `IND`/`NEL`/`HTS`/`RI` (`0x84`/`0x85`/`0x88`/`0x8d`) 单字节控制，按 `ESC D`/`ESC E`/`ESC H`/`ESC M` 等价语义映射到 cursor/tab-set action，避免这些终端控制字节泄漏为可见文本。

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

当前状态：已完成项目 skill discovery、目录式 `SKILL.md` prompt metadata loading、project legacy `.claude/commands` prompt command loading、command registry metadata/lookup/filter、部分内置 slash command aliases/metadata、prompt expansion、基础 `Skill` tool inline 调用、本地项目 prompt skill 的基础 slash 调用接入、本地 prompt skill 的 command permissions attachment/current-turn 权限继承，以及 `/clear` 基础 local command no-query 路径；仍缺 bundled/plugin/MCP/remote skills、forked skill/agent 执行、完整 local/local-jsx 实际执行、`/help`/`/skills` UI、权限 UI/SDK 展示、plugin manifest/marketplace/cache/update、skill prompt shell injection 和完整 hooks/agents/MCP 接线。

### M9: MCP Platform

目标：完整 MCP client/server 平台。

需要完成：

- stdio/SSE/HTTP/WebSocket/sdk/claudeai-proxy transport。
- server config merge、policy allow/deny、OAuth。
- resources/prompts/tools/list/call/read。
- MCP tool result truncation/persist。
- elicitation、channel notifications、session-expired。
- 内置工具 MCP server。

本轮补充：SSE transport 现在会记录流事件 `id:`，stream 断开后把连接标记为需要重连；等待 async response 的同一请求遇到 EOF 会重新建立 SSE stream 并继续等待，重连时携带 `Last-Event-ID` 与已有 `mcp-session-id`，避免传统 SSE async response 在断流后只 POST 不重连。

本轮补充：`cmd/claude-mcp` 内置 stdio server 现在补齐 `resources/templates/list` 空列表响应、`completion/complete` 空 completion 响应和 `completions` capability 声明，以及 `logging/setLevel` level 校验和 no-op 成功路径，减少 MCP 客户端调用常见 utility/template 方法时落入 method-not-found。

本轮补充：MCP protocol client 和 server tool wrapper 现在支持 `resources/templates/list`，包括 `resourceTemplates`/`resource_templates`/`templates` 响应字段、`uriTemplate`/`uri_template` 和 `mimeType`/`mime_type` aliases、分页 cursor guard，以及 `mcp__server__list_resource_templates` 只读 helper tool。

本轮补充：MCP protocol client 现在补齐 `completion/complete` 和 `logging/setLevel` utility 调用入口，completion 结果支持 `hasMore`/`has_more`，logging level 会在发送前 trim，和内置 server 的 utility 方法形成客户端/服务端两侧覆盖。

本轮补充：MCP protocol client 现在支持 client roots surface，新增 `roots/list` inbound request handler、`Root`/`RootsProvider`/file root helper、roots capability 注入，以及 configured MCP toolsets 默认把当前工作目录作为 client root 传入真实 stdio/HTTP/SSE/WS protocol client。

本轮补充：MCP protocol client 现在新增通用 client notification 发送入口和 `notifications/roots/list_changed` helper；roots provider 可声明 `listChanged:true` capability，后续动态 workspace roots 变化能主动通知 server 刷新。

本轮补充：MCP protocol client 现在新增 `notifications/cancelled` helper，可按当前协议向 server 通知取消指定 request id，并统一 trim request id/reason 和空 id 校验。

本轮补充：MCP protocol client 现在新增 `notifications/progress` helper，支持 string/integer progressToken、progress、可选 total/message，并校验 token 类型与 finite progress 数值，补齐 client 侧 progress channel notification 发送入口。

本轮补充：MCP protocol client 现在新增 `ping` lifecycle utility 调用入口，复用现有 request/retry/reinitialize/authorization recovery 路径，和内置 MCP server 的 ping 支持形成客户端/服务端两侧覆盖。

本轮补充：MCP protocol client 现在新增 `resources/subscribe` 调用入口，按当前 resources 规范向 server 订阅资源更新，并在发送前 trim/校验 URI，后续可和 resources/updated notification surface 贯通。

本轮补充：`cmd/claude-mcp` 内置 stdio server 现在补齐 `resources/subscribe` 兼容处理；在当前没有内置资源的情况下会按 URI 参数返回 `resource not found`，避免客户端探测订阅能力时落入 method-not-found。

本轮补充：MCP tool wrapper 现在新增 `mcp__server__subscribe_resource` 只读 helper tool，把 `resources/subscribe` 从 protocol client 暴露到实际工具层；订阅成功会返回 `{uri, subscribed:true}` 结构化结果。

本轮补充：MCP resource/prompt helper tools 现在会在调用远端前 trim `uri`/`name` 并按 trim 后结果校验必填字段，避免纯空白 resource URI 或 prompt name 穿透到 MCP server。

本轮补充：MCP protocol client 的 `completion/complete` 和 `logging/setLevel` 现在会在本地 trim 并校验必填参数，拒绝空 ref type、空 ref name/uri、空 argument name 和空 logging level，避免发出无效 utility RPC。

本轮补充：MCP protocol client 的 `prompts/get` 现在会 trim prompt name、拒绝空名称，并把 nil arguments 统一为空对象，和 helper tool 的输入校验保持一致。

本轮补充：MCP protocol client 的 `tools/call` 现在会在本地 trim tool name 并拒绝空白名称，确保无效工具调用不会穿透到 MCP server。

本轮补充：MCP protocol client 的 `resources/read` 现在会 trim resource URI 并拒绝空值，和 `resources/subscribe` 及 helper tools 的 resource URI 校验保持一致。

本轮补充：MCP protocol client 现在会在 initialize 协商成功后把 server protocolVersion 回填到支持的 transport，HTTP/SSE 后续请求和 initialized notification 会携带 `mcp-protocol-version` header，WS 重连时也可复用该协商版本。

本轮补充：MCP WebSocket transport 现在会在等待响应帧时响应调用方 context 取消，通过 read deadline 打断阻塞读取并返回 `context.Canceled`/`DeadlineExceeded`，避免 server 不回包时调用悬挂。

本轮补充：MCP stdio transport 现在会保留可关闭 reader，并在等待响应行时响应调用方 context 取消；真实 stdio process stdout pipe 被取消时会关闭读端并返回 `context.Canceled`，避免服务端无响应导致调用悬挂。

本轮补充：MCP HTTP event-stream response 现在会用显式 byte-limit reader 执行 `MaxResponseBytes`，超限时返回和普通 JSON response 一致的 `mcp http response exceeds ... bytes` 错误，避免大流响应退化成 decode/not-found。

本轮补充：MCP SSE endpoint discovery 现在复用 HTTP event-stream 的显式 byte-limit reader，初始 SSE stream 超过 `MaxResponseBytes` 时会返回明确超限错误，而不是落到 endpoint not found。

本轮补充：conversation runner 现在新增 `LoadMCPConfigFromSettingsFiles` 接线入口，可从 `CLAUDE_CONFIG_DIR/settings.json`、项目 `.claude/settings.json` 和 `.claude/settings.local.json` 读取三层 settings，忽略缺失文件并生成 runner 可直接消费的 `MCPConfig`。

本轮补充：auth 层现在新增文件型 `CredentialStore` 基础设施，默认写入 Claude config 目录的 `credentials.json`，保存时创建 0700 目录、0600 临时文件并原子 rename，支持 load/save/delete 和 context 取消，为 OAuth refresh credentials 的持久化接线打基础。

本轮补充：`OAuthTokenProvider` 现在支持可选 `CredentialStore`，refresh 成功后会把更新后的 access/refresh token、scope 和 expiresAt 写回 store；保存失败会作为 refresh 错误返回，避免静默丢失新凭据。

本轮补充：MCP 层现在新增 file-backed `ServerAccessTokenProvider`，可按 MCP server 名称从 Claude config 目录下的独立 credential file 加载 OAuth credentials，创建可 refresh 的 token provider，并在 refresh 成功后写回同一 store；自定义 provider 注入路径保持不变。

本轮补充：`LoadMCPConfigFromSettingsFiles` 现在会默认把 file-backed MCP OAuth provider 注入 runner `ToolOptions`，因此后续 CLI/TUI 通过该 loader 创建 runner 时，OAuth MCP server 可直接从默认 credential file 读取/refresh token。

本轮补充：MCP elicitation surface 现在新增结构化 `ElicitationRequestHandler` adapter，可把 inbound `elicitation/create` request 解析成 `ElicitationRequest` 交给 handler，并把 handler 返回的 action/status/type 与 content/values/value 规范化为协议响应，同时保留非 elicitation fallback。

本轮补充：MCP notification surface 现在新增 `NotificationEventRPCHandler` adapter 和 `ProtocolClient.SetNotificationEventHandler`，调用方可以直接消费按 server name 归一化后的 `NotificationEvent`，同时 raw notification capture 保持不变。

本轮补充：CLI bootstrap 现在新增 `State.ConversationRunner`，用当前 CWD 构造带 `SessionID`、`WorkingDirectory` 和 settings-derived `MCPConfig` 的 conversation runner skeleton；`cmd/claude` 启动时会先走该路径加载/校验 MCP settings，为后续完整 CLI/TUI runner 主循环接线打基础。

当前状态：已新增 `internal/mcp` 配置地基，覆盖 transport 归一化、stdio/url server signature、CCR proxy URL 解包、plugin MCP server 去重、allowed/denied MCP policy 的基础判定和过滤、MCP server config 环境变量展开、server scope/merge 基础、`.mcp.json` schema 解析/校验基础、项目目录链 `.mcp.json` 加载/合并、settings `mcpServers` scope 解析和 user/project/local 手工配置合并过滤、settings/.mcp.json/policy 到多 server toolset 的高层装配入口基础、conversation runner 对 configured MCP toolsets 的自动合并/执行基础、settings 文件到 runner `MCPConfig` 的 loader 接线入口及默认 MCP OAuth provider 注入、CLI bootstrap 到 runner MCP config skeleton 接线基础、MCP tool result 归一化/错误标记/result meta/content aliases/content-item aliases/大输出落盘基础、`mcp__server__tool` 名称归一化/解析 helper、MCP remote tool discovery/call 到 Go tool registry 的基础适配、MCP tool input/output schema 解析和传播、MCP tools/resources/prompts list pagination 和 cursor alias、MCP resource read content aliases 和 subscribe 调用/工具入口、MCP prompt get message aliases、MCP resource list/read/subscribe helper tool 基础、MCP prompt list/get helper tool 基础、resource/prompt helper 输入 trim 校验、MCP utility/prompt/resource client 输入 trim/校验、MCP JSON-RPC protocol client initialize/initialized lifecycle、ping 和 session-expired 判定/reset/reinitialize/retry 基础、client roots/list capability、roots/list_changed notification、cancelled/progress notification 和 CWD root 注入基础、stdio newline JSON-RPC transport/process launch 和 context cancellation 基础、HTTP JSON-RPC transport 基础、HTTP event-stream response parsing、byte limit 和 inbound request response POST 基础、`mcp-session-id` header 复用和 DELETE close 基础、传统 SSE endpoint discovery byte limit + POST + async response stream 和 stream inbound request response POST 基础、WebSocket JSON-RPC transport 和 context cancellation 基础、stdio/HTTP/SSE/WS JSON-RPC notification 捕获/handler 和 notification event 归一化/结构化 handler adapter surface 基础、stdio/WS inbound server request handler 与 elicitation/create 默认 cancel/自定义 handler、elicitation request/response alias surface 和结构化 handler adapter 基础、static authToken/OAuth beta transport header 基础、HTTP/SSE/WS dynamic auth header provider 基础、通用 OAuth refresh-token provider 基础、OAuth credential file store 和 refresh 持久化基础、MCP OAuth server file-backed access-token provider 注入基础、MCP HTTP/SSE/WS 401 reactive authorization refresh 基础、stdio/HTTP/SSE/WS protocol client 到 server toolset 的装配基础、多 server toolset 聚合和统一 close 基础、MCP tool annotations read-only/destructive hint 解析，以及 `cmd/claude-mcp` stdio 内置工具 server 初版（initialize、initialized lifecycle guard、ping、tools/list annotations 和 outputSchema、tools/call、resources/prompts 空列表与 not-found 响应、resources/subscribe 兼容 not-found 响应、JSON-RPC id 保留、batch request、invalid request 错误、client cancellation notification 基础、read-only 默认权限、`--allow-mutating-tools`）。完整 CLI/TUI 交互主循环与 runner 执行接线、完整 SSE/WS lifecycle hardening、HTTP streaming lifecycle hardening、stdio lifecycle hardening、完整 OAuth 授权/secure storage、完整 channel notification/elicitation surface 和完整内置 MCP server parity 仍未完成。

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

1. 补强 M5 基础工具兼容：优先 `Bash` shell parser/sandbox/background 和 `PowerShell` parser/permission/golden，继续补 `Glob/Grep`、`TodoWrite`、`WebFetch`、`WebSearch` 的官方 golden 兼容。
2. 补强 `Read/Edit/Write` 高级分支：PDF/image/notebook/diff/LSP/history。
3. 扩展 CLI `--print` 和 SDK JSON/NDJSON，用 golden 对齐官方行为。
4. 推进 session resume、compact、memory。
5. 再进入 TUI、MCP、plugins、skills、agents、remote。

## Verification Strategy

每个模块都需要至少三层验证：

- Unit tests：验证 Go 内部逻辑。
- Golden/parity tests：验证 JSON、stdout/stderr、transcript、tool result 等稳定输出。
- Official CLI black-box tests：无法从源码确定的行为，必须用官方 CLI 采样反推。

`go test ./...` 只能说明当前实现内部一致，不能证明 Claude Code 100% parity。
