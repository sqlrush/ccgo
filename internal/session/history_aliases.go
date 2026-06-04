package session

import (
	"bytes"
	"encoding/json"
	"strconv"
	"strings"
	"time"

	"ccgo/internal/contracts"
)

func (d *ImageDimensions) UnmarshalJSON(data []byte) error {
	type ImageDimensionsJSON ImageDimensions
	var aux struct {
		*ImageDimensionsJSON
		OriginalWidthSnake  int `json:"original_width"`
		OriginalHeightSnake int `json:"original_height"`
		DisplayWidthSnake   int `json:"display_width"`
		DisplayHeightSnake  int `json:"display_height"`
		Width               int `json:"width"`
		Height              int `json:"height"`
	}
	base := ImageDimensionsJSON{}
	aux.ImageDimensionsJSON = &base
	if err := json.Unmarshal(data, &aux); err != nil {
		return err
	}
	*d = ImageDimensions(base)
	if d.OriginalWidth == 0 {
		d.OriginalWidth = aux.OriginalWidthSnake
	}
	if d.OriginalHeight == 0 {
		d.OriginalHeight = aux.OriginalHeightSnake
	}
	if d.DisplayWidth == 0 {
		d.DisplayWidth = aux.DisplayWidthSnake
	}
	if d.DisplayHeight == 0 {
		d.DisplayHeight = aux.DisplayHeightSnake
	}
	if d.OriginalWidth == 0 {
		d.OriginalWidth = aux.Width
	}
	if d.OriginalHeight == 0 {
		d.OriginalHeight = aux.Height
	}
	if d.DisplayWidth == 0 {
		d.DisplayWidth = d.OriginalWidth
	}
	if d.DisplayHeight == 0 {
		d.DisplayHeight = d.OriginalHeight
	}
	return nil
}

func (c *PastedContent) UnmarshalJSON(data []byte) error {
	fields := map[string]json.RawMessage{}
	if err := json.Unmarshal(data, &fields); err != nil {
		return err
	}
	*c = PastedContent{
		ID:         historyIntJSONField(fields, historyIDFieldNames()...),
		Type:       canonicalPastedContentType(historyStringJSONField(fields, "type", "kind", "pastedType", "pasted_type")),
		Content:    historyStringJSONField(fields, "content", "value", "data", "base64"),
		MediaType:  historyStringJSONField(fields, "mediaType", "media_type", "mimeType", "mime_type", "contentType", "content_type"),
		Filename:   historyStringJSONField(fields, "filename", "fileName", "file_name", "name"),
		Dimensions: historyDimensionsJSONField(fields, "dimensions", "imageDimensions", "image_dimensions"),
		SourcePath: historyStringJSONField(fields, "sourcePath", "source_path", "filePath", "file_path", "path"),
	}
	return nil
}

func (c *StoredPastedContent) UnmarshalJSON(data []byte) error {
	fields := map[string]json.RawMessage{}
	if err := json.Unmarshal(data, &fields); err != nil {
		return err
	}
	*c = StoredPastedContent{
		ID:          historyIntJSONField(fields, historyIDFieldNames()...),
		Type:        canonicalPastedContentType(historyStringJSONField(fields, "type", "kind", "pastedType", "pasted_type")),
		Content:     historyStringJSONField(fields, "content", "value", "data", "base64"),
		ContentHash: historyStringJSONField(fields, "contentHash", "content_hash", "hash", "contentDigest", "content_digest"),
		MediaType:   historyStringJSONField(fields, "mediaType", "media_type", "mimeType", "mime_type", "contentType", "content_type"),
		Filename:    historyStringJSONField(fields, "filename", "fileName", "file_name", "name"),
		Dimensions:  historyDimensionsJSONField(fields, "dimensions", "imageDimensions", "image_dimensions"),
		SourcePath:  historyStringJSONField(fields, "sourcePath", "source_path", "filePath", "file_path", "path"),
	}
	return nil
}

func canonicalPastedContentType(value string) string {
	trimmed := strings.TrimSpace(value)
	normalized := strings.ToLower(trimmed)
	normalized = strings.ReplaceAll(normalized, "-", "_")
	normalized = strings.ReplaceAll(normalized, " ", "_")
	switch normalized {
	case "", PastedContentText:
		return normalized
	case PastedContentImage, "input_image", "inputimage", "pasted_image", "pastedimage", "image_paste", "imagepaste", "file_image", "fileimage":
		return PastedContentImage
	case "paste", "pasted_text", "pastedtext", "input_text", "inputtext", "text_paste", "textpaste", "clipboard_text", "clipboardtext":
		return PastedContentText
	default:
		return trimmed
	}
}

func historyIDFieldNames() []string {
	return []string{
		"id",
		"pastedId",
		"pastedID",
		"pasted_id",
		"pastedContentId",
		"pastedContentID",
		"pasted_content_id",
		"contentId",
		"contentID",
		"content_id",
		"attachmentId",
		"attachmentID",
		"attachment_id",
		"imageId",
		"imageID",
		"image_id",
	}
}

func historyStringJSONField(fields map[string]json.RawMessage, names ...string) string {
	for _, name := range names {
		raw, ok := fields[name]
		if !ok {
			continue
		}
		var value string
		if err := json.Unmarshal(raw, &value); err == nil {
			return value
		}
	}
	return ""
}

func historyIntJSONField(fields map[string]json.RawMessage, names ...string) int {
	for _, name := range names {
		raw, ok := fields[name]
		if !ok {
			continue
		}
		var value int
		if err := json.Unmarshal(raw, &value); err == nil {
			return value
		}
		var text string
		if err := json.Unmarshal(raw, &text); err == nil {
			parsed, err := strconv.Atoi(strings.TrimSpace(text))
			if err == nil {
				return parsed
			}
		}
	}
	return 0
}

func historyDimensionsJSONField(fields map[string]json.RawMessage, names ...string) *ImageDimensions {
	for _, name := range names {
		raw, ok := fields[name]
		if !ok {
			continue
		}
		if bytes.Equal(bytes.TrimSpace(raw), []byte("null")) {
			continue
		}
		var dimensions ImageDimensions
		if err := json.Unmarshal(raw, &dimensions); err == nil {
			return &dimensions
		}
	}
	return nil
}

func historyIDJSONField(fields map[string]json.RawMessage, names ...string) contracts.ID {
	for _, name := range names {
		raw, ok := fields[name]
		if !ok {
			continue
		}
		var id contracts.ID
		if err := json.Unmarshal(raw, &id); err == nil {
			return id
		}
	}
	return ""
}

func historyTimestampJSONField(fields map[string]json.RawMessage, names ...string) int64 {
	for _, name := range names {
		raw, ok := fields[name]
		if !ok {
			continue
		}
		var value int64
		if err := json.Unmarshal(raw, &value); err == nil {
			return value
		}
		var text string
		if err := json.Unmarshal(raw, &text); err == nil {
			trimmed := strings.TrimSpace(text)
			parsed, err := strconv.ParseInt(trimmed, 10, 64)
			if err == nil {
				return parsed
			}
			when, err := time.Parse(time.RFC3339Nano, trimmed)
			if err == nil {
				return when.UnixMilli()
			}
			when, err = time.Parse(time.RFC3339, trimmed)
			if err == nil {
				return when.UnixMilli()
			}
		}
	}
	return 0
}

func historyPastedContentsJSONField(fields map[string]json.RawMessage, names ...string) map[int]PastedContent {
	for _, name := range names {
		raw, ok := fields[name]
		if !ok {
			continue
		}
		var byID map[int]PastedContent
		if err := json.Unmarshal(raw, &byID); err == nil {
			return normalizeHistoryPastedContentIDs(byID)
		}
		var list []PastedContent
		if err := json.Unmarshal(raw, &list); err == nil {
			return historyPastedContentListMap(list)
		}
		var single PastedContent
		if err := json.Unmarshal(raw, &single); err == nil && single.ID > 0 {
			return historyPastedContentListMap([]PastedContent{single})
		}
	}
	return nil
}

func normalizeHistoryPastedContentIDs(contents map[int]PastedContent) map[int]PastedContent {
	out := make(map[int]PastedContent, len(contents))
	for id, content := range contents {
		if content.ID == 0 {
			content.ID = id
		}
		out[id] = content
	}
	return out
}

func historyPastedContentListMap(contents []PastedContent) map[int]PastedContent {
	out := make(map[int]PastedContent, len(contents))
	for _, content := range contents {
		if content.ID <= 0 {
			continue
		}
		out[content.ID] = content
	}
	if len(out) == 0 && len(contents) > 0 {
		return nil
	}
	return out
}

func historyStoredPastedContentsJSONField(fields map[string]json.RawMessage, names ...string) map[int]StoredPastedContent {
	for _, name := range names {
		raw, ok := fields[name]
		if !ok {
			continue
		}
		var byID map[int]StoredPastedContent
		if err := json.Unmarshal(raw, &byID); err == nil {
			return normalizeHistoryStoredPastedContentIDs(byID)
		}
		var list []StoredPastedContent
		if err := json.Unmarshal(raw, &list); err == nil {
			return historyStoredPastedContentListMap(list)
		}
		var single StoredPastedContent
		if err := json.Unmarshal(raw, &single); err == nil && single.ID > 0 {
			return historyStoredPastedContentListMap([]StoredPastedContent{single})
		}
	}
	return nil
}

func normalizeHistoryStoredPastedContentIDs(contents map[int]StoredPastedContent) map[int]StoredPastedContent {
	out := make(map[int]StoredPastedContent, len(contents))
	for id, content := range contents {
		if content.ID == 0 {
			content.ID = id
		}
		out[id] = content
	}
	return out
}

func historyStoredPastedContentListMap(contents []StoredPastedContent) map[int]StoredPastedContent {
	out := make(map[int]StoredPastedContent, len(contents))
	for _, content := range contents {
		if content.ID <= 0 {
			continue
		}
		out[content.ID] = content
	}
	if len(out) == 0 && len(contents) > 0 {
		return nil
	}
	return out
}

func historyPastedContentContainerFieldNames() []string {
	return []string{
		"pastedContents",
		"pasted_contents",
		"pastedContent",
		"pasted_content",
		"pastes",
		"pasteContents",
		"paste_contents",
		"pasteContent",
		"paste_content",
		"attachments",
		"attachment",
	}
}

func (e *HistoryEntry) UnmarshalJSON(data []byte) error {
	fields := map[string]json.RawMessage{}
	if err := json.Unmarshal(data, &fields); err != nil {
		return err
	}
	*e = HistoryEntry{
		Display:        historyStringJSONField(fields, "display", "prompt", "text", "input", "content", "value"),
		PastedContents: historyPastedContentsJSONField(fields, historyPastedContentContainerFieldNames()...),
	}
	return nil
}

func (e *LogEntry) UnmarshalJSON(data []byte) error {
	fields := map[string]json.RawMessage{}
	if err := json.Unmarshal(data, &fields); err != nil {
		return err
	}
	*e = LogEntry{
		Display:        historyStringJSONField(fields, "display", "prompt", "text", "input", "content", "value"),
		PastedContents: historyStoredPastedContentsJSONField(fields, historyPastedContentContainerFieldNames()...),
		Timestamp:      historyTimestampJSONField(fields, "timestamp", "createdAt", "created_at", "time", "unixTimestamp", "unix_timestamp"),
		Project:        historyStringJSONField(fields, "project", "projectPath", "project_path", "cwd", "cwdPath", "cwd_path", "workingDirectory", "working_directory", "workspacePath", "workspace_path", "workspace"),
		SessionID: firstHistoryID(
			historyIDJSONField(fields, "sessionId"),
			historyIDJSONField(fields, "sessionID"),
			historyIDJSONField(fields, "session_id"),
			historyIDJSONField(fields, "session"),
			historyIDJSONField(fields, "sessionUuid"),
			historyIDJSONField(fields, "sessionUUID"),
			historyIDJSONField(fields, "session_uuid"),
		),
	}
	return nil
}

func firstHistoryID(values ...contracts.ID) contracts.ID {
	for _, value := range values {
		if value != "" {
			return value
		}
	}
	return ""
}
