package repl

import (
	"strings"
	"testing"

	"ccgo/internal/tui"
)

func TestThemePickerEnterSubmits(t *testing.T) {
	p := NewThemePicker([]string{"dark", "light"})
	p.ApplyKey(tui.Key{Type: tui.KeyDown})
	res, _ := p.ApplyKey(tui.Key{Type: tui.KeyEnter})
	if res.Submit != "theme:light" {
		t.Fatalf("submit = %q want theme:light", res.Submit)
	}
}

func TestThemePickerEscDismisses(t *testing.T) {
	p := NewThemePicker([]string{"dark", "light"})
	res, _ := p.ApplyKey(tui.Key{Type: tui.KeyEsc})
	if !res.Dismissed {
		t.Fatal("Esc should dismiss theme picker")
	}
}

func TestThemePickerRenderShowsThemes(t *testing.T) {
	p := NewThemePicker([]string{"dark", "light", "solarized"})
	lines := p.Render(80, 24)
	joined := strings.Join(lines, "\n")
	if !strings.Contains(joined, "dark") {
		t.Fatalf("render missing 'dark': %v", lines)
	}
	if !strings.Contains(joined, "light") {
		t.Fatalf("render missing 'light': %v", lines)
	}
}

func TestThemePickerUpAtTopNoOp(t *testing.T) {
	p := NewThemePicker([]string{"dark", "light"})
	res, handled := p.ApplyKey(tui.Key{Type: tui.KeyUp})
	if !handled {
		t.Fatal("Up should be handled")
	}
	if res.Submit != "" || res.Dismissed {
		t.Fatalf("Up at top should return empty result, got %+v", res)
	}
	// Verify cursor is still at 0 by pressing Enter and checking first item
	res2, _ := p.ApplyKey(tui.Key{Type: tui.KeyEnter})
	if res2.Submit != "theme:dark" {
		t.Fatalf("cursor should be at first item 'dark', got submit=%q", res2.Submit)
	}
}

func TestThemePickerDownAtBottomNoOp(t *testing.T) {
	p := NewThemePicker([]string{"dark", "light"})
	p.ApplyKey(tui.Key{Type: tui.KeyDown}) // cursor at 1 (last)
	res, handled := p.ApplyKey(tui.Key{Type: tui.KeyDown})
	if !handled {
		t.Fatal("Down should be handled")
	}
	if res.Submit != "" || res.Dismissed {
		t.Fatalf("Down at bottom should return empty result, got %+v", res)
	}
	// Verify cursor is still at last item
	res2, _ := p.ApplyKey(tui.Key{Type: tui.KeyEnter})
	if res2.Submit != "theme:light" {
		t.Fatalf("cursor should still be at 'light', got submit=%q", res2.Submit)
	}
}

func TestThemePickerEnterOnEmptyDismisses(t *testing.T) {
	p := NewThemePicker([]string{})
	res, handled := p.ApplyKey(tui.Key{Type: tui.KeyEnter})
	if !handled {
		t.Fatal("Enter should be handled")
	}
	if !res.Dismissed {
		t.Fatalf("Enter on empty list should dismiss, got Submit=%q", res.Submit)
	}
}

func TestThemePickerImmutability(t *testing.T) {
	themes := []string{"dark", "light"}
	original := make([]string, len(themes))
	copy(original, themes)

	NewThemePicker(themes)

	for i, th := range themes {
		if th != original[i] {
			t.Fatalf("input slice was mutated at index %d: got %q, want %q", i, th, original[i])
		}
	}
}
