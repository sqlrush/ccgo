package lsptools

import (
	"encoding/json"
	"fmt"
	"strings"

	"ccgo/internal/contracts"
	"ccgo/internal/lsp"
	"ccgo/internal/tool"
)

const (
	diagnosticsDefaultLimit = 50
	diagnosticsMaxLimit     = 200
)

type diagnosticsInput struct {
	FilePath string `json:"file_path,omitempty"`
	Severity string `json:"severity,omitempty"`
	Limit    *int   `json:"limit,omitempty"`
}

func NewDiagnosticsTool() tool.Tool {
	return tool.FuncTool{
		DefinitionValue: contracts.ToolDefinition{
			Name:            "LSPDiagnostics",
			Description:     "Reads language-server diagnostics recorded for the current session.",
			ReadOnly:        true,
			ConcurrencySafe: true,
			InputSchema: contracts.JSONSchema{
				"type": "object",
				"properties": map[string]any{
					"file_path": map[string]any{
						"type":        "string",
						"description": "Optional workspace-relative file path to filter diagnostics.",
					},
					"severity": map[string]any{
						"type":        "string",
						"enum":        []any{"error", "warning", "info", "hint"},
						"description": "Optional severity filter.",
					},
					"limit": map[string]any{
						"type":        "integer",
						"minimum":     1,
						"maximum":     diagnosticsMaxLimit,
						"description": "Maximum diagnostics to return.",
					},
				},
			},
		},
		PromptFunc: func(tool.PromptContext) (string, error) {
			return "Reads the current session's recorded LSP diagnostics. Use file_path or severity to narrow results. This tool does not start language servers; it only reports diagnostics already captured by the runtime.", nil
		},
		ValidateFunc: func(ctx tool.Context, raw json.RawMessage) error {
			input, err := decodeDiagnosticsInput(raw)
			if err != nil {
				return err
			}
			if !lsp.IsKnownSeverity(input.Severity) {
				return fmt.Errorf("unsupported severity %q", input.Severity)
			}
			if input.Limit != nil && (*input.Limit <= 0 || *input.Limit > diagnosticsMaxLimit) {
				return fmt.Errorf("limit must be between 1 and %d", diagnosticsMaxLimit)
			}
			return nil
		},
		PermissionFunc: func(tool.Context, json.RawMessage) (contracts.PermissionDecision, error) {
			return contracts.PermissionDecision{Behavior: contracts.PermissionAllow, DecisionReason: "LSPDiagnostics reads session diagnostic metadata"}, nil
		},
		CallFunc: func(ctx tool.Context, raw json.RawMessage, sink tool.ProgressSink) (contracts.ToolResult, error) {
			return callDiagnostics(ctx, raw)
		},
		ReadOnlyFunc: func(json.RawMessage) bool {
			return true
		},
		ConcurrencyFunc: func(json.RawMessage) bool {
			return true
		},
	}
}

func decodeDiagnosticsInput(raw json.RawMessage) (diagnosticsInput, error) {
	if len(raw) == 0 {
		return diagnosticsInput{}, nil
	}
	var input diagnosticsInput
	if err := json.Unmarshal(raw, &input); err != nil {
		return diagnosticsInput{}, err
	}
	var aliases struct {
		FilePathCamel   string `json:"filePath"`
		MaxResults      *int   `json:"max_results"`
		MaxResultsCamel *int   `json:"maxResults"`
	}
	if err := json.Unmarshal(raw, &aliases); err != nil {
		return diagnosticsInput{}, err
	}
	if input.FilePath == "" {
		input.FilePath = aliases.FilePathCamel
	}
	if input.Limit == nil {
		if aliases.MaxResults != nil {
			input.Limit = aliases.MaxResults
		} else {
			input.Limit = aliases.MaxResultsCamel
		}
	}
	return input, nil
}

func callDiagnostics(ctx tool.Context, raw json.RawMessage) (contracts.ToolResult, error) {
	input, err := decodeDiagnosticsInput(raw)
	if err != nil {
		return contracts.ToolResult{}, err
	}
	path := diagnosticsPath(ctx)
	if path == "" {
		return contracts.ToolResult{
			Content: "LSP diagnostics are unavailable because this turn has no session path.",
			StructuredContent: map[string]any{
				"available": false,
				"count":     0,
			},
		}, nil
	}
	diagnostics, err := lsp.LoadSnapshot(path)
	if err != nil {
		return contracts.ToolResult{}, err
	}
	limit := diagnosticsDefaultLimit
	if input.Limit != nil {
		limit = *input.Limit
	}
	filtered, truncated := lsp.FilterDiagnostics(diagnostics, lsp.Filter{
		FilePath: input.FilePath,
		Severity: input.Severity,
		Limit:    limit,
	})
	return contracts.ToolResult{
		Content: formatDiagnostics(filtered, len(diagnostics), truncated),
		StructuredContent: map[string]any{
			"available":   true,
			"count":       len(filtered),
			"total_count": len(diagnostics),
			"truncated":   truncated,
			"filter": map[string]any{
				"file_path": strings.TrimSpace(input.FilePath),
				"severity":  lsp.NormalizeSeverity(input.Severity),
				"limit":     limit,
			},
			"diagnostics": filtered,
		},
	}, nil
}

func diagnosticsPath(ctx tool.Context) string {
	if ctx.Metadata == nil {
		return ""
	}
	sessionPath, _ := ctx.Metadata[tool.MetadataSessionPathKey].(string)
	return lsp.SessionDiagnosticsPath(sessionPath, ctx.SessionID)
}

func formatDiagnostics(diagnostics []lsp.Diagnostic, total int, truncated bool) string {
	if total == 0 {
		return "No LSP diagnostics recorded for this session."
	}
	if len(diagnostics) == 0 {
		return fmt.Sprintf("No LSP diagnostics matched the filter. Total recorded diagnostics: %d.", total)
	}
	var lines []string
	header := fmt.Sprintf("LSP diagnostics (%d of %d)", len(diagnostics), total)
	if truncated {
		header += " (truncated)"
	}
	lines = append(lines, header)
	for _, diagnostic := range diagnostics {
		location := fmt.Sprintf("%s:%d:%d", diagnostic.FilePath, diagnostic.Range.Start.Line+1, diagnostic.Range.Start.Character+1)
		severity := diagnostic.Severity
		if severity == "" {
			severity = "diagnostic"
		}
		source := strings.TrimSpace(strings.Join(nonEmptyStrings(diagnostic.Source, diagnostic.Code), " "))
		if source != "" {
			source = " [" + source + "]"
		}
		lines = append(lines, fmt.Sprintf("- %s %s%s: %s", location, severity, source, diagnostic.Message))
	}
	return strings.Join(lines, "\n")
}

func nonEmptyStrings(values ...string) []string {
	out := make([]string, 0, len(values))
	for _, value := range values {
		if trimmed := strings.TrimSpace(value); trimmed != "" {
			out = append(out, trimmed)
		}
	}
	return out
}
