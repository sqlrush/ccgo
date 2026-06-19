package config

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

func ReadUserSettingsDocument() (map[string]any, error) {
	path := UserSettingsPath()
	document := map[string]any{}
	data, err := os.ReadFile(path)
	if err == nil && len(strings.TrimSpace(string(data))) > 0 {
		if err := json.Unmarshal(data, &document); err != nil {
			return nil, fmt.Errorf("decode %s: %w", path, err)
		}
	} else if err != nil && !os.IsNotExist(err) {
		return nil, err
	}
	return document, nil
}

func WriteUserSettingsDocument(document map[string]any) error {
	path := UserSettingsPath()
	data, err := json.MarshalIndent(document, "", "  ")
	if err != nil {
		return err
	}
	data = append(data, '\n')
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return err
	}
	return os.WriteFile(path, data, 0o600)
}

func SetUserPluginEnabled(name string, enabled bool) error {
	name = strings.TrimSpace(name)
	if name == "" {
		return fmt.Errorf("plugin name is required")
	}
	return SetUserPluginsEnabled(map[string]bool{name: enabled})
}

func SetUserPluginsEnabled(states map[string]bool) error {
	document, err := ReadUserSettingsDocument()
	if err != nil {
		return err
	}
	enabledPlugins, _ := document["enabledPlugins"].(map[string]any)
	if enabledPlugins == nil {
		enabledPlugins = map[string]any{}
	}
	for name, enabled := range states {
		name = strings.TrimSpace(name)
		if name == "" {
			continue
		}
		enabledPlugins[name] = enabled
	}
	document["enabledPlugins"] = enabledPlugins
	return WriteUserSettingsDocument(document)
}
