package contracts

import (
	"encoding/json"
	"testing"
)

func TestMessageUnmarshalAcceptsStringContent(t *testing.T) {
	var message Message
	if err := json.Unmarshal([]byte(`{"role":"User","id":"m1","content":"hello"}`), &message); err != nil {
		t.Fatal(err)
	}
	if message.Type != MessageUser || message.ID != "m1" {
		t.Fatalf("message metadata = %#v", message)
	}
	if len(message.Content) != 1 || message.Content[0].Type != ContentText || message.Content[0].Text != "hello" {
		t.Fatalf("content = %#v", message.Content)
	}
}

func TestMessageUnmarshalAcceptsSingleContentBlock(t *testing.T) {
	var message Message
	if err := json.Unmarshal([]byte(`{"messageType":"assistant","content":{"type":"text","text":"hello"}}`), &message); err != nil {
		t.Fatal(err)
	}
	if message.Type != MessageAssistant {
		t.Fatalf("message type = %q", message.Type)
	}
	if len(message.Content) != 1 || message.Content[0].Type != ContentText || message.Content[0].Text != "hello" {
		t.Fatalf("content = %#v", message.Content)
	}
}

func TestMessageUnmarshalAcceptsMixedStringContentArray(t *testing.T) {
	var message Message
	if err := json.Unmarshal([]byte(`{"type":"assistant","content":["hello",{"type":"text","text":" world"}]}`), &message); err != nil {
		t.Fatal(err)
	}
	if len(message.Content) != 2 ||
		message.Content[0].Type != ContentText || message.Content[0].Text != "hello" ||
		message.Content[1].Type != ContentText || message.Content[1].Text != " world" {
		t.Fatalf("content = %#v", message.Content)
	}
}

func TestMessageUnmarshalAcceptsTextBodyAliases(t *testing.T) {
	for name, raw := range map[string]string{
		"text":    `{"role":"assistant","text":"from text"}`,
		"body":    `{"role":"assistant","body":"from body"}`,
		"message": `{"role":"assistant","message":"from message"}`,
		"value":   `{"role":"assistant","value":"from value"}`,
		"output":  `{"role":"assistant","output":"from output"}`,
		"null":    `{"role":"assistant","content":null,"text":"from null fallback"}`,
	} {
		t.Run(name, func(t *testing.T) {
			var message Message
			if err := json.Unmarshal([]byte(raw), &message); err != nil {
				t.Fatal(err)
			}
			if message.Type != MessageAssistant || len(message.Content) != 1 || message.Content[0].Text == "" {
				t.Fatalf("message = %#v", message)
			}
		})
	}
}

func TestContentBlockUnmarshalAcceptsTextAliases(t *testing.T) {
	for name, raw := range map[string]string{
		"body":         `{"type":"text","body":"from body"}`,
		"message":      `{"type":"text","message":"from message"}`,
		"value":        `{"type":"text","value":"from value"}`,
		"output":       `{"type":"text","output":"from output"}`,
		"contentText":  `{"type":"text","contentText":"from contentText"}`,
		"content_text": `{"type":"text","content_text":"from content_text"}`,
		"content":      `{"type":"text","content":"from content"}`,
		"thinking":     `{"type":"thinking","content":"from thinking"}`,
		"default_type": `{"body":"from default"}`,
	} {
		t.Run(name, func(t *testing.T) {
			var block ContentBlock
			if err := json.Unmarshal([]byte(raw), &block); err != nil {
				t.Fatal(err)
			}
			if block.Text == "" {
				t.Fatalf("block = %#v", block)
			}
			if name == "default_type" && block.Type != ContentText {
				t.Fatalf("default type = %q", block.Type)
			}
			if name == "thinking" && block.Type != ContentThinking {
				t.Fatalf("thinking type = %q", block.Type)
			}
		})
	}
}

func TestContentBlockUnmarshalAcceptsTypeAliases(t *testing.T) {
	var message Message
	if err := json.Unmarshal([]byte(`{"role":"assistant","content":[
		{"type":"toolUse","id":"toolu_1","name":"Read"},
		{"type":"tool-result","toolUseId":"toolu_1","content":"ok"},
		{"type":"cacheEdits","cacheReference":"cache_1"},
		{"type":"inputImage","mimeType":"image/png","base64":"AAAA"},
		{"type":"chain-of-thought","content":"reasoning"}
	]}`), &message); err != nil {
		t.Fatal(err)
	}
	if len(message.Content) != 5 {
		t.Fatalf("content = %#v", message.Content)
	}
	if message.Content[0].Type != ContentToolUse || message.Content[0].ID != "toolu_1" {
		t.Fatalf("tool use = %#v", message.Content[0])
	}
	if message.Content[1].Type != ContentToolResult || message.Content[1].ToolUseID != "toolu_1" {
		t.Fatalf("tool result = %#v", message.Content[1])
	}
	if message.Content[2].Type != ContentCacheEdits || message.Content[2].CacheReference != "cache_1" {
		t.Fatalf("cache edits = %#v", message.Content[2])
	}
	source, ok := message.Content[3].Source.(ImageSource)
	if message.Content[3].Type != ContentImage || !ok || source.MediaType != "image/png" || source.Data != "AAAA" {
		t.Fatalf("image = %#v source=%#v", message.Content[3], message.Content[3].Source)
	}
	if message.Content[4].Type != ContentThinking || message.Content[4].Text != "reasoning" {
		t.Fatalf("thinking = %#v", message.Content[4])
	}
}

func TestMessageUnmarshalAcceptsContentBlockTextAliases(t *testing.T) {
	var message Message
	if err := json.Unmarshal([]byte(`{"role":"assistant","content":[{"type":"text","body":"hello"},{"type":"thinking","content":"reasoning"}]}`), &message); err != nil {
		t.Fatal(err)
	}
	if len(message.Content) != 2 ||
		message.Content[0].Type != ContentText || message.Content[0].Text != "hello" ||
		message.Content[1].Type != ContentThinking || message.Content[1].Text != "reasoning" {
		t.Fatalf("content = %#v", message.Content)
	}
}

func TestImageSourceUnmarshalAcceptsAliases(t *testing.T) {
	var source ImageSource
	if err := json.Unmarshal([]byte(`{"kind":"base64","mimeType":"image/jpeg","base64":"AAAA"}`), &source); err != nil {
		t.Fatal(err)
	}
	if source.Type != "base64" || source.MediaType != "image/jpeg" || source.Data != "AAAA" {
		t.Fatalf("source = %#v", source)
	}
}

func TestContentBlockUnmarshalNormalizesImageSourceAliases(t *testing.T) {
	var block ContentBlock
	if err := json.Unmarshal([]byte(`{"type":"image","source":{"kind":"base64","contentType":"image/webp","payload":"BBBB"}}`), &block); err != nil {
		t.Fatal(err)
	}
	source, ok := block.Source.(ImageSource)
	if !ok || source.Type != "base64" || source.MediaType != "image/webp" || source.Data != "BBBB" {
		t.Fatalf("source = %#v", block.Source)
	}
}

func TestContentBlockUnmarshalAcceptsTopLevelImageSourceAliases(t *testing.T) {
	var block ContentBlock
	if err := json.Unmarshal([]byte(`{"type":"image","mimeType":"image/png","base64":"CCCC"}`), &block); err != nil {
		t.Fatal(err)
	}
	source, ok := block.Source.(ImageSource)
	if !ok || source.Type != "base64" || source.MediaType != "image/png" || source.Data != "CCCC" {
		t.Fatalf("source = %#v", block.Source)
	}
}

func TestMessageUnmarshalAcceptsImageContentBlockAliases(t *testing.T) {
	var message Message
	if err := json.Unmarshal([]byte(`{"role":"user","content":[{"type":"image","source":{"mimeType":"image/png","base64":"DDDD"}}]}`), &message); err != nil {
		t.Fatal(err)
	}
	if len(message.Content) != 1 || message.Content[0].Type != ContentImage {
		t.Fatalf("content = %#v", message.Content)
	}
	source, ok := message.Content[0].Source.(ImageSource)
	if !ok || source.Type != "base64" || source.MediaType != "image/png" || source.Data != "DDDD" {
		t.Fatalf("source = %#v", message.Content[0].Source)
	}
}
