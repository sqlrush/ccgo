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

当前进度：

- 本轮补充：tool executor 会围绕 `PreToolUse`、`PostToolUse`、`PermissionDenied` 和 `PermissionRequest` hook 发出 `hook_started`、`hook_completed`、`hook_failed`、`hook_blocked` 进度事件，携带 phase/tool/hook_index 以及阻断、错误、权限行为和 input 更新摘要；`PermissionAsk` 现在走独立 `PermissionRequest` phase 并发出 `permission_requested` 进度，conversation runner 已通过现有 tool progress 通道透出这些事件。settings command/HTTP hook 主路径已接入：支持 matcher/`if` 过滤、JSON stdin/body、stdout/HTTP body JSON `hookSpecificOutput.updatedInput`、exit 2/block、HTTP URL allowlist、HTTP header env allowlist 插值和 `PermissionRequest` allow/deny。plugin/prompt/agent hook、async/background hook 和完整 hook telemetry 仍未宣称完成。

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

当前进度：

- Read/Edit/Write 初版已落地，覆盖文本 Read、PDF text/page-selection 初版（含常见 Page/Contents 间接对象、Pages/Kids 页序、FlateDecode 文本流和 UTF-16 BOM 字符串）、PNG/JPEG/GIF/WebP image Read、Jupyter notebook cell 渲染初版、Read 大文本 tool-result budget 截断/落盘、read-before-write、mtime stale guard、唯一匹配、`replace_all`、Write/Edit structured diff hunks、`.claude/settings.json`/`settings.local.json` 写前 JSON/语义校验、team-memory secret guard、Read 去重和跨 tool round read-state。
- NotebookEdit 初版已落地，按官方 `notebook_path`/`cell_id`/`new_source`/`cell_type`/`edit_mode` schema 支持 replace/insert/delete 主路径、真实 cell id 和 `cell-N` 索引、code cell 修改后清空 outputs/execution_count、read-before-edit/stale guard、read-state 刷新、`notebook_path` 权限路径识别、结构化结果和 cell-level diff/hunks；完整 notebook UI/file-history/golden parity 仍需继续补。
- Bash 初版已落地，覆盖 command/timeout/description 输入校验、`/bin/sh -c` 执行、stdout/stderr/exit code/timeout 结构化结果、动态 read-only/concurrency-safe/destructive 分类、Git diff/log/show/status/ls-files/grep/rev-parse/branch/tag/ls-remote safe-flag 校验，Git remote/push/reflog/stash/worktree/merge-base/describe/cat-file/for-each-ref/rev-list/blame/shortlog/config-get 参数级安全分类、`git remote show/get-url` 参数收紧、`git ls-remote` URL/SSH/server-option guard、branch/tag 裸 positional 创建防护、`git reflog expire/delete`、`git stash drop/pop/clear` 和 `git worktree remove/prune` 破坏性分类、`find -delete/-exec rm` 与 `xargs rm` 破坏性分类、safe wrapper/env 前缀归一化（`time`/`nohup`/`timeout`/`nice`/`stdbuf`/`env`）后的只读/破坏性分类、临时环境赋值前缀后的破坏性命令识别、权限规则接入、后台启动、同会话 `BashOutput` 输出读取和 `KillBash` 取消；完整 shell parser、真实 sandbox、interrupt、后台任务完整生命周期和官方 golden 仍需继续补。
- Glob/Grep 纯 Go 初版已落地，覆盖 `**` 递归 glob、Glob 绝对 pattern base-dir 提取、Glob 官方 pattern/path-only strict schema、Glob/Grep 输出工作目录相对路径、Glob 默认 no-ignore/hidden 搜索及 `CLAUDE_CODE_GLOB_NO_IGNORE`/`CLAUDE_CODE_GLOB_HIDDEN` env 切换、Grep 官方 VCS metadata 目录排除（`.git`/`.svn`/`.hg`/`.bzr`/`.jj`/`.sl`）、Grep 层级 `.gitignore`/`.ignore`、Grep `hidden`/`--hidden` 和 `no_hidden`/`--no-hidden` 隐藏文件遍历控制、Glob oldest-first modified/path 排序、Glob 截断 tool-result 提示、Grep regex/fixed string (`fixed_strings`/`-F`)、multiline 跨行 dotall 搜索、glob/iglob/type/type-not 过滤、Grep glob 空白/逗号多 pattern 与 brace alternation、Glob/Grep path 存在性校验和 Glob directory-only path 校验、`output_mode`/`outputMode` 的 `files`/`files_with_matches`/`content`/`count` 输出模式、Grep files/--files 不读内容列出将被搜索文件、Grep files_with_matches file-count summary、Grep count-mode occurrence/file summary、Grep `--max-columns 500` 长匹配/上下文行省略占位、`context`/`before_context`/`after_context` 及 `-C`/`-B`/`-A` 上下文行和官方 precedence（非 content 模式忽略）、`line_numbers`/`lineNumbers`/`-n` line-number 控制、`byte_offset`/`--byte-offset`/`-b`、`path_separator`/`--path-separator`、`null`/`--null`/`-0`、`field_match_separator`/`--field-match-separator`、`field_context_separator`/`--field-context-separator`、`context_separator`/`--context-separator` 和 `no_context_separator`/`--no-context-separator` 输出分隔符控制、`max_count`/`maxCount`/`-m` per-file match limiting、`offset`/`head_limit` 分页和 content-mode pagination tool-result 提示、默认 250 条 Grep head limit、`head_limit=0` unlimited、`ignore_case`/`case_insensitive`/`caseInsensitive`/`-i` 大小写不敏感搜索，以及 Grep 数字/布尔参数的 quoted semantic string 兼容；完整 ripgrep parity 和剩余输出参数仍需继续补。
- TodoWrite 初版已落地，覆盖完整 todo list 写入、状态/优先级校验、重复 id 拒绝、单个 `in_progress` 约束、结构化结果、tool metadata 状态保存和 session-scoped 本地持久化/恢复；TUI 同步和官方 golden 仍需继续补。
- WebFetch 初版已落地，覆盖 URL/timeout/max_bytes 输入校验、HTTP GET、HEAD preflight、metadata/raw `skipWebFetchPreflight` skip-preflight、二进制 preflight 跳过 GET、文本/二进制判定、截断、非 2xx error 标记、结构化结果、HTML-to-text rendering、prompt-focused excerpt、prompt phrase scoring/metadata 和 `WebFetch(domain:...)` 权限规则适配；browser 渲染、完整 prompt-aware summarization 和官方 golden 仍需继续补。
- WebSearch HTML/JSON 搜索适配初版已落地，覆盖 query/max_results/timeout/domain filters 输入校验、可注入搜索 endpoint、DuckDuckGo HTML 链接解析、DuckDuckGo subdomain redirect unwrap、常见 JSON result shapes、DuckDuckGo result snippet 抽取、domain allow/block 过滤、结构化结果和 query 权限规则匹配；官方搜索后端、ranking parity 和 golden 仍需继续补。
- PowerShell 初版已落地，覆盖 command/timeout/description/run_in_background 输入校验、`pwsh`/`powershell` 前台执行、后台启动、`PowerShellOutput` 输出读取、`KillPowerShell` 取消、stdout/stderr/exit code/timeout/cancel 结构化结果、动态 read-only/concurrency-safe/destructive 分类、常见 mutating alias canonicalization、文件读取类命令的基础相对路径 guard、path-free `git`/`git.exe`/`git.cmd` 等外部 Git 命令复用 Bash Git safety 分类、Docker `ps`/`images`/`logs`/`inspect` 只读外部命令分类和变量/未知 flag guard、只读 PowerShell cmdlet safe-flag allowlist 和路径参数 guard、数据转换/对象检查/系统信息类 cmdlet 只读 allowlist、pipeline-tail 格式化/对象选择类 cmdlet 只读 allowlist 和变量/hashtable/scriptblock guard、网络/事件/CIM 元数据类 cmdlet 只读 allowlist 和远程/XML/hashtable 风险参数排除、native/external 原生命令只读 allowlist（`ipconfig`/`netstat`/`systeminfo`/`tasklist`/`where.exe`/`hostname`/`whoami`/`route print`/`file`/`findstr`/`dotnet` 等）和写操作形态拒绝、前台/后台输出 tool-result 截断/落盘测试覆盖、缺失可执行文件结构化错误、默认工具注册和基本跨平台进程配置；完整 parser、完整权限/path validation、后台生命周期 edge cases、官方前台截断 golden、session 记录和官方 golden 仍需继续补。
- 本轮补充：WebFetch HEAD preflight 现在记录 `Content-Disposition`，并会通过 attachment filename 的常见二进制扩展名（如 PDF/image/archive/office/media）跳过 GET，覆盖服务端缺失 `Content-Type` 但通过下载文件名暴露类型的二进制响应。
- 本轮补充：WebFetch HTML-to-text rendering 现在会保留 anchor `href` 作为链接上下文，并把 `img` 的 `alt`/`title`/`aria-label` 和 `src` 渲染成可见图片说明；prompt-focused excerpt 可以命中图片说明文本，同时避免重复 URL 链接文本和 `javascript:` href。
- 本轮补充：WebFetch GET 会记录 redirect 后的 `final_url`，HTML rendering 会按 final URL 解析相对 anchor/image URL，确保重定向页面中的相对链接和图片说明指向浏览器实际可见的目标地址。
- 本轮补充：WebFetch 现在按官方 cross-host redirect 语义处理跨 host 跳转，HEAD preflight 和 GET 都不会自动触达新 host，而是返回包含 original URL、redirect URL 和 status 的 redirect notice；同 host redirect 仍继续跟随并保留 `final_url`。
- 本轮补充：WebFetch input schema 现在与官方对齐，`url` 和 `prompt` 都是必填字段；既有本地扩展 `timeout`、`max_bytes`/`maxBytes` 仍保持可选。
- 本轮补充：WebFetch 文本 body 现在会按 BOM、`Content-Type` charset 或 HTML `<meta charset>`/`http-equiv` charset 解码常见网页编码，包括 UTF-8/UTF-16LE/UTF-16BE、Latin-1 和 Windows-1252，并在 structured content 暴露归一化 `charset`。
- 本轮补充：WebSearch JSON parser 现在会递归解包 `web`、`response`、`search`、`hits`、`documents`、`records`、`entries` 等常见后端 wrapper，保留 URL 去重和 domain filter。
- 本轮补充：WebSearch JSON result parser 现在支持 `pageUrl`/`targetUrl`/`source_url`/`formattedUrl` 等 URL aliases、`htmlTitle`/`htmlSnippet` 等 HTML 标记字段清理、嵌套 URL object，以及 `deepLinks`/`siteLinks` 子结果递归解析。
- 本轮补充：WebSearch JSON parser 现在继续覆盖 `answerBox`/`answer_box`、`knowledgeGraph`/`knowledge_graph`、`news`/`news_results`、`topStories`、`peopleAlsoAsk` 和 `related_questions` 等常见搜索后端 wrapper，并识别 `website`/`sourceLink` URL alias、`question` title alias、`answer`/`excerpt` snippet alias。
- 本轮补充：WebSearch `query` schema 现在按官方 `min(2)` 约束拒绝单字符查询，通用 tool schema validator 同步支持 `minLength`，让工具定义可直接表达字符串最小长度契约。
- 本轮补充：WebSearch domain filters 现在在 schema 层声明 array `items:string`，通用 tool schema validator 同步支持 `items` 校验；`allowed_domains`/`blocked_domains` 会拒绝空字符串、URL/port、非法 wildcard 和非域名 label。
- 本轮补充：通用 tool schema validator 现在支持 `enum`，可直接执行 Grep output mode、NotebookEdit edit mode/cell type、Todo status/priority、Task target/action、LSP severity 等工具 schema 的枚举契约。
- 本轮补充：通用 tool schema validator 现在支持数字 `minimum`/`maximum`，可直接执行 LSPDiagnostics `limit` 等工具 schema 的数值范围契约。
- 本轮补充：通用 tool schema validator 现在支持 `required` 的 Go `[]string` 形态和 object `additionalProperties` schema 校验，MCP `get_prompt.arguments` 会在 schema 层拒绝非字符串参数值。
- 本轮补充：通用 tool schema validator 现在支持 `const`、`pattern`、`maxLength`、`minItems`/`maxItems`、`minProperties`/`maxProperties`、`exclusiveMinimum`/`exclusiveMaximum` 以及 `allOf`/`anyOf`/`oneOf`，并兼容 Go 代码直接构造的 typed schema list，外部 MCP 工具 schema 的基础 JSON Schema 约束会在本地调用前执行。
- 本轮补充：`FuncTool.Validate` 现在使用与模型 tool definition 同源的动态 `InputSchemaFunc`，本地执行前校验会应用 Task subagent enum 等 runtime metadata 驱动的 schema 约束，避免“模型看到的 schema”和“实际执行校验”分叉。
- 本轮补充：通用 tool schema validator 继续补齐 `not`、`multipleOf`、`uniqueItems`、`prefixItems`/`items:false`、`contains`/`minContains`/`maxContains`、`patternProperties` 和 `dependentRequired`；`additionalProperties:false` 会正确把 pattern-matched 字段视为已定义，Go typed nested schema map 也会被执行。
- 本轮补充：通用 tool schema validator 现在支持条件类 JSON Schema 约束 `propertyNames`、`dependentSchemas` 和 `if`/`then`/`else`，外部 MCP/动态工具可用 schema 表达字段名规则、属性依赖和条件必填逻辑。
- 本轮补充：WebFetch/WebSearch 输入解码现在兼容 `timeout`、`max_bytes`/`maxBytes`、`max_results`/`maxResults` 的 quoted semantic string 数值；WebSearch 也会按官方校验拒绝同一请求同时设置 `allowed_domains` 和 `blocked_domains`。
- 本轮补充：Grep 现在支持 whole-word 搜索参数 `word_regexp`/`wordRegexp`/`word-regexp`/`-w`，在 regex 和 fixed-string 模式下按词边界过滤匹配，并兼容 quoted boolean 输入。
- 本轮补充：Grep 现在支持反向匹配参数 `invert_match`/`invertMatch`/`invert-match`/`-v`，`files_with_matches`、`content`、`count` 和 multiline 模式都会按非匹配行/未覆盖行输出，并兼容 quoted boolean 输入。
- 本轮补充：Grep content 输出现在支持 `only_matching`/`onlyMatching`/`only-matching`/`-o`，只输出匹配片段而不是整行，并在 structured matches 中暴露片段 column；`-o` 同样接受 quoted boolean。
- 本轮补充：Grep count 输出现在支持 `count_matches`/`countMatches`/`count-matches`/`--count-matches`，需要时按匹配片段次数计数；默认 count 继续保持匹配行计数，`countMatches` 同样接受 quoted boolean。
- 本轮补充：Grep 长行省略阈值现在支持 `max_columns`/`maxColumns`/`max-columns`/`--max-columns`，默认保持 500，传 `0` 可关闭省略，quoted semantic number 同样兼容。
- 本轮补充：Grep content 输出现在支持 `no_line_number`/`noLineNumber`/`no_line_numbers`/`noLineNumbers`/`no-line-number`/`--no-line-number`/`-N`，可用 ripgrep 风格显式关闭默认行号输出；quoted semantic boolean 同样兼容，且 no-line-number 优先于 line-number 开关。
- 本轮补充：Grep content 输出现在支持 `column`/`column_numbers`/`columnNumbers`/`column-number`/`--column`，显式开启时匹配行会输出 `path:line:column:text` 并在 structured matches 中暴露首个匹配列号，context 行保持原格式；quoted semantic boolean 同样兼容。
- 本轮补充：Grep 文件列表输出现在支持 `files_without_match`/`filesWithoutMatch`/`files-without-match`/`--files-without-match`/`-L`，也接受 `output_mode` 的 `files_without_match(es)`，用于列出不含匹配的文件并兼容 quoted boolean。
- 本轮补充：Grep 文件列表输出现在显式支持 `files_with_match(es)`/`filesWithMatch(es)`/`files-with-match(es)`/`--files-with-match(es)`/`-l`，并接受 `output_mode` 的 `files_with_match` alias，统一归一为 `files_with_matches`。
- 本轮补充：Grep 文件列表输出现在支持 ripgrep 风格 `files`/`--files` 和 `output_mode:"files"`，不要求 `pattern` 且不读取文件内容，会列出通过 ignore、hidden、glob/iglob、type/type-not 和 binary/text 过滤后将被搜索的文件。
- 本轮补充：Grep 路径过滤现在支持 ripgrep 风格 `--glob`/`-g` 和 `--type`/`-t` aliases，执行和 structured content 都统一使用归一化后的 glob/type 过滤值。
- 本轮补充：Grep 路径过滤现在支持 ripgrep 风格 `type_not`/`typeNot`/`type-not`/`--type-not`/`-T`，可在 `type` 限定后排除指定文件类型，structured content 会回传归一化后的排除类型。
- 本轮补充：Grep 路径过滤现在支持 ripgrep 风格 `iglob`/`--iglob` 大小写不敏感 glob，和 `glob` 共享正向/`!` 排除规则集合并语义，structured content 会回传归一化后的 `iglob` 过滤值。
- 本轮补充：Grep 路径过滤现在支持 ripgrep 风格 `glob_case_insensitive`/`globCaseInsensitive`/`glob-case-insensitive`/`--glob-case-insensitive` 及 no-override aliases，让普通 `glob`/`--glob`/`-g` 按大小写不敏感方式匹配。
- 本轮补充：Grep `glob`/`--glob`/`-g` 路径过滤现在支持 ripgrep 风格 `!pattern` 排除规则，可与正向 glob、逗号/空白多 pattern 和 brace alternation 组合；只有排除规则时默认包含未被排除的路径。
- 本轮补充：Grep 搜索现在支持 ripgrep 风格 `text`/`--text`/`-a`，可显式把二进制扩展名文件按文本读取参与匹配，structured content 会回传 `text` 状态；`-a` 同样兼容 quoted semantic boolean。
- 本轮补充：Grep content 输出现在支持 ripgrep 风格 `passthru`/`passthrough`/`--passthru`/`--passthrough`，可输出被搜索文件的全部行并保留匹配行标记；启用后按官方语义覆盖 context 行数，quoted semantic boolean 同样兼容。
- 本轮补充：Grep content 输出现在支持 ripgrep 风格 `trim`/`--trim` 和 `no_trim`/`--no-trim`，会删除每条已打印文本行开头的 ASCII 空白，同时保留原始匹配列号；quoted semantic boolean 同样兼容。
- 本轮补充：Grep 长行输出现在支持 ripgrep 风格 `max_columns_preview`/`--max-columns-preview` 和 `no_max_columns_preview`/`--no-max-columns-preview`，会在 `max_columns` 触发时输出截断预览加官方 omitted-end 后缀；quoted semantic boolean 同样兼容。
- 本轮补充：Grep 搜索现在支持 ripgrep 风格 `line_regexp`/`line-regexp`/`--line-regexp`/`-x`，将 pattern 限定为整行匹配，并按官方语义优先于 `word_regexp`；fixed-string、multiline 和 quoted semantic boolean 组合均兼容。
- 本轮补充：Grep content 输出现在支持 ripgrep 风格 `vimgrep`/`--vimgrep`，匹配行按每个匹配重复输出 `path:line:column:text`，context 行保持单行输出，并兼容 `-N`、`only_matching` 和 quoted semantic boolean。
- 本轮补充：Grep content/count 输出现在支持 ripgrep 风格 `with_filename`/`--with-filename`/`-H` 和 `no_filename`/`--no-filename`/`-I`，可控制匹配行和计数输出的文件名前缀；文件列表模式仍保留路径输出。
- 本轮补充：Grep content 输出现在支持 ripgrep 风格 `replace`/`--replace`/`-r` 显示替换，匹配行按 replacement 展示、context 行保持原文，`only_matching` 和 `vimgrep` 输出也保留替换语义。
- 本轮补充：Grep count 输出现在支持 ripgrep 风格 `include_zero`/`--include-zero`，普通 line count 和 `count_matches` occurrence count 都会输出零命中文件，并兼容 `--no-filename`。
- 本轮补充：Grep content 输出现在支持 ripgrep 风格 `heading`/`--heading` 和 `no_heading`/`--no-heading`，可按文件分组输出 heading，组内行去掉文件名前缀，并兼容 context、`--no-filename` 和 quoted boolean；`vimgrep`/count 模式会按官方语义忽略 heading。
- 本轮补充：Grep 带路径的输出现在支持 ripgrep 风格 `path_separator`/`--path-separator`，可替换 content/count/file-list/heading 可见路径中的 `/`，并按官方约束拒绝多字节分隔符。
- 本轮补充：Grep 带路径的输出现在支持 ripgrep 风格 `null`/`--null`/`-0`，文件列表路径使用 NUL 终止，content/count/heading 输出把路径字段后的分隔符替换为 NUL；`--no-filename` 时不会额外产生 NUL。
- 本轮补充：Grep content 输出现在支持 ripgrep 风格 `field_match_separator`/`fieldMatchSeparator`/`field-match-separator`/`--field-match-separator` 和 `field_context_separator`/`fieldContextSeparator`/`field-context-separator`/`--field-context-separator`，匹配行与上下文行可分别使用自定义字段分隔符，并兼容空字符串分隔符与 `--null` 路径字段分隔。
- 本轮补充：Grep content 上下文输出现在支持 ripgrep 风格 `context_separator`/`contextSeparator`/`context-separator`/`--context-separator` 和 `no_context_separator`/`noContextSeparator`/`no-context-separator`/`--no-context-separator`，会在同文件不连续上下文块和非 heading 跨文件块之间插入默认 `--` 或自定义分隔行，heading 模式仅在同一文件内部插入。
- 本轮补充：Grep content 输出现在支持 ripgrep 风格 `byte_offset`/`byteOffset`/`byte-offset`/`--byte-offset`/`-b`，普通匹配和 context 行输出行起始 byte offset，`only_matching`/`vimgrep` 输出匹配起始 byte offset，并兼容 `--column`、自定义字段分隔符和 `--null` 路径字段分隔。
- 本轮补充：Grep 遍历现在支持 ripgrep 风格 `hidden`/`--hidden` 和 `no_hidden`/`noHidden`/`no-hidden`/`--no-hidden`，可显式包含或排除隐藏文件/目录；`--no-ignore` 仍不会覆盖 `--no-hidden`，VCS metadata 目录继续固定排除。
- 本轮补充：Grep 常用布尔参数继续补齐 ripgrep 长参数 aliases，覆盖 `--line-number`、`--ignore-case`、`--fixed-strings`、`--word-regexp`、`--invert-match` 和 `--only-matching`，并兼容 quoted semantic boolean。
- 本轮补充：Grep multiline 搜索现在支持 ripgrep 风格 `-U`、`--multiline`、`multiline-dotall` 和 `--multiline-dotall` aliases，统一映射到既有跨行 dotall 匹配逻辑并兼容 quoted semantic boolean。
- 本轮补充：Grep 搜索现在支持 `no_ignore`/`noIgnore`/`no-ignore`/`--no-ignore`，可跳过 `.gitignore`/`.ignore` 规则，同时继续排除 VCS metadata 目录并保留 Read deny 额外 ignore 保护；`--no-ignore` 兼容 quoted boolean。
- 本轮补充：Grep 的 `files_with_matches` 输出现在按官方行为使用文件修改时间倒序排序，mtime 相同再按路径排序；分页和 `head_limit` 会在排序后应用。
- 本轮补充：Grep 结果排序现在支持 ripgrep 风格 `sort`/`--sort` 和 `sortr`/`--sortr` 参数，覆盖 `path`/`modified`/`none` 及常见别名；显式排序会作用于 files/content/count 输出，structured content 会回传实际 sort、reverse 和 explicit 状态。
- 本轮补充：Grep 结果排序现在支持 ripgrep deprecated `sort_files`/`sortFiles`/`sort-files`/`--sort-files` aliases，统一映射到显式 `sort=path`，并兼容 quoted semantic boolean。
- 本轮补充：Grep 遍历现在支持 ripgrep 风格 `max_depth`/`maxDepth`/`max-depth`/`--max-depth`/`-d`，按 `--max-depth 0` 目录 no-op、`1` 仅直属文件、`2` 下一层文件的语义限制递归，并兼容 quoted number。
- 本轮补充：Glob/Grep 搜索遍历现在会读取 permission context 中的 `Read(...)` deny 规则，并把对应 basename/path/directory pattern 作为额外 ignore rule，避免被禁止读取的文件出现在搜索结果中。
- 本轮补充：Bash `grep`/`rg` read-only 分类现在会把 pattern-file 参数 `-f FILE`、`-fFILE` 和 `--file=FILE` 当作路径读取处理，缺值、绝对路径和 `..` 路径不再进入 read-only fast path。
- 本轮补充：Bash/PowerShell read-only 分类会先校验 tokenizer 视角的语法完整性，未闭合 quote 或末尾 escape/line-continuation 不再进入只读 fast path。
- 本轮补充：Bash/PowerShell tokenizer 现在按 single-quoted literal 处理单引号内的 escape 字符，Bash 的 `\` 和 PowerShell 的 backtick 在单引号内不再导致错误的 quote/segment/token 状态。
- 本轮补充：Bash/PowerShell 分类现在把未引用 newline 作为命令分隔符，并支持未引用 `#` 行注释剥离；注释文本不再污染分类，下一行命令仍保留 read-only/destructive 判断。
- 本轮补充：Bash destructive 分类把未引用单个 `&` 后台分隔符纳入命令分段，后台后续命令会独立参与 destructive 判断。
- 本轮补充：Bash/BashOutput 输入解码现在兼容 `timeout`、`run_in_background`/`runInBackground`、`tail_lines`/`tailLines` 的 quoted semantic string 形式，官方 SDK 风格的 `"1000"`/`"true"` 不再被严格 JSON 类型拒绝。
- 本轮补充：PowerShell/PowerShellOutput 输入解码现在同样兼容 `timeout`、`run_in_background`/`runInBackground`、`tail_lines`/`tailLines` 的 quoted semantic string 形式，和官方 PowerShell tool 的 semantic number/boolean schema 对齐。
- 本轮补充：BashOutput/PowerShellOutput structured content 现在会回传实际应用的 `tail_lines`，未请求时为 `0`，让后台输出截断状态可被 UI/golden 明确审计。
- 本轮补充：Read/Edit 输入解码现在兼容 `offset`/`limit` 和 `replace_all` 的 quoted semantic string 形式；whole-decimal 数字字符串会按官方 `semanticNumber(...int())` 语义归一为整数，fractional 数字继续拒绝。
- 本轮补充：Bash/PowerShell 输入校验现在会阻断前台首语句长 `sleep`/`Start-Sleep`（2 秒及以上）并提示改用 `run_in_background`；短 sleep、浮点 sleep、PowerShell `-Milliseconds` 和显式后台执行保持允许。
- 本轮补充：Bash/PowerShell 输入解码现在接受官方 `dangerouslyDisableSandbox` semantic boolean 字段并在 structured content 中记录请求；真实 sandbox adapter/override 执行语义仍未宣称完成。
- 本轮补充：Bash/PowerShell 的 `dangerouslyDisableSandbox` 会传入权限请求，普通/default/auto/plan/acceptEdits 模式要求确认，`dontAsk` 拒绝，只有可用的 `bypassPermissions` 模式会放行；完整 sandbox adapter 仍未宣称完成。
- 本轮补充：settings `sandbox.allowUnsandboxedCommands` 会合并到 permission context 并约束 `dangerouslyDisableSandbox`；为 `false` 时 sandbox override 直接拒绝，settings validation 会校验相关 sandbox 布尔字段类型。
- 本轮补充：settings `sandbox.filesystem.allowWrite`/`denyWrite`/`denyRead`/`allowRead` 会合并到 permission context 并参与路径权限判定；`denyRead` 可被更窄的 `allowRead` 覆盖，`denyWrite` 会阻断写入，`allowWrite` 在危险根路径和敏感路径安全检查之后放行额外写目录，同时修正 request cwd-relative path 展开顺序。
- 本轮补充：permission internal path context 新增 `SkillDirs`，Runner 可把 skill/bundled-skill 目录传入 tool metadata，让 `SKILL.md` 及其资源文件作为内部路径只读访问；该 allowlist 不允许写入，完整 skill discovery/activation/SkillTool 仍未宣称完成。
- 本轮补充：`internal/skills` 现在提供项目 skill discovery 基础能力，按 `.claude/skills/<skill>/SKILL.md` 目录格式从 cwd 向 git root/home 收集 skill roots，并可按文件路径发现 cwd 以下嵌套 skills；Runner 会把工作目录发现到的项目 skill roots 自动加入工具只读权限上下文。
- 本轮补充：Read/Write/Edit/NotebookEdit 文件工具会在成功处理文件路径时触发嵌套 skill discovery，把新发现的 skill roots 追加到共享 tool metadata 的内部只读路径上下文；后续工具可读取对应 skill 文件和资源，但完整 skill activation/SkillTool/UI 仍未宣称完成。
- 本轮补充：`internal/skills` 现在可加载目录式 `<skill>/SKILL.md` 并生成 prompt command 元数据，覆盖 description/frontmatter fallback、allowed-tools、argument-hint、arguments、when_to_use/when-to-use、version、model（含 inherit 归一为无覆盖）、context: fork、agent、effort、paths、content length、user-invocable hidden 状态和 disable-model-invocation；项目 skill commands 可按 discovery 顺序导出，slash command 注册、SkillTool 调用和 UI 激活仍未宣称完成。
- 本轮补充：新增 `internal/commands` registry 基础层，按官方 command 来源顺序合并 bundled/builtin-plugin/project-skill/workflow/plugin/dynamic/builtin metadata，并提供 dynamic 去重、display-name/alias 查找、hidden 过滤、SkillTool/slash-skill 过滤和 bridge-safe 判定；实际 local/local-jsx 执行、`/help`/`/skills` UI、plugin/MCP/workflow 加载仍未宣称完成。
- 本轮补充：command registry 现在保存本地 skill prompt template，并提供 prompt expansion 入口，覆盖 `$ARGUMENTS`、indexed/shorthand 参数、frontmatter named arguments、`${CLAUDE_SESSION_ID}` 替换、非 MCP skill 的 `${CLAUDE_SKILL_DIR}` 替换和 meta user message 输出；shell injection、SkillTool wrapper、local/local-jsx 执行和 REPL/UI wiring 仍未宣称完成。
- 本轮补充：新增基础 `Skill` tool wrapper 并注册到默认内置工具集，可调用本地项目 prompt skill、兼容官方 `skill`/`args` 与相邻别名，并把 prompt expansion 产生的 meta user message 通过 `ToolResult.NewMessages` 交给 conversation runner；runner 现在会把这些新消息写入 transcript 并追加到下一轮模型请求。forked/remote/MCP/plugin skills、shell injection、slash/local command UI wiring 仍未宣称完成。
- 本轮补充：新增基础 slash command parser/executor，支持官方 `/command args` 和 `/mcp:tool (MCP) args` 解析；conversation runner 现在会在请求模型前展开本地项目 prompt skill slash command，生成 command metadata user message 和 meta prompt message，保留 transcript parent chain，并支持 skill `model` 覆盖本轮请求。local/local-jsx 目前只返回未实现输出且不会误发模型，command permissions attachment、forked/MCP/plugin/bundled slash command 和 UI wiring 仍未宣称完成。
- 本轮补充：本地 prompt skill slash command 和 `Skill` tool 现在都会生成 `command_permissions` attachment，解析 `allowed-tools` 的 comma/space 分隔形式并保留括号内 tool pattern；Runner 会在当前 turn 内把 attachment 转成 `PermissionSourceCommand` allow rules 合并进 engine permission decider，让 skill 授权的后续工具调用可在同一轮通过。完整权限 UI/SDK 展示、forked/MCP/plugin/bundled skill 权限继承仍未宣称完成。
- 本轮补充：skill frontmatter 标量兼容继续补齐，`allowed_tools`/`argument_hint`/`disable_model_invocation`/`user_invocable`/`when-to-use` 等相邻字段会映射到 canonical command metadata；`model: inherit` 不再误触发模型覆盖，`context: fork`、`agent`、`effort` 会保留在 command contract 中，为后续 forked skill/agent 执行接线提供 metadata。
- 本轮补充：project legacy `.claude/commands/**/*.md` 现在会加载为 `commands_DEPRECATED` prompt command，覆盖普通 markdown 命名空间、目录式 `SKILL.md` 命名空间、frontmatter metadata、SkillTool 可见性过滤和 prompt expansion；目录式 legacy command 保留 base directory 前缀和 `${CLAUDE_SKILL_DIR}` 替换。完整 user/managed commands、plugin commands、local/local-jsx 执行仍未宣称完成。
- 本轮补充：现有 Go 内置 slash command metadata 继续贴近官方源快照，补齐 `config`/`resume`/`clear` 的 aliases（`settings`、`continue`、`reset`、`new`），以及 `mcp`/`resume`/`model` 的 argument hint、`mcp`/`status`/`model` 的 immediate 标记和部分官方描述；大量内置 command 的真实 local/local-jsx UI 执行仍未宣称完成。
- 本轮补充：slash command 现在有基础 local command result 抽象，`/clear` 不再落入 unsupported 分支，会生成 local text result、保留 command metadata message，并且不会请求模型；完整 REPL conversation reset、local command text/compact/skip 全语义、`/cost`/`/status`/`/compact` 和 local-jsx UI 执行仍未宣称完成。
- 本轮补充：Bash destructive 分类会递归检查未 single-quoted 的 `$()`、backtick 和 subshell `(...)` 内容，嵌套破坏性命令会触发 destructive 标记。
- 本轮补充：PowerShell destructive 分类会递归检查未 single-quoted 的括号表达式、`$()` 子表达式和 scriptblock `{...}`，嵌套 `Remove-Item`/mutating cmdlet 不再只停留在 not-read-only 状态。
- 本轮补充：Bash 文件读取/搜索类 read-only 命令增加基础相对路径 guard，绝对路径、home、父目录、变量路径、Windows drive、UNC 和 URI/provider-like 路径不再自动允许。
- 本轮补充：Bash `rg` read-only 分类现在拒绝 `--pre`/`--pre=...` 外部预处理命令，避免 ripgrep 调用任意预处理器时仍进入只读快路径。
- 本轮补充：Bash `go list` read-only 分类从只看子命令收敛到参数级 allowlist，允许常见查询参数和 `-mod=readonly/vendor`，拒绝 `-mod=mod`、`-modfile`、`-overlay`、未知 flag、缺值 flag 以及非本地 package pattern。
- 本轮补充：Bash `find` read-only 分类现在拒绝 `-delete`、`-fprint`、`-fprint0`、`-fprintf` 和 `-fls` 等删除/写文件 action，避免同一命令既被标成 destructive 又进入只读快路径。
- 本轮补充：Bash safety 分类将 `find -exec*` 形态排除出 read-only fast path，并补 `find`/`xargs` 经 `sh`/`bash`/`zsh`/`dash`/`ksh -c` 包装的嵌套破坏性脚本检测；`find -exec*` 和 `xargs` 现在还会复用 safe wrapper/env/assignment 归一化，覆盖 `env rm`、`timeout rm`、`xargs -I{} env sh -c ...` 等形态。
- 本轮补充：PowerShell native/external 文件读取/搜索命令（`where.exe`/`file`/`tree`/`findstr`）现在也对路径型 positional 和 path flag 使用相对路径 guard，拒绝 Windows drive、UNC、URI/provider-like、父目录和缺值 path flag；`where.exe` 从 allow-all flag 收敛到 `/R`/`/Q`/`/F`/`/T`。

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

- 已落地 JSONL/session 基础、resume/search/title 支撑、prompt history lock/buffered flush/field aliases、official subagent transcript layout、agent metadata sidecar/field aliases、sidechain runtime start/append/finish/cancel/fail 和 parent-chain append/finish 初版、sidechain manager orchestration 初版、sidechain manifest 聚合、sidechain state/list/resume/content-field aliases 初版、sidechain resume context builder、sidechain conversation/content-replacement reconstruction、transcript tail/window/metadata/index loaders、byte-budget transcript tail loader、agent-scoped content replacement metadata/record field-alias loading、session-scoped metadata reappend including AI-title/last-prompt/task-summary、transcript line offset index/window/byte-budget-window/parent-chain/resume/tail/byte-budget-tail loaders、extended transcript metadata entries/type/field aliases、transcript message/session UUID field aliases including top-level `messageUuid`/`messageId`/`id` record IDs and `role`/`entry_type`/`messageType`/`createdAt` timestamp aliases、transcript tombstone metadata delete/relink、transcript resume conversation builder、index 文本预览和 AI-title/last-prompt/task metadata 字段、流式 transcript 搜索、session list pagination、remote history token refresh、remote history 全量分页抓取/page-field/event-list/records/entries/eventList/sessionEvents/last-id/cursor/event-id/has-next alias/wrapped-data/links/paging/bare-array/connection/eventConnection/sessionEventsConnection wrapper response/edge-cursor fallback/max-pages 截断状态/before_id 续抓、remote event transcript materialization/fallback field fill/去重追加/duplicate parent guard、remote history 一步 sync 到 transcript、CLAUDE.md/memdir 扫描、team-memory secret guard、compact runner/boundary plan、conversation auto-compact、失败熔断、microcompact/cache 初版、persistent cached microcompact 初版、cache digest structural/rich-content metadata 覆盖、cache version/TTL/prune、in-memory micro cache prune、memory-cache write-through 到磁盘、atomic cache write、坏缓存默认 fail-open、session memory summary/frontmatter aliases/recall 初版、model-ranked recall session-id selection 和 invalid-selection fallback 初版、recall agent alternate/camel response keys/fenced-prose JSON extraction/scalar id parsing/nested/wrapped/collection-alias selection parsing、resume context model-assisted recall 接入、session memory rollup/prune compaction、rollup archive exclusion/merge、rune-safe rollup truncation、resume context + session memory recall、conversation recall 注入、deterministic/model-backed memory extraction 初版、extraction agent fenced-prose JSON/wrapped facts/provider-style response wrapper/alternate/structured field/nested source object/nested response/fact kind alias parsing 和 turn-end memory extraction 落盘。
- 本轮补充：sidechain agent metadata sidecar 读取接受 `type`/`subagentType`/`agentName`/`name`、`workspaceRoot`/`workspacePath`/`workspace`/`path`/`directory`、`taskDescription`/`operationName`/`commandName`/`displayTitle`/`prompt`/`input`/`command`/`title` 等字段别名，支持 JSON:API/resource-style wrapper，并避免把 `resource.type` 误当成 agent 类型，避免历史或第三方 subagent sidecar 在 resume/list 时丢失 agent 类型、worktree 路径或任务描述。
- 本轮补充：sidechain/subagent lifecycle content 读取会递归解包 `payload`/`data`/`body`/`result`/`response`/`metadata` 等 wrapper，嵌套的 subagent ID、status/outcome、summary、agent type、workspace path 和 task description 都可参与 state/list/resume 恢复。
- 本轮补充：sidechain/subagent lifecycle content 读取现在也接受 JSON:API/resource-style 的 `resource`/`attributes`/`properties` wrapper；外层 resource `id` 可作为 sidechain ID fallback，内层 metadata、status/outcome 和 summary 仍会参与 state/list/resume 恢复。
- 本轮补充：sidechain/subagent lifecycle content 读取现在也递归解包 GraphQL/JSON:API 风格的 `edge`/`node`/`attrs` wrapper，wrapped start/summary event 可继续恢复 ID、status、summary 和 agent metadata。
- 本轮补充：sidechain/subagent lifecycle 字段提取现在也会穿透 `edges`/`nodes`/`included` 等 collection wrapper 和数组元素，GraphQL connection 或 JSON:API included 风格的 start/summary payload 可继续恢复 state/list/resume 所需字段。
- 本轮补充：sidechain/subagent lifecycle 的 ID 等 string-like 字段接受 JSON number 和 Go 数字标量，numeric subagent ID 会保留为字符串并可用于 state/list/resume 查找。
- 本轮补充：sidechain/subagent lifecycle subtype 现在接受 `subagent_started`、`agentStarted`、`task_failed`、`sidechainCompleted` 等相邻事件名，并从 `taskID`/`workerId`/`runId`、`agentName`/`kind`、`resultText`/`finalMessage` 等字段恢复 state/list/resume 信息；`*_failed`/`*_cancelled` subtype 无显式 status 时会归一到 failed/cancelled。
- 本轮补充：sidechain/subagent lifecycle status 归一化现在接受 compact/camel aliases，包括 `inProgress`、`completedSuccessfully`、`cancelledByUser`/`canceledByUser`、`failedError`/`failedWithError` 和 `timedOut`，并继续写回 canonical running/completed/cancelled/failed 状态。
- 本轮补充：transcript metadata loader 为 file-history snapshot 和 attribution snapshot 建立 `messageId` 索引，并接受 `message_id`/`messageUuid`/`id` 等字段别名，保留 raw list 的同时支持按消息恢复 snapshot。
- 本轮补充：transcript message/index/session list 读取 `gitBranch`，兼容 `git_branch`/`branch` 别名，并让 session search 可以按分支名命中，贴近官方 lite metadata 的 branch 展示和检索行为。
- 本轮补充：full transcript `TitleFromTranscript` 的标题优先级和 indexed/lite 路径对齐，按 custom title、AI title、首个用户 prompt、last-prompt、summary 顺序兜底。
- 本轮补充：lightweight transcript index 的 `content-replacement` 计数按请求的 session id 过滤，避免 session list/search 摘要被同文件其它 session 污染。
- 本轮补充：transcript message/index/session list 读取 `cwd` 作为 project path，兼容 `projectPath`/`project_path`/`workingDirectory`/`working_directory` 等别名，并让 session search 可以按项目路径命中，贴近官方 lite metadata 的 projectPath 恢复行为。
- 本轮补充：TranscriptMessage 结构化读取官方 SerializedMessage 元数据 `userType`、`entrypoint`、`version`、`slug`，并兼容 user/entrypoint/version/slug 的 snake/camel/旧字段别名，减少旧 transcript 只能靠 raw JSON 保留元数据的情况。
- 本轮补充：model-backed session memory recall prompt 现在显式写入 requested limit 和 excluded current session id，减少模型返回超量或当前 session 后再 fallback 的概率。
- 本轮补充：remote history connection/pageInfo 解析接受 `hasPrevious`/`hasPreviousPage`、`hasOlder`/`more` 继续分页标记，以及 `previousCursor`/`prevCursor`/`beforeCursor`/`olderCursor` before-id cursor 别名，覆盖 GraphQL 向更旧事件翻页的响应形态。
- 本轮补充：remote history pagination bool 字段除 JSON bool 和 `true`/`false` 字符串外，也接受 `1`/`0`、`yes`/`no`、`on`/`off` 等数值/字符串布尔形态，以及 whole-number JSON number 或数字字符串如 `1.0`/`"0.0"`，避免 wrapper/pageInfo 中的非严格布尔值中断分页。
- 本轮补充：remote history pagination cursor/id 字段现在接受 JSON number 并原样转成字符串，覆盖 `next_cursor` 等 page 字段和 `edges[].cursor` 的数字形态。
- 本轮补充：remote history pagination 现在接受 `nextPageToken`/`nextToken`/`pageToken`/`continuationToken` 及 snake_case 形式，响应字段和 link URL query 参数都会归一到续抓 before-id。
- 本轮补充：remote history pagination 现在也接受通用 `paginationToken`、`cursorToken` 和 `token` continuation aliases，覆盖响应字段、link object 和 link URL query 参数。
- 本轮补充：remote history pagination 现在也接受 `previousPageToken`/`prevPageToken`/`olderPageToken`、`previousToken`/`prevToken`/`olderToken` 及 snake_case 形式，响应字段、link object 和 link URL query 参数都会归一到续抓 before-id。
- 本轮补充：remote history pagination 现在也接受相邻 before-cursor aliases，包括 `before`、`beforeID`、`olderThan`、`endingBefore` 和 `untilId`，响应字段、link object 和 link URL query 参数都会归一到续抓 before-id。
- 本轮补充：remote history pagination 现在也接受 OData next-link 字段 `@odata.nextLink`、`odata.nextLink` 和 `__next`，并从 `$skiptoken`/`skipToken` link query 参数提取续抓 cursor。
- 本轮补充：remote history 普通事件数组现在也接受 JSON:API/resource-style 元素，事件 payload 可放在 `attributes` 或 `properties` 里，并使用外层 resource `id` 作为 SDK event ID fallback。
- 本轮补充：remote history fetch 把 HTTP 204 和 200 空 body 视为空的终止页，避免空历史响应被标成 incomplete 或触发 JSON EOF。
- 本轮补充：remote history fetch 现在把 HTTP 404/410 missing/deleted session response 视为空的终止页；其它非 OK 响应仍保持 nil page/incomplete，避免把临时服务错误误报为完整空历史。
- 本轮补充：contract `ID` JSON 读取现在接受 JSON number/null，remote history event/message/session/parent ID alias 可继承数字 ID 兼容面并在 transcript materialization 中保留为字符串。
- 本轮补充：remote history response parser 会递归解包 `data.session.events`、`data.projectSession.eventConnection`、`data.viewer.session.events`、`data.node.eventConnection`、`conversation`、`remoteHistory`、`_embedded` 等 GraphQL/session/HAL wrapper，继续复用 `nodes`/`edges[].node` 和 `pageInfo` pagination 解析。
- 本轮补充：remote history event-list 接受 `value`/`values`/`resources`/`collection` 别名，connection edge 也接受 `resource`/`value` 作为 node payload，覆盖 OData/HAL/resource collection 风格响应。
- 本轮补充：remote history response parser 现在也递归解包页级 JSON:API/resource `attributes`/`properties` wrapper，event-list 接受 `list`/`object`/`objects` aliases，并能把单个 `data.attributes` resource event 作为一条 SDK event 恢复。
- 本轮补充：remote history response parser 现在也递归解包 JSON:API `relationships` wrapper，并接受 `children`、`resultsConnection`/`results_connection` 和 `childrenConnection`/`children_connection` 事件集合别名，relationship 内的 `pageInfo` pagination 仍可驱动续抓。
- 本轮补充：remote history 在 JSON:API `relationships.events.data` 只有 resource identifier 时，会继续使用 top-level `included` 中的真实事件 resource，避免把 `{type,id}` 标识符误当作空事件遮蔽完整 payload。
- 本轮补充：remote history 顶层事件数组现在会展开混合 JSON:API `data` 资源中的 session relationship event page，合并嵌套 pagination，并跳过 tool/task 等明确非事件资源，避免空 SDK event 污染历史。
- 本轮补充：remote history response parser 现在也接受 JSON:API `included` collection，会过滤非事件资源，并递归解包 `resource`/`attributes`/`properties` 后保留外层 resource id 作为事件 ID fallback。
- 本轮补充：remote history response parser 会解包 `payload`/`response`/`result`/`body` 等通用响应外壳，外壳内的 event list、pagination、links 会继续递归解析。
- 本轮补充：remote history response parser 现在也接受 provider-style `choices`/`outputs`/`candidates`/`generations` wrapper 以及顶层 `message`/`content`/`text` envelope，可从 `message.content`、content-block array 和 `content.parts[].text` 中恢复 event page JSON（包括 fenced `json` code block），并保留 pagination 继续驱动 `before_id` 续抓。
- 本轮补充：remote history provider wrapper 的 fenced JSON 提取现在接受 inline/glued fence 形态，例如语言标记后同一行直接跟 JSON 对象，模型输出不换行时仍能恢复 event page。
- 本轮补充：remote history pagination 现在接受 `starting_after`/`startingAfter`/`after*` cursor aliases，page 字段和 link URL query 都可驱动下一页 `before_id` 续抓。
- 本轮补充：remote history `SDKEvent` 本体接受 `eventType`/`event_type`/`role` 类型别名、`createdAt`/`created_at` 时间戳别名，以及 `payload`/`data`/`body`/`metadata`/`meta`/`attributes`/`properties`/`serializedMessage` message payload 别名；payload 只有 `role`/`content` 时也能 materialize 成 transcript message。
- 本轮补充：sidechain lifecycle 读取现在接受运行时相邻字段别名，包括 `jobId`/`threadId`/`workflowId`/`operationId`/`requestId`、`workerType`/`taskType`、`workspaceRoot`/`projectPath`、`instructions`/`operationName`/`commandName`/`displayTitle`、`jobStatus`、`resultState`、`outputText` 和毫秒级 start/end time 字段，并会递归解包 `runtime`/`context`/`state` 容器。
- 本轮补充：sidechain lifecycle start/end time 现在接受 `startTimestamp`/`startTimestampMs`/`startedAtUnix` 以及 `endTimestamp`/`completedTimestamp`/`completedAtUnixMs` 等相邻时间别名，恢复第三方/旧 runtime transcript 时不再只依赖 `startedAt`/`endedAt`。
- 本轮补充：sidechain lifecycle 和 metadata sidecar 的 task description/summary 读取现在接受 visible text payload，包括 text content block、message object、content block array、provider-style `message.content`/`parts`/`outputText` wrapper，并继续跳过 thinking/tool/image 等非可见块。
- 本轮补充：transcript resume 的嵌套 content block 接受 `toolUseId`/`toolUseID`、`isError`、`cacheControl`、`cacheReference` 字段别名，并保留 cache edit 的 `cacheReference`。
- 本轮补充：transcript resume 的 nested content block `id`/`tool_use_id`/`toolUseId` 现在接受 JSON number，并保留为字符串 tool-use ID。
- 本轮补充：嵌套 contract message 的 `content` 接受字符串、单个 content-block 对象，以及混合字符串/content-block 数组；字符串会归一为 text block，并接受 `text`/`body`/`message`/`value`/`output` 正文字段和 `role`/`messageType` 类型别名，提升 transcript/remote history payload 恢复率。
- 本轮补充：lightweight transcript metadata loader 在 `system`/`compact_boundary` 后清空旧 `marble-origami-commit`/`marble-origami-snapshot` 状态，和 full loader/官方 sessionStorage compact-boundary 语义一致。
- 本轮补充：transcript metadata loader 接受 `sessionID`/`session` 作为 session-scoped metadata ID 别名，并容忍 `prNumber`、`timeSavedMs`、`lastSpawnTokens` 等计数字段使用数字字符串。
- 本轮补充：transcript metadata ID helper 现在复用 contract `ID` JSON 解码，`messageID`/`sessionID` 等 metadata ID 字段可接受 JSON number 并保留为字符串。
- 本轮补充：context-collapse commit metadata 的 collapse/summary/archived ID 字段现在也走 metadata ID helper，支持 JSON number ID 并保持 full/lightweight loader 一致。
- 本轮补充：content-replacement metadata 的 `agentId`、record `toolUseId` 和 `blockId` 现在也接受 JSON number，并在 full/lightweight loader 中保留为字符串 ID。
- 本轮补充：context-collapse snapshot metadata 接受 `isArmed`/`enabled` bool 别名、`spawnTokens`/`tokenCount` 计数字段别名，以及 `stagedMessages`/`items` staged payload wrapper，full loader 和 metadata loader 保持一致。
- 本轮补充：transcript metadata 读取现在会先解包 JSON:API/resource、GraphQL edge/node、`included` 以及 collection/list/values wrapper，再做 metadata type 分类；full loader、lightweight metadata loader 和 transcript index 都可恢复 wrapped title/task/tag/worktree/content-replacement/context-collapse 条目。
- 本轮补充：transcript metadata type 现在接受 compact/camel aliases，例如 `aiTitle`、`lastPrompt`、`taskSummary`、`contentReplacement`、`fileHistorySnapshot`、`speculationAccept` 和 `contextCollapseSnapshot`；full loader、lightweight metadata loader 和 transcript index 使用同一归一化。
- 本轮补充：transcript metadata 字段查找现在也接受大小写、snake_case、kebab-case 和空格分隔形式归一，`Session-ID`、`Custom-Title`、`Pull-Request-Number` 等相邻字段可在 full loader、lightweight metadata 和 transcript index 中恢复同一 metadata。
- 本轮补充：transcript message/envelope 和嵌套 contract message 字段查找现在也复用大小写、snake_case、kebab-case 和空格分隔形式归一，`Message-Type`、`Message ID`、`Parent-Message-ID`、`Session-ID`、`Git-Branch`、`Message Text` 等相邻字段可贯通 full loader、progress bridge、line index 和 indexed resume。
- 本轮补充：legacy session JSONL 的 `SessionEntry` 读取现在也复用同一 normalized 字段查找，`Entry Type`、`Message-ID`、`Parent Message ID`、`Session-ID`、`Created At` 等字段可经 `session.Load` 恢复旧 entry 与嵌套 message。
- 本轮补充：remote-history `SDKEvent` 读取现在也复用 normalized 字段查找，`Event Type`、`Event-ID`、`Parent Message ID`、`Created At`、`Message-Payload`、`Status Message`/`Failure Reason`/`Final Output` 等字段可恢复事件类型、ID、parent、时间戳、状态/错误/结果和 transcript materialization 所需 message。
- 本轮补充：transcript message 和嵌套 contract message 接受顶层 `sessionID` 作为 session id 别名，`LoadTranscript`、`LoadTranscriptIndex` 和 indexed resume 会保留该 session id（覆盖测试：`TestLoadTranscriptAcceptsSessionIDUpperAlias`）。
- 本轮补充：嵌套 contract message 接受 `parentUUID`、`parentId`/`parentID`/`parent_id`、`parentMessageId`/`parentMessageID`/`parent_message_id` 和 parent-message UUID 别名，transcript/remote history payload 自带 parent alias 时不会丢失嵌套 parent。
- 本轮补充：indexed resume chain 现在区分 byte budget 截断掉的 parent 和 transcript 里真实缺失的 parent，bounded resume 会分别暴露 `TruncatedParent` 与 `MissingParent`。
- 本轮补充：contract message、session entry 和 transcript loader 现在会把 message-type aliases 归一化为 canonical 类型，包括 `assistant_message`、`userMessage`、`system-event`、`attachmentMessage`、`progress_update` 和 `tombstone_event`；full loader、line index 和 indexed resume 统一使用 canonical user/assistant/system/attachment，并保留 progress bridge 语义。
- 本轮补充：tail、byte-tail、window 和 metadata-only transcript loader 也改用 canonical message type 处理 progress bridge 与 compact-boundary，`progress_update` 和 `system-event` 等别名不再只在 full loader 路径生效。
- 本轮补充：tail、byte-tail、window 和 streaming transcript search 现在复用 full/index loader 的 wrapped record 展开逻辑，可从 JSON:API/resource、GraphQL edge/node 和 collection/list wrapper 中恢复 transcript 批次，并保留 progress bridge。
- 本轮补充：TUI Vim prompt editing 增加基础 visual/visual-line 模式，支持 `v`/`V` 进入选择、motion 扩展 selection、visual `o` 切换 active end、visual `<`/`>` 行缩进/反缩进、visual `~` 大小写切换、visual `u`/`U` 小写/大写转换、`y`/`d`/`c` 以及常用 visual `x`/`s` aliases 作用于选择范围、Esc 回到 normal，并让 interaction script 可用 `visual`/`visualLine` 断言当前 mode。
- 本轮补充：TUI Vim prompt editing 增加 prompt-local `H`/`M`/`L` screen-line motions，normal/visual/operator 路径都可用，`H`/`L` 支持 count 定位首/末附近行，operator line-motion 可 dot-repeat。
- 本轮补充：TUI Vim prompt editing 支持 normal-mode `gv` 重新进入上一次 characterwise/linewise visual selection，后续 visual operator 会复用恢复出的选择范围。
- 本轮补充：TUI Vim prompt editing 增加 `gu`/`gU`/`g~` case-conversion operator，复用 motion、linewise、find/till、text-object 和 dot-repeat operator 管线，并保持大小写转换不写入 yank register。
- 本轮补充：TUI Vim prompt editing 增加 normal-mode `gJ` raw line join，不插入/规范化空格，并接入 dot-repeat。
- 本轮补充：TUI Vim prompt editing 增加 visual/visual-line `J`/`gJ` 行拼接，支持选择范围内的 whitespace-normalized join 和 raw join，并沿用 undo、`gv` selection 记忆和 dot-repeat change 记录。
- 本轮补充：TUI Vim prompt editing 增加 visual/visual-line `p`/`P` 粘贴替换 selection，支持 characterwise 和 linewise register，替换出的文本会回写 unnamed register，并避免行选择替换到末尾时留下额外空行。
- 本轮补充：TUI Vim prompt editing 增加 visual/visual-line `r{char}` selection 替换，按选区把非换行字符替换为目标字符，保留行结构、接入 undo，并让 `gv` 能重选替换前的 visual range。
- 本轮补充：TUI Vim prompt editing 增加 normal-mode `R` replace mode，输入会从当前 cursor 开始覆盖现有字符、超过文本尾部时追加，并接入 undo 与 dot-repeat。
- 本轮补充：TUI Vim prompt editing 增加 prompt-local marks，支持 `m{mark}` 设置位置、`` `{mark}` 精确跳转、`'{mark}` 跳到 mark 所在行首，并支持 `d`/`c`/`y` 等 operator 以 mark 作为 motion。
- 本轮补充：TUI Vim prompt editing 增加基础 macro 录制和回放，支持 `q{reg}` 开始录制、normal-mode `q` 停止、`@{reg}` 按 count 回放，以及 `@@` 重放上一 macro。
- 本轮补充：TUI Vim prompt editing 增加 prompt-local `/` 和 `?` 搜索模式，支持 Enter 执行、Esc 取消、Backspace 编辑查询、wraparound 匹配，以及 `n`/`N` 重复上一搜索方向或反向搜索。
- 本轮补充：TUI Vim prompt editing 将 `/` 和 `?` 搜索接入 operator motion，支持 `d/search`、`c?search`、搜索 count、取消清理 pending 状态，以及 search operator 的 dot-repeat 记录。
- 本轮补充：TUI Vim prompt editing 将 `/`、`?`、`n` 和 `N` 接入 visual/visual-line 模式，搜索会临时进入 search prompt、Enter 后恢复 selection 并移动 active end，Esc 取消时保留原 visual selection。
- 本轮补充：TUI Vim prompt editing 增加 named register 前缀，支持 normal/operator/visual 路径里的 `"{reg}` yank/delete/paste、uppercase register append、black-hole register no-op，以及普通移动命令后清理未使用的 register selection。
- 本轮补充：TUI Vim prompt editing 的 normal-mode `x`/`X` 现在会把删除字符写入 unnamed 或 selected named register，并保持 `.` dot-repeat 删除路径继续可用。
- 本轮补充：TUI Vim prompt editing 现在支持 visual/visual-line `Y`/`D`/`C` linewise aliases，字符 visual 选区也会按所在整行 yank/delete/change，并保持 unnamed/named register 的 linewise 内容一致。
- 本轮补充：prompt history 写入现在保留 image pasted-content 的 media type、filename、dimensions 和 image-cache source path 元数据，同时继续不把 inline base64 image bytes 或 text-paste hash 写进图片历史记录。
- 本轮补充：prompt history 读取旧 image pasted-content 记录时，如果缺少 source path 但对应 session 的 image-cache 文件仍存在，会自动补回 source path 并刷新内存 image path cache。
- 本轮补充：interaction script key 字段现在接受 DOM-style key event object，可从 `key`/`code`（包括 `Numpad*`、扩展数字区括号/hash/backspace 和标点 key code）、旧式 `keyIdentifier`、数字 `keyCode`/`which`/`charCode`（包括标点和数字区运算符）、`keypress.which` 字符码、`ctrlKey`/`altKey`/`metaKey`/`shiftKey` 和 `modifiers` 数组还原现有 key 名，wrapper payload 中的 key event 也可驱动脚本回放。
- 本轮补充：interaction script mouse payload 现在可从 `mouseup`、`pointerUp`、`touchend` 等 event type 推导 release 状态；显式 release bool 仍优先生效。
- 本轮补充：interaction script mouse payload 现在可从 `wheel`/`mousewheel`、`scrollUp`/`scrollDown`、`direction`、`deltaY` 和旧式 `wheelDelta` 推导 SGR wheel button，录制的 DOM/compact 滚轮事件可直接驱动 viewport 滚动。
- 本轮补充：interaction script mouse payload 现在接受 DOM `which` 和 `buttons`/`buttonState` bitmask，并映射到 SGR left/middle/right button，避免录制脚本的右键/中键被误当成 primary click。
- 本轮补充：interaction script mouse payload 现在会把 DOM `mousemove`/`pointermove` 的 buttonless motion 映射成 SGR `35`，把带 `buttons`/`which` 的 move/drag 映射成 SGR motion button，避免 hover/move 录制事件误触发 dialog/viewport primary click。
- 本轮补充：interaction script touch payload 现在可从 `touches`/`targetTouches`/`changedTouches` 的首个 touch point 恢复坐标，`touchmove` 映射为 SGR drag motion，`touchcancel` 映射为 release，减少 DOM touch 录制 fixture 的手工改写。
- 本轮补充：REPL dialog 鼠标处理现在忽略 SGR motion/drag button，只响应实际 press/click，避免 pointer/touch move 回放关闭 permission/task dialog。
- 本轮补充：interaction script paste payload 现在接受 DOM `clipboardData`/`dataTransfer` 对象，可从 `text/plain`、`plainText` 和 `items[].text` 恢复 pasted text，减少 ClipboardEvent 录制 fixture 的手工改写。
- 本轮补充：interaction script paste payload 现在也接受 ClipboardItem 风格的 `items[].getAsString`/`get_as_string` 和相邻 `stringData`/`textData` 文本字段，录制脚本不需要先把 clipboard item 扁平化成 `text`。
- 本轮补充：interaction script paste payload 现在也可从 DOM `clipboardData.items` 与 `dataTransfer.files` 中恢复 `image/*` file item 为 image paste，并避免把 image file 的 `data`/`base64` 内容误当成普通 pasted text。
- 本轮补充：interaction script clipboard/dataTransfer image paste 现在优先读取 `items[].file`、`items[].blob`、`items[].getAsFile` 等嵌套 file payload，保留真实 filename、media type、base64 和 source path。
- 本轮补充：interaction script resize payload 现在接受 DOM/window 尺寸别名 `innerWidth`/`innerHeight`、`clientWidth`/`clientHeight`、`offsetWidth`/`offsetHeight` 和 ResizeObserver 风格 `contentRect`/`target` wrapper。
- 本轮补充：interaction script resize payload 现在接受 ResizeObserver `contentBoxSize`/`borderBoxSize` 数组里的 `inlineSize`/`blockSize` 字段，覆盖现代浏览器 box-size 事件形态。
- 本轮补充：嵌套 contract message 接受 `messageId`/`messageID`/`message_id` 和 `messageUuid`/`messageUUID`/`message_uuid` 作为自身 ID/UUID 别名，indexed resume 会保留 payload 自带的 nested message id。
- 本轮补充：嵌套 contract message 的 primary `id` 现在接受 JSON number，`LoadTranscript` 和 indexed resume 会保留为字符串 message id。
- 本轮补充：基础 `SessionEntry` JSONL loader 接受 `role`/`entryType`/`messageType`、message ID/UUID、parent ID/UUID 和 `sessionID`/`session`/session UUID 别名，旧 entry 文件可通过 `session.Load` 保留类型、parent 和 session。
- 本轮补充：tombstone metadata target/parent 加宽到 `targetId`/`deletedId`/`messageId` 和 `parentId`/`parentMessageId` ID/UUID 别名，delete/relink replay 可兼容旧字段拼写。
- 本轮补充：transcript metadata 加宽 summary `leafID`/message ID、content-replacement `agentID`/`toolUseID`/`blockID` 和 context-collapse `collapseID`/`summaryID`/archived ID 别名，full loader 和 lightweight metadata loader 保持一致。
- 本轮补充：worktree-state metadata 接受 `worktreeState`/`worktree_state`/`worktree`/`workspace` wrapper 别名，full loader 和 lightweight metadata loader 都能保留旧 worktree payload。
- 本轮补充：PR link metadata 接受 `pullRequestNumber`/`pull_request_number`、`pullRequestURL`/`pull_request_url` 和 `repoFullName`/`repositoryFullName` 别名，full loader 和 lightweight metadata loader 都能恢复旧 PR metadata。
- 本轮补充：task-summary metadata 接受 `taskSummary`/`task_summary`/`content`/`text` 摘要别名和 `createdAt`/`created_at` timestamp 别名，旧任务摘要记录不会只保留 session id。
- 本轮补充：summary/custom-title/ai-title/last-prompt metadata 接受 `content`/`text`/`title`/`name`/`prompt` 等值字段别名，full loader、metadata loader 和 transcript index 的标题/摘要恢复保持一致。
- 本轮补充：tag、agent-name、agent-color、agent-setting 和 mode metadata 接受 `label`/`name`/`color`/`setting`/`status` 等值字段别名，full loader、metadata loader 和 transcript index 的 agent/session 状态恢复保持一致。
- 本轮补充：content-replacement metadata 接受 `records`/`contentReplacements` 等 record wrapper，以及 record 内 `type`/`content`/`hash` 等字段别名，full loader、metadata loader 和 transcript index 的 replacement 恢复保持一致。
- 本轮补充：remote history `SDKEvent` 解码接受顶层 `sessionID` 作为事件 session id 别名，materialize transcript 时会填充 record 与嵌套 message 的 session id（覆盖测试：`TestRemoteHistoryTranscriptMessagesAcceptsSessionIDUpperAlias`）。
- 本轮补充：remote history `SDKEvent` 解码接受 `parentUUID`、`parentId`/`parentID`/`parent_id`、`parentMessageId`/`parentMessageID`/`parent_message_id` parent aliases，materialize transcript 时会保留 parent chain（覆盖测试：`TestRemoteHistoryTranscriptMessagesAcceptsParentIDAliases`）。
- 本轮补充：remote history `SDKEvent` 解码接受 `eventID`、`messageId`/`messageID`/`message_id` 和 `messageUuid`/`messageUUID`/`message_uuid` 作为事件 ID aliases；只有 `uuid` 时也会作为 event ID fallback，materialize transcript 时能稳定生成 message UUID 与 parent chain（覆盖测试：`TestRemoteHistoryTranscriptMessagesAcceptsEventMessageIDAliases`）。
- 本轮补充：remote history `SDKEvent` timestamp 解码接受 `created`/`createdTime`/`created_time`、`date_time`、`eventTime`/`event_time`、`occurredAt`/`occurred_at` 等相邻字段，并容忍 `timestamp`/`createdAt` 等时间字段为 JSON number，materialize transcript 时继续填充 record 与嵌套 message timestamp（覆盖测试：`TestSDKEventUnmarshalAcceptsTimestampAliases`、`TestRemoteHistoryTranscriptMessagesAcceptsEventTimestampAliases`）。
- 本轮补充：remote history `SDKEvent.message` 现在接受顶层字符串和 content-block 数组形态，会先包装成 message content 再解码，避免 provider 把 assistant/user event message 直接写成 `"message":"text"` 或 `message:[...]` 时 JSON 解码失败（覆盖测试：`TestSDKEventUnmarshalAcceptsScalarMessagePayload`、`TestRemoteHistoryTranscriptMessagesAcceptsScalarEventMessagePayload`）。
- 本轮补充：remote history `SDKEvent` 的 status/error/result 内容字段现在接受 `statusMessage`/`progress_message`、`stateMessage`/`updateText`/`messageText`、`errorMessage`/`failure_reason`、`failureMessage`/`exceptionMessage`/`diagnosticMessage`、`resultText`/`outputText`/`completionText`、`summaryText`/`finalOutput`/`responseText` 以及 result object aliases `output`/`response`/`value`/`completion`/`summary`/`final`，仅在对应 canonical event type 下补值，避免和 assistant/user message payload 混淆（覆盖测试：`TestSDKEventUnmarshalAcceptsStatusErrorResultAliases`、`TestRemoteHistoryEventsAcceptStatusErrorResultAliases`）。
- 本轮补充：memory 层补齐官方 `memoryAge`/freshness note 语义，`ReadDocumentsWithOptions` 可为超过 1 天的 memory 文档前缀 system-reminder，提示模型把 memory 当作 point-in-time observation 并核对当前代码。
- 本轮补充：Read tool 在 metadata 提供 auto-memory 目录时，会为读取旧 auto-memory 文件的 tool result 前缀 freshness system-reminder，和官方 FileReadTool 的 memory freshness prefix 对齐。
- 本轮补充：memory 层补齐官方 `relevant_memories` attachment 基础，包含 stable memory header、system-reminder 渲染、surfaced path/byte 扫描、按 200 行/4096 bytes 读取并附截断提示的 surfacing reader、mark-after-filter 的 duplicate memory attachment 过滤、最后非 meta user prompt/单词 prompt/60KB session cap 的 prefetch gating、多目录结果排除 read-state/surfaced 后取前 5 个候选的选择逻辑，以及 recent successful tools 窗口收集并排除 pending/failed/同名失败工具。
- 本轮补充：conversation `BuildRequest` 会把 history 里的 `relevant_memories` attachment 展开为 user/meta system-reminder 后再 NormalizeForAPI，补齐 official messages.ts attachment 渲染路径的基础 runtime 接线。
- 本轮补充：Runner 增加显式 `RelevantMemoryDir` runtime：配置后会扫描 memory dir、默认用 deterministic selector 选出相关 md memory，若配置 `MemoryAgentClient` 则优先用 model-backed selector，读取为 `relevant_memories` attachment 并注入 request；默认关闭。
- 本轮补充：Runner 会把 `RelevantMemoryDir` 放入 tool metadata 的 internal auto-memory path context，使 Read tool 的 stale-memory freshness prefix 和 permission internal-path policy 与同一配置对齐。
- 本轮补充：transcript resume fallback 转换 attachment message 时保留 raw attachment payload，恢复后的 `relevant_memories` attachment 仍可进入 conversation request 的 system-reminder 展开路径。
- 本轮补充：memory 层增加可取消 `PrefetchRelevantMemories` runtime，复用现有 gating/selector/surfacing 逻辑返回 plan、selection 和 attachments；conversation `RunTurn` 会在用户消息进入后启动 relevant memory prefetch，并在第一轮 model request 消费结果，预取文件系统错误 fail-open 且不阻断主请求。
- 本轮补充：relevant memory prefetch 接入 model-backed sideQuery selector：当 `MemoryAgentClient` 可用时，先向模型提供候选 memory manifest，请模型返回 `memory_paths`/`memoryPaths`/`filePath`/`matches`/嵌套 selection 等路径别名，按模型顺序读取附件；模型错误或无效路径会 fail-open 回落 deterministic selector。完整官方 prompt/telemetry parity 仍继续推进。
- 本轮补充：model-backed relevant memory selector prompt 现在包含 recent successful tools 和 already-surfaced memory paths 的有界上下文，模型侧选择与 deterministic prefilter 的 tool/surfaced 约束更一致。
- 本轮补充：session-memory recall agent 和 relevant-memory selector 现在递归解包 `data`/`payload`/`body`、JSON:API `resource`/`attributes`/`properties`/`attrs`、`included`，以及 GraphQL `viewer`/`edge`/`node`/`nodes`/`edges`、`collection`/`list`/`children`/`values` selection wrapper；带明确非 memory/session `type` 的 resource 不再用裸 `id` 污染选择顺序，API-shaped model response 中的 session IDs 和 memory paths 会按模型顺序保留。
- 本轮补充：session-memory recall agent 和 relevant-memory selector 现在也接受 provider-style `choices`/`outputs`/`candidates`/`generations` response wrapper 以及顶层 `message`/`content`/`text` envelope，可从 `message.content`、content-block array、`content.parts[].text` 和 fenced `json` code block 里递归恢复 JSON selection payload。
- 本轮补充：session-memory recall agent、relevant-memory selector 和 model-backed fact extraction 的 fenced JSON 提取现在接受 inline/glued fence 形态，模型输出语言标记后同一行直接跟 JSON 时仍能恢复 selection/facts。
- 本轮补充：session-memory recall agent 和 relevant-memory selector 的 query 解析现在接受 `user_query`/`userQuery`、`question`、`prompt`、`input`、`search`、`search_text`/`searchText` 等相邻别名，模型返回非 canonical query key 时仍能保留改写后的检索语义。
- 本轮补充：relevant-memory selector 现在接受 `uri`/`url`/`href`、`fileUri`/`fileUrl`、`sourcePath` 和 `documentPath` 等 memory path aliases，并能从 `file://` URI 或 API URL path basename 恢复候选 memory；顶层 `memories`/`matches`/`filesList` 集合在同时带 `query` 时也会参与 selection 解析，避免 query 快路径丢掉模型选择。
- 本轮补充：session-memory recall agent 现在接受 `summaries`、`selectedSummaries`、`relevantSummaries`、`candidateSummaries` 等 summary collection aliases，并会继续从嵌套 `summary.sessionId`/`summaryId` 恢复模型排序的 session IDs。
- 本轮补充：session-memory recall agent 现在也接受 `sessionUri`/`sessionUrl`、`uri`/`url`/`href` 等 summary link aliases，能从 `file://.../summary.md` 或 API URL path 中恢复 session ID 并按模型顺序匹配 summary。
- 本轮补充：session-memory recall agent 现在也接受 `sessionPath`/`session_path`、`sessionSummaryPath`、`summaryPath`/`summary_path`、`sessionFilePath` 和 `transcriptPath` selection aliases，模型/API 直接返回 summary 或 transcript/session JSONL 文件路径时可复用现有 path lookup 找回 session。
- 本轮补充：model-backed memory fact extraction 的正文解析现在接受 `fact`、`statement`、`insight`、`result`、`output` 等相邻 text aliases，模型不用 canonical `text`/`content`/`summary` 字段时也能恢复事实内容。
- 本轮补充：model-backed memory fact extraction 的 kind 归一化现在接受 `constraint`、`user_rule`、`guideline`、`standing_instruction`、`policy` 等 instruction-like aliases，并归入现有 preference 事实类型。
- 本轮补充：session-memory summary frontmatter 的 `updatedAt`/`createdAt` 时间现在接受 Unix 秒、Unix 毫秒和 `updatedAtMs`/`timestampMs` 等相邻字段别名，recall 排序不再只依赖 RFC3339 字符串。
- 本轮补充：session-memory summary frontmatter 的 session/message ID 现在接受 `sessionUUID`、`conversationId`、`threadId`、`transcriptId`、`messageID` 和 `leafID` 等相邻别名。
- 本轮补充：session-memory summary 正文现在在 markdown body 为空时接受 frontmatter `summaryText`、`summary`、`content`、`text`、`resultSummary`、`finalSummary` 等字段兜底，同时保持 body 优先。
- 本轮补充：model-backed memory fact extraction 现在也接受 provider-style `choices`/`outputs`/`candidates`/`generations` response wrapper 以及顶层 `message`/`content`/`text` envelope，可从 `message.content`、content-block array、`content.parts[].text` 和 fenced `json` code block 里递归恢复 JSON facts payload。
- 本轮补充：model-backed memory fact extraction 现在接受 `sourceMessageUUID`/`source_message_uuid`、`sourceEventId`/`source_event_id`、`originId` 和 `turn`/`event` source object 等来源别名，并把 numeric source ID 保留为字符串。
- 本轮补充：compact runner 的 summary 响应现在也接受 provider-style `choices`/`outputs`/`candidates`/`generations` wrapper，可在构建 compact plan 前从 `message.content`、content-block array、`content.parts[].text` 和 fenced `json` code block 中恢复 visible summary text。
- 本轮补充：remote history `SDKEvent` type 现在会把 provider-style aliases 归一化为现有 canonical 事件类型，包括 `assistant_message`/`assistant_delta`、`userMessage`/`humanMessage`、`system-event`、`result_event`/`finalResult`/`response.completed`、`errorEvent`/`failureEvent` 和 `status_update`/`statusMessage`/`progress`，single-object page 与 transcript materialization 不再因事件类型拼写相邻而丢消息。
- 本轮补充：conversation runner 会在用户消息入队后基于 compact window 计算 token warning state，并在达到 warning/error/auto-compact/blocking 阈值时发出 `token_warning` event；warning state 接入 blocking-limit env override，auto-compact 判断接入既有 `CLAUDE_AUTOCOMPACT_PCT_OVERRIDE`，避免 warning 和 compact 阈值来源分叉。
- 本轮补充：microcompact disk cache loader 现在读取 Go 默认、camelCase 和 snake_case 字段别名，string-like cache ID/version 字段可把 JSON number 原样保留成字符串，并容忍计数字段的数字字符串、whole-number JSON number 和 whole-number 数字字符串，同时继续拒绝 fractional count，以及 RFC3339、Unix 秒、Unix 毫秒时间字段，提升 cached microcompact 文件在不同实现/版本间的恢复率。
- 本轮补充：microcompact disk cache loader 现在接受 `cacheEntry`/`cache_entry`、`micro_compact`/`micro_compact_result` wrapper，以及 `summaryMarkdown`/`resultSummary`/`compressedText`、`cacheDigest`/`digestHash`/`fingerprint`、`summarizedCount`/`retainedCount`、`formatVersion` 和 `ttlMilliseconds`/`expiresInMilliseconds`/`maxAgeMilliseconds` 等相邻实现字段别名。
- 本轮补充：microcompact disk cache loader 继续接受相邻 timestamp/expiry aliases，包括 `cachedAt`、`cacheCreatedAt`、`storedAt`、`generatedAt`、`updatedAt`、`timestamp`、`expiry`、`expirationTime`、`validUntil`、`notAfter`，以及 `timeToLiveSeconds`、`validForMs` 等相对 TTL 字段。
- 本轮补充：microcompact disk cache loader 的相对 TTL 现在也接受分钟、小时和天级别名，例如 `ttlMinutes`、`expiresInHours`、`validForDays` 及 snake/camel 相邻形式，恢复 cached microcompact 过期时间时不再局限于秒/毫秒。
- 本轮补充：microcompact disk cache loader 的相对 TTL 字符串现在也接受固定单位 ISO-8601 duration，例如 `PT1H30M`、`P1D`、`P1DT2H`，并继续拒绝年/月这类不定长 duration。
- 本轮补充：microcompact disk cache loader 的字段和 wrapper 查找现在接受大小写、snake_case 和 kebab-case 相邻形式归一，例如 `cache-entry` 内的 `summary-text`、`cache-key`、`cache-version`、`created-at` 和 `ttl-seconds` 可恢复同一 cache entry。
- 本轮补充：microcompact disk cache loader 的 direct payload 判定也复用大小写、snake_case 和 kebab-case summary 别名归一，顶层 `summary-text` 搭配 `data`/`payload` sidecar 时仍会优先保留顶层摘要，不会被误判为 wrapper。
- 本轮补充：microcompact disk cache loader 和 prune 现在接受 digest 缺失但文件名已 keyed 的 cache entry，会用 `<digest>.json` 文件名作为 digest fallback，同时仍保留显式 digest mismatch 的 invalid-cache guard。
- 本轮补充：microcompact disk cache loader 的 `cached`/`fromCache`/`cacheHit`/`isCached` 布尔字段现在接受 JSON bool、`true`/`false`、`yes`/`no`、`on`/`off`、`1`/`0` 数字/字符串形态，以及 whole-number 数字字符串如 `"1.0"`/`"0.0"`。
- 本轮补充：microcompact disk cache loader 现在会从 `metadata`/`meta`/`cacheInfo`/`cacheDetails`/`cacheEntry`/`entry`/`record`/`cache` 等 sidecar object 中补缺失的 digest、version、cache-hit、timestamp、TTL 和 count aliases；主 summary payload 字段仍保持优先。
- 本轮补充：microcompact disk cache loader 现在也接受 JSON:API/resource-style `resource`/`attributes`/`properties` wrapper，summary payload 可放在 attributes/properties 内，外层 resource `id` 可作为 digest fallback。
- 本轮补充：microcompact disk cache loader 现在也递归解包 GraphQL-style `viewer`/`edge`/`node`/`attrs` wrapper，node `id` 可作为 digest fallback，attrs/properties 内的 summary、version、timestamp 和 TTL aliases 都会恢复。
- 本轮补充：microcompact disk cache loader 现在也会穿透 `edges`/`nodes`/`included` 等 collection wrapper 和数组元素，跳过无 summary 的非 cache resource，并恢复 GraphQL connection 或 JSON:API included 风格 cache entry。
- 本轮补充：microcompact disk cache loader 的 summary-like payload 现在接受 text content-block object、text content-block array 和 string array，会把可见 text block 合并为 summary，并会继续解包 text block 内嵌的 JSON/fenced summary payload，兼容官方/SDK 响应内容块形态的 cached microcompact 文件。
- 本轮补充：microcompact disk cache loader 的 summary array item 现在也复用 provider-style `parts`/`content.parts`/`output` 文本恢复路径，且 provider summary/wrapper 字段同样接受大小写、snake_case 和 kebab-case 相邻形式归一，批量候选或 provider cache item 不再因数组元素不是标准 text block 或字段拼写相邻而失效。
- 本轮补充：microcompact disk cache loader 的 summary-like payload 现在也接受完整 contract message object，并会递归解包 `message`/`assistantMessage`/`resultMessage`/`outputMessage`/`completionMessage` wrapper，从 message content 中恢复 visible text summary。
- 本轮补充：microcompact disk cache loader 的 summary array 元素现在也接受完整 contract message object，可把 message list 与 content-block 混合数组恢复成可见摘要文本。
- 本轮补充：microcompact disk cache loader 现在会把 `value` 字段中的 text content-block object 识别为 direct summary payload，同时继续从同一 `value` object 中补 digest、version、timestamp 等 cache metadata，避免 `value` 作为 summary/cache wrapper 双义字段时丢摘要或 sidecar 信息。
- 本轮补充：microcompact disk cache loader 现在也接受 provider-style `choices`/`outputs`/`candidates`/`generations`/`results` 等响应数组 wrapper，并可从 `message.content`、`output.content`、`content.parts[].text` 和 fenced `json` code block 中恢复 summary，同时保留外层 cache metadata。
- 本轮补充：microcompact disk cache loader 现在也会解包一行内 fenced JSON summary payload，例如 `json {...}` 或 `json{...}` 与 opening fence 在同一行的 provider/SDK 输出，不再把整段 code fence 当作可见摘要文本。
- 本轮补充：contract content block 与 microcompact cache loader 现在把 provider `output_text`/`outputText` content block 归一为可见 text，OpenAI Responses 风格 `output[].content[].type="output_text"` cache payload 可恢复 summary，而 reasoning/tool/image blocks 继续跳过。
- 本轮补充：microcompact disk cache loader 的 summary-like 字段现在也接受 `body`、`markdown`、`description`/`details`、`finalSummary`、`summaryContent`、`resultText`、`completionText`、`responseText`、`messageText` 等 direct/provider aliases，并在 nested cache entry、JSON:API/resource 和 provider-style response wrapper 中统一恢复可见摘要文本。

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
- 本轮补充：task runtime 在状态行、任务面板排序/渲染、批量取消和 scripted task expectation 前会把 `active`/`inProgress`/`in_progress`、`success`/`done`/`completedSuccessfully`、`error`/`failedWithError`、`canceled`/`cancelledByUser` 等 task state 别名归一为 canonical 状态。
- 本轮补充：scripted task runtime payload 和 task expectation 现在接受 `taskID`、`jobId`、`runId`、`label`、`displayName`/`displayTitle`、`operationName`/`commandName`、`phase`/`taskState`/`jobStatus`/`resultState`、`message`/`currentStep`/`statusMessage`/`outputText`/`resultText`、`percent`/`percentage`/`pct`/`completionPercent` 等相邻字段，并支持数字 task ID 与数字字符串 progress。
- 本轮补充：scripted task runtime 的 task payload、expectation 和 remove-task mutation 现在共享 `operationId`、`requestId`、`threadId`、`workflowId`、`toolUseId` 等相邻 task ID aliases，录制脚本可用 operation/request 风格 ID 追踪和移除任务。
- 本轮补充：permission runtime 会把 `Reject`/`deny`/`decline`/`disallow`/`no` 等 permission action 归一为 denied 结果，把 `Cancel`/`abort` 归一为 cancelled 结果，并让 scripted dialog-result status 断言接受 `rejected`/`approved` 等状态别名。
- 本轮补充：scripted permission payload、dialog expectation、event、cancel-permission 和 dialog-result expectation 现在接受 `ID`/`ToolName`/`Actions`、`permissionID`、`requestID`、`toolUseID`、`operationID`、`operation`、`commandName`、`resourcePath`、`body`、`reasonText`、`allowedActions`、`buttons` 等相邻字段，并支持数字 request ID。
- 本轮补充：configurable keybinding name parser 现在接受 macOS/DOM 风格 `cmd`/`command`/`super` modifier aliases，和现有 `meta`/`option` 一样映射到 prompt word motion、yank-pop、delete-word-back 与 modified left/right arrow key。
- 本轮补充：scripted runtime 的 permission/task payload 现在会递归解包 `value`/`payload`/`data`/`resource`/`attributes`/`properties`/`attrs`/`edge`/`node`、`included`/`collection`/`list`/`values` 等 JSON:API/GraphQL wrapper；resource/node 顶层 ID 会回填到内层 permission/task，带明确非 permission/task `type` 的 included resource 会被跳过，并避免仅因 wrapper 顶层 ID 生成半空 runtime 对象。
- 本轮补充：scripted runtime mutation 的 `removeTask`/`cancelPermission`/`cancelTasks` 现在也会从 wrapped `value`/`payload`/`data`/`resource`/`edge.node` 中递归读取 task/permission ID 和 cancellation detail，兼容 resource/action fixture 直接驱动 runtime mutation。
- 本轮补充：scripted runtime mutation 的直接 alias 字段现在也接受 object payload，例如 `removeTask: {resource:{id}}`、`cancelPermission: {edge:{node:{id}}}` 和 `cancelTasks: {resource:{attributes:{reasonText}}}` 不再因 string/bool 强类型字段提前解析失败。
- 本轮补充：scripted runtime action 的 boolean payload 现在同样递归解包 `resource`/`attributes`/`edge.node`/`attrs`，`cancelTasks`/`openTasks` 等动作可用 wrapped `enabled:false`/`open:false` 明确禁用，避免 wrapper object 默认 fallback 成 true。
- 本轮补充：scripted interaction 的直接 key alias 字段现在也接受 wrapped object，例如 `key: {resource:{attributes:{value}}}`、`keyPress: {resource:{attributes:{key}}}` 和 `keyPresses: {edge:{node:{attrs:{sequence}}}}` 不再因 string/list 强类型字段提前解析失败。
- 本轮补充：scripted interaction 的布尔字段现在接受非严格 bool payload，包括 `"true"`/`"false"`、`yes`/`no`、`on`/`off` 和数字 `1`/`0`；覆盖 mouse release、dialog visible/result、prompt empty、vim state、reverse-search，以及 focus/cancel/openTasks/expectNoEvent/expectFocused 等顶层 step 控制字段。
- 本轮补充：scripted interaction step 现在接受 `expect`/`expected`/`assertions`/`checks`/`verify`/`then`/`after` 等 expectation wrapper object，可把嵌套的 prompt/event/dialog/snapshot/screen/task/vim/viewport 断言映射到已有 `expect*` 字段。
- 本轮补充：scripted interaction expectation wrapper 现在也接受 assertion/check 数组；数组元素可用 `type`/`kind`/`name`/`target` 等 discriminator 加 `value`/`payload` 载荷声明 prompt/event/dialog/snapshot/screen/task/vim/viewport 断言，覆盖官方 fixture 常见的分列 checks 形态。
- 本轮补充：scripted interaction assertion/check 数组元素的载荷现在也接受 `resource`/`node`/`attributes`/`properties`/`result`/`response`/`output`，让 JSON:API/resource-style 断言体可直接映射到 prompt/event/dialog/snapshot/screen/task/vim/viewport expectation。
- 本轮补充：keybinding config、keymap 解析和 interaction script named-key 输入接受 `ctrl-h`/`ctrl-i`/`ctrl-j`/`ctrl-m`、`ctrl-[`、`ctrl-?`、对应 `control-*` 以及 compact/camel 形式；terminal parser 支持 CSI-u/kitty keyboard protocol 的 ctrl/alt/shift-enter/shift-tab 序列；image hint parser 支持 OSC ST terminator 和 base64 `name=` filename；keybinding JSON loader 支持 wrapper object-map、`shortcuts`、object action 字段、string-array key sequence/chord 和 `null`/`false` unbind；mouse parser 支持 legacy X10/normal tracking 序列；interaction script 支持结构化 mouse/mouse_event 步骤，button 接受 `buttonMask`/`button_mask`/`btn`/`code`/`mask`，坐标接受 `mouseX`/`mouse_x`/`clientX`/`screenX`/`pageX`/`offsetX`/`viewportX` 和对应 Y/row/line 别名，release 接受 `mouseUp`/`isRelease`/`mouseRelease`/`releaseEvent` 等字段别名；interaction script 还支持字符串 `keys` 和 `input`/`input_text`/`keys_text`/`raw_key`/`paste_text` 字段别名，并允许 status/snapshot/viewport/pasted-content contains 断言使用单字符串或字符串数组，`keybindings`、`expectEvents`、`expectDialogResults`、`expectPrompt.pastedContents` 和 `expectTasks.contains` 使用单对象或对象数组。
- 本轮补充：terminal input parser 现在把 modified SS3 function-key 序列（如 `ESC O 1;2P`、`ESC O 1;5Q`、`ESC O 1;16S`）归一为现有 F1-F4 key surface，补齐 xterm/kitty 兼容模式下的 F-key 输入形态。
- 本轮补充：terminal input parser 和 configurable keybinding name parser 现在接受 xterm 扩展功能键 F13-F20，包括 `ESC [25~`、`ESC [26~`、`ESC [28~`、`ESC [29~`、`ESC [31~` 到 `ESC [34~`，以及 `f13`/`function-key-20` 等配置名。
- 本轮补充：terminal input parser 现在也接受 modified SS3 application-cursor navigation 序列（如 `ESC O 1;2A`、`ESC O 1;5D`、`ESC O 1;16C`），和 CSI modified navigation 一样把 shift 降级为方向键、alt/meta 映射到 word-motion key、ctrl 组合映射到 ctrl word-motion key。
- 本轮补充：terminal input parser 现在把显式默认参数的 CSI navigation key 序列（如 `ESC [ 1 A`、`ESC [ 1 D`、`ESC [ 1 H`、`ESC [ 1 F`）归一为现有 arrow/Home/End key surface，同时继续让 `ESC [ 2 A` 这类 cursor-count 控制保持 unknown。
- 本轮补充：keybinding config 的 page/navigation/editing key name 现在接受 `pgup`/`pg-up`/`prior`/`pageUpKey`、`pgdn`/`pg-down`/`next`/`pageDownKey`、`homeKey`/`endKey`、`deleteForward`/`forwardDelete` 和 `deleteBackward`，覆盖常见终端、DOM 和配置别名。
- 本轮补充：keybinding config 和脚本 named-key 输入现在接受 DOM/fixture 风格的 `enterKey`/`returnKey`/`numpadEnter`、`escapeKey`/`escKey`、`tabKey`、`shiftEnterKey`/`shiftNumpadEnter`、`shiftTabKey`/`backtabKey`，减少配置导入时对基础控制键名称的手工归一。
- 本轮补充：keybinding config 和脚本 named-key 输入接受 DOM-style arrow key aliases，包括 `arrowLeft`/`arrowRight`/`arrowUp`/`arrowDown` 以及 ctrl/alt/meta/option arrow variants。
- 本轮补充：keybinding action parser 接受更多 editor/global-style action aliases，包括 `cursorLeft`/`cursorRight`、`previousWord`/`nextWord`、`lineStart`/`lineEnd`、`moveToBeginningOfLine`/`moveToEndOfLine`、`deletePreviousChar`/`deleteNextChar`、`backwardDelete`/`forwardDelete`、`deleteToStartOfLine`/`deleteToEndOfLine`、`killLine`、`pasteKillRing`/`yankPrevious`、`clearScreen`、`openExternalEditor`、`toggleTasks`、`cancelAgents`、`focusPrev`、`acceptSelection` 和 `search`。
- 本轮补充：keybinding config 和脚本 named-key 输入接受短修饰符别名，包括 `c-`/`m-`/`a-`/`opt-`/`s-` 以及 compact/camel 形式，可覆盖 control、meta、alt、option 和 shift key names。
- 本轮补充：keybinding config 和脚本 named-key 输入现在接受 `backtab`/`back-tab`/`btab` 等 Shift-Tab terminfo/fixture 别名，并映射到既有 focus-previous key surface。
- 本轮补充：keybinding JSON loader 现在递归解包 `data`/`payload`/`settings`/`config`/`keyboard`/`keymap` 等外层 wrapper，嵌套 preference export 中的 `bindings`/`shortcuts` 不需要手工扁平化。
- 本轮补充：keybinding JSON loader 现在也递归解包 JSON:API/resource-style `resource`/`attributes`/`properties`/`attrs` wrapper，API/preferences envelope 内的 `keybindings`/`keymap` 可直接加载。
- 本轮补充：keybinding JSON loader 现在把 `data`/`payload`/`body`/`result`/`response`、`resources`、`included`、`collection`/`list`/`children`/`values`、`nodes` 和 `items` 下的数组视为 binding list，数组元素也可直接使用 JSON:API/resource-style `resource`/`node`/`attributes`/`properties` wrapper。
- 本轮补充：keybinding JSON loader 现在也接受 GraphQL connection 风格的 `edges` binding list，binding item 可用 `edges[].node` 或 `edge.node` wrapper，外层可递归解包 `viewer`/`node`/`*Connection` wrapper。
- 本轮补充：keybinding JSON loader 现在接受 `keymap`/`keymaps`、`keyboardShortcuts`、`hotkeys`、`userKeybindings`、`customKeybindings` 等集合字段别名，并同时支持直接 object-map 和嵌套 `bindings` wrapper；单条 binding 的 key 字段也接受 `accelerator`、`keystroke`、`hotKey`、`keyCombo` 和 `keyChord` aliases。
- 本轮补充：keybinding JSON loader 现在也接受 provider-style `choices`/`outputs`/`candidates`/`generations` response wrapper，可从 `message.content`、content-block array 和 `content.parts[].text` 里恢复 binding array 或 object map。
- 本轮补充：interaction script 的 per-step keybinding mutation 复用同一套 collection alias、object-map 和 resource wrapper 解析，脚本步骤可直接使用 `keymap`、`keyboardShortcuts`、`hotkeys`、`keyboard`、`preferences` 或 `keybindingConfig` 临时改键位。
- 本轮补充：interaction script 的 `keys` 字段支持 printable text chunk 和空格分隔 named-key sequence，例如 `ctrl-x ctrl-k`，减少官方脚本把连续输入拆成数组的改写成本。
- 本轮补充：interaction script key input 接受 press-style aliases，包括 `press`、`keyPress`、`keypress`、`shortcutKey`、`presses`、`keyPresses` 和 `shortcuts`。
- 本轮补充：interaction script loader 现在会扁平化 `cases`/`tests`/`testCases`/`scenarios`/`fixtures` 等 suite array，每个 case 内的 `steps`/`timeline`/`scriptSteps` 会按顺序展开，顶层数组也可直接混入 case object。
- 本轮补充：interaction script loader 现在也接受 provider-style `choices`/`outputs`/`candidates`/`generations` response wrapper，可从 `message.content`、content-block array 和 `content.parts[].text` 里递归恢复 script JSON。
- 本轮补充：interaction script 和 keybinding provider response 现在会剥离 fenced `json` code block，模型/SDK 返回 code-fenced 脚本或 keybinding 配置时不再需要手工去 fence。
- 本轮补充：interaction script 和 keybinding provider response 的 fenced JSON 提取现在接受 inline/glued fence 形态，例如语言标记后同一行直接跟脚本数组或 keybinding map，模型输出不换行时仍能加载配置。
- 本轮补充：interaction script key/keySequence action payload 现在递归解包 JSON:API/GraphQL-style wrapper，`payload.resource.attributes.key` 和 `edge.node.attrs.sequence` 可直接驱动按键与组合键序列。
- 本轮补充：interaction script 的直接字符串 alias 字段现在也接受 wrapped object；`text`、`pasteText`、`setStatus`、`snapshotName` 等字段可从 `resource.attributes` 或 `edge.node.attrs` 中恢复正文、paste、status 和 snapshot 名称，避免 direct field fixture 在 scalar decode 阶段失败。
- 本轮补充：interaction script action/type/kind/name/operation 动作判别字段接受 compact/camel fixture aliases，包括 `typeText`、`inputText`、`insertText`、`keyPress`、`pressKey`、`keySequence`、`pasteText`、`pastedText`、`clipboardText`、`setStatus`、`statusLine`、`terminalSize` 和 `screenSize`。
- 本轮补充：interaction script 的 typeText/pasteText/setStatus/snapshot 字符串 action payload 现在递归解包 JSON:API/GraphQL-style wrapper，`payload.resource.attributes.text`、`edge.node.attrs.content`、`resource.attributes.message` 和 wrapped snapshot `name` 可直接驱动 prompt、paste、status 与 snapshot capture。
- 本轮补充：interaction script resize/terminalSize/screenSize action payload 现在递归解包 `value`/`payload`/`data`/`resource`/`attributes`/`properties`/`attrs`/`edge.node` 等 wrapper，官方/API fixture 可把 columns/rows 放在 JSON:API 或 GraphQL envelope 内。
- 本轮补充：interaction script 的 direct resize 数字 alias 字段现在也递归解包 wrapper；`resizeWidth`/`resizeHeight`、`screenWidth`/`screenHeight` 和 terminal width/height 相邻别名可从 wrapped `value`、`columns`、`rows` 中恢复尺寸。
- 本轮补充：interaction script 的 direct focus bool alias 字段现在也递归解包 wrapper；`focus`、`focused`、`focusIn`、`focusOut`、`blur`/`blurred` 可从 wrapped `enabled`、`value`、`selected` 等字段恢复焦点事件控制。
- 本轮补充：interaction script 的 focus/blur action-discriminator 现在也接受 `value`/`payload`/`data` 中的 wrapped bool；`action:"focus"`、`kind:"blur"`、`operation:"focusState"` 和 `name:"setFocus"` 可用 `focused:false`、`blurred:false` 或非严格 bool payload 明确发出 focus-out/focus-in。
- 本轮补充：interaction script 的 direct expectation bool alias 字段现在也递归解包 wrapper；`expectNoEvent`、`expectNoDialogResult(s)`、`expectFocused` 可从 wrapped `value`/`enabled`/`selected` 恢复断言控制。
- 本轮补充：interaction script 的 direct expectation count alias 字段现在也递归解包 wrapper；`expectEventCount`、`expectTotalEventCount`、`expectDialogResultCount`、`expectTotalDialogResultCount` 可从 wrapped `value`/`count`/`total` 恢复计数断言。
- 本轮补充：interaction script 的 direct expectation string-list alias 字段现在也递归解包 wrapper；`expectStatusContains`/`NotContains` 和 `expectSnapshotContains`/`NotContains` 可从 wrapped `value`/`values`/`contains`/`items` 恢复断言列表。
- 本轮补充：interaction script 的 direct expectation collection alias 字段现在也递归解包 wrapper；`expectEvents`、`expectDialogResults` 可从 wrapped `events`/`results`/`items`/`nodes` 中恢复结构化断言列表。
- 本轮补充：interaction script 的 direct single expectation alias 字段现在也递归解包 wrapper；`expectEvent`、`expectDialogResult` 可从 wrapped `event`/`result`/`expected` 中恢复结构化单项断言。
- 本轮补充：interaction script 的 direct single expectation alias 字段现在改为 raw payload 解析；`expectEvent`、`expectDialogResult` singular 字段也可接受单元素数组并取首项，避免基础 step unmarshal 提前失败。
- 本轮补充：interaction script 的 direct prompt expectation 现在也递归解包 JSON:API/GraphQL-style wrapper；`expectPrompt`/`expect_prompt` 可从 `resource.attributes`、`edge.node.attrs` 等外壳恢复 prompt text、cursor、empty、pasted-content count 和 next pasted ID 断言。
- 本轮补充：interaction script 的 direct vim expectation 现在也递归解包 JSON:API/GraphQL-style wrapper；`expectVim`/`expect_vim` 可从 `resource.attributes`、`edge.node.attrs` 等外壳恢复 enabled、mode、register 和 register-linewise 断言。
- 本轮补充：interaction script 的 direct screen/viewport expectation 现在也递归解包 JSON:API/GraphQL-style wrapper；`expectScreen`/`expect_screen` 和 `expectViewport`/`expect_viewport` 可从 `resource.attributes`、`edge.node.attrs` 等外壳恢复 columns/rows、scroll offset、visible line count 和 visible contains/not-contains 断言。
- 本轮补充：interaction script 的 direct task/reverse-search expectation 现在也递归解包 JSON:API/GraphQL-style wrapper；`expectTasks`/`expect_tasks` 和 `expectReverseSearch`/`expect_reverse_search` 可从 `resource.attributes`、`edge.node.attrs` 等外壳恢复 task count/stateCounts/contains、reverse active/query/cursor/current/result-count 断言，并保留 wrapped `active:false` 与 `taskCount:0`。
- 本轮补充：interaction script 的 direct dialog expectation 现在也递归解包 JSON:API/GraphQL-style wrapper；`expectDialog`/`expect_dialog` 可从 `resource.attributes`、`edge.node.attrs` 等外壳恢复 active、ID/kind、title/body、body/action contains、action count 和 focused index 断言，并保留 wrapped `active:false`。
- 本轮补充：interaction script action/type/kind/name/operation 动作判别字段继续接受 compact/camel event/media aliases，包括 `focusIn`、`focusOut`、`mouseEvent`、`pasteImage` 和 `imagePaste`。
- 本轮补充：interaction script mouseEvent/pasteImage action payload 现在递归解包 JSON:API/GraphQL-style `resource`/`attributes`/`properties`/`attrs`/`edge.node` wrapper，mouse payload 的 Button/X/Y/Release 可接受数字字符串/非严格 bool，wrapped mouse 坐标/按钮和 image filename/media/content 可直接驱动 dialog click 与 image paste。
- 本轮补充：interaction script action/type/kind/name/operation 动作判别字段也可驱动 runtime/dialog mutation，支持 `requestPermission`、`taskStatus`、`showTasks`、`cancelTasks`、`removeTask` 和 `showDialog` 等动作，并从 `value`/`payload`/`data`/`body` 载荷解析对象、ID 或取消原因。
- 本轮补充：interaction script 的 direct runtime mutation 字段现在改为 raw payload 解析；`requestPermission`/`request_permission` 和 `upsertTask`/`upsert_task` singular 字段也可接受单元素数组并取首项，避免基础 step unmarshal 提前失败。
- 本轮补充：interaction script 的 direct mouse 字段现在改为 raw payload 解析；`mouse`/`mouse_event`/`mouseEvent` singular 字段可接受单元素数组并递归解包 `resource.attributes`、`edge.node.attrs` 等 wrapper，避免录制脚本把鼠标事件包成 API payload 时提前失败。
- 本轮补充：interaction script 的 direct message list 字段现在改为 raw payload 解析；`messages`/`appendMessages`/`transcriptMessages` 可递归解包 `resource.attributes`、`data[]`、`edge.node.attrs` 等 API/GraphQL wrapper，并保留 image paste id 等 message metadata。
- 本轮补充：interaction script 的 direct single message 字段现在也复用 raw message parser；`message`/`Message` 可递归解包 `resource.attributes`、`edge.node.attrs`，并在数组形态下回退为多条 message 追加，避免单条消息 wrapper 被基础解码成空消息。
- 本轮补充：interaction script 的 direct dialog 字段现在改为 raw payload 解析；`dialog`/`Dialog` 可递归解包 `resource.attributes`、`edge.node.attrs` 和单元素数组，`showDialog` action payload 也复用同一解析路径，dialog payload 的 `ID`/`Focused` 可接受数字和数字字符串，避免 wrapper-only 或 Go 默认字段名 dialog 被解码失败/空弹窗。
- 本轮补充：interaction script 的 direct image 字段现在改为 raw payload 解析；`image`/`Image` 可复用 pasteImage action 的 wrapper 解析，递归解包 `resource.attributes`、`edge.node.attrs` 和单元素数组，保留 filename/media/content/source-path 后再生成 prompt pasted-image。
- 本轮补充：interaction script 的 direct keybinding mutation 字段现在改为 raw-only 解析；`keybindings`/`key_bindings`/`keyBindings`/`keybindingSpecs`/`Keybindings` 不再先走 `[]BindingSpec` 基础解码，统一复用 keybinding loader 的 object-map、resource wrapper 和 edge/node collection parser。
- 本轮补充：interaction script 的 UpperCamel step 字段现在也进入 raw/wrapper 解析路径；`UpsertTask`、`CancelAllTasks`、`OpenTasksDialog`、`ExpectEvent(s)`、`ExpectDialog`、`ExpectDialogResult(s)`、`ExpectPrompt`、`ExpectVim`、`ExpectScreen`、`ExpectViewport`、`ExpectReverseSearch`、`ExpectTasks` 等 Go 默认字段名可直接接受 JSON:API/GraphQL-style wrapper，task status/expectation 的 ID/title/state/detail/progress、tasks expectation 的 Count/StateCounts、event expectation 的 Type/Value/DialogID、dialog expectation 的 Active/ActionCount/FocusedIndex、dialog-result expectation 的 ID/Found/Stale、prompt expectation 的 Cursor/PastedContentCount/NextPastedID/Empty、pasted-content expectation 的 ID/MediaType/Filename/ContentContains、vim expectation 的 Enabled/Mode/Register/RegisterLinewise、screen expectation 的 Width/Height、viewport expectation 的 Offset/VisibleLineCount 和 reverse-search expectation 的 Cursor/ResultCount/NoResults 改为 raw-first 解码并继续接受数字字符串/非严格 bool。
- 本轮补充：terminal CSI-u/kitty keyboard parser 接受 `codepoint:alternate` 和 `modifier:event-type` 冒号字段，按主 codepoint/modifier 解析 ctrl/alt/shift/rune 键，覆盖 kitty progressive keyboard protocol 的常见变体。
- 本轮补充：terminal CSI-u/kitty keyboard parser 接受无 modifier 字段或 modifier `1` 的 base key 序列，覆盖 printable rune、Enter、Tab、Esc 和 Backspace，避免 extended-key 模式下普通键序列被解析成 unknown。
- 本轮补充：terminal CSI-u/kitty keyboard parser 现在把 shift-only Backspace (`CSI 8;2u`/`CSI 127;2u`) 仍映射到 Backspace，避免 kitty extended-key 模式下退格被误当作 DEL rune 或 unknown。
- 本轮补充：terminal input parser 接受 xterm modified arrow 序列（如 `CSI 1;2D`、`CSI 1;6C`、`CSI 1;7D`），把 shift-arrow 降级为方向键、alt-arrow 映射到 word-motion key、ctrl/ctrl+alt-arrow 映射到 ctrl word-motion key，避免 extended navigation 序列落入 unknown。
- 本轮补充：terminal input parser 现在把 xterm modified navigation modifier 范围扩展到 `2..16`，覆盖 meta/shift+meta/ctrl+meta 组合（如 `CSI 1;10D`、`CSI 1;16C`）以及对应 Home/End/Delete/PageUp/PageDown 序列。
- 本轮补充：terminal CSI-u/kitty keyboard parser 现在按 modifier bitfield 解码 `9..16` 扩展组合，把 meta/shift+meta 映射到现有 alt key surface，把 ctrl+meta 组合保留为 ctrl key，覆盖 `CSI 98;9u`、`CSI 97;13u` 等序列。
- 本轮补充：terminal CSI parser 把 DA/device attributes (`CSI c`、`CSI >c`、`CSI =c`) 归入 report action，并在 terminal parser dispatcher 中作为 `TerminalActionReport` 暴露。
- 本轮补充：terminal CSI parser 现在保留多参数 DA/device-attributes response 的完整 code list，例如 `CSI ?62;1;2;6c` 不再只留下首个 terminal type code。
- 本轮补充：terminal CSI parser 接受 `CSI a`/`CSI e`/`CSI \`` cursor alias final bytes，并映射到已有 cursor-forward/cursor-down/cursor-column actions。
- 本轮补充：terminal CSI parser 接受 ECMA `CSI Ps j` / `CSI Ps k` HPB/VPB backward cursor final bytes，并映射到已有 cursor-back/cursor-up actions。
- 本轮补充：terminal CSI parser 接受 DEC private mode `?1047h/l` alternate-screen buffer 和 `?1048h/l` save/restore cursor，复用已有 mode/cursor actions。
- 本轮补充：terminal CSI parser 把 DECREQTPARM terminal-parameters (`CSI x`) 归入 report action，保留 code/private marker。
- 本轮补充：terminal CSI parser 现在保留 DECREPTPARM/terminal-parameters response 的完整参数列表，例如 `CSI 2;1;1;112;112;1;0x` 不再只留下 report code。
- 本轮补充：terminal CSI parser 把 DECRQM mode request (`CSI Ps $ p` / `CSI ? Ps $ p`) 归入 report action，保留 mode code 和 DEC private marker。
- 本轮补充：terminal CSI parser 现在保留 DECRQM mode-request 的完整参数列表，例如 `CSI ?25;1000$p` 会同时暴露首个 mode code 和原始 params。
- 本轮补充：terminal CSI parser 把 CPR cursor-position responses (`CSI row;col R` / DEC private `CSI ? row;col R`) 归入 report action，结构化暴露 row/column 并保持 visible-text stripping。
- 本轮补充：terminal CSI parser 继续补齐 DEC 私有 DSR/CPR，`CSI ?6n` 现在归入 cursor-position report query，`CSI ?row;col;page R` 会保留 page 元数据。
- 本轮补充：terminal CSI parser 现在保留 CPR cursor-position response 的完整参数列表，例如 `CSI ?12;34;2R` 会同时暴露 row/column/page 和原始 params。
- 本轮补充：terminal CSI parser 现在保留 DSR/device-status report 的完整参数列表，例如 `CSI ?6;1n` 会同时暴露首个 report code 和原始 params。
- 本轮补充：terminal CSI parser 把 xterm window manipulation/report (`CSI t`) 归入 report action，覆盖常见 `CSI 14t`/`CSI 18t` 查询，并把 `CSI 4;height;width t` 与 `CSI 8;rows;cols t` 的 pixel/text-area 尺寸参数结构化暴露。
- 本轮补充：terminal CSI parser 现在保留 xterm window report 的完整参数列表，例如 `CSI 3;x;y t`、`CSI 4;height;width t`、`CSI 8;rows;cols t` 不再丢失 report code 后面的原始字段。
- 本轮补充：terminal CSI parser 把 DECRPM mode status report (`CSI Ps;Ps $ y` / `CSI ? Ps;Ps $ y`) 归入 report action，保留 mode code、status 和 DEC private marker。
- 本轮补充：terminal CSI parser 现在保留 DECRPM mode-status response 的完整参数列表，例如 `CSI ?25;2$y` 会同时暴露 code/status 和原始 params。
- 本轮补充：terminal CSI parser 把 TBC tab-clear (`CSI g`/`CSI 3g`) 归入 cursor action，保留 clear-current/all code。
- 本轮补充：terminal ESC parser 把 HTS horizontal-tab-set (`ESC H`) 归入 cursor/tab-set action，和 CSI tab-clear 控制序列形成闭环。
- 本轮补充：terminal sequence dispatcher 把 SS3 application cursor (`ESC OA`/`OB`/`OC`/`OD`) 归入结构化 cursor move action，避免 application cursor mode 序列落入 unknown。
- 本轮补充：terminal sequence dispatcher 现在也把 modified SS3 application cursor (`ESC O 1;2A`/`1;5B`/`1;16D`) 归入结构化 cursor move action，和 input parser 的 modified SS3 navigation 支持保持一致。
- 本轮补充：terminal CSI parser 把 DEC `?1h/l` application cursor mode 解析成独立 mode action，和 SS3 application cursor key 解析闭环。
- 本轮补充：terminal CSI parser 把 DEC `?3h/l` 132/80-column mode 解析成结构化 `columnMode` action，覆盖常见列宽状态切换序列。
- 本轮补充：terminal CSI parser 把 DEC `?40h/l` allow column switching mode 解析成结构化 `allowColumnSwitch` mode action，补齐 `?3h/l` 列宽切换相邻的许可状态序列。
- 本轮补充：terminal CSI parser 把 DEC `?95h/l` no-clear-on-column-switch mode 解析成结构化 `noClearOnColumnSwitch` mode action，补齐列宽切换时是否清屏的状态序列。
- 本轮补充：terminal CSI parser 把 DEC `?5h/l` reverse video/screen mode 解析成结构化 `reverseVideo` mode action，继续减少终端显示状态序列的 unknown fallback。
- 本轮补充：terminal CSI parser 把普通 `CSI 4h/l` insert/replace mode 解析成 `insertMode` action，避免 ECMA mode set/reset 序列落入 unknown。
- 本轮补充：terminal CSI parser 把普通 `CSI 20h/l` line-feed/new-line mode 解析成 `lineFeedMode` action，继续覆盖 ECMA mode set/reset 序列。
- 本轮补充：terminal CSI parser 把 DEC `?6h/l` origin mode 和 `?7h/l` auto-wrap mode 解析成结构化 mode action，继续减少终端状态序列的 unknown fallback。
- 本轮补充：terminal CSI parser 把 DEC `?8h/l` auto-repeat mode 解析成结构化 `autoRepeat` mode action，补齐键盘重复状态序列。
- 本轮补充：terminal CSI parser 把 DEC `?12h/l` cursor blink mode 解析成结构化 `cursorBlink` mode action，补齐 cursor visibility/style 相邻的终端状态序列。
- 本轮补充：terminal CSI parser 把 DEC `?44h/l` margin bell mode 解析成结构化 `marginBell` mode action，补齐 wrap/margin 相邻的响铃状态序列。
- 本轮补充：terminal CSI parser 把 xterm/DEC `?45h/l` reverse-wraparound mode 解析成结构化 `reverseWrap` mode action，补齐 auto-wrap 相邻的 wrap 状态序列。
- 本轮补充：terminal CSI parser 把 DEC `?46h/l` logging mode 解析成结构化 `logging` mode action，避免日志状态序列落入 unknown fallback。
- 本轮补充：terminal CSI parser 把 DEC `?66h/l` application keypad mode 解析成结构化 `applicationKeypad` mode action，补齐 application cursor mode 相邻的输入状态序列。
- 本轮补充：terminal ESC parser 现在把 VT100 `ESC =`/`ESC >` application/numeric keypad 模式也归一成 `applicationKeypad` mode action，和 CSI `?66h/l` 输出保持一致。
- 本轮补充：terminal CSI parser 把 DEC `?67h/l` backarrow key mode 解析成结构化 `backarrowKey` mode action，补齐键盘输入状态序列。
- 本轮补充：terminal CSI parser 把 DEC `?69h/l` left/right margin mode 解析成结构化 `leftRightMarginMode` mode action，补齐 scroll-region 相邻的 margin 状态序列。
- 本轮补充：terminal CSI parser 现在把带参数的 `CSI Pl;Pr s` 解析成 left/right horizontal margin region action，同时保留无参数 `CSI s` save-cursor 语义，和 DEC `?69h/l` margin mode 闭环。
- 本轮补充：terminal CSI parser 现在识别带 intermediate space 的 `CSI Ps SP @` / `CSI Ps SP A` scroll-left/right 序列，避免误解析成 insert-characters 或 cursor-up。
- 本轮补充：terminal tokenizer/dispatcher/parser 现在接受 C1 单字节 ESC 等价控制 `IND`/`NEL`/`HTS`/`RI` (`0x84`/`0x85`/`0x88`/`0x8d`)，映射到已有 `ESC D`/`ESC E`/`ESC H`/`ESC M` cursor/tab-set 动作，并在 visible-text 提取时消费这些控制字节。
- 本轮补充：terminal ESC parser 现在把 charset selection (`ESC ( B` / `ESC ) 0` / `ESC * B` / `ESC / A` / `ESC % G` 等) 解析成结构化 charset action，并在 terminal parser 可见文本管线中消费，避免常见终端 charset 选择序列残留为 unknown。
- 本轮补充：terminal ESC parser 现在也把 ISO-2022 charset shift 控制 (`ESC N`、`ESC n`、`ESC o`、`ESC |`、`ESC }`、`ESC ~`) 解析成结构化 charset-shift action，并在可见文本管线中消费，继续减少真实终端输出里的 unknown 控制序列。
- 本轮补充：terminal ESC parser 现在把 DEC line/screen attribute (`ESC # 3/4/5/6/8`) 解析成结构化 screen action，terminal parser 会透传 double-height top/bottom、single/double-width 和 alignment-test 控制。
- 本轮补充：terminal ESC parser 现在把 DECID identify-terminal (`ESC Z`) 解析成 device-attributes report action，和 `CSI c` 查询路径保持一致。
- 本轮补充：terminal CSI parser 现在把 DEC selective erase `CSI ? Ps J` / `CSI ? Ps K` 标记为 selective display/line erase，和普通 ED/EL 区分开。
- 本轮补充：terminal CSI parser 现在把 ECMA `CSI Ps N` / `CSI Ps O` 解析成 erase-in-field / erase-in-area action，覆盖 to-end/to-start/all 三种 region。
- 本轮补充：terminal CSI parser 把 DEC insert/delete columns (`CSI Ps ' }` / `CSI Ps ' ~`) 归入 edit action，避免列编辑控制序列落入 unknown fallback。
- 本轮补充：terminal CSI parser 把 REP repeat-preceding-character (`CSI b`) 归入 edit action，visible-text/snapshot pipeline 和 ANSI message wrapping/trim 会按重复次数展开前一个可重复 grapheme。
- 本轮补充：terminal CSI parser 按 ANSI 默认参数解析 scroll-region (`CSI r`/`CSI ;10r`)，缺失 top 默认为 1，缺失 bottom 保持 0 表示 reset/full-height，避免 reset scroll region 被误判为单行区域。
- 本轮补充：terminal CSI parser 现在按 ANSI 默认参数处理显式 `0` 计数/位置参数，cursor movement/position/column、insert/repeat/erase chars 和 scroll up/down 这类动作会把 `CSI 0...` 解析为默认 1，同时保留 mode/report/erase selector 的原始 0 语义。
- 本轮补充：terminal CSI parser 把 DECSTR soft reset (`CSI !p`) 归入 reset action，并在 terminal parser 中清理 SGR/link 状态。
- 本轮补充：terminal parser 的 text grapheme 分段继续补齐 emoji keycap sequence，`1️⃣`/`2⃣` 这类 keycap 在完整输入和跨 `Feed()` 边界切在 base 或 variation selector 后时都会保持单个宽 grapheme；完整 Unicode UAX #29 分段仍未宣称完成。
- 本轮补充：terminal parser 的 text grapheme 分段继续补齐 Hangul L/V/T jamo 连接规则，decomposed `한` 这类音节在完整输入以及跨 `Feed()` 边界切在 leading/vowel jamo 后时都会保持单个宽 grapheme；完整 Unicode UAX #29 分段仍未宣称完成。
- 本轮补充：terminal parser 的 text grapheme 分段继续补齐 CRLF line-break cluster，完整输入和跨 `Feed()` 边界切在 `\r`/`\n` 中间时都会保持单个零宽 line-break grapheme，wrap/trim/render 宽度路径也复用同一 line-break 判断；完整 Unicode UAX #29 分段仍未宣称完成。
- 本轮补充：terminal parser 的 text grapheme 分段继续补齐 Unicode mark category 和 Prepend 规则，nonspacing/enclosing/spacing mark 会归入前一 cluster，`का` 这类 spacing mark cluster 不再被拆宽，Arabic prepend mark 加 base text 会保持同一 grapheme，单独 prepend mark flush 时按零宽处理；完整 Unicode UAX #29 分段仍未宣称完成。
- 本轮补充：terminal parser 的 text grapheme 分段继续补齐常见 Indic virama conjunct，`क्ष` 这类 consonant + virama + consonant cluster 在完整输入和跨 `Feed()` 边界切在 virama 后时都会保持单个窄 grapheme；完整 Unicode UAX #29 分段仍未宣称完成。
- 本轮补充：prompt history 写入按官方 `history.ts` 跳过 image pasted content，避免把 image base64/filename/mediaType 存入 `history.jsonl`，读取路径仍兼容旧 image metadata。
- 本轮补充：paste-cache 增加按 cutoff mtime 清理旧 `.txt` paste 文件的 best-effort 入口，忽略缺失目录、非 `.txt` 文件和单文件错误，和官方 `cleanupOldPastes` 语义对齐。
- 本轮补充：Buffered prompt history writer 支持撤销最近 pending entry，覆盖官方 `removeLastFromHistory` 在异步 flush 前直接从 pending buffer 移除的 fast path。
- 本轮补充：Buffered prompt history writer 支持撤销已 flush entry 的 slow path：最近 entry 已落盘时记录 timestamp，并让同一 writer 的 up-arrow/ctrl-r 历史读取按当前 session 跳过该 entry。
- 本轮补充：image-cache 增加 session-scoped 图片路径缓存、base64 图片落盘、批量 image 存储、内存路径查询和旧 session cache 清理，贴近官方 `imageStore.ts` 的存取/清理基础。
- 本轮补充：PromptInput/REPL screen 可显式启用 image-cache session，image hint paste 会在插入 `[Image #N]` 时同步缓存路径并写入图片文件，补齐官方 PromptInput 侧的 `cacheImagePath`/`storeImage` 接入点。
- 本轮补充：image paste cache 现在会在没有原始 source path 时把生成的缓存路径写回 pasted-content 的 `SourcePath`，prompt image metadata/history 恢复不再只依赖全局 image-id 路径缓存。
- 本轮补充：prompt submit event 保留 display 和 pasted-content metadata，session 层提供 `PromptMessages` 将 text paste 展开、image paste 转成 Anthropic image source block，并为已缓存图片追加 source-path meta message。
- 本轮补充：pasted image metadata 保留 `dimensions` 和 `sourcePath`，支持 source/dimension 字段别名，并按官方 `createImageMetadataText` 格式生成 source path、原始/显示尺寸和坐标换算倍率 meta text。
- 本轮补充：image hint parser 现在从 iTerm2 OSC File metadata 解析 `width`/`height`、original/display dimension 别名和 `sourcePath`/`source_path`/`path`，并把这些字段传入 prompt pasted image metadata。
- 本轮补充：PromptInput paste 按官方路径清理 ANSI/CR/tab，并用 `PASTE_THRESHOLD=800` 和 `min(rows-10, 2)` 行阈值决定正常高度短 paste 内联、小窗口/长 paste 折叠为 pasted-content ref。
- 本轮补充：PromptInput 编辑后会移除未被 `[Image #N]` 引用的 image pasted-content，session `PromptMessages` 也会在请求构造前过滤 orphan image，避免删除的图片继续作为 image block/meta 发送。
- 本轮补充：image paste pill 支持官方 lazy-space 语义，连续图片自动分隔，图片后直接输入非空白字符时补一个空格，显式空格/换行不会重复补空格。
- 本轮补充：REPL message metadata 保留 `imagePasteIds`，并从已有用户消息的 image ids 和 pasted refs 初始化/推进 `NextPastedID`，贴近官方 resume/continue 避免 paste ID 重用的逻辑。
- 本轮补充：reverse-search 现在按完整 `HistoryEntry` 匹配和选择历史，选中后恢复 text/image pasted-content metadata，后续 submit event 仍携带 display 与图片元数据。
- 本轮补充：prompt history pasted-content 读取接受 `mimeType`/`mime_type`/`contentType`、`fileName`/`file_name`/`name`、`filePath`/`file_path`/`path` 等 text/image metadata 别名，使历史恢复路径和 image hint/parser metadata 兼容面一致。
- 本轮补充：prompt history pasted-content 正文字段接受 `text`/`body`/`message`/`raw`/`base64Data` 等别名，stored pasted-content hash 接受 `digest`/`checksum`/`sha256` 等别名，attachment/cache 风格历史记录可以恢复正文或命中 paste-cache。
- 本轮补充：prompt history `LogEntry` 读取接受 `sessionID`/`session`/`sessionUuid`/`sessionUUID`/`session_uuid` 作为 session id 别名，current-session-first 历史排序不会因 session 字段拼写不同而失效。
- 本轮补充：prompt history `HistoryEntry`/`LogEntry` 和 pasted-content item 现在接受 `entry`/`record`/`item`/`payload` 等 wrapper，stored history 可从 nested payload 读取显示文本/附件并用外层 project/session/timestamp metadata 补齐。
- 本轮补充：prompt history `HistoryEntry`/`LogEntry` 和 pasted-content item 现在也递归解包 `edge`/`node`/`resource`/`attributes`/`properties`/`attrs` wrapper，GraphQL/JSON:API history exports 可直接恢复 prompt 与附件 metadata。
- 本轮补充：REPL message restore 现在可从用户消息 content blocks、`imagePasteIds` 和 pasted-content metadata 重建 prompt text、`[Image #N]` 引用和 base64 image pasted contents。
- 本轮补充：Ctrl-S prompt stash 现在保存/恢复 prompt text、cursor 和 pasted-content metadata，空 prompt 下再次触发会恢复 stash，贴近官方 `chat:stash`。
- 本轮补充：remote history REST/link 风格分页接受 `links`/`_links` 的 `next`/`previous`/`prev`/`older` 字符串 URL 或 `{href,url,uri,link}` 对象，并从 `before_id`、`beforeId`、`cursor`、`pageCursor`、`previousCursor`、`prevCursor`、`beforeCursor`、`olderCursor`、`startCursor`、`endCursor` 等 query 参数提取续抓 before-id。
- 本轮补充：remote history REST/link 风格分页也接受 RFC/JSON:API 风格 `links` 数组，按 `rel`/`relation`/`name`/`type`/`kind`/`label` 中的 `previous`/`prev`/`older`/`next` 选择 continuation URL。
- 本轮补充：remote history HTTP `Link` header 分页接受 `rel="previous"`/`prev`/`older`/`next` URL，并按 RFC Link 结构处理 `<...>` URL 和 quoted 参数里的逗号，再从同一组 before/cursor query 参数提取续抓 before-id。
- 本轮补充：sidechain/subagent state loader 接受 `subagent_start`/`agent_start`/`task_start` 和 `sidechain_end`/`subagent_finish`/`agent_finish`/`task_summary` 等 subtype 别名，并归一化 `active`/`success`/`canceled`/`error` 等状态别名。
- 本轮补充：sidechain runtime finish 在写入 summary 前会把 `success`/`error`/`canceled` 等状态别名归一为 `completed`/`failed`/`cancelled`，sidechain transcript 和主 transcript 的 lifecycle 输出保持 canonical。
- 本轮补充：sidechain runtime 现在会拒绝同一 sidechain ID 的 running 状态重复 start；已完成后重新 start 会被视为新 lifecycle，state loader 会清空上一轮 summary/endedAt 并使用新的 startedAt。
- 本轮补充：sidechain/subagent lifecycle content 读取现在递归解包 `payload`/`data`/`body`/`result`/`response`/`metadata` 等 wrapper，并从嵌套 start event 中恢复 agent type、workspace path 和 task description。
- 本轮补充：sidechain/subagent lifecycle content 读取现在也递归解包 GraphQL/JSON:API 风格的 `edge`/`node`/`attrs` wrapper，wrapped start/summary event 可继续恢复 ID、status、summary 和 agent metadata。
- 本轮补充：sidechain/subagent lifecycle content 的 ID 等 string-like 字段现在接受 JSON number/数字标量，numeric subagent ID 会保留为字符串并能通过 resume fallback 找回对应 sidechain。
- 本轮补充：interaction script 的 `message` step 接受 `type`/`speaker` role 别名和 `content`/`body`/`message` text 别名；`image` step 和 iTerm2 image hint 接受 `fileName`/`file_name`/`name`、`mimeType`/`mime_type`/`contentType`、source path/URL 和 `data`/`base64` 内容别名；permission request step 接受 request/permission/tool-use ID、path、description 和 action 字段别名，并允许 `actions` 使用单字符串；`expectPrompt` 接受 `value`/`input`/`content`/`message`、`expandedText`/`fullText`、`cursorIndex`/`cursorPosition`、`isEmpty`/`blank` 等字段别名，且 `pastedContents` 断言接受 `pastedId`/`pastedContentId`、`kind`/`pastedType`、`value`/`data`/`base64`、`contentType`/`mimeType`、`fileName`/`name` 和 `contains` 等字段别名；`expectVim` 接受 `vimEnabled`/`isEnabled`、`vimMode`/`modeName`/`currentMode`、`vimRegister`/`registerValue`/`yankRegister`、`registerLinewise`/`linewise` 等字段别名；`expectTasks` 接受 `taskCount`/`total`/`size`/`length` 和 `statusCounts`/`countsByState` 等字段别名；`expectScreen`/`expectViewport` 接受 `columns`/`rows`、`screenWidth`/`screenHeight`、`scrollOffset`/`viewportOffset`、`visibleRows`/`lineCount` 等字段别名；`expectReverseSearch` 接受 `isActive`/`visible`/`open`、`search`/`term`/`pattern`、`cursorIndex`、`currentResult`、`matchCount`/`matches`、`noMatches` 等字段别名；`expectDialog` 可断言 body contains/not-contains、actions/action contains/not-contains、action count 和 focused action，runtime-aware scripts 会在步骤间保留 dialog focused action，且接受 `isActive`/`visible`、`dialogId`/`dialogID`、`dialogKind`、`heading`/`header`、`content`/`text`/`message` 等字段别名；`expectEvent`/`expectDialogResult` 接受 `eventType`/`event`/`name`、`payload`/`text`/`message`、`dialogId`/`dialogID`/`dialogKind`、`actionValue`/`resultStatus`/`exists`/`isStale` 等字段别名。
- 本轮补充：interaction script step 接受 `resize`/`terminalSize`/`screenSize` 对象或 `[width,height]` 数组、顶层 `columns`/`rows` resize 别名、`focus`/`focused`/`blur`/`focusIn`/`focusOut` focus event 别名、`snapshot`/`snapshotId`/`snapshotLabel` capture 名称别名，以及 runtime-aware mutation 别名如 `permission`/`permissionRequest`、`task`/`taskStatus`、`removeTask`/`deleteTask`、`cancelPermission`、`cancelTasks`/`cancelReason`、`openTasks`/`showTasks`。
- 本轮补充：interaction script step 可通过 `status`/`setStatus`/`statusLine`/`baseStatus` 设置状态行；runtime-aware scripts 会把它作为 base status，并继续叠加 permission/task 计数，方便复用带 status line 的 ANSI/interaction fixture。
- 本轮补充：interaction script 批量消息注入接受 `messages`、`append_messages`/`appendMessages`、`transcript_messages`/`transcriptMessages` 字段，且这些字段可用单对象或对象数组，复用 chat/transcript role/text 别名；message 注入也接受 `pastedContent`/`attachments` 粘贴内容别名、单数 `imagePasteId` 图片引用别名，以及 pasted-content 的 `kind`/`value`/`data` 内容字段别名。
- 本轮补充：interaction script direct `dialog` step 接受 `dialogId`/`dialogKind`、`heading`/`header`、`content`/`text`/`message`、`options`/`choices`/`buttons`、`focusedIndex`/`selectedIndex` 等字段别名，且 actions/options 可用单字符串。
- 本轮补充：interaction script loader 接受 `scriptSteps`/`script_steps`、`interactionSteps`/`interaction_steps` wrapper 字段，并支持一层 `scenario`/`test`/`case`/`fixture`/`interaction` 嵌套对象。
- 本轮补充：interaction script loader 现在也接受 JSON:API/resource-style `resource`/`attributes`/`properties` wrapper，可从 attributes/properties 内继续解析 `steps`/`records`/`timeline`，减少官方 fixture API envelope 的改写成本。
- 本轮补充：interaction script 的单个 step item 现在也接受 JSON:API/resource-style `resource`/`node`/`attributes`/`properties` wrapper，数组元素和 JSONL 行可直接使用 API fixture 的 step resource 形态。
- 本轮补充：interaction script loader 现在把 `data`/`payload`/`body`/`result`/`response`、`resources` 和 `nodes` 中的数组也视为 step list，可直接加载 API/GraphQL collection envelope，同时保留单步 `data` 载荷兼容。
- 本轮补充：interaction script loader 现在接受 GraphQL connection 风格的 `edges` step list 和 JSON:API/HAL collection 风格的 `included`、`collection`/`list`/`children`/`values` step list，数组元素可用 `edges[].node`、`edge.node`、`resource.attributes` 或 `resource.properties` wrapper，外层也可递归解包 `viewer`/`node`/`*Connection` wrapper 来加载录制脚本。
- 本轮补充：interaction script loader 现在也接受 provider-style `choices`/`outputs`/`candidates`/`generations` response wrapper，可从 `message.content`、content-block array 和 `content.parts[].text` 里恢复 script JSON，减少模型/SDK 录制脚本响应的手工拆包。
- 本轮补充：ANSI snapshot corpus 比对支持 `.ansi` only baseline fallback，strict stale-baseline 检查同时覆盖 `.txt` 和 `.ansi`。
- 本轮补充：interaction script JSONL loader 单行上限提升到 50MiB，和 transcript/session 大记录读取容忍度对齐，覆盖大型 paste、image metadata 或 snapshot fixture 脚本行。
- 本轮补充：terminal lifecycle 增加可选 extended-key mode，按官方 `CSI >1u`/`CSI >4;2m` 启用 kitty keyboard protocol 和 modifyOtherKeys，退出时重置 modifyOtherKeys 并 pop kitty stack，reassert 时先 pop 再 push 以避免长期会话 stack 泄漏。
- 本轮补充：renderer/snapshot 增加 opt-in DEC 2026 synchronized output 包裹入口，可用官方 BSU/ESU (`CSI ?2026h`/`CSI ?2026l`) 生成整帧 ANSI fixture，同时默认渲染保持不变。
- 本轮补充：terminal OSC helper 增加 OSC 0 title/icon 序列生成，输入会先 strip ANSI；`StripANSI` 现在会完整跳过 OSC/DCS/APC/PM/SOS payload，避免 title/snapshot 可见文本被终端控制串污染。
- 本轮补充：terminal OSC helper 增加 OSC 21337 tab status 序列、清理序列和 tmux/screen passthrough 包裹，status 文本按官方规则转义分号和反斜杠。
- 本轮补充：terminal OSC helper 增加 OSC 8 hyperlink 开始/结束序列，按官方 rolling hash 为 URL 自动生成 `id=`，并允许显式 params 覆盖。
- 本轮补充：terminal OSC helper 增加 OSC 9;4 progress 序列，覆盖 clear/set/error/indeterminate，running/error 百分比按官方规则 clamp 到 0..100。
- 本轮补充：terminal OSC helper 增加 iTerm2、Kitty、Ghostty notification 序列和 raw BEL helper，调用方可按环境选择是否 wrap multiplexer。
- 本轮补充：terminal OSC helper 增加 OSC 52 clipboard 序列生成，支持默认 `c` selection、显式 clipboard selection 和 clear 序列，并按 UTF-8 base64 编码 payload；native integrations 已补 session-scoped clipboard runtime、system/tmux/OSC52 clipboard adapter 检测与命令计划审计，以及 system/tmux 外部剪贴板命令读写执行 API。
- 本轮补充：terminal OSC helper 增加显式 ST (`ESC \\`) terminator 入口，可按官方 Kitty 避免 BEL 的路径生成 OSC 序列，同时默认 `OSCSequence` 仍保持 BEL terminator。
- 本轮补充：terminal OSC helper 增加 OSC color parser，支持 `#RRGGBB` 和 XParseColor 风格 `rgb:R/G/B`，按官方规则把 1-4 位 hex component 缩放到 8-bit RGB。
- 本轮补充：terminal OSC parser 把 OSC 10-19 dynamic color 设置/查询解析为结构化 color action，复用既有 OSC color parser，并支持同一序列内按官方递增规则连续设置多个 target；visible text/snapshot 继续剥离这些控制串。
- 本轮补充：terminal OSC parser 把 OSC 110-119 dynamic color reset 序列解析为结构化 color reset action，覆盖前景/背景/光标、pointer、Tektronix 和 highlight 动态色 reset。
- 本轮补充：terminal OSC parser 把 OSC 4 palette color 设置/查询和 OSC 104 palette reset 解析为结构化 palette action，支持同一序列内多组 index/color、index/? 和按 index reset。
- 本轮补充：terminal OSC parser 把 OSC 5 special color 设置/查询和 OSC 105 reset 解析为结构化 specialColor action，限制 special index 为 0-4，并保持 visible text/snapshot 清洁。
- 本轮补充：terminal OSC helper 增加 OSC 21337 tab-status payload parser，支持 `\;`/`\\` 转义、bare key 或空值清理、unknown key ignore，并复用 OSC color parser 解析 indicator/status-color。
- 本轮补充：terminal OSC helper 增加 OSC 8 hyperlink payload parser，按官方规则解析 params、保留 URL 内部分号，并把空 URL 识别为 hyperlink end。
- 本轮补充：terminal OSC helper 增加轻量 `ParseOSCContent`，覆盖官方 title(0/1/2)、OSC 8 hyperlink、OSC 21337 tab status 和 unknown action 分支。
- 本轮补充：terminal OSC helper 增加完整 OSC sequence parser，可从带 `ESC ]` 前缀且以 BEL 或 ST 终止的序列解析出 `ParseOSCContent` action。
- 本轮补充：terminal OSC parser 把 OSC 52 clipboard、iTerm2 progress/notification、Kitty notification 和 Ghostty notification 作为结构化 terminal actions 暴露，visible text/snapshot 继续正确剥离这些控制串。
- 本轮补充：terminal OSC parser 把 OSC 133/633 shell integration marks (`A`/`B`/`C`/`D`) 作为结构化 shellIntegration action 暴露，保留 command-end exit code，visible text/snapshot 继续剥离这些 shell 标记。
- 本轮补充：terminal OSC parser 现在识别 VS Code OSC 633 `E` command-line 和 `P` property 记录，保留 raw value，并把 semicolon property payload 解析成结构化 metadata，visible text/snapshot 继续剥离这些 shell 标记。
- 本轮补充：terminal OSC parser 现在解析 OSC 7 current-directory URI，保留 raw URI 并暴露 scheme/host/path，TerminalParser 会输出 directory action，visible text/snapshot replay 继续剥离该控制串。
- 本轮补充：terminal renderer constants 增加官方 clear scrollback (`CSI 3J`) 和 legacy Windows home (`CSI 0f`) 序列 helper，支持现代 clear-screen+scrollback 和 legacy Windows clear 组合；平台自动探测仍留给调用方。
- 本轮补充：terminal CSI helper 增加通用 `CSISequence`、cursor up/down/forward/back/position/move 和 line/screen erase 序列，按官方 helper 的零移动返回空串与 horizontal-first cursorMove 行为生成 ANSI。
- 本轮补充：terminal CSI helper 增加 scroll up/down、set scroll region 和 reset scroll region 序列，scroll 零值返回空串，便于后续补齐官方 viewport/scroll-region 输出路径。
- 本轮补充：terminal CSI helper 增加 DECSCUSR cursor-style 序列，覆盖 block/underline/bar 的 blinking 与 non-blinking code，并保留 unknown style 的默认 cursor fallback。
- 本轮补充：terminal CSI helper 增加 bracketed paste start/end 和 focus in/out 输入 marker 常量，并用现有 parser 验证 focus marker 映射，方便官方交互 fixture 复用原始 CSI marker。
- 本轮补充：terminal CSI helper 增加 `EraseLinesSequence(n)`，按官方 `eraseLines` 语义逐行 `CSI 2K`、行间上移并以 `CSI G` 回到列 1，`n<=0` 返回空串。
- 本轮补充：terminal CSI helper 增加官方 CSI param/intermediate/final byte range 常量和判定函数，为后续更完整 CSI parser/action tests 提供基础。
- 本轮补充：terminal CSI helper 增加官方 CSI final-byte/DEC mode 常量和 `ParseCSISequence` 动作解析，覆盖 cursor move/position/save/restore/show/hide/style、erase display/line/chars、scroll up/down/region、SGR params、alternate-screen/bracketed-paste/mouse/focus mode 和 unknown sequence fallback。
- 本轮补充：terminal CSI parser 现在支持多参数 mode set/reset 序列，例如 `CSI ?1000;1006;2004h` 和 `CSI 4;20l`，在保持单 `Mode` 兼容字段的同时通过 `Modes` 暴露完整 mode 列表。
- 本轮补充：terminal CSI parser 现在对混合 cursor visibility 和 mode 的多参数序列（如 `CSI ?25;1000h`）保留完整 mode list，避免真实终端初始化/恢复序列只暴露 cursor action 而丢失后续 mode。
- 本轮补充：terminal CSI parser 补齐 insert/delete chars、insert/delete lines、forward tab/back tab action，`CSI M` 在 output parser 中按 delete-lines 处理，同时 input tokenizer 仍保留 X10 mouse payload 边界消费。
- 本轮补充：terminal CSI parser 现在把 DSR (`CSI n`) 解析成 report action，覆盖 device-status、cursor-position 和 private-mode unknown report，避免 terminal status query/response 序列继续落入 generic unknown。
- 本轮补充：terminal CSI parser 现在把 DEC `?1006h/l` SGR mouse mode 解析成 mouseTracking action，和 lifecycle 发出的 SGR mouse enable/disable 序列闭环。
- 本轮补充：terminal CSI parser 现在把 DEC `?9h/l` X10 mouse tracking mode 解析成 mouseTracking `x10` action，和 input tokenizer/parser 的 X10 mouse payload 支撑闭环。
- 本轮补充：terminal CSI parser 现在也把 DEC `?1001h/l` highlight、`?1005h/l` UTF-8 mouse mode、`?1015h/l` urxvt numeric mouse mode 和 xterm `?1016h/l` SGR-pixels mouse mode 解析成 mouseTracking action，和输入侧 numeric/SGR mouse 兼容面闭环。
- 本轮补充：terminal CSI parser 现在把 xterm `?1007h/l` alternate scroll mode 解析成独立 mode action，避免 alternate-screen wheel 兼容序列落入 unknown。
- 本轮补充：terminal CSI parser 现在把 DEC `?2026h/l` synchronized output mode 解析成 mode action，和 renderer/snapshot 的 BSU/ESU 包裹路径闭环。
- 本轮补充：terminal ESC helper 增加官方 ESC final-byte 判定和 `ParseESCSequence`/`ParseESCContent`，覆盖 RIS reset、DECSC/DECRC save/restore、IND/RI/NEL cursor action、HTS、charset selection 和 unknown sequence fallback。
- 本轮补充：terminal SGR helper 增加官方 `TextStyle` 状态解析基础，覆盖 reset、bold/dim/italic/underline/blink/inverse/hidden/strikethrough/overline、普通/亮色命名色、256 色、RGB 色、underline color、分号和冒号参数格式；完整 ANSI parser/render style 应用仍继续推进。
- 本轮补充：terminal sequence dispatcher 增加官方 `identifySequence` 等价分流，按 CSI/OSC/ESC/SS3/unknown 识别并委派现有 parser，SS3 按官方 output parser 作为 unknown action；streaming tokenizer 和 text grapheme action 仍继续推进。
- 本轮补充：terminal tokenizer 增加官方 streaming escape boundary 状态机，支持跨 chunk buffer/flush/reset、CSI/SS3/OSC/DCS/APC 序列边界、OSC BEL/ST terminator、ESC intermediate charset 序列、invalid CSI text fallback 和 opt-in X10 mouse payload 消费；完整 text grapheme action parser 仍继续推进。
- 本轮补充：terminal tokenizer 的 SS3 状态现在会消费参数字节后再等待 final byte，`ESC O 1;5D` 这类 modified SS3 cursor 序列可跨 chunk 作为完整 sequence token 进入 dispatcher。
- 本轮补充：terminal parser 增加轻量 ANSI action pipeline，串接 tokenizer、CSI/OSC/ESC dispatcher 和 SGR style state，输出 text/bell/cursor/erase/scroll/mode/title/link/tabStatus/reset/unknown action，文本宽度覆盖 ASCII、emoji 和 East Asian wide；完整 grapheme cluster segmentation 与 renderer style 应用仍继续推进。
- 本轮补充：terminal parser 跟踪 OSC 8 hyperlink start/end 状态，暴露当前 `inLink` 和 `linkUrl`，reset 时清空链接状态，贴近官方 parser 的 link range 状态语义。
- 本轮补充：terminal parser 的 text grapheme 基础分段补齐 combining mark、variation selector、emoji modifier、ZWJ emoji 序列和 regional indicator flag pair；宽度计算现在让 base+combining-mark cluster 保持 base glyph 宽度，emoji presentation/ZWJ/flag 仍按宽 grapheme 处理，完整 Unicode UAX #29 分段仍未宣称完成。
- 本轮补充：terminal parser 的 streaming text action 现在会暂存可能跨 chunk 延续的末尾 grapheme；ZWJ emoji、VS16 emoji ZWJ sequence、emoji modifier sequence、regional indicator flag 和未完成 emoji tag sequence 跨 `Feed()` 边界时不会被拆成两个宽字符，遇到控制序列或 `Flush()` 会先落地 pending text。
- 本轮补充：terminal parser 的 text grapheme 分段继续补齐 emoji tag sequence，把 subdivision flag 这类 black-flag base + tag chars + cancel tag 作为单个宽 grapheme，完整输入以及跨 `Feed()` 边界切在 base emoji 或 tag char 后时都不会拆分视觉 emoji。
- 本轮补充：terminal CSI parser 现在对 tokenizer flush 出来的非 final-byte incomplete CSI 返回 unknown action，而不是丢弃，贴近官方 `parseCSI` 对 flushed partial sequence 的 fallback 行为。
- 本轮补充：terminal sequence dispatcher 对 tokenizer flush 出来的 OSC partial sequence 使用 `ParseOSCContent` fallback，允许无 BEL/ST terminator 的 title/link/tab-status content 按官方 parser 语义产出 action。
- 本轮补充：terminal tokenizer 增加明确的 output/input 构造器，output 路径默认不吞 `CSI M` 后续字节，input 路径默认开启 X10 mouse payload 边界消费，避免调用方误用布尔选项导致 output parser 吞文本或 stdin mouse payload 泄漏。
- 本轮补充：mouse parser 接受 urxvt/xterm 1015 numeric mouse `CSI button;x;yM`，按 legacy offset 还原 button code，左键、释放和滚轮语义与 SGR/X10 mouse 保持一致。
- 本轮补充：SGR mouse parser 现在拒绝负 button 和 0/负坐标，和 URXVT/X10 parser 的坐标下界 guard 对齐，避免无效 terminal mouse packet 触发点击/滚动事件。
- 本轮补充：terminal tokenizer 补齐 PM (`ESC ^`) 和 SOS (`ESC X`) string-control 状态，和 OSC/DCS/APC 一样支持 BEL 或 ST terminator，避免这些控制串 payload 泄漏为 text token。
- 本轮补充：terminal sequence dispatcher/parser 现在把 DCS/APC/PM/SOS string-control 序列分类为 `stringControl` action，保留 payload、terminator 和 incomplete flush 状态，同时 visible text 继续忽略这些不可见控制串。
- 本轮补充：snapshot/OSC 复用 terminal parser 的 visible-text pipeline，`StripANSI` 不再维护独立手写 scanner；可见文本提取统一覆盖 CSI/OSC/DCS/APC/PM/SOS、flushed partial OSC 和 raw BEL 兼容行为，为后续 ANSI parser 与 renderer/snapshot parity 收口。
- 本轮补充：terminal tokenizer、sequence dispatcher、CSI parser 和 visible-text stripping 现在接受 8-bit C1 CSI (`0x9b`) 序列，覆盖分块 SGR 输入以及 input tokenizer 的 X10 mouse payload 边界。
- 本轮补充：terminal key parser 现在接受 8-bit C1 CSI (`0x9b`) 输入形态，覆盖 bracketed paste、focus、direct/numbered/modified navigation、function-key、CSI-u/Kitty key、SGR/URXVT mouse 和 X10 mouse。
- 本轮补充：terminal tokenizer、SS3 parser 和 key parser 现在接受 8-bit C1 SS3 (`0x8f`) 序列，覆盖 application cursor、modified SS3 navigation 和 F1-F4 function-key 输入。
- 本轮补充：terminal tokenizer、OSC parser、string-control dispatcher 和 visible-text stripping 现在接受 8-bit C1 OSC/DCS/APC/PM/SOS 以及 C1 ST (`0x9c`) 终止符，同时保留合法 UTF-8 continuation byte，不会把 emoji/CJK 文本误切成控制串。
- 本轮补充：message renderer 增加 ANSI-aware wrapping/padding，带 SGR 的 message text 会通过 terminal parser 按 grapheme 可见宽度换行，并把 `TextStyle` action 重新渲染为 SGR 序列，避免 escape bytes 参与 layout 宽度计算；普通文本路径保持不变。
- 本轮补充：基础 wrap/pad/trim 改为按 terminal grapheme 可见宽度计算，普通 message、status/dialog/viewport/prompt 的 CJK/emoji 宽字符不再按单 rune 宽度参与布局，继续向 terminal column parity 收口。
- 本轮补充：prompt layout 的 chunking 和 cursor column 映射改为按 terminal grapheme 可见宽度计算，宽字符输入换行和 cursor CSI 定位不再按 rune index 误算列宽。
- 本轮补充：reverse-search footer 的 cursor CSI 定位改为按 query 光标前 terminal grapheme visible width 计算，宽字符历史搜索输入不再按 rune index 误算列宽。

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

当前进度：

- 已有 sidechain transcript/runtime 地基，新增基础 `Task` tool 入口，可按 description/prompt/subagent_type 启动 sidechain、写入 task prompt，并返回 running 状态的 structured content；conversation runner 会把 session transcript path 透传给工具，确保 Task tool 能落到当前 session。完整 AgentTool 执行循环、内置/自定义 agent 选择、task progress/output、kill/resume、worktree isolation/cleanup、remote/team/swarm 仍未完成。
- 本轮补充：conversation runner 会把本地 plugin manifest 发现到的 agent 名称/描述透传给工具 metadata；`Task` tool 的 prompt/schema 会列出 `general-purpose` 和可用 plugin agents，并在存在明确 agent 清单时拒绝未知 `subagent_type`，减少 subagent 类型拼写错误落成孤立 sidechain。
- 本轮补充：plugin agent loader 保留 agent markdown 正文，runner 将 agent path/prompt 传入 `Task` tool；Task 启动 plugin agent sidechain 时会把 agent prompt 写入 sidechain system message，并在 metadata sidecar/lifecycle payload 中持久化 agent path/prompt，后续执行器可直接从 sidechain 恢复 agent 指令。
- 本轮补充：sidechain resume context 现在会在 tail 窗口截断了原始 agent prompt 时，从 metadata 合成一个 deterministic 的 `agent_prompt` system meta message 前置到恢复消息中，避免后续恢复/执行缺失 agent 指令。
- 本轮补充：plugin agent frontmatter 的 `model`、`permissionMode` 和 `tools`/`allowed-tools` 会进入 runner tool metadata，并由 `Task` 写入 sidechain metadata/lifecycle payload 与 structured content，为后续 subagent 执行器接入模型选择、权限模式和工具 allowlist 打地基。
- 本轮补充：plugin command/agent 的 `allowed-tools`/`tools` frontmatter 解析现在只在顶层逗号或空白处分隔，保留括号、方括号和引号内的逗号/空白，避免 `Bash(git commit -m "x,y")` 这类 tool pattern 被误拆。
- 本轮补充：新增 `TaskOutput`/`AgentOutputTool` 和 `KillTask`/`TaskStop` 工具入口；`TaskOutput` 可列出当前 session 的 sidechain task，或按 task/sidechain ID 读取状态、summary、tail 输出和 agent metadata；`KillTask` 会通过 `SidechainManager.Cancel` 写入 cancelled lifecycle summary，已接入默认内置工具集。真实 AgentTool 执行循环、progress event streaming、resume command UI 和 worktree isolation 仍未完成。
- 本轮补充：新增 `ResumeTask`/`TaskResume` 只读工具入口，复用 `BuildSidechainResumeContext` 返回 `can_resume`、截断状态、message limit、agent metadata 和恢复用 tail message 摘要；当 tail 截断原始 plugin agent prompt 时，会沿用已有 deterministic `agent_prompt` system meta message 注入。完整恢复后的 agent 执行循环和 UI picker 仍未完成。
- 本轮补充：tool executor 会为工具内部发出的空-ID progress 自动补当前 `tool_use_id`，conversation runner 新增 `tool_progress` 事件并透出 `contracts.ToolProgress`；`Task`/`TaskOutput`/`KillTask`/`ResumeTask` 现在分别发 `task_started`、`task_listed`/`task_output`、`task_cancelled`/`task_not_running`、`task_resume_context` 进度事件，带 task ID、status、resume/输出摘要字段。完整 agent 执行期间的 step-level streaming 和 TUI task 面板接线仍未完成。
- 本轮补充：sidechain metadata/lifecycle 现在记录 `worktreeOwned` 与 worktree cleanup status/reason/timestamp，`SidechainManager.MarkWorktreeCleanup` 可写入 `worktree_cleanup` lifecycle marker，`TaskOutput` structured content 也会透出 cleanup 状态。真实 worktree 创建、隔离、删除和 ownership enforcement 仍未完成。
- 本轮补充：`Task` 支持显式 `worktree: true`，会从当前 git HEAD 创建 ccgo 受管 detached worktree 并把 owned path 写入 sidechain metadata/structured output；`KillTask` 会对 owned worktree 做受管路径校验后执行 `git worktree remove --force` 并记录 cleanup marker。默认 Task 仍保持原工作目录，完整自动 agent 执行循环、worktree settings/sparse/symlink 语义和完成后自动 cleanup 仍未完成。
- 本轮补充：显式 owned worktree 创建后会应用 settings `worktree.sparsePaths` 和 `worktree.symlinkDirectories`：前者通过 git sparse-checkout 限定 checkout，后者把主 repo 中存在的目录 symlink 到 isolated worktree；应用后的 sparse/symlink 列表会写入 sidechain metadata/lifecycle 并由 `TaskOutput` 返回。完整 agent 执行闭环仍未完成。
- 本轮补充：settings `worktree.enabled`/`worktree.default`/`worktree.auto` 现在可作为 Task 默认 worktree 策略；Task 未显式传 `worktree` 时会按 settings 默认创建 owned worktree，显式 `worktree:false` 会覆盖默认并留在原工作目录。由于默认策略可能创建 worktree，未显式 opt-out 的 Task 权限判定不再按只读处理。
- 本轮补充：`Task` 支持显式 `run: true`，conversation runner 会读取 sidechain conversation，用同一 model client 驱动 subagent 多轮工具循环；subagent assistant/tool_result 都写回 sidechain transcript，最终通过 `SidechainManager.Finish` 标记 completed，主对话收到的 tool result 会更新为 completed summary 并发 `task_agent_started`/`task_agent_completed` progress。完整多 agent 编排和取消传播仍未完成。
- 本轮补充：subagent nested tool loop 会读取 agent metadata 的 `agentAllowedTools`，构造过滤后的 tool registry；子 agent 请求只暴露允许的工具，未列入 allowlist 的工具不会进入 request tools，也不能被 registry lookup 执行。完整 MCP/tool permission UI 展示和团队编排仍未完成。
- 本轮补充：agent `allowedTools` 现在也会注入 subagent permission decider：`Bash(git status:*)` 这类 scoped rule 既会把 Bash 暴露给子 agent，又会拒绝不匹配 pattern 的 Bash 调用；匹配 pattern 的调用继续走基础 permission engine，因此仍保留更高优先级 deny/sandbox 判断。
- 本轮补充：agent metadata 的 `permissionMode` 现在会在 `run:true` subagent 执行时覆盖子 runner permission engine mode；例如 `bypassPermissions` agent 可以执行基础 default mode 下会 ask 的 mutating tool，同时保留原 engine context/rules 以及随后叠加的 agent allowed-tools 限制。
- 本轮补充：`run:true` subagent 执行时会切换到 sidechain metadata 的 `worktreePath`，因此 nested tool loop 的本地工具在 isolated worktree 内运行；完成后会复用 `KillTask` 的受管路径校验和 `git worktree remove --force` 清理 owned worktree，并把 cleanup 状态写回 structured result、progress 和 sidechain lifecycle。团队编排仍未完成。
- 本轮补充：`run:true` subagent 的错误路径现在会收敛 sidechain 终态：context cancel 标记 `cancelled`，其它执行错误标记 `failed`；两类终态都会尝试清理 owned worktree，并把 cleanup marker 透出到主 Task structured result。更细的外部中断 UI/进度呈现仍未完成。
- 本轮补充：新增 `SendMessage`/`TaskSendMessage` 工具入口，可向 running sidechain task 追加 user message，并返回更新后的 message_count、message_uuid 和 structured task state；这为后续 coordinator/team agent 编排提供了最小通信原语。完整 TeamCreate/TeamDelete/team scheduling 仍未完成。
- 本轮补充：新增 `TeamCreate`/`TeamDelete` 工具入口和 session-scoped `teams.json` manifest；TeamCreate 可把已有 sidechain task ID 归组成 team，TeamDelete 删除 team 记录但不取消底层 task，structured result 会返回 team_id、task_ids、task_count 和 team_count。完整 team scheduling、coordinator 策略和远端协作仍未完成。
- 本轮补充：新增 `TeamOutput` 工具入口，可列出当前 session 所有 team，或按 team_id 读取单个 team 并附带成员 task 的当前 status/summary/message_count 等 structured task 摘要。完整 team scheduling、coordinator 策略和远端协作仍未完成。
- 本轮补充：新增 `TeamSendMessage` 工具入口，可向 team 内所有 running task 广播同一条 user message；执行前会验证 team 存在且所有成员 task 仍为 running，避免部分成员收到消息的半成功状态。完整 team scheduling、coordinator 策略和远端协作仍未完成。
- 本轮补充：`TeamCreate`/team manifest 现在可记录可选 `coordinator_task_id`，会校验 coordinator task 已存在并归一化 ID；`TeamOutput` 单队伍读取会返回 coordinator 的当前 task status/summary/message_count，同时列表输出保留 coordinator_task_id。完整 coordinator 调度策略和自动分派仍未完成。
- 本轮补充：`TeamSendMessage` 现在支持 `target` 路由，默认 `members` 仍只广播成员任务，也可显式选择 `coordinator` 或 `all`；发送前会对选中 recipient 做全量 running 校验，避免 coordinator/成员半成功。完整自动任务分派和 coordinator 决策循环仍未完成。
- 本轮补充：新增 `TeamCoordinate` 工具入口，可把 team description、成员 task status/summary 和用户 objective 组装为 deterministic briefing，发送给 running coordinator task；这提供了 coordinator 驱动团队协作的最小上下文注入能力。完整自动调度循环和远端协作仍未完成。
- 本轮补充：新增 `TeamDispatch` 工具入口，可把不同 assignment message 分别发送给 team 内 running members，并在整批发送前校验所有目标成员归属和 running 状态；这补齐了 broadcast 之外的结构化分派原语。完整自动调度循环仍未完成。
- 本轮补充：新增 `TeamSchedule` 工具入口，可根据 team objective 为每个 running member 生成 deterministic scheduled assignment，消息包含成员序号、当前 team status 和 objective，并在写入前全量校验目标成员 running 状态；完整后台调度循环和模型驱动团队自动调度仍未完成。
- 本轮补充：新增 `TeamAutoSchedule` 工具入口，会在一次调用中先向 running coordinator 注入带成员状态的 objective briefing（如果 team 配置了 coordinator），再为所有 running members 生成 deterministic assignment；这补上了 coordinator briefing 与 member schedule 的一段可验证接线。完整模型驱动 coordinator 决策循环仍未完成。
- 本轮补充：`TeamAutoSchedule` 现在接受可选 coordinator/model 生成的 `assignments`/`plan`，会把 planned assignment 逐条写入指定 running member，并在 coordinator briefing、structured content 和 progress 中标记 `schedule_source=coordinator_plan`；未提供 plan 时继续走 deterministic assignment。完整后台 coordinator 决策循环仍未完成。
- 本轮补充：`TeamAutoSchedule` 的 coordinator/model plan 输入现在也接受 `coordinator_plan`/`member_plan` wrapper，并可从 wrapper 内恢复 `objective`，assignment item 支持 `taskId`/`member` 与 `assignment`/`content` 等相邻字段别名，减少模型输出到工具输入之间的手工整理。
- 本轮补充：新增 `Sleep` 工具入口，支持 `duration_ms`/`seconds`/Go duration 字符串，最大 60 秒，并使用 tool context cancellation 中断等待；这补齐了 proactive/gated 工具中的安全 wait 原语。完整 ScheduleCron/RemoteTrigger 仍未完成。
- 本轮补充：新增 `Brief` 工具入口，可把 summary/title/status/details/next_steps/risks 规范为 structured handoff brief，并支持常见字段别名与单字符串列表项归一化；这为后续远端协作/UI brief surface 提供稳定 payload。完整 remote brief UI/调度接线仍未完成。
- 本轮补充：新增 `ScheduleCron` 工具入口和 session-scoped `schedules.json` manifest，可 create/list/delete/trigger/run_due cron schedule metadata，校验 5-field cron 或常见 `@daily` 类表达式，并可绑定 team_id/target/message；`trigger` 会把保存的 schedule message 发送给绑定 team 的 running recipients，`run_due` 会按当前分钟执行到期且启用的 schedule，并记录 last run 状态避免同一分钟重复触发。当前已有手动与一次性到期执行路径，完整后台 daemon 仍未完成。
- 本轮补充：conversation runner 在每轮主请求前会执行一次 `ScheduleCron` due tick，复用 `run_due` 的触发与 last-run 去重逻辑，把到期 schedule 自动注入 running team recipients；无到期任务时不发噪声进度，触发失败会 fail-open 并发 `schedule_due_error` progress。完整常驻后台 daemon 和远端服务接入仍未完成。
- 本轮补充：新增 session-scoped `daemon-state.json` 状态契约和 `/status show daemon` 审计 section，可记录 runtime_state、PID、endpoint、started_at、heartbeat_at 和错误，并按 heartbeat 超时把 running 状态判定为 stale；CLI 新增 `--daemon` heartbeat loop 和 `--daemon-once` 单次写入模式，`--daemon` 会启动 loopback `/health`/`/status`/`/tick` HTTP endpoint，定时 tick 和 `POST /tick` 都复用 `ScheduleCron run_due` 执行到期 schedule。完整 daemon 进程管理/远端托管仍未完成。
- 本轮补充：daemon 控制面新增 loopback `POST /stop` 与 CLI `--daemon-status`/`--daemon-stop`/`--daemon-tick`/`--daemon-state`；未显式传 state path 时会扫描当前项目所有 session 的 `daemon-state.json`，优先选择 running，其次 stale，再按生成时间选择最新 state，让 headless CLI 可跨 invocation 查询、触发 tick 和优雅停止 daemon。完整 detached start/restart 和远端托管仍未完成。
- 本轮补充：CLI 新增 `--daemon-start`/`--daemon-restart` 和内部 `--daemon-session` 接线，父进程会生成 daemon session id、detached 启动同一可执行文件的 `--daemon` 子进程、等待 state running 后输出 state path/pid/endpoint；已有 running daemon 时 start 会复用 discovery，restart 会先走 loopback stop。完整远端托管仍未完成。
- 本轮补充：新增 `internal/remote` session-scoped `remote-service.json` manifest，`advanced.bridge=true` 时会聚合 remote.defaultEnvironmentId、bridge direct endpoint/WebSocket/capabilities/token_required 和 daemon endpoint/capabilities，并通过 `/status show remote` 暴露可审计的远端服务 discovery surface。完整 CCR 云端长连接、云端注册和消息泵仍未完成。
- 本轮补充：新增 `RemoteTrigger` 工具入口，可把 source/event/message 作为远端触发事件注入到 running team recipients；默认优先发给 coordinator，消息正文保留远端来源和事件类型。`event_id` 可选，提供后会写入 session-scoped `remote_triggers.json` receipt，重复投递会 no-op 并记录 duplicate_count，避免远端重试重复注入。完整 remote websocket/CCR 服务接入仍未完成。
- 本轮补充：bridge direct server 新增 loopback-only `POST /remote-trigger` HTTP endpoint，并沿用 direct server token guard；conversation runner 会把该 endpoint 接到 `RemoteTrigger` 的校验、注入和 event_id dedupe 逻辑，远端系统可通过受控 HTTP 请求向 running team 注入事件。完整 remote websocket/CCR 长连接服务仍未完成。
- 本轮补充：bridge direct WebSocket JSON 通道新增 `remote_trigger` action，复用 direct remote trigger request/response 结构和同一回调，可在已鉴权的 loopback WebSocket 连接上注入远端事件。完整 CCR 云端长连接协议仍未完成。
- 本轮补充：bridge manifest 新增 `remote_trigger` capability，声明 `/remote-trigger` HTTP path 和 `remote_trigger` WebSocket action；runner 写出的 session-scoped bridge manifest、direct `/manifest` 响应和 `/status show bridge` 都会暴露该能力，方便远端控制端发现可用入口。完整 CCR 能力协商仍未完成。
- 本轮补充：bridge direct WebSocket JSON 通道新增 `hello`/`health`/`manifest` action，`hello` 会返回 protocol version、session、command count、capabilities 和可用 action 列表；bridge manifest 同时暴露 `websocket_protocol` capability，远端长连接客户端可在同一连接内完成能力握手。完整 CCR 云端长连接服务仍未完成。
- 本轮补充：bridge direct server 新增 `GET /remote-service` discovery endpoint 和 WebSocket `remote_status` action，返回同一份 session-scoped remote service manifest；bridge manifest、direct `/manifest` 响应和 `/status show bridge` 同步暴露 `remote_service` capability，远端控制端可通过 HTTP 或已鉴权 WebSocket 查询 bridge/daemon 服务状态。完整 CCR 云端注册和消息泵仍未完成。
- 本轮补充：remote settings 新增 `registrationUrl`/`authToken`，`advanced.bridge=true` 写出 remote service manifest 后会把 manifest POST 到注册 URL，并将 registered/failed/disabled、HTTP status、远端 session/websocket/poll 信息写入 session-scoped `remote-registration.json`；`/status show remote` 可审计注册状态且不泄露 token/query。完整 CCR 云端长连接仍未完成。
- 本轮补充：remote registration 响应解析现在会先读取顶层字段，再递归解包 `data`、`session`、`remote_session`、`registration`、`result`、`payload` wrapper，兼容云端把 remote session、registration id、websocket/poll endpoint 放在 envelope 内返回。更深的注册协议协商和租约刷新仍未完成。
- 本轮补充：remote registration 响应现在会持久化 `protocolVersion`/`protocol_version`、`capabilities`/`features` 和 `leaseRenewUrl`/`lease_refresh_url` 元数据，并在 `/status show remote` 中以脱敏 URL 暴露协议版本、能力列表和 lease renew endpoint；注册协议版本现在强制校验，只接受空 legacy 版本、`ccr.remote.v1` 和 `ccr.remote.v2`，未知版本会把 registration 标为 failed 并清掉可用 endpoint。更完整的云端协议演进策略仍未完成。
- 本轮补充：remote registration 的显式 `capabilities`/`features` 现在会参与 endpoint 启用：非空能力列表缺少 `websocket_protocol` 时会忽略 websocket URL，缺少 `lease_renew`/`lease_refresh` 时会忽略 lease renew endpoint，并把 capability warning 写入 `remote-registration.json` 与 `/status show remote`；空能力列表仍按 legacy 兼容处理。更完整的云端能力矩阵和协议演进策略仍未完成。
- 本轮补充：新增 session-scoped `remote-pump.json` 和 daemon remote 消息泵；daemon tick 会在 ScheduleCron due tick 后优先读取 registered `websocket_url`，通过 Bearer auth 建立 WebSocket、读取单帧事件并复用 poll 解码/`RemoteTrigger` 注入路径；无 WebSocket 或 WebSocket 失败且存在 poll URL 时回退到带 cursor 的 poll 拉取。pump 状态会记录 transport、websocket/poll URL 脱敏值、cursor、HTTP status、event/delivered/duplicate/error 计数，`/status show remote` 可审计当前传输。完整 CCR WebSocket 常驻持久 stream 和云端协议 hardening 仍未完成。
- 本轮补充：remote WebSocket pump 现在支持单次 tick 内读取多帧事件，并在握手/读帧失败或非正常 close 时按可配置 backoff 重连；daemon 默认读取最多 8 帧、最多重连 2 次，并把 frame/connect/reconnect 计数和 WebSocket close code 写入 `remote-pump.json` 和 `/status show remote`。完整 CCR 云端 WebSocket 常驻持久 stream 与更深协议 hardening 仍未完成。
- 本轮补充：`internal/remote` 新增 callback 型 `StreamWebSocketEvents` primitive，可保持 WebSocket 连接逐帧解码并把事件批次交给调用方，支持 context 取消、可选帧上限、handler 错误传播、异常 close/读错后的 backoff 重连以及 `ReconnectAttempts < 0` 无限重连语义；该能力为 daemon 常驻 stream 托管接线打底。完整云端协议 hardening 仍未完成。
- 本轮补充：`--daemon` 常驻模式现在会在初始 tick 后启动 remote WebSocket stream goroutine，按 heartbeat 间隔重试注册状态，复用 `StreamWebSocketEvents` 和 `RemoteTrigger` delivery/dedupe，把推送事件实时注入 running team，并在 `remote-pump.json` 中持续更新 `websocket_stream` transport、frame/connect/reconnect、delivered/duplicate/error 计数；daemon heartbeat/tick 在已注册 `websocket_url` 时会跳过短 WebSocket 读取，避免 stream 和 tick 双连接/重复写 pump state，poll-only 注册仍走原 tick 路径；daemon stop/context cancel 会取消 stream，并在 pump state 与 `/status show remote` 中记录 stream start/end/stop reason。完整云端协议 hardening 仍未完成。
- 本轮补充：remote poll/WebSocket 共用解码器现在兼容 `data`、`event`、`remote_event`、`delivery`、`payload` 包裹的单条事件，以及这些 wrapper 下的 `events/items/messages/deliveries` 列表；云端可以用 envelope 协议携带 cursor 和事件内容，而无需强制把事件字段铺在顶层。更深的鉴权刷新、ack/lease 和服务端协议协商仍未完成。
- 本轮补充：remote poll/WebSocket 事件现在会解析 `ack_url`/`ackUrl`/`acknowledge_url`/`receipt_url` 和 `lease_id`/`lease_expires_at` 及 `ack`/`lease` nested object 元数据；daemon 会在 delivered/duplicate/failed 后对注册 poll/websocket 同源的 ack URL 做 best-effort POST，带 Bearer auth 和 event/status/sent_count/duplicate/error payload，并对 transport error、408/429/5xx 做一次短退避重试；pump state、structured result 和 `/status show remote` 会记录 ack event/sent/error 与 lease event 计数；非同源 ack URL 会被拒绝且脱敏；已过期 lease 会被跳过投递并 ack `expired`，同时记录 `lease_expired_count`。更深的服务端协议协商仍未完成。
- 本轮补充：remote ack 和 lease renew 的 transient retry 现在会优先遵守服务端 `Retry-After` header（秒数或 HTTP-date），再回退到本地指数退避，并继续受最大退避上限约束，减少云端 429/503 限流时的协议偏差。
- 本轮补充：remote poll fetch 现在也支持 transient retry，遇到 transport error、408、429 或 5xx 时可按 PollOptions 重试，并同样优先遵守服务端 `Retry-After` header；PollResult 暴露 `attempt_count` 便于 daemon/pump 审计。
- 本轮补充：daemon remote poll 与 WebSocket fallback poll 现在实际启用 transient retry（一次短退避，优先遵守 `Retry-After`），并把 poll `attempt_count` 写入 structured result、`remote-pump.json` 和 `/status show remote`。
- 本轮补充：remote pump state 现在持久化 `attempt_count`，`/status show remote` 会显示 `attempts N`，让 poll/WebSocket pump 的重试行为能被 CLI 状态页审计。
- 本轮补充：remote WebSocket upgrade 失败后的重连退避现在会读取服务端 `Retry-After` header（秒数或 HTTP-date），Fetch/Stream 两条 WebSocket 路径都会优先遵守云端限流退避，再回退到本地指数退避。
- 本轮补充：remote WebSocket `FetchWebSocketEvents`/`StreamWebSocketEvents` result 现在暴露 `status_code` 和 `attempt_count`；成功握手记录 101，upgrade 失败记录服务端 HTTP status，daemon tick/常驻 stream 会把这些字段写入 `remote-pump.json`、structured result 和 `/status show remote` 审计面。
- 本轮补充：`StreamWebSocketEvents` 现在提供状态快照回调，daemon 常驻 WebSocket stream 会在握手成功、重连失败和每帧读取后实时刷新 `remote-pump.json` 的 status/attempt/frame/connect/reconnect/close 计数，运行中 `/status show remote` 不再等 stream 结束才看到这些审计字段。
- 本轮补充：daemon remote delivery 现在会对带 `lease_id` 且未过期的事件，在投递前向注册响应提供的同源 `lease_renew_url`/`lease_refresh_url` 做 best-effort POST，携带 Bearer auth、event_id 和 lease_id；renew 对 transport error、408/429/5xx 做一次短退避重试，成功/失败计数会写入 `remote-pump.json`、structured result 和 `/status show remote`。完整租约续期策略和云端协议演进策略仍未完成。

### M11: Bridge 和高级集成

产出：

- repl bridge、remote-control、session websocket、direct connect。
- LSP、IDE integration、Chrome native host、voice、computer-use、buddy、ultraplan。

验收：

- 每个 gated feature 独立开关测试。
- 不启用 feature 时二进制行为和可见 schema 不泄露 gated 工具/命令。

- 本轮补充：新增 `advanced` settings gate 地基，覆盖 bridge/LSP/telemetry/Chrome/voice/computer-use/native integrations 的独立 bool 开关解析、settings merge、headless `/config show advanced` 和 `/config search` 审计；`advanced.telemetry=true` 时会在 session 目录写入安全摘要 JSONL 诊断事件，记录事件类型、session/model、tool/progress keys、token/compact/error 摘要，不写入用户/助手正文或工具结果内容，并已有安全 JSONL 读取、类型/模型过滤、汇总统计、deterministic trace/span id、JSON summary export、可配置 `telemetryExport` 本地 JSONL exporter / HTTP backend POST、endpoint 脱敏 status，以及 headless `/status show telemetry` trace/span/exporter 审计地基；`advanced.lsp=true` 时才向模型暴露只读 `LSPDiagnostics` 工具，用于读取 session-scoped diagnostics snapshot，并支持 file/severity/limit 过滤，底层已能解析 LSP `textDocument/publishDiagnostics` params/notification payload、按文件替换 snapshot、按 LSP 空 diagnostics 语义清空旧文件诊断；同时会写出 session-scoped LSP manager status，按默认或显式 server definitions 解析 workspace/root marker/file extension 命中情况，将已匹配但未启动的 server 标为 `not_started`，并可通过 headless `/status show lsp` 审计 diagnostics 与 manager runtime state；LSP Content-Length framed JSON-RPC reader/writer 与 diagnostics stream processor 已能持续消费 `textDocument/publishDiagnostics` notification、捕获 initialize response capabilities 并更新 session snapshot；LSP server process lifecycle API 已可启动配置命令、消费 stdout framed diagnostics stream，并把 `running`/`exited`/`failed`、PID 和时间戳写回 manager status；LSP client handshake send path 已可向 server stdin 写入 `initialize`、`initialized` 和 `textDocument/didOpen` framed JSON-RPC，支持 root/client/document metadata 和 file URI/language 默认推断；conversation runner 在 `advanced.lsp=true` 且显式或默认 server definitions 命中 workspace 时，会自动启动可执行命令存在的 LSP server、发送 handshake/startup documents、对缺失命令保持可审计 `not_started` reason，并复用 session diagnostics/manager status 生命周期；`advanced.bridge=true` 时会写出 session-scoped bridge manifest 和 direct connect state，列出 bridge-safe slash/local command 元数据，启动 loopback-only direct HTTP/WebSocket endpoint，提供 `/health`、`/manifest`、`/resolve`、`/execute` 和 `/ws` JSON 通道，`/ws` 支持 `hello`/`health`/`manifest`/`resolve`/`execute`/`remote_trigger` action 与 protocol version/capability 握手，执行前强制 bridge-safe 白名单并默认不回传展开后的 prompt 正文，支持可选 bearer/`X-Bridge-Token` token guard，并可通过 headless `/status show bridge` 审计 manifest、HTTP URL、WebSocket URL 和 token-required 状态；`advanced.nativeIntegrations=true` 时会写出 session-scoped native capability manifest、native file index、native clipboard state 和 system/tmux/OSC52 clipboard adapter 检测结果，只记录路径/大小/mtime 等文件元数据、跳过常见 runtime/vendor 目录，clipboard status 不展示文本内容，并已有 ANSI color diff rendering runtime、system/tmux 外部剪贴板命令读写执行 API 和显式 `/native clipboard read|write` 用户路径，可通过 headless `/status show native` 审计 index/clipboard 路径、数量与 adapter 命令计划；任一 `advanced.chrome`/`advanced.voice`/`advanced.computerUse=true` 时会写出 session-scoped integrations manifest 和独立 runtime state file，将 Chrome/voice/computer-use 的 enabled 与 `runtime_state=ready` 分开记录，并记录 browser/native-host、audio-capture、screen-capture/input-control adapter 探测结果；`advanced.chrome=true` 时还会写出 session-scoped Chrome native host manifest 与 wrapper artifact，并已有 `--chrome-native-host` fast path、Chrome native messaging length-prefixed JSON frame 编解码和 ping/status/hello/capabilities/session/runtime 响应，以及显式 `/native chrome status|install` 用户路径，可将 manifest/wrapper 安装到 macOS/Linux Chrome/Chromium/Edge NativeMessagingHosts 目录，并在 Windows 通过 HKCU NativeMessagingHosts 注册表项注册 manifest；`advanced.voice=true` 时会写出 session-scoped voice capture plan artifact，记录选中的 audio-capture adapter、16kHz/mono/pcm_s16le/streaming 参数，并已有受 duration/max-bytes 限制的 voice capture command runner API、可配置 `CLAUDE_VOICE_TRANSCRIBE_COMMAND` 的 STT command runner，以及显式 `/native voice capture|transcribe` 用户路径；`advanced.computerUse=true` 时会写出 session-scoped computer-use driver plan artifact，记录 screen-capture/input-control adapter、png 截图格式和 screen_pixels 坐标系，并已有 screen capture stdout 捕获、显式 `/native computer screenshot` 与 `/native computer move|click|type|key` 用户路径，以及 xdotool、macOS osascript、Windows PowerShell 输入执行 API，可通过 headless `/status show integrations` 审计 runtime state 路径与 adapter/manifest/voice/computer-use plan 命令计划；未启用时仍不注册或泄露 gated 工具 schema。实际 Chrome 扩展 UI/事件通道、voice 实时/服务端 STT 体验、computer-use Wayland/权限边界与更深交互接入等平台增强仍未完成。

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
