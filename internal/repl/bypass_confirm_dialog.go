package repl

import (
	"strings"

	"ccgo/internal/tui"
)

// BypassConfirmDialog is the security confirmation overlay that the user must
// accept before the REPL switches to BypassPermissions mode (OVL-31).
//
// CC behaviour: BypassPermissionsModeDialog — selecting "No, exit" shuts down
// the process; in ccgo we instead cancel the mode switch (keep current mode).
// The caller is responsible for not flipping the mode until Submit="bypass:accept".
type BypassConfirmDialog struct {
	cursor int // 0 = "No, go back" (safe default), 1 = "Yes, I accept"
}

// NewBypassConfirmDialog constructs the bypass-permissions confirmation overlay.
func NewBypassConfirmDialog() *BypassConfirmDialog {
	return &BypassConfirmDialog{}
}

func (d *BypassConfirmDialog) ApplyKey(key tui.Key) (OverlayResult, bool) {
	switch key.Type {
	case tui.KeyEsc:
		// Escape = decline (safe path — keep current mode).
		return OverlayResult{Submit: "bypass:decline"}, true
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
		if d.cursor == 1 {
			return OverlayResult{Submit: "bypass:accept"}, true
		}
		return OverlayResult{Submit: "bypass:decline"}, true
	default:
		return OverlayResult{}, false
	}
}

func (d *BypassConfirmDialog) Render(width, _ int) []string {
	lines := []string{
		"WARNING: Bypass Permissions mode",
		"",
		"In Bypass Permissions mode, Claude Code will not ask for your",
		"approval before running potentially dangerous commands.",
		"This mode should only be used in a sandboxed container/VM",
		"with restricted internet access that can be restored if damaged.",
		"",
		"By proceeding, you accept all responsibility for actions taken",
		"while running in Bypass Permissions mode.",
		"",
	}
	no := "  No, go back  "
	yes := "  Yes, I accept  "
	if d.cursor == 0 {
		no = "[No, go back]"
	} else {
		yes = "[Yes, I accept]"
	}
	lines = append(lines, truncateToWidth(no+"   "+yes, width))
	return lines
}

// autoModeDescription mirrors CC's AUTO_MODE_DESCRIPTION (legally reviewed copy).
const autoModeDescription = "Auto mode lets Claude handle permission prompts automatically — " +
	"Claude checks each tool call for risky actions and prompt injection before executing. " +
	"Actions Claude identifies as safe are executed, while risky actions are blocked and " +
	"Claude may try a different approach. Ideal for long-running tasks. Sessions are slightly " +
	"more expensive. Claude can make mistakes that allow harmful commands to run; " +
	"recommended for isolated environments only. Shift+Tab to change mode."

// AutoModeOptInDialog is shown before the REPL switches to auto mode (OVL-32).
// Options: "Yes, enable auto mode" / "No, go back".
// Submit values: "auto:accept" or "auto:decline".
type AutoModeOptInDialog struct {
	cursor int // 0 = "Yes, enable auto mode", 1 = "No, go back"
}

// NewAutoModeOptInDialog constructs the auto-mode opt-in overlay.
func NewAutoModeOptInDialog() *AutoModeOptInDialog {
	return &AutoModeOptInDialog{}
}

func (d *AutoModeOptInDialog) ApplyKey(key tui.Key) (OverlayResult, bool) {
	switch key.Type {
	case tui.KeyEsc:
		return OverlayResult{Submit: "auto:decline"}, true
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
			return OverlayResult{Submit: "auto:accept"}, true
		}
		return OverlayResult{Submit: "auto:decline"}, true
	default:
		return OverlayResult{}, false
	}
}

func (d *AutoModeOptInDialog) Render(width, _ int) []string {
	// Word-wrap the description into width-bounded lines.
	lines := []string{
		"Enable auto mode?",
		"",
	}
	lines = append(lines, wordWrap(autoModeDescription, width)...)
	lines = append(lines, "")

	yes := "  Yes, enable auto mode  "
	no := "  No, go back  "
	if d.cursor == 0 {
		yes = "[Yes, enable auto mode]"
	} else {
		no = "[No, go back]"
	}
	lines = append(lines, truncateToWidth(yes+"   "+no, width))
	return lines
}

// wordWrap breaks s into lines of at most maxWidth visible characters.
func wordWrap(s string, maxWidth int) []string {
	if maxWidth <= 0 {
		return []string{s}
	}
	var out []string
	words := strings.Fields(s)
	var current strings.Builder
	for _, w := range words {
		if current.Len() == 0 {
			current.WriteString(w)
		} else if current.Len()+1+len(w) <= maxWidth {
			current.WriteByte(' ')
			current.WriteString(w)
		} else {
			out = append(out, current.String())
			current.Reset()
			current.WriteString(w)
		}
	}
	if current.Len() > 0 {
		out = append(out, current.String())
	}
	return out
}
