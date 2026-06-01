# Claude Code To Go Module Map

目标：把 TypeScript 快照按行为边界拆成 Go 模块，避免按目录机械翻译。

## 1. 分层架构

建议 Go 包层级：

```text
cmd/claude
internal/bootstrap
internal/config
internal/auth
internal/model
internal/api/anthropic
internal/messages
internal/conversation
internal/tool
internal/tools/...
internal/permissions
internal/sandbox
internal/tui
internal/commands
internal/skills
internal/plugins
internal/mcp
internal/session
internal/hooks
internal/tasks
internal/agent
internal/memory
internal/compact
internal/bridge
internal/remote
internal/lsp
internal/platform
internal/telemetry
pkg/sdk
pkg/mcpserver
test/parity
```

依赖方向：

```text
platform/config/auth/model
  -> messages/session
  -> permissions/tool
  -> tools/mcp/commands/skills/plugins
  -> conversation
  -> cli/tui/sdk/bridge/remote
```

`internal/tui`、`internal/api/anthropic`、`internal/mcp`、`internal/platform` 应通过接口依赖 `conversation` 和 `tool`，避免形成 TypeScript 当前的大循环。

## 2. 源目录到 Go 包映射

| TS 源 | Go 模块 | 重写职责 |
| --- | --- | --- |
| `src/entrypoints/cli.tsx` | `cmd/claude`, `internal/bootstrap` | fast path、特殊入口、启动路由 |
| `src/main.tsx` | `cmd/claude`, `internal/bootstrap`, `internal/cli` | 参数解析、启动编排、模式选择 |
| `src/cli/print.ts` | `internal/cli/headless`, `pkg/sdk` | print/SDK/structured IO/control 协议 |
| `src/entrypoints/mcp.ts` | `pkg/mcpserver`, `cmd/claude-mcp` | 内置工具 MCP server |
| `src/QueryEngine.ts`, `src/query.ts`, `src/query/*` | `internal/conversation` | agentic loop、streaming、tool loop、compact、stop hooks |
| `src/services/api/*` | `internal/api/anthropic`, `internal/api/usage` | Anthropic API、retry、headers、usage/cost、files/session ingress |
| `src/Tool.ts`, `src/tools.ts` | `internal/tool` | 工具接口、注册、filter/merge/defer |
| `src/tools/BashTool/*` | `internal/tools/bash` | bash 执行、解析、权限、sandbox、background、输出 |
| `src/tools/PowerShellTool/*` | `internal/tools/powershell` | PowerShell parser、权限、执行 |
| `src/tools/FileReadTool/*` | `internal/tools/file/read` | 文本/图片/PDF/notebook 读取、token 限制 |
| `src/tools/FileEditTool/*` | `internal/tools/file/edit` | 精确替换、patch、mtime、diff、IDE/LSP 通知 |
| `src/tools/FileWriteTool/*` | `internal/tools/file/write` | create/update、diff、read-before-write 规则 |
| `src/tools/GlobTool`, `GrepTool` | `internal/tools/search` | glob/grep/ripgrep 行为 |
| `src/tools/WebFetchTool`, `WebSearchTool` | `internal/tools/web` | fetch/search、preapproval、渲染 |
| `src/tools/MCPTool`, `ListMcpResourcesTool`, `ReadMcpResourceTool`, `McpAuthTool` | `internal/tools/mcp` | MCP tool/resource/auth 包装 |
| `src/tools/AgentTool/*` | `internal/tools/agent`, `internal/agent` | subagent、async agent、fork/worktree/remote、agent definitions |
| `src/tools/Task*`, `src/tasks/*` | `internal/tasks` | local shell/agent/dream/remote task lifecycle |
| `src/tools/TodoWriteTool/*` | `internal/tools/todo` | Todo list state/tool |
| `src/tools/SkillTool`, `ToolSearchTool` | `internal/tools/skill`, `internal/tools/searchtools` | skill invocation、deferred tool discovery |
| `src/commands.ts`, `src/commands/*` | `internal/commands` | slash/local/local-jsx command registry 和实现 |
| `src/skills/*` | `internal/skills` | bundled/user/plugin/MCP skills |
| `src/plugins/*`, `src/utils/plugins/*`, `src/services/plugins/*` | `internal/plugins` | plugin manifest、marketplace、cache、install、MCP/commands/hooks/agents 加载 |
| `src/services/mcp/*` | `internal/mcp` | MCP config、client、transport、OAuth、channel、normalization |
| `src/utils/permissions/*`, `src/types/permissions.ts`, permission components | `internal/permissions` | rules、modes、decision、classifier、UI request model |
| `src/utils/sandbox/*`, `src/commands/sandbox-toggle/*` | `internal/sandbox` | sandbox adapter、配置、doctor |
| `src/ink/*`, `src/components/*`, `src/hooks/*`, `src/screens/*` | `internal/tui` | terminal renderer、组件、输入、消息视图、dialogs |
| `src/state/*`, `src/context/*` | `internal/state` | app state store、notifications、modal、stats、mailbox |
| `src/types/command.ts`, `types/logs.ts`, missing `types/message` | `internal/contracts` | Go 结构体和 JSON schema 契约 |
| `src/utils/messages.ts`, `utils/messages/*` | `internal/messages` | message constructors、normalization、API 映射 |
| `src/utils/sessionStorage.ts`, `history.ts`, `assistant/sessionHistory.ts` | `internal/session` | transcript、resume、search、sidechain、titles |
| `src/utils/settings/*`, migrations | `internal/config/settings` | settings schema、merge、migration、managed policy |
| `src/utils/config.ts`, env/bootstrap state | `internal/config` | global/project config、env、paths |
| `src/services/analytics/*`, `diagnosticTracking`, `internalLogging` | `internal/telemetry` | event queue、sinks、diagnostics、safe metadata |
| `src/utils/hooks.ts`, `src/schemas/hooks.ts`, `src/utils/hooks/*` | `internal/hooks` | hook registry、shell/http/prompt/agent hooks |
| `src/memdir/*`, `src/services/SessionMemory`, `extractMemories`, `teamMemorySync` | `internal/memory` | CLAUDE.md、auto/team memory、secret scanning |
| `src/services/compact/*` | `internal/compact` | compact、auto compact、microcompact、token warning |
| `src/bridge/*`, `src/remote/*`, `src/server/*` | `internal/bridge`, `internal/remote` | repl bridge、remote-control、direct connect、session websocket |
| `src/services/lsp/*`, `src/tools/LSPTool/*` | `internal/lsp` | LSP server manager、diagnostics、LSP tool |
| `src/keybindings/*`, `src/vim/*` | `internal/input` | keybindings、vim mode、history/search input |
| `src/native-ts/*` | `internal/native` | yoga/file-index/color-diff 的 Go 替代 |
| `src/utils/bash/*`, `utils/shell/*`, `utils/powershell/*`, `utils/Shell*` | `internal/platform/shell` | shell parser/executor、quoting、providers |
| `src/utils/secureStorage/*` | `internal/platform/securestore` | macOS keychain / fallback storage |
| `src/utils/model/*` | `internal/model` | model aliases、capabilities、providers、cost |
| `src/voice/*`, `src/services/voice*` | `internal/voice` | voice mode/STT |
| `src/utils/claudeInChrome/*`, chrome command | `internal/chrome` | Chrome native host/MCP |
| `src/utils/computerUse/*` | `internal/computeruse` | computer-use MCP wrapper |
| `src/buddy/*` | `internal/buddy` | companion feature |

## 3. Go 模块职责边界

### `internal/contracts`

第一优先级。定义稳定 JSON/stream 契约：

- Message union
- SDK message/event/control types
- Tool schema/result/progress
- Command type
- Permission result/update
- Settings/MCP config schema
- Session JSONL entries

当前 TS 快照缺少 `types/message`、`types/tools`、`entrypoints/sdk/controlTypes` 等，Go 侧应先基于使用点和官方 CLI 输出重建。

### `internal/tool`

Go 版工具接口建议：

```go
type Tool interface {
    Name() string
    Aliases() []string
    InputSchema(ctx PromptContext) JSONSchema
    Prompt(ctx PromptContext) (string, error)
    Validate(ctx ToolContext, input json.RawMessage) error
    CheckPermissions(ctx ToolContext, input json.RawMessage) (permissions.Decision, error)
    Call(ctx ToolContext, input json.RawMessage, progress ProgressSink) (ToolResult, error)
    IsReadOnly(input json.RawMessage) bool
    IsConcurrencySafe(input json.RawMessage) bool
    IsDestructive(input json.RawMessage) bool
}
```

不要把 TUI 渲染方法放入核心接口。渲染应通过 `ToolRenderer` 适配层完成，避免工具包依赖 terminal UI。

### `internal/conversation`

负责 query loop，不依赖具体 TUI：

- request config 构建
- streaming parser
- tool call queue
- tool result pairing
- retry/fallback
- compact/auto compact
- stop hooks
- token budget
- transcript append

### `internal/tui`

TypeScript 中 `src/ink` 是自研 renderer。Go 侧有两种选择：

1. 使用 Bubble Tea/Lip Gloss 快速实现基本 UI。
2. 自建轻量 renderer 以复刻 layout、focus、selection、scroll、alternate screen 和 ANSI 行为。

若目标是 100% 还原，最终应采用第 2 种，Bubble Tea 只能作为早期原型。

### `internal/api/anthropic`

必须严格保持：

- beta header 组合
- tool schema serialization
- prompt cache breakpoint/TTL 行为
- thinking/adaptive/effort/task budget
- structured output
- model fallback/retry
- rate-limit/quota/error 映射
- usage/cost 计算

### `internal/permissions`

必须独立于具体工具实现：

- 规则解析与匹配
- ask/allow/deny 合并优先级
- settings/policy/cli/session 来源
- hook/classifier/permission prompt tool 决策
- auto/dontAsk/bypass/plan 模式
- file/shell/MCP/agent 规则适配器

### `internal/session`

Go 重写的 transcript 必须能读写现有 JSONL：

- parentUuid chain
- resume
- subagent transcript
- file history snapshot
- attribution snapshot
- content replacement
- compact boundary
- tombstone
- legacy progress bridge

## 4. 优先级切分

P0 基础契约：

- contracts/messages
- contracts/sdk
- settings schema
- tool interface
- permission result
- session entry

P1 可运行核心：

- CLI bootstrap
- config/auth/model
- Anthropic API streaming
- query loop
- Bash/Read/Edit/Write/Glob/Grep
- transcript

P2 交互体验：

- TUI renderer
- prompt input
- permission dialogs
- messages rendering
- slash commands
- history/resume

P3 扩展平台：

- MCP
- skills/plugins
- hooks
- LSP
- agents/tasks

P4 高级/内部/gated：

- bridge/remote
- KAIROS/proactive
- coordinator/swarm/team
- context collapse/snip/cached microcompact
- voice/chrome/computer-use/buddy/ultraplan

## 5. 模块验收方式

每个 Go 模块必须有三类验收：

- Contract parity：JSON schema、CLI flags、settings、session entry、SDK event 与 TS/官方 CLI 一致。
- Behavioral parity：同输入、同环境、同 fixture 下输出一致。
- Failure parity：错误消息、权限拒绝、retry、timeout、cancel、interrupt 行为一致。

没有 golden corpus 的模块不得声明 100% 完成。
