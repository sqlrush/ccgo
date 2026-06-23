package repl

import (
	"fmt"

	"ccgo/internal/tui"
)

// MCPServerApprovalDialog is shown when a new MCP server is found in .mcp.json
// and has not yet been approved by the user (OVL-30).
//
// CC behaviour (MCPServerApprovalDialog.tsx): three options —
//   "Use this and all future MCP servers in this project" → submit "mcp:yes_all:<name>"
//   "Use this MCP server"                                 → submit "mcp:yes:<name>"
//   "Continue without using this MCP server"             → submit "mcp:no:<name>"
//
// The loop's handleOverlaySubmit routes "mcp:" prefixed submissions to
// onOverlaySubmit (the host seam). The host is responsible for writing
// enabledMcpjsonServers / disabledMcpjsonServers to local settings.
type MCPServerApprovalDialog struct {
	serverName string
	cursor     int // 0 = yes_all, 1 = yes, 2 = no
}

// NewMCPServerApprovalDialog constructs the MCP server trust overlay for the
// given server name.
func NewMCPServerApprovalDialog(serverName string) *MCPServerApprovalDialog {
	return &MCPServerApprovalDialog{serverName: serverName}
}

var mcpOptions = []struct {
	label  string
	submit string
}{
	{"Use this and all future MCP servers in this project", "mcp:yes_all"},
	{"Use this MCP server", "mcp:yes"},
	{"Continue without using this MCP server", "mcp:no"},
}

func (d *MCPServerApprovalDialog) ApplyKey(key tui.Key) (OverlayResult, bool) {
	switch key.Type {
	case tui.KeyEsc:
		// Escape = decline (safe path).
		return OverlayResult{Submit: fmt.Sprintf("mcp:no:%s", d.serverName)}, true
	case tui.KeyUp:
		if d.cursor > 0 {
			d.cursor--
		}
		return OverlayResult{}, true
	case tui.KeyDown:
		if d.cursor < len(mcpOptions)-1 {
			d.cursor++
		}
		return OverlayResult{}, true
	case tui.KeyEnter:
		opt := mcpOptions[d.cursor]
		return OverlayResult{Submit: fmt.Sprintf("%s:%s", opt.submit, d.serverName)}, true
	default:
		return OverlayResult{}, false
	}
}

func (d *MCPServerApprovalDialog) Render(width, _ int) []string {
	lines := []string{
		fmt.Sprintf("New MCP server found in .mcp.json: %s", d.serverName),
		"",
		"MCP servers can execute code on your machine. Only enable servers",
		"you trust, from sources you control.",
		"",
	}
	for i, opt := range mcpOptions {
		marker := "  "
		if i == d.cursor {
			marker = "> "
		}
		lines = append(lines, truncateToWidth(marker+opt.label, width))
	}
	return lines
}
