package plugins

// plugin_settings_base.go — PLUGIN-20
//
// CC ref: utils/settings/settingsCache.ts getPluginSettingsBase;
//         utils/plugins/pluginLoader.ts loadPluginSettings (PluginSettingsSchema picks only "agent").
//
// Each installed plugin may ship a settings.json that contributes the "agent"
// key (the only allowlisted key in PluginSettingsSchema). These are merged
// together and exposed as the lowest-priority settings base via PluginSettingsBase.
// All file-based sources (user/project/local/policy) override plugin settings.

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"

	"ccgo/internal/contracts"
)

const pluginSettingsFileName = "settings.json"

// PluginSettingsBase loads plugin-contributed settings from every installed
// plugin directory and merges them into a single contracts.Settings value.
// Only the "agent" key is allowlisted (mirrors PluginSettingsSchema in CC).
// Plugin settings are the lowest priority — all user/project/managed sources
// override them. When multiple plugins set "agent", the last one wins.
//
// CC ref: utils/settings/settingsCache.ts:66-79 (pluginSettingsBase global);
//         utils/plugins/pluginLoader.ts:1776-1799 (PluginSettingsSchema + parsePluginSettings).
func PluginSettingsBase(pluginRoots []string, settings contracts.Settings) contracts.Settings {
	plugins := LoadPluginDirsWithSettings(pluginRoots, settings)
	return pluginSettingsFromLoaded(plugins)
}

// pluginSettingsFromLoaded merges "agent" settings contributed by each loaded plugin.
func pluginSettingsFromLoaded(plugins []LoadedPlugin) contracts.Settings {
	var base contracts.Settings
	for _, plugin := range plugins {
		s := loadPluginSettings(plugin.Root)
		if s.Agent != "" {
			base.Agent = s.Agent
		}
	}
	return base
}

// loadPluginSettings reads settings.json from a plugin root directory and
// extracts the allowlisted "agent" field (PLUGIN-20).
// On any error or missing file, an empty Settings is returned (non-fatal).
func loadPluginSettings(pluginRoot string) contracts.Settings {
	path := filepath.Join(cleanAbs(pluginRoot), pluginSettingsFileName)
	data, err := os.ReadFile(path)
	if err != nil {
		// Missing settings.json is expected — most plugins don't have one.
		return contracts.Settings{}
	}
	data = []byte(strings.TrimSpace(string(data)))
	if len(data) == 0 {
		return contracts.Settings{}
	}
	var raw map[string]any
	if err := json.Unmarshal(data, &raw); err != nil {
		return contracts.Settings{}
	}
	// Only "agent" is allowlisted — mirrors PluginSettingsSchema().pick({agent:true}).
	var s contracts.Settings
	if agent, ok := raw["agent"].(string); ok {
		s.Agent = strings.TrimSpace(agent)
	}
	return s
}
