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
			"max_results":"2.0",
			"timeout":"1000"
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

func TestWebSearchParsesJSONResults(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{
			"results": [
				{"title": "Example JSON", "url": "https://example.com/json", "snippet": "JSON snippet"}
			],
			"webPages": {
				"value": [
					{"name": "Docs JSON", "url": "https://docs.example.com/guide", "description": "Docs description"}
				]
			},
			"organic_results": [
				{"title": "Duplicate JSON", "link": "https://example.com/json", "snippet": "duplicate"},
				{"title": "Blocked JSON", "link": "https://blocked.example.net/nope", "snippet": "blocked"}
			]
		}`))
	}))
	defer server.Close()
	executor := webExecutor(t)
	result, err := executor.Execute(tool.Context{
		Context: context.Background(),
		Metadata: map[string]any{
			MetadataWebSearchEndpointKey: server.URL,
		},
	}, contracts.ToolUse{
		ID:   "toolu_search_json",
		Name: "WebSearch",
		Input: json.RawMessage(`{
			"query":"json search",
			"allowed_domains":["example.com"],
			"max_results":5
		}`),
	}, nil)
	if err != nil {
		t.Fatal(err)
	}
	if result.IsError {
		t.Fatalf("result should not be error: %#v", result)
	}
	results, ok := result.StructuredContent["results"].([]map[string]any)
	if !ok || len(results) != 2 {
		t.Fatalf("structured results = %#v", result.StructuredContent["results"])
	}
	if results[0]["title"] != "Example JSON" || results[0]["snippet"] != "JSON snippet" || results[0]["url"] != "https://example.com/json" {
		t.Fatalf("first result = %#v", results[0])
	}
	if results[1]["title"] != "Docs JSON" || results[1]["snippet"] != "Docs description" || results[1]["url"] != "https://docs.example.com/guide" {
		t.Fatalf("second result = %#v", results[1])
	}
	content := result.Content.(string)
	if !strings.Contains(content, "Example JSON") || !strings.Contains(content, "Docs description") || strings.Contains(content, "Duplicate JSON") || strings.Contains(content, "Blocked JSON") {
		t.Fatalf("content = %#v", content)
	}
}

func TestWebSearchParsesNestedJSONResultWrappers(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{
			"web": {
				"results": [
					{"title": "Web Result", "url": "https://example.com/web", "snippet": "web snippet"}
				]
			},
			"response": {
				"documents": [
					{"headline": "Document Result", "href": "https://docs.example.com/doc", "text": "document text"}
				]
			},
			"hits": [
				{"title": "Duplicate Web", "url": "https://example.com/web", "snippet": "duplicate"},
				{"name": "Hit Result", "link": "https://example.com/hit", "content": "hit content"}
			]
		}`))
	}))
	defer server.Close()
	executor := webExecutor(t)
	result, err := executor.Execute(tool.Context{
		Context: context.Background(),
		Metadata: map[string]any{
			MetadataWebSearchEndpointKey: server.URL,
		},
	}, contracts.ToolUse{
		ID:    "toolu_search_nested_json",
		Name:  "WebSearch",
		Input: json.RawMessage(`{"query":"nested json","allowed_domains":["example.com"],"max_results":5}`),
	}, nil)
	if err != nil {
		t.Fatal(err)
	}
	results, ok := result.StructuredContent["results"].([]map[string]any)
	if !ok || len(results) != 3 {
		t.Fatalf("structured results = %#v", result.StructuredContent["results"])
	}
	if results[0]["title"] != "Web Result" || results[1]["title"] != "Document Result" || results[2]["title"] != "Hit Result" {
		t.Fatalf("results = %#v", results)
	}
	if results[1]["snippet"] != "document text" || results[2]["snippet"] != "hit content" {
		t.Fatalf("snippets = %#v", results)
	}
}

func TestWebSearchParsesAlternateJSONFieldAliases(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{
			"search": {
				"organic": [
					{
						"htmlTitle": "<b>Alias Result</b>",
						"pageUrl": {"raw": "https://example.com/alias"},
						"htmlSnippet": "Alias <b>snippet</b>",
						"deepLinks": [
							{"heading": "Deep Link", "targetUrl": "https://docs.example.com/deep", "summary": "Deep summary"}
						]
					},
					{"name": "Source Result", "source_url": "https://example.com/source", "abstract": "Source abstract"},
					{"headline": "Formatted Result", "formattedUrl": "https://example.com/formatted", "caption": "Formatted caption"},
					{"title": "Display Only", "displayUrl": "example.com/not-a-real-url", "snippet": "ignored"}
				]
			}
		}`))
	}))
	defer server.Close()
	executor := webExecutor(t)
	result, err := executor.Execute(tool.Context{
		Context: context.Background(),
		Metadata: map[string]any{
			MetadataWebSearchEndpointKey: server.URL,
		},
	}, contracts.ToolUse{
		ID:    "toolu_search_alias_json",
		Name:  "WebSearch",
		Input: json.RawMessage(`{"query":"alias json","max_results":5}`),
	}, nil)
	if err != nil {
		t.Fatal(err)
	}
	results, ok := result.StructuredContent["results"].([]map[string]any)
	if !ok || len(results) != 4 {
		t.Fatalf("structured results = %#v", result.StructuredContent["results"])
	}
	if results[0]["title"] != "Alias Result" || results[0]["url"] != "https://example.com/alias" || results[0]["snippet"] != "Alias snippet" {
		t.Fatalf("first result = %#v", results[0])
	}
	if results[1]["title"] != "Deep Link" || results[1]["url"] != "https://docs.example.com/deep" || results[1]["snippet"] != "Deep summary" {
		t.Fatalf("deep link result = %#v", results[1])
	}
	if results[2]["title"] != "Source Result" || results[2]["url"] != "https://example.com/source" || results[2]["snippet"] != "Source abstract" {
		t.Fatalf("source result = %#v", results[2])
	}
	if results[3]["title"] != "Formatted Result" || results[3]["url"] != "https://example.com/formatted" || results[3]["snippet"] != "Formatted caption" {
		t.Fatalf("formatted result = %#v", results[3])
	}
	content := result.Content.(string)
	if strings.Contains(content, "Display Only") || strings.Contains(content, "<b>") {
		t.Fatalf("content = %#v", content)
	}
}

func TestWebSearchParsesSearchBackendWrapperObjects(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{
			"answerBox": {
				"title": "Answer Box",
				"link": "https://example.com/answer",
				"answer": "direct answer"
			},
			"knowledgeGraph": {
				"title": "Knowledge Graph",
				"website": "https://example.com/kg",
				"description": "knowledge description"
			},
			"news": {
				"results": [
					{"title": "News Result", "url": "https://news.example.com/story", "excerpt": "news excerpt"}
				]
			},
			"peopleAlsoAsk": [
				{"question": "Question Result", "sourceLink": "https://example.com/question", "snippet": "question snippet"}
			],
			"related_questions": [
				{"question": "Duplicate Question", "link": "https://example.com/question", "snippet": "duplicate"}
			]
		}`))
	}))
	defer server.Close()
	executor := webExecutor(t)
	result, err := executor.Execute(tool.Context{
		Context: context.Background(),
		Metadata: map[string]any{
			MetadataWebSearchEndpointKey: server.URL,
		},
	}, contracts.ToolUse{
		ID:    "toolu_search_backend_wrappers",
		Name:  "WebSearch",
		Input: json.RawMessage(`{"query":"backend wrappers","allowed_domains":["example.com"],"max_results":10}`),
	}, nil)
	if err != nil {
		t.Fatal(err)
	}
	results, ok := result.StructuredContent["results"].([]map[string]any)
	if !ok || len(results) != 4 {
		t.Fatalf("structured results = %#v", result.StructuredContent["results"])
	}
	wantTitles := []string{"Answer Box", "Knowledge Graph", "News Result", "Question Result"}
	wantSnippets := []string{"direct answer", "knowledge description", "news excerpt", "question snippet"}
	for i := range wantTitles {
		if results[i]["title"] != wantTitles[i] || results[i]["snippet"] != wantSnippets[i] {
			t.Fatalf("result %d = %#v", i, results[i])
		}
	}
	content := result.Content.(string)
	if !strings.Contains(content, "Knowledge Graph") || !strings.Contains(content, "news excerpt") || strings.Contains(content, "Duplicate Question") {
		t.Fatalf("content = %#v", content)
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
		Input: json.RawMessage(`{"query":"xx","blocked_domains":["example.com"]}`),
	}, nil)
	if err != nil {
		t.Fatal(err)
	}
	if result.Content != "No search results found for: xx" {
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
		{name: "short query", input: `{"query":"x"}`, want: "input.query must be at least 2 characters"},
		{name: "bad max results", input: `{"query":"xx","max_results":0}`, want: "max_results must be positive"},
		{name: "too many results", input: `{"query":"xx","maxResults":21}`, want: "max_results must be at most 20"},
		{name: "bad timeout", input: `{"query":"xx","timeout":0}`, want: "timeout must be positive"},
		{name: "bad domain type", input: `{"query":"xx","allowed_domains":[3]}`, want: "input.allowed_domains[0] must be string"},
		{name: "empty domain", input: `{"query":"xx","allowed_domains":[""]}`, want: "allowed_domains[0] must be a domain name"},
		{name: "bad domain", input: `{"query":"xx","allowed_domains":["https://example.com"]}`, want: "allowed_domains[0] must be a domain name"},
		{name: "bad wildcard domain", input: `{"query":"xx","allowed_domains":["*example.com"]}`, want: "allowed_domains[0] must be a domain name"},
		{name: "bad domain label", input: `{"query":"xx","allowed_domains":["bad_domain.com"]}`, want: "allowed_domains[0] must be a domain name"},
		{name: "allowed and blocked domains", input: `{"query":"xx","allowedDomains":["example.com"],"blockedDomains":["blocked.example.net"]}`, want: "Cannot specify both allowed_domains and blocked_domains"},
		{name: "unknown field", input: `{"query":"xx","extra":true}`, want: "input.extra is not allowed"},
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
