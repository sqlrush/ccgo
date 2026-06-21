package repl

import (
	"fmt"

	"ccgo/internal/conversation"
	"ccgo/internal/messages"
	"ccgo/internal/tui"
)

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

