package anthropic

import (
	"encoding/json"
	"testing"

	"ccgo/internal/contracts"
)

func TestToolFromContractPreservesStrictAndDeferLoading(t *testing.T) {
	cacheControl := &contracts.CacheControl{Type: "ephemeral", Scope: "global", TTL: "1h"}
	got := ToolFromContract(contracts.ToolDefinition{
		Name:        "Task",
		Description: "Start a task",
		InputSchema: contracts.JSONSchema{
			"type": "object",
		},
		Strict:              true,
		ShouldDefer:         true,
		EagerInputStreaming: true,
		CacheControl:        cacheControl,
	})
	if got.Name != "Task" || got.Description != "Start a task" || got.InputSchema["type"] != "object" {
		t.Fatalf("tool = %#v", got)
	}
	if !got.Strict {
		t.Fatalf("strict = false, want true")
	}
	if !got.DeferLoading {
		t.Fatalf("defer loading = false, want true")
	}
	if !got.EagerInputStreaming {
		t.Fatalf("eager input streaming = false, want true")
	}
	if got.CacheControl == nil || got.CacheControl.Type != "ephemeral" || got.CacheControl.Scope != "global" || got.CacheControl.TTL != "1h" {
		t.Fatalf("cache control = %#v", got.CacheControl)
	}
	got.CacheControl.Scope = "mutated"
	if cacheControl.Scope != "global" {
		t.Fatalf("cache control was aliased: %#v", cacheControl)
	}
}

func TestToolFromContractAlwaysLoadOverridesShouldDefer(t *testing.T) {
	got := ToolFromContract(contracts.ToolDefinition{
		Name:        "Task",
		InputSchema: contracts.JSONSchema{"type": "object"},
		ShouldDefer: true,
		AlwaysLoad:  true,
	})
	if got.DeferLoading {
		t.Fatalf("defer loading = true, want false when always_load is set")
	}
}

func TestToolFromContractDefersMCPToolsUnlessAlwaysLoad(t *testing.T) {
	got := ToolFromContract(contracts.ToolDefinition{
		Name:        "mcp__github__search",
		InputSchema: contracts.JSONSchema{"type": "object"},
		MCP:         &contracts.MCPToolRef{ServerName: "github", ToolName: "search"},
	})
	if !got.DeferLoading {
		t.Fatalf("defer loading = false, want true for MCP tools")
	}
	got = ToolFromContract(contracts.ToolDefinition{
		Name:        "mcp__github__search",
		InputSchema: contracts.JSONSchema{"type": "object"},
		AlwaysLoad:  true,
		MCP:         &contracts.MCPToolRef{ServerName: "github", ToolName: "search"},
	})
	if got.DeferLoading {
		t.Fatalf("defer loading = true, want false when always_load is set")
	}
}

func TestToolFromContractDescriptionFallback(t *testing.T) {
	cases := []struct {
		name string
		def  contracts.ToolDefinition
		want string
	}{
		{
			name: "description",
			def: contracts.ToolDefinition{
				Name:        "Primary",
				Description: "Primary description",
				Prompt:      "Prompt description",
				SearchHint:  "search hint",
			},
			want: "Primary description",
		},
		{
			name: "prompt",
			def: contracts.ToolDefinition{
				Name:       "PromptOnly",
				Prompt:     "Prompt description",
				SearchHint: "search hint",
			},
			want: "Prompt description",
		},
		{
			name: "search hint",
			def: contracts.ToolDefinition{
				Name:       "HintOnly",
				SearchHint: "search hint",
			},
			want: "search hint",
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := ToolFromContract(tc.def)
			if got.Description != tc.want {
				t.Fatalf("description = %q, want %q", got.Description, tc.want)
			}
		})
	}
}

// TestClientToolMarshalHasNoServerToolKeys verifies that a ToolDefinition built
// from a client tool contract serializes with NO type, max_uses, allowed_domains,
// blocked_domains, or server_tools top-level keys — i.e. byte-compatible with
// the pre-fix wire format for all ordinary requests.
func TestClientToolMarshalHasNoServerToolKeys(t *testing.T) {
	td := ToolFromContract(contracts.ToolDefinition{
		Name:        "Bash",
		Description: "Run a bash command",
		InputSchema: contracts.JSONSchema{"type": "object", "properties": map[string]any{"cmd": map[string]any{"type": "string"}}},
	})
	b, err := json.Marshal(td)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	// Parse and check top-level keys only (not nested values inside input_schema).
	var parsed map[string]any
	if err := json.Unmarshal(b, &parsed); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	for _, forbidden := range []string{"type", "max_uses", "allowed_domains", "blocked_domains", "server_tools"} {
		if _, ok := parsed[forbidden]; ok {
			t.Fatalf("client tool JSON must not contain top-level %q key; got: %s", forbidden, b)
		}
	}
	// Must still have the regular client-tool keys.
	for _, required := range []string{"name", "input_schema"} {
		if _, ok := parsed[required]; !ok {
			t.Fatalf("client tool JSON missing top-level %q key; got: %s", required, b)
		}
	}
}

// TestWebSearchToolDefinitionMarshalInToolsArray verifies that a Request that
// includes a web_search server tool serializes it INSIDE the "tools" array
// (not as a separate top-level "server_tools" key), matching the Anthropic API
// contract (CC WebSearchTool.ts:76-84, extraToolSchemas line 284).
func TestWebSearchToolDefinitionMarshalInToolsArray(t *testing.T) {
	clientTool := ToolFromContract(contracts.ToolDefinition{
		Name:        "Read",
		Description: "Read a file",
		InputSchema: contracts.JSONSchema{"type": "object"},
	})
	serverTool := NewWebSearchToolDefinition([]string{"example.com"}, nil, 5)

	req := Request{
		Model:     "claude-sonnet-4-5",
		MaxTokens: 1024,
		Messages:  []contracts.APIMessage{{Role: "user", Content: []contracts.ContentBlock{contracts.NewTextBlock("search this")}}},
		Tools:     []ToolDefinition{clientTool, serverTool},
	}
	b, err := json.Marshal(req)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	var parsed map[string]any
	if err := json.Unmarshal(b, &parsed); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}

	// Must NOT have a top-level "server_tools" key.
	if _, ok := parsed["server_tools"]; ok {
		t.Fatalf("request must not have top-level server_tools key; JSON: %s", b)
	}

	// The "tools" array must contain both entries.
	toolsRaw, ok := parsed["tools"].([]any)
	if !ok || len(toolsRaw) != 2 {
		t.Fatalf("expected tools array with 2 entries; JSON: %s", b)
	}

	// The second entry must be the server tool with the right fields.
	serverEntry, ok := toolsRaw[1].(map[string]any)
	if !ok {
		t.Fatalf("tools[1] is not an object; JSON: %s", b)
	}
	if serverEntry["type"] != "web_search_20250305" {
		t.Fatalf("tools[1].type = %v, want web_search_20250305; JSON: %s", serverEntry["type"], b)
	}
	if serverEntry["name"] != "web_search" {
		t.Fatalf("tools[1].name = %v, want web_search; JSON: %s", serverEntry["name"], b)
	}
	if serverEntry["max_uses"] != float64(5) {
		t.Fatalf("tools[1].max_uses = %v, want 5; JSON: %s", serverEntry["max_uses"], b)
	}
	allowed, _ := serverEntry["allowed_domains"].([]any)
	if len(allowed) != 1 || allowed[0] != "example.com" {
		t.Fatalf("tools[1].allowed_domains = %v; JSON: %s", allowed, b)
	}
	// Must NOT have input_schema on the server tool.
	if _, ok := serverEntry["input_schema"]; ok {
		t.Fatalf("server tool must not have input_schema key; JSON: %s", b)
	}

	// The first entry must be the client tool — no type/max_uses keys.
	clientEntry, ok := toolsRaw[0].(map[string]any)
	if !ok {
		t.Fatalf("tools[0] is not an object; JSON: %s", b)
	}
	if _, ok := clientEntry["type"]; ok {
		t.Fatalf("client tool must not have type key; JSON: %s", b)
	}
	if _, ok := clientEntry["max_uses"]; ok {
		t.Fatalf("client tool must not have max_uses key; JSON: %s", b)
	}
}
