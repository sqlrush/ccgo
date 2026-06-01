package memory

import (
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"ccgo/internal/contracts"
	"ccgo/internal/session"
)

const SessionSummaryFilename = "summary.md"

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
