package mcp

import (
	"bytes"
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

type fakePaginatedTransport struct {
	responses map[string][]json.RawMessage
	requests  []RPCRequest
	calls     map[string]int
}

type fakeLifecycleTransport struct {
	requests      []RPCRequest
	notifications []RPCNotification
}

type fakeSessionRecoveryTransport struct {
	requests      []RPCRequest
	notifications []RPCNotification
	resets        int
	listCalls     int
}

type fakeAuthorizationRecoveryTransport struct {
	requests      []RPCRequest
	notifications []RPCNotification
	resets        int
	refreshes     int
	listCalls     int
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

func (t *fakePaginatedTransport) RoundTrip(_ context.Context, request RPCRequest) (RPCResponse, error) {
	t.requests = append(t.requests, request)
	if t.calls == nil {
		t.calls = map[string]int{}
	}
	index := t.calls[request.Method]
	t.calls[request.Method] = index + 1
	pages := t.responses[request.Method]
	if index >= len(pages) {
		return RPCResponse{ID: request.ID, Error: &RPCError{Code: -32603, Message: "unexpected page request"}}, nil
	}
	return RPCResponse{
		JSONRPC: JSONRPCVersion,
		ID:      request.ID,
		Result:  pages[index],
	}, nil
}

func (t *fakeLifecycleTransport) RoundTrip(_ context.Context, request RPCRequest) (RPCResponse, error) {
	t.requests = append(t.requests, request)
	if request.Method != "initialize" {
		return RPCResponse{ID: request.ID, Result: json.RawMessage(`{}`)}, nil
	}
	return RPCResponse{
		JSONRPC: JSONRPCVersion,
		ID:      request.ID,
		Result: json.RawMessage(`{
			"protocolVersion":"2025-06-18",
			"capabilities":{"tools":{}},
			"serverInfo":{"name":"test-server","version":"1.2.3"},
			"instructions":"be useful"
		}`),
	}, nil
}

func (t *fakeLifecycleTransport) SendNotification(_ context.Context, notification RPCNotification) error {
	t.notifications = append(t.notifications, notification)
	return nil
}

func (t *fakeSessionRecoveryTransport) RoundTrip(_ context.Context, request RPCRequest) (RPCResponse, error) {
	t.requests = append(t.requests, request)
	switch request.Method {
	case "initialize":
		return RPCResponse{
			JSONRPC: JSONRPCVersion,
			ID:      request.ID,
			Result:  json.RawMessage(`{"protocolVersion":"2025-06-18","capabilities":{"tools":{}}}`),
		}, nil
	case "tools/list":
		t.listCalls++
		if t.listCalls == 1 {
			return RPCResponse{JSONRPC: JSONRPCVersion, ID: request.ID, Error: &RPCError{
				Code:    -32001,
				Message: "session expired",
				Data:    map[string]any{"type": "session-expired"},
			}}, nil
		}
		return RPCResponse{
			JSONRPC: JSONRPCVersion,
			ID:      request.ID,
			Result:  json.RawMessage(`{"tools":[{"name":"ping","readOnly":true}]}`),
		}, nil
	default:
		return RPCResponse{JSONRPC: JSONRPCVersion, ID: request.ID, Result: json.RawMessage(`{}`)}, nil
	}
}

func (t *fakeSessionRecoveryTransport) SendNotification(_ context.Context, notification RPCNotification) error {
	t.notifications = append(t.notifications, notification)
	return nil
}

func (t *fakeSessionRecoveryTransport) ResetSession() {
	t.resets++
}

func (t *fakeAuthorizationRecoveryTransport) RoundTrip(_ context.Context, request RPCRequest) (RPCResponse, error) {
	t.requests = append(t.requests, request)
	switch request.Method {
	case "initialize":
		return RPCResponse{
			JSONRPC: JSONRPCVersion,
			ID:      request.ID,
			Result:  json.RawMessage(`{"protocolVersion":"2025-06-18","capabilities":{"tools":{}}}`),
		}, nil
	case "tools/list":
		t.listCalls++
		if t.listCalls == 1 {
			return RPCResponse{}, &HTTPStatusError{Prefix: "mcp http", StatusCode: 401, Body: "expired"}
		}
		return RPCResponse{
			JSONRPC: JSONRPCVersion,
			ID:      request.ID,
			Result:  json.RawMessage(`{"tools":[{"name":"ping","readOnly":true}]}`),
		}, nil
	default:
		return RPCResponse{JSONRPC: JSONRPCVersion, ID: request.ID, Result: json.RawMessage(`{}`)}, nil
	}
}

func (t *fakeAuthorizationRecoveryTransport) SendNotification(_ context.Context, notification RPCNotification) error {
	t.notifications = append(t.notifications, notification)
	return nil
}

func (t *fakeAuthorizationRecoveryTransport) ResetSession() {
	t.resets++
}

func (t *fakeAuthorizationRecoveryTransport) RefreshAuthorization(context.Context) (bool, error) {
	t.refreshes++
	return true, nil
}

func TestProtocolClientListsAndCallsTools(t *testing.T) {
	transport := &fakeRPCTransport{responses: map[string]json.RawMessage{
		"tools/list": json.RawMessage(`{"tools":[{"name":"search","description":"Search issues","inputSchema":{"type":"object"},"outputSchema":{"type":"object","properties":{"total":{"type":"number"}}},"annotations":{"readOnlyHint":true}}]}`),
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
	if tools[0].OutputSchema["type"] != "object" {
		t.Fatalf("output schema = %#v", tools[0].OutputSchema)
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

func TestProtocolClientReadsToolAnnotations(t *testing.T) {
	transport := &fakeRPCTransport{responses: map[string]json.RawMessage{
		"tools/list": json.RawMessage(`{"tools":[{"name":"delete","annotations":{"destructiveHint":true}}]}`),
	}}
	client := NewProtocolClient(transport)

	tools, err := client.ListTools(context.Background(), "local")
	if err != nil {
		t.Fatal(err)
	}
	if len(tools) != 1 || tools[0].Name != "delete" || tools[0].ReadOnly || !tools[0].Destructive {
		t.Fatalf("tools = %#v", tools)
	}
}

func TestProtocolClientPaginatesListMethods(t *testing.T) {
	transport := &fakePaginatedTransport{responses: map[string][]json.RawMessage{
		"tools/list": {
			json.RawMessage(`{"tools":[{"name":"first"}],"nextCursor":"tools-page-2"}`),
			json.RawMessage(`{"tools":[{"name":"second"}]}`),
		},
		"resources/list": {
			json.RawMessage(`{"resources":[{"uri":"file:///first"}],"next_cursor":"resources-page-2"}`),
			json.RawMessage(`{"resources":[{"uri":"file:///second"}]}`),
		},
		"prompts/list": {
			json.RawMessage(`{"prompts":[{"name":"first"}],"cursor":"prompts-page-2"}`),
			json.RawMessage(`{"prompts":[{"name":"second"}]}`),
		},
	}}
	client := NewProtocolClient(transport)

	tools, err := client.ListTools(context.Background(), "server")
	if err != nil {
		t.Fatal(err)
	}
	if len(tools) != 2 || tools[0].Name != "first" || tools[1].Name != "second" {
		t.Fatalf("tools = %#v", tools)
	}
	resources, err := client.ListResources(context.Background(), "server")
	if err != nil {
		t.Fatal(err)
	}
	if len(resources) != 2 || resources[0].URI != "file:///first" || resources[1].URI != "file:///second" {
		t.Fatalf("resources = %#v", resources)
	}
	prompts, err := client.ListPrompts(context.Background(), "server")
	if err != nil {
		t.Fatal(err)
	}
	if len(prompts) != 2 || prompts[0].Name != "first" || prompts[1].Name != "second" {
		t.Fatalf("prompts = %#v", prompts)
	}

	if len(transport.requests) != 6 {
		t.Fatalf("requests = %#v", transport.requests)
	}
	wantCursors := map[int]string{
		1: `"cursor":"tools-page-2"`,
		3: `"cursor":"resources-page-2"`,
		5: `"cursor":"prompts-page-2"`,
	}
	for index, want := range wantCursors {
		params := mustJSON(t, transport.requests[index].Params)
		if !strings.Contains(params, want) {
			t.Fatalf("request %d params = %s, want %s", index, params, want)
		}
	}
}

func TestProtocolClientRejectsRepeatedListCursor(t *testing.T) {
	transport := &fakePaginatedTransport{responses: map[string][]json.RawMessage{
		"tools/list": {
			json.RawMessage(`{"tools":[{"name":"first"}],"nextCursor":"again"}`),
			json.RawMessage(`{"tools":[{"name":"second"}],"nextCursor":"again"}`),
		},
	}}
	client := NewProtocolClient(transport)

	_, err := client.ListTools(context.Background(), "server")
	if err == nil || !strings.Contains(err.Error(), "repeated cursor") {
		t.Fatalf("expected repeated cursor error, got %v", err)
	}
}

func TestProtocolClientInitializeSendsLifecycleMessages(t *testing.T) {
	transport := &fakeLifecycleTransport{}
	client := NewProtocolClient(transport)

	if err := client.EnsureInitialized(context.Background()); err != nil {
		t.Fatal(err)
	}
	result := client.initializeResult
	if result.ProtocolVersion != DefaultProtocolVersion || result.ServerInfo.Name != "test-server" || result.Instructions != "be useful" {
		t.Fatalf("initialize result = %#v", result)
	}
	if len(transport.requests) != 1 || transport.requests[0].Method != "initialize" {
		t.Fatalf("requests = %#v", transport.requests)
	}
	params := mustJSON(t, transport.requests[0].Params)
	for _, want := range []string{`"protocolVersion":"2025-06-18"`, `"clientInfo"`, `"capabilities"`, `"elicitation"`} {
		if !strings.Contains(params, want) {
			t.Fatalf("initialize params missing %q in %s", want, params)
		}
	}
	if len(transport.notifications) != 1 || transport.notifications[0].Method != "notifications/initialized" {
		t.Fatalf("notifications = %#v", transport.notifications)
	}
	if err := client.EnsureInitialized(context.Background()); err != nil {
		t.Fatal(err)
	}
	if len(transport.requests) != 1 || len(transport.notifications) != 1 {
		t.Fatalf("lifecycle repeated requests=%#v notifications=%#v", transport.requests, transport.notifications)
	}
}

func TestProtocolClientInitializeRejectsUnsupportedVersion(t *testing.T) {
	transport := &fakeLifecycleTransport{}
	client := NewProtocolClient(transport)
	_, err := client.Initialize(context.Background(), InitializeOptions{
		ProtocolVersion:           "2024-11-05",
		SupportedProtocolVersions: []string{"2024-11-05"},
		Capabilities:              map[string]any{},
		ClientInfo:                ImplementationInfo{Name: "test", Version: "1"},
	})
	if err == nil || !strings.Contains(err.Error(), "not supported") {
		t.Fatalf("expected unsupported version error, got %v", err)
	}
	if len(transport.notifications) != 0 {
		t.Fatalf("notifications = %#v", transport.notifications)
	}
}

func TestProtocolClientRecoversExpiredSession(t *testing.T) {
	transport := &fakeSessionRecoveryTransport{}
	client := NewProtocolClient(transport)
	if err := client.EnsureInitialized(context.Background()); err != nil {
		t.Fatal(err)
	}
	tools, err := client.ListTools(context.Background(), "remote")
	if err != nil {
		t.Fatal(err)
	}
	if len(tools) != 1 || tools[0].Name != "ping" {
		t.Fatalf("tools = %#v", tools)
	}
	if transport.resets != 1 {
		t.Fatalf("resets = %d", transport.resets)
	}
	if len(transport.notifications) != 2 {
		t.Fatalf("notifications = %#v", transport.notifications)
	}
	var methods []string
	for _, request := range transport.requests {
		methods = append(methods, request.Method)
	}
	want := []string{"initialize", "tools/list", "initialize", "tools/list"}
	if len(methods) != len(want) {
		t.Fatalf("methods = %#v", methods)
	}
	for i := range want {
		if methods[i] != want[i] {
			t.Fatalf("methods = %#v", methods)
		}
	}
}

func TestProtocolClientRefreshesAuthorizationOnUnauthorized(t *testing.T) {
	transport := &fakeAuthorizationRecoveryTransport{}
	client := NewProtocolClient(transport)
	if err := client.EnsureInitialized(context.Background()); err != nil {
		t.Fatal(err)
	}
	tools, err := client.ListTools(context.Background(), "remote")
	if err != nil {
		t.Fatal(err)
	}
	if len(tools) != 1 || tools[0].Name != "ping" {
		t.Fatalf("tools = %#v", tools)
	}
	if transport.refreshes != 1 || transport.resets != 1 {
		t.Fatalf("refreshes=%d resets=%d", transport.refreshes, transport.resets)
	}
	if len(transport.notifications) != 2 {
		t.Fatalf("notifications = %#v", transport.notifications)
	}
	var methods []string
	for _, request := range transport.requests {
		methods = append(methods, request.Method)
	}
	want := []string{"initialize", "tools/list", "initialize", "tools/list"}
	if len(methods) != len(want) {
		t.Fatalf("methods = %#v", methods)
	}
	for i := range want {
		if methods[i] != want[i] {
			t.Fatalf("methods = %#v", methods)
		}
	}
}

func TestProtocolClientCapturesTransportNotifications(t *testing.T) {
	reader := strings.NewReader(
		`{"jsonrpc":"2.0","method":"notifications/message","params":{"level":"info","data":"hello"}}` + "\n" +
			`{"jsonrpc":"2.0","id":"1","result":{"tools":[]}}` + "\n",
	)
	transport := NewStdioTransport(reader, &bytes.Buffer{})
	client := NewProtocolClient(transport)
	var handled []RPCNotification
	client.SetNotificationHandler(func(notification RPCNotification) {
		handled = append(handled, notification)
	})

	tools, err := client.ListTools(context.Background(), "stdio")
	if err != nil {
		t.Fatal(err)
	}
	if len(tools) != 0 {
		t.Fatalf("tools = %#v", tools)
	}
	notifications := client.Notifications()
	if len(notifications) != 1 || notifications[0].Method != "notifications/message" || !strings.Contains(string(notifications[0].Params), `"hello"`) {
		t.Fatalf("notifications = %#v", notifications)
	}
	if len(handled) != 1 || handled[0].Method != notifications[0].Method {
		t.Fatalf("handled = %#v", handled)
	}
}

func TestProtocolClientHandlesInboundElicitationRequests(t *testing.T) {
	reader := strings.NewReader(
		`{"jsonrpc":"2.0","id":"server-1","method":"elicitation/create","params":{"message":"Confirm?"}}` + "\n" +
			`{"jsonrpc":"2.0","id":"1","result":{"tools":[]}}` + "\n",
	)
	var writer bytes.Buffer
	transport := NewStdioTransport(reader, &writer)
	client := NewProtocolClient(transport)
	client.SetRequestHandler(func(ctx context.Context, request RPCInboundRequest) (any, *RPCError) {
		if request.Method != "elicitation/create" || !strings.Contains(string(request.Params), "Confirm?") {
			t.Fatalf("request = %#v", request)
		}
		return map[string]any{
			"action": "accept",
			"content": map[string]any{
				"confirmed": true,
			},
		}, nil
	})

	if _, err := client.ListTools(context.Background(), "stdio"); err != nil {
		t.Fatal(err)
	}
	lines := strings.Split(strings.TrimSpace(writer.String()), "\n")
	if len(lines) != 2 {
		t.Fatalf("writer lines = %#v", lines)
	}
	if !strings.Contains(lines[1], `"id":"server-1"`) || !strings.Contains(lines[1], `"action":"accept"`) || !strings.Contains(lines[1], `"confirmed":true`) {
		t.Fatalf("elicitation response = %s", lines[1])
	}
}

func TestDefaultRPCRequestHandlerCancelsElicitation(t *testing.T) {
	result, rpcErr := DefaultRPCRequestHandler(context.Background(), RPCInboundRequest{
		ID:     "server-1",
		Method: "elicitation/create",
	})
	if rpcErr != nil {
		t.Fatal(rpcErr)
	}
	if got := result.(map[string]any)["action"]; got != "cancel" {
		t.Fatalf("result = %#v", result)
	}

	_, rpcErr = DefaultRPCRequestHandler(context.Background(), RPCInboundRequest{ID: "server-2", Method: "unknown"})
	if rpcErr == nil || rpcErr.Code != -32601 {
		t.Fatalf("rpcErr = %#v", rpcErr)
	}
}

func TestRPCResponseAcceptsNumericIDs(t *testing.T) {
	var response RPCResponse
	if err := json.Unmarshal([]byte(`{"jsonrpc":"2.0","id":42,"result":{"ok":true}}`), &response); err != nil {
		t.Fatal(err)
	}
	if response.ID != "42" || !strings.Contains(string(response.Result), `"ok":true`) {
		t.Fatalf("response = %#v", response)
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

func TestProtocolClientReadsResourceContentAliases(t *testing.T) {
	single := NewProtocolClient(&fakeRPCTransport{responses: map[string]json.RawMessage{
		"resources/read": json.RawMessage(`{"content":{"uri":"file:///one.txt","mimeType":"text/plain","text":"one"}}`),
	}})
	contents, err := single.ReadResource(context.Background(), "files", "file:///one.txt")
	if err != nil {
		t.Fatal(err)
	}
	if len(contents) != 1 || contents[0].URI != "file:///one.txt" || contents[0].Text != "one" {
		t.Fatalf("single content = %#v", contents)
	}

	wrapped := NewProtocolClient(&fakeRPCTransport{responses: map[string]json.RawMessage{
		"resources/read": json.RawMessage(`{"resourceContents":[{"uri":"file:///two.txt","mimeType":"text/plain","text":"two"}]}`),
	}})
	contents, err = wrapped.ReadResource(context.Background(), "files", "file:///two.txt")
	if err != nil {
		t.Fatal(err)
	}
	if len(contents) != 1 || contents[0].URI != "file:///two.txt" || contents[0].Text != "two" {
		t.Fatalf("wrapped content = %#v", contents)
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
