package mcp

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"sync"
)

const DefaultHTTPResponseLimitBytes int64 = 10 * 1024 * 1024

type HTTPDoer interface {
	Do(*http.Request) (*http.Response, error)
}

type HTTPTransport struct {
	URL                    string
	Headers                map[string]string
	HeaderProvider         func(context.Context) (map[string]string, error)
	AuthorizationRefresher func(context.Context) error
	Client                 HTTPDoer
	MaxResponseBytes       int64
	ProtocolVersionHeader  string
	SessionID              string
	mu                     sync.Mutex
	notificationMu         sync.RWMutex
	notificationHandler    RPCNotificationHandler
	requestMu              sync.RWMutex
	requestHandler         RPCRequestHandler
}

func NewHTTPTransport(rawURL string, headers map[string]string, client HTTPDoer) *HTTPTransport {
	return &HTTPTransport{
		URL:     strings.TrimSpace(rawURL),
		Headers: cloneStringMap(headers),
		Client:  client,
	}
}

func (t *HTTPTransport) RoundTrip(ctx context.Context, request RPCRequest) (RPCResponse, error) {
	if t == nil || t.URL == "" {
		return RPCResponse{}, fmt.Errorf("mcp http transport url is required")
	}
	data, err := json.Marshal(request)
	if err != nil {
		return RPCResponse{}, err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, t.URL, bytes.NewReader(data))
	if err != nil {
		return RPCResponse{}, err
	}
	req.Header.Set("content-type", "application/json")
	if err := t.applyHeaders(ctx, req); err != nil {
		return RPCResponse{}, err
	}

	client := t.Client
	if client == nil {
		client = http.DefaultClient
	}
	resp, err := client.Do(req)
	if err != nil {
		return RPCResponse{}, err
	}
	defer resp.Body.Close()
	if nextSessionID := strings.TrimSpace(resp.Header.Get("mcp-session-id")); nextSessionID != "" {
		t.mu.Lock()
		t.SessionID = nextSessionID
		t.mu.Unlock()
	}

	limit := t.MaxResponseBytes
	if limit <= 0 {
		limit = DefaultHTTPResponseLimitBytes
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		body, _ := io.ReadAll(io.LimitReader(resp.Body, limit+1))
		return RPCResponse{}, &HTTPStatusError{Prefix: "mcp http", StatusCode: resp.StatusCode, Body: string(body)}
	}
	if isEventStream(resp.Header.Get("content-type")) {
		return rpcResponseFromSSEWithHandlers(io.LimitReader(resp.Body, limit+1), request.ID, t.dispatchNotification, func(response RPCResponse) (bool, error) {
			return t.dispatchInboundRequest(ctx, response)
		})
	}
	body, err := io.ReadAll(io.LimitReader(resp.Body, limit+1))
	if err != nil {
		return RPCResponse{}, err
	}
	if int64(len(body)) > limit {
		return RPCResponse{}, fmt.Errorf("mcp http response exceeds %d bytes", limit)
	}
	if len(bytes.TrimSpace(body)) == 0 {
		return RPCResponse{ID: request.ID}, nil
	}
	var rpcResponse RPCResponse
	if err := json.Unmarshal(body, &rpcResponse); err != nil {
		return RPCResponse{}, fmt.Errorf("decode mcp http response: %w", err)
	}
	if t.dispatchNotification(rpcResponse) {
		return RPCResponse{}, fmt.Errorf("mcp http response for id %q not found", request.ID)
	}
	if handled, err := t.dispatchInboundRequest(ctx, rpcResponse); err != nil {
		return RPCResponse{}, err
	} else if handled {
		return RPCResponse{}, fmt.Errorf("mcp http response for id %q not found", request.ID)
	}
	return rpcResponse, nil
}

func (t *HTTPTransport) SendNotification(ctx context.Context, notification RPCNotification) error {
	if t == nil || t.URL == "" {
		return fmt.Errorf("mcp http transport url is required")
	}
	if notification.JSONRPC == "" {
		notification.JSONRPC = JSONRPCVersion
	}
	data, err := json.Marshal(notification)
	if err != nil {
		return err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, t.URL, bytes.NewReader(data))
	if err != nil {
		return err
	}
	req.Header.Set("content-type", "application/json")
	if err := t.applyHeaders(ctx, req); err != nil {
		return err
	}
	client := t.responsePostClient()
	resp, err := client.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if nextSessionID := strings.TrimSpace(resp.Header.Get("mcp-session-id")); nextSessionID != "" {
		t.mu.Lock()
		t.SessionID = nextSessionID
		t.mu.Unlock()
	}
	body, _ := io.ReadAll(io.LimitReader(resp.Body, 4096))
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return &HTTPStatusError{Prefix: "mcp http notification", StatusCode: resp.StatusCode, Body: string(body)}
	}
	return nil
}

func (t *HTTPTransport) RefreshAuthorization(ctx context.Context) (bool, error) {
	if t == nil || t.AuthorizationRefresher == nil {
		return false, nil
	}
	return true, t.AuthorizationRefresher(ctx)
}

func (t *HTTPTransport) ResetSession() {
	if t == nil {
		return
	}
	t.mu.Lock()
	t.SessionID = ""
	t.mu.Unlock()
}

func (t *HTTPTransport) SetNotificationHandler(handler RPCNotificationHandler) {
	if t == nil {
		return
	}
	t.notificationMu.Lock()
	t.notificationHandler = handler
	t.notificationMu.Unlock()
}

func (t *HTTPTransport) dispatchNotification(response RPCResponse) bool {
	notification, ok := NotificationFromRPCResponse(response)
	if !ok {
		return false
	}
	t.notificationMu.RLock()
	handler := t.notificationHandler
	t.notificationMu.RUnlock()
	if handler != nil {
		handler(notification)
	}
	return true
}

func (t *HTTPTransport) SetRequestHandler(handler RPCRequestHandler) {
	if t == nil {
		return
	}
	t.requestMu.Lock()
	t.requestHandler = handler
	t.requestMu.Unlock()
}

func (t *HTTPTransport) dispatchInboundRequest(ctx context.Context, response RPCResponse) (bool, error) {
	request, ok := InboundRequestFromRPCResponse(response)
	if !ok {
		return false, nil
	}
	t.requestMu.RLock()
	handler := t.requestHandler
	t.requestMu.RUnlock()
	return true, t.postRPCResponse(ctx, ResponseForInboundRequest(ctx, request, handler))
}

func (t *HTTPTransport) postRPCResponse(ctx context.Context, response RPCResponse) error {
	if t == nil || t.URL == "" {
		return fmt.Errorf("mcp http transport url is required")
	}
	data, err := json.Marshal(response)
	if err != nil {
		return err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, t.URL, bytes.NewReader(data))
	if err != nil {
		return err
	}
	req.Header.Set("content-type", "application/json")
	if err := t.applyHeaders(ctx, req); err != nil {
		return err
	}
	client := t.Client
	if client == nil {
		client = http.DefaultClient
	}
	resp, err := client.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if nextSessionID := strings.TrimSpace(resp.Header.Get("mcp-session-id")); nextSessionID != "" {
		t.mu.Lock()
		t.SessionID = nextSessionID
		t.mu.Unlock()
	}
	body, _ := io.ReadAll(io.LimitReader(resp.Body, 4096))
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return &HTTPStatusError{Prefix: "mcp http response", StatusCode: resp.StatusCode, Body: string(body)}
	}
	return nil
}

func (t *HTTPTransport) responsePostClient() HTTPDoer {
	if t == nil || t.Client == nil {
		return &http.Client{Transport: http.DefaultTransport.(*http.Transport).Clone()}
	}
	client, ok := t.Client.(*http.Client)
	if !ok {
		return t.Client
	}
	clone := *client
	switch transport := client.Transport.(type) {
	case *http.Transport:
		if transport != nil {
			clone.Transport = transport.Clone()
		}
	case nil:
		clone.Transport = http.DefaultTransport.(*http.Transport).Clone()
	}
	return &clone
}

func (t *HTTPTransport) Close() error {
	if t == nil || t.URL == "" {
		return nil
	}
	t.mu.Lock()
	sessionID := t.SessionID
	t.mu.Unlock()
	if sessionID == "" {
		return nil
	}
	req, err := http.NewRequestWithContext(context.Background(), http.MethodDelete, t.URL, nil)
	if err != nil {
		return err
	}
	if err := t.applyHeadersWithSession(context.Background(), req, sessionID); err != nil {
		return err
	}
	client := t.Client
	if client == nil {
		client = http.DefaultClient
	}
	resp, err := client.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(io.LimitReader(resp.Body, 4096))
	switch {
	case resp.StatusCode >= 200 && resp.StatusCode < 300:
		return nil
	case resp.StatusCode == http.StatusNotFound || resp.StatusCode == http.StatusMethodNotAllowed:
		return nil
	default:
		return &HTTPStatusError{Prefix: "mcp http close", StatusCode: resp.StatusCode, Body: string(body)}
	}
}

func (t *HTTPTransport) applyHeaders(ctx context.Context, req *http.Request) error {
	t.mu.Lock()
	sessionID := t.SessionID
	t.mu.Unlock()
	return t.applyHeadersWithSession(ctx, req, sessionID)
}

func (t *HTTPTransport) applyHeadersWithSession(ctx context.Context, req *http.Request, sessionID string) error {
	req.Header.Set("accept", "application/json, text/event-stream")
	if t.ProtocolVersionHeader != "" {
		req.Header.Set("mcp-protocol-version", t.ProtocolVersionHeader)
	}
	if sessionID != "" {
		req.Header.Set("mcp-session-id", sessionID)
	}
	for key, value := range t.Headers {
		if strings.TrimSpace(key) != "" {
			req.Header.Set(key, value)
		}
	}
	if t.HeaderProvider != nil {
		headers, err := t.HeaderProvider(ctx)
		if err != nil {
			return err
		}
		for key, value := range headers {
			if strings.TrimSpace(key) != "" {
				req.Header.Set(key, value)
			}
		}
	}
	return nil
}

type SSEEvent struct {
	Event string
	Data  string
	ID    string
}

func ParseSSEEvents(r io.Reader) ([]SSEEvent, error) {
	scanner := newSSEScanner(r)
	var events []SSEEvent
	for {
		event, ok, err := scanSSEEvent(scanner)
		if err != nil {
			return nil, err
		}
		if !ok {
			return events, nil
		}
		events = append(events, event)
	}
}

type sseScanner struct {
	scanner *bufio.Scanner
}

func newSSEScanner(r io.Reader) *sseScanner {
	scanner := bufio.NewScanner(r)
	scanner.Buffer(make([]byte, 0, 64*1024), 10*1024*1024)
	return &sseScanner{scanner: scanner}
}

func scanSSEEvent(scanner *sseScanner) (SSEEvent, bool, error) {
	var current SSEEvent
	var dataLines []string

	flush := func() (SSEEvent, bool) {
		if current.Event == "" && current.ID == "" && len(dataLines) == 0 {
			return SSEEvent{}, false
		}
		current.Data = strings.Join(dataLines, "\n")
		return current, true
	}

	for scanner.scanner.Scan() {
		line := scanner.scanner.Text()
		line = strings.TrimSuffix(line, "\r")
		if line == "" {
			if event, ok := flush(); ok {
				return event, true, nil
			}
			continue
		}
		if strings.HasPrefix(line, ":") {
			continue
		}
		field, value, hasValue := strings.Cut(line, ":")
		if hasValue && strings.HasPrefix(value, " ") {
			value = strings.TrimPrefix(value, " ")
		}
		switch field {
		case "event":
			current.Event = value
		case "data":
			dataLines = append(dataLines, value)
		case "id":
			current.ID = value
		}
	}
	if err := scanner.scanner.Err(); err != nil {
		return SSEEvent{}, false, err
	}
	if event, ok := flush(); ok {
		return event, true, nil
	}
	return SSEEvent{}, false, nil
}

func rpcResponseFromSSE(r io.Reader, requestID string) (RPCResponse, error) {
	return rpcResponseFromSSEWithHandlers(r, requestID, nil, nil)
}

func rpcResponseFromSSEWithNotifications(r io.Reader, requestID string, notify func(RPCResponse) bool) (RPCResponse, error) {
	return rpcResponseFromSSEWithHandlers(r, requestID, notify, nil)
}

func rpcResponseFromSSEWithHandlers(r io.Reader, requestID string, notify func(RPCResponse) bool, handleRequest func(RPCResponse) (bool, error)) (RPCResponse, error) {
	scanner := newSSEScanner(r)
	for {
		event, ok, err := scanSSEEvent(scanner)
		if err != nil {
			return RPCResponse{}, err
		}
		if !ok {
			break
		}
		if strings.TrimSpace(event.Data) == "" {
			continue
		}
		var response RPCResponse
		if err := json.Unmarshal([]byte(event.Data), &response); err != nil {
			if event.Event != "" && event.Event != "message" {
				continue
			}
			return RPCResponse{}, fmt.Errorf("decode mcp http event-stream response: %w", err)
		}
		if _, ok := NotificationFromRPCResponse(response); ok {
			if notify != nil {
				notify(response)
			}
			continue
		}
		if handleRequest != nil {
			handled, err := handleRequest(response)
			if err != nil {
				return RPCResponse{}, err
			}
			if handled {
				continue
			}
		}
		if _, ok := InboundRequestFromRPCResponse(response); ok {
			continue
		}
		if requestID == "" || response.ID == requestID {
			return response, nil
		}
	}
	return RPCResponse{}, fmt.Errorf("mcp http event-stream response for id %q not found", requestID)
}

func isEventStream(contentType string) bool {
	return strings.Contains(strings.ToLower(contentType), "text/event-stream")
}
