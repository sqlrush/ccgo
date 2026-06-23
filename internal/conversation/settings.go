package conversation

import (
	"ccgo/internal/config"
	"ccgo/internal/contracts"
	pluginpkg "ccgo/internal/plugins"
)

func (c *MCPConfig) MergedSettings() contracts.Settings {
	if c == nil {
		return contracts.Settings{}
	}
	// PLUGIN-20: plugin settings.json contributes "agent" as the lowest-priority
	// base (below user/project/local/policy). Mirrors CC getPluginSettingsBase().
	// CC ref: utils/settings/settingsCache.ts:66-79; utils/settings/settings.ts:660-668.
	pluginBase := contracts.Settings{}
	if c.CWD != "" {
		pluginBase = pluginpkg.PluginSettingsBase(pluginpkg.InstalledPluginDirs(c.CWD), contracts.Settings{
			EnabledPlugins:         c.UserSettings.EnabledPlugins,
			ExtraKnownMarketplaces: c.UserSettings.ExtraKnownMarketplaces,
		})
	}
	return config.MergeSettings(pluginBase, c.UserSettings, c.ProjectSettings, c.LocalSettings, c.PolicySettings)
}

func (c *MCPConfig) SettingsSources() []config.SourceSettings {
	if c == nil {
		return nil
	}
	return []config.SourceSettings{
		{Source: contracts.PermissionSourceUserSettings, Settings: c.UserSettings},
		{Source: contracts.PermissionSourceProjectSettings, Settings: c.ProjectSettings},
		{Source: contracts.PermissionSourceLocalSettings, Settings: c.LocalSettings},
		{Source: contracts.PermissionSourcePolicySettings, Settings: c.PolicySettings},
	}
}
