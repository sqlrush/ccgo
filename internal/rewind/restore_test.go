package rewind

import (
	"os"
	"path/filepath"
	"testing"
	"time"
)

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

	changed, err := Restore(snap, store)
	if err != nil {
		t.Fatal(err)
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

	res, err := Rewind(tp, "mid-1", store)
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
