package session

import (
	"os"
	"path/filepath"
	"time"

	"ccgo/internal/platform"
)

// CleanupOldTranscripts removes JSONL transcript files older than retentionDays
// from all project directories under the claude home directory.
// When retentionDays is 0, cleanup is disabled (no files are removed).
// CC ref: utils/settings/types.ts cleanupPeriodDays (CFG-13).
func CleanupOldTranscripts(retentionDays int) error {
	if retentionDays <= 0 {
		return nil
	}
	projectsDir := filepath.Join(platform.ClaudeHomeDir(), "projects")
	cutoff := time.Now().Add(-time.Duration(retentionDays) * 24 * time.Hour)

	entries, err := os.ReadDir(projectsDir)
	if err != nil {
		if os.IsNotExist(err) {
			return nil
		}
		return err
	}
	for _, entry := range entries {
		if !entry.IsDir() {
			continue
		}
		projectDir := filepath.Join(projectsDir, entry.Name())
		if err := cleanupTranscriptsInDir(projectDir, cutoff); err != nil {
			// Non-fatal: log but continue cleaning other projects.
			_ = err
		}
	}
	return nil
}

// cleanupTranscriptsInDir removes JSONL files older than cutoff in dir.
func cleanupTranscriptsInDir(dir string, cutoff time.Time) error {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return err
	}
	for _, entry := range entries {
		if entry.IsDir() || filepath.Ext(entry.Name()) != ".jsonl" {
			continue
		}
		info, err := entry.Info()
		if err != nil {
			continue
		}
		if info.ModTime().Before(cutoff) {
			_ = os.Remove(filepath.Join(dir, entry.Name()))
		}
	}
	return nil
}
