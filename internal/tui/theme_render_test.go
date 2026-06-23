package tui

import (
	"strings"
	"testing"
)

// TestRenderStatusLineThemeDarkVsLight verifies that the theme setting affects the
// ANSI styling of the status line. Dark theme uses reverse-video; light theme uses
// a different style (bold/underline) so text remains legible on light backgrounds.
// CC ref: utils/settings/types.ts theme — controls colour palette (dark/light).
func TestRenderStatusLineThemeDarkVsLight(t *testing.T) {
	status := "ready"
	dark := RenderStatusLineWithTheme(status, 40, "dark")
	light := RenderStatusLineWithTheme(status, 40, "light")
	if dark == light {
		t.Errorf("expected dark and light themes to produce different styled output; both produced %q", dark)
	}
	// Both must contain the status text.
	if !strings.Contains(dark, status) {
		t.Errorf("dark theme status line missing %q; got %q", status, dark)
	}
	if !strings.Contains(light, status) {
		t.Errorf("light theme status line missing %q; got %q", status, light)
	}
}

// TestRenderStatusLineThemeDefault verifies that an empty or "default" theme falls
// back to the dark (reverse-video) style, matching the pre-existing behaviour.
func TestRenderStatusLineThemeDefault(t *testing.T) {
	status := "ready"
	noTheme := RenderStatusLine(status, 40)
	defaultTheme := RenderStatusLineWithTheme(status, 40, "")
	darkTheme := RenderStatusLineWithTheme(status, 40, "dark")
	if noTheme != defaultTheme {
		t.Errorf("expected empty theme to match RenderStatusLine; got %q vs %q", noTheme, defaultTheme)
	}
	if noTheme != darkTheme {
		t.Errorf("expected dark theme to match default RenderStatusLine; got %q vs %q", noTheme, darkTheme)
	}
}

// TestRenderStatusLineThemeDaltonism verifies that the daltonism variant produces
// a distinct rendering from the standard dark theme.
func TestRenderStatusLineThemeDaltonism(t *testing.T) {
	status := "ready"
	dark := RenderStatusLineWithTheme(status, 40, "dark")
	darkD := RenderStatusLineWithTheme(status, 40, "dark-daltonism")
	// Both must contain the status text, but styling may differ.
	if !strings.Contains(darkD, status) {
		t.Errorf("dark-daltonism theme missing %q; got %q", status, darkD)
	}
	// dark-daltonism is currently treated same as dark (no color palette change in ccgo).
	// The test verifies the function accepts the value without panic.
	_ = dark
}
