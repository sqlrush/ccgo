package doctor

import (
	"strings"
	"testing"
)

// TestRunChecksSandboxDiagnosticsEnabled verifies SBX-38/SBX-39:
// when SandboxEnabled=true and SandboxUnavailableReason is non-empty,
// a WARN check is emitted.
func TestRunChecksSandboxDiagnosticsUnavailable(t *testing.T) {
	report := Run(Input{
		Version:                  "1.0.0",
		CWD:                      t.TempDir(),
		SandboxEnabled:           true,
		SandboxUnavailableReason: "sandbox.enabled is set but sandbox-exec not found at /usr/bin/sandbox-exec",
	})
	var sawSandbox bool
	for _, c := range report.Checks {
		if strings.Contains(strings.ToLower(c.Name), "sandbox") {
			sawSandbox = true
			if c.Status != StatusWarn && c.Status != StatusError {
				t.Fatalf("sandbox check should be WARN or ERR when unavailable; got %q detail=%q", c.Status, c.Detail)
			}
			if !strings.Contains(c.Detail, "sandbox-exec") {
				t.Fatalf("sandbox check detail should contain the unavailable reason; got %q", c.Detail)
			}
		}
	}
	if !sawSandbox {
		t.Fatal("expected a sandbox check when SandboxEnabled=true and reason is non-empty")
	}
}

// TestRunChecksSandboxEnabledAndAvailable verifies that when sandbox is enabled
// and working, an OK check is emitted.
func TestRunChecksSandboxEnabledAndAvailable(t *testing.T) {
	report := Run(Input{
		Version:        "1.0.0",
		CWD:            t.TempDir(),
		SandboxEnabled: true,
		// No SandboxUnavailableReason → sandbox is working.
	})
	var sawSandbox bool
	for _, c := range report.Checks {
		if strings.Contains(strings.ToLower(c.Name), "sandbox") {
			sawSandbox = true
			if c.Status != StatusOK {
				t.Fatalf("sandbox check should be OK when enabled and no reason; got %q detail=%q", c.Status, c.Detail)
			}
		}
	}
	if !sawSandbox {
		t.Fatal("expected a sandbox check when SandboxEnabled=true")
	}
}

// TestRunChecksSandboxDisabledNoCheck verifies that when sandbox is disabled,
// no sandbox check is emitted (no noise for users who don't use it).
func TestRunChecksSandboxDisabledNoCheck(t *testing.T) {
	report := Run(Input{
		Version: "1.0.0",
		CWD:     t.TempDir(),
		// SandboxEnabled=false (default)
	})
	for _, c := range report.Checks {
		if strings.Contains(strings.ToLower(c.Name), "sandbox") {
			t.Fatalf("should not emit sandbox check when sandbox is disabled; got check: %+v", c)
		}
	}
}

// TestRunChecksSandboxDepWarnings verifies that sandbox dep warnings
// (from SandboxDepWarnings) surface as WARN checks.
func TestRunChecksSandboxDepWarnings(t *testing.T) {
	report := Run(Input{
		Version:            "1.0.0",
		CWD:                t.TempDir(),
		SandboxEnabled:     true,
		SandboxDepWarnings: []string{"rg (ripgrep) not found in PATH — file-search performance may be degraded"},
	})
	var sawDepWarn bool
	for _, c := range report.Checks {
		if strings.Contains(strings.ToLower(c.Name), "sandbox") &&
			strings.Contains(strings.ToLower(c.Detail), "ripgrep") {
			sawDepWarn = true
			if c.Status != StatusWarn {
				t.Fatalf("sandbox dep warning check should be WARN; got %q", c.Status)
			}
		}
	}
	if !sawDepWarn {
		t.Fatal("expected a sandbox WARN check about ripgrep from SandboxDepWarnings")
	}
}
