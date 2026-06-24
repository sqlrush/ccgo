package repl

import (
	"context"
	"os"
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
// It checks well-known environment variables without spawning child processes.
// This covers the common "running inside a terminal in an IDE" scenario and
// matches CC's env-var-first detection path (utils/env.ts, utils/ide.ts).
// CC ref: src/utils/ide.ts detectIDEs; src/utils/env.ts (terminal detection).
func defaultIDEDetect() []string {
	var found []string

	// Cursor: sets CURSOR_TRACE_ID when the terminal is inside Cursor.
	// CC ref: src/utils/env.ts:136.
	if os.Getenv("CURSOR_TRACE_ID") != "" {
		found = append(found, "Cursor")
	}

	// VS Code: sets VSCODE_PID or VSCODE_IPC_HOOK_CLI.
	// TERM_PROGRAM=vscode is used by VS Code and Cursor (and Windsurf under WSL).
	// We already checked Cursor above, so TERM_PROGRAM=vscode here means VS Code.
	// CC ref: src/utils/env.ts:181-182.
	if os.Getenv("VSCODE_PID") != "" || os.Getenv("VSCODE_IPC_HOOK_CLI") != "" {
		if !sliceContains(found, "Cursor") {
			found = append(found, "VS Code")
		}
	} else if termProg := os.Getenv("TERM_PROGRAM"); termProg == "vscode" {
		if !sliceContains(found, "Cursor") {
			found = append(found, "VS Code")
		}
	}

	// Windsurf: sets WINDSURF_TRACE_ID.
	if os.Getenv("WINDSURF_TRACE_ID") != "" {
		found = append(found, "Windsurf")
	}

	// JetBrains: sets TERMINAL_EMULATOR=JetBrains-JediTerm.
	// CC ref: src/utils/jetbrains.ts (checks process ancestry; we use env only).
	if strings.Contains(os.Getenv("TERMINAL_EMULATOR"), "JetBrains") {
		found = append(found, "JetBrains")
	}

	return found
}

// sliceContains returns true when s is in the slice (case-sensitive).
func sliceContains(slice []string, s string) bool {
	for _, v := range slice {
		if v == s {
			return true
		}
	}
	return false
}
