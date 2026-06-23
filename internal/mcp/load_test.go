package mcp

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"ccgo/internal/contracts"
)

func TestLoadSettingsServersParsesScopedMCPServers(t *testing.T) {
	settings := contracts.Settings{
		MCPServers: map[string]contracts.MCPServer{
			"user": {
				Type: TransportHTTP,
				URL:  "https://${HOST}/mcp",
			},
		},
	}

	result, err := LoadSettingsServers(settings, ScopeUser, ParseOptions{
		ExpandVars: true,
		UseEnvMap:  true,
		Env:        map[string]string{"HOST": "api.example.com"},
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(result.Errors) != 0 {
		t.Fatalf("errors = %#v", result.Errors)
	}
	if got := result.Servers["user"]; got.URL != "https://api.example.com/mcp" || got.Scope != ScopeUser {
		t.Fatalf("server = %#v", got)
	}
}

func TestMergeManualConfigSourcesUsesUserProjectLocalPrecedenceAndPolicy(t *testing.T) {
	user := AddScopeToServers(map[string]contracts.MCPServer{
		"shared":  {Command: "user"},
		"allowed": {Command: "node", Args: []string{"server.js"}},
	}, ScopeUser)
	project := AddScopeToServers(map[string]contracts.MCPServer{
		"shared": {Command: "project"},
	}, ScopeProject)
	local := AddScopeToServers(map[string]contracts.MCPServer{
		"shared": {Command: "local"},
		"blocked": {
			Type: TransportHTTP,
			URL:  "https://blocked.example/mcp",
		},
	}, ScopeLocal)

	result := MergeManualConfigSources(ManualConfigSources{
		User:    user,
		Project: project,
		Local:   local,
		Policy: Policy{
			AllowlistSet: true,
			Allowed: []contracts.MCPServerPolicyEntry{
				{ServerCommand: []string{"local"}},
				{ServerCommand: []string{"node", "server.js"}},
			},
		},
	})

	if got := result.Servers["shared"]; got.Command != "local" || got.Scope != ScopeLocal {
		t.Fatalf("shared = %#v", got)
	}
	if got := result.Servers["allowed"]; got.Command != "node" || got.Scope != ScopeUser {
		t.Fatalf("allowed = %#v", got)
	}
	if _, ok := result.Servers["blocked"]; ok {
		t.Fatalf("blocked server kept: %#v", result.Servers)
	}
	if len(result.Blocked) != 1 || result.Blocked[0] != "blocked" {
		t.Fatalf("blocked = %#v", result.Blocked)
	}
}

func TestMergeManualConfigSourcesAddsPluginServersAfterDedupAndPolicy(t *testing.T) {
	user := AddScopeToServers(map[string]contracts.MCPServer{
		"manual": {Command: "node", Args: []string{"manual.js"}},
		"same-name": {
			Type: TransportHTTP,
			URL:  "https://manual.example/mcp",
		},
	}, ScopeUser)
	plugin := map[string]contracts.MCPServer{
		"plugin:unique": {
			Command:      "python",
			Args:         []string{"plugin.py"},
			PluginSource: "demo",
		},
		"plugin:duplicate": {
			Command:      "node",
			Args:         []string{"manual.js"},
			PluginSource: "demo",
		},
		"same-name": {
			Type:         TransportHTTP,
			URL:          "https://plugin.example/mcp",
			PluginSource: "demo",
		},
		"plugin:blocked": {
			Type:         TransportHTTP,
			URL:          "https://blocked.example/mcp",
			PluginSource: "demo",
		},
	}

	result := MergeManualConfigSources(ManualConfigSources{
		User:   user,
		Plugin: plugin,
		Policy: Policy{
			Denied: []contracts.MCPServerPolicyEntry{{ServerURL: "https://blocked.example/*"}},
		},
	})

	if _, ok := result.Servers["plugin:unique"]; !ok {
		t.Fatalf("unique plugin server missing: %#v", result.Servers)
	}
	if _, ok := result.Servers["plugin:duplicate"]; ok {
		t.Fatalf("duplicate plugin server kept: %#v", result.Servers)
	}
	if got := result.Servers["same-name"]; got.URL != "https://manual.example/mcp" {
		t.Fatalf("same-name = %#v", got)
	}
	if _, ok := result.Servers["plugin:blocked"]; ok {
		t.Fatalf("blocked plugin server kept: %#v", result.Servers)
	}
	if len(result.Blocked) != 1 || result.Blocked[0] != "plugin:blocked" {
		t.Fatalf("blocked = %#v", result.Blocked)
	}
}

func TestLoadProjectConfigChainMergesFromRootToCWD(t *testing.T) {
	root := t.TempDir()
	parent := filepath.Join(root, "project")
	child := filepath.Join(parent, "sub")
	if err := os.MkdirAll(child, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(parent, ".mcp.json"), []byte(`{
		"mcpServers": {
			"shared": {"command": "parent"},
			"parent-only": {"command": "parent-only"}
		}
	}`), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(child, ".mcp.json"), []byte(`{
		"mcpServers": {
			"shared": {"command": "child"},
			"child-only": {"type": "http", "url": "https://child.example/mcp"}
		}
	}`), 0o644); err != nil {
		t.Fatal(err)
	}

	result, err := LoadProjectConfigChain(child, ParseOptions{})
	if err != nil {
		t.Fatal(err)
	}
	if len(result.Errors) != 0 {
		t.Fatalf("errors = %#v", result.Errors)
	}
	if got := result.Servers["shared"]; got.Command != "child" || got.Scope != ScopeProject {
		t.Fatalf("shared = %#v", got)
	}
	if got := result.Servers["parent-only"]; got.Command != "parent-only" || got.Scope != ScopeProject {
		t.Fatalf("parent-only = %#v", got)
	}
	if got := result.Servers["child-only"]; got.URL != "https://child.example/mcp" || got.Scope != ScopeProject {
		t.Fatalf("child-only = %#v", got)
	}
}

func TestLoadProjectConfigChainSkipsMissingFilesButKeepsMalformedErrors(t *testing.T) {
	root := t.TempDir()
	parent := filepath.Join(root, "project")
	child := filepath.Join(parent, "sub")
	if err := os.MkdirAll(child, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(parent, ".mcp.json"), []byte(`{
		"mcpServers": {
			"bad": {"type": "http"}
		}
	}`), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(child, ".mcp.json"), []byte(`{
		"mcpServers": {
			"good": {"command": "node"}
		}
	}`), 0o644); err != nil {
		t.Fatal(err)
	}

	result, err := LoadProjectConfigChain(child, ParseOptions{})
	if err != nil {
		t.Fatal(err)
	}
	if got := result.Servers["good"]; got.Command != "node" {
		t.Fatalf("good = %#v", got)
	}
	if _, ok := result.Servers["bad"]; ok {
		t.Fatalf("bad server should not be loaded: %#v", result.Servers)
	}
	if len(result.Errors) != 1 || result.Errors[0].ServerName != "bad" || result.Errors[0].Severity != "fatal" {
		t.Fatalf("errors = %#v", result.Errors)
	}
}

func TestLoadProjectConfigChainExpandsEnvWarnings(t *testing.T) {
	root := t.TempDir()
	if err := os.WriteFile(filepath.Join(root, ".mcp.json"), []byte(`{
		"mcpServers": {
			"env": {"command": "${NODE}", "args": ["${MISSING}"]}
		}
	}`), 0o644); err != nil {
		t.Fatal(err)
	}

	result, err := LoadProjectConfigChain(root, ParseOptions{
		ExpandVars: true,
		UseEnvMap:  true,
		Env:        map[string]string{"NODE": "node"},
	})
	if err != nil {
		t.Fatal(err)
	}
	if got := result.Servers["env"]; got.Command != "node" {
		t.Fatalf("env server = %#v", got)
	}
	if len(result.Errors) != 1 || result.Errors[0].Severity != "warning" || result.Errors[0].ServerName != "env" {
		t.Fatalf("errors = %#v", result.Errors)
	}
}

func TestBuildConfiguredToolSetsMergesSourcesAndBuildsToolsets(t *testing.T) {
	root := t.TempDir()
	if err := os.WriteFile(filepath.Join(root, ".mcp.json"), []byte(`{
		"mcpServers": {
			"shared-project": {"command": "project-chain"},
			"chain-only": {"command": "chain"}
		}
	}`), 0o644); err != nil {
		t.Fatal(err)
	}

	result, err := BuildConfiguredToolSets(context.Background(), ConfiguredToolSetOptions{
		UserSettings: contracts.Settings{
			MCPServers: map[string]contracts.MCPServer{
				"user-only":      {Command: "user"},
				"shared-global":  {Command: "user-shared"},
				"shared-project": {Command: "user-project"},
			},
			AllowedMCPServers: []contracts.MCPServerPolicyEntry{
				{ServerCommand: []string{"local"}},
				{ServerCommand: []string{"project-chain"}},
				{ServerCommand: []string{"chain"}},
				{ServerCommand: []string{"plugin"}},
			},
		},
		ProjectSettings: contracts.Settings{
			MCPServers: map[string]contracts.MCPServer{
				"shared-project": {Command: "project-settings"},
			},
		},
		LocalSettings: contracts.Settings{
			MCPServers: map[string]contracts.MCPServer{
				"shared-global": {Command: "local"},
			},
		},
		PluginServers: map[string]contracts.MCPServer{
			"plugin-only": {Command: "plugin", PluginSource: "demo"},
		},
		CWD: root,
		ToolOptions: ServerToolOptions{
			DisableResources: true,
			DisablePrompts:   true,
			OpenClient: func(_ context.Context, name string, _ contracts.MCPServer) (ClientHandle, error) {
				return ClientHandle{Client: &fakeMCPClient{tools: []RemoteTool{{Name: "ping", ReadOnly: true}}}}, nil
			},
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(result.LoadErrors) != 0 {
		t.Fatalf("load errors = %#v", result.LoadErrors)
	}
	if got := result.Servers["shared-global"]; got.Command != "local" || got.Scope != ScopeLocal {
		t.Fatalf("shared global = %#v", got)
	}
	if got := result.Servers["shared-project"]; got.Command != "project-chain" || got.Scope != ScopeProject {
		t.Fatalf("shared project = %#v", got)
	}
	if got := result.Servers["chain-only"]; got.Command != "chain" || got.Scope != ScopeProject {
		t.Fatalf("chain only = %#v", got)
	}
	if got := result.Servers["plugin-only"]; got.Command != "plugin" || got.PluginSource != "demo" {
		t.Fatalf("plugin only = %#v", got)
	}
	if _, ok := result.Servers["user-only"]; ok {
		t.Fatalf("blocked user-only kept: %#v", result.Servers)
	}
	if len(result.Blocked) != 1 || result.Blocked[0] != "user-only" {
		t.Fatalf("blocked = %#v", result.Blocked)
	}
	registry, err := result.ToolSets.Registry()
	if err != nil {
		t.Fatal(err)
	}
	for _, name := range []string{"mcp__chain-only__ping", "mcp__plugin-only__ping", "mcp__shared-global__ping", "mcp__shared-project__ping"} {
		if _, ok := registry.Lookup(name); !ok {
			t.Fatalf("missing %q in %#v", name, registry.Names())
		}
	}
}

func TestBuildConfiguredToolSetsUsesPolicySettings(t *testing.T) {
	result, err := BuildConfiguredToolSets(context.Background(), ConfiguredToolSetOptions{
		UserSettings: contracts.Settings{
			MCPServers: map[string]contracts.MCPServer{
				"allowed": {Command: "allowed-server"},
				"blocked": {Command: "blocked-server"},
			},
		},
		PolicySettings: contracts.Settings{
			AllowedMCPServers: []contracts.MCPServerPolicyEntry{{ServerName: "allowed"}},
		},
		ToolOptions: ServerToolOptions{
			DisableResources: true,
			DisablePrompts:   true,
			OpenClient: func(_ context.Context, _ string, _ contracts.MCPServer) (ClientHandle, error) {
				return ClientHandle{Client: &fakeMCPClient{}}, nil
			},
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	if _, ok := result.Servers["allowed"]; !ok {
		t.Fatalf("allowed server missing: %#v", result.Servers)
	}
	if _, ok := result.Servers["blocked"]; ok {
		t.Fatalf("blocked server kept: %#v", result.Servers)
	}
	if len(result.Blocked) != 1 || result.Blocked[0] != "blocked" {
		t.Fatalf("blocked = %#v", result.Blocked)
	}
}

func TestBuildConfiguredToolSetsHonorsStrictPluginOnlyMCP(t *testing.T) {
	root := t.TempDir()
	if err := os.WriteFile(filepath.Join(root, ".mcp.json"), []byte(`{
		"mcpServers": {"chain": {"command": "chain"}}
	}`), 0o644); err != nil {
		t.Fatal(err)
	}
	result, err := BuildConfiguredToolSets(context.Background(), ConfiguredToolSetOptions{
		UserSettings: contracts.Settings{
			MCPServers:        map[string]contracts.MCPServer{"user": {Command: "user"}},
			AllowedMCPServers: []contracts.MCPServerPolicyEntry{},
		},
		ProjectSettings: contracts.Settings{
			MCPServers: map[string]contracts.MCPServer{"project": {Command: "project"}},
		},
		LocalSettings: contracts.Settings{
			MCPServers: map[string]contracts.MCPServer{"local": {Command: "local"}},
		},
		PolicySettings: contracts.Settings{StrictPluginOnlyCustomization: []any{"mcp"}},
		PluginServers: map[string]contracts.MCPServer{
			"plugin": {Command: "plugin", PluginSource: "demo"},
		},
		CWD: root,
		ToolOptions: ServerToolOptions{
			DisableResources: true,
			DisablePrompts:   true,
			OpenClient: func(_ context.Context, _ string, _ contracts.MCPServer) (ClientHandle, error) {
				return ClientHandle{Client: &fakeMCPClient{}}, nil
			},
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(result.Servers) != 1 {
		t.Fatalf("servers = %#v", result.Servers)
	}
	if got := result.Servers["plugin"]; got.Command != "plugin" || got.PluginSource != "demo" {
		t.Fatalf("plugin server = %#v", got)
	}
	for _, name := range []string{"user", "project", "local", "chain"} {
		if _, ok := result.Servers[name]; ok {
			t.Fatalf("%s server should be blocked by plugin-only policy: %#v", name, result.Servers)
		}
	}
	if len(result.Blocked) != 0 {
		t.Fatalf("plugin-only skipped servers should not appear as policy-blocked: %#v", result.Blocked)
	}
}

func TestBuildConfiguredToolSetsAdvertisesCWDRoots(t *testing.T) {
	root := t.TempDir()
	var initializeParams string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var raw RPCRequest
		if err := json.NewDecoder(r.Body).Decode(&raw); err != nil {
			t.Fatal(err)
		}
		switch raw.Method {
		case "initialize":
			initializeParams = mustJSON(t, raw.Params)
			_, _ = w.Write([]byte(`{"jsonrpc":"2.0","id":"` + raw.ID + `","result":{"protocolVersion":"2025-06-18","capabilities":{"tools":{}}}}`))
		case "notifications/initialized":
			w.WriteHeader(http.StatusAccepted)
		case "tools/list":
			_, _ = w.Write([]byte(`{"jsonrpc":"2.0","id":"` + raw.ID + `","result":{"tools":[]}}`))
		default:
			t.Fatalf("method = %s", raw.Method)
		}
	}))
	defer server.Close()

	result, err := BuildConfiguredToolSets(context.Background(), ConfiguredToolSetOptions{
		UserSettings: contracts.Settings{
			MCPServers: map[string]contracts.MCPServer{
				"remote": {Type: TransportHTTP, URL: server.URL},
			},
		},
		CWD: root,
		ToolOptions: ServerToolOptions{
			DisableResources: true,
			DisablePrompts:   true,
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(result.ToolSets.Errors) != 0 {
		t.Fatalf("toolset errors = %#v", result.ToolSets.Errors)
	}
	if !strings.Contains(initializeParams, `"roots":{}`) {
		t.Fatalf("initialize params missing cwd roots capability: %s", initializeParams)
	}
}

func TestBuildConfiguredToolSetsPreservesEmptyAllowlist(t *testing.T) {
	result, err := BuildConfiguredToolSets(context.Background(), ConfiguredToolSetOptions{
		UserSettings: contracts.Settings{
			MCPServers: map[string]contracts.MCPServer{
				"blocked": {Command: "node"},
			},
			AllowedMCPServers: []contracts.MCPServerPolicyEntry{},
		},
		ToolOptions: ServerToolOptions{
			OpenClient: func(_ context.Context, name string, _ contracts.MCPServer) (ClientHandle, error) {
				t.Fatalf("blocked server %q should not be opened", name)
				return ClientHandle{}, nil
			},
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(result.Servers) != 0 {
		t.Fatalf("servers = %#v", result.Servers)
	}
	if len(result.Blocked) != 1 || result.Blocked[0] != "blocked" {
		t.Fatalf("blocked = %#v", result.Blocked)
	}
	if len(result.ToolSets.Tools) != 0 {
		t.Fatalf("toolsets = %#v", result.ToolSets)
	}
}

func TestBuildConfiguredToolSetsReturnsProjectConfigErrors(t *testing.T) {
	root := t.TempDir()
	if err := os.WriteFile(filepath.Join(root, ".mcp.json"), []byte(`{
		"mcpServers": {
			"bad": {"type": "http"}
		}
	}`), 0o644); err != nil {
		t.Fatal(err)
	}

	result, err := BuildConfiguredToolSets(context.Background(), ConfiguredToolSetOptions{
		CWD: root,
		ToolOptions: ServerToolOptions{
			DisableResources: true,
			DisablePrompts:   true,
			OpenClient: func(_ context.Context, name string, _ contracts.MCPServer) (ClientHandle, error) {
				return ClientHandle{Client: &fakeMCPClient{tools: []RemoteTool{{Name: "ping", ReadOnly: true}}}}, nil
			},
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	if _, ok := result.Servers["bad"]; ok {
		t.Fatalf("bad server kept: %#v", result.Servers)
	}
	if len(result.LoadErrors) != 1 || result.LoadErrors[0].ServerName != "bad" {
		t.Fatalf("load errors = %#v", result.LoadErrors)
	}
	if len(result.ToolSets.Tools) != 0 {
		t.Fatalf("toolsets = %#v", result.ToolSets)
	}
}

// TestDoesEnterpriseMCPConfigExistAbsent verifies missing path returns false (MCP-27).
func TestDoesEnterpriseMCPConfigExistAbsent(t *testing.T) {
	dir := t.TempDir()
	if DoesEnterpriseMCPConfigExist(filepath.Join(dir, "nonexistent.json")) {
		t.Fatal("expected false for nonexistent file")
	}
}

// TestDoesEnterpriseMCPConfigExistPresent verifies existing file returns true (MCP-27).
func TestDoesEnterpriseMCPConfigExistPresent(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "managed-mcp.json")
	if err := os.WriteFile(path, []byte(`{"mcpServers":{}}`), 0o600); err != nil {
		t.Fatal(err)
	}
	if !DoesEnterpriseMCPConfigExist(path) {
		t.Fatal("expected true for existing file")
	}
}

// TestLoadEnterpriseMCPConfigMissingReturnsEmpty verifies missing file is
// silently handled (MCP-27).
func TestLoadEnterpriseMCPConfigMissingReturnsEmpty(t *testing.T) {
	dir := t.TempDir()
	result, err := LoadEnterpriseMCPConfig(filepath.Join(dir, "missing.json"))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(result.Servers) != 0 {
		t.Fatalf("expected empty servers, got %v", result.Servers)
	}
}

// TestLoadEnterpriseMCPConfigLoadsServers verifies managed-mcp.json servers
// are loaded with enterprise scope (MCP-27).
func TestLoadEnterpriseMCPConfigLoadsServers(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "managed-mcp.json")
	if err := os.WriteFile(path, []byte(`{"mcpServers":{"corp-tools":{"command":"corp-mcp","args":[]}}}`), 0o600); err != nil {
		t.Fatal(err)
	}
	result, err := LoadEnterpriseMCPConfig(path)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	srv, ok := result.Servers["corp-tools"]
	if !ok {
		t.Fatalf("expected corp-tools server, got %v", result.Servers)
	}
	if srv.Scope != ScopeEnterprise {
		t.Fatalf("scope = %q, want enterprise", srv.Scope)
	}
}
