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
