package mcp

import (
	"context"
	"encoding/json"
	"reflect"
	"testing"

	"ccgo/internal/contracts"
	"ccgo/internal/tool"
)

type fakeMCPClient struct {
	tools      []RemoteTool
	calls      []fakeMCPCall
	callResult any
	listErr    error
	callErr    error
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
