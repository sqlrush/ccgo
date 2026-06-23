package conversation

import (
	"time"

	compactpkg "ccgo/internal/compact"
	"ccgo/internal/contracts"
	"ccgo/internal/rewind"
)

// maybeCaptureRewindSnapshot captures a file-history snapshot at the start of
// a user turn (keyed by the user message UUID). Errors are discarded — a
// snapshot failure must never abort the turn. Mirrors CC QueryEngine.ts:641-654
// (fileHistoryMakeSnapshot per user message). (REWIND-01)
func (r *Runner) maybeCaptureRewindSnapshot(messageID contracts.ID) {
	if r.RewindWriter == nil || r.RewindStore == nil {
		return
	}
	paths := r.trackedFilePaths()
	if len(paths) == 0 {
		return
	}
	snap, err := r.RewindStore.Capture(messageID, paths, time.Now().UTC())
	if err != nil {
		// Non-fatal: log via emit but do not abort.
		return
	}
	_ = r.RewindWriter.Record(snap, false)
}

// trackedFilePaths returns the list of file paths currently tracked by the
// session-scoped ReadState, if one is configured.
func (r *Runner) trackedFilePaths() []string {
	if r.ReadState == nil {
		return nil
	}
	return r.ReadState.Paths()
}

// buildPostCompactAttachments converts the session ReadState into ReadFileEntries
// and calls BuildPostCompactAttachments. Returns nil when ReadState is nil or empty.
// (COMPACT-05)
func (r *Runner) buildPostCompactAttachments(preservedPaths map[string]bool) []contracts.Message {
	if r.ReadState == nil {
		return nil
	}
	entries := r.ReadState.Entries()
	if len(entries) == 0 {
		return nil
	}
	files := make([]compactpkg.ReadFileEntry, len(entries))
	for i, e := range entries {
		files[i] = compactpkg.ReadFileEntry{
			Path:      e.Path,
			Content:   e.Content,
			Timestamp: e.Timestamp,
		}
	}
	return compactpkg.BuildPostCompactAttachments(files, compactpkg.PostCompactOptions{
		PreservedPaths: preservedPaths,
	})
}

// RewindConfig holds the paths needed to construct a rewind.Writer and
// rewind.Store for injection into a Runner. SessionPath is the transcript
// JSONL file; the backup store lives in the same parent directory.
type RewindConfig struct {
	SessionPath string
}

// NewRewindPair constructs a matched Writer+Store from a RewindConfig. Returns
// nil pointers when SessionPath is empty (rewind disabled).
func NewRewindPair(cfg RewindConfig) (*rewind.Writer, *rewind.Store) {
	if cfg.SessionPath == "" {
		return nil, nil
	}
	w := &rewind.Writer{TranscriptPath: cfg.SessionPath}
	s := rewind.NewStore(cfg.SessionPath + ".d")
	return w, &s
}
