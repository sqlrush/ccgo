package session

import (
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"ccgo/internal/contracts"
	msgs "ccgo/internal/messages"
)

type SessionInfo struct {
	ID       contracts.ID
	Path     string
	Title    string
	Modified time.Time
	Size     int64
}

type SearchResult struct {
	SessionInfo
	Matches []string
}

func ListProjectSessions(root string) ([]SessionInfo, error) {
	dir := ProjectDir(root)
	entries, err := os.ReadDir(dir)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, err
	}
	var sessions []SessionInfo
	for _, entry := range entries {
		if entry.IsDir() || filepath.Ext(entry.Name()) != ".jsonl" {
			continue
		}
		path := filepath.Join(dir, entry.Name())
		info, err := entry.Info()
		if err != nil {
			continue
		}
		id := contracts.ID(strings.TrimSuffix(entry.Name(), ".jsonl"))
		title := ""
		if index, err := LoadTranscriptIndex(path, id); err == nil {
			title = index.Title
		}
		sessions = append(sessions, SessionInfo{
			ID:       id,
			Path:     path,
			Title:    title,
			Modified: info.ModTime(),
			Size:     info.Size(),
		})
	}
	sort.SliceStable(sessions, func(i, j int) bool {
		return sessions[i].Modified.After(sessions[j].Modified)
	})
	return sessions, nil
}

func SearchProjectSessions(root string, query string, limit int) ([]SearchResult, error) {
	query = strings.ToLower(strings.TrimSpace(query))
	if limit <= 0 {
		limit = 20
	}
	sessions, err := ListProjectSessions(root)
	if err != nil {
		return nil, err
	}
	var results []SearchResult
	for _, info := range sessions {
		transcript, err := LoadTranscript(info.Path)
		if err != nil {
			continue
		}
		matches := SearchTranscript(transcript, query, 3)
		if query != "" && len(matches) == 0 && !strings.Contains(strings.ToLower(info.Title), query) {
			continue
		}
		results = append(results, SearchResult{SessionInfo: info, Matches: matches})
		if len(results) >= limit {
			break
		}
	}
	return results, nil
}

func SearchTranscript(transcript Transcript, query string, maxMatches int) []string {
	if strings.TrimSpace(query) == "" {
		return nil
	}
	if maxMatches <= 0 {
		maxMatches = 3
	}
	var matches []string
	for _, id := range transcript.Order {
		msg := transcript.Messages[id]
		text := strings.TrimSpace(textFromTranscriptMessage(msg))
		if text == "" {
			continue
		}
		if strings.Contains(strings.ToLower(text), query) {
			matches = append(matches, snippet(text, query, 160))
			if len(matches) >= maxMatches {
				break
			}
		}
	}
	return matches
}

func TitleFromTranscript(transcript Transcript, sessionID contracts.ID) string {
	if title := strings.TrimSpace(transcript.CustomTitles[sessionID]); title != "" {
		return title
	}
	for _, id := range transcript.Order {
		msg := transcript.Messages[id]
		if msg == nil || msg.Type != "user" {
			continue
		}
		text := strings.TrimSpace(textFromTranscriptMessage(msg))
		if text != "" {
			return truncateLine(text, 80)
		}
	}
	for _, summary := range transcript.Summaries {
		if strings.TrimSpace(summary) != "" {
			return truncateLine(summary, 80)
		}
	}
	return "Untitled session"
}

func textFromTranscriptMessage(msg *TranscriptMessage) string {
	if msg == nil {
		return ""
	}
	if msg.Message != nil {
		return msgs.TextContent(*msg.Message)
	}
	var parts []string
	for _, block := range transcriptContentBlocks(msg) {
		if block.Type == contracts.ContentText && block.Text != "" {
			parts = append(parts, block.Text)
		}
	}
	return strings.Join(parts, "\n")
}

func snippet(text string, query string, maxRunes int) string {
	if maxRunes <= 0 {
		return ""
	}
	lower := strings.ToLower(text)
	index := strings.Index(lower, query)
	if index < 0 {
		return truncateLine(text, maxRunes)
	}
	runes := []rune(text)
	prefixRunes := []rune(text[:index])
	start := len(prefixRunes) - maxRunes/3
	if start < 0 {
		start = 0
	}
	end := start + maxRunes
	if end > len(runes) {
		end = len(runes)
	}
	out := string(runes[start:end])
	if start > 0 {
		out = "..." + out
	}
	if end < len(runes) {
		out += "..."
	}
	return out
}

func truncateLine(text string, maxRunes int) string {
	text = strings.Join(strings.Fields(text), " ")
	runes := []rune(text)
	if len(runes) <= maxRunes {
		return text
	}
	if maxRunes <= 3 {
		return string(runes[:maxRunes])
	}
	return string(runes[:maxRunes-3]) + "..."
}
