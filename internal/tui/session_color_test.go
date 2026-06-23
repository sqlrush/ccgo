package tui

import (
	"strings"
	"testing"
)

// TestREPLScreenSessionColorAffectsRender verifies that the SessionColor field
// on REPLScreen affects the rendered prompt prefix, distinguishing this agent's
// output visually in a multi-agent (swarm) scenario.
// CC ref: src/commands/color/color.ts — /color sets the agent's session color.
func TestREPLScreenSessionColorAffectsRender(t *testing.T) {
	plain := NewREPLScreen(80, 24, nil)
	plain.Status = "ready"

	colored := NewREPLScreen(80, 24, nil)
	colored.Status = "ready"
	colored.SessionColor = "blue"

	plainRender := plain.Render()
	coloredRender := colored.Render()

	if plainRender == coloredRender {
		t.Error("expected session color 'blue' to produce different render output from no-color")
	}
}

// TestREPLScreenSessionColorEmpty leaves render unchanged.
func TestREPLScreenSessionColorEmpty(t *testing.T) {
	screen := NewREPLScreen(80, 24, nil)
	screen.Status = "ready"
	screen.SessionColor = ""

	noColor := screen.Render()
	_ = noColor // no-op: just verify no panic
	if !strings.Contains(noColor, "ready") {
		t.Errorf("expected 'ready' in render output with empty session color; got length %d", len(noColor))
	}
}
