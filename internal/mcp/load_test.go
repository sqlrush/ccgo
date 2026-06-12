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
	for _, name := range []string{"mcp__chain-only__ping", "mcp__shared-global__ping", "mcp__shared-project__ping"} {
		if _, ok := registry.Lookup(name); !ok {
			t.Fatalf("missing %q in %#v", name, registry.Names())
		}
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
