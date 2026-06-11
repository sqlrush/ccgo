package webtools

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"mime"
	"net/http"
	"net/url"
	"strings"
	"time"
	"unicode/utf8"

	"ccgo/internal/contracts"
	"ccgo/internal/tool"
)

const (
	defaultWebFetchTimeoutMillis = 30_000
	maxWebFetchTimeoutMillis     = 120_000
	defaultWebFetchMaxBytes      = 200_000
	maxWebFetchMaxBytes          = 1_000_000
)

type webFetchInput struct {
	URL         string `json:"url"`
	Prompt      string `json:"prompt,omitempty"`
	Timeout     *int   `json:"timeout,omitempty"`
	MaxBytes    *int   `json:"max_bytes,omitempty"`
	MaxBytesAlt *int   `json:"maxBytes,omitempty"`
}

func NewWebFetchTool() tool.Tool {
	var self tool.Tool
	self = tool.FuncTool{
		DefinitionValue: contracts.ToolDefinition{
			Name:               "WebFetch",
			Description:        "Fetch content from a URL.",
			SearchHint:         "fetch web page",
			ReadOnly:           true,
			ConcurrencySafe:    true,
			Strict:             true,
			MaxResultSizeChars: 100_000,
			InputSchema: contracts.JSONSchema{
				"type":     "object",
				"required": []any{"url"},
				"properties": map[string]any{
					"url":       map[string]any{"type": "string"},
					"prompt":    map[string]any{"type": "string"},
					"timeout":   map[string]any{"type": "integer"},
					"max_bytes": map[string]any{"type": "integer"},
					"maxBytes":  map[string]any{"type": "integer"},
				},
			},
		},
		PromptFunc: func(tool.PromptContext) (string, error) {
			return "Fetches a web URL and returns text content. Provide url and optionally prompt, timeout in milliseconds, and max_bytes. Prompt-aware summarization and browser rendering are not implemented yet.", nil
		},
		ValidateFunc: validateWebFetch,
		PermissionFunc: func(ctx tool.Context, raw json.RawMessage) (contracts.PermissionDecision, error) {
			return checkWebFetchPermissions(self, ctx, raw)
		},
		CallFunc:        callWebFetch,
		ReadOnlyFunc:    func(json.RawMessage) bool { return true },
		ConcurrencyFunc: func(json.RawMessage) bool { return true },
	}
	return self
}

func validateWebFetch(_ tool.Context, raw json.RawMessage) error {
	input, err := decodeWebFetch(raw)
	if err != nil {
		return err
	}
	parsed, err := parseFetchURL(input.URL)
	if err != nil {
		return err
	}
	if parsed.Scheme != "http" && parsed.Scheme != "https" {
		return fmt.Errorf("url must use http or https")
	}
	if parsed.Hostname() == "" {
		return fmt.Errorf("url must include a hostname")
	}
	if input.Timeout != nil {
		if *input.Timeout <= 0 {
			return fmt.Errorf("timeout must be positive")
		}
		if *input.Timeout > maxWebFetchTimeoutMillis {
			return fmt.Errorf("timeout must be at most %d milliseconds", maxWebFetchTimeoutMillis)
		}
	}
	maxBytes := webFetchMaxBytes(input)
	if maxBytes <= 0 {
		return fmt.Errorf("max_bytes must be positive")
	}
	if maxBytes > maxWebFetchMaxBytes {
		return fmt.Errorf("max_bytes must be at most %d", maxWebFetchMaxBytes)
	}
	return nil
}

func checkWebFetchPermissions(self tool.Tool, ctx tool.Context, raw json.RawMessage) (contracts.PermissionDecision, error) {
	if ctx.Permissions == nil {
		return contracts.PermissionDecision{Behavior: contracts.PermissionAllow, DecisionReason: "no permission engine configured"}, nil
	}
	input, err := decodeWebFetch(raw)
	if err != nil {
		return contracts.PermissionDecision{}, err
	}
	parsed, err := parseFetchURL(input.URL)
	if err != nil {
		return contracts.PermissionDecision{}, err
	}
	domainInput, err := json.Marshal(map[string]any{
		"url":    "domain:" + strings.ToLower(parsed.Hostname()),
		"prompt": input.Prompt,
	})
	if err != nil {
		return contracts.PermissionDecision{}, err
	}
	return ctx.Permissions.DecideTool(self, domainInput, ctx)
}

func callWebFetch(ctx tool.Context, raw json.RawMessage, _ tool.ProgressSink) (contracts.ToolResult, error) {
	input, err := decodeWebFetch(raw)
	if err != nil {
		return contracts.ToolResult{}, err
	}
	parsed, err := parseFetchURL(input.URL)
	if err != nil {
		return contracts.ToolResult{}, err
	}
	timeout := webFetchTimeout(input)
	maxBytes := webFetchMaxBytes(input)
	result, err := fetchURL(ctx.Context, parsed.String(), timeout, maxBytes)
	if err != nil {
		return contracts.ToolResult{}, err
	}
	content := formatWebFetchContent(input, result)
	return contracts.ToolResult{
		Content: content,
		IsError: result.StatusCode < 200 || result.StatusCode >= 300 || result.Binary,
		StructuredContent: map[string]any{
			"type":         "web_fetch",
			"url":          parsed.String(),
			"domain":       strings.ToLower(parsed.Hostname()),
			"prompt":       input.Prompt,
			"status_code":  result.StatusCode,
			"content_type": result.ContentType,
			"body":         result.Body,
			"bytes":        result.Bytes,
			"truncated":    result.Truncated,
			"binary":       result.Binary,
			"duration_ms":  result.DurationMS,
		},
	}, nil
}

type fetchResult struct {
	StatusCode  int
	ContentType string
	Body        string
	Bytes       int
	Truncated   bool
	Binary      bool
	DurationMS  int64
}

func fetchURL(ctx context.Context, rawURL string, timeout time.Duration, maxBytes int) (fetchResult, error) {
	if ctx == nil {
		ctx = context.Background()
	}
	fetchCtx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()
	req, err := http.NewRequestWithContext(fetchCtx, http.MethodGet, rawURL, nil)
	if err != nil {
		return fetchResult{}, err
	}
	req.Header.Set("User-Agent", "ccgo-webfetch/0.1")
	start := time.Now()
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return fetchResult{}, err
	}
	defer resp.Body.Close()
	limited := io.LimitReader(resp.Body, int64(maxBytes)+1)
	data, err := io.ReadAll(limited)
	if err != nil {
		return fetchResult{}, err
	}
	truncated := len(data) > maxBytes
	if truncated {
		data = data[:maxBytes]
	}
	contentType := resp.Header.Get("Content-Type")
	if contentType == "" {
		contentType = http.DetectContentType(data)
	}
	binary := isBinaryWebContent(contentType, data)
	body := ""
	if !binary {
		body = string(data)
	}
	return fetchResult{
		StatusCode:  resp.StatusCode,
		ContentType: contentType,
		Body:        body,
		Bytes:       len(data),
		Truncated:   truncated,
		Binary:      binary,
		DurationMS:  time.Since(start).Milliseconds(),
	}, nil
}

func formatWebFetchContent(input webFetchInput, result fetchResult) string {
	var b strings.Builder
	fmt.Fprintf(&b, "Fetched %s with status %d", input.URL, result.StatusCode)
	if result.ContentType != "" {
		fmt.Fprintf(&b, " (%s)", result.ContentType)
	}
	if result.Truncated {
		fmt.Fprintf(&b, "; truncated to %d bytes", result.Bytes)
	}
	b.WriteString(".")
	if input.Prompt != "" {
		b.WriteString("\nPrompt: ")
		b.WriteString(input.Prompt)
	}
	if result.Binary {
		b.WriteString("\nResponse body is binary and was not included.")
		return b.String()
	}
	if result.Body == "" {
		b.WriteString("\nResponse body is empty.")
		return b.String()
	}
	b.WriteString("\n\n")
	b.WriteString(result.Body)
	return strings.TrimRight(b.String(), "\n")
}

func decodeWebFetch(raw json.RawMessage) (webFetchInput, error) {
	var obj map[string]json.RawMessage
	if err := json.Unmarshal(raw, &obj); err != nil {
		return webFetchInput{}, err
	}
	for key := range obj {
		switch key {
		case "url", "prompt", "timeout", "max_bytes", "maxBytes":
		default:
			return webFetchInput{}, fmt.Errorf("input.%s is not allowed", key)
		}
	}
	var input webFetchInput
	data, err := json.Marshal(obj)
	if err != nil {
		return webFetchInput{}, err
	}
	if err := json.Unmarshal(data, &input); err != nil {
		return webFetchInput{}, err
	}
	return input, nil
}

func parseFetchURL(raw string) (*url.URL, error) {
	if strings.TrimSpace(raw) == "" {
		return nil, fmt.Errorf("url is required")
	}
	parsed, err := url.Parse(strings.TrimSpace(raw))
	if err != nil {
		return nil, fmt.Errorf("invalid url: %w", err)
	}
	return parsed, nil
}

func webFetchTimeout(input webFetchInput) time.Duration {
	if input.Timeout == nil {
		return time.Duration(defaultWebFetchTimeoutMillis) * time.Millisecond
	}
	return time.Duration(*input.Timeout) * time.Millisecond
}

func webFetchMaxBytes(input webFetchInput) int {
	if input.MaxBytes != nil {
		return *input.MaxBytes
	}
	if input.MaxBytesAlt != nil {
		return *input.MaxBytesAlt
	}
	return defaultWebFetchMaxBytes
}

func isBinaryWebContent(contentType string, data []byte) bool {
	mediaType, _, err := mime.ParseMediaType(contentType)
	if err == nil {
		mediaType = strings.ToLower(mediaType)
		if strings.HasPrefix(mediaType, "text/") ||
			strings.Contains(mediaType, "json") ||
			strings.Contains(mediaType, "xml") ||
			strings.Contains(mediaType, "javascript") ||
			strings.Contains(mediaType, "html") {
			return false
		}
	}
	if !utf8.Valid(data) {
		return true
	}
	for _, b := range data {
		if b == 0 {
			return true
		}
	}
	return false
}
