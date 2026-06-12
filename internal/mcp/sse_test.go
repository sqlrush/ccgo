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
