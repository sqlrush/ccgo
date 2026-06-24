package main

// G3: --include-partial-messages wiring for stream-json output.
// Tests that attachStreamJSON emits partial assistant_message events as
// text_delta stream events arrive when includePartialMessages=true,
// and suppresses them when false.
//
// CC ref: --include-partial-messages flag (F2-C04).
// REPL-21 / partial-messages items in docs/cc-parity/sections/01-headless.md.

import (
	"bytes"
	"encoding/json"
	"strings"
	"testing"

	anthropicpkg "ccgo/internal/api/anthropic"
	"ccgo/internal/conversation"
)

func makeStreamTextDeltaEvent(text string) conversation.Event {
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

// TestAttachStreamJSONPartialMessages_WithFlag verifies that when
// includePartialMessages=true, text_delta events cause partial assistant_message
// events (type "assistant_message") to be emitted, one per delta, each
// containing the accumulated text so far.
func TestAttachStreamJSONPartialMessages_WithFlag(t *testing.T) {
	var buf bytes.Buffer
	runner := conversation.Runner{
		Model:     "claude-stub",
		SessionID: "sess-partial",
	}

	runner, getErr := attachStreamJSON(&buf, runner, true, false)

	runner.OnEvent(makeStreamTextDeltaEvent("Hello"))
	runner.OnEvent(makeStreamTextDeltaEvent(", world"))

	if err := getErr(); err != nil {
		t.Fatalf("attachStreamJSON error: %v", err)
	}

	// Collect all partial assistant_message events.
	partials := collectPartialMessages(t, buf.String())
	if len(partials) < 2 {
		t.Fatalf("expected ≥2 partial assistant_message events, got %d; output:\n%s",
			len(partials), buf.String())
	}
	// First partial should contain only "Hello".
	if partials[0] != "Hello" {
		t.Errorf("1st partial text = %q, want %q", partials[0], "Hello")
	}
	// Second partial should contain "Hello, world".
	if partials[1] != "Hello, world" {
		t.Errorf("2nd partial text = %q, want %q", partials[1], "Hello, world")
	}
}

// TestAttachStreamJSONPartialMessages_WithoutFlag verifies that when
// includePartialMessages=false, text_delta events do NOT emit extra
// assistant_message events (the stream_event events still flow as usual).
func TestAttachStreamJSONPartialMessages_WithoutFlag(t *testing.T) {
	var buf bytes.Buffer
	runner := conversation.Runner{
		Model:     "claude-stub",
		SessionID: "sess-no-partial",
	}

	runner, getErr := attachStreamJSON(&buf, runner, false, false)

	runner.OnEvent(makeStreamTextDeltaEvent("Hello"))
	runner.OnEvent(makeStreamTextDeltaEvent(", world"))

	if err := getErr(); err != nil {
		t.Fatalf("attachStreamJSON error: %v", err)
	}

	// No partial assistant_message events expected.
	partials := collectPartialMessages(t, buf.String())
	if len(partials) != 0 {
		t.Fatalf("expected 0 partial assistant_message events, got %d; output:\n%s",
			len(partials), buf.String())
	}
	// But stream_event lines should still appear.
	if !strings.Contains(buf.String(), `"stream_event"`) {
		t.Fatalf("expected stream_event lines in output; got:\n%s", buf.String())
	}
}

// collectPartialMessages parses NDJSON and returns the accumulated text from
// each {"type":"assistant_message","is_partial":true} event.
func collectPartialMessages(t *testing.T, ndjson string) []string {
	t.Helper()
	var out []string
	for _, line := range strings.Split(strings.TrimSpace(ndjson), "\n") {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		var event map[string]any
		if err := json.Unmarshal([]byte(line), &event); err != nil {
			t.Fatalf("invalid JSON line %q: %v", line, err)
		}
		if event["type"] == "assistant_message" && event["is_partial"] == true {
			// Extract accumulated text from the message field.
			msgRaw, ok := event["message"]
			if !ok {
				t.Fatalf("partial assistant_message missing message field: %v", event)
			}
			msgMap, ok := msgRaw.(map[string]any)
			if !ok {
				t.Fatalf("message field is not an object: %v", msgRaw)
			}
			// Text is in content[0].text.
			content, _ := msgMap["content"].([]any)
			text := ""
			if len(content) > 0 {
				block, _ := content[0].(map[string]any)
				text, _ = block["text"].(string)
			}
			out = append(out, text)
		}
	}
	return out
}
