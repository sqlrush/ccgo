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
				Name:                "Read",
				Aliases:             []string{"View"},
				Description:         "Read files from the workspace",
				ReadOnly:            true,
				ConcurrencySafe:     true,
				RequiresInteraction: true,
				ShouldDefer:         true,
				AlwaysLoad:          true,
				EagerInputStreaming: true,
				Strict:              true,
				InterruptBehavior:   "block",
				MaxResultSizeChars:  1024,
				CacheControl:        &contracts.CacheControl{Type: "ephemeral"},
				InputSchema: contracts.JSONSchema{
					"type": "object",
					"properties": map[string]any{
						"path": map[string]any{"type": "string"},
					},
				},
				OutputSchema: contracts.JSONSchema{
					"type": "object",
					"properties": map[string]any{
						"content": map[string]any{"type": "string"},
					},
				},
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
	if result.StructuredContent["matches"] != 1 || result.StructuredContent["limit"] != 1 {
		t.Fatalf("structured content = %#v", result.StructuredContent)
	}
	if result.StructuredContent["ranking"] != "bm25" {
		t.Fatalf("ranking = %#v", result.StructuredContent["ranking"])
	}
	if result.StructuredContent["query_type"] != "keyword" {
		t.Fatalf("query_type = %#v", result.StructuredContent["query_type"])
	}
	if text, ok := result.StructuredContent["text"].(string); !ok || !strings.Contains(text, "Tool search: view files") || !strings.Contains(text, "- Read: Read files from the workspace") {
		t.Fatalf("text = %#v", result.StructuredContent["text"])
	}
	contentReferences, ok := result.Content.([]contracts.ToolReference)
	if !ok || len(contentReferences) != 1 || contentReferences[0].ToolName != "Read" || contentReferences[0].Type != "tool_reference" {
		t.Fatalf("content references = %#v", result.Content)
	}
	toolReferences, ok := result.StructuredContent["tool_references"].([]contracts.ToolReference)
	if !ok || len(toolReferences) != 1 || toolReferences[0].ToolName != "Read" || toolReferences[0].Type != "tool_reference" {
		t.Fatalf("tool references = %#v", result.StructuredContent["tool_references"])
	}
	results, ok := result.StructuredContent["results"].([]map[string]any)
	if !ok || len(results) != 1 {
		t.Fatalf("results = %#v", result.StructuredContent["results"])
	}
	if results[0]["name"] != "Read" || results[0]["read_only"] != true || results[0]["concurrency_safe"] != true {
		t.Fatalf("result item = %#v", results[0])
	}
	for _, key := range []string{"requires_interaction", "should_defer", "always_load", "eager_input_streaming", "strict"} {
		if results[0][key] != true {
			t.Fatalf("%s = %#v, result item = %#v", key, results[0][key], results[0])
		}
	}
	if results[0]["max_result_size_chars"] != 1024 {
		t.Fatalf("max_result_size_chars = %#v", results[0]["max_result_size_chars"])
	}
	if cacheControl, ok := results[0]["cache_control"].(contracts.CacheControl); !ok || cacheControl.Type != "ephemeral" {
		t.Fatalf("cache_control = %#v", results[0]["cache_control"])
	}
	inputSchema, ok := results[0]["input_schema"].(contracts.JSONSchema)
	if !ok || inputSchema["type"] != "object" {
		t.Fatalf("input_schema = %#v", results[0]["input_schema"])
	}
	inputProperties, ok := inputSchema["properties"].(map[string]any)
	if !ok {
		t.Fatalf("input schema properties = %#v", inputSchema["properties"])
	}
	if pathProperty, ok := inputProperties["path"].(map[string]any); !ok || pathProperty["type"] != "string" {
		t.Fatalf("path property = %#v", inputProperties["path"])
	}
	outputSchema, ok := results[0]["output_schema"].(contracts.JSONSchema)
	if !ok || outputSchema["type"] != "object" {
		t.Fatalf("output_schema = %#v", results[0]["output_schema"])
	}
	outputProperties, ok := outputSchema["properties"].(map[string]any)
	if !ok {
		t.Fatalf("output schema properties = %#v", outputSchema["properties"])
	}
	if contentProperty, ok := outputProperties["content"].(map[string]any); !ok || contentProperty["type"] != "string" {
		t.Fatalf("content property = %#v", outputProperties["content"])
	}
	if aliases, ok := results[0]["aliases"].([]string); !ok || len(aliases) != 1 || aliases[0] != "View" {
		t.Fatalf("aliases = %#v", results[0]["aliases"])
	}
}

func TestToolSearchSelectReturnsToolReferences(t *testing.T) {
	definitions := []contracts.ToolDefinition{
		{
			Name:        "Read",
			Aliases:     []string{"View"},
			Description: "Read files",
		},
		{
			Name:        "Edit",
			Description: "Modify files",
		},
	}
	results, missing := selectToolDefinitions(definitions, []string{"Edit", "Missing", "view", "Read"})
	if len(results) != 2 || results[0].Definition.Name != "Edit" || results[1].Definition.Name != "Read" {
		t.Fatalf("selected = %#v", results)
	}
	if len(missing) != 1 || missing[0] != "Missing" {
		t.Fatalf("missing = %#v", missing)
	}

	registry, err := tool.NewRegistry(
		tool.FuncTool{
			DefinitionValue: definitions[0],
			CallFunc: func(tool.Context, json.RawMessage, tool.ProgressSink) (contracts.ToolResult, error) {
				return contracts.ToolResult{Content: "read"}, nil
			},
		},
		tool.FuncTool{
			DefinitionValue: definitions[1],
			CallFunc: func(tool.Context, json.RawMessage, tool.ProgressSink) (contracts.ToolResult, error) {
				return contracts.ToolResult{Content: "edit"}, nil
			},
		},
		NewToolSearchTool(),
	)
	if err != nil {
		t.Fatal(err)
	}
	result, err := tool.NewExecutor(registry).Execute(tool.Context{Context: context.Background()}, contracts.ToolUse{
		ID:    "toolu_search_select",
		Name:  "ToolSearch",
		Input: json.RawMessage(`{"query":"select:Edit,Missing,View"}`),
	}, nil)
	if err != nil {
		t.Fatal(err)
	}
	if result.StructuredContent["query_type"] != "select" || result.StructuredContent["ranking"] != "select" || result.StructuredContent["matches"] != 2 {
		t.Fatalf("structured content = %#v", result.StructuredContent)
	}
	contentReferences, ok := result.Content.([]contracts.ToolReference)
	if !ok || len(contentReferences) != 2 || contentReferences[0].ToolName != "Edit" || contentReferences[1].ToolName != "Read" {
		t.Fatalf("content references = %#v", result.Content)
	}
	toolReferences, ok := result.StructuredContent["tool_references"].([]contracts.ToolReference)
	if !ok || len(toolReferences) != 2 || toolReferences[0].ToolName != "Edit" || toolReferences[1].ToolName != "Read" {
		t.Fatalf("tool references = %#v", result.StructuredContent["tool_references"])
	}
	missingNames, ok := result.StructuredContent["missing"].([]string)
	if !ok || len(missingNames) != 1 || missingNames[0] != "Missing" {
		t.Fatalf("missing = %#v", result.StructuredContent["missing"])
	}
	if text := result.StructuredContent["text"].(string); !strings.Contains(text, "Missing: Missing") {
		t.Fatalf("text = %#v", text)
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

func TestToolSearchMatchesInputSchemaFields(t *testing.T) {
	results := matchToolDefinitions([]contracts.ToolDefinition{
		{
			Name:        "Runner",
			Description: "Execute configured actions",
			InputSchema: contracts.JSONSchema{
				"type":     "object",
				"required": []any{"file_path"},
				"properties": map[string]any{
					"file_path": map[string]any{
						"description": "Absolute path to inspect",
					},
				},
			},
		},
		{
			Name:        "Other",
			Description: "General helper",
		},
	}, "file path", 2)
	if len(results) != 1 || results[0].Definition.Name != "Runner" {
		t.Fatalf("results = %#v, want Runner from input schema match", results)
	}
}

func TestToolSearchMatchesOutputSchemaFields(t *testing.T) {
	results := matchToolDefinitions([]contracts.ToolDefinition{
		{
			Name:        "Collector",
			Description: "Collects server output",
			OutputSchema: contracts.JSONSchema{
				"type": "object",
				"properties": map[string]any{
					"severity": map[string]any{
						"enum": []any{"error", "warning"},
					},
				},
			},
		},
		{
			Name:        "Other",
			Description: "General helper",
		},
	}, "severity warning", 2)
	if len(results) != 1 || results[0].Definition.Name != "Collector" {
		t.Fatalf("results = %#v, want Collector from output schema match", results)
	}
}

func TestCopyJSONSchemaDeepCopiesNestedMapsAndSlices(t *testing.T) {
	original := contracts.JSONSchema{
		"type":     "object",
		"required": []any{"path"},
		"properties": map[string]any{
			"path": map[string]any{"type": "string"},
		},
	}
	copied := copyJSONSchema(original)
	copied["required"].([]any)[0] = "other"
	copied["properties"].(map[string]any)["path"].(map[string]any)["type"] = "number"

	if original["required"].([]any)[0] != "path" {
		t.Fatalf("original required was mutated: %#v", original["required"])
	}
	pathProperty := original["properties"].(map[string]any)["path"].(map[string]any)
	if pathProperty["type"] != "string" {
		t.Fatalf("original nested property was mutated: %#v", pathProperty)
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
