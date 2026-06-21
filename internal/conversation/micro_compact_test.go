package conversation

import (
	"testing"

	"ccgo/internal/contracts"
	msgs "ccgo/internal/messages"
)

// microCompactableHistory builds a history with enough messages so that
// MicroCompact (KeepLast=1) summarizes the first N-1 messages.
// MicroCompact summarizes history[:len(history)-keepLast], so with KeepLast=1
// and 4 messages, 3 messages are summarized (MessagesSummarized=3).
func microCompactableHistory(t *testing.T, n int) []contracts.Message {
	t.Helper()
	if n < 2 {
		t.Fatal("microCompactableHistory: n must be >= 2 so at least one message is summarized")
	}
	history := make([]contracts.Message, 0, n)
	for i := 0; i < n; i++ {
		if i%2 == 0 {
			history = append(history, msgs.UserText("user message"))
		} else {
			history = append(history, msgs.AssistantText("assistant reply", "sonnet", nil))
		}
	}
	return history
}

func TestMaybeMicroCompactDisabledNoop(t *testing.T) {
	r := Runner{} // EnableMicroCompact is false (zero value)
	history := []contracts.Message{
		{Type: contracts.MessageUser, Content: []contracts.ContentBlock{contracts.NewTextBlock("a")}},
	}
	out, result, ok := r.maybeMicroCompact(history)
	if ok || result != nil {
		t.Fatalf("disabled micro-compact must be a no-op; ok=%v result=%v", ok, result)
	}
	if len(out) != len(history) {
		t.Fatalf("history changed while disabled: %d -> %d", len(history), len(out))
	}
	// Must return the same slice (no copy when disabled)
	if &out[0] != &history[0] {
		t.Fatal("disabled micro-compact must return the original slice")
	}
}

func TestMaybeMicroCompactEmptyHistoryNoop(t *testing.T) {
	r := Runner{EnableMicroCompact: true, MicroCompactKeepLast: 1}
	out, result, ok := r.maybeMicroCompact(nil)
	if ok || result != nil {
		t.Fatalf("empty history must be a no-op; ok=%v result=%v", ok, result)
	}
	if out != nil {
		t.Fatalf("expected nil history, got %v", out)
	}
}

func TestMaybeMicroCompactRunsWhenEnabled(t *testing.T) {
	// KeepLast=1 means the last message is kept; all prior are summarized.
	r := Runner{EnableMicroCompact: true, MicroCompactKeepLast: 1}
	history := microCompactableHistory(t, 4) // 4 messages, 3 summarized
	out, result, ok := r.maybeMicroCompact(history)
	if !ok || result == nil {
		t.Fatalf("expected micro-compact to run; ok=%v result=%v", ok, result)
	}
	// Result must record how many messages were summarized
	if result.MessagesSummarized == 0 {
		t.Fatalf("MessagesSummarized = 0, want > 0")
	}
	// Output history must not grow
	if len(out) > len(history) {
		t.Fatalf("micro-compact must not grow history: %d -> %d", len(history), len(out))
	}
	// Output history must be shorter than input (boundary+summary replace summarized messages)
	// Output = [boundary, summary, ...kept] so len = 2 + KeepLast = 3
	if len(out) >= len(history) {
		t.Fatalf("micro-compact should shorten history: in=%d out=%d", len(history), len(out))
	}
}

func TestMaybeMicroCompactDoesNotMutateInput(t *testing.T) {
	r := Runner{EnableMicroCompact: true, MicroCompactKeepLast: 1}
	history := microCompactableHistory(t, 4)
	origLen := len(history)
	origFirst := history[0].UUID
	_, _, _ = r.maybeMicroCompact(history)
	if len(history) != origLen {
		t.Fatalf("input history length changed: %d -> %d", origLen, len(history))
	}
	if history[0].UUID != origFirst {
		t.Fatal("input history was mutated")
	}
}

func TestMaybeMicroCompactKeepLastAll(t *testing.T) {
	// When KeepLast >= len(history), nothing is summarized.
	r := Runner{EnableMicroCompact: true, MicroCompactKeepLast: 10}
	history := microCompactableHistory(t, 4)
	out, result, ok := r.maybeMicroCompact(history)
	if ok || result != nil {
		t.Fatalf("keeping all messages must be a no-op; ok=%v result=%v", ok, result)
	}
	if len(out) != len(history) {
		t.Fatalf("history changed when nothing to summarize: %d -> %d", len(history), len(out))
	}
}
