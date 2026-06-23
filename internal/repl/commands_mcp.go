package repl

import (
	"context"
	"strings"

	"ccgo/internal/mcp"
)

// mcpHandlerWith returns a CommandHandler for /mcp that renders live connection
// status from the Manager. When mgr is nil it returns a graceful fallback.
// CC ref: src/commands/mcp/mcp.tsx:83 (MCPSettings panel) — G11 live wiring.
func mcpHandlerWith(mgr *mcp.Manager) CommandHandler {
	return func(ctx context.Context, cc CommandContext) (CommandOutcome, error) {
		if mgr == nil {
			return CommandOutcome{
				Handled: true,
				Status:  "MCP: no connection manager is active in this session.",
			}, nil
		}
		entries := mcpServerEntriesFromManager(mgr)
		return CommandOutcome{
			Handled: true,
			Status:  mcpStatusPanel(entries),
		}, nil
	}
}

// mcpServerEntriesFromManager converts Manager.Status() into the MCPServerEntry
// slice used by mcpStatusPanel. Returns an immutable slice (all value types).
func mcpServerEntriesFromManager(mgr *mcp.Manager) []MCPServerEntry {
	if mgr == nil {
		return nil
	}
	live := mgr.Status()
	out := make([]MCPServerEntry, 0, len(live))
	for _, s := range live {
		out = append(out, MCPServerEntry{
			Name:   s.Name,
			Status: s.Status,
			Error:  s.Error,
			// Transport and Target are not available from the Manager alone
			// (they require the full MCPConfig). We omit them here; the panel
			// renders gracefully with empty transport/target fields.
		})
	}
	return out
}

// mcpHandlerWithPanel is like mcpHandlerWith but also provides transport/target
// metadata from a server config lookup map.
// Exported for use by callers that have both a Manager and config.
func mcpHandlerWithPanel(mgr *mcp.Manager, serverTransport func(name string) (transport, target string)) CommandHandler {
	return func(ctx context.Context, cc CommandContext) (CommandOutcome, error) {
		if mgr == nil {
			return CommandOutcome{
				Handled: true,
				Status:  "MCP: no connection manager is active in this session.",
			}, nil
		}
		live := mgr.Status()
		entries := make([]MCPServerEntry, 0, len(live))
		for _, s := range live {
			transport := ""
			target := ""
			if serverTransport != nil {
				transport, target = serverTransport(s.Name)
			}
			entries = append(entries, MCPServerEntry{
				Name:      s.Name,
				Transport: transport,
				Target:    target,
				Status:    s.Status,
				Error:     s.Error,
			})
		}
		return CommandOutcome{
			Handled: true,
			Status:  mcpStatusPanel(entries),
		}, nil
	}
}

// mcpStatusPanelFromManager renders the /mcp panel using live Manager status.
// Used by the REPL loop when a Manager is available.
func mcpStatusPanelFromManager(mgr *mcp.Manager) string {
	return strings.TrimRight(mcpStatusPanel(mcpServerEntriesFromManager(mgr)), "\n")
}
