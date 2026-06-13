package bootstrap

import (
	"os"
	"path/filepath"
	"testing"
)

func TestStateFeature(t *testing.T) {
	state, err := New()
	if err != nil {
		t.Fatal(err)
	}
	state.SetFeature("BUDDY", true)
	if !state.Feature("BUDDY") {
		t.Fatal("feature not enabled")
	}
	if state.SessionID() == "" || state.CWD() == "" {
		t.Fatalf("state missing ids: %#v", state)
	}
}

func TestStateConversationRunnerLoadsMCPSettings(t *testing.T) {
	root := t.TempDir()
	claudeHome := filepath.Join(root, "home")
	project := filepath.Join(root, "project")
	if err := os.MkdirAll(claudeHome, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Join(project, ".claude"), 0o700); err != nil {
		t.Fatal(err)
	}
	t.Setenv("CLAUDE_CONFIG_DIR", claudeHome)
	if err := os.WriteFile(filepath.Join(claudeHome, "settings.json"), []byte(`{"mcpServers":{"user":{"command":"user-server"}}}`), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(project, ".claude", "settings.json"), []byte(`{"mcpServers":{"project":{"command":"project-server"}}}`), 0o600); err != nil {
		t.Fatal(err)
	}

	state, err := New()
	if err != nil {
		t.Fatal(err)
	}
	state.SetCWD(project)
	runner, err := state.ConversationRunner()
	if err != nil {
		t.Fatal(err)
	}
	if runner.SessionID != state.SessionID() || runner.WorkingDirectory != project {
		t.Fatalf("runner identity = %#v state=%#v", runner, state)
	}
	if runner.MCP == nil {
		t.Fatal("missing MCP config")
	}
	if got := runner.MCP.UserSettings.MCPServers["user"].Command; got != "user-server" {
		t.Fatalf("user server command = %q", got)
	}
	if got := runner.MCP.ProjectSettings.MCPServers["project"].Command; got != "project-server" {
		t.Fatalf("project server command = %q", got)
	}
	if runner.MCP.ToolOptions.AccessTokenProvider == nil {
		t.Fatal("missing default MCP OAuth provider")
	}
}
