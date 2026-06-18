package webtools

import (
	"context"
	"encoding/json"
	"fmt"
	htmlstd "html"
	"io"
	"net/http"
	"net/url"
	"regexp"
	"strings"
	"time"

	"ccgo/internal/contracts"
	"ccgo/internal/tool"
)

const (
	MetadataWebSearchEndpointKey = "ccgo.tools.web.search_endpoint"

	defaultWebSearchEndpoint      = "https://duckduckgo.com/html/"
	defaultWebSearchTimeoutMillis = 30_000
	maxWebSearchTimeoutMillis     = 120_000
	defaultWebSearchLimit         = 5
	maxWebSearchLimit             = 20
	maxWebSearchResponseBytes     = 1_000_000
)

type webSearchInput struct {
	Query             string   `json:"query"`
	AllowedDomains    []string `json:"allowed_domains,omitempty"`
	AllowedDomainsAlt []string `json:"allowedDomains,omitempty"`
	BlockedDomains    []string `json:"blocked_domains,omitempty"`
	BlockedDomainsAlt []string `json:"blockedDomains,omitempty"`
	MaxResults        *int     `json:"max_results,omitempty"`
	MaxResultsAlt     *int     `json:"maxResults,omitempty"`
	Timeout           *int     `json:"timeout,omitempty"`
}

var allowedWebSearchInputKeys = map[string]struct{}{
	"query":           {},
	"allowed_domains": {},
	"allowedDomains":  {},
	"blocked_domains": {},
	"blockedDomains":  {},
	"max_results":     {},
	"maxResults":      {},
	"timeout":         {},
}

var webSearchSemanticNumberKeys = map[string]struct{}{
	"max_results": {},
	"maxResults":  {},
	"timeout":     {},
}

type searchResult struct {
	Title   string
	URL     string
	Snippet string
}

type webSearchResult struct {
	Results    []searchResult
	SourceURL  string
	StatusCode int
	DurationMS int64
}

func NewWebSearchTool() tool.Tool {
	return tool.FuncTool{
		DefinitionValue: contracts.ToolDefinition{
			Name:               "WebSearch",
			Description:        "Search the web.",
			SearchHint:         "search web",
			ReadOnly:           true,
			ConcurrencySafe:    true,
			Strict:             true,
			MaxResultSizeChars: 100_000,
			InputSchema: contracts.JSONSchema{
				"type":     "object",
				"required": []any{"query"},
				"properties": map[string]any{
					"query":           map[string]any{"type": "string", "minLength": 2},
					"allowed_domains": map[string]any{"type": "array", "items": map[string]any{"type": "string"}},
					"allowedDomains":  map[string]any{"type": "array", "items": map[string]any{"type": "string"}},
					"blocked_domains": map[string]any{"type": "array", "items": map[string]any{"type": "string"}},
					"blockedDomains":  map[string]any{"type": "array", "items": map[string]any{"type": "string"}},
					"max_results":     map[string]any{"type": "integer"},
					"maxResults":      map[string]any{"type": "integer"},
					"timeout":         map[string]any{"type": "integer"},
				},
			},
		},
		PromptFunc: func(tool.PromptContext) (string, error) {
			return "Searches the web for a query and returns result titles and URLs. Supports optional allowed_domains, blocked_domains, max_results, and timeout. Official search backend parity is not implemented yet.", nil
		},
		NormalizeFunc:   normalizeWebSearchRawInput,
		ValidateFunc:    validateWebSearch,
		CallFunc:        callWebSearch,
		ReadOnlyFunc:    func(json.RawMessage) bool { return true },
		ConcurrencyFunc: func(json.RawMessage) bool { return true },
	}
}

func validateWebSearch(_ tool.Context, raw json.RawMessage) error {
	input, err := decodeWebSearch(raw)
	if err != nil {
		return err
	}
	if strings.TrimSpace(input.Query) == "" {
		return fmt.Errorf("query is required")
	}
	limit := webSearchLimit(input)
	if limit <= 0 {
		return fmt.Errorf("max_results must be positive")
	}
	if limit > maxWebSearchLimit {
		return fmt.Errorf("max_results must be at most %d", maxWebSearchLimit)
	}
	if input.Timeout != nil {
		if *input.Timeout <= 0 {
			return fmt.Errorf("timeout must be positive")
		}
		if *input.Timeout > maxWebSearchTimeoutMillis {
			return fmt.Errorf("timeout must be at most %d milliseconds", maxWebSearchTimeoutMillis)
		}
	}
	allowedDomainsRaw := webSearchAllowedDomainsRaw(input)
	blockedDomainsRaw := webSearchBlockedDomainsRaw(input)
	allowedDomains := normalizeDomains(allowedDomainsRaw)
	blockedDomains := normalizeDomains(blockedDomainsRaw)
	if len(allowedDomains) > 0 && len(blockedDomains) > 0 {
		return fmt.Errorf("Cannot specify both allowed_domains and blocked_domains in the same request")
	}
	if err := validateDomains("allowed_domains", allowedDomainsRaw); err != nil {
		return err
	}
	return validateDomains("blocked_domains", blockedDomainsRaw)
}

func callWebSearch(ctx tool.Context, raw json.RawMessage, _ tool.ProgressSink) (contracts.ToolResult, error) {
	input, err := decodeWebSearch(raw)
	if err != nil {
		return contracts.ToolResult{}, err
	}
	endpoint := webSearchEndpoint(ctx.Metadata)
	limit := webSearchLimit(input)
	result, err := runWebSearch(ctx.Context, endpoint, input, limit)
	if err != nil {
		return contracts.ToolResult{}, err
	}
	return contracts.ToolResult{
		Content: formatWebSearchContent(input.Query, result.Results),
		IsError: result.StatusCode < 200 || result.StatusCode >= 300,
		StructuredContent: map[string]any{
			"type":            "web_search",
			"query":           input.Query,
			"results":         structuredSearchResults(result.Results),
			"allowed_domains": webSearchAllowedDomains(input),
			"blocked_domains": webSearchBlockedDomains(input),
			"source_url":      result.SourceURL,
			"status_code":     result.StatusCode,
			"duration_ms":     result.DurationMS,
		},
	}, nil
}

func runWebSearch(ctx context.Context, endpoint string, input webSearchInput, limit int) (webSearchResult, error) {
	if ctx == nil {
		ctx = context.Background()
	}
	searchURL, err := buildSearchURL(endpoint, input.Query)
	if err != nil {
		return webSearchResult{}, err
	}
	searchCtx, cancel := context.WithTimeout(ctx, webSearchTimeout(input))
	defer cancel()
	req, err := http.NewRequestWithContext(searchCtx, http.MethodGet, searchURL, nil)
	if err != nil {
		return webSearchResult{}, err
	}
	req.Header.Set("User-Agent", "ccgo-websearch/0.1")
	start := time.Now()
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return webSearchResult{}, err
	}
	defer resp.Body.Close()
	data, err := io.ReadAll(io.LimitReader(resp.Body, maxWebSearchResponseBytes))
	if err != nil {
		return webSearchResult{}, err
	}
	results := filterSearchResults(parseSearchResults(string(data), resp.Request.URL), webSearchAllowedDomains(input), webSearchBlockedDomains(input), limit)
	return webSearchResult{
		Results:    results,
		SourceURL:  resp.Request.URL.String(),
		StatusCode: resp.StatusCode,
		DurationMS: time.Since(start).Milliseconds(),
	}, nil
}

func buildSearchURL(endpoint string, query string) (string, error) {
	parsed, err := url.Parse(endpoint)
	if err != nil {
		return "", err
	}
	values := parsed.Query()
	values.Set("q", query)
	parsed.RawQuery = values.Encode()
	return parsed.String(), nil
}

func parseSearchResults(body string, base *url.URL) []searchResult {
	if results, ok := parseJSONSearchResults(body, base); ok {
		return results
	}
	return parseHTMLSearchResults(body, base)
}

func parseHTMLSearchResults(body string, base *url.URL) []searchResult {
	anchors := anchorRe.FindAllStringSubmatchIndex(body, -1)
	resultBase := searchHTMLBaseURL(body, base)
	results := parseHTMLJSONLDSearchResults(body, resultBase)
	seen := map[string]struct{}{}
	for _, result := range results {
		seen[result.URL] = struct{}{}
	}
	for idx, anchor := range anchors {
		attrs := body[anchor[2]:anchor[3]]
		if isSearchSnippetAnchor(attrs) {
			continue
		}
		label := strings.TrimSpace(stripHTML(body[anchor[4]:anchor[5]]))
		if label == "" {
			continue
		}
		href := hrefFromAttrs(attrs)
		resolved := resolveSearchURL(href, resultBase)
		if resolved == "" || isSearchChromeURL(resolved) {
			continue
		}
		if _, ok := seen[resolved]; ok {
			continue
		}
		seen[resolved] = struct{}{}
		results = append(results, searchResult{Title: label, URL: resolved, Snippet: searchSnippetAfterAnchor(body, anchors, idx)})
	}
	return results
}

func parseHTMLJSONLDSearchResults(body string, base *url.URL) []searchResult {
	var results []searchResult
	seen := map[string]struct{}{}
	for _, match := range scriptTagRe.FindAllStringSubmatch(body, -1) {
		if len(match) < 3 || !strings.Contains(strings.ToLower(match[1]), "ld+json") {
			continue
		}
		parsed, ok := parseJSONSearchResults(htmlstd.UnescapeString(match[2]), base)
		if !ok {
			continue
		}
		for _, result := range parsed {
			if _, exists := seen[result.URL]; exists {
				continue
			}
			seen[result.URL] = struct{}{}
			results = append(results, result)
		}
	}
	return results
}

func searchHTMLBaseURL(body string, fallback *url.URL) *url.URL {
	searchBody := htmlCommentRe.ReplaceAllString(body, "")
	for _, match := range baseTagRe.FindAllStringSubmatch(searchBody, -1) {
		if len(match) < 2 {
			continue
		}
		href := strings.TrimSpace(hrefFromAttrs(match[1]))
		if href == "" || strings.HasPrefix(strings.ToLower(href), "javascript:") {
			continue
		}
		parsed, err := url.Parse(href)
		if err != nil {
			continue
		}
		if fallback != nil {
			parsed = fallback.ResolveReference(parsed)
		}
		if parsed.Scheme == "http" || parsed.Scheme == "https" {
			return parsed
		}
	}
	return fallback
}

func parseJSONSearchResults(body string, base *url.URL) ([]searchResult, bool) {
	trimmed := strings.TrimSpace(body)
	if !strings.HasPrefix(trimmed, "{") && !strings.HasPrefix(trimmed, "[") {
		return nil, false
	}
	var payload any
	if err := json.Unmarshal([]byte(trimmed), &payload); err != nil {
		return nil, false
	}
	var results []searchResult
	seen := map[string]struct{}{}
	collectJSONSearchResults(payload, base, &results, seen)
	return results, true
}

func collectJSONSearchResults(value any, base *url.URL, results *[]searchResult, seen map[string]struct{}) {
	switch typed := value.(type) {
	case []any:
		for _, item := range typed {
			collectJSONSearchResults(item, base, results, seen)
		}
	case map[string]any:
		if result, ok := searchResultFromJSONObject(typed, base); ok {
			if _, exists := seen[result.URL]; !exists {
				seen[result.URL] = struct{}{}
				*results = append(*results, result)
			}
		}
		for _, key := range []string{
			"results", "organic_results", "organicResults", "items", "value", "data",
			"webPages", "web_pages", "web", "response", "search",
			"hits", "documents", "records", "entries", "organic",
			"answer_box", "answerBox", "knowledge_graph", "knowledgeGraph",
			"news", "news_results", "newsResults", "top_stories", "topStories",
			"people_also_ask", "peopleAlsoAsk", "related_questions", "relatedQuestions",
			"deepLinks", "deep_links", "siteLinks", "sitelinks", "pages", "matches",
			"@graph", "itemListElement", "item_list_element", "item", "mainEntity", "main_entity",
		} {
			if child, ok := typed[key]; ok {
				collectJSONSearchResults(child, base, results, seen)
			}
		}
	}
}

func searchResultFromJSONObject(obj map[string]any, base *url.URL) (searchResult, bool) {
	rawURL := jsonSearchURLField(obj)
	resolved := resolveSearchURL(rawURL, base)
	if resolved == "" || isSearchChromeURL(resolved) {
		return searchResult{}, false
	}
	title := cleanJSONSearchText(jsonStringField(obj,
		"title", "name", "headline", "heading",
		"question", "label", "htmlTitle", "html_title",
	))
	if strings.TrimSpace(title) == "" {
		title = resolved
	}
	snippet := cleanJSONSearchText(jsonStringField(obj,
		"snippet", "description", "content", "text",
		"htmlSnippet", "html_snippet",
		"summary", "extract", "abstract", "body", "caption",
		"answer", "excerpt",
	))
	return searchResult{
		Title:   title,
		URL:     resolved,
		Snippet: truncateSearchSnippet(snippet, 320),
	}, true
}

func jsonSearchURLField(obj map[string]any) string {
	if raw := jsonStringField(obj,
		"url", "link", "href", "@id",
		"pageUrl", "pageURL", "page_url",
		"targetUrl", "targetURL", "target_url",
		"webUrl", "webURL", "web_url",
		"sourceUrl", "sourceURL", "source_url",
		"sourceLink", "source_link",
		"website", "site",
	); raw != "" {
		return raw
	}
	raw := jsonStringField(obj,
		"displayUrl", "displayURL", "display_url",
		"formattedUrl", "formattedURL", "formatted_url",
	)
	if isAbsoluteSearchURL(raw) {
		return raw
	}
	return ""
}

func jsonStringField(obj map[string]any, names ...string) string {
	for _, name := range names {
		if value, ok := obj[name]; ok {
			if text := jsonSearchString(value); text != "" {
				return text
			}
		}
	}
	return ""
}

func jsonSearchString(value any) string {
	switch typed := value.(type) {
	case string:
		return strings.TrimSpace(typed)
	case float64:
		return strings.TrimSpace(fmt.Sprintf("%.0f", typed))
	case map[string]any:
		return jsonStringField(typed,
			"url", "link", "href", "@id",
			"raw", "value", "text", "content",
			"title", "name", "headline", "heading",
		)
	case []any:
		for _, item := range typed {
			if text := jsonSearchString(item); text != "" {
				return text
			}
		}
		return ""
	default:
		return ""
	}
}

func cleanJSONSearchText(value string) string {
	value = htmlstd.UnescapeString(value)
	value = jsonHTMLTagRe.ReplaceAllString(value, "")
	return strings.Join(strings.Fields(value), " ")
}

func isAbsoluteSearchURL(raw string) bool {
	raw = strings.TrimSpace(strings.ToLower(raw))
	return strings.HasPrefix(raw, "http://") || strings.HasPrefix(raw, "https://") || strings.HasPrefix(raw, "//")
}

var (
	anchorRe      = regexp.MustCompile(`(?is)<a\b([^>]*)>(.*?)</a>`)
	baseTagRe     = regexp.MustCompile(`(?is)<base\b([^>]*)>`)
	scriptTagRe   = regexp.MustCompile(`(?is)<script\b([^>]*)>(.*?)</script>`)
	htmlCommentRe = regexp.MustCompile(`(?is)<!--.*?-->`)
	hrefRe        = regexp.MustCompile(`(?is)\bhref\s*=\s*(?:"([^"]*)"|'([^']*)'|([^\s>]+))`)
	snippetRe     = regexp.MustCompile(`(?is)<(?:a|div|span|td|p)\b[^>]*(?:result__snippet|snippet)[^>]*>(.*?)</(?:a|div|span|td|p)>`)
	tagRe         = regexp.MustCompile(`(?is)<[^>]+>`)
	jsonHTMLTagRe = regexp.MustCompile(`(?is)</?[a-z][a-z0-9:-]*(?:\s[^>]*)?>`)
)

func hrefFromAttrs(attrs string) string {
	match := hrefRe.FindStringSubmatch(attrs)
	if len(match) == 0 {
		return ""
	}
	for _, item := range match[1:] {
		if item != "" {
			return htmlstd.UnescapeString(item)
		}
	}
	return ""
}

func stripHTML(value string) string {
	plain := tagRe.ReplaceAllString(value, "")
	plain = htmlstd.UnescapeString(plain)
	return strings.Join(strings.Fields(plain), " ")
}

func isSearchSnippetAnchor(attrs string) bool {
	lower := strings.ToLower(attrs)
	return strings.Contains(lower, "snippet") && !strings.Contains(lower, "result__a")
}

func searchSnippetAfterAnchor(body string, anchors [][]int, index int) string {
	if index < 0 || index >= len(anchors) {
		return ""
	}
	start := anchors[index][1]
	end := len(body)
	for next := index + 1; next < len(anchors); next++ {
		attrs := body[anchors[next][2]:anchors[next][3]]
		if !isSearchSnippetAnchor(attrs) {
			end = anchors[next][0]
			break
		}
	}
	if start >= end {
		return ""
	}
	return extractSearchSnippet(body[start:end])
}

func extractSearchSnippet(fragment string) string {
	match := snippetRe.FindStringSubmatch(fragment)
	if len(match) < 2 {
		return ""
	}
	return truncateSearchSnippet(stripHTML(match[1]), 320)
}

func truncateSearchSnippet(snippet string, limit int) string {
	if limit <= 0 || len([]rune(snippet)) <= limit {
		return snippet
	}
	runes := []rune(snippet)
	return string(runes[:limit]) + "..."
}

func resolveSearchURL(raw string, base *url.URL) string {
	if strings.TrimSpace(raw) == "" {
		return ""
	}
	parsed, err := url.Parse(strings.TrimSpace(raw))
	if err != nil {
		return ""
	}
	if base != nil {
		parsed = base.ResolveReference(parsed)
	}
	if unwrapped := unwrapSearchRedirectURL(parsed); unwrapped != nil {
		parsed = unwrapped
	}
	if parsed.Scheme != "http" && parsed.Scheme != "https" {
		return ""
	}
	return parsed.String()
}

func unwrapSearchRedirectURL(parsed *url.URL) *url.URL {
	if parsed == nil {
		return nil
	}
	current := *parsed
	for i := 0; i < 3; i++ {
		target := searchRedirectTarget(&current)
		if target == "" {
			return &current
		}
		next, err := url.Parse(target)
		if err != nil {
			return &current
		}
		if next.Scheme == "" && next.Host != "" && (current.Scheme == "http" || current.Scheme == "https") {
			next.Scheme = current.Scheme
		}
		if !next.IsAbs() || (next.Scheme != "http" && next.Scheme != "https") {
			return &current
		}
		current = *next
	}
	return &current
}

func searchRedirectTarget(parsed *url.URL) string {
	if parsed == nil {
		return ""
	}
	values := parsed.Query()
	if isDuckDuckGoHost(parsed.Hostname()) && strings.HasPrefix(parsed.Path, "/l/") {
		return firstAbsoluteSearchRedirectValue(values, "uddg")
	}
	if !looksLikeSearchRedirectPath(parsed.Path) {
		return ""
	}
	return firstAbsoluteSearchRedirectValue(values, "url", "u", "target", "to", "redirect", "dest", "destination", "q")
}

func firstAbsoluteSearchRedirectValue(values url.Values, keys ...string) string {
	for _, key := range keys {
		for _, candidate := range values[key] {
			if target := cleanSearchRedirectCandidate(candidate); target != "" {
				return target
			}
		}
	}
	return ""
}

func cleanSearchRedirectCandidate(raw string) string {
	raw = strings.TrimSpace(raw)
	for i := 0; i < 2; i++ {
		if isAbsoluteSearchURL(raw) {
			return raw
		}
		unescaped, err := url.QueryUnescape(raw)
		if err != nil || unescaped == raw {
			break
		}
		raw = strings.TrimSpace(unescaped)
	}
	if isAbsoluteSearchURL(raw) {
		return raw
	}
	return ""
}

func looksLikeSearchRedirectPath(path string) bool {
	path = strings.ToLower(strings.TrimSpace(path))
	switch strings.TrimSuffix(path, "/") {
	case "/url", "/redirect", "/redirects", "/link", "/links", "/out", "/outbound", "/away":
		return true
	default:
		return strings.HasPrefix(path, "/url/") ||
			strings.HasPrefix(path, "/redirect/") ||
			strings.HasPrefix(path, "/link/") ||
			strings.HasPrefix(path, "/out/") ||
			strings.HasPrefix(path, "/outbound/") ||
			strings.HasPrefix(path, "/away/")
	}
}

func isSearchChromeURL(raw string) bool {
	parsed, err := url.Parse(raw)
	if err != nil {
		return true
	}
	host := strings.ToLower(parsed.Hostname())
	if host == "" {
		return true
	}
	if isDuckDuckGoHost(host) && !strings.HasPrefix(parsed.Path, "/l/") {
		return true
	}
	return false
}

func isDuckDuckGoHost(host string) bool {
	host = strings.TrimSuffix(strings.ToLower(strings.TrimSpace(host)), ".")
	return host == "duckduckgo.com" || strings.HasSuffix(host, ".duckduckgo.com")
}

func filterSearchResults(results []searchResult, allowedDomains []string, blockedDomains []string, limit int) []searchResult {
	out := make([]searchResult, 0, len(results))
	for _, result := range results {
		host := hostname(result.URL)
		if host == "" {
			continue
		}
		if len(allowedDomains) > 0 && !domainAllowed(host, allowedDomains) {
			continue
		}
		if domainAllowed(host, blockedDomains) {
			continue
		}
		out = append(out, result)
		if len(out) >= limit {
			break
		}
	}
	return out
}

func hostname(raw string) string {
	parsed, err := url.Parse(raw)
	if err != nil {
		return ""
	}
	return strings.ToLower(parsed.Hostname())
}

func domainAllowed(host string, domains []string) bool {
	host = strings.TrimSuffix(strings.ToLower(host), ".")
	for _, domain := range domains {
		domain = strings.TrimPrefix(strings.TrimSuffix(strings.ToLower(strings.TrimSpace(domain)), "."), "*.")
		if domain == "" {
			continue
		}
		if host == domain || strings.HasSuffix(host, "."+domain) {
			return true
		}
	}
	return false
}

func formatWebSearchContent(query string, results []searchResult) string {
	if len(results) == 0 {
		return "No search results found for: " + query
	}
	var b strings.Builder
	fmt.Fprintf(&b, "Search results for: %s\n", query)
	for i, result := range results {
		fmt.Fprintf(&b, "\n%d. %s\n   %s", i+1, result.Title, result.URL)
		if result.Snippet != "" {
			fmt.Fprintf(&b, "\n   %s", result.Snippet)
		}
	}
	return b.String()
}

func decodeWebSearch(raw json.RawMessage) (webSearchInput, error) {
	obj, err := decodeWebStrictObject(raw, allowedWebSearchInputKeys)
	if err != nil {
		return webSearchInput{}, err
	}
	coerceWebSemanticJSONNumbers(obj, webSearchSemanticNumberKeys)
	var input webSearchInput
	data, err := json.Marshal(obj)
	if err != nil {
		return webSearchInput{}, err
	}
	if err := json.Unmarshal(data, &input); err != nil {
		return webSearchInput{}, err
	}
	return input, nil
}

func normalizeWebSearchRawInput(raw json.RawMessage) (json.RawMessage, error) {
	obj, err := decodeWebStrictObject(raw, allowedWebSearchInputKeys)
	if err != nil {
		return nil, err
	}
	coerceWebSemanticJSONNumbers(obj, webSearchSemanticNumberKeys)
	data, err := json.Marshal(obj)
	if err != nil {
		return nil, err
	}
	return data, nil
}

func webSearchEndpoint(metadata map[string]any) string {
	if metadata != nil {
		if endpoint, ok := metadata[MetadataWebSearchEndpointKey].(string); ok && strings.TrimSpace(endpoint) != "" {
			return strings.TrimSpace(endpoint)
		}
	}
	return defaultWebSearchEndpoint
}

func webSearchLimit(input webSearchInput) int {
	if input.MaxResults != nil {
		return *input.MaxResults
	}
	if input.MaxResultsAlt != nil {
		return *input.MaxResultsAlt
	}
	return defaultWebSearchLimit
}

func webSearchTimeout(input webSearchInput) time.Duration {
	if input.Timeout == nil {
		return time.Duration(defaultWebSearchTimeoutMillis) * time.Millisecond
	}
	return time.Duration(*input.Timeout) * time.Millisecond
}

func webSearchAllowedDomains(input webSearchInput) []string {
	return normalizeDomains(webSearchAllowedDomainsRaw(input))
}

func webSearchAllowedDomainsRaw(input webSearchInput) []string {
	if len(input.AllowedDomains) > 0 {
		return input.AllowedDomains
	}
	return input.AllowedDomainsAlt
}

func webSearchBlockedDomains(input webSearchInput) []string {
	return normalizeDomains(webSearchBlockedDomainsRaw(input))
}

func webSearchBlockedDomainsRaw(input webSearchInput) []string {
	if len(input.BlockedDomains) > 0 {
		return input.BlockedDomains
	}
	return input.BlockedDomainsAlt
}

func normalizeDomains(domains []string) []string {
	out := make([]string, 0, len(domains))
	for _, domain := range domains {
		domain = strings.TrimSpace(strings.ToLower(domain))
		if domain != "" {
			out = append(out, domain)
		}
	}
	return out
}

func validateDomains(field string, domains []string) error {
	for i, domain := range domains {
		if strings.Contains(domain, "://") || strings.ContainsAny(domain, "/?#") {
			return fmt.Errorf("%s[%d] must be a domain name, not a URL", field, i)
		}
		if !isValidWebSearchDomain(domain) {
			return fmt.Errorf("%s[%d] must be a domain name", field, i)
		}
	}
	return nil
}

func isValidWebSearchDomain(domain string) bool {
	domain = strings.TrimSuffix(strings.ToLower(strings.TrimSpace(domain)), ".")
	if domain == "" || strings.ContainsAny(domain, " \t\r\n:") {
		return false
	}
	if strings.HasPrefix(domain, "*.") {
		domain = strings.TrimPrefix(domain, "*.")
	} else if strings.Contains(domain, "*") {
		return false
	}
	if domain == "" {
		return false
	}
	for _, label := range strings.Split(domain, ".") {
		if label == "" || len(label) > 63 || strings.HasPrefix(label, "-") || strings.HasSuffix(label, "-") {
			return false
		}
		for _, r := range label {
			if (r >= 'a' && r <= 'z') || (r >= '0' && r <= '9') || r == '-' {
				continue
			}
			return false
		}
	}
	return true
}

func structuredSearchResults(results []searchResult) []map[string]any {
	out := make([]map[string]any, 0, len(results))
	for _, result := range results {
		out = append(out, map[string]any{
			"title":   result.Title,
			"url":     result.URL,
			"snippet": result.Snippet,
		})
	}
	return out
}
