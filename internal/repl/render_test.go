package repl

import (
	"encoding/json"
	"testing"

	"ccgo/internal/contracts"
	"ccgo/internal/conversation"
	"ccgo/internal/messages"
	"ccgo/internal/tui"
)

func TestMessageFromEventAssistant(t *testing.T) {
	asst := messages.UserText("") // placeholder; build assistant message:
	asst.Type = contracts.MessageAssistant
	asst.Content = []contracts.ContentBlock{contracts.NewTextBlock("hello there")}

	ev := conversation.Event{Type: conversation.EventAssistantMessage, Message: &asst}
	msg, ok := messageFromEvent(ev)
	if !ok {
		t.Fatal("expected a renderable message for assistant event")
	}
	if msg.Text != "hello there" {
		t.Fatalf("msg.Text = %q want %q", msg.Text, "hello there")
	}
	if msg.Role != tui.RoleAssistant {
		t.Fatalf("msg.Role = %q want %q", msg.Role, tui.RoleAssistant)
	}
}

func TestMessageFromEventSkipsInternal(t *testing.T) {
	ev := conversation.Event{Type: conversation.EventToolSearchDecision}
	if _, ok := messageFromEvent(ev); ok {
		t.Fatal("internal event should not render")
	}
}

func TestMessageFromEventToolUse(t *testing.T) {
	use := contracts.ToolUse{Name: "bash"}
	ev := conversation.Event{Type: conversation.EventToolUse, ToolUse: &use}
	msg, ok := messageFromEvent(ev)
	if !ok {
		t.Fatal("expected renderable message for tool_use event")
	}
	if msg.Role != tui.RoleTool {
		t.Fatalf("msg.Role = %q want %q", msg.Role, tui.RoleTool)
	}
	if msg.Text != "⏺ bash" {
		t.Fatalf("msg.Text = %q want %q", msg.Text, "⏺ bash")
	}
}

func TestMessageFromEventToolResultSkipped(t *testing.T) {
	// EventToolResult is now handled by applyEvent via renderToolResultText;
	// messageFromEvent must not render it (returns false).
	res := contracts.ToolResult{IsError: false}
	ev := conversation.Event{Type: conversation.EventToolResult, ToolResult: &res}
	if _, ok := messageFromEvent(ev); ok {
		t.Fatal("EventToolResult should no longer be rendered by messageFromEvent")
	}
}

func TestRenderToolResultTextOkSummary(t *testing.T) {
	tu := &contracts.ToolUse{ID: "r1", Name: "Read", Input: json.RawMessage(`{}`)}
	tr := &contracts.ToolResult{ToolUseID: "r1", IsError: false}
	out := renderToolResultText(tu, tr)
	if out != "  ⎿ ok" {
		t.Fatalf("renderToolResultText ok = %q want %q", out, "  ⎿ ok")
	}
}

func TestRenderToolResultTextErrorSummary(t *testing.T) {
	tu := &contracts.ToolUse{ID: "r2", Name: "Read", Input: json.RawMessage(`{}`)}
	tr := &contracts.ToolResult{ToolUseID: "r2", IsError: true}
	out := renderToolResultText(tu, tr)
	if out != "  ⎿ error" {
		t.Fatalf("renderToolResultText error = %q want %q", out, "  ⎿ error")
	}
}

func TestMessageFromEventSkipsDeferredPool(t *testing.T) {
	ev := conversation.Event{Type: conversation.EventDeferredPoolChange}
	if _, ok := messageFromEvent(ev); ok {
		t.Fatal("EventDeferredPoolChange should not render")
	}
}
