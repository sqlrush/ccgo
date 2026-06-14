package conversation

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"ccgo/internal/auth"
	"ccgo/internal/contracts"
	"ccgo/internal/mcp"
)

func TestLoadMCPConfigFromSettingsFiles(t *testing.T) {
	root := t.TempDir()
	claudeHome := filepath.Join(root, "home")
	project := filepath.Join(root, "project")
	if err := os.MkdirAll(filepath.Join(project, ".claude"), 0o755); err != nil {
		t.Fatal(err)
	}
	pluginDir := filepath.Join(project, ".claude", "plugins", "demo")
	if err := os.MkdirAll(pluginDir, 0o755); err != nil {
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
	writeSettingsFile(t, filepath.Join(pluginDir, "plugin.json"), `{
		"name": "demo",
		"mcpServers": {
			"plugin:docs": {"type": "http", "url": "https://example.com/mcp"}
		}
	}`)
	store := auth.NewFileCredentialStore(mcp.DefaultMCPServerCredentialsPath("remote"))
	if err := store.Save(context.Background(), auth.Credentials{
		Source:      auth.SourceOAuth,
		AccessToken: "cached",
	}); err != nil {
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
	if config.UserSettings.MCPServers["user"].Command != "user-server" {
		t.Fatalf("user settings = %#v", config.UserSettings.MCPServers)
	}
	if config.ProjectSettings.MCPServers["project"].Command != "project-server" {
		t.Fatalf("project settings = %#v", config.ProjectSettings.MCPServers)
	}
	if config.LocalSettings.MCPServers["local"].Command != "local-server" {
		t.Fatalf("local settings = %#v", config.LocalSettings.MCPServers)
	}
	if config.PluginServers["plugin:docs"].URL != "https://example.com/mcp" || config.PluginServers["plugin:docs"].PluginSource != "demo" {
		t.Fatalf("plugin servers = %#v", config.PluginServers)
	}
	if config.ToolOptions.AccessTokenProvider == nil {
		t.Fatal("missing default MCP OAuth access token provider")
	}
	tokenProvider, err := config.ToolOptions.AccessTokenProvider(context.Background(), "remote", contracts.MCPServer{
		OAuth: &contracts.MCPOAuthConfig{ClientID: "client"},
	})
	if err != nil {
		t.Fatal(err)
	}
	token, err := tokenProvider.CurrentAccessToken(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if token != "cached" {
		t.Fatalf("token = %q", token)
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

func TestLoadMCPConfigFromSettingsFilesSkipsDisabledPluginServers(t *testing.T) {
	root := t.TempDir()
	claudeHome := filepath.Join(root, "home")
	project := filepath.Join(root, "project")
	pluginDir := filepath.Join(project, ".claude", "plugins", "demo")
	if err := os.MkdirAll(pluginDir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(claudeHome, 0o755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("CLAUDE_CONFIG_DIR", claudeHome)
	writeSettingsFile(t, filepath.Join(project, ".claude", "settings.json"), `{
		"enabledPlugins": {"demo": false}
	}`)
	writeSettingsFile(t, filepath.Join(pluginDir, "plugin.json"), `{
		"name": "demo",
		"mcpServers": {
			"plugin:docs": {"type": "http", "url": "https://example.com/mcp"}
		}
	}`)

	config, err := LoadMCPConfigFromSettingsFiles(project)
	if err != nil {
		t.Fatal(err)
	}
	if len(config.PluginServers) != 0 {
		t.Fatalf("plugin servers = %#v", config.PluginServers)
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
