package rewind

import (
	"ccgo/internal/contracts"
	"ccgo/internal/session"
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

// SnapshotTranscriptMessage builds a session.TranscriptMessage whose Type and
// UUID are set correctly so the session parser recognises the line as a
// file-history-snapshot and indexes it by the snapshot's messageId (via the
// uuid fallback in parseSnapshotMessageID).
//
// When the line is marshaled by Writer.Record it is written as a snapshotLine
// directly; this function is used by callers that need a TranscriptMessage
// handle (e.g. tests writing their own JSONL).
func SnapshotTranscriptMessage(snap Snapshot, _ bool) session.TranscriptMessage {
	return session.TranscriptMessage{
		Type: SnapshotType,
		UUID: snap.MessageID,
	}
}
