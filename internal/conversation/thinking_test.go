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
	cfg := thinkingRequestConfig(cap, 8000, 16000)
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
	if cfg := thinkingRequestConfig(cap, 8000, 16000); cfg != nil {
		t.Fatalf("expected nil thinking config for non-thinking model, got %v", cfg)
	}
}

func TestThinkingRequestConfigNilForZeroBudget(t *testing.T) {
	cap, ok := model.DefaultRegistry().Resolve("claude-sonnet-4-6")
	if !ok {
		t.Skip("model not in registry")
	}
	if cfg := thinkingRequestConfig(cap, 0, 16000); cfg != nil {
		t.Fatalf("expected nil thinking config when budget is 0, got %v", cfg)
	}
}

// TestThinkingBudgetClampsToMaxTokensMinus1 verifies that when ThinkingBudgetTokens >= MaxTokens,
// buildRequest clamps budget_tokens to maxTokens-1, matching usage.go:AdjustRequestForNonStreaming.
func TestThinkingBudgetClampsToMaxTokensMinus1(t *testing.T) {
	reg, err := tool.NewRegistry()
	if err != nil {
		t.Fatal(err)
	}
	// ThinkingBudgetTokens equals MaxTokens — must be clamped to MaxTokens-1.
	r := Runner{
		Tools:                tool.NewExecutor(reg),
		Model:                "claude-sonnet-4-6",
		MaxTokens:            4096,
		ThinkingBudgetTokens: 4096,
	}
	history := []contracts.Message{
		{Type: contracts.MessageUser, Content: []contracts.ContentBlock{contracts.NewTextBlock("hi")}},
	}
	req, err := r.buildRequest(context.Background(), history, r.model(), relevantMemoryRequestContext{SkipSync: true})
	if err != nil {
		t.Fatalf("buildRequest err: %v", err)
	}
	if req.Thinking == nil {
		t.Fatal("expected thinking to be set")
	}
	budget, ok := req.Thinking["budget_tokens"].(int)
	if !ok {
		t.Fatalf("budget_tokens not int: %T %v", req.Thinking["budget_tokens"], req.Thinking["budget_tokens"])
	}
	if budget >= r.MaxTokens {
		t.Fatalf("budget_tokens %d must be < max_tokens %d", budget, r.MaxTokens)
	}
	if budget != r.MaxTokens-1 {
		t.Fatalf("budget_tokens = %d want %d (maxTokens-1)", budget, r.MaxTokens-1)
	}
}

// TestThinkingBudgetUnchangedWhenBelowMaxTokens verifies that a budget < max_tokens is left unchanged.
func TestThinkingBudgetUnchangedWhenBelowMaxTokens(t *testing.T) {
	reg, err := tool.NewRegistry()
	if err != nil {
		t.Fatal(err)
	}
	r := Runner{
		Tools:                tool.NewExecutor(reg),
		Model:                "claude-sonnet-4-6",
		MaxTokens:            8000,
		ThinkingBudgetTokens: 4000,
	}
	history := []contracts.Message{
		{Type: contracts.MessageUser, Content: []contracts.ContentBlock{contracts.NewTextBlock("hi")}},
	}
	req, err := r.buildRequest(context.Background(), history, r.model(), relevantMemoryRequestContext{SkipSync: true})
	if err != nil {
		t.Fatalf("buildRequest err: %v", err)
	}
	if req.Thinking == nil {
		t.Fatal("expected thinking to be set")
	}
	budget, ok := req.Thinking["budget_tokens"].(int)
	if !ok {
		t.Fatalf("budget_tokens not int: %T %v", req.Thinking["budget_tokens"], req.Thinking["budget_tokens"])
	}
	if budget != 4000 {
		t.Fatalf("budget_tokens = %d want 4000 (unchanged)", budget)
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
