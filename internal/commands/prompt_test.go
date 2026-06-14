package commands

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"ccgo/internal/contracts"
)

func TestExpandPromptSubstitutesArgumentsAndReturnsMetaUserMessage(t *testing.T) {
	registry := FromSources(Sources{
		ProjectSkillPrompts: []PromptTemplate{{
			Command: contracts.Command{
				Name:          "deploy",
				DisplayName:   "Deploy Helper",
				Type:          contracts.CommandPrompt,
				Source:        contracts.CommandSourceSkills,
				LoadedFrom:    "skills",
				SkillRoot:     "/tmp/skill",
				ArgumentNames: []string{"service", "env"},
			},
			Content: strings.Join([]string{
				"Base directory for this skill: /tmp/skill",
				"",
				"Deploy $service to $env.",
				"Raw args: $ARGUMENTS",
				"First: $0 / indexed: $ARGUMENTS[1]",
				"Skill dir: ${CLAUDE_SKILL_DIR}",
				"Session: ${CLAUDE_SESSION_ID}",
			}, "\n"),
		}},
	})

	expanded, err := registry.ExpandPrompt("Deploy Helper", `"api service" prod`, "sess_123")
	if err != nil {
		t.Fatal(err)
	}
	want := strings.Join([]string{
		"Base directory for this skill: /tmp/skill",
		"",
		"Deploy api service to prod.",
		`Raw args: "api service" prod`,
		"First: api service / indexed: prod",
		"Skill dir: /tmp/skill",
		"Session: sess_123",
	}, "\n")
	if expanded.Content != want {
		t.Fatalf("content = %q, want %q", expanded.Content, want)
	}
	if len(expanded.ContentBlocks) != 1 || expanded.ContentBlocks[0].Type != contracts.ContentText || expanded.ContentBlocks[0].Text != want {
		t.Fatalf("content blocks = %#v", expanded.ContentBlocks)
	}
	if expanded.Message.Type != contracts.MessageUser || !expanded.Message.IsMeta || expanded.Message.SessionID != "sess_123" {
		t.Fatalf("message = %#v", expanded.Message)
	}
	if len(expanded.Message.Content) != 1 || expanded.Message.Content[0].Text != want {
		t.Fatalf("message content = %#v", expanded.Message.Content)
	}
}

func TestExpandPromptDoesNotSubstituteSkillDirForMCPSkills(t *testing.T) {
	registry := FromSources(Sources{
		PluginSkillPrompts: []PromptTemplate{{
			Command: contracts.Command{
				Name:       "remote",
				Type:       contracts.CommandPrompt,
				Source:     contracts.CommandSourceMCP,
				LoadedFrom: "mcp",
				SkillRoot:  "/tmp/remote",
			},
			Content: "Use ${CLAUDE_SKILL_DIR} in ${CLAUDE_SESSION_ID}.",
		}},
	})

	expanded, err := registry.ExpandPrompt("remote", "", "sess_789")
	if err != nil {
		t.Fatal(err)
	}
	want := "Use ${CLAUDE_SKILL_DIR} in sess_789."
	if expanded.Content != want {
		t.Fatalf("content = %q, want %q", expanded.Content, want)
	}
}

func TestExpandPromptSubstitutesPluginUserConfig(t *testing.T) {
	registry := FromSources(Sources{
		PluginSkillPrompts: []PromptTemplate{{
			Command: contracts.Command{
				Name:       "demo:deploy",
				Type:       contracts.CommandPrompt,
				Source:     contracts.CommandSourcePlugin,
				LoadedFrom: "plugin",
				UserConfig: map[string]any{
					"env":     "prod",
					"enabled": true,
					"count":   2,
					"nested":  map[string]any{"region": "iad"},
					"labels":  []any{"api", "blue"},
				},
			},
			Content: strings.Join([]string{
				"Deploy ${user_config.env} to $user_config.nested.region.",
				"Enabled: {{ userConfig.enabled }}.",
				"Count: $USER_CONFIG.count.",
				"Labels: {{user_config.labels}}.",
				"Missing: ${user_config.missing}.",
			}, "\n"),
		}},
	})

	expanded, err := registry.ExpandPrompt("demo:deploy", "", "")
	if err != nil {
		t.Fatal(err)
	}
	want := strings.Join([]string{
		"Deploy prod to iad.",
		"Enabled: true.",
		"Count: 2.",
		`Labels: ["api","blue"].`,
		"Missing: .",
	}, "\n")
	if expanded.Content != want {
		t.Fatalf("content = %q, want %q", expanded.Content, want)
	}
}

func TestLoadProjectSkillCanExpandPrompt(t *testing.T) {
	repo := filepath.Join(t.TempDir(), "repo")
	cwd := filepath.Join(repo, "pkg")
	skillDir := filepath.Join(cwd, ".claude", "skills", "explain")
	writeCommandSkill(t, skillDir, `---
description: Explain a target
arguments: target
---
Explain $target in session ${CLAUDE_SESSION_ID}.
`)
	if err := os.MkdirAll(filepath.Join(repo, ".git"), 0o755); err != nil {
		t.Fatal(err)
	}

	registry := Load(Options{CWD: cwd})
	expanded, err := registry.ExpandPrompt("explain", "planner", "sess_456")
	if err != nil {
		t.Fatal(err)
	}
	want := "Base directory for this skill: " + skillDir + "\n\nExplain planner in session sess_456.\n"
	if expanded.Content != want {
		t.Fatalf("content = %q, want %q", expanded.Content, want)
	}
}

func TestExpandPromptAppendsArgumentsWhenNoPlaceholder(t *testing.T) {
	registry := FromSources(Sources{
		ProjectSkillPrompts: []PromptTemplate{{
			Command: contracts.Command{Name: "review", Type: contracts.CommandPrompt, Source: contracts.CommandSourceSkills, LoadedFrom: "skills"},
			Content: "Review this change.",
		}},
	})

	expanded, err := registry.ExpandPrompt("review", "src/main.go", "")
	if err != nil {
		t.Fatal(err)
	}
	if expanded.Content != "Review this change.\n\nARGUMENTS: src/main.go" {
		t.Fatalf("content = %q", expanded.Content)
	}
}

func TestPromptTemplateLookupUsesAliasAndDefensiveClone(t *testing.T) {
	registry := FromSources(Sources{
		ProjectSkillPrompts: []PromptTemplate{{
			Command: contracts.Command{
				Name:         "skill",
				Type:         contracts.CommandPrompt,
				Aliases:      []string{"s"},
				AllowedTools: []string{"Read"},
				Source:       contracts.CommandSourceSkills,
				LoadedFrom:   "skills",
				UserConfig:   map[string]any{"env": "prod"},
			},
			Content: "Use $ARGUMENTS.",
		}},
	})

	template, ok := registry.PromptTemplate("s")
	if !ok {
		t.Fatalf("expected template by alias")
	}
	template.Command.AllowedTools[0] = "Write"
	template.Command.UserConfig["env"] = "dev"

	again, ok := registry.PromptTemplate("skill")
	if !ok {
		t.Fatalf("expected template by name")
	}
	if again.Command.AllowedTools[0] != "Read" {
		t.Fatalf("template was mutated: %#v", again.Command)
	}
	if again.Command.UserConfig["env"] != "prod" {
		t.Fatalf("template user config was mutated: %#v", again.Command.UserConfig)
	}
}

func TestSubstituteArgumentsSupportsIndexesAndFallbackParsing(t *testing.T) {
	content := "$name $ARGUMENTS[1] $1 $ARGUMENTS $nameX $ARGUMENTS[9]"
	got := SubstituteArguments(content, `alice "hello world"`, true, []string{"name"})
	want := `alice hello world hello world alice "hello world" $nameX `
	if got != want {
		t.Fatalf("substitution = %q, want %q", got, want)
	}

	got = SubstituteArguments("$0 $1", `"unterminated value`, true, nil)
	if got != `"unterminated value` {
		t.Fatalf("fallback substitution = %q", got)
	}
}

func TestParseArgumentsShellLike(t *testing.T) {
	got := ParseArguments(`one "two words" 'three words' four\ five`)
	want := []string{"one", "two words", "three words", "four five"}
	if !sameCommandNames(got, want) {
		t.Fatalf("args = %#v, want %#v", got, want)
	}
}
