## 17. OUT-of-scope 附录

以下各项均为 CC 真实存在的功能，ccgo 明确不实现（原因：依赖 Anthropic 私有云后端、配套闭源 App、内部遥测或纯调试工具），全部标记为 `N/A`，此处仅作完整功能面的可见台账。

| ID | 功能 | 执行层 | 测试 | CC 参照 | 状态 |
|---|---|---|---|---|---|
| **云端远程栈** | | | | | |
| NA-01 | `teleport` 子命令 — 将本地 REPL 会话"传送"到 CCR 云端容器执行 | N/A | — | `src/commands/teleport/index.js`（实现在 `src/utils/teleport/`） | N/A（云端栈；需 Anthropic 私有 CCR 后端） |
| NA-02 | `RemoteAgentTask` — 在远端云会话中运行 agent 任务并轮询事件 | N/A | — | `src/tasks/RemoteAgentTask/RemoteAgentTask.tsx:1` | N/A（云端栈；依赖 `fetchSession`/`pollRemoteSessionEvents` 私有 API） |
| NA-03 | `RemoteTrigger` 工具 — agent 在云端会话内触发/管理 webhook trigger | N/A | — | `src/tools/RemoteTriggerTool/RemoteTriggerTool.ts:46` | N/A（云端栈；feature flag `AGENT_TRIGGERS_REMOTE`，调用 Anthropic trigger API） |
| NA-04 | CCR upstreamproxy / relay — 容器内 CONNECT-over-WebSocket 出口代理 | N/A | — | `src/upstreamproxy/upstreamproxy.ts:2`、`src/upstreamproxy/relay.ts:3` | N/A（云端栈；仅在 CCR 容器内有意义） |
| NA-05 | `remote-setup` 子命令 — 配置远端云环境（availability: `claude-ai`） | N/A | — | `src/commands/remote-setup/index.ts:10` | N/A（云端栈；需 claude.ai 后端） |
| NA-06 | `remote-env` 子命令 — 管理远端会话环境变量 | N/A | — | `src/commands/remote-env/index.ts` | N/A（云端栈；同上） |
| NA-07 | 云端调度 Cron 工具三件套：`CronCreate` / `CronDelete` / `CronList` | N/A | — | `src/tools/ScheduleCronTool/CronCreateTool.ts`、`CronDeleteTool.ts`、`CronListTool.ts` | N/A（云端栈；向 Anthropic 调度服务注册 cron job） |
| NA-08 | Bridge / 云端 code-session 协议 — worker_jwt 获取、WebSocket session 连接、trustedDevice token | N/A | — | `src/bridge/codeSessionApi.ts:87`、`src/bridge/trustedDevice.ts:51`、`src/bridge/remoteBridgeCore.ts:15` | N/A（云端栈；私有 `/v1/code/sessions` 后端） |
| NA-09 | `SessionsWebSocket` / `RemoteSessionManager` — 云端会话 WebSocket 管理 | N/A | — | `src/remote/RemoteSessionManager.ts`、`src/bridge/SessionsWebSocket.ts` | N/A（云端栈） |
| NA-10 | `createDirectConnectSession` / `directConnectManager` — 直连 CCR session server | N/A | — | `src/server/createDirectConnectSession.ts`、`src/server/directConnectManager.ts` | N/A（云端栈） |
| **GitHub App / Actions 集成** | | | | | |
| NA-11 | `install-github-app` 子命令 — 为仓库安装 Claude GitHub Actions（OAuth 流、写 secrets、生成 workflow） | N/A | — | `src/commands/install-github-app/index.ts:4`、`install-github-app.tsx` | N/A（依赖 Anthropic 托管 GitHub App 注册） |
| NA-12 | GitHub Actions 元数据采集 & 遥测上报（`is_github_action`、runner 环境等字段） | N/A | — | `src/types/generated/events_mono/claude_code/v1/claude_code_internal_event.ts:31` | N/A（内部遥测；数据送 Anthropic 私有端点） |
| **Slack App / 会话分享** | | | | | |
| NA-13 | `install-slack-app` 子命令 — 安装 Claude Slack App（availability: `claude-ai`） | N/A | — | `src/commands/install-slack-app/index.ts:3` | N/A（依赖 Anthropic 托管 Slack App 注册） |
| NA-14 | `share` 子命令 — 生成 claude.ai 会话分享链接 | N/A | — | `src/commands/share/index.js`（stub: `isEnabled: () => false`） | N/A（依赖 Anthropic 服务端 session share 后端） |
| **IDE 扩展 / 桥接** | | | | | |
| NA-15 | IDE bridge（VS Code / JetBrains）— `initReplBridge`、`replBridge`、桥接协议 | N/A | — | `src/bridge/initReplBridge.ts`、`src/bridge/replBridge.ts`、`src/bridge/bridgeMain.ts` | N/A（配套闭源 IDE 扩展；另一半不公开） |
| NA-16 | IDE bridge 权限回调 & 消息路由（`bridgePermissionCallbacks`、`inboundMessages`） | N/A | — | `src/bridge/bridgePermissionCallbacks.ts`、`src/bridge/inboundMessages.ts` | N/A（同 NA-15） |
| NA-17 | LSP / diagnostics bridge — 把 IDE 诊断信息注入 agent 上下文（`src/services/lsp/`） | N/A | — | `src/services/lsp/`（目录） | N/A（需 IDE 扩展对端） |
| **桌面 App / Chrome 扩展 / 移动端** | | | | | |
| NA-18 | `desktop` 子命令 — 启动/连接桌面 App（availability: `claude-ai`） | N/A | — | `src/commands/desktop/index.ts:18` | N/A（配套闭源桌面 App） |
| NA-19 | `chrome` 子命令 — 连接 Chrome 扩展 | N/A | — | `src/commands/chrome/chrome.tsx` | N/A（配套闭源 Chrome 扩展） |
| NA-20 | `mobile` 子命令 — 移动端配对/桥接 | N/A | — | `src/commands/mobile/mobile.tsx` | N/A（配套闭源移动 App） |
| NA-21 | trusted-device JWT 配对（移动 App ↔ CLI bridge） | N/A | — | `src/bridge/trustedDevice.ts:190`、`src/bridge/jwtUtils.ts:22` | N/A（配套闭源移动 App） |
| **语音 / STT** | | | | | |
| NA-22 | `voice` 子命令 & 语音录制服务（push-to-talk，`audio-capture-napi`） | N/A | — | `src/commands/voice/voice.ts`、`src/services/voice.ts:1` | N/A（依赖 Anthropic voice_stream 私有端点 + native audio 模块） |
| NA-23 | `voiceStreamSTT` — 向 Anthropic voice_stream WebSocket 发送 PCM 流并转录 | N/A | — | `src/services/voiceStreamSTT.ts:1` | N/A（私有 voice_stream 端点） |
| NA-24 | `voiceKeyterms` — 构建 STT keyterm boosting 列表（Deepgram keywords） | N/A | — | `src/services/voiceKeyterms.ts:58` | N/A（同 NA-23） |
| **buddy / companion 精灵** | | | | | |
| NA-25 | `buddy` companion 精灵动画（`CompanionSprite`、`useBuddyNotification`） | N/A | — | `src/buddy/companion.ts`、`src/buddy/CompanionSprite.tsx` | N/A（视觉彩蛋；只在 claude.ai 产品线启用） |
| **服务端特性开关 / A-B 实验** | | | | | |
| NA-26 | GrowthBook feature flags — 运行时从 Anthropic 服务端拉取实验分组与动态配置 | N/A | — | `src/services/analytics/growthbook.ts:1` | N/A（服务端控制；parity 在构造上不可达） |
| NA-27 | `statsig` 动态配置（`claude_code_global_system_caching` 等） | N/A | — | `src/tools.ts:191` | N/A（同 NA-26） |
| NA-28 | `remoteManagedSettings` — 从 Anthropic API 拉取并覆盖本地 settings | N/A | — | `src/services/remoteManagedSettings/index.ts:4` | N/A（服务端驱动；私有 API） |
| NA-29 | `settingsSync` — 用户 settings 云端同步服务 | N/A | — | `src/services/settingsSync/` | N/A（依赖 Anthropic 云端存储） |
| **内部遥测 / 事件上报** | | | | | |
| NA-30 | 1st-party 事件日志（OpenTelemetry → Anthropic 私有收集端点） | N/A | — | `src/services/analytics/firstPartyEventLogger.ts:105` | N/A（内部遥测；私有端点） |
| NA-31 | Datadog 事件上报（`tengu_log_datadog_events` gate 控制） | N/A | — | `src/services/analytics/datadog.ts:13`、`sink.ts:11` | N/A（内部遥测） |
| NA-32 | `internalLogging` — 会话生命周期内部日志服务 | N/A | — | `src/services/internalLogging.ts` | N/A（内部遥测） |
| NA-33 | `diagnosticTracking` — 诊断事件追踪服务 | N/A | — | `src/services/diagnosticTracking.ts` | N/A（内部遥测） |
| NA-34 | `teamMemorySync` — 把 team memory 文件上传到 Anthropic 云端（watcher + secret 扫描） | N/A | — | `src/services/teamMemorySync/watcher.ts:96` | N/A（依赖 Anthropic 云端 team memory 存储） |
| **debug-only / dev-only 命令** | | | | | |
| NA-35 | `/ant-trace` — Anthropic 内部 trace 命令 | N/A | — | `src/commands/ant-trace/` | N/A（内部调试工具） |
| NA-36 | `/heapdump` — Node.js heap dump | N/A | — | `src/commands/heapdump/` | N/A（内部调试工具） |
| NA-37 | `/mock-limits` & `/reset-limits` — 模拟/重置速率限制（测试用） | N/A | — | `src/commands/mock-limits/`、`src/commands/reset-limits/` | N/A（内部调试工具） |
| NA-38 | `/perf-issue` — 上报性能问题 | N/A | — | `src/commands/perf-issue/` | N/A（内部工具；上报至私有端点） |
| NA-39 | `/debug-tool-call` — 调试工具调用 | N/A | — | `src/commands/debug-tool-call/` | N/A（内部调试工具） |
| NA-40 | `/ctx_viz` — 上下文可视化（内部） | N/A | — | `src/commands/ctx_viz/` | N/A（内部调试工具） |
| NA-41 | `/break-cache` — 手动破缓存（调试） | N/A | — | `src/commands/break-cache/` | N/A（内部调试工具） |
| NA-42 | `/backfill-sessions` — 回填会话数据（内部） | N/A | — | `src/commands/backfill-sessions/` | N/A（内部工具） |
| NA-43 | `/good-claude` — 内部正向反馈快捷命令 | N/A | — | `src/commands/good-claude/` | N/A（内部工具） |
| NA-44 | `/btw` — 内部便签快捷命令 | N/A | — | `src/commands/btw/` | N/A（内部工具） |
| NA-45 | `/stickers` — 内部贴纸/彩蛋命令 | N/A | — | `src/commands/stickers/` | N/A（内部工具） |
| NA-46 | `/passes` — 内部 pass 统计命令 | N/A | — | `src/commands/passes/` | N/A（内部工具） |
| NA-47 | `/thinkback` & `/thinkback-play` — 思考回放（内部分析） | N/A | — | `src/commands/thinkback/`、`src/commands/thinkback-play/` | N/A（内部工具） |
| **沙箱运行时（本地 + 远程）** | | | | | |
| NA-48 | `/sandbox-toggle` 及 `@anthropic-ai/sandbox-runtime` 集成（macOS seatbelt / Linux namespaces） | N/A | — | `src/commands/sandbox-toggle/sandbox-toggle.tsx:1`、`src/utils/sandbox/sandbox-adapter.ts:2` | N/A（ccgo 不捆绑 sandbox-runtime；该能力 T3 后期再评估） |

小计：48 项（全部 N/A）
