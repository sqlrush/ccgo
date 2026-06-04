package session

import (
	"encoding/json"

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
	return nil
}

func (c *PastedContent) UnmarshalJSON(data []byte) error {
	type PastedContentJSON PastedContent
	var aux struct {
		*PastedContentJSON
		Kind             string `json:"kind"`
		PastedType       string `json:"pastedType"`
		PastedTypeSnake  string `json:"pasted_type"`
		Value            string `json:"value"`
		Data             string `json:"data"`
		Base64           string `json:"base64"`
		MediaTypeSnake   string `json:"media_type"`
		MimeType         string `json:"mimeType"`
		MimeTypeSnake    string `json:"mime_type"`
		ContentType      string `json:"contentType"`
		ContentTypeSnake string `json:"content_type"`
		FileName         string `json:"fileName"`
		FileNameSnake    string `json:"file_name"`
		Name             string `json:"name"`
		SourcePathSnake  string `json:"source_path"`
		FilePath         string `json:"filePath"`
		FilePathSnake    string `json:"file_path"`
		Path             string `json:"path"`
	}
	base := PastedContentJSON{}
	aux.PastedContentJSON = &base
	if err := json.Unmarshal(data, &aux); err != nil {
		return err
	}
	*c = PastedContent(base)
	if c.Type == "" {
		c.Type = firstHistoryString(aux.Kind, aux.PastedType, aux.PastedTypeSnake)
	}
	if c.Content == "" {
		c.Content = firstHistoryString(aux.Value, aux.Data, aux.Base64)
	}
	if c.MediaType == "" {
		c.MediaType = firstHistoryString(aux.MediaTypeSnake, aux.MimeType, aux.MimeTypeSnake, aux.ContentType, aux.ContentTypeSnake)
	}
	if c.Filename == "" {
		c.Filename = firstHistoryString(aux.FileName, aux.FileNameSnake, aux.Name)
	}
	if c.SourcePath == "" {
		c.SourcePath = firstHistoryString(aux.SourcePathSnake, aux.FilePath, aux.FilePathSnake, aux.Path)
	}
	return nil
}

func (c *StoredPastedContent) UnmarshalJSON(data []byte) error {
	type StoredPastedContentJSON StoredPastedContent
	var aux struct {
		*StoredPastedContentJSON
		ContentHashSnake string `json:"content_hash"`
		MediaTypeSnake   string `json:"media_type"`
		MimeType         string `json:"mimeType"`
		MimeTypeSnake    string `json:"mime_type"`
		ContentType      string `json:"contentType"`
		ContentTypeSnake string `json:"content_type"`
		FileName         string `json:"fileName"`
		FileNameSnake    string `json:"file_name"`
		Name             string `json:"name"`
		SourcePathSnake  string `json:"source_path"`
		FilePath         string `json:"filePath"`
		FilePathSnake    string `json:"file_path"`
		Path             string `json:"path"`
	}
	base := StoredPastedContentJSON{}
	aux.StoredPastedContentJSON = &base
	if err := json.Unmarshal(data, &aux); err != nil {
		return err
	}
	*c = StoredPastedContent(base)
	if c.ContentHash == "" {
		c.ContentHash = aux.ContentHashSnake
	}
	if c.MediaType == "" {
		c.MediaType = firstHistoryString(aux.MediaTypeSnake, aux.MimeType, aux.MimeTypeSnake, aux.ContentType, aux.ContentTypeSnake)
	}
	if c.Filename == "" {
		c.Filename = firstHistoryString(aux.FileName, aux.FileNameSnake, aux.Name)
	}
	if c.SourcePath == "" {
		c.SourcePath = firstHistoryString(aux.SourcePathSnake, aux.FilePath, aux.FilePathSnake, aux.Path)
	}
	return nil
}

func firstHistoryString(values ...string) string {
	for _, value := range values {
		if value != "" {
			return value
		}
	}
	return ""
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
