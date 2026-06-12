package mcp

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"sync"
)

type SSETransport struct {
	URL                    string
	Headers                map[string]string
	HeaderProvider         func(context.Context) (map[string]string, error)
	AuthorizationRefresher func(context.Context) error
	Client                 HTTPDoer
	MaxResponseBytes       int64
	ProtocolVersionHeader  string

	connectMu   sync.Mutex
	mu          sync.Mutex
	endpointURL string
	sessionID   string
	streamClose context.CancelFunc
	waiters     map[string]chan sseWaitResult
	pending     map[string]sseWaitResult

	notificationMu      sync.RWMutex
	notificationHandler RPCNotificationHandler
	requestMu           sync.RWMutex
	requestHandler      RPCRequestHandler
}

type sseWaitResult struct {
	Response RPCResponse
	Err      error
}

func NewSSETransport(rawURL string, headers map[string]string, client HTTPDoer) *SSETransport {
	return &SSETransport{
		URL:     strings.TrimSpace(rawURL),
		Headers: cloneStringMap(headers),
		Client:  client,
	}
}

func (t *SSETransport) RoundTrip(ctx context.Context, request RPCRequest) (RPCResponse, error) {
	endpoint, err := t.endpoint(ctx)
	if err != nil {
		return RPCResponse{}, err
	}
	waiter := t.registerWaiter(request.ID)
	httpTransport := NewHTTPTransport(endpoint, t.Headers, t.Client)
	httpTransport.MaxResponseBytes = t.MaxResponseBytes
	httpTransport.HeaderProvider = t.HeaderProvider
	httpTransport.AuthorizationRefresher = t.AuthorizationRefresher
	t.requestMu.RLock()
	httpTransport.SetRequestHandler(t.requestHandler)
	t.requestMu.RUnlock()
	t.mu.Lock()
	httpTransport.SessionID = t.sessionID
	httpTransport.ProtocolVersionHeader = t.ProtocolVersionHeader
	t.mu.Unlock()
	response, err := httpTransport.RoundTrip(ctx, request)
	t.mu.Lock()
	t.sessionID = httpTransport.SessionID
	t.mu.Unlock()
	if err != nil {
		t.unregisterWaiter(request.ID, waiter)
		return RPCResponse{}, err
	}
	if response.Error != nil || len(response.Result) > 0 {
		t.unregisterWaiter(request.ID, waiter)
		return response, nil
	}
	select {
	case result := <-waiter:
		if result.Err != nil {
			return RPCResponse{}, result.Err
		}
		return result.Response, nil
	case <-ctx.Done():
		t.unregisterWaiter(request.ID, waiter)
		return RPCResponse{}, ctx.Err()
	}
}

func (t *SSETransport) SendNotification(ctx context.Context, notification RPCNotification) error {
	endpoint, err := t.endpoint(ctx)
	if err != nil {
		return err
	}
	httpTransport := NewHTTPTransport(endpoint, t.Headers, t.Client)
	httpTransport.MaxResponseBytes = t.MaxResponseBytes
	httpTransport.HeaderProvider = t.HeaderProvider
	httpTransport.AuthorizationRefresher = t.AuthorizationRefresher
	t.requestMu.RLock()
	httpTransport.SetRequestHandler(t.requestHandler)
	t.requestMu.RUnlock()
	t.mu.Lock()
	httpTransport.SessionID = t.sessionID
	httpTransport.ProtocolVersionHeader = t.ProtocolVersionHeader
	t.mu.Unlock()
	err = httpTransport.SendNotification(ctx, notification)
	t.mu.Lock()
	t.sessionID = httpTransport.SessionID
	t.mu.Unlock()
	return err
}

func (t *SSETransport) ResetSession() {
	if t == nil {
		return
	}
	t.mu.Lock()
	streamClose := t.streamClose
	waiters := t.waiters
	t.endpointURL = ""
	t.sessionID = ""
	t.streamClose = nil
	t.waiters = nil
	t.pending = nil
	t.mu.Unlock()
	if streamClose != nil {
		streamClose()
	}
	for id, waiter := range waiters {
		delete(waiters, id)
		waiter <- sseWaitResult{Err: fmt.Errorf("mcp sse session reset")}
	}
}

func (t *SSETransport) RefreshAuthorization(ctx context.Context) (bool, error) {
	if t == nil || t.AuthorizationRefresher == nil {
		return false, nil
	}
	return true, t.AuthorizationRefresher(ctx)
}

func (t *SSETransport) SetProtocolVersionHeader(version string) {
	if t == nil {
		return
	}
	t.mu.Lock()
	t.ProtocolVersionHeader = strings.TrimSpace(version)
	t.mu.Unlock()
}

func (t *SSETransport) Close() error {
	t.mu.Lock()
	endpoint := t.endpointURL
	sessionID := t.sessionID
	protocolVersion := t.ProtocolVersionHeader
	streamClose := t.streamClose
	t.streamClose = nil
	t.mu.Unlock()
	if streamClose != nil {
		streamClose()
	}
	if endpoint == "" || sessionID == "" {
		return nil
	}
	httpTransport := NewHTTPTransport(endpoint, t.Headers, t.Client)
	httpTransport.MaxResponseBytes = t.MaxResponseBytes
	httpTransport.HeaderProvider = t.HeaderProvider
	httpTransport.AuthorizationRefresher = t.AuthorizationRefresher
	httpTransport.SessionID = sessionID
	httpTransport.ProtocolVersionHeader = protocolVersion
	return httpTransport.Close()
}

func (t *SSETransport) SetNotificationHandler(handler RPCNotificationHandler) {
	if t == nil {
		return
	}
	t.notificationMu.Lock()
	t.notificationHandler = handler
	t.notificationMu.Unlock()
}

func (t *SSETransport) dispatchNotification(response RPCResponse) bool {
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

func (t *SSETransport) SetRequestHandler(handler RPCRequestHandler) {
	if t == nil {
		return
	}
	t.requestMu.Lock()
	t.requestHandler = handler
	t.requestMu.Unlock()
}

func (t *SSETransport) endpoint(ctx context.Context) (string, error) {
	if t == nil || t.URL == "" {
		return "", fmt.Errorf("mcp sse transport url is required")
	}
	t.mu.Lock()
	endpoint := t.endpointURL
	t.mu.Unlock()
	if endpoint != "" {
		return endpoint, nil
	}
	return t.connect(ctx)
}

func (t *SSETransport) connect(ctx context.Context) (string, error) {
	t.connectMu.Lock()
	defer t.connectMu.Unlock()
	t.mu.Lock()
	if t.endpointURL != "" {
		endpoint := t.endpointURL
		t.mu.Unlock()
		return endpoint, nil
	}
	t.mu.Unlock()

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, t.URL, nil)
	if err != nil {
		return "", err
	}
	req.Header.Set("accept", "text/event-stream")
	for key, value := range t.Headers {
		if strings.TrimSpace(key) != "" {
			req.Header.Set(key, value)
		}
	}
	if t.HeaderProvider != nil {
		headers, err := t.HeaderProvider(ctx)
		if err != nil {
			return "", err
		}
		for key, value := range headers {
			if strings.TrimSpace(key) != "" {
				req.Header.Set(key, value)
			}
		}
	}
	t.mu.Lock()
	protocolVersion := t.ProtocolVersionHeader
	t.mu.Unlock()
	if protocolVersion != "" {
		req.Header.Set("mcp-protocol-version", protocolVersion)
	}

	client := t.Client
	if client == nil {
		client = http.DefaultClient
	}
	resp, err := client.Do(req)
	if err != nil {
		return "", err
	}

	limit := t.MaxResponseBytes
	if limit <= 0 {
		limit = DefaultHTTPResponseLimitBytes
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		body, _ := io.ReadAll(io.LimitReader(resp.Body, limit+1))
		_ = resp.Body.Close()
		return "", &HTTPStatusError{Prefix: "mcp sse", StatusCode: resp.StatusCode, Body: string(body)}
	}
	if nextSessionID := strings.TrimSpace(resp.Header.Get("mcp-session-id")); nextSessionID != "" {
		t.mu.Lock()
		t.sessionID = nextSessionID
		t.mu.Unlock()
	}

	scanner := newSSEScanner(io.LimitReader(resp.Body, limit+1))
	for {
		event, ok, err := scanSSEEvent(scanner)
		if err != nil {
			_ = resp.Body.Close()
			return "", err
		}
		if !ok {
			_ = resp.Body.Close()
			return "", fmt.Errorf("mcp sse endpoint event not found")
		}
		if event.Event != "endpoint" {
			t.dispatchSSEEvent(ctx, event)
			continue
		}
		endpoint, err := resolveSSEEndpoint(t.URL, event.Data)
		if err != nil {
			_ = resp.Body.Close()
			return "", err
		}
		streamCtx, cancel := context.WithCancel(ctx)
		t.mu.Lock()
		t.endpointURL = endpoint
		t.streamClose = cancel
		t.mu.Unlock()
		go t.readStream(streamCtx, scanner, resp.Body)
		return endpoint, nil
	}
}

func (t *SSETransport) readStream(ctx context.Context, scanner *sseScanner, body io.Closer) {
	defer body.Close()
	for {
		select {
		case <-ctx.Done():
			return
		default:
		}
		event, ok, err := scanSSEEvent(scanner)
		if err != nil {
			t.failSSEWaiters(err)
			return
		}
		if !ok {
			t.failSSEWaiters(io.EOF)
			return
		}
		t.dispatchSSEEvent(ctx, event)
	}
}

func (t *SSETransport) registerWaiter(id string) chan sseWaitResult {
	ch := make(chan sseWaitResult, 1)
	if id == "" {
		return ch
	}
	t.mu.Lock()
	defer t.mu.Unlock()
	if t.pending != nil {
		if result, ok := t.pending[id]; ok {
			delete(t.pending, id)
			ch <- result
			return ch
		}
	}
	if t.waiters == nil {
		t.waiters = map[string]chan sseWaitResult{}
	}
	t.waiters[id] = ch
	return ch
}

func (t *SSETransport) unregisterWaiter(id string, ch chan sseWaitResult) {
	if id == "" {
		return
	}
	t.mu.Lock()
	defer t.mu.Unlock()
	if t.waiters != nil && t.waiters[id] == ch {
		delete(t.waiters, id)
	}
}

func (t *SSETransport) dispatchSSEEvent(ctx context.Context, event SSEEvent) {
	if event.Event != "" && event.Event != "message" {
		return
	}
	if strings.TrimSpace(event.Data) == "" {
		return
	}
	var response RPCResponse
	if err := json.Unmarshal([]byte(event.Data), &response); err != nil {
		t.failSSEWaiters(fmt.Errorf("decode mcp sse response: %w", err))
		return
	}
	if t.dispatchNotification(response) {
		return
	}
	if handled, err := t.dispatchInboundRequest(ctx, response); err != nil {
		t.failSSEWaiters(err)
		return
	} else if handled {
		return
	}
	if response.ID == "" {
		return
	}
	result := sseWaitResult{Response: response}
	t.mu.Lock()
	defer t.mu.Unlock()
	if waiter := t.waiters[response.ID]; waiter != nil {
		delete(t.waiters, response.ID)
		waiter <- result
		return
	}
	if t.pending == nil {
		t.pending = map[string]sseWaitResult{}
	}
	t.pending[response.ID] = result
}

func (t *SSETransport) dispatchInboundRequest(ctx context.Context, response RPCResponse) (bool, error) {
	request, ok := InboundRequestFromRPCResponse(response)
	if !ok {
		return false, nil
	}
	t.requestMu.RLock()
	handler := t.requestHandler
	t.requestMu.RUnlock()
	t.mu.Lock()
	endpoint := t.endpointURL
	sessionID := t.sessionID
	protocolVersion := t.ProtocolVersionHeader
	t.mu.Unlock()
	if endpoint == "" {
		return true, fmt.Errorf("mcp sse response endpoint is not established")
	}
	httpTransport := NewHTTPTransport(endpoint, t.Headers, t.Client)
	httpTransport.MaxResponseBytes = t.MaxResponseBytes
	httpTransport.ProtocolVersionHeader = t.ProtocolVersionHeader
	httpTransport.HeaderProvider = t.HeaderProvider
	httpTransport.AuthorizationRefresher = t.AuthorizationRefresher
	httpTransport.SessionID = sessionID
	httpTransport.ProtocolVersionHeader = protocolVersion
	if err := httpTransport.postRPCResponse(ctx, ResponseForInboundRequest(ctx, request, handler)); err != nil {
		return true, err
	}
	t.mu.Lock()
	t.sessionID = httpTransport.SessionID
	t.mu.Unlock()
	return true, nil
}

func (t *SSETransport) failSSEWaiters(err error) {
	if err == nil {
		return
	}
	t.mu.Lock()
	defer t.mu.Unlock()
	for id, waiter := range t.waiters {
		delete(t.waiters, id)
		waiter <- sseWaitResult{Err: err}
	}
}

func resolveSSEEndpoint(baseURL string, data string) (string, error) {
	raw := endpointDataValue(data)
	if raw == "" {
		return "", fmt.Errorf("mcp sse endpoint is empty")
	}
	parsed, err := url.Parse(raw)
	if err != nil {
		return "", err
	}
	if parsed.IsAbs() {
		return parsed.String(), nil
	}
	base, err := url.Parse(baseURL)
	if err != nil {
		return "", err
	}
	return base.ResolveReference(parsed).String(), nil
}

func endpointDataValue(data string) string {
	data = strings.TrimSpace(data)
	if data == "" {
		return ""
	}
	if strings.HasPrefix(data, `"`) {
		var value string
		if err := json.Unmarshal([]byte(data), &value); err == nil {
			return strings.TrimSpace(value)
		}
	}
	if strings.HasPrefix(data, "{") {
		var object map[string]string
		if err := json.Unmarshal([]byte(data), &object); err == nil {
			for _, key := range []string{"endpoint", "url", "uri"} {
				if value := strings.TrimSpace(object[key]); value != "" {
					return value
				}
			}
		}
	}
	return data
}
