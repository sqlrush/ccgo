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
	if text, ok := keyBindingProviderResponseText(data); ok {
		specs, err := ParseKeyBindingSpecs([]byte(text))
		if err != nil {
			return nil, fmt.Errorf("parse keybinding provider response: %w", err)
		}
		return specs, nil
	}
	specs, err := parseKeyBindingMap(data)
	if err != nil {
		return nil, fmt.Errorf("parse keybinding spec map: %w", err)
	}
	return specs, nil
}

var keyBindingCollectionFields = []string{
	"bindings",
	"keybindings",
	"keyBindings",
	"keyboardBindings",
	"keyboardShortcuts",
	"keyboard_shortcuts",
	"keybinding_specs",
	"keybindingSpecs",
	"keymap",
	"keyMap",
	"keymaps",
	"keyMaps",
	"shortcuts",
	"shortcutBindings",
	"hotkeys",
	"hotKeys",
	"hot_keys",
	"userKeybindings",
	"userKeyBindings",
	"user_keybindings",
	"customKeybindings",
	"customKeyBindings",
	"custom_keybindings",
}

var keyBindingOuterWrapperFields = []string{
	"data", "payload", "body", "result", "response",
	"viewer", "node", "connection", "keybindingConnection", "keybindingsConnection", "keyboardShortcutConnection", "keyboardShortcutsConnection",
	"resource", "attributes", "properties", "attrs",
	"settings", "config", "configuration", "keyboard", "keymap", "preferences", "userPreferences",
}

var keyBindingArrayWrapperFields = []string{"data", "payload", "body", "result", "response", "resources", "included", "collection", "collections", "list", "lists", "children", "values", "nodes", "items", "edges"}

func parseKeyBindingWrapper(data []byte) ([]BindingSpec, bool, error) {
	return parseKeyBindingWrapperDepth(data, 0)
}

func parseKeyBindingWrapperDepth(data []byte, depth int) ([]BindingSpec, bool, error) {
	if depth > 4 {
		return nil, false, nil
	}
	var raw map[string]json.RawMessage
	if err := json.Unmarshal(data, &raw); err != nil {
		return nil, false, fmt.Errorf("parse keybinding spec wrapper: %w", err)
	}
	var specs []BindingSpec
	for _, name := range keyBindingCollectionFields {
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
		for _, name := range keyBindingArrayWrapperFields {
			value, ok := raw[name]
			if !ok {
				continue
			}
			parsed, ok, err := parseKeyBindingOptionalSpecValue(name, value, depth+1)
			if ok || err != nil {
				return parsed, ok, err
			}
		}
		for _, name := range keyBindingOuterWrapperFields {
			value, ok := raw[name]
			if !ok {
				continue
			}
			value = bytes.TrimSpace(value)
			if len(value) == 0 || value[0] != '{' {
				continue
			}
			parsed, ok, err := parseKeyBindingWrapperDepth(value, depth+1)
			if ok || err != nil {
				return parsed, ok, err
			}
		}
		return nil, false, nil
	}
	return specs, true, nil
}

func keyBindingProviderResponseText(data []byte) (string, bool) {
	var raw map[string]json.RawMessage
	if err := json.Unmarshal(data, &raw); err != nil {
		return "", false
	}
	for _, name := range []string{
		"choice",
		"choices",
		"output",
		"outputs",
		"candidate",
		"candidates",
		"generation",
		"generations",
		"completion",
		"completions",
		"response",
		"responses",
		"result",
		"results",
	} {
		value, ok := raw[name]
		if !ok {
			continue
		}
		if text, ok := interactionScriptProviderTextFromRaw(value, 0, false); ok {
			if payload, ok := interactionScriptProviderJSONPayload(text); ok {
				return payload, true
			}
			return text, true
		}
	}
	return "", false
}

func parseKeyBindingOptionalSpecValue(name string, data json.RawMessage, depth int) ([]BindingSpec, bool, error) {
	data = bytes.TrimSpace(data)
	if len(data) == 0 || bytes.Equal(data, []byte("null")) {
		return nil, false, nil
	}
	switch data[0] {
	case '[':
		specs, err := parseKeyBindingSpecValue(name, data)
		return specs, true, err
	case '{':
		specs, ok, err := parseKeyBindingWrapperDepth(data, depth)
		return specs, ok, err
	default:
		return nil, false, nil
	}
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
		specs, ok, err := parseKeyBindingWrapper(data)
		if ok || err != nil {
			return specs, err
		}
		specs, err = parseKeyBindingMap(data)
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
	data = unwrapBindingSpecJSON(data)
	var fields map[string]json.RawMessage
	if err := json.Unmarshal(data, &fields); err != nil {
		return err
	}
	*spec = BindingSpec{}

	key, ok, err := bindingKeyField(fields, "Key", "key", "keys", "key_sequence", "keySequence", "shortcut", "shortcut_key", "shortcutKey", "shortcut_keys", "shortcutKeys", "sequence", "accelerator", "accelerators", "keystroke", "keyStroke", "hotkey", "hotKey", "key_combo", "keyCombo", "chord", "keyChord")
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

func unwrapBindingSpecJSON(data []byte) []byte {
	var fields map[string]json.RawMessage
	if err := json.Unmarshal(data, &fields); err != nil {
		return data
	}
	if bindingSpecJSONHasDirectFields(fields) {
		return data
	}
	for _, name := range []string{
		"binding",
		"keybinding",
		"keyBinding",
		"shortcutBinding",
		"entry",
		"item",
		"edge",
		"node",
		"resource",
		"attributes",
		"properties",
		"attrs",
	} {
		raw, ok := fields[name]
		if !ok {
			continue
		}
		raw = bytes.TrimSpace(raw)
		if len(raw) > 0 && raw[0] == '{' {
			return unwrapBindingSpecJSON(raw)
		}
	}
	return data
}

func bindingSpecJSONHasDirectFields(fields map[string]json.RawMessage) bool {
	for _, name := range []string{
		"Key", "key", "keys", "key_sequence", "keySequence", "shortcut", "shortcut_key", "shortcutKey", "shortcut_keys", "shortcutKeys", "sequence", "accelerator", "accelerators", "keystroke", "keyStroke", "hotkey", "hotKey", "key_combo", "keyCombo", "chord", "keyChord",
		"Action", "action", "command", "action_name", "actionName", "command_name", "commandName", "command_id", "commandId",
	} {
		if raw, ok := fields[name]; ok && len(bytes.TrimSpace(raw)) > 0 && !bytes.Equal(bytes.TrimSpace(raw), []byte("null")) {
			return true
		}
	}
	return false
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
