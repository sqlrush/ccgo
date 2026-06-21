package rewind

import (
	"ccgo/internal/contracts"
)

// SnapshotType is the transcript line type for file history snapshots.
const SnapshotType = "file-history-snapshot"

// FileBackup records a content-addressed backup entry for a single file.
type FileBackup struct {
	BackupFileName string `json:"backupFileName"`
	Version        int    `json:"version"`
	BackupTime     string `json:"backupTime"`
}

// Snapshot holds a point-in-time record of tracked file backups at a message boundary.
type Snapshot struct {
	MessageID          contracts.ID          `json:"messageId"`
	Timestamp          string                `json:"timestamp"`
	TrackedFileBackups map[string]FileBackup `json:"trackedFileBackups"`
}

// snapshotLine is the canonical JSONL wire shape emitted to the transcript
// (mirrors CC sessionStorage.ts:1090). It carries messageId at the top level
// so the session parser's parseSnapshotMessageID resolves the key correctly.
type snapshotLine struct {
	Type             string       `json:"type"`
	MessageID        contracts.ID `json:"messageId"`
	IsSnapshotUpdate bool         `json:"isSnapshotUpdate"`
	Snapshot         Snapshot     `json:"snapshot"`
}
