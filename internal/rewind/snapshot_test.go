package rewind

import (
	"os"
	"path/filepath"
	"testing"
	"time"

	"ccgo/internal/session"
)

// TestCaptureAndSnapshotLineRoundTrips verifies that Writer.Record produces a
// lossless round-trip: the transcript line must be parsed as a
// file-history-snapshot and indexed by the snapshot's messageId.
// (SnapshotTranscriptMessage was deleted in favour of Writer.Record which
// carries the full payload and is the only production-used write path.)
func TestCaptureAndSnapshotLineRoundTrips(t *testing.T) {
	work := t.TempDir()
	src := filepath.Join(work, "a.go")
	if err := os.WriteFile(src, []byte("package a\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	store := NewStore(filepath.Join(work, ".snap"))
	snap, err := store.Capture("m1", []string{src}, time.Unix(0, 0).UTC())
	if err != nil {
		t.Fatal(err)
	}
	if b, ok := snap.TrackedFileBackups[src]; !ok || b.BackupFileName == "" || b.Version != 1 {
		t.Fatalf("bad backup entry: %+v", snap.TrackedFileBackups)
	}
	// Backup file actually written with original bytes.
	bk := filepath.Join(store.Dir, snap.TrackedFileBackups[src].BackupFileName)
	if data, err := os.ReadFile(bk); err != nil || string(data) != "package a\n" {
		t.Fatalf("backup content = %q,%v", data, err)
	}

	// Write the snapshot using Writer.Record (the canonical write path that
	// carries the full payload) and confirm the session parser indexes it.
	tp := filepath.Join(work, "session.jsonl")
	if err := (Writer{TranscriptPath: tp}).Record(snap, false); err != nil {
		t.Fatal(err)
	}
	tr, err := session.LoadTranscript(tp)
	if err != nil {
		t.Fatal(err)
	}
	if len(tr.FileHistorySnapshots) != 1 {
		t.Fatalf("parser saw %d snapshots want 1", len(tr.FileHistorySnapshots))
	}
	if _, ok := tr.FileHistoryByMessageID["m1"]; !ok {
		t.Fatalf("snapshot not keyed by messageId m1: %v", tr.FileHistoryByMessageID)
	}
}

func TestDedupSameContent(t *testing.T) {
	work := t.TempDir()
	content := []byte("same content\n")
	src1 := filepath.Join(work, "a.txt")
	src2 := filepath.Join(work, "b.txt")
	if err := os.WriteFile(src1, content, 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(src2, content, 0o644); err != nil {
		t.Fatal(err)
	}

	store := NewStore(filepath.Join(work, ".snap"))
	snap, err := store.Capture("m2", []string{src1, src2}, time.Now().UTC())
	if err != nil {
		t.Fatal(err)
	}

	b1 := snap.TrackedFileBackups[src1].BackupFileName
	b2 := snap.TrackedFileBackups[src2].BackupFileName
	if b1 == "" || b2 == "" {
		t.Fatalf("expected non-empty backup filenames, got %q and %q", b1, b2)
	}
	if b1 != b2 {
		t.Fatalf("same content should dedup to same backup file: %q != %q", b1, b2)
	}

	// Verify only one physical file exists in the backup dir.
	entries, err := os.ReadDir(store.Dir)
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 1 {
		t.Fatalf("expected 1 backup file for deduped content, got %d", len(entries))
	}
}

func TestDifferentContentDifferentFile(t *testing.T) {
	work := t.TempDir()
	src1 := filepath.Join(work, "a.txt")
	src2 := filepath.Join(work, "b.txt")
	if err := os.WriteFile(src1, []byte("content A\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(src2, []byte("content B\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	store := NewStore(filepath.Join(work, ".snap"))
	snap, err := store.Capture("m3", []string{src1, src2}, time.Now().UTC())
	if err != nil {
		t.Fatal(err)
	}

	b1 := snap.TrackedFileBackups[src1].BackupFileName
	b2 := snap.TrackedFileBackups[src2].BackupFileName
	if b1 == b2 {
		t.Fatalf("different content should produce different backup filenames: both %q", b1)
	}
}
