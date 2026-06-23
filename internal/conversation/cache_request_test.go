package conversation

import (
	"context"
	encoding_json "encoding/json"
	"testing"

	"ccgo/internal/contracts"
	"ccgo/internal/tool"
)

func lastContentBlock(msg contracts.APIMessage) (contracts.ContentBlock, bool) {
	if len(msg.Content) == 0 {
		return contracts.ContentBlock{}, false
	}
	return msg.Content[len(msg.Content)-1], true
}

func TestBuildRequestAddsCacheBreakpointWhenEnabled(t *testing.T) {
	reg, err := tool.NewRegistry()
	if err != nil {
		t.Fatal(err)
	}
	r := Runner{
		Tools:               tool.NewExecutor(reg),
		Model:               "claude-sonnet-4-6",
		EnablePromptCaching: true,
		PromptCacheTTL:      "1h",
	}
	history := []contracts.Message{
		{Type: contracts.MessageUser, Content: []contracts.ContentBlock{contracts.NewTextBlock("hello")}},
	}
	req, err := r.buildRequest(context.Background(), history, r.model(), relevantMemoryRequestContext{SkipSync: true})
	if err != nil {
		t.Fatalf("buildRequest err: %v", err)
	}
	if len(req.Messages) == 0 {
		t.Fatal("no messages built")
	}
	block, ok := lastContentBlock(req.Messages[len(req.Messages)-1])
	if !ok || block.CacheControl == nil {
		t.Fatalf("expected cache_control on last block, got %#v", block)
	}
	if block.CacheControl.Type != "ephemeral" {
		t.Fatalf("cache_control.type = %q want ephemeral", block.CacheControl.Type)
	}
	if block.CacheControl.TTL != "1h" {
		t.Fatalf("cache_control.ttl = %q want 1h", block.CacheControl.TTL)
	}
}

// TestBuildRequestGlobalCacheScopeOnSystemBlock verifies that when both
// prompt caching and a first-party base URL are active, the system prompt
// block carries cache_control.scope="global". CC ref: claude.ts:1207-1229.
func TestBuildRequestGlobalCacheScopeOnSystemBlock(t *testing.T) {
	// Clear any custom base URL so isFirstPartyAnthropicBaseURL returns true.
	t.Setenv("ANTHROPIC_BASE_URL", "")
	t.Setenv("CLAUDE_CODE_DISABLE_EXPERIMENTAL_BETAS", "")
	reg, err := tool.NewRegistry()
	if err != nil {
		t.Fatal(err)
	}
	r := Runner{
		Tools:               tool.NewExecutor(reg),
		Model:               "claude-sonnet-4-6",
		SystemPrompt:        "Be helpful.",
		EnablePromptCaching: true,
	}
	history := []contracts.Message{
		{Type: contracts.MessageUser, Content: []contracts.ContentBlock{contracts.NewTextBlock("hello")}},
	}
	req, err := r.buildRequest(context.Background(), history, r.model(), relevantMemoryRequestContext{SkipSync: true})
	if err != nil {
		t.Fatalf("buildRequest err: %v", err)
	}
	if !req.UseGlobalCacheScope {
		t.Fatal("UseGlobalCacheScope should be true for first-party URL with caching")
	}
	blocks, ok := req.System.([]contracts.ContentBlock)
	if !ok {
		t.Fatalf("system should be []ContentBlock when global scope active, got %T", req.System)
	}
	if len(blocks) == 0 {
		t.Fatal("system blocks should not be empty")
	}
	last := blocks[len(blocks)-1]
	if last.CacheControl == nil {
		t.Fatal("system block should have cache_control")
	}
	if last.CacheControl.Scope != "global" {
		t.Fatalf("system block cache_control.scope = %q want global", last.CacheControl.Scope)
	}
}

// TestBuildRequestToolSearchActiveInRequest verifies that buildRequest sets
// ToolSearchActive on the request when tool search is enabled.
// CC ref: betas.ts TOOL_SEARCH_BETA_HEADER_1P; claude.ts:1174-1182.
func TestBuildRequestToolSearchActiveInRequest(t *testing.T) {
	t.Setenv("ENABLE_TOOL_SEARCH", "1")
	t.Setenv("ANTHROPIC_BASE_URL", "")
	t.Setenv("CLAUDE_CODE_DISABLE_EXPERIMENTAL_BETAS", "")

	reg, err := tool.NewRegistry(tool.FuncTool{
		DefinitionValue: contracts.ToolDefinition{
			Name:        "ToolSearch",
			Description: "search",
			InputSchema: contracts.JSONSchema{"type": "object"},
		},
		CallFunc: func(ctx tool.Context, _ encoding_json.RawMessage, _ tool.ProgressSink) (contracts.ToolResult, error) {
			return contracts.ToolResult{}, nil
		},
	}, tool.FuncTool{
		DefinitionValue: contracts.ToolDefinition{
			Name:        "MCPDeferred",
			Description: "mcp tool",
			ShouldDefer: true,
			MCP:         &contracts.MCPToolRef{ServerName: "test"},
			InputSchema: contracts.JSONSchema{"type": "object"},
		},
		CallFunc: func(ctx tool.Context, _ encoding_json.RawMessage, _ tool.ProgressSink) (contracts.ToolResult, error) {
			return contracts.ToolResult{}, nil
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	r := Runner{
		Tools: tool.NewExecutor(reg),
		Model: "claude-sonnet-4-6",
	}
	history := []contracts.Message{
		{Type: contracts.MessageUser, Content: []contracts.ContentBlock{contracts.NewTextBlock("hi")}},
	}
	req, err := r.buildRequest(context.Background(), history, r.model(), relevantMemoryRequestContext{SkipSync: true})
	if err != nil {
		t.Fatalf("buildRequest err: %v", err)
	}
	if !req.ToolSearchActive {
		t.Fatal("ToolSearchActive should be true when ENABLE_TOOL_SEARCH=1 and MCP deferred tools exist")
	}
}

func TestBuildRequestNoCacheWhenDisabled(t *testing.T) {
	reg, err := tool.NewRegistry()
	if err != nil {
		t.Fatal(err)
	}
	r := Runner{Tools: tool.NewExecutor(reg), Model: "claude-sonnet-4-6"}
	history := []contracts.Message{
		{Type: contracts.MessageUser, Content: []contracts.ContentBlock{contracts.NewTextBlock("hello")}},
	}
	req, err := r.buildRequest(context.Background(), history, r.model(), relevantMemoryRequestContext{SkipSync: true})
	if err != nil {
		t.Fatalf("buildRequest err: %v", err)
	}
	block, _ := lastContentBlock(req.Messages[len(req.Messages)-1])
	if block.CacheControl != nil {
		t.Fatalf("expected no cache_control when disabled, got %#v", block.CacheControl)
	}
}
