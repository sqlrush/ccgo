package skills

// SKILL-03: ManagedSkillDirs respects CLAUDE_CODE_DISABLE_POLICY_SKILLS env gate.

import (
	"testing"
)

func TestManagedSkillDirsRespectDisableEnvGate(t *testing.T) {
	// With CLAUDE_CODE_DISABLE_POLICY_SKILLS=1, ManagedSkillDirs must return nil.
	t.Setenv("CLAUDE_CODE_DISABLE_POLICY_SKILLS", "1")
	dirs := ManagedSkillDirs()
	if len(dirs) != 0 {
		t.Errorf("ManagedSkillDirs() = %v, want nil when CLAUDE_CODE_DISABLE_POLICY_SKILLS=1", dirs)
	}
}

func TestManagedSkillDirsDisabledFalsy(t *testing.T) {
	// With CLAUDE_CODE_DISABLE_POLICY_SKILLS=0 (falsy), ManagedSkillDirs must
	// not be suppressed by the env gate (may still return nil if managed dir absent).
	t.Setenv("CLAUDE_CODE_DISABLE_POLICY_SKILLS", "0")
	// No assertion on content — managed dir likely absent in test env.
	// Just ensure it doesn't panic.
	_ = ManagedSkillDirs()
}

func TestManagedSkillDirsVariousTruthyValues(t *testing.T) {
	for _, val := range []string{"true", "yes", "on"} {
		t.Run(val, func(t *testing.T) {
			t.Setenv("CLAUDE_CODE_DISABLE_POLICY_SKILLS", val)
			dirs := ManagedSkillDirs()
			if len(dirs) != 0 {
				t.Errorf("ManagedSkillDirs() = %v, want nil when CLAUDE_CODE_DISABLE_POLICY_SKILLS=%q", dirs, val)
			}
		})
	}
}
