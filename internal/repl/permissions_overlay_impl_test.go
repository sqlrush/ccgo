package repl

// G24: /permissions interactive overlay wiring tests.
// Tests that the PermissionsOverlay (Overlay impl) exposes the state transitions
// defined in PermissionsOverlayState, and that the permissions handler returns
// an overlay (not just text) when invoked with no args in interactive mode.
//
// PERM-PERSIST-06: state layer already exists (permissions_overlay.go);
// this file tests the Overlay implementation that wraps that state.

import (
	"context"
	"testing"

	"ccgo/internal/tui"
)

func TestPermissionsOverlayImplInitialTabIsRules(t *testing.T) {
	ov := newPermissionsOverlayImpl(permissionsOverlayOptions{
		AllowRules: []string{"Bash(git:*)"},
		DenyRules:  []string{"Bash(rm:*)"},
	})
	if ov.State().ActiveTab != 0 {
		t.Fatalf("initial tab = %d want 0 (Rules)", ov.State().ActiveTab)
	}
}

func TestPermissionsOverlayImplTabSwitchNextPrev(t *testing.T) {
	ov := newPermissionsOverlayImpl(permissionsOverlayOptions{})
	// Tab → next tab
	res, handled := ov.ApplyKey(tui.Key{Type: tui.KeyTab})
	if !handled {
		t.Fatal("Tab should be handled")
	}
	if res.Dismissed || res.Submit != "" {
		t.Fatalf("Tab should not dismiss/submit: %+v", res)
	}
	if ov.State().ActiveTab != 1 {
		t.Fatalf("after Tab: ActiveTab = %d want 1", ov.State().ActiveTab)
	}
	// Shift+Tab back to tab 0
	ov.ApplyKey(tui.Key{Type: tui.KeyShiftTab})
	if ov.State().ActiveTab != 0 {
		t.Fatalf("after Shift+Tab: ActiveTab = %d want 0", ov.State().ActiveTab)
	}
}

func TestPermissionsOverlayImplEscDismisses(t *testing.T) {
	ov := newPermissionsOverlayImpl(permissionsOverlayOptions{})
	res, handled := ov.ApplyKey(tui.Key{Type: tui.KeyEsc})
	if !handled {
		t.Fatal("Esc should be handled")
	}
	if !res.Dismissed {
		t.Fatalf("Esc should dismiss, got %+v", res)
	}
}

func TestPermissionsOverlayImplRenderNonEmpty(t *testing.T) {
	ov := newPermissionsOverlayImpl(permissionsOverlayOptions{
		AllowRules: []string{"Bash(git:*)"},
	})
	lines := ov.Render(80, 24)
	if len(lines) == 0 {
		t.Fatal("Render should return non-empty lines")
	}
}

func TestPermissionsOverlayImplRenderShowsTabName(t *testing.T) {
	ov := newPermissionsOverlayImpl(permissionsOverlayOptions{})
	lines := ov.Render(80, 24)
	found := false
	for _, l := range lines {
		if contains(l, "Rules") {
			found = true
			break
		}
	}
	if !found {
		t.Fatalf("Render missing 'Rules' tab: %v", lines)
	}
}

func TestPermissionsOverlayImplRenderShowsAllowRule(t *testing.T) {
	ov := newPermissionsOverlayImpl(permissionsOverlayOptions{
		AllowRules: []string{"Bash(git:*)"},
	})
	lines := ov.Render(80, 24)
	found := false
	for _, l := range lines {
		if contains(l, "Bash(git:*)") {
			found = true
			break
		}
	}
	if !found {
		t.Fatalf("Render missing allow rule 'Bash(git:*)': %v", lines)
	}
}

func TestPermissionsOverlayImplAddRuleViaKeybinding(t *testing.T) {
	// 'a' key in Rules tab opens add-rule mode; after typing + Enter, submit
	// "perm:add:allow:<rule>".
	ov := newPermissionsOverlayImpl(permissionsOverlayOptions{})
	// Press 'a' to enter add-allow mode
	ov.ApplyKey(tui.Key{Type: tui.KeyRune, Rune: 'a'})
	if !ov.IsAddingRule() {
		t.Fatal("after 'a': should be in add-rule mode")
	}
	// Type a rule
	ov.ApplyKey(tui.Key{Type: tui.KeyRune, Rune: 'B'})
	ov.ApplyKey(tui.Key{Type: tui.KeyRune, Rune: 'a'})
	ov.ApplyKey(tui.Key{Type: tui.KeyRune, Rune: 's'})
	ov.ApplyKey(tui.Key{Type: tui.KeyRune, Rune: 'h'})
	// Enter to confirm
	res, handled := ov.ApplyKey(tui.Key{Type: tui.KeyEnter})
	if !handled {
		t.Fatal("Enter in add-rule mode should be handled")
	}
	if !contains(res.Submit, "perm:add:allow:Bash") {
		t.Fatalf("submit = %q want prefix perm:add:allow:Bash", res.Submit)
	}
	if ov.IsAddingRule() {
		t.Fatal("after Enter: should have exited add-rule mode")
	}
}

func TestPermissionsOverlayImplCursorNavInRulesTab(t *testing.T) {
	ov := newPermissionsOverlayImpl(permissionsOverlayOptions{
		AllowRules: []string{"Bash(git:*)", "Read(**)"},
	})
	// ↓ moves cursor in rule list
	ov.ApplyKey(tui.Key{Type: tui.KeyDown})
	if ov.RuleCursor() != 1 {
		t.Fatalf("cursor after Down = %d want 1", ov.RuleCursor())
	}
	// ↑ back to 0
	ov.ApplyKey(tui.Key{Type: tui.KeyUp})
	if ov.RuleCursor() != 0 {
		t.Fatalf("cursor after Up = %d want 0", ov.RuleCursor())
	}
}

func TestPermissionsOverlayImplDeleteRule(t *testing.T) {
	var removedBehavior, removedRule string
	ov := newPermissionsOverlayImplWithRemover(permissionsOverlayOptions{
		AllowRules: []string{"Bash(git:*)", "Read(**)"},
	}, func(behavior, rule string) error {
		removedBehavior = behavior
		removedRule = rule
		return nil
	})
	// Navigate to second rule and delete
	ov.ApplyKey(tui.Key{Type: tui.KeyDown}) // cursor=1 → Read(**)
	res, handled := ov.ApplyKey(tui.Key{Type: tui.KeyRune, Rune: 'd'})
	if !handled {
		t.Fatal("'d' should be handled (delete rule)")
	}
	if removedBehavior != "allow" {
		t.Fatalf("removed behavior = %q want allow", removedBehavior)
	}
	if removedRule != "Read(**)" {
		t.Fatalf("removed rule = %q want Read(**)", removedRule)
	}
	if !contains(res.Submit, "perm:remove:allow:Read") {
		t.Fatalf("submit = %q want prefix perm:remove:allow:Read", res.Submit)
	}
}

func TestPermissionsHandlerNoArgOpensOverlay(t *testing.T) {
	// When /permissions is called with no args in the REPL, it should open
	// the PermissionsOverlay, not just print text.
	m := &permsMutator{}
	h := permissionsOverlayHandlerWith("", m.add, m.remove)
	out, err := h(context.Background(), CommandContext{Args: ""})
	if err != nil {
		t.Fatalf("handler err: %v", err)
	}
	if !out.Handled {
		t.Fatal("must be Handled")
	}
	if out.Overlay == nil {
		t.Fatal("no-arg /permissions must return an Overlay (not just text)")
	}
}

func TestPermissionsHandlerWithArgStillWorksAsText(t *testing.T) {
	// /permissions allow <rule> still returns text confirmation (no overlay).
	m := &permsMutator{}
	h := permissionsOverlayHandlerWith("", m.add, m.remove)
	out, err := h(context.Background(), CommandContext{Args: "allow Bash(git:*)"})
	if err != nil {
		t.Fatalf("handler err: %v", err)
	}
	if !out.Handled {
		t.Fatal("must be Handled")
	}
	// With arg: adds rule → returns text, no overlay.
	if out.Overlay != nil {
		t.Fatal("with arg, /permissions should not open overlay")
	}
	if len(m.calls) == 0 {
		t.Fatal("allow rule should have been added")
	}
}

func TestPermissionsOverlayImplTabCycleWrapsAround(t *testing.T) {
	ov := newPermissionsOverlayImpl(permissionsOverlayOptions{})
	// Tab through all 3 tabs (0→1→2→0)
	ov.ApplyKey(tui.Key{Type: tui.KeyTab}) // 0→1
	ov.ApplyKey(tui.Key{Type: tui.KeyTab}) // 1→2
	ov.ApplyKey(tui.Key{Type: tui.KeyTab}) // 2→0 (wrap)
	if ov.State().ActiveTab != 0 {
		t.Fatalf("tab wrap: ActiveTab = %d want 0", ov.State().ActiveTab)
	}
}
