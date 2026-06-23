package skills

// SKILL-23: bundled skills tests.
// Verifies registerBundledSkill, GetBundledSkills, ClearBundledSkills.
// CC ref: skills/bundledSkills.ts:registerBundledSkill / getBundledSkills.

import (
	"testing"

	"ccgo/internal/contracts"
)

func TestRegisterAndGetBundledSkill(t *testing.T) {
	ClearBundledSkills()
	defer ClearBundledSkills()

	RegisterBundledSkill(BundledSkillDefinition{
		Name:        "test-skill",
		Description: "A test skill",
		WhenToUse:   "when testing",
	})

	got := GetBundledSkills()
	if len(got) != 1 {
		t.Fatalf("expected 1 bundled skill, got %d", len(got))
	}
	if got[0].Name != "test-skill" {
		t.Errorf("Name = %q, want %q", got[0].Name, "test-skill")
	}
	if got[0].Description != "A test skill" {
		t.Errorf("Description = %q, want %q", got[0].Description, "A test skill")
	}
	if got[0].WhenToUse != "when testing" {
		t.Errorf("WhenToUse = %q, want %q", got[0].WhenToUse, "when testing")
	}
	if got[0].Source != contracts.CommandSourceBundled {
		t.Errorf("Source = %q, want %q", got[0].Source, contracts.CommandSourceBundled)
	}
	if got[0].LoadedFrom != "bundled" {
		t.Errorf("LoadedFrom = %q, want %q", got[0].LoadedFrom, "bundled")
	}
}

func TestGetBundledSkillsReturnsCopy(t *testing.T) {
	ClearBundledSkills()
	defer ClearBundledSkills()

	RegisterBundledSkill(BundledSkillDefinition{Name: "skill-a", Description: "A"})
	first := GetBundledSkills()
	RegisterBundledSkill(BundledSkillDefinition{Name: "skill-b", Description: "B"})
	second := GetBundledSkills()

	// first copy must not be affected by the second registration.
	if len(first) != 1 {
		t.Errorf("first copy length = %d, want 1", len(first))
	}
	if len(second) != 2 {
		t.Errorf("second copy length = %d, want 2", len(second))
	}
}

func TestClearBundledSkills(t *testing.T) {
	ClearBundledSkills()
	RegisterBundledSkill(BundledSkillDefinition{Name: "x", Description: "X"})
	if len(GetBundledSkills()) != 1 {
		t.Fatal("expected 1 skill after register")
	}
	ClearBundledSkills()
	if len(GetBundledSkills()) != 0 {
		t.Fatal("expected 0 skills after clear")
	}
}

func TestBundledSkillUserInvocable(t *testing.T) {
	ClearBundledSkills()
	defer ClearBundledSkills()

	notInvocable := false
	RegisterBundledSkill(BundledSkillDefinition{
		Name:          "hidden-skill",
		Description:   "Hidden",
		UserInvocable: &notInvocable,
	})

	got := GetBundledSkills()
	if len(got) != 1 {
		t.Fatalf("expected 1 skill, got %d", len(got))
	}
	if !got[0].Hidden {
		t.Errorf("expected Hidden=true for non-user-invocable skill")
	}
}

func TestBundledSkillAliases(t *testing.T) {
	ClearBundledSkills()
	defer ClearBundledSkills()

	RegisterBundledSkill(BundledSkillDefinition{
		Name:        "my-skill",
		Description: "My skill",
		Aliases:     []string{"ms", "myskill"},
	})

	got := GetBundledSkills()
	if len(got) != 1 {
		t.Fatalf("expected 1 skill, got %d", len(got))
	}
	if len(got[0].Aliases) != 2 {
		t.Errorf("expected 2 aliases, got %d: %v", len(got[0].Aliases), got[0].Aliases)
	}
}
