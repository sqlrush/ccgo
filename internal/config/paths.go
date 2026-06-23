package config

import (
	"os"
	"path/filepath"
	"runtime"

	"ccgo/internal/platform"
)

func UserSettingsPath() string {
	return filepath.Join(platform.ClaudeHomeDir(), "settings.json")
}

func ManagedSettingsPath() string {
	return filepath.Join(ManagedSettingsDir(), "managed-settings.json")
}

func ManagedSettingsDropInDir() string {
	return filepath.Join(ManagedSettingsDir(), "managed-settings.d")
}

func ManagedSettingsDir() string {
	if os.Getenv("USER_TYPE") == "ant" {
		if path := os.Getenv("CLAUDE_CODE_MANAGED_SETTINGS_PATH"); path != "" {
			return path
		}
	}
	switch runtime.GOOS {
	case "darwin":
		return filepath.Join(string(filepath.Separator), "Library", "Application Support", "ClaudeCode")
	case "windows":
		return `C:\Program Files\ClaudeCode`
	default:
		return filepath.Join(string(filepath.Separator), "etc", "claude-code")
	}
}

// EnterpriseMCPPath returns the path to the enterprise-managed MCP
// configuration file (managed-mcp.json).  When it exists, ccgo treats it as
// having exclusive control over all MCP servers (MCP-27).
// CC ref: src/services/mcp/config.ts:63 (getEnterpriseMcpFilePath).
func EnterpriseMCPPath() string {
	return filepath.Join(ManagedSettingsDir(), "managed-mcp.json")
}

func ProjectSettingsPath(root string) string {
	return filepath.Join(root, ".claude", "settings.json")
}

func LocalSettingsPath(root string) string {
	return filepath.Join(root, ".claude", "settings.local.json")
}
