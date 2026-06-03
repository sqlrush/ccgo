package tui

import (
	"encoding/json"
	"sort"
	"strings"

	"ccgo/internal/contracts"
	"ccgo/internal/session"
)

func (s *REPLScreen) RestoreMessageAt(index int) bool {
	if index < 0 || index >= len(s.Messages) {
		return false
	}
	message := s.Messages[index]
	if !s.RestoreUserMessage(message) {
		return false
	}
	s.Messages = append([]Message(nil), s.Messages[:index]...)
	s.SelectedViewportLine = -1
	s.rebuildViewport()
	return true
}

func (s *REPLScreen) RestoreUserMessage(message Message) bool {
	return s.Prompt.RestoreUserMessage(message)
}

func (p *PromptState) RestoreUserMessage(message Message) bool {
	if message.Role != RoleUser {
		return false
	}
	contents := restoredMessagePastedContents(message)
	display := restoredMessageDisplay(message, contents)
	p.Text = display
	p.Cursor = len([]rune(display))
	p.replacePastedContents(contents)
	p.resetHistoryCursor()
	p.SeedNextPastedIDFromMessages([]Message{message})
	return true
}

func restoredMessagePastedContents(message Message) map[int]session.PastedContent {
	contents := clonePastedContents(message.PastedContents)
	for id, content := range imagePastedContentsFromBlocks(message) {
		if contents == nil {
			contents = map[int]session.PastedContent{}
		}
		if _, ok := contents[id]; !ok {
			contents[id] = content
		}
	}
	return contents
}

func restoredMessageDisplay(message Message, contents map[int]session.PastedContent) string {
	if message.Text != "" {
		return message.Text
	}
	display := displayFromContentBlocks(message)
	if display != "" {
		return display
	}
	ids := sortedImagePastedContentIDs(contents)
	parts := make([]string, 0, len(ids))
	for _, id := range ids {
		parts = append(parts, session.FormatImageRef(id))
	}
	return strings.Join(parts, " ")
}

func displayFromContentBlocks(message Message) string {
	if len(message.ContentBlocks) == 0 {
		return ""
	}
	parts := []string{}
	imageIndex := 0
	for _, block := range message.ContentBlocks {
		switch block.Type {
		case contracts.ContentText:
			if block.Text != "" {
				parts = append(parts, block.Text)
			}
		case contracts.ContentImage:
			id := imagePasteID(message, imageIndex)
			parts = append(parts, session.FormatImageRef(id))
			imageIndex++
		}
	}
	return strings.Join(parts, " ")
}

func imagePastedContentsFromBlocks(message Message) map[int]session.PastedContent {
	if len(message.ContentBlocks) == 0 {
		return nil
	}
	out := map[int]session.PastedContent{}
	imageIndex := 0
	for _, block := range message.ContentBlocks {
		if block.Type != contracts.ContentImage {
			continue
		}
		id := imagePasteID(message, imageIndex)
		imageIndex++
		mediaType, data, ok := base64ImageSource(block.Source)
		if !ok {
			continue
		}
		out[id] = session.PastedContent{
			ID:        id,
			Type:      session.PastedContentImage,
			Content:   data,
			MediaType: mediaType,
		}
	}
	if len(out) == 0 {
		return nil
	}
	return out
}

func imagePasteID(message Message, imageIndex int) int {
	if imageIndex >= 0 && imageIndex < len(message.ImagePasteIDs) && message.ImagePasteIDs[imageIndex] > 0 {
		return message.ImagePasteIDs[imageIndex]
	}
	return imageIndex + 1
}

func base64ImageSource(source any) (string, string, bool) {
	switch typed := source.(type) {
	case contracts.ImageSource:
		return base64ImageSourceValues(typed.Type, typed.MediaType, typed.Data)
	case map[string]any:
		return base64ImageSourceValues(
			stringAnyField(typed, "type"),
			stringAnyField(typed, "media_type", "mediaType"),
			stringAnyField(typed, "data"),
		)
	case map[string]string:
		return base64ImageSourceValues(
			typed["type"],
			firstString(typed["media_type"], typed["mediaType"]),
			typed["data"],
		)
	default:
		if source == nil {
			return "", "", false
		}
		data, err := json.Marshal(source)
		if err != nil {
			return "", "", false
		}
		var decoded contracts.ImageSource
		if err := json.Unmarshal(data, &decoded); err != nil {
			return "", "", false
		}
		return base64ImageSourceValues(decoded.Type, decoded.MediaType, decoded.Data)
	}
}

func base64ImageSourceValues(sourceType string, mediaType string, data string) (string, string, bool) {
	if sourceType != "" && sourceType != "base64" {
		return "", "", false
	}
	if data == "" {
		return "", "", false
	}
	if mediaType == "" {
		mediaType = "image/png"
	}
	return mediaType, data, true
}

func stringAnyField(values map[string]any, names ...string) string {
	for _, name := range names {
		if value, ok := values[name].(string); ok {
			return value
		}
	}
	return ""
}

func firstString(values ...string) string {
	for _, value := range values {
		if value != "" {
			return value
		}
	}
	return ""
}

func sortedImagePastedContentIDs(contents map[int]session.PastedContent) []int {
	ids := make([]int, 0, len(contents))
	for id, content := range contents {
		if content.Type == session.PastedContentImage {
			ids = append(ids, id)
		}
	}
	sort.Ints(ids)
	return ids
}
