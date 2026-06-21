package memory

import (
	"ccgo/internal/config"
	"ccgo/internal/platform"
)

func defaultManagedDir() string { return config.ManagedSettingsDir() }
func defaultUserDir() string    { return platform.ClaudeHomeDir() }
