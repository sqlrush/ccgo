package mcp

import (
	"testing"

	"ccgo/internal/contracts"
)

func TestNormalizeNameForMCP(t *testing.T) {
	if got := NormalizeNameForMCP("my.server name/tool"); got != "my_server_name_tool" {
		t.Fatalf("normalized = %q", got)
	}
	if got := NormalizeNameForMCP("claude.ai Slack Connector"); got != "claude_ai_Slack_Connector" {
		t.Fatalf("claude.ai normalized = %q", got)
	}
	if got := NormalizeNameForMCP("claude.ai .Slack  Connector."); got != "claude_ai_Slack_Connector" {
		t.Fatalf("claude.ai collapsed = %q", got)
	}
}

func TestBuildAndParseToolName(t *testing.T) {
	name := BuildToolName("github.com", "issue__create")
	if name != "mcp__github_com__issue__create" {
		t.Fatalf("name = %q", name)
	}
	info, ok := InfoFromToolName(name)
	if !ok {
		t.Fatalf("not mcp: %q", name)
	}
	if info.ServerName != "github_com" || info.ToolName != "issue__create" {
		t.Fatalf("info = %#v", info)
	}
}

func TestInfoFromToolNameAllowsServerOnlyRules(t *testing.T) {
	info, ok := InfoFromToolName("mcp__github")
	if !ok {
		t.Fatal("expected mcp server rule")
	}
	if info.ServerName != "github" || info.ToolName != "" {
		t.Fatalf("info = %#v", info)
	}
	if _, ok := InfoFromToolName("Bash"); ok {
		t.Fatal("non-mcp tool parsed as mcp")
	}
}

func TestDisplayNameHelpers(t *testing.T) {
	if got := DisplayName("mcp__github__search", "github"); got != "search" {
		t.Fatalf("display = %q", got)
	}
	if got := ExtractToolDisplayName("github - Search issues (MCP)"); got != "Search issues" {
		t.Fatalf("display = %q", got)
	}
	if got := ExtractToolDisplayName("Search issues (MCP)"); got != "Search issues" {
		t.Fatalf("display without server = %q", got)
	}
}

func TestToolNameForPermissionCheck(t *testing.T) {
	regular := contracts.ToolDefinition{Name: "Write"}
	if got := ToolNameForPermissionCheck(regular); got != "Write" {
		t.Fatalf("regular = %q", got)
	}
	mcpTool := contracts.ToolDefinition{
		Name: "Search",
		MCP:  &contracts.MCPToolRef{ServerName: "github.com", ToolName: "search/issues"},
	}
	if got := ToolNameForPermissionCheck(mcpTool); got != "mcp__github_com__search_issues" {
		t.Fatalf("mcp = %q", got)
	}
}
