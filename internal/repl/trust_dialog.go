package repl

import (
	"fmt"
	"strings"

	"ccgo/internal/tui"
)

// TrustInfo describes the configuration sources detected for a folder, shown
// in the first-run trust dialog so the user knows what they're enabling.
type TrustInfo struct {
	FolderPath      string
	HasBashRules    bool
	HasMCPServers   bool
	HasHooks        bool
	HasAPIKeyHelper bool
}

// TrustDialog is the first-run "trust this folder?" overlay.
type TrustDialog struct {
	info   TrustInfo
	cursor int // 0 = Yes, 1 = No
}

func NewTrustDialog(info TrustInfo) *TrustDialog {
	return &TrustDialog{info: info}
}

func (d *TrustDialog) ApplyKey(key tui.Key) (OverlayResult, bool) {
	switch key.Type {
	case tui.KeyEsc:
		return OverlayResult{Submit: "trust:no"}, true
	case tui.KeyUp, tui.KeyDown, tui.KeyTab:
		d.cursor ^= 1
		return OverlayResult{}, true
	case tui.KeyEnter:
		if d.cursor == 0 {
			return OverlayResult{Submit: "trust:yes"}, true
		}
		return OverlayResult{Submit: "trust:no"}, true
	default:
		return OverlayResult{}, false
	}
}

func (d *TrustDialog) Render(width, height int) []string {
	lines := []string{
		"Do you trust the files in this folder?",
		"  " + d.info.FolderPath,
		"",
	}
	for _, src := range d.detectedSources() {
		lines = append(lines, "  • "+src)
	}
	lines = append(lines, "")
	lines = append(lines, d.actionLine())
	return lines
}

func (d *TrustDialog) detectedSources() []string {
	var out []string
	if d.info.HasBashRules {
		out = append(out, "Bash permission rules")
	}
	if d.info.HasMCPServers {
		out = append(out, "MCP servers")
	}
	if d.info.HasHooks {
		out = append(out, "Hooks")
	}
	if d.info.HasAPIKeyHelper {
		out = append(out, "API key helper")
	}
	if len(out) == 0 {
		out = append(out, "No special configuration detected")
	}
	return out
}

func (d *TrustDialog) actionLine() string {
	yes, no := " Yes, trust this folder ", " No "
	if d.cursor == 0 {
		yes = "[Yes, trust this folder]"
	} else {
		no = "[No]"
	}
	return fmt.Sprintf("%s   %s", strings.TrimRight(yes, " "), strings.TrimRight(no, " "))
}
