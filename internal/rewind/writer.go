package rewind

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"

	"ccgo/internal/platform"
)

// Writer appends file-history-snapshot lines to a session transcript.
type Writer struct {
	TranscriptPath string
}

// Record marshals snap as a canonical snapshotLine and appends it to the
// transcript file. The full CC wire shape (type, messageId, isSnapshotUpdate,
// snapshot) is preserved so the session parser indexes it correctly.
func (w Writer) Record(snap Snapshot, isUpdate bool) error {
	line := snapshotLine{
		Type:             SnapshotType,
		MessageID:        snap.MessageID,
		IsSnapshotUpdate: isUpdate,
		Snapshot:         snap,
	}
	data, err := json.Marshal(line)
	if err != nil {
		return fmt.Errorf("rewind: marshal snapshot: %w", err)
	}

	if err := platform.EnsureDir(filepath.Dir(w.TranscriptPath)); err != nil {
		return fmt.Errorf("rewind: ensure transcript dir: %w", err)
	}

	f, err := os.OpenFile(w.TranscriptPath, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o600)
	if err != nil {
		return fmt.Errorf("rewind: open transcript: %w", err)
	}
	defer f.Close()

	if _, err := f.Write(append(data, '\n')); err != nil {
		return fmt.Errorf("rewind: write snapshot line: %w", err)
	}
	return nil
}
