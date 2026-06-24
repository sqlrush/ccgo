# CC 全功能对照功能测试清单（主索引）

> 设计 spec：[`docs/superpowers/specs/2026-06-22-cc-parity-functional-tests.md`](../superpowers/specs/2026-06-22-cc-parity-functional-tests.md)
> 产出方式：方案 A —— 逐功能域并行审计真实 CC 源码（`/Users/sqlrush/agent/claude-code/src`）枚举功能，
> 并核对 ccgo 代码定当前状态。每域一份 section 文件（见下表链接）。

**状态图例：** `✅ 通过`（已实现且在运行的 ccgo 中可达） · `⚠️ 已建未接`（代码+测试在，但运行路径不可达） ·
`❌ 缺失`（CC 有，ccgo 未实现） · `N/A`（按 §10 锁定范围 OUT-of-scope，故意不做）。

---

## 总览（计数由各 section 文件自动重算）

| 指标 | 数值 |
|---|---|
| 测试项总数 | **1086** |
| `✅ 通过` | **943** |
| `⚠️ 已建未接` | **1** |
| `❌ 缺失` | **0** |
| `N/A`（OUT-of-scope） | **142** |
| **In-scope（剔除 N/A）** | **963** |
| **In-scope 通过率** | **99.9%**（943/944；起始 64.6%） |

**怎么读这份清单：** `⚠️`（114）+ `❌`（227）= 距 CC 100% 对齐还要做的工作。`⚠️` 是"地基已造、插上线即可用"
的接线工作（成本低、收益高）；`❌` 是 CC 有而 ccgo 从未实现的真缺口（需新建）。`✅` 64.6% 反映：之前
完成的是**计划内的 88 个 task**，而 CC 的完整功能面比那份计划更大——这正是本清单要暴露的差距。

---

## 分域汇总

| 域 | 文件 | 行数 | ✅ | ⚠️ | ❌ | N/A |
|---|---|---:|---:|---:|---:|---:|
| 1 CLI 与入口 | [01-cli-entrypoints.md](sections/01-cli-entrypoints.md) | 112 | 48 | 3 | 44 | 17 |
| 2 交互 REPL / TUI | [02-repl-tui.md](sections/02-repl-tui.md) | 60 | 41 | 9 | 9 | 1 |
| 3 Overlay 与对话框 | [03-overlays-dialogs.md](sections/03-overlays-dialogs.md) | 54 | 26 | 8 | 19 | 1 |
| 4 agent 循环 / API | [04-agent-loop-api.md](sections/04-agent-loop-api.md) | 50 | 39 | 3 | 8 | 0 |
| 5 工具 | [05-tools.md](sections/05-tools.md) | 91 | 71 | 11 | 8 | 1 |
| 6 权限 | [06-permissions.md](sections/06-permissions.md) | 36 | 32 | 3 | 1 | 0 |
| 7 slash 命令 | [07-slash-commands.md](sections/07-slash-commands.md) | 73 | 24 | 8 | 18 | 23 |
| 8 CLI 子命令（行为深度） | [08-cli-subcommands.md](sections/08-cli-subcommands.md) | 62 | 34 | 1 | 27 | 0 |
| 9 MCP | [09-mcp.md](sections/09-mcp.md) | 55 | 37 | 8 | 9 | 1 |
| 10 Hooks | [10-hooks.md](sections/10-hooks.md) | 64 | 41 | 4 | 6 | 13 |
| 11 会话 / 记忆 / 压缩 | [11-sessions-memory-compact.md](sections/11-sessions-memory-compact.md) | 38 | 25 | 8 | 5 | 0 |
| 12 配置 / 插件 / skills | [12-config-plugins-skills.md](sections/12-config-plugins-skills.md) | 119 | 78 | 37 | 4 | 0 |
| 13 认证 | [13-auth.md](sections/13-auth.md) | 45 | 37 | 2 | 6 | 0 |
| 14 编排 | [14-orchestration.md](sections/14-orchestration.md) | 39 | 28 | 5 | 3 | 3 |
| 15 沙箱 | [15-sandbox.md](sections/15-sandbox.md) | 61 | 29 | 0 | 28 | 4 |
| 16 SDK | [16-sdk.md](sections/16-sdk.md) | 68 | 32 | 4 | 32 | 0 |
| 17 OUT-of-scope 附录 | [17-out-of-scope.md](sections/17-out-of-scope.md) | 59 | 0 | 0 | 0 | 59 |
| **合计** | | **1086** | **622** | **114** | **227** | **123** |

---

## worklist 预览（下一步基于此细化、依赖排序）

下面是从 `⚠️`/`❌` 行汇总出的主题，**仅为预览**——正式 worklist（依赖排序、可执行任务、对应测试项 ID）作为下一步产出。

### P0 — 高价值接线（`⚠️`，地基已造、插线即可用）
- **CLAUDE.md 注入系统提示**（`MEM-02`）：`LoadScopedClaudeContext` 已建但 bootstrap 从不调用 → 模型看不到项目记忆。
- **Shift+Tab 模式切换同步到决策引擎**（`PERM-MODE-07/PERM-PERSIST-03`）：状态栏变了但权限决策用的是值拷贝的旧引擎。
- **流式渲染 text_delta**（`REPL-21`）：现在只渲整轮完成的消息，看不到实时输出。
- **overlay 触发器接线**（`OVL-09/11/13/14`、`CMD-RESUME/THEME/MEMORY/HELP/CONFIG/MODEL`）：picker/对话框组件已建，但 slash 命令只输出纯文本、不打开 overlay。
- **vim 模式接线**（`REPL-31..33`）：3259 行 vim 实现已建，但启动时从不读取 `editorMode`。
- **rewind 快照写触发**（`REWIND-01`）：还原逻辑已建，但从不在文件修改工具前写快照 → `/rewind` 无数据可还原。
- **post-compact 文件重附**（`COMPACT-05`）：`BuildPostCompactAttachments` 已建但压缩路径不调用。
- **Task `run_in_background` → AgentRegistry**（`ORCH-03`）+ **`model` 覆盖生效**（`TOOL-TASK-04`）。
- **真实 Team 执行接线**（`TEAM-01`）：`TeamRunner` 已建，但 dispatch/coordinate 仍 append-only。
- **远程 MCP OAuth + reconnect 接线**（`MCP-39/43`）：`remoteauth`/`reconnect` 包零生产引用。
- **Notification hook emit-side**（`HOOK-35`）：`RunNotificationHooks` 从不被调用。
- **~31 个 settings key 应用**（`CFG/STYLE` 多项）：effortLevel/thinking 预算、theme 渲染、alwaysThinking、cleanupPeriodDays 等解析了却未生效。

### P1 — 缺失功能补齐（`❌`，CC 有、ccgo 无）
- **SDK CLI 入口 + 控制子类型**（`SDK-19`、`SDK-30..45`）：无 `cmd/claude` 控制模式入口 + 缺 16 个控制子类型 → Python/JS SDK 无法对接；`initialize` 响应字段不全。
- **认证补齐**（`AUTH-LOGIN-01/02`、`AUTH-SETUP-01`、`AUTH-CLI-*`）：`/login`·`/logout` 在 REPL 无效果；缺 `setup-token`；`auth login` 缺 console/sso/email flags。
- **沙箱硬化**（`SBX-40..43`、`SBX-24/25`、`SBX-34/35`）：缺 settings/skills 自动保护注入、Linux deny-path、PowerShell 沙箱。
- **~44 个 CLI flag**（`CLI-FLAG-*`）：`--settings`/`--verbose`/`--strict-mcp-config`/`--fork-session`/`--fallback-model` 等约 2/3 flag 面未注册。
- **`claude update` 真实实现 + `install`/`setup-token` 子命令**（`SUBCMD-*`）。
- **缺失 hook 事件**（`HOOK-12/21/32`）：`async:true`、`PostToolUseFailure`、`StopFailure` 触发。
- **API-side context management + betas**（`LOOP-07/08/35`）：interleaved-thinking / redacted_thinking / context_management 字段。
- **EnterWorktree/ExitWorktree 工具**（`TOOL-WORKTREE-01/02`）。
- **bypass/auto 模式二次确认对话框**（`OVL-31/32`，安全相关）。

---

## 下一步
1. 用户 review 本清单（及各 section）。
2. 据 `⚠️`/`❌` 产出正式**接线 & 优化 worklist**：依赖排序、每项关联测试项 ID、给出 TDD 任务。
3. 以"让测试项转 `✅`"为目标实现（`writing-plans` + `subagent-driven-development`）。
4. 将 `AUTO` 行随转绿沉淀为 `e2e/` Go 回归套件（CI 可跑）。
