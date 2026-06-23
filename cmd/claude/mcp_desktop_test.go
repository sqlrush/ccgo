package main

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// writeDesktopConfig creates a claude_desktop_config.json in a temp dir.
func writeDesktopConfig(t *testing.T, servers map[string]any) string {
	t.Helper()
	dir := t.TempDir()
	path := filepath.Join(dir, "claude_desktop_config.json")
	data, err := json.MarshalIndent(map[string]any{"mcpServers": servers}, "", "  ")
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, data, 0o600); err != nil {
		t.Fatal(err)
	}
	return path
}

// TestMCPAddFromClaudeDesktopImportsServers verifies that servers from
// Claude Desktop config are imported into the target scope (MCP-18).
func TestMCPAddFromClaudeDesktopImportsServers(t *testing.T) {
	desktopPath := writeDesktopConfig(t, map[string]any{
		"fs-server": map[string]any{"command": "npx", "args": []any{"-y", "@modelcontextprotocol/server-filesystem"}},
		"git-tool":  map[string]any{"command": "/usr/bin/git-mcp", "args": []any{}},
	})

	env := newMCPTestEnv(t)
	env.DesktopConfigPath = desktopPath

	var out, errb bytes.Buffer
	code := runMCPCommand([]string{"add-from-claude-desktop", "-s", "local"}, &out, &errb, env)
	if code != 0 {
		t.Fatalf("add-from-claude-desktop exit=%d stderr=%q stdout=%q", code, errb.String(), out.String())
	}
	output := out.String()
	if !strings.Contains(output, "fs-server") || !strings.Contains(output, "git-tool") {
		t.Fatalf("expected both server names in output: %q", output)
	}

	// Verify servers were written to local settings.
	var out2, errb2 bytes.Buffer
	if code2 := runMCPCommand([]string{"list"}, &out2, &errb2, env); code2 != 0 {
		t.Fatalf("list exit=%d stderr=%q", code2, errb2.String())
	}
	listed := out2.String()
	if !strings.Contains(listed, "fs-server") || !strings.Contains(listed, "git-tool") {
		t.Fatalf("imported servers not in list: %q", listed)
	}
}

// TestMCPAddFromClaudeDesktopAbsentConfig verifies graceful handling of absent
// Claude Desktop config (MCP-18).
func TestMCPAddFromClaudeDesktopAbsentConfig(t *testing.T) {
	env := newMCPTestEnv(t)
	env.DesktopConfigPath = filepath.Join(t.TempDir(), "absent.json")

	var out, errb bytes.Buffer
	code := runMCPCommand([]string{"add-from-claude-desktop"}, &out, &errb, env)
	if code != 0 {
		t.Fatalf("expected exit 0 for absent file, got %d stderr=%q stdout=%q", code, errb.String(), out.String())
	}
	if !strings.Contains(out.String(), "No MCP servers") {
		t.Fatalf("expected 'No MCP servers' message: %q", out.String())
	}
}

// TestReadClaudeDesktopServersParsesMalformed verifies that malformed entries
// are skipped and valid ones are returned (MCP-18).
func TestReadClaudeDesktopServersParsesMalformed(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "claude_desktop_config.json")
	// One valid entry, one with missing command/url (empty), one raw invalid JSON entry.
	raw := `{"mcpServers":{"valid":{"command":"mcp"},"empty":{}}}`
	if err := os.WriteFile(path, []byte(raw), 0o600); err != nil {
		t.Fatal(err)
	}
	servers, err := readClaudeDesktopServers(path)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if _, ok := servers["valid"]; !ok {
		t.Fatalf("expected 'valid' server, got %v", servers)
	}
	if _, ok := servers["empty"]; ok {
		t.Fatalf("'empty' server should be skipped, got %v", servers)
	}
}
