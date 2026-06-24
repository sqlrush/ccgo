package sandbox

import (
	"testing"

	"ccgo/internal/contracts"
)

// TestPolicyFromSettingsWeakerNestedSandbox verifies SBX-52:
// PolicyFromSettings maps sandbox.enableWeakerNestedSandbox → Policy.EnableWeakerNestedSandbox.
//
// Given:  settings.Sandbox = {"enabled":true, "enableWeakerNestedSandbox":true}
// When:   PolicyFromSettings is called
// Then:   Policy.EnableWeakerNestedSandbox = true
func TestPolicyFromSettingsWeakerNestedSandbox(t *testing.T) {
	t.Run("true", func(t *testing.T) {
		s := contracts.Settings{
			Sandbox: map[string]any{
				"enabled":                   true,
				"enableWeakerNestedSandbox": true,
			},
		}
		p := PolicyFromSettings(s)
		if !p.EnableWeakerNestedSandbox {
			t.Fatal("EnableWeakerNestedSandbox should be true when setting is true")
		}
	})

	t.Run("false", func(t *testing.T) {
		s := contracts.Settings{
			Sandbox: map[string]any{
				"enabled":                   true,
				"enableWeakerNestedSandbox": false,
			},
		}
		p := PolicyFromSettings(s)
		if p.EnableWeakerNestedSandbox {
			t.Fatal("EnableWeakerNestedSandbox should be false when setting is false")
		}
	})

	t.Run("absent defaults to false", func(t *testing.T) {
		s := contracts.Settings{
			Sandbox: map[string]any{"enabled": true},
		}
		p := PolicyFromSettings(s)
		if p.EnableWeakerNestedSandbox {
			t.Fatal("EnableWeakerNestedSandbox should default to false when not set")
		}
	})
}

// TestPolicyFromSettingsWeakerNetworkIsolation verifies SBX-53:
// PolicyFromSettings maps sandbox.enableWeakerNetworkIsolation → Policy.EnableWeakerNetworkIsolation.
//
// Given:  settings.Sandbox = {"enabled":true, "enableWeakerNetworkIsolation":true}
// When:   PolicyFromSettings is called
// Then:   Policy.EnableWeakerNetworkIsolation = true
func TestPolicyFromSettingsWeakerNetworkIsolation(t *testing.T) {
	t.Run("true", func(t *testing.T) {
		s := contracts.Settings{
			Sandbox: map[string]any{
				"enabled":                       true,
				"enableWeakerNetworkIsolation":  true,
			},
		}
		p := PolicyFromSettings(s)
		if !p.EnableWeakerNetworkIsolation {
			t.Fatal("EnableWeakerNetworkIsolation should be true when setting is true")
		}
	})

	t.Run("false", func(t *testing.T) {
		s := contracts.Settings{
			Sandbox: map[string]any{
				"enabled":                       true,
				"enableWeakerNetworkIsolation":  false,
			},
		}
		p := PolicyFromSettings(s)
		if p.EnableWeakerNetworkIsolation {
			t.Fatal("EnableWeakerNetworkIsolation should be false when setting is false")
		}
	})

	t.Run("absent defaults to false", func(t *testing.T) {
		s := contracts.Settings{
			Sandbox: map[string]any{"enabled": true},
		}
		p := PolicyFromSettings(s)
		if p.EnableWeakerNetworkIsolation {
			t.Fatal("EnableWeakerNetworkIsolation should default to false when not set")
		}
	})
}

// TestWeakerFieldsIndependent verifies that EnableWeakerNestedSandbox and
// EnableWeakerNetworkIsolation can be set independently.
func TestWeakerFieldsIndependent(t *testing.T) {
	s := contracts.Settings{
		Sandbox: map[string]any{
			"enabled":                       true,
			"enableWeakerNestedSandbox":     true,
			"enableWeakerNetworkIsolation":  false,
		},
	}
	p := PolicyFromSettings(s)
	if !p.EnableWeakerNestedSandbox {
		t.Error("EnableWeakerNestedSandbox should be true")
	}
	if p.EnableWeakerNetworkIsolation {
		t.Error("EnableWeakerNetworkIsolation should be false")
	}
}
