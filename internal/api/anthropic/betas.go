package anthropic

import "strings"

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
