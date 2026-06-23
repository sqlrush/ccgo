package repl

// W-C05: tests that Shift+Tab mode-switch and allow-always persist are
// reflected in the live EnginePermissionDecider used by StartTurn.
//
// RED phase: tests that require l.onModeChange and engine-pointer sync fail
// before the fix. TestModeSwitchReachesEngine is a pure engine-layer sanity
// check that must pass before and after.

import (
	"context"
	"testing"
	"time"

	"ccgo/internal/contracts"
	"ccgo/internal/conversation"
	"ccgo/internal/permissions"
	"ccgo/internal/tool"
	"ccgo/internal/tui"
)

// TestModeSwitchReachesEngine is a pure permissions.Engine sanity check:
// ApplyUpdate("setMode") changes decisions correctly. This must pass before
// and after the fix.
func TestModeSwitchReachesEngine(t *testing.T) {
	eng := permissions.NewEngine(contracts.PermissionContext{
		Mode:            contracts.PermissionDefault,
		BypassAvailable: true,
		AutoAvailable:   true,
	})

	// Baseline: default mode, Edit (WritesFiles) → Ask.
	decBase := eng.Decide(permissions.Request{ToolName: "Edit", WritesFiles: true})
	if decBase.Behavior != contracts.PermissionAsk {
		t.Fatalf("baseline default mode: want Ask for Edit, got %s", decBase.Behavior)
	}

	// After acceptEdits mode update: Edit → Allow.
	ae, err := eng.ApplyUpdate(contracts.PermissionUpdate{Type: "setMode", Mode: contracts.PermissionAcceptEdits})
	if err != nil {
		t.Fatalf("ApplyUpdate acceptEdits: %v", err)
	}
	decAE := ae.Decide(permissions.Request{ToolName: "Edit", WritesFiles: true})
	if decAE.Behavior != contracts.PermissionAllow {
		t.Fatalf("acceptEdits mode: want Allow for Edit, got %s", decAE.Behavior)
	}

	// After bypassPermissions mode update: any write tool → Allow.
	bp, err := eng.ApplyUpdate(contracts.PermissionUpdate{Type: "setMode", Mode: contracts.PermissionBypassPermissions})
	if err != nil {
		t.Fatalf("ApplyUpdate bypass: %v", err)
	}
	decBP := bp.Decide(permissions.Request{ToolName: "Bash"})
	if decBP.Behavior != contracts.PermissionAllow {
		t.Fatalf("bypassPermissions mode: want Allow for Bash, got %s", decBP.Behavior)
	}
}

// TestModeSwitchPropagatesViaLoop is the integration test: Loop.handleKey
// for Shift+Tab must fire l.onModeChange with the new mode so callers can
// update the engine pointer.
func TestModeSwitchPropagatesViaLoop(t *testing.T) {
	eng := permissions.NewEngine(contracts.PermissionContext{
		Mode:            contracts.PermissionDefault,
		BypassAvailable: true,
		AutoAvailable:   true,
	})
	engPtr := &eng

	ft := NewFakeTerminal("", 80, 24)
	l := NewLoop(ft, nil)
	l.SetMode(contracts.PermissionDefault)

	// Wire onModeChange: must be set by the fix.
	l.onModeChange = func(mode contracts.PermissionMode) {
		next, err := engPtr.ApplyUpdate(contracts.PermissionUpdate{
			Type: "setMode",
			Mode: mode,
		})
		if err == nil {
			*engPtr = next
		}
	}

	// Fire Shift+Tab: cycleMode(default) → acceptEdits.
	l.handleKey(tui.Key{Type: tui.KeyShiftTab})

	if l.mode != contracts.PermissionAcceptEdits {
		t.Fatalf("loop.mode = %s; want acceptEdits", l.mode)
	}
	if engPtr.Mode() != contracts.PermissionAcceptEdits {
		t.Fatalf("engine.Mode() = %s; want acceptEdits", engPtr.Mode())
	}

	dec := engPtr.Decide(permissions.Request{ToolName: "Edit", WritesFiles: true})
	if dec.Behavior != contracts.PermissionAllow {
		t.Fatalf("engine decision after mode sync: got %s want Allow", dec.Behavior)
	}
}

// TestPersistDecisionRefreshesEngine asserts that persistDecision calls
// onRulePersisted so the engine pointer gets updated with the new allow rule.
func TestPersistDecisionRefreshesEngine(t *testing.T) {
	eng := permissions.NewEngine(contracts.PermissionContext{
		Mode:            contracts.PermissionDefault,
		BypassAvailable: true,
		AutoAvailable:   true,
	})
	engPtr := &eng

	decBefore := engPtr.Decide(permissions.Request{ToolName: "Bash", Command: "npm run build"})
	if decBefore.Behavior != contracts.PermissionAsk {
		t.Fatalf("baseline: want Ask for Bash, got %s", decBefore.Behavior)
	}

	ft := NewFakeTerminal("", 80, 24)
	l := NewLoop(ft, nil)

	// Wire onRulePersisted to refresh engine (the fix also wires this in RunInteractiveWithOptions).
	l.onRulePersisted = func(update contracts.PermissionUpdate) {
		next, err := engPtr.ApplyUpdate(update)
		if err == nil {
			*engPtr = next
		}
	}

	var applied []contracts.PermissionUpdate
	l.SetSettingsWriter(recordingWriter{onApply: func(u contracts.PermissionUpdate) error {
		applied = append(applied, u)
		return nil
	}})

	update := contracts.PermissionUpdate{
		Type:        "addRules",
		Behavior:    contracts.PermissionAllow,
		Destination: string(contracts.PermissionSourceLocalSettings),
		Rules:       []contracts.PermissionRuleValue{{ToolName: "Bash", RuleContent: "npm run build"}},
	}
	l.persistDecision(contracts.PermissionDecision{
		Behavior:    contracts.PermissionAllow,
		Suggestions: []contracts.PermissionUpdate{update},
	})

	if len(applied) != 1 {
		t.Fatalf("expected 1 persisted rule, got %d", len(applied))
	}

	decAfter := engPtr.Decide(permissions.Request{ToolName: "Bash", Command: "npm run build"})
	if decAfter.Behavior != contracts.PermissionAllow {
		t.Fatalf("after persist: want Allow for Bash(npm run build), got %s", decAfter.Behavior)
	}
}

// TestRunInteractiveWithOptionsWiresEngineSync is the end-to-end test:
// RunInteractiveWithOptions receives an Engine pointer; Shift+Tab cycles the
// mode; the engine pointer is updated to acceptEdits; a decision for a
// file-write tool is Allow (not Ask).
func TestRunInteractiveWithOptionsWiresEngineSync(t *testing.T) {
	eng := permissions.NewEngine(contracts.PermissionContext{
		Mode:            contracts.PermissionDefault,
		BypassAvailable: true,
		AutoAvailable:   true,
	})

	// Shift+Tab escape sequence followed by EOF.
	shiftTabESC := "\x1b[Z"
	ft := NewFakeTerminal(shiftTabESC, 80, 24)

	base := conversation.Runner{
		Client:      fakeClient{},
		Model:       "claude-test",
		MaxTokens:   256,
		Permissions: tool.NewEnginePermissionDecider(eng),
	}

	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()

	if err := RunInteractiveWithOptions(ctx, ft, base, nil, InteractiveOptions{
		Engine: &eng,
		Mode:   contracts.PermissionDefault,
	}); err != nil {
		t.Fatalf("RunInteractiveWithOptions: %v", err)
	}

	// Engine must now be in acceptEdits mode.
	if eng.Mode() != contracts.PermissionAcceptEdits {
		t.Fatalf("engine.Mode() = %s after Shift+Tab; want acceptEdits", eng.Mode())
	}

	dec := eng.Decide(permissions.Request{ToolName: "Edit", WritesFiles: true})
	if dec.Behavior != contracts.PermissionAllow {
		t.Fatalf("engine decision post wire: got %s want Allow", dec.Behavior)
	}
}
