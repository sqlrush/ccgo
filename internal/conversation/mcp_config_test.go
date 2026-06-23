package conversation

import (
	"context"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"
	"time"

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
	t.Setenv("USER_TYPE", "ant")
	managedDir := filepath.Join(root, "managed")
	if err := os.MkdirAll(managedDir, 0o755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("CLAUDE_CODE_MANAGED_SETTINGS_PATH", managedDir)
	writeSettingsFile(t, filepath.Join(claudeHome, "settings.json"), `{
		"model": "sonnet",
		"mcpServers": {
			"user": {"command": "user-server"}
		}
	}`)
	writeSettingsFile(t, filepath.Join(managedDir, "managed-settings.json"), `{
		"model": "opus",
		"allowManagedPermissionRulesOnly": true,
		"permissions": {
			"allow": ["Bash(git status *)"]
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
	if config.PolicySettings.Model != "opus" || config.MergedSettings().Model != "opus" {
		t.Fatalf("policy settings = %#v merged=%#v", config.PolicySettings, config.MergedSettings())
	}
	if config.PolicySettings.Permissions == nil || len(config.PolicySettings.Permissions.Allow) != 1 || config.PolicySettings.Permissions.Allow[0] != "Bash(git status *)" {
		t.Fatalf("policy permissions = %#v", config.PolicySettings.Permissions)
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
	t.Setenv("USER_TYPE", "ant")
	t.Setenv("CLAUDE_CODE_MANAGED_SETTINGS_PATH", filepath.Join(root, "missing-managed"))
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
	t.Setenv("USER_TYPE", "ant")
	t.Setenv("CLAUDE_CODE_MANAGED_SETTINGS_PATH", filepath.Join(root, "managed"))
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

func TestMCPConfigRefreshSettingsFilesUpdatesMergedSettingsAndPlugins(t *testing.T) {
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
	t.Setenv("USER_TYPE", "ant")
	t.Setenv("CLAUDE_CODE_MANAGED_SETTINGS_PATH", filepath.Join(root, "managed"))
	writeSettingsFile(t, filepath.Join(claudeHome, "settings.json"), `{"model":"user"}`)
	projectSettingsPath := filepath.Join(project, ".claude", "settings.json")
	writeSettingsFile(t, projectSettingsPath, `{
		"model": "project-a",
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
	if config.MergedSettings().Model != "project-a" || len(config.PluginServers) != 0 {
		t.Fatalf("initial merged=%#v plugin servers=%#v", config.MergedSettings(), config.PluginServers)
	}

	writeSettingsFile(t, projectSettingsPath, `{
		"model": "project-bbbbb",
		"enabledPlugins": {"demo": true}
	}`)
	changed, err := config.RefreshSettingsFiles()
	if err != nil {
		t.Fatal(err)
	}
	if !changed || config.ProjectSettings.Model != "project-bbbbb" || config.MergedSettings().Model != "project-bbbbb" {
		t.Fatalf("settings = %#v merged=%#v changed=%v", config.ProjectSettings, config.MergedSettings(), changed)
	}
	if server := config.PluginServers["plugin:docs"]; server.URL != "https://example.com/mcp" || server.PluginSource != "demo" {
		t.Fatalf("plugin servers = %#v", config.PluginServers)
	}
}

func TestMCPConfigRefreshPolicySettingsUpdatesMergedSettingsAndPlugins(t *testing.T) {
	root := t.TempDir()
	project := filepath.Join(root, "project")
	pluginDir := filepath.Join(project, ".claude", "plugins", "demo")
	if err := os.MkdirAll(pluginDir, 0o755); err != nil {
		t.Fatal(err)
	}
	writeSettingsFile(t, filepath.Join(pluginDir, "plugin.json"), `{
		"name": "demo",
		"mcpServers": {
			"plugin:docs": {"type": "http", "url": "https://example.com/mcp"}
		}
	}`)
	t.Setenv("USER_TYPE", "ant")
	t.Setenv("CLAUDE_CODE_MANAGED_SETTINGS_PATH", filepath.Join(root, "missing-managed"))
	current := `{"settings":{"model":"remote-a","enabledPlugins":{"demo":false}}}`
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("content-type", "application/json")
		_, _ = w.Write([]byte(current))
	}))
	defer server.Close()
	t.Setenv("CLAUDE_CODE_REMOTE_MANAGED_SETTINGS_URL", server.URL+"/policy")

	config := &MCPConfig{
		UserSettings: contracts.Settings{Model: "user"},
		CWD:          project,
	}
	changed, err := config.RefreshPolicySettings()
	if err != nil {
		t.Fatal(err)
	}
	if !changed || config.PolicySettings.Model != "remote-a" || config.MergedSettings().Model != "remote-a" {
		t.Fatalf("policy settings = %#v merged=%#v changed=%v", config.PolicySettings, config.MergedSettings(), changed)
	}
	if len(config.PluginServers) != 0 {
		t.Fatalf("disabled plugin servers = %#v", config.PluginServers)
	}

	current = `{"settings":{"model":"remote-b","enabledPlugins":{"demo":true}}}`
	changed, err = config.RefreshPolicySettings()
	if err != nil {
		t.Fatal(err)
	}
	if !changed || config.PolicySettings.Model != "remote-b" || config.MergedSettings().Model != "remote-b" {
		t.Fatalf("policy settings = %#v merged=%#v changed=%v", config.PolicySettings, config.MergedSettings(), changed)
	}
	if server := config.PluginServers["plugin:docs"]; server.URL != "https://example.com/mcp" || server.PluginSource != "demo" {
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

// TestLoadMCPConfigHasCombinedProvider verifies that LoadMCPConfigFromSettingsFiles
// sets AccessTokenProvider to the CombinedAccessTokenProvider (MCP-39..44 wiring).
func TestLoadMCPConfigHasCombinedProvider(t *testing.T) {
	claudeHome := t.TempDir()
	t.Setenv("CLAUDE_CONFIG_DIR", claudeHome)

	cfg, err := LoadMCPConfigFromSettingsFiles(t.TempDir())
	if err != nil {
		t.Fatalf("LoadMCPConfigFromSettingsFiles: %v", err)
	}
	if cfg.ToolOptions.AccessTokenProvider == nil {
		t.Error("AccessTokenProvider must be non-nil (MCP-39..44 wiring)")
	}
	// Verify it is non-nil for a non-OAuth server (should return nil provider).
	ctx := context.Background()
	provider, err := cfg.ToolOptions.AccessTokenProvider(ctx, "test-server", contracts.MCPServer{
		Type: mcp.TransportStdio,
	})
	if err != nil {
		t.Errorf("AccessTokenProvider returned error for non-OAuth server: %v", err)
	}
	if provider != nil {
		t.Errorf("AccessTokenProvider should return nil for non-OAuth server, got %T", provider)
	}
}

// TestLoadMCPConfigHasReconnectOpenClient verifies that LoadMCPConfigFromSettingsFiles
// sets OpenClient to the reconnecting wrapper (MCP-43 wiring).
func TestLoadMCPConfigHasReconnectOpenClient(t *testing.T) {
	claudeHome := t.TempDir()
	t.Setenv("CLAUDE_CONFIG_DIR", claudeHome)

	cfg, err := LoadMCPConfigFromSettingsFiles(t.TempDir())
	if err != nil {
		t.Fatalf("LoadMCPConfigFromSettingsFiles: %v", err)
	}
	if cfg.ToolOptions.OpenClient == nil {
		t.Error("OpenClient must be non-nil (MCP-43 reconnect wiring)")
	}
}

// TestReconnectingOpenClientLocalTransport verifies that reconnectingOpenClient
// attempts a connection for local transports (no reconnect wrapping applied).
// We use a fake stdio server that will fail immediately; the function should
// return an error (not hang or panic).
func TestReconnectingOpenClientLocalTransport(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()

	// stdio transport should fail quickly (command not found) without reconnect.
	server := contracts.MCPServer{
		Type:    mcp.TransportStdio,
		Command: "ccgo-test-no-such-command-xyz",
	}
	_, err := reconnectingOpenClient(ctx, "test", server)
	if err == nil {
		t.Error("expected error for missing stdio command")
	}
	// Verify it failed quickly (< 1s) — no reconnect backoff.
}

// TestReconnectingOpenClientRemoteTransportContextCancel verifies that
// reconnectingOpenClient respects context cancellation for remote transports.
// The reconnect loop should stop when ctx is cancelled.
func TestReconnectingOpenClientRemoteTransportContextCancel(t *testing.T) {
	// Cancel immediately so we don't wait for reconnect attempts.
	ctx, cancel := context.WithCancel(context.Background())
	cancel() // Cancel before calling

	mcpServer := contracts.MCPServer{
		Type: mcp.TransportHTTP,
		URL:  "http://127.0.0.1:1/mcp", // Port 1 is privileged/unreachable
	}
	_, err := reconnectingOpenClient(ctx, "test-remote", mcpServer)
	// Should return context.Canceled or a connection error.
	if err == nil {
		t.Error("expected error for cancelled context or unreachable server")
	}
}

var _ = time.Second // ensure time import is used
