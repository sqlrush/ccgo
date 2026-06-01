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
	raw = strings.TrimPrefix(raw, "```json")
	raw = strings.TrimPrefix(raw, "```")
	raw = strings.TrimSuffix(raw, "```")
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return "", nil, false
	}
	var object struct {
		Query              string   `json:"query"`
		SearchQuery        string   `json:"search_query"`
		SessionIDs         []string `json:"session_ids"`
		SessionIDsCamel    []string `json:"sessionIds"`
		SelectedSessionIDs []string `json:"selected_session_ids"`
		IDs                []string `json:"ids"`
	}
	if err := json.Unmarshal([]byte(raw), &object); err == nil {
		ids := recallIDs(object.SessionIDs)
		if len(ids) == 0 {
			ids = recallIDs(object.SessionIDsCamel)
		}
		if len(ids) == 0 {
			ids = recallIDs(object.SelectedSessionIDs)
		}
		if len(ids) == 0 {
			ids = recallIDs(object.IDs)
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
	return raw, nil, true
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
	raw = strings.TrimPrefix(raw, "```json")
	raw = strings.TrimPrefix(raw, "```")
	raw = strings.TrimSuffix(raw, "```")
	raw = strings.TrimSpace(raw)
	var entries []struct {
		Kind       string `json:"kind"`
		Text       string `json:"text"`
		SourceUUID string `json:"source_uuid"`
	}
	if err := json.Unmarshal([]byte(raw), &entries); err != nil {
		return nil, err
	}
	var facts []MemoryFact
	for _, entry := range entries {
		kind := FactKind(strings.TrimSpace(entry.Kind))
		switch kind {
		case FactPreference, FactRequest, FactDecision, FactTool:
		default:
			continue
		}
		text := strings.TrimSpace(entry.Text)
		if text == "" {
			continue
		}
		facts = append(facts, MemoryFact{Kind: kind, Text: text, SourceUUID: contracts.ID(entry.SourceUUID)})
	}
	return facts, nil
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
