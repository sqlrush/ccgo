package conversation

import (
	"context"
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
