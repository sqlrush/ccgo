package session

import (
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"ccgo/internal/contracts"
	msgs "ccgo/internal/messages"
	"ccgo/internal/platform"
)

type SessionInfo struct {
	ID          contracts.ID
	Path        string
	Title       string
	ProjectPath string
	GitBranch   string
	Modified    time.Time
	Size        int64
}

type SearchResult struct {
	SessionInfo
	Matches []string
}

type SessionListPage struct {
	Sessions []SessionInfo
	Offset   int
	Limit    int
	Total    int
	HasMore  bool
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
		projectPath := ""
		gitBranch := ""
		if index, err := LoadTranscriptIndex(path, id); err == nil {
			title = index.Title
			projectPath = index.ProjectPath
			gitBranch = index.GitBranch
		}
		sessions = append(sessions, SessionInfo{
			ID:          id,
			Path:        path,
			Title:       title,
			ProjectPath: projectPath,
			GitBranch:   gitBranch,
			Modified:    info.ModTime(),
			Size:        info.Size(),
		})
	}
	sort.SliceStable(sessions, func(i, j int) bool {
		return sessions[i].Modified.After(sessions[j].Modified)
	})
	return sessions, nil
}

func ListProjectSessionsPage(root string, offset int, limit int) (SessionListPage, error) {
	sessions, err := ListProjectSessions(root)
	if err != nil {
		return SessionListPage{}, err
	}
	total := len(sessions)
	if offset < 0 {
		offset = 0
	}
	if offset > total {
		offset = total
	}
	if limit <= 0 {
		limit = total - offset
	}
	end := offset + limit
	if end > total {
		end = total
	}
	return SessionListPage{
		Sessions: append([]SessionInfo(nil), sessions[offset:end]...),
		Offset:   offset,
		Limit:    limit,
		Total:    total,
		HasMore:  end < total,
	}, nil
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
		matches, err := SearchTranscriptFile(info.Path, query, 3)
		if err != nil {
			continue
		}
		if query != "" && len(matches) == 0 && !sessionInfoMatches(info, query) {
			continue
		}
		results = append(results, SearchResult{SessionInfo: info, Matches: matches})
		if len(results) >= limit {
			break
		}
	}
	return results, nil
}

func sessionInfoMatches(info SessionInfo, query string) bool {
	return strings.Contains(strings.ToLower(info.Title), query) ||
		strings.Contains(strings.ToLower(info.ProjectPath), query) ||
		strings.Contains(strings.ToLower(info.GitBranch), query)
}

func SearchTranscriptFile(path string, query string, maxMatches int) ([]string, error) {
	query = strings.ToLower(strings.TrimSpace(query))
	if query == "" {
		return nil, nil
	}
	if maxMatches <= 0 {
		maxMatches = 3
	}
	f, err := os.Open(path)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, err
	}
	defer f.Close()

	var matches []string
	progressBridge := map[contracts.ID]*contracts.ID{}
	scanner := newTranscriptScanner(f)
	for scanner.Scan() {
		for _, msg := range transcriptMessagesFromPhysicalLine(scanner.Bytes(), progressBridge) {
			text := strings.TrimSpace(textFromTranscriptMessage(&msg))
			if text == "" || !strings.Contains(strings.ToLower(text), query) {
				continue
			}
			matches = append(matches, snippet(text, query, 160))
			if len(matches) >= maxMatches {
				break
			}
		}
		if len(matches) >= maxMatches {
			break
		}
	}
	if err := scanner.Err(); err != nil {
		return nil, err
	}
	return matches, nil
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
	customTitle := transcript.CustomTitles[sessionID]
	aiTitle := transcript.AITitles[sessionID]
	lastPrompt := transcript.LastPrompts[sessionID]
	firstUserText := ""
	for _, id := range transcript.Order {
		msg := transcript.Messages[id]
		if msg == nil || msg.Type != "user" {
			continue
		}
		text := strings.TrimSpace(textFromTranscriptMessage(msg))
		if text != "" {
			firstUserText = text
			break
		}
	}
	firstSummary := firstSummaryFromTranscript(transcript)
	return titleFromIndex(customTitle, aiTitle, firstUserText, lastPrompt, firstSummary)
}

func firstSummaryFromTranscript(transcript Transcript) string {
	for _, id := range transcript.Order {
		if summary := strings.TrimSpace(transcript.Summaries[id]); summary != "" {
			return summary
		}
	}
	for _, summary := range transcript.Summaries {
		if strings.TrimSpace(summary) != "" {
			return summary
		}
	}
	return ""
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

// FindSessionGlobally searches all project directories under the Claude home
// for a session file matching sessionID. Returns the absolute path and true
// when found.
func FindSessionGlobally(sessionID string) (string, bool, error) {
	homeDir := platform.ClaudeHomeDir()
	projectsDir := filepath.Join(homeDir, "projects")
	entries, err := os.ReadDir(projectsDir)
	if err != nil {
		if os.IsNotExist(err) {
			return "", false, nil
		}
		return "", false, err
	}
	target := strings.TrimSpace(sessionID) + ".jsonl"
	for _, entry := range entries {
		if !entry.IsDir() {
			continue
		}
		candidate := filepath.Join(projectsDir, entry.Name(), target)
		if _, err := os.Stat(candidate); err == nil {
			return candidate, true, nil
		}
	}
	return "", false, nil
}
