package anthropic

import (
	"testing"

	"ccgo/internal/contracts"
)

func TestAccumulatorCollectsThinkingAndSignature(t *testing.T) {
	acc := NewStreamAccumulator()
	mustAdd := func(e StreamEvent) {
		if err := acc.Add(e); err != nil {
			t.Fatalf("Add(%s): %v", e.Type, err)
		}
	}
	mustAdd(StreamEvent{Type: "message_start", Message: &Response{Model: "claude-sonnet-4-6"}})
	mustAdd(StreamEvent{
		Type:         "content_block_start",
		Index:        0,
		ContentBlock: &contracts.ContentBlock{Type: contracts.ContentThinking},
	})
	mustAdd(StreamEvent{Type: "content_block_delta", Index: 0, Delta: map[string]any{"type": "thinking_delta", "thinking": "Let me "}})
	mustAdd(StreamEvent{Type: "content_block_delta", Index: 0, Delta: map[string]any{"type": "thinking_delta", "thinking": "think."}})
	mustAdd(StreamEvent{Type: "content_block_delta", Index: 0, Delta: map[string]any{"type": "signature_delta", "signature": "SIG=="}})
	mustAdd(StreamEvent{Type: "content_block_stop", Index: 0})

	resp := acc.Finish()
	if len(resp.Content) != 1 {
		t.Fatalf("content len = %d want 1", len(resp.Content))
	}
	block := resp.Content[0]
	if block.Type != contracts.ContentThinking {
		t.Fatalf("type = %q want thinking", block.Type)
	}
	if block.Text != "Let me think." {
		t.Fatalf("thinking text = %q want %q", block.Text, "Let me think.")
	}
	if block.Signature != "SIG==" {
		t.Fatalf("signature = %q want SIG==", block.Signature)
	}
}
