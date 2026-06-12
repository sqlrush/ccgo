package mcp

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"ccgo/internal/contracts"
)

func TestTransformResultSupportsToolResult(t *testing.T) {
	result, err := TransformResult(map[string]any{"toolResult": 42}, "github", "search")
	if err != nil {
		t.Fatal(err)
	}
	if result.Type != ResultTypeToolResult || result.Content != "42" {
		t.Fatalf("result = %#v", result)
	}
}

func TestProcessToolResultPreservesMCPErrorFlag(t *testing.T) {
	result, err := ProcessToolResult(map[string]any{
		"content": []any{
			map[string]any{"type": "text", "text": "failed"},
		},
		"isError": true,
	}, ResultOptions{
		ToolUseID:  "toolu_error",
		ServerName: "github",
		ToolName:   "search",
	})
	if err != nil {
		t.Fatal(err)
	}
	if !result.IsError {
		t.Fatalf("expected error result: %#v", result)
	}
}

func TestProcessToolResultPreservesMCPErrorAlias(t *testing.T) {
	result, err := ProcessToolResult(map[string]any{
		"structuredContent": map[string]any{"message": "failed"},
		"is_error":          "true",
	}, ResultOptions{
		ToolUseID:  "toolu_error_alias",
		ServerName: "github",
		ToolName:   "search",
	})
	if err != nil {
		t.Fatal(err)
	}
	if !result.IsError {
		t.Fatalf("expected error alias result: %#v", result)
	}
}

func TestProcessToolResultPreservesMCPResultMeta(t *testing.T) {
	result, err := ProcessToolResult(map[string]any{
		"content": []any{
			map[string]any{"type": "text", "text": "ok"},
		},
		"_meta": map[string]any{
			"trace_id": "trace-123",
		},
	}, ResultOptions{
		ToolUseID:  "toolu_meta",
		ServerName: "github",
		ToolName:   "search",
	})
	if err != nil {
		t.Fatal(err)
	}
	meta, ok := result.Meta["mcp_result_meta"].(map[string]any)
	if !ok || meta["trace_id"] != "trace-123" {
		t.Fatalf("mcp result meta = %#v", result.Meta)
	}
}

func TestProcessToolResultPreservesMCPResultMetaAlias(t *testing.T) {
	result, err := ProcessToolResult(map[string]any{
		"toolResult": "ok",
		"meta": map[string]any{
			"cursor": "next",
		},
	}, ResultOptions{
		ToolUseID:  "toolu_meta_alias",
		ServerName: "github",
		ToolName:   "search",
	})
	if err != nil {
		t.Fatal(err)
	}
	meta, ok := result.Meta["mcp_result_meta"].(map[string]any)
	if !ok || meta["cursor"] != "next" {
		t.Fatalf("mcp result meta alias = %#v", result.Meta)
	}
}

func TestProcessToolResultSupportsStructuredContent(t *testing.T) {
	raw := map[string]any{
		"structuredContent": map[string]any{
			"title": "Issue",
			"items": []any{
				map[string]any{"id": float64(1), "name": "one"},
			},
		},
	}

	result, err := ProcessToolResult(raw, ResultOptions{
		ToolUseID:  "toolu_mcp",
		ServerName: "github",
		ToolName:   "issues",
	})
	if err != nil {
		t.Fatal(err)
	}
	if result.ToolUseID != "toolu_mcp" {
		t.Fatalf("tool use id = %q", result.ToolUseID)
	}
	if result.StructuredContent["title"] != "Issue" {
		t.Fatalf("structured content = %#v", result.StructuredContent)
	}
	if !strings.Contains(result.Content.(string), `"title":"Issue"`) {
		t.Fatalf("content = %#v", result.Content)
	}
	if result.Meta["mcp_schema"] == "" {
		t.Fatalf("missing schema meta: %#v", result.Meta)
	}
}

func TestProcessToolResultSupportsStructuredContentAlias(t *testing.T) {
	result, err := ProcessToolResult(map[string]any{
		"structured_content": map[string]any{
			"title": "Issue",
		},
	}, ResultOptions{
		ToolUseID:  "toolu_mcp_alias",
		ServerName: "github",
		ToolName:   "issues",
	})
	if err != nil {
		t.Fatal(err)
	}
	if result.StructuredContent["title"] != "Issue" {
		t.Fatalf("structured content alias = %#v", result.StructuredContent)
	}
}

func TestTransformResultContentArray(t *testing.T) {
	rawJSON := []byte(`{
		"content": [
			{"type": "text", "text": "hello"},
			{"type": "resource", "resource": {"uri": "file:///tmp/a.txt", "text": "body"}},
			{"type": "resource_link", "name": "doc", "uri": "https://example.com/doc", "description": "readme"},
			{"type": "image", "data": "abcd", "mimeType": "image/png"}
		]
	}`)
	var raw map[string]any
	if err := json.Unmarshal(rawJSON, &raw); err != nil {
		t.Fatal(err)
	}

	result, err := TransformResult(raw, "files", "read")
	if err != nil {
		t.Fatal(err)
	}
	blocks, ok := result.Content.([]contracts.ContentBlock)
	if !ok {
		t.Fatalf("content = %#v", result.Content)
	}
	if len(blocks) != 4 {
		t.Fatalf("blocks = %#v", blocks)
	}
	if blocks[0].Text != "hello" {
		t.Fatalf("text block = %#v", blocks[0])
	}
	if !strings.Contains(blocks[1].Text, "[Resource from files at file:///tmp/a.txt] body") {
		t.Fatalf("resource block = %#v", blocks[1])
	}
	if blocks[2].Text != "[Resource link: doc] https://example.com/doc (readme)" {
		t.Fatalf("resource link = %#v", blocks[2])
	}
	if blocks[3].Type != contracts.ContentImage {
		t.Fatalf("image = %#v", blocks[3])
	}
}

func TestTransformResultContentAliases(t *testing.T) {
	single, err := TransformResult(map[string]any{
		"content": map[string]any{"type": "text", "text": "single"},
	}, "github", "search")
	if err != nil {
		t.Fatal(err)
	}
	blocks := single.Content.([]contracts.ContentBlock)
	if len(blocks) != 1 || blocks[0].Text != "single" {
		t.Fatalf("single content = %#v", blocks)
	}

	text, err := TransformResult(map[string]any{
		"contents": "plain text",
	}, "github", "search")
	if err != nil {
		t.Fatal(err)
	}
	blocks = text.Content.([]contracts.ContentBlock)
	if len(blocks) != 1 || blocks[0].Text != "plain text" {
		t.Fatalf("string content = %#v", blocks)
	}
}

func TestTransformResultContentItemAliases(t *testing.T) {
	rawJSON := []byte(`{
		"content": [
			{"type": "textContent", "content": "alias text"},
			{"type": "input_image", "source": {"base64": "BBBB", "mimeType": "image/jpeg"}},
			{"type": "embedded_resource", "uri": "file:///alias.txt", "text": "alias body"},
			{"type": "resourceLink", "name": "guide", "uri": "https://example.com/guide"}
		]
	}`)
	var raw map[string]any
	if err := json.Unmarshal(rawJSON, &raw); err != nil {
		t.Fatal(err)
	}
	result, err := TransformResult(raw, "files", "read")
	if err != nil {
		t.Fatal(err)
	}
	blocks := result.Content.([]contracts.ContentBlock)
	if len(blocks) != 4 {
		t.Fatalf("blocks = %#v", blocks)
	}
	if blocks[0].Text != "alias text" {
		t.Fatalf("text alias = %#v", blocks[0])
	}
	source := blocks[1].Source.(map[string]any)
	if blocks[1].Type != contracts.ContentImage || source["data"] != "BBBB" || source["media_type"] != "image/jpeg" {
		t.Fatalf("image alias = %#v", blocks[1])
	}
	if !strings.Contains(blocks[2].Text, "[Resource from files at file:///alias.txt] alias body") {
		t.Fatalf("resource alias = %#v", blocks[2])
	}
	if blocks[3].Text != "[Resource link: guide] https://example.com/guide" {
		t.Fatalf("resource link alias = %#v", blocks[3])
	}
}

func TestProcessToolResultPersistsLargeOutput(t *testing.T) {
	dir := t.TempDir()
	result, err := ProcessToolResult(map[string]any{"toolResult": "0123456789"}, ResultOptions{
		ToolUseID:      "toolu_big",
		ServerName:     "github",
		ToolName:       "search",
		MaxChars:       5,
		ResultStoreDir: dir,
	})
	if err != nil {
		t.Fatal(err)
	}
	if result.Meta["truncated"] != true {
		t.Fatalf("meta = %#v", result.Meta)
	}
	content := result.Content.(string)
	if !strings.Contains(content, "MCP tool output truncated") {
		t.Fatalf("content = %#v", content)
	}
	path, ok := result.Meta["full_output_path"].(string)
	if !ok {
		t.Fatalf("path meta = %#v", result.Meta)
	}
	if filepath.Dir(path) != dir {
		t.Fatalf("path = %q", path)
	}
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if string(data) != "0123456789" {
		t.Fatalf("persisted = %q", string(data))
	}
}

func TestTransformResultRejectsUnexpectedShape(t *testing.T) {
	_, err := TransformResult(map[string]any{"ok": true}, "github", "search")
	if err == nil {
		t.Fatal("expected error")
	}
}
