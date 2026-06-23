package anthropic

import (
	"strings"

	"ccgo/internal/contracts"
)

const (
	PromptCachingScopeBetaHeader  = "prompt-caching-scope-2026-01-05"
	Context1MBetaHeader           = "context-1m-2025-08-07"
	StructuredOutputsBetaHeader   = "structured-outputs-2025-11-13"
	FastModeBetaHeader            = "fast-mode-2025-01-24"
	CacheEditingBetaHeader        = "cache-editing-2025-01-24"
	ToolSearchBetaHeader          = "advanced-tool-use-2025-11-20"  // CC: TOOL_SEARCH_BETA_HEADER_1P (betas.ts:9)
	InterleavedThinkingBetaHeader = "interleaved-thinking-2025-05-14" // CC: INTERLEAVED_THINKING_BETA_HEADER (betas.ts:2)
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
	switch request := payload.(type) {
	case Request:
		return dynamicBetaHeadersForRequest(request)
	case CountTokensRequest:
		return dynamicBetaHeadersForCountTokensRequest(request.Model, request.Messages, request.System, request.Tools)
	default:
		return nil
	}
}

func dynamicBetaHeadersForRequest(request Request) []string {
	var betas []string
	// Global cache scope implies prompt-caching-scope beta; add unconditionally
	// when UseGlobalCacheScope is set so the header is present even if messages
	// have no explicit cache_control yet. CC ref: claude.ts:1216-1222.
	if request.UseGlobalCacheScope || requestUsesPromptCaching(request.Messages, request.System, request.Tools) {
		betas = append(betas, PromptCachingScopeBetaHeader)
	}
	if requestUsesCacheEditing(request.Messages) {
		betas = append(betas, CacheEditingBetaHeader)
	}
	if requestUsesStructuredOutputs(request.Tools) {
		betas = append(betas, StructuredOutputsBetaHeader)
	}
	if requestUsesContext1M(request.Model) {
		betas = append(betas, Context1MBetaHeader)
	}
	// Tool search (defer_loading) requires the advanced-tool-use beta header.
	// CC ref: betas.ts getToolSearchBetaHeader; claude.ts:1174-1182.
	if request.ToolSearchActive {
		betas = append(betas, ToolSearchBetaHeader)
	}
	return betas
}

func dynamicBetaHeadersForCountTokensRequest(modelName string, messages []contracts.APIMessage, system any, tools []ToolDefinition) []string {
	var betas []string
	if requestUsesPromptCaching(messages, system, tools) {
		betas = append(betas, PromptCachingScopeBetaHeader)
	}
	if requestUsesCacheEditing(messages) {
		betas = append(betas, CacheEditingBetaHeader)
	}
	if requestUsesStructuredOutputs(tools) {
		betas = append(betas, StructuredOutputsBetaHeader)
	}
	if requestUsesContext1M(modelName) {
		betas = append(betas, Context1MBetaHeader)
	}
	return betas
}

func requestUsesPromptCaching(messages []contracts.APIMessage, system any, tools []ToolDefinition) bool {
	for _, msg := range messages {
		if contentBlocksUsePromptCaching(msg.Content) {
			return true
		}
	}
	if systemBlocks, ok := system.([]contracts.ContentBlock); ok && contentBlocksUsePromptCaching(systemBlocks) {
		return true
	}
	for _, tool := range tools {
		if tool.CacheControl != nil {
			return true
		}
	}
	return false
}

func requestUsesCacheEditing(messages []contracts.APIMessage) bool {
	for _, msg := range messages {
		for _, block := range msg.Content {
			if block.Type == contracts.ContentCacheEdits || len(block.Edits) > 0 {
				return true
			}
		}
	}
	return false
}

func requestUsesStructuredOutputs(tools []ToolDefinition) bool {
	for _, tool := range tools {
		if tool.Strict {
			return true
		}
	}
	return false
}

func requestUsesContext1M(modelName string) bool {
	return strings.HasSuffix(strings.TrimSpace(strings.ToLower(modelName)), "[1m]")
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
