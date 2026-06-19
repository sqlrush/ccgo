package messages

import (
	"strings"

	"ccgo/internal/contracts"
)

func NormalizeForAPI(in []contracts.Message) []contracts.APIMessage {
	out := make([]contracts.APIMessage, 0, len(in))
	for _, msg := range in {
		switch msg.Type {
		case contracts.MessageUser:
			out = append(out, contracts.APIMessage{Role: "user", Content: msg.Content})
		case contracts.MessageAssistant:
			out = append(out, contracts.APIMessage{Role: "assistant", Content: msg.Content})
		case contracts.MessageAttachment:
			if apiMessage, ok := attachmentMessageForAPI(msg); ok {
				out = append(out, apiMessage)
			}
		default:
			continue
		}
	}
	return out
}

func attachmentMessageForAPI(msg contracts.Message) (contracts.APIMessage, bool) {
	payload, ok := attachmentPayload(msg)
	if !ok {
		return contracts.APIMessage{}, false
	}
	if strings.TrimSpace(attachmentString(payload["type"])) != "deferred_tools_delta" {
		return contracts.APIMessage{}, false
	}
	var parts []string
	if addedLines := attachmentStringSlice(payload["addedLines"], payload["added_lines"], payload["addedNames"], payload["added_names"]); len(addedLines) > 0 {
		parts = append(parts, "The following deferred tools are now available via ToolSearch:\n"+strings.Join(addedLines, "\n"))
	}
	if removedNames := attachmentStringSlice(payload["removedNames"], payload["removed_names"]); len(removedNames) > 0 {
		parts = append(parts, "The following deferred tools are no longer available (their MCP server disconnected). Do not search for them - ToolSearch will return no match:\n"+strings.Join(removedNames, "\n"))
	}
	if len(parts) == 0 {
		return contracts.APIMessage{}, false
	}
	return contracts.APIMessage{
		Role:    "user",
		Content: []contracts.ContentBlock{contracts.NewTextBlock(strings.Join(parts, "\n\n"))},
	}, true
}

func attachmentPayload(msg contracts.Message) (map[string]any, bool) {
	if msg.Type != contracts.MessageAttachment || len(msg.Raw) == 0 {
		return nil, false
	}
	payload, ok := msg.Raw["attachment"].(map[string]any)
	return payload, ok
}

func attachmentStringSlice(values ...any) []string {
	for _, value := range values {
		switch typed := value.(type) {
		case []string:
			out := cleanStringSlice(typed)
			if len(out) > 0 {
				return out
			}
		case []any:
			out := make([]string, 0, len(typed))
			for _, item := range typed {
				if trimmed := strings.TrimSpace(attachmentString(item)); trimmed != "" {
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
			out := cleanStringSlice(fields)
			if len(out) > 0 {
				return out
			}
		}
	}
	return nil
}

func cleanStringSlice(values []string) []string {
	out := make([]string, 0, len(values))
	for _, value := range values {
		if trimmed := strings.TrimSpace(value); trimmed != "" {
			out = append(out, trimmed)
		}
	}
	return out
}

func attachmentString(value any) string {
	switch typed := value.(type) {
	case string:
		return typed
	case []byte:
		return string(typed)
	default:
		return ""
	}
}

func LinkParentChain(messages []contracts.Message) []contracts.Message {
	if len(messages) == 0 {
		return nil
	}
	out := make([]contracts.Message, len(messages))
	copy(out, messages)
	var parent *contracts.ID
	for i := range out {
		if out[i].UUID == "" {
			out[i].UUID = contracts.NewID()
		}
		out[i].ParentUUID = parent
		id := out[i].UUID
		parent = &id
	}
	return out
}
