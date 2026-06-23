package repl

import (
	"context"
	"testing"

	"ccgo/internal/tui"
)

func TestModelHandlerNoArgOpensOverlay(t *testing.T) {
	models := []string{"claude-opus-4-5", "claude-sonnet-4-5"}
	h := modelHandlerWith(models)
	screen := tui.NewREPLScreen(80, 24, nil)
	out, err := h(context.Background(), CommandContext{Args: "", Screen: &screen})
	if err != nil {
		t.Fatalf("handler error: %v", err)
	}
	if !out.Handled {
		t.Fatal("Handled must be true")
	}
	if out.Overlay == nil {
		t.Fatal("/model with no arg must open ModelPicker overlay, got nil")
	}
}

func TestModelPickerRender(t *testing.T) {
	models := []string{"claude-opus-4-5", "claude-sonnet-4-5"}
	picker := NewModelPicker(models)
	lines := picker.Render(80, 10)
	if len(lines) < 2 {
		t.Fatalf("picker must render at least title + one model, got %d lines", len(lines))
	}
}
