package bashtools

import (
	"os"
	"strings"
	"testing"

	"ccgo/internal/contracts"
	"ccgo/internal/sandbox"
	"ccgo/internal/tool"
)

// TestSandboxedCommandWrapsWhenEnabled asserts that when the sandbox is enabled
// and supported, sandboxedShellCommand returns a wrapped executable (not the
// raw shell), so the command runs confined.
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

// TestSandboxBypassRespectsPolicy asserts that when the policy explicitly
// allows unsandboxed commands (AllowUnsandboxed=true) AND the caller sets
// dangerouslyDisableSandbox=true, the raw shell is returned (no wrapping).
func TestSandboxBypassRespectsPolicy(t *testing.T) {
	p := sandbox.Policy{Enabled: true, AllowUnsandboxed: true}
	// dangerouslyDisableSandbox=true + policy allows => no wrapping.
	name, _ := sandboxedShellCommand("echo hi", p, true)
	if name != defaultShell() {
		t.Fatalf("flag+policy should bypass sandbox, got wrapper %q", name)
	}
}

// TestSandboxFlagIgnoredWhenPolicyForbids is the critical anti-footgun test:
// when the policy does NOT allow unsandboxed commands, the dangerous flag must
// not silently disable the sandbox (SECURITY invariant).
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

// TestSandboxDefaultOff asserts that the zero-value policy (sandbox disabled)
// produces no wrapping — preserving backward-compatible default behavior.
func TestSandboxDefaultOff(t *testing.T) {
	p := sandbox.Policy{} // zero value: Enabled=false
	name, args := sandboxedShellCommand("echo hi", p, false)
	if name != defaultShell() {
		t.Fatalf("sandbox off: expected raw shell %q, got %q", defaultShell(), name)
	}
	_ = args
}

// TestFailIfUnavailableFailsClosedWhenUnsupported asserts that when
// FailIfUnavailable=true and the sandbox is not supported, sandboxedShellCommand
// returns a command that will exit non-zero (fail-closed), never running
// unconfined. We test this even on supported platforms by using the
// failClosedCommand helper directly.
func TestFailIfUnavailableFailsClosedWhenUnsupported(t *testing.T) {
	if sandbox.Supported() {
		// On a supported platform we can't force Supported() to be false,
		// so we test the failClosedCommand path directly.
		p := sandbox.Policy{Enabled: true, FailIfUnavailable: true}
		name, args := failClosedCommand(p)
		if name == "" {
			t.Fatal("failClosedCommand returned empty name")
		}
		if len(args) == 0 {
			t.Fatal("failClosedCommand returned no args")
		}
		// The args must produce a non-zero exit; verify the exit-1 sentinel is
		// present somewhere in the shell expression.
		found := false
		for _, a := range args {
			if a == "exit 1" || contains(a, "exit 1") {
				found = true
				break
			}
		}
		if !found {
			t.Errorf("failClosedCommand args do not contain 'exit 1': %v", args)
		}
	} else {
		// On an unsupported platform, verify that sandboxedShellCommand with
		// FailIfUnavailable=true does not return the raw shell (it must fail-close).
		p := sandbox.Policy{Enabled: true, FailIfUnavailable: true}
		name, args := sandboxedShellCommand("echo hi", p, false)
		// Must NOT be the raw shell invocation of "echo hi".
		if name == defaultShell() && len(args) > 0 && args[len(args)-1] == "echo hi" {
			t.Fatal("FailIfUnavailable=true but sandbox unsupported: must fail-closed, not run unconfined")
		}
		_ = args
	}
}

// TestFailIfUnavailableFalseRunsUnconfined asserts that when
// FailIfUnavailable=false and the sandbox is not supported, the command runs
// unconfined (best-effort degraded mode) on unsupported platforms.
func TestFailIfUnavailableFalseRunsUnconfined(t *testing.T) {
	if sandbox.Supported() {
		t.Skip("platform supports sandbox; unsupported-path cannot be triggered here")
	}
	p := sandbox.Policy{Enabled: true, FailIfUnavailable: false}
	name, _ := sandboxedShellCommand("echo hi", p, false)
	if name != defaultShell() {
		t.Fatalf("FailIfUnavailable=false + unsupported: expected raw shell %q, got %q", defaultShell(), name)
	}
}

// TestSandboxExcludedCommandSkipsSandbox asserts that a command matching
// ExcludedCommands bypasses the sandbox (SBX-05 integration).
func TestSandboxExcludedCommandSkipsSandbox(t *testing.T) {
	p := sandbox.Policy{
		Enabled:          true,
		AllowUnsandboxed: true,
		ExcludedCommands: []string{"git"},
	}
	name, _ := sandboxedShellCommand("git status", p, false)
	if name != defaultShell() {
		t.Fatalf("SBX-05: excluded 'git status' must bypass sandbox, got wrapper %q", name)
	}
}

// TestSandboxExcludedCompoundCommandSkipsSandbox asserts compound commands
// where any segment matches ExcludedCommands bypass the sandbox (SBX-06).
func TestSandboxExcludedCompoundCommandSkipsSandbox(t *testing.T) {
	p := sandbox.Policy{
		Enabled:          true,
		AllowUnsandboxed: true,
		ExcludedCommands: []string{"git"},
	}
	name, _ := sandboxedShellCommand("echo hi && git status", p, false)
	if name != defaultShell() {
		t.Fatalf("SBX-06: compound with excluded segment must bypass sandbox, got wrapper %q", name)
	}
}

// TestCFG20_SandboxSettingsWiredViaBashTool verifies CFG-20:
// settings.sandbox is fully wired to the Bash tool's sandbox enforcement path.
// sandboxPolicyFromContext reads contracts.Settings from tool metadata and
// PolicyFromSettings maps sandbox.enabled → Policy.Enabled — the same settings
// value that sandbox-adapter.ts reads in CC (CC ref: utils/settings/types.ts:255+).
func TestCFG20_SandboxSettingsWiredViaBashTool(t *testing.T) {
	t.Parallel()

	// Given: metadata carrying settings with sandbox.enabled=true
	ctx := tool.Context{
		Metadata: map[string]any{
			tool.MetadataSettingsKey: contracts.Settings{
				Sandbox: map[string]any{"enabled": true},
			},
		},
	}

	// When: sandboxPolicyFromContext is called (the path callBash takes)
	policy := sandboxPolicyFromContext(ctx)

	// Then: Policy.Enabled reflects the settings value
	if !policy.Enabled {
		t.Fatal("CFG-20: settings.sandbox.enabled=true must yield Policy.Enabled=true")
	}
}

// TestSBX58_PolicyRebuiltPerCall verifies SBX-58:
// sandboxPolicyFromContext rebuilds the policy from metadata on every call,
// so a settings change between two Bash tool calls is reflected in the new Policy
// without any explicit refreshConfig() mechanism.
//
// This is ccgo's answer to CC's refreshConfig(): rather than mutable module
// state, the policy is derived from the live tool.Context metadata injected
// freshly for each turn/call.
// CC ref: sandbox-adapter.ts:798,920,941 refreshConfig().
func TestSBX58_PolicyRebuiltPerCall(t *testing.T) {
	t.Parallel()
	// First call: sandbox disabled in settings.
	ctx1 := tool.Context{
		Metadata: map[string]any{
			tool.MetadataSettingsKey: contracts.Settings{
				Sandbox: map[string]any{"enabled": false},
			},
		},
	}
	p1 := sandboxPolicyFromContext(ctx1)
	if p1.Enabled {
		t.Fatal("SBX-58: policy from ctx1 must have Enabled=false")
	}

	// Second call: sandbox enabled. Policy must reflect updated settings.
	ctx2 := tool.Context{
		Metadata: map[string]any{
			tool.MetadataSettingsKey: contracts.Settings{
				Sandbox: map[string]any{"enabled": true},
			},
		},
	}
	p2 := sandboxPolicyFromContext(ctx2)
	if !p2.Enabled {
		t.Fatal("SBX-58: policy from ctx2 must have Enabled=true (settings changed)")
	}
}

// TestSBX48_StartProxyForPolicyNoProxy verifies that startProxyForPolicy returns
// nil env vars and a noop stop when the policy has no domain rules.
func TestSBX48_StartProxyForPolicyNoProxy(t *testing.T) {
	p := sandbox.Policy{Enabled: true, AllowNetwork: true}
	envVars, stop, err := startProxyForPolicy(p)
	if err != nil {
		t.Fatalf("SBX-48: startProxyForPolicy with no domains returned error: %v", err)
	}
	defer stop()
	if envVars != nil {
		t.Errorf("SBX-48: no domain rules → envVars must be nil, got %v", envVars)
	}
}

// TestSBX48_StartProxyForPolicyWithDomains verifies that startProxyForPolicy
// returns proxy env vars (HTTP_PROXY etc.) when AllowedDomains is non-empty.
// This confirms the proxy wiring is prod-reachable (not nil-DI).
func TestSBX48_StartProxyForPolicyWithDomains(t *testing.T) {
	p := sandbox.Policy{
		Enabled:        true,
		AllowedDomains: []string{"api.github.com"},
	}
	envVars, stop, err := startProxyForPolicy(p)
	if err != nil {
		t.Fatalf("SBX-48: startProxyForPolicy with domains returned error: %v", err)
	}
	defer stop()
	if len(envVars) == 0 {
		t.Fatal("SBX-48: AllowedDomains set → envVars must be non-empty (proxy started)")
	}
	// Must include HTTP_PROXY and HTTPS_PROXY.
	for _, key := range []string{"HTTP_PROXY", "HTTPS_PROXY", "ALL_PROXY"} {
		found := false
		for _, e := range envVars {
			if strings.HasPrefix(e, key+"=") {
				found = true
				break
			}
		}
		if !found {
			t.Errorf("SBX-48: env missing %s (proxy vars not injected)", key)
		}
	}
}

// TestSBX48_EnvWithProxyVarsNilPassthrough verifies that envWithProxyVars(nil)
// returns os.Environ() unchanged.
func TestSBX48_EnvWithProxyVarsNilPassthrough(t *testing.T) {
	got := envWithProxyVars(nil)
	want := os.Environ()
	if len(got) != len(want) {
		t.Errorf("SBX-48: envWithProxyVars(nil) len = %d, want %d (os.Environ)", len(got), len(want))
	}
}

// TestSBX48_EnvWithProxyVarsMerges verifies that proxy vars are appended to
// os.Environ() when provided.
func TestSBX48_EnvWithProxyVarsMerges(t *testing.T) {
	extra := []string{"HTTP_PROXY=http://127.0.0.1:9999"}
	got := envWithProxyVars(extra)
	base := os.Environ()
	if len(got) != len(base)+1 {
		t.Errorf("SBX-48: envWithProxyVars merged len = %d, want %d", len(got), len(base)+1)
	}
	last := got[len(got)-1]
	if last != extra[0] {
		t.Errorf("SBX-48: proxy var not at end: got %q, want %q", last, extra[0])
	}
}

// contains is a tiny helper for the test file only.
func contains(s, sub string) bool {
	return len(s) >= len(sub) && (s == sub || len(s) > 0 && stringContains(s, sub))
}

func stringContains(s, sub string) bool {
	for i := 0; i <= len(s)-len(sub); i++ {
		if s[i:i+len(sub)] == sub {
			return true
		}
	}
	return false
}
