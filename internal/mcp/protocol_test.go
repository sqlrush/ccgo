package mcp

import (
	"context"
	"encoding/json"
	"strings"
	"testing"
)

type fakeRPCTransport struct {
	responses map[string]json.RawMessage
	rpcErr    *RPCError
	requests  []RPCRequest
}

func (t *fakeRPCTransport) RoundTrip(_ context.Context, request RPCRequest) (RPCResponse, error) {
	t.requests = append(t.requests, request)
	if t.rpcErr != nil {
		return RPCResponse{ID: request.ID, Error: t.rpcErr}, nil
	}
	return RPCResponse{
		JSONRPC: JSONRPCVersion,
		ID:      request.ID,
		Result:  t.responses[request.Method],
	}, nil
}

func TestProtocolClientListsAndCallsTools(t *testing.T) {
	transport := &fakeRPCTransport{responses: map[string]json.RawMessage{
		"tools/list": json.RawMessage(`{"tools":[{"name":"search","description":"Search issues","inputSchema":{"type":"object"},"readOnly":true}]}`),
		"tools/call": json.RawMessage(`{"content":[{"type":"text","text":"ok"}]}`),
	}}
	client := NewProtocolClient(transport)

	tools, err := client.ListTools(context.Background(), "github")
	if err != nil {
		t.Fatal(err)
	}
	if len(tools) != 1 || tools[0].Name != "search" || !tools[0].ReadOnly {
		t.Fatalf("tools = %#v", tools)
	}
	if tools[0].InputSchema["type"] != "object" {
		t.Fatalf("schema = %#v", tools[0].InputSchema)
	}

	result, err := client.CallTool(context.Background(), "github", "search", json.RawMessage(`{"query":"bugs"}`))
	if err != nil {
		t.Fatal(err)
	}
	resultMap := result.(map[string]any)
	if _, ok := resultMap["content"]; !ok {
		t.Fatalf("result = %#v", result)
	}
	if len(transport.requests) != 2 || transport.requests[0].Method != "tools/list" || transport.requests[1].Method != "tools/call" {
		t.Fatalf("requests = %#v", transport.requests)
	}
	params := mustJSON(t, transport.requests[1].Params)
	if !strings.Contains(params, `"name":"search"`) || !strings.Contains(params, `"query":"bugs"`) {
		t.Fatalf("call params = %s", params)
	}
}

func TestProtocolClientResourcesAndPrompts(t *testing.T) {
	transport := &fakeRPCTransport{responses: map[string]json.RawMessage{
		"resources/list": json.RawMessage(`{"resources":[{"uri":"file:///a.txt","name":"a.txt","description":"A file","mimeType":"text/plain"}]}`),
		"resources/read": json.RawMessage(`{"contents":[{"uri":"file:///a.txt","mime_type":"text/plain","text":"hello"}]}`),
		"prompts/list":   json.RawMessage(`{"prompts":[{"name":"deploy","description":"Deploy","arguments":[{"name":"env","description":"Target","required":true}]}]}`),
		"prompts/get":    json.RawMessage(`{"description":"Deploy","messages":[{"role":"user","content":{"type":"text","text":"deploy prod"}}]}`),
	}}
	client := NewProtocolClient(transport)

	resources, err := client.ListResources(context.Background(), "files")
	if err != nil {
		t.Fatal(err)
	}
	if len(resources) != 1 || resources[0].URI != "file:///a.txt" || resources[0].MimeType != "text/plain" {
		t.Fatalf("resources = %#v", resources)
	}
	contents, err := client.ReadResource(context.Background(), "files", "file:///a.txt")
	if err != nil {
		t.Fatal(err)
	}
	if len(contents) != 1 || contents[0].Text != "hello" || contents[0].MimeType != "text/plain" {
		t.Fatalf("contents = %#v", contents)
	}

	prompts, err := client.ListPrompts(context.Background(), "workflow")
	if err != nil {
		t.Fatal(err)
	}
	if len(prompts) != 1 || prompts[0].Name != "deploy" || len(prompts[0].Arguments) != 1 || !prompts[0].Arguments[0].Required {
		t.Fatalf("prompts = %#v", prompts)
	}
	prompt, err := client.GetPrompt(context.Background(), "workflow", "deploy", map[string]string{"env": "prod"})
	if err != nil {
		t.Fatal(err)
	}
	content := prompt.Messages[0].Content.(map[string]any)
	if prompt.Description != "Deploy" || content["text"] != "deploy prod" {
		t.Fatalf("prompt = %#v", prompt)
	}

	if len(transport.requests) != 4 {
		t.Fatalf("requests = %#v", transport.requests)
	}
	readParams := mustJSON(t, transport.requests[1].Params)
	getParams := mustJSON(t, transport.requests[3].Params)
	if !strings.Contains(readParams, `"uri":"file:///a.txt"`) || !strings.Contains(getParams, `"env":"prod"`) {
		t.Fatalf("params = %s / %s", readParams, getParams)
	}
}

func TestProtocolClientRPCErrorAndSessionExpired(t *testing.T) {
	client := NewProtocolClient(&fakeRPCTransport{rpcErr: &RPCError{
		Code:    -32001,
		Message: "Session expired",
		Data:    map[string]any{"type": "session-expired"},
	}})
	_, err := client.ListTools(context.Background(), "github")
	if err == nil {
		t.Fatal("expected rpc error")
	}
	if !IsSessionExpiredError(err) {
		t.Fatalf("expected session expired error: %v", err)
	}
}

func mustJSON(t *testing.T, value any) string {
	t.Helper()
	data, err := json.Marshal(value)
	if err != nil {
		t.Fatal(err)
	}
	return string(data)
}
