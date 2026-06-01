package memory

import (
	"fmt"
	"strings"

	"ccgo/internal/contracts"
	msgs "ccgo/internal/messages"
)

type FactKind string

const (
	FactPreference FactKind = "preference"
	FactRequest    FactKind = "request"
	FactDecision   FactKind = "decision"
	FactTool       FactKind = "tool"
)

type MemoryFact struct {
	Kind       FactKind
	Text       string
	SourceUUID contracts.ID
}

type ExtractOptions struct {
	Limit int
}

func ExtractFacts(messages []contracts.Message, options ExtractOptions) []MemoryFact {
	limit := options.Limit
	if limit <= 0 {
		limit = 20
	}
	var facts []MemoryFact
	for _, message := range messages {
		if len(facts) >= limit {
			break
		}
		facts = appendFacts(facts, extractMessageFacts(message, limit-len(facts))...)
	}
	return facts
}

func BuildFactsSummary(facts []MemoryFact) string {
	if len(facts) == 0 {
		return ""
	}
	lines := []string{"Extracted session memory:"}
	for _, fact := range facts {
		lines = append(lines, fmt.Sprintf("- [%s] %s", fact.Kind, fact.Text))
	}
	return strings.Join(lines, "\n")
}

func extractMessageFacts(message contracts.Message, limit int) []MemoryFact {
	if limit <= 0 {
		return nil
	}
	var facts []MemoryFact
	text := strings.TrimSpace(msgs.TextContent(message))
	if text != "" {
		if fact, ok := textFact(message, text); ok {
			facts = append(facts, fact)
		}
	}
	for _, block := range message.Content {
		if len(facts) >= limit {
			break
		}
		if block.Type == contracts.ContentToolUse && block.Name != "" {
			facts = append(facts, MemoryFact{
				Kind:       FactTool,
				Text:       "Used tool " + block.Name,
				SourceUUID: message.UUID,
			})
		}
	}
	return facts
}

func textFact(message contracts.Message, text string) (MemoryFact, bool) {
	normalized := strings.Join(strings.Fields(text), " ")
	lower := strings.ToLower(normalized)
	switch {
	case strings.HasPrefix(lower, "remember "):
		return MemoryFact{Kind: FactPreference, Text: strings.TrimSpace(normalized[len("remember "):]), SourceUUID: message.UUID}, true
	case strings.HasPrefix(lower, "记住"):
		return MemoryFact{Kind: FactPreference, Text: strings.TrimSpace(strings.TrimPrefix(normalized, "记住")), SourceUUID: message.UUID}, true
	case strings.HasPrefix(lower, "decision:"):
		return MemoryFact{Kind: FactDecision, Text: strings.TrimSpace(normalized[len("decision:"):]), SourceUUID: message.UUID}, true
	case message.Type == contracts.MessageUser:
		return MemoryFact{Kind: FactRequest, Text: normalized, SourceUUID: message.UUID}, true
	default:
		return MemoryFact{}, false
	}
}

func appendFacts(existing []MemoryFact, next ...MemoryFact) []MemoryFact {
	seen := map[string]struct{}{}
	for _, fact := range existing {
		seen[string(fact.Kind)+"\x00"+fact.Text] = struct{}{}
	}
	for _, fact := range next {
		if fact.Text == "" {
			continue
		}
		key := string(fact.Kind) + "\x00" + fact.Text
		if _, ok := seen[key]; ok {
			continue
		}
		seen[key] = struct{}{}
		existing = append(existing, fact)
	}
	return existing
}
