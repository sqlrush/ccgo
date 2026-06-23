package webtools

import (
	"context"
	"encoding/json"
	"strings"
	"testing"

	"ccgo/internal/tool"
)

type fakeServerSearch struct {
	gotReq ServerSearchRequest
	resp   ServerSearchResponse
}

func (f *fakeServerSearch) Search(_ context.Context, req ServerSearchRequest) (ServerSearchResponse, error) {
	f.gotReq = req
	return f.resp, nil
}

func TestWebSearchUsesServerToolWhenConfigured(t *testing.T) {
	srv := &fakeServerSearch{resp: ServerSearchResponse{
		Results: []searchResult{{Title: "Go 1.26", URL: "https://go.dev/blog", Snippet: "release"}},
	}}
	toolImpl := NewWebSearchTool()
	raw, _ := json.Marshal(map[string]any{"query": "go 1.26 release", "allowed_domains": []string{"go.dev"}})
	ctx := tool.Context{
		Context:  context.Background(),
		Metadata: map[string]any{MetadataServerSearchClientKey: srv},
	}
	res, err := toolImpl.Call(ctx, raw, tool.NopProgressSink())
	if err != nil {
		t.Fatalf("Call err: %v", err)
	}
	if srv.gotReq.Query != "go 1.26 release" {
		t.Fatalf("server search query = %q", srv.gotReq.Query)
	}
	if srv.gotReq.MaxUses != serverSearchMaxUses {
		t.Fatalf("max_uses = %d want %d", srv.gotReq.MaxUses, serverSearchMaxUses)
	}
	if len(srv.gotReq.AllowedDomains) != 1 || srv.gotReq.AllowedDomains[0] != "go.dev" {
		t.Fatalf("allowed_domains = %v", srv.gotReq.AllowedDomains)
	}
	content, _ := res.Content.(string)
	if !strings.Contains(content, "Go 1.26") || !strings.Contains(content, "https://go.dev/blog") {
		t.Fatalf("result missing server search hit: %q", content)
	}
}

// TestWebSearchSurfacesInterleavedText verifies that ServerSearchResponse.Text
// (model-generated text emitted alongside search blocks) is included in the
// formatted output, not silently discarded.
func TestWebSearchSurfacesInterleavedText(t *testing.T) {
	srv := &fakeServerSearch{resp: ServerSearchResponse{
		Results: []searchResult{{Title: "Go 1.26", URL: "https://go.dev/blog", Snippet: "release"}},
		Text:    "Here is some interleaved summary text from the model.",
	}}
	toolImpl := NewWebSearchTool()
	raw, _ := json.Marshal(map[string]any{"query": "go 1.26 release"})
	ctx := tool.Context{
		Context:  context.Background(),
		Metadata: map[string]any{MetadataServerSearchClientKey: srv},
	}
	res, err := toolImpl.Call(ctx, raw, tool.NopProgressSink())
	if err != nil {
		t.Fatalf("Call err: %v", err)
	}
	content, _ := res.Content.(string)
	if !strings.Contains(content, "interleaved summary text") {
		t.Fatalf("resp.Text not surfaced in output; got: %q", content)
	}
}

// TestWebSearchFallsBackWhenNoServerClient verifies serverSearchClient returns
// nil when the injection key is absent, ensuring the scrape path is taken by
// callWebSearch (no network needed — we only check the accessor).
func TestWebSearchFallsBackWhenNoServerClient(t *testing.T) {
	if client := serverSearchClient(map[string]any{"other.key": "value"}); client != nil {
		t.Fatalf("expected nil client when key absent, got %T", client)
	}
	if client := serverSearchClient(nil); client != nil {
		t.Fatalf("expected nil client for nil metadata, got %T", client)
	}
}
