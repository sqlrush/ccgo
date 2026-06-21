package contracts

import (
	"encoding/json"
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
