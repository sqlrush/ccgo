package mcp

import (
	"bytes"
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
	URL                   string
	Headers               map[string]string
	Client                HTTPDoer
	MaxResponseBytes      int64
	ProtocolVersionHeader string

	mu          sync.Mutex
	endpointURL string
	sessionID   string
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
	httpTransport := NewHTTPTransport(endpoint, t.Headers, t.Client)
	httpTransport.MaxResponseBytes = t.MaxResponseBytes
	httpTransport.ProtocolVersionHeader = t.ProtocolVersionHeader
	t.mu.Lock()
	httpTransport.SessionID = t.sessionID
	t.mu.Unlock()
	response, err := httpTransport.RoundTrip(ctx, request)
	t.mu.Lock()
	t.sessionID = httpTransport.SessionID
	t.mu.Unlock()
	return response, err
}

func (t *SSETransport) Close() error {
	t.mu.Lock()
	endpoint := t.endpointURL
	sessionID := t.sessionID
	t.mu.Unlock()
	if endpoint == "" || sessionID == "" {
		return nil
	}
	httpTransport := NewHTTPTransport(endpoint, t.Headers, t.Client)
	httpTransport.MaxResponseBytes = t.MaxResponseBytes
	httpTransport.ProtocolVersionHeader = t.ProtocolVersionHeader
	httpTransport.SessionID = sessionID
	return httpTransport.Close()
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
	discovered, err := t.discoverEndpoint(ctx)
	if err != nil {
		return "", err
	}
	t.mu.Lock()
	if t.endpointURL == "" {
		t.endpointURL = discovered
	}
	endpoint = t.endpointURL
	t.mu.Unlock()
	return endpoint, nil
}

func (t *SSETransport) discoverEndpoint(ctx context.Context) (string, error) {
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
	if t.ProtocolVersionHeader != "" {
		req.Header.Set("mcp-protocol-version", t.ProtocolVersionHeader)
	}

	client := t.Client
	if client == nil {
		client = http.DefaultClient
	}
	resp, err := client.Do(req)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()

	limit := t.MaxResponseBytes
	if limit <= 0 {
		limit = DefaultHTTPResponseLimitBytes
	}
	body, err := io.ReadAll(io.LimitReader(resp.Body, limit+1))
	if err != nil {
		return "", err
	}
	if int64(len(body)) > limit {
		return "", fmt.Errorf("mcp sse response exceeds %d bytes", limit)
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return "", fmt.Errorf("mcp sse status %d: %s", resp.StatusCode, strings.TrimSpace(string(body)))
	}
	events, err := ParseSSEEvents(bytes.NewReader(body))
	if err != nil {
		return "", err
	}
	for _, event := range events {
		if event.Event != "endpoint" {
			continue
		}
		return resolveSSEEndpoint(t.URL, event.Data)
	}
	return "", fmt.Errorf("mcp sse endpoint event not found")
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
