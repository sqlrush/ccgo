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
		Trigger:            trigger,
		PreTokens:          options.PreTokens,
		UserContext:        options.UserContext,
		MessagesSummarized: len(summarized),
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
