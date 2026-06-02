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
		switch normalizeTranscriptMetadataType(envelope.Type) {
		case "summary":
			if _, summary, ok := parseSummaryMetadata(line); ok {
				index.SummaryCount++
				if firstSummary == "" && strings.TrimSpace(summary) != "" {
					firstSummary = summary
				}
			}
		case "custom-title":
			if entrySessionID, title, ok := parseSessionStringMetadata(line, "customTitle", "custom_title"); ok && strings.TrimSpace(title) != "" {
				if sessionID == "" || entrySessionID == sessionID {
					customTitle = title
				}
			}
		case "ai-title":
			if entrySessionID, title, ok := parseSessionStringMetadata(line, "aiTitle", "ai_title"); ok && (sessionID == "" || entrySessionID == sessionID) {
				index.AITitle = title
			}
		case "last-prompt":
			if entrySessionID, prompt, ok := parseSessionStringMetadata(line, "lastPrompt", "last_prompt"); ok && (sessionID == "" || entrySessionID == sessionID) {
				index.LastPrompt = prompt
			}
		case "task-summary":
			if entry, ok := parseTaskSummaryMetadata(line); ok && (sessionID == "" || entry.SessionID == sessionID) {
				index.TaskSummary = entry.Summary
			}
		case "tag":
			if entrySessionID, tag, ok := parseSessionStringMetadata(line, "tag"); ok && (sessionID == "" || entrySessionID == sessionID) {
				index.Tag = tag
			}
		case "agent-name":
			if entrySessionID, name, ok := parseSessionStringMetadata(line, "agentName", "agent_name"); ok && (sessionID == "" || entrySessionID == sessionID) {
				index.AgentName = name
			}
		case "agent-color":
			if entrySessionID, color, ok := parseSessionStringMetadata(line, "agentColor", "agent_color"); ok && (sessionID == "" || entrySessionID == sessionID) {
				index.AgentColor = color
			}
		case "agent-setting":
			if entrySessionID, setting, ok := parseSessionStringMetadata(line, "agentSetting", "agent_setting"); ok && (sessionID == "" || entrySessionID == sessionID) {
				index.AgentSetting = setting
			}
		case "pr-link":
			if entry, ok := parsePRLinkMetadata(line); ok && (sessionID == "" || entry.SessionID == sessionID) {
				index.PRNumber = entry.PRNumber
				index.PRURL = entry.PRURL
				index.PRRepository = entry.PRRepository
			}
		case "mode":
			if entrySessionID, mode, ok := parseSessionStringMetadata(line, "mode"); ok && (sessionID == "" || entrySessionID == sessionID) {
				index.Mode = mode
			}
		case "worktree-state":
			if entry, ok := parseWorktreeStateMetadata(line); ok && (sessionID == "" || entry.SessionID == sessionID) {
				index.HasWorktreeState = len(entry.WorktreeSession) > 0 && string(entry.WorktreeSession) != "null"
			}
		case "content-replacement":
			if entry, ok := parseContentReplacementMetadata(line); ok {
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
