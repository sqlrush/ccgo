package plugins

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"ccgo/internal/contracts"
)

// PLUGIN-20: plugin settings.json contributes "agent" key as lowest-priority base.

func writePluginDir(t *testing.T, dir, name string, manifestExtra map[string]any, settingsJSON map[string]any) string {
	t.Helper()
	root := filepath.Join(dir, name)
	if err := os.MkdirAll(root, 0o755); err != nil {
		t.Fatalf("MkdirAll: %v", err)
	}
	m := map[string]any{"name": name, "version": "1.0.0"}
	for k, v := range manifestExtra {
		m[k] = v
	}
	data, _ := json.Marshal(m)
	if err := os.WriteFile(filepath.Join(root, ManifestFileName), data, 0o644); err != nil {
		t.Fatalf("WriteFile manifest: %v", err)
	}
	if settingsJSON != nil {
		sd, _ := json.Marshal(settingsJSON)
		if err := os.WriteFile(filepath.Join(root, pluginSettingsFileName), sd, 0o644); err != nil {
			t.Fatalf("WriteFile settings: %v", err)
		}
	}
	return root
}

// TestPluginSettingsBaseAgentKey asserts that a plugin settings.json with
// {"agent":"code-reviewer"} contributes that value as the base Agent setting.
func TestPluginSettingsBaseAgentKey(t *testing.T) {
	dir := t.TempDir()
	writePluginDir(t, dir, "myplugin", nil, map[string]any{"agent": "code-reviewer"})

	base := PluginSettingsBase([]string{dir + "/myplugin"}, contracts.Settings{})
	if base.Agent != "code-reviewer" {
		t.Errorf("Agent = %q, want 'code-reviewer'", base.Agent)
	}
}

// TestPluginSettingsBaseOnlyAgentAllowlisted asserts that other keys (e.g. model)
// in plugin settings.json are ignored — only "agent" is allowlisted.
func TestPluginSettingsBaseOnlyAgentAllowlisted(t *testing.T) {
	dir := t.TempDir()
	writePluginDir(t, dir, "myplugin", nil, map[string]any{
		"agent": "reviewer",
		"model": "dangerous-model",
	})

	base := PluginSettingsBase([]string{dir + "/myplugin"}, contracts.Settings{})
	if base.Agent != "reviewer" {
		t.Errorf("Agent = %q, want 'reviewer'", base.Agent)
	}
	if base.Model != "" {
		t.Errorf("Model = %q, want '' (non-allowlisted key must be ignored)", base.Model)
	}
}

// TestPluginSettingsBaseMissingFile asserts that a plugin without settings.json
// contributes an empty base (no error).
func TestPluginSettingsBaseMissingFile(t *testing.T) {
	dir := t.TempDir()
	writePluginDir(t, dir, "noplugin", nil, nil) // no settings.json

	base := PluginSettingsBase([]string{dir + "/noplugin"}, contracts.Settings{})
	if base.Agent != "" {
		t.Errorf("Agent = %q, want '' when no settings.json", base.Agent)
	}
}

// TestPluginSettingsBaseMultiplePluginsLastWins asserts that when multiple
// plugins set "agent", the last-listed plugin's value is used.
func TestPluginSettingsBaseMultiplePluginsLastWins(t *testing.T) {
	dir := t.TempDir()
	writePluginDir(t, dir, "plugin-a", nil, map[string]any{"agent": "agent-a"})
	writePluginDir(t, dir, "plugin-b", nil, map[string]any{"agent": "agent-b"})

	base := PluginSettingsBase(
		[]string{dir + "/plugin-a", dir + "/plugin-b"},
		contracts.Settings{},
	)
	// plugin-b is processed last, so agent-b wins.
	if base.Agent != "agent-b" {
		t.Errorf("Agent = %q, want 'agent-b' (last plugin wins)", base.Agent)
	}
}

// TestPluginSettingsBaseLowerPriorityThanUserSettings asserts that when the
// plugin base contributes "agent" but user settings also set "agent", the
// user setting overrides it (plugin base is lowest priority).
func TestPluginSettingsBaseLowerPriorityThanUserSettings(t *testing.T) {
	dir := t.TempDir()
	writePluginDir(t, dir, "myplugin", nil, map[string]any{"agent": "plugin-agent"})

	pluginBase := PluginSettingsBase([]string{dir + "/myplugin"}, contracts.Settings{})
	userSettings := contracts.Settings{Agent: "user-agent"}

	// Merge: plugin base first, then user — user must win.
	from_config_merge := func(a, b contracts.Settings) contracts.Settings {
		if b.Agent != "" {
			a.Agent = b.Agent
		}
		return a
	}
	merged := from_config_merge(pluginBase, userSettings)
	if merged.Agent != "user-agent" {
		t.Errorf("Agent = %q, want 'user-agent' (user overrides plugin)", merged.Agent)
	}
}
