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
	index.Title = titleFromIndex(customTitle, index.FirstUserText, firstSummary)
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

func titleFromIndex(customTitle string, firstUserText string, firstSummary string) string {
	if title := strings.TrimSpace(customTitle); title != "" {
		return truncateLine(title, 80)
	}
	if text := strings.TrimSpace(firstUserText); text != "" {
		return truncateLine(text, 80)
	}
	if summary := strings.TrimSpace(firstSummary); summary != "" {
		return truncateLine(summary, 80)
	}
	return "Untitled session"
}
