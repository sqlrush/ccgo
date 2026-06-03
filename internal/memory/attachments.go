package memory

import (
	"encoding/json"
	"fmt"
	"os"
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

func RelevantMemoriesFromAttachmentMessage(message contracts.Message) []RelevantMemory {
	if message.Type != contracts.MessageAttachment {
		return nil
	}
	if attachment, ok := message.Raw["attachment"]; ok {
		return relevantMemoriesFromPayload(attachment)
	}
	return relevantMemoriesFromPayload(message.Raw)
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
