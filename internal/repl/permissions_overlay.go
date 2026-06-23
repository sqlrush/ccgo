package repl

// PERM-PERSIST-06: /permissions TUI overlay state layer.
//
// This file provides the pure-data state model for the permissions overlay
// (Tab switching, rule management, recent-denials, workspace-dirs).
//
// Rendering is MANUAL — the TUI layer to render this state using bubbletea or
// equivalent is intentionally deferred. The state and its mutators are fully
// testable without a terminal.
//
// CC ref: src/components/permissions/rules/PermissionRuleList.tsx
//         (WorkspaceTab / RecentDenialsTab / AddPermissionRules).

// PermissionDenialRecord records a single denied permission request so it can
// be shown in the "Recent-Denials" tab and retried by the user.
type PermissionDenialRecord struct {
	ToolName    string
	Description string
	Path        string
}

// permissionsOverlayOptions supplies the initial data for the overlay.
type permissionsOverlayOptions struct {
	AllowRules    []string
	DenyRules     []string
	AskRules      []string
	RecentDenials []PermissionDenialRecord
	WorkspaceDirs []string
}

// PermissionsOverlayState is an immutable snapshot of the /permissions overlay.
// All mutator methods return a new value; the original is never modified.
type PermissionsOverlayState struct {
	// Tabs is the ordered list of tab labels.
	Tabs []string
	// ActiveTab is the index of the currently selected tab (0 = Rules, etc.).
	ActiveTab int

	// Per-tab data (read from settings at construction time).
	AllowRules    []string
	DenyRules     []string
	AskRules      []string
	RecentDenials []PermissionDenialRecord
	WorkspaceDirs []string

	// Edits made in the overlay but not yet persisted.
	PendingAllowRules []string
	PendingDenyRules  []string
	PendingAskRules   []string
}

var overlayTabs = []string{"Rules", "Recent-Denials", "Workspace"}

// newPermissionsOverlayState constructs an initial state from options.
func newPermissionsOverlayState(opts permissionsOverlayOptions) PermissionsOverlayState {
	return PermissionsOverlayState{
		Tabs:          append([]string(nil), overlayTabs...),
		ActiveTab:     0,
		AllowRules:    append([]string(nil), opts.AllowRules...),
		DenyRules:     append([]string(nil), opts.DenyRules...),
		AskRules:      append([]string(nil), opts.AskRules...),
		RecentDenials: append([]PermissionDenialRecord(nil), opts.RecentDenials...),
		WorkspaceDirs: append([]string(nil), opts.WorkspaceDirs...),
	}
}

// WithActiveTab returns a copy of the state with the given tab index selected.
func (s PermissionsOverlayState) WithActiveTab(tab int) PermissionsOverlayState {
	next := s.clone()
	next.ActiveTab = tab
	return next
}

// WithRuleAdded returns a copy of the state with a pending rule added for
// the given behavior ("allow", "deny", or "ask").
func (s PermissionsOverlayState) WithRuleAdded(behavior, rule string) PermissionsOverlayState {
	next := s.clone()
	switch behavior {
	case "allow":
		next.PendingAllowRules = append(append([]string(nil), s.PendingAllowRules...), rule)
	case "deny":
		next.PendingDenyRules = append(append([]string(nil), s.PendingDenyRules...), rule)
	case "ask":
		next.PendingAskRules = append(append([]string(nil), s.PendingAskRules...), rule)
	}
	return next
}

// WithRuleRemoved returns a copy of the state with the given rule removed from
// the behavior list (operates on persisted rules, not pending).
func (s PermissionsOverlayState) WithRuleRemoved(behavior, rule string) PermissionsOverlayState {
	next := s.clone()
	switch behavior {
	case "allow":
		next.AllowRules = filterRules(s.AllowRules, rule)
	case "deny":
		next.DenyRules = filterRules(s.DenyRules, rule)
	case "ask":
		next.AskRules = filterRules(s.AskRules, rule)
	}
	return next
}

// clone creates a shallow copy of the state (slices are re-allocated).
func (s PermissionsOverlayState) clone() PermissionsOverlayState {
	return PermissionsOverlayState{
		Tabs:              append([]string(nil), s.Tabs...),
		ActiveTab:         s.ActiveTab,
		AllowRules:        append([]string(nil), s.AllowRules...),
		DenyRules:         append([]string(nil), s.DenyRules...),
		AskRules:          append([]string(nil), s.AskRules...),
		RecentDenials:     append([]PermissionDenialRecord(nil), s.RecentDenials...),
		WorkspaceDirs:     append([]string(nil), s.WorkspaceDirs...),
		PendingAllowRules: append([]string(nil), s.PendingAllowRules...),
		PendingDenyRules:  append([]string(nil), s.PendingDenyRules...),
		PendingAskRules:   append([]string(nil), s.PendingAskRules...),
	}
}

// filterRules returns a copy of rules with target removed (all occurrences).
func filterRules(rules []string, target string) []string {
	out := make([]string, 0, len(rules))
	for _, r := range rules {
		if r != target {
			out = append(out, r)
		}
	}
	return out
}
