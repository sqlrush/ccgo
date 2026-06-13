package mcp

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"ccgo/internal/contracts"
)

func TestSSETransportDiscoversEndpointAndPostsRequests(t *testing.T) {
	var gotRequest RPCRequest
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/sse":
			if got := r.Header.Get("Authorization"); got != "Bearer token" {
				t.Fatalf("sse authorization = %q", got)
			}
			w.Header().Set("content-type", "text/event-stream")
			_, _ = w.Write([]byte("event: endpoint\n"))
			_, _ = w.Write([]byte("data: /message\n\n"))
		case "/message":
			if r.Method != http.MethodPost {
				t.Fatalf("method = %s", r.Method)
			}
			if err := json.NewDecoder(r.Body).Decode(&gotRequest); err != nil {
				t.Fatal(err)
			}
			_, _ = w.Write([]byte(`{"jsonrpc":"2.0","id":"3","result":{"tools":[{"name":"pong"}]}}`))
		default:
			t.Fatalf("path = %s", r.URL.Path)
		}
	}))
	defer server.Close()

	transport := NewSSETransport(server.URL+"/sse", map[string]string{"Authorization": "Bearer token"}, server.Client())
	response, err := transport.RoundTrip(context.Background(), NewRPCRequest("3", "tools/list", nil))
	if err != nil {
		t.Fatal(err)
	}
	if gotRequest.Method != "tools/list" || response.ID != "3" || !strings.Contains(string(response.Result), `"pong"`) {
		t.Fatalf("request=%#v response=%#v", gotRequest, response)
	}
	if endpoint, err := transport.endpoint(context.Background()); err != nil || endpoint != server.URL+"/message" {
		t.Fatalf("endpoint = %q, %v", endpoint, err)
	}
}

func TestSSETransportEndpointDiscoveryResponseLimit(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("content-type", "text/event-stream")
		_, _ = w.Write([]byte(
			"event: message\n" +
				"data: " + strings.Repeat("x", 80) + "\n\n",
		))
	}))
	defer server.Close()

	transport := NewSSETransport(server.URL+"/sse", nil, server.Client())
	transport.MaxResponseBytes = 24
	_, err := transport.RoundTrip(context.Background(), NewRPCRequest("3", "tools/list", nil))
	if err == nil || !strings.Contains(err.Error(), "exceeds 24 bytes") {
		t.Fatalf("expected endpoint discovery response limit error, got %v", err)
	}
}

func TestSSETransportWaitsForAsyncStreamResponse(t *testing.T) {
	postReceived := make(chan RPCRequest, 1)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/sse":
			flusher, ok := w.(http.Flusher)
			if !ok {
				t.Fatal("missing flusher")
			}
			w.Header().Set("content-type", "text/event-stream")
			_, _ = w.Write([]byte("event: endpoint\n"))
			_, _ = w.Write([]byte("data: /message\n\n"))
			flusher.Flush()
			request := <-postReceived
			_, _ = w.Write([]byte("event: message\n"))
			_, _ = w.Write([]byte(`data: {"jsonrpc":"2.0","method":"notifications/resources/list_changed","params":{"uri":"file:///a"}}` + "\n\n"))
			flusher.Flush()
			_, _ = w.Write([]byte("event: message\n"))
			_, _ = w.Write([]byte(`data: {"jsonrpc":"2.0","id":"` + request.ID + `","result":{"tools":[{"name":"async"}]}}` + "\n\n"))
			flusher.Flush()
		case "/message":
			var request RPCRequest
			if err := json.NewDecoder(r.Body).Decode(&request); err != nil {
				t.Fatal(err)
			}
			postReceived <- request
			w.WriteHeader(http.StatusAccepted)
		default:
			t.Fatalf("path = %s", r.URL.Path)
		}
	}))
	defer server.Close()

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	transport := NewSSETransport(server.URL+"/sse", nil, server.Client())
	var notifications []RPCNotification
	transport.SetNotificationHandler(func(notification RPCNotification) {
		notifications = append(notifications, notification)
	})
	response, err := transport.RoundTrip(ctx, NewRPCRequest("11", "tools/list", nil))
	if err != nil {
		t.Fatal(err)
	}
	if response.ID != "11" || !strings.Contains(string(response.Result), `"async"`) {
		t.Fatalf("response = %#v", response)
	}
	if len(notifications) != 1 || notifications[0].Method != "notifications/resources/list_changed" || !strings.Contains(string(notifications[0].Params), `file:///a`) {
		t.Fatalf("notifications = %#v", notifications)
	}
}

func TestSSETransportRespondsToStreamInboundRequests(t *testing.T) {
	postReceived := make(chan RPCRequest, 1)
	elicitationResponse := make(chan RPCResponse, 1)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/sse":
			flusher, ok := w.(http.Flusher)
			if !ok {
				t.Fatal("missing flusher")
			}
			w.Header().Set("content-type", "text/event-stream")
			_, _ = w.Write([]byte("event: endpoint\n"))
			_, _ = w.Write([]byte("data: /message\n\n"))
			flusher.Flush()
			request := <-postReceived
			_, _ = w.Write([]byte("event: message\n"))
			_, _ = w.Write([]byte(`data: {"jsonrpc":"2.0","id":"server-1","method":"elicitation/create","params":{"message":"Approve?"}}` + "\n\n"))
			flusher.Flush()
			select {
			case response := <-elicitationResponse:
				if response.ID != "server-1" || !strings.Contains(string(response.Result), `"approved":true`) {
					t.Fatalf("elicitation response = %#v", response)
				}
			case <-time.After(2 * time.Second):
				t.Fatal("timed out waiting for elicitation response")
			}
			_, _ = w.Write([]byte("event: message\n"))
			_, _ = w.Write([]byte(`data: {"jsonrpc":"2.0","id":"` + request.ID + `","result":{"tools":[{"name":"done"}]}}` + "\n\n"))
			flusher.Flush()
		case "/message":
			var raw struct {
				ID     string          `json:"id"`
				Method string          `json:"method"`
				Result json.RawMessage `json:"result"`
			}
			if err := json.NewDecoder(r.Body).Decode(&raw); err != nil {
				t.Fatal(err)
			}
			if raw.Method != "" {
				postReceived <- RPCRequest{ID: raw.ID, Method: raw.Method}
			} else {
				elicitationResponse <- RPCResponse{ID: raw.ID, Result: raw.Result}
			}
			w.WriteHeader(http.StatusAccepted)
		default:
			t.Fatalf("path = %s", r.URL.Path)
		}
	}))
	defer server.Close()

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	transport := NewSSETransport(server.URL+"/sse", nil, server.Client())
	transport.SetRequestHandler(func(ctx context.Context, request RPCInboundRequest) (any, *RPCError) {
		parsed, ok := ParseElicitationRequest(request)
		if !ok || parsed.Message != "Approve?" {
			t.Fatalf("request = %#v parsed=%#v", request, parsed)
		}
		return ElicitationResponse("accept", map[string]any{"approved": true}), nil
	})
	response, err := transport.RoundTrip(ctx, NewRPCRequest("31", "tools/list", nil))
	if err != nil {
		t.Fatal(err)
	}
	if response.ID != "31" || !strings.Contains(string(response.Result), `"done"`) {
		t.Fatalf("response = %#v", response)
	}
}

func TestSSETransportReconnectsWithLastEventID(t *testing.T) {
	firstPost := make(chan struct{}, 1)
	var sseCalls int
	var postCalls int
	var secondLastEventID string
	var secondSessionID string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/sse":
			sseCalls++
			flusher, ok := w.(http.Flusher)
			if !ok {
				t.Fatal("missing flusher")
			}
			w.Header().Set("content-type", "text/event-stream")
			switch sseCalls {
			case 1:
				if got := r.Header.Get("Last-Event-ID"); got != "" {
					t.Fatalf("first last-event-id = %q", got)
				}
				w.Header().Set("mcp-session-id", "session-reconnect")
				_, _ = w.Write([]byte("event: endpoint\n"))
				_, _ = w.Write([]byte("data: /message\n\n"))
				flusher.Flush()
				select {
				case <-firstPost:
				case <-time.After(2 * time.Second):
					t.Fatal("timed out waiting for first post")
				}
				_, _ = w.Write([]byte("id: evt-1\n"))
				_, _ = w.Write([]byte("event: message\n"))
				_, _ = w.Write([]byte(`data: {"jsonrpc":"2.0","method":"notifications/message","params":{"message":"before reconnect"}}` + "\n\n"))
				flusher.Flush()
			case 2:
				secondLastEventID = r.Header.Get("Last-Event-ID")
				secondSessionID = r.Header.Get("mcp-session-id")
				_, _ = w.Write([]byte("event: endpoint\n"))
				_, _ = w.Write([]byte("data: /message\n\n"))
				flusher.Flush()
				_, _ = w.Write([]byte("event: message\n"))
				_, _ = w.Write([]byte(`data: {"jsonrpc":"2.0","id":"41","result":{"tools":[{"name":"after-reconnect"}]}}` + "\n\n"))
				flusher.Flush()
			default:
				t.Fatalf("unexpected sse reconnect %d", sseCalls)
			}
		case "/message":
			postCalls++
			var request RPCRequest
			if err := json.NewDecoder(r.Body).Decode(&request); err != nil {
				t.Fatal(err)
			}
			if request.ID == "41" {
				firstPost <- struct{}{}
				w.WriteHeader(http.StatusAccepted)
				return
			}
			t.Fatalf("unexpected post request = %#v", request)
		default:
			t.Fatalf("path = %s", r.URL.Path)
		}
	}))
	defer server.Close()

	transport := NewSSETransport(server.URL+"/sse", nil, server.Client())
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	response, err := transport.RoundTrip(ctx, NewRPCRequest("41", "tools/list", nil))
	if err != nil {
		t.Fatal(err)
	}
	if response.ID != "41" || !strings.Contains(string(response.Result), `"after-reconnect"`) {
		t.Fatalf("response = %#v", response)
	}
	if sseCalls != 2 || postCalls != 1 || secondLastEventID != "evt-1" || secondSessionID != "session-reconnect" {
		t.Fatalf("sseCalls=%d postCalls=%d lastEventID=%q sessionID=%q", sseCalls, postCalls, secondLastEventID, secondSessionID)
	}
}

func TestOpenServerClientSupportsSSETransport(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/events":
			w.Header().Set("content-type", "text/event-stream")
			_, _ = w.Write([]byte(`event: endpoint` + "\n"))
			_, _ = w.Write([]byte(`data: {"endpoint":"/rpc"}` + "\n\n"))
		case "/rpc":
			_, _ = w.Write([]byte(`{"jsonrpc":"2.0","id":"1","result":{"tools":[{"name":"sse-tool","readOnly":true}]}}`))
		default:
			t.Fatalf("path = %s", r.URL.Path)
		}
	}))
	defer server.Close()

	handle, err := OpenServerClient(context.Background(), "remote", contracts.MCPServer{
		Type: TransportSSE,
		URL:  server.URL + "/events",
	})
	if err != nil {
		t.Fatal(err)
	}
	tools, err := handle.Client.ListTools(context.Background(), "remote")
	if err != nil {
		t.Fatal(err)
	}
	if len(tools) != 1 || tools[0].Name != "sse-tool" || !tools[0].ReadOnly {
		t.Fatalf("tools = %#v", tools)
	}
	if handle.Close == nil {
		t.Fatal("expected sse close")
	}
}

func TestResolveSSEEndpointAcceptsQuotedAndAbsoluteURLs(t *testing.T) {
	relative, err := resolveSSEEndpoint("https://example.com/sse", `"/messages"`)
	if err != nil {
		t.Fatal(err)
	}
	if relative != "https://example.com/messages" {
		t.Fatalf("relative = %q", relative)
	}
	absolute, err := resolveSSEEndpoint("https://example.com/sse", `{"url":"https://api.example.com/rpc"}`)
	if err != nil {
		t.Fatal(err)
	}
	if absolute != "https://api.example.com/rpc" {
		t.Fatalf("absolute = %q", absolute)
	}
}
