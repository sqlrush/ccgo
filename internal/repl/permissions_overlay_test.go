package repl

// PERM-PERSIST-06: /permissions TUI overlay state layer tests.
// The overlay has three tabs: Rules / Recent-Denials / Workspace.
// This tests the state construction and mutation; rendering is MANUAL.
// CC ref: src/components/permissions/rules/PermissionRuleList.tsx.

import (
	"testing"
)

func TestNewPermissionsOverlayStateHasThreeTabs(t *testing.T) {
	state := newPermissionsOverlayState(permissionsOverlayOptions{})
	if len(state.Tabs) != 3 {
		t.Errorf("expected 3 tabs, got %d", len(state.Tabs))
	}
	expectedTabs := []string{"Rules", "Recent-Denials", "Workspace"}
	for i, want := range expectedTabs {
		if state.Tabs[i] != want {
			t.Errorf("Tab[%d] = %q, want %q", i, state.Tabs[i], want)
		}
	}
}

func TestPermissionsOverlayStateInitialTab(t *testing.T) {
	state := newPermissionsOverlayState(permissionsOverlayOptions{})
	if state.ActiveTab != 0 {
		t.Errorf("initial active tab = %d, want 0 (Rules)", state.ActiveTab)
	}
}

func TestPermissionsOverlayStateNextTab(t *testing.T) {
	state := newPermissionsOverlayState(permissionsOverlayOptions{})
	next := state.WithActiveTab(1)
	if next.ActiveTab != 1 {
		t.Errorf("WithActiveTab(1).ActiveTab = %d, want 1", next.ActiveTab)
	}
	// Immutability: original unchanged.
	if state.ActiveTab != 0 {
		t.Errorf("original state mutated: ActiveTab = %d, want 0", state.ActiveTab)
	}
}

func TestPermissionsOverlayStateAddRule(t *testing.T) {
	state := newPermissionsOverlayState(permissionsOverlayOptions{})
	updated := state.WithRuleAdded("allow", "Bash(git:*)")
	if len(updated.PendingAllowRules) != 1 {
		t.Errorf("expected 1 pending allow rule, got %d", len(updated.PendingAllowRules))
	}
	if updated.PendingAllowRules[0] != "Bash(git:*)" {
		t.Errorf("PendingAllowRules[0] = %q, want %q", updated.PendingAllowRules[0], "Bash(git:*)")
	}
	// Immutability: original unchanged.
	if len(state.PendingAllowRules) != 0 {
		t.Errorf("original PendingAllowRules mutated")
	}
}

func TestPermissionsOverlayStateRemoveRule(t *testing.T) {
	state := newPermissionsOverlayState(permissionsOverlayOptions{
		AllowRules: []string{"Bash(git:*)", "Read(**)", "Write(**)"},
	})
	updated := state.WithRuleRemoved("allow", "Read(**)")
	remaining := updated.AllowRules
	for _, r := range remaining {
		if r == "Read(**)" {
			t.Errorf("expected Read(**) to be removed, still present in: %v", remaining)
		}
	}
	if len(remaining) != 2 {
		t.Errorf("expected 2 remaining allow rules, got %d: %v", len(remaining), remaining)
	}
}

func TestPermissionsOverlayStateRecentDenials(t *testing.T) {
	denials := []PermissionDenialRecord{
		{ToolName: "Bash", Description: "rm -rf /tmp/foo"},
		{ToolName: "Write", Description: "/etc/passwd"},
	}
	state := newPermissionsOverlayState(permissionsOverlayOptions{
		RecentDenials: denials,
	})
	if len(state.RecentDenials) != 2 {
		t.Errorf("expected 2 recent denials, got %d", len(state.RecentDenials))
	}
	if state.RecentDenials[0].ToolName != "Bash" {
		t.Errorf("RecentDenials[0].ToolName = %q, want %q", state.RecentDenials[0].ToolName, "Bash")
	}
}

func TestPermissionsOverlayStateWorkspaceDirs(t *testing.T) {
	state := newPermissionsOverlayState(permissionsOverlayOptions{
		WorkspaceDirs: []string{"/home/user/project", "/shared"},
	})
	if len(state.WorkspaceDirs) != 2 {
		t.Errorf("expected 2 workspace dirs, got %d", len(state.WorkspaceDirs))
	}
}
