package compact

import (
	"strings"
	"time"

	"ccgo/internal/contracts"
	msgs "ccgo/internal/messages"
	"ccgo/internal/session"
)

type Trigger string

const (
	TriggerManual Trigger = "manual"
	TriggerAuto   Trigger = "auto"
	TriggerSnip   Trigger = "snip"
)

type PlanOptions struct {
	Trigger        Trigger
	PreTokens      int
	UserContext    string
	KeepLast       int
	Summary        string
	BoundaryUUID   contracts.ID
	SummaryUUID    contracts.ID
	PreserveRecent bool
}

type Plan struct {
	Summarized []contracts.Message
	Kept       []contracts.Message
	Boundary   contracts.Message
	Summary    contracts.Message
	Output     []contracts.Message
	Metadata   session.CompactMetadata
}

func BuildPlan(history []contracts.Message, options PlanOptions) Plan {
	keepLast := options.KeepLast
	if keepLast < 0 {
		keepLast = 0
	}
	if keepLast > len(history) {
		keepLast = len(history)
	}
	split := len(history) - keepLast
	summarized := append([]contracts.Message(nil), history[:split]...)
	kept := append([]contracts.Message(nil), history[split:]...)

	boundaryID := options.BoundaryUUID
	if boundaryID == "" {
		boundaryID = contracts.NewID()
	}
	summaryID := options.SummaryUUID
	if summaryID == "" {
		summaryID = contracts.NewID()
	}
	trigger := string(options.Trigger)
	if trigger == "" {
		trigger = string(TriggerManual)
	}
	metadata := session.CompactMetadata{
		Trigger:                   trigger,
		PreTokens:                 options.PreTokens,
		UserContext:               options.UserContext,
		MessagesSummarized:        len(summarized),
		PreCompactDiscoveredTools: discoveredToolReferences(history),
	}
	boundary := contracts.Message{
		Type:      contracts.MessageSystem,
		UUID:      boundaryID,
		Subtype:   "compact_boundary",
		Timestamp: time.Now().UTC().Format(time.RFC3339Nano),
		Raw: map[string]any{
			"compactMetadata": metadata,
		},
	}
	summaryText := strings.TrimSpace(options.Summary)
	if summaryText == "" {
		summaryText = "Previous conversation was compacted; no summary was provided."
	}
	summary := msgs.UserText("Context summary from previous conversation:\n\n" + summaryText)
	summary.UUID = summaryID
	parent := boundary.UUID
	summary.ParentUUID = &parent

	output := []contracts.Message{boundary, summary}
	if options.PreserveRecent {
		last := summary.UUID
		for _, message := range kept {
			parentID := last
			message.ParentUUID = &parentID
			output = append(output, message)
			last = message.UUID
		}
	}
	return Plan{
		Summarized: summarized,
		Kept:       kept,
		Boundary:   boundary,
		Summary:    summary,
		Output:     output,
		Metadata:   metadata,
	}
}

func discoveredToolReferences(history []contracts.Message) []string {
	var out []string
	seen := map[string]struct{}{}
	for _, message := range history {
		for _, block := range message.Content {
			if block.Type != contracts.ContentToolResult {
				continue
			}
			collectToolReferenceNames(block.Content, seen, &out)
		}
	}
	return out
}

func collectToolReferenceNames(content any, seen map[string]struct{}, out *[]string) {
	switch typed := content.(type) {
	case contracts.ToolReference:
		addToolReferenceName(typed.ToolName, seen, out)
	case []contracts.ToolReference:
		for _, reference := range typed {
			addToolReferenceName(reference.ToolName, seen, out)
		}
	case map[string]any:
		if typeName, _ := typed["type"].(string); typeName == "tool_reference" {
			if toolName, _ := typed["tool_name"].(string); toolName != "" {
				addToolReferenceName(toolName, seen, out)
			}
		}
	case []any:
		for _, item := range typed {
			collectToolReferenceNames(item, seen, out)
		}
	}
}

func addToolReferenceName(name string, seen map[string]struct{}, out *[]string) {
	name = strings.TrimSpace(name)
	if name == "" {
		return
	}
	key := strings.ToLower(name)
	if _, ok := seen[key]; ok {
		return
	}
	seen[key] = struct{}{}
	*out = append(*out, name)
}

func BoundaryTranscriptMessage(message contracts.Message, metadata session.CompactMetadata) session.TranscriptMessage {
	rawMetadata := metadata
	return session.TranscriptMessage{
		Type:            "system",
		UUID:            message.UUID,
		ParentUUID:      message.ParentUUID,
		Timestamp:       message.Timestamp,
		Subtype:         "compact_boundary",
		Message:         &message,
		CompactMetadata: &rawMetadata,
	}
}
