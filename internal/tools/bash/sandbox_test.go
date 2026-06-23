package bashtools

import (
	"testing"

	"ccgo/internal/sandbox"
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
