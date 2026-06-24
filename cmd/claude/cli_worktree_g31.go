package main

import (
	"fmt"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	"ccgo/internal/contracts"
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

// applyWorktreeAutoSetting checks the worktree.auto setting and, when enabled,
// automatically creates a git worktree for the session (CFG-41).
// The worktree name is derived from the session ID (first 8 chars) or a
// timestamp fallback so it is unique per session.
// CC ref: utils/settings/types.ts worktree:{auto:boolean}.
func applyWorktreeAutoSetting(merged contracts.Settings, sessionID contracts.ID, wd *string) error {
	if merged.Worktree == nil || merged.Worktree.Auto == nil || !*merged.Worktree.Auto {
		return nil
	}
	// Derive a short deterministic name from the session ID.
	name := worktreeAutoName(string(sessionID))
	return createWorktreeIfRequested(cliOptions{Worktree: name}, wd)
}

// worktreeAutoName returns a short safe worktree branch name from a session ID
// or a timestamp fallback when the session ID is empty.
func worktreeAutoName(sessionID string) string {
	if len(sessionID) >= 8 {
		return "auto-" + sessionID[:8]
	}
	if sessionID != "" {
		return "auto-" + sessionID
	}
	return "auto-" + time.Now().UTC().Format("20060102-150405")
}
