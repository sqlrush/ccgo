package webtools

import (
	"context"
	"fmt"
	"strings"
)

// MetadataServerSearchClientKey injects the official web-search server-tool
// client. Absent → fall back to the HTML-scraping path (today's behaviour).
const MetadataServerSearchClientKey = "ccgo.tools.web.server_search"

// serverSearchMaxUses mirrors CC's hardcoded max_uses (WebSearchTool.ts:82).
const serverSearchMaxUses = 8

// ServerSearchClient runs the web_search_20250305 server tool and returns the
// parsed hits plus any interleaved model text.
type ServerSearchClient interface {
	Search(ctx context.Context, req ServerSearchRequest) (ServerSearchResponse, error)
}

// ServerSearchRequest carries the parameters forwarded to the server tool.
type ServerSearchRequest struct {
	Query          string
	AllowedDomains []string
	BlockedDomains []string
	MaxUses        int
}

// ServerSearchResponse holds the parsed results from the server tool response,
// mirroring the CC makeOutputFromSearchResponse result shape.
type ServerSearchResponse struct {
	Results []searchResult
	Text    string
}

// serverSearchClient extracts the injected ServerSearchClient from metadata,
// returning nil when absent so callWebSearch falls back to the scrape path.
func serverSearchClient(metadata map[string]any) ServerSearchClient {
	if metadata == nil {
		return nil
	}
	client, _ := metadata[MetadataServerSearchClientKey].(ServerSearchClient)
	return client
}

// runServerSearch delegates to the injected server-tool client, passes
// max_uses=8 (CC parity), and filters the returned results by domain rules.
func runServerSearch(ctx context.Context, client ServerSearchClient, input webSearchInput, limit int) (webSearchResult, error) {
	if ctx == nil {
		ctx = context.Background()
	}
	resp, err := client.Search(ctx, ServerSearchRequest{
		Query:          strings.TrimSpace(input.Query),
		AllowedDomains: webSearchAllowedDomains(input),
		BlockedDomains: webSearchBlockedDomains(input),
		MaxUses:        serverSearchMaxUses,
	})
	if err != nil {
		return webSearchResult{}, fmt.Errorf("server search failed: %w", err)
	}
	results := filterSearchResults(resp.Results, webSearchAllowedDomains(input), webSearchBlockedDomains(input), limit)
	return webSearchResult{Results: results, StatusCode: 200}, nil
}
