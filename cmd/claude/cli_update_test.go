package main

import (
	"bytes"
	"strings"
	"testing"
)

// TestUpdatePrintsVersion and TestUpdatePrintsNotConfiguredMessage test the
// runUpdateCLIv2 function directly with an explicit "unknown" install type so
// the dev-build guard doesn't interfere (test binaries are classified as
// "development" by resolveInstallType).

func TestUpdatePrintsVersion(t *testing.T) {
	var out, errOut bytes.Buffer
	opts := updateOptions{Ver: "0.1.0", Channel: "latest", InstallType: "unknown"}
	if code := runUpdateCLIv2(nil, opts, &out, &errOut); code != 0 {
		t.Fatalf("exit %d stderr=%s", code, errOut.String())
	}
	if !strings.Contains(out.String(), "0.1.0") {
		t.Fatalf("update output missing version: %q", out.String())
	}
}

func TestUpdatePrintsNotConfiguredMessage(t *testing.T) {
	var out, errOut bytes.Buffer
	opts := updateOptions{Ver: "1.2.3", Channel: "latest", InstallType: "unknown"}
	if code := runUpdateCLIv2(nil, opts, &out, &errOut); code != 0 {
		t.Fatalf("exit %d stderr=%s", code, errOut.String())
	}
	s := out.String()
	// Should include some indication that auto-update is not configured.
	if !strings.Contains(s, "package manager") && !strings.Contains(s, "not configured") &&
		!strings.Contains(s, "your package manager") {
		t.Fatalf("update output missing guidance message: %q", s)
	}
}

func TestUpdateCheckFlag(t *testing.T) {
	var out, errOut bytes.Buffer
	// --check is a no-op stub; must succeed and print version.
	if code := runUpdateCLI([]string{"--check"}, "2.0.0", &out, &errOut); code != 0 {
		t.Fatalf("--check exit %d stderr=%s", code, errOut.String())
	}
	if !strings.Contains(out.String(), "2.0.0") {
		t.Fatalf("--check output missing version: %q", out.String())
	}
}
