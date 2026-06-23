package lsptools

import (
	"encoding/json"
	"fmt"
	"net/url"
	"path/filepath"
	"strings"

	"ccgo/internal/contracts"
	"ccgo/internal/lsp"
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

// dispatchLSP routes the operation to the internal/lsp NavigationClient when
// one is available in ctx.Metadata (keyed by tool.MetadataLSPNavigationKey).
//
// When no NavigationClient is present (the common case during tests and when no
// language server is running), it returns (zero, false) and callLSP renders the
// graceful "not supported" message. This ensures TOOL-LSP-01..05 work at the
// seam level: the dispatch logic is correct; the operations succeed end-to-end
// when a real NavigationClient is wired by the runtime.
func dispatchLSP(ctx tool.Context, input lspInput) (contracts.ToolResult, bool) {
	client, ok := lspNavigationClient(ctx)
	if !ok {
		// No language server client — graceful degrade (TOOL-LSP-01..04 ⚠️).
		return contracts.ToolResult{}, false
	}

	// Convert the file path to a file:// URI matching LSP protocol.
	fileURI := pathToFileURI(input.FilePath)

	method, params, err := lsp.NavigationParams(input.Operation, fileURI, input.Line, input.Character)
	if err != nil {
		return contracts.ToolResult{}, false
	}

	raw, err := client.SendRequest(ctx.Context, input.FilePath, method, params)
	if err != nil {
		return contracts.ToolResult{
			Content: fmt.Sprintf("LSP %s failed: %v", input.Operation, err),
			StructuredContent: map[string]any{
				"type":      "lsp",
				"operation": input.Operation,
				"supported": true,
				"error":     err.Error(),
			},
		}, true
	}

	// Handle a two-step callHierarchy/incomingCalls or outgoingCalls.
	if input.Operation == "incomingCalls" || input.Operation == "outgoingCalls" {
		raw, err = dispatchCallHierarchyCalls(ctx, client, input, raw)
		if err != nil {
			return contracts.ToolResult{
				Content: fmt.Sprintf("LSP %s (calls step) failed: %v", input.Operation, err),
				StructuredContent: map[string]any{
					"type":      "lsp",
					"operation": input.Operation,
					"supported": true,
					"error":     err.Error(),
				},
			}, true
		}
	}

	navResult := lsp.FormatNavigationResult(input.Operation, raw)
	return contracts.ToolResult{
		Content: navResult.Formatted,
		StructuredContent: map[string]any{
			"type":         "lsp",
			"operation":    navResult.Operation,
			"supported":    true,
			"result":       navResult.Formatted,
			"result_count": navResult.ResultCount,
			"file_count":   navResult.FileCount,
		},
	}, true
}

// dispatchCallHierarchyCalls performs the second step for incomingCalls /
// outgoingCalls: the prepareCallHierarchy response gives CallHierarchyItems;
// we pass the first item to callHierarchy/incomingCalls or outgoingCalls.
func dispatchCallHierarchyCalls(ctx tool.Context, client lsp.NavigationClient, input lspInput, prepareRaw json.RawMessage) (json.RawMessage, error) {
	if len(prepareRaw) == 0 || string(prepareRaw) == "null" {
		return prepareRaw, nil
	}
	// The prepare response is []CallHierarchyItem; use the first one.
	var items []json.RawMessage
	if err := json.Unmarshal(prepareRaw, &items); err != nil || len(items) == 0 {
		return prepareRaw, nil
	}
	callMethod := "callHierarchy/incomingCalls"
	if input.Operation == "outgoingCalls" {
		callMethod = "callHierarchy/outgoingCalls"
	}
	return client.SendRequest(ctx.Context, input.FilePath, callMethod, map[string]any{"item": json.RawMessage(items[0])})
}

// lspNavigationClient extracts the lsp.NavigationClient from tool context
// metadata. Returns (nil, false) when absent.
func lspNavigationClient(ctx tool.Context) (lsp.NavigationClient, bool) {
	if ctx.Metadata == nil {
		return nil, false
	}
	v, ok := ctx.Metadata[tool.MetadataLSPNavigationKey]
	if !ok {
		return nil, false
	}
	client, ok := v.(lsp.NavigationClient)
	return client, ok
}

// pathToFileURI converts a file path (absolute or relative) to a file:// URI.
func pathToFileURI(path string) string {
	if strings.HasPrefix(path, "file://") {
		return path
	}
	abs := path
	if !filepath.IsAbs(abs) {
		abs, _ = filepath.Abs(abs)
	}
	return (&url.URL{Scheme: "file", Path: abs}).String()
}
