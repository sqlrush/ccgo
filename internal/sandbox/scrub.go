package sandbox

// ScrubBareGitRepoFiles removes planted bare git-repo files that were created
// inside the sandbox during a Bash command.  When Linux bwrap or macOS sandbox-exec
// confines a command, the sandbox may create stub entries for paths it denied
// write access to.  Removing them post-command prevents later un-sandboxed git
// operations from seeing spurious files (e.g. a 0-byte HEAD that breaks
// `git log HEAD`).
//
// The canonical bare-repo files are: HEAD, objects, refs, hooks, config.
// CC ref: src/utils/sandbox/sandbox-adapter.ts:scrubBareGitRepoFiles (SBX-43).
//
// Only plain files and empty directories are removed — non-empty directories are
// skipped to avoid accidental data loss if the user genuinely created content.

import (
	"io/fs"
	"os"
	"path/filepath"
)

// bareGitRepoFiles are the entry names planted by sandbox-exec in the cwd
// of a bare git repository to deny write access during the command.
// Matches CC's bareGitRepoFiles list (sandbox-adapter.ts:270).
var bareGitRepoFiles = []string{
	"HEAD",
	"objects",
	"refs",
	"hooks",
	"config",
}

// ScrubBareGitRepoFiles removes sandbox-planted bare-repo stub files/dirs
// from dir.  It is best-effort: errors are silently ignored (ENOENT is the
// expected common case — nothing was planted).
// CC ref: src/utils/sandbox/sandbox-adapter.ts:scrubBareGitRepoFiles (SBX-43).
func ScrubBareGitRepoFiles(dir string) {
	for _, name := range bareGitRepoFiles {
		p := filepath.Join(dir, name)
		info, err := os.Lstat(p)
		if err != nil {
			// ENOENT is the common case — nothing planted.
			continue
		}
		if info.Mode()&fs.ModeType == 0 {
			// Plain file — remove it.
			_ = os.Remove(p)
			continue
		}
		if info.IsDir() {
			// Only remove if the directory is empty to avoid data loss.
			entries, readErr := os.ReadDir(p)
			if readErr == nil && len(entries) == 0 {
				_ = os.Remove(p)
			}
		}
	}
}
