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
const DefaultProtocolVersion = "2025-06-18"
const maxListPaginationPages = 100

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

type RPCNotificationSender interface {
	SendNotification(context.Context, RPCNotification) error
}

type RPCSessionResetter interface {
	ResetSession()
}

type RPCAuthorizationRefresher interface {
	RefreshAuthorization(context.Context) (bool, error)
}

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

	initMu           sync.Mutex
	initialized      bool
	initializeResult InitializeResult

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

type ImplementationInfo struct {
	Name    string `json:"name,omitempty"`
	Title   string `json:"title,omitempty"`
	Version string `json:"version,omitempty"`
}

type InitializeOptions struct {
	ProtocolVersion             string
	SupportedProtocolVersions   []string
	Capabilities                map[string]any
	ClientInfo                  ImplementationInfo
	SkipInitializedNotification bool
}

type InitializeResult struct {
	ProtocolVersion string             `json:"protocolVersion"`
	Capabilities    map[string]any     `json:"capabilities,omitempty"`
	ServerInfo      ImplementationInfo `json:"serverInfo,omitempty"`
	Instructions    string             `json:"instructions,omitempty"`
}

func DefaultInitializeOptions() InitializeOptions {
	return InitializeOptions{
		ProtocolVersion:           DefaultProtocolVersion,
		SupportedProtocolVersions: []string{DefaultProtocolVersion},
		Capabilities: map[string]any{
			"elicitation": map[string]any{},
		},
		ClientInfo: ImplementationInfo{
			Name:    "ccgo",
			Title:   "ccgo",
			Version: "0.0.0-dev",
		},
	}
}

func (c *ProtocolClient) EnsureInitialized(ctx context.Context) error {
	_, err := c.Initialize(ctx, DefaultInitializeOptions())
	return err
}

func (c *ProtocolClient) Initialize(ctx context.Context, options InitializeOptions) (InitializeResult, error) {
	if c == nil {
		return InitializeResult{}, fmt.Errorf("mcp protocol client is nil")
	}
	c.initMu.Lock()
	defer c.initMu.Unlock()
	if c.initialized {
		return c.initializeResult, nil
	}
	options = normalizeInitializeOptions(options)
	for attempt := 0; attempt < 2; attempt++ {
		raw, err := c.rawRequest(ctx, "initialize", map[string]any{
			"protocolVersion": options.ProtocolVersion,
			"capabilities":    options.Capabilities,
			"clientInfo":      options.ClientInfo,
		})
		if err != nil {
			if attempt == 0 && IsUnauthorizedError(err) {
				if recoverErr := c.refreshAuthorizationLocked(ctx); recoverErr != nil {
					return InitializeResult{}, fmt.Errorf("%w; authorization refresh failed: %v", err, recoverErr)
				}
				continue
			}
			return InitializeResult{}, err
		}
		var result InitializeResult
		if err := json.Unmarshal(raw, &result); err != nil {
			return InitializeResult{}, err
		}
		if !supportsProtocolVersion(result.ProtocolVersion, options.SupportedProtocolVersions) {
			return InitializeResult{}, fmt.Errorf("mcp server protocol version %q is not supported", result.ProtocolVersion)
		}
		if !options.SkipInitializedNotification {
			if err := c.sendInitialized(ctx); err != nil {
				if attempt == 0 && IsUnauthorizedError(err) {
					if recoverErr := c.refreshAuthorizationLocked(ctx); recoverErr != nil {
						return InitializeResult{}, fmt.Errorf("%w; authorization refresh failed: %v", err, recoverErr)
					}
					continue
				}
				return InitializeResult{}, err
			}
		}
		c.initialized = true
		c.initializeResult = result
		return result, nil
	}
	return InitializeResult{}, fmt.Errorf("mcp initialize authorization retry exhausted")
}

func (c *ProtocolClient) ListTools(ctx context.Context, serverName string) ([]RemoteTool, error) {
	var tools []RemoteTool
	cursor := ""
	seen := map[string]bool{}
	for page := 0; page < maxListPaginationPages; page++ {
		raw, err := c.request(ctx, "tools/list", listPaginationParams(cursor))
		if err != nil {
			return nil, err
		}
		var response struct {
			Tools           []rpcTool `json:"tools"`
			NextCursor      string    `json:"nextCursor"`
			NextCursorSnake string    `json:"next_cursor"`
			Cursor          string    `json:"cursor"`
		}
		if err := json.Unmarshal(raw, &response); err != nil {
			return nil, err
		}
		for _, item := range response.Tools {
			tools = append(tools, item.remoteTool())
		}
		nextCursor := listResponseCursor(response.NextCursor, response.NextCursorSnake, response.Cursor)
		if nextCursor == "" {
			return tools, nil
		}
		if seen[nextCursor] {
			return nil, fmt.Errorf("mcp tools/list pagination repeated cursor %q", nextCursor)
		}
		seen[nextCursor] = true
		cursor = nextCursor
	}
	return nil, fmt.Errorf("mcp tools/list pagination exceeded %d pages", maxListPaginationPages)
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
	var resources []RemoteResource
	cursor := ""
	seen := map[string]bool{}
	for page := 0; page < maxListPaginationPages; page++ {
		raw, err := c.request(ctx, "resources/list", listPaginationParams(cursor))
		if err != nil {
			return nil, err
		}
		var response struct {
			Resources       []rpcResource `json:"resources"`
			NextCursor      string        `json:"nextCursor"`
			NextCursorSnake string        `json:"next_cursor"`
			Cursor          string        `json:"cursor"`
		}
		if err := json.Unmarshal(raw, &response); err != nil {
			return nil, err
		}
		for _, item := range response.Resources {
			resources = append(resources, item.remoteResource())
		}
		nextCursor := listResponseCursor(response.NextCursor, response.NextCursorSnake, response.Cursor)
		if nextCursor == "" {
			return resources, nil
		}
		if seen[nextCursor] {
			return nil, fmt.Errorf("mcp resources/list pagination repeated cursor %q", nextCursor)
		}
		seen[nextCursor] = true
		cursor = nextCursor
	}
	return nil, fmt.Errorf("mcp resources/list pagination exceeded %d pages", maxListPaginationPages)
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
	var prompts []RemotePrompt
	cursor := ""
	seen := map[string]bool{}
	for page := 0; page < maxListPaginationPages; page++ {
		raw, err := c.request(ctx, "prompts/list", listPaginationParams(cursor))
		if err != nil {
			return nil, err
		}
		var response struct {
			Prompts         []rpcPrompt `json:"prompts"`
			NextCursor      string      `json:"nextCursor"`
			NextCursorSnake string      `json:"next_cursor"`
			Cursor          string      `json:"cursor"`
		}
		if err := json.Unmarshal(raw, &response); err != nil {
			return nil, err
		}
		for _, item := range response.Prompts {
			prompts = append(prompts, item.remotePrompt())
		}
		nextCursor := listResponseCursor(response.NextCursor, response.NextCursorSnake, response.Cursor)
		if nextCursor == "" {
			return prompts, nil
		}
		if seen[nextCursor] {
			return nil, fmt.Errorf("mcp prompts/list pagination repeated cursor %q", nextCursor)
		}
		seen[nextCursor] = true
		cursor = nextCursor
	}
	return nil, fmt.Errorf("mcp prompts/list pagination exceeded %d pages", maxListPaginationPages)
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

func listPaginationParams(cursor string) any {
	if cursor == "" {
		return nil
	}
	return map[string]any{"cursor": cursor}
}

func listResponseCursor(values ...string) string {
	for _, value := range values {
		if strings.TrimSpace(value) != "" {
			return value
		}
	}
	return ""
}

func (c *ProtocolClient) request(ctx context.Context, method string, params any) (json.RawMessage, error) {
	raw, err := c.rawRequest(ctx, method, params)
	switch {
	case IsSessionExpiredError(err):
		if recoverErr := c.recoverExpiredSession(ctx); recoverErr != nil {
			return nil, fmt.Errorf("%w; session recovery failed: %v", err, recoverErr)
		}
		return c.rawRequest(ctx, method, params)
	case IsUnauthorizedError(err):
		if recoverErr := c.recoverAuthorization(ctx); recoverErr != nil {
			return nil, fmt.Errorf("%w; authorization refresh failed: %v", err, recoverErr)
		}
		return c.rawRequest(ctx, method, params)
	default:
		return raw, err
	}
}

func (c *ProtocolClient) rawRequest(ctx context.Context, method string, params any) (json.RawMessage, error) {
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

func (c *ProtocolClient) sendInitialized(ctx context.Context) error {
	sender, ok := c.Transport.(RPCNotificationSender)
	if !ok {
		return fmt.Errorf("mcp rpc transport cannot send initialized notification")
	}
	return sender.SendNotification(ctx, RPCNotification{JSONRPC: JSONRPCVersion, Method: "notifications/initialized"})
}

func (c *ProtocolClient) recoverExpiredSession(ctx context.Context) error {
	resetter, ok := c.Transport.(RPCSessionResetter)
	if !ok {
		return fmt.Errorf("mcp rpc transport cannot reset expired session")
	}
	resetter.ResetSession()
	c.initMu.Lock()
	c.initialized = false
	c.initializeResult = InitializeResult{}
	c.initMu.Unlock()
	return c.EnsureInitialized(ctx)
}

func (c *ProtocolClient) recoverAuthorization(ctx context.Context) error {
	c.initMu.Lock()
	err := c.refreshAuthorizationLocked(ctx)
	c.initMu.Unlock()
	if err != nil {
		return err
	}
	return c.EnsureInitialized(ctx)
}

func (c *ProtocolClient) refreshAuthorizationLocked(ctx context.Context) error {
	refresher, ok := c.Transport.(RPCAuthorizationRefresher)
	if !ok {
		return fmt.Errorf("mcp rpc transport cannot refresh authorization")
	}
	refreshed, err := refresher.RefreshAuthorization(ctx)
	if err != nil {
		return err
	}
	if !refreshed {
		return fmt.Errorf("mcp rpc transport authorization refresh is not configured")
	}
	if resetter, ok := c.Transport.(RPCSessionResetter); ok {
		resetter.ResetSession()
	}
	c.initialized = false
	c.initializeResult = InitializeResult{}
	return nil
}

func normalizeInitializeOptions(options InitializeOptions) InitializeOptions {
	defaults := DefaultInitializeOptions()
	if options.ProtocolVersion == "" {
		options.ProtocolVersion = defaults.ProtocolVersion
	}
	if len(options.SupportedProtocolVersions) == 0 {
		options.SupportedProtocolVersions = append([]string(nil), defaults.SupportedProtocolVersions...)
	}
	if options.Capabilities == nil {
		options.Capabilities = defaults.Capabilities
	}
	if options.ClientInfo.Name == "" {
		options.ClientInfo.Name = defaults.ClientInfo.Name
	}
	if options.ClientInfo.Title == "" {
		options.ClientInfo.Title = defaults.ClientInfo.Title
	}
	if options.ClientInfo.Version == "" {
		options.ClientInfo.Version = defaults.ClientInfo.Version
	}
	return options
}

func supportsProtocolVersion(version string, supported []string) bool {
	for _, item := range supported {
		if version == item {
			return true
		}
	}
	return false
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
	Name              string               `json:"name"`
	Description       string               `json:"description"`
	InputSchema       contracts.JSONSchema `json:"inputSchema"`
	InputSchemaSnake  contracts.JSONSchema `json:"input_schema"`
	OutputSchema      contracts.JSONSchema `json:"outputSchema"`
	OutputSchemaSnake contracts.JSONSchema `json:"output_schema"`
	ReadOnly          bool                 `json:"readOnly"`
	ReadOnlySnake     bool                 `json:"read_only"`
	Annotations       rpcToolAnnotations   `json:"annotations"`
}

type rpcToolAnnotations struct {
	ReadOnlyHint    bool `json:"readOnlyHint"`
	DestructiveHint bool `json:"destructiveHint"`
}

func (t rpcTool) remoteTool() RemoteTool {
	schema := t.InputSchema
	if schema == nil {
		schema = t.InputSchemaSnake
	}
	outputSchema := t.OutputSchema
	if outputSchema == nil {
		outputSchema = t.OutputSchemaSnake
	}
	return RemoteTool{
		Name:         t.Name,
		Description:  t.Description,
		InputSchema:  schema,
		OutputSchema: outputSchema,
		ReadOnly:     t.ReadOnly || t.ReadOnlySnake || t.Annotations.ReadOnlyHint,
		Destructive:  t.Annotations.DestructiveHint,
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
