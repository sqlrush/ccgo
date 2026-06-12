package mcp

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"testing"
	"time"

	"ccgo/internal/contracts"
)

func TestHTTPTransportRoundTripPostsJSONRPC(t *testing.T) {
	var gotRequest RPCRequest
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			t.Fatalf("method = %s", r.Method)
		}
		if got := r.Header.Get("Authorization"); got != "Bearer token" {
			t.Fatalf("authorization = %q", got)
		}
		if got := r.Header.Get("Accept"); got != "application/json, text/event-stream" {
			t.Fatalf("accept = %q", got)
		}
		if err := json.NewDecoder(r.Body).Decode(&gotRequest); err != nil {
			t.Fatal(err)
		}
		w.Header().Set("content-type", "application/json")
		_, _ = w.Write([]byte(`{"jsonrpc":"2.0","id":"7","result":{"tools":[]}}`))
	}))
	defer server.Close()

	transport := NewHTTPTransport(server.URL, map[string]string{"Authorization": "Bearer token"}, server.Client())
	response, err := transport.RoundTrip(context.Background(), NewRPCRequest("7", "tools/list", nil))
	if err != nil {
		t.Fatal(err)
	}
	if gotRequest.JSONRPC != JSONRPCVersion || gotRequest.ID != "7" || gotRequest.Method != "tools/list" {
		t.Fatalf("request = %#v", gotRequest)
	}
	if response.ID != "7" || !strings.Contains(string(response.Result), `"tools":[]`) {
		t.Fatalf("response = %#v", response)
	}
}

func TestHTTPTransportUsesDynamicHeadersPerRequest(t *testing.T) {
	var calls int
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls++
		if got, want := r.Header.Get("Authorization"), "Bearer token-"+strconv.Itoa(calls); got != want {
			t.Fatalf("authorization = %q, want %q", got, want)
		}
		_, _ = w.Write([]byte(`{"jsonrpc":"2.0","id":"1","result":{"tools":[]}}`))
	}))
	defer server.Close()

	transport := NewHTTPTransport(server.URL, map[string]string{"Authorization": "Bearer stale"}, server.Client())
	transport.HeaderProvider = func(context.Context) (map[string]string, error) {
		return map[string]string{"Authorization": "Bearer token-" + strconv.Itoa(calls+1)}, nil
	}
	if _, err := transport.RoundTrip(context.Background(), NewRPCRequest("1", "tools/list", nil)); err != nil {
		t.Fatal(err)
	}
	if _, err := transport.RoundTrip(context.Background(), NewRPCRequest("1", "tools/list", nil)); err != nil {
		t.Fatal(err)
	}
}

func TestOpenServerClientSupportsHTTPTransport(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if got := r.Header.Get("Authorization"); got != "Bearer token" {
			t.Fatalf("authorization = %q", got)
		}
		if got := r.Header.Get("anthropic-beta"); got == "" {
			t.Fatal("missing anthropic-beta")
		}
		_, _ = w.Write([]byte(`{"jsonrpc":"2.0","id":"1","result":{"tools":[{"name":"ping","readOnly":true}]}}`))
	}))
	defer server.Close()

	handle, err := OpenServerClient(context.Background(), "remote", contracts.MCPServer{
		Type:      TransportHTTP,
		URL:       server.URL,
		AuthToken: "token",
		OAuth:     &contracts.MCPOAuthConfig{ClientID: "client"},
	})
	if err != nil {
		t.Fatal(err)
	}
	tools, err := handle.Client.ListTools(context.Background(), "remote")
	if err != nil {
		t.Fatal(err)
	}
	if len(tools) != 1 || tools[0].Name != "ping" || !tools[0].ReadOnly {
		t.Fatalf("tools = %#v", tools)
	}
	if handle.Close == nil {
		t.Fatalf("http close should be configured")
	}
	if err := handle.Close(); err != nil {
		t.Fatal(err)
	}
}

func TestBuildServerToolSetInitializesHTTPProtocolClient(t *testing.T) {
	var methods []string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var raw struct {
			ID     string          `json:"id"`
			Method string          `json:"method"`
			Params json.RawMessage `json:"params"`
		}
		if err := json.NewDecoder(r.Body).Decode(&raw); err != nil {
			t.Fatal(err)
		}
		methods = append(methods, raw.Method)
		switch raw.Method {
		case "initialize":
			if !strings.Contains(string(raw.Params), `"protocolVersion":"2025-06-18"`) || !strings.Contains(string(raw.Params), `"clientInfo"`) {
				t.Fatalf("initialize params = %s", raw.Params)
			}
			_, _ = w.Write([]byte(`{"jsonrpc":"2.0","id":"` + raw.ID + `","result":{"protocolVersion":"2025-06-18","capabilities":{"tools":{}},"serverInfo":{"name":"http-test"}}}`))
		case "notifications/initialized":
			if got := r.Header.Get("mcp-protocol-version"); got != "2025-06-18" {
				t.Fatalf("initialized protocol version = %q", got)
			}
			if raw.ID != "" {
				t.Fatalf("initialized notification id = %q", raw.ID)
			}
			w.WriteHeader(http.StatusAccepted)
		case "tools/list":
			if got := r.Header.Get("mcp-protocol-version"); got != "2025-06-18" {
				t.Fatalf("tools/list protocol version = %q", got)
			}
			_, _ = w.Write([]byte(`{"jsonrpc":"2.0","id":"` + raw.ID + `","result":{"tools":[{"name":"ping","readOnly":true}]}}`))
		default:
			t.Fatalf("method = %s", raw.Method)
		}
	}))
	defer server.Close()

	toolset, err := BuildServerToolSet(context.Background(), "remote", contracts.MCPServer{Type: TransportHTTP, URL: server.URL}, ServerToolOptions{
		DisableResources: true,
		DisablePrompts:   true,
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(toolset.Tools) != 1 || toolset.Tools[0].Name() != "mcp__remote__ping" {
		t.Fatalf("tools = %#v", toolset.Tools)
	}
	want := []string{"initialize", "notifications/initialized", "tools/list"}
	if len(methods) != len(want) {
		t.Fatalf("methods = %#v", methods)
	}
	for i := range want {
		if methods[i] != want[i] {
			t.Fatalf("methods = %#v", methods)
		}
	}
}

func TestBuildServerToolSetUsesDynamicHeaders(t *testing.T) {
	var calls int
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls++
		if got, want := r.Header.Get("Authorization"), "Bearer dynamic-"+strconv.Itoa(calls); got != want {
			t.Fatalf("authorization = %q, want %q", got, want)
		}
		var raw struct {
			ID     string `json:"id"`
			Method string `json:"method"`
		}
		if err := json.NewDecoder(r.Body).Decode(&raw); err != nil {
			t.Fatal(err)
		}
		switch raw.Method {
		case "initialize":
			_, _ = w.Write([]byte(`{"jsonrpc":"2.0","id":"` + raw.ID + `","result":{"protocolVersion":"2025-06-18","capabilities":{"tools":{}}}}`))
		case "notifications/initialized":
			w.WriteHeader(http.StatusAccepted)
		case "tools/list":
			_, _ = w.Write([]byte(`{"jsonrpc":"2.0","id":"` + raw.ID + `","result":{"tools":[]}}`))
		default:
			t.Fatalf("method = %s", raw.Method)
		}
	}))
	defer server.Close()

	_, err := BuildServerToolSet(context.Background(), "remote", contracts.MCPServer{
		Type:      TransportHTTP,
		URL:       server.URL,
		AuthToken: "stale",
	}, ServerToolOptions{
		DisableResources: true,
		DisablePrompts:   true,
		HeaderProvider: func(ctx context.Context, name string, server contracts.MCPServer) (map[string]string, error) {
			if name != "remote" || server.URL == "" {
				t.Fatalf("provider input name=%q server=%#v", name, server)
			}
			return map[string]string{"Authorization": "Bearer dynamic-" + strconv.Itoa(calls+1)}, nil
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	if calls != 3 {
		t.Fatalf("calls = %d", calls)
	}
}

func TestBuildServerToolSetUsesOAuthAccessTokenProvider(t *testing.T) {
	var calls int
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls++
		if got := r.Header.Get("Authorization"); got != "Bearer fresh" {
			t.Fatalf("authorization = %q", got)
		}
		if got := r.Header.Get("anthropic-beta"); got == "" {
			t.Fatal("missing oauth beta header")
		}
		var raw struct {
			ID     string `json:"id"`
			Method string `json:"method"`
		}
		if err := json.NewDecoder(r.Body).Decode(&raw); err != nil {
			t.Fatal(err)
		}
		switch raw.Method {
		case "initialize":
			_, _ = w.Write([]byte(`{"jsonrpc":"2.0","id":"` + raw.ID + `","result":{"protocolVersion":"2025-06-18","capabilities":{"tools":{}}}}`))
		case "notifications/initialized":
			w.WriteHeader(http.StatusAccepted)
		case "tools/list":
			_, _ = w.Write([]byte(`{"jsonrpc":"2.0","id":"` + raw.ID + `","result":{"tools":[{"name":"ping","readOnly":true}]}}`))
		default:
			t.Fatalf("method = %s", raw.Method)
		}
	}))
	defer server.Close()

	toolset, err := BuildServerToolSet(context.Background(), "remote", contracts.MCPServer{
		Type:      TransportHTTP,
		URL:       server.URL,
		AuthToken: "stale",
		OAuth:     &contracts.MCPOAuthConfig{ClientID: "client"},
	}, ServerToolOptions{
		DisableResources: true,
		DisablePrompts:   true,
		AccessTokenProvider: func(ctx context.Context, name string, server contracts.MCPServer) (AccessTokenProvider, error) {
			if name != "remote" || server.OAuth == nil {
				t.Fatalf("provider input name=%q server=%#v", name, server)
			}
			return testAccessTokenProvider{token: "fresh"}, nil
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(toolset.Tools) != 1 || toolset.Tools[0].Name() != "mcp__remote__ping" {
		t.Fatalf("tools = %#v", toolset.Tools)
	}
	if calls != 3 {
		t.Fatalf("calls = %d", calls)
	}
}

func TestBuildServerToolSetRefreshesOAuthTokenOnUnauthorized(t *testing.T) {
	provider := &refreshableTestAccessTokenProvider{token: "stale", refreshed: "fresh"}
	var calls int
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls++
		if calls == 1 {
			if got := r.Header.Get("Authorization"); got != "Bearer stale" {
				t.Fatalf("initial authorization = %q", got)
			}
			http.Error(w, "expired", http.StatusUnauthorized)
			return
		}
		if got := r.Header.Get("Authorization"); got != "Bearer fresh" {
			t.Fatalf("authorization = %q", got)
		}
		var raw struct {
			ID     string `json:"id"`
			Method string `json:"method"`
		}
		if err := json.NewDecoder(r.Body).Decode(&raw); err != nil {
			t.Fatal(err)
		}
		switch raw.Method {
		case "initialize":
			_, _ = w.Write([]byte(`{"jsonrpc":"2.0","id":"` + raw.ID + `","result":{"protocolVersion":"2025-06-18","capabilities":{"tools":{}}}}`))
		case "notifications/initialized":
			w.WriteHeader(http.StatusAccepted)
		case "tools/list":
			_, _ = w.Write([]byte(`{"jsonrpc":"2.0","id":"` + raw.ID + `","result":{"tools":[{"name":"ping","readOnly":true}]}}`))
		default:
			t.Fatalf("method = %s", raw.Method)
		}
	}))
	defer server.Close()

	toolset, err := BuildServerToolSet(context.Background(), "remote", contracts.MCPServer{
		Type:      TransportHTTP,
		URL:       server.URL,
		AuthToken: "stale-static",
		OAuth:     &contracts.MCPOAuthConfig{ClientID: "client"},
	}, ServerToolOptions{
		DisableResources:    true,
		DisablePrompts:      true,
		AccessTokenProvider: StaticServerAccessTokenProvider(provider),
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(toolset.Tools) != 1 || toolset.Tools[0].Name() != "mcp__remote__ping" {
		t.Fatalf("tools = %#v", toolset.Tools)
	}
	if provider.refreshes != 1 || calls != 4 {
		t.Fatalf("refreshes=%d calls=%d", provider.refreshes, calls)
	}
}

func TestHTTPTransportReportsNonSuccessStatus(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, "nope", http.StatusUnauthorized)
	}))
	defer server.Close()

	_, err := NewHTTPTransport(server.URL, nil, server.Client()).RoundTrip(context.Background(), NewRPCRequest("1", "tools/list", nil))
	if err == nil || !strings.Contains(err.Error(), "status 401") {
		t.Fatalf("expected status error, got %v", err)
	}
}

func TestHTTPTransportResponseLimit(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(`{"too":"large"}`))
	}))
	defer server.Close()

	transport := NewHTTPTransport(server.URL, nil, server.Client())
	transport.MaxResponseBytes = 4
	_, err := transport.RoundTrip(context.Background(), NewRPCRequest("1", "tools/list", nil))
	if err == nil || !strings.Contains(err.Error(), "exceeds 4 bytes") {
		t.Fatalf("expected response limit error, got %v", err)
	}
}

func TestHTTPTransportParsesEventStreamResponse(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("content-type", "text/event-stream")
		_, _ = w.Write([]byte(
			": keepalive\n\n" +
				"event: ping\n" +
				"data: still alive\n\n" +
				"event: message\n" +
				"data: {\"jsonrpc\":\"2.0\",\"id\":\"old\",\"result\":{\"ignored\":true}}\n\n" +
				"event: message\n" +
				"data: {\"jsonrpc\":\"2.0\",\"id\":\"9\",\"result\":{\"tools\":[{\"name\":\"sse\"}]}}\n\n",
		))
	}))
	defer server.Close()

	response, err := NewHTTPTransport(server.URL, nil, server.Client()).RoundTrip(context.Background(), NewRPCRequest("9", "tools/list", nil))
	if err != nil {
		t.Fatal(err)
	}
	if response.ID != "9" || !strings.Contains(string(response.Result), `"name":"sse"`) {
		t.Fatalf("response = %#v", response)
	}
}

func TestHTTPTransportCapturesEventStreamNotifications(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("content-type", "text/event-stream")
		_, _ = w.Write([]byte(
			"event: message\n" +
				"data: {\"jsonrpc\":\"2.0\",\"method\":\"notifications/tools/list_changed\",\"params\":{\"reason\":\"reload\"}}\n\n" +
				"event: message\n" +
				"data: {\"jsonrpc\":\"2.0\",\"id\":\"12\",\"result\":{\"tools\":[]}}\n\n",
		))
	}))
	defer server.Close()

	transport := NewHTTPTransport(server.URL, nil, server.Client())
	var notifications []RPCNotification
	transport.SetNotificationHandler(func(notification RPCNotification) {
		notifications = append(notifications, notification)
	})
	response, err := transport.RoundTrip(context.Background(), NewRPCRequest("12", "tools/list", nil))
	if err != nil {
		t.Fatal(err)
	}
	if response.ID != "12" {
		t.Fatalf("response = %#v", response)
	}
	if len(notifications) != 1 || notifications[0].Method != "notifications/tools/list_changed" || !strings.Contains(string(notifications[0].Params), `"reload"`) {
		t.Fatalf("notifications = %#v", notifications)
	}
}

func TestHTTPTransportRespondsToEventStreamInboundRequests(t *testing.T) {
	elicitationResponse := make(chan RPCResponse, 1)
	var calls int
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls++
		var raw struct {
			ID     string          `json:"id"`
			Method string          `json:"method"`
			Result json.RawMessage `json:"result"`
		}
		if err := json.NewDecoder(r.Body).Decode(&raw); err != nil {
			t.Fatal(err)
		}
		switch raw.Method {
		case "tools/list":
			flusher, ok := w.(http.Flusher)
			if !ok {
				t.Fatal("missing flusher")
			}
			w.Header().Set("content-type", "text/event-stream")
			_, _ = w.Write([]byte(
				"event: message\n" +
					"data: {\"jsonrpc\":\"2.0\",\"id\":\"server-1\",\"method\":\"elicitation/create\",\"params\":{\"message\":\"Confirm?\"}}\n\n",
			))
			flusher.Flush()
			select {
			case response := <-elicitationResponse:
				if response.ID != "server-1" || !strings.Contains(string(response.Result), `"confirmed":true`) {
					t.Fatalf("elicitation response = %#v", response)
				}
			case <-time.After(2 * time.Second):
				t.Fatal("timed out waiting for elicitation response")
			}
			_, _ = w.Write([]byte(
				"event: message\n" +
					"data: {\"jsonrpc\":\"2.0\",\"id\":\"21\",\"result\":{\"tools\":[]}}\n\n",
			))
			flusher.Flush()
		case "":
			elicitationResponse <- RPCResponse{ID: raw.ID, Result: append(json.RawMessage(nil), raw.Result...)}
			w.WriteHeader(http.StatusAccepted)
		default:
			t.Fatalf("method = %s", raw.Method)
		}
	}))
	defer server.Close()

	transport := NewHTTPTransport(server.URL, nil, server.Client())
	transport.SetRequestHandler(func(ctx context.Context, request RPCInboundRequest) (any, *RPCError) {
		parsed, ok := ParseElicitationRequest(request)
		if !ok || parsed.Message != "Confirm?" {
			t.Fatalf("request = %#v parsed=%#v", request, parsed)
		}
		return ElicitationResponse("accept", map[string]any{"confirmed": true}), nil
	})
	response, err := transport.RoundTrip(context.Background(), NewRPCRequest("21", "tools/list", nil))
	if err != nil {
		t.Fatal(err)
	}
	if response.ID != "21" {
		t.Fatalf("response = %#v", response)
	}
	if calls != 2 {
		t.Fatalf("calls=%d", calls)
	}
}

func TestRPCResponseFromSSESkipsNotificationsWithoutHandler(t *testing.T) {
	response, err := rpcResponseFromSSE(strings.NewReader(
		"event: message\n"+
			"data: {\"jsonrpc\":\"2.0\",\"method\":\"notifications/message\",\"params\":{\"level\":\"info\"}}\n\n"+
			"event: message\n"+
			"data: {\"jsonrpc\":\"2.0\",\"id\":\"server-1\",\"method\":\"elicitation/create\",\"params\":{\"message\":\"Confirm?\"}}\n\n"+
			"event: message\n"+
			"data: {\"jsonrpc\":\"2.0\",\"id\":\"13\",\"result\":{\"ok\":true}}\n\n",
	), "13")
	if err != nil {
		t.Fatal(err)
	}
	if response.ID != "13" || !strings.Contains(string(response.Result), `"ok":true`) {
		t.Fatalf("response = %#v", response)
	}
}

func TestHTTPTransportStoresAndSendsSessionID(t *testing.T) {
	var calls int
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls++
		if calls == 1 {
			w.Header().Set("mcp-session-id", "session-1")
		} else if got := r.Header.Get("mcp-session-id"); got != "session-1" {
			t.Fatalf("session header = %q", got)
		}
		_, _ = w.Write([]byte(`{"jsonrpc":"2.0","id":"1","result":{"tools":[]}}`))
	}))
	defer server.Close()

	transport := NewHTTPTransport(server.URL, nil, server.Client())
	if _, err := transport.RoundTrip(context.Background(), NewRPCRequest("1", "tools/list", nil)); err != nil {
		t.Fatal(err)
	}
	if transport.SessionID != "session-1" {
		t.Fatalf("session id = %q", transport.SessionID)
	}
	if _, err := transport.RoundTrip(context.Background(), NewRPCRequest("1", "tools/list", nil)); err != nil {
		t.Fatal(err)
	}
}

func TestHTTPTransportResetSessionClearsSessionHeader(t *testing.T) {
	var calls int
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls++
		switch calls {
		case 1:
			w.Header().Set("mcp-session-id", "session-reset")
		case 2:
			if got := r.Header.Get("mcp-session-id"); got != "" {
				t.Fatalf("session header after reset = %q", got)
			}
		default:
			t.Fatalf("unexpected call %d", calls)
		}
		_, _ = w.Write([]byte(`{"jsonrpc":"2.0","id":"1","result":{"tools":[]}}`))
	}))
	defer server.Close()

	transport := NewHTTPTransport(server.URL, nil, server.Client())
	if _, err := transport.RoundTrip(context.Background(), NewRPCRequest("1", "tools/list", nil)); err != nil {
		t.Fatal(err)
	}
	if transport.SessionID != "session-reset" {
		t.Fatalf("session id = %q", transport.SessionID)
	}
	transport.ResetSession()
	if transport.SessionID != "" {
		t.Fatalf("session id after reset = %q", transport.SessionID)
	}
	if _, err := transport.RoundTrip(context.Background(), NewRPCRequest("1", "tools/list", nil)); err != nil {
		t.Fatal(err)
	}
}

func TestHTTPTransportCloseDeletesSession(t *testing.T) {
	var deleted bool
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.Method {
		case http.MethodPost:
			w.Header().Set("mcp-session-id", "session-close")
			_, _ = w.Write([]byte(`{"jsonrpc":"2.0","id":"1","result":{"tools":[]}}`))
		case http.MethodDelete:
			if got := r.Header.Get("mcp-session-id"); got != "session-close" {
				t.Fatalf("delete session header = %q", got)
			}
			deleted = true
			w.WriteHeader(http.StatusNoContent)
		default:
			t.Fatalf("method = %s", r.Method)
		}
	}))
	defer server.Close()

	transport := NewHTTPTransport(server.URL, map[string]string{"X-Test": "yes"}, server.Client())
	if _, err := transport.RoundTrip(context.Background(), NewRPCRequest("1", "tools/list", nil)); err != nil {
		t.Fatal(err)
	}
	if err := transport.Close(); err != nil {
		t.Fatal(err)
	}
	if !deleted {
		t.Fatal("expected delete request")
	}
}

func TestParseSSEEventsSupportsMultilineData(t *testing.T) {
	events, err := ParseSSEEvents(strings.NewReader(
		": ignored\n" +
			"id: e1\n" +
			"event: message\n" +
			"data: {\"a\":1,\n" +
			"data: \"b\":2}\n\n",
	))
	if err != nil {
		t.Fatal(err)
	}
	if len(events) != 1 || events[0].ID != "e1" || events[0].Event != "message" || !strings.Contains(events[0].Data, "\n") {
		t.Fatalf("events = %#v", events)
	}
}
