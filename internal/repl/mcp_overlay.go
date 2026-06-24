package repl

// G24: /mcp interactive overlay.
//
// MCPOverlay wraps live mcp.Manager status in an Overlay. The user can:
//   - ↑/↓ navigate the server list
//   - 'e' → enable the selected server (Manager.SetEnabled(true))
//   - 'd' → disable the selected server (Manager.SetEnabled(false))
//   - 'r' → reconnect the selected server (Manager.Reconnect)
//   - Esc → dismiss
//
// All three actions call the Manager synchronously (which is concurrency-safe)
// and return a structured submit token ("mcp:<action>:<name>") so the loop's
// handleOverlaySubmit can route them.
//
// CC ref: src/commands/mcp/mcp.tsx:83 (MCPSettings panel with server actions).

import (
	"context"
	"fmt"
	"strings"

	"ccgo/internal/mcp"
	"ccgo/internal/tui"
)

// MCPOverlay is the interactive /mcp panel overlay.
type MCPOverlay struct {
	mgr      *mcp.Manager
	cursor   int
	// snapshot holds the server list at the time of the last key event.
	// Refreshed from mgr on each key dispatch so actions see current state.
	snapshot []mcp.ServerStatus
}

// newMCPOverlay creates an MCPOverlay backed by the given Manager.
// mgr may be nil (overlay renders a "no manager" message).
func newMCPOverlay(mgr *mcp.Manager) *MCPOverlay {
	ov := &MCPOverlay{mgr: mgr}
	ov.refresh()
	return ov
}

// refresh updates the internal snapshot from the live Manager.
func (o *MCPOverlay) refresh() {
	if o.mgr == nil {
		o.snapshot = nil
		return
	}
	o.snapshot = o.mgr.Status()
	// Clamp cursor.
	if o.cursor >= len(o.snapshot) {
		o.cursor = max(0, len(o.snapshot)-1)
	}
}

// Cursor returns the current cursor position.
func (o *MCPOverlay) Cursor() int { return o.cursor }

// ApplyKey processes a key event. Implements Overlay.
func (o *MCPOverlay) ApplyKey(key tui.Key) (OverlayResult, bool) {
	o.refresh()

	switch key.Type {
	case tui.KeyEsc:
		return OverlayResult{Dismissed: true}, true

	case tui.KeyDown:
		if o.cursor < len(o.snapshot)-1 {
			o.cursor++
		}
		return OverlayResult{}, true

	case tui.KeyUp:
		if o.cursor > 0 {
			o.cursor--
		}
		return OverlayResult{}, true

	case tui.KeyRune:
		switch key.Rune {
		case 'd':
			return o.dispatchAction("disable")
		case 'e':
			return o.dispatchAction("enable")
		case 'r':
			return o.dispatchAction("reconnect")
		}
	}
	return OverlayResult{}, false
}

// dispatchAction performs the named action on the currently selected server.
func (o *MCPOverlay) dispatchAction(action string) (OverlayResult, bool) {
	if len(o.snapshot) == 0 || o.mgr == nil {
		return OverlayResult{}, true
	}
	name := o.snapshot[o.cursor].Name
	ctx := context.Background()
	switch action {
	case "disable":
		_ = o.mgr.SetEnabled(ctx, name, false)
	case "enable":
		_ = o.mgr.SetEnabled(ctx, name, true)
	case "reconnect":
		_ = o.mgr.Reconnect(ctx, name)
	}
	o.refresh()
	return OverlayResult{Submit: fmt.Sprintf("mcp:%s:%s", action, name)}, true
}

// Render returns display lines for the overlay. Implements Overlay.
func (o *MCPOverlay) Render(width, height int) []string {
	if o.mgr == nil {
		return []string{"MCP: no connection manager active.", "(Esc to close)"}
	}
	lines := []string{"MCP Servers (↑↓ navigate, e=enable d=disable r=reconnect Esc=close)"}
	for i, s := range o.snapshot {
		marker := "  "
		if i == o.cursor {
			marker = "> "
		}
		status := s.Status
		if s.Error != "" {
			status += ": " + s.Error
		}
		line := fmt.Sprintf("%s%-20s [%s]", marker, s.Name, status)
		if width > 0 && len(line) > width {
			line = line[:width]
		}
		lines = append(lines, line)
	}
	if len(o.snapshot) == 0 {
		lines = append(lines, "  (no servers configured)")
	}
	if height > 0 && len(lines) > height {
		lines = lines[:height]
	}
	return lines
}

// mcpOverlayHandlerWith returns a CommandHandler for /mcp that opens the
// MCPOverlay when a Manager is available, otherwise falls back to text.
//
// CMD-MCP-01 (G24): interactive overlay wired to live Manager.
// CC ref: src/commands/mcp/index.ts (enable|disable subcommands).
func mcpOverlayHandlerWith(mgr *mcp.Manager) CommandHandler {
	return func(ctx context.Context, cc CommandContext) (CommandOutcome, error) {
		if mgr == nil {
			return CommandOutcome{
				Handled: true,
				Status:  "MCP: no connection manager is active in this session.",
			}, nil
		}
		return CommandOutcome{
			Handled: true,
			Overlay: newMCPOverlay(mgr),
		}, nil
	}
}

// max is a local integer maximum helper.
func max(a, b int) int {
	if a > b {
		return a
	}
	return b
}

// Ensure unused import is satisfied.
var _ = strings.Contains
