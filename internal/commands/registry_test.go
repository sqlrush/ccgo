package commands

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"ccgo/internal/contracts"
)

func TestLoadIncludesProjectSkillsBeforeBuiltinsAndFindsDisplayName(t *testing.T) {
	repo := filepath.Join(t.TempDir(), "repo")
	cwd := filepath.Join(repo, "pkg")
	skillDir := filepath.Join(cwd, ".claude", "skills", "deploy")
	writeCommandSkill(t, skillDir, `---
name: Deploy Helper
description: Deploys the service
---
Run deployment checks.
`)
	if err := os.MkdirAll(filepath.Join(repo, ".git"), 0o755); err != nil {
		t.Fatal(err)
	}

	registry := Load(Options{CWD: cwd})
	commands := registry.All()
	if len(commands) < 2 {
		t.Fatalf("commands = %#v", commands)
	}
	if commands[0].Name != "deploy" || commands[0].Source != contracts.CommandSourceSkills {
		t.Fatalf("first command = %#v", commands[0])
	}
	if commands[len(commands)-1].Source != contracts.CommandSourceBuiltin {
		t.Fatalf("last command should be builtin, got %#v", commands[len(commands)-1])
	}
	if cmd, ok := registry.Find("Deploy Helper"); !ok || cmd.Name != "deploy" {
		t.Fatalf("find display name = %#v, %v", cmd, ok)
	}
	if _, ok := registry.Find("help"); !ok {
		t.Fatalf("expected builtin help command")
	}
}

func TestLoadIncludesLegacyProjectCommands(t *testing.T) {
	repo := filepath.Join(t.TempDir(), "repo")
	cwd := filepath.Join(repo, "pkg")
	commandPath := filepath.Join(cwd, ".claude", "commands", "team", "review.md")
	writeCommandMarkdown(t, commandPath, `---
description: Review command
arguments: target
---
Review $target during ${CLAUDE_SESSION_ID}.
`)
	if err := os.MkdirAll(filepath.Join(repo, ".git"), 0o755); err != nil {
		t.Fatal(err)
	}

	registry := Load(Options{CWD: cwd})
	cmd, ok := registry.Find("team:review")
	if !ok {
		t.Fatalf("legacy command not found")
	}
	if cmd.LoadedFrom != loadedFromCommandsDeprecated || cmd.Source != contracts.CommandSourceSkills {
		t.Fatalf("legacy command metadata = %#v", cmd)
	}

	expanded, err := registry.ExpandPrompt("team:review", "auth", "sess_legacy")
	if err != nil {
		t.Fatal(err)
	}
	want := "Review auth during sess_legacy.\n"
	if expanded.Content != want {
		t.Fatalf("expanded content = %q, want %q", expanded.Content, want)
	}
}

func TestLoadIncludesUserLegacyCommands(t *testing.T) {
	configHome := t.TempDir()
	t.Setenv("CLAUDE_CONFIG_DIR", configHome)
	repo := filepath.Join(t.TempDir(), "repo")
	cwd := filepath.Join(repo, "pkg")
	commandPath := filepath.Join(configHome, "commands", "personal.md")
	writeCommandMarkdown(t, commandPath, `---
description: Personal command
arguments: target
---
Personal $target for ${CLAUDE_SESSION_ID}.
`)
	if err := os.MkdirAll(filepath.Join(repo, ".git"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(cwd, 0o755); err != nil {
		t.Fatal(err)
	}

	registry := Load(Options{CWD: cwd})
	cmd, ok := registry.Find("personal")
	if !ok {
		t.Fatalf("user command not found")
	}
	if cmd.LoadedFrom != loadedFromCommandsDeprecated || cmd.Source != contracts.CommandSourceSkills {
		t.Fatalf("user command metadata = %#v", cmd)
	}
	expanded, err := registry.ExpandPrompt("personal", "docs", "sess_user")
	if err != nil {
		t.Fatal(err)
	}
	if expanded.Content != "Personal docs for sess_user.\n" {
		t.Fatalf("expanded content = %q", expanded.Content)
	}
}

func TestLoadIncludesUserSkills(t *testing.T) {
	configHome := t.TempDir()
	t.Setenv("CLAUDE_CONFIG_DIR", configHome)
	repo := filepath.Join(t.TempDir(), "repo")
	cwd := filepath.Join(repo, "pkg")
	skillDir := filepath.Join(configHome, "skills", "personal")
	writeCommandSkill(t, skillDir, `---
name: Personal Skill
description: Personal skill
---
Use personal skill for $ARGUMENTS.
`)
	if err := os.MkdirAll(filepath.Join(repo, ".git"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(cwd, 0o755); err != nil {
		t.Fatal(err)
	}

	registry := Load(Options{CWD: cwd})
	cmd, ok := registry.Find("Personal Skill")
	if !ok {
		t.Fatalf("user skill not found")
	}
	if cmd.Name != "personal" || cmd.Source != contracts.CommandSourceSkills || cmd.LoadedFrom != "skills" {
		t.Fatalf("user skill metadata = %#v", cmd)
	}
	expanded, err := registry.ExpandPrompt("personal", "docs", "sess_user_skill")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(expanded.Content, "Use personal skill for docs.") {
		t.Fatalf("expanded content = %q", expanded.Content)
	}
}

func TestLoadProjectSkillPromptWinsOverUserSkillPrompt(t *testing.T) {
	configHome := t.TempDir()
	t.Setenv("CLAUDE_CONFIG_DIR", configHome)
	repo := filepath.Join(t.TempDir(), "repo")
	cwd := filepath.Join(repo, "pkg")
	projectSkill := filepath.Join(cwd, ".claude", "skills", "deploy")
	userSkill := filepath.Join(configHome, "skills", "deploy")
	writeCommandSkill(t, projectSkill, `---
description: Project deploy
---
Project deploy $ARGUMENTS.
`)
	writeCommandSkill(t, userSkill, `---
description: User deploy
---
User deploy $ARGUMENTS.
`)
	if err := os.MkdirAll(filepath.Join(repo, ".git"), 0o755); err != nil {
		t.Fatal(err)
	}

	registry := Load(Options{CWD: cwd})
	expanded, err := registry.ExpandPrompt("deploy", "api", "sess_project")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(expanded.Content, "Project deploy api.") || strings.Contains(expanded.Content, "User deploy") {
		t.Fatalf("expanded content = %q", expanded.Content)
	}
}

func TestBuiltinCommandsExposeOfficialAliasesAndMetadata(t *testing.T) {
	registry := FromSources(Sources{Builtins: BuiltinCommands()})

	config, ok := registry.Find("settings")
	if !ok || config.Name != "config" {
		t.Fatalf("settings alias = %#v, %v", config, ok)
	}
	resume, ok := registry.Find("continue")
	if !ok || resume.Name != "resume" || resume.ArgumentHint != "[conversation id or search term]" {
		t.Fatalf("continue alias = %#v, %v", resume, ok)
	}
	clear, ok := registry.Find("reset")
	if !ok || clear.Name != "clear" {
		t.Fatalf("reset alias = %#v, %v", clear, ok)
	}
	clear, ok = registry.Find("new")
	if !ok || clear.Name != "clear" {
		t.Fatalf("new alias = %#v, %v", clear, ok)
	}
	mcp, ok := registry.Find("mcp")
	if !ok || !mcp.Immediate || mcp.ArgumentHint != "[enable|disable [server-name]]" {
		t.Fatalf("mcp metadata = %#v, %v", mcp, ok)
	}
	status, ok := registry.Find("status")
	if !ok || !status.Immediate {
		t.Fatalf("status metadata = %#v, %v", status, ok)
	}
	model, ok := registry.Find("model")
	if !ok || !model.Immediate || model.ArgumentHint != "[model]" {
		t.Fatalf("model metadata = %#v, %v", model, ok)
	}
}

func TestFromSourcesUsesCommandOrderAndDedupesDynamicSkills(t *testing.T) {
	registry := FromSources(Sources{
		BundledSkills:       []contracts.Command{promptCommand("bundled", "bundled")},
		BuiltinPluginSkills: []contracts.Command{promptCommand("builtin-plugin", "plugin")},
		ProjectSkills:       []contracts.Command{promptCommand("project", "skills")},
		WorkflowCommands:    []contracts.Command{promptCommand("workflow", "plugin")},
		PluginCommands:      []contracts.Command{promptCommand("plugin-command", "plugin")},
		PluginSkills:        []contracts.Command{promptCommand("plugin-skill", "plugin")},
		DynamicSkills: []contracts.Command{
			promptCommand("project", "skills"),
			promptCommand("dynamic", "skills"),
		},
		Builtins: []contracts.Command{builtinCommand("help")},
	})

	var got []string
	for _, cmd := range registry.All() {
		got = append(got, cmd.Name)
	}
	want := []string{"bundled", "builtin-plugin", "project", "workflow", "plugin-command", "plugin-skill", "dynamic", "help"}
	if !sameCommandNames(got, want) {
		t.Fatalf("order = %#v, want %#v", got, want)
	}
}

func TestLoadDiscoversProjectPluginPromptCommands(t *testing.T) {
	repo := filepath.Join(t.TempDir(), "repo")
	cwd := filepath.Join(repo, "pkg")
	pluginDir := filepath.Join(repo, ".claude", "plugins", "demo")
	if err := os.MkdirAll(filepath.Join(repo, ".git"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(pluginDir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(cwd, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(pluginDir, "plugin.json"), []byte(`{
		"name": "demo",
		"commands": [{
			"name": "plugin:deploy",
			"description": "Deploy plugin",
			"prompt": "Deploy $ARGUMENTS from plugin."
		}]
	}`), 0o644); err != nil {
		t.Fatal(err)
	}

	registry := Load(Options{CWD: cwd})
	command, ok := registry.Find("plugin:deploy")
	if !ok || command.Source != contracts.CommandSourcePlugin || command.LoadedFrom != "plugin" {
		t.Fatalf("plugin command = %#v ok=%v", command, ok)
	}
	expanded, err := registry.ExpandPrompt("plugin:deploy", "api", "sess_plugin")
	if err != nil {
		t.Fatal(err)
	}
	if expanded.Content != "Deploy api from plugin." {
		t.Fatalf("expanded = %q", expanded.Content)
	}
}

func TestLoadSkipsDisabledProjectPluginCommands(t *testing.T) {
	repo := filepath.Join(t.TempDir(), "repo")
	cwd := filepath.Join(repo, "pkg")
	pluginDir := filepath.Join(repo, ".claude", "plugins", "demo")
	if err := os.MkdirAll(filepath.Join(repo, ".git"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(pluginDir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(cwd, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(pluginDir, "plugin.json"), []byte(`{
		"name": "demo",
		"commands": [{
			"name": "demo:deploy",
			"description": "Deploy plugin",
			"prompt": "Deploy $ARGUMENTS."
		}]
	}`), 0o644); err != nil {
		t.Fatal(err)
	}

	registry := Load(Options{
		CWD:      cwd,
		Settings: contracts.Settings{EnabledPlugins: map[string]any{"demo": false}},
	})
	if command, ok := registry.Find("demo:deploy"); ok {
		t.Fatalf("disabled plugin command loaded: %#v", command)
	}
}

func TestLoadDiscoversProjectPluginCommandDirectory(t *testing.T) {
	repo := filepath.Join(t.TempDir(), "repo")
	cwd := filepath.Join(repo, "pkg")
	pluginDir := filepath.Join(repo, ".claude", "plugins", "demo")
	if err := os.MkdirAll(filepath.Join(repo, ".git"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Join(pluginDir, "commands", "team"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(cwd, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(pluginDir, "plugin.json"), []byte(`{"name":"demo"}`), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(pluginDir, "commands", "team", "release.md"), []byte("---\ndescription: Release service\n---\nRelease $ARGUMENTS from ${CLAUDE_SKILL_DIR}."), 0o644); err != nil {
		t.Fatal(err)
	}

	registry := Load(Options{CWD: cwd})
	command, ok := registry.Find("demo:team:release")
	if !ok || command.Source != contracts.CommandSourcePlugin || command.LoadedFrom != "plugin" {
		t.Fatalf("plugin directory command = %#v ok=%v", command, ok)
	}
	expanded, err := registry.ExpandPrompt("demo:team:release", "api", "sess_plugin")
	if err != nil {
		t.Fatal(err)
	}
	want := "Release api from " + filepath.Join(pluginDir, "commands", "team") + "."
	if expanded.Content != want {
		t.Fatalf("expanded = %q, want %q", expanded.Content, want)
	}
}

func TestLoadInjectsPluginUserConfigIntoPromptExpansion(t *testing.T) {
	repo := filepath.Join(t.TempDir(), "repo")
	cwd := filepath.Join(repo, "pkg")
	pluginDir := filepath.Join(repo, ".claude", "plugins", "demo")
	if err := os.MkdirAll(filepath.Join(repo, ".git"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(pluginDir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(cwd, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(pluginDir, "plugin.json"), []byte(`{
		"name": "demo",
		"commands": [{
			"name": "demo:deploy",
			"description": "Deploy plugin",
			"prompt": "Deploy ${user_config.env} to $user_config.region."
		}]
	}`), 0o644); err != nil {
		t.Fatal(err)
	}

	registry := Load(Options{
		CWD: cwd,
		Settings: contracts.Settings{
			PluginConfigs: map[string]contracts.PluginConfig{
				"demo": {Options: map[string]any{"env": "prod", "region": "iad"}},
			},
		},
	})
	expanded, err := registry.ExpandPrompt("demo:deploy", "", "sess_plugin")
	if err != nil {
		t.Fatal(err)
	}
	if expanded.Content != "Deploy prod to iad." {
		t.Fatalf("expanded plugin command = %q", expanded.Content)
	}
}

func TestLoadDiscoversProjectPluginSkillDirectory(t *testing.T) {
	repo := filepath.Join(t.TempDir(), "repo")
	cwd := filepath.Join(repo, "pkg")
	pluginDir := filepath.Join(repo, ".claude", "plugins", "demo")
	if err := os.MkdirAll(filepath.Join(repo, ".git"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Join(pluginDir, "skills", "review"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(cwd, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(pluginDir, "plugin.json"), []byte(`{"name":"demo"}`), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(pluginDir, "skills", "review", "SKILL.md"), []byte("---\ndescription: Review code\n---\nReview $ARGUMENTS from ${CLAUDE_SKILL_DIR}."), 0o644); err != nil {
		t.Fatal(err)
	}

	registry := Load(Options{CWD: cwd})
	command, ok := registry.Find("demo:review")
	if !ok || command.Source != contracts.CommandSourcePlugin || command.LoadedFrom != "plugin" {
		t.Fatalf("plugin skill command = %#v ok=%v", command, ok)
	}
	expanded, err := registry.ExpandPrompt("demo:review", "api", "sess_plugin")
	if err != nil {
		t.Fatal(err)
	}
	skillRoot := filepath.Join(pluginDir, "skills", "review")
	want := "Base directory for this skill: " + skillRoot + "\n\nReview api from " + skillRoot + "."
	if expanded.Content != want {
		t.Fatalf("expanded = %q, want %q", expanded.Content, want)
	}
}

func TestSkillToolCommandsMatchesModelInvocablePromptSkills(t *testing.T) {
	commands := []contracts.Command{
		builtinCommand("help"),
		promptCommand("project-no-frontmatter-description", "skills"),
		{Name: "plugin-undescribed", Type: contracts.CommandPrompt, Source: contracts.CommandSourcePlugin, LoadedFrom: "plugin"},
		{Name: "plugin-when", Type: contracts.CommandPrompt, Source: contracts.CommandSourcePlugin, LoadedFrom: "plugin", WhenToUse: "Use for plugin work"},
		{Name: "disabled", Type: contracts.CommandPrompt, Source: contracts.CommandSourceSkills, LoadedFrom: "skills", DisableModelInvocation: true, HasUserSpecifiedDetails: true},
	}

	got := SkillToolCommands(commands)
	var names []string
	for _, cmd := range got {
		names = append(names, cmd.Name)
	}
	want := []string{"project-no-frontmatter-description", "plugin-when"}
	if !sameCommandNames(names, want) {
		t.Fatalf("skill tool commands = %#v, want %#v", names, want)
	}
}

func TestSlashCommandToolSkillsRequiresDescriptionOrWhenToUse(t *testing.T) {
	commands := []contracts.Command{
		promptCommand("project-fallback-description", "skills"),
		{Name: "project-frontmatter-description", Type: contracts.CommandPrompt, Source: contracts.CommandSourceSkills, LoadedFrom: "skills", HasUserSpecifiedDetails: true},
		{Name: "plugin-when", Type: contracts.CommandPrompt, Source: contracts.CommandSourcePlugin, LoadedFrom: "plugin", WhenToUse: "Use for plugin work"},
		{Name: "disabled-with-description", Type: contracts.CommandPrompt, Source: contracts.CommandSourceSkills, LoadedFrom: "skills", HasUserSpecifiedDetails: true, DisableModelInvocation: true},
	}

	got := SlashCommandToolSkills(commands)
	var names []string
	for _, cmd := range got {
		names = append(names, cmd.Name)
	}
	want := []string{"project-frontmatter-description", "plugin-when", "disabled-with-description"}
	if !sameCommandNames(names, want) {
		t.Fatalf("slash command tool skills = %#v, want %#v", names, want)
	}
}

func TestRegistryReturnsClonedCommandSlices(t *testing.T) {
	registry := FromSources(Sources{
		ProjectSkills: []contracts.Command{{
			Name:         "skill",
			Type:         contracts.CommandPrompt,
			Source:       contracts.CommandSourceSkills,
			LoadedFrom:   "skills",
			Aliases:      []string{"s"},
			AllowedTools: []string{"Read"},
		}},
	})

	first := registry.All()
	first[0].Aliases[0] = "mutated"
	first[0].AllowedTools[0] = "Write"

	second := registry.All()
	if second[0].Aliases[0] != "s" || second[0].AllowedTools[0] != "Read" {
		t.Fatalf("registry returned mutable command internals: %#v", second[0])
	}
}

func TestIsBridgeSafeCommand(t *testing.T) {
	if !IsBridgeSafeCommand(promptCommand("skill", "skills")) {
		t.Fatalf("prompt commands should be bridge-safe")
	}
	if IsBridgeSafeCommand(contracts.Command{Name: "help", Type: contracts.CommandLocalJSX}) {
		t.Fatalf("local-jsx commands should not be bridge-safe")
	}
	if !IsBridgeSafeCommand(contracts.Command{Name: "compact", Type: contracts.CommandLocal}) {
		t.Fatalf("compact should be bridge-safe")
	}
	if IsBridgeSafeCommand(contracts.Command{Name: "model", Type: contracts.CommandLocal}) {
		t.Fatalf("model should not be bridge-safe without explicit allowlist")
	}
}

func writeCommandSkill(t *testing.T, dir string, content string) {
	t.Helper()
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "SKILL.md"), []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
}

func writeCommandMarkdown(t *testing.T, path string, content string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
}

func promptCommand(name string, loadedFrom string) contracts.Command {
	source := contracts.CommandSourceSkills
	if loadedFrom == "plugin" {
		source = contracts.CommandSourcePlugin
	}
	if loadedFrom == "bundled" {
		source = contracts.CommandSourceBundled
	}
	return contracts.Command{
		Name:       name,
		Type:       contracts.CommandPrompt,
		Source:     source,
		LoadedFrom: loadedFrom,
	}
}

func builtinCommand(name string) contracts.Command {
	return contracts.Command{
		Name:   name,
		Type:   contracts.CommandLocalJSX,
		Source: contracts.CommandSourceBuiltin,
	}
}

func sameCommandNames(a []string, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}
