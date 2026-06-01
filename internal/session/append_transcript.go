package session

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"time"

	"ccgo/internal/contracts"
	"ccgo/internal/platform"
)

func AppendTranscriptMessage(path string, message TranscriptMessage) error {
	if message.Timestamp == "" {
		message.Timestamp = time.Now().UTC().Format(time.RFC3339Nano)
	}
	if err := platform.EnsureDir(filepath.Dir(path)); err != nil {
		return err
	}
	f, err := os.OpenFile(path, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o600)
	if err != nil {
		return err
	}
	defer f.Close()
	encoded, err := json.Marshal(message)
	if err != nil {
		return err
	}
	_, err = f.Write(append(encoded, '\n'))
	return err
}

type ReappendSessionMetadataResult struct {
	Written int
}

func ReappendSessionMetadata(path string, sessionID contracts.ID) (ReappendSessionMetadataResult, error) {
	if sessionID == "" {
		return ReappendSessionMetadataResult{}, nil
	}
	metadata, err := LoadTranscriptMetadata(path)
	if err != nil {
		return ReappendSessionMetadataResult{}, err
	}
	entries := sessionMetadataEntries(metadata, sessionID)
	if len(entries) == 0 {
		return ReappendSessionMetadataResult{}, nil
	}
	if err := platform.EnsureDir(filepath.Dir(path)); err != nil {
		return ReappendSessionMetadataResult{}, err
	}
	f, err := os.OpenFile(path, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o600)
	if err != nil {
		return ReappendSessionMetadataResult{}, err
	}
	defer f.Close()
	for _, entry := range entries {
		encoded, err := json.Marshal(entry)
		if err != nil {
			return ReappendSessionMetadataResult{}, err
		}
		if _, err := f.Write(append(encoded, '\n')); err != nil {
			return ReappendSessionMetadataResult{}, err
		}
	}
	return ReappendSessionMetadataResult{Written: len(entries)}, nil
}

func sessionMetadataEntries(metadata TranscriptMetadata, sessionID contracts.ID) []any {
	entries := []any{}
	if title := strings.TrimSpace(metadata.CustomTitles[sessionID]); title != "" {
		entries = append(entries, struct {
			Type        string       `json:"type"`
			SessionID   contracts.ID `json:"sessionId"`
			CustomTitle string       `json:"customTitle"`
		}{Type: "custom-title", SessionID: sessionID, CustomTitle: metadata.CustomTitles[sessionID]})
	}
	if title := strings.TrimSpace(metadata.AITitles[sessionID]); title != "" {
		entries = append(entries, struct {
			Type      string       `json:"type"`
			SessionID contracts.ID `json:"sessionId"`
			AITitle   string       `json:"aiTitle"`
		}{Type: "ai-title", SessionID: sessionID, AITitle: metadata.AITitles[sessionID]})
	}
	if prompt := strings.TrimSpace(metadata.LastPrompts[sessionID]); prompt != "" {
		entries = append(entries, struct {
			Type       string       `json:"type"`
			SessionID  contracts.ID `json:"sessionId"`
			LastPrompt string       `json:"lastPrompt"`
		}{Type: "last-prompt", SessionID: sessionID, LastPrompt: metadata.LastPrompts[sessionID]})
	}
	if task, ok := metadata.TaskSummaries[sessionID]; ok && strings.TrimSpace(task.Summary) != "" {
		task.Type = "task-summary"
		task.SessionID = sessionID
		entries = append(entries, task)
	}
	if tag, ok := metadata.Tags[sessionID]; ok {
		entries = append(entries, struct {
			Type      string       `json:"type"`
			SessionID contracts.ID `json:"sessionId"`
			Tag       string       `json:"tag"`
		}{Type: "tag", SessionID: sessionID, Tag: tag})
	}
	if agentName := strings.TrimSpace(metadata.AgentNames[sessionID]); agentName != "" {
		entries = append(entries, struct {
			Type      string       `json:"type"`
			SessionID contracts.ID `json:"sessionId"`
			AgentName string       `json:"agentName"`
		}{Type: "agent-name", SessionID: sessionID, AgentName: metadata.AgentNames[sessionID]})
	}
	if agentColor := strings.TrimSpace(metadata.AgentColors[sessionID]); agentColor != "" {
		entries = append(entries, struct {
			Type       string       `json:"type"`
			SessionID  contracts.ID `json:"sessionId"`
			AgentColor string       `json:"agentColor"`
		}{Type: "agent-color", SessionID: sessionID, AgentColor: metadata.AgentColors[sessionID]})
	}
	if agentSetting := strings.TrimSpace(metadata.AgentSettings[sessionID]); agentSetting != "" {
		entries = append(entries, struct {
			Type         string       `json:"type"`
			SessionID    contracts.ID `json:"sessionId"`
			AgentSetting string       `json:"agentSetting"`
		}{Type: "agent-setting", SessionID: sessionID, AgentSetting: metadata.AgentSettings[sessionID]})
	}
	if mode := strings.TrimSpace(metadata.Modes[sessionID]); mode != "" {
		entries = append(entries, struct {
			Type      string       `json:"type"`
			SessionID contracts.ID `json:"sessionId"`
			Mode      string       `json:"mode"`
		}{Type: "mode", SessionID: sessionID, Mode: metadata.Modes[sessionID]})
	}
	if worktree, ok := metadata.WorktreeStates[sessionID]; ok {
		worktree.Type = "worktree-state"
		worktree.SessionID = sessionID
		entries = append(entries, worktree)
	}
	if pr, ok := metadata.PRLinks[sessionID]; ok {
		pr.Type = "pr-link"
		pr.SessionID = sessionID
		entries = append(entries, pr)
	}
	return entries
}
