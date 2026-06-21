package repl

import (
	"os"
	"path/filepath"
	"testing"
	"time"

	"ccgo/internal/rewind"
)

func TestRewindToMessageDelegates(t *testing.T) {
	work := t.TempDir()
	f := filepath.Join(work, "x.txt")
	if err := os.WriteFile(f, []byte("orig"), 0o644); err != nil {
		t.Fatal(err)
	}

	sessionDir := filepath.Join(work, ".session")
	store := rewind.NewStore(sessionDir)
	snap, err := store.Capture("msg-1", []string{f}, time.Now().UTC())
	if err != nil {
		t.Fatal(err)
	}

	transcriptPath := filepath.Join(work, "transcript.jsonl")
	w := rewind.Writer{TranscriptPath: transcriptPath}
	if err := w.Record(snap, false); err != nil {
		t.Fatal(err)
	}

	// Mutate after snapshot
	if err := os.WriteFile(f, []byte("modified"), 0o644); err != nil {
		t.Fatal(err)
	}

	res, err := RewindToMessage(transcriptPath, "msg-1", sessionDir, work)
	if err != nil {
		t.Fatalf("RewindToMessage: %v", err)
	}
	if res.MessageID != "msg-1" {
		t.Fatalf("MessageID = %q, want msg-1", res.MessageID)
	}
	data, err := os.ReadFile(f)
	if err != nil {
		t.Fatal(err)
	}
	if string(data) != "orig" {
		t.Fatalf("file not restored: got %q, want orig", string(data))
	}
}
