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
		MediaTypeSnake  string `json:"media_type"`
		SourcePathSnake string `json:"source_path"`
	}
	base := PastedContentJSON{}
	aux.PastedContentJSON = &base
	if err := json.Unmarshal(data, &aux); err != nil {
		return err
	}
	*c = PastedContent(base)
	if c.MediaType == "" {
		c.MediaType = aux.MediaTypeSnake
	}
	if c.SourcePath == "" {
		c.SourcePath = aux.SourcePathSnake
	}
	return nil
}

func (c *StoredPastedContent) UnmarshalJSON(data []byte) error {
	type StoredPastedContentJSON StoredPastedContent
	var aux struct {
		*StoredPastedContentJSON
		ContentHashSnake string `json:"content_hash"`
		MediaTypeSnake   string `json:"media_type"`
		SourcePathSnake  string `json:"source_path"`
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
		c.MediaType = aux.MediaTypeSnake
	}
	if c.SourcePath == "" {
		c.SourcePath = aux.SourcePathSnake
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
		SessionIDSnake      contracts.ID                `json:"session_id"`
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
		e.SessionID = aux.SessionIDSnake
	}
	return nil
}
