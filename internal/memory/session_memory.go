package memory

import (
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"time"

	"ccgo/internal/contracts"
	"ccgo/internal/session"
)

const (
	SessionSummaryFilename                  = "summary.md"
	SessionMemoryRollupTrigger              = "session-memory-rollup"
	SessionMemoryRollupID      contracts.ID = SessionMemoryRollupTrigger
)

type SessionSummary struct {
	SessionID       contracts.ID
	Path            string
	Summary         string
	UpdatedAt       time.Time
	LastMessageUUID contracts.ID
	Metadata        session.CompactMetadata
}

type SessionSummaryOptions struct {
	Root            string
	SessionID       contracts.ID
	Summary         string
	UpdatedAt       time.Time
	LastMessageUUID contracts.ID
	Metadata        session.CompactMetadata
}

type SessionMemoryCompactionOptions struct {
	KeepLatest      int
	MaxSummaryChars int
	ArchiveID       contracts.ID
	UpdatedAt       time.Time
}

type SessionMemoryCompactionResult struct {
	Kept      []SessionSummary
	Compacted []SessionSummary
	Archive   *SessionSummary
}

func DefaultSessionMemoryRoot(sessionPath string) string {
	if sessionPath == "" {
		return ""
	}
	return filepath.Join(filepath.Dir(sessionPath), "session-memory")
}

func WriteSessionSummary(options SessionSummaryOptions) (SessionSummary, error) {
	if options.Root == "" {
		return SessionSummary{}, fmt.Errorf("session memory root is required")
	}
	if options.SessionID == "" {
		return SessionSummary{}, fmt.Errorf("session id is required")
	}
	updatedAt := options.UpdatedAt
	if updatedAt.IsZero() {
		updatedAt = time.Now().UTC()
	}
	path := filepath.Join(options.Root, string(options.SessionID), SessionSummaryFilename)
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return SessionSummary{}, err
	}
	summary := strings.TrimSpace(options.Summary)
	content := formatSessionSummary(SessionSummary{
		SessionID:       options.SessionID,
		Path:            path,
		Summary:         summary,
		UpdatedAt:       updatedAt,
		LastMessageUUID: options.LastMessageUUID,
		Metadata:        options.Metadata,
	})
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		return SessionSummary{}, err
	}
	return SessionSummary{
		SessionID:       options.SessionID,
		Path:            path,
		Summary:         summary,
		UpdatedAt:       updatedAt,
		LastMessageUUID: options.LastMessageUUID,
		Metadata:        options.Metadata,
	}, nil
}

func LoadSessionSummary(path string) (SessionSummary, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return SessionSummary{}, err
	}
	frontmatter, body := ParseFrontmatter(string(data))
	updatedAt, _ := time.Parse(time.RFC3339, frontmatter["updated_at"])
	metadata := session.CompactMetadata{
		Trigger:            frontmatter["compact_trigger"],
		UserContext:        frontmatter["user_context"],
		MessagesSummarized: parseInt(frontmatter["messages_summarized"]),
		PreTokens:          parseInt(frontmatter["pre_tokens"]),
	}
	return SessionSummary{
		SessionID:       contracts.ID(frontmatter["session_id"]),
		Path:            path,
		Summary:         strings.TrimSpace(body),
		UpdatedAt:       updatedAt,
		LastMessageUUID: contracts.ID(frontmatter["last_message_uuid"]),
		Metadata:        metadata,
	}, nil
}

func CompactSessionMemory(root string, options SessionMemoryCompactionOptions) (SessionMemoryCompactionResult, error) {
	if root == "" {
		return SessionMemoryCompactionResult{}, fmt.Errorf("session memory root is required")
	}
	archiveID := options.ArchiveID
	if archiveID == "" {
		archiveID = SessionMemoryRollupID
	}
	summaries, err := LoadSessionSummaries(root)
	if err != nil {
		return SessionMemoryCompactionResult{}, err
	}
	var archives []SessionSummary
	var candidates []SessionSummary
	for _, summary := range summaries {
		if isSessionMemoryRollupArchive(summary, archiveID) {
			archives = append(archives, summary)
			continue
		}
		candidates = append(candidates, summary)
	}
	archive := mergeSessionMemoryArchives(archives, archiveID)
	sortSessionSummariesNewestFirst(candidates)
	keepLatest := options.KeepLatest
	if keepLatest < 0 {
		keepLatest = 0
	}
	if keepLatest > len(candidates) {
		keepLatest = len(candidates)
	}
	result := SessionMemoryCompactionResult{
		Kept:      append([]SessionSummary(nil), candidates[:keepLatest]...),
		Compacted: append([]SessionSummary(nil), candidates[keepLatest:]...),
	}
	if len(result.Compacted) == 0 {
		if archive != nil {
			result.Archive = archive
		}
		return result, nil
	}
	body := BuildSessionMemoryRollup(archive, result.Compacted, options.MaxSummaryChars)
	written, err := WriteSessionSummary(SessionSummaryOptions{
		Root:      root,
		SessionID: archiveID,
		Summary:   body,
		UpdatedAt: sessionMemoryCompactionTime(options.UpdatedAt),
		Metadata: session.CompactMetadata{
			Trigger:            SessionMemoryRollupTrigger,
			MessagesSummarized: len(result.Compacted),
		},
	})
	if err != nil {
		return SessionMemoryCompactionResult{}, err
	}
	for _, summary := range result.Compacted {
		if err := os.Remove(summary.Path); err != nil && !os.IsNotExist(err) {
			return SessionMemoryCompactionResult{}, err
		}
		_ = os.Remove(filepath.Dir(summary.Path))
	}
	result.Archive = &written
	return result, nil
}

func LoadSessionSummaries(root string) ([]SessionSummary, error) {
	var summaries []SessionSummary
	err := filepath.WalkDir(root, func(path string, entry os.DirEntry, err error) error {
		if err != nil {
			return nil
		}
		if entry.IsDir() || entry.Name() != SessionSummaryFilename {
			return nil
		}
		summary, err := LoadSessionSummary(path)
		if err != nil {
			return nil
		}
		if summary.UpdatedAt.IsZero() {
			if info, err := entry.Info(); err == nil {
				summary.UpdatedAt = info.ModTime()
			}
		}
		summaries = append(summaries, summary)
		return nil
	})
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, err
	}
	sortSessionSummariesNewestFirst(summaries)
	return summaries, nil
}

func BuildSessionMemoryRollup(existing *SessionSummary, summaries []SessionSummary, maxChars int) string {
	var b strings.Builder
	b.WriteString("Session memory rollup:")
	if existingText := normalizeSessionMemoryRollupText(existing); existingText != "" {
		b.WriteString("\n")
		b.WriteString(existingText)
	}
	for _, summary := range summaries {
		text := strings.Join(strings.Fields(summary.Summary), " ")
		if text == "" {
			continue
		}
		b.WriteString("\n")
		b.WriteString("[")
		b.WriteString(string(summary.SessionID))
		if !summary.UpdatedAt.IsZero() {
			b.WriteString(" | ")
			b.WriteString(summary.UpdatedAt.UTC().Format(time.RFC3339))
		}
		b.WriteString("] ")
		b.WriteString(text)
		if exceedsRuneLimit(b.String(), maxChars) {
			return truncateRunes(b.String(), maxChars)
		}
	}
	return strings.TrimSpace(b.String())
}

func exceedsRuneLimit(text string, maxChars int) bool {
	if maxChars <= 0 {
		return false
	}
	count := 0
	for range text {
		count++
		if count > maxChars {
			return true
		}
	}
	return false
}

func truncateRunes(text string, maxChars int) string {
	if maxChars <= 0 {
		return strings.TrimSpace(text)
	}
	count := 0
	for index := range text {
		if count == maxChars {
			return strings.TrimSpace(text[:index])
		}
		count++
	}
	return strings.TrimSpace(text)
}

func isSessionMemoryRollupArchive(summary SessionSummary, archiveID contracts.ID) bool {
	return summary.SessionID == archiveID || summary.Metadata.Trigger == SessionMemoryRollupTrigger
}

func mergeSessionMemoryArchives(summaries []SessionSummary, archiveID contracts.ID) *SessionSummary {
	if len(summaries) == 0 {
		return nil
	}
	archives := append([]SessionSummary(nil), summaries...)
	sort.SliceStable(archives, func(i, j int) bool {
		iArchiveID := archives[i].SessionID == archiveID
		jArchiveID := archives[j].SessionID == archiveID
		if iArchiveID != jArchiveID {
			return iArchiveID
		}
		if !archives[i].UpdatedAt.Equal(archives[j].UpdatedAt) {
			return archives[i].UpdatedAt.After(archives[j].UpdatedAt)
		}
		return archives[i].SessionID < archives[j].SessionID
	})
	merged := archives[0]
	var parts []string
	seen := map[string]struct{}{}
	for _, archive := range archives {
		text := normalizeSessionMemoryRollupText(&archive)
		if text == "" {
			continue
		}
		if _, ok := seen[text]; ok {
			continue
		}
		seen[text] = struct{}{}
		parts = append(parts, text)
	}
	merged.Summary = strings.Join(parts, "\n")
	return &merged
}

func normalizeSessionMemoryRollupText(summary *SessionSummary) string {
	if summary == nil {
		return ""
	}
	text := strings.TrimSpace(summary.Summary)
	for strings.HasPrefix(text, "Session memory rollup:") {
		text = strings.TrimSpace(strings.TrimPrefix(text, "Session memory rollup:"))
	}
	return text
}

func formatSessionSummary(summary SessionSummary) string {
	var b strings.Builder
	b.WriteString("---\n")
	b.WriteString("description: Compacted session summary\n")
	b.WriteString("type: session\n")
	b.WriteString("session_id: ")
	b.WriteString(string(summary.SessionID))
	b.WriteString("\n")
	b.WriteString("updated_at: ")
	b.WriteString(summary.UpdatedAt.UTC().Format(time.RFC3339))
	b.WriteString("\n")
	if summary.LastMessageUUID != "" {
		b.WriteString("last_message_uuid: ")
		b.WriteString(string(summary.LastMessageUUID))
		b.WriteString("\n")
	}
	if summary.Metadata.Trigger != "" {
		b.WriteString("compact_trigger: ")
		b.WriteString(summary.Metadata.Trigger)
		b.WriteString("\n")
	}
	if summary.Metadata.MessagesSummarized > 0 {
		b.WriteString("messages_summarized: ")
		b.WriteString(strconv.Itoa(summary.Metadata.MessagesSummarized))
		b.WriteString("\n")
	}
	if summary.Metadata.PreTokens > 0 {
		b.WriteString("pre_tokens: ")
		b.WriteString(strconv.Itoa(summary.Metadata.PreTokens))
		b.WriteString("\n")
	}
	if summary.Metadata.UserContext != "" {
		b.WriteString("user_context: ")
		b.WriteString(summary.Metadata.UserContext)
		b.WriteString("\n")
	}
	b.WriteString("---\n")
	b.WriteString(strings.TrimSpace(summary.Summary))
	b.WriteString("\n")
	return b.String()
}

func parseInt(raw string) int {
	n, _ := strconv.Atoi(strings.TrimSpace(raw))
	return n
}

func sessionMemoryCompactionTime(t time.Time) time.Time {
	if !t.IsZero() {
		return t.UTC()
	}
	return time.Now().UTC()
}

func sortSessionSummariesNewestFirst(summaries []SessionSummary) {
	sort.SliceStable(summaries, func(i, j int) bool {
		if !summaries[i].UpdatedAt.Equal(summaries[j].UpdatedAt) {
			return summaries[i].UpdatedAt.After(summaries[j].UpdatedAt)
		}
		return summaries[i].SessionID < summaries[j].SessionID
	})
}
