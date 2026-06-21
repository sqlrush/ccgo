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
