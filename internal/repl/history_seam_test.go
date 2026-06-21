package repl

import (
	"os"
	"path/filepath"
	"testing"

	"ccgo/internal/session"
)

func TestHistoryRecorderAppends(t *testing.T) {
	dir := t.TempDir()
	rec := HistoryRecorder{
		Path:      filepath.Join(dir, "history.jsonl"),
		Project:   "/home/u/proj",
		SessionID: "s1",
	}
	if err := rec.Record("first prompt"); err != nil {
		t.Fatal(err)
	}
	if err := rec.Record("second prompt"); err != nil {
		t.Fatal(err)
	}
	entries, err := session.LoadHistory(rec.Path, rec.Project, rec.SessionID, 10, nil)
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) < 2 {
		t.Fatalf("expected >=2 history entries, got %d", len(entries))
	}
}

func TestHistoryRecorderSkip(t *testing.T) {
	dir := t.TempDir()
	rec := HistoryRecorder{Path: filepath.Join(dir, "history.jsonl"), Skip: true}
	if err := rec.Record("ignored"); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(rec.Path); err == nil {
		t.Fatal("skip mode must not create the history file")
	}
}

func TestHistoryRecorderEmptyPrompt(t *testing.T) {
	dir := t.TempDir()
	rec := HistoryRecorder{
		Path:      filepath.Join(dir, "history.jsonl"),
		Project:   "/proj",
		SessionID: "s2",
	}
	if err := rec.Record(""); err != nil {
		t.Fatal(err)
	}
	// empty prompt must not create the file
	if _, err := os.Stat(rec.Path); err == nil {
		t.Fatal("empty prompt must not create the history file")
	}
}
