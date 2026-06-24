package sandbox

// ScrubBareGitRepoFiles tests (SBX-43).
// CC ref: src/utils/sandbox/sandbox-adapter.ts:scrubBareGitRepoFiles.

import (
	"os"
	"path/filepath"
	"testing"
)

// TestScrubBareGitRepoFilesRemovesPlainFiles verifies that ScrubBareGitRepoFiles
// removes plain files matching bare-repo names (HEAD, objects, refs, hooks, config).
func TestScrubBareGitRepoFilesRemovesPlainFiles(t *testing.T) {
	dir := t.TempDir()

	// Plant bare-repo stubs as plain files.
	for _, name := range bareGitRepoFiles {
		if err := os.WriteFile(filepath.Join(dir, name), []byte("stub"), 0o644); err != nil {
			t.Fatalf("WriteFile %s: %v", name, err)
		}
	}

	ScrubBareGitRepoFiles(dir)

	for _, name := range bareGitRepoFiles {
		p := filepath.Join(dir, name)
		if _, err := os.Lstat(p); err == nil {
			t.Errorf("ScrubBareGitRepoFiles: %s should be removed but still exists", name)
		}
	}
}

// TestScrubBareGitRepoFilesRemovesEmptyDirs verifies that empty directories
// with bare-repo names are removed.
func TestScrubBareGitRepoFilesRemovesEmptyDirs(t *testing.T) {
	dir := t.TempDir()

	// Plant "objects" and "refs" as empty dirs (common sandbox stub pattern).
	for _, name := range []string{"objects", "refs"} {
		if err := os.Mkdir(filepath.Join(dir, name), 0o755); err != nil {
			t.Fatalf("Mkdir %s: %v", name, err)
		}
	}

	ScrubBareGitRepoFiles(dir)

	for _, name := range []string{"objects", "refs"} {
		p := filepath.Join(dir, name)
		if _, err := os.Lstat(p); err == nil {
			t.Errorf("ScrubBareGitRepoFiles: empty dir %s should be removed but still exists", name)
		}
	}
}

// TestScrubBareGitRepoFilesKeepsNonEmptyDirs verifies that non-empty directories
// are NOT removed (safety guard against accidental data loss).
func TestScrubBareGitRepoFilesKeepsNonEmptyDirs(t *testing.T) {
	dir := t.TempDir()

	// Create a non-empty "objects" directory.
	objDir := filepath.Join(dir, "objects")
	if err := os.MkdirAll(filepath.Join(objDir, "pack"), 0o755); err != nil {
		t.Fatalf("MkdirAll: %v", err)
	}

	ScrubBareGitRepoFiles(dir)

	if _, err := os.Lstat(objDir); err != nil {
		t.Error("ScrubBareGitRepoFiles: non-empty objects/ should be kept but was removed")
	}
}

// TestScrubBareGitRepoFilesNoOpWhenAbsent verifies that ScrubBareGitRepoFiles
// is a no-op when no bare-repo stubs exist (the expected common case).
func TestScrubBareGitRepoFilesNoOpWhenAbsent(t *testing.T) {
	dir := t.TempDir()
	// Should not panic or error even when nothing to remove.
	ScrubBareGitRepoFiles(dir)
	// dir should still be empty.
	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatalf("ReadDir: %v", err)
	}
	if len(entries) != 0 {
		t.Errorf("ScrubBareGitRepoFiles: dir should remain empty, got %v", entries)
	}
}
