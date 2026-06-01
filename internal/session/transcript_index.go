package session

import (
	"bufio"
	"bytes"
	"encoding/json"
	"os"
	"strings"

	"ccgo/internal/contracts"
)

type TranscriptIndex struct {
	SessionID               contracts.ID
	MessageCount            int
	UserMessageCount        int
	AssistantMessageCount   int
	SystemMessageCount      int
	FirstUUID               contracts.ID
	LastUUID                contracts.ID
	FirstTimestamp          string
	LastTimestamp           string
	FirstUserText           string
	LastUserText            string
	LastAssistantText       string
	AITitle                 string
	LastPrompt              string
	TaskSummary             string
	Tag                     string
	AgentName               string
	AgentColor              string
	AgentSetting            string
	PRNumber                int
	PRURL                   string
	PRRepository            string
	Mode                    string
	HasWorktreeState        bool
	TextBytes               int
	Title                   string
	SummaryCount            int
	ContentReplacementCount int
}

func LoadTranscriptIndex(path string, sessionID contracts.ID) (TranscriptIndex, error) {
	f, err := os.Open(path)
	if err != nil {
		if os.IsNotExist(err) {
			return TranscriptIndex{SessionID: sessionID, Title: "Untitled session"}, nil
		}
		return TranscriptIndex{}, err
	}
	defer f.Close()

	index := TranscriptIndex{SessionID: sessionID}
	customTitle := ""
	firstSummary := ""
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
		if isTranscriptType(envelope.Type) {
			var msg TranscriptMessage
			if err := json.Unmarshal(line, &msg); err != nil || msg.UUID == "" {
				continue
			}
			index.addMessage(msg)
			continue
		}
		switch envelope.Type {
		case "summary":
			var entry struct {
				Summary string `json:"summary"`
			}
			if err := json.Unmarshal(line, &entry); err == nil {
				index.SummaryCount++
				if firstSummary == "" && strings.TrimSpace(entry.Summary) != "" {
					firstSummary = entry.Summary
				}
			}
		case "custom-title":
			var entry struct {
				SessionID   contracts.ID `json:"sessionId"`
				CustomTitle string       `json:"customTitle"`
			}
			if err := json.Unmarshal(line, &entry); err == nil && strings.TrimSpace(entry.CustomTitle) != "" {
				if sessionID == "" || entry.SessionID == sessionID {
					customTitle = entry.CustomTitle
				}
			}
		case "ai-title":
			var entry struct {
				SessionID contracts.ID `json:"sessionId"`
				AITitle   string       `json:"aiTitle"`
			}
			if err := json.Unmarshal(line, &entry); err == nil && (sessionID == "" || entry.SessionID == sessionID) {
				index.AITitle = entry.AITitle
			}
		case "last-prompt":
			var entry struct {
				SessionID  contracts.ID `json:"sessionId"`
				LastPrompt string       `json:"lastPrompt"`
			}
			if err := json.Unmarshal(line, &entry); err == nil && (sessionID == "" || entry.SessionID == sessionID) {
				index.LastPrompt = entry.LastPrompt
			}
		case "task-summary":
			var entry TaskSummaryEntry
			if err := json.Unmarshal(line, &entry); err == nil && (sessionID == "" || entry.SessionID == sessionID) {
				index.TaskSummary = entry.Summary
			}
		case "tag":
			var entry struct {
				SessionID contracts.ID `json:"sessionId"`
				Tag       string       `json:"tag"`
			}
			if err := json.Unmarshal(line, &entry); err == nil && (sessionID == "" || entry.SessionID == sessionID) {
				index.Tag = entry.Tag
			}
		case "agent-name":
			var entry struct {
				SessionID contracts.ID `json:"sessionId"`
				AgentName string       `json:"agentName"`
			}
			if err := json.Unmarshal(line, &entry); err == nil && (sessionID == "" || entry.SessionID == sessionID) {
				index.AgentName = entry.AgentName
			}
		case "agent-color":
			var entry struct {
				SessionID  contracts.ID `json:"sessionId"`
				AgentColor string       `json:"agentColor"`
			}
			if err := json.Unmarshal(line, &entry); err == nil && (sessionID == "" || entry.SessionID == sessionID) {
				index.AgentColor = entry.AgentColor
			}
		case "agent-setting":
			var entry struct {
				SessionID    contracts.ID `json:"sessionId"`
				AgentSetting string       `json:"agentSetting"`
			}
			if err := json.Unmarshal(line, &entry); err == nil && (sessionID == "" || entry.SessionID == sessionID) {
				index.AgentSetting = entry.AgentSetting
			}
		case "pr-link":
			var entry PRLinkEntry
			if err := json.Unmarshal(line, &entry); err == nil && (sessionID == "" || entry.SessionID == sessionID) {
				index.PRNumber = entry.PRNumber
				index.PRURL = entry.PRURL
				index.PRRepository = entry.PRRepository
			}
		case "mode":
			var entry struct {
				SessionID contracts.ID `json:"sessionId"`
				Mode      string       `json:"mode"`
			}
			if err := json.Unmarshal(line, &entry); err == nil && (sessionID == "" || entry.SessionID == sessionID) {
				index.Mode = entry.Mode
			}
		case "worktree-state":
			var entry WorktreeStateEntry
			if err := json.Unmarshal(line, &entry); err == nil && (sessionID == "" || entry.SessionID == sessionID) {
				index.HasWorktreeState = len(entry.WorktreeSession) > 0 && string(entry.WorktreeSession) != "null"
			}
		case "content-replacement":
			var entry ContentReplacementEntry
			if err := json.Unmarshal(line, &entry); err == nil {
				index.ContentReplacementCount += len(entry.Replacements)
			}
		}
	}
	if err := scanner.Err(); err != nil {
		return TranscriptIndex{}, err
	}
	index.Title = titleFromIndex(customTitle, index.AITitle, index.FirstUserText, index.LastPrompt, firstSummary)
	return index, nil
}

func (i *TranscriptIndex) addMessage(msg TranscriptMessage) {
	i.MessageCount++
	if i.SessionID == "" && msg.SessionID != "" {
		i.SessionID = msg.SessionID
	}
	if i.FirstUUID == "" {
		i.FirstUUID = msg.UUID
		i.FirstTimestamp = msg.Timestamp
	}
	i.LastUUID = msg.UUID
	i.LastTimestamp = msg.Timestamp
	switch msg.Type {
	case "user":
		i.UserMessageCount++
		text := strings.TrimSpace(textFromTranscriptMessage(&msg))
		if text != "" {
			i.TextBytes += len(text)
			if i.FirstUserText == "" {
				i.FirstUserText = text
			}
			i.LastUserText = text
		}
	case "assistant":
		i.AssistantMessageCount++
		text := strings.TrimSpace(textFromTranscriptMessage(&msg))
		if text != "" {
			i.TextBytes += len(text)
			i.LastAssistantText = text
		}
	case "system":
		i.SystemMessageCount++
	}
}

func titleFromIndex(customTitle string, aiTitle string, firstUserText string, lastPrompt string, firstSummary string) string {
	if title := strings.TrimSpace(customTitle); title != "" {
		return truncateLine(title, 80)
	}
	if title := strings.TrimSpace(aiTitle); title != "" {
		return truncateLine(title, 80)
	}
	if text := strings.TrimSpace(firstUserText); text != "" {
		return truncateLine(text, 80)
	}
	if text := strings.TrimSpace(lastPrompt); text != "" {
		return truncateLine(text, 80)
	}
	if summary := strings.TrimSpace(firstSummary); summary != "" {
		return truncateLine(summary, 80)
	}
	return "Untitled session"
}
