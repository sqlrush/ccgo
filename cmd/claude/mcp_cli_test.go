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
	data, _ := json.MarshalIndent(doc, "", "  ")
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
