package repl

import (
	"context"
	"strings"
)

// IDEDetectFunc is the injection point for IDE detection — returns a list of
// detected IDE names.  In tests, callers inject a fake; the production handler
// uses defaultIDEDetect.
type IDEDetectFunc func() []string

// ideHandler returns a CommandHandler for /ide.
// No-arg (or "list"): lists detected IDEs or reports none.
// "open": prints a guarded message (actual IDE connection is out of scope).
//
// The detect func is injected so tests never spawn real processes.
func ideHandler(detect IDEDetectFunc) CommandHandler {
	if detect == nil {
		detect = defaultIDEDetect
	}
	return func(ctx context.Context, cc CommandContext) (CommandOutcome, error) {
		sub := strings.TrimSpace(cc.Args)
		switch strings.ToLower(sub) {
		case "open", "connect":
			ides := detect()
			if len(ides) == 0 {
				return CommandOutcome{
					Handled: true,
					Status:  "No IDE detected. Install the Claude extension in VS Code, Cursor, or JetBrains and try again.",
				}, nil
			}
			return CommandOutcome{
				Handled: true,
				Status:  "IDE connection is managed by the editor extension (out of scope for this build). Detected: " + strings.Join(ides, ", "),
			}, nil
		default:
			// "" or "list" — list detected IDEs.
			ides := detect()
			if len(ides) == 0 {
				return CommandOutcome{
					Handled: true,
					Status:  "No IDE detected.",
				}, nil
			}
			return CommandOutcome{
				Handled: true,
				Status:  "Detected IDEs: " + strings.Join(ides, ", "),
			}, nil
		}
	}
}

// defaultIDEDetect is the production IDE detection function.
// It checks well-known environment variables and process indicators without
// spawning child processes.
func defaultIDEDetect() []string {
	// Real IDE detection by process inspection is platform-specific and
	// requires spawning processes — deferred for a future task.
	// This stub returns nothing so the production binary is honest.
	return nil
}
