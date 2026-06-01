package memory

import (
	"fmt"
	"strings"

	"ccgo/internal/contracts"
)

const RecallContextSubtype = "session_memory_recall"

func BuildRecallContext(matches []RecallMatch) string {
	if len(matches) == 0 {
		return ""
	}
	lines := []string{"Relevant session memory:"}
	for _, match := range matches {
		snippet := match.Snippet
		if snippet == "" {
			snippet = snippetText(match.Summary.Summary, 240)
		}
		if snippet == "" {
			continue
		}
		sessionID := string(match.Summary.SessionID)
		if sessionID == "" {
			sessionID = "unknown"
		}
		lines = append(lines, fmt.Sprintf("- [%s] %s", sessionID, snippet))
	}
	if len(lines) == 1 {
		return ""
	}
	return strings.Join(lines, "\n")
}

func RecallContextMessage(matches []RecallMatch) contracts.Message {
	text := BuildRecallContext(matches)
	if text == "" {
		return contracts.Message{}
	}
	return contracts.Message{
		Type:    contracts.MessageUser,
		UUID:    contracts.NewID(),
		Subtype: RecallContextSubtype,
		IsMeta:  true,
		Content: []contracts.ContentBlock{contracts.NewTextBlock(text)},
	}
}

func snippetText(text string, max int) string {
	text = strings.Join(strings.Fields(strings.TrimSpace(text)), " ")
	if max <= 0 || len(text) <= max {
		return text
	}
	return strings.TrimSpace(text[:max])
}
