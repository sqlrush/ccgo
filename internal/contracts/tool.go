package contracts

import "encoding/json"

type JSONSchema map[string]any

type ToolDefinition struct {
	Name                string        `json:"name"`
	Aliases             []string      `json:"aliases,omitempty"`
	Description         string        `json:"description,omitempty"`
	Prompt              string        `json:"prompt,omitempty"`
	SearchHint          string        `json:"searchHint,omitempty"`
	InputSchema         JSONSchema    `json:"input_schema,omitempty"`
	OutputSchema        JSONSchema    `json:"output_schema,omitempty"`
	ConcurrencySafe     bool          `json:"concurrency_safe,omitempty"`
	ReadOnly            bool          `json:"read_only,omitempty"`
	Destructive         bool          `json:"destructive,omitempty"`
	RequiresInteraction bool          `json:"requires_interaction,omitempty"`
	ShouldDefer         bool          `json:"should_defer,omitempty"`
	AlwaysLoad          bool          `json:"always_load,omitempty"`
	EagerInputStreaming bool          `json:"eager_input_streaming,omitempty"`
	CacheControl        *CacheControl `json:"cache_control,omitempty"`
	MaxResultSizeChars  int           `json:"max_result_size_chars,omitempty"`
	Strict              bool          `json:"strict,omitempty"`
	InterruptBehavior   string        `json:"interrupt_behavior,omitempty"`
	MCP                 *MCPToolRef   `json:"mcp,omitempty"`
}

type MCPToolRef struct {
	ServerName string `json:"server_name"`
	ToolName   string `json:"tool_name"`
}

type ToolUse struct {
	ID    ID              `json:"id"`
	Name  string          `json:"name"`
	Input json.RawMessage `json:"input"`
}

type ToolResult struct {
	ToolUseID         ID             `json:"tool_use_id"`
	Content           any            `json:"content,omitempty"`
	IsError           bool           `json:"is_error,omitempty"`
	NewMessages       []Message      `json:"new_messages,omitempty"`
	StructuredContent map[string]any `json:"structured_content,omitempty"`
	Meta              map[string]any `json:"_meta,omitempty"`
}

type ToolProgress struct {
	ToolUseID ID             `json:"tool_use_id"`
	Type      string         `json:"type"`
	Data      map[string]any `json:"data,omitempty"`
}
