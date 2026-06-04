package session

import (
	"bytes"
	"encoding/json"
	"strconv"
	"strings"

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

func (e *HistoryEntry) UnmarshalJSON(data []byte) error {
	type HistoryEntryJSON HistoryEntry
	var aux struct {
		*HistoryEntryJSON
		PastedContentsSnake map[int]PastedContent `json:"pasted_contents"`
	}
	base := HistoryEntryJSON{}
	aux.HistoryEntryJSON = &base
	if err := json.Unmarshal(data, &aux); err != nil {
		return err
	}
	*e = HistoryEntry(base)
	if e.PastedContents == nil {
		e.PastedContents = aux.PastedContentsSnake
	}
	return nil
}

func (e *LogEntry) UnmarshalJSON(data []byte) error {
	type LogEntryJSON LogEntry
	var aux struct {
		*LogEntryJSON
		PastedContentsSnake map[int]StoredPastedContent `json:"pasted_contents"`
		SessionIDUpper      contracts.ID                `json:"sessionID"`
		SessionIDSnake      contracts.ID                `json:"session_id"`
		Session             contracts.ID                `json:"session"`
		SessionUUID         contracts.ID                `json:"sessionUuid"`
		SessionUUIDUpper    contracts.ID                `json:"sessionUUID"`
		SessionUUIDSnake    contracts.ID                `json:"session_uuid"`
	}
	base := LogEntryJSON{}
	aux.LogEntryJSON = &base
	if err := json.Unmarshal(data, &aux); err != nil {
		return err
	}
	*e = LogEntry(base)
	if e.PastedContents == nil {
		e.PastedContents = aux.PastedContentsSnake
	}
	if e.SessionID == "" {
		e.SessionID = firstHistoryID(aux.SessionIDUpper, aux.SessionIDSnake, aux.Session, aux.SessionUUID, aux.SessionUUIDUpper, aux.SessionUUIDSnake)
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
