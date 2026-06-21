package rewind

import (
	"os"
	"path/filepath"
	"testing"
	"time"
)

// TestRestoreRejectsOutOfRootPath proves that a snapshot whose tracked path
// escapes the project root (e.g. /tmp/outside/victim) is silently skipped —
// neither written nor deleted — and reported in the Skipped slice.
func TestRestoreRejectsOutOfRootPath(t *testing.T) {
	root := t.TempDir()
	outside := t.TempDir() // a different temp dir = outside root
	victim := filepath.Join(outside, "victim.txt")
	if err := os.WriteFile(victim, []byte("sensitive"), 0o644); err != nil {
		t.Fatal(err)
	}

	store := NewStore(filepath.Join(root, ".snap"))

	// Craft a snapshot that claims to track a file OUTSIDE root.
	// We cannot use store.Capture (it would resolve paths correctly); instead
	// build the snapshot manually to simulate a tampered transcript.
	snap := Snapshot{
		MessageID: "evil",
		TrackedFileBackups: map[string]FileBackup{
			victim: {BackupFileName: "", Version: 1}, // deletion sentinel
		},
	}

	changed, skipped, err := Restore(snap, store, root)
	if err != nil {
		t.Fatalf("Restore returned hard error: %v", err)
	}
	if len(changed) != 0 {
		t.Fatalf("expected no changes; got %v", changed)
	}
	if len(skipped) == 0 {
		t.Fatal("expected out-of-root path to appear in Skipped; got none")
	}
	// Victim must NOT have been deleted.
	if _, err := os.Stat(victim); os.IsNotExist(err) {
		t.Fatal("victim.txt was deleted despite being outside root — SECURITY BUG")
	}
}

// TestRestoreEmptyRootReturnsError proves that calling Restore with an empty
// Root is a programming error that returns an error without touching any files.
func TestRestoreEmptyRootReturnsError(t *testing.T) {
	root := t.TempDir()
	f := filepath.Join(root, "f.txt")
	if err := os.WriteFile(f, []byte("data"), 0o644); err != nil {
		t.Fatal(err)
	}
	store := NewStore(filepath.Join(root, ".snap"))
	snap := Snapshot{
		MessageID:          "x",
		TrackedFileBackups: map[string]FileBackup{f: {BackupFileName: "", Version: 1}},
	}
	_, _, err := Restore(snap, store, "")
	if err == nil {
		t.Fatal("expected error for empty Root, got nil")
	}
}

// TestRewindRejectsOutOfRootPath proves that the Rewind helper also confines
// paths (it delegates to Restore, so the skipped slice surfaces correctly).
func TestRewindRejectsOutOfRootPath(t *testing.T) {
	root := t.TempDir()
	outside := t.TempDir()
	victim := filepath.Join(outside, "victim2.txt")
	if err := os.WriteFile(victim, []byte("sensitive"), 0o644); err != nil {
		t.Fatal(err)
	}

	store := NewStore(filepath.Join(root, ".snap"))
	// Build a snapshot via Capture of an in-root file so the backup store is
	// initialised, then manually inject the out-of-root entry.
	inRoot := filepath.Join(root, "legit.txt")
	if err := os.WriteFile(inRoot, []byte("legit"), 0o644); err != nil {
		t.Fatal(err)
	}
	snap, err := store.Capture("mid-evil", []string{inRoot}, time.Now().UTC())
	if err != nil {
		t.Fatal(err)
	}
	snap.TrackedFileBackups[victim] = FileBackup{BackupFileName: "", Version: 1}

	tp := filepath.Join(root, "session.jsonl")
	if err := (Writer{TranscriptPath: tp}).Record(snap, false); err != nil {
		t.Fatal(err)
	}

	res, err := Rewind(tp, "mid-evil", store, root)
	if err != nil {
		t.Fatalf("Rewind returned hard error: %v", err)
	}
	if len(res.Skipped) == 0 {
		t.Fatal("expected out-of-root path in Skipped; got none")
	}
	if _, err := os.Stat(victim); os.IsNotExist(err) {
		t.Fatal("victim2.txt was deleted despite being outside root — SECURITY BUG")
	}
}

func TestRestoreRewritesAndDeletes(t *testing.T) {
	work := t.TempDir()
	keep := filepath.Join(work, "keep.txt")
	created := filepath.Join(work, "created.txt") // absent at snapshot time
	_ = os.WriteFile(keep, []byte("v1"), 0o644)

	store := NewStore(filepath.Join(work, ".snap"))
	snap, err := store.Capture("m1", []string{keep, created}, time.Now().UTC())
	if err != nil {
		t.Fatal(err)
	}

	// Mutate the tree after the snapshot.
	_ = os.WriteFile(keep, []byte("v2-modified"), 0o644)
	_ = os.WriteFile(created, []byte("new file"), 0o644)

	changed, skipped, err := Restore(snap, store, work)
	if err != nil {
		t.Fatal(err)
	}
	if len(skipped) != 0 {
		t.Fatalf("unexpected skipped paths: %v", skipped)
	}
	if data, _ := os.ReadFile(keep); string(data) != "v1" {
		t.Fatalf("keep.txt = %q want v1 (restored)", data)
	}
	if _, err := os.Stat(created); !os.IsNotExist(err) {
		t.Fatalf("created.txt should be deleted on restore; stat err=%v", err)
	}
	if len(changed) != 2 {
		t.Fatalf("changed = %v want 2 entries", changed)
	}
}

func TestRewindFindsSnapshotByMessageID(t *testing.T) {
	work := t.TempDir()
	f := filepath.Join(work, "x.txt")
	_ = os.WriteFile(f, []byte("orig"), 0o644)
	store := NewStore(filepath.Join(work, ".snap"))
	snap, _ := store.Capture("mid-1", []string{f}, time.Now().UTC())
	tp := filepath.Join(work, "s.jsonl")
	if err := (Writer{TranscriptPath: tp}).Record(snap, false); err != nil {
		t.Fatal(err)
	}
	_ = os.WriteFile(f, []byte("changed"), 0o644)

	res, err := Rewind(tp, "mid-1", store, work)
	if err != nil {
		t.Fatal(err)
	}
	if res.MessageID != "mid-1" {
		t.Fatalf("MessageID = %q", res.MessageID)
	}
	if data, _ := os.ReadFile(f); string(data) != "orig" {
		t.Fatalf("file not restored: %q", data)
	}
}
