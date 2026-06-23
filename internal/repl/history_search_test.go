package repl

import (
	"strings"
	"testing"

	"ccgo/internal/tui"
)

// ── construction ─────────────────────────────────────────────────────────────

func TestHistorySearchOverlayConstruction(t *testing.T) {
	entries := []string{"hello world", "fix the bug", "write tests"}
	o := NewHistorySearchOverlay(entries)
	// With no query all entries are visible.
	if len(o.filtered) != 3 {
		t.Errorf("expected 3 filtered items, got %d", len(o.filtered))
	}
}

func TestHistorySearchOverlaySkipsEmpty(t *testing.T) {
	entries := []string{"hello", "", "world", ""}
	o := NewHistorySearchOverlay(entries)
	if len(o.all) != 2 {
		t.Errorf("expected 2 non-empty items, got %d", len(o.all))
	}
}

func TestHistorySearchOverlayEmpty(t *testing.T) {
	o := NewHistorySearchOverlay(nil)
	if len(o.filtered) != 0 {
		t.Errorf("expected 0 items")
	}
}

// ── filtering ─────────────────────────────────────────────────────────────────

func TestHistorySearchOverlayExactFilter(t *testing.T) {
	entries := []string{"fix the bug", "write tests", "deploy to prod"}
	o := NewHistorySearchOverlay(entries)
	for _, r := range "fix" {
		o.ApplyKey(tui.Key{Type: tui.KeyRune, Rune: r})
	}
	if len(o.filtered) != 1 || o.filtered[0].firstLine != "fix the bug" {
		t.Errorf("expected only 'fix the bug', got %v", o.filtered)
	}
}

func TestHistorySearchOverlaySubsequenceFilter(t *testing.T) {
	entries := []string{"hello world", "fix the bug"}
	o := NewHistorySearchOverlay(entries)
	// "hw" is a subsequence of "hello world" (h then w in order)
	for _, r := range "hw" {
		o.ApplyKey(tui.Key{Type: tui.KeyRune, Rune: r})
	}
	found := false
	for _, item := range o.filtered {
		if strings.Contains(item.display, "hello world") {
			found = true
		}
	}
	if !found {
		t.Errorf("expected subsequence match of 'hello world' for 'hw', got %v", o.filtered)
	}
}

func TestHistorySearchOverlayEmptyQueryShowsAll(t *testing.T) {
	entries := []string{"a", "b", "c"}
	o := NewHistorySearchOverlay(entries)
	if len(o.filtered) != 3 {
		t.Errorf("expected all 3 items with no query, got %d", len(o.filtered))
	}
}

// ── cursor navigation ─────────────────────────────────────────────────────────

func TestHistorySearchOverlayCursorMovement(t *testing.T) {
	entries := []string{"a", "b", "c"}
	o := NewHistorySearchOverlay(entries)
	o.ApplyKey(tui.Key{Type: tui.KeyDown})
	if o.cursor != 1 {
		t.Errorf("expected cursor 1, got %d", o.cursor)
	}
	o.ApplyKey(tui.Key{Type: tui.KeyUp})
	if o.cursor != 0 {
		t.Errorf("expected cursor 0, got %d", o.cursor)
	}
}

func TestHistorySearchOverlayCursorClampedAtBounds(t *testing.T) {
	entries := []string{"a"}
	o := NewHistorySearchOverlay(entries)
	// Up at 0 → stays 0
	o.ApplyKey(tui.Key{Type: tui.KeyUp})
	if o.cursor != 0 {
		t.Errorf("expected cursor stay 0, got %d", o.cursor)
	}
	// Down at last → stays at last
	o.ApplyKey(tui.Key{Type: tui.KeyDown})
	if o.cursor != 0 {
		t.Errorf("expected cursor stay 0, got %d", o.cursor)
	}
}

// ── enter selects ─────────────────────────────────────────────────────────────

func TestHistorySearchOverlayEnterSelects(t *testing.T) {
	entries := []string{"fix the bug", "deploy to prod"}
	o := NewHistorySearchOverlay(entries)
	res, handled := o.ApplyKey(tui.Key{Type: tui.KeyEnter})
	if !handled {
		t.Fatal("expected handled")
	}
	if !strings.HasPrefix(res.Submit, "historysearch:") {
		t.Errorf("expected historysearch: prefix, got %q", res.Submit)
	}
	if !strings.Contains(res.Submit, "fix the bug") {
		t.Errorf("expected 'fix the bug' in submit, got %q", res.Submit)
	}
}

func TestHistorySearchOverlayEnterEmptyDismisses(t *testing.T) {
	o := NewHistorySearchOverlay(nil)
	res, _ := o.ApplyKey(tui.Key{Type: tui.KeyEnter})
	if !res.Dismissed {
		t.Errorf("expected Dismissed when empty, got %+v", res)
	}
}

// ── esc dismisses ────────────────────────────────────────────────────────────

func TestHistorySearchOverlayEscDismisses(t *testing.T) {
	o := NewHistorySearchOverlay([]string{"some entry"})
	res, handled := o.ApplyKey(tui.Key{Type: tui.KeyEsc})
	if !handled || !res.Dismissed {
		t.Errorf("expected Dismissed, got %+v handled=%v", res, handled)
	}
}

// ── backspace ─────────────────────────────────────────────────────────────────

func TestHistorySearchOverlayBackspace(t *testing.T) {
	o := NewHistorySearchOverlay([]string{"hello"})
	for _, r := range "hel" {
		o.ApplyKey(tui.Key{Type: tui.KeyRune, Rune: r})
	}
	o.ApplyKey(tui.Key{Type: tui.KeyBackspace})
	if o.Query() != "he" {
		t.Errorf("expected query 'he' after backspace, got %q", o.Query())
	}
}

// ── multiline display: enter uses first line ──────────────────────────────────

func TestHistorySearchOverlayMultilineFirstLine(t *testing.T) {
	entries := []string{"first line\nsecond line\nthird line"}
	o := NewHistorySearchOverlay(entries)
	if o.all[0].firstLine != "first line" {
		t.Errorf("expected 'first line', got %q", o.all[0].firstLine)
	}
}

// ── render smoke ─────────────────────────────────────────────────────────────

func TestHistorySearchOverlayRender(t *testing.T) {
	entries := []string{"fix the bug", "write tests"}
	o := NewHistorySearchOverlay(entries)
	lines := o.Render(80, 10)
	if len(lines) == 0 {
		t.Fatal("expected non-empty render")
	}
	found := false
	for _, l := range lines {
		if strings.Contains(l, "fix the bug") {
			found = true
		}
	}
	if !found {
		t.Errorf("expected 'fix the bug' in render, got %v", lines)
	}
}

// ── handleHistorySearchSubmit ─────────────────────────────────────────────────

func TestHandleHistorySearchSubmitSingleLine(t *testing.T) {
	prompt := ""
	if !handleHistorySearchSubmit(&prompt, "historysearch:fix the bug") {
		t.Fatal("expected true")
	}
	if prompt != "fix the bug" {
		t.Errorf("expected 'fix the bug', got %q", prompt)
	}
}

func TestHandleHistorySearchSubmitMultilineUsesFirstLine(t *testing.T) {
	prompt := ""
	if !handleHistorySearchSubmit(&prompt, "historysearch:first line\nsecond line") {
		t.Fatal("expected true")
	}
	if prompt != "first line" {
		t.Errorf("expected 'first line', got %q", prompt)
	}
}

func TestHandleHistorySearchSubmitIgnoresUnknown(t *testing.T) {
	prompt := ""
	if handleHistorySearchSubmit(&prompt, "resume:abc") {
		t.Fatal("expected false")
	}
}

// ── loop wiring: SetCWD / SetPromptHistory ────────────────────────────────────

func TestLoopSetCWD(t *testing.T) {
	term := NewFakeTerminal("", 80, 24)
	l := NewLoop(term, nil)
	l.SetCWD("/tmp/project")
	if l.cwd != "/tmp/project" {
		t.Errorf("expected cwd '/tmp/project', got %q", l.cwd)
	}
}

func TestLoopSetPromptHistory(t *testing.T) {
	term := NewFakeTerminal("", 80, 24)
	l := NewLoop(term, nil)
	entries := []string{"hello", "world"}
	l.SetPromptHistory(entries)
	if len(l.promptHistoryEntries) != 2 {
		t.Errorf("expected 2 entries, got %d", len(l.promptHistoryEntries))
	}
	// Mutation of original must not affect loop state.
	entries[0] = "mutated"
	if l.promptHistoryEntries[0] != "hello" {
		t.Errorf("expected immutable copy, got %q", l.promptHistoryEntries[0])
	}
}

// ── loop wiring: @ trigger opens QuickOpen ────────────────────────────────────

func TestLoopAtTriggerOpensQuickOpen(t *testing.T) {
	term := NewFakeTerminal("", 80, 24)
	l := NewLoop(term, nil)
	l.SetCWD("/tmp")
	// Simulate typing "@" which should trigger QuickOpen after screen.ApplyKey.
	l.screen.Prompt.Text = ""
	l.screen.Prompt.Cursor = 0
	// Directly type "@" via handleKey.
	l.handleKey(tui.Key{Type: tui.KeyRune, Rune: '@'})
	if _, ok := l.activeOverlay.(*QuickOpenOverlay); !ok {
		t.Errorf("expected QuickOpenOverlay after '@', got %T", l.activeOverlay)
	}
	// Prompt should be cleared (the @ was consumed by the overlay).
	if l.screen.Prompt.Text != "" {
		t.Errorf("expected prompt cleared, got %q", l.screen.Prompt.Text)
	}
}

func TestLoopAtTriggerNoOpWithoutCWD(t *testing.T) {
	term := NewFakeTerminal("", 80, 24)
	l := NewLoop(term, nil)
	// No CWD set → no QuickOpen overlay.
	l.handleKey(tui.Key{Type: tui.KeyRune, Rune: '@'})
	if l.activeOverlay != nil {
		t.Errorf("expected no overlay when cwd not set, got %T", l.activeOverlay)
	}
}

// ── loop wiring: Ctrl+Q opens HistorySearch ───────────────────────────────────

func TestLoopCtrlQOpensHistorySearch(t *testing.T) {
	term := NewFakeTerminal("", 80, 24)
	l := NewLoop(term, nil)
	l.SetPromptHistory([]string{"hello", "world"})
	l.handleKey(tui.Key{Type: tui.KeyCtrlQ})
	if _, ok := l.activeOverlay.(*HistorySearchOverlay); !ok {
		t.Errorf("expected HistorySearchOverlay after Ctrl+Q, got %T", l.activeOverlay)
	}
}

func TestLoopCtrlQNoOpWithoutHistory(t *testing.T) {
	term := NewFakeTerminal("", 80, 24)
	l := NewLoop(term, nil)
	// No history → no overlay.
	l.handleKey(tui.Key{Type: tui.KeyCtrlQ})
	if l.activeOverlay != nil {
		t.Errorf("expected no overlay when history empty, got %T", l.activeOverlay)
	}
}

// ── handleOverlaySubmit: quickopen routes ────────────────────────────────────

func TestLoopHandleOverlaySubmitQuickOpen(t *testing.T) {
	term := NewFakeTerminal("", 80, 24)
	l := NewLoop(term, nil)
	handled := l.handleOverlaySubmit("quickopen:internal/foo.go")
	if !handled {
		t.Fatal("expected handled=true for quickopen:")
	}
	if l.screen.Prompt.Text != "@internal/foo.go " {
		t.Errorf("expected '@internal/foo.go ', got %q", l.screen.Prompt.Text)
	}
}

func TestLoopHandleOverlaySubmitQuickOpenInsert(t *testing.T) {
	term := NewFakeTerminal("", 80, 24)
	l := NewLoop(term, nil)
	handled := l.handleOverlaySubmit("quickopen-insert:internal/foo.go")
	if !handled {
		t.Fatal("expected handled=true for quickopen-insert:")
	}
	if l.screen.Prompt.Text != "internal/foo.go " {
		t.Errorf("expected 'internal/foo.go ', got %q", l.screen.Prompt.Text)
	}
}

func TestLoopHandleOverlaySubmitHistorySearch(t *testing.T) {
	term := NewFakeTerminal("", 80, 24)
	l := NewLoop(term, nil)
	handled := l.handleOverlaySubmit("historysearch:fix the bug")
	if !handled {
		t.Fatal("expected handled=true for historysearch:")
	}
	if l.screen.Prompt.Text != "fix the bug" {
		t.Errorf("expected 'fix the bug', got %q", l.screen.Prompt.Text)
	}
}
