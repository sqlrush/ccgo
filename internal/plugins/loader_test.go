package plugins

import (
	"os"
	"path/filepath"
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
	if err := os.WriteFile(filepath.Join(root, "agents", "review.md"), []byte("---\nname: reviewer\ndescription: Review changes\npermissionMode: bypassPermissions\n---\nReview."), 0o644); err != nil {
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
	if plugin.Name != "demo" || plugin.Version != "1.2.3" || plugin.Description != "Demo plugin" {
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
	if len(plugin.HookEvents) != 2 || plugin.HookEvents[0].Event != "PostToolUse" || plugin.HookEvents[0].Count != 1 || plugin.HookEvents[1].Event != "PreToolUse" || plugin.HookEvents[1].Count != 1 {
		t.Fatalf("hook events = %#v", plugin.HookEvents)
	}
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
