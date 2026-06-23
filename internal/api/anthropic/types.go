package anthropic

import (
	"encoding/json"
	"strings"

	"ccgo/internal/contracts"
)

const DefaultVersion = "2023-06-01"
const DefaultBaseURL = "https://api.anthropic.com"

type Request struct {
	Model        string                 `json:"model"`
	MaxTokens    int                    `json:"max_tokens"`
	Messages     []contracts.APIMessage `json:"messages"`
	System       any                    `json:"system,omitempty"`
	Tools        []ToolDefinition       `json:"tools,omitempty"`
	ToolChoice   any                    `json:"tool_choice,omitempty"`
	Temperature  *float64               `json:"temperature,omitempty"`
	TopP         *float64               `json:"top_p,omitempty"`
	TopK         *int                   `json:"top_k,omitempty"`
	Metadata     map[string]any         `json:"metadata,omitempty"`
	Thinking     map[string]any         `json:"thinking,omitempty"`
	OutputConfig map[string]any         `json:"output_config,omitempty"` // CC: output_config.effort (effort-2025-11-24 beta)
	Stream       bool                   `json:"stream,omitempty"`

	// ToolSearchActive indicates that deferred tool search is active for this
	// request; DynamicBetaHeaders will include ToolSearchBetaHeader when true.
	// Not serialized to JSON — used only for beta-header selection.
	// CC reference: betas.ts TOOL_SEARCH_BETA_HEADER_1P; claude.ts:1174-1182.
	ToolSearchActive bool `json:"-"`

	// UseGlobalCacheScope indicates that global (cross-user) prompt caching
	// is enabled; DynamicBetaHeaders ensures PromptCachingScopeBetaHeader is
	// included and buildRequest attaches scope:"global" to system blocks.
	// CC reference: betas.ts:shouldUseGlobalCacheScope; claude.ts:1207-1229.
	UseGlobalCacheScope bool `json:"-"`
}

type CountTokensRequest struct {
	Model    string                 `json:"model"`
	Messages []contracts.APIMessage `json:"messages"`
	System   any                    `json:"system,omitempty"`
	Tools    []ToolDefinition       `json:"tools,omitempty"`
	Thinking map[string]any         `json:"thinking,omitempty"`
}

type CountTokensResponse struct {
	InputTokens int `json:"input_tokens"`
}

// ToolDefinition covers both client-side tools and server-side tools (e.g.
// web_search_20250305). Server-tool fields are omitempty so they are absent
// from the JSON of ordinary client tools — the serialized shape is therefore
// byte-identical to before this change for all existing client tool requests.
//
// The Anthropic API expects server tools to appear as entries inside the same
// "tools" array as client tools (not a separate top-level key). Use
// NewWebSearchToolDefinition to construct the server-tool entry.
// Reference: CC WebSearchTool.ts:76-84, extraToolSchemas line 284.
type ToolDefinition struct {
	// Client-tool fields.
	Name                string                  `json:"name"`
	Description         string                  `json:"description,omitempty"`
	InputSchema         contracts.JSONSchema    `json:"input_schema,omitempty"`
	Strict              bool                    `json:"strict,omitempty"`
	DeferLoading        bool                    `json:"defer_loading,omitempty"`
	EagerInputStreaming bool                    `json:"eager_input_streaming,omitempty"`
	CacheControl        *contracts.CacheControl `json:"cache_control,omitempty"`

	// Server-tool fields (omitempty — absent for all client tools).
	// When Type is set (e.g. "web_search_20250305") the entry is a server tool
	// and Name/Description/InputSchema are ignored by the API.
	Type           string   `json:"type,omitempty"`
	AllowedDomains []string `json:"allowed_domains,omitempty"`
	BlockedDomains []string `json:"blocked_domains,omitempty"`
	MaxUses        int      `json:"max_uses,omitempty"`
}

// NewWebSearchToolDefinition returns a ToolDefinition that represents the
// web_search_20250305 server tool. Append it to Request.Tools to enable
// server-side web search. Mirrors CC WebSearchTool.ts makeToolSchema (line 76).
func NewWebSearchToolDefinition(allowedDomains, blockedDomains []string, maxUses int) ToolDefinition {
	if maxUses <= 0 {
		maxUses = 8 // CC default (WebSearchTool.ts:82)
	}
	return ToolDefinition{
		Type:           "web_search_20250305",
		Name:           "web_search",
		AllowedDomains: allowedDomains,
		BlockedDomains: blockedDomains,
		MaxUses:        maxUses,
	}
}

func ToolFromContract(def contracts.ToolDefinition) ToolDefinition {
	return ToolDefinition{
		Name:                def.Name,
		Description:         toolDescriptionFromContract(def),
		InputSchema:         def.InputSchema,
		Strict:              def.Strict,
		DeferLoading:        toolDeferLoadingFromContract(def),
		EagerInputStreaming: def.EagerInputStreaming,
		CacheControl:        copyCacheControl(def.CacheControl),
	}
}

func toolDeferLoadingFromContract(def contracts.ToolDefinition) bool {
	if def.AlwaysLoad {
		return false
	}
	return def.ShouldDefer || def.MCP != nil
}

func toolDescriptionFromContract(def contracts.ToolDefinition) string {
	for _, value := range []string{def.Description, def.Prompt, def.SearchHint} {
		if trimmed := strings.TrimSpace(value); trimmed != "" {
			return trimmed
		}
	}
	return ""
}

func copyCacheControl(value *contracts.CacheControl) *contracts.CacheControl {
	if value == nil {
		return nil
	}
	copied := *value
	return &copied
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
