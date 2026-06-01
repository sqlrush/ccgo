package config

import (
	"path/filepath"

	"ccgo/internal/platform"
)

func UserSettingsPath() string {
	return filepath.Join(platform.ClaudeHomeDir(), "settings.json")
}

func ProjectSettingsPath(root string) string {
	return filepath.Join(root, ".claude", "settings.json")
}

func LocalSettingsPath(root string) string {
	return filepath.Join(root, ".claude", "settings.local.json")
}
