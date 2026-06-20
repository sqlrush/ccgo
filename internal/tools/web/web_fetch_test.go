package webtools

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
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
		Input: json.RawMessage(`{"url":` + strconvQuote(server.URL) + `,"prompt":"summarize","max_bytes":"5.0","timeout":"1000"}`),
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

func TestWebFetchDecodesTextCharsets(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/utf16le":
			w.Header().Set("Content-Type", "text/plain; charset=utf-16le")
			_, _ = w.Write([]byte{'H', 0, 'e', 0, 'l', 0, 'l', 0, 'o', 0, '!', 0})
		case "/windows1252":
			w.Header().Set("Content-Type", "text/plain; charset=windows-1252")
			_, _ = w.Write([]byte{'P', 'r', 'i', 'c', 'e', ' ', 0x80, '9', ' ', 0x93, 'q', 'u', 'o', 't', 'e', 'd', 0x94})
		case "/meta-windows1252":
			w.Header().Set("Content-Type", "text/html")
			_, _ = w.Write([]byte("<!doctype html><html><head><meta charset=\"windows-1252\"></head><body><p>Price \x809 \x93quoted\x94</p></body></html>"))
		case "/meta-http-equiv-latin1":
			w.Header().Set("Content-Type", "text/html")
			_, _ = w.Write([]byte("<!doctype html><html><head><meta http-equiv=\"Content-Type\" content=\"text/html; charset=iso-8859-1\"></head><body><p>Caf\xe9</p></body></html>"))
		default:
			http.NotFound(w, r)
		}
	}))
	defer server.Close()
	executor := webExecutor(t)
	utf16Result, err := executor.Execute(tool.Context{Context: context.Background(), Metadata: map[string]any{}}, contracts.ToolUse{
		ID:    "toolu_web_utf16",
		Name:  "WebFetch",
		Input: json.RawMessage(`{"url":` + strconvQuote(server.URL+"/utf16le") + `,"prompt":"extract text"}`),
	}, nil)
	if err != nil {
		t.Fatal(err)
	}
	if utf16Result.StructuredContent["body"] != "Hello!" || utf16Result.StructuredContent["charset"] != "utf-16le" {
		t.Fatalf("utf16 structured content = %#v", utf16Result.StructuredContent)
	}
	cp1252Result, err := executor.Execute(tool.Context{Context: context.Background(), Metadata: map[string]any{}}, contracts.ToolUse{
		ID:    "toolu_web_cp1252",
		Name:  "WebFetch",
		Input: json.RawMessage(`{"url":` + strconvQuote(server.URL+"/windows1252") + `,"prompt":"extract price"}`),
	}, nil)
	if err != nil {
		t.Fatal(err)
	}
	want := "Price \u20ac9 \u201cquoted\u201d"
	if cp1252Result.StructuredContent["body"] != want || cp1252Result.StructuredContent["charset"] != "windows-1252" {
		t.Fatalf("windows-1252 structured content = %#v", cp1252Result.StructuredContent)
	}
	metaCP1252Result, err := executor.Execute(tool.Context{Context: context.Background(), Metadata: map[string]any{}}, contracts.ToolUse{
		ID:    "toolu_web_meta_cp1252",
		Name:  "WebFetch",
		Input: json.RawMessage(`{"url":` + strconvQuote(server.URL+"/meta-windows1252") + `,"prompt":"extract price"}`),
	}, nil)
	if err != nil {
		t.Fatal(err)
	}
	if metaCP1252Result.StructuredContent["charset"] != "windows-1252" || !strings.Contains(metaCP1252Result.StructuredContent["rendered_body"].(string), want) {
		t.Fatalf("meta windows-1252 structured content = %#v", metaCP1252Result.StructuredContent)
	}
	metaLatin1Result, err := executor.Execute(tool.Context{Context: context.Background(), Metadata: map[string]any{}}, contracts.ToolUse{
		ID:    "toolu_web_meta_latin1",
		Name:  "WebFetch",
		Input: json.RawMessage(`{"url":` + strconvQuote(server.URL+"/meta-http-equiv-latin1") + `,"prompt":"extract text"}`),
	}, nil)
	if err != nil {
		t.Fatal(err)
	}
	if metaLatin1Result.StructuredContent["charset"] != "iso-8859-1" || !strings.Contains(metaLatin1Result.StructuredContent["rendered_body"].(string), "Caf\u00e9") {
		t.Fatalf("meta latin1 structured content = %#v", metaLatin1Result.StructuredContent)
	}
}

func TestWebFetchRendersHTMLAndPromptExcerpt(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		_, _ = w.Write([]byte(`<!doctype html>
<html>
<head>
  <title>Invisible beta pricing title leak</title>
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
	if strings.Contains(rendered, "script noise") || strings.Contains(rendered, "color: red") || strings.Contains(rendered, "Invisible beta pricing title leak") {
		t.Fatalf("rendered body leaked removed blocks: %#v", rendered)
	}
	excerpt, ok := result.StructuredContent["prompt_excerpt"].(string)
	if !ok || !strings.Contains(excerpt, "beta pricing plan") || strings.Contains(excerpt, "Alpha plan") || strings.Contains(excerpt, "Invisible beta pricing title leak") {
		t.Fatalf("prompt excerpt = %#v", result.StructuredContent["prompt_excerpt"])
	}
	terms, ok := result.StructuredContent["prompt_terms"].([]string)
	if !ok || len(terms) != 2 || terms[0] != "beta" || terms[1] != "pricing" {
		t.Fatalf("prompt terms = %#v", result.StructuredContent["prompt_terms"])
	}
	phrases, ok := result.StructuredContent["prompt_phrases"].([]string)
	if !ok || len(phrases) != 1 || phrases[0] != "beta pricing" {
		t.Fatalf("prompt phrases = %#v", result.StructuredContent["prompt_phrases"])
	}
	if result.StructuredContent["rendered"] != true {
		t.Fatalf("structured content = %#v", result.StructuredContent)
	}
	if body, ok := result.StructuredContent["body"].(string); !ok || !strings.Contains(body, "<html>") {
		t.Fatalf("raw body should be preserved: %#v", result.StructuredContent["body"])
	}
}

func TestWebFetchPromptExcerptMatchesPluralVariants(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		_, _ = w.Write([]byte(`<!doctype html>
<html>
<body>
  <main>
    <p>Overview page with general launch notes.</p>
    <p>The beta plan costs $20 and includes priority support.</p>
  </main>
</body>
</html>`))
	}))
	defer server.Close()
	executor := webExecutor(t)
	result, err := executor.Execute(tool.Context{Context: context.Background(), Metadata: map[string]any{}}, contracts.ToolUse{
		ID:    "toolu_web_html_plural_excerpt",
		Name:  "WebFetch",
		Input: json.RawMessage(`{"url":` + strconvQuote(server.URL) + `,"prompt":"cost"}`),
	}, nil)
	if err != nil {
		t.Fatal(err)
	}
	content := result.Content.(string)
	if !strings.Contains(content, "Relevant excerpt:") || !strings.Contains(content, "The beta plan costs $20") {
		t.Fatalf("content = %#v", content)
	}
	excerpt, ok := result.StructuredContent["prompt_excerpt"].(string)
	if !ok || !strings.Contains(excerpt, "The beta plan costs $20") || strings.Contains(excerpt, "Overview page") {
		t.Fatalf("prompt excerpt = %#v", result.StructuredContent["prompt_excerpt"])
	}
}

func TestWebFetchHTMLRenderingSkipsHiddenElements(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		_, _ = w.Write([]byte(`<!doctype html>
<html>
<body>
  <main>
    <div hidden>
      <p>Hidden pricing says the enterprise plan is free.</p>
      <span>Nested hidden text should not leak.</span>
    </div>
    <p aria-hidden="true">Invisible beta pricing should not be excerpted.</p>
    <section style="display: none !important"><p>Display none pricing leak.</p></section>
    <section style="visibility: hidden"><p>Visibility hidden pricing leak.</p></section>
    <img hidden alt="Hidden architecture diagram" src="/hidden.png">
    <p>The visible pricing plan costs $30 and includes audit logs.</p>
  </main>
</body>
</html>`))
	}))
	defer server.Close()
	executor := webExecutor(t)
	result, err := executor.Execute(tool.Context{Context: context.Background(), Metadata: map[string]any{}}, contracts.ToolUse{
		ID:    "toolu_web_html_hidden",
		Name:  "WebFetch",
		Input: json.RawMessage(`{"url":` + strconvQuote(server.URL) + `,"prompt":"pricing"}`),
	}, nil)
	if err != nil {
		t.Fatal(err)
	}
	rendered, ok := result.StructuredContent["rendered_body"].(string)
	if !ok || !strings.Contains(rendered, "The visible pricing plan costs $30") {
		t.Fatalf("rendered body = %#v", result.StructuredContent["rendered_body"])
	}
	for _, leaked := range []string{
		"enterprise plan is free",
		"Nested hidden text",
		"Invisible beta pricing",
		"Display none pricing",
		"Visibility hidden pricing",
		"Hidden architecture diagram",
	} {
		if strings.Contains(rendered, leaked) {
			t.Fatalf("rendered body leaked hidden text %q: %#v", leaked, rendered)
		}
	}
	excerpt, ok := result.StructuredContent["prompt_excerpt"].(string)
	if !ok || !strings.Contains(excerpt, "The visible pricing plan costs $30") {
		t.Fatalf("prompt excerpt = %#v", result.StructuredContent["prompt_excerpt"])
	}
	if strings.Contains(excerpt, "enterprise plan is free") || strings.Contains(excerpt, "Invisible beta pricing") {
		t.Fatalf("prompt excerpt used hidden text: %#v", excerpt)
	}
}

func TestWebFetchHTMLRenderingPreservesAccessibleLinkLabels(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		_, _ = w.Write([]byte(`<!doctype html>
<html>
<body>
  <main>
    <a href="/reports/q4" aria-label="Download quarterly report"><svg><title>ignored download icon</title></svg></a>
    <a href="/help" title="Help center"></a>
    <a href="javascript:alert(1)" aria-label="Unsafe menu"></a>
  </main>
</body>
</html>`))
	}))
	defer server.Close()
	executor := webExecutor(t)
	result, err := executor.Execute(tool.Context{Context: context.Background(), Metadata: map[string]any{}}, contracts.ToolUse{
		ID:    "toolu_web_html_accessible_links",
		Name:  "WebFetch",
		Input: json.RawMessage(`{"url":` + strconvQuote(server.URL) + `,"prompt":"quarterly report"}`),
	}, nil)
	if err != nil {
		t.Fatal(err)
	}
	rendered, ok := result.StructuredContent["rendered_body"].(string)
	if !ok {
		t.Fatalf("rendered body = %#v", result.StructuredContent["rendered_body"])
	}
	if !strings.Contains(rendered, "Link: Download quarterly report ("+server.URL+"/reports/q4)") ||
		!strings.Contains(rendered, "Link: Help center ("+server.URL+"/help)") {
		t.Fatalf("rendered body missing accessible link labels: %#v", rendered)
	}
	if strings.Contains(rendered, "ignored download icon") || strings.Contains(rendered, "Unsafe menu") || strings.Contains(rendered, "javascript:alert") {
		t.Fatalf("rendered body leaked hidden/unsafe accessible link text: %#v", rendered)
	}
	excerpt, ok := result.StructuredContent["prompt_excerpt"].(string)
	if !ok || !strings.Contains(excerpt, "Download quarterly report") {
		t.Fatalf("prompt excerpt missing accessible link label: %#v", result.StructuredContent["prompt_excerpt"])
	}
}

func TestWebFetchHTMLRenderingHonorsDetailsAndDialogVisibility(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		_, _ = w.Write([]byte(`<!doctype html>
<html>
<body>
  <main>
    <details>
      <summary>Billing details</summary>
      <p>Hidden discount text should not render.</p>
    </details>
    <details open>
      <summary>Open release notes</summary>
      <p>Visible open details include migration timing.</p>
    </details>
    <dialog><p>Hidden modal warning should not render.</p></dialog>
    <dialog open><p>Visible modal confirmation appears.</p></dialog>
  </main>
</body>
</html>`))
	}))
	defer server.Close()
	executor := webExecutor(t)
	result, err := executor.Execute(tool.Context{Context: context.Background(), Metadata: map[string]any{}}, contracts.ToolUse{
		ID:    "toolu_web_html_details_dialog",
		Name:  "WebFetch",
		Input: json.RawMessage(`{"url":` + strconvQuote(server.URL) + `,"prompt":"billing details"}`),
	}, nil)
	if err != nil {
		t.Fatal(err)
	}
	rendered, ok := result.StructuredContent["rendered_body"].(string)
	if !ok {
		t.Fatalf("rendered body = %#v", result.StructuredContent["rendered_body"])
	}
	for _, want := range []string{
		"Billing details",
		"Open release notes",
		"Visible open details include migration timing.",
		"Visible modal confirmation appears.",
	} {
		if !strings.Contains(rendered, want) {
			t.Fatalf("rendered body missing %q: %#v", want, rendered)
		}
	}
	for _, leaked := range []string{
		"Hidden discount text",
		"Hidden modal warning",
	} {
		if strings.Contains(rendered, leaked) {
			t.Fatalf("rendered body leaked %q: %#v", leaked, rendered)
		}
	}
	excerpt, ok := result.StructuredContent["prompt_excerpt"].(string)
	if !ok || !strings.Contains(excerpt, "Billing details") {
		t.Fatalf("prompt excerpt missing summary text: %#v", result.StructuredContent["prompt_excerpt"])
	}
	if strings.Contains(excerpt, "Hidden discount text") {
		t.Fatalf("prompt excerpt used collapsed details body: %#v", excerpt)
	}
}

func TestWebFetchHTMLRenderingPreservesLinksAndImageText(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		_, _ = w.Write([]byte(`<!doctype html>
<html>
<body>
  <main>
    <p>Read the <a href="/docs/setup">setup guide</a> before deployment.</p>
    <p>External reference: <a href="https://example.com/reference">https://example.com/reference</a>.</p>
    <img alt="Architecture diagram" src="/assets/diagram.png">
    <img title="Release checklist" src="/assets/checklist.png">
    <img aria-label="Responsive diagram" srcset="/assets/responsive-1x.png 1x, /assets/responsive-2x.png 2x">
    <img alt="Lazy responsive diagram" data-srcset="/assets/lazy-1x.png 1x, /assets/lazy-2x.png 2x">
    <img alt="Lazy source diagram" data-src="/assets/lazy-source.png">
    <img alt="Lazy data placeholder" src="data:image/gif;base64,R0lGODlhAQABAIAAAAAAAP" data-original-src="/assets/real-lazy.png">
    <img alt="Srcset data placeholder" srcset="data:image/gif;base64,R0lGODlhAQABAIAAAAAAAP 1x, /assets/srcset-real.png 2x">
    <picture>
      <source media="(min-width: 800px)" srcset="/assets/hero-large.png 1x, /assets/hero-large@2x.png 2x">
      <img alt="Hero diagram" src="/assets/hero-fallback.png">
    </picture>
    <picture>
      <source srcset="javascript:alert(2)">
      <source srcset="/assets/secondary-source.png">
      <img alt="Secondary diagram" src="/assets/secondary-fallback.png">
    </picture>
    <picture>
      <source srcset="javascript:alert(3)">
      <img alt="Fallback diagram" src="/assets/fallback.png">
    </picture>
    <picture>
      <source srcset="data:image/gif;base64,R0lGODlhAQABAIAAAAAAAP 1x, /assets/picture-real.png 2x">
      <img alt="Picture data placeholder" src="/assets/picture-fallback.png">
    </picture>
    <a href="javascript:alert(1)">ignored script link</a>
    <a href="data:text/plain;base64,SGVsbG8=">ignored data link</a>
    <a href="blob:https://example.com/asset-id">ignored blob link</a>
    <a href="vbscript:msgbox(1)">ignored vbscript link</a>
  </main>
</body>
</html>`))
	}))
	defer server.Close()
	executor := webExecutor(t)
	result, err := executor.Execute(tool.Context{Context: context.Background(), Metadata: map[string]any{}}, contracts.ToolUse{
		ID:    "toolu_web_html_links",
		Name:  "WebFetch",
		Input: json.RawMessage(`{"url":` + strconvQuote(server.URL) + `,"prompt":"architecture diagram"}`),
	}, nil)
	if err != nil {
		t.Fatal(err)
	}
	rendered, ok := result.StructuredContent["rendered_body"].(string)
	if !ok {
		t.Fatalf("rendered body = %#v", result.StructuredContent["rendered_body"])
	}
	if !strings.Contains(rendered, "setup guide ("+server.URL+"/docs/setup)") {
		t.Fatalf("rendered body missing link href: %#v", rendered)
	}
	if strings.Count(rendered, "https://example.com/reference") != 1 {
		t.Fatalf("rendered body should not duplicate URL link text: %#v", rendered)
	}
	if !strings.Contains(rendered, "Image: Architecture diagram ("+server.URL+"/assets/diagram.png)") || !strings.Contains(rendered, "Image: Release checklist ("+server.URL+"/assets/checklist.png)") {
		t.Fatalf("rendered body missing image text: %#v", rendered)
	}
	if !strings.Contains(rendered, "Image: Responsive diagram ("+server.URL+"/assets/responsive-1x.png)") {
		t.Fatalf("rendered body missing srcset image text: %#v", rendered)
	}
	if !strings.Contains(rendered, "Image: Lazy responsive diagram ("+server.URL+"/assets/lazy-1x.png)") || !strings.Contains(rendered, "Image: Lazy source diagram ("+server.URL+"/assets/lazy-source.png)") {
		t.Fatalf("rendered body missing lazy image text: %#v", rendered)
	}
	if !strings.Contains(rendered, "Image: Lazy data placeholder ("+server.URL+"/assets/real-lazy.png)") || !strings.Contains(rendered, "Image: Srcset data placeholder ("+server.URL+"/assets/srcset-real.png)") {
		t.Fatalf("rendered body missing data-placeholder image text: %#v", rendered)
	}
	if !strings.Contains(rendered, "Image: Hero diagram ("+server.URL+"/assets/hero-large.png)") {
		t.Fatalf("rendered body missing picture source image text: %#v", rendered)
	}
	if !strings.Contains(rendered, "Image: Secondary diagram ("+server.URL+"/assets/secondary-source.png)") {
		t.Fatalf("rendered body missing second picture source image text: %#v", rendered)
	}
	if !strings.Contains(rendered, "Image: Fallback diagram ("+server.URL+"/assets/fallback.png)") {
		t.Fatalf("rendered body missing unsafe picture fallback image text: %#v", rendered)
	}
	if !strings.Contains(rendered, "Image: Picture data placeholder ("+server.URL+"/assets/picture-real.png)") {
		t.Fatalf("rendered body missing data-placeholder picture source text: %#v", rendered)
	}
	if strings.Contains(rendered, "javascript:alert") || strings.Contains(rendered, "data:image") || strings.Contains(rendered, "R0lGOD") ||
		strings.Contains(rendered, "data:text/plain") || strings.Contains(rendered, "blob:https://") || strings.Contains(rendered, "vbscript:") {
		t.Fatalf("rendered body kept unsafe href: %#v", rendered)
	}
	excerpt, ok := result.StructuredContent["prompt_excerpt"].(string)
	if !ok || !strings.Contains(excerpt, "Image: Architecture diagram") {
		t.Fatalf("prompt excerpt = %#v", result.StructuredContent["prompt_excerpt"])
	}
}

func TestWebFetchHTMLRenderingPreservesVisibleFormControls(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		_, _ = w.Write([]byte(`<!doctype html>
<html>
<body>
  <main>
    <form>
      <label for="query">Search</label>
      <input id="query" type="search" placeholder="Search docs">
      <input type="text" value="prefilled workspace">
      <input type="submit" value="Run search">
      <input type="image" alt="Search icon" src="/assets/search.png">
      <input type="checkbox" aria-label="Include archived docs">
      <button aria-label="Open filters"><svg><title>ignored svg</title></svg></button>
      <button aria-label="Visible label should not duplicate">Plain button text</button>
      <textarea placeholder="Describe the rollout"></textarea>
      <textarea placeholder="Draft placeholder">Existing note</textarea>
      <select aria-label="Environment">
        <option>Development</option>
        <option selected>Production</option>
        <option>Staging</option>
      </select>
      <select aria-label="Region">
        <option>US East</option>
        <option>EU West</option>
      </select>
      <select aria-label="Tier">
        <option label="Starter tier" value="starter-secret"></option>
        <option label="Enterprise tier" selected value="enterprise-secret"></option>
      </select>
      <select aria-label="Datacenters" multiple>
        <option selected>AMS</option>
        <option>LHR</option>
        <option selected>IAD</option>
      </select>
      <input type="hidden" value="csrf-secret">
      <input type="password" value="super-secret-password">
      <input type="file" value="/private/report.pdf">
    </form>
  </main>
</body>
</html>`))
	}))
	defer server.Close()
	executor := webExecutor(t)
	result, err := executor.Execute(tool.Context{Context: context.Background(), Metadata: map[string]any{}}, contracts.ToolUse{
		ID:    "toolu_web_html_forms",
		Name:  "WebFetch",
		Input: json.RawMessage(`{"url":` + strconvQuote(server.URL) + `,"prompt":"run search"}`),
	}, nil)
	if err != nil {
		t.Fatal(err)
	}
	rendered, ok := result.StructuredContent["rendered_body"].(string)
	if !ok {
		t.Fatalf("rendered body = %#v", result.StructuredContent["rendered_body"])
	}
	for _, want := range []string{
		"Input: Search docs",
		"Input: prefilled workspace",
		"Input: Run search",
		"Input: Search icon",
		"Input: Include archived docs",
		"Input: Open filters",
		"Plain button text",
		"Input: Describe the rollout",
		"Existing note",
		"Input: Environment: Production",
		"Input: Region: US East",
		"Input: Tier: Enterprise tier",
		"Input: Datacenters: AMS, IAD",
	} {
		if !strings.Contains(rendered, want) {
			t.Fatalf("rendered body missing %q: %#v", want, rendered)
		}
	}
	if strings.Contains(rendered, "Visible label should not duplicate") {
		t.Fatalf("rendered body duplicated non-empty button label: %#v", rendered)
	}
	for _, leaked := range []string{"Draft placeholder", "Development", "Staging", "EU West", "Starter tier", "starter-secret", "enterprise-secret", "LHR", "csrf-secret", "super-secret-password", "/private/report.pdf"} {
		if strings.Contains(rendered, leaked) {
			t.Fatalf("rendered body leaked %q: %#v", leaked, rendered)
		}
	}
	excerpt, ok := result.StructuredContent["prompt_excerpt"].(string)
	if !ok || !strings.Contains(excerpt, "Input: Run search") {
		t.Fatalf("prompt excerpt = %#v", result.StructuredContent["prompt_excerpt"])
	}
}

func TestWebFetchResolvesHTMLLinksAgainstFinalURL(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/start":
			http.Redirect(w, r, "/nested/page.html", http.StatusFound)
		case "/nested/page.html":
			w.Header().Set("Content-Type", "text/html; charset=utf-8")
			_, _ = w.Write([]byte(`<!doctype html>
<html>
<body>
  <a href="guide">Nested guide</a>
  <img alt="Nested diagram" src="../assets/diagram.png">
</body>
</html>`))
		default:
			http.NotFound(w, r)
		}
	}))
	defer server.Close()
	executor := webExecutor(t)
	result, err := executor.Execute(tool.Context{Context: context.Background(), Metadata: map[string]any{}}, contracts.ToolUse{
		ID:    "toolu_web_final_url",
		Name:  "WebFetch",
		Input: json.RawMessage(`{"url":` + strconvQuote(server.URL+"/start") + `,"prompt":"nested diagram"}`),
	}, nil)
	if err != nil {
		t.Fatal(err)
	}
	if result.StructuredContent["final_url"] != server.URL+"/nested/page.html" {
		t.Fatalf("final_url = %#v", result.StructuredContent["final_url"])
	}
	rendered, ok := result.StructuredContent["rendered_body"].(string)
	if !ok {
		t.Fatalf("rendered body = %#v", result.StructuredContent["rendered_body"])
	}
	if !strings.Contains(rendered, "Nested guide ("+server.URL+"/nested/guide)") {
		t.Fatalf("rendered body missing final-url-resolved link: %#v", rendered)
	}
	if !strings.Contains(rendered, "Image: Nested diagram ("+server.URL+"/assets/diagram.png)") {
		t.Fatalf("rendered body missing final-url-resolved image: %#v", rendered)
	}
	excerpt, ok := result.StructuredContent["prompt_excerpt"].(string)
	if !ok || !strings.Contains(excerpt, "Image: Nested diagram ("+server.URL+"/assets/diagram.png)") {
		t.Fatalf("prompt excerpt = %#v", result.StructuredContent["prompt_excerpt"])
	}
}

func TestWebFetchResolvesHTMLLinksAgainstBaseHref(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/start":
			http.Redirect(w, r, "/nested/page.html", http.StatusFound)
		case "/nested/page.html":
			w.Header().Set("Content-Type", "text/html; charset=utf-8")
			_, _ = w.Write([]byte(`<!doctype html>
<html>
<head><base href="/static/root/"></head>
<body>
  <a href="guide">Base guide</a>
  <img alt="Base diagram" src="images/diagram.png">
</body>
</html>`))
		default:
			http.NotFound(w, r)
		}
	}))
	defer server.Close()
	executor := webExecutor(t)
	result, err := executor.Execute(tool.Context{Context: context.Background(), Metadata: map[string]any{}}, contracts.ToolUse{
		ID:    "toolu_web_base_href",
		Name:  "WebFetch",
		Input: json.RawMessage(`{"url":` + strconvQuote(server.URL+"/start") + `,"prompt":"base diagram"}`),
	}, nil)
	if err != nil {
		t.Fatal(err)
	}
	rendered, ok := result.StructuredContent["rendered_body"].(string)
	if !ok {
		t.Fatalf("rendered body = %#v", result.StructuredContent["rendered_body"])
	}
	if !strings.Contains(rendered, "Base guide ("+server.URL+"/static/root/guide)") {
		t.Fatalf("rendered body missing base-href-resolved link: %#v", rendered)
	}
	if !strings.Contains(rendered, "Image: Base diagram ("+server.URL+"/static/root/images/diagram.png)") {
		t.Fatalf("rendered body missing base-href-resolved image: %#v", rendered)
	}
	if strings.Contains(rendered, server.URL+"/nested/guide") || strings.Contains(rendered, server.URL+"/nested/images/diagram.png") {
		t.Fatalf("rendered body resolved against final URL instead of base href: %#v", rendered)
	}
	excerpt, ok := result.StructuredContent["prompt_excerpt"].(string)
	if !ok || !strings.Contains(excerpt, "Image: Base diagram ("+server.URL+"/static/root/images/diagram.png)") {
		t.Fatalf("prompt excerpt = %#v", result.StructuredContent["prompt_excerpt"])
	}
}

func TestWebFetchReportsCrossHostRedirect(t *testing.T) {
	var targetHits atomic.Int32
	target := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		targetHits.Add(1)
		w.Header().Set("Content-Type", "text/plain; charset=utf-8")
		_, _ = w.Write([]byte("target body should not be fetched"))
	}))
	defer target.Close()
	redirectURL := target.URL + "/landing"
	source := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Redirect(w, r, redirectURL, http.StatusFound)
	}))
	defer source.Close()

	executor := webExecutor(t)
	result, err := executor.Execute(tool.Context{Context: context.Background(), Metadata: map[string]any{}}, contracts.ToolUse{
		ID:    "toolu_web_cross_host_redirect",
		Name:  "WebFetch",
		Input: json.RawMessage(`{"url":` + strconvQuote(source.URL) + `,"prompt":"summarize redirected page"}`),
	}, nil)
	if err != nil {
		t.Fatal(err)
	}
	if result.IsError {
		t.Fatalf("redirect notice should not be marked as error: %#v", result)
	}
	if hits := targetHits.Load(); hits != 0 {
		t.Fatalf("cross-host redirect target should not be fetched; hits=%d", hits)
	}
	content := result.Content.(string)
	if !strings.Contains(content, "REDIRECT DETECTED") || !strings.Contains(content, redirectURL) || !strings.Contains(content, source.URL) {
		t.Fatalf("content = %#v", content)
	}
	if strings.Contains(content, "target body should not be fetched") {
		t.Fatalf("content included redirected target body: %#v", content)
	}
	if result.StructuredContent["redirect_detected"] != true ||
		result.StructuredContent["redirect_url"] != redirectURL ||
		result.StructuredContent["final_url"] != source.URL ||
		result.StructuredContent["status_code"] != http.StatusFound {
		t.Fatalf("structured content = %#v", result.StructuredContent)
	}
	body, ok := result.StructuredContent["body"].(string)
	if !ok || !strings.Contains(body, "REDIRECT DETECTED") {
		t.Fatalf("body = %#v", result.StructuredContent["body"])
	}
}

func TestWebFetchPromptPhraseScoring(t *testing.T) {
	terms := webFetchPromptTerms("release candidate")
	phrases := webFetchPromptPhrases("release candidate")
	separate := scoreWebFetchPassage("Release plans mention candidate owners. Release owners review candidate risks.", terms, phrases)
	exact := scoreWebFetchPassage("The release candidate is ready.", terms, phrases)
	if exact <= separate {
		t.Fatalf("exact phrase score = %d, separate term score = %d", exact, separate)
	}
	if score := scoreWebFetchPassage("Trust the process.", []string{"rust"}, nil); score != 0 {
		t.Fatalf("substring term should not match word-boundary scoring: %d", score)
	}
	if score := scoreWebFetchPassage("The beta plan costs $20.", []string{"cost"}, nil); score == 0 {
		t.Fatalf("singular prompt term should match plural passage word")
	}
	if score := scoreWebFetchPassage("The beta plan cost $20.", []string{"costs"}, nil); score == 0 {
		t.Fatalf("plural prompt term should match singular passage word")
	}
}

func TestWebFetchPreflightSkipsBinaryGet(t *testing.T) {
	var heads int
	var gets int
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.Method {
		case http.MethodHead:
			heads++
			w.Header().Set("Content-Type", "application/pdf")
			w.Header().Set("Content-Length", "4096")
			w.WriteHeader(http.StatusOK)
		case http.MethodGet:
			gets++
			w.Header().Set("Content-Type", "application/pdf")
			_, _ = w.Write([]byte("%PDF should not be downloaded"))
		default:
			http.Error(w, "unexpected method", http.StatusMethodNotAllowed)
		}
	}))
	defer server.Close()
	executor := webExecutor(t)
	result, err := executor.Execute(tool.Context{Context: context.Background(), Metadata: map[string]any{}}, contracts.ToolUse{
		ID:    "toolu_web_preflight",
		Name:  "WebFetch",
		Input: json.RawMessage(`{"url":` + strconvQuote(server.URL) + `,"prompt":"summarize"}`),
	}, nil)
	if err != nil {
		t.Fatal(err)
	}
	if heads != 1 || gets != 0 {
		t.Fatalf("heads=%d gets=%d", heads, gets)
	}
	if !result.IsError || result.StructuredContent["binary"] != true {
		t.Fatalf("result = %#v", result)
	}
	preflight, ok := result.StructuredContent["preflight"].(map[string]any)
	if !ok || preflight["attempted"] != true || preflight["skipped_get"] != true || preflight["content_type"] != "application/pdf" {
		t.Fatalf("preflight = %#v", result.StructuredContent["preflight"])
	}
	if !strings.Contains(result.Content.(string), "Preflight identified binary content") {
		t.Fatalf("content = %#v", result.Content)
	}
}

func TestWebFetchPreflightSkipsBinaryAttachmentFilename(t *testing.T) {
	var heads int
	var gets int
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.Method {
		case http.MethodHead:
			heads++
			w.Header().Set("Content-Disposition", `attachment; filename="report.pdf"`)
			w.WriteHeader(http.StatusOK)
		case http.MethodGet:
			gets++
			w.Header().Set("Content-Type", "application/pdf")
			_, _ = w.Write([]byte("%PDF should not be downloaded"))
		default:
			http.Error(w, "unexpected method", http.StatusMethodNotAllowed)
		}
	}))
	defer server.Close()
	executor := webExecutor(t)
	result, err := executor.Execute(tool.Context{Context: context.Background(), Metadata: map[string]any{}}, contracts.ToolUse{
		ID:    "toolu_web_preflight_attachment",
		Name:  "WebFetch",
		Input: json.RawMessage(`{"url":` + strconvQuote(server.URL) + `,"prompt":"summarize"}`),
	}, nil)
	if err != nil {
		t.Fatal(err)
	}
	if heads != 1 || gets != 0 {
		t.Fatalf("heads=%d gets=%d", heads, gets)
	}
	if !result.IsError || result.StructuredContent["binary"] != true {
		t.Fatalf("result = %#v", result)
	}
	preflight, ok := result.StructuredContent["preflight"].(map[string]any)
	if !ok || preflight["skipped_get"] != true || preflight["content_disposition"] != `attachment; filename="report.pdf"` {
		t.Fatalf("preflight = %#v", result.StructuredContent["preflight"])
	}
}

func TestWebFetchSkipPreflightMetadata(t *testing.T) {
	var heads int
	var gets int
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.Method {
		case http.MethodHead:
			heads++
			w.Header().Set("Content-Type", "application/pdf")
			w.WriteHeader(http.StatusOK)
		case http.MethodGet:
			gets++
			w.Header().Set("Content-Type", "text/plain; charset=utf-8")
			_, _ = w.Write([]byte("downloaded after skipping preflight"))
		default:
			http.Error(w, "unexpected method", http.StatusMethodNotAllowed)
		}
	}))
	defer server.Close()
	executor := webExecutor(t)
	result, err := executor.Execute(tool.Context{
		Context: context.Background(),
		Metadata: map[string]any{
			MetadataWebFetchSkipPreflightKey: true,
		},
	}, contracts.ToolUse{
		ID:    "toolu_web_skip_preflight",
		Name:  "WebFetch",
		Input: json.RawMessage(`{"url":` + strconvQuote(server.URL) + `,"prompt":"summarize"}`),
	}, nil)
	if err != nil {
		t.Fatal(err)
	}
	if heads != 0 || gets != 1 {
		t.Fatalf("heads=%d gets=%d", heads, gets)
	}
	if result.IsError || result.StructuredContent["body"] != "downloaded after skipping preflight" {
		t.Fatalf("result = %#v", result)
	}
	preflight, ok := result.StructuredContent["preflight"].(map[string]any)
	if !ok || preflight["attempted"] != false || preflight["skipped"] != true || preflight["skipped_get"] != false {
		t.Fatalf("preflight = %#v", result.StructuredContent["preflight"])
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
		Input: json.RawMessage(`{"url":` + strconvQuote(server.URL+"/missing") + `,"prompt":"summarize"}`),
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
		Input: json.RawMessage(`{"url":` + strconvQuote(server.URL+"/binary") + `,"prompt":"summarize"}`),
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
		{name: "missing prompt", input: `{"url":"https://example.com"}`, want: "input.prompt is required"},
		{name: "ftp", input: `{"url":"ftp://example.com/file","prompt":"summarize"}`, want: "url must use http or https"},
		{name: "missing host", input: `{"url":"https:///path","prompt":"summarize"}`, want: "url must include a hostname"},
		{name: "bad timeout", input: `{"url":"https://example.com","prompt":"summarize","timeout":0}`, want: "timeout must be positive"},
		{name: "bad max bytes", input: `{"url":"https://example.com","prompt":"summarize","max_bytes":0}`, want: "max_bytes must be positive"},
		{name: "unknown field", input: `{"url":"https://example.com","prompt":"summarize","extra":true}`, want: "input.extra is not allowed"},
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
	decision, err := webFetch.CheckPermissions(ctx, json.RawMessage(`{"url":"https://example.com/path","prompt":"summarize"}`))
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
