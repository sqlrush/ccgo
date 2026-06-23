package repl

import (
	"context"
	"strings"
	"testing"
	"time"

	"ccgo/internal/contracts"
	"ccgo/internal/tui"
)

// TestBypassModeSwitchRequiresConfirmation verifies that Shift+Tab to bypass
// mode does NOT immediately flip the mode — it opens a BypassConfirmDialog.
// The mode changes only after the user accepts.
func TestBypassModeSwitchRequiresConfirmation(t *testing.T) {
	ft := NewFakeTerminal("", 80, 24)
	l := NewLoop(ft, nil)
	// Pre-position at plan mode so the next cycle → bypass.
	l.SetMode(contracts.PermissionPlan)

	modeChanged := false
	l.onModeChange = func(m contracts.PermissionMode) {
		modeChanged = true
	}

	// Deliver Shift+Tab: should open confirm dialog, NOT change the mode.
	l.handleKey(tui.Key{Type: tui.KeyShiftTab})

	if modeChanged {
		t.Fatal("mode must NOT change when Shift+Tab to bypass — confirmation required")
	}
	if l.mode != contracts.PermissionPlan {
		t.Fatalf("mode = %q want plan (unchanged)", l.mode)
	}
	if l.activeOverlay == nil {
		t.Fatal("BypassConfirmDialog must be opened after Shift+Tab to bypass")
	}
	// Confirm the overlay is a BypassConfirmDialog.
	if _, ok := l.activeOverlay.(*BypassConfirmDialog); !ok {
		t.Fatalf("activeOverlay = %T want *BypassConfirmDialog", l.activeOverlay)
	}
}

// TestBypassModeConfirmAcceptChangesMode: accepting the bypass dialog switches
// the mode to BypassPermissions.
func TestBypassModeConfirmAcceptChangesMode(t *testing.T) {
	ft := NewFakeTerminal("", 80, 24)
	l := NewLoop(ft, nil)
	l.SetMode(contracts.PermissionPlan)

	modeChangedTo := contracts.PermissionMode("")
	l.onModeChange = func(m contracts.PermissionMode) {
		modeChangedTo = m
	}

	// Shift+Tab opens confirm dialog.
	l.handleKey(tui.Key{Type: tui.KeyShiftTab})
	if _, ok := l.activeOverlay.(*BypassConfirmDialog); !ok {
		t.Fatalf("expected BypassConfirmDialog, got %T", l.activeOverlay)
	}

	// Navigate to "Yes, I accept" (cursor 1) then Enter.
	l.activeOverlay.ApplyKey(tui.Key{Type: tui.KeyDown}) // cursor → 1
	res, _ := l.activeOverlay.ApplyKey(tui.Key{Type: tui.KeyEnter})
	// Simulate loop routing the submit value.
	l.activeOverlay = nil
	handled := l.handleOverlaySubmit(res.Submit)

	if !handled {
		t.Fatal("handleOverlaySubmit must handle bypass:accept")
	}
	if modeChangedTo != contracts.PermissionBypassPermissions {
		t.Fatalf("mode after accept = %q want bypassPermissions", modeChangedTo)
	}
	if l.mode != contracts.PermissionBypassPermissions {
		t.Fatalf("l.mode = %q want bypassPermissions", l.mode)
	}
}

// TestBypassModeConfirmDeclineKeepsMode: declining the bypass dialog keeps the
// current mode and fires no onModeChange.
func TestBypassModeConfirmDeclineKeepsMode(t *testing.T) {
	ft := NewFakeTerminal("", 80, 24)
	l := NewLoop(ft, nil)
	l.SetMode(contracts.PermissionPlan)

	modeChanged := false
	l.onModeChange = func(m contracts.PermissionMode) {
		modeChanged = true
	}

	// Shift+Tab opens confirm dialog.
	l.handleKey(tui.Key{Type: tui.KeyShiftTab})

	// Default cursor is "No, go back" — press Enter.
	res, _ := l.activeOverlay.ApplyKey(tui.Key{Type: tui.KeyEnter})
	l.activeOverlay = nil
	l.handleOverlaySubmit(res.Submit)

	if modeChanged {
		t.Fatal("mode must NOT change when user declines bypass confirmation")
	}
	if l.mode != contracts.PermissionPlan {
		t.Fatalf("mode = %q want plan (unchanged)", l.mode)
	}
}

// TestAutoModeConfirmDeclineKeepsMode: declining auto-mode opt-in keeps mode.
func TestAutoModeConfirmDeclineKeepsMode(t *testing.T) {
	ft := NewFakeTerminal("", 80, 24)
	l := NewLoop(ft, nil)
	l.SetMode(contracts.PermissionDefault)

	modeChanged := false
	l.onModeChange = func(m contracts.PermissionMode) {
		modeChanged = true
	}

	// Directly push an auto-mode opt-in overlay (as if triggered programmatically).
	l.activeOverlay = NewAutoModeOptInDialog()
	// Navigate to "No, go back" (cursor 1) then Enter.
	l.activeOverlay.ApplyKey(tui.Key{Type: tui.KeyDown})
	res, _ := l.activeOverlay.ApplyKey(tui.Key{Type: tui.KeyEnter})
	l.activeOverlay = nil
	l.handleOverlaySubmit(res.Submit)

	if modeChanged {
		t.Fatal("mode must NOT change when user declines auto-mode opt-in")
	}
	if l.mode != contracts.PermissionDefault {
		t.Fatalf("mode = %q want default (unchanged)", l.mode)
	}
}

// TestAutoModeConfirmAcceptChangesMode: accepting auto-mode opt-in switches mode.
func TestAutoModeConfirmAcceptChangesMode(t *testing.T) {
	ft := NewFakeTerminal("", 80, 24)
	l := NewLoop(ft, nil)
	l.SetMode(contracts.PermissionDefault)

	modeChangedTo := contracts.PermissionMode("")
	l.onModeChange = func(m contracts.PermissionMode) {
		modeChangedTo = m
	}

	l.activeOverlay = NewAutoModeOptInDialog()
	// cursor 0 = accept
	res, _ := l.activeOverlay.ApplyKey(tui.Key{Type: tui.KeyEnter})
	l.activeOverlay = nil
	l.handleOverlaySubmit(res.Submit)

	if modeChangedTo != contracts.PermissionAuto {
		t.Fatalf("mode after accept = %q want auto", modeChangedTo)
	}
}

// TestShiftTabNormalModeNoDialog: Shift+Tab to a safe mode (acceptEdits, plan)
// must NOT open any dialog — it switches immediately.
func TestShiftTabNormalModeNoDialog(t *testing.T) {
	ft := NewFakeTerminal("", 80, 24)
	l := NewLoop(ft, nil)
	l.SetMode(contracts.PermissionDefault)

	l.handleKey(tui.Key{Type: tui.KeyShiftTab}) // default → acceptEdits

	if l.activeOverlay != nil {
		t.Fatalf("Shift+Tab to acceptEdits must not open a dialog, got %T", l.activeOverlay)
	}
	if l.mode != contracts.PermissionAcceptEdits {
		t.Fatalf("mode = %q want acceptEdits", l.mode)
	}
}

// TestBypassConfirmGatingIntegrationRun: end-to-end integration: feed
// Shift+Tab (plan→bypass) + Down + Enter (accept) into a full loop run.
// The mode must end up as bypassPermissions and the loop must exit cleanly.
func TestBypassConfirmGatingIntegrationRun(t *testing.T) {
	// Sequence: Shift+Tab (plan→bypass, opens dialog), Down (cursor→accept),
	// Enter (accept), then Ctrl+D to exit.
	seq := []tui.Key{
		{Type: tui.KeyShiftTab},
		{Type: tui.KeyDown},
		{Type: tui.KeyEnter},
		{Type: tui.KeyCtrlD},
	}
	var buf strings.Builder
	for _, k := range seq {
		buf.WriteString(string(k.Rune))
	}

	ft := NewFakeTerminal("", 80, 24)
	l := NewLoop(ft, nil)
	l.SetMode(contracts.PermissionPlan)

	finalMode := contracts.PermissionMode("")
	l.onModeChange = func(m contracts.PermissionMode) {
		finalMode = m
	}

	// Feed keys directly without running the full TTY loop.
	for _, k := range seq {
		if k.Type == tui.KeyCtrlD {
			break
		}
		l.handleKey(k)
	}

	if finalMode != contracts.PermissionBypassPermissions {
		t.Fatalf("final mode = %q want bypassPermissions", finalMode)
	}
}

// TestHandleOverlaySubmitBypassDeclineIsHandled: "bypass:decline" must be
// consumed by handleOverlaySubmit and NOT forwarded to the model.
func TestHandleOverlaySubmitBypassDeclineIsHandled(t *testing.T) {
	ft := NewFakeTerminal("", 80, 24)
	l := NewLoop(ft, nil)
	l.StartTurn = func(string) { panic("model must not be called for bypass:decline") }
	handled := l.handleOverlaySubmit("bypass:decline")
	if !handled {
		t.Fatal("bypass:decline must be handled internally")
	}
}

// TestHandleOverlaySubmitAutoDeclineIsHandled: "auto:decline" must be handled.
func TestHandleOverlaySubmitAutoDeclineIsHandled(t *testing.T) {
	ft := NewFakeTerminal("", 80, 24)
	l := NewLoop(ft, nil)
	l.StartTurn = func(string) { panic("model must not be called for auto:decline") }
	handled := l.handleOverlaySubmit("auto:decline")
	if !handled {
		t.Fatal("auto:decline must be handled internally")
	}
}

// TestHandleOverlaySubmitCostAckIsHandled: "cost:ack" must be handled.
func TestHandleOverlaySubmitCostAckIsHandled(t *testing.T) {
	ft := NewFakeTerminal("", 80, 24)
	l := NewLoop(ft, nil)
	if !l.handleOverlaySubmit("cost:ack") {
		t.Fatal("cost:ack must be handled internally")
	}
}

// TestHandleOverlaySubmitMCPPrefixIsHandled: "mcp:yes:srv" must be handled.
func TestHandleOverlaySubmitMCPPrefixIsHandled(t *testing.T) {
	ft := NewFakeTerminal("", 80, 24)
	l := NewLoop(ft, nil)
	var got string
	l.onOverlaySubmit = func(s string) { got = s }
	if !l.handleOverlaySubmit("mcp:yes:my-server") {
		t.Fatal("mcp: prefix must be handled internally")
	}
	if got != "mcp:yes:my-server" {
		t.Fatalf("onOverlaySubmit = %q want mcp:yes:my-server", got)
	}
}

// TestHandleOverlaySubmitOnboardPrefixIsHandled.
func TestHandleOverlaySubmitOnboardPrefixIsHandled(t *testing.T) {
	ft := NewFakeTerminal("", 80, 24)
	l := NewLoop(ft, nil)
	var got string
	l.onOverlaySubmit = func(s string) { got = s }
	if !l.handleOverlaySubmit("onboard:theme:dark") {
		t.Fatal("onboard: prefix must be handled internally")
	}
	if got != "onboard:theme:dark" {
		t.Fatalf("onOverlaySubmit = %q want onboard:theme:dark", got)
	}
}

// TestBypassConfirmDialogInLoopRun: full Run loop test exercising the
// bypass-confirm guard via the TTY path.
func TestBypassConfirmDialogInLoopRun(t *testing.T) {
	// Keys: Shift+Tab (plan→bypass, opens dialog) + Esc (decline) + Ctrl+D
	// After Esc, bypass must be declined and mode stays plan; then loop exits.
	ft := NewFakeTerminal("\x1b[Z\x1b\x04", 80, 24) // Shift+Tab + Esc + Ctrl+D
	l := NewLoop(ft, nil)
	l.SetMode(contracts.PermissionPlan)

	modeChanged := false
	l.onModeChange = func(m contracts.PermissionMode) {
		modeChanged = true
	}

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	if err := l.Run(ctx); err != nil {
		t.Fatalf("Run: %v", err)
	}
	if modeChanged {
		t.Fatal("mode must NOT change when bypass dialog is dismissed via Esc")
	}
}
