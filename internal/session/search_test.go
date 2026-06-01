package session

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"ccgo/internal/contracts"
)

func TestListProjectSessionsSortsAndBuildsTitles(t *testing.T) {
	t.Setenv("CLAUDE_CONFIG_DIR", t.TempDir())
	root := "/repo/project"
	dir := ProjectDir(root)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	first := filepath.Join(dir, "sess_1.jsonl")
	second := filepath.Join(dir, "sess_2.jsonl")
	writeRawSession(t, first, []string{
		`{"type":"user","uuid":"u1","sessionId":"sess_1","message":{"type":"user","content":[{"type":"text","text":"first title from prompt"}]}}`,
	})
	writeRawSession(t, second, []string{
		`{"type":"custom-title","sessionId":"sess_2","customTitle":"Custom Title"}`,
		`{"type":"user","uuid":"u2","sessionId":"sess_2","message":{"type":"user","content":[{"type":"text","text":"second prompt"}]}}`,
	})
	if err := os.Chtimes(first, time.Unix(10, 0), time.Unix(10, 0)); err != nil {
		t.Fatal(err)
	}
	if err := os.Chtimes(second, time.Unix(20, 0), time.Unix(20, 0)); err != nil {
		t.Fatal(err)
	}

	sessions, err := ListProjectSessions(root)
	if err != nil {
		t.Fatal(err)
	}
	if len(sessions) != 2 {
		t.Fatalf("sessions = %#v", sessions)
	}
	if sessions[0].ID != "sess_2" || sessions[0].Title != "Custom Title" {
		t.Fatalf("first session = %#v", sessions[0])
	}
	if sessions[1].Title != "first title from prompt" {
		t.Fatalf("second title = %#v", sessions[1])
	}
}

func TestSearchProjectSessionsFindsTranscriptText(t *testing.T) {
	t.Setenv("CLAUDE_CONFIG_DIR", t.TempDir())
	root := "/repo/project"
	dir := ProjectDir(root)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	writeRawSession(t, filepath.Join(dir, "sess_1.jsonl"), []string{
		`{"type":"user","uuid":"u1","sessionId":"sess_1","message":{"type":"user","content":[{"type":"text","text":"implement compact memory support"}]}}`,
		`{"type":"assistant","uuid":"a1","parentUuid":"u1","sessionId":"sess_1","message":{"type":"assistant","content":[{"type":"text","text":"done"}]}}`,
	})
	writeRawSession(t, filepath.Join(dir, "sess_2.jsonl"), []string{
		`{"type":"user","uuid":"u2","sessionId":"sess_2","message":{"type":"user","content":[{"type":"text","text":"unrelated"}]}}`,
	})

	results, err := SearchProjectSessions(root, "compact", 10)
	if err != nil {
		t.Fatal(err)
	}
	if len(results) != 1 || results[0].ID != contracts.ID("sess_1") {
		t.Fatalf("results = %#v", results)
	}
	if len(results[0].Matches) != 1 || !strings.Contains(results[0].Matches[0], "compact memory") {
		t.Fatalf("matches = %#v", results[0].Matches)
	}
}

func writeRawSession(t *testing.T, path string, lines []string) {
	t.Helper()
	if err := os.WriteFile(path, []byte(strings.Join(lines, "\n")+"\n"), 0o600); err != nil {
		t.Fatal(err)
	}
}
