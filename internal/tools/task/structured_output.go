package tasktools

import (
	"encoding/json"
	"fmt"

	"ccgo/internal/contracts"
	"ccgo/internal/tool"
)

// NewStructuredOutputTool returns the StructuredOutput tool, which is used by
// headless/SDK callers to return a final response as structured JSON.
func NewStructuredOutputTool() tool.Tool {
	return tool.FuncTool{
		DefinitionValue: contracts.ToolDefinition{
			Name:               "StructuredOutput",
			Description:        "Return structured output in the requested format",
			SearchHint:         "return the final response as structured JSON",
			ReadOnly:           true,
			ConcurrencySafe:    true,
			MaxResultSizeChars: 100_000,
			InputSchema: contracts.JSONSchema{
				"type":                 "object",
				"additionalProperties": true,
			},
		},
		PromptFunc: func(tool.PromptContext) (string, error) {
			return "Use this tool to return your final response in the requested structured format. You MUST call this tool exactly once at the end of your response to provide the structured output.", nil
		},
		PermissionFunc: func(_ tool.Context, _ json.RawMessage) (contracts.PermissionDecision, error) {
			return contracts.PermissionDecision{
				Behavior:       contracts.PermissionAllow,
				DecisionReason: "StructuredOutput always allowed",
			}, nil
		},
		ValidateFunc: func(_ tool.Context, raw json.RawMessage) error {
			var obj map[string]json.RawMessage
			if err := json.Unmarshal(raw, &obj); err != nil {
				return fmt.Errorf("input must be a JSON object: %w", err)
			}
			return nil
		},
		CallFunc: func(_ tool.Context, raw json.RawMessage, _ tool.ProgressSink) (contracts.ToolResult, error) {
			var obj map[string]any
			if err := json.Unmarshal(raw, &obj); err != nil {
				return contracts.ToolResult{}, fmt.Errorf("invalid input: %w", err)
			}
			return contracts.ToolResult{
				Content:           "Structured output provided successfully",
				StructuredContent: obj,
			}, nil
		},
		ReadOnlyFunc:    func(json.RawMessage) bool { return true },
		ConcurrencyFunc: func(json.RawMessage) bool { return true },
	}
}
