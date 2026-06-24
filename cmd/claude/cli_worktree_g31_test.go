package main

import (
	"os"
	"os/exec"
	"path/filepath"
	"testing"
)

// TestCreateWorktreeIfRequested_NoOp verifies that an empty Worktree option is a no-op.
func TestCreateWorktreeIfRequested_NoOp(t *testing.T) {
	original := "/some/path"
	wd := original
	if err := createWorktreeIfRequested(cliOptions{}, &wd); err != nil {
		t.Fatalf("expected no error for empty Worktree, got: %v", err)
	}
	if wd != original {
		t.Errorf("expected wd to remain %q, got %q", original, wd)
	}
}

// TestCreateWorktreeIfRequested_CreatesWorktree verifies that --worktree creates a git
// worktree and updates *wd to the worktree path.
// CLI-FLAG-34: CC ref: src/main.tsx --worktree flag.
func TestCreateWorktreeIfRequested_CreatesWorktree(t *testing.T) {
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git not available")
	}

	// Create a temp dir and initialise a git repo with one commit.
	repoDir := t.TempDir()
	runGit := func(args ...string) {
		t.Helper()
		cmd := exec.Command("git", args...)
		cmd.Dir = repoDir
		cmd.Env = append(os.Environ(),
			"GIT_AUTHOR_NAME=Test",
			"GIT_AUTHOR_EMAIL=test@test.com",
			"GIT_COMMITTER_NAME=Test",
			"GIT_COMMITTER_EMAIL=test@test.com",
		)
		if out, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("git %v: %v\n%s", args, err, out)
		}
	}
	runGit("init")
	runGit("commit", "--allow-empty", "-m", "init")

	wd := repoDir
	if err := createWorktreeIfRequested(cliOptions{Worktree: "my-branch"}, &wd); err != nil {
		t.Fatalf("createWorktreeIfRequested: %v", err)
	}

	// wd should have changed to the new worktree path.
	// Use filepath.EvalSymlinks to handle macOS /var → /private/var symlink.
	resolvedRepoDir, err := filepath.EvalSymlinks(repoDir)
	if err != nil {
		resolvedRepoDir = repoDir
	}
	expectedPath := filepath.Join(filepath.Dir(resolvedRepoDir), ".ccgo-worktrees", "my-branch")
	if wd != expectedPath {
		t.Errorf("expected wd=%q, got %q", expectedPath, wd)
	}

	// The worktree directory must exist.
	if _, err := os.Stat(wd); err != nil {
		t.Errorf("worktree directory %q does not exist: %v", wd, err)
	}

	// Cleanup: remove the worktree so git doesn't leave stale state.
	// Best-effort; test failures don't cascade.
	_ = exec.Command("git", "-C", repoDir, "worktree", "remove", "--force", wd).Run()
}
