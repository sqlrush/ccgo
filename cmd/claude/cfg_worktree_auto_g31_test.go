package main

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"ccgo/internal/contracts"
)

// boolPtrG31 returns a pointer to the given bool value.
// (local helper; avoids collisions with any similarly named helper elsewhere)
func boolPtrG31(b bool) *bool { return &b }

// TestWorktreeAutoFieldExists is a seam test verifying that contracts.WorktreeSetting
// has an Auto field that can be set and read back.
// CFG-41: worktree.auto should auto-set isolation=worktree for the task tool.
// CC ref: utils/settings/types.ts worktree:{auto:boolean}.
func TestWorktreeAutoFieldExists(t *testing.T) {
	s := contracts.Settings{
		Worktree: &contracts.WorktreeSetting{
			Auto: boolPtrG31(true),
		},
	}
	if s.Worktree == nil {
		t.Fatal("expected Worktree to be non-nil")
	}
	if s.Worktree.Auto == nil {
		t.Fatal("expected Worktree.Auto to be non-nil")
	}
	if !*s.Worktree.Auto {
		t.Errorf("expected Worktree.Auto=true, got false")
	}
}

// TestWorktreeAutoNameFormat verifies worktreeAutoName produces a valid name.
// CFG-41: the auto-worktree name is "auto-<first8charsOfSessionID>".
func TestWorktreeAutoNameFormat(t *testing.T) {
	name := worktreeAutoName("abc12345-9999-aaaa-bbbb-ccccddddeeee")
	if name != "auto-abc12345" {
		t.Errorf("expected auto-abc12345, got %q", name)
	}

	// Short session ID: use entire ID.
	short := worktreeAutoName("xyz")
	if short != "auto-xyz" {
		t.Errorf("expected auto-xyz, got %q", short)
	}

	// Empty session ID: fallback to timestamp prefix.
	ts := worktreeAutoName("")
	if !strings.HasPrefix(ts, "auto-") {
		t.Errorf("expected auto-<timestamp> prefix, got %q", ts)
	}
}

// TestApplyWorktreeAutoSetting_NoOp verifies that when Auto is false the wd is unchanged.
// CFG-41.
func TestApplyWorktreeAutoSetting_NoOp(t *testing.T) {
	wd := "/original"
	merged := contracts.Settings{
		Worktree: &contracts.WorktreeSetting{Auto: boolPtrG31(false)},
	}
	if err := applyWorktreeAutoSetting(merged, "sess-123", &wd); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if wd != "/original" {
		t.Errorf("expected wd unchanged, got %q", wd)
	}
}

// TestApplyWorktreeAutoSetting_NilWorktree verifies nil worktree setting is a no-op.
func TestApplyWorktreeAutoSetting_NilWorktree(t *testing.T) {
	wd := "/original"
	merged := contracts.Settings{}
	if err := applyWorktreeAutoSetting(merged, "sess-123", &wd); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if wd != "/original" {
		t.Errorf("expected wd unchanged, got %q", wd)
	}
}

// TestApplyWorktreeAutoSetting_CreatesWorktree verifies that worktree.auto=true
// creates a git worktree and updates *wd.
// CFG-41: wired to headlessRunner via applyWorktreeAutoSetting.
func TestApplyWorktreeAutoSetting_CreatesWorktree(t *testing.T) {
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git not available")
	}

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

	// Resolve symlinks so macOS /var → /private/var is handled.
	resolvedRepo, err := filepath.EvalSymlinks(repoDir)
	if err != nil {
		resolvedRepo = repoDir
	}

	wd := repoDir
	merged := contracts.Settings{
		Worktree: &contracts.WorktreeSetting{Auto: boolPtrG31(true)},
	}
	sessionID := contracts.ID("abcdef12-1111-2222-3333-444455556666")
	if err := applyWorktreeAutoSetting(merged, sessionID, &wd); err != nil {
		t.Fatalf("applyWorktreeAutoSetting: %v", err)
	}

	expectedName := "auto-abcdef12"
	expectedPath := filepath.Join(filepath.Dir(resolvedRepo), ".ccgo-worktrees", expectedName)
	if wd != expectedPath {
		t.Errorf("expected wd=%q, got %q", expectedPath, wd)
	}
	if _, err := os.Stat(wd); err != nil {
		t.Errorf("worktree directory %q does not exist: %v", wd, err)
	}

	// Cleanup.
	_ = exec.Command("git", "-C", repoDir, "worktree", "remove", "--force", wd).Run()
}
