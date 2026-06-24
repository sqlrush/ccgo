package repl

// REPL-52: Image paste handling via KeyImageHint.
//
// KeyImageHint is handled by tui.PromptState.Apply (input.go:819-820), which
// calls insertImageHint. Because loop.handleKey routes all keys through
// l.screen.ApplyKey → PromptState.Apply, the image hint path is ALREADY wired.
//
// This test verifies that path end-to-end at the PromptState level and via the
// Loop's screen (which uses paste-references mode).

import (
	"strings"
	"testing"

	"ccgo/internal/tui"
)

// TestImageHintInsertsPlaceholder verifies that applying a KeyImageHint key
// with non-empty Text inserts that text into the prompt (without paste refs).
func TestImageHintInsertsPlaceholder(t *testing.T) {
	p := tui.NewPromptState(nil)
	// UsePasteReferences is false by default on a plain PromptState.
	key := tui.Key{Type: tui.KeyImageHint, Text: "[Image]"}
	p.Apply(key)
	if !strings.Contains(p.Text, "[Image]") {
		t.Fatalf("prompt text after KeyImageHint = %q; want to contain [Image]", p.Text)
	}
}

// TestImageHintFallsBackToPlaceholder verifies that a KeyImageHint with empty
// Text inserts the default ImageHintPlaceholder constant ("[Image]").
func TestImageHintFallsBackToPlaceholder(t *testing.T) {
	p := tui.NewPromptState(nil)
	key := tui.Key{Type: tui.KeyImageHint, Text: ""}
	p.Apply(key)
	if !strings.Contains(p.Text, tui.ImageHintPlaceholder) {
		t.Fatalf("prompt text = %q; want to contain %q", p.Text, tui.ImageHintPlaceholder)
	}
}

// TestImageHintViaLoopScreen verifies the full path: loop.handleKey routes
// KeyImageHint through screen.ApplyKey → PromptState.Apply → insertImageHint.
// The Loop's REPLScreen uses paste-reference mode, so the inserted text is
// rendered as "[Image #N]" rather than the raw placeholder "[Image]". We check
// for the "[Image" prefix which covers both forms.
// REPL-52 is ✅ (already wired via screen.ApplyKey).
func TestImageHintViaLoopScreen(t *testing.T) {
	ft := NewFakeTerminal("", 80, 24)
	l := NewLoop(ft, nil)
	l.handleKey(tui.Key{Type: tui.KeyImageHint, Text: "[Image]"})
	// In paste-reference mode the text becomes "[Image #1]"; plain mode gives
	// "[Image]". Either way the prefix "[Image" must be present.
	if !strings.Contains(l.screen.Prompt.Text, "[Image") {
		t.Fatalf("loop prompt text after KeyImageHint = %q; want to contain [Image...", l.screen.Prompt.Text)
	}
}
