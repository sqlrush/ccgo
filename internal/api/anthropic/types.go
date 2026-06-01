package anthropic

import (
	"encoding/json"

	"ccgo/internal/contracts"
)

const DefaultVersion = "2023-06-01"
const DefaultBaseURL = "https://api.anthropic.com"

type Request struct {
	Model       string                 `json:"model"`
	MaxTokens   int                    `json:"max_tokens"`
	Messages    []contracts.APIMessage `json:"messages"`
	System      any                    `json:"system,omitempty"`
	Tools       []ToolDefinition       `json:"tools,omitempty"`
	ToolChoice  any                    `json:"tool_choice,omitempty"`
	Temperature *float64               `json:"temperature,omitempty"`
	TopP        *float64               `json:"top_p,omitempty"`
	TopK        *int                   `json:"top_k,omitempty"`
	Metadata    map[string]any         `json:"metadata,omitempty"`
	Thinking    map[string]any         `json:"thinking,omitempty"`
	Stream      bool                   `json:"stream,omitempty"`
}

type ToolDefinition struct {
	Name                string                  `json:"name"`
	Description         string                  `json:"description,omitempty"`
	InputSchema         contracts.JSONSchema    `json:"input_schema"`
	Strict              bool                    `json:"strict,omitempty"`
	DeferLoading        bool                    `json:"defer_loading,omitempty"`
	EagerInputStreaming bool                    `json:"eager_input_streaming,omitempty"`
	CacheControl        *contracts.CacheControl `json:"cache_control,omitempty"`
}

func ToolFromContract(def contracts.ToolDefinition) ToolDefinition {
	return ToolDefinition{
		Name:        def.Name,
		Description: def.Description,
		InputSchema: def.InputSchema,
	}
}

func ToolsFromContracts(defs []contracts.ToolDefinition) []ToolDefinition {
	out := make([]ToolDefinition, 0, len(defs))
	for _, def := range defs {
		out = append(out, ToolFromContract(def))
	}
	return out
}

type Response struct {
	ID           string                   `json:"id"`
	Type         string                   `json:"type"`
	Role         string                   `json:"role"`
	Model        string                   `json:"model"`
	Content      []contracts.ContentBlock `json:"content"`
	StopReason   string                   `json:"stop_reason,omitempty"`
	StopSequence string                   `json:"stop_sequence,omitempty"`
	Usage        contracts.Usage          `json:"usage,omitempty"`
	Raw          json.RawMessage          `json:"-"`
}

type StreamEvent struct {
	Event        string                  `json:"event,omitempty"`
	Type         string                  `json:"type"`
	Index        int                     `json:"index,omitempty"`
	Message      *Response               `json:"message,omitempty"`
	ContentBlock *contracts.ContentBlock `json:"content_block,omitempty"`
	Delta        map[string]any          `json:"delta,omitempty"`
	Usage        *contracts.Usage        `json:"usage,omitempty"`
	Raw          json.RawMessage         `json:"-"`
}

type MessageDelta struct {
	StopReason   string          `json:"stop_reason,omitempty"`
	StopSequence string          `json:"stop_sequence,omitempty"`
	Usage        contracts.Usage `json:"usage,omitempty"`
}

func (e StreamEvent) TextDelta() string {
	if e.Delta == nil {
		return ""
	}
	text, _ := e.Delta["text"].(string)
	return text
}
