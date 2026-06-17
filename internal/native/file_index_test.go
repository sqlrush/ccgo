package native

import (
	"os"
	"path/filepath"
	"testing"
)

func TestSessionFileIndexPath(t *testing.T) {
	got := SessionFileIndexPath(filepath.Join("tmp", "sessions", "session.jsonl"), "sess_1")
	want := filepath.Join("tmp", "sessions", "sess_1", fileIndexName)
	if got != want {
		t.Fatalf("SessionFileIndexPath() = %q, want %q", got, want)
	}
	if got := SessionFileIndexPath("", "sess_1"); got != "" {
		t.Fatalf("empty transcript path = %q, want empty", got)
	}
}

func TestBuildFileIndexSkipsRuntimeDirsAndTruncates(t *testing.T) {
	dir := t.TempDir()
	mustWriteFile(t, filepath.Join(dir, "a.go"), "package main\n")
	mustWriteFile(t, filepath.Join(dir, "pkg", "b.go"), "package pkg\n")
	mustWriteFile(t, filepath.Join(dir, ".git", "config"), "secret\n")
	mustWriteFile(t, filepath.Join(dir, "node_modules", "dep.js"), "dep\n")
	index, err := BuildFileIndex("sess_native", dir, FileIndexOptions{MaxFiles: 1})
	if err != nil {
		t.Fatal(err)
	}
	if index.SessionID != "sess_native" || index.WorkingDirectory != dir || index.GeneratedAt == "" || !index.Truncated {
		t.Fatalf("index metadata = %#v", index)
	}
	if len(index.Files) != 1 || index.Files[0].Path != "a.go" || index.Files[0].Size == 0 || index.Files[0].Modified == "" {
		t.Fatalf("index files = %#v", index.Files)
	}
	for _, entry := range index.Files {
		if entry.Path == ".git/config" || entry.Path == "node_modules/dep.js" {
			t.Fatalf("indexed skipped path: %#v", index.Files)
		}
	}
}

func TestWriteAndLoadFileIndex(t *testing.T) {
	path := filepath.Join(t.TempDir(), "sess_native", fileIndexName)
	input := FileIndex{
		SessionID:        "sess_native",
		WorkingDirectory: "/work",
		Files:            []FileEntry{{Path: "main.go", Size: 12}},
	}
	if err := WriteFileIndex(path, input); err != nil {
		t.Fatal(err)
	}
	loaded, err := LoadFileIndex(path)
	if err != nil {
		t.Fatal(err)
	}
	if loaded.SessionID != "sess_native" || loaded.GeneratedAt == "" || len(loaded.Files) != 1 || loaded.Files[0].Path != "main.go" {
		t.Fatalf("loaded = %#v", loaded)
	}
}

func mustWriteFile(t *testing.T, path string, data string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(data), 0o644); err != nil {
		t.Fatal(err)
	}
}
