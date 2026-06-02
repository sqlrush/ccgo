package session

import (
	"encoding/json"

	"ccgo/internal/contracts"
)

func (c *PastedContent) UnmarshalJSON(data []byte) error {
	type PastedContentJSON PastedContent
	var aux struct {
		*PastedContentJSON
		MediaTypeSnake string `json:"media_type"`
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
	return nil
}

func (c *StoredPastedContent) UnmarshalJSON(data []byte) error {
	type StoredPastedContentJSON StoredPastedContent
	var aux struct {
		*StoredPastedContentJSON
		ContentHashSnake string `json:"content_hash"`
		MediaTypeSnake   string `json:"media_type"`
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
