package sandbox

// AddExcludedCommandToSettings appends command to the sandbox.excludedCommands
// list in the .claude/settings.local.json file at settingsPath.
// If command is already present the file is not modified (idempotent).
// This mirrors CC's addToExcludedCommands (sandbox-adapter.ts:828) which writes
// the pattern to localSettings.sandbox.excludedCommands (SBX-59).
//
// CC ref: src/utils/sandbox/sandbox-adapter.ts:addToExcludedCommands.

import (
	"fmt"
)

// ExcludedCommandsReader reads the current sandbox.excludedCommands list from
// any source.  The interface is small so tests can inject a fake.
type ExcludedCommandsReader interface {
	// ReadExcludedCommands returns the current list; nil slice means "empty".
	ReadExcludedCommands() ([]string, error)
}

// ExcludedCommandsWriter appends a single command to the persistent list.
type ExcludedCommandsWriter interface {
	// WriteExcludedCommands replaces the persisted list with commands.
	WriteExcludedCommands(commands []string) error
}

// ExcludedCommandsStore reads and writes the excluded commands list.
// A concrete implementation backed by .claude/settings.local.json is provided
// by NewLocalExcludedCommandsStore.
type ExcludedCommandsStore interface {
	ExcludedCommandsReader
	ExcludedCommandsWriter
}

// AddExcludedCommand appends command to the store's excluded list, if not
// already present.  Returns the (possibly unchanged) list that is now stored.
// CC ref: sandbox-adapter.ts:addToExcludedCommands (SBX-59).
func AddExcludedCommand(store ExcludedCommandsStore, command string) ([]string, error) {
	if command == "" {
		return nil, fmt.Errorf("addExcludedCommand: command must be non-empty")
	}
	existing, err := store.ReadExcludedCommands()
	if err != nil {
		return nil, fmt.Errorf("addExcludedCommand: read: %w", err)
	}
	for _, e := range existing {
		if e == command {
			// Already present — idempotent.
			return existing, nil
		}
	}
	updated := append(append([]string(nil), existing...), command)
	if err := store.WriteExcludedCommands(updated); err != nil {
		return nil, fmt.Errorf("addExcludedCommand: write: %w", err)
	}
	return updated, nil
}
