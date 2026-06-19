package searchtools

import (
	"encoding/json"
	"fmt"
	"sort"
	"strconv"
	"strings"

	"ccgo/internal/contracts"
	"ccgo/internal/tool"
)

const defaultToolSearchLimit = 8
const maxToolSearchLimit = 50

type searchInput struct {
	Query string `json:"query"`
	TopN  int    `json:"topn,omitempty"`
}

func NewToolSearchTool() tool.Tool {
	return tool.FuncTool{
		DefinitionValue: contracts.ToolDefinition{
			Name:            "ToolSearch",
			Description:     "Search available tool definitions by name and description.",
			ReadOnly:        true,
			ConcurrencySafe: true,
			Strict:          true,
			InputSchema: contracts.JSONSchema{
				"type":     "object",
				"required": []any{"query"},
				"properties": map[string]any{
					"query": map[string]any{
						"type":        "string",
						"description": "Search query for available tools.",
					},
					"topn": map[string]any{
						"type":        "integer",
						"description": "Maximum number of matching tools to return.",
					},
				},
			},
		},
		NormalizeFunc:   normalizeSearchInput,
		ValidateFunc:    validateSearchInput,
		PermissionFunc:  allowToolSearch,
		CallFunc:        callToolSearch,
		ReadOnlyFunc:    func(json.RawMessage) bool { return true },
		ConcurrencyFunc: func(json.RawMessage) bool { return true },
	}
}

func normalizeSearchInput(raw json.RawMessage) (json.RawMessage, error) {
	var obj map[string]any
	if err := json.Unmarshal(raw, &obj); err != nil {
		return nil, err
	}
	input := searchInput{}
	if value, ok := firstString(obj, "query", "q", "search", "search_query"); ok {
		input.Query = value
	}
	if value, ok, err := firstInt(obj, "topn", "topN", "limit", "max_results", "maxResults"); err != nil {
		return nil, err
	} else if ok {
		input.TopN = value
	}
	return json.Marshal(input)
}

func validateSearchInput(_ tool.Context, raw json.RawMessage) error {
	input, err := decodeSearchInput(raw)
	if err != nil {
		return err
	}
	if strings.TrimSpace(input.Query) == "" {
		return fmt.Errorf("query is required")
	}
	if input.TopN < 0 {
		return fmt.Errorf("topn must be nonnegative")
	}
	if input.TopN > maxToolSearchLimit {
		return fmt.Errorf("topn must be at most %d", maxToolSearchLimit)
	}
	return nil
}

func allowToolSearch(tool.Context, json.RawMessage) (contracts.PermissionDecision, error) {
	return contracts.PermissionDecision{
		Behavior:       contracts.PermissionAllow,
		DecisionReason: "tool definition search is read-only",
	}, nil
}

func callToolSearch(ctx tool.Context, raw json.RawMessage, _ tool.ProgressSink) (contracts.ToolResult, error) {
	input, err := decodeSearchInput(raw)
	if err != nil {
		return contracts.ToolResult{}, err
	}
	registry, ok := toolRegistryFromMetadata(ctx.Metadata)
	if !ok {
		return contracts.ToolResult{}, fmt.Errorf("tool registry metadata is unavailable")
	}
	limit := input.TopN
	if limit == 0 {
		limit = defaultToolSearchLimit
	}
	definitions, err := registry.Definitions(tool.PromptContext{
		WorkingDirectory: ctx.WorkingDirectory,
		Metadata:         ctx.Metadata,
	})
	if err != nil {
		return contracts.ToolResult{}, err
	}
	results := matchToolDefinitions(definitions, input.Query, limit)
	structuredResults := make([]map[string]any, 0, len(results))
	lines := []string{
		"Tool search: " + strings.TrimSpace(input.Query),
		fmt.Sprintf("Matches: %d", len(results)),
	}
	for _, result := range results {
		entry := map[string]any{
			"name":              result.Definition.Name,
			"score":             result.Score,
			"read_only":         result.Definition.ReadOnly,
			"concurrency_safe":  result.Definition.ConcurrencySafe,
			"destructive":       result.Definition.Destructive,
			"interruptBehavior": result.Definition.InterruptBehavior,
		}
		if len(result.Definition.Aliases) > 0 {
			entry["aliases"] = append([]string(nil), result.Definition.Aliases...)
		}
		if description := toolDefinitionDescription(result.Definition); description != "" {
			entry["description"] = description
		}
		structuredResults = append(structuredResults, entry)
		line := "- " + result.Definition.Name
		if description := toolDefinitionDescription(result.Definition); description != "" {
			line += ": " + description
		}
		lines = append(lines, line)
	}
	if len(results) == 0 {
		lines = append(lines, "No tools matched.")
	}
	return contracts.ToolResult{
		Content: strings.Join(lines, "\n"),
		StructuredContent: map[string]any{
			"query":   strings.TrimSpace(input.Query),
			"limit":   limit,
			"matches": len(results),
			"results": structuredResults,
		},
	}, nil
}

type scoredDefinition struct {
	Definition contracts.ToolDefinition
	Score      int
}

func matchToolDefinitions(definitions []contracts.ToolDefinition, query string, limit int) []scoredDefinition {
	query = strings.ToLower(strings.TrimSpace(query))
	terms := strings.Fields(query)
	if len(terms) == 0 {
		return nil
	}
	var results []scoredDefinition
	for _, definition := range definitions {
		score := toolDefinitionScore(definition, terms)
		if score <= 0 {
			continue
		}
		results = append(results, scoredDefinition{Definition: definition, Score: score})
	}
	sort.Slice(results, func(i, j int) bool {
		if results[i].Score != results[j].Score {
			return results[i].Score > results[j].Score
		}
		return results[i].Definition.Name < results[j].Definition.Name
	})
	if limit > 0 && len(results) > limit {
		results = results[:limit]
	}
	return results
}

func toolDefinitionScore(definition contracts.ToolDefinition, terms []string) int {
	name := strings.ToLower(definition.Name)
	aliases := make([]string, 0, len(definition.Aliases))
	for _, alias := range definition.Aliases {
		aliases = append(aliases, strings.ToLower(alias))
	}
	text := strings.ToLower(strings.Join([]string{definition.Description, definition.Prompt, definition.SearchHint}, " "))
	score := 0
	for _, term := range terms {
		matched := false
		if name == term {
			score += 20
			matched = true
		} else if strings.Contains(name, term) {
			score += 12
			matched = true
		}
		for _, alias := range aliases {
			if alias == term {
				score += 16
				matched = true
			} else if strings.Contains(alias, term) {
				score += 8
				matched = true
			}
		}
		if strings.Contains(text, term) {
			score += 4
			matched = true
		}
		if !matched {
			return 0
		}
	}
	return score
}

func decodeSearchInput(raw json.RawMessage) (searchInput, error) {
	var input searchInput
	if err := json.Unmarshal(raw, &input); err != nil {
		return searchInput{}, err
	}
	input.Query = strings.TrimSpace(input.Query)
	return input, nil
}

func toolRegistryFromMetadata(metadata map[string]any) (*tool.Registry, bool) {
	if metadata == nil {
		return nil, false
	}
	switch registry := metadata[tool.MetadataToolRegistryKey].(type) {
	case *tool.Registry:
		return registry, registry != nil
	default:
		return nil, false
	}
}

func toolDefinitionDescription(definition contracts.ToolDefinition) string {
	for _, text := range []string{definition.Description, definition.SearchHint, definition.Prompt} {
		if trimmed := trimSearchSnippet(text); trimmed != "" {
			return trimmed
		}
	}
	return ""
}

func trimSearchSnippet(text string) string {
	text = strings.Join(strings.Fields(strings.TrimSpace(text)), " ")
	const limit = 160
	if len(text) <= limit {
		return text
	}
	return text[:limit-3] + "..."
}

func firstString(obj map[string]any, keys ...string) (string, bool) {
	for _, key := range keys {
		if value, ok := obj[key].(string); ok {
			return value, true
		}
	}
	return "", false
}

func firstInt(obj map[string]any, keys ...string) (int, bool, error) {
	for _, key := range keys {
		value, ok := obj[key]
		if !ok {
			continue
		}
		switch typed := value.(type) {
		case float64:
			if typed != float64(int(typed)) {
				return 0, false, fmt.Errorf("%s must be an integer", key)
			}
			return int(typed), true, nil
		case string:
			if strings.TrimSpace(typed) == "" {
				continue
			}
			parsed, err := strconv.Atoi(strings.TrimSpace(typed))
			if err != nil {
				return 0, false, fmt.Errorf("%s must be an integer", key)
			}
			return parsed, true, nil
		}
	}
	return 0, false, nil
}
