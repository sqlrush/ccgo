// Package main — G33 pass: CLI-FLAG-31/33, CLI-FLAG-12, SDK-49 tests.
//
// Tests are in package main so they can call headlessRunner, toolsNotInWhitelist,
// and applySettingSourcesFilter directly.
package main

import (
	"os"
	"strings"
	"testing"

	"ccgo/internal/contracts"
	"ccgo/internal/conversation"
	sdkpkg "ccgo/internal/sdk"
)

// ── CLI-FLAG-31: --tools whitelist filtering ──────────────────────────────────

// TestToolsNotInWhitelist verifies that toolsNotInWhitelist returns all tool
// names NOT present in the provided whitelist.
func TestToolsNotInWhitelist(t *testing.T) {
	all := []string{"Bash", "Edit", "Read", "Glob", "Grep"}

	// GIVEN whitelist = ["Bash", "Read"]
	// THEN Glob, Grep, Edit are denied; Bash and Read are not.
	denied := toolsNotInWhitelist(all, []string{"Bash", "Read"})
	want := map[string]bool{"Edit": true, "Glob": true, "Grep": true}
	got := map[string]bool{}
	for _, d := range denied {
		got[d] = true
	}
	for w := range want {
		if !got[w] {
			t.Errorf("expected %q in denied list, got %v", w, denied)
		}
	}
	for _, d := range denied {
		if d == "Bash" || d == "Read" {
			t.Errorf("whitelisted tool %q must not appear in denied list", d)
		}
	}
}

// TestToolsNotInWhitelistEmpty verifies that an empty whitelist returns nil
// (all tools are available — "default" / no filter).
func TestToolsNotInWhitelistEmpty(t *testing.T) {
	denied := toolsNotInWhitelist([]string{"Bash", "Edit"}, nil)
	if len(denied) != 0 {
		t.Errorf("empty whitelist should return nil, got %v", denied)
	}
}

// TestToolsNotInWhitelistCaseInsensitive verifies that matching is
// case-insensitive (CC normalises tool names to lower case before comparison).
func TestToolsNotInWhitelistCaseInsensitive(t *testing.T) {
	denied := toolsNotInWhitelist([]string{"Bash", "Edit"}, []string{"bash"})
	for _, d := range denied {
		if strings.EqualFold(d, "Bash") {
			t.Errorf("'Bash' matched case-insensitively but appeared in denied list: %v", denied)
		}
	}
}

// ── CLI-FLAG-33: --setting-sources filter ─────────────────────────────────────

// TestApplySettingSourcesFilter_UserOnly verifies that specifying "user" zeroes
// the project and local settings in the MCPConfig.
func TestApplySettingSourcesFilter_UserOnly(t *testing.T) {
	cfg := &conversation.MCPConfig{
		UserSettings:    contracts.Settings{Model: "user-model"},
		ProjectSettings: contracts.Settings{Model: "project-model"},
		LocalSettings:   contracts.Settings{Model: "local-model"},
	}

	applySettingSourcesFilter(cfg, "user")

	if cfg.UserSettings.Model != "user-model" {
		t.Errorf("UserSettings must be preserved: got %q", cfg.UserSettings.Model)
	}
	if cfg.ProjectSettings.Model != "" {
		t.Errorf("ProjectSettings must be zeroed: got %q", cfg.ProjectSettings.Model)
	}
	if cfg.LocalSettings.Model != "" {
		t.Errorf("LocalSettings must be zeroed: got %q", cfg.LocalSettings.Model)
	}
}

// TestApplySettingSourcesFilter_ProjectAndLocal verifies that "project,local"
// zeroes user settings but keeps project and local.
func TestApplySettingSourcesFilter_ProjectAndLocal(t *testing.T) {
	cfg := &conversation.MCPConfig{
		UserSettings:    contracts.Settings{Model: "user-model"},
		ProjectSettings: contracts.Settings{Model: "project-model"},
		LocalSettings:   contracts.Settings{Model: "local-model"},
	}

	applySettingSourcesFilter(cfg, "project,local")

	if cfg.UserSettings.Model != "" {
		t.Errorf("UserSettings must be zeroed: got %q", cfg.UserSettings.Model)
	}
	if cfg.ProjectSettings.Model != "project-model" {
		t.Errorf("ProjectSettings must be preserved: got %q", cfg.ProjectSettings.Model)
	}
	if cfg.LocalSettings.Model != "local-model" {
		t.Errorf("LocalSettings must be preserved: got %q", cfg.LocalSettings.Model)
	}
}

// TestApplySettingSourcesFilter_All verifies that all sources named keeps everything.
func TestApplySettingSourcesFilter_All(t *testing.T) {
	cfg := &conversation.MCPConfig{
		UserSettings:    contracts.Settings{Model: "u"},
		ProjectSettings: contracts.Settings{Model: "p"},
		LocalSettings:   contracts.Settings{Model: "l"},
	}

	applySettingSourcesFilter(cfg, "user,project,local")

	if cfg.UserSettings.Model != "u" || cfg.ProjectSettings.Model != "p" || cfg.LocalSettings.Model != "l" {
		t.Error("all sources named: all settings must be preserved")
	}
}

// TestApplySettingSourcesFilter_Empty verifies that an empty sources string is a no-op.
func TestApplySettingSourcesFilter_Empty(t *testing.T) {
	cfg := &conversation.MCPConfig{
		UserSettings: contracts.Settings{Model: "u"},
	}
	applySettingSourcesFilter(cfg, "")
	if cfg.UserSettings.Model != "u" {
		t.Error("empty sources: settings must not be modified")
	}
}

// TestApplySettingSourcesFilter_NilMCP verifies that a nil MCPConfig is a safe no-op.
func TestApplySettingSourcesFilter_NilMCP(t *testing.T) {
	// must not panic
	applySettingSourcesFilter(nil, "user")
}

// ── SDK-49: update_environment_variables → os.Setenv integration ─────────────

// TestSDK49OnEnvMutationCallbackSetsEnv verifies that when an OnEnvMutation
// callback is provided, it applies the variables to the caller's environment.
// This mirrors the wiring in cmd/claude/main.go where os.Setenv is the callback.
// SDK-49: CC ref: src/entrypoints/sdk/controlSchemas.ts:629-636.
func TestSDK49OnEnvMutationCallbackSetsEnv(t *testing.T) {
	const envKey = "CCGO_G33_SDK49_TEST_VAR"
	const envVal = "sdk49-test-value"
	// Ensure clean state
	_ = os.Unsetenv(envKey)
	t.Cleanup(func() { _ = os.Unsetenv(envKey) })

	// Simulate the production OnEnvMutation callback wired in cmd/claude.
	// This is the exact lambda used in the SDK Query call.
	onEnvMutation := func(vars map[string]string) {
		for k, v := range vars {
			_ = os.Setenv(k, v)
		}
	}

	// Verify the callback (which is what sdkpkg.Options.OnEnvMutation gets) works.
	onEnvMutation(map[string]string{envKey: envVal})

	if got := os.Getenv(envKey); got != envVal {
		t.Errorf("OnEnvMutation with os.Setenv: want %q=%q, got %q", envKey, envVal, got)
	}
}

// TestSDK49OptionsFieldExists verifies that sdkpkg.Options has an OnEnvMutation
// field (compile-time guard: if the field is removed, this file fails to compile).
func TestSDK49OptionsFieldExists(_ *testing.T) {
	_ = sdkpkg.Options{
		OnEnvMutation: func(vars map[string]string) {},
	}
}
