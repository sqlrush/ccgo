package sandbox

import (
	"runtime"
	"strings"
	"testing"
)

// TestDepCheckResultFields verifies DepCheckResult carries expected fields.
func TestDepCheckResultFields(t *testing.T) {
	result := DepCheck(depCheckInput{
		lookupFile: func(path string) bool { return true },
		lookPath:   func(cmd string) (string, error) { return "/usr/bin/" + cmd, nil },
	})
	// Result must be defined (not panic).
	_ = result.Available
	_ = result.Errors
	_ = result.Warnings
}

// TestDepCheckDarwinSandboxExecMissing verifies that a missing sandbox-exec on macOS
// is reported as an error.
func TestDepCheckDarwinSandboxExecMissing(t *testing.T) {
	if runtime.GOOS != "darwin" {
		t.Skip("darwin-only check")
	}
	result := DepCheck(depCheckInput{
		lookupFile: func(path string) bool { return false }, // everything missing
		lookPath:   func(cmd string) (string, error) { return "", nil },
	})
	found := false
	for _, e := range result.Errors {
		if strings.Contains(e, "sandbox-exec") {
			found = true
			break
		}
	}
	if !found {
		t.Fatalf("expected error about missing sandbox-exec; got errors=%v warnings=%v", result.Errors, result.Warnings)
	}
	if result.Available {
		t.Fatal("Available must be false when sandbox-exec is missing on darwin")
	}
}

// TestDepCheckAllPresent verifies Available=true when required executables are present.
func TestDepCheckAllPresent(t *testing.T) {
	result := DepCheck(depCheckInput{
		lookupFile: func(path string) bool { return true },
		lookPath:   func(cmd string) (string, error) { return "/usr/bin/" + cmd, nil },
	})
	if !result.Available {
		t.Fatalf("all deps present but Available=false; errors=%v", result.Errors)
	}
}

// TestDepCheckRipgrepMissingWarning verifies that a missing ripgrep produces a warning
// (degraded, not unavailable).
func TestDepCheckRipgrepMissingWarning(t *testing.T) {
	result := DepCheck(depCheckInput{
		lookupFile: func(path string) bool { return path != "rg" }, // rg missing
		lookPath:   func(cmd string) (string, error) {
			if cmd == "rg" {
				return "", errNotFound
			}
			return "/usr/bin/" + cmd, nil
		},
	})
	found := false
	for _, w := range result.Warnings {
		if strings.Contains(w, "rg") || strings.Contains(w, "ripgrep") {
			found = true
			break
		}
	}
	if !found {
		t.Fatalf("expected warning about missing rg; got warnings=%v", result.Warnings)
	}
}
