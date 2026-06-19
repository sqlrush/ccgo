package plugins

import (
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"ccgo/internal/contracts"
)

func TestLoadPluginDirLoadsPromptCommandsAndSkills(t *testing.T) {
	root := filepath.Join(t.TempDir(), "demo")
	if err := os.MkdirAll(filepath.Join(root, "skills", "audit"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Join(root, "agents"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Join(root, "hooks"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "prompt.md"), []byte("Run plugin prompt for $ARGUMENTS."), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "skills", "audit", "SKILL.md"), []byte("---\ndescription: Audit code\nallowed-tools: Read\n---\nAudit ${CLAUDE_SKILL_DIR}."), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "agents", "review.md"), []byte("---\nname: reviewer\ndescription: Review changes\nmodel: opus\npermissionMode: bypassPermissions\ntools: Read, Bash(git commit -m \"x,y\"), Edit\n---\nReview."), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "extra-agent.md"), []byte("# Extra agent\nHelp with extra tasks."), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, ".mcp.json"), []byte(`{
		"mcpServers": {
			"plugin:default": {
				"command": "default-mcp"
			}
		}
	}`), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "mcp-extra.json"), []byte(`{
		"mcpServers": {
			"plugin:extra": {
				"type": "http",
				"url": "https://extra.example/mcp"
			}
		}
	}`), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "hooks", "hooks.json"), []byte(`{
		"hooks": {
			"PreToolUse": [{
				"matcher": "Bash",
				"hooks": [{"type": "command", "command": "echo pre"}]
			}]
		}
	}`), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, ManifestFileName), []byte(`{
		"name": "demo",
		"version": "1.2.3",
		"description": "Demo plugin",
		"marketplace": "internal",
		"commands": [{
			"name": "plugin:deploy",
			"description": "Deploy from plugin",
			"path": "prompt.md",
			"allowedTools": ["Read", "Edit"],
			"model": "opus"
		}],
		"skills": [{
			"path": "skills/audit",
			"name": "plugin:audit"
		}],
		"agents": "extra-agent.md",
		"hooks": {
			"PostToolUse": [{
				"hooks": [{"type": "command", "command": "echo post"}]
			}]
		},
		"mcpServers": [
			"mcp-extra.json",
			{
				"plugin:docs": {
					"type": "http",
					"url": "https://example.com/mcp"
				}
			}
		],
		"mcp_servers": {
			"plugin:snake": {
				"command": "snake-mcp"
			}
		}
	}`), 0o644); err != nil {
		t.Fatal(err)
	}

	plugin, err := LoadPluginDir(root)
	if err != nil {
		t.Fatal(err)
	}
	if plugin.Name != "demo" || plugin.Version != "1.2.3" || plugin.Description != "Demo plugin" || plugin.Marketplace != "internal" {
		t.Fatalf("plugin metadata = %#v", plugin)
	}
	if len(plugin.PromptTemplates) != 2 || len(plugin.Commands) != 0 {
		t.Fatalf("plugin commands = %#v prompts=%#v", plugin.Commands, plugin.PromptTemplates)
	}
	command := plugin.PromptTemplates[0].Command
	if command.Name != "plugin:deploy" || command.Source != contracts.CommandSourcePlugin || command.LoadedFrom != "plugin" || command.Model != "opus" {
		t.Fatalf("command = %#v", command)
	}
	if strings.TrimSpace(plugin.PromptTemplates[0].Content) != "Run plugin prompt for $ARGUMENTS." {
		t.Fatalf("prompt content = %q", plugin.PromptTemplates[0].Content)
	}
	skillCommand := plugin.PromptTemplates[1].Command
	if skillCommand.Name != "plugin:audit" || skillCommand.Source != contracts.CommandSourcePlugin || skillCommand.LoadedFrom != "plugin" || skillCommand.SkillRoot == "" {
		t.Fatalf("skill command = %#v", skillCommand)
	}
	server, ok := plugin.MCPServers["plugin:docs"]
	if !ok || server.PluginSource != "demo" || server.Name != "plugin:docs" || server.URL != "https://example.com/mcp" {
		t.Fatalf("mcp servers = %#v", plugin.MCPServers)
	}
	if server := plugin.MCPServers["plugin:default"]; server.Command != "default-mcp" || server.PluginSource != "demo" {
		t.Fatalf("default mcp server = %#v", server)
	}
	if server := plugin.MCPServers["plugin:extra"]; server.URL != "https://extra.example/mcp" || server.PluginSource != "demo" {
		t.Fatalf("extra mcp server = %#v", server)
	}
	if server := plugin.MCPServers["plugin:snake"]; server.Command != "snake-mcp" || server.PluginSource != "demo" {
		t.Fatalf("snake mcp server = %#v", server)
	}
	if len(plugin.Agents) != 2 || plugin.Agents[0].Name != "demo:extra-agent" || plugin.Agents[1].Name != "demo:reviewer" {
		t.Fatalf("agents = %#v", plugin.Agents)
	}
	if plugin.Agents[0].Prompt != "# Extra agent\nHelp with extra tasks." || plugin.Agents[1].Prompt != "Review." {
		t.Fatalf("agent prompts = %#v", plugin.Agents)
	}
	if plugin.Agents[1].Model != "opus" || plugin.Agents[1].PermissionMode != contracts.PermissionBypassPermissions || !reflect.DeepEqual(plugin.Agents[1].AllowedTools, []string{"Read", "Bash(git commit -m \"x,y\")", "Edit"}) {
		t.Fatalf("agent frontmatter = %#v", plugin.Agents[1])
	}
	if len(plugin.HookEvents) != 2 || plugin.HookEvents[0].Event != "PostToolUse" || plugin.HookEvents[0].Count != 1 || plugin.HookEvents[1].Event != "PreToolUse" || plugin.HookEvents[1].Count != 1 {
		t.Fatalf("hook events = %#v", plugin.HookEvents)
	}
	hookCounts := hookCountsFromRaw(plugin.Hooks)
	if hookCounts["PreToolUse"] != 1 || hookCounts["PostToolUse"] != 1 {
		t.Fatalf("raw hooks = %#v counts=%#v", plugin.Hooks, hookCounts)
	}
}

func TestParseFrontmatterWordsKeepsNestedToolPatterns(t *testing.T) {
	tests := []struct {
		name string
		raw  string
		want []string
	}{
		{
			name: "bracketed comma list",
			raw:  `[Read, Bash(git commit -m "x,y"), "Bash(grep -E \"a,b\" file)", Edit]`,
			want: []string{"Read", `Bash(git commit -m "x,y")`, `Bash(grep -E \"a,b\" file)`, "Edit"},
		},
		{
			name: "space separated list",
			raw:  `Read Bash(git status:*) "Bash(git commit -m \"x,y\")"`,
			want: []string{"Read", "Bash(git status:*)", `Bash(git commit -m \"x,y\")`},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := parseFrontmatterWords(tt.raw); !reflect.DeepEqual(got, tt.want) {
				t.Fatalf("parseFrontmatterWords(%q) = %#v, want %#v", tt.raw, got, tt.want)
			}
		})
	}
}

func TestLoadPluginDirsWithSettingsSkipsDisabledPlugins(t *testing.T) {
	root := t.TempDir()
	enabledRoot := filepath.Join(root, "enabled")
	disabledRoot := filepath.Join(root, "disabled")
	baseDisabledRoot := filepath.Join(root, "base-disabled")
	for _, dir := range []string{enabledRoot, disabledRoot, baseDisabledRoot} {
		if err := os.MkdirAll(dir, 0o755); err != nil {
			t.Fatal(err)
		}
	}
	if err := os.WriteFile(filepath.Join(enabledRoot, ManifestFileName), []byte(`{"name":"enabled"}`), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(disabledRoot, ManifestFileName), []byte(`{"name":"disabled"}`), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(baseDisabledRoot, ManifestFileName), []byte(`{"name":"different-name"}`), 0o644); err != nil {
		t.Fatal(err)
	}

	plugins := LoadPluginDirsWithSettings([]string{enabledRoot, disabledRoot, baseDisabledRoot}, contracts.Settings{
		EnabledPlugins: map[string]any{
			"disabled":      false,
			"base-disabled": "off",
		},
	})
	if len(plugins) != 1 || plugins[0].Name != "enabled" {
		t.Fatalf("plugins = %#v", plugins)
	}
}

func TestLoadPluginDirsWithSettingsEnforcesMarketplacePolicy(t *testing.T) {
	root := t.TempDir()
	localRoot := filepath.Join(root, "local")
	allowedRoot := filepath.Join(root, "allowed")
	blockedRoot := filepath.Join(root, "blocked")
	strictBlockedRoot := filepath.Join(root, "strict-blocked")
	disabledRoot := filepath.Join(root, "disabled")
	for _, dir := range []string{localRoot, allowedRoot, blockedRoot, strictBlockedRoot, disabledRoot} {
		if err := os.MkdirAll(dir, 0o755); err != nil {
			t.Fatal(err)
		}
	}
	manifests := map[string]string{
		filepath.Join(localRoot, ManifestFileName):         `{"name":"local"}`,
		filepath.Join(allowedRoot, ManifestFileName):       `{"name":"allowed","source":{"name":"enterprise"}}`,
		filepath.Join(blockedRoot, ManifestFileName):       `{"name":"blocked","marketplace":"blocked-market"}`,
		filepath.Join(strictBlockedRoot, ManifestFileName): `{"name":"strict-blocked","marketplaceName":"internal"}`,
		filepath.Join(disabledRoot, ManifestFileName):      `{"name":"disabled","marketplace_name":"enterprise"}`,
	}
	for path, data := range manifests {
		if err := os.WriteFile(path, []byte(data), 0o644); err != nil {
			t.Fatal(err)
		}
	}

	plugins := LoadPluginDirsWithSettings([]string{localRoot, allowedRoot, blockedRoot, strictBlockedRoot, disabledRoot}, contracts.Settings{
		EnabledPlugins: map[string]any{"disabled": false},
		StrictKnownMarketplaces: []any{
			map[string]any{"name": "enterprise"},
		},
		BlockedMarketplaces: []any{"blocked-market"},
	})
	if got := loadedPluginNames(plugins); !reflect.DeepEqual(got, []string{"local", "allowed"}) {
		t.Fatalf("plugins = %#v names=%#v", plugins, got)
	}
}

func TestLoadPluginDirsWithSettingsLoadsSettingsMarketplacePlugins(t *testing.T) {
	root := t.TempDir()
	teamRoot := filepath.Join(root, "team-plugin")
	teamObjectRoot := filepath.Join(root, "team-object-plugin")
	blockedRoot := filepath.Join(root, "blocked-plugin")
	for _, dir := range []string{teamRoot, teamObjectRoot, blockedRoot} {
		if err := os.MkdirAll(dir, 0o755); err != nil {
			t.Fatal(err)
		}
	}
	manifests := map[string]string{
		filepath.Join(teamRoot, ManifestFileName):       `{"name":"team-plugin"}`,
		filepath.Join(teamObjectRoot, ManifestFileName): `{"name":"team-object-plugin"}`,
		filepath.Join(blockedRoot, ManifestFileName):    `{"name":"blocked-plugin"}`,
	}
	for path, data := range manifests {
		if err := os.WriteFile(path, []byte(data), 0o644); err != nil {
			t.Fatal(err)
		}
	}

	plugins := LoadPluginDirsWithSettings(nil, contracts.Settings{
		ExtraKnownMarketplaces: map[string]any{
			"team": map[string]any{"source": map[string]any{
				"source":  "settings",
				"name":    "team",
				"plugins": []any{teamRoot, map[string]any{"root": teamObjectRoot}},
			}},
			"blocked": map[string]any{"source": map[string]any{
				"source":  "settings",
				"name":    "blocked",
				"plugins": []any{blockedRoot},
			}},
		},
		StrictKnownMarketplaces: []any{"team"},
		BlockedMarketplaces:     []any{"blocked"},
	})
	if got := loadedPluginNames(plugins); !reflect.DeepEqual(got, []string{"team-plugin", "team-object-plugin"}) {
		t.Fatalf("plugins = %#v names=%#v", plugins, got)
	}
	for _, plugin := range plugins {
		if plugin.Marketplace != "team" {
			t.Fatalf("plugin marketplace = %#v", plugins)
		}
	}
}

func TestMarketplacePolicyEnforcesBlockedAndStrictSettings(t *testing.T) {
	policy := NewMarketplacePolicy(contracts.Settings{
		ExtraKnownMarketplaces: map[string]any{
			"internal": map[string]any{"url": "https://market.example"},
		},
		StrictKnownMarketplaces: []any{
			"official",
			map[string]any{"name": "enterprise"},
		},
		BlockedMarketplaces: []any{
			"official",
			map[string]any{"hostPattern": "*.blocked.example"},
		},
	})

	if !policy.StrictMode() {
		t.Fatal("strict mode should be active")
	}
	if decision := policy.Decision("official"); decision.Allowed || !strings.Contains(decision.Reason, "blocked") {
		t.Fatalf("official decision = %#v", decision)
	}
	if decision := policy.Decision("enterprise"); !decision.Allowed {
		t.Fatalf("enterprise decision = %#v", decision)
	}
	if decision := policy.Decision("internal"); decision.Allowed || !strings.Contains(decision.Reason, "strictKnownMarketplaces") {
		t.Fatalf("internal decision = %#v", decision)
	}
	if decision := policy.Decision("*.blocked.example"); decision.Allowed || !strings.Contains(decision.Reason, "blocked") {
		t.Fatalf("hostPattern decision = %#v", decision)
	}
}

func TestMarketplacePolicyDefaultsToAllowUnlessBlocked(t *testing.T) {
	policy := NewMarketplacePolicy(contracts.Settings{
		ExtraKnownMarketplaces: map[string]any{
			"internal": map[string]any{"url": "https://market.example"},
		},
		BlockedMarketplaces: []any{"blocked"},
	})

	if decision := policy.Decision("internal"); !decision.Allowed || decision.Name != "internal" {
		t.Fatalf("internal decision = %#v", decision)
	}
	if decision := policy.Decision("new-market"); !decision.Allowed || decision.Name != "new-market" {
		t.Fatalf("new market decision = %#v", decision)
	}
	if decision := policy.Decision("blocked"); decision.Allowed || !strings.Contains(decision.Reason, "blocked") {
		t.Fatalf("blocked decision = %#v", decision)
	}
	if decision := policy.Decision(" "); decision.Allowed || !strings.Contains(decision.Reason, "empty") {
		t.Fatalf("empty decision = %#v", decision)
	}
}

func loadedPluginNames(plugins []LoadedPlugin) []string {
	names := make([]string, 0, len(plugins))
	for _, plugin := range plugins {
		names = append(names, plugin.Name)
	}
	return names
}

func TestProjectPluginDirsWalksToGitRoot(t *testing.T) {
	repo := filepath.Join(t.TempDir(), "repo")
	cwd := filepath.Join(repo, "pkg", "sub")
	for _, dir := range []string{
		filepath.Join(repo, ".git"),
		filepath.Join(repo, ".claude", "plugins", "root"),
		filepath.Join(repo, "pkg", ".claude", "plugins", "pkg"),
		cwd,
	} {
		if err := os.MkdirAll(dir, 0o755); err != nil {
			t.Fatal(err)
		}
	}
	for _, path := range []string{
		filepath.Join(repo, ".claude", "plugins", "root", ManifestFileName),
		filepath.Join(repo, "pkg", ".claude", "plugins", "pkg", ManifestFileName),
	} {
		if err := os.WriteFile(path, []byte(`{"name":"plugin"}`), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	dirs := ProjectPluginDirs(cwd)
	if len(dirs) != 2 || !strings.HasSuffix(dirs[0], filepath.Join("pkg", ".claude", "plugins", "pkg")) || !strings.HasSuffix(dirs[1], filepath.Join(".claude", "plugins", "root")) {
		t.Fatalf("dirs = %#v", dirs)
	}
}

func TestLoadPluginDirLoadsCommandMarkdownDirectoryAndManifestPaths(t *testing.T) {
	root := filepath.Join(t.TempDir(), "demo")
	if err := os.MkdirAll(filepath.Join(root, "commands", "team"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "commands", "team", "release.md"), []byte("---\ndescription: Release service\nargument-hint: [service]\nallowed-tools: Read, Bash(git status:*)\n---\nRelease $ARGUMENTS from ${CLAUDE_SKILL_DIR}."), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, ManifestFileName), []byte(`{"name":"demo"}`), 0o644); err != nil {
		t.Fatal(err)
	}

	plugin, err := LoadPluginDir(root)
	if err != nil {
		t.Fatal(err)
	}
	if len(plugin.PromptTemplates) != 1 {
		t.Fatalf("prompt templates = %#v", plugin.PromptTemplates)
	}
	command := plugin.PromptTemplates[0].Command
	if command.Name != "demo:team:release" || command.Description != "Release service" || command.ArgumentHint != "[service]" {
		t.Fatalf("command = %#v", command)
	}
	if len(command.AllowedTools) != 2 || command.AllowedTools[0] != "Read" || command.AllowedTools[1] != "Bash(git status:*)" {
		t.Fatalf("allowed tools = %#v", command.AllowedTools)
	}

	overrideRoot := filepath.Join(t.TempDir(), "override")
	if err := os.MkdirAll(filepath.Join(overrideRoot, "commands"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(overrideRoot, "commands", "ignored.md"), []byte("Ignored."), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(overrideRoot, "extra.md"), []byte("---\ndescription: Extra command\n---\nExtra $ARGUMENTS."), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(overrideRoot, ManifestFileName), []byte(`{"name":"demo","commands":["extra.md"]}`), 0o644); err != nil {
		t.Fatal(err)
	}
	plugin, err = LoadPluginDir(overrideRoot)
	if err != nil {
		t.Fatal(err)
	}
	if len(plugin.PromptTemplates) != 1 || plugin.PromptTemplates[0].Command.Name != "demo:extra" {
		t.Fatalf("manifest command templates = %#v", plugin.PromptTemplates)
	}
}

func TestLoadPluginDirLoadsCommandObjectMapping(t *testing.T) {
	root := filepath.Join(t.TempDir(), "demo")
	if err := os.MkdirAll(root, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "about.md"), []byte("---\ndescription: File fallback\n---\nAbout $ARGUMENTS."), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, ManifestFileName), []byte(`{
		"name": "demo",
		"commands": {
			"about": {
				"source": "about.md",
				"description": "About plugin",
				"argumentHint": "[topic]",
				"allowedTools": ["Read"],
				"model": "inherit"
			},
			"inline": {
				"content": "Inline $ARGUMENTS.",
				"description": "Inline command"
			}
		}
	}`), 0o644); err != nil {
		t.Fatal(err)
	}

	plugin, err := LoadPluginDir(root)
	if err != nil {
		t.Fatal(err)
	}
	if len(plugin.PromptTemplates) != 2 {
		t.Fatalf("prompt templates = %#v", plugin.PromptTemplates)
	}
	about := plugin.PromptTemplates[0]
	if about.Command.Name != "demo:about" || about.Command.Description != "About plugin" || about.Command.ArgumentHint != "[topic]" || about.Command.Model != "" {
		t.Fatalf("about command = %#v", about.Command)
	}
	if len(about.Command.AllowedTools) != 1 || about.Command.AllowedTools[0] != "Read" || strings.TrimSpace(about.Content) != "About $ARGUMENTS." {
		t.Fatalf("about prompt = %#v content=%q", about.Command.AllowedTools, about.Content)
	}
	inline := plugin.PromptTemplates[1]
	if inline.Command.Name != "demo:inline" || inline.Command.Description != "Inline command" || inline.Content != "Inline $ARGUMENTS." {
		t.Fatalf("inline prompt = %#v content=%q", inline.Command, inline.Content)
	}
}

func TestLoadPluginDirLoadsDefaultAndManifestSkillDirectories(t *testing.T) {
	root := filepath.Join(t.TempDir(), "demo")
	if err := os.MkdirAll(filepath.Join(root, "skills", "review"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "skills", "review", "SKILL.md"), []byte("---\ndescription: Review code\nallowed-tools: Read\n---\nReview from ${CLAUDE_SKILL_DIR}."), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, ManifestFileName), []byte(`{"name":"demo"}`), 0o644); err != nil {
		t.Fatal(err)
	}
	plugin, err := LoadPluginDir(root)
	if err != nil {
		t.Fatal(err)
	}
	if len(plugin.PromptTemplates) != 1 {
		t.Fatalf("default skill prompts = %#v", plugin.PromptTemplates)
	}
	skill := plugin.PromptTemplates[0]
	if skill.Command.Name != "demo:review" || skill.Command.Description != "Review code" || skill.Command.LoadedFrom != "plugin" || skill.Command.Source != contracts.CommandSourcePlugin {
		t.Fatalf("default skill command = %#v", skill.Command)
	}

	overrideRoot := filepath.Join(t.TempDir(), "override")
	if err := os.MkdirAll(filepath.Join(overrideRoot, "skills", "ignored"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(overrideRoot, "skills", "ignored", "SKILL.md"), []byte("---\ndescription: Ignored\n---\nIgnored."), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Join(overrideRoot, "extra-skills", "audit"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(overrideRoot, "extra-skills", "audit", "SKILL.md"), []byte("---\ndescription: Audit code\n---\nAudit."), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Join(overrideRoot, "direct-skill"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(overrideRoot, "direct-skill", "SKILL.md"), []byte("---\ndescription: Direct skill\n---\nDirect."), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(overrideRoot, ManifestFileName), []byte(`{"name":"demo","skills":["extra-skills","direct-skill"]}`), 0o644); err != nil {
		t.Fatal(err)
	}
	plugin, err = LoadPluginDir(overrideRoot)
	if err != nil {
		t.Fatal(err)
	}
	if len(plugin.PromptTemplates) != 2 {
		t.Fatalf("manifest skill prompts = %#v", plugin.PromptTemplates)
	}
	if plugin.PromptTemplates[0].Command.Name != "demo:audit" || plugin.PromptTemplates[1].Command.Name != "demo:direct-skill" {
		t.Fatalf("manifest skill names = %#v %#v", plugin.PromptTemplates[0].Command, plugin.PromptTemplates[1].Command)
	}
}

func TestLoadPluginDirLoadsOutputStyles(t *testing.T) {
	root := filepath.Join(t.TempDir(), "demo")
	if err := os.MkdirAll(filepath.Join(root, "output-styles", "nested"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "output-styles", "concise.md"), []byte("---\ndescription: Concise style\nforce-for-plugin: true\n---\nBe concise."), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "output-styles", "nested", "named.md"), []byte("---\nname: Formal\n---\n# Formal style\nBe formal."), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, ManifestFileName), []byte(`{"name":"demo"}`), 0o644); err != nil {
		t.Fatal(err)
	}
	plugin, err := LoadPluginDir(root)
	if err != nil {
		t.Fatal(err)
	}
	if len(plugin.OutputStyles) != 2 {
		t.Fatalf("output styles = %#v", plugin.OutputStyles)
	}
	if plugin.OutputStyles[0].Name != "demo:concise" || plugin.OutputStyles[0].Description != "Concise style" || plugin.OutputStyles[0].ForceForPlugin == nil || !*plugin.OutputStyles[0].ForceForPlugin {
		t.Fatalf("concise style = %#v", plugin.OutputStyles[0])
	}
	if plugin.OutputStyles[1].Name != "demo:Formal" || plugin.OutputStyles[1].Description != "Formal style" {
		t.Fatalf("formal style = %#v", plugin.OutputStyles[1])
	}

	overrideRoot := filepath.Join(t.TempDir(), "override")
	if err := os.MkdirAll(filepath.Join(overrideRoot, "output-styles"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(overrideRoot, "output-styles", "ignored.md"), []byte("Ignored."), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Join(overrideRoot, "styles"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(overrideRoot, "styles", "focused.md"), []byte("---\ndescription: Focused style\n---\nFocus."), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(overrideRoot, ManifestFileName), []byte(`{"name":"demo","outputStyles":["styles"]}`), 0o644); err != nil {
		t.Fatal(err)
	}
	plugin, err = LoadPluginDir(overrideRoot)
	if err != nil {
		t.Fatal(err)
	}
	if len(plugin.OutputStyles) != 1 || plugin.OutputStyles[0].Name != "demo:focused" {
		t.Fatalf("manifest output styles = %#v", plugin.OutputStyles)
	}
}
