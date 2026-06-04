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
