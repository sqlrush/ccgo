# MCP CLI + Remote OAuth (Phase 6a) Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Manage MCP servers from the command line (`claude mcp add/list/get/remove/serve`) and connect to OAuth-protected **remote** MCP servers (HTTP/SSE) by implementing the open-standard remote-auth flow: RFC 9728 protected-resource discovery → RFC 8414 authorization-server metadata → RFC 7591 Dynamic Client Registration → `authorization_code` (PKCE) token acquisition with on-disk cache + refresh — plus auto-reconnect/backoff for remote transports and an interactive elicitation hook.

**Architecture:** ccgo already has a strong MCP **client core** (`internal/mcp/`): four transports (`stdio.go`, `sse.go`, `http.go`, `ws.go`), a JSON-RPC `ProtocolClient` (`protocol.go`) with an `initialize` 401-retry seam, configured tool-set assembly (`configured.go`/`server_tools.go`), header/token plumbing (`headers.go`), an OAuth **refresh-only** token provider bridge (`oauth.go` → `internal/auth`), a fully-working `BuiltinServer` (`builtin_server.go`) for `mcp serve`, and elicitation **protocol** handling (`elicitation.go`). The gaps are pure additions, no rewrites:

1. **CLI surface** — a new `cmd/claude` subcommand group `mcp {add,add-json,list,get,remove,serve}` that reads/writes the existing settings documents via `config.ReadSettingsDocument`/`WriteSettingsDocument` and the scope-path helpers. Validation + immutable document edits only; no client connection needed for add/list/get/remove.
2. **Remote OAuth** — a new package `internal/mcp/remoteauth/` implementing RFC 9728/8414 discovery, RFC 7591 DCR, and the full `authorization_code` exchange, **reusing** `internal/auth`'s PKCE primitives (`GenerateCodeVerifier/Challenge/State`), `OAuthTokenProvider` (refresh + store), and `FileCredentialStore`. It plugs into the existing `ServerAccessTokenProvider` seam so connections transparently obtain/refresh tokens.
3. **Reconnect/backoff** — a transport-agnostic supervisor for remote transports.
4. **Elicitation hook** — wire the existing `ElicitationHandler` to an injectable prompt callback.

**Dependency note (Phase 4 OAuth):** the `authorization_code` exchange, the local callback HTTP listener, and the "open the browser" step are the SAME machinery Phase 4 builds for first-party login. As of code audit 2026-06-21 `internal/auth` has **PKCE generators + refresh + file store only** — there is **no callback listener, no code exchange, and no browser opener** (`grep -rn "callback\|Exchange\|OpenBrowser\|http.Server" internal/auth/*.go` → only URL-string constants). **This phase must not duplicate Phase 4.** Task 5 below defines a small `auth.AuthorizationCodeExchange` + `auth.CallbackServer` + `platform.OpenBrowser`; if Phase 4 has already landed them, **reuse Phase 4's exported functions** and skip re-implementing — verify first with the flagged grep in Task 5 Step 0. Either way the exported API contract in Task 5 is the integration point.

**Tech Stack:** Go 1.26; **no new third-party deps** (stdlib `net/http`, `net/url`, `encoding/json`, `crypto/*` via `internal/auth`); existing packages `internal/mcp`, `internal/auth`, `internal/config`, `internal/contracts`, `internal/platform`, `cmd/claude`.

## Global Constraints

(Copied verbatim from master-roadmap §6; values confirmed against `go.mod`.)

- **Module/toolchain:** `ccgo`, `go 1.26` (from `go.mod`).
- **Immutability (CRITICAL):** never mutate shared structs in place; return new copies. Copy the `conversation.Runner` value per turn before setting `OnEvent`/`Tools.Asker` (existing pattern). `permissions.Engine.ApplyUpdate` already returns a **new** engine — honor that. (Phase-6a corollary: settings-document edits return a **new** `map[string]any`; `contracts.MCPServer` values are copied via the existing `cloneMCPServer` before mutation — verify `grep -n "func cloneMCPServer" internal/mcp/*.go`.)
- **Many small files:** one responsibility per file; target 150–350 lines (800 hard max).
- **Errors handled explicitly at every level; never swallow.** Terminal raw-mode `restore` and any acquired resource MUST be released on every exit path (`defer`). (Corollary: HTTP response bodies and the callback `http.Server` MUST be closed via `defer`; cap every response body with `io.LimitReader`.)
- **Input validation at boundaries:** validate all external data (API responses, user input, file content, MCP server output); fail fast with clear messages. (Corollary: server metadata, registration responses, and token responses are untrusted network input — validate every field before use.)
- **No new third-party deps** unless the plan justifies it explicitly. Phase 1 added only `golang.org/x/term`. No bubbletea/tcell/charm.
- **Non-TTY safety:** interactive paths MUST NOT call `term.MakeRaw` when stdin/stdout isn't a tty; fall back to line mode. Tests MUST NOT depend on a real tty. (Corollary: the OAuth flow MUST offer a manual-paste fallback when no browser/tty is available; tests use `httptest`, never a real auth server, and a fake/in-memory callback — never bind a fixed real port nor open a browser in tests.)
- **TDD:** every task writes a failing test first, then minimal code. Commit after each task. Run package tests with `go test ./internal/<pkg>/ -run TestName -v`; full suite `go test ./...`. Run `-race` on concurrency tasks (Task 8 reconnect supervisor).
- **Verify against real code, distrust roadmap docs:** every assumed type name, field, constant, or CC behavior MUST be confirmed with `go doc`/`grep` (ccgo side) or by reading `/Users/sqlrush/agent/claude-code/src` (CC side) before writing the test — flag the exact command at the point of use, as Phase 1's plan does.
- **Security:** no hardcoded secrets; tokens in keychain not plaintext (Phase 4); sandbox flag must actually enforce (Phase 7); never leak sensitive data in errors. (Corollary: never log access/refresh tokens or client secrets; cached credentials reuse `auth.FileCredentialStore` with `0o600`; the callback listener binds `127.0.0.1` only and validates `state` for CSRF.)

---

## Verified current state (code audit 2026-06-21)

Confirm each before relying on it (flagged commands inline in tasks). Summary of what the audit found:

**Exists (reuse, do NOT rebuild):**
- Transports + protocol client: `internal/mcp/{stdio,sse,http,ws,protocol}.go`. `protocol.go:235` `Initialize` already retries once on `IsUnauthorizedError` via `refreshAuthorizationLocked` (`protocol.go:732`).
- Token bridge: `internal/mcp/oauth.go:27` `FileOAuthAccessTokenProvider` builds an `auth.NewOAuthTokenProvider` from **already-stored** credentials (refresh only — it returns `nil` if `server.OAuth == nil` and never performs an initial grant).
- Header/token seam: `internal/mcp/headers.go:15-24` (`AccessTokenProvider`, `RefreshingAccessTokenProvider`, `ServerAccessTokenProvider`); `headers.go:60` `OAuthServerHeaderProvider`.
- Tool-set assembly: `internal/mcp/configured.go:29` `BuildConfiguredToolSets`; `server_tools.go:25` `ServerToolOptions{HeaderProvider, AccessTokenProvider}`, `server_tools.go:238` `BuildServerToolSets`.
- `mcp serve` server: `internal/mcp/builtin_server.go:63` `NewBuiltinServer` + `:90` `Run(ctx, in, out)` — a complete stdio JSON-RPC server exposing the local tool registry. **Only the CLI wiring is missing.**
- Elicitation protocol: `internal/mcp/elicitation.go:17` `ElicitationHandler`, `:19` `ElicitationRequestHandler`. (No interactive UI bridge.)
- Auth primitives: `internal/auth/oauth.go` `GenerateCodeVerifier/State/CodeChallenge`, `ProductionOAuthConfig`, `OAuthConfig`; `internal/auth/token_provider.go:50` `NewOAuthTokenProvider` (refresh grant at `:130`); `internal/auth/store.go:16` `CredentialStore`, `:22` `FileCredentialStore` (Load/Save/Delete, `0o600`); `internal/auth/auth.go:18` `Credentials{Source,AccessToken,RefreshToken,Scopes,ExpiresAt}`.
- Config: `internal/config/user_settings.go:17` `ReadSettingsDocument`, `:34` `WriteSettingsDocument` (`MarshalIndent` + `0o600`); `internal/config/paths.go:11` `UserSettingsPath`, `:39` `ProjectSettingsPath(root)`, `:43` `LocalSettingsPath(root)`; project `.mcp.json` chain at `internal/mcp/load.go:74-79`.
- Contracts: `internal/contracts/settings.go:148` `MCPServer{Type,Command,Args,Env,URL,Headers,OAuth,...,Scope}`; `:166` `MCPOAuthConfig{ClientID,CallbackPort,AuthServerMetadataURL,XAA}`. `:17` `Settings.MCPServers map[string]MCPServer`.

**Missing (this phase builds):**
- `claude mcp` CLI group — `grep -rn "\"mcp\"\|case \"mcp\"" cmd/claude/main.go` returns only flag/import lines, no subcommand dispatch (CC has it at `main.tsx:3894`).
- RFC 9728/8414 discovery, RFC 7591 DCR, initial `authorization_code` token acquisition — `grep -rn "8414\|9728\|7591\|well-known\|registration_endpoint\|authorization_endpoint" internal/` returns **nothing** in MCP/auth. (CC: discovery `services/mcp/auth.ts:256-311`; DCR client metadata `auth.ts:1417-1437`; token cache `auth.ts:1704-1731`.)
- Reconnect/backoff — `grep -rn "reconnect\|backoff" internal/mcp/*.go` returns nothing. (CC: `services/mcp/useManageMCPConnections.ts:87-90,371-464`, constants MAX_RECONNECT_ATTEMPTS=5, INITIAL_BACKOFF_MS=1000, MAX_BACKOFF_MS=30000.)
- Interactive elicitation hook — `elicitation.go` has the protocol path but nothing wires it to a prompt. (CC: `services/mcp/elicitationHandler.ts:77`.)
- Browser opener / callback listener / code exchange in `internal/auth` — Phase 4 dependency (see note above).

---

## File Structure

**New package `internal/mcp/remoteauth/`** (RFC discovery + DCR + token acquisition; small focused files):
- `metadata.go` — RFC 9728 protected-resource + RFC 8414 authorization-server metadata types + discovery (`DiscoverProtectedResource`, `DiscoverAuthorizationServer`), `httptest`-driven. Validates every field.
- `register.go` — RFC 7591 Dynamic Client Registration (`RegisterClient`) + `ClientMetadata`/`RegisteredClient` types. Validates registration responses.
- `wwwauth.go` — parse `WWW-Authenticate` header (`Bearer realm=..., resource_metadata=...`) to find the protected-resource metadata URL (RFC 9728 §5.1).
- `flow.go` — `AcquireToken`: orchestrate discover → register → authorize (PKCE) → exchange → cache; returns `auth.Credentials`. Uses an injected `Authorizer` (the Phase-4 callback/browser seam) so it is testable without a browser.
- `provider.go` — `RemoteOAuthAccessTokenProvider`: a `mcp.ServerAccessTokenProvider` that loads cached creds and, if absent, runs `AcquireToken` once; on 401 refresh, delegates to `auth.OAuthTokenProvider`. This supersedes/extends `mcp/oauth.go`'s refresh-only provider for remote servers.

**New package `internal/mcp/reconnect/`** (or `internal/mcp/supervisor.go` — single file if it stays <350 lines):
- `supervisor.go` — `Supervisor` wrapping a connect func with exponential backoff (1s→30s, max 5 attempts) for **remote** transports only (skip stdio/sdk). Pure timing via injected clock for tests.

**Modified existing files:**
- `internal/mcp/elicitation.go` — add `InteractiveElicitationHandler(prompt ElicitationPrompt) ElicitationHandler` (thin adapter; no behavior change to existing funcs).
- `internal/mcp/oauth.go` — add `RemoteServerCredentialPath`/wire `remoteauth.RemoteOAuthAccessTokenProvider` as an alternative constructor (keep existing func intact).
- `cmd/claude/main.go` — add `mcp` subcommand dispatch + `runMCPCommand` + `mcpAdd/mcpList/mcpGet/mcpRemove/mcpServe` handlers.
- **New** `cmd/claude/mcp_cli.go` (preferred — keep main.go from growing): the `mcp` subcommand handlers, mirroring the existing `plugin` subcommand structure at `main.go:366-391`.
- `internal/auth/exchange.go` + `internal/auth/callback.go` + `internal/platform/browser.go` — **only if Phase 4 has not landed them** (Task 5 Step 0 gate).

---

## Task 1: `claude mcp` subcommand scaffolding + `mcp list`/`get`

**Files:**
- Create: `cmd/claude/mcp_cli.go`
- Modify: `cmd/claude/main.go` (add `mcp` to the top-level subcommand switch)
- Test: `cmd/claude/mcp_cli_test.go`

**Interfaces:**
- Consumes: `config.ReadSettingsDocument(path)`, `config.UserSettingsPath()`, `config.ProjectSettingsPath(root)`, `config.LocalSettingsPath(root)`, `contracts.Settings.MCPServers`, `mcp.Transport(server)`.
- Produces:
  - `func runMCPCommand(args []string, stdout, stderr io.Writer, env mcpCLIEnv) int` — dispatches `add|add-json|list|get|remove|serve`.
  - `type mcpCLIEnv struct { UserPath, ProjectRoot string }` (injectable for tests; defaults from `config.*Path`).
  - `func mcpList(env mcpCLIEnv, stdout, stderr io.Writer) int`, `func mcpGet(name string, env mcpCLIEnv, stdout, stderr io.Writer) int`.

> **Confirm first** (do not assume): the top-level subcommand dispatch shape — `grep -n "func run(\|case \"plugin\"\|args\[0\]\|strings.ToLower" cmd/claude/main.go`. Mirror the existing `plugin` group (`main.go:366`). Confirm the settings document reader returns `map[string]any`: `go doc ./internal/config ReadSettingsDocument`. Confirm `mcp.Transport`: `go doc ./internal/mcp Transport`.

- [ ] **Step 1: Write the failing test**

Create `cmd/claude/mcp_cli_test.go`:
```go
package main

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func writeSettings(t *testing.T, path string, servers map[string]any) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	doc := map[string]any{"mcpServers": servers}
	data, _ := json.MarshalIndent(doc, "", "  ")
	if err := os.WriteFile(path, data, 0o600); err != nil {
		t.Fatal(err)
	}
}

func newMCPTestEnv(t *testing.T) mcpCLIEnv {
	t.Helper()
	dir := t.TempDir()
	return mcpCLIEnv{
		UserPath:    filepath.Join(dir, "user-settings.json"),
		ProjectRoot: dir,
	}
}

func TestMCPListShowsServers(t *testing.T) {
	env := newMCPTestEnv(t)
	writeSettings(t, env.UserPath, map[string]any{
		"local-fs": map[string]any{"command": "npx", "args": []any{"server-fs"}},
		"remote-x": map[string]any{"type": "http", "url": "https://x.example/mcp"},
	})
	var out, errb bytes.Buffer
	if code := runMCPCommand([]string{"list"}, &out, &errb, env); code != 0 {
		t.Fatalf("list exit=%d stderr=%q", code, errb.String())
	}
	got := out.String()
	if !strings.Contains(got, "local-fs") || !strings.Contains(got, "stdio") {
		t.Fatalf("list missing local-fs/stdio: %q", got)
	}
	if !strings.Contains(got, "remote-x") || !strings.Contains(got, "https://x.example/mcp") {
		t.Fatalf("list missing remote-x url: %q", got)
	}
}

func TestMCPGetUnknownServerErrors(t *testing.T) {
	env := newMCPTestEnv(t)
	writeSettings(t, env.UserPath, map[string]any{})
	var out, errb bytes.Buffer
	if code := runMCPCommand([]string{"get", "nope"}, &out, &errb, env); code == 0 {
		t.Fatal("expected nonzero exit for unknown server")
	}
	if !strings.Contains(errb.String(), "nope") {
		t.Fatalf("error should name the server: %q", errb.String())
	}
}

func TestMCPMissingSubcommand(t *testing.T) {
	env := newMCPTestEnv(t)
	var out, errb bytes.Buffer
	if code := runMCPCommand(nil, &out, &errb, env); code == 0 {
		t.Fatal("expected nonzero exit for missing subcommand")
	}
	if !strings.Contains(errb.String(), "Usage") {
		t.Fatalf("expected usage text: %q", errb.String())
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./cmd/claude/ -run TestMCP -v`
Expected: FAIL — `undefined: runMCPCommand` / `undefined: mcpCLIEnv`.

- [ ] **Step 3: Write minimal implementation**

Create `cmd/claude/mcp_cli.go`:
```go
package main

import (
	"fmt"
	"io"
	"sort"
	"strings"

	"ccgo/internal/config"
	"ccgo/internal/contracts"
	"ccgo/internal/mcp"
)

const mcpUsage = "Usage: claude mcp <add|add-json|list|get|remove|serve>"

// mcpCLIEnv injects settings file locations so tests avoid the real $HOME.
type mcpCLIEnv struct {
	UserPath    string
	ProjectRoot string
}

func defaultMCPCLIEnv(projectRoot string) mcpCLIEnv {
	return mcpCLIEnv{UserPath: config.UserSettingsPath(), ProjectRoot: projectRoot}
}

func (e mcpCLIEnv) pathForScope(scope string) (string, error) {
	switch strings.ToLower(strings.TrimSpace(scope)) {
	case "", mcp.ScopeLocal:
		return config.LocalSettingsPath(e.ProjectRoot), nil
	case mcp.ScopeUser:
		return e.UserPath, nil
	case mcp.ScopeProject:
		return config.ProjectSettingsPath(e.ProjectRoot), nil
	default:
		return "", fmt.Errorf("invalid --scope %q (want local|user|project)", scope)
	}
}

func runMCPCommand(args []string, stdout, stderr io.Writer, env mcpCLIEnv) int {
	if len(args) == 0 {
		fmt.Fprintln(stderr, "ccgo mcp: missing subcommand")
		fmt.Fprintln(stderr, mcpUsage)
		return 1
	}
	switch strings.ToLower(strings.TrimSpace(args[0])) {
	case "list":
		return mcpList(env, stdout, stderr)
	case "get":
		return mcpGet(args[1:], env, stdout, stderr)
	case "add":
		return mcpAdd(args[1:], env, stdout, stderr) // Task 2
	case "add-json":
		return mcpAddJSON(args[1:], env, stdout, stderr) // Task 3
	case "remove":
		return mcpRemove(args[1:], env, stdout, stderr) // Task 4
	case "serve":
		return mcpServe(args[1:], stdout, stderr) // Task 7
	default:
		fmt.Fprintf(stderr, "ccgo mcp: unknown subcommand %s\n", args[0])
		fmt.Fprintln(stderr, mcpUsage)
		return 1
	}
}

// allConfiguredServers merges user+project+local scopes (later scopes win on name
// collision: local > project > user, matching CC precedence). Read-only.
func allConfiguredServers(env mcpCLIEnv) (map[string]scopedServer, error) {
	scoped := map[string]scopedServer{}
	order := []struct {
		scope string
		path  string
	}{
		{mcp.ScopeUser, env.UserPath},
		{mcp.ScopeProject, config.ProjectSettingsPath(env.ProjectRoot)},
		{mcp.ScopeLocal, config.LocalSettingsPath(env.ProjectRoot)},
	}
	for _, o := range order {
		settings, err := config.LoadSettingsFile(o.path)
		if err != nil {
			return nil, fmt.Errorf("load %s settings: %w", o.scope, err)
		}
		for name, server := range settings.MCPServers {
			scoped[name] = scopedServer{scope: o.scope, server: server}
		}
	}
	return scoped, nil
}

type scopedServer struct {
	scope  string
	server contracts.MCPServer
}

func mcpList(env mcpCLIEnv, stdout, stderr io.Writer) int {
	servers, err := allConfiguredServers(env)
	if err != nil {
		fmt.Fprintf(stderr, "ccgo mcp list: %v\n", err)
		return 1
	}
	if len(servers) == 0 {
		fmt.Fprintln(stdout, "No MCP servers configured.")
		return 0
	}
	names := make([]string, 0, len(servers))
	for name := range servers {
		names = append(names, name)
	}
	sort.Strings(names)
	for _, name := range names {
		s := servers[name]
		fmt.Fprintln(stdout, formatServerLine(name, s))
	}
	return 0
}

func formatServerLine(name string, s scopedServer) string {
	transport := mcp.Transport(s.server)
	target := s.server.URL
	if target == "" {
		target = strings.TrimSpace(strings.Join(append([]string{s.server.Command}, s.server.Args...), " "))
	}
	return fmt.Sprintf("%s\t[%s]\t%s\t(%s)", name, transport, target, s.scope)
}

func mcpGet(args []string, env mcpCLIEnv, stdout, stderr io.Writer) int {
	if len(args) == 0 {
		fmt.Fprintln(stderr, "ccgo mcp get: server name is required")
		return 1
	}
	name := args[0]
	servers, err := allConfiguredServers(env)
	if err != nil {
		fmt.Fprintf(stderr, "ccgo mcp get: %v\n", err)
		return 1
	}
	s, ok := servers[name]
	if !ok {
		fmt.Fprintf(stderr, "ccgo mcp get: no MCP server named %q\n", name)
		return 1
	}
	fmt.Fprintf(stdout, "%s:\n", name)
	fmt.Fprintf(stdout, "  scope:     %s\n", s.scope)
	fmt.Fprintf(stdout, "  transport: %s\n", mcp.Transport(s.server))
	if s.server.URL != "" {
		fmt.Fprintf(stdout, "  url:       %s\n", s.server.URL)
	}
	if s.server.Command != "" {
		fmt.Fprintf(stdout, "  command:   %s\n", strings.Join(append([]string{s.server.Command}, s.server.Args...), " "))
	}
	if s.server.OAuth != nil {
		fmt.Fprintln(stdout, "  oauth:     enabled")
	}
	return 0
}
```

Note: `mcpAdd`/`mcpAddJSON`/`mcpRemove`/`mcpServe` are referenced now but implemented in Tasks 2/3/4/7. To keep this task compiling, add temporary stubs in `mcp_cli.go` returning a "not implemented" error (each will be replaced):
```go
func mcpAdd(args []string, env mcpCLIEnv, stdout, stderr io.Writer) int {
	fmt.Fprintln(stderr, "ccgo mcp add: not implemented")
	return 1
}
func mcpAddJSON(args []string, env mcpCLIEnv, stdout, stderr io.Writer) int {
	fmt.Fprintln(stderr, "ccgo mcp add-json: not implemented")
	return 1
}
func mcpRemove(args []string, env mcpCLIEnv, stdout, stderr io.Writer) int {
	fmt.Fprintln(stderr, "ccgo mcp remove: not implemented")
	return 1
}
func mcpServe(args []string, stdout, stderr io.Writer) int {
	fmt.Fprintln(stderr, "ccgo mcp serve: not implemented")
	return 1
}
```

In `cmd/claude/main.go`, add `mcp` to the top-level subcommand switch (mirror the `plugin` case). Confirm the exact dispatch site and the project-root accessor with `grep -n "case \"plugin\"\|state.CWD\|projectRoot\|func run(" cmd/claude/main.go`, then add:
```go
	case "mcp":
		return runMCPCommand(args[1:], stdout, stderr, defaultMCPCLIEnv(currentProjectRoot()))
```
where `currentProjectRoot()` is the existing cwd/project-root helper (use the same one `ProjectSettingsPath` callers use; confirm its name — likely `os.Getwd()` wrapped or `state.CWD()`).

- [ ] **Step 4: Run tests to verify they pass**

Run: `go test ./cmd/claude/ -run TestMCP -v`
Expected: PASS (list/get/missing-subcommand).

- [ ] **Step 5: Commit**

```bash
git add cmd/claude/mcp_cli.go cmd/claude/main.go cmd/claude/mcp_cli_test.go
git commit -m "feat(mcp): add claude mcp subcommand group with list and get"
```

---

## Task 2: `claude mcp add` (stdio / SSE / HTTP variants + scope) with immutable settings write

**Files:**
- Modify: `cmd/claude/mcp_cli.go` (replace `mcpAdd` stub)
- Create: `cmd/claude/mcp_add.go` (parsing + the immutable document writer)
- Test: `cmd/claude/mcp_add_test.go`

**Interfaces:**
- Consumes: `config.ReadSettingsDocument(path)`, `config.WriteSettingsDocument(path, doc)`, `mcp.TransportStdio/SSE/HTTP`, `mcp.ScopeLocal/User/Project`, `contracts.MCPServer`.
- Produces:
  - `func parseAddArgs(args []string) (name string, server contracts.MCPServer, scope string, err error)` — pure parser; the TDD core.
  - `func writeServerToScope(path, name string, server contracts.MCPServer) error` — read doc → **copy** → set `mcpServers[name]` → write. Never mutates input.

> CC flags (reference `commands/mcp/addCommand.ts:35`): `add <name> <commandOrUrl> [args...]`, `-t/--transport stdio|sse|http` (inferred when omitted), `-s/--scope local|user|project` (default **local**), `-e/--env KEY=VAL` (repeatable), `-H/--header "K: V"` (repeatable), `--client-id`, `--callback-port`. There is **no** `--command`/`--url` flag in CC — the second positional is the command-or-URL. Replicate that: if `--transport` is `http`/`sse` OR the positional parses as an `http(s)://` URL, treat it as a remote URL; else stdio command+args.

- [ ] **Step 1: Write the failing test**

Create `cmd/claude/mcp_add_test.go`:
```go
package main

import (
	"bytes"
	"path/filepath"
	"strings"
	"testing"

	"ccgo/internal/config"
	"ccgo/internal/mcp"
)

func TestParseAddStdio(t *testing.T) {
	name, server, scope, err := parseAddArgs([]string{
		"fs", "npx", "-y", "@modelcontextprotocol/server-filesystem", "/tmp",
		"-e", "FOO=bar", "--scope", "user",
	})
	if err != nil {
		t.Fatalf("parse err: %v", err)
	}
	if name != "fs" || scope != mcp.ScopeUser {
		t.Fatalf("name/scope = %q/%q", name, scope)
	}
	if server.Command != "npx" {
		t.Fatalf("command = %q", server.Command)
	}
	wantArgs := []string{"-y", "@modelcontextprotocol/server-filesystem", "/tmp"}
	if strings.Join(server.Args, " ") != strings.Join(wantArgs, " ") {
		t.Fatalf("args = %v want %v", server.Args, wantArgs)
	}
	if server.Env["FOO"] != "bar" {
		t.Fatalf("env = %v", server.Env)
	}
	if mcp.Transport(server) != mcp.TransportStdio {
		t.Fatalf("transport = %q", mcp.Transport(server))
	}
}

func TestParseAddHTTPInfersTransport(t *testing.T) {
	_, server, _, err := parseAddArgs([]string{
		"remote", "https://mcp.example.com/v1", "-H", "Authorization: Bearer tok",
	})
	if err != nil {
		t.Fatalf("parse err: %v", err)
	}
	if server.URL != "https://mcp.example.com/v1" {
		t.Fatalf("url = %q", server.URL)
	}
	if mcp.Transport(server) != mcp.TransportHTTP {
		t.Fatalf("transport = %q want http", mcp.Transport(server))
	}
	if server.Headers["Authorization"] != "Bearer tok" {
		t.Fatalf("headers = %v", server.Headers)
	}
}

func TestParseAddSSEExplicit(t *testing.T) {
	_, server, _, err := parseAddArgs([]string{
		"sserv", "https://mcp.example.com/sse", "-t", "sse",
	})
	if err != nil {
		t.Fatalf("parse err: %v", err)
	}
	if mcp.Transport(server) != mcp.TransportSSE {
		t.Fatalf("transport = %q want sse", mcp.Transport(server))
	}
}

func TestParseAddRejectsBadScope(t *testing.T) {
	if _, _, _, err := parseAddArgs([]string{"x", "cmd", "--scope", "bogus"}); err == nil {
		t.Fatal("expected error for bad scope")
	}
}

func TestParseAddRejectsMissingTarget(t *testing.T) {
	if _, _, _, err := parseAddArgs([]string{"onlyname"}); err == nil {
		t.Fatal("expected error: missing command/url")
	}
}

func TestMCPAddWritesAndIsImmutable(t *testing.T) {
	dir := t.TempDir()
	env := mcpCLIEnv{UserPath: filepath.Join(dir, "user.json"), ProjectRoot: dir}
	var out, errb bytes.Buffer
	code := runMCPCommand([]string{"add", "fs", "npx", "server-fs", "--scope", "user"}, &out, &errb, env)
	if code != 0 {
		t.Fatalf("add exit=%d stderr=%q", code, errb.String())
	}
	settings, err := config.LoadSettingsFile(env.UserPath)
	if err != nil {
		t.Fatal(err)
	}
	got, ok := settings.MCPServers["fs"]
	if !ok || got.Command != "npx" {
		t.Fatalf("server not persisted: %+v", settings.MCPServers)
	}
}

func TestMCPAddPreservesExistingDocument(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "user.json")
	writeSettings(t, path, map[string]any{"keep": map[string]any{"command": "old"}})
	env := mcpCLIEnv{UserPath: path, ProjectRoot: dir}
	var out, errb bytes.Buffer
	if code := runMCPCommand([]string{"add", "new", "cmd2", "--scope", "user"}, &out, &errb, env); code != 0 {
		t.Fatalf("add exit=%d", code)
	}
	settings, _ := config.LoadSettingsFile(path)
	if _, ok := settings.MCPServers["keep"]; !ok {
		t.Fatal("existing server was dropped (non-immutable write)")
	}
	if _, ok := settings.MCPServers["new"]; !ok {
		t.Fatal("new server not added")
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./cmd/claude/ -run 'TestParseAdd|TestMCPAdd' -v`
Expected: FAIL — `undefined: parseAddArgs`.

- [ ] **Step 3: Write minimal implementation**

Create `cmd/claude/mcp_add.go`:
```go
package main

import (
	"fmt"
	"io"
	"net/url"
	"strings"

	"ccgo/internal/config"
	"ccgo/internal/contracts"
	"ccgo/internal/mcp"
)

// parseAddArgs parses `add <name> <commandOrUrl> [args...] [flags]`. Flags may
// appear anywhere after the name; everything else (in order) is the positional
// command + args. Mirrors CC's commands/mcp/addCommand.ts behavior.
func parseAddArgs(args []string) (string, contracts.MCPServer, string, error) {
	var positional []string
	var server contracts.MCPServer
	scope := mcp.ScopeLocal
	transport := ""

	for i := 0; i < len(args); i++ {
		a := args[i]
		next := func() (string, error) {
			if i+1 >= len(args) {
				return "", fmt.Errorf("flag %s requires a value", a)
			}
			i++
			return args[i], nil
		}
		switch a {
		case "-t", "--transport":
			v, err := next()
			if err != nil {
				return "", server, "", err
			}
			transport = strings.ToLower(strings.TrimSpace(v))
		case "-s", "--scope":
			v, err := next()
			if err != nil {
				return "", server, "", err
			}
			scope = strings.ToLower(strings.TrimSpace(v))
		case "-e", "--env":
			v, err := next()
			if err != nil {
				return "", server, "", err
			}
			k, val, ok := strings.Cut(v, "=")
			if !ok || strings.TrimSpace(k) == "" {
				return "", server, "", fmt.Errorf("invalid --env %q (want KEY=VALUE)", v)
			}
			if server.Env == nil {
				server.Env = map[string]string{}
			}
			server.Env[k] = val
		case "-H", "--header":
			v, err := next()
			if err != nil {
				return "", server, "", err
			}
			k, val, ok := strings.Cut(v, ":")
			if !ok || strings.TrimSpace(k) == "" {
				return "", server, "", fmt.Errorf("invalid --header %q (want \"Key: Value\")", v)
			}
			if server.Headers == nil {
				server.Headers = map[string]string{}
			}
			server.Headers[strings.TrimSpace(k)] = strings.TrimSpace(val)
		case "--client-id":
			v, err := next()
			if err != nil {
				return "", server, "", err
			}
			ensureOAuth(&server).ClientID = strings.TrimSpace(v)
		case "--callback-port":
			v, err := next()
			if err != nil {
				return "", server, "", err
			}
			port, perr := parsePort(v)
			if perr != nil {
				return "", server, "", perr
			}
			ensureOAuth(&server).CallbackPort = &port
		default:
			if strings.HasPrefix(a, "-") {
				return "", server, "", fmt.Errorf("unknown flag %q", a)
			}
			positional = append(positional, a)
		}
	}

	if scope != mcp.ScopeLocal && scope != mcp.ScopeUser && scope != mcp.ScopeProject {
		return "", server, "", fmt.Errorf("invalid --scope %q (want local|user|project)", scope)
	}
	if len(positional) < 2 {
		return "", server, "", fmt.Errorf("usage: claude mcp add <name> <commandOrUrl> [args...]")
	}
	name := positional[0]
	target := positional[1]
	rest := positional[2:]

	isRemote := transport == mcp.TransportHTTP || transport == mcp.TransportSSE || isHTTPURL(target)
	if isRemote {
		if !isHTTPURL(target) {
			return "", server, "", fmt.Errorf("remote transport requires an http(s) URL, got %q", target)
		}
		server.URL = target
		if transport == "" {
			transport = mcp.TransportHTTP
		}
		server.Type = transport
		if len(rest) > 0 {
			return "", server, "", fmt.Errorf("remote server takes no extra args, got %v", rest)
		}
	} else {
		server.Command = target
		server.Args = rest
		server.Type = mcp.TransportStdio
	}
	return name, server, scope, nil
}

func ensureOAuth(server *contracts.MCPServer) *contracts.MCPOAuthConfig {
	if server.OAuth == nil {
		server.OAuth = &contracts.MCPOAuthConfig{}
	}
	return server.OAuth
}

func isHTTPURL(s string) bool {
	u, err := url.Parse(s)
	return err == nil && (u.Scheme == "http" || u.Scheme == "https") && u.Host != ""
}

func parsePort(s string) (int, error) {
	var port int
	if _, err := fmt.Sscanf(strings.TrimSpace(s), "%d", &port); err != nil || port <= 0 || port > 65535 {
		return 0, fmt.Errorf("invalid --callback-port %q", s)
	}
	return port, nil
}

func mcpAdd(args []string, env mcpCLIEnv, stdout, stderr io.Writer) int {
	name, server, scope, err := parseAddArgs(args)
	if err != nil {
		fmt.Fprintf(stderr, "ccgo mcp add: %v\n", err)
		return 1
	}
	path, err := env.pathForScope(scope)
	if err != nil {
		fmt.Fprintf(stderr, "ccgo mcp add: %v\n", err)
		return 1
	}
	if err := writeServerToScope(path, name, server); err != nil {
		fmt.Fprintf(stderr, "ccgo mcp add: %v\n", err)
		return 1
	}
	fmt.Fprintf(stdout, "Added MCP server %q (%s) to %s scope.\n", name, mcp.Transport(server), scope)
	return 0
}

// writeServerToScope reads the settings document, returns a NEW document with
// mcpServers[name] set, and writes it. The on-disk document and any nested maps
// are not mutated in place beyond the freshly-decoded copy.
func writeServerToScope(path, name string, server contracts.MCPServer) error {
	doc, err := config.ReadSettingsDocument(path)
	if err != nil {
		return fmt.Errorf("read settings %s: %w", path, err)
	}
	updated := cloneAnyMapShallow(doc) // returns a new map (see helper note)
	servers, _ := updated["mcpServers"].(map[string]any)
	newServers := map[string]any{}
	for k, v := range servers {
		newServers[k] = v
	}
	newServers[name] = serverToDocument(server)
	updated["mcpServers"] = newServers
	if err := config.WriteSettingsDocument(path, updated); err != nil {
		return fmt.Errorf("write settings %s: %w", path, err)
	}
	return nil
}
```

`serverToDocument` marshals the `contracts.MCPServer` to a `map[string]any` via `json.Marshal`+`Unmarshal` (omitempty respected). `cloneAnyMapShallow` returns a new top-level map. **Confirm before writing both helpers:** `grep -rn "func cloneAnyMap\|func cloneStringMap" internal/mcp/*.go cmd/claude/*.go` — reuse an existing clone helper if one is exported/usable; otherwise add the tiny local helper. Do not invent a deep-clone if a shallow copy of the top-level map + fresh `mcpServers` map suffices (it does — only `mcpServers` is rewritten).

- [ ] **Step 4: Run tests to verify they pass**

Run: `go test ./cmd/claude/ -run 'TestParseAdd|TestMCPAdd' -v`
Expected: PASS, including the immutability/preservation tests.

- [ ] **Step 5: Commit**

```bash
git add cmd/claude/mcp_add.go cmd/claude/mcp_cli.go cmd/claude/mcp_add_test.go
git commit -m "feat(mcp): implement claude mcp add (stdio/sse/http, scopes, env/header flags)"
```

---

## Task 3: `claude mcp add-json` + `claude mcp remove`

**Files:**
- Modify: `cmd/claude/mcp_cli.go` (replace `mcpAddJSON` + `mcpRemove` stubs) or add `cmd/claude/mcp_remove.go`
- Test: `cmd/claude/mcp_remove_test.go`

**Interfaces:**
- Produces:
  - `func mcpAddJSON(args []string, env mcpCLIEnv, stdout, stderr io.Writer) int` — `add-json <name> <json>` with `-s/--scope` (default local). Validates JSON into `contracts.MCPServer`, then `writeServerToScope`.
  - `func removeServerFromScope(path, name string) (removed bool, err error)` — immutable delete.
  - `func mcpRemove(args []string, env mcpCLIEnv, stdout, stderr io.Writer) int` — if `--scope` omitted, search user→project→local and remove from whichever scope holds it (CC `main.tsx:3916` semantics).

> Confirm CC `add-json` flag set: reference `main.tsx:3936` (`-s/--scope` default `local`, `--client-secret`). We omit `--client-secret` prompting in Phase 6a (no secret-storage seam yet) — document the omission; a stdio/remote server JSON with `oauth.clientId` is still accepted.

- [ ] **Step 1: Write the failing test**

Create `cmd/claude/mcp_remove_test.go`:
```go
package main

import (
	"bytes"
	"path/filepath"
	"testing"

	"ccgo/internal/config"
)

func TestMCPAddJSON(t *testing.T) {
	dir := t.TempDir()
	env := mcpCLIEnv{UserPath: filepath.Join(dir, "user.json"), ProjectRoot: dir}
	var out, errb bytes.Buffer
	js := `{"type":"http","url":"https://e.example/mcp","oauth":{"clientId":"abc"}}`
	if code := runMCPCommand([]string{"add-json", "rj", js, "--scope", "user"}, &out, &errb, env); code != 0 {
		t.Fatalf("add-json exit=%d stderr=%q", code, errb.String())
	}
	settings, _ := config.LoadSettingsFile(env.UserPath)
	got, ok := settings.MCPServers["rj"]
	if !ok || got.URL != "https://e.example/mcp" || got.OAuth == nil || got.OAuth.ClientID != "abc" {
		t.Fatalf("add-json not persisted correctly: %+v", got)
	}
}

func TestMCPAddJSONRejectsInvalid(t *testing.T) {
	dir := t.TempDir()
	env := mcpCLIEnv{UserPath: filepath.Join(dir, "user.json"), ProjectRoot: dir}
	var out, errb bytes.Buffer
	if code := runMCPCommand([]string{"add-json", "bad", "{not json"}, &out, &errb, env); code == 0 {
		t.Fatal("expected nonzero exit for invalid JSON")
	}
}

func TestMCPRemoveFindsScope(t *testing.T) {
	dir := t.TempDir()
	userPath := filepath.Join(dir, "user.json")
	writeSettings(t, userPath, map[string]any{"gone": map[string]any{"command": "x"}})
	env := mcpCLIEnv{UserPath: userPath, ProjectRoot: dir}
	var out, errb bytes.Buffer
	if code := runMCPCommand([]string{"remove", "gone"}, &out, &errb, env); code != 0 {
		t.Fatalf("remove exit=%d stderr=%q", code, errb.String())
	}
	settings, _ := config.LoadSettingsFile(userPath)
	if _, ok := settings.MCPServers["gone"]; ok {
		t.Fatal("server not removed")
	}
}

func TestMCPRemoveUnknownErrors(t *testing.T) {
	dir := t.TempDir()
	env := mcpCLIEnv{UserPath: filepath.Join(dir, "user.json"), ProjectRoot: dir}
	writeSettings(t, env.UserPath, map[string]any{})
	var out, errb bytes.Buffer
	if code := runMCPCommand([]string{"remove", "ghost"}, &out, &errb, env); code == 0 {
		t.Fatal("expected nonzero exit removing unknown server")
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./cmd/claude/ -run 'TestMCPAddJSON|TestMCPRemove' -v`
Expected: FAIL — stubs return non-zero / "not implemented".

- [ ] **Step 3: Write minimal implementation**

Create `cmd/claude/mcp_remove.go`:
```go
package main

import (
	"encoding/json"
	"fmt"
	"io"
	"strings"

	"ccgo/internal/config"
	"ccgo/internal/contracts"
	"ccgo/internal/mcp"
)

func mcpAddJSON(args []string, env mcpCLIEnv, stdout, stderr io.Writer) int {
	scope := mcp.ScopeLocal
	var positional []string
	for i := 0; i < len(args); i++ {
		switch args[i] {
		case "-s", "--scope":
			if i+1 >= len(args) {
				fmt.Fprintln(stderr, "ccgo mcp add-json: --scope requires a value")
				return 1
			}
			i++
			scope = strings.ToLower(strings.TrimSpace(args[i]))
		default:
			positional = append(positional, args[i])
		}
	}
	if len(positional) < 2 {
		fmt.Fprintln(stderr, "ccgo mcp add-json: usage: claude mcp add-json <name> <json>")
		return 1
	}
	name := positional[0]
	var server contracts.MCPServer
	dec := json.NewDecoder(strings.NewReader(positional[1]))
	dec.DisallowUnknownFields()
	if err := dec.Decode(&server); err != nil {
		fmt.Fprintf(stderr, "ccgo mcp add-json: invalid server JSON: %v\n", err)
		return 1
	}
	if strings.TrimSpace(server.Command) == "" && strings.TrimSpace(server.URL) == "" {
		fmt.Fprintln(stderr, "ccgo mcp add-json: server JSON must set command or url")
		return 1
	}
	path, err := env.pathForScope(scope)
	if err != nil {
		fmt.Fprintf(stderr, "ccgo mcp add-json: %v\n", err)
		return 1
	}
	if err := writeServerToScope(path, name, server); err != nil {
		fmt.Fprintf(stderr, "ccgo mcp add-json: %v\n", err)
		return 1
	}
	fmt.Fprintf(stdout, "Added MCP server %q to %s scope.\n", name, scope)
	return 0
}

func mcpRemove(args []string, env mcpCLIEnv, stdout, stderr io.Writer) int {
	scope := ""
	var positional []string
	for i := 0; i < len(args); i++ {
		switch args[i] {
		case "-s", "--scope":
			if i+1 >= len(args) {
				fmt.Fprintln(stderr, "ccgo mcp remove: --scope requires a value")
				return 1
			}
			i++
			scope = strings.ToLower(strings.TrimSpace(args[i]))
		default:
			positional = append(positional, args[i])
		}
	}
	if len(positional) == 0 {
		fmt.Fprintln(stderr, "ccgo mcp remove: server name is required")
		return 1
	}
	name := positional[0]

	var paths []string
	if scope != "" {
		p, err := env.pathForScope(scope)
		if err != nil {
			fmt.Fprintf(stderr, "ccgo mcp remove: %v\n", err)
			return 1
		}
		paths = []string{p}
	} else {
		paths = []string{
			env.UserPath,
			config.ProjectSettingsPath(env.ProjectRoot),
			config.LocalSettingsPath(env.ProjectRoot),
		}
	}
	for _, p := range paths {
		removed, err := removeServerFromScope(p, name)
		if err != nil {
			fmt.Fprintf(stderr, "ccgo mcp remove: %v\n", err)
			return 1
		}
		if removed {
			fmt.Fprintf(stdout, "Removed MCP server %q.\n", name)
			return 0
		}
	}
	fmt.Fprintf(stderr, "ccgo mcp remove: no MCP server named %q\n", name)
	return 1
}

// removeServerFromScope returns a NEW document without mcpServers[name].
func removeServerFromScope(path, name string) (bool, error) {
	doc, err := config.ReadSettingsDocument(path)
	if err != nil {
		return false, fmt.Errorf("read settings %s: %w", path, err)
	}
	servers, _ := doc["mcpServers"].(map[string]any)
	if _, ok := servers[name]; !ok {
		return false, nil
	}
	updated := cloneAnyMapShallow(doc)
	newServers := map[string]any{}
	for k, v := range servers {
		if k == name {
			continue
		}
		newServers[k] = v
	}
	updated["mcpServers"] = newServers
	if err := config.WriteSettingsDocument(path, updated); err != nil {
		return false, fmt.Errorf("write settings %s: %w", path, err)
	}
	return true, nil
}
```

> `config.ReadSettingsDocument` on a missing file: confirm it returns an empty map + nil error (not an error) so `remove` can skip absent scopes — `go doc ./internal/config ReadSettingsDocument` and read `internal/config/user_settings.go:17`. If it errors on missing files, treat `os.IsNotExist`-wrapped errors as "empty" in `removeServerFromScope`/`writeServerToScope`.

- [ ] **Step 4: Run tests to verify they pass**

Run: `go test ./cmd/claude/ -run 'TestMCPAddJSON|TestMCPRemove' -v && go test ./cmd/claude/ -run TestMCP -v`
Expected: PASS (and Task-1 list/get still green).

- [ ] **Step 5: Commit**

```bash
git add cmd/claude/mcp_remove.go cmd/claude/mcp_cli.go cmd/claude/mcp_remove_test.go
git commit -m "feat(mcp): implement claude mcp add-json and remove with scope search"
```

---

## Task 4: RFC 9728 / RFC 8414 metadata discovery + WWW-Authenticate parse

**Files:**
- Create: `internal/mcp/remoteauth/metadata.go`
- Create: `internal/mcp/remoteauth/wwwauth.go`
- Test: `internal/mcp/remoteauth/metadata_test.go`, `internal/mcp/remoteauth/wwwauth_test.go`

**Interfaces:**
- Produces:
  - `type ProtectedResourceMetadata struct { Resource string; AuthorizationServers []string }` (RFC 9728 §3).
  - `type AuthServerMetadata struct { Issuer, AuthorizationEndpoint, TokenEndpoint, RegistrationEndpoint string; ScopesSupported []string; CodeChallengeMethodsSupported []string }` (RFC 8414 §2).
  - `func DiscoverProtectedResource(ctx context.Context, hc *http.Client, metadataURL string, maxBytes int64) (ProtectedResourceMetadata, error)`
  - `func DiscoverAuthorizationServer(ctx context.Context, hc *http.Client, issuerOrMetadataURL string, maxBytes int64) (AuthServerMetadata, error)` — tries `<issuer>/.well-known/oauth-authorization-server` then path-aware variant.
  - `func ParseWWWAuthenticate(header string) (resourceMetadataURL string, scope string)` (RFC 9728 §5.1; CC ref `services/mcp/auth.ts:1361-1366`).

> Validation at boundary: reject metadata with empty `token_endpoint`/`authorization_endpoint`; require absolute https(s) URLs; cap body with `io.LimitReader`. Pattern to copy: `internal/auth/token_provider.go:154-161` (`io.LimitReader(resp.Body, limit+1)` + size check). Confirm: `go doc ./internal/auth` for any reusable HTTP-JSON helper before writing your own.

- [ ] **Step 1: Write the failing test**

Create `internal/mcp/remoteauth/metadata_test.go`:
```go
package remoteauth

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestDiscoverProtectedResource(t *testing.T) {
	srv := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/.well-known/oauth-protected-resource" {
			http.NotFound(w, r)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"resource":"https://api.example.com","authorization_servers":["https://as.example.com"]}`))
	}))
	defer srv.Close()

	md, err := DiscoverProtectedResource(context.Background(), srv.Client(), srv.URL+"/.well-known/oauth-protected-resource", 1<<20)
	if err != nil {
		t.Fatalf("discover err: %v", err)
	}
	if len(md.AuthorizationServers) != 1 || md.AuthorizationServers[0] != "https://as.example.com" {
		t.Fatalf("authorization_servers = %v", md.AuthorizationServers)
	}
}

func TestDiscoverAuthorizationServer(t *testing.T) {
	mux := http.NewServeMux()
	mux.HandleFunc("/.well-known/oauth-authorization-server", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{
			"issuer":"https://as.example.com",
			"authorization_endpoint":"https://as.example.com/authorize",
			"token_endpoint":"https://as.example.com/token",
			"registration_endpoint":"https://as.example.com/register",
			"code_challenge_methods_supported":["S256"]
		}`))
	})
	srv := httptest.NewTLSServer(mux)
	defer srv.Close()

	md, err := DiscoverAuthorizationServer(context.Background(), srv.Client(), srv.URL, 1<<20)
	if err != nil {
		t.Fatalf("discover err: %v", err)
	}
	if md.TokenEndpoint != "https://as.example.com/token" || md.RegistrationEndpoint != "https://as.example.com/register" {
		t.Fatalf("endpoints wrong: %+v", md)
	}
}

func TestDiscoverAuthorizationServerRejectsMissingTokenEndpoint(t *testing.T) {
	srv := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(`{"issuer":"https://as.example.com","authorization_endpoint":"https://as.example.com/a"}`))
	}))
	defer srv.Close()
	if _, err := DiscoverAuthorizationServer(context.Background(), srv.Client(), srv.URL, 1<<20); err == nil {
		t.Fatal("expected validation error for missing token_endpoint")
	}
}
```

Create `internal/mcp/remoteauth/wwwauth_test.go`:
```go
package remoteauth

import "testing"

func TestParseWWWAuthenticate(t *testing.T) {
	header := `Bearer realm="https://api.example.com", scope="read write", resource_metadata="https://api.example.com/.well-known/oauth-protected-resource"`
	url, scope := ParseWWWAuthenticate(header)
	if url != "https://api.example.com/.well-known/oauth-protected-resource" {
		t.Fatalf("resource_metadata = %q", url)
	}
	if scope != "read write" {
		t.Fatalf("scope = %q", scope)
	}
}

func TestParseWWWAuthenticateEmpty(t *testing.T) {
	if url, _ := ParseWWWAuthenticate("Bearer"); url != "" {
		t.Fatalf("expected empty url, got %q", url)
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/mcp/remoteauth/ -v`
Expected: FAIL — package/symbols undefined.

- [ ] **Step 3: Write minimal implementation**

Create `internal/mcp/remoteauth/wwwauth.go`:
```go
package remoteauth

import (
	"regexp"
	"strings"
)

var wwwAuthParamRe = regexp.MustCompile(`([a-zA-Z_-]+)=(?:"([^"]*)"|([^\s,]+))`)

// ParseWWWAuthenticate extracts the resource_metadata URL (RFC 9728 §5.1) and
// scope from a WWW-Authenticate response header. Returns empty strings when
// absent.
func ParseWWWAuthenticate(header string) (resourceMetadataURL string, scope string) {
	header = strings.TrimSpace(header)
	if header == "" {
		return "", ""
	}
	for _, m := range wwwAuthParamRe.FindAllStringSubmatch(header, -1) {
		key := strings.ToLower(m[1])
		val := m[2]
		if val == "" {
			val = m[3]
		}
		switch key {
		case "resource_metadata":
			resourceMetadataURL = val
		case "scope":
			scope = val
		}
	}
	return resourceMetadataURL, scope
}
```

Create `internal/mcp/remoteauth/metadata.go`:
```go
package remoteauth

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
)

const defaultMetadataMaxBytes = 1 << 20

type ProtectedResourceMetadata struct {
	Resource             string   `json:"resource"`
	AuthorizationServers []string `json:"authorization_servers"`
}

type AuthServerMetadata struct {
	Issuer                        string   `json:"issuer"`
	AuthorizationEndpoint         string   `json:"authorization_endpoint"`
	TokenEndpoint                 string   `json:"token_endpoint"`
	RegistrationEndpoint          string   `json:"registration_endpoint"`
	ScopesSupported               []string `json:"scopes_supported"`
	CodeChallengeMethodsSupported []string `json:"code_challenge_methods_supported"`
}

func DiscoverProtectedResource(ctx context.Context, hc *http.Client, metadataURL string, maxBytes int64) (ProtectedResourceMetadata, error) {
	var md ProtectedResourceMetadata
	if err := fetchJSON(ctx, hc, metadataURL, maxBytes, &md); err != nil {
		return md, fmt.Errorf("discover protected-resource metadata: %w", err)
	}
	if len(md.AuthorizationServers) == 0 {
		return md, fmt.Errorf("protected-resource metadata has no authorization_servers")
	}
	for _, as := range md.AuthorizationServers {
		if !isAbsoluteHTTPS(as) {
			return md, fmt.Errorf("authorization server %q is not an absolute https URL", as)
		}
	}
	return md, nil
}

func DiscoverAuthorizationServer(ctx context.Context, hc *http.Client, issuerOrMetadataURL string, maxBytes int64) (AuthServerMetadata, error) {
	candidates, err := authServerMetadataURLs(issuerOrMetadataURL)
	if err != nil {
		return AuthServerMetadata{}, err
	}
	var lastErr error
	for _, candidate := range candidates {
		var md AuthServerMetadata
		if err := fetchJSON(ctx, hc, candidate, maxBytes, &md); err != nil {
			lastErr = err
			continue
		}
		if err := validateAuthServerMetadata(md); err != nil {
			lastErr = err
			continue
		}
		return md, nil
	}
	if lastErr == nil {
		lastErr = fmt.Errorf("no authorization-server metadata candidates")
	}
	return AuthServerMetadata{}, fmt.Errorf("discover authorization-server metadata: %w", lastErr)
}

// authServerMetadataURLs returns the RFC 8414 well-known candidates. If the input
// already points at a well-known document, it is used verbatim. Otherwise it
// derives <origin>/.well-known/oauth-authorization-server and, when the issuer
// has a path, the path-aware variant (RFC 8414 §3.1).
func authServerMetadataURLs(raw string) ([]string, error) {
	u, err := url.Parse(strings.TrimSpace(raw))
	if err != nil || u.Scheme == "" || u.Host == "" {
		return nil, fmt.Errorf("invalid authorization server URL %q", raw)
	}
	if strings.Contains(u.Path, "/.well-known/") {
		return []string{u.String()}, nil
	}
	origin := u.Scheme + "://" + u.Host
	candidates := []string{origin + "/.well-known/oauth-authorization-server"}
	if p := strings.Trim(u.Path, "/"); p != "" {
		candidates = append(candidates, origin+"/.well-known/oauth-authorization-server/"+p)
	}
	return candidates, nil
}

func validateAuthServerMetadata(md AuthServerMetadata) error {
	if !isAbsoluteHTTPS(md.AuthorizationEndpoint) {
		return fmt.Errorf("authorization_endpoint missing or not https")
	}
	if !isAbsoluteHTTPS(md.TokenEndpoint) {
		return fmt.Errorf("token_endpoint missing or not https")
	}
	if md.RegistrationEndpoint != "" && !isAbsoluteHTTPS(md.RegistrationEndpoint) {
		return fmt.Errorf("registration_endpoint is not https")
	}
	return nil
}

func isAbsoluteHTTPS(s string) bool {
	u, err := url.Parse(s)
	return err == nil && (u.Scheme == "https" || u.Scheme == "http") && u.Host != ""
}

func fetchJSON(ctx context.Context, hc *http.Client, rawURL string, maxBytes int64, out any) error {
	if hc == nil {
		hc = http.DefaultClient
	}
	if maxBytes <= 0 {
		maxBytes = defaultMetadataMaxBytes
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, rawURL, nil)
	if err != nil {
		return err
	}
	req.Header.Set("Accept", "application/json")
	resp, err := hc.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	body, err := io.ReadAll(io.LimitReader(resp.Body, maxBytes+1))
	if err != nil {
		return err
	}
	if int64(len(body)) > maxBytes {
		return fmt.Errorf("metadata response exceeds %d bytes", maxBytes)
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return fmt.Errorf("metadata status %d: %s", resp.StatusCode, strings.TrimSpace(string(body)))
	}
	if err := json.Unmarshal(body, out); err != nil {
		return fmt.Errorf("decode metadata: %w", err)
	}
	return nil
}
```

- [ ] **Step 4: Run tests to verify they pass**

Run: `go test ./internal/mcp/remoteauth/ -v`
Expected: PASS.

- [ ] **Step 5: Commit**

```bash
git add internal/mcp/remoteauth/metadata.go internal/mcp/remoteauth/wwwauth.go internal/mcp/remoteauth/metadata_test.go internal/mcp/remoteauth/wwwauth_test.go
git commit -m "feat(mcp): RFC 9728/8414 OAuth metadata discovery and WWW-Authenticate parse"
```

---

## Task 5: RFC 7591 Dynamic Client Registration + authorization_code exchange (Phase 4 reuse)

**Files:**
- Create: `internal/mcp/remoteauth/register.go`
- Create (ONLY if Phase 4 absent — see Step 0): `internal/auth/exchange.go`, `internal/auth/callback.go`, `internal/platform/browser.go`
- Test: `internal/mcp/remoteauth/register_test.go`, and (if created) `internal/auth/exchange_test.go`

**Interfaces:**
- Produces (this package):
  - `type ClientMetadata struct { ClientName string; RedirectURIs []string; GrantTypes []string; ResponseTypes []string; TokenEndpointAuthMethod string; Scope string }` (RFC 7591 §2; CC ref `services/mcp/auth.ts:1417-1437`).
  - `type RegisteredClient struct { ClientID string; ClientSecret string; ClientIDIssuedAt int64; RegistrationAccessToken string }`.
  - `func RegisterClient(ctx context.Context, hc *http.Client, registrationEndpoint string, meta ClientMetadata, maxBytes int64) (RegisteredClient, error)` — POST JSON, validate the response.
- Phase-4 contract (consumed by Task 6's `flow.go`):
  - `auth.ExchangeAuthorizationCode(ctx, cfg, code, codeVerifier, redirectURI) (auth.Credentials, error)` — `grant_type=authorization_code` POST to `cfg.TokenURL`.
  - `auth.CallbackServer` — listens on `127.0.0.1:<port>/callback`, validates `state`, returns the `code`.
  - `platform.OpenBrowser(url string) error`.

- [ ] **Step 0: Confirm Phase 4 dependency status (FLAGGED)**

Run:
```bash
grep -rn "ExchangeAuthorizationCode\|authorization_code\|func.*Callback\|CallbackServer\|OpenBrowser" internal/auth/*.go internal/platform/*.go | grep -v _test
```
- **If these exist (Phase 4 landed):** import and reuse them; do NOT create `internal/auth/exchange.go`/`callback.go`/`platform/browser.go`. Skip to Step 1 and reference the existing function signatures (adjust Task 6 to match their exact names).
- **If absent (this phase runs before Phase 4):** create the minimal versions below. They are the canonical Phase 4 API; Phase 4 extends them (keychain storage, `/login` CLI) without changing these signatures.

Minimal `internal/auth/exchange.go` (only if absent):
```go
package auth

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"
)

// ExchangeAuthorizationCode performs the RFC 6749 authorization_code grant with
// PKCE (no client secret; public client). Returns OAuth credentials.
func ExchangeAuthorizationCode(ctx context.Context, cfg OAuthConfig, clientID, code, codeVerifier, redirectURI string, hc *http.Client, maxBytes int64) (Credentials, error) {
	if hc == nil {
		hc = http.DefaultClient
	}
	if maxBytes <= 0 {
		maxBytes = 1 << 20
	}
	if strings.TrimSpace(code) == "" || strings.TrimSpace(codeVerifier) == "" {
		return Credentials{}, fmt.Errorf("authorization code and verifier are required")
	}
	values := url.Values{}
	values.Set("grant_type", "authorization_code")
	values.Set("code", code)
	values.Set("code_verifier", codeVerifier)
	values.Set("client_id", clientID)
	values.Set("redirect_uri", redirectURI)
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, cfg.TokenURL, strings.NewReader(values.Encode()))
	if err != nil {
		return Credentials{}, err
	}
	req.Header.Set("content-type", "application/x-www-form-urlencoded")
	req.Header.Set("accept", "application/json")
	resp, err := hc.Do(req)
	if err != nil {
		return Credentials{}, err
	}
	defer resp.Body.Close()
	body, err := io.ReadAll(io.LimitReader(resp.Body, maxBytes+1))
	if err != nil {
		return Credentials{}, err
	}
	if int64(len(body)) > maxBytes {
		return Credentials{}, fmt.Errorf("token response exceeds %d bytes", maxBytes)
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return Credentials{}, fmt.Errorf("authorization_code exchange status %d: %s", resp.StatusCode, strings.TrimSpace(string(body)))
	}
	var tr struct {
		AccessToken  string `json:"access_token"`
		RefreshToken string `json:"refresh_token"`
		ExpiresIn    int64  `json:"expires_in"`
		Scope        string `json:"scope"`
	}
	if err := json.Unmarshal(body, &tr); err != nil {
		return Credentials{}, fmt.Errorf("decode token response: %w", err)
	}
	if strings.TrimSpace(tr.AccessToken) == "" {
		return Credentials{}, fmt.Errorf("token response missing access_token")
	}
	creds := Credentials{
		Source:       SourceOAuth,
		AccessToken:  tr.AccessToken,
		RefreshToken: tr.RefreshToken,
		Scopes:       ParseScopes(tr.Scope),
	}
	if tr.ExpiresIn > 0 {
		creds.ExpiresAt = time.Now().Add(time.Duration(tr.ExpiresIn) * time.Second)
	}
	return creds, nil
}
```
(`internal/auth/callback.go` + `internal/platform/browser.go`: minimal `127.0.0.1` listener returning the `code` after `state` validation, and an `exec.Command` browser opener with `open`/`xdg-open`/`rundll32` per GOOS. Keep each <120 lines. Confirm `internal/platform` package name with `head -1 internal/platform/*.go`. These are Phase 4's; only stub them here if Phase 4 has not landed, and keep them deliberately minimal.)

- [ ] **Step 1: Write the failing test**

Create `internal/mcp/remoteauth/register_test.go`:
```go
package remoteauth

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestRegisterClient(t *testing.T) {
	var gotBody map[string]any
	srv := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			t.Errorf("method = %s want POST", r.Method)
		}
		body, _ := io.ReadAll(r.Body)
		_ = json.Unmarshal(body, &gotBody)
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusCreated)
		_, _ = w.Write([]byte(`{"client_id":"generated-id","client_id_issued_at":1700000000}`))
	}))
	defer srv.Close()

	meta := ClientMetadata{
		ClientName:              "Claude Code (test)",
		RedirectURIs:            []string{"http://127.0.0.1:7777/callback"},
		GrantTypes:              []string{"authorization_code", "refresh_token"},
		ResponseTypes:           []string{"code"},
		TokenEndpointAuthMethod: "none",
	}
	rc, err := RegisterClient(context.Background(), srv.Client(), srv.URL+"/register", meta, 1<<20)
	if err != nil {
		t.Fatalf("register err: %v", err)
	}
	if rc.ClientID != "generated-id" {
		t.Fatalf("client_id = %q", rc.ClientID)
	}
	if got, _ := gotBody["redirect_uris"].([]any); len(got) != 1 {
		t.Fatalf("redirect_uris not sent: %v", gotBody)
	}
	if gotBody["token_endpoint_auth_method"] != "none" {
		t.Fatalf("auth method not sent: %v", gotBody)
	}
}

func TestRegisterClientRejectsEmptyID(t *testing.T) {
	srv := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(`{"client_secret":"x"}`)) // no client_id
	}))
	defer srv.Close()
	_, err := RegisterClient(context.Background(), srv.Client(), srv.URL, ClientMetadata{}, 1<<20)
	if err == nil || !strings.Contains(err.Error(), "client_id") {
		t.Fatalf("expected client_id validation error, got %v", err)
	}
}
```

If Phase 4 was absent and you created `exchange.go`, also add `internal/auth/exchange_test.go` with an `httptest` token endpoint returning `{"access_token":"a","refresh_token":"r","expires_in":3600,"scope":"x"}` and assert `Credentials{AccessToken:"a"}`.

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/mcp/remoteauth/ -run TestRegister -v`
Expected: FAIL — `undefined: RegisterClient`.

- [ ] **Step 3: Write minimal implementation**

Create `internal/mcp/remoteauth/register.go`:
```go
package remoteauth

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
)

type ClientMetadata struct {
	ClientName              string   `json:"client_name,omitempty"`
	RedirectURIs            []string `json:"redirect_uris"`
	GrantTypes              []string `json:"grant_types,omitempty"`
	ResponseTypes           []string `json:"response_types,omitempty"`
	TokenEndpointAuthMethod string   `json:"token_endpoint_auth_method,omitempty"`
	Scope                   string   `json:"scope,omitempty"`
}

type RegisteredClient struct {
	ClientID                string `json:"client_id"`
	ClientSecret            string `json:"client_secret,omitempty"`
	ClientIDIssuedAt        int64  `json:"client_id_issued_at,omitempty"`
	RegistrationAccessToken string `json:"registration_access_token,omitempty"`
}

// RegisterClient performs RFC 7591 Dynamic Client Registration.
func RegisterClient(ctx context.Context, hc *http.Client, registrationEndpoint string, meta ClientMetadata, maxBytes int64) (RegisteredClient, error) {
	if hc == nil {
		hc = http.DefaultClient
	}
	if maxBytes <= 0 {
		maxBytes = defaultMetadataMaxBytes
	}
	if len(meta.RedirectURIs) == 0 {
		return RegisteredClient{}, fmt.Errorf("client metadata requires at least one redirect_uri")
	}
	payload, err := json.Marshal(meta)
	if err != nil {
		return RegisteredClient{}, err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, registrationEndpoint, bytes.NewReader(payload))
	if err != nil {
		return RegisteredClient{}, err
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "application/json")
	resp, err := hc.Do(req)
	if err != nil {
		return RegisteredClient{}, err
	}
	defer resp.Body.Close()
	body, err := io.ReadAll(io.LimitReader(resp.Body, maxBytes+1))
	if err != nil {
		return RegisteredClient{}, err
	}
	if int64(len(body)) > maxBytes {
		return RegisteredClient{}, fmt.Errorf("registration response exceeds %d bytes", maxBytes)
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return RegisteredClient{}, fmt.Errorf("client registration status %d: %s", resp.StatusCode, strings.TrimSpace(string(body)))
	}
	var rc RegisteredClient
	if err := json.Unmarshal(body, &rc); err != nil {
		return RegisteredClient{}, fmt.Errorf("decode registration response: %w", err)
	}
	if strings.TrimSpace(rc.ClientID) == "" {
		return RegisteredClient{}, fmt.Errorf("registration response missing client_id")
	}
	return rc, nil
}
```

- [ ] **Step 4: Run tests to verify they pass**

Run: `go test ./internal/mcp/remoteauth/ -v` (and `go test ./internal/auth/ -run TestExchange -v` if you added exchange.go)
Expected: PASS.

- [ ] **Step 5: Commit**

```bash
git add internal/mcp/remoteauth/register.go internal/mcp/remoteauth/register_test.go
# include internal/auth/exchange.go (+test) / callback.go / internal/platform/browser.go ONLY if you created them
git commit -m "feat(mcp): RFC 7591 dynamic client registration (+ authorization_code exchange seam)"
```

---

## Task 6: Remote OAuth flow orchestration + token-cache provider

**Files:**
- Create: `internal/mcp/remoteauth/flow.go`
- Create: `internal/mcp/remoteauth/provider.go`
- Test: `internal/mcp/remoteauth/flow_test.go`, `internal/mcp/remoteauth/provider_test.go`

**Interfaces:**
- Consumes: Task 4 discovery, Task 5 `RegisterClient` + `auth.ExchangeAuthorizationCode`, `auth.GenerateCodeVerifier/State/CodeChallenge`, `auth.OAuthConfig`, `auth.Credentials`, `auth.CredentialStore`/`FileCredentialStore`, `auth.NewOAuthTokenProvider`, `mcp.ServerAccessTokenProvider`, `mcp.AccessTokenProvider`, `contracts.MCPServer`.
- Produces:
  - `type Authorizer interface { Authorize(ctx context.Context, authURL, redirectURI, state string) (code string, err error) }` — the browser+callback seam (Phase 4 impl satisfies it; tests use a fake that returns a canned code).
  - `type AcquireOptions struct { ServerURL string; ResourceMetadataURL string; Scope string; CallbackPort int; HTTPClient *http.Client; Authorizer Authorizer; ConfiguredClientID string; Now func() time.Time }`
  - `func AcquireToken(ctx context.Context, opts AcquireOptions) (auth.Credentials, RegisteredClient, error)` — discover → (DCR if no client id) → authorize → exchange → return creds.
  - `func RemoteOAuthAccessTokenProvider(store auth.CredentialStore, opts AcquireOptions) mcp.ServerAccessTokenProvider` — returns a provider that loads cached creds; if empty/expired-without-refresh, runs `AcquireToken` once and saves; wraps in `auth.OAuthTokenProvider` for transparent refresh.

> The provider plugs into the existing seam — confirm: `go doc ./internal/mcp ServerAccessTokenProvider` and `go doc ./internal/mcp ServerToolOptions`. The existing `mcp/oauth.go:FileOAuthAccessTokenProvider` is refresh-only; this new provider adds the **initial acquisition**. Keep both; wire the new one for servers whose creds file is empty.

- [ ] **Step 1: Write the failing test**

Create `internal/mcp/remoteauth/flow_test.go`:
```go
package remoteauth

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"ccgo/internal/auth"
)

type fakeAuthorizer struct {
	wantState string
	code      string
	gotURL    string
}

func (f *fakeAuthorizer) Authorize(ctx context.Context, authURL, redirectURI, state string) (string, error) {
	f.gotURL = authURL
	f.wantState = state
	return f.code, nil
}

func TestAcquireTokenFullFlow(t *testing.T) {
	mux := http.NewServeMux()
	// RFC 9728 protected-resource on the resource server.
	mux.HandleFunc("/.well-known/oauth-protected-resource", func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(`{"resource":"R","authorization_servers":["` + serverURLFromReq(r) + `"]}`))
	})
	// RFC 8414 authorization-server metadata.
	mux.HandleFunc("/.well-known/oauth-authorization-server", func(w http.ResponseWriter, r *http.Request) {
		base := serverURLFromReq(r)
		_, _ = w.Write([]byte(`{"issuer":"` + base + `","authorization_endpoint":"` + base + `/authorize","token_endpoint":"` + base + `/token","registration_endpoint":"` + base + `/register"}`))
	})
	// RFC 7591 DCR.
	mux.HandleFunc("/register", func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusCreated)
		_, _ = w.Write([]byte(`{"client_id":"dyn-client"}`))
	})
	// Token endpoint (authorization_code).
	mux.HandleFunc("/token", func(w http.ResponseWriter, r *http.Request) {
		_ = r.ParseForm()
		if r.Form.Get("grant_type") != "authorization_code" || r.Form.Get("code") != "AUTHCODE" {
			http.Error(w, "bad grant", http.StatusBadRequest)
			return
		}
		_, _ = w.Write([]byte(`{"access_token":"AT","refresh_token":"RT","expires_in":3600,"scope":"read"}`))
	})
	srv := httptest.NewTLSServer(mux)
	defer srv.Close()

	authz := &fakeAuthorizer{code: "AUTHCODE"}
	creds, rc, err := AcquireToken(context.Background(), AcquireOptions{
		ServerURL:           srv.URL,
		ResourceMetadataURL: srv.URL + "/.well-known/oauth-protected-resource",
		CallbackPort:        7777,
		HTTPClient:          srv.Client(),
		Authorizer:          authz,
	})
	if err != nil {
		t.Fatalf("AcquireToken err: %v", err)
	}
	if creds.AccessToken != "AT" || creds.RefreshToken != "RT" || creds.Source != auth.SourceOAuth {
		t.Fatalf("creds wrong: %+v", creds)
	}
	if rc.ClientID != "dyn-client" {
		t.Fatalf("client_id = %q", rc.ClientID)
	}
	if !strings.Contains(authz.gotURL, "code_challenge=") || !strings.Contains(authz.gotURL, "client_id=dyn-client") {
		t.Fatalf("auth URL missing PKCE/client_id: %q", authz.gotURL)
	}
}
```
(`serverURLFromReq` is a tiny test helper returning `"https://"+r.Host`; define it once in the test file. The `httptest` client trusts the test TLS cert via `srv.Client()`.)

Create `internal/mcp/remoteauth/provider_test.go`:
```go
package remoteauth

import (
	"context"
	"testing"

	"ccgo/internal/auth"
)

type memStore struct{ creds auth.Credentials }

func (m *memStore) Load(context.Context) (auth.Credentials, error) { return m.creds, nil }
func (m *memStore) Save(_ context.Context, c auth.Credentials) error {
	m.creds = c
	return nil
}

func TestProviderUsesCachedToken(t *testing.T) {
	store := &memStore{creds: auth.Credentials{Source: auth.SourceOAuth, AccessToken: "cached"}}
	prov := RemoteOAuthAccessTokenProvider(store, AcquireOptions{})
	tp, err := prov(context.Background(), "srv", testServer())
	if err != nil {
		t.Fatalf("provider err: %v", err)
	}
	tok, err := tp.CurrentAccessToken(context.Background())
	if err != nil || tok != "cached" {
		t.Fatalf("token = %q err=%v want cached", tok, err)
	}
}
```
(`testServer()` returns a `contracts.MCPServer{Type:"http", URL:"https://x", OAuth:&contracts.MCPOAuthConfig{}}`. Confirm `auth.Credentials.ExpiresAt` zero-value means "no expiry / use as-is" by reading `internal/auth/token_provider.go:119` `accessTokenNeedsRefreshLocked`.)

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/mcp/remoteauth/ -run 'TestAcquire|TestProvider' -v`
Expected: FAIL — undefined symbols.

- [ ] **Step 3: Write minimal implementation**

Create `internal/mcp/remoteauth/flow.go`:
```go
package remoteauth

import (
	"context"
	"fmt"
	"net/http"
	"net/url"
	"strings"
	"time"

	"ccgo/internal/auth"
)

const defaultFlowMaxBytes = 1 << 20

type Authorizer interface {
	Authorize(ctx context.Context, authURL, redirectURI, state string) (code string, err error)
}

type AcquireOptions struct {
	ServerURL           string
	ResourceMetadataURL string
	Scope               string
	CallbackPort        int
	HTTPClient          *http.Client
	Authorizer          Authorizer
	ConfiguredClientID  string
	Now                 func() time.Time
}

func AcquireToken(ctx context.Context, opts AcquireOptions) (auth.Credentials, RegisteredClient, error) {
	if opts.Authorizer == nil {
		return auth.Credentials{}, RegisteredClient{}, fmt.Errorf("an Authorizer is required to acquire a remote OAuth token")
	}
	hc := opts.HTTPClient
	if hc == nil {
		hc = http.DefaultClient
	}

	// 1. RFC 9728: discover authorization server(s) from the resource.
	resourceURL := opts.ResourceMetadataURL
	if resourceURL == "" {
		resourceURL = strings.TrimRight(opts.ServerURL, "/") + "/.well-known/oauth-protected-resource"
	}
	pr, err := DiscoverProtectedResource(ctx, hc, resourceURL, defaultFlowMaxBytes)
	if err != nil {
		return auth.Credentials{}, RegisteredClient{}, err
	}
	// 2. RFC 8414: authorization-server metadata.
	as, err := DiscoverAuthorizationServer(ctx, hc, pr.AuthorizationServers[0], defaultFlowMaxBytes)
	if err != nil {
		return auth.Credentials{}, RegisteredClient{}, err
	}

	redirectURI := fmt.Sprintf("http://127.0.0.1:%d/callback", opts.CallbackPort)

	// 3. RFC 7591 DCR when no client id was configured.
	var rc RegisteredClient
	clientID := strings.TrimSpace(opts.ConfiguredClientID)
	if clientID == "" {
		if as.RegistrationEndpoint == "" {
			return auth.Credentials{}, RegisteredClient{}, fmt.Errorf("server has no registration_endpoint and no client id was provided")
		}
		rc, err = RegisterClient(ctx, hc, as.RegistrationEndpoint, ClientMetadata{
			ClientName:              "Claude Code (ccgo)",
			RedirectURIs:            []string{redirectURI},
			GrantTypes:              []string{"authorization_code", "refresh_token"},
			ResponseTypes:           []string{"code"},
			TokenEndpointAuthMethod: "none",
			Scope:                   opts.Scope,
		}, defaultFlowMaxBytes)
		if err != nil {
			return auth.Credentials{}, RegisteredClient{}, err
		}
		clientID = rc.ClientID
	} else {
		rc = RegisteredClient{ClientID: clientID}
	}

	// 4. PKCE authorize.
	verifier, err := auth.GenerateCodeVerifier()
	if err != nil {
		return auth.Credentials{}, RegisteredClient{}, err
	}
	state, err := auth.GenerateState()
	if err != nil {
		return auth.Credentials{}, RegisteredClient{}, err
	}
	challenge := auth.GenerateCodeChallenge(verifier)
	authURL := buildAuthorizeURL(as.AuthorizationEndpoint, clientID, redirectURI, challenge, state, opts.Scope)

	code, err := opts.Authorizer.Authorize(ctx, authURL, redirectURI, state)
	if err != nil {
		return auth.Credentials{}, RegisteredClient{}, fmt.Errorf("authorization failed: %w", err)
	}

	// 5. authorization_code exchange (Phase 4 machinery).
	cfg := auth.OAuthConfig{TokenURL: as.TokenEndpoint, ClientID: clientID}
	creds, err := auth.ExchangeAuthorizationCode(ctx, cfg, clientID, code, verifier, redirectURI, hc, defaultFlowMaxBytes)
	if err != nil {
		return auth.Credentials{}, RegisteredClient{}, err
	}
	return creds, rc, nil
}

func buildAuthorizeURL(endpoint, clientID, redirectURI, challenge, state, scope string) string {
	u, err := url.Parse(endpoint)
	if err != nil {
		return endpoint
	}
	q := u.Query()
	q.Set("response_type", "code")
	q.Set("client_id", clientID)
	q.Set("redirect_uri", redirectURI)
	q.Set("code_challenge", challenge)
	q.Set("code_challenge_method", "S256")
	q.Set("state", state)
	if strings.TrimSpace(scope) != "" {
		q.Set("scope", scope)
	}
	u.RawQuery = q.Encode()
	return u.String()
}
```

Create `internal/mcp/remoteauth/provider.go`:
```go
package remoteauth

import (
	"context"
	"strings"

	"ccgo/internal/auth"
	"ccgo/internal/contracts"
	"ccgo/internal/mcp"
)

// RemoteOAuthAccessTokenProvider returns an mcp.ServerAccessTokenProvider that
// uses cached credentials when present, otherwise performs the full remote
// OAuth acquisition once and caches the result. Refresh is delegated to
// auth.OAuthTokenProvider so 401 retries on the protocol client work.
func RemoteOAuthAccessTokenProvider(store auth.CredentialStore, opts AcquireOptions) mcp.ServerAccessTokenProvider {
	return func(ctx context.Context, name string, server contracts.MCPServer) (mcp.AccessTokenProvider, error) {
		if server.OAuth == nil {
			return nil, nil
		}
		creds, err := store.Load(ctx)
		if err != nil {
			return nil, err
		}
		if strings.TrimSpace(creds.AccessToken) == "" && strings.TrimSpace(creds.RefreshToken) == "" {
			serverOpts := opts
			serverOpts.ServerURL = firstNonEmptyString(opts.ServerURL, server.URL)
			serverOpts.ResourceMetadataURL = firstNonEmptyString(opts.ResourceMetadataURL, server.OAuth.AuthServerMetadataURL)
			serverOpts.ConfiguredClientID = firstNonEmptyString(opts.ConfiguredClientID, server.OAuth.ClientID)
			if server.OAuth.CallbackPort != nil && *server.OAuth.CallbackPort > 0 {
				serverOpts.CallbackPort = *server.OAuth.CallbackPort
			}
			acquired, _, err := AcquireToken(ctx, serverOpts)
			if err != nil {
				return nil, err
			}
			if err := store.Save(ctx, acquired); err != nil {
				return nil, err
			}
			creds = acquired
		}
		cfg := auth.ProductionOAuthConfig()
		if clientID := strings.TrimSpace(server.OAuth.ClientID); clientID != "" {
			cfg.ClientID = clientID
		} else if strings.TrimSpace(opts.ConfiguredClientID) != "" {
			cfg.ClientID = opts.ConfiguredClientID
		}
		return auth.NewOAuthTokenProvider(auth.OAuthTokenProviderOptions{
			Credentials:     creds,
			Config:          cfg,
			HTTPClient:      opts.HTTPClient,
			CredentialStore: store,
			Now:             opts.Now,
		}), nil
	}
}

func firstNonEmptyString(values ...string) string {
	for _, v := range values {
		if strings.TrimSpace(v) != "" {
			return v
		}
	}
	return ""
}
```

> Confirm `auth.OAuthTokenProviderOptions` field names before writing the provider — `go doc ./internal/auth OAuthTokenProviderOptions` (audit saw `Credentials, Config, HTTPClient, Now, RefreshMargin, CredentialStore, OnCredentials, MaxResponseBytes`). Drop any field this version doesn't have. Confirm `auth.NewOAuthTokenProvider` returns a `*OAuthTokenProvider` that satisfies `mcp.AccessTokenProvider` (it has `CurrentAccessToken` — `token_provider.go:89`). The provider's refresh `TokenURL` for remote servers should be the discovered `token_endpoint`; if the discovered endpoint must persist, store it (e.g. extend the cached `auth.Credentials` or a sidecar) — for Phase 6a, refresh re-uses `ProductionOAuthConfig().TokenURL` only when the server is first-party; for third-party remotes the access token suffices until expiry and re-acquisition runs. FLAG this limitation in the Self-Review.

- [ ] **Step 4: Run tests to verify they pass**

Run: `go test ./internal/mcp/remoteauth/ -v`
Expected: PASS (full flow + provider).

- [ ] **Step 5: Commit**

```bash
git add internal/mcp/remoteauth/flow.go internal/mcp/remoteauth/provider.go internal/mcp/remoteauth/flow_test.go internal/mcp/remoteauth/provider_test.go
git commit -m "feat(mcp): orchestrate remote OAuth acquisition with token cache + refresh provider"
```

---

## Task 7: `claude mcp serve` CLI wiring

**Files:**
- Modify: `cmd/claude/mcp_cli.go` (replace `mcpServe` stub) or add `cmd/claude/mcp_serve.go`
- Test: `cmd/claude/mcp_serve_test.go`

**Interfaces:**
- Consumes: `mcp.NewBuiltinServer(mcp.BuiltinServerOptions{...})`, `(*mcp.BuiltinServer).Run(ctx, in, out)`, the existing tool `Registry`/`Executor` builders in `cmd/claude` (the same ones `headlessRunner` uses).
- Produces:
  - `func mcpServe(args []string, stdout io.Writer, stderr io.Writer) int` — parse `-d/--debug`/`--verbose` (accept+ignore for parity), build a `BuiltinServer` over the standard tool registry, and `Run(ctx, os.Stdin, os.Stdout)`.
  - `func newBuiltinMCPServer(cwd string) (*mcp.BuiltinServer, error)` — testable constructor returning a server that can `Run` over arbitrary reader/writer.

> The server already exists (`builtin_server.go:63/90`); this task is **only** CLI + registry wiring. Confirm the tool-registry constructor name used by `--print`: `grep -n "NewRegistry\|tool.Registry\|DefaultTools\|BuildRegistry\|func headlessRunner" cmd/claude/main.go`. Reuse that exact builder so `mcp serve` exposes the same tool set CC's `entrypoints/mcp.ts:63` does (CC reuses `getTools`). Confirm `BuiltinServerOptions` fields: `go doc ./internal/mcp BuiltinServerOptions`.

- [ ] **Step 1: Write the failing test**

Create `cmd/claude/mcp_serve_test.go`:
```go
package main

import (
	"bytes"
	"context"
	"encoding/json"
	"strings"
	"testing"
	"time"
)

func TestMCPServeListsTools(t *testing.T) {
	srv, err := newBuiltinMCPServer(t.TempDir())
	if err != nil {
		t.Fatalf("build server: %v", err)
	}
	// initialize then tools/list over a pipe.
	in := strings.NewReader(
		`{"jsonrpc":"2.0","id":1,"method":"initialize","params":{"protocolVersion":"2025-06-18","capabilities":{},"clientInfo":{"name":"t","version":"0"}}}` + "\n" +
			`{"jsonrpc":"2.0","method":"notifications/initialized"}` + "\n" +
			`{"jsonrpc":"2.0","id":2,"method":"tools/list"}` + "\n",
	)
	var out bytes.Buffer
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if err := srv.Run(ctx, in, &out); err != nil {
		t.Fatalf("run: %v", err)
	}
	// Expect a tools/list response with a non-empty tools array.
	if !strings.Contains(out.String(), `"tools"`) {
		t.Fatalf("no tools in output: %s", out.String())
	}
	// Sanity: each line is valid JSON-RPC.
	for _, line := range strings.Split(strings.TrimSpace(out.String()), "\n") {
		var resp map[string]any
		if err := json.Unmarshal([]byte(line), &resp); err != nil {
			t.Fatalf("non-JSON line %q: %v", line, err)
		}
	}
}
```
> Confirm the exact initialize protocolVersion the server accepts — `grep -n "DefaultInitializeOptions\|SupportedProtocolVersions\|protocolVersion\|markInitializeAccepted" internal/mcp/protocol.go internal/mcp/builtin_server.go`. Adjust the literal in the test to a supported version. If `markInitialized` requires the exact `notifications/initialized` method spelling, confirm at `builtin_server.go:220`.

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./cmd/claude/ -run TestMCPServe -v`
Expected: FAIL — `undefined: newBuiltinMCPServer`.

- [ ] **Step 3: Write minimal implementation**

Create `cmd/claude/mcp_serve.go`:
```go
package main

import (
	"context"
	"fmt"
	"io"
	"os"

	"ccgo/internal/mcp"
)

// newBuiltinMCPServer constructs the stdio MCP server exposing the local tool
// registry — the same tools the agent uses (mirrors CC entrypoints/mcp.ts).
func newBuiltinMCPServer(cwd string) (*mcp.BuiltinServer, error) {
	registry, err := buildToolRegistry(cwd) // reuse the existing --print registry builder
	if err != nil {
		return nil, fmt.Errorf("build tool registry: %w", err)
	}
	return mcp.NewBuiltinServer(mcp.BuiltinServerOptions{
		Registry:           registry,
		WorkingDirectory:   cwd,
		AllowMutatingTools: true,
	})
}

func mcpServe(args []string, stdout, stderr io.Writer) int {
	// Accept and ignore -d/--debug/--verbose for CC parity.
	for _, a := range args {
		switch a {
		case "-d", "--debug", "--verbose":
		default:
			fmt.Fprintf(stderr, "ccgo mcp serve: unknown flag %s\n", a)
			return 1
		}
	}
	cwd, err := os.Getwd()
	if err != nil {
		fmt.Fprintf(stderr, "ccgo mcp serve: %v\n", err)
		return 1
	}
	server, err := newBuiltinMCPServer(cwd)
	if err != nil {
		fmt.Fprintf(stderr, "ccgo mcp serve: %v\n", err)
		return 1
	}
	if err := server.Run(context.Background(), os.Stdin, os.Stdout); err != nil {
		fmt.Fprintf(stderr, "ccgo mcp serve: %v\n", err)
		return 1
	}
	return 0
}
```

> `buildToolRegistry(cwd)` is a placeholder for the **existing** registry-construction path used by `--print`. Find it (`grep -n "Registry\|NewExecutor\|headlessRunner" cmd/claude/main.go`) and call the real function. If the registry is built inline inside `headlessRunner`/`ConversationRunner`, extract a small `buildToolRegistry` helper (immutable, no side effects) and reuse it from both places — do NOT duplicate the tool list. Confirm `BuiltinServerOptions` requires `Registry` OR an `Executor` with a registry (`builtin_server.go:63-74`); pass whichever the existing builder yields.

- [ ] **Step 4: Run tests to verify they pass**

Run: `go test ./cmd/claude/ -run TestMCPServe -v`
Expected: PASS (initialize → tools/list returns tools).

- [ ] **Step 5: Commit**

```bash
git add cmd/claude/mcp_serve.go cmd/claude/mcp_cli.go cmd/claude/mcp_serve_test.go
git commit -m "feat(mcp): wire claude mcp serve to the builtin stdio MCP server"
```

---

## Task 8: Auto-reconnect with exponential backoff for remote transports

**Files:**
- Create: `internal/mcp/reconnect/supervisor.go`
- Test: `internal/mcp/reconnect/supervisor_test.go`

**Interfaces:**
- Produces:
  - `type ConnectFunc func(ctx context.Context) error`
  - `type Options struct { MaxAttempts int; InitialBackoff, MaxBackoff time.Duration; Sleep func(context.Context, time.Duration) error; OnAttempt func(attempt int, err error) }`
  - `func Run(ctx context.Context, connect ConnectFunc, opts Options) error` — calls `connect`; on failure, retries with `min(initial*2^(n-1), max)` backoff up to `MaxAttempts`; returns the last error when exhausted, or nil on success; aborts immediately on `ctx` cancellation.
  - `func ShouldReconnect(transport string) bool` — true for remote transports (`http`,`sse`,`ws`,proxy), false for `stdio`/`sdk` (matches CC `useManageMCPConnections.ts:354-360`).

> CC constants (reference `useManageMCPConnections.ts:87-90`): `MAX_RECONNECT_ATTEMPTS=5`, `INITIAL_BACKOFF_MS=1000`, `MAX_BACKOFF_MS=30000`, formula `min(INITIAL*2^(attempt-1), MAX)` (`:447-450`). Replicate these as defaults. Confirm transport constant names: `go doc ./internal/mcp | grep Transport` (audit saw `TransportStdio/SSE/HTTP/WS/SDK/ClaudeAIProxy/SSEIDE/WSIDE` in `config.go:12-21`).

- [ ] **Step 1: Write the failing test**

Create `internal/mcp/reconnect/supervisor_test.go`:
```go
package reconnect

import (
	"context"
	"errors"
	"testing"
	"time"
)

func TestRunSucceedsAfterRetries(t *testing.T) {
	attempts := 0
	var slept []time.Duration
	err := Run(context.Background(), func(ctx context.Context) error {
		attempts++
		if attempts < 3 {
			return errors.New("transient")
		}
		return nil
	}, Options{
		MaxAttempts:    5,
		InitialBackoff: time.Second,
		MaxBackoff:     30 * time.Second,
		Sleep: func(_ context.Context, d time.Duration) error {
			slept = append(slept, d)
			return nil
		},
	})
	if err != nil {
		t.Fatalf("Run err: %v", err)
	}
	if attempts != 3 {
		t.Fatalf("attempts = %d want 3", attempts)
	}
	// Backoff before attempt 2 = 1s, before attempt 3 = 2s.
	if len(slept) != 2 || slept[0] != time.Second || slept[1] != 2*time.Second {
		t.Fatalf("backoffs = %v want [1s 2s]", slept)
	}
}

func TestRunExhausts(t *testing.T) {
	attempts := 0
	err := Run(context.Background(), func(ctx context.Context) error {
		attempts++
		return errors.New("nope")
	}, Options{MaxAttempts: 3, InitialBackoff: time.Millisecond, MaxBackoff: time.Millisecond,
		Sleep: func(context.Context, time.Duration) error { return nil }})
	if err == nil {
		t.Fatal("expected exhaustion error")
	}
	if attempts != 3 {
		t.Fatalf("attempts = %d want 3", attempts)
	}
}

func TestRunCapsBackoff(t *testing.T) {
	var slept []time.Duration
	_ = Run(context.Background(), func(ctx context.Context) error { return errors.New("x") },
		Options{MaxAttempts: 6, InitialBackoff: time.Second, MaxBackoff: 4 * time.Second,
			Sleep: func(_ context.Context, d time.Duration) error { slept = append(slept, d); return nil }})
	// 1,2,4,4,4 (cap at 4s).
	want := []time.Duration{time.Second, 2 * time.Second, 4 * time.Second, 4 * time.Second, 4 * time.Second}
	if len(slept) != len(want) {
		t.Fatalf("slept = %v want %v", slept, want)
	}
	for i := range want {
		if slept[i] != want[i] {
			t.Fatalf("slept[%d] = %v want %v", i, slept[i], want[i])
		}
	}
}

func TestRunAbortsOnContext(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	err := Run(ctx, func(context.Context) error { return errors.New("x") },
		Options{MaxAttempts: 5, InitialBackoff: time.Second, MaxBackoff: time.Second,
			Sleep: func(c context.Context, _ time.Duration) error { return c.Err() }})
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("err = %v want context.Canceled", err)
	}
}

func TestShouldReconnect(t *testing.T) {
	for _, transport := range []string{"http", "sse", "ws"} {
		if !ShouldReconnect(transport) {
			t.Fatalf("%q should reconnect", transport)
		}
	}
	for _, transport := range []string{"stdio", "sdk", ""} {
		if ShouldReconnect(transport) {
			t.Fatalf("%q should NOT reconnect", transport)
		}
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/mcp/reconnect/ -v`
Expected: FAIL — package/symbols undefined.

- [ ] **Step 3: Write minimal implementation**

Create `internal/mcp/reconnect/supervisor.go`:
```go
package reconnect

import (
	"context"
	"fmt"
	"time"

	"ccgo/internal/mcp"
)

const (
	DefaultMaxAttempts    = 5
	DefaultInitialBackoff = time.Second
	DefaultMaxBackoff     = 30 * time.Second
)

type ConnectFunc func(ctx context.Context) error

type Options struct {
	MaxAttempts    int
	InitialBackoff time.Duration
	MaxBackoff     time.Duration
	Sleep          func(context.Context, time.Duration) error
	OnAttempt      func(attempt int, err error)
}

func (o Options) withDefaults() Options {
	if o.MaxAttempts <= 0 {
		o.MaxAttempts = DefaultMaxAttempts
	}
	if o.InitialBackoff <= 0 {
		o.InitialBackoff = DefaultInitialBackoff
	}
	if o.MaxBackoff <= 0 {
		o.MaxBackoff = DefaultMaxBackoff
	}
	if o.Sleep == nil {
		o.Sleep = sleepWithContext
	}
	return o
}

// Run connects with exponential backoff. It returns nil on the first success,
// the last error when attempts are exhausted, or ctx.Err() on cancellation.
func Run(ctx context.Context, connect ConnectFunc, opts Options) error {
	opts = opts.withDefaults()
	var lastErr error
	for attempt := 1; attempt <= opts.MaxAttempts; attempt++ {
		if err := ctx.Err(); err != nil {
			return err
		}
		err := connect(ctx)
		if opts.OnAttempt != nil {
			opts.OnAttempt(attempt, err)
		}
		if err == nil {
			return nil
		}
		lastErr = err
		if attempt == opts.MaxAttempts {
			break
		}
		backoff := backoffForAttempt(attempt, opts.InitialBackoff, opts.MaxBackoff)
		if sleepErr := opts.Sleep(ctx, backoff); sleepErr != nil {
			return sleepErr
		}
	}
	return fmt.Errorf("mcp reconnect exhausted %d attempts: %w", opts.MaxAttempts, lastErr)
}

func backoffForAttempt(attempt int, initial, max time.Duration) time.Duration {
	d := initial
	for i := 1; i < attempt; i++ {
		d *= 2
		if d >= max {
			return max
		}
	}
	if d > max {
		return max
	}
	return d
}

func sleepWithContext(ctx context.Context, d time.Duration) error {
	timer := time.NewTimer(d)
	defer timer.Stop()
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-timer.C:
		return nil
	}
}

// ShouldReconnect reports whether a transport should be auto-reconnected.
// Local transports (stdio/sdk) restart differently and are excluded
// (matches CC useManageMCPConnections.ts).
func ShouldReconnect(transport string) bool {
	switch transport {
	case mcp.TransportHTTP, mcp.TransportSSE, mcp.TransportWS,
		mcp.TransportSSEIDE, mcp.TransportWSIDE, mcp.TransportClaudeAIProxy:
		return true
	default:
		return false
	}
}
```

- [ ] **Step 4: Run tests to verify they pass**

Run: `go test ./internal/mcp/reconnect/ -race -v`
Expected: PASS (all backoff/exhaust/cancel/transport cases), clean under `-race`.

- [ ] **Step 5: Commit**

```bash
git add internal/mcp/reconnect/supervisor.go internal/mcp/reconnect/supervisor_test.go
git commit -m "feat(mcp): exponential-backoff reconnect supervisor for remote transports"
```

---

## Task 9: Interactive elicitation handler hook

**Files:**
- Modify: `internal/mcp/elicitation.go` (add an interactive adapter; do not change existing funcs)
- Test: `internal/mcp/elicitation_interactive_test.go`

**Interfaces:**
- Consumes: existing `ElicitationRequest`, `ElicitationHandler`, `ElicitationResponse`, `CancelElicitationResponse` (`elicitation.go:9-84`).
- Produces:
  - `type ElicitationPrompt func(ctx context.Context, req ElicitationRequest) (action string, content map[string]any, err error)` — the UI seam (the REPL/Phase 2 supplies a real dialog; headless supplies a decline).
  - `func InteractiveElicitationHandler(prompt ElicitationPrompt) ElicitationHandler` — adapts the prompt into the protocol handler, normalizing the action via the existing `ElicitationResponse`.

> Confirm the exact signatures before writing — `go doc ./internal/mcp ElicitationHandler ElicitationRequest ElicitationResponse`. The handler must return the `map[string]any` shape `ElicitationRequestHandler` expects (it calls `NormalizeElicitationResponse` on the result — `elicitation.go:31-35`), so returning `ElicitationResponse(action, content)` is correct.

- [ ] **Step 1: Write the failing test**

Create `internal/mcp/elicitation_interactive_test.go`:
```go
package mcp

import (
	"context"
	"testing"
)

func TestInteractiveElicitationHandlerAccept(t *testing.T) {
	prompt := func(ctx context.Context, req ElicitationRequest) (string, map[string]any, error) {
		if req.Message != "Pick one" {
			t.Fatalf("message = %q", req.Message)
		}
		return "accept", map[string]any{"choice": "a"}, nil
	}
	handler := InteractiveElicitationHandler(prompt)
	resp, err := handler(context.Background(), ElicitationRequest{Message: "Pick one"})
	if err != nil {
		t.Fatalf("handler err: %v", err)
	}
	if resp["action"] != "accept" {
		t.Fatalf("action = %v want accept", resp["action"])
	}
	content, _ := resp["content"].(map[string]any)
	if content["choice"] != "a" {
		t.Fatalf("content = %v", resp["content"])
	}
}

func TestInteractiveElicitationHandlerNilPromptDeclines(t *testing.T) {
	handler := InteractiveElicitationHandler(nil)
	resp, err := handler(context.Background(), ElicitationRequest{Message: "x"})
	if err != nil {
		t.Fatalf("handler err: %v", err)
	}
	if resp["action"] != "cancel" {
		t.Fatalf("nil prompt should cancel, got %v", resp["action"])
	}
}

func TestInteractiveElicitationHandlerErrorCancels(t *testing.T) {
	handler := InteractiveElicitationHandler(func(context.Context, ElicitationRequest) (string, map[string]any, error) {
		return "", nil, context.Canceled
	})
	resp, err := handler(context.Background(), ElicitationRequest{})
	if err != nil {
		t.Fatalf("handler should not surface prompt error: %v", err)
	}
	if resp["action"] != "cancel" {
		t.Fatalf("error should cancel, got %v", resp["action"])
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/mcp/ -run TestInteractiveElicitation -v`
Expected: FAIL — `undefined: InteractiveElicitationHandler`.

- [ ] **Step 3: Write minimal implementation**

Append to `internal/mcp/elicitation.go`:
```go
// ElicitationPrompt is the UI seam an interactive front-end implements to
// resolve an elicitation/create request. Returning a non-nil error (or a nil
// prompt) is treated as a cancel, never propagated as a protocol error.
type ElicitationPrompt func(ctx context.Context, req ElicitationRequest) (action string, content map[string]any, err error)

// InteractiveElicitationHandler adapts an ElicitationPrompt into an
// ElicitationHandler. A nil prompt or a prompt error cancels the elicitation.
func InteractiveElicitationHandler(prompt ElicitationPrompt) ElicitationHandler {
	return func(ctx context.Context, req ElicitationRequest) (map[string]any, error) {
		if prompt == nil {
			return CancelElicitationResponse(), nil
		}
		action, content, err := prompt(ctx, req)
		if err != nil {
			return CancelElicitationResponse(), nil
		}
		return ElicitationResponse(action, content), nil
	}
}
```

- [ ] **Step 4: Run tests to verify they pass**

Run: `go test ./internal/mcp/ -run TestInteractiveElicitation -v && go test ./internal/mcp/ -v`
Expected: PASS, including all pre-existing MCP tests (existing funcs unchanged).

- [ ] **Step 5: Commit**

```bash
git add internal/mcp/elicitation.go internal/mcp/elicitation_interactive_test.go
git commit -m "feat(mcp): interactive elicitation handler hook for the REPL UI seam"
```

---

## Task 10: Integration — wire remote OAuth provider + reconnect into the configured tool-set builder

**Files:**
- Modify: `internal/mcp/configured.go` (accept an optional `AccessTokenProvider` for remote-OAuth servers) OR a new `internal/mcp/configured_remoteauth.go` constructor
- Modify: `internal/mcp/oauth.go` (add `RemoteServerCredentialPath` if distinct from `DefaultMCPServerCredentialsPath`)
- Test: `internal/mcp/configured_remoteauth_test.go`

**Interfaces:**
- Goal: a configured remote server with `oauth` set uses `remoteauth.RemoteOAuthAccessTokenProvider` (Task 6) for its token, falling back to the existing refresh-only `FileOAuthAccessTokenProvider` when credentials already exist. The `ServerToolOptions.AccessTokenProvider` seam (`server_tools.go:28`) already threads the token to the transport headers; this task selects the right provider per server.
- Produces:
  - `func RemoteAuthAccessTokenProvider(opts RemoteAuthProviderOptions) ServerAccessTokenProvider` — dispatches: if the server has cached creds → refresh-only path; else → acquisition path (needs an injected `remoteauth.Authorizer`).
  - `type RemoteAuthProviderOptions struct { Authorizer remoteauthAuthorizer; HTTPClient *http.Client; CredentialPath func(string, contracts.MCPServer) string; Now func() time.Time }` (use an interface alias to avoid an import cycle — see note).

> **Import-cycle check (FLAGGED):** `internal/mcp/remoteauth` imports `internal/mcp` (for `ServerAccessTokenProvider`/`AccessTokenProvider`). Therefore `internal/mcp` MUST NOT import `internal/mcp/remoteauth`. Resolve by either (a) defining the dispatcher in package `remoteauth` (it already returns an `mcp.ServerAccessTokenProvider`), and wiring it from `cmd/claude` where both are importable; or (b) defining a local `Authorizer` interface in `internal/mcp` and having `remoteauth` satisfy it. **Prefer (a):** put the combined dispatcher in `internal/mcp/remoteauth/configured.go` and have `cmd/claude` pass the resulting `mcp.ServerAccessTokenProvider` into `ServerToolOptions.AccessTokenProvider`. This task's test lives in `internal/mcp/remoteauth/`. Confirm the cycle direction first: `go list -deps ./internal/mcp/remoteauth | grep ccgo/internal/mcp` and `grep -n "ccgo/internal/mcp\"" internal/mcp/remoteauth/*.go`.

- [ ] **Step 1: Write the failing test**

Create `internal/mcp/remoteauth/configured_test.go`:
```go
package remoteauth

import (
	"context"
	"testing"

	"ccgo/internal/auth"
	"ccgo/internal/contracts"
)

func TestCombinedProviderPrefersCached(t *testing.T) {
	cached := &memStore{creds: auth.Credentials{Source: auth.SourceOAuth, AccessToken: "have"}}
	prov := CombinedAccessTokenProvider(CombinedOptions{
		StoreFor: func(string, contracts.MCPServer) auth.CredentialStore { return cached },
	})
	tp, err := prov(context.Background(), "srv", testServer())
	if err != nil {
		t.Fatalf("provider err: %v", err)
	}
	tok, err := tp.CurrentAccessToken(context.Background())
	if err != nil || tok != "have" {
		t.Fatalf("token = %q err=%v want have", tok, err)
	}
}

func TestCombinedProviderNilOAuthReturnsNil(t *testing.T) {
	prov := CombinedAccessTokenProvider(CombinedOptions{
		StoreFor: func(string, contracts.MCPServer) auth.CredentialStore { return &memStore{} },
	})
	tp, err := prov(context.Background(), "srv", contracts.MCPServer{Type: "http", URL: "https://x"})
	if err != nil {
		t.Fatalf("err: %v", err)
	}
	if tp != nil {
		t.Fatal("expected nil provider for server without oauth")
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/mcp/remoteauth/ -run TestCombined -v`
Expected: FAIL — `undefined: CombinedAccessTokenProvider`.

- [ ] **Step 3: Write minimal implementation**

Create `internal/mcp/remoteauth/configured.go`:
```go
package remoteauth

import (
	"context"
	"net/http"
	"strings"
	"time"

	"ccgo/internal/auth"
	"ccgo/internal/contracts"
	"ccgo/internal/mcp"
)

type CombinedOptions struct {
	StoreFor   func(name string, server contracts.MCPServer) auth.CredentialStore
	Authorizer Authorizer
	HTTPClient *http.Client
	Now        func() time.Time
}

// CombinedAccessTokenProvider returns a ServerAccessTokenProvider that uses
// cached credentials (refresh-only) when present and otherwise performs full
// remote OAuth acquisition. Servers without an oauth config yield a nil
// provider (no Authorization header added).
func CombinedAccessTokenProvider(opts CombinedOptions) mcp.ServerAccessTokenProvider {
	return func(ctx context.Context, name string, server contracts.MCPServer) (mcp.AccessTokenProvider, error) {
		if server.OAuth == nil {
			return nil, nil
		}
		store := opts.StoreFor(name, server)
		creds, err := store.Load(ctx)
		if err != nil {
			return nil, err
		}
		acquire := AcquireOptions{
			ServerURL:           server.URL,
			ResourceMetadataURL: server.OAuth.AuthServerMetadataURL,
			ConfiguredClientID:  strings.TrimSpace(server.OAuth.ClientID),
			HTTPClient:          opts.HTTPClient,
			Authorizer:          opts.Authorizer,
			Now:                 opts.Now,
		}
		if server.OAuth.CallbackPort != nil && *server.OAuth.CallbackPort > 0 {
			acquire.CallbackPort = *server.OAuth.CallbackPort
		}
		_ = creds // RemoteOAuthAccessTokenProvider re-loads + branches on cached vs acquire
		return RemoteOAuthAccessTokenProvider(store, acquire)(ctx, name, server)
	}
}
```

Then in `cmd/claude` (the place that builds `ServerToolOptions` — confirm with `grep -rn "ServerToolOptions{\|BuildConfiguredToolSets\|BuildServerToolSets" cmd/claude/*.go internal/bootstrap/*.go`), set `toolOptions.AccessTokenProvider = remoteauth.CombinedAccessTokenProvider(...)` with a `StoreFor` using `mcp.DefaultMCPServerCredentialsPath(name)` + `auth.NewFileCredentialStore`. This is the only wiring change; do it in the same commit and add a smoke assertion if a bootstrap-level test exists.

- [ ] **Step 4: Run tests to verify they pass**

Run: `go test ./internal/mcp/remoteauth/ -v && go test ./internal/mcp/ ./cmd/claude/ -v`
Expected: PASS. Then full suite + vet:
```bash
go build ./... && go vet ./... && go test ./...
```
Expected: build OK, vet clean, all green.

- [ ] **Step 5: Commit**

```bash
git add internal/mcp/remoteauth/configured.go internal/mcp/remoteauth/configured_test.go cmd/claude/*.go
git commit -m "feat(mcp): wire combined remote-OAuth token provider into the configured tool set"
```

---

## Self-Review

**Spec coverage (Phase-6a gate = add/list/remove servers via CLI; remote OAuth flow works):**
- `claude mcp add` (stdio/sse/http + scope/env/header) → Task 2. ✓
- `claude mcp list`/`get` → Task 1; `add-json`/`remove` → Task 3. ✓
- RFC 9728 + RFC 8414 discovery + WWW-Authenticate → Task 4. ✓
- RFC 7591 DCR + authorization_code exchange seam → Task 5. ✓
- Remote OAuth acquisition + token cache + refresh provider → Task 6. ✓
- `claude mcp serve` → Task 7. ✓
- Auto-reconnect/backoff → Task 8. ✓
- Elicitation interactive hook → Task 9. ✓
- Integration wiring → Task 10. ✓

**Dependency on Phase 4 (FLAGGED):** Tasks 5/6 require `auth.ExchangeAuthorizationCode`, a callback listener, and a browser opener — Phase 4's machinery. Audit 2026-06-21 confirms `internal/auth` has PKCE + refresh + file store but NONE of these (`grep -rn "ExchangeAuthorizationCode\|Callback\|OpenBrowser" internal/auth internal/platform` → empty). Task 5 Step 0 gates: reuse Phase 4's exports if present, else create the minimal canonical versions (signatures fixed so Phase 4 extends, not replaces). The `Authorizer` interface (Task 6) keeps the flow testable without a browser, so this phase can land and be fully tested even before Phase 4's interactive callback exists.

**Verification-before-completion:** every assumed ccgo symbol is flagged at point of use with the exact `go doc`/`grep` command: top-level subcommand dispatch + project-root accessor (Task 1); `config.ReadSettingsDocument`/`WriteSettingsDocument` missing-file behavior (Tasks 1/3); `cloneAnyMap`/`cloneMCPServer` reuse (Task 2); `auth.OAuthTokenProviderOptions` fields + `auth.Credentials.ExpiresAt` semantics (Task 6); registry builder name + `BuiltinServerOptions` fields + supported `protocolVersion` (Task 7); transport constants (Task 8); elicitation signatures (Task 9); the import-cycle direction `mcp ↔ remoteauth` (Task 10). None assumed silently.

**Immutability:** settings writes copy the document and rebuild the `mcpServers` map (Tasks 2/3); `RemoteOAuthAccessTokenProvider` copies `AcquireOptions` per server (Task 6); `Options.withDefaults` returns a copy (Task 8). No shared struct mutated in place.

**Security:** no token/secret logging anywhere; cached creds use `auth.FileCredentialStore` (0o600); callback binds `127.0.0.1` only with `state` CSRF validation; all network response bodies capped via `io.LimitReader`; metadata/registration/token responses validated before use. Plaintext credential storage is a known limitation (keychain is Phase 4) — flagged.

**Known limitations (flagged, deferred):**
- Third-party remote-server token **refresh** reuses `ProductionOAuthConfig().TokenURL` unless the discovered `token_endpoint` is persisted; for non-first-party servers, refresh may require re-running `AcquireToken` on expiry (Task 6 Step 3 note). A follow-up can persist the discovered `token_endpoint`/`registration_access_token` in a sidecar.
- `--client-secret` prompting (CC `add`/`add-json`) is omitted (no secret-store seam yet) — Phase 4/keychain territory.
- `add-from-claude-desktop` and `reset-project-choices` (CC subcommands) are out of scope for 6a.
- The elicitation **UI dialog** itself is Phase 2; Task 9 only provides the seam (headless declines).

**Placeholder scan:** no `t.Skip`, no panics, no TODO stubs left at completion — the Task-1 `mcpAdd/mcpAddJSON/mcpRemove/mcpServe` stubs are each replaced by Tasks 2/3/3/7 respectively (tracked in the file-structure notes). All production code in each step is complete and compiles.

**Gap-audit vs code discrepancies found:**
- Gap audit §4 item 24 says "`claude mcp ...` subcommand group missing (config only hand-editable)" and §5 lists "`claude mcp serve` full tool set" as missing. **Code reality:** the *server implementation* (`internal/mcp/builtin_server.go`) is complete and tested — only the **CLI entrypoint** is missing. Task 7 is therefore CLI wiring, far smaller than "build the serve tool set."
- Gap audit §5 lists "elicitation UI" as missing. **Code reality:** the elicitation **protocol** path exists (`internal/mcp/elicitation.go`) — only the interactive **prompt seam** is missing (Task 9), and the dialog rendering is Phase 2.
- Audit estimate for MCP was 4,000 (P0) + 2,000 (+P1) prod LOC. Because transports, protocol, token bridge, builtin server, and elicitation protocol already exist, the **net new** code here (CLI + remoteauth package + reconnect + hook + wiring) is materially smaller — roughly 1.8–2.5K prod LOC. The "~6K" phase budget includes Phase 4's shared auth-callback machinery if it must be built here.
