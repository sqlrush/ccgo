package commands

import (
	"strings"
	"testing"

	"ccgo/internal/contracts"
)

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

func TestExecuteSlashLocalCommandReturnsUnsupportedOutput(t *testing.T) {
	registry := FromSources(Sources{Builtins: BuiltinCommands()})
	result, handled, err := ExecuteSlashCommand(registry, "/status now", SlashOptions{UUID: "user_status"})
	if err != nil {
		t.Fatal(err)
	}
	if !handled || result.ShouldQuery || !result.Unsupported {
		t.Fatalf("handled=%v result=%#v", handled, result)
	}
	if len(result.Messages) != 2 || result.Messages[0].UUID != "user_status" {
		t.Fatalf("messages = %#v", result.Messages)
	}
	if !strings.Contains(result.Messages[1].Content[0].Text, "<local-command-stderr>") {
		t.Fatalf("stderr message = %#v", result.Messages[1])
	}
}

func TestExecuteSlashClearReturnsLocalTextResult(t *testing.T) {
	registry := FromSources(Sources{Builtins: BuiltinCommands()})
	result, handled, err := ExecuteSlashCommand(registry, "/clear", SlashOptions{UUID: "user_clear"})
	if err != nil {
		t.Fatal(err)
	}
	if !handled || result.ShouldQuery || result.Unsupported || result.LocalResult == nil {
		t.Fatalf("handled=%v result=%#v", handled, result)
	}
	if result.LocalResult.Type != LocalCommandResultText || result.LocalResult.Value != "" {
		t.Fatalf("local result = %#v", result.LocalResult)
	}
	if len(result.Messages) != 1 || result.Messages[0].UUID != "user_clear" {
		t.Fatalf("messages = %#v", result.Messages)
	}
	if text := result.Messages[0].Content[0].Text; !strings.Contains(text, "<command-name>/clear</command-name>") {
		t.Fatalf("clear command message = %q", text)
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
