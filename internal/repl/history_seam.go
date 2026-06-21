package repl

import (
	"os"

	"ccgo/internal/contracts"
	"ccgo/internal/session"
)

// HistoryRecorder appends submitted prompts to ~/.claude/history.jsonl.
// It mirrors CC's history recording behaviour (history.ts) and respects
// the CLAUDE_CODE_SKIP_PROMPT_HISTORY env var for opt-out.
type HistoryRecorder struct {
	Path      string
	Project   string
	SessionID contracts.ID
	// Skip disables recording without returning an error (opt-out parity with CC).
	Skip bool
}

// NewHistoryRecorder builds a recorder whose Path is the canonical
// ~/.claude/history.jsonl.  Skip is set when CLAUDE_CODE_SKIP_PROMPT_HISTORY=true
// (CC parity, history.ts:414).
func NewHistoryRecorder(project string, sessionID contracts.ID) HistoryRecorder {
	return HistoryRecorder{
		Path:      session.HistoryPath(),
		Project:   project,
		SessionID: sessionID,
		Skip:      os.Getenv("CLAUDE_CODE_SKIP_PROMPT_HISTORY") == "true",
	}
}

// Record appends prompt to history.jsonl.  Empty prompts and skip-mode are
// silently ignored.  History failures are returned but must not abort the
// turn — callers should discard with `_ =`.
func (r HistoryRecorder) Record(prompt string) error {
	if r.Skip || prompt == "" {
		return nil
	}
	_, err := session.AddToHistory(r.Path, r.Project, r.SessionID, session.HistoryEntry{Display: prompt})
	if err != nil {
		return err
	}
	return nil
}
