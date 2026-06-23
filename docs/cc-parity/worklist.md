# ccgo 功能对齐执行清单（Execution Worklist）

> 本文件由检查表（`docs/cc-parity/sections/` 共 17 章）自动提取生成，覆盖全部 `⚠️ 已建未接`（114 项）与 `❌ 缺失`（227 项）行，共 341 项。
> 目标：逐 cluster 驱动，将所有在范围内的 ⚠️/❌ 改写为 ✅。
> 执行策略：先打通接线（Phase W），再分层补全缺失功能（Phase F1…F11）。
> 每个 cluster 对应一次 TDD 任务（写测试 → 实现 → 审查），可独立提交。

---

## 阶段总览

| 阶段 | 主题 | clusters 数 | 覆盖项数 | 相对规模 |
|------|------|------------|---------|---------|
| W | 接线优先（⚠️ 已建未接） | 24 | 114 | L |
| F1 | SDK 控制协议入口与子类型 | 6 | 38 | M |
| F2 | CLI flags 补全 | 6 | 30 | M |
| F3 | CLI 子命令深度（update/doctor/subcommands） | 5 | 25 | M |
| F4 | slash 命令补全 | 4 | 16 | S |
| F5 | Overlay 与对话框 | 5 | 22 | M |
| F6 | agent 循环 / API 高级特性 | 5 | 15 | M |
| F7 | 工具缺失（EnterWorktree/LSP/Bash/Task） | 4 | 10 | S |
| F8 | MCP 缺口（strict-config/reconnect/OAuth/elicitation） | 4 | 12 | M |
| F9 | Hooks 缺口 | 3 | 7 | S |
| F10 | 沙箱硬化 | 4 | 33 | L |
| F11 | 认证/配置/技能/插件 | 5 | 19 | M |
| **合计** | | **75** | **341** | |

---

## Phase W — 接线优先（已建未接，⚠️）

> 代码与测试均存在，只需在生产调用路径上接通。每 cluster 收益最高，应优先执行。

---

### W-C01: CLAUDE.md 注入系统提示

- [ ] 覆盖项：`MEM-02`
- 文件：`internal/memory/`（已实现），`conversation/runner.go` 或 bootstrap
- 方案：在 conversation runner/bootstrap 的 `SystemPrompt` 构建阶段调用 `memory.LoadScopedClaudeContext(cwd)`，将结果追加到 `runner.SystemPrompt`。
- 执行层：AUTO

---

### W-C02: vim 模式从 config.editorMode 接通

- [ ] 覆盖项：`REPL-31`、`REPL-32`、`REPL-33`、`CFG-52`
- 文件：`cmd/claude/main.go`（构建 `InteractiveOptions`），`internal/repl/loop.go`（`SetVimEnabled`）
- 方案：在 `main.go:341` 构建 `InteractiveOptions` 时读 `mergedSettings.EditorMode`，若为 `"vim"` 则调用 `loop.SetVimEnabled(true)`。
- 执行层：MANUAL（TUI，需人工验证）

---

### W-C03: 自定义快捷键从 keybindings.json 加载

- [ ] 覆盖项：`REPL-54`
- 文件：`internal/tui/keybinding_loader.go`（已实现），`cmd/claude/main.go`
- 方案：在启动 REPL 前读取 `~/.claude/keybindings.json`，调用 `LoadKeyBindingSpecs` + `KeymapFromSpecs`，将生成的 Keymap 传入 Loop。
- 执行层：MANUAL（TUI）

---

### W-C04: loop.go 事件分发补齐（Ctrl+S/Ctrl+G/Ctrl+O/focus 事件）

- [ ] 覆盖项：`REPL-46`、`REPL-47`、`REPL-48`、`REPL-57`
- 文件：`internal/repl/loop.go`（switch case 缺失）
- 方案：在 `loop.go` 的 screen event switch 中补齐 `ScreenEventStashPrompt`、`ScreenEventExternalEditor`（调用 `$EDITOR`）、`ScreenEventToggleTranscript`、`ScreenEventFocusIn/Out` 四个 case。`FocusIn/Out` 触发 stash 提示逻辑，`ExternalEditor` 调用 `os/exec` 启动 `$EDITOR`。
- 执行层：MANUAL（TUI）

---

### W-C05: 权限模式切换同步到 engine（mode-switch→engine sync）

- [ ] 覆盖项：`PERM-MODE-07`、`PERM-PERSIST-03`
- 文件：`internal/repl/loop.go`（`cycleMode`），`internal/conversation/run.go`（`EnginePermissionDecider`）
- 方案：在 `cycleMode` 更新 `l.mode` 后，同步调用 `l.engine.ApplyUpdate(...)` 更新运行时引擎；`persistDecision` 写盘后同样调用 `engine.ApplyUpdate` 刷新内存规则集，避免同会话内二次 Ask。
- 执行层：AUTO

---

### W-C06: 历史条目从磁盘加载传入 REPL

- [ ] 覆盖项：`HIST-03`、`HIST-04`
- 文件：`cmd/claude/main.go`（`NewLoop`），`internal/session/history.go`（`LoadHistory`）
- 方案：在 `main.go` 启动 REPL 前调用 `history.LoadHistory(projectDir)` 并将条目传入 `Loop`，使 Up 箭头/Ctrl+R 能覆盖磁盘历史。
- 执行层：MANUAL（TUI）

---

### W-C07: 会话选择器（ResumePicker/MemorySelector/ThemePicker）接线

- [ ] 覆盖项：`SESS-05`（`InteractiveOptions.ResumeEntries`）、`MEM-09`（`MemoryFiles`）、`OVL-09`（ResumePicker loop 路由）、`OVL-11`（ThemePicker 无参数路径）、`OVL-13`（MemorySelector loop 路由）、`CMD-RESUME-02`、`CMD-MEMORY-01`、`CMD-MODEL-01`
- 文件：`cmd/claude/main.go`（构建 `InteractiveOptions`），`internal/repl/loop.go`（`onCommand` 分支）
- 方案：（1）在 `main.go` 构建 opts 时填充 `ResumeEntries`（调用 `session.ListSessions`）与 `MemoryFiles`（调用 `memory.DiscoverScopedClaudeFiles`）；（2）在 `loop.go:onCommand` 中，当 `/resume` 无参数时触发 `NewResumePicker`，`/memory` 无参数时触发 `NewMemorySelector`，`/theme` 无参数时触发 `NewThemePicker`，`/model` 无参数时触发 `NewModelPicker`。
- 执行层：MANUAL（TUI）

---

### W-C08: HelpScreen/DoctorOverlay 接线到 loop

- [ ] 覆盖项：`OVL-14`（`/help` overlay）、`OVL-15`（`/doctor` overlay）
- 文件：`internal/repl/loop.go`（`onCommand`），`internal/repl/panels.go`
- 方案：在 `onCommand` 分支中，`/help` 改为触发 `NewHelpScreen`，`/doctor` 改为调用 `doctor.Run()` 并以 overlay 形式渲染，而非 `appendLocalTextResult` 文本输出。
- 执行层：MANUAL（TUI）

---

### W-C09: rewind 快照写入接线

- [ ] 覆盖项：`REWIND-01`、`REWIND-02`
- 文件：`internal/repl/rewind_command.go`（只有还原），`internal/conversation/run.go` 或工具层
- 方案：在每次用户消息到达前（conversation runner 或工具执行前钩子），调用 `rewind.Writer.Record(...)` 捕获文件快照；在 `internal/commands/registry.go` 注册 `/rewind` slash 命令。
- 执行层：AUTO（快照写入部分）/ MANUAL（TUI 选择器）

---

### W-C10: cost 持久化（Save/Restore）接线

- [ ] 覆盖项：`COST-02`
- 文件：`internal/costtrack/store.go`（已实现），`cmd/claude/main.go` 退出路径，session resume 路径
- 方案：（1）`--resume` 时在加载历史后调用 `costtrack.Restore(sessionID)` 恢复累计；（2）会话退出前调用 `costtrack.Save(sessionID, cost)`。
- 执行层：AUTO

---

### W-C11: post-compact 文件重附接线

- [ ] 覆盖项：`COMPACT-05`
- 文件：`internal/compact/postcompact.go`（已实现），`internal/conversation/run.go`（`maybeAutoCompact`/`manualCompact`）
- 方案：在 auto-compact 和 manual-compact 成功后，调用 `compact.BuildPostCompactAttachments(readFileState, history)`，将最近读取文件（≤5 个）重附到新历史。需先在 runner 中建立 `readFileState` 跟踪。
- 执行层：AUTO

---

### W-C12: SDK CLI 入口接线（CLI-SDK-01/02）

- [ ] 覆盖项：`CLI-SDK-01`、`CLI-SDK-02`
- 文件：`cmd/claude/main.go`，`internal/sdk/`（已实现）
- 方案：在 `main.go` 增加 `--sdk` flag 或 `sdk` 子命令，检测到时以 stdin→`sdk.Decoder`→`sdk.Controller`+`sdk.Query`→stdout 模式驱动，使 Python/JS SDK 通过管道可调用。
- 执行层：AUTO

---

### W-C13: AskUserQuestion/ExitPlanMode TUI 接线

- [ ] 覆盖项：`TOOL-ASK-01`、`TOOL-ASK-03`、`TOOL-PLAN-02`
- 文件：`internal/repl/loop.go`，工具层 asker 接口
- 方案：在 TUI 模式下，`AskUserQuestion` 触发 permission dialog 走 `loopAsker.Ask`；`ExitPlanMode` 在交互模式下弹出审批对话框，headless 自动批准（已通过）。
- 执行层：MANUAL（TUI）

---

### W-C14: Tool-LSP 操作接线（5 个 LSP 操作）

- [ ] 覆盖项：`TOOL-LSP-01`、`TOOL-LSP-02`、`TOOL-LSP-03`、`TOOL-LSP-04`、`TOOL-LSP-05`
- 文件：`internal/tools/lsp/`，`dispatchLSP` 函数
- 方案：将 `dispatchLSP` 中的 `supported=false` 占位替换为实际 LSP server 调用（`goToDefinition`、`findReferences`、`hover`、`documentSymbol`、`diagnostics`）；同时接通 `settings.Advanced.LSP=true` 时的 LSP server manager 初始化。
- 执行层：AUTO

---

### W-C15: Task 异步后台路径（run_in_background/AgentRegistry）

- [ ] 覆盖项：`TOOL-TASK-02`、`TOOL-TASK-04`、`ORCH-03`、`ORCH-05`
- 文件：`internal/tools/task/`（`callTask`），`internal/orchestration/registry.go`（已实现）
- 方案：（1）在 `callTask` 中读取 `input.RunBackground`，若为 true 则调用 `AgentRegistry.StartBackground`（异步路径）；（2）读取 `input.Model` 并写入 `SidechainOptions.AgentModel`，覆盖 agentfile 模型。
- 执行层：AUTO

---

### W-C16: TOOL-BASH-07 stdout 截断

- [ ] 覆盖项：`TOOL-BASH-07`
- 文件：`internal/tools/bash/`（`formatBashContent`）
- 方案：在 `formatBashContent` 中添加 32MB（2^25 字符）上限截断，从末尾截断并附注提示。
- 执行层：AUTO

---

### W-C17: MCP 接线补齐（OAuth/reconnect/session 到期）

- [ ] 覆盖项：`MCP-39`、`MCP-40`、`MCP-41`、`MCP-42`、`MCP-43`、`MCP-44`
- 文件：`cmd/claude/main.go`（`LoadMCPConfigFromSettingsFiles`），`internal/mcp/reconnect/`（已实现）
- 方案：（1）将 `mcp.FileOAuthAccessTokenProvider` 替换/组合为 `remoteauth.CombinedAccessTokenProvider`（含 `Authorizer` 实现），启用首次 OAuth 获取；（2）将 `reconnect.Run` + `ShouldReconnect` 导入生产路径（SSE/HTTP/WS transport 断线时触发）；（3）在 HTTP transport 中接通 session 到期检测后的连接缓存清除与重建路径。
- 执行层：AUTO

---

### W-C18: Stop Hook 完整字段与阻断语义

- [ ] 覆盖项：`HOOK-27`、`HOOK-30`、`HOOK-35`
- 文件：`internal/conversation/lifecycle.go`（`RunSessionStartHooks`），`internal/conversation/run.go`（`runStopHooks`），`internal/conversation/hooks.go`（`RunNotificationHooks`）
- 方案：（1）`RunSessionStartHooks` 提取并返回 `initialUserMessage` 字段，注入会话首条消息；（2）Stop hook input 补充 `stop_hook_active`/`last_assistant_message` 标准字段；（3）Stop hook 阻断时改为触发循环继续（重新调用 API）而非返回错误；（4）在任何生产路径（如 turn 结束）调用 `RunNotificationHooks`。
- 执行层：AUTO

---

### W-C19: Hook 基础字段补充（agent_id/agent_type）

- [ ] 覆盖项：`HOOK-54`
- 文件：`internal/hooks/input.go`（`BuildInput`）
- 方案：在 `BuildInput` 中补充 `agent_id`/`agent_type` 字段（子 agent 场景时注入），以及 `CLAUDE_ENV_FILE`/`CLAUDE_PLUGIN_ROOT` 环境变量注入。
- 执行层：AUTO

---

### W-C20: global cache scope / tool-search beta header

- [ ] 覆盖项：`LOOP-12`、`LOOP-44`
- 文件：`internal/api/anthropic/betas.go`，`internal/conversation/request.go`，`types.go`
- 方案：（1）在 `Runner` 增加 `UseGlobalCacheScope` 字段，`buildRequest` 按此设置 `cache_control.scope="global"`；（2）在 `DynamicBetaHeaders` 中添加 `tool-search` beta header 逻辑（`requestUsesToolSearch`）。
- 执行层：AUTO

---

### W-C21: 请求元数据 user_id 填充

- [ ] 覆盖项：`LOOP-41`
- 文件：`internal/conversation/request.go`（`buildRequest`），`types.go`
- 方案：在 `buildRequest` 中填充 `request.Metadata["user_id"]`，格式为含 `device_id`+`session_id` 的 JSON 字符串。
- 执行层：AUTO

---

### W-C22: doctor 安装类型报告

- [ ] 覆盖项：`SUBCMD-DOCTOR-01`
- 文件：`internal/doctor/`
- 方案：在 doctor 输出中增加安装类型字段（`npm-global`/`npm-local`/`native`/`package-manager`/`development`/`unknown`），对照当前二进制路径判断。
- 执行层：AUTO

---

### W-C23: SDK initialize/can_use_tool 字段完整化

- [ ] 覆盖项：`SDK-13`、`SDK-55`、`SDK-67`、`SDK-68`
- 文件：`internal/sdk/controller.go`（`initialize` 响应），`internal/sdk/asker.go`（`can_use_tool` 字段）
- 方案：（1）`initialize` 响应补充 `commands`/`models`/`account`/`output_style`/`available_output_styles`/`pid`；（2）`can_use_tool` 请求补充 `input`（完整工具 input map）、`permission_suggestions`、`display_name`、`agent_id`、`title`；（3）stream-json 补充 `system/status` 子类型（如 compacting 状态）。
- 执行层：AUTO

---

### W-C24: 配置 settings key 接线补齐（高频 ⚠️ 字段）

- [ ] 覆盖项：`CFG-07`、`CFG-08`、`CFG-13`、`CFG-14`、`CFG-15`、`CFG-16`、`CFG-18`、`CFG-19`、`CFG-20`、`CFG-26`、`CFG-27`、`CFG-28`、`CFG-32`、`CFG-33`、`CFG-35`、`CFG-36`、`CFG-37`、`CFG-38`、`CFG-39`、`CFG-40`、`CFG-41`、`CFG-42`、`CFG-43`、`CFG-44`、`CFG-45`、`CFG-46`、`CFG-47`、`CFG-48`、`CFG-49`、`CFG-50`、`CFG-51`、`CFG-53`、`PLUGIN-20`、`SKILL-03`、`SKILL-23`
- 文件：`internal/config/settings.go`（字段消费），各功能模块
- 方案：按字段分批接入消费端：
  - **系统提示注入**：`language`（注入 system prompt 语言前缀）、`includeGitInstructions`（控制内置 git 指引）、`statusLine`（TUI 状态行 command 执行）；
  - **运行时校验**：`availableModels` 白名单、`minimumVersion` 版本检查、`forceLoginMethod`；
  - **清理/自动化**：`cleanupPeriodDays`（会话清理任务）、`autoMemoryEnabled`（自动记忆写入）、`plansDirectory`（plan tool 目录）、`claudeMdExcludes`（CLAUDE.md 排除）；
  - **Hook 策略**：`allowManagedHooksOnly`（hook 过滤）、`allowedHttpHookUrls`/`httpHookAllowedEnvVars`（HTTP hook 策略）、`allowManagedPermissionRulesOnly`；
  - **UI/REPL**：`syntaxHighlightingDisabled`、`terminalTitleFromRename`、`spinnerTipsEnabled`、`statusLine`、`fileSuggestion`、`companyAnnouncements`；
  - **其他**：`effortLevel` thinking budget、`alwaysThinkingEnabled`、`worktree.auto`、`respectGitignore`、`verbose` debug log、managed skills 目录、bundled skills 注册、plugin settings base 贡献。
- 执行层：AUTO（大部分）/ MANUAL（UI 相关）

---

## Phase F1 — SDK 控制协议入口与子类型

### F1-C01: SDK CLI 主入口（`--sdk` flag 或子命令）

- [ ] 覆盖项：`SDK-19`、`CLI-SDK-01`、`CLI-SDK-02`
- 文件：`cmd/claude/main.go`
- 方案：新增 `--sdk-control` flag（或 `sdk` 子命令），命中时以 stdin NDJSON→`sdk.Query`→stdout NDJSON 驱动；集成 `sdk.Controller` 处理 interrupt/set_model/initialize；构建对应测试。
- 执行层：AUTO

---

### F1-C02: SDK Controller 缺失子类型（set_permission_mode/set_max_thinking_tokens/mcp_status/get_context_usage）

- [ ] 覆盖项：`SDK-30`、`SDK-31`、`SDK-32`、`SDK-33`
- 文件：`internal/sdk/controller.go`
- 方案：在 `controller.go` switch 中添加 `set_permission_mode`、`set_max_thinking_tokens`、`mcp_status`、`get_context_usage` 四个 case 及回调。
- 执行层：AUTO

---

### F1-C03: SDK Controller 文件/rewind/取消相关子类型

- [ ] 覆盖项：`SDK-34`、`SDK-35`、`SDK-36`、`SDK-37`
- 文件：`internal/sdk/controller.go`
- 方案：添加 `rewind_files`（含 dry_run）、`cancel_async_message`、`seed_read_state`、`hook_callback` 四个 case。
- 执行层：AUTO

---

### F1-C04: SDK Controller MCP 控制子类型

- [ ] 覆盖项：`SDK-38`、`SDK-39`、`SDK-40`、`SDK-41`、`SDK-42`
- 文件：`internal/sdk/controller.go`
- 方案：添加 `mcp_message`、`mcp_set_servers`、`reload_plugins`、`mcp_reconnect`、`mcp_toggle` 五个 case。
- 执行层：AUTO

---

### F1-C05: SDK Controller 任务与配置子类型

- [ ] 覆盖项：`SDK-43`、`SDK-44`、`SDK-45`、`SDK-46`、`SDK-47`、`SDK-48`、`SDK-49`
- 文件：`internal/sdk/controller.go`、`internal/sdk/protocol.go`（read loop）
- 方案：添加 `stop_task`、`apply_flag_settings`、`get_settings`、`elicitation`；在 `readControlLoop` 中处理 `control_cancel_request`、`keep_alive`（无声忽略+可选响应）、`update_environment_variables`。
- 执行层：AUTO

---

### F1-C06: stream-json 缺失事件类型

- [ ] 覆盖项：`SDK-54`（rate_limit_event）、`SDK-57`（auth_status）、`SDK-58`（task_notification）、`SDK-59`（session_state_changed）、`SDK-60`（post_turn_summary）、`SDK-61`（elicitation_complete）、`SDK-62`（prompt_suggestion）、`SDK-63`（streamlined_text）、`SDK-64`（files_persisted）、`SDK-65`（hook_started/progress/response）、`SDK-66`（local_command_output）
- 文件：`cmd/claude/main.go`（`printStreamEvent`），`internal/conversation/events.go`
- 方案：依次在 `printStreamEvent` 和 conversation event 流中添加各缺失事件类型的发射点。
- 执行层：AUTO

---

## Phase F2 — CLI flags 补全

### F2-C01: 调试/模式 flags（--verbose/--debug/--bare/--thinking/--effort）

- [ ] 覆盖项：`CLI-FLAG-17`、`CLI-FLAG-18`、`CLI-FLAG-24`、`CLI-FLAG-25`、`CLI-FLAG-26`
- 文件：`cmd/claude/main.go`
- 方案：注册 `--verbose`、`--debug [filter]`、`--bare`（设 `CLAUDE_CODE_SIMPLE=1`，跳过 hooks/LSP/plugin-sync）、`--thinking <enabled|adaptive|disabled>`、`--effort <low|medium|high|max>` 五个 flag 并接入对应配置路径。
- 执行层：AUTO

---

### F2-C02: 会话管理 flags（--settings/--session-id/--no-session-persistence/--fork-session/--name）

- [ ] 覆盖项：`CLI-FLAG-19`、`CLI-FLAG-20`、`CLI-FLAG-21`、`CLI-FLAG-22`、`CLI-FLAG-23`、`SESS-07`、`SESS-08`、`CLI-FLAG-12`
- 文件：`cmd/claude/main.go`
- 方案：注册 `--settings`（加载额外 settings 文件或 JSON）、`--session-id`、`--no-session-persistence`、`--fork-session`、`--name`；`--resume` 无参数时触发交互式选择器；`--resume + --session-id` 无 `--fork-session` 时报错。
- 执行层：AUTO（大部分）/ MANUAL（交互选择器）

---

### F2-C03: 代理/模型高级 flags（--agent/--agents/--fallback-model/--betas/--tools）

- [ ] 覆盖项：`CLI-FLAG-27`、`CLI-FLAG-28`、`CLI-FLAG-29`、`CLI-FLAG-30`、`CLI-FLAG-31`
- 文件：`cmd/claude/main.go`
- 方案：注册 `--agent`、`--agents <json>`、`--fallback-model`、`--betas`、`--tools <tools>` flag，接入 runner/agent 配置。
- 执行层：AUTO

---

### F2-C04: 输出控制 flags（--strict-mcp-config/--setting-sources/--include-hook-events/--include-partial-messages/--replay-user-messages/--permission-prompt-tool/--json-schema/--max-budget-usd）

- [ ] 覆盖项：`CLI-FLAG-32`、`CLI-FLAG-33`、`CLI-FLAG-41`、`CLI-FLAG-42`、`CLI-FLAG-43`、`CLI-FLAG-44`、`CLI-FLAG-40`、`CLI-FLAG-49`、`MCP-26`（`--strict-mcp-config` 实现）
- 文件：`cmd/claude/main.go`，MCP 加载路径
- 方案：注册上述 flag；`--strict-mcp-config` 过滤 MCP 服务器只保留 `--mcp-config` 指定的来源；`--permission-prompt-tool` 将权限委托给 MCP 工具；`--json-schema` 实现结构化输出验证；`--max-budget-usd` 在超出时中断。
- 执行层：AUTO

---

### F2-C05: 系统提示文件 flags（--system-prompt-file/--append-system-prompt-file/--plugin-dir/--disable-slash-commands/--allow-dangerously-skip-permissions）

- [ ] 覆盖项：`CLI-FLAG-45`、`CLI-FLAG-46`、`CLI-FLAG-47`、`CLI-FLAG-48`、`CLI-FLAG-50`
- 文件：`cmd/claude/main.go`
- 方案：注册上述 flag，从文件读取系统提示内容并追加；`--plugin-dir` 加载额外插件目录；`--disable-slash-commands` 禁用 skill/slash 命令。
- 执行层：AUTO

---

### F2-C06: worktree/chrome/file/pr flags（--worktree/--tmux/--ide/--chrome/--file/--from-pr）

- [ ] 覆盖项：`CLI-FLAG-34`、`CLI-FLAG-35`、`CLI-FLAG-36`、`CLI-FLAG-37`、`CLI-FLAG-38`、`CLI-FLAG-39`
- 文件：`cmd/claude/main.go`
- 方案：注册 `--worktree [-w]`（创建新 git worktree 并在其中启动 REPL）、`--tmux`（配合 worktree 创建 tmux 会话）、`--ide`、`--chrome/--no-chrome`、`--file <specs>`（启动时下载文件资源）、`--from-pr`；MANUAL 项（worktree/tmux/ide/chrome）需标注人工验证。
- 执行层：MANUAL（worktree/tmux/ide/chrome）/ AUTO（file/from-pr 基础部分）

---

## Phase F3 — CLI 子命令深度

### F3-C01: `claude auth login` 高级选项（--console/--sso/--email）

- [ ] 覆盖项：`AUTH-CLI-03`、`AUTH-CLI-04`、`AUTH-CLI-05`、`CLI-SUBCMD-14`、`CLI-SUBCMD-15`、`CLI-SUBCMD-16`、`AUTH-LOGIN-01`、`AUTH-LOGIN-02`
- 文件：`cmd/claude/main.go`（auth login 子命令），`internal/auth/oauth.go`
- 方案：在 `runAuthLogin` 中解析 `--console`/`--sso`/`--email` flag，设置 `LoginWithClaudeAI`/`LoginMethod`/`LoginHint`；接通 REPL 内 `/login`/`/logout` 的 `LocalCommandResultLogin/Logout` 到 `newProductionRouter`。
- 执行层：MANUAL（OAuth 流程）

---

### F3-C02: `claude auth status --json` 与账号详情

- [ ] 覆盖项：`AUTH-CLI-07`、`CLI-SUBCMD-18`
- 文件：`cmd/claude/main.go`（auth status 子命令）
- 方案：在 `auth status` 中添加 `--json`/`--text` flag；JSON 输出含 email/orgId/subscriptionType 等详细字段。
- 执行层：AUTO

---

### F3-C03: `claude update/upgrade` 网络检查与渠道

- [ ] 覆盖项：`CLI-SUBCMD-23`（upgrade 别名）、`SUBCMD-UPDATE-02`~`SUBCMD-UPDATE-07`
- 文件：`internal/update/`，`cmd/claude/main.go`
- 方案：（1）`upgrade` 别名；（2）读取 `autoUpdatesChannel` 设置；（3）网络拉取最新版本，对比后执行下载或输出 "up to date"；（4）检测 homebrew/winget 安装，输出对应命令；（5）多重安装检测；（6）development build 警告。
- 执行层：AUTO

---

### F3-C04: `claude doctor` 深度检查

- [ ] 覆盖项：`SUBCMD-DOCTOR-08`~`SUBCMD-DOCTOR-13`
- 文件：`internal/doctor/`
- 方案：添加多重安装检测、install method mismatch、网络版本对比、Linux sandbox glob 检查、PID 锁清理、MCP 解析错误检查。
- 执行层：AUTO

---

### F3-C05: `claude setup-token` 与 `claude install` 子命令

- [ ] 覆盖项：`CLI-SUBCMD-35`（setup-token）、`AUTH-OAUTH-10`（REPL `/login` 接线）、`AUTH-SETUP-01`、`CLI-SUBCMD-36`（install）、`SUBCMD-SETUP-TOKEN-01`~`03`、`SUBCMD-INSTALL-01`~`04`、`CLI-SUBCMD-10`~`11`（mcp add-from-claude-desktop/reset-project-choices）、`SUBCMD-COMPLETION-06`（completion --output）
- 文件：`cmd/claude/main.go`
- 方案：（1）`setup-token`：触发 `inferenceOnly=true` OAuth 流程，token 写 keychain；（2）`install [target]`：下载并安装 native binary，清理旧 npm 安装；（3）`mcp add-from-claude-desktop`/`mcp reset-project-choices`；（4）`completion --output <file>`。
- 执行层：MANUAL（setup-token/install 需网络/系统权限）

---

## Phase F4 — slash 命令补全

### F4-C01: 缺失 slash 命令（add-dir/rewind/plan/pr-comments/terminal-setup）

- [ ] 覆盖项：`CMD-ADDIR-01`、`CMD-REWIND-01`、`CMD-PLAN-01`、`CMD-PRCMTS-01`、`CMD-TERMSETUP-01`
- 文件：`internal/commands/registry.go`，`internal/repl/commands_*.go`
- 方案：注册上述 slash 命令；`/add-dir` 写入 settings `additionalDirectories`；`/rewind` 调用 `RewindToMessage`（同时依赖 W-C09 快照已接线）；`/plan` 切换 plan 模式；`/pr-comments` 通过 GitHub API 拉取 PR 评论；`/terminal-setup` 检测字体/颜色。
- 执行层：AUTO（add-dir/rewind/plan/pr-comments）/ MANUAL（terminal-setup）

---

### F4-C02: 缺失 slash 命令（branch/rename/diff/copy/exit/fast/stats/tag/tasks/keybindings/reload-plugins/color/statusline）

- [ ] 覆盖项：`CMD-BRANCH-01`、`CMD-RENAME-01`、`CMD-DIFF-01`、`CMD-COPY-01`、`CMD-EXIT-01`、`CMD-FAST-01`、`CMD-STATS-01`、`CMD-TAG-01`、`CMD-TASKS-01`、`CMD-KEYBIND-01`、`CMD-RELOADPLUGINS-01`、`CMD-COLOR-01`、`CMD-STATUSLINE-01`
- 文件：`internal/commands/registry.go`
- 方案：注册上述命令；`/branch` 显示/切换 git 分支；`/rename` 更新会话名；`/diff` 输出 git diff；`/copy` 写系统剪贴板；`/exit` 退出 REPL；`/fast` 切换 Haiku 模型；`/stats` 跨会话统计；`/tag` 标签会话；`/tasks` 任务列表；`/keybindings` 快捷键管理；`/reload-plugins` 清除插件缓存；`/color` 设置提示栏颜色；`/statusline` 切换状态栏。
- 执行层：AUTO（大部分）/ MANUAL（tasks/keybindings overlay）

---

### F4-C03: claude agents 子命令深度（4~5 分组/shadow/model 展示）

- [ ] 覆盖项：`CLI-SUBCMD-21`（--setting-sources）、`SUBCMD-AGENTS-04`（分组/model）、`SUBCMD-AGENTS-05`（shadow 标注）
- 文件：`cmd/claude/main.go`（agents 子命令），`internal/agentfile/`
- 方案：（1）`claude agents --setting-sources user` 过滤；（2）按 User/Project/Local/Plugin/Built-in 五组输出，展示 model 字段；（3）同名 agent 标注 `(shadowed by ...)`。
- 执行层：AUTO

---

### F4-C04: plugin 子命令深度（update/validate/sparse）

- [ ] 覆盖项：`SUBCMD-PLUGIN-12`（update）、`SUBCMD-PLUGIN-13`（validate）、`SUBCMD-PLUGIN-16`（sparse）
- 文件：`cmd/claude/main.go`（plugin dispatch），`internal/plugins/install.go`
- 方案：`plugin update` 实现更新逻辑；`plugin validate` 接入 manifest 校验；`plugin marketplace add --sparse` 支持 monorepo sparse-checkout。
- 执行层：AUTO

---

## Phase F5 — Overlay 与对话框

### F5-C01: onboarding 流程

- [ ] 覆盖项：`OVL-16`（Onboarding 多步向导）
- 文件：`internal/repl/` 新建 onboarding overlay
- 方案：实现首次启动时的主题选择→安全说明→API-key 验证多步向导，与 TrustDialog 集成。
- 执行层：MANUAL

---

### F5-C02: BypassPermissionsDialog / AutoModeOptInDialog

- [ ] 覆盖项：`OVL-31`、`OVL-32`
- 文件：`internal/repl/loop.go`（`cycleMode`），新增 dialog 组件
- 方案：首次切换到 bypassPermissions 时弹出二次确认对话框；首次切换到 auto 模式时弹出 AutoModeOptIn 说明。
- 执行层：MANUAL

---

### F5-C03: CostThresholdDialog / TokenWarning / 上下文压缩倒计时

- [ ] 覆盖项：`OVL-33`、`OVL-38`、`OVL-39`、`OVL-40`、`OVL-41`
- 文件：`internal/repl/` 新增组件，`internal/conversation/run.go`
- 方案：（1）`maxCost` 超出时弹出 CostThresholdDialog；（2）context 超 200k token 时显示 TokenWarning；（3）接近自动压缩阈值时显示倒计时面板；（4）context 按来源分组可视化；（5）memory write 后显示 MemoryUpdateNotification。
- 执行层：MANUAL

---

### F5-C04: MCPServerApprovalDialog 与 elicitation TUI

- [ ] 覆盖项：`OVL-30`（MCP 信任对话框）、`MCP-24`（`enableAllProjectMcpServers` 接线）、`MCP-35`（elicitation TUI）、`OVL-18`（TrustDialog 详情）
- 文件：`internal/repl/`，`internal/mcp/`
- 方案：（1）首次发现 `.mcp.json` 未批准服务器时触发 MCP 信任对话框，确认后写 local settings；（2）实现 MCP elicitation TUI 对话框（表单字段）；（3）TrustDialog 显示 hooks/MCP 风险摘要。
- 执行层：MANUAL

---

### F5-C05: 更新提示/StatusNotices/IdleReturnDialog/WorktreeExitDialog/BypassConfirmation 等其他 overlay

- [ ] 覆盖项：`OVL-43`（更新提示）、`OVL-44`（StatusNotices）、`OVL-50`（IdleReturnDialog）、`OVL-51`（WorktreeExitDialog）、`OVL-45`（SandboxPermissionRequest）、`OVL-05`~`OVL-08`（QuickOpen/历史搜索/全局搜索）、`REPL-30`（MessageSelector）、`REPL-21`（流式渲染）、`REPL-23`（thinking spinner）、`REPL-24`（BriefIdleStatus）、`REPL-49`（! bash 模式）、`REPL-52`（图片粘贴）、`REPL-56`（SIGCONT）、`REPL-58`（OSC-0 标题）、`REPL-59`（OS 通知）、`OVL-42`（终端 OSC 通知）、`OVL-53`（ModelPicker）、`OVL-37`（context 可视化分组）、`OVL-52`（ExportDialog 文件名输入）、`PERM-PERSIST-06`（/permissions TUI overlay）、`PERM-TOOL-02`（工具专属对话框内容）、`CMD-CONFIG-01`（/config 面板）、`CMD-LOGIN-01`（/login OAuth）、`CMD-PLUGIN-01`（/plugin overlay）、`CMD-IDE-01`（IDE 检测）、`CMD-MCP-01`（/mcp 交互）
- 文件：`internal/repl/` 各组件，`internal/tui/`
- 方案：此 cluster 覆盖较多 MANUAL-only overlay，建议按 overlay 类型进一步拆分执行任务：先实现流式渲染（REPL-21，对用户体验影响最大），再依次实现其余。
- 执行层：MANUAL（所有项）

---

## Phase F6 — agent 循环 / API 高级特性

### F6-C01: interleaved thinking / redacted_thinking / effort / task_budget

- [ ] 覆盖项：`LOOP-07`、`LOOP-08`、`LOOP-36`、`LOOP-37`
- 文件：`internal/api/anthropic/betas.go`，`internal/conversation/request.go`，`types.go`
- 方案：（1）在 `DynamicBetaHeaders` 添加 `interleaved-thinking-2025-05-14` header；（2）`contracts/messages.go` 添加 `redacted_thinking` block 类型，`NormalizeForAPI` 保留而非丢失；（3）Request struct 添加 `OutputConfig.effort` 字段及 `effort-2025-11-24` beta；（4）添加 `output_config.task_budget` 字段及 `task-budgets-2026-03-13` beta。
- 执行层：AUTO

---

### F6-C02: API 上下文管理（context-management beta）

- [ ] 覆盖项：`LOOP-35`
- 文件：`internal/conversation/request.go`，`internal/api/anthropic/betas.go`
- 方案：在 Request struct 添加 `context_management` 字段（`edits` 数组，含 `clear_thinking_20251015`/`clear_tool_uses_20250919` 策略），添加 `context-management-2025-06-27` beta header 逻辑。
- 执行层：AUTO

---

### F6-C03: CLAUDE_CODE_EXTRA_BODY / 媒体超限截断 / 非流式超时

- [ ] 覆盖项：`LOOP-48`、`LOOP-49`、`LOOP-50`
- 文件：`internal/api/anthropic/client.go`，`internal/conversation/request.go`
- 方案：（1）解析 `CLAUDE_CODE_EXTRA_BODY` 环境变量，将 JSON 合并到请求 body；（2）在 `NormalizeForAPI/buildRequest` 中添加 `API_MAX_MEDIA_PER_REQUEST=100` 媒体项截断；（3）非流式 fallback 请求添加专用超时（remote 120s，本地 300s）。
- 执行层：AUTO

---

### F6-C04: REPL 内 `/login` OAuth 接线 + TeamRunner 接线

- [ ] 覆盖项：`TEAM-01`（`TeamRunner.RunTeammate` 接线）、`ORCH-12`（agentfile isolation 字段）、`ORCH-13`（agentfile background 字段）、`ORCH-35`（子 agent CLAUDE.md 注入/跳过）
- 文件：`internal/orchestration/runner.go`，`internal/agentfile/agentfile.go`，`internal/tools/task/`
- 方案：（1）`callTeamDispatch`/`callTeamCoordinate`/`callTeamSendMessage` 改为调用 `TeamRunner.RunTeammate` 触发真实模型轮次；（2）agentfile.go 解析 `isolation`/`background` frontmatter 字段，写入 `tool.AgentInfo`；（3）子 agent runner 在非 `omitClaudeMd` 时注入 CLAUDE.md 内容。
- 执行层：AUTO

---

### F6-C05: SessionEnd hook 超时 / PostToolUseFailure / StopFailure / Stop 阻断继续

- [ ] 覆盖项：`HOOK-29`、`HOOK-21`、`HOOK-31`、`HOOK-32`
- 文件：`internal/conversation/hooks.go`，`internal/tools/executor.go`，`internal/conversation/run.go`
- 方案：（1）SessionEnd hook 设 1500ms 专用超时；（2）工具失败路径触发 `PostToolUseFailure` hook；（3）Stop hook 阻断时触发 loop 继续（重新调用 API）而非返回错误；（4）API 错误路径触发 `StopFailure` hook。
- 执行层：AUTO

---

## Phase F7 — 工具缺失

### F7-C01: EnterWorktree / ExitWorktree 工具

- [ ] 覆盖项：`TOOL-WORKTREE-01`、`TOOL-WORKTREE-02`
- 文件：`internal/tools/worktree/`（新建），工具注册
- 方案：实现 `EnterWorktreeTool`（创建 git worktree，切换 session cwd）和 `ExitWorktreeTool`（恢复 cwd，keep/remove worktree 参数），注册为内置工具。
- 执行层：MANUAL（需要 TUI session cwd 更新）

---

### F7-C02: StructuredOutput 工具（SDK 控制路径）

- [ ] 覆盖项：`TOOL-STRUCTURED-01`、`SDK-19`（依赖项）
- 文件：`internal/tools/structured_output/`（新建）
- 方案：实现 `StructuredOutput`（`SyntheticOutputTool`），用于 SDK 控制协议会话的结构化输出提交，输出 NDJSON `control_response`。
- 执行层：AUTO

---

### F7-C03: Bash sleep 拦截 / 沙箱 PowerShell / autoAllowBashIfSandboxed

- [ ] 覆盖项：`TOOL-BASH-08`（sleep 拦截）、`SBX-34`（PowerShell 沙箱）、`SBX-35`（autoAllowBashIfSandboxed）
- 文件：`internal/tools/bash/`，`internal/tools/powershell/`，`internal/sandbox/`
- 方案：（1）Bash 工具检测 `sleep N` 命令（MONITOR_TOOL 特性开启时），返回建议使用 `run_in_background` 提示；（2）`callPowerShell` 调用 `sandboxedShellCommand`；（3）Policy 添加 `autoAllowBashIfSandboxed` 字段，沙箱化时 Bash 工具调用跳过权限提示。
- 执行层：AUTO

---

### F7-C04: 会话/auth 缺失功能（cross-project resume / agentic search / TaskCreate ant-only）

- [ ] 覆盖项：`SESS-09`（跨项目选择器警告）、`SESS-10`（agentic 语义搜索）、`TOOL-TASKCRUD-01`、`TOOL-TASKCRUD-02`（ant-only TaskCreate/Get/Update/List，可标 N/A 或 stub）
- 文件：`internal/session/`，`internal/tools/`
- 方案：（1）交互式选择器检测跨目录会话并弹出警告；（2）`--resume "query"` 支持内容模糊匹配；（3）TaskCreate/Get/Update/List 若属 ant-only 可建 stub 占位。
- 执行层：AUTO（resume 搜索）/ MANUAL（跨项目警告 TUI）

---

## Phase F8 — MCP 缺口

### F8-C01: `--strict-mcp-config` / enterprise managed-mcp / `claude mcp add-from-claude-desktop`

- [ ] 覆盖项：`MCP-26`（strict-mcp-config）、`MCP-27`（enterprise managed-mcp.json）、`MCP-18`（add-from-claude-desktop）、`CLI-SUBCMD-10`、`CLI-SUBCMD-11`（reset-project-choices）
- 文件：`cmd/claude/main.go`，`internal/mcp/`
- 方案：（1）实现 `--strict-mcp-config` flag，过滤只保留 `--mcp-config` 指定服务器；（2）加载 managed-mcp.json，具备独占控制语义；（3）实现 `mcp add-from-claude-desktop` 从 Desktop 配置导入，TUI 多选；（4）`mcp reset-project-choices` 清空 `.mcp.json` 选择记录。
- 执行层：AUTO（strict-mcp/managed）/ MANUAL（add-from-claude-desktop TUI）

---

### F8-C02: MCP server instructions/tool description 截断

- [ ] 覆盖项：`MCP-48`、`MCP-49`
- 文件：`internal/mcp/protocol.go`，`internal/mcp/server_tools.go`
- 方案：server instructions 超 2048 字符时截断并加 `… [truncated]`；工具 description 超 2048 字符时同样截断。
- 执行层：AUTO

---

### F8-C03: /mcp slash 命令（TUI 状态面板/reconnect/enable/disable）

- [ ] 覆盖项：`MCP-53`、`MCP-54`、`MCP-55`、`CMD-MCP-01`
- 文件：`internal/commands/`，`internal/repl/loop.go`
- 方案：实现 `/mcp` TUI 面板（列出服务器名/transport/连接状态）、`/mcp reconnect <name>`、`/mcp enable/disable <name>`（写 `disabledMcpServers` 到 local settings 并断连/重连）。
- 执行层：MANUAL

---

### F8-C04: project scope MCP 信任写入路径（enableAllProjectMcpServers）

- [ ] 覆盖项：`MCP-24`（完整接线，含 F5-C04 TUI 部分）
- 文件：`internal/mcp/load.go`，`internal/repl/run.go`
- 方案：读取 `enableAllProjectMcpServers` 字段，与 `.mcp.json` 服务器过滤逻辑对接；首次信任后写入 local settings 的 `enabledMcpjsonServers`。
- 执行层：AUTO（写入逻辑）

---

## Phase F9 — Hooks 缺口

### F9-C01: async hook / Stop hook 继续语义 / StopFailure / PostToolUseFailure（与 F6-C05 合并执行）

- [ ] 覆盖项：`HOOK-12`（async:true JSON 输出→后台化）、`HOOK-29`（SessionEnd 1500ms 超时）、`HOOK-21`（PostToolUseFailure）、`HOOK-31`（Stop 阻断→继续）、`HOOK-32`（StopFailure）
- 文件：`internal/hooks/`，`internal/conversation/hooks.go`
- 方案：（1）`hookResultFromJSON` 识别 `"async":true` 字段，注册到 AsyncHookRegistry 后台化；（2）其余见 F6-C05。
- 执行层：AUTO

---

### F9-C02: workspace trust guard for hooks / hook_62

- [ ] 覆盖项：`HOOK-62`
- 文件：`internal/hooks/command.go`（或 `hooks.go`）
- 方案：在 hook 执行前检查 `shouldSkipHookDueToTrust()`：若工作区未受信任（trust dialog 未确认），跳过 hook 执行。
- 执行层：MANUAL（依赖 TrustDialog 流程）

---

### F9-C03: SessionEnd 超时 / HOOK-29（独立 cluster）

- [ ] 覆盖项：`HOOK-29`（独立拆出）
- 文件：`internal/conversation/hooks.go`（`runConversationHooks` 或 SessionEnd 专用路径）
- 方案：SessionEnd hook 调用路径设 `CLAUDE_CODE_SESSIONEND_HOOKS_TIMEOUT_MS`（默认 1500ms）专用超时，与通用 hook 超时分离。
- 执行层：AUTO

---

## Phase F10 — 沙箱硬化

> ⚠️ 此阶段全部为 ❌ 缺失，且大量属于安全加固，建议整体审查后分批执行。

### F10-C01: excludedCommands（复合命令检查/剥离前缀）

- [ ] 覆盖项：`SBX-05`、`SBX-06`、`SBX-07`、`SBX-59`（运行时追加）
- 文件：`internal/tools/bash/sandbox.go`，`internal/sandbox/`
- 方案：实现 `excludedCommands` 列表读取，逐段检查复合命令（`&&`/`;`/`|`），剥离环境变量前缀与安全包装器后匹配；支持运行时 `addToExcludedCommands`。
- 执行层：AUTO

---

### F10-C02: 安全加固（settings.json DenyWrite / skills DenyWrite / 裸仓库检测 / 安全加固路径注入）

- [ ] 覆盖项：`SBX-40`、`SBX-41`、`SBX-42`、`SBX-43`、`SBX-44`、`SBX-45`、`SBX-46`、`SBX-47`
- 文件：`internal/sandbox/`（`PolicyFromSettings`），`internal/tools/bash/sandbox.go`
- 方案：（1）自动将 settings.json / `.claude/skills` 路径注入 `DenyWrite`；（2）检测裸仓库，注入 `DenyWrite`，命令结束后清理 `HEAD/objects/refs` 等文件；（3）git worktree 时将主仓库路径加入 `AllowWrite`；（4）`permissions.additionalDirectories` → `AllowWrite`；（5）FileEdit allow/deny 规则 → `AllowWrite`/`DenyWrite`；（6）FileRead deny 规则 → `DenyRead`。
- 执行层：AUTO

---

### F10-C03: 沙箱运行时扩展（enabledPlatforms / checkDependencies / 诊断信息 / 违规追踪）

- [ ] 覆盖项：`SBX-37`、`SBX-38`、`SBX-39`、`SBX-52`、`SBX-53`、`SBX-54`、`SBX-55`、`SBX-56`、`SBX-57`、`SBX-58`
- 文件：`internal/sandbox/`
- 方案：（1）`enabledPlatforms` 字段限制沙箱启用平台；（2）启动时检查 ripgrep/bubblewrap 等依赖，缺失时禁用沙箱；（3）`getSandboxUnavailableReason` 返回人类可读原因；（4）`enableWeakerNestedSandbox`/`enableWeakerNetworkIsolation` 字段；（5）`SandboxViolationStore` 违规追踪；（6）`annotateStderrWithSandboxFailures` / `removeSandboxViolationTags`；（7）`ignoreViolations`；（8）`refreshConfig()` 动态刷新。
- 执行层：AUTO（大部分）/ MANUAL（Linux 内核版本 MANUAL）

---

### F10-C04: 沙箱网络细粒度控制（per-domain / Unix socket / Linux DenyRead/DenyWrite）

- [ ] 覆盖项：`SBX-24`、`SBX-25`（Linux DenyRead/DenyWrite 实现）、`SBX-48`（allowedDomains/deniedDomains）、`SBX-49`（allowUnixSockets）
- 文件：`internal/sandbox/enforce_linux.go`，`internal/sandbox/enforce_darwin.go`
- 方案：（1）Linux landlock 实现 DenyRead/DenyWrite（landlock V4+ overlay FS 方案或通过 seccomp 路径限制）；（2）macOS seatbelt 添加 per-domain network 规则；（3）添加 allowUnixSockets Policy 字段（macOS）。
- 执行层：MANUAL（需 Linux/macOS 实机验证）

---

## Phase F11 — 认证/配置/技能/插件

### F11-C01: OAuth REPL /login 接线 + /logout（已部分覆盖于 F3-C01）

- [ ] 覆盖项：`AUTH-LOGIN-01`、`AUTH-LOGIN-02`、`AUTH-OAUTH-10`（REPL 内接线）、`AUTH-SETUP-01`（setup-token）
- 文件：`internal/repl/loop.go`（`newProductionRouter` 或 `onCommand`），`cmd/claude/main.go`
- 方案：在 REPL router 中注册 `LocalCommandResultLogin` 处理器，触发 `auth.RunLoginFlow`；`LocalCommandResultLogout` 触发 `runAuthLogout`；`setup-token` 触发 `inferenceOnly=true` OAuth。
- 执行层：MANUAL

---

### F11-C02: /permissions TUI overlay / MCP reset / `/config` 面板 / git attribution

- [ ] 覆盖项：`PERM-PERSIST-06`（/permissions TUI overlay）、`CFG-14`/`CFG-15`（git attribution）、`CFG-16`（includeGitInstructions）、`SUBCMD-CONFIG-01`/`02`（/config TUI 面板 + /settings 别名）
- 文件：`internal/repl/`，`internal/commands/`
- 方案：（1）`/permissions` 打开 TUI overlay（Rules/Recent-Denials/Workspace 三个 Tab）；（2）git commit/PR attribution 逻辑读 `attribution`/`includeCoAuthoredBy`；（3）`/config` 打开 Settings TUI 覆盖层，注册 `/settings` 别名。
- 执行层：MANUAL（overlay）/ AUTO（git attribution）

---

### F11-C03: skills 补全（managed skills / bundled skills）

- [ ] 覆盖项：`SKILL-03`（managed skills + CLAUDE_CODE_DISABLE_POLICY_SKILLS）、`SKILL-04`（--add-dir skills）、`SKILL-21`（hooks frontmatter）、`SKILL-23`（bundled skills 注册）
- 文件：`internal/skills/discovery.go`，`internal/commands/registry.go`
- 方案：（1）实现 managed skills 目录加载，支持 `CLAUDE_CODE_DISABLE_POLICY_SKILLS` 门控；（2）`--add-dir` 额外目录的 `.claude/skills/` 发现；（3）SKILL.md frontmatter 解析 `hooks` 字段；（4）注册 bundled skills（程序化内置 skill，随处可用）。
- 执行层：AUTO

---

### F11-C04: 插件补全（npm install / GitHub shorthand）

- [ ] 覆盖项：`PLUGIN-21`（npm install）、`PLUGIN-22`（GitHub shorthand）
- 文件：`internal/plugins/install.go`
- 方案：（1）实现 `installFromNpm`（通过 `npm install` 或 REST API 下载）；（2）解析 `user/repo` shorthand 为 `github.com/user/repo` git clone URL。
- 执行层：AUTO

---

### F11-C05: auth status 详情 / auth 子命令深度 / OVL 通知 / REPL SIGCONT

- [ ] 覆盖项：`AUTH-CLI-07`（auth status 详情）、`REPL-56`（SIGCONT 信号处理）、`OVL-42`（OSC 终端通知生产路径）、`OVL-43`（版本更新检查 UI）
- 文件：`cmd/claude/main.go`（auth status），`internal/repl/`（SIGCONT），`internal/conversation/lifecycle.go`（RunNotificationHooks）
- 方案：（1）`auth status` 输出含 email/orgId/subscriptionType 详情（依赖 OAuth 会话信息）；（2）注册 SIGCONT 信号处理器，恢复 alt-screen 并重绘；（3）将 `RunNotificationHooks` 接入生产路径（turn 完成时触发）；（4）启动时检查并展示更新提示。
- 执行层：AUTO（大部分）/ MANUAL（SIGCONT 需终端挂起测试）

---

## 附录：MANUAL-only clusters 标注

以下 cluster 全部或主要为 MANUAL 执行层，**不可自动化测试**，需人工在 REPL 中验证：

| Cluster | 需人工验证的 MANUAL 项目 |
|---------|----------------------|
| W-C02 | vim 模式键绑定 |
| W-C03 | 自定义快捷键 |
| W-C04 | Ctrl+S/Ctrl+G/Ctrl+O/focus 事件 TUI 效果 |
| W-C07 | 选择器 overlay 弹出与交互 |
| W-C08 | Help/Doctor overlay |
| W-C09 | rewind TUI 选择器 |
| F2-C06 | worktree/tmux/ide/chrome |
| F3-C01 | OAuth 浏览器流程 |
| F3-C05 | setup-token/install |
| F5-C01~C05 | 所有 overlay/对话框 |
| F7-C01 | EnterWorktree TUI session 切换 |
| F8-C03 | /mcp TUI 面板 |
| F9-C02 | workspace trust guard |
| F10-C04 | 沙箱 Linux/macOS 实机验证 |
| F11-C01 | OAuth /login REPL 流程 |
| F11-C02 | /permissions TUI overlay |

---

*生成时间：2026-06-23。数据来源：`docs/cc-parity/sections/` 共 17 章。*
