package config

import (
	"os"
	"path/filepath"
	"testing"

	"ccgo/internal/contracts"
)

func TestLoadSettingsFile(t *testing.T) {
	path := filepath.Join(t.TempDir(), "settings.json")
	if err := os.WriteFile(path, []byte(`{"model":"opus","env":{"DEBUG":1,"TRACE":true},"permissions":{"allow":["Bash(ls *)"]}}`), 0o644); err != nil {
		t.Fatal(err)
	}
	settings, err := LoadSettingsFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if settings.Model != "opus" {
		t.Fatalf("model = %q", settings.Model)
	}
	if got := settings.Permissions.Allow[0]; got != "Bash(ls *)" {
		t.Fatalf("allow[0] = %q", got)
	}
	if settings.Env["DEBUG"] != "1" || settings.Env["TRACE"] != "true" {
		t.Fatalf("env = %#v", settings.Env)
	}
}

func TestLoadSettingsFileWithWarningsFiltersInvalidPermissionRules(t *testing.T) {
	path := filepath.Join(t.TempDir(), "settings.json")
	if err := os.WriteFile(path, []byte(`{
		"permissions": {
			"allow": ["Bash(ls *)", "Bash()", 42],
			"deny": ["WebFetch(https://example.com)"],
			"defaultMode": "bad-mode",
			"disableBypassPermissionsMode": true
		},
		"sandbox": {
			"allowUnsandboxedCommands": "no"
		}
	}`), 0o644); err != nil {
		t.Fatal(err)
	}
	settings, warnings, err := LoadSettingsFileWithWarnings(path)
	if err != nil {
		t.Fatal(err)
	}
	if len(settings.Permissions.Allow) != 1 || settings.Permissions.Allow[0] != "Bash(ls *)" {
		t.Fatalf("allow = %#v", settings.Permissions.Allow)
	}
	if len(settings.Permissions.Deny) != 0 {
		t.Fatalf("deny = %#v", settings.Permissions.Deny)
	}
	if len(warnings) != 6 {
		t.Fatalf("warnings = %#v", warnings)
	}
	paths := map[string]int{}
	for _, warning := range warnings {
		paths[warning.Path]++
	}
	if paths["permissions.allow"] != 2 || paths["permissions.deny"] != 1 || paths["permissions.defaultMode"] != 1 || paths["permissions.disableBypassPermissionsMode"] != 1 || paths["sandbox.allowUnsandboxedCommands"] != 1 {
		t.Fatalf("warning paths = %#v warnings=%#v", paths, warnings)
	}
}

func TestValidateSettingsWarnsForInvalidSandboxFilesystem(t *testing.T) {
	_, warnings, err := ParseSettingsJSON([]byte(`{
		"sandbox": {
			"filesystem": {
				"allowWrite": "../tmp",
				"denyRead": ["private", 42]
			}
		}
	}`), "settings.json")
	if err != nil {
		t.Fatal(err)
	}
	if len(warnings) != 2 {
		t.Fatalf("warnings = %#v", warnings)
	}
	paths := map[string]int{}
	for _, warning := range warnings {
		paths[warning.Path]++
	}
	if paths["sandbox.filesystem.allowWrite"] != 1 || paths["sandbox.filesystem.denyRead"] != 1 {
		t.Fatalf("warning paths = %#v warnings=%#v", paths, warnings)
	}
}

func TestMergeSettings(t *testing.T) {
	a := contracts.Settings{
		Env: map[string]string{"A": "1"},
		Permissions: &contracts.PermissionsSetting{
			Allow:       []string{"Read"},
			DefaultMode: contracts.PermissionDefault,
		},
	}
	b := contracts.Settings{
		Env:   map[string]string{"A": "2", "B": "3"},
		Model: "sonnet",
		Permissions: &contracts.PermissionsSetting{
			Deny:        []string{"Bash(rm *)"},
			DefaultMode: contracts.PermissionPlan,
		},
	}
	merged := MergeSettings(a, b)
	if merged.Env["A"] != "2" || merged.Env["B"] != "3" {
		t.Fatalf("env = %#v", merged.Env)
	}
	if merged.Permissions.DefaultMode != contracts.PermissionPlan {
		t.Fatalf("default mode = %q", merged.Permissions.DefaultMode)
	}
	if len(merged.Permissions.Allow) != 1 || len(merged.Permissions.Deny) != 1 {
		t.Fatalf("permissions = %#v", merged.Permissions)
	}
}

func TestMergeSettingsPreservesSandboxSettings(t *testing.T) {
	merged := MergeSettings(
		contracts.Settings{Sandbox: map[string]any{
			"enabled":                  true,
			"allowUnsandboxedCommands": true,
			"filesystem":               map[string]any{"allowWrite": []any{"tmp"}},
		}},
		contracts.Settings{Sandbox: map[string]any{
			"allowUnsandboxedCommands": false,
			"filesystem":               map[string]any{"denyRead": []any{"private"}},
		}},
	)
	if merged.Sandbox["enabled"] != true || merged.Sandbox["allowUnsandboxedCommands"] != false {
		t.Fatalf("sandbox = %#v", merged.Sandbox)
	}
	filesystem, ok := merged.Sandbox["filesystem"].(map[string]any)
	if !ok || filesystem["allowWrite"] == nil || filesystem["denyRead"] == nil {
		t.Fatalf("sandbox filesystem = %#v", merged.Sandbox["filesystem"])
	}
}

func TestMergeSettingsSourcesHonorsManagedPermissionRulesOnly(t *testing.T) {
	managedOnly := true
	policy := contracts.Settings{
		AllowManagedPermissionRulesOnly: &managedOnly,
		Permissions: &contracts.PermissionsSetting{
			Allow: []string{"Read"},
			Deny:  []string{"Bash(rm *)"},
		},
	}
	user := contracts.Settings{
		Permissions: &contracts.PermissionsSetting{
			Allow:                 []string{"Bash(ls *)"},
			Ask:                   []string{"Write"},
			AdditionalDirectories: []string{"/tmp/work"},
			DefaultMode:           contracts.PermissionPlan,
		},
	}
	merged := MergeSettingsSources(
		SourceSettings{Source: contracts.PermissionSourcePolicySettings, Settings: policy},
		SourceSettings{Source: contracts.PermissionSourceUserSettings, Settings: user},
	)
	if len(merged.Permissions.Allow) != 1 || merged.Permissions.Allow[0] != "Read" {
		t.Fatalf("allow = %#v", merged.Permissions.Allow)
	}
	if len(merged.Permissions.Deny) != 1 || merged.Permissions.Deny[0] != "Bash(rm *)" {
		t.Fatalf("deny = %#v", merged.Permissions.Deny)
	}
	if len(merged.Permissions.Ask) != 0 {
		t.Fatalf("ask = %#v", merged.Permissions.Ask)
	}
	if merged.Permissions.DefaultMode != contracts.PermissionPlan || len(merged.Permissions.AdditionalDirectories) != 1 {
		t.Fatalf("permissions metadata should still merge: %#v", merged.Permissions)
	}
}
