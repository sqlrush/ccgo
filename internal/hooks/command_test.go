package hooks

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
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

func TestHTTPHookPostsInputAndParsesJSONResponse(t *testing.T) {
	t.Setenv("HOOK_TOKEN", "secret-token")
	var sawAuth string
	var sawInput struct {
		HookEventName string          `json:"hook_event_name"`
		ToolName      string          `json:"tool_name"`
		ToolInput     json.RawMessage `json:"tool_input"`
	}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		sawAuth = r.Header.Get("Authorization")
		if err := json.NewDecoder(r.Body).Decode(&sawInput); err != nil {
			t.Fatal(err)
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"hookSpecificOutput":{"hookEventName":"PreToolUse","updatedInput":{"text":"from-http"}},"systemMessage":"http updated"}`))
	}))
	defer server.Close()
	settings := contracts.Settings{
		AllowedHTTPHookURLs:    []string{server.URL + "/*"},
		HTTPHookAllowedEnvVars: []string{"HOOK_TOKEN"},
		Hooks: map[string]any{
			"PreToolUse": []any{map[string]any{
				"matcher": "Echo",
				"hooks": []any{map[string]any{
					"type":           "http",
					"url":            server.URL + "/hook",
					"headers":        map[string]any{"Authorization": "Bearer $HOOK_TOKEN", "X-Blocked": "$HOME"},
					"allowedEnvVars": []any{"HOOK_TOKEN", "HOME"},
				}},
			}},
		},
	}
	parsed := FromSettings(settings)
	if len(parsed) != 1 {
		t.Fatalf("hooks = %#v", parsed)
	}
	result, err := parsed[0].RunToolHook(tool.Context{
		Context:          context.Background(),
		WorkingDirectory: t.TempDir(),
		SessionID:        "sess_http_hook",
	}, tool.HookEvent{
		Phase:    tool.HookPreToolUse,
		ToolUse:  contracts.ToolUse{ID: "toolu_http", Name: "Echo"},
		ToolName: "Echo",
		Input:    json.RawMessage(`{"text":"original"}`),
	})
	if err != nil {
		t.Fatal(err)
	}
	if sawAuth != "Bearer secret-token" {
		t.Fatalf("authorization = %q", sawAuth)
	}
	if sawInput.HookEventName != tool.HookPreToolUse || sawInput.ToolName != "Echo" || strings.TrimSpace(string(sawInput.ToolInput)) != `{"text":"original"}` {
		t.Fatalf("saw input = %#v", sawInput)
	}
	if result.Message != "http updated" || strings.TrimSpace(string(result.UpdatedInput)) != `{"text":"from-http"}` {
		t.Fatalf("result = %#v", result)
	}
}

func TestHTTPHookRespectsAllowedURLPatterns(t *testing.T) {
	called := false
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		called = true
	}))
	defer server.Close()
	hook := HTTPHook{
		Phase:              tool.HookPreToolUse,
		Matcher:            "Echo",
		URL:                server.URL + "/hook",
		AllowedURLPatterns: []string{"https://hooks.example.com/*"},
		Timeout:            defaultHTTPHookTimeout,
	}
	result, err := hook.RunToolHook(tool.Context{Context: context.Background(), WorkingDirectory: t.TempDir()}, tool.HookEvent{
		Phase:    tool.HookPreToolUse,
		ToolUse:  contracts.ToolUse{ID: "toolu_http_block", Name: "Echo"},
		ToolName: "Echo",
		Input:    json.RawMessage(`{}`),
	})
	if err != nil {
		t.Fatal(err)
	}
	if called {
		t.Fatalf("blocked HTTP hook should not call server")
	}
	if result.Metadata["error"] == nil || !strings.Contains(result.Metadata["error"].(string), "does not match") {
		t.Fatalf("result = %#v", result)
	}
}

func TestHTTPHeaderInterpolationSanitizesControlCharacters(t *testing.T) {
	t.Setenv("HOOK_TOKEN", "a\r\nbc")
	hook := HTTPHook{AllowedEnvVars: []string{"HOOK_TOKEN"}}
	if got := hook.interpolateHeader("Bearer ${HOOK_TOKEN}\x00 $MISSING"); got != "Bearer abc " {
		t.Fatalf("header = %q", got)
	}
}
