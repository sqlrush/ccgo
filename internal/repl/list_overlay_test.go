package repl

import (
	"testing"

	"ccgo/internal/tui"
)

// TestListOverlayEdgeCases tests the shared listOverlay directly via its constructors.

func TestListOverlayUpAtTopNoOp(t *testing.T) {
	o := newListOverlay("title", []listItem{{Label: "a", Submit: "a"}, {Label: "b", Submit: "b"}})
	if o.cursor != 0 {
		t.Fatalf("initial cursor = %d want 0", o.cursor)
	}
	res, handled := o.ApplyKey(tui.Key{Type: tui.KeyUp})
	if !handled {
		t.Fatal("Up should be handled")
	}
	if res.Submit != "" || res.Dismissed {
		t.Fatalf("Up at top should return empty result, got %+v", res)
	}
	if o.cursor != 0 {
		t.Fatalf("cursor after Up at top = %d want 0", o.cursor)
	}
}

func TestListOverlayDownAtBottomNoOp(t *testing.T) {
	o := newListOverlay("title", []listItem{{Label: "a", Submit: "a"}, {Label: "b", Submit: "b"}})
	o.ApplyKey(tui.Key{Type: tui.KeyDown}) // now at 1 (last)
	res, handled := o.ApplyKey(tui.Key{Type: tui.KeyDown})
	if !handled {
		t.Fatal("Down should be handled")
	}
	if res.Submit != "" || res.Dismissed {
		t.Fatalf("Down at bottom should return empty result, got %+v", res)
	}
	if o.cursor != 1 {
		t.Fatalf("cursor after Down at bottom = %d want 1", o.cursor)
	}
}

func TestListOverlayEnterOnEmptyDismisses(t *testing.T) {
	o := newListOverlay("title", []listItem{})
	res, handled := o.ApplyKey(tui.Key{Type: tui.KeyEnter})
	if !handled {
		t.Fatal("Enter should be handled even on empty list")
	}
	if !res.Dismissed {
		t.Fatalf("Enter on empty list should dismiss, got Submit=%q Dismissed=%v", res.Submit, res.Dismissed)
	}
	if res.Submit != "" {
		t.Fatalf("Enter on empty list should have empty Submit, got %q", res.Submit)
	}
}

func TestListOverlayEnterSelectsItem(t *testing.T) {
	o := newListOverlay("title", []listItem{
		{Label: "first", Submit: "val:first"},
		{Label: "second", Submit: "val:second"},
	})
	o.ApplyKey(tui.Key{Type: tui.KeyDown})
	res, handled := o.ApplyKey(tui.Key{Type: tui.KeyEnter})
	if !handled {
		t.Fatal("Enter should be handled")
	}
	if res.Submit != "val:second" {
		t.Fatalf("submit = %q want val:second", res.Submit)
	}
}

func TestListOverlayEscDismisses(t *testing.T) {
	o := newListOverlay("title", []listItem{{Label: "a", Submit: "a"}})
	res, handled := o.ApplyKey(tui.Key{Type: tui.KeyEsc})
	if !handled {
		t.Fatal("Esc should be handled")
	}
	if !res.Dismissed {
		t.Fatalf("Esc should dismiss, got %+v", res)
	}
}

func TestListOverlayUnknownKeyNotHandled(t *testing.T) {
	o := newListOverlay("title", []listItem{{Label: "a", Submit: "a"}})
	_, handled := o.ApplyKey(tui.Key{Type: tui.KeyRune, Rune: 'x'})
	if handled {
		t.Fatal("unknown key should not be handled by listOverlay")
	}
}

func TestListOverlayDefensiveCopyInput(t *testing.T) {
	items := []listItem{
		{Label: "a", Submit: "a"},
		{Label: "b", Submit: "b"},
	}
	o := newListOverlay("title", items)
	// Mutate the original slice
	items[0] = listItem{Label: "mutated", Submit: "mutated"}
	// The overlay should still have the original
	res, _ := o.ApplyKey(tui.Key{Type: tui.KeyEnter})
	if res.Submit == "mutated" {
		t.Fatal("overlay should not share backing array with input slice")
	}
	if res.Submit != "a" {
		t.Fatalf("overlay item 0 submit = %q want 'a'", res.Submit)
	}
}

func TestListOverlayRenderTitleFirst(t *testing.T) {
	o := newListOverlay("My Title", []listItem{{Label: "item1", Submit: "x"}})
	lines := o.Render(80, 24)
	if len(lines) == 0 {
		t.Fatal("render should return at least one line")
	}
	if lines[0] != "My Title" {
		t.Fatalf("first render line = %q want 'My Title'", lines[0])
	}
}

func TestListOverlayRenderCursorMarker(t *testing.T) {
	o := newListOverlay("title", []listItem{
		{Label: "alpha", Submit: "a"},
		{Label: "beta", Submit: "b"},
	})
	lines := o.Render(80, 24)
	// cursor at 0 → second line (index 1) should have "> " prefix
	if len(lines) < 2 {
		t.Fatalf("expected >=2 lines, got %d", len(lines))
	}
	if lines[1][:2] != "> " {
		t.Fatalf("selected item should have '> ' prefix, got %q", lines[1])
	}
	if lines[2][:2] == "> " {
		t.Fatalf("non-selected item should not have '> ' prefix, got %q", lines[2])
	}
}
