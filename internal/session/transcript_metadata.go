package session

import (
	"bufio"
	"bytes"
	"encoding/json"
	"os"

	"ccgo/internal/contracts"
)

type TranscriptMetadata struct {
	Summaries               map[contracts.ID]string
	CustomTitles            map[contracts.ID]string
	Tags                    map[contracts.ID]string
	ContentReplacements     map[contracts.ID][]ContentReplacementRecord
	ContextCollapseCommits  []ContextCollapseCommitEntry
	ContextCollapseSnapshot *ContextCollapseSnapshotEntry
}

func NewTranscriptMetadata() TranscriptMetadata {
	return TranscriptMetadata{
		Summaries:           map[contracts.ID]string{},
		CustomTitles:        map[contracts.ID]string{},
		Tags:                map[contracts.ID]string{},
		ContentReplacements: map[contracts.ID][]ContentReplacementRecord{},
	}
}

func LoadTranscriptMetadata(path string) (TranscriptMetadata, error) {
	f, err := os.Open(path)
	if err != nil {
		if os.IsNotExist(err) {
			return NewTranscriptMetadata(), nil
		}
		return TranscriptMetadata{}, err
	}
	defer f.Close()

	metadata := NewTranscriptMetadata()
	scanner := bufio.NewScanner(f)
	buf := make([]byte, 0, 1024*1024)
	scanner.Buffer(buf, 50*1024*1024)
	for scanner.Scan() {
		line := bytes.TrimSpace(scanner.Bytes())
		if len(line) == 0 {
			continue
		}
		var envelope transcriptEnvelope
		if err := json.Unmarshal(line, &envelope); err != nil {
			continue
		}
		switch envelope.Type {
		case "summary":
			var entry struct {
				LeafUUID contracts.ID `json:"leafUuid"`
				Summary  string       `json:"summary"`
			}
			if err := json.Unmarshal(line, &entry); err == nil && entry.LeafUUID != "" {
				metadata.Summaries[entry.LeafUUID] = entry.Summary
			}
		case "custom-title":
			var entry struct {
				SessionID   contracts.ID `json:"sessionId"`
				CustomTitle string       `json:"customTitle"`
			}
			if err := json.Unmarshal(line, &entry); err == nil && entry.SessionID != "" {
				metadata.CustomTitles[entry.SessionID] = entry.CustomTitle
			}
		case "tag":
			var entry struct {
				SessionID contracts.ID `json:"sessionId"`
				Tag       string       `json:"tag"`
			}
			if err := json.Unmarshal(line, &entry); err == nil && entry.SessionID != "" {
				metadata.Tags[entry.SessionID] = entry.Tag
			}
		case "content-replacement":
			var entry ContentReplacementEntry
			if err := json.Unmarshal(line, &entry); err == nil && entry.SessionID != "" {
				metadata.ContentReplacements[entry.SessionID] = append(metadata.ContentReplacements[entry.SessionID], entry.Replacements...)
			}
		case "marble-origami-commit":
			var entry ContextCollapseCommitEntry
			if err := json.Unmarshal(line, &entry); err == nil {
				metadata.ContextCollapseCommits = append(metadata.ContextCollapseCommits, entry)
			}
		case "marble-origami-snapshot":
			var entry ContextCollapseSnapshotEntry
			if err := json.Unmarshal(line, &entry); err == nil {
				metadata.ContextCollapseSnapshot = &entry
			}
		}
	}
	if err := scanner.Err(); err != nil {
		return TranscriptMetadata{}, err
	}
	return metadata, nil
}
