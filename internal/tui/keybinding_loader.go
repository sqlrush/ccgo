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
	var bindings map[string]Action
	if err := json.Unmarshal(data, &bindings); err != nil {
		return nil, fmt.Errorf("parse keybinding spec map: %w", err)
	}
	keys := make([]string, 0, len(bindings))
	for key := range bindings {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	specs := make([]BindingSpec, 0, len(keys))
	for _, key := range keys {
		specs = append(specs, BindingSpec{Key: key, Action: bindings[key]})
	}
	return specs, nil
}

func parseKeyBindingWrapper(data []byte) ([]BindingSpec, bool, error) {
	var wrapper struct {
		Bindings         []BindingSpec `json:"bindings"`
		Keybindings      []BindingSpec `json:"keybindings"`
		KeyBindings      []BindingSpec `json:"keyBindings"`
		KeyboardBindings []BindingSpec `json:"keyboardBindings"`
	}
	if err := json.Unmarshal(data, &wrapper); err != nil {
		return nil, false, fmt.Errorf("parse keybinding spec wrapper: %w", err)
	}
	total := len(wrapper.Bindings) + len(wrapper.Keybindings) + len(wrapper.KeyBindings) + len(wrapper.KeyboardBindings)
	if total == 0 {
		return nil, false, nil
	}
	specs := make([]BindingSpec, 0, total)
	specs = append(specs, wrapper.Bindings...)
	specs = append(specs, wrapper.Keybindings...)
	specs = append(specs, wrapper.KeyBindings...)
	specs = append(specs, wrapper.KeyboardBindings...)
	return specs, true, nil
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
		Command        *Action `json:"command"`
		ActionName     *Action `json:"action_name"`
		ActionNameAlt  *Action `json:"actionName"`
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
	if fields.Command != nil {
		spec.Action = *fields.Command
	}
	if fields.ActionName != nil {
		spec.Action = *fields.ActionName
	}
	if fields.ActionNameAlt != nil {
		spec.Action = *fields.ActionNameAlt
	}
	return nil
}
