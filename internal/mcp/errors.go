package mcp

import (
	"errors"
	"fmt"
	"strings"
)

type HTTPStatusError struct {
	Prefix     string
	StatusCode int
	Body       string
}

func (e *HTTPStatusError) Error() string {
	if e == nil {
		return ""
	}
	prefix := strings.TrimSpace(e.Prefix)
	if prefix == "" {
		prefix = "mcp http"
	}
	body := strings.TrimSpace(e.Body)
	if body == "" {
		return fmt.Sprintf("%s status %d", prefix, e.StatusCode)
	}
	return fmt.Sprintf("%s status %d: %s", prefix, e.StatusCode, body)
}

func IsUnauthorizedError(err error) bool {
	if err == nil {
		return false
	}
	var statusErr *HTTPStatusError
	if errors.As(err, &statusErr) && statusErr != nil && statusErr.StatusCode == 401 {
		return true
	}
	var rpcErr *RPCError
	if errors.As(err, &rpcErr) && rpcErr != nil {
		if rpcErr.Code == 401 {
			return true
		}
		text := strings.ToLower(rpcErr.Message)
		return strings.Contains(text, "unauthorized") || strings.Contains(text, "invalid token") || strings.Contains(text, "expired token")
	}
	return false
}
