package session

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"net/url"
	"path/filepath"
	"strings"
	"testing"

	"ccgo/internal/auth"
	"ccgo/internal/contracts"
)

type testRemoteTokenProvider struct {
	current   string
	refreshed string
	refreshes int
}

func (p *testRemoteTokenProvider) CurrentAccessToken(context.Context) (string, error) {
	return p.current, nil
}

func (p *testRemoteTokenProvider) RefreshAccessToken(context.Context) (string, error) {
	p.refreshes++
	return p.refreshed, nil
}

func TestRemoteHistoryAuthContextAndQueries(t *testing.T) {
	ctx := NewRemoteHistoryAuthContext("session/1", "token", "org", auth.OAuthConfig{BaseAPIURL: "https://example.test/"})
	if ctx.BaseURL != "https://example.test/v1/sessions/session%2F1/events" {
		t.Fatalf("base URL = %q", ctx.BaseURL)
	}
	if got := ctx.Headers.Get("Authorization"); got != "Bearer token" {
		t.Fatalf("Authorization = %q", got)
	}
	if got := ctx.Headers.Get("anthropic-beta"); got != RemoteHistoryBeta {
		t.Fatalf("anthropic-beta = %q", got)
	}
	if got := ctx.Headers.Get("x-organization-uuid"); got != "org" {
		t.Fatalf("x-organization-uuid = %q", got)
	}

	latest := LatestEventsQuery(0)
	if latest.Get("limit") != "100" || latest.Get("anchor_to_latest") != "true" {
		t.Fatalf("latest query = %s", latest.Encode())
	}
	older := OlderEventsQuery("evt_1", 25)
	if older.Get("limit") != "25" || older.Get("before_id") != "evt_1" {
		t.Fatalf("older query = %s", older.Encode())
	}
}

func TestFetchLatestAndOlderEvents(t *testing.T) {
	var seen []url.Values
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") != "Bearer token" {
			t.Fatalf("missing Authorization header")
		}
		if r.Header.Get("anthropic-beta") != RemoteHistoryBeta {
			t.Fatalf("missing beta header")
		}
		seen = append(seen, r.URL.Query())
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"data":[{"type":"status","session_id":"s","status":"ok"}],"has_more":true,"first_id":"evt_1","last_id":"evt_2"}`))
	}))
	defer server.Close()

	authCtx := NewRemoteHistoryAuthContext("s", "token", "org", auth.OAuthConfig{BaseAPIURL: server.URL})
	page, err := FetchLatestEvents(context.Background(), server.Client(), authCtx, 5)
	if err != nil {
		t.Fatal(err)
	}
	if page == nil || len(page.Events) != 1 || page.FirstID != "evt_1" || !page.HasMore {
		t.Fatalf("latest page = %#v", page)
	}
	_, err = FetchOlderEvents(context.Background(), server.Client(), authCtx, "evt_1", 7)
	if err != nil {
		t.Fatal(err)
	}
	if len(seen) != 2 {
		t.Fatalf("requests = %d", len(seen))
	}
	if seen[0].Get("limit") != "5" || seen[0].Get("anchor_to_latest") != "true" {
		t.Fatalf("latest query = %s", seen[0].Encode())
	}
	if seen[1].Get("limit") != "7" || seen[1].Get("before_id") != "evt_1" {
		t.Fatalf("older query = %s", seen[1].Encode())
	}
}

func TestFetchRemoteHistoryNonOKReturnsNil(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusBadGateway)
	}))
	defer server.Close()

	authCtx := NewRemoteHistoryAuthContext("s", "", "", auth.OAuthConfig{BaseAPIURL: server.URL})
	page, err := FetchLatestEvents(context.Background(), server.Client(), authCtx, 0)
	if err != nil {
		t.Fatal(err)
	}
	if page != nil {
		t.Fatalf("page = %#v", page)
	}
}

func TestFetchRemoteHistoryAcceptsEmptyTerminalPages(t *testing.T) {
	for _, tc := range []struct {
		name  string
		serve func(http.ResponseWriter)
	}{
		{name: "no-content", serve: func(w http.ResponseWriter) {
			w.WriteHeader(http.StatusNoContent)
		}},
		{name: "empty-ok", serve: func(w http.ResponseWriter) {
			w.Header().Set("Content-Type", "application/json")
		}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				tc.serve(w)
			}))
			defer server.Close()

			authCtx := NewRemoteHistoryAuthContext("s", "", "", auth.OAuthConfig{BaseAPIURL: server.URL})
			events, err := FetchRemoteHistory(context.Background(), server.Client(), authCtx, RemoteHistoryFetchOptions{Limit: 1})
			if err != nil {
				t.Fatal(err)
			}
			if !events.Complete || events.Pages != 1 || len(events.Events) != 0 || events.NextBeforeID != "" {
				t.Fatalf("events = %#v", events)
			}
		})
	}
}

func TestFetchRemoteHistoryRefreshesTokenOnUnauthorized(t *testing.T) {
	var tokens []string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		tokens = append(tokens, r.Header.Get("Authorization"))
		if len(tokens) == 1 {
			w.WriteHeader(http.StatusUnauthorized)
			return
		}
		if r.Header.Get("Authorization") != "Bearer fresh" {
			t.Fatalf("Authorization after refresh = %q", r.Header.Get("Authorization"))
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"data":[{"type":"status","session_id":"s","status":"ok"}],"has_more":false,"first_id":"evt_2"}`))
	}))
	defer server.Close()

	provider := &testRemoteTokenProvider{current: "stale", refreshed: "fresh"}
	authCtx := NewRemoteHistoryAuthContext("s", "", "", auth.OAuthConfig{BaseAPIURL: server.URL})
	page, err := FetchLatestEventsWithTokenRefresh(context.Background(), server.Client(), authCtx, provider, 0)
	if err != nil {
		t.Fatal(err)
	}
	if page == nil || page.FirstID != "evt_2" || len(page.Events) != 1 {
		t.Fatalf("page = %#v", page)
	}
	if provider.refreshes != 1 || strings.Join(tokens, ",") != "Bearer stale,Bearer fresh" {
		t.Fatalf("refreshes=%d tokens=%#v", provider.refreshes, tokens)
	}
}

func TestFetchRemoteHistoryPagesUntilComplete(t *testing.T) {
	var seen []url.Values
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		seen = append(seen, r.URL.Query())
		w.Header().Set("Content-Type", "application/json")
		switch r.URL.Query().Get("before_id") {
		case "":
			_, _ = w.Write([]byte(`{"data":[{"type":"status","session_id":"s","status":"latest"}],"has_more":true,"first_id":"evt_2"}`))
		case "evt_2":
			_, _ = w.Write([]byte(`{"data":[{"type":"status","session_id":"s","status":"older"}],"has_more":false,"first_id":"evt_1"}`))
		default:
			t.Fatalf("unexpected before_id = %q", r.URL.Query().Get("before_id"))
		}
	}))
	defer server.Close()

	authCtx := NewRemoteHistoryAuthContext("s", "token", "", auth.OAuthConfig{BaseAPIURL: server.URL})
	events, err := FetchRemoteHistory(context.Background(), server.Client(), authCtx, RemoteHistoryFetchOptions{Limit: 2})
	if err != nil {
		t.Fatal(err)
	}
	if !events.Complete || events.Pages != 2 || events.NextBeforeID != "" {
		t.Fatalf("events = %#v", events)
	}
	if len(events.Events) != 2 || events.Events[0].Status != "latest" || events.Events[1].Status != "older" {
		t.Fatalf("event order = %#v", events.Events)
	}
	if len(seen) != 2 || seen[0].Get("anchor_to_latest") != "true" || seen[1].Get("before_id") != "evt_2" {
		t.Fatalf("queries = %#v", seen)
	}
}

func TestFetchRemoteHistoryAcceptsCamelCasePageFields(t *testing.T) {
	var seen []url.Values
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		seen = append(seen, r.URL.Query())
		w.Header().Set("Content-Type", "application/json")
		switch r.URL.Query().Get("before_id") {
		case "":
			_, _ = w.Write([]byte(`{"events":[{"type":"status","sessionId":"s","status":"latest"}],"hasMore":true,"lastId":"evt_2"}`))
		case "evt_2":
			_, _ = w.Write([]byte(`{"events":[{"type":"status","sessionId":"s","status":"older"}],"hasMore":false,"nextBeforeId":"evt_1"}`))
		default:
			t.Fatalf("unexpected before_id = %q", r.URL.Query().Get("before_id"))
		}
	}))
	defer server.Close()

	authCtx := NewRemoteHistoryAuthContext("s", "token", "", auth.OAuthConfig{BaseAPIURL: server.URL})
	events, err := FetchRemoteHistory(context.Background(), server.Client(), authCtx, RemoteHistoryFetchOptions{Limit: 2})
	if err != nil {
		t.Fatal(err)
	}
	if !events.Complete || events.Pages != 2 || events.NextBeforeID != "" {
		t.Fatalf("events = %#v", events)
	}
	if len(events.Events) != 2 || events.Events[0].Status != "latest" || events.Events[1].SessionIDCamel != "s" {
		t.Fatalf("event aliases = %#v", events.Events)
	}
	if len(seen) != 2 || seen[1].Get("before_id") != "evt_2" {
		t.Fatalf("queries = %#v", seen)
	}
}

func TestFetchRemoteHistoryAcceptsCursorPageFields(t *testing.T) {
	var seen []url.Values
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		seen = append(seen, r.URL.Query())
		w.Header().Set("Content-Type", "application/json")
		switch r.URL.Query().Get("before_id") {
		case "":
			_, _ = w.Write([]byte(`{"data":[{"type":"status","session_id":"s","status":"latest"}],"has_next":true,"next_cursor":"evt_cursor"}`))
		case "evt_cursor":
			_, _ = w.Write([]byte(`{"data":[{"type":"status","session_id":"s","status":"older"}],"hasNext":false,"cursor":"ignored_when_complete"}`))
		default:
			t.Fatalf("unexpected before_id = %q", r.URL.Query().Get("before_id"))
		}
	}))
	defer server.Close()

	authCtx := NewRemoteHistoryAuthContext("s", "token", "", auth.OAuthConfig{BaseAPIURL: server.URL})
	events, err := FetchRemoteHistory(context.Background(), server.Client(), authCtx, RemoteHistoryFetchOptions{Limit: 2})
	if err != nil {
		t.Fatal(err)
	}
	if !events.Complete || events.Pages != 2 || events.NextBeforeID != "" {
		t.Fatalf("events = %#v", events)
	}
	if len(events.Events) != 2 || events.Events[0].Status != "latest" || events.Events[1].Status != "older" {
		t.Fatalf("event order = %#v", events.Events)
	}
	if len(seen) != 2 || seen[1].Get("before_id") != "evt_cursor" {
		t.Fatalf("queries = %#v", seen)
	}
}

func TestFetchRemoteHistoryAcceptsNumericCursorFields(t *testing.T) {
	var seen []url.Values
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		seen = append(seen, r.URL.Query())
		w.Header().Set("Content-Type", "application/json")
		switch r.URL.Query().Get("before_id") {
		case "":
			_, _ = w.Write([]byte(`{"data":[{"type":"status","session_id":"s","status":"latest"}],"has_next":true,"next_cursor":42}`))
		case "42":
			_, _ = w.Write([]byte(`{"data":[{"type":"status","session_id":"s","status":"older"}],"hasNext":false}`))
		default:
			t.Fatalf("unexpected before_id = %q", r.URL.Query().Get("before_id"))
		}
	}))
	defer server.Close()

	authCtx := NewRemoteHistoryAuthContext("s", "token", "", auth.OAuthConfig{BaseAPIURL: server.URL})
	events, err := FetchRemoteHistory(context.Background(), server.Client(), authCtx, RemoteHistoryFetchOptions{Limit: 2})
	if err != nil {
		t.Fatal(err)
	}
	if !events.Complete || events.Pages != 2 || events.NextBeforeID != "" {
		t.Fatalf("events = %#v", events)
	}
	if len(events.Events) != 2 || events.Events[0].Status != "latest" || events.Events[1].Status != "older" {
		t.Fatalf("event order = %#v", events.Events)
	}
	if len(seen) != 2 || seen[1].Get("before_id") != "42" {
		t.Fatalf("queries = %#v", seen)
	}
}

func TestFetchRemoteHistoryAcceptsPaginationTokenAliases(t *testing.T) {
	var seen []url.Values
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		seen = append(seen, r.URL.Query())
		w.Header().Set("Content-Type", "application/json")
		switch r.URL.Query().Get("before_id") {
		case "":
			_, _ = w.Write([]byte(`{"data":[{"type":"status","session_id":"s","status":"latest"}],"hasNext":true,"nextPageToken":"evt_token"}`))
		case "evt_token":
			_, _ = w.Write([]byte(`{"data":[{"type":"status","session_id":"s","status":"older"}],"links":{"older":"/v1/sessions/s/events?continuationToken=evt_continuation"}}`))
		case "evt_continuation":
			_, _ = w.Write([]byte(`{"data":[{"type":"status","session_id":"s","status":"oldest"}],"has_more":false,"pageToken":"ignored_when_complete"}`))
		default:
			t.Fatalf("unexpected before_id = %q", r.URL.Query().Get("before_id"))
		}
	}))
	defer server.Close()

	authCtx := NewRemoteHistoryAuthContext("s", "token", "", auth.OAuthConfig{BaseAPIURL: server.URL})
	events, err := FetchRemoteHistory(context.Background(), server.Client(), authCtx, RemoteHistoryFetchOptions{Limit: 3})
	if err != nil {
		t.Fatal(err)
	}
	if !events.Complete || events.Pages != 3 || len(events.Events) != 3 || events.NextBeforeID != "" {
		t.Fatalf("events = %#v", events)
	}
	if events.Events[0].Status != "latest" || events.Events[1].Status != "older" || events.Events[2].Status != "oldest" {
		t.Fatalf("events = %#v", events.Events)
	}
	if len(seen) != 3 || seen[1].Get("before_id") != "evt_token" || seen[2].Get("before_id") != "evt_continuation" {
		t.Fatalf("queries = %#v", seen)
	}
}

func TestFetchRemoteHistoryAcceptsPreviousPaginationTokenAliases(t *testing.T) {
	var seen []url.Values
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		seen = append(seen, r.URL.Query())
		w.Header().Set("Content-Type", "application/json")
		switch r.URL.Query().Get("before_id") {
		case "":
			_, _ = w.Write([]byte(`{"data":[{"type":"status","session_id":"s","status":"latest"}],"hasPreviousPage":true,"previousPageToken":"evt_previous"}`))
		case "evt_previous":
			_, _ = w.Write([]byte(`{"data":[{"type":"status","session_id":"s","status":"older"}],"links":{"previous":"/v1/sessions/s/events?prevPageToken=evt_prev"}}`))
		case "evt_prev":
			_, _ = w.Write([]byte(`{"data":[{"type":"status","session_id":"s","status":"oldest"}],"hasOlder":false,"olderPageToken":"ignored_when_complete"}`))
		default:
			t.Fatalf("unexpected before_id = %q", r.URL.Query().Get("before_id"))
		}
	}))
	defer server.Close()

	authCtx := NewRemoteHistoryAuthContext("s", "token", "", auth.OAuthConfig{BaseAPIURL: server.URL})
	events, err := FetchRemoteHistory(context.Background(), server.Client(), authCtx, RemoteHistoryFetchOptions{Limit: 3})
	if err != nil {
		t.Fatal(err)
	}
	if !events.Complete || events.Pages != 3 || len(events.Events) != 3 || events.NextBeforeID != "" {
		t.Fatalf("events = %#v", events)
	}
	if events.Events[0].Status != "latest" || events.Events[1].Status != "older" || events.Events[2].Status != "oldest" {
		t.Fatalf("events = %#v", events.Events)
	}
	if len(seen) != 3 || seen[1].Get("before_id") != "evt_previous" || seen[2].Get("before_id") != "evt_prev" {
		t.Fatalf("queries = %#v", seen)
	}
}

func TestFetchRemoteHistoryAcceptsTruncatedPaginationAliases(t *testing.T) {
	var seen []url.Values
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		seen = append(seen, r.URL.Query())
		w.Header().Set("Content-Type", "application/json")
		switch r.URL.Query().Get("before_id") {
		case "":
			_, _ = w.Write([]byte(`{"data":[{"type":"status","session_id":"s","status":"latest"}],"hasMoreResults":true,"nextKey":"evt_key"}`))
		case "evt_key":
			_, _ = w.Write([]byte(`{"data":[{"type":"status","session_id":"s","status":"older"}],"isTruncated":"true","lastEvaluatedKey":"evt_last"}`))
		case "evt_last":
			_, _ = w.Write([]byte(`{"data":[{"type":"status","session_id":"s","status":"oldest"}],"hasMoreItems":false}`))
		default:
			t.Fatalf("unexpected before_id = %q", r.URL.Query().Get("before_id"))
		}
	}))
	defer server.Close()

	authCtx := NewRemoteHistoryAuthContext("s", "token", "", auth.OAuthConfig{BaseAPIURL: server.URL})
	events, err := FetchRemoteHistory(context.Background(), server.Client(), authCtx, RemoteHistoryFetchOptions{Limit: 3})
	if err != nil {
		t.Fatal(err)
	}
	if !events.Complete || events.Pages != 3 || len(events.Events) != 3 || events.NextBeforeID != "" {
		t.Fatalf("events = %#v", events)
	}
	if events.Events[0].Status != "latest" || events.Events[1].Status != "older" || events.Events[2].Status != "oldest" {
		t.Fatalf("events = %#v", events.Events)
	}
	if len(seen) != 3 || seen[1].Get("before_id") != "evt_key" || seen[2].Get("before_id") != "evt_last" {
		t.Fatalf("queries = %#v", seen)
	}
}

func TestFetchRemoteHistoryAcceptsKeysetCursorLinkQueries(t *testing.T) {
	var seen []url.Values
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		seen = append(seen, r.URL.Query())
		w.Header().Set("Content-Type", "application/json")
		switch r.URL.Query().Get("before_id") {
		case "":
			_, _ = w.Write([]byte(`{"data":[{"type":"status","session_id":"s","status":"latest"}],"links":{"older":"/v1/sessions/s/events?lastEvaluatedKey=evt_link"}}`))
		case "evt_link":
			_, _ = w.Write([]byte(`{"data":[{"type":"status","session_id":"s","status":"older"}],"links":{"next":"/v1/sessions/s/events?nextKey=evt_next_key"}}`))
		case "evt_next_key":
			_, _ = w.Write([]byte(`{"data":[{"type":"status","session_id":"s","status":"oldest"}],"hasMoreResults":false}`))
		default:
			t.Fatalf("unexpected before_id = %q", r.URL.Query().Get("before_id"))
		}
	}))
	defer server.Close()

	authCtx := NewRemoteHistoryAuthContext("s", "token", "", auth.OAuthConfig{BaseAPIURL: server.URL})
	events, err := FetchRemoteHistory(context.Background(), server.Client(), authCtx, RemoteHistoryFetchOptions{Limit: 3})
	if err != nil {
		t.Fatal(err)
	}
	if !events.Complete || events.Pages != 3 || len(events.Events) != 3 || events.NextBeforeID != "" {
		t.Fatalf("events = %#v", events)
	}
	if events.Events[0].Status != "latest" || events.Events[1].Status != "older" || events.Events[2].Status != "oldest" {
		t.Fatalf("events = %#v", events.Events)
	}
	if len(seen) != 3 || seen[1].Get("before_id") != "evt_link" || seen[2].Get("before_id") != "evt_next_key" {
		t.Fatalf("queries = %#v", seen)
	}
}

func TestFetchRemoteHistoryAcceptsWrappedDataPageFields(t *testing.T) {
	var seen []url.Values
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		seen = append(seen, r.URL.Query())
		w.Header().Set("Content-Type", "application/json")
		switch r.URL.Query().Get("before_id") {
		case "":
			_, _ = w.Write([]byte(`{"data":{"events":[{"type":"status","session_id":"s","status":"latest"}],"has_more":1,"next_before_id":"evt_wrapped"}}`))
		case "evt_wrapped":
			_, _ = w.Write([]byte(`{"data":{"items":[{"type":"status","session_id":"s","status":"older"}]},"pagination":{"hasNext":"0","cursor":"ignored_when_complete"}}`))
		default:
			t.Fatalf("unexpected before_id = %q", r.URL.Query().Get("before_id"))
		}
	}))
	defer server.Close()

	authCtx := NewRemoteHistoryAuthContext("s", "token", "", auth.OAuthConfig{BaseAPIURL: server.URL})
	events, err := FetchRemoteHistory(context.Background(), server.Client(), authCtx, RemoteHistoryFetchOptions{Limit: 2})
	if err != nil {
		t.Fatal(err)
	}
	if !events.Complete || events.Pages != 2 || events.NextBeforeID != "" {
		t.Fatalf("events = %#v", events)
	}
	if len(events.Events) != 2 || events.Events[0].Status != "latest" || events.Events[1].Status != "older" {
		t.Fatalf("event order = %#v", events.Events)
	}
	if len(seen) != 2 || seen[1].Get("before_id") != "evt_wrapped" {
		t.Fatalf("queries = %#v", seen)
	}
}

func TestFetchRemoteHistoryAcceptsGraphQLSessionWrappers(t *testing.T) {
	var seen []url.Values
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		seen = append(seen, r.URL.Query())
		w.Header().Set("Content-Type", "application/json")
		switch r.URL.Query().Get("before_id") {
		case "":
			_, _ = w.Write([]byte(`{"data":{"session":{"events":{"edges":[{"cursor":"evt_graph","node":{"type":"status","session_id":"s","status":"latest"}}],"pageInfo":{"hasPreviousPage":true,"startCursor":"evt_graph"}}}}}`))
		case "evt_graph":
			_, _ = w.Write([]byte(`{"data":{"projectSession":{"eventConnection":{"nodes":[{"type":"status","session_id":"s","status":"older"}],"pageInfo":{"hasPreviousPage":false}}}}}`))
		default:
			t.Fatalf("unexpected before_id = %q", r.URL.Query().Get("before_id"))
		}
	}))
	defer server.Close()

	authCtx := NewRemoteHistoryAuthContext("s", "token", "", auth.OAuthConfig{BaseAPIURL: server.URL})
	events, err := FetchRemoteHistory(context.Background(), server.Client(), authCtx, RemoteHistoryFetchOptions{Limit: 2})
	if err != nil {
		t.Fatal(err)
	}
	if !events.Complete || events.Pages != 2 || events.NextBeforeID != "" {
		t.Fatalf("events = %#v", events)
	}
	if len(events.Events) != 2 || events.Events[0].Status != "latest" || events.Events[0].ID != "evt_graph" || events.Events[1].Status != "older" {
		t.Fatalf("event order = %#v", events.Events)
	}
	if len(seen) != 2 || seen[1].Get("before_id") != "evt_graph" {
		t.Fatalf("queries = %#v", seen)
	}
}

func TestFetchRemoteHistoryAcceptsGraphQLViewerAndNodeWrappers(t *testing.T) {
	var seen []url.Values
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		seen = append(seen, r.URL.Query())
		w.Header().Set("Content-Type", "application/json")
		switch r.URL.Query().Get("before_id") {
		case "":
			_, _ = w.Write([]byte(`{"data":{"viewer":{"session":{"events":{"edges":[{"cursor":"evt_viewer","node":{"type":"status","session_id":"s","status":"latest"}}],"pageInfo":{"hasPreviousPage":true,"startCursor":"evt_viewer"}}}}}}`))
		case "evt_viewer":
			_, _ = w.Write([]byte(`{"data":{"node":{"eventConnection":{"nodes":[{"type":"status","event_id":"evt_node","session_id":"s","status":"older"}],"pageInfo":{"hasPreviousPage":false}}}}}`))
		default:
			t.Fatalf("unexpected before_id = %q", r.URL.Query().Get("before_id"))
		}
	}))
	defer server.Close()

	authCtx := NewRemoteHistoryAuthContext("s", "token", "", auth.OAuthConfig{BaseAPIURL: server.URL})
	events, err := FetchRemoteHistory(context.Background(), server.Client(), authCtx, RemoteHistoryFetchOptions{Limit: 2})
	if err != nil {
		t.Fatal(err)
	}
	if !events.Complete || events.Pages != 2 || events.NextBeforeID != "" {
		t.Fatalf("events = %#v", events)
	}
	if len(events.Events) != 2 || events.Events[0].ID != "evt_viewer" || events.Events[0].Status != "latest" || events.Events[1].ID != "evt_node" || events.Events[1].Status != "older" {
		t.Fatalf("event order = %#v", events.Events)
	}
	if len(seen) != 2 || seen[1].Get("before_id") != "evt_viewer" {
		t.Fatalf("queries = %#v", seen)
	}
}

func TestFetchRemoteHistoryAcceptsProviderResponseWrappers(t *testing.T) {
	providerWrapper := func(t *testing.T, field string, page map[string]any) []byte {
		t.Helper()
		pageData, err := json.Marshal(page)
		if err != nil {
			t.Fatal(err)
		}
		var wrapper map[string]any
		switch field {
		case "choices":
			wrapper = map[string]any{
				"choices": []any{map[string]any{
					"message": map[string]any{"content": "```json\n" + string(pageData) + "\n```"},
				}},
			}
		case "candidates":
			wrapper = map[string]any{
				"candidates": []any{map[string]any{
					"content": map[string]any{
						"parts": []any{map[string]any{"text": string(pageData)}},
					},
				}},
			}
		default:
			t.Fatalf("unknown provider field %q", field)
		}
		data, err := json.Marshal(wrapper)
		if err != nil {
			t.Fatal(err)
		}
		return data
	}

	var seen []url.Values
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		seen = append(seen, r.URL.Query())
		w.Header().Set("Content-Type", "application/json")
		switch r.URL.Query().Get("before_id") {
		case "":
			_, _ = w.Write(providerWrapper(t, "choices", map[string]any{
				"events":  []any{map[string]any{"type": "status", "event_id": "evt_provider", "session_id": "s", "status": "latest"}},
				"hasMore": true,
			}))
		case "evt_provider":
			_, _ = w.Write(providerWrapper(t, "candidates", map[string]any{
				"items": []any{map[string]any{"type": "status", "eventId": "evt_candidate", "sessionId": "s", "status": "older"}},
			}))
		default:
			t.Fatalf("unexpected before_id = %q", r.URL.Query().Get("before_id"))
		}
	}))
	defer server.Close()

	authCtx := NewRemoteHistoryAuthContext("s", "token", "", auth.OAuthConfig{BaseAPIURL: server.URL})
	events, err := FetchRemoteHistory(context.Background(), server.Client(), authCtx, RemoteHistoryFetchOptions{Limit: 2})
	if err != nil {
		t.Fatal(err)
	}
	if !events.Complete || events.Pages != 2 || events.NextBeforeID != "" {
		t.Fatalf("events = %#v", events)
	}
	if len(events.Events) != 2 || events.Events[0].ID != "evt_provider" || events.Events[0].Status != "latest" || events.Events[1].ID != "evt_candidate" || events.Events[1].Status != "older" {
		t.Fatalf("event order = %#v", events.Events)
	}
	if len(seen) != 2 || seen[1].Get("before_id") != "evt_provider" {
		t.Fatalf("queries = %#v", seen)
	}
}

func TestFetchRemoteHistoryAcceptsLinkURLCursors(t *testing.T) {
	var seen []url.Values
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		seen = append(seen, r.URL.Query())
		w.Header().Set("Content-Type", "application/json")
		switch r.URL.Query().Get("before_id") {
		case "":
			_, _ = w.Write([]byte(`{"data":[{"type":"status","session_id":"s","status":"latest"}],"links":{"previous":{"href":"/v1/sessions/s/events?before_id=evt_link"}}}`))
		case "evt_link":
			_, _ = w.Write([]byte(`{"data":[{"type":"status","session_id":"s","status":"older"}],"links":{"older":"/v1/sessions/s/events?cursor=evt_old"}}`))
		case "evt_old":
			_, _ = w.Write([]byte(`{"data":[{"type":"status","session_id":"s","status":"oldest"}],"links":{"next":null}}`))
		default:
			t.Fatalf("unexpected before_id = %q", r.URL.Query().Get("before_id"))
		}
	}))
	defer server.Close()

	authCtx := NewRemoteHistoryAuthContext("s", "token", "", auth.OAuthConfig{BaseAPIURL: server.URL})
	events, err := FetchRemoteHistory(context.Background(), server.Client(), authCtx, RemoteHistoryFetchOptions{Limit: 3})
	if err != nil {
		t.Fatal(err)
	}
	if !events.Complete || events.Pages != 3 || len(events.Events) != 3 || events.NextBeforeID != "" {
		t.Fatalf("events = %#v", events)
	}
	if events.Events[0].Status != "latest" || events.Events[1].Status != "older" || events.Events[2].Status != "oldest" {
		t.Fatalf("events = %#v", events.Events)
	}
	if len(seen) != 3 || seen[1].Get("before_id") != "evt_link" || seen[2].Get("before_id") != "evt_old" {
		t.Fatalf("queries = %#v", seen)
	}
}

func TestFetchRemoteHistoryAcceptsLinkObjectCursorFields(t *testing.T) {
	var seen []url.Values
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		seen = append(seen, r.URL.Query())
		w.Header().Set("Content-Type", "application/json")
		switch r.URL.Query().Get("before_id") {
		case "":
			_, _ = w.Write([]byte(`{"data":[{"type":"status","session_id":"s","status":"latest"}],"links":{"older":{"lastEvaluatedKey":"evt_object"}}}`))
		case "evt_object":
			_, _ = w.Write([]byte(`{"data":[{"type":"status","session_id":"s","status":"older"}],"links":[{"rel":"previous","cursor":"evt_array"}]}`))
		case "evt_array":
			_, _ = w.Write([]byte(`{"data":[{"type":"status","session_id":"s","status":"oldest"}],"links":{"next":null}}`))
		default:
			t.Fatalf("unexpected before_id = %q", r.URL.Query().Get("before_id"))
		}
	}))
	defer server.Close()

	authCtx := NewRemoteHistoryAuthContext("s", "token", "", auth.OAuthConfig{BaseAPIURL: server.URL})
	events, err := FetchRemoteHistory(context.Background(), server.Client(), authCtx, RemoteHistoryFetchOptions{Limit: 3})
	if err != nil {
		t.Fatal(err)
	}
	if !events.Complete || events.Pages != 3 || len(events.Events) != 3 || events.NextBeforeID != "" {
		t.Fatalf("events = %#v", events)
	}
	if events.Events[0].Status != "latest" || events.Events[1].Status != "older" || events.Events[2].Status != "oldest" {
		t.Fatalf("events = %#v", events.Events)
	}
	if len(seen) != 3 || seen[1].Get("before_id") != "evt_object" || seen[2].Get("before_id") != "evt_array" {
		t.Fatalf("queries = %#v", seen)
	}
}

func TestFetchRemoteHistoryAcceptsAdjacentBeforeCursorAliases(t *testing.T) {
	var seen []url.Values
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		seen = append(seen, r.URL.Query())
		w.Header().Set("Content-Type", "application/json")
		switch r.URL.Query().Get("before_id") {
		case "":
			_, _ = w.Write([]byte(`{"data":[{"type":"status","session_id":"s","status":"latest"}],"hasMore":true,"before":"evt_before"}`))
		case "evt_before":
			_, _ = w.Write([]byte(`{"data":[{"type":"status","session_id":"s","status":"older"}],"_links":{"older":{"olderThan":"evt_older_than"}}}`))
		case "evt_older_than":
			_, _ = w.Write([]byte(`{"data":[{"type":"status","session_id":"s","status":"older-than"}],"links":{"previous":"/v1/sessions/s/events?ending_before=evt_ending"}}`))
		case "evt_ending":
			_, _ = w.Write([]byte(`{"data":[{"type":"status","session_id":"s","status":"oldest"}]}`))
		default:
			t.Fatalf("unexpected before_id = %q", r.URL.Query().Get("before_id"))
		}
	}))
	defer server.Close()

	authCtx := NewRemoteHistoryAuthContext("s", "token", "", auth.OAuthConfig{BaseAPIURL: server.URL})
	events, err := FetchRemoteHistory(context.Background(), server.Client(), authCtx, RemoteHistoryFetchOptions{Limit: 4})
	if err != nil {
		t.Fatal(err)
	}
	if !events.Complete || events.Pages != 4 || len(events.Events) != 4 || events.NextBeforeID != "" {
		t.Fatalf("events = %#v", events)
	}
	for index, want := range []string{"latest", "older", "older-than", "oldest"} {
		if events.Events[index].Status != want {
			t.Fatalf("events = %#v", events.Events)
		}
	}
	if len(seen) != 4 ||
		seen[1].Get("before_id") != "evt_before" ||
		seen[2].Get("before_id") != "evt_older_than" ||
		seen[3].Get("before_id") != "evt_ending" {
		t.Fatalf("queries = %#v", seen)
	}
}

func TestFetchRemoteHistoryAcceptsODataNextLinkCursors(t *testing.T) {
	var seen []url.Values
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		seen = append(seen, r.URL.Query())
		w.Header().Set("Content-Type", "application/json")
		switch r.URL.Query().Get("before_id") {
		case "":
			_, _ = w.Write([]byte(`{"value":[{"type":"status","event_id":"evt_latest","session_id":"s","status":"latest"}],"@odata.nextLink":"/v1/sessions/s/events?$skiptoken=evt_odata"}`))
		case "evt_odata":
			_, _ = w.Write([]byte(`{"value":[{"type":"status","event_id":"evt_older","session_id":"s","status":"older"}],"odata.nextLink":"/v1/sessions/s/events?skipToken=evt_oldest"}`))
		case "evt_oldest":
			_, _ = w.Write([]byte(`{"value":[{"type":"status","event_id":"evt_oldest","session_id":"s","status":"oldest"}]}`))
		default:
			t.Fatalf("unexpected before_id = %q", r.URL.Query().Get("before_id"))
		}
	}))
	defer server.Close()

	authCtx := NewRemoteHistoryAuthContext("s", "token", "", auth.OAuthConfig{BaseAPIURL: server.URL})
	events, err := FetchRemoteHistory(context.Background(), server.Client(), authCtx, RemoteHistoryFetchOptions{Limit: 3})
	if err != nil {
		t.Fatal(err)
	}
	if !events.Complete || events.Pages != 3 || len(events.Events) != 3 || events.NextBeforeID != "" {
		t.Fatalf("events = %#v", events)
	}
	if events.Events[0].Status != "latest" || events.Events[1].Status != "older" || events.Events[2].Status != "oldest" {
		t.Fatalf("events = %#v", events.Events)
	}
	if len(seen) != 3 || seen[1].Get("before_id") != "evt_odata" || seen[2].Get("before_id") != "evt_oldest" {
		t.Fatalf("queries = %#v", seen)
	}
}

func TestFetchRemoteHistoryAcceptsLinkArrayCursors(t *testing.T) {
	var seen []url.Values
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		seen = append(seen, r.URL.Query())
		w.Header().Set("Content-Type", "application/json")
		switch r.URL.Query().Get("before_id") {
		case "":
			_, _ = w.Write([]byte(`{"data":[{"type":"status","session_id":"s","status":"latest"}],"links":[{"rel":"self","href":"/v1/sessions/s/events?cursor=self"},{"rel":"previous","href":"/v1/sessions/s/events?before_id=evt_array"}]}`))
		case "evt_array":
			_, _ = w.Write([]byte(`{"data":[{"type":"status","session_id":"s","status":"older"}],"links":[{"rel":["older"],"url":"/v1/sessions/s/events?cursor=evt_array_old"}]}`))
		case "evt_array_old":
			_, _ = w.Write([]byte(`{"data":[{"type":"status","session_id":"s","status":"oldest"}],"links":[]}`))
		default:
			t.Fatalf("unexpected before_id = %q", r.URL.Query().Get("before_id"))
		}
	}))
	defer server.Close()

	authCtx := NewRemoteHistoryAuthContext("s", "token", "", auth.OAuthConfig{BaseAPIURL: server.URL})
	events, err := FetchRemoteHistory(context.Background(), server.Client(), authCtx, RemoteHistoryFetchOptions{Limit: 3})
	if err != nil {
		t.Fatal(err)
	}
	if !events.Complete || events.Pages != 3 || len(events.Events) != 3 || events.NextBeforeID != "" {
		t.Fatalf("events = %#v", events)
	}
	if events.Events[0].Status != "latest" || events.Events[1].Status != "older" || events.Events[2].Status != "oldest" {
		t.Fatalf("events = %#v", events.Events)
	}
	if len(seen) != 3 || seen[1].Get("before_id") != "evt_array" || seen[2].Get("before_id") != "evt_array_old" {
		t.Fatalf("queries = %#v", seen)
	}
}

func TestFetchRemoteHistoryAcceptsHALLinkCursors(t *testing.T) {
	var seen []url.Values
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		seen = append(seen, r.URL.Query())
		w.Header().Set("Content-Type", "application/json")
		switch r.URL.Query().Get("before_id") {
		case "":
			_, _ = w.Write([]byte(`{"data":[{"type":"status","session_id":"s","status":"latest"}],"_links":{"self":{"href":"/v1/sessions/s/events?cursor=self"},"prev":{"href":"/v1/sessions/s/events?before_id=evt_hal"}}}`))
		case "evt_hal":
			_, _ = w.Write([]byte(`{"data":[{"type":"status","session_id":"s","status":"older"}],"_links":{"older":{"href":"/v1/sessions/s/events?pageToken=evt_hal_old"}}}`))
		case "evt_hal_old":
			_, _ = w.Write([]byte(`{"data":[{"type":"status","session_id":"s","status":"oldest"}],"_links":{}}`))
		default:
			t.Fatalf("unexpected before_id = %q", r.URL.Query().Get("before_id"))
		}
	}))
	defer server.Close()

	authCtx := NewRemoteHistoryAuthContext("s", "token", "", auth.OAuthConfig{BaseAPIURL: server.URL})
	events, err := FetchRemoteHistory(context.Background(), server.Client(), authCtx, RemoteHistoryFetchOptions{Limit: 3})
	if err != nil {
		t.Fatal(err)
	}
	if !events.Complete || events.Pages != 3 || len(events.Events) != 3 || events.NextBeforeID != "" {
		t.Fatalf("events = %#v", events)
	}
	if events.Events[0].Status != "latest" || events.Events[1].Status != "older" || events.Events[2].Status != "oldest" {
		t.Fatalf("events = %#v", events.Events)
	}
	if len(seen) != 3 || seen[1].Get("before_id") != "evt_hal" || seen[2].Get("before_id") != "evt_hal_old" {
		t.Fatalf("queries = %#v", seen)
	}
}

func TestFetchRemoteHistoryAcceptsHALEmbeddedEvents(t *testing.T) {
	var seen []url.Values
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		seen = append(seen, r.URL.Query())
		w.Header().Set("Content-Type", "application/json")
		switch r.URL.Query().Get("before_id") {
		case "":
			_, _ = w.Write([]byte(`{"_embedded":{"events":[{"type":"status","session_id":"s","status":"latest"}]},"_links":{"prev":{"href":"/v1/sessions/s/events?before_id=evt_embed"}}}`))
		case "evt_embed":
			_, _ = w.Write([]byte(`{"embedded":{"items":[{"type":"status","session_id":"s","status":"older"}]},"_links":{}}`))
		default:
			t.Fatalf("unexpected before_id = %q", r.URL.Query().Get("before_id"))
		}
	}))
	defer server.Close()

	authCtx := NewRemoteHistoryAuthContext("s", "token", "", auth.OAuthConfig{BaseAPIURL: server.URL})
	events, err := FetchRemoteHistory(context.Background(), server.Client(), authCtx, RemoteHistoryFetchOptions{Limit: 2})
	if err != nil {
		t.Fatal(err)
	}
	if !events.Complete || events.Pages != 2 || len(events.Events) != 2 || events.NextBeforeID != "" {
		t.Fatalf("events = %#v", events)
	}
	if events.Events[0].Status != "latest" || events.Events[1].Status != "older" {
		t.Fatalf("events = %#v", events.Events)
	}
	if len(seen) != 2 || seen[1].Get("before_id") != "evt_embed" {
		t.Fatalf("queries = %#v", seen)
	}
}

func TestFetchRemoteHistoryAcceptsLinkHeaderCursors(t *testing.T) {
	var seen []url.Values
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		seen = append(seen, r.URL.Query())
		w.Header().Set("Content-Type", "application/json")
		switch r.URL.Query().Get("before_id") {
		case "":
			w.Header().Set("Link", `</v1/sessions/s/events?cursor=self>; rel="self", </v1/sessions/s/events?before_id=evt_header>; rel="prev"`)
			_, _ = w.Write([]byte(`{"data":[{"type":"status","session_id":"s","status":"latest"}]}`))
		case "evt_header":
			w.Header().Set("Link", `</v1/sessions/s/events?cursor=evt_older>; rel="older"`)
			_, _ = w.Write([]byte(`{"data":[{"type":"status","session_id":"s","status":"older"}]}`))
		case "evt_older":
			_, _ = w.Write([]byte(`{"data":[{"type":"status","session_id":"s","status":"oldest"}]}`))
		default:
			t.Fatalf("unexpected before_id = %q", r.URL.Query().Get("before_id"))
		}
	}))
	defer server.Close()

	authCtx := NewRemoteHistoryAuthContext("s", "token", "", auth.OAuthConfig{BaseAPIURL: server.URL})
	events, err := FetchRemoteHistory(context.Background(), server.Client(), authCtx, RemoteHistoryFetchOptions{Limit: 3})
	if err != nil {
		t.Fatal(err)
	}
	if !events.Complete || events.Pages != 3 || len(events.Events) != 3 || events.NextBeforeID != "" {
		t.Fatalf("events = %#v", events)
	}
	if events.Events[0].Status != "latest" || events.Events[1].Status != "older" || events.Events[2].Status != "oldest" {
		t.Fatalf("events = %#v", events.Events)
	}
	if len(seen) != 3 || seen[1].Get("before_id") != "evt_header" || seen[2].Get("before_id") != "evt_older" {
		t.Fatalf("queries = %#v", seen)
	}
}

func TestFetchRemoteHistoryAcceptsBareEventArray(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`[{"type":"status","session_id":"s","status":"bare"}]`))
	}))
	defer server.Close()

	authCtx := NewRemoteHistoryAuthContext("s", "token", "", auth.OAuthConfig{BaseAPIURL: server.URL})
	page, err := FetchLatestEvents(context.Background(), server.Client(), authCtx, 3)
	if err != nil {
		t.Fatal(err)
	}
	if page == nil || page.HasMore || page.FirstID != "" || len(page.Events) != 1 || page.Events[0].Status != "bare" {
		t.Fatalf("page = %#v", page)
	}
}

func TestFetchRemoteHistoryAcceptsEventListAliases(t *testing.T) {
	var seen []url.Values
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		seen = append(seen, r.URL.Query())
		w.Header().Set("Content-Type", "application/json")
		switch r.URL.Query().Get("before_id") {
		case "":
			_, _ = w.Write([]byte(`{"items":[{"type":"status","session_id":"s","status":"latest"}],"has_more":true,"first_id":"evt_2"}`))
		case "evt_2":
			_, _ = w.Write([]byte(`{"results":[{"type":"status","session_id":"s","status":"older"}],"has_more":false}`))
		default:
			t.Fatalf("unexpected before_id = %q", r.URL.Query().Get("before_id"))
		}
	}))
	defer server.Close()

	authCtx := NewRemoteHistoryAuthContext("s", "token", "", auth.OAuthConfig{BaseAPIURL: server.URL})
	events, err := FetchRemoteHistory(context.Background(), server.Client(), authCtx, RemoteHistoryFetchOptions{Limit: 2})
	if err != nil {
		t.Fatal(err)
	}
	if !events.Complete || events.Pages != 2 || len(events.Events) != 2 {
		t.Fatalf("events = %#v", events)
	}
	if events.Events[0].Status != "latest" || events.Events[1].Status != "older" {
		t.Fatalf("event aliases = %#v", events.Events)
	}
	if len(seen) != 2 || seen[1].Get("before_id") != "evt_2" {
		t.Fatalf("queries = %#v", seen)
	}
}

func TestFetchRemoteHistoryAcceptsWrappedEventArrayItems(t *testing.T) {
	var seen []url.Values
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		seen = append(seen, r.URL.Query())
		w.Header().Set("Content-Type", "application/json")
		switch r.URL.Query().Get("before_id") {
		case "":
			_, _ = w.Write([]byte(`{"events":[{"cursor":"evt_wrapped","event":{"type":"status","session_id":"s","status":"latest"}},{"record":{"event_id":"evt_record","type":"status","session_id":"s","status":"record"}}],"has_more":true,"first_id":"evt_next"}`))
		case "evt_next":
			_, _ = w.Write([]byte(`{"items":[{"data":{"eventId":"evt_data","type":"status","sessionId":"s","status":"data"}},{"event_id":"evt_direct","type":"status","session_id":"s","status":"direct","record":{"role":"user","text":"message payload"}}],"has_more":false}`))
		default:
			t.Fatalf("unexpected before_id = %q", r.URL.Query().Get("before_id"))
		}
	}))
	defer server.Close()

	authCtx := NewRemoteHistoryAuthContext("s", "token", "", auth.OAuthConfig{BaseAPIURL: server.URL})
	events, err := FetchRemoteHistory(context.Background(), server.Client(), authCtx, RemoteHistoryFetchOptions{Limit: 4})
	if err != nil {
		t.Fatal(err)
	}
	if !events.Complete || events.Pages != 2 || len(events.Events) != 4 || events.NextBeforeID != "" {
		t.Fatalf("events = %#v", events)
	}
	if events.Events[0].ID != "evt_wrapped" || events.Events[0].Status != "latest" || events.Events[1].ID != "evt_record" || events.Events[1].Status != "record" || events.Events[2].ID != "evt_data" || events.Events[2].Status != "data" || events.Events[3].ID != "evt_direct" || events.Events[3].Status != "direct" {
		t.Fatalf("wrapped events = %#v", events.Events)
	}
	if len(seen) != 2 || seen[1].Get("before_id") != "evt_next" {
		t.Fatalf("queries = %#v", seen)
	}
}

func TestFetchRemoteHistoryAcceptsResourceAttributeEvents(t *testing.T) {
	var seen []url.Values
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		seen = append(seen, r.URL.Query())
		w.Header().Set("Content-Type", "application/json")
		switch r.URL.Query().Get("before_id") {
		case "":
			_, _ = w.Write([]byte(`{
				"data": [
					{"id":"evt_attr_1","type":"session-events","attributes":{"eventType":"status","sessionID":"s","status":"latest"}},
					{"id":"evt_attr_2","type":"session-events","attributes":{"event_type":"assistant","session_id":"s","parentMessageID":"evt_attr_1","createdAt":"2026-01-01T00:00:02Z","message":{"type":"assistant","content":[{"type":"text","text":"hello"}]}}}
				],
				"links": {"prev": "/v1/sessions/s/events?before_id=evt_attr_2"}
			}`))
		case "evt_attr_2":
			_, _ = w.Write([]byte(`{
				"data": [
					{"id":"evt_attr_3","type":"session-events","properties":{"role":"user","sessionId":"s","createdAt":"2026-01-01T00:00:03Z","message":{"type":"user","content":[{"type":"text","text":"older"}]}}}
				]
			}`))
		default:
			t.Fatalf("unexpected before_id = %q", r.URL.Query().Get("before_id"))
		}
	}))
	defer server.Close()

	authCtx := NewRemoteHistoryAuthContext("s", "token", "", auth.OAuthConfig{BaseAPIURL: server.URL})
	events, err := FetchRemoteHistory(context.Background(), server.Client(), authCtx, RemoteHistoryFetchOptions{Limit: 3})
	if err != nil {
		t.Fatal(err)
	}
	if !events.Complete || events.Pages != 2 || len(events.Events) != 3 || events.NextBeforeID != "" {
		t.Fatalf("events = %#v", events)
	}
	if events.Events[0].ID != "evt_attr_1" || events.Events[0].Status != "latest" || events.Events[0].SessionID != "s" {
		t.Fatalf("status resource event = %#v", events.Events[0])
	}
	if events.Events[1].ID != "evt_attr_2" || events.Events[1].Type != contracts.SDKEventAssistant || events.Events[1].Message == nil || len(events.Events[1].Message.Content) != 1 || events.Events[1].Message.Content[0].Text != "hello" {
		t.Fatalf("assistant resource event = %#v", events.Events[1])
	}
	if events.Events[2].ID != "evt_attr_3" || events.Events[2].Type != contracts.SDKEventUser || events.Events[2].Message == nil || len(events.Events[2].Message.Content) != 1 || events.Events[2].Message.Content[0].Text != "older" {
		t.Fatalf("properties resource event = %#v", events.Events[2])
	}
	if len(seen) != 2 || seen[1].Get("before_id") != "evt_attr_2" {
		t.Fatalf("queries = %#v", seen)
	}
}

func TestFetchRemoteHistoryAcceptsResourcePageAttributes(t *testing.T) {
	var seen []url.Values
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		seen = append(seen, r.URL.Query())
		w.Header().Set("Content-Type", "application/json")
		switch r.URL.Query().Get("before_id") {
		case "":
			_, _ = w.Write([]byte(`{
				"data": {
					"id": "page_1",
					"type": "session-event-page",
					"attributes": {
						"list": [
							{"eventType":"status","eventId":"evt_page","sessionID":"s","status":"latest"}
						],
						"pageInfo": {"hasPreviousPage": true, "startCursor": "evt_page"}
					}
				}
			}`))
		case "evt_page":
			_, _ = w.Write([]byte(`{
				"data": {
					"id": "evt_single_resource",
					"type": "session-events",
					"attributes": {"eventType":"status","sessionID":"s","status":"single"}
				}
			}`))
		default:
			t.Fatalf("unexpected before_id = %q", r.URL.Query().Get("before_id"))
		}
	}))
	defer server.Close()

	authCtx := NewRemoteHistoryAuthContext("s", "token", "", auth.OAuthConfig{BaseAPIURL: server.URL})
	events, err := FetchRemoteHistory(context.Background(), server.Client(), authCtx, RemoteHistoryFetchOptions{Limit: 2})
	if err != nil {
		t.Fatal(err)
	}
	if !events.Complete || events.Pages != 2 || len(events.Events) != 2 || events.NextBeforeID != "" {
		t.Fatalf("events = %#v", events)
	}
	if events.Events[0].ID != "evt_page" || events.Events[0].Status != "latest" || events.Events[0].SessionID != "s" {
		t.Fatalf("page resource event = %#v", events.Events[0])
	}
	if events.Events[1].ID != "evt_single_resource" || events.Events[1].Status != "single" || events.Events[1].SessionID != "s" {
		t.Fatalf("single resource event = %#v", events.Events[1])
	}
	if len(seen) != 2 || seen[1].Get("before_id") != "evt_page" {
		t.Fatalf("queries = %#v", seen)
	}
}

func TestFetchRemoteHistoryAcceptsIncludedResourceEvents(t *testing.T) {
	var seen []url.Values
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		seen = append(seen, r.URL.Query())
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{
			"included": [
				{"id":"tool_1","type":"tool","attributes":{"name":"Bash"}},
				{"id":"evt_included_status","type":"session-events","attributes":{"eventType":"status","sessionID":"s","status":"included"}},
				{"resource":{"id":"evt_included_assistant","type":"session-events","properties":{"role":"assistant","sessionId":"s","createdAt":"2026-01-01T00:00:02Z","message":{"type":"assistant","content":[{"type":"text","text":"hello included"}]}}}}
			],
			"pageInfo": {"hasPreviousPage": false}
		}`))
	}))
	defer server.Close()

	authCtx := NewRemoteHistoryAuthContext("s", "token", "", auth.OAuthConfig{BaseAPIURL: server.URL})
	events, err := FetchRemoteHistory(context.Background(), server.Client(), authCtx, RemoteHistoryFetchOptions{Limit: 3})
	if err != nil {
		t.Fatal(err)
	}
	if !events.Complete || events.Pages != 1 || len(events.Events) != 2 || events.NextBeforeID != "" {
		t.Fatalf("events = %#v", events)
	}
	if events.Events[0].ID != "evt_included_status" || events.Events[0].Status != "included" || events.Events[0].SessionID != "s" {
		t.Fatalf("status included event = %#v", events.Events[0])
	}
	if events.Events[1].ID != "evt_included_assistant" || events.Events[1].Type != contracts.SDKEventAssistant || events.Events[1].Message == nil || len(events.Events[1].Message.Content) != 1 || events.Events[1].Message.Content[0].Text != "hello included" {
		t.Fatalf("assistant included event = %#v", events.Events[1])
	}
	if len(seen) != 1 {
		t.Fatalf("queries = %#v", seen)
	}
}

func TestFetchRemoteHistoryResolvesRelationshipIdentifiersFromIncluded(t *testing.T) {
	var seen []url.Values
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		seen = append(seen, r.URL.Query())
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{
			"data": {
				"type": "session",
				"id": "s",
				"relationships": {
					"events": {
						"data": [
							{"type":"session-events","id":"evt_rel_status"},
							{"type":"session-events","id":"evt_rel_assistant"}
						],
						"pageInfo": {"hasPreviousPage": false}
					}
				}
			},
			"included": [
				{"id":"tool_1","type":"tool","attributes":{"name":"Bash"}},
				{"id":"evt_rel_status","type":"session-events","attributes":{"eventType":"status","sessionID":"s","status":"linked"}},
				{"id":"evt_rel_assistant","type":"session-events","attributes":{"role":"assistant","sessionId":"s","createdAt":"2026-01-01T00:00:02Z","message":{"type":"assistant","content":[{"type":"text","text":"linked assistant"}]}}}
			]
		}`))
	}))
	defer server.Close()

	authCtx := NewRemoteHistoryAuthContext("s", "token", "", auth.OAuthConfig{BaseAPIURL: server.URL})
	events, err := FetchRemoteHistory(context.Background(), server.Client(), authCtx, RemoteHistoryFetchOptions{Limit: 3})
	if err != nil {
		t.Fatal(err)
	}
	if !events.Complete || events.Pages != 1 || len(events.Events) != 2 || events.NextBeforeID != "" {
		t.Fatalf("events = %#v", events)
	}
	if events.Events[0].ID != "evt_rel_status" || events.Events[0].Status != "linked" || events.Events[0].SessionID != "s" {
		t.Fatalf("linked status event = %#v", events.Events[0])
	}
	if events.Events[1].ID != "evt_rel_assistant" || events.Events[1].Type != contracts.SDKEventAssistant || events.Events[1].Message == nil || len(events.Events[1].Message.Content) != 1 || events.Events[1].Message.Content[0].Text != "linked assistant" {
		t.Fatalf("linked assistant event = %#v", events.Events[1])
	}
	if len(seen) != 1 {
		t.Fatalf("queries = %#v", seen)
	}
}

func TestFetchRemoteHistoryAcceptsKeyedEventMaps(t *testing.T) {
	var seen []url.Values
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		seen = append(seen, r.URL.Query())
		w.Header().Set("Content-Type", "application/json")
		switch r.URL.Query().Get("before_id") {
		case "":
			_, _ = w.Write([]byte(`{"events":{"evt_map_1":{"type":"status","session_id":"s","status":"latest"},"evt_map_2":{"record":{"type":"status","session_id":"s","status":"wrapped"}}},"has_more":true,"first_id":"evt_map_2"}`))
		case "evt_map_2":
			_, _ = w.Write([]byte(`{"items":{"evt_map_3":{"type":"status","session_id":"s","status":"older"}},"has_more":false}`))
		default:
			t.Fatalf("unexpected before_id = %q", r.URL.Query().Get("before_id"))
		}
	}))
	defer server.Close()

	authCtx := NewRemoteHistoryAuthContext("s", "token", "", auth.OAuthConfig{BaseAPIURL: server.URL})
	events, err := FetchRemoteHistory(context.Background(), server.Client(), authCtx, RemoteHistoryFetchOptions{Limit: 3})
	if err != nil {
		t.Fatal(err)
	}
	if !events.Complete || events.Pages != 2 || len(events.Events) != 3 || events.NextBeforeID != "" {
		t.Fatalf("events = %#v", events)
	}
	for index, want := range []struct {
		id     contracts.ID
		status string
	}{
		{id: "evt_map_1", status: "latest"},
		{id: "evt_map_2", status: "wrapped"},
		{id: "evt_map_3", status: "older"},
	} {
		if events.Events[index].ID != want.id || events.Events[index].Status != want.status {
			t.Fatalf("event %d = %#v, want %#v", index, events.Events[index], want)
		}
	}
	if len(seen) != 2 || seen[1].Get("before_id") != "evt_map_2" {
		t.Fatalf("queries = %#v", seen)
	}
}

func TestFetchRemoteHistoryAcceptsRecordAliasesAndEventIDCursor(t *testing.T) {
	var seen []url.Values
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		seen = append(seen, r.URL.Query())
		w.Header().Set("Content-Type", "application/json")
		switch r.URL.Query().Get("before_id") {
		case "":
			_, _ = w.Write([]byte(`{"records":[{"type":"status","event_id":"evt_2","sessionUuid":"s","status":"latest"}],"links":{"has_next":true}}`))
		case "evt_2":
			_, _ = w.Write([]byte(`{"entries":[{"type":"status","eventId":"evt_1","sessionUUID":"s","status":"older"}],"paging":{"hasNext":false}}`))
		default:
			t.Fatalf("unexpected before_id = %q", r.URL.Query().Get("before_id"))
		}
	}))
	defer server.Close()

	authCtx := NewRemoteHistoryAuthContext("s", "token", "", auth.OAuthConfig{BaseAPIURL: server.URL})
	events, err := FetchRemoteHistory(context.Background(), server.Client(), authCtx, RemoteHistoryFetchOptions{Limit: 2})
	if err != nil {
		t.Fatal(err)
	}
	if !events.Complete || events.Pages != 2 || len(events.Events) != 2 || events.NextBeforeID != "" {
		t.Fatalf("events = %#v", events)
	}
	if events.Events[0].ID != "evt_2" || events.Events[0].SessionID != "s" || events.Events[1].ID != "evt_1" || events.Events[1].Status != "older" {
		t.Fatalf("event aliases = %#v", events.Events)
	}
	if len(seen) != 2 || seen[1].Get("before_id") != "evt_2" {
		t.Fatalf("queries = %#v", seen)
	}
}

func TestFetchRemoteHistoryAcceptsConnectionWrappers(t *testing.T) {
	var seen []url.Values
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		seen = append(seen, r.URL.Query())
		w.Header().Set("Content-Type", "application/json")
		switch r.URL.Query().Get("before_id") {
		case "":
			_, _ = w.Write([]byte(`{"history":{"edges":[{"cursor":"edge_cursor","node":{"type":"status","eventId":"evt_edge","sessionUUID":"s","status":"latest"}}],"pageInfo":{"hasNextPage":"true","endCursor":"evt_edge"}}}`))
		case "evt_edge":
			_, _ = w.Write([]byte(`{"messages":{"nodes":[{"type":"status","event_id":"evt_old","session_uuid":"s","status":"older"}],"page_info":{"has_next_page":false,"start_cursor":"ignored_when_complete"}}}`))
		default:
			t.Fatalf("unexpected before_id = %q", r.URL.Query().Get("before_id"))
		}
	}))
	defer server.Close()

	authCtx := NewRemoteHistoryAuthContext("s", "token", "", auth.OAuthConfig{BaseAPIURL: server.URL})
	events, err := FetchRemoteHistory(context.Background(), server.Client(), authCtx, RemoteHistoryFetchOptions{Limit: 2})
	if err != nil {
		t.Fatal(err)
	}
	if !events.Complete || events.Pages != 2 || len(events.Events) != 2 || events.NextBeforeID != "" {
		t.Fatalf("events = %#v", events)
	}
	if events.Events[0].ID != "evt_edge" || events.Events[0].SessionID != "s" || events.Events[0].Status != "latest" {
		t.Fatalf("latest event = %#v", events.Events[0])
	}
	if events.Events[1].ID != "evt_old" || events.Events[1].SessionID != "s" || events.Events[1].Status != "older" {
		t.Fatalf("older event = %#v", events.Events[1])
	}
	if len(seen) != 2 || seen[1].Get("before_id") != "evt_edge" {
		t.Fatalf("queries = %#v", seen)
	}
}

func TestFetchRemoteHistoryAcceptsValueAndResourceAliases(t *testing.T) {
	var seen []url.Values
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		seen = append(seen, r.URL.Query())
		w.Header().Set("Content-Type", "application/json")
		switch r.URL.Query().Get("before_id") {
		case "":
			_, _ = w.Write([]byte(`{"value":[{"type":"status","eventId":"evt_value","session_id":"s","status":"latest"}],"hasNext":true,"nextPageToken":"evt_value"}`))
		case "evt_value":
			_, _ = w.Write([]byte(`{"history":{"edges":[{"cursor":"evt_resource","resource":{"type":"status","session_id":"s","status":"resource"}}],"pageInfo":{"hasNextPage":true}}}`))
		case "evt_resource":
			_, _ = w.Write([]byte(`{"resources":[{"type":"status","event_id":"evt_old","session_id":"s","status":"oldest"}],"has_more":false}`))
		default:
			t.Fatalf("unexpected before_id = %q", r.URL.Query().Get("before_id"))
		}
	}))
	defer server.Close()

	authCtx := NewRemoteHistoryAuthContext("s", "token", "", auth.OAuthConfig{BaseAPIURL: server.URL})
	events, err := FetchRemoteHistory(context.Background(), server.Client(), authCtx, RemoteHistoryFetchOptions{Limit: 3})
	if err != nil {
		t.Fatal(err)
	}
	if !events.Complete || events.Pages != 3 || len(events.Events) != 3 || events.NextBeforeID != "" {
		t.Fatalf("events = %#v", events)
	}
	if events.Events[0].ID != "evt_value" || events.Events[0].Status != "latest" || events.Events[1].ID != "evt_resource" || events.Events[1].Status != "resource" || events.Events[2].ID != "evt_old" || events.Events[2].Status != "oldest" {
		t.Fatalf("events = %#v", events.Events)
	}
	if len(seen) != 3 || seen[1].Get("before_id") != "evt_value" || seen[2].Get("before_id") != "evt_resource" {
		t.Fatalf("queries = %#v", seen)
	}
}

func TestFetchRemoteHistoryAcceptsGenericResponseWrappers(t *testing.T) {
	var seen []url.Values
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		seen = append(seen, r.URL.Query())
		w.Header().Set("Content-Type", "application/json")
		switch r.URL.Query().Get("before_id") {
		case "":
			_, _ = w.Write([]byte(`{"payload":{"events":[{"type":"status","event_id":"evt_payload","session_id":"s","status":"payload"}],"hasMore":true,"nextCursor":"evt_payload"}}`))
		case "evt_payload":
			_, _ = w.Write([]byte(`{"response":{"items":[{"type":"status","event_id":"evt_response","session_id":"s","status":"response"}],"paging":{"more":true,"olderCursor":"evt_response"}}}`))
		case "evt_response":
			_, _ = w.Write([]byte(`{"result":{"value":[{"type":"status","event_id":"evt_result","session_id":"s","status":"result"}],"@odata.nextLink":"/v1/sessions/s/events?skipToken=evt_body"}}`))
		case "evt_body":
			_, _ = w.Write([]byte(`{"body":{"resources":[{"type":"status","event_id":"evt_body","session_id":"s","status":"body"}],"has_more":false}}`))
		default:
			t.Fatalf("unexpected before_id = %q", r.URL.Query().Get("before_id"))
		}
	}))
	defer server.Close()

	authCtx := NewRemoteHistoryAuthContext("s", "token", "", auth.OAuthConfig{BaseAPIURL: server.URL})
	events, err := FetchRemoteHistory(context.Background(), server.Client(), authCtx, RemoteHistoryFetchOptions{Limit: 4})
	if err != nil {
		t.Fatal(err)
	}
	if !events.Complete || events.Pages != 4 || len(events.Events) != 4 || events.NextBeforeID != "" {
		t.Fatalf("events = %#v", events)
	}
	for index, want := range []string{"payload", "response", "result", "body"} {
		if events.Events[index].Status != want {
			t.Fatalf("event %d = %#v, want status %q", index, events.Events[index], want)
		}
	}
	if len(seen) != 4 || seen[1].Get("before_id") != "evt_payload" || seen[2].Get("before_id") != "evt_response" || seen[3].Get("before_id") != "evt_body" {
		t.Fatalf("queries = %#v", seen)
	}
}

func TestFetchRemoteHistoryAcceptsSingleObjectEventPages(t *testing.T) {
	var seen []url.Values
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		seen = append(seen, r.URL.Query())
		w.Header().Set("Content-Type", "application/json")
		switch r.URL.Query().Get("before_id") {
		case "":
			_, _ = w.Write([]byte(`{"data":{"eventType":"status","eventId":"evt_single","session_id":"s","status":"single"},"hasNext":true,"nextCursor":"evt_single"}`))
		case "evt_single":
			_, _ = w.Write([]byte(`{"result":{"type":"status","id":"evt_result","session_id":"s","status":"result"},"more":false}`))
		default:
			t.Fatalf("unexpected before_id = %q", r.URL.Query().Get("before_id"))
		}
	}))
	defer server.Close()

	authCtx := NewRemoteHistoryAuthContext("s", "token", "", auth.OAuthConfig{BaseAPIURL: server.URL})
	events, err := FetchRemoteHistory(context.Background(), server.Client(), authCtx, RemoteHistoryFetchOptions{Limit: 2})
	if err != nil {
		t.Fatal(err)
	}
	if !events.Complete || events.Pages != 2 || len(events.Events) != 2 || events.NextBeforeID != "" {
		t.Fatalf("events = %#v", events)
	}
	if events.Events[0].ID != "evt_single" || events.Events[0].Status != "single" || events.Events[1].ID != "evt_result" || events.Events[1].Status != "result" {
		t.Fatalf("events = %#v", events.Events)
	}
	if len(seen) != 2 || seen[1].Get("before_id") != "evt_single" {
		t.Fatalf("queries = %#v", seen)
	}
}

func TestFetchRemoteHistoryUsesEdgeCursorWhenNodeIDMissing(t *testing.T) {
	var seen []url.Values
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		seen = append(seen, r.URL.Query())
		w.Header().Set("Content-Type", "application/json")
		switch r.URL.Query().Get("before_id") {
		case "":
			_, _ = w.Write([]byte(`{"history":{"edges":[{"cursor":420,"node":{"type":"status","session_id":"s","status":"latest"}}],"pageInfo":{"hasNextPage":true}}}`))
		case "420":
			_, _ = w.Write([]byte(`{"history":{"edges":[{"cursor":421,"node":{"type":"status","session_id":"s","status":"older"}}],"pageInfo":{"hasNextPage":false}}}`))
		default:
			t.Fatalf("unexpected before_id = %q", r.URL.Query().Get("before_id"))
		}
	}))
	defer server.Close()

	authCtx := NewRemoteHistoryAuthContext("s", "token", "", auth.OAuthConfig{BaseAPIURL: server.URL})
	events, err := FetchRemoteHistory(context.Background(), server.Client(), authCtx, RemoteHistoryFetchOptions{Limit: 2})
	if err != nil {
		t.Fatal(err)
	}
	if !events.Complete || events.Pages != 2 || len(events.Events) != 2 || events.NextBeforeID != "" {
		t.Fatalf("events = %#v", events)
	}
	if events.Events[0].ID != "420" || events.Events[0].Status != "latest" || events.Events[1].ID != "421" || events.Events[1].Status != "older" {
		t.Fatalf("edge cursor events = %#v", events.Events)
	}
	if len(seen) != 2 || seen[1].Get("before_id") != "420" {
		t.Fatalf("queries = %#v", seen)
	}
}

func TestFetchRemoteHistoryAcceptsPreviousPageCursors(t *testing.T) {
	var seen []url.Values
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		seen = append(seen, r.URL.Query())
		w.Header().Set("Content-Type", "application/json")
		switch r.URL.Query().Get("before_id") {
		case "":
			_, _ = w.Write([]byte(`{"history":{"edges":[{"cursor":"edge_latest","node":{"type":"status","session_id":"s","status":"latest"}}],"pageInfo":{"hasPreviousPage":true,"startCursor":"edge_latest"}}}`))
		case "edge_latest":
			_, _ = w.Write([]byte(`{"history":{"edges":[{"cursor":"edge_older","node":{"type":"status","session_id":"s","status":"older"}}],"pageInfo":{"has_previous_page":true,"previousCursor":"edge_older"}}}`))
		case "edge_older":
			_, _ = w.Write([]byte(`{"history":{"edges":[{"cursor":"edge_oldest","node":{"type":"status","session_id":"s","status":"oldest"}}],"pageInfo":{"hasOlder":false,"olderCursor":"ignored_when_complete"}}}`))
		default:
			t.Fatalf("unexpected before_id = %q", r.URL.Query().Get("before_id"))
		}
	}))
	defer server.Close()

	authCtx := NewRemoteHistoryAuthContext("s", "token", "", auth.OAuthConfig{BaseAPIURL: server.URL})
	events, err := FetchRemoteHistory(context.Background(), server.Client(), authCtx, RemoteHistoryFetchOptions{Limit: 3})
	if err != nil {
		t.Fatal(err)
	}
	if !events.Complete || events.Pages != 3 || len(events.Events) != 3 || events.NextBeforeID != "" {
		t.Fatalf("events = %#v", events)
	}
	if events.Events[0].Status != "latest" || events.Events[1].Status != "older" || events.Events[2].Status != "oldest" {
		t.Fatalf("events = %#v", events.Events)
	}
	if len(seen) != 3 || seen[1].Get("before_id") != "edge_latest" || seen[2].Get("before_id") != "edge_older" {
		t.Fatalf("queries = %#v", seen)
	}
}

func TestFetchRemoteHistoryAcceptsConnectionAliasWrappers(t *testing.T) {
	var seen []url.Values
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		seen = append(seen, r.URL.Query())
		w.Header().Set("Content-Type", "application/json")
		switch r.URL.Query().Get("before_id") {
		case "":
			_, _ = w.Write([]byte(`{"data":{"sessionEventsConnection":{"edges":[{"cursor":"evt_conn","node":{"type":"status","session_id":"s","status":"latest"}}],"pageInfo":{"hasNextPage":true}}}}`))
		case "evt_conn":
			_, _ = w.Write([]byte(`{"connection":{"eventList":[{"type":"status","event_id":"evt_old","session_id":"s","status":"older"}],"page_info":{"has_next_page":false}}}`))
		default:
			t.Fatalf("unexpected before_id = %q", r.URL.Query().Get("before_id"))
		}
	}))
	defer server.Close()

	authCtx := NewRemoteHistoryAuthContext("s", "token", "", auth.OAuthConfig{BaseAPIURL: server.URL})
	events, err := FetchRemoteHistory(context.Background(), server.Client(), authCtx, RemoteHistoryFetchOptions{Limit: 2})
	if err != nil {
		t.Fatal(err)
	}
	if !events.Complete || events.Pages != 2 || len(events.Events) != 2 || events.NextBeforeID != "" {
		t.Fatalf("events = %#v", events)
	}
	if events.Events[0].ID != "evt_conn" || events.Events[0].Status != "latest" || events.Events[1].ID != "evt_old" || events.Events[1].Status != "older" {
		t.Fatalf("connection alias events = %#v", events.Events)
	}
	if len(seen) != 2 || seen[1].Get("before_id") != "evt_conn" {
		t.Fatalf("queries = %#v", seen)
	}
}

func TestFetchRemoteHistoryAcceptsRelationshipAndConnectionAliasWrappers(t *testing.T) {
	var seen []url.Values
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		seen = append(seen, r.URL.Query())
		w.Header().Set("Content-Type", "application/json")
		switch r.URL.Query().Get("before_id") {
		case "":
			_, _ = w.Write([]byte(`{
				"data": {
					"type": "session",
					"id": "s",
					"relationships": {
						"events": {
							"data": [
								{"type":"status","eventId":"evt_rel","sessionID":"s","status":"relationship"}
							],
							"pageInfo": {"hasPreviousPage": true, "startCursor": "evt_rel"}
						}
					}
				}
			}`))
		case "evt_rel":
			_, _ = w.Write([]byte(`{
				"data": {
					"resultsConnection": {
						"children": [
							{"type":"status","event_id":"evt_child","session_id":"s","status":"child"}
						],
						"pageInfo": {"hasPreviousPage": false}
					}
				}
			}`))
		default:
			t.Fatalf("unexpected before_id = %q", r.URL.Query().Get("before_id"))
		}
	}))
	defer server.Close()

	authCtx := NewRemoteHistoryAuthContext("s", "token", "", auth.OAuthConfig{BaseAPIURL: server.URL})
	events, err := FetchRemoteHistory(context.Background(), server.Client(), authCtx, RemoteHistoryFetchOptions{Limit: 2})
	if err != nil {
		t.Fatal(err)
	}
	if !events.Complete || events.Pages != 2 || len(events.Events) != 2 || events.NextBeforeID != "" {
		t.Fatalf("events = %#v", events)
	}
	if events.Events[0].ID != "evt_rel" || events.Events[0].Status != "relationship" || events.Events[1].ID != "evt_child" || events.Events[1].Status != "child" {
		t.Fatalf("relationship events = %#v", events.Events)
	}
	if len(seen) != 2 || seen[1].Get("before_id") != "evt_rel" {
		t.Fatalf("queries = %#v", seen)
	}
}

func TestFetchRemoteHistoryStopsAtMaxPages(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"data":[{"type":"status","session_id":"s","status":"page"}],"has_more":true,"first_id":"evt_next"}`))
	}))
	defer server.Close()

	authCtx := NewRemoteHistoryAuthContext("s", "token", "", auth.OAuthConfig{BaseAPIURL: server.URL})
	events, err := FetchRemoteHistory(context.Background(), server.Client(), authCtx, RemoteHistoryFetchOptions{Limit: 1, MaxPages: 1})
	if err != nil {
		t.Fatal(err)
	}
	if events.Complete || events.Pages != 1 || events.NextBeforeID != "evt_next" || len(events.Events) != 1 {
		t.Fatalf("events = %#v", events)
	}
}

func TestFetchRemoteHistoryResumesFromBeforeID(t *testing.T) {
	var seen url.Values
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		seen = r.URL.Query()
		if seen.Get("anchor_to_latest") != "" {
			t.Fatalf("resume query should not anchor to latest: %s", seen.Encode())
		}
		if seen.Get("before_id") != "evt_resume" {
			t.Fatalf("before_id = %q", seen.Get("before_id"))
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"data":[{"type":"status","session_id":"s","status":"older"}],"has_more":false,"first_id":"evt_old"}`))
	}))
	defer server.Close()

	authCtx := NewRemoteHistoryAuthContext("s", "token", "", auth.OAuthConfig{BaseAPIURL: server.URL})
	events, err := FetchRemoteHistory(context.Background(), server.Client(), authCtx, RemoteHistoryFetchOptions{Limit: 3, BeforeID: "evt_resume"})
	if err != nil {
		t.Fatal(err)
	}
	if !events.Complete || events.Pages != 1 || events.NextBeforeID != "" || len(events.Events) != 1 || events.Events[0].Status != "older" {
		t.Fatalf("events = %#v", events)
	}
	if seen.Get("limit") != "3" {
		t.Fatalf("limit = %q", seen.Get("limit"))
	}
}

func TestFetchRemoteHistoryRefreshesTokenAcrossPages(t *testing.T) {
	var tokens []string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		tokens = append(tokens, r.Header.Get("Authorization"))
		if r.URL.Query().Get("before_id") == "evt_2" && r.Header.Get("Authorization") == "Bearer stale" {
			w.WriteHeader(http.StatusUnauthorized)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		switch r.URL.Query().Get("before_id") {
		case "":
			_, _ = w.Write([]byte(`{"data":[{"type":"status","session_id":"s","status":"latest"}],"has_more":true,"first_id":"evt_2"}`))
		case "evt_2":
			_, _ = w.Write([]byte(`{"data":[{"type":"status","session_id":"s","status":"older"}],"has_more":false,"first_id":"evt_1"}`))
		default:
			t.Fatalf("unexpected before_id = %q", r.URL.Query().Get("before_id"))
		}
	}))
	defer server.Close()

	provider := &testRemoteTokenProvider{current: "stale", refreshed: "fresh"}
	authCtx := NewRemoteHistoryAuthContext("s", "", "", auth.OAuthConfig{BaseAPIURL: server.URL})
	events, err := FetchRemoteHistoryWithTokenRefresh(context.Background(), server.Client(), authCtx, provider, RemoteHistoryFetchOptions{Limit: 2})
	if err != nil {
		t.Fatal(err)
	}
	if !events.Complete || len(events.Events) != 2 || provider.refreshes != 1 {
		t.Fatalf("events = %#v refreshes=%d", events, provider.refreshes)
	}
	if got := strings.Join(tokens, ","); got != "Bearer stale,Bearer stale,Bearer fresh" {
		t.Fatalf("tokens = %s", got)
	}
}

func TestRemoteHistoryTranscriptMessagesSortsAndLinksMissingParents(t *testing.T) {
	events := []contracts.SDKEvent{
		{
			Type:      contracts.SDKEventAssistant,
			SessionID: "s1",
			Message: &contracts.Message{
				Type:      contracts.MessageAssistant,
				UUID:      "a1",
				Timestamp: "2026-01-01T00:00:02Z",
				Content:   []contracts.ContentBlock{contracts.NewTextBlock("hello")},
			},
		},
		{
			Type:      contracts.SDKEventUser,
			SessionID: "s1",
			Message: &contracts.Message{
				Type:      contracts.MessageUser,
				UUID:      "u1",
				Timestamp: "2026-01-01T00:00:01Z",
				Content:   []contracts.ContentBlock{contracts.NewTextBlock("hi")},
			},
		},
		{Type: contracts.SDKEventStatus, SessionID: "s1", Status: "ignored"},
	}

	messages := RemoteHistoryTranscriptMessages(events)
	if len(messages) != 2 || messages[0].UUID != "u1" || messages[1].UUID != "a1" {
		t.Fatalf("messages = %#v", messages)
	}
	if messages[1].ParentUUID == nil || *messages[1].ParentUUID != "u1" {
		t.Fatalf("assistant parent = %#v", messages[1].ParentUUID)
	}
	if messages[1].Message == events[0].Message {
		t.Fatal("message should be cloned before materializing")
	}
}

func TestRemoteHistoryTranscriptMessagesUsesEventFallbackFields(t *testing.T) {
	parent := contracts.ID("evt_u1")
	events := []contracts.SDKEvent{
		{
			Type:            contracts.SDKEventAssistant,
			ID:              "evt_a1",
			SessionIDCamel:  "s1",
			ParentUUIDCamel: &parent,
			Timestamp:       "2026-01-01T00:00:02Z",
			Message: &contracts.Message{
				Type:    contracts.MessageAssistant,
				Content: []contracts.ContentBlock{contracts.NewTextBlock("hello")},
			},
		},
		{
			Type:           contracts.SDKEventUser,
			UUID:           "evt_u1",
			SessionIDCamel: "s1",
			Timestamp:      "2026-01-01T00:00:01Z",
			Message: &contracts.Message{
				Type:    contracts.MessageUser,
				Content: []contracts.ContentBlock{contracts.NewTextBlock("hi")},
			},
		},
	}

	messages := RemoteHistoryTranscriptMessages(events)
	if len(messages) != 2 || messages[0].UUID != "evt_u1" || messages[1].UUID != "evt_a1" {
		t.Fatalf("messages = %#v", messages)
	}
	if messages[0].SessionID != "s1" || messages[0].Timestamp != "2026-01-01T00:00:01Z" {
		t.Fatalf("user fallback fields = %#v", messages[0])
	}
	if messages[1].ParentUUID == nil || *messages[1].ParentUUID != "evt_u1" {
		t.Fatalf("assistant parent = %#v", messages[1].ParentUUID)
	}
	if messages[1].Message == nil || messages[1].Message.ParentUUID == nil || *messages[1].Message.ParentUUID != "evt_u1" || messages[1].Message.SessionID != "s1" || messages[1].Message.Timestamp != "2026-01-01T00:00:02Z" {
		t.Fatalf("assistant message fallback fields = %#v", messages[1].Message)
	}
}

func TestRemoteHistoryTranscriptMessagesAcceptsSessionIDUpperAlias(t *testing.T) {
	var response sessionEventsResponse
	if err := json.Unmarshal([]byte(`{"events":[{"type":"assistant","id":"evt_a1","sessionID":"s_upper","timestamp":"2026-01-01T00:00:02Z","message":{"type":"assistant","content":[{"type":"text","text":"hello"}]}}]}`), &response); err != nil {
		t.Fatal(err)
	}
	messages := RemoteHistoryTranscriptMessages(response.Events)
	if len(messages) != 1 {
		t.Fatalf("messages = %#v", messages)
	}
	if messages[0].SessionID != "s_upper" || messages[0].Message == nil || messages[0].Message.SessionID != "s_upper" {
		t.Fatalf("sessionID alias materialization = %#v", messages[0])
	}
}

func TestRemoteHistoryTranscriptMessagesAcceptsParentIDAliases(t *testing.T) {
	var response sessionEventsResponse
	if err := json.Unmarshal([]byte(`{"events":[{"type":"user","id":"evt_u1","sessionId":"s1","timestamp":"2026-01-01T00:00:01Z","message":{"type":"user","content":[{"type":"text","text":"hi"}]}},{"type":"assistant","eventId":"evt_a1","sessionId":"s1","parentMessageID":"evt_u1","timestamp":"2026-01-01T00:00:02Z","message":{"type":"assistant","content":[{"type":"text","text":"hello"}]}}]}`), &response); err != nil {
		t.Fatal(err)
	}
	messages := RemoteHistoryTranscriptMessages(response.Events)
	if len(messages) != 2 || messages[1].UUID != "evt_a1" {
		t.Fatalf("messages = %#v", messages)
	}
	if messages[1].ParentUUID == nil || *messages[1].ParentUUID != "evt_u1" {
		t.Fatalf("parent alias = %#v", messages[1].ParentUUID)
	}
	if messages[1].Message == nil || messages[1].Message.ParentUUID == nil || *messages[1].Message.ParentUUID != "evt_u1" {
		t.Fatalf("message parent alias = %#v", messages[1].Message)
	}
}

func TestRemoteHistoryTranscriptMessagesAcceptsEventPayloadAliases(t *testing.T) {
	var response sessionEventsResponse
	if err := json.Unmarshal([]byte(`{"events":[
		{"eventType":"user","eventId":"evt_u1","sessionID":"s_payload","createdAt":"2026-01-01T00:00:01Z","payload":{"role":"user","text":"hi"}},
		{"event_type":"assistant","event_id":"evt_a1","session_id":"s_payload","parentMessageId":"evt_u1","created_at":"2026-01-01T00:00:02Z","body":{"role":"assistant","message":"hello"}}
	]}`), &response); err != nil {
		t.Fatal(err)
	}
	messages := RemoteHistoryTranscriptMessages(response.Events)
	if len(messages) != 2 || messages[0].UUID != "evt_u1" || messages[1].UUID != "evt_a1" {
		t.Fatalf("messages = %#v", messages)
	}
	if messages[0].Type != "user" || messages[0].SessionID != "s_payload" || messages[0].Timestamp != "2026-01-01T00:00:01Z" {
		t.Fatalf("user payload aliases = %#v", messages[0])
	}
	if messages[0].Message == nil || messages[0].Message.Type != contracts.MessageUser || messages[0].Message.SessionID != "s_payload" {
		t.Fatalf("user nested message = %#v", messages[0].Message)
	}
	if len(messages[0].Message.Content) != 1 || messages[0].Message.Content[0].Text != "hi" {
		t.Fatalf("user content = %#v", messages[0].Message.Content)
	}
	if messages[1].Type != "assistant" || messages[1].SessionID != "s_payload" || messages[1].Timestamp != "2026-01-01T00:00:02Z" {
		t.Fatalf("assistant payload aliases = %#v", messages[1])
	}
	if messages[1].ParentUUID == nil || *messages[1].ParentUUID != "evt_u1" {
		t.Fatalf("assistant parent alias = %#v", messages[1].ParentUUID)
	}
	if messages[1].Message == nil || messages[1].Message.Type != contracts.MessageAssistant || messages[1].Message.ParentUUID == nil || *messages[1].Message.ParentUUID != "evt_u1" {
		t.Fatalf("assistant nested message = %#v", messages[1].Message)
	}
	if len(messages[1].Message.Content) != 1 || messages[1].Message.Content[0].Text != "hello" {
		t.Fatalf("assistant content = %#v", messages[1].Message.Content)
	}
}

func TestRemoteHistoryTranscriptMessagesAcceptsNestedEventPayloadWrappers(t *testing.T) {
	var response sessionEventsResponse
	if err := json.Unmarshal([]byte(`{"events":[
		{"eventType":"user","eventId":"evt_u1","sessionID":"s_nested","createdAt":"2026-01-01T00:00:01Z","payload":{"record":{"role":"user","text":"hi"}}},
		{"eventType":"assistant","eventId":"evt_a1","sessionID":"s_nested","parentMessageID":"evt_u1","createdAt":"2026-01-01T00:00:02Z","data":{"entry":{"role":"assistant","message":"hello"}}}
	]}`), &response); err != nil {
		t.Fatal(err)
	}
	messages := RemoteHistoryTranscriptMessages(response.Events)
	if len(messages) != 2 || messages[0].UUID != "evt_u1" || messages[1].UUID != "evt_a1" {
		t.Fatalf("messages = %#v", messages)
	}
	if messages[0].Message == nil || len(messages[0].Message.Content) != 1 || messages[0].Message.Content[0].Text != "hi" {
		t.Fatalf("user nested payload = %#v", messages[0].Message)
	}
	if messages[1].Message == nil || len(messages[1].Message.Content) != 1 || messages[1].Message.Content[0].Text != "hello" {
		t.Fatalf("assistant nested payload = %#v", messages[1].Message)
	}
	if messages[1].ParentUUID == nil || *messages[1].ParentUUID != "evt_u1" || messages[1].Message.ParentUUID == nil || *messages[1].Message.ParentUUID != "evt_u1" {
		t.Fatalf("parent linkage = %#v message=%#v", messages[1].ParentUUID, messages[1].Message)
	}
}

func TestRemoteHistoryTranscriptMessagesAcceptsNumericIDAliases(t *testing.T) {
	var response sessionEventsResponse
	if err := json.Unmarshal([]byte(`{"events":[{"type":"user","id":101,"sessionId":7,"timestamp":"2026-01-01T00:00:01Z","message":{"type":"user","content":[{"type":"text","text":"hi"}]}},{"type":"assistant","eventId":102,"sessionId":7,"parentMessageID":101,"timestamp":"2026-01-01T00:00:02Z","message":{"type":"assistant","content":[{"type":"text","text":"hello"}]}}]}`), &response); err != nil {
		t.Fatal(err)
	}
	messages := RemoteHistoryTranscriptMessages(response.Events)
	if len(messages) != 2 || messages[0].UUID != "101" || messages[1].UUID != "102" {
		t.Fatalf("messages = %#v", messages)
	}
	if messages[0].SessionID != "7" || messages[1].SessionID != "7" {
		t.Fatalf("numeric session ids = %#v", messages)
	}
	if messages[1].ParentUUID == nil || *messages[1].ParentUUID != "101" {
		t.Fatalf("numeric parent alias = %#v", messages[1].ParentUUID)
	}
	if messages[1].Message == nil || messages[1].Message.ParentUUID == nil || *messages[1].Message.ParentUUID != "101" || messages[1].Message.SessionID != "7" {
		t.Fatalf("message numeric aliases = %#v", messages[1].Message)
	}
}

func TestAppendRemoteHistoryTranscriptDeduplicatesExistingMessages(t *testing.T) {
	path := writeTranscript(t, []string{
		`{"type":"user","uuid":"u1","sessionId":"s1","timestamp":"2026-01-01T00:00:01Z","message":{"type":"user","uuid":"u1","sessionId":"s1","content":[{"type":"text","text":"hi"}]}}`,
	})
	events := []contracts.SDKEvent{
		{
			Type:      contracts.SDKEventUser,
			SessionID: "s1",
			Message: &contracts.Message{
				Type:      contracts.MessageUser,
				UUID:      "u1",
				Timestamp: "2026-01-01T00:00:01Z",
				Content:   []contracts.ContentBlock{contracts.NewTextBlock("hi")},
			},
		},
		{
			Type:      contracts.SDKEventAssistant,
			SessionID: "s1",
			Message: &contracts.Message{
				Type:      contracts.MessageAssistant,
				UUID:      "a1",
				Timestamp: "2026-01-01T00:00:02Z",
				Content:   []contracts.ContentBlock{contracts.NewTextBlock("hello")},
			},
		},
	}

	result, err := AppendRemoteHistoryTranscript(path, events)
	if err != nil {
		t.Fatal(err)
	}
	if result.Considered != 2 || result.Appended != 1 || result.Duplicates != 1 || result.LastUUID != "a1" {
		t.Fatalf("result = %#v", result)
	}
	transcript, err := LoadTranscript(path)
	if err != nil {
		t.Fatal(err)
	}
	if len(transcript.Order) != 2 || transcript.Order[0] != "u1" || transcript.Order[1] != "a1" {
		t.Fatalf("order = %#v", transcript.Order)
	}
	if transcript.Messages["a1"].ParentUUID == nil || *transcript.Messages["a1"].ParentUUID != "u1" {
		t.Fatalf("a1 parent = %#v", transcript.Messages["a1"].ParentUUID)
	}
}

func TestAppendRemoteHistoryTranscriptLinksToExistingLeaf(t *testing.T) {
	path := writeTranscript(t, []string{
		`{"type":"user","uuid":"u1","sessionId":"s1","timestamp":"2026-01-01T00:00:01Z","message":{"type":"user","uuid":"u1","sessionId":"s1","content":[{"type":"text","text":"hi"}]}}`,
	})
	events := []contracts.SDKEvent{
		{
			Type:      contracts.SDKEventAssistant,
			SessionID: "s1",
			Message: &contracts.Message{
				Type:      contracts.MessageAssistant,
				UUID:      "a1",
				Timestamp: "2026-01-01T00:00:02Z",
				Content:   []contracts.ContentBlock{contracts.NewTextBlock("hello")},
			},
		},
	}
	result, err := AppendRemoteHistoryTranscript(path, events)
	if err != nil {
		t.Fatal(err)
	}
	if result.Appended != 1 || result.LastUUID != "a1" {
		t.Fatalf("result = %#v", result)
	}
	transcript, err := LoadTranscript(path)
	if err != nil {
		t.Fatal(err)
	}
	if transcript.Messages["a1"].ParentUUID == nil || *transcript.Messages["a1"].ParentUUID != "u1" {
		t.Fatalf("a1 parent = %#v", transcript.Messages["a1"].ParentUUID)
	}
}

func TestAppendRemoteHistoryTranscriptSkipsDuplicateParentLinking(t *testing.T) {
	path := writeTranscript(t, []string{
		`{"type":"user","uuid":"u1","sessionId":"s1","timestamp":"2026-01-01T00:00:01Z","message":{"type":"user","uuid":"u1","sessionId":"s1","content":[{"type":"text","text":"hi"}]}}`,
		`{"type":"assistant","uuid":"a1","parentUuid":"u1","sessionId":"s1","timestamp":"2026-01-01T00:00:02Z","message":{"type":"assistant","uuid":"a1","parentUuid":"u1","sessionId":"s1","content":[{"type":"text","text":"hello"}]}}`,
	})
	events := []contracts.SDKEvent{
		{
			Type:      contracts.SDKEventUser,
			SessionID: "s1",
			Message: &contracts.Message{
				Type:      contracts.MessageUser,
				UUID:      "u1",
				Timestamp: "2026-01-01T00:00:01Z",
				Content:   []contracts.ContentBlock{contracts.NewTextBlock("hi")},
			},
		},
		{
			Type:      contracts.SDKEventAssistant,
			SessionID: "s1",
			Message: &contracts.Message{
				Type:      contracts.MessageAssistant,
				UUID:      "a2",
				Timestamp: "2026-01-01T00:00:03Z",
				Content:   []contracts.ContentBlock{contracts.NewTextBlock("next")},
			},
		},
	}
	result, err := AppendRemoteHistoryTranscript(path, events)
	if err != nil {
		t.Fatal(err)
	}
	if result.Appended != 1 || result.Duplicates != 1 || result.LastUUID != "a2" {
		t.Fatalf("result = %#v", result)
	}
	transcript, err := LoadTranscript(path)
	if err != nil {
		t.Fatal(err)
	}
	if transcript.Messages["a2"].ParentUUID == nil || *transcript.Messages["a2"].ParentUUID != "a1" {
		t.Fatalf("a2 parent = %#v", transcript.Messages["a2"].ParentUUID)
	}
}

func TestRemoteHistoryTranscriptMessagesGeneratesStableUUID(t *testing.T) {
	events := []contracts.SDKEvent{
		{
			Type:      contracts.SDKEventAssistant,
			SessionID: "s1",
			Message: &contracts.Message{
				Type:      contracts.MessageAssistant,
				Timestamp: "2026-01-01T00:00:02Z",
				Content:   []contracts.ContentBlock{contracts.NewTextBlock("hello")},
			},
		},
	}
	first := RemoteHistoryTranscriptMessages(events)
	second := RemoteHistoryTranscriptMessages(events)
	if len(first) != 1 || len(second) != 1 || first[0].UUID == "" || first[0].UUID != second[0].UUID {
		t.Fatalf("uuids = %#v %#v", first, second)
	}
}

func TestSyncRemoteHistoryTranscriptFetchesAndAppends(t *testing.T) {
	var seen []url.Values
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		seen = append(seen, r.URL.Query())
		w.Header().Set("Content-Type", "application/json")
		switch r.URL.Query().Get("before_id") {
		case "":
			_, _ = w.Write([]byte(`{"data":[{"type":"assistant","session_id":"s1","message":{"type":"assistant","uuid":"a1","sessionId":"s1","timestamp":"2026-01-01T00:00:02Z","content":[{"type":"text","text":"hello"}]}}],"has_more":true,"first_id":"evt_2"}`))
		case "evt_2":
			_, _ = w.Write([]byte(`{"data":[{"type":"user","session_id":"s1","message":{"type":"user","uuid":"u1","sessionId":"s1","timestamp":"2026-01-01T00:00:01Z","content":[{"type":"text","text":"hi"}]}}],"has_more":false,"first_id":"evt_1"}`))
		default:
			t.Fatalf("unexpected before_id = %q", r.URL.Query().Get("before_id"))
		}
	}))
	defer server.Close()

	path := filepath.Join(t.TempDir(), "remote.jsonl")
	authCtx := NewRemoteHistoryAuthContext("s1", "token", "", auth.OAuthConfig{BaseAPIURL: server.URL})
	result, err := SyncRemoteHistoryTranscript(context.Background(), server.Client(), authCtx, nil, path, RemoteHistoryFetchOptions{Limit: 1})
	if err != nil {
		t.Fatal(err)
	}
	if !result.Complete || result.Pages != 2 || result.Appended != 2 || result.NextBeforeID != "" {
		t.Fatalf("result = %#v", result)
	}
	if len(seen) != 2 || seen[0].Get("anchor_to_latest") != "true" || seen[1].Get("before_id") != "evt_2" {
		t.Fatalf("queries = %#v", seen)
	}
	transcript, err := LoadTranscript(path)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Join(chainIDs(transcript.BuildConversationChain("a1")), ",") != "u1,a1" {
		t.Fatalf("chain = %#v", chainIDs(transcript.BuildConversationChain("a1")))
	}
}
