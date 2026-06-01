# Claude Code Source Scan

扫描时间：2026-06-01  
源路径：`/Users/sqlrush/agent/claude-code`  
目标工作区：`/Users/sqlrush/ccgo`

## 1. 快照状态

当前源仓库是一个源码镜像/快照，而不是完整可直接构建的 npm 工程。根目录只有：

- `.git`
- `README.md`
- `assets/`
- `src/`

未发现 `package.json`、lockfile、构建脚本、测试目录或依赖声明文件。README 明确说明该仓库来源于 npm sourcemap 还原，因此 Go 重写计划必须把“源码行为分析”和“缺失契约补齐”分开处理。

## 2. 规模统计

| 项目 | 结果 |
| --- | ---: |
| `src` 总大小 | 33MB |
| `assets` 总大小 | 656KB |
| 源文件数 | 1,902 |
| 估算源码行数 | 512,685 |
| `.ts` 文件 | 1,332 |
| `.tsx` 文件 | 552 |
| `.js` 文件 | 18 |
| 带内联 `sourceMappingURL=data` 的文件 | 552 |

按一级目录统计：

| 目录 | 文件数 | 说明 |
| --- | ---: | --- |
| `src/utils` | 564 | 平台、文件、消息、权限、插件、模型、hooks、shell、会话等核心基础层 |
| `src/components` | 389 | TUI 组件、权限弹窗、消息渲染、设置、MCP、任务面板 |
| `src/commands` | 207 | slash/local 命令注册与实现 |
| `src/tools` | 184 | Agent 可调用工具集合 |
| `src/services` | 130 | API、MCP、compact、analytics、LSP、OAuth、sync 等服务 |
| `src/hooks` | 104 | React/Ink hooks 与交互逻辑 |
| `src/ink` | 96 | 自研 terminal React renderer |
| `src/bridge` | 31 | remote-control / repl bridge |
| `src/constants` | 21 | prompt、beta、工具、系统常量 |
| `src/skills` | 20 | bundled skills 和技能加载 |
| `src/cli` | 19 | headless/SDK/structured IO |
| `src/tasks` | 12 | local shell/agent/remote/dream task |
| `src/types` | 11 | 命令、权限、日志、插件等类型 |
| 其他 | 154 | state、entrypoints、memdir、remote、server、buddy 等 |

最大文件集中在运行时核心和 TUI：

| 文件 | 行数 | 用途 |
| --- | ---: | --- |
| `src/cli/print.ts` | 5,594 | headless/SDK print 模式、structured IO、control 协议 |
| `src/utils/messages.ts` | 5,512 | 消息构造、归一化、渲染/存储边界 |
| `src/utils/sessionStorage.ts` | 5,105 | JSONL transcript、resume、sidechain/subagent 存储 |
| `src/utils/hooks.ts` | 5,022 | hook 生命周期执行器 |
| `src/screens/REPL.tsx` | 5,005 | 主交互 TUI |
| `src/main.tsx` | 4,683 | 主 CLI、Commander 参数、启动编排 |
| `src/utils/bash/bashParser.ts` | 4,436 | shell 命令解析 |
| `src/services/api/claude.ts` | 3,419 | Anthropic API 调用、流式响应、beta/header/tool schema |
| `src/services/mcp/client.ts` | 3,348 | MCP client、tool/resource/prompt 映射 |
| `src/utils/plugins/pluginLoader.ts` | 3,302 | plugin 加载和缓存 |

## 3. 入口和运行模式

### CLI bootstrap

- `src/entrypoints/cli.tsx` 是轻量 bootstrap，优先处理 `--version`、特殊 MCP/native host、daemon/background/bridge 等 fast path，然后动态导入主程序。
- `src/main.tsx` 是完整 CLI 入口，负责启动 profiling、MDM/keychain 预取、配置加载、Commander 参数、auth、policy、MCP、plugins、skills、settings、model、permission context、REPL/print 模式调度。

### 交互模式

- `src/screens/REPL.tsx` 是主 UI，围绕 React hooks、自研 Ink renderer、message list、prompt input、permission overlay、task dialogs、MCP/bridge/voice/buddy/ultraplan 等 feature gate 组织。
- `src/ink/` 不是普通 Ink 依赖，而是项目内的 terminal renderer：layout、reconciler、termio parser、events、components、hooks 都在仓库内。

### Headless/SDK 模式

- `src/cli/print.ts` 处理 `-p`/非交互场景、structured JSON/NDJSON、SDK control initialize/request/response、MCP 动态设置、resume、permissions、hooks、session state。
- `src/QueryEngine.ts` 抽出 query lifecycle，复用 `src/query.ts` 的 agentic loop，偏 SDK/headless。

### MCP server 模式

- `src/entrypoints/mcp.ts` 把内置工具暴露为 MCP stdio server，当前只暴露本地内置工具，不复暴露外部 MCP 工具。

### Remote/bridge 模式

- `src/bridge/*` 实现 local machine 作为 claude.ai remote-control environment 的桥接。
- `src/remote/*` 处理 remote session manager 和 WebSocket 订阅。

## 4. 核心执行链

主链路可以概括为：

1. `entrypoints/cli.tsx` fast path 或进入 `main.tsx`
2. `main.tsx` 构建 settings/auth/model/permission/MCP/tools/commands/app state
3. 交互模式进入 `REPL.tsx`，headless 进入 `cli/print.ts`
4. 用户输入经 `utils/processUserInput/*` 转成消息、slash command、bash shortcut 或 queued command
5. `QueryEngine.submitMessage()` 或 REPL 直接调用 `query()`
6. `query.ts` 构建 API request，处理 streaming、tool use、compact、stop hooks、retry/fallback、token budget
7. `services/api/claude.ts` 负责 Anthropic SDK 请求、beta headers、tool schema、cost/usage/cache/telemetry
8. tool use 进入 `services/tools/toolExecution.ts` 和 `toolOrchestration.ts` / `StreamingToolExecutor.ts`
9. 工具执行结果转成 user `tool_result` 消息，继续循环直到 stop
10. `sessionStorage.ts` 记录 JSONL transcript，TUI/SDK 输出事件

## 5. 工具系统

核心类型在 `src/Tool.ts`。每个工具实现以下行为面：

- `name`/`aliases`/`searchHint`
- Zod `inputSchema`/可选 `outputSchema`
- `prompt()` 和 `description()`
- `validateInput()`
- `checkPermissions()`
- `call()`
- 并发安全、只读/破坏性、可中断行为、UI 渲染、结果映射、auto-classifier 输入、MCP 元数据等扩展点

`src/tools.ts` 是内置工具池的事实入口。基础工具包含：

- Agent/Task：`AgentTool`、`TaskOutputTool`、`TaskStopTool`、Todo/Task v2 工具
- Shell：`BashTool`、可选 `PowerShellTool`、REPL/Tungsten/TerminalCapture 等 gated 工具
- 文件：Read/Edit/Write/NotebookEdit/Glob/Grep
- Web：WebFetch/WebSearch/可选 WebBrowser
- MCP：MCPTool、McpAuth、List/Read resources
- Planning/worktree：Enter/ExitPlanMode、Enter/ExitWorktree
- Skill/Search：SkillTool、ToolSearchTool
- Team/swarm/remote/proactive：SendMessage、TeamCreate/Delete、ScheduleCron、RemoteTrigger、Sleep、Brief 等 gated 工具

工具执行层有两个并发模型：

- `toolOrchestration.ts`：把连续 concurrency-safe 工具成批并发，非安全工具串行。
- `StreamingToolExecutor.ts`：流式收到 tool_use 时立即调度，保证非安全工具独占，并按原始顺序输出结果。

## 6. 命令和技能系统

`src/commands.ts` 统一注册内置 slash/local 命令，并合并：

- 内置 commands
- bundled skills
- `~/.claude` / 项目 skills
- plugin commands/skills
- MCP skills
- workflow commands
- dynamic skills

命令类型在 `src/types/command.ts`：

- `prompt`：扩展 prompt 或作为 SkillTool 供模型调用
- `local`：返回文本或 compact 结果
- `local-jsx`：渲染交互 TUI

命令还带 `availability`、`isEnabled`、`isHidden`、`immediate`、`isSensitive`、`loadedFrom`、`disableModelInvocation` 等控制位。

## 7. 权限、安全和沙箱

权限类型在 `src/types/permissions.ts`，实现分布在 `src/utils/permissions/*` 和各工具内。

关键机制：

- 权限模式：`default`、`acceptEdits`、`bypassPermissions`、`dontAsk`、`plan`，以及 gated 的 `auto`
- 规则来源：user/project/local/flag/policy/cli/session/command
- 行为：allow/deny/ask
- 路径权限、shell 规则、MCP server/tool 规则、agent deny 规则
- Hook、classifier、permission prompt tool 可以参与最终决策
- Bash/PowerShell 有专门 parser、dangerous pattern、read-only validation、destructive warning 和 sandbox override
- File write/edit 要求先读、mtime 检查、UNC 防泄漏、team memory secret guard、LSP diagnostic 清理、IDE 更新通知
- BashTool 支持 sandbox、background、timeout、sed edit 预览、输出落盘、图像输出处理

## 8. MCP 和外部集成

MCP 主要模块：

- `src/services/mcp/config.ts`：从 `.mcp.json`、settings、managed、policy、Claude.ai、plugins 合并配置，支持 stdio/SSE/HTTP/WebSocket/sdk/claudeai-proxy。
- `src/services/mcp/client.ts`：连接 MCP server，获取 tools/resources/prompts，执行 tool，处理 OAuth、session 过期、tool result 截断和持久化。
- `src/tools/MCPTool/*`、`ListMcpResourcesTool`、`ReadMcpResourceTool`、`McpAuthTool`：模型可见工具层。
- `src/components/mcp/*`：TUI 管理界面。

其他集成：

- OAuth / keychain / secure storage
- LSP diagnostic server manager
- GitHub commands、review、install GitHub app
- IDE bridge / VS Code SDK MCP
- Chrome extension / native host
- voice stream STT
- computer-use MCP gated 模块
- remote session / CCR / teleport

## 9. 会话、消息和持久化

消息是全系统核心契约，但当前快照缺少 `src/types/message.ts`。从引用和使用可见主要类别包括：

- user
- assistant
- system
- attachment
- progress
- compact boundary
- tombstone
- tool use summary
- SDK stream events

`src/utils/messages.ts` 提供消息构造、API normalization、tool result pairing、thinking blocks、附件、compact boundary、system reminders、renderable/normalized 转换。

`src/utils/sessionStorage.ts` 负责：

- `~/.claude/projects/<project>/<session>.jsonl`
- parent UUID chain
- resume/search/session title
- subagent sidechain transcript
- file history snapshot
- attribution snapshot
- content replacement records
- worktree session metadata
- 大文件 transcript 的 head/tail 读取和 tombstone 防 OOM

## 10. 配置、settings 和策略

settings 主实现：

- `src/utils/settings/types.ts`
- `src/utils/settings/settings.ts`
- `src/utils/settings/validation.ts`
- `src/utils/settings/constants.ts`
- `src/services/remoteManagedSettings/*`
- `src/services/policyLimits/*`

配置来源包括：

- user settings
- project settings
- local settings
- flag settings
- policy/managed settings
- MDM / HKCU
- remote managed settings
- plugin defaults/options
- environment variables

settings schema 明确要求新增字段保持后向兼容：只能增加可选字段、保留旧 enum、保留未知字段，不能删除或强收窄。

## 11. Feature gates 和条件编译

源码广泛使用 `feature('...')` 与 `process.env` 控制功能面和 dead-code elimination。扫描中高频 gate 包括：

- `KAIROS`、`PROACTIVE`
- `COORDINATOR_MODE`
- `BRIDGE_MODE`
- `BG_SESSIONS`
- `TRANSCRIPT_CLASSIFIER`
- `BASH_CLASSIFIER`
- `TOKEN_BUDGET`
- `CONTEXT_COLLAPSE`
- `REACTIVE_COMPACT`
- `CACHED_MICROCOMPACT`
- `HISTORY_SNIP`
- `TEAMMEM`
- `VOICE_MODE`
- `CHICAGO_MCP`
- `WEB_BROWSER_TOOL`
- `WORKFLOW_SCRIPTS`
- `MCP_SKILLS`
- `ULTRAPLAN`
- `BUDDY`

Go 重写不能把这些 gate 当普通 if 处理完事；需要一个 build/runtime feature matrix，并能生成不同发行目标的功能集合。

## 12. 快照缺口

用本地 import 解析扫描 1,902 个源码文件，发现 188 个缺失 import 目标。高影响缺口如下：

| 缺失目标 | 被引用次数 | 影响 |
| --- | ---: | --- |
| `src/types/message` | 184 | 消息核心类型缺失，必须重建 |
| `src/constants/querySource` | 21 | query/telemetry/source 分类缺失 |
| `src/services/contextCollapse/index` | 20 | context collapse 主实现缺失 |
| `src/types/tools` | 19 | tool progress 类型缺失 |
| `src/entrypoints/sdk/controlTypes` | 19 | SDK control 协议类型缺失 |
| `src/keybindings/types` | 17 | keybinding 类型缺失 |
| `src/types/utils` | 15 | DeepImmutable 等基础类型缺失 |
| `src/proactive/index` | 15 | proactive/KAIROS 模块缺口 |
| `src/services/compact/snipCompact` | 14 | history snip 缺口 |
| `src/components/agents/new-agent-creation/types` | 13 | agent wizard 类型缺口 |
| `src/components/mcp/types` | 10 | MCP UI 类型缺口 |
| `src/services/compact/cachedMicrocompact` | 10 | cached microcompact 缺口 |

这些缺口不阻止制定重写计划，但阻止“仅凭当前快照证明 100% 行为还原”。计划中必须加入：

- 缺失文件恢复或等价契约重建
- 官方 CLI 黑盒行为采样
- transcript/API/tool JSON golden corpus
- TUI screenshot/ANSI golden corpus

## 13. 结论

Claude Code 不是单一 CLI，而是一个复合系统：

- terminal app
- agentic LLM runtime
- tool execution engine
- permission/sandbox/security engine
- MCP client/server host
- plugin/skill platform
- session database
- remote-control bridge
- multi-agent/task orchestrator
- enterprise settings/policy client
- telemetry/diagnostics runtime

Go 重写要以“契约优先、行为 golden、模块替换”的方式推进。逐文件翻译会被 React/Ink、Zod schema、feature-gated dead code、缺失 types 和平台细节拖垮。
