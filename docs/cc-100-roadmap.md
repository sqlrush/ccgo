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
| 全量测试 | 当前 `go test ./...` 通过 |

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

当前状态：session/history 有大量基础能力；compact/memory/subagent layout 仍缺。

### M7: TUI Renderer And Interaction

目标：还原交互式 Claude Code 体验。

需要完成：

- Terminal renderer、layout、event、input、scroll、selection、alternate screen。
- REPL screen、PromptInput、Messages、StatusLine。
- permission dialogs、task dialogs。
- keybindings、vim mode、history/search。
- ANSI snapshots 和交互脚本。

当前状态：未开始真实 TUI。

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
