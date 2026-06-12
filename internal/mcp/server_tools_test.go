package mcp

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"testing"

	"ccgo/internal/contracts"
	"ccgo/internal/tool"
)

func TestBuildServerToolSetBuildsRemoteAndHelperTools(t *testing.T) {
	client := &fakeMCPClient{
		tools: []RemoteTool{{
			Name:     "search",
			ReadOnly: true,
		}},
		callResult: map[string]any{"toolResult": "ok"},
	}
	toolset, err := BuildServerToolSet(context.Background(), "github", contracts.MCPServer{Command: "node"}, ServerToolOptions{
		OpenClient: func(context.Context, string, contracts.MCPServer) (ClientHandle, error) {
			return ClientHandle{Client: client}, nil
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	if toolset.ServerName != "github" || toolset.Client != client {
		t.Fatalf("toolset = %#v", toolset)
	}
	if len(toolset.Tools) != 5 {
		t.Fatalf("tools = %#v", toolset.Tools)
	}
	registry, err := tool.NewRegistry(toolset.Tools...)
	if err != nil {
		t.Fatal(err)
	}
	for _, name := range []string{
		"mcp__github__search",
		"mcp__github__list_resources",
		"mcp__github__read_resource",
		"mcp__github__list_prompts",
		"mcp__github__get_prompt",
	} {
		if _, ok := registry.Lookup(name); !ok {
			t.Fatalf("missing tool %q in %#v", name, registry.Names())
		}
	}

	result, err := (tool.Executor{Registry: registry}).Execute(tool.Context{Context: context.Background()}, contracts.ToolUse{
		ID:    "toolu_search",
		Name:  "mcp__github__search",
		Input: json.RawMessage(`{"query":"bugs"}`),
	}, nil)
	if err != nil {
		t.Fatal(err)
	}
	if result.Content != "ok" {
		t.Fatalf("result = %#v", result)
	}
}

func TestBuildServerToolSetsAggregatesSuccessesAndErrors(t *testing.T) {
	closed := []string{}
	result := BuildServerToolSets(context.Background(), map[string]contracts.MCPServer{
		"zeta":  {Command: "zeta"},
		"alpha": {Command: "alpha"},
		"bad":   {Command: "bad"},
	}, ServerToolOptions{
		DisableResources: true,
		DisablePrompts:   true,
		OpenClient: func(_ context.Context, name string, _ contracts.MCPServer) (ClientHandle, error) {
			if name == "bad" {
				return ClientHandle{}, errors.New("cannot start")
			}
			return ClientHandle{
				Client: &fakeMCPClient{tools: []RemoteTool{{
					Name:     "ping",
					ReadOnly: true,
				}}},
				Close: func() error {
					closed = append(closed, name)
					return nil
				},
			}, nil
		},
	})
	if len(result.Errors) != 1 || result.Errors[0].ServerName != "bad" {
		t.Fatalf("errors = %#v", result.Errors)
	}
	if len(result.Servers) != 2 || result.Servers[0].ServerName != "alpha" || result.Servers[1].ServerName != "zeta" {
		t.Fatalf("servers = %#v", result.Servers)
	}
	if len(result.Tools) != 2 {
		t.Fatalf("tools = %#v", result.Tools)
	}
	registry, err := result.Registry()
	if err != nil {
		t.Fatal(err)
	}
	if _, ok := registry.Lookup("mcp__alpha__ping"); !ok {
		t.Fatalf("missing alpha tool: %#v", registry.Names())
	}
	if _, ok := registry.Lookup("mcp__zeta__ping"); !ok {
		t.Fatalf("missing zeta tool: %#v", registry.Names())
	}
	if err := result.Close(); err != nil {
		t.Fatal(err)
	}
	if len(closed) != 2 || closed[0] != "alpha" || closed[1] != "zeta" {
		t.Fatalf("closed = %#v", closed)
	}
}

func TestBuildServerToolSetClosesClientWhenToolDiscoveryFails(t *testing.T) {
	closed := false
	client := &fakeMCPClient{listErr: errors.New("boom")}
	_, err := BuildServerToolSet(context.Background(), "github", contracts.MCPServer{Command: "node"}, ServerToolOptions{
		OpenClient: func(context.Context, string, contracts.MCPServer) (ClientHandle, error) {
			return ClientHandle{
				Client: client,
				Close: func() error {
					closed = true
					return nil
				},
			}, nil
		},
	})
	if err == nil {
		t.Fatal("expected discovery error")
	}
	if !closed {
		t.Fatal("expected client to be closed")
	}
}

func TestOpenServerClientRejectsUnsupportedTransport(t *testing.T) {
	_, err := OpenServerClient(context.Background(), "remote", contracts.MCPServer{
		Type: TransportHTTP,
		URL:  "https://example.com/mcp",
	})
	if err == nil || !strings.Contains(err.Error(), "not supported yet") {
		t.Fatalf("expected unsupported transport error, got %v", err)
	}
}
