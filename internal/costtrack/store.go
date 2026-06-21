// Package costtrack persists per-project API cost and restores it on session
// resume, mirroring the lastSessionId guard in CC's cost-tracker.ts.
package costtrack

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"

	"ccgo/internal/contracts"
	"ccgo/internal/platform"
)

// ProjectCost mirrors the minimal cost fields stored in CC's project config
// (config.ts:76-105).
type ProjectCost struct {
	LastCost              float64      `json:"lastCost"`
	LastSessionID         contracts.ID `json:"lastSessionId"`
	LastTotalInputTokens  int          `json:"lastTotalInputTokens"`
	LastTotalOutputTokens int          `json:"lastTotalOutputTokens"`
}

// Options parameterises where cost files are stored. ProjectsDir is injectable
// so tests can use t.TempDir() without touching the real ~/.claude directory.
type Options struct {
	ProjectsDir string
	CWD         string
}

// DefaultOptions returns Options backed by the real Claude home directory.
func DefaultOptions(cwd string) Options {
	return Options{
		ProjectsDir: filepath.Join(platform.ClaudeHomeDir(), "projects"),
		CWD:         cwd,
	}
}

// costPath returns the canonical path for the project cost file:
//
//	<ProjectsDir>/<SanitizeProjectPath(CWD)>/cost.json
func costPath(opts Options) string {
	return filepath.Join(opts.ProjectsDir, platform.SanitizeProjectPath(opts.CWD), "cost.json")
}

// Save writes cost to the per-project cost file, creating directories as
// needed. The file is written with mode 0600 (owner read/write only).
func Save(opts Options, cost ProjectCost) error {
	path := costPath(opts)
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return fmt.Errorf("costtrack: mkdir: %w", err)
	}
	data, err := json.MarshalIndent(cost, "", "  ")
	if err != nil {
		return fmt.Errorf("costtrack: marshal: %w", err)
	}
	if err := os.WriteFile(path, data, 0o600); err != nil {
		return fmt.Errorf("costtrack: write: %w", err)
	}
	return nil
}

// Restore reads the persisted cost and applies CC's lastSessionId guard
// (cost-tracker.ts getStoredSessionCosts): it returns (cost, true, nil) only
// when the stored LastSessionID matches sessionID. A missing file or a session
// mismatch both return (_, false, nil) without an error — the caller should
// start cost tracking from zero in either case.
func Restore(opts Options, sessionID contracts.ID) (ProjectCost, bool, error) {
	data, err := os.ReadFile(costPath(opts))
	if errors.Is(err, os.ErrNotExist) {
		return ProjectCost{}, false, nil
	}
	if err != nil {
		return ProjectCost{}, false, fmt.Errorf("costtrack: read: %w", err)
	}
	var cost ProjectCost
	if err := json.Unmarshal(data, &cost); err != nil {
		return ProjectCost{}, false, fmt.Errorf("costtrack: parse: %w", err)
	}
	if cost.LastSessionID != sessionID {
		// Different session — do not carry over cost (CC's guard prevents
		// double-counting across sessions).
		return ProjectCost{}, false, nil
	}
	return cost, true, nil
}
