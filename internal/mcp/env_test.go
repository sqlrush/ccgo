package mcp

import (
	"reflect"
	"testing"

	"ccgo/internal/contracts"
)

func TestExpandEnvVarsInStringSupportsVariablesAndDefaults(t *testing.T) {
	env := map[string]string{
		"HOST": "api.example.com",
	}
	result := ExpandEnvVarsInStringWithMap("https://${HOST}/${MISSING:-mcp}/${UNSET}", env)

	if result.Expanded != "https://api.example.com/mcp/${UNSET}" {
		t.Fatalf("expanded = %q", result.Expanded)
	}
	if want := []string{"UNSET"}; !reflect.DeepEqual(result.MissingVars, want) {
		t.Fatalf("missing = %#v, want %#v", result.MissingVars, want)
	}
}

func TestExpandServerEnvVarsExpandsStdioFields(t *testing.T) {
	server := contracts.MCPServer{
		Command: "${NODE}",
		Args:    []string{"${SCRIPT:-server.js}", "${TOKEN}"},
		Env: map[string]string{
			"API_TOKEN": "${TOKEN}",
			"OPTIONAL":  "${MISSING}",
		},
	}
	env := map[string]string{
		"NODE":  "node",
		"TOKEN": "secret",
	}

	result := ExpandServerEnvVarsWithMap(server, env)
	if result.Expanded.Command != "node" {
		t.Fatalf("command = %q", result.Expanded.Command)
	}
	if want := []string{"server.js", "secret"}; !reflect.DeepEqual(result.Expanded.Args, want) {
		t.Fatalf("args = %#v, want %#v", result.Expanded.Args, want)
	}
	if result.Expanded.Env["API_TOKEN"] != "secret" || result.Expanded.Env["OPTIONAL"] != "${MISSING}" {
		t.Fatalf("env = %#v", result.Expanded.Env)
	}
	if want := []string{"MISSING"}; !reflect.DeepEqual(result.MissingVars, want) {
		t.Fatalf("missing = %#v, want %#v", result.MissingVars, want)
	}
	if server.Command != "${NODE}" || server.Args[0] != "${SCRIPT:-server.js}" {
		t.Fatalf("input server was mutated: %#v", server)
	}
}

func TestExpandServerEnvVarsExpandsRemoteURLAndHeaders(t *testing.T) {
	server := contracts.MCPServer{
		Type: TransportHTTP,
		URL:  "https://${HOST}/mcp",
		Headers: map[string]string{
			"Authorization": "Bearer ${TOKEN}",
		},
	}
	env := map[string]string{
		"HOST":  "api.example.com",
		"TOKEN": "secret",
	}

	result := ExpandServerEnvVarsWithMap(server, env)
	if result.Expanded.URL != "https://api.example.com/mcp" {
		t.Fatalf("url = %q", result.Expanded.URL)
	}
	if got := result.Expanded.Headers["Authorization"]; got != "Bearer secret" {
		t.Fatalf("authorization = %q", got)
	}
	if len(result.MissingVars) != 0 {
		t.Fatalf("missing = %#v", result.MissingVars)
	}
}

func TestExpandServerEnvVarsLeavesSDKAndProxyFieldsUnchanged(t *testing.T) {
	env := map[string]string{"HOST": "api.example.com"}
	for _, server := range []contracts.MCPServer{
		{Type: TransportSDK, Name: "${HOST}", URL: "https://${HOST}/mcp"},
		{Type: TransportClaudeAIProxy, URL: "https://${HOST}/mcp"},
		{Type: TransportSSEIDE, URL: "https://${HOST}/mcp"},
	} {
		result := ExpandServerEnvVarsWithMap(server, env)
		if !reflect.DeepEqual(result.Expanded, server) {
			t.Fatalf("server changed: got %#v want %#v", result.Expanded, server)
		}
		if len(result.MissingVars) != 0 {
			t.Fatalf("missing = %#v", result.MissingVars)
		}
	}
}
