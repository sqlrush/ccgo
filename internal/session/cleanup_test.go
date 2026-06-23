package session

// CFG-13: CleanupOldTranscripts removes JSONL files older than retentionDays.

import (
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestCleanupOldTranscriptsNoop(t *testing.T) {
	// retentionDays=0 → no-op, should not error even on nonexistent dir.
	if err := CleanupOldTranscripts(0); err != nil {
		t.Fatalf("CleanupOldTranscripts(0) = %v, want nil", err)
	}
}

func TestCleanupOldTranscriptsRemovesOldFiles(t *testing.T) {
	// Build a fake projects structure under a temp dir.
	tmpDir := t.TempDir()
	projectDir := filepath.Join(tmpDir, "projects", "my-project")
	if err := os.MkdirAll(projectDir, 0o755); err != nil {
		t.Fatal(err)
	}

	// Create an old file (modified 10 days ago) and a recent file.
	oldFile := filepath.Join(projectDir, "old.jsonl")
	newFile := filepath.Join(projectDir, "new.jsonl")
	if err := os.WriteFile(oldFile, []byte("{}"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(newFile, []byte("{}"), 0o644); err != nil {
		t.Fatal(err)
	}
	// Back-date the old file by 10 days.
	oldTime := time.Now().Add(-10 * 24 * time.Hour)
	if err := os.Chtimes(oldFile, oldTime, oldTime); err != nil {
		t.Fatal(err)
	}

	// Monkey-patch: cleanupTranscriptsInDir targets the project dir directly.
	cutoff := time.Now().Add(-7 * 24 * time.Hour) // 7-day retention
	if err := cleanupTranscriptsInDir(projectDir, cutoff); err != nil {
		t.Fatalf("cleanupTranscriptsInDir: %v", err)
	}

	if _, err := os.Stat(oldFile); !os.IsNotExist(err) {
		t.Errorf("expected old.jsonl to be removed, but it still exists")
	}
	if _, err := os.Stat(newFile); err != nil {
		t.Errorf("expected new.jsonl to still exist, got: %v", err)
	}
}

func TestCleanupOldTranscriptsKeepsNonJsonl(t *testing.T) {
	tmpDir := t.TempDir()
	oldFile := filepath.Join(tmpDir, "old.txt")
	if err := os.WriteFile(oldFile, []byte("data"), 0o644); err != nil {
		t.Fatal(err)
	}
	// Back-date the file by 10 days.
	oldTime := time.Now().Add(-10 * 24 * time.Hour)
	if err := os.Chtimes(oldFile, oldTime, oldTime); err != nil {
		t.Fatal(err)
	}

	cutoff := time.Now().Add(-7 * 24 * time.Hour)
	if err := cleanupTranscriptsInDir(tmpDir, cutoff); err != nil {
		t.Fatalf("cleanupTranscriptsInDir: %v", err)
	}
	// Non-.jsonl file must survive.
	if _, err := os.Stat(oldFile); err != nil {
		t.Errorf("expected old.txt to survive cleanup, got: %v", err)
	}
}
