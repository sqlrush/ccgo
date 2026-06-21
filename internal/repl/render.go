package repl

import (
	"fmt"

	"ccgo/internal/contracts"
	"ccgo/internal/conversation"
	"ccgo/internal/messages"
	"ccgo/internal/tui"
)

// historyToScreen converts a contracts.Message slice to tui.Message slice for
// display. Messages with no text content are omitted.
func historyToScreen(history []contracts.Message) []tui.Message {
	result := make([]tui.Message, 0, len(history))
	for _, m := range history {
		var role tui.Role
		switch m.Type {
		case contracts.MessageUser:
			role = tui.RoleUser
		case contracts.MessageAssistant:
			role = tui.RoleAssistant
		default:
			continue
		}
		var text string
		for _, block := range m.Content {
			if block.Type == contracts.ContentText {
				text += block.Text
			}
		}
		if text == "" {
			continue
		}
		result = append(result, tui.Message{Role: role, Text: text})
	}
	return result
}

// messageFromEvent maps a conversation event to a renderable screen message.
// Returns false for events that should not appear in the transcript view.
func messageFromEvent(ev conversation.Event) (tui.Message, bool) {
	switch ev.Type {
	case conversation.EventAssistantMessage:
		if ev.Message == nil {
			return tui.Message{}, false
		}
		text := messages.TextContent(*ev.Message)
		if text == "" {
			return tui.Message{}, false
		}
		return tui.Message{Role: tui.RoleAssistant, Text: text}, true
	case conversation.EventToolUse:
		if ev.ToolUse == nil {
			return tui.Message{}, false
		}
		return tui.Message{Role: tui.RoleTool, Text: fmt.Sprintf("⏺ %s", ev.ToolUse.Name)}, true
	default:
		return tui.Message{}, false
	}
}

