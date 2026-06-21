# Auth / OAuth (Phase 4) Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

> ⚠️ **ToS GRAY-ZONE — READ FIRST (policy, not technical).** Interactive OAuth login here uses the
> official Claude Code client's `client_id` (`9d1c250a-e61b-44d9-88ed-5944d1962f5e`) and Anthropic's
> production OAuth endpoints. This is **technically reproducible but a Terms-of-Service / account-policy
> gray area** (master-roadmap §1 "Gray zone (IN, flagged risk)", §7 risk 2; gap-audit §"Caveat on the
> gray-zone item"). Per master-roadmap §7 the *policy* decision (ship it / scope it / API-key-only) is
> outside this plan. This plan therefore makes OAuth login **opt-in and clearly labelled**: the
> `/login` and `claude auth login` paths emit a one-line consent/warning before opening a browser, and
> the flow is gated behind an explicit confirmation (Task 5). API-key auth (env / `apiKeyHelper`) is the
> default and needs none of this. **Do not** remove that guard. **Do not** hardcode any secret — the
> public `client_id` is not a secret (PKCE has no client secret); never log tokens.

**Goal:** Let a brand-new user authenticate from zero. Implement the PKCE *authorization-code* login
that ccgo is missing: spin up a localhost callback HTTP listener on an ephemeral port, open the system
browser to the authorize URL, validate the returned `state` (CSRF) + `code`, exchange the code for
tokens at the token endpoint, persist them, and wire it to `/login` / `/logout` slash commands and a
`claude auth` CLI subcommand. Replace plaintext credential storage with an OS keychain on macOS
(Linux/Windows fall back to the existing chmod-0600 file, matching CC). Add `apiKeyHelper` resolution.

**Architecture:** ccgo already has every PKCE *primitive* (`internal/auth/oauth.go`:
`GenerateCodeVerifier`/`GenerateState`/`GenerateCodeChallenge`/`BuildAuthURL`, plus `ProductionOAuthConfig`
with the real endpoints) and a *refresh-token* exchanger (`internal/auth/token_provider.go`). What is
absent (verified below) is the **front half** of the flow: callback listener, browser open, and the
`authorization_code` exchange. We add four small, independently-testable seams in `internal/auth/`:
(1) `CallbackListener` — `net/http` server on `127.0.0.1:0`, validates `state`, captures `code`,
returns a browser success page; (2) `BrowserOpener` interface + `osBrowserOpener` (real) and an
injected fake so tests never open a browser; (3) `ExchangeAuthorizationCode` — POST `grant_type=
authorization_code` to `OAuthConfig.TokenURL`, decoded into the existing `Credentials`; (4) `LoginFlow`
— orchestrates listener → URL → browser → wait → exchange → store, all over `httptest`-able seams.
Storage gains a `KeychainCredentialStore` implementing the existing `CredentialStore` interface (so the
rest of the codebase is untouched) that shells to `/usr/bin/security` on macOS and falls back to the
existing `FileCredentialStore` elsewhere. Finally `apiKeyHelper` resolution lands as a tiny cached
shell-command runner that feeds the existing `Credentials{Source: SourceAPIKey}` path. The `/login`
`/logout` commands route through the existing builtin-command machinery; `claude auth` adds a
top-level CLI subcommand mirroring the existing `claude plugin` dispatch.

**Tech Stack:** Go 1.26; **no new third-party deps** — `net/http`, `net/http/httptest`, `os/exec`,
`runtime` only (all stdlib). `golang.org/x/sys v0.46.0` is *already* an indirect dep (`go.mod`) and is
available if a future Windows wincred path is added, but this phase needs no syscall code. Existing
packages: `internal/auth`, `internal/platform`, `internal/commands`, `internal/conversation`,
`internal/contracts`, `internal/config`, `internal/bootstrap`, `cmd/claude`.

## Global Constraints

Copied verbatim from master-roadmap §6 (apply to this plan):

- **Module/toolchain:** `ccgo`, `go 1.26` (from `go.mod`).
- **Immutability (CRITICAL):** never mutate shared structs in place; return new copies. Copy the
  `conversation.Runner` value per turn before setting `OnEvent`/`Tools.Asker` (existing pattern).
  `permissions.Engine.ApplyUpdate` already returns a **new** engine — honor that.
- **Many small files:** one responsibility per file; target 150–350 lines (800 hard max).
- **Errors handled explicitly at every level; never swallow.** Terminal raw-mode `restore` and any
  acquired resource MUST be released on every exit path (`defer`).
- **Input validation at boundaries:** validate all external data (API responses, user input, file
  content, MCP server output); fail fast with clear messages.
- **No new third-party deps** unless the plan justifies it explicitly. Phase 1 added only
  `golang.org/x/term`. No bubbletea/tcell/charm.
- **Non-TTY safety:** interactive paths MUST NOT call `term.MakeRaw` when stdin/stdout isn't a tty;
  fall back to line mode. Tests MUST NOT depend on a real tty.
- **TDD:** every task writes a failing test first, then minimal code. Commit after each task.
  Run package tests with `go test ./internal/<pkg>/ -run TestName -v`; full suite `go test ./...`.
- **Verify against real code, distrust roadmap docs:** every assumed type name, field, constant, or
  CC behavior MUST be confirmed with `go doc`/`grep` (ccgo side) or by reading
  `/Users/sqlrush/agent/claude-code/src` (CC side) before writing the test — flag the exact command
  at the point of use, as Phase 1's plan does.
- **Security:** no hardcoded secrets; tokens in keychain not plaintext (Phase 4); sandbox flag must
  actually enforce (Phase 7); never leak sensitive data in errors.

**Phase-4-specific security rules (in addition):**
- The `client_id` is the public official OAuth client identifier (no client secret — PKCE). It is
  already in the repo (`internal/auth/oauth.go:49`) and is **not** a secret. Do not invent new secrets.
- **Never** include `access_token`, `refresh_token`, `code`, or `code_verifier` in any returned error,
  log line, or browser page. Token-exchange/refresh errors surface status + a *generic* message only.
- **Validate every external input:** the callback HTTP request (path, `state` exact-match, presence of
  `code`, reject any `error=` param from the IdP), and the token-endpoint JSON response (status,
  size-limit via `io.LimitReader`, required `access_token` non-empty).
- The login flow is **opt-in** (gray-zone guard above); the consent line must remain.

---

## Code-verified current state (do NOT trust roadmap prose; these were grepped/read 2026-06-21)

**ccgo — what already EXISTS (`internal/auth/`):**
- `oauth.go`: `ProductionOAuthConfig()` with the exact endpoints (`TokenURL =
  https://platform.claude.com/v1/oauth/token`, `ConsoleAuthorizeURL`, `ClaudeAIAuthorizeURL`,
  `ManualRedirectURL = https://platform.claude.com/oauth/code/callback`, `ClientID =
  9d1c250a-e61b-44d9-88ed-5944d1962f5e`). PKCE: `GenerateCodeVerifier`/`GenerateState`/
  `GenerateCodeChallenge` (S256). `BuildAuthURL(AuthURLParams)` already emits
  `redirect_uri = http://localhost:<Port>/callback` (oauth.go:115), `code_challenge_method=S256`,
  `state`, `scope`. Scopes: `AllOAuthScopes()`, `ConsoleOAuthScopes`, `ClaudeAIOAuthScopes`.
  `IsOAuthTokenExpired`. Beta header const `OAuthBetaHeader = "oauth-2025-04-20"`.
- `auth.go`: `Credentials{Source, APIKey, AccessToken, RefreshToken, Scopes, ExpiresAt}`,
  `CredentialSource` (`SourceNone`/`SourceAPIKey`/`SourceOAuth`), `FromEnv()`, `(Credentials).Validate()`.
- `store.go`: `CredentialStore` **interface** (`Load`/`Save`/`Delete`), `FileCredentialStore` (atomic
  temp-file write, `chmod 0o600`, dir `0o700`), `DefaultCredentialsPath()` =
  `filepath.Join(platform.ClaudeHomeDir(), "credentials.json")`.
- `token_provider.go`: `OAuthTokenProvider` — does the **refresh_token** grant only
  (`refreshAccessTokenLocked`, `grant_type=refresh_token`), persists via `CredentialStore`,
  `oauthTokenResponse{AccessToken,RefreshToken,ExpiresIn,Scope,TokenType}`, size-limited body read.

**ccgo — what is ABSENT (the gap this phase fills) — verified with:**
`grep -rn "authorization_code\|callback\|Exchange\|listen\|Listen\|http.Server\|browser\|openBrowser\|exec.Command" internal/auth/`
→ only `BuildAuthURL`'s redirect string + tests; **no callback listener, no `authorization_code`
exchange, no browser open** (gap-audit §4.C item 7, §2 row "OAuth PKCE primitives … ⚠️ no callback +
no code exchange").
- No keychain: `grep -rn "keychain\|Keychain\|security\|secret-tool\|wincred" internal/ cmd/` → nothing
  (gap-audit §"Config/Skills": "token keychain (not plaintext)" missing).
- `apiKeyHelper`: the **setting** exists (`internal/contracts/settings.go:5` `APIKeyHelper string`,
  merged at `internal/config/settings.go:40`) but **nothing executes it** — `grep -rn "apiKeyHelper"
  internal/ cmd/` shows only the struct field + JSON key, no runner.

**CC reference anchors (TypeScript, `/Users/sqlrush/agent/claude-code/src`) — read, cite, do not copy:**
- `services/oauth/auth-code-listener.ts`: `createServer()`; `listen(port ?? 0, 'localhost')` (ephemeral
  port, host `localhost`); callback path default `/callback`; rejects non-callback paths 404; extracts
  `code`+`state`; **state mismatch → HTTP 400 "Invalid state parameter"** (lines ~164-169); missing code
  → 400.
- `services/oauth/client.ts`: `exchangeCodeForTokens()` POST to `TOKEN_URL`, **JSON** body
  `{grant_type:"authorization_code", code, redirect_uri:"http://localhost:${port}/callback", client_id,
  code_verifier, state}`, 15s timeout. `buildAuthUrl()` matches ccgo's `BuildAuthURL`.
- `utils/browser.ts`: `openBrowser(url)` validates `http/https`, honors `$BROWSER`, then macOS `open`,
  Windows `rundll32 url,OpenURL`, else `xdg-open` (via `execFileNoThrow`).
- `constants/oauth.ts`: `PROD_OAUTH_CONFIG` — same endpoints/`client_id`/scopes ccgo already has.
- `utils/secureStorage/`: `index.ts` → **macOS = keychain w/ plaintext fallback; Linux/Windows =
  plaintext only** (`// TODO: add libsecret`). `macOsKeychainStorage.ts` drives `/usr/bin/security`
  (`find-/add-/delete-generic-password`, value hex-encoded, written via `security -i` stdin to avoid
  leaking secrets in argv). `macOsKeychainHelpers.ts`: service name base **`Claude Code`**, OAuth
  credentials suffix **`-credentials`** (→ `Claude Code-credentials`), account = `$USER` /
  `claude-code-user`; 30s read cache. `plainTextStorage.ts`: `.credentials.json`, `chmod 0o600`.
- `utils/auth.ts`: `_executeApiKeyHelper()` = `execa(cmd, {shell:true, timeout:10*60_000})`, stdout
  trimmed → bearer; **5-min cache** (`DEFAULT_API_KEY_HELPER_TTL = 5*60*1000`), env TTL override
  `CLAUDE_CODE_API_KEY_HELPER_TTL_MS`. `services/api/client.ts`: helper output →
  `Authorization: Bearer <key>`.

**Discrepancy noted (gap-audit vs code):** gap-audit §4.C item 7 says "**no callback listener, no
browser open, no `authorization_code` exchange**" — confirmed accurate. But the audit's wording "has
refresh only" understates the surrounding scaffolding: `BuildAuthURL`, every PKCE primitive, the exact
endpoints, and `ClaudeAIOAuthScopes` are **already present and tested** (`oauth_test.go`). So Phase 4 is
mostly the front-half + storage swap + helper, not green-field OAuth — cheaper than the audit's
2,000-LOC line item implies.

**Keychain dependency decision (master §6 requires justification for any new dep):** **No new
dependency.** CC itself does NOT use a Go-style native keyring binding — on macOS it shells to the
system `/usr/bin/security` CLI, and on Linux/Windows it uses **no** OS keyring (plaintext `chmod 0600`).
We mirror exactly that: `os/exec` to `/usr/bin/security` on macOS (stdlib), and the existing
`FileCredentialStore` (already `chmod 0600`) as the cross-platform fallback. This avoids
`github.com/zalando/go-keyring`/`99designs/keyring` (both pull cgo/dbus/transitive deps) while matching
CC's actual behavior and the security goal ("tokens in keychain not plaintext" *on the platform CC
secures, macOS*). A Linux Secret Service / Windows Cred Manager path is explicitly deferred (CC has a
`// TODO: add libsecret` too); the `KeychainStore` interface (Task 6) leaves room to add them later
without touching callers.

---

## File Structure

**New files in `internal/auth/`:**
- `browser.go` — `BrowserOpener` interface; `osBrowserOpener` (macOS `open` / Windows `rundll32` /
  else `xdg-open`, `$BROWSER` override, http/https validation). One responsibility.
- `callback.go` — `CallbackListener`: `net/http` server on `127.0.0.1:0`; `Wait` returns the validated
  `code`; state CSRF check; success/error browser HTML.
- `exchange.go` — `ExchangeAuthorizationCode(ctx, http.Client, OAuthConfig, ExchangeParams) (Credentials, error)`:
  the `authorization_code` POST + response validation. Reuses `oauthTokenResponse` shape.
- `login.go` — `LoginFlow` orchestrator (listener → BuildAuthURL → browser → Wait → Exchange → Store).
- `keychain.go` — `KeychainStore` interface; `macOSKeychainStore` (`/usr/bin/security`);
  `NewKeychainCredentialStore(path)` → returns a `CredentialStore` that prefers keychain on macOS,
  falls back to `FileCredentialStore`.
- `apikey_helper.go` — `APIKeyHelperResolver`: cached shell-command runner; `Resolve(ctx) (string, error)`.

**Modified existing files:**
- `internal/commands/slash.go` — add `LocalCommandResultLogin`/`LocalCommandResultLogout` consts +
  dispatch cases; add `login`/`logout` to `BuiltinCommands()` in `registry.go`.
- `internal/conversation/run.go` — handle the two new `LocalCommandResult` types (surface a text result
  plus a typed signal the REPL/headless caller acts on).
- `cmd/claude/main.go` — add top-level `claude auth` dispatch (mirror `claude plugin` at main.go:197),
  with `login`/`logout`/`status` subcommands; add `apiKeyHelper` to credential resolution.

---

## Task 1: Browser opener seam (cross-platform, injectable, no real browser in tests)

**Files:**
- Create: `internal/auth/browser.go`
- Test: `internal/auth/browser_test.go`

**Interfaces:**
- Produces:
  - `type BrowserOpener interface { Open(url string) error }`
  - `type osBrowserOpener struct{ runner func(name string, args ...string) error }`
  - `func NewOSBrowserOpener() *osBrowserOpener`
  - `func browserCommand(goos string, url string) (name string, args []string)` — pure; the TDD core.
  - `func validateBrowserURL(raw string) error` — only `http`/`https` allowed.

- [ ] **Step 1: Write the failing test**

Create `internal/auth/browser_test.go`:
```go
package auth

import (
	"errors"
	"strings"
	"testing"
)

func TestBrowserCommand(t *testing.T) {
	cases := []struct {
		goos     string
		wantName string
		wantArg0 string
	}{
		{"darwin", "open", "https://example.com/x"},
		{"linux", "xdg-open", "https://example.com/x"},
		{"windows", "rundll32", "url.dll,FileProtocolHandler"},
	}
	for _, tc := range cases {
		t.Run(tc.goos, func(t *testing.T) {
			name, args := browserCommand(tc.goos, "https://example.com/x")
			if name != tc.wantName {
				t.Fatalf("name = %q want %q", name, tc.wantName)
			}
			if len(args) == 0 || args[0] != tc.wantArg0 {
				t.Fatalf("args = %v want first %q", args, tc.wantArg0)
			}
		})
	}
}

func TestValidateBrowserURL(t *testing.T) {
	if err := validateBrowserURL("https://platform.claude.com/oauth/authorize"); err != nil {
		t.Fatalf("https should be valid: %v", err)
	}
	if err := validateBrowserURL("file:///etc/passwd"); err == nil {
		t.Fatal("file:// must be rejected")
	}
	if err := validateBrowserURL("javascript:alert(1)"); err == nil {
		t.Fatal("javascript: must be rejected")
	}
}

func TestOSBrowserOpenerInvokesRunner(t *testing.T) {
	var gotName string
	var gotArgs []string
	op := &osBrowserOpener{runner: func(name string, args ...string) error {
		gotName, gotArgs = name, args
		return nil
	}}
	if err := op.Open("https://example.com/cb"); err != nil {
		t.Fatalf("Open err: %v", err)
	}
	if gotName == "" || len(gotArgs) == 0 {
		t.Fatalf("runner not invoked: name=%q args=%v", gotName, gotArgs)
	}
	// The URL must appear in the argv (exact position is OS-dependent).
	if !strings.Contains(strings.Join(gotArgs, " "), "https://example.com/cb") {
		t.Fatalf("url not passed to runner: %v", gotArgs)
	}
}

func TestOSBrowserOpenerRejectsBadScheme(t *testing.T) {
	op := &osBrowserOpener{runner: func(string, ...string) error {
		t.Fatal("runner must not run for invalid scheme")
		return nil
	}}
	if err := op.Open("file:///etc/passwd"); err == nil || !errors.Is(err, errInvalidBrowserURL) {
		t.Fatalf("expected errInvalidBrowserURL, got %v", err)
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/auth/ -run 'TestBrowser|TestValidateBrowserURL|TestOSBrowserOpener' -v`
Expected: FAIL — `undefined: browserCommand` / `undefined: osBrowserOpener`.

- [ ] **Step 3: Write minimal implementation**

Create `internal/auth/browser.go`:
```go
package auth

import (
	"errors"
	"fmt"
	"net/url"
	"os"
	"os/exec"
	"runtime"
)

var errInvalidBrowserURL = errors.New("auth: browser url must be http or https")

// BrowserOpener opens a URL in the user's default browser. Injected so tests
// never launch a real browser.
type BrowserOpener interface {
	Open(url string) error
}

// osBrowserOpener launches the platform browser command. runner is a seam for
// tests; in production it execs the command.
type osBrowserOpener struct {
	runner func(name string, args ...string) error
}

// NewOSBrowserOpener returns a BrowserOpener backed by the OS browser command.
func NewOSBrowserOpener() *osBrowserOpener {
	return &osBrowserOpener{runner: func(name string, args ...string) error {
		return exec.Command(name, args...).Start()
	}}
}

func (o *osBrowserOpener) Open(raw string) error {
	if err := validateBrowserURL(raw); err != nil {
		return err
	}
	// Honor $BROWSER like CC does, when it names a single command.
	if custom := os.Getenv("BROWSER"); custom != "" {
		return o.runner(custom, raw)
	}
	name, args := browserCommand(runtime.GOOS, raw)
	return o.runner(name, args...)
}

// validateBrowserURL rejects anything but http/https to avoid passing a
// file:// or javascript: URL to the OS opener.
func validateBrowserURL(raw string) error {
	u, err := url.Parse(raw)
	if err != nil {
		return fmt.Errorf("%w: %v", errInvalidBrowserURL, err)
	}
	if u.Scheme != "http" && u.Scheme != "https" {
		return errInvalidBrowserURL
	}
	return nil
}

// browserCommand returns the OS-specific command + args to open url. Pure.
func browserCommand(goos string, url string) (string, []string) {
	switch goos {
	case "darwin":
		return "open", []string{url}
	case "windows":
		// rundll32 url.dll,FileProtocolHandler <url>
		return "rundll32", []string{"url.dll,FileProtocolHandler", url}
	default:
		return "xdg-open", []string{url}
	}
}
```

- [ ] **Step 4: Run tests to verify they pass**

Run: `go test ./internal/auth/ -run 'TestBrowser|TestValidateBrowserURL|TestOSBrowserOpener' -v`
Expected: PASS.

- [ ] **Step 5: Commit**

```bash
git add internal/auth/browser.go internal/auth/browser_test.go
git commit -m "feat(auth): add cross-platform injectable browser opener"
```

---

## Task 2: Local callback HTTP listener (ephemeral port, state CSRF validation)

**Files:**
- Create: `internal/auth/callback.go`
- Test: `internal/auth/callback_test.go`

**Interfaces:**
- Produces:
  - `type CallbackResult struct { Code string; State string }`
  - `type CallbackListener struct { ... }`
  - `func StartCallbackListener(expectedState string) (*CallbackListener, error)` — binds
    `127.0.0.1:0`, starts serving in a goroutine.
  - `func (l *CallbackListener) Port() int`
  - `func (l *CallbackListener) RedirectURI() string` — `http://localhost:<port>/callback` (matches
    CC's `localhost`, not `127.0.0.1`, so the IdP's registered redirect matches).
  - `func (l *CallbackListener) Wait(ctx context.Context) (CallbackResult, error)`
  - `func (l *CallbackListener) Close() error`

CC anchor confirmed: `services/oauth/auth-code-listener.ts` listens `port ?? 0` host `localhost`, path
`/callback`, returns 400 `Invalid state parameter` on mismatch. Confirm ccgo's existing redirect string
shape with: `grep -n "localhost:%d/callback" internal/auth/oauth.go` (oauth.go:115 — keep it identical).

- [ ] **Step 1: Write the failing test**

Create `internal/auth/callback_test.go`:
```go
package auth

import (
	"context"
	"net/http"
	"strings"
	"testing"
	"time"
)

func TestCallbackListenerSuccess(t *testing.T) {
	l, err := StartCallbackListener("st-123")
	if err != nil {
		t.Fatalf("start: %v", err)
	}
	defer l.Close()

	if l.Port() <= 0 {
		t.Fatalf("port = %d", l.Port())
	}
	if want := "http://localhost:"; !strings.HasPrefix(l.RedirectURI(), want) ||
		!strings.HasSuffix(l.RedirectURI(), "/callback") {
		t.Fatalf("redirect = %q", l.RedirectURI())
	}

	// Simulate the IdP redirect hitting the loopback callback.
	go func() {
		url := l.RedirectURI() + "?code=AUTH_CODE&state=st-123"
		resp, err := http.Get(url)
		if err == nil {
			resp.Body.Close()
		}
	}()

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	res, err := l.Wait(ctx)
	if err != nil {
		t.Fatalf("Wait err: %v", err)
	}
	if res.Code != "AUTH_CODE" {
		t.Fatalf("code = %q want AUTH_CODE", res.Code)
	}
}

func TestCallbackListenerStateMismatch(t *testing.T) {
	l, err := StartCallbackListener("good-state")
	if err != nil {
		t.Fatalf("start: %v", err)
	}
	defer l.Close()

	var status int
	go func() {
		resp, err := http.Get(l.RedirectURI() + "?code=X&state=WRONG")
		if err == nil {
			status = resp.StatusCode
			resp.Body.Close()
		}
	}()

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	_, err = l.Wait(ctx)
	if err == nil {
		t.Fatal("expected error on state mismatch")
	}
	// The error MUST NOT leak the bad state value back.
	if strings.Contains(err.Error(), "WRONG") {
		t.Fatalf("error leaked attacker-controlled state: %v", err)
	}
	// Give the goroutine a moment; the HTTP response should be a 4xx.
	time.Sleep(50 * time.Millisecond)
	if status != 0 && (status < 400 || status >= 500) {
		t.Fatalf("callback status = %d want 4xx", status)
	}
}

func TestCallbackListenerIdPError(t *testing.T) {
	l, err := StartCallbackListener("s")
	if err != nil {
		t.Fatalf("start: %v", err)
	}
	defer l.Close()
	go func() {
		resp, e := http.Get(l.RedirectURI() + "?error=access_denied&error_description=nope&state=s")
		if e == nil {
			resp.Body.Close()
		}
	}()
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	if _, err := l.Wait(ctx); err == nil {
		t.Fatal("expected error when IdP returns error=")
	}
}

func TestCallbackListenerContextCancel(t *testing.T) {
	l, err := StartCallbackListener("s")
	if err != nil {
		t.Fatalf("start: %v", err)
	}
	defer l.Close()
	ctx, cancel := context.WithTimeout(context.Background(), 100*time.Millisecond)
	defer cancel()
	if _, err := l.Wait(ctx); err == nil {
		t.Fatal("expected ctx deadline error")
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/auth/ -run TestCallbackListener -v`
Expected: FAIL — `undefined: StartCallbackListener`.

- [ ] **Step 3: Write minimal implementation**

Create `internal/auth/callback.go`:
```go
package auth

import (
	"context"
	"errors"
	"fmt"
	"net"
	"net/http"
	"strings"
	"sync"
)

// CallbackResult is the validated content of the OAuth redirect.
type CallbackResult struct {
	Code  string
	State string
}

// CallbackListener serves the loopback OAuth redirect on an ephemeral port.
type CallbackListener struct {
	expectedState string
	listener      net.Listener
	server        *http.Server
	resultCh      chan CallbackResult
	errCh         chan error
	once          sync.Once
}

const callbackPath = "/callback"

// StartCallbackListener binds 127.0.0.1 on an OS-assigned port and begins
// serving. expectedState must be the PKCE state generated for this login.
func StartCallbackListener(expectedState string) (*CallbackListener, error) {
	if strings.TrimSpace(expectedState) == "" {
		return nil, errors.New("auth: callback listener requires a non-empty state")
	}
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		return nil, fmt.Errorf("auth: bind callback listener: %w", err)
	}
	l := &CallbackListener{
		expectedState: expectedState,
		listener:      ln,
		resultCh:      make(chan CallbackResult, 1),
		errCh:         make(chan error, 1),
	}
	mux := http.NewServeMux()
	mux.HandleFunc(callbackPath, l.handle)
	l.server = &http.Server{Handler: mux}
	go func() { _ = l.server.Serve(ln) }()
	return l, nil
}

// Port returns the OS-assigned callback port.
func (l *CallbackListener) Port() int {
	return l.listener.Addr().(*net.TCPAddr).Port
}

// RedirectURI is the exact redirect_uri to register in the authorize request
// AND replay in the token exchange. Uses host "localhost" to match CC.
func (l *CallbackListener) RedirectURI() string {
	return fmt.Sprintf("http://localhost:%d%s", l.Port(), callbackPath)
}

// handle validates the redirect request and pushes the first result/error.
func (l *CallbackListener) handle(w http.ResponseWriter, r *http.Request) {
	q := r.URL.Query()

	// IdP-reported error takes precedence (do not echo description back raw).
	if e := q.Get("error"); e != "" {
		writeCallbackPage(w, http.StatusBadRequest, "Login failed. You can close this window.")
		l.fail(fmt.Errorf("auth: authorization error %q", sanitizeErrorCode(e)))
		return
	}
	state := q.Get("state")
	if state != l.expectedState {
		writeCallbackPage(w, http.StatusBadRequest, "Invalid request. You can close this window.")
		// Do NOT include the received state in the error (CSRF / log hygiene).
		l.fail(errors.New("auth: callback state mismatch"))
		return
	}
	code := q.Get("code")
	if code == "" {
		writeCallbackPage(w, http.StatusBadRequest, "Missing authorization code. You can close this window.")
		l.fail(errors.New("auth: callback missing authorization code"))
		return
	}
	writeCallbackPage(w, http.StatusOK, "Login successful. You can close this window and return to the terminal.")
	l.succeed(CallbackResult{Code: code, State: state})
}

func (l *CallbackListener) succeed(res CallbackResult) {
	l.once.Do(func() { l.resultCh <- res })
}

func (l *CallbackListener) fail(err error) {
	l.once.Do(func() { l.errCh <- err })
}

// Wait blocks until the callback fires, an error occurs, or ctx is done.
func (l *CallbackListener) Wait(ctx context.Context) (CallbackResult, error) {
	select {
	case res := <-l.resultCh:
		return res, nil
	case err := <-l.errCh:
		return CallbackResult{}, err
	case <-ctx.Done():
		return CallbackResult{}, ctx.Err()
	}
}

// Close shuts the HTTP server and releases the port.
func (l *CallbackListener) Close() error {
	return l.server.Close()
}

func writeCallbackPage(w http.ResponseWriter, status int, message string) {
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.WriteHeader(status)
	// message is one of our own constant strings, never attacker input.
	_, _ = w.Write([]byte("<!doctype html><html><body><p>" + message + "</p></body></html>"))
}

// sanitizeErrorCode keeps only the OAuth error code charset; never reflects
// arbitrary IdP text into our error string.
func sanitizeErrorCode(s string) string {
	const max = 64
	clean := make([]rune, 0, len(s))
	for _, r := range s {
		if r == '_' || (r >= 'a' && r <= 'z') || (r >= 'A' && r <= 'Z') {
			clean = append(clean, r)
		}
		if len(clean) >= max {
			break
		}
	}
	return string(clean)
}
```

- [ ] **Step 4: Run tests to verify they pass**

Run: `go test ./internal/auth/ -run TestCallbackListener -v`
Expected: PASS (all four subtests). Each test must `defer l.Close()` so ports release.

- [ ] **Step 5: Commit**

```bash
git add internal/auth/callback.go internal/auth/callback_test.go
git commit -m "feat(auth): add loopback OAuth callback listener with state CSRF validation"
```

---

## Task 3: Authorization-code → token exchange

**Files:**
- Create: `internal/auth/exchange.go`
- Test: `internal/auth/exchange_test.go`

**Interfaces:**
- Produces:
  - `type ExchangeParams struct { Code string; CodeVerifier string; RedirectURI string; State string }`
  - `func ExchangeAuthorizationCode(ctx context.Context, client *http.Client, config OAuthConfig, params ExchangeParams) (Credentials, error)`

Behavior (CC anchor `services/oauth/client.ts` `exchangeCodeForTokens`): POST JSON
`{grant_type:"authorization_code", code, redirect_uri, client_id, code_verifier, state}` to
`config.TokenURL`, size-limited body read, decode `oauthTokenResponse`, require non-empty
`access_token`, compute `ExpiresAt` from `expires_in`, set `Source = SourceOAuth`. CC uses JSON body
(not form-encoded) for this exchange — confirm by reading `services/oauth/client.ts:115-133` before
coding. Reuse the existing `oauthTokenResponse` struct (token_provider.go:42) and the
`defaultOAuthTokenResponseLimit` const — confirm names with
`grep -n "oauthTokenResponse\|defaultOAuthTokenResponseLimit" internal/auth/token_provider.go`.

- [ ] **Step 1: Write the failing test**

Create `internal/auth/exchange_test.go`:
```go
package auth

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestExchangeAuthorizationCodeSuccess(t *testing.T) {
	var gotBody map[string]any
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewDecoder(r.Body).Decode(&gotBody)
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"access_token":"at-1","refresh_token":"rt-1","expires_in":3600,"scope":"user:profile user:inference"}`))
	}))
	defer srv.Close()

	cfg := ProductionOAuthConfig()
	cfg.TokenURL = srv.URL

	creds, err := ExchangeAuthorizationCode(context.Background(), srv.Client(), cfg, ExchangeParams{
		Code:         "the-code",
		CodeVerifier: "the-verifier",
		RedirectURI:  "http://localhost:55555/callback",
		State:        "the-state",
	})
	if err != nil {
		t.Fatalf("exchange err: %v", err)
	}
	if creds.Source != SourceOAuth || creds.AccessToken != "at-1" || creds.RefreshToken != "rt-1" {
		t.Fatalf("creds = %+v", creds)
	}
	if creds.ExpiresAt.IsZero() {
		t.Fatal("ExpiresAt should be set from expires_in")
	}
	// Verify the request body matched the spec.
	if gotBody["grant_type"] != "authorization_code" || gotBody["code"] != "the-code" ||
		gotBody["code_verifier"] != "the-verifier" || gotBody["redirect_uri"] != "http://localhost:55555/callback" {
		t.Fatalf("request body = %#v", gotBody)
	}
	if gotBody["client_id"] == "" || gotBody["client_id"] == nil {
		t.Fatalf("client_id missing in body: %#v", gotBody)
	}
}

func TestExchangeAuthorizationCodeHTTPError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusBadRequest)
		_, _ = w.Write([]byte(`{"error":"invalid_grant","secret_hint":"super-secret"}`))
	}))
	defer srv.Close()
	cfg := ProductionOAuthConfig()
	cfg.TokenURL = srv.URL
	_, err := ExchangeAuthorizationCode(context.Background(), srv.Client(), cfg, ExchangeParams{Code: "x", CodeVerifier: "v", RedirectURI: "http://localhost:1/callback"})
	if err == nil {
		t.Fatal("expected error on 400")
	}
	// Error must surface status but MUST NOT echo a token-bearing body wholesale.
	if !strings.Contains(err.Error(), "400") {
		t.Fatalf("error should mention status: %v", err)
	}
}

func TestExchangeAuthorizationCodeMissingAccessToken(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(`{"refresh_token":"rt"}`))
	}))
	defer srv.Close()
	cfg := ProductionOAuthConfig()
	cfg.TokenURL = srv.URL
	if _, err := ExchangeAuthorizationCode(context.Background(), srv.Client(), cfg, ExchangeParams{Code: "x", CodeVerifier: "v", RedirectURI: "http://localhost:1/callback"}); err == nil {
		t.Fatal("expected error when access_token absent")
	}
}

func TestExchangeAuthorizationCodeValidatesParams(t *testing.T) {
	cfg := ProductionOAuthConfig()
	cfg.TokenURL = "http://unused"
	if _, err := ExchangeAuthorizationCode(context.Background(), http.DefaultClient, cfg, ExchangeParams{Code: "", CodeVerifier: "v", RedirectURI: "r"}); err == nil {
		t.Fatal("empty code must be rejected before any network call")
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/auth/ -run TestExchangeAuthorizationCode -v`
Expected: FAIL — `undefined: ExchangeAuthorizationCode`.

- [ ] **Step 3: Write minimal implementation**

Create `internal/auth/exchange.go`:
```go
package auth

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"
)

// ExchangeParams carries the inputs to the authorization_code grant.
type ExchangeParams struct {
	Code         string
	CodeVerifier string
	RedirectURI  string
	State        string
}

func (p ExchangeParams) validate() error {
	if strings.TrimSpace(p.Code) == "" {
		return fmt.Errorf("auth: authorization code is required")
	}
	if strings.TrimSpace(p.CodeVerifier) == "" {
		return fmt.Errorf("auth: code_verifier is required")
	}
	if strings.TrimSpace(p.RedirectURI) == "" {
		return fmt.Errorf("auth: redirect_uri is required")
	}
	return nil
}

// authCodeRequest is the JSON body CC posts (services/oauth/client.ts:115).
type authCodeRequest struct {
	GrantType    string `json:"grant_type"`
	Code         string `json:"code"`
	RedirectURI  string `json:"redirect_uri"`
	ClientID     string `json:"client_id"`
	CodeVerifier string `json:"code_verifier"`
	State        string `json:"state,omitempty"`
}

// ExchangeAuthorizationCode swaps an authorization code for OAuth credentials.
func ExchangeAuthorizationCode(ctx context.Context, client *http.Client, config OAuthConfig, params ExchangeParams) (Credentials, error) {
	if err := params.validate(); err != nil {
		return Credentials{}, err
	}
	if config.TokenURL == "" || config.ClientID == "" {
		production := ProductionOAuthConfig()
		if config.TokenURL == "" {
			config.TokenURL = production.TokenURL
		}
		if config.ClientID == "" {
			config.ClientID = production.ClientID
		}
	}
	if client == nil {
		client = http.DefaultClient
	}

	body, err := json.Marshal(authCodeRequest{
		GrantType:    "authorization_code",
		Code:         params.Code,
		RedirectURI:  params.RedirectURI,
		ClientID:     config.ClientID,
		CodeVerifier: params.CodeVerifier,
		State:        params.State,
	})
	if err != nil {
		return Credentials{}, err
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, config.TokenURL, bytes.NewReader(body))
	if err != nil {
		return Credentials{}, err
	}
	req.Header.Set("content-type", "application/json")
	req.Header.Set("accept", "application/json")

	resp, err := client.Do(req)
	if err != nil {
		return Credentials{}, fmt.Errorf("auth: token exchange request failed: %w", err)
	}
	defer resp.Body.Close()

	limit := defaultOAuthTokenResponseLimit
	raw, err := io.ReadAll(io.LimitReader(resp.Body, limit+1))
	if err != nil {
		return Credentials{}, err
	}
	if int64(len(raw)) > limit {
		return Credentials{}, fmt.Errorf("auth: token response exceeds %d bytes", limit)
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		// Surface status only; never echo the (possibly token-bearing) body.
		return Credentials{}, fmt.Errorf("auth: token exchange failed with status %d", resp.StatusCode)
	}

	var tr oauthTokenResponse
	if err := json.Unmarshal(raw, &tr); err != nil {
		return Credentials{}, fmt.Errorf("auth: decode token response: %w", err)
	}
	accessToken := strings.TrimSpace(tr.AccessToken)
	if accessToken == "" {
		return Credentials{}, fmt.Errorf("auth: token response missing access_token")
	}

	creds := Credentials{
		Source:       SourceOAuth,
		AccessToken:  accessToken,
		RefreshToken: strings.TrimSpace(tr.RefreshToken),
		Scopes:       ParseScopes(tr.Scope),
	}
	if tr.ExpiresIn > 0 {
		creds.ExpiresAt = time.Now().Add(time.Duration(tr.ExpiresIn) * time.Second)
	}
	return creds, nil
}
```

- [ ] **Step 4: Run tests to verify they pass**

Run: `go test ./internal/auth/ -run TestExchangeAuthorizationCode -v`
Expected: PASS.

- [ ] **Step 5: Commit**

```bash
git add internal/auth/exchange.go internal/auth/exchange_test.go
git commit -m "feat(auth): add authorization_code to token exchange with response validation"
```

---

## Task 4: `LoginFlow` orchestrator (listener → browser → exchange → store)

**Files:**
- Create: `internal/auth/login.go`
- Test: `internal/auth/login_test.go`

**Interfaces:**
- Produces:
  - `type LoginOptions struct { Config OAuthConfig; HTTPClient *http.Client; Browser BrowserOpener; Store CredentialStore; LoginWithClaudeAI bool; OrgUUID string; LoginHint string; Now func() time.Time; OnURL func(string) }`
  - `func RunLoginFlow(ctx context.Context, opts LoginOptions) (Credentials, error)`

`OnURL` is invoked with the authorize URL after the browser open is attempted (so the caller can print
the manual-paste fallback line CC shows). `Browser`/`Store`/`HTTPClient` are all injectable seams so the
test drives the full flow with `httptest` + a fake browser that hits the real loopback listener — **no
real browser, no real network**. Uses the existing `GenerateCodeVerifier`/`GenerateState`/
`GenerateCodeChallenge`/`BuildAuthURL` — confirm signatures with
`go doc ./internal/auth BuildAuthURL` and `go doc ./internal/auth AuthURLParams`.

- [ ] **Step 1: Write the failing test**

Create `internal/auth/login_test.go`:
```go
package auth

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"net/url"
	"testing"
	"time"
)

// fakeBrowser, instead of opening a browser, parses the authorize URL,
// extracts the redirect_uri + state, and GETs the loopback callback — exactly
// what a real IdP would do after the user approves.
type fakeBrowser struct{ t *testing.T }

func (b fakeBrowser) Open(authURL string) error {
	u, err := url.Parse(authURL)
	if err != nil {
		return err
	}
	q := u.Query()
	redirect := q.Get("redirect_uri")
	state := q.Get("state")
	go func() {
		resp, err := http.Get(redirect + "?code=GRANTED&state=" + url.QueryEscape(state))
		if err == nil {
			resp.Body.Close()
		}
	}()
	return nil
}

// memStore is an in-memory CredentialStore for tests.
type memStore struct{ saved Credentials }

func (m *memStore) Load(context.Context) (Credentials, error)  { return m.saved, nil }
func (m *memStore) Save(_ context.Context, c Credentials) error { m.saved = c; return nil }
func (m *memStore) Delete(context.Context) error               { m.saved = Credentials{}; return nil }

func TestRunLoginFlow(t *testing.T) {
	tokenSrv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var body map[string]any
		_ = json.NewDecoder(r.Body).Decode(&body)
		if body["code"] != "GRANTED" {
			w.WriteHeader(http.StatusBadRequest)
			return
		}
		_, _ = w.Write([]byte(`{"access_token":"AT","refresh_token":"RT","expires_in":3600,"scope":"user:profile user:inference"}`))
	}))
	defer tokenSrv.Close()

	cfg := ProductionOAuthConfig()
	cfg.TokenURL = tokenSrv.URL

	store := &memStore{}
	var sawURL string
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()

	creds, err := RunLoginFlow(ctx, LoginOptions{
		Config:     cfg,
		HTTPClient: tokenSrv.Client(),
		Browser:    fakeBrowser{t: t},
		Store:      store,
		OnURL:      func(u string) { sawURL = u },
	})
	if err != nil {
		t.Fatalf("RunLoginFlow err: %v", err)
	}
	if creds.AccessToken != "AT" || creds.Source != SourceOAuth {
		t.Fatalf("creds = %+v", creds)
	}
	if store.saved.AccessToken != "AT" {
		t.Fatal("credentials not persisted to store")
	}
	if sawURL == "" {
		t.Fatal("OnURL was not called with the authorize URL")
	}
}

func TestRunLoginFlowBrowserFailureStillPrintsURL(t *testing.T) {
	// If the browser can't open, the flow must still surface the URL (manual
	// paste) rather than aborting before the user can authenticate.
	cfg := ProductionOAuthConfig()
	cfg.TokenURL = "http://unused"
	var sawURL string
	ctx, cancel := context.WithTimeout(context.Background(), 300*time.Millisecond)
	defer cancel()
	_, _ = RunLoginFlow(ctx, LoginOptions{
		Config:  cfg,
		Browser: failingBrowser{},
		Store:   &memStore{},
		OnURL:   func(u string) { sawURL = u },
	})
	if sawURL == "" {
		t.Fatal("authorize URL must be shown even when browser open fails")
	}
}

type failingBrowser struct{}

func (failingBrowser) Open(string) error { return http.ErrServerClosed }
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/auth/ -run TestRunLoginFlow -v`
Expected: FAIL — `undefined: RunLoginFlow`.

- [ ] **Step 3: Write minimal implementation**

Create `internal/auth/login.go`:
```go
package auth

import (
	"context"
	"fmt"
	"net/http"
	"time"
)

// LoginOptions configures an interactive OAuth login. Browser/Store/HTTPClient
// are seams so the flow is fully testable without a real browser or network.
type LoginOptions struct {
	Config            OAuthConfig
	HTTPClient        *http.Client
	Browser           BrowserOpener
	Store             CredentialStore
	LoginWithClaudeAI bool
	OrgUUID           string
	LoginHint         string
	InferenceOnly     bool
	Now               func() time.Time
	// OnURL is called with the authorize URL so the caller can print the
	// manual-paste fallback. Never nil-checked away — always invoked.
	OnURL func(string)
}

// RunLoginFlow performs the full PKCE authorization-code login:
// generate PKCE -> start loopback listener -> build URL -> open browser ->
// wait for callback -> exchange code -> persist. Returns the new credentials.
func RunLoginFlow(ctx context.Context, opts LoginOptions) (Credentials, error) {
	config := opts.Config
	if config.ClientID == "" || config.TokenURL == "" {
		config = mergeWithProduction(config)
	}

	verifier, err := GenerateCodeVerifier()
	if err != nil {
		return Credentials{}, fmt.Errorf("auth: generate code verifier: %w", err)
	}
	state, err := GenerateState()
	if err != nil {
		return Credentials{}, fmt.Errorf("auth: generate state: %w", err)
	}
	challenge := GenerateCodeChallenge(verifier)

	listener, err := StartCallbackListener(state)
	if err != nil {
		return Credentials{}, err
	}
	defer listener.Close()

	authURL, err := BuildAuthURL(AuthURLParams{
		CodeChallenge:     challenge,
		State:             state,
		Port:              listener.Port(),
		LoginWithClaudeAI: opts.LoginWithClaudeAI,
		InferenceOnly:     opts.InferenceOnly,
		OrgUUID:           opts.OrgUUID,
		LoginHint:         opts.LoginHint,
		Config:            config,
	})
	if err != nil {
		return Credentials{}, err
	}

	// Always show the URL first (manual fallback), then try the browser.
	if opts.OnURL != nil {
		opts.OnURL(authURL)
	}
	if opts.Browser != nil {
		// A browser-open failure is non-fatal: the user can paste the URL.
		_ = opts.Browser.Open(authURL)
	}

	result, err := listener.Wait(ctx)
	if err != nil {
		return Credentials{}, err
	}

	creds, err := ExchangeAuthorizationCode(ctx, opts.HTTPClient, config, ExchangeParams{
		Code:         result.Code,
		CodeVerifier: verifier,
		RedirectURI:  listener.RedirectURI(),
		State:        state,
	})
	if err != nil {
		return Credentials{}, err
	}

	if opts.Store != nil {
		if err := opts.Store.Save(ctx, creds); err != nil {
			return Credentials{}, fmt.Errorf("auth: persist credentials: %w", err)
		}
	}
	return creds, nil
}

func mergeWithProduction(config OAuthConfig) OAuthConfig {
	production := ProductionOAuthConfig()
	if config.ClientID == "" {
		config.ClientID = production.ClientID
	}
	if config.TokenURL == "" {
		config.TokenURL = production.TokenURL
	}
	if config.ConsoleAuthorizeURL == "" {
		config.ConsoleAuthorizeURL = production.ConsoleAuthorizeURL
	}
	if config.ClaudeAIAuthorizeURL == "" {
		config.ClaudeAIAuthorizeURL = production.ClaudeAIAuthorizeURL
	}
	if config.ManualRedirectURL == "" {
		config.ManualRedirectURL = production.ManualRedirectURL
	}
	return config
}
```

Note: confirm `AuthURLParams` has the fields used above (`CodeChallenge,State,Port,LoginWithClaudeAI,
InferenceOnly,OrgUUID,LoginHint,Config`) with `go doc ./internal/auth AuthURLParams` — they were
verified present in oauth.go:62. If `BuildAuthURL` ignores a field, drop it; do not add fields to the
production struct just for this.

- [ ] **Step 4: Run tests to verify they pass**

Run: `go test ./internal/auth/ -run TestRunLoginFlow -v && go test ./internal/auth/ -v`
Expected: PASS, including pre-existing `oauth_test.go`/`store_test.go`/`token_provider_test.go`.

- [ ] **Step 5: Commit**

```bash
git add internal/auth/login.go internal/auth/login_test.go
git commit -m "feat(auth): orchestrate PKCE authorization-code login flow"
```

---

## Task 5: `/login` and `/logout` slash commands + gray-zone consent

**Files:**
- Modify: `internal/commands/slash.go` (add result types + dispatch)
- Modify: `internal/commands/registry.go` (add builtins)
- Modify: `internal/conversation/run.go` (handle new result types)
- Test: `internal/commands/slash_test.go` (add cases), `internal/conversation/run_test.go` (add case)

**Interfaces:**
- Produces:
  - `LocalCommandResultLogin LocalCommandResultType = "login"`
  - `LocalCommandResultLogout LocalCommandResultType = "logout"`
  - dispatch cases in `ExecuteBuiltinLocalCommand`
  - `login`/`logout` entries in `BuiltinCommands()`
  - `run.go` handling that returns a text result (and the typed signal the REPL acts on in Phase 2).

Confirm the existing dispatch shape first: `grep -n "ExecuteBuiltinLocalCommand\|LocalCommandResult" internal/commands/slash.go`
(verified: switch on `cmd.Name`, returns `LocalCommandResult{Type,Value}`). Confirm `BuiltinCommands`
entry shape: `grep -n "BuiltinCommands\|CommandLocalJSX\|CommandSourceBuiltin" internal/commands/registry.go`
(verified: `{Type: contracts.CommandLocalJSX, Name: "...", Description: "...", Source:
contracts.CommandSourceBuiltin}`).

- [ ] **Step 1: Write the failing test**

Add to `internal/commands/slash_test.go`:
```go
func TestExecuteBuiltinLoginLogout(t *testing.T) {
	reg := FromSources(Sources{Builtins: BuiltinCommands()})

	loginCmd, ok := reg.Find("login")
	if !ok {
		t.Fatal("login builtin not registered")
	}
	res, ok := ExecuteBuiltinLocalCommand(reg, loginCmd, "")
	if !ok || res.Type != LocalCommandResultLogin {
		t.Fatalf("login result = %+v ok=%v", res, ok)
	}

	logoutCmd, ok := reg.Find("logout")
	if !ok {
		t.Fatal("logout builtin not registered")
	}
	res, ok = ExecuteBuiltinLocalCommand(reg, logoutCmd, "")
	if !ok || res.Type != LocalCommandResultLogout {
		t.Fatalf("logout result = %+v ok=%v", res, ok)
	}
}
```
(Confirm the registry constructor used by other slash tests — `grep -n "FromSources\|Load(Options" internal/commands/slash_test.go` — and reuse that exact helper.)

Add to `internal/conversation/run_test.go` a case asserting that a `/logout` produces a result the
runner surfaces without an API call (mirror an existing local-command test such as the `/cost` one;
find it with `grep -n "LocalCommandResultCost\|shouldQuery\|appendLocalTextResult" internal/conversation/run_test.go`).

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/commands/ -run TestExecuteBuiltinLoginLogout -v`
Expected: FAIL — `undefined: LocalCommandResultLogin` / `login builtin not registered`.

- [ ] **Step 3: Write minimal implementation**

In `internal/commands/slash.go`, add the two consts to the `LocalCommandResultType` block:
```go
	LocalCommandResultLogin  LocalCommandResultType = "login"
	LocalCommandResultLogout LocalCommandResultType = "logout"
```
Add cases to `ExecuteBuiltinLocalCommand`'s switch (next to `case "config":`):
```go
	case "login":
		return LocalCommandResult{Type: LocalCommandResultLogin, Value: strings.TrimSpace(args)}, true
	case "logout":
		return LocalCommandResult{Type: LocalCommandResultLogout, Value: strings.TrimSpace(args)}, true
```

In `internal/commands/registry.go`, add to the `BuiltinCommands()` slice (after the existing entries):
```go
		{Type: contracts.CommandLocalJSX, Name: "login", Description: "Sign in with your Claude account (OAuth)", Source: contracts.CommandSourceBuiltin, Immediate: true},
		{Type: contracts.CommandLocalJSX, Name: "logout", Description: "Sign out and remove stored credentials", Source: contracts.CommandSourceBuiltin, Immediate: true},
```

In `internal/conversation/run.go`, add handling next to the other `localResult` branches (the
`!shouldQuery` block). The runner does not itself perform the browser flow (it has no terminal); it
returns a typed result the interactive REPL (Phase 2) and the headless caller act on. For now surface a
clear text result so behavior is testable and never silently no-ops:
```go
		if localResult != nil && localResult.Type == commands.LocalCommandResultLogin {
			result.Login = true
			return r.appendLocalTextResult(result, history, "Run `claude auth login` to sign in, or use /login in an interactive session.")
		}
		if localResult != nil && localResult.Type == commands.LocalCommandResultLogout {
			text, err := r.runLogout(ctx)
			if err != nil {
				return result, err
			}
			result.LoggedOut = true
			return r.appendLocalTextResult(result, history, text)
		}
```
Add `Login bool` and `LoggedOut bool` to the `Result` struct (confirm the struct with
`grep -n "type Result struct" internal/conversation/run.go` and follow the existing `Cleared`/`Compacted`
boolean pattern). Add a small `runLogout` method that deletes credentials via the runner's credential
store — confirm whether the runner already holds a store with
`grep -n "CredentialStore\|credentialStore\|Credentials" internal/conversation/*.go`; if not, plumb a
`CredentialStore` field onto `Runner` (defaulting to `auth.NewKeychainCredentialStore("")` from Task 6)
and call `Delete(ctx)`:
```go
func (r *Runner) runLogout(ctx context.Context) (string, error) {
	if r.CredentialStore == nil {
		return "No stored credentials to remove.", nil
	}
	if err := r.CredentialStore.Delete(ctx); err != nil {
		return "", fmt.Errorf("logout: %w", err)
	}
	return "Signed out. Stored credentials removed.", nil
}
```
The gray-zone **consent line** lives in the *interactive* `/login` path and the `claude auth login` CLI
(Task 7): both print one line — e.g. `"OAuth login uses Anthropic's official client; this is a ToS gray
area. Continue? [y/N]"` — before opening a browser. The slash-command result above intentionally directs
the user to `claude auth login` so the consent gate is consistent (the full interactive in-REPL browser
launch is wired in Phase 2's UI; this keeps Phase 4 self-contained and testable).

- [ ] **Step 4: Run tests to verify they pass**

Run: `go test ./internal/commands/ ./internal/conversation/ -v`
Expected: PASS, including the new cases and all pre-existing command/runner tests.

- [ ] **Step 5: Commit**

```bash
git add internal/commands/slash.go internal/commands/registry.go internal/conversation/run.go internal/commands/slash_test.go internal/conversation/run_test.go
git commit -m "feat(commands): add /login and /logout builtin commands"
```

---

## Task 6: Keychain credential store (macOS `security`; file fallback) replacing plaintext

**Files:**
- Create: `internal/auth/keychain.go`
- Test: `internal/auth/keychain_test.go`

**Interfaces:**
- Produces:
  - `type KeychainStore interface { Get(account, service string) (string, error); Set(account, service, value string) error; Delete(account, service string) error; Available() bool }`
  - `type macOSKeychainStore struct{ run func(stdin string, args ...string) (string, error) }`
  - `func NewKeychainCredentialStore(path string) CredentialStore` — returns a `CredentialStore` that
    uses the keychain on macOS (with file fallback) and the plain `FileCredentialStore` elsewhere.
  - internal `keychainCredentialStore struct { kc KeychainStore; file *FileCredentialStore }`
  - constants `keychainServiceName = "Claude Code-credentials"`, `keychainAccount()` (= `$USER` or
    `"claude-code-user"`), matching CC's `macOsKeychainHelpers.ts`.

**Keychain dep decision (restated):** no new dependency — `os/exec` to `/usr/bin/security`, mirroring
CC (`utils/secureStorage/macOsKeychainStorage.ts`). Linux/Windows fall back to the existing
`FileCredentialStore` (chmod 0600), exactly as CC does (`// TODO: add libsecret`). Justified per
master §6.

- [ ] **Step 1: Write the failing test**

Create `internal/auth/keychain_test.go`:
```go
package auth

import (
	"context"
	"strings"
	"testing"
)

// fakeKeychain is an in-memory KeychainStore for testing the credential store
// wrapper without touching the real OS keychain.
type fakeKeychain struct {
	data      map[string]string
	available bool
}

func newFakeKeychain() *fakeKeychain { return &fakeKeychain{data: map[string]string{}, available: true} }

func (f *fakeKeychain) key(a, s string) string { return a + "\x00" + s }
func (f *fakeKeychain) Available() bool         { return f.available }
func (f *fakeKeychain) Get(a, s string) (string, error) {
	v, ok := f.data[f.key(a, s)]
	if !ok {
		return "", errKeychainNotFound
	}
	return v, nil
}
func (f *fakeKeychain) Set(a, s, v string) error { f.data[f.key(a, s)] = v; return nil }
func (f *fakeKeychain) Delete(a, s string) error { delete(f.data, f.key(a, s)); return nil }

func TestKeychainCredentialStoreRoundTrip(t *testing.T) {
	kc := newFakeKeychain()
	store := &keychainCredentialStore{kc: kc, file: NewFileCredentialStore(t.TempDir() + "/credentials.json")}

	creds := Credentials{Source: SourceOAuth, AccessToken: "AT", RefreshToken: "RT", Scopes: []string{"user:profile"}}
	if err := store.Save(context.Background(), creds); err != nil {
		t.Fatalf("Save err: %v", err)
	}
	// The keychain holds the value; the plaintext file must NOT have been written.
	if len(kc.data) == 0 {
		t.Fatal("expected credentials in keychain")
	}

	loaded, err := store.Load(context.Background())
	if err != nil {
		t.Fatalf("Load err: %v", err)
	}
	if loaded.AccessToken != "AT" || loaded.RefreshToken != "RT" {
		t.Fatalf("loaded = %+v", loaded)
	}

	if err := store.Delete(context.Background()); err != nil {
		t.Fatalf("Delete err: %v", err)
	}
	loaded, _ = store.Load(context.Background())
	if loaded.AccessToken != "" {
		t.Fatal("credentials not deleted")
	}
}

func TestKeychainCredentialStoreFallsBackToFile(t *testing.T) {
	kc := newFakeKeychain()
	kc.available = false
	file := NewFileCredentialStore(t.TempDir() + "/credentials.json")
	store := &keychainCredentialStore{kc: kc, file: file}

	creds := Credentials{Source: SourceOAuth, AccessToken: "AT2"}
	if err := store.Save(context.Background(), creds); err != nil {
		t.Fatalf("Save err: %v", err)
	}
	if len(kc.data) != 0 {
		t.Fatal("keychain unavailable: must not write to keychain")
	}
	loaded, err := store.Load(context.Background())
	if err != nil || loaded.AccessToken != "AT2" {
		t.Fatalf("file fallback failed: %+v err=%v", loaded, err)
	}
}

func TestMacOSSecurityArgsHaveNoSecretInArgv(t *testing.T) {
	// Verify Set routes the secret via stdin, not argv, to avoid leaking it to
	// process listings (matches CC's `security -i` approach).
	var sawStdin string
	var sawArgs []string
	kc := &macOSKeychainStore{run: func(stdin string, args ...string) (string, error) {
		sawStdin, sawArgs = stdin, args
		return "", nil
	}}
	if err := kc.Set("acct", "svc", "TOPSECRET"); err != nil {
		t.Fatalf("Set err: %v", err)
	}
	if strings.Contains(strings.Join(sawArgs, " "), "TOPSECRET") {
		t.Fatalf("secret leaked into argv: %v", sawArgs)
	}
	if !strings.Contains(sawStdin, "TOPSECRET") && !strings.Contains(sawStdin, "544f5053454352455424") {
		// either raw on stdin or hex-encoded; both keep it out of argv
		t.Fatalf("secret not passed via stdin: %q", sawStdin)
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/auth/ -run 'TestKeychain|TestMacOSSecurity' -v`
Expected: FAIL — `undefined: keychainCredentialStore` / `undefined: macOSKeychainStore`.

- [ ] **Step 3: Write minimal implementation**

Create `internal/auth/keychain.go`:
```go
package auth

import (
	"context"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"runtime"
	"strings"
)

var errKeychainNotFound = errors.New("auth: keychain item not found")

// Service/account names mirror CC (utils/secureStorage/macOsKeychainHelpers.ts).
const keychainServiceName = "Claude Code-credentials"

func keychainAccount() string {
	if u := os.Getenv("USER"); u != "" {
		return u
	}
	return "claude-code-user"
}

// KeychainStore is a minimal secret store. macOSKeychainStore is the only real
// backend today; other platforms fall back to a file.
type KeychainStore interface {
	Available() bool
	Get(account, service string) (string, error)
	Set(account, service, value string) error
	Delete(account, service string) error
}

// macOSKeychainStore drives /usr/bin/security. run is a seam for tests.
type macOSKeychainStore struct {
	run func(stdin string, args ...string) (string, error)
}

func newMacOSKeychainStore() *macOSKeychainStore {
	return &macOSKeychainStore{run: runSecurity}
}

// runSecurity executes /usr/bin/security, optionally feeding stdin.
func runSecurity(stdin string, args ...string) (string, error) {
	cmd := exec.Command("/usr/bin/security", args...)
	if stdin != "" {
		cmd.Stdin = strings.NewReader(stdin)
	}
	out, err := cmd.Output()
	return string(out), err
}

func (m *macOSKeychainStore) Available() bool { return runtime.GOOS == "darwin" }

func (m *macOSKeychainStore) Get(account, service string) (string, error) {
	out, err := m.run("", "find-generic-password", "-a", account, "-w", "-s", service)
	if err != nil {
		return "", errKeychainNotFound
	}
	return strings.TrimRight(out, "\n"), nil
}

func (m *macOSKeychainStore) Set(account, service, value string) error {
	// Hex-encode the value and pass it via stdin (security -i) so the secret
	// never appears in argv / process listings (matches CC's approach).
	hexVal := hex.EncodeToString([]byte(value))
	stdin := fmt.Sprintf("add-generic-password -U -a %q -s %q -X %s\n", account, service, hexVal)
	_, err := m.run(stdin, "-i")
	return err
}

func (m *macOSKeychainStore) Delete(account, service string) error {
	_, err := m.run("", "delete-generic-password", "-a", account, "-s", service)
	if err != nil {
		// "not found" is not an error for Delete.
		return nil
	}
	return nil
}

// keychainCredentialStore implements CredentialStore atop a KeychainStore,
// falling back to a file store when the keychain is unavailable.
type keychainCredentialStore struct {
	kc   KeychainStore
	file *FileCredentialStore
}

// NewKeychainCredentialStore returns the preferred CredentialStore for this
// platform: keychain-backed on macOS, plain file elsewhere.
func NewKeychainCredentialStore(path string) CredentialStore {
	file := NewFileCredentialStore(path)
	if runtime.GOOS != "darwin" {
		return file
	}
	return &keychainCredentialStore{kc: newMacOSKeychainStore(), file: file}
}

func (s *keychainCredentialStore) usingKeychain() bool {
	return s.kc != nil && s.kc.Available()
}

func (s *keychainCredentialStore) Load(ctx context.Context) (Credentials, error) {
	if err := ctx.Err(); err != nil {
		return Credentials{}, err
	}
	if !s.usingKeychain() {
		return s.file.Load(ctx)
	}
	raw, err := s.kc.Get(keychainAccount(), keychainServiceName)
	if err != nil {
		if errors.Is(err, errKeychainNotFound) {
			return Credentials{Source: SourceNone}, nil
		}
		return Credentials{}, err
	}
	if strings.TrimSpace(raw) == "" {
		return Credentials{Source: SourceNone}, nil
	}
	var creds Credentials
	if err := json.Unmarshal([]byte(raw), &creds); err != nil {
		return Credentials{}, fmt.Errorf("auth: decode keychain credentials: %w", err)
	}
	if creds.Source == "" {
		creds.Source = SourceNone
	}
	return creds, nil
}

func (s *keychainCredentialStore) Save(ctx context.Context, creds Credentials) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	if creds.Source == "" {
		creds.Source = SourceNone
	}
	if err := creds.Validate(); err != nil {
		return err
	}
	if !s.usingKeychain() {
		return s.file.Save(ctx, creds)
	}
	data, err := json.Marshal(creds)
	if err != nil {
		return err
	}
	return s.kc.Set(keychainAccount(), keychainServiceName, string(data))
}

func (s *keychainCredentialStore) Delete(ctx context.Context) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	if !s.usingKeychain() {
		return s.file.Delete(ctx)
	}
	return s.kc.Delete(keychainAccount(), keychainServiceName)
}
```

Confirm `FileCredentialStore`/`NewFileCredentialStore`/`CredentialStore`/`(Credentials).Validate`
signatures with `go doc ./internal/auth FileCredentialStore` and `go doc ./internal/auth CredentialStore`
(verified in store.go/auth.go). The wrapper deliberately reuses the *same* `CredentialStore` interface so
no caller changes.

- [ ] **Step 4: Run tests to verify they pass**

Run: `go test ./internal/auth/ -run 'TestKeychain|TestMacOSSecurity' -v && go test ./internal/auth/ -v`
Expected: PASS. The macOS `security` round-trip is NOT exercised against a real keychain (CI-safe); the
real path is smoke-tested manually in Task 7.

- [ ] **Step 5: Commit**

```bash
git add internal/auth/keychain.go internal/auth/keychain_test.go
git commit -m "feat(auth): add macOS keychain credential store with file fallback"
```

---

## Task 7: `claude auth` CLI (login/logout/status) + apiKeyHelper resolution

**Files:**
- Create: `internal/auth/apikey_helper.go`
- Test: `internal/auth/apikey_helper_test.go`
- Modify: `cmd/claude/main.go` (add `claude auth` dispatch; wire apiKeyHelper into credential resolution)
- Test: `cmd/claude/main_test.go` (add a `claude auth status` case)

**Interfaces:**
- Produces (`apikey_helper.go`):
  - `type APIKeyHelperResolver struct { Command string; TTL time.Duration; Now func() time.Time; run func(ctx context.Context, command string) (string, error) }`
  - `func NewAPIKeyHelperResolver(command string) *APIKeyHelperResolver`
  - `func (r *APIKeyHelperResolver) Resolve(ctx context.Context) (string, error)` — runs the shell
    command, trims stdout, caches for TTL (default 5m, CC `DEFAULT_API_KEY_HELPER_TTL`; env override
    `CLAUDE_CODE_API_KEY_HELPER_TTL_MS`).
- Produces (`cmd/claude/main.go`): top-level `auth` subcommand mirroring `plugin` dispatch at main.go:197.

CC anchors: `utils/auth.ts:_executeApiKeyHelper` (`execa(cmd,{shell:true,timeout:600000})`, stdout
trimmed), `DEFAULT_API_KEY_HELPER_TTL = 5*60*1000`, env `CLAUDE_CODE_API_KEY_HELPER_TTL_MS`; output →
`Authorization: Bearer`. The ccgo setting already exists at `internal/contracts/settings.go:5`
(`APIKeyHelper string`) — confirm with `grep -n "APIKeyHelper" internal/contracts/settings.go
internal/config/settings.go`.

- [ ] **Step 1: Write the failing test**

Create `internal/auth/apikey_helper_test.go`:
```go
package auth

import (
	"context"
	"errors"
	"testing"
	"time"
)

func TestAPIKeyHelperResolveCaches(t *testing.T) {
	calls := 0
	r := &APIKeyHelperResolver{
		Command: "print-key",
		TTL:     time.Minute,
		Now:     func() time.Time { return time.Unix(1000, 0) },
		run: func(ctx context.Context, command string) (string, error) {
			calls++
			return "  sk-from-helper\n", nil
		},
	}
	key, err := r.Resolve(context.Background())
	if err != nil || key != "sk-from-helper" {
		t.Fatalf("Resolve = %q,%v want sk-from-helper,nil", key, err)
	}
	// Second call within TTL must hit the cache, not re-run.
	if _, err := r.Resolve(context.Background()); err != nil {
		t.Fatal(err)
	}
	if calls != 1 {
		t.Fatalf("helper ran %d times; expected cached (1)", calls)
	}
}

func TestAPIKeyHelperResolveExpiresCache(t *testing.T) {
	calls := 0
	now := time.Unix(0, 0)
	r := &APIKeyHelperResolver{
		Command: "print-key",
		TTL:     time.Minute,
		Now:     func() time.Time { return now },
		run:     func(ctx context.Context, command string) (string, error) { calls++; return "k", nil },
	}
	if _, err := r.Resolve(context.Background()); err != nil {
		t.Fatal(err)
	}
	now = now.Add(2 * time.Minute) // past TTL
	if _, err := r.Resolve(context.Background()); err != nil {
		t.Fatal(err)
	}
	if calls != 2 {
		t.Fatalf("helper ran %d times; expected re-run after TTL (2)", calls)
	}
}

func TestAPIKeyHelperEmptyOutputIsError(t *testing.T) {
	r := &APIKeyHelperResolver{
		Command: "noop",
		Now:     time.Now,
		run:     func(ctx context.Context, command string) (string, error) { return "   \n", nil },
	}
	if _, err := r.Resolve(context.Background()); err == nil {
		t.Fatal("empty helper output must be an error")
	}
}

func TestAPIKeyHelperRunFailure(t *testing.T) {
	r := &APIKeyHelperResolver{
		Command: "boom",
		Now:     time.Now,
		run:     func(ctx context.Context, command string) (string, error) { return "", errors.New("exit 1") },
	}
	if _, err := r.Resolve(context.Background()); err == nil {
		t.Fatal("helper failure must propagate")
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/auth/ -run TestAPIKeyHelper -v`
Expected: FAIL — `undefined: APIKeyHelperResolver`.

- [ ] **Step 3: Write minimal implementation**

Create `internal/auth/apikey_helper.go`:
```go
package auth

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"runtime"
	"strconv"
	"strings"
	"sync"
	"time"
)

const defaultAPIKeyHelperTTL = 5 * time.Minute
const apiKeyHelperTimeout = 10 * time.Minute

// APIKeyHelperResolver runs a user-configured shell command whose stdout is an
// API key, caching the result for a TTL (mirrors CC's apiKeyHelper).
type APIKeyHelperResolver struct {
	Command string
	TTL     time.Duration
	Now     func() time.Time
	run     func(ctx context.Context, command string) (string, error)

	mu        sync.Mutex
	cached    string
	cachedAt  time.Time
	hasCached bool
}

// NewAPIKeyHelperResolver builds a resolver for the given shell command.
func NewAPIKeyHelperResolver(command string) *APIKeyHelperResolver {
	return &APIKeyHelperResolver{
		Command: command,
		TTL:     apiKeyHelperTTL(),
		Now:     time.Now,
		run:     runShellCommand,
	}
}

// apiKeyHelperTTL honors CLAUDE_CODE_API_KEY_HELPER_TTL_MS, default 5 minutes.
func apiKeyHelperTTL() time.Duration {
	if v := os.Getenv("CLAUDE_CODE_API_KEY_HELPER_TTL_MS"); v != "" {
		if ms, err := strconv.ParseInt(v, 10, 64); err == nil && ms > 0 {
			return time.Duration(ms) * time.Millisecond
		}
	}
	return defaultAPIKeyHelperTTL
}

// Resolve returns the API key, running the helper if the cache is cold/expired.
func (r *APIKeyHelperResolver) Resolve(ctx context.Context) (string, error) {
	if strings.TrimSpace(r.Command) == "" {
		return "", fmt.Errorf("auth: apiKeyHelper command is empty")
	}
	now := r.now()
	r.mu.Lock()
	if r.hasCached && now.Sub(r.cachedAt) < r.ttl() {
		key := r.cached
		r.mu.Unlock()
		return key, nil
	}
	r.mu.Unlock()

	tctx, cancel := context.WithTimeout(ctx, apiKeyHelperTimeout)
	defer cancel()
	out, err := r.run(tctx, r.Command)
	if err != nil {
		// Never include the command output (may contain a key) in the error.
		return "", fmt.Errorf("auth: apiKeyHelper failed: %w", err)
	}
	key := strings.TrimSpace(out)
	if key == "" {
		return "", fmt.Errorf("auth: apiKeyHelper returned no value")
	}
	r.mu.Lock()
	r.cached, r.cachedAt, r.hasCached = key, now, true
	r.mu.Unlock()
	return key, nil
}

func (r *APIKeyHelperResolver) now() time.Time {
	if r.Now != nil {
		return r.Now()
	}
	return time.Now()
}

func (r *APIKeyHelperResolver) ttl() time.Duration {
	if r.TTL > 0 {
		return r.TTL
	}
	return defaultAPIKeyHelperTTL
}

// runShellCommand runs command through the platform shell (matches CC's
// execa({shell:true})).
func runShellCommand(ctx context.Context, command string) (string, error) {
	var cmd *exec.Cmd
	if runtime.GOOS == "windows" {
		cmd = exec.CommandContext(ctx, "cmd", "/C", command)
	} else {
		cmd = exec.CommandContext(ctx, "sh", "-c", command)
	}
	out, err := cmd.Output()
	return string(out), err
}
```

Now wire it into `cmd/claude/main.go`. First add the `claude auth` dispatch, mirroring the `plugin`
branch (verified at main.go:197 `if ... strings.EqualFold(flags.Args()[0], "plugin") { return
runPluginCLI(...) }`). Add an analogous guard:
```go
	if !*printMode && len(flags.Args()) > 0 && strings.EqualFold(flags.Args()[0], "auth") {
		return runAuthCLI(context.Background(), state, flags.Args()[1:], stdout, stderr)
	}
```
And the handler (new function near `runPluginCLI`):
```go
func runAuthCLI(ctx context.Context, state *bootstrap.State, args []string, stdout io.Writer, stderr io.Writer) int {
	if len(args) == 0 {
		fmt.Fprintln(stderr, "ccgo auth: missing subcommand (login|logout|status)")
		return 2
	}
	store := auth.NewKeychainCredentialStore("")
	switch strings.ToLower(strings.TrimSpace(args[0])) {
	case "login":
		// Gray-zone consent gate (see plan intro).
		fmt.Fprintln(stdout, "OAuth login uses Anthropic's official client and endpoints.")
		fmt.Fprintln(stdout, "This is a ToS/account-policy gray area. Opening your browser to sign in...")
		creds, err := auth.RunLoginFlow(ctx, auth.LoginOptions{
			Browser:           auth.NewOSBrowserOpener(),
			Store:             store,
			LoginWithClaudeAI: true,
			OnURL:             func(u string) { fmt.Fprintf(stdout, "If your browser did not open, visit:\n%s\n", u) },
		})
		if err != nil {
			fmt.Fprintf(stderr, "ccgo auth: login failed: %v\n", err)
			return 1
		}
		_ = creds // never print tokens
		fmt.Fprintln(stdout, "Login successful.")
		return 0
	case "logout":
		if err := store.Delete(ctx); err != nil {
			fmt.Fprintf(stderr, "ccgo auth: logout failed: %v\n", err)
			return 1
		}
		fmt.Fprintln(stdout, "Signed out. Stored credentials removed.")
		return 0
	case "status":
		creds, err := store.Load(ctx)
		if err != nil {
			fmt.Fprintf(stderr, "ccgo auth: %v\n", err)
			return 1
		}
		switch creds.Source {
		case auth.SourceOAuth:
			fmt.Fprintln(stdout, "Authenticated via OAuth.")
		case auth.SourceAPIKey:
			fmt.Fprintln(stdout, "Authenticated via API key.")
		default:
			fmt.Fprintln(stdout, "Not authenticated. Run `claude auth login` or set ANTHROPIC_API_KEY.")
		}
		return 0
	default:
		fmt.Fprintf(stderr, "ccgo auth: unknown subcommand %s\n", args[0])
		return 2
	}
}
```
Add `"ccgo/internal/auth"` to main.go imports if not present (confirm with
`grep -n "ccgo/internal/auth" cmd/claude/main.go`).

Then wire `apiKeyHelper` into credential resolution. Find where `Credentials` are resolved for the
client (verified seam: `auth.FromEnv()` + `auth.NewFileCredentialStore` feed the anthropic client via
`WithCredentials`/`WithAccessTokenProvider`; locate the exact call with
`grep -rn "FromEnv\|WithCredentials\|NewFileCredentialStore\|CredentialStore" cmd/claude/ internal/bootstrap/`).
At that resolution point, after env/keychain but honoring CC's precedence (apiKeyHelper, once
configured, wins over keychain/OAuth — CC `utils/auth.ts:320-335`), insert:
```go
	if helperCmd := strings.TrimSpace(settings.APIKeyHelper); helperCmd != "" {
		if key, err := auth.NewAPIKeyHelperResolver(helperCmd).Resolve(ctx); err == nil && key != "" {
			creds = auth.Credentials{Source: auth.SourceAPIKey, APIKey: key}
		}
		// On helper error, fall through to env/keychain credentials (do not abort).
	}
```
Confirm the `settings` value carrying `APIKeyHelper` is in scope at that point
(`grep -n "Settings\|settings\." cmd/claude/main.go | head`); it is merged at
`internal/config/settings.go:40`. Replace the existing plaintext `NewFileCredentialStore` construction
in the credential-resolution path with `auth.NewKeychainCredentialStore("")` so OAuth tokens persist to
the keychain on macOS — verify the existing construction site first and swap it in place (one line).

- [ ] **Step 4: Build, run tests, smoke-test**

Run:
```bash
go build ./... && go vet ./... && go test ./internal/auth/ ./internal/commands/ ./internal/conversation/ ./cmd/claude/ -v
```
Expected: build OK, vet clean, package tests PASS.

Manual smoke tests (cannot be fully automated — login needs a browser/IdP; status/logout can be run):
```bash
# status with no creds:
unset ANTHROPIC_API_KEY; go run ./cmd/claude auth status
#   -> "Not authenticated. ..."

# apiKeyHelper path (no real key needed to prove resolution):
echo '{"apiKeyHelper":"printf sk-test-123"}' > /tmp/s.json   # then point settings at it / or set in ~/.claude/settings.json
go run ./cmd/claude auth status   # with a configured helper, status reflects API key source

# macOS keychain round-trip (real):
go run ./cmd/claude auth login    # consent line prints, browser opens; after IdP approve -> "Login successful."
go run ./cmd/claude auth status   # -> "Authenticated via OAuth."
security find-generic-password -s "Claude Code-credentials" -w   # confirms token is in Keychain, not credentials.json
go run ./cmd/claude auth logout   # -> "Signed out."
```

- [ ] **Step 5: Commit**

```bash
git add internal/auth/apikey_helper.go internal/auth/apikey_helper_test.go cmd/claude/main.go cmd/claude/main_test.go
git commit -m "feat(claude): add claude auth CLI and apiKeyHelper credential resolution"
```

---

## Self-Review

**Spec coverage (Phase-4 brief = first-time interactive login from zero + keychain + apiKeyHelper):**
- (1) Local callback HTTP listener — ephemeral port (`127.0.0.1:0`), `state` CSRF validation, code
  capture, IdP-error handling → **Task 2**. ✓
- (2) Open the system browser to the authorize URL — cross-platform, `$BROWSER` override, scheme
  validation, injectable seam → **Task 1**. ✓
- (3) `authorization_code` → token exchange + store + refresh integration — JSON body, response
  validation, reuses `oauthTokenResponse`; refresh already exists in `token_provider.go` and consumes
  the same `Credentials` → **Task 3** (exchange) + **Task 4** (orchestration persists via the
  `CredentialStore` the refresher already uses). ✓
- (4) `/login` `/logout` slash commands + `claude auth` CLI subcommand → **Task 5** (slash) + **Task 7**
  (CLI login/logout/status). ✓
- (5) Token keychain storage replacing plaintext — macOS `security` CLI, file fallback elsewhere, same
  `CredentialStore` interface → **Task 6**, wired in **Task 7**. ✓
- (6) `apiKeyHelper` support — cached shell-command resolver, CC-matching TTL/precedence → **Task 7**. ✓
- ToS gray-zone flagged prominently in the intro + an opt-in consent gate in both `/login` routing and
  `claude auth login`. ✓

**Deferred to later phases (explicitly NOT in Phase 4, by design):**
- In-REPL interactive `/login` browser launch with a TUI progress/spinner — the *command* and the full
  flow land here; the in-REPL ceremony (rendering the "waiting for browser…" dialog) is Phase 2 UI work
  (master §3: "Phase 6b `/login` `/logout` overlaps Phase 4; keep auth commands in Phase 4"). Task 5
  therefore routes the interactive `/login` to the consent-gated CLI flow for now.
- Linux Secret Service / Windows Credential Manager — deferred exactly as CC defers them
  (`// TODO: add libsecret`); the `KeychainStore` interface leaves the seam open.
- Manual-paste (`MANUAL_REDIRECT_URL`) fallback as a *separate* entry path — the URL is already shown
  (`OnURL`) so a user can copy it, but a dedicated "paste the code" prompt is not built here.

**Keychain dependency decision (master §6):** **No new dependency.** macOS uses stdlib `os/exec` →
`/usr/bin/security` (exactly what CC does); other platforms reuse the existing chmod-0600
`FileCredentialStore`. This avoids cgo/dbus-pulling keyring libraries while matching CC's real behavior
and the "tokens in keychain not plaintext" security goal on the platform CC itself secures.

**Security self-check:** no hardcoded secrets (public `client_id` already in repo, PKCE has no secret);
tokens never appear in errors, logs, the browser page, or argv (keychain `Set` uses `security -i` stdin);
callback validates path/`state`/`code` and rejects `error=`; token response is size-limited and
status-checked; apiKeyHelper errors never echo output; `defer listener.Close()` releases the port on
every path.

**Type/interface consistency:** `CredentialStore` (`Load`/`Save`/`Delete`) is the single storage
interface reused by `FileCredentialStore`, `keychainCredentialStore`, `OAuthTokenProvider`, and the new
flow — no caller signature changes. `oauthTokenResponse` and `defaultOAuthTokenResponseLimit` are reused
from `token_provider.go` (confirmed). `Credentials`/`SourceOAuth`/`SourceAPIKey`/`ParseScopes`/
`BuildAuthURL`/`AuthURLParams` are all existing, verified symbols.

**Verification-before-completion:** every assumed existing symbol is flagged with the exact `go doc`/
`grep` to confirm at its point of use — `AuthURLParams` fields, `oauthTokenResponse`/limit const,
`ExecuteBuiltinLocalCommand`/`BuiltinCommands` shapes, `Result` struct booleans, the runner's
credential-store field, `cmd/claude/main.go`'s plugin-dispatch pattern + credential-resolution seam,
and `contracts.Settings.APIKeyHelper`. None assumed silently.

**Gate (master §4):** "new user logs in from zero; token in keychain." Demonstrated by the Task 7 manual
smoke test (`claude auth login` → `auth status` shows OAuth → `security find-generic-password` shows the
token in Keychain, absent from `credentials.json`), plus the fully-automated `RunLoginFlow` test
(Task 4) that drives listener→browser-seam→exchange→store end-to-end with `httptest`.
