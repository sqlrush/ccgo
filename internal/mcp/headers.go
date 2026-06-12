package mcp

import (
	"strings"

	"ccgo/internal/auth"
	"ccgo/internal/contracts"
)

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
