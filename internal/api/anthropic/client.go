package anthropic

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"

	"ccgo/internal/auth"
)

type Client struct {
	BaseURL     string
	Version     string
	Beta        []string
	Headers     http.Header
	APIKey      string
	AccessToken string
	UserAgent   string
	HTTPClient  *http.Client
	Retry       RetryConfig
	Dumper      *PromptDumper
}

type Option func(*Client)

func NewClient(options ...Option) *Client {
	c := &Client{
		BaseURL: DefaultBaseURL,
		Version: DefaultVersion,
		Headers: http.Header{},
		Retry:   DefaultRetryConfig(),
		HTTPClient: &http.Client{
			Timeout: 10 * time.Minute,
		},
	}
	for _, option := range options {
		option(c)
	}
	return c
}

func WithBaseURL(baseURL string) Option {
	return func(c *Client) {
		c.BaseURL = strings.TrimRight(baseURL, "/")
	}
}

func WithHTTPClient(client *http.Client) Option {
	return func(c *Client) {
		if client != nil {
			c.HTTPClient = client
		}
	}
}

func WithAPIKey(apiKey string) Option {
	return func(c *Client) {
		c.APIKey = apiKey
	}
}

func WithAccessToken(token string) Option {
	return func(c *Client) {
		c.AccessToken = token
	}
}

func WithCredentials(credentials auth.Credentials) Option {
	return func(c *Client) {
		c.APIKey = credentials.APIKey
		c.AccessToken = credentials.AccessToken
	}
}

func WithVersion(version string) Option {
	return func(c *Client) {
		if strings.TrimSpace(version) != "" {
			c.Version = version
		}
	}
}

func WithBeta(beta ...string) Option {
	return func(c *Client) {
		c.Beta = MergeBetaHeaders(beta)
	}
}

func WithUserAgent(userAgent string) Option {
	return func(c *Client) {
		c.UserAgent = userAgent
	}
}

func WithHeader(key string, value string) Option {
	return func(c *Client) {
		if c.Headers == nil {
			c.Headers = http.Header{}
		}
		c.Headers.Set(key, value)
	}
}

func WithHeaders(headers http.Header) Option {
	return func(c *Client) {
		if c.Headers == nil {
			c.Headers = http.Header{}
		}
		for key, values := range headers {
			c.Headers.Del(key)
			for _, value := range values {
				c.Headers.Add(key, value)
			}
		}
	}
}

func WithRetryConfig(config RetryConfig) Option {
	return func(c *Client) {
		if config.BaseDelay == 0 {
			config.BaseDelay = DefaultRetryConfig().BaseDelay
		}
		if config.MaxDelay == 0 {
			config.MaxDelay = DefaultRetryConfig().MaxDelay
		}
		c.Retry = config
	}
}

func WithPromptDumper(dumper *PromptDumper) Option {
	return func(c *Client) {
		c.Dumper = dumper
	}
}

func WithMaxRetries(maxRetries int) Option {
	return func(c *Client) {
		c.Retry.MaxRetries = maxRetries
	}
}

func (c *Client) CreateMessage(ctx context.Context, request Request) (*Response, error) {
	request.Stream = false
	if err := validateRequest(request); err != nil {
		return nil, err
	}
	body, err := c.doJSON(ctx, "/v1/messages", request)
	if err != nil {
		return nil, err
	}
	var response Response
	if err := json.Unmarshal(body, &response); err != nil {
		return nil, err
	}
	modelName := response.Model
	if modelName == "" {
		modelName = request.Model
	}
	response.Usage = UsageWithCost(modelName, response.Usage)
	response.Raw = body
	return &response, nil
}

func (c *Client) StreamMessages(ctx context.Context, request Request, handle func(StreamEvent) error) error {
	request.Stream = true
	if err := validateRequest(request); err != nil {
		return err
	}
	for attempt := 1; ; attempt++ {
		req, dumpTimestamp, err := c.newJSONRequestWithDump(ctx, "/v1/messages", request)
		if err != nil {
			return err
		}
		resp, err := c.httpClient().Do(req)
		if err != nil {
			if attempt > c.Retry.MaxRetries || !ShouldRetryError(err) {
				return err
			}
			if err := sleepWithConfig(ctx, RetryDelay(attempt, "", c.Retry), c.Retry); err != nil {
				return err
			}
			continue
		}
		if resp.StatusCode < 200 || resp.StatusCode > 299 {
			apiErr := decodeAPIError(resp)
			resp.Body.Close()
			if attempt > c.Retry.MaxRetries || !ShouldRetryError(apiErr) {
				return apiErr
			}
			retryAfter := ""
			var decoded APIError
			if asAPIError(apiErr, &decoded) {
				retryAfter = decoded.Header.Get("retry-after")
			}
			if err := sleepWithConfig(ctx, RetryDelay(attempt, retryAfter, c.Retry), c.Retry); err != nil {
				return err
			}
			continue
		}
		defer resp.Body.Close()
		var chunks []json.RawMessage
		err = ParseStream(resp.Body, func(event StreamEvent) error {
			if len(event.Raw) > 0 {
				chunks = append(chunks, append(json.RawMessage(nil), event.Raw...))
			}
			return handle(event)
		})
		if err == nil {
			c.dumpStreamResponse(dumpTimestamp, chunks)
		}
		return err
	}
}

func (c *Client) doJSON(ctx context.Context, path string, payload any) (json.RawMessage, error) {
	payloadForAttempt := payload
	var lastErr error
	for attempt := 1; ; attempt++ {
		body, err := c.doJSONOnce(ctx, path, payloadForAttempt)
		if err == nil {
			return body, nil
		}
		lastErr = err
		var apiErr APIError
		if asAPIError(err, &apiErr) {
			if overflow, ok := ParseMaxTokensContextOverflowError(apiErr); ok && attempt <= c.Retry.MaxRetries {
				if adjusted, ok := adjustRequestPayloadForContextOverflow(payloadForAttempt, overflow); ok {
					payloadForAttempt = adjusted
					continue
				}
			}
		}
		if attempt > c.Retry.MaxRetries || !ShouldRetryError(err) {
			return nil, err
		}
		retryAfter := ""
		if asAPIError(err, &apiErr) {
			retryAfter = apiErr.Header.Get("retry-after")
		}
		if err := sleepWithConfig(ctx, RetryDelay(attempt, retryAfter, c.Retry), c.Retry); err != nil {
			return nil, err
		}
	}
	return nil, lastErr
}

func (c *Client) doJSONOnce(ctx context.Context, path string, payload any) (json.RawMessage, error) {
	req, dumpTimestamp, err := c.newJSONRequestWithDump(ctx, path, payload)
	if err != nil {
		return nil, err
	}
	resp, err := c.httpClient().Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode > 299 {
		return nil, decodeAPIError(resp)
	}
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, err
	}
	c.dumpResponse(dumpTimestamp, body)
	return body, nil
}

func (c *Client) newJSONRequest(ctx context.Context, path string, payload any) (*http.Request, error) {
	req, _, err := c.newJSONRequestWithDump(ctx, path, payload)
	return req, err
}

func (c *Client) newJSONRequestWithDump(ctx context.Context, path string, payload any) (*http.Request, string, error) {
	data, err := json.Marshal(payload)
	if err != nil {
		return nil, "", err
	}
	dumpTimestamp := c.dumpRequest(path, data)
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.url(path), bytes.NewReader(data))
	if err != nil {
		return nil, "", err
	}
	req.Header.Set("content-type", "application/json")
	req.Header.Set("accept", "application/json")
	req.Header.Set("anthropic-version", c.version())
	betas := MergeBetaHeaders(c.Beta, DynamicBetaHeaders(payload))
	if len(betas) > 0 {
		req.Header.Set("anthropic-beta", BetaHeaderValue(betas))
	}
	for key, values := range c.Headers {
		for _, value := range values {
			req.Header.Add(key, value)
		}
	}
	if c.UserAgent != "" {
		req.Header.Set("user-agent", c.UserAgent)
	}
	if c.APIKey != "" {
		req.Header.Set("x-api-key", c.APIKey)
	}
	if c.AccessToken != "" {
		req.Header.Set("authorization", "Bearer "+c.AccessToken)
	}
	return req, dumpTimestamp, nil
}

func (c *Client) dumpRequest(path string, data []byte) string {
	if c.Dumper == nil || path != "/v1/messages" {
		return ""
	}
	return c.Dumper.DumpRequest(data)
}

func (c *Client) dumpResponse(timestamp string, body json.RawMessage) {
	if c.Dumper == nil || timestamp == "" {
		return
	}
	c.Dumper.DumpResponse(timestamp, append(json.RawMessage(nil), body...))
}

func (c *Client) dumpStreamResponse(timestamp string, chunks []json.RawMessage) {
	if c.Dumper == nil || timestamp == "" {
		return
	}
	c.Dumper.DumpStreamResponse(timestamp, chunks)
}

func asAPIError(err error, target *APIError) bool {
	if err == nil {
		return false
	}
	return errors.As(err, target)
}

func adjustRequestPayloadForContextOverflow(payload any, overflow ContextOverflow) (any, bool) {
	request, ok := payload.(Request)
	if !ok {
		return payload, false
	}
	thinkingBudget := 0
	if request.Thinking != nil && request.Thinking["type"] == "enabled" {
		if budget, ok := intFromAny(request.Thinking["budget_tokens"]); ok {
			thinkingBudget = budget
		}
	}
	adjustedMaxTokens, ok := AdjustMaxTokensForContextOverflow(overflow, thinkingBudget)
	if !ok {
		return payload, false
	}
	request.MaxTokens = adjustedMaxTokens
	return request, true
}

func (c *Client) url(path string) string {
	return strings.TrimRight(c.BaseURL, "/") + path
}

func (c *Client) version() string {
	if c.Version == "" {
		return DefaultVersion
	}
	return c.Version
}

func (c *Client) httpClient() *http.Client {
	if c.HTTPClient == nil {
		return http.DefaultClient
	}
	return c.HTTPClient
}

func validateRequest(request Request) error {
	if strings.TrimSpace(request.Model) == "" {
		return fmt.Errorf("anthropic request missing model")
	}
	if request.MaxTokens <= 0 {
		return fmt.Errorf("anthropic request max_tokens must be positive")
	}
	if len(request.Messages) == 0 {
		return fmt.Errorf("anthropic request missing messages")
	}
	return nil
}
