package memory

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/url"
	"path/filepath"
	"sort"
	"strconv"
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

type AgentRelevantMemoryResult struct {
	Query       string
	SelectedIDs []string
	Selected    []RelevantMemorySelection
	Request     anthropic.Request
	Response    *anthropic.Response
	Fallback    bool
}

type RelevantMemorySelectorOptions struct {
	Limit          int
	CandidateLimit int
	RecentTools    []string
	Surfaced       map[string]struct{}
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
		request = a.buildRecallRequest(query, candidates, options)
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

func (a Agent) SelectRelevantMemories(ctx context.Context, root string, query string, options RelevantMemorySelectorOptions) (AgentRelevantMemoryResult, error) {
	searchQuery := strings.TrimSpace(query)
	var selectedIDs []string
	var selected []RelevantMemorySelection
	var request anthropic.Request
	var response *anthropic.Response
	fallback := false
	if a.Client != nil {
		candidates, err := relevantMemoryAgentCandidates(root, searchQuery, options)
		if err != nil {
			return AgentRelevantMemoryResult{}, err
		}
		request = a.buildRelevantMemorySelectorRequest(searchQuery, candidates, options)
		var responseErr error
		response, responseErr = a.Client.CreateMessage(ctx, request)
		if responseErr != nil {
			fallback = true
		} else {
			expanded, ids, ok := parseRelevantMemoryAgentResponse(responseText(response))
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
			if len(selectedIDs) > 0 {
				selected = relevantMemorySelectionsByIDs(candidates, selectedIDs, options.Surfaced, options.Limit)
				if len(selected) > 0 {
					return AgentRelevantMemoryResult{
						Query:       searchQuery,
						SelectedIDs: selectedIDs,
						Selected:    selected,
						Request:     request,
						Response:    response,
						Fallback:    fallback,
					}, nil
				}
				fallback = true
			}
		}
	} else {
		fallback = true
	}
	selected, err := FindRelevantMemorySelections(root, searchQuery, options.RecentTools, options.Surfaced, options.Limit)
	if err != nil {
		return AgentRelevantMemoryResult{}, err
	}
	return AgentRelevantMemoryResult{
		Query:       searchQuery,
		SelectedIDs: selectedIDs,
		Selected:    selected,
		Request:     request,
		Response:    response,
		Fallback:    fallback,
	}, nil
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

func (a Agent) buildRecallRequest(query string, candidates []RecallMatch, options RecallOptions) anthropic.Request {
	var b strings.Builder
	b.WriteString("Select relevant session memories for the user request. Return a JSON object with keys query and session_ids. The query should be a concise search query. session_ids must be ordered from most to least relevant and use only candidate IDs. Return no prose.\n\nUser request:\n")
	b.WriteString(strings.TrimSpace(query))
	if options.Limit > 0 {
		b.WriteString("\n\nReturn at most ")
		b.WriteString(fmt.Sprint(options.Limit))
		b.WriteString(" session_ids.")
	}
	if options.ExcludeSessionID != "" {
		b.WriteString("\nDo not select excluded current session id: ")
		b.WriteString(string(options.ExcludeSessionID))
	}
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

func (a Agent) buildRelevantMemorySelectorRequest(query string, candidates []relevantMemoryCandidate, options RelevantMemorySelectorOptions) anthropic.Request {
	var b strings.Builder
	b.WriteString("Select relevant memory files for the user request. Return a JSON object with keys query and memory_paths. The query should be a concise search query. memory_paths must be ordered from most to least relevant and use only candidate ids or paths. Return no prose.\n\nUser request:\n")
	b.WriteString(strings.TrimSpace(query))
	if len(options.RecentTools) > 0 {
		b.WriteString("\n\nRecent successful tools in this turn:\n")
		for _, toolName := range limitStrings(options.RecentTools, 12) {
			b.WriteString("- ")
			b.WriteString(toolName)
			b.WriteString("\n")
		}
		b.WriteString("Prefer memories that add durable context beyond these tool names/results.\n")
	}
	if len(options.Surfaced) > 0 {
		b.WriteString("\n\nAlready surfaced memory paths to avoid selecting again:\n")
		for _, path := range limitedSortedMapKeys(options.Surfaced, 12) {
			b.WriteString("- ")
			b.WriteString(path)
			b.WriteString("\n")
		}
	}
	if len(candidates) > 0 {
		b.WriteString("\n\nCandidate memory files:\n")
		for _, candidate := range candidates {
			header := candidate.Header
			b.WriteString("- id: ")
			b.WriteString(header.Filename)
			b.WriteString("\n  path: ")
			b.WriteString(header.Path)
			b.WriteString("\n  saved_at: ")
			if !header.Mtime.IsZero() {
				b.WriteString(header.Mtime.UTC().Format("2006-01-02T15:04:05Z"))
			}
			if header.Type != "" {
				b.WriteString("\n  type: ")
				b.WriteString(string(header.Type))
			}
			b.WriteString("\n  description: ")
			b.WriteString(snippet(header.Description, 360))
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

func limitStrings(values []string, limit int) []string {
	if limit <= 0 || len(values) <= limit {
		return values
	}
	return values[:limit]
}

func limitedSortedMapKeys(values map[string]struct{}, limit int) []string {
	keys := make([]string, 0, len(values))
	for key := range values {
		if strings.TrimSpace(key) != "" {
			keys = append(keys, key)
		}
	}
	sort.Strings(keys)
	return limitStrings(keys, limit)
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

type relevantMemoryCandidate struct {
	Header Header
	Score  int
}

func relevantMemoryAgentCandidates(root string, query string, options RelevantMemorySelectorOptions) ([]relevantMemoryCandidate, error) {
	headers, err := ScanDirectory(root, ScanOptions{})
	if err != nil {
		return nil, err
	}
	terms := queryTerms(query)
	candidates := make([]relevantMemoryCandidate, 0, len(headers))
	for _, header := range headers {
		if options.Surfaced != nil {
			if _, ok := options.Surfaced[header.Path]; ok {
				continue
			}
		}
		haystack := strings.ToLower(header.Filename + " " + header.Description)
		if suppressRecentToolReference(haystack, options.RecentTools) {
			continue
		}
		score := 0
		for _, term := range terms {
			score += strings.Count(haystack, term)
		}
		candidates = append(candidates, relevantMemoryCandidate{Header: header, Score: score})
	}
	sort.SliceStable(candidates, func(i, j int) bool {
		if candidates[i].Score != candidates[j].Score {
			return candidates[i].Score > candidates[j].Score
		}
		return candidates[i].Header.Mtime.After(candidates[j].Header.Mtime)
	})
	limit := options.CandidateLimit
	if limit <= 0 {
		switch {
		case options.Limit > 0 && options.Limit*4 > 20:
			limit = options.Limit * 4
		default:
			limit = 20
		}
	}
	if len(candidates) > limit {
		candidates = candidates[:limit]
	}
	return candidates, nil
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

func parseRelevantMemoryAgentResponse(raw string) (string, []string, bool) {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return "", nil, false
	}
	payload := stripMarkdownFence(raw)
	if query, ids, ok := parseRelevantMemoryAgentJSON(payload); ok {
		return query, ids, true
	}
	if startsJSONValue(payload) {
		return "", nil, false
	}
	if payload, ok := firstJSONValue(raw); ok {
		if query, ids, parsed := parseRelevantMemoryAgentJSON(payload); parsed {
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
	var scalar string
	if err := json.Unmarshal([]byte(raw), &scalar); err == nil {
		scalar = strings.TrimSpace(scalar)
		if scalar == "" {
			return "", nil, false
		}
		if startsJSONValue(scalar) {
			return parseRecallAgentJSON(scalar)
		}
		if len(strings.Fields(scalar)) == 1 {
			return "", recallIDs([]string{scalar}), true
		}
		return scalar, nil, true
	}
	if text, ok := selectionProviderResponseText(raw); ok {
		return parseRecallAgentJSON(text)
	}
	var rawObject map[string]json.RawMessage
	queryFromAliases := ""
	if err := json.Unmarshal([]byte(raw), &rawObject); err == nil {
		queryFromAliases = selectionQueryFromRawObject(rawObject)
	}
	var object struct {
		Query                        string            `json:"query"`
		SearchQuery                  string            `json:"search_query"`
		SearchQueryCamel             string            `json:"searchQuery"`
		RewrittenQuery               string            `json:"rewritten_query"`
		RewrittenQueryCamel          string            `json:"rewrittenQuery"`
		ExpandedQuery                string            `json:"expanded_query"`
		ExpandedQueryCamel           string            `json:"expandedQuery"`
		SessionID                    string            `json:"session_id"`
		SessionIDs                   []string          `json:"session_ids"`
		SessionIDsCamel              []string          `json:"sessionIds"`
		ConversationID               string            `json:"conversation_id"`
		ConversationIDCamel          string            `json:"conversationId"`
		ThreadID                     string            `json:"thread_id"`
		ThreadIDCamel                string            `json:"threadId"`
		TranscriptID                 string            `json:"transcript_id"`
		TranscriptIDCamel            string            `json:"transcriptId"`
		SelectedSessionID            string            `json:"selected_session_id"`
		SelectedSessionIDs           []string          `json:"selected_session_ids"`
		SelectedSessionIDsCamel      []string          `json:"selectedSessionIds"`
		SelectedConversationID       string            `json:"selected_conversation_id"`
		SelectedConversationIDCamel  string            `json:"selectedConversationId"`
		SelectedThreadID             string            `json:"selected_thread_id"`
		SelectedThreadIDCamel        string            `json:"selectedThreadId"`
		SelectedTranscriptID         string            `json:"selected_transcript_id"`
		SelectedTranscriptIDCamel    string            `json:"selectedTranscriptId"`
		SelectedIDs                  []string          `json:"selected_ids"`
		SelectedIDsCamel             []string          `json:"selectedIds"`
		RelevantIDs                  []string          `json:"relevant_ids"`
		RelevantIDsCamel             []string          `json:"relevantIds"`
		RelevantSessionIDs           []string          `json:"relevant_session_ids"`
		RelevantSessionIDsCamel      []string          `json:"relevantSessionIds"`
		RelevantConversationID       string            `json:"relevant_conversation_id"`
		RelevantConversationIDCamel  string            `json:"relevantConversationId"`
		RelevantThreadID             string            `json:"relevant_thread_id"`
		RelevantThreadIDCamel        string            `json:"relevantThreadId"`
		RelevantTranscriptID         string            `json:"relevant_transcript_id"`
		RelevantTranscriptIDCamel    string            `json:"relevantTranscriptId"`
		MemoryID                     string            `json:"memory_id"`
		MemoryIDCamel                string            `json:"memoryId"`
		MemoryIDs                    []string          `json:"memory_ids"`
		MemoryIDsCamel               []string          `json:"memoryIds"`
		CandidateID                  string            `json:"candidate_id"`
		CandidateIDCamel             string            `json:"candidateId"`
		CandidateIDs                 []string          `json:"candidate_ids"`
		CandidateIDsCamel            []string          `json:"candidateIds"`
		CandidateConversationID      string            `json:"candidate_conversation_id"`
		CandidateConversationIDCamel string            `json:"candidateConversationId"`
		CandidateThreadID            string            `json:"candidate_thread_id"`
		CandidateThreadIDCamel       string            `json:"candidateThreadId"`
		CandidateTranscriptID        string            `json:"candidate_transcript_id"`
		CandidateTranscriptIDCamel   string            `json:"candidateTranscriptId"`
		SessionURI                   string            `json:"session_uri"`
		SessionURICamel              string            `json:"sessionUri"`
		SessionURL                   string            `json:"session_url"`
		SessionURLCamel              string            `json:"sessionUrl"`
		SessionPath                  string            `json:"session_path"`
		SessionPathCamel             string            `json:"sessionPath"`
		SessionSummaryPath           string            `json:"session_summary_path"`
		SessionSummaryPathCamel      string            `json:"sessionSummaryPath"`
		SummaryPath                  string            `json:"summary_path"`
		SummaryPathCamel             string            `json:"summaryPath"`
		URI                          string            `json:"uri"`
		URIs                         []string          `json:"uris"`
		URL                          string            `json:"url"`
		URLs                         []string          `json:"urls"`
		Href                         string            `json:"href"`
		Hrefs                        []string          `json:"hrefs"`
		Links                        json.RawMessage   `json:"links"`
		HALLinks                     json.RawMessage   `json:"_links"`
		ID                           string            `json:"id"`
		IDs                          []string          `json:"ids"`
		Type                         string            `json:"type"`
		ResourceType                 string            `json:"resource_type"`
		ResourceTypeCamel            string            `json:"resourceType"`
		Kind                         string            `json:"kind"`
		Matches                      []json.RawMessage `json:"matches"`
		Memories                     []json.RawMessage `json:"memories"`
		Sessions                     []json.RawMessage `json:"sessions"`
		Summaries                    []json.RawMessage `json:"summaries"`
		SelectedSessions             []json.RawMessage `json:"selected_sessions"`
		SelectedSessionsCamel        []json.RawMessage `json:"selectedSessions"`
		SelectedConversations        []json.RawMessage `json:"selected_conversations"`
		SelectedConversationsCamel   []json.RawMessage `json:"selectedConversations"`
		SelectedThreads              []json.RawMessage `json:"selected_threads"`
		SelectedThreadsCamel         []json.RawMessage `json:"selectedThreads"`
		SelectedTranscripts          []json.RawMessage `json:"selected_transcripts"`
		SelectedTranscriptsCamel     []json.RawMessage `json:"selectedTranscripts"`
		SelectedMemories             []json.RawMessage `json:"selected_memories"`
		SelectedMemoriesCamel        []json.RawMessage `json:"selectedMemories"`
		SelectedSummaries            []json.RawMessage `json:"selected_summaries"`
		SelectedSummariesCamel       []json.RawMessage `json:"selectedSummaries"`
		RelevantSessions             []json.RawMessage `json:"relevant_sessions"`
		RelevantSessionsCamel        []json.RawMessage `json:"relevantSessions"`
		RelevantConversations        []json.RawMessage `json:"relevant_conversations"`
		RelevantConversationsCamel   []json.RawMessage `json:"relevantConversations"`
		RelevantThreads              []json.RawMessage `json:"relevant_threads"`
		RelevantThreadsCamel         []json.RawMessage `json:"relevantThreads"`
		RelevantTranscripts          []json.RawMessage `json:"relevant_transcripts"`
		RelevantTranscriptsCamel     []json.RawMessage `json:"relevantTranscripts"`
		RelevantMemories             []json.RawMessage `json:"relevant_memories"`
		RelevantMemoriesCamel        []json.RawMessage `json:"relevantMemories"`
		RelevantSummaries            []json.RawMessage `json:"relevant_summaries"`
		RelevantSummariesCamel       []json.RawMessage `json:"relevantSummaries"`
		CandidateSessions            []json.RawMessage `json:"candidate_sessions"`
		CandidateSessionsCamel       []json.RawMessage `json:"candidateSessions"`
		CandidateConversations       []json.RawMessage `json:"candidate_conversations"`
		CandidateConversationsCamel  []json.RawMessage `json:"candidateConversations"`
		CandidateThreads             []json.RawMessage `json:"candidate_threads"`
		CandidateThreadsCamel        []json.RawMessage `json:"candidateThreads"`
		CandidateTranscripts         []json.RawMessage `json:"candidate_transcripts"`
		CandidateTranscriptsCamel    []json.RawMessage `json:"candidateTranscripts"`
		CandidateMemories            []json.RawMessage `json:"candidate_memories"`
		CandidateMemoriesCamel       []json.RawMessage `json:"candidateMemories"`
		CandidateSummaries           []json.RawMessage `json:"candidate_summaries"`
		CandidateSummariesCamel      []json.RawMessage `json:"candidateSummaries"`
		Candidates                   []json.RawMessage `json:"candidates"`
		Results                      []json.RawMessage `json:"results"`
		Nodes                        []json.RawMessage `json:"nodes"`
		Edges                        []json.RawMessage `json:"edges"`
		Items                        []json.RawMessage `json:"items"`
		Resources                    []json.RawMessage `json:"resources"`
		Included                     json.RawMessage   `json:"included"`
		Collection                   json.RawMessage   `json:"collection"`
		List                         json.RawMessage   `json:"list"`
		Children                     json.RawMessage   `json:"children"`
		Values                       json.RawMessage   `json:"values"`
		Records                      json.RawMessage   `json:"records"`
		Entries                      json.RawMessage   `json:"entries"`
		Selection                    json.RawMessage   `json:"selection"`
		Selected                     json.RawMessage   `json:"selected"`
		Data                         json.RawMessage   `json:"data"`
		Payload                      json.RawMessage   `json:"payload"`
		Body                         json.RawMessage   `json:"body"`
		Resource                     json.RawMessage   `json:"resource"`
		Attributes                   json.RawMessage   `json:"attributes"`
		Properties                   json.RawMessage   `json:"properties"`
		Attrs                        json.RawMessage   `json:"attrs"`
		Viewer                       json.RawMessage   `json:"viewer"`
		Edge                         json.RawMessage   `json:"edge"`
		Node                         json.RawMessage   `json:"node"`
		Result                       json.RawMessage   `json:"result"`
		Response                     json.RawMessage   `json:"response"`
		Recall                       json.RawMessage   `json:"recall"`
		MemoryRecall                 json.RawMessage   `json:"memory_recall"`
		MemoryRecallCamel            json.RawMessage   `json:"memoryRecall"`
	}
	if err := json.Unmarshal([]byte(raw), &object); err == nil {
		ids := recallIDs(append([]string{object.SessionID}, object.SessionIDs...))
		if len(ids) == 0 {
			ids = recallIDs(object.SessionIDsCamel)
		}
		if len(ids) == 0 {
			ids = recallIDs([]string{object.ConversationID, object.ConversationIDCamel, object.ThreadID, object.ThreadIDCamel, object.TranscriptID, object.TranscriptIDCamel})
		}
		if len(ids) == 0 {
			ids = recallIDs(append([]string{object.SelectedSessionID}, object.SelectedSessionIDs...))
		}
		if len(ids) == 0 {
			ids = recallIDs(object.SelectedSessionIDsCamel)
		}
		if len(ids) == 0 {
			ids = recallIDs([]string{object.SelectedConversationID, object.SelectedConversationIDCamel, object.SelectedThreadID, object.SelectedThreadIDCamel, object.SelectedTranscriptID, object.SelectedTranscriptIDCamel})
		}
		if len(ids) == 0 {
			ids = recallIDs(append(object.SelectedIDs, object.SelectedIDsCamel...))
		}
		if len(ids) == 0 {
			ids = recallIDs(append(object.RelevantIDs, object.RelevantIDsCamel...))
		}
		if len(ids) == 0 {
			ids = recallIDs(object.RelevantSessionIDs)
		}
		if len(ids) == 0 {
			ids = recallIDs(object.RelevantSessionIDsCamel)
		}
		if len(ids) == 0 {
			ids = recallIDs([]string{object.RelevantConversationID, object.RelevantConversationIDCamel, object.RelevantThreadID, object.RelevantThreadIDCamel, object.RelevantTranscriptID, object.RelevantTranscriptIDCamel})
		}
		if len(ids) == 0 {
			ids = recallIDs(append([]string{object.MemoryID, object.MemoryIDCamel}, append(object.MemoryIDs, object.MemoryIDsCamel...)...))
		}
		if len(ids) == 0 {
			ids = recallIDs(append([]string{object.CandidateID, object.CandidateIDCamel}, append(object.CandidateIDs, object.CandidateIDsCamel...)...))
		}
		if len(ids) == 0 {
			ids = recallIDs([]string{object.CandidateConversationID, object.CandidateConversationIDCamel, object.CandidateThreadID, object.CandidateThreadIDCamel, object.CandidateTranscriptID, object.CandidateTranscriptIDCamel})
		}
		if len(ids) == 0 {
			ids = recallIDs(append(
				[]string{object.SessionURI, object.SessionURICamel, object.SessionURL, object.SessionURLCamel, object.SessionPath, object.SessionPathCamel, object.SessionSummaryPath, object.SessionSummaryPathCamel, object.SummaryPath, object.SummaryPathCamel, object.URI, object.URL, object.Href},
				appendManyStringSlices(object.URIs, object.URLs, object.Hrefs)...,
			))
		}
		if len(ids) == 0 {
			ids = recallIDsFromSelectionLinks(object.Links, object.HALLinks)
		}
		if len(ids) == 0 {
			if recallResourceTypeAllowsBareID(normalizedSelectionResourceTypeFromStrings(object.Type, object.ResourceType, object.ResourceTypeCamel, object.Kind)) {
				ids = recallIDs(append([]string{object.ID}, object.IDs...))
			}
		}
		if len(ids) == 0 {
			ids = recallIDsFromRawItems(
				object.Matches,
				object.Memories,
				object.Sessions,
				object.Summaries,
				object.SelectedSessions,
				object.SelectedSessionsCamel,
				object.SelectedConversations,
				object.SelectedConversationsCamel,
				object.SelectedThreads,
				object.SelectedThreadsCamel,
				object.SelectedTranscripts,
				object.SelectedTranscriptsCamel,
				object.SelectedMemories,
				object.SelectedMemoriesCamel,
				object.SelectedSummaries,
				object.SelectedSummariesCamel,
				object.RelevantSessions,
				object.RelevantSessionsCamel,
				object.RelevantConversations,
				object.RelevantConversationsCamel,
				object.RelevantThreads,
				object.RelevantThreadsCamel,
				object.RelevantTranscripts,
				object.RelevantTranscriptsCamel,
				object.RelevantMemories,
				object.RelevantMemoriesCamel,
				object.RelevantSummaries,
				object.RelevantSummariesCamel,
				object.CandidateSessions,
				object.CandidateSessionsCamel,
				object.CandidateConversations,
				object.CandidateConversationsCamel,
				object.CandidateThreads,
				object.CandidateThreadsCamel,
				object.CandidateTranscripts,
				object.CandidateTranscriptsCamel,
				object.CandidateMemories,
				object.CandidateMemoriesCamel,
				object.CandidateSummaries,
				object.CandidateSummariesCamel,
				object.Candidates,
				object.Results,
				object.Nodes,
				object.Edges,
				object.Items,
				object.Resources,
			)
		}
		query := strings.TrimSpace(object.Query)
		if query == "" {
			query = strings.TrimSpace(object.SearchQuery)
		}
		if query == "" {
			query = strings.TrimSpace(object.SearchQueryCamel)
		}
		if query == "" {
			query = strings.TrimSpace(object.RewrittenQuery)
		}
		if query == "" {
			query = strings.TrimSpace(object.RewrittenQueryCamel)
		}
		if query == "" {
			query = strings.TrimSpace(object.ExpandedQuery)
		}
		if query == "" {
			query = strings.TrimSpace(object.ExpandedQueryCamel)
		}
		if query == "" {
			query = queryFromAliases
		}
		if len(ids) == 0 {
			query, ids = recallSelectionFromNestedPayloads(query,
				object.Selection,
				object.Selected,
				object.Data,
				object.Payload,
				object.Body,
				object.Resource,
				object.Attributes,
				object.Properties,
				object.Attrs,
				object.Viewer,
				object.Edge,
				object.Node,
				object.Included,
				object.Collection,
				object.List,
				object.Children,
				object.Values,
				object.Records,
				object.Entries,
				object.Result,
				object.Response,
				object.Recall,
				object.MemoryRecall,
				object.MemoryRecallCamel,
			)
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

func parseRelevantMemoryAgentJSON(raw string) (string, []string, bool) {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return "", nil, false
	}
	var scalar string
	if err := json.Unmarshal([]byte(raw), &scalar); err == nil {
		scalar = strings.TrimSpace(scalar)
		if scalar == "" {
			return "", nil, false
		}
		if startsJSONValue(scalar) {
			return parseRelevantMemoryAgentJSON(scalar)
		}
		if relevantMemoryLooksLikeID(scalar) {
			return "", relevantMemoryIDs([]string{scalar}), true
		}
		return scalar, nil, true
	}
	if text, ok := selectionProviderResponseText(raw); ok {
		return parseRelevantMemoryAgentJSON(text)
	}
	var rawObject map[string]json.RawMessage
	if err := json.Unmarshal([]byte(raw), &rawObject); err == nil {
		query := selectionQueryFromRawObject(rawObject)
		ids := relevantMemoryIDsFromRawObject(rawObject)
		if len(ids) == 0 {
			query, ids = relevantMemorySelectionFromNestedPayloads(query, rawObjectValues(rawObject,
				"selected",
				"selection",
				"data",
				"payload",
				"body",
				"resource",
				"attributes",
				"properties",
				"attrs",
				"viewer",
				"edge",
				"node",
				"nodes",
				"edges",
				"matches",
				"memories",
				"files",
				"files_list",
				"filesList",
				"items",
				"resources",
				"links",
				"_links",
				"included",
				"collection",
				"list",
				"children",
				"values",
				"records",
				"entries",
				"result",
				"response",
				"memory_selection",
				"memorySelection",
				"relevant_memory_selection",
				"relevantMemorySelection",
			)...)
		}
		if query != "" || len(ids) > 0 {
			return query, ids, true
		}
	}
	var object struct {
		Query                        string            `json:"query"`
		SearchQuery                  string            `json:"search_query"`
		SearchQueryCamel             string            `json:"searchQuery"`
		RewrittenQuery               string            `json:"rewritten_query"`
		RewrittenQueryCamel          string            `json:"rewrittenQuery"`
		ExpandedQuery                string            `json:"expanded_query"`
		ExpandedQueryCamel           string            `json:"expandedQuery"`
		MemoryPath                   string            `json:"memory_path"`
		MemoryPathCamel              string            `json:"memoryPath"`
		MemoryPaths                  []string          `json:"memory_paths"`
		MemoryPathsCamel             []string          `json:"memoryPaths"`
		SelectedMemoryPath           string            `json:"selected_memory_path"`
		SelectedMemoryPathCamel      string            `json:"selectedMemoryPath"`
		SelectedMemoryPaths          []string          `json:"selected_memory_paths"`
		SelectedMemoryPathsCamel     []string          `json:"selectedMemoryPaths"`
		RelevantMemoryPath           string            `json:"relevant_memory_path"`
		RelevantMemoryPathCamel      string            `json:"relevantMemoryPath"`
		RelevantMemoryPaths          []string          `json:"relevant_memory_paths"`
		RelevantMemoryPathsCamel     []string          `json:"relevantMemoryPaths"`
		FilePath                     string            `json:"file_path"`
		FilePathCamel                string            `json:"filePath"`
		FilePaths                    []string          `json:"file_paths"`
		FilePathsCamel               []string          `json:"filePaths"`
		FileURI                      string            `json:"file_uri"`
		FileURICamel                 string            `json:"fileUri"`
		FileURL                      string            `json:"file_url"`
		FileURLCamel                 string            `json:"fileUrl"`
		Path                         string            `json:"path"`
		Paths                        []string          `json:"paths"`
		URI                          string            `json:"uri"`
		URIs                         []string          `json:"uris"`
		URL                          string            `json:"url"`
		URLs                         []string          `json:"urls"`
		Href                         string            `json:"href"`
		Hrefs                        []string          `json:"hrefs"`
		Links                        json.RawMessage   `json:"links"`
		HALLinks                     json.RawMessage   `json:"_links"`
		File                         string            `json:"file"`
		Files                        []string          `json:"files"`
		ID                           string            `json:"id"`
		IDs                          []string          `json:"ids"`
		Type                         string            `json:"type"`
		ResourceType                 string            `json:"resource_type"`
		ResourceTypeCamel            string            `json:"resourceType"`
		Kind                         string            `json:"kind"`
		MemoryID                     string            `json:"memory_id"`
		MemoryIDCamel                string            `json:"memoryId"`
		MemoryIDs                    []string          `json:"memory_ids"`
		MemoryIDsCamel               []string          `json:"memoryIds"`
		SelectedID                   string            `json:"selected_id"`
		SelectedIDCamel              string            `json:"selectedId"`
		SelectedIDs                  []string          `json:"selected_ids"`
		SelectedIDsCamel             []string          `json:"selectedIds"`
		RelevantIDs                  []string          `json:"relevant_ids"`
		RelevantIDsCamel             []string          `json:"relevantIds"`
		Matches                      []json.RawMessage `json:"matches"`
		Memories                     []json.RawMessage `json:"memories"`
		FilesList                    []json.RawMessage `json:"files_list"`
		FilesListCamel               []json.RawMessage `json:"filesList"`
		Nodes                        []json.RawMessage `json:"nodes"`
		Edges                        []json.RawMessage `json:"edges"`
		Items                        []json.RawMessage `json:"items"`
		Resources                    []json.RawMessage `json:"resources"`
		Included                     json.RawMessage   `json:"included"`
		Collection                   json.RawMessage   `json:"collection"`
		List                         json.RawMessage   `json:"list"`
		Children                     json.RawMessage   `json:"children"`
		Values                       json.RawMessage   `json:"values"`
		Records                      json.RawMessage   `json:"records"`
		Entries                      json.RawMessage   `json:"entries"`
		Selected                     json.RawMessage   `json:"selected"`
		Selection                    json.RawMessage   `json:"selection"`
		Data                         json.RawMessage   `json:"data"`
		Payload                      json.RawMessage   `json:"payload"`
		Body                         json.RawMessage   `json:"body"`
		Resource                     json.RawMessage   `json:"resource"`
		Attributes                   json.RawMessage   `json:"attributes"`
		Properties                   json.RawMessage   `json:"properties"`
		Attrs                        json.RawMessage   `json:"attrs"`
		Viewer                       json.RawMessage   `json:"viewer"`
		Edge                         json.RawMessage   `json:"edge"`
		Node                         json.RawMessage   `json:"node"`
		Result                       json.RawMessage   `json:"result"`
		Response                     json.RawMessage   `json:"response"`
		MemorySelection              json.RawMessage   `json:"memory_selection"`
		MemorySelectionCamel         json.RawMessage   `json:"memorySelection"`
		RelevantMemorySelection      json.RawMessage   `json:"relevant_memory_selection"`
		RelevantMemorySelectionCamel json.RawMessage   `json:"relevantMemorySelection"`
	}
	if err := json.Unmarshal([]byte(raw), &object); err == nil {
		directIDs := []string{
			object.MemoryPath,
			object.MemoryPathCamel,
			object.SelectedMemoryPath,
			object.SelectedMemoryPathCamel,
			object.RelevantMemoryPath,
			object.RelevantMemoryPathCamel,
			object.FilePath,
			object.FilePathCamel,
			object.FileURI,
			object.FileURICamel,
			object.FileURL,
			object.FileURLCamel,
			object.Path,
			object.URI,
			object.URL,
			object.Href,
			object.File,
			object.MemoryID,
			object.MemoryIDCamel,
			object.SelectedID,
			object.SelectedIDCamel,
		}
		directIDGroups := [][]string{
			object.MemoryPaths,
			object.MemoryPathsCamel,
			object.SelectedMemoryPaths,
			object.SelectedMemoryPathsCamel,
			object.RelevantMemoryPaths,
			object.RelevantMemoryPathsCamel,
			object.FilePaths,
			object.FilePathsCamel,
			[]string{object.FileURI, object.FileURICamel, object.FileURL, object.FileURLCamel},
			object.Paths,
			object.URIs,
			object.URLs,
			object.Hrefs,
			object.Files,
			object.MemoryIDs,
			object.MemoryIDsCamel,
			object.SelectedIDs,
			object.SelectedIDsCamel,
			object.RelevantIDs,
			object.RelevantIDsCamel,
		}
		if relevantMemoryResourceTypeAllowsBareID(normalizedSelectionResourceTypeFromStrings(object.Type, object.ResourceType, object.ResourceTypeCamel, object.Kind)) {
			directIDs = append(directIDs, object.ID)
			directIDGroups = append(directIDGroups, object.IDs)
		}
		ids := relevantMemoryIDs(append(directIDs, appendManyStringSlices(directIDGroups...)...))
		if len(ids) == 0 {
			ids = relevantMemoryIDsFromSelectionLinks(object.Links, object.HALLinks)
		}
		if len(ids) == 0 {
			ids = relevantMemoryIDsFromRawItems(
				object.Matches,
				object.Memories,
				object.FilesList,
				object.FilesListCamel,
				object.Nodes,
				object.Edges,
				object.Items,
				object.Resources,
			)
		}
		query := firstNonEmpty(object.Query, object.SearchQuery, object.SearchQueryCamel, object.RewrittenQuery, object.RewrittenQueryCamel, object.ExpandedQuery, object.ExpandedQueryCamel)
		if len(ids) == 0 {
			query, ids = relevantMemorySelectionFromNestedPayloads(query,
				object.Selected,
				object.Selection,
				object.Data,
				object.Payload,
				object.Body,
				object.Resource,
				object.Attributes,
				object.Properties,
				object.Attrs,
				object.Viewer,
				object.Edge,
				object.Node,
				object.Included,
				object.Collection,
				object.List,
				object.Children,
				object.Values,
				object.Records,
				object.Entries,
				object.Result,
				object.Response,
				object.MemorySelection,
				object.MemorySelectionCamel,
				object.RelevantMemorySelection,
				object.RelevantMemorySelectionCamel,
			)
		}
		return query, ids, query != "" || len(ids) > 0
	}
	var ids []string
	if err := json.Unmarshal([]byte(raw), &ids); err == nil {
		parsed := relevantMemoryIDs(ids)
		return "", parsed, len(parsed) > 0
	}
	var items []json.RawMessage
	if err := json.Unmarshal([]byte(raw), &items); err == nil {
		parsed := relevantMemoryIDsFromRawItems(items)
		return "", parsed, len(parsed) > 0
	}
	return "", nil, false
}

func selectionQueryFromRawObject(object map[string]json.RawMessage) string {
	for _, key := range []string{
		"query",
		"search_query",
		"searchQuery",
		"rewritten_query",
		"rewrittenQuery",
		"expanded_query",
		"expandedQuery",
		"user_query",
		"userQuery",
		"question",
		"prompt",
		"input",
		"search",
		"search_text",
		"searchText",
	} {
		if value := stringFromRawJSON(object[key]); value != "" {
			return value
		}
	}
	return ""
}

func relevantMemoryIDsFromRawObject(object map[string]json.RawMessage) []string {
	var raw []string
	allowBareObjectID := relevantMemoryObjectAllowsBareID(object)
	for _, key := range relevantMemoryItemIDKeys {
		if !allowBareObjectID && relevantMemoryItemKeyIsBareID(key) {
			continue
		}
		if value, ok := object[key]; ok {
			raw = appendRelevantMemoryIDsFromRawValue(raw, value)
		}
	}
	for _, key := range selectionLinkContainerKeys {
		if value, ok := object[key]; ok {
			raw = appendSelectionLinkStrings(raw, value)
		}
	}
	return relevantMemoryIDs(raw)
}

func rawObjectValues(object map[string]json.RawMessage, keys ...string) []json.RawMessage {
	var values []json.RawMessage
	for _, key := range keys {
		if value, ok := object[key]; ok && len(value) > 0 {
			values = append(values, value)
		}
	}
	return values
}

func stringFromRawJSON(value json.RawMessage) string {
	if len(value) == 0 || string(value) == "null" {
		return ""
	}
	var text string
	if err := json.Unmarshal(value, &text); err == nil {
		return strings.TrimSpace(text)
	}
	return ""
}

func selectionProviderResponseText(raw string) (string, bool) {
	var object map[string]json.RawMessage
	if err := json.Unmarshal([]byte(raw), &object); err != nil {
		return "", false
	}
	for _, key := range []string{
		"choice",
		"choices",
		"output",
		"outputs",
		"candidate",
		"candidates",
		"generation",
		"generations",
		"completion",
		"completions",
		"response",
		"responses",
		"result",
		"results",
		"message",
		"content",
		"text",
		"outputText",
		"output_text",
	} {
		value, ok := object[key]
		if !ok {
			continue
		}
		if text, ok := selectionProviderTextFromRaw(value, 0); ok {
			if payload, ok := selectionProviderJSONPayload(text); ok {
				return payload, true
			}
			return text, true
		}
	}
	return "", false
}

func selectionProviderJSONPayload(text string) (string, bool) {
	text = strings.TrimSpace(text)
	if startsJSONValue(text) {
		return text, true
	}
	payload := stripMarkdownFence(text)
	if startsJSONValue(payload) {
		return payload, true
	}
	return "", false
}

func selectionProviderTextFromRaw(raw json.RawMessage, depth int) (string, bool) {
	raw = bytes.TrimSpace(raw)
	if len(raw) == 0 || bytes.Equal(raw, []byte("null")) || depth > 8 {
		return "", false
	}
	var text string
	if err := json.Unmarshal(raw, &text); err == nil {
		text = strings.TrimSpace(text)
		return text, text != ""
	}
	if raw[0] == '[' {
		var items []json.RawMessage
		if err := json.Unmarshal(raw, &items); err != nil {
			return "", false
		}
		parts := make([]string, 0, len(items))
		for _, item := range items {
			part, ok := selectionProviderTextFromRaw(item, depth+1)
			if ok {
				parts = append(parts, part)
			}
		}
		if len(parts) == 0 {
			return "", false
		}
		return strings.Join(parts, "\n"), true
	}
	if raw[0] != '{' {
		return "", false
	}
	var block contracts.ContentBlock
	if err := json.Unmarshal(raw, &block); err == nil && block.Type == contracts.ContentText && strings.TrimSpace(block.Text) != "" {
		return strings.TrimSpace(block.Text), true
	}
	fields := map[string]json.RawMessage{}
	if err := json.Unmarshal(raw, &fields); err != nil {
		return "", false
	}
	for _, key := range []string{"text", "content", "value", "output", "outputText", "output_text"} {
		if value, ok := fields[key]; ok {
			if text, ok := selectionProviderTextFromRaw(value, depth+1); ok {
				return text, true
			}
		}
	}
	for _, key := range []string{"message", "delta", "part", "parts", "candidate", "choice", "generation", "result", "response"} {
		if value, ok := fields[key]; ok {
			if text, ok := selectionProviderTextFromRaw(value, depth+1); ok {
				return text, true
			}
		}
	}
	return "", false
}

func recallSelectionFromNestedPayloads(fallbackQuery string, payloads ...json.RawMessage) (string, []contracts.ID) {
	query := fallbackQuery
	for _, payload := range payloads {
		if len(payload) == 0 {
			continue
		}
		nestedQuery, nestedIDs, ok := parseRecallAgentJSON(string(payload))
		if !ok {
			continue
		}
		if nestedQuery != "" {
			query = nestedQuery
		}
		if len(nestedIDs) > 0 {
			return query, nestedIDs
		}
	}
	return query, nil
}

func relevantMemorySelectionFromNestedPayloads(fallbackQuery string, payloads ...json.RawMessage) (string, []string) {
	query := fallbackQuery
	for _, payload := range payloads {
		if len(payload) == 0 {
			continue
		}
		nestedQuery, nestedIDs, ok := parseRelevantMemoryAgentJSON(string(payload))
		if !ok {
			continue
		}
		if nestedQuery != "" {
			query = nestedQuery
		}
		if len(nestedIDs) > 0 {
			return query, nestedIDs
		}
	}
	return query, nil
}

func recallIDsFromRawItems(groups ...[]json.RawMessage) []contracts.ID {
	var raw []string
	for _, group := range groups {
		for _, item := range group {
			raw = appendRecallIDsFromRawValue(raw, item)
		}
	}
	return recallIDs(raw)
}

func relevantMemoryIDsFromRawItems(groups ...[]json.RawMessage) []string {
	var raw []string
	for _, group := range groups {
		for _, item := range group {
			raw = appendRelevantMemoryIDsFromRawValue(raw, item)
		}
	}
	return relevantMemoryIDs(raw)
}

func appendRecallIDsFromRawValue(raw []string, value json.RawMessage) []string {
	if len(value) == 0 || string(value) == "null" {
		return raw
	}
	var id string
	if err := json.Unmarshal(value, &id); err == nil {
		return append(raw, id)
	}
	var ids []string
	if err := json.Unmarshal(value, &ids); err == nil {
		return append(raw, ids...)
	}
	var items []json.RawMessage
	if err := json.Unmarshal(value, &items); err == nil {
		for _, item := range items {
			raw = appendRecallIDsFromRawValue(raw, item)
		}
		return raw
	}
	var object map[string]json.RawMessage
	if err := json.Unmarshal(value, &object); err != nil {
		return raw
	}
	allowBareObjectID := recallObjectAllowsBareID(object)
	for _, key := range recallItemIDKeys {
		if !allowBareObjectID && recallItemKeyIsBareID(key) {
			continue
		}
		if nested, ok := object[key]; ok {
			raw = appendRecallIDsFromRawValue(raw, nested)
		}
	}
	for _, key := range selectionLinkContainerKeys {
		if nested, ok := object[key]; ok {
			raw = appendSelectionLinkStrings(raw, nested)
		}
	}
	for _, key := range recallNestedItemKeys {
		if nested, ok := object[key]; ok {
			raw = appendRecallIDsFromRawValue(raw, nested)
		}
	}
	return raw
}

func appendRelevantMemoryIDsFromRawValue(raw []string, value json.RawMessage) []string {
	if len(value) == 0 || string(value) == "null" {
		return raw
	}
	var id string
	if err := json.Unmarshal(value, &id); err == nil {
		return append(raw, id)
	}
	var ids []string
	if err := json.Unmarshal(value, &ids); err == nil {
		return append(raw, ids...)
	}
	var items []json.RawMessage
	if err := json.Unmarshal(value, &items); err == nil {
		for _, item := range items {
			raw = appendRelevantMemoryIDsFromRawValue(raw, item)
		}
		return raw
	}
	var object map[string]json.RawMessage
	if err := json.Unmarshal(value, &object); err != nil {
		return raw
	}
	allowBareObjectID := relevantMemoryObjectAllowsBareID(object)
	for _, key := range relevantMemoryItemIDKeys {
		if !allowBareObjectID && relevantMemoryItemKeyIsBareID(key) {
			continue
		}
		if nested, ok := object[key]; ok {
			raw = appendRelevantMemoryIDsFromRawValue(raw, nested)
		}
	}
	for _, key := range selectionLinkContainerKeys {
		if nested, ok := object[key]; ok {
			raw = appendSelectionLinkStrings(raw, nested)
		}
	}
	for _, key := range relevantMemoryNestedItemKeys {
		if nested, ok := object[key]; ok {
			raw = appendRelevantMemoryIDsFromRawValue(raw, nested)
		}
	}
	return raw
}

var selectionLinkContainerKeys = []string{"links", "_links"}

var selectionLinkIDKeys = []string{
	"href",
	"url",
	"uri",
	"link",
	"path",
	"file",
	"file_path",
	"filePath",
	"file_uri",
	"fileUri",
	"file_url",
	"fileUrl",
	"session_uri",
	"sessionUri",
	"session_url",
	"sessionUrl",
	"summary_uri",
	"summaryUri",
	"summary_url",
	"summaryUrl",
}

var selectionLinkMetadataKeys = map[string]struct{}{
	"rel":      {},
	"relation": {},
	"name":     {},
	"type":     {},
	"kind":     {},
	"label":    {},
	"title":    {},
	"method":   {},
}

func recallIDsFromSelectionLinks(payloads ...json.RawMessage) []contracts.ID {
	var raw []string
	for _, payload := range payloads {
		raw = appendSelectionLinkStrings(raw, payload)
	}
	return recallIDs(raw)
}

func relevantMemoryIDsFromSelectionLinks(payloads ...json.RawMessage) []string {
	var raw []string
	for _, payload := range payloads {
		raw = appendSelectionLinkStrings(raw, payload)
	}
	return relevantMemoryIDs(raw)
}

func appendSelectionLinkStrings(raw []string, value json.RawMessage) []string {
	value = bytes.TrimSpace(value)
	if len(value) == 0 || bytes.Equal(value, []byte("null")) {
		return raw
	}
	var id string
	if err := json.Unmarshal(value, &id); err == nil {
		return append(raw, id)
	}
	var ids []string
	if err := json.Unmarshal(value, &ids); err == nil {
		return append(raw, ids...)
	}
	var items []json.RawMessage
	if err := json.Unmarshal(value, &items); err == nil {
		for _, item := range items {
			raw = appendSelectionLinkStrings(raw, item)
		}
		return raw
	}
	var object map[string]json.RawMessage
	if err := json.Unmarshal(value, &object); err != nil {
		return raw
	}
	foundDirectLink := false
	for _, key := range selectionLinkIDKeys {
		if nested, ok := object[key]; ok {
			raw = appendSelectionLinkStrings(raw, nested)
			foundDirectLink = true
		}
	}
	if foundDirectLink {
		return raw
	}
	for key, nested := range object {
		if _, skip := selectionLinkMetadataKeys[key]; skip {
			continue
		}
		raw = appendSelectionLinkStrings(raw, nested)
	}
	return raw
}

var recallItemIDKeys = []string{
	"session_id",
	"sessionId",
	"sessionID",
	"session_uuid",
	"sessionUuid",
	"sessionUUID",
	"conversation_id",
	"conversationId",
	"conversationID",
	"conversation_uuid",
	"conversationUuid",
	"conversationUUID",
	"thread_id",
	"threadId",
	"threadID",
	"transcript_id",
	"transcriptId",
	"transcriptID",
	"selected_id",
	"selectedId",
	"selectedID",
	"selected_session_id",
	"selectedSessionId",
	"selectedSessionID",
	"selected_session",
	"selectedSession",
	"selected_conversation_id",
	"selectedConversationId",
	"selectedConversationID",
	"selected_conversation",
	"selectedConversation",
	"selected_conversations",
	"selectedConversations",
	"selected_thread_id",
	"selectedThreadId",
	"selectedThreadID",
	"selected_thread",
	"selectedThread",
	"selected_threads",
	"selectedThreads",
	"selected_transcript_id",
	"selectedTranscriptId",
	"selectedTranscriptID",
	"selected_transcript",
	"selectedTranscript",
	"selected_transcripts",
	"selectedTranscripts",
	"relevant_id",
	"relevantId",
	"relevantID",
	"relevant_session_id",
	"relevantSessionId",
	"relevantSessionID",
	"relevant_session",
	"relevantSession",
	"relevant_conversation_id",
	"relevantConversationId",
	"relevantConversationID",
	"relevant_conversation",
	"relevantConversation",
	"relevant_conversations",
	"relevantConversations",
	"relevant_thread_id",
	"relevantThreadId",
	"relevantThreadID",
	"relevant_thread",
	"relevantThread",
	"relevant_threads",
	"relevantThreads",
	"relevant_transcript_id",
	"relevantTranscriptId",
	"relevantTranscriptID",
	"relevant_transcript",
	"relevantTranscript",
	"relevant_transcripts",
	"relevantTranscripts",
	"memory_id",
	"memoryId",
	"memoryID",
	"memory_uuid",
	"memoryUuid",
	"memoryUUID",
	"selected_memory_id",
	"selectedMemoryId",
	"selectedMemoryID",
	"selected_memory",
	"selectedMemory",
	"relevant_memory_id",
	"relevantMemoryId",
	"relevantMemoryID",
	"relevant_memory",
	"relevantMemory",
	"candidate_id",
	"candidateId",
	"candidateID",
	"candidate_session_id",
	"candidateSessionId",
	"candidateSessionID",
	"candidate_session",
	"candidateSession",
	"candidate_conversation_id",
	"candidateConversationId",
	"candidateConversationID",
	"candidate_conversation",
	"candidateConversation",
	"candidate_conversations",
	"candidateConversations",
	"candidate_thread_id",
	"candidateThreadId",
	"candidateThreadID",
	"candidate_thread",
	"candidateThread",
	"candidate_threads",
	"candidateThreads",
	"candidate_transcript_id",
	"candidateTranscriptId",
	"candidateTranscriptID",
	"candidate_transcript",
	"candidateTranscript",
	"candidate_transcripts",
	"candidateTranscripts",
	"candidate_memory_id",
	"candidateMemoryId",
	"candidateMemoryID",
	"candidate_memory",
	"candidateMemory",
	"session_uri",
	"sessionUri",
	"sessionURI",
	"session_url",
	"sessionUrl",
	"sessionURL",
	"session_path",
	"sessionPath",
	"session_summary_path",
	"sessionSummaryPath",
	"summary_path",
	"summaryPath",
	"uri",
	"uris",
	"url",
	"urls",
	"href",
	"hrefs",
	"summary_id",
	"summaryId",
	"summaryID",
	"id",
	"uuid",
}

var relevantMemoryItemIDKeys = []string{
	"memory_path",
	"memoryPath",
	"memory_paths",
	"memoryPaths",
	"selected_memory_path",
	"selectedMemoryPath",
	"selected_memory_paths",
	"selectedMemoryPaths",
	"relevant_memory_path",
	"relevantMemoryPath",
	"relevant_memory_paths",
	"relevantMemoryPaths",
	"file_path",
	"filePath",
	"file_paths",
	"filePaths",
	"selected_file_path",
	"selectedFilePath",
	"selected_file_paths",
	"selectedFilePaths",
	"relevant_file_path",
	"relevantFilePath",
	"relevant_file_paths",
	"relevantFilePaths",
	"candidate_file_path",
	"candidateFilePath",
	"candidate_file_paths",
	"candidateFilePaths",
	"file_uri",
	"fileUri",
	"fileURI",
	"file_url",
	"fileUrl",
	"fileURL",
	"path",
	"paths",
	"uri",
	"uris",
	"url",
	"urls",
	"href",
	"hrefs",
	"file",
	"files",
	"selected_file",
	"selectedFile",
	"selected_files",
	"selectedFiles",
	"relevant_file",
	"relevantFile",
	"relevant_files",
	"relevantFiles",
	"candidate_file",
	"candidateFile",
	"candidate_files",
	"candidateFiles",
	"id",
	"ids",
	"uuid",
	"memory_id",
	"memoryId",
	"memoryID",
	"selected_id",
	"selectedId",
	"selectedID",
	"relevant_id",
	"relevantId",
	"relevantID",
}

var recallNestedItemKeys = []string{
	"session",
	"conversation",
	"conversations",
	"thread",
	"threads",
	"transcript",
	"transcripts",
	"memory",
	"summary",
	"summaries",
	"candidate",
	"selected_session",
	"selectedSession",
	"selected_conversation",
	"selectedConversation",
	"selected_conversations",
	"selectedConversations",
	"selected_thread",
	"selectedThread",
	"selected_threads",
	"selectedThreads",
	"selected_transcript",
	"selectedTranscript",
	"selected_transcripts",
	"selectedTranscripts",
	"selected_memory",
	"selectedMemory",
	"selected_summary",
	"selectedSummary",
	"selected_summaries",
	"selectedSummaries",
	"relevant_session",
	"relevantSession",
	"relevant_conversation",
	"relevantConversation",
	"relevant_conversations",
	"relevantConversations",
	"relevant_thread",
	"relevantThread",
	"relevant_threads",
	"relevantThreads",
	"relevant_transcript",
	"relevantTranscript",
	"relevant_transcripts",
	"relevantTranscripts",
	"relevant_memory",
	"relevantMemory",
	"relevant_summary",
	"relevantSummary",
	"relevant_summaries",
	"relevantSummaries",
	"candidate_session",
	"candidateSession",
	"candidate_conversation",
	"candidateConversation",
	"candidate_conversations",
	"candidateConversations",
	"candidate_thread",
	"candidateThread",
	"candidate_threads",
	"candidateThreads",
	"candidate_transcript",
	"candidateTranscript",
	"candidate_transcripts",
	"candidateTranscripts",
	"candidate_memory",
	"candidateMemory",
	"candidate_summary",
	"candidateSummary",
	"candidate_summaries",
	"candidateSummaries",
	"data",
	"payload",
	"body",
	"result",
	"response",
	"resource",
	"attributes",
	"properties",
	"attrs",
	"viewer",
	"edge",
	"node",
	"nodes",
	"edges",
	"items",
	"resources",
	"included",
	"collection",
	"collections",
	"list",
	"lists",
	"children",
	"values",
	"records",
	"entries",
	"objects",
	"record",
	"entry",
	"item",
	"value",
}

var relevantMemoryNestedItemKeys = []string{
	"memory",
	"file",
	"candidate",
	"selected",
	"selection",
	"selected_memory",
	"selectedMemory",
	"relevant_memory",
	"relevantMemory",
	"candidate_memory",
	"candidateMemory",
	"data",
	"payload",
	"body",
	"result",
	"response",
	"resource",
	"attributes",
	"properties",
	"attrs",
	"viewer",
	"edge",
	"node",
	"nodes",
	"edges",
	"items",
	"resources",
	"included",
	"collection",
	"collections",
	"list",
	"lists",
	"children",
	"values",
	"records",
	"entries",
	"objects",
	"record",
	"entry",
	"item",
	"value",
}

func recallItemKeyIsBareID(key string) bool {
	switch key {
	case "id", "uuid":
		return true
	default:
		return false
	}
}

func relevantMemoryItemKeyIsBareID(key string) bool {
	switch key {
	case "id", "ids", "uuid":
		return true
	default:
		return false
	}
}

func recallObjectAllowsBareID(object map[string]json.RawMessage) bool {
	return recallResourceTypeAllowsBareID(normalizedSelectionResourceType(object))
}

func relevantMemoryObjectAllowsBareID(object map[string]json.RawMessage) bool {
	return relevantMemoryResourceTypeAllowsBareID(normalizedSelectionResourceType(object))
}

func recallResourceTypeAllowsBareID(resourceType string) bool {
	if resourceType == "" {
		return true
	}
	return strings.Contains(resourceType, "session") ||
		strings.Contains(resourceType, "memory") ||
		strings.Contains(resourceType, "summary") ||
		strings.Contains(resourceType, "recall") ||
		strings.Contains(resourceType, "selection") ||
		strings.Contains(resourceType, "candidate")
}

func relevantMemoryResourceTypeAllowsBareID(resourceType string) bool {
	if resourceType == "" {
		return true
	}
	return strings.Contains(resourceType, "memory") ||
		strings.Contains(resourceType, "file") ||
		strings.Contains(resourceType, "path") ||
		strings.Contains(resourceType, "selection") ||
		strings.Contains(resourceType, "candidate")
}

func normalizedSelectionResourceType(object map[string]json.RawMessage) string {
	for _, key := range []string{"type", "resource_type", "resourceType", "kind"} {
		value := stringFromRawJSON(object[key])
		if normalized := normalizeSelectionResourceType(value); normalized != "" {
			return normalized
		}
	}
	return ""
}

func normalizeSelectionResourceType(value string) string {
	value = strings.TrimSpace(value)
	if value == "" {
		return ""
	}
	value = strings.ToLower(value)
	value = strings.ReplaceAll(value, "-", "")
	value = strings.ReplaceAll(value, "_", "")
	value = strings.ReplaceAll(value, " ", "")
	return value
}

func normalizedSelectionResourceTypeFromStrings(values ...string) string {
	for _, value := range values {
		normalized := normalizeSelectionResourceType(value)
		if normalized != "" {
			return normalized
		}
	}
	return ""
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

func relevantMemoryIDs(raw []string) []string {
	seen := map[string]struct{}{}
	var ids []string
	for _, value := range raw {
		id := strings.TrimSpace(value)
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

func appendManyStringSlices(groups ...[]string) []string {
	var out []string
	for _, group := range groups {
		out = append(out, group...)
	}
	return out
}

func relevantMemoryLooksLikeID(value string) bool {
	return len(strings.Fields(value)) == 1 ||
		strings.Contains(value, "/") ||
		strings.Contains(value, "\\") ||
		strings.HasSuffix(strings.ToLower(value), ".md")
}

func recallMatchesBySessionIDs(root string, ids []contracts.ID, query string, options RecallOptions) ([]RecallMatch, error) {
	summaries, err := LoadSessionSummaries(root)
	if err != nil {
		return nil, err
	}
	byID := map[contracts.ID]SessionSummary{}
	lookup := map[string]SessionSummary{}
	for _, summary := range summaries {
		if options.ExcludeSessionID != "" && summary.SessionID == options.ExcludeSessionID {
			continue
		}
		byID[summary.SessionID] = summary
		for _, key := range recallSummaryLookupKeys(summary) {
			if _, ok := lookup[key]; !ok {
				lookup[key] = summary
			}
		}
	}
	terms := queryTerms(query)
	var matches []RecallMatch
	for _, id := range ids {
		summary, ok := byID[id]
		if !ok {
			summary, ok = recallLookupSummary(lookup, string(id))
		}
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

func recallSummaryLookupKeys(summary SessionSummary) []string {
	path := strings.TrimSpace(summary.Path)
	sessionID := strings.TrimSpace(string(summary.SessionID))
	keys := []string{sessionID}
	if path != "" {
		keys = append(keys,
			path,
			filepath.ToSlash(path),
			relevantMemoryFileURI(path),
			filepath.Base(filepath.Dir(path)),
		)
	}
	return uniqueNonEmptyStrings(keys)
}

func recallLookupSummary(lookup map[string]SessionSummary, id string) (SessionSummary, bool) {
	for _, key := range recallLookupKeysForID(id) {
		if summary, ok := lookup[key]; ok {
			return summary, true
		}
	}
	return SessionSummary{}, false
}

func recallLookupKeysForID(id string) []string {
	id = strings.TrimSpace(id)
	if id == "" {
		return nil
	}
	keys := []string{id}
	parsed, err := url.Parse(id)
	if err == nil && parsed.Scheme != "" && parsed.Path != "" {
		path := strings.TrimSpace(parsed.Path)
		fromSlash := filepath.FromSlash(path)
		keys = append(keys, path, fromSlash, filepath.ToSlash(fromSlash))
		keys = append(keys, filepath.Base(filepath.Dir(fromSlash)), filepath.Base(fromSlash), strings.TrimSuffix(filepath.Base(fromSlash), filepath.Ext(fromSlash)))
	}
	return uniqueNonEmptyStrings(keys)
}

func relevantMemorySelectionsByIDs(candidates []relevantMemoryCandidate, ids []string, surfaced map[string]struct{}, limit int) []RelevantMemorySelection {
	if limit <= 0 {
		limit = MaxRelevantMemoryAttachments
	}
	lookup := map[string]RelevantMemorySelection{}
	for _, candidate := range candidates {
		header := candidate.Header
		if header.Path == "" {
			continue
		}
		if surfaced != nil {
			if _, ok := surfaced[header.Path]; ok {
				continue
			}
		}
		selection := RelevantMemorySelection{Path: header.Path, MtimeMs: header.Mtime.UnixMilli()}
		base := filepath.Base(header.Path)
		keys := []string{
			header.Filename,
			header.Path,
			filepath.ToSlash(header.Path),
			relevantMemoryFileURI(header.Path),
			base,
			strings.TrimSuffix(base, filepath.Ext(base)),
			strings.TrimSuffix(header.Filename, filepath.Ext(header.Filename)),
		}
		for _, key := range keys {
			key = strings.TrimSpace(key)
			if key == "" {
				continue
			}
			if _, ok := lookup[key]; !ok {
				lookup[key] = selection
			}
		}
	}
	selected := make([]RelevantMemorySelection, 0, limit)
	seen := map[string]struct{}{}
	for _, id := range ids {
		selection, ok := relevantMemoryLookupSelection(lookup, id)
		if !ok {
			continue
		}
		if _, ok := seen[selection.Path]; ok {
			continue
		}
		seen[selection.Path] = struct{}{}
		selected = append(selected, selection)
		if len(selected) >= limit {
			break
		}
	}
	return selected
}

func relevantMemoryLookupSelection(lookup map[string]RelevantMemorySelection, id string) (RelevantMemorySelection, bool) {
	for _, key := range relevantMemoryLookupKeysForID(id) {
		if selection, ok := lookup[key]; ok {
			return selection, true
		}
	}
	return RelevantMemorySelection{}, false
}

func relevantMemoryLookupKeysForID(id string) []string {
	id = strings.TrimSpace(id)
	if id == "" {
		return nil
	}
	keys := []string{id}
	parsed, err := url.Parse(id)
	if err == nil && parsed.Scheme != "" {
		if parsed.Scheme == "file" && parsed.Path != "" {
			path := filepath.FromSlash(parsed.Path)
			keys = append(keys, path, filepath.ToSlash(path))
		}
		if parsed.Path != "" {
			path := strings.TrimSpace(parsed.Path)
			keys = append(keys, path, filepath.FromSlash(path), filepath.ToSlash(path))
			base := filepath.Base(filepath.FromSlash(path))
			keys = append(keys, base, strings.TrimSuffix(base, filepath.Ext(base)))
		}
	}
	base := filepath.Base(filepath.FromSlash(id))
	if base != "." && base != string(filepath.Separator) && base != "" {
		keys = append(keys, base, strings.TrimSuffix(base, filepath.Ext(base)))
	}
	return uniqueNonEmptyStrings(keys)
}

func relevantMemoryFileURI(path string) string {
	path = strings.TrimSpace(path)
	if path == "" {
		return ""
	}
	if !filepath.IsAbs(path) {
		if abs, err := filepath.Abs(path); err == nil {
			path = abs
		}
	}
	return (&url.URL{Scheme: "file", Path: filepath.ToSlash(path)}).String()
}

func uniqueNonEmptyStrings(values []string) []string {
	seen := map[string]struct{}{}
	out := make([]string, 0, len(values))
	for _, value := range values {
		value = strings.TrimSpace(value)
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
	return parseFactsText(raw, 0)
}

func parseFactsText(raw string, depth int) ([]MemoryFact, error) {
	if depth > 8 {
		return nil, fmt.Errorf("memory facts response nested too deeply")
	}
	raw = strings.TrimSpace(raw)
	payload := stripMarkdownFence(raw)
	facts, err := parseFactsJSON(payload)
	if err == nil && len(facts) > 0 {
		return facts, nil
	}
	if text, ok := scalarJSONString(payload); ok && startsJSONValue(text) {
		return parseFactsText(text, depth+1)
	}
	if text, ok := selectionProviderResponseText(payload); ok {
		return parseFactsText(text, depth+1)
	}
	if err == nil {
		return facts, nil
	}
	if startsJSONValue(payload) {
		return nil, err
	}
	if payload, ok := firstJSONValue(raw); ok {
		return parseFactsText(payload, depth+1)
	}
	return nil, err
}

func scalarJSONString(raw string) (string, bool) {
	var scalar string
	if err := json.Unmarshal([]byte(raw), &scalar); err != nil {
		return "", false
	}
	scalar = strings.TrimSpace(scalar)
	return scalar, scalar != ""
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
		kind, ok := normalizeFactKind(firstNonEmpty(entry.Kind, entry.Type, entry.FactType, entry.Category, entry.Label))
		if !ok {
			continue
		}
		text := strings.TrimSpace(firstNonEmpty(entry.Text, entry.Content, entry.Fact, entry.Statement, entry.Summary, entry.Value, entry.Detail, entry.Note, entry.Description, entry.Body, entry.MessageText, entry.Observation, entry.Finding, entry.Insight, entry.Result, entry.Output))
		if text == "" {
			continue
		}
		sourceUUID := firstNonEmpty(entry.SourceUUID, entry.SourceUUIDCamel, entry.SourceID, entry.SourceIDCamel, entry.Source, entry.MessageUUID, entry.MessageUUIDCamel, entry.MessageID, entry.MessageIDCamel, entry.SourceMessageID, entry.SourceMessageIDCamel, entry.UUID)
		facts = append(facts, MemoryFact{Kind: kind, Text: text, SourceUUID: contracts.ID(sourceUUID)})
	}
	return facts, nil
}

func normalizeFactKind(raw string) (FactKind, bool) {
	name := strings.ToLower(strings.TrimSpace(raw))
	name = strings.NewReplacer("-", "_", " ", "_").Replace(name)
	switch name {
	case "preference", "pref", "user_pref", "user_preference", "memory_preference", "personal_preference", "instruction", "user_instruction", "standing_instruction", "constraint", "user_constraint", "rule", "user_rule", "guideline", "policy":
		return FactPreference, true
	case "request", "user_request", "ask", "todo", "task", "requirement", "user_requirement", "action_item", "follow_up", "followup":
		return FactRequest, true
	case "decision", "decided", "choice", "resolution", "outcome", "conclusion", "agreement":
		return FactDecision, true
	case "tool", "tool_use", "tool_usage", "tool_result", "tool_call", "command", "command_run", "operation":
		return FactTool, true
	default:
		return "", false
	}
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
			"extractions",
			"extracted_memory",
			"extractedMemory",
			"session_memory",
			"sessionMemory",
			"memory_items",
			"memoryItems",
			"items",
			"entries",
			"records",
			"resources",
			"results",
			"observations",
			"notes",
			"findings",
			"data",
			"payload",
			"body",
			"response",
			"result",
			"output",
			"outputs",
			"resource",
			"attributes",
			"properties",
			"attrs",
			"node",
			"edge",
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
	fact := rawMemoryFact{
		Kind:                 stringMapField(value, "kind"),
		Type:                 stringMapField(value, "type"),
		FactType:             stringMapField(value, "fact_type", "factType"),
		Category:             stringMapField(value, "category"),
		Label:                stringMapField(value, "label"),
		Text:                 stringMapField(value, "text"),
		Content:              stringMapField(value, "content"),
		Fact:                 stringMapField(value, "fact"),
		Statement:            stringMapField(value, "statement"),
		Summary:              stringMapField(value, "summary"),
		Value:                stringMapField(value, "value"),
		Detail:               stringMapField(value, "detail"),
		Note:                 stringMapField(value, "note", "notes"),
		Description:          stringMapField(value, "description", "details"),
		Body:                 stringMapField(value, "body"),
		MessageText:          stringMapField(value, "message"),
		Observation:          stringMapField(value, "observation"),
		Finding:              stringMapField(value, "finding"),
		Insight:              stringMapField(value, "insight"),
		Result:               stringMapField(value, "result"),
		Output:               stringMapField(value, "output"),
		SourceUUID:           idMapField(value, "source_uuid", "sourceUUID"),
		SourceUUIDCamel:      idMapField(value, "sourceUuid"),
		SourceID:             idMapField(value, "source_id", "sourceID", "source_event_id", "source_event_uuid", "origin_id", "origin_uuid"),
		SourceIDCamel:        idMapField(value, "sourceId", "sourceEventId", "sourceEventID", "sourceEventUuid", "sourceEventUUID", "originId", "originUuid", "originUUID"),
		Source:               sourceIDFromFactMap(value),
		MessageUUID:          idMapField(value, "message_uuid", "messageUUID"),
		MessageUUIDCamel:     idMapField(value, "messageUuid"),
		MessageID:            idMapField(value, "message_id", "messageID"),
		MessageIDCamel:       idMapField(value, "messageId"),
		SourceMessageID:      idMapField(value, "source_message_id", "source_message_uuid", "sourceMessageID", "sourceMessageUUID"),
		SourceMessageIDCamel: idMapField(value, "sourceMessageId", "sourceMessageUuid"),
		UUID:                 idMapField(value, "uuid"),
	}
	if fact.MessageUUID == "" {
		if _, ok := value["message"].(map[string]any); ok {
			fact.MessageUUID = nestedIDFromValue(value["message"])
		}
	}
	if fact.SourceMessageID == "" {
		fact.SourceMessageID = nestedIDFromValue(value["source_message"])
	}
	if fact.SourceMessageID == "" {
		fact.SourceMessageID = nestedIDFromValue(value["sourceMessage"])
	}
	if firstNonEmpty(fact.Kind, fact.Type, fact.FactType, fact.Category, fact.Label) == "" {
		return rawMemoryFact{}, false
	}
	if firstNonEmpty(fact.Text, fact.Content, fact.Fact, fact.Statement, fact.Summary, fact.Value, fact.Detail, fact.Note, fact.Description, fact.Body, fact.MessageText, fact.Observation, fact.Finding, fact.Insight, fact.Result, fact.Output) == "" {
		return rawMemoryFact{}, false
	}
	return fact, true
}

func idMapField(value map[string]any, keys ...string) string {
	return idMapFieldDepth(value, 0, keys...)
}

func idMapFieldDepth(value map[string]any, depth int, keys ...string) string {
	for _, key := range keys {
		if id := nestedIDFromValueDepth(value[key], depth+1); id != "" {
			return id
		}
	}
	return ""
}

func stringMapField(value map[string]any, keys ...string) string {
	for _, key := range keys {
		if text := textFromValue(value[key]); text != "" {
			return text
		}
	}
	return ""
}

func sourceIDFromFactMap(value map[string]any) string {
	for _, key := range []string{
		"source",
		"origin",
		"source_message",
		"sourceMessage",
		"source_event",
		"sourceEvent",
		"source_turn",
		"sourceTurn",
		"event",
		"turn",
	} {
		if id := nestedIDFromValue(value[key]); id != "" {
			return id
		}
	}
	return ""
}

func nestedIDFromValue(value any) string {
	return nestedIDFromValueDepth(value, 0)
}

func nestedIDFromValueDepth(value any, depth int) string {
	if depth > 6 {
		return ""
	}
	if id := directIDValue(value); id != "" {
		return id
	}
	object, ok := value.(map[string]any)
	if !ok {
		return ""
	}
	if id := idMapFieldDepth(object, depth+1,
		"source_uuid", "sourceUuid", "sourceUUID",
		"source_id", "sourceId", "sourceID",
		"source_message_uuid", "sourceMessageUuid", "sourceMessageUUID",
		"source_message_id", "sourceMessageId", "sourceMessageID",
		"message_uuid", "messageUuid", "messageUUID",
		"message_id", "messageId", "messageID",
		"event_uuid", "eventUuid", "eventUUID",
		"event_id", "eventId", "eventID",
		"turn_uuid", "turnUuid", "turnUUID",
		"turn_id", "turnId", "turnID",
		"origin_uuid", "originUuid", "originUUID",
		"origin_id", "originId", "originID",
		"uuid", "id",
	); id != "" {
		return id
	}
	for _, key := range []string{
		"source",
		"origin",
		"source_message",
		"sourceMessage",
		"source_event",
		"sourceEvent",
		"source_turn",
		"sourceTurn",
		"event",
		"turn",
	} {
		if id := nestedIDFromValueDepth(object[key], depth+1); id != "" {
			return id
		}
	}
	return ""
}

func textFromValue(value any) string {
	if text := directStringValue(value); text != "" {
		return text
	}
	switch typed := value.(type) {
	case []any:
		var parts []string
		for _, item := range typed {
			if text := textFromValue(item); text != "" {
				parts = append(parts, text)
			}
		}
		return strings.Join(parts, "\n")
	case map[string]any:
		return stringMapField(typed, "text", "content", "fact", "statement", "summary", "value", "detail", "note", "description", "body", "message", "observation", "finding", "insight", "result", "output")
	default:
		return ""
	}
}

func directStringValue(value any) string {
	text, ok := value.(string)
	if !ok {
		return ""
	}
	return strings.TrimSpace(text)
}

func directIDValue(value any) string {
	switch typed := value.(type) {
	case string:
		return strings.TrimSpace(typed)
	case json.Number:
		return strings.TrimSpace(typed.String())
	case float64:
		return strings.TrimSpace(strconv.FormatFloat(typed, 'f', -1, 64))
	case float32:
		return strings.TrimSpace(strconv.FormatFloat(float64(typed), 'f', -1, 32))
	case int:
		return strconv.Itoa(typed)
	case int8:
		return strconv.FormatInt(int64(typed), 10)
	case int16:
		return strconv.FormatInt(int64(typed), 10)
	case int32:
		return strconv.FormatInt(int64(typed), 10)
	case int64:
		return strconv.FormatInt(typed, 10)
	case uint:
		return strconv.FormatUint(uint64(typed), 10)
	case uint8:
		return strconv.FormatUint(uint64(typed), 10)
	case uint16:
		return strconv.FormatUint(uint64(typed), 10)
	case uint32:
		return strconv.FormatUint(uint64(typed), 10)
	case uint64:
		return strconv.FormatUint(typed, 10)
	default:
		return ""
	}
}

type rawMemoryFact struct {
	Kind                 string `json:"kind"`
	Type                 string `json:"type"`
	FactType             string `json:"fact_type"`
	Category             string `json:"category"`
	Label                string `json:"label"`
	Text                 string `json:"text"`
	Content              string `json:"content"`
	Fact                 string `json:"fact"`
	Statement            string `json:"statement"`
	Summary              string `json:"summary"`
	Value                string `json:"value"`
	Detail               string `json:"detail"`
	Note                 string `json:"note"`
	Description          string `json:"description"`
	Body                 string `json:"body"`
	MessageText          string `json:"message"`
	Observation          string `json:"observation"`
	Finding              string `json:"finding"`
	Insight              string `json:"insight"`
	Result               string `json:"result"`
	Output               string `json:"output"`
	SourceUUID           string `json:"source_uuid"`
	SourceUUIDCamel      string `json:"sourceUuid"`
	SourceID             string `json:"source_id"`
	SourceIDCamel        string `json:"sourceId"`
	Source               string `json:"source"`
	MessageUUID          string `json:"message_uuid"`
	MessageUUIDCamel     string `json:"messageUuid"`
	MessageID            string `json:"message_id"`
	MessageIDCamel       string `json:"messageId"`
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
