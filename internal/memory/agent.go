package memory

import (
	"context"
	"encoding/json"
	"fmt"
	"sort"
	"strings"

	"ccgo/internal/api/anthropic"
	"ccgo/internal/contracts"
	msgs "ccgo/internal/messages"
)

type MessageClient interface {
	CreateMessage(context.Context, anthropic.Request) (*anthropic.Response, error)
}

type Agent struct {
	Client    MessageClient
	Model     string
	MaxTokens int
}

type AgentExtractResult struct {
	Facts    []MemoryFact
	Request  anthropic.Request
	Response *anthropic.Response
	Fallback bool
}

type AgentRecallResult struct {
	Query       string
	SelectedIDs []contracts.ID
	Matches     []RecallMatch
	Request     anthropic.Request
	Response    *anthropic.Response
	Fallback    bool
}

func (a Agent) Extract(ctx context.Context, history []contracts.Message, options ExtractOptions) (AgentExtractResult, error) {
	if a.Client == nil {
		return AgentExtractResult{Facts: ExtractFacts(history, options), Fallback: true}, nil
	}
	request := a.buildExtractRequest(history, options)
	response, err := a.Client.CreateMessage(ctx, request)
	if err != nil {
		return AgentExtractResult{Facts: ExtractFacts(history, options), Request: request, Fallback: true}, nil
	}
	facts, err := parseFacts(responseText(response))
	if err != nil || len(facts) == 0 {
		return AgentExtractResult{Facts: ExtractFacts(history, options), Request: request, Response: response, Fallback: true}, nil
	}
	if options.Limit > 0 && len(facts) > options.Limit {
		facts = facts[:options.Limit]
	}
	return AgentExtractResult{Facts: facts, Request: request, Response: response}, nil
}

func (a Agent) Recall(ctx context.Context, root string, query string, options RecallOptions) (AgentRecallResult, error) {
	searchQuery := strings.TrimSpace(query)
	var selectedIDs []contracts.ID
	var request anthropic.Request
	var response *anthropic.Response
	fallback := false
	if a.Client != nil {
		candidates, err := recallAgentCandidates(root, query, recallCandidateOptions(options))
		if err != nil {
			return AgentRecallResult{}, err
		}
		request = a.buildRecallRequest(query, candidates)
		var responseErr error
		response, responseErr = a.Client.CreateMessage(ctx, request)
		if responseErr != nil {
			fallback = true
		} else {
			expanded, ids, ok := parseRecallAgentResponse(responseText(response))
			switch {
			case ok && expanded != "":
				searchQuery = expanded
				selectedIDs = ids
			case ok && len(ids) > 0:
				selectedIDs = ids
			case expanded != "":
				searchQuery = expanded
			default:
				fallback = true
			}
		}
	} else {
		fallback = true
	}
	if len(selectedIDs) > 0 {
		matches, err := recallMatchesBySessionIDs(root, selectedIDs, searchQuery, options)
		if err != nil {
			return AgentRecallResult{}, err
		}
		if len(matches) > 0 {
			return AgentRecallResult{Query: searchQuery, SelectedIDs: selectedIDs, Matches: matches, Request: request, Response: response, Fallback: fallback}, nil
		}
		fallback = true
	}
	matches, err := RecallSessionSummaries(root, searchQuery, options)
	if err != nil {
		return AgentRecallResult{}, err
	}
	return AgentRecallResult{Query: searchQuery, SelectedIDs: selectedIDs, Matches: matches, Request: request, Response: response, Fallback: fallback}, nil
}

func (a Agent) buildExtractRequest(history []contracts.Message, options ExtractOptions) anthropic.Request {
	return anthropic.Request{
		Model:     a.model(),
		MaxTokens: a.maxTokens(),
		Messages: []contracts.APIMessage{{
			Role: "user",
			Content: []contracts.ContentBlock{contracts.NewTextBlock(
				"Extract durable memory facts from this conversation. Return only JSON array entries with kind, text, source_uuid. Valid kind values are preference, request, decision, tool. Limit: " +
					fmt.Sprint(extractLimit(options)) + "\n\n" + transcriptForMemory(history),
			)},
		}},
	}
}

func (a Agent) buildRecallRequest(query string, candidates []RecallMatch) anthropic.Request {
	var b strings.Builder
	b.WriteString("Select relevant session memories for the user request. Return a JSON object with keys query and session_ids. The query should be a concise search query. session_ids must be ordered from most to least relevant and use only candidate IDs. Return no prose.\n\nUser request:\n")
	b.WriteString(strings.TrimSpace(query))
	if len(candidates) > 0 {
		b.WriteString("\n\nCandidate session summaries:\n")
		for _, candidate := range candidates {
			b.WriteString("- id: ")
			b.WriteString(string(candidate.Summary.SessionID))
			b.WriteString("\n  updated_at: ")
			if !candidate.Summary.UpdatedAt.IsZero() {
				b.WriteString(candidate.Summary.UpdatedAt.UTC().Format("2006-01-02T15:04:05Z"))
			}
			b.WriteString("\n  summary: ")
			b.WriteString(snippet(candidate.Summary.Summary, 480))
			b.WriteString("\n")
		}
	}
	return anthropic.Request{
		Model:     a.model(),
		MaxTokens: a.maxTokens(),
		Messages: []contracts.APIMessage{{
			Role:    "user",
			Content: []contracts.ContentBlock{contracts.NewTextBlock(b.String())},
		}},
	}
}

func (a Agent) model() string {
	if a.Model != "" {
		return a.Model
	}
	return "sonnet"
}

func (a Agent) maxTokens() int {
	if a.MaxTokens > 0 {
		return a.MaxTokens
	}
	return 512
}

func extractLimit(options ExtractOptions) int {
	if options.Limit > 0 {
		return options.Limit
	}
	return 20
}

func recallCandidateOptions(options RecallOptions) RecallOptions {
	out := options
	if out.CandidateLimit <= 0 {
		switch {
		case out.Limit > 0 && out.Limit*4 > 20:
			out.CandidateLimit = out.Limit * 4
		default:
			out.CandidateLimit = 20
		}
	}
	out.Limit = out.CandidateLimit
	return out
}

func recallAgentCandidates(root string, query string, options RecallOptions) ([]RecallMatch, error) {
	summaries, err := LoadSessionSummaries(root)
	if err != nil {
		return nil, err
	}
	terms := queryTerms(query)
	var matches []RecallMatch
	for _, summary := range summaries {
		if options.ExcludeSessionID != "" && summary.SessionID == options.ExcludeSessionID {
			continue
		}
		matches = append(matches, RecallMatch{
			Summary: summary,
			Score:   recallScore(summary, terms),
			Snippet: matchSnippet(summary.Summary, terms, 240),
		})
	}
	sort.SliceStable(matches, func(i, j int) bool {
		if matches[i].Score != matches[j].Score {
			return matches[i].Score > matches[j].Score
		}
		return matches[i].Summary.UpdatedAt.After(matches[j].Summary.UpdatedAt)
	})
	if options.Limit > 0 && len(matches) > options.Limit {
		matches = matches[:options.Limit]
	}
	return matches, nil
}

func parseRecallAgentResponse(raw string) (string, []contracts.ID, bool) {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return "", nil, false
	}
	payload := stripMarkdownFence(raw)
	if query, ids, ok := parseRecallAgentJSON(payload); ok {
		return query, ids, true
	}
	if startsJSONValue(payload) {
		return "", nil, false
	}
	if payload, ok := firstJSONValue(raw); ok {
		if query, ids, parsed := parseRecallAgentJSON(payload); parsed {
			return query, ids, true
		}
		return "", nil, false
	}
	return raw, nil, true
}

func parseRecallAgentJSON(raw string) (string, []contracts.ID, bool) {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return "", nil, false
	}
	var object struct {
		Query              string            `json:"query"`
		SearchQuery        string            `json:"search_query"`
		SessionID          string            `json:"session_id"`
		SessionIDs         []string          `json:"session_ids"`
		SessionIDsCamel    []string          `json:"sessionIds"`
		SelectedSessionID  string            `json:"selected_session_id"`
		SelectedSessionIDs []string          `json:"selected_session_ids"`
		SelectedIDs        []string          `json:"selected_ids"`
		RelevantSessionIDs []string          `json:"relevant_session_ids"`
		ID                 string            `json:"id"`
		IDs                []string          `json:"ids"`
		Matches            []json.RawMessage `json:"matches"`
		Memories           []json.RawMessage `json:"memories"`
		Sessions           []json.RawMessage `json:"sessions"`
		SelectedSessions   []json.RawMessage `json:"selected_sessions"`
	}
	if err := json.Unmarshal([]byte(raw), &object); err == nil {
		ids := recallIDs(append([]string{object.SessionID}, object.SessionIDs...))
		if len(ids) == 0 {
			ids = recallIDs(object.SessionIDsCamel)
		}
		if len(ids) == 0 {
			ids = recallIDs(append([]string{object.SelectedSessionID}, object.SelectedSessionIDs...))
		}
		if len(ids) == 0 {
			ids = recallIDs(append(object.SelectedIDs, object.RelevantSessionIDs...))
		}
		if len(ids) == 0 {
			ids = recallIDs(append([]string{object.ID}, object.IDs...))
		}
		if len(ids) == 0 {
			ids = recallIDsFromRawItems(object.Matches, object.Memories, object.Sessions, object.SelectedSessions)
		}
		query := strings.TrimSpace(object.Query)
		if query == "" {
			query = strings.TrimSpace(object.SearchQuery)
		}
		return query, ids, query != "" || len(ids) > 0
	}
	var ids []string
	if err := json.Unmarshal([]byte(raw), &ids); err == nil {
		return "", recallIDs(ids), len(ids) > 0
	}
	var items []json.RawMessage
	if err := json.Unmarshal([]byte(raw), &items); err == nil {
		parsedIDs := recallIDsFromRawItems(items)
		return "", parsedIDs, len(parsedIDs) > 0
	}
	return "", nil, false
}

func recallIDsFromRawItems(groups ...[]json.RawMessage) []contracts.ID {
	var raw []string
	for _, group := range groups {
		for _, item := range group {
			var id string
			if err := json.Unmarshal(item, &id); err == nil {
				raw = append(raw, id)
				continue
			}
			var object struct {
				SessionID       string `json:"session_id"`
				SessionIDCamel  string `json:"sessionId"`
				SelectedID      string `json:"selected_id"`
				SelectedSession string `json:"selected_session"`
				ID              string `json:"id"`
				UUID            string `json:"uuid"`
			}
			if err := json.Unmarshal(item, &object); err == nil {
				raw = append(raw, object.SessionID, object.SessionIDCamel, object.SelectedID, object.SelectedSession, object.ID, object.UUID)
			}
		}
	}
	return recallIDs(raw)
}

func stripMarkdownFence(raw string) string {
	raw = strings.TrimSpace(raw)
	if !strings.HasPrefix(raw, "```") {
		return raw
	}
	lineEnd := strings.IndexByte(raw, '\n')
	if lineEnd < 0 {
		return strings.TrimSpace(strings.Trim(raw, "`"))
	}
	body := raw[lineEnd+1:]
	if end := strings.LastIndex(body, "```"); end >= 0 {
		body = body[:end]
	}
	return strings.TrimSpace(body)
}

func firstJSONValue(raw string) (string, bool) {
	for index, r := range raw {
		if r != '{' && r != '[' {
			continue
		}
		var payload json.RawMessage
		decoder := json.NewDecoder(strings.NewReader(raw[index:]))
		if err := decoder.Decode(&payload); err == nil && len(payload) > 0 {
			return string(payload), true
		}
	}
	return "", false
}

func startsJSONValue(raw string) bool {
	raw = strings.TrimSpace(raw)
	return strings.HasPrefix(raw, "{") || strings.HasPrefix(raw, "[")
}

func recallIDs(raw []string) []contracts.ID {
	seen := map[contracts.ID]struct{}{}
	var ids []contracts.ID
	for _, value := range raw {
		id := contracts.ID(strings.TrimSpace(value))
		if id == "" {
			continue
		}
		if _, ok := seen[id]; ok {
			continue
		}
		seen[id] = struct{}{}
		ids = append(ids, id)
	}
	return ids
}

func recallMatchesBySessionIDs(root string, ids []contracts.ID, query string, options RecallOptions) ([]RecallMatch, error) {
	summaries, err := LoadSessionSummaries(root)
	if err != nil {
		return nil, err
	}
	byID := map[contracts.ID]SessionSummary{}
	for _, summary := range summaries {
		if options.ExcludeSessionID != "" && summary.SessionID == options.ExcludeSessionID {
			continue
		}
		byID[summary.SessionID] = summary
	}
	terms := queryTerms(query)
	var matches []RecallMatch
	for _, id := range ids {
		summary, ok := byID[id]
		if !ok {
			continue
		}
		matches = append(matches, RecallMatch{
			Summary: summary,
			Score:   recallScore(summary, terms),
			Snippet: matchSnippet(summary.Summary, terms, 240),
		})
	}
	if options.Limit > 0 && len(matches) > options.Limit {
		matches = matches[:options.Limit]
	}
	return matches, nil
}

func transcriptForMemory(history []contracts.Message) string {
	var lines []string
	for _, message := range history {
		role := string(message.Type)
		if role == "" {
			role = "message"
		}
		text := strings.TrimSpace(msgs.TextContent(message))
		if text != "" {
			lines = append(lines, fmt.Sprintf("%s %s: %s", role, message.UUID, text))
		}
		for _, block := range message.Content {
			if block.Type == contracts.ContentToolUse {
				lines = append(lines, fmt.Sprintf("%s %s: tool_use %s", role, message.UUID, block.Name))
			}
		}
	}
	return strings.Join(lines, "\n")
}

func parseFacts(raw string) ([]MemoryFact, error) {
	raw = strings.TrimSpace(raw)
	payload := stripMarkdownFence(raw)
	facts, err := parseFactsJSON(payload)
	if err == nil {
		return facts, nil
	}
	if startsJSONValue(payload) {
		return nil, err
	}
	if payload, ok := firstJSONValue(raw); ok {
		return parseFactsJSON(payload)
	}
	return nil, err
}

func parseFactsJSON(raw string) ([]MemoryFact, error) {
	raw = strings.TrimSpace(raw)
	var value any
	if err := json.Unmarshal([]byte(raw), &value); err != nil {
		return nil, err
	}
	entries := collectRawMemoryFacts(value)
	var facts []MemoryFact
	for _, entry := range entries {
		kind := FactKind(strings.TrimSpace(firstNonEmpty(entry.Kind, entry.Type, entry.FactType, entry.Category, entry.Label)))
		switch kind {
		case FactPreference, FactRequest, FactDecision, FactTool:
		default:
			continue
		}
		text := strings.TrimSpace(firstNonEmpty(entry.Text, entry.Content, entry.Summary, entry.Value, entry.Detail))
		if text == "" {
			continue
		}
		sourceUUID := firstNonEmpty(entry.SourceUUID, entry.SourceUUIDCamel, entry.SourceID, entry.Source, entry.MessageUUID, entry.MessageUUIDCamel, entry.SourceMessageID, entry.SourceMessageIDCamel, entry.UUID)
		facts = append(facts, MemoryFact{Kind: kind, Text: text, SourceUUID: contracts.ID(sourceUUID)})
	}
	return facts, nil
}

func collectRawMemoryFacts(value any) []rawMemoryFact {
	switch typed := value.(type) {
	case []any:
		var entries []rawMemoryFact
		for _, item := range typed {
			entries = append(entries, collectRawMemoryFacts(item)...)
		}
		return entries
	case map[string]any:
		var entries []rawMemoryFact
		if fact, ok := rawMemoryFactFromMap(typed); ok {
			entries = append(entries, fact)
		}
		for _, key := range []string{
			"facts",
			"memory",
			"memories",
			"memory_facts",
			"memoryFacts",
			"extracted_facts",
			"extractedFacts",
			"extracted_memory",
			"extractedMemory",
			"session_memory",
			"sessionMemory",
			"items",
			"entries",
			"results",
		} {
			if child, ok := typed[key]; ok {
				entries = append(entries, collectRawMemoryFacts(child)...)
			}
		}
		return entries
	default:
		return nil
	}
}

func rawMemoryFactFromMap(value map[string]any) (rawMemoryFact, bool) {
	encoded, err := json.Marshal(value)
	if err != nil {
		return rawMemoryFact{}, false
	}
	var fact rawMemoryFact
	if err := json.Unmarshal(encoded, &fact); err != nil {
		return rawMemoryFact{}, false
	}
	if firstNonEmpty(fact.Kind, fact.Type, fact.FactType, fact.Category, fact.Label) == "" {
		return rawMemoryFact{}, false
	}
	if firstNonEmpty(fact.Text, fact.Content, fact.Summary, fact.Value, fact.Detail) == "" {
		return rawMemoryFact{}, false
	}
	return fact, true
}

type rawMemoryFact struct {
	Kind                 string `json:"kind"`
	Type                 string `json:"type"`
	FactType             string `json:"fact_type"`
	Category             string `json:"category"`
	Label                string `json:"label"`
	Text                 string `json:"text"`
	Content              string `json:"content"`
	Summary              string `json:"summary"`
	Value                string `json:"value"`
	Detail               string `json:"detail"`
	SourceUUID           string `json:"source_uuid"`
	SourceUUIDCamel      string `json:"sourceUuid"`
	SourceID             string `json:"source_id"`
	Source               string `json:"source"`
	MessageUUID          string `json:"message_uuid"`
	MessageUUIDCamel     string `json:"messageUuid"`
	SourceMessageID      string `json:"source_message_id"`
	SourceMessageIDCamel string `json:"sourceMessageId"`
	UUID                 string `json:"uuid"`
}

func firstNonEmpty(values ...string) string {
	for _, value := range values {
		value = strings.TrimSpace(value)
		if value != "" {
			return value
		}
	}
	return ""
}

func responseText(response *anthropic.Response) string {
	if response == nil {
		return ""
	}
	var parts []string
	for _, block := range response.Content {
		if block.Type == contracts.ContentText && block.Text != "" {
			parts = append(parts, block.Text)
		}
	}
	return strings.Join(parts, "\n")
}
