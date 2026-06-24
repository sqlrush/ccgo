package repl

import (
	"bytes"
	"os/exec"
	"strings"
)

// SetFileSuggestionCmd wires the shell command whose output (one path per line)
// is used to populate the QuickOpen overlay instead of walking the filesystem.
// CFG-40: CC ref: utils/settings/types.ts fileSuggestion:{type:"command",command:string}.
func (l *Loop) SetFileSuggestionCmd(cmd string) {
	l.fileSuggestionCmd = cmd
}

// fileSuggestionFiles runs l.fileSuggestionCmd and returns the list of paths from
// its stdout (one path per line, empty lines and surrounding whitespace trimmed).
// Returns nil on any error so callers fall back to filesystem walking.
func (l *Loop) fileSuggestionFiles() []string {
	if l.fileSuggestionCmd == "" {
		return nil
	}
	out, err := exec.Command("sh", "-c", l.fileSuggestionCmd).Output() //nolint:gosec
	if err != nil {
		return nil
	}
	var paths []string
	for _, line := range bytes.Split(out, []byte("\n")) {
		p := strings.TrimSpace(string(line))
		if p != "" {
			paths = append(paths, p)
		}
	}
	return paths
}
