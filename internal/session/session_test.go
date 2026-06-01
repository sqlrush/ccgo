package session

import (
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
