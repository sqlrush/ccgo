package sandbox

// LocalExcludedCommandsStore reads/writes sandbox.excludedCommands in a
// .claude/settings.local.json file.  It is the production implementation of
// ExcludedCommandsStore used by AddExcludedCommand (SBX-59).
// CC ref: src/utils/sandbox/sandbox-adapter.ts:addToExcludedCommands.

import (
	"fmt"

	"ccgo/internal/config"
)

// LocalExcludedCommandsStore is an ExcludedCommandsStore backed by a
// .claude/settings.local.json file at SettingsPath.
type LocalExcludedCommandsStore struct {
	// SettingsPath is the absolute path to the local settings file.
	SettingsPath string
}

// ReadExcludedCommands reads sandbox.excludedCommands from SettingsPath.
// Returns an empty slice when the file or key is absent (not an error).
func (s LocalExcludedCommandsStore) ReadExcludedCommands() ([]string, error) {
	doc, err := config.ReadSettingsDocument(s.SettingsPath)
	if err != nil {
		return nil, fmt.Errorf("read settings: %w", err)
	}
	if doc == nil {
		return nil, nil
	}
	box, ok := doc["sandbox"].(map[string]any)
	if !ok {
		return nil, nil
	}
	raw, ok := box["excludedCommands"]
	if !ok {
		return nil, nil
	}
	switch v := raw.(type) {
	case []string:
		return append([]string(nil), v...), nil
	case []any:
		out := make([]string, 0, len(v))
		for _, item := range v {
			if s, ok := item.(string); ok {
				out = append(out, s)
			}
		}
		return out, nil
	}
	return nil, nil
}

// WriteExcludedCommands replaces sandbox.excludedCommands in SettingsPath with
// commands.  The rest of the document is preserved.
func (s LocalExcludedCommandsStore) WriteExcludedCommands(commands []string) error {
	doc, err := config.ReadSettingsDocument(s.SettingsPath)
	if err != nil {
		return fmt.Errorf("read settings: %w", err)
	}
	if doc == nil {
		doc = map[string]any{}
	}
	// Merge into existing sandbox section to preserve other keys.
	box, _ := doc["sandbox"].(map[string]any)
	if box == nil {
		box = map[string]any{}
	}
	// Return a new map — don't mutate the existing one.
	newBox := make(map[string]any, len(box)+1)
	for k, v := range box {
		newBox[k] = v
	}
	newBox["excludedCommands"] = commands
	newDoc := make(map[string]any, len(doc)+1)
	for k, v := range doc {
		newDoc[k] = v
	}
	newDoc["sandbox"] = newBox
	if err := config.WriteSettingsDocument(s.SettingsPath, newDoc); err != nil {
		return fmt.Errorf("write settings: %w", err)
	}
	return nil
}
