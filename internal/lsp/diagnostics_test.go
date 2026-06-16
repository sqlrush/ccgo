package lsp

import (
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"testing"
)

func TestSessionDiagnosticsPath(t *testing.T) {
	got := SessionDiagnosticsPath(filepath.Join("tmp", "sessions", "session.jsonl"), "sess_1")
	want := filepath.Join("tmp", "sessions", "sess_1", diagnosticsFileName)
	if got != want {
		t.Fatalf("SessionDiagnosticsPath() = %q, want %q", got, want)
	}
	if got := SessionDiagnosticsPath("", "sess_1"); got != "" {
		t.Fatalf("empty transcript path = %q, want empty", got)
	}
	if got := SessionDiagnosticsPath("session.jsonl", ""); got != "" {
		t.Fatalf("empty session id = %q, want empty", got)
	}
}

func TestWriteLoadAndFilterDiagnostics(t *testing.T) {
	path := filepath.Join(t.TempDir(), "diag", diagnosticsFileName)
	input := []Diagnostic{
		{FilePath: "./b.go", Severity: "2", Message: "warn", Range: Range{Start: Position{Line: 10}}},
		{FilePath: "a.go", Severity: "error", Message: "err", Range: Range{Start: Position{Line: 2}}},
		{FilePath: "", Severity: "error", Message: "missing path"},
		{FilePath: "c.go", Severity: "hint", Message: ""},
	}
	if err := WriteSnapshot(path, input); err != nil {
		t.Fatal(err)
	}
	loaded, err := LoadSnapshot(path)
	if err != nil {
		t.Fatal(err)
	}
	if len(loaded) != 2 || loaded[0].FilePath != "a.go" || loaded[1].Severity != "warning" {
		t.Fatalf("loaded diagnostics = %#v", loaded)
	}
	filtered, truncated := FilterDiagnostics(loaded, Filter{Severity: "warning", Limit: 1})
	if truncated {
		t.Fatalf("single warning should not be truncated")
	}
	if len(filtered) != 1 || filtered[0].FilePath != "b.go" {
		t.Fatalf("filtered diagnostics = %#v", filtered)
	}
	filtered, truncated = FilterDiagnostics(loaded, Filter{Limit: 1})
	if !truncated || len(filtered) != 1 || filtered[0].FilePath != "a.go" {
		t.Fatalf("limited diagnostics = %#v truncated=%v", filtered, truncated)
	}
}

func TestLoadSnapshotMissingFile(t *testing.T) {
	got, err := LoadSnapshot(filepath.Join(t.TempDir(), diagnosticsFileName))
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(got, []Diagnostic(nil)) {
		t.Fatalf("missing snapshot = %#v", got)
	}
}

func TestWriteSnapshotRejectsEmptyPath(t *testing.T) {
	if err := WriteSnapshot("", nil); !errors.Is(err, os.ErrInvalid) {
		t.Fatalf("WriteSnapshot empty path err = %v", err)
	}
}
