package repl

import (
	"strings"
	"testing"

	"ccgo/internal/tui"
)

// ── construction ─────────────────────────────────────────────────────────────

func TestQuickOpenOverlayConstruction(t *testing.T) {
	files := []string{"main.go", "internal/foo.go", "internal/bar.go"}
	o := newQuickOpenOverlayWithFiles(files)
	// With no query all files are visible.
	if len(o.filtered) != 3 {
		t.Errorf("expected 3 filtered items, got %d", len(o.filtered))
	}
}

func TestQuickOpenOverlayEmptyFiles(t *testing.T) {
	o := newQuickOpenOverlayWithFiles(nil)
	if len(o.filtered) != 0 {
		t.Errorf("expected 0 items, got %d", len(o.filtered))
	}
}

// ── typing filters ────────────────────────────────────────────────────────────

func TestQuickOpenOverlayTypingFilters(t *testing.T) {
	files := []string{"main.go", "internal/fuzzy.go", "cmd/foo.go"}
	o := newQuickOpenOverlayWithFiles(files)
	// Type "fuzz"
	for _, r := range "fuzz" {
		res, handled := o.ApplyKey(tui.Key{Type: tui.KeyRune, Rune: r})
		if !handled {
			t.Fatalf("expected key handled")
		}
		if res.Submit != "" || res.Dismissed {
			t.Fatalf("unexpected result %+v", res)
		}
	}
	if o.Query() != "fuzz" {
		t.Errorf("expected query 'fuzz', got %q", o.Query())
	}
	if len(o.filtered) != 1 || !strings.Contains(o.filtered[0], "fuzzy") {
		t.Errorf("expected fuzzy.go only, got %v", o.filtered)
	}
}

// ── cursor navigation ─────────────────────────────────────────────────────────

func TestQuickOpenOverlayCursorNavigation(t *testing.T) {
	files := []string{"a.go", "b.go", "c.go"}
	o := newQuickOpenOverlayWithFiles(files)
	// Initial cursor at 0.
	if o.cursor != 0 {
		t.Errorf("expected cursor 0, got %d", o.cursor)
	}
	// Down moves to 1.
	o.ApplyKey(tui.Key{Type: tui.KeyDown})
	if o.cursor != 1 {
		t.Errorf("expected cursor 1 after Down, got %d", o.cursor)
	}
	// Down again → 2.
	o.ApplyKey(tui.Key{Type: tui.KeyDown})
	if o.cursor != 2 {
		t.Errorf("expected cursor 2, got %d", o.cursor)
	}
	// Down past end → stays at 2.
	o.ApplyKey(tui.Key{Type: tui.KeyDown})
	if o.cursor != 2 {
		t.Errorf("expected cursor stay at 2, got %d", o.cursor)
	}
	// Up → 1.
	o.ApplyKey(tui.Key{Type: tui.KeyUp})
	if o.cursor != 1 {
		t.Errorf("expected cursor 1 after Up, got %d", o.cursor)
	}
}

// ── enter selects (@ mention) ─────────────────────────────────────────────────

func TestQuickOpenOverlayEnterSelectsMention(t *testing.T) {
	files := []string{"main.go", "internal/foo.go"}
	o := newQuickOpenOverlayWithFiles(files)
	// Cursor at 0 → main.go
	res, handled := o.ApplyKey(tui.Key{Type: tui.KeyEnter})
	if !handled {
		t.Fatal("expected handled")
	}
	if res.Submit != "quickopen:main.go" {
		t.Errorf("expected quickopen:main.go, got %q", res.Submit)
	}
}

// ── tab selects (insert plain path) ──────────────────────────────────────────

func TestQuickOpenOverlayTabInsertsPath(t *testing.T) {
	files := []string{"src/lib.go"}
	o := newQuickOpenOverlayWithFiles(files)
	res, handled := o.ApplyKey(tui.Key{Type: tui.KeyTab})
	if !handled {
		t.Fatal("expected handled")
	}
	if res.Submit != "quickopen-insert:src/lib.go" {
		t.Errorf("expected quickopen-insert:src/lib.go, got %q", res.Submit)
	}
}

// ── esc dismisses ────────────────────────────────────────────────────────────

func TestQuickOpenOverlayEscDismisses(t *testing.T) {
	o := newQuickOpenOverlayWithFiles([]string{"a.go"})
	res, handled := o.ApplyKey(tui.Key{Type: tui.KeyEsc})
	if !handled || !res.Dismissed {
		t.Errorf("expected Dismissed=true, got %+v", res)
	}
}

// ── backspace removes query char ──────────────────────────────────────────────

func TestQuickOpenOverlayBackspaceRemovesQueryChar(t *testing.T) {
	files := []string{"main.go", "internal/foo.go"}
	o := newQuickOpenOverlayWithFiles(files)
	// Type "mai"
	for _, r := range "mai" {
		o.ApplyKey(tui.Key{Type: tui.KeyRune, Rune: r})
	}
	if o.Query() != "mai" {
		t.Fatalf("expected query 'mai', got %q", o.Query())
	}
	// Backspace removes "i"
	o.ApplyKey(tui.Key{Type: tui.KeyBackspace})
	if o.Query() != "ma" {
		t.Errorf("expected query 'ma' after backspace, got %q", o.Query())
	}
}

// ── enter with empty list dismisses ──────────────────────────────────────────

func TestQuickOpenOverlayEnterEmptyListDismisses(t *testing.T) {
	o := newQuickOpenOverlayWithFiles([]string{"foo.go"})
	// Type something that matches nothing.
	for _, r := range "zzzzz" {
		o.ApplyKey(tui.Key{Type: tui.KeyRune, Rune: r})
	}
	if len(o.filtered) != 0 {
		t.Fatalf("expected empty filtered list")
	}
	res, _ := o.ApplyKey(tui.Key{Type: tui.KeyEnter})
	if !res.Dismissed {
		t.Errorf("expected Dismissed when list empty, got %+v", res)
	}
}

// ── cursor resets on refilter ─────────────────────────────────────────────────

func TestQuickOpenOverlayCursorResetsOnFilter(t *testing.T) {
	files := []string{"aaa.go", "aab.go", "abc.go"}
	o := newQuickOpenOverlayWithFiles(files)
	// Move cursor to 2
	o.ApplyKey(tui.Key{Type: tui.KeyDown})
	o.ApplyKey(tui.Key{Type: tui.KeyDown})
	if o.cursor != 2 {
		t.Fatalf("cursor should be 2")
	}
	// Type to narrow list to 1 item → cursor clamped to 0
	for _, r := range "abc" {
		o.ApplyKey(tui.Key{Type: tui.KeyRune, Rune: r})
	}
	if o.cursor != 0 {
		t.Errorf("expected cursor 0 after refilter, got %d", o.cursor)
	}
}

// ── render smoke test ─────────────────────────────────────────────────────────

func TestQuickOpenOverlayRenderContainsItems(t *testing.T) {
	files := []string{"main.go", "internal/foo.go"}
	o := newQuickOpenOverlayWithFiles(files)
	lines := o.Render(80, 10)
	if len(lines) == 0 {
		t.Fatal("expected non-empty render")
	}
	found := false
	for _, l := range lines {
		if strings.Contains(l, "main.go") {
			found = true
			break
		}
	}
	if !found {
		t.Errorf("expected main.go in render, got %v", lines)
	}
}

// ── handleQuickOpenSubmit ─────────────────────────────────────────────────────

func TestHandleQuickOpenSubmitMention(t *testing.T) {
	prompt := ""
	if !handleQuickOpenSubmit(&prompt, "quickopen:src/lib.go") {
		t.Fatal("expected true")
	}
	if prompt != "@src/lib.go " {
		t.Errorf("expected '@src/lib.go ', got %q", prompt)
	}
}

func TestHandleQuickOpenSubmitInsertPath(t *testing.T) {
	prompt := ""
	if !handleQuickOpenSubmit(&prompt, "quickopen-insert:src/lib.go") {
		t.Fatal("expected true")
	}
	if prompt != "src/lib.go " {
		t.Errorf("expected 'src/lib.go ', got %q", prompt)
	}
}

func TestHandleQuickOpenSubmitIgnoresUnknown(t *testing.T) {
	prompt := ""
	if handleQuickOpenSubmit(&prompt, "resume:session123") {
		t.Fatal("expected false for non-quickopen prefix")
	}
}
