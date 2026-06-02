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

- 已落地 JSONL/session 基础、resume/search/title 支撑、prompt history lock/buffered flush/field aliases、official subagent transcript layout、agent metadata sidecar/field aliases、sidechain runtime start/append/finish/cancel/fail 和 parent-chain append/finish 初版、sidechain manager orchestration 初版、sidechain manifest 聚合、sidechain state/list/resume/content-field aliases 初版、sidechain resume context builder、sidechain conversation/content-replacement reconstruction、transcript tail/window/metadata/index loaders、byte-budget transcript tail loader、agent-scoped content replacement metadata/record field-alias loading、session-scoped metadata reappend including AI-title/last-prompt/task-summary、transcript line offset index/window/byte-budget-window/parent-chain/resume/tail/byte-budget-tail loaders、extended transcript metadata entries/type/field aliases、transcript message/session UUID field aliases、transcript tombstone metadata delete/relink、transcript resume conversation builder、index 文本预览和 AI-title/last-prompt/task metadata 字段、流式 transcript 搜索、session list pagination、remote history token refresh、remote history 全量分页抓取/page-field/event-list/records/entries/last-id/cursor/event-id/has-next alias/wrapped-data/links/paging/bare-array/connection wrapper response/edge-cursor fallback/max-pages 截断状态/before_id 续抓、remote event transcript materialization/fallback field fill/去重追加/duplicate parent guard、remote history 一步 sync 到 transcript、CLAUDE.md/memdir 扫描、team-memory secret guard、compact runner/boundary plan、conversation auto-compact、失败熔断、microcompact/cache 初版、persistent cached microcompact 初版、cache digest structural/rich-content metadata 覆盖、cache version/TTL/prune、in-memory micro cache prune、memory-cache write-through 到磁盘、atomic cache write、坏缓存默认 fail-open、session memory summary/frontmatter aliases/recall 初版、model-ranked recall session-id selection 和 invalid-selection fallback 初版、recall agent alternate/camel response keys/fenced-prose JSON extraction/scalar id parsing/nested/wrapped/collection-alias selection parsing、resume context model-assisted recall 接入、session memory rollup/prune compaction、rollup archive exclusion/merge、rune-safe rollup truncation、resume context + session memory recall、conversation recall 注入、deterministic/model-backed memory extraction 初版、extraction agent fenced-prose JSON/wrapped facts/alternate/structured field/nested source object/nested response/fact kind alias parsing 和 turn-end memory extraction 落盘。

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
- 本轮补充：keybinding config、keymap 解析和 interaction script named-key 输入接受 `ctrl-h`/`ctrl-i`/`ctrl-j`/`ctrl-m`、`ctrl-[`、`ctrl-?`、对应 `control-*` 以及 compact/camel 形式；terminal parser 支持 CSI-u/kitty keyboard protocol 的 ctrl/alt/shift-enter/shift-tab 序列；image hint parser 支持 OSC ST terminator 和 base64 `name=` filename；keybinding JSON loader 支持 wrapper object-map、`shortcuts`、object action 字段、string-array key sequence/chord 和 `null`/`false` unbind；mouse parser 支持 legacy X10/normal tracking 序列；interaction script 支持结构化 mouse/mouse_event 步骤、字符串 `keys` 和 `input`/`input_text`/`keys_text`/`raw_key`/`paste_text` 字段别名，并允许 status/snapshot/viewport/pasted-content contains 断言使用单字符串或字符串数组，`expectPrompt.pastedContents` 和 `expectTasks.contains` 使用单对象或对象数组。

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
