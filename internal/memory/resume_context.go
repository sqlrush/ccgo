package memory

import (
	"os"
	"path/filepath"
	"strings"

	"ccgo/internal/contracts"
	msgs "ccgo/internal/messages"
	"ccgo/internal/session"
)

const CurrentSessionContextSubtype = "session_memory_current"

type ResumeContextOptions struct {
	SessionPath string
	SessionID   contracts.ID
	Leaf        contracts.ID
	MemoryRoot  string
	RecallLimit int
}

type ResumeContext struct {
	Conversation   session.ResumeConversation
	CurrentSummary *SessionSummary
	RecallQuery    string
	Recalled       []RecallMatch
}

func (c ResumeContext) ContextMessages() []contracts.Message {
	var messages []contracts.Message
	if message := CurrentSessionSummaryMessage(c.CurrentSummary); message.Type != "" {
		messages = append(messages, message)
	}
	if message := RecallContextMessage(c.Recalled); message.Type != "" {
		messages = append(messages, message)
	}
	return messages
}

func (c ResumeContext) MessagesWithContext() []contracts.Message {
	contextMessages := c.ContextMessages()
	out := make([]contracts.Message, 0, len(contextMessages)+len(c.Conversation.Messages))
	out = append(out, contextMessages...)
	out = append(out, c.Conversation.Messages...)
	return out
}

func CurrentSessionSummaryMessage(summary *SessionSummary) contracts.Message {
	if summary == nil {
		return contracts.Message{}
	}
	text := strings.TrimSpace(summary.Summary)
	if text == "" {
		return contracts.Message{}
	}
	return contracts.Message{
		Type:      contracts.MessageUser,
		UUID:      contracts.NewID(),
		SessionID: summary.SessionID,
		Subtype:   CurrentSessionContextSubtype,
		IsMeta:    true,
		Content: []contracts.ContentBlock{contracts.NewTextBlock(
			"Current session memory:\n" + text,
		)},
	}
}

func BuildResumeContext(options ResumeContextOptions) (ResumeContext, error) {
	conversation, err := session.BuildResumeConversation(options.SessionPath, options.Leaf)
	if err != nil {
		return ResumeContext{}, err
	}
	root := options.MemoryRoot
	if root == "" {
		root = DefaultSessionMemoryRoot(options.SessionPath)
	}
	context := ResumeContext{Conversation: conversation}
	if root == "" {
		return context, nil
	}
	sessionID := options.SessionID
	if sessionID == "" {
		sessionID = inferSessionID(conversation.Messages)
	}
	if sessionID != "" {
		summary, ok, err := loadCurrentSessionSummary(root, sessionID)
		if err != nil {
			return context, err
		}
		if ok {
			context.CurrentSummary = &summary
		}
	}
	query := resumeRecallQuery(conversation.Messages)
	context.RecallQuery = query
	matches, err := RecallSessionSummaries(root, query, RecallOptions{
		Limit:            options.RecallLimit,
		ExcludeSessionID: sessionID,
	})
	if err != nil {
		return context, err
	}
	context.Recalled = matches
	return context, nil
}

func loadCurrentSessionSummary(root string, sessionID contracts.ID) (SessionSummary, bool, error) {
	path := filepath.Join(root, string(sessionID), SessionSummaryFilename)
	summary, err := LoadSessionSummary(path)
	if err != nil {
		if os.IsNotExist(err) {
			return SessionSummary{}, false, nil
		}
		return SessionSummary{}, false, err
	}
	return summary, true, nil
}

func inferSessionID(messages []contracts.Message) contracts.ID {
	for i := len(messages) - 1; i >= 0; i-- {
		if messages[i].SessionID != "" {
			return messages[i].SessionID
		}
	}
	return ""
}

func resumeRecallQuery(messages []contracts.Message) string {
	var fallback string
	for i := len(messages) - 1; i >= 0; i-- {
		text := msgs.TextContent(messages[i])
		if text == "" {
			continue
		}
		if fallback == "" {
			fallback = text
		}
		if messages[i].Type == contracts.MessageUser {
			return text
		}
	}
	return fallback
}
