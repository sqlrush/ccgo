package main

import (
	"errors"
	"fmt"
	"os/exec"
	"path/filepath"
	"strings"
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

// tmuxNotFoundError is returned (as error value) by tmuxRunner stubs when the
// tmux binary is not available. Production code returns a descriptive fmt.Errorf
// string; this type lets tests assert the graceful-degrade path.
type tmuxNotFoundError struct{}

func (e *tmuxNotFoundError) Error() string { return "--tmux: tmux binary not found in PATH" }

// tmuxRunner is a function that executes the tmux command. The default production
// runner uses exec.Command; tests inject a stub via createTmuxSessionIfRequested.
// Signature: func(command string, args ...string) error.
type tmuxRunner func(command string, args ...string) error

// defaultTmuxRunner is the production tmux runner: looks up the tmux binary in
// PATH and runs it with the given args. CLI-FLAG-35.
func defaultTmuxRunner(command string, args ...string) error {
	bin, err := exec.LookPath(command)
	if err != nil {
		return &tmuxNotFoundError{}
	}
	cmd := exec.Command(bin, args...)
	if out, err := cmd.CombinedOutput(); err != nil {
		return fmt.Errorf("--tmux: %s %s failed: %w\n%s", command, strings.Join(args, " "), err, string(out))
	}
	return nil
}

// createTmuxSessionIfRequested creates a tmux session for the worktree when
// options.Tmux is true. It is a no-op when Tmux=false.
//
// CLI-FLAG-35 production call site: called from main.go after createWorktreeIfRequested
// so that the new session starts in the newly-created worktree directory.
//
// sessionName is the short name for the tmux session (derived from the worktree
// name). worktreePath is the working directory for the new tmux session.
//
// runner is injected so tests can verify the command argv without a real tmux
// binary. Pass defaultTmuxRunner in production.
func createTmuxSessionIfRequested(opts cliOptions, sessionName, worktreePath string, runner tmuxRunner) error {
	if !opts.Tmux {
		return nil
	}
	if sessionName == "" {
		// Derive session name from worktree path basename when not provided.
		sessionName = filepath.Base(worktreePath)
	}
	sess := tmuxSession{Name: sessionName, WorktreePath: worktreePath}
	if err := validateTmuxSession(sess); err != nil {
		return err
	}
	args := buildTmuxArgs(sess)
	return runner("tmux", args...)
}
