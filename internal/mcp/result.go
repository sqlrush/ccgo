package mcp

import (
	"encoding/json"
	"fmt"
	"path/filepath"
	"sort"
	"strings"

	"ccgo/internal/contracts"
	"ccgo/internal/platform"
)

const DefaultResultLimitChars = 100_000

type ResultType string

const (
	ResultTypeToolResult        ResultType = "toolResult"
	ResultTypeStructuredContent ResultType = "structuredContent"
	ResultTypeContentArray      ResultType = "contentArray"
)

type TransformedResult struct {
	Content           any
	Type              ResultType
	Schema            string
	StructuredContent map[string]any
	IsError           bool
	Meta              map[string]any
}

type ResultOptions struct {
	ToolUseID      contracts.ID
	ServerName     string
	ToolName       string
	MaxChars       int
	ResultStoreDir string
}

func ProcessToolResult(raw any, options ResultOptions) (contracts.ToolResult, error) {
	transformed, err := TransformResult(raw, options.ServerName, options.ToolName)
	if err != nil {
		return contracts.ToolResult{}, err
	}
	result := contracts.ToolResult{
		ToolUseID:         options.ToolUseID,
		Content:           transformed.Content,
		IsError:           transformed.IsError,
		StructuredContent: transformed.StructuredContent,
		Meta: map[string]any{
			"mcp": map[string]any{
				"server": options.ServerName,
				"tool":   options.ToolName,
				"type":   string(transformed.Type),
			},
		},
	}
	if transformed.Schema != "" {
		result.Meta["mcp_schema"] = transformed.Schema
	}
	if len(transformed.Meta) > 0 {
		result.Meta["mcp_result_meta"] = transformed.Meta
	}
	return limitMCPResult(result, options), nil
}

func TransformResult(raw any, serverName string, toolName string) (TransformedResult, error) {
	obj, ok := raw.(map[string]any)
	if !ok {
		return TransformedResult{}, unexpectedResultError(serverName, toolName)
	}
	isError := boolValue(firstNonEmpty(obj["isError"], obj["is_error"]))
	meta := resultMeta(obj)
	if value, ok := obj["toolResult"]; ok {
		return TransformedResult{
			Content: fmt.Sprint(value),
			Type:    ResultTypeToolResult,
			IsError: isError,
			Meta:    meta,
		}, nil
	}
	if value, ok := resultValue(obj, "structuredContent", "structured_content"); ok && value != nil {
		content, err := json.Marshal(value)
		if err != nil {
			return TransformedResult{}, err
		}
		return TransformedResult{
			Content:           string(content),
			Type:              ResultTypeStructuredContent,
			Schema:            InferCompactSchema(value, 2),
			StructuredContent: structuredContentMap(value),
			IsError:           isError,
			Meta:              meta,
		}, nil
	}
	if value, ok := resultContentItems(obj); ok {
		blocks := make([]contracts.ContentBlock, 0, len(value))
		for _, item := range value {
			blocks = append(blocks, transformContentItem(item, serverName)...)
		}
		return TransformedResult{
			Content: blocks,
			Type:    ResultTypeContentArray,
			Schema:  InferCompactSchema(blocks, 2),
			IsError: isError,
			Meta:    meta,
		}, nil
	}
	return TransformedResult{}, unexpectedResultError(serverName, toolName)
}

func InferCompactSchema(value any, depth int) string {
	if value == nil {
		return "null"
	}
	if depth < 0 {
		return "..."
	}
	switch typed := value.(type) {
	case []any:
		if len(typed) == 0 {
			return "[]"
		}
		return "[" + InferCompactSchema(typed[0], depth-1) + "]"
	case []contracts.ContentBlock:
		if len(typed) == 0 {
			return "[]"
		}
		return "[" + InferCompactSchema(contentBlockSchemaMap(typed[0]), depth-1) + "]"
	case map[string]any:
		if depth <= 0 {
			return "{...}"
		}
		keys := sortedAnyKeys(typed)
		if len(keys) > 10 {
			keys = keys[:10]
		}
		parts := make([]string, 0, len(keys))
		for _, key := range keys {
			parts = append(parts, fmt.Sprintf("%s: %s", key, InferCompactSchema(typed[key], depth-1)))
		}
		if len(typed) > len(keys) {
			parts = append(parts, "...")
		}
		return "{" + strings.Join(parts, ", ") + "}"
	case string:
		return "string"
	case bool:
		return "boolean"
	case float64, float32, int, int64, int32, uint, uint64, uint32:
		return "number"
	default:
		return fmt.Sprintf("%T", value)
	}
}

func transformContentItem(item any, serverName string) []contracts.ContentBlock {
	obj, ok := item.(map[string]any)
	if !ok {
		return nil
	}
	switch mcpContentType(obj["type"]) {
	case "text":
		return []contracts.ContentBlock{{Type: contracts.ContentText, Text: stringValue(firstNonEmpty(obj["text"], obj["content"], obj["value"]))}}
	case "image":
		return []contracts.ContentBlock{{Type: contracts.ContentImage, Source: imageContentSource(obj)}}
	case "resource":
		return transformResourceContent(obj, serverName)
	case "resource_link":
		return []contracts.ContentBlock{{Type: contracts.ContentText, Text: resourceLinkText(obj)}}
	default:
		return nil
	}
}

func mcpContentType(value any) string {
	normalized := strings.ToLower(strings.NewReplacer("-", "_", " ", "_").Replace(strings.TrimSpace(stringValue(value))))
	switch normalized {
	case "text", "text_content", "textcontent":
		return "text"
	case "image", "input_image", "inputimage", "image_content", "imagecontent":
		return "image"
	case "resource", "embedded_resource", "embeddedresource":
		return "resource"
	case "resource_link", "resourcelink":
		return "resource_link"
	default:
		return normalized
	}
}

func imageContentSource(obj map[string]any) map[string]any {
	source, _ := firstMapValue(obj, "source", "imageSource", "image_source")
	data := firstNonEmpty(obj["data"], obj["base64"], obj["content"])
	mimeType := firstNonEmpty(obj["mimeType"], obj["mime_type"], obj["mediaType"], obj["media_type"], obj["mime"])
	if source != nil {
		data = firstNonEmpty(source["data"], source["base64"], source["content"], data)
		mimeType = firstNonEmpty(source["mimeType"], source["mime_type"], source["mediaType"], source["media_type"], source["mime"], mimeType)
	}
	return map[string]any{
		"type":       "base64",
		"data":       stringValue(data),
		"media_type": stringValue(mimeType),
	}
}

func transformResourceContent(obj map[string]any, serverName string) []contracts.ContentBlock {
	resource, _ := firstMapValue(obj, "resource", "embeddedResource", "embedded_resource")
	if resource == nil {
		resource = obj
	}
	prefix := fmt.Sprintf("[Resource from %s at %s] ", serverName, stringValue(resource["uri"]))
	if text := stringValue(resource["text"]); text != "" {
		return []contracts.ContentBlock{{Type: contracts.ContentText, Text: prefix + text}}
	}
	if blob := stringValue(resource["blob"]); blob != "" {
		mimeType := stringValue(resource["mimeType"])
		if mimeType == "" {
			mimeType = stringValue(resource["mime_type"])
		}
		return []contracts.ContentBlock{{Type: contracts.ContentText, Text: fmt.Sprintf("%sBinary content (%s, %d base64 characters)", prefix, mimeType, len(blob))}}
	}
	return nil
}

func resourceLinkText(obj map[string]any) string {
	text := fmt.Sprintf("[Resource link: %s] %s", stringValue(obj["name"]), stringValue(obj["uri"]))
	if description := stringValue(obj["description"]); description != "" {
		text += " (" + description + ")"
	}
	return text
}

func limitMCPResult(result contracts.ToolResult, options ResultOptions) contracts.ToolResult {
	limit := options.MaxChars
	if limit <= 0 {
		limit = DefaultResultLimitChars
	}
	content, serialized, ok := serializableContent(result.Content)
	if !ok || len(serialized) <= limit {
		return result
	}
	dir := options.ResultStoreDir
	if dir == "" {
		dir = filepath.Join(platform.ClaudeHomeDir(), "tool-results")
	}
	name := fmt.Sprintf("mcp-%s-%s-%s", normalizeResultName(options.ServerName), normalizeResultName(options.ToolName), string(options.ToolUseID))
	path := filepath.Join(dir, normalizeResultName(name)+".txt")
	_ = platform.AtomicWriteFile(path, []byte(serialized), 0o600)
	preview := serialized[:limit]
	result.Content = fmt.Sprintf("%s\n\n[MCP tool output truncated; full output saved to %s]", strings.TrimRight(preview, "\n"), path)
	if result.Meta == nil {
		result.Meta = map[string]any{}
	}
	result.Meta["truncated"] = true
	result.Meta["full_output_path"] = path
	result.Meta["full_output_bytes"] = len(serialized)
	if content != nil {
		result.Meta["original_content_type"] = fmt.Sprintf("%T", content)
	}
	return result
}

func serializableContent(content any) (any, string, bool) {
	switch typed := content.(type) {
	case string:
		return typed, typed, true
	default:
		data, err := json.MarshalIndent(content, "", "  ")
		if err != nil {
			return content, "", false
		}
		return content, string(data), true
	}
}

func structuredContentMap(value any) map[string]any {
	if obj, ok := value.(map[string]any); ok {
		return obj
	}
	return map[string]any{"value": value}
}

func resultValue(values map[string]any, keys ...string) (any, bool) {
	for _, key := range keys {
		if value, ok := values[key]; ok {
			return value, true
		}
	}
	return nil, false
}

func resultContentItems(values map[string]any) ([]any, bool) {
	value, ok := resultValue(values, "content", "contents")
	if !ok || value == nil {
		return nil, false
	}
	switch typed := value.(type) {
	case []any:
		return typed, true
	case map[string]any:
		return []any{typed}, true
	case string:
		return []any{map[string]any{"type": "text", "text": typed}}, true
	default:
		return []any{map[string]any{"type": "text", "text": fmt.Sprint(typed)}}, true
	}
}

func resultMeta(values map[string]any) map[string]any {
	for _, key := range []string{"_meta", "meta"} {
		if meta, ok := values[key].(map[string]any); ok && len(meta) > 0 {
			return meta
		}
	}
	return nil
}

func firstMapValue(values map[string]any, keys ...string) (map[string]any, bool) {
	for _, key := range keys {
		if value, ok := values[key].(map[string]any); ok {
			return value, true
		}
	}
	return nil, false
}

func contentBlockSchemaMap(block contracts.ContentBlock) map[string]any {
	out := map[string]any{"type": string(block.Type)}
	if block.Text != "" {
		out["text"] = block.Text
	}
	if block.Source != nil {
		out["source"] = block.Source
	}
	return out
}

func sortedAnyKeys(values map[string]any) []string {
	keys := make([]string, 0, len(values))
	for key := range values {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	return keys
}

func stringValue(value any) string {
	if value == nil {
		return ""
	}
	if text, ok := value.(string); ok {
		return text
	}
	return fmt.Sprint(value)
}

func boolValue(value any) bool {
	switch typed := value.(type) {
	case bool:
		return typed
	case string:
		switch strings.ToLower(strings.TrimSpace(typed)) {
		case "true", "1", "yes":
			return true
		}
	}
	return false
}

func firstNonEmpty(values ...any) any {
	for _, value := range values {
		if stringValue(value) != "" {
			return value
		}
	}
	return nil
}

func normalizeResultName(value string) string {
	value = filepath.Base(value)
	value = strings.Map(func(r rune) rune {
		if r >= 'a' && r <= 'z' || r >= 'A' && r <= 'Z' || r >= '0' && r <= '9' || r == '-' || r == '_' || r == '.' {
			return r
		}
		return '_'
	}, value)
	if value == "" || value == "." {
		return "result"
	}
	return value
}

func unexpectedResultError(serverName string, toolName string) error {
	return fmt.Errorf("MCP server %q tool %q: unexpected response format", serverName, toolName)
}
