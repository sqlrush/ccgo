package conversation

import (
	"ccgo/internal/config"
	"ccgo/internal/contracts"
)

func (c *MCPConfig) MergedSettings() contracts.Settings {
	if c == nil {
		return contracts.Settings{}
	}
	return config.MergeSettings(c.UserSettings, c.ProjectSettings, c.LocalSettings, c.PolicySettings)
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
