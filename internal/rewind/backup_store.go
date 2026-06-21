package rewind

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"os"
	"path/filepath"
	"time"

	"ccgo/internal/contracts"
)

// Store is a content-addressed backup store for tracked file snapshots.
// Files with identical content share a single backup file (deduplication).
type Store struct {
	Dir string
}

// NewStore returns a Store whose backups live under sessionDir/file-history.
func NewStore(sessionDir string) Store {
	return Store{Dir: filepath.Join(sessionDir, "file-history")}
}

// Capture reads the current content of each path, writes a content-addressed
// backup for each unique file content, and returns a Snapshot keyed by
// messageID. now is injected so callers control determinism. Files that do
// not exist are recorded with an empty BackupFileName (deletion sentinel).
func (s Store) Capture(messageID contracts.ID, paths []string, now time.Time) (Snapshot, error) {
	if err := os.MkdirAll(s.Dir, 0o755); err != nil {
		return Snapshot{}, fmt.Errorf("rewind: mkdir backup dir: %w", err)
	}

	ts := now.UTC().Format(time.RFC3339Nano)
	backups := make(map[string]FileBackup, len(paths))

	for _, p := range paths {
		abs, err := filepath.Abs(p)
		if err != nil {
			return Snapshot{}, fmt.Errorf("rewind: abs %q: %w", p, err)
		}

		data, err := os.ReadFile(abs)
		if err != nil {
			// File does not exist or is unreadable — record deletion sentinel.
			backups[abs] = FileBackup{Version: 1, BackupTime: ts}
			continue
		}

		name, writeErr := s.storeContent(data)
		if writeErr != nil {
			return Snapshot{}, writeErr
		}
		backups[abs] = FileBackup{BackupFileName: name, Version: 1, BackupTime: ts}
	}

	return Snapshot{
		MessageID:          messageID,
		Timestamp:          ts,
		TrackedFileBackups: backups,
	}, nil
}

// storeContent writes data to a content-addressed file named <sha256hex>@v1
// under s.Dir. If the file already exists the write is skipped (dedup).
// Returns the backup file name.
func (s Store) storeContent(data []byte) (string, error) {
	sum := sha256.Sum256(data)
	name := hex.EncodeToString(sum[:]) + "@v1"
	dest := filepath.Join(s.Dir, name)

	if _, err := os.Stat(dest); err == nil {
		// Already backed up — dedup hit, nothing to write.
		return name, nil
	}

	if err := os.WriteFile(dest, data, 0o600); err != nil {
		return "", fmt.Errorf("rewind: write backup %q: %w", name, err)
	}
	return name, nil
}
