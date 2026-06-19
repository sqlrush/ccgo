package config

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"ccgo/internal/contracts"
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

func SetUserMarketplace(name string, source map[string]any, installLocation string) (bool, error) {
	name = strings.TrimSpace(name)
	if name == "" {
		return false, fmt.Errorf("marketplace name is required")
	}
	source = cloneAnyMap(source)
	entry := map[string]any{"source": source}
	if installLocation = strings.TrimSpace(installLocation); installLocation != "" {
		entry["installLocation"] = installLocation
	}
	if err := validateUserMarketplaceEntry(name, entry); err != nil {
		return false, err
	}
	document, err := ReadUserSettingsDocument()
	if err != nil {
		return false, err
	}
	extraKnown, ok := document["extraKnownMarketplaces"].(map[string]any)
	if !ok {
		if _, exists := document["extraKnownMarketplaces"]; exists {
			return false, fmt.Errorf("extraKnownMarketplaces must be an object")
		}
		extraKnown = map[string]any{}
	}
	_, existed := extraKnown[name]
	extraKnown[name] = entry
	document["extraKnownMarketplaces"] = extraKnown
	return existed, WriteUserSettingsDocument(document)
}

func RemoveUserMarketplace(name string) (bool, error) {
	name = strings.TrimSpace(name)
	if name == "" {
		return false, fmt.Errorf("marketplace name is required")
	}
	document, err := ReadUserSettingsDocument()
	if err != nil {
		return false, err
	}
	extraKnown, ok := document["extraKnownMarketplaces"].(map[string]any)
	if !ok {
		if _, exists := document["extraKnownMarketplaces"]; exists {
			return false, fmt.Errorf("extraKnownMarketplaces must be an object")
		}
		return false, nil
	}
	if _, ok := extraKnown[name]; !ok {
		return false, nil
	}
	delete(extraKnown, name)
	if len(extraKnown) == 0 {
		delete(document, "extraKnownMarketplaces")
	} else {
		document["extraKnownMarketplaces"] = extraKnown
	}
	return true, WriteUserSettingsDocument(document)
}

func validateUserMarketplaceEntry(name string, entry map[string]any) error {
	warnings := ValidateSettings(contracts.Settings{
		ExtraKnownMarketplaces: map[string]any{name: entry},
	}, UserSettingsPath())
	if len(warnings) == 0 {
		return nil
	}
	first := warnings[0]
	if first.Path != "" {
		return fmt.Errorf("%s: %s", first.Path, first.Message)
	}
	return fmt.Errorf("%s", first.Message)
}

func cloneAnyMap(values map[string]any) map[string]any {
	out := map[string]any{}
	for key, value := range values {
		if nested, ok := value.(map[string]any); ok {
			out[key] = cloneAnyMap(nested)
			continue
		}
		out[key] = value
	}
	return out
}
