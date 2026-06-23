package main

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func writeSettings(t *testing.T, path string, servers map[string]any) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	doc := map[string]any{"mcpServers": servers}
	data, err := json.MarshalIndent(doc, "", "  ")
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, data, 0o600); err != nil {
		t.Fatal(err)
	}
}

func newMCPTestEnv(t *testing.T) mcpCLIEnv {
	t.Helper()
	dir := t.TempDir()
	return mcpCLIEnv{
		UserPath:    filepath.Join(dir, "user-settings.json"),
		ProjectRoot: dir,
	}
}

func TestMCPListShowsServers(t *testing.T) {
	env := newMCPTestEnv(t)
	writeSettings(t, env.UserPath, map[string]any{
		"local-fs": map[string]any{"command": "npx", "args": []any{"server-fs"}},
		"remote-x": map[string]any{"type": "http", "url": "https://x.example/mcp"},
	})
	var out, errb bytes.Buffer
	if code := runMCPCommand([]string{"list"}, &out, &errb, env); code != 0 {
		t.Fatalf("list exit=%d stderr=%q", code, errb.String())
	}
	got := out.String()
	if !strings.Contains(got, "local-fs") || !strings.Contains(got, "stdio") {
		t.Fatalf("list missing local-fs/stdio: %q", got)
	}
	if !strings.Contains(got, "remote-x") || !strings.Contains(got, "https://x.example/mcp") {
		t.Fatalf("list missing remote-x url: %q", got)
	}
}

func TestMCPGetUnknownServerErrors(t *testing.T) {
	env := newMCPTestEnv(t)
	writeSettings(t, env.UserPath, map[string]any{})
	var out, errb bytes.Buffer
	if code := runMCPCommand([]string{"get", "nope"}, &out, &errb, env); code == 0 {
		t.Fatal("expected nonzero exit for unknown server")
	}
	if !strings.Contains(errb.String(), "nope") {
		t.Fatalf("error should name the server: %q", errb.String())
	}
}

func TestMCPMissingSubcommand(t *testing.T) {
	env := newMCPTestEnv(t)
	var out, errb bytes.Buffer
	if code := runMCPCommand(nil, &out, &errb, env); code == 0 {
		t.Fatal("expected nonzero exit for missing subcommand")
	}
	if !strings.Contains(errb.String(), "Usage") {
		t.Fatalf("expected usage text: %q", errb.String())
	}
}

func TestMCPListIncludesMCPJSONServers(t *testing.T) {
	env := newMCPTestEnv(t)
	// Write a server only in .mcp.json, not in settings.json
	mcpJSONPath := filepath.Join(env.ProjectRoot, ".mcp.json")
	doc := map[string]any{
		"mcpServers": map[string]any{
			"mcp-json-server": map[string]any{
				"type":    "http",
				"url":     "https://mcp.example/api",
				"headers": map[string]string{"Authorization": "Bearer secret123"},
			},
		},
	}
	data, err := json.MarshalIndent(doc, "", "  ")
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(mcpJSONPath, data, 0o600); err != nil {
		t.Fatal(err)
	}

	var out, errb bytes.Buffer
	if code := runMCPCommand([]string{"list"}, &out, &errb, env); code != 0 {
		t.Fatalf("list exit=%d stderr=%q", code, errb.String())
	}
	got := out.String()
	if !strings.Contains(got, "mcp-json-server") {
		t.Fatalf("list should show .mcp.json server, got: %q", got)
	}
	if !strings.Contains(got, "https://mcp.example/api") {
		t.Fatalf("list should show .mcp.json server URL, got: %q", got)
	}
}

func TestMCPGetRedactsHeaders(t *testing.T) {
	env := newMCPTestEnv(t)
	// Write a server with headers to settings
	writeSettings(t, env.UserPath, map[string]any{
		"http-with-headers": map[string]any{
			"type": "http",
			"url":  "https://api.example/mcp",
			"headers": map[string]string{
				"Authorization": "Bearer secret123",
				"X-API-Key":     "key456",
			},
		},
	})

	var out, errb bytes.Buffer
	if code := runMCPCommand([]string{"get", "http-with-headers"}, &out, &errb, env); code != 0 {
		t.Fatalf("get exit=%d stderr=%q", code, errb.String())
	}
	got := out.String()

	// Check that headers count is shown but values are redacted
	if !strings.Contains(got, "2 header(s) [redacted]") {
		t.Fatalf("should show header count and redaction, got: %q", got)
	}
	// Make sure actual secrets are NOT in output
	if strings.Contains(got, "secret123") || strings.Contains(got, "key456") {
		t.Fatalf("output should not contain secret values: %q", got)
	}
}

// TestMCPAddBlockedByEnterpriseConfig verifies that mcp add is rejected when
// managed-mcp.json is present (MCP-27).
// CC ref: src/services/mcp/config.ts:650-653.
func TestMCPAddBlockedByEnterpriseConfig(t *testing.T) {
	env := newMCPTestEnv(t)
	// Write a managed-mcp.json to simulate enterprise control.
	entPath := filepath.Join(t.TempDir(), "managed-mcp.json")
	if err := os.WriteFile(entPath, []byte(`{"mcpServers":{}}`), 0o600); err != nil {
		t.Fatal(err)
	}
	env.EnterpriseMCPPath = entPath

	var out, errb bytes.Buffer
	code := runMCPCommand([]string{"add", "myserver", "/usr/bin/mcp"}, &out, &errb, env)
	if code == 0 {
		t.Fatal("expected non-zero exit when enterprise MCP config is active")
	}
	if !strings.Contains(errb.String(), "enterprise") {
		t.Fatalf("expected enterprise error message, got: %q", errb.String())
	}
}

// TestMCPAddAllowedWithoutEnterpriseConfig verifies that mcp add succeeds
// when managed-mcp.json is absent (MCP-27 negative path).
func TestMCPAddAllowedWithoutEnterpriseConfig(t *testing.T) {
	env := newMCPTestEnv(t)
	env.EnterpriseMCPPath = filepath.Join(t.TempDir(), "absent-managed-mcp.json")

	var out, errb bytes.Buffer
	code := runMCPCommand([]string{"add", "myserver", "/usr/bin/mcp", "-s", "local"}, &out, &errb, env)
	if code != 0 {
		t.Fatalf("expected zero exit, got %d stderr=%q", code, errb.String())
	}
	if !strings.Contains(out.String(), "myserver") {
		t.Fatalf("expected success output containing server name: %q", out.String())
	}
}

// TestMCPListEnterpriseModeShowsOnlyEnterpriseServers verifies that when
// managed-mcp.json exists, `mcp list` returns only enterprise servers and
// omits user/project servers (MCP-27).
// CC ref: src/services/mcp/config.ts:1083.
func TestMCPListEnterpriseModeShowsOnlyEnterpriseServers(t *testing.T) {
	env := newMCPTestEnv(t)

	// Write user-level server (should be hidden in enterprise mode).
	writeSettings(t, env.UserPath, map[string]any{
		"user-server": map[string]any{"command": "npx", "args": []any{"user-mcp"}},
	})

	// Write enterprise managed-mcp.json with a single server.
	entPath := filepath.Join(t.TempDir(), "managed-mcp.json")
	entData, err := json.Marshal(map[string]any{
		"mcpServers": map[string]any{
			"enterprise-server": map[string]any{"command": "ent-mcp", "args": []any{"--stdio"}},
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(entPath, entData, 0o600); err != nil {
		t.Fatal(err)
	}
	env.EnterpriseMCPPath = entPath

	var out, errb bytes.Buffer
	if code := runMCPCommand([]string{"list"}, &out, &errb, env); code != 0 {
		t.Fatalf("list exit=%d stderr=%q", code, errb.String())
	}
	got := out.String()
	if !strings.Contains(got, "enterprise-server") {
		t.Fatalf("enterprise server missing from list: %q", got)
	}
	if strings.Contains(got, "user-server") {
		t.Fatalf("user server should be hidden in enterprise mode: %q", got)
	}
	// Scope label should be "enterprise".
	if !strings.Contains(got, "enterprise") {
		t.Fatalf("expected enterprise scope label in output: %q", got)
	}
}

// TestMCPListNoEnterpriseFallsBackToAllServers verifies that when no
// managed-mcp.json exists, all configured user/project servers are shown (MCP-27 negative path).
func TestMCPListNoEnterpriseFallsBackToAllServers(t *testing.T) {
	env := newMCPTestEnv(t)
	env.EnterpriseMCPPath = filepath.Join(t.TempDir(), "absent-managed-mcp.json")

	writeSettings(t, env.UserPath, map[string]any{
		"user-server": map[string]any{"command": "npx", "args": []any{"user-mcp"}},
	})

	var out, errb bytes.Buffer
	if code := runMCPCommand([]string{"list"}, &out, &errb, env); code != 0 {
		t.Fatalf("list exit=%d stderr=%q", code, errb.String())
	}
	if !strings.Contains(out.String(), "user-server") {
		t.Fatalf("user server should appear when no enterprise config: %q", out.String())
	}
}
