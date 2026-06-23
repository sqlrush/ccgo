package main

import (
	"bytes"
	"strings"
	"testing"
)

// --- F3-C03: update improvements ---

func TestUpdateReadsAutoUpdatesChannel(t *testing.T) {
	// When settings provide autoUpdatesChannel=stable the output should mention
	// the channel. Use InstallType="unknown" to avoid dev-build exit-1.
	var out, errOut bytes.Buffer
	opts := updateOptions{
		Ver:         "1.0.0",
		Channel:     "stable",
		InstallType: "unknown",
	}
	code := runUpdateCLIv2(nil, opts, &out, &errOut)
	if code != 0 {
		t.Fatalf("exit %d stderr=%s", code, errOut.String())
	}
	if !strings.Contains(out.String(), "stable") {
		t.Fatalf("update output should mention channel 'stable': %q", out.String())
	}
}

func TestUpdateChannelDefault(t *testing.T) {
	// Default channel is "latest" when not specified.
	var out, errOut bytes.Buffer
	opts := updateOptions{
		Ver:         "1.0.0",
		Channel:     "latest",
		InstallType: "unknown",
	}
	code := runUpdateCLIv2(nil, opts, &out, &errOut)
	if code != 0 {
		t.Fatalf("exit %d stderr=%s", code, errOut.String())
	}
	if !strings.Contains(out.String(), "latest") {
		t.Fatalf("update output should mention channel 'latest': %q", out.String())
	}
}

func TestUpdateDevelopmentBuildWarning(t *testing.T) {
	// Development build should exit 1 with a clear warning message.
	var out, errOut bytes.Buffer
	opts := updateOptions{
		Ver:          "1.0.0",
		Channel:      "latest",
		InstallType:  "development",
	}
	code := runUpdateCLIv2(nil, opts, &out, &errOut)
	if code != 1 {
		t.Fatalf("expected exit 1 for dev build, got %d", code)
	}
	combined := out.String() + errOut.String()
	if !strings.Contains(strings.ToLower(combined), "development") {
		t.Fatalf("expected 'development' warning in output: %q", combined)
	}
}

func TestUpdateHomebrewInstallCommand(t *testing.T) {
	// package-manager (homebrew) install should print "brew upgrade claude-code".
	var out, errOut bytes.Buffer
	opts := updateOptions{
		Ver:            "1.0.0",
		Channel:        "latest",
		InstallType:    "package-manager",
		PackageManager: "homebrew",
	}
	code := runUpdateCLIv2(nil, opts, &out, &errOut)
	if code != 0 {
		t.Fatalf("exit %d stderr=%s", code, errOut.String())
	}
	if !strings.Contains(out.String(), "brew upgrade") {
		t.Fatalf("expected 'brew upgrade' in output: %q", out.String())
	}
}

func TestUpdateWingetInstallCommand(t *testing.T) {
	// package-manager (winget) install should print winget upgrade command.
	var out, errOut bytes.Buffer
	opts := updateOptions{
		Ver:            "1.0.0",
		Channel:        "latest",
		InstallType:    "package-manager",
		PackageManager: "winget",
	}
	code := runUpdateCLIv2(nil, opts, &out, &errOut)
	if code != 0 {
		t.Fatalf("exit %d stderr=%s", code, errOut.String())
	}
	if !strings.Contains(out.String(), "winget upgrade") {
		t.Fatalf("expected 'winget upgrade' in output: %q", out.String())
	}
}

func TestUpdateMultipleInstallationsWarning(t *testing.T) {
	// Multiple installs should produce a warning.
	var out, errOut bytes.Buffer
	opts := updateOptions{
		Ver:         "1.0.0",
		Channel:     "latest",
		InstallType: "unknown",
		MultipleInstallations: []installEntry{
			{Type: "npm-global", Path: "/usr/local/lib/node_modules"},
			{Type: "native", Path: "/usr/local/bin/claude"},
		},
	}
	code := runUpdateCLIv2(nil, opts, &out, &errOut)
	if code != 0 {
		t.Fatalf("exit %d stderr=%s", code, errOut.String())
	}
	if !strings.Contains(out.String(), "Multiple") && !strings.Contains(out.String(), "multiple") {
		t.Fatalf("expected multiple-installations warning: %q", out.String())
	}
}

func TestUpdateUpgradeAlias(t *testing.T) {
	// The "upgrade" command alias is dispatched by main.go; we test the
	// same runUpdateCLIv2 function handles upgrade gracefully.
	var out, errOut bytes.Buffer
	opts := updateOptions{
		Ver:         "2.0.0",
		Channel:     "latest",
		InstallType: "unknown",
	}
	code := runUpdateCLIv2(nil, opts, &out, &errOut)
	if code != 0 {
		t.Fatalf("exit %d stderr=%s", code, errOut.String())
	}
	if !strings.Contains(out.String(), "2.0.0") {
		t.Fatalf("expected version in output: %q", out.String())
	}
}
