package repl

import (
	"testing"

	"ccgo/internal/contracts"
	"ccgo/internal/tui"
)

func sampleCommands() []contracts.Command {
	return []contracts.Command{
		{Name: "help", Description: "Show help"},
		{Name: "clear", Description: "Clear conversation"},
		{Name: "compact", Description: "Compact"},
		{Name: "config", Description: "Config"},
	}
}

func TestSlashMenuFiltersByPrefix(t *testing.T) {
	m := NewSlashMenu(sampleCommands(), "c")
	if len(m.filtered) != 3 { // clear, compact, config
		t.Fatalf("filtered = %d want 3 (%v)", len(m.filtered), m.filtered)
	}
}

func TestSlashMenuEnterSubmitsSelected(t *testing.T) {
	m := NewSlashMenu(sampleCommands(), "co") // compact, config
	res, _ := m.ApplyKey(tui.Key{Type: tui.KeyDown}) // move to config
	_ = res
	res, _ = m.ApplyKey(tui.Key{Type: tui.KeyEnter})
	if res.Submit != "/config" {
		t.Fatalf("submit = %q want /config", res.Submit)
	}
}

func TestSlashMenuEscDismisses(t *testing.T) {
	m := NewSlashMenu(sampleCommands(), "")
	res, _ := m.ApplyKey(tui.Key{Type: tui.KeyEsc})
	if !res.Dismissed {
		t.Fatal("Esc should dismiss the slash menu")
	}
}

func TestSlashMenuUpAtTopIsNoop(t *testing.T) {
	m := NewSlashMenu(sampleCommands(), "") // >=2 commands, cursor at 0
	if m.cursor != 0 {
		t.Fatalf("initial cursor = %d want 0", m.cursor)
	}
	res, _ := m.ApplyKey(tui.Key{Type: tui.KeyUp})
	if m.cursor != 0 {
		t.Fatalf("cursor after Up = %d want 0", m.cursor)
	}
	if res.Dismissed {
		t.Fatal("Up at top should not dismiss")
	}
}

func TestSlashMenuDownAtBottomIsNoop(t *testing.T) {
	m := NewSlashMenu(sampleCommands(), "") // 4 commands
	// Move cursor to last index
	for i := 0; i < len(m.filtered)-1; i++ {
		m.ApplyKey(tui.Key{Type: tui.KeyDown})
	}
	if m.cursor != len(m.filtered)-1 {
		t.Fatalf("cursor = %d want %d", m.cursor, len(m.filtered)-1)
	}
	// Send Down again at bottom
	res, _ := m.ApplyKey(tui.Key{Type: tui.KeyDown})
	if m.cursor != len(m.filtered)-1 {
		t.Fatalf("cursor after Down at bottom = %d want %d", m.cursor, len(m.filtered)-1)
	}
	if res.Dismissed {
		t.Fatal("Down at bottom should not dismiss")
	}
}

func TestSlashMenuEnterOnEmptyFilterDismisses(t *testing.T) {
	m := NewSlashMenu(sampleCommands(), "zzzznomatch") // query matching nothing
	if len(m.filtered) != 0 {
		t.Fatalf("filtered should be empty, got %d commands", len(m.filtered))
	}
	res, _ := m.ApplyKey(tui.Key{Type: tui.KeyEnter})
	if !res.Dismissed {
		t.Fatal("Enter on empty filter should dismiss")
	}
	if res.Submit != "" {
		t.Fatalf("submit = %q want empty string", res.Submit)
	}
}
