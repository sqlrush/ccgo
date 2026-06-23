package tui

import (
	"strings"
	"testing"
)

// TestREPLScreenThemeAffectsRender verifies that the Theme field on REPLScreen
// propagates through Frame() into Renderer.Render(), producing different ANSI
// styling for dark vs light themes.
// CC ref: utils/settings/types.ts theme — controls colour palette.
func TestREPLScreenThemeAffectsRender(t *testing.T) {
	darkScreen := NewREPLScreen(80, 24, nil)
	darkScreen.Theme = "dark"
	darkScreen.Status = "ready"

	lightScreen := NewREPLScreen(80, 24, nil)
	lightScreen.Theme = "light"
	lightScreen.Status = "ready"

	dark := darkScreen.Render()
	light := lightScreen.Render()

	// Both must contain the status text.
	if !strings.Contains(dark, "ready") {
		t.Errorf("dark theme render missing 'ready'; render length %d", len(dark))
	}
	if !strings.Contains(light, "ready") {
		t.Errorf("light theme render missing 'ready'; render length %d", len(light))
	}
	// The ANSI sequences must differ between dark and light.
	if dark == light {
		t.Error("expected dark and light theme renders to differ (different ANSI sequences)")
	}
}
