package repl

import (
	"testing"

	"ccgo/internal/contracts"
	"ccgo/internal/tool"
)

func TestPermissionActionsBash(t *testing.T) {
	pa := permissionActions(tool.PermissionAskRequest{ToolName: "Bash", Description: "run git status"})
	if len(pa.Actions) != 3 {
		t.Fatalf("Bash actions = %v want 3", pa.Actions)
	}
	if pa.AlwaysIndex < 0 || pa.AlwaysIndex >= len(pa.Actions) {
		t.Fatalf("AlwaysIndex %d out of range", pa.AlwaysIndex)
	}
}

func TestDecisionForActionAllowOnce(t *testing.T) {
	pa := permissionActions(tool.PermissionAskRequest{ToolName: "Read", Path: "/tmp/a"})
	d := decisionForAction(tool.PermissionAskRequest{ToolName: "Read", Path: "/tmp/a"}, pa.Actions[0])
	if d.Behavior != contracts.PermissionAllow {
		t.Fatalf("allow-once behavior = %v want allow", d.Behavior)
	}
	if len(d.Suggestions) != 0 {
		t.Fatalf("allow-once must not carry persistence suggestions: %#v", d.Suggestions)
	}
}

func TestDecisionForActionAllowAlwaysCarriesSuggestion(t *testing.T) {
	req := tool.PermissionAskRequest{ToolName: "Read", Path: "/tmp/a"}
	pa := permissionActions(req)
	d := decisionForAction(req, pa.Actions[pa.AlwaysIndex])
	if d.Behavior != contracts.PermissionAllow {
		t.Fatalf("always behavior = %v want allow", d.Behavior)
	}
	if len(d.Suggestions) != 1 {
		t.Fatalf("always must carry exactly one suggestion, got %d", len(d.Suggestions))
	}
	s := d.Suggestions[0]
	if s.Type != "addRules" || s.Behavior != contracts.PermissionAllow {
		t.Fatalf("suggestion = %+v want addRules/allow", s)
	}
	if len(s.Rules) != 1 || s.Rules[0].ToolName != "Read" {
		t.Fatalf("suggestion rule = %+v want Read", s.Rules)
	}
}

func TestDecisionForActionDeny(t *testing.T) {
	req := tool.PermissionAskRequest{ToolName: "Bash"}
	d := decisionForAction(req, "Deny")
	if d.Behavior != contracts.PermissionDeny {
		t.Fatalf("deny behavior = %v", d.Behavior)
	}
}

// TestDecisionForActionBashAlwaysWithoutScopeDoesNotPersist asserts that
// picking "always" for Bash without a concrete scope yields no persistence rule
// (security: must not write a bare allow-all-Bash rule).
func TestDecisionForActionBashAlwaysWithoutScopeDoesNotPersist(t *testing.T) {
	req := tool.PermissionAskRequest{ToolName: "Bash"}
	pa := permissionActions(req)
	d := decisionForAction(req, pa.Actions[pa.AlwaysIndex])
	if d.Behavior != contracts.PermissionAllow {
		t.Fatalf("behavior = %v, want allow", d.Behavior)
	}
	if len(d.Suggestions) != 0 {
		t.Fatalf("unscoped Bash always must not persist: got %d suggestions: %#v", len(d.Suggestions), d.Suggestions)
	}
	if d.Message == "" {
		t.Fatal("unscoped Bash always must set a Message explaining degradation to allow-once")
	}
}

// TestDecisionForActionWebFetchAlwaysWithoutScopeDoesNotPersist asserts the
// same least-privilege property for WebFetch (no host = no persist).
func TestDecisionForActionWebFetchAlwaysWithoutScopeDoesNotPersist(t *testing.T) {
	req := tool.PermissionAskRequest{ToolName: "WebFetch"}
	pa := permissionActions(req)
	d := decisionForAction(req, pa.Actions[pa.AlwaysIndex])
	if d.Behavior != contracts.PermissionAllow {
		t.Fatalf("behavior = %v, want allow", d.Behavior)
	}
	if len(d.Suggestions) != 0 {
		t.Fatalf("unscoped WebFetch always must not persist: got %d suggestions: %#v", len(d.Suggestions), d.Suggestions)
	}
	if d.Message == "" {
		t.Fatal("unscoped WebFetch always must set a Message explaining degradation to allow-once")
	}
}

// TestDecisionForActionBashAlwaysWithScopePersistsScopedRule asserts that a
// scoped Bash request (scope via req.Path) DOES produce exactly one scoped rule.
func TestDecisionForActionBashAlwaysWithScopePersistsScopedRule(t *testing.T) {
	req := tool.PermissionAskRequest{
		ToolName: "Bash",
		Path:     "git status:*",
	}
	pa := permissionActions(req)
	d := decisionForAction(req, pa.Actions[pa.AlwaysIndex])
	if d.Behavior != contracts.PermissionAllow {
		t.Fatalf("behavior = %v, want allow", d.Behavior)
	}
	if len(d.Suggestions) != 1 {
		t.Fatalf("scoped Bash always must have exactly 1 suggestion, got %d", len(d.Suggestions))
	}
	s := d.Suggestions[0]
	if len(s.Rules) != 1 {
		t.Fatalf("suggestion must carry exactly 1 rule, got %d", len(s.Rules))
	}
	rule := s.Rules[0]
	if rule.ToolName != "Bash" {
		t.Fatalf("rule ToolName = %q, want Bash", rule.ToolName)
	}
	if rule.RuleContent == "" {
		t.Fatal("scoped Bash rule must have non-empty RuleContent (scoped, not bare)")
	}
}
