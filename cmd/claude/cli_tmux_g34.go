package main

import (
	"errors"
	"fmt"
	"os/exec"
)

// tmuxSession describes the arguments for `tmux new-session`.
// CLI-FLAG-35: --tmux creates a tmux session for the worktree.
// CC ref: src/utils/worktree.ts createTmuxSessionForWorktree.
type tmuxSession struct {
	// Name is the tmux session name (derived from the worktree name).
	Name string
	// WorktreePath is the working directory for the new session.
	WorktreePath string
}

// buildTmuxArgs returns the argv for `tmux new-session -d -s <name> -c <path>`.
// The binary name ("tmux") is NOT included; callers prepend it as needed.
// This is the canonical ccgo argv for CLI-FLAG-35.
// CC ref: src/utils/worktree.ts:createTmuxSessionForWorktree args.
func buildTmuxArgs(s tmuxSession) []string {
	return []string{
		"new-session",
		"-d",          // detach: start session in the background
		"-s", s.Name,  // session name
		"-c", s.WorktreePath, // start directory
	}
}

// createTmuxSession invokes `tmux new-session` with the given session parameters.
// When tmux is not available it returns an error describing the missing binary.
// CLI-FLAG-35.
func createTmuxSession(s tmuxSession) error {
	if err := validateTmuxSession(s); err != nil {
		return err
	}
	tmuxBin, err := exec.LookPath("tmux")
	if err != nil {
		return fmt.Errorf("--tmux: tmux binary not found in PATH: %w", err)
	}
	args := buildTmuxArgs(s)
	cmd := exec.Command(tmuxBin, args...)
	if out, err := cmd.CombinedOutput(); err != nil {
		return fmt.Errorf("--tmux: tmux new-session failed: %w\n%s", err, string(out))
	}
	return nil
}

// validateTmuxSession returns an error if the session parameters are invalid.
func validateTmuxSession(s tmuxSession) error {
	if s.Name == "" {
		return errors.New("--tmux: session name must not be empty")
	}
	if s.WorktreePath == "" {
		return errors.New("--tmux: worktree path must not be empty")
	}
	return nil
}
