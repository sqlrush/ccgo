package settingswriter

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"ccgo/internal/contracts"
)

func readDoc(t *testing.T, path string) map[string]any {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read %s: %v", path, err)
	}
	doc := map[string]any{}
	if err := json.Unmarshal(data, &doc); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	return doc
}

func allowList(t *testing.T, doc map[string]any) []any {
	t.Helper()
	perms, ok := doc["permissions"].(map[string]any)
	if !ok {
		t.Fatalf("permissions missing or wrong type: %#v", doc["permissions"])
	}
	list, _ := perms["allow"].([]any)
	return list
}

func TestApplyAddsUserAllowRule(t *testing.T) {
	dir := t.TempDir()
	userPath := filepath.Join(dir, "user", "settings.json")
	projPath := filepath.Join(dir, "proj", ".claude", "settings.json")
	w := New(userPath, projPath)

	err := w.Apply(contracts.PermissionUpdate{
		Type:        "addRules",
		Destination: "userSettings",
		Behavior:    contracts.PermissionAllow,
		Rules:       []contracts.PermissionRuleValue{{ToolName: "Bash", RuleContent: "git status:*"}},
	})
	if err != nil {
		t.Fatalf("Apply: %v", err)
	}
	list := allowList(t, readDoc(t, userPath))
	if len(list) != 1 || list[0] != "Bash(git status:*)" {
		t.Fatalf("allow = %#v want [Bash(git status:*)]", list)
	}
}

func TestApplyProjectDestinationAndDedup(t *testing.T) {
	dir := t.TempDir()
	userPath := filepath.Join(dir, "user", "settings.json")
	projPath := filepath.Join(dir, "proj", ".claude", "settings.json")
	w := New(userPath, projPath)

	upd := contracts.PermissionUpdate{
		Type:        "addRules",
		Destination: "projectSettings",
		Behavior:    contracts.PermissionAllow,
		Rules:       []contracts.PermissionRuleValue{{ToolName: "Read"}},
	}
	if err := w.Apply(upd); err != nil {
		t.Fatalf("Apply 1: %v", err)
	}
	if err := w.Apply(upd); err != nil { // idempotent: no duplicate
		t.Fatalf("Apply 2: %v", err)
	}
	list := allowList(t, readDoc(t, projPath))
	if len(list) != 1 || list[0] != "Read" {
		t.Fatalf("allow = %#v want exactly [Read]", list)
	}
}

func TestApplyRejectsEmptyRules(t *testing.T) {
	w := New(filepath.Join(t.TempDir(), "s.json"), filepath.Join(t.TempDir(), "p.json"))
	err := w.Apply(contracts.PermissionUpdate{Type: "addRules", Behavior: contracts.PermissionAllow})
	if err == nil {
		t.Fatal("expected error for update with no rules")
	}
}

func TestApplyPreservesSiblingPermissionsKeys(t *testing.T) {
	dir := t.TempDir()
	userPath := filepath.Join(dir, "settings.json")
	initial := map[string]any{
		"theme": "dark",
		"permissions": map[string]any{
			"deny": []any{"Bash(rm -rf:*)"},
		},
	}
	data, _ := json.MarshalIndent(initial, "", "  ")
	if err := os.MkdirAll(filepath.Dir(userPath), 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(userPath, data, 0600); err != nil {
		t.Fatal(err)
	}
	w := New(userPath, filepath.Join(dir, "proj.json"))
	if err := w.Apply(contracts.PermissionUpdate{
		Type:        "addRules",
		Destination: "userSettings",
		Behavior:    contracts.PermissionAllow,
		Rules:       []contracts.PermissionRuleValue{{ToolName: "Read"}},
	}); err != nil {
		t.Fatalf("Apply: %v", err)
	}
	doc := readDoc(t, userPath)
	if doc["theme"] != "dark" {
		t.Errorf("theme key clobbered: %v", doc["theme"])
	}
	perms := doc["permissions"].(map[string]any)
	if deny, _ := perms["deny"].([]any); len(deny) != 1 || deny[0] != "Bash(rm -rf:*)" {
		t.Errorf("deny key clobbered: %v", perms["deny"])
	}
	if allow, _ := perms["allow"].([]any); len(allow) != 1 || allow[0] != "Read" {
		t.Errorf("allow not written: %v", perms["allow"])
	}
}
