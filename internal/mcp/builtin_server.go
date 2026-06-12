package mcp

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"strings"
	"sync"

	"ccgo/internal/contracts"
	"ccgo/internal/tool"
)

const builtinServerName = "ccgo-mcp"
const builtinServerVersion = "0.0.0-dev"
const defaultMCPServerScanLimit = 10 * 1024 * 1024

type BuiltinServerOptions struct {
	Registry            *tool.Registry
	Executor            tool.Executor
	WorkingDirectory    string
	AllowMutatingTools  bool
	PromptContext       tool.PromptContext
	ToolContextMetadata map[string]any
}

type BuiltinServer struct {
	registry            *tool.Registry
	executor            tool.Executor
	workingDirectory    string
	allowMutatingTools  bool
	promptContext       tool.PromptContext
	toolContextMetadata map[string]any

	mu                 sync.Mutex
	initializeAccepted bool
	initialized        bool
	cancelled          map[string]string
}

type serverRPCRequest struct {
	JSONRPC string          `json:"jsonrpc,omitempty"`
	ID      json.RawMessage `json:"id,omitempty"`
	Method  string          `json:"method"`
	Params  json.RawMessage `json:"params,omitempty"`
}

type serverRPCResponse struct {
	JSONRPC string          `json:"jsonrpc"`
	ID      json.RawMessage `json:"id"`
	Result  any             `json:"result,omitempty"`
	Error   *RPCError       `json:"error,omitempty"`
}

type toolCallParams struct {
	Name      string          `json:"name"`
	Arguments json.RawMessage `json:"arguments"`
}

func NewBuiltinServer(options BuiltinServerOptions) (*BuiltinServer, error) {
	registry := options.Registry
	executor := options.Executor
	if registry == nil {
		registry = executor.Registry
	}
	if registry == nil {
		return nil, fmt.Errorf("mcp builtin server registry is required")
	}
	if executor.Registry == nil {
		executor.Registry = registry
	}
	promptContext := options.PromptContext
	if promptContext.WorkingDirectory == "" {
		promptContext.WorkingDirectory = options.WorkingDirectory
	}
	return &BuiltinServer{
		registry:            registry,
		executor:            executor,
		workingDirectory:    strings.TrimSpace(options.WorkingDirectory),
		allowMutatingTools:  options.AllowMutatingTools,
		promptContext:       promptContext,
		toolContextMetadata: cloneAnyMap(options.ToolContextMetadata),
		cancelled:           map[string]string{},
	}, nil
}

func (s *BuiltinServer) Run(ctx context.Context, in io.Reader, out io.Writer) error {
	if s == nil {
		return fmt.Errorf("mcp builtin server is nil")
	}
	if in == nil {
		return fmt.Errorf("mcp builtin server input is nil")
	}
	if out == nil {
		return fmt.Errorf("mcp builtin server output is nil")
	}
	scanner := bufio.NewScanner(in)
	scanner.Buffer(make([]byte, 0, 64*1024), defaultMCPServerScanLimit)
	encoder := json.NewEncoder(out)
	for scanner.Scan() {
		if err := ctx.Err(); err != nil {
			return err
		}
		line := strings.TrimSpace(scanner.Text())
		if line == "" {
			continue
		}
		response, ok := s.handleLine(ctx, []byte(line))
		if !ok {
			continue
		}
		if err := encoder.Encode(response); err != nil {
			return err
		}
	}
	if err := scanner.Err(); err != nil {
		return err
	}
	return nil
}

func (s *BuiltinServer) handleLine(ctx context.Context, data []byte) (any, bool) {
	if firstNonWhitespace(data) == '[' {
		return s.handleBatch(ctx, data)
	}
	return s.handleSingle(ctx, data)
}

func (s *BuiltinServer) handleBatch(ctx context.Context, data []byte) (any, bool) {
	var batch []json.RawMessage
	if err := json.Unmarshal(data, &batch); err != nil {
		return parseErrorResponse(err), true
	}
	if len(batch) == 0 {
		return invalidRequestResponse(nil, "invalid request"), true
	}
	responses := make([]serverRPCResponse, 0, len(batch))
	for _, item := range batch {
		response, ok := s.handleSingle(ctx, item)
		if ok {
			responses = append(responses, response)
		}
	}
	if len(responses) == 0 {
		return nil, false
	}
	return responses, true
}

func (s *BuiltinServer) handleSingle(ctx context.Context, data []byte) (serverRPCResponse, bool) {
	var request serverRPCRequest
	if rpcErr := decodeServerRPCRequest(data, &request); rpcErr != nil {
		return serverRPCResponse{JSONRPC: JSONRPCVersion, ID: normalizedResponseID(request.ID), Error: rpcErr}, true
	}
	if !request.HasID() {
		_ = s.handleNotification(ctx, request)
		return serverRPCResponse{}, false
	}
	if rpcErr := s.lifecycleError(request.Method); rpcErr != nil {
		return serverRPCResponse{JSONRPC: JSONRPCVersion, ID: normalizedResponseID(request.ID), Error: rpcErr}, true
	}
	if reason, ok := s.consumeCancellation(request.ID); ok {
		return serverRPCResponse{
			JSONRPC: JSONRPCVersion,
			ID:      normalizedResponseID(request.ID),
			Error: &RPCError{
				Code:    -32800,
				Message: "request cancelled",
				Data:    map[string]any{"reason": reason},
			},
		}, true
	}
	result, rpcErr := s.handleRequest(ctx, request)
	response := serverRPCResponse{JSONRPC: JSONRPCVersion, ID: normalizedResponseID(request.ID)}
	if rpcErr != nil {
		response.Error = rpcErr
		return response, true
	}
	response.Result = result
	if request.Method == "initialize" {
		s.markInitializeAccepted()
	}
	return response, true
}

func decodeServerRPCRequest(data []byte, request *serverRPCRequest) *RPCError {
	var object map[string]json.RawMessage
	if err := json.Unmarshal(data, &object); err != nil {
		if len(object) == 0 && firstNonWhitespace(data) != '{' {
			return &RPCError{Code: -32600, Message: "invalid request", Data: err.Error()}
		}
		return &RPCError{Code: -32700, Message: "parse error", Data: err.Error()}
	}
	if object == nil {
		return &RPCError{Code: -32600, Message: "invalid request"}
	}
	if rawID, ok := object["id"]; ok {
		request.ID = append(json.RawMessage(nil), rawID...)
	}
	if err := json.Unmarshal(data, request); err != nil {
		return &RPCError{Code: -32600, Message: "invalid request", Data: err.Error()}
	}
	if strings.TrimSpace(request.Method) == "" {
		return &RPCError{Code: -32600, Message: "invalid request"}
	}
	return nil
}

func parseErrorResponse(err error) serverRPCResponse {
	return serverRPCResponse{JSONRPC: JSONRPCVersion, ID: json.RawMessage(`null`), Error: &RPCError{Code: -32700, Message: "parse error", Data: err.Error()}}
}

func invalidRequestResponse(id json.RawMessage, message string) serverRPCResponse {
	return serverRPCResponse{JSONRPC: JSONRPCVersion, ID: normalizedResponseID(id), Error: &RPCError{Code: -32600, Message: message}}
}

func (s *BuiltinServer) handleNotification(_ context.Context, request serverRPCRequest) error {
	switch request.Method {
	case "notifications/initialized":
		s.markInitialized()
	case "$/cancelRequest", "notifications/cancelled":
		s.recordCancellation(request.Params)
	}
	return nil
}

func (s *BuiltinServer) lifecycleError(method string) *RPCError {
	switch method {
	case "initialize":
		s.mu.Lock()
		alreadyAccepted := s.initializeAccepted
		s.mu.Unlock()
		if alreadyAccepted {
			return &RPCError{Code: -32600, Message: "server already initialized"}
		}
		return nil
	case "ping":
		return nil
	}
	s.mu.Lock()
	initialized := s.initialized
	s.mu.Unlock()
	if !initialized {
		return &RPCError{Code: -32002, Message: "server not initialized"}
	}
	return nil
}

func (s *BuiltinServer) markInitializeAccepted() {
	s.mu.Lock()
	s.initializeAccepted = true
	s.mu.Unlock()
}

func (s *BuiltinServer) markInitialized() {
	s.mu.Lock()
	if s.initializeAccepted {
		s.initialized = true
	}
	s.mu.Unlock()
}

func (s *BuiltinServer) handleRequest(ctx context.Context, request serverRPCRequest) (any, *RPCError) {
	switch request.Method {
	case "initialize":
		return map[string]any{
			"protocolVersion": DefaultProtocolVersion,
			"capabilities": map[string]any{
				"tools":       map[string]any{},
				"resources":   map[string]any{},
				"prompts":     map[string]any{},
				"completions": map[string]any{},
			},
			"serverInfo": map[string]any{
				"name":    builtinServerName,
				"title":   "ccgo MCP",
				"version": builtinServerVersion,
			},
		}, nil
	case "ping":
		return map[string]any{}, nil
	case "tools/list":
		return s.listTools()
	case "tools/call":
		return s.callTool(ctx, request.Params)
	case "resources/list":
		return map[string]any{"resources": []any{}}, nil
	case "resources/templates/list":
		return map[string]any{"resourceTemplates": []any{}}, nil
	case "resources/read":
		return nil, validateNamedParams(request.Params, "uri", "resource not found")
	case "prompts/list":
		return map[string]any{"prompts": []any{}}, nil
	case "prompts/get":
		return nil, validateNamedParams(request.Params, "name", "prompt not found")
	case "completion/complete":
		return emptyCompletionResult(request.Params)
	case "logging/setLevel":
		if rpcErr := validateLoggingSetLevel(request.Params); rpcErr != nil {
			return nil, rpcErr
		}
		return map[string]any{}, nil
	default:
		return nil, &RPCError{Code: -32601, Message: "method not found"}
	}
}

func (s *BuiltinServer) listTools() (any, *RPCError) {
	definitions, err := s.registry.Definitions(s.promptContext)
	if err != nil {
		return nil, &RPCError{Code: -32603, Message: "list tools failed", Data: err.Error()}
	}
	tools := make([]map[string]any, 0, len(definitions))
	for _, definition := range definitions {
		inputSchema := definition.InputSchema
		if inputSchema == nil {
			inputSchema = contracts.JSONSchema{"type": "object"}
		}
		annotations := map[string]any{
			"readOnlyHint":    definition.ReadOnly,
			"destructiveHint": definition.Destructive,
		}
		toolInfo := map[string]any{
			"name":        definition.Name,
			"description": definition.Description,
			"inputSchema": inputSchema,
			"annotations": annotations,
			"readOnly":    definition.ReadOnly,
		}
		if definition.OutputSchema != nil {
			toolInfo["outputSchema"] = definition.OutputSchema
		}
		tools = append(tools, toolInfo)
	}
	return map[string]any{"tools": tools}, nil
}

func (s *BuiltinServer) callTool(ctx context.Context, raw json.RawMessage) (any, *RPCError) {
	var params toolCallParams
	if len(strings.TrimSpace(string(raw))) == 0 {
		return nil, &RPCError{Code: -32602, Message: "tools/call params are required"}
	}
	if err := json.Unmarshal(raw, &params); err != nil {
		return nil, &RPCError{Code: -32602, Message: "invalid tools/call params", Data: err.Error()}
	}
	params.Name = strings.TrimSpace(params.Name)
	if params.Name == "" {
		return nil, &RPCError{Code: -32602, Message: "tool name is required"}
	}
	if len(params.Arguments) == 0 {
		params.Arguments = json.RawMessage(`{}`)
	}
	use := contracts.ToolUse{
		ID:    contracts.ID("mcp-" + params.Name),
		Name:  params.Name,
		Input: params.Arguments,
	}
	result, err := s.executor.Execute(s.toolContext(ctx), use, tool.NopProgressSink())
	if err != nil && errors.Is(err, tool.ErrUnknownTool) {
		return nil, &RPCError{Code: -32602, Message: err.Error()}
	}
	return toolResultToMCPCallResult(result, err), nil
}

func validateNamedParams(raw json.RawMessage, required string, notFoundMessage string) *RPCError {
	if len(strings.TrimSpace(string(raw))) == 0 {
		return &RPCError{Code: -32602, Message: required + " is required"}
	}
	var params map[string]any
	if err := json.Unmarshal(raw, &params); err != nil {
		return &RPCError{Code: -32602, Message: "invalid params", Data: err.Error()}
	}
	value, _ := params[required].(string)
	if strings.TrimSpace(value) == "" {
		return &RPCError{Code: -32602, Message: required + " is required"}
	}
	return &RPCError{Code: -32602, Message: notFoundMessage, Data: map[string]any{required: value}}
}

func emptyCompletionResult(raw json.RawMessage) (any, *RPCError) {
	if len(strings.TrimSpace(string(raw))) > 0 {
		var params map[string]any
		if err := json.Unmarshal(raw, &params); err != nil {
			return nil, &RPCError{Code: -32602, Message: "invalid completion params", Data: err.Error()}
		}
	}
	return map[string]any{
		"completion": map[string]any{
			"values":  []any{},
			"total":   0,
			"hasMore": false,
		},
	}, nil
}

func validateLoggingSetLevel(raw json.RawMessage) *RPCError {
	if len(strings.TrimSpace(string(raw))) == 0 {
		return &RPCError{Code: -32602, Message: "level is required"}
	}
	var params map[string]any
	if err := json.Unmarshal(raw, &params); err != nil {
		return &RPCError{Code: -32602, Message: "invalid logging params", Data: err.Error()}
	}
	level := stringParam(params, "level", "logLevel", "severity")
	if level == "" {
		return &RPCError{Code: -32602, Message: "level is required"}
	}
	switch strings.ToLower(level) {
	case "debug", "info", "notice", "warning", "error", "critical", "alert", "emergency":
		return nil
	default:
		return &RPCError{Code: -32602, Message: "unsupported logging level", Data: map[string]any{"level": level}}
	}
}

func stringParam(params map[string]any, names ...string) string {
	for _, name := range names {
		value, _ := params[name].(string)
		if trimmed := strings.TrimSpace(value); trimmed != "" {
			return trimmed
		}
	}
	return ""
}

func (s *BuiltinServer) recordCancellation(raw json.RawMessage) {
	key, reason := cancellationParams(raw)
	if key == "" {
		return
	}
	s.mu.Lock()
	s.cancelled[key] = reason
	s.mu.Unlock()
}

func (s *BuiltinServer) consumeCancellation(rawID json.RawMessage) (string, bool) {
	key := normalizedIDKey(rawID)
	if key == "" {
		return "", false
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	reason, ok := s.cancelled[key]
	if ok {
		delete(s.cancelled, key)
	}
	return reason, ok
}

func cancellationParams(raw json.RawMessage) (string, string) {
	var params map[string]json.RawMessage
	if err := json.Unmarshal(raw, &params); err != nil {
		return "", ""
	}
	var id json.RawMessage
	for _, name := range []string{"requestId", "requestID", "request_id", "id"} {
		if value, ok := params[name]; ok {
			id = value
			break
		}
	}
	key := normalizedIDKey(id)
	if key == "" {
		return "", ""
	}
	var reason string
	for _, name := range []string{"reason", "message"} {
		if value, ok := params[name]; ok {
			_ = json.Unmarshal(value, &reason)
			if strings.TrimSpace(reason) != "" {
				break
			}
		}
	}
	if strings.TrimSpace(reason) == "" {
		reason = "cancelled by client"
	}
	return key, reason
}

func (s *BuiltinServer) toolContext(ctx context.Context) tool.Context {
	permissions := tool.PermissionDecider(readOnlyMCPPermissionDecider{allowMutating: s.allowMutatingTools})
	if s.allowMutatingTools {
		permissions = nil
	}
	return tool.Context{
		Context:          ctx,
		WorkingDirectory: s.workingDirectory,
		Permissions:      permissions,
		Metadata:         cloneAnyMap(s.toolContextMetadata),
	}
}

type readOnlyMCPPermissionDecider struct {
	allowMutating bool
}

func (d readOnlyMCPPermissionDecider) DecideTool(t tool.Tool, raw json.RawMessage, _ tool.Context) (contracts.PermissionDecision, error) {
	if d.allowMutating || t.IsReadOnly(raw) {
		return contracts.PermissionDecision{Behavior: contracts.PermissionAllow, DecisionReason: "mcp builtin server policy"}, nil
	}
	return contracts.PermissionDecision{
		Behavior:       contracts.PermissionDeny,
		Message:        "mutating tools are disabled for this MCP server",
		DecisionReason: "mcp builtin server read-only policy",
	}, nil
}

func toolResultToMCPCallResult(result contracts.ToolResult, callErr error) map[string]any {
	out := map[string]any{
		"content": mcpContentBlocks(result.Content),
	}
	if result.IsError || callErr != nil {
		out["isError"] = true
	}
	if len(result.StructuredContent) > 0 {
		out["structuredContent"] = result.StructuredContent
	}
	if len(result.Meta) > 0 {
		out["_meta"] = result.Meta
	}
	if callErr != nil {
		meta, _ := out["_meta"].(map[string]any)
		if meta == nil {
			meta = map[string]any{}
			out["_meta"] = meta
		}
		meta["error"] = callErr.Error()
	}
	return out
}

func mcpContentBlocks(content any) []map[string]any {
	switch typed := content.(type) {
	case nil:
		return []map[string]any{{"type": "text", "text": ""}}
	case string:
		return []map[string]any{{"type": "text", "text": typed}}
	case []contracts.ContentBlock:
		out := make([]map[string]any, 0, len(typed))
		for _, block := range typed {
			out = append(out, contractBlockToMCPContent(block))
		}
		if len(out) == 0 {
			return []map[string]any{{"type": "text", "text": ""}}
		}
		return out
	case []any:
		out := make([]map[string]any, 0, len(typed))
		for _, item := range typed {
			if obj, ok := item.(map[string]any); ok {
				out = append(out, obj)
			}
		}
		if len(out) > 0 {
			return out
		}
	}
	data, err := json.Marshal(content)
	if err != nil {
		return []map[string]any{{"type": "text", "text": fmt.Sprint(content)}}
	}
	return []map[string]any{{"type": "text", "text": string(data)}}
}

func contractBlockToMCPContent(block contracts.ContentBlock) map[string]any {
	switch block.Type {
	case contracts.ContentImage:
		content := map[string]any{"type": "image"}
		if source, ok := block.Source.(contracts.ImageSource); ok {
			content["data"] = source.Data
			if source.MediaType != "" {
				content["mimeType"] = source.MediaType
			}
			return content
		}
		if source, ok := block.Source.(map[string]any); ok {
			if data, ok := source["data"].(string); ok {
				content["data"] = data
			}
			if mimeType, ok := source["media_type"].(string); ok {
				content["mimeType"] = mimeType
			}
			if mimeType, ok := source["mimeType"].(string); ok {
				content["mimeType"] = mimeType
			}
			return content
		}
		return map[string]any{"type": "text", "text": fmt.Sprint(block.Source)}
	default:
		if block.Text != "" {
			return map[string]any{"type": "text", "text": block.Text}
		}
		data, err := json.Marshal(block)
		if err != nil {
			return map[string]any{"type": "text", "text": fmt.Sprint(block)}
		}
		return map[string]any{"type": "text", "text": string(data)}
	}
}

func (r serverRPCRequest) HasID() bool {
	return len(r.ID) > 0
}

func normalizedResponseID(raw json.RawMessage) json.RawMessage {
	trimmed := strings.TrimSpace(string(raw))
	if trimmed == "" {
		return json.RawMessage(`null`)
	}
	return append(json.RawMessage(nil), []byte(trimmed)...)
}

func normalizedIDKey(raw json.RawMessage) string {
	trimmed := strings.TrimSpace(string(raw))
	if trimmed == "" || trimmed == "null" {
		return ""
	}
	return trimmed
}

func firstNonWhitespace(data []byte) byte {
	for _, b := range data {
		switch b {
		case ' ', '\n', '\r', '\t':
			continue
		default:
			return b
		}
	}
	return 0
}

func cloneAnyMap(values map[string]any) map[string]any {
	if len(values) == 0 {
		return nil
	}
	out := make(map[string]any, len(values))
	for key, value := range values {
		out[key] = value
	}
	return out
}
