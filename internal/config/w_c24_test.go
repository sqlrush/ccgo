package config

// W-C24 tests: verify settings keys are properly merged and exposed.

import (
	"testing"

	"ccgo/internal/contracts"
)

// CFG-07: availableModels merge (lists are appended from all layers).
func TestMergeSettingsAvailableModels(t *testing.T) {
	base := contracts.Settings{AvailableModels: []string{"model-a"}}
	overlay := contracts.Settings{AvailableModels: []string{"model-b"}}
	merged := MergeSettings(base, overlay)
	// Both layers contribute; merged list must contain entries from both.
	found := map[string]bool{}
	for _, m := range merged.AvailableModels {
		found[m] = true
	}
	if !found["model-a"] || !found["model-b"] {
		t.Errorf("expected both 'model-a' and 'model-b' in AvailableModels, got %v", merged.AvailableModels)
	}
}

// CFG-08: modelOverrides merge (map[string]string).
func TestMergeSettingsModelOverrides(t *testing.T) {
	overlay := contracts.Settings{ModelOverrides: map[string]string{"large": "big-model"}}
	merged := MergeSettings(contracts.Settings{}, overlay)
	if len(merged.ModelOverrides) == 0 {
		t.Fatal("expected ModelOverrides to be non-empty after merge")
	}
	if merged.ModelOverrides["large"] != "big-model" {
		t.Errorf("ModelOverrides[large] = %q, want 'big-model'", merged.ModelOverrides["large"])
	}
}

// CFG-44: claudeMdExcludes merge.
func TestMergeSettingsClaudeMdExcludes(t *testing.T) {
	overlay := contracts.Settings{ClaudeMdExcludes: []string{"*.secret", "private/"}}
	merged := MergeSettings(contracts.Settings{}, overlay)
	if len(merged.ClaudeMdExcludes) != 2 {
		t.Fatalf("expected 2 ClaudeMdExcludes, got %d: %v", len(merged.ClaudeMdExcludes), merged.ClaudeMdExcludes)
	}
}

// CFG-46: minimumVersion merge.
func TestMergeSettingsMinimumVersion(t *testing.T) {
	overlay := contracts.Settings{MinimumVersion: "1.2.3"}
	merged := MergeSettings(contracts.Settings{}, overlay)
	if merged.MinimumVersion != "1.2.3" {
		t.Errorf("MinimumVersion = %q, want '1.2.3'", merged.MinimumVersion)
	}
}

// CFG-42: autoMemoryEnabled + autoMemoryDirectory merge.
func TestMergeSettingsAutoMemory(t *testing.T) {
	enabled := true
	overlay := contracts.Settings{
		AutoMemoryEnabled:   &enabled,
		AutoMemoryDirectory: "/tmp/memory",
	}
	merged := MergeSettings(contracts.Settings{}, overlay)
	if merged.AutoMemoryEnabled == nil || !*merged.AutoMemoryEnabled {
		t.Error("expected AutoMemoryEnabled=true after merge")
	}
	if merged.AutoMemoryDirectory != "/tmp/memory" {
		t.Errorf("AutoMemoryDirectory = %q, want '/tmp/memory'", merged.AutoMemoryDirectory)
	}
}

// CFG-43: plansDirectory merge.
func TestMergeSettingsPlansDirectory(t *testing.T) {
	overlay := contracts.Settings{PlansDirectory: "/tmp/plans"}
	merged := MergeSettings(contracts.Settings{}, overlay)
	if merged.PlansDirectory != "/tmp/plans" {
		t.Errorf("PlansDirectory = %q, want '/tmp/plans'", merged.PlansDirectory)
	}
}

// CFG-53: verbose merge.
func TestMergeSettingsVerbose(t *testing.T) {
	v := true
	overlay := contracts.Settings{Verbose: &v}
	merged := MergeSettings(contracts.Settings{}, overlay)
	if merged.Verbose == nil || !*merged.Verbose {
		t.Error("expected Verbose=true after merge")
	}
}
