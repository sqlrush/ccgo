package session

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"ccgo/internal/contracts"
	"ccgo/internal/platform"
)

const (
	DefaultToolResultBudgetChars = 200_000
	PersistedOutputTag           = "<persisted-output>"
	persistedOutputClosingTag    = "</persisted-output>"
	toolResultsSubdir            = "tool-results"
	toolResultPreviewBytes       = 2000
)

type ContentReplacementState struct {
	SeenIDs      map[string]struct{}
	Replacements map[string]string
}

type ToolResultBudgetOptions struct {
	LimitChars    int
	StoreDir      string
	SkipToolNames map[string]struct{}
}

type toolResultCandidate struct {
	ToolUseID string
	Content   any
	Size      int
}

func NewContentReplacementState() *ContentReplacementState {
	return &ContentReplacementState{
		SeenIDs:      map[string]struct{}{},
		Replacements: map[string]string{},
	}
}

func ReconstructContentReplacementState(messages []contracts.Message, records []ContentReplacementRecord) *ContentReplacementState {
	state := NewContentReplacementState()
	candidateIDs := map[string]struct{}{}
	for _, group := range collectToolResultCandidatesByMessage(messages) {
		for _, candidate := range group {
			candidateIDs[candidate.ToolUseID] = struct{}{}
			state.SeenIDs[candidate.ToolUseID] = struct{}{}
		}
	}
	for _, record := range records {
		if record.ToolUseID == "" || record.Replacement == "" {
			continue
		}
		if record.Kind != "" && record.Kind != "tool-result" {
			continue
		}
		if _, ok := candidateIDs[record.ToolUseID]; ok {
			state.Replacements[record.ToolUseID] = record.Replacement
		}
	}
	return state
}

func ApplyToolResultBudget(messages []contracts.Message, state *ContentReplacementState, options ToolResultBudgetOptions) ([]contracts.Message, []ContentReplacementRecord, error) {
	if state == nil {
		return messages, nil, nil
	}
	if state.SeenIDs == nil {
		state.SeenIDs = map[string]struct{}{}
	}
	if state.Replacements == nil {
		state.Replacements = map[string]string{}
	}
	limit := options.LimitChars
	if limit <= 0 {
		limit = DefaultToolResultBudgetChars
	}

	nameByToolUseID := buildToolNameMap(messages)
	replacementMap := map[string]string{}
	var toPersist []toolResultCandidate
	for _, candidates := range collectToolResultCandidatesByMessage(messages) {
		var frozen []toolResultCandidate
		var fresh []toolResultCandidate
		for _, candidate := range candidates {
			if replacement, ok := state.Replacements[candidate.ToolUseID]; ok {
				replacementMap[candidate.ToolUseID] = replacement
				continue
			}
			if _, ok := state.SeenIDs[candidate.ToolUseID]; ok {
				frozen = append(frozen, candidate)
				continue
			}
			fresh = append(fresh, candidate)
		}
		if len(fresh) == 0 {
			for _, candidate := range candidates {
				state.SeenIDs[candidate.ToolUseID] = struct{}{}
			}
			continue
		}

		var eligible []toolResultCandidate
		for _, candidate := range fresh {
			if _, skip := options.SkipToolNames[nameByToolUseID[candidate.ToolUseID]]; skip {
				state.SeenIDs[candidate.ToolUseID] = struct{}{}
				continue
			}
			eligible = append(eligible, candidate)
		}

		frozenSize := 0
		for _, candidate := range frozen {
			frozenSize += candidate.Size
		}
		freshSize := 0
		for _, candidate := range eligible {
			freshSize += candidate.Size
		}

		selected := []toolResultCandidate{}
		if frozenSize+freshSize > limit {
			selected = selectFreshToolResultsToReplace(eligible, frozenSize, limit)
		}
		selectedIDs := map[string]struct{}{}
		for _, candidate := range selected {
			selectedIDs[candidate.ToolUseID] = struct{}{}
		}
		for _, candidate := range candidates {
			if _, selected := selectedIDs[candidate.ToolUseID]; !selected {
				state.SeenIDs[candidate.ToolUseID] = struct{}{}
			}
		}
		toPersist = append(toPersist, selected...)
	}

	if len(replacementMap) == 0 && len(toPersist) == 0 {
		return messages, nil, nil
	}

	var newlyReplaced []ContentReplacementRecord
	for _, candidate := range toPersist {
		state.SeenIDs[candidate.ToolUseID] = struct{}{}
		replacement, err := buildToolResultReplacement(candidate, options.StoreDir)
		if err != nil {
			continue
		}
		replacementMap[candidate.ToolUseID] = replacement
		state.Replacements[candidate.ToolUseID] = replacement
		newlyReplaced = append(newlyReplaced, ContentReplacementRecord{
			Kind:        "tool-result",
			ToolUseID:   candidate.ToolUseID,
			Replacement: replacement,
		})
	}
	if len(replacementMap) == 0 {
		return messages, nil, nil
	}
	return replaceToolResultContents(messages, replacementMap), newlyReplaced, nil
}

func AppendContentReplacements(path string, sessionID contracts.ID, records []ContentReplacementRecord) error {
	if len(records) == 0 {
		return nil
	}
	entry := ContentReplacementEntry{
		Type:         "content-replacement",
		SessionID:    sessionID,
		Replacements: records,
	}
	if err := platform.EnsureDir(filepath.Dir(path)); err != nil {
		return err
	}
	f, err := os.OpenFile(path, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o600)
	if err != nil {
		return err
	}
	defer f.Close()
	encoded, err := json.Marshal(entry)
	if err != nil {
		return err
	}
	_, err = f.Write(append(encoded, '\n'))
	return err
}

func collectToolResultCandidatesByMessage(messages []contracts.Message) [][]toolResultCandidate {
	var groups [][]toolResultCandidate
	var current []toolResultCandidate
	flush := func() {
		if len(current) > 0 {
			groups = append(groups, current)
		}
		current = nil
	}
	seenAssistantIDs := map[string]struct{}{}
	for _, message := range messages {
		switch message.Type {
		case contracts.MessageUser:
			current = append(current, collectToolResultCandidates(message)...)
		case contracts.MessageAssistant:
			if message.ID == "" {
				flush()
				continue
			}
			if _, ok := seenAssistantIDs[message.ID]; !ok {
				flush()
				seenAssistantIDs[message.ID] = struct{}{}
			}
		}
	}
	flush()
	return groups
}

func collectToolResultCandidates(message contracts.Message) []toolResultCandidate {
	var out []toolResultCandidate
	for _, block := range message.Content {
		if block.Type != contracts.ContentToolResult || block.ToolUseID == "" || block.Content == nil {
			continue
		}
		if isPersistedToolResult(block.Content) || hasImageContent(block.Content) {
			continue
		}
		out = append(out, toolResultCandidate{
			ToolUseID: block.ToolUseID,
			Content:   block.Content,
			Size:      toolResultContentSize(block.Content),
		})
	}
	return out
}

func buildToolNameMap(messages []contracts.Message) map[string]string {
	out := map[string]string{}
	for _, message := range messages {
		if message.Type != contracts.MessageAssistant {
			continue
		}
		for _, block := range message.Content {
			if block.Type == contracts.ContentToolUse && block.ID != "" {
				out[block.ID] = block.Name
			}
		}
	}
	return out
}

func selectFreshToolResultsToReplace(fresh []toolResultCandidate, frozenSize int, limit int) []toolResultCandidate {
	sorted := append([]toolResultCandidate(nil), fresh...)
	sort.SliceStable(sorted, func(i, j int) bool {
		return sorted[i].Size > sorted[j].Size
	})
	remaining := frozenSize
	for _, candidate := range fresh {
		remaining += candidate.Size
	}
	var selected []toolResultCandidate
	for _, candidate := range sorted {
		if remaining <= limit {
			break
		}
		selected = append(selected, candidate)
		remaining -= candidate.Size
	}
	return selected
}

func replaceToolResultContents(messages []contracts.Message, replacementMap map[string]string) []contracts.Message {
	out := append([]contracts.Message(nil), messages...)
	for i, message := range out {
		if message.Type != contracts.MessageUser {
			continue
		}
		changed := false
		content := append([]contracts.ContentBlock(nil), message.Content...)
		for j, block := range content {
			replacement, ok := replacementMap[block.ToolUseID]
			if block.Type != contracts.ContentToolResult || !ok {
				continue
			}
			content[j].Content = replacement
			changed = true
		}
		if changed {
			out[i].Content = content
		}
	}
	return out
}

func buildToolResultReplacement(candidate toolResultCandidate, storeDir string) (string, error) {
	persisted, err := persistToolResultContent(candidate, storeDir)
	if err != nil {
		return "", err
	}
	return buildPersistedOutputMessage(persisted), nil
}

type persistedToolResult struct {
	Path         string
	OriginalSize int
	Preview      string
	HasMore      bool
}

func persistToolResultContent(candidate toolResultCandidate, storeDir string) (persistedToolResult, error) {
	content, isJSON, err := serializableToolResultContent(candidate.Content)
	if err != nil {
		return persistedToolResult{}, err
	}
	if storeDir == "" {
		storeDir = filepath.Join(platform.ClaudeHomeDir(), toolResultsSubdir)
	}
	if err := platform.EnsureDir(storeDir); err != nil {
		return persistedToolResult{}, err
	}
	ext := "txt"
	if isJSON {
		ext = "json"
	}
	path := filepath.Join(storeDir, sanitizeToolResultID(candidate.ToolUseID)+"."+ext)
	if err := writeFileOnce(path, []byte(content)); err != nil {
		return persistedToolResult{}, err
	}
	preview, hasMore := generateToolResultPreview(content, toolResultPreviewBytes)
	return persistedToolResult{
		Path:         path,
		OriginalSize: len(content),
		Preview:      preview,
		HasMore:      hasMore,
	}, nil
}

func serializableToolResultContent(content any) (string, bool, error) {
	if text, ok := content.(string); ok {
		return text, false, nil
	}
	blocks, ok := contentBlocksFromAny(content)
	if !ok {
		return "", false, fmt.Errorf("unsupported tool result content type %T", content)
	}
	for _, block := range blocks {
		if block.Type != contracts.ContentText {
			return "", false, fmt.Errorf("cannot persist non-text tool result content")
		}
	}
	data, err := json.MarshalIndent(blocks, "", "  ")
	if err != nil {
		return "", false, err
	}
	return string(data), true, nil
}

func writeFileOnce(path string, data []byte) error {
	f, err := os.OpenFile(path, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o600)
	if err != nil {
		if os.IsExist(err) {
			return nil
		}
		return err
	}
	defer f.Close()
	_, err = f.Write(data)
	return err
}

func buildPersistedOutputMessage(result persistedToolResult) string {
	var b strings.Builder
	b.WriteString(PersistedOutputTag)
	b.WriteString("\n")
	b.WriteString(fmt.Sprintf("Output too large (%s). Full output saved to: %s\n\n", formatFileSize(result.OriginalSize), result.Path))
	b.WriteString(fmt.Sprintf("Preview (first %s):\n", formatFileSize(toolResultPreviewBytes)))
	b.WriteString(result.Preview)
	if result.HasMore {
		b.WriteString("\n...\n")
	} else {
		b.WriteString("\n")
	}
	b.WriteString(persistedOutputClosingTag)
	return b.String()
}

func generateToolResultPreview(content string, maxBytes int) (string, bool) {
	if len(content) <= maxBytes {
		return content, false
	}
	truncated := content[:maxBytes]
	if idx := strings.LastIndex(truncated, "\n"); idx > maxBytes/2 {
		return content[:idx], true
	}
	return truncated, true
}

func toolResultContentSize(content any) int {
	if text, ok := content.(string); ok {
		return len(text)
	}
	blocks, ok := contentBlocksFromAny(content)
	if !ok {
		data, err := json.Marshal(content)
		if err != nil {
			return 0
		}
		return len(data)
	}
	size := 0
	for _, block := range blocks {
		if block.Type == contracts.ContentText {
			size += len(block.Text)
		}
	}
	return size
}

func isPersistedToolResult(content any) bool {
	text, ok := content.(string)
	return ok && strings.HasPrefix(text, PersistedOutputTag)
}

func hasImageContent(content any) bool {
	blocks, ok := contentBlocksFromAny(content)
	if !ok {
		return false
	}
	for _, block := range blocks {
		if block.Type == contracts.ContentImage {
			return true
		}
	}
	return false
}

func contentBlocksFromAny(content any) ([]contracts.ContentBlock, bool) {
	switch value := content.(type) {
	case []contracts.ContentBlock:
		return value, true
	case []any:
		data, err := json.Marshal(value)
		if err != nil {
			return nil, false
		}
		var blocks []contracts.ContentBlock
		if err := json.Unmarshal(data, &blocks); err != nil {
			return nil, false
		}
		return blocks, true
	default:
		data, err := json.Marshal(value)
		if err != nil {
			return nil, false
		}
		var blocks []contracts.ContentBlock
		if err := json.Unmarshal(data, &blocks); err != nil || len(blocks) == 0 {
			return nil, false
		}
		return blocks, true
	}
}

func sanitizeToolResultID(id string) string {
	name := filepath.Base(id)
	name = strings.Map(func(r rune) rune {
		switch r {
		case '/', '\\', ':', 0:
			return '-'
		default:
			return r
		}
	}, name)
	if name == "" || name == "." || name == string(os.PathSeparator) {
		return "tool-result"
	}
	return name
}

func formatFileSize(size int) string {
	const unit = 1024
	if size < unit {
		return fmt.Sprintf("%d B", size)
	}
	value := float64(size)
	units := []string{"KB", "MB", "GB"}
	for _, suffix := range units {
		value /= unit
		if value < unit {
			return fmt.Sprintf("%.1f %s", value, suffix)
		}
	}
	return fmt.Sprintf("%.1f TB", value/unit)
}
