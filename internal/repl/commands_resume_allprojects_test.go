package repl

// SESS-09/10: cross-project resume picker test.
//
// Verifies that ResumeEntriesFromAllProjects populates picker entries from
// all project directories under the Claude home.

import (
	"os"
	"path/filepath"
	"testing"
)

// TestResumeEntriesFromAllProjectsEmpty returns zero entries when no projects exist.
func TestResumeEntriesFromAllProjectsEmpty(t *testing.T) {
	tmpHome := t.TempDir()
	t.Setenv("CLAUDE_CONFIG_DIR", tmpHome)

	entries, err := ResumeEntriesFromAllProjects()
	if err != nil {
		t.Fatalf("err = %v", err)
	}
	if len(entries) != 0 {
		t.Fatalf("expected 0 entries, got %d", len(entries))
	}
}

// TestResumeEntriesFromAllProjectsPopulated verifies that sessions from
// multiple project directories are collected into ResumeEntry values.
func TestResumeEntriesFromAllProjectsPopulated(t *testing.T) {
	tmpHome := t.TempDir()
	t.Setenv("CLAUDE_CONFIG_DIR", tmpHome)

	proj1Dir := filepath.Join(tmpHome, "projects", "proj1")
	proj2Dir := filepath.Join(tmpHome, "projects", "proj2")
	if err := os.MkdirAll(proj1Dir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(proj2Dir, 0o755); err != nil {
		t.Fatal(err)
	}

	writeRawSession := func(path string, lines []string) {
		t.Helper()
		content := ""
		for _, l := range lines {
			content += l + "\n"
		}
		if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
			t.Fatal(err)
		}
	}

	writeRawSession(filepath.Join(proj1Dir, "sess_a.jsonl"), []string{
		`{"type":"custom-title","sessionId":"sess_a","customTitle":"Alpha Session"}`,
		`{"type":"user","uuid":"u1","sessionId":"sess_a","cwd":"/repo/proj1","message":{"type":"user","content":[{"type":"text","text":"hello"}]}}`,
	})
	writeRawSession(filepath.Join(proj2Dir, "sess_b.jsonl"), []string{
		`{"type":"user","uuid":"u2","sessionId":"sess_b","cwd":"/repo/proj2","message":{"type":"user","content":[{"type":"text","text":"world"}]}}`,
	})

	entries, err := ResumeEntriesFromAllProjects()
	if err != nil {
		t.Fatalf("err = %v", err)
	}
	if len(entries) != 2 {
		t.Fatalf("expected 2 entries, got %d: %+v", len(entries), entries)
	}

	ids := make(map[string]bool)
	for _, e := range entries {
		ids[e.ID] = true
	}
	if !ids["sess_a"] || !ids["sess_b"] {
		t.Fatalf("missing expected IDs: %v", ids)
	}

	// sess_a should have the custom title.
	for _, e := range entries {
		if e.ID == "sess_a" && e.Summary != "Alpha Session" {
			t.Fatalf("sess_a summary = %q, want 'Alpha Session'", e.Summary)
		}
	}
}
