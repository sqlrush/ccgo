package repl

import (
	"context"
	"fmt"
	"strconv"
	"strings"

	"ccgo/internal/contracts"
	"ccgo/internal/session"
)

// resumeEntry is the minimal session-listing row the resume handler needs.
type resumeEntry struct {
	ID    contracts.ID
	Path  string
	Title string
}

type resumeLister func() ([]resumeEntry, error)
type resumeLoader func(path string, id contracts.ID) ([]contracts.Message, error)

// resumeHandlerWith is the dependency-injected core (testable without disk).
func resumeHandlerWith(list resumeLister, load resumeLoader) CommandHandler {
	return func(ctx context.Context, cc CommandContext) (CommandOutcome, error) {
		entries, err := list()
		if err != nil {
			return CommandOutcome{}, fmt.Errorf("list sessions: %w", err)
		}
		arg := strings.TrimSpace(cc.Args)
		if arg == "" {
			return CommandOutcome{Handled: true, Status: formatResumeList(entries)}, nil
		}
		entry, ok := resolveResumeTarget(entries, arg)
		if !ok {
			return CommandOutcome{Handled: true, Status: fmt.Sprintf("No session matched %q.", arg)}, nil
		}
		msgs, err := load(entry.Path, entry.ID)
		if err != nil {
			return CommandOutcome{}, fmt.Errorf("load session %s: %w", entry.ID, err)
		}
		return CommandOutcome{
			Handled:        true,
			ReplaceHistory: true,
			NewHistory:     msgs,
			Status:         fmt.Sprintf("Resumed %s (%d messages)", entry.ID, len(msgs)),
		}, nil
	}
}

// resumeHandler builds the production handler over the real session store.
func resumeHandler(cwd string) CommandHandler {
	return resumeHandlerWith(
		func() ([]resumeEntry, error) {
			infos, err := session.ListProjectSessions(cwd)
			if err != nil {
				return nil, err
			}
			out := make([]resumeEntry, 0, len(infos))
			for _, info := range infos {
				out = append(out, resumeEntry{ID: info.ID, Path: info.Path, Title: info.Title})
			}
			return out, nil
		},
		func(path string, id contracts.ID) ([]contracts.Message, error) {
			resumed, err := session.BuildResumeConversation(path, "")
			if err != nil {
				return nil, err
			}
			if !resumed.Found {
				return nil, fmt.Errorf("session %s has no resumable messages", id)
			}
			return resumed.Messages, nil
		},
	)
}

// resolveResumeTarget matches arg against the entry list using three strategies:
// exact ID match, 1-based index, then case-insensitive substring of title or ID.
func resolveResumeTarget(entries []resumeEntry, arg string) (resumeEntry, bool) {
	// Exact ID.
	for _, e := range entries {
		if string(e.ID) == arg {
			return e, true
		}
	}
	// 1-based index.
	if n, err := strconv.Atoi(arg); err == nil && n >= 1 && n <= len(entries) {
		return entries[n-1], true
	}
	// Title / ID substring (case-insensitive).
	lower := strings.ToLower(arg)
	for _, e := range entries {
		if strings.Contains(strings.ToLower(e.Title), lower) ||
			strings.Contains(strings.ToLower(string(e.ID)), lower) {
			return e, true
		}
	}
	return resumeEntry{}, false
}

// formatResumeList renders a numbered list of sessions for display in the REPL.
func formatResumeList(entries []resumeEntry) string {
	if len(entries) == 0 {
		return "No previous sessions found."
	}
	lines := []string{"Resumable sessions (use /resume <number|id|search>):"}
	for i, e := range entries {
		title := strings.TrimSpace(e.Title)
		if title == "" {
			title = string(e.ID)
		}
		lines = append(lines, fmt.Sprintf("  %d. %s  (%s)", i+1, title, e.ID))
	}
	return strings.Join(lines, "\n")
}
