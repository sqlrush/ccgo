package config

import (
	"testing"

	"ccgo/internal/contracts"
)

func TestIsRestrictedToPluginOnly(t *testing.T) {
	if !IsRestrictedToPluginOnly(contracts.Settings{StrictPluginOnlyCustomization: true}, CustomizationSurfaceSkills) {
		t.Fatal("true policy should restrict all surfaces")
	}
	if !IsRestrictedToPluginOnly(contracts.Settings{StrictPluginOnlyCustomization: []any{"hooks", "mcp"}}, CustomizationSurfaceMCP) {
		t.Fatal("array policy should restrict listed surface")
	}
	if IsRestrictedToPluginOnly(contracts.Settings{StrictPluginOnlyCustomization: []string{"hooks"}}, CustomizationSurfaceSkills) {
		t.Fatal("array policy should not restrict unlisted surface")
	}
	if IsRestrictedToPluginOnly(contracts.Settings{StrictPluginOnlyCustomization: "skills"}, CustomizationSurfaceSkills) {
		t.Fatal("invalid policy value should not restrict surfaces")
	}
}

func TestIsAdminTrustedCustomizationSource(t *testing.T) {
	for _, source := range []string{"plugin", "policySettings", "built-in", "builtin", "bundled"} {
		if !IsAdminTrustedCustomizationSource(source) {
			t.Fatalf("source %q should be trusted", source)
		}
	}
	for _, source := range []string{"", "skills", "projectSettings", "userSettings", "localSettings", "mcp"} {
		if IsAdminTrustedCustomizationSource(source) {
			t.Fatalf("source %q should not be trusted", source)
		}
	}
}
