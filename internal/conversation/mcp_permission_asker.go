// Package conversation provides the conversation runner and related types.
package conversation

import (
	"context"
	"encoding/json"
	"fmt"

	"ccgo/internal/contracts"
	"ccgo/internal/tool"
)

// MCPPermissionAsker implements tool.PermissionAsker by delegating to a named
// MCP tool. The tool is looked up from Registry at ask-time (lazy resolution
// allows the registry to be populated after the asker is constructed).
//
// The MCP permission tool receives a JSON object:
//
//	{"tool_name": "...", "input": {...}, "tool_use_id": "..."}
//
// and must return a JSON object with a "behavior" field ("allow" or "deny")
// and an optional "message" field.
//
// CLI-FLAG-44: --permission-prompt-tool
// CC ref: src/cli/structuredIO.ts:623; src/main.tsx:--permission-prompt-tool.
type MCPPermissionAsker struct {
	ToolName string
	Registry *tool.Registry
}

// Ask calls the named MCP tool with the permission request and returns the
// resolved PermissionDecision. When the tool cannot be found or returns an
// unexpected response, Ask falls back to deny so that missing or broken
// permission tools are fail-safe.
func (a *MCPPermissionAsker) Ask(ctx context.Context, req tool.PermissionAskRequest) (contracts.PermissionDecision, error) {
	deny := contracts.PermissionDecision{Behavior: contracts.PermissionDeny}
	if a.Registry == nil || a.ToolName == "" {
		return deny, nil
	}
	t, ok := a.Registry.Lookup(a.ToolName)
	if !ok {
		// Tool not (yet) registered — fail safe.
		return deny, fmt.Errorf("permission-prompt-tool %q not found in registry", a.ToolName)
	}
	// Build the input payload matching the CC structuredIO shape.
	payload := map[string]any{
		"tool_name":   req.ToolName,
		"tool_use_id": string(req.ToolUseID),
	}
	if req.Input != nil {
		payload["input"] = req.Input
	}
	rawInput, err := json.Marshal(payload)
	if err != nil {
		return deny, fmt.Errorf("mcp permission asker: marshal input: %w", err)
	}
	toolCtx := tool.Context{
		Context:          ctx,
		WorkingDirectory: "",
	}
	result, callErr := t.Call(toolCtx, rawInput, tool.NopProgressSink())
	if callErr != nil {
		return deny, fmt.Errorf("mcp permission asker: tool call failed: %w", callErr)
	}
	// Parse the tool result content as JSON: {"behavior":"allow"|"deny","message":"..."}.
	content, ok := result.Content.(string)
	if !ok {
		return deny, fmt.Errorf("mcp permission asker: unexpected result type %T", result.Content)
	}
	var response struct {
		Behavior string `json:"behavior"`
		Message  string `json:"message"`
	}
	if err := json.Unmarshal([]byte(content), &response); err != nil {
		return deny, fmt.Errorf("mcp permission asker: parse response: %w", err)
	}
	switch response.Behavior {
	case "allow":
		return contracts.PermissionDecision{
			Behavior: contracts.PermissionAllow,
			Message:  response.Message,
		}, nil
	default:
		return contracts.PermissionDecision{
			Behavior: contracts.PermissionDeny,
			Message:  response.Message,
		}, nil
	}
}
