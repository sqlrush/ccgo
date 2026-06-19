package searchtools

import (
	"context"
	"encoding/json"
	"strings"
	"testing"

	"ccgo/internal/contracts"
	"ccgo/internal/tool"
)

func TestToolSearchFindsToolsFromExecutorRegistry(t *testing.T) {
	registry, err := tool.NewRegistry(
		tool.FuncTool{
			DefinitionValue: contracts.ToolDefinition{
				Name:              "Read",
				Aliases:           []string{"View"},
				Description:       "Read files from the workspace",
				ReadOnly:          true,
				ConcurrencySafe:   true,
				InterruptBehavior: "block",
				InputSchema:       contracts.JSONSchema{"type": "object"},
			},
			CallFunc: func(tool.Context, json.RawMessage, tool.ProgressSink) (contracts.ToolResult, error) {
				return contracts.ToolResult{Content: "read"}, nil
			},
		},
		tool.FuncTool{
			DefinitionValue: contracts.ToolDefinition{
				Name:        "Edit",
				Description: "Modify files",
				InputSchema: contracts.JSONSchema{"type": "object"},
			},
			CallFunc: func(tool.Context, json.RawMessage, tool.ProgressSink) (contracts.ToolResult, error) {
				return contracts.ToolResult{Content: "edit"}, nil
			},
		},
		NewToolSearchTool(),
	)
	if err != nil {
		t.Fatal(err)
	}
	ctx := tool.Context{Context: context.Background(), Metadata: map[string]any{}}
	result, err := tool.NewExecutor(registry).Execute(ctx, contracts.ToolUse{
		ID:    "toolu_search",
		Name:  "ToolSearch",
		Input: json.RawMessage(`{"query":"view files","limit":"1"}`),
	}, nil)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(result.Content.(string), "Tool search: view files") || !strings.Contains(result.Content.(string), "- Read: Read files from the workspace") {
		t.Fatalf("content = %#v", result.Content)
	}
	if result.StructuredContent["matches"] != 1 || result.StructuredContent["limit"] != 1 {
		t.Fatalf("structured content = %#v", result.StructuredContent)
	}
	if result.StructuredContent["ranking"] != "bm25" {
		t.Fatalf("ranking = %#v", result.StructuredContent["ranking"])
	}
	results, ok := result.StructuredContent["results"].([]map[string]any)
	if !ok || len(results) != 1 {
		t.Fatalf("results = %#v", result.StructuredContent["results"])
	}
	if results[0]["name"] != "Read" || results[0]["read_only"] != true || results[0]["concurrency_safe"] != true {
		t.Fatalf("result item = %#v", results[0])
	}
	if aliases, ok := results[0]["aliases"].([]string); !ok || len(aliases) != 1 || aliases[0] != "View" {
		t.Fatalf("aliases = %#v", results[0]["aliases"])
	}
}

func TestToolSearchBM25RanksCamelCaseToolName(t *testing.T) {
	results := matchToolDefinitions([]contracts.ToolDefinition{
		{
			Name:        "GenericFetcher",
			Description: "Search web snippets and fetch fetch fetch repeated context",
		},
		{
			Name:        "WebFetch",
			Description: "Fetch web page content",
		},
		{
			Name:        "FetchWebTelemetry",
			Description: "Fetch telemetry from web services",
		},
	}, "web fetch", 3)
	if len(results) == 0 {
		t.Fatal("expected results")
	}
	if results[0].Definition.Name != "WebFetch" {
		t.Fatalf("first result = %s, want WebFetch; results = %#v", results[0].Definition.Name, results)
	}
	if results[0].Score <= 0 {
		t.Fatalf("score = %v, want positive", results[0].Score)
	}
}

func TestToolSearchReportsNoMatches(t *testing.T) {
	registry, err := tool.NewRegistry(
		tool.FuncTool{
			DefinitionValue: contracts.ToolDefinition{
				Name:        "Read",
				Description: "Read files",
				InputSchema: contracts.JSONSchema{"type": "object"},
			},
			CallFunc: func(tool.Context, json.RawMessage, tool.ProgressSink) (contracts.ToolResult, error) {
				return contracts.ToolResult{}, nil
			},
		},
		NewToolSearchTool(),
	)
	if err != nil {
		t.Fatal(err)
	}
	result, err := tool.NewExecutor(registry).Execute(tool.Context{Context: context.Background()}, contracts.ToolUse{
		ID:    "toolu_search_none",
		Name:  "ToolSearch",
		Input: json.RawMessage(`{"query":"database migration"}`),
	}, nil)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(result.Content.(string), "No tools matched.") || result.StructuredContent["matches"] != 0 {
		t.Fatalf("result = %#v", result)
	}
}

func TestToolSearchRequiresRegistryMetadataWhenCalledDirectly(t *testing.T) {
	_, err := callToolSearch(tool.Context{Context: context.Background()}, json.RawMessage(`{"query":"read"}`), nil)
	if err == nil || !strings.Contains(err.Error(), "tool registry metadata is unavailable") {
		t.Fatalf("err = %v", err)
	}
}
