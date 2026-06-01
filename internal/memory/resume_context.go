package memory

import (
	"context"
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
	RecallAgent *Agent
	Context     context.Context
}

type ResumeContext struct {
	Conversation      session.ResumeConversation
	CurrentSummary    *SessionSummary
	RecallQuery       string
	RecallSelectedIDs []contracts.ID
	RecallFallback    bool
	Recalled          []RecallMatch
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
	resumeContext := ResumeContext{Conversation: conversation}
	if root == "" {
		return resumeContext, nil
	}
	sessionID := options.SessionID
	if sessionID == "" {
		sessionID = inferSessionID(conversation.Messages)
	}
	if sessionID != "" {
		summary, ok, err := loadCurrentSessionSummary(root, sessionID)
		if err != nil {
			return resumeContext, err
		}
		if ok {
			resumeContext.CurrentSummary = &summary
		}
	}
	query := resumeRecallQuery(conversation.Messages)
	resumeContext.RecallQuery = query
	recallOptions := RecallOptions{
		Limit:            options.RecallLimit,
		ExcludeSessionID: sessionID,
	}
	if options.RecallAgent != nil {
		ctx := options.Context
		if ctx == nil {
			ctx = context.Background()
		}
		result, err := options.RecallAgent.Recall(ctx, root, query, recallOptions)
		if err != nil {
			return resumeContext, err
		}
		resumeContext.RecallQuery = result.Query
		resumeContext.RecallSelectedIDs = append([]contracts.ID(nil), result.SelectedIDs...)
		resumeContext.RecallFallback = result.Fallback
		resumeContext.Recalled = result.Matches
		return resumeContext, nil
	}
	matches, err := RecallSessionSummaries(root, query, recallOptions)
	if err != nil {
		return resumeContext, err
	}
	resumeContext.Recalled = matches
	return resumeContext, nil
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
