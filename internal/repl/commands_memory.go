package repl

import (
	"context"
	"fmt"

	"ccgo/internal/memory"
)

// memoryFileLister is the DI interface for discovering memory files.
type memoryFileLister func() ([]string, error)

// memoryHandlerWith is the dependency-injected core (testable without disk).
// Discovery errors degrade gracefully into a Status message rather than aborting
// the REPL turn.
func memoryHandlerWith(list memoryFileLister) CommandHandler {
	return func(ctx context.Context, cc CommandContext) (CommandOutcome, error) {
		files, err := list()
		if err != nil {
			return CommandOutcome{Handled: true, Status: fmt.Sprintf("Error discovering memory files: %s", err.Error())}, nil
		}
		if len(files) == 0 {
			return CommandOutcome{Handled: true, Status: "No CLAUDE.md memory files found."}, nil
		}
		return CommandOutcome{Handled: true, Overlay: NewMemorySelector(files)}, nil
	}
}

// memoryHandler builds the production handler over real disk discovery.
func memoryHandler(cwd string) CommandHandler {
	return memoryHandlerWith(func() ([]string, error) {
		claudeFiles, err := memory.DiscoverScopedClaudeFiles(memory.ScopeOptions{CWD: cwd})
		if err != nil {
			return nil, fmt.Errorf("discover memory files: %w", err)
		}
		// Deduplicate paths while preserving discovery order.
		seen := make(map[string]struct{}, len(claudeFiles))
		deduped := make([]string, 0, len(claudeFiles))
		for _, f := range claudeFiles {
			if _, ok := seen[f.Path]; ok {
				continue
			}
			seen[f.Path] = struct{}{}
			deduped = append(deduped, f.Path)
		}
		return deduped, nil
	})
}
