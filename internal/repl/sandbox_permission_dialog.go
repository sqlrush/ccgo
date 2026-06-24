package repl

// OVL-45: SandboxPermissionRequest overlay.
// CC ref: src/components/permissions/SandboxPermissionRequest.tsx:15 —
// renders a "sandbox violation" dialog with violation text and Allow/Deny
// buttons.

import (
	"ccgo/internal/tui"
)

// SandboxPermissionDialog is shown when a sandbox policy violation is detected.
// The user can Allow the operation (for this session) or Deny it.
//
// Submit values:
//   - "sandbox:allow" — user chose Allow
//   - "sandbox:deny"  — user chose Deny or dismissed via Esc
type SandboxPermissionDialog struct {
	violation string // human-readable description of the violation
	cursor    int    // 0 = Allow, 1 = Deny
}

// NewSandboxPermissionDialog constructs a SandboxPermissionDialog for the given
// violation description.
func NewSandboxPermissionDialog(violation string) *SandboxPermissionDialog {
	return &SandboxPermissionDialog{violation: violation}
}

// ApplyKey implements Overlay.
func (d *SandboxPermissionDialog) ApplyKey(key tui.Key) (OverlayResult, bool) {
	switch key.Type {
	case tui.KeyEsc:
		// Dismiss → deny (safe default).
		return OverlayResult{Submit: "sandbox:deny"}, true
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
			return OverlayResult{Submit: "sandbox:allow"}, true
		}
		return OverlayResult{Submit: "sandbox:deny"}, true
	default:
		return OverlayResult{}, false
	}
}

// Render implements Overlay.
func (d *SandboxPermissionDialog) Render(width, _ int) []string {
	lines := []string{
		"Sandbox Violation",
		"",
	}
	// Word-wrap the violation text.
	lines = append(lines, wordWrap(d.violation, width)...)
	lines = append(lines, "")

	allow := "  Allow  "
	deny := "  Deny  "
	if d.cursor == 0 {
		allow = "[Allow]"
	} else {
		deny = "[Deny]"
	}
	lines = append(lines, truncateToWidth(allow+"   "+deny, width))
	return lines
}
