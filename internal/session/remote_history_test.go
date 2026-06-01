package session

import (
	"context"
	"net/http"
	"net/http/httptest"
	"net/url"
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
