package main

import (
	"bytes"
	"strings"
	"testing"
)

func TestUpdatePrintsVersion(t *testing.T) {
	var out, errOut bytes.Buffer
	if code := runUpdateCLI(nil, "0.1.0", &out, &errOut); code != 0 {
		t.Fatalf("exit %d stderr=%s", code, errOut.String())
	}
	if !strings.Contains(out.String(), "0.1.0") {
		t.Fatalf("update output missing version: %q", out.String())
	}
}

func TestUpdatePrintsNotConfiguredMessage(t *testing.T) {
	var out, errOut bytes.Buffer
	if code := runUpdateCLI(nil, "1.2.3", &out, &errOut); code != 0 {
		t.Fatalf("exit %d stderr=%s", code, errOut.String())
	}
	s := out.String()
	// Should include some indication that auto-update is not configured.
	if !strings.Contains(s, "package manager") && !strings.Contains(s, "not configured") {
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
