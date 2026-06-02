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
		switch normalizeTranscriptMetadataType(envelope.Type) {
		case "summary":
			if leafUUID, summary, ok := parseSummaryMetadata(line); ok && leafUUID != "" {
				metadata.Summaries[leafUUID] = summary
			}
		case "custom-title":
			if sessionID, title, ok := parseSessionStringMetadata(line, "customTitle", "custom_title"); ok && sessionID != "" {
				metadata.CustomTitles[sessionID] = title
			}
		case "ai-title":
			if sessionID, title, ok := parseSessionStringMetadata(line, "aiTitle", "ai_title"); ok && sessionID != "" {
				metadata.AITitles[sessionID] = title
			}
		case "last-prompt":
			if sessionID, prompt, ok := parseSessionStringMetadata(line, "lastPrompt", "last_prompt"); ok && sessionID != "" {
				metadata.LastPrompts[sessionID] = prompt
			}
		case "task-summary":
			if entry, ok := parseTaskSummaryMetadata(line); ok && entry.SessionID != "" {
				metadata.TaskSummaries[entry.SessionID] = entry
			}
		case "tag":
			if sessionID, tag, ok := parseSessionStringMetadata(line, "tag"); ok && sessionID != "" {
				metadata.Tags[sessionID] = tag
			}
		case "agent-name":
			if sessionID, name, ok := parseSessionStringMetadata(line, "agentName", "agent_name"); ok && sessionID != "" {
				metadata.AgentNames[sessionID] = name
			}
		case "agent-color":
			if sessionID, color, ok := parseSessionStringMetadata(line, "agentColor", "agent_color"); ok && sessionID != "" {
				metadata.AgentColors[sessionID] = color
			}
		case "agent-setting":
			if sessionID, setting, ok := parseSessionStringMetadata(line, "agentSetting", "agent_setting"); ok && sessionID != "" {
				metadata.AgentSettings[sessionID] = setting
			}
		case "pr-link":
			if entry, ok := parsePRLinkMetadata(line); ok && entry.SessionID != "" {
				metadata.PRLinks[entry.SessionID] = entry
			}
		case "mode":
			if sessionID, mode, ok := parseSessionStringMetadata(line, "mode"); ok && sessionID != "" {
				metadata.Modes[sessionID] = mode
			}
		case "worktree-state":
			if entry, ok := parseWorktreeStateMetadata(line); ok && entry.SessionID != "" {
				metadata.WorktreeStates[entry.SessionID] = entry
			}
		case "content-replacement":
			if entry, ok := parseContentReplacementMetadata(line); ok && entry.SessionID != "" {
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
			if entry, ok := parseSpeculationAcceptMetadata(line); ok {
				metadata.SpeculationAccepts = append(metadata.SpeculationAccepts, entry)
			}
		case "marble-origami-commit":
			if entry, ok := parseContextCollapseCommitMetadata(line); ok {
				metadata.ContextCollapseCommits = append(metadata.ContextCollapseCommits, entry)
			}
		case "marble-origami-snapshot":
			if entry, ok := parseContextCollapseSnapshotMetadata(line); ok {
				metadata.ContextCollapseSnapshot = &entry
			}
		}
	}
	if err := scanner.Err(); err != nil {
		return TranscriptMetadata{}, err
	}
	return metadata, nil
}
