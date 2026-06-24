package main

import (
	"testing"

	"ccgo/internal/contracts"
)

// boolPtrG31 returns a pointer to the given bool value.
// (local helper; avoids collisions with any similarly named helper elsewhere)
func boolPtrG31(b bool) *bool { return &b }

// TestWorktreeAutoFieldExists is a seam test verifying that contracts.WorktreeSetting
// has an Auto field that can be set and read back.
// CFG-41: worktree.auto should auto-set isolation=worktree for the task tool.
// ⚠️  Full wiring is deferred — the task tool isolation=worktree requires session
// context not available at settings-load time.
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
