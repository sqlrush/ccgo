package lsptools

import (
	"encoding/json"
	"fmt"
	"strings"

	"ccgo/internal/contracts"
	"ccgo/internal/tool"
)

// lspOperations is the complete set of 9 operations exposed by the LSP tool,
// matching the CC discriminated-union from schemas.ts:8-215.
var lspOperations = map[string]struct{}{
	"goToDefinition":       {},
	"findReferences":       {},
	"hover":                {},
	"documentSymbol":       {},
	"workspaceSymbol":      {},
	"goToImplementation":   {},
	"prepareCallHierarchy": {},
	"incomingCalls":        {},
	"outgoingCalls":        {},
}

type lspInput struct {
	Operation string `json:"operation"`
	FilePath  string `json:"filePath"`
	Line      int    `json:"line"`
	Character int    `json:"character"`
}

// NewLSPTool returns a Tool named "LSP" that exposes 9 navigation operations
// matching the CC reference (schemas.ts discriminatedUnion). The internal/lsp
// package currently has no navigation methods (only diagnostics/process
// management), so all 9 operations degrade gracefully with a "not supported"
// message rather than an error. When the backend grows to support individual
// ops, wire them in dispatchLSP.
func NewLSPTool() tool.Tool {
	return tool.FuncTool{
		DefinitionValue: contracts.ToolDefinition{
			Name:            "LSP",
			Description:     "Query a language server for code navigation and symbol information.",
			ReadOnly:        true,
			ConcurrencySafe: true,
			InputSchema: contracts.JSONSchema{
				"type":     "object",
				"required": []any{"operation", "filePath", "line", "character"},
				"properties": map[string]any{
					"operation": map[string]any{
						"type":        "string",
						"description": "The LSP navigation operation to perform.",
						"enum": []any{
							"goToDefinition",
							"findReferences",
							"hover",
							"documentSymbol",
							"workspaceSymbol",
							"goToImplementation",
							"prepareCallHierarchy",
							"incomingCalls",
							"outgoingCalls",
						},
					},
					"filePath": map[string]any{
						"type":        "string",
						"description": "The absolute or relative path to the file.",
					},
					"line": map[string]any{
						"type":        "integer",
						"minimum":     1,
						"description": "The line number (1-based, as shown in editors).",
					},
					"character": map[string]any{
						"type":        "integer",
						"minimum":     1,
						"description": "The character offset (1-based, as shown in editors).",
					},
				},
			},
		},
		PromptFunc: func(tool.PromptContext) (string, error) {
			return "Queries the language server for code navigation and symbol information. " +
				"Operations: goToDefinition, findReferences, hover, documentSymbol, workspaceSymbol, " +
				"goToImplementation, prepareCallHierarchy, incomingCalls, outgoingCalls. " +
				"Provide filePath plus 1-based line and character position.", nil
		},
		ValidateFunc: validateLSP,
		PermissionFunc: func(_ tool.Context, _ json.RawMessage) (contracts.PermissionDecision, error) {
			return contracts.PermissionDecision{
				Behavior:       contracts.PermissionAllow,
				DecisionReason: "LSP navigation queries are read-only",
			}, nil
		},
		CallFunc:        callLSP,
		ReadOnlyFunc:    func(json.RawMessage) bool { return true },
		ConcurrencyFunc: func(json.RawMessage) bool { return true },
	}
}

func validateLSP(_ tool.Context, raw json.RawMessage) error {
	input, err := decodeLSPInput(raw)
	if err != nil {
		return err
	}
	if _, ok := lspOperations[input.Operation]; !ok {
		ops := make([]string, 0, len(lspOperations))
		for op := range lspOperations {
			ops = append(ops, op)
		}
		return fmt.Errorf("unsupported operation %q; must be one of the 9 LSP navigation operations", input.Operation)
	}
	if strings.TrimSpace(input.FilePath) == "" {
		return fmt.Errorf("filePath is required and must not be empty")
	}
	if input.Line < 1 {
		return fmt.Errorf("line must be >= 1 (1-based); got %d", input.Line)
	}
	if input.Character < 1 {
		return fmt.Errorf("character must be >= 1 (1-based); got %d", input.Character)
	}
	return nil
}

func callLSP(ctx tool.Context, raw json.RawMessage, _ tool.ProgressSink) (contracts.ToolResult, error) {
	input, err := decodeLSPInput(raw)
	if err != nil {
		return contracts.ToolResult{}, fmt.Errorf("lsp: decode input: %w", err)
	}
	// Dispatch to internal/lsp backend. Only call confirmed methods; for ops
	// the backend cannot serve, return a graceful unsupported result so the
	// turn can continue without an error.
	result, supported := dispatchLSP(ctx, input)
	if !supported {
		return contracts.ToolResult{
			Content: fmt.Sprintf(
				"LSP operation %q is not supported by the configured language server. "+
					"The language server backend does not yet expose a method for this operation.",
				input.Operation,
			),
			StructuredContent: map[string]any{
				"type":      "lsp",
				"operation": input.Operation,
				"supported": false,
			},
		}, nil
	}
	return result, nil
}

func decodeLSPInput(raw json.RawMessage) (lspInput, error) {
	if len(raw) == 0 {
		return lspInput{}, fmt.Errorf("lsp: input is required")
	}
	var input lspInput
	if err := json.Unmarshal(raw, &input); err != nil {
		return lspInput{}, fmt.Errorf("lsp: decode input: %w", err)
	}
	return input, nil
}

// dispatchLSP routes the operation to the internal/lsp client.
//
// The internal/lsp package currently provides only diagnostics and server
// process management — no navigation methods (GoToDefinition, FindReferences,
// Hover, DocumentSymbol, WorkspaceSymbol, GoToImplementation,
// PrepareCallHierarchy, IncomingCalls, OutgoingCalls). Until those methods are
// added to the backend, every operation returns (zero, false) so callLSP
// responds with the graceful "not supported" message. Wire confirmed methods
// here incrementally as the LSP client grows.
func dispatchLSP(_ tool.Context, _ lspInput) (contracts.ToolResult, bool) {
	// No navigation methods exist yet in internal/lsp.
	// Return supported=false for all 9 operations; callLSP handles the rest.
	return contracts.ToolResult{}, false
}
