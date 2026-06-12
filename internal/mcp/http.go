package mcp

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
)

const DefaultHTTPResponseLimitBytes int64 = 10 * 1024 * 1024

type HTTPDoer interface {
	Do(*http.Request) (*http.Response, error)
}

type HTTPTransport struct {
	URL                   string
	Headers               map[string]string
	Client                HTTPDoer
	MaxResponseBytes      int64
	ProtocolVersionHeader string
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
	req.Header.Set("accept", "application/json, text/event-stream")
	if t.ProtocolVersionHeader != "" {
		req.Header.Set("mcp-protocol-version", t.ProtocolVersionHeader)
	}
	for key, value := range t.Headers {
		if strings.TrimSpace(key) != "" {
			req.Header.Set(key, value)
		}
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

	limit := t.MaxResponseBytes
	if limit <= 0 {
		limit = DefaultHTTPResponseLimitBytes
	}
	body, err := io.ReadAll(io.LimitReader(resp.Body, limit+1))
	if err != nil {
		return RPCResponse{}, err
	}
	if int64(len(body)) > limit {
		return RPCResponse{}, fmt.Errorf("mcp http response exceeds %d bytes", limit)
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return RPCResponse{}, fmt.Errorf("mcp http status %d: %s", resp.StatusCode, strings.TrimSpace(string(body)))
	}
	if len(bytes.TrimSpace(body)) == 0 {
		return RPCResponse{ID: request.ID}, nil
	}
	var rpcResponse RPCResponse
	if err := json.Unmarshal(body, &rpcResponse); err != nil {
		return RPCResponse{}, fmt.Errorf("decode mcp http response: %w", err)
	}
	return rpcResponse, nil
}
