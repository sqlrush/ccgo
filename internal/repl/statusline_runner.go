package repl

import (
	"context"
	"os/exec"
	"strings"
	"time"
)

const statusLineCmdTimeout = 2 * time.Second

// RunStatusLineCommand executes the given shell command with a short timeout and
// returns its trimmed stdout. An empty command returns ("", nil).
// CC ref: src/bridge/bridgeUI.ts renderStatusLine — the statusLine.command is
// executed and its stdout is displayed in the status bar.
func RunStatusLineCommand(command string) (string, error) {
	command = strings.TrimSpace(command)
	if command == "" {
		return "", nil
	}
	ctx, cancel := context.WithTimeout(context.Background(), statusLineCmdTimeout)
	defer cancel()
	//nolint:gosec // command is from user-controlled settings, not raw user input
	cmd := exec.CommandContext(ctx, "sh", "-c", command)
	out, err := cmd.Output()
	if err != nil {
		// Non-zero exit or timeout: return empty to avoid polluting the status bar.
		return "", nil
	}
	return strings.TrimSpace(string(out)), nil
}
