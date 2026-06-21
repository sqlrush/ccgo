package conversation

import (
	"context"
	"testing"

	"ccgo/internal/contracts"
	"ccgo/internal/model"
	"ccgo/internal/tool"
)

func TestThinkingRequestConfigEnabledForThinkingModel(t *testing.T) {
	cap, ok := model.DefaultRegistry().Resolve("claude-sonnet-4-6")
	if !ok {
		t.Skip("model not in registry; confirm name via go doc ./internal/model")
	}
	cfg := thinkingRequestConfig(cap, 8000)
	if cfg == nil {
		t.Fatal("expected thinking config for a thinking-capable model")
	}
	if cfg["type"] != "enabled" {
		t.Fatalf("type = %v want enabled", cfg["type"])
	}
	if cfg["budget_tokens"] != 8000 {
		t.Fatalf("budget_tokens = %v want 8000", cfg["budget_tokens"])
	}
}

func TestThinkingRequestConfigNilForNonThinkingModel(t *testing.T) {
	cap, ok := model.DefaultRegistry().Resolve(model.Claude45Haiku)
	if !ok {
		t.Skip("model not in registry")
	}
	if cfg := thinkingRequestConfig(cap, 8000); cfg != nil {
		t.Fatalf("expected nil thinking config for non-thinking model, got %v", cfg)
	}
}

func TestThinkingRequestConfigNilForZeroBudget(t *testing.T) {
	cap, ok := model.DefaultRegistry().Resolve("claude-sonnet-4-6")
	if !ok {
		t.Skip("model not in registry")
	}
	if cfg := thinkingRequestConfig(cap, 0); cfg != nil {
		t.Fatalf("expected nil thinking config when budget is 0, got %v", cfg)
	}
}

func TestBuildRequestSetsThinkingWhenBudgetSet(t *testing.T) {
	reg, err := tool.NewRegistry()
	if err != nil {
		t.Fatal(err)
	}
	r := Runner{Tools: tool.NewExecutor(reg), Model: "claude-sonnet-4-6", ThinkingBudgetTokens: 8000}
	history := []contracts.Message{
		{Type: contracts.MessageUser, Content: []contracts.ContentBlock{contracts.NewTextBlock("hi")}},
	}
	req, err := r.buildRequest(context.Background(), history, r.model(), relevantMemoryRequestContext{SkipSync: true})
	if err != nil {
		t.Fatalf("buildRequest err: %v", err)
	}
	if req.Thinking == nil || req.Thinking["type"] != "enabled" {
		t.Fatalf("expected thinking enabled, got %#v", req.Thinking)
	}
}
