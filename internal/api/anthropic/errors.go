package anthropic

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
)

type APIError struct {
	StatusCode int
	RequestID  string
	Type       string
	Message    string
	Header     http.Header
	Raw        []byte
}

func (e APIError) Error() string {
	if e.Type == "" && e.Message == "" {
		return fmt.Sprintf("anthropic api error: status %d", e.StatusCode)
	}
	if e.Type == "" {
		return fmt.Sprintf("anthropic api error: status %d: %s", e.StatusCode, e.Message)
	}
	return fmt.Sprintf("anthropic api error: status %d: %s: %s", e.StatusCode, e.Type, e.Message)
}

func (e APIError) Retryable() bool {
	return ShouldRetryAPIError(e)
}

func (e APIError) RateLimited() bool {
	return e.StatusCode == http.StatusTooManyRequests || e.Type == "rate_limit_error"
}

func (e APIError) AuthError() bool {
	return e.StatusCode == http.StatusUnauthorized || e.StatusCode == http.StatusForbidden || e.Type == "authentication_error" || e.Type == "permission_error"
}

func decodeAPIError(resp *http.Response) error {
	body, readErr := io.ReadAll(resp.Body)
	if readErr != nil {
		return readErr
	}
	apiErr := APIError{
		StatusCode: resp.StatusCode,
		RequestID:  resp.Header.Get("request-id"),
		Header:     resp.Header.Clone(),
		Raw:        body,
	}
	var envelope struct {
		Error struct {
			Type    string `json:"type"`
			Message string `json:"message"`
		} `json:"error"`
	}
	if err := json.Unmarshal(body, &envelope); err == nil {
		apiErr.Type = envelope.Error.Type
		apiErr.Message = envelope.Error.Message
	}
	return apiErr
}
