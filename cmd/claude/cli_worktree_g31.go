package main

import (
	"fmt"
	"os/exec"
	"path/filepath"
	"strings"
)

// createWorktreeIfRequested creates a git worktree when options.Worktree is non-empty.
// On success it changes *wd to the worktree path.
// Returns without error when options.Worktree is empty (no-op).
// CLI-FLAG-34: --worktree creates git worktree.
// CC ref: src/main.tsx --worktree flag; EnterWorktree tool in internal/tools/worktree.
func createWorktreeIfRequested(options cliOptions, wd *string) error {
	if options.Worktree == "" {
		return nil
	}

	// Resolve git root from working directory.
	// Use -C *wd so we resolve relative to the runner's working directory,
	// not the process cwd (which may differ when running tests).
	rootOut, err := exec.Command("git", "-C", *wd, "rev-parse", "--show-toplevel").Output()
	if err != nil {
		return fmt.Errorf("--worktree: not in a git repository: %w", err)
	}
	root := strings.TrimSpace(string(rootOut))

	// Generate worktree path: <parent-of-root>/.ccgo-worktrees/<name>.
	worktreePath := filepath.Join(filepath.Dir(root), ".ccgo-worktrees", options.Worktree)

	// Create the worktree.
	addCmd := exec.Command("git", "-C", root, "worktree", "add", "--detach", worktreePath, "HEAD")
	if out, err := addCmd.CombinedOutput(); err != nil {
		return fmt.Errorf("--worktree: git worktree add failed: %w\n%s", err, strings.TrimSpace(string(out)))
	}

	*wd = worktreePath
	return nil
}
