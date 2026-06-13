package conversation

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"

	"ccgo/internal/config"
	"ccgo/internal/contracts"
	"ccgo/internal/mcp"
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
	localSettings, err := loadOptionalSettings(config.LocalSettingsPath(resolvedCWD))
	if err != nil {
		return nil, err
	}
	return &MCPConfig{
		UserSettings:    userSettings,
		ProjectSettings: projectSettings,
		LocalSettings:   localSettings,
		CWD:             resolvedCWD,
		ToolOptions: mcp.ServerToolOptions{
			AccessTokenProvider: mcp.FileOAuthAccessTokenProvider(mcp.FileOAuthAccessTokenProviderOptions{}),
		},
	}, nil
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
