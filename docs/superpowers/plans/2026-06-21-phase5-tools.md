# Tools (Phase 5) Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Bring ccgo's tool *behavior* to Claude Code parity: replace the one-line Bash/PowerShell prompt stubs with the full CC prompts (git/PR workflow, quoting, tool-preference, banned commands); make WebFetch summarize fetched content with a small/fast secondary model; make WebSearch use the official `web_search_20250305` server tool instead of scraping DuckDuckGo; add the `AskUserQuestion`, `EnterPlanMode`, and `ExitPlanMode` interactive tools behind the existing `Executor.Asker` dialog seam (their UI ceremony lands in Phase 2); grow `LSPDiagnostics` into the 9-operation `LSP` tool; persist Bash working directory across calls; and migrate `TodoWrite` to the `activeForm` schema.

**Architecture:** ccgo tools are values of `tool.FuncTool` (`internal/tool/func_tool.go:18`) built by `New<Name>Tool()` constructors in `internal/tools/<domain>/`. Each carries `PromptFunc`, `InputSchema`, `ValidateFunc`, `PermissionFunc`, `CallFunc`, and the read-only/concurrency/destructive predicates. The agent loop invokes them through `tool.Executor.Execute` (`internal/tool/executor.go:42`), which already consults the Phase-1 `Executor.Asker` seam (`executor.go:95-144`, `tool/types.go:39-51`) when a tool's permission decision is `PermissionAsk`. This phase therefore (1) swaps prompt strings for real composed prompts, (2) adds an injected **secondary-model client seam** to the web package (mirroring the existing `MetadataWebSearchEndpointKey` test-injection pattern, `web_search.go:20`) so WebFetch summarization and WebSearch-as-server-tool stay off the network in tests, (3) extends `anthropic.Request`/`anthropic.ToolDefinition` and `contracts.ContentBlockType` to carry the server-side web-search tool and its `web_search_tool_result` blocks, (4) adds the three interactive tools that return `PermissionAsk` from `CheckPermissions` so the executor's Asker renders the dialog (Phase 2 supplies the rich dialog; here we cover the tool + behavior + a yes/no fallback), (5) replaces `LSPDiagnostics` with a discriminated-union `LSP` tool, (6) threads a persisted cwd through `tool.Context.WorkingDirectory`, and (7) changes the `TodoWrite` schema and `Todo` struct.

**Tech Stack:** Go 1.26; module `ccgo`. Existing packages: `internal/tool`, `internal/tools/{bash,powershell,web,lsp,todo}`, `internal/contracts`, `internal/api/anthropic`, `internal/conversation`, `internal/model`, `internal/lsp`, `internal/permissions`. **No new third-party deps** (HTML→text already lives in `web_fetch.go`; no Turndown/markdown lib needed).

## Global Constraints

Copied verbatim from the master roadmap §6:

- **Module/toolchain:** `ccgo`, `go 1.26` (from `go.mod`).
- **Immutability (CRITICAL):** never mutate shared structs in place; return new copies. Copy the `conversation.Runner` value per turn before setting `OnEvent`/`Tools.Asker` (existing pattern). `permissions.Engine.ApplyUpdate` already returns a **new** engine — honor that.
- **Many small files:** one responsibility per file; target 150–350 lines (800 hard max).
- **Errors handled explicitly at every level; never swallow.** Terminal raw-mode `restore` and any acquired resource MUST be released on every exit path (`defer`).
- **Input validation at boundaries:** validate all external data (API responses, user input, file content, MCP server output); fail fast with clear messages.
- **No new third-party deps** unless the plan justifies it explicitly. Phase 1 added only `golang.org/x/term`. No bubbletea/tcell/charm.
- **Non-TTY safety:** interactive paths MUST NOT call `term.MakeRaw` when stdin/stdout isn't a tty; fall back to line mode. Tests MUST NOT depend on a real tty.
- **TDD:** every task writes a failing test first, then minimal code. Commit after each task. Run package tests with `go test ./internal/<pkg>/ -run TestName -v`; full suite `go test ./...`.
- **Verify against real code, distrust roadmap docs:** every assumed type name, field, constant, or CC behavior MUST be confirmed with `go doc`/`grep` (ccgo side) or by reading `/Users/sqlrush/agent/claude-code/src` (CC side) before writing the test — flag the exact command at the point of use, as Phase 1's plan does.
- **Security:** no hardcoded secrets; tokens in keychain not plaintext (Phase 4); sandbox flag must actually enforce (Phase 7); never leak sensitive data in errors.

Phase-5-specific constraints:

- **No network in tests.** WebFetch and WebSearch tests MUST inject a fake HTTP client (via `httptest.NewServer` + the existing metadata endpoint key) and a fake secondary-model client (new seam in Task 3). Confirm the existing pattern with `grep -n "httptest.NewServer\|MetadataWebSearchEndpointKey" internal/tools/web/web_search_test.go` before writing — it is already used heavily.
- **Cross-phase dependency on Phase 2 (dialogs):** Tasks 5 and 6 add tools that return `PermissionAsk`, routed through `Executor.Asker` (Phase 1). The *rich* dialogs (multi-question chips for AskUserQuestion, plan-approval ceremony for ExitPlanMode) are Phase 2's `internal/tui` work. Here the tool lands fully + a **headless/yes-no fallback** through the existing single-decision `PermissionAsker`; the richer `Asker` surface is added as a *new optional interface* (Task 5) so Phase 2 can implement it without breaking Phase 1's `loopAsker`.

---

## File Structure

**New files:**
- `internal/tools/bash/prompt.go` — `BashPrompt(ctx tool.PromptContext) (string, error)` + section builders (git/PR/quoting/tool-preference/instructions).
- `internal/tools/bash/prompt_test.go`
- `internal/tools/powershell/prompt.go` — `PowerShellPrompt(ctx) (string, error)` + edition/quoting/cmdlet-preference sections.
- `internal/tools/powershell/prompt_test.go`
- `internal/tools/web/model_client.go` — `SecondaryModelClient` interface + metadata key + extractor.
- `internal/tools/web/summarize.go` — WebFetch secondary-model summarization.
- `internal/tools/web/server_search.go` — WebSearch via the `web_search_20250305` server tool.
- `internal/tools/plan/tools.go` — `NewEnterPlanModeTool()`, `NewExitPlanModeTool()`, plan-file read/write helpers.
- `internal/tools/plan/tools_test.go`
- `internal/tools/ask/tools.go` — `NewAskUserQuestionTool()` + the `QuestionAsker` seam.
- `internal/tools/ask/tools_test.go`
- `internal/tools/lsp/lsp_tool.go` — the 9-op `LSP` discriminated-union tool.
- `internal/tools/lsp/lsp_tool_test.go`

**Modified files:**
- `internal/tools/bash/tools.go` — wire `PromptFunc` to `BashPrompt`; thread persisted cwd.
- `internal/tools/powershell/tools.go` — wire `PromptFunc` to `PowerShellPrompt`.
- `internal/tools/web/web_fetch.go` — call summarization in `prepareWebFetchResult`/`callWebFetch`.
- `internal/tools/web/web_search.go` — route to `server_search.go` when a model client is present; keep scrape as fallback.
- `internal/tool/types.go` — extend `PermissionAsker` with an optional `QuestionAsker` interface (additive).
- `internal/api/anthropic/types.go` — add server-tool fields to `ToolDefinition` (or a `ServerToolDefinition`) + `Request.Tools` carry.
- `internal/contracts/messages.go` — add `ContentServerToolUse` + `ContentWebSearchToolResult` block types.
- `internal/tools/todo/state.go`, `internal/tools/todo/tools.go` — `activeForm` schema; drop `priority`.
- `internal/tool/types.go` (Context) — add `WorkingDirectory` persistence note (already a field; Task 8 adds a session-scoped cwd store).

---

## Task 1: Full Bash tool prompt

**Files:**
- Create: `internal/tools/bash/prompt.go`
- Test: `internal/tools/bash/prompt_test.go`
- Modify: `internal/tools/bash/tools.go` (line 816-818 `PromptFunc`)

**Interfaces:**
- Produces: `func BashPrompt(ctx tool.PromptContext) (string, error)`; unexported section builders `bashToolPreferenceSection()`, `bashInstructionsSection()`, `bashGitSection()`, `bashPRSection()`.

**CC reference (read first):** `/Users/sqlrush/agent/claude-code/src/tools/BashTool/prompt.ts` — `getSimplePrompt()` (lines 275–369); git/PR via `getCommitAndPRInstructions()` (lines 42–161). Verbatim headings to reproduce: `# Committing changes with git` (prompt.ts:81), `Git Safety Protocol:` (prompt.ts:87), `# Creating pull requests` (prompt.ts:127), `# Other common operations` (prompt.ts:159), `# Instructions` (prompt.ts:364). Banned commands (prompt.ts:293-295): `find`, `grep`, `cat`, `head`, `tail`, `sed`, `awk`, `echo`. Tool preferences (prompt.ts:280-291): Glob > find/ls, Grep > grep/rg, Read > cat/head/tail, Edit > sed/awk, Write > echo/heredoc. Quoting rule (prompt.ts:333): `Always quote file paths that contain spaces with double quotes`.

- [ ] **Step 1: Confirm the current stub before changing it**

Run:
```bash
grep -n "Runs a shell command in the current working directory" internal/tools/bash/tools.go
grep -n "PromptFunc:" internal/tools/bash/tools.go
go doc ./internal/tool PromptContext
```
Expected: the one-line stub at tools.go:816-818; `PromptContext{Model, WorkingDirectory, Metadata}` confirmed. **Flag:** if `PromptContext` field names differ, adjust `BashPrompt`'s signature accordingly.

- [ ] **Step 2: Write the failing test**

Create `internal/tools/bash/prompt_test.go`:
```go
package bashtools

import (
	"strings"
	"testing"

	"ccgo/internal/tool"
)

func TestBashPromptHasCoreSections(t *testing.T) {
	got, err := BashPrompt(tool.PromptContext{WorkingDirectory: "/repo"})
	if err != nil {
		t.Fatalf("BashPrompt err: %v", err)
	}
	for _, want := range []string{
		"Executes a given bash command",
		"# Committing changes with git",
		"# Creating pull requests",
		"# Instructions",
		"Glob",  // tool preference
		"Grep",
		"Read",
		"quote file paths that contain spaces",
	} {
		if !strings.Contains(got, want) {
			t.Fatalf("BashPrompt missing %q", want)
		}
	}
	// Banned-command guidance must name the dedicated-tool fallbacks.
	for _, banned := range []string{"`find`", "`grep`", "`cat`", "`head`", "`tail`", "`sed`", "`awk`", "`echo`"} {
		if !strings.Contains(got, banned) {
			t.Fatalf("BashPrompt missing banned-command mention %q", banned)
		}
	}
	if len(got) < 1500 {
		t.Fatalf("BashPrompt too short (%d chars); expected the full prompt", len(got))
	}
}
```

- [ ] **Step 3: Run test to verify it fails**

Run: `go test ./internal/tools/bash/ -run TestBashPrompt -v`
Expected: FAIL — `undefined: BashPrompt`.

- [ ] **Step 4: Write minimal implementation**

Create `internal/tools/bash/prompt.go`:
```go
package bashtools

import (
	"strings"

	"ccgo/internal/tool"
)

// BashPrompt composes the full Bash tool prompt, mirroring Claude Code's
// getSimplePrompt() (src/tools/BashTool/prompt.ts:275-369). The git/PR
// workflow, quoting rules, tool-preference guidance, and banned-command list
// are reproduced so model behavior matches CC.
func BashPrompt(_ tool.PromptContext) (string, error) {
	var b strings.Builder
	b.WriteString("Executes a given bash command and returns its output. ")
	b.WriteString("The command runs in a persistent shell session in the current working directory.\n\n")
	b.WriteString(bashToolPreferenceSection())
	b.WriteString("\n")
	b.WriteString(bashInstructionsSection())
	b.WriteString("\n")
	b.WriteString(bashGitSection())
	b.WriteString("\n")
	b.WriteString(bashPRSection())
	return strings.TrimRight(b.String(), "\n"), nil
}

func bashToolPreferenceSection() string {
	return strings.Join([]string{
		"IMPORTANT: Avoid using this tool to run `find`, `grep`, `cat`, `head`, `tail`, `sed`, `awk`, or `echo` commands, unless explicitly instructed or after you have verified that a dedicated tool cannot accomplish your task. Instead, use the appropriate dedicated tool:",
		"- File search: Use Glob (NOT find or ls)",
		"- Content search: Use Grep (NOT grep or rg)",
		"- Read files: Use Read (NOT cat/head/tail)",
		"- Edit files: Use Edit (NOT sed/awk)",
		"- Write files: Use Write (NOT echo >/cat <<EOF)",
		"- Communication: Output text directly (NOT echo/printf)",
		"",
	}, "\n")
}

func bashInstructionsSection() string {
	return strings.Join([]string{
		"# Instructions",
		"- Before running the command, verify the directory exists.",
		"- Always quote file paths that contain spaces with double quotes (e.g., cd \"path with spaces/file.txt\").",
		"- Avoid using `cd` to change directories; prefer absolute paths.",
		"- Use the optional timeout parameter (milliseconds) for long-running commands.",
		"- Use run_in_background: true for commands that should not block the turn.",
		"- Chain dependent commands with `&&`; use separate calls for independent ones.",
		"",
	}, "\n")
}

func bashGitSection() string {
	return strings.Join([]string{
		"# Committing changes with git",
		"Git Safety Protocol: Only commit when the user explicitly asks. Never push without being asked.",
		"1. Run `git status`, `git diff`, and `git log` (in parallel) to understand the changes.",
		"2. Stage relevant files and write a concise commit message describing why the change was made.",
		"3. Use a HEREDOC for the message body to ensure correct formatting:",
		"   git commit -m \"$(cat <<'EOF'",
		"   <type>: <description>",
		"   EOF",
		"   )\"",
		"4. Confirm the commit succeeded with `git status`.",
		"",
	}, "\n")
}

func bashPRSection() string {
	return strings.Join([]string{
		"# Creating pull requests",
		"Use the `gh` CLI for all GitHub operations.",
		"1. Review the full branch state: `git status`, `git diff [base-branch]...HEAD`, and `git log`.",
		"2. Push with `-u` if the branch is new.",
		"3. Create the PR with a HEREDOC body:",
		"   gh pr create --title \"...\" --body \"$(cat <<'EOF'",
		"   ## Summary",
		"   ## Test plan",
		"   EOF",
		"   )\"",
		"",
		"# Other common operations",
		"- View PR comments: gh api ...",
		"",
	}, "\n")
}
```

Wire it in `internal/tools/bash/tools.go`. Replace the stub `PromptFunc` (tools.go:816-818):
```go
		PromptFunc: BashPrompt,
```

- [ ] **Step 5: Run tests to verify they pass**

Run: `go test ./internal/tools/bash/ -v`
Expected: PASS, including pre-existing Bash tests (only the prompt string changed).

- [ ] **Step 6: Commit**

```bash
git add internal/tools/bash/prompt.go internal/tools/bash/prompt_test.go internal/tools/bash/tools.go
git commit -m "feat(tools): replace Bash prompt stub with full CC prompt (git/PR/quoting/tool-preference)"
```

---

## Task 2: Full PowerShell tool prompt

**Files:**
- Create: `internal/tools/powershell/prompt.go`
- Test: `internal/tools/powershell/prompt_test.go`
- Modify: `internal/tools/powershell/tools.go` (line 95-96 `PromptFunc`)

**Interfaces:**
- Produces: `func PowerShellPrompt(ctx tool.PromptContext) (string, error)`.

**CC reference (read first):** `/Users/sqlrush/agent/claude-code/src/tools/PowerShellTool/prompt.ts` — `getPrompt()` (lines 73–145). Edition note (`getEditionSection()`, lines 51-71): on 5.1 warn `&&`/`||`/`?:`/`??`/`?.` unavailable, use `A; if ($?) { B }`. Quoting/syntax (lines 93-103): `$` vars, backtick escape (not backslash), Verb-Noun cmdlets, `@'...'@` here-strings, `HKLM:`/`HKCU:` drives, `$env:NAME`, call operator `&`, stop-parsing `--%`. Non-interactive guards (lines 105-108): never `Read-Host`/`Get-Credential`/`Out-GridView`; add `-Confirm:$false`. Cmdlet preferences (lines 127-133): Glob > `Get-ChildItem -Recurse`, Grep > `Select-String`, Read > `Get-Content`, Write > `Set-Content/Out-File`. **No git/PR ceremony block** (only a short safety list, lines 141-144).

- [ ] **Step 1: Confirm the current stub**

Run: `grep -n "Runs a PowerShell command in the current working directory" internal/tools/powershell/tools.go`
Expected: the one-line stub at tools.go:95-96.

- [ ] **Step 2: Write the failing test**

Create `internal/tools/powershell/prompt_test.go`:
```go
package powershelltools

import (
	"strings"
	"testing"

	"ccgo/internal/tool"
)

func TestPowerShellPromptHasCoreSections(t *testing.T) {
	got, err := PowerShellPrompt(tool.PromptContext{})
	if err != nil {
		t.Fatalf("PowerShellPrompt err: %v", err)
	}
	for _, want := range []string{
		"PowerShell",
		"Verb-Noun",
		"backtick",            // escape rule
		"$env:",               // env var syntax
		"-NonInteractive",
		"Read-Host",           // forbidden interactive cmdlet
		"Glob",                // cmdlet preference
		"Select-String",
		"-Confirm:$false",
	} {
		if !strings.Contains(got, want) {
			t.Fatalf("PowerShellPrompt missing %q", want)
		}
	}
	if strings.Contains(got, "# Creating pull requests") {
		t.Fatal("PowerShell prompt must NOT include the Bash git/PR ceremony")
	}
}
```

- [ ] **Step 3: Run test to verify it fails**

Run: `go test ./internal/tools/powershell/ -run TestPowerShellPrompt -v`
Expected: FAIL — `undefined: PowerShellPrompt`.

- [ ] **Step 4: Write minimal implementation**

Create `internal/tools/powershell/prompt.go`:
```go
package powershelltools

import (
	"strings"

	"ccgo/internal/tool"
)

// PowerShellPrompt composes the full PowerShell tool prompt, mirroring CC's
// getPrompt() (src/tools/PowerShellTool/prompt.ts:73-145). Unlike Bash it has
// no git/PR ceremony, and it adds Windows quoting + non-interactive guards.
func PowerShellPrompt(_ tool.PromptContext) (string, error) {
	return strings.TrimRight(strings.Join([]string{
		"Runs a PowerShell command and returns its output. The command runs non-interactively in the current working directory.",
		"DO NOT use it for file operations that have a dedicated tool — use the specialized tools.",
		"",
		"# Syntax and quoting",
		"- Cmdlets follow Verb-Noun naming (e.g., Get-ChildItem).",
		"- Escape characters with a backtick (`), NOT a backslash.",
		"- Reference variables with a $ prefix; environment variables as $env:NAME.",
		"- Use single-quoted here-strings @'...'@ with the closing '@ at column 0.",
		"- Invoke quoted executable paths with the call operator: & \"C:\\Program Files\\app.exe\".",
		"- Windows PowerShell 5.1 does not support && / || / ?: / ?? / ?.; use A; if ($?) { B } instead.",
		"",
		"# Non-interactive guards",
		"- The shell runs with -NonInteractive; never use Read-Host, Get-Credential, Out-GridView, or pause.",
		"- Add -Confirm:$false to destructive cmdlets so they do not block on confirmation.",
		"",
		"# Tool preferences",
		"- File search: Use Glob (NOT Get-ChildItem -Recurse)",
		"- Content search: Use Grep (NOT Select-String)",
		"- Read files: Use Read (NOT Get-Content)",
		"- Write files: Use Write (NOT Set-Content/Out-File)",
		"- Communication: Output text directly (NOT Write-Output/Write-Host)",
		"",
		"# Instructions",
		"- Do NOT prefix commands with cd or Set-Location; the cwd is already set.",
		"- For git commands, only commit/push when explicitly asked.",
	}, "\n"), "\n"), nil
}
```

Wire it in `internal/tools/powershell/tools.go`. Replace the stub `PromptFunc` (tools.go:95-97):
```go
		PromptFunc: PowerShellPrompt,
```

- [ ] **Step 5: Run tests to verify they pass**

Run: `go test ./internal/tools/powershell/ -v`
Expected: PASS.

- [ ] **Step 6: Commit**

```bash
git add internal/tools/powershell/prompt.go internal/tools/powershell/prompt_test.go internal/tools/powershell/tools.go
git commit -m "feat(tools): replace PowerShell prompt stub with full CC prompt (quoting/guards/cmdlet-preference)"
```

---

## Task 3: WebFetch secondary-model summarization

**Files:**
- Create: `internal/tools/web/model_client.go`
- Create: `internal/tools/web/summarize.go`
- Modify: `internal/tools/web/web_fetch.go` (`callWebFetch`, line 149; `prepareWebFetchResult`, line 379; `PromptFunc`, line 79-81)
- Test: `internal/tools/web/summarize_test.go`

**Interfaces:**
- Produces:
  - `type SecondaryModelClient interface { Summarize(ctx context.Context, req SummarizeRequest) (string, error) }`
  - `type SummarizeRequest struct { Model, SystemPrompt, Content, Prompt string }`
  - `const MetadataSecondaryModelClientKey = "ccgo.tools.web.secondary_model"`
  - `func secondaryModelClient(metadata map[string]any) SecondaryModelClient`
  - `func makeSecondaryModelPrompt(content, prompt string) string`

**CC reference (read first):** `/Users/sqlrush/agent/claude-code/src/tools/WebFetchTool/utils.ts:484-530` (`applyPromptToMarkdown` → `queryHaiku(... querySource:'web_fetch_apply')`, single non-streaming completion, reads `content[0].text`); prompt template `/Users/sqlrush/agent/claude-code/src/tools/WebFetchTool/prompt.ts:23-46` (`makeSecondaryModelPrompt`: `Web page content:\n---\n${markdownContent}\n---\n\n${prompt}\n\n${guidelines}`, 125-char quote cap); content cap `MAX_MARKDOWN_LENGTH = 100_000` (utils.ts:128); 15-min cache `CACHE_TTL_MS` (utils.ts:63) — **note:** the 15-min cache is P1 polish; this task does the summarization, the cache is optional and flagged below.

**ccgo small-model constant:** confirm with `grep -n "Claude45Haiku" internal/model/model.go` → `model.Claude45Haiku = "claude-haiku-4-5-20251001"`.

- [ ] **Step 1: Confirm injection pattern + no existing model seam**

Run:
```bash
grep -n "MetadataWebSearchEndpointKey\|httptest.NewServer" internal/tools/web/web_search_test.go
grep -rn "SecondaryModel\|queryHaiku\|Summarize" internal/tools/web/
go doc ./internal/model | grep -i haiku
```
Expected: endpoint-key + httptest injection pattern exists; **no** summarization seam yet; `Claude45Haiku` constant present. **Flag:** if `model.Claude45Haiku` is named differently, use the confirmed identifier.

- [ ] **Step 2: Write the failing test**

Create `internal/tools/web/summarize_test.go`:
```go
package webtools

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"ccgo/internal/tool"
)

type fakeSummarizer struct {
	gotContent string
	gotPrompt  string
	reply      string
}

func (f *fakeSummarizer) Summarize(_ context.Context, req SummarizeRequest) (string, error) {
	f.gotContent = req.Content
	f.gotPrompt = req.Prompt
	return f.reply, nil
}

func TestWebFetchSummarizesWithSecondaryModel(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "text/html")
		_, _ = w.Write([]byte("<html><body><p>The release ships on Tuesday.</p></body></html>"))
	}))
	defer server.Close()

	sum := &fakeSummarizer{reply: "Ships Tuesday."}
	toolImpl := NewWebFetchTool()
	raw, _ := json.Marshal(map[string]any{"url": server.URL, "prompt": "When does it ship?"})
	ctx := tool.Context{
		Context: context.Background(),
		Metadata: map[string]any{
			MetadataWebFetchSkipPreflightKey: true,
			MetadataSecondaryModelClientKey:  sum,
		},
	}
	res, err := toolImpl.Call(ctx, raw, tool.NopProgressSink())
	if err != nil {
		t.Fatalf("Call err: %v", err)
	}
	if sum.gotPrompt != "When does it ship?" {
		t.Fatalf("summarizer prompt = %q", sum.gotPrompt)
	}
	if !strings.Contains(sum.gotContent, "ships on Tuesday") {
		t.Fatalf("summarizer did not receive rendered content: %q", sum.gotContent)
	}
	content, _ := res.Content.(string)
	if !strings.Contains(content, "Ships Tuesday.") {
		t.Fatalf("result missing model summary: %q", content)
	}
}

func TestMakeSecondaryModelPromptStructure(t *testing.T) {
	got := makeSecondaryModelPrompt("BODY", "QUESTION")
	if !strings.Contains(got, "Web page content:") || !strings.Contains(got, "BODY") || !strings.Contains(got, "QUESTION") {
		t.Fatalf("prompt structure wrong: %q", got)
	}
}
```

- [ ] **Step 3: Run test to verify it fails**

Run: `go test ./internal/tools/web/ -run 'TestWebFetchSummarizes|TestMakeSecondary' -v`
Expected: FAIL — `undefined: MetadataSecondaryModelClientKey` / `undefined: makeSecondaryModelPrompt`.

- [ ] **Step 4: Write minimal implementation**

Create `internal/tools/web/model_client.go`:
```go
package webtools

import "context"

// MetadataSecondaryModelClientKey injects the small/fast model client WebFetch
// uses to summarize rendered content against the user's prompt. Absent → no
// summarization (raw rendered text is returned, preserving today's behavior).
const MetadataSecondaryModelClientKey = "ccgo.tools.web.secondary_model"

// SecondaryModelClient runs a single non-streaming completion. Mirrors CC's
// queryHaiku (src/tools/WebFetchTool/utils.ts:503).
type SecondaryModelClient interface {
	Summarize(ctx context.Context, req SummarizeRequest) (string, error)
}

type SummarizeRequest struct {
	Model        string
	SystemPrompt string
	Content      string
	Prompt       string
}

func secondaryModelClient(metadata map[string]any) SecondaryModelClient {
	if metadata == nil {
		return nil
	}
	client, _ := metadata[MetadataSecondaryModelClientKey].(SecondaryModelClient)
	return client
}
```

Create `internal/tools/web/summarize.go`:
```go
package webtools

import (
	"context"
	"strings"
	"unicode/utf8"
)

// maxSummarizeMarkdown caps content sent to the secondary model
// (CC MAX_MARKDOWN_LENGTH = 100_000, utils.ts:128).
const maxSummarizeMarkdown = 100_000

// secondaryModelName is the small/fast model WebFetch summarizes with.
// Confirm with: grep -n "Claude45Haiku" internal/model/model.go
const secondaryModelName = "claude-haiku-4-5-20251001"

const summarizeSystemPrompt = "You are summarizing web page content to answer a specific question. Be concise and factual; quote at most 125 characters at a time."

// makeSecondaryModelPrompt mirrors CC makeSecondaryModelPrompt (prompt.ts:23-46).
func makeSecondaryModelPrompt(content, prompt string) string {
	var b strings.Builder
	b.WriteString("Web page content:\n---\n")
	b.WriteString(truncateMarkdown(content))
	b.WriteString("\n---\n\n")
	b.WriteString(prompt)
	b.WriteString("\n\nProvide a focused answer. Use a strict 125-character maximum for any direct quotes.")
	return b.String()
}

func truncateMarkdown(content string) string {
	if utf8.RuneCountInString(content) <= maxSummarizeMarkdown {
		return content
	}
	return string([]rune(content)[:maxSummarizeMarkdown])
}

// summarizeWebFetch returns the model summary, or "" if no client/empty input.
func summarizeWebFetch(ctx context.Context, client SecondaryModelClient, content, prompt string) (string, error) {
	if client == nil || strings.TrimSpace(content) == "" || strings.TrimSpace(prompt) == "" {
		return "", nil
	}
	if ctx == nil {
		ctx = context.Background()
	}
	return client.Summarize(ctx, SummarizeRequest{
		Model:        secondaryModelName,
		SystemPrompt: summarizeSystemPrompt,
		Content:      content,
		Prompt:       makeSecondaryModelPrompt(content, prompt),
	})
}
```

In `internal/tools/web/web_fetch.go`, add a `Summary` field to `fetchResult` (after `PromptExcerpt`, line ~205) and populate it in `callWebFetch` after `prepareWebFetchResult` (line 164). Replace the body of `callWebFetch` from line 164 onward:
```go
	result = prepareWebFetchResult(result, input.Prompt)
	if !result.Binary && !result.RedirectDetected {
		body := result.RenderedBody
		if body == "" {
			body = result.Body
		}
		summary, sumErr := summarizeWebFetch(ctx.Context, secondaryModelClient(ctx.Metadata), body, input.Prompt)
		if sumErr != nil {
			return contracts.ToolResult{}, fmt.Errorf("web fetch summarization: %w", sumErr)
		}
		result.Summary = summary
	}
	content := formatWebFetchContent(input, result)
```
In `formatWebFetchContent` (line 397), when `result.Summary != ""` prefer it: insert before the "Relevant excerpt" branch (line ~429):
```go
	if result.Summary != "" {
		b.WriteString("\n\nSummary:\n")
		b.WriteString(result.Summary)
		return strings.TrimRight(b.String(), "\n")
	}
```
Add `"summary": result.Summary` to the `StructuredContent` map in `callWebFetch` (line 169). Update the `PromptFunc` (line 79-81) to state summarization IS implemented: replace the trailing `Browser rendering and model summarization are not implemented yet.` with `When a small fast model is configured, the rendered content is summarized against your prompt.`

- [ ] **Step 5: Run tests to verify they pass**

Run: `go test ./internal/tools/web/ -v`
Expected: PASS, including pre-existing WebFetch tests (no metadata client → `summarizeWebFetch` returns "", behavior unchanged).

- [ ] **Step 6: Commit**

```bash
git add internal/tools/web/model_client.go internal/tools/web/summarize.go internal/tools/web/web_fetch.go internal/tools/web/summarize_test.go
git commit -m "feat(tools): summarize WebFetch content with an injected small/fast model"
```

> **Deferred (P1, flagged):** the 15-minute self-cleaning URL cache (`CACHE_TTL_MS`, utils.ts:63) is not in this task — add it later only if WebFetch becomes a hot path.

---

## Task 4: WebSearch via the official `web_search_20250305` server tool

**Files:**
- Create: `internal/tools/web/server_search.go`
- Modify: `internal/tools/web/web_search.go` (`callWebSearch`, line 167)
- Modify: `internal/api/anthropic/types.go` (add server-tool carry)
- Modify: `internal/contracts/messages.go` (add server block types)
- Test: `internal/tools/web/server_search_test.go`

**Interfaces:**
- Produces:
  - `const MetadataServerSearchClientKey = "ccgo.tools.web.server_search"`
  - `type ServerSearchClient interface { Search(ctx context.Context, req ServerSearchRequest) (ServerSearchResponse, error) }`
  - `type ServerSearchRequest struct { Query string; AllowedDomains, BlockedDomains []string; MaxUses int }`
  - `type ServerSearchResponse struct { Results []searchResult; Text string }`
  - `func serverSearchClient(metadata map[string]any) ServerSearchClient`
- Adds to `anthropic`: a `ServerToolDefinition` carried on `Request` (so the loop can attach `web_search_20250305`).
- Adds to `contracts`: `ContentServerToolUse ContentBlockType = "server_tool_use"`, `ContentWebSearchToolResult ContentBlockType = "web_search_tool_result"`.

**CC reference (read first):** `/Users/sqlrush/agent/claude-code/src/tools/WebSearchTool/WebSearchTool.ts:76-84` (`makeToolSchema` → `{ type:'web_search_20250305', name:'web_search', allowed_domains, blocked_domains, max_uses:8 }`); wired via `extraToolSchemas` (line 284); results parsed in `makeOutputFromSearchResponse` (lines 86-150) from `server_tool_use` (line 104) + `web_search_tool_result` (line 115, hits at 124) + interleaved `text` (line 131). `max_uses` hardcoded 8 (line 82).

- [ ] **Step 1: Confirm absence of server-tool carry**

Run:
```bash
grep -n "type ToolDefinition struct\|type Request struct" internal/api/anthropic/types.go
grep -rn "web_search_20250305\|server_tool_use\|ServerToolDefinition" internal/api/ internal/contracts/ internal/conversation/
grep -n "ContentServerToolUse\|ContentWebSearchToolResult" internal/contracts/messages.go
```
Expected: `Request` has only `Tools []ToolDefinition` (types.go:18); `ToolDefinition` has no `Type` field (types.go:40-48); **no** server-tool wiring anywhere; **no** server block-type constants. **Flag:** confirm the exact `Request`/`ToolDefinition` field set before editing.

- [ ] **Step 2: Write the failing test**

Create `internal/tools/web/server_search_test.go`:
```go
package webtools

import (
	"context"
	"encoding/json"
	"strings"
	"testing"

	"ccgo/internal/tool"
)

type fakeServerSearch struct {
	gotReq ServerSearchRequest
	resp   ServerSearchResponse
}

func (f *fakeServerSearch) Search(_ context.Context, req ServerSearchRequest) (ServerSearchResponse, error) {
	f.gotReq = req
	return f.resp, nil
}

func TestWebSearchUsesServerToolWhenConfigured(t *testing.T) {
	srv := &fakeServerSearch{resp: ServerSearchResponse{
		Results: []searchResult{{Title: "Go 1.26", URL: "https://go.dev/blog", Snippet: "release"}},
	}}
	toolImpl := NewWebSearchTool()
	raw, _ := json.Marshal(map[string]any{"query": "go 1.26 release", "allowed_domains": []string{"go.dev"}})
	ctx := tool.Context{
		Context:  context.Background(),
		Metadata: map[string]any{MetadataServerSearchClientKey: srv},
	}
	res, err := toolImpl.Call(ctx, raw, tool.NopProgressSink())
	if err != nil {
		t.Fatalf("Call err: %v", err)
	}
	if srv.gotReq.Query != "go 1.26 release" {
		t.Fatalf("server search query = %q", srv.gotReq.Query)
	}
	if srv.gotReq.MaxUses != serverSearchMaxUses {
		t.Fatalf("max_uses = %d want %d", srv.gotReq.MaxUses, serverSearchMaxUses)
	}
	if len(srv.gotReq.AllowedDomains) != 1 || srv.gotReq.AllowedDomains[0] != "go.dev" {
		t.Fatalf("allowed_domains = %v", srv.gotReq.AllowedDomains)
	}
	content, _ := res.Content.(string)
	if !strings.Contains(content, "Go 1.26") || !strings.Contains(content, "https://go.dev/blog") {
		t.Fatalf("result missing server search hit: %q", content)
	}
}
```

- [ ] **Step 3: Run test to verify it fails**

Run: `go test ./internal/tools/web/ -run TestWebSearchUsesServerTool -v`
Expected: FAIL — `undefined: MetadataServerSearchClientKey` / `undefined: serverSearchMaxUses`.

- [ ] **Step 4: Write minimal implementation**

First extend the API + contracts types.

In `internal/contracts/messages.go`, add to the `ContentBlockType` const block (after line 28):
```go
	ContentServerToolUse       ContentBlockType = "server_tool_use"
	ContentWebSearchToolResult ContentBlockType = "web_search_tool_result"
```

In `internal/api/anthropic/types.go`, add a server-tool definition + carry it on the request (additive; existing `Tools` untouched):
```go
// ServerToolDefinition is an Anthropic server-side tool (e.g. web search) that
// runs on the API rather than client-side. Mirrors BetaWebSearchTool20250305
// (CC WebSearchTool.ts:76-84).
type ServerToolDefinition struct {
	Type           string   `json:"type"`           // "web_search_20250305"
	Name           string   `json:"name"`           // "web_search"
	AllowedDomains []string `json:"allowed_domains,omitempty"`
	BlockedDomains []string `json:"blocked_domains,omitempty"`
	MaxUses        int      `json:"max_uses,omitempty"`
}
```
Add `ServerTools []ServerToolDefinition` to `Request` (after line 18) — wired into the JSON `tools` array at request-build time is Phase 3/loop work; for THIS task only the type is needed so `server_search.go` can build the request when a real client is supplied. **Flag:** the actual loop wiring (merging `ServerTools` into the outbound `tools` array + parsing `web_search_tool_result` from the stream) belongs to the conversation runner; this task adds the tool-side client seam and types, and an in-tool fallback. Note this cross-phase boundary in the Self-Review.

Create `internal/tools/web/server_search.go`:
```go
package webtools

import (
	"context"
	"strings"
)

// MetadataServerSearchClientKey injects the official web-search server-tool
// client. Absent → fall back to the HTML-scraping path (today's behavior).
const MetadataServerSearchClientKey = "ccgo.tools.web.server_search"

// serverSearchMaxUses mirrors CC's hardcoded max_uses (WebSearchTool.ts:82).
const serverSearchMaxUses = 8

// ServerSearchClient runs the web_search_20250305 server tool and returns the
// parsed hits plus any interleaved model text.
type ServerSearchClient interface {
	Search(ctx context.Context, req ServerSearchRequest) (ServerSearchResponse, error)
}

type ServerSearchRequest struct {
	Query          string
	AllowedDomains []string
	BlockedDomains []string
	MaxUses        int
}

type ServerSearchResponse struct {
	Results []searchResult
	Text    string
}

func serverSearchClient(metadata map[string]any) ServerSearchClient {
	if metadata == nil {
		return nil
	}
	client, _ := metadata[MetadataServerSearchClientKey].(ServerSearchClient)
	return client
}

func runServerSearch(ctx context.Context, client ServerSearchClient, input webSearchInput, limit int) (webSearchResult, error) {
	if ctx == nil {
		ctx = context.Background()
	}
	resp, err := client.Search(ctx, ServerSearchRequest{
		Query:          strings.TrimSpace(input.Query),
		AllowedDomains: webSearchAllowedDomains(input),
		BlockedDomains: webSearchBlockedDomains(input),
		MaxUses:        serverSearchMaxUses,
	})
	if err != nil {
		return webSearchResult{}, err
	}
	results := filterSearchResults(resp.Results, webSearchAllowedDomains(input), webSearchBlockedDomains(input), limit)
	return webSearchResult{Results: results, StatusCode: 200}, nil
}
```

In `internal/tools/web/web_search.go`, branch at the top of `callWebSearch` (line 167-177) to prefer the server tool:
```go
func callWebSearch(ctx tool.Context, raw json.RawMessage, _ tool.ProgressSink) (contracts.ToolResult, error) {
	input, err := decodeWebSearch(raw)
	if err != nil {
		return contracts.ToolResult{}, err
	}
	limit := webSearchLimit(input)
	if client := serverSearchClient(ctx.Metadata); client != nil {
		result, err := runServerSearch(ctx.Context, client, input, limit)
		if err != nil {
			return contracts.ToolResult{}, err
		}
		return webSearchToolResult(input, result), nil
	}
	endpoint := webSearchEndpoint(ctx.Metadata)
	result, err := runWebSearch(ctx.Context, endpoint, input, limit)
	if err != nil {
		return contracts.ToolResult{}, err
	}
	return webSearchToolResult(input, result), nil
}
```
Extract the existing `return contracts.ToolResult{...}` body (lines 178-191) into a shared `func webSearchToolResult(input webSearchInput, result webSearchResult) contracts.ToolResult`. Update the `PromptFunc` (line 120-122): replace `Official search backend parity is not implemented yet.` with `When the official web-search server tool is configured, results come directly from the API.`

- [ ] **Step 5: Run tests to verify they pass**

Run: `go test ./internal/tools/web/ ./internal/api/anthropic/ ./internal/contracts/ -v`
Expected: PASS, including the pre-existing DuckDuckGo-scrape tests (no server client → scrape fallback unchanged).

- [ ] **Step 6: Commit**

```bash
git add internal/tools/web/server_search.go internal/tools/web/web_search.go internal/api/anthropic/types.go internal/contracts/messages.go internal/tools/web/server_search_test.go
git commit -m "feat(tools): route WebSearch through the official web_search_20250305 server tool"
```

---

## Task 5: AskUserQuestion tool (via the dialog seam)

**Files:**
- Create: `internal/tools/ask/tools.go`
- Modify: `internal/tool/types.go` (add the `QuestionAsker` optional interface)
- Test: `internal/tools/ask/tools_test.go`

**Interfaces:**
- Produces: `func NewAskUserQuestionTool() tool.Tool`.
- Adds to `internal/tool/types.go` (additive — does NOT change the Phase-1 `PermissionAsker`):
  ```go
  type Question struct { Header, Question string; Options []QuestionOption; MultiSelect bool }
  type QuestionOption struct { Label, Description string }
  type QuestionAnswer struct { Header string; Selected []string }
  type QuestionAsker interface { AskQuestions(ctx context.Context, qs []Question) ([]QuestionAnswer, error) }
  ```
- `const MetadataQuestionAskerKey = "ccgo.tools.ask.asker"` — the tool reads the `QuestionAsker` from `ctx.Metadata`; absent → headless deny ("no interactive question handler available"). Phase 1's `loopAsker` (yes/no) is NOT a `QuestionAsker`; Phase 2 implements the chip dialog and injects it.

**CC reference (read first):** `/Users/sqlrush/agent/claude-code/src/tools/AskUserQuestionTool/AskUserQuestionTool.tsx:14-67`. Schema: `questions` array `.min(1).max(4)`; each question `{ question: string (ends with ?), header: string (max 12 chars), options: array .min(2).max(4) of { label (1-5 words), description, preview? }, multiSelect: bool default false }`. Question texts unique; option labels unique within a question. "Other" is auto-provided. Returns `User has answered your questions: ...`.

- [ ] **Step 1: Confirm the seam + chip-width constant**

Run:
```bash
grep -n "PermissionAsker\|QuestionAsker" internal/tool/types.go
grep -rn "AskUserQuestion" internal/ cmd/ | grep -v _test
```
Expected: `PermissionAsker` exists (types.go:49); **no** `QuestionAsker`; **no** AskUserQuestion tool anywhere. CC chip width = 12 (prompt.ts:5).

- [ ] **Step 2: Write the failing test**

Create `internal/tools/ask/tools_test.go`:
```go
package asktools

import (
	"context"
	"encoding/json"
	"strings"
	"testing"

	"ccgo/internal/tool"
)

type fakeQuestionAsker struct {
	got    []tool.Question
	answer []tool.QuestionAnswer
}

func (f *fakeQuestionAsker) AskQuestions(_ context.Context, qs []tool.Question) ([]tool.QuestionAnswer, error) {
	f.got = qs
	return f.answer, nil
}

func validAskInput() json.RawMessage {
	raw, _ := json.Marshal(map[string]any{
		"questions": []any{map[string]any{
			"header":   "Theme",
			"question": "Which theme do you want?",
			"options": []any{
				map[string]any{"label": "Dark", "description": "Dark UI"},
				map[string]any{"label": "Light", "description": "Light UI"},
			},
		}},
	})
	return raw
}

func TestAskUserQuestionValidatesSchema(t *testing.T) {
	toolImpl := NewAskUserQuestionTool()
	// Empty questions array → error.
	bad, _ := json.Marshal(map[string]any{"questions": []any{}})
	if err := toolImpl.Validate(tool.Context{Context: context.Background()}, bad); err == nil {
		t.Fatal("expected validation error for empty questions")
	}
	// Valid input passes.
	if err := toolImpl.Validate(tool.Context{Context: context.Background()}, validAskInput()); err != nil {
		t.Fatalf("valid input failed validation: %v", err)
	}
}

func TestAskUserQuestionCallsAsker(t *testing.T) {
	asker := &fakeQuestionAsker{answer: []tool.QuestionAnswer{{Header: "Theme", Selected: []string{"Dark"}}}}
	toolImpl := NewAskUserQuestionTool()
	ctx := tool.Context{
		Context:  context.Background(),
		Metadata: map[string]any{MetadataQuestionAskerKey: asker},
	}
	res, err := toolImpl.Call(ctx, validAskInput(), tool.NopProgressSink())
	if err != nil {
		t.Fatalf("Call err: %v", err)
	}
	if len(asker.got) != 1 || asker.got[0].Header != "Theme" {
		t.Fatalf("asker did not receive question: %+v", asker.got)
	}
	content, _ := res.Content.(string)
	if !strings.Contains(content, "Dark") {
		t.Fatalf("result missing answer: %q", content)
	}
}

func TestAskUserQuestionHeadlessDeny(t *testing.T) {
	toolImpl := NewAskUserQuestionTool()
	res, err := toolImpl.Call(tool.Context{Context: context.Background()}, validAskInput(), tool.NopProgressSink())
	if err == nil && !res.IsError {
		t.Fatal("expected error when no QuestionAsker is configured")
	}
}
```

- [ ] **Step 3: Run test to verify it fails**

Run: `go test ./internal/tools/ask/ -v`
Expected: FAIL — `undefined: NewAskUserQuestionTool` / `undefined: MetadataQuestionAskerKey` / `tool.Question undefined`.

- [ ] **Step 4: Write minimal implementation**

In `internal/tool/types.go`, add (after `PermissionAsker`, line 51):
```go
// Question / QuestionOption / QuestionAnswer model the AskUserQuestion tool.
type Question struct {
	Header      string
	Question    string
	Options     []QuestionOption
	MultiSelect bool
}

type QuestionOption struct {
	Label       string
	Description string
}

type QuestionAnswer struct {
	Header   string
	Selected []string
}

// QuestionAsker renders interactive multiple-choice questions. Phase 2's TUI
// implements it; headless callers leave it unset (the tool then errors).
type QuestionAsker interface {
	AskQuestions(ctx context.Context, questions []Question) ([]QuestionAnswer, error)
}
```

Create `internal/tools/ask/tools.go`:
```go
package asktools

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	"ccgo/internal/contracts"
	"ccgo/internal/tool"
)

const MetadataQuestionAskerKey = "ccgo.tools.ask.asker"

const (
	maxQuestions     = 4
	minOptions       = 2
	maxOptions       = 4
	maxHeaderChars   = 12
)

type askInput struct {
	Questions []askQuestion `json:"questions"`
}

type askQuestion struct {
	Header      string      `json:"header"`
	Question    string      `json:"question"`
	Options     []askOption `json:"options"`
	MultiSelect bool        `json:"multiSelect,omitempty"`
}

type askOption struct {
	Label       string `json:"label"`
	Description string `json:"description"`
}

func NewAskUserQuestionTool() tool.Tool {
	return tool.FuncTool{
		DefinitionValue: contracts.ToolDefinition{
			Name:                "AskUserQuestion",
			Description:         "Ask the user one to four multiple-choice questions.",
			RequiresInteraction: true,
			InputSchema: contracts.JSONSchema{
				"type":     "object",
				"required": []any{"questions"},
				"properties": map[string]any{
					"questions": map[string]any{
						"type":     "array",
						"minItems": 1,
						"maxItems": maxQuestions,
						"items": map[string]any{
							"type":     "object",
							"required": []any{"header", "question", "options"},
							"properties": map[string]any{
								"header":      map[string]any{"type": "string", "maxLength": maxHeaderChars},
								"question":    map[string]any{"type": "string"},
								"multiSelect": map[string]any{"type": "boolean"},
								"options": map[string]any{
									"type":     "array",
									"minItems": minOptions,
									"maxItems": maxOptions,
									"items": map[string]any{
										"type":     "object",
										"required": []any{"label", "description"},
										"properties": map[string]any{
											"label":       map[string]any{"type": "string"},
											"description": map[string]any{"type": "string"},
										},
									},
								},
							},
						},
					},
				},
			},
		},
		PromptFunc: func(tool.PromptContext) (string, error) {
			return "Asks the user 1-4 multiple-choice questions and waits for their selections. Each question has a short header (<=12 chars), the question text, and 2-4 options with a label and description. An 'Other' free-text option is always added automatically.", nil
		},
		ValidateFunc: validateAsk,
		PermissionFunc: func(tool.Context, json.RawMessage) (contracts.PermissionDecision, error) {
			// Always allow: the interaction itself is the user's consent.
			return contracts.PermissionDecision{Behavior: contracts.PermissionAllow, DecisionReason: "AskUserQuestion is inherently interactive"}, nil
		},
		CallFunc:        callAsk,
		ReadOnlyFunc:    func(json.RawMessage) bool { return true },
		ConcurrencyFunc: func(json.RawMessage) bool { return false },
	}
}

func validateAsk(_ tool.Context, raw json.RawMessage) error {
	input, err := decodeAsk(raw)
	if err != nil {
		return err
	}
	if len(input.Questions) == 0 {
		return fmt.Errorf("questions is required (1-%d)", maxQuestions)
	}
	if len(input.Questions) > maxQuestions {
		return fmt.Errorf("at most %d questions allowed", maxQuestions)
	}
	seenHeader := map[string]struct{}{}
	for i, q := range input.Questions {
		if strings.TrimSpace(q.Header) == "" {
			return fmt.Errorf("questions[%d].header is required", i)
		}
		if len([]rune(q.Header)) > maxHeaderChars {
			return fmt.Errorf("questions[%d].header must be at most %d chars", i, maxHeaderChars)
		}
		if _, dup := seenHeader[q.Header]; dup {
			return fmt.Errorf("questions[%d].header duplicates %q", i, q.Header)
		}
		seenHeader[q.Header] = struct{}{}
		if strings.TrimSpace(q.Question) == "" {
			return fmt.Errorf("questions[%d].question is required", i)
		}
		if len(q.Options) < minOptions || len(q.Options) > maxOptions {
			return fmt.Errorf("questions[%d].options must have %d-%d entries", i, minOptions, maxOptions)
		}
		seenLabel := map[string]struct{}{}
		for j, o := range q.Options {
			if strings.TrimSpace(o.Label) == "" {
				return fmt.Errorf("questions[%d].options[%d].label is required", i, j)
			}
			if _, dup := seenLabel[o.Label]; dup {
				return fmt.Errorf("questions[%d].options[%d].label duplicates %q", i, j, o.Label)
			}
			seenLabel[o.Label] = struct{}{}
		}
	}
	return nil
}

func callAsk(ctx tool.Context, raw json.RawMessage, _ tool.ProgressSink) (contracts.ToolResult, error) {
	input, err := decodeAsk(raw)
	if err != nil {
		return contracts.ToolResult{}, err
	}
	asker := questionAsker(ctx.Metadata)
	if asker == nil {
		return contracts.ToolResult{
			IsError: true,
			Content: "AskUserQuestion is unavailable: no interactive question handler is configured (headless mode).",
		}, fmt.Errorf("no QuestionAsker configured")
	}
	answers, err := asker.AskQuestions(ctx.Context, toToolQuestions(input.Questions))
	if err != nil {
		return contracts.ToolResult{}, err
	}
	return contracts.ToolResult{
		Content:           formatAnswers(answers),
		StructuredContent: map[string]any{"type": "ask_user_question", "answers": structuredAnswers(answers)},
	}, nil
}

func toToolQuestions(qs []askQuestion) []tool.Question {
	out := make([]tool.Question, 0, len(qs))
	for _, q := range qs {
		opts := make([]tool.QuestionOption, 0, len(q.Options))
		for _, o := range q.Options {
			opts = append(opts, tool.QuestionOption{Label: o.Label, Description: o.Description})
		}
		out = append(out, tool.Question{Header: q.Header, Question: q.Question, Options: opts, MultiSelect: q.MultiSelect})
	}
	return out
}

func formatAnswers(answers []tool.QuestionAnswer) string {
	var parts []string
	for _, a := range answers {
		parts = append(parts, fmt.Sprintf("%s: %s", a.Header, strings.Join(a.Selected, ", ")))
	}
	return "User has answered your questions: " + strings.Join(parts, "; ") + ". You can now continue with the user's answers in mind."
}

func structuredAnswers(answers []tool.QuestionAnswer) []map[string]any {
	out := make([]map[string]any, 0, len(answers))
	for _, a := range answers {
		out = append(out, map[string]any{"header": a.Header, "selected": a.Selected})
	}
	return out
}

func questionAsker(metadata map[string]any) tool.QuestionAsker {
	if metadata == nil {
		return nil
	}
	asker, _ := metadata[MetadataQuestionAskerKey].(tool.QuestionAsker)
	return asker
}

func decodeAsk(raw json.RawMessage) (askInput, error) {
	var input askInput
	if err := json.Unmarshal(raw, &input); err != nil {
		return askInput{}, err
	}
	return input, nil
}
```

- [ ] **Step 5: Run tests to verify they pass**

Run: `go test ./internal/tools/ask/ ./internal/tool/ -v`
Expected: PASS.

- [ ] **Step 6: Commit**

```bash
git add internal/tool/types.go internal/tools/ask/tools.go internal/tools/ask/tools_test.go
git commit -m "feat(tools): add AskUserQuestion tool with a QuestionAsker dialog seam"
```

> **Cross-phase note:** the chip/multi-select dialog UI is Phase 2 (`internal/tui`), which implements `tool.QuestionAsker` and injects it via `MetadataQuestionAskerKey`. The headless path errors cleanly, matching CC's non-interactive behavior.

---

## Task 6: EnterPlanMode + ExitPlanMode tools

**Files:**
- Create: `internal/tools/plan/tools.go`
- Test: `internal/tools/plan/tools_test.go`

**Interfaces:**
- Produces: `func NewEnterPlanModeTool() tool.Tool`, `func NewExitPlanModeTool() tool.Tool`, `func PlanFilePath(sessionPath string, sessionID contracts.ID) string`, `func WritePlan(...)`, `func ReadPlan(...)`.
- `EnterPlanMode`: empty input schema; `CheckPermissions` returns `Allow`; `Call` records the intent to switch `PermissionMode` to `contracts.PermissionPlan` (confirmed value, permissions.go:10) by emitting it in `StructuredContent` (the runner applies the mode — Phase 2 wires the UI indicator).
- `ExitPlanMode`: input schema is `{}` (CC ExitPlanModeV2 reads the plan from disk, NOT from a `plan` param — see CC reference); `CheckPermissions` returns `PermissionAsk` with message `Exit plan mode?` so the executor's `Asker` runs the approval ceremony (Phase 2 supplies the rich plan-preview dialog); on Allow the `Call` reads the plan from disk, returns `User has approved your plan...`, and signals restoring `PrePlanMode` (contracts permissions.go:86) in `StructuredContent`.

**CC reference (read first):** EnterPlanMode `/Users/sqlrush/agent/claude-code/src/tools/EnterPlanModeTool/EnterPlanModeTool.ts:21-25` (empty `z.strictObject({})`), `:77-118` (sets mode `plan`, returns "Entered plan mode..."). ExitPlanModeV2 `/Users/sqlrush/agent/claude-code/src/tools/ExitPlanModeTool/ExitPlanModeV2Tool.ts:77-89` (internal schema = optional `allowedPrompts` only; **plan read from disk** via `getPlan`/`getPlanFilePath`, lines 246-253), `:233-238` (`checkPermissions` → `{behavior:'ask', message:'Exit plan mode?'}`), `:195-220` (validate: must be in `plan` mode), `:481-491` ("User has approved your plan. You can now start coding...").

**ccgo confirmations (run first):**
```bash
grep -n "PermissionPlan\|PrePlanMode" internal/contracts/permissions.go
grep -n "MetadataSessionPathKey" internal/tool/types.go
go doc ./internal/contracts NewID
```
Expected: `PermissionPlan = "plan"` (permissions.go:10), `PrePlanMode` field (permissions.go:86), `MetadataSessionPathKey` (types.go:96), `contracts.NewID()` exists.

- [ ] **Step 1: Write the failing test**

Create `internal/tools/plan/tools_test.go`:
```go
package plantools

import (
	"context"
	"encoding/json"
	"strings"
	"testing"

	"ccgo/internal/contracts"
	"ccgo/internal/tool"
)

func TestEnterPlanModeAllowsAndSignalsMode(t *testing.T) {
	toolImpl := NewEnterPlanModeTool()
	ctx := tool.Context{Context: context.Background()}
	dec, err := toolImpl.CheckPermissions(ctx, json.RawMessage(`{}`))
	if err != nil || dec.Behavior != contracts.PermissionAllow {
		t.Fatalf("CheckPermissions = %v, %v", dec.Behavior, err)
	}
	res, err := toolImpl.Call(ctx, json.RawMessage(`{}`), tool.NopProgressSink())
	if err != nil {
		t.Fatalf("Call err: %v", err)
	}
	if got, _ := res.StructuredContent["permission_mode"].(string); got != string(contracts.PermissionPlan) {
		t.Fatalf("permission_mode = %q want %q", got, contracts.PermissionPlan)
	}
	content, _ := res.Content.(string)
	if !strings.Contains(content, "plan mode") {
		t.Fatalf("Call content = %q", content)
	}
}

func TestExitPlanModeAsksThenApproves(t *testing.T) {
	dir := t.TempDir()
	if err := WritePlan(dir, "s1", "1. do the thing"); err != nil {
		t.Fatalf("WritePlan err: %v", err)
	}
	toolImpl := NewExitPlanModeTool()
	ctx := tool.Context{
		Context:   context.Background(),
		SessionID: "s1",
		Metadata:  map[string]any{tool.MetadataSessionPathKey: dir},
	}
	dec, err := toolImpl.CheckPermissions(ctx, json.RawMessage(`{}`))
	if err != nil {
		t.Fatalf("CheckPermissions err: %v", err)
	}
	if dec.Behavior != contracts.PermissionAsk {
		t.Fatalf("ExitPlanMode behavior = %q want ask", dec.Behavior)
	}
	// After approval the executor calls Call; it must echo the plan.
	res, err := toolImpl.Call(ctx, json.RawMessage(`{}`), tool.NopProgressSink())
	if err != nil {
		t.Fatalf("Call err: %v", err)
	}
	content, _ := res.Content.(string)
	if !strings.Contains(content, "approved your plan") || !strings.Contains(content, "do the thing") {
		t.Fatalf("Call content = %q", content)
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/tools/plan/ -v`
Expected: FAIL — `undefined: NewEnterPlanModeTool` / `undefined: WritePlan`.

- [ ] **Step 3: Write minimal implementation**

Create `internal/tools/plan/tools.go`:
```go
package plantools

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"ccgo/internal/contracts"
	"ccgo/internal/tool"
)

// PlanFilePath returns where the active plan markdown is stored for a session.
// CC reads the plan from disk in ExitPlanMode (ExitPlanModeV2Tool.ts:246).
func PlanFilePath(sessionPath string, sessionID contracts.ID) string {
	dir := strings.TrimSpace(sessionPath)
	if dir == "" {
		dir = "."
	}
	name := string(sessionID)
	if name == "" {
		name = "plan"
	}
	return filepath.Join(dir, name+".plan.md")
}

func WritePlan(sessionPath string, sessionID contracts.ID, plan string) error {
	path := PlanFilePath(sessionPath, sessionID)
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return fmt.Errorf("create plan dir: %w", err)
	}
	if err := os.WriteFile(path, []byte(plan), 0o600); err != nil {
		return fmt.Errorf("write plan: %w", err)
	}
	return nil
}

func ReadPlan(sessionPath string, sessionID contracts.ID) (string, error) {
	data, err := os.ReadFile(PlanFilePath(sessionPath, sessionID))
	if err != nil {
		if os.IsNotExist(err) {
			return "", nil
		}
		return "", fmt.Errorf("read plan: %w", err)
	}
	return string(data), nil
}

func NewEnterPlanModeTool() tool.Tool {
	return tool.FuncTool{
		DefinitionValue: contracts.ToolDefinition{
			Name:                "EnterPlanMode",
			Description:         "Requests permission to enter plan mode for complex tasks requiring exploration and design.",
			ReadOnly:            true,
			RequiresInteraction: true,
			InputSchema:         contracts.JSONSchema{"type": "object", "properties": map[string]any{}},
		},
		PromptFunc: func(tool.PromptContext) (string, error) {
			return "Enters plan mode. In plan mode you focus on exploring and designing; DO NOT write or edit any files. Write the plan to disk, then call ExitPlanMode to request approval to start coding.", nil
		},
		PermissionFunc: func(tool.Context, json.RawMessage) (contracts.PermissionDecision, error) {
			return contracts.PermissionDecision{Behavior: contracts.PermissionAllow, DecisionReason: "entering plan mode"}, nil
		},
		CallFunc: func(_ tool.Context, _ json.RawMessage, _ tool.ProgressSink) (contracts.ToolResult, error) {
			return contracts.ToolResult{
				Content: "Entered plan mode. Focus on exploring and designing. DO NOT write or edit any files yet. Write your plan, then call ExitPlanMode.",
				StructuredContent: map[string]any{
					"type":            "enter_plan_mode",
					"permission_mode": string(contracts.PermissionPlan),
				},
			}, nil
		},
		ReadOnlyFunc:    func(json.RawMessage) bool { return true },
		ConcurrencyFunc: func(json.RawMessage) bool { return false },
	}
}

func NewExitPlanModeTool() tool.Tool {
	return tool.FuncTool{
		DefinitionValue: contracts.ToolDefinition{
			Name:                "ExitPlanMode",
			Description:         "Prompts the user to exit plan mode and start coding.",
			RequiresInteraction: true,
			InputSchema:         contracts.JSONSchema{"type": "object", "properties": map[string]any{}},
		},
		PromptFunc: func(tool.PromptContext) (string, error) {
			return "Requests approval to exit plan mode and begin coding. This tool does NOT take the plan as a parameter — it reads the plan you wrote to disk. Only call it when you have finished planning.", nil
		},
		PermissionFunc: func(tool.Context, json.RawMessage) (contracts.PermissionDecision, error) {
			// Ask routes through Executor.Asker; Phase 2 renders the plan preview.
			return contracts.PermissionDecision{Behavior: contracts.PermissionAsk, Message: "Exit plan mode?"}, nil
		},
		CallFunc: func(ctx tool.Context, _ json.RawMessage, _ tool.ProgressSink) (contracts.ToolResult, error) {
			sessionPath, _ := ctx.Metadata[tool.MetadataSessionPathKey].(string)
			plan, err := ReadPlan(sessionPath, ctx.SessionID)
			if err != nil {
				return contracts.ToolResult{}, err
			}
			content := "User has approved your plan. You can now start coding."
			if strings.TrimSpace(plan) != "" {
				content += "\n\nApproved plan:\n" + plan
			}
			return contracts.ToolResult{
				Content: content,
				StructuredContent: map[string]any{
					"type":            "exit_plan_mode",
					"restore_mode":    true, // runner restores PrePlanMode
					"plan":            plan,
				},
			}, nil
		},
		ReadOnlyFunc:    func(json.RawMessage) bool { return false },
		ConcurrencyFunc: func(json.RawMessage) bool { return false },
	}
}
```

- [ ] **Step 4: Run tests to verify they pass**

Run: `go test ./internal/tools/plan/ -v`
Expected: PASS.

- [ ] **Step 5: Commit**

```bash
git add internal/tools/plan/tools.go internal/tools/plan/tools_test.go
git commit -m "feat(tools): add EnterPlanMode and ExitPlanMode tools behind the Asker seam"
```

> **Cross-phase note:** the plan-approval ceremony UI (rich plan preview, mode indicator) is Phase 2. Here `ExitPlanMode` returns `PermissionAsk`, so Phase 1's `Executor.Asker` gates it; the runner's application of `permission_mode`/`restore_mode` from `StructuredContent` is wired alongside Phase 2's mode-switch UI.

---

## Task 7: LSPTool 9-operation tool

**Files:**
- Create: `internal/tools/lsp/lsp_tool.go`
- Test: `internal/tools/lsp/lsp_tool_test.go`

**Interfaces:**
- Produces: `func NewLSPTool() tool.Tool` — a single tool named `LSP` with a discriminated-union `operation` field. Keep the existing `NewDiagnosticsTool()` (`LSPDiagnostics`) untouched.
- 9 operations (verbatim from CC, `schemas.ts:14-166`): `goToDefinition`, `findReferences`, `hover`, `documentSymbol`, `workspaceSymbol`, `goToImplementation`, `prepareCallHierarchy`, `incomingCalls`, `outgoingCalls`. Every op requires `filePath` (string), `line` (1-based positive int), `character` (1-based positive int).

**CC reference (read first):** `/Users/sqlrush/agent/claude-code/src/tools/LSPTool/schemas.ts:8-215` (the `z.discriminatedUnion('operation', ...)`, 9 literals) and `prompt.ts:3-21`. Tool name `LSP_TOOL_NAME = 'LSP'`.

**ccgo LSP backend (confirm available ops):**
```bash
go doc ./internal/lsp | grep -i "func\|Definition\|References\|Hover\|Symbol\|Implementation\|CallHierarchy"
grep -rn "func.*ServerProcess\|GoToDefinition\|FindReferences\|Hover\|DocumentSymbol" internal/lsp/*.go | grep -v _test | head
```
Expected: identify which operations the existing `internal/lsp` client supports. **Flag:** if the LSP client lacks a method for an op, the tool returns a clear "operation not supported by the configured language server" result rather than a panic — validate the op name, attempt the call, surface unsupported gracefully. Do NOT invent backend methods; only call confirmed ones, and for the rest return the unsupported message.

- [ ] **Step 1: Write the failing test**

Create `internal/tools/lsp/lsp_tool_test.go`:
```go
package lsptools

import (
	"context"
	"encoding/json"
	"testing"

	"ccgo/internal/tool"
)

func TestLSPToolValidatesOperation(t *testing.T) {
	toolImpl := NewLSPTool()
	if toolImpl.Name() != "LSP" {
		t.Fatalf("Name = %q want LSP", toolImpl.Name())
	}
	ctx := tool.Context{Context: context.Background()}
	// Unknown operation rejected.
	bad, _ := json.Marshal(map[string]any{"operation": "bogus", "filePath": "a.go", "line": 1, "character": 1})
	if err := toolImpl.Validate(ctx, bad); err == nil {
		t.Fatal("expected error for unknown operation")
	}
	// Each of the 9 ops with valid coords passes validation.
	for _, op := range []string{
		"goToDefinition", "findReferences", "hover", "documentSymbol",
		"workspaceSymbol", "goToImplementation", "prepareCallHierarchy",
		"incomingCalls", "outgoingCalls",
	} {
		raw, _ := json.Marshal(map[string]any{"operation": op, "filePath": "a.go", "line": 1, "character": 1})
		if err := toolImpl.Validate(ctx, raw); err != nil {
			t.Fatalf("operation %q failed validation: %v", op, err)
		}
	}
	// Non-positive line rejected.
	zero, _ := json.Marshal(map[string]any{"operation": "hover", "filePath": "a.go", "line": 0, "character": 1})
	if err := toolImpl.Validate(ctx, zero); err == nil {
		t.Fatal("expected error for line < 1")
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/tools/lsp/ -run TestLSPTool -v`
Expected: FAIL — `undefined: NewLSPTool`.

- [ ] **Step 3: Write minimal implementation**

Create `internal/tools/lsp/lsp_tool.go`:
```go
package lsptools

import (
	"encoding/json"
	"fmt"
	"strings"

	"ccgo/internal/contracts"
	"ccgo/internal/tool"
)

var lspOperations = map[string]struct{}{
	"goToDefinition":       {},
	"findReferences":       {},
	"hover":                {},
	"documentSymbol":       {},
	"workspaceSymbol":      {},
	"goToImplementation":   {},
	"prepareCallHierarchy": {},
	"incomingCalls":        {},
	"outgoingCalls":        {},
}

type lspInput struct {
	Operation string `json:"operation"`
	FilePath  string `json:"filePath"`
	Line      int    `json:"line"`
	Character int    `json:"character"`
}

func NewLSPTool() tool.Tool {
	return tool.FuncTool{
		DefinitionValue: contracts.ToolDefinition{
			Name:            "LSP",
			Description:     "Query a language server for navigation, symbols, and call hierarchy.",
			ReadOnly:        true,
			ConcurrencySafe: true,
			InputSchema: contracts.JSONSchema{
				"type":     "object",
				"required": []any{"operation", "filePath", "line", "character"},
				"properties": map[string]any{
					"operation": map[string]any{
						"type": "string",
						"enum": []any{
							"goToDefinition", "findReferences", "hover", "documentSymbol",
							"workspaceSymbol", "goToImplementation", "prepareCallHierarchy",
							"incomingCalls", "outgoingCalls",
						},
					},
					"filePath":  map[string]any{"type": "string"},
					"line":      map[string]any{"type": "integer", "minimum": 1},
					"character": map[string]any{"type": "integer", "minimum": 1},
				},
			},
		},
		PromptFunc: func(tool.PromptContext) (string, error) {
			return "Queries the language server. Operations: goToDefinition, findReferences, hover, documentSymbol, workspaceSymbol, goToImplementation, prepareCallHierarchy, incomingCalls, outgoingCalls. Provide filePath plus 1-based line and character.", nil
		},
		ValidateFunc: validateLSP,
		PermissionFunc: func(tool.Context, json.RawMessage) (contracts.PermissionDecision, error) {
			return contracts.PermissionDecision{Behavior: contracts.PermissionAllow, DecisionReason: "LSP queries are read-only"}, nil
		},
		CallFunc:        callLSP,
		ReadOnlyFunc:    func(json.RawMessage) bool { return true },
		ConcurrencyFunc: func(json.RawMessage) bool { return true },
	}
}

func validateLSP(_ tool.Context, raw json.RawMessage) error {
	input, err := decodeLSP(raw)
	if err != nil {
		return err
	}
	if _, ok := lspOperations[input.Operation]; !ok {
		return fmt.Errorf("unsupported operation %q", input.Operation)
	}
	if strings.TrimSpace(input.FilePath) == "" {
		return fmt.Errorf("filePath is required")
	}
	if input.Line < 1 {
		return fmt.Errorf("line must be >= 1")
	}
	if input.Character < 1 {
		return fmt.Errorf("character must be >= 1")
	}
	return nil
}

func callLSP(ctx tool.Context, raw json.RawMessage, _ tool.ProgressSink) (contracts.ToolResult, error) {
	input, err := decodeLSP(raw)
	if err != nil {
		return contracts.ToolResult{}, err
	}
	// Dispatch to the configured LSP client. Only call confirmed backend
	// methods (Step 1's go doc); for operations the server cannot serve,
	// return the unsupported message rather than erroring the turn.
	result, supported := dispatchLSP(ctx, input)
	if !supported {
		return contracts.ToolResult{
			Content: fmt.Sprintf("LSP operation %q is not supported by the configured language server.", input.Operation),
			StructuredContent: map[string]any{"type": "lsp", "operation": input.Operation, "supported": false},
		}, nil
	}
	return result, nil
}

func decodeLSP(raw json.RawMessage) (lspInput, error) {
	var input lspInput
	if len(raw) == 0 {
		return lspInput{}, fmt.Errorf("input is required")
	}
	if err := json.Unmarshal(raw, &input); err != nil {
		return lspInput{}, err
	}
	return input, nil
}

// dispatchLSP routes to internal/lsp. Implement only the ops the backend
// confirmed in Step 1; everything else returns supported=false.
func dispatchLSP(ctx tool.Context, input lspInput) (contracts.ToolResult, bool) {
	// TODO(impl): wire confirmed internal/lsp methods here. Until a backend
	// method exists for an op, return supported=false so the tool degrades
	// gracefully. The discriminated-union surface is the deliverable; the
	// per-op backend calls are added as internal/lsp grows (flagged P1).
	return contracts.ToolResult{}, false
}
```

**Flag (honest scope):** the 9-op *surface* (schema, validation, dispatch skeleton, graceful-degrade) is this task's deliverable. Wiring each op to a real `internal/lsp` round-trip depends on the LSP client exposing those methods; do that incrementally as the backend grows. The test above validates the surface, not live LSP I/O (which would need a running language server). If Step 1 shows `internal/lsp` already supports e.g. `goToDefinition`, implement that one op in `dispatchLSP` and add a focused test using the existing LSP test harness (check `internal/tools/lsp/tools_test.go` for the snapshot-based pattern).

- [ ] **Step 4: Run tests to verify they pass**

Run: `go test ./internal/tools/lsp/ -v`
Expected: PASS (surface validation), existing `LSPDiagnostics` tests still green.

- [ ] **Step 5: Commit**

```bash
git add internal/tools/lsp/lsp_tool.go internal/tools/lsp/lsp_tool_test.go
git commit -m "feat(tools): add 9-operation LSP tool surface alongside LSPDiagnostics"
```

---

## Task 8: Bash working-directory persistence across calls

**Files:**
- Create: `internal/tools/bash/cwd.go`
- Modify: `internal/tools/bash/tools.go` (`runBashCommand`, line 1040; `callBash`, line 936)
- Test: `internal/tools/bash/cwd_test.go`

**Interfaces:**
- Produces:
  - `const MetadataBashCWDKey = "ccgo.tools.bash.cwd"`
  - `type CWDState struct { ... }` with `func NewCWDState(initial string) *CWDState`, `Get() string`, `Set(dir string)`.
  - `func bashEffectiveCWD(ctx tool.Context) string` — returns the persisted cwd if set, else `ctx.WorkingDirectory`.
  - `func updateBashCWD(ctx tool.Context, command string)` — detects a leading/standalone `cd <dir>` and updates the state (best-effort, like CC's persistent shell session).

**CC reference:** CC runs Bash in a *persistent shell session* so `cd` persists (BashTool prompt note "runs in a persistent shell session"). ccgo spawns a fresh `/bin/sh -c` per call (`shellCommand`, tools.go:1193) with `cmd.Dir = ctx.WorkingDirectory` (tools.go:1052) — cwd does NOT persist. Gap-audit §5 "Bash cwd not persisted across calls."

- [ ] **Step 1: Confirm the per-call cwd + no state today**

Run:
```bash
grep -n "cmd.Dir = ctx.WorkingDirectory\|func runBashCommand\|func shellCommand" internal/tools/bash/tools.go
grep -rn "MetadataBashCWDKey\|CWDState\|persist.*cwd" internal/tools/bash/
```
Expected: `cmd.Dir = ctx.WorkingDirectory` per call; **no** cwd state. **Flag:** confirm `tool.Context.WorkingDirectory` is the field name (`grep -n "WorkingDirectory" internal/tool/types.go` → types.go:28).

- [ ] **Step 2: Write the failing test**

Create `internal/tools/bash/cwd_test.go`:
```go
package bashtools

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"ccgo/internal/tool"
)

func TestBashCWDPersistsAcrossCalls(t *testing.T) {
	root := t.TempDir()
	sub := filepath.Join(root, "sub")
	if err := os.Mkdir(sub, 0o755); err != nil {
		t.Fatal(err)
	}
	state := NewCWDState(root)
	ctx := tool.Context{
		Context:          context.Background(),
		WorkingDirectory: root,
		Metadata:         map[string]any{MetadataBashCWDKey: state},
	}
	// First call: cd into sub.
	raw1, _ := json.Marshal(map[string]any{"command": "cd sub"})
	if _, err := NewBashTool().Call(ctx, raw1, tool.NopProgressSink()); err != nil {
		t.Fatalf("call 1 err: %v", err)
	}
	if got := state.Get(); got != sub {
		t.Fatalf("cwd after cd = %q want %q", got, sub)
	}
	// Second call: pwd should report sub, proving persistence.
	raw2, _ := json.Marshal(map[string]any{"command": "pwd"})
	res, err := NewBashTool().Call(ctx, raw2, tool.NopProgressSink())
	if err != nil {
		t.Fatalf("call 2 err: %v", err)
	}
	content, _ := res.Content.(string)
	if !strings.Contains(content, "sub") {
		t.Fatalf("pwd output = %q want it to contain sub", content)
	}
}

func TestBashEffectiveCWDFallsBackToContext(t *testing.T) {
	ctx := tool.Context{WorkingDirectory: "/repo"}
	if got := bashEffectiveCWD(ctx); got != "/repo" {
		t.Fatalf("bashEffectiveCWD = %q want /repo", got)
	}
}
```

- [ ] **Step 3: Run test to verify it fails**

Run: `go test ./internal/tools/bash/ -run TestBashCWD -v`
Expected: FAIL — `undefined: NewCWDState` / `undefined: MetadataBashCWDKey`.

- [ ] **Step 4: Write minimal implementation**

Create `internal/tools/bash/cwd.go`:
```go
package bashtools

import (
	"path/filepath"
	"strings"
	"sync"

	"ccgo/internal/tool"
)

// MetadataBashCWDKey injects a session-scoped *CWDState so `cd` persists across
// Bash calls, emulating CC's persistent shell session.
const MetadataBashCWDKey = "ccgo.tools.bash.cwd"

type CWDState struct {
	mu  sync.RWMutex
	dir string
}

func NewCWDState(initial string) *CWDState {
	return &CWDState{dir: initial}
}

func (s *CWDState) Get() string {
	if s == nil {
		return ""
	}
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.dir
}

func (s *CWDState) Set(dir string) {
	if s == nil || strings.TrimSpace(dir) == "" {
		return
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	s.dir = dir
}

func bashCWDState(ctx tool.Context) *CWDState {
	if ctx.Metadata == nil {
		return nil
	}
	state, _ := ctx.Metadata[MetadataBashCWDKey].(*CWDState)
	return state
}

// bashEffectiveCWD returns the persisted cwd if present, else ctx.WorkingDirectory.
func bashEffectiveCWD(ctx tool.Context) string {
	if state := bashCWDState(ctx); state != nil {
		if dir := state.Get(); dir != "" {
			return dir
		}
	}
	return ctx.WorkingDirectory
}

// updateBashCWD detects a leading "cd <dir>" and updates the persisted cwd.
// Best-effort: only the simple, common single-segment form is tracked.
func updateBashCWD(ctx tool.Context, command string) {
	state := bashCWDState(ctx)
	if state == nil {
		return
	}
	segments := splitCommandSegments(command)
	if len(segments) != 1 {
		return // compound command; don't guess.
	}
	words := shellWords(segments[0])
	if len(words) != 2 || words[0] != "cd" {
		return
	}
	target := words[1]
	if !filepath.IsAbs(target) {
		target = filepath.Join(bashEffectiveCWD(ctx), target)
	}
	state.Set(filepath.Clean(target))
}
```

In `internal/tools/bash/tools.go`, use the effective cwd in `runBashCommand` (replace `if ctx.WorkingDirectory != "" { cmd.Dir = ctx.WorkingDirectory }` at line 1052):
```go
	if dir := bashEffectiveCWD(ctx); dir != "" {
		cmd.Dir = dir
	}
```
Apply the same in `startBackgroundBash` (line 1095). In `callBash` (line 936), after the command runs, persist any `cd` — add at the top of `callBash` after decoding (line 941):
```go
	updateBashCWD(ctx, strings.TrimSpace(input.Command))
```

- [ ] **Step 5: Run tests to verify they pass**

Run: `go test ./internal/tools/bash/ -v`
Expected: PASS. (No `MetadataBashCWDKey` → `bashEffectiveCWD` falls back to `ctx.WorkingDirectory`, preserving today's behavior; existing tests unaffected.)

- [ ] **Step 6: Commit**

```bash
git add internal/tools/bash/cwd.go internal/tools/bash/tools.go internal/tools/bash/cwd_test.go
git commit -m "feat(tools): persist Bash working directory across calls via cd tracking"
```

---

## Task 9: TodoWrite `activeForm` schema

**Files:**
- Modify: `internal/tools/todo/state.go` (`Todo` struct, line 19)
- Modify: `internal/tools/todo/tools.go` (schema line 27-45; `validateTodos`; `decodeTodoWrite`; `structuredTodos`; prompt)
- Test: `internal/tools/todo/activeform_test.go`

**Interfaces:**
- Changes `Todo` to `{ Content, Status, ActiveForm string }` — drops `ID` and `Priority`.

**CC reference (read first):** `/Users/sqlrush/agent/claude-code/src/utils/todo/types.ts:8-14` — `{ content: string.min(1), status: enum(pending|in_progress|completed), activeForm: string.min(1) }`. No `id`, no `priority`. Prompt (prompt.ts:152-153, 184): `content` = imperative, `activeForm` = present-continuous.

- [ ] **Step 1: Confirm current schema**

Run:
```bash
grep -n "Priority\|ActiveForm\|activeForm" internal/tools/todo/state.go internal/tools/todo/tools.go
grep -rn "todo.Priority\|\.Priority" internal/ | grep -i todo
```
Expected: `Todo.Priority` (state.go:23) + schema requires `priority` (tools.go:36); **no** `ActiveForm`. **Flag:** find every reader of `Todo.Priority` (TUI rendering, session restore) so they migrate together — check the second grep's hits.

- [ ] **Step 2: Write the failing test**

Create `internal/tools/todo/activeform_test.go`:
```go
package todotools

import (
	"context"
	"encoding/json"
	"testing"

	"ccgo/internal/tool"
)

func TestTodoWriteRequiresActiveForm(t *testing.T) {
	toolImpl := NewTodoWriteTool()
	ctx := tool.Context{Context: context.Background()}

	// New schema: content/status/activeForm, no id/priority.
	good, _ := json.Marshal(map[string]any{"todos": []any{
		map[string]any{"content": "Write the parser", "status": "in_progress", "activeForm": "Writing the parser"},
	}})
	if err := toolImpl.Validate(ctx, good); err != nil {
		t.Fatalf("valid activeForm todo failed: %v", err)
	}

	// Missing activeForm → error.
	noForm, _ := json.Marshal(map[string]any{"todos": []any{
		map[string]any{"content": "x", "status": "pending"},
	}})
	if err := toolImpl.Validate(ctx, noForm); err == nil {
		t.Fatal("expected error when activeForm missing")
	}

	// Legacy priority field → rejected as not allowed.
	legacy, _ := json.Marshal(map[string]any{"todos": []any{
		map[string]any{"content": "x", "status": "pending", "activeForm": "Doing x", "priority": "high"},
	}})
	if err := toolImpl.Validate(ctx, legacy); err == nil {
		t.Fatal("expected error for legacy priority field")
	}
}
```

- [ ] **Step 3: Run test to verify it fails**

Run: `go test ./internal/tools/todo/ -run TestTodoWriteRequiresActiveForm -v`
Expected: FAIL — schema still requires `priority`, allows no `activeForm`.

- [ ] **Step 4: Write minimal implementation**

In `internal/tools/todo/state.go`, change the struct (line 19-24):
```go
type Todo struct {
	Content    string `json:"content"`
	Status     string `json:"status"`
	ActiveForm string `json:"activeForm"`
}
```

In `internal/tools/todo/tools.go`:
- Schema (lines 35-43): replace the `items` `required`/`properties`:
```go
							"required": []any{"content", "status", "activeForm"},
							"properties": map[string]any{
								"content":    map[string]any{"type": "string"},
								"status":     map[string]any{"type": "string", "enum": []any{"pending", "in_progress", "completed"}},
								"activeForm": map[string]any{"type": "string"},
							},
```
- `PromptFunc` (line 47-49): update to describe content (imperative) + activeForm (present continuous), drop priority.
- `validateTodos` (line 65): remove the `id`/duplicate-id and `priority` checks; add `activeForm` required:
```go
func validateTodos(todos []Todo) error {
	inProgress := 0
	for i, todo := range todos {
		prefix := fmt.Sprintf("todos[%d]", i)
		if strings.TrimSpace(todo.Content) == "" {
			return fmt.Errorf("%s.content is required", prefix)
		}
		if strings.TrimSpace(todo.ActiveForm) == "" {
			return fmt.Errorf("%s.activeForm is required", prefix)
		}
		if !validTodoStatus(todo.Status) {
			return fmt.Errorf("%s.status must be one of pending, in_progress, or completed", prefix)
		}
		if todo.Status == "in_progress" {
			inProgress++
		}
	}
	if inProgress > 1 {
		return fmt.Errorf("only one todo can be in_progress at a time")
	}
	return nil
}
```
- `validateTodoKeys` (line 160): change `allowed` + required to `content`/`status`/`activeForm`.
- Delete `validTodoPriority` (line 193).
- `structuredTodos` (line 202): emit `content`/`status`/`activeForm`.

**Flag:** update every reader found in Step 1 (TUI todo rendering, session todo restore) to use `ActiveForm` instead of `Priority` in the SAME commit so the build stays green. Run `go build ./...` to find them all.

- [ ] **Step 5: Run tests to verify they pass**

Run: `go build ./... && go test ./internal/tools/todo/ -v`
Expected: build clean, tests PASS. Fix any `Todo.Priority`/`Todo.ID` reference the compiler flags (per Step 1).

- [ ] **Step 6: Commit**

```bash
git add internal/tools/todo/state.go internal/tools/todo/tools.go internal/tools/todo/activeform_test.go
git commit -m "feat(tools): migrate TodoWrite to the activeForm schema (drop id/priority)"
```

---

## Task 10: Register the new tools + full-suite verification

**Files:**
- Modify: the tool-registration site (find with `grep -rn "NewBashTool\|NewWebFetchTool\|NewDiagnosticsTool" internal/bootstrap/ internal/conversation/ cmd/`).
- Test: a registration test in the same package.

**Interfaces:** wires `NewAskUserQuestionTool`, `NewEnterPlanModeTool`, `NewExitPlanModeTool`, `NewLSPTool` into the default registry so they reach the model. Confirms no name collisions (`tool.Registry.Register` errors on dup, registry.go:43).

- [ ] **Step 1: Find the registration site**

Run:
```bash
grep -rn "NewBashTool()\|NewDiagnosticsTool()\|NewWebFetchTool()\|tool.NewRegistry\|registry.Register" internal/bootstrap/ internal/conversation/ cmd/ | grep -v _test
```
Expected: a central list where built-in tools are constructed and registered. **Flag:** confirm the exact file + function before editing; do not assume `internal/bootstrap/state.go`.

- [ ] **Step 2: Write the failing test**

In the registration package's test file, assert the new tools are present:
```go
func TestDefaultRegistryHasPhase5Tools(t *testing.T) {
	reg := /* call the production registry constructor */
	for _, name := range []string{"AskUserQuestion", "EnterPlanMode", "ExitPlanMode", "LSP"} {
		if _, ok := reg.Lookup(name); !ok {
			t.Fatalf("registry missing %q", name)
		}
	}
}
```
Adapt the constructor call to the real registry builder found in Step 1.

- [ ] **Step 3: Run test to verify it fails**

Run: `go test <registration-package> -run TestDefaultRegistryHasPhase5Tools -v`
Expected: FAIL — tools not registered.

- [ ] **Step 4: Wire the tools**

Add the four constructors (`asktools.NewAskUserQuestionTool()`, `plantools.NewEnterPlanModeTool()`, `plantools.NewExitPlanModeTool()`, `lsptools.NewLSPTool()`) to the built-in tool list at the site found in Step 1, with the matching imports. Keep `LSPDiagnostics` registered too.

- [ ] **Step 5: Full verification**

Run:
```bash
go build ./... && go vet ./... && go test ./... 2>&1 | tail -30
```
Expected: build OK, vet clean, full suite green.

- [ ] **Step 6: Commit**

```bash
git add -A
git commit -m "feat(tools): register AskUserQuestion, Enter/ExitPlanMode, and LSP tools"
```

---

## Self-Review

**Spec coverage (Phase-5 brief = tool behavior matches CC):**
- Full Bash prompt → Task 1. ✓
- Full PowerShell prompt → Task 2. ✓
- WebFetch secondary-model summarization → Task 3. ✓
- WebSearch official `web_search_20250305` server tool → Task 4. ✓
- AskUserQuestion (via dialog seam) → Task 5. ✓
- EnterPlanMode + ExitPlanMode (behind the Asker seam) → Task 6. ✓
- LSPTool 9-op → Task 7. ✓
- Bash cwd persistence → Task 8. ✓
- TodoWrite `activeForm` schema → Task 9. ✓
- Registration + full-suite gate → Task 10. ✓
- `StructuredOutput` / Enter+ExitWorktree / Config tool — **deferred** (see below).

**Code-verified anchors used (not assumed):**
- Bash prompt stub: `internal/tools/bash/tools.go:816-818`; PowerShell stub: `internal/tools/powershell/tools.go:95-96`.
- Tool type: `tool.FuncTool` (`internal/tool/func_tool.go:18`), `tool.PromptContext{Model,WorkingDirectory,Metadata}` (`tool/types.go:10-14`), `tool.Context.WorkingDirectory` (`tool/types.go:28`).
- Asker seam already consulted in the Ask branch: `executor.go:95-144`; `PermissionAsker` at `tool/types.go:39-51`.
- WebFetch raw-text-only: `web_fetch.go:79-81` (stub prompt), no model client; WebSearch scrapes DuckDuckGo: `web_search.go:22` (`defaultWebSearchEndpoint`). Test-injection pattern via `MetadataWebSearchEndpointKey` + `httptest.NewServer` confirmed in `web_search_test.go`.
- API `Request`/`ToolDefinition` lack server-tool fields: `anthropic/types.go:13-48`; `contracts.ContentBlockType` has no server-tool constants (`contracts/messages.go:23-28`); usage already counts `ServerToolUse.WebSearchRequests` (`contracts/messages.go:626,636`).
- TodoWrite old schema `id`+`priority`: `todo/tools.go:36`, `Todo` struct `todo/state.go:19-24`.
- Only `LSPDiagnostics` exists: `lsp/tools.go:24-81`.
- `contracts.PermissionPlan = "plan"` (`permissions.go:10`), `PrePlanMode` (`permissions.go:86`), `PermissionAsk/Allow/Deny` (`permissions.go:18-20`), `PermissionDecision{Message,BlockedPath,UpdatedInput}` (`permissions.go:50-59`).
- Small model constant: `model.Claude45Haiku = "claude-haiku-4-5-20251001"` (`model/model.go:9,52`).

**CC reference anchors (file:line) used:** Bash `BashTool/prompt.ts:42-161,275-369`; PowerShell `PowerShellTool/prompt.ts:51-145`; WebFetch `WebFetchTool/utils.ts:484-530,63,128` + `prompt.ts:23-46`; WebSearch `WebSearchTool/WebSearchTool.ts:76-84,86-150,284`; AskUserQuestion `AskUserQuestionTool.tsx:14-67`; Enter/ExitPlanMode `EnterPlanModeTool.ts:21-25,77-118` + `ExitPlanModeV2Tool.ts:77-89,233-238,481-491`; LSP `LSPTool/schemas.ts:8-215`; TodoWrite `utils/todo/types.ts:8-14`.

**Gap-audit vs code discrepancies flagged:**
- Gap-audit §4.E lists ExitPlanMode as taking a plan; the **real CC ExitPlanModeV2 reads the plan from disk** (no `plan` param). Task 6 follows the code, not the audit.
- Gap-audit §5 implies a generic "LSPTool 9-op"; the **real 9 ops are navigation + call-hierarchy** (goToDefinition/findReferences/hover/documentSymbol/workspaceSymbol/goToImplementation/prepareCallHierarchy/incomingCalls/outgoingCalls) — NOT completion/rename/formatting. Task 7 uses the verified list.
- ccgo `TodoWrite` is `id`+`priority` (audit said "old schema (`id`+`priority`, no `activeForm`)") — confirmed exactly; CC has only `content`/`status`/`activeForm`.

**Cross-phase dependencies / risks:**
- **Depends on Phase 1 (done):** `Executor.Asker` seam (Tasks 5, 6 route `PermissionAsk` through it).
- **Depends on Phase 2 (dialogs):** the *rich* AskUserQuestion chip dialog (`tool.QuestionAsker` impl) and the ExitPlanMode plan-approval ceremony UI + mode indicators are Phase 2. Here the tools land fully with headless-safe fallbacks; Phase 1's yes/no `PermissionAsker` gates ExitPlanMode, and AskUserQuestion errors cleanly headless. Phase 2 injects `MetadataQuestionAskerKey` and renders the plan preview.
- **Touches Phase 3 territory (flagged, scoped narrowly):** Task 4 adds server-tool *types* to `anthropic.Request`/`contracts` and a tool-side client seam, but the **outbound request wiring + stream parsing of `web_search_tool_result`** is conversation-runner work that overlaps Phase 3's stream-handling. Task 4 keeps the scrape fallback so WebSearch works before that wiring lands; the runner integration is a follow-up.
- **Task 7 honest scope:** the 9-op *surface* is delivered; per-op live LSP round-trips depend on `internal/lsp` exposing methods — implemented incrementally, degrading gracefully (`supported=false`) until then.
- **Task 9 migration risk:** `Todo.Priority`/`ID` may have readers (TUI, session restore); they must migrate in the same commit (`go build ./...` finds them).

**Deferred to later (explicitly NOT in Phase 5, by design):** WebFetch 15-min URL cache (P1); `StructuredOutput` tool (the `structured-outputs-2025-11-13` beta is already wired in `betas.go:12,59`, so a dedicated tool is low value now); `EnterWorktree`/`ExitWorktree` (git-worktree isolation belongs with Phase 7's Team/isolation work); `Config` tool; per-op live LSP backends; the conversation-runner integration of the web-search server tool and plan-mode `permission_mode`/`restore_mode` application (lands with Phase 2/3 wiring). These are flagged at their tasks, not silently dropped.
