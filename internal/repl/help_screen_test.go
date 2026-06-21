package repl

import (
	"strings"
	"testing"

	"ccgo/internal/contracts"
	"ccgo/internal/tui"
)

func TestHelpScreenListsCommandsAndDismisses(t *testing.T) {
	h := NewHelpScreen([]contracts.Command{{Name: "clear", Description: "Clear"}})
	lines := h.Render(80, 24)
	if !strings.Contains(strings.Join(lines, "\n"), "/clear") {
		t.Fatalf("help should list /clear: %v", lines)
	}
	res, _ := h.ApplyKey(tui.Key{Type: tui.KeyEsc})
	if !res.Dismissed {
		t.Fatal("Esc should dismiss help")
	}
}

func TestHelpScreenHidesHiddenCommands(t *testing.T) {
	h := NewHelpScreen([]contracts.Command{
		{Name: "visible", Description: "Visible command"},
		{Name: "secret", Description: "Hidden command", Hidden: true},
	})
	lines := h.Render(80, 24)
	joined := strings.Join(lines, "\n")
	if !strings.Contains(joined, "/visible") {
		t.Fatalf("should list /visible: %v", lines)
	}
	if strings.Contains(joined, "/secret") {
		t.Fatalf("should not list hidden /secret: %v", lines)
	}
}

func TestHelpScreenEnterSubmitsCommand(t *testing.T) {
	h := NewHelpScreen([]contracts.Command{
		{Name: "clear", Description: "Clear"},
		{Name: "compact", Description: "Compact"},
	})
	// move to second item
	h.ApplyKey(tui.Key{Type: tui.KeyDown})
	res, handled := h.ApplyKey(tui.Key{Type: tui.KeyEnter})
	if !handled {
		t.Fatal("Enter should be handled")
	}
	if res.Submit != "/compact" {
		t.Fatalf("submit = %q want /compact", res.Submit)
	}
}
