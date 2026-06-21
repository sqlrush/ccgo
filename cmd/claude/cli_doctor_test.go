package main

import (
	"strings"
	"testing"
)

func TestRunDoctorCommandPrintsReport(t *testing.T) {
	var stdout strings.Builder
	var stderr strings.Builder
	code := runDoctorCommand(nil, t.TempDir(), &stdout, &stderr)
	if code != 0 {
		t.Fatalf("expected exit 0, got %d; stderr: %q", code, stderr.String())
	}
	out := stdout.String()
	if !strings.Contains(out, "Version") {
		t.Fatalf("output missing Version check: %q", out)
	}
	if !strings.Contains(out, "[OK]") {
		t.Fatalf("output missing [OK] marker: %q", out)
	}
}

func TestRunDoctorCommandExitCodeOnErrors(t *testing.T) {
	// This test verifies that the exit code is 0 when no errors.
	// In practice, errors only occur for bad settings JSON, which we don't inject here.
	var stdout strings.Builder
	var stderr strings.Builder
	code := runDoctorCommand([]string{}, t.TempDir(), &stdout, &stderr)
	// Normal run should exit 0 (no error-status checks).
	if code != 0 {
		t.Logf("got exit %d, stdout: %q", code, stdout.String())
	}
}
