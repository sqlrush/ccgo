package mcp

import (
	"testing"

	"ccgo/internal/contracts"
)

// tests for filterProjectMCPServers (MCP-24)
// CC ref: src/services/mcpServerApproval.tsx (isMCPServerTrusted)

func makeServers(names ...string) map[string]contracts.MCPServer {
	m := make(map[string]contracts.MCPServer, len(names))
	for _, n := range names {
		m[n] = contracts.MCPServer{Name: n}
	}
	return m
}

func TestFilterProjectMCPServers_AllBlocked_WhenNoSettings(t *testing.T) {
	servers := makeServers("alpha", "beta")
	result := filterProjectMCPServers(servers, contracts.Settings{})
	if len(result) != 0 {
		t.Fatalf("expected all servers blocked when no settings, got: %v", result)
	}
}

func TestFilterProjectMCPServers_EnableAll(t *testing.T) {
	yes := true
	servers := makeServers("alpha", "beta", "gamma")
	result := filterProjectMCPServers(servers, contracts.Settings{
		EnableAllProjectMCPServers: &yes,
	})
	if len(result) != 3 {
		t.Fatalf("expected all 3 servers allowed, got %d: %v", len(result), result)
	}
}

func TestFilterProjectMCPServers_EnableAll_HonoursDisabledList(t *testing.T) {
	yes := true
	servers := makeServers("alpha", "beta", "gamma")
	result := filterProjectMCPServers(servers, contracts.Settings{
		EnableAllProjectMCPServers:  &yes,
		DisabledMCPJSONServers: []string{"beta"},
	})
	if len(result) != 2 {
		t.Fatalf("expected 2 servers, got %d: %v", len(result), result)
	}
	if _, ok := result["beta"]; ok {
		t.Fatal("beta should have been blocked by disabled list")
	}
}

func TestFilterProjectMCPServers_ExplicitAllowList(t *testing.T) {
	servers := makeServers("alpha", "beta", "gamma")
	result := filterProjectMCPServers(servers, contracts.Settings{
		EnabledMCPJSONServers: []string{"alpha", "gamma"},
	})
	if len(result) != 2 {
		t.Fatalf("expected 2 servers, got %d: %v", len(result), result)
	}
	if _, ok := result["beta"]; ok {
		t.Fatal("beta should not be in result")
	}
	if _, ok := result["alpha"]; !ok {
		t.Fatal("alpha should be in result")
	}
	if _, ok := result["gamma"]; !ok {
		t.Fatal("gamma should be in result")
	}
}

func TestFilterProjectMCPServers_AllowListExcludesDisabled(t *testing.T) {
	servers := makeServers("alpha", "beta")
	result := filterProjectMCPServers(servers, contracts.Settings{
		EnabledMCPJSONServers:  []string{"alpha", "beta"},
		DisabledMCPJSONServers: []string{"beta"},
	})
	if len(result) != 1 {
		t.Fatalf("expected 1 server, got %d: %v", len(result), result)
	}
	if _, ok := result["beta"]; ok {
		t.Fatal("beta should be excluded by disabled list")
	}
}

func TestFilterProjectMCPServers_EmptyServersPassThrough(t *testing.T) {
	yes := true
	result := filterProjectMCPServers(nil, contracts.Settings{
		EnableAllProjectMCPServers: &yes,
	})
	if result != nil {
		t.Fatalf("expected nil for empty servers, got: %v", result)
	}
}
