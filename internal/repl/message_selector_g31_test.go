package repl

// REPL-30: ESC double-press → MessageSelector overlay tests.
// CC ref: src/components/PromptInput/PromptInput.tsx:1254.

import (
	"strings"
	"testing"

	"ccgo/internal/contracts"
	"ccgo/internal/tui"
)

// ─── MessageSelectorOverlay unit tests ────────────────────────────────────────

func TestMessageSelectorRenderContainsEntries(t *testing.T) {
	entries := []string{"Hello from user", "Hello from assistant", "Another message"}
	overlay := NewMessageSelectorOverlay(entries)
	lines := overlay.Render(80, 24)
	joined := strings.Join(lines, "\n")
	for _, entry := range entries {
		if !strings.Contains(joined, entry) {
			t.Fatalf("Render output missing entry %q:\n%s", entry, joined)
		}
	}
}

func TestMessageSelectorRenderEmptyEntries(t *testing.T) {
	overlay := NewMessageSelectorOverlay(nil)
	lines := overlay.Render(80, 24)
	joined := strings.Join(lines, "\n")
	if !strings.Contains(joined, "no messages") {
		t.Fatalf("Render with no entries should mention 'no messages': %q", joined)
	}
}

func TestMessageSelectorNavigateAndConfirm(t *testing.T) {
	entries := []string{"msg-one", "msg-two", "msg-three"}
	overlay := NewMessageSelectorOverlay(entries)

	// Move down to entry index 1.
	overlay.ApplyKey(tui.Key{Type: tui.KeyDown})
	res, handled := overlay.ApplyKey(tui.Key{Type: tui.KeyEnter})
	if !handled {
		t.Fatal("Enter should be handled")
	}
	want := "rewind:msg-two"
	if res.Submit != want {
		t.Fatalf("Enter at cursor 1: Submit = %q, want %q", res.Submit, want)
	}
}

func TestMessageSelectorDefaultSubmitFirst(t *testing.T) {
	entries := []string{"first", "second"}
	overlay := NewMessageSelectorOverlay(entries)
	res, _ := overlay.ApplyKey(tui.Key{Type: tui.KeyEnter})
	if res.Submit != "rewind:first" {
		t.Fatalf("default Enter: Submit = %q, want rewind:first", res.Submit)
	}
}

func TestMessageSelectorEscDismisses(t *testing.T) {
	overlay := NewMessageSelectorOverlay([]string{"x"})
	res, handled := overlay.ApplyKey(tui.Key{Type: tui.KeyEsc})
	if !handled {
		t.Fatal("Esc should be handled")
	}
	if !res.Dismissed {
		t.Fatalf("Esc should set Dismissed: %+v", res)
	}
}

func TestMessageSelectorCursorClamps(t *testing.T) {
	overlay := NewMessageSelectorOverlay([]string{"a", "b"})
	// Move up at index 0 — should clamp.
	overlay.ApplyKey(tui.Key{Type: tui.KeyUp})
	if overlay.cursor != 0 {
		t.Fatalf("cursor clamped below 0: got %d", overlay.cursor)
	}
	// Move down past last entry.
	overlay.ApplyKey(tui.Key{Type: tui.KeyDown})
	overlay.ApplyKey(tui.Key{Type: tui.KeyDown})
	if overlay.cursor != 1 {
		t.Fatalf("cursor clamped above max: got %d", overlay.cursor)
	}
}

// ─── ESC double-press integration test ───────────────────────────────────────

// buildLoopWithHistory creates a Loop that has history messages seeded so the
// ESC double-press can open the MessageSelectorOverlay.
func buildLoopWithHistory(t *testing.T) *Loop {
	t.Helper()
	ft := NewFakeTerminal("", 80, 24)
	l := NewLoop(ft, nil)
	// Inject one user and one assistant message so historyToScreen produces entries.
	l.history = []contracts.Message{
		{
			Type: contracts.MessageUser,
			Content: []contracts.ContentBlock{
				{Type: contracts.ContentText, Text: "user text"},
			},
		},
		{
			Type: contracts.MessageAssistant,
			Content: []contracts.ContentBlock{
				{Type: contracts.ContentText, Text: "assistant reply"},
			},
		},
	}
	return l
}

func TestEscDoublePressWith800msWindowOpensOverlay(t *testing.T) {
	l := buildLoopWithHistory(t)

	// First ESC: records time, no overlay yet.
	l.handleKey(tui.Key{Type: tui.KeyEsc})
	if l.activeOverlay != nil {
		t.Fatal("first ESC should not open overlay")
	}
	if l.lastEscTime.IsZero() {
		t.Fatal("first ESC should record lastEscTime")
	}

	// Second ESC immediately after: within window → should open overlay.
	l.handleKey(tui.Key{Type: tui.KeyEsc})
	if l.activeOverlay == nil {
		t.Fatal("second ESC within window should open MessageSelectorOverlay")
	}
	if _, ok := l.activeOverlay.(*MessageSelectorOverlay); !ok {
		t.Fatalf("overlay type = %T, want *MessageSelectorOverlay", l.activeOverlay)
	}
}

func TestEscSinglePressDoesNotOpenOverlay(t *testing.T) {
	l := buildLoopWithHistory(t)
	l.handleKey(tui.Key{Type: tui.KeyEsc})
	if l.activeOverlay != nil {
		t.Fatal("single ESC should not open overlay")
	}
}

func TestEscDoublePressWithNonEmptyPromptDoesNotOpenOverlay(t *testing.T) {
	l := buildLoopWithHistory(t)
	// Pre-populate prompt text.
	l.screen.Prompt.Text = "some text"
	// Two ESCs with a non-empty prompt should not open overlay.
	l.handleKey(tui.Key{Type: tui.KeyEsc})
	l.handleKey(tui.Key{Type: tui.KeyEsc})
	if _, ok := l.activeOverlay.(*MessageSelectorOverlay); ok {
		t.Fatal("ESC double-press with non-empty prompt should not open MessageSelector")
	}
}
