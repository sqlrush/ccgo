package repl

import (
	"context"
	"fmt"
	"strings"

	"ccgo/internal/config"
)

// settingsSetter is the DI interface for writing a single settings key.
// Production: wrap config.SetSettingsValue(config.UserSettingsPath(), ...).
// Tests: a fake recorder.
type settingsSetter func(key string, value any) error

// builtinThemes mirrors the CC theme list from src/commands/theme/index.ts.
var builtinThemes = []string{"dark", "light", "dark-daltonism", "light-daltonism", "default"}

// validEffortLevels are the allowed values for effortLevel (CC utils/effort.ts:14).
var validEffortLevels = map[string]bool{
	"low":    true,
	"medium": true,
	"high":   true,
	"max":    true,
	"auto":   true,
}

// effortHandlerWith returns a CommandHandler for /effort backed by the given setter.
// Valid values: low | medium | high | max | auto (auto clears the key).
func effortHandlerWith(set settingsSetter) CommandHandler {
	return func(ctx context.Context, cc CommandContext) (CommandOutcome, error) {
		arg := strings.TrimSpace(cc.Args)
		if arg == "" {
			return CommandOutcome{
				Handled: true,
				Status:  "Usage: /effort <low|medium|high|max|auto>",
			}, nil
		}
		if !validEffortLevels[arg] {
			return CommandOutcome{
				Handled: true,
				Status:  fmt.Sprintf("Unknown effort level %q. Valid values: low, medium, high, max, auto.", arg),
			}, nil
		}
		if arg == "auto" {
			// auto clears effortLevel (mirrors CC effort.tsx:76).
			if err := set("effortLevel", nil); err != nil {
				return CommandOutcome{}, fmt.Errorf("effort auto clear: %w", err)
			}
			return CommandOutcome{Handled: true, Status: "Effort level cleared (auto)."}, nil
		}
		if err := set("effortLevel", arg); err != nil {
			return CommandOutcome{}, fmt.Errorf("set effortLevel: %w", err)
		}
		return CommandOutcome{Handled: true, Status: fmt.Sprintf("Effort level set to %q.", arg)}, nil
	}
}

// effortHandler is the production handler using the real user settings file.
func effortHandler() CommandHandler {
	return effortHandlerWith(func(key string, value any) error {
		return config.SetSettingsValue(config.UserSettingsPath(), key, value)
	})
}

// themeHandlerWith returns a CommandHandler for /theme backed by the given setter.
func themeHandlerWith(set settingsSetter) CommandHandler {
	return func(ctx context.Context, cc CommandContext) (CommandOutcome, error) {
		arg := strings.TrimSpace(cc.Args)
		if arg == "" {
			return CommandOutcome{Handled: true, Overlay: NewThemePicker(builtinThemes)}, nil
		}
		if err := set("theme", arg); err != nil {
			return CommandOutcome{}, fmt.Errorf("set theme: %w", err)
		}
		return CommandOutcome{Handled: true, Status: fmt.Sprintf("Theme set to %q.", arg)}, nil
	}
}

// themeHandler is the production handler using the real user settings file.
func themeHandler() CommandHandler {
	return themeHandlerWith(func(key string, value any) error {
		return config.SetSettingsValue(config.UserSettingsPath(), key, value)
	})
}

// vimHandlerWith returns a CommandHandler for /vim backed by the given setter.
// It toggles cc.Screen.VimEnabled (live) and persists editorMode.
func vimHandlerWith(set settingsSetter) CommandHandler {
	return func(ctx context.Context, cc CommandContext) (CommandOutcome, error) {
		newEnabled := true
		if cc.Screen != nil {
			newEnabled = !cc.Screen.VimEnabled
		}
		if cc.Screen != nil {
			cc.Screen.SetVimEnabled(newEnabled)
		}
		mode := "normal"
		if newEnabled {
			mode = "vim"
		}
		if err := set("editorMode", mode); err != nil {
			return CommandOutcome{}, fmt.Errorf("set editorMode: %w", err)
		}
		return CommandOutcome{
			Handled: true,
			Status:  fmt.Sprintf("Vim mode %s.", mode),
		}, nil
	}
}

// vimHandler is the production handler using the real user settings file.
func vimHandler() CommandHandler {
	return vimHandlerWith(func(key string, value any) error {
		return config.SetSettingsValue(config.UserSettingsPath(), key, value)
	})
}
