package lsp

import (
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"strings"
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

func TestSummarizeDiagnostics(t *testing.T) {
	summary := Summarize([]Diagnostic{
		{FilePath: "a.go", Severity: "error", Source: "gopls", Message: "err"},
		{FilePath: "a.go", Severity: "warning", Source: "gopls", Message: "warn"},
		{FilePath: "b.go", Severity: "info", Source: "tsserver", Message: "info"},
		{FilePath: "c.go", Severity: "hint", Message: "hint"},
		{FilePath: "", Severity: "error", Message: "ignored"},
	})
	if summary.Total != 4 ||
		summary.Files != 3 ||
		summary.ErrorCount != 1 ||
		summary.WarningCount != 1 ||
		summary.InfoCount != 1 ||
		summary.HintCount != 1 ||
		summary.BySeverity["error"] != 1 ||
		summary.BySource["gopls"] != 2 ||
		summary.BySource["tsserver"] != 1 {
		t.Fatalf("summary = %#v", summary)
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

func TestDiagnosticsFromPublishDiagnosticsParams(t *testing.T) {
	got, err := DiagnosticsFromPublishDiagnostics([]byte(`{
		"uri": "file:///work/main.go",
		"diagnostics": [{
			"range": {"start": {"line": 4, "character": 2}, "end": {"line": 4, "character": 7}},
			"severity": 1,
			"code": "E100",
			"source": "gopls",
			"message": "broken"
		}]
	}`))
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 1 ||
		got[0].FilePath != "/work/main.go" ||
		got[0].Severity != "error" ||
		got[0].Code != "E100" ||
		got[0].Source != "gopls" ||
		got[0].Message != "broken" {
		t.Fatalf("diagnostics = %#v", got)
	}
}

func TestDiagnosticsFromPublishDiagnosticsWrapper(t *testing.T) {
	got, err := DiagnosticsFromPublishDiagnostics([]byte(`{
		"jsonrpc": "2.0",
		"method": "textDocument/publishDiagnostics",
		"params": {
			"uri": "file:///work/with%20space.go",
			"diagnostics": [{"severity": "warning", "code": 7, "message": "warn"}]
		}
	}`))
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 1 || got[0].FilePath != "/work/with space.go" || got[0].Severity != "warning" || got[0].Code != "7" {
		t.Fatalf("diagnostics = %#v", got)
	}
}

func TestDiagnosticsUpdateFromPublishDiagnosticsPreservesEmptyUpdates(t *testing.T) {
	update, err := DiagnosticsUpdateFromPublishDiagnostics([]byte(`{
		"uri": "file:///work/main.go",
		"diagnostics": []
	}`))
	if err != nil {
		t.Fatal(err)
	}
	if update.FilePath != "/work/main.go" || len(update.Diagnostics) != 0 {
		t.Fatalf("update = %#v", update)
	}
}

func TestDiagnosticsFromPublishDiagnosticsRequiresURI(t *testing.T) {
	_, err := DiagnosticsFromPublishDiagnostics([]byte(`{"diagnostics":[]}`))
	if err == nil || !strings.Contains(err.Error(), "uri is required") {
		t.Fatalf("err = %v", err)
	}
}

func TestApplyDiagnosticsUpdateReplacesUpdatedFiles(t *testing.T) {
	existing := []Diagnostic{
		{FilePath: "a.go", Severity: "error", Message: "old a"},
		{FilePath: "b.go", Severity: "warning", Message: "old b"},
	}
	update := []Diagnostic{
		{FilePath: "./a.go", Severity: "2", Message: "new a"},
		{FilePath: "", Severity: "error", Message: "ignored"},
	}
	got := ApplyDiagnosticsUpdate(existing, update)
	if len(got) != 2 {
		t.Fatalf("updated diagnostics = %#v", got)
	}
	if got[0].FilePath != "a.go" || got[0].Severity != "warning" || got[0].Message != "new a" {
		t.Fatalf("updated diagnostics = %#v", got)
	}
	if got[1].FilePath != "b.go" || got[1].Message != "old b" {
		t.Fatalf("updated diagnostics = %#v", got)
	}
}

func TestApplyDiagnosticsForFileClearsUpdatedFile(t *testing.T) {
	existing := []Diagnostic{
		{FilePath: "a.go", Severity: "error", Message: "old a"},
		{FilePath: "b.go", Severity: "warning", Message: "old b"},
	}
	got := ApplyDiagnosticsForFile(existing, "./a.go", nil)
	if len(got) != 1 || got[0].FilePath != "b.go" || got[0].Message != "old b" {
		t.Fatalf("updated diagnostics = %#v", got)
	}
}

func TestApplyPublishDiagnosticsSnapshotWritesReplacement(t *testing.T) {
	path := filepath.Join(t.TempDir(), diagnosticsFileName)
	if err := WriteSnapshot(path, []Diagnostic{
		{FilePath: "/a.go", Severity: "error", Message: "old a"},
		{FilePath: "b.go", Severity: "warning", Message: "old b"},
	}); err != nil {
		t.Fatal(err)
	}
	updated, err := ApplyPublishDiagnosticsSnapshot(path, []byte(`{
		"uri": "file:///a.go",
		"diagnostics": [{"severity": 1, "message": "new a"}]
	}`))
	if err != nil {
		t.Fatal(err)
	}
	if len(updated) != 2 || updated[0].FilePath != "/a.go" || updated[0].Message != "new a" || updated[1].FilePath != "b.go" {
		t.Fatalf("updated diagnostics = %#v", updated)
	}
	updated, err = ApplyPublishDiagnosticsSnapshot(path, []byte(`{
		"uri": "file:///a.go",
		"diagnostics": []
	}`))
	if err != nil {
		t.Fatal(err)
	}
	if len(updated) != 1 || updated[0].FilePath != "b.go" {
		t.Fatalf("cleared diagnostics = %#v", updated)
	}
}

func TestApplyDiagnosticsUpdateIgnoresEmptyUpdate(t *testing.T) {
	existing := []Diagnostic{{FilePath: "a.go", Severity: "error", Message: "old a"}}
	got := ApplyDiagnosticsUpdate(existing, nil)
	if len(got) != 1 || got[0].Message != "old a" {
		t.Fatalf("updated diagnostics = %#v", got)
	}
}
