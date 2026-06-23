package skills

// SKILL-21: skill frontmatter hooks parsing.
// When a SKILL.md has a hooks: JSON frontmatter key, the parsed map
// must be available on Command.Hooks.
// CC ref: skills/loadSkillsDir.ts:parseHooksFromFrontmatter.
// ccgo uses JSON-encoded hooks in frontmatter (YAML is a superset of JSON,
// so this is valid in both YAML and JSON contexts).

import (
	"os"
	"path/filepath"
	"testing"
)

func TestLoadSkillDirParsesHooksFrontmatter(t *testing.T) {
	dir := t.TempDir()
	// JSON-encoded hooks value (valid YAML inline scalar).
	content := `---
description: Test skill with hooks
hooks: {"PreToolUse":[{"matcher":"Bash","hooks":[{"type":"command","command":"echo pre"}]}]}
---
# Test skill
Do something.
`
	if err := os.WriteFile(filepath.Join(dir, "SKILL.md"), []byte(content), 0o644); err != nil {
		t.Fatalf("write SKILL.md: %v", err)
	}

	skill, err := LoadSkillDir(dir, "")
	if err != nil {
		t.Fatalf("LoadSkillDir: %v", err)
	}

	if skill.Command.Hooks == nil {
		t.Fatal("expected Command.Hooks to be non-nil")
	}
	if _, ok := skill.Command.Hooks["PreToolUse"]; !ok {
		t.Errorf("expected PreToolUse key in Hooks, got: %v", skill.Command.Hooks)
	}
}

func TestLoadSkillDirNoHooksKey(t *testing.T) {
	dir := t.TempDir()
	content := "---\ndescription: No hooks\n---\n# Test\nDo something."
	if err := os.WriteFile(filepath.Join(dir, "SKILL.md"), []byte(content), 0o644); err != nil {
		t.Fatalf("write SKILL.md: %v", err)
	}

	skill, err := LoadSkillDir(dir, "")
	if err != nil {
		t.Fatalf("LoadSkillDir: %v", err)
	}

	if skill.Command.Hooks != nil {
		t.Errorf("expected nil Hooks when no hooks key, got: %v", skill.Command.Hooks)
	}
}

func TestLoadSkillDirInvalidHooksIgnored(t *testing.T) {
	dir := t.TempDir()
	// Invalid JSON for hooks value — should be silently ignored.
	content := "---\ndescription: Bad hooks\nhooks: not-valid-json\n---\n# Test\nDo something."
	if err := os.WriteFile(filepath.Join(dir, "SKILL.md"), []byte(content), 0o644); err != nil {
		t.Fatalf("write SKILL.md: %v", err)
	}

	skill, err := LoadSkillDir(dir, "")
	if err != nil {
		t.Fatalf("LoadSkillDir: %v", err)
	}
	// Invalid hooks must be silently dropped — skill should still load.
	if skill.Command.Hooks != nil {
		t.Errorf("expected nil Hooks for invalid JSON, got: %v", skill.Command.Hooks)
	}
}
