package webtools

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"

	"ccgo/internal/contracts"
	"ccgo/internal/permissions"
	"ccgo/internal/tool"
)

func TestWebSearchReturnsParsedResults(t *testing.T) {
	var gotQuery string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotQuery = r.URL.Query().Get("q")
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		_, _ = w.Write([]byte(`
			<html><body>
				<a class="result__a" href="/redirect">navigation</a>
				<a class="result__a" href="https://example.com/one">Example &amp; One</a>
				<a class="result__snippet" href="https://example.com/one">First result &amp; details</a>
				<a class="result__a" href="//duckduckgo.com/l/?uddg=https%3A%2F%2Fdocs.example.com%2Ftwo">Docs Two</a>
				<div class="result__snippet">Docs snippet with <b>bold</b> text</div>
				<a href="https://blocked.example.net/three">Blocked</a>
			</body></html>`))
	}))
	defer server.Close()
	executor := webExecutor(t)
	result, err := executor.Execute(tool.Context{
		Context: context.Background(),
		Metadata: map[string]any{
			MetadataWebSearchEndpointKey: server.URL + "/search",
		},
	}, contracts.ToolUse{
		ID:   "toolu_search",
		Name: "WebSearch",
		Input: json.RawMessage(`{
			"query":"claude go",
			"allowed_domains":["example.com"],
			"max_results":2
		}`),
	}, nil)
	if err != nil {
		t.Fatal(err)
	}
	if gotQuery != "claude go" {
		t.Fatalf("query = %q", gotQuery)
	}
	if result.IsError {
		t.Fatalf("result should not be error: %#v", result)
	}
	content := result.Content.(string)
	if !strings.Contains(content, "Example & One") || !strings.Contains(content, "https://docs.example.com/two") || !strings.Contains(content, "First result & details") || !strings.Contains(content, "Docs snippet with bold text") || strings.Contains(content, "Blocked") {
		t.Fatalf("content = %#v", content)
	}
	results, ok := result.StructuredContent["results"].([]map[string]any)
	if !ok || len(results) != 2 || results[0]["url"] != "https://example.com/one" {
		t.Fatalf("structured results = %#v", result.StructuredContent["results"])
	}
	if results[0]["snippet"] != "First result & details" || results[1]["snippet"] != "Docs snippet with bold text" {
		t.Fatalf("structured snippets = %#v", result.StructuredContent["results"])
	}
}

func TestWebSearchBlockedDomainsAndNoResults(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/html")
		_, _ = w.Write([]byte(`<a href="https://example.com/one">One</a>`))
	}))
	defer server.Close()
	executor := webExecutor(t)
	result, err := executor.Execute(tool.Context{
		Context: context.Background(),
		Metadata: map[string]any{
			MetadataWebSearchEndpointKey: server.URL,
		},
	}, contracts.ToolUse{
		ID:    "toolu_search_empty",
		Name:  "WebSearch",
		Input: json.RawMessage(`{"query":"x","blocked_domains":["example.com"]}`),
	}, nil)
	if err != nil {
		t.Fatal(err)
	}
	if result.Content != "No search results found for: x" {
		t.Fatalf("content = %#v", result.Content)
	}
	results := result.StructuredContent["results"].([]map[string]any)
	if len(results) != 0 {
		t.Fatalf("results = %#v", results)
	}
}

func TestWebSearchUnwrapsDuckDuckGoSubdomainRedirects(t *testing.T) {
	base, err := url.Parse("https://html.duckduckgo.com/html/")
	if err != nil {
		t.Fatal(err)
	}
	got := resolveSearchURL("/l/?uddg=https%3A%2F%2Fdocs.example.com%2Fguide%3Fq%3Dgo", base)
	if got != "https://docs.example.com/guide?q=go" {
		t.Fatalf("resolved URL = %q", got)
	}
	if !isSearchChromeURL("https://html.duckduckgo.com/html/?q=claude") {
		t.Fatalf("DuckDuckGo search chrome should be filtered")
	}
}

func TestWebSearchValidation(t *testing.T) {
	executor := webExecutor(t)
	tests := []struct {
		name  string
		input string
		want  string
	}{
		{name: "missing query", input: `{}`, want: "input.query is required"},
		{name: "empty query", input: `{"query":"  "}`, want: "query is required"},
		{name: "bad max results", input: `{"query":"x","max_results":0}`, want: "max_results must be positive"},
		{name: "too many results", input: `{"query":"x","maxResults":21}`, want: "max_results must be at most 20"},
		{name: "bad timeout", input: `{"query":"x","timeout":0}`, want: "timeout must be positive"},
		{name: "bad domain", input: `{"query":"x","allowed_domains":["https://example.com"]}`, want: "allowed_domains[0] must be a domain name"},
		{name: "unknown field", input: `{"query":"x","extra":true}`, want: "input.extra is not allowed"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := executor.Execute(tool.Context{Context: context.Background(), Metadata: map[string]any{}}, contracts.ToolUse{
				ID:    "toolu_invalid",
				Name:  "WebSearch",
				Input: json.RawMessage(tt.input),
			}, nil)
			if err == nil || !strings.Contains(err.Error(), tt.want) {
				t.Fatalf("err = %v, want %q", err, tt.want)
			}
		})
	}
}

func TestWebSearchPermissionsUseQueryTarget(t *testing.T) {
	webSearch := NewWebSearchTool()
	ctx := tool.Context{
		Context: context.Background(),
		Permissions: tool.NewEnginePermissionDecider(permissions.NewEngine(
			contracts.PermissionContext{Mode: contracts.PermissionDefault},
			permissions.MustParseRule(contracts.PermissionSourceSession, contracts.PermissionDeny, "WebSearch(claude code)"),
		)),
	}
	decision, err := webSearch.CheckPermissions(ctx, json.RawMessage(`{"query":"latest claude code release"}`))
	if err != nil {
		t.Fatal(err)
	}
	if decision.Behavior != contracts.PermissionDeny {
		t.Fatalf("decision = %#v", decision)
	}
}
