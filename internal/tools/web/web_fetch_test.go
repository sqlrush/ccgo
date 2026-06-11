package webtools

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"ccgo/internal/contracts"
	"ccgo/internal/permissions"
	"ccgo/internal/tool"
)

func webExecutor(t *testing.T) tool.Executor {
	t.Helper()
	registry, err := tool.NewRegistry(NewWebFetchTool())
	if err != nil {
		t.Fatal(err)
	}
	return tool.NewExecutor(registry)
}

func TestWebFetchReturnsTextResponse(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/plain; charset=utf-8")
		_, _ = w.Write([]byte("hello from test server\n"))
	}))
	defer server.Close()
	executor := webExecutor(t)
	result, err := executor.Execute(tool.Context{Context: context.Background(), Metadata: map[string]any{}}, contracts.ToolUse{
		ID:    "toolu_web",
		Name:  "WebFetch",
		Input: json.RawMessage(`{"url":` + strconvQuote(server.URL) + `,"prompt":"summarize"}`),
	}, nil)
	if err != nil {
		t.Fatal(err)
	}
	if result.IsError {
		t.Fatalf("result should not be error: %#v", result)
	}
	content := result.Content.(string)
	if !strings.Contains(content, "hello from test server") || !strings.Contains(content, "Prompt: summarize") {
		t.Fatalf("content = %#v", content)
	}
	if result.StructuredContent["status_code"] != 200 || result.StructuredContent["body"] != "hello from test server\n" {
		t.Fatalf("structured content = %#v", result.StructuredContent)
	}
}

func TestWebFetchTruncatesBody(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/plain")
		_, _ = w.Write([]byte("0123456789"))
	}))
	defer server.Close()
	executor := webExecutor(t)
	result, err := executor.Execute(tool.Context{Context: context.Background(), Metadata: map[string]any{}}, contracts.ToolUse{
		ID:    "toolu_web_truncate",
		Name:  "WebFetch",
		Input: json.RawMessage(`{"url":` + strconvQuote(server.URL) + `,"max_bytes":5}`),
	}, nil)
	if err != nil {
		t.Fatal(err)
	}
	if result.StructuredContent["body"] != "01234" || result.StructuredContent["truncated"] != true {
		t.Fatalf("structured content = %#v", result.StructuredContent)
	}
	if !strings.Contains(result.Content.(string), "truncated to 5 bytes") {
		t.Fatalf("content = %#v", result.Content)
	}
}

func TestWebFetchMarksNonSuccessAndBinaryResponses(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/missing":
			w.WriteHeader(http.StatusNotFound)
			_, _ = w.Write([]byte("missing"))
		case "/binary":
			w.Header().Set("Content-Type", "application/octet-stream")
			_, _ = w.Write([]byte{0, 1, 2, 3})
		}
	}))
	defer server.Close()
	executor := webExecutor(t)
	missing, err := executor.Execute(tool.Context{Context: context.Background(), Metadata: map[string]any{}}, contracts.ToolUse{
		ID:    "toolu_web_missing",
		Name:  "WebFetch",
		Input: json.RawMessage(`{"url":` + strconvQuote(server.URL+"/missing") + `}`),
	}, nil)
	if err != nil {
		t.Fatal(err)
	}
	if !missing.IsError || missing.StructuredContent["status_code"] != 404 {
		t.Fatalf("missing result = %#v", missing)
	}

	binary, err := executor.Execute(tool.Context{Context: context.Background(), Metadata: map[string]any{}}, contracts.ToolUse{
		ID:    "toolu_web_binary",
		Name:  "WebFetch",
		Input: json.RawMessage(`{"url":` + strconvQuote(server.URL+"/binary") + `}`),
	}, nil)
	if err != nil {
		t.Fatal(err)
	}
	if !binary.IsError || binary.StructuredContent["binary"] != true || !strings.Contains(binary.Content.(string), "binary") {
		t.Fatalf("binary result = %#v", binary)
	}
}

func TestWebFetchValidation(t *testing.T) {
	executor := webExecutor(t)
	tests := []struct {
		name  string
		input string
		want  string
	}{
		{name: "missing url", input: `{}`, want: "input.url is required"},
		{name: "ftp", input: `{"url":"ftp://example.com/file"}`, want: "url must use http or https"},
		{name: "missing host", input: `{"url":"https:///path"}`, want: "url must include a hostname"},
		{name: "bad timeout", input: `{"url":"https://example.com","timeout":0}`, want: "timeout must be positive"},
		{name: "bad max bytes", input: `{"url":"https://example.com","max_bytes":0}`, want: "max_bytes must be positive"},
		{name: "unknown field", input: `{"url":"https://example.com","extra":true}`, want: "input.extra is not allowed"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := executor.Execute(tool.Context{Context: context.Background(), Metadata: map[string]any{}}, contracts.ToolUse{
				ID:    "toolu_invalid",
				Name:  "WebFetch",
				Input: json.RawMessage(tt.input),
			}, nil)
			if err == nil || !strings.Contains(err.Error(), tt.want) {
				t.Fatalf("err = %v, want %q", err, tt.want)
			}
		})
	}
}

func TestWebFetchPermissionsUseDomainTarget(t *testing.T) {
	webFetch := NewWebFetchTool()
	ctx := tool.Context{
		Context: context.Background(),
		Permissions: tool.NewEnginePermissionDecider(permissions.NewEngine(
			contracts.PermissionContext{Mode: contracts.PermissionDefault},
			permissions.MustParseRule(contracts.PermissionSourceSession, contracts.PermissionDeny, "WebFetch(domain:example.com)"),
		)),
	}
	decision, err := webFetch.CheckPermissions(ctx, json.RawMessage(`{"url":"https://example.com/path"}`))
	if err != nil {
		t.Fatal(err)
	}
	if decision.Behavior != contracts.PermissionDeny {
		t.Fatalf("decision = %#v", decision)
	}
}

func strconvQuote(value string) string {
	data, err := json.Marshal(value)
	if err != nil {
		panic(err)
	}
	return string(data)
}
