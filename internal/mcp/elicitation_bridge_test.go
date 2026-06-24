package mcp

// G29: MCP-34/35 elicitation bridge tests.
//
// Tests that:
// 1. ServerToolOptions.ElicitationHandler wires into the ProtocolClient
//    request handler via newProtocolClientWithOptions.
// 2. Manager.SetElicitationHandler propagates to all connected clients.

import (
	"context"
	"encoding/json"
	"testing"

	"ccgo/internal/contracts"
)

// TestServerToolOptionsElicitationHandlerWired verifies that when
// ServerToolOptions.ElicitationHandler is set, newProtocolClientWithOptions
// configures the underlying ProtocolClient to route elicitation/create
// requests through that handler.
func TestServerToolOptionsElicitationHandlerWired(t *testing.T) {
	var got ElicitationRequest
	handler := func(_ context.Context, req ElicitationRequest) (map[string]any, error) {
		got = req
		return ElicitationResponse("accept", nil), nil
	}

	opts := ServerToolOptions{ElicitationHandler: handler}
	transport := &minimalFakeTransport{}
	client := newProtocolClientWithOptions(transport, opts)

	// Simulate an inbound elicitation/create from the server.
	reqParams, _ := json.Marshal(map[string]any{"message": "Please confirm"})
	result, rpcErr := client.handleRequest(context.Background(), RPCInboundRequest{
		ID:     "e1",
		Method: "elicitation/create",
		Params: reqParams,
	})
	if rpcErr != nil {
		t.Fatalf("handleRequest rpcErr: %v", rpcErr)
	}
	if result == nil {
		t.Fatal("handleRequest returned nil result")
	}
	if got.Message != "Please confirm" {
		t.Fatalf("handler got message = %q, want 'Please confirm'", got.Message)
	}
	resp, ok := result.(map[string]any)
	if !ok {
		t.Fatalf("result type = %T", result)
	}
	if resp["action"] != "accept" {
		t.Fatalf("action = %v, want accept", resp["action"])
	}
}

// TestManagerSetElicitationHandlerPropagates verifies that calling
// SetElicitationHandler on a Manager propagates the handler to all
// currently-connected clients via their SetRequestHandler.
func TestManagerSetElicitationHandlerPropagates(t *testing.T) {
	var handlerCalled bool
	handler := func(_ context.Context, _ ElicitationRequest) (map[string]any, error) {
		handlerCalled = true
		return ElicitationResponse("cancel", nil), nil
	}

	// Build a manager with one ProtocolClient over a fake transport.
	transport := &minimalFakeTransport{}
	fakeProtoClient := NewProtocolClient(transport)

	mgr := NewManager(map[string]contracts.MCPServer{}, func(context.Context, string, contracts.MCPServer) (ClientHandle, error) {
		return ClientHandle{}, nil
	})
	// Inject a connected entry directly (bypasses dial for unit-test speed).
	mgr.mu.Lock()
	mgr.servers["s1"] = &serverEntry{
		status: ServerStatusConnected,
		client: fakeProtoClient,
	}
	mgr.mu.Unlock()

	mgr.SetElicitationHandler(handler)

	// Verify the handler is wired by simulating an inbound request on the client.
	reqParams, _ := json.Marshal(map[string]any{"message": "test"})
	_, rpcErr := fakeProtoClient.handleRequest(context.Background(), RPCInboundRequest{
		ID:     "e2",
		Method: "elicitation/create",
		Params: reqParams,
	})
	if rpcErr != nil {
		t.Fatalf("handleRequest rpcErr: %v", rpcErr)
	}
	if !handlerCalled {
		t.Fatal("elicitation handler was not called after SetElicitationHandler")
	}
}

// minimalFakeTransport satisfies RPCTransport + RPCRequestTransport.
type minimalFakeTransport struct {
	handler RPCRequestHandler
}

func (f *minimalFakeTransport) RoundTrip(_ context.Context, _ RPCRequest) (RPCResponse, error) {
	return RPCResponse{}, nil
}

func (f *minimalFakeTransport) SetRequestHandler(h RPCRequestHandler) {
	f.handler = h
}
