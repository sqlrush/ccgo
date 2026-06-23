// Package main – W-Batch-D wiring tests (W-C10/12/17/22).
// Tests are in package main so they can call run() and headlessRunner() directly.
package main

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"ccgo/internal/bootstrap"
	"ccgo/internal/contracts"
	"ccgo/internal/conversation"
	"ccgo/internal/costtrack"
	"ccgo/internal/mcp/reconnect"
	"ccgo/internal/rewind"
)

// ─── W-C10: cost persistence ──────────────────────────────────────────────────

// TestCostSaveRestoreRoundTrip verifies that costtrack.Save/Restore work as a
// round-trip: saving a ProjectCost under a session ID, then restoring with that
// same ID, yields the original cost (COST-02).
func TestCostSaveRestoreRoundTrip(t *testing.T) {
	dir := t.TempDir()
	opts := costtrack.Options{ProjectsDir: dir, CWD: "/fake/project"}
	want := costtrack.ProjectCost{
		LastCost:              0.0042,
		LastSessionID:         "sess-abc",
		LastTotalInputTokens:  100,
		LastTotalOutputTokens: 50,
	}
	if err := costtrack.Save(opts, want); err != nil {
		t.Fatalf("Save: %v", err)
	}
	got, ok, err := costtrack.Restore(opts, "sess-abc")
	if err != nil {
		t.Fatalf("Restore: %v", err)
	}
	if !ok {
		t.Fatal("Restore returned ok=false; expected cost to be found")
	}
	if got.LastCost != want.LastCost {
		t.Errorf("LastCost: got %v, want %v", got.LastCost, want.LastCost)
	}
	if got.LastSessionID != want.LastSessionID {
		t.Errorf("LastSessionID: got %v, want %v", got.LastSessionID, want.LastSessionID)
	}
	if got.LastTotalInputTokens != want.LastTotalInputTokens {
		t.Errorf("LastTotalInputTokens: got %v, want %v", got.LastTotalInputTokens, want.LastTotalInputTokens)
	}
}

// TestCostRestoreSessionMismatch verifies that Restore returns ok=false when
// the stored session ID differs from the requested ID.
func TestCostRestoreSessionMismatch(t *testing.T) {
	dir := t.TempDir()
	opts := costtrack.Options{ProjectsDir: dir, CWD: "/fake/project"}
	stored := costtrack.ProjectCost{LastSessionID: "sess-A", LastCost: 1.0}
	if err := costtrack.Save(opts, stored); err != nil {
		t.Fatalf("Save: %v", err)
	}
	_, ok, err := costtrack.Restore(opts, "sess-B")
	if err != nil {
		t.Fatalf("Restore: %v", err)
	}
	if ok {
		t.Error("Restore should return ok=false for mismatched session ID")
	}
}

// TestHeadlessRunnerSavesCostAfterPrint verifies that running --print creates a
// cost.json file in the project config directory (COST-02 wiring).
func TestHeadlessRunnerSavesCostAfterPrint(t *testing.T) {
	configDir := t.TempDir()
	projectDir := t.TempDir()

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("content-type", "application/json")
		_, _ = w.Write([]byte(`{"id":"msg_1","type":"message","role":"assistant","model":"claude-haiku-4-5-20251001","content":[{"type":"text","text":"hi"}],"stop_reason":"end_turn","usage":{"input_tokens":5,"output_tokens":3}}`))
	}))
	defer server.Close()

	t.Setenv("ANTHROPIC_API_KEY", "test-key")
	t.Setenv("ANTHROPIC_BASE_URL", server.URL)
	t.Setenv("ANTHROPIC_MODEL", "")
	t.Setenv("CLAUDE_MODEL", "")
	t.Setenv("ANTHROPIC_BETA", "")
	t.Setenv("CLAUDE_CONFIG_DIR", configDir)

	var stdout, stderr bytes.Buffer
	code := run([]string{"--print", "--cwd", projectDir, "hello"}, strings.NewReader(""), &stdout, &stderr)
	if code != 0 {
		t.Fatalf("run exit %d; stderr=%s", code, stderr.String())
	}

	// After running, a cost.json file must exist for this project.
	// Walk configDir/projects to find any cost.json file — the exact path uses
	// platform.SanitizeProjectPath which calls filepath.Clean (on macOS, /var
	// resolves to /private/var via symlink) so we avoid hard-coding the path.
	projectsDir := filepath.Join(configDir, "projects")
	var costFile string
	_ = filepath.Walk(projectsDir, func(p string, fi os.FileInfo, err error) error {
		if err != nil {
			return nil
		}
		if fi.Name() == "cost.json" {
			costFile = p
		}
		return nil
	})
	if costFile == "" {
		// List everything for a clear failure message.
		var allFiles []string
		_ = filepath.Walk(configDir, func(p string, _ os.FileInfo, err error) error {
			if err == nil {
				allFiles = append(allFiles, p)
			}
			return nil
		})
		t.Fatalf("cost.json not created after --print run; projectDir=%s\nfiles in configDir: %v",
			projectDir, allFiles)
	}
	var raw map[string]any
	data, err := os.ReadFile(costFile)
	if err != nil {
		t.Fatalf("read cost.json: %v", err)
	}
	if err := json.Unmarshal(data, &raw); err != nil {
		t.Fatalf("parse cost.json: %v; content=%s", err, string(data))
	}
	if _, ok := raw["lastSessionId"]; !ok {
		t.Errorf("cost.json missing lastSessionId field; content=%s", string(data))
	}
}

// ─── Rewind production wiring ─────────────────────────────────────────────────

// TestHeadlessRunnerPopulatesRewindFields verifies that headlessRunner builds a
// Runner with non-nil ReadState, RewindWriter, and RewindStore (REWIND-01).
func TestHeadlessRunnerPopulatesRewindFields(t *testing.T) {
	configDir := t.TempDir()
	projectDir := t.TempDir()

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("content-type", "application/json")
		_, _ = w.Write([]byte(`{"id":"msg_1","type":"message","role":"assistant","model":"claude-haiku-4-5-20251001","content":[{"type":"text","text":"ok"}],"stop_reason":"end_turn","usage":{"input_tokens":1,"output_tokens":1}}`))
	}))
	defer server.Close()

	t.Setenv("ANTHROPIC_API_KEY", "test-key")
	t.Setenv("ANTHROPIC_BASE_URL", server.URL)
	t.Setenv("ANTHROPIC_MODEL", "")
	t.Setenv("CLAUDE_MODEL", "")
	t.Setenv("ANTHROPIC_BETA", "")
	t.Setenv("CLAUDE_CONFIG_DIR", configDir)

	state, err := bootstrap.New()
	if err != nil {
		t.Fatalf("bootstrap.New: %v", err)
	}
	state.SetCWD(projectDir)

	runner, err := headlessRunner(context.Background(), state, cliOptions{
		PermissionMode: "default",
	})
	if err != nil {
		t.Fatalf("headlessRunner: %v", err)
	}
	if runner.ReadState == nil {
		t.Error("ReadState must be non-nil for rewind to work (REWIND-01)")
	}
	if runner.RewindWriter == nil {
		t.Error("RewindWriter must be non-nil for rewind snapshots to record (REWIND-01)")
	}
	if runner.RewindStore == nil {
		t.Error("RewindStore must be non-nil for rewind snapshots to record (REWIND-01)")
	}
}

// TestRewindStoreAndWriterRoundTrip verifies the rewind Store→Writer pipeline
// can capture a snapshot and record it to a transcript (REWIND-01 seam test).
func TestRewindStoreAndWriterRoundTrip(t *testing.T) {
	dir := t.TempDir()
	store := rewind.NewStore(dir)

	tracked := filepath.Join(dir, "hello.txt")
	if err := os.WriteFile(tracked, []byte("content"), 0o600); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}

	snap, err := store.Capture(contracts.ID("msg-1"), []string{tracked}, time.Now().UTC())
	if err != nil {
		t.Fatalf("Capture: %v", err)
	}
	if len(snap.TrackedFileBackups) == 0 {
		t.Error("expected non-empty TrackedFileBackups")
	}

	transcriptPath := filepath.Join(dir, "sess.jsonl")
	w := rewind.Writer{TranscriptPath: transcriptPath}
	if err := w.Record(snap, false); err != nil {
		t.Fatalf("Record: %v", err)
	}
	data, err := os.ReadFile(transcriptPath)
	if err != nil {
		t.Fatalf("ReadFile: %v", err)
	}
	if !strings.Contains(string(data), "file-history-snapshot") {
		t.Errorf("transcript missing snapshot type: %s", string(data))
	}
}

// ─── W-C12: SDK stream-json CLI entry ────────────────────────────────────────

// TestSDKStreamJSONRoutesPrintToSDKQuery verifies that passing
// --print --input-format stream-json --output-format stream-json with a valid
// user event on stdin routes through sdk.Query and emits NDJSON to stdout
// (CLI-SDK-01).
func TestSDKStreamJSONRoutesPrintToSDKQuery(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("content-type", "application/json")
		_, _ = w.Write([]byte(`{"id":"msg_sdk1","type":"message","role":"assistant","model":"claude-haiku-4-5-20251001","content":[{"type":"text","text":"sdk-hello"}],"stop_reason":"end_turn","usage":{"input_tokens":2,"output_tokens":3}}`))
	}))
	defer server.Close()

	configDir := t.TempDir()
	t.Setenv("ANTHROPIC_API_KEY", "test-key")
	t.Setenv("ANTHROPIC_BASE_URL", server.URL)
	t.Setenv("ANTHROPIC_MODEL", "")
	t.Setenv("CLAUDE_MODEL", "")
	t.Setenv("ANTHROPIC_BETA", "")
	t.Setenv("CLAUDE_CONFIG_DIR", configDir)

	// CC stream-json input: a user event with a message.
	inputMsg := map[string]any{
		"type":    "user",
		"message": map[string]any{"role": "user", "content": "hello from sdk"},
	}
	inputBytes, _ := json.Marshal(inputMsg)

	var stdout, stderr bytes.Buffer
	code := run(
		[]string{"--print", "--input-format", "stream-json", "--output-format", "stream-json"},
		strings.NewReader(string(inputBytes)+"\n"),
		&stdout,
		&stderr,
	)
	if code != 0 {
		t.Fatalf("exit=%d stdout=%q stderr=%q", code, stdout.String(), stderr.String())
	}
	out := stdout.String()
	// SDK path emits NDJSON lines — each non-empty line must be valid JSON.
	if !isNDJSONOutput(out) {
		t.Fatalf("expected NDJSON output from SDK path; got %q", out)
	}
}

// TestSDKStreamJSONEmptyInputFails verifies that empty stream-json stdin exits
// non-zero (CLI-SDK-01 pre-condition: prompt required).
func TestSDKStreamJSONEmptyInputFails(t *testing.T) {
	configDir := t.TempDir()
	t.Setenv("ANTHROPIC_API_KEY", "test-key")
	t.Setenv("ANTHROPIC_BASE_URL", "http://localhost:0") // unreachable
	t.Setenv("ANTHROPIC_MODEL", "")
	t.Setenv("CLAUDE_MODEL", "")
	t.Setenv("ANTHROPIC_BETA", "")
	t.Setenv("CLAUDE_CONFIG_DIR", configDir)

	var stdout, stderr bytes.Buffer
	code := run(
		[]string{"--print", "--input-format", "stream-json", "--output-format", "stream-json"},
		strings.NewReader(""),
		&stdout,
		&stderr,
	)
	if code == 0 {
		t.Fatalf("expected non-zero exit for empty stream-json input; stdout=%q stderr=%q", stdout.String(), stderr.String())
	}
}

// isNDJSONOutput returns true when every non-empty line in s is valid JSON.
func isNDJSONOutput(s string) bool {
	lines := strings.Split(strings.TrimSpace(s), "\n")
	hasAny := false
	for _, line := range lines {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		if !json.Valid([]byte(line)) {
			return false
		}
		hasAny = true
	}
	return hasAny
}

// ─── W-C17: MCP remote auth + reconnect seam ─────────────────────────────────

// TestMCPConfigToolOptionsHasCombinedProvider verifies that
// LoadMCPConfigFromSettingsFiles populates ToolOptions.AccessTokenProvider
// with a combined provider that supports first-time OAuth (MCP-39..44).
func TestMCPConfigToolOptionsHasCombinedProvider(t *testing.T) {
	configDir := t.TempDir()
	t.Setenv("CLAUDE_CONFIG_DIR", configDir)

	cfg, err := conversation.LoadMCPConfigFromSettingsFiles(t.TempDir())
	if err != nil {
		t.Fatalf("LoadMCPConfigFromSettingsFiles: %v", err)
	}
	if cfg.ToolOptions.AccessTokenProvider == nil {
		t.Error("ToolOptions.AccessTokenProvider must be non-nil (MCP-39/40/41)")
	}
}

// TestShouldReconnectTransport verifies that shouldReconnect (wrapping
// reconnect.ShouldReconnect) correctly categorises remote vs. local transports
// (MCP-43).
func TestShouldReconnectTransport(t *testing.T) {
	cases := []struct {
		transport string
		want      bool
	}{
		{"http", true},
		{"sse", true},
		{"ws", true},
		{"sse-ide", true},
		{"ws-ide", true},
		{"claudeai-proxy", true},
		{"stdio", false},
		{"sdk", false},
		{"", false},
	}
	for _, tc := range cases {
		t.Run(tc.transport, func(t *testing.T) {
			got := reconnect.ShouldReconnect(tc.transport)
			if got != tc.want {
				t.Errorf("ShouldReconnect(%q) = %v, want %v", tc.transport, got, tc.want)
			}
		})
	}
}

// ─── Cost restore on --resume path ───────────────────────────────────────────

// TestCostRestoredOnResume verifies that when --resume is used and the stored
// cost session ID matches the resumed session, cost data is accessible.
func TestCostRestoredOnResume(t *testing.T) {
	dir := t.TempDir()
	sid := contracts.ID("sess-resume-001")
	opts := costtrack.Options{ProjectsDir: dir, CWD: "/tmp/proj"}
	stored := costtrack.ProjectCost{
		LastCost:              0.0099,
		LastSessionID:         sid,
		LastTotalInputTokens:  200,
		LastTotalOutputTokens: 100,
	}
	if err := costtrack.Save(opts, stored); err != nil {
		t.Fatalf("Save: %v", err)
	}
	// Simulate resume: Restore with the matching session ID.
	got, ok, err := costtrack.Restore(opts, sid)
	if err != nil {
		t.Fatalf("Restore: %v", err)
	}
	if !ok {
		t.Fatal("expected ok=true for matching session ID")
	}
	if got.LastCost != stored.LastCost {
		t.Errorf("cost mismatch: got %v, want %v", got.LastCost, stored.LastCost)
	}
	if got.LastTotalInputTokens != stored.LastTotalInputTokens {
		t.Errorf("input tokens mismatch: got %v, want %v", got.LastTotalInputTokens, stored.LastTotalInputTokens)
	}
}
