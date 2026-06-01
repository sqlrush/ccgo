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
	AITitles                map[contracts.ID]string
	LastPrompts             map[contracts.ID]string
	TaskSummaries           map[contracts.ID]TaskSummaryEntry
	Tags                    map[contracts.ID]string
	AgentNames              map[contracts.ID]string
	AgentColors             map[contracts.ID]string
	AgentSettings           map[contracts.ID]string
	PRLinks                 map[contracts.ID]PRLinkEntry
	Modes                   map[contracts.ID]string
	WorktreeStates          map[contracts.ID]WorktreeStateEntry
	ContentReplacements     map[contracts.ID][]ContentReplacementRecord
	FileHistorySnapshots    []json.RawMessage
	AttributionSnapshots    []json.RawMessage
	SpeculationAccepts      []SpeculationAcceptEntry
	ContextCollapseCommits  []ContextCollapseCommitEntry
	ContextCollapseSnapshot *ContextCollapseSnapshotEntry
}

func NewTranscriptMetadata() TranscriptMetadata {
	return TranscriptMetadata{
		Summaries:           map[contracts.ID]string{},
		CustomTitles:        map[contracts.ID]string{},
		AITitles:            map[contracts.ID]string{},
		LastPrompts:         map[contracts.ID]string{},
		TaskSummaries:       map[contracts.ID]TaskSummaryEntry{},
		Tags:                map[contracts.ID]string{},
		AgentNames:          map[contracts.ID]string{},
		AgentColors:         map[contracts.ID]string{},
		AgentSettings:       map[contracts.ID]string{},
		PRLinks:             map[contracts.ID]PRLinkEntry{},
		Modes:               map[contracts.ID]string{},
		WorktreeStates:      map[contracts.ID]WorktreeStateEntry{},
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
		case "ai-title":
			var entry struct {
				SessionID contracts.ID `json:"sessionId"`
				AITitle   string       `json:"aiTitle"`
			}
			if err := json.Unmarshal(line, &entry); err == nil && entry.SessionID != "" {
				metadata.AITitles[entry.SessionID] = entry.AITitle
			}
		case "last-prompt":
			var entry struct {
				SessionID  contracts.ID `json:"sessionId"`
				LastPrompt string       `json:"lastPrompt"`
			}
			if err := json.Unmarshal(line, &entry); err == nil && entry.SessionID != "" {
				metadata.LastPrompts[entry.SessionID] = entry.LastPrompt
			}
		case "task-summary":
			var entry TaskSummaryEntry
			if err := json.Unmarshal(line, &entry); err == nil && entry.SessionID != "" {
				metadata.TaskSummaries[entry.SessionID] = entry
			}
		case "tag":
			var entry struct {
				SessionID contracts.ID `json:"sessionId"`
				Tag       string       `json:"tag"`
			}
			if err := json.Unmarshal(line, &entry); err == nil && entry.SessionID != "" {
				metadata.Tags[entry.SessionID] = entry.Tag
			}
		case "agent-name":
			var entry struct {
				SessionID contracts.ID `json:"sessionId"`
				AgentName string       `json:"agentName"`
			}
			if err := json.Unmarshal(line, &entry); err == nil && entry.SessionID != "" {
				metadata.AgentNames[entry.SessionID] = entry.AgentName
			}
		case "agent-color":
			var entry struct {
				SessionID  contracts.ID `json:"sessionId"`
				AgentColor string       `json:"agentColor"`
			}
			if err := json.Unmarshal(line, &entry); err == nil && entry.SessionID != "" {
				metadata.AgentColors[entry.SessionID] = entry.AgentColor
			}
		case "agent-setting":
			var entry struct {
				SessionID    contracts.ID `json:"sessionId"`
				AgentSetting string       `json:"agentSetting"`
			}
			if err := json.Unmarshal(line, &entry); err == nil && entry.SessionID != "" {
				metadata.AgentSettings[entry.SessionID] = entry.AgentSetting
			}
		case "pr-link":
			var entry PRLinkEntry
			if err := json.Unmarshal(line, &entry); err == nil && entry.SessionID != "" {
				metadata.PRLinks[entry.SessionID] = entry
			}
		case "mode":
			var entry struct {
				SessionID contracts.ID `json:"sessionId"`
				Mode      string       `json:"mode"`
			}
			if err := json.Unmarshal(line, &entry); err == nil && entry.SessionID != "" {
				metadata.Modes[entry.SessionID] = entry.Mode
			}
		case "worktree-state":
			var entry WorktreeStateEntry
			if err := json.Unmarshal(line, &entry); err == nil && entry.SessionID != "" {
				metadata.WorktreeStates[entry.SessionID] = entry
			}
		case "content-replacement":
			var entry ContentReplacementEntry
			if err := json.Unmarshal(line, &entry); err == nil && entry.SessionID != "" {
				key := entry.SessionID
				if entry.AgentID != "" {
					key = contracts.ID(entry.AgentID)
				}
				metadata.ContentReplacements[key] = append(metadata.ContentReplacements[key], entry.Replacements...)
			}
		case "file-history-snapshot":
			metadata.FileHistorySnapshots = append(metadata.FileHistorySnapshots, append(json.RawMessage(nil), line...))
		case "attribution-snapshot":
			metadata.AttributionSnapshots = append(metadata.AttributionSnapshots, append(json.RawMessage(nil), line...))
		case "speculation-accept":
			var entry SpeculationAcceptEntry
			if err := json.Unmarshal(line, &entry); err == nil {
				metadata.SpeculationAccepts = append(metadata.SpeculationAccepts, entry)
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
