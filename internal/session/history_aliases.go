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
	if nested, ok := historyWrappedPayloadJSON(fields, historyPastedContentWrapperFieldNames(), historyPastedContentValueFieldNames(), nil); ok {
		var content PastedContent
		if err := json.Unmarshal(nested, &content); err != nil {
			return err
		}
		historyApplyPastedContentFields(&content, fields, false)
		*c = content
		return nil
	}
	var content PastedContent
	historyApplyPastedContentFields(&content, fields, true)
	*c = content
	return nil
}

func (c *StoredPastedContent) UnmarshalJSON(data []byte) error {
	fields := map[string]json.RawMessage{}
	if err := json.Unmarshal(data, &fields); err != nil {
		return err
	}
	if nested, ok := historyWrappedPayloadJSON(fields, historyPastedContentWrapperFieldNames(), historyPastedContentValueFieldNames(), nil); ok {
		var content StoredPastedContent
		if err := json.Unmarshal(nested, &content); err != nil {
			return err
		}
		historyApplyStoredPastedContentFields(&content, fields, false)
		*c = content
		return nil
	}
	var content StoredPastedContent
	historyApplyStoredPastedContentFields(&content, fields, true)
	*c = content
	return nil
}

func historyApplyPastedContentFields(content *PastedContent, fields map[string]json.RawMessage, overwrite bool) {
	if value := historyIntJSONField(fields, historyIDFieldNames()...); value > 0 && (overwrite || content.ID == 0) {
		content.ID = value
	}
	if value := canonicalPastedContentType(historyStringJSONField(fields, "type", "kind", "pastedType", "pasted_type")); value != "" && (overwrite || content.Type == "") {
		content.Type = value
	}
	if value := historyStringJSONField(fields, historyPastedContentContentFieldNames()...); value != "" && (overwrite || content.Content == "") {
		content.Content = value
	}
	if value := historyStringJSONField(fields, "mediaType", "media_type", "mimeType", "mime_type", "contentType", "content_type"); value != "" && (overwrite || content.MediaType == "") {
		content.MediaType = value
	}
	if value := historyStringJSONField(fields, "filename", "fileName", "file_name", "name"); value != "" && (overwrite || content.Filename == "") {
		content.Filename = value
	}
	if value := historyDimensionsJSONField(fields, "dimensions", "imageDimensions", "image_dimensions"); value != nil && (overwrite || content.Dimensions == nil) {
		content.Dimensions = value
	}
	if value := historyStringJSONField(fields, "sourcePath", "source_path", "filePath", "file_path", "path"); value != "" && (overwrite || content.SourcePath == "") {
		content.SourcePath = value
	}
}

func historyApplyStoredPastedContentFields(content *StoredPastedContent, fields map[string]json.RawMessage, overwrite bool) {
	if value := historyIntJSONField(fields, historyIDFieldNames()...); value > 0 && (overwrite || content.ID == 0) {
		content.ID = value
	}
	if value := canonicalPastedContentType(historyStringJSONField(fields, "type", "kind", "pastedType", "pasted_type")); value != "" && (overwrite || content.Type == "") {
		content.Type = value
	}
	if value := historyStringJSONField(fields, historyPastedContentContentFieldNames()...); value != "" && (overwrite || content.Content == "") {
		content.Content = value
	}
	if value := historyStringJSONField(fields, historyPastedContentHashFieldNames()...); value != "" && (overwrite || content.ContentHash == "") {
		content.ContentHash = value
	}
	if value := historyStringJSONField(fields, "mediaType", "media_type", "mimeType", "mime_type", "contentType", "content_type"); value != "" && (overwrite || content.MediaType == "") {
		content.MediaType = value
	}
	if value := historyStringJSONField(fields, "filename", "fileName", "file_name", "name"); value != "" && (overwrite || content.Filename == "") {
		content.Filename = value
	}
	if value := historyDimensionsJSONField(fields, "dimensions", "imageDimensions", "image_dimensions"); value != nil && (overwrite || content.Dimensions == nil) {
		content.Dimensions = value
	}
	if value := historyStringJSONField(fields, "sourcePath", "source_path", "filePath", "file_path", "path"); value != "" && (overwrite || content.SourcePath == "") {
		content.SourcePath = value
	}
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
	if nested, ok := historyWrappedPayloadJSON(fields, historyEntryWrapperFieldNames(), historyEntryValueFieldNames(), historyPastedContentContainerFieldNames()); ok {
		var entry HistoryEntry
		if err := json.Unmarshal(nested, &entry); err != nil {
			return err
		}
		historyApplyEntryFields(&entry, fields, false)
		*e = entry
		return nil
	}
	var entry HistoryEntry
	historyApplyEntryFields(&entry, fields, true)
	*e = entry
	return nil
}

func (e *LogEntry) UnmarshalJSON(data []byte) error {
	fields := map[string]json.RawMessage{}
	if err := json.Unmarshal(data, &fields); err != nil {
		return err
	}
	if nested, ok := historyWrappedPayloadJSON(fields, historyEntryWrapperFieldNames(), historyEntryValueFieldNames(), historyPastedContentContainerFieldNames()); ok {
		var entry LogEntry
		if err := json.Unmarshal(nested, &entry); err != nil {
			return err
		}
		historyApplyLogEntryFields(&entry, fields, false)
		*e = entry
		return nil
	}
	var entry LogEntry
	historyApplyLogEntryFields(&entry, fields, true)
	*e = entry
	return nil
}

func historyApplyEntryFields(entry *HistoryEntry, fields map[string]json.RawMessage, overwrite bool) {
	if value := historyStringJSONField(fields, "display", "prompt", "text", "input", "content", "value"); value != "" && (overwrite || entry.Display == "") {
		entry.Display = value
	}
	if value := historyPastedContentsJSONField(fields, historyPastedContentContainerFieldNames()...); value != nil && (overwrite || len(entry.PastedContents) == 0) {
		entry.PastedContents = value
	}
}

func historyApplyLogEntryFields(entry *LogEntry, fields map[string]json.RawMessage, overwrite bool) {
	if value := historyStringJSONField(fields, "display", "prompt", "text", "input", "content", "value"); value != "" && (overwrite || entry.Display == "") {
		entry.Display = value
	}
	if value := historyStoredPastedContentsJSONField(fields, historyPastedContentContainerFieldNames()...); value != nil && (overwrite || len(entry.PastedContents) == 0) {
		entry.PastedContents = value
	}
	if value := historyTimestampJSONField(fields, "timestamp", "createdAt", "created_at", "time", "unixTimestamp", "unix_timestamp"); value != 0 && (overwrite || entry.Timestamp == 0) {
		entry.Timestamp = value
	}
	if value := historyStringJSONField(fields, "project", "projectPath", "project_path", "cwd", "cwdPath", "cwd_path", "workingDirectory", "working_directory", "workspacePath", "workspace_path", "workspace"); value != "" && (overwrite || entry.Project == "") {
		entry.Project = value
	}
	if value := firstHistoryID(
		historyIDJSONField(fields, "sessionId"),
		historyIDJSONField(fields, "sessionID"),
		historyIDJSONField(fields, "session_id"),
		historyIDJSONField(fields, "session"),
		historyIDJSONField(fields, "sessionUuid"),
		historyIDJSONField(fields, "sessionUUID"),
		historyIDJSONField(fields, "session_uuid"),
	); value != "" && (overwrite || entry.SessionID == "") {
		entry.SessionID = value
	}
}

func historyEntryWrapperFieldNames() []string {
	return []string{
		"entry", "record", "item", "edge", "node", "resource", "attributes", "properties", "attrs",
		"payload", "body", "data", "result", "value",
		"historyEntry", "history_entry", "logEntry", "log_entry",
	}
}

func historyEntryValueFieldNames() []string {
	return []string{"display", "prompt", "text", "input", "content", "value"}
}

func historyPastedContentWrapperFieldNames() []string {
	return []string{
		"pastedContent", "pasted_content", "storedPastedContent", "stored_pasted_content", "attachment", "paste",
		"entry", "record", "item", "edge", "node", "resource", "attributes", "properties", "attrs",
		"payload", "body", "data", "result", "value",
	}
}

func historyPastedContentValueFieldNames() []string {
	return historyPastedContentContentFieldNames()
}

func historyPastedContentContentFieldNames() []string {
	return []string{
		"content",
		"value",
		"data",
		"base64",
		"text",
		"body",
		"message",
		"input",
		"raw",
		"payloadText",
		"payload_text",
		"base64Data",
		"base64_data",
		"encodedContent",
		"encoded_content",
	}
}

func historyPastedContentHashFieldNames() []string {
	return []string{
		"contentHash",
		"content_hash",
		"hash",
		"contentDigest",
		"content_digest",
		"digest",
		"checksum",
		"sha256",
		"contentSHA256",
		"content_sha256",
		"contentChecksum",
		"content_checksum",
	}
}

func historyWrappedPayloadJSON(fields map[string]json.RawMessage, wrappers []string, scalarDirect []string, containerDirect []string) (json.RawMessage, bool) {
	if historyHasScalarPayload(fields, scalarDirect) || historyHasContainerPayload(fields, containerDirect) {
		return nil, false
	}
	for _, name := range wrappers {
		raw, ok := fields[name]
		if !ok {
			continue
		}
		trimmed := bytes.TrimSpace(raw)
		if len(trimmed) > 0 && trimmed[0] == '{' {
			return raw, true
		}
	}
	return nil, false
}

func historyHasScalarPayload(fields map[string]json.RawMessage, names []string) bool {
	for _, name := range names {
		raw, ok := fields[name]
		if !ok {
			continue
		}
		trimmed := bytes.TrimSpace(raw)
		if len(trimmed) == 0 || bytes.Equal(trimmed, []byte("null")) {
			continue
		}
		if trimmed[0] != '{' {
			return true
		}
	}
	return false
}

func historyHasContainerPayload(fields map[string]json.RawMessage, names []string) bool {
	for _, name := range names {
		raw, ok := fields[name]
		if !ok {
			continue
		}
		trimmed := bytes.TrimSpace(raw)
		if len(trimmed) > 0 && !bytes.Equal(trimmed, []byte("null")) {
			return true
		}
	}
	return false
}

func firstHistoryID(values ...contracts.ID) contracts.ID {
	for _, value := range values {
		if value != "" {
			return value
		}
	}
	return ""
}
