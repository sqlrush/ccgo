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
