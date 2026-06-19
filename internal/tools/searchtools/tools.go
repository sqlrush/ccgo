package searchtools

import (
	"encoding/json"
	"fmt"
	"math"
	"sort"
	"strconv"
	"strings"
	"unicode"

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
			"name":                  result.Definition.Name,
			"score":                 result.Score,
			"read_only":             result.Definition.ReadOnly,
			"concurrency_safe":      result.Definition.ConcurrencySafe,
			"destructive":           result.Definition.Destructive,
			"requires_interaction":  result.Definition.RequiresInteraction,
			"should_defer":          result.Definition.ShouldDefer,
			"always_load":           result.Definition.AlwaysLoad,
			"eager_input_streaming": result.Definition.EagerInputStreaming,
			"strict":                result.Definition.Strict,
			"interruptBehavior":     result.Definition.InterruptBehavior,
		}
		if len(result.Definition.InputSchema) > 0 {
			entry["input_schema"] = copyJSONSchema(result.Definition.InputSchema)
		}
		if len(result.Definition.OutputSchema) > 0 {
			entry["output_schema"] = copyJSONSchema(result.Definition.OutputSchema)
		}
		if result.Definition.MaxResultSizeChars > 0 {
			entry["max_result_size_chars"] = result.Definition.MaxResultSizeChars
		}
		if result.Definition.CacheControl != nil {
			entry["cache_control"] = *result.Definition.CacheControl
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
			"ranking": "bm25",
			"results": structuredResults,
		},
	}, nil
}

type scoredDefinition struct {
	Definition contracts.ToolDefinition
	Score      float64
}

type bm25Document struct {
	Definition      contracts.ToolDefinition
	TermFrequencies map[string]float64
	Length          float64
}

type bm25Corpus struct {
	Documents      []bm25Document
	DocumentCounts map[string]int
	AverageDocSize float64
	TotalDocuments int
}

func matchToolDefinitions(definitions []contracts.ToolDefinition, query string, limit int) []scoredDefinition {
	terms := uniqueStrings(searchTokens(query))
	if len(terms) == 0 {
		return nil
	}
	corpus := buildBM25Corpus(definitions)
	var results []scoredDefinition
	for _, document := range corpus.Documents {
		score := bm25Score(document, corpus, terms)
		if score <= 0 {
			continue
		}
		results = append(results, scoredDefinition{Definition: document.Definition, Score: roundScore(score)})
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

func buildBM25Corpus(definitions []contracts.ToolDefinition) bm25Corpus {
	documents := make([]bm25Document, 0, len(definitions))
	documentCounts := map[string]int{}
	var totalLength float64
	for _, definition := range definitions {
		document := toolSearchDocument(definition)
		if len(document.TermFrequencies) == 0 {
			continue
		}
		documents = append(documents, document)
		totalLength += document.Length
		for term := range document.TermFrequencies {
			documentCounts[term]++
		}
	}
	averageDocSize := 1.0
	if len(documents) > 0 {
		averageDocSize = totalLength / float64(len(documents))
	}
	return bm25Corpus{
		Documents:      documents,
		DocumentCounts: documentCounts,
		AverageDocSize: averageDocSize,
		TotalDocuments: len(documents),
	}
}

func toolSearchDocument(definition contracts.ToolDefinition) bm25Document {
	document := bm25Document{
		Definition:      definition,
		TermFrequencies: map[string]float64{},
	}
	addWeightedField(&document, definition.Name, 4.0)
	for _, alias := range definition.Aliases {
		addWeightedField(&document, alias, 3.0)
	}
	addWeightedField(&document, definition.Description, 1.4)
	addWeightedField(&document, definition.SearchHint, 1.3)
	addWeightedField(&document, definition.Prompt, 1.0)
	return document
}

func addWeightedField(document *bm25Document, text string, weight float64) {
	for _, term := range searchTokens(text) {
		document.TermFrequencies[term] += weight
		document.Length += weight
	}
}

func bm25Score(document bm25Document, corpus bm25Corpus, terms []string) float64 {
	if corpus.TotalDocuments == 0 || document.Length == 0 {
		return 0
	}
	const k1 = 1.2
	const b = 0.75
	var score float64
	for _, term := range terms {
		tf := document.TermFrequencies[term]
		if tf == 0 {
			continue
		}
		df := corpus.DocumentCounts[term]
		idf := math.Log(1 + (float64(corpus.TotalDocuments)-float64(df)+0.5)/(float64(df)+0.5))
		denominator := tf + k1*(1-b+b*(document.Length/corpus.AverageDocSize))
		score += idf * ((tf * (k1 + 1)) / denominator)
	}
	return score
}

func roundScore(score float64) float64 {
	return math.Round(score*10000) / 10000
}

func searchTokens(text string) []string {
	var builder strings.Builder
	var previous rune
	for _, r := range text {
		if unicode.IsLetter(r) || unicode.IsDigit(r) {
			if shouldSplitCamel(previous, r) {
				builder.WriteByte(' ')
			}
			builder.WriteRune(unicode.ToLower(r))
		} else {
			builder.WriteByte(' ')
		}
		previous = r
	}
	terms := strings.Fields(builder.String())
	compact := compactToken(text)
	if compact != "" && len(terms) > 1 && !stringSliceContains(terms, compact) {
		terms = append(terms, compact)
	}
	return terms
}

func shouldSplitCamel(previous rune, current rune) bool {
	return previous != 0 && unicode.IsLower(previous) && unicode.IsUpper(current)
}

func compactToken(text string) string {
	var builder strings.Builder
	for _, r := range text {
		if unicode.IsLetter(r) || unicode.IsDigit(r) {
			builder.WriteRune(unicode.ToLower(r))
		}
	}
	return builder.String()
}

func uniqueStrings(values []string) []string {
	seen := map[string]struct{}{}
	out := make([]string, 0, len(values))
	for _, value := range values {
		if value == "" {
			continue
		}
		if _, ok := seen[value]; ok {
			continue
		}
		seen[value] = struct{}{}
		out = append(out, value)
	}
	return out
}

func stringSliceContains(values []string, target string) bool {
	for _, value := range values {
		if value == target {
			return true
		}
	}
	return false
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

func copyJSONSchema(schema contracts.JSONSchema) contracts.JSONSchema {
	if schema == nil {
		return nil
	}
	copied := make(contracts.JSONSchema, len(schema))
	for key, value := range schema {
		copied[key] = copySchemaValue(value)
	}
	return copied
}

func copySchemaValue(value any) any {
	switch typed := value.(type) {
	case map[string]any:
		copied := make(map[string]any, len(typed))
		for key, child := range typed {
			copied[key] = copySchemaValue(child)
		}
		return copied
	case contracts.JSONSchema:
		return copyJSONSchema(typed)
	case []any:
		copied := make([]any, len(typed))
		for i, child := range typed {
			copied[i] = copySchemaValue(child)
		}
		return copied
	default:
		return value
	}
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
