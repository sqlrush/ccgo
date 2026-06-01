package platform

import (
	"os"
	"path/filepath"
	"runtime"
	"strings"
)

func ClaudeHomeDir() string {
	if v := os.Getenv("CLAUDE_CONFIG_DIR"); v != "" {
		return v
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return ".claude"
	}
	return filepath.Join(home, ".claude")
}

func ExpandPath(path string) string {
	if path == "" {
		return path
	}
	if path == "~" || strings.HasPrefix(path, "~/") {
		home, err := os.UserHomeDir()
		if err == nil {
			if path == "~" {
				return home
			}
			return filepath.Join(home, path[2:])
		}
	}
	if filepath.IsAbs(path) {
		return filepath.Clean(path)
	}
	cwd, err := os.Getwd()
	if err != nil {
		return filepath.Clean(path)
	}
	return filepath.Clean(filepath.Join(cwd, path))
}

func SanitizeProjectPath(path string) string {
	clean := filepath.Clean(path)
	if runtime.GOOS == "windows" {
		clean = strings.ReplaceAll(clean, "\\", "-")
	}
	clean = strings.ReplaceAll(clean, "/", "-")
	clean = strings.ReplaceAll(clean, ":", "-")
	clean = strings.Trim(clean, "-")
	if clean == "" {
		return "root"
	}
	return clean
}
