package main

import (
	"bytes"
	"path/filepath"
	"testing"

	"ccgo/internal/config"
)

func TestMCPAddJSON(t *testing.T) {
	dir := t.TempDir()
	env := mcpCLIEnv{UserPath: filepath.Join(dir, "user.json"), ProjectRoot: dir}
	var out, errb bytes.Buffer
	js := `{"type":"http","url":"https://e.example/mcp","oauth":{"clientId":"abc"}}`
	if code := runMCPCommand([]string{"add-json", "rj", js, "--scope", "user"}, &out, &errb, env); code != 0 {
		t.Fatalf("add-json exit=%d stderr=%q", code, errb.String())
	}
	settings, _ := config.LoadSettingsFile(env.UserPath)
	got, ok := settings.MCPServers["rj"]
	if !ok || got.URL != "https://e.example/mcp" || got.OAuth == nil || got.OAuth.ClientID != "abc" {
		t.Fatalf("add-json not persisted correctly: %+v", got)
	}
}

func TestMCPAddJSONRejectsInvalid(t *testing.T) {
	dir := t.TempDir()
	env := mcpCLIEnv{UserPath: filepath.Join(dir, "user.json"), ProjectRoot: dir}
	var out, errb bytes.Buffer
	if code := runMCPCommand([]string{"add-json", "bad", "{not json"}, &out, &errb, env); code == 0 {
		t.Fatal("expected nonzero exit for invalid JSON")
	}
}

func TestMCPRemoveFindsScope(t *testing.T) {
	dir := t.TempDir()
	userPath := filepath.Join(dir, "user.json")
	writeSettings(t, userPath, map[string]any{"gone": map[string]any{"command": "x"}})
	env := mcpCLIEnv{UserPath: userPath, ProjectRoot: dir}
	var out, errb bytes.Buffer
	if code := runMCPCommand([]string{"remove", "gone"}, &out, &errb, env); code != 0 {
		t.Fatalf("remove exit=%d stderr=%q", code, errb.String())
	}
	settings, _ := config.LoadSettingsFile(userPath)
	if _, ok := settings.MCPServers["gone"]; ok {
		t.Fatal("server not removed")
	}
}

func TestMCPRemoveUnknownErrors(t *testing.T) {
	dir := t.TempDir()
	env := mcpCLIEnv{UserPath: filepath.Join(dir, "user.json"), ProjectRoot: dir}
	writeSettings(t, env.UserPath, map[string]any{})
	var out, errb bytes.Buffer
	if code := runMCPCommand([]string{"remove", "ghost"}, &out, &errb, env); code == 0 {
		t.Fatal("expected nonzero exit removing unknown server")
	}
}

// TestMCPRemovePreservesOtherServers verifies that removing one server
// does not clobber other servers in the same settings file.
func TestMCPRemovePreservesOtherServers(t *testing.T) {
	dir := t.TempDir()
	userPath := filepath.Join(dir, "user.json")
	writeSettings(t, userPath, map[string]any{
		"keep":   map[string]any{"command": "keep-cmd"},
		"remove": map[string]any{"command": "remove-cmd"},
	})
	env := mcpCLIEnv{UserPath: userPath, ProjectRoot: dir}
	var out, errb bytes.Buffer
	if code := runMCPCommand([]string{"remove", "remove", "--scope", "user"}, &out, &errb, env); code != 0 {
		t.Fatalf("remove exit=%d stderr=%q", code, errb.String())
	}
	settings, _ := config.LoadSettingsFile(userPath)
	if _, ok := settings.MCPServers["remove"]; ok {
		t.Fatal("'remove' server should have been deleted")
	}
	if _, ok := settings.MCPServers["keep"]; !ok {
		t.Fatal("'keep' server was clobbered")
	}
}

// TestMCPRemoveScopeSearch verifies that without --scope, the command searches
// user→project→local and removes from whichever scope holds the server.
func TestMCPRemoveScopeSearch(t *testing.T) {
	dir := t.TempDir()
	// Put the server only in the project settings (not user)
	projectPath := config.ProjectSettingsPath(dir)
	writeSettings(t, projectPath, map[string]any{
		"proj-server": map[string]any{"command": "proj-cmd"},
	})
	env := mcpCLIEnv{UserPath: filepath.Join(dir, "user.json"), ProjectRoot: dir}
	var out, errb bytes.Buffer
	if code := runMCPCommand([]string{"remove", "proj-server"}, &out, &errb, env); code != 0 {
		t.Fatalf("remove exit=%d stderr=%q", code, errb.String())
	}
	settings, _ := config.LoadSettingsFile(projectPath)
	if _, ok := settings.MCPServers["proj-server"]; ok {
		t.Fatal("proj-server should have been removed from project scope")
	}
}
