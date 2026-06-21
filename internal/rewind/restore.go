package rewind

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"

	"ccgo/internal/contracts"
	"ccgo/internal/session"
)

// Restore applies a snapshot to disk. For each tracked path it either rewrites
// the file from its backup or deletes it (if the backup name is empty, meaning
// the file did not exist when the snapshot was taken).
func Restore(snap Snapshot, store Store) ([]string, error) {
	var changed []string
	for path, backup := range snap.TrackedFileBackups {
		if !filepath.IsAbs(path) {
			return changed, fmt.Errorf("rewind: refuse non-absolute restore path %q", path)
		}
		if backup.BackupFileName == "" {
			if err := os.Remove(path); err != nil && !os.IsNotExist(err) {
				return changed, fmt.Errorf("rewind: remove %q: %w", path, err)
			}
			changed = append(changed, path)
			continue
		}
		data, err := os.ReadFile(filepath.Join(store.Dir, backup.BackupFileName))
		if err != nil {
			return changed, fmt.Errorf("rewind: read backup for %q: %w", path, err)
		}
		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
			return changed, fmt.Errorf("rewind: mkdir for %q: %w", path, err)
		}
		if err := os.WriteFile(path, data, 0o644); err != nil {
			return changed, fmt.Errorf("rewind: write %q: %w", path, err)
		}
		changed = append(changed, path)
	}
	return changed, nil
}

// RewindResult carries the result of a Rewind call.
type RewindResult struct {
	Snapshot  Snapshot
	Changed   []string
	MessageID contracts.ID
}

// Rewind loads the transcript, finds the snapshot for messageID, and applies it.
func Rewind(transcriptPath string, messageID contracts.ID, store Store) (RewindResult, error) {
	tr, err := session.LoadTranscript(transcriptPath)
	if err != nil {
		return RewindResult{}, err
	}
	raw, ok := tr.FileHistoryByMessageID[messageID]
	if !ok {
		return RewindResult{}, fmt.Errorf("rewind: no snapshot for message %q", messageID)
	}
	snap, err := decodeSnapshot(raw)
	if err != nil {
		return RewindResult{}, err
	}
	changed, err := Restore(snap, store)
	if err != nil {
		return RewindResult{}, err
	}
	return RewindResult{Snapshot: snap, Changed: changed, MessageID: messageID}, nil
}

// decodeSnapshot extracts the Snapshot from a stored file-history-snapshot line.
// The canonical wire shape (snapshotLine) nests the payload under the "snapshot" key.
func decodeSnapshot(raw json.RawMessage) (Snapshot, error) {
	var line struct {
		Snapshot Snapshot `json:"snapshot"`
	}
	if err := json.Unmarshal(raw, &line); err != nil {
		return Snapshot{}, fmt.Errorf("rewind: decode snapshot line: %w", err)
	}
	return line.Snapshot, nil
}
