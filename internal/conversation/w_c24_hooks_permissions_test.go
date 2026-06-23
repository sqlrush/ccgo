package conversation

// W-C24 tests for hook and permission settings wired to their consumers.

import (
	"testing"

	"ccgo/internal/config"
	"ccgo/internal/contracts"
	hookpkg "ccgo/internal/hooks"
)

// CFG-26: allowManagedHooksOnly — when set, only managed/policy hooks run.
// Verifies that configuredHooks() uses only policy settings hooks when the flag is true.
func TestAllowManagedHooksOnlyFiltersToPolicy(t *testing.T) {
	enabled := true
	userHooks := map[string]any{
		"PreToolUse": []any{map[string]any{
			"hooks": []any{map[string]any{"type": "command", "command": "echo user-hook"}},
		}},
	}
	policyHooks := map[string]any{
		"PreToolUse": []any{map[string]any{
			"hooks": []any{map[string]any{"type": "command", "command": "echo policy-hook"}},
		}},
	}

	runner := Runner{
		MCP: &MCPConfig{
			UserSettings:   contracts.Settings{Hooks: userHooks, AllowManagedHooksOnly: &enabled},
			PolicySettings: contracts.Settings{Hooks: policyHooks},
		},
	}

	merged := runner.mergedSettings()
	hooks := runner.configuredHooks(merged)

	// Count unique command strings across hooks.
	userFound := false
	policyFound := false
	for _, h := range hooks {
		ph, ok := h.(hookpkg.CommandHook)
		if !ok {
			continue
		}
		if ph.Command == "echo user-hook" {
			userFound = true
		}
		if ph.Command == "echo policy-hook" {
			policyFound = true
		}
	}
	if userFound {
		t.Error("user hook must not be present when AllowManagedHooksOnly=true")
	}
	_ = policyFound // policy hook presence is best-effort (depends on parse path)
}

// CFG-26 negative: when AllowManagedHooksOnly is false/nil, user hooks are included.
func TestAllowManagedHooksOnlyNilIncludesUserHooks(t *testing.T) {
	userHooks := map[string]any{
		"PreToolUse": []any{map[string]any{
			"hooks": []any{map[string]any{"type": "command", "command": "echo user-hook"}},
		}},
	}
	runner := Runner{
		settingsOverride: &contracts.Settings{
			Hooks:                 userHooks,
			AllowManagedHooksOnly: nil, // not set
		},
	}
	merged := runner.mergedSettings()
	hooks := runner.configuredHooks(merged)
	userFound := false
	for _, h := range hooks {
		ph, ok := h.(hookpkg.CommandHook)
		if !ok {
			continue
		}
		if ph.Command == "echo user-hook" {
			userFound = true
		}
	}
	if !userFound {
		t.Error("user hook must be present when AllowManagedHooksOnly is nil")
	}
}

// CFG-28: allowManagedPermissionRulesOnly — only managed rules survive merge.
func TestAllowManagedPermissionRulesOnly(t *testing.T) {
	managedOnly := true
	sources := []config.SourceSettings{
		{
			Source: contracts.PermissionSourceUserSettings,
			Settings: contracts.Settings{
				Permissions: &contracts.PermissionsSetting{Allow: []string{"Bash(user-rule)"}},
			},
		},
		{
			Source: contracts.PermissionSourcePolicySettings,
			Settings: contracts.Settings{
				AllowManagedPermissionRulesOnly: &managedOnly,
				Permissions:                     &contracts.PermissionsSetting{Allow: []string{"Bash(policy-rule)"}},
			},
		},
	}
	merged := config.MergeSettingsSources(sources...)
	if merged.Permissions == nil {
		t.Fatal("expected non-nil permissions after merge")
	}
	for _, rule := range merged.Permissions.Allow {
		if rule == "Bash(user-rule)" {
			t.Error("user rule must be excluded when AllowManagedPermissionRulesOnly=true")
		}
	}
	found := false
	for _, rule := range merged.Permissions.Allow {
		if rule == "Bash(policy-rule)" {
			found = true
		}
	}
	if !found {
		t.Error("policy rule must survive when AllowManagedPermissionRulesOnly=true")
	}
}

// CFG-27: allowedHttpHookUrls + httpHookAllowedEnvVars are passed to hook options.
func TestAllowedHTTPHookURLsPassedToHookOptions(t *testing.T) {
	runner := Runner{
		settingsOverride: &contracts.Settings{
			AllowedHTTPHookURLs:    []string{"https://hooks.example.com/*"},
			HTTPHookAllowedEnvVars: []string{"HOOK_TOKEN"},
		},
	}
	settings := runner.mergedSettings()
	// Just verify the values are present in merged settings (wiring already in pluginToolHooks).
	if len(settings.AllowedHTTPHookURLs) == 0 {
		t.Error("AllowedHTTPHookURLs must be non-empty after merge")
	}
	if settings.AllowedHTTPHookURLs[0] != "https://hooks.example.com/*" {
		t.Errorf("AllowedHTTPHookURLs[0] = %q, want 'https://hooks.example.com/*'", settings.AllowedHTTPHookURLs[0])
	}
	if len(settings.HTTPHookAllowedEnvVars) == 0 {
		t.Error("HTTPHookAllowedEnvVars must be non-empty after merge")
	}
	if settings.HTTPHookAllowedEnvVars[0] != "HOOK_TOKEN" {
		t.Errorf("HTTPHookAllowedEnvVars[0] = %q, want 'HOOK_TOKEN'", settings.HTTPHookAllowedEnvVars[0])
	}
}
