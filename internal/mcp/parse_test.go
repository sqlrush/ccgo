package mcp

import (
	"os"
	"path/filepath"
	"reflect"
	"testing"
)

func TestParseConfigJSONParsesAndScopesSupportedServers(t *testing.T) {
	data := []byte(`{
		"mcpServers": {
			"stdio": {
				"command": "node",
				"args": ["server.js"],
				"env": {"TOKEN": "secret"}
			},
			"http": {
				"type": "http",
				"url": "https://example.com/mcp",
				"headers": {"Authorization": "Bearer token"},
				"headersHelper": "helper",
				"oauth": {"clientId": "client", "callbackPort": 3999, "xaa": true}
			},
			"ide": {
				"type": "ws-ide",
				"url": "ws://localhost/mcp",
				"ideName": "vscode",
				"authToken": "token",
				"ideRunningInWindows": true
			},
			"sdk": {
				"type": "sdk",
				"name": "claude-vscode"
			},
			"proxy": {
				"type": "claudeai-proxy",
				"url": "https://proxy.example/mcp",
				"id": "srv_123"
			}
		}
	}`)

	result, err := ParseConfigJSON(data, ParseOptions{Scope: ScopeProject, FilePath: ".mcp.json"})
	if err != nil {
		t.Fatal(err)
	}
	if result.Config == nil {
		t.Fatalf("config is nil with errors %#v", result.Errors)
	}
	if len(result.Errors) != 0 {
		t.Fatalf("errors = %#v", result.Errors)
	}

	stdio := result.Config.MCPServers["stdio"]
	if stdio.Scope != ScopeProject || stdio.Command != "node" || !reflect.DeepEqual(stdio.Args, []string{"server.js"}) {
		t.Fatalf("stdio = %#v", stdio)
	}
	http := result.Config.MCPServers["http"]
	if http.HeadersHelper != "helper" || http.OAuth == nil || http.OAuth.ClientID != "client" {
		t.Fatalf("http = %#v", http)
	}
	if http.OAuth.CallbackPort == nil || *http.OAuth.CallbackPort != 3999 {
		t.Fatalf("oauth callback = %#v", http.OAuth)
	}
	ide := result.Config.MCPServers["ide"]
	if ide.IDEName != "vscode" || ide.IDERunningInWindows == nil || !*ide.IDERunningInWindows {
		t.Fatalf("ide = %#v", ide)
	}
	proxy := result.Config.MCPServers["proxy"]
	if proxy.ID != "srv_123" {
		t.Fatalf("proxy = %#v", proxy)
	}
}

func TestParseConfigJSONExpandsEnvAndReturnsWarnings(t *testing.T) {
	data := []byte(`{
		"mcpServers": {
			"stdio": {
				"command": "${NODE}",
				"args": ["${SCRIPT:-server.js}", "${MISSING}"]
			},
			"remote": {
				"type": "http",
				"url": "https://${HOST}/mcp",
				"headers": {"Authorization": "Bearer ${TOKEN}"}
			}
		}
	}`)

	result, err := ParseConfigJSON(data, ParseOptions{
		ExpandVars: true,
		Scope:      ScopeUser,
		UseEnvMap:  true,
		Env: map[string]string{
			"NODE":  "node",
			"HOST":  "api.example.com",
			"TOKEN": "secret",
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	if result.Config == nil {
		t.Fatalf("config is nil with errors %#v", result.Errors)
	}
	if got := result.Config.MCPServers["stdio"].Command; got != "node" {
		t.Fatalf("command = %q", got)
	}
	if got := result.Config.MCPServers["remote"].URL; got != "https://api.example.com/mcp" {
		t.Fatalf("url = %q", got)
	}
	if len(result.Errors) != 1 || result.Errors[0].Severity != "warning" || result.Errors[0].ServerName != "stdio" {
		t.Fatalf("errors = %#v", result.Errors)
	}
}

func TestParseConfigJSONReturnsFatalErrorsForInvalidConfig(t *testing.T) {
	result, err := ParseConfigJSON([]byte(`{
		"mcpServers": {
			"bad-stdio": {"args": ["server.js"]},
			"bad-http": {"type": "http", "headers": {"X": "Y"}},
			"bad-type": {"type": "smtp", "url": "smtp://example.com"}
		}
	}`), ParseOptions{Scope: ScopeLocal, FilePath: ".mcp.json"})
	if err != nil {
		t.Fatal(err)
	}
	if result.Config != nil {
		t.Fatalf("config = %#v, want nil", result.Config)
	}
	if len(result.Errors) != 3 {
		t.Fatalf("errors = %#v", result.Errors)
	}
	for _, err := range result.Errors {
		if err.Severity != "fatal" || err.File != ".mcp.json" || err.Scope != ScopeLocal {
			t.Fatalf("error = %#v", err)
		}
	}
}

func TestParseConfigJSONRejectsMissingTopLevelMCPServers(t *testing.T) {
	result, err := ParseConfigJSON([]byte(`{"servers": {}}`), ParseOptions{})
	if err != nil {
		t.Fatal(err)
	}
	if result.Config != nil || len(result.Errors) != 1 || result.Errors[0].Path != "mcpServers" {
		t.Fatalf("result = %#v", result)
	}
}

func TestParseConfigFileReadsFileAndReportsMissingFile(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, ".mcp.json")
	if err := os.WriteFile(path, []byte(`{"mcpServers":{"stdio":{"command":"node"}}}`), 0o644); err != nil {
		t.Fatal(err)
	}

	result, err := ParseConfigFile(path, ParseOptions{Scope: ScopeProject})
	if err != nil {
		t.Fatal(err)
	}
	if result.Config == nil || result.Config.MCPServers["stdio"].Command != "node" {
		t.Fatalf("result = %#v", result)
	}
	if result.Config.MCPServers["stdio"].Scope != ScopeProject {
		t.Fatalf("scope = %q", result.Config.MCPServers["stdio"].Scope)
	}

	missing, err := ParseConfigFile(filepath.Join(dir, "missing.json"), ParseOptions{Scope: ScopeProject})
	if err != nil {
		t.Fatal(err)
	}
	if missing.Config != nil || len(missing.Errors) != 1 || missing.Errors[0].Message != "MCP config file not found" {
		t.Fatalf("missing = %#v", missing)
	}
}
