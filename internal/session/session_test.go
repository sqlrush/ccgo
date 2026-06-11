package session

import (
	"os"
	"path/filepath"
	"testing"

	"ccgo/internal/contracts"
	"ccgo/internal/messages"
)

func TestAppendAndLoad(t *testing.T) {
	path := filepath.Join(t.TempDir(), "session.jsonl")
	sessionID := contracts.NewID()
	msg := messages.UserText("hello")
	entry := EntryFromMessage(sessionID, msg)
	if err := Append(path, entry); err != nil {
		t.Fatal(err)
	}
	entries, err := Load(path)
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 1 {
		t.Fatalf("len(entries) = %d", len(entries))
	}
	if entries[0].Message == nil || entries[0].Message.UUID != msg.UUID {
		t.Fatalf("entry = %#v", entries[0])
	}
	if entries[0].Timestamp == "" {
		t.Fatal("timestamp not populated")
	}
}

func TestLoadAcceptsSessionEntryAliases(t *testing.T) {
	path := filepath.Join(t.TempDir(), "session.jsonl")
	line := `{"role":"user","messageID":"u1","parentMessageID":"p1","sessionID":"sess_1","message":{"type":"user","messageId":"nested_u1","sessionID":"sess_1","content":[{"type":"text","text":"hi"}]}}`
	if err := os.WriteFile(path, []byte(line+"\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	entries, err := Load(path)
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 1 {
		t.Fatalf("entries = %#v", entries)
	}
	entry := entries[0]
	if entry.Type != contracts.MessageUser || entry.UUID != "u1" || entry.ParentUUID == nil || *entry.ParentUUID != "p1" || entry.SessionID != "sess_1" {
		t.Fatalf("aliased entry = %#v", entry)
	}
	if entry.Message == nil || entry.Message.ID != "nested_u1" || entry.Message.UUID != "nested_u1" || entry.Message.SessionID != "sess_1" {
		t.Fatalf("aliased nested message = %#v", entry.Message)
	}
}

func TestLoadAcceptsNormalizedSessionEntryAliases(t *testing.T) {
	path := filepath.Join(t.TempDir(), "session.jsonl")
	line := `{"Entry Type":"assistant-message","Message-ID":202,"Parent Message ID":201,"Session-ID":"sess_norm","Created At":"2026-01-01T00:00:02Z","message":{"Message-Type":"assistant-message","Message Text":"done"}}`
	if err := os.WriteFile(path, []byte(line+"\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	entries, err := Load(path)
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 1 {
		t.Fatalf("entries = %#v", entries)
	}
	entry := entries[0]
	if entry.Type != contracts.MessageAssistant || entry.UUID != "202" || entry.ParentUUID == nil || *entry.ParentUUID != "201" {
		t.Fatalf("normalized entry IDs = %#v", entry)
	}
	if entry.SessionID != "sess_norm" || entry.Timestamp != "2026-01-01T00:00:02Z" {
		t.Fatalf("normalized entry metadata = %#v", entry)
	}
	if entry.Message == nil || entry.Message.Type != contracts.MessageAssistant || len(entry.Message.Content) != 1 || entry.Message.Content[0].Text != "done" {
		t.Fatalf("normalized nested message = %#v", entry.Message)
	}
}
