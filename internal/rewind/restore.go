package rewind

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"ccgo/internal/contracts"
	"ccgo/internal/session"
)

// isUnderRoot reports whether path is contained within root.
// Both arguments must be cleaned absolute paths.
// Returns an error when path escapes root.
func isUnderRoot(root, path string) error {
	rel, err := filepath.Rel(root, path)
	if err != nil {
		return fmt.Errorf("rewind: cannot compute relative path from root %q to %q: %w", root, path, err)
	}
	if rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) || filepath.IsAbs(rel) {
		return fmt.Errorf("rewind: path %q escapes project root %q", path, root)
	}
	return nil
}

// resolveRoot returns the canonical absolute path for root, resolving symlinks
// so that containment checks work correctly on platforms where t.TempDir() and
// os.Getwd() return paths that differ only by a symlink (e.g. macOS
// /var/folders vs /private/var/folders).
func resolveRoot(root string) (string, error) {
	abs, err := filepath.Abs(root)
	if err != nil {
		return "", fmt.Errorf("rewind: abs root %q: %w", root, err)
	}
	resolved, err := filepath.EvalSymlinks(abs)
	if err != nil {
		// Root does not exist yet — fall back to the absolute-but-unresolved path.
		return abs, nil
	}
	return resolved, nil
}

// confinedPath validates that path is absolute, cleans it, resolves symlinks of
// the parent directory (so a symlink pointing outside root is caught), and
// checks that the result is contained within root.
// Returns the cleaned resolved absolute path on success.
func confinedPath(root, path string) (string, error) {
	if root == "" {
		return "", fmt.Errorf("rewind: Root must not be empty for a destructive restore")
	}
	if !filepath.IsAbs(path) {
		return "", fmt.Errorf("rewind: refuse non-absolute restore path %q", path)
	}
	clean := filepath.Clean(path)

	// Resolve symlinks on the parent dir so a symlink inside root that points
	// outside is detected before we write or delete.
	parent := filepath.Dir(clean)
	resolvedParent, err := filepath.EvalSymlinks(parent)
	if err != nil {
		// Parent does not exist yet (new file to be created) — use the cleaned
		// path as-is; the containment check on clean is still applied.
		resolvedParent = parent
	}
	resolved := filepath.Join(resolvedParent, filepath.Base(clean))

	absRoot, err := resolveRoot(root)
	if err != nil {
		return "", err
	}
	if err := isUnderRoot(absRoot, resolved); err != nil {
		return "", err
	}
	return resolved, nil
}

// Restore applies a snapshot to disk. For each tracked path it either rewrites
// the file from its backup or deletes it (if the backup name is empty, meaning
// the file did not exist when the snapshot was taken).
//
// Root must be non-empty: every path in the snapshot is validated to reside
// under Root before any write or delete is performed. Paths outside Root are
// skipped and collected as errors (non-fatal); the restore continues for
// remaining in-root paths. An empty Root is a programming error and causes
// Restore to return immediately with an error without touching any files.
func Restore(snap Snapshot, store Store, root string) ([]string, []error, error) {
	if root == "" {
		return nil, nil, fmt.Errorf("rewind: Root must not be empty for a destructive restore")
	}
	var changed []string
	var skipped []error
	for path, backup := range snap.TrackedFileBackups {
		confined, err := confinedPath(root, path)
		if err != nil {
			skipped = append(skipped, err)
			continue
		}
		if backup.BackupFileName == "" {
			if err := os.Remove(confined); err != nil && !os.IsNotExist(err) {
				return changed, skipped, fmt.Errorf("rewind: remove %q: %w", confined, err)
			}
			changed = append(changed, confined)
			continue
		}
		data, err := os.ReadFile(filepath.Join(store.Dir, backup.BackupFileName))
		if err != nil {
			return changed, skipped, fmt.Errorf("rewind: read backup for %q: %w", confined, err)
		}
		if err := os.MkdirAll(filepath.Dir(confined), 0o755); err != nil {
			return changed, skipped, fmt.Errorf("rewind: mkdir for %q: %w", confined, err)
		}
		if err := os.WriteFile(confined, data, 0o644); err != nil {
			return changed, skipped, fmt.Errorf("rewind: write %q: %w", confined, err)
		}
		changed = append(changed, confined)
	}
	return changed, skipped, nil
}

// RewindResult carries the result of a Rewind call.
type RewindResult struct {
	Snapshot  Snapshot
	Changed   []string
	Skipped   []error // paths outside root that were rejected (non-fatal)
	MessageID contracts.ID
}

// Rewind loads the transcript, finds the snapshot for messageID, and applies it.
// root must be non-empty and confines which paths may be written or deleted.
func Rewind(transcriptPath string, messageID contracts.ID, store Store, root string) (RewindResult, error) {
	if root == "" {
		return RewindResult{}, fmt.Errorf("rewind: Root must not be empty for a destructive restore")
	}
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
	changed, skipped, err := Restore(snap, store, root)
	if err != nil {
		return RewindResult{}, err
	}
	return RewindResult{Snapshot: snap, Changed: changed, Skipped: skipped, MessageID: messageID}, nil
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
