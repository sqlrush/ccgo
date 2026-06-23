package repl

import (
	"fmt"
	"time"

	"ccgo/internal/tui"
)

// ━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━
// OVL-40: ContextVisualizationOverlay
// Matches CC ContextVisualization.tsx — shows context distribution grouped by
// source (System/Tool/User/Assistant/CompactionBuffer). Opened by /context when
// breakdown data is available. Submit: "ctx:dismiss".
// ━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━

// ContextGroup is one labelled group of tokens for the context visualisation.
type ContextGroup struct {
	Label  string
	Tokens int
}

// ContextVisualizationOverlay displays a grouped breakdown of context usage.
type ContextVisualizationOverlay struct {
	groups   []ContextGroup
	total    int
	maxTotal int
}

// NewContextVisualizationOverlay constructs the overlay. groups should be ordered
// from most-significant (e.g. System) to least (CompactionBuffer). maxTotal is
// the model's context window size.
func NewContextVisualizationOverlay(groups []ContextGroup, maxTotal int) *ContextVisualizationOverlay {
	copied := make([]ContextGroup, len(groups))
	copy(copied, groups)
	total := 0
	for _, g := range copied {
		total += g.Tokens
	}
	return &ContextVisualizationOverlay{groups: copied, total: total, maxTotal: maxTotal}
}

func (o *ContextVisualizationOverlay) ApplyKey(key tui.Key) (OverlayResult, bool) {
	switch key.Type {
	case tui.KeyEsc, tui.KeyEnter:
		return OverlayResult{Submit: "ctx:dismiss"}, true
	default:
		return OverlayResult{}, false
	}
}

func (o *ContextVisualizationOverlay) Render(width, _ int) []string {
	lines := []string{"Context window usage:", ""}
	maxW := width - 20
	if maxW < 10 {
		maxW = 10
	}
	for _, g := range o.groups {
		pct := 0
		bar := ""
		if o.maxTotal > 0 {
			pct = g.Tokens * 100 / o.maxTotal
			barLen := g.Tokens * maxW / o.maxTotal
			for i := 0; i < barLen; i++ {
				bar += "█"
			}
		}
		lines = append(lines, fmt.Sprintf("  %-20s %5d tokens (%2d%%) %s",
			truncateToWidth(g.Label, 20), g.Tokens, pct, bar))
	}
	lines = append(lines, "")
	if o.maxTotal > 0 {
		pct := o.total * 100 / o.maxTotal
		lines = append(lines, fmt.Sprintf("  Total: %d / %d tokens (%d%%)", o.total, o.maxTotal, pct))
	} else {
		lines = append(lines, fmt.Sprintf("  Total: %d tokens", o.total))
	}
	lines = append(lines, "", "[Press Enter or Esc to close]")
	return lines
}

// ━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━
// OVL-41: MemoryUpdateNotification
// A status-bar notice that appears after a memory write. It is surfaced by the
// loop as a dismissible one-line overlay or system message — not a blocking
// dialog. NewMemoryUpdateNotice returns a formatted system-message string that
// callers append to screen.AppendMessage (role=RoleSystem).
// ━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━

// MemoryUpdateNotice returns the formatted notice text shown after a memory
// file is updated by the model (OVL-41). Callers append this as a system
// message; no overlay is required.
func MemoryUpdateNotice(filePath string) string {
	return "Memory updated: " + filePath
}

// ━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━
// OVL-43: UpdateAvailableNotice
// A single-line status notification shown at REPL startup if a newer version
// is available. Surfaced as a system message, not a blocking overlay.
// ━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━

// UpdateAvailableNotice returns the notice text shown when a newer version of
// ccgo is available (OVL-43). Callers show this as a system message at startup.
func UpdateAvailableNotice(currentVersion, newVersion string) string {
	return fmt.Sprintf("New version available: %s (current: %s). Run `go install` to update.",
		newVersion, currentVersion)
}

// ━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━
// OVL-44: StatusNoticesOverlay
// Shows active global notices (rate-limit, deprecation, etc.) as a dismissible
// overlay. Submit: "notices:dismiss".
// ━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━

// StatusNotice is one active status notice (rate-limit, deprecation, etc.).
type StatusNotice struct {
	Level   string // "info", "warn", "error"
	Message string
}

// StatusNoticesOverlay shows a list of active global status notices (OVL-44).
type StatusNoticesOverlay struct {
	notices []StatusNotice
}

// NewStatusNoticesOverlay constructs the overlay. notices must be non-empty;
// callers should check before opening.
func NewStatusNoticesOverlay(notices []StatusNotice) *StatusNoticesOverlay {
	copied := make([]StatusNotice, len(notices))
	copy(copied, notices)
	return &StatusNoticesOverlay{notices: copied}
}

func (o *StatusNoticesOverlay) ApplyKey(key tui.Key) (OverlayResult, bool) {
	switch key.Type {
	case tui.KeyEsc, tui.KeyEnter:
		return OverlayResult{Submit: "notices:dismiss"}, true
	default:
		return OverlayResult{}, false
	}
}

func (o *StatusNoticesOverlay) Render(width, _ int) []string {
	lines := []string{"Status notices:", ""}
	for _, n := range o.notices {
		mark := "·"
		switch n.Level {
		case "warn", "warning":
			mark = "!"
		case "error":
			mark = "✗"
		case "info":
			mark = "i"
		}
		lines = append(lines, fmt.Sprintf("  %s %s", mark, truncateToWidth(n.Message, width-4)))
	}
	lines = append(lines, "", "[Press Enter or Esc to close]")
	return lines
}

// ━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━
// OVL-50: IdleReturnDialog
// Shown after a configurable idle timeout; asks whether the user wants to
// continue or exit. Submit: "idle:continue" or "idle:exit".
// ━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━

// IdleReturnDialog is shown after the session has been idle for idleDuration
// (OVL-50). Submit: "idle:continue" or "idle:exit".
type IdleReturnDialog struct {
	idleDuration time.Duration
	cursor       int // 0 = Continue, 1 = Exit
}

// NewIdleReturnDialog constructs the idle-return overlay. idleDuration is shown
// in the dialog text.
func NewIdleReturnDialog(idleDuration time.Duration) *IdleReturnDialog {
	return &IdleReturnDialog{idleDuration: idleDuration}
}

func (d *IdleReturnDialog) ApplyKey(key tui.Key) (OverlayResult, bool) {
	switch key.Type {
	case tui.KeyEsc:
		return OverlayResult{Submit: "idle:continue"}, true
	case tui.KeyUp:
		if d.cursor > 0 {
			d.cursor--
		}
		return OverlayResult{}, true
	case tui.KeyDown:
		if d.cursor < 1 {
			d.cursor++
		}
		return OverlayResult{}, true
	case tui.KeyTab:
		d.cursor ^= 1
		return OverlayResult{}, true
	case tui.KeyEnter:
		if d.cursor == 0 {
			return OverlayResult{Submit: "idle:continue"}, true
		}
		return OverlayResult{Submit: "idle:exit"}, true
	default:
		return OverlayResult{}, false
	}
}

func (d *IdleReturnDialog) Render(width, _ int) []string {
	dur := d.idleDuration.Round(time.Second)
	lines := []string{
		fmt.Sprintf("You've been idle for %s.", dur),
		"",
		"Do you want to continue this session or exit?",
		"",
	}
	cont := "  Continue  "
	exit := "  Exit  "
	if d.cursor == 0 {
		cont = "[Continue]"
	} else {
		exit = "[Exit]"
	}
	lines = append(lines, truncateToWidth(cont+"   "+exit, width))
	return lines
}

// ━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━
// OVL-51: WorktreeExitDialog
// Shown when the user attempts to exit a git-worktree isolation session.
// Submit: "worktree:merge", "worktree:discard", "worktree:cancel".
// ━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━

// WorktreeExitDialog is shown when the user tries to exit a worktree-isolated
// REPL session (OVL-51). Options: merge branch / discard / cancel.
type WorktreeExitDialog struct {
	worktreePath string
	branch       string
	cursor       int // 0 = cancel, 1 = discard, 2 = merge
}

// NewWorktreeExitDialog constructs the worktree-exit confirmation overlay.
func NewWorktreeExitDialog(worktreePath, branch string) *WorktreeExitDialog {
	return &WorktreeExitDialog{worktreePath: worktreePath, branch: branch}
}

var worktreeOptions = []struct {
	label  string
	submit string
}{
	{"Cancel — stay in worktree session", "worktree:cancel"},
	{"Discard changes and exit", "worktree:discard"},
	{"Merge branch and exit", "worktree:merge"},
}

func (d *WorktreeExitDialog) ApplyKey(key tui.Key) (OverlayResult, bool) {
	switch key.Type {
	case tui.KeyEsc:
		return OverlayResult{Submit: "worktree:cancel"}, true
	case tui.KeyUp:
		if d.cursor > 0 {
			d.cursor--
		}
		return OverlayResult{}, true
	case tui.KeyDown:
		if d.cursor < len(worktreeOptions)-1 {
			d.cursor++
		}
		return OverlayResult{}, true
	case tui.KeyEnter:
		return OverlayResult{Submit: worktreeOptions[d.cursor].submit}, true
	default:
		return OverlayResult{}, false
	}
}

func (d *WorktreeExitDialog) Render(width, _ int) []string {
	lines := []string{
		"Exit worktree session?",
		"",
		fmt.Sprintf("Worktree: %s", d.worktreePath),
		fmt.Sprintf("Branch:   %s", d.branch),
		"",
	}
	for i, opt := range worktreeOptions {
		marker := "  "
		if i == d.cursor {
			marker = "> "
		}
		lines = append(lines, truncateToWidth(marker+opt.label, width))
	}
	return lines
}
