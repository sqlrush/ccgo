package conversation

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"testing"
	"time"

	"ccgo/internal/api/anthropic"
	bridgepkg "ccgo/internal/bridge"
	"ccgo/internal/commands"
	compactpkg "ccgo/internal/compact"
	"ccgo/internal/config"
	"ccgo/internal/contracts"
	daemonpkg "ccgo/internal/daemon"
	integrationspkg "ccgo/internal/integrations"
	lsppkg "ccgo/internal/lsp"
	"ccgo/internal/mcp"
	"ccgo/internal/memory"
	"ccgo/internal/messages"
	modelpkg "ccgo/internal/model"
	nativepkg "ccgo/internal/native"
	"ccgo/internal/permissions"
	pluginpkg "ccgo/internal/plugins"
	remotepkg "ccgo/internal/remote"
	"ccgo/internal/session"
	telemetrypkg "ccgo/internal/telemetry"
	"ccgo/internal/tool"
	filetools "ccgo/internal/tools/file"
	searchtools "ccgo/internal/tools/searchtools"
	skilltools "ccgo/internal/tools/skill"
	tasktools "ccgo/internal/tools/task"
)

type fakeCall struct {
	response *anthropic.Response
	err      error
}

type fakeCountTokensCall struct {
	response *anthropic.CountTokensResponse
	err      error
}

type fakeClient struct {
	calls       []fakeCall
	requests    []anthropic.Request
	countCalls  []anthropic.CountTokensRequest
	countTokens []fakeCountTokensCall
	streams     [][]anthropic.StreamEvent
	streamErrs  []error
	dumpPath    string
	dumpCache   []anthropic.PromptDumpCacheEntry
}

type fakeRunnerMCPClient struct {
	tools      []mcp.RemoteTool
	callResult any
	calls      []fakeRunnerMCPCall
}

type fakeRunnerMCPCall struct {
	ServerName string
	ToolName   string
	Input      json.RawMessage
}

func (f *fakeClient) CreateMessage(ctx context.Context, req anthropic.Request) (*anthropic.Response, error) {
	f.requests = append(f.requests, req)
	if len(f.calls) == 0 {
		return nil, anthropic.APIError{StatusCode: http.StatusInternalServerError, Type: "test_error", Message: "no fake call configured"}
	}
	call := f.calls[0]
	f.calls = f.calls[1:]
	return call.response, call.err
}

func (f *fakeClient) CountTokens(ctx context.Context, req anthropic.CountTokensRequest) (*anthropic.CountTokensResponse, error) {
	f.countCalls = append(f.countCalls, req)
	if len(f.countTokens) == 0 {
		return nil, fmt.Errorf("no fake count tokens call configured")
	}
	call := f.countTokens[0]
	f.countTokens = f.countTokens[1:]
	return call.response, call.err
}

func (f *fakeClient) StreamMessages(ctx context.Context, req anthropic.Request, handle func(anthropic.StreamEvent) error) error {
	f.requests = append(f.requests, req)
	if len(f.streams) == 0 && len(f.streamErrs) == 0 {
		return anthropic.APIError{StatusCode: http.StatusInternalServerError, Type: "test_error", Message: "no fake stream configured"}
	}
	var events []anthropic.StreamEvent
	if len(f.streams) > 0 {
		events = f.streams[0]
		f.streams = f.streams[1:]
	}
	for _, event := range events {
		if err := handle(event); err != nil {
			return err
		}
	}
	if len(f.streamErrs) > 0 {
		err := f.streamErrs[0]
		f.streamErrs = f.streamErrs[1:]
		return err
	}
	return nil
}

func (f *fakeClient) CachedPromptDumpRequests() []anthropic.PromptDumpCacheEntry {
	return append([]anthropic.PromptDumpCacheEntry(nil), f.dumpCache...)
}

func (f *fakeClient) PromptDumpPath() string {
	return f.dumpPath
}

func (f *fakeRunnerMCPClient) ListTools(_ context.Context, serverName string) ([]mcp.RemoteTool, error) {
	return f.tools, nil
}

func (f *fakeRunnerMCPClient) CallTool(_ context.Context, serverName string, toolName string, input json.RawMessage) (any, error) {
	f.calls = append(f.calls, fakeRunnerMCPCall{
		ServerName: serverName,
		ToolName:   toolName,
		Input:      append(json.RawMessage(nil), input...),
	})
	return f.callResult, nil
}

func (f *fakeRunnerMCPClient) ListResources(_ context.Context, serverName string) ([]mcp.RemoteResource, error) {
	return nil, nil
}

func (f *fakeRunnerMCPClient) ListResourceTemplates(_ context.Context, serverName string) ([]mcp.RemoteResourceTemplate, error) {
	return nil, nil
}

func (f *fakeRunnerMCPClient) ReadResource(_ context.Context, serverName string, uri string) ([]mcp.ResourceContent, error) {
	return nil, nil
}

func (f *fakeRunnerMCPClient) SubscribeResource(_ context.Context, serverName string, uri string) error {
	return nil
}

func (f *fakeRunnerMCPClient) ListPrompts(_ context.Context, serverName string) ([]mcp.RemotePrompt, error) {
	return nil, nil
}

func (f *fakeRunnerMCPClient) GetPrompt(_ context.Context, serverName string, promptName string, arguments map[string]string) (mcp.PromptResult, error) {
	return mcp.PromptResult{}, nil
}

func TestRunnerExecutesToolUseAndContinuesConversation(t *testing.T) {
	registry, err := tool.NewRegistry(tool.FuncTool{
		DefinitionValue: contracts.ToolDefinition{
			Name:        "Echo",
			Description: "echoes text",
			ReadOnly:    true,
			InputSchema: contracts.JSONSchema{
				"type":     "object",
				"required": []any{"text"},
				"properties": map[string]any{
					"text": map[string]any{"type": "string"},
				},
			},
		},
		CallFunc: func(ctx tool.Context, raw json.RawMessage, sink tool.ProgressSink) (contracts.ToolResult, error) {
			var input struct {
				Text string `json:"text"`
			}
			if err := json.Unmarshal(raw, &input); err != nil {
				return contracts.ToolResult{}, err
			}
			return contracts.ToolResult{Content: "echo:" + input.Text}, nil
		},
	})
	if err != nil {
		t.Fatal(err)
	}

	client := &fakeClient{calls: []fakeCall{
		{response: &anthropic.Response{
			ID:         "msg_tool",
			Type:       "message",
			Role:       "assistant",
			Model:      "sonnet",
			StopReason: "tool_use",
			Content: []contracts.ContentBlock{{
				Type:  contracts.ContentToolUse,
				ID:    "toolu_1",
				Name:  "Echo",
				Input: json.RawMessage(`{"text":"hello"}`),
			}},
		}},
		{response: &anthropic.Response{
			ID:         "msg_done",
			Type:       "message",
			Role:       "assistant",
			Model:      "sonnet",
			StopReason: "end_turn",
			Content:    []contracts.ContentBlock{contracts.NewTextBlock("done")},
			Usage:      contracts.Usage{InputTokens: 5, OutputTokens: 2},
		}},
	}}
	transcriptPath := filepath.Join(t.TempDir(), "session.jsonl")
	runner := Runner{
		Client:      client,
		Tools:       tool.NewExecutor(registry),
		Model:       "sonnet",
		MaxTokens:   128,
		SessionID:   "sess_1",
		SessionPath: transcriptPath,
	}

	result, err := runner.RunTurn(context.Background(), nil, messages.UserText("run echo"))
	if err != nil {
		t.Fatal(err)
	}
	if result.Assistant.Content[0].Text != "done" {
		t.Fatalf("assistant = %#v", result.Assistant)
	}
	if len(result.ToolResults) != 1 || result.ToolResults[0].Content != "echo:hello" {
		t.Fatalf("tool results = %#v", result.ToolResults)
	}
	if len(client.requests) != 2 {
		t.Fatalf("requests = %d, want 2", len(client.requests))
	}
	last := client.requests[1].Messages[len(client.requests[1].Messages)-1]
	if last.Role != "user" || last.Content[0].Type != contracts.ContentToolResult || last.Content[0].ToolUseID != "toolu_1" {
		t.Fatalf("last api message = %#v", last)
	}
	entries, err := session.Load(transcriptPath)
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 4 {
		t.Fatalf("transcript entries = %d, want 4", len(entries))
	}
	if entries[1].Message.ParentUUID == nil {
		t.Fatalf("assistant transcript entry missing parent")
	}
}

func TestRunnerToolValidationMentionsUndiscoveredDeferredSchema(t *testing.T) {
	registry, err := tool.NewRegistry(
		tool.FuncTool{DefinitionValue: contracts.ToolDefinition{
			Name:        "DeferredArray",
			Description: "expects typed array input",
			ShouldDefer: true,
			InputSchema: contracts.JSONSchema{
				"type":     "object",
				"required": []any{"items"},
				"properties": map[string]any{
					"items": map[string]any{"type": "array", "items": map[string]any{"type": "string"}},
				},
			},
		}},
		tool.FuncTool{DefinitionValue: contracts.ToolDefinition{
			Name:        "ToolSearch",
			ReadOnly:    true,
			InputSchema: contracts.JSONSchema{"type": "object"},
		}},
	)
	if err != nil {
		t.Fatal(err)
	}
	client := &fakeClient{calls: []fakeCall{
		{response: &anthropic.Response{
			ID:         "msg_tool",
			Type:       "message",
			Role:       "assistant",
			Model:      "sonnet",
			StopReason: "tool_use",
			Content: []contracts.ContentBlock{{
				Type:  contracts.ContentToolUse,
				ID:    "toolu_deferred",
				Name:  "DeferredArray",
				Input: json.RawMessage(`{"items":"one"}`),
			}},
		}},
		{response: &anthropic.Response{
			ID:         "msg_done",
			Type:       "message",
			Role:       "assistant",
			Model:      "sonnet",
			StopReason: "end_turn",
			Content:    []contracts.ContentBlock{contracts.NewTextBlock("done")},
		}},
	}}
	runner := Runner{
		Client:    client,
		Tools:     tool.NewExecutor(registry),
		Model:     "sonnet",
		MaxTokens: 128,
	}
	result, err := runner.RunTurn(context.Background(), nil, messages.UserText("call deferred directly"))
	if err != nil {
		t.Fatal(err)
	}
	if len(result.ToolResults) != 1 || !result.ToolResults[0].IsError {
		t.Fatalf("tool results = %#v", result.ToolResults)
	}
	text := fmt.Sprint(result.ToolResults[0].Content)
	if !strings.Contains(text, "This tool's schema was not sent to the API") || !strings.Contains(text, `ToolSearch with query "select:DeferredArray"`) {
		t.Fatalf("tool result content = %q", text)
	}
}

func TestRunnerPreservesToolReferenceResultContent(t *testing.T) {
	registry, err := tool.NewRegistry(tool.FuncTool{
		DefinitionValue: contracts.ToolDefinition{
			Name:        "ToolSearch",
			Description: "selects tool references",
			ReadOnly:    true,
			InputSchema: contracts.JSONSchema{"type": "object"},
		},
		CallFunc: func(ctx tool.Context, raw json.RawMessage, sink tool.ProgressSink) (contracts.ToolResult, error) {
			return contracts.ToolResult{
				Content: []contracts.ToolReference{
					contracts.NewToolReference("Read"),
					contracts.NewToolReference("Edit"),
				},
				StructuredContent: map[string]any{
					"matches": 2,
				},
			}, nil
		},
	})
	if err != nil {
		t.Fatal(err)
	}

	client := &fakeClient{calls: []fakeCall{
		{response: &anthropic.Response{
			ID:         "msg_tool",
			Type:       "message",
			Role:       "assistant",
			Model:      "sonnet",
			StopReason: "tool_use",
			Content: []contracts.ContentBlock{{
				Type:  contracts.ContentToolUse,
				ID:    "toolu_search",
				Name:  "ToolSearch",
				Input: json.RawMessage(`{"query":"select:Read,Edit"}`),
			}},
		}},
		{response: &anthropic.Response{
			ID:         "msg_done",
			Type:       "message",
			Role:       "assistant",
			Model:      "sonnet",
			StopReason: "end_turn",
			Content:    []contracts.ContentBlock{contracts.NewTextBlock("done")},
		}},
	}}
	runner := Runner{
		Client:    client,
		Tools:     tool.NewExecutor(registry),
		Model:     "sonnet",
		MaxTokens: 128,
	}
	result, err := runner.RunTurn(context.Background(), nil, messages.UserText("find tools"))
	if err != nil {
		t.Fatal(err)
	}
	if len(result.ToolResults) != 1 {
		t.Fatalf("tool results = %#v", result.ToolResults)
	}
	references, ok := result.ToolResults[0].Content.([]contracts.ToolReference)
	if !ok || len(references) != 2 || references[0].ToolName != "Read" || references[1].ToolName != "Edit" {
		t.Fatalf("runner tool result content = %#v", result.ToolResults[0].Content)
	}
	if len(client.requests) != 2 {
		t.Fatalf("requests = %d, want 2", len(client.requests))
	}
	last := client.requests[1].Messages[len(client.requests[1].Messages)-1]
	if last.Role != "user" || len(last.Content) != 1 || last.Content[0].Type != contracts.ContentToolResult {
		t.Fatalf("last api message = %#v", last)
	}
	placeholder, ok := last.Content[0].Content.([]contracts.ContentBlock)
	if !ok || len(placeholder) != 1 || placeholder[0].Text != "[Tool references removed - tool search not enabled]" {
		t.Fatalf("tool result content = %#v", last.Content[0].Content)
	}
}

func TestRunnerExecutesSettingsCommandHookForToolUse(t *testing.T) {
	registry, err := tool.NewRegistry(tool.FuncTool{
		DefinitionValue: contracts.ToolDefinition{
			Name:     "Echo",
			ReadOnly: true,
			InputSchema: contracts.JSONSchema{
				"type":     "object",
				"required": []any{"text"},
				"properties": map[string]any{
					"text": map[string]any{"type": "string"},
				},
			},
		},
		CallFunc: func(ctx tool.Context, raw json.RawMessage, sink tool.ProgressSink) (contracts.ToolResult, error) {
			var input struct {
				Text string `json:"text"`
			}
			if err := json.Unmarshal(raw, &input); err != nil {
				return contracts.ToolResult{}, err
			}
			return contracts.ToolResult{Content: "echo:" + input.Text}, nil
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	client := &fakeClient{calls: []fakeCall{
		{response: &anthropic.Response{
			ID:         "msg_tool",
			Type:       "message",
			Role:       "assistant",
			Model:      "sonnet",
			StopReason: "tool_use",
			Content: []contracts.ContentBlock{{
				Type:  contracts.ContentToolUse,
				ID:    "toolu_hook",
				Name:  "Echo",
				Input: json.RawMessage(`{"text":"original"}`),
			}},
		}},
		{response: &anthropic.Response{
			ID:         "msg_done",
			Type:       "message",
			Role:       "assistant",
			Model:      "sonnet",
			StopReason: "end_turn",
			Content:    []contracts.ContentBlock{contracts.NewTextBlock("done")},
		}},
	}}
	var progress []contracts.ToolProgress
	runner := Runner{
		Client:           client,
		Tools:            tool.NewExecutor(registry),
		Model:            "sonnet",
		MaxTokens:        128,
		SessionID:        "sess_settings_hook",
		SessionPath:      filepath.Join(t.TempDir(), "session.jsonl"),
		WorkingDirectory: t.TempDir(),
		MCP: &MCPConfig{ProjectSettings: contracts.Settings{Hooks: map[string]any{
			"PreToolUse": []any{
				map[string]any{
					"matcher": "Echo",
					"hooks": []any{
						map[string]any{
							"type":    "command",
							"command": `printf '%s\n' '{"hookSpecificOutput":{"hookEventName":"PreToolUse","updatedInput":{"text":"from-command"}},"systemMessage":"updated"}'`,
						},
					},
				},
			},
		}}},
		OnEvent: func(event Event) {
			if event.Type == EventToolProgress && event.ToolProgress != nil {
				progress = append(progress, *event.ToolProgress)
			}
		},
	}
	result, err := runner.RunTurn(context.Background(), nil, messages.UserText("run echo"))
	if err != nil {
		t.Fatal(err)
	}
	if len(result.ToolResults) != 1 || result.ToolResults[0].Content != "echo:from-command" {
		t.Fatalf("tool results = %#v", result.ToolResults)
	}
	if !hasHookProgress(progress, tool.HookPreToolUse, "hook_completed") {
		t.Fatalf("hook progress = %#v", progress)
	}
}

func TestRunnerExecutesPluginCommandHookForToolUse(t *testing.T) {
	registry, err := tool.NewRegistry(namedTextTool("Echo", "echo:"))
	if err != nil {
		t.Fatal(err)
	}
	repo := filepath.Join(t.TempDir(), "repo")
	cwd := filepath.Join(repo, "pkg")
	pluginDir := filepath.Join(repo, ".claude", "plugins", "demo")
	if err := os.MkdirAll(filepath.Join(repo, ".git"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Join(pluginDir, "hooks"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(cwd, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(pluginDir, "plugin.json"), []byte(`{"name":"demo","version":"1.0.0"}`), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(pluginDir, "hooks", "hooks.json"), []byte(`{
		"hooks": {
			"PreToolUse": [{
				"matcher": "Echo",
				"hooks": [{
					"type": "command",
					"command": "printf '%s\n' '{\"hookSpecificOutput\":{\"hookEventName\":\"PreToolUse\",\"updatedInput\":{\"text\":\"from-plugin\"}},\"systemMessage\":\"plugin updated\"}'"
				}]
			}]
		}
	}`), 0o644); err != nil {
		t.Fatal(err)
	}
	client := &fakeClient{calls: []fakeCall{
		{response: &anthropic.Response{
			ID:         "msg_tool",
			Type:       "message",
			Role:       "assistant",
			Model:      "sonnet",
			StopReason: "tool_use",
			Content: []contracts.ContentBlock{{
				Type:  contracts.ContentToolUse,
				ID:    "toolu_plugin_hook",
				Name:  "Echo",
				Input: json.RawMessage(`{"text":"original"}`),
			}},
		}},
		{response: &anthropic.Response{
			ID:         "msg_done",
			Type:       "message",
			Role:       "assistant",
			Model:      "sonnet",
			StopReason: "end_turn",
			Content:    []contracts.ContentBlock{contracts.NewTextBlock("done")},
		}},
	}}
	var progress []contracts.ToolProgress
	runner := Runner{
		Client:           client,
		Tools:            tool.NewExecutor(registry),
		Model:            "sonnet",
		MaxTokens:        128,
		SessionID:        "sess_plugin_hook",
		SessionPath:      filepath.Join(t.TempDir(), "session.jsonl"),
		WorkingDirectory: cwd,
		MCP:              &MCPConfig{},
		OnEvent: func(event Event) {
			if event.Type == EventToolProgress && event.ToolProgress != nil {
				progress = append(progress, *event.ToolProgress)
			}
		},
	}
	result, err := runner.RunTurn(context.Background(), nil, messages.UserText("run echo"))
	if err != nil {
		t.Fatal(err)
	}
	if len(result.ToolResults) != 1 || result.ToolResults[0].Content != "echo:from-plugin" {
		t.Fatalf("tool results = %#v", result.ToolResults)
	}
	if !hasHookProgress(progress, tool.HookPreToolUse, "hook_completed") {
		t.Fatalf("hook progress = %#v", progress)
	}
}

func TestRunnerStrictPluginOnlyHooksSkipsProjectHooks(t *testing.T) {
	registry, err := tool.NewRegistry(namedTextTool("Echo", "echo:"))
	if err != nil {
		t.Fatal(err)
	}
	repo := filepath.Join(t.TempDir(), "repo")
	cwd := filepath.Join(repo, "pkg")
	pluginDir := filepath.Join(repo, ".claude", "plugins", "demo")
	if err := os.MkdirAll(filepath.Join(repo, ".git"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Join(pluginDir, "hooks"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(cwd, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(pluginDir, "plugin.json"), []byte(`{"name":"demo","version":"1.0.0"}`), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(pluginDir, "hooks", "hooks.json"), []byte(`{
		"hooks": {
			"PreToolUse": [{
				"matcher": "Echo",
				"hooks": [{
					"type": "command",
					"command": "printf '%s\n' '{\"hookSpecificOutput\":{\"hookEventName\":\"PreToolUse\",\"updatedInput\":{\"text\":\"from-plugin\"}}}'"
				}]
			}]
		}
	}`), 0o644); err != nil {
		t.Fatal(err)
	}
	client := &fakeClient{calls: []fakeCall{
		{response: &anthropic.Response{
			ID:         "msg_tool",
			Type:       "message",
			Role:       "assistant",
			Model:      "sonnet",
			StopReason: "tool_use",
			Content: []contracts.ContentBlock{{
				Type:  contracts.ContentToolUse,
				ID:    "toolu_plugin_only_hook",
				Name:  "Echo",
				Input: json.RawMessage(`{"text":"original"}`),
			}},
		}},
		{response: &anthropic.Response{
			ID:         "msg_done",
			Type:       "message",
			Role:       "assistant",
			Model:      "sonnet",
			StopReason: "end_turn",
			Content:    []contracts.ContentBlock{contracts.NewTextBlock("done")},
		}},
	}}
	runner := Runner{
		Client:           client,
		Tools:            tool.NewExecutor(registry),
		Model:            "sonnet",
		MaxTokens:        128,
		SessionID:        "sess_plugin_only_hook",
		SessionPath:      filepath.Join(t.TempDir(), "session.jsonl"),
		WorkingDirectory: cwd,
		MCP: &MCPConfig{
			ProjectSettings: contracts.Settings{Hooks: map[string]any{
				"PreToolUse": []any{map[string]any{
					"matcher": "Echo",
					"hooks": []any{map[string]any{
						"type":    "command",
						"command": `printf '%s\n' 'blocked project hook' >&2; exit 2`,
					}},
				}},
			}},
			PolicySettings: contracts.Settings{StrictPluginOnlyCustomization: []any{"hooks"}},
		},
	}
	result, err := runner.RunTurn(context.Background(), nil, messages.UserText("run echo"))
	if err != nil {
		t.Fatal(err)
	}
	if len(result.ToolResults) != 1 || result.ToolResults[0].Content != "echo:from-plugin" {
		t.Fatalf("tool results = %#v", result.ToolResults)
	}
}

func TestRunnerUserPromptSubmitHookAddsContext(t *testing.T) {
	client := &fakeClient{calls: []fakeCall{{response: &anthropic.Response{
		ID:         "msg_done",
		Type:       "message",
		Role:       "assistant",
		Model:      "sonnet",
		StopReason: "end_turn",
		Content:    []contracts.ContentBlock{contracts.NewTextBlock("done")},
	}}}}
	var progress []contracts.ToolProgress
	runner := Runner{
		Client:           client,
		Model:            "sonnet",
		MaxTokens:        128,
		SessionID:        "sess_prompt_hook",
		SessionPath:      filepath.Join(t.TempDir(), "session.jsonl"),
		WorkingDirectory: t.TempDir(),
		MCP: &MCPConfig{ProjectSettings: contracts.Settings{Hooks: map[string]any{
			"UserPromptSubmit": []any{map[string]any{
				"hooks": []any{map[string]any{
					"type":    "command",
					"command": `printf '%s\n' '{"hookSpecificOutput":{"hookEventName":"UserPromptSubmit","additionalContext":"context from prompt hook"}}'`,
				}},
			}},
		}}},
		OnEvent: func(event Event) {
			if event.Type == EventToolProgress && event.ToolProgress != nil {
				progress = append(progress, *event.ToolProgress)
			}
		},
	}
	if _, err := runner.RunTurn(context.Background(), nil, messages.UserText("hello")); err != nil {
		t.Fatal(err)
	}
	if len(client.requests) != 1 {
		t.Fatalf("requests = %#v", client.requests)
	}
	got := client.requests[0].Messages[0].Content[0].Text
	if !strings.Contains(got, "hello") || !strings.Contains(got, "context from prompt hook") {
		t.Fatalf("prompt = %q", got)
	}
	if !hasHookProgress(progress, tool.HookUserPromptSubmit, "hook_completed") {
		t.Fatalf("hook progress = %#v", progress)
	}
}

func TestRunnerStopHookRunsAfterAssistantCompletes(t *testing.T) {
	client := &fakeClient{calls: []fakeCall{{response: &anthropic.Response{
		ID:         "msg_done",
		Type:       "message",
		Role:       "assistant",
		Model:      "sonnet",
		StopReason: "end_turn",
		Content:    []contracts.ContentBlock{contracts.NewTextBlock("done")},
	}}}}
	var progress []contracts.ToolProgress
	runner := Runner{
		Client:           client,
		Model:            "sonnet",
		MaxTokens:        128,
		SessionID:        "sess_stop_hook",
		SessionPath:      filepath.Join(t.TempDir(), "session.jsonl"),
		WorkingDirectory: t.TempDir(),
		MCP: &MCPConfig{ProjectSettings: contracts.Settings{Hooks: map[string]any{
			"Stop": []any{map[string]any{
				"hooks": []any{map[string]any{
					"type":    "command",
					"command": `printf '%s\n' '{"hookSpecificOutput":{"hookEventName":"Stop","additionalContext":"stop observed"}}'`,
				}},
			}},
		}}},
		OnEvent: func(event Event) {
			if event.Type == EventToolProgress && event.ToolProgress != nil {
				progress = append(progress, *event.ToolProgress)
			}
		},
	}
	if _, err := runner.RunTurn(context.Background(), nil, messages.UserText("hello")); err != nil {
		t.Fatal(err)
	}
	if !hasHookProgress(progress, tool.HookStop, "hook_completed") {
		t.Fatalf("hook progress = %#v", progress)
	}
}

func TestRunnerRunsDueSchedulesBeforeMainRequest(t *testing.T) {
	transcriptPath := filepath.Join(t.TempDir(), "session.jsonl")
	sessionID := contracts.ID("sess_schedule_tick")
	manager := session.NewSidechainManager(transcriptPath, sessionID)
	if _, err := manager.Start(session.SidechainOptions{
		ID:          "agent/scheduled",
		AgentType:   "general-purpose",
		Description: "Scheduled task",
	}); err != nil {
		t.Fatal(err)
	}
	if _, _, err := manager.CreateTeam(session.TeamOptions{
		ID:          "ops/team",
		Description: "Ops team",
		TaskIDs:     []string{"agent/scheduled"},
	}); err != nil {
		t.Fatal(err)
	}
	if _, _, err := manager.UpsertSchedule(session.ScheduleOptions{
		ID:        "minute/check",
		Cron:      "* * * * *",
		Message:   "Check due work.",
		TeamID:    "ops/team",
		Enabled:   true,
		Timestamp: time.Date(2026, 6, 17, 9, 0, 0, 0, time.UTC),
	}); err != nil {
		t.Fatal(err)
	}
	client := &fakeClient{calls: []fakeCall{{response: &anthropic.Response{
		ID:         "msg_done",
		Type:       "message",
		Role:       "assistant",
		Model:      "sonnet",
		StopReason: "end_turn",
		Content:    []contracts.ContentBlock{contracts.NewTextBlock("done")},
	}}}}
	var progress []contracts.ToolProgress
	runner := Runner{
		Client:      client,
		Model:       "sonnet",
		MaxTokens:   128,
		SessionID:   sessionID,
		SessionPath: transcriptPath,
		OnEvent: func(event Event) {
			if event.Type == EventToolProgress && event.ToolProgress != nil {
				progress = append(progress, *event.ToolProgress)
			}
		},
	}
	if _, err := runner.RunTurn(context.Background(), nil, messages.UserText("continue")); err != nil {
		t.Fatal(err)
	}
	resume, err := manager.ResumeContext("agent/scheduled", 2)
	if err != nil {
		t.Fatal(err)
	}
	if len(resume.Messages) != 2 {
		t.Fatalf("scheduled sidechain messages = %#v", resume.Messages)
	}
	text := messages.TextContent(resume.Messages[1])
	if !strings.Contains(text, "Scheduled cron trigger received.") || !strings.Contains(text, "Schedule: minute_check") || !strings.Contains(text, "Check due work.") {
		t.Fatalf("scheduled tick message = %q", text)
	}
	if !hasScheduleDueTickProgress(progress) {
		t.Fatalf("schedule due progress = %#v", progress)
	}
}

func TestRunnerTaskToolStartsSidechainFromSessionMetadata(t *testing.T) {
	registry, err := tool.NewRegistry(tasktools.NewTaskTool())
	if err != nil {
		t.Fatal(err)
	}
	client := &fakeClient{calls: []fakeCall{
		{response: &anthropic.Response{
			ID:         "msg_task",
			Type:       "message",
			Role:       "assistant",
			Model:      "sonnet",
			StopReason: "tool_use",
			Content: []contracts.ContentBlock{{
				Type:  contracts.ContentToolUse,
				ID:    "toolu_task",
				Name:  "Task",
				Input: json.RawMessage(`{"id":"agent/api","description":"Review API","prompt":"Inspect the API changes","subagent_type":"general-purpose"}`),
			}},
		}},
		{response: &anthropic.Response{
			ID:         "msg_done",
			Type:       "message",
			Role:       "assistant",
			Model:      "sonnet",
			StopReason: "end_turn",
			Content:    []contracts.ContentBlock{contracts.NewTextBlock("task recorded")},
		}},
	}}
	transcriptPath := filepath.Join(t.TempDir(), "session.jsonl")
	cwd := t.TempDir()
	var progress []contracts.ToolProgress
	runner := Runner{
		Client:           client,
		Tools:            tool.NewExecutor(registry),
		Model:            "sonnet",
		MaxTokens:        128,
		SessionID:        "sess_task_runner",
		SessionPath:      transcriptPath,
		WorkingDirectory: cwd,
		OnEvent: func(event Event) {
			if event.Type == EventToolProgress && event.ToolProgress != nil {
				progress = append(progress, *event.ToolProgress)
			}
		},
	}

	result, err := runner.RunTurn(context.Background(), nil, messages.UserText("start task"))
	if err != nil {
		t.Fatal(err)
	}
	if len(result.ToolResults) != 1 {
		t.Fatalf("tool results = %#v", result.ToolResults)
	}
	if result.ToolResults[0].StructuredContent["sidechain_id"] != "agent_api" ||
		result.ToolResults[0].StructuredContent["status"] != session.SidechainStatusRunning {
		t.Fatalf("structured content = %#v", result.ToolResults[0].StructuredContent)
	}
	if len(client.requests) != 2 {
		t.Fatalf("requests = %d, want 2", len(client.requests))
	}
	last := client.requests[1].Messages[len(client.requests[1].Messages)-1]
	if last.Role != "user" || last.Content[0].Type != contracts.ContentToolResult || last.Content[0].ToolUseID != "toolu_task" {
		t.Fatalf("last api message = %#v", last)
	}

	states, err := session.ListSidechainStates(transcriptPath, "sess_task_runner")
	if err != nil {
		t.Fatal(err)
	}
	if len(states) != 1 {
		t.Fatalf("states = %#v", states)
	}
	state := states[0]
	if state.ID != "agent_api" || state.Status != session.SidechainStatusRunning || state.MessageCount != 2 {
		t.Fatalf("state = %#v", state)
	}
	if state.Metadata.AgentType != "general-purpose" || state.Metadata.Description != "Review API" || state.Metadata.WorktreePath != cwd {
		t.Fatalf("metadata = %#v", state.Metadata)
	}
	if !hasToolProgress(progress, "toolu_task", "task_started", "agent_api", session.SidechainStatusRunning) {
		t.Fatalf("progress = %#v", progress)
	}
}

func TestRunnerTaskToolRunExecutesOneShotSubagent(t *testing.T) {
	registry, err := tool.NewRegistry(tasktools.NewTaskTool())
	if err != nil {
		t.Fatal(err)
	}
	client := &fakeClient{calls: []fakeCall{
		{response: &anthropic.Response{
			ID:         "msg_task",
			Type:       "message",
			Role:       "assistant",
			Model:      "sonnet",
			StopReason: "tool_use",
			Content: []contracts.ContentBlock{{
				Type:  contracts.ContentToolUse,
				ID:    "toolu_task",
				Name:  "Task",
				Input: json.RawMessage(`{"id":"agent/run","description":"Run task","prompt":"Investigate and answer","subagent_type":"general-purpose","run":true}`),
			}},
		}},
		{response: &anthropic.Response{
			ID:         "msg_subagent",
			Type:       "message",
			Role:       "assistant",
			Model:      "sonnet",
			StopReason: "end_turn",
			Content:    []contracts.ContentBlock{contracts.NewTextBlock("Subagent done")},
		}},
		{response: &anthropic.Response{
			ID:         "msg_done",
			Type:       "message",
			Role:       "assistant",
			Model:      "sonnet",
			StopReason: "end_turn",
			Content:    []contracts.ContentBlock{contracts.NewTextBlock("task completed")},
		}},
	}}
	transcriptPath := filepath.Join(t.TempDir(), "session.jsonl")
	cwd := t.TempDir()
	var progress []contracts.ToolProgress
	runner := Runner{
		Client:           client,
		Tools:            tool.NewExecutor(registry),
		Model:            "sonnet",
		MaxTokens:        128,
		SessionID:        "sess_task_agent",
		SessionPath:      transcriptPath,
		WorkingDirectory: cwd,
		OnEvent: func(event Event) {
			if event.Type == EventToolProgress && event.ToolProgress != nil {
				progress = append(progress, *event.ToolProgress)
			}
		},
	}

	result, err := runner.RunTurn(context.Background(), nil, messages.UserText("start task"))
	if err != nil {
		t.Fatal(err)
	}
	if len(result.ToolResults) != 1 {
		t.Fatalf("tool results = %#v", result.ToolResults)
	}
	taskResult := result.ToolResults[0]
	if taskResult.IsError || taskResult.StructuredContent["status"] != session.SidechainStatusCompleted || taskResult.StructuredContent["summary"] != "Subagent done" {
		t.Fatalf("task result = %#v", taskResult)
	}
	if !strings.Contains(taskResult.Content.(string), "Task completed: agent_run") {
		t.Fatalf("task content = %#v", taskResult.Content)
	}
	if len(client.requests) != 3 {
		t.Fatalf("requests = %d, want 3", len(client.requests))
	}
	subagentRequest := client.requests[1]
	if !requestHasTool(subagentRequest, "Task") {
		t.Fatalf("subagent tools = %#v", subagentRequest.Tools)
	}
	if len(subagentRequest.Messages) != 1 || subagentRequest.Messages[0].Role != "user" || subagentRequest.Messages[0].Content[0].Text != "Investigate and answer" {
		t.Fatalf("subagent request messages = %#v", subagentRequest.Messages)
	}

	state, err := session.FindSidechainState(transcriptPath, "sess_task_agent", "agent/run")
	if err != nil {
		t.Fatal(err)
	}
	if state.Status != session.SidechainStatusCompleted || state.Summary != "Subagent done" || state.MessageCount != 4 {
		t.Fatalf("state = %#v", state)
	}
	if !hasToolProgress(progress, "toolu_task", "task_agent_completed", "agent_run", session.SidechainStatusCompleted) {
		t.Fatalf("progress = %#v", progress)
	}
}

func TestRunnerSubagentStopHookRunsAfterOneShotSubagent(t *testing.T) {
	registry, err := tool.NewRegistry(tasktools.NewTaskTool())
	if err != nil {
		t.Fatal(err)
	}
	client := &fakeClient{calls: []fakeCall{
		{response: &anthropic.Response{
			ID:         "msg_task",
			Type:       "message",
			Role:       "assistant",
			Model:      "sonnet",
			StopReason: "tool_use",
			Content: []contracts.ContentBlock{{
				Type:  contracts.ContentToolUse,
				ID:    "toolu_task",
				Name:  "Task",
				Input: json.RawMessage(`{"id":"agent/run","description":"Run task","prompt":"Investigate and answer","subagent_type":"general-purpose","run":true}`),
			}},
		}},
		{response: &anthropic.Response{
			ID:         "msg_subagent",
			Type:       "message",
			Role:       "assistant",
			Model:      "sonnet",
			StopReason: "end_turn",
			Content:    []contracts.ContentBlock{contracts.NewTextBlock("Subagent done")},
		}},
		{response: &anthropic.Response{
			ID:         "msg_done",
			Type:       "message",
			Role:       "assistant",
			Model:      "sonnet",
			StopReason: "end_turn",
			Content:    []contracts.ContentBlock{contracts.NewTextBlock("task completed")},
		}},
	}}
	transcriptPath := filepath.Join(t.TempDir(), "session.jsonl")
	var progress []contracts.ToolProgress
	runner := Runner{
		Client:           client,
		Tools:            tool.NewExecutor(registry),
		Model:            "sonnet",
		MaxTokens:        128,
		SessionID:        "sess_subagent_stop_hook",
		SessionPath:      transcriptPath,
		WorkingDirectory: t.TempDir(),
		MCP: &MCPConfig{ProjectSettings: contracts.Settings{Hooks: map[string]any{
			"SubagentStop": []any{map[string]any{
				"hooks": []any{map[string]any{
					"type":    "command",
					"command": `printf '%s\n' '{"hookSpecificOutput":{"hookEventName":"SubagentStop","additionalContext":"subagent stop observed"}}'`,
				}},
			}},
		}}},
		OnEvent: func(event Event) {
			if event.Type == EventToolProgress && event.ToolProgress != nil {
				progress = append(progress, *event.ToolProgress)
			}
		},
	}
	result, err := runner.RunTurn(context.Background(), nil, messages.UserText("start task"))
	if err != nil {
		t.Fatal(err)
	}
	if len(result.ToolResults) != 1 || result.ToolResults[0].StructuredContent["status"] != session.SidechainStatusCompleted {
		t.Fatalf("tool results = %#v", result.ToolResults)
	}
	if !hasHookProgress(progress, tool.HookSubagentStop, "hook_completed") {
		t.Fatalf("hook progress = %#v", progress)
	}
}

func TestRunnerTaskSubagentExecutesNestedToolLoop(t *testing.T) {
	registry, err := tool.NewRegistry(tasktools.NewTaskTool(), tool.FuncTool{
		DefinitionValue: contracts.ToolDefinition{
			Name:            "Echo",
			Description:     "Echo text",
			ReadOnly:        true,
			ConcurrencySafe: true,
			InputSchema: contracts.JSONSchema{
				"type":       "object",
				"properties": map[string]any{"text": map[string]any{"type": "string"}},
			},
		},
		CallFunc: func(ctx tool.Context, raw json.RawMessage, sink tool.ProgressSink) (contracts.ToolResult, error) {
			var input struct {
				Text string `json:"text"`
			}
			if err := json.Unmarshal(raw, &input); err != nil {
				return contracts.ToolResult{}, err
			}
			return contracts.ToolResult{Content: "echo:" + input.Text}, nil
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	client := &fakeClient{calls: []fakeCall{
		{response: &anthropic.Response{
			ID:         "msg_task",
			Type:       "message",
			Role:       "assistant",
			Model:      "sonnet",
			StopReason: "tool_use",
			Content: []contracts.ContentBlock{{
				Type:  contracts.ContentToolUse,
				ID:    "toolu_task",
				Name:  "Task",
				Input: json.RawMessage(`{"id":"agent/tools","description":"Run tools","prompt":"Use echo","subagent_type":"general-purpose","run":true}`),
			}},
		}},
		{response: &anthropic.Response{
			ID:         "msg_subagent_tool",
			Type:       "message",
			Role:       "assistant",
			Model:      "sonnet",
			StopReason: "tool_use",
			Content: []contracts.ContentBlock{{
				Type:  contracts.ContentToolUse,
				ID:    "toolu_echo",
				Name:  "Echo",
				Input: json.RawMessage(`{"text":"hello"}`),
			}},
		}},
		{response: &anthropic.Response{
			ID:         "msg_subagent_done",
			Type:       "message",
			Role:       "assistant",
			Model:      "sonnet",
			StopReason: "end_turn",
			Content:    []contracts.ContentBlock{contracts.NewTextBlock("Echo returned echo:hello")},
		}},
		{response: &anthropic.Response{
			ID:         "msg_done",
			Type:       "message",
			Role:       "assistant",
			Model:      "sonnet",
			StopReason: "end_turn",
			Content:    []contracts.ContentBlock{contracts.NewTextBlock("task completed")},
		}},
	}}
	transcriptPath := filepath.Join(t.TempDir(), "session.jsonl")
	runner := Runner{
		Client:           client,
		Tools:            tool.NewExecutor(registry),
		Model:            "sonnet",
		MaxTokens:        128,
		SessionID:        "sess_task_nested",
		SessionPath:      transcriptPath,
		WorkingDirectory: t.TempDir(),
	}

	result, err := runner.RunTurn(context.Background(), nil, messages.UserText("start task"))
	if err != nil {
		t.Fatal(err)
	}
	if len(result.ToolResults) != 1 || result.ToolResults[0].StructuredContent["summary"] != "Echo returned echo:hello" {
		t.Fatalf("tool results = %#v", result.ToolResults)
	}
	if len(client.requests) != 4 {
		t.Fatalf("requests = %d, want 4", len(client.requests))
	}
	subagentToolResultRequest := client.requests[2]
	last := subagentToolResultRequest.Messages[len(subagentToolResultRequest.Messages)-1]
	if last.Role != "user" || last.Content[0].Type != contracts.ContentToolResult || last.Content[0].ToolUseID != "toolu_echo" {
		t.Fatalf("subagent tool result request = %#v", subagentToolResultRequest.Messages)
	}
	state, err := session.FindSidechainState(transcriptPath, "sess_task_nested", "agent/tools")
	if err != nil {
		t.Fatal(err)
	}
	if state.Status != session.SidechainStatusCompleted || state.Summary != "Echo returned echo:hello" || state.MessageCount != 6 {
		t.Fatalf("state = %#v", state)
	}
	output, err := session.LoadTranscript(state.Path)
	if err != nil {
		t.Fatal(err)
	}
	var foundEchoResult bool
	for _, id := range output.Order {
		entry := output.Messages[id]
		if entry != nil && entry.Message != nil && len(entry.Message.Content) > 0 && entry.Message.Content[0].ToolUseID == "toolu_echo" {
			foundEchoResult = true
			break
		}
	}
	if !foundEchoResult {
		t.Fatalf("sidechain transcript missing nested tool result: %#v", output.Order)
	}
}

func TestRunnerTaskSubagentHonorsAgentAllowedTools(t *testing.T) {
	registry, err := tool.NewRegistry(tasktools.NewTaskTool(), namedTextTool("Echo", "echo:"), namedTextTool("Secret", "secret:"))
	if err != nil {
		t.Fatal(err)
	}
	repo := filepath.Join(t.TempDir(), "repo")
	cwd := filepath.Join(repo, "pkg")
	pluginDir := filepath.Join(repo, ".claude", "plugins", "demo")
	if err := os.MkdirAll(filepath.Join(repo, ".git"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Join(pluginDir, "agents"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(cwd, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(pluginDir, "plugin.json"), []byte(`{"name":"demo","version":"1.0.0"}`), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(pluginDir, "agents", "runner.md"), []byte("---\nname: runner\ndescription: Run with echo\ntools: Echo\n---\nUse only Echo."), 0o644); err != nil {
		t.Fatal(err)
	}
	client := &fakeClient{calls: []fakeCall{
		{response: &anthropic.Response{
			ID:         "msg_task",
			Type:       "message",
			Role:       "assistant",
			Model:      "sonnet",
			StopReason: "tool_use",
			Content: []contracts.ContentBlock{{
				Type:  contracts.ContentToolUse,
				ID:    "toolu_task",
				Name:  "Task",
				Input: json.RawMessage(`{"id":"agent/allowed","description":"Allowed tools","prompt":"Use echo","subagent_type":"demo:runner","run":true}`),
			}},
		}},
		{response: &anthropic.Response{
			ID:         "msg_subagent_tool",
			Type:       "message",
			Role:       "assistant",
			Model:      "sonnet",
			StopReason: "tool_use",
			Content: []contracts.ContentBlock{{
				Type:  contracts.ContentToolUse,
				ID:    "toolu_echo",
				Name:  "Echo",
				Input: json.RawMessage(`{"text":"hello"}`),
			}},
		}},
		{response: &anthropic.Response{
			ID:         "msg_subagent_done",
			Type:       "message",
			Role:       "assistant",
			Model:      "sonnet",
			StopReason: "end_turn",
			Content:    []contracts.ContentBlock{contracts.NewTextBlock("Echo allowed")},
		}},
		{response: &anthropic.Response{
			ID:         "msg_done",
			Type:       "message",
			Role:       "assistant",
			Model:      "sonnet",
			StopReason: "end_turn",
			Content:    []contracts.ContentBlock{contracts.NewTextBlock("task completed")},
		}},
	}}
	runner := Runner{
		Client:           client,
		Tools:            tool.NewExecutor(registry),
		Model:            "sonnet",
		MaxTokens:        128,
		SessionID:        "sess_task_allowed",
		SessionPath:      filepath.Join(t.TempDir(), "session.jsonl"),
		WorkingDirectory: cwd,
	}

	result, err := runner.RunTurn(context.Background(), nil, messages.UserText("start task"))
	if err != nil {
		t.Fatal(err)
	}
	if len(result.ToolResults) != 1 || result.ToolResults[0].StructuredContent["summary"] != "Echo allowed" {
		t.Fatalf("tool results = %#v", result.ToolResults)
	}
	if len(client.requests) != 4 {
		t.Fatalf("requests = %d, want 4", len(client.requests))
	}
	if !requestHasTool(client.requests[1], "Echo") || requestHasTool(client.requests[1], "Secret") || requestHasTool(client.requests[1], "Task") {
		t.Fatalf("subagent allowed tools = %#v", client.requests[1].Tools)
	}
	if !requestHasTool(client.requests[2], "Echo") || requestHasTool(client.requests[2], "Secret") || requestHasTool(client.requests[2], "Task") {
		t.Fatalf("subagent follow-up tools = %#v", client.requests[2].Tools)
	}
}

func TestRunnerTaskSubagentEnforcesAllowedBashPattern(t *testing.T) {
	var bashCommands []string
	bashTool := tool.FuncTool{
		DefinitionValue: contracts.ToolDefinition{
			Name:            "Bash",
			Description:     "Run bash",
			ReadOnly:        true,
			ConcurrencySafe: true,
			InputSchema: contracts.JSONSchema{
				"type":       "object",
				"properties": map[string]any{"command": map[string]any{"type": "string"}},
			},
		},
		CallFunc: func(ctx tool.Context, raw json.RawMessage, sink tool.ProgressSink) (contracts.ToolResult, error) {
			var input struct {
				Command string `json:"command"`
			}
			if err := json.Unmarshal(raw, &input); err != nil {
				return contracts.ToolResult{}, err
			}
			bashCommands = append(bashCommands, input.Command)
			return contracts.ToolResult{Content: input.Command}, nil
		},
	}
	registry, err := tool.NewRegistry(tasktools.NewTaskTool(), bashTool)
	if err != nil {
		t.Fatal(err)
	}
	repo := filepath.Join(t.TempDir(), "repo")
	cwd := filepath.Join(repo, "pkg")
	pluginDir := filepath.Join(repo, ".claude", "plugins", "demo")
	if err := os.MkdirAll(filepath.Join(repo, ".git"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Join(pluginDir, "agents"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(cwd, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(pluginDir, "plugin.json"), []byte(`{"name":"demo","version":"1.0.0"}`), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(pluginDir, "agents", "runner.md"), []byte("---\nname: runner\ndescription: Run with scoped bash\ntools: Bash(git status:*)\n---\nUse scoped Bash."), 0o644); err != nil {
		t.Fatal(err)
	}
	client := &fakeClient{calls: []fakeCall{
		{response: &anthropic.Response{
			ID:         "msg_task",
			Type:       "message",
			Role:       "assistant",
			Model:      "sonnet",
			StopReason: "tool_use",
			Content: []contracts.ContentBlock{{
				Type:  contracts.ContentToolUse,
				ID:    "toolu_task",
				Name:  "Task",
				Input: json.RawMessage(`{"id":"agent/bash-pattern","description":"Scoped bash","prompt":"Use bash","subagent_type":"demo:runner","worktree":false,"run":true}`),
			}},
		}},
		{response: &anthropic.Response{
			ID:         "msg_subagent_denied",
			Type:       "message",
			Role:       "assistant",
			Model:      "sonnet",
			StopReason: "tool_use",
			Content: []contracts.ContentBlock{{
				Type:  contracts.ContentToolUse,
				ID:    "toolu_bash_denied",
				Name:  "Bash",
				Input: json.RawMessage(`{"command":"ls"}`),
			}},
		}},
		{response: &anthropic.Response{
			ID:         "msg_subagent_allowed",
			Type:       "message",
			Role:       "assistant",
			Model:      "sonnet",
			StopReason: "tool_use",
			Content: []contracts.ContentBlock{{
				Type:  contracts.ContentToolUse,
				ID:    "toolu_bash_allowed",
				Name:  "Bash",
				Input: json.RawMessage(`{"command":"git status --short"}`),
			}},
		}},
		{response: &anthropic.Response{
			ID:         "msg_subagent_done",
			Type:       "message",
			Role:       "assistant",
			Model:      "sonnet",
			StopReason: "end_turn",
			Content:    []contracts.ContentBlock{contracts.NewTextBlock("bash scoped")},
		}},
		{response: &anthropic.Response{
			ID:         "msg_done",
			Type:       "message",
			Role:       "assistant",
			Model:      "sonnet",
			StopReason: "end_turn",
			Content:    []contracts.ContentBlock{contracts.NewTextBlock("task completed")},
		}},
	}}
	runner := Runner{
		Client:           client,
		Tools:            tool.NewExecutor(registry),
		Permissions:      tool.NewEnginePermissionDecider(permissions.NewEngine(contracts.PermissionContext{Mode: contracts.PermissionDefault})),
		Model:            "sonnet",
		MaxTokens:        128,
		SessionID:        "sess_task_bash_pattern",
		SessionPath:      filepath.Join(t.TempDir(), "session.jsonl"),
		WorkingDirectory: cwd,
	}

	result, err := runner.RunTurn(context.Background(), nil, messages.UserText("start task"))
	if err != nil {
		t.Fatal(err)
	}
	if len(result.ToolResults) != 1 || result.ToolResults[0].StructuredContent["summary"] != "bash scoped" {
		t.Fatalf("tool results = %#v", result.ToolResults)
	}
	if len(bashCommands) != 1 || bashCommands[0] != "git status --short" {
		t.Fatalf("bash commands = %#v, want only allowed command", bashCommands)
	}
}

func TestRunnerTaskSubagentHonorsAgentPermissionMode(t *testing.T) {
	editCalls := 0
	editTool := tool.FuncTool{
		DefinitionValue: contracts.ToolDefinition{
			Name:   "Edit",
			Strict: true,
			InputSchema: contracts.JSONSchema{
				"type":       "object",
				"properties": map[string]any{},
			},
		},
		CallFunc: func(ctx tool.Context, raw json.RawMessage, sink tool.ProgressSink) (contracts.ToolResult, error) {
			editCalls++
			return contracts.ToolResult{Content: "edited"}, nil
		},
	}
	registry, err := tool.NewRegistry(tasktools.NewTaskTool(), editTool)
	if err != nil {
		t.Fatal(err)
	}
	repo := filepath.Join(t.TempDir(), "repo")
	cwd := filepath.Join(repo, "pkg")
	pluginDir := filepath.Join(repo, ".claude", "plugins", "demo")
	if err := os.MkdirAll(filepath.Join(repo, ".git"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Join(pluginDir, "agents"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(cwd, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(pluginDir, "plugin.json"), []byte(`{"name":"demo","version":"1.0.0"}`), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(pluginDir, "agents", "runner.md"), []byte("---\nname: runner\ndescription: Run with bypass\npermissionMode: bypassPermissions\n---\nUse Edit."), 0o644); err != nil {
		t.Fatal(err)
	}
	client := &fakeClient{calls: []fakeCall{
		{response: &anthropic.Response{
			ID:         "msg_task",
			Type:       "message",
			Role:       "assistant",
			Model:      "sonnet",
			StopReason: "tool_use",
			Content: []contracts.ContentBlock{{
				Type:  contracts.ContentToolUse,
				ID:    "toolu_task",
				Name:  "Task",
				Input: json.RawMessage(`{"id":"agent/permission-mode","description":"Bypass edit","prompt":"Use edit","subagent_type":"demo:runner","worktree":false,"run":true}`),
			}},
		}},
		{response: &anthropic.Response{
			ID:         "msg_subagent_tool",
			Type:       "message",
			Role:       "assistant",
			Model:      "sonnet",
			StopReason: "tool_use",
			Content: []contracts.ContentBlock{{
				Type:  contracts.ContentToolUse,
				ID:    "toolu_edit",
				Name:  "Edit",
				Input: json.RawMessage(`{}`),
			}},
		}},
		{response: &anthropic.Response{
			ID:         "msg_subagent_done",
			Type:       "message",
			Role:       "assistant",
			Model:      "sonnet",
			StopReason: "end_turn",
			Content:    []contracts.ContentBlock{contracts.NewTextBlock("edit completed")},
		}},
		{response: &anthropic.Response{
			ID:         "msg_done",
			Type:       "message",
			Role:       "assistant",
			Model:      "sonnet",
			StopReason: "end_turn",
			Content:    []contracts.ContentBlock{contracts.NewTextBlock("task completed")},
		}},
	}}
	runner := Runner{
		Client: client,
		Tools:  tool.NewExecutor(registry),
		Permissions: tool.NewEnginePermissionDecider(permissions.NewEngine(contracts.PermissionContext{
			Mode:            contracts.PermissionDefault,
			BypassAvailable: true,
		})),
		Model:            "sonnet",
		MaxTokens:        128,
		SessionID:        "sess_task_permission_mode",
		SessionPath:      filepath.Join(t.TempDir(), "session.jsonl"),
		WorkingDirectory: cwd,
	}

	result, err := runner.RunTurn(context.Background(), nil, messages.UserText("start task"))
	if err != nil {
		t.Fatal(err)
	}
	if len(result.ToolResults) != 1 || result.ToolResults[0].StructuredContent["summary"] != "edit completed" {
		t.Fatalf("tool results = %#v", result.ToolResults)
	}
	if editCalls != 1 {
		t.Fatalf("edit calls = %d, want 1", editCalls)
	}
}

func TestRunnerTaskSubagentUsesAndCleansOwnedWorktree(t *testing.T) {
	var toolCWDs []string
	cwdTool := tool.FuncTool{
		DefinitionValue: contracts.ToolDefinition{
			Name:            "CWD",
			Description:     "Return cwd",
			ReadOnly:        true,
			ConcurrencySafe: true,
			InputSchema:     contracts.JSONSchema{"type": "object"},
		},
		CallFunc: func(ctx tool.Context, raw json.RawMessage, sink tool.ProgressSink) (contracts.ToolResult, error) {
			toolCWDs = append(toolCWDs, ctx.WorkingDirectory)
			return contracts.ToolResult{Content: ctx.WorkingDirectory}, nil
		},
	}
	registry, err := tool.NewRegistry(tasktools.NewTaskTool(), cwdTool)
	if err != nil {
		t.Fatal(err)
	}
	repo := initConversationGitRepo(t)
	sessionPath := filepath.Join(t.TempDir(), "session.jsonl")
	client := &fakeClient{calls: []fakeCall{
		{response: &anthropic.Response{
			ID:         "msg_task",
			Type:       "message",
			Role:       "assistant",
			Model:      "sonnet",
			StopReason: "tool_use",
			Content: []contracts.ContentBlock{{
				Type:  contracts.ContentToolUse,
				ID:    "toolu_task",
				Name:  "Task",
				Input: json.RawMessage(`{"id":"agent/worktree-run","description":"Run in worktree","prompt":"Report cwd","subagent_type":"general-purpose","worktree":true,"run":true}`),
			}},
		}},
		{response: &anthropic.Response{
			ID:         "msg_subagent_tool",
			Type:       "message",
			Role:       "assistant",
			Model:      "sonnet",
			StopReason: "tool_use",
			Content: []contracts.ContentBlock{{
				Type:  contracts.ContentToolUse,
				ID:    "toolu_cwd",
				Name:  "CWD",
				Input: json.RawMessage(`{}`),
			}},
		}},
		{response: &anthropic.Response{
			ID:         "msg_subagent_done",
			Type:       "message",
			Role:       "assistant",
			Model:      "sonnet",
			StopReason: "end_turn",
			Content:    []contracts.ContentBlock{contracts.NewTextBlock("worktree done")},
		}},
		{response: &anthropic.Response{
			ID:         "msg_done",
			Type:       "message",
			Role:       "assistant",
			Model:      "sonnet",
			StopReason: "end_turn",
			Content:    []contracts.ContentBlock{contracts.NewTextBlock("task completed")},
		}},
	}}
	runner := Runner{
		Client:           client,
		Tools:            tool.NewExecutor(registry),
		Model:            "sonnet",
		MaxTokens:        128,
		SessionID:        "sess_task_worktree_run",
		SessionPath:      sessionPath,
		WorkingDirectory: repo,
	}

	result, err := runner.RunTurn(context.Background(), nil, messages.UserText("start task"))
	if err != nil {
		t.Fatal(err)
	}
	if len(result.ToolResults) != 1 {
		t.Fatalf("tool results = %#v", result.ToolResults)
	}
	toolResult := result.ToolResults[0]
	worktreePath, ok := toolResult.StructuredContent["worktree_path"].(string)
	if !ok || worktreePath == "" || worktreePath == repo {
		t.Fatalf("worktree path structured content = %#v", toolResult.StructuredContent)
	}
	if len(toolCWDs) != 1 || filepath.Clean(toolCWDs[0]) != filepath.Clean(worktreePath) {
		t.Fatalf("subagent tool cwd = %#v, want %s", toolCWDs, worktreePath)
	}
	if toolResult.StructuredContent["worktree_cleanup_attempted"] != true ||
		toolResult.StructuredContent["worktree_cleanup_status"] != "removed" ||
		toolResult.StructuredContent["worktree_cleanup_reason"] != "subagent completed" {
		t.Fatalf("cleanup structured content = %#v", toolResult.StructuredContent)
	}
	if _, err := os.Stat(worktreePath); !os.IsNotExist(err) {
		t.Fatalf("worktree still exists after subagent completion: %v", err)
	}
	state, err := session.FindSidechainState(sessionPath, runner.SessionID, "agent/worktree-run")
	if err != nil {
		t.Fatal(err)
	}
	if state.Metadata.WorktreeCleanupStatus != "removed" || state.Metadata.WorktreeCleanupReason != "subagent completed" {
		t.Fatalf("cleanup metadata = %#v", state.Metadata)
	}
}

func TestRunnerTaskSubagentCancelCleansOwnedWorktree(t *testing.T) {
	registry, err := tool.NewRegistry(tasktools.NewTaskTool())
	if err != nil {
		t.Fatal(err)
	}
	repo := initConversationGitRepo(t)
	sessionPath := filepath.Join(t.TempDir(), "session.jsonl")
	client := &fakeClient{calls: []fakeCall{
		{response: &anthropic.Response{
			ID:         "msg_task",
			Type:       "message",
			Role:       "assistant",
			Model:      "sonnet",
			StopReason: "tool_use",
			Content: []contracts.ContentBlock{{
				Type:  contracts.ContentToolUse,
				ID:    "toolu_task",
				Name:  "Task",
				Input: json.RawMessage(`{"id":"agent/cancel-worktree","description":"Cancel in worktree","prompt":"Run until cancelled","subagent_type":"general-purpose","worktree":true,"run":true}`),
			}},
		}},
		{err: context.Canceled},
		{response: &anthropic.Response{
			ID:         "msg_done",
			Type:       "message",
			Role:       "assistant",
			Model:      "sonnet",
			StopReason: "end_turn",
			Content:    []contracts.ContentBlock{contracts.NewTextBlock("task cancelled")},
		}},
	}}
	runner := Runner{
		Client:           client,
		Tools:            tool.NewExecutor(registry),
		Model:            "sonnet",
		MaxTokens:        128,
		SessionID:        "sess_task_cancel_worktree",
		SessionPath:      sessionPath,
		WorkingDirectory: repo,
	}

	result, err := runner.RunTurn(context.Background(), nil, messages.UserText("start task"))
	if err != nil {
		t.Fatal(err)
	}
	if len(result.ToolResults) != 1 {
		t.Fatalf("tool results = %#v", result.ToolResults)
	}
	toolResult := result.ToolResults[0]
	worktreePath, ok := toolResult.StructuredContent["worktree_path"].(string)
	if !ok || worktreePath == "" || worktreePath == repo {
		t.Fatalf("worktree path structured content = %#v", toolResult.StructuredContent)
	}
	if !toolResult.IsError ||
		toolResult.StructuredContent["status"] != session.SidechainStatusCancelled ||
		toolResult.StructuredContent["cancelled"] != true ||
		toolResult.StructuredContent["worktree_cleanup_status"] != "removed" {
		t.Fatalf("cancel structured content = %#v", toolResult.StructuredContent)
	}
	if _, err := os.Stat(worktreePath); !os.IsNotExist(err) {
		t.Fatalf("worktree still exists after subagent cancellation: %v", err)
	}
	state, err := session.FindSidechainState(sessionPath, runner.SessionID, "agent/cancel-worktree")
	if err != nil {
		t.Fatal(err)
	}
	if state.Status != session.SidechainStatusCancelled ||
		state.Metadata.WorktreeCleanupStatus != "removed" ||
		state.Metadata.WorktreeCleanupReason != "subagent cancelled" {
		t.Fatalf("cancelled state = %#v metadata=%#v", state, state.Metadata)
	}
}

func TestRunnerTaskToolUsesPluginAgentMetadata(t *testing.T) {
	repo := filepath.Join(t.TempDir(), "repo")
	cwd := filepath.Join(repo, "pkg")
	pluginDir := filepath.Join(repo, ".claude", "plugins", "demo")
	if err := os.MkdirAll(filepath.Join(repo, ".git"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Join(pluginDir, "agents"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(cwd, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(pluginDir, "plugin.json"), []byte(`{"name":"demo","version":"1.0.0"}`), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(pluginDir, "agents", "reviewer.md"), []byte("---\nname: reviewer\ndescription: Review changes\n---\nReview."), 0o644); err != nil {
		t.Fatal(err)
	}
	registry, err := tool.NewRegistry(tasktools.NewTaskTool())
	if err != nil {
		t.Fatal(err)
	}
	transcriptPath := filepath.Join(t.TempDir(), "session.jsonl")
	client := &fakeClient{calls: []fakeCall{
		{response: &anthropic.Response{
			ID:         "msg_task",
			Type:       "message",
			Role:       "assistant",
			Model:      "sonnet",
			StopReason: "tool_use",
			Content: []contracts.ContentBlock{{
				Type:  contracts.ContentToolUse,
				ID:    "toolu_task",
				Name:  "Task",
				Input: json.RawMessage(`{"description":"Review API","prompt":"Inspect the API changes","subagent_type":"demo:reviewer"}`),
			}},
		}},
		{response: &anthropic.Response{
			ID:         "msg_done",
			Type:       "message",
			Role:       "assistant",
			Model:      "sonnet",
			StopReason: "end_turn",
			Content:    []contracts.ContentBlock{contracts.NewTextBlock("task recorded")},
		}},
	}}
	runner := Runner{
		Client:           client,
		Tools:            tool.NewExecutor(registry),
		Model:            "sonnet",
		MaxTokens:        128,
		SessionID:        "sess_task_plugin",
		SessionPath:      transcriptPath,
		WorkingDirectory: cwd,
	}

	request, err := runner.BuildRequest([]contracts.Message{messages.UserText("start task")}, "sonnet")
	if err != nil {
		t.Fatal(err)
	}
	if len(request.Tools) != 1 || request.Tools[0].Name != "Task" {
		t.Fatalf("tools = %#v", request.Tools)
	}
	properties := request.Tools[0].InputSchema["properties"].(map[string]any)
	subagent := properties["subagent_type"].(map[string]any)
	enumValues, ok := subagent["enum"].([]any)
	if !ok || !containsAnyString(enumValues, "general-purpose") || !containsAnyString(enumValues, "demo:reviewer") {
		t.Fatalf("task schema enum = %#v", subagent["enum"])
	}

	result, err := runner.RunTurn(context.Background(), nil, messages.UserText("start task"))
	if err != nil {
		t.Fatal(err)
	}
	if len(result.ToolResults) != 1 || result.ToolResults[0].StructuredContent["subagent_type"] != "demo:reviewer" {
		t.Fatalf("tool results = %#v", result.ToolResults)
	}
	states, err := session.ListSidechainStates(transcriptPath, "sess_task_plugin")
	if err != nil {
		t.Fatal(err)
	}
	if len(states) != 1 || states[0].MessageCount != 3 || states[0].Metadata.AgentType != "demo:reviewer" || states[0].Metadata.AgentPath == "" || states[0].Metadata.AgentPrompt != "Review." {
		t.Fatalf("states = %#v", states)
	}
}

func TestRunnerAddsConfiguredMCPToolsAndExecutesUse(t *testing.T) {
	mcpClient := &fakeRunnerMCPClient{
		tools: []mcp.RemoteTool{{
			Name:        "ping",
			Description: "Ping remote MCP server",
			InputSchema: contracts.JSONSchema{
				"type": "object",
				"properties": map[string]any{
					"text": map[string]any{"type": "string"},
				},
			},
			ReadOnly: true,
		}},
		callResult: map[string]any{"toolResult": "pong"},
	}
	closed := false
	registry, err := tool.NewRegistry(tool.FuncTool{
		DefinitionValue: contracts.ToolDefinition{
			Name:        "Local",
			Description: "local tool",
			ReadOnly:    true,
			InputSchema: contracts.JSONSchema{"type": "object"},
		},
		CallFunc: func(ctx tool.Context, raw json.RawMessage, sink tool.ProgressSink) (contracts.ToolResult, error) {
			return contracts.ToolResult{Content: "local"}, nil
		},
	})
	if err != nil {
		t.Fatal(err)
	}

	client := &fakeClient{calls: []fakeCall{
		{response: &anthropic.Response{
			ID:         "msg_tool",
			Type:       "message",
			Role:       "assistant",
			Model:      "sonnet",
			StopReason: "tool_use",
			Content: []contracts.ContentBlock{{
				Type:  contracts.ContentToolUse,
				ID:    "toolu_mcp",
				Name:  "mcp__remote__ping",
				Input: json.RawMessage(`{"text":"hello"}`),
			}},
		}},
		{response: &anthropic.Response{
			ID:         "msg_done",
			Type:       "message",
			Role:       "assistant",
			Model:      "sonnet",
			StopReason: "end_turn",
			Content:    []contracts.ContentBlock{contracts.NewTextBlock("done")},
		}},
	}}
	runner := Runner{
		Client:    client,
		Tools:     tool.NewExecutor(registry),
		Model:     "sonnet",
		MaxTokens: 128,
		MCP: &MCPConfig{
			UserSettings: contracts.Settings{
				MCPServers: map[string]contracts.MCPServer{
					"remote": {Command: "node", Args: []string{"server.js"}},
				},
			},
			ToolOptions: mcp.ServerToolOptions{
				DisableResources: true,
				DisablePrompts:   true,
				OpenClient: func(_ context.Context, name string, server contracts.MCPServer) (mcp.ClientHandle, error) {
					if name != "remote" || server.Command != "node" {
						t.Fatalf("opened server %q %#v", name, server)
					}
					return mcp.ClientHandle{
						Client: mcpClient,
						Close: func() error {
							closed = true
							return nil
						},
					}, nil
				},
			},
		},
	}

	result, err := runner.RunTurn(context.Background(), nil, messages.UserText("use mcp"))
	if err != nil {
		t.Fatal(err)
	}
	if !closed {
		t.Fatal("configured MCP client was not closed")
	}
	if len(client.requests) != 2 {
		t.Fatalf("requests = %d, want 2", len(client.requests))
	}
	if !requestHasTool(client.requests[0], "Local") || !requestHasTool(client.requests[0], "mcp__remote__ping") {
		t.Fatalf("request tools = %#v", client.requests[0].Tools)
	}
	if len(mcpClient.calls) != 1 || mcpClient.calls[0].ServerName != "remote" || mcpClient.calls[0].ToolName != "ping" {
		t.Fatalf("mcp calls = %#v", mcpClient.calls)
	}
	if string(mcpClient.calls[0].Input) != `{"text":"hello"}` {
		t.Fatalf("mcp input = %s", mcpClient.calls[0].Input)
	}
	if len(result.ToolResults) != 1 || result.ToolResults[0].Content != "pong" {
		t.Fatalf("tool results = %#v", result.ToolResults)
	}
}

func TestRunnerAppendsToolNewMessagesAfterToolResult(t *testing.T) {
	registry, err := tool.NewRegistry(tool.FuncTool{
		DefinitionValue: contracts.ToolDefinition{Name: "Meta", ReadOnly: true},
		CallFunc: func(ctx tool.Context, raw json.RawMessage, sink tool.ProgressSink) (contracts.ToolResult, error) {
			return contracts.ToolResult{
				Content: "launched",
				NewMessages: []contracts.Message{{
					Type:    contracts.MessageUser,
					IsMeta:  true,
					Content: []contracts.ContentBlock{contracts.NewTextBlock("meta skill content")},
				}},
			}, nil
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	client := &fakeClient{calls: []fakeCall{
		{response: &anthropic.Response{
			ID:         "msg_tool",
			Type:       "message",
			Role:       "assistant",
			Model:      "sonnet",
			StopReason: "tool_use",
			Content: []contracts.ContentBlock{{
				Type:  contracts.ContentToolUse,
				ID:    "toolu_meta",
				Name:  "Meta",
				Input: json.RawMessage(`{}`),
			}},
		}},
		{response: &anthropic.Response{
			ID:         "msg_done",
			Type:       "message",
			Role:       "assistant",
			Model:      "sonnet",
			StopReason: "end_turn",
			Content:    []contracts.ContentBlock{contracts.NewTextBlock("done")},
		}},
	}}
	transcriptPath := filepath.Join(t.TempDir(), "session.jsonl")
	runner := Runner{
		Client:      client,
		Tools:       tool.NewExecutor(registry),
		Model:       "sonnet",
		MaxTokens:   128,
		SessionID:   "sess_meta",
		SessionPath: transcriptPath,
	}

	result, err := runner.RunTurn(context.Background(), nil, messages.UserText("run meta"))
	if err != nil {
		t.Fatal(err)
	}
	if len(result.ToolResults) != 1 || result.ToolResults[0].Content != "launched" {
		t.Fatalf("tool results = %#v", result.ToolResults)
	}
	if len(client.requests) != 2 {
		t.Fatalf("requests = %d, want 2", len(client.requests))
	}
	second := client.requests[1].Messages
	if len(second) < 2 {
		t.Fatalf("second request messages = %#v", second)
	}
	toolResult := second[len(second)-2]
	meta := second[len(second)-1]
	if toolResult.Role != "user" || toolResult.Content[0].Type != contracts.ContentToolResult || toolResult.Content[0].ToolUseID != "toolu_meta" {
		t.Fatalf("tool result api message = %#v", toolResult)
	}
	if meta.Role != "user" || meta.Content[0].Text != "meta skill content" {
		t.Fatalf("meta api message = %#v", meta)
	}
	entries, err := session.Load(transcriptPath)
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 5 {
		t.Fatalf("transcript entries = %d, want 5", len(entries))
	}
	if !entries[3].Message.IsMeta || entries[3].Message.SessionID != "sess_meta" {
		t.Fatalf("meta transcript entry = %#v", entries[3].Message)
	}
}

func TestRunnerExpandsPromptSlashCommandBeforeQuery(t *testing.T) {
	repo := filepath.Join(t.TempDir(), "repo")
	cwd := filepath.Join(repo, "pkg")
	skillDir := filepath.Join(cwd, ".claude", "skills", "explain")
	if err := os.MkdirAll(skillDir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(skillDir, "SKILL.md"), []byte("---\ndescription: Explain target\nmodel: opus\n---\nExplain $ARGUMENTS in ${CLAUDE_SESSION_ID}."), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Join(repo, ".git"), 0o755); err != nil {
		t.Fatal(err)
	}
	client := &fakeClient{calls: []fakeCall{{response: &anthropic.Response{
		ID:         "msg_done",
		Type:       "message",
		Role:       "assistant",
		Model:      "opus",
		StopReason: "end_turn",
		Content:    []contracts.ContentBlock{contracts.NewTextBlock("done")},
	}}}}
	transcriptPath := filepath.Join(t.TempDir(), "session.jsonl")
	runner := Runner{
		Client:           client,
		Model:            "sonnet",
		APIKeySource:     "oauth",
		MaxTokens:        128,
		SessionID:        "sess_slash",
		SessionPath:      transcriptPath,
		WorkingDirectory: cwd,
	}

	result, err := runner.RunTurn(context.Background(), nil, messages.UserText("/explain planner"))
	if err != nil {
		t.Fatal(err)
	}
	if result.FinalRequest.Model != "opus" {
		t.Fatalf("request model = %q", result.FinalRequest.Model)
	}
	if runner.Model != "sonnet" {
		t.Fatalf("runner model = %q", runner.Model)
	}
	if len(client.requests) != 1 {
		t.Fatalf("requests = %d, want 1", len(client.requests))
	}
	requestMessages := client.requests[0].Messages
	if len(requestMessages) != 2 {
		t.Fatalf("request messages = %#v", requestMessages)
	}
	if text := requestMessages[0].Content[0].Text; !strings.Contains(text, "<command-name>/explain</command-name>") || strings.Contains(text, "/explain planner") {
		t.Fatalf("command metadata text = %q", text)
	}
	if text := requestMessages[1].Content[0].Text; !strings.Contains(text, "Base directory for this skill: "+skillDir) || !strings.Contains(text, "Explain planner in sess_slash.") {
		t.Fatalf("expanded skill text = %q", text)
	}
	if len(result.Messages) != 4 || !result.Messages[1].IsMeta || result.Messages[2].Subtype != "command_permissions" {
		t.Fatalf("result messages = %#v", result.Messages)
	}
	entries, err := session.Load(transcriptPath)
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 4 {
		t.Fatalf("transcript entries = %d, want 4", len(entries))
	}
	if entries[1].Message.ParentUUID == nil || *entries[1].Message.ParentUUID != entries[0].Message.UUID {
		t.Fatalf("slash prompt parent chain = %#v then %#v", entries[0].Message, entries[1].Message)
	}
	if entries[2].Message.ParentUUID == nil || *entries[2].Message.ParentUUID != entries[1].Message.UUID {
		t.Fatalf("command permissions parent chain = %#v then %#v", entries[1].Message, entries[2].Message)
	}
}

func TestRunnerExecutesClearSlashCommandWithoutQuery(t *testing.T) {
	client := &fakeClient{}
	runner := Runner{
		Client:    client,
		Model:     "sonnet",
		MaxTokens: 128,
		SessionID: "sess_clear",
		MCP: &MCPConfig{
			UserSettings: contracts.Settings{
				MCPServers: map[string]contracts.MCPServer{
					"remote": {Command: "node"},
				},
			},
			ToolOptions: mcp.ServerToolOptions{
				OpenClient: func(_ context.Context, name string, _ contracts.MCPServer) (mcp.ClientHandle, error) {
					t.Fatalf("no-query slash command opened MCP server %q", name)
					return mcp.ClientHandle{}, nil
				},
			},
		},
	}

	result, err := runner.RunTurn(context.Background(), []contracts.Message{messages.UserText("old context")}, messages.UserText("/clear"))
	if err != nil {
		t.Fatal(err)
	}
	if len(client.requests) != 0 {
		t.Fatalf("model should not be queried, requests = %#v", client.requests)
	}
	if result.FinalRequest.Model != "" || result.Assistant.Type != "" {
		t.Fatalf("unexpected model result = %#v", result)
	}
	if !result.Cleared {
		t.Fatalf("clear result did not set Cleared: %#v", result)
	}
	if len(result.Messages) != 1 {
		t.Fatalf("result messages = %#v", result.Messages)
	}
	if text := result.Messages[0].Content[0].Text; !strings.Contains(text, "<command-name>/clear</command-name>") {
		t.Fatalf("clear message = %q", text)
	}
}

func TestRunnerExecutesIssueSlashCommandWithoutQuery(t *testing.T) {
	dir := t.TempDir()
	client := &fakeClient{
		dumpPath: filepath.Join(dir, "prompt-dump.jsonl"),
		dumpCache: []anthropic.PromptDumpCacheEntry{{
			Timestamp: "2026-01-02T03:04:05Z",
			Request:   json.RawMessage(`{"model":"sonnet","max_tokens":64,"stream":true,"system":"secret system","messages":[{"role":"user","content":"super secret prompt"}],"tools":[{"name":"SecretTool"}]}`),
		}},
	}
	transcriptPath := filepath.Join(dir, "session.jsonl")
	runner := Runner{
		Client:           client,
		Model:            "sonnet",
		MaxTokens:        128,
		SessionID:        "sess_issue",
		SessionPath:      transcriptPath,
		WorkingDirectory: "/repo",
	}

	result, err := runner.RunTurn(context.Background(), nil, messages.UserText("/issue auth failed"))
	if err != nil {
		t.Fatal(err)
	}
	if len(client.requests) != 0 {
		t.Fatalf("model should not be queried, requests = %#v", client.requests)
	}
	if len(result.Messages) != 2 {
		t.Fatalf("result messages = %#v", result.Messages)
	}
	text := result.Messages[1].Content[0].Text
	for _, want := range []string{
		"Issue report context",
		"Description: auth failed",
		"Session ID: sess_issue",
		"Working directory: /repo",
		"Model: sonnet",
		"Prompt dump path: " + client.dumpPath,
		"Recent prompt dumps: 1",
		"timestamp=2026-01-02T03:04:05Z",
		"request_sha256=",
		"max_tokens=64",
		"stream=true",
		"messages=1",
		"tools=1",
		"system=true",
		"Issue bundle path: " + filepath.Join(dir, "sess_issue", "issue-report.json"),
		"Submission: local issue bundle prepared.",
	} {
		if !strings.Contains(text, want) {
			t.Fatalf("issue summary missing %q in:\n%s", want, text)
		}
	}
	for _, leaked := range []string{"super secret prompt", "secret system", "SecretTool"} {
		if strings.Contains(text, leaked) {
			t.Fatalf("issue summary leaked %q in:\n%s", leaked, text)
		}
	}
	data, err := os.ReadFile(filepath.Join(dir, "sess_issue", "issue-report.json"))
	if err != nil {
		t.Fatal(err)
	}
	var bundle struct {
		Description       string   `json:"description"`
		SessionID         string   `json:"session_id"`
		WorkingDirectory  string   `json:"working_directory"`
		Model             string   `json:"model"`
		PromptDumpPath    string   `json:"prompt_dump_path"`
		RecentPromptDumps []string `json:"recent_prompt_dumps"`
	}
	if err := json.Unmarshal(data, &bundle); err != nil {
		t.Fatal(err)
	}
	if bundle.Description != "auth failed" || bundle.SessionID != "sess_issue" || bundle.WorkingDirectory != "/repo" || bundle.Model != "sonnet" || bundle.PromptDumpPath != client.dumpPath {
		t.Fatalf("issue bundle = %#v", bundle)
	}
	if len(bundle.RecentPromptDumps) != 1 || !strings.Contains(bundle.RecentPromptDumps[0], "request_sha256=") {
		t.Fatalf("issue bundle prompt summaries = %#v", bundle.RecentPromptDumps)
	}
	for _, leaked := range []string{"super secret prompt", "secret system", "SecretTool"} {
		if strings.Contains(string(data), leaked) {
			t.Fatalf("issue bundle leaked %q in:\n%s", leaked, data)
		}
	}
}

func TestRunnerExecutesCompactSlashCommandWithoutMainQuery(t *testing.T) {
	client := &fakeClient{calls: []fakeCall{{response: &anthropic.Response{
		ID:         "msg_manual_compact",
		Type:       "message",
		Role:       "assistant",
		Model:      "sonnet",
		StopReason: "end_turn",
		Content:    []contracts.ContentBlock{contracts.NewTextBlock("manual summary")},
	}}}}
	var events []EventType
	transcriptPath := filepath.Join(t.TempDir(), "session.jsonl")
	runner := Runner{
		Client:      client,
		Model:       "sonnet",
		MaxTokens:   128,
		SessionID:   "sess_manual_compact",
		SessionPath: transcriptPath,
		OnEvent: func(event Event) {
			events = append(events, event.Type)
		},
	}
	history := []contracts.Message{
		messages.UserText("old one"),
		messages.AssistantText("old two", "sonnet", nil),
	}
	result, err := runner.RunTurn(context.Background(), history, messages.UserText("/compact focus on API"))
	if err != nil {
		t.Fatal(err)
	}
	if !result.Compacted || result.Compact == nil || result.Assistant.Type != "" {
		t.Fatalf("manual compact result = %#v", result)
	}
	if len(client.requests) != 1 {
		t.Fatalf("requests = %d, want compact only", len(client.requests))
	}
	for _, message := range client.requests[0].Messages {
		if strings.Contains(message.Content[0].Text, "<command-name>/compact</command-name>") {
			t.Fatalf("compact request included command metadata: %#v", client.requests[0].Messages)
		}
	}
	if len(result.Messages) != 3 {
		t.Fatalf("result messages = %#v", result.Messages)
	}
	if !strings.Contains(result.Messages[0].Content[0].Text, "<command-name>/compact</command-name>") {
		t.Fatalf("compact command message = %#v", result.Messages[0])
	}
	if result.Messages[1].Subtype != "compact_boundary" || !strings.Contains(result.Messages[2].Content[0].Text, "manual summary") {
		t.Fatalf("compact messages = %#v", result.Messages)
	}
	if result.Compact.Plan.Metadata.Trigger != string(compactpkg.TriggerManual) ||
		result.Compact.Plan.Metadata.UserContext != "focus on API" ||
		result.Compact.Plan.Metadata.MessagesSummarized != 2 {
		t.Fatalf("compact metadata = %#v", result.Compact.Plan.Metadata)
	}
	if !containsEvent(events, EventCompact) {
		t.Fatalf("events = %#v", events)
	}
	transcript, err := session.LoadTranscript(transcriptPath)
	if err != nil {
		t.Fatal(err)
	}
	var foundBoundary bool
	for _, id := range transcript.Order {
		msg := transcript.Messages[id]
		if msg != nil && msg.IsCompactBoundary() && msg.CompactMetadata != nil &&
			msg.CompactMetadata.Trigger == string(compactpkg.TriggerManual) &&
			msg.CompactMetadata.UserContext == "focus on API" {
			foundBoundary = true
			break
		}
	}
	if !foundBoundary {
		t.Fatalf("transcript missing manual compact boundary: %#v", transcript.Order)
	}
}

func TestRunnerPreCompactHookAddsSummaryInstructions(t *testing.T) {
	client := &fakeClient{calls: []fakeCall{{response: &anthropic.Response{
		ID:         "msg_summary",
		Type:       "message",
		Role:       "assistant",
		Model:      "sonnet",
		StopReason: "end_turn",
		Content:    []contracts.ContentBlock{contracts.NewTextBlock("manual summary")},
	}}}}
	var progress []contracts.ToolProgress
	runner := Runner{
		Client:           client,
		Model:            "sonnet",
		MaxTokens:        128,
		SessionID:        "sess_precompact_hook",
		SessionPath:      filepath.Join(t.TempDir(), "session.jsonl"),
		WorkingDirectory: t.TempDir(),
		MCP: &MCPConfig{ProjectSettings: contracts.Settings{Hooks: map[string]any{
			"PreCompact": []any{map[string]any{
				"hooks": []any{map[string]any{
					"type":    "command",
					"command": `printf '%s\n' '{"hookSpecificOutput":{"hookEventName":"PreCompact","additionalContext":"include hook compact instructions"}}'`,
				}},
			}},
		}}},
		OnEvent: func(event Event) {
			if event.Type == EventToolProgress && event.ToolProgress != nil {
				progress = append(progress, *event.ToolProgress)
			}
		},
	}
	history := []contracts.Message{
		messages.UserText("old one"),
		messages.AssistantText("old two", "sonnet", nil),
	}
	result, err := runner.RunTurn(context.Background(), history, messages.UserText("/compact focus on API"))
	if err != nil {
		t.Fatal(err)
	}
	if !result.Compacted || result.Compact == nil {
		t.Fatalf("manual compact result = %#v", result)
	}
	if len(client.requests) != 1 {
		t.Fatalf("requests = %#v", client.requests)
	}
	compactPrompt := client.requests[0].Messages[len(client.requests[0].Messages)-1].Content[0].Text
	if !strings.Contains(compactPrompt, "include hook compact instructions") {
		t.Fatalf("compact prompt = %q", compactPrompt)
	}
	if !hasHookProgress(progress, tool.HookPreCompact, "hook_completed") {
		t.Fatalf("hook progress = %#v", progress)
	}
}

func TestRunnerExecutesCostSlashCommandWithoutQuery(t *testing.T) {
	client := &fakeClient{}
	transcriptPath := filepath.Join(t.TempDir(), "session.jsonl")
	runner := Runner{
		Client:      client,
		Model:       "sonnet",
		MaxTokens:   128,
		SessionID:   "sess_cost",
		SessionPath: transcriptPath,
	}
	firstUsage := contracts.Usage{
		InputTokens:              10,
		OutputTokens:             20,
		CacheCreationInputTokens: 3,
		CacheReadInputTokens:     4,
		ServerToolUse:            contracts.ToolUseUsage{WebSearchRequests: 1},
		CostUSD:                  0.123456,
	}
	secondUsage := contracts.Usage{
		InputTokens:   5,
		OutputTokens:  7,
		ServerToolUse: contracts.ToolUseUsage{WebFetchRequests: 2},
		CostUSD:       0.5,
	}
	history := []contracts.Message{
		messages.UserText("old one"),
		messages.AssistantText("old two", "sonnet", &firstUsage),
		messages.AssistantText("old three", "sonnet", &secondUsage),
	}
	result, err := runner.RunTurn(context.Background(), history, messages.UserText("/cost"))
	if err != nil {
		t.Fatal(err)
	}
	if len(client.requests) != 0 {
		t.Fatalf("model should not be queried, requests = %#v", client.requests)
	}
	if result.Assistant.Type != "" || result.FinalRequest.Model != "" {
		t.Fatalf("unexpected model result = %#v", result)
	}
	if len(result.Messages) != 2 {
		t.Fatalf("result messages = %#v", result.Messages)
	}
	text := result.Messages[1].Content[0].Text
	for _, want := range []string{
		"Total cost: $0.623456",
		"Input tokens: 15",
		"Output tokens: 27",
		"Cache creation input tokens: 3",
		"Cache read input tokens: 4",
		"Web search requests: 1",
		"Web fetch requests: 2",
	} {
		if !strings.Contains(text, want) {
			t.Fatalf("cost text missing %q: %q", want, text)
		}
	}
	entries, err := session.Load(transcriptPath)
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 2 || !strings.Contains(entries[1].Message.Content[0].Text, "Total cost: $0.623456") {
		t.Fatalf("transcript entries = %#v", entries)
	}

	for _, prompt := range []string{"/cost status", "/cost current", "/cost usage"} {
		result, err = runner.RunTurn(context.Background(), history, messages.UserText(prompt))
		if err != nil {
			t.Fatal(err)
		}
		if len(client.requests) != 0 {
			t.Fatalf("%s should not query model, requests = %#v", prompt, client.requests)
		}
		if got := result.Messages[1].Content[0].Text; !strings.Contains(got, "Total cost: $0.623456") || strings.Contains(got, "Cost breakdown") {
			t.Fatalf("%s cost text = %q", prompt, got)
		}
	}
}

func TestRunnerCostSlashCommandReportsMissingUsage(t *testing.T) {
	runner := Runner{Client: &fakeClient{}, SessionID: "sess_cost_empty"}
	result, err := runner.RunTurn(context.Background(), []contracts.Message{messages.UserText("old")}, messages.UserText("/cost"))
	if err != nil {
		t.Fatal(err)
	}
	if len(result.Messages) != 2 {
		t.Fatalf("result messages = %#v", result.Messages)
	}
	if got := result.Messages[1].Content[0].Text; got != "No cost data available for this session." {
		t.Fatalf("cost text = %q", got)
	}
}

func TestRunnerCostSlashCommandRestoresUsageFromSessionTranscript(t *testing.T) {
	client := &fakeClient{}
	transcriptPath := filepath.Join(t.TempDir(), "session.jsonl")
	usage := contracts.Usage{
		InputTokens:  11,
		OutputTokens: 13,
		CostUSD:      0.25,
	}
	prior := messages.AssistantText("old charged turn", "sonnet", &usage)
	prior.UUID = "cost_restored"
	prior.SessionID = "sess_cost_restore"
	if err := session.Append(transcriptPath, session.EntryFromMessage("sess_cost_restore", prior)); err != nil {
		t.Fatal(err)
	}
	runner := Runner{
		Client:      client,
		Model:       "sonnet",
		MaxTokens:   128,
		SessionID:   "sess_cost_restore",
		SessionPath: transcriptPath,
	}
	result, err := runner.RunTurn(context.Background(), nil, messages.UserText("/cost"))
	if err != nil {
		t.Fatal(err)
	}
	if len(client.requests) != 0 {
		t.Fatalf("model should not be queried, requests = %#v", client.requests)
	}
	text := result.Messages[1].Content[0].Text
	for _, want := range []string{
		"Total cost: $0.250000",
		"Input tokens: 11",
		"Output tokens: 13",
	} {
		if !strings.Contains(text, want) {
			t.Fatalf("restored cost missing %q: %q", want, text)
		}
	}
}

func TestRunnerCostSlashCommandDedupesResumeHistoryAndTranscriptUsage(t *testing.T) {
	client := &fakeClient{}
	transcriptPath := filepath.Join(t.TempDir(), "session.jsonl")
	usage := contracts.Usage{
		InputTokens:  11,
		OutputTokens: 13,
		CostUSD:      0.25,
	}
	prior := messages.AssistantText("old charged turn", "sonnet", &usage)
	prior.UUID = "cost_dedupe"
	prior.SessionID = "sess_cost_dedupe"
	if err := session.Append(transcriptPath, session.EntryFromMessage("sess_cost_dedupe", prior)); err != nil {
		t.Fatal(err)
	}
	runner := Runner{
		Client:      client,
		Model:       "sonnet",
		MaxTokens:   128,
		SessionID:   "sess_cost_dedupe",
		SessionPath: transcriptPath,
	}
	result, err := runner.RunTurn(context.Background(), []contracts.Message{prior}, messages.UserText("/cost"))
	if err != nil {
		t.Fatal(err)
	}
	text := result.Messages[1].Content[0].Text
	for _, want := range []string{
		"Total cost: $0.250000",
		"Input tokens: 11",
		"Output tokens: 13",
	} {
		if !strings.Contains(text, want) {
			t.Fatalf("deduped cost missing %q: %q", want, text)
		}
	}
	if strings.Contains(text, "$0.500000") || strings.Contains(text, "Input tokens: 22") || strings.Contains(text, "Output tokens: 26") {
		t.Fatalf("cost was double-counted: %q", text)
	}
}

func TestRunnerCostSlashCommandReportsBreakdown(t *testing.T) {
	client := &fakeClient{}
	runner := Runner{Client: client, SessionID: "sess_cost_breakdown"}
	firstUsage := contracts.Usage{
		InputTokens:              10,
		OutputTokens:             20,
		CacheCreationInputTokens: 3,
		CacheReadInputTokens:     4,
		ServerToolUse:            contracts.ToolUseUsage{WebSearchRequests: 1},
		CostUSD:                  0.123456,
	}
	secondUsage := contracts.Usage{
		InputTokens:   5,
		OutputTokens:  7,
		ServerToolUse: contracts.ToolUseUsage{WebFetchRequests: 2},
		CostUSD:       0.5,
	}
	first := messages.AssistantText("old two", "sonnet", &firstUsage)
	first.UUID = "cost_one"
	second := messages.AssistantText("old three", "opus", &secondUsage)
	second.UUID = "cost_two"
	history := []contracts.Message{
		messages.UserText("old one"),
		first,
		second,
	}
	result, err := runner.RunTurn(context.Background(), history, messages.UserText("/cost breakdown"))
	if err != nil {
		t.Fatal(err)
	}
	if len(client.requests) != 0 {
		t.Fatalf("model should not be queried, requests = %#v", client.requests)
	}
	text := result.Messages[1].Content[0].Text
	for _, want := range []string{
		"Cost breakdown",
		"Total cost: $0.623456",
		"- assistant cost_one (sonnet): cost $0.123456, input 10, output 20, cache create 3, cache read 4, web search 1, web fetch 0",
		"- assistant cost_two (opus): cost $0.500000, input 5, output 7, cache create 0, cache read 0, web search 0, web fetch 2",
		"Messages with usage: 2",
	} {
		if !strings.Contains(text, want) {
			t.Fatalf("cost breakdown missing %q: %q", want, text)
		}
	}
}

func TestRunnerCostSlashCommandReportsJSONAndDuration(t *testing.T) {
	client := &fakeClient{}
	runner := Runner{Client: client, SessionID: "sess_cost_json"}
	firstUsage := contracts.Usage{
		InputTokens:  10,
		OutputTokens: 20,
		CostUSD:      0.25,
	}
	secondUsage := contracts.Usage{
		InputTokens:  5,
		OutputTokens: 7,
		CostUSD:      0.5,
	}
	user := messages.UserText("old one")
	user.Timestamp = time.Unix(90, 0).UTC().Format(time.RFC3339Nano)
	first := messages.AssistantText("old two", "sonnet", &firstUsage)
	first.UUID = "cost_json_one"
	first.Timestamp = time.Unix(100, 0).UTC().Format(time.RFC3339Nano)
	second := messages.AssistantText("old three", "opus", &secondUsage)
	second.UUID = "cost_json_two"
	second.Timestamp = time.Unix(160, 0).UTC().Format(time.RFC3339Nano)
	history := []contracts.Message{user, first, second}

	result, err := runner.RunTurn(context.Background(), history, messages.UserText("/cost json"))
	if err != nil {
		t.Fatal(err)
	}
	if len(client.requests) != 0 {
		t.Fatalf("model should not be queried, requests = %#v", client.requests)
	}
	var payload struct {
		Available         bool            `json:"available"`
		Messages          int             `json:"messages"`
		MessagesWithUsage int             `json:"messages_with_usage"`
		TotalCostUSD      float64         `json:"total_cost_usd"`
		Usage             contracts.Usage `json:"usage"`
		Timing            struct {
			StartedAt string `json:"started_at"`
			UpdatedAt string `json:"updated_at"`
			Duration  string `json:"duration"`
			Seconds   int64  `json:"duration_seconds"`
			Messages  int    `json:"messages_with_timestamps"`
		} `json:"timing"`
		Breakdown []struct {
			Label     string          `json:"label"`
			Model     string          `json:"model"`
			Timestamp string          `json:"timestamp"`
			Usage     contracts.Usage `json:"usage"`
		} `json:"breakdown"`
	}
	text := result.Messages[1].Content[0].Text
	if err := json.Unmarshal([]byte(text), &payload); err != nil {
		t.Fatalf("cost json should be valid JSON: %v\n%s", err, text)
	}
	if !payload.Available || payload.Messages != 3 || payload.MessagesWithUsage != 2 {
		t.Fatalf("cost json counts = %#v", payload)
	}
	if payload.TotalCostUSD != 0.75 || payload.Usage.InputTokens != 15 || payload.Usage.OutputTokens != 27 {
		t.Fatalf("cost json usage = %#v", payload)
	}
	if payload.Timing.Duration != "1m10s" || payload.Timing.Seconds != 70 || payload.Timing.Messages != 3 {
		t.Fatalf("cost json timing = %#v", payload.Timing)
	}
	if len(payload.Breakdown) != 2 || payload.Breakdown[0].Label != "assistant cost_json_one (sonnet)" || payload.Breakdown[1].Model != "opus" {
		t.Fatalf("cost json breakdown = %#v", payload.Breakdown)
	}

	result, err = runner.RunTurn(context.Background(), history, messages.UserText("/cost"))
	if err != nil {
		t.Fatal(err)
	}
	text = result.Messages[1].Content[0].Text
	for _, want := range []string{
		"Session started: 1970-01-01T00:01:30Z",
		"Session updated: 1970-01-01T00:02:40Z",
		"Session duration: 1m10s",
	} {
		if !strings.Contains(text, want) {
			t.Fatalf("cost duration missing %q: %q", want, text)
		}
	}
}

func TestRunnerCostSlashCommandReportsUsageForUnknownSubcommand(t *testing.T) {
	runner := Runner{Client: &fakeClient{}, SessionID: "sess_cost_unknown"}
	result, err := runner.RunTurn(context.Background(), nil, messages.UserText("/cost nope"))
	if err != nil {
		t.Fatal(err)
	}
	got := result.Messages[1].Content[0].Text
	for _, want := range []string{
		"Unknown cost subcommand: nope",
		"Usage: /cost [summary|status|current|usage|breakdown|show|details|json|export]",
	} {
		if !strings.Contains(got, want) {
			t.Fatalf("unknown cost result missing %q: %q", want, got)
		}
	}
	if strings.Contains(got, "not implemented") {
		t.Fatalf("unknown cost should not report not implemented: %q", got)
	}
}

func TestRunnerExecutesSummarySlashCommandWithoutQuery(t *testing.T) {
	client := &fakeClient{}
	runner := Runner{
		Client:           client,
		Model:            "sonnet",
		MaxTokens:        128,
		SessionID:        "sess_summary",
		WorkingDirectory: "/tmp/project",
	}
	assistant := messages.AssistantText("done with tests", "sonnet", nil)
	assistant.Content = append(assistant.Content, contracts.ContentBlock{
		Type:  contracts.ContentToolUse,
		ID:    "toolu_1",
		Name:  "Read",
		Input: json.RawMessage(`{"file_path":"README.md"}`),
	})
	history := []contracts.Message{
		messages.UserText("old prompt"),
		assistant,
		{
			Type: contracts.MessageUser,
			Content: []contracts.ContentBlock{{
				Type:      contracts.ContentToolResult,
				ToolUseID: "toolu_1",
				Content:   "ok",
			}},
		},
	}
	result, err := runner.RunTurn(context.Background(), history, messages.UserText("/summary local commands"))
	if err != nil {
		t.Fatal(err)
	}
	if len(client.requests) != 0 {
		t.Fatalf("model should not be queried, requests = %#v", client.requests)
	}
	if result.Assistant.Type != "" || result.FinalRequest.Model != "" {
		t.Fatalf("unexpected model result = %#v", result)
	}
	if len(result.Messages) != 2 {
		t.Fatalf("result messages = %#v", result.Messages)
	}
	text := result.Messages[1].Content[0].Text
	for _, want := range []string{
		"Conversation summary",
		"Session ID: sess_summary",
		"Working directory: /tmp/project",
		"Messages: 3",
		"User messages: 2",
		"Assistant messages: 1",
		"Tool uses: 1",
		"Tool results: 1",
		"Requested focus: local commands",
		"Last user: old prompt",
		"Last assistant: done with tests",
	} {
		if !strings.Contains(text, want) {
			t.Fatalf("summary text missing %q: %q", want, text)
		}
	}
}

func TestRunnerExecutesFilesSlashCommandWithoutQuery(t *testing.T) {
	client := &fakeClient{}
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "a.txt"), []byte("a"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.Mkdir(filepath.Join(dir, "subdir"), 0o755); err != nil {
		t.Fatal(err)
	}
	runner := Runner{
		Client:           client,
		Model:            "sonnet",
		MaxTokens:        128,
		SessionID:        "sess_files",
		WorkingDirectory: dir,
	}
	result, err := runner.RunTurn(context.Background(), nil, messages.UserText("/files src"))
	if err != nil {
		t.Fatal(err)
	}
	if len(client.requests) != 0 {
		t.Fatalf("model should not be queried, requests = %#v", client.requests)
	}
	if len(result.Messages) != 2 {
		t.Fatalf("result messages = %#v", result.Messages)
	}
	text := result.Messages[1].Content[0].Text
	for _, want := range []string{
		"Workspace files",
		"Working directory: " + dir,
		"Entries: 2",
		"Directories: 1",
		"Files: 1",
		"Requested filter: src",
		"- a.txt",
		"- subdir/",
	} {
		if !strings.Contains(text, want) {
			t.Fatalf("files text missing %q: %q", want, text)
		}
	}
}

func TestRunnerExecutesReleaseNotesSlashCommandWithoutQuery(t *testing.T) {
	client := &fakeClient{}
	runner := Runner{
		Client:    client,
		Model:     "sonnet",
		MaxTokens: 128,
		SessionID: "sess_release_notes",
	}
	result, err := runner.RunTurn(context.Background(), nil, messages.UserText("/release-notes plugins"))
	if err != nil {
		t.Fatal(err)
	}
	if len(client.requests) != 0 {
		t.Fatalf("model should not be queried, requests = %#v", client.requests)
	}
	if len(result.Messages) != 2 {
		t.Fatalf("result messages = %#v", result.Messages)
	}
	text := result.Messages[1].Content[0].Text
	for _, want := range []string{
		"Release notes",
		"Bundled release notes: unavailable",
		"Status: release notes are not packaged in this Go runtime yet.",
		"Requested topic: plugins",
	} {
		if !strings.Contains(text, want) {
			t.Fatalf("release notes text missing %q: %q", want, text)
		}
	}
}

func TestRunnerExecutesStatusSlashCommandWithoutQuery(t *testing.T) {
	client := &fakeClient{}
	registry, err := tool.NewRegistry(tool.FuncTool{
		DefinitionValue: contracts.ToolDefinition{Name: "Read", InputSchema: contracts.JSONSchema{"type": "object"}},
		CallFunc: func(tool.Context, json.RawMessage, tool.ProgressSink) (contracts.ToolResult, error) {
			t.Fatal("status should not call tools")
			return contracts.ToolResult{}, nil
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	transcriptPath := filepath.Join(t.TempDir(), "session.jsonl")
	runner := Runner{
		Client:           client,
		Tools:            tool.NewExecutor(registry),
		Model:            "sonnet",
		APIKeySource:     "oauth",
		PermissionMode:   contracts.PermissionPlan,
		BetaHeaders:      []string{"beta-one", "beta-two"},
		FastMode:         true,
		MaxTokens:        128,
		SessionID:        "sess_status",
		SessionPath:      transcriptPath,
		WorkingDirectory: "/tmp/project",
		MCP: &MCPConfig{UserSettings: contracts.Settings{
			OutputStyle: "Explanatory",
			MCPServers: map[string]contracts.MCPServer{
				"zeta":  {Command: "node"},
				"alpha": {Command: "python"},
			},
		}},
	}
	result, err := runner.RunTurn(context.Background(), []contracts.Message{messages.UserText("old")}, messages.UserText("/status"))
	if err != nil {
		t.Fatal(err)
	}
	if len(client.requests) != 0 {
		t.Fatalf("model should not be queried, requests = %#v", client.requests)
	}
	if result.Assistant.Type != "" || result.FinalRequest.Model != "" {
		t.Fatalf("unexpected model result = %#v", result)
	}
	if len(result.Messages) != 2 {
		t.Fatalf("result messages = %#v", result.Messages)
	}
	text := result.Messages[1].Content[0].Text
	for _, want := range []string{
		"Status",
		"Session ID: sess_status",
		"Working directory: /tmp/project",
		"Model: sonnet",
		"Output style: Explanatory",
		"Auth source: oauth",
		"Permission mode: plan",
		"Fast mode: enabled",
		"Betas: beta-one, beta-two",
		"Tools: 1",
		"MCP servers: alpha, zeta",
	} {
		if !strings.Contains(text, want) {
			t.Fatalf("status text missing %q: %q", want, text)
		}
	}
	entries, err := session.Load(transcriptPath)
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 2 || !strings.Contains(entries[1].Message.Content[0].Text, "Session ID: sess_status") {
		t.Fatalf("transcript entries = %#v", entries)
	}
}

func TestRunnerStatusSlashCommandReportsAllSections(t *testing.T) {
	client := &fakeClient{}
	registry, err := tool.NewRegistry(tool.FuncTool{
		DefinitionValue: contracts.ToolDefinition{Name: "Read", InputSchema: contracts.JSONSchema{"type": "object"}},
		CallFunc: func(tool.Context, json.RawMessage, tool.ProgressSink) (contracts.ToolResult, error) {
			t.Fatal("status should not call tools")
			return contracts.ToolResult{}, nil
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	runner := Runner{
		Client:           client,
		Tools:            tool.NewExecutor(registry),
		Model:            "sonnet",
		SessionID:        "sess_status_all",
		SessionPath:      filepath.Join(t.TempDir(), "session.jsonl"),
		WorkingDirectory: t.TempDir(),
	}
	for _, prompt := range []string{"/status all", "/status show all"} {
		result, err := runner.RunTurn(context.Background(), nil, messages.UserText(prompt))
		if err != nil {
			t.Fatal(err)
		}
		if len(client.requests) != 0 {
			t.Fatalf("%s should not query model, requests = %#v", prompt, client.requests)
		}
		text := result.Messages[1].Content[0].Text
		for _, want := range []string{
			"Status all",
			"Status session",
			"Status model",
			"Status auth",
			"Status tools",
			"Status plugins",
			"Status telemetry",
			"Status bridge",
			"Status native",
			"Status advanced integrations",
		} {
			if !strings.Contains(text, want) {
				t.Fatalf("%s missing %q: %q", prompt, want, text)
			}
		}
	}
}

func TestRunnerStatusSlashCommandReportsUsageForUnknownSection(t *testing.T) {
	runner := Runner{Client: &fakeClient{}, SessionID: "sess_status_unknown"}
	result, err := runner.RunTurn(context.Background(), nil, messages.UserText("/status unknown-section"))
	if err != nil {
		t.Fatal(err)
	}
	got := result.Messages[1].Content[0].Text
	for _, want := range []string{
		"Unknown status section: unknown-section",
		"Usage: /status <all|session|model|auth|tools|mcp|plugins|telemetry|bridge|remote|daemon|lsp|native|integrations>",
	} {
		if !strings.Contains(got, want) {
			t.Fatalf("unknown status result missing %q: %q", want, got)
		}
	}
	if strings.Contains(got, "not implemented") {
		t.Fatalf("unknown status should not report not implemented: %q", got)
	}
}

func TestRunnerTelemetryDisabledByDefault(t *testing.T) {
	client := &fakeClient{}
	dir := t.TempDir()
	transcriptPath := filepath.Join(dir, "session.jsonl")
	runner := Runner{
		Client:           client,
		Model:            "sonnet",
		SessionID:        "sess_telemetry_disabled",
		SessionPath:      transcriptPath,
		WorkingDirectory: dir,
	}
	if _, err := runner.RunTurn(context.Background(), nil, messages.UserText("/status")); err != nil {
		t.Fatal(err)
	}
	path := telemetrypkg.SessionPath(transcriptPath, "sess_telemetry_disabled")
	if _, err := os.Stat(path); !os.IsNotExist(err) {
		t.Fatalf("telemetry file exists with disabled telemetry: %v", err)
	}
}

func TestRunnerRecordsGatedTelemetrySummaries(t *testing.T) {
	client := &fakeClient{}
	dir := t.TempDir()
	transcriptPath := filepath.Join(dir, "session.jsonl")
	telemetryEnabled := true
	runner := Runner{
		Client:           client,
		Model:            "sonnet",
		SessionID:        "sess_telemetry",
		SessionPath:      transcriptPath,
		WorkingDirectory: dir,
		MCP: &MCPConfig{UserSettings: contracts.Settings{
			Advanced: &contracts.AdvancedSetting{Telemetry: &telemetryEnabled},
		}},
	}
	if _, err := runner.RunTurn(context.Background(), nil, messages.UserText("/status")); err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(telemetrypkg.SessionPath(transcriptPath, "sess_telemetry"))
	if err != nil {
		t.Fatal(err)
	}
	raw := string(data)
	for _, reject := range []string{"/status", "Status", "Session ID"} {
		if strings.Contains(raw, reject) {
			t.Fatalf("telemetry leaked message content %q: %q", reject, raw)
		}
	}
	lines := strings.Split(strings.TrimSpace(raw), "\n")
	if len(lines) < 2 {
		t.Fatalf("telemetry lines = %d, want at least 2: %q", len(lines), raw)
	}
	var first telemetrypkg.Event
	if err := json.Unmarshal([]byte(lines[0]), &first); err != nil {
		t.Fatal(err)
	}
	if first.Timestamp == "" ||
		first.TraceID == "" ||
		first.SpanID == "" ||
		first.SessionID != "sess_telemetry" ||
		first.Type != string(EventUserMessage) ||
		first.MessageType != string(contracts.MessageUser) ||
		first.MessageUUID == "" {
		t.Fatalf("telemetry summary = %#v", first)
	}
}

func TestRunnerTelemetryEventIncludesRetryBreadcrumb(t *testing.T) {
	event := Runner{SessionID: "sess_retry"}.telemetryEvent(Event{
		Type:  EventRetry,
		Model: "sonnet",
		Retry: &RetryInfo{
			Attempt:     1,
			MaxAttempts: 2,
			FailedModel: "sonnet",
			NextModel:   "haiku",
			Fallback:    true,
		},
	})
	if event.Type != string(EventRetry) || event.Model != "sonnet" || event.RetryAttempt != 1 || event.RetryMax != 2 || event.RetryFailed != "sonnet" || event.RetryNext != "haiku" || !event.RetryFallback {
		t.Fatalf("telemetry event = %#v", event)
	}
}

func TestRunnerExportsGatedTelemetryToConfiguredBackend(t *testing.T) {
	client := &fakeClient{}
	dir := t.TempDir()
	var posted []telemetrypkg.Event
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("X-Test") != "ok" {
			t.Fatalf("headers = %#v", r.Header)
		}
		var event telemetrypkg.Event
		if err := json.NewDecoder(r.Body).Decode(&event); err != nil {
			t.Fatal(err)
		}
		posted = append(posted, event)
		w.WriteHeader(http.StatusAccepted)
	}))
	defer server.Close()
	transcriptPath := filepath.Join(dir, "session.jsonl")
	exportPath := filepath.Join(dir, "export", "events.jsonl")
	telemetryEnabled := true
	runner := Runner{
		Client:           client,
		Model:            "sonnet",
		SessionID:        "sess_telemetry_export",
		SessionPath:      transcriptPath,
		WorkingDirectory: dir,
		MCP: &MCPConfig{UserSettings: contracts.Settings{
			Advanced: &contracts.AdvancedSetting{Telemetry: &telemetryEnabled},
			TelemetryExport: &contracts.TelemetryExportSetting{
				Path:    exportPath,
				URL:     server.URL + "/collect?token=secret",
				Headers: map[string]string{"X-Test": "ok"},
			},
		}},
	}
	if _, err := runner.RunTurn(context.Background(), nil, messages.UserText("/status")); err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(exportPath)
	if err != nil {
		t.Fatal(err)
	}
	raw := string(data)
	for _, reject := range []string{"/status", "Status", "Session ID", "token=secret"} {
		if strings.Contains(raw, reject) {
			t.Fatalf("telemetry export leaked %q: %q", reject, raw)
		}
	}
	lines := strings.Split(strings.TrimSpace(raw), "\n")
	if len(lines) < 2 || len(posted) < 2 {
		t.Fatalf("export lines=%d posted=%d raw=%q posted=%#v", len(lines), len(posted), raw, posted)
	}
	var first telemetrypkg.Event
	if err := json.Unmarshal([]byte(lines[0]), &first); err != nil {
		t.Fatal(err)
	}
	if first.SessionID != "sess_telemetry_export" || first.TraceID == "" || first.SpanID == "" || posted[0].SessionID != "sess_telemetry_export" {
		t.Fatalf("exported events first=%#v posted=%#v", first, posted)
	}
	status, err := runner.RunTurn(context.Background(), nil, messages.UserText("/status show telemetry"))
	if err != nil {
		t.Fatal(err)
	}
	text := status.Messages[1].Content[0].Text
	if !strings.Contains(text, "Exporter path: "+exportPath) || !strings.Contains(text, "Exporter url: "+server.URL+"/collect") {
		t.Fatalf("telemetry status missing exporter details: %q", text)
	}
	if strings.Contains(text, "token=secret") || strings.Contains(text, "X-Test") {
		t.Fatalf("telemetry status leaked secret exporter config: %q", text)
	}
}

func TestRunnerBridgeManifestDisabledByDefault(t *testing.T) {
	t.Setenv("CLAUDE_CONFIG_DIR", t.TempDir())
	client := &fakeClient{}
	dir := t.TempDir()
	transcriptPath := filepath.Join(dir, "session.jsonl")
	runner := Runner{
		Client:           client,
		Model:            "sonnet",
		SessionID:        "sess_bridge_disabled",
		SessionPath:      transcriptPath,
		WorkingDirectory: dir,
	}
	if _, err := runner.RunTurn(context.Background(), nil, messages.UserText("/status")); err != nil {
		t.Fatal(err)
	}
	path := bridgepkg.SessionManifestPath(transcriptPath, "sess_bridge_disabled")
	if _, err := os.Stat(path); !os.IsNotExist(err) {
		t.Fatalf("bridge manifest exists with disabled bridge: %v", err)
	}
}

func TestRunnerWritesGatedBridgeManifest(t *testing.T) {
	t.Setenv("CLAUDE_CONFIG_DIR", t.TempDir())
	client := &fakeClient{}
	dir := t.TempDir()
	transcriptPath := filepath.Join(dir, "session.jsonl")
	bridgeEnabled := true
	var registeredManifest remotepkg.Manifest
	var registrationAuth string
	registrationServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/register" {
			t.Fatalf("registration path = %s", r.URL.Path)
		}
		registrationAuth = r.Header.Get("Authorization")
		if err := json.NewDecoder(r.Body).Decode(&registeredManifest); err != nil {
			t.Fatal(err)
		}
		w.Header().Set("content-type", "application/json")
		_, _ = w.Write([]byte(`{"remoteSessionId":"remote-sess","websocketUrl":"wss://remote/ws"}`))
	}))
	defer registrationServer.Close()
	runner := Runner{
		Client:           client,
		Model:            "sonnet",
		SessionID:        "sess_bridge",
		SessionPath:      transcriptPath,
		WorkingDirectory: dir,
		MCP: &MCPConfig{UserSettings: contracts.Settings{
			Advanced: &contracts.AdvancedSetting{Bridge: &bridgeEnabled},
			Remote: &contracts.RemoteSetting{
				DefaultEnvironmentID: "env-test",
				RegistrationURL:      registrationServer.URL + "/register?token=secret",
				AuthToken:            "registration-token",
			},
		}},
	}
	if _, err := runner.RunTurn(context.Background(), nil, messages.UserText("/status")); err != nil {
		t.Fatal(err)
	}
	if runner.BridgeDirectServer == nil {
		t.Fatal("bridge direct server was not started")
	}
	t.Cleanup(func() {
		ctx, cancel := context.WithTimeout(context.Background(), time.Second)
		defer cancel()
		if err := runner.BridgeDirectServer.Close(ctx); err != nil {
			t.Fatalf("close bridge direct server: %v", err)
		}
	})
	manifest, err := bridgepkg.LoadManifest(bridgepkg.SessionManifestPath(transcriptPath, "sess_bridge"))
	if err != nil {
		t.Fatal(err)
	}
	if manifest.SessionID != "sess_bridge" || manifest.WorkingDirectory != dir || manifest.GeneratedAt == "" {
		t.Fatalf("manifest metadata = %#v", manifest)
	}
	if !bridgeManifestHasCommand(manifest, "compact") || !bridgeManifestHasCommand(manifest, "clear") {
		t.Fatalf("manifest missing bridge-safe commands: %#v", manifest.Commands)
	}
	if bridgeManifestHasCommand(manifest, "status") || bridgeManifestHasCommand(manifest, "model") {
		t.Fatalf("manifest leaked unsafe commands: %#v", manifest.Commands)
	}
	if !bridgeManifestHasCapability(manifest, "remote_trigger") || !bridgeManifestHasCapability(manifest, "remote_service") {
		t.Fatalf("manifest missing remote capabilities: %#v", manifest.Capabilities)
	}
	state, err := bridgepkg.LoadDirectState(bridgepkg.SessionDirectStatePath(transcriptPath, "sess_bridge"))
	if err != nil {
		t.Fatal(err)
	}
	if state.SessionID != "sess_bridge" || state.RuntimeState != bridgepkg.DirectRuntimeRunning || state.URL == "" || state.WebSocketURL == "" || state.TokenRequired {
		t.Fatalf("direct state = %#v", state)
	}
	remoteManifest, err := remotepkg.LoadManifest(remotepkg.SessionManifestPath(transcriptPath, "sess_bridge"))
	if err != nil {
		t.Fatal(err)
	}
	if remoteManifest.SessionID != "sess_bridge" || remoteManifest.EnvironmentID != "env-test" || len(remoteManifest.Services) == 0 {
		t.Fatalf("remote manifest = %#v", remoteManifest)
	}
	if remoteManifest.Services[0].Name != "bridge" || remoteManifest.Services[0].RuntimeState != bridgepkg.DirectRuntimeRunning || remoteManifest.Services[0].Endpoint == "" || remotepkg.ServiceCapabilityNames(remoteManifest.Services[0]) == "" {
		t.Fatalf("remote bridge service = %#v", remoteManifest.Services[0])
	}
	if registeredManifest.SessionID != "sess_bridge" || len(registeredManifest.Services) == 0 || registrationAuth != "Bearer registration-token" {
		t.Fatalf("registered manifest = %#v auth=%q", registeredManifest, registrationAuth)
	}
	registrationState, err := remotepkg.LoadRegistrationState(remotepkg.SessionRegistrationPath(transcriptPath, "sess_bridge"))
	if err != nil {
		t.Fatal(err)
	}
	if registrationState.RuntimeState != remotepkg.RegistrationRegistered || registrationState.RemoteSessionID != "remote-sess" || registrationState.WebSocketURL != "wss://remote/ws" || strings.Contains(registrationState.RegistrationURL, "secret") {
		t.Fatalf("registration state = %#v", registrationState)
	}
	resp, err := http.Get(state.URL + "/health")
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("direct health status = %d", resp.StatusCode)
	}
}

func TestRunnerBridgeRemoteTriggerEndpointInjectsTeam(t *testing.T) {
	t.Setenv("CLAUDE_CONFIG_DIR", t.TempDir())
	client := &fakeClient{}
	dir := t.TempDir()
	transcriptPath := filepath.Join(dir, "session.jsonl")
	sessionID := contracts.ID("sess_bridge_remote")
	manager := session.NewSidechainManager(transcriptPath, sessionID)
	if _, err := manager.Start(session.SidechainOptions{
		ID:          "agent/remote-lead",
		AgentType:   "general-purpose",
		Description: "Remote trigger lead",
	}); err != nil {
		t.Fatal(err)
	}
	if _, _, err := manager.CreateTeam(session.TeamOptions{
		ID:                "remote/team",
		Description:       "Remote team",
		CoordinatorTaskID: "agent/remote-lead",
	}); err != nil {
		t.Fatal(err)
	}
	bridgeEnabled := true
	runner := Runner{
		Client:           client,
		Model:            "sonnet",
		SessionID:        sessionID,
		SessionPath:      transcriptPath,
		WorkingDirectory: dir,
		MCP: &MCPConfig{UserSettings: contracts.Settings{
			Advanced: &contracts.AdvancedSetting{Bridge: &bridgeEnabled},
		}},
	}
	if _, err := runner.RunTurn(context.Background(), nil, messages.UserText("/status")); err != nil {
		t.Fatal(err)
	}
	if runner.BridgeDirectServer == nil {
		t.Fatal("bridge direct server was not started")
	}
	t.Cleanup(func() {
		ctx, cancel := context.WithTimeout(context.Background(), time.Second)
		defer cancel()
		if err := runner.BridgeDirectServer.Close(ctx); err != nil {
			t.Fatalf("close bridge direct server: %v", err)
		}
	})
	resp, err := http.Post(runner.BridgeDirectServer.URL()+"/remote-trigger", "application/json", strings.NewReader(`{"team_id":"remote/team","event_id":"delivery-1","source":"webhook","event":"deploy","message":"Deploy now."}`))
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("remote trigger status = %d", resp.StatusCode)
	}
	var response bridgepkg.DirectRemoteTriggerResponse
	if err := json.NewDecoder(resp.Body).Decode(&response); err != nil {
		t.Fatal(err)
	}
	if !response.Accepted || response.Duplicate || response.TeamID != "remote_team" || response.Target != "coordinator" || response.EventID != "delivery-1" || response.SentCount != 1 {
		t.Fatalf("remote trigger response = %#v", response)
	}
	resume, err := manager.ResumeContext("agent/remote-lead", 2)
	if err != nil {
		t.Fatal(err)
	}
	if len(resume.Messages) != 2 {
		t.Fatalf("remote lead messages = %#v", resume.Messages)
	}
	text := messages.TextContent(resume.Messages[1])
	if !strings.Contains(text, "Remote trigger received.") || !strings.Contains(text, "Source: webhook") || !strings.Contains(text, "Event: deploy") || !strings.Contains(text, "Deploy now.") {
		t.Fatalf("remote trigger message = %q", text)
	}
}

func TestRunnerLSPManagerStatusDisabledByDefault(t *testing.T) {
	client := &fakeClient{}
	dir := t.TempDir()
	transcriptPath := filepath.Join(dir, "session.jsonl")
	runner := Runner{
		Client:           client,
		Model:            "sonnet",
		SessionID:        "sess_lsp_disabled",
		SessionPath:      transcriptPath,
		WorkingDirectory: dir,
	}
	if _, err := runner.RunTurn(context.Background(), nil, messages.UserText("/status")); err != nil {
		t.Fatal(err)
	}
	path := lsppkg.SessionManagerStatusPath(transcriptPath, "sess_lsp_disabled")
	if _, err := os.Stat(path); !os.IsNotExist(err) {
		t.Fatalf("lsp manager status exists with disabled lsp: %v", err)
	}
}

func TestRunnerWritesGatedLSPManagerStatus(t *testing.T) {
	client := &fakeClient{}
	dir := t.TempDir()
	t.Setenv("PATH", t.TempDir())
	if err := os.WriteFile(filepath.Join(dir, "go.mod"), []byte("module example\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	transcriptPath := filepath.Join(dir, "session.jsonl")
	lspEnabled := true
	runner := Runner{
		Client:           client,
		Model:            "sonnet",
		SessionID:        "sess_lsp_manager",
		SessionPath:      transcriptPath,
		WorkingDirectory: dir,
		MCP: &MCPConfig{UserSettings: contracts.Settings{
			Advanced: &contracts.AdvancedSetting{LSP: &lspEnabled},
		}},
	}
	if _, err := runner.RunTurn(context.Background(), nil, messages.UserText("/status")); err != nil {
		t.Fatal(err)
	}
	status, err := lsppkg.LoadManagerStatus(lsppkg.SessionManagerStatusPath(transcriptPath, "sess_lsp_manager"))
	if err != nil {
		t.Fatal(err)
	}
	if status.SessionID != "sess_lsp_manager" || status.WorkingDirectory != dir || status.GeneratedAt == "" {
		t.Fatalf("lsp manager status metadata = %#v", status)
	}
	gopls := lspServerStatus(status.Servers, "gopls")
	if gopls.RuntimeState != lsppkg.ServerRuntimeNotStarted || len(gopls.MatchReasons) == 0 {
		t.Fatalf("gopls status = %#v", gopls)
	}
	if !strings.Contains(gopls.Reason, "command not found") {
		t.Fatalf("gopls reason = %q", gopls.Reason)
	}
}

func TestRunnerStartsConfiguredLSPServer(t *testing.T) {
	client := &fakeClient{}
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "go.mod"), []byte("module example\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	transcriptPath := filepath.Join(dir, "session.jsonl")
	lspEnabled := true
	runner := Runner{
		Client:           client,
		Model:            "sonnet",
		SessionID:        "sess_lsp_start",
		SessionPath:      transcriptPath,
		WorkingDirectory: dir,
		MCP: &MCPConfig{UserSettings: contracts.Settings{
			Advanced: &contracts.AdvancedSetting{LSP: &lspEnabled},
		}},
		LSPServerDefinitions: []lsppkg.ServerDefinition{conversationLSPHelperDefinition()},
		LSPStartupDocuments: []lsppkg.OpenDocument{{
			URI:        "file:///work/main.go",
			LanguageID: "go",
			Version:    1,
			Text:       "package main\n",
		}},
	}
	if _, err := runner.RunTurn(context.Background(), nil, messages.UserText("/status")); err != nil {
		t.Fatal(err)
	}
	process := runner.LSPProcesses["conversation-lsp-helper"]
	if process == nil {
		t.Fatalf("lsp process was not recorded: %#v", runner.LSPProcesses)
	}
	result := waitForConversationLSPProcess(t, process, "lsp")
	if result.RuntimeState != lsppkg.ServerRuntimeExited || result.Diagnostics.InitializeResponses != 1 || result.Diagnostics.DiagnosticsUpdates != 1 {
		t.Fatalf("lsp process result = %#v", result)
	}
	status, err := lsppkg.LoadManagerStatus(lsppkg.SessionManagerStatusPath(transcriptPath, "sess_lsp_start"))
	if err != nil {
		t.Fatal(err)
	}
	server := lspServerStatus(status.Servers, "conversation-lsp-helper")
	if server.RuntimeState != lsppkg.ServerRuntimeExited || server.ProcessID == 0 || server.StartedAt == "" || server.EndedAt == "" {
		t.Fatalf("server status = %#v", server)
	}
	diagnostics, err := lsppkg.LoadSnapshot(lsppkg.SessionDiagnosticsPath(transcriptPath, "sess_lsp_start"))
	if err != nil {
		t.Fatal(err)
	}
	if len(diagnostics) != 1 || diagnostics[0].Message != "runner lsp diagnostic" {
		t.Fatalf("diagnostics = %#v", diagnostics)
	}
}

func TestRunnerStartsDefaultLSPServer(t *testing.T) {
	client := &fakeClient{}
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "go.mod"), []byte("module example\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	writeConversationLSPCommandShim(t, "gopls")
	transcriptPath := filepath.Join(dir, "session.jsonl")
	lspEnabled := true
	runner := Runner{
		Client:           client,
		Model:            "sonnet",
		SessionID:        "sess_lsp_default_start",
		SessionPath:      transcriptPath,
		WorkingDirectory: dir,
		MCP: &MCPConfig{UserSettings: contracts.Settings{
			Advanced: &contracts.AdvancedSetting{LSP: &lspEnabled},
		}},
		LSPStartupDocuments: []lsppkg.OpenDocument{{
			URI:        "file:///work/main.go",
			LanguageID: "go",
			Version:    1,
			Text:       "package main\n",
		}},
	}
	if _, err := runner.RunTurn(context.Background(), nil, messages.UserText("/status")); err != nil {
		t.Fatal(err)
	}
	process := runner.LSPProcesses["gopls"]
	if process == nil {
		t.Fatalf("default lsp process was not recorded: %#v", runner.LSPProcesses)
	}
	result := waitForConversationLSPProcess(t, process, "default lsp")
	if result.RuntimeState != lsppkg.ServerRuntimeExited || result.Diagnostics.InitializeResponses != 1 || result.Diagnostics.DiagnosticsUpdates != 1 {
		t.Fatalf("default lsp process result = %#v", result)
	}
	status, err := lsppkg.LoadManagerStatus(lsppkg.SessionManagerStatusPath(transcriptPath, "sess_lsp_default_start"))
	if err != nil {
		t.Fatal(err)
	}
	server := lspServerStatus(status.Servers, "gopls")
	if server.RuntimeState != lsppkg.ServerRuntimeExited || server.ProcessID == 0 || server.StartedAt == "" || server.EndedAt == "" {
		t.Fatalf("default server status = %#v", server)
	}
	diagnostics, err := lsppkg.LoadSnapshot(lsppkg.SessionDiagnosticsPath(transcriptPath, "sess_lsp_default_start"))
	if err != nil {
		t.Fatal(err)
	}
	if len(diagnostics) != 1 || diagnostics[0].Message != "runner lsp diagnostic" {
		t.Fatalf("default diagnostics = %#v", diagnostics)
	}
}

func TestRunnerWritesGatedNativeManifest(t *testing.T) {
	client := &fakeClient{}
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "main.go"), []byte("package main\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	transcriptPath := filepath.Join(dir, "session.jsonl")
	nativeEnabled := true
	runner := Runner{
		Client:           client,
		Model:            "sonnet",
		SessionID:        "sess_native",
		SessionPath:      transcriptPath,
		WorkingDirectory: dir,
		MCP: &MCPConfig{UserSettings: contracts.Settings{
			Advanced: &contracts.AdvancedSetting{NativeIntegrations: &nativeEnabled},
		}},
	}
	if _, err := runner.RunTurn(context.Background(), nil, messages.UserText("/status")); err != nil {
		t.Fatal(err)
	}
	manifest, err := nativepkg.LoadManifest(nativepkg.SessionManifestPath(transcriptPath, "sess_native"))
	if err != nil {
		t.Fatal(err)
	}
	if manifest.SessionID != "sess_native" || manifest.WorkingDirectory != dir || manifest.GeneratedAt == "" {
		t.Fatalf("manifest metadata = %#v", manifest)
	}
	if len(manifest.Capabilities) == 0 || nativepkg.CountAvailable(manifest.Capabilities) == 0 {
		t.Fatalf("manifest capabilities = %#v", manifest.Capabilities)
	}
	if !nativeCapabilityAvailable(manifest.Capabilities, "native_file_index", true) ||
		!nativeCapabilityAvailable(manifest.Capabilities, "native_clipboard", true) ||
		!nativeCapabilityAvailable(manifest.Capabilities, "native_color_diff", true) {
		t.Fatalf("manifest capabilities = %#v", manifest.Capabilities)
	}
	clipboard, err := nativepkg.LoadClipboard(nativepkg.SessionClipboardPath(transcriptPath, "sess_native"))
	if err != nil {
		t.Fatal(err)
	}
	if clipboard.SessionID != "sess_native" || clipboard.UpdatedAt == "" || len(clipboard.Items) != 0 {
		t.Fatalf("clipboard state = %#v", clipboard)
	}
	index, err := nativepkg.LoadFileIndex(nativepkg.SessionFileIndexPath(transcriptPath, "sess_native"))
	if err != nil {
		t.Fatal(err)
	}
	if index.SessionID != "sess_native" || index.WorkingDirectory != dir || index.GeneratedAt == "" {
		t.Fatalf("file index metadata = %#v", index)
	}
	if !nativeIndexHasPath(index.Files, "main.go") {
		t.Fatalf("file index entries = %#v", index.Files)
	}
}

func TestRunnerExecutesNativeClipboardCommandWithoutQuery(t *testing.T) {
	setupFakeClipboardCommandPath(t)
	client := &fakeClient{}
	dir := t.TempDir()
	transcriptPath := filepath.Join(dir, "session.jsonl")
	var gotCommand []string
	var gotStdin string
	runner := Runner{
		Client:           client,
		Model:            "sonnet",
		SessionID:        "sess_native_command",
		SessionPath:      transcriptPath,
		WorkingDirectory: dir,
		NativeClipboardRunner: func(ctx context.Context, command []string, stdin string) (string, error) {
			gotCommand = append([]string(nil), command...)
			gotStdin = stdin
			return "", nil
		},
	}
	result, err := runner.RunTurn(context.Background(), nil, messages.UserText("/native clipboard write hello world"))
	if err != nil {
		t.Fatal(err)
	}
	if len(client.requests) != 0 {
		t.Fatalf("client requests = %d, want 0", len(client.requests))
	}
	if len(result.Messages) == 0 || !strings.Contains(result.Messages[len(result.Messages)-1].Content[0].Text, "Native clipboard write") {
		t.Fatalf("messages = %#v", result.Messages)
	}
	if len(gotCommand) == 0 || gotStdin != "hello world" {
		t.Fatalf("command = %#v stdin=%q", gotCommand, gotStdin)
	}
	text, ok, err := nativepkg.ReadClipboardText(nativepkg.SessionClipboardPath(transcriptPath, "sess_native_command"), "clipboard")
	if err != nil {
		t.Fatal(err)
	}
	if !ok || text != "hello world" {
		t.Fatalf("clipboard text = %q ok=%v", text, ok)
	}
}

func TestRunnerExecutesNativeClipboardReadCommandWithoutQuery(t *testing.T) {
	setupFakeClipboardCommandPath(t)
	client := &fakeClient{}
	dir := t.TempDir()
	runner := Runner{
		Client:           client,
		Model:            "sonnet",
		SessionID:        "sess_native_read",
		SessionPath:      filepath.Join(dir, "session.jsonl"),
		WorkingDirectory: dir,
		NativeClipboardRunner: func(ctx context.Context, command []string, stdin string) (string, error) {
			return "from external", nil
		},
	}
	result, err := runner.RunTurn(context.Background(), nil, messages.UserText("/native clipboard read"))
	if err != nil {
		t.Fatal(err)
	}
	if len(client.requests) != 0 {
		t.Fatalf("client requests = %d, want 0", len(client.requests))
	}
	if len(result.Messages) == 0 || !strings.Contains(result.Messages[len(result.Messages)-1].Content[0].Text, "Text: from external") {
		t.Fatalf("messages = %#v", result.Messages)
	}
}

func TestRunnerNativeClipboardStatusCommandWithoutQuery(t *testing.T) {
	setupFakeClipboardCommandPath(t)
	client := &fakeClient{}
	dir := t.TempDir()
	sessionPath := filepath.Join(dir, "session.jsonl")
	sessionID := contracts.ID("sess_native_clipboard_status")
	clipboardPath := nativepkg.SessionClipboardPath(sessionPath, sessionID)
	if _, err := nativepkg.WriteClipboardText(clipboardPath, sessionID, "clipboard", "secret status text"); err != nil {
		t.Fatal(err)
	}
	runnerCalled := false
	runner := Runner{
		Client:           client,
		Model:            "sonnet",
		SessionID:        sessionID,
		SessionPath:      sessionPath,
		WorkingDirectory: dir,
		NativeClipboardRunner: func(ctx context.Context, command []string, stdin string) (string, error) {
			runnerCalled = true
			return "", nil
		},
	}
	for _, prompt := range []string{"/native clipboard status", "/native clipboard show"} {
		result, err := runner.RunTurn(context.Background(), nil, messages.UserText(prompt))
		if err != nil {
			t.Fatal(err)
		}
		if len(client.requests) != 0 {
			t.Fatalf("%s queried model, requests = %d", prompt, len(client.requests))
		}
		if runnerCalled {
			t.Fatalf("%s invoked external clipboard runner", prompt)
		}
		text := result.Messages[len(result.Messages)-1].Content[0].Text
		for _, want := range []string{
			"Native clipboard status",
			"Clipboard path: " + clipboardPath,
			"Session clipboard items: 1",
			"Clipboard updated:",
			"Clipboard adapters:",
			"- osc52: available (terminal)",
		} {
			if !strings.Contains(text, want) {
				t.Fatalf("%s status missing %q: %q", prompt, want, text)
			}
		}
		if strings.Contains(text, "secret status text") {
			t.Fatalf("%s leaked clipboard text: %q", prompt, text)
		}
	}
}

func TestRunnerNativeStatusCommandWithoutQuery(t *testing.T) {
	client := &fakeClient{}
	dir := t.TempDir()
	sessionID := contracts.ID("sess_native_status")
	sessionPath := filepath.Join(dir, "session.jsonl")
	manifestPath := nativepkg.SessionManifestPath(sessionPath, sessionID)
	if err := nativepkg.WriteManifest(manifestPath, nativepkg.Manifest{
		SessionID: sessionID,
		GOOS:      "testos",
		GOARCH:    "testarch",
		Terminal:  "xterm-256color",
		ClipboardAdapters: []nativepkg.ClipboardAdapter{
			{Name: "osc52", Kind: nativepkg.ClipboardAdapterKindTerminal, Available: true},
		},
		Capabilities: []nativepkg.Capability{
			{Name: "native_clipboard", Available: false},
			{Name: "osc52_clipboard", Available: true},
		},
	}); err != nil {
		t.Fatal(err)
	}
	runner := Runner{
		Client:           client,
		Model:            "sonnet",
		SessionID:        sessionID,
		SessionPath:      sessionPath,
		WorkingDirectory: dir,
	}
	for _, prompt := range []string{"/native status", "/native show"} {
		result, err := runner.RunTurn(context.Background(), nil, messages.UserText(prompt))
		if err != nil {
			t.Fatal(err)
		}
		if len(client.requests) != 0 {
			t.Fatalf("%s queried model, requests = %d", prompt, len(client.requests))
		}
		text := result.Messages[len(result.Messages)-1].Content[0].Text
		for _, want := range []string{
			"Status native integrations",
			"Enabled: disabled",
			"Manifest path: " + manifestPath,
			"Platform: testos/testarch",
			"Capabilities: 2",
			"Available capabilities: 1",
			"Clipboard adapters: 1",
			"Terminal: xterm-256color",
			"- native_clipboard: unavailable",
			"- osc52_clipboard: available",
		} {
			if !strings.Contains(text, want) {
				t.Fatalf("%s status missing %q: %q", prompt, want, text)
			}
		}
	}
}

func TestRunnerNativeCommandReportsUsageForUnknownCommand(t *testing.T) {
	runner := Runner{Client: &fakeClient{}, SessionID: "sess_native_unknown"}
	result, err := runner.RunTurn(context.Background(), nil, messages.UserText("/native unknown"))
	if err != nil {
		t.Fatal(err)
	}
	got := result.Messages[len(result.Messages)-1].Content[0].Text
	for _, want := range []string{
		"Unknown native command: unknown",
		"Usage: /native <clipboard|chrome|voice|computer>",
	} {
		if !strings.Contains(got, want) {
			t.Fatalf("unknown native result missing %q: %q", want, got)
		}
	}
	if strings.Contains(got, "not implemented") {
		t.Fatalf("unknown native should not report not implemented: %q", got)
	}
}

func TestRunnerExecutesNativeChromeInstallCommandWithoutQuery(t *testing.T) {
	client := &fakeClient{}
	dir := t.TempDir()
	installDir := filepath.Join(dir, "NativeMessagingHosts")
	t.Setenv("CLAUDE_CHROME_NATIVE_HOST_INSTALL_DIR", installDir)
	runner := Runner{
		Client:           client,
		Model:            "sonnet",
		SessionID:        "sess_native_chrome",
		SessionPath:      filepath.Join(dir, "session.jsonl"),
		WorkingDirectory: dir,
	}
	result, err := runner.RunTurn(context.Background(), nil, messages.UserText("/native chrome install"))
	if err != nil {
		t.Fatal(err)
	}
	if len(client.requests) != 0 {
		t.Fatalf("client requests = %d, want 0", len(client.requests))
	}
	targetPath := filepath.Join(installDir, integrationspkg.ChromeNativeHostName+".json")
	manifest, err := integrationspkg.LoadChromeNativeHostManifest(targetPath)
	if err != nil {
		t.Fatal(err)
	}
	if manifest.Name != integrationspkg.ChromeNativeHostName || !strings.HasPrefix(manifest.Path, installDir) || !strings.Contains(manifest.Path, "chrome-native-host") {
		t.Fatalf("installed manifest = %#v", manifest)
	}
	if len(result.Messages) == 0 || !strings.Contains(result.Messages[len(result.Messages)-1].Content[0].Text, "Installed manifest: "+targetPath) {
		t.Fatalf("messages = %#v", result.Messages)
	}
}

func TestRunnerNativeVoiceStatusCommandWithoutQuery(t *testing.T) {
	setupFakeNativeIntegrationCommandPath(t)
	client := &fakeClient{}
	dir := t.TempDir()
	runner := Runner{
		Client:           client,
		Model:            "sonnet",
		SessionID:        "sess_native_voice_status",
		SessionPath:      filepath.Join(dir, "session.jsonl"),
		WorkingDirectory: dir,
	}
	for _, prompt := range []string{"/native voice status", "/native voice show"} {
		result, err := runner.RunTurn(context.Background(), nil, messages.UserText(prompt))
		if err != nil {
			t.Fatal(err)
		}
		if len(client.requests) != 0 {
			t.Fatalf("%s queried model, requests = %d", prompt, len(client.requests))
		}
		text := result.Messages[len(result.Messages)-1].Content[0].Text
		for _, want := range []string{
			"Native voice status",
			"Plan path: " + integrationspkg.VoiceCapturePlanPath(runner.SessionPath, runner.SessionID),
			"Adapter available: enabled",
			"Sample rate: 16000",
			"Encoding: pcm_s16le",
		} {
			if !strings.Contains(text, want) {
				t.Fatalf("%s status missing %q: %q", prompt, want, text)
			}
		}
	}
}

func TestRunnerExecutesNativeVoiceCaptureCommandWithoutQuery(t *testing.T) {
	setupFakeNativeIntegrationCommandPath(t)
	client := &fakeClient{}
	dir := t.TempDir()
	runner := Runner{
		Client:           client,
		Model:            "sonnet",
		SessionID:        "sess_native_voice",
		SessionPath:      filepath.Join(dir, "session.jsonl"),
		WorkingDirectory: dir,
		NativeVoiceRunner: func(ctx context.Context, command []string, maxBytes int64) ([]byte, bool, error) {
			return []byte{1, 2, 3}, false, nil
		},
	}
	result, err := runner.RunTurn(context.Background(), nil, messages.UserText("/native voice capture"))
	if err != nil {
		t.Fatal(err)
	}
	if len(client.requests) != 0 {
		t.Fatalf("client requests = %d, want 0", len(client.requests))
	}
	if len(result.Messages) == 0 || !strings.Contains(result.Messages[len(result.Messages)-1].Content[0].Text, "Audio bytes: 3") {
		t.Fatalf("messages = %#v", result.Messages)
	}
}

func TestRunnerExecutesNativeVoiceTranscribeCommandWithoutQuery(t *testing.T) {
	setupFakeNativeIntegrationCommandPath(t)
	t.Setenv("CLAUDE_VOICE_TRANSCRIBE_COMMAND", "mock-stt --format pcm_s16le")
	client := &fakeClient{}
	dir := t.TempDir()
	var gotAudio []byte
	runner := Runner{
		Client:           client,
		Model:            "sonnet",
		SessionID:        "sess_native_voice_transcribe",
		SessionPath:      filepath.Join(dir, "session.jsonl"),
		WorkingDirectory: dir,
		NativeVoiceRunner: func(ctx context.Context, command []string, maxBytes int64) ([]byte, bool, error) {
			return []byte{1, 2, 3}, false, nil
		},
		NativeVoiceTranscribeRunner: func(ctx context.Context, command []string, audio []byte, maxBytes int64) (string, bool, error) {
			gotAudio = append([]byte(nil), audio...)
			return "hello from voice", false, nil
		},
	}
	result, err := runner.RunTurn(context.Background(), nil, messages.UserText("/native voice transcribe"))
	if err != nil {
		t.Fatal(err)
	}
	if len(client.requests) != 0 {
		t.Fatalf("client requests = %d, want 0", len(client.requests))
	}
	if len(gotAudio) != 3 {
		t.Fatalf("transcribe audio = %#v", gotAudio)
	}
	if len(result.Messages) == 0 || !strings.Contains(result.Messages[len(result.Messages)-1].Content[0].Text, "Transcript: hello from voice") {
		t.Fatalf("messages = %#v", result.Messages)
	}
}

func TestRunnerNativeComputerStatusCommandWithoutQuery(t *testing.T) {
	setupFakeNativeIntegrationCommandPath(t)
	client := &fakeClient{}
	dir := t.TempDir()
	runner := Runner{
		Client:           client,
		Model:            "sonnet",
		SessionID:        "sess_native_computer_status",
		SessionPath:      filepath.Join(dir, "session.jsonl"),
		WorkingDirectory: dir,
	}
	for _, prompt := range []string{"/native computer status", "/native computer show"} {
		result, err := runner.RunTurn(context.Background(), nil, messages.UserText(prompt))
		if err != nil {
			t.Fatal(err)
		}
		if len(client.requests) != 0 {
			t.Fatalf("%s queried model, requests = %d", prompt, len(client.requests))
		}
		text := result.Messages[len(result.Messages)-1].Content[0].Text
		for _, want := range []string{
			"Native computer status",
			"Plan path: " + integrationspkg.ComputerUseDriverPlanPath(runner.SessionPath, runner.SessionID),
			"Screen capture available: enabled",
			"Input control available: enabled",
			"Screenshot format: png",
			"Coordinate system: screen_pixels",
		} {
			if !strings.Contains(text, want) {
				t.Fatalf("%s status missing %q: %q", prompt, want, text)
			}
		}
	}
}

func TestRunnerExecutesNativeComputerScreenshotCommandWithoutQuery(t *testing.T) {
	setupFakeNativeIntegrationCommandPath(t)
	client := &fakeClient{}
	dir := t.TempDir()
	runner := Runner{
		Client:           client,
		Model:            "sonnet",
		SessionID:        "sess_native_computer",
		SessionPath:      filepath.Join(dir, "session.jsonl"),
		WorkingDirectory: dir,
		NativeComputerUseRunner: func(ctx context.Context, command []string, stdin string, maxBytes int64) ([]byte, bool, error) {
			return []byte{0x89, 'P', 'N', 'G'}, false, nil
		},
	}
	result, err := runner.RunTurn(context.Background(), nil, messages.UserText("/native computer screenshot"))
	if err != nil {
		t.Fatal(err)
	}
	if len(client.requests) != 0 {
		t.Fatalf("client requests = %d, want 0", len(client.requests))
	}
	if len(result.Messages) == 0 || !strings.Contains(result.Messages[len(result.Messages)-1].Content[0].Text, "Image bytes: 4") {
		t.Fatalf("messages = %#v", result.Messages)
	}
}

func TestParseNativeComputerInputActionClick(t *testing.T) {
	action, err := parseNativeComputerInputAction("click 12 34 3")
	if err != nil {
		t.Fatal(err)
	}
	if action.Type != "click" || !action.HasPosition || action.X != 12 || action.Y != 34 || action.Button != 3 {
		t.Fatalf("action = %#v", action)
	}
}

func TestRunnerHandlesNativeComputerInputCommandWithoutQuery(t *testing.T) {
	setupFakeNativeIntegrationCommandPath(t)
	client := &fakeClient{}
	dir := t.TempDir()
	var gotCommand []string
	runner := Runner{
		Client:           client,
		Model:            "sonnet",
		SessionID:        "sess_native_computer_input",
		SessionPath:      filepath.Join(dir, "session.jsonl"),
		WorkingDirectory: dir,
		NativeComputerUseRunner: func(ctx context.Context, command []string, stdin string, maxBytes int64) ([]byte, bool, error) {
			gotCommand = append([]string(nil), command...)
			return nil, false, nil
		},
	}
	result, err := runner.RunTurn(context.Background(), nil, messages.UserText("/native computer click 12 34 3"))
	if err != nil {
		t.Fatal(err)
	}
	if len(client.requests) != 0 {
		t.Fatalf("client requests = %d, want 0", len(client.requests))
	}
	if len(result.Messages) == 0 || !strings.Contains(result.Messages[len(result.Messages)-1].Content[0].Text, "Native computer input") {
		t.Fatalf("messages = %#v", result.Messages)
	}
	if runtime.GOOS == "linux" {
		wantSuffix := []string{"mousemove", "12", "34", "click", "3"}
		if !hasStringSuffix(gotCommand, wantSuffix) {
			t.Fatalf("command = %#v, want suffix %#v", gotCommand, wantSuffix)
		}
	}
}

func setupFakeNativeIntegrationCommandPath(t *testing.T) {
	t.Helper()
	dir := t.TempDir()
	names := []string{
		"pw-record", "parecord", "arecord", "rec", "sox", "ffmpeg", "ffmpeg.exe",
		"grim", "gnome-screenshot", "import", "screencapture", "powershell.exe", "xdotool", "ydotool", "osascript",
	}
	for _, name := range names {
		path := filepath.Join(dir, name)
		body := "#!/bin/sh\nexit 0\n"
		if runtime.GOOS == "windows" {
			body = "@echo off\r\nexit /b 0\r\n"
		}
		if err := os.WriteFile(path, []byte(body), 0o755); err != nil {
			t.Fatal(err)
		}
	}
	t.Setenv("PATH", dir)
	t.Setenv("WAYLAND_DISPLAY", "")
	t.Setenv("DISPLAY", ":0")
}

func hasStringSuffix(values []string, suffix []string) bool {
	if len(values) < len(suffix) {
		return false
	}
	offset := len(values) - len(suffix)
	for i := range suffix {
		if values[offset+i] != suffix[i] {
			return false
		}
	}
	return true
}

func setupFakeClipboardCommandPath(t *testing.T) {
	t.Helper()
	dir := t.TempDir()
	names := []string{"pbcopy", "pbpaste", "xclip", "xsel", "tmux", "powershell.exe", "pwsh", "clip.exe"}
	for _, name := range names {
		path := filepath.Join(dir, name)
		body := "#!/bin/sh\nexit 0\n"
		if runtime.GOOS == "windows" {
			body = "@echo off\r\nexit /b 0\r\n"
		}
		if err := os.WriteFile(path, []byte(body), 0o755); err != nil {
			t.Fatal(err)
		}
	}
	t.Setenv("PATH", dir)
	t.Setenv("WAYLAND_DISPLAY", "")
	t.Setenv("DISPLAY", ":0")
	t.Setenv("TMUX", "")
}

func TestRunnerIntegrationsManifestDisabledByDefault(t *testing.T) {
	client := &fakeClient{}
	dir := t.TempDir()
	transcriptPath := filepath.Join(dir, "session.jsonl")
	runner := Runner{
		Client:           client,
		Model:            "sonnet",
		SessionID:        "sess_integrations_disabled",
		SessionPath:      transcriptPath,
		WorkingDirectory: dir,
	}
	if _, err := runner.RunTurn(context.Background(), nil, messages.UserText("/status")); err != nil {
		t.Fatal(err)
	}
	path := integrationspkg.SessionManifestPath(transcriptPath, "sess_integrations_disabled")
	if _, err := os.Stat(path); !os.IsNotExist(err) {
		t.Fatalf("integrations manifest exists with disabled integrations: %v", err)
	}
}

func TestRunnerWritesGatedIntegrationsManifest(t *testing.T) {
	client := &fakeClient{}
	dir := t.TempDir()
	transcriptPath := filepath.Join(dir, "session.jsonl")
	chromeEnabled := true
	computerUseEnabled := true
	voiceEnabled := true
	runner := Runner{
		Client:           client,
		Model:            "sonnet",
		SessionID:        "sess_integrations",
		SessionPath:      transcriptPath,
		WorkingDirectory: dir,
		MCP: &MCPConfig{UserSettings: contracts.Settings{
			Advanced: &contracts.AdvancedSetting{
				Chrome:      &chromeEnabled,
				ComputerUse: &computerUseEnabled,
				Voice:       &voiceEnabled,
			},
		}},
	}
	if _, err := runner.RunTurn(context.Background(), nil, messages.UserText("/status")); err != nil {
		t.Fatal(err)
	}
	manifest, err := integrationspkg.LoadManifest(integrationspkg.SessionManifestPath(transcriptPath, "sess_integrations"))
	if err != nil {
		t.Fatal(err)
	}
	if manifest.SessionID != "sess_integrations" || manifest.WorkingDirectory != dir || manifest.GeneratedAt == "" {
		t.Fatalf("manifest metadata = %#v", manifest)
	}
	if integrationspkg.CountEnabled(manifest.Integrations) != 3 {
		t.Fatalf("manifest integrations = %#v", manifest.Integrations)
	}
	if !integrationHasState(manifest.Integrations, "chrome", true, integrationspkg.RuntimeStateReady) ||
		!integrationHasState(manifest.Integrations, "computer_use", true, integrationspkg.RuntimeStateReady) ||
		!integrationHasState(manifest.Integrations, "voice", true, integrationspkg.RuntimeStateReady) {
		t.Fatalf("manifest integration states = %#v", manifest.Integrations)
	}
	chromeState, err := integrationspkg.LoadRuntimeState(integrationspkg.SessionRuntimeStatePath(transcriptPath, "sess_integrations", "chrome"))
	if err != nil {
		t.Fatal(err)
	}
	if chromeState.SessionID != "sess_integrations" || chromeState.Name != "chrome" || chromeState.RuntimeState != integrationspkg.RuntimeStateReady || chromeState.Artifacts["state"] == "" {
		t.Fatalf("chrome runtime state = %#v", chromeState)
	}
	chromeManifestPath := chromeState.Artifacts["chrome_native_host_manifest"]
	if chromeManifestPath == "" {
		t.Fatalf("chrome native host manifest artifact missing: %#v", chromeState.Artifacts)
	}
	chromeWrapperPath := chromeState.Artifacts["chrome_native_host_wrapper"]
	if chromeWrapperPath == "" {
		t.Fatalf("chrome native host wrapper artifact missing: %#v", chromeState.Artifacts)
	}
	chromeManifest, err := integrationspkg.LoadChromeNativeHostManifest(chromeManifestPath)
	if err != nil {
		t.Fatal(err)
	}
	if chromeManifest.Name != integrationspkg.ChromeNativeHostName || chromeManifest.Type != "stdio" || chromeManifest.Path != chromeWrapperPath {
		t.Fatalf("chrome native host manifest = %#v", chromeManifest)
	}
	if info, err := os.Stat(chromeWrapperPath); err != nil || info.Mode().Perm() != 0o755 {
		t.Fatalf("chrome native host wrapper stat = %v info=%v", err, info)
	}
	computerState, err := integrationspkg.LoadRuntimeState(integrationspkg.SessionRuntimeStatePath(transcriptPath, "sess_integrations", "computer_use"))
	if err != nil {
		t.Fatal(err)
	}
	if computerState.Name != "computer_use" || computerState.RuntimeState != integrationspkg.RuntimeStateReady {
		t.Fatalf("computer runtime state = %#v", computerState)
	}
	computerUsePlanPath := computerState.Artifacts["computer_use_driver_plan"]
	if computerUsePlanPath == "" {
		t.Fatalf("computer-use driver plan artifact missing: %#v", computerState.Artifacts)
	}
	computerUsePlan, err := integrationspkg.LoadComputerUseDriverPlan(computerUsePlanPath)
	if err != nil {
		t.Fatal(err)
	}
	if computerUsePlan.SessionID != "sess_integrations" || computerUsePlan.ScreenshotFormat != "png" || computerUsePlan.ExecutionMode != "planned" {
		t.Fatalf("computer-use driver plan = %#v", computerUsePlan)
	}
	voiceState, err := integrationspkg.LoadRuntimeState(integrationspkg.SessionRuntimeStatePath(transcriptPath, "sess_integrations", "voice"))
	if err != nil {
		t.Fatal(err)
	}
	voicePlanPath := voiceState.Artifacts["voice_capture_plan"]
	if voicePlanPath == "" {
		t.Fatalf("voice capture plan artifact missing: %#v", voiceState.Artifacts)
	}
	voicePlan, err := integrationspkg.LoadVoiceCapturePlan(voicePlanPath)
	if err != nil {
		t.Fatal(err)
	}
	if voicePlan.SessionID != "sess_integrations" || voicePlan.SampleRateHz != 16000 || !voicePlan.Streaming {
		t.Fatalf("voice capture plan = %#v", voicePlan)
	}
}

func TestFormatRemoteRegistrationReportsUnsupportedProtocol(t *testing.T) {
	lines := formatRemoteRegistration(remotepkg.RegistrationState{
		RuntimeState:    remotepkg.RegistrationFailed,
		StatusCode:      http.StatusOK,
		RemoteSessionID: "remote-future",
		ProtocolVersion: "ccr.remote.v99",
		Error:           "unsupported remote protocol version \"ccr.remote.v99\"; supported: ccr.remote.v1, ccr.remote.v2",
	})
	text := strings.Join(lines, "\n")
	if !strings.Contains(text, "Remote registration: failed: status 200: remote session remote-future: protocol ccr.remote.v99") || !strings.Contains(text, "unsupported remote protocol version") {
		t.Fatalf("registration status = %q", text)
	}
}

func TestFormatRemoteRegistrationReportsCapabilityWarnings(t *testing.T) {
	lines := formatRemoteRegistration(remotepkg.RegistrationState{
		RuntimeState:       remotepkg.RegistrationRegistered,
		RemoteSessionID:    "remote-limited",
		ProtocolVersion:    remotepkg.RemoteProtocolVersionV1,
		Capabilities:       []string{"remote_trigger"},
		PollURL:            "https://remote.example/poll?token=secret",
		CapabilityWarnings: []string{"websocket url ignored: missing websocket_protocol capability", "lease renew url ignored: missing lease_renew capability"},
	})
	text := strings.Join(lines, "\n")
	if !strings.Contains(text, "Remote registration warnings: websocket url ignored: missing websocket_protocol capability; lease renew url ignored: missing lease_renew capability") || strings.Contains(text, "token=secret") {
		t.Fatalf("registration status = %q", text)
	}
}

func TestRunnerExecutesStatusShowSectionsWithoutQuery(t *testing.T) {
	client := &fakeClient{}
	t.Setenv("CLAUDE_CONFIG_DIR", t.TempDir())
	registry, err := tool.NewRegistry(
		tool.FuncTool{
			DefinitionValue: contracts.ToolDefinition{Name: "Write", InputSchema: contracts.JSONSchema{"type": "object"}},
			CallFunc: func(tool.Context, json.RawMessage, tool.ProgressSink) (contracts.ToolResult, error) {
				t.Fatal("status should not call tools")
				return contracts.ToolResult{}, nil
			},
		},
		tool.FuncTool{
			DefinitionValue: contracts.ToolDefinition{Name: "Read", InputSchema: contracts.JSONSchema{"type": "object"}},
			CallFunc: func(tool.Context, json.RawMessage, tool.ProgressSink) (contracts.ToolResult, error) {
				t.Fatal("status should not call tools")
				return contracts.ToolResult{}, nil
			},
		},
	)
	if err != nil {
		t.Fatal(err)
	}
	transcriptPath := filepath.Join(t.TempDir(), "session.jsonl")
	if err := telemetrypkg.Append(telemetrypkg.SessionPath(transcriptPath, "sess_status_show"), telemetrypkg.Event{
		SessionID: "sess_status_show",
		Type:      string(EventAssistantMessage),
		Model:     "sonnet",
	}); err != nil {
		t.Fatal(err)
	}
	if err := telemetrypkg.Append(telemetrypkg.SessionPath(transcriptPath, "sess_status_show"), telemetrypkg.Event{
		SessionID:     "sess_status_show",
		Type:          string(EventToolResult),
		ToolUseID:     "toolu_status",
		ToolResultErr: true,
		Error:         "tool failed",
	}); err != nil {
		t.Fatal(err)
	}
	if err := bridgepkg.WriteManifest(bridgepkg.SessionManifestPath(transcriptPath, "sess_status_show"), bridgepkg.Manifest{
		SessionID:        "sess_status_show",
		WorkingDirectory: "/tmp/project",
		Commands: []bridgepkg.Command{
			{Name: "compact", Type: contracts.CommandLocal},
			{Name: "ask", Type: contracts.CommandPrompt, Aliases: []string{"question"}},
		},
		Capabilities: bridgepkg.WithRemoteServiceCapability(bridgepkg.WithRemoteTriggerCapability(bridgepkg.WithWebSocketProtocolCapability(bridgepkg.Manifest{}))).Capabilities,
	}); err != nil {
		t.Fatal(err)
	}
	if err := daemonpkg.WriteState(daemonpkg.SessionStatePath(transcriptPath, "sess_status_show"), daemonpkg.State{
		SessionID:        "sess_status_show",
		WorkingDirectory: "/tmp/project",
		RuntimeState:     daemonpkg.RuntimeRunning,
		PID:              4242,
		Endpoint:         "http://127.0.0.1:7777",
		GeneratedAt:      "2026-06-17T10:00:00Z",
		StartedAt:        "2026-06-17T09:59:00Z",
		HeartbeatAt:      time.Now().UTC().Format(time.RFC3339Nano),
	}); err != nil {
		t.Fatal(err)
	}
	if err := remotepkg.WriteManifest(remotepkg.SessionManifestPath(transcriptPath, "sess_status_show"), remotepkg.Manifest{
		SessionID:     "sess_status_show",
		EnvironmentID: "env-status",
		GeneratedAt:   "2026-06-17T10:01:00Z",
		Services: []remotepkg.Service{
			{
				Name:          "bridge",
				RuntimeState:  bridgepkg.DirectRuntimeRunning,
				Endpoint:      "http://127.0.0.1:8888",
				WebSocketURL:  "ws://127.0.0.1:8888/ws",
				TokenRequired: true,
				Commands:      2,
				Capabilities:  []string{"websocket_protocol", "remote_trigger", "remote_service"},
			},
			{
				Name:         "daemon",
				RuntimeState: daemonpkg.RuntimeRunning,
				Endpoint:     "http://127.0.0.1:7777",
				PID:          4242,
				Capabilities: []string{"health", "status", "tick", "stop"},
			},
		},
	}); err != nil {
		t.Fatal(err)
	}
	if err := remotepkg.WriteRegistrationState(remotepkg.SessionRegistrationPath(transcriptPath, "sess_status_show"), remotepkg.RegistrationState{
		SessionID:       "sess_status_show",
		EnvironmentID:   "env-status",
		RuntimeState:    remotepkg.RegistrationRegistered,
		RegistrationURL: "https://remote.example/register",
		StatusCode:      http.StatusAccepted,
		RemoteSessionID: "remote-status",
		ProtocolVersion: "ccr.remote.v1",
		Capabilities:    []string{"websocket_protocol", "lease_renew"},
		WebSocketURL:    "wss://remote.example/ws?token=secret",
		PollURL:         "https://remote.example/poll?token=secret",
		LeaseRenewURL:   "https://remote.example/leases/renew?token=secret",
		RegisteredAt:    "2026-06-17T10:02:00Z",
	}); err != nil {
		t.Fatal(err)
	}
	if err := remotepkg.WritePumpState(remotepkg.SessionPumpPath(transcriptPath, "sess_status_show"), remotepkg.PumpState{
		SessionID:         "sess_status_show",
		RuntimeState:      remotepkg.PumpRunning,
		Transport:         "websocket",
		WebSocketURL:      "wss://remote.example/ws?token=secret",
		PollURL:           "https://remote.example/poll?token=secret",
		LastCursor:        "cursor-2",
		StreamStartedAt:   "2026-06-17T10:03:00Z",
		StreamEndedAt:     "2026-06-17T10:08:00Z",
		StreamStopReason:  "context_cancelled",
		StatusCode:        http.StatusOK,
		AttemptCount:      3,
		CloseCode:         1000,
		FrameCount:        2,
		ConnectCount:      1,
		ReconnectCount:    1,
		AckEventCount:     2,
		AckSentCount:      1,
		AckErrorCount:     1,
		LeaseEventCount:   1,
		LeaseExpiredCount: 1,
		LeaseRenewSent:    1,
		LeaseRenewErrors:  1,
		EventCount:        3,
		DeliveredCount:    2,
		DuplicateCount:    1,
		ErrorCount:        0,
	}); err != nil {
		t.Fatal(err)
	}
	if err := lsppkg.WriteSnapshot(lsppkg.SessionDiagnosticsPath(transcriptPath, "sess_status_show"), []lsppkg.Diagnostic{
		{FilePath: "main.go", Severity: "error", Source: "gopls", Message: "broken"},
		{FilePath: "main.go", Severity: "warning", Source: "gopls", Message: "unused"},
		{FilePath: "web.ts", Severity: "info", Source: "tsserver", Message: "info"},
	}); err != nil {
		t.Fatal(err)
	}
	if err := lsppkg.WriteManagerStatus(lsppkg.SessionManagerStatusPath(transcriptPath, "sess_status_show"), lsppkg.ManagerStatus{
		SessionID: "sess_status_show",
		Servers: []lsppkg.ServerStatus{
			{Name: "gopls", Command: "gopls", RuntimeState: lsppkg.ServerRuntimeNotStarted, MatchReasons: []string{"root:go.mod"}},
			{Name: "rust-analyzer", Command: "rust-analyzer", RuntimeState: lsppkg.ServerRuntimeNoWorkspaceMatch},
		},
	}); err != nil {
		t.Fatal(err)
	}
	if err := nativepkg.WriteManifest(nativepkg.SessionManifestPath(transcriptPath, "sess_status_show"), nativepkg.Manifest{
		SessionID: "sess_status_show",
		GOOS:      "testos",
		GOARCH:    "testarch",
		Terminal:  "xterm-256color",
		Capabilities: []nativepkg.Capability{
			{Name: "native_clipboard", Available: false},
			{Name: "osc52_clipboard", Available: true},
		},
	}); err != nil {
		t.Fatal(err)
	}
	if err := integrationspkg.WriteManifest(integrationspkg.SessionManifestPath(transcriptPath, "sess_status_show"), integrationspkg.Manifest{
		SessionID: "sess_status_show",
		Integrations: []integrationspkg.Integration{
			{Name: "chrome", Enabled: true, RuntimeState: integrationspkg.RuntimeStateNotWired},
			{Name: "voice", RuntimeState: integrationspkg.RuntimeStateDisabled},
		},
	}); err != nil {
		t.Fatal(err)
	}
	runner := Runner{
		Client:           client,
		Tools:            tool.NewExecutor(registry),
		Model:            "sonnet",
		APIKeySource:     "oauth",
		PermissionMode:   contracts.PermissionPlan,
		BetaHeaders:      []string{"beta-one"},
		FastMode:         true,
		MaxTokens:        128,
		SessionID:        "sess_status_show",
		SessionPath:      transcriptPath,
		WorkingDirectory: t.TempDir(),
		MCP: &MCPConfig{UserSettings: contracts.Settings{
			MCPServers: map[string]contracts.MCPServer{
				"zeta":  {Command: "node"},
				"alpha": {Command: "python"},
			},
			DeniedMCPServers: []contracts.MCPServerPolicyEntry{{ServerName: "zeta"}},
			EnabledPlugins:   map[string]any{"market/a": true, "market/b": false},
			PluginConfigs: map[string]contracts.PluginConfig{
				"market/a": {Options: map[string]any{"token": "plugin-secret"}},
			},
		}},
	}

	assertStatusShow := func(prompt string, wants []string, rejects []string) {
		t.Helper()
		result, err := runner.RunTurn(context.Background(), nil, messages.UserText(prompt))
		if err != nil {
			t.Fatal(err)
		}
		if len(client.requests) != 0 {
			t.Fatalf("model should not be queried, requests = %#v", client.requests)
		}
		text := result.Messages[1].Content[0].Text
		for _, want := range wants {
			if !strings.Contains(text, want) {
				t.Fatalf("%s missing %q: %q", prompt, want, text)
			}
		}
		for _, reject := range rejects {
			if strings.Contains(text, reject) {
				t.Fatalf("%s leaked %q: %q", prompt, reject, text)
			}
		}
	}

	assertStatusShow("/status show session", []string{
		"Status session",
		"Session ID: sess_status_show",
		"Transcript path: " + transcriptPath,
	}, nil)
	assertStatusShow("/status show tools", []string{
		"Status tools",
		"Tools: 2",
		"Tool names: Read, Write",
	}, nil)
	assertStatusShow("/status show mcp", []string{
		"Status MCP servers",
		"MCP servers: 2",
		"- alpha: configured (stdio, settings)",
		"- zeta: blocked:",
	}, nil)
	assertStatusShow("/status show plugins", []string{
		"Status plugins",
		"Enabled plugin entries: 2",
		"Enabled plugins: 1",
		"Plugin configs: 1",
		"- market/a: enabled",
		"- market/b: disabled",
	}, []string{"plugin-secret"})
	assertStatusShow("/status show telemetry", []string{
		"Status telemetry",
		"Enabled: disabled",
		"Events: 2",
		"Traces: 1",
		"Spans: 2",
		"Tool events: 1",
		"Tool errors: 1",
		"Error events: 1",
		"Event types:",
		"- assistant_message: 1",
		"- tool_result: 1",
		"Models:",
		"- sonnet: 1",
	}, []string{"tool failed"})
	assertStatusShow("/status show bridge", []string{
		"Status bridge",
		"Enabled: disabled",
		"Bridge-safe commands: 2",
		"Bridge capabilities: 3",
		"- websocket_protocol: http /ws: websocket hello",
		"- remote_trigger: http /remote-trigger: websocket remote_trigger",
		"- remote_service: http /remote-service: websocket remote_status",
		"Command names: ask, compact",
	}, nil)
	assertStatusShow("/status show remote", []string{
		"Status remote",
		"Enabled: disabled",
		"Remote environment: env-status",
		"Remote registration: registered: url https://remote.example/register: status 202: remote session remote-status: protocol ccr.remote.v1: capabilities websocket_protocol, lease_renew: websocket wss://remote.example/ws: poll https://remote.example/poll: lease renew https://remote.example/leases/renew",
		"Remote pump: running: transport websocket: websocket wss://remote.example/ws: poll https://remote.example/poll: cursor cursor-2: status 200: attempts 3: frames 2: connects 1: reconnects 1: ack events 2: ack sent 1: ack errors 1: lease events 1: lease expired 1: lease renew sent 1: lease renew errors 1: events 3: delivered 2: duplicates 1: errors 0",
		"close 1000",
		"stream started 2026-06-17T10:03:00Z: stream ended 2026-06-17T10:08:00Z: stream stop context_cancelled",
		"Remote services: 2",
		"- bridge: running: endpoint http://127.0.0.1:8888: websocket ws://127.0.0.1:8888/ws: token required: commands 2: capabilities websocket_protocol, remote_trigger, remote_service",
		"- daemon: running: endpoint http://127.0.0.1:7777: pid 4242: capabilities health, status, tick, stop",
	}, []string{"token=secret"})
	assertStatusShow("/status show daemon", []string{
		"Status daemon",
		"Daemon state: running",
		"Daemon pid: 4242",
		"Daemon endpoint: http://127.0.0.1:7777",
	}, nil)
	assertStatusShow("/status show lsp", []string{
		"Status LSP",
		"Enabled: disabled",
		"Diagnostics: 3",
		"Files: 2",
		"Errors: 1",
		"Warnings: 1",
		"Info: 1",
		"Severities:",
		"- error: 1",
		"- warning: 1",
		"Sources:",
		"- gopls: 2",
		"- tsserver: 1",
		"Configured LSP servers: 2",
		"Matched LSP servers: 1",
		"Server runtime states:",
		"- no_workspace_match: 1",
		"- not_started: 1",
	}, []string{"broken", "unused"})
	assertStatusShow("/status show native", []string{
		"Status native integrations",
		"Enabled: disabled",
		"Platform: testos/testarch",
		"Capabilities: 2",
		"Available capabilities: 1",
		"Clipboard adapters: 0",
		"Terminal: xterm-256color",
		"- native_clipboard: unavailable",
		"- osc52_clipboard: available",
	}, nil)
	assertStatusShow("/status show integrations", []string{
		"Status advanced integrations",
		"Enabled: disabled",
		"Integrations: 2",
		"Enabled integrations: 1",
		"Runtime states:",
		"- disabled: 1",
		"- not_wired: 1",
		"- chrome: enabled=enabled runtime=not_wired",
		"- voice: enabled=disabled runtime=disabled",
	}, nil)
	assertStatusShow("/status integrations", []string{
		"Status advanced integrations",
		"Integrations: 2",
		"- chrome: enabled=enabled runtime=not_wired",
	}, nil)
	assertStatusShow("/status diagnostics", []string{
		"Status LSP",
		"Diagnostics: 3",
		"Configured LSP servers: 2",
	}, []string{"broken", "unused"})
	assertStatusShow("/status show unknown", []string{
		"Unknown status section unknown.",
		"Available sections:",
		"telemetry",
		"daemon",
		"integrations",
	}, nil)
}

func TestRunnerExecutesConfigSlashCommandWithoutQuery(t *testing.T) {
	client := &fakeClient{}
	configHome := t.TempDir()
	t.Setenv("CLAUDE_CONFIG_DIR", configHome)
	cwd := t.TempDir()
	if err := os.WriteFile(filepath.Join(configHome, "settings.json"), []byte(`{"model":"sonnet"}`), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Join(cwd, ".claude"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(cwd, ".claude", "settings.json"), []byte(`{"env":{"PROJECT":"1"}}`), 0o644); err != nil {
		t.Fatal(err)
	}
	runner := Runner{
		Client:           client,
		Model:            "sonnet",
		APIKeySource:     "api_key",
		PermissionMode:   contracts.PermissionDefault,
		BetaHeaders:      []string{"beta-one"},
		SessionID:        "sess_config",
		WorkingDirectory: cwd,
		MCP: &MCPConfig{
			UserSettings: contracts.Settings{
				OutputStyle: "Explanatory",
				Env:         map[string]string{"USER_ENV": "1"},
				MCPServers: map[string]contracts.MCPServer{
					"alpha": {Command: "python"},
				},
			},
			ProjectSettings: contracts.Settings{
				Permissions: &contracts.PermissionsSetting{
					Allow: []string{"Read"},
					Deny:  []string{"Bash(rm *)"},
					Ask:   []string{"Edit"},
				},
			},
			LocalSettings: contracts.Settings{
				Hooks: map[string]any{"PreToolUse": []any{}},
			},
		},
	}
	result, err := runner.RunTurn(context.Background(), nil, messages.UserText("/config"))
	if err != nil {
		t.Fatal(err)
	}
	if len(client.requests) != 0 {
		t.Fatalf("model should not be queried, requests = %#v", client.requests)
	}
	if result.Assistant.Type != "" || result.FinalRequest.Model != "" {
		t.Fatalf("unexpected model result = %#v", result)
	}
	if len(result.Messages) != 2 {
		t.Fatalf("result messages = %#v", result.Messages)
	}
	text := result.Messages[1].Content[0].Text
	for _, want := range []string{
		"Config",
		"Working directory: " + cwd,
		"Model: sonnet",
		"- user: " + filepath.Join(configHome, "settings.json") + " (present)",
		"- project: " + filepath.Join(cwd, ".claude", "settings.json") + " (present)",
		"- env vars: 1",
		"- MCP servers: 1",
		"- output style: Explanatory",
		"- auth source: api_key",
		"- permission mode: default",
		"- fast mode: disabled",
		"- beta headers: 1",
		"- permission rules: allow 1, deny 1, ask 1",
		"- hooks: 1",
	} {
		if !strings.Contains(text, want) {
			t.Fatalf("config text missing %q: %q", want, text)
		}
	}
}

func TestRunnerRefreshesRemoteManagedPolicyAtTurnStart(t *testing.T) {
	client := &fakeClient{}
	root := t.TempDir()
	t.Setenv("CLAUDE_CONFIG_DIR", filepath.Join(root, "home"))
	t.Setenv("USER_TYPE", "ant")
	t.Setenv("CLAUDE_CODE_MANAGED_SETTINGS_PATH", filepath.Join(root, "missing-managed"))
	current := `{"settings":{"model":"remote-a"}}`
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("content-type", "application/json")
		_, _ = w.Write([]byte(current))
	}))
	defer server.Close()
	t.Setenv("CLAUDE_CODE_REMOTE_MANAGED_SETTINGS_URL", server.URL+"/policy")

	runner := Runner{
		Client:    client,
		SessionID: "sess_remote_policy_refresh",
		MCP:       &MCPConfig{},
	}
	result, err := runner.RunTurn(context.Background(), nil, messages.UserText("/config show model"))
	if err != nil {
		t.Fatal(err)
	}
	if len(client.requests) != 0 {
		t.Fatalf("model should not be queried, requests = %#v", client.requests)
	}
	text := result.Messages[1].Content[0].Text
	if !strings.Contains(text, "Configured model: remote-a") || runner.MCP.PolicySettings.Model != "remote-a" {
		t.Fatalf("text = %q policy=%#v", text, runner.MCP.PolicySettings)
	}

	current = `{"settings":{"model":"remote-b"}}`
	result, err = runner.RunTurn(context.Background(), nil, messages.UserText("/config show model"))
	if err != nil {
		t.Fatal(err)
	}
	text = result.Messages[1].Content[0].Text
	if !strings.Contains(text, "Configured model: remote-b") || runner.MCP.PolicySettings.Model != "remote-b" {
		t.Fatalf("text = %q policy=%#v", text, runner.MCP.PolicySettings)
	}
}

func TestRunnerRefreshesSettingsFilesAtTurnStart(t *testing.T) {
	client := &fakeClient{}
	root := t.TempDir()
	configHome := filepath.Join(root, "home")
	cwd := filepath.Join(root, "project")
	if err := os.MkdirAll(configHome, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(cwd, 0o755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("CLAUDE_CONFIG_DIR", configHome)
	t.Setenv("USER_TYPE", "ant")
	t.Setenv("CLAUDE_CODE_MANAGED_SETTINGS_PATH", filepath.Join(root, "missing-managed"))
	settingsPath := filepath.Join(configHome, "settings.json")
	if err := os.WriteFile(settingsPath, []byte(`{"model":"sonnet"}`), 0o644); err != nil {
		t.Fatal(err)
	}
	mcpConfig, err := LoadMCPConfigFromSettingsFiles(cwd)
	if err != nil {
		t.Fatal(err)
	}
	runner := Runner{
		Client:    client,
		SessionID: "sess_settings_refresh",
		MCP:       mcpConfig,
	}
	result, err := runner.RunTurn(context.Background(), nil, messages.UserText("/config show model"))
	if err != nil {
		t.Fatal(err)
	}
	text := result.Messages[1].Content[0].Text
	if !strings.Contains(text, "Configured model: sonnet") {
		t.Fatalf("text = %q", text)
	}

	if err := os.WriteFile(settingsPath, []byte(`{"model":"opus"}`), 0o644); err != nil {
		t.Fatal(err)
	}
	result, err = runner.RunTurn(context.Background(), nil, messages.UserText("/config show model"))
	if err != nil {
		t.Fatal(err)
	}
	text = result.Messages[1].Content[0].Text
	if !strings.Contains(text, "Configured model: opus") || runner.MCP.UserSettings.Model != "opus" {
		t.Fatalf("text = %q user settings=%#v", text, runner.MCP.UserSettings)
	}
	if len(client.requests) != 0 {
		t.Fatalf("model should not be queried, requests = %#v", client.requests)
	}
}

func TestRunnerExecutesConfigShowSectionsWithoutQuery(t *testing.T) {
	client := &fakeClient{}
	t.Setenv("CLAUDE_CONFIG_DIR", t.TempDir())
	cwd := t.TempDir()
	disableBypass := true
	bridgeEnabled := true
	lspDisabled := false
	telemetryEnabled := true
	runner := Runner{
		Client:           client,
		Model:            "sonnet",
		APIKeySource:     "oauth",
		PermissionMode:   contracts.PermissionPlan,
		BetaHeaders:      []string{"beta-one"},
		SessionID:        "sess_config_show",
		WorkingDirectory: cwd,
		MCP: &MCPConfig{UserSettings: contracts.Settings{
			Env: map[string]string{
				"PUBLIC_FLAG":  "1",
				"SECRET_TOKEN": "secret-value",
			},
			MCPServers: map[string]contracts.MCPServer{
				"alpha": {
					Command: "node",
					Args:    []string{"server.js"},
					Env:     map[string]string{"TOKEN": "hidden-token"},
				},
			},
			Permissions: &contracts.PermissionsSetting{
				Allow:                 []string{"Read"},
				Deny:                  []string{"Bash(rm *)"},
				Ask:                   []string{"Edit"},
				DefaultMode:           contracts.PermissionPlan,
				DisableBypassMode:     disableBypass,
				AdditionalDirectories: []string{cwd},
			},
			Hooks:          map[string]any{"PreToolUse": []any{}},
			EnabledPlugins: map[string]any{"market/a": true, "market/b": false},
			PluginConfigs: map[string]contracts.PluginConfig{
				"market/a": {Options: map[string]any{"token": "plugin-secret"}},
			},
			Sandbox: map[string]any{"allowUnsandboxedCommands": false},
			Advanced: &contracts.AdvancedSetting{
				Bridge:    &bridgeEnabled,
				LSP:       &lspDisabled,
				Telemetry: &telemetryEnabled,
			},
		}},
	}

	assertConfigShow := func(prompt string, wants []string, rejects []string) {
		t.Helper()
		result, err := runner.RunTurn(context.Background(), nil, messages.UserText(prompt))
		if err != nil {
			t.Fatal(err)
		}
		if len(client.requests) != 0 {
			t.Fatalf("model should not be queried, requests = %#v", client.requests)
		}
		text := result.Messages[1].Content[0].Text
		for _, want := range wants {
			if !strings.Contains(text, want) {
				t.Fatalf("%s missing %q: %q", prompt, want, text)
			}
		}
		for _, reject := range rejects {
			if strings.Contains(text, reject) {
				t.Fatalf("%s leaked %q: %q", prompt, reject, text)
			}
		}
	}

	assertConfigShow("/config show env", []string{
		"Config env",
		"Env vars: 2",
		"Env names: PUBLIC_FLAG, SECRET_TOKEN",
	}, []string{"secret-value"})
	assertConfigShow("/config env", []string{
		"Config env",
		"Env vars: 2",
		"Env names: PUBLIC_FLAG, SECRET_TOKEN",
	}, []string{"secret-value"})
	assertConfigShow("/config show permissions", []string{
		"Config permissions",
		"Default mode: plan",
		"Allow rules: 1",
		"- Read",
		"Deny rules: 1",
		"- Bash(rm *)",
		"Ask rules: 1",
		"- Edit",
		"Additional directories: 1",
		"Disable bypass mode: enabled",
	}, nil)
	assertConfigShow("/config permissions", []string{
		"Config permissions",
		"Default mode: plan",
		"Allow rules: 1",
		"- Read",
		"Deny rules: 1",
		"- Bash(rm *)",
		"Ask rules: 1",
		"- Edit",
		"Additional directories: 1",
		"Disable bypass mode: enabled",
	}, nil)
	assertConfigShow("/config show mcp", []string{
		"Config MCP servers",
		"MCP servers: 1",
		"- alpha (stdio, configured, settings)",
	}, []string{"hidden-token", "server.js"})
	assertConfigShow("/config show plugins", []string{
		"Config plugins",
		"Enabled plugin entries: 2",
		"Enabled plugins: 1",
		"Plugin configs: 1",
		"- market/a: enabled",
		"- market/b: disabled",
		"Plugin config names: market/a",
	}, []string{"plugin-secret"})
	assertConfigShow("/config show advanced", []string{
		"Config advanced integrations",
		"Bridge: enabled",
		"LSP: disabled",
		"Telemetry: enabled",
		"Chrome: (unset)",
		"Computer use: (unset)",
		"Native integrations: (unset)",
		"tengu_glacier_2xr: (unset)",
	}, nil)
	assertConfigShow("/config integrations", []string{
		"Config advanced integrations",
		"Bridge: enabled",
		"LSP: disabled",
		"Telemetry: enabled",
		"Chrome: (unset)",
		"Computer use: (unset)",
		"Native integrations: (unset)",
		"tengu_glacier_2xr: (unset)",
	}, nil)
	assertConfigShow("/config show schema", []string{
		"Config settings schema",
		"Schema ID: " + config.SettingsJSONSchemaID,
		"Draft: " + config.SettingsJSONSchemaDraft,
		"Settings properties:",
		"Additional properties: allowed",
		"Generated schema size:",
	}, nil)
	assertConfigShow("/config settings-schema", []string{
		"Config settings schema",
	}, nil)
	assertConfigShow("/config show unknown", []string{
		"Unknown config section unknown.",
		"Available sections:",
		"schema",
		"advanced",
	}, nil)

	for _, prompt := range []string{"/config all", "/config show all"} {
		assertConfigShow(prompt, []string{
			"Config all",
			"Config settings files",
			"Config model",
			"Config output style",
			"Config auth",
			"Config env",
			"Config permissions",
			"Config MCP servers",
			"Config plugins",
			"Plugin marketplaces",
			"Config settings schema",
			"Config advanced integrations",
		}, []string{"secret-value", "hidden-token", "plugin-secret"})
	}
}

func TestRunnerConfigSlashCommandReportsUsageForUnknownSubcommand(t *testing.T) {
	runner := Runner{Client: &fakeClient{}, SessionID: "sess_config_unknown"}
	result, err := runner.RunTurn(context.Background(), nil, messages.UserText("/config nope"))
	if err != nil {
		t.Fatal(err)
	}
	got := result.Messages[1].Content[0].Text
	for _, want := range []string{
		"Unknown config subcommand: nope",
		"Usage: /config [all|settings|model|output-style|auth|fast-mode|betas|env|permissions|mcp|hooks|plugins|marketplaces|sandbox|schema|advanced|show <section>|search <query>|output-style <name>|fast-mode <on|off>|model <name>|permission-mode <mode>]",
	} {
		if !strings.Contains(got, want) {
			t.Fatalf("unknown config result missing %q: %q", want, got)
		}
	}
	if strings.Contains(got, "not implemented") {
		t.Fatalf("unknown config should not report not implemented: %q", got)
	}
}

func TestRunnerConfigSearchFindsSettingsWithoutLeakingValues(t *testing.T) {
	client := &fakeClient{}
	t.Setenv("CLAUDE_CONFIG_DIR", t.TempDir())
	cwd := t.TempDir()
	telemetryEnabled := true
	runner := Runner{
		Client:           client,
		Model:            "sonnet",
		APIKeySource:     "api_key",
		PermissionMode:   contracts.PermissionPlan,
		BetaHeaders:      []string{"beta-token-name"},
		SessionID:        "sess_config_search",
		WorkingDirectory: cwd,
		MCP: &MCPConfig{UserSettings: contracts.Settings{
			Env: map[string]string{
				"PUBLIC_FLAG":  "1",
				"SECRET_TOKEN": "hidden-token",
			},
			MCPServers: map[string]contracts.MCPServer{
				"alpha": {
					Command: "node",
					Args:    []string{"--token=hidden-token"},
					Env:     map[string]string{"TOKEN": "hidden-token"},
					Headers: map[string]string{"X-Token": "secret-header"},
				},
			},
			Hooks:                  map[string]any{"PreToolUse": []any{}},
			HTTPHookAllowedEnvVars: []string{"HOOK_TOKEN"},
			EnabledPlugins:         map[string]any{"market/plugin": true},
			PluginConfigs: map[string]contracts.PluginConfig{
				"market/plugin": {
					Options: map[string]any{
						"apiKey": "secret-value",
						"region": "iad",
					},
					MCPServers: map[string]map[string]any{
						"docs": {"enabled": true},
					},
				},
			},
			Plugins: map[string]any{
				"market/plugin": map[string]any{"legacyToken": "legacy-secret"},
			},
			ExtraKnownMarketplaces: map[string]any{"internal-market": map[string]any{"url": "https://market.example"}},
			Sandbox:                map[string]any{"allowUnsandboxedCommands": false},
			Advanced:               &contracts.AdvancedSetting{Telemetry: &telemetryEnabled},
		}},
	}

	result, err := runner.RunTurn(context.Background(), nil, messages.UserText("/config search token"))
	if err != nil {
		t.Fatal(err)
	}
	if len(client.requests) != 0 {
		t.Fatalf("model should not be queried, requests = %#v", client.requests)
	}
	text := result.Messages[1].Content[0].Text
	for _, want := range []string{
		"Config search: token",
		"- betas: beta beta-token-name",
		"- env: env name SECRET_TOKEN",
		"- hooks: HTTP hook env name HOOK_TOKEN",
		"- mcp: alpha env name TOKEN",
		"- mcp: alpha header name X-Token",
		"- plugins: market/plugin legacy setting legacyToken",
	} {
		if !strings.Contains(text, want) {
			t.Fatalf("config search missing %q: %q", want, text)
		}
	}
	for _, leaked := range []string{"hidden-token", "secret-header", "secret-value", "legacy-secret"} {
		if strings.Contains(text, leaked) {
			t.Fatalf("config search leaked value %q: %q", leaked, text)
		}
	}

	result, err = runner.RunTurn(context.Background(), nil, messages.UserText("/config find apikey"))
	if err != nil {
		t.Fatal(err)
	}
	text = result.Messages[1].Content[0].Text
	if !strings.Contains(text, "- plugins: market/plugin option key apiKey") || strings.Contains(text, "secret-value") {
		t.Fatalf("config find option key text = %q", text)
	}

	result, err = runner.RunTurn(context.Background(), nil, messages.UserText("/config search telemetry"))
	if err != nil {
		t.Fatal(err)
	}
	text = result.Messages[1].Content[0].Text
	if !strings.Contains(text, "- advanced: telemetry enabled") {
		t.Fatalf("config search advanced text = %q", text)
	}

	result, err = runner.RunTurn(context.Background(), nil, messages.UserText("/config search hidden-token"))
	if err != nil {
		t.Fatal(err)
	}
	if got := result.Messages[1].Content[0].Text; got != "No config matched hidden-token." {
		t.Fatalf("config search should not match secret values = %q", got)
	}

	result, err = runner.RunTurn(context.Background(), nil, messages.UserText("/config search"))
	if err != nil {
		t.Fatal(err)
	}
	if got := result.Messages[1].Content[0].Text; got != "Usage: /config search <query>" {
		t.Fatalf("config search usage = %q", got)
	}
}

func TestRunnerExecutesConfigOutputStyleWithoutQuery(t *testing.T) {
	client := &fakeClient{}
	configHome := t.TempDir()
	t.Setenv("CLAUDE_CONFIG_DIR", configHome)
	runner := Runner{
		Client:    client,
		SessionID: "sess_config_style",
		MCP:       &MCPConfig{},
	}
	result, err := runner.RunTurn(context.Background(), nil, messages.UserText("/config output-style learning"))
	if err != nil {
		t.Fatal(err)
	}
	if len(client.requests) != 0 {
		t.Fatalf("model should not be queried, requests = %#v", client.requests)
	}
	if got := result.Messages[1].Content[0].Text; got != "Output style set to Learning." {
		t.Fatalf("output style text = %q", got)
	}
	if runner.MCP.UserSettings.OutputStyle != "Learning" {
		t.Fatalf("runner output style = %q", runner.MCP.UserSettings.OutputStyle)
	}
	var settings map[string]any
	data, err := os.ReadFile(filepath.Join(configHome, "settings.json"))
	if err != nil {
		t.Fatal(err)
	}
	if err := json.Unmarshal(data, &settings); err != nil {
		t.Fatal(err)
	}
	if settings["outputStyle"] != "Learning" {
		t.Fatalf("settings = %#v", settings)
	}
}

func TestRunnerExecutesConfigFastModeWithoutQuery(t *testing.T) {
	client := &fakeClient{}
	configHome := t.TempDir()
	t.Setenv("CLAUDE_CONFIG_DIR", configHome)
	runner := Runner{
		Client:    client,
		SessionID: "sess_config_fast",
		MCP:       &MCPConfig{},
	}
	result, err := runner.RunTurn(context.Background(), nil, messages.UserText("/config fast-mode on"))
	if err != nil {
		t.Fatal(err)
	}
	if len(client.requests) != 0 {
		t.Fatalf("model should not be queried, requests = %#v", client.requests)
	}
	if got := result.Messages[1].Content[0].Text; got != "Fast mode enabled." {
		t.Fatalf("fast mode text = %q", got)
	}
	if !runner.FastMode || runner.MCP.UserSettings.FastMode == nil || !*runner.MCP.UserSettings.FastMode {
		t.Fatalf("runner fast mode = %#v settings=%#v", runner.FastMode, runner.MCP.UserSettings.FastMode)
	}
	var settings map[string]any
	data, err := os.ReadFile(filepath.Join(configHome, "settings.json"))
	if err != nil {
		t.Fatal(err)
	}
	if err := json.Unmarshal(data, &settings); err != nil {
		t.Fatal(err)
	}
	if settings["fastMode"] != true {
		t.Fatalf("settings = %#v", settings)
	}

	result, err = runner.RunTurn(context.Background(), nil, messages.UserText("/config fast-mode off"))
	if err != nil {
		t.Fatal(err)
	}
	if got := result.Messages[1].Content[0].Text; got != "Fast mode disabled." {
		t.Fatalf("fast mode disable text = %q", got)
	}
	if runner.FastMode || runner.MCP.UserSettings.FastMode == nil || *runner.MCP.UserSettings.FastMode {
		t.Fatalf("runner fast mode after disable = %#v settings=%#v", runner.FastMode, runner.MCP.UserSettings.FastMode)
	}
}

func TestRunnerExecutesConfigModelWithoutQuery(t *testing.T) {
	client := &fakeClient{}
	configHome := t.TempDir()
	t.Setenv("CLAUDE_CONFIG_DIR", configHome)
	runner := Runner{
		Client:    client,
		Model:     "sonnet",
		SessionID: "sess_config_model",
		MCP:       &MCPConfig{},
	}
	result, err := runner.RunTurn(context.Background(), nil, messages.UserText("/config model opus"))
	if err != nil {
		t.Fatal(err)
	}
	if len(client.requests) != 0 {
		t.Fatalf("model should not be queried, requests = %#v", client.requests)
	}
	text := result.Messages[1].Content[0].Text
	if !strings.Contains(text, "Model set to claude-opus-4-6.") || !strings.Contains(text, "Display name: Opus 4.6") {
		t.Fatalf("model text = %q", text)
	}
	if runner.Model != "claude-opus-4-6" || runner.MCP.UserSettings.Model != "claude-opus-4-6" {
		t.Fatalf("runner model = %q settings=%q", runner.Model, runner.MCP.UserSettings.Model)
	}
	var settings map[string]any
	data, err := os.ReadFile(filepath.Join(configHome, "settings.json"))
	if err != nil {
		t.Fatal(err)
	}
	if err := json.Unmarshal(data, &settings); err != nil {
		t.Fatal(err)
	}
	if settings["model"] != "claude-opus-4-6" {
		t.Fatalf("settings = %#v", settings)
	}
}

func TestRunnerExecutesConfigPermissionModeWithoutQuery(t *testing.T) {
	client := &fakeClient{}
	configHome := t.TempDir()
	t.Setenv("CLAUDE_CONFIG_DIR", configHome)
	if err := os.MkdirAll(configHome, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(configHome, "settings.json"), []byte(`{
		"permissions": {
			"allow": ["Bash(git status:*)"]
		}
	}`), 0o644); err != nil {
		t.Fatal(err)
	}
	runner := Runner{
		Client:         client,
		PermissionMode: contracts.PermissionDefault,
		SessionID:      "sess_config_permission_mode",
		MCP:            &MCPConfig{},
	}
	result, err := runner.RunTurn(context.Background(), nil, messages.UserText("/config permission-mode dont-ask"))
	if err != nil {
		t.Fatal(err)
	}
	if len(client.requests) != 0 {
		t.Fatalf("model should not be queried, requests = %#v", client.requests)
	}
	if got := result.Messages[1].Content[0].Text; got != "Permission mode set to dontAsk." {
		t.Fatalf("permission mode text = %q", got)
	}
	if runner.PermissionMode != contracts.PermissionDontAsk || runner.MCP.UserSettings.Permissions == nil || runner.MCP.UserSettings.Permissions.DefaultMode != contracts.PermissionDontAsk {
		t.Fatalf("runner permission mode = %#v settings=%#v", runner.PermissionMode, runner.MCP.UserSettings.Permissions)
	}
	var settings map[string]any
	data, err := os.ReadFile(filepath.Join(configHome, "settings.json"))
	if err != nil {
		t.Fatal(err)
	}
	if err := json.Unmarshal(data, &settings); err != nil {
		t.Fatal(err)
	}
	permissions := settings["permissions"].(map[string]any)
	if permissions["defaultMode"] != "dontAsk" || len(permissions["allow"].([]any)) != 1 {
		t.Fatalf("settings = %#v", settings)
	}
}

func TestRunnerExecutesPluginSlashCommandWithoutQuery(t *testing.T) {
	client := &fakeClient{}
	repo := filepath.Join(t.TempDir(), "repo")
	cwd := filepath.Join(repo, "pkg")
	pluginDir := filepath.Join(repo, ".claude", "plugins", "demo")
	if err := os.MkdirAll(filepath.Join(repo, ".git"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(pluginDir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Join(pluginDir, "agents"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Join(pluginDir, "hooks"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Join(pluginDir, "skills", "audit"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(cwd, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(pluginDir, "agents", "review.md"), []byte("---\nname: reviewer\ndescription: Review changes\n---\nReview."), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(pluginDir, "hooks", "hooks.json"), []byte(`{
		"hooks": {
			"PreToolUse": [{"hooks": [{"type": "command", "command": "echo pre"}]}]
		}
	}`), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(pluginDir, "skills", "audit", "SKILL.md"), []byte("---\ndescription: Audit code\n---\nAudit."), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Join(pluginDir, "output-styles"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(pluginDir, "output-styles", "brief.md"), []byte("---\ndescription: Brief style\n---\nBrief."), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(pluginDir, "plugin.json"), []byte(`{
		"name": "demo",
		"version": "1.2.3",
		"commands": [{
			"name": "plugin:deploy",
			"description": "Deploy plugin",
			"prompt": "Deploy $ARGUMENTS."
		}],
		"mcpServers": {
			"plugin:docs": {"type": "http", "url": "https://docs.example/mcp"}
		}
	}`), 0o644); err != nil {
		t.Fatal(err)
	}
	runner := Runner{
		Client:           client,
		SessionID:        "sess_plugin",
		WorkingDirectory: cwd,
		MCP: &MCPConfig{UserSettings: contracts.Settings{
			EnabledPlugins:         map[string]any{"market/disabled": false, "market/plugin": true},
			PluginConfigs:          map[string]contracts.PluginConfig{"market/plugin": {Options: map[string]any{"flag": true}}},
			Plugins:                map[string]any{"legacy": map[string]any{}},
			ExtraKnownMarketplaces: map[string]any{"internal": map[string]any{}},
			StrictKnownMarketplaces: []any{
				"internal",
			},
			BlockedMarketplaces: []any{"blocked"},
		}},
	}
	result, err := runner.RunTurn(context.Background(), nil, messages.UserText("/plugin list"))
	if err != nil {
		t.Fatal(err)
	}
	if len(client.requests) != 0 {
		t.Fatalf("model should not be queried, requests = %#v", client.requests)
	}
	if result.Assistant.Type != "" || result.FinalRequest.Model != "" {
		t.Fatalf("unexpected model result = %#v", result)
	}
	if len(result.Messages) != 2 {
		t.Fatalf("result messages = %#v", result.Messages)
	}
	text := result.Messages[1].Content[0].Text
	for _, want := range []string{
		"Plugins",
		"Enabled plugins: 1",
		"Plugin configs: 1",
		"Plugin settings entries: 1",
		"Extra known marketplaces: 1",
		"Strict known marketplaces: 1",
		"Blocked marketplaces: 1",
		"Local plugin manifests: 1",
		"Registered plugin commands: 1",
		"Plugin skills: 1",
		"Plugin agents: 1",
		"Plugin MCP servers: 1",
		"Plugin output styles: 1",
		"Plugin hooks: 1",
		"Plugin enabled states:",
		"- market/disabled: disabled",
		"- market/plugin: enabled",
		"Local plugins:",
		"- demo@1.2.3",
		"Plugin commands:",
		"- /plugin:deploy",
		"Plugin skills:",
		"- /demo:audit",
		"Plugin agents:",
		"- demo:reviewer",
		"Plugin MCP servers:",
		"- plugin:docs",
		"Plugin output styles:",
		"- demo:brief",
		"Plugin hook events:",
		"- PreToolUse (1)",
	} {
		if !strings.Contains(text, want) {
			t.Fatalf("plugin text missing %q: %q", want, text)
		}
	}

	result, err = runner.RunTurn(context.Background(), nil, messages.UserText("/plugin manage"))
	if err != nil {
		t.Fatal(err)
	}
	if len(client.requests) != 0 {
		t.Fatalf("model should not be queried, requests = %#v", client.requests)
	}
	text = result.Messages[1].Content[0].Text
	for _, want := range []string{
		"Plugins",
		"Local plugin manifests: 1",
		"Plugin commands:",
		"- /plugin:deploy",
	} {
		if !strings.Contains(text, want) {
			t.Fatalf("plugin manage text missing %q: %q", want, text)
		}
	}

	for _, prompt := range []string{"/plugin help", "/plugin --help", "/plugin -h", "/plugins help", "/marketplace help"} {
		result, err = runner.RunTurn(context.Background(), nil, messages.UserText(prompt))
		if err != nil {
			t.Fatal(err)
		}
		if len(client.requests) != 0 {
			t.Fatalf("%s queried model, requests = %#v", prompt, client.requests)
		}
		text = result.Messages[1].Content[0].Text
		for _, want := range []string{
			"Plugin Command Usage:",
			"Installation:",
			"/plugin install <plugin>@<market> - Install plugin from marketplace",
			"Management:",
			"/plugin manage - Manage installed plugins",
			"Marketplaces:",
			"/plugin marketplace list - List all marketplaces",
			"Validation:",
			"/plugin validate <path> - Validate a manifest file or directory",
			"Other:",
			"/plugin help - Show this help",
		} {
			if !strings.Contains(text, want) {
				t.Fatalf("%s help missing %q: %q", prompt, want, text)
			}
		}
	}

	result, err = runner.RunTurn(context.Background(), nil, messages.UserText("/plugin unknown"))
	if err != nil {
		t.Fatal(err)
	}
	text = result.Messages[1].Content[0].Text
	for _, want := range []string{
		"Unknown plugin subcommand: unknown",
		"Plugin Command Usage:",
		"/plugin help - Show this help",
	} {
		if !strings.Contains(text, want) {
			t.Fatalf("plugin unknown missing %q: %q", want, text)
		}
	}
	if strings.Contains(text, "not implemented") {
		t.Fatalf("plugin unknown should not report not implemented: %q", text)
	}
}

func TestRunnerPluginShowReportsLocalPluginDetails(t *testing.T) {
	client := &fakeClient{}
	repo := filepath.Join(t.TempDir(), "repo")
	cwd := filepath.Join(repo, "pkg")
	pluginDir := filepath.Join(repo, ".claude", "plugins", "demo")
	if err := os.MkdirAll(filepath.Join(repo, ".git"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Join(pluginDir, "agents"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Join(pluginDir, "hooks"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Join(pluginDir, "skills", "audit"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Join(pluginDir, "output-styles"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(cwd, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(pluginDir, "agents", "review.md"), []byte("---\nname: reviewer\ndescription: Review changes\n---\nReview."), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(pluginDir, "hooks", "hooks.json"), []byte(`{"hooks":{"PreToolUse":[{"hooks":[{"type":"command","command":"echo pre"}]}]}}`), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(pluginDir, "skills", "audit", "SKILL.md"), []byte("---\ndescription: Audit code\n---\nAudit."), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(pluginDir, "output-styles", "brief.md"), []byte("---\ndescription: Brief style\n---\nBrief."), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(pluginDir, "plugin.json"), []byte(`{
		"name": "demo",
		"version": "1.2.3",
		"description": "Demo plugin",
		"commands": [{"name": "plugin:deploy", "description": "Deploy plugin", "prompt": "Deploy."}],
		"mcpServers": {"plugin:docs": {"type": "http", "url": "https://docs.example/mcp"}}
	}`), 0o644); err != nil {
		t.Fatal(err)
	}
	runner := Runner{
		Client:           client,
		SessionID:        "sess_plugin_show",
		WorkingDirectory: cwd,
		MCP:              &MCPConfig{},
	}

	result, err := runner.RunTurn(context.Background(), nil, messages.UserText("/plugin show demo"))
	if err != nil {
		t.Fatal(err)
	}
	if len(client.requests) != 0 {
		t.Fatalf("model should not be queried, requests = %#v", client.requests)
	}
	text := result.Messages[1].Content[0].Text
	for _, want := range []string{
		"Plugin demo",
		"State: enabled",
		"Path: " + pluginDir,
		"Version: 1.2.3",
		"Description: Demo plugin",
		"Commands: 1",
		"Skills: 1",
		"Agents: 1",
		"MCP servers: 1",
		"Output styles: 1",
		"Hooks: 1",
		"Commands:",
		"- /plugin:deploy",
		"Skills:",
		"- /demo:audit",
		"Agents:",
		"- demo:reviewer",
		"MCP servers:",
		"- plugin:docs",
		"Output styles:",
		"- demo:brief",
		"Hook events:",
		"- PreToolUse (1)",
	} {
		if !strings.Contains(text, want) {
			t.Fatalf("plugin show missing %q: %q", want, text)
		}
	}

	result, err = runner.RunTurn(context.Background(), nil, messages.UserText("/plugin demo"))
	if err != nil {
		t.Fatal(err)
	}
	if len(client.requests) != 0 {
		t.Fatalf("model should not be queried, requests = %#v", client.requests)
	}
	text = result.Messages[1].Content[0].Text
	for _, want := range []string{
		"Plugin demo",
		"State: enabled",
		"Path: " + pluginDir,
		"Version: 1.2.3",
		"Description: Demo plugin",
		"Commands: 1",
		"Skills: 1",
		"Agents: 1",
		"MCP servers: 1",
		"Output styles: 1",
		"Hooks: 1",
		"Commands:",
		"- /plugin:deploy",
		"Skills:",
		"- /demo:audit",
		"Agents:",
		"- demo:reviewer",
		"MCP servers:",
		"- plugin:docs",
		"Output styles:",
		"- demo:brief",
		"Hook events:",
		"- PreToolUse (1)",
	} {
		if !strings.Contains(text, want) {
			t.Fatalf("direct plugin show missing %q: %q", want, text)
		}
	}
}

func TestRunnerPluginListIncludesSettingsMarketplacePlugins(t *testing.T) {
	client := &fakeClient{}
	root := t.TempDir()
	cwd := filepath.Join(root, "project")
	pluginDir := filepath.Join(root, "marketplace-plugin")
	if err := os.MkdirAll(cwd, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(pluginDir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(pluginDir, "plugin.json"), []byte(`{
		"name": "market-demo",
		"commands": [{"name": "market:deploy", "description": "Deploy marketplace plugin", "prompt": "Deploy."}]
	}`), 0o644); err != nil {
		t.Fatal(err)
	}
	runner := Runner{
		Client:           client,
		SessionID:        "sess_plugin_settings_marketplace",
		WorkingDirectory: cwd,
		MCP: &MCPConfig{UserSettings: contracts.Settings{
			ExtraKnownMarketplaces: map[string]any{
				"team": map[string]any{"source": map[string]any{
					"source":  "settings",
					"name":    "team",
					"plugins": []any{pluginDir},
				}},
			},
			StrictKnownMarketplaces: []any{"team"},
		}},
	}

	result, err := runner.RunTurn(context.Background(), nil, messages.UserText("/plugin list"))
	if err != nil {
		t.Fatal(err)
	}
	if len(client.requests) != 0 {
		t.Fatalf("model should not be queried, requests = %#v", client.requests)
	}
	text := result.Messages[1].Content[0].Text
	for _, want := range []string{
		"Local plugin manifests: 1",
		"Registered plugin commands: 1",
		"Local plugins:",
		"- market-demo",
		"Plugin commands:",
		"- /market:deploy",
	} {
		if !strings.Contains(text, want) {
			t.Fatalf("plugin list missing %q: %q", want, text)
		}
	}
}

func TestRunnerPluginInstallCopiesSettingsMarketplacePlugin(t *testing.T) {
	client := &fakeClient{}
	configHome := t.TempDir()
	t.Setenv("CLAUDE_CONFIG_DIR", configHome)
	root := t.TempDir()
	cwd := filepath.Join(root, "project")
	pluginDir := filepath.Join(root, "marketplace-plugin")
	if err := os.MkdirAll(cwd, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Join(pluginDir, "assets"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(pluginDir, "assets", "README.md"), []byte("market asset"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(pluginDir, "plugin.json"), []byte(`{
		"name": "market demo",
		"commands": [{"name": "market:deploy", "description": "Deploy marketplace plugin", "prompt": "Deploy."}],
		"mcpServers": {"plugin:docs": {"type": "http", "url": "https://docs.example/mcp"}}
	}`), 0o644); err != nil {
		t.Fatal(err)
	}
	runner := Runner{
		Client:           client,
		SessionID:        "sess_plugin_install_marketplace",
		WorkingDirectory: cwd,
		MCP: &MCPConfig{UserSettings: contracts.Settings{
			ExtraKnownMarketplaces: map[string]any{
				"team": map[string]any{"source": map[string]any{
					"source":  "settings",
					"name":    "team",
					"plugins": []any{pluginDir},
				}},
			},
			StrictKnownMarketplaces: []any{"team"},
		}},
	}

	result, err := runner.RunTurn(context.Background(), nil, messages.UserText("/plugin i market demo"))
	if err != nil {
		t.Fatal(err)
	}
	if len(client.requests) != 0 {
		t.Fatalf("model should not be queried, requests = %#v", client.requests)
	}
	installedDir := filepath.Join(cwd, ".claude", "plugins", "market-demo")
	text := result.Messages[1].Content[0].Text
	for _, want := range []string{
		"Plugin installed",
		"Name: market demo",
		"Source: " + pluginDir,
		"Installed path: " + installedDir,
		"Marketplace: team",
		"Status: installed",
	} {
		if !strings.Contains(text, want) {
			t.Fatalf("plugin install missing %q: %q", want, text)
		}
	}
	if _, err := os.Stat(filepath.Join(installedDir, "plugin.json")); err != nil {
		t.Fatalf("installed plugin manifest: %v", err)
	}
	if data, err := os.ReadFile(filepath.Join(installedDir, "assets", "README.md")); err != nil || string(data) != "market asset" {
		t.Fatalf("installed plugin asset = %q err=%v", data, err)
	}
	if runner.MCP.CWD != cwd {
		t.Fatalf("runner MCP cwd = %q", runner.MCP.CWD)
	}
	server, ok := runner.MCP.PluginServers["plugin:docs"]
	if !ok || server.URL != "https://docs.example/mcp" || server.PluginSource != "market demo" {
		t.Fatalf("plugin MCP servers = %#v", runner.MCP.PluginServers)
	}

	result, err = runner.RunTurn(context.Background(), nil, messages.UserText("/plugin install market demo"))
	if err != nil {
		t.Fatal(err)
	}
	text = result.Messages[1].Content[0].Text
	if !strings.Contains(text, "Status: already installed") {
		t.Fatalf("plugin reinstall text = %q", text)
	}

	result, err = runner.RunTurn(context.Background(), nil, messages.UserText("/plugin install --scope user market demo"))
	if err != nil {
		t.Fatal(err)
	}
	userInstalledDir := filepath.Join(configHome, "plugins", "market-demo")
	text = result.Messages[1].Content[0].Text
	for _, want := range []string{
		"Plugin installed",
		"Installed path: " + userInstalledDir,
		"Status: installed",
	} {
		if !strings.Contains(text, want) {
			t.Fatalf("user scoped plugin install missing %q: %q", want, text)
		}
	}
	if data, err := os.ReadFile(filepath.Join(userInstalledDir, "assets", "README.md")); err != nil || string(data) != "market asset" {
		t.Fatalf("user scoped installed plugin asset = %q err=%v", data, err)
	}

	result, err = runner.RunTurn(context.Background(), nil, messages.UserText("/plugin list"))
	if err != nil {
		t.Fatal(err)
	}
	text = result.Messages[1].Content[0].Text
	for _, want := range []string{
		"Local plugin manifests: 1",
		"- market demo",
		"- /market:deploy",
		"- plugin:docs",
	} {
		if !strings.Contains(text, want) {
			t.Fatalf("plugin list after install missing %q: %q", want, text)
		}
	}
}

func TestRunnerPluginUninstallCommandWithoutQuery(t *testing.T) {
	client := &fakeClient{}
	configHome := t.TempDir()
	t.Setenv("CLAUDE_CONFIG_DIR", configHome)
	cwd := filepath.Join(t.TempDir(), "project")
	pluginsDir := filepath.Join(cwd, ".claude", "plugins")
	projectPlugin := filepath.Join(pluginsDir, "demo")
	removePlugin := filepath.Join(pluginsDir, "remove-demo")
	rmPlugin := filepath.Join(pluginsDir, "rm-demo")
	userPlugin := filepath.Join(configHome, "plugins", "user-demo")
	for path, name := range map[string]string{
		projectPlugin: "demo",
		removePlugin:  "remove-demo",
		rmPlugin:      "rm-demo",
		userPlugin:    "user-demo",
	} {
		if err := os.MkdirAll(path, 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(path, "plugin.json"), []byte(`{"name":"`+name+`"}`+"\n"), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	runner := Runner{
		Client:           client,
		SessionID:        "sess_plugin_uninstall",
		WorkingDirectory: cwd,
		MCP:              &MCPConfig{},
	}
	for _, item := range []struct {
		prompt string
		name   string
		path   string
		scope  string
	}{
		{prompt: "/plugin uninstall demo", name: "demo", path: projectPlugin, scope: "project"},
		{prompt: "/plugin remove remove-demo", name: "remove-demo", path: removePlugin, scope: "project"},
		{prompt: "/plugin rm rm-demo", name: "rm-demo", path: rmPlugin, scope: "project"},
		{prompt: "/plugin rm --scope user user-demo", name: "user-demo", path: userPlugin, scope: "user"},
	} {
		result, err := runner.RunTurn(context.Background(), nil, messages.UserText(item.prompt))
		if err != nil {
			t.Fatal(err)
		}
		if len(client.requests) != 0 {
			t.Fatalf("%s queried model, requests = %#v", item.prompt, client.requests)
		}
		text := result.Messages[1].Content[0].Text
		for _, want := range []string{
			"Plugin uninstalled",
			"Name: " + item.name,
			"Removed path: " + item.path,
			"Scope: " + item.scope,
			"Status: uninstalled",
		} {
			if !strings.Contains(text, want) {
				t.Fatalf("%s missing %q: %q", item.prompt, want, text)
			}
		}
		if _, err := os.Stat(item.path); !os.IsNotExist(err) {
			t.Fatalf("%s plugin still exists or unexpected stat err: %v", item.prompt, err)
		}
	}
}

func TestRunnerPluginValidateCommandWithoutQuery(t *testing.T) {
	client := &fakeClient{}
	cwd := t.TempDir()
	pluginDir := filepath.Join(cwd, "demo-plugin")
	if err := os.MkdirAll(filepath.Join(pluginDir, "commands"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(pluginDir, "commands", "deploy.md"), []byte("---\ndescription: Deploy app\n---\nDeploy."), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(pluginDir, "plugin.json"), []byte(`{
		"name": "demo-plugin",
		"version": "1.0.0",
		"description": "Demo plugin",
		"author": {"name": "Team"}
	}`), 0o644); err != nil {
		t.Fatal(err)
	}
	badPlugin := filepath.Join(cwd, "bad-plugin")
	if err := os.MkdirAll(badPlugin, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(badPlugin, "plugin.json"), []byte(`{"name":`), 0o644); err != nil {
		t.Fatal(err)
	}
	runner := Runner{
		Client:           client,
		SessionID:        "sess_plugin_validate",
		WorkingDirectory: cwd,
		MCP:              &MCPConfig{},
	}
	for _, item := range []struct {
		prompt string
		wants  []string
	}{
		{
			prompt: "/plugin validate demo-plugin",
			wants: []string{
				"Validating plugin manifest: " + filepath.Join(pluginDir, "plugin.json"),
				"Plugin: demo-plugin",
				"Commands: 1",
				"Validation passed",
			},
		},
		{
			prompt: "/plugin validate",
			wants: []string{
				"Usage: /plugin validate <path>",
				"Validate a plugin or marketplace manifest file or directory.",
				"claude plugin validate <path>",
			},
		},
		{
			prompt: "/plugin validate bad-plugin",
			wants: []string{
				"Validating plugin manifest: " + filepath.Join(badPlugin, "plugin.json"),
				"Found 1 error:",
				"json: Invalid JSON syntax",
				"Validation failed",
			},
		},
	} {
		result, err := runner.RunTurn(context.Background(), nil, messages.UserText(item.prompt))
		if err != nil {
			t.Fatal(err)
		}
		if len(client.requests) != 0 {
			t.Fatalf("%s queried model, requests = %#v", item.prompt, client.requests)
		}
		text := result.Messages[1].Content[0].Text
		for _, want := range item.wants {
			if !strings.Contains(text, want) {
				t.Fatalf("%s missing %q: %q", item.prompt, want, text)
			}
		}
	}
}

func TestRunnerPluginUpdateRefreshesInstalledMarketplacePlugin(t *testing.T) {
	client := &fakeClient{}
	configHome := t.TempDir()
	t.Setenv("CLAUDE_CONFIG_DIR", configHome)
	root := t.TempDir()
	cwd := filepath.Join(root, "project")
	pluginDir := filepath.Join(root, "marketplace-plugin")
	if err := os.MkdirAll(cwd, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Join(pluginDir, "assets"), 0o755); err != nil {
		t.Fatal(err)
	}
	writeMarketDemo := func(version string, asset string) {
		t.Helper()
		if err := os.WriteFile(filepath.Join(pluginDir, "assets", "README.md"), []byte(asset), 0o644); err != nil {
			t.Fatal(err)
		}
		manifest := `{
			"name": "market demo",
			"version": "` + version + `",
			"commands": [{"name": "market:deploy", "description": "Deploy marketplace plugin", "prompt": "Deploy."}]
		}`
		if err := os.WriteFile(filepath.Join(pluginDir, "plugin.json"), []byte(manifest), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	writeMarketDemo("1.0.0", "v1 asset")
	runner := Runner{
		Client:           client,
		SessionID:        "sess_plugin_update_marketplace",
		WorkingDirectory: cwd,
		MCP: &MCPConfig{UserSettings: contracts.Settings{
			ExtraKnownMarketplaces: map[string]any{
				"team": map[string]any{"source": map[string]any{
					"source":  "settings",
					"name":    "team",
					"plugins": []any{pluginDir},
				}},
			},
			StrictKnownMarketplaces: []any{"team"},
		}},
	}

	if _, err := runner.RunTurn(context.Background(), nil, messages.UserText("/plugin install market demo")); err != nil {
		t.Fatal(err)
	}
	installedDir := filepath.Join(cwd, ".claude", "plugins", "market-demo")
	writeMarketDemo("2.0.0", "v2 asset")
	result, err := runner.RunTurn(context.Background(), nil, messages.UserText("/plugin update --scope=local market demo"))
	if err != nil {
		t.Fatal(err)
	}
	if len(client.requests) != 0 {
		t.Fatalf("model should not be queried, requests = %#v", client.requests)
	}
	text := result.Messages[1].Content[0].Text
	for _, want := range []string{
		"Plugin update",
		"Marketplace plugins: 1",
		"Updated plugins: 1",
		"- market demo -> " + installedDir,
	} {
		if !strings.Contains(text, want) {
			t.Fatalf("plugin update missing %q: %q", want, text)
		}
	}
	installed, err := pluginpkg.LoadPluginDir(installedDir)
	if err != nil {
		t.Fatal(err)
	}
	if installed.Version != "2.0.0" {
		t.Fatalf("installed version = %q", installed.Version)
	}
	if data, err := os.ReadFile(filepath.Join(installedDir, "assets", "README.md")); err != nil || string(data) != "v2 asset" {
		t.Fatalf("updated plugin asset = %q err=%v", data, err)
	}

	if _, err := runner.RunTurn(context.Background(), nil, messages.UserText("/plugin install --scope user market demo")); err != nil {
		t.Fatal(err)
	}
	userInstalledDir := filepath.Join(configHome, "plugins", "market-demo")
	writeMarketDemo("3.0.0", "v3 asset")
	result, err = runner.RunTurn(context.Background(), nil, messages.UserText("/plugin update --scope user market demo"))
	if err != nil {
		t.Fatal(err)
	}
	text = result.Messages[1].Content[0].Text
	for _, want := range []string{
		"Plugin update",
		"Updated plugins: 1",
		"- market demo -> " + userInstalledDir,
	} {
		if !strings.Contains(text, want) {
			t.Fatalf("user scoped plugin update missing %q: %q", want, text)
		}
	}
	userInstalled, err := pluginpkg.LoadPluginDir(userInstalledDir)
	if err != nil {
		t.Fatal(err)
	}
	if userInstalled.Version != "3.0.0" {
		t.Fatalf("user installed version = %q", userInstalled.Version)
	}
	if data, err := os.ReadFile(filepath.Join(userInstalledDir, "assets", "README.md")); err != nil || string(data) != "v3 asset" {
		t.Fatalf("user updated plugin asset = %q err=%v", data, err)
	}
}

func TestRunnerPluginAvailableListsMarketplacePlugins(t *testing.T) {
	client := &fakeClient{}
	root := t.TempDir()
	cwd := filepath.Join(root, "project")
	marketDir := filepath.Join(root, "marketplace-plugin")
	lintDir := filepath.Join(root, "lint-plugin")
	installedDir := filepath.Join(cwd, ".claude", "plugins", "market-demo")
	for _, dir := range []string{cwd, marketDir, lintDir, installedDir} {
		if err := os.MkdirAll(dir, 0o755); err != nil {
			t.Fatal(err)
		}
	}
	if err := os.WriteFile(filepath.Join(marketDir, "plugin.json"), []byte(`{
		"name": "market demo",
		"version": "2.0.0",
		"description": "Deploy marketplace plugin",
		"commands": [{"name": "market:deploy", "description": "Deploy marketplace plugin", "prompt": "Deploy."}]
	}`), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(lintDir, "plugin.json"), []byte(`{
		"name": "lint tool",
		"version": "1.0.0",
		"description": "Static checks"
	}`), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(installedDir, "plugin.json"), []byte(`{
		"name": "market demo",
		"version": "1.0.0",
		"commands": [{"name": "market:deploy", "description": "Deploy installed plugin", "prompt": "Deploy."}]
	}`), 0o644); err != nil {
		t.Fatal(err)
	}
	runner := Runner{
		Client:           client,
		SessionID:        "sess_plugin_available_marketplace",
		WorkingDirectory: cwd,
		MCP: &MCPConfig{UserSettings: contracts.Settings{
			EnabledPlugins: map[string]any{"market demo": false},
			ExtraKnownMarketplaces: map[string]any{
				"team": map[string]any{"source": map[string]any{
					"source":  "settings",
					"name":    "team",
					"plugins": []any{marketDir, lintDir},
				}},
			},
			StrictKnownMarketplaces: []any{"team"},
		}},
	}

	result, err := runner.RunTurn(context.Background(), nil, messages.UserText("/plugin available"))
	if err != nil {
		t.Fatal(err)
	}
	if len(client.requests) != 0 {
		t.Fatalf("model should not be queried, requests = %#v", client.requests)
	}
	text := result.Messages[1].Content[0].Text
	for _, want := range []string{
		"Marketplace plugins",
		"Marketplace plugins: 2",
		"Matches: 2",
		"- lint tool@1.0.0 [team] (available): Static checks",
		"- market demo@2.0.0 [team] (update available: installed 1.0.0 at " + installedDir + "): Deploy marketplace plugin",
	} {
		if !strings.Contains(text, want) {
			t.Fatalf("plugin available missing %q: %q", want, text)
		}
	}

	result, err = runner.RunTurn(context.Background(), nil, messages.UserText("/plugin marketplace show market demo"))
	if err != nil {
		t.Fatal(err)
	}
	text = result.Messages[1].Content[0].Text
	for _, want := range []string{
		"Marketplace plugin market demo",
		"State: update available: installed 1.0.0 at " + installedDir,
		"Marketplace: team",
		"Version: 2.0.0",
		"Commands: 1",
		"- /market:deploy",
	} {
		if !strings.Contains(text, want) {
			t.Fatalf("plugin marketplace show missing %q: %q", want, text)
		}
	}

	result, err = runner.RunTurn(context.Background(), nil, messages.UserText("/plugin marketplace search lint"))
	if err != nil {
		t.Fatal(err)
	}
	text = result.Messages[1].Content[0].Text
	if !strings.Contains(text, "Query: lint") || !strings.Contains(text, "- lint tool@1.0.0 [team] (available)") || strings.Contains(text, "market demo@2.0.0") {
		t.Fatalf("plugin marketplace search text = %q", text)
	}
}

func TestRunnerPluginShowReportsDisabledLocalPlugin(t *testing.T) {
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
	if err := os.WriteFile(filepath.Join(pluginDir, "plugin.json"), []byte(`{"name":"demo","commands":[{"name":"demo:deploy","prompt":"Deploy."}]}`), 0o644); err != nil {
		t.Fatal(err)
	}
	runner := Runner{
		Client:           &fakeClient{},
		SessionID:        "sess_plugin_show_disabled",
		WorkingDirectory: cwd,
		MCP: &MCPConfig{UserSettings: contracts.Settings{
			EnabledPlugins: map[string]any{"demo": false},
		}},
	}

	result, err := runner.RunTurn(context.Background(), nil, messages.UserText("/plugin show demo"))
	if err != nil {
		t.Fatal(err)
	}
	text := result.Messages[1].Content[0].Text
	if !strings.Contains(text, "Plugin demo") || !strings.Contains(text, "State: disabled") || !strings.Contains(text, "- /demo:deploy") {
		t.Fatalf("plugin show disabled text = %q", text)
	}
}

func TestRunnerPluginShowReportsMarketplaceBlockedLocalPlugin(t *testing.T) {
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
	if err := os.WriteFile(filepath.Join(pluginDir, "plugin.json"), []byte(`{"name":"demo","marketplace":"internal","commands":[{"name":"demo:deploy","prompt":"Deploy."}]}`), 0o644); err != nil {
		t.Fatal(err)
	}
	runner := Runner{
		Client:           &fakeClient{},
		SessionID:        "sess_plugin_show_marketplace_blocked",
		WorkingDirectory: cwd,
		MCP: &MCPConfig{UserSettings: contracts.Settings{
			StrictKnownMarketplaces: []any{"enterprise"},
		}},
	}

	result, err := runner.RunTurn(context.Background(), nil, messages.UserText("/plugin show demo"))
	if err != nil {
		t.Fatal(err)
	}
	text := result.Messages[1].Content[0].Text
	for _, want := range []string{
		"Plugin demo",
		"State: blocked: not listed in settings strictKnownMarketplaces",
		"Marketplace: internal",
		"- /demo:deploy",
	} {
		if !strings.Contains(text, want) {
			t.Fatalf("plugin show blocked missing %q: %q", want, text)
		}
	}
}

func TestRunnerPluginSearchFindsLocalPluginMetadata(t *testing.T) {
	client := &fakeClient{}
	repo := filepath.Join(t.TempDir(), "repo")
	cwd := filepath.Join(repo, "pkg")
	demoDir := filepath.Join(repo, ".claude", "plugins", "demo")
	disabledDir := filepath.Join(repo, ".claude", "plugins", "disabled")
	if err := os.MkdirAll(filepath.Join(repo, ".git"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Join(demoDir, "agents"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(disabledDir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(cwd, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(demoDir, "agents", "review.md"), []byte("---\nname: reviewer\ndescription: Review changes\n---\nReview."), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(demoDir, "plugin.json"), []byte(`{
		"name": "demo",
		"version": "1.2.3",
		"description": "Demo release plugin",
		"commands": [{"name": "plugin:deploy", "description": "Deploy plugin", "prompt": "Deploy."}]
	}`), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(disabledDir, "plugin.json"), []byte(`{
		"name": "disabled",
		"description": "Review disabled workflows"
	}`), 0o644); err != nil {
		t.Fatal(err)
	}
	runner := Runner{
		Client:           client,
		SessionID:        "sess_plugin_search",
		WorkingDirectory: cwd,
		MCP: &MCPConfig{UserSettings: contracts.Settings{
			EnabledPlugins: map[string]any{"disabled": false},
		}},
	}
	result, err := runner.RunTurn(context.Background(), nil, messages.UserText("/plugin search review"))
	if err != nil {
		t.Fatal(err)
	}
	if len(client.requests) != 0 {
		t.Fatalf("model should not be queried, requests = %#v", client.requests)
	}
	text := result.Messages[1].Content[0].Text
	for _, want := range []string{
		"Plugin search: review",
		"Matches: 2",
		"- demo@1.2.3 (enabled): agent demo:reviewer",
		"- disabled (disabled): plugin metadata",
	} {
		if !strings.Contains(text, want) {
			t.Fatalf("plugin search missing %q: %q", want, text)
		}
	}

	result, err = runner.RunTurn(context.Background(), nil, messages.UserText("/plugin search"))
	if err != nil {
		t.Fatal(err)
	}
	if got := result.Messages[1].Content[0].Text; got != "Usage: /plugin search <query>" {
		t.Fatalf("plugin search usage = %q", got)
	}

	result, err = runner.RunTurn(context.Background(), nil, messages.UserText("/plugin search nowhere"))
	if err != nil {
		t.Fatal(err)
	}
	if got := result.Messages[1].Content[0].Text; got != "No plugins matched nowhere." {
		t.Fatalf("plugin search missing = %q", got)
	}
}

func TestRunnerPluginReportsMarketplacesAndConfigDetails(t *testing.T) {
	runner := Runner{
		Client:    &fakeClient{},
		SessionID: "sess_plugin_marketplace",
		MCP: &MCPConfig{UserSettings: contracts.Settings{
			EnabledPlugins: map[string]any{"market/plugin": true},
			PluginConfigs: map[string]contracts.PluginConfig{
				"market/plugin": {
					Options: map[string]any{
						"apiKey": "secret-value",
						"region": "iad",
					},
					MCPServers: map[string]map[string]any{
						"docs": {"enabled": true},
					},
				},
			},
			Plugins: map[string]any{
				"market/plugin": map[string]any{"legacyToken": "legacy-secret"},
				"legacy/only":   map[string]any{"legacyFlag": true},
			},
			ExtraKnownMarketplaces: map[string]any{
				"internal": map[string]any{"url": "https://market.example"},
			},
			StrictKnownMarketplaces: []any{
				"official",
				map[string]any{"name": "enterprise"},
			},
			BlockedMarketplaces: []any{"blocked"},
		}},
	}

	result, err := runner.RunTurn(context.Background(), nil, messages.UserText("/plugin marketplaces"))
	if err != nil {
		t.Fatal(err)
	}
	text := result.Messages[1].Content[0].Text
	for _, want := range []string{
		"Plugin marketplaces",
		"Extra known marketplaces: 1",
		"Strict known marketplaces: 2",
		"Blocked marketplaces: 1",
		"Marketplace policy: strict allowlist active",
		"Extra known marketplaces:",
		"- internal (blocked: not listed in settings strictKnownMarketplaces)",
		"Strict known marketplaces:",
		"- enterprise (allowed)",
		"- official (allowed)",
		"Blocked marketplaces:",
		"- blocked (blocked: blocked by settings blockedMarketplaces)",
	} {
		if !strings.Contains(text, want) {
			t.Fatalf("plugin marketplaces missing %q: %q", want, text)
		}
	}

	result, err = runner.RunTurn(context.Background(), nil, messages.UserText("/plugin config market/plugin"))
	if err != nil {
		t.Fatal(err)
	}
	text = result.Messages[1].Content[0].Text
	for _, want := range []string{
		"Plugin config market/plugin",
		"State: enabled",
		"Option keys: 2",
		"Options: apiKey, region",
		"MCP server configs: 1",
		"MCP server config names: docs",
		"Legacy settings keys: 1",
		"Legacy settings: legacyToken",
	} {
		if !strings.Contains(text, want) {
			t.Fatalf("plugin config missing %q: %q", want, text)
		}
	}
	for _, leaked := range []string{"secret-value", "legacy-secret"} {
		if strings.Contains(text, leaked) {
			t.Fatalf("plugin config leaked value %q: %q", leaked, text)
		}
	}

	result, err = runner.RunTurn(context.Background(), nil, messages.UserText("/plugin config missing"))
	if err != nil {
		t.Fatal(err)
	}
	if got := result.Messages[1].Content[0].Text; got != "Plugin config missing was not found." {
		t.Fatalf("plugin missing config = %q", got)
	}
}

func TestRunnerPluginMarketplaceAddRemoveUpdateCommands(t *testing.T) {
	configHome := t.TempDir()
	t.Setenv("CLAUDE_CONFIG_DIR", configHome)
	if err := os.MkdirAll(configHome, 0o755); err != nil {
		t.Fatal(err)
	}
	project := t.TempDir()
	marketDir := filepath.Join(t.TempDir(), "team-market")
	if err := os.MkdirAll(marketDir, 0o755); err != nil {
		t.Fatal(err)
	}
	runner := Runner{
		Client:           &fakeClient{},
		SessionID:        "sess_plugin_marketplace_write",
		WorkingDirectory: project,
		MCP:              &MCPConfig{CWD: project},
	}

	result, err := runner.RunTurn(context.Background(), nil, messages.UserText("/plugin marketplace add --type directory --install-location project team "+marketDir))
	if err != nil {
		t.Fatal(err)
	}
	if got := result.Messages[1].Content[0].Text; got != "Marketplace team added." {
		t.Fatalf("marketplace add text = %q", got)
	}
	document, err := readUserSettingsDocument()
	if err != nil {
		t.Fatal(err)
	}
	entry := marketplaceDocumentEntry(t, document, "team")
	source, _ := entry["source"].(map[string]any)
	if source["source"] != "directory" || source["path"] != marketDir || entry["installLocation"] != "project" {
		t.Fatalf("user marketplace entry = %#v", entry)
	}
	if _, ok := runner.MCP.UserSettings.ExtraKnownMarketplaces["team"]; !ok {
		t.Fatalf("runner user marketplaces = %#v", runner.MCP.UserSettings.ExtraKnownMarketplaces)
	}

	result, err = runner.RunTurn(context.Background(), result.Messages, messages.UserText("/plugin marketplace update team"))
	if err != nil {
		t.Fatal(err)
	}
	text := result.Messages[len(result.Messages)-1].Content[0].Text
	for _, want := range []string{
		"Updating marketplace: team...",
		"Successfully updated marketplace: team",
	} {
		if !strings.Contains(text, want) {
			t.Fatalf("marketplace update missing %q: %q", want, text)
		}
	}

	result, err = runner.RunTurn(context.Background(), result.Messages, messages.UserText("/plugin marketplace remove team"))
	if err != nil {
		t.Fatal(err)
	}
	if got := result.Messages[len(result.Messages)-1].Content[0].Text; got != "Marketplace team removed." {
		t.Fatalf("marketplace remove text = %q", got)
	}
	document, err = readUserSettingsDocument()
	if err != nil {
		t.Fatal(err)
	}
	if _, ok := document["extraKnownMarketplaces"]; ok {
		t.Fatalf("user extraKnownMarketplaces should be removed: %#v", document)
	}
	if len(runner.MCP.UserSettings.ExtraKnownMarketplaces) != 0 {
		t.Fatalf("runner user marketplaces should be empty: %#v", runner.MCP.UserSettings.ExtraKnownMarketplaces)
	}

	result, err = runner.RunTurn(context.Background(), result.Messages, messages.UserText("/plugin marketplace add --scope local local-tools npm:@example/tools"))
	if err != nil {
		t.Fatal(err)
	}
	if got := result.Messages[len(result.Messages)-1].Content[0].Text; got != "Marketplace local-tools added." {
		t.Fatalf("marketplace local add text = %q", got)
	}
	localDocument, err := config.ReadSettingsDocument(config.LocalSettingsPath(project))
	if err != nil {
		t.Fatal(err)
	}
	entry = marketplaceDocumentEntry(t, localDocument, "local-tools")
	source, _ = entry["source"].(map[string]any)
	if source["source"] != "npm" || source["package"] != "@example/tools" {
		t.Fatalf("local marketplace entry = %#v", entry)
	}

	result, err = runner.RunTurn(context.Background(), result.Messages, messages.UserText("/plugin marketplace remove --scope local local-tools"))
	if err != nil {
		t.Fatal(err)
	}
	if got := result.Messages[len(result.Messages)-1].Content[0].Text; got != "Marketplace local-tools removed." {
		t.Fatalf("marketplace local remove text = %q", got)
	}
	localDocument, err = config.ReadSettingsDocument(config.LocalSettingsPath(project))
	if err != nil {
		t.Fatal(err)
	}
	if _, ok := localDocument["extraKnownMarketplaces"]; ok {
		t.Fatalf("local extraKnownMarketplaces should be removed: %#v", localDocument)
	}

	result, err = runner.RunTurn(context.Background(), result.Messages, messages.UserText("/plugin marketplace add --type directory --sparse plugins/demo bad-sparse "+marketDir))
	if err != nil {
		t.Fatal(err)
	}
	if got := result.Messages[len(result.Messages)-1].Content[0].Text; !strings.Contains(got, "--sparse is only supported for github and git marketplace sources") {
		t.Fatalf("marketplace bad sparse text = %q", got)
	}
}

func TestRunnerExecutesPluginEnableDisableWithoutQuery(t *testing.T) {
	client := &fakeClient{}
	configHome := t.TempDir()
	t.Setenv("CLAUDE_CONFIG_DIR", configHome)
	runner := Runner{
		Client:    client,
		SessionID: "sess_plugin_toggle",
		MCP:       &MCPConfig{},
	}

	result, err := runner.RunTurn(context.Background(), nil, messages.UserText("/plugin enable market/plugin"))
	if err != nil {
		t.Fatal(err)
	}
	if len(client.requests) != 0 {
		t.Fatalf("model should not be queried, requests = %#v", client.requests)
	}
	if got := result.Messages[1].Content[0].Text; got != "Plugin market/plugin enabled." {
		t.Fatalf("enable text = %q", got)
	}
	if got := runner.MCP.UserSettings.EnabledPlugins["market/plugin"]; got != true {
		t.Fatalf("enabled state = %#v", runner.MCP.UserSettings.EnabledPlugins)
	}
	var settings map[string]any
	data, err := os.ReadFile(filepath.Join(configHome, "settings.json"))
	if err != nil {
		t.Fatal(err)
	}
	if err := json.Unmarshal(data, &settings); err != nil {
		t.Fatal(err)
	}
	enabledPlugins := settings["enabledPlugins"].(map[string]any)
	if enabledPlugins["market/plugin"] != true {
		t.Fatalf("settings after enable = %#v", settings)
	}

	result, err = runner.RunTurn(context.Background(), nil, messages.UserText("/plugin disable market/plugin"))
	if err != nil {
		t.Fatal(err)
	}
	if len(client.requests) != 0 {
		t.Fatalf("model should not be queried, requests = %#v", client.requests)
	}
	if got := result.Messages[1].Content[0].Text; got != "Plugin market/plugin disabled." {
		t.Fatalf("disable text = %q", got)
	}
	if got := runner.MCP.UserSettings.EnabledPlugins["market/plugin"]; got != false {
		t.Fatalf("disabled state = %#v", runner.MCP.UserSettings.EnabledPlugins)
	}
	data, err = os.ReadFile(filepath.Join(configHome, "settings.json"))
	if err != nil {
		t.Fatal(err)
	}
	if err := json.Unmarshal(data, &settings); err != nil {
		t.Fatal(err)
	}
	enabledPlugins = settings["enabledPlugins"].(map[string]any)
	if enabledPlugins["market/plugin"] != false {
		t.Fatalf("settings after disable = %#v", settings)
	}
}

func TestRunnerPluginSummarySkipsDisabledLocalPlugin(t *testing.T) {
	client := &fakeClient{}
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
		"commands": [{"name": "demo:deploy", "prompt": "Deploy."}]
	}`), 0o644); err != nil {
		t.Fatal(err)
	}
	runner := Runner{
		Client:           client,
		SessionID:        "sess_plugin_disabled",
		WorkingDirectory: cwd,
		MCP: &MCPConfig{UserSettings: contracts.Settings{
			EnabledPlugins: map[string]any{"demo": false},
		}},
	}
	result, err := runner.RunTurn(context.Background(), nil, messages.UserText("/plugin list"))
	if err != nil {
		t.Fatal(err)
	}
	text := result.Messages[1].Content[0].Text
	for _, want := range []string{
		"Enabled plugins: 0",
		"Local plugin manifests: 0",
		"Registered plugin commands: 0",
		"- demo: disabled",
	} {
		if !strings.Contains(text, want) {
			t.Fatalf("plugin disabled text missing %q: %q", want, text)
		}
	}
	if strings.Contains(text, "/demo:deploy") {
		t.Fatalf("disabled plugin command should not be listed: %q", text)
	}
}

func TestRunnerExecutesMemorySlashCommandWithoutQuery(t *testing.T) {
	client := &fakeClient{}
	sessionRoot := t.TempDir()
	relevantRoot := t.TempDir()
	if _, err := memory.WriteSessionSummary(memory.SessionSummaryOptions{
		Root:      sessionRoot,
		SessionID: "sess_old",
		Summary:   "remember deployment flow",
	}); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(relevantRoot, "db.md"), []byte("database rules\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(relevantRoot, "notes.txt"), []byte("not memory\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	runner := Runner{
		Client:                    client,
		SessionID:                 "sess_memory",
		SessionMemoryRoot:         sessionRoot,
		RelevantMemoryDir:         relevantRoot,
		EnableSessionMemoryRecall: true,
		EnableMemoryExtraction:    true,
	}
	result, err := runner.RunTurn(context.Background(), nil, messages.UserText("/memory status"))
	if err != nil {
		t.Fatal(err)
	}
	if len(client.requests) != 0 {
		t.Fatalf("model should not be queried, requests = %#v", client.requests)
	}
	if result.Assistant.Type != "" || result.FinalRequest.Model != "" {
		t.Fatalf("unexpected model result = %#v", result)
	}
	if len(result.Messages) != 2 {
		t.Fatalf("result messages = %#v", result.Messages)
	}
	text := result.Messages[1].Content[0].Text
	for _, want := range []string{
		"Memory",
		"Session memory root: " + sessionRoot,
		"Session summaries: 1",
		"Relevant memory directory: " + relevantRoot,
		"Relevant memory files: 1",
		"Session memory recall: enabled",
		"Turn-end memory extraction: enabled",
	} {
		if !strings.Contains(text, want) {
			t.Fatalf("memory text missing %q: %q", want, text)
		}
	}
}

func TestRunnerMemorySlashCommandReportsUsageForUnknownSubcommand(t *testing.T) {
	runner := Runner{Client: &fakeClient{}, SessionID: "sess_memory_unknown"}
	result, err := runner.RunTurn(context.Background(), nil, messages.UserText("/memory unknown"))
	if err != nil {
		t.Fatal(err)
	}
	got := result.Messages[1].Content[0].Text
	for _, want := range []string{
		"Unknown memory subcommand: unknown",
		"Usage: /memory [status|list|show [file]|search <query>|save <relative.md> <content>|remove <relative.md>]",
	} {
		if !strings.Contains(got, want) {
			t.Fatalf("memory unknown result missing %q: %q", want, got)
		}
	}
	if strings.Contains(got, "not implemented") {
		t.Fatalf("memory unknown should not report not implemented: %q", got)
	}

	result, err = runner.RunTurn(context.Background(), nil, messages.UserText("/memory help"))
	if err != nil {
		t.Fatal(err)
	}
	if got := result.Messages[1].Content[0].Text; got != "Usage: /memory [status|list|show [file]|search <query>|save <relative.md> <content>|remove <relative.md>]" {
		t.Fatalf("memory help result = %q", got)
	}
}

func TestRunnerMemoryShowListsMemoryFiles(t *testing.T) {
	client := &fakeClient{}
	sessionRoot := t.TempDir()
	relevantRoot := t.TempDir()
	if _, err := memory.WriteSessionSummary(memory.SessionSummaryOptions{
		Root:      sessionRoot,
		SessionID: "sess_show",
		Summary:   "remember deployment flow",
	}); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Join(relevantRoot, "team"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(relevantRoot, "team", "db.md"), []byte("---\ntitle: DB\n---\n# Database rules\nKeep migrations small.\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	runner := Runner{
		Client:            client,
		SessionID:         "sess_memory_show",
		SessionMemoryRoot: sessionRoot,
		RelevantMemoryDir: relevantRoot,
	}
	result, err := runner.RunTurn(context.Background(), nil, messages.UserText("/memory show"))
	if err != nil {
		t.Fatal(err)
	}
	if len(client.requests) != 0 {
		t.Fatalf("model should not be queried, requests = %#v", client.requests)
	}
	text := result.Messages[1].Content[0].Text
	for _, want := range []string{
		"Memory files",
		"Session memory root: " + sessionRoot,
		"sess_show/" + memory.SessionSummaryFilename + ": remember deployment flow",
		"Relevant memory directory: " + relevantRoot,
		"team/db.md: Database rules",
	} {
		if !strings.Contains(text, want) {
			t.Fatalf("memory show missing %q: %q", want, text)
		}
	}

	for _, prompt := range []string{"/memory list", "/memory files", "/memory ls"} {
		result, err = runner.RunTurn(context.Background(), nil, messages.UserText(prompt))
		if err != nil {
			t.Fatal(err)
		}
		if len(client.requests) != 0 {
			t.Fatalf("%s should not query model, requests = %#v", prompt, client.requests)
		}
		text = result.Messages[1].Content[0].Text
		for _, want := range []string{
			"Memory files",
			"Session memory root: " + sessionRoot,
			"sess_show/" + memory.SessionSummaryFilename + ": remember deployment flow",
			"Relevant memory directory: " + relevantRoot,
			"team/db.md: Database rules",
		} {
			if !strings.Contains(text, want) {
				t.Fatalf("%s missing %q: %q", prompt, want, text)
			}
		}
	}
}

func TestRunnerMemorySearchFindsMemoryFiles(t *testing.T) {
	client := &fakeClient{}
	sessionRoot := t.TempDir()
	relevantRoot := t.TempDir()
	if _, err := memory.WriteSessionSummary(memory.SessionSummaryOptions{
		Root:      sessionRoot,
		SessionID: "sess_search",
		Summary:   "remember deployment search flow",
	}); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Join(relevantRoot, "team"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(relevantRoot, "team", "db.md"), []byte("---\ntitle: DB\n---\n# Database rules\nKeep migrations small.\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	runner := Runner{
		Client:            client,
		SessionID:         "sess_memory_search",
		SessionMemoryRoot: sessionRoot,
		RelevantMemoryDir: relevantRoot,
	}
	result, err := runner.RunTurn(context.Background(), nil, messages.UserText("/memory search migrations"))
	if err != nil {
		t.Fatal(err)
	}
	if len(client.requests) != 0 {
		t.Fatalf("model should not be queried, requests = %#v", client.requests)
	}
	text := result.Messages[1].Content[0].Text
	for _, want := range []string{
		"Memory search: migrations",
		"Matches: 1",
		"Relevant memory directory/team/db.md: Keep migrations small.",
	} {
		if !strings.Contains(text, want) {
			t.Fatalf("memory search missing %q: %q", want, text)
		}
	}
	if strings.Contains(text, "title: DB") {
		t.Fatalf("frontmatter leaked into memory search: %q", text)
	}

	result, err = runner.RunTurn(context.Background(), nil, messages.UserText("/memory find deployment"))
	if err != nil {
		t.Fatal(err)
	}
	if text := result.Messages[1].Content[0].Text; !strings.Contains(text, "Session memory root/sess_search/"+memory.SessionSummaryFilename+": remember deployment search flow") {
		t.Fatalf("memory find session summary = %q", text)
	}

	result, err = runner.RunTurn(context.Background(), nil, messages.UserText("/memory search"))
	if err != nil {
		t.Fatal(err)
	}
	if got := result.Messages[1].Content[0].Text; got != "Usage: /memory search <query>" {
		t.Fatalf("memory search usage = %q", got)
	}

	result, err = runner.RunTurn(context.Background(), nil, messages.UserText("/memory search nowhere"))
	if err != nil {
		t.Fatal(err)
	}
	if got := result.Messages[1].Content[0].Text; got != "No memory files matched nowhere." {
		t.Fatalf("memory search missing = %q", got)
	}
}

func TestRunnerMemoryShowDisplaysSingleMemoryFile(t *testing.T) {
	relevantRoot := t.TempDir()
	if err := os.MkdirAll(filepath.Join(relevantRoot, "team"), 0o755); err != nil {
		t.Fatal(err)
	}
	memoryPath := filepath.Join(relevantRoot, "team", "db.md")
	if err := os.WriteFile(memoryPath, []byte("---\ntitle: DB\n---\n# Database rules\nKeep migrations small.\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	outside := filepath.Join(filepath.Dir(relevantRoot), "outside.md")
	if err := os.WriteFile(outside, []byte("outside memory\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	runner := Runner{
		Client:            &fakeClient{},
		SessionID:         "sess_memory_file_show",
		RelevantMemoryDir: relevantRoot,
	}
	result, err := runner.RunTurn(context.Background(), nil, messages.UserText("/memory show team/db.md"))
	if err != nil {
		t.Fatal(err)
	}
	text := result.Messages[1].Content[0].Text
	for _, want := range []string{
		"Memory file team/db.md",
		"Root: Relevant memory directory",
		"Path: team/db.md",
		"Absolute path: " + memoryPath,
		"Truncated: false",
		"Content:",
		"# Database rules\nKeep migrations small.",
	} {
		if !strings.Contains(text, want) {
			t.Fatalf("memory file show missing %q: %q", want, text)
		}
	}
	if strings.Contains(text, "title: DB") {
		t.Fatalf("frontmatter leaked into memory file body: %q", text)
	}

	result, err = runner.RunTurn(context.Background(), nil, messages.UserText("/memory show ../outside.md"))
	if err != nil {
		t.Fatal(err)
	}
	if got := result.Messages[1].Content[0].Text; got != "Memory file ../outside.md was not found." {
		t.Fatalf("outside memory result = %q", got)
	}
}

func TestRunnerMemoryWritesAndRemovesRelevantMemoryFile(t *testing.T) {
	client := &fakeClient{}
	relevantRoot := t.TempDir()
	runner := Runner{
		Client:            client,
		SessionID:         "sess_memory_write",
		RelevantMemoryDir: relevantRoot,
	}
	result, err := runner.RunTurn(context.Background(), nil, messages.UserText("/memory save team/runbook.md remember blue deploys"))
	if err != nil {
		t.Fatal(err)
	}
	if len(client.requests) != 0 {
		t.Fatalf("model should not be queried, requests = %#v", client.requests)
	}
	memoryPath := filepath.Join(relevantRoot, "team", "runbook.md")
	text := result.Messages[1].Content[0].Text
	for _, want := range []string{
		"Memory file saved",
		"Root: Relevant memory directory",
		"Path: team/runbook.md",
		"Absolute path: " + memoryPath,
	} {
		if !strings.Contains(text, want) {
			t.Fatalf("memory save missing %q: %q", want, text)
		}
	}
	data, err := os.ReadFile(memoryPath)
	if err != nil {
		t.Fatal(err)
	}
	if got := string(data); got != "remember blue deploys\n" {
		t.Fatalf("memory file content = %q", got)
	}

	result, err = runner.RunTurn(context.Background(), nil, messages.UserText("/memory show team/runbook.md"))
	if err != nil {
		t.Fatal(err)
	}
	if text := result.Messages[1].Content[0].Text; !strings.Contains(text, "remember blue deploys") {
		t.Fatalf("memory show written file = %q", text)
	}

	result, err = runner.RunTurn(context.Background(), nil, messages.UserText("/memory save ../outside.md nope"))
	if err != nil {
		t.Fatal(err)
	}
	if got := result.Messages[1].Content[0].Text; got != "Memory file could not be saved: path must stay inside the relevant memory directory" {
		t.Fatalf("outside save result = %q", got)
	}
	outside := filepath.Join(filepath.Dir(relevantRoot), "outside.md")
	if _, err := os.Stat(outside); !os.IsNotExist(err) {
		t.Fatalf("outside file should not exist, stat err = %v", err)
	}

	result, err = runner.RunTurn(context.Background(), nil, messages.UserText("/memory save team/runbook.txt nope"))
	if err != nil {
		t.Fatal(err)
	}
	if got := result.Messages[1].Content[0].Text; got != "Memory file could not be saved: path must use .md extension" {
		t.Fatalf("non-markdown save result = %q", got)
	}

	result, err = runner.RunTurn(context.Background(), nil, messages.UserText("/memory rm team/runbook.md"))
	if err != nil {
		t.Fatal(err)
	}
	text = result.Messages[1].Content[0].Text
	for _, want := range []string{
		"Memory file removed",
		"Path: team/runbook.md",
		"Absolute path: " + memoryPath,
	} {
		if !strings.Contains(text, want) {
			t.Fatalf("memory remove missing %q: %q", want, text)
		}
	}
	if _, err := os.Stat(memoryPath); !os.IsNotExist(err) {
		t.Fatalf("memory file should be removed, stat err = %v", err)
	}

	result, err = runner.RunTurn(context.Background(), nil, messages.UserText("/memory delete team/runbook.md"))
	if err != nil {
		t.Fatal(err)
	}
	if got := result.Messages[1].Content[0].Text; got != "Memory file team/runbook.md was not found." {
		t.Fatalf("missing remove result = %q", got)
	}

	outsideTarget := filepath.Join(filepath.Dir(relevantRoot), "outside-target.md")
	if err := os.WriteFile(outsideTarget, []byte("outside target\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(outsideTarget, memoryPath); err == nil {
		result, err = runner.RunTurn(context.Background(), nil, messages.UserText("/memory save team/runbook.md nope"))
		if err != nil {
			t.Fatal(err)
		}
		if got := result.Messages[1].Content[0].Text; got != "Memory file could not be saved: target path is a symlink" {
			t.Fatalf("symlink save result = %q", got)
		}

		result, err = runner.RunTurn(context.Background(), nil, messages.UserText("/memory rm team/runbook.md"))
		if err != nil {
			t.Fatal(err)
		}
		if got := result.Messages[1].Content[0].Text; got != "Memory file could not be removed: target path is a symlink" {
			t.Fatalf("symlink remove result = %q", got)
		}
		if err := os.Remove(memoryPath); err != nil {
			t.Fatal(err)
		}
	} else {
		t.Logf("skipping symlink target assertions: %v", err)
	}

	noConfigRunner := Runner{Client: &fakeClient{}, SessionID: "sess_memory_write_no_config"}
	result, err = noConfigRunner.RunTurn(context.Background(), nil, messages.UserText("/memory save team/runbook.md remember"))
	if err != nil {
		t.Fatal(err)
	}
	if got := result.Messages[1].Content[0].Text; got != "Memory file could not be saved: relevant memory directory is not configured" {
		t.Fatalf("no config save result = %q", got)
	}
}

func TestRunnerExecutesModelSlashCommandWithoutQuery(t *testing.T) {
	runner := Runner{
		Client:    &fakeClient{},
		Model:     "sonnet",
		SessionID: "sess_model",
	}
	result, err := runner.RunTurn(context.Background(), []contracts.Message{messages.UserText("old")}, messages.UserText("/model opus"))
	if err != nil {
		t.Fatal(err)
	}
	if result.Assistant.Type != "" || result.FinalRequest.Model != "" {
		t.Fatalf("unexpected model result = %#v", result)
	}
	if len(result.Messages) != 2 {
		t.Fatalf("result messages = %#v", result.Messages)
	}
	text := result.Messages[1].Content[0].Text
	if !strings.Contains(text, "Selected model: claude-opus-4-6") || !strings.Contains(text, "Display name: Opus 4.6") {
		t.Fatalf("model text = %q", text)
	}
	if runner.Model != "claude-opus-4-6" {
		t.Fatalf("runner model = %q", runner.Model)
	}
}

func TestRunnerModelSlashCommandReportsCurrentModel(t *testing.T) {
	client := &fakeClient{}
	runner := Runner{
		Client:    client,
		Model:     "claude-sonnet-4-6",
		SessionID: "sess_model_current",
	}
	result, err := runner.RunTurn(context.Background(), nil, messages.UserText("/model"))
	if err != nil {
		t.Fatal(err)
	}
	if len(result.Messages) != 2 {
		t.Fatalf("result messages = %#v", result.Messages)
	}
	if got := result.Messages[1].Content[0].Text; got != "Current model: claude-sonnet-4-6" {
		t.Fatalf("model text = %q", got)
	}
	if runner.Model != "claude-sonnet-4-6" {
		t.Fatalf("runner model changed to %q", runner.Model)
	}

	for _, prompt := range []string{"/model show", "/model info", "/model current"} {
		result, err = runner.RunTurn(context.Background(), nil, messages.UserText(prompt))
		if err != nil {
			t.Fatal(err)
		}
		if len(client.requests) != 0 {
			t.Fatalf("model should not be queried, requests = %#v", client.requests)
		}
		if got := result.Messages[1].Content[0].Text; got != "Current model: claude-sonnet-4-6" {
			t.Fatalf("%s text = %q", prompt, got)
		}
		if runner.Model != "claude-sonnet-4-6" {
			t.Fatalf("%s changed runner model to %q", prompt, runner.Model)
		}
	}
}

func TestRunnerModelSlashCommandListsModelsWithoutSelecting(t *testing.T) {
	client := &fakeClient{}
	runner := Runner{
		Client:    client,
		Model:     "sonnet",
		SessionID: "sess_model_list",
	}
	result, err := runner.RunTurn(context.Background(), nil, messages.UserText("/model list"))
	if err != nil {
		t.Fatal(err)
	}
	if len(client.requests) != 0 {
		t.Fatalf("model should not be queried, requests = %#v", client.requests)
	}
	text := result.Messages[1].Content[0].Text
	for _, want := range []string{
		"Available models",
		"Current model: sonnet",
		"Models: 11",
		"Aliases: ",
		"- Opus 4.6: claude-opus-4-6",
		"Alias names: ",
		"sonnet4.6",
	} {
		if !strings.Contains(text, want) {
			t.Fatalf("model list missing %q: %q", want, text)
		}
	}
	if runner.Model != "sonnet" {
		t.Fatalf("runner model changed to %q", runner.Model)
	}
}

func TestRunnerModelSlashCommandSearchesModelsWithoutSelecting(t *testing.T) {
	client := &fakeClient{}
	runner := Runner{
		Client:    client,
		Model:     "claude-opus-4-6",
		SessionID: "sess_model_search",
	}
	result, err := runner.RunTurn(context.Background(), nil, messages.UserText("/model search opus4.6"))
	if err != nil {
		t.Fatal(err)
	}
	if len(client.requests) != 0 {
		t.Fatalf("model should not be queried, requests = %#v", client.requests)
	}
	text := result.Messages[1].Content[0].Text
	for _, want := range []string{
		"Model search: opus4.6",
		"Matches: 1",
		"Current model: claude-opus-4-6",
		"- Opus 4.6: claude-opus-4-6",
		"aliases: best, opus, opus4.6",
		"current",
	} {
		if !strings.Contains(text, want) {
			t.Fatalf("model search missing %q: %q", want, text)
		}
	}
	if runner.Model != "claude-opus-4-6" {
		t.Fatalf("runner model changed to %q", runner.Model)
	}

	result, err = runner.RunTurn(context.Background(), nil, messages.UserText("/model find haiku"))
	if err != nil {
		t.Fatal(err)
	}
	text = result.Messages[1].Content[0].Text
	if !strings.Contains(text, "Model search: haiku") || !strings.Contains(text, "Haiku 4.5") || !strings.Contains(text, "Haiku 3.5") {
		t.Fatalf("model find haiku text = %q", text)
	}

	result, err = runner.RunTurn(context.Background(), nil, messages.UserText("/model search"))
	if err != nil {
		t.Fatal(err)
	}
	if got := result.Messages[1].Content[0].Text; got != "Usage: /model search <query>" {
		t.Fatalf("model search usage = %q", got)
	}

	result, err = runner.RunTurn(context.Background(), nil, messages.UserText("/model search nowhere"))
	if err != nil {
		t.Fatal(err)
	}
	if got := result.Messages[1].Content[0].Text; got != "No models matched nowhere." {
		t.Fatalf("model search missing = %q", got)
	}
}

func TestRunnerExecutesMCPSlashCommandWithoutQuery(t *testing.T) {
	runner := Runner{
		Client:    &fakeClient{},
		SessionID: "sess_mcp",
		MCP: &MCPConfig{UserSettings: contracts.Settings{
			MCPServers: map[string]contracts.MCPServer{
				"zeta":  {URL: "https://example.com/mcp"},
				"alpha": {Command: "python", Args: []string{"server.py"}},
			},
		}, PluginServers: map[string]contracts.MCPServer{
			"plugin-docs": {Type: "http", URL: "https://plugin.example/mcp", PluginSource: "demo"},
		}},
	}
	result, err := runner.RunTurn(context.Background(), []contracts.Message{messages.UserText("old")}, messages.UserText("/mcp list"))
	if err != nil {
		t.Fatal(err)
	}
	if result.Assistant.Type != "" || result.FinalRequest.Model != "" {
		t.Fatalf("unexpected model result = %#v", result)
	}
	if len(result.Messages) != 2 {
		t.Fatalf("result messages = %#v", result.Messages)
	}
	text := result.Messages[1].Content[0].Text
	for _, want := range []string{
		"MCP servers:",
		"- alpha (stdio): python server.py",
		"- plugin-docs (http): https://plugin.example/mcp",
		"- zeta (http): https://example.com/mcp",
	} {
		if !strings.Contains(text, want) {
			t.Fatalf("mcp text missing %q: %q", want, text)
		}
	}
}

func TestRunnerMCPSlashCommandShowsServerDetails(t *testing.T) {
	runner := Runner{
		Client:    &fakeClient{},
		SessionID: "sess_mcp_show",
		MCP: &MCPConfig{UserSettings: contracts.Settings{
			MCPServers: map[string]contracts.MCPServer{
				"alpha": {
					Command: "python",
					Args:    []string{"server.py"},
					Env: map[string]string{
						"API_TOKEN": "secret-token",
						"HOST":      "localhost",
					},
				},
			},
		}},
	}
	result, err := runner.RunTurn(context.Background(), nil, messages.UserText("/mcp show alpha"))
	if err != nil {
		t.Fatal(err)
	}
	text := result.Messages[1].Content[0].Text
	for _, want := range []string{
		"MCP server alpha",
		"Status: configured",
		"Policy: allowlist-unset",
		"Transport: stdio",
		"Target: python server.py",
		"Source: settings",
		"Command: python",
		"Args: server.py",
		"Env vars: 2",
		"Env names: API_TOKEN, HOST",
	} {
		if !strings.Contains(text, want) {
			t.Fatalf("mcp show missing %q: %q", want, text)
		}
	}
	if strings.Contains(text, "secret-token") {
		t.Fatalf("mcp show leaked env value: %q", text)
	}

	result, err = runner.RunTurn(context.Background(), nil, messages.UserText("/mcp alpha"))
	if err != nil {
		t.Fatal(err)
	}
	text = result.Messages[1].Content[0].Text
	for _, want := range []string{
		"MCP server alpha",
		"Status: configured",
		"Transport: stdio",
		"Target: python server.py",
		"Source: settings",
		"Env names: API_TOKEN, HOST",
	} {
		if !strings.Contains(text, want) {
			t.Fatalf("direct mcp show missing %q: %q", want, text)
		}
	}
	if strings.Contains(text, "secret-token") {
		t.Fatalf("direct mcp show leaked env value: %q", text)
	}
}

func TestRunnerMCPSlashCommandSearchesServers(t *testing.T) {
	runner := Runner{
		Client:    &fakeClient{},
		SessionID: "sess_mcp_search",
		MCP: &MCPConfig{UserSettings: contracts.Settings{
			MCPServers: map[string]contracts.MCPServer{
				"alpha": {
					Command: "python",
					Args:    []string{"server.py"},
					Env:     map[string]string{"API_TOKEN": "secret-token"},
				},
				"zeta": {URL: "https://example.com/mcp"},
			},
			DeniedMCPServers: []contracts.MCPServerPolicyEntry{{ServerName: "zeta"}},
		}, PluginServers: map[string]contracts.MCPServer{
			"plugin-docs": {
				Type:         "http",
				URL:          "https://plugin.example/mcp",
				Headers:      map[string]string{"Authorization": "Bearer secret", "X-Trace": "1"},
				PluginSource: "demo",
			},
		}},
	}
	result, err := runner.RunTurn(context.Background(), nil, messages.UserText("/mcp search API_TOKEN"))
	if err != nil {
		t.Fatal(err)
	}
	text := result.Messages[1].Content[0].Text
	for _, want := range []string{
		"MCP search: API_TOKEN",
		"Matches: 1",
		"- alpha (stdio, configured, settings): env API_TOKEN",
	} {
		if !strings.Contains(text, want) {
			t.Fatalf("mcp search missing %q: %q", want, text)
		}
	}
	if strings.Contains(text, "secret-token") {
		t.Fatalf("mcp search leaked env value: %q", text)
	}

	result, err = runner.RunTurn(context.Background(), nil, messages.UserText("/mcp search Authorization"))
	if err != nil {
		t.Fatal(err)
	}
	text = result.Messages[1].Content[0].Text
	for _, want := range []string{
		"MCP search: Authorization",
		"- plugin-docs (http, configured, plugin): header Authorization",
	} {
		if !strings.Contains(text, want) {
			t.Fatalf("mcp header search missing %q: %q", want, text)
		}
	}
	if strings.Contains(text, "Bearer secret") {
		t.Fatalf("mcp search leaked header value: %q", text)
	}

	result, err = runner.RunTurn(context.Background(), nil, messages.UserText("/mcp search denied"))
	if err != nil {
		t.Fatal(err)
	}
	if text := result.Messages[1].Content[0].Text; !strings.Contains(text, "- zeta (http, blocked: denied, settings): policy denied") {
		t.Fatalf("mcp denied search = %q", text)
	}

	result, err = runner.RunTurn(context.Background(), nil, messages.UserText("/mcp search"))
	if err != nil {
		t.Fatal(err)
	}
	if got := result.Messages[1].Content[0].Text; got != "Usage: /mcp search <query>" {
		t.Fatalf("mcp search usage = %q", got)
	}

	result, err = runner.RunTurn(context.Background(), nil, messages.UserText("/mcp search nowhere"))
	if err != nil {
		t.Fatal(err)
	}
	if got := result.Messages[1].Content[0].Text; got != "No MCP servers matched nowhere." {
		t.Fatalf("mcp search missing = %q", got)
	}
}

func TestRunnerMCPSlashCommandShowsPluginServerDetails(t *testing.T) {
	callbackPort := 3999
	runner := Runner{
		Client:    &fakeClient{},
		SessionID: "sess_mcp_plugin_show",
		MCP: &MCPConfig{PluginServers: map[string]contracts.MCPServer{
			"plugin-docs": {
				Type:          "http",
				URL:           "https://plugin.example/mcp",
				Headers:       map[string]string{"Authorization": "Bearer secret", "X-Trace": "1"},
				HeadersHelper: "headers-helper",
				AuthToken:     "static-secret",
				OAuth: &contracts.MCPOAuthConfig{
					ClientID:              "client-id",
					CallbackPort:          &callbackPort,
					AuthServerMetadataURL: "https://plugin.example/.well-known/oauth",
				},
				PluginSource: "demo",
			},
		}},
	}
	result, err := runner.RunTurn(context.Background(), nil, messages.UserText("/mcp info plugin-docs"))
	if err != nil {
		t.Fatal(err)
	}
	text := result.Messages[1].Content[0].Text
	for _, want := range []string{
		"MCP server plugin-docs",
		"Status: configured",
		"Transport: http",
		"Target: https://plugin.example/mcp",
		"Source: plugin",
		"Plugin source: demo",
		"Headers: 2",
		"Header names: Authorization, X-Trace",
		"Headers helper: configured",
		"Auth token: configured",
		"OAuth: configured",
		"OAuth client ID: client-id",
		"OAuth callback port: 3999",
		"OAuth metadata URL: https://plugin.example/.well-known/oauth",
	} {
		if !strings.Contains(text, want) {
			t.Fatalf("mcp plugin show missing %q: %q", want, text)
		}
	}
	for _, leaked := range []string{"Bearer secret", "static-secret"} {
		if strings.Contains(text, leaked) {
			t.Fatalf("mcp show leaked secret %q: %q", leaked, text)
		}
	}
}

func TestRunnerMCPSlashCommandMarksPolicyBlockedServers(t *testing.T) {
	runner := Runner{
		Client:    &fakeClient{},
		SessionID: "sess_mcp_blocked",
		MCP: &MCPConfig{UserSettings: contracts.Settings{
			MCPServers: map[string]contracts.MCPServer{
				"alpha": {Command: "python", Args: []string{"server.py"}},
			},
			DeniedMCPServers: []contracts.MCPServerPolicyEntry{{ServerName: "alpha"}},
		}},
	}
	result, err := runner.RunTurn(context.Background(), nil, messages.UserText("/mcp list"))
	if err != nil {
		t.Fatal(err)
	}
	text := result.Messages[1].Content[0].Text
	if !strings.Contains(text, "- alpha (stdio): python server.py [blocked: denied]") {
		t.Fatalf("mcp text = %q", text)
	}
}

func TestRunnerMCPSlashCommandShowsBlockedServerPolicy(t *testing.T) {
	runner := Runner{
		Client:    &fakeClient{},
		SessionID: "sess_mcp_show_blocked",
		MCP: &MCPConfig{UserSettings: contracts.Settings{
			MCPServers: map[string]contracts.MCPServer{
				"alpha": {Command: "python", Args: []string{"server.py"}},
			},
			DeniedMCPServers: []contracts.MCPServerPolicyEntry{{ServerName: "alpha"}},
		}},
	}
	result, err := runner.RunTurn(context.Background(), nil, messages.UserText("/mcp show alpha"))
	if err != nil {
		t.Fatal(err)
	}
	text := result.Messages[1].Content[0].Text
	for _, want := range []string{
		"MCP server alpha",
		"Status: blocked",
		"Policy: denied",
	} {
		if !strings.Contains(text, want) {
			t.Fatalf("mcp blocked show missing %q: %q", want, text)
		}
	}
}

func TestRunnerMCPSlashCommandReportsNoServers(t *testing.T) {
	runner := Runner{Client: &fakeClient{}, SessionID: "sess_mcp_empty"}
	result, err := runner.RunTurn(context.Background(), nil, messages.UserText("/mcp"))
	if err != nil {
		t.Fatal(err)
	}
	if len(result.Messages) != 2 {
		t.Fatalf("result messages = %#v", result.Messages)
	}
	if got := result.Messages[1].Content[0].Text; got != "No MCP servers configured." {
		t.Fatalf("mcp text = %q", got)
	}
}

func TestRunnerMCPSlashCommandReportsMissingServerDetails(t *testing.T) {
	runner := Runner{Client: &fakeClient{}, SessionID: "sess_mcp_missing", MCP: &MCPConfig{UserSettings: contracts.Settings{
		MCPServers: map[string]contracts.MCPServer{"alpha": {Command: "python"}},
	}}}
	result, err := runner.RunTurn(context.Background(), nil, messages.UserText("/mcp show missing"))
	if err != nil {
		t.Fatal(err)
	}
	if got := result.Messages[1].Content[0].Text; got != "MCP server missing was not found." {
		t.Fatalf("mcp text = %q", got)
	}
}

func TestRunnerMCPSlashCommandRemovesUserServer(t *testing.T) {
	configHome := t.TempDir()
	t.Setenv("CLAUDE_CONFIG_DIR", configHome)
	if err := os.MkdirAll(configHome, 0o755); err != nil {
		t.Fatal(err)
	}
	settingsPath := filepath.Join(configHome, "settings.json")
	if err := os.WriteFile(settingsPath, []byte(`{
		"mcpServers": {
			"alpha": {"command": "node", "args": ["alpha.js"]},
			"beta": {"type": "http", "url": "https://beta.example/mcp"}
		},
		"allowedMcpServers": [{"serverName": "alpha"}],
		"deniedMcpServers": [{"serverName": "alpha"}]
	}`), 0o644); err != nil {
		t.Fatal(err)
	}
	runner := Runner{
		Client:    &fakeClient{},
		SessionID: "sess_mcp_remove",
		MCP: &MCPConfig{UserSettings: contracts.Settings{
			MCPServers: map[string]contracts.MCPServer{
				"alpha": {Command: "node", Args: []string{"alpha.js"}, Scope: mcp.ScopeUser},
				"beta":  {Type: "http", URL: "https://beta.example/mcp", Scope: mcp.ScopeUser},
			},
			AllowedMCPServers: []contracts.MCPServerPolicyEntry{{ServerName: "alpha"}},
			DeniedMCPServers:  []contracts.MCPServerPolicyEntry{{ServerName: "alpha"}},
		}},
	}

	result, err := runner.RunTurn(context.Background(), nil, messages.UserText("/mcp delete alpha"))
	if err != nil {
		t.Fatal(err)
	}
	if got := result.Messages[1].Content[0].Text; got != "MCP server alpha removed from user settings." {
		t.Fatalf("mcp remove text = %q", got)
	}
	if _, ok := runner.MCP.UserSettings.MCPServers["alpha"]; ok {
		t.Fatalf("alpha server should be removed from runner settings: %#v", runner.MCP.UserSettings.MCPServers)
	}
	if _, ok := runner.MCP.UserSettings.MCPServers["beta"]; !ok {
		t.Fatalf("beta server should remain in runner settings: %#v", runner.MCP.UserSettings.MCPServers)
	}
	if hasMCPPolicyEntry(runner.MCP.UserSettings.AllowedMCPServers, "alpha") || hasMCPPolicyEntry(runner.MCP.UserSettings.DeniedMCPServers, "alpha") {
		t.Fatalf("alpha policy should be removed from runner settings: %#v %#v", runner.MCP.UserSettings.AllowedMCPServers, runner.MCP.UserSettings.DeniedMCPServers)
	}
	document, err := readUserSettingsDocument()
	if err != nil {
		t.Fatal(err)
	}
	if hasMCPServerDocumentEntry(document["mcpServers"], "alpha") || !hasMCPServerDocumentEntry(document["mcpServers"], "beta") {
		t.Fatalf("settings document mcpServers = %#v", document["mcpServers"])
	}
	if hasMCPPolicyDocumentEntry(document["allowedMcpServers"], "alpha") || hasMCPPolicyDocumentEntry(document["deniedMcpServers"], "alpha") {
		t.Fatalf("settings document policy = %#v %#v", document["allowedMcpServers"], document["deniedMcpServers"])
	}

	result, err = runner.RunTurn(context.Background(), result.Messages, messages.UserText("/mcp rm beta"))
	if err != nil {
		t.Fatal(err)
	}
	if got := result.Messages[len(result.Messages)-1].Content[0].Text; got != "MCP server beta removed from user settings." {
		t.Fatalf("mcp rm text = %q", got)
	}
	document, err = readUserSettingsDocument()
	if err != nil {
		t.Fatal(err)
	}
	if _, ok := document["mcpServers"]; ok {
		t.Fatalf("mcpServers should be removed when empty: %#v", document)
	}
}

func TestRunnerMCPSlashCommandRefusesToRemoveProjectServerFromUserSettings(t *testing.T) {
	t.Setenv("CLAUDE_CONFIG_DIR", t.TempDir())
	runner := Runner{Client: &fakeClient{}, SessionID: "sess_mcp_remove_project", MCP: &MCPConfig{ProjectSettings: contracts.Settings{
		MCPServers: map[string]contracts.MCPServer{"alpha": {Command: "python", Scope: mcp.ScopeProject}},
	}}}
	result, err := runner.RunTurn(context.Background(), nil, messages.UserText("/mcp delete alpha"))
	if err != nil {
		t.Fatal(err)
	}
	if got := result.Messages[1].Content[0].Text; got != "MCP server alpha is defined in project settings; remove it from that source." {
		t.Fatalf("mcp remove project text = %q", got)
	}
}

func TestRunnerMCPSlashCommandReportsUsageForUnknownSubcommand(t *testing.T) {
	runner := Runner{Client: &fakeClient{}, SessionID: "sess_mcp_subcommand"}
	result, err := runner.RunTurn(context.Background(), nil, messages.UserText("/mcp unknown alpha"))
	if err != nil {
		t.Fatal(err)
	}
	if len(result.Messages) != 2 {
		t.Fatalf("result messages = %#v", result.Messages)
	}
	got := result.Messages[1].Content[0].Text
	for _, want := range []string{
		"Unknown MCP subcommand: unknown alpha",
		"Usage: /mcp [list|status|show <server>|search <query>|refresh|restart [server]|enable <server>|disable <server>|remove <server>]",
	} {
		if !strings.Contains(got, want) {
			t.Fatalf("mcp unknown result missing %q: %q", want, got)
		}
	}
	if strings.Contains(got, "not implemented") {
		t.Fatalf("mcp unknown should not report not implemented: %q", got)
	}

	result, err = runner.RunTurn(context.Background(), nil, messages.UserText("/mcp help"))
	if err != nil {
		t.Fatal(err)
	}
	if got := result.Messages[1].Content[0].Text; got != "Usage: /mcp [list|status|show <server>|search <query>|refresh|restart [server]|enable <server>|disable <server>|remove <server>]" {
		t.Fatalf("mcp help text = %q", got)
	}
}

func TestRunnerMCPSlashCommandRefreshesPluginServerState(t *testing.T) {
	project := t.TempDir()
	pluginDir := filepath.Join(project, ".claude", "plugins", "demo")
	if err := os.MkdirAll(pluginDir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(pluginDir, "plugin.json"), []byte(`{
		"name": "demo",
		"mcpServers": {
			"plugin:docs": {"type": "http", "url": "https://example.com/mcp"}
		}
	}`), 0o644); err != nil {
		t.Fatal(err)
	}
	runner := Runner{
		Client:           &fakeClient{},
		SessionID:        "sess_mcp_refresh",
		WorkingDirectory: project,
		MCP:              &MCPConfig{CWD: project},
	}

	result, err := runner.RunTurn(context.Background(), nil, messages.UserText("/mcp refresh"))
	if err != nil {
		t.Fatal(err)
	}
	text := result.Messages[1].Content[0].Text
	for _, want := range []string{
		"MCP configuration refreshed.",
		"Settings files changed: false",
		"Plugin MCP servers: 1",
		"Configured MCP servers: 1",
	} {
		if !strings.Contains(text, want) {
			t.Fatalf("mcp refresh missing %q: %q", want, text)
		}
	}
	if server := runner.MCP.PluginServers["plugin:docs"]; server.URL != "https://example.com/mcp" || server.PluginSource != "demo" {
		t.Fatalf("plugin MCP servers = %#v", runner.MCP.PluginServers)
	}

	usage, err := runner.RunTurn(context.Background(), result.Messages, messages.UserText("/mcp refresh alpha"))
	if err != nil {
		t.Fatal(err)
	}
	if got := usage.Messages[len(usage.Messages)-1].Content[0].Text; got != "Usage: /mcp refresh" {
		t.Fatalf("mcp refresh usage = %q", got)
	}
}

func TestRunnerMCPSlashCommandRestartsStatelessServer(t *testing.T) {
	project := t.TempDir()
	runner := Runner{
		Client:           &fakeClient{},
		SessionID:        "sess_mcp_restart",
		WorkingDirectory: project,
		MCP: &MCPConfig{
			CWD: project,
			UserSettings: contracts.Settings{
				MCPServers: map[string]contracts.MCPServer{
					"alpha": {Command: "python", Args: []string{"server.py"}},
				},
			},
		},
	}

	result, err := runner.RunTurn(context.Background(), nil, messages.UserText("/mcp restart alpha"))
	if err != nil {
		t.Fatal(err)
	}
	text := result.Messages[1].Content[0].Text
	for _, want := range []string{
		"MCP server alpha restart requested.",
		"Settings files changed: false",
		"Plugin MCP servers: 0",
		"Configured MCP servers: 1",
		"Runtime lifecycle: stateless; server will be reopened on the next tool call.",
	} {
		if !strings.Contains(text, want) {
			t.Fatalf("mcp restart missing %q: %q", want, text)
		}
	}

	missing, err := runner.RunTurn(context.Background(), result.Messages, messages.UserText("/mcp reconnect missing"))
	if err != nil {
		t.Fatal(err)
	}
	if got := missing.Messages[len(missing.Messages)-1].Content[0].Text; got != "MCP server missing was not found." {
		t.Fatalf("mcp reconnect missing = %q", got)
	}
}

func TestRunnerMCPSlashCommandUpdatesPolicySettings(t *testing.T) {
	configHome := t.TempDir()
	t.Setenv("CLAUDE_CONFIG_DIR", configHome)
	if err := os.MkdirAll(configHome, 0o755); err != nil {
		t.Fatal(err)
	}
	settingsPath := filepath.Join(configHome, "settings.json")
	if err := os.WriteFile(settingsPath, []byte(`{
		"allowedMcpServers": [
			{"serverName": "alpha"},
			{"serverName": "beta"}
		]
	}`), 0o644); err != nil {
		t.Fatal(err)
	}
	runner := Runner{
		Client:    &fakeClient{},
		SessionID: "sess_mcp_policy",
		MCP: &MCPConfig{UserSettings: contracts.Settings{
			AllowedMCPServers: []contracts.MCPServerPolicyEntry{
				{ServerName: "alpha"},
				{ServerName: "beta"},
			},
		}},
	}

	result, err := runner.RunTurn(context.Background(), nil, messages.UserText("/mcp disable alpha"))
	if err != nil {
		t.Fatal(err)
	}
	if got := result.Messages[1].Content[0].Text; got != "MCP server alpha disabled." {
		t.Fatalf("disable text = %q", got)
	}
	if hasMCPPolicyEntry(runner.MCP.UserSettings.AllowedMCPServers, "alpha") || !hasMCPPolicyEntry(runner.MCP.UserSettings.DeniedMCPServers, "alpha") {
		t.Fatalf("runner policy after disable = %#v %#v", runner.MCP.UserSettings.AllowedMCPServers, runner.MCP.UserSettings.DeniedMCPServers)
	}
	document, err := readUserSettingsDocument()
	if err != nil {
		t.Fatal(err)
	}
	if hasMCPPolicyDocumentEntry(document["allowedMcpServers"], "alpha") || !hasMCPPolicyDocumentEntry(document["deniedMcpServers"], "alpha") {
		t.Fatalf("settings document after disable = %#v", document)
	}

	result, err = runner.RunTurn(context.Background(), result.Messages, messages.UserText("/mcp enable alpha"))
	if err != nil {
		t.Fatal(err)
	}
	if got := result.Messages[len(result.Messages)-1].Content[0].Text; got != "MCP server alpha enabled." {
		t.Fatalf("enable text = %q", got)
	}
	if !hasMCPPolicyEntry(runner.MCP.UserSettings.AllowedMCPServers, "alpha") || hasMCPPolicyEntry(runner.MCP.UserSettings.DeniedMCPServers, "alpha") {
		t.Fatalf("runner policy after enable = %#v %#v", runner.MCP.UserSettings.AllowedMCPServers, runner.MCP.UserSettings.DeniedMCPServers)
	}
	document, err = readUserSettingsDocument()
	if err != nil {
		t.Fatal(err)
	}
	if !hasMCPPolicyDocumentEntry(document["allowedMcpServers"], "alpha") || hasMCPPolicyDocumentEntry(document["deniedMcpServers"], "alpha") {
		t.Fatalf("settings document after enable = %#v", document)
	}
}

func TestRunnerMCPEnableCreatesUserAllowEntryWhenProjectAllowlistActive(t *testing.T) {
	configHome := t.TempDir()
	t.Setenv("CLAUDE_CONFIG_DIR", configHome)
	runner := Runner{
		Client:    &fakeClient{},
		SessionID: "sess_mcp_project_allowlist",
		MCP: &MCPConfig{
			UserSettings: contracts.Settings{
				DeniedMCPServers: []contracts.MCPServerPolicyEntry{{ServerName: "alpha"}},
			},
			ProjectSettings: contracts.Settings{
				AllowedMCPServers: []contracts.MCPServerPolicyEntry{{ServerName: "beta"}},
			},
		},
	}

	result, err := runner.RunTurn(context.Background(), nil, messages.UserText("/mcp enable alpha"))
	if err != nil {
		t.Fatal(err)
	}
	if got := result.Messages[1].Content[0].Text; got != "MCP server alpha enabled." {
		t.Fatalf("enable text = %q", got)
	}
	if !hasMCPPolicyEntry(runner.MCP.UserSettings.AllowedMCPServers, "alpha") || hasMCPPolicyEntry(runner.MCP.UserSettings.DeniedMCPServers, "alpha") {
		t.Fatalf("runner user policy = %#v %#v", runner.MCP.UserSettings.AllowedMCPServers, runner.MCP.UserSettings.DeniedMCPServers)
	}
	document, err := readUserSettingsDocument()
	if err != nil {
		t.Fatal(err)
	}
	if !hasMCPPolicyDocumentEntry(document["allowedMcpServers"], "alpha") || hasMCPPolicyDocumentEntry(document["deniedMcpServers"], "alpha") {
		t.Fatalf("settings document = %#v", document)
	}
}

func hasMCPPolicyEntry(entries []contracts.MCPServerPolicyEntry, name string) bool {
	for _, entry := range entries {
		if mcpPolicyEntryNameMatches(entry, name) {
			return true
		}
	}
	return false
}

func hasMCPPolicyDocumentEntry(value any, name string) bool {
	entries, _ := value.([]any)
	for _, entry := range entries {
		if mcpPolicyEntryValueMatches(entry, name) {
			return true
		}
	}
	return false
}

func hasMCPServerDocumentEntry(value any, name string) bool {
	servers, _ := value.(map[string]any)
	for key := range servers {
		if strings.TrimSpace(key) == name {
			return true
		}
	}
	return false
}

func marketplaceDocumentEntry(t *testing.T, document map[string]any, name string) map[string]any {
	t.Helper()
	extraKnown, ok := document["extraKnownMarketplaces"].(map[string]any)
	if !ok {
		t.Fatalf("extraKnownMarketplaces = %#v", document["extraKnownMarketplaces"])
	}
	entry, ok := extraKnown[name].(map[string]any)
	if !ok {
		t.Fatalf("marketplace %q = %#v", name, extraKnown[name])
	}
	return entry
}

func TestRunnerExecutesResumeSlashCommandWithoutQuery(t *testing.T) {
	cwd := t.TempDir()
	t.Setenv("CLAUDE_CONFIG_DIR", t.TempDir())
	path := session.TranscriptPath(cwd, "sess_resume_list")
	if err := session.Append(path, session.EntryFromMessage("sess_resume_list", messages.UserText("deploy question"))); err != nil {
		t.Fatal(err)
	}
	runner := Runner{
		Client:           &fakeClient{},
		SessionID:        "sess_resume_current",
		WorkingDirectory: cwd,
	}
	result, err := runner.RunTurn(context.Background(), nil, messages.UserText("/resume"))
	if err != nil {
		t.Fatal(err)
	}
	if result.Assistant.Type != "" || result.FinalRequest.Model != "" {
		t.Fatalf("unexpected model result = %#v", result)
	}
	if len(result.Messages) != 2 {
		t.Fatalf("result messages = %#v", result.Messages)
	}
	text := result.Messages[1].Content[0].Text
	if !strings.Contains(text, "Recent sessions:") || !strings.Contains(text, "sess_resume_list") {
		t.Fatalf("resume text = %q", text)
	}
}

func TestRunnerResumeSlashCommandSearchesSessions(t *testing.T) {
	cwd := t.TempDir()
	t.Setenv("CLAUDE_CONFIG_DIR", t.TempDir())
	path := session.TranscriptPath(cwd, "sess_resume_search")
	if err := session.Append(path, session.EntryFromMessage("sess_resume_search", messages.UserText("deploy searchable question"))); err != nil {
		t.Fatal(err)
	}
	runner := Runner{
		Client:           &fakeClient{},
		SessionID:        "sess_resume_search_current",
		WorkingDirectory: cwd,
	}
	result, err := runner.RunTurn(context.Background(), nil, messages.UserText("/resume searchable"))
	if err != nil {
		t.Fatal(err)
	}
	if len(result.Messages) != 2 {
		t.Fatalf("result messages = %#v", result.Messages)
	}
	text := result.Messages[1].Content[0].Text
	if !strings.Contains(text, `Matching sessions for "searchable":`) || !strings.Contains(text, "sess_resume_search") {
		t.Fatalf("resume search text = %q", text)
	}

	result, err = runner.RunTurn(context.Background(), nil, messages.UserText("/resume search searchable"))
	if err != nil {
		t.Fatal(err)
	}
	text = result.Messages[1].Content[0].Text
	if !strings.Contains(text, `Matching sessions for "searchable":`) || !strings.Contains(text, "sess_resume_search") {
		t.Fatalf("resume explicit search text = %q", text)
	}

	result, err = runner.RunTurn(context.Background(), nil, messages.UserText("/resume search"))
	if err != nil {
		t.Fatal(err)
	}
	if got := result.Messages[1].Content[0].Text; got != "Usage: /resume search <query>" {
		t.Fatalf("resume search usage = %q", got)
	}
}

func TestRunnerResumeSlashCommandShowsSessionDetails(t *testing.T) {
	cwd := t.TempDir()
	t.Setenv("CLAUDE_CONFIG_DIR", t.TempDir())
	sessionID := contracts.ID("sess_resume_detail")
	path := session.TranscriptPath(cwd, sessionID)
	user := messages.UserText("first deploy request")
	user.UUID = "user_detail"
	assistant := messages.AssistantText("deployment response", "sonnet", nil)
	assistant.UUID = "assistant_detail"
	if err := session.AppendTranscriptMessage(path, session.TranscriptMessage{
		Type:      "user",
		UUID:      user.UUID,
		SessionID: sessionID,
		Message:   &user,
		CWD:       cwd,
		GitBranch: "main",
	}); err != nil {
		t.Fatal(err)
	}
	if err := session.AppendTranscriptMessage(path, session.TranscriptMessage{
		Type:      "assistant",
		UUID:      assistant.UUID,
		SessionID: sessionID,
		Message:   &assistant,
	}); err != nil {
		t.Fatal(err)
	}
	appendRawTranscriptLines(t, path, []string{
		`{"type":"custom-title","sessionId":"sess_resume_detail","customTitle":"Deploy Detail"}`,
		`{"type":"ai-title","sessionId":"sess_resume_detail","aiTitle":"AI Deploy"}`,
		`{"type":"last-prompt","sessionId":"sess_resume_detail","lastPrompt":"resume last prompt"}`,
		`{"type":"task-summary","sessionId":"sess_resume_detail","summary":"running deployment checks"}`,
		`{"type":"tag","sessionId":"sess_resume_detail","tag":"ops"}`,
		`{"type":"mode","sessionId":"sess_resume_detail","mode":"plan"}`,
		`{"type":"pr-link","sessionId":"sess_resume_detail","prNumber":42,"prUrl":"https://example/pr/42","prRepository":"org/repo"}`,
	})

	runner := Runner{
		Client:           &fakeClient{},
		SessionID:        "sess_resume_detail_current",
		WorkingDirectory: cwd,
	}
	result, err := runner.RunTurn(context.Background(), nil, messages.UserText("/resume show sess_resume_detail"))
	if err != nil {
		t.Fatal(err)
	}
	text := result.Messages[1].Content[0].Text
	for _, want := range []string{
		"Session sess_resume_detail",
		"Title: Deploy Detail",
		"Path: " + path,
		"Messages: 2",
		"User messages: 1",
		"Assistant messages: 1",
		"First message UUID: user_detail",
		"Last message UUID: assistant_detail",
		"First user: first deploy request",
		"Last assistant: deployment response",
		"Project path: " + cwd,
		"Git branch: main",
		"AI title: AI Deploy",
		"Last prompt: resume last prompt",
		"Task summary: running deployment checks",
		"Tag: ops",
		"Mode: plan",
		"Pull request: #42 org/repo https://example/pr/42",
	} {
		if !strings.Contains(text, want) {
			t.Fatalf("resume show missing %q: %q", want, text)
		}
	}
}

func TestRunnerResumeSlashCommandReportsMissingSessionDetails(t *testing.T) {
	cwd := t.TempDir()
	t.Setenv("CLAUDE_CONFIG_DIR", t.TempDir())
	runner := Runner{
		Client:           &fakeClient{},
		SessionID:        "sess_resume_missing_current",
		WorkingDirectory: cwd,
	}
	result, err := runner.RunTurn(context.Background(), nil, messages.UserText("/resume show missing"))
	if err != nil {
		t.Fatal(err)
	}
	if got := result.Messages[1].Content[0].Text; got != "Session missing was not found." {
		t.Fatalf("resume show missing = %q", got)
	}
}

func TestRunnerResumeSlashCommandReportsNoSessions(t *testing.T) {
	cwd := t.TempDir()
	t.Setenv("CLAUDE_CONFIG_DIR", t.TempDir())
	runner := Runner{
		Client:           &fakeClient{},
		SessionID:        "sess_resume_empty",
		WorkingDirectory: cwd,
	}
	result, err := runner.RunTurn(context.Background(), nil, messages.UserText("/resume"))
	if err != nil {
		t.Fatal(err)
	}
	if len(result.Messages) != 2 {
		t.Fatalf("result messages = %#v", result.Messages)
	}
	if got := result.Messages[1].Content[0].Text; !strings.Contains(got, "No sessions found for ") {
		t.Fatalf("resume text = %q", got)
	}
}

func appendRawTranscriptLines(t *testing.T, path string, lines []string) {
	t.Helper()
	f, err := os.OpenFile(path, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o600)
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	for _, line := range lines {
		if _, err := f.WriteString(line + "\n"); err != nil {
			t.Fatal(err)
		}
	}
}

func TestRunnerAppliesSlashCommandAllowedToolsToToolPermissions(t *testing.T) {
	repo := filepath.Join(t.TempDir(), "repo")
	cwd := filepath.Join(repo, "pkg")
	skillDir := filepath.Join(cwd, ".claude", "skills", "permit")
	if err := os.MkdirAll(skillDir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(skillDir, "SKILL.md"), []byte("---\ndescription: Permit edit\nallowed-tools: Edit\n---\nUse Edit for $ARGUMENTS."), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Join(repo, ".git"), 0o755); err != nil {
		t.Fatal(err)
	}
	registry, err := tool.NewRegistry(tool.FuncTool{
		DefinitionValue: contracts.ToolDefinition{
			Name:   "Edit",
			Strict: true,
			InputSchema: contracts.JSONSchema{
				"type":       "object",
				"properties": map[string]any{},
			},
		},
		CallFunc: func(ctx tool.Context, raw json.RawMessage, sink tool.ProgressSink) (contracts.ToolResult, error) {
			return contracts.ToolResult{Content: "edited"}, nil
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	client := &fakeClient{calls: []fakeCall{
		{response: &anthropic.Response{
			ID:         "msg_tool",
			Type:       "message",
			Role:       "assistant",
			Model:      "sonnet",
			StopReason: "tool_use",
			Content: []contracts.ContentBlock{{
				Type:  contracts.ContentToolUse,
				ID:    "toolu_edit",
				Name:  "Edit",
				Input: json.RawMessage(`{}`),
			}},
		}},
		{response: &anthropic.Response{
			ID:         "msg_done",
			Type:       "message",
			Role:       "assistant",
			Model:      "sonnet",
			StopReason: "end_turn",
			Content:    []contracts.ContentBlock{contracts.NewTextBlock("done")},
		}},
	}}
	runner := Runner{
		Client:           client,
		Tools:            tool.NewExecutor(registry),
		Permissions:      tool.NewEnginePermissionDecider(permissions.NewEngine(contracts.PermissionContext{Mode: contracts.PermissionDefault})),
		Model:            "sonnet",
		MaxTokens:        128,
		SessionID:        "sess_allowed",
		WorkingDirectory: cwd,
	}

	result, err := runner.RunTurn(context.Background(), nil, messages.UserText("/permit target"))
	if err != nil {
		t.Fatal(err)
	}
	if len(result.ToolResults) != 1 || result.ToolResults[0].IsError || result.ToolResults[0].Content != "edited" {
		t.Fatalf("tool results = %#v", result.ToolResults)
	}
}

func TestRunnerAppliesSkillToolAllowedToolsToFollowingToolRound(t *testing.T) {
	commandRegistry := commands.FromSources(commands.Sources{
		ProjectSkillPrompts: []commands.PromptTemplate{{
			Command: contracts.Command{
				Name:         "permit",
				Type:         contracts.CommandPrompt,
				Source:       contracts.CommandSourceSkills,
				LoadedFrom:   "skills",
				AllowedTools: []string{"Edit"},
			},
			Content: "Use Edit for $ARGUMENTS.",
		}},
	})
	toolRegistry, err := tool.NewRegistry(skilltools.NewSkillTool(commandRegistry), tool.FuncTool{
		DefinitionValue: contracts.ToolDefinition{
			Name:   "Edit",
			Strict: true,
			InputSchema: contracts.JSONSchema{
				"type":       "object",
				"properties": map[string]any{},
			},
		},
		CallFunc: func(ctx tool.Context, raw json.RawMessage, sink tool.ProgressSink) (contracts.ToolResult, error) {
			return contracts.ToolResult{Content: "edited"}, nil
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	client := &fakeClient{calls: []fakeCall{
		{response: &anthropic.Response{
			ID:         "msg_skill",
			Type:       "message",
			Role:       "assistant",
			Model:      "sonnet",
			StopReason: "tool_use",
			Content: []contracts.ContentBlock{{
				Type:  contracts.ContentToolUse,
				ID:    "toolu_skill",
				Name:  "Skill",
				Input: json.RawMessage(`{"skill":"permit","args":"target"}`),
			}},
		}},
		{response: &anthropic.Response{
			ID:         "msg_edit",
			Type:       "message",
			Role:       "assistant",
			Model:      "sonnet",
			StopReason: "tool_use",
			Content: []contracts.ContentBlock{{
				Type:  contracts.ContentToolUse,
				ID:    "toolu_edit",
				Name:  "Edit",
				Input: json.RawMessage(`{}`),
			}},
		}},
		{response: &anthropic.Response{
			ID:         "msg_done",
			Type:       "message",
			Role:       "assistant",
			Model:      "sonnet",
			StopReason: "end_turn",
			Content:    []contracts.ContentBlock{contracts.NewTextBlock("done")},
		}},
	}}
	runner := Runner{
		Client: client,
		Tools:  tool.NewExecutor(toolRegistry),
		Permissions: tool.NewEnginePermissionDecider(permissions.NewEngine(contracts.PermissionContext{
			Mode: contracts.PermissionDefault,
		})),
		Model:     "sonnet",
		MaxTokens: 128,
		SessionID: "sess_skill_allowed",
	}

	result, err := runner.RunTurn(context.Background(), nil, messages.UserText("use permit skill"))
	if err != nil {
		t.Fatal(err)
	}
	if len(result.ToolResults) != 2 || result.ToolResults[1].IsError || result.ToolResults[1].Content != "edited" {
		t.Fatalf("tool results = %#v", result.ToolResults)
	}
}

func TestRunnerAppliesToolResultBudgetBeforeNextRequest(t *testing.T) {
	registry, err := tool.NewRegistry(tool.FuncTool{
		DefinitionValue: contracts.ToolDefinition{Name: "Big", ReadOnly: true},
		CallFunc: func(ctx tool.Context, raw json.RawMessage, sink tool.ProgressSink) (contracts.ToolResult, error) {
			return contracts.ToolResult{Content: "0123456789abcdef"}, nil
		},
	})
	if err != nil {
		t.Fatal(err)
	}

	client := &fakeClient{calls: []fakeCall{
		{response: &anthropic.Response{
			ID:         "msg_tool",
			Type:       "message",
			Role:       "assistant",
			Model:      "sonnet",
			StopReason: "tool_use",
			Content: []contracts.ContentBlock{{
				Type:  contracts.ContentToolUse,
				ID:    "toolu_big",
				Name:  "Big",
				Input: json.RawMessage(`{}`),
			}},
		}},
		{response: &anthropic.Response{
			ID:         "msg_done",
			Type:       "message",
			Role:       "assistant",
			Model:      "sonnet",
			StopReason: "end_turn",
			Content:    []contracts.ContentBlock{contracts.NewTextBlock("done")},
		}},
	}}
	transcriptPath := filepath.Join(t.TempDir(), "session.jsonl")
	runner := Runner{
		Client:           client,
		Tools:            tool.NewExecutor(registry),
		Model:            "sonnet",
		MaxTokens:        128,
		SessionID:        "sess_budget",
		SessionPath:      transcriptPath,
		ContentBudget:    session.NewContentReplacementState(),
		ContentBudgetMax: 8,
	}

	if _, err := runner.RunTurn(context.Background(), nil, messages.UserText("run big")); err != nil {
		t.Fatal(err)
	}
	if len(client.requests) != 2 {
		t.Fatalf("requests = %d, want 2", len(client.requests))
	}
	last := client.requests[1].Messages[len(client.requests[1].Messages)-1]
	got, _ := last.Content[0].Content.(string)
	if !strings.HasPrefix(got, session.PersistedOutputTag) {
		t.Fatalf("tool result was not replaced in request: %#v", got)
	}
	transcript, err := session.LoadTranscript(transcriptPath)
	if err != nil {
		t.Fatal(err)
	}
	records := transcript.ContentReplacements["sess_budget"]
	if len(records) != 1 || records[0].ToolUseID != "toolu_big" {
		t.Fatalf("content replacement records = %#v", records)
	}
	persistedPath := filepath.Join(filepath.Dir(transcriptPath), "sess_budget", "tool-results", "toolu_big.txt")
	data, err := os.ReadFile(persistedPath)
	if err != nil {
		t.Fatal(err)
	}
	if string(data) != "0123456789abcdef" {
		t.Fatalf("persisted content = %q", string(data))
	}
}

func TestRunnerPreservesToolMetadataAcrossRounds(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "note.txt")
	if err := os.WriteFile(path, []byte("old\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	registry, err := tool.NewRegistry(filetools.BuiltinTools()...)
	if err != nil {
		t.Fatal(err)
	}
	client := &fakeClient{calls: []fakeCall{
		{response: &anthropic.Response{
			ID:         "msg_read",
			Type:       "message",
			Role:       "assistant",
			Model:      "sonnet",
			StopReason: "tool_use",
			Content: []contracts.ContentBlock{{
				Type:  contracts.ContentToolUse,
				ID:    "toolu_read",
				Name:  "Read",
				Input: json.RawMessage(`{"file_path":"note.txt"}`),
			}},
		}},
		{response: &anthropic.Response{
			ID:         "msg_edit",
			Type:       "message",
			Role:       "assistant",
			Model:      "sonnet",
			StopReason: "tool_use",
			Content: []contracts.ContentBlock{{
				Type:  contracts.ContentToolUse,
				ID:    "toolu_edit",
				Name:  "Edit",
				Input: json.RawMessage(`{"file_path":"note.txt","old_string":"old","new_string":"new"}`),
			}},
		}},
		{response: &anthropic.Response{
			ID:         "msg_done",
			Type:       "message",
			Role:       "assistant",
			Model:      "sonnet",
			StopReason: "end_turn",
			Content:    []contracts.ContentBlock{contracts.NewTextBlock("done")},
		}},
	}}
	runner := Runner{
		Client:           client,
		Tools:            tool.NewExecutor(registry),
		Model:            "sonnet",
		MaxTokens:        128,
		WorkingDirectory: dir,
	}
	if _, err := runner.RunTurn(context.Background(), nil, messages.UserText("read then edit")); err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if string(data) != "new\n" {
		t.Fatalf("edited content = %q", data)
	}
}

func TestRunnerAutoCompactsBeforeMainRequest(t *testing.T) {
	client := &fakeClient{calls: []fakeCall{
		{response: &anthropic.Response{
			ID:         "msg_summary",
			Type:       "message",
			Role:       "assistant",
			Model:      "sonnet",
			StopReason: "end_turn",
			Content:    []contracts.ContentBlock{contracts.NewTextBlock("summary text")},
		}},
		{response: &anthropic.Response{
			ID:         "msg_done",
			Type:       "message",
			Role:       "assistant",
			Model:      "sonnet",
			StopReason: "end_turn",
			Content:    []contracts.ContentBlock{contracts.NewTextBlock("done")},
		}},
	}}
	var events []EventType
	transcriptPath := filepath.Join(t.TempDir(), "session.jsonl")
	runner := Runner{
		Client:      client,
		Model:       "sonnet",
		MaxTokens:   128,
		SessionID:   "sess_compact",
		SessionPath: transcriptPath,
		AutoCompact: &compactpkg.AutoConfig{
			Enabled:  true,
			Force:    true,
			KeepLast: 1,
		},
		OnEvent: func(event Event) {
			events = append(events, event.Type)
		},
	}
	history := []contracts.Message{
		messages.UserText("old one"),
		messages.AssistantText("old two", "sonnet", nil),
	}
	result, err := runner.RunTurn(context.Background(), history, messages.UserText("new request"))
	if err != nil {
		t.Fatal(err)
	}
	if !result.Compacted || result.Compact == nil {
		t.Fatalf("result did not compact: %#v", result)
	}
	if len(client.requests) != 2 {
		t.Fatalf("requests = %d, want 2", len(client.requests))
	}
	compactPrompt := client.requests[0].Messages[len(client.requests[0].Messages)-1]
	if !strings.Contains(compactPrompt.Content[0].Text, "Do NOT call any tools") {
		t.Fatalf("compact prompt = %#v", compactPrompt)
	}
	mainReq := client.requests[1]
	if len(mainReq.Messages) != 2 {
		t.Fatalf("main request messages = %#v", mainReq.Messages)
	}
	if got := mainReq.Messages[0].Content[0].Text; !strings.Contains(got, "summary text") {
		t.Fatalf("main summary = %q", got)
	}
	if got := mainReq.Messages[1].Content[0].Text; got != "new request" {
		t.Fatalf("kept recent message = %q", got)
	}
	if !containsEvent(events, EventCompact) {
		t.Fatalf("events = %#v", events)
	}
	transcript, err := session.LoadTranscript(transcriptPath)
	if err != nil {
		t.Fatal(err)
	}
	foundBoundary := false
	for _, id := range transcript.Order {
		msg := transcript.Messages[id]
		if msg != nil && msg.IsCompactBoundary() && msg.CompactMetadata != nil && msg.CompactMetadata.MessagesSummarized == 2 {
			foundBoundary = true
			break
		}
	}
	if !foundBoundary {
		t.Fatalf("transcript missing compact boundary: %#v", transcript.Order)
	}
	summary, err := memory.LoadSessionSummary(filepath.Join(filepath.Dir(transcriptPath), "session-memory", "sess_compact", memory.SessionSummaryFilename))
	if err != nil {
		t.Fatal(err)
	}
	if summary.SessionID != "sess_compact" || !strings.Contains(summary.Summary, "summary text") || summary.Metadata.MessagesSummarized != 2 {
		t.Fatalf("session memory summary = %#v", summary)
	}
}

func TestRunnerEmitsTokenWarningBeforeMainRequest(t *testing.T) {
	client := &fakeClient{calls: []fakeCall{
		{response: &anthropic.Response{
			ID:         "msg_done",
			Type:       "message",
			Role:       "assistant",
			Model:      "sonnet",
			StopReason: "end_turn",
			Content:    []contracts.ContentBlock{contracts.NewTextBlock("done")},
		}},
	}}
	var events []EventType
	var warnings []TokenWarning
	runner := Runner{
		Client:    client,
		Model:     "sonnet",
		MaxTokens: 128,
		AutoCompact: &compactpkg.AutoConfig{
			Enabled:    true,
			TokenUsage: 160_000,
			Window: compactpkg.WindowConfig{
				ContextWindow:   200_000,
				MaxOutputTokens: 20_000,
			},
		},
		OnEvent: func(event Event) {
			events = append(events, event.Type)
			if event.Type == EventTokenWarning {
				if event.TokenWarning == nil {
					t.Fatal("token warning event missing payload")
				}
				warnings = append(warnings, *event.TokenWarning)
			}
		},
	}

	result, err := runner.RunTurn(context.Background(), []contracts.Message{messages.UserText("old")}, messages.UserText("new"))
	if err != nil {
		t.Fatal(err)
	}
	if result.Compacted {
		t.Fatalf("warning-only turn should not compact: %#v", result)
	}
	if len(client.requests) != 1 {
		t.Fatalf("requests = %d, want only main request", len(client.requests))
	}
	if len(warnings) != 1 {
		t.Fatalf("warnings = %#v events = %#v", warnings, events)
	}
	warning := warnings[0]
	if warning.TokenUsage != 160_000 || warning.State.PercentLeft != 4 {
		t.Fatalf("warning payload = %#v", warning)
	}
	if !warning.State.IsAboveWarningThreshold || warning.State.IsAboveAutoCompactThreshold || !warning.Window.AutoCompactEnabled {
		t.Fatalf("warning state = %#v window = %#v", warning.State, warning.Window)
	}
	if len(events) < 2 || events[0] != EventUserMessage || events[1] != EventTokenWarning {
		t.Fatalf("events = %#v", events)
	}
}

func TestRunnerAutoCompactFailureDoesNotBlockMainRequest(t *testing.T) {
	client := &fakeClient{calls: []fakeCall{
		{err: anthropic.APIError{StatusCode: http.StatusInternalServerError, Type: "overloaded_error", Message: "compact failed"}},
		{response: &anthropic.Response{
			ID:         "msg_done",
			Type:       "message",
			Role:       "assistant",
			Model:      "sonnet",
			StopReason: "end_turn",
			Content:    []contracts.ContentBlock{contracts.NewTextBlock("done")},
		}},
	}}
	var compactErrors int
	config := &compactpkg.AutoConfig{Enabled: true, Force: true}
	runner := Runner{
		Client:      client,
		Model:       "sonnet",
		MaxTokens:   128,
		AutoCompact: config,
		OnEvent: func(event Event) {
			if event.Type == EventCompact && event.Error != nil {
				compactErrors++
			}
		},
	}

	result, err := runner.RunTurn(context.Background(), []contracts.Message{messages.UserText("old")}, messages.UserText("new"))
	if err != nil {
		t.Fatal(err)
	}
	if result.Compacted || result.Assistant.Content[0].Text != "done" {
		t.Fatalf("result = %#v", result)
	}
	if config.ConsecutiveFailures != 1 || compactErrors != 1 {
		t.Fatalf("failures=%d compactErrors=%d", config.ConsecutiveFailures, compactErrors)
	}
	if len(client.requests) != 2 {
		t.Fatalf("requests = %d, want compact attempt and main request", len(client.requests))
	}
}

func TestRunnerAutoCompactSkipsAfterFailureLimit(t *testing.T) {
	client := &fakeClient{calls: []fakeCall{
		{response: &anthropic.Response{
			ID:         "msg_done",
			Type:       "message",
			Role:       "assistant",
			Model:      "sonnet",
			StopReason: "end_turn",
			Content:    []contracts.ContentBlock{contracts.NewTextBlock("done")},
		}},
	}}
	config := &compactpkg.AutoConfig{
		Enabled:             true,
		TokenUsage:          10_000,
		ConsecutiveFailures: compactpkg.DefaultMaxConsecutiveFailures,
		Window: compactpkg.WindowConfig{
			ContextWindow:      12_000,
			MaxOutputTokens:    1_000,
			AutoCompactEnabled: true,
		},
	}
	runner := Runner{
		Client:      client,
		Model:       "sonnet",
		MaxTokens:   128,
		AutoCompact: config,
	}

	if _, err := runner.RunTurn(context.Background(), []contracts.Message{messages.UserText("old")}, messages.UserText("new")); err != nil {
		t.Fatal(err)
	}
	if len(client.requests) != 1 {
		t.Fatalf("requests = %d, want only main request", len(client.requests))
	}
	if config.ConsecutiveFailures != compactpkg.DefaultMaxConsecutiveFailures {
		t.Fatalf("failure count changed = %d", config.ConsecutiveFailures)
	}
}

func TestRunnerInjectsSessionMemoryRecallIntoRequest(t *testing.T) {
	root := filepath.Join(t.TempDir(), "session-memory")
	if _, err := memory.WriteSessionSummary(memory.SessionSummaryOptions{
		Root:      root,
		SessionID: "prior",
		Summary:   "database permissions and migration notes",
	}); err != nil {
		t.Fatal(err)
	}
	if _, err := memory.WriteSessionSummary(memory.SessionSummaryOptions{
		Root:      root,
		SessionID: "current",
		Summary:   "database current session should be excluded",
	}); err != nil {
		t.Fatal(err)
	}
	client := &fakeClient{calls: []fakeCall{
		{response: &anthropic.Response{
			ID:         "msg_done",
			Type:       "message",
			Role:       "assistant",
			Model:      "sonnet",
			StopReason: "end_turn",
			Content:    []contracts.ContentBlock{contracts.NewTextBlock("done")},
		}},
	}}
	runner := Runner{
		Client:                    client,
		Model:                     "sonnet",
		MaxTokens:                 128,
		SessionID:                 "current",
		EnableSessionMemoryRecall: true,
		SessionMemoryRecallRoot:   root,
		SessionMemoryRecallLimit:  2,
	}
	if _, err := runner.RunTurn(context.Background(), nil, messages.UserText("database permissions")); err != nil {
		t.Fatal(err)
	}
	if len(client.requests) != 1 {
		t.Fatalf("requests = %d", len(client.requests))
	}
	apiMessages := client.requests[0].Messages
	if len(apiMessages) != 2 {
		t.Fatalf("api messages = %#v", apiMessages)
	}
	recall := apiMessages[0].Content[0].Text
	if !strings.Contains(recall, "Relevant session memory") || !strings.Contains(recall, "[prior]") || strings.Contains(recall, "[current]") {
		t.Fatalf("recall = %q", recall)
	}
	if got := apiMessages[1].Content[0].Text; got != "database permissions" {
		t.Fatalf("user message = %q", got)
	}
}

func TestRunnerExpandsRelevantMemoryAttachmentsIntoRequest(t *testing.T) {
	now := time.Date(2026, 6, 3, 12, 0, 0, 0, time.UTC)
	mem := memory.NewRelevantMemory("/repo/.claude/memory/db.md", "database memory", now, now)
	runner := Runner{Model: "sonnet", MaxTokens: 128}

	request, err := runner.BuildRequest([]contracts.Message{
		memory.RelevantMemoriesAttachmentMessage([]memory.RelevantMemory{mem}),
		messages.UserText("continue database work"),
	}, "sonnet")
	if err != nil {
		t.Fatal(err)
	}
	if len(request.Messages) != 2 {
		t.Fatalf("messages = %#v", request.Messages)
	}
	first := request.Messages[0]
	if first.Role != "user" || len(first.Content) != 1 || !strings.HasPrefix(first.Content[0].Text, "<system-reminder>\nMemory (saved today): /repo/.claude/memory/db.md:") || !strings.Contains(first.Content[0].Text, "database memory\n</system-reminder>") {
		t.Fatalf("first message = %#v", first)
	}
	if got := request.Messages[1].Content[0].Text; got != "continue database work" {
		t.Fatalf("user text = %q", got)
	}
}

func TestRunnerInjectsRelevantMemoryFromConfiguredDirIntoRequest(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "db.md")
	if err := os.WriteFile(path, []byte("---\ndescription: database permissions migration\n---\nremember database permission rules\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	now := time.Now()
	if err := os.Chtimes(path, now, now); err != nil {
		t.Fatal(err)
	}
	runner := Runner{Model: "sonnet", MaxTokens: 128, RelevantMemoryDir: dir}

	request, err := runner.BuildRequest([]contracts.Message{messages.UserText("database permissions")}, "sonnet")
	if err != nil {
		t.Fatal(err)
	}
	if len(request.Messages) != 2 {
		t.Fatalf("messages = %#v", request.Messages)
	}
	if got := request.Messages[0].Content[0].Text; got != "database permissions" {
		t.Fatalf("user message = %q", got)
	}
	memoryText := request.Messages[1].Content[0].Text
	if !strings.HasPrefix(memoryText, "<system-reminder>\nMemory (saved today): ") || !strings.Contains(memoryText, "/db.md:") || !strings.Contains(memoryText, "remember database permission rules\n\n</system-reminder>") {
		t.Fatalf("memory text = %q", memoryText)
	}

	request, err = runner.BuildRequest([]contracts.Message{messages.UserText("database")}, "sonnet")
	if err != nil {
		t.Fatal(err)
	}
	if len(request.Messages) != 1 {
		t.Fatalf("single-word messages = %#v", request.Messages)
	}
}

func TestRunnerPrefetchesRelevantMemoryIntoFirstRequest(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "db.md")
	if err := os.WriteFile(path, []byte("---\ndescription: database permissions migration\n---\nremember database permission rules\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	now := time.Now()
	if err := os.Chtimes(path, now, now); err != nil {
		t.Fatal(err)
	}
	client := &fakeClient{calls: []fakeCall{
		{response: &anthropic.Response{
			ID:         "msg_done",
			Type:       "message",
			Role:       "assistant",
			Model:      "sonnet",
			StopReason: "end_turn",
			Content:    []contracts.ContentBlock{contracts.NewTextBlock("done")},
		}},
	}}
	runner := Runner{
		Client:            client,
		Model:             "sonnet",
		MaxTokens:         128,
		RelevantMemoryDir: dir,
	}

	if _, err := runner.RunTurn(context.Background(), nil, messages.UserText("database permissions")); err != nil {
		t.Fatal(err)
	}
	if len(client.requests) != 1 {
		t.Fatalf("requests = %d", len(client.requests))
	}
	request := client.requests[0]
	if len(request.Messages) != 2 {
		t.Fatalf("messages = %#v", request.Messages)
	}
	if got := request.Messages[0].Content[0].Text; got != "database permissions" {
		t.Fatalf("user message = %q", got)
	}
	if memoryText := request.Messages[1].Content[0].Text; !strings.Contains(memoryText, "/db.md:") || !strings.Contains(memoryText, "remember database permission rules") {
		t.Fatalf("memory text = %q", memoryText)
	}
}

func TestRunnerRelevantMemoryPrefetchUsesMemoryAgentSelector(t *testing.T) {
	dir := t.TempDir()
	dbPath := filepath.Join(dir, "db.md")
	modelPath := filepath.Join(dir, "model.md")
	if err := os.WriteFile(dbPath, []byte("---\ndescription: database permissions migration\n---\ndeterministic memory\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(modelPath, []byte("---\ndescription: model selected memory\n---\nmodel selected memory\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	mainClient := &fakeClient{calls: []fakeCall{
		{response: &anthropic.Response{
			ID:         "msg_done",
			Type:       "message",
			Role:       "assistant",
			Model:      "sonnet",
			StopReason: "end_turn",
			Content:    []contracts.ContentBlock{contracts.NewTextBlock("done")},
		}},
	}}
	memoryClient := &fakeClient{calls: []fakeCall{
		{response: &anthropic.Response{
			ID:      "msg_memory_select",
			Type:    "message",
			Role:    "assistant",
			Model:   "sonnet",
			Content: []contracts.ContentBlock{contracts.NewTextBlock(`{"memory_paths":["model.md"]}`)},
		}},
	}}
	runner := Runner{
		Client:            mainClient,
		Model:             "sonnet",
		MaxTokens:         128,
		RelevantMemoryDir: dir,
		MemoryAgentClient: memoryClient,
	}

	if _, err := runner.RunTurn(context.Background(), nil, messages.UserText("database permissions")); err != nil {
		t.Fatal(err)
	}
	if len(memoryClient.requests) != 1 || !strings.Contains(memoryClient.requests[0].Messages[0].Content[0].Text, "Candidate memory files") {
		t.Fatalf("memory selector requests = %#v", memoryClient.requests)
	}
	if len(mainClient.requests) != 1 || len(mainClient.requests[0].Messages) != 2 {
		t.Fatalf("main request = %#v", mainClient.requests)
	}
	memoryText := mainClient.requests[0].Messages[1].Content[0].Text
	if !strings.Contains(memoryText, "/model.md:") || !strings.Contains(memoryText, "model selected memory") || strings.Contains(memoryText, "deterministic memory") {
		t.Fatalf("memory text = %q", memoryText)
	}
}

func TestRunnerRelevantMemoryPrefetchFailsOpen(t *testing.T) {
	client := &fakeClient{calls: []fakeCall{
		{response: &anthropic.Response{
			ID:         "msg_done",
			Type:       "message",
			Role:       "assistant",
			Model:      "sonnet",
			StopReason: "end_turn",
			Content:    []contracts.ContentBlock{contracts.NewTextBlock("done")},
		}},
	}}
	runner := Runner{
		Client:            client,
		Model:             "sonnet",
		MaxTokens:         128,
		RelevantMemoryDir: "\x00",
	}

	if _, err := runner.RunTurn(context.Background(), nil, messages.UserText("database permissions")); err != nil {
		t.Fatal(err)
	}
	if len(client.requests) != 1 {
		t.Fatalf("requests = %d", len(client.requests))
	}
	if len(client.requests[0].Messages) != 1 || client.requests[0].Messages[0].Content[0].Text != "database permissions" {
		t.Fatalf("messages = %#v", client.requests[0].Messages)
	}
}

func TestRunnerPassesRelevantMemoryDirToFileTools(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "old.md")
	if err := os.WriteFile(path, []byte("---\ndescription: stale memory\n---\nold memory fact\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	mtime := time.Now().Add(-3 * 24 * time.Hour)
	if err := os.Chtimes(path, mtime, mtime); err != nil {
		t.Fatal(err)
	}
	registry, err := tool.NewRegistry(filetools.BuiltinTools()...)
	if err != nil {
		t.Fatal(err)
	}
	client := &fakeClient{calls: []fakeCall{
		{response: &anthropic.Response{
			ID:         "msg_read",
			Type:       "message",
			Role:       "assistant",
			Model:      "sonnet",
			StopReason: "tool_use",
			Content: []contracts.ContentBlock{{
				Type:  contracts.ContentToolUse,
				ID:    "toolu_read_memory",
				Name:  "Read",
				Input: json.RawMessage(`{"file_path":` + strconv.Quote(path) + `}`),
			}},
		}},
		{response: &anthropic.Response{
			ID:         "msg_done",
			Type:       "message",
			Role:       "assistant",
			Model:      "sonnet",
			StopReason: "end_turn",
			Content:    []contracts.ContentBlock{contracts.NewTextBlock("done")},
		}},
	}}
	runner := Runner{
		Client:            client,
		Tools:             tool.NewExecutor(registry),
		Model:             "sonnet",
		MaxTokens:         128,
		RelevantMemoryDir: dir,
	}
	result, err := runner.RunTurn(context.Background(), nil, messages.UserText("read stale memory"))
	if err != nil {
		t.Fatal(err)
	}
	if len(result.ToolResults) != 1 {
		t.Fatalf("tool results = %#v", result.ToolResults)
	}
	content := result.ToolResults[0].Content.(string)
	if !strings.HasPrefix(content, "<system-reminder>This memory is 3 days old.") || !strings.Contains(content, "old memory fact") {
		t.Fatalf("content = %q", content)
	}
}

func TestRunnerPassesSkillDirsToToolMetadata(t *testing.T) {
	t.Setenv("CLAUDE_CONFIG_DIR", t.TempDir())
	memoryDir := filepath.Join(t.TempDir(), "memory")
	skillDir := filepath.Join(t.TempDir(), "bundled-skill")
	runner := Runner{
		RelevantMemoryDir: memoryDir,
		SkillDirs:         []string{skillDir},
	}

	internal := tool.InternalPathContextFromMetadata(runner.toolMetadata())
	if internal.AutoMemoryDir != memoryDir {
		t.Fatalf("auto memory dir = %q, want %q", internal.AutoMemoryDir, memoryDir)
	}
	if len(internal.SkillDirs) != 1 || internal.SkillDirs[0] != skillDir {
		t.Fatalf("skill dirs = %#v", internal.SkillDirs)
	}
	internal.SkillDirs[0] = "mutated"
	again := tool.InternalPathContextFromMetadata(runner.toolMetadata())
	if again.SkillDirs[0] != skillDir {
		t.Fatalf("skill dirs should be copied from runner: %#v", again.SkillDirs)
	}
}

func TestRunnerDiscoversUserSkillDirsForToolMetadata(t *testing.T) {
	configHome := t.TempDir()
	t.Setenv("CLAUDE_CONFIG_DIR", configHome)
	userSkill := filepath.Join(configHome, "skills", "personal")
	if err := os.MkdirAll(userSkill, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(userSkill, "SKILL.md"), []byte("---\ndescription: test\n---\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	runner := Runner{}
	internal := tool.InternalPathContextFromMetadata(runner.toolMetadata())
	if len(internal.SkillDirs) != 1 || internal.SkillDirs[0] != userSkill {
		t.Fatalf("skill dirs = %#v, want %#v", internal.SkillDirs, []string{userSkill})
	}
}

func TestRunnerDiscoversLegacyCommandSkillDirsForToolMetadata(t *testing.T) {
	configHome := t.TempDir()
	t.Setenv("CLAUDE_CONFIG_DIR", configHome)
	userCommandSkill := filepath.Join(configHome, "commands", "personal")
	if err := os.MkdirAll(userCommandSkill, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(userCommandSkill, "SKILL.md"), []byte("---\ndescription: test\n---\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	repo := filepath.Join(t.TempDir(), "repo")
	cwd := filepath.Join(repo, "pkg")
	projectCommandSkill := filepath.Join(cwd, ".claude", "commands", "team", "deploy")
	if err := os.MkdirAll(projectCommandSkill, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(projectCommandSkill, "SKILL.md"), []byte("---\ndescription: test\n---\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Join(repo, ".git"), 0o755); err != nil {
		t.Fatal(err)
	}

	runner := Runner{WorkingDirectory: cwd}
	internal := tool.InternalPathContextFromMetadata(runner.toolMetadata())
	want := []string{userCommandSkill, projectCommandSkill}
	if len(internal.SkillDirs) != len(want) {
		t.Fatalf("skill dirs = %#v, want %#v", internal.SkillDirs, want)
	}
	for i := range want {
		if internal.SkillDirs[i] != want[i] {
			t.Fatalf("skill dirs = %#v, want %#v", internal.SkillDirs, want)
		}
	}
}

func TestRunnerDiscoversProjectSkillDirsForToolMetadata(t *testing.T) {
	t.Setenv("CLAUDE_CONFIG_DIR", t.TempDir())
	repo := filepath.Join(t.TempDir(), "repo")
	cwd := filepath.Join(repo, "pkg")
	rootSkill := filepath.Join(repo, ".claude", "skills", "root")
	nestedSkill := filepath.Join(cwd, ".claude", "skills", "nested")
	for _, dir := range []string{rootSkill, nestedSkill} {
		if err := os.MkdirAll(dir, 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(dir, "SKILL.md"), []byte("---\ndescription: test\n---\n"), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	if err := os.MkdirAll(filepath.Join(repo, ".git"), 0o755); err != nil {
		t.Fatal(err)
	}

	runner := Runner{WorkingDirectory: cwd}
	internal := tool.InternalPathContextFromMetadata(runner.toolMetadata())
	want := []string{nestedSkill, rootSkill}
	if len(internal.SkillDirs) != len(want) {
		t.Fatalf("skill dirs = %#v, want %#v", internal.SkillDirs, want)
	}
	for i := range want {
		if internal.SkillDirs[i] != want[i] {
			t.Fatalf("skill dirs = %#v, want %#v", internal.SkillDirs, want)
		}
	}
}

func TestRunnerExtractsSessionMemoryAfterTurn(t *testing.T) {
	root := filepath.Join(t.TempDir(), "session-memory")
	client := &fakeClient{calls: []fakeCall{
		{response: &anthropic.Response{
			ID:         "msg_done",
			Type:       "message",
			Role:       "assistant",
			Model:      "sonnet",
			StopReason: "end_turn",
			Content:    []contracts.ContentBlock{contracts.NewTextBlock("done")},
		}},
	}}
	runner := Runner{
		Client:                 client,
		Model:                  "sonnet",
		MaxTokens:              128,
		SessionID:              "extract_session",
		SessionMemoryRoot:      root,
		EnableMemoryExtraction: true,
		MemoryExtractLimit:     4,
	}
	if _, err := runner.RunTurn(context.Background(), nil, messages.UserText("Remember use brief summaries")); err != nil {
		t.Fatal(err)
	}
	summary, err := memory.LoadSessionSummary(filepath.Join(root, "extract_session", memory.SessionSummaryFilename))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(summary.Summary, "Extracted session memory") || !strings.Contains(summary.Summary, "use brief summaries") {
		t.Fatalf("summary = %#v", summary)
	}
}

func TestRunnerFallsBackOnRetryableAPIError(t *testing.T) {
	client := &fakeClient{calls: []fakeCall{
		{err: anthropic.APIError{StatusCode: http.StatusInternalServerError, Type: "overloaded_error", Message: "try later"}},
		{response: &anthropic.Response{
			ID:         "msg_1",
			Type:       "message",
			Role:       "assistant",
			Model:      "haiku",
			StopReason: "end_turn",
			Content:    []contracts.ContentBlock{contracts.NewTextBlock("fallback ok")},
		}},
	}}
	var retryEvents []RetryInfo
	runner := Runner{
		Client:         client,
		Model:          "sonnet",
		FallbackModels: []string{"haiku"},
		MaxTokens:      64,
		OnEvent: func(event Event) {
			if event.Type == EventRetry && event.Retry != nil {
				retryEvents = append(retryEvents, *event.Retry)
			}
		},
	}
	result, err := runner.RunTurn(context.Background(), nil, messages.UserText("hello"))
	if err != nil {
		t.Fatal(err)
	}
	if result.Assistant.Model != "haiku" {
		t.Fatalf("assistant model = %q", result.Assistant.Model)
	}
	if len(client.requests) != 2 || client.requests[0].Model != "sonnet" || client.requests[1].Model != "haiku" {
		t.Fatalf("requests = %#v", client.requests)
	}
	if len(retryEvents) != 1 {
		t.Fatalf("retry events = %#v", retryEvents)
	}
	retry := retryEvents[0]
	if retry.Attempt != 1 || retry.MaxAttempts != 2 || retry.FailedModel != "sonnet" || retry.NextModel != "haiku" || !retry.Fallback {
		t.Fatalf("retry event = %#v", retry)
	}
}

func containsEvent(events []EventType, target EventType) bool {
	for _, event := range events {
		if event == target {
			return true
		}
	}
	return false
}

func hasToolProgress(progress []contracts.ToolProgress, toolUseID contracts.ID, progressType string, taskID string, status string) bool {
	for _, item := range progress {
		if item.ToolUseID != toolUseID || item.Type != progressType {
			continue
		}
		if item.Data["task_id"] == taskID && item.Data["status"] == status {
			return true
		}
	}
	return false
}

func hasScheduleDueTickProgress(progress []contracts.ToolProgress) bool {
	for _, item := range progress {
		if item.ToolUseID != "schedule_due_tick" || item.Type != "schedule_due_run" {
			continue
		}
		if item.Data["due_count"] == 1 && item.Data["triggered_count"] == 1 && item.Data["error_count"] == 0 {
			return true
		}
	}
	return false
}

func hasHookProgress(progress []contracts.ToolProgress, phase string, progressType string) bool {
	for _, item := range progress {
		if item.Type == progressType && item.Data["phase"] == phase {
			return true
		}
	}
	return false
}

func initConversationGitRepo(t *testing.T) string {
	t.Helper()
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git is required for worktree tests")
	}
	repo := filepath.Join(t.TempDir(), "repo")
	if err := os.MkdirAll(repo, 0o755); err != nil {
		t.Fatal(err)
	}
	runConversationGitTest(t, repo, "init")
	if err := os.WriteFile(filepath.Join(repo, "README.md"), []byte("hello\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	runConversationGitTest(t, repo, "add", "README.md")
	runConversationGitTest(t, repo, "-c", "user.name=Test User", "-c", "user.email=test@example.com", "commit", "-m", "init")
	return repo
}

func runConversationGitTest(t *testing.T, dir string, args ...string) {
	t.Helper()
	cmd := exec.Command("git", args...)
	cmd.Dir = dir
	output, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("git %s: %v\n%s", strings.Join(args, " "), err, string(output))
	}
}

func namedTextTool(name string, prefix string) tool.Tool {
	return tool.FuncTool{
		DefinitionValue: contracts.ToolDefinition{
			Name:            name,
			Description:     name + " text",
			ReadOnly:        true,
			ConcurrencySafe: true,
			InputSchema: contracts.JSONSchema{
				"type":       "object",
				"properties": map[string]any{"text": map[string]any{"type": "string"}},
			},
		},
		CallFunc: func(ctx tool.Context, raw json.RawMessage, sink tool.ProgressSink) (contracts.ToolResult, error) {
			var input struct {
				Text string `json:"text"`
			}
			if err := json.Unmarshal(raw, &input); err != nil {
				return contracts.ToolResult{}, err
			}
			return contracts.ToolResult{Content: prefix + input.Text}, nil
		},
	}
}

func requestHasTool(request anthropic.Request, name string) bool {
	for _, definition := range request.Tools {
		if definition.Name == name {
			return true
		}
	}
	return false
}

func allowToolSearchForTest(t *testing.T) {
	t.Helper()
	t.Setenv("ANTHROPIC_BASE_URL", "")
	t.Setenv("ENABLE_TOOL_SEARCH", "true")
	t.Setenv("CLAUDE_CODE_DISABLE_EXPERIMENTAL_BETAS", "")
}

func testDeferredToolDefinition(name string, description string) tool.FuncTool {
	return tool.FuncTool{DefinitionValue: contracts.ToolDefinition{
		Name:        name,
		Description: description,
		ShouldDefer: true,
		InputSchema: contracts.JSONSchema{
			"type":       "object",
			"properties": map[string]any{"value": map[string]any{"type": "string"}},
		},
	}}
}

func requestTool(request anthropic.Request, name string) anthropic.ToolDefinition {
	for _, definition := range request.Tools {
		if definition.Name == name {
			return definition
		}
	}
	return anthropic.ToolDefinition{}
}

func bridgeManifestHasCommand(manifest bridgepkg.Manifest, name string) bool {
	for _, command := range manifest.Commands {
		if command.Name == name {
			return true
		}
	}
	return false
}

func bridgeManifestHasCapability(manifest bridgepkg.Manifest, name string) bool {
	for _, capability := range manifest.Capabilities {
		if capability.Name == name {
			return true
		}
	}
	return false
}

func integrationHasState(integrations []integrationspkg.Integration, name string, enabled bool, state string) bool {
	for _, integration := range integrations {
		if integration.Name == name && integration.Enabled == enabled && integration.RuntimeState == state {
			return true
		}
	}
	return false
}

func nativeCapabilityAvailable(capabilities []nativepkg.Capability, name string, available bool) bool {
	for _, capability := range capabilities {
		if capability.Name == name && capability.Available == available {
			return true
		}
	}
	return false
}

func nativeIndexHasPath(files []nativepkg.FileEntry, path string) bool {
	for _, file := range files {
		if file.Path == path {
			return true
		}
	}
	return false
}

func lspServerStatus(servers []lsppkg.ServerStatus, name string) lsppkg.ServerStatus {
	for _, server := range servers {
		if server.Name == name {
			return server
		}
	}
	return lsppkg.ServerStatus{}
}

func containsAnyString(values []any, want string) bool {
	for _, value := range values {
		if value == want {
			return true
		}
	}
	return false
}

func TestBuildRequestIncludesToolDefinitions(t *testing.T) {
	registry, err := tool.NewRegistry(tool.FuncTool{
		DefinitionValue: contracts.ToolDefinition{
			Name:        "Read",
			Description: "read a file",
			ReadOnly:    true,
			InputSchema: contracts.JSONSchema{"type": "object"},
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	runner := Runner{
		Tools:     tool.NewExecutor(registry),
		Model:     "sonnet",
		MaxTokens: 100,
	}
	req, err := runner.BuildRequest([]contracts.Message{messages.UserText("hi")}, "sonnet")
	if err != nil {
		t.Fatal(err)
	}
	if len(req.Tools) != 1 || req.Tools[0].Name != "Read" || req.Tools[0].InputSchema["type"] != "object" {
		t.Fatalf("tools = %#v", req.Tools)
	}
}

func TestBuildRequestPreservesDeferredToolMetadata(t *testing.T) {
	registry, err := tool.NewRegistry(tasktools.NewTaskTool())
	if err != nil {
		t.Fatal(err)
	}
	runner := Runner{
		Tools:     tool.NewExecutor(registry),
		Model:     "sonnet",
		MaxTokens: 100,
	}
	req, err := runner.BuildRequest([]contracts.Message{messages.UserText("hi")}, "sonnet")
	if err != nil {
		t.Fatal(err)
	}
	if len(req.Tools) != 1 || req.Tools[0].Name != "Task" {
		t.Fatalf("tools = %#v", req.Tools)
	}
	if !req.Tools[0].DeferLoading {
		t.Fatalf("task defer_loading = false, want true")
	}
	if !req.Tools[0].Strict {
		t.Fatalf("task strict = false, want true")
	}
}

func TestBuildRequestWithToolSearchOmitsUndiscoveredDeferredTools(t *testing.T) {
	allowToolSearchForTest(t)
	registry, err := tool.NewRegistry(tasktools.NewTaskTool(), searchtools.NewToolSearchTool())
	if err != nil {
		t.Fatal(err)
	}
	runner := Runner{
		Tools:     tool.NewExecutor(registry),
		Model:     "sonnet",
		MaxTokens: 100,
	}
	req, err := runner.BuildRequest([]contracts.Message{messages.UserText("hi")}, "sonnet")
	if err != nil {
		t.Fatal(err)
	}
	if len(req.Tools) != 1 || req.Tools[0].Name != "ToolSearch" {
		t.Fatalf("tools = %#v", req.Tools)
	}
	if len(req.Messages) != 2 {
		t.Fatalf("messages = %#v", req.Messages)
	}
	deferred := req.Messages[0]
	if deferred.Role != "user" || !strings.Contains(deferred.Content[0].Text, "<available-deferred-tools>\nTask\n</available-deferred-tools>") {
		t.Fatalf("deferred tools message = %#v", deferred)
	}
	if got := req.Messages[1].Content[0].Text; got != "hi" {
		t.Fatalf("original user message = %q", got)
	}
}

func TestBuildRequestWithToolSearchLoadsDiscoveredDeferredTools(t *testing.T) {
	allowToolSearchForTest(t)
	registry, err := tool.NewRegistry(tasktools.NewTaskTool(), searchtools.NewToolSearchTool())
	if err != nil {
		t.Fatal(err)
	}
	runner := Runner{
		Tools:     tool.NewExecutor(registry),
		Model:     "sonnet",
		MaxTokens: 100,
	}
	history := []contracts.Message{
		messages.UserText("find task"),
		{
			Type: contracts.MessageUser,
			Content: []contracts.ContentBlock{{
				Type:      contracts.ContentToolResult,
				ToolUseID: "toolu_search",
				Content:   []contracts.ToolReference{contracts.NewToolReference("Task")},
			}},
		},
	}
	req, err := runner.BuildRequest(history, "sonnet")
	if err != nil {
		t.Fatal(err)
	}
	if !requestHasTool(req, "ToolSearch") || !requestHasTool(req, "Task") || len(req.Tools) != 2 {
		t.Fatalf("tools = %#v", req.Tools)
	}
	taskTool := requestTool(req, "Task")
	if taskTool.DeferLoading {
		t.Fatalf("discovered task defer_loading = true")
	}
	if len(req.Messages) < 1 || !strings.Contains(req.Messages[0].Content[0].Text, "<available-deferred-tools>\nTask\n</available-deferred-tools>") {
		t.Fatalf("messages = %#v", req.Messages)
	}
}

func TestBuildRequestWithToolSearchButNoDeferredToolsOmitsToolSearch(t *testing.T) {
	allowToolSearchForTest(t)
	registry, err := tool.NewRegistry(
		tool.FuncTool{DefinitionValue: contracts.ToolDefinition{
			Name:        "Read",
			Description: "read a file",
			ReadOnly:    true,
			InputSchema: contracts.JSONSchema{"type": "object"},
		}},
		searchtools.NewToolSearchTool(),
	)
	if err != nil {
		t.Fatal(err)
	}
	runner := Runner{
		Tools:     tool.NewExecutor(registry),
		Model:     "sonnet",
		MaxTokens: 100,
	}
	req, err := runner.BuildRequest([]contracts.Message{messages.UserText("hi")}, "sonnet")
	if err != nil {
		t.Fatal(err)
	}
	if len(req.Tools) != 1 || req.Tools[0].Name != "Read" {
		t.Fatalf("tools = %#v", req.Tools)
	}
	if len(req.Messages) != 1 || req.Messages[0].Content[0].Text != "hi" {
		t.Fatalf("messages = %#v", req.Messages)
	}
}

func TestBuildRequestWithToolSearchDisabledForHaikuLoadsDeferredTools(t *testing.T) {
	allowToolSearchForTest(t)
	registry, err := tool.NewRegistry(tasktools.NewTaskTool(), searchtools.NewToolSearchTool())
	if err != nil {
		t.Fatal(err)
	}
	runner := Runner{
		Tools:     tool.NewExecutor(registry),
		Model:     "claude-haiku-4-5-20251001",
		MaxTokens: 100,
	}
	req, err := runner.BuildRequest([]contracts.Message{messages.UserText("hi")}, "claude-haiku-4-5-20251001")
	if err != nil {
		t.Fatal(err)
	}
	if len(req.Tools) != 1 || req.Tools[0].Name != "Task" || req.Tools[0].DeferLoading {
		t.Fatalf("tools = %#v", req.Tools)
	}
	if len(req.Messages) != 1 || req.Messages[0].Content[0].Text != "hi" {
		t.Fatalf("messages = %#v", req.Messages)
	}
}

func TestBuildRequestWithToolSearchDisabledByEnvLoadsDeferredTools(t *testing.T) {
	cases := []struct {
		name  string
		setup func(*testing.T)
	}{
		{
			name: "enable_false",
			setup: func(t *testing.T) {
				t.Setenv("ENABLE_TOOL_SEARCH", "false")
			},
		},
		{
			name: "auto_100",
			setup: func(t *testing.T) {
				t.Setenv("ENABLE_TOOL_SEARCH", "auto:100")
			},
		},
		{
			name: "disable_experimental_betas",
			setup: func(t *testing.T) {
				t.Setenv("ENABLE_TOOL_SEARCH", "true")
				t.Setenv("CLAUDE_CODE_DISABLE_EXPERIMENTAL_BETAS", "true")
			},
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Setenv("ANTHROPIC_BASE_URL", "")
			t.Setenv("CLAUDE_CODE_DISABLE_EXPERIMENTAL_BETAS", "")
			tc.setup(t)
			registry, err := tool.NewRegistry(tasktools.NewTaskTool(), searchtools.NewToolSearchTool())
			if err != nil {
				t.Fatal(err)
			}
			runner := Runner{
				Tools:     tool.NewExecutor(registry),
				Model:     "sonnet",
				MaxTokens: 100,
			}
			req, err := runner.BuildRequest([]contracts.Message{messages.UserText("hi")}, "sonnet")
			if err != nil {
				t.Fatal(err)
			}
			if len(req.Tools) != 1 || req.Tools[0].Name != "Task" || req.Tools[0].DeferLoading {
				t.Fatalf("tools = %#v", req.Tools)
			}
			if len(req.Messages) != 1 || req.Messages[0].Content[0].Text != "hi" {
				t.Fatalf("messages = %#v", req.Messages)
			}
		})
	}
}

func TestBuildRequestWithToolSearchAutoBelowThresholdLoadsDeferredTools(t *testing.T) {
	t.Setenv("ANTHROPIC_BASE_URL", "")
	t.Setenv("ENABLE_TOOL_SEARCH", "auto")
	t.Setenv("CLAUDE_CODE_DISABLE_EXPERIMENTAL_BETAS", "")
	t.Setenv("USER_TYPE", "")
	t.Setenv("CLAUDE_CODE_MAX_CONTEXT_TOKENS", "")
	registry, err := tool.NewRegistry(testDeferredToolDefinition("TinyDeferred", "small"), searchtools.NewToolSearchTool())
	if err != nil {
		t.Fatal(err)
	}
	runner := Runner{
		Tools:     tool.NewExecutor(registry),
		Model:     "sonnet",
		MaxTokens: 100,
	}
	req, err := runner.BuildRequest([]contracts.Message{messages.UserText("hi")}, "sonnet")
	if err != nil {
		t.Fatal(err)
	}
	if len(req.Tools) != 1 || req.Tools[0].Name != "TinyDeferred" || req.Tools[0].DeferLoading {
		t.Fatalf("tools = %#v", req.Tools)
	}
	if len(req.Messages) != 1 || strings.HasPrefix(req.Messages[0].Content[0].Text, "<available-deferred-tools>") {
		t.Fatalf("messages = %#v", req.Messages)
	}
}

func TestBuildRequestWithToolSearchAutoAboveThresholdOmitsUndiscoveredDeferredTools(t *testing.T) {
	t.Setenv("ANTHROPIC_BASE_URL", "")
	t.Setenv("ENABLE_TOOL_SEARCH", "auto:10suffix")
	t.Setenv("CLAUDE_CODE_DISABLE_EXPERIMENTAL_BETAS", "")
	t.Setenv("USER_TYPE", "")
	t.Setenv("CLAUDE_CODE_MAX_CONTEXT_TOKENS", "")
	registry, err := tool.NewRegistry(testDeferredToolDefinition("LargeDeferred", strings.Repeat("large ", 12000)), searchtools.NewToolSearchTool())
	if err != nil {
		t.Fatal(err)
	}
	runner := Runner{
		Tools:     tool.NewExecutor(registry),
		Model:     "sonnet",
		MaxTokens: 100,
	}
	req, err := runner.BuildRequest([]contracts.Message{messages.UserText("hi")}, "sonnet")
	if err != nil {
		t.Fatal(err)
	}
	if len(req.Tools) != 1 || req.Tools[0].Name != "ToolSearch" {
		t.Fatalf("tools = %#v", req.Tools)
	}
	if len(req.Messages) != 2 || !strings.Contains(req.Messages[0].Content[0].Text, "<available-deferred-tools>\nLargeDeferred\n</available-deferred-tools>") {
		t.Fatalf("messages = %#v", req.Messages)
	}
}

func TestBuildRequestWithDeferredToolsDeltaAttachment(t *testing.T) {
	resetDeferredToolTokenCountCache()
	t.Cleanup(resetDeferredToolTokenCountCache)
	t.Setenv("ANTHROPIC_BASE_URL", "")
	t.Setenv("ENABLE_TOOL_SEARCH", "auto:10")
	t.Setenv("CLAUDE_CODE_DISABLE_EXPERIMENTAL_BETAS", "")
	t.Setenv("USER_TYPE", "ant")
	t.Setenv("CLAUDE_CODE_MAX_CONTEXT_TOKENS", "10")
	registry, err := tool.NewRegistry(testDeferredToolDefinition("TinyDeferred", "small"), searchtools.NewToolSearchTool())
	if err != nil {
		t.Fatal(err)
	}
	runner := Runner{
		Tools:     tool.NewExecutor(registry),
		Model:     "sonnet",
		MaxTokens: 100,
	}
	history := []contracts.Message{
		deferredToolsDeltaMessage(deferredToolsDelta{
			AddedNames: []string{"TinyDeferred"},
			AddedLines: []string{"TinyDeferred"},
		}),
		messages.UserText("hi"),
	}
	req, err := runner.BuildRequest(history, "sonnet")
	if err != nil {
		t.Fatal(err)
	}
	if len(req.Tools) != 1 || req.Tools[0].Name != "ToolSearch" {
		t.Fatalf("tools = %#v", req.Tools)
	}
	if len(req.Messages) != 2 {
		t.Fatalf("messages = %#v", req.Messages)
	}
	if strings.Contains(req.Messages[0].Content[0].Text, "<available-deferred-tools>") ||
		!strings.Contains(req.Messages[0].Content[0].Text, "The following deferred tools are now available via ToolSearch:\nTinyDeferred") {
		t.Fatalf("delta message = %#v", req.Messages[0])
	}
	if req.Messages[1].Content[0].Text != "hi" {
		t.Fatalf("user message = %#v", req.Messages[1])
	}
}

func TestRunnerToolSearchAutoUsesCountTokensWhenAvailable(t *testing.T) {
	resetDeferredToolTokenCountCache()
	t.Cleanup(resetDeferredToolTokenCountCache)
	t.Setenv("ANTHROPIC_BASE_URL", "")
	t.Setenv("ENABLE_TOOL_SEARCH", "auto")
	t.Setenv("CLAUDE_CODE_DISABLE_EXPERIMENTAL_BETAS", "")
	t.Setenv("USER_TYPE", "")
	t.Setenv("CLAUDE_CODE_MAX_CONTEXT_TOKENS", "")
	registry, err := tool.NewRegistry(testDeferredToolDefinition("TinyDeferred", "small"), searchtools.NewToolSearchTool())
	if err != nil {
		t.Fatal(err)
	}
	client := &fakeClient{
		countTokens: []fakeCountTokensCall{{response: &anthropic.CountTokensResponse{InputTokens: 20501}}},
		calls: []fakeCall{{response: &anthropic.Response{
			ID:         "msg_done",
			Type:       "message",
			Role:       "assistant",
			Model:      "sonnet",
			StopReason: "end_turn",
			Content:    []contracts.ContentBlock{contracts.NewTextBlock("done")},
		}}},
	}
	runner := Runner{
		Client:    client,
		Tools:     tool.NewExecutor(registry),
		Model:     "sonnet",
		MaxTokens: 100,
	}
	_, err = runner.RunTurn(context.Background(), nil, messages.UserText("hi"))
	if err != nil {
		t.Fatal(err)
	}
	if len(client.countCalls) != 1 {
		t.Fatalf("count token calls = %#v", client.countCalls)
	}
	countRequest := client.countCalls[0]
	if len(countRequest.Tools) != 1 || countRequest.Tools[0].Name != "TinyDeferred" || countRequest.Tools[0].DeferLoading {
		t.Fatalf("count token tools = %#v", countRequest.Tools)
	}
	if len(client.requests) != 1 || len(client.requests[0].Tools) != 1 || client.requests[0].Tools[0].Name != "ToolSearch" {
		t.Fatalf("request tools = %#v", client.requests)
	}
	if len(client.requests[0].Messages) != 2 || !strings.Contains(client.requests[0].Messages[0].Content[0].Text, "<available-deferred-tools>\nTinyDeferred\n</available-deferred-tools>") {
		t.Fatalf("request messages = %#v", client.requests[0].Messages)
	}
}

func TestRunnerRecordsToolSearchModeDecisionTelemetry(t *testing.T) {
	resetDeferredToolTokenCountCache()
	t.Cleanup(resetDeferredToolTokenCountCache)
	t.Setenv("ANTHROPIC_BASE_URL", "")
	t.Setenv("ENABLE_TOOL_SEARCH", "auto")
	t.Setenv("CLAUDE_CODE_DISABLE_EXPERIMENTAL_BETAS", "")
	t.Setenv("USER_TYPE", "")
	t.Setenv("CLAUDE_CODE_MAX_CONTEXT_TOKENS", "")
	registry, err := tool.NewRegistry(testDeferredToolDefinition("TinyDeferred", "small"), searchtools.NewToolSearchTool())
	if err != nil {
		t.Fatal(err)
	}
	client := &fakeClient{
		countTokens: []fakeCountTokensCall{{response: &anthropic.CountTokensResponse{InputTokens: 20501}}},
		calls: []fakeCall{{response: &anthropic.Response{
			ID:         "msg_done",
			Type:       "message",
			Role:       "assistant",
			Model:      "sonnet",
			StopReason: "end_turn",
			Content:    []contracts.ContentBlock{contracts.NewTextBlock("done")},
		}}},
	}
	dir := t.TempDir()
	transcriptPath := filepath.Join(dir, "session.jsonl")
	telemetryEnabled := true
	runner := Runner{
		Client:      client,
		Tools:       tool.NewExecutor(registry),
		Model:       "sonnet",
		MaxTokens:   100,
		SessionID:   "sess_tool_search_mode",
		SessionPath: transcriptPath,
		MCP: &MCPConfig{UserSettings: contracts.Settings{
			Advanced: &contracts.AdvancedSetting{Telemetry: &telemetryEnabled},
		}},
	}
	if _, err := runner.RunTurn(context.Background(), nil, messages.UserText("hi")); err != nil {
		t.Fatal(err)
	}
	telemetryPath := telemetrypkg.SessionPath(transcriptPath, "sess_tool_search_mode")
	telemetryData, err := os.ReadFile(telemetryPath)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(telemetryData), "TinyDeferred") {
		t.Fatalf("telemetry leaked deferred tool name: %q", telemetryData)
	}
	events, err := telemetrypkg.Load(telemetryPath)
	if err != nil {
		t.Fatal(err)
	}
	var decision *telemetrypkg.Event
	for i := range events {
		if events[i].Type == string(EventToolSearchDecision) {
			decision = &events[i]
			break
		}
	}
	if decision == nil {
		t.Fatalf("tool-search mode telemetry event not found: %#v", events)
	}
	if decision.ToolSearchEnabled == nil || !*decision.ToolSearchEnabled ||
		decision.ToolSearchMode != string(toolSearchModeTSTAuto) ||
		decision.ToolSearchReason != "auto_above_threshold" ||
		decision.ToolSearchCheckedModel != "sonnet" ||
		decision.ToolSearchUserType != "external" ||
		decision.ToolSearchDeferredToolTokens != 20001 ||
		decision.ToolSearchThreshold != 20000 {
		t.Fatalf("tool-search mode telemetry = %#v", decision)
	}
}

func TestRunnerToolSearchAutoFallsBackToHaikuTokenCount(t *testing.T) {
	resetDeferredToolTokenCountCache()
	t.Cleanup(resetDeferredToolTokenCountCache)
	t.Setenv("ANTHROPIC_BASE_URL", "")
	t.Setenv("ENABLE_TOOL_SEARCH", "auto")
	t.Setenv("CLAUDE_CODE_DISABLE_EXPERIMENTAL_BETAS", "")
	t.Setenv("USER_TYPE", "")
	t.Setenv("CLAUDE_CODE_MAX_CONTEXT_TOKENS", "")
	registry, err := tool.NewRegistry(testDeferredToolDefinition("TinyDeferred", "small"), searchtools.NewToolSearchTool())
	if err != nil {
		t.Fatal(err)
	}
	client := &fakeClient{
		countTokens: []fakeCountTokensCall{{err: fmt.Errorf("count unavailable")}},
		calls: []fakeCall{
			{response: &anthropic.Response{
				ID:    "msg_count",
				Type:  "message",
				Role:  "assistant",
				Model: modelpkg.Claude45Haiku,
				Usage: contracts.Usage{InputTokens: 20501},
			}},
			{response: &anthropic.Response{
				ID:         "msg_done",
				Type:       "message",
				Role:       "assistant",
				Model:      "sonnet",
				StopReason: "end_turn",
				Content:    []contracts.ContentBlock{contracts.NewTextBlock("done")},
			}},
		},
	}
	runner := Runner{
		Client:    client,
		Tools:     tool.NewExecutor(registry),
		Model:     "sonnet",
		MaxTokens: 100,
	}
	_, err = runner.RunTurn(context.Background(), nil, messages.UserText("hi"))
	if err != nil {
		t.Fatal(err)
	}
	if len(client.countCalls) != 1 {
		t.Fatalf("count token calls = %#v", client.countCalls)
	}
	if len(client.requests) != 2 {
		t.Fatalf("requests = %#v", client.requests)
	}
	fallbackRequest := client.requests[0]
	if fallbackRequest.Model != modelpkg.Claude45Haiku || fallbackRequest.MaxTokens != 1 {
		t.Fatalf("fallback request = %#v", fallbackRequest)
	}
	if len(fallbackRequest.Messages) != 1 || fallbackRequest.Messages[0].Content[0].Text != "count" {
		t.Fatalf("fallback messages = %#v", fallbackRequest.Messages)
	}
	if len(fallbackRequest.Tools) != 1 || fallbackRequest.Tools[0].Name != "TinyDeferred" || fallbackRequest.Tools[0].DeferLoading {
		t.Fatalf("fallback tools = %#v", fallbackRequest.Tools)
	}
	mainRequest := client.requests[1]
	if len(mainRequest.Tools) != 1 || mainRequest.Tools[0].Name != "ToolSearch" {
		t.Fatalf("main request tools = %#v", mainRequest.Tools)
	}
	if len(mainRequest.Messages) != 2 || !strings.Contains(mainRequest.Messages[0].Content[0].Text, "<available-deferred-tools>\nTinyDeferred\n</available-deferred-tools>") {
		t.Fatalf("main request messages = %#v", mainRequest.Messages)
	}
}

func TestRunnerToolSearchAutoDefersMCPTools(t *testing.T) {
	resetDeferredToolTokenCountCache()
	t.Cleanup(resetDeferredToolTokenCountCache)
	t.Setenv("ANTHROPIC_BASE_URL", "")
	t.Setenv("ENABLE_TOOL_SEARCH", "auto")
	t.Setenv("CLAUDE_CODE_DISABLE_EXPERIMENTAL_BETAS", "")
	t.Setenv("USER_TYPE", "")
	t.Setenv("CLAUDE_CODE_MAX_CONTEXT_TOKENS", "")
	registry, err := tool.NewRegistry(searchtools.NewToolSearchTool())
	if err != nil {
		t.Fatal(err)
	}
	client := &fakeClient{
		countTokens: []fakeCountTokensCall{{response: &anthropic.CountTokensResponse{InputTokens: 20501}}},
		calls: []fakeCall{{response: &anthropic.Response{
			ID:         "msg_done",
			Type:       "message",
			Role:       "assistant",
			Model:      "sonnet",
			StopReason: "end_turn",
			Content:    []contracts.ContentBlock{contracts.NewTextBlock("done")},
		}}},
	}
	mcpClient := &fakeRunnerMCPClient{tools: []mcp.RemoteTool{{
		Name:        "search",
		Description: "search issues",
		ReadOnly:    true,
	}}}
	runner := Runner{
		Client:    client,
		Tools:     tool.NewExecutor(registry),
		Model:     "sonnet",
		MaxTokens: 100,
		MCP: &MCPConfig{
			UserSettings: contracts.Settings{MCPServers: map[string]contracts.MCPServer{
				"github": {Command: "node"},
			}},
			ToolOptions: mcp.ServerToolOptions{
				DisableResources: true,
				DisablePrompts:   true,
				OpenClient: func(context.Context, string, contracts.MCPServer) (mcp.ClientHandle, error) {
					return mcp.ClientHandle{Client: mcpClient}, nil
				},
			},
		},
	}
	_, err = runner.RunTurn(context.Background(), nil, messages.UserText("hi"))
	if err != nil {
		t.Fatal(err)
	}
	if len(client.countCalls) != 1 {
		t.Fatalf("count token calls = %#v", client.countCalls)
	}
	countRequest := client.countCalls[0]
	if len(countRequest.Tools) != 1 || countRequest.Tools[0].Name != "mcp__github__search" || countRequest.Tools[0].DeferLoading {
		t.Fatalf("count token tools = %#v", countRequest.Tools)
	}
	if len(client.requests) != 1 || len(client.requests[0].Tools) != 1 || client.requests[0].Tools[0].Name != "ToolSearch" {
		t.Fatalf("request tools = %#v", client.requests)
	}
	if len(client.requests[0].Messages) != 2 || !strings.Contains(client.requests[0].Messages[0].Content[0].Text, "<available-deferred-tools>\nmcp__github__search\n</available-deferred-tools>") {
		t.Fatalf("request messages = %#v", client.requests[0].Messages)
	}
}

func TestRunnerAddsDeferredToolsDeltaAttachmentForAnt(t *testing.T) {
	resetDeferredToolTokenCountCache()
	t.Cleanup(resetDeferredToolTokenCountCache)
	t.Setenv("ANTHROPIC_BASE_URL", "")
	t.Setenv("ENABLE_TOOL_SEARCH", "true")
	t.Setenv("CLAUDE_CODE_DISABLE_EXPERIMENTAL_BETAS", "")
	t.Setenv("USER_TYPE", "ant")
	registry, err := tool.NewRegistry(testDeferredToolDefinition("TinyDeferred", "small"), searchtools.NewToolSearchTool())
	if err != nil {
		t.Fatal(err)
	}
	client := &fakeClient{
		calls: []fakeCall{{response: &anthropic.Response{
			ID:         "msg_done",
			Type:       "message",
			Role:       "assistant",
			Model:      "sonnet",
			StopReason: "end_turn",
			Content:    []contracts.ContentBlock{contracts.NewTextBlock("done")},
		}}},
	}
	transcriptPath := filepath.Join(t.TempDir(), "session.jsonl")
	runner := Runner{
		Client:      client,
		Tools:       tool.NewExecutor(registry),
		Model:       "sonnet",
		MaxTokens:   100,
		SessionID:   "sess_delta",
		SessionPath: transcriptPath,
	}
	result, err := runner.RunTurn(context.Background(), nil, messages.UserText("hi"))
	if err != nil {
		t.Fatal(err)
	}
	if len(result.Messages) != 3 || result.Messages[1].Type != contracts.MessageAttachment {
		t.Fatalf("messages = %#v", result.Messages)
	}
	payload, ok := deferredToolsDeltaAttachmentPayload(result.Messages[1])
	if !ok || strings.Join(stringSliceValue(payload["addedNames"]), ",") != "TinyDeferred" {
		t.Fatalf("attachment payload = %#v ok=%v", payload, ok)
	}
	if len(client.requests) != 1 || len(client.requests[0].Messages) != 2 {
		t.Fatalf("requests = %#v", client.requests)
	}
	requestText := client.requests[0].Messages[1].Content[0].Text
	if strings.Contains(requestText, "<available-deferred-tools>") ||
		!strings.Contains(requestText, "The following deferred tools are now available via ToolSearch:\nTinyDeferred") {
		t.Fatalf("request delta text = %q", requestText)
	}
	entries, err := session.Load(transcriptPath)
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 3 || entries[1].Type != contracts.MessageAttachment || entries[1].Message == nil {
		t.Fatalf("transcript entries = %#v", entries)
	}
	if _, ok := deferredToolsDeltaAttachmentPayload(*entries[1].Message); !ok {
		t.Fatalf("transcript attachment = %#v", entries[1].Message)
	}
}

func TestRunnerAddsDeferredToolsDeltaAttachmentForFeatureGate(t *testing.T) {
	resetDeferredToolTokenCountCache()
	t.Cleanup(resetDeferredToolTokenCountCache)
	t.Setenv("ANTHROPIC_BASE_URL", "")
	t.Setenv("ENABLE_TOOL_SEARCH", "true")
	t.Setenv("CLAUDE_CODE_DISABLE_EXPERIMENTAL_BETAS", "")
	t.Setenv("USER_TYPE", "")
	registry, err := tool.NewRegistry(testDeferredToolDefinition("TinyDeferred", "small"), searchtools.NewToolSearchTool())
	if err != nil {
		t.Fatal(err)
	}
	client := &fakeClient{
		calls: []fakeCall{{response: &anthropic.Response{
			ID:         "msg_done",
			Type:       "message",
			Role:       "assistant",
			Model:      "sonnet",
			StopReason: "end_turn",
			Content:    []contracts.ContentBlock{contracts.NewTextBlock("done")},
		}}},
	}
	transcriptPath := filepath.Join(t.TempDir(), "session.jsonl")
	enabled := true
	runner := Runner{
		Client:      client,
		Tools:       tool.NewExecutor(registry),
		Model:       "sonnet",
		MaxTokens:   100,
		SessionID:   "sess_delta_feature",
		SessionPath: transcriptPath,
		MCP: &MCPConfig{UserSettings: contracts.Settings{
			Advanced: &contracts.AdvancedSetting{Telemetry: &enabled, TenguGlacier2XR: &enabled},
		}},
	}
	result, err := runner.RunTurn(context.Background(), nil, messages.UserText("hi"))
	if err != nil {
		t.Fatal(err)
	}
	if len(result.Messages) != 3 || result.Messages[1].Type != contracts.MessageAttachment {
		t.Fatalf("messages = %#v", result.Messages)
	}
	payload, ok := deferredToolsDeltaAttachmentPayload(result.Messages[1])
	if !ok || strings.Join(stringSliceValue(payload["addedNames"]), ",") != "TinyDeferred" {
		t.Fatalf("attachment payload = %#v ok=%v", payload, ok)
	}
	if len(client.requests) != 1 || len(client.requests[0].Messages) != 2 {
		t.Fatalf("requests = %#v", client.requests)
	}
	requestText := client.requests[0].Messages[1].Content[0].Text
	if strings.Contains(requestText, "<available-deferred-tools>") ||
		!strings.Contains(requestText, "The following deferred tools are now available via ToolSearch:\nTinyDeferred") {
		t.Fatalf("request delta text = %q", requestText)
	}
	entries, err := session.Load(transcriptPath)
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 3 || entries[1].Type != contracts.MessageAttachment || entries[1].Message == nil {
		t.Fatalf("transcript entries = %#v", entries)
	}
	telemetryPath := telemetrypkg.SessionPath(transcriptPath, "sess_delta_feature")
	telemetryData, err := os.ReadFile(telemetryPath)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(telemetryData), "TinyDeferred") {
		t.Fatalf("telemetry leaked deferred tool name: %q", telemetryData)
	}
	events, err := telemetrypkg.Load(telemetryPath)
	if err != nil {
		t.Fatal(err)
	}
	var poolChange *telemetrypkg.Event
	for i := range events {
		if events[i].Type == string(EventDeferredPoolChange) {
			poolChange = &events[i]
			break
		}
	}
	if poolChange == nil {
		t.Fatalf("pool-change telemetry event not found: %#v", events)
	}
	if poolChange.DeferredToolAddedCount != 1 ||
		poolChange.DeferredToolRemovedCount != 0 ||
		poolChange.DeferredToolPriorAnnounced != 0 ||
		poolChange.DeferredToolMessagesLength != 1 ||
		poolChange.DeferredToolAttachmentCount != 0 ||
		poolChange.DeferredToolsDeltaCount != 0 ||
		poolChange.DeferredToolsDeltaCallSite != "attachments_main" ||
		poolChange.DeferredToolsDeltaQuerySource != "unknown" {
		t.Fatalf("pool-change telemetry = %#v", poolChange)
	}
}

func TestRunnerToolSearchAutoCachesTokenCountByDeferredToolNames(t *testing.T) {
	resetDeferredToolTokenCountCache()
	t.Cleanup(resetDeferredToolTokenCountCache)
	t.Setenv("ANTHROPIC_BASE_URL", "")
	t.Setenv("ENABLE_TOOL_SEARCH", "auto")
	t.Setenv("CLAUDE_CODE_DISABLE_EXPERIMENTAL_BETAS", "")
	t.Setenv("USER_TYPE", "")
	t.Setenv("CLAUDE_CODE_MAX_CONTEXT_TOKENS", "")
	registry, err := tool.NewRegistry(testDeferredToolDefinition("TinyDeferred", "small"), searchtools.NewToolSearchTool())
	if err != nil {
		t.Fatal(err)
	}
	client := &fakeClient{
		countTokens: []fakeCountTokensCall{{response: &anthropic.CountTokensResponse{InputTokens: 20501}}},
		calls: []fakeCall{
			{response: &anthropic.Response{
				ID:         "msg_done_1",
				Type:       "message",
				Role:       "assistant",
				Model:      "sonnet",
				StopReason: "end_turn",
				Content:    []contracts.ContentBlock{contracts.NewTextBlock("done one")},
			}},
			{response: &anthropic.Response{
				ID:         "msg_done_2",
				Type:       "message",
				Role:       "assistant",
				Model:      "sonnet",
				StopReason: "end_turn",
				Content:    []contracts.ContentBlock{contracts.NewTextBlock("done two")},
			}},
		},
	}
	runner := Runner{
		Client:    client,
		Tools:     tool.NewExecutor(registry),
		Model:     "sonnet",
		MaxTokens: 100,
	}
	if _, err := runner.RunTurn(context.Background(), nil, messages.UserText("hi")); err != nil {
		t.Fatal(err)
	}
	if _, err := runner.RunTurn(context.Background(), nil, messages.UserText("hi again")); err != nil {
		t.Fatal(err)
	}
	if len(client.countCalls) != 1 {
		t.Fatalf("count token calls = %#v", client.countCalls)
	}
	if len(client.requests) != 2 {
		t.Fatalf("requests = %#v", client.requests)
	}
	for _, request := range client.requests {
		if len(request.Tools) != 1 || request.Tools[0].Name != "ToolSearch" {
			t.Fatalf("request tools = %#v", request.Tools)
		}
	}
}

func TestRunnerToolSearchAutoInvalidatesTokenCountCacheOnMCPLifecycle(t *testing.T) {
	resetDeferredToolTokenCountCache()
	t.Cleanup(resetDeferredToolTokenCountCache)
	t.Setenv("ANTHROPIC_BASE_URL", "")
	t.Setenv("ENABLE_TOOL_SEARCH", "auto")
	t.Setenv("CLAUDE_CODE_DISABLE_EXPERIMENTAL_BETAS", "")
	t.Setenv("USER_TYPE", "")
	t.Setenv("CLAUDE_CODE_MAX_CONTEXT_TOKENS", "")
	registry, err := tool.NewRegistry(searchtools.NewToolSearchTool())
	if err != nil {
		t.Fatal(err)
	}
	client := &fakeClient{
		countTokens: []fakeCountTokensCall{
			{response: &anthropic.CountTokensResponse{InputTokens: 20501}},
			{response: &anthropic.CountTokensResponse{InputTokens: 20501}},
		},
		calls: []fakeCall{
			{response: &anthropic.Response{
				ID:         "msg_done_1",
				Type:       "message",
				Role:       "assistant",
				Model:      "sonnet",
				StopReason: "end_turn",
				Content:    []contracts.ContentBlock{contracts.NewTextBlock("done one")},
			}},
			{response: &anthropic.Response{
				ID:         "msg_done_2",
				Type:       "message",
				Role:       "assistant",
				Model:      "sonnet",
				StopReason: "end_turn",
				Content:    []contracts.ContentBlock{contracts.NewTextBlock("done two")},
			}},
		},
	}
	mcpClient := &fakeRunnerMCPClient{tools: []mcp.RemoteTool{{
		Name:        "search",
		Description: "search issues",
		ReadOnly:    true,
	}}}
	openCount := 0
	closeCount := 0
	runner := Runner{
		Client:    client,
		Tools:     tool.NewExecutor(registry),
		Model:     "sonnet",
		MaxTokens: 100,
		MCP: &MCPConfig{
			UserSettings: contracts.Settings{MCPServers: map[string]contracts.MCPServer{
				"github": {Command: "node"},
			}},
			ToolOptions: mcp.ServerToolOptions{
				DisableResources: true,
				DisablePrompts:   true,
				OpenClient: func(context.Context, string, contracts.MCPServer) (mcp.ClientHandle, error) {
					openCount++
					return mcp.ClientHandle{
						Client: mcpClient,
						Close: func() error {
							closeCount++
							return nil
						},
					}, nil
				},
			},
		},
	}
	if _, err := runner.RunTurn(context.Background(), nil, messages.UserText("hi")); err != nil {
		t.Fatal(err)
	}
	if _, err := runner.RunTurn(context.Background(), nil, messages.UserText("hi again")); err != nil {
		t.Fatal(err)
	}
	if openCount != 2 || closeCount != 2 {
		t.Fatalf("open=%d close=%d", openCount, closeCount)
	}
	if len(client.countCalls) != 2 {
		t.Fatalf("count token calls = %#v", client.countCalls)
	}
	if len(client.requests) != 2 {
		t.Fatalf("requests = %#v", client.requests)
	}
}

func TestRunnerToolSearchAutoCachesUnavailableTokenCount(t *testing.T) {
	resetDeferredToolTokenCountCache()
	t.Cleanup(resetDeferredToolTokenCountCache)
	t.Setenv("ANTHROPIC_BASE_URL", "")
	t.Setenv("ENABLE_TOOL_SEARCH", "auto")
	t.Setenv("CLAUDE_CODE_DISABLE_EXPERIMENTAL_BETAS", "")
	t.Setenv("USER_TYPE", "")
	t.Setenv("CLAUDE_CODE_MAX_CONTEXT_TOKENS", "")
	registry, err := tool.NewRegistry(testDeferredToolDefinition("TinyDeferred", "small"), searchtools.NewToolSearchTool())
	if err != nil {
		t.Fatal(err)
	}
	client := &fakeClient{
		countTokens: []fakeCountTokensCall{{err: fmt.Errorf("count unavailable")}},
		calls: []fakeCall{
			{err: fmt.Errorf("haiku unavailable")},
			{response: &anthropic.Response{
				ID:         "msg_done_1",
				Type:       "message",
				Role:       "assistant",
				Model:      "sonnet",
				StopReason: "end_turn",
				Content:    []contracts.ContentBlock{contracts.NewTextBlock("done one")},
			}},
			{response: &anthropic.Response{
				ID:         "msg_done_2",
				Type:       "message",
				Role:       "assistant",
				Model:      "sonnet",
				StopReason: "end_turn",
				Content:    []contracts.ContentBlock{contracts.NewTextBlock("done two")},
			}},
		},
	}
	runner := Runner{
		Client:    client,
		Tools:     tool.NewExecutor(registry),
		Model:     "sonnet",
		MaxTokens: 100,
	}
	if _, err := runner.RunTurn(context.Background(), nil, messages.UserText("hi")); err != nil {
		t.Fatal(err)
	}
	if _, err := runner.RunTurn(context.Background(), nil, messages.UserText("hi again")); err != nil {
		t.Fatal(err)
	}
	if len(client.countCalls) != 1 {
		t.Fatalf("count token calls = %#v", client.countCalls)
	}
	if len(client.requests) != 3 {
		t.Fatalf("requests = %#v", client.requests)
	}
	if client.requests[0].Model != modelpkg.Claude45Haiku {
		t.Fatalf("fallback request = %#v", client.requests[0])
	}
	for _, request := range client.requests[1:] {
		if len(request.Tools) != 1 || request.Tools[0].Name != "TinyDeferred" || request.Tools[0].DeferLoading {
			t.Fatalf("request tools = %#v", request.Tools)
		}
		if len(request.Messages) != 1 || strings.HasPrefix(request.Messages[0].Content[0].Text, "<available-deferred-tools>") {
			t.Fatalf("request messages = %#v", request.Messages)
		}
	}
}

func TestBuildRequestWithToolSearchDisabledForCustomBaseURLUnlessExplicitlyEnabled(t *testing.T) {
	t.Setenv("ANTHROPIC_BASE_URL", "http://127.0.0.1:12345")
	t.Setenv("ENABLE_TOOL_SEARCH", "")
	t.Setenv("CLAUDE_CODE_DISABLE_EXPERIMENTAL_BETAS", "")
	registry, err := tool.NewRegistry(tasktools.NewTaskTool(), searchtools.NewToolSearchTool())
	if err != nil {
		t.Fatal(err)
	}
	runner := Runner{
		Tools:     tool.NewExecutor(registry),
		Model:     "sonnet",
		MaxTokens: 100,
	}
	req, err := runner.BuildRequest([]contracts.Message{messages.UserText("hi")}, "sonnet")
	if err != nil {
		t.Fatal(err)
	}
	if len(req.Tools) != 1 || req.Tools[0].Name != "Task" || req.Tools[0].DeferLoading {
		t.Fatalf("tools = %#v", req.Tools)
	}

	t.Setenv("ENABLE_TOOL_SEARCH", "true")
	req, err = runner.BuildRequest([]contracts.Message{messages.UserText("hi")}, "sonnet")
	if err != nil {
		t.Fatal(err)
	}
	if len(req.Tools) != 1 || req.Tools[0].Name != "ToolSearch" {
		t.Fatalf("tools with explicit enable = %#v", req.Tools)
	}
}

func TestBuildRequestStripsToolReferencesWhenToolSearchInactive(t *testing.T) {
	registry, err := tool.NewRegistry(tasktools.NewTaskTool())
	if err != nil {
		t.Fatal(err)
	}
	runner := Runner{
		Tools:     tool.NewExecutor(registry),
		Model:     "sonnet",
		MaxTokens: 100,
	}
	history := []contracts.Message{{
		Type: contracts.MessageUser,
		Content: []contracts.ContentBlock{{
			Type:      contracts.ContentToolResult,
			ToolUseID: "toolu_search",
			Content:   []contracts.ToolReference{contracts.NewToolReference("Task")},
		}},
	}}
	req, err := runner.BuildRequest(history, "sonnet")
	if err != nil {
		t.Fatal(err)
	}
	if len(req.Messages) != 1 {
		t.Fatalf("messages = %#v", req.Messages)
	}
	block := req.Messages[0].Content[0]
	items, ok := block.Content.([]contracts.ContentBlock)
	if !ok || len(items) != 1 || items[0].Text != "[Tool references removed - tool search not enabled]" {
		t.Fatalf("tool result content = %#v", block.Content)
	}
	if !requestHasTool(req, "Task") || requestTool(req, "Task").DeferLoading {
		t.Fatalf("tools = %#v", req.Tools)
	}
}

func TestBuildRequestStripsOnlyToolReferencesFromMixedContentWhenInactive(t *testing.T) {
	registry, err := tool.NewRegistry(tasktools.NewTaskTool())
	if err != nil {
		t.Fatal(err)
	}
	runner := Runner{
		Tools:     tool.NewExecutor(registry),
		Model:     "sonnet",
		MaxTokens: 100,
	}
	history := []contracts.Message{{
		Type: contracts.MessageUser,
		Content: []contracts.ContentBlock{{
			Type:      contracts.ContentToolResult,
			ToolUseID: "toolu_search",
			Content: []any{
				map[string]any{"type": "text", "text": "kept"},
				map[string]any{"type": "tool_reference", "tool_name": "Task"},
			},
		}},
	}}
	req, err := runner.BuildRequest(history, "sonnet")
	if err != nil {
		t.Fatal(err)
	}
	items, ok := req.Messages[0].Content[0].Content.([]any)
	if !ok || len(items) != 1 {
		t.Fatalf("tool result content = %#v", req.Messages[0].Content[0].Content)
	}
	item, ok := items[0].(map[string]any)
	if !ok || item["type"] != "text" || item["text"] != "kept" {
		t.Fatalf("kept item = %#v", items[0])
	}
}

func TestBuildRequestKeepsToolReferencesWhenToolSearchActive(t *testing.T) {
	allowToolSearchForTest(t)
	registry, err := tool.NewRegistry(tasktools.NewTaskTool(), searchtools.NewToolSearchTool())
	if err != nil {
		t.Fatal(err)
	}
	runner := Runner{
		Tools:     tool.NewExecutor(registry),
		Model:     "sonnet",
		MaxTokens: 100,
	}
	history := []contracts.Message{{
		Type: contracts.MessageUser,
		Content: []contracts.ContentBlock{{
			Type:      contracts.ContentToolResult,
			ToolUseID: "toolu_search",
			Content:   []contracts.ToolReference{contracts.NewToolReference("Task")},
		}},
	}}
	req, err := runner.BuildRequest(history, "sonnet")
	if err != nil {
		t.Fatal(err)
	}
	items, ok := req.Messages[1].Content[0].Content.([]contracts.ToolReference)
	if !ok || len(items) != 1 || items[0].ToolName != "Task" {
		t.Fatalf("tool result content = %#v", req.Messages[1].Content[0].Content)
	}
}

func TestBuildRequestLoadsDiscoveredToolReferences(t *testing.T) {
	registry, err := tool.NewRegistry(tasktools.NewTaskTool())
	if err != nil {
		t.Fatal(err)
	}
	runner := Runner{
		Tools:     tool.NewExecutor(registry),
		Model:     "sonnet",
		MaxTokens: 100,
	}
	history := []contracts.Message{
		messages.UserText("find task"),
		{
			Type: contracts.MessageUser,
			Content: []contracts.ContentBlock{{
				Type:      contracts.ContentToolResult,
				ToolUseID: "toolu_search",
				Content: []contracts.ToolReference{
					contracts.NewToolReference("Task"),
				},
			}},
		},
	}
	req, err := runner.BuildRequest(history, "sonnet")
	if err != nil {
		t.Fatal(err)
	}
	if len(req.Tools) != 1 || req.Tools[0].Name != "Task" {
		t.Fatalf("tools = %#v", req.Tools)
	}
	if req.Tools[0].DeferLoading {
		t.Fatalf("task defer_loading = true after tool_reference discovery")
	}
}

func TestBuildRequestLoadsDecodedToolReferenceMaps(t *testing.T) {
	registry, err := tool.NewRegistry(tasktools.NewTaskTool())
	if err != nil {
		t.Fatal(err)
	}
	runner := Runner{
		Tools:     tool.NewExecutor(registry),
		Model:     "sonnet",
		MaxTokens: 100,
	}
	history := []contracts.Message{
		{
			Type: contracts.MessageUser,
			Content: []contracts.ContentBlock{{
				Type:      contracts.ContentToolResult,
				ToolUseID: "toolu_search",
				Content: []any{
					map[string]any{"type": "tool_reference", "tool_name": "Task"},
				},
			}},
		},
	}
	req, err := runner.BuildRequest(history, "sonnet")
	if err != nil {
		t.Fatal(err)
	}
	if len(req.Tools) != 1 || req.Tools[0].Name != "Task" {
		t.Fatalf("tools = %#v", req.Tools)
	}
	if req.Tools[0].DeferLoading {
		t.Fatalf("task defer_loading = true after decoded tool_reference discovery")
	}
}

func TestBuildRequestLoadsCompactBoundaryDiscoveredTools(t *testing.T) {
	registry, err := tool.NewRegistry(tasktools.NewTaskTool())
	if err != nil {
		t.Fatal(err)
	}
	runner := Runner{
		Tools:     tool.NewExecutor(registry),
		Model:     "sonnet",
		MaxTokens: 100,
	}
	history := []contracts.Message{
		{
			Type:    contracts.MessageSystem,
			Subtype: "compact_boundary",
			Raw: map[string]any{
				"compactMetadata": session.CompactMetadata{
					PreCompactDiscoveredTools: []string{"Task"},
				},
			},
		},
	}
	req, err := runner.BuildRequest(history, "sonnet")
	if err != nil {
		t.Fatal(err)
	}
	if len(req.Tools) != 1 || req.Tools[0].Name != "Task" {
		t.Fatalf("tools = %#v", req.Tools)
	}
	if req.Tools[0].DeferLoading {
		t.Fatalf("task defer_loading = true after compact-boundary discovery")
	}
}

func TestBuildRequestLoadsDecodedCompactBoundaryDiscoveredTools(t *testing.T) {
	registry, err := tool.NewRegistry(tasktools.NewTaskTool())
	if err != nil {
		t.Fatal(err)
	}
	runner := Runner{
		Tools:     tool.NewExecutor(registry),
		Model:     "sonnet",
		MaxTokens: 100,
	}
	history := []contracts.Message{
		{
			Type:    contracts.MessageSystem,
			Subtype: "compact_boundary",
			Raw: map[string]any{
				"compact_metadata": map[string]any{
					"pre_compact_discovered_tools": []any{"Task"},
				},
			},
		},
	}
	req, err := runner.BuildRequest(history, "sonnet")
	if err != nil {
		t.Fatal(err)
	}
	if len(req.Tools) != 1 || req.Tools[0].Name != "Task" {
		t.Fatalf("tools = %#v", req.Tools)
	}
	if req.Tools[0].DeferLoading {
		t.Fatalf("task defer_loading = true after decoded compact-boundary discovery")
	}
}

func TestRunnerGatesAdvancedLSPDiagnosticsTool(t *testing.T) {
	requestForAdvancedSetting := func(t *testing.T, advanced *contracts.AdvancedSetting) anthropic.Request {
		t.Helper()
		client := &fakeClient{calls: []fakeCall{{response: &anthropic.Response{
			ID:         "msg_done",
			Type:       "message",
			Role:       "assistant",
			Model:      "sonnet",
			StopReason: "end_turn",
			Content:    []contracts.ContentBlock{contracts.NewTextBlock("done")},
		}}}}
		registry, err := tool.NewRegistry()
		if err != nil {
			t.Fatal(err)
		}
		runner := Runner{
			Client:    client,
			Tools:     tool.NewExecutor(registry),
			Model:     "sonnet",
			MaxTokens: 128,
			MCP:       &MCPConfig{UserSettings: contracts.Settings{Advanced: advanced}},
		}
		if _, err := runner.RunTurn(context.Background(), nil, messages.UserText("check diagnostics")); err != nil {
			t.Fatal(err)
		}
		if len(client.requests) != 1 {
			t.Fatalf("requests = %#v", client.requests)
		}
		return client.requests[0]
	}

	disabled := false
	if requestHasTool(requestForAdvancedSetting(t, &contracts.AdvancedSetting{LSP: &disabled}), "LSPDiagnostics") {
		t.Fatalf("LSPDiagnostics should not be exposed when advanced.lsp is disabled")
	}
	enabled := true
	if !requestHasTool(requestForAdvancedSetting(t, &contracts.AdvancedSetting{LSP: &enabled}), "LSPDiagnostics") {
		t.Fatalf("LSPDiagnostics should be exposed when advanced.lsp is enabled")
	}
}

func TestBuildRequestIncludesSystemPrompt(t *testing.T) {
	runner := Runner{
		Model:        "sonnet",
		MaxTokens:    100,
		SystemPrompt: "Use terse answers.",
	}
	req, err := runner.BuildRequest([]contracts.Message{messages.UserText("hi")}, "sonnet")
	if err != nil {
		t.Fatal(err)
	}
	if req.System != "Use terse answers." {
		t.Fatalf("system = %#v", req.System)
	}
}

func TestBuildRequestIncludesOutputStyleSystemSection(t *testing.T) {
	configHome := t.TempDir()
	t.Setenv("CLAUDE_CONFIG_DIR", configHome)
	cwd := t.TempDir()
	if err := os.MkdirAll(filepath.Join(cwd, ".claude", "output-styles"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(cwd, ".claude", "output-styles", "brief.md"), []byte("---\ndescription: Brief\n---\nUse short answers."), 0o644); err != nil {
		t.Fatal(err)
	}
	runner := Runner{
		Model:            "sonnet",
		MaxTokens:        100,
		SystemPrompt:     "Base system.",
		WorkingDirectory: cwd,
		MCP: &MCPConfig{UserSettings: contracts.Settings{
			OutputStyle: "brief",
		}},
	}
	req, err := runner.BuildRequest([]contracts.Message{messages.UserText("hi")}, "sonnet")
	if err != nil {
		t.Fatal(err)
	}
	system, ok := req.System.(string)
	if !ok {
		t.Fatalf("system type = %#v", req.System)
	}
	if !strings.Contains(system, "Base system.\n\n# Output Style: brief\nUse short answers.") {
		t.Fatalf("system = %q", system)
	}
}

func TestDisabledPluginDoesNotProvideOutputStyle(t *testing.T) {
	configHome := t.TempDir()
	t.Setenv("CLAUDE_CONFIG_DIR", configHome)
	repo := filepath.Join(t.TempDir(), "repo")
	cwd := filepath.Join(repo, "pkg")
	pluginDir := filepath.Join(repo, ".claude", "plugins", "demo")
	if err := os.MkdirAll(filepath.Join(repo, ".git"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Join(pluginDir, "output-styles"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(cwd, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(pluginDir, "plugin.json"), []byte(`{"name":"demo"}`), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(pluginDir, "output-styles", "brief.md"), []byte("---\ndescription: Brief\n---\nBrief."), 0o644); err != nil {
		t.Fatal(err)
	}
	runner := Runner{
		WorkingDirectory: cwd,
		MCP: &MCPConfig{UserSettings: contracts.Settings{
			EnabledPlugins: map[string]any{"demo": false},
			OutputStyle:    "demo:brief",
		}},
	}
	if got := runner.EffectiveOutputStyleName(); got != "demo:brief" {
		t.Fatalf("effective output style should preserve configured name, got %q", got)
	}
	for _, name := range runner.AvailableOutputStyleNames() {
		if name == "demo:brief" {
			t.Fatalf("disabled plugin style was available: %#v", runner.AvailableOutputStyleNames())
		}
	}
	req, err := runner.BuildRequest([]contracts.Message{messages.UserText("hi")}, "sonnet")
	if err != nil {
		t.Fatal(err)
	}
	if system, ok := req.System.(string); ok && strings.Contains(system, "Brief.") {
		t.Fatalf("disabled plugin output style injected into system: %q", system)
	}
}

func TestRunnerCanUseStreamingClient(t *testing.T) {
	client := &fakeClient{streams: [][]anthropic.StreamEvent{{
		{Type: "message_start", Message: &anthropic.Response{ID: "msg_1", Type: "message", Role: "assistant", Model: "sonnet"}},
		{Type: "content_block_start", Index: 0, ContentBlock: &contracts.ContentBlock{Type: contracts.ContentText}},
		{Type: "content_block_delta", Index: 0, Delta: map[string]any{"type": "text_delta", "text": "streamed"}},
		{Type: "message_delta", Delta: map[string]any{"stop_reason": "end_turn"}, Usage: &contracts.Usage{OutputTokens: 1}},
	}}}
	runner := Runner{
		Client:       client,
		Model:        "sonnet",
		MaxTokens:    64,
		UseStreaming: true,
	}
	var streamEvents []Event
	runner.OnEvent = func(event Event) {
		if event.Type == EventStreamEvent {
			streamEvents = append(streamEvents, event)
		}
	}
	result, err := runner.RunTurn(context.Background(), nil, messages.UserText("hello"))
	if err != nil {
		t.Fatal(err)
	}
	if result.Assistant.Content[0].Text != "streamed" || !client.requests[0].Stream {
		t.Fatalf("result = %#v request = %#v", result, client.requests[0])
	}
	if len(streamEvents) != 4 || streamEvents[2].StreamEvent == nil || streamEvents[2].StreamEvent.TextDelta() != "streamed" {
		t.Fatalf("stream events = %#v", streamEvents)
	}
}

func TestRunnerFallsBackToNonStreamingWhenStreamFailsBeforeEvents(t *testing.T) {
	client := &fakeClient{
		streamErrs: []error{anthropic.APIError{StatusCode: http.StatusServiceUnavailable, Type: "overloaded_error", Message: "stream unavailable"}},
		calls: []fakeCall{{
			response: &anthropic.Response{ID: "msg_fallback", Type: "message", Role: "assistant", Model: "sonnet", Content: []contracts.ContentBlock{contracts.NewTextBlock("fallback ok")}},
		}},
	}
	runner := Runner{
		Client:       client,
		Model:        "sonnet",
		MaxTokens:    64,
		UseStreaming: true,
	}
	result, err := runner.RunTurn(context.Background(), nil, messages.UserText("hello"))
	if err != nil {
		t.Fatal(err)
	}
	if got := result.Assistant.Content[0].Text; got != "fallback ok" {
		t.Fatalf("assistant text = %q", got)
	}
	if len(client.requests) != 2 {
		t.Fatalf("requests = %#v", client.requests)
	}
	if !client.requests[0].Stream {
		t.Fatalf("first request should be streaming: %#v", client.requests[0])
	}
	if client.requests[1].Stream {
		t.Fatalf("fallback request should be non-streaming: %#v", client.requests[1])
	}
}

func TestRunnerDoesNotFallbackAfterStreamEvents(t *testing.T) {
	client := &fakeClient{
		streams: [][]anthropic.StreamEvent{{
			{Type: "message_start", Message: &anthropic.Response{ID: "msg_partial", Type: "message", Role: "assistant", Model: "sonnet"}},
		}},
		streamErrs: []error{anthropic.APIError{StatusCode: http.StatusServiceUnavailable, Type: "overloaded_error", Message: "stream interrupted"}},
		calls: []fakeCall{{
			response: &anthropic.Response{ID: "msg_fallback", Type: "message", Role: "assistant", Model: "sonnet", Content: []contracts.ContentBlock{contracts.NewTextBlock("should not run")}},
		}},
	}
	runner := Runner{
		Client:       client,
		Model:        "sonnet",
		MaxTokens:    64,
		UseStreaming: true,
	}
	_, err := runner.RunTurn(context.Background(), nil, messages.UserText("hello"))
	if err == nil {
		t.Fatal("expected stream error")
	}
	if len(client.requests) != 1 {
		t.Fatalf("requests = %#v", client.requests)
	}
}

func conversationLSPHelperDefinition() lsppkg.ServerDefinition {
	return lsppkg.ServerDefinition{
		Name:           "conversation-lsp-helper",
		Command:        os.Args[0],
		Args:           []string{"-test.run=TestConversationLSPServerHelper", "--", "conversation-lsp-helper"},
		FileExtensions: []string{".go"},
		RootMarkers:    []string{"go.mod"},
	}
}

func waitForConversationLSPProcess(t *testing.T, process *lsppkg.ServerProcess, label string) lsppkg.ServerProcessResult {
	t.Helper()
	select {
	case result := <-process.Done():
		return result
	case <-time.After(30 * time.Second):
		t.Fatalf("timed out waiting for %s process", label)
		return lsppkg.ServerProcessResult{}
	}
}

func writeConversationLSPCommandShim(t *testing.T, name string) {
	t.Helper()
	dir := t.TempDir()
	testBinary, err := filepath.Abs(os.Args[0])
	if err != nil {
		t.Fatal(err)
	}
	t.Setenv("GO_TEST_BINARY", testBinary)
	t.Setenv("PATH", dir+string(os.PathListSeparator)+os.Getenv("PATH"))
	path := filepath.Join(dir, name)
	script := "#!/bin/sh\nexec \"$GO_TEST_BINARY\" -test.run=TestConversationLSPServerHelper -- conversation-lsp-helper\n"
	if err := os.WriteFile(path, []byte(script), 0o755); err != nil {
		t.Fatal(err)
	}
}

func TestConversationLSPServerHelper(t *testing.T) {
	if os.Getenv("GO_WANT_CONVERSATION_LSP_HELPER") != "1" {
		return
	}
	reader := bufio.NewReader(os.Stdin)
	if _, err := lsppkg.ReadFramedMessage(reader, 8<<20); err != nil {
		os.Exit(2)
	}
	if err := lsppkg.WriteFramedMessage(os.Stdout, []byte(`{"jsonrpc":"2.0","id":1,"result":{"capabilities":{"textDocumentSync":1}}}`)); err != nil {
		os.Exit(3)
	}
	if _, err := lsppkg.ReadFramedMessage(reader, 8<<20); err != nil {
		os.Exit(4)
	}
	if _, err := lsppkg.ReadFramedMessage(reader, 8<<20); err != nil {
		os.Exit(5)
	}
	if err := lsppkg.WriteFramedMessage(os.Stdout, []byte(`{"jsonrpc":"2.0","method":"textDocument/publishDiagnostics","params":{"uri":"file:///work/main.go","diagnostics":[{"severity":1,"message":"runner lsp diagnostic"}]}}`)); err != nil {
		os.Exit(6)
	}
	os.Exit(0)
}

func TestRunnerLogoutSlashCommandWithoutCredentialStore(t *testing.T) {
	runner := Runner{Client: &fakeClient{}, SessionID: "sess_logout"}
	result, err := runner.RunTurn(context.Background(), nil, messages.UserText("/logout"))
	if err != nil {
		t.Fatal(err)
	}
	if len(runner.Client.(*fakeClient).requests) != 0 {
		t.Fatalf("model should not be queried for /logout")
	}
	if !result.LoggedOut {
		t.Fatalf("result.LoggedOut should be true, got false")
	}
	if len(result.Messages) != 2 {
		t.Fatalf("expected 2 messages (user + assistant), got %d: %#v", len(result.Messages), result.Messages)
	}
	text := result.Messages[1].Content[0].Text
	if !strings.Contains(text, "No stored credentials") {
		t.Fatalf("logout text = %q, want 'No stored credentials...'", text)
	}
}

func TestRunnerLoginSlashCommandWithoutCredentialStore(t *testing.T) {
	runner := Runner{Client: &fakeClient{}, SessionID: "sess_login"}
	result, err := runner.RunTurn(context.Background(), nil, messages.UserText("/login"))
	if err != nil {
		t.Fatal(err)
	}
	if len(runner.Client.(*fakeClient).requests) != 0 {
		t.Fatalf("model should not be queried for /login")
	}
	if !result.Login {
		t.Fatalf("result.Login should be true, got false")
	}
	if len(result.Messages) != 2 {
		t.Fatalf("expected 2 messages (user + assistant), got %d: %#v", len(result.Messages), result.Messages)
	}
	text := result.Messages[1].Content[0].Text
	if !strings.Contains(text, "claude auth login") && !strings.Contains(text, "sign in") {
		t.Fatalf("login text = %q, want to contain 'claude auth login' or 'sign in'", text)
	}
}

func TestRunnerContextSlashCommandReturnsReport(t *testing.T) {
	runner := Runner{Client: &fakeClient{}, SessionID: "sess_context", Model: "claude-sonnet-4-6"}
	result, err := runner.RunTurn(context.Background(), nil, messages.UserText("/context"))
	if err != nil {
		t.Fatal(err)
	}
	if len(runner.Client.(*fakeClient).requests) != 0 {
		t.Fatalf("model should not be queried for /context")
	}
	if len(result.Messages) != 2 {
		t.Fatalf("expected 2 messages (user + report), got %d: %#v", len(result.Messages), result.Messages)
	}
	text := result.Messages[1].Content[0].Text
	if !strings.Contains(text, "Context window usage") {
		t.Fatalf("/context report missing header: %q", text)
	}
	if !strings.Contains(text, "%") {
		t.Fatalf("/context report missing percentage: %q", text)
	}
}

// ─── CLI-FLAG-40: --json-schema wired into BuildRequest ──────────────────────

func TestBuildRequestOutputSchemaInjectsFormatIntoOutputConfig(t *testing.T) {
	// When OutputSchema is set, output_config.format must be populated.
	runner := Runner{
		Model:     "claude-3-5-sonnet-20241022",
		MaxTokens: 100,
		OutputSchema: contracts.JSONSchema{
			"type": "object",
			"properties": map[string]any{
				"name": map[string]any{"type": "string"},
			},
		},
	}
	req, err := runner.BuildRequest([]contracts.Message{messages.UserText("hi")}, runner.Model)
	if err != nil {
		t.Fatalf("BuildRequest: %v", err)
	}
	if req.OutputConfig == nil {
		t.Fatal("OutputConfig should not be nil when OutputSchema is set")
	}
	format, ok := req.OutputConfig["format"].(map[string]any)
	if !ok {
		t.Fatalf("OutputConfig[format] = %T, want map[string]any", req.OutputConfig["format"])
	}
	if format["type"] != "json_schema" {
		t.Errorf("OutputConfig[format][type] = %q, want %q", format["type"], "json_schema")
	}
	if _, ok := format["json_schema"]; !ok {
		t.Error("OutputConfig[format][json_schema] should be present")
	}
}

func TestBuildRequestOutputSchemaCombinesWithEffortLevel(t *testing.T) {
	// When both EffortLevel and OutputSchema are set, both keys appear in OutputConfig.
	runner := Runner{
		Model:        "claude-3-5-sonnet-20241022",
		MaxTokens:    100,
		EffortLevel:  "high",
		OutputSchema: contracts.JSONSchema{"type": "object"},
	}
	req, err := runner.BuildRequest([]contracts.Message{messages.UserText("hi")}, runner.Model)
	if err != nil {
		t.Fatalf("BuildRequest: %v", err)
	}
	if req.OutputConfig == nil {
		t.Fatal("OutputConfig should not be nil when EffortLevel and OutputSchema are both set")
	}
	if _, ok := req.OutputConfig["effort"]; !ok {
		t.Error("OutputConfig[effort] should be present when EffortLevel is set")
	}
	if _, ok := req.OutputConfig["format"]; !ok {
		t.Error("OutputConfig[format] should be present when OutputSchema is set")
	}
}

func TestBuildRequestNoOutputSchemaLeavesOutputConfigNil(t *testing.T) {
	// When OutputSchema is empty and EffortLevel is empty, OutputConfig should be nil.
	runner := Runner{
		Model:     "claude-3-5-sonnet-20241022",
		MaxTokens: 100,
	}
	req, err := runner.BuildRequest([]contracts.Message{messages.UserText("hi")}, runner.Model)
	if err != nil {
		t.Fatalf("BuildRequest: %v", err)
	}
	if req.OutputConfig != nil {
		t.Errorf("OutputConfig should be nil when no effort or schema; got %v", req.OutputConfig)
	}
}

// ─── CLI-FLAG-44: MCPPermissionAsker ─────────────────────────────────────────

func TestMCPPermissionAskerAllowsWhenToolReturnsAllow(t *testing.T) {
	allowTool := tool.FuncTool{
		DefinitionValue: contracts.ToolDefinition{
			Name:        "perm_tool",
			InputSchema: contracts.JSONSchema{"type": "object"},
		},
		CallFunc: func(_ tool.Context, _ json.RawMessage, _ tool.ProgressSink) (contracts.ToolResult, error) {
			return contracts.ToolResult{Content: `{"behavior":"allow"}`}, nil
		},
	}
	registry, err := tool.NewRegistry(allowTool)
	if err != nil {
		t.Fatal(err)
	}
	asker := &MCPPermissionAsker{ToolName: "perm_tool", Registry: registry}
	decision, err := asker.Ask(context.Background(), tool.PermissionAskRequest{
		ToolName: "Bash",
		Input:    map[string]any{"command": "ls"},
	})
	if err != nil {
		t.Fatalf("Ask: %v", err)
	}
	if decision.Behavior != contracts.PermissionAllow {
		t.Errorf("Behavior = %q, want %q", decision.Behavior, contracts.PermissionAllow)
	}
}

func TestMCPPermissionAskerDeniesWhenToolReturnsDeny(t *testing.T) {
	denyTool := tool.FuncTool{
		DefinitionValue: contracts.ToolDefinition{
			Name:        "perm_tool",
			InputSchema: contracts.JSONSchema{"type": "object"},
		},
		CallFunc: func(_ tool.Context, _ json.RawMessage, _ tool.ProgressSink) (contracts.ToolResult, error) {
			return contracts.ToolResult{Content: `{"behavior":"deny","message":"blocked"}`}, nil
		},
	}
	registry, err := tool.NewRegistry(denyTool)
	if err != nil {
		t.Fatal(err)
	}
	asker := &MCPPermissionAsker{ToolName: "perm_tool", Registry: registry}
	decision, err := asker.Ask(context.Background(), tool.PermissionAskRequest{
		ToolName: "Bash",
	})
	if err != nil {
		t.Fatalf("Ask: %v", err)
	}
	if decision.Behavior != contracts.PermissionDeny {
		t.Errorf("Behavior = %q, want %q", decision.Behavior, contracts.PermissionDeny)
	}
	if decision.Message != "blocked" {
		t.Errorf("Message = %q, want %q", decision.Message, "blocked")
	}
}

func TestMCPPermissionAskerDeniesWhenToolNotFound(t *testing.T) {
	registry, err := tool.NewRegistry()
	if err != nil {
		t.Fatal(err)
	}
	asker := &MCPPermissionAsker{ToolName: "missing_tool", Registry: registry}
	decision, askErr := asker.Ask(context.Background(), tool.PermissionAskRequest{ToolName: "Bash"})
	if askErr == nil {
		t.Error("Ask should return error when tool is not found")
	}
	if decision.Behavior != contracts.PermissionDeny {
		t.Errorf("Behavior = %q, want %q (fail-safe deny)", decision.Behavior, contracts.PermissionDeny)
	}
}

func TestMCPPermissionAskerDeniesWhenRegistryNil(t *testing.T) {
	asker := &MCPPermissionAsker{ToolName: "perm_tool", Registry: nil}
	decision, err := asker.Ask(context.Background(), tool.PermissionAskRequest{ToolName: "Bash"})
	if err != nil {
		t.Fatalf("Ask with nil registry: %v", err)
	}
	if decision.Behavior != contracts.PermissionDeny {
		t.Errorf("Behavior = %q, want deny (fail-safe)", decision.Behavior)
	}
}

func TestMain(m *testing.M) {
	if shouldRunConversationLSPHelper() {
		os.Setenv("GO_WANT_CONVERSATION_LSP_HELPER", "1")
	}
	os.Exit(m.Run())
}

func shouldRunConversationLSPHelper() bool {
	for i, arg := range os.Args {
		if arg == "--" && i+1 < len(os.Args) && os.Args[i+1] == "conversation-lsp-helper" {
			return true
		}
	}
	return false
}
