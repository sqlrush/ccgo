package repl

// G24: /config interactive panel overlay state tests.
// Tests for the configOverlay state machine: open → navigate → edit → apply / cancel.
// All tests are pixel-free; Render() is verified only for non-empty output.
//
// CC ref: src/components/Settings/Config.tsx — the Config tab of the Settings panel.

import (
	"context"
	"testing"

	"ccgo/internal/tui"
)

func sampleConfigEntries() []configSettingEntry {
	return []configSettingEntry{
		{Key: "model", Label: "Model", Value: "claude-opus-4", Editable: true},
		{Key: "theme", Label: "Theme", Value: "dark", Editable: true},
		{Key: "verbose", Label: "Verbose", Value: "false", Editable: true, Type: configTypeBool},
	}
}

func TestNewConfigOverlayHasEntries(t *testing.T) {
	ov := newConfigOverlay(sampleConfigEntries(), nil)
	if ov == nil {
		t.Fatal("newConfigOverlay returned nil")
	}
	if ov.Len() != 3 {
		t.Fatalf("Len = %d want 3", ov.Len())
	}
}

func TestConfigOverlayInitialCursor(t *testing.T) {
	ov := newConfigOverlay(sampleConfigEntries(), nil)
	if ov.Cursor() != 0 {
		t.Fatalf("initial cursor = %d want 0", ov.Cursor())
	}
}

func TestConfigOverlayNavigateDown(t *testing.T) {
	ov := newConfigOverlay(sampleConfigEntries(), nil)
	res, handled := ov.ApplyKey(tui.Key{Type: tui.KeyDown})
	if !handled {
		t.Fatal("Down should be handled")
	}
	if res.Dismissed || res.Submit != "" {
		t.Fatalf("Down should not dismiss/submit: %+v", res)
	}
	if ov.Cursor() != 1 {
		t.Fatalf("cursor after Down = %d want 1", ov.Cursor())
	}
}

func TestConfigOverlayNavigateUp(t *testing.T) {
	ov := newConfigOverlay(sampleConfigEntries(), nil)
	ov.ApplyKey(tui.Key{Type: tui.KeyDown}) // cursor=1
	res, handled := ov.ApplyKey(tui.Key{Type: tui.KeyUp})
	if !handled {
		t.Fatal("Up should be handled")
	}
	if ov.Cursor() != 0 {
		t.Fatalf("cursor after Up = %d want 0", ov.Cursor())
	}
	_ = res
}

func TestConfigOverlayEscDismisses(t *testing.T) {
	ov := newConfigOverlay(sampleConfigEntries(), nil)
	res, handled := ov.ApplyKey(tui.Key{Type: tui.KeyEsc})
	if !handled {
		t.Fatal("Esc should be handled")
	}
	if !res.Dismissed {
		t.Fatalf("Esc should dismiss, got %+v", res)
	}
}

func TestConfigOverlayEnterOnBoolToggleFalseToTrue(t *testing.T) {
	var writtenKey, writtenVal string
	entries := []configSettingEntry{
		{Key: "verbose", Label: "Verbose", Value: "false", Editable: true, Type: configTypeBool},
	}
	writer := func(key, val string) error {
		writtenKey = key
		writtenVal = val
		return nil
	}
	ov := newConfigOverlay(entries, writer)
	res, handled := ov.ApplyKey(tui.Key{Type: tui.KeyEnter})
	if !handled {
		t.Fatal("Enter should be handled")
	}
	if writtenKey != "verbose" {
		t.Fatalf("writer key = %q want verbose", writtenKey)
	}
	if writtenVal != "true" {
		t.Fatalf("writer val = %q want true", writtenVal)
	}
	if res.Submit != "config:verbose:true" {
		t.Fatalf("submit = %q want config:verbose:true", res.Submit)
	}
}

func TestConfigOverlayEnterOnBoolToggleTrueToFalse(t *testing.T) {
	var writtenVal string
	entries := []configSettingEntry{
		{Key: "verbose", Label: "Verbose", Value: "true", Editable: true, Type: configTypeBool},
	}
	writer := func(key, val string) error {
		writtenVal = val
		return nil
	}
	ov := newConfigOverlay(entries, writer)
	res, _ := ov.ApplyKey(tui.Key{Type: tui.KeyEnter})
	if writtenVal != "false" {
		t.Fatalf("toggle true→false: writer val = %q want false", writtenVal)
	}
	if res.Submit != "config:verbose:false" {
		t.Fatalf("submit = %q want config:verbose:false", res.Submit)
	}
}

func TestConfigOverlayNilWriterDoesNotPanic(t *testing.T) {
	entries := []configSettingEntry{
		{Key: "verbose", Label: "Verbose", Value: "false", Editable: true, Type: configTypeBool},
	}
	ov := newConfigOverlay(entries, nil)
	_, handled := ov.ApplyKey(tui.Key{Type: tui.KeyEnter})
	if !handled {
		t.Fatal("Enter should be handled even with nil writer")
	}
}

func TestConfigOverlayRenderNonEmpty(t *testing.T) {
	ov := newConfigOverlay(sampleConfigEntries(), nil)
	lines := ov.Render(80, 24)
	if len(lines) == 0 {
		t.Fatal("Render should return non-empty lines")
	}
}

func TestConfigOverlayRenderContainsLabel(t *testing.T) {
	ov := newConfigOverlay(sampleConfigEntries(), nil)
	lines := ov.Render(80, 24)
	found := false
	for _, l := range lines {
		if contains(l, "Model") {
			found = true
			break
		}
	}
	if !found {
		t.Fatalf("Render output missing label 'Model': %v", lines)
	}
}

func TestConfigOverlayDownAtBottomNoOp(t *testing.T) {
	entries := []configSettingEntry{
		{Key: "a", Label: "A", Value: "1", Editable: true},
	}
	ov := newConfigOverlay(entries, nil)
	ov.ApplyKey(tui.Key{Type: tui.KeyDown}) // already at bottom
	if ov.Cursor() != 0 {
		t.Fatalf("cursor at bottom after Down = %d want 0", ov.Cursor())
	}
}

func TestConfigHandlerWithOverlayReturnsOverlay(t *testing.T) {
	loader := func() ([]configSettingEntry, error) {
		return sampleConfigEntries(), nil
	}
	h := configHandlerWithOverlay(loader, nil)
	out, err := h(context.Background(), CommandContext{})
	if err != nil {
		t.Fatalf("handler err: %v", err)
	}
	if !out.Handled {
		t.Fatal("must be Handled")
	}
	if out.Overlay == nil {
		t.Fatal("handler must return an Overlay when entries are available")
	}
}

func TestConfigHandlerWithOverlayEmptyEntriesReturnsText(t *testing.T) {
	loader := func() ([]configSettingEntry, error) {
		return nil, nil
	}
	h := configHandlerWithOverlay(loader, nil)
	out, err := h(context.Background(), CommandContext{})
	if err != nil {
		t.Fatalf("handler err: %v", err)
	}
	if !out.Handled {
		t.Fatal("must be Handled")
	}
	// No entries → falls back to text summary
	if out.Status == "" {
		t.Fatal("empty entries: Status should be non-empty fallback text")
	}
}

func TestConfigOverlayDefaultEntriesFromSettings(t *testing.T) {
	// defaultConfigEntriesFromSettings should return a non-nil slice for non-zero settings.
	entries := defaultConfigEntriesFromSettings("claude-sonnet-4", "dark", false)
	if len(entries) == 0 {
		t.Fatal("defaultConfigEntriesFromSettings returned empty slice")
	}
	// Verify model entry present.
	found := false
	for _, e := range entries {
		if e.Key == "model" && e.Value == "claude-sonnet-4" {
			found = true
			break
		}
	}
	if !found {
		t.Fatalf("entries missing model=claude-sonnet-4: %v", entries)
	}
}
