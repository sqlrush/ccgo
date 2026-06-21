package repl

import (
	"strings"

	"ccgo/internal/contracts"
	"ccgo/internal/tui"
)

// cycleMode advances the permission mode in the same order CC's Shift+Tab uses:
// default → acceptEdits → plan → bypassPermissions → default.
func cycleMode(cur contracts.PermissionMode) contracts.PermissionMode {
	switch cur {
	case contracts.PermissionDefault:
		return contracts.PermissionAcceptEdits
	case contracts.PermissionAcceptEdits:
		return contracts.PermissionPlan
	case contracts.PermissionPlan:
		return contracts.PermissionBypassPermissions
	default:
		return contracts.PermissionDefault
	}
}

// modeIndicator is the status-bar fragment for the current mode + vim state.
// Default mode with no vim insert returns "" (no clutter), matching CC.
func modeIndicator(mode contracts.PermissionMode, vimEnabled bool, vimMode tui.VimMode) string {
	var parts []string
	switch mode {
	case contracts.PermissionAcceptEdits:
		parts = append(parts, "accept edits")
	case contracts.PermissionPlan:
		parts = append(parts, "plan mode")
	case contracts.PermissionBypassPermissions:
		parts = append(parts, "bypass permissions")
	}
	if vimEnabled && vimMode == tui.VimInsert {
		parts = append(parts, "-- INSERT --")
	}
	return strings.Join(parts, " · ")
}
