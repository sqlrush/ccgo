package tui

import (
	"bytes"
	"encoding/json"
	"fmt"
	"os"
	"sort"
	"strings"
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
	var fields map[string]json.RawMessage
	if err := json.Unmarshal(data, &fields); err != nil {
		return err
	}
	*spec = BindingSpec{}

	key, ok, err := bindingKeyField(fields, "Key", "key", "keys", "key_sequence", "keySequence", "shortcut", "shortcut_key", "shortcutKey", "sequence")
	if err != nil {
		return err
	}
	if ok {
		spec.Key = key
	}

	action, ok, err := bindingActionField(fields, "Action", "action", "command", "action_name", "actionName", "command_name", "commandName", "command_id", "commandId")
	if err != nil {
		return err
	}
	if ok {
		spec.Action = action
	}
	return nil
}

func bindingKeyField(fields map[string]json.RawMessage, names ...string) (string, bool, error) {
	for _, name := range names {
		data, ok := fields[name]
		if !ok {
			continue
		}
		data = bytes.TrimSpace(data)
		if len(data) == 0 || bytes.Equal(data, []byte("null")) {
			return "", true, nil
		}
		if data[0] == '"' {
			var key string
			if err := json.Unmarshal(data, &key); err != nil {
				return "", false, fmt.Errorf("%s: %w", name, err)
			}
			return key, true, nil
		}
		if data[0] == '[' {
			var keys []string
			if err := json.Unmarshal(data, &keys); err != nil {
				return "", false, fmt.Errorf("%s: %w", name, err)
			}
			return strings.Join(keys, " "), true, nil
		}
		return "", false, fmt.Errorf("%s must be a string, string array, or null", name)
	}
	return "", false, nil
}

func bindingActionField(fields map[string]json.RawMessage, names ...string) (Action, bool, error) {
	for _, name := range names {
		data, ok := fields[name]
		if !ok {
			continue
		}
		data = bytes.TrimSpace(data)
		if len(data) == 0 || bytes.Equal(data, []byte("null")) {
			return ActionNone, true, nil
		}
		if data[0] == '"' {
			var action Action
			if err := json.Unmarshal(data, &action); err != nil {
				return "", false, fmt.Errorf("%s: %w", name, err)
			}
			return action, true, nil
		}
		var enabled bool
		if err := json.Unmarshal(data, &enabled); err == nil {
			if !enabled {
				return ActionNone, true, nil
			}
			return "", false, fmt.Errorf("%s boolean true must use an action name", name)
		}
		return "", false, fmt.Errorf("%s must be an action string, null, or false", name)
	}
	return "", false, nil
}
