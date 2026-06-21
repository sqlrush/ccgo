package repl

import (
	"ccgo/internal/contracts"
	"ccgo/internal/rewind"
)

// RewindToMessage restores the working tree to the snapshot recorded for
// messageID.  It delegates to rewind.Rewind with a Store rooted at sessionDir.
// cwd is the project root: every restore path must reside under it.
//
// The interactive /rewind UI (snapshot picker + confirmation dialog) belongs
// to Phase 2 / Phase 6b.  This function is the pure-logic seam that the
// future command handler calls.
func RewindToMessage(transcriptPath string, messageID contracts.ID, sessionDir string, cwd string) (rewind.RewindResult, error) {
	return rewind.Rewind(transcriptPath, messageID, rewind.NewStore(sessionDir), cwd)
}
