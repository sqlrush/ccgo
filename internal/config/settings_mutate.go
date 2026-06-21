package config

import (
	"fmt"
	"strings"
)

// SetSettingsValue read-modify-writes a single top-level key in the settings
// document at path, preserving all other keys. It creates the file (and parent
// dir) if missing. A value of nil deletes the key.
func SetSettingsValue(path string, key string, value any) error {
	key = strings.TrimSpace(key)
	if key == "" {
		return fmt.Errorf("settings key must be non-empty")
	}
	doc, err := ReadSettingsDocument(path)
	if err != nil {
		return fmt.Errorf("read settings %s: %w", path, err)
	}
	if doc == nil {
		doc = map[string]any{}
	}
	if value == nil {
		delete(doc, key)
	} else {
		doc[key] = value
	}
	if err := WriteSettingsDocument(path, doc); err != nil {
		return fmt.Errorf("write settings %s: %w", path, err)
	}
	return nil
}
