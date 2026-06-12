package mcp

import (
	"reflect"
	"testing"

	"ccgo/internal/contracts"
)

func TestEvaluatePolicyAllowlistUnsetAllowsUnlessDenied(t *testing.T) {
	server := contracts.MCPServer{Command: "node", Args: []string{"server.js"}}

	decision := EvaluatePolicy("local", server, Policy{})
	if !decision.Allowed || decision.Reason != "allowlist-unset" {
		t.Fatalf("decision = %#v", decision)
	}

	decision = EvaluatePolicy("local", server, Policy{
		Denied: []contracts.MCPServerPolicyEntry{{ServerName: "local"}},
	})
	if decision.Allowed || decision.Reason != "denied" {
		t.Fatalf("denied decision = %#v", decision)
	}
}

func TestEvaluatePolicyEmptyAllowlistBlocksNonSDKServers(t *testing.T) {
	policy := Policy{AllowlistSet: true}

	remote := contracts.MCPServer{Type: TransportHTTP, URL: "https://example.com/mcp"}
	if decision := EvaluatePolicy("remote", remote, policy); decision.Allowed || decision.Reason != "allowlist-empty" {
		t.Fatalf("remote decision = %#v", decision)
	}

	sdk := contracts.MCPServer{Type: TransportSDK, Name: "claude-vscode"}
	if decision := EvaluatePolicy("sdk", sdk, policy); !decision.Allowed || decision.Reason != "sdk-exempt" {
		t.Fatalf("sdk decision = %#v", decision)
	}
}

func TestEvaluatePolicyDenyTakesPrecedenceOverAllow(t *testing.T) {
	server := contracts.MCPServer{Command: "node", Args: []string{"server.js"}}
	policy := Policy{
		AllowlistSet: true,
		Allowed: []contracts.MCPServerPolicyEntry{
			{ServerCommand: []string{"node", "server.js"}},
		},
		Denied: []contracts.MCPServerPolicyEntry{
			{ServerName: "local"},
		},
	}

	if decision := EvaluatePolicy("local", server, policy); decision.Allowed || decision.Reason != "denied" {
		t.Fatalf("decision = %#v", decision)
	}
}

func TestEvaluatePolicyCommandEntriesGateStdioServers(t *testing.T) {
	policy := Policy{
		AllowlistSet: true,
		Allowed: []contracts.MCPServerPolicyEntry{
			{ServerName: "local"},
			{ServerCommand: []string{"node", "allowed.js"}},
		},
	}

	allowed := contracts.MCPServer{Command: "node", Args: []string{"allowed.js"}}
	if decision := EvaluatePolicy("anything", allowed, policy); !decision.Allowed {
		t.Fatalf("allowed command decision = %#v", decision)
	}

	blocked := contracts.MCPServer{Command: "node", Args: []string{"blocked.js"}}
	if decision := EvaluatePolicy("local", blocked, policy); decision.Allowed {
		t.Fatalf("command entries should ignore name fallback: %#v", decision)
	}
}

func TestEvaluatePolicyURLEntriesGateRemoteServers(t *testing.T) {
	policy := Policy{
		AllowlistSet: true,
		Allowed: []contracts.MCPServerPolicyEntry{
			{ServerName: "remote"},
			{ServerURL: "https://*.example.com/*"},
		},
	}

	allowed := contracts.MCPServer{Type: TransportHTTP, URL: "https://api.example.com/mcp"}
	if decision := EvaluatePolicy("anything", allowed, policy); !decision.Allowed {
		t.Fatalf("allowed url decision = %#v", decision)
	}

	blocked := contracts.MCPServer{Type: TransportSSE, URL: "https://evil.test/mcp"}
	if decision := EvaluatePolicy("remote", blocked, policy); decision.Allowed {
		t.Fatalf("url entries should ignore name fallback: %#v", decision)
	}
}

func TestEvaluatePolicyFallsBackToNameWhenNoCommandOrURLEntries(t *testing.T) {
	policy := Policy{
		AllowlistSet: true,
		Allowed: []contracts.MCPServerPolicyEntry{
			{ServerName: "named"},
		},
	}

	server := contracts.MCPServer{Type: TransportHTTP, URL: "https://example.com/mcp"}
	if decision := EvaluatePolicy("named", server, policy); !decision.Allowed {
		t.Fatalf("named decision = %#v", decision)
	}
	if decision := EvaluatePolicy("other", server, policy); decision.Allowed {
		t.Fatalf("other decision = %#v", decision)
	}
}

func TestFilterServersByPolicyReturnsAllowedAndBlockedNames(t *testing.T) {
	servers := map[string]contracts.MCPServer{
		"allowed": {Command: "node", Args: []string{"server.js"}},
		"blocked": {Type: TransportHTTP, URL: "https://blocked.example/mcp"},
		"sdk":     {Type: TransportSDK, Name: "claude-vscode"},
	}
	policy := Policy{
		AllowlistSet: true,
		Allowed: []contracts.MCPServerPolicyEntry{
			{ServerCommand: []string{"node", "server.js"}},
		},
	}

	allowed, blocked := FilterServersByPolicy(servers, policy)
	if _, ok := allowed["allowed"]; !ok {
		t.Fatalf("allowed server missing: %#v", allowed)
	}
	if _, ok := allowed["sdk"]; !ok {
		t.Fatalf("sdk server missing: %#v", allowed)
	}
	if _, ok := allowed["blocked"]; ok {
		t.Fatalf("blocked server kept: %#v", allowed)
	}
	if want := []string{"blocked"}; !reflect.DeepEqual(blocked, want) {
		t.Fatalf("blocked = %#v, want %#v", blocked, want)
	}
}

func TestPolicyFromSettingsPreservesAllowlistSet(t *testing.T) {
	unset := PolicyFromSettings(contracts.Settings{})
	if unset.AllowlistSet {
		t.Fatalf("unset allowlist marked set: %#v", unset)
	}

	empty := PolicyFromSettings(contracts.Settings{AllowedMCPServers: []contracts.MCPServerPolicyEntry{}})
	if !empty.AllowlistSet || len(empty.Allowed) != 0 {
		t.Fatalf("empty allowlist policy = %#v", empty)
	}
}
