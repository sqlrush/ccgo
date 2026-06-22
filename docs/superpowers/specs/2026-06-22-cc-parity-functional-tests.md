# CC 全功能对照测试清单 — 设计 Spec

**日期：** 2026-06-22
**目标：** 一份对照 Claude Code **真实功能面**的功能测试清单，力求覆盖 **CC 100% 的功能**。每条
是一个 pass/fail 的功能检查，并给出 ccgo 的当前状态判定。清单随后作为两件事的来源：(a) 一份接线
与优化 worklist（把 ⚠️/❌ 行推到 ✅）；(b) 一套回归测试套件（AUTO 行 → Go `e2e/`）。

**事实来源：** CC 参考源码 `/Users/sqlrush/agent/claude-code/src`（直接审计，**不**信自述 roadmap
文档），并与 ccgo 代码交叉核对当前状态。

**锁定范围参照：** `docs/gap-audit-2026-06-21.md` §10（IN/OUT）。OUT-of-scope 的 CC 功能在本清单
里**仍然枚举**（标 `N/A`），以便清单反映 CC 的完整功能面。

---

## 1. 决策（已与用户确认 2026-06-22）

1. **范围：** 枚举 CC 全部功能；OUT-of-scope 项逐条列出并标 `N/A` + 原因。
2. **执行模型：** 分层 —— `AUTO`（headless `claude --print` / 程序化，可机器断言）
   + `MANUAL`（交互 TUI / 对话框，人工跑、带预期结果）。
3. **产物：** 先出清单（Markdown），再把 `AUTO` 行沉淀为可运行的 Go 套件。

---

## 2. 交付物 & 行格式

一份主清单 Markdown：`docs/cc-parity/functional-tests.md`（外加本设计 spec）。按功能域分章（§4）。
每条测试项为一行：

| 列 | 含义 |
|---|---|
| **ID** | 稳定编号，`AREA-FEATURE-NN`（如 `TOOL-BASH-03`、`MCP-OAUTH-02`）。供 worklist + 套件引用。 |
| **功能** | 受测的 CC 具体行为。 |
| **执行层** | `AUTO`（headless 可断言）/ `MANUAL`（交互 TUI）/ `N/A`（out-of-scope）。 |
| **测试（given → when → then）** | 前置 → 操作 → **预期结果**（即通过判据）。 |
| **CC 参照** | `src/…:line`，证明 CC 确实有此行为（防臆造）。 |
| **状态** | `✅ 通过` / `⚠️ 已建未接`（代码+测试在，但运行路径不可达）/ `❌ 缺失` / `N/A`。 |

**状态列即 gap 分析。** 每章末尾 + 顶部汇总给出各状态计数。`⚠️` 行 → 接线工作；`❌` 行 → 真缺口；
二者合起来 = worklist。

### 样例行（让格式具体可见）

```
| TOOL-BASH-01 | Bash 执行 shell 命令，返回 stdout/stderr/退出码 | AUTO | 前置:一个仓库;操作:`claude --print "run: echo hi"` 触发 Bash `echo hi`;预期:结果含 "hi",exit 0 | src/tools/BashTool/*.ts | ✅ 通过 |
| PERM-MODE-03 | Shift+Tab 循环切换权限模式(default→acceptEdits→plan→bypass) | MANUAL | 前置:REPL 中;操作:按 Shift+Tab 4 次;预期:状态行指示器循环并回到 default | src/components/PromptInput/PromptInputFooterLeftSide.tsx:70 | ✅ 通过 |
| CMD-RESUME-02 | `/resume` 打开交互式会话选择器 | MANUAL | 前置:有历史会话;操作:`/resume`(无参);预期:选择器对话框列出同仓库会话,方向键+回车加载 | src/screens/ResumeConversation.tsx | ⚠️ 已建未接(经 `/resume <id|N>` 的功能恢复可用;选择器对话框需 P2 触发器) |
| SDK-QUERY-01 | `claude` 暴露可 import 的 SDK query() 入口(经 CLI) | AUTO | 前置:二进制;操作:以 SDK/控制模式调用;预期:control_request/response NDJSON 驱动一个回合 | src/entrypoints/agentSdkTypes.ts:112 | ⚠️ 已建未接(sdk.Query 已建+已测;无 cmd/claude 子命令) |
| REMOTE-TELEPORT-01 | teleport 到云端远程 agent 会话 | N/A | — | src/… | N/A(云端栈 OUT-of-scope §10) |
```

---

## 3. 清单如何产出（方案 A —— 已确认）

**混合 + 真源审计。** 逐功能域（§4）做一遍：
1. **枚举** 该域的 CC 功能 —— 读 `/Users/sqlrush/agent/claude-code/src`（标 file:line），以 gap 审计 /
   phase 计划作为"去哪儿看"的线索清单，但 CC 源码对"有什么"是权威。
2. **写** 每个功能一行功能测试（given→when→then + 预期），打 Layer 标签。
3. **判定 ccgo 状态** —— 核对 ccgo 代码（`grep`/`go doc`）：已实现+已接线（`✅`）、已建但不可达
   （`⚠️`）、缺失（`❌`）、还是 out-of-scope（`N/A`）？

产出采用扇出：每个功能域一个 agent（并行），各自返回本域的行，由 controller 汇成主文档。（与最初
gap 审计同法 —— `dispatching-parallel-agents`。）这是研究/枚举型交付物、不是写代码，所以不走
writing-plans；writing-plans 留到后面，用于清单产出的接线 worklist。

---

## 4. 功能域分章（CC 全功能面）

1. **CLI 与入口** —— `claude`、`--print`、所有 flag、子命令（`mcp`/`auth`/`agents`/`doctor`/`update`/`completion`/`config`/`plugin`/`mcp serve`）。
2. **交互 REPL / TUI** —— 原始输入+编辑、实时流式渲染、spinner/进度、resize、Ctrl-C/ESC 中断、vim 模式、模式切换 UI、bracketed paste、alt-screen。
3. **Overlay 与对话框** —— slash 菜单+autocomplete、resume 选择器、theme 选择器、`/memory` 选择器、HelpV2、Doctor 屏、onboarding/TrustDialog、全部权限对话框（Bash/Edit/Write/WebFetch/Skill/NotebookEdit/AskUserQuestion/Plan…）、status/cost/context 面板、通知。
4. **agent 循环 / API** —— 流式、扩展思考+signature、prompt 缓存、模型 fallback、`stop_reason` 控制流（max_tokens/pause_turn/refusal/ctx-window）、孤儿 tool_result、micro + auto 压缩、token 预算。
5. **工具** —— Read/Write/Edit/MultiEdit/NotebookEdit/Bash/BashOutput/KillShell/Glob/Grep/WebFetch/WebSearch/Task/TodoWrite/AskUserQuestion/EnterPlanMode/ExitPlanMode/LSP/Skill/StructuredOutput/Worktree/Config —— schema、行为、prompt。
6. **权限** —— 规则匹配、4 种模式（default/acceptEdits/plan/bypass）、交互 ask、allow-once/allow-always 持久化、`/permissions`、各工具对话框。
7. **slash 命令** —— 完整 ~78 命令注册表，每条:分发 + 效果。
8. **CLI 子命令** —— `doctor`/`update`/`agents`/`completion`/`mcp …`/`auth …`/`config`/`plugin`。
9. **MCP** —— stdio/SSE/HTTP/WS 传输、`claude mcp add/add-json/list/get/remove/serve`、远程 OAuth（RFC 8414/9728/7591）+ token 缓存/刷新、elicitation、reconnect/backoff、`.mcp.json` + settings 作用域。
10. **Hooks** —— 完整事件分类（SessionStart/End/PreToolUse/PostToolUse/UserPromptSubmit/Stop/SubagentStart/Stop/Notification/PreCompact/PostCompact/…）、matcher、并行 deny>ask>allow、输入/输出 schema。
11. **会话 / 记忆 / 压缩** —— resume/continue、rewind/checkpoint、CLAUDE.md 作用域层级 + `@import`、`history.jsonl`、cost 持久化、压缩后文件还原。
12. **配置 / 插件 / skills / output-styles** —— 设置层级（user/project/local/managed）、插件、skills（内置 + 发现 + 激活）、输出样式。
13. **认证** —— 交互式 OAuth 登录（callback/浏览器/交换）、`/login`·`/logout`·`claude auth`、keychain、apiKeyHelper、API-key/env 优先级。
14. **编排** —— 同步子 agent、异步/后台（`run_in_background`）、真实本地 Team（dispatch/coordinate）、git-worktree 隔离、Task `model`/`isolation`。
15. **沙箱** —— macOS seatbelt、Linux landlock+seccomp、`dangerouslyDisableSandbox` 语义、fail-closed。
16. **SDK** —— 控制协议（control_request/response）、`canUseTool`/interrupt/set_model、可 import 的 `Query` 入口。
17. **OUT-of-scope 附录**（`N/A`）—— 云端栈（teleport/RemoteAgentTask/CCR/云端 cron/远程 CLI）、GitHub/Slack App + 会话分享、IDE/桌面/Chrome/移动/语音伴生端、服务端 feature-flag/AB（statsig）、内部遥测 + debug-only 命令。

---

## 5. 后续闭环（清单之后）

1. 用户 review 清单。
2. 把 `⚠️ 已建未接` + `❌ 缺失` 汇总成一份**接线 & 优化 worklist**，按依赖排序
   （如:真实 Team 执行、SDK CLI 入口、rewind 快照写触发、overlay slash 触发器、
   远程-MCP-OAuth 的 `cmd/claude` 接线、PowerShell 沙箱对齐 …）。
3. 以"让这些测试行转 ✅"为目标实现 worklist（TDD 微提交，依 `writing-plans` +
   `subagent-driven-development` —— 此时才调用 writing-plans）。
4. 把 `AUTO` 行随转绿沉淀为 `e2e/` Go 测试（CI 可跑回归套件）。
