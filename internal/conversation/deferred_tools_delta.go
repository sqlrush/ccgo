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
}

func (r Runner) maybeAppendDeferredToolsDeltaAttachment(ctx context.Context, history []contracts.Message) ([]contracts.Message, *contracts.Message, error) {
	if !deferredToolsDeltaEnabled() || r.Tools.Registry == nil {
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
	message := deferredToolsDeltaMessage(*delta)
	next, message := appendMessage(history, message)
	return next, &message, nil
}

func deferredToolsDeltaFromHistory(definitions []contracts.ToolDefinition, history []contracts.Message) *deferredToolsDelta {
	announced := map[string]struct{}{}
	for _, message := range history {
		payload, ok := deferredToolsDeltaAttachmentPayload(message)
		if !ok {
			continue
		}
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
	sort.Strings(addedNames)
	sort.Strings(addedLines)
	sort.Strings(removedNames)
	return &deferredToolsDelta{AddedNames: addedNames, AddedLines: addedLines, RemovedNames: removedNames}
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

func formatDeferredToolLine(definition contracts.ToolDefinition) string {
	return definition.Name
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
