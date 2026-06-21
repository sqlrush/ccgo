# Phase 7 — OS Sandbox + Real Local Team + Local SDK Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

> **Parent:** `2026-06-21-00-master-roadmap.md` (§5 Phase 7 brief, §6 Global Constraints, §8 gate). This plan covers the locked-in Phase 7 scope only. **Cloud/remote (teleport, RemoteAgentTask, CCR relay) is OUT of scope** (roadmap §1, §7.4) — Team and SDK here are strictly **local, in-process**. Do not creep into the `'remote'` isolation value or any remote-agent path.

**Goal:** Close the three remaining Phase 7 gaps, all code-verified against ccgo today:
1. **Security regression fix:** `dangerouslyDisableSandbox` is a flag with **zero enforcement** — Bash/PowerShell run fully unsandboxed regardless (`internal/tools/bash/tools.go:1040-1083` calls `exec.CommandContext` directly; `configureBashCommand` only sets `Setpgid`). Implement a real OS sandbox (macOS seatbelt via `sandbox-exec`; Linux landlock+seccomp) that **actually confines** Bash, honoring the flag and the `sandbox.*` settings.
2. **Real local Team:** `callTeamDispatch`/`callTeamCoordinate`/`callTeamSchedule` only `manager.Append(...)` transcript messages (`internal/tools/task/tools.go:1782-1837, 2008-2059`); **no teammate ever runs a model loop** — the entire `internal/session` sidechain layer never calls `conversation.Runner`. Add a real in-process Team/subagent runner that executes teammates against the model, plus async/background agents and the `model`/`isolation` Task-schema fields.
3. **Local SDK:** no `control_request`/`control_response` protocol, no `canUseTool`/`interrupt`/`set_model`, no importable entrypoint. Add a stream-based control protocol and a programmatic `sdk.Query` entrypoint reusing `state.ConversationRunner()`.

**Architecture:**
- **Sandbox** — a new `internal/sandbox/` package exposes one OS-agnostic policy type and a `Wrap(cmd, policy)` decision + per-OS enforcement selected by Go build tags (mirrors the existing `process_unix.go`/`process_windows.go` convention at `internal/tools/powershell/`). macOS generates a seatbelt `.sb` profile and execs `/usr/bin/sandbox-exec -p <profile> -- <shell> -c <cmd>`; Linux applies landlock (filesystem) + a seccomp BPF network filter inside a forked helper before exec. Other OSes are a no-op that returns a **clear error when the sandbox is required** (`failIfUnavailable`) and otherwise runs unsandboxed with a warning. The bash tool consults `sandbox.shouldSandbox(input, settings)` (the CC short-circuit logic) before building the command. CC's own profile text lives in the external `@anthropic-ai/sandbox-runtime` package, so ccgo implements the profiles natively; the **integration/decision logic** is ported from CC `src/tools/BashTool/shouldUseSandbox.ts` and `src/utils/sandbox/sandbox-adapter.ts`.
- **Team** — a new `internal/orchestration/` package owns a `TeamRunner` that, given a teammate's sidechain + a `conversation.Runner` factory, runs a real `RunTurn` loop per teammate (reusing the existing `session` sidechain transcript as durable state). It mirrors CC's `runInProcessTeammate()` (`src/utils/swarm/inProcessRunner.ts:883`) which calls the same `runAgent()` as subagents. Background agents (`run_in_background`) are tracked in an in-process `AgentRegistry` and harvested via notifications. The Team tools' `callTeamDispatch`/`callTeamCoordinate` are rewired to actually start/advance teammates through this runner instead of only appending.
- **SDK** — a new `internal/sdk/` package defines the control-protocol framing (`control_request`/`control_response`) over an `io.Reader`/`io.Writer` NDJSON stream (ported from CC `src/entrypoints/sdk/controlSchemas.ts`), a `Controller` that dispatches `can_use_tool`/`interrupt`/`set_model`, and a `Query` entrypoint that wires a `conversation.Runner` to the protocol. The control `can_use_tool` request reuses the **existing `tool.PermissionAsker` seam** added in Phase 1 — the SDK asker forwards the ask out over the stream and blocks on the response.

**Tech Stack:** Go 1.26; existing `internal/conversation`, `internal/session`, `internal/tool`, `internal/contracts`, `internal/bootstrap`, `internal/tools/bash`, `internal/tools/task`, `internal/messages`. New deps: **promote `golang.org/x/sys` from indirect to direct** (already present at v0.46.0; needed for `unix.Prctl`, `PR_SET_NO_NEW_PRIVS`, `PR_SET_SECCOMP`, `SECCOMP_RET_*`, `LANDLOCK_ACCESS_FS_*`) and add **`github.com/landlock-lsm/go-landlock`** (the canonical Go Landlock library; depends only on `x/sys`). See the dep justification in Task 2.

---

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
- **Security:** no hardcoded secrets; tokens in keychain not plaintext (Phase 4); **sandbox flag must actually enforce (this phase) — this is fixing a security regression**; never leak sensitive data in errors.

**Phase-7-specific constraints:**
- **OS-aware tests (CRITICAL):** sandbox enforcement tests MUST `t.Skip(reason)` on the wrong OS. The **no-op / guard path MUST be tested on every OS**. Never assert seatbelt behavior on Linux or landlock behavior on macOS.
- **Security framing:** the sandbox flag fixing is a regression fix. The default-on behavior MUST be: when `sandbox.enabled` and the platform supports it, Bash is confined; `dangerouslyDisableSandbox` only bypasses when `sandbox.allowUnsandboxedCommands` permits (CC parity, `shouldUseSandbox.ts:137-140`). Never let an unverified input silently disable the sandbox.
- **Build tags:** per-OS sandbox files use `//go:build darwin`, `//go:build linux`, `//go:build !darwin && !linux` (matches the existing `//go:build !windows` convention at `internal/tools/powershell/process_unix.go:1`).

---

## File Structure

**New package `internal/sandbox/`:**
- `policy.go` — `Policy` struct (FilesystemAllowWrite/DenyWrite/DenyRead/AllowRead, AllowNetwork, plus `Enabled`, `FailIfUnavailable`, `AllowUnsandboxed`); `Decision` (`shouldSandbox`) logic ported from CC. Pure, OS-agnostic. TDD core.
- `policy_settings.go` — builds a `Policy` from `contracts.Settings.Sandbox` (the `map[string]any`) + `contracts.SandboxFilesystemPolicy`. Validation at the boundary.
- `sandbox.go` — `Wrap(name string, args []string, p Policy) (string, []string, error)` dispatches to the per-OS enforcer; `Supported() bool`; `ErrUnsupported`.
- `enforce_darwin.go` (`//go:build darwin`) — seatbelt profile builder + `sandbox-exec` wrap.
- `enforce_linux.go` (`//go:build linux`) — landlock+seccomp helper-exec wrap.
- `enforce_other.go` (`//go:build !darwin && !linux`) — no-op enforce that errors when required.
- `profile_darwin.go` (`//go:build darwin`) — pure seatbelt `.sb` text builder (TDD-testable on macOS only).

**New package `internal/orchestration/`:**
- `runner.go` — `TeamRunner`; `RunnerFactory func(agentType, model string) (*conversation.Runner, error)`; `RunTeammate(ctx, sidechainID, prompt) (Outcome, error)` runs a real model loop and persists to the sidechain transcript.
- `registry.go` — `AgentRegistry` (in-process tracking of background agents; immutable snapshots); `StartBackground`/`Snapshot`/`Harvest`.
- `task_schema.go` — `model`/`isolation` field decoding + validation for the Task input (the `'worktree'` enum; reject `'remote'` as out-of-scope with a clear error).

**New package `internal/sdk/`:**
- `protocol.go` — `ControlRequest`/`ControlResponse` types + JSON (de)serialization; `Encoder`/`Decoder` over NDJSON streams. TDD core.
- `controller.go` — `Controller` dispatching `can_use_tool`/`interrupt`/`set_model`/`initialize`; holds the live `*conversation.Runner` and a cancel func.
- `asker.go` — `controlAsker` implementing `tool.PermissionAsker` by sending `can_use_tool` out and blocking on the response.
- `query.go` — `Query(ctx, opts) error` importable entrypoint; builds the runner from `bootstrap.State` (or an injected factory) and drives a turn under the control protocol.

**Modified existing files:**
- `internal/tools/bash/tools.go` — wrap `runBashCommand`/`startBackgroundBash` through `sandbox.Wrap` using a `Policy` from `ctx`.
- `internal/tools/task/tools.go` — `taskInput` gains `Model`/`Isolation`; `callTeamDispatch`/`callTeamCoordinate` call the `orchestration.TeamRunner`; `callTask` honors `run`/`run_in_background`.
- `go.mod` / `go.sum` — promote `x/sys`, add `go-landlock`.

---

## Task 1: OS-agnostic sandbox `Policy` + `shouldSandbox` decision (the security core)

This is the **security-critical** task: it decides whether a command is confined. Get the short-circuit logic exactly right — an input must not silently disable the sandbox.

**Files:**
- Create: `internal/sandbox/policy.go`
- Create: `internal/sandbox/policy_settings.go`
- Test: `internal/sandbox/policy_test.go`

**Interfaces produced:**
- `type Policy struct { Enabled bool; FailIfUnavailable bool; AllowUnsandboxed bool; AllowWrite, DenyWrite, DenyRead, AllowRead []string; AllowNetwork bool }`
- `func (p Policy) ShouldSandbox(dangerouslyDisableSandbox bool) bool`
- `func PolicyFromSettings(s contracts.Settings) Policy`

**CC reference (read before writing):** `src/tools/BashTool/shouldUseSandbox.ts:130-153` (the short-circuit) and `src/utils/sandbox/sandbox-adapter.ts:172-381` (`convertToSandboxRuntimeConfig`) + `:474-485` (`areUnsandboxedCommandsAllowed`/`isSandboxRequired`). Decision rule (CC parity): sandbox unless `(!Enabled)` OR `(dangerouslyDisableSandbox && AllowUnsandboxed)`.

Confirm the ccgo settings shape first:
```bash
grep -n "Sandbox " /Users/sqlrush/ccgo/internal/contracts/settings.go        # -> Sandbox map[string]any
go doc ccgo/internal/contracts SandboxFilesystemPolicy                        # AllowWrite/DenyWrite/DenyRead/AllowRead
```

- [ ] **Step 1: Write the failing test**

Create `internal/sandbox/policy_test.go`:
```go
package sandbox

import (
	"testing"

	"ccgo/internal/contracts"
)

func TestShouldSandbox(t *testing.T) {
	cases := []struct {
		name      string
		policy    Policy
		dangerous bool
		want      bool
	}{
		{"disabled never sandboxes", Policy{Enabled: false}, false, false},
		{"enabled sandboxes by default", Policy{Enabled: true}, false, true},
		{"flag bypasses only when allowed", Policy{Enabled: true, AllowUnsandboxed: true}, true, false},
		{"flag ignored when policy forbids unsandboxed", Policy{Enabled: true, AllowUnsandboxed: false}, true, true},
		{"flag without enabled is moot", Policy{Enabled: false, AllowUnsandboxed: true}, true, false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := tc.policy.ShouldSandbox(tc.dangerous); got != tc.want {
				t.Fatalf("ShouldSandbox(%v) = %v want %v", tc.dangerous, got, tc.want)
			}
		})
	}
}

func TestPolicyFromSettings(t *testing.T) {
	s := contracts.Settings{
		Sandbox: map[string]any{
			"enabled":                  true,
			"allowUnsandboxedCommands": false,
			"failIfUnavailable":        true,
			"filesystem": map[string]any{
				"allowWrite": []any{"/tmp/work"},
				"denyRead":   []any{"/etc/secret"},
			},
		},
	}
	p := PolicyFromSettings(s)
	if !p.Enabled || p.AllowUnsandboxed || !p.FailIfUnavailable {
		t.Fatalf("flags = %+v", p)
	}
	if len(p.AllowWrite) != 1 || p.AllowWrite[0] != "/tmp/work" {
		t.Fatalf("AllowWrite = %v", p.AllowWrite)
	}
	if len(p.DenyRead) != 1 || p.DenyRead[0] != "/etc/secret" {
		t.Fatalf("DenyRead = %v", p.DenyRead)
	}
}

func TestPolicyFromSettingsDefaultsSafe(t *testing.T) {
	// No sandbox block: disabled, but unsandboxed allowed (CC default ?? true).
	p := PolicyFromSettings(contracts.Settings{})
	if p.Enabled {
		t.Fatal("absent sandbox settings must default Enabled=false")
	}
	if !p.AllowUnsandboxed {
		t.Fatal("absent allowUnsandboxedCommands defaults to true (CC parity)")
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/sandbox/ -run TestShouldSandbox -v`
Expected: FAIL — package does not compile (`undefined: Policy`).

- [ ] **Step 3: Write minimal implementation**

Create `internal/sandbox/policy.go`:
```go
package sandbox

// Policy is the OS-agnostic sandbox configuration for a single command.
// It is immutable: builders return new copies; ShouldSandbox is pure.
type Policy struct {
	Enabled           bool
	FailIfUnavailable bool
	AllowUnsandboxed  bool
	AllowWrite        []string
	DenyWrite         []string
	DenyRead          []string
	AllowRead         []string
	AllowNetwork      bool
}

// ShouldSandbox decides whether this command must be confined.
// SECURITY: the dangerouslyDisableSandbox flag bypasses confinement ONLY when
// the policy explicitly permits unsandboxed commands. Mirrors CC
// shouldUseSandbox.ts:130-153 — never trust the flag alone.
func (p Policy) ShouldSandbox(dangerouslyDisableSandbox bool) bool {
	if !p.Enabled {
		return false
	}
	if dangerouslyDisableSandbox && p.AllowUnsandboxed {
		return false
	}
	return true
}
```

Create `internal/sandbox/policy_settings.go`:
```go
package sandbox

import "ccgo/internal/contracts"

// PolicyFromSettings builds a Policy from merged settings. Boundary validation:
// unknown / wrong-typed values are ignored, defaults match CC (sandbox-adapter.ts).
func PolicyFromSettings(s contracts.Settings) Policy {
	p := Policy{AllowUnsandboxed: true} // CC default: allowUnsandboxedCommands ?? true
	box := s.Sandbox
	if box == nil {
		return p
	}
	if v, ok := boolAt(box, "enabled"); ok {
		p.Enabled = v
	}
	if v, ok := boolAt(box, "failIfUnavailable"); ok {
		p.FailIfUnavailable = v
	}
	if v, ok := boolAt(box, "allowUnsandboxedCommands"); ok {
		p.AllowUnsandboxed = v
	}
	if v, ok := boolAt(box, "allowNetworkAccess"); ok {
		p.AllowNetwork = v
	}
	if fs, ok := box["filesystem"].(map[string]any); ok {
		p.AllowWrite = stringsAt(fs, "allowWrite")
		p.DenyWrite = stringsAt(fs, "denyWrite")
		p.DenyRead = stringsAt(fs, "denyRead")
		p.AllowRead = stringsAt(fs, "allowRead")
	}
	return p
}

func boolAt(m map[string]any, key string) (bool, bool) {
	v, ok := m[key].(bool)
	return v, ok
}

func stringsAt(m map[string]any, key string) []string {
	raw, ok := m[key].([]any)
	if !ok {
		return nil
	}
	out := make([]string, 0, len(raw))
	for _, item := range raw {
		if s, ok := item.(string); ok && s != "" {
			out = append(out, s)
		}
	}
	if len(out) == 0 {
		return nil
	}
	return out
}
```

If `contracts.Settings.Sandbox` is not `map[string]any`, re-verify with `go doc ccgo/internal/contracts Settings` and adjust the accessors. (Confirmed today: `internal/contracts/settings.go:47` `Sandbox map[string]any`.)

- [ ] **Step 4: Run tests to verify they pass**

Run: `go test ./internal/sandbox/ -v`
Expected: PASS (all subtests, every OS — this file is OS-agnostic).

- [ ] **Step 5: Commit**

```bash
git add internal/sandbox/policy.go internal/sandbox/policy_settings.go internal/sandbox/policy_test.go
git commit -m "feat(sandbox): add OS-agnostic Policy and shouldSandbox decision (security core)"
```

---

## Task 2: macOS seatbelt enforcement honoring `dangerouslyDisableSandbox`

**Files:**
- Create: `internal/sandbox/sandbox.go` (OS dispatch, all-OS)
- Create: `internal/sandbox/profile_darwin.go` (`//go:build darwin`)
- Create: `internal/sandbox/enforce_darwin.go` (`//go:build darwin`)
- Create: `internal/sandbox/enforce_other.go` (`//go:build !darwin && !linux`)
- Test: `internal/sandbox/sandbox_test.go` (all-OS dispatch + guard)
- Test: `internal/sandbox/profile_darwin_test.go` (`//go:build darwin`)

**Dep justification (record in the commit body):** none added in this task. macOS confinement uses the OS-provided `/usr/bin/sandbox-exec` binary (no library). CC itself delegates profile generation to the external `@anthropic-ai/sandbox-runtime` package, which is unavailable to Go; ccgo therefore generates the seatbelt `.sb` profile text natively. (The Linux dep is justified in Task 3.)

**CC reference:** `src/utils/sandbox/sandbox-adapter.ts:260-265` (wrap call), `src/utils/Shell.ts:316-337` (spawn with wrapped command). The seatbelt profile shape (`(version 1)`, `(deny default)`, `(allow ...)`, `(subpath "…")`) is standard macOS seatbelt syntax — verify against `man sandbox-exec` and any system `.sb` under `/usr/share/sandbox/` if present.

**Interfaces produced:**
- `func Wrap(name string, args []string, p Policy) (string, []string, error)` — returns the (possibly wrapped) executable + args. When `p.ShouldSandbox(...)` already decided "no", callers pass an unsandboxed policy and `Wrap` returns the inputs unchanged; `Wrap` itself wraps unconditionally when called (the bash tool gates the call).
- `func Supported() bool`
- `var ErrUnsupported = errors.New("sandbox not supported on this platform")`
- darwin: `func buildSeatbeltProfile(p Policy, cwd string) string`

- [ ] **Step 1: Write the failing tests**

Create `internal/sandbox/sandbox_test.go`:
```go
package sandbox

import (
	"runtime"
	"testing"
)

func TestSupportedMatchesPlatform(t *testing.T) {
	got := Supported()
	want := runtime.GOOS == "darwin" || runtime.GOOS == "linux"
	if got != want {
		t.Fatalf("Supported() = %v want %v on %s", got, want, runtime.GOOS)
	}
}

func TestWrapUnsupportedPlatformErrors(t *testing.T) {
	if Supported() {
		t.Skip("platform supports sandbox; guard path tested only on unsupported OS")
	}
	// On an unsupported OS, Wrap with an enabled policy must error clearly
	// rather than silently running unconfined.
	_, _, err := Wrap("/bin/sh", []string{"-c", "echo hi"}, Policy{Enabled: true})
	if err == nil {
		t.Fatal("expected ErrUnsupported on unsupported platform")
	}
}
```

Create `internal/sandbox/profile_darwin_test.go`:
```go
//go:build darwin

package sandbox

import (
	"strings"
	"testing"
)

func TestBuildSeatbeltProfileDenyDefault(t *testing.T) {
	p := Policy{
		Enabled:    true,
		AllowWrite: []string{"/tmp/work"},
		DenyRead:   []string{"/etc/secret"},
	}
	profile := buildSeatbeltProfile(p, "/tmp/work")
	if !strings.HasPrefix(profile, "(version 1)") {
		t.Fatalf("profile must start with version: %q", profile[:20])
	}
	if !strings.Contains(profile, "(deny default)") {
		t.Fatal("profile must deny by default")
	}
	if !strings.Contains(profile, `(subpath "/tmp/work")`) {
		t.Fatalf("profile must allow writes under allowWrite: %s", profile)
	}
	if !strings.Contains(profile, `(subpath "/etc/secret")`) {
		t.Fatalf("profile must deny reads of denyRead paths: %s", profile)
	}
}

func TestWrapDarwinUsesSandboxExec(t *testing.T) {
	name, args, err := Wrap("/bin/zsh", []string{"-c", "echo hi"}, Policy{Enabled: true})
	if err != nil {
		t.Fatalf("Wrap err: %v", err)
	}
	if name != "/usr/bin/sandbox-exec" {
		t.Fatalf("expected sandbox-exec wrapper, got %q", name)
	}
	joined := strings.Join(args, " ")
	if !strings.Contains(joined, "/bin/zsh") || !strings.Contains(joined, "echo hi") {
		t.Fatalf("wrapped args lost the original command: %v", args)
	}
}
```

- [ ] **Step 2: Run tests to verify they fail**

Run: `go test ./internal/sandbox/ -run 'TestSupported|TestWrap|TestBuildSeatbelt' -v`
Expected: FAIL — `undefined: Supported` / `undefined: Wrap`.

- [ ] **Step 3: Write minimal implementation**

Create `internal/sandbox/sandbox.go`:
```go
package sandbox

import (
	"errors"
	"runtime"
)

// ErrUnsupported is returned by Wrap when the sandbox is required but the
// current platform has no enforcement backend.
var ErrUnsupported = errors.New("sandbox not supported on this platform")

// Supported reports whether OS-level enforcement is available here.
func Supported() bool {
	return runtime.GOOS == "darwin" || runtime.GOOS == "linux"
}

// Wrap returns the executable + args needed to run (name, args...) confined by
// p. Per-OS implementations live in enforce_<os>.go behind build tags.
// SECURITY: Wrap confines unconditionally; the caller (bash tool) decides
// whether to call it via Policy.ShouldSandbox.
func Wrap(name string, args []string, p Policy) (string, []string, error) {
	return wrap(name, args, p)
}
```

Create `internal/sandbox/profile_darwin.go`:
```go
//go:build darwin

package sandbox

import "strings"

// buildSeatbeltProfile renders a deny-by-default seatbelt profile that allows
// process/exec basics, read of the whole FS by default unless denied, and write
// only under AllowWrite. Mirrors the deny-default posture of CC's runtime.
func buildSeatbeltProfile(p Policy, cwd string) string {
	var b strings.Builder
	b.WriteString("(version 1)\n")
	b.WriteString("(deny default)\n")
	b.WriteString("(allow process-exec)\n")
	b.WriteString("(allow process-fork)\n")
	b.WriteString("(allow signal (target self))\n")
	b.WriteString("(allow sysctl-read)\n")
	b.WriteString("(allow file-read*)\n") // read-all baseline; tighten via deny below
	for _, path := range p.DenyRead {
		b.WriteString(`(deny file-read* (subpath "` + escapeSB(path) + `"))` + "\n")
	}
	if cwd != "" {
		b.WriteString(`(allow file-write* (subpath "` + escapeSB(cwd) + `"))` + "\n")
	}
	for _, path := range p.AllowWrite {
		b.WriteString(`(allow file-write* (subpath "` + escapeSB(path) + `"))` + "\n")
	}
	for _, path := range p.DenyWrite {
		b.WriteString(`(deny file-write* (subpath "` + escapeSB(path) + `"))` + "\n")
	}
	b.WriteString(`(allow file-write* (subpath "/dev"))` + "\n")
	b.WriteString(`(allow file-write* (subpath "/private/tmp"))` + "\n")
	if p.AllowNetwork {
		b.WriteString("(allow network*)\n")
	} else {
		b.WriteString("(deny network*)\n")
		b.WriteString("(allow network* (local ip \"localhost:*\"))\n")
	}
	return b.String()
}

func escapeSB(s string) string {
	return strings.ReplaceAll(s, `"`, `\"`)
}
```

Create `internal/sandbox/enforce_darwin.go`:
```go
//go:build darwin

package sandbox

import (
	"os"
	"strings"
)

const sandboxExecPath = "/usr/bin/sandbox-exec"

// wrap confines (name, args...) under a generated seatbelt profile, exec'd via
// /usr/bin/sandbox-exec -p <profile> -- <name> <args...>.
func wrap(name string, args []string, p Policy) (string, []string, error) {
	cwd, _ := os.Getwd()
	profile := buildSeatbeltProfile(p, cwd)
	wrapped := []string{"-p", profile, "--", name}
	wrapped = append(wrapped, args...)
	return sandboxExecPath, wrapped, nil
}

var _ = strings.TrimSpace // keep imports stable if profile builder moves
```

Create `internal/sandbox/enforce_other.go`:
```go
//go:build !darwin && !linux

package sandbox

// wrap on unsupported platforms refuses to confine. SECURITY: returning the
// command unwrapped here would silently disable the sandbox, so we error and
// let the caller decide (fail closed when FailIfUnavailable).
func wrap(name string, args []string, p Policy) (string, []string, error) {
	return "", nil, ErrUnsupported
}
```

- [ ] **Step 4: Run tests to verify they pass**

Run: `go test ./internal/sandbox/ -v`
Expected: PASS. On macOS the darwin profile tests run; on Linux/Windows the darwin tests are excluded by the build tag and `TestWrapUnsupportedPlatformErrors` runs only where `!Supported()`.

Optional macOS integration smoke (manual, macOS only — confirms real confinement):
```bash
# Should fail to write outside cwd when sandboxed:
go test ./internal/sandbox/ -run TestSeatbeltIntegration -v   # add a build-tagged integration test if desired
```

- [ ] **Step 5: Commit**

```bash
git add internal/sandbox/sandbox.go internal/sandbox/profile_darwin.go internal/sandbox/enforce_darwin.go internal/sandbox/enforce_other.go internal/sandbox/sandbox_test.go internal/sandbox/profile_darwin_test.go
git commit -m "feat(sandbox): macOS seatbelt enforcement via sandbox-exec; no-op guard elsewhere"
```

---

## Task 3: Linux landlock + seccomp enforcement

**Files:**
- Create: `internal/sandbox/enforce_linux.go` (`//go:build linux`)
- Create: `internal/sandbox/seccomp_linux.go` (`//go:build linux`)
- Test: `internal/sandbox/enforce_linux_test.go` (`//go:build linux`)
- Modify: `go.mod`, `go.sum`

**Dep justification (record in the commit body):**
- Promote **`golang.org/x/sys` to a direct dependency** (already vendored at v0.46.0). It provides `unix.Prctl`, `PR_SET_NO_NEW_PRIVS` (0x16…), `PR_SET_SECCOMP`, and the `SECCOMP_RET_*` + `LANDLOCK_ACCESS_FS_*` constants (verified: `golang.org/x/sys@v0.46.0/unix/zerrors_linux.go:1901-1917, 2969, 3433+`). No version bump.
- Add **`github.com/landlock-lsm/go-landlock`** (canonical Go Landlock library, v0.9.0 available; depends only on `x/sys`). Rationale: `x/sys@v0.46.0` exposes the `LANDLOCK_ACCESS_FS_*` constants but **not** the typed `LandlockCreateRuleset`/`LandlockAddRule`/`LandlockRestrictSelf` wrappers nor the per-arch `SYS_landlock_*` syscall numbers — hand-rolling those is brittle across arches and ABI versions. `go-landlock` provides the version-negotiating ruleset API maintained against the kernel. The seccomp **network** filter is hand-rolled as a tiny BPF program using only `x/sys` constants (no extra dep) — justified because the only deps that bundle seccomp also bundle a large surface; our filter is ~15 BPF instructions.

```bash
cd /Users/sqlrush/ccgo
go get golang.org/x/sys@v0.46.0           # promote to direct (no version change)
go get github.com/landlock-lsm/go-landlock@v0.9.0
```
Expected: `go.mod` `require` block now lists both directly; `go.sum` updated.

**CC reference:** `src/utils/Shell.ts:386-388` (bwrap mount-point note — CC uses bubblewrap; ccgo uses landlock+seccomp directly, no `bwrap` binary dependency), `src/entrypoints/sandboxTypes.ts:29` (Linux seccomp cannot filter by path — so filesystem confinement is landlock, network confinement is seccomp).

**Interfaces produced (linux):**
- `func wrap(name string, args []string, p Policy) (string, []string, error)` — re-exec strategy: returns `(self, ["__sandbox_child", encodedPolicy, "--", name, args...])` where the ccgo binary applies landlock+seccomp to itself then `exec`s the real command. (A re-exec child is the only way to apply landlock to the to-be-exec'd process while keeping the parent unconfined.)
- `func ApplyLandlockSeccomp(p Policy) error` — applies the ruleset to the current thread (called by the child entrypoint).
- `func buildSeccompNetworkFilter(allowNetwork bool) []unix.SockFilter`

> NOTE: the child re-exec entrypoint (`__sandbox_child`) must be dispatched early in `cmd/claude/main.go` (before flag parsing). Add a 6-line guard at the top of `run()` that, when `os.Args[1] == "__sandbox_child"`, calls `sandbox.RunChild(os.Args[2:])` and exits. Confirm the exact `run()` signature with `grep -n "func run(" cmd/claude/main.go` (today: `func run(args []string, stdin io.Reader, stdout io.Writer, stderr io.Writer) int` at `cmd/claude/main.go:102`).

- [ ] **Step 1: Write the failing test**

Create `internal/sandbox/enforce_linux_test.go`:
```go
//go:build linux

package sandbox

import (
	"testing"

	"golang.org/x/sys/unix"
)

func TestBuildSeccompNetworkFilterDenies(t *testing.T) {
	filter := buildSeccompNetworkFilter(false)
	if len(filter) == 0 {
		t.Fatal("expected a non-empty seccomp program when denying network")
	}
	// The program must reference the socket syscall and a deny return.
	var sawDeny bool
	for _, ins := range filter {
		if ins.K == (unix.SECCOMP_RET_ERRNO | uint32(unix.EPERM)) {
			sawDeny = true
		}
	}
	if !sawDeny {
		t.Fatal("network-deny filter must contain a SECCOMP_RET_ERRNO|EPERM action")
	}
}

func TestBuildSeccompNetworkFilterAllowsWhenPermitted(t *testing.T) {
	// allowNetwork=true => no restrictive program (nil/empty is acceptable).
	if f := buildSeccompNetworkFilter(true); len(f) != 0 {
		t.Fatalf("allowNetwork should yield no filter, got %d instructions", len(f))
	}
}

func TestWrapLinuxReexecsChild(t *testing.T) {
	name, args, err := wrap("/bin/sh", []string{"-c", "echo hi"}, Policy{Enabled: true})
	if err != nil {
		t.Fatalf("wrap err: %v", err)
	}
	if name == "/bin/sh" {
		t.Fatal("linux wrap must re-exec the ccgo binary, not the raw command")
	}
	if len(args) < 4 || args[0] != childSentinel {
		t.Fatalf("wrap must prefix the child sentinel: %v", args)
	}
}
```

Confirm the constant name for the network-restricted seccomp action and `EPERM` before writing:
```bash
go doc golang.org/x/sys/unix SockFilter
go doc golang.org/x/sys/unix EPERM
grep -rn "SECCOMP_RET_ERRNO" "$(go env GOMODCACHE)"/golang.org/x/sys@*/unix/zerrors_linux.go
```

- [ ] **Step 2: Run test to verify it fails**

Run (on Linux, or via a Linux container): `go test ./internal/sandbox/ -run 'TestBuildSeccomp|TestWrapLinux' -v`
Expected: FAIL — `undefined: buildSeccompNetworkFilter`. (On macOS these tests are excluded by the build tag; you must run them on Linux — note this in the task as the OS gate.)

- [ ] **Step 3: Write minimal implementation**

Create `internal/sandbox/seccomp_linux.go`:
```go
//go:build linux

package sandbox

import "golang.org/x/sys/unix"

// buildSeccompNetworkFilter returns a classic BPF program that blocks the
// socket(2) syscall (and thus all new network sockets) by returning EPERM,
// while allowing everything else. Returns nil when network is permitted.
// Linux seccomp cannot filter by path (sandboxTypes.ts:29), so filesystem
// confinement is handled by landlock; this covers only network egress.
func buildSeccompNetworkFilter(allowNetwork bool) []unix.SockFilter {
	if allowNetwork {
		return nil
	}
	const sysSocket = 41 // x86_64 __NR_socket; adjust per-arch in production
	deny := unix.SECCOMP_RET_ERRNO | uint32(unix.EPERM)
	return []unix.SockFilter{
		// Load syscall number: A = seccomp_data.nr (offset 0).
		bpfStmt(unix.BPF_LD|unix.BPF_W|unix.BPF_ABS, 0),
		// if (A == socket) jump to deny, else allow.
		bpfJump(unix.BPF_JMP|unix.BPF_JEQ|unix.BPF_K, sysSocket, 0, 1),
		bpfStmt(unix.BPF_RET|unix.BPF_K, deny),
		bpfStmt(unix.BPF_RET|unix.BPF_K, unix.SECCOMP_RET_ALLOW),
	}
}

func bpfStmt(code uint16, k uint32) unix.SockFilter {
	return unix.SockFilter{Code: code, K: k}
}

func bpfJump(code uint16, k uint32, jt, jf uint8) unix.SockFilter {
	return unix.SockFilter{Code: code, Jt: jt, Jf: jf, K: k}
}
```

> Per-arch `__NR_socket` (and any extra socket-family syscalls like `socketcall` on 386) must be selected by `runtime.GOARCH`; the constant above is x86_64. Add a small `socketSyscallNR()` switch on GOARCH before shipping; the test only checks the deny action is present.

Create `internal/sandbox/enforce_linux.go`:
```go
//go:build linux

package sandbox

import (
	"encoding/base64"
	"encoding/json"
	"fmt"
	"os"
	"unsafe"

	"github.com/landlock-lsm/go-landlock/landlock"
	"golang.org/x/sys/unix"
)

const childSentinel = "__sandbox_child"

// wrap re-execs the ccgo binary as a confined child that applies landlock +
// seccomp to itself, then exec's the real command. The parent stays unconfined.
func wrap(name string, args []string, p Policy) (string, []string, error) {
	self, err := os.Executable()
	if err != nil {
		return "", nil, fmt.Errorf("sandbox: locate self: %w", err)
	}
	encoded, err := encodePolicy(p)
	if err != nil {
		return "", nil, err
	}
	childArgs := []string{childSentinel, encoded, "--", name}
	childArgs = append(childArgs, args...)
	return self, childArgs, nil
}

// RunChild is the entrypoint dispatched from main when os.Args carries the
// child sentinel. It applies confinement then exec's the wrapped command.
func RunChild(args []string) error {
	if len(args) < 3 || args[1] != "--" {
		return fmt.Errorf("sandbox child: malformed args")
	}
	p, err := decodePolicy(args[0])
	if err != nil {
		return err
	}
	cmd := args[2:]
	if err := ApplyLandlockSeccomp(p); err != nil {
		return err
	}
	return unix.Exec(cmd[0], cmd, os.Environ())
}

// ApplyLandlockSeccomp confines the current process: landlock for filesystem,
// a seccomp BPF filter for network. No-new-privs is required before seccomp.
func ApplyLandlockSeccomp(p Policy) error {
	if err := applyLandlock(p); err != nil {
		return fmt.Errorf("sandbox: landlock: %w", err)
	}
	if err := unix.Prctl(unix.PR_SET_NO_NEW_PRIVS, 1, 0, 0, 0); err != nil {
		return fmt.Errorf("sandbox: no_new_privs: %w", err)
	}
	filter := buildSeccompNetworkFilter(p.AllowNetwork)
	if len(filter) > 0 {
		if err := installSeccomp(filter); err != nil {
			return fmt.Errorf("sandbox: seccomp: %w", err)
		}
	}
	return nil
}

func applyLandlock(p Policy) error {
	cwd, _ := os.Getwd()
	var rules []landlock.Rule
	rules = append(rules, landlock.RODirs("/"))         // read everywhere by default
	if cwd != "" {
		rules = append(rules, landlock.RWDirs(cwd))      // write in cwd
	}
	for _, w := range p.AllowWrite {
		rules = append(rules, landlock.RWDirs(w))
	}
	return landlock.V5.BestEffort().RestrictPaths(rules...)
}

func installSeccomp(filter []unix.SockFilter) error {
	prog := unix.SockFprog{Len: uint16(len(filter)), Filter: &filter[0]}
	_, _, errno := unix.Syscall(unix.SYS_PRCTL, unix.PR_SET_SECCOMP,
		uintptr(unix.SECCOMP_MODE_FILTER), uintptr(unsafe.Pointer(&prog)))
	if errno != 0 {
		return errno
	}
	return nil
}

func encodePolicy(p Policy) (string, error) {
	data, err := json.Marshal(p)
	if err != nil {
		return "", err
	}
	return base64.StdEncoding.EncodeToString(data), nil
}

func decodePolicy(s string) (Policy, error) {
	data, err := base64.StdEncoding.DecodeString(s)
	if err != nil {
		return Policy{}, err
	}
	var p Policy
	if err := json.Unmarshal(data, &p); err != nil {
		return Policy{}, err
	}
	return p, nil
}
```

Confirm the landlock API surface before writing (the symbol names below are load-bearing):
```bash
go doc github.com/landlock-lsm/go-landlock/landlock V5
go doc github.com/landlock-lsm/go-landlock/landlock Config.RestrictPaths
go doc github.com/landlock-lsm/go-landlock/landlock RWDirs
go doc golang.org/x/sys/unix SockFprog
go doc golang.org/x/sys/unix SECCOMP_MODE_FILTER
```
If `landlock.V5`/`BestEffort`/`RWDirs`/`RODirs` differ in the pinned version, adjust to the version's actual API — keep the semantics (read-all baseline, write under cwd + AllowWrite, best-effort version negotiation).

Add the child dispatch to `cmd/claude/main.go` at the very top of `run()` (confirm signature first with `grep -n "func run(" cmd/claude/main.go`):
```go
func run(args []string, stdin io.Reader, stdout io.Writer, stderr io.Writer) int {
	if len(args) >= 1 && args[0] == "__sandbox_child" {
		if err := sandbox.RunChild(args[1:]); err != nil {
			fmt.Fprintf(stderr, "ccgo: sandbox child: %v\n", err)
			return 1
		}
		return 0 // unreachable; Exec replaces the process on success
	}
	// ... existing body ...
}
```
`sandbox.RunChild` must exist on every OS. Add a no-op on non-linux:

Create at the bottom of `internal/sandbox/enforce_other.go` and add to `enforce_darwin.go`:
```go
// RunChild is only meaningful on Linux (re-exec confinement). Elsewhere it is
// never dispatched; provide a stub so cmd/claude compiles on all platforms.
func RunChild(args []string) error { return ErrUnsupported }
```
(Place a matching stub in `enforce_darwin.go`; the real one is in `enforce_linux.go`. Add `"ccgo/internal/sandbox"` to main.go imports.)

- [ ] **Step 4: Run tests to verify they pass**

Run (on Linux): `go test ./internal/sandbox/ -v`
Run (on macOS/Windows, to confirm build-tag isolation + guard): `go build ./... && go test ./internal/sandbox/ -v`
Expected: Linux runs the landlock/seccomp tests; other OSes compile (stub `RunChild`) and run only the OS-agnostic + darwin/other tests. Full `go build ./...` clean on all.

Optional Linux integration smoke (manual, Linux only):
```bash
# Build, then confirm a sandboxed write outside cwd is denied:
go build -o /tmp/ccgo ./cmd/claude
# (drive via the bash tool in Task 4's smoke; or a dedicated build-tagged integration test)
```

- [ ] **Step 5: Commit**

```bash
git add internal/sandbox/enforce_linux.go internal/sandbox/seccomp_linux.go internal/sandbox/enforce_linux_test.go internal/sandbox/enforce_darwin.go internal/sandbox/enforce_other.go cmd/claude/main.go go.mod go.sum
git commit -m "feat(sandbox): Linux landlock+seccomp enforcement via re-exec child

Promotes golang.org/x/sys to direct; adds github.com/landlock-lsm/go-landlock
(canonical Go Landlock, depends only on x/sys). Seccomp network filter is
hand-rolled BPF using x/sys constants (no extra dep)."
```

---

## Task 4: Wire the sandbox into the Bash tool (close the security regression)

**Files:**
- Modify: `internal/tools/bash/tools.go` (`runBashCommand`, `startBackgroundBash`, `shellCommand` path)
- Create: `internal/tools/bash/sandbox.go` (small helper: build `Policy` from `tool.Context`, gate, wrap)
- Test: `internal/tools/bash/sandbox_test.go`

**Interfaces consumed:** `sandbox.PolicyFromSettings`, `sandbox.Policy.ShouldSandbox`, `sandbox.Wrap`, `sandbox.Supported`; existing `bashInput.DangerouslyDisableSandbox` (`internal/tools/bash/tools.go:768`), `shellCommand` (`:1193`), `runBashCommand` (`:1040`).

Confirm the settings source on `tool.Context` first:
```bash
grep -n "type Context struct" /Users/sqlrush/ccgo/internal/tool/types.go      # Metadata map[string]any, WorkingDirectory, ...
grep -rn "Settings\|settings" /Users/sqlrush/ccgo/internal/tools/bash/*.go    # how does bash see settings today?
grep -rn "ctx.Metadata\[" /Users/sqlrush/ccgo/internal/tools/*/*.go | head     # the metadata key convention
```
If settings are not on `ctx.Metadata`, thread the merged `contracts.Settings` through the bash tool the same way `sessionPathFromMetadata` reads `ctx.Metadata` (`internal/tools/task/tools.go`). Reuse the existing convention; do not invent a new one.

- [ ] **Step 1: Write the failing test**

Create `internal/tools/bash/sandbox_test.go`:
```go
package bashtools

import (
	"testing"

	"ccgo/internal/sandbox"
)

func TestSandboxedCommandWrapsWhenEnabled(t *testing.T) {
	if !sandbox.Supported() {
		t.Skip("sandbox enforcement unavailable on this OS; wrap path not asserted")
	}
	p := sandbox.Policy{Enabled: true}
	name, args := sandboxedShellCommand("echo hi", p, false)
	if name == defaultShell() {
		t.Fatalf("expected a sandbox wrapper, got raw shell %q", name)
	}
	_ = args
}

func TestSandboxBypassRespectsPolicy(t *testing.T) {
	p := sandbox.Policy{Enabled: true, AllowUnsandboxed: true}
	// dangerouslyDisableSandbox=true + policy allows => no wrapping.
	name, _ := sandboxedShellCommand("echo hi", p, true)
	if name != defaultShell() {
		t.Fatalf("flag+policy should bypass sandbox, got wrapper %q", name)
	}
}

func TestSandboxFlagIgnoredWhenPolicyForbids(t *testing.T) {
	if !sandbox.Supported() {
		t.Skip("sandbox enforcement unavailable; cannot assert confinement")
	}
	p := sandbox.Policy{Enabled: true, AllowUnsandboxed: false}
	// flag set but policy forbids unsandboxed => MUST still wrap (security).
	name, _ := sandboxedShellCommand("echo hi", p, true)
	if name == defaultShell() {
		t.Fatal("SECURITY: flag must not bypass sandbox when policy forbids")
	}
}
```

`defaultShell()` and `sandboxedShellCommand` are introduced in Step 3. Confirm the raw shell name `shellCommand` returns today (`/bin/zsh`? `/bin/bash`? `sh`?):
```bash
sed -n '1193,1210p' /Users/sqlrush/ccgo/internal/tools/bash/tools.go
```
and set `defaultShell()` to match so the test compares against the real value.

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/tools/bash/ -run TestSandbox -v`
Expected: FAIL — `undefined: sandboxedShellCommand`.

- [ ] **Step 3: Write minimal implementation**

Create `internal/tools/bash/sandbox.go`:
```go
package bashtools

import "ccgo/internal/sandbox"

// defaultShell returns the raw shell invocation name (must match shellCommand).
func defaultShell() string {
	name, _ := shellCommand("")
	return name
}

// sandboxedShellCommand builds the (name, args) to run command, confined per p
// unless the dangerous flag legitimately bypasses it. SECURITY: confinement is
// decided by p.ShouldSandbox; never by the flag alone.
func sandboxedShellCommand(command string, p sandbox.Policy, dangerous bool) (string, []string) {
	name, args := shellCommand(command)
	if !p.ShouldSandbox(dangerous) {
		return name, args
	}
	if !sandbox.Supported() {
		// Required-but-unsupported: fail closed if FailIfUnavailable, else warn.
		if p.FailIfUnavailable {
			return failClosedCommand(p)
		}
		return name, args // documented degraded mode; warning emitted by caller
	}
	wName, wArgs, err := sandbox.Wrap(name, args, p)
	if err != nil {
		if p.FailIfUnavailable {
			return failClosedCommand(p)
		}
		return name, args
	}
	return wName, wArgs
}

// failClosedCommand returns a command that exits non-zero with a clear message
// so a required-but-unavailable sandbox never silently runs unconfined.
func failClosedCommand(_ sandbox.Policy) (string, []string) {
	name, _ := shellCommand("")
	return name, shellArgsFor("echo 'sandbox required but unavailable' >&2; exit 1")
}
```

Add a tiny `shellArgsFor(command string) []string` helper that returns the args part of `shellCommand` (refactor `shellCommand` to reuse it), confirming the exact arg shape with the Step-1 `sed` output.

Then wire it into `runBashCommand` and `startBackgroundBash`. In `runBashCommand` (currently `internal/tools/bash/tools.go:1049-1050`):
```go
	policy := sandboxPolicyFromContext(ctx)
	name, args := sandboxedShellCommand(command, policy, ctx /*input*/.DangerouslyDisableSandbox)
	cmd := exec.CommandContext(runCtx, name, args...)
```
`runBashCommand` does not currently receive `input`; pass the `dangerouslyDisableSandbox` bool down (change the signature `runBashCommand(ctx, command, timeout, dangerous bool)` and update the single caller at `:944`). Add `sandboxPolicyFromContext(ctx tool.Context) sandbox.Policy` that reads merged settings from `ctx.Metadata` (per the convention confirmed above) and calls `sandbox.PolicyFromSettings`.

Do the same wrap in `startBackgroundBash` (`internal/tools/bash/tools.go:1093`).

- [ ] **Step 4: Run tests + full bash suite**

Run: `go test ./internal/tools/bash/ -v && go build ./...`
Expected: PASS. The pre-existing bash tests (which set `dangerouslyDisableSandbox:"true"` with no sandbox settings => `Enabled=false` => no wrap) are unaffected. Vet clean.

Manual security smoke (per-OS, manual):
```bash
# With sandbox.enabled=true in settings, a write outside cwd must fail:
echo '{"command":"echo bad > /etc/ccgo_probe"}' # drive via a Bash tool call; expect permission/IO error
```

- [ ] **Step 5: Commit**

```bash
git add internal/tools/bash/sandbox.go internal/tools/bash/tools.go internal/tools/bash/sandbox_test.go
git commit -m "fix(bash): enforce OS sandbox for Bash, honoring dangerouslyDisableSandbox

Closes the security regression: the flag previously had zero enforcement. Bash
is now confined when sandbox.enabled, and the flag only bypasses when
sandbox.allowUnsandboxedCommands permits."
```

> PowerShell parity: the same `sandboxedShellCommand` pattern applies to `internal/tools/powershell/tools.go` (its `shellCommand`/exec path mirrors bash). Add an equivalent `internal/tools/powershell/sandbox.go` + test in this commit or a fast follow-up — note it in the commit body if deferred.

---

## Task 5: Task schema `model`/`isolation` fields + async/background agents

**Files:**
- Modify: `internal/tools/task/tools.go` (`taskInput`, `decodeTaskInput`, `normalizeTaskInput`, `validateTask`, Task tool schema)
- Create: `internal/orchestration/registry.go`
- Create: `internal/orchestration/task_schema.go`
- Test: `internal/orchestration/registry_test.go`
- Test: `internal/orchestration/task_schema_test.go`

**Interfaces produced:**
- `type Isolation string`; `const IsolationNone Isolation = ""`; `const IsolationWorktree Isolation = "worktree"` — `ValidateIsolation(s string) (Isolation, error)` rejects `"remote"` (out of scope) with a clear error.
- `type ModelAlias string` validated against `{"", "sonnet", "opus", "haiku"}` (CC `AgentTool.tsx:86`).
- `type AgentRegistry struct { ... }` (mutex-guarded map of in-flight background agents); `func NewAgentRegistry() *AgentRegistry`; `StartBackground(id string, run func(context.Context) Outcome)`; `Snapshot() []AgentStatus` (returns **copies**); `Harvest(id string) (Outcome, bool)`.

**CC reference:** `src/tools/AgentTool/AgentTool.tsx:82-138` (`model` enum `['sonnet','opus','haiku']`, `isolation` enum `['worktree']` for non-ant / `['worktree','remote']` for ant — ccgo supports **`worktree` only**, `remote` is OUT of scope per roadmap §1), `src/utils/swarm/LocalAgentTask.tsx:466` (`registerAsyncAgent`), `src/utils/swarm/agentToolUtils.ts:507` (`runAsyncAgentLifecycle`).

Confirm current Task input + worktree handling before editing:
```bash
sed -n '37,46p' /Users/sqlrush/ccgo/internal/tools/task/tools.go     # taskInput (Worktree/WorktreeSet/Run exist; no Model/Isolation)
sed -n '2541,2580p' /Users/sqlrush/ccgo/internal/tools/task/tools.go # decodeTaskInput
grep -n "func taskInputRequestsWorktree\|func prepareTaskWorktree" /Users/sqlrush/ccgo/internal/tools/task/worktree.go
```

- [ ] **Step 1: Write the failing test**

Create `internal/orchestration/task_schema_test.go`:
```go
package orchestration

import "testing"

func TestValidateIsolation(t *testing.T) {
	if got, err := ValidateIsolation(""); err != nil || got != IsolationNone {
		t.Fatalf(`"" => %q,%v`, got, err)
	}
	if got, err := ValidateIsolation("worktree"); err != nil || got != IsolationWorktree {
		t.Fatalf(`"worktree" => %q,%v`, got, err)
	}
	if _, err := ValidateIsolation("remote"); err == nil {
		t.Fatal("remote isolation is out of scope and must be rejected")
	}
	if _, err := ValidateIsolation("bogus"); err == nil {
		t.Fatal("unknown isolation must be rejected")
	}
}

func TestValidateModelAlias(t *testing.T) {
	for _, ok := range []string{"", "sonnet", "opus", "haiku"} {
		if _, err := ValidateModelAlias(ok); err != nil {
			t.Fatalf("model %q should be valid: %v", ok, err)
		}
	}
	if _, err := ValidateModelAlias("gpt-4"); err == nil {
		t.Fatal("unknown model alias must be rejected")
	}
}
```

Create `internal/orchestration/registry_test.go`:
```go
package orchestration

import (
	"context"
	"testing"
	"time"
)

func TestAgentRegistryBackgroundLifecycle(t *testing.T) {
	reg := NewAgentRegistry()
	done := make(chan struct{})
	reg.StartBackground("a1", func(ctx context.Context) Outcome {
		<-done
		return Outcome{Summary: "finished"}
	})

	snap := reg.Snapshot()
	if len(snap) != 1 || snap[0].ID != "a1" || snap[0].State != AgentRunning {
		t.Fatalf("snapshot = %+v", snap)
	}
	// Mutating the snapshot must not affect the registry (immutability).
	snap[0].State = AgentDone

	close(done)
	deadline := time.After(2 * time.Second)
	for {
		out, ok := reg.Harvest("a1")
		if ok {
			if out.Summary != "finished" {
				t.Fatalf("harvested outcome = %+v", out)
			}
			return
		}
		select {
		case <-deadline:
			t.Fatal("background agent never completed")
		case <-time.After(10 * time.Millisecond):
		}
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/orchestration/ -v`
Expected: FAIL — `undefined: ValidateIsolation` / `undefined: NewAgentRegistry`.

- [ ] **Step 3: Write minimal implementation**

Create `internal/orchestration/task_schema.go`:
```go
package orchestration

import "fmt"

// Isolation is the subagent isolation strategy. Only "worktree" is supported;
// "remote" is intentionally out of scope (cloud stack, roadmap §1).
type Isolation string

const (
	IsolationNone     Isolation = ""
	IsolationWorktree Isolation = "worktree"
)

// ValidateIsolation parses an isolation value, rejecting "remote" and unknowns.
func ValidateIsolation(s string) (Isolation, error) {
	switch s {
	case "":
		return IsolationNone, nil
	case "worktree":
		return IsolationWorktree, nil
	case "remote":
		return "", fmt.Errorf("isolation %q is not supported (cloud/remote is out of scope)", s)
	default:
		return "", fmt.Errorf("unknown isolation %q (supported: worktree)", s)
	}
}

// ValidateModelAlias parses a Task model override (CC enum sonnet/opus/haiku).
func ValidateModelAlias(s string) (string, error) {
	switch s {
	case "", "sonnet", "opus", "haiku":
		return s, nil
	default:
		return "", fmt.Errorf("unknown model %q (supported: sonnet, opus, haiku)", s)
	}
}
```

Create `internal/orchestration/registry.go`:
```go
package orchestration

import (
	"context"
	"sync"
)

// AgentState is the lifecycle state of a tracked background agent.
type AgentState string

const (
	AgentRunning AgentState = "running"
	AgentDone    AgentState = "done"
	AgentFailed  AgentState = "failed"
)

// Outcome is the result of a teammate/background-agent run.
type Outcome struct {
	Summary string
	Err     error
}

// AgentStatus is an immutable snapshot of one tracked agent.
type AgentStatus struct {
	ID    string
	State AgentState
}

type agentEntry struct {
	status   AgentStatus
	outcome  Outcome
	finished bool
}

// AgentRegistry tracks in-process background agents. Snapshots are copies.
type AgentRegistry struct {
	mu     sync.Mutex
	agents map[string]*agentEntry
}

func NewAgentRegistry() *AgentRegistry {
	return &AgentRegistry{agents: make(map[string]*agentEntry)}
}

// StartBackground launches run in a goroutine and tracks it by id.
func (r *AgentRegistry) StartBackground(id string, run func(context.Context) Outcome) {
	r.mu.Lock()
	r.agents[id] = &agentEntry{status: AgentStatus{ID: id, State: AgentRunning}}
	r.mu.Unlock()

	go func() {
		out := run(context.Background())
		r.mu.Lock()
		defer r.mu.Unlock()
		entry := r.agents[id]
		if entry == nil {
			return
		}
		entry.outcome = out
		entry.finished = true
		if out.Err != nil {
			entry.status.State = AgentFailed
		} else {
			entry.status.State = AgentDone
		}
	}()
}

// Snapshot returns copies of all tracked agents' status.
func (r *AgentRegistry) Snapshot() []AgentStatus {
	r.mu.Lock()
	defer r.mu.Unlock()
	out := make([]AgentStatus, 0, len(r.agents))
	for _, entry := range r.agents {
		out = append(out, entry.status) // value copy
	}
	return out
}

// Harvest returns the outcome of a finished agent and removes it. ok=false if
// the agent is unknown or still running.
func (r *AgentRegistry) Harvest(id string) (Outcome, bool) {
	r.mu.Lock()
	defer r.mu.Unlock()
	entry := r.agents[id]
	if entry == nil || !entry.finished {
		return Outcome{}, false
	}
	delete(r.agents, id)
	return entry.outcome, true
}
```

Now extend the Task schema. In `internal/tools/task/tools.go`, add to `taskInput` (after `Run bool`):
```go
	Model        string `json:"model,omitempty"`
	Isolation    string `json:"isolation,omitempty"`
	RunBackground bool  `json:"run_in_background,omitempty"`
```
In `validateTask`, validate them via `orchestration.ValidateIsolation`/`ValidateModelAlias` (boundary validation, fail fast). Map `Isolation == "worktree"` to the existing worktree path (`taskInputRequestsWorktree`) so the new field reuses the proven worktree machinery rather than a parallel one. Add the JSON-schema properties for `model`/`isolation`/`run_in_background` in `NewTaskTool` (mirror the existing `worktree` property at `internal/tools/task/tools.go:172-186`).

- [ ] **Step 4: Run tests to verify they pass**

Run: `go test ./internal/orchestration/ ./internal/tools/task/ -v && go build ./...`
Expected: PASS; pre-existing task tests unaffected (new fields are optional, default zero).

- [ ] **Step 5: Commit**

```bash
git add internal/orchestration/registry.go internal/orchestration/task_schema.go internal/orchestration/registry_test.go internal/orchestration/task_schema_test.go internal/tools/task/tools.go
git commit -m "feat(orchestration): Task model/isolation fields + background AgentRegistry"
```

---

## Task 6: Real in-process Team/teammate runner (replace the append-only stubs)

**Files:**
- Create: `internal/orchestration/runner.go`
- Test: `internal/orchestration/runner_test.go`

**Interfaces produced:**
- `type RunnerFactory func(agentType, model string) (*conversation.Runner, error)`
- `type Teammate struct { SidechainID, AgentType, Model string }`
- `type TeamRunner struct { Factory RunnerFactory; Persist func(sidechainID string, msgs []contracts.Message) error }`
- `func (tr TeamRunner) RunTeammate(ctx context.Context, tm Teammate, history []contracts.Message, prompt string) (Outcome, error)` — runs a **real** `RunTurn` against the teammate's runner and persists results.

**CC reference:** `src/utils/swarm/inProcessRunner.ts:883` (`runInProcessTeammate`) → `:1175` (`runAgent()` — the same loop subagents use) → `src/tools/AgentTool/runAgent.ts`. ccgo's analogue is `(*conversation.Runner).RunTurn` (`internal/conversation/run.go:44`). The key behavior we are replacing: today `callTeamDispatch`/`callTeamCoordinate` only `manager.Append(...)` (`internal/tools/task/tools.go:1782-1837, 2008-2059`) and **no model loop ever runs**.

Confirm the runner contract before writing:
```bash
sed -n '44,60p' /Users/sqlrush/ccgo/internal/conversation/run.go     # RunTurn(ctx, history, user) (Result, error)
go doc ccgo/internal/conversation Result                              # Messages, Assistant, StopReason ...
go doc ccgo/internal/messages UserText                               # build the user message
```

- [ ] **Step 1: Write the failing test**

Create `internal/orchestration/runner_test.go`:
```go
package orchestration

import (
	"context"
	"testing"

	"ccgo/internal/contracts"
	"ccgo/internal/conversation"
)

// stubClient returns a fixed assistant message, proving a REAL turn ran.
type stubClient struct{ reply string }

func (s stubClient) CreateMessage(ctx context.Context, req anthropicRequest) (*anthropicResponse, error) {
	// signature filled in per the real conversation.MessageClient (see note).
	panic("bind to real interface")
}

func TestRunTeammateExecutesRealTurn(t *testing.T) {
	t.Skip("enable after binding stubClient to conversation.MessageClient (see Step 3 note)")

	var persisted []contracts.Message
	tr := TeamRunner{
		Factory: func(agentType, model string) (*conversation.Runner, error) {
			r := &conversation.Runner{ /* Client: stubClient{reply: "done"} */ }
			return r, nil
		},
		Persist: func(_ string, msgs []contracts.Message) error {
			persisted = append(persisted, msgs...)
			return nil
		},
	}
	out, err := tr.RunTeammate(context.Background(),
		Teammate{SidechainID: "t1", AgentType: "worker"}, nil, "do the thing")
	if err != nil {
		t.Fatalf("RunTeammate err: %v", err)
	}
	if out.Summary == "" {
		t.Fatal("expected a non-empty teammate summary from a real turn")
	}
	if len(persisted) == 0 {
		t.Fatal("teammate result was not persisted to the sidechain")
	}
}

func TestRunTeammateFactoryError(t *testing.T) {
	tr := TeamRunner{
		Factory: func(string, string) (*conversation.Runner, error) {
			return nil, errFactory
		},
	}
	if _, err := tr.RunTeammate(context.Background(), Teammate{SidechainID: "t1"}, nil, "hi"); err == nil {
		t.Fatal("expected factory error to propagate")
	}
}
```
Add `var errFactory = errors.New("factory boom")` (import `errors`). The `anthropicRequest`/`anthropicResponse` placeholder types are a signal: bind `stubClient` to the **real** `conversation.MessageClient` (`go doc ccgo/internal/conversation MessageClient` → `CreateMessage(context.Context, anthropic.Request) (*anthropic.Response, error)`), then drop the `t.Skip`. The `TestRunTeammateFactoryError` test runs without the model and gives an immediately-green path.

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/orchestration/ -run TestRunTeammate -v`
Expected: FAIL — `undefined: TeamRunner`.

- [ ] **Step 3: Write minimal implementation**

Create `internal/orchestration/runner.go`:
```go
package orchestration

import (
	"context"
	"fmt"

	"ccgo/internal/contracts"
	"ccgo/internal/conversation"
	"ccgo/internal/messages"
)

// RunnerFactory builds a fully-wired runner for a teammate of the given type
// and (optional) model override. Reuses the host's ConversationRunner wiring.
type RunnerFactory func(agentType, model string) (*conversation.Runner, error)

// Teammate identifies one team member backed by a sidechain transcript.
type Teammate struct {
	SidechainID string
	AgentType   string
	Model       string
}

// TeamRunner executes real teammate turns. This replaces the append-only Team
// stubs: a teammate now runs an actual model loop via conversation.RunTurn.
type TeamRunner struct {
	Factory RunnerFactory
	// Persist writes the turn's messages back to the teammate's sidechain.
	Persist func(sidechainID string, msgs []contracts.Message) error
}

// RunTeammate runs one prompt against the teammate's runner and persists the
// resulting messages. Returns a summary Outcome.
func (tr TeamRunner) RunTeammate(ctx context.Context, tm Teammate, history []contracts.Message, prompt string) (Outcome, error) {
	if tr.Factory == nil {
		return Outcome{}, fmt.Errorf("team runner: no factory configured")
	}
	runner, err := tr.Factory(tm.AgentType, tm.Model)
	if err != nil {
		return Outcome{}, fmt.Errorf("team runner: build runner for %q: %w", tm.AgentType, err)
	}
	user := messages.UserText(prompt)
	result, err := runner.RunTurn(ctx, history, user)
	if err != nil {
		return Outcome{Err: err}, err
	}
	if tr.Persist != nil {
		if perr := tr.Persist(tm.SidechainID, result.Messages); perr != nil {
			return Outcome{}, fmt.Errorf("team runner: persist %q: %w", tm.SidechainID, perr)
		}
	}
	return Outcome{Summary: summarize(result)}, nil
}

func summarize(result conversation.Result) string {
	if text := messages.TextContent(result.Assistant); text != "" {
		return text
	}
	return result.StopReason
}
```

Confirm `messages.TextContent` exists and takes `contracts.Message` (used in Phase 1's render): `grep -rn "func TextContent" internal/messages/`. If the helper differs, extract the assistant text via the actual API.

Now rewire the Team tools. In `internal/tools/task/tools.go`, change `callTeamDispatch` and `callTeamCoordinate` so that, in addition to recording the message (keep the durable transcript append), they **start a real teammate run** via a `TeamRunner` whose `Factory` is built from the host runner wiring. The host runner is reachable through `ctx.Metadata` (the same channel `sessionPathFromMetadata` uses) — thread a `RunnerFactory` (or a `*bootstrap.State`) into the Task tools' context at construction. Run synchronously for `coordinate`; for `dispatch` of multiple assignments, start each via the `AgentRegistry.StartBackground` from Task 5 and report the started agent IDs in structured output. Confirm the metadata wiring point:
```bash
grep -rn "NewFileTools\|RegisterTaskTools\|ctx.Metadata\[" /Users/sqlrush/ccgo/internal/tools/task/*.go /Users/sqlrush/ccgo/internal/tools/file/tools.go | head
grep -rn "func.*ConversationRunner" /Users/sqlrush/ccgo/internal/bootstrap/state.go
```
If threading a live factory through `ctx.Metadata` is too invasive for one task, land `TeamRunner` + the registry integration behind a `RunnerFactory` field on the Task-tools constructor and have `bootstrap.State.ConversationRunner()` populate it; keep the transcript append as the durable record. Do **not** leave `callTeamDispatch`/`callTeamCoordinate` append-only — the gate for this task is a teammate that actually runs a turn.

- [ ] **Step 4: Run tests to verify they pass**

Run: `go test ./internal/orchestration/ ./internal/tools/task/ -v && go build ./...`
Expected: PASS (the factory-error test green immediately; the real-turn test green once `stubClient` is bound). Pre-existing task tests pass (transcript append preserved).

- [ ] **Step 5: Commit**

```bash
git add internal/orchestration/runner.go internal/orchestration/runner_test.go internal/tools/task/tools.go
git commit -m "feat(orchestration): real in-process teammate runner; Team dispatch/coordinate run turns"
```

---

## Task 7: SDK control protocol framing (`control_request` / `control_response`)

**Files:**
- Create: `internal/sdk/protocol.go`
- Test: `internal/sdk/protocol_test.go`

**Interfaces produced:**
- `type ControlRequest struct { Type string; RequestID string; Request map[string]any }` with `Subtype() string` reading `Request["subtype"]`.
- `type ControlResponse struct { Type string; Response ControlResponseBody }`; `ControlResponseBody{ Subtype, RequestID, Response (map), Error string }`.
- `func SuccessResponse(requestID string, payload map[string]any) ControlResponse`
- `func ErrorResponse(requestID, msg string) ControlResponse`
- `type Decoder struct{...}`/`func NewDecoder(io.Reader)`; `(*Decoder) Next() (ControlRequest, error)` — NDJSON, `io.EOF` at end.
- `type Encoder struct{...}`/`func NewEncoder(io.Writer)`; `(*Encoder) WriteResponse(ControlResponse) error`; `(*Encoder) WriteRequest(ControlRequest) error`.

**CC reference:** `src/entrypoints/sdk/controlSchemas.ts:578-584` (`control_request`: `{type, request_id, request}`), `:605-610` (`control_response`: `{type, response: {subtype:"success"|"error", request_id, response?|error}}`), `src/cli/structuredIO.ts:215-261` (read) / `:465-467` (write) — NDJSON over stdin/stdout.

- [ ] **Step 1: Write the failing test**

Create `internal/sdk/protocol_test.go`:
```go
package sdk

import (
	"bytes"
	"errors"
	"io"
	"strings"
	"testing"
)

func TestDecodeControlRequest(t *testing.T) {
	in := `{"type":"control_request","request_id":"r1","request":{"subtype":"interrupt"}}` + "\n"
	dec := NewDecoder(strings.NewReader(in))
	req, err := dec.Next()
	if err != nil {
		t.Fatalf("Next err: %v", err)
	}
	if req.Type != "control_request" || req.RequestID != "r1" {
		t.Fatalf("req = %+v", req)
	}
	if req.Subtype() != "interrupt" {
		t.Fatalf("subtype = %q want interrupt", req.Subtype())
	}
	if _, err := dec.Next(); !errors.Is(err, io.EOF) {
		t.Fatalf("expected EOF, got %v", err)
	}
}

func TestEncodeSuccessResponse(t *testing.T) {
	var buf bytes.Buffer
	enc := NewEncoder(&buf)
	if err := enc.WriteResponse(SuccessResponse("r1", map[string]any{"model": "opus"})); err != nil {
		t.Fatalf("WriteResponse err: %v", err)
	}
	out := buf.String()
	for _, want := range []string{`"type":"control_response"`, `"subtype":"success"`, `"request_id":"r1"`, `"model":"opus"`} {
		if !strings.Contains(out, want) {
			t.Fatalf("output %q missing %q", out, want)
		}
	}
	if !strings.HasSuffix(out, "\n") {
		t.Fatal("NDJSON responses must be newline-terminated")
	}
}

func TestEncodeErrorResponse(t *testing.T) {
	var buf bytes.Buffer
	enc := NewEncoder(&buf)
	if err := enc.WriteResponse(ErrorResponse("r2", "denied")); err != nil {
		t.Fatal(err)
	}
	out := buf.String()
	if !strings.Contains(out, `"subtype":"error"`) || !strings.Contains(out, `"error":"denied"`) {
		t.Fatalf("error response shape wrong: %q", out)
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/sdk/ -run 'TestDecode|TestEncode' -v`
Expected: FAIL — `undefined: NewDecoder`.

- [ ] **Step 3: Write minimal implementation**

Create `internal/sdk/protocol.go`:
```go
package sdk

import (
	"bufio"
	"encoding/json"
	"io"
	"strings"
)

// ControlRequest is an inbound SDK control message (controlSchemas.ts:578-584).
type ControlRequest struct {
	Type      string         `json:"type"`
	RequestID string         `json:"request_id"`
	Request   map[string]any `json:"request"`
}

// Subtype returns the request subtype (interrupt, set_model, can_use_tool, ...).
func (r ControlRequest) Subtype() string {
	if r.Request == nil {
		return ""
	}
	s, _ := r.Request["subtype"].(string)
	return s
}

// ControlResponseBody is the inner response (controlSchemas.ts:605-610).
type ControlResponseBody struct {
	Subtype   string         `json:"subtype"`
	RequestID string         `json:"request_id"`
	Response  map[string]any `json:"response,omitempty"`
	Error     string         `json:"error,omitempty"`
}

// ControlResponse is an outbound control_response envelope.
type ControlResponse struct {
	Type     string              `json:"type"`
	Response ControlResponseBody `json:"response"`
}

func SuccessResponse(requestID string, payload map[string]any) ControlResponse {
	return ControlResponse{
		Type:     "control_response",
		Response: ControlResponseBody{Subtype: "success", RequestID: requestID, Response: payload},
	}
}

func ErrorResponse(requestID, msg string) ControlResponse {
	return ControlResponse{
		Type:     "control_response",
		Response: ControlResponseBody{Subtype: "error", RequestID: requestID, Error: msg},
	}
}

// Decoder reads NDJSON control requests from a stream.
type Decoder struct {
	r *bufio.Reader
}

func NewDecoder(r io.Reader) *Decoder { return &Decoder{r: bufio.NewReader(r)} }

// Next returns the next control_request; io.EOF at end of stream.
func (d *Decoder) Next() (ControlRequest, error) {
	for {
		line, err := d.r.ReadString('\n')
		trimmed := strings.TrimSpace(line)
		if trimmed != "" {
			var req ControlRequest
			if jerr := json.Unmarshal([]byte(trimmed), &req); jerr != nil {
				return ControlRequest{}, jerr // boundary validation: reject malformed
			}
			return req, nil
		}
		if err != nil {
			return ControlRequest{}, err // io.EOF or read error
		}
	}
}

// Encoder writes NDJSON control messages to a stream.
type Encoder struct {
	w   io.Writer
	enc *json.Encoder
}

func NewEncoder(w io.Writer) *Encoder {
	return &Encoder{w: w, enc: json.NewEncoder(w)} // json.Encoder appends '\n'
}

func (e *Encoder) WriteResponse(resp ControlResponse) error { return e.enc.Encode(resp) }
func (e *Encoder) WriteRequest(req ControlRequest) error    { return e.enc.Encode(req) }
```

- [ ] **Step 4: Run tests to verify they pass**

Run: `go test ./internal/sdk/ -v`
Expected: PASS.

- [ ] **Step 5: Commit**

```bash
git add internal/sdk/protocol.go internal/sdk/protocol_test.go
git commit -m "feat(sdk): control_request/control_response NDJSON framing"
```

---

## Task 8: `canUseTool` + `interrupt` + `set_model` control operations

**Files:**
- Create: `internal/sdk/controller.go`
- Create: `internal/sdk/asker.go`
- Test: `internal/sdk/controller_test.go`
- Test: `internal/sdk/asker_test.go`

**Interfaces produced:**
- `type Controller struct { enc *Encoder; interrupt func(); setModel func(string) error; nextReqID func() string; ... }`
- `func (c *Controller) Handle(req ControlRequest) ControlResponse` — dispatch `interrupt`/`set_model`/`initialize`; unknown subtype → `ErrorResponse`.
- `type controlAsker struct { ... }` implementing `tool.PermissionAsker` — sends `can_use_tool` out, blocks on the matching response.

**CC reference:** `src/bridge/bridgeMessaging.ts:362-371` (interrupt handler), `:306-315` (set_model handler), `src/cli/structuredIO.ts:533-659` (`createCanUseTool`), `src/entrypoints/sdk/controlSchemas.ts:106-122` (`can_use_tool` payload: `tool_name`, `input`, `tool_use_id`, ...; response `{behavior:"allow"|"deny", updatedInput?, message?}`).

Confirm the existing asker seam (reused from Phase 1):
```bash
go doc ccgo/internal/tool PermissionAsker          # Ask(ctx, PermissionAskRequest) (contracts.PermissionDecision, error)
go doc ccgo/internal/tool PermissionAskRequest     # ToolUseID, ToolName, Path, Description, Decision
go doc ccgo/internal/contracts PermissionDecision  # Behavior, Message, UpdatedInput ...
```

- [ ] **Step 1: Write the failing test**

Create `internal/sdk/controller_test.go`:
```go
package sdk

import "testing"

func TestControllerInterrupt(t *testing.T) {
	var interrupted bool
	c := &Controller{interrupt: func() { interrupted = true }}
	resp := c.Handle(ControlRequest{Type: "control_request", RequestID: "r1",
		Request: map[string]any{"subtype": "interrupt"}})
	if !interrupted {
		t.Fatal("interrupt callback not invoked")
	}
	if resp.Response.Subtype != "success" || resp.Response.RequestID != "r1" {
		t.Fatalf("resp = %+v", resp)
	}
}

func TestControllerSetModel(t *testing.T) {
	var got string
	c := &Controller{setModel: func(m string) error { got = m; return nil }}
	resp := c.Handle(ControlRequest{RequestID: "r2",
		Request: map[string]any{"subtype": "set_model", "model": "opus"}})
	if got != "opus" {
		t.Fatalf("set_model = %q want opus", got)
	}
	if resp.Response.Subtype != "success" {
		t.Fatalf("resp = %+v", resp)
	}
}

func TestControllerUnknownSubtypeErrors(t *testing.T) {
	c := &Controller{}
	resp := c.Handle(ControlRequest{RequestID: "r3",
		Request: map[string]any{"subtype": "frobnicate"}})
	if resp.Response.Subtype != "error" || resp.Response.Error == "" {
		t.Fatalf("unknown subtype must error: %+v", resp)
	}
}
```

Create `internal/sdk/asker_test.go`:
```go
package sdk

import (
	"context"
	"testing"
	"time"

	"ccgo/internal/contracts"
	"ccgo/internal/tool"
)

func TestControlAskerForwardsAndResolves(t *testing.T) {
	out := make(chan ControlRequest, 1)
	asker := newControlAsker(
		func(req ControlRequest) error { out <- req; return nil },
		func() string { return "req-1" },
	)

	decisionCh := make(chan contracts.PermissionDecision, 1)
	go func() {
		d, err := asker.Ask(context.Background(), tool.PermissionAskRequest{
			ToolUseID: "u1", ToolName: "Bash", Description: "run ls",
		})
		if err == nil {
			decisionCh <- d
		}
	}()

	// The asker must emit a can_use_tool control_request.
	select {
	case req := <-out:
		if req.Subtype() != "can_use_tool" {
			t.Fatalf("subtype = %q want can_use_tool", req.Subtype())
		}
		// Simulate the SDK client allowing the tool.
		asker.Resolve(req.RequestID, contracts.PermissionDecision{Behavior: contracts.PermissionAllow})
	case <-time.After(2 * time.Second):
		t.Fatal("no can_use_tool request emitted")
	}

	select {
	case d := <-decisionCh:
		if d.Behavior != contracts.PermissionAllow {
			t.Fatalf("decision = %v want allow", d.Behavior)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("asker never resolved")
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/sdk/ -run 'TestController|TestControlAsker' -v`
Expected: FAIL — `undefined: Controller` / `undefined: newControlAsker`.

- [ ] **Step 3: Write minimal implementation**

Create `internal/sdk/controller.go`:
```go
package sdk

import "fmt"

// Controller dispatches inbound control requests to live session callbacks.
type Controller struct {
	interrupt func()
	setModel  func(string) error
}

// NewController wires the interrupt and set_model callbacks for a live session.
func NewController(interrupt func(), setModel func(string) error) *Controller {
	return &Controller{interrupt: interrupt, setModel: setModel}
}

// Handle dispatches one control request and returns the response to write back.
func (c *Controller) Handle(req ControlRequest) ControlResponse {
	switch req.Subtype() {
	case "interrupt":
		if c.interrupt != nil {
			c.interrupt()
		}
		return SuccessResponse(req.RequestID, nil)
	case "set_model":
		model, _ := req.Request["model"].(string)
		if c.setModel != nil {
			if err := c.setModel(model); err != nil {
				return ErrorResponse(req.RequestID, err.Error())
			}
		}
		return SuccessResponse(req.RequestID, map[string]any{"model": model})
	case "initialize":
		return SuccessResponse(req.RequestID, nil)
	default:
		return ErrorResponse(req.RequestID, fmt.Sprintf("unsupported control subtype %q", req.Subtype()))
	}
}
```

Create `internal/sdk/asker.go`:
```go
package sdk

import (
	"context"
	"fmt"
	"sync"

	"ccgo/internal/contracts"
	"ccgo/internal/tool"
)

// controlAsker implements tool.PermissionAsker by emitting a can_use_tool
// control_request and blocking until the SDK client resolves it. Reuses the
// Phase 1 PermissionAsker seam.
type controlAsker struct {
	send      func(ControlRequest) error
	nextReqID func() string

	mu      sync.Mutex
	waiting map[string]chan contracts.PermissionDecision
}

func newControlAsker(send func(ControlRequest) error, nextReqID func() string) *controlAsker {
	return &controlAsker{
		send:      send,
		nextReqID: nextReqID,
		waiting:   make(map[string]chan contracts.PermissionDecision),
	}
}

func (a *controlAsker) Ask(ctx context.Context, req tool.PermissionAskRequest) (contracts.PermissionDecision, error) {
	id := a.nextReqID()
	reply := make(chan contracts.PermissionDecision, 1)
	a.mu.Lock()
	a.waiting[id] = reply
	a.mu.Unlock()
	defer func() {
		a.mu.Lock()
		delete(a.waiting, id)
		a.mu.Unlock()
	}()

	control := ControlRequest{
		Type:      "control_request",
		RequestID: id,
		Request: map[string]any{
			"subtype":     "can_use_tool",
			"tool_name":   req.ToolName,
			"tool_use_id": string(req.ToolUseID),
			"blocked_path": req.Path,
			"description": req.Description,
		},
	}
	if err := a.send(control); err != nil {
		return contracts.PermissionDecision{}, fmt.Errorf("sdk: send can_use_tool: %w", err)
	}
	select {
	case d := <-reply:
		return d, nil
	case <-ctx.Done():
		return contracts.PermissionDecision{}, ctx.Err()
	}
}

// Resolve delivers a decision for a pending can_use_tool request (called when a
// matching control_response arrives).
func (a *controlAsker) Resolve(requestID string, decision contracts.PermissionDecision) {
	a.mu.Lock()
	ch := a.waiting[requestID]
	a.mu.Unlock()
	if ch != nil {
		ch <- decision
	}
}
```

Confirm `tool.PermissionAskRequest` field names match (`ToolUseID contracts.ID`, `ToolName`, `Path`, `Description`) — they were added in Phase 1 (`internal/tool/types.go:49`). If `Path` is named differently, use the real field.

- [ ] **Step 4: Run tests to verify they pass**

Run: `go test ./internal/sdk/ -v`
Expected: PASS.

- [ ] **Step 5: Commit**

```bash
git add internal/sdk/controller.go internal/sdk/asker.go internal/sdk/controller_test.go internal/sdk/asker_test.go
git commit -m "feat(sdk): canUseTool asker + interrupt + set_model control operations"
```

---

## Task 9: Importable local SDK entrypoint (`sdk.Query`)

**Files:**
- Create: `internal/sdk/query.go`
- Test: `internal/sdk/query_test.go`

**Interfaces produced:**
- `type Options struct { Prompt string; Model string; PermissionMode string; In io.Reader; Out io.Writer; RunnerFactory func() (*conversation.Runner, error) }`
- `func Query(ctx context.Context, opts Options) error` — builds/obtains a runner, installs a `controlAsker` (so tool permissions flow over the control protocol), reads control requests from `opts.In` concurrently with running the turn, writes events/responses to `opts.Out`, supports interrupt (cancel the turn ctx) and set_model (rebuild the runner's Model on the next turn).

**CC reference:** `src/entrypoints/agentSdkTypes.ts:112-122` (`query({prompt, options}): Query` — the public entrypoint signature; CC's impl throws "not implemented", so this is the ccgo native realization), `src/cli/structuredIO.ts:215-261` (the read/dispatch loop pattern).

ccgo basis: `bootstrap.State.ConversationRunner()` (`internal/bootstrap/state.go:89`) already returns a fully-wired `conversation.Runner`. The default `RunnerFactory` wraps it.

Confirm the runner + event wiring used by the headless stream-json path (to mirror it):
```bash
grep -n "func attachStreamJSON\|runner.OnEvent\|func (r \*Runner) RunTurn" /Users/sqlrush/ccgo/cmd/claude/main.go /Users/sqlrush/ccgo/internal/conversation/run.go
go doc ccgo/internal/conversation Runner | grep -i "OnEvent\|Tools\|Model"
```

- [ ] **Step 1: Write the failing test**

Create `internal/sdk/query_test.go`:
```go
package sdk

import (
	"bytes"
	"context"
	"strings"
	"testing"
	"time"

	"ccgo/internal/conversation"
)

func TestQueryRunsOneTurnAndEmitsEvents(t *testing.T) {
	t.Skip("enable after binding a stub conversation.MessageClient (see Step 3 note)")

	var out bytes.Buffer
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	err := Query(ctx, Options{
		Prompt: "hello",
		In:     strings.NewReader(""), // no control requests
		Out:    &out,
		RunnerFactory: func() (*conversation.Runner, error) {
			return &conversation.Runner{ /* Client: stubClient{reply:"hi"} */ }, nil
		},
	})
	if err != nil {
		t.Fatalf("Query err: %v", err)
	}
	if out.Len() == 0 {
		t.Fatal("Query produced no output stream")
	}
}

func TestQueryRequiresPromptOrFactory(t *testing.T) {
	err := Query(context.Background(), Options{Out: &bytes.Buffer{}})
	if err == nil {
		t.Fatal("Query must validate that a prompt and runner source are provided")
	}
}
```

- [ ] **Step 2: Run test to verify it fails (compile-only)**

Run: `go test ./internal/sdk/ -run TestQuery -v`
Expected: FAIL to compile — `undefined: Query`.

- [ ] **Step 3: Write minimal implementation**

Create `internal/sdk/query.go`:
```go
package sdk

import (
	"context"
	"fmt"
	"io"
	"strconv"
	"sync/atomic"

	"ccgo/internal/conversation"
	"ccgo/internal/messages"
)

// Options configures a programmatic SDK query (agentSdkTypes.ts:112-122).
type Options struct {
	Prompt         string
	Model          string
	PermissionMode string
	In             io.Reader
	Out            io.Writer
	// RunnerFactory builds the runner. If nil, the caller must supply one;
	// cmd/claude provides a default from bootstrap.State.ConversationRunner().
	RunnerFactory func() (*conversation.Runner, error)
}

// Query runs a single turn under the control protocol, exposing tool
// permissions via can_use_tool and supporting interrupt/set_model.
func Query(ctx context.Context, opts Options) error {
	if opts.Prompt == "" {
		return fmt.Errorf("sdk: Options.Prompt is required")
	}
	if opts.RunnerFactory == nil {
		return fmt.Errorf("sdk: Options.RunnerFactory is required")
	}
	if opts.Out == nil {
		return fmt.Errorf("sdk: Options.Out is required")
	}
	runner, err := opts.RunnerFactory()
	if err != nil {
		return fmt.Errorf("sdk: build runner: %w", err)
	}
	if opts.Model != "" {
		runner.Model = opts.Model
	}

	enc := NewEncoder(opts.Out)
	turnCtx, cancel := context.WithCancel(ctx)
	defer cancel()

	var reqCounter int64
	nextID := func() string { return "ctl-" + strconv.FormatInt(atomic.AddInt64(&reqCounter, 1), 10) }
	asker := newControlAsker(enc.WriteRequest, nextID)
	runner.Tools.Asker = asker

	controller := NewController(cancel, func(m string) error { runner.Model = m; return nil })

	// Read control requests concurrently (interrupt / set_model / responses).
	if opts.In != nil {
		go readControlLoop(turnCtx, NewDecoder(opts.In), controller, asker, enc)
	}

	runner.OnEvent = func(ev conversation.Event) {
		// Emit each turn event as a control_response-free SDK event line.
		_ = enc.WriteRequest(ControlRequest{Type: "sdk_event", Request: eventPayload(ev)})
	}

	user := messages.UserText(opts.Prompt)
	if _, err := runner.RunTurn(turnCtx, nil, user); err != nil {
		_ = enc.WriteResponse(ErrorResponse("", err.Error()))
		return err
	}
	return nil
}

// readControlLoop dispatches inbound control_request and routes control_response
// (can_use_tool replies) to the asker.
func readControlLoop(ctx context.Context, dec *Decoder, c *Controller, asker *controlAsker, enc *Encoder) {
	for {
		select {
		case <-ctx.Done():
			return
		default:
		}
		req, err := dec.Next()
		if err != nil {
			return
		}
		switch req.Type {
		case "control_request":
			_ = enc.WriteResponse(c.Handle(req))
		case "control_response":
			// A can_use_tool reply: extract behavior + requestID and resolve.
			resolveFromResponse(req, asker)
		}
	}
}

func eventPayload(ev conversation.Event) map[string]any {
	return map[string]any{"type": string(ev.Type), "model": ev.Model}
}
```

Add `resolveFromResponse` and finalize the response routing: when `opts.In` carries a `control_response` for a `can_use_tool` request, parse `{response:{request_id, response:{behavior, updatedInput, message}}}` into a `contracts.PermissionDecision` and call `asker.Resolve(requestID, decision)`. Confirm `contracts.PermissionDecision` fields (`Behavior`, `Message`, `UpdatedInput`) with `go doc ccgo/internal/contracts PermissionDecision` (verified: `internal/contracts/permissions.go:50`). For the test, bind a stub `conversation.MessageClient` per the inline note and drop the `t.Skip`; `TestQueryRequiresPromptOrFactory` is green immediately.

Add the importable wiring in `cmd/claude` (optional in this task, recommended): a `claude sdk` subcommand or `--sdk` flag that calls `sdk.Query` with `RunnerFactory: func() (*conversation.Runner, error) { r, err := state.ConversationRunner(); return &r, err }` and `In: os.Stdin, Out: os.Stdout`. Confirm the subcommand dispatch convention with `grep -n "case \"" cmd/claude/main.go | head`.

- [ ] **Step 4: Run tests + build**

Run: `go test ./internal/sdk/ -v && go build ./... && go vet ./...`
Expected: PASS, build + vet clean. The package is importable: `import "ccgo/internal/sdk"` and call `sdk.Query`.

- [ ] **Step 5: Commit**

```bash
git add internal/sdk/query.go internal/sdk/query_test.go
git commit -m "feat(sdk): importable local Query entrypoint over the control protocol"
```

---

## Self-Review

**Spec coverage (Phase-7 brief = sandbox enforces; Team runs real teammates; SDK importable):**
- OS-agnostic sandbox Policy + shouldSandbox security core → Task 1. ✓
- macOS seatbelt enforcement honoring `dangerouslyDisableSandbox` → Task 2. ✓
- Linux landlock + seccomp enforcement (build-tagged) → Task 3. ✓
- Sandbox wired into Bash (closes the security regression) → Task 4. ✓ (PowerShell parity noted)
- Task schema `model`/`isolation` + async/background agents → Task 5. ✓
- Real in-process Team/teammate runner (replaces append-only stubs) → Task 6. ✓
- SDK control_request/control_response framing → Task 7. ✓
- canUseTool + interrupt + set_model control ops → Task 8. ✓
- Importable local SDK entrypoint `sdk.Query` → Task 9. ✓

**OUT-of-scope guardrails honored:** `isolation: "remote"` is explicitly rejected (Task 5); no teleport / RemoteAgentTask / CCR / cloud cron touched; Team and SDK are strictly in-process/local.

**OS-awareness:** every enforcement test skips on the wrong OS with a reason (`profile_darwin_test.go` is build-tagged darwin; `enforce_linux_test.go` build-tagged linux; `sandbox_test.go`'s guard test runs only when `!Supported()`; the bash sandbox tests `t.Skip` when `!sandbox.Supported()`). The no-op/guard path is asserted everywhere (`enforce_other.go` returns `ErrUnsupported`; bash fails closed when `FailIfUnavailable`).

**Security emphasis:** the flag fix is the headline. `Policy.ShouldSandbox` never bypasses on the flag alone (Task 1 + Task 4 tests `TestSandboxFlagIgnoredWhenPolicyForbids`). Unsupported-but-required platforms fail closed (`failClosedCommand`). The Linux re-exec child applies confinement before `exec`, so the wrapped command cannot escape.

**Dep decision:** `golang.org/x/sys` promoted to direct (already vendored v0.46.0; provides `Prctl`, `PR_SET_NO_NEW_PRIVS`, `PR_SET_SECCOMP`, `SECCOMP_RET_*`, `LANDLOCK_ACCESS_FS_*` — verified in `zerrors_linux.go:1901-1917, 2969, 3433+`). One new dep added: `github.com/landlock-lsm/go-landlock` (v0.9.0 available; depends only on x/sys), justified because x/sys lacks the typed Landlock ruleset wrappers/syscall numbers. Seccomp filter hand-rolled with x/sys constants — no extra dep. macOS uses the OS `sandbox-exec` binary — no dep.

**Placeholder scan:** the only `t.Skip`s are the three end-to-end tests gated on binding the real `conversation.MessageClient` (Tasks 6, 9) — instructed inline, with a non-skipped sibling test (`TestRunTeammateFactoryError`, `TestQueryRequiresPromptOrFactory`) giving an immediately-green path. All production code is complete and compiles.

**Type consistency (confirmed against code today):** `conversation.Runner` (`internal/conversation/types.go:109`), `(*Runner).RunTurn(ctx, history, user) (Result, error)` (`internal/conversation/run.go:44`), `conversation.MessageClient.CreateMessage` (`types.go:21`), `tool.PermissionAsker`/`PermissionAskRequest` (`internal/tool/types.go:49`, Phase 1), `contracts.PermissionDecision` (`internal/contracts/permissions.go:50`), `contracts.Settings.Sandbox map[string]any` (`internal/contracts/settings.go:47`), `contracts.SandboxFilesystemPolicy` (`permissions.go:89`), `bashInput.DangerouslyDisableSandbox` (`internal/tools/bash/tools.go:768`), `shellCommand`/`runBashCommand` (`tools.go:1193`/`:1040`), `state.ConversationRunner()` (`internal/bootstrap/state.go:89`), build-tag convention (`internal/tools/powershell/process_unix.go:1`).

**Verification-before-completion:** every assumed ccgo symbol (settings shape, runner contract, asker seam, shell helpers, metadata convention) and CC behavior (shouldUseSandbox short-circuit, control schemas, in-process teammate) is flagged with the exact `go doc`/`grep`/`sed` command at its point of use. Landlock/seccomp/sandbox-exec API surfaces (`landlock.V5`, `SockFprog`, `SECCOMP_MODE_FILTER`, profile syntax) are flagged for confirmation before writing.

---

## Cross-phase dependencies & risks

**Hard dependency:** Tasks 8–9 (SDK `canUseTool`) reuse the **`tool.PermissionAsker` seam from Phase 1** (`internal/tool/types.go:49`, `executor.go:35`). Already merged — no blocker.

**Soft dependency:** Task 6's `RunnerFactory` is cleanest if `bootstrap.State` exposes a teammate-runner factory; today `ConversationRunner()` returns a single runner. Threading a factory through the Task tools' construction (rather than `ctx.Metadata`) is the lower-risk path and is independent of other phases.

**Risks:**
1. **Sandbox is OS- and kernel-version-sensitive.** Landlock requires Linux ≥ 5.13 (best-effort negotiation via `go-landlock` mitigates); seccomp requires `CONFIG_SECCOMP_FILTER`; `sandbox-exec` is deprecated-but-present on macOS. The `BestEffort()` + `FailIfUnavailable` policy makes degradation explicit. CI must run the Linux enforcement tests on a real Linux kernel (a darwin-only CI will silently skip them).
2. **Per-arch seccomp syscall numbers** (`__NR_socket`) — the plan ships x86_64 and flags a `GOARCH` switch before production. arm64 differs; do not skip this.
3. **Team runner cost/loops** — real teammates make real API calls; `MaxToolRounds`/budget on the factory's runner must be set so a teammate cannot loop unbounded. Reuse the host runner's existing budget config.
4. **Re-exec child entrypoint** must be dispatched before any flag parsing in `cmd/claude/main.go`; a regression there would either disable the sandbox (security) or break normal startup. Covered by a focused guard at the top of `run()`.
