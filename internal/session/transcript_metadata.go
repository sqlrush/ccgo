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
	Tombstones              map[contracts.ID]TombstoneEntry
	FileHistorySnapshots    []json.RawMessage
	AttributionSnapshots    []json.RawMessage
	FileHistoryByMessageID  map[contracts.ID]json.RawMessage
	AttributionByMessageID  map[contracts.ID]json.RawMessage
	SpeculationAccepts      []SpeculationAcceptEntry
	ContextCollapseCommits  []ContextCollapseCommitEntry
	ContextCollapseSnapshot *ContextCollapseSnapshotEntry
}

func NewTranscriptMetadata() TranscriptMetadata {
	return TranscriptMetadata{
		Summaries:              map[contracts.ID]string{},
		CustomTitles:           map[contracts.ID]string{},
		AITitles:               map[contracts.ID]string{},
		LastPrompts:            map[contracts.ID]string{},
		TaskSummaries:          map[contracts.ID]TaskSummaryEntry{},
		Tags:                   map[contracts.ID]string{},
		AgentNames:             map[contracts.ID]string{},
		AgentColors:            map[contracts.ID]string{},
		AgentSettings:          map[contracts.ID]string{},
		PRLinks:                map[contracts.ID]PRLinkEntry{},
		Modes:                  map[contracts.ID]string{},
		WorktreeStates:         map[contracts.ID]WorktreeStateEntry{},
		ContentReplacements:    map[contracts.ID][]ContentReplacementRecord{},
		Tombstones:             map[contracts.ID]TombstoneEntry{},
		FileHistoryByMessageID: map[contracts.ID]json.RawMessage{},
		AttributionByMessageID: map[contracts.ID]json.RawMessage{},
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
		physicalLine := bytes.TrimSpace(scanner.Bytes())
		if len(physicalLine) == 0 {
			continue
		}
		for _, line := range transcriptRecordLines(physicalLine) {
			var envelope transcriptEnvelope
			if err := json.Unmarshal(line, &envelope); err != nil {
				continue
			}
			if isTranscriptType(envelope.Type) {
				if contracts.CanonicalMessageType(envelope.Type) == contracts.MessageSystem {
					var msg TranscriptMessage
					if err := json.Unmarshal(line, &msg); err == nil && msg.IsCompactBoundary() {
						metadata.ContextCollapseCommits = nil
						metadata.ContextCollapseSnapshot = nil
					}
				}
				continue
			}
			switch normalizeTranscriptMetadataType(envelope.Type) {
			case "summary":
				if leafUUID, summary, ok := parseSummaryMetadata(line); ok && leafUUID != "" {
					metadata.Summaries[leafUUID] = summary
				}
			case "custom-title":
				if sessionID, title, ok := parseSessionStringMetadata(line, "customTitle", "custom_title", "title", "name"); ok && sessionID != "" {
					metadata.CustomTitles[sessionID] = title
				}
			case "ai-title":
				if sessionID, title, ok := parseSessionStringMetadata(line, "aiTitle", "ai_title", "title", "name"); ok && sessionID != "" {
					metadata.AITitles[sessionID] = title
				}
			case "last-prompt":
				if sessionID, prompt, ok := parseSessionStringMetadata(line, "lastPrompt", "last_prompt", "prompt", "input", "text", "content", "message"); ok && sessionID != "" {
					metadata.LastPrompts[sessionID] = prompt
				}
			case "task-summary":
				if entry, ok := parseTaskSummaryMetadata(line); ok && entry.SessionID != "" {
					metadata.TaskSummaries[entry.SessionID] = entry
				}
			case "tag":
				if sessionID, tag, ok := parseSessionStringMetadata(line, "tag", "value", "name", "label"); ok && sessionID != "" {
					metadata.Tags[sessionID] = tag
				}
			case "agent-name":
				if sessionID, name, ok := parseSessionStringMetadata(line, "agentName", "agent_name", "name", "agent", "title"); ok && sessionID != "" {
					metadata.AgentNames[sessionID] = name
				}
			case "agent-color":
				if sessionID, color, ok := parseSessionStringMetadata(line, "agentColor", "agent_color", "color", "colour", "value"); ok && sessionID != "" {
					metadata.AgentColors[sessionID] = color
				}
			case "agent-setting":
				if sessionID, setting, ok := parseSessionStringMetadata(line, "agentSetting", "agent_setting", "setting", "value", "mode"); ok && sessionID != "" {
					metadata.AgentSettings[sessionID] = setting
				}
			case "pr-link":
				if entry, ok := parsePRLinkMetadata(line); ok && entry.SessionID != "" {
					metadata.PRLinks[entry.SessionID] = entry
				}
			case "mode":
				if sessionID, mode, ok := parseSessionStringMetadata(line, "mode", "value", "name", "status"); ok && sessionID != "" {
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
			case "tombstone":
				if entry, ok := parseTombstoneMetadata(line); ok && entry.TargetUUID != "" {
					metadata.Tombstones[entry.TargetUUID] = entry
				}
			case "file-history-snapshot":
				snapshot := append(json.RawMessage(nil), line...)
				metadata.FileHistorySnapshots = append(metadata.FileHistorySnapshots, snapshot)
				if messageID := parseSnapshotMessageID(line); messageID != "" {
					metadata.FileHistoryByMessageID[messageID] = snapshot
				}
			case "attribution-snapshot":
				snapshot := append(json.RawMessage(nil), line...)
				metadata.AttributionSnapshots = append(metadata.AttributionSnapshots, snapshot)
				if messageID := parseSnapshotMessageID(line); messageID != "" {
					metadata.AttributionByMessageID[messageID] = snapshot
				}
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
	}
	if err := scanner.Err(); err != nil {
		return TranscriptMetadata{}, err
	}
	return metadata, nil
}
