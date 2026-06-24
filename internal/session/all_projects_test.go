package session

// SESS-09/10: cross-project resume picker entries.
//
// ListAllProjectSessions lists sessions from all known project directories
// under the Claude home (similar to FindSessionGlobally but returns full entries).

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// TestListAllProjectSessionsEmptyDir returns nil when the projects dir is absent.
func TestListAllProjectSessionsEmptyDir(t *testing.T) {
	tmpHome := t.TempDir()
	t.Setenv("CLAUDE_CONFIG_DIR", tmpHome)

	sessions, err := ListAllProjectSessions()
	if err != nil {
		t.Fatalf("err = %v, want nil", err)
	}
	if len(sessions) != 0 {
		t.Fatalf("expected 0 sessions, got %d", len(sessions))
	}
}

// TestListAllProjectSessionsAcrossProjects verifies that sessions from
// two different project directories are both returned and sorted newest-first.
func TestListAllProjectSessionsAcrossProjects(t *testing.T) {
	tmpHome := t.TempDir()
	t.Setenv("CLAUDE_CONFIG_DIR", tmpHome)

	// Create two project directories with one session each.
	proj1Dir := filepath.Join(tmpHome, "projects", "proj1")
	proj2Dir := filepath.Join(tmpHome, "projects", "proj2")
	if err := os.MkdirAll(proj1Dir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(proj2Dir, 0o755); err != nil {
		t.Fatal(err)
	}

	writeRawSession(t, filepath.Join(proj1Dir, "sess_a.jsonl"), []string{
		`{"type":"custom-title","sessionId":"sess_a","customTitle":"Proj1 Session"}`,
		`{"type":"user","uuid":"u1","sessionId":"sess_a","cwd":"/repo/proj1","message":{"type":"user","content":[{"type":"text","text":"hello from proj1"}]}}`,
	})
	writeRawSession(t, filepath.Join(proj2Dir, "sess_b.jsonl"), []string{
		`{"type":"user","uuid":"u2","sessionId":"sess_b","cwd":"/repo/proj2","message":{"type":"user","content":[{"type":"text","text":"hello from proj2"}]}}`,
	})

	sessions, err := ListAllProjectSessions()
	if err != nil {
		t.Fatalf("err = %v", err)
	}
	if len(sessions) != 2 {
		t.Fatalf("expected 2 sessions across 2 projects, got %d: %+v", len(sessions), sessions)
	}

	// Both sessions should be present.
	ids := make(map[string]bool)
	for _, s := range sessions {
		ids[string(s.ID)] = true
	}
	if !ids["sess_a"] || !ids["sess_b"] {
		t.Fatalf("missing expected session ids: %v", ids)
	}

	// sess_a has a custom title.
	for _, s := range sessions {
		if string(s.ID) == "sess_a" && !strings.Contains(s.Title, "Proj1 Session") {
			t.Fatalf("sess_a title = %q, want 'Proj1 Session'", s.Title)
		}
	}
}

// TestListAllProjectSessionsSkipsNonDirs verifies that non-directory entries
// in the projects folder are silently ignored.
func TestListAllProjectSessionsSkipsNonDirs(t *testing.T) {
	tmpHome := t.TempDir()
	t.Setenv("CLAUDE_CONFIG_DIR", tmpHome)

	projectsDir := filepath.Join(tmpHome, "projects")
	if err := os.MkdirAll(projectsDir, 0o755); err != nil {
		t.Fatal(err)
	}
	// Create a stray file in the projects directory.
	if err := os.WriteFile(filepath.Join(projectsDir, "stray.txt"), []byte("junk"), 0o600); err != nil {
		t.Fatal(err)
	}
	// Create one real project.
	proj1Dir := filepath.Join(projectsDir, "proj1")
	if err := os.MkdirAll(proj1Dir, 0o755); err != nil {
		t.Fatal(err)
	}
	writeRawSession(t, filepath.Join(proj1Dir, "sess_x.jsonl"), []string{
		`{"type":"user","uuid":"u1","sessionId":"sess_x","message":{"type":"user","content":[{"type":"text","text":"test"}]}}`,
	})

	sessions, err := ListAllProjectSessions()
	if err != nil {
		t.Fatalf("err = %v", err)
	}
	if len(sessions) != 1 {
		t.Fatalf("expected 1 session, got %d", len(sessions))
	}
}
