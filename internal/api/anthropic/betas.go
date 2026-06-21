package anthropic

import (
	"strings"

	"ccgo/internal/contracts"
)

const (
	PromptCachingScopeBetaHeader = "prompt-caching-scope-2026-01-05"
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
	switch request := payload.(type) {
	case Request:
		return dynamicBetaHeadersForRequest(request.Model, request.Messages, request.System, request.Tools)
	case CountTokensRequest:
		return dynamicBetaHeadersForRequest(request.Model, request.Messages, request.System, request.Tools)
	default:
		return nil
	}
}

func dynamicBetaHeadersForRequest(modelName string, messages []contracts.APIMessage, system any, tools []ToolDefinition) []string {
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
