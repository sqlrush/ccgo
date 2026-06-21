package repl

import (
	"strings"
	"testing"

	"ccgo/internal/tui"
)

func TestMemorySelectorEnterSubmits(t *testing.T) {
	s := NewMemorySelector([]string{"~/.claude/CLAUDE.md", "./CLAUDE.md"})
	res, _ := s.ApplyKey(tui.Key{Type: tui.KeyEnter})
	if res.Submit != "memory:~/.claude/CLAUDE.md" {
		t.Fatalf("submit = %q", res.Submit)
	}
}

func TestMemorySelectorEscDismisses(t *testing.T) {
	s := NewMemorySelector([]string{"~/.claude/CLAUDE.md"})
	res, _ := s.ApplyKey(tui.Key{Type: tui.KeyEsc})
	if !res.Dismissed {
		t.Fatal("Esc should dismiss memory selector")
	}
}

func TestMemorySelectorRenderShowsFiles(t *testing.T) {
	s := NewMemorySelector([]string{"~/.claude/CLAUDE.md", "./CLAUDE.md"})
	lines := s.Render(80, 24)
	joined := strings.Join(lines, "\n")
	if !strings.Contains(joined, "~/.claude/CLAUDE.md") {
		t.Fatalf("render missing user memory file: %v", lines)
	}
	if !strings.Contains(joined, "./CLAUDE.md") {
		t.Fatalf("render missing project memory file: %v", lines)
	}
}

func TestMemorySelectorNavigateAndSelect(t *testing.T) {
	s := NewMemorySelector([]string{"~/.claude/CLAUDE.md", "./CLAUDE.md"})
	s.ApplyKey(tui.Key{Type: tui.KeyDown})
	res, handled := s.ApplyKey(tui.Key{Type: tui.KeyEnter})
	if !handled {
		t.Fatal("Enter should be handled")
	}
	if res.Submit != "memory:./CLAUDE.md" {
		t.Fatalf("submit = %q want memory:./CLAUDE.md", res.Submit)
	}
}

func TestMemorySelectorUpAtTopNoOp(t *testing.T) {
	s := NewMemorySelector([]string{"~/.claude/CLAUDE.md", "./CLAUDE.md"})
	res, handled := s.ApplyKey(tui.Key{Type: tui.KeyUp})
	if !handled {
		t.Fatal("Up should be handled")
	}
	if res.Submit != "" || res.Dismissed {
		t.Fatalf("Up at top should return empty result, got %+v", res)
	}
	// Still at first item
	res2, _ := s.ApplyKey(tui.Key{Type: tui.KeyEnter})
	if res2.Submit != "memory:~/.claude/CLAUDE.md" {
		t.Fatalf("cursor should still be at first item, got submit=%q", res2.Submit)
	}
}

func TestMemorySelectorDownAtBottomNoOp(t *testing.T) {
	s := NewMemorySelector([]string{"~/.claude/CLAUDE.md", "./CLAUDE.md"})
	s.ApplyKey(tui.Key{Type: tui.KeyDown}) // now at last
	res, handled := s.ApplyKey(tui.Key{Type: tui.KeyDown})
	if !handled {
		t.Fatal("Down should be handled")
	}
	if res.Submit != "" || res.Dismissed {
		t.Fatalf("Down at bottom should return empty result, got %+v", res)
	}
	res2, _ := s.ApplyKey(tui.Key{Type: tui.KeyEnter})
	if res2.Submit != "memory:./CLAUDE.md" {
		t.Fatalf("cursor should still be at last item, got submit=%q", res2.Submit)
	}
}

func TestMemorySelectorEnterOnEmptyDismisses(t *testing.T) {
	s := NewMemorySelector([]string{})
	res, handled := s.ApplyKey(tui.Key{Type: tui.KeyEnter})
	if !handled {
		t.Fatal("Enter should be handled")
	}
	if !res.Dismissed {
		t.Fatalf("Enter on empty list should dismiss, got Submit=%q", res.Submit)
	}
}

func TestMemorySelectorImmutability(t *testing.T) {
	files := []string{"~/.claude/CLAUDE.md", "./CLAUDE.md"}
	original := make([]string, len(files))
	copy(original, files)

	NewMemorySelector(files)

	for i, f := range files {
		if f != original[i] {
			t.Fatalf("input slice was mutated at index %d: got %q, want %q", i, f, original[i])
		}
	}
}
