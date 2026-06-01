package session

import (
	"bufio"
	"encoding/json"
	"os"
	"path/filepath"
	"time"

	"ccgo/internal/contracts"
	"ccgo/internal/platform"
)

func ProjectDir(root string) string {
	return filepath.Join(platform.ClaudeHomeDir(), "projects", platform.SanitizeProjectPath(root))
}

func TranscriptPath(root string, sessionID contracts.ID) string {
	return filepath.Join(ProjectDir(root), string(sessionID)+".jsonl")
}

func Append(path string, entry contracts.SessionEntry) error {
	if entry.Timestamp == "" {
		entry.Timestamp = time.Now().UTC().Format(time.RFC3339Nano)
	}
	if err := platform.EnsureDir(filepath.Dir(path)); err != nil {
		return err
	}
	f, err := os.OpenFile(path, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o600)
	if err != nil {
		return err
	}
	defer f.Close()
	encoded, err := json.Marshal(entry)
	if err != nil {
		return err
	}
	if _, err := f.Write(append(encoded, '\n')); err != nil {
		return err
	}
	return nil
}

func Load(path string) ([]contracts.SessionEntry, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer f.Close()

	var entries []contracts.SessionEntry
	scanner := bufio.NewScanner(f)
	buf := make([]byte, 0, 1024*1024)
	scanner.Buffer(buf, 50*1024*1024)
	for scanner.Scan() {
		line := scanner.Bytes()
		if len(line) == 0 {
			continue
		}
		var entry contracts.SessionEntry
		if err := json.Unmarshal(line, &entry); err != nil {
			return nil, err
		}
		entries = append(entries, entry)
	}
	if err := scanner.Err(); err != nil {
		return nil, err
	}
	return entries, nil
}

func EntryFromMessage(sessionID contracts.ID, msg contracts.Message) contracts.SessionEntry {
	return contracts.SessionEntry{
		Type:       msg.Type,
		UUID:       msg.UUID,
		ParentUUID: msg.ParentUUID,
		SessionID:  sessionID,
		Message:    &msg,
	}
}
