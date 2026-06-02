package tui

import (
	"bytes"
	"encoding/json"
	"fmt"
	"os"
	"sort"
)

// LoadKeyBindingSpecs reads keybinding specs from a JSON array, object map, or wrapper object.
func LoadKeyBindingSpecs(path string) ([]BindingSpec, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("load keybinding specs %q: %w", path, err)
	}
	return ParseKeyBindingSpecs(data)
}

// ParseKeyBindingSpecs parses keybinding specs from JSON arrays, object maps, or wrapper objects.
func ParseKeyBindingSpecs(data []byte) ([]BindingSpec, error) {
	data = bytes.TrimSpace(data)
	if len(data) == 0 {
		return nil, nil
	}
	if data[0] == '[' {
		var specs []BindingSpec
		if err := json.Unmarshal(data, &specs); err != nil {
			return nil, fmt.Errorf("parse keybinding spec array: %w", err)
		}
		return specs, nil
	}

	if specs, ok, err := parseKeyBindingWrapper(data); ok || err != nil {
		return specs, err
	}
	specs, err := parseKeyBindingMap(data)
	if err != nil {
		return nil, fmt.Errorf("parse keybinding spec map: %w", err)
	}
	return specs, nil
}

func parseKeyBindingWrapper(data []byte) ([]BindingSpec, bool, error) {
	var raw map[string]json.RawMessage
	if err := json.Unmarshal(data, &raw); err != nil {
		return nil, false, fmt.Errorf("parse keybinding spec wrapper: %w", err)
	}
	var specs []BindingSpec
	for _, name := range []string{"bindings", "keybindings", "keyBindings", "keyboardBindings", "keybinding_specs", "keybindingSpecs", "shortcuts", "shortcutBindings"} {
		value, ok := raw[name]
		if !ok {
			continue
		}
		parsed, err := parseKeyBindingSpecValue(name, value)
		if err != nil {
			return nil, true, err
		}
		specs = append(specs, parsed...)
	}
	if len(specs) == 0 {
		return nil, false, nil
	}
	return specs, true, nil
}

func parseKeyBindingSpecValue(name string, data json.RawMessage) ([]BindingSpec, error) {
	data = bytes.TrimSpace(data)
	if len(data) == 0 || bytes.Equal(data, []byte("null")) {
		return nil, nil
	}
	switch data[0] {
	case '[':
		var specs []BindingSpec
		if err := json.Unmarshal(data, &specs); err != nil {
			return nil, fmt.Errorf("parse %s array: %w", name, err)
		}
		return specs, nil
	case '{':
		specs, err := parseKeyBindingMap(data)
		if err != nil {
			return nil, fmt.Errorf("parse %s map: %w", name, err)
		}
		return specs, nil
	default:
		return nil, fmt.Errorf("%s must be an array, object, or null", name)
	}
}

func parseKeyBindingMap(data json.RawMessage) ([]BindingSpec, error) {
	var bindings map[string]json.RawMessage
	if err := json.Unmarshal(data, &bindings); err != nil {
		return nil, err
	}
	keys := make([]string, 0, len(bindings))
	for key := range bindings {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	specs := make([]BindingSpec, 0, len(keys))
	for _, key := range keys {
		spec, err := parseKeyBindingMapEntry(key, bindings[key])
		if err != nil {
			return nil, err
		}
		specs = append(specs, spec)
	}
	return specs, nil
}

func parseKeyBindingMapEntry(key string, data json.RawMessage) (BindingSpec, error) {
	data = bytes.TrimSpace(data)
	if len(data) == 0 || bytes.Equal(data, []byte("null")) {
		return BindingSpec{Key: key, Action: ActionNone}, nil
	}
	if data[0] == '"' {
		var action Action
		if err := json.Unmarshal(data, &action); err != nil {
			return BindingSpec{}, fmt.Errorf("%s: %w", key, err)
		}
		return BindingSpec{Key: key, Action: action}, nil
	}
	if data[0] == '{' {
		var spec BindingSpec
		if err := json.Unmarshal(data, &spec); err != nil {
			return BindingSpec{}, fmt.Errorf("%s: %w", key, err)
		}
		if spec.Key == "" {
			spec.Key = key
		}
		return spec, nil
	}
	var enabled bool
	if err := json.Unmarshal(data, &enabled); err == nil {
		if !enabled {
			return BindingSpec{Key: key, Action: ActionNone}, nil
		}
		return BindingSpec{}, fmt.Errorf("%s boolean true must use an action name", key)
	}
	return BindingSpec{}, fmt.Errorf("%s must map to an action string, object, null, or false", key)
}

func (spec *BindingSpec) UnmarshalJSON(data []byte) error {
	type alias BindingSpec
	var raw alias
	if err := json.Unmarshal(data, &raw); err != nil {
		return err
	}
	*spec = BindingSpec(raw)

	var fields struct {
		Keys           *string `json:"keys"`
		KeySequence    *string `json:"key_sequence"`
		KeySequenceAlt *string `json:"keySequence"`
		Shortcut       *string `json:"shortcut"`
		ShortcutKey    *string `json:"shortcut_key"`
		ShortcutKeyAlt *string `json:"shortcutKey"`
		Sequence       *string `json:"sequence"`
		Command        *Action `json:"command"`
		ActionName     *Action `json:"action_name"`
		ActionNameAlt  *Action `json:"actionName"`
		CommandName    *Action `json:"command_name"`
		CommandNameAlt *Action `json:"commandName"`
		CommandID      *Action `json:"command_id"`
		CommandIDAlt   *Action `json:"commandId"`
	}
	if err := json.Unmarshal(data, &fields); err != nil {
		return err
	}
	if fields.Keys != nil {
		spec.Key = *fields.Keys
	}
	if fields.KeySequence != nil {
		spec.Key = *fields.KeySequence
	}
	if fields.KeySequenceAlt != nil {
		spec.Key = *fields.KeySequenceAlt
	}
	if fields.Shortcut != nil {
		spec.Key = *fields.Shortcut
	}
	if fields.ShortcutKey != nil {
		spec.Key = *fields.ShortcutKey
	}
	if fields.ShortcutKeyAlt != nil {
		spec.Key = *fields.ShortcutKeyAlt
	}
	if fields.Sequence != nil {
		spec.Key = *fields.Sequence
	}
	if fields.Command != nil {
		spec.Action = *fields.Command
	}
	if fields.ActionName != nil {
		spec.Action = *fields.ActionName
	}
	if fields.ActionNameAlt != nil {
		spec.Action = *fields.ActionNameAlt
	}
	if fields.CommandName != nil {
		spec.Action = *fields.CommandName
	}
	if fields.CommandNameAlt != nil {
		spec.Action = *fields.CommandNameAlt
	}
	if fields.CommandID != nil {
		spec.Action = *fields.CommandID
	}
	if fields.CommandIDAlt != nil {
		spec.Action = *fields.CommandIDAlt
	}
	return nil
}
