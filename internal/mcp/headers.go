package mcp

import (
	"context"
	"strings"

	"ccgo/internal/auth"
	"ccgo/internal/contracts"
)

type HeaderProvider func(context.Context, contracts.MCPServer) (map[string]string, error)

type ServerHeaderProvider func(context.Context, string, contracts.MCPServer) (map[string]string, error)

func TransportHeaders(server contracts.MCPServer) map[string]string {
	headers := cloneStringMap(server.Headers)
	if headers == nil {
		headers = map[string]string{}
	}
	if token := strings.TrimSpace(server.AuthToken); token != "" && !hasHeader(headers, "authorization") {
		headers["Authorization"] = bearerHeaderValue(token)
	}
	if server.OAuth != nil {
		addBetaHeader(headers, auth.OAuthBetaHeader)
	}
	if len(headers) == 0 {
		return nil
	}
	return headers
}

func AuthTokenHeaderProvider(tokenFunc func(context.Context, contracts.MCPServer) (string, error)) HeaderProvider {
	if tokenFunc == nil {
		return nil
	}
	return func(ctx context.Context, server contracts.MCPServer) (map[string]string, error) {
		token, err := tokenFunc(ctx, server)
		if err != nil {
			return nil, err
		}
		token = strings.TrimSpace(token)
		if token == "" {
			return nil, nil
		}
		return map[string]string{"Authorization": bearerHeaderValue(token)}, nil
	}
}

func MergeTransportHeaders(static map[string]string, dynamic map[string]string) map[string]string {
	headers := cloneStringMap(static)
	if headers == nil {
		headers = map[string]string{}
	}
	for key, value := range dynamic {
		key = strings.TrimSpace(key)
		if key == "" {
			continue
		}
		if existing := headerKey(headers, key); existing != "" && existing != key {
			delete(headers, existing)
		}
		headers[key] = value
	}
	if len(headers) == 0 {
		return nil
	}
	return headers
}

func hasHeader(headers map[string]string, name string) bool {
	for key := range headers {
		if strings.EqualFold(strings.TrimSpace(key), name) {
			return true
		}
	}
	return false
}

func bearerHeaderValue(token string) string {
	token = strings.TrimSpace(token)
	if strings.HasPrefix(strings.ToLower(token), "bearer ") {
		return token
	}
	return "Bearer " + token
}

func addBetaHeader(headers map[string]string, beta string) {
	beta = strings.TrimSpace(beta)
	if beta == "" {
		return
	}
	key := headerKey(headers, "anthropic-beta")
	if key == "" {
		headers["anthropic-beta"] = beta
		return
	}
	values := splitHeaderList(headers[key])
	for _, value := range values {
		if value == beta {
			return
		}
	}
	values = append(values, beta)
	headers[key] = strings.Join(values, ",")
}

func headerKey(headers map[string]string, name string) string {
	for key := range headers {
		if strings.EqualFold(strings.TrimSpace(key), name) {
			return key
		}
	}
	return ""
}

func splitHeaderList(value string) []string {
	parts := strings.Split(value, ",")
	out := make([]string, 0, len(parts))
	seen := map[string]bool{}
	for _, part := range parts {
		part = strings.TrimSpace(part)
		if part == "" || seen[part] {
			continue
		}
		seen[part] = true
		out = append(out, part)
	}
	return out
}
