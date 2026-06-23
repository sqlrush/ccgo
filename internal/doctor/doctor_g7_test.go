package doctor

import (
	"runtime"
	"strings"
	"testing"
)

// --- G7: Doctor deep checks ---

// TestRunChecksConfigMismatch verifies SUBCMD-DOCTOR-09: when the configured
// installMethod disagrees with the detected install type, a WARN is emitted.
func TestRunChecksConfigMismatch(t *testing.T) {
	// Binary is native (/usr/local/bin/) but ConfigInstallMethod says "global".
	report := Run(Input{
		Version:             "1.0.0",
		CWD:                 t.TempDir(),
		ExecutableFn:        func() (string, error) { return "/usr/local/bin/claude", nil },
		ConfigInstallMethod: "global",
	})
	var sawMismatch bool
	for _, c := range report.Checks {
		if strings.Contains(strings.ToLower(c.Name), "config") &&
			strings.Contains(strings.ToLower(c.Name), "mismatch") {
			sawMismatch = true
			if c.Status != StatusWarn {
				t.Fatalf("config-mismatch check should be WARN, got %q detail=%q", c.Status, c.Detail)
			}
			if !strings.Contains(c.Detail, "global") {
				t.Fatalf("config-mismatch detail should mention configured value; got %q", c.Detail)
			}
		}
	}
	if !sawMismatch {
		t.Fatal("expected a config-mismatch check when installMethod disagrees with detected type")
	}
}

// TestRunChecksConfigMatchNoWarn verifies no config-mismatch warn when values agree.
func TestRunChecksConfigMatchNoWarn(t *testing.T) {
	// Binary is native, config also says "native" → no mismatch warn.
	report := Run(Input{
		Version:             "1.0.0",
		CWD:                 t.TempDir(),
		ExecutableFn:        func() (string, error) { return "/usr/local/bin/claude", nil },
		ConfigInstallMethod: "native",
	})
	for _, c := range report.Checks {
		if strings.Contains(strings.ToLower(c.Name), "config") &&
			strings.Contains(strings.ToLower(c.Name), "mismatch") &&
			c.Status == StatusWarn {
			t.Fatalf("should not warn when installMethod matches; check: %+v", c)
		}
	}
}

// TestRunChecksConfigMismatchEmptySkipped verifies that when ConfigInstallMethod
// is empty (not set), no mismatch check is emitted.
func TestRunChecksConfigMismatchEmptySkipped(t *testing.T) {
	report := Run(Input{
		Version: "1.0.0",
		CWD:     t.TempDir(),
	})
	for _, c := range report.Checks {
		if strings.Contains(strings.ToLower(c.Name), "config") &&
			strings.Contains(strings.ToLower(c.Name), "mismatch") {
			t.Fatalf("should not emit mismatch check when ConfigInstallMethod is empty; check: %+v", c)
		}
	}
}

// TestRunChecksLinuxGlobPatternsWarning verifies SUBCMD-DOCTOR-11: when
// LinuxGlobPatterns is non-empty (Linux platform), a WARN check is emitted.
func TestRunChecksLinuxGlobPatternsWarning(t *testing.T) {
	if runtime.GOOS != "linux" {
		t.Skip("linux-specific check only runs on Linux")
	}
	report := Run(Input{
		Version:            "1.0.0",
		CWD:                t.TempDir(),
		LinuxGlobPatterns:  []string{"~/code/**", "/tmp/*.sh"},
	})
	var sawGlob bool
	for _, c := range report.Checks {
		if strings.Contains(strings.ToLower(c.Name), "glob") ||
			strings.Contains(strings.ToLower(c.Name), "sandbox") {
			sawGlob = true
			if c.Status != StatusWarn {
				t.Fatalf("glob patterns check should be WARN on Linux, got %q detail=%q", c.Status, c.Detail)
			}
		}
	}
	if !sawGlob {
		t.Fatal("expected a sandbox glob-patterns check on Linux when LinuxGlobPatterns is set")
	}
}

// TestRunChecksLinuxGlobPatternsInjectedAlwaysRuns verifies that when
// LinuxGlobPatterns is set, a WARN check appears regardless of OS,
// as long as len > 0 (allows testing on macOS/Windows CI).
func TestRunChecksLinuxGlobPatternsInjected(t *testing.T) {
	report := Run(Input{
		Version:           "1.0.0",
		CWD:               t.TempDir(),
		LinuxGlobPatterns: []string{"~/code/**"},
		// Force Linux behaviour via injection even on other OS.
		ForceLinuxGlobCheck: true,
	})
	var sawGlob bool
	for _, c := range report.Checks {
		if strings.Contains(strings.ToLower(c.Name), "glob") ||
			strings.Contains(strings.ToLower(c.Name), "sandbox") {
			sawGlob = true
			if c.Status != StatusWarn {
				t.Fatalf("glob patterns check should be WARN, got %q detail=%q", c.Status, c.Detail)
			}
			if !strings.Contains(c.Detail, "~/code/**") {
				t.Fatalf("glob check detail should mention a pattern; got %q", c.Detail)
			}
		}
	}
	if !sawGlob {
		t.Fatal("expected a sandbox glob-patterns check when ForceLinuxGlobCheck=true and LinuxGlobPatterns non-empty")
	}
}

// TestRunChecksNoGlobWhenEmpty verifies no glob warning when LinuxGlobPatterns is nil/empty.
func TestRunChecksNoGlobWhenEmpty(t *testing.T) {
	report := Run(Input{
		Version:             "1.0.0",
		CWD:                 t.TempDir(),
		ForceLinuxGlobCheck: true, // even forced, empty patterns → no warn
	})
	for _, c := range report.Checks {
		if (strings.Contains(strings.ToLower(c.Name), "glob") ||
			strings.Contains(strings.ToLower(c.Name), "sandbox")) &&
			c.Status == StatusWarn {
			t.Fatalf("should not warn about glob patterns when list is empty; check: %+v", c)
		}
	}
}

// TestRunChecksStaleLockFiles verifies SUBCMD-DOCTOR-12: when StaleLockFiles
// is non-empty, a WARN check is emitted listing the count.
func TestRunChecksStaleLockFiles(t *testing.T) {
	report := Run(Input{
		Version: "1.0.0",
		CWD:     t.TempDir(),
		StaleLockFiles: []string{
			"/home/user/.local/state/claude/locks/12345.lock",
			"/home/user/.local/state/claude/locks/67890.lock",
		},
	})
	var sawLock bool
	for _, c := range report.Checks {
		if strings.Contains(strings.ToLower(c.Name), "lock") ||
			strings.Contains(strings.ToLower(c.Name), "stale") {
			sawLock = true
			if c.Status != StatusWarn {
				t.Fatalf("stale-locks check should be WARN, got %q detail=%q", c.Status, c.Detail)
			}
			if !strings.Contains(c.Detail, "2") {
				t.Fatalf("stale-locks detail should mention count; got %q", c.Detail)
			}
		}
	}
	if !sawLock {
		t.Fatal("expected a stale-lock-files check when StaleLockFiles is non-empty")
	}
}

// TestRunChecksNoStaleLockWhenEmpty verifies no stale-lock warn with empty list.
func TestRunChecksNoStaleLockWhenEmpty(t *testing.T) {
	report := Run(Input{
		Version: "1.0.0",
		CWD:     t.TempDir(),
	})
	for _, c := range report.Checks {
		if (strings.Contains(strings.ToLower(c.Name), "lock") ||
			strings.Contains(strings.ToLower(c.Name), "stale")) &&
			c.Status == StatusWarn {
			t.Fatalf("should not warn about stale locks when list is empty; check: %+v", c)
		}
	}
}
