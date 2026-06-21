package webtools

import (
	"context"
	"strings"
	"unicode/utf8"

	"ccgo/internal/model"
)

// maxSummarizeMarkdown caps content sent to the secondary model.
// Matches CC MAX_MARKDOWN_LENGTH = 100_000 (utils.ts:128).
const maxSummarizeMarkdown = 100_000

// secondaryModelName is the small/fast model WebFetch summarizes with.
var secondaryModelName = model.Claude45Haiku

const summarizeSystemPrompt = "You are summarizing web page content to answer a specific question. Be concise and factual; quote at most 125 characters at a time."

// makeSecondaryModelPrompt mirrors CC makeSecondaryModelPrompt (prompt.ts:23-46).
// Template: Web page content:\n---\n<content>\n---\n\n<prompt>\n\n<guidelines>
func makeSecondaryModelPrompt(content, prompt string) string {
	var b strings.Builder
	b.WriteString("Web page content:\n---\n")
	b.WriteString(truncateMarkdown(content))
	b.WriteString("\n---\n\n")
	b.WriteString(prompt)
	b.WriteString("\n\nProvide a focused answer. Use a strict 125-character maximum for any direct quotes.")
	return b.String()
}

// truncateMarkdown caps content at maxSummarizeMarkdown runes.
func truncateMarkdown(content string) string {
	if utf8.RuneCountInString(content) <= maxSummarizeMarkdown {
		return content
	}
	return string([]rune(content)[:maxSummarizeMarkdown])
}

// summarizeWebFetch calls the secondary model to produce a focused answer.
// Returns "" when no client is available or inputs are empty (falls back to
// raw rendered text, preserving default behavior).
//
// SummarizeRequest fields:
//   - Content: the rendered body (capped at maxSummarizeMarkdown runes), so
//     callers can assert the 100 K cap was applied.
//   - Prompt: the original user prompt string.
//
// The actual message sent to the model is makeSecondaryModelPrompt(content, prompt),
// which mirrors CC's makeSecondaryModelPrompt (prompt.ts:23-46).
func summarizeWebFetch(ctx context.Context, client SecondaryModelClient, content, prompt string) (string, error) {
	if client == nil || strings.TrimSpace(content) == "" || strings.TrimSpace(prompt) == "" {
		return "", nil
	}
	if ctx == nil {
		ctx = context.Background()
	}
	capped := truncateMarkdown(content)
	return client.Summarize(ctx, SummarizeRequest{
		Model:        secondaryModelName,
		SystemPrompt: summarizeSystemPrompt,
		Content:      capped,
		Prompt:       makeSecondaryModelPrompt(capped, prompt),
	})
}
