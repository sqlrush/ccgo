package repl

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// permsMutator is a test double for the permissionsMutatorFn signature.
type permsMutator struct {
	calls []permsMutatorCall
}

type permsMutatorCall struct {
	op       string // "add" or "remove"
	behavior string
	rule     string
}

func (m *permsMutator) add(path, behavior, rule string) error {
	m.calls = append(m.calls, permsMutatorCall{op: "add", behavior: behavior, rule: rule})
	return nil
}

func (m *permsMutator) remove(path, behavior, rule string) error {
	m.calls = append(m.calls, permsMutatorCall{op: "remove", behavior: behavior, rule: rule})
	return nil
}

func TestPermissionsHandlerList(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "settings.json")

	// Write a seed doc with known rules.
	initial := map[string]any{
		"permissions": map[string]any{
			"allow": []any{"Bash(ls:*)"},
			"deny":  []any{"Bash(rm:*)"},
			"ask":   []any{},
		},
	}
	data, _ := json.Marshal(initial)
	if err := os.WriteFile(path, data, 0o600); err != nil {
		t.Fatalf("seed: %v", err)
	}

	m := &permsMutator{}
	h := permissionsHandlerWith(path, m.add, m.remove)

	out, err := h(context.Background(), CommandContext{Args: ""})
	if err != nil {
		t.Fatalf("list err: %v", err)
	}
	if !out.Handled {
		t.Fatal("must be handled")
	}
	if !strings.Contains(out.Status, "Bash(ls:*)") {
		t.Errorf("list should contain allow rule, got: %q", out.Status)
	}
	if !strings.Contains(out.Status, "Bash(rm:*)") {
		t.Errorf("list should contain deny rule, got: %q", out.Status)
	}
	if len(m.calls) != 0 {
		t.Fatalf("list must not mutate; calls: %v", m.calls)
	}
}

func TestPermissionsHandlerAllow(t *testing.T) {
	dir := t.TempDir()
	m := &permsMutator{}
	h := permissionsHandlerWith(filepath.Join(dir, "settings.json"), m.add, m.remove)

	out, err := h(context.Background(), CommandContext{Args: "allow Bash(git status:*)"})
	if err != nil {
		t.Fatalf("allow err: %v", err)
	}
	if !out.Handled {
		t.Fatal("must be handled")
	}
	if len(m.calls) != 1 {
		t.Fatalf("expected 1 call, got %d", len(m.calls))
	}
	c := m.calls[0]
	if c.op != "add" || c.behavior != "allow" || c.rule != "Bash(git status:*)" {
		t.Fatalf("unexpected call: %+v", c)
	}
	if !strings.Contains(out.Status, "allow") {
		t.Errorf("status should mention allow: %q", out.Status)
	}
}

func TestPermissionsHandlerDeny(t *testing.T) {
	dir := t.TempDir()
	m := &permsMutator{}
	h := permissionsHandlerWith(filepath.Join(dir, "settings.json"), m.add, m.remove)

	out, err := h(context.Background(), CommandContext{Args: "deny Bash(rm:*)"})
	if err != nil {
		t.Fatalf("deny err: %v", err)
	}
	if !out.Handled {
		t.Fatal("must be handled")
	}
	if len(m.calls) != 1 {
		t.Fatalf("expected 1 call, got %d", len(m.calls))
	}
	c := m.calls[0]
	if c.op != "add" || c.behavior != "deny" || c.rule != "Bash(rm:*)" {
		t.Fatalf("unexpected call: %+v", c)
	}
}

func TestPermissionsHandlerRemove(t *testing.T) {
	dir := t.TempDir()
	m := &permsMutator{}
	h := permissionsHandlerWith(filepath.Join(dir, "settings.json"), m.add, m.remove)

	out, err := h(context.Background(), CommandContext{Args: "remove allow Bash(ls:*)"})
	if err != nil {
		t.Fatalf("remove err: %v", err)
	}
	if !out.Handled {
		t.Fatal("must be handled")
	}
	if len(m.calls) != 1 {
		t.Fatalf("expected 1 call, got %d", len(m.calls))
	}
	c := m.calls[0]
	if c.op != "remove" || c.behavior != "allow" || c.rule != "Bash(ls:*)" {
		t.Fatalf("unexpected call: %+v", c)
	}
}

func TestPermissionsHandlerNoArg(t *testing.T) {
	// No-arg on missing file → list shows empty state (no error).
	dir := t.TempDir()
	m := &permsMutator{}
	h := permissionsHandlerWith(filepath.Join(dir, "settings.json"), m.add, m.remove)
	out, err := h(context.Background(), CommandContext{Args: ""})
	if err != nil {
		t.Fatalf("no-arg on missing file: %v", err)
	}
	if !out.Handled {
		t.Fatal("must be handled")
	}
	if len(m.calls) != 0 {
		t.Fatalf("no-arg must not mutate: %v", m.calls)
	}
}

func TestPermissionsHandlerBadSubcommand(t *testing.T) {
	dir := t.TempDir()
	m := &permsMutator{}
	h := permissionsHandlerWith(filepath.Join(dir, "settings.json"), m.add, m.remove)
	out, err := h(context.Background(), CommandContext{Args: "bogus"})
	if err != nil {
		t.Fatalf("bad subcommand should not error: %v", err)
	}
	if !out.Handled {
		t.Fatal("must be handled even on bad subcommand (show usage)")
	}
	if len(m.calls) != 0 {
		t.Fatalf("bad subcommand must not mutate: %v", m.calls)
	}
}
