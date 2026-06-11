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
					"query":           map[string]any{"type": "string"},
					"allowed_domains": map[string]any{"type": "array"},
					"allowedDomains":  map[string]any{"type": "array"},
					"blocked_domains": map[string]any{"type": "array"},
					"blockedDomains":  map[string]any{"type": "array"},
					"max_results":     map[string]any{"type": "integer"},
					"maxResults":      map[string]any{"type": "integer"},
					"timeout":         map[string]any{"type": "integer"},
				},
			},
		},
		PromptFunc: func(tool.PromptContext) (string, error) {
			return "Searches the web for a query and returns result titles and URLs. Supports optional allowed_domains, blocked_domains, max_results, and timeout. Official search backend parity is not implemented yet.", nil
		},
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
	if err := validateDomains("allowed_domains", webSearchAllowedDomains(input)); err != nil {
		return err
	}
	return validateDomains("blocked_domains", webSearchBlockedDomains(input))
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
	anchors := anchorRe.FindAllStringSubmatch(body, -1)
	results := make([]searchResult, 0, len(anchors))
	seen := map[string]struct{}{}
	for _, anchor := range anchors {
		attrs := anchor[1]
		label := strings.TrimSpace(stripHTML(anchor[2]))
		if label == "" {
			continue
		}
		href := hrefFromAttrs(attrs)
		resolved := resolveSearchURL(href, base)
		if resolved == "" || isSearchChromeURL(resolved) {
			continue
		}
		if _, ok := seen[resolved]; ok {
			continue
		}
		seen[resolved] = struct{}{}
		results = append(results, searchResult{Title: label, URL: resolved})
	}
	return results
}

var (
	anchorRe = regexp.MustCompile(`(?is)<a\b([^>]*)>(.*?)</a>`)
	hrefRe   = regexp.MustCompile(`(?is)\bhref\s*=\s*(?:"([^"]*)"|'([^']*)'|([^\s>]+))`)
	tagRe    = regexp.MustCompile(`(?is)<[^>]+>`)
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
	if parsed.Hostname() == "duckduckgo.com" && strings.HasPrefix(parsed.Path, "/l/") {
		if target := parsed.Query().Get("uddg"); target != "" {
			if unescaped, err := url.QueryUnescape(target); err == nil {
				parsed, err = url.Parse(unescaped)
				if err != nil {
					return ""
				}
			}
		}
	}
	if parsed.Scheme != "http" && parsed.Scheme != "https" {
		return ""
	}
	return parsed.String()
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
	if strings.Contains(host, "duckduckgo.com") && !strings.HasPrefix(parsed.Path, "/l/") {
		return true
	}
	return false
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
	var obj map[string]json.RawMessage
	if err := json.Unmarshal(raw, &obj); err != nil {
		return webSearchInput{}, err
	}
	for key := range obj {
		switch key {
		case "query", "allowed_domains", "allowedDomains", "blocked_domains", "blockedDomains", "max_results", "maxResults", "timeout":
		default:
			return webSearchInput{}, fmt.Errorf("input.%s is not allowed", key)
		}
	}
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
	if len(input.AllowedDomains) > 0 {
		return normalizeDomains(input.AllowedDomains)
	}
	return normalizeDomains(input.AllowedDomainsAlt)
}

func webSearchBlockedDomains(input webSearchInput) []string {
	if len(input.BlockedDomains) > 0 {
		return normalizeDomains(input.BlockedDomains)
	}
	return normalizeDomains(input.BlockedDomainsAlt)
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
	}
	return nil
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
