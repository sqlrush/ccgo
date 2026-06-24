package main

import (
	"os"
	"path/filepath"
	"strings"

	"ccgo/internal/contracts"
)

// resolveAutoMemoryDir resolves the auto-memory directory from settings.
// When autoMemoryEnabled is explicitly false, returns "".
// When autoMemoryDirectory is set (and autoMemoryEnabled is not false), uses it.
// Otherwise returns "".
// CFG-42: wires the runner.RelevantMemoryDir from settings.
// CC ref: src/memdir/paths.ts getAutoMemPath / isAutoMemoryEnabled.
func resolveAutoMemoryDir(merged contracts.Settings, _ string) string {
	// When the setting is explicitly disabled, do not wire the directory.
	if merged.AutoMemoryEnabled != nil && !*merged.AutoMemoryEnabled {
		return ""
	}
	// Use the configured directory if set.
	dir := strings.TrimSpace(merged.AutoMemoryDirectory)
	if dir == "" {
		return ""
	}
	// Expand ~ prefix.
	if len(dir) >= 2 && dir[:2] == "~/" {
		home, err := os.UserHomeDir()
		if err == nil {
			dir = filepath.Join(home, dir[2:])
		}
	}
	return filepath.Clean(dir)
}
