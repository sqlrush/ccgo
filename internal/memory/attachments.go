package memory

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"sort"
	"strings"
	"time"
	"unicode/utf8"

	"ccgo/internal/contracts"
)

const (
	RelevantMemoriesAttachmentType = "relevant_memories"
	RelevantMemoriesSubtype        = "relevant_memories"
	MaxRelevantMemoryLines         = 200
	MaxRelevantMemoryBytes         = 4096
	MaxRelevantMemorySessionBytes  = 60 * 1024
	MaxRelevantMemoryAttachments   = 5
)

type RelevantMemorySelection struct {
	Path    string
	MtimeMs int64
}

type RelevantMemorySurfaceOptions struct {
	Now      time.Time
	MaxLines int
	MaxBytes int
}

type RelevantMemory struct {
	Path    string `json:"path"`
	Content string `json:"content"`
	MtimeMs int64  `json:"mtimeMs"`
	Header  string `json:"header,omitempty"`
	Limit   *int   `json:"limit,omitempty"`
}

func (m *RelevantMemory) UnmarshalJSON(data []byte) error {
	type relevantMemoryJSON RelevantMemory
	var aux struct {
		relevantMemoryJSON
		MtimeMSSnake int64 `json:"mtime_ms"`
	}
	if err := json.Unmarshal(data, &aux); err != nil {
		return err
	}
	*m = RelevantMemory(aux.relevantMemoryJSON)
	if m.MtimeMs == 0 {
		m.MtimeMs = aux.MtimeMSSnake
	}
	return nil
}

type SurfacedMemories struct {
	Paths      map[string]struct{}
	TotalBytes int
}

type RelevantMemoryPrefetchPlan struct {
	Input    string
	Surfaced SurfacedMemories
}

type RelevantMemoryPrefetchOptions struct {
	Root            string
	Limit           int
	MaxSessionBytes int
	Now             time.Time
	Agent           *Agent
}

type RelevantMemoryPrefetchResult struct {
	Plan     RelevantMemoryPrefetchPlan
	Selected []RelevantMemorySelection
	Memories []RelevantMemory
	Agent    *AgentRelevantMemoryResult
}

type RelevantMemoryReadState struct {
	Content string
	MtimeMs int64
	Limit   *int
}

func NewRelevantMemory(path string, content string, mtime time.Time, now time.Time) RelevantMemory {
	return RelevantMemory{
		Path:    path,
		Content: content,
		MtimeMs: mtime.UnixMilli(),
		Header:  RelevantMemoryHeader(path, mtime, now),
	}
}

func ReadMemoriesForSurfacing(selected []RelevantMemorySelection, options RelevantMemorySurfaceOptions) []RelevantMemory {
	if len(selected) == 0 {
		return nil
	}
	maxLines := options.MaxLines
	if maxLines <= 0 {
		maxLines = MaxRelevantMemoryLines
	}
	maxBytes := options.MaxBytes
	if maxBytes <= 0 {
		maxBytes = MaxRelevantMemoryBytes
	}
	memories := make([]RelevantMemory, 0, len(selected))
	for _, item := range selected {
		if item.Path == "" {
			continue
		}
		content, lineCount, truncatedByLines, truncatedByBytes, mtime, err := readRelevantMemoryFile(item, maxLines, maxBytes)
		if err != nil {
			continue
		}
		memory := NewRelevantMemory(item.Path, content, mtime, options.Now)
		if truncatedByLines || truncatedByBytes {
			reason := fmt.Sprintf("first %d lines", maxLines)
			if truncatedByBytes {
				reason = fmt.Sprintf("%d byte limit", maxBytes)
			}
			memory.Content += fmt.Sprintf("\n\n> This memory file was truncated (%s). Use the Read tool to view the complete file at: %s", reason, item.Path)
			memory.Limit = &lineCount
		}
		memories = append(memories, memory)
	}
	return memories
}

func FindRelevantMemorySelections(root string, query string, recentTools []string, surfaced map[string]struct{}, limit int) ([]RelevantMemorySelection, error) {
	headers, err := ScanDirectory(root, ScanOptions{})
	if err != nil {
		return nil, err
	}
	if len(headers) == 0 {
		return nil, nil
	}
	terms := queryTerms(query)
	if len(terms) == 0 {
		return nil, nil
	}
	type scoredSelection struct {
		selection RelevantMemorySelection
		score     int
		mtime     time.Time
	}
	var scored []scoredSelection
	for _, header := range headers {
		if surfaced != nil {
			if _, ok := surfaced[header.Path]; ok {
				continue
			}
		}
		score := relevantMemoryScore(header, terms, recentTools)
		if score <= 0 {
			continue
		}
		scored = append(scored, scoredSelection{
			selection: RelevantMemorySelection{Path: header.Path, MtimeMs: header.Mtime.UnixMilli()},
			score:     score,
			mtime:     header.Mtime,
		})
	}
	sort.SliceStable(scored, func(i, j int) bool {
		if scored[i].score != scored[j].score {
			return scored[i].score > scored[j].score
		}
		return scored[i].mtime.After(scored[j].mtime)
	})
	results := make([][]RelevantMemorySelection, 1)
	for _, item := range scored {
		results[0] = append(results[0], item.selection)
	}
	return SelectRelevantMemoryCandidates(results, nil, surfaced, limit), nil
}

func RelevantMemoryHeader(path string, mtime time.Time, now time.Time) string {
	if text := MemoryFreshnessText(mtime, now); text != "" {
		return text + "\n\nMemory: " + path + ":"
	}
	return fmt.Sprintf("Memory (saved %s): %s:", MemoryAge(mtime, now), path)
}

func RelevantMemoriesAttachmentMessage(memories []RelevantMemory) contracts.Message {
	if len(memories) == 0 {
		return contracts.Message{}
	}
	return contracts.Message{
		Type:    contracts.MessageAttachment,
		UUID:    contracts.NewID(),
		Subtype: RelevantMemoriesSubtype,
		Raw: map[string]any{
			"attachment": relevantMemoriesAttachmentPayload{Type: RelevantMemoriesAttachmentType, Memories: memories},
		},
	}
}

func RenderRelevantMemoriesAttachment(message contracts.Message, now time.Time) []contracts.Message {
	memories := RelevantMemoriesFromAttachmentMessage(message)
	if len(memories) == 0 {
		return nil
	}
	out := make([]contracts.Message, 0, len(memories))
	for _, item := range memories {
		header := item.Header
		if header == "" {
			header = RelevantMemoryHeader(item.Path, time.UnixMilli(item.MtimeMs), now)
		}
		out = append(out, contracts.Message{
			Type:    contracts.MessageUser,
			UUID:    contracts.NewID(),
			Subtype: RelevantMemoriesSubtype,
			IsMeta:  true,
			Content: []contracts.ContentBlock{contracts.NewTextBlock(wrapSystemReminder(header + "\n\n" + item.Content))},
		})
	}
	return out
}

func ExpandRelevantMemoryAttachments(messages []contracts.Message, now time.Time) []contracts.Message {
	if len(messages) == 0 {
		return nil
	}
	out := make([]contracts.Message, 0, len(messages))
	for _, message := range messages {
		rendered := RenderRelevantMemoriesAttachment(message, now)
		if len(rendered) == 0 {
			out = append(out, message)
			continue
		}
		out = append(out, rendered...)
	}
	return out
}

func CollectSurfacedMemories(messages []contracts.Message) SurfacedMemories {
	out := SurfacedMemories{Paths: map[string]struct{}{}}
	for _, message := range messages {
		for _, item := range RelevantMemoriesFromAttachmentMessage(message) {
			if item.Path == "" {
				continue
			}
			out.Paths[item.Path] = struct{}{}
			out.TotalBytes += len(item.Content)
		}
	}
	return out
}

func RelevantMemoryPrefetchPlanForMessages(messages []contracts.Message, maxSessionBytes int) (RelevantMemoryPrefetchPlan, bool) {
	if maxSessionBytes <= 0 {
		maxSessionBytes = MaxRelevantMemorySessionBytes
	}
	input := lastNonMetaUserText(messages)
	if input == "" || !strings.ContainsAny(strings.TrimSpace(input), " \t\r\n") {
		return RelevantMemoryPrefetchPlan{}, false
	}
	surfaced := CollectSurfacedMemories(messages)
	if surfaced.TotalBytes >= maxSessionBytes {
		return RelevantMemoryPrefetchPlan{}, false
	}
	return RelevantMemoryPrefetchPlan{Input: input, Surfaced: surfaced}, true
}

func PrefetchRelevantMemories(ctx context.Context, messages []contracts.Message, options RelevantMemoryPrefetchOptions) (RelevantMemoryPrefetchResult, error) {
	if ctx == nil {
		ctx = context.Background()
	}
	if strings.TrimSpace(options.Root) == "" {
		return RelevantMemoryPrefetchResult{}, nil
	}
	plan, ok := RelevantMemoryPrefetchPlanForMessages(messages, options.MaxSessionBytes)
	if !ok {
		return RelevantMemoryPrefetchResult{}, nil
	}
	if err := ctx.Err(); err != nil {
		return RelevantMemoryPrefetchResult{Plan: plan}, err
	}
	recentTools := CollectRecentSuccessfulTools(messages)
	var selected []RelevantMemorySelection
	var agentResult *AgentRelevantMemoryResult
	if options.Agent != nil {
		result, err := options.Agent.SelectRelevantMemories(ctx, options.Root, plan.Input, RelevantMemorySelectorOptions{
			Limit:       options.Limit,
			RecentTools: recentTools,
			Surfaced:    plan.Surfaced.Paths,
		})
		if err != nil {
			return RelevantMemoryPrefetchResult{Plan: plan}, err
		}
		agentResult = &result
		selected = result.Selected
	} else {
		var err error
		selected, err = FindRelevantMemorySelections(
			options.Root,
			plan.Input,
			recentTools,
			plan.Surfaced.Paths,
			options.Limit,
		)
		if err != nil {
			return RelevantMemoryPrefetchResult{Plan: plan}, err
		}
	}
	if err := ctx.Err(); err != nil {
		return RelevantMemoryPrefetchResult{Plan: plan, Selected: selected, Agent: agentResult}, err
	}
	memories := ReadMemoriesForSurfacing(selected, RelevantMemorySurfaceOptions{Now: options.Now})
	if err := ctx.Err(); err != nil {
		return RelevantMemoryPrefetchResult{Plan: plan, Selected: selected, Memories: memories, Agent: agentResult}, err
	}
	return RelevantMemoryPrefetchResult{Plan: plan, Selected: selected, Memories: memories, Agent: agentResult}, nil
}

func SelectRelevantMemoryCandidates(results [][]RelevantMemorySelection, state map[string]RelevantMemoryReadState, surfaced map[string]struct{}, limit int) []RelevantMemorySelection {
	if limit <= 0 {
		limit = MaxRelevantMemoryAttachments
	}
	selected := make([]RelevantMemorySelection, 0, limit)
	for _, group := range results {
		for _, item := range group {
			if item.Path == "" {
				continue
			}
			if state != nil {
				if _, ok := state[item.Path]; ok {
					continue
				}
			}
			if surfaced != nil {
				if _, ok := surfaced[item.Path]; ok {
					continue
				}
			}
			selected = append(selected, item)
			if len(selected) >= limit {
				return selected
			}
		}
	}
	return selected
}

func CollectRecentSuccessfulTools(messages []contracts.Message) []string {
	lastUser := lastHumanTurnIndex(messages)
	if lastUser < 0 {
		return nil
	}
	useIDToName := map[string]string{}
	var useOrder []string
	resultByUseID := map[string]bool{}
	for i := len(messages) - 1; i >= 0; i-- {
		message := messages[i]
		if isHumanTurnMessage(message) && i != lastUser {
			break
		}
		switch message.Type {
		case contracts.MessageAssistant:
			for _, block := range message.Content {
				if block.Type == contracts.ContentToolUse && block.ID != "" && block.Name != "" {
					if _, ok := useIDToName[block.ID]; !ok {
						useOrder = append(useOrder, block.ID)
					}
					useIDToName[block.ID] = block.Name
				}
			}
		case contracts.MessageUser:
			for _, block := range message.Content {
				if block.Type == contracts.ContentToolResult && block.ToolUseID != "" {
					resultByUseID[block.ToolUseID] = block.IsError
				}
			}
		}
	}
	failed := map[string]struct{}{}
	var succeeded []string
	seenSucceeded := map[string]struct{}{}
	for _, id := range useOrder {
		name := useIDToName[id]
		errored, ok := resultByUseID[id]
		if !ok {
			continue
		}
		if errored {
			failed[name] = struct{}{}
			continue
		}
		if _, ok := seenSucceeded[name]; !ok {
			succeeded = append(succeeded, name)
			seenSucceeded[name] = struct{}{}
		}
	}
	out := succeeded[:0]
	for _, name := range succeeded {
		if _, ok := failed[name]; !ok {
			out = append(out, name)
		}
	}
	return out
}

func FilterDuplicateRelevantMemoryAttachments(messages []contracts.Message, state map[string]RelevantMemoryReadState) []contracts.Message {
	if len(messages) == 0 {
		return nil
	}
	out := make([]contracts.Message, 0, len(messages))
	for _, message := range messages {
		memories := RelevantMemoriesFromAttachmentMessage(message)
		if len(memories) == 0 {
			out = append(out, message)
			continue
		}
		filtered := FilterDuplicateRelevantMemories(memories, state)
		if len(filtered) == 0 {
			continue
		}
		out = append(out, withRelevantMemories(message, filtered))
	}
	return out
}

func FilterDuplicateRelevantMemories(memories []RelevantMemory, state map[string]RelevantMemoryReadState) []RelevantMemory {
	if len(memories) == 0 {
		return nil
	}
	filtered := make([]RelevantMemory, 0, len(memories))
	for _, item := range memories {
		if item.Path == "" {
			continue
		}
		if state != nil {
			if _, ok := state[item.Path]; ok {
				continue
			}
		}
		filtered = append(filtered, item)
		if state != nil {
			state[item.Path] = RelevantMemoryReadState{
				Content: item.Content,
				MtimeMs: item.MtimeMs,
				Limit:   item.Limit,
			}
		}
	}
	return filtered
}

func lastHumanTurnIndex(messages []contracts.Message) int {
	for i := len(messages) - 1; i >= 0; i-- {
		if isHumanTurnMessage(messages[i]) {
			return i
		}
	}
	return -1
}

func isHumanTurnMessage(message contracts.Message) bool {
	return message.Type == contracts.MessageUser && !message.IsMeta && !hasToolResultBlock(message)
}

func hasToolResultBlock(message contracts.Message) bool {
	for _, block := range message.Content {
		if block.Type == contracts.ContentToolResult {
			return true
		}
	}
	return false
}

func lastNonMetaUserText(messages []contracts.Message) string {
	for i := len(messages) - 1; i >= 0; i-- {
		if messages[i].Type != contracts.MessageUser || messages[i].IsMeta {
			continue
		}
		var parts []string
		for _, block := range messages[i].Content {
			if block.Type == contracts.ContentText && block.Text != "" {
				parts = append(parts, block.Text)
			}
		}
		text := strings.TrimSpace(strings.Join(parts, "\n"))
		if text != "" {
			return text
		}
	}
	return ""
}

func RelevantMemoriesFromAttachmentMessage(message contracts.Message) []RelevantMemory {
	if message.Type != contracts.MessageAttachment {
		return nil
	}
	if attachment, ok := message.Raw["attachment"]; ok {
		return relevantMemoriesFromPayload(attachment)
	}
	return relevantMemoriesFromPayload(message.Raw)
}

func withRelevantMemories(message contracts.Message, memories []RelevantMemory) contracts.Message {
	if message.Raw == nil {
		message.Raw = map[string]any{}
	}
	message.Type = contracts.MessageAttachment
	message.Subtype = RelevantMemoriesSubtype
	message.Raw["attachment"] = relevantMemoriesAttachmentPayload{Type: RelevantMemoriesAttachmentType, Memories: memories}
	return message
}

func wrapSystemReminder(content string) string {
	return "<system-reminder>\n" + content + "\n</system-reminder>"
}

func relevantMemoriesFromPayload(value any) []RelevantMemory {
	if value == nil {
		return nil
	}
	var payload relevantMemoriesAttachmentPayload
	data, err := json.Marshal(value)
	if err != nil {
		return nil
	}
	if err := json.Unmarshal(data, &payload); err != nil {
		return nil
	}
	if payload.Type != RelevantMemoriesAttachmentType || len(payload.Memories) == 0 {
		return nil
	}
	return payload.Memories
}

type relevantMemoriesAttachmentPayload struct {
	Type     string           `json:"type"`
	Memories []RelevantMemory `json:"memories"`
}

func relevantMemoryScore(header Header, terms []string, recentTools []string) int {
	haystack := strings.ToLower(header.Filename + " " + header.Description)
	if suppressRecentToolReference(haystack, recentTools) {
		return 0
	}
	score := 0
	for _, term := range terms {
		score += strings.Count(haystack, term)
	}
	return score
}

func suppressRecentToolReference(haystack string, recentTools []string) bool {
	if len(recentTools) == 0 {
		return false
	}
	hasReferenceWord := strings.Contains(haystack, "reference") ||
		strings.Contains(haystack, "docs") ||
		strings.Contains(haystack, "documentation") ||
		strings.Contains(haystack, "api") ||
		strings.Contains(haystack, "usage")
	if !hasReferenceWord {
		return false
	}
	hasWarningWord := strings.Contains(haystack, "warning") ||
		strings.Contains(haystack, "gotcha") ||
		strings.Contains(haystack, "issue") ||
		strings.Contains(haystack, "bug") ||
		strings.Contains(haystack, "error")
	if hasWarningWord {
		return false
	}
	for _, tool := range recentTools {
		tool = strings.ToLower(strings.TrimSpace(tool))
		if tool != "" && strings.Contains(haystack, tool) {
			return true
		}
	}
	return false
}

func readRelevantMemoryFile(item RelevantMemorySelection, maxLines int, maxBytes int) (string, int, bool, bool, time.Time, error) {
	data, err := os.ReadFile(item.Path)
	if err != nil {
		return "", 0, false, false, time.Time{}, err
	}
	mtime := time.UnixMilli(item.MtimeMs)
	if item.MtimeMs == 0 {
		if info, err := os.Stat(item.Path); err == nil {
			mtime = info.ModTime()
		}
	}
	truncatedByBytes := false
	if maxBytes > 0 && len(data) > maxBytes {
		data = data[:maxBytes]
		truncatedByBytes = true
		for len(data) > 0 && !utf8.Valid(data) {
			data = data[:len(data)-1]
		}
	}
	content := normalizeMemoryFileContent(string(data))
	selected, lineCount, truncatedByLines := firstMemoryLines(content, maxLines)
	return selected, lineCount, truncatedByLines, truncatedByBytes, mtime, nil
}

func normalizeMemoryFileContent(content string) string {
	content = strings.TrimPrefix(content, "\ufeff")
	content = strings.ReplaceAll(content, "\r\n", "\n")
	return strings.ReplaceAll(content, "\r", "\n")
}

func firstMemoryLines(content string, maxLines int) (string, int, bool) {
	if maxLines <= 0 {
		return "", 0, content != ""
	}
	lines := 0
	for i, r := range content {
		if r != '\n' {
			continue
		}
		lines++
		if lines == maxLines {
			return content[:i+1], lines, i+1 < len(content)
		}
	}
	return content, countMemoryLines(content), false
}

func countMemoryLines(content string) int {
	if content == "" {
		return 0
	}
	lines := strings.Count(content, "\n")
	if !strings.HasSuffix(content, "\n") {
		lines++
	}
	return lines
}
