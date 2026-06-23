package anthropic

import (
	"testing"

	"ccgo/internal/contracts"
)

func TestPromptCachingScopeBetaHeaderIsCurrent(t *testing.T) {
	const want = "prompt-caching-scope-2026-01-05"
	if PromptCachingScopeBetaHeader != want {
		t.Fatalf("PromptCachingScopeBetaHeader = %q want %q", PromptCachingScopeBetaHeader, want)
	}
}

func TestToolSearchBetaHeaderConstant(t *testing.T) {
	const want = "advanced-tool-use-2025-11-20"
	if ToolSearchBetaHeader != want {
		t.Fatalf("ToolSearchBetaHeader = %q want %q", ToolSearchBetaHeader, want)
	}
}

func TestInterleavedThinkingBetaHeaderConstant(t *testing.T) {
	const want = "interleaved-thinking-2025-05-14"
	if InterleavedThinkingBetaHeader != want {
		t.Fatalf("InterleavedThinkingBetaHeader = %q want %q", InterleavedThinkingBetaHeader, want)
	}
}

func TestDynamicBetaHeadersIncludesToolSearch(t *testing.T) {
	req := Request{
		Model: "claude-sonnet-4-6",
		Messages: []contracts.APIMessage{{
			Role:    "user",
			Content: []contracts.ContentBlock{contracts.NewTextBlock("hi")},
		}},
		Tools: []ToolDefinition{{
			Name:         "ToolSearch",
			DeferLoading: true,
		}},
		ToolSearchActive: true,
	}
	betas := DynamicBetaHeaders(req)
	found := false
	for _, b := range betas {
		if b == ToolSearchBetaHeader {
			found = true
		}
	}
	if !found {
		t.Fatalf("expected tool search beta header %q in %v", ToolSearchBetaHeader, betas)
	}
}

func TestDynamicBetaHeadersOmitsToolSearchWhenInactive(t *testing.T) {
	req := Request{
		Model: "claude-sonnet-4-6",
		Messages: []contracts.APIMessage{{
			Role:    "user",
			Content: []contracts.ContentBlock{contracts.NewTextBlock("hi")},
		}},
		ToolSearchActive: false,
	}
	betas := DynamicBetaHeaders(req)
	for _, b := range betas {
		if b == ToolSearchBetaHeader {
			t.Fatalf("tool search beta header should be absent when ToolSearchActive=false, got %v", betas)
		}
	}
}

func TestGlobalCacheScopeBetaHeaderIncluded(t *testing.T) {
	req := Request{
		Model: "claude-sonnet-4-6",
		Messages: []contracts.APIMessage{{
			Role:    "user",
			Content: []contracts.ContentBlock{contracts.NewTextBlock("hi")},
		}},
		UseGlobalCacheScope: true,
	}
	betas := DynamicBetaHeaders(req)
	found := false
	for _, b := range betas {
		if b == PromptCachingScopeBetaHeader {
			found = true
		}
	}
	if !found {
		t.Fatalf("expected prompt-caching-scope beta header when UseGlobalCacheScope=true, got %v", betas)
	}
}

func TestDynamicBetaHeadersEmitsCurrentCacheScope(t *testing.T) {
	req := Request{
		Model: "claude-sonnet-4-6",
		Messages: []contracts.APIMessage{{
			Role: "user",
			Content: []contracts.ContentBlock{{
				Type:         contracts.ContentText,
				Text:         "hi",
				CacheControl: &contracts.CacheControl{Type: "ephemeral"},
			}},
		}},
	}
	betas := DynamicBetaHeaders(req)
	found := false
	for _, b := range betas {
		if b == "prompt-caching-scope-2026-01-05" {
			found = true
		}
	}
	if !found {
		t.Fatalf("expected current cache-scope header in %v", betas)
	}
}
