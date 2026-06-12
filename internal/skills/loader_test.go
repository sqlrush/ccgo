package skills

import (
	"os"
	"path/filepath"
	"testing"

	"ccgo/internal/contracts"
)

func TestLoadSkillDirParsesCommandMetadata(t *testing.T) {
	root := filepath.Join(t.TempDir(), "commit-helper")
	writeSkillFile(t, root, `---
name: Commit Helper
description: Helps commit staged changes
allowed-tools: Bash(git status*), Read, "Bash(git commit -m \"x,y\")"
argument-hint: [scope]
arguments: scope, message
when_to_use: When preparing commits
disable-model-invocation: true
user-invocable: false
paths: src/**, docs/**, **
version: 1.2.3
model: opus
context: fork
agent: review-agent
effort: high
---
Use ${CLAUDE_SKILL_DIR} when preparing commits.
`)

	skill, err := LoadSkillDir(root, contracts.CommandSourceSkills)
	if err != nil {
		t.Fatal(err)
	}

	command := skill.Command
	if command.Type != contracts.CommandPrompt {
		t.Fatalf("type = %q", command.Type)
	}
	if command.Name != "commit-helper" {
		t.Fatalf("name = %q", command.Name)
	}
	if command.DisplayName != "Commit Helper" {
		t.Fatalf("display name = %q", command.DisplayName)
	}
	if command.Description != "Helps commit staged changes" || !command.HasUserSpecifiedDetails {
		t.Fatalf("description = %q, specified = %v", command.Description, command.HasUserSpecifiedDetails)
	}
	if command.ArgumentHint != "[scope]" {
		t.Fatalf("argument hint = %q", command.ArgumentHint)
	}
	if !sameStringSlice(command.ArgumentNames, []string{"scope", "message"}) {
		t.Fatalf("argument names = %#v", command.ArgumentNames)
	}
	if command.Source != contracts.CommandSourceSkills || command.LoadedFrom != "skills" {
		t.Fatalf("source = %q, loaded from = %q", command.Source, command.LoadedFrom)
	}
	if command.SkillRoot != root {
		t.Fatalf("skill root = %q, want %q", command.SkillRoot, root)
	}
	if !command.DisableModelInvocation {
		t.Fatalf("disable model invocation should be true")
	}
	if !command.Hidden || skill.UserInvocable {
		t.Fatalf("hidden = %v, user invocable = %v", command.Hidden, skill.UserInvocable)
	}
	wantTools := []string{"Bash(git status*)", "Read", `Bash(git commit -m \"x,y\")`}
	if !sameStringSlice(command.AllowedTools, wantTools) {
		t.Fatalf("allowed tools = %#v, want %#v", command.AllowedTools, wantTools)
	}
	if command.WhenToUse != "When preparing commits" {
		t.Fatalf("when to use = %q", command.WhenToUse)
	}
	if command.Version != "1.2.3" || command.Model != "opus" {
		t.Fatalf("version/model = %q/%q", command.Version, command.Model)
	}
	if command.Context != "fork" || command.Agent != "review-agent" || command.Effort != "high" {
		t.Fatalf("context/agent/effort = %q/%q/%q", command.Context, command.Agent, command.Effort)
	}
	if !sameStringSlice(skill.Paths, []string{"src", "docs"}) || !sameStringSlice(command.Paths, skill.Paths) {
		t.Fatalf("paths = %#v, command paths = %#v", skill.Paths, command.Paths)
	}
	if command.ContentLength == 0 {
		t.Fatalf("content length should be set")
	}
	if command.ProgressMessage != "running" {
		t.Fatalf("progress message = %q", command.ProgressMessage)
	}
	if want := "Base directory for this skill: " + root + "\n\nUse " + root + " when preparing commits.\n"; skill.Content != want {
		t.Fatalf("content = %q, want %q", skill.Content, want)
	}
}

func TestLoadSkillDirParsesFrontmatterAliasesAndModelInherit(t *testing.T) {
	root := filepath.Join(t.TempDir(), "alias-skill")
	writeSkillFile(t, root, `---
description: Alias skill
allowed_tools: Read, Edit
argument_hint: [target]
disable_model_invocation: true
user_invocable: false
when-to-use: When aliases are used
model: inherit
context: inline
---
Use aliases.
`)

	skill, err := LoadSkillDir(root, contracts.CommandSourceSkills)
	if err != nil {
		t.Fatal(err)
	}
	command := skill.Command
	if command.ArgumentHint != "[target]" {
		t.Fatalf("argument hint = %q", command.ArgumentHint)
	}
	if !sameStringSlice(command.AllowedTools, []string{"Read", "Edit"}) {
		t.Fatalf("allowed tools = %#v", command.AllowedTools)
	}
	if !command.DisableModelInvocation || !command.Hidden || skill.UserInvocable {
		t.Fatalf("disable/hidden/userInvocable = %v/%v/%v", command.DisableModelInvocation, command.Hidden, skill.UserInvocable)
	}
	if command.WhenToUse != "When aliases are used" {
		t.Fatalf("when to use = %q", command.WhenToUse)
	}
	if command.Model != "" {
		t.Fatalf("model inherit should not set override, got %q", command.Model)
	}
	if command.Context != "" {
		t.Fatalf("non-fork context should not be preserved for file skill metadata, got %q", command.Context)
	}
}

func TestLoadSkillDirFiltersNumericArgumentNames(t *testing.T) {
	root := filepath.Join(t.TempDir(), "argument-filter")
	writeSkillFile(t, root, `---
description: test
arguments: 0, target, 12, mode
---
Use $target and $mode.
`)

	skill, err := LoadSkillDir(root, contracts.CommandSourceSkills)
	if err != nil {
		t.Fatal(err)
	}
	if !sameStringSlice(skill.Command.ArgumentNames, []string{"target", "mode"}) {
		t.Fatalf("argument names = %#v", skill.Command.ArgumentNames)
	}
}

func TestLoadSkillDirDescriptionFallback(t *testing.T) {
	root := filepath.Join(t.TempDir(), "fallback")
	writeSkillFile(t, root, `---
allowed-tools: Read
---

# Analyze Logs

Inspect runtime logs.
`)

	skill, err := LoadSkillDir(root, "")
	if err != nil {
		t.Fatal(err)
	}
	if skill.Command.Description != "Analyze Logs" {
		t.Fatalf("description = %q", skill.Command.Description)
	}
	if skill.Command.HasUserSpecifiedDetails {
		t.Fatalf("description should not be marked user-specified")
	}
	if skill.Command.Source != contracts.CommandSourceSkills || skill.Command.LoadedFrom != "skills" {
		t.Fatalf("source = %q, loaded from = %q", skill.Command.Source, skill.Command.LoadedFrom)
	}
}

func TestLoadSkillDirsSkipsInvalidAndPreservesOrder(t *testing.T) {
	dir := t.TempDir()
	first := filepath.Join(dir, "first")
	second := filepath.Join(dir, "second")
	writeSkillFile(t, first, "---\ndescription: first\n---\nfirst\n")
	writeSkillFile(t, second, "---\ndescription: second\n---\nsecond\n")

	got := LoadSkillDirs([]string{first, filepath.Join(dir, "missing"), first, second}, contracts.CommandSourceSkills)
	if len(got) != 2 {
		t.Fatalf("loaded skills = %#v", got)
	}
	if got[0].Command.Name != "first" || got[1].Command.Name != "second" {
		t.Fatalf("loaded order = %q, %q", got[0].Command.Name, got[1].Command.Name)
	}
}

func TestProjectSkillCommandsLoadsDiscoveredSkillDirs(t *testing.T) {
	repo := filepath.Join(t.TempDir(), "repo")
	cwd := filepath.Join(repo, "pkg")
	rootSkill := filepath.Join(repo, ".claude", "skills", "root")
	pkgSkill := filepath.Join(cwd, ".claude", "skills", "pkg")
	writeSkillFile(t, rootSkill, "---\ndescription: root skill\n---\nroot\n")
	writeSkillFile(t, pkgSkill, "---\ndescription: pkg skill\n---\npkg\n")
	if err := os.MkdirAll(filepath.Join(repo, ".git"), 0o755); err != nil {
		t.Fatal(err)
	}

	got := ProjectSkillCommands(cwd)
	if len(got) != 2 {
		t.Fatalf("commands = %#v", got)
	}
	if got[0].Name != "pkg" || got[0].Description != "pkg skill" {
		t.Fatalf("first command = %#v", got[0])
	}
	if got[1].Name != "root" || got[1].Description != "root skill" {
		t.Fatalf("second command = %#v", got[1])
	}
}

func writeSkillFile(t *testing.T, dir string, content string) {
	t.Helper()
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, skillFileName), []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
}
