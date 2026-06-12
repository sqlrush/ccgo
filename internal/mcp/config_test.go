package mcp

import (
	"reflect"
	"testing"

	"ccgo/internal/contracts"
)

func TestServerSignatureForStdioAndURL(t *testing.T) {
	stdio := contracts.MCPServer{
		Command: "node",
		Args:    []string{"server.js"},
	}
	if got, ok := ServerSignature(stdio); !ok || got != `stdio:["node","server.js"]` {
		t.Fatalf("stdio signature = %q, %v", got, ok)
	}

	http := contracts.MCPServer{
		Type: TransportHTTP,
		URL:  "https://example.com/mcp",
	}
	if got, ok := ServerSignature(http); !ok || got != "url:https://example.com/mcp" {
		t.Fatalf("url signature = %q, %v", got, ok)
	}

	sdk := contracts.MCPServer{Type: TransportSDK, Name: "local-sdk"}
	if got, ok := ServerSignature(sdk); ok || got != "" {
		t.Fatalf("sdk signature = %q, %v", got, ok)
	}
}

func TestServerCommandArraySkipsNonStdioTransports(t *testing.T) {
	server := contracts.MCPServer{
		Type:    TransportHTTP,
		Command: "node",
		Args:    []string{"server.js"},
		URL:     "https://example.com/mcp",
	}
	if got, ok := ServerCommandArray(server); ok || got != nil {
		t.Fatalf("command array = %#v, %v", got, ok)
	}
}

func TestUnwrapCCRProxyURL(t *testing.T) {
	wrapped := "https://ccr.example/v2/session_ingress/shttp/mcp/server?mcp_url=https%3A%2F%2Fvendor.example%2Fmcp"
	if got := UnwrapCCRProxyURL(wrapped); got != "https://vendor.example/mcp" {
		t.Fatalf("wrapped url = %q", got)
	}

	plain := "https://vendor.example/mcp"
	if got := UnwrapCCRProxyURL(plain); got != plain {
		t.Fatalf("plain url = %q", got)
	}

	invalid := "://not a url"
	if got := UnwrapCCRProxyURL(invalid); got != invalid {
		t.Fatalf("invalid url = %q", got)
	}
}

func TestDedupPluginServersSuppressesManualAndEarlierPluginDuplicates(t *testing.T) {
	manual := map[string]contracts.MCPServer{
		"manual:slack": {
			Type: TransportHTTP,
			URL:  "https://vendor.example/mcp",
		},
	}
	plugin := map[string]contracts.MCPServer{
		"plugin:slack": {
			Type: TransportHTTP,
			URL:  "https://ccr.example/v2/ccr-sessions/session?mcp_url=https%3A%2F%2Fvendor.example%2Fmcp",
		},
		"plugin:first": {
			Command: "node",
			Args:    []string{"server.js"},
		},
		"plugin:second": {
			Type:    TransportStdio,
			Command: "node",
			Args:    []string{"server.js"},
		},
		"plugin:sdk": {
			Type: TransportSDK,
			Name: "sdk-server",
		},
	}

	got := DedupPluginServers(plugin, manual)
	if _, ok := got.Servers["plugin:slack"]; ok {
		t.Fatalf("manual duplicate was kept: %#v", got.Servers)
	}
	if _, ok := got.Servers["plugin:second"]; ok {
		t.Fatalf("plugin duplicate was kept: %#v", got.Servers)
	}
	if _, ok := got.Servers["plugin:first"]; !ok {
		t.Fatalf("first plugin server was suppressed: %#v", got.Servers)
	}
	if _, ok := got.Servers["plugin:sdk"]; !ok {
		t.Fatalf("unsigned sdk server was suppressed: %#v", got.Servers)
	}

	wantSuppressed := []SuppressedServer{
		{Name: "plugin:second", DuplicateOf: "plugin:first"},
		{Name: "plugin:slack", DuplicateOf: "manual:slack"},
	}
	if !reflect.DeepEqual(got.Suppressed, wantSuppressed) {
		t.Fatalf("suppressed = %#v, want %#v", got.Suppressed, wantSuppressed)
	}
}
