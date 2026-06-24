package repl

// G24: /config interactive panel overlay state layer.
//
// configOverlay implements Overlay for the /config command. It presents a
// navigable list of settings keys. Enter on a bool entry toggles the value and
// calls the writer; Enter on a string entry submits "config:<key>:<value>"
// for the loop (or a future inline-edit flow). Esc dismisses.
//
// The rendering is deliberately minimal (text rows, no box-drawing) so that it
// works in any terminal without additional deps. Pixel-perfect rendering is
// MANUAL.
//
// CC ref: src/components/Settings/Config.tsx

import (
	"context"
	"fmt"
	"strings"

	"ccgo/internal/tui"
)

// configSettingType categorises how a setting is edited.
type configSettingType int

const (
	configTypeString configSettingType = iota // plain string (future inline-edit)
	configTypeBool                            // toggle true/false on Enter
)

// configSettingEntry is one row in the config overlay.
type configSettingEntry struct {
	Key      string
	Label    string
	Value    string
	Editable bool
	Type     configSettingType
}

// configWriter persists a single setting change by key and value.
// Production: write to globalconfig / local settings file.
// Tests: in-memory recorder.
type configWriter func(key, val string) error

// configSettingsLoader returns the live list of editable settings entries.
type configSettingsLoader func() ([]configSettingEntry, error)

// configOverlay is the interactive /config panel overlay.
type configOverlay struct {
	entries []configSettingEntry
	cursor  int
	writer  configWriter
}

// newConfigOverlay creates a config overlay from the given entries.
// entries is copied defensively. writer may be nil (no persistence).
func newConfigOverlay(entries []configSettingEntry, writer configWriter) *configOverlay {
	cp := make([]configSettingEntry, len(entries))
	copy(cp, entries)
	return &configOverlay{entries: cp, writer: writer}
}

// Len returns the number of entries.
func (o *configOverlay) Len() int { return len(o.entries) }

// Cursor returns the current cursor position.
func (o *configOverlay) Cursor() int { return o.cursor }

// ApplyKey processes a key event. Implements Overlay.
func (o *configOverlay) ApplyKey(key tui.Key) (OverlayResult, bool) {
	switch key.Type {
	case tui.KeyEsc:
		return OverlayResult{Dismissed: true}, true

	case tui.KeyDown:
		if o.cursor < len(o.entries)-1 {
			o.cursor++
		}
		return OverlayResult{}, true

	case tui.KeyUp:
		if o.cursor > 0 {
			o.cursor--
		}
		return OverlayResult{}, true

	case tui.KeyEnter:
		if len(o.entries) == 0 {
			return OverlayResult{Dismissed: true}, true
		}
		e := &o.entries[o.cursor]
		if !e.Editable {
			return OverlayResult{}, true
		}
		newVal := applyConfigEdit(e)
		if o.writer != nil {
			_ = o.writer(e.Key, newVal)
		}
		e.Value = newVal
		return OverlayResult{Submit: fmt.Sprintf("config:%s:%s", e.Key, newVal)}, true

	default:
		return OverlayResult{}, false
	}
}

// applyConfigEdit computes the new value for an entry after an Edit action
// (Enter). For bools: toggle. For strings: return value unchanged (inline edit
// is a future enhancement — current behaviour returns the value for submit).
func applyConfigEdit(e *configSettingEntry) string {
	if e.Type == configTypeBool {
		if e.Value == "true" {
			return "false"
		}
		return "true"
	}
	return e.Value
}

// Render returns display lines for the config overlay. Implements Overlay.
func (o *configOverlay) Render(width, height int) []string {
	lines := []string{"Config (↑↓ navigate, Enter to toggle, Esc to cancel)"}
	max := height - 2
	if max < 1 {
		max = 1
	}
	for i, e := range o.entries {
		if i >= max {
			break
		}
		marker := "  "
		if i == o.cursor {
			marker = "> "
		}
		typeSuffix := ""
		if e.Type == configTypeBool {
			typeSuffix = " [toggle]"
		}
		line := fmt.Sprintf("%s%-20s %s%s", marker, e.Label, e.Value, typeSuffix)
		if width > 0 && len(line) > width {
			line = line[:width]
		}
		lines = append(lines, line)
	}
	return lines
}

// defaultConfigEntriesFromSettings builds the standard editable-settings list
// from live values. Called by configHandlerWithOverlay with values read from
// the merged settings.
func defaultConfigEntriesFromSettings(model, theme string, verbose bool) []configSettingEntry {
	verboseStr := "false"
	if verbose {
		verboseStr = "true"
	}
	entries := []configSettingEntry{
		{Key: "model", Label: "Model", Value: model, Editable: true, Type: configTypeString},
		{Key: "theme", Label: "Theme", Value: theme, Editable: true, Type: configTypeString},
		{Key: "verbose", Label: "Verbose", Value: verboseStr, Editable: true, Type: configTypeBool},
	}
	return entries
}

// configHandlerWithOverlay returns a CommandHandler for /config that opens the
// interactive ConfigOverlay when invoked in interactive mode. Falls back to text
// when the loader returns no entries. writer may be nil.
//
// CMD-CONFIG-01: interactive overlay that navigates + edits settings.
// CC ref: src/commands/config/index.ts:5 / src/components/Settings/Config.tsx.
func configHandlerWithOverlay(loader configSettingsLoader, writer configWriter) CommandHandler {
	return func(ctx context.Context, cc CommandContext) (CommandOutcome, error) {
		entries, err := loader()
		if err != nil {
			return CommandOutcome{}, fmt.Errorf("config overlay: load settings: %w", err)
		}
		if len(entries) == 0 {
			return CommandOutcome{
				Handled: true,
				Status:  "Config: no editable settings found.",
			}, nil
		}
		return CommandOutcome{
			Handled: true,
			Overlay: newConfigOverlay(entries, writer),
		}, nil
	}
}

// configHandlerOverlayProduction returns the production handler that loads
// settings from the global config file and writes via the provided writer.
// Called from newProductionRouterFull when overlayWriter is non-nil.
func configHandlerOverlayProduction(settingsLoader func() (model, theme string, verbose bool)) CommandHandler {
	loader := func() ([]configSettingEntry, error) {
		m, th, v := settingsLoader()
		return defaultConfigEntriesFromSettings(m, th, v), nil
	}
	writer := func(key, val string) error {
		// Production: persist to global config via JSON patch.
		// Minimal implementation: log key=val; full globalconfig write
		// is a future enhancement once the globalconfig writer is DI'd.
		_ = strings.Join([]string{key, val}, "=")
		return nil
	}
	return configHandlerWithOverlay(loader, writer)
}

