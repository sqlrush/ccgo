package mcp

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"sync"
	"sync/atomic"

	"ccgo/internal/contracts"
)

const JSONRPCVersion = "2.0"

type RPCRequest struct {
	JSONRPC string `json:"jsonrpc"`
	ID      string `json:"id,omitempty"`
	Method  string `json:"method"`
	Params  any    `json:"params,omitempty"`
}

type RPCResponse struct {
	JSONRPC string          `json:"jsonrpc,omitempty"`
	ID      string          `json:"id,omitempty"`
	Method  string          `json:"method,omitempty"`
	Params  json.RawMessage `json:"params,omitempty"`
	Result  json.RawMessage `json:"result,omitempty"`
	Error   *RPCError       `json:"error,omitempty"`
}

func (r *RPCResponse) UnmarshalJSON(data []byte) error {
	var raw struct {
		JSONRPC string          `json:"jsonrpc,omitempty"`
		ID      json.RawMessage `json:"id,omitempty"`
		Method  string          `json:"method,omitempty"`
		Params  json.RawMessage `json:"params,omitempty"`
		Result  json.RawMessage `json:"result,omitempty"`
		Error   *RPCError       `json:"error,omitempty"`
	}
	if err := json.Unmarshal(data, &raw); err != nil {
		return err
	}
	id, err := decodeRPCID(raw.ID)
	if err != nil {
		return err
	}
	r.JSONRPC = raw.JSONRPC
	r.ID = id
	r.Method = raw.Method
	r.Params = raw.Params
	r.Result = raw.Result
	r.Error = raw.Error
	return nil
}

type RPCError struct {
	Code    int    `json:"code"`
	Message string `json:"message"`
	Data    any    `json:"data,omitempty"`
}

func (e *RPCError) Error() string {
	if e == nil {
		return ""
	}
	if e.Message == "" {
		return fmt.Sprintf("mcp rpc error %d", e.Code)
	}
	return fmt.Sprintf("mcp rpc error %d: %s", e.Code, e.Message)
}

type RPCTransport interface {
	RoundTrip(ctx context.Context, request RPCRequest) (RPCResponse, error)
}

type RPCNotification struct {
	JSONRPC string          `json:"jsonrpc,omitempty"`
	Method  string          `json:"method"`
	Params  json.RawMessage `json:"params,omitempty"`
}

type RPCNotificationHandler func(RPCNotification)

type RPCInboundRequest struct {
	JSONRPC string          `json:"jsonrpc,omitempty"`
	ID      string          `json:"id"`
	Method  string          `json:"method"`
	Params  json.RawMessage `json:"params,omitempty"`
}

type RPCRequestHandler func(context.Context, RPCInboundRequest) (any, *RPCError)

type RPCNotificationTransport interface {
	SetNotificationHandler(RPCNotificationHandler)
}

type RPCRequestTransport interface {
	SetRequestHandler(RPCRequestHandler)
}

type ProtocolClient struct {
	Transport RPCTransport
	nextID    atomic.Uint64

	notificationMu      sync.Mutex
	notifications       []RPCNotification
	notificationHandler RPCNotificationHandler
	requestHandler      RPCRequestHandler
}

func NewProtocolClient(transport RPCTransport) *ProtocolClient {
	client := &ProtocolClient{Transport: transport}
	if transport, ok := transport.(RPCNotificationTransport); ok {
		transport.SetNotificationHandler(client.handleNotification)
	}
	if transport, ok := transport.(RPCRequestTransport); ok {
		transport.SetRequestHandler(client.handleRequest)
	}
	return client
}

func NewRPCRequest(id string, method string, params any) RPCRequest {
	return RPCRequest{
		JSONRPC: JSONRPCVersion,
		ID:      id,
		Method:  method,
		Params:  params,
	}
}

func (c *ProtocolClient) ListTools(ctx context.Context, serverName string) ([]RemoteTool, error) {
	raw, err := c.request(ctx, "tools/list", nil)
	if err != nil {
		return nil, err
	}
	var response struct {
		Tools []rpcTool `json:"tools"`
	}
	if err := json.Unmarshal(raw, &response); err != nil {
		return nil, err
	}
	tools := make([]RemoteTool, 0, len(response.Tools))
	for _, item := range response.Tools {
		tools = append(tools, item.remoteTool())
	}
	return tools, nil
}

func (c *ProtocolClient) CallTool(ctx context.Context, serverName string, toolName string, input json.RawMessage) (any, error) {
	arguments, err := rawObject(input)
	if err != nil {
		return nil, err
	}
	raw, err := c.request(ctx, "tools/call", map[string]any{
		"name":      toolName,
		"arguments": arguments,
	})
	if err != nil {
		return nil, err
	}
	var result map[string]any
	if err := json.Unmarshal(raw, &result); err != nil {
		return nil, err
	}
	return result, nil
}

func (c *ProtocolClient) ListResources(ctx context.Context, serverName string) ([]RemoteResource, error) {
	raw, err := c.request(ctx, "resources/list", nil)
	if err != nil {
		return nil, err
	}
	var response struct {
		Resources []rpcResource `json:"resources"`
	}
	if err := json.Unmarshal(raw, &response); err != nil {
		return nil, err
	}
	resources := make([]RemoteResource, 0, len(response.Resources))
	for _, item := range response.Resources {
		resources = append(resources, item.remoteResource())
	}
	return resources, nil
}

func (c *ProtocolClient) ReadResource(ctx context.Context, serverName string, uri string) ([]ResourceContent, error) {
	raw, err := c.request(ctx, "resources/read", map[string]any{"uri": uri})
	if err != nil {
		return nil, err
	}
	var response struct {
		Contents []rpcResourceContent `json:"contents"`
	}
	if err := json.Unmarshal(raw, &response); err != nil {
		return nil, err
	}
	contents := make([]ResourceContent, 0, len(response.Contents))
	for _, item := range response.Contents {
		contents = append(contents, item.resourceContent())
	}
	return contents, nil
}

func (c *ProtocolClient) ListPrompts(ctx context.Context, serverName string) ([]RemotePrompt, error) {
	raw, err := c.request(ctx, "prompts/list", nil)
	if err != nil {
		return nil, err
	}
	var response struct {
		Prompts []rpcPrompt `json:"prompts"`
	}
	if err := json.Unmarshal(raw, &response); err != nil {
		return nil, err
	}
	prompts := make([]RemotePrompt, 0, len(response.Prompts))
	for _, item := range response.Prompts {
		prompts = append(prompts, item.remotePrompt())
	}
	return prompts, nil
}

func (c *ProtocolClient) GetPrompt(ctx context.Context, serverName string, promptName string, arguments map[string]string) (PromptResult, error) {
	raw, err := c.request(ctx, "prompts/get", map[string]any{
		"name":      promptName,
		"arguments": arguments,
	})
	if err != nil {
		return PromptResult{}, err
	}
	var response rpcPromptResult
	if err := json.Unmarshal(raw, &response); err != nil {
		return PromptResult{}, err
	}
	return response.promptResult(), nil
}

func (c *ProtocolClient) request(ctx context.Context, method string, params any) (json.RawMessage, error) {
	if c == nil || c.Transport == nil {
		return nil, fmt.Errorf("mcp rpc transport is nil")
	}
	request := NewRPCRequest(fmt.Sprintf("%d", c.nextID.Add(1)), method, params)
	response, err := c.Transport.RoundTrip(ctx, request)
	if err != nil {
		return nil, err
	}
	if response.Error != nil {
		return nil, response.Error
	}
	if response.ID != "" && response.ID != request.ID {
		return nil, fmt.Errorf("mcp rpc response id mismatch: got %q, want %q", response.ID, request.ID)
	}
	if len(response.Result) == 0 {
		return json.RawMessage(`null`), nil
	}
	return response.Result, nil
}

func (c *ProtocolClient) SetNotificationHandler(handler RPCNotificationHandler) {
	if c == nil {
		return
	}
	c.notificationMu.Lock()
	c.notificationHandler = handler
	c.notificationMu.Unlock()
}

func (c *ProtocolClient) Notifications() []RPCNotification {
	if c == nil {
		return nil
	}
	c.notificationMu.Lock()
	defer c.notificationMu.Unlock()
	return append([]RPCNotification(nil), c.notifications...)
}

func (c *ProtocolClient) SetRequestHandler(handler RPCRequestHandler) {
	if c == nil {
		return
	}
	c.notificationMu.Lock()
	c.requestHandler = handler
	c.notificationMu.Unlock()
}

func (c *ProtocolClient) handleNotification(notification RPCNotification) {
	c.notificationMu.Lock()
	c.notifications = append(c.notifications, notification)
	handler := c.notificationHandler
	c.notificationMu.Unlock()
	if handler != nil {
		handler(notification)
	}
}

func (c *ProtocolClient) handleRequest(ctx context.Context, request RPCInboundRequest) (any, *RPCError) {
	c.notificationMu.Lock()
	handler := c.requestHandler
	c.notificationMu.Unlock()
	if handler != nil {
		return handler(ctx, request)
	}
	return DefaultRPCRequestHandler(ctx, request)
}

func NotificationFromRPCResponse(response RPCResponse) (RPCNotification, bool) {
	if strings.TrimSpace(response.Method) == "" || strings.TrimSpace(response.ID) != "" {
		return RPCNotification{}, false
	}
	return RPCNotification{
		JSONRPC: response.JSONRPC,
		Method:  response.Method,
		Params:  append(json.RawMessage(nil), response.Params...),
	}, true
}

func InboundRequestFromRPCResponse(response RPCResponse) (RPCInboundRequest, bool) {
	if strings.TrimSpace(response.Method) == "" || strings.TrimSpace(response.ID) == "" {
		return RPCInboundRequest{}, false
	}
	return RPCInboundRequest{
		JSONRPC: response.JSONRPC,
		ID:      response.ID,
		Method:  response.Method,
		Params:  append(json.RawMessage(nil), response.Params...),
	}, true
}

func ResponseForInboundRequest(ctx context.Context, request RPCInboundRequest, handler RPCRequestHandler) RPCResponse {
	if handler == nil {
		handler = DefaultRPCRequestHandler
	}
	result, rpcErr := handler(ctx, request)
	if rpcErr != nil {
		return RPCResponse{JSONRPC: JSONRPCVersion, ID: request.ID, Error: rpcErr}
	}
	raw, err := json.Marshal(result)
	if err != nil {
		return RPCResponse{
			JSONRPC: JSONRPCVersion,
			ID:      request.ID,
			Error: &RPCError{
				Code:    -32603,
				Message: "failed to encode MCP client response",
				Data:    err.Error(),
			},
		}
	}
	return RPCResponse{JSONRPC: JSONRPCVersion, ID: request.ID, Result: raw}
}

func DefaultRPCRequestHandler(_ context.Context, request RPCInboundRequest) (any, *RPCError) {
	if request.Method == "elicitation/create" {
		return map[string]any{"action": "cancel"}, nil
	}
	return nil, &RPCError{Code: -32601, Message: "method not found"}
}

func decodeRPCID(raw json.RawMessage) (string, error) {
	trimmed := strings.TrimSpace(string(raw))
	if trimmed == "" || trimmed == "null" {
		return "", nil
	}
	if strings.HasPrefix(trimmed, `"`) {
		var id string
		if err := json.Unmarshal(raw, &id); err != nil {
			return "", err
		}
		return id, nil
	}
	return trimmed, nil
}

func IsSessionExpiredError(err error) bool {
	if err == nil {
		return false
	}
	var rpcErr *RPCError
	if !errors.As(err, &rpcErr) || rpcErr == nil {
		return false
	}
	text := strings.ToLower(rpcErr.Message)
	if strings.Contains(text, "session expired") || strings.Contains(text, "session-expired") {
		return true
	}
	if data, ok := rpcErr.Data.(map[string]any); ok {
		for _, key := range []string{"reason", "code", "type"} {
			if strings.Contains(strings.ToLower(fmt.Sprint(data[key])), "session") && strings.Contains(strings.ToLower(fmt.Sprint(data[key])), "expired") {
				return true
			}
		}
	}
	return false
}

type rpcTool struct {
	Name             string               `json:"name"`
	Description      string               `json:"description"`
	InputSchema      contracts.JSONSchema `json:"inputSchema"`
	InputSchemaSnake contracts.JSONSchema `json:"input_schema"`
	ReadOnly         bool                 `json:"readOnly"`
	ReadOnlySnake    bool                 `json:"read_only"`
}

func (t rpcTool) remoteTool() RemoteTool {
	schema := t.InputSchema
	if schema == nil {
		schema = t.InputSchemaSnake
	}
	return RemoteTool{
		Name:        t.Name,
		Description: t.Description,
		InputSchema: schema,
		ReadOnly:    t.ReadOnly || t.ReadOnlySnake,
	}
}

type rpcResource struct {
	URI         string `json:"uri"`
	Name        string `json:"name"`
	Description string `json:"description"`
	MimeType    string `json:"mimeType"`
	MimeTypeAlt string `json:"mime_type"`
}

func (r rpcResource) remoteResource() RemoteResource {
	mimeType := r.MimeType
	if mimeType == "" {
		mimeType = r.MimeTypeAlt
	}
	return RemoteResource{
		URI:         r.URI,
		Name:        r.Name,
		Description: r.Description,
		MimeType:    mimeType,
	}
}

type rpcResourceContent struct {
	URI         string `json:"uri"`
	MimeType    string `json:"mimeType"`
	MimeTypeAlt string `json:"mime_type"`
	Text        string `json:"text"`
	Blob        string `json:"blob"`
}

func (c rpcResourceContent) resourceContent() ResourceContent {
	mimeType := c.MimeType
	if mimeType == "" {
		mimeType = c.MimeTypeAlt
	}
	return ResourceContent{
		URI:      c.URI,
		MimeType: mimeType,
		Text:     c.Text,
		Blob:     c.Blob,
	}
}

type rpcPrompt struct {
	Name        string              `json:"name"`
	Description string              `json:"description"`
	Arguments   []rpcPromptArgument `json:"arguments"`
}

func (p rpcPrompt) remotePrompt() RemotePrompt {
	arguments := make([]PromptArgument, 0, len(p.Arguments))
	for _, argument := range p.Arguments {
		arguments = append(arguments, argument.promptArgument())
	}
	return RemotePrompt{
		Name:        p.Name,
		Description: p.Description,
		Arguments:   arguments,
	}
}

type rpcPromptArgument struct {
	Name        string `json:"name"`
	Description string `json:"description"`
	Required    bool   `json:"required"`
}

func (a rpcPromptArgument) promptArgument() PromptArgument {
	return PromptArgument{
		Name:        a.Name,
		Description: a.Description,
		Required:    a.Required,
	}
}

type rpcPromptResult struct {
	Description string             `json:"description"`
	Messages    []rpcPromptMessage `json:"messages"`
}

func (r rpcPromptResult) promptResult() PromptResult {
	messages := make([]PromptMessage, 0, len(r.Messages))
	for _, message := range r.Messages {
		messages = append(messages, message.promptMessage())
	}
	return PromptResult{
		Description: r.Description,
		Messages:    messages,
	}
}

type rpcPromptMessage struct {
	Role    string          `json:"role"`
	Content json.RawMessage `json:"content"`
}

func (m rpcPromptMessage) promptMessage() PromptMessage {
	var content any
	if len(m.Content) > 0 {
		_ = json.Unmarshal(m.Content, &content)
	}
	return PromptMessage{
		Role:    m.Role,
		Content: content,
	}
}

func rawObject(raw json.RawMessage) (map[string]any, error) {
	if len(strings.TrimSpace(string(raw))) == 0 {
		return map[string]any{}, nil
	}
	var out map[string]any
	if err := json.Unmarshal(raw, &out); err != nil {
		return nil, err
	}
	if out == nil {
		out = map[string]any{}
	}
	return out, nil
}
