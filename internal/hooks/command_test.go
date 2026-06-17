package hooks

import (
	"context"
	"encoding/json"
	"strings"
	"testing"

	"ccgo/internal/contracts"
	"ccgo/internal/tool"
)

func TestCommandHookUpdatesPreToolUseInput(t *testing.T) {
	hook := CommandHook{
		Phase:   tool.HookPreToolUse,
		Matcher: "Echo",
		Command: `printf '%s\n' '{"hookSpecificOutput":{"hookEventName":"PreToolUse","updatedInput":{"text":"from-command"}},"systemMessage":"updated"}'`,
	}
	result, err := hook.RunToolHook(tool.Context{
		Context:          context.Background(),
		WorkingDirectory: t.TempDir(),
		SessionID:        "sess_hook",
	}, tool.HookEvent{
		Phase:    tool.HookPreToolUse,
		ToolUse:  contracts.ToolUse{ID: "toolu_hook", Name: "Echo"},
		ToolName: "Echo",
		Input:    json.RawMessage(`{"text":"original"}`),
	})
	if err != nil {
		t.Fatal(err)
	}
	if result.Message != "updated" {
		t.Fatalf("message = %q", result.Message)
	}
	if strings.TrimSpace(string(result.UpdatedInput)) != `{"text":"from-command"}` {
		t.Fatalf("updated input = %s", result.UpdatedInput)
	}
}

func TestCommandHookBlocksOnExitCodeTwo(t *testing.T) {
	hook := CommandHook{
		Phase:   tool.HookPreToolUse,
		Matcher: "Write",
		Command: `printf '%s\n' 'blocked by policy' >&2; exit 2`,
	}
	result, err := hook.RunToolHook(tool.Context{
		Context:          context.Background(),
		WorkingDirectory: t.TempDir(),
	}, tool.HookEvent{
		Phase:    tool.HookPreToolUse,
		ToolUse:  contracts.ToolUse{ID: "toolu_block", Name: "Write"},
		ToolName: "Write",
		Input:    json.RawMessage(`{"path":"file.txt"}`),
	})
	if err != nil {
		t.Fatal(err)
	}
	if !result.Block || !strings.Contains(result.Message, "blocked by policy") {
		t.Fatalf("result = %#v", result)
	}
}

func TestFromSettingsParsesCommandHooksAndMatchesIfRule(t *testing.T) {
	settings := contracts.Settings{
		Hooks: map[string]any{
			"PreToolUse": []any{
				map[string]any{
					"matcher": "Bash|Read",
					"hooks": []any{
						map[string]any{"type": "command", "command": "printf ok", "if": "Bash(git status*)"},
					},
				},
			},
		},
	}
	parsed := FromSettings(settings)
	if len(parsed) != 1 {
		t.Fatalf("hooks = %#v", parsed)
	}
	hook, ok := parsed[0].(CommandHook)
	if !ok {
		t.Fatalf("hook type = %T", parsed[0])
	}
	if !hook.matchesIf(tool.HookEvent{ToolName: "Bash", Input: json.RawMessage(`{"command":"git status --short"}`)}, t.TempDir()) {
		t.Fatalf("expected if rule to match")
	}
	if hook.matchesIf(tool.HookEvent{ToolName: "Bash", Input: json.RawMessage(`{"command":"rm -rf build"}`)}, t.TempDir()) {
		t.Fatalf("expected if rule to reject")
	}
}
