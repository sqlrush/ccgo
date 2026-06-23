package session

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"ccgo/internal/contracts"
)

// TestForkSessionCreatesNewTranscriptFile verifies that ForkSession writes a
// new JSONL transcript file under the same project directory as the source.
func TestForkSessionCreatesNewTranscriptFile(t *testing.T) {
	root := t.TempDir()
	srcID := contracts.NewID()
	srcPath := TranscriptPath(root, srcID)
	if err := os.MkdirAll(filepath.Dir(srcPath), 0o700); err != nil {
		t.Fatal(err)
	}
	msg := TranscriptMessage{
		Type:      "user",
		UUID:      contracts.NewID(),
		SessionID: srcID,
	}
	line, _ := json.Marshal(msg)
	if err := os.WriteFile(srcPath, append(line, '\n'), 0o600); err != nil {
		t.Fatal(err)
	}

	result, err := ForkSession(srcID, root, "My Branch")
	if err != nil {
		t.Fatalf("ForkSession: %v", err)
	}
	if result.SessionID == srcID {
		t.Fatal("fork must have a different session ID")
	}
	if result.SessionID == "" {
		t.Fatal("fork session ID must not be empty")
	}
	forkPath := TranscriptPath(root, result.SessionID)
	data, err := os.ReadFile(forkPath)
	if err != nil {
		t.Fatalf("fork transcript not found: %v", err)
	}
	if len(data) == 0 {
		t.Fatal("fork transcript must not be empty")
	}
}

// TestForkSessionRewritesSessionID verifies that the forked transcript uses
// the new session ID (not the original) in every message line.
func TestForkSessionRewritesSessionID(t *testing.T) {
	root := t.TempDir()
	srcID := contracts.NewID()
	srcPath := TranscriptPath(root, srcID)
	if err := os.MkdirAll(filepath.Dir(srcPath), 0o700); err != nil {
		t.Fatal(err)
	}
	msgID := contracts.NewID()
	msg := TranscriptMessage{
		Type:      "user",
		UUID:      msgID,
		SessionID: srcID,
	}
	line, _ := json.Marshal(msg)
	if err := os.WriteFile(srcPath, append(line, '\n'), 0o600); err != nil {
		t.Fatal(err)
	}

	result, err := ForkSession(srcID, root, "")
	if err != nil {
		t.Fatalf("ForkSession: %v", err)
	}
	forkPath := TranscriptPath(root, result.SessionID)
	data, err := os.ReadFile(forkPath)
	if err != nil {
		t.Fatalf("fork transcript: %v", err)
	}
	for _, rawLine := range strings.Split(strings.TrimSpace(string(data)), "\n") {
		if rawLine == "" {
			continue
		}
		var envelope struct {
			SessionID contracts.ID `json:"sessionId"`
		}
		if err := json.Unmarshal([]byte(rawLine), &envelope); err != nil {
			continue
		}
		if envelope.SessionID != "" && envelope.SessionID == srcID {
			t.Fatalf("fork transcript still contains original session ID %q", srcID)
		}
	}
}

// TestForkSessionEmptyTranscriptErrors verifies that ForkSession returns an
// error when the source transcript is empty (nothing to branch).
func TestForkSessionEmptyTranscriptErrors(t *testing.T) {
	root := t.TempDir()
	srcID := contracts.NewID()
	srcPath := TranscriptPath(root, srcID)
	if err := os.MkdirAll(filepath.Dir(srcPath), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(srcPath, []byte{}, 0o600); err != nil {
		t.Fatal(err)
	}
	_, err := ForkSession(srcID, root, "")
	if err == nil {
		t.Fatal("ForkSession must return an error for an empty transcript")
	}
}

// TestForkSessionMissingTranscriptErrors verifies that ForkSession returns an
// error when the source transcript does not exist.
func TestForkSessionMissingTranscriptErrors(t *testing.T) {
	root := t.TempDir()
	srcID := contracts.NewID()
	_, err := ForkSession(srcID, root, "")
	if err == nil {
		t.Fatal("ForkSession must return an error when source transcript is missing")
	}
}

// TestForkSessionCustomTitleWritten verifies that ForkSession writes a
// custom-title metadata record when a title is provided.
func TestForkSessionCustomTitleWritten(t *testing.T) {
	root := t.TempDir()
	srcID := contracts.NewID()
	srcPath := TranscriptPath(root, srcID)
	if err := os.MkdirAll(filepath.Dir(srcPath), 0o700); err != nil {
		t.Fatal(err)
	}
	msg := TranscriptMessage{
		Type:      "user",
		UUID:      contracts.NewID(),
		SessionID: srcID,
	}
	line, _ := json.Marshal(msg)
	if err := os.WriteFile(srcPath, append(line, '\n'), 0o600); err != nil {
		t.Fatal(err)
	}

	result, err := ForkSession(srcID, root, "Custom Title")
	if err != nil {
		t.Fatalf("ForkSession: %v", err)
	}
	if result.Title != "Custom Title (Branch)" {
		t.Fatalf("ForkResult.Title = %q, want %q", result.Title, "Custom Title (Branch)")
	}
	forkPath := TranscriptPath(root, result.SessionID)
	data, _ := os.ReadFile(forkPath)
	if !strings.Contains(string(data), "Custom Title (Branch)") {
		t.Fatalf("fork transcript must contain custom-title record; got:\n%s", data)
	}
}
