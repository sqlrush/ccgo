package session

import (
	"context"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"

	"ccgo/internal/auth"
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
