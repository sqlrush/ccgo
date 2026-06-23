// W-C24 tests — parsed-but-unused settings keys wired to their consumers.
// Tests are in package main so they can call the unexported helper functions
// (checkAvailableModels, applyModelOverrides, checkMinimumVersion).
package main

import (
	"testing"
)

// CFG-07: availableModels — model whitelist enforcement.

func TestCheckAvailableModelsEmpty(t *testing.T) {
	// Empty list means no restriction.
	if err := checkAvailableModels("claude-opus-4-5", nil); err != nil {
		t.Errorf("expected nil error for empty availableModels, got %v", err)
	}
}

func TestCheckAvailableModelsAllowed(t *testing.T) {
	allowed := []string{"claude-haiku-4-5", "claude-sonnet-4-5"}
	if err := checkAvailableModels("claude-haiku-4-5", allowed); err != nil {
		t.Errorf("expected nil error for allowed model, got %v", err)
	}
}

func TestCheckAvailableModelsBlocked(t *testing.T) {
	allowed := []string{"claude-haiku-4-5"}
	if err := checkAvailableModels("claude-opus-4-5", allowed); err == nil {
		t.Error("expected error when model not in availableModels list")
	}
}

func TestCheckAvailableModelsCaseInsensitive(t *testing.T) {
	allowed := []string{"Claude-Haiku-4-5"}
	if err := checkAvailableModels("claude-haiku-4-5", allowed); err != nil {
		t.Errorf("expected case-insensitive match; got %v", err)
	}
}

// CFG-08: modelOverrides — model ID remapping (e.g. to Bedrock ARN).

func TestApplyModelOverridesNoOverrides(t *testing.T) {
	got := applyModelOverrides("claude-opus-4-5", nil)
	if got != "claude-opus-4-5" {
		t.Errorf("applyModelOverrides with nil overrides = %q, want original", got)
	}
}

func TestApplyModelOverridesMatch(t *testing.T) {
	overrides := map[string]string{"claude-opus-4-5": "arn:aws:bedrock:us-east-1::foundation-model/anthropic.claude-opus"}
	got := applyModelOverrides("claude-opus-4-5", overrides)
	if got != "arn:aws:bedrock:us-east-1::foundation-model/anthropic.claude-opus" {
		t.Errorf("applyModelOverrides = %q, want ARN", got)
	}
}

func TestApplyModelOverridesNoMatch(t *testing.T) {
	overrides := map[string]string{"claude-opus-4-5": "arn:bedrock:xxx"}
	got := applyModelOverrides("claude-haiku-4-5", overrides)
	if got != "claude-haiku-4-5" {
		t.Errorf("applyModelOverrides with no match = %q, want original", got)
	}
}

func TestApplyModelOverridesCaseInsensitive(t *testing.T) {
	overrides := map[string]string{"CLAUDE-HAIKU-4-5": "bedrock-arn"}
	got := applyModelOverrides("claude-haiku-4-5", overrides)
	if got != "bedrock-arn" {
		t.Errorf("applyModelOverrides case-insensitive = %q, want 'bedrock-arn'", got)
	}
}

// CFG-46: minimumVersion — version gate enforcement.

func TestCheckMinimumVersionEmpty(t *testing.T) {
	if err := checkMinimumVersion(""); err != nil {
		t.Errorf("expected nil error for empty minimumVersion, got %v", err)
	}
}

func TestCheckMinimumVersionSatisfied(t *testing.T) {
	// compareSemver with equal versions returns 0 → not below minimum.
	if got := compareSemver("1.5.0", "1.5.0"); got != 0 {
		t.Errorf("compareSemver(1.5.0, 1.5.0) = %d, want 0", got)
	}
}

func TestCheckMinimumVersionFuture(t *testing.T) {
	// A very high minimum version should fail (no real version will satisfy 999.0.0).
	if err := checkMinimumVersion("999.0.0"); err == nil {
		t.Error("expected error when minimumVersion is 999.0.0 (higher than any real version)")
	}
}

func TestCheckMinimumVersionVPrefix(t *testing.T) {
	// "v" prefix must be stripped: compareSemver of dev build (0.0.0-dev) < 999.0.0
	// but we test the stripPrefix logic indirectly: v999.0.0 and 999.0.0 must behave identically.
	err1 := checkMinimumVersion("999.0.0")
	err2 := checkMinimumVersion("v999.0.0")
	if (err1 == nil) != (err2 == nil) {
		t.Errorf("v-prefix and no-v-prefix gave different error values: %v vs %v", err1, err2)
	}
}

// compareSemver helper.

func TestCompareSemverEqual(t *testing.T) {
	if got := compareSemver("1.2.3", "1.2.3"); got != 0 {
		t.Errorf("compareSemver(1.2.3, 1.2.3) = %d, want 0", got)
	}
}

func TestCompareSemverLess(t *testing.T) {
	if got := compareSemver("1.2.3", "1.2.4"); got >= 0 {
		t.Errorf("compareSemver(1.2.3, 1.2.4) = %d, want < 0", got)
	}
}

func TestCompareSemverGreater(t *testing.T) {
	if got := compareSemver("2.0.0", "1.9.9"); got <= 0 {
		t.Errorf("compareSemver(2.0.0, 1.9.9) = %d, want > 0", got)
	}
}
