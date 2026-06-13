package conversation

import (
	"os"
	"path/filepath"
	"testing"
)

func TestLoadMCPConfigFromSettingsFiles(t *testing.T) {
	root := t.TempDir()
	claudeHome := filepath.Join(root, "home")
	project := filepath.Join(root, "project")
	if err := os.MkdirAll(filepath.Join(project, ".claude"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(claudeHome, 0o755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("CLAUDE_CONFIG_DIR", claudeHome)
	writeSettingsFile(t, filepath.Join(claudeHome, "settings.json"), `{
		"mcpServers": {
			"user": {"command": "user-server"}
		}
	}`)
	writeSettingsFile(t, filepath.Join(project, ".claude", "settings.json"), `{
		"mcpServers": {
			"project": {"command": "project-server"}
		}
	}`)
	writeSettingsFile(t, filepath.Join(project, ".claude", "settings.local.json"), `{
		"mcpServers": {
			"local": {"command": "local-server"}
		}
	}`)

	config, err := LoadMCPConfigFromSettingsFiles(project)
	if err != nil {
		t.Fatal(err)
	}
	wantCWD := resolvedTestPath(t, project)
	if config.CWD != wantCWD {
		t.Fatalf("cwd = %q", config.CWD)
	}
	if config.UserSettings.MCPServers["user"].Command != "user-server" {
		t.Fatalf("user settings = %#v", config.UserSettings.MCPServers)
	}
	if config.ProjectSettings.MCPServers["project"].Command != "project-server" {
		t.Fatalf("project settings = %#v", config.ProjectSettings.MCPServers)
	}
	if config.LocalSettings.MCPServers["local"].Command != "local-server" {
		t.Fatalf("local settings = %#v", config.LocalSettings.MCPServers)
	}
}

func TestLoadMCPConfigFromSettingsFilesIgnoresMissingFiles(t *testing.T) {
	root := t.TempDir()
	t.Setenv("CLAUDE_CONFIG_DIR", filepath.Join(root, "missing-home"))
	project := filepath.Join(root, "project")
	if err := os.MkdirAll(project, 0o755); err != nil {
		t.Fatal(err)
	}

	config, err := LoadMCPConfigFromSettingsFiles(project)
	if err != nil {
		t.Fatal(err)
	}
	wantCWD := resolvedTestPath(t, project)
	if config.CWD != wantCWD {
		t.Fatalf("cwd = %q", config.CWD)
	}
	if len(config.UserSettings.MCPServers) != 0 || len(config.ProjectSettings.MCPServers) != 0 || len(config.LocalSettings.MCPServers) != 0 {
		t.Fatalf("settings = %#v %#v %#v", config.UserSettings.MCPServers, config.ProjectSettings.MCPServers, config.LocalSettings.MCPServers)
	}
}

func writeSettingsFile(t *testing.T, path string, content string) {
	t.Helper()
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
}

func resolvedTestPath(t *testing.T, path string) string {
	t.Helper()
	resolved, err := filepath.EvalSymlinks(path)
	if err != nil {
		t.Fatal(err)
	}
	return resolved
}
