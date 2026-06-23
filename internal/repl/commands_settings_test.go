package repl

import (
	"context"
	"testing"

	"ccgo/internal/tui"
)

func TestEffortHandlerValidatesAndWrites(t *testing.T) {
	var key string
	var val any
	set := func(k string, v any) error { key, val = k, v; return nil }
	h := effortHandlerWith(set)

	if out, err := h(context.Background(), CommandContext{Args: "high"}); err != nil || !out.Handled {
		t.Fatalf("high: %v %+v", err, out)
	}
	if key != "effortLevel" || val != "high" {
		t.Fatalf("wrote %q=%v want effortLevel=high", key, val)
	}
	// auto clears (nil value).
	if _, err := h(context.Background(), CommandContext{Args: "auto"}); err != nil {
		t.Fatalf("auto: %v", err)
	}
	if val != nil {
		t.Fatalf("auto must clear effortLevel, got %v", val)
	}
	// invalid value is rejected without writing.
	key = ""
	if out, _ := h(context.Background(), CommandContext{Args: "turbo"}); !out.Handled || key != "" {
		t.Fatalf("invalid effort should report but not write; key=%q out=%+v", key, out)
	}
}

func TestThemeHandlerWrites(t *testing.T) {
	var key string
	set := func(k string, v any) error { key = k; return nil }
	h := themeHandlerWith(set)
	if out, err := h(context.Background(), CommandContext{Args: "dark"}); err != nil || !out.Handled {
		t.Fatalf("theme: %v %+v", err, out)
	}
	if key != "theme" {
		t.Fatalf("wrote %q want theme", key)
	}
}

func TestVimHandlerTogglesAndPersists(t *testing.T) {
	calls := map[string]any{}
	set := func(k string, v any) error { calls[k] = v; return nil }

	screen := &tui.REPLScreen{}
	screen.VimEnabled = false

	h := vimHandlerWith(set)
	cc := CommandContext{Screen: screen}

	// First toggle: normal -> vim.
	out, err := h(context.Background(), cc)
	if err != nil {
		t.Fatalf("first toggle: %v", err)
	}
	if !out.Handled {
		t.Fatal("must be handled")
	}
	if !screen.VimEnabled {
		t.Fatal("screen.VimEnabled should be true after first toggle")
	}
	if calls["editorMode"] != "vim" {
		t.Fatalf("expected editorMode=vim got %v", calls["editorMode"])
	}

	// Second toggle: vim -> normal.
	out, err = h(context.Background(), cc)
	if err != nil {
		t.Fatalf("second toggle: %v", err)
	}
	if !out.Handled {
		t.Fatal("must be handled")
	}
	if screen.VimEnabled {
		t.Fatal("screen.VimEnabled should be false after second toggle")
	}
	if calls["editorMode"] != "normal" {
		t.Fatalf("expected editorMode=normal got %v", calls["editorMode"])
	}
}

func TestThemeHandlerEmptyArg(t *testing.T) {
	written := false
	set := func(k string, v any) error { written = true; return nil }
	h := themeHandlerWith(set)
	out, err := h(context.Background(), CommandContext{Args: ""})
	if err != nil {
		t.Fatalf("empty arg: %v", err)
	}
	if !out.Handled {
		t.Fatal("must be handled even with empty arg (opens picker)")
	}
	if written {
		t.Fatal("empty arg must not write")
	}
}

// TestThemeHandlerNoArgOpensOverlay: /theme with no arg opens ThemePicker overlay.
func TestThemeHandlerNoArgOpensOverlay(t *testing.T) {
	var set settingsSetter = func(key string, value any) error { return nil }
	h := themeHandlerWith(set)
	screen := tui.NewREPLScreen(80, 24, nil)
	out, err := h(context.Background(), CommandContext{Args: "", Screen: &screen})
	if err != nil {
		t.Fatalf("handler error: %v", err)
	}
	if !out.Handled {
		t.Fatal("Handled must be true")
	}
	if out.Overlay == nil {
		t.Fatal("/theme with no arg must return ThemePicker overlay, got nil")
	}
	if out.Status != "" {
		t.Fatalf("/theme with no arg must not return Status text, got: %q", out.Status)
	}
}

func TestEffortHandlerEmptyArg(t *testing.T) {
	written := false
	set := func(k string, v any) error { written = true; return nil }
	h := effortHandlerWith(set)
	out, err := h(context.Background(), CommandContext{Args: ""})
	if err != nil {
		t.Fatalf("empty arg: %v", err)
	}
	if !out.Handled {
		t.Fatal("must be handled even with empty arg (shows usage)")
	}
	if written {
		t.Fatal("empty arg must not write")
	}
}
