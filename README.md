# ccgo

Claude Code Go 重写工程。当前仓库已经从规划工作区进入可编译的 Go 工程阶段，已落地“4 件事”、第一批地基模块和第二批运行时核心模块的可运行实现，并正在按 100% Claude Code 行为兼容标准继续补齐。

## 当前状态

已完成：

- Go 工程初始化：`go.mod`、CLI 入口、MCP 入口占位、基础目录和 `Makefile`。
- `tools/sourceaudit`：可扫描 Claude Code 源快照并导出结构化 JSON。
- 契约层：Message、Tool、Command、Permission、Settings、SessionEntry、SDKEvent。
- parity/golden 测试框架：支持 fixture 对比和 `UPDATE_GOLDEN=1` 更新。
- 地基模块：`contracts`、`bootstrap`、`platform`、`config`、`auth`、`model`、`messages`、`session`。
- 第二批运行时核心：`permissions`、`tool`、`api/anthropic`、`conversation`。
- M5 初始核心工具：`internal/tools/file` 已提供文本版 `Read`、`Write`、`Edit`，覆盖读前写、mtime stale guard、唯一匹配/`replace_all`、读结果去重和跨 tool round 的 read-state 保留。
- M6 初始上下文层：`internal/memory`、`internal/compact` 和 session search/title 支撑已落地，覆盖 CLAUDE.md/memdir 扫描、memory manifest、team-memory secret guard、compact 阈值/提示/runner/边界计划、conversation auto-compact 接入、失败熔断、microcompact/cache 初版、persistent cached microcompact 初版、cache version/TTL/prune、session memory summary/recall 初版、session memory rollup/prune compaction、resume context + session memory recall、conversation recall 注入、deterministic/model-backed memory extraction 初版、turn-end memory extraction 落盘、sidechain transcript 路径初版、sidechain runtime start/append/finish 初版、sidechain manager orchestration 初版、sidechain manifest 聚合、sidechain state/list/resume 初版、transcript tail/window/metadata/index loaders、transcript resume conversation builder、index 文本预览字段、流式 transcript 搜索、session 列表分页、remote history token refresh、remote history 全量分页抓取/max-pages 截断状态、remote event transcript materialization/去重追加、remote history 一步 sync 到 transcript 和 transcript 搜索。
- M7 初始 TUI 层：`internal/tui` 已提供轻量 terminal frame renderer、PromptInput 状态机、history 导航、reverse-search 状态/渲染/脚本断言/空结果和选择回填、paste/image hint 输入、pasted-content 引用和提交展开、SGR mouse 解析和滚轮滚动、focus/blur 事件、resize 视口保持、keybinding resolver/config 和 focus/mouse/paste/image key name 覆盖、vim insert/normal/word/delete/count/replace/undo/find/till 动作和 operator 字符范围、REPL screen 模型、permission/task dialog builder、dialog kind/id routing/runtime/status line、runtime 到 REPL screen 的 dialog/status 同步、runtime-aware interaction script runner、prompt text/cursor/expanded 脚本断言、viewport 脚本断言、stale dialog race guard、cancel active、task lifecycle 初版、alternate screen lifecycle 初版、ANSI snapshot 基础、snapshot corpus write/compare、scripted interaction runner/assertions/multi-key 初版、status/dialog/message components、viewport 和 selection 模型。
- 第一批/第二批已重新扫描并进入 parity hardening；未完成项见 `docs/first-second-parity-audit.md`。

## 目录

- `cmd/claude`: Go CLI 入口。
- `cmd/claude-mcp`: MCP server 入口占位。
- `internal/contracts`: 兼容性契约类型。
- `internal/bootstrap`: 进程启动状态和基础上下文。
- `internal/platform`: 路径、文件和平台基础能力。
- `internal/config`: settings 读取、合并和路径解析。
- `internal/auth`: API key / OAuth 环境凭据解析。
- `internal/model`: 模型别名和能力注册表。
- `internal/messages`: 消息构造、归一化和父链处理。
- `internal/session`: Claude transcript JSONL 路径、读写、sidechain 路径/runtime/state/resume 和消息落盘。
- `internal/permissions`: permission rules、allow/deny/ask 判定和模式处理。
- `internal/tool`: Tool interface、registry、schema 校验、权限判定和执行器。
- `internal/api/anthropic`: Anthropic messages API client、错误映射和 SSE streaming 解析。
- `internal/conversation`: conversation turn loop、tool_use 执行、tool_result 回填、fallback 和 transcript append。
- `internal/tools/file`: 文本文件 `Read`、`Write`、`Edit` 工具的初始兼容实现。
- `internal/memory`: CLAUDE.md/memdir 扫描、manifest、session memory summary/recall/rollup compaction 和 memory secret guard。
- `internal/compact`: compact prompt、runner、token warning、auto-compact threshold 和 compact boundary plan。
- `internal/tui`: terminal frame renderer、prompt input、mouse input、keybinding/config、vim 基础、REPL screen、permission/task dialogs/runtime、alternate screen lifecycle、ANSI snapshot/corpus、viewport/selection 和基础组件。
- `tools/sourceaudit`: 源快照审计工具。
- `test/parity`: parity/golden 测试框架。

## 常用命令

```sh
make test
make audit
go run ./cmd/claude --version
go run ./tools/sourceaudit -source /Users/sqlrush/agent/claude-code -out docs/sourceaudit.json
```

## 本地文档

- [docs/claude-code-source-scan.md](docs/claude-code-source-scan.md): 对 `/Users/sqlrush/agent/claude-code` 当前源码快照的结构扫描、关键入口、功能域和缺口记录。
- [docs/claude-code-module-map.md](docs/claude-code-module-map.md): TypeScript 源目录到 Go 模块/包的拆分映射。
- [docs/claude-code-go-rewrite-plan.md](docs/claude-code-go-rewrite-plan.md): 以 100% 行为兼容为目标的 Go 重写路线、里程碑、验证策略和风险清单。
- [docs/sourceaudit.json](docs/sourceaudit.json): `tools/sourceaudit` 生成的机器可读审计结果。
- [docs/first-second-parity-audit.md](docs/first-second-parity-audit.md): 第一批和第二批对 Claude Code 源快照的重新扫描、已补齐项和剩余缺口。

## 当前扫描结论

- 源快照包含 `src` 约 1,902 个源码文件，约 513,237 行，约 33MB。
- 快照不是完整可构建 npm 工程：根目录缺少 `package.json` 和锁文件。
- 快照存在缺失 import 目标，尤其是 `src/types/message`、`src/types/tools`、`src/types/utils`、`src/constants/querySource` 等；Go 重写必须先补齐契约或用官方 CLI 行为测试反推。
- 重写不应按文件逐行翻译，而应先固化消息、工具、权限、会话、MCP、TUI 等稳定契约，再逐模块替换实现。
