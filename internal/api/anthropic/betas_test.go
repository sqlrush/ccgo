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
