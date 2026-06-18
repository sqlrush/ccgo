package anthropic

import (
	"strings"

	"ccgo/internal/contracts"
)

const (
	PromptCachingScopeBetaHeader = "prompt-caching-scope-2024-07-31"
	Context1MBetaHeader          = "context-1m-2025-08-07"
	StructuredOutputsBetaHeader  = "structured-outputs-2025-11-13"
	FastModeBetaHeader           = "fast-mode-2025-01-24"
	CacheEditingBetaHeader       = "cache-editing-2025-01-24"
)

func MergeBetaHeaders(groups ...[]string) []string {
	seen := map[string]struct{}{}
	var out []string
	for _, group := range groups {
		for _, header := range group {
			header = strings.TrimSpace(header)
			if header == "" {
				continue
			}
			if _, ok := seen[header]; ok {
				continue
			}
			seen[header] = struct{}{}
			out = append(out, header)
		}
	}
	return out
}

func BetaHeaderValue(headers []string) string {
	return strings.Join(MergeBetaHeaders(headers), ",")
}

func DynamicBetaHeaders(payload any) []string {
	request, ok := payload.(Request)
	if !ok {
		return nil
	}
	var betas []string
	if requestUsesPromptCaching(request) {
		betas = append(betas, PromptCachingScopeBetaHeader)
	}
	if requestUsesCacheEditing(request) {
		betas = append(betas, CacheEditingBetaHeader)
	}
	if requestUsesStructuredOutputs(request) {
		betas = append(betas, StructuredOutputsBetaHeader)
	}
	if requestUsesContext1M(request) {
		betas = append(betas, Context1MBetaHeader)
	}
	return betas
}

func requestUsesPromptCaching(request Request) bool {
	for _, msg := range request.Messages {
		if contentBlocksUsePromptCaching(msg.Content) {
			return true
		}
	}
	if systemBlocks, ok := request.System.([]contracts.ContentBlock); ok && contentBlocksUsePromptCaching(systemBlocks) {
		return true
	}
	for _, tool := range request.Tools {
		if tool.CacheControl != nil {
			return true
		}
	}
	return false
}

func requestUsesCacheEditing(request Request) bool {
	for _, msg := range request.Messages {
		for _, block := range msg.Content {
			if block.Type == contracts.ContentCacheEdits || len(block.Edits) > 0 {
				return true
			}
		}
	}
	return false
}

func requestUsesStructuredOutputs(request Request) bool {
	for _, tool := range request.Tools {
		if tool.Strict {
			return true
		}
	}
	return false
}

func requestUsesContext1M(request Request) bool {
	return strings.HasSuffix(strings.TrimSpace(strings.ToLower(request.Model)), "[1m]")
}

func contentBlocksUsePromptCaching(blocks []contracts.ContentBlock) bool {
	for _, block := range blocks {
		if block.CacheControl != nil || strings.TrimSpace(block.CacheReference) != "" {
			return true
		}
		if block.Type == contracts.ContentCacheEdits || len(block.Edits) > 0 {
			return true
		}
	}
	return false
}
