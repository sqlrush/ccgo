package conversation

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"reflect"

	"ccgo/internal/config"
	"ccgo/internal/contracts"
	"ccgo/internal/mcp"
	pluginpkg "ccgo/internal/plugins"
)

func LoadMCPConfigFromSettingsFiles(cwd string) (*MCPConfig, error) {
	resolvedCWD, err := resolveMCPConfigCWD(cwd)
	if err != nil {
		return nil, err
	}
	userSettings, err := loadOptionalSettings(config.UserSettingsPath())
	if err != nil {
		return nil, err
	}
	projectSettings, err := loadOptionalSettings(config.ProjectSettingsPath(resolvedCWD))
	if err != nil {
		return nil, err
	}
	policySettings, err := config.LoadPolicySettings()
	if err != nil {
		return nil, err
	}
	localSettings, err := loadOptionalSettings(config.LocalSettingsPath(resolvedCWD))
	if err != nil {
		return nil, err
	}
	mergedSettings := config.MergeSettings(userSettings, projectSettings, localSettings, policySettings)
	settingsFileDetector, err := config.NewSettingsChangeDetector(mcpConfigSettingsFilePaths(resolvedCWD))
	if err != nil {
		return nil, err
	}
	return &MCPConfig{
		UserSettings:         userSettings,
		ProjectSettings:      projectSettings,
		LocalSettings:        localSettings,
		PolicySettings:       policySettings,
		PluginServers:        pluginpkg.LoadMCPServersWithSettings(pluginpkg.ProjectPluginDirs(resolvedCWD), mergedSettings),
		CWD:                  resolvedCWD,
		settingsFileDetector: settingsFileDetector,
		ToolOptions: mcp.ServerToolOptions{
			AccessTokenProvider: mcp.FileOAuthAccessTokenProvider(mcp.FileOAuthAccessTokenProviderOptions{}),
		},
	}, nil
}

func (c *MCPConfig) RefreshSettingsFiles() (bool, error) {
	if c == nil || c.settingsFileDetector == nil {
		return false, nil
	}
	changes, err := c.settingsFileDetector.DetectChanges(mcpConfigSettingsFilePaths(c.CWD))
	if err != nil {
		return false, err
	}
	if len(changes) == 0 {
		return false, nil
	}
	userSettings, projectSettings, localSettings, err := loadMCPConfigSettingsFiles(c.CWD)
	if err != nil {
		return false, err
	}
	changed := !reflect.DeepEqual(c.UserSettings, userSettings) ||
		!reflect.DeepEqual(c.ProjectSettings, projectSettings) ||
		!reflect.DeepEqual(c.LocalSettings, localSettings)
	c.UserSettings = userSettings
	c.ProjectSettings = projectSettings
	c.LocalSettings = localSettings
	c.refreshPluginServers()
	return changed, nil
}

func (c *MCPConfig) RefreshPolicySettings() (bool, error) {
	if c == nil {
		return false, nil
	}
	policySettings, err := config.LoadPolicySettings()
	if err != nil {
		return false, err
	}
	changed := !reflect.DeepEqual(c.PolicySettings, policySettings)
	c.PolicySettings = policySettings
	c.refreshPluginServers()
	return changed, nil
}

func (c *MCPConfig) refreshPluginServers() {
	if c == nil {
		return
	}
	if c.CWD != "" {
		c.PluginServers = pluginpkg.LoadMCPServersWithSettings(pluginpkg.ProjectPluginDirs(c.CWD), c.MergedSettings())
	} else {
		c.PluginServers = nil
	}
}

func resolveMCPConfigCWD(cwd string) (string, error) {
	if cwd == "" {
		var err error
		cwd, err = os.Getwd()
		if err != nil {
			return "", err
		}
	}
	abs, err := filepath.Abs(cwd)
	if err != nil {
		return "", err
	}
	if resolved, err := filepath.EvalSymlinks(abs); err == nil {
		abs = resolved
	}
	return abs, nil
}

func loadOptionalSettings(path string) (contracts.Settings, error) {
	settings, err := config.LoadSettingsFile(path)
	if err == nil {
		return settings, nil
	}
	if errors.Is(err, os.ErrNotExist) {
		return contracts.Settings{}, nil
	}
	return contracts.Settings{}, fmt.Errorf("load settings %s: %w", path, err)
}

func loadMCPConfigSettingsFiles(cwd string) (contracts.Settings, contracts.Settings, contracts.Settings, error) {
	userSettings, err := loadOptionalSettings(config.UserSettingsPath())
	if err != nil {
		return contracts.Settings{}, contracts.Settings{}, contracts.Settings{}, err
	}
	if cwd == "" {
		return userSettings, contracts.Settings{}, contracts.Settings{}, nil
	}
	projectSettings, err := loadOptionalSettings(config.ProjectSettingsPath(cwd))
	if err != nil {
		return contracts.Settings{}, contracts.Settings{}, contracts.Settings{}, err
	}
	localSettings, err := loadOptionalSettings(config.LocalSettingsPath(cwd))
	if err != nil {
		return contracts.Settings{}, contracts.Settings{}, contracts.Settings{}, err
	}
	return userSettings, projectSettings, localSettings, nil
}

func mcpConfigSettingsFilePaths(cwd string) []string {
	paths := []string{config.UserSettingsPath()}
	if cwd != "" {
		paths = append(paths, config.ProjectSettingsPath(cwd), config.LocalSettingsPath(cwd))
	}
	return paths
}
