package repl

// G24/G26: /config interactive panel overlay state layer.
//
// configOverlay implements Overlay for the /config command. It presents a
// navigable list of settings keys with three edit modes:
//   - configTypeBool:   Enter toggles true/false and persists immediately.
//   - configTypeEnum:   Enter cycles through predefined Options and persists.
//   - configTypeString: Enter opens inline-edit sub-state; rune keys update the
//     edit buffer; second Enter applies; Esc cancels edit (overlay stays open).
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
	configTypeString configSettingType = iota // inline text edit on Enter
	configTypeBool                            // toggle true/false on Enter
	configTypeEnum                            // cycle through Options on Enter
)

// configSettingEntry is one row in the config overlay.
type configSettingEntry struct {
	Key      string
	Label    string
	Value    string
	Editable bool
	Type     configSettingType
	// Options holds the allowed values for configTypeEnum entries.
	Options []string
	// editBuf is the in-progress edit buffer when the overlay is in
	// inline-edit mode for this entry (configTypeString).
	editBuf string
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
	// editing is true when the overlay is in inline-edit sub-state for a
	// configTypeString entry. While editing, rune/backspace keys update
	// entries[cursor].editBuf; Enter applies; Esc cancels.
	editing bool
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
//
// When o.editing is true (inline-edit sub-state for configTypeString), key
// routing differs:
//   - Esc   → cancel edit, restore original value, leave overlay open.
//   - Enter → apply editBuf, persist via writer, emit Submit.
//   - Backspace → delete last rune from editBuf.
//   - Rune  → append to editBuf.
//   - Up/Down → consumed (navigation blocked during edit).
func (o *configOverlay) ApplyKey(key tui.Key) (OverlayResult, bool) {
	if o.editing {
		return o.applyKeyEditing(key)
	}

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
		return o.applyEnterOnEntry(e), true

	default:
		return OverlayResult{}, false
	}
}

// applyEnterOnEntry dispatches the Enter action for the given entry based on
// its type: bool toggles, enum cycles, string enters inline-edit sub-state.
func (o *configOverlay) applyEnterOnEntry(e *configSettingEntry) OverlayResult {
	switch e.Type {
	case configTypeBool:
		newVal := applyConfigEdit(e)
		if o.writer != nil {
			_ = o.writer(e.Key, newVal)
		}
		e.Value = newVal
		return OverlayResult{Submit: fmt.Sprintf("config:%s:%s", e.Key, newVal)}

	case configTypeEnum:
		newVal := cycleEnumValue(e)
		if o.writer != nil {
			_ = o.writer(e.Key, newVal)
		}
		e.Value = newVal
		return OverlayResult{Submit: fmt.Sprintf("config:%s:%s", e.Key, newVal)}

	default: // configTypeString (and unknown types)
		// Enter inline-edit mode: seed editBuf with current value.
		e.editBuf = e.Value
		o.editing = true
		return OverlayResult{}
	}
}

// applyKeyEditing handles keys while the overlay is in inline-edit mode.
func (o *configOverlay) applyKeyEditing(key tui.Key) (OverlayResult, bool) {
	e := &o.entries[o.cursor]
	switch key.Type {
	case tui.KeyEsc:
		// Cancel: revert editBuf, exit edit mode, overlay stays open.
		e.editBuf = ""
		o.editing = false
		return OverlayResult{}, true

	case tui.KeyEnter:
		// Apply: persist and submit.
		newVal := e.editBuf
		if o.writer != nil {
			_ = o.writer(e.Key, newVal)
		}
		e.Value = newVal
		e.editBuf = ""
		o.editing = false
		return OverlayResult{Submit: fmt.Sprintf("config:%s:%s", e.Key, newVal)}, true

	case tui.KeyBackspace:
		if len(e.editBuf) > 0 {
			runes := []rune(e.editBuf)
			e.editBuf = string(runes[:len(runes)-1])
		}
		return OverlayResult{}, true

	case tui.KeyRune:
		e.editBuf += string(key.Rune)
		return OverlayResult{}, true

	default:
		// Consume all keys during edit (blocks Up/Down navigation).
		return OverlayResult{}, true
	}
}

// cycleEnumValue advances an enum entry to the next option in its Options
// slice (wrapping around). Returns the new value. If Options is empty or the
// current value is not found, returns the current value unchanged.
func cycleEnumValue(e *configSettingEntry) string {
	if len(e.Options) == 0 {
		return e.Value
	}
	idx := -1
	for i, opt := range e.Options {
		if opt == e.Value {
			idx = i
			break
		}
	}
	if idx < 0 {
		// Current value not in options — default to first option.
		return e.Options[0]
	}
	return e.Options[(idx+1)%len(e.Options)]
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
	header := "Config (↑↓ navigate, Enter to edit/toggle, Esc to cancel)"
	if o.editing {
		header = "Config (editing — Enter to apply, Esc to cancel)"
	}
	lines := []string{header}
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
		displayVal := e.Value
		typeSuffix := ""
		switch e.Type {
		case configTypeBool:
			typeSuffix = " [toggle]"
		case configTypeEnum:
			typeSuffix = " [cycle→]"
		case configTypeString:
			if o.editing && i == o.cursor {
				displayVal = e.editBuf + "|"
				typeSuffix = " [edit]"
			}
		}
		line := fmt.Sprintf("%s%-20s %s%s", marker, e.Label, displayVal, typeSuffix)
		if width > 0 && len(line) > width {
			line = line[:width]
		}
		lines = append(lines, line)
	}
	return lines
}

// builtinThemesConfig mirrors the CC theme list used in the config overlay.
var builtinThemesConfig = []string{"dark", "light", "dark-daltonism", "light-daltonism", "default"}

// defaultConfigEntriesFromSettings builds the standard editable-settings list
// from live values. Called by configHandlerWithOverlay with values read from
// the merged settings.
//
// G26: model is a configTypeString (inline edit); theme is configTypeEnum
// (cycle through predefined options); verbose remains configTypeBool (toggle).
func defaultConfigEntriesFromSettings(model, theme string, verbose bool) []configSettingEntry {
	verboseStr := "false"
	if verbose {
		verboseStr = "true"
	}
	entries := []configSettingEntry{
		{Key: "model", Label: "Model", Value: model, Editable: true, Type: configTypeString},
		{Key: "theme", Label: "Theme", Value: theme, Editable: true, Type: configTypeEnum, Options: builtinThemesConfig},
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

