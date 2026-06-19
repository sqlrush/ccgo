package skilltools

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"ccgo/internal/commands"
	"ccgo/internal/contracts"
	"ccgo/internal/tool"
)

func TestSkillToolExpandsPromptAndReturnsNewMetaMessage(t *testing.T) {
	registry := commands.FromSources(commands.Sources{
		ProjectSkillPrompts: []commands.PromptTemplate{{
			Command: contracts.Command{
				Name:         "deploy",
				DisplayName:  "Deploy Helper",
				Type:         contracts.CommandPrompt,
				Source:       contracts.CommandSourceSkills,
				LoadedFrom:   "skills",
				AllowedTools: []string{"Read"},
				Model:        "opus",
			},
			Content: "Deploy $ARGUMENTS in ${CLAUDE_SESSION_ID}.",
		}},
	})
	executor := skillExecutor(t, registry)

	result, err := executor.Execute(tool.Context{Context: context.Background(), SessionID: "sess_skill"}, contracts.ToolUse{
		ID:    "toolu_skill",
		Name:  "Skill",
		Input: json.RawMessage(`{"skill":"Deploy Helper","args":"api"}`),
	}, nil)
	if err != nil {
		t.Fatal(err)
	}
	if result.Content != "Launching skill: deploy" {
		t.Fatalf("content = %#v", result.Content)
	}
	if result.StructuredContent["commandName"] != "deploy" || result.StructuredContent["status"] != "inline" {
		t.Fatalf("structured content = %#v", result.StructuredContent)
	}
	if result.StructuredContent["model"] != "opus" {
		t.Fatalf("model = %#v", result.StructuredContent["model"])
	}
	if len(result.NewMessages) != 2 {
		t.Fatalf("new messages = %#v", result.NewMessages)
	}
	message := result.NewMessages[0]
	if message.Type != contracts.MessageUser || !message.IsMeta || message.SessionID != "sess_skill" {
		t.Fatalf("new message = %#v", message)
	}
	if len(message.Content) != 1 || message.Content[0].Text != "Deploy api in sess_skill." {
		t.Fatalf("new message content = %#v", message.Content)
	}
	perms, ok := commands.CommandPermissionsFromMessage(result.NewMessages[1])
	if !ok || perms.Model != "opus" || len(perms.AllowedTools) != 1 || perms.AllowedTools[0] != "Read" {
		t.Fatalf("command permissions = %#v ok=%v", perms, ok)
	}
}

func TestSkillToolAcceptsCommandNameAndArgumentsAliases(t *testing.T) {
	registry := commands.FromSources(commands.Sources{
		ProjectSkillPrompts: []commands.PromptTemplate{{
			Command: contracts.Command{Name: "review", Type: contracts.CommandPrompt, Source: contracts.CommandSourceSkills, LoadedFrom: "skills"},
			Content: "Review $ARGUMENTS.",
		}},
	})
	executor := skillExecutor(t, registry)

	result, err := executor.Execute(tool.Context{Context: context.Background()}, contracts.ToolUse{
		ID:    "toolu_skill_alias",
		Name:  "Skill",
		Input: json.RawMessage(`{"commandName":"review","arguments":"src/main.go"}`),
	}, nil)
	if err != nil {
		t.Fatal(err)
	}
	if result.NewMessages[0].Content[0].Text != "Review src/main.go." {
		t.Fatalf("new message content = %#v", result.NewMessages[0].Content)
	}
}

func TestSkillToolLoadsProjectSkillsFromWorkingDirectory(t *testing.T) {
	repo := filepath.Join(t.TempDir(), "repo")
	cwd := filepath.Join(repo, "pkg")
	skillDir := filepath.Join(cwd, ".claude", "skills", "explain")
	if err := os.MkdirAll(skillDir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(skillDir, "SKILL.md"), []byte("---\ndescription: Explain target\n---\nExplain $ARGUMENTS."), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Join(repo, ".git"), 0o755); err != nil {
		t.Fatal(err)
	}
	executor := skillExecutor(t)

	result, err := executor.Execute(tool.Context{Context: context.Background(), WorkingDirectory: cwd}, contracts.ToolUse{
		ID:    "toolu_skill_project",
		Name:  "Skill",
		Input: json.RawMessage(`{"skill":"explain","args":"planner"}`),
	}, nil)
	if err != nil {
		t.Fatal(err)
	}
	text := result.NewMessages[0].Content[0].Text
	if !strings.Contains(text, "Base directory for this skill: "+skillDir) || !strings.Contains(text, "Explain planner.") {
		t.Fatalf("expanded text = %q", text)
	}
}

func TestSkillToolLoadsPluginSkillAndReturnsCommandMetadata(t *testing.T) {
	repo := filepath.Join(t.TempDir(), "repo")
	cwd := filepath.Join(repo, "pkg")
	pluginDir := filepath.Join(repo, ".claude", "plugins", "demo")
	skillDir := filepath.Join(pluginDir, "skills", "review")
	if err := os.MkdirAll(filepath.Join(repo, ".git"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(skillDir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(cwd, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(pluginDir, "plugin.json"), []byte(`{"name":"demo"}`), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(skillDir, "SKILL.md"), []byte(`---
name: Review Helper
description: Review code
argument-hint: "[target]"
arguments: target
allowed-tools: Read, Bash(git status:*)
when_to_use: During reviews
version: 1.2.3
model: opus
context: fork
agent: reviewer
effort: high
paths: "**/*.go"
---
Review $target from ${CLAUDE_SKILL_DIR}.`), 0o644); err != nil {
		t.Fatal(err)
	}
	executor := skillExecutor(t)

	result, err := executor.Execute(tool.Context{Context: context.Background(), WorkingDirectory: cwd, SessionID: "sess_plugin_skill"}, contracts.ToolUse{
		ID:    "toolu_skill_plugin",
		Name:  "Skill",
		Input: json.RawMessage(`{"skill":"Review Helper","args":"api"}`),
	}, nil)
	if err != nil {
		t.Fatal(err)
	}
	if result.StructuredContent["commandName"] != "demo:review" ||
		result.StructuredContent["displayName"] != "Review Helper" ||
		result.StructuredContent["source"] != "plugin" ||
		result.StructuredContent["loadedFrom"] != "plugin" ||
		result.StructuredContent["skillRoot"] != skillDir ||
		result.StructuredContent["description"] != "Review code" ||
		result.StructuredContent["argumentHint"] != "[target]" ||
		result.StructuredContent["whenToUse"] != "During reviews" ||
		result.StructuredContent["version"] != "1.2.3" ||
		result.StructuredContent["model"] != "opus" ||
		result.StructuredContent["context"] != "fork" ||
		result.StructuredContent["agent"] != "reviewer" ||
		result.StructuredContent["effort"] != "high" ||
		result.StructuredContent["progressMessage"] != "running" {
		t.Fatalf("structured content = %#v", result.StructuredContent)
	}
	if got, ok := result.StructuredContent["argumentNames"].([]string); !ok || len(got) != 1 || got[0] != "target" {
		t.Fatalf("argument names = %#v", result.StructuredContent["argumentNames"])
	}
	if got, ok := result.StructuredContent["allowedTools"].([]string); !ok || len(got) != 2 || got[0] != "Read" || got[1] != "Bash(git status:*)" {
		t.Fatalf("allowed tools = %#v", result.StructuredContent["allowedTools"])
	}
	if got, ok := result.StructuredContent["paths"].([]string); !ok || len(got) != 1 || got[0] != "**/*.go" {
		t.Fatalf("paths = %#v", result.StructuredContent["paths"])
	}
	text := result.NewMessages[0].Content[0].Text
	if !strings.Contains(text, "Review api from "+skillDir+".") {
		t.Fatalf("expanded text = %q", text)
	}
	perms, ok := commands.CommandPermissionsFromMessage(result.NewMessages[1])
	if !ok || perms.Model != "opus" || len(perms.AllowedTools) != 2 || perms.AllowedTools[1] != "Bash(git status:*)" {
		t.Fatalf("command permissions = %#v ok=%v", perms, ok)
	}
}

func TestSkillToolSkipsDisabledPluginSkillsFromMetadataSettings(t *testing.T) {
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
	executor := skillExecutor(t)
	ctx := tool.Context{
		Context:          context.Background(),
		WorkingDirectory: cwd,
		Metadata: map[string]any{
			tool.MetadataSettingsKey: contracts.Settings{EnabledPlugins: map[string]any{"demo": false}},
		},
	}

	result, err := executor.Execute(ctx, contracts.ToolUse{
		ID:    "toolu_skill_disabled_plugin",
		Name:  "Skill",
		Input: json.RawMessage(`{"skill":"demo:deploy","args":"api"}`),
	}, nil)
	if err == nil {
		t.Fatal("expected disabled plugin skill error")
	}
	if !result.IsError || !strings.Contains(result.Content.(string), `skill "demo:deploy" not found`) {
		t.Fatalf("result = %#v", result)
	}
	skillTool := NewSkillTool()
	prompt, err := skillTool.Prompt(tool.PromptContext{
		WorkingDirectory: cwd,
		Metadata: map[string]any{
			tool.MetadataSettingsKey: contracts.Settings{EnabledPlugins: map[string]any{"demo": false}},
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(prompt, "demo:deploy") {
		t.Fatalf("prompt listed disabled plugin skill: %q", prompt)
	}
}

func TestSkillToolRejectsDisabledModelInvocation(t *testing.T) {
	registry := commands.FromSources(commands.Sources{
		ProjectSkillPrompts: []commands.PromptTemplate{{
			Command: contracts.Command{
				Name:                   "hidden",
				Type:                   contracts.CommandPrompt,
				Source:                 contracts.CommandSourceSkills,
				LoadedFrom:             "skills",
				DisableModelInvocation: true,
			},
			Content: "Hidden.",
		}},
	})
	executor := skillExecutor(t, registry)

	result, err := executor.Execute(tool.Context{Context: context.Background()}, contracts.ToolUse{
		ID:    "toolu_skill_disabled",
		Name:  "Skill",
		Input: json.RawMessage(`{"skill":"hidden"}`),
	}, nil)
	if err == nil {
		t.Fatal("expected disabled skill error")
	}
	if !result.IsError || !strings.Contains(result.Content.(string), "disable-model-invocation") {
		t.Fatalf("result = %#v", result)
	}
}

func TestSkillToolPromptListsAvailableSkills(t *testing.T) {
	registry := commands.FromSources(commands.Sources{
		ProjectSkillPrompts: []commands.PromptTemplate{{
			Command: contracts.Command{Name: "review", DisplayName: "Review Helper", Type: contracts.CommandPrompt, Source: contracts.CommandSourceSkills, LoadedFrom: "skills"},
			Content: "Review.",
		}},
	})
	skillTool := NewSkillTool(registry)
	prompt, err := skillTool.Prompt(tool.PromptContext{})
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(prompt, "Review Helper") {
		t.Fatalf("prompt = %q", prompt)
	}
}

func skillExecutor(t *testing.T, registries ...commands.Registry) tool.Executor {
	t.Helper()
	registry, err := tool.NewRegistry(NewSkillTool(registries...))
	if err != nil {
		t.Fatal(err)
	}
	return tool.NewExecutor(registry)
}
