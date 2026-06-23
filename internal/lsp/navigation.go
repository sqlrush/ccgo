package lsp

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
)

// NavigationClient is the interface implemented by a live LSP server connection
// that can service navigation requests. The tool layer depends on this interface
// so tests can supply a mock without a real language server.
type NavigationClient interface {
	// SendRequest sends a JSON-RPC request to the language server and returns
	// the raw JSON result. Returns (nil, nil) when no server is available for
	// the requested file type.
	SendRequest(ctx context.Context, filePath, method string, params any) (json.RawMessage, error)
}

// NavigationLocation represents a source position returned by goToDefinition,
// findReferences, and goToImplementation (LSP Location type).
type NavigationLocation struct {
	URI   string `json:"uri"`
	Range struct {
		Start struct {
			Line      int `json:"line"`
			Character int `json:"character"`
		} `json:"start"`
		End struct {
			Line      int `json:"line"`
			Character int `json:"character"`
		} `json:"end"`
	} `json:"range"`
}

// NavigationSymbol represents a symbol entry returned by documentSymbol and
// workspaceSymbol operations.
type NavigationSymbol struct {
	Name     string `json:"name"`
	Kind     int    `json:"kind"`
	FilePath string `json:"filePath,omitempty"`
	Line     int    `json:"line"`
}

// NavigationHover holds the markdown content from a hover response.
type NavigationHover struct {
	Contents string `json:"contents"`
}

// NavigationResult holds the formatted output for all navigation operations.
type NavigationResult struct {
	// Operation is the LSP operation name (goToDefinition, hover, etc.).
	Operation string `json:"operation"`
	// Formatted is the human-readable result string.
	Formatted string `json:"result"`
	// ResultCount is the number of individual results (locations, symbols, etc.).
	ResultCount int `json:"result_count,omitempty"`
	// FileCount is the number of distinct files in the result set.
	FileCount int `json:"file_count,omitempty"`
}

// NavigationParams builds the LSP method name and JSON-serialisable params for
// a navigation operation. line and character are 1-based (tool input convention)
// and are converted to 0-based LSP positions internally.
func NavigationParams(operation, fileURI string, line, character int) (method string, params any, err error) {
	// Convert 1-based coords to 0-based LSP positions.
	pos := map[string]any{
		"line":      line - 1,
		"character": character - 1,
	}
	textDocPos := map[string]any{
		"textDocument": map[string]any{"uri": fileURI},
		"position":     pos,
	}
	switch operation {
	case "goToDefinition":
		return "textDocument/definition", textDocPos, nil
	case "findReferences":
		return "textDocument/references", map[string]any{
			"textDocument": map[string]any{"uri": fileURI},
			"position":     pos,
			"context":      map[string]any{"includeDeclaration": true},
		}, nil
	case "hover":
		return "textDocument/hover", textDocPos, nil
	case "documentSymbol":
		return "textDocument/documentSymbol", map[string]any{
			"textDocument": map[string]any{"uri": fileURI},
		}, nil
	case "workspaceSymbol":
		return "workspace/symbol", map[string]any{"query": ""}, nil
	case "goToImplementation":
		return "textDocument/implementation", textDocPos, nil
	case "prepareCallHierarchy":
		return "textDocument/prepareCallHierarchy", textDocPos, nil
	case "incomingCalls":
		return "textDocument/prepareCallHierarchy", textDocPos, nil
	case "outgoingCalls":
		return "textDocument/prepareCallHierarchy", textDocPos, nil
	default:
		return "", nil, fmt.Errorf("unknown LSP operation %q", operation)
	}
}

// FormatNavigationResult converts a raw JSON-RPC result into a NavigationResult
// for the given operation. An empty result (nil or JSON null) is formatted as
// "No results found."
func FormatNavigationResult(operation string, raw json.RawMessage) NavigationResult {
	if len(raw) == 0 || string(raw) == "null" {
		return NavigationResult{
			Operation: operation,
			Formatted: "No results found.",
		}
	}
	switch operation {
	case "goToDefinition", "findReferences", "goToImplementation":
		return formatLocations(operation, raw)
	case "hover":
		return formatHover(operation, raw)
	case "documentSymbol":
		return formatDocumentSymbols(operation, raw)
	case "workspaceSymbol":
		return formatWorkspaceSymbols(operation, raw)
	case "prepareCallHierarchy", "incomingCalls", "outgoingCalls":
		return formatCallHierarchy(operation, raw)
	default:
		return NavigationResult{Operation: operation, Formatted: string(raw)}
	}
}

func formatLocations(operation string, raw json.RawMessage) NavigationResult {
	var locations []NavigationLocation
	if err := json.Unmarshal(raw, &locations); err != nil {
		// Maybe a single Location not an array.
		var single NavigationLocation
		if err2 := json.Unmarshal(raw, &single); err2 == nil {
			locations = []NavigationLocation{single}
		} else {
			return NavigationResult{Operation: operation, Formatted: string(raw)}
		}
	}
	if len(locations) == 0 {
		return NavigationResult{Operation: operation, Formatted: "No results found."}
	}
	files := map[string]struct{}{}
	var lines []string
	for _, loc := range locations {
		path := uriToPath(loc.URI)
		files[path] = struct{}{}
		lines = append(lines, fmt.Sprintf("%s:%d:%d",
			path,
			loc.Range.Start.Line+1,
			loc.Range.Start.Character+1,
		))
	}
	return NavigationResult{
		Operation:   operation,
		Formatted:   strings.Join(lines, "\n"),
		ResultCount: len(locations),
		FileCount:   len(files),
	}
}

func formatHover(operation string, raw json.RawMessage) NavigationResult {
	var hover struct {
		Contents any `json:"contents"`
	}
	if err := json.Unmarshal(raw, &hover); err != nil {
		return NavigationResult{Operation: operation, Formatted: string(raw)}
	}
	contents := extractHoverContents(hover.Contents)
	if contents == "" {
		return NavigationResult{Operation: operation, Formatted: "No hover information available."}
	}
	return NavigationResult{Operation: operation, Formatted: contents, ResultCount: 1}
}

func extractHoverContents(v any) string {
	switch val := v.(type) {
	case string:
		return val
	case map[string]any:
		if value, ok := val["value"].(string); ok {
			return value
		}
	case []any:
		var parts []string
		for _, item := range val {
			if s := extractHoverContents(item); s != "" {
				parts = append(parts, s)
			}
		}
		return strings.Join(parts, "\n")
	}
	return ""
}

func formatDocumentSymbols(operation string, raw json.RawMessage) NavigationResult {
	var symbols []struct {
		Name     string `json:"name"`
		Kind     int    `json:"kind"`
		Location *struct {
			Range struct {
				Start struct {
					Line int `json:"line"`
				} `json:"start"`
			} `json:"range"`
		} `json:"location,omitempty"`
		Range *struct {
			Start struct {
				Line int `json:"line"`
			} `json:"start"`
		} `json:"range,omitempty"`
		Children json.RawMessage `json:"children,omitempty"`
	}
	if err := json.Unmarshal(raw, &symbols); err != nil {
		return NavigationResult{Operation: operation, Formatted: string(raw)}
	}
	if len(symbols) == 0 {
		return NavigationResult{Operation: operation, Formatted: "No symbols found."}
	}
	var lines []string
	for _, sym := range symbols {
		line := 0
		if sym.Range != nil {
			line = sym.Range.Start.Line + 1
		} else if sym.Location != nil {
			line = sym.Location.Range.Start.Line + 1
		}
		lines = append(lines, fmt.Sprintf("%s (kind:%d) line:%d", sym.Name, sym.Kind, line))
	}
	return NavigationResult{
		Operation:   operation,
		Formatted:   strings.Join(lines, "\n"),
		ResultCount: len(symbols),
	}
}

func formatWorkspaceSymbols(operation string, raw json.RawMessage) NavigationResult {
	var symbols []struct {
		Name     string `json:"name"`
		Kind     int    `json:"kind"`
		Location struct {
			URI   string `json:"uri"`
			Range struct {
				Start struct {
					Line int `json:"line"`
				} `json:"start"`
			} `json:"range"`
		} `json:"location"`
	}
	if err := json.Unmarshal(raw, &symbols); err != nil {
		return NavigationResult{Operation: operation, Formatted: string(raw)}
	}
	if len(symbols) == 0 {
		return NavigationResult{Operation: operation, Formatted: "No symbols found."}
	}
	files := map[string]struct{}{}
	var lines []string
	for _, sym := range symbols {
		path := uriToPath(sym.Location.URI)
		files[path] = struct{}{}
		lines = append(lines, fmt.Sprintf("%s (kind:%d) %s:%d",
			sym.Name, sym.Kind, path, sym.Location.Range.Start.Line+1))
	}
	return NavigationResult{
		Operation:   operation,
		Formatted:   strings.Join(lines, "\n"),
		ResultCount: len(symbols),
		FileCount:   len(files),
	}
}

func formatCallHierarchy(operation string, raw json.RawMessage) NavigationResult {
	// incomingCalls/outgoingCalls items have a `from`/`to` CallHierarchyItem field.
	var items []struct {
		From *struct {
			Name string `json:"name"`
			URI  string `json:"uri"`
		} `json:"from,omitempty"`
		To *struct {
			Name string `json:"name"`
			URI  string `json:"uri"`
		} `json:"to,omitempty"`
		// prepareCallHierarchy returns CallHierarchyItem directly.
		Name string `json:"name,omitempty"`
		URI  string `json:"uri,omitempty"`
	}
	if err := json.Unmarshal(raw, &items); err != nil {
		return NavigationResult{Operation: operation, Formatted: string(raw)}
	}
	if len(items) == 0 {
		return NavigationResult{Operation: operation, Formatted: "No call hierarchy results."}
	}
	var lines []string
	for _, item := range items {
		switch {
		case item.From != nil:
			lines = append(lines, fmt.Sprintf("from: %s (%s)", item.From.Name, uriToPath(item.From.URI)))
		case item.To != nil:
			lines = append(lines, fmt.Sprintf("to: %s (%s)", item.To.Name, uriToPath(item.To.URI)))
		case item.Name != "":
			lines = append(lines, fmt.Sprintf("%s (%s)", item.Name, uriToPath(item.URI)))
		}
	}
	return NavigationResult{
		Operation:   operation,
		Formatted:   strings.Join(lines, "\n"),
		ResultCount: len(items),
	}
}

// uriToPath strips the file:// scheme from a URI, returning a path.
func uriToPath(uri string) string {
	uri = strings.TrimPrefix(uri, "file://")
	if decoded, err := decodeURIPath(uri); err == nil {
		return decoded
	}
	return uri
}

func decodeURIPath(s string) (string, error) {
	var out strings.Builder
	for i := 0; i < len(s); {
		if s[i] == '%' && i+2 < len(s) {
			hi := hexVal(s[i+1])
			lo := hexVal(s[i+2])
			if hi >= 0 && lo >= 0 {
				out.WriteByte(byte(hi<<4 | lo))
				i += 3
				continue
			}
		}
		out.WriteByte(s[i])
		i++
	}
	return out.String(), nil
}

func hexVal(c byte) int {
	switch {
	case c >= '0' && c <= '9':
		return int(c - '0')
	case c >= 'a' && c <= 'f':
		return int(c-'a') + 10
	case c >= 'A' && c <= 'F':
		return int(c-'A') + 10
	}
	return -1
}
