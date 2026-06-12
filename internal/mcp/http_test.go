package mcp

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

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

func TestOpenServerClientSupportsHTTPTransport(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(`{"jsonrpc":"2.0","id":"1","result":{"tools":[{"name":"ping","readOnly":true}]}}`))
	}))
	defer server.Close()

	handle, err := OpenServerClient(context.Background(), "remote", contracts.MCPServer{
		Type: TransportHTTP,
		URL:  server.URL,
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
	if handle.Close != nil {
		t.Fatalf("http close should be nil")
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
