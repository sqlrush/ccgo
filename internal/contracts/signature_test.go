package contracts

import (
	"encoding/json"
	"strings"
	"testing"
)

func TestContentBlockSignatureRoundTrip(t *testing.T) {
	in := `{"type":"thinking","thinking":"reasoning","signature":"abc123"}`
	var block ContentBlock
	if err := json.Unmarshal([]byte(in), &block); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if block.Type != ContentThinking {
		t.Fatalf("type = %q want thinking", block.Type)
	}
	if block.Signature != "abc123" {
		t.Fatalf("signature = %q want abc123", block.Signature)
	}
	out, err := json.Marshal(block)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	var round map[string]any
	if err := json.Unmarshal(out, &round); err != nil {
		t.Fatalf("re-unmarshal: %v", err)
	}
	if round["signature"] != "abc123" {
		t.Fatalf("marshalled signature = %v want abc123", round["signature"])
	}
}

func TestContentBlockSignatureOmittedWhenEmpty(t *testing.T) {
	out, err := json.Marshal(ContentBlock{Type: ContentText, Text: "hi"})
	if err != nil {
		t.Fatal(err)
	}
	var round map[string]any
	if err := json.Unmarshal(out, &round); err != nil {
		t.Fatal(err)
	}
	if _, ok := round["signature"]; ok {
		t.Fatalf("signature should be omitted when empty: %s", out)
	}
}

// TestThinkingBlockUnmarshalsReasoningIntoText verifies that the "thinking" JSON
// key (not "text") is mapped into block.Text when the block type is thinking.
func TestThinkingBlockUnmarshalsReasoningIntoText(t *testing.T) {
	in := `{"type":"thinking","thinking":"reasoning here","signature":"abc"}`
	var block ContentBlock
	if err := json.Unmarshal([]byte(in), &block); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if block.Type != ContentThinking {
		t.Fatalf("type = %q, want thinking", block.Type)
	}
	if block.Text != "reasoning here" {
		t.Fatalf("Text = %q, want %q", block.Text, "reasoning here")
	}
	if block.Signature != "abc" {
		t.Fatalf("Signature = %q, want %q", block.Signature, "abc")
	}
}

// TestThinkingBlockMarshalsReasoningUnderThinkingKey verifies that marshaling a
// thinking block emits the reasoning under the "thinking" key, not "text".
func TestThinkingBlockMarshalsReasoningUnderThinkingKey(t *testing.T) {
	block := ContentBlock{
		Type:      ContentThinking,
		Text:      "reasoning here",
		Signature: "abc",
	}
	out, err := json.Marshal(block)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	outStr := string(out)

	var m map[string]any
	if err := json.Unmarshal(out, &m); err != nil {
		t.Fatalf("re-unmarshal: %v", err)
	}
	if m["thinking"] != "reasoning here" {
		t.Fatalf("thinking key = %v, want %q (got JSON: %s)", m["thinking"], "reasoning here", outStr)
	}
	if m["signature"] != "abc" {
		t.Fatalf("signature = %v, want %q", m["signature"], "abc")
	}
	if strings.Contains(outStr, `"text":`) {
		t.Fatalf("JSON must not contain a \"text\" key for thinking blocks, got: %s", outStr)
	}
}

// TestNonThinkingBlocksMarshalUnchanged guards against regressions introduced
// by adding a custom MarshalJSON: non-thinking block types must still emit the
// same JSON keys they did before (i.e. text blocks use "text", tool_use blocks
// use "input", tool_result blocks use "content", image blocks use "source").
func TestNonThinkingBlocksMarshalUnchanged(t *testing.T) {
	cases := []struct {
		name     string
		block    ContentBlock
		wantKeys []string
		wantMiss []string
	}{
		{
			name:     "text block",
			block:    ContentBlock{Type: ContentText, Text: "hello"},
			wantKeys: []string{"type", "text"},
			wantMiss: []string{"thinking"},
		},
		{
			name:     "tool_use block",
			block:    ContentBlock{Type: ContentToolUse, ID: "tu_1", Name: "bash", Input: json.RawMessage(`{"cmd":"ls"}`)},
			wantKeys: []string{"type", "id", "name", "input"},
			wantMiss: []string{"thinking", "text"},
		},
		{
			name:     "tool_result block",
			block:    ContentBlock{Type: ContentToolResult, ToolUseID: "tu_1", Content: "output"},
			wantKeys: []string{"type", "tool_use_id", "content"},
			wantMiss: []string{"thinking"},
		},
		{
			name:     "image block",
			block:    ContentBlock{Type: ContentImage, Source: ImageSource{Type: "base64", MediaType: "image/png", Data: "abc="}},
			wantKeys: []string{"type", "source"},
			wantMiss: []string{"thinking", "text"},
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			out, err := json.Marshal(tc.block)
			if err != nil {
				t.Fatalf("marshal: %v", err)
			}
			var m map[string]any
			if err := json.Unmarshal(out, &m); err != nil {
				t.Fatalf("re-unmarshal: %v", err)
			}
			for _, key := range tc.wantKeys {
				if _, ok := m[key]; !ok {
					t.Errorf("missing expected key %q in JSON: %s", key, out)
				}
			}
			for _, key := range tc.wantMiss {
				if _, ok := m[key]; ok {
					t.Errorf("unexpected key %q present in JSON: %s", key, out)
				}
			}
		})
	}
}
