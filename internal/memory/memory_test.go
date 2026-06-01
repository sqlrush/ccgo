package memory

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"ccgo/internal/session"
)

func TestScanDirectoryParsesFrontmatterAndFormatsManifest(t *testing.T) {
	dir := t.TempDir()
	writeFile(t, filepath.Join(dir, "a.md"), "---\ndescription: Alpha\ntype: project\n---\nbody\n")
	writeFile(t, filepath.Join(dir, "nested", "b.md"), "---\ndescription: Beta\ntype: team\n---\nbody\n")
	writeFile(t, filepath.Join(dir, "MEMORY.md"), "ignored")
	if err := os.Chtimes(filepath.Join(dir, "a.md"), time.Unix(10, 0), time.Unix(10, 0)); err != nil {
		t.Fatal(err)
	}
	if err := os.Chtimes(filepath.Join(dir, "nested", "b.md"), time.Unix(20, 0), time.Unix(20, 0)); err != nil {
		t.Fatal(err)
	}

	headers, err := ScanDirectory(dir, ScanOptions{})
	if err != nil {
		t.Fatal(err)
	}
	if len(headers) != 2 {
		t.Fatalf("headers = %#v", headers)
	}
	if headers[0].Filename != "nested/b.md" || headers[0].Description != "Beta" || headers[0].Type != TypeTeam {
		t.Fatalf("first header = %#v", headers[0])
	}
	manifest := FormatManifest(headers)
	if !strings.Contains(manifest, "- [team] nested/b.md") || !strings.Contains(manifest, ": Beta") {
		t.Fatalf("manifest = %q", manifest)
	}
}

func TestDiscoverClaudeFilesReturnsRootToLeaf(t *testing.T) {
	root := t.TempDir()
	child := filepath.Join(root, "sub", "project")
	if err := os.MkdirAll(child, 0o755); err != nil {
		t.Fatal(err)
	}
	writeFile(t, filepath.Join(root, "CLAUDE.md"), "root")
	writeFile(t, filepath.Join(root, "sub", "CLAUDE.md"), "sub")

	files, err := DiscoverClaudeFiles(child)
	if err != nil {
		t.Fatal(err)
	}
	if len(files) != 2 {
		t.Fatalf("files = %#v", files)
	}
	if files[0].Root != root || files[1].Root != filepath.Join(root, "sub") {
		t.Fatalf("order = %#v", files)
	}
}

func TestGuardTeamMemoryWriteRejectsSecrets(t *testing.T) {
	err := GuardTeamMemoryWrite("/repo/.claude/team-memory/auth.md", "token = ghp_123456789012345678901234567890123456")
	if err == nil {
		t.Fatal("expected secret error")
	}
	if !strings.Contains(err.Error(), "line 1") {
		t.Fatalf("err = %v", err)
	}
	if err := GuardTeamMemoryWrite("/repo/notes.md", "token = ghp_123456789012345678901234567890123456"); err != nil {
		t.Fatalf("non-team memory should not be blocked: %v", err)
	}
}

func TestWriteAndLoadSessionSummary(t *testing.T) {
	root := filepath.Join(t.TempDir(), "session-memory")
	updatedAt := time.Unix(100, 0).UTC()
	written, err := WriteSessionSummary(SessionSummaryOptions{
		Root:            root,
		SessionID:       "sess_1",
		Summary:         "summary text\n",
		UpdatedAt:       updatedAt,
		LastMessageUUID: "msg_summary",
		Metadata: sessionCompactMetadata(
			"auto",
			123,
			4,
		),
	})
	if err != nil {
		t.Fatal(err)
	}
	if written.Path != filepath.Join(root, "sess_1", SessionSummaryFilename) {
		t.Fatalf("path = %q", written.Path)
	}
	loaded, err := LoadSessionSummary(written.Path)
	if err != nil {
		t.Fatal(err)
	}
	if loaded.SessionID != "sess_1" || loaded.Summary != "summary text" || !loaded.UpdatedAt.Equal(updatedAt) {
		t.Fatalf("loaded = %#v", loaded)
	}
	if loaded.Metadata.Trigger != "auto" || loaded.Metadata.PreTokens != 123 || loaded.Metadata.MessagesSummarized != 4 {
		t.Fatalf("metadata = %#v", loaded.Metadata)
	}
	headers, err := ScanDirectory(root, ScanOptions{})
	if err != nil {
		t.Fatal(err)
	}
	if len(headers) != 1 || headers[0].Type != TypeSession {
		t.Fatalf("headers = %#v", headers)
	}
}

func TestRecallSessionSummariesScoresAndLimits(t *testing.T) {
	root := filepath.Join(t.TempDir(), "session-memory")
	if _, err := WriteSessionSummary(SessionSummaryOptions{
		Root:      root,
		SessionID: "older",
		Summary:   "database migration notes",
		UpdatedAt: time.Unix(100, 0).UTC(),
		Metadata:  sessionCompactMetadata("auto", 10, 2),
	}); err != nil {
		t.Fatal(err)
	}
	if _, err := WriteSessionSummary(SessionSummaryOptions{
		Root:      root,
		SessionID: "newer",
		Summary:   "database database permissions",
		UpdatedAt: time.Unix(200, 0).UTC(),
		Metadata:  sessionCompactMetadata("auto", 10, 2),
	}); err != nil {
		t.Fatal(err)
	}
	matches, err := RecallSessionSummaries(root, "database permissions", RecallOptions{Limit: 1})
	if err != nil {
		t.Fatal(err)
	}
	if len(matches) != 1 || matches[0].Summary.SessionID != "newer" || matches[0].Score != 3 {
		t.Fatalf("matches = %#v", matches)
	}
	all, err := RecallSessionSummaries(root, "", RecallOptions{})
	if err != nil {
		t.Fatal(err)
	}
	if len(all) != 2 || all[0].Summary.SessionID != "newer" {
		t.Fatalf("all = %#v", all)
	}
	excluding, err := RecallSessionSummaries(root, "database", RecallOptions{ExcludeSessionID: "newer"})
	if err != nil {
		t.Fatal(err)
	}
	if len(excluding) != 1 || excluding[0].Summary.SessionID != "older" {
		t.Fatalf("excluding = %#v", excluding)
	}
	context := BuildRecallContext(matches)
	if !strings.Contains(context, "Relevant session memory") || !strings.Contains(context, "[newer]") {
		t.Fatalf("context = %q", context)
	}
	message := RecallContextMessage(matches)
	if message.Subtype != RecallContextSubtype || !message.IsMeta || !strings.Contains(message.Content[0].Text, "permissions") {
		t.Fatalf("message = %#v", message)
	}
}

func sessionCompactMetadata(trigger string, preTokens int, summarized int) session.CompactMetadata {
	return session.CompactMetadata{Trigger: trigger, PreTokens: preTokens, MessagesSummarized: summarized}
}

func writeFile(t *testing.T, path string, content string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
}
