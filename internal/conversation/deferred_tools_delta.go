package conversation

import (
	"context"
	"sort"
	"strings"

	"ccgo/internal/contracts"
)

type deferredToolsDelta struct {
	AddedNames   []string
	AddedLines   []string
	RemovedNames []string
	PoolChange   DeferredToolsPoolChange
}

func (r Runner) maybeAppendDeferredToolsDeltaAttachment(ctx context.Context, history []contracts.Message) ([]contracts.Message, *contracts.Message, error) {
	if !r.deferredToolsDeltaEnabled() || r.Tools.Registry == nil {
		return history, nil, nil
	}
	definitions, err := r.Tools.Registry.Definitions(toolPromptContext(r))
	if err != nil {
		return history, nil, err
	}
	if !hasToolSearchDefinition(definitions) {
		return history, nil, nil
	}
	if !toolSearchEnabledForRequest(r.model(), definitions, r.deferredToolTokenCounter(ctx, r.model())) {
		return history, nil, nil
	}
	delta := deferredToolsDeltaFromHistory(definitions, history)
	if delta == nil {
		return history, nil, nil
	}
	poolChange := delta.PoolChange
	poolChange.CallSite = "attachments_main"
	if poolChange.QuerySource == "" {
		poolChange.QuerySource = "unknown"
	}
	r.emit(Event{Type: EventDeferredPoolChange, DeferredToolsPoolChange: &poolChange})
	message := deferredToolsDeltaMessage(*delta)
	next, message := appendMessage(history, message)
	return next, &message, nil
}

func deferredToolsDeltaFromHistory(definitions []contracts.ToolDefinition, history []contracts.Message) *deferredToolsDelta {
	announced := map[string]struct{}{}
	attachmentTypesSeen := map[string]struct{}{}
	attachmentCount := 0
	deferredToolsDeltaCount := 0
	for _, message := range history {
		if message.Type == contracts.MessageAttachment {
			attachmentCount++
			if attachmentType := attachmentMessageType(message); attachmentType != "" {
				attachmentTypesSeen[attachmentType] = struct{}{}
			}
		}
		payload, ok := deferredToolsDeltaAttachmentPayload(message)
		if !ok {
			continue
		}
		deferredToolsDeltaCount++
		for _, name := range stringSliceValue(payload["addedNames"], payload["added_names"]) {
			announced[name] = struct{}{}
		}
		for _, name := range stringSliceValue(payload["removedNames"], payload["removed_names"]) {
			delete(announced, name)
		}
	}

	deferredNames := map[string]struct{}{}
	poolNames := map[string]struct{}{}
	var addedNames []string
	var addedLines []string
	for _, definition := range definitions {
		if strings.TrimSpace(definition.Name) == "" {
			continue
		}
		poolNames[definition.Name] = struct{}{}
		if !toolDefinitionDeferred(definition) {
			continue
		}
		deferredNames[definition.Name] = struct{}{}
		if _, ok := announced[definition.Name]; ok {
			continue
		}
		addedNames = append(addedNames, definition.Name)
		addedLines = append(addedLines, formatDeferredToolLine(definition))
	}

	var removedNames []string
	for name := range announced {
		if _, stillDeferred := deferredNames[name]; stillDeferred {
			continue
		}
		if _, stillInPool := poolNames[name]; stillInPool {
			continue
		}
		removedNames = append(removedNames, name)
	}
	if len(addedNames) == 0 && len(removedNames) == 0 {
		return nil
	}
	poolChange := DeferredToolsPoolChange{
		AddedCount:              len(addedNames),
		RemovedCount:            len(removedNames),
		PriorAnnouncedCount:     len(announced),
		MessagesLength:          len(history),
		AttachmentCount:         attachmentCount,
		DeferredToolsDeltaCount: deferredToolsDeltaCount,
		AttachmentTypesSeen:     strings.Join(sortedStringSet(attachmentTypesSeen), ","),
	}
	sort.Strings(addedNames)
	sort.Strings(addedLines)
	sort.Strings(removedNames)
	return &deferredToolsDelta{AddedNames: addedNames, AddedLines: addedLines, RemovedNames: removedNames, PoolChange: poolChange}
}

func deferredToolsDeltaMessage(delta deferredToolsDelta) contracts.Message {
	attachment := map[string]any{
		"type":         "deferred_tools_delta",
		"addedNames":   append([]string(nil), delta.AddedNames...),
		"addedLines":   append([]string(nil), delta.AddedLines...),
		"removedNames": append([]string(nil), delta.RemovedNames...),
	}
	return contracts.Message{
		Type:    contracts.MessageAttachment,
		UUID:    contracts.NewID(),
		Subtype: "deferred_tools_delta",
		IsMeta:  true,
		Raw:     map[string]any{"attachment": attachment},
	}
}

func deferredToolsDeltaAttachmentPayload(message contracts.Message) (map[string]any, bool) {
	if message.Type != contracts.MessageAttachment {
		return nil, false
	}
	raw := message.Raw
	if len(raw) == 0 {
		return nil, false
	}
	payload, ok := raw["attachment"].(map[string]any)
	if !ok {
		if nested, nestedOK := raw["attachment"].(map[string]string); nestedOK {
			payload = map[string]any{}
			for key, value := range nested {
				payload[key] = value
			}
			ok = true
		}
	}
	if !ok {
		return nil, false
	}
	if strings.TrimSpace(stringValue(payload["type"])) != "deferred_tools_delta" {
		return nil, false
	}
	return payload, true
}

func attachmentMessageType(message contracts.Message) string {
	if message.Type != contracts.MessageAttachment || len(message.Raw) == 0 {
		return ""
	}
	payload, ok := message.Raw["attachment"].(map[string]any)
	if !ok {
		if nested, nestedOK := message.Raw["attachment"].(map[string]string); nestedOK {
			return strings.TrimSpace(nested["type"])
		}
		return ""
	}
	return strings.TrimSpace(stringValue(payload["type"]))
}

func formatDeferredToolLine(definition contracts.ToolDefinition) string {
	return definition.Name
}

func sortedStringSet(values map[string]struct{}) []string {
	if len(values) == 0 {
		return nil
	}
	out := make([]string, 0, len(values))
	for value := range values {
		if trimmed := strings.TrimSpace(value); trimmed != "" {
			out = append(out, trimmed)
		}
	}
	sort.Strings(out)
	return out
}

func stringSliceValue(values ...any) []string {
	for _, value := range values {
		switch typed := value.(type) {
		case []string:
			out := make([]string, 0, len(typed))
			for _, item := range typed {
				if trimmed := strings.TrimSpace(item); trimmed != "" {
					out = append(out, trimmed)
				}
			}
			if len(out) > 0 {
				return out
			}
		case []any:
			out := make([]string, 0, len(typed))
			for _, item := range typed {
				if trimmed := strings.TrimSpace(stringValue(item)); trimmed != "" {
					out = append(out, trimmed)
				}
			}
			if len(out) > 0 {
				return out
			}
		case string:
			fields := strings.FieldsFunc(typed, func(r rune) bool {
				return r == ',' || r == '\n' || r == '\r' || r == '\t'
			})
			out := make([]string, 0, len(fields))
			for _, item := range fields {
				if trimmed := strings.TrimSpace(item); trimmed != "" {
					out = append(out, trimmed)
				}
			}
			if len(out) > 0 {
				return out
			}
		}
	}
	return nil
}

func stringValue(value any) string {
	switch typed := value.(type) {
	case string:
		return typed
	case []byte:
		return string(typed)
	default:
		return ""
	}
}
