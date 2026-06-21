package sandbox

import (
	"testing"

	"ccgo/internal/contracts"
)

func TestShouldSandbox(t *testing.T) {
	cases := []struct {
		name      string
		policy    Policy
		dangerous bool
		want      bool
	}{
		{"disabled never sandboxes", Policy{Enabled: false}, false, false},
		{"enabled sandboxes by default", Policy{Enabled: true}, false, true},
		{"flag bypasses only when allowed", Policy{Enabled: true, AllowUnsandboxed: true}, true, false},
		{"flag ignored when policy forbids unsandboxed", Policy{Enabled: true, AllowUnsandboxed: false}, true, true},
		{"flag without enabled is moot", Policy{Enabled: false, AllowUnsandboxed: true}, true, false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := tc.policy.ShouldSandbox(tc.dangerous); got != tc.want {
				t.Fatalf("ShouldSandbox(%v) = %v want %v", tc.dangerous, got, tc.want)
			}
		})
	}
}

func TestPolicyFromSettings(t *testing.T) {
	s := contracts.Settings{
		Sandbox: map[string]any{
			"enabled":                  true,
			"allowUnsandboxedCommands": false,
			"failIfUnavailable":        true,
			"filesystem": map[string]any{
				"allowWrite": []any{"/tmp/work"},
				"denyRead":   []any{"/etc/secret"},
			},
		},
	}
	p := PolicyFromSettings(s)
	if !p.Enabled || p.AllowUnsandboxed || !p.FailIfUnavailable {
		t.Fatalf("flags = %+v", p)
	}
	if len(p.AllowWrite) != 1 || p.AllowWrite[0] != "/tmp/work" {
		t.Fatalf("AllowWrite = %v", p.AllowWrite)
	}
	if len(p.DenyRead) != 1 || p.DenyRead[0] != "/etc/secret" {
		t.Fatalf("DenyRead = %v", p.DenyRead)
	}
}

func TestPolicyFromSettingsDefaultsSafe(t *testing.T) {
	// No sandbox block: disabled, but unsandboxed allowed (CC default ?? true).
	p := PolicyFromSettings(contracts.Settings{})
	if p.Enabled {
		t.Fatal("absent sandbox settings must default Enabled=false")
	}
	if !p.AllowUnsandboxed {
		t.Fatal("absent allowUnsandboxedCommands defaults to true (CC parity)")
	}
}
