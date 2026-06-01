package compact

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"strings"
	"sync"

	"ccgo/internal/contracts"
	msgs "ccgo/internal/messages"
)

const DefaultMicroMaxChars = 4_000

type MicroOptions struct {
	KeepLast int
	MaxChars int
	Cache    *MicroCache
}

type MicroResult struct {
	Summary            string
	Digest             string
	Cached             bool
	MessagesSummarized int
	MessagesKept       int
}

type MicroCache struct {
	mu      sync.RWMutex
	entries map[string]MicroResult
}

func NewMicroCache() *MicroCache {
	return &MicroCache{entries: map[string]MicroResult{}}
}

func (c *MicroCache) Get(digest string) (MicroResult, bool) {
	if c == nil {
		return MicroResult{}, false
	}
	c.mu.RLock()
	defer c.mu.RUnlock()
	result, ok := c.entries[digest]
	if ok {
		result.Cached = true
	}
	return result, ok
}

func (c *MicroCache) Set(result MicroResult) {
	if c == nil || result.Digest == "" {
		return
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	result.Cached = false
	c.entries[result.Digest] = result
}

func MicroCompact(history []contracts.Message, options MicroOptions) MicroResult {
	keepLast := options.KeepLast
	if keepLast < 0 {
		keepLast = 0
	}
	if keepLast > len(history) {
		keepLast = len(history)
	}
	summarized := history[:len(history)-keepLast]
	digest := DigestMessages(summarized)
	if cached, ok := options.Cache.Get(digest); ok {
		cached.MessagesKept = keepLast
		return cached
	}
	maxChars := options.MaxChars
	if maxChars <= 0 {
		maxChars = DefaultMicroMaxChars
	}
	result := MicroResult{
		Summary:            summarizeMessages(summarized, maxChars),
		Digest:             digest,
		MessagesSummarized: len(summarized),
		MessagesKept:       keepLast,
	}
	options.Cache.Set(result)
	return result
}

func DigestMessages(messages []contracts.Message) string {
	hash := sha256.New()
	for _, message := range messages {
		fmt.Fprintf(hash, "%s\x00%s\x00%s\x00", message.Type, message.UUID, message.Subtype)
		for _, block := range message.Content {
			fmt.Fprintf(hash, "%s\x00%s\x00%s\x00%s\x00%s\x00", block.Type, block.ID, block.Name, block.ToolUseID, block.Text)
			if block.Input != nil {
				hash.Write(block.Input)
			}
		}
	}
	return hex.EncodeToString(hash.Sum(nil))
}

func summarizeMessages(messages []contracts.Message, maxChars int) string {
	if len(messages) == 0 {
		return "No previous messages to microcompact."
	}
	var lines []string
	remaining := maxChars
	for _, message := range messages {
		if remaining <= 0 {
			break
		}
		line := microLine(message)
		if line == "" {
			continue
		}
		if len(line) > remaining {
			line = line[:remaining]
		}
		lines = append(lines, line)
		remaining -= len(line) + 1
	}
	if len(lines) == 0 {
		return "Previous messages contained no text summary."
	}
	return strings.Join(lines, "\n")
}

func microLine(message contracts.Message) string {
	role := string(message.Type)
	if role == "" {
		role = "message"
	}
	text := strings.TrimSpace(msgs.TextContent(message))
	if text != "" {
		text = strings.Join(strings.Fields(text), " ")
		return role + ": " + text
	}
	for _, block := range message.Content {
		switch block.Type {
		case contracts.ContentToolUse:
			return fmt.Sprintf("%s: tool_use %s", role, block.Name)
		case contracts.ContentToolResult:
			return fmt.Sprintf("%s: tool_result %s", role, block.ToolUseID)
		}
	}
	return ""
}
