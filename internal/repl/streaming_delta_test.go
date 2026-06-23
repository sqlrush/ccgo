package repl

// REPL-21: incremental streaming delta render.
// Tests that EventStreamEvent/text_delta events accumulate in the loop's
// streaming buffer and update the live assistant message incrementally,
// rather than waiting for the final EventAssistantMessage.

import (
	"strings"
	"testing"

	anthropicpkg "ccgo/internal/api/anthropic"
	"ccgo/internal/contracts"
	"ccgo/internal/conversation"
	"ccgo/internal/tui"
)

// makeTextDeltaEvent returns a conversation.Event wrapping a content_block_delta
// with a text_delta payload — the exact shape that conversation.Run emits while
// streaming.
func makeTextDeltaEvent(text string) conversation.Event {
	ev := anthropicpkg.StreamEvent{
		Type:  "content_block_delta",
		Index: 0,
		Delta: map[string]any{"type": "text_delta", "text": text},
	}
	return conversation.Event{
		Type:        conversation.EventStreamEvent,
		StreamEvent: &ev,
	}
}

func makeThinkingDeltaEvent(thinking string) conversation.Event {
	ev := anthropicpkg.StreamEvent{
		Type:  "content_block_delta",
		Index: 0,
		Delta: map[string]any{"type": "thinking_delta", "thinking": thinking},
	}
	return conversation.Event{
		Type:        conversation.EventStreamEvent,
		StreamEvent: &ev,
	}
}

func makeAssistantMsgEvent(text string) conversation.Event {
	msg := contracts.Message{
		Type: contracts.MessageAssistant,
		Content: []contracts.ContentBlock{
			{Type: contracts.ContentText, Text: text},
		},
	}
	return conversation.Event{
		Type:    conversation.EventAssistantMessage,
		Message: &msg,
	}
}

// assistantMessages returns the assistant-role messages from the loop's screen.
func assistantMessages(l *Loop) []tui.Message {
	var out []tui.Message
	for _, m := range l.screen.Messages {
		if m.Role == tui.RoleAssistant {
			out = append(out, m)
		}
	}
	return out
}

// TestStreamingDeltaAccumulatesBuffer verifies that successive text_delta events
// grow the loop's streaming buffer monotonically: after N deltas the buffer
// contains all N chunks concatenated.
func TestStreamingDeltaAccumulatesBuffer(t *testing.T) {
	ft := NewFakeTerminal("", 80, 24)
	l := NewLoop(ft, nil)

	l.applyEvent(makeTextDeltaEvent("Hello"))
	if l.streamingBuf != "Hello" {
		t.Fatalf("after 1st delta: streamingBuf = %q, want %q", l.streamingBuf, "Hello")
	}

	l.applyEvent(makeTextDeltaEvent(", "))
	if l.streamingBuf != "Hello, " {
		t.Fatalf("after 2nd delta: streamingBuf = %q, want %q", l.streamingBuf, "Hello, ")
	}

	l.applyEvent(makeTextDeltaEvent("world"))
	if l.streamingBuf != "Hello, world" {
		t.Fatalf("after 3rd delta: streamingBuf = %q, want %q", l.streamingBuf, "Hello, world")
	}
}

// TestStreamingDeltaUpdatesScreenMessage verifies that after text_delta events,
// the screen contains exactly one assistant message whose text matches the
// accumulated buffer — i.e. streaming updates in-place, not appending new rows.
func TestStreamingDeltaUpdatesScreenMessage(t *testing.T) {
	ft := NewFakeTerminal("", 80, 24)
	l := NewLoop(ft, nil)

	l.applyEvent(makeTextDeltaEvent("Hi"))
	msgs := assistantMessages(l)
	if len(msgs) != 1 {
		t.Fatalf("after 1st delta: %d assistant messages, want 1", len(msgs))
	}
	if msgs[0].Text != "Hi" {
		t.Fatalf("after 1st delta: message text = %q, want %q", msgs[0].Text, "Hi")
	}

	l.applyEvent(makeTextDeltaEvent(" there"))
	msgs = assistantMessages(l)
	if len(msgs) != 1 {
		t.Fatalf("after 2nd delta: %d assistant messages, want 1 (streaming updates in place)", len(msgs))
	}
	if msgs[0].Text != "Hi there" {
		t.Fatalf("after 2nd delta: message text = %q, want %q", msgs[0].Text, "Hi there")
	}
}

// TestStreamingDeltaThinkingAccumulates verifies that thinking_delta events are
// accumulated in the streaming buffer and displayed as a screen message.
func TestStreamingDeltaThinkingAccumulates(t *testing.T) {
	ft := NewFakeTerminal("", 80, 24)
	l := NewLoop(ft, nil)

	l.applyEvent(makeThinkingDeltaEvent("Let me think"))
	if !strings.Contains(l.streamingBuf, "Let me think") {
		t.Fatalf("thinking delta not in streamingBuf: %q", l.streamingBuf)
	}
	msgs := assistantMessages(l)
	if len(msgs) != 1 {
		t.Fatalf("thinking delta: %d assistant messages, want 1", len(msgs))
	}
}

// TestStreamingDeltaClearedByAssistantMessage verifies that when the final
// EventAssistantMessage arrives the streaming buffer is cleared and the full
// message appears on screen.
func TestStreamingDeltaClearedByAssistantMessage(t *testing.T) {
	ft := NewFakeTerminal("", 80, 24)
	l := NewLoop(ft, nil)

	l.applyEvent(makeTextDeltaEvent("partial"))
	if l.streamingBuf == "" {
		t.Fatal("expected non-empty streamingBuf after delta")
	}

	l.applyEvent(makeAssistantMsgEvent("final complete text"))

	if l.streamingBuf != "" {
		t.Fatalf("streamingBuf should be cleared after EventAssistantMessage, got %q", l.streamingBuf)
	}
	if l.streamingActive {
		t.Fatal("streamingActive should be false after EventAssistantMessage")
	}
	msgs := assistantMessages(l)
	hasFinal := false
	for _, m := range msgs {
		if m.Text == "final complete text" {
			hasFinal = true
		}
	}
	if !hasFinal {
		t.Fatalf("final message not on screen; messages: %+v", msgs)
	}
}

// TestStreamingDeltaNonDeltaEventDoesNotAppend verifies that a non-content_block_delta
// stream event (e.g. message_start) does not add an assistant message or update
// the buffer.
func TestStreamingDeltaNonDeltaEventDoesNotAppend(t *testing.T) {
	ft := NewFakeTerminal("", 80, 24)
	l := NewLoop(ft, nil)

	ev := anthropicpkg.StreamEvent{Type: "message_start"}
	l.applyEvent(conversation.Event{
		Type:        conversation.EventStreamEvent,
		StreamEvent: &ev,
	})
	if l.streamingBuf != "" {
		t.Fatalf("non-delta event should not update streamingBuf; got %q", l.streamingBuf)
	}
	if len(assistantMessages(l)) != 0 {
		t.Fatal("non-delta event should not add assistant message")
	}
}
