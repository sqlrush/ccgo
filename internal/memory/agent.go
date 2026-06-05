package memory

import (
	"context"
	"encoding/json"
	"fmt"
	"path/filepath"
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
		if len(strings.Fields(scalar)) == 1 {
			return "", recallIDs([]string{scalar}), true
		}
		return scalar, nil, true
	}
	var object struct {
		Query                   string            `json:"query"`
		SearchQuery             string            `json:"search_query"`
		SearchQueryCamel        string            `json:"searchQuery"`
		RewrittenQuery          string            `json:"rewritten_query"`
		RewrittenQueryCamel     string            `json:"rewrittenQuery"`
		ExpandedQuery           string            `json:"expanded_query"`
		ExpandedQueryCamel      string            `json:"expandedQuery"`
		SessionID               string            `json:"session_id"`
		SessionIDs              []string          `json:"session_ids"`
		SessionIDsCamel         []string          `json:"sessionIds"`
		SelectedSessionID       string            `json:"selected_session_id"`
		SelectedSessionIDs      []string          `json:"selected_session_ids"`
		SelectedSessionIDsCamel []string          `json:"selectedSessionIds"`
		SelectedIDs             []string          `json:"selected_ids"`
		SelectedIDsCamel        []string          `json:"selectedIds"`
		RelevantIDs             []string          `json:"relevant_ids"`
		RelevantIDsCamel        []string          `json:"relevantIds"`
		RelevantSessionIDs      []string          `json:"relevant_session_ids"`
		RelevantSessionIDsCamel []string          `json:"relevantSessionIds"`
		MemoryID                string            `json:"memory_id"`
		MemoryIDCamel           string            `json:"memoryId"`
		MemoryIDs               []string          `json:"memory_ids"`
		MemoryIDsCamel          []string          `json:"memoryIds"`
		CandidateID             string            `json:"candidate_id"`
		CandidateIDCamel        string            `json:"candidateId"`
		CandidateIDs            []string          `json:"candidate_ids"`
		CandidateIDsCamel       []string          `json:"candidateIds"`
		ID                      string            `json:"id"`
		IDs                     []string          `json:"ids"`
		Matches                 []json.RawMessage `json:"matches"`
		Memories                []json.RawMessage `json:"memories"`
		Sessions                []json.RawMessage `json:"sessions"`
		SelectedSessions        []json.RawMessage `json:"selected_sessions"`
		SelectedSessionsCamel   []json.RawMessage `json:"selectedSessions"`
		SelectedMemories        []json.RawMessage `json:"selected_memories"`
		SelectedMemoriesCamel   []json.RawMessage `json:"selectedMemories"`
		RelevantSessions        []json.RawMessage `json:"relevant_sessions"`
		RelevantSessionsCamel   []json.RawMessage `json:"relevantSessions"`
		RelevantMemories        []json.RawMessage `json:"relevant_memories"`
		RelevantMemoriesCamel   []json.RawMessage `json:"relevantMemories"`
		CandidateSessions       []json.RawMessage `json:"candidate_sessions"`
		CandidateSessionsCamel  []json.RawMessage `json:"candidateSessions"`
		CandidateMemories       []json.RawMessage `json:"candidate_memories"`
		CandidateMemoriesCamel  []json.RawMessage `json:"candidateMemories"`
		Candidates              []json.RawMessage `json:"candidates"`
		Results                 []json.RawMessage `json:"results"`
		Nodes                   []json.RawMessage `json:"nodes"`
		Edges                   []json.RawMessage `json:"edges"`
		Items                   []json.RawMessage `json:"items"`
		Resources               []json.RawMessage `json:"resources"`
		Selection               json.RawMessage   `json:"selection"`
		Selected                json.RawMessage   `json:"selected"`
		Data                    json.RawMessage   `json:"data"`
		Payload                 json.RawMessage   `json:"payload"`
		Body                    json.RawMessage   `json:"body"`
		Resource                json.RawMessage   `json:"resource"`
		Attributes              json.RawMessage   `json:"attributes"`
		Properties              json.RawMessage   `json:"properties"`
		Attrs                   json.RawMessage   `json:"attrs"`
		Viewer                  json.RawMessage   `json:"viewer"`
		Edge                    json.RawMessage   `json:"edge"`
		Node                    json.RawMessage   `json:"node"`
		Result                  json.RawMessage   `json:"result"`
		Response                json.RawMessage   `json:"response"`
		Recall                  json.RawMessage   `json:"recall"`
		MemoryRecall            json.RawMessage   `json:"memory_recall"`
		MemoryRecallCamel       json.RawMessage   `json:"memoryRecall"`
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
			ids = recallIDs(object.SelectedSessionIDsCamel)
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
			ids = recallIDs(append([]string{object.MemoryID, object.MemoryIDCamel}, append(object.MemoryIDs, object.MemoryIDsCamel...)...))
		}
		if len(ids) == 0 {
			ids = recallIDs(append([]string{object.CandidateID, object.CandidateIDCamel}, append(object.CandidateIDs, object.CandidateIDsCamel...)...))
		}
		if len(ids) == 0 {
			ids = recallIDs(append([]string{object.ID}, object.IDs...))
		}
		if len(ids) == 0 {
			ids = recallIDsFromRawItems(
				object.Matches,
				object.Memories,
				object.Sessions,
				object.SelectedSessions,
				object.SelectedSessionsCamel,
				object.SelectedMemories,
				object.SelectedMemoriesCamel,
				object.RelevantSessions,
				object.RelevantSessionsCamel,
				object.RelevantMemories,
				object.RelevantMemoriesCamel,
				object.CandidateSessions,
				object.CandidateSessionsCamel,
				object.CandidateMemories,
				object.CandidateMemoriesCamel,
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
		if relevantMemoryLooksLikeID(scalar) {
			return "", relevantMemoryIDs([]string{scalar}), true
		}
		return scalar, nil, true
	}
	var rawObject map[string]json.RawMessage
	if err := json.Unmarshal([]byte(raw), &rawObject); err == nil {
		query := relevantMemoryQueryFromRawObject(rawObject)
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
				"items",
				"resources",
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
		Path                         string            `json:"path"`
		Paths                        []string          `json:"paths"`
		File                         string            `json:"file"`
		Files                        []string          `json:"files"`
		ID                           string            `json:"id"`
		IDs                          []string          `json:"ids"`
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
		ids := relevantMemoryIDs(append([]string{
			object.MemoryPath,
			object.MemoryPathCamel,
			object.SelectedMemoryPath,
			object.SelectedMemoryPathCamel,
			object.RelevantMemoryPath,
			object.RelevantMemoryPathCamel,
			object.FilePath,
			object.FilePathCamel,
			object.Path,
			object.File,
			object.ID,
			object.MemoryID,
			object.MemoryIDCamel,
			object.SelectedID,
			object.SelectedIDCamel,
		}, appendManyStringSlices(
			object.MemoryPaths,
			object.MemoryPathsCamel,
			object.SelectedMemoryPaths,
			object.SelectedMemoryPathsCamel,
			object.RelevantMemoryPaths,
			object.RelevantMemoryPathsCamel,
			object.FilePaths,
			object.FilePathsCamel,
			object.Paths,
			object.Files,
			object.IDs,
			object.MemoryIDs,
			object.MemoryIDsCamel,
			object.SelectedIDs,
			object.SelectedIDsCamel,
			object.RelevantIDs,
			object.RelevantIDsCamel,
		)...))
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

func relevantMemoryQueryFromRawObject(object map[string]json.RawMessage) string {
	for _, key := range []string{
		"query",
		"search_query",
		"searchQuery",
		"rewritten_query",
		"rewrittenQuery",
		"expanded_query",
		"expandedQuery",
	} {
		if value := stringFromRawJSON(object[key]); value != "" {
			return value
		}
	}
	return ""
}

func relevantMemoryIDsFromRawObject(object map[string]json.RawMessage) []string {
	var raw []string
	for _, key := range relevantMemoryItemIDKeys {
		if value, ok := object[key]; ok {
			raw = appendRelevantMemoryIDsFromRawValue(raw, value)
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
	for _, key := range recallItemIDKeys {
		if nested, ok := object[key]; ok {
			raw = appendRecallIDsFromRawValue(raw, nested)
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
	for _, key := range relevantMemoryItemIDKeys {
		if nested, ok := object[key]; ok {
			raw = appendRelevantMemoryIDsFromRawValue(raw, nested)
		}
	}
	for _, key := range relevantMemoryNestedItemKeys {
		if nested, ok := object[key]; ok {
			raw = appendRelevantMemoryIDsFromRawValue(raw, nested)
		}
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
	"selected_id",
	"selectedId",
	"selectedID",
	"selected_session_id",
	"selectedSessionId",
	"selectedSessionID",
	"selected_session",
	"selectedSession",
	"relevant_id",
	"relevantId",
	"relevantID",
	"relevant_session_id",
	"relevantSessionId",
	"relevantSessionID",
	"relevant_session",
	"relevantSession",
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
	"candidate_memory_id",
	"candidateMemoryId",
	"candidateMemoryID",
	"candidate_memory",
	"candidateMemory",
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
	"path",
	"paths",
	"file",
	"files",
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
	"memory",
	"summary",
	"candidate",
	"selected_session",
	"selectedSession",
	"selected_memory",
	"selectedMemory",
	"relevant_session",
	"relevantSession",
	"relevant_memory",
	"relevantMemory",
	"candidate_session",
	"candidateSession",
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
	"record",
	"entry",
	"item",
	"value",
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
		selection, ok := lookup[strings.TrimSpace(id)]
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
		kind, ok := normalizeFactKind(firstNonEmpty(entry.Kind, entry.Type, entry.FactType, entry.Category, entry.Label))
		if !ok {
			continue
		}
		text := strings.TrimSpace(firstNonEmpty(entry.Text, entry.Content, entry.Summary, entry.Value, entry.Detail))
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
	case "preference", "pref", "user_preference", "memory_preference":
		return FactPreference, true
	case "request", "user_request", "ask", "todo", "task":
		return FactRequest, true
	case "decision", "decided", "choice", "resolution":
		return FactDecision, true
	case "tool", "tool_use", "tool_result", "tool_call":
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
	fact := rawMemoryFact{
		Kind:                 stringMapField(value, "kind"),
		Type:                 stringMapField(value, "type"),
		FactType:             stringMapField(value, "fact_type", "factType"),
		Category:             stringMapField(value, "category"),
		Label:                stringMapField(value, "label"),
		Text:                 stringMapField(value, "text"),
		Content:              stringMapField(value, "content"),
		Summary:              stringMapField(value, "summary"),
		Value:                stringMapField(value, "value"),
		Detail:               stringMapField(value, "detail"),
		SourceUUID:           stringMapField(value, "source_uuid"),
		SourceUUIDCamel:      stringMapField(value, "sourceUuid"),
		SourceID:             stringMapField(value, "source_id"),
		SourceIDCamel:        stringMapField(value, "sourceId"),
		Source:               nestedIDFromValue(value["source"]),
		MessageUUID:          stringMapField(value, "message_uuid"),
		MessageUUIDCamel:     stringMapField(value, "messageUuid"),
		MessageID:            stringMapField(value, "message_id"),
		MessageIDCamel:       stringMapField(value, "messageId"),
		SourceMessageID:      stringMapField(value, "source_message_id"),
		SourceMessageIDCamel: stringMapField(value, "sourceMessageId"),
		UUID:                 stringMapField(value, "uuid"),
	}
	if fact.MessageUUID == "" {
		fact.MessageUUID = nestedIDFromValue(value["message"])
	}
	if fact.SourceMessageID == "" {
		fact.SourceMessageID = nestedIDFromValue(value["source_message"])
	}
	if firstNonEmpty(fact.Kind, fact.Type, fact.FactType, fact.Category, fact.Label) == "" {
		return rawMemoryFact{}, false
	}
	if firstNonEmpty(fact.Text, fact.Content, fact.Summary, fact.Value, fact.Detail) == "" {
		return rawMemoryFact{}, false
	}
	return fact, true
}

func stringMapField(value map[string]any, keys ...string) string {
	for _, key := range keys {
		if text := textFromValue(value[key]); text != "" {
			return text
		}
	}
	return ""
}

func nestedIDFromValue(value any) string {
	if text := directStringValue(value); text != "" {
		return text
	}
	object, ok := value.(map[string]any)
	if !ok {
		return ""
	}
	return stringMapField(object, "source_uuid", "sourceUuid", "source_id", "sourceId", "message_uuid", "messageUuid", "message_id", "messageId", "uuid", "id")
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
		return stringMapField(typed, "text", "content", "summary", "value", "detail")
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
