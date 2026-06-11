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
	registry, err := tool.NewRegistry(NewWebFetchTool(), NewWebSearchTool())
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

func TestWebFetchRendersHTMLAndPromptExcerpt(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		_, _ = w.Write([]byte(`<!doctype html>
<html>
<head>
  <title>Plans</title>
  <script>window.secret = "script noise";</script>
  <style>body { color: red; }</style>
</head>
<body>
  <nav>Home Docs Blog</nav>
  <main>
    <h1>Plans</h1>
    <p>Alpha plan includes standard documentation access.</p>
    <p>The beta pricing plan costs $20 and includes priority support.</p>
  </main>
</body>
</html>`))
	}))
	defer server.Close()
	executor := webExecutor(t)
	result, err := executor.Execute(tool.Context{Context: context.Background(), Metadata: map[string]any{}}, contracts.ToolUse{
		ID:    "toolu_web_html",
		Name:  "WebFetch",
		Input: json.RawMessage(`{"url":` + strconvQuote(server.URL) + `,"prompt":"beta pricing"}`),
	}, nil)
	if err != nil {
		t.Fatal(err)
	}
	if result.IsError {
		t.Fatalf("result should not be error: %#v", result)
	}
	content := result.Content.(string)
	if !strings.Contains(content, "Relevant excerpt:") || !strings.Contains(content, "The beta pricing plan costs $20") {
		t.Fatalf("content = %#v", content)
	}
	if strings.Contains(content, "script noise") || strings.Contains(content, "color: red") || strings.Contains(content, "<p>") {
		t.Fatalf("content leaked raw html/script/style: %#v", content)
	}
	rendered, ok := result.StructuredContent["rendered_body"].(string)
	if !ok || !strings.Contains(rendered, "Alpha plan") || !strings.Contains(rendered, "beta pricing plan") {
		t.Fatalf("rendered body = %#v", result.StructuredContent["rendered_body"])
	}
	if strings.Contains(rendered, "script noise") || strings.Contains(rendered, "color: red") {
		t.Fatalf("rendered body leaked removed blocks: %#v", rendered)
	}
	excerpt, ok := result.StructuredContent["prompt_excerpt"].(string)
	if !ok || !strings.Contains(excerpt, "beta pricing plan") || strings.Contains(excerpt, "Alpha plan") {
		t.Fatalf("prompt excerpt = %#v", result.StructuredContent["prompt_excerpt"])
	}
	terms, ok := result.StructuredContent["prompt_terms"].([]string)
	if !ok || len(terms) != 2 || terms[0] != "beta" || terms[1] != "pricing" {
		t.Fatalf("prompt terms = %#v", result.StructuredContent["prompt_terms"])
	}
	if result.StructuredContent["rendered"] != true {
		t.Fatalf("structured content = %#v", result.StructuredContent)
	}
	if body, ok := result.StructuredContent["body"].(string); !ok || !strings.Contains(body, "<html>") {
		t.Fatalf("raw body should be preserved: %#v", result.StructuredContent["body"])
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
