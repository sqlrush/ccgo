package rewind

import (
	"os"
	"path/filepath"
	"testing"
	"time"

	"ccgo/internal/contracts"
	"ccgo/internal/session"
)

func TestWriterAppendsParsableSnapshot(t *testing.T) {
	work := t.TempDir()
	src := filepath.Join(work, "f.txt")
	_ = os.WriteFile(src, []byte("hi"), 0o644)
	store := NewStore(filepath.Join(work, ".snap"))
	snap, err := store.Capture("mX", []string{src}, time.Now().UTC())
	if err != nil {
		t.Fatal(err)
	}
	tp := filepath.Join(work, "s.jsonl")
	w := Writer{TranscriptPath: tp}
	if err := w.Record(snap, false); err != nil {
		t.Fatal(err)
	}
	tr, err := session.LoadTranscript(tp)
	if err != nil {
		t.Fatal(err)
	}
	if _, ok := tr.FileHistoryByMessageID["mX"]; !ok {
		t.Fatal("written snapshot not found by parser")
	}
}

func TestWriterAppendsMultipleSnapshots(t *testing.T) {
	work := t.TempDir()
	tp := filepath.Join(work, "s.jsonl")
	w := Writer{TranscriptPath: tp}

	for i, id := range []contracts.ID{"msg-1", "msg-2", "msg-3"} {
		src := filepath.Join(work, "file.txt")
		_ = os.WriteFile(src, []byte("version "+string(rune('A'+i))), 0o644)
		store := NewStore(filepath.Join(work, ".snap"))
		snap, err := store.Capture(id, []string{src}, time.Now().UTC())
		if err != nil {
			t.Fatal(err)
		}
		if err := w.Record(snap, false); err != nil {
			t.Fatalf("Record %q: %v", id, err)
		}
	}

	tr, err := session.LoadTranscript(tp)
	if err != nil {
		t.Fatal(err)
	}
	if len(tr.FileHistorySnapshots) != 3 {
		t.Fatalf("expected 3 snapshots, got %d", len(tr.FileHistorySnapshots))
	}
	for _, id := range []contracts.ID{"msg-1", "msg-2", "msg-3"} {
		if _, ok := tr.FileHistoryByMessageID[id]; !ok {
			t.Errorf("snapshot not found by messageId %q", id)
		}
	}
}
