package commands

// SKILL-04: --add-dir extra directory skill discovery.
// When settings.Permissions.AdditionalDirectories contains a path, skills
// under that path's .claude/skills/ subdirectory must also be discovered.
// CC ref: src/utils/skills/skillChangeDetector.ts:223-234.

import (
	"os"
	"path/filepath"
	"testing"

	"ccgo/internal/contracts"
)

func TestLoadProjectSkillPromptsDiscoversAddDirSkills(t *testing.T) {
	// Set up a temporary --add-dir directory with a skill inside.
	addDir := t.TempDir()
	skillsDir := filepath.Join(addDir, ".claude", "skills", "extra-skill")
	if err := os.MkdirAll(skillsDir, 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	if err := os.WriteFile(filepath.Join(skillsDir, "SKILL.md"), []byte("---\ndescription: Extra skill\n---\n# Extra skill\nDo extra things."), 0o644); err != nil {
		t.Fatalf("write SKILL.md: %v", err)
	}

	settings := contracts.Settings{
		Permissions: &contracts.PermissionsSetting{
			AdditionalDirectories: []string{addDir},
		},
	}

	prompts := loadProjectSkillPromptsWithSettings(t.TempDir(), settings)

	found := false
	for _, p := range prompts {
		if p.Command.Name == "extra-skill" {
			found = true
			break
		}
	}
	if !found {
		names := make([]string, 0, len(prompts))
		for _, p := range prompts {
			names = append(names, p.Command.Name)
		}
		t.Errorf("extra-skill not found in prompts; got: %v", names)
	}
}
