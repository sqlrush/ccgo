package config

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
)

func readPerms(t *testing.T, path string) map[string]any {
	t.Helper()
	raw, _ := os.ReadFile(path)
	var doc map[string]any
	_ = json.Unmarshal(raw, &doc)
	perms, _ := doc["permissions"].(map[string]any)
	return perms
}

func TestAddRemovePermissionRule(t *testing.T) {
	path := filepath.Join(t.TempDir(), "settings.json")
	if err := AddPermissionRule(path, "allow", "Bash(ls:*)"); err != nil {
		t.Fatalf("add err: %v", err)
	}
	if err := AddPermissionRule(path, "allow", "Bash(ls:*)"); err != nil { // idempotent
		t.Fatalf("re-add err: %v", err)
	}
	perms := readPerms(t, path)
	allow, _ := perms["allow"].([]any)
	if len(allow) != 1 || allow[0] != "Bash(ls:*)" {
		t.Fatalf("allow = %v want one Bash(ls:*)", allow)
	}
	if err := RemovePermissionRule(path, "allow", "Bash(ls:*)"); err != nil {
		t.Fatalf("remove err: %v", err)
	}
	perms = readPerms(t, path)
	if allow, _ := perms["allow"].([]any); len(allow) != 0 {
		t.Fatalf("rule not removed: %v", allow)
	}
}

func TestAddPermissionRuleRejectsBadBehavior(t *testing.T) {
	if err := AddPermissionRule(filepath.Join(t.TempDir(), "s.json"), "maybe", "Bash(x)"); err == nil {
		t.Fatal("invalid behavior must error")
	}
}

// TestNoClobberOtherKeysAndBehaviors verifies that adding/removing rules in one
// behavior list does not disturb other behaviors or unrelated top-level keys.
func TestNoClobberOtherKeysAndBehaviors(t *testing.T) {
	path := filepath.Join(t.TempDir(), "settings.json")

	// Seed a doc with an existing deny rule and an unrelated top-level key.
	initial := map[string]any{
		"theme": "dark",
		"permissions": map[string]any{
			"deny": []any{"Bash(rm:*)"},
			"ask":  []any{"Read(secret:*)"},
		},
	}
	data, _ := json.Marshal(initial)
	if err := os.WriteFile(path, data, 0o600); err != nil {
		t.Fatalf("seed: %v", err)
	}

	// Add an allow rule.
	if err := AddPermissionRule(path, "allow", "Bash(ls:*)"); err != nil {
		t.Fatalf("add: %v", err)
	}

	raw, _ := os.ReadFile(path)
	var doc map[string]any
	_ = json.Unmarshal(raw, &doc)

	// Unrelated top-level key must survive.
	if doc["theme"] != "dark" {
		t.Fatalf("theme clobbered: %v", doc["theme"])
	}

	perms, _ := doc["permissions"].(map[string]any)

	// deny list must survive.
	denyList, _ := perms["deny"].([]any)
	if len(denyList) != 1 || denyList[0] != "Bash(rm:*)" {
		t.Fatalf("deny clobbered: %v", denyList)
	}

	// ask list must survive.
	askList, _ := perms["ask"].([]any)
	if len(askList) != 1 || askList[0] != "Read(secret:*)" {
		t.Fatalf("ask clobbered: %v", askList)
	}

	// allow rule must be present.
	allowList, _ := perms["allow"].([]any)
	if len(allowList) != 1 || allowList[0] != "Bash(ls:*)" {
		t.Fatalf("allow not added: %v", allowList)
	}

	// Now remove the allow rule; deny + ask must still be intact.
	if err := RemovePermissionRule(path, "allow", "Bash(ls:*)"); err != nil {
		t.Fatalf("remove: %v", err)
	}

	raw, _ = os.ReadFile(path)
	_ = json.Unmarshal(raw, &doc)
	perms, _ = doc["permissions"].(map[string]any)

	if doc["theme"] != "dark" {
		t.Fatalf("theme clobbered after remove: %v", doc["theme"])
	}
	denyList, _ = perms["deny"].([]any)
	if len(denyList) != 1 {
		t.Fatalf("deny clobbered after remove: %v", denyList)
	}
	askList, _ = perms["ask"].([]any)
	if len(askList) != 1 {
		t.Fatalf("ask clobbered after remove: %v", askList)
	}
	allowList, _ = perms["allow"].([]any)
	if len(allowList) != 0 {
		t.Fatalf("allow not empty after remove: %v", allowList)
	}
}
