package mcp

import (
	"context"
	"encoding/json"
	"reflect"
	"strings"
	"testing"

	"ccgo/internal/contracts"
	"ccgo/internal/tool"
)

type fakeMCPClient struct {
	tools        []RemoteTool
	resources    []RemoteResource
	contents     []ResourceContent
	prompts      []RemotePrompt
	promptResult PromptResult
	calls        []fakeMCPCall
	callResult   any
	listErr      error
	callErr      error
}

type fakeMCPCall struct {
	ServerName string
	ToolName   string
	Input      json.RawMessage
}

func (c *fakeMCPClient) ListTools(_ context.Context, serverName string) ([]RemoteTool, error) {
	if c.listErr != nil {
		return nil, c.listErr
	}
	return c.tools, nil
}

func (c *fakeMCPClient) CallTool(_ context.Context, serverName string, toolName string, input json.RawMessage) (any, error) {
	if c.callErr != nil {
		return nil, c.callErr
	}
	c.calls = append(c.calls, fakeMCPCall{ServerName: serverName, ToolName: toolName, Input: append(json.RawMessage(nil), input...)})
	return c.callResult, nil
}

func (c *fakeMCPClient) ListResources(_ context.Context, serverName string) ([]RemoteResource, error) {
	return c.resources, nil
}

func (c *fakeMCPClient) ReadResource(_ context.Context, serverName string, uri string) ([]ResourceContent, error) {
	c.calls = append(c.calls, fakeMCPCall{ServerName: serverName, ToolName: "read_resource", Input: json.RawMessage(`{"uri":` + quoteJSON(uri) + `}`)})
	return c.contents, nil
}

func (c *fakeMCPClient) ListPrompts(_ context.Context, serverName string) ([]RemotePrompt, error) {
	return c.prompts, nil
}

func (c *fakeMCPClient) GetPrompt(_ context.Context, serverName string, promptName string, arguments map[string]string) (PromptResult, error) {
	input, _ := json.Marshal(map[string]any{"name": promptName, "arguments": arguments})
	c.calls = append(c.calls, fakeMCPCall{ServerName: serverName, ToolName: "get_prompt", Input: input})
	return c.promptResult, nil
}

func TestBuildToolsCreatesMCPToolDefinitions(t *testing.T) {
	client := &fakeMCPClient{tools: []RemoteTool{
		{
			Name:        "search/issues",
			Description: "Search issues",
			InputSchema: contracts.JSONSchema{
				"type": "object",
				"properties": map[string]any{
					"query": map[string]any{"type": "string"},
				},
			},
			ReadOnly: true,
		},
		{Name: ""},
	}}

	tools, err := BuildTools(context.Background(), ToolBuildOptions{ServerName: "github.com", Client: client})
	if err != nil {
		t.Fatal(err)
	}
	if len(tools) != 1 {
		t.Fatalf("tools = %#v", tools)
	}
	def, err := tool.Definition(tool.PromptContext{}, tools[0])
	if err != nil {
		t.Fatal(err)
	}
	if def.Name != "mcp__github_com__search_issues" {
		t.Fatalf("name = %q", def.Name)
	}
	if def.MCP == nil || def.MCP.ServerName != "github.com" || def.MCP.ToolName != "search/issues" {
		t.Fatalf("mcp ref = %#v", def.MCP)
	}
	if !def.ReadOnly || !def.ConcurrencySafe {
		t.Fatalf("definition flags = %#v", def)
	}
	if !reflect.DeepEqual(def.InputSchema, client.tools[0].InputSchema) {
		t.Fatalf("schema = %#v", def.InputSchema)
	}
}

func TestBuildToolsPropagatesDestructiveHint(t *testing.T) {
	client := &fakeMCPClient{tools: []RemoteTool{{
		Name:        "delete",
		Description: "Delete an item",
		Destructive: true,
	}}}
	tools, err := BuildTools(context.Background(), ToolBuildOptions{ServerName: "local", Client: client})
	if err != nil {
		t.Fatal(err)
	}
	if len(tools) != 1 {
		t.Fatalf("tools = %#v", tools)
	}
	def, err := tool.Definition(tool.PromptContext{}, tools[0])
	if err != nil {
		t.Fatal(err)
	}
	if !def.Destructive || def.ReadOnly || def.ConcurrencySafe {
		t.Fatalf("definition flags = %#v", def)
	}
}

func TestBuiltMCPToolCallsClientAndProcessesResult(t *testing.T) {
	client := &fakeMCPClient{
		tools: []RemoteTool{{
			Name:     "search",
			ReadOnly: true,
		}},
		callResult: map[string]any{"toolResult": "ok"},
	}
	tools, err := BuildTools(context.Background(), ToolBuildOptions{ServerName: "github", Client: client})
	if err != nil {
		t.Fatal(err)
	}

	registry, err := tool.NewRegistry(tools...)
	if err != nil {
		t.Fatal(err)
	}
	executor := tool.Executor{Registry: registry}
	result, err := executor.Execute(tool.Context{Context: context.Background()}, contracts.ToolUse{
		ID:    "toolu_mcp",
		Name:  "mcp__github__search",
		Input: json.RawMessage(`{"query":"bugs"}`),
	}, nil)
	if err != nil {
		t.Fatal(err)
	}
	if result.ToolUseID != "toolu_mcp" || result.Content != "ok" {
		t.Fatalf("result = %#v", result)
	}
	if len(client.calls) != 1 {
		t.Fatalf("calls = %#v", client.calls)
	}
	if client.calls[0].ServerName != "github" || client.calls[0].ToolName != "search" {
		t.Fatalf("call = %#v", client.calls[0])
	}
	if string(client.calls[0].Input) != `{"query":"bugs"}` {
		t.Fatalf("input = %s", client.calls[0].Input)
	}
	if result.Meta["mcp"] == nil {
		t.Fatalf("missing mcp meta: %#v", result.Meta)
	}
}

func TestBuildToolsRequiresClient(t *testing.T) {
	if _, err := BuildTools(context.Background(), ToolBuildOptions{ServerName: "github"}); err == nil {
		t.Fatal("expected nil client error")
	}
}

func TestBuildResourceToolsListAndReadResources(t *testing.T) {
	client := &fakeMCPClient{
		resources: []RemoteResource{{
			URI:         "file:///tmp/a.txt",
			Name:        "a.txt",
			Description: "A file",
			MimeType:    "text/plain",
		}},
		contents: []ResourceContent{{
			URI:      "file:///tmp/a.txt",
			MimeType: "text/plain",
			Text:     "hello",
		}},
	}
	tools := BuildResourceTools(ToolBuildOptions{ServerName: "files", Client: client})
	if len(tools) != 2 {
		t.Fatalf("tools = %#v", tools)
	}
	registry, err := tool.NewRegistry(tools...)
	if err != nil {
		t.Fatal(err)
	}
	executor := tool.Executor{Registry: registry}

	listResult, err := executor.Execute(tool.Context{Context: context.Background()}, contracts.ToolUse{
		ID:    "toolu_list",
		Name:  "mcp__files__list_resources",
		Input: json.RawMessage(`{}`),
	}, nil)
	if err != nil {
		t.Fatal(err)
	}
	if listResult.ToolUseID != "toolu_list" {
		t.Fatalf("list result = %#v", listResult)
	}
	if !strings.Contains(listResult.Content.(string), "file:///tmp/a.txt") {
		t.Fatalf("list content = %#v", listResult.Content)
	}

	readResult, err := executor.Execute(tool.Context{Context: context.Background()}, contracts.ToolUse{
		ID:    "toolu_read",
		Name:  "mcp__files__read_resource",
		Input: json.RawMessage(`{"uri":"file:///tmp/a.txt"}`),
	}, nil)
	if err != nil {
		t.Fatal(err)
	}
	if readResult.ToolUseID != "toolu_read" {
		t.Fatalf("read result = %#v", readResult)
	}
	blocks := readResult.Content.([]contracts.ContentBlock)
	if len(blocks) != 1 || !strings.Contains(blocks[0].Text, "hello") {
		t.Fatalf("read blocks = %#v", blocks)
	}
	if len(client.calls) != 1 || client.calls[0].ToolName != "read_resource" {
		t.Fatalf("calls = %#v", client.calls)
	}
}

func TestBuildPromptToolsListAndGetPrompt(t *testing.T) {
	client := &fakeMCPClient{
		prompts: []RemotePrompt{{
			Name:        "deploy",
			Description: "Deploy service",
			Arguments: []PromptArgument{{
				Name:        "env",
				Description: "Target environment",
				Required:    true,
			}},
		}},
		promptResult: PromptResult{
			Description: "Deploy service",
			Messages: []PromptMessage{{
				Role: "user",
				Content: []any{map[string]any{
					"type": "text",
					"text": "deploy prod",
				}},
			}},
		},
	}
	tools := BuildPromptTools(ToolBuildOptions{ServerName: "workflow", Client: client})
	if len(tools) != 2 {
		t.Fatalf("tools = %#v", tools)
	}
	registry, err := tool.NewRegistry(tools...)
	if err != nil {
		t.Fatal(err)
	}
	executor := tool.Executor{Registry: registry}

	listResult, err := executor.Execute(tool.Context{Context: context.Background()}, contracts.ToolUse{
		ID:    "toolu_prompts",
		Name:  "mcp__workflow__list_prompts",
		Input: json.RawMessage(`{}`),
	}, nil)
	if err != nil {
		t.Fatal(err)
	}
	if listResult.ToolUseID != "toolu_prompts" {
		t.Fatalf("list result = %#v", listResult)
	}
	if !strings.Contains(listResult.Content.(string), "deploy") {
		t.Fatalf("list content = %#v", listResult.Content)
	}

	getResult, err := executor.Execute(tool.Context{Context: context.Background()}, contracts.ToolUse{
		ID:    "toolu_get_prompt",
		Name:  "mcp__workflow__get_prompt",
		Input: json.RawMessage(`{"name":"deploy","arguments":{"env":"prod"}}`),
	}, nil)
	if err != nil {
		t.Fatal(err)
	}
	if getResult.ToolUseID != "toolu_get_prompt" {
		t.Fatalf("get result = %#v", getResult)
	}
	if !strings.Contains(getResult.Content.(string), "deploy prod") {
		t.Fatalf("get content = %#v", getResult.Content)
	}
	if len(client.calls) != 1 || client.calls[0].ToolName != "get_prompt" {
		t.Fatalf("calls = %#v", client.calls)
	}
	if !strings.Contains(string(client.calls[0].Input), `"env":"prod"`) {
		t.Fatalf("get input = %s", client.calls[0].Input)
	}
}

func quoteJSON(value string) string {
	data, _ := json.Marshal(value)
	return string(data)
}
