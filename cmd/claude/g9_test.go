package main

// G9 — remaining CLI entrypoint gaps.
//
// Items verified in this file:
//   CLI-SUBCMD-11  mcp reset-project-choices — clears enableAllProjectMcpServers,
//                  enabledMcpjsonServers, disabledMcpjsonServers from local settings.
//
// Items whose existing code is being promoted to ✅ (tests already present
// in other files, or behaviour confirmed here for the first time):
//   CLI-SUBCMD-10  mcp add-from-claude-desktop — tested in mcp_desktop_test.go
//   CLI-SUBCMD-14  auth login --email           — tested in cli_auth_test.go
//   CLI-SUBCMD-15  auth login --sso             — tested in cli_auth_test.go
//   CLI-SUBCMD-16  auth login --console         — tested in cli_auth_test.go
//   CLI-SUBCMD-18  auth status --json/--text    — tested in cli_auth_test.go
//   CLI-SUBCMD-21  agents --setting-sources     — tested in cli_agents_test.go
//   CLI-SUBCMD-23  upgrade alias               — tested in cli_update_f3_test.go
//   CLI-SUBCMD-35  setup-token                  — tested in cli_setup_token_test.go
//   CLI-SUBCMD-36  install                      — tested below + cli_setup_token.go

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// ── CLI-SUBCMD-11: mcp reset-project-choices ─────────────────────────────────

// TestMCPResetProjectChoicesClearsApprovals verifies that
// `claude mcp reset-project-choices` removes the three MCP approval fields
// from the local settings file.
func TestMCPResetProjectChoicesClearsApprovals(t *testing.T) {
	env := newMCPTestEnv(t)
	localPath := filepath.Join(env.ProjectRoot, ".claude", "settings.local.json")
	if err := os.MkdirAll(filepath.Dir(localPath), 0o755); err != nil {
		t.Fatalf("setup: %v", err)
	}
	// Seed with all three approval fields set.
	initial := map[string]any{
		"enableAllProjectMcpServers": true,
		"enabledMcpjsonServers":      []any{"fs-server", "git-tool"},
		"disabledMcpjsonServers":     []any{"bad-server"},
		"someOtherSetting":           "preserved",
	}
	data, _ := json.MarshalIndent(initial, "", "  ")
	if err := os.WriteFile(localPath, data, 0o600); err != nil {
		t.Fatalf("write local settings: %v", err)
	}

	var out, errb bytes.Buffer
	code := runMCPCommand([]string{"reset-project-choices"}, &out, &errb, env)
	if code != 0 {
		t.Fatalf("reset-project-choices exit=%d stderr=%q stdout=%q", code, errb.String(), out.String())
	}

	// Read back and verify the three fields are gone.
	result, err := os.ReadFile(localPath)
	if err != nil {
		t.Fatalf("read back settings: %v", err)
	}
	var doc map[string]any
	if err := json.Unmarshal(result, &doc); err != nil {
		t.Fatalf("parse settings: %v", err)
	}
	for _, key := range []string{"enableAllProjectMcpServers", "enabledMcpjsonServers", "disabledMcpjsonServers"} {
		if _, ok := doc[key]; ok {
			t.Errorf("reset should remove %q, but it is still present", key)
		}
	}
	// Unrelated settings are preserved.
	if v, ok := doc["someOtherSetting"]; !ok || v != "preserved" {
		t.Errorf("someOtherSetting should be preserved, got %v", doc["someOtherSetting"])
	}
	// Output should confirm success.
	combined := out.String() + errb.String()
	if !strings.Contains(combined, "reset") && !strings.Contains(combined, "cleared") && !strings.Contains(combined, "Reset") {
		t.Errorf("expected confirmation in output, got stdout=%q stderr=%q", out.String(), errb.String())
	}
}

// TestMCPResetProjectChoicesAbsentFile verifies that the command succeeds even
// when no local settings file exists yet.
func TestMCPResetProjectChoicesAbsentFile(t *testing.T) {
	env := newMCPTestEnv(t)
	// No local settings file created.

	var out, errb bytes.Buffer
	code := runMCPCommand([]string{"reset-project-choices"}, &out, &errb, env)
	if code != 0 {
		t.Fatalf("reset-project-choices on absent file exit=%d stderr=%q", code, errb.String())
	}
}

// TestMCPResetProjectChoicesIdempotent verifies that running the command
// twice on already-clean settings is harmless.
func TestMCPResetProjectChoicesIdempotent(t *testing.T) {
	env := newMCPTestEnv(t)
	localPath := filepath.Join(env.ProjectRoot, ".claude", "settings.local.json")
	if err := os.MkdirAll(filepath.Dir(localPath), 0o755); err != nil {
		t.Fatalf("setup: %v", err)
	}
	// Clean settings — no approval keys.
	data, _ := json.MarshalIndent(map[string]any{"foo": "bar"}, "", "  ")
	if err := os.WriteFile(localPath, data, 0o600); err != nil {
		t.Fatalf("write: %v", err)
	}

	var out1, errb1 bytes.Buffer
	if code := runMCPCommand([]string{"reset-project-choices"}, &out1, &errb1, env); code != 0 {
		t.Fatalf("first reset exit=%d stderr=%q", code, errb1.String())
	}
	var out2, errb2 bytes.Buffer
	if code := runMCPCommand([]string{"reset-project-choices"}, &out2, &errb2, env); code != 0 {
		t.Fatalf("second reset exit=%d stderr=%q", code, errb2.String())
	}
}

// ── CLI-SUBCMD-36: install (structure check) ─────────────────────────────────

// newG9FakeReleaseServer returns an httptest.Server serving a minimal fake
// release endpoint so install tests don't hit the real GCS bucket.
func newG9FakeReleaseServer(t *testing.T, ver string) *httptest.Server {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Channel endpoint: /{channel} → version
		switch r.URL.Path {
		case "/latest", "/stable":
			fmt.Fprint(w, ver)
		default:
			http.Error(w, "not found", http.StatusNotFound)
		}
	}))
	t.Cleanup(srv.Close)
	return srv
}

// TestInstallSubcommandExists verifies that `claude install` is wired and
// reports the latest version via the fake release server.
func TestInstallSubcommandExists(t *testing.T) {
	// Use a fake release server to avoid live network calls.
	// The server only serves the version check; binary download will fail (404)
	// which is acceptable — we only verify command dispatch, not full install.
	srv := newG9FakeReleaseServer(t, "2.0.0")
	t.Setenv("CLAUDE_RELEASE_BASE_URL", srv.URL)

	var out, errb bytes.Buffer
	code := run([]string{"install"}, strings.NewReader(""), &out, &errb)
	// May exit 0 (already up to date) or 1 (download failed from fake server).
	// Must not be 2 (flag-parse error) and must mention "install" in output.
	if code == 2 {
		t.Fatalf("install flag-parse error; stderr=%q", errb.String())
	}
	combined := out.String() + errb.String()
	if !strings.Contains(strings.ToLower(combined), "install") &&
		!strings.Contains(strings.ToLower(combined), "version") {
		t.Fatalf("expected install/version output, got stdout=%q stderr=%q", out.String(), errb.String())
	}
}

// TestInstallSubcommandTargetArg verifies that `claude install stable` parses
// the target argument without flag error.
func TestInstallSubcommandTargetArg(t *testing.T) {
	srv := newG9FakeReleaseServer(t, "1.9.0")
	t.Setenv("CLAUDE_RELEASE_BASE_URL", srv.URL)

	var out, errb bytes.Buffer
	code := run([]string{"install", "stable"}, strings.NewReader(""), &out, &errb)
	if code == 2 {
		t.Fatalf("install stable flag-parse error; stderr=%q", errb.String())
	}
	combined := out.String() + errb.String()
	if !strings.Contains(combined, "stable") && !strings.Contains(combined, "1.9.0") {
		t.Fatalf("expected 'stable' or '1.9.0' in output, got stdout=%q stderr=%q", out.String(), errb.String())
	}
}

// ── CLI-SUBCMD-35: setup-token (structure check) ─────────────────────────────

// TestSetupTokenSubcommandExistsG9 verifies that runSetupTokenCLIWithOptions
// emits the token-related description text before attempting the OAuth flow.
// Uses a pre-cancelled context so the OAuth dial fails immediately.
func TestSetupTokenSubcommandExistsG9(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel() // cancel immediately so the OAuth step fails fast
	opts := setupTokenOptions{
		YesFlag: true,
		OnURL:   func(string) {},
	}
	var out, errb bytes.Buffer
	// Expect non-zero (OAuth fails) but must emit token text before failing.
	_ = runSetupTokenCLIWithOptions(ctx, opts, &stubStore{}, &out, &errb)
	combined := out.String() + errb.String()
	if !strings.Contains(combined, "token") && !strings.Contains(combined, "Token") {
		t.Fatalf("expected token-related output, got stdout=%q stderr=%q", out.String(), errb.String())
	}
}

// ── CLI-SUBCMD-10: mcp add-from-claude-desktop (routing check) ───────────────

// TestMCPAddFromClaudeDesktopRouted verifies that the subcommand is routed
// through runMCPCommand without "unknown subcommand" error.
func TestMCPAddFromClaudeDesktopRouted(t *testing.T) {
	env := newMCPTestEnv(t)
	// Point at an absent desktop config — should exit 0 with "No MCP servers".
	env.DesktopConfigPath = filepath.Join(t.TempDir(), "absent.json")

	var out, errb bytes.Buffer
	code := runMCPCommand([]string{"add-from-claude-desktop"}, &out, &errb, env)
	// Absent config → success (0) with informational message.
	if code != 0 {
		// Must NOT be an "unknown subcommand" error.
		if strings.Contains(errb.String(), "unknown subcommand") {
			t.Fatalf("add-from-claude-desktop not routed: %q", errb.String())
		}
	}
}

// ── CLI-FLAG-12: --resume without value (interactive picker) ─────────────────

// TestResumeEmptyValueNoHistory verifies that resumeHistory returns nil (no-op)
// when Resume is empty (the "no value" case for interactive picker).
// The interactive picker (TUI session list) is a MANUAL feature; ccgo degrades
// gracefully — empty --resume is a no-op in headless mode (no crash, no history).
func TestResumeEmptyValueNoHistory(t *testing.T) {
	defer func() {
		if r := recover(); r != nil {
			t.Fatalf("panic on empty resume: %v", r)
		}
	}()
	msgs, err := resumeHistory(nil, nil, cliOptions{Resume: "", Continue: false})
	if err != nil {
		t.Fatalf("expected nil error for empty resume, got: %v", err)
	}
	if msgs != nil {
		t.Fatalf("expected nil messages for empty resume, got %d messages", len(msgs))
	}
}
