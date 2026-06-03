package memory

import (
	"encoding/json"
	"fmt"
	"time"

	"ccgo/internal/contracts"
)

const (
	RelevantMemoriesAttachmentType = "relevant_memories"
	RelevantMemoriesSubtype        = "relevant_memories"
)

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
