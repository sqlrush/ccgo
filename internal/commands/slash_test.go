package commands

import (
	"strings"
	"testing"

	"ccgo/internal/contracts"
)

func TestExecuteBuiltinLoginLogout(t *testing.T) {
	reg := FromSources(Sources{Builtins: BuiltinCommands()})

	loginCmd, ok := reg.Find("login")
	if !ok {
		t.Fatal("login builtin not registered")
	}
	res, ok := ExecuteBuiltinLocalCommand(reg, loginCmd, "")
	if !ok || res.Type != LocalCommandResultLogin {
		t.Fatalf("login result = %+v ok=%v", res, ok)
	}

	logoutCmd, ok := reg.Find("logout")
	if !ok {
		t.Fatal("logout builtin not registered")
	}
	res, ok = ExecuteBuiltinLocalCommand(reg, logoutCmd, "")
	if !ok || res.Type != LocalCommandResultLogout {
		t.Fatalf("logout result = %+v ok=%v", res, ok)
	}
}

func TestParseSlashCommandSupportsMCPMarker(t *testing.T) {
	parsed, ok := ParseSlashCommand("/mcp:search (MCP) foo bar")
	if !ok {
		t.Fatal("slash command was not parsed")
	}
	if parsed.CommandName != "mcp:search (MCP)" || parsed.Args != "foo bar" || !parsed.IsMCP {
		t.Fatalf("parsed = %#v", parsed)
	}
}

func TestExecuteSlashPromptCommandBuildsMetadataAndMetaPrompt(t *testing.T) {
	registry := FromSources(Sources{
		ProjectSkillPrompts: []PromptTemplate{{
			Command: contracts.Command{
				Name:         "deploy",
				Type:         contracts.CommandPrompt,
				Source:       contracts.CommandSourceSkills,
				LoadedFrom:   "skills",
				AllowedTools: []string{"Read"},
				Model:        "opus",
			},
			Content: "Deploy $ARGUMENTS in ${CLAUDE_SESSION_ID}.",
		}},
	})

	result, handled, err := ExecuteSlashCommand(registry, "/deploy api", SlashOptions{
		SessionID: "sess_cmd",
		UUID:      "user_cmd",
	})
	if err != nil {
		t.Fatal(err)
	}
	if !handled || !result.ShouldQuery {
		t.Fatalf("handled=%v result=%#v", handled, result)
	}
	if result.Model != "opus" || len(result.AllowedTools) != 1 || result.AllowedTools[0] != "Read" {
		t.Fatalf("result metadata = %#v", result)
	}
	if len(result.Messages) != 3 {
		t.Fatalf("messages = %#v", result.Messages)
	}
	metadata := result.Messages[0]
	if metadata.UUID != "user_cmd" || metadata.IsMeta {
		t.Fatalf("metadata message = %#v", metadata)
	}
	if text := metadata.Content[0].Text; !strings.Contains(text, "<command-name>/deploy</command-name>") || !strings.Contains(text, "<command-args>api</command-args>") {
		t.Fatalf("metadata text = %q", text)
	}
	prompt := result.Messages[1]
	if !prompt.IsMeta || prompt.SessionID != "sess_cmd" || prompt.Content[0].Text != "Deploy api in sess_cmd." {
		t.Fatalf("prompt message = %#v", prompt)
	}
	perms, ok := CommandPermissionsFromMessage(result.Messages[2])
	if !ok || perms.Model != "opus" || len(perms.AllowedTools) != 1 || perms.AllowedTools[0] != "Read" {
		t.Fatalf("command permissions = %#v ok=%v", perms, ok)
	}
}

func TestParseToolListSplitsCommaAndSpaceOutsideParens(t *testing.T) {
	got := ParseToolList([]string{"Read, Edit Bash(git status *)", "WebFetch(domain:example.com)"})
	want := []string{"Read", "Edit", "Bash(git status *)", "WebFetch(domain:example.com)"}
	if strings.Join(got, "|") != strings.Join(want, "|") {
		t.Fatalf("tool list = %#v, want %#v", got, want)
	}
}

func TestExecuteSlashUnknownCommandDoesNotQuery(t *testing.T) {
	result, handled, err := ExecuteSlashCommand(FromSources(Sources{}), "/missing arg", SlashOptions{SessionID: "sess"})
	if err != nil {
		t.Fatal(err)
	}
	if !handled || result.ShouldQuery || !result.Unknown {
		t.Fatalf("handled=%v result=%#v", handled, result)
	}
	if result.Messages[0].Content[0].Text != "Unknown skill: missing" {
		t.Fatalf("unknown message = %#v", result.Messages[0])
	}
}

func TestExecuteMalformedSlashDoesNotQuery(t *testing.T) {
	result, handled, err := ExecuteSlashCommand(FromSources(Sources{}), "/", SlashOptions{UUID: "user_slash"})
	if err != nil {
		t.Fatal(err)
	}
	if !handled || result.ShouldQuery {
		t.Fatalf("handled=%v result=%#v", handled, result)
	}
	if result.Messages[0].UUID != "user_slash" || result.Messages[0].Content[0].Text != "Commands are in the form `/command [args]`" {
		t.Fatalf("malformed slash message = %#v", result.Messages[0])
	}
}

func TestExecuteSlashNonCommandPathFallsThrough(t *testing.T) {
	_, handled, err := ExecuteSlashCommand(FromSources(Sources{}), "/tmp/file.txt", SlashOptions{})
	if err != nil {
		t.Fatal(err)
	}
	if handled {
		t.Fatal("path-like slash input should fall through as normal prompt text")
	}
}

func TestExecuteSlashLocalCommandWithoutExecutorReturnsUnavailableOutput(t *testing.T) {
	registry := FromSources(Sources{Builtins: []contracts.Command{{
		Type:   contracts.CommandLocalJSX,
		Name:   "unsupported",
		Source: contracts.CommandSourceBuiltin,
	}}})
	result, handled, err := ExecuteSlashCommand(registry, "/unsupported", SlashOptions{UUID: "user_unsupported"})
	if err != nil {
		t.Fatal(err)
	}
	if !handled || result.ShouldQuery || !result.Unsupported {
		t.Fatalf("handled=%v result=%#v", handled, result)
	}
	if len(result.Messages) != 2 || result.Messages[0].UUID != "user_unsupported" {
		t.Fatalf("messages = %#v", result.Messages)
	}
	stderrText := result.Messages[1].Content[0].Text
	if !strings.Contains(stderrText, "<local-command-stderr>") {
		t.Fatalf("stderr message = %#v", result.Messages[1])
	}
	if !strings.Contains(stderrText, "this runtime cannot execute it") || strings.Contains(stderrText, "not implemented") {
		t.Fatalf("unavailable stderr = %q", stderrText)
	}
}

func TestExecuteSlashBuiltinLocalCommandsHaveExecutors(t *testing.T) {
	registry := FromSources(Sources{Builtins: BuiltinCommands()})
	for _, cmd := range BuiltinCommands() {
		if cmd.Type != contracts.CommandLocal && cmd.Type != contracts.CommandLocalJSX {
			continue
		}
		t.Run(cmd.Name, func(t *testing.T) {
			local, ok := ExecuteBuiltinLocalCommand(registry, cmd, "")
			if !ok {
				t.Fatalf("builtin local command %q has no executor", cmd.Name)
			}
			if local.Type == "" {
				t.Fatalf("builtin local command %q returned empty local result", cmd.Name)
			}
		})
	}
}

func TestExecuteSlashClearReturnsLocalClearResult(t *testing.T) {
	registry := FromSources(Sources{Builtins: BuiltinCommands()})
	result, handled, err := ExecuteSlashCommand(registry, "/clear", SlashOptions{UUID: "user_clear"})
	if err != nil {
		t.Fatal(err)
	}
	if !handled || result.ShouldQuery || result.Unsupported || result.LocalResult == nil {
		t.Fatalf("handled=%v result=%#v", handled, result)
	}
	if result.LocalResult.Type != LocalCommandResultClear || result.LocalResult.Value != "" {
		t.Fatalf("local result = %#v", result.LocalResult)
	}
	if len(result.Messages) != 1 || result.Messages[0].UUID != "user_clear" {
		t.Fatalf("messages = %#v", result.Messages)
	}
	if text := result.Messages[0].Content[0].Text; !strings.Contains(text, "<command-name>/clear</command-name>") {
		t.Fatalf("clear command message = %q", text)
	}
}

func TestExecuteSlashHelpReturnsLocalTextResult(t *testing.T) {
	registry := FromSources(Sources{Builtins: BuiltinCommands()})
	result, handled, err := ExecuteSlashCommand(registry, "/help", SlashOptions{UUID: "user_help"})
	if err != nil {
		t.Fatal(err)
	}
	if !handled || result.ShouldQuery || result.Unsupported || result.LocalResult == nil {
		t.Fatalf("handled=%v result=%#v", handled, result)
	}
	if result.LocalResult.Type != LocalCommandResultText || !strings.Contains(result.LocalResult.Value, "Available commands:") {
		t.Fatalf("local result = %#v", result.LocalResult)
	}
	if len(result.Messages) != 2 || result.Messages[0].UUID != "user_help" {
		t.Fatalf("messages = %#v", result.Messages)
	}
	if text := result.Messages[1].Content[0].Text; !strings.Contains(text, "/help - Show help") || !strings.Contains(text, "/status - Show Claude Code status") {
		t.Fatalf("help text = %q", text)
	}
}

func TestExecuteSlashHelpWithCommandReturnsDetail(t *testing.T) {
	registry := FromSources(Sources{Builtins: BuiltinCommands()})
	result, handled, err := ExecuteSlashCommand(registry, "/help status", SlashOptions{UUID: "user_help_status"})
	if err != nil {
		t.Fatal(err)
	}
	if !handled || result.ShouldQuery || result.Unsupported || result.LocalResult == nil {
		t.Fatalf("handled=%v result=%#v", handled, result)
	}
	if result.LocalResult.Type != LocalCommandResultText {
		t.Fatalf("local result = %#v", result.LocalResult)
	}
	text := result.Messages[1].Content[0].Text
	for _, want := range []string{
		"Command /status",
		"Type: local-jsx",
		"Source: builtin",
		"Description: Show Claude Code status",
		"Immediate: true",
	} {
		if !strings.Contains(text, want) {
			t.Fatalf("help detail missing %q in %q", want, text)
		}
	}
}

func TestExecuteSlashHelpSearchesCommands(t *testing.T) {
	registry := FromSources(Sources{Builtins: BuiltinCommands()})
	result, handled, err := ExecuteSlashCommand(registry, "/help search MCP", SlashOptions{UUID: "user_help_search"})
	if err != nil {
		t.Fatal(err)
	}
	if !handled || result.ShouldQuery || result.Unsupported || result.LocalResult == nil {
		t.Fatalf("handled=%v result=%#v", handled, result)
	}
	text := result.Messages[1].Content[0].Text
	for _, want := range []string{
		"Help search: MCP",
		"Matches: 1",
		"/mcp - Manage MCP servers",
	} {
		if !strings.Contains(text, want) {
			t.Fatalf("help search missing %q: %q", want, text)
		}
	}

	result, handled, err = ExecuteSlashCommand(registry, "/help search", SlashOptions{UUID: "user_help_search_empty"})
	if err != nil {
		t.Fatal(err)
	}
	if !handled || result.LocalResult == nil || result.Messages[1].Content[0].Text != "Usage: /help search <query>" {
		t.Fatalf("empty help search = %#v", result)
	}

	result, handled, err = ExecuteSlashCommand(registry, "/help search nowhere", SlashOptions{UUID: "user_help_search_none"})
	if err != nil {
		t.Fatal(err)
	}
	if !handled || result.LocalResult == nil || result.Messages[1].Content[0].Text != "No commands matched nowhere." {
		t.Fatalf("missing help search = %#v", result)
	}
}

func TestExecuteSlashMCPReturnsLocalMCPResult(t *testing.T) {
	registry := FromSources(Sources{Builtins: BuiltinCommands()})
	result, handled, err := ExecuteSlashCommand(registry, "/mcp list", SlashOptions{UUID: "user_mcp"})
	if err != nil {
		t.Fatal(err)
	}
	if !handled || result.ShouldQuery || result.Unsupported || result.LocalResult == nil {
		t.Fatalf("handled=%v result=%#v", handled, result)
	}
	if result.LocalResult.Type != LocalCommandResultMCP || result.LocalResult.Value != "list" {
		t.Fatalf("local result = %#v", result.LocalResult)
	}
	if len(result.Messages) != 1 || result.Messages[0].UUID != "user_mcp" {
		t.Fatalf("messages = %#v", result.Messages)
	}
	if text := result.Messages[0].Content[0].Text; !strings.Contains(text, "<command-name>/mcp</command-name>") || !strings.Contains(text, "<command-args>list</command-args>") {
		t.Fatalf("mcp command message = %q", text)
	}
}

func TestExecuteSlashConfigReturnsLocalConfigResult(t *testing.T) {
	registry := FromSources(Sources{Builtins: BuiltinCommands()})
	result, handled, err := ExecuteSlashCommand(registry, "/settings show", SlashOptions{UUID: "user_config"})
	if err != nil {
		t.Fatal(err)
	}
	if !handled || result.ShouldQuery || result.Unsupported || result.LocalResult == nil {
		t.Fatalf("handled=%v result=%#v", handled, result)
	}
	if result.LocalResult.Type != LocalCommandResultConfig || result.LocalResult.Value != "show" {
		t.Fatalf("local result = %#v", result.LocalResult)
	}
	if len(result.Messages) != 1 || result.Messages[0].UUID != "user_config" {
		t.Fatalf("messages = %#v", result.Messages)
	}
	if text := result.Messages[0].Content[0].Text; !strings.Contains(text, "<command-name>/config</command-name>") || !strings.Contains(text, "<command-args>show</command-args>") {
		t.Fatalf("config command message = %q", text)
	}
}

func TestExecuteSlashPluginReturnsLocalPluginResult(t *testing.T) {
	registry := FromSources(Sources{Builtins: BuiltinCommands()})
	result, handled, err := ExecuteSlashCommand(registry, "/plugin list", SlashOptions{UUID: "user_plugin"})
	if err != nil {
		t.Fatal(err)
	}
	if !handled || result.ShouldQuery || result.Unsupported || result.LocalResult == nil {
		t.Fatalf("handled=%v result=%#v", handled, result)
	}
	if result.LocalResult.Type != LocalCommandResultPlugin || result.LocalResult.Value != "list" {
		t.Fatalf("local result = %#v", result.LocalResult)
	}
	if len(result.Messages) != 1 || result.Messages[0].UUID != "user_plugin" {
		t.Fatalf("messages = %#v", result.Messages)
	}
	if text := result.Messages[0].Content[0].Text; !strings.Contains(text, "<command-name>/plugin</command-name>") || !strings.Contains(text, "<command-args>list</command-args>") {
		t.Fatalf("plugin command message = %q", text)
	}
}

func TestExecuteSlashMemoryReturnsLocalMemoryResult(t *testing.T) {
	registry := FromSources(Sources{Builtins: BuiltinCommands()})
	result, handled, err := ExecuteSlashCommand(registry, "/memory status", SlashOptions{UUID: "user_memory"})
	if err != nil {
		t.Fatal(err)
	}
	if !handled || result.ShouldQuery || result.Unsupported || result.LocalResult == nil {
		t.Fatalf("handled=%v result=%#v", handled, result)
	}
	if result.LocalResult.Type != LocalCommandResultMemory || result.LocalResult.Value != "status" {
		t.Fatalf("local result = %#v", result.LocalResult)
	}
	if len(result.Messages) != 1 || result.Messages[0].UUID != "user_memory" {
		t.Fatalf("messages = %#v", result.Messages)
	}
	if text := result.Messages[0].Content[0].Text; !strings.Contains(text, "<command-name>/memory</command-name>") || !strings.Contains(text, "<command-args>status</command-args>") {
		t.Fatalf("memory command message = %q", text)
	}
}

func TestExecuteSlashCompactReturnsLocalCompactResult(t *testing.T) {
	registry := FromSources(Sources{Builtins: BuiltinCommands()})
	result, handled, err := ExecuteSlashCommand(registry, "/compact focus on API", SlashOptions{UUID: "user_compact"})
	if err != nil {
		t.Fatal(err)
	}
	if !handled || result.ShouldQuery || result.Unsupported || result.LocalResult == nil {
		t.Fatalf("handled=%v result=%#v", handled, result)
	}
	if result.LocalResult.Type != LocalCommandResultCompact || result.LocalResult.Value != "focus on API" {
		t.Fatalf("local result = %#v", result.LocalResult)
	}
	if len(result.Messages) != 1 || result.Messages[0].UUID != "user_compact" {
		t.Fatalf("messages = %#v", result.Messages)
	}
	text := result.Messages[0].Content[0].Text
	if !strings.Contains(text, "<command-name>/compact</command-name>") || !strings.Contains(text, "<command-args>focus on API</command-args>") {
		t.Fatalf("compact command message = %q", text)
	}
}

func TestExecuteSlashSkillsReturnsLocalTextResult(t *testing.T) {
	registry := FromSources(Sources{
		ProjectSkillPrompts: []PromptTemplate{{
			Command: contracts.Command{
				Name:        "deploy",
				Type:        contracts.CommandPrompt,
				Source:      contracts.CommandSourceSkills,
				Description: "Deploy service",
			},
			Content: "Deploy $ARGUMENTS.",
		}},
		Builtins: BuiltinCommands(),
	})
	result, handled, err := ExecuteSlashCommand(registry, "/skills", SlashOptions{UUID: "user_skills"})
	if err != nil {
		t.Fatal(err)
	}
	if !handled || result.ShouldQuery || result.Unsupported || result.LocalResult == nil {
		t.Fatalf("handled=%v result=%#v", handled, result)
	}
	if result.LocalResult.Type != LocalCommandResultText {
		t.Fatalf("local result = %#v", result.LocalResult)
	}
	if len(result.Messages) != 2 || result.Messages[0].UUID != "user_skills" {
		t.Fatalf("messages = %#v", result.Messages)
	}
	if text := result.Messages[1].Content[0].Text; !strings.Contains(text, "Available skills:") || !strings.Contains(text, "/deploy - Deploy service") {
		t.Fatalf("skills text = %q", text)
	}
}

func TestExecuteSlashSkillsShowReturnsDetail(t *testing.T) {
	registry := FromSources(Sources{
		ProjectSkillPrompts: []PromptTemplate{{
			Command: contracts.Command{
				Name:          "deploy",
				DisplayName:   "Deploy Helper",
				Type:          contracts.CommandPrompt,
				Source:        contracts.CommandSourceSkills,
				LoadedFrom:    "skills",
				Description:   "Deploy service",
				ArgumentHint:  "[service]",
				ArgumentNames: []string{"service"},
				AllowedTools:  []string{"Read", "Bash(git status:*)"},
				Model:         "opus",
				SkillRoot:     "/tmp/deploy",
				UserConfig:    map[string]any{"env": "prod", "region": "iad"},
			},
			Content: "Deploy $service.",
		}},
		Builtins: BuiltinCommands(),
	})
	result, handled, err := ExecuteSlashCommand(registry, "/skills show deploy", SlashOptions{UUID: "user_skill_detail"})
	if err != nil {
		t.Fatal(err)
	}
	if !handled || result.ShouldQuery || result.Unsupported || result.LocalResult == nil {
		t.Fatalf("handled=%v result=%#v", handled, result)
	}
	text := result.Messages[1].Content[0].Text
	for _, want := range []string{
		"Skill /Deploy Helper",
		"Name: /deploy",
		"Type: prompt",
		"Source: skills",
		"Loaded from: skills",
		"Description: Deploy service",
		"Argument hint: [service]",
		"Arguments: service",
		"Allowed tools: Read, Bash(git status:*)",
		"Model: opus",
		"Skill root: /tmp/deploy",
		"User config keys: env, region",
	} {
		if !strings.Contains(text, want) {
			t.Fatalf("skill detail missing %q in %q", want, text)
		}
	}
}

func TestExecuteSlashSkillsSearchesSkills(t *testing.T) {
	registry := FromSources(Sources{
		ProjectSkillPrompts: []PromptTemplate{
			{
				Command: contracts.Command{
					Name:        "deploy",
					Type:        contracts.CommandPrompt,
					Source:      contracts.CommandSourceSkills,
					Description: "Deploy service",
					WhenToUse:   "Use for release rollout",
				},
				Content: "Deploy $ARGUMENTS.",
			},
			{
				Command: contracts.Command{
					Name:        "review",
					Type:        contracts.CommandPrompt,
					Source:      contracts.CommandSourceSkills,
					Description: "Review code",
				},
				Content: "Review $ARGUMENTS.",
			},
		},
		Builtins: BuiltinCommands(),
	})
	result, handled, err := ExecuteSlashCommand(registry, "/skills search release", SlashOptions{UUID: "user_skill_search"})
	if err != nil {
		t.Fatal(err)
	}
	if !handled || result.ShouldQuery || result.Unsupported || result.LocalResult == nil {
		t.Fatalf("handled=%v result=%#v", handled, result)
	}
	text := result.Messages[1].Content[0].Text
	for _, want := range []string{
		"Skills search: release",
		"Matches: 1",
		"/deploy - Deploy service",
	} {
		if !strings.Contains(text, want) {
			t.Fatalf("skill search missing %q: %q", want, text)
		}
	}
	if strings.Contains(text, "/review") || strings.Contains(text, "/status") {
		t.Fatalf("skill search included unrelated command: %q", text)
	}

	result, handled, err = ExecuteSlashCommand(registry, "/skills search", SlashOptions{UUID: "user_skill_search_empty"})
	if err != nil {
		t.Fatal(err)
	}
	if !handled || result.LocalResult == nil || result.Messages[1].Content[0].Text != "Usage: /skills search <query>" {
		t.Fatalf("empty skill search = %#v", result)
	}

	result, handled, err = ExecuteSlashCommand(registry, "/skills search nowhere", SlashOptions{UUID: "user_skill_search_none"})
	if err != nil {
		t.Fatal(err)
	}
	if !handled || result.LocalResult == nil || result.Messages[1].Content[0].Text != "No skills matched nowhere." {
		t.Fatalf("missing skill search = %#v", result)
	}
}

func TestExecuteSlashOutputStyleReturnsDeprecatedTextResult(t *testing.T) {
	registry := FromSources(Sources{Builtins: BuiltinCommands()})
	result, handled, err := ExecuteSlashCommand(registry, "/output-style", SlashOptions{UUID: "user_output_style"})
	if err != nil {
		t.Fatal(err)
	}
	if !handled || result.ShouldQuery || result.Unsupported || result.LocalResult == nil {
		t.Fatalf("handled=%v result=%#v", handled, result)
	}
	if result.LocalResult.Type != LocalCommandResultText {
		t.Fatalf("local result = %#v", result.LocalResult)
	}
	if len(result.Messages) != 2 || result.Messages[0].UUID != "user_output_style" {
		t.Fatalf("messages = %#v", result.Messages)
	}
	if text := result.Messages[1].Content[0].Text; !strings.Contains(text, "/output-style has been deprecated") || !strings.Contains(text, "/config") {
		t.Fatalf("output-style text = %q", text)
	}
}

func TestExecuteSlashCostReturnsLocalCostResult(t *testing.T) {
	registry := FromSources(Sources{Builtins: BuiltinCommands()})
	result, handled, err := ExecuteSlashCommand(registry, "/cost breakdown", SlashOptions{UUID: "user_cost"})
	if err != nil {
		t.Fatal(err)
	}
	if !handled || result.ShouldQuery || result.Unsupported || result.LocalResult == nil {
		t.Fatalf("handled=%v result=%#v", handled, result)
	}
	if result.LocalResult.Type != LocalCommandResultCost {
		t.Fatalf("local result = %#v", result.LocalResult)
	}
	if result.LocalResult.Value != "breakdown" {
		t.Fatalf("local result value = %q", result.LocalResult.Value)
	}
	if len(result.Messages) != 1 || result.Messages[0].UUID != "user_cost" {
		t.Fatalf("messages = %#v", result.Messages)
	}
	if text := result.Messages[0].Content[0].Text; !strings.Contains(text, "<command-name>/cost</command-name>") {
		t.Fatalf("cost command message = %q", text)
	}
}

func TestExecuteSlashBridgeSafeLocalCommandsReturnLocalResults(t *testing.T) {
	registry := FromSources(Sources{Builtins: BuiltinCommands()})
	tests := []struct {
		input string
		typ   LocalCommandResultType
		value string
	}{
		{input: "/summary recent work", typ: LocalCommandResultSummary, value: "recent work"},
		{input: "/release-notes plugins", typ: LocalCommandResultRelease, value: "plugins"},
		{input: "/files src", typ: LocalCommandResultFiles, value: "src"},
	}
	for _, tt := range tests {
		t.Run(tt.input, func(t *testing.T) {
			result, handled, err := ExecuteSlashCommand(registry, tt.input, SlashOptions{UUID: "user_local"})
			if err != nil {
				t.Fatal(err)
			}
			if !handled || result.ShouldQuery || result.Unsupported || result.LocalResult == nil {
				t.Fatalf("handled=%v result=%#v", handled, result)
			}
			if result.LocalResult.Type != tt.typ || result.LocalResult.Value != tt.value {
				t.Fatalf("local result = %#v", result.LocalResult)
			}
			if len(result.Messages) != 1 || result.Messages[0].UUID != "user_local" {
				t.Fatalf("messages = %#v", result.Messages)
			}
			if text := result.Messages[0].Content[0].Text; !strings.Contains(text, "<command-name>/"+strings.TrimPrefix(strings.Fields(tt.input)[0], "/")+"</command-name>") {
				t.Fatalf("command message = %q", text)
			}
		})
	}
}

func TestExecuteSlashIssueReturnsLocalIssueResult(t *testing.T) {
	registry := FromSources(Sources{Builtins: BuiltinCommands()})
	result, handled, err := ExecuteSlashCommand(registry, "/issue auth failed", SlashOptions{UUID: "user_issue"})
	if err != nil {
		t.Fatal(err)
	}
	if !handled || result.ShouldQuery || result.Unsupported || result.LocalResult == nil {
		t.Fatalf("handled=%v result=%#v", handled, result)
	}
	if result.LocalResult.Type != LocalCommandResultIssue {
		t.Fatalf("local result = %#v", result.LocalResult)
	}
	if result.LocalResult.Value != "auth failed" {
		t.Fatalf("local result value = %q", result.LocalResult.Value)
	}
	if len(result.Messages) != 1 || result.Messages[0].UUID != "user_issue" {
		t.Fatalf("messages = %#v", result.Messages)
	}
	if text := result.Messages[0].Content[0].Text; !strings.Contains(text, "<command-name>/issue</command-name>") {
		t.Fatalf("issue command message = %q", text)
	}
}

func TestExecuteSlashStatusReturnsLocalStatusResult(t *testing.T) {
	registry := FromSources(Sources{Builtins: BuiltinCommands()})
	result, handled, err := ExecuteSlashCommand(registry, "/status show tools", SlashOptions{UUID: "user_status"})
	if err != nil {
		t.Fatal(err)
	}
	if !handled || result.ShouldQuery || result.Unsupported || result.LocalResult == nil {
		t.Fatalf("handled=%v result=%#v", handled, result)
	}
	if result.LocalResult.Type != LocalCommandResultStatus {
		t.Fatalf("local result = %#v", result.LocalResult)
	}
	if result.LocalResult.Value != "show tools" {
		t.Fatalf("local result value = %q", result.LocalResult.Value)
	}
	if len(result.Messages) != 1 || result.Messages[0].UUID != "user_status" {
		t.Fatalf("messages = %#v", result.Messages)
	}
	if text := result.Messages[0].Content[0].Text; !strings.Contains(text, "<command-name>/status</command-name>") {
		t.Fatalf("status command message = %q", text)
	}
}

func TestExecuteSlashNativeReturnsLocalNativeResult(t *testing.T) {
	registry := FromSources(Sources{Builtins: BuiltinCommands()})
	result, handled, err := ExecuteSlashCommand(registry, "/native clipboard write hello", SlashOptions{UUID: "user_native"})
	if err != nil {
		t.Fatal(err)
	}
	if !handled || result.ShouldQuery || result.Unsupported || result.LocalResult == nil {
		t.Fatalf("handled=%v result=%#v", handled, result)
	}
	if result.LocalResult.Type != LocalCommandResultNative || result.LocalResult.Value != "clipboard write hello" {
		t.Fatalf("local result = %#v", result.LocalResult)
	}
	if len(result.Messages) != 1 || result.Messages[0].UUID != "user_native" {
		t.Fatalf("messages = %#v", result.Messages)
	}
	if text := result.Messages[0].Content[0].Text; !strings.Contains(text, "<command-name>/native</command-name>") || !strings.Contains(text, "<command-args>clipboard write hello</command-args>") {
		t.Fatalf("native command message = %q", text)
	}
}

func TestExecuteSlashModelReturnsLocalModelResult(t *testing.T) {
	registry := FromSources(Sources{Builtins: BuiltinCommands()})
	result, handled, err := ExecuteSlashCommand(registry, "/model opus", SlashOptions{UUID: "user_model"})
	if err != nil {
		t.Fatal(err)
	}
	if !handled || result.ShouldQuery || result.Unsupported || result.LocalResult == nil {
		t.Fatalf("handled=%v result=%#v", handled, result)
	}
	if result.LocalResult.Type != LocalCommandResultModel || result.LocalResult.Value != "opus" {
		t.Fatalf("local result = %#v", result.LocalResult)
	}
	if len(result.Messages) != 1 || result.Messages[0].UUID != "user_model" {
		t.Fatalf("messages = %#v", result.Messages)
	}
	if text := result.Messages[0].Content[0].Text; !strings.Contains(text, "<command-name>/model</command-name>") || !strings.Contains(text, "<command-args>opus</command-args>") {
		t.Fatalf("model command message = %q", text)
	}
}

func TestExecuteSlashResumeReturnsLocalResumeResult(t *testing.T) {
	registry := FromSources(Sources{Builtins: BuiltinCommands()})
	result, handled, err := ExecuteSlashCommand(registry, "/resume deploy", SlashOptions{UUID: "user_resume"})
	if err != nil {
		t.Fatal(err)
	}
	if !handled || result.ShouldQuery || result.Unsupported || result.LocalResult == nil {
		t.Fatalf("handled=%v result=%#v", handled, result)
	}
	if result.LocalResult.Type != LocalCommandResultResume || result.LocalResult.Value != "deploy" {
		t.Fatalf("local result = %#v", result.LocalResult)
	}
	if len(result.Messages) != 1 || result.Messages[0].UUID != "user_resume" {
		t.Fatalf("messages = %#v", result.Messages)
	}
	if text := result.Messages[0].Content[0].Text; !strings.Contains(text, "<command-name>/resume</command-name>") || !strings.Contains(text, "<command-args>deploy</command-args>") {
		t.Fatalf("resume command message = %q", text)
	}
}
