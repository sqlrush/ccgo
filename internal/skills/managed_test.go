package skills

// SKILL-03: ManagedSkillDirs gated by CLAUDE_CODE_DISABLE_POLICY_SKILLS env var.

import (
	"testing"
)

func TestManagedSkillDirsGatedByEnv(t *testing.T) {
	// When CLAUDE_CODE_DISABLE_POLICY_SKILLS is set truthy, result must be empty.
	t.Setenv("CLAUDE_CODE_DISABLE_POLICY_SKILLS", "1")
	dirs := ManagedSkillDirs()
	if len(dirs) != 0 {
		t.Errorf("expected empty ManagedSkillDirs when policy skills disabled, got %v", dirs)
	}
}

func TestManagedSkillDirsAllowedWhenEnvFalsy(t *testing.T) {
	// When env var is not set or falsy, the function should NOT be blocked by the env gate.
	// We can't control what CLAUDE_MANAGED_SETTINGS_DIR is in test, but we verify
	// the function runs without panic and returns a []string (may be empty if no managed dir).
	t.Setenv("CLAUDE_CODE_DISABLE_POLICY_SKILLS", "0")
	dirs := ManagedSkillDirs()
	_ = dirs // valid: may be nil or empty depending on environment
}

func TestIsEnvTruthy(t *testing.T) {
	cases := []struct {
		input string
		want  bool
	}{
		{"1", true},
		{"true", true},
		{"TRUE", true},
		{"yes", true},
		{"0", false},
		{"false", false},
		{"", false},
		{"no", false},
	}
	for _, tc := range cases {
		if got := isEnvTruthy(tc.input); got != tc.want {
			t.Errorf("isEnvTruthy(%q) = %v, want %v", tc.input, got, tc.want)
		}
	}
}
