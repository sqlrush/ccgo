package anthropic

import (
	"context"
	"encoding/json"
	"errors"
	"math"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"ccgo/internal/contracts"
)

type testAccessTokenProvider struct {
	currentToken string
	refreshToken string
	currentCalls int
	refreshCalls int
}

func (p *testAccessTokenProvider) CurrentAccessToken(context.Context) (string, error) {
	p.currentCalls++
	return p.currentToken, nil
}

func (p *testAccessTokenProvider) RefreshAccessToken(context.Context) (string, error) {
	p.refreshCalls++
	p.currentToken = p.refreshToken
	return p.currentToken, nil
}

func TestCreateMessageSendsHeadersAndDecodesResponse(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if got := r.Header.Get("x-api-key"); got != "test-key" {
			t.Fatalf("x-api-key = %q", got)
		}
		if got := r.Header.Get("anthropic-version"); got != DefaultVersion {
			t.Fatalf("version = %q", got)
		}
		var req Request
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			t.Fatal(err)
		}
		if req.Stream {
			t.Fatalf("stream should be false")
		}
		w.Header().Set("content-type", "application/json")
		_, _ = w.Write([]byte(`{"id":"msg_1","type":"message","role":"assistant","model":"sonnet","content":[{"type":"text","text":"hi"}],"usage":{"input_tokens":1,"output_tokens":2}}`))
	}))
	defer server.Close()

	client := NewClient(WithBaseURL(server.URL), WithAPIKey("test-key"))
	resp, err := client.CreateMessage(context.Background(), Request{
		Model:     "sonnet",
		MaxTokens: 32,
		Messages:  []contracts.APIMessage{{Role: "user", Content: []contracts.ContentBlock{contracts.NewTextBlock("hello")}}},
	})
	if err != nil {
		t.Fatal(err)
	}
	if resp.ID != "msg_1" || resp.Content[0].Text != "hi" || resp.Usage.OutputTokens != 2 {
		t.Fatalf("response = %#v", resp)
	}
}

func TestAPIErrorMapping(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("request-id", "req_1")
		w.WriteHeader(http.StatusTooManyRequests)
		_, _ = w.Write([]byte(`{"error":{"type":"rate_limit_error","message":"slow down"}}`))
	}))
	defer server.Close()

	client := NewClient(WithBaseURL(server.URL), WithMaxRetries(0))
	_, err := client.CreateMessage(context.Background(), Request{
		Model:     "sonnet",
		MaxTokens: 32,
		Messages:  []contracts.APIMessage{{Role: "user", Content: []contracts.ContentBlock{contracts.NewTextBlock("hello")}}},
	})
	var apiErr APIError
	if !errors.As(err, &apiErr) {
		t.Fatalf("error = %v, want APIError", err)
	}
	if !apiErr.RateLimited() || !apiErr.Retryable() || apiErr.RequestID != "req_1" {
		t.Fatalf("apiErr = %#v", apiErr)
	}
}

func TestCreateMessageRefreshesOAuthTokenOnUnauthorized(t *testing.T) {
	var authorizations []string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		authorizations = append(authorizations, r.Header.Get("authorization"))
		if len(authorizations) == 1 {
			w.WriteHeader(http.StatusUnauthorized)
			_, _ = w.Write([]byte(`{"error":{"type":"authentication_error","message":"expired token"}}`))
			return
		}
		w.Header().Set("content-type", "application/json")
		_, _ = w.Write([]byte(`{"id":"msg_1","type":"message","role":"assistant","model":"sonnet","content":[{"type":"text","text":"ok"}]}`))
	}))
	defer server.Close()

	provider := &testAccessTokenProvider{currentToken: "stale", refreshToken: "fresh"}
	client := NewClient(WithBaseURL(server.URL), WithAccessTokenProvider(provider), WithMaxRetries(0))
	resp, err := client.CreateMessage(context.Background(), Request{
		Model:     "sonnet",
		MaxTokens: 32,
		Messages:  []contracts.APIMessage{{Role: "user", Content: []contracts.ContentBlock{contracts.NewTextBlock("hello")}}},
	})
	if err != nil {
		t.Fatal(err)
	}
	if resp.Content[0].Text != "ok" || provider.refreshCalls != 1 || provider.currentCalls != 2 || client.AccessToken != "fresh" {
		t.Fatalf("resp=%#v provider=%#v client token=%q", resp, provider, client.AccessToken)
	}
	if strings.Join(authorizations, ",") != "Bearer stale,Bearer fresh" {
		t.Fatalf("authorizations = %#v", authorizations)
	}
}

func TestParseStream(t *testing.T) {
	var events []StreamEvent
	err := ParseStream(strings.NewReader("event: content_block_delta\ndata: {\"type\":\"content_block_delta\",\"index\":0,\"delta\":{\"type\":\"text_delta\",\"text\":\"hi\"}}\n\n"), func(event StreamEvent) error {
		events = append(events, event)
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(events) != 1 || events[0].Type != "content_block_delta" || events[0].TextDelta() != "hi" {
		t.Fatalf("events = %#v", events)
	}
}

func TestStreamMessages(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("content-type", "text/event-stream")
		_, _ = w.Write([]byte("event: message_start\ndata: {\"type\":\"message_start\",\"message\":{\"id\":\"msg_1\",\"type\":\"message\",\"role\":\"assistant\",\"model\":\"sonnet\",\"content\":[]}}\n\n"))
	}))
	defer server.Close()

	client := NewClient(WithBaseURL(server.URL))
	var seen []StreamEvent
	err := client.StreamMessages(context.Background(), Request{
		Model:     "sonnet",
		MaxTokens: 32,
		Messages:  []contracts.APIMessage{{Role: "user", Content: []contracts.ContentBlock{contracts.NewTextBlock("hello")}}},
	}, func(event StreamEvent) error {
		seen = append(seen, event)
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(seen) != 1 || seen[0].Message.ID != "msg_1" {
		t.Fatalf("seen = %#v", seen)
	}
}

func TestStreamMessagesRefreshesOAuthTokenOnUnauthorized(t *testing.T) {
	var authorizations []string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		authorizations = append(authorizations, r.Header.Get("authorization"))
		if len(authorizations) == 1 {
			w.WriteHeader(http.StatusUnauthorized)
			_, _ = w.Write([]byte(`{"error":{"type":"authentication_error","message":"expired token"}}`))
			return
		}
		w.Header().Set("content-type", "text/event-stream")
		_, _ = w.Write([]byte("event: message_start\ndata: {\"type\":\"message_start\",\"message\":{\"id\":\"msg_stream\",\"type\":\"message\",\"role\":\"assistant\",\"model\":\"sonnet\",\"content\":[]}}\n\n"))
	}))
	defer server.Close()

	provider := &testAccessTokenProvider{currentToken: "stale", refreshToken: "fresh"}
	client := NewClient(WithBaseURL(server.URL), WithAccessTokenProvider(provider), WithMaxRetries(0))
	var seen []StreamEvent
	err := client.StreamMessages(context.Background(), Request{
		Model:     "sonnet",
		MaxTokens: 32,
		Messages:  []contracts.APIMessage{{Role: "user", Content: []contracts.ContentBlock{contracts.NewTextBlock("hello")}}},
	}, func(event StreamEvent) error {
		seen = append(seen, event)
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(seen) != 1 || seen[0].Message.ID != "msg_stream" || provider.refreshCalls != 1 || provider.currentCalls != 2 || client.AccessToken != "fresh" {
		t.Fatalf("seen=%#v provider=%#v client token=%q", seen, provider, client.AccessToken)
	}
	if strings.Join(authorizations, ",") != "Bearer stale,Bearer fresh" {
		t.Fatalf("authorizations = %#v", authorizations)
	}
}

func TestStreamAccumulator(t *testing.T) {
	acc := NewStreamAccumulator()
	events := []StreamEvent{
		{Type: "message_start", Message: &Response{ID: "msg_1", Type: "message", Role: "assistant", Model: "sonnet"}},
		{Type: "content_block_start", Index: 0, ContentBlock: &contracts.ContentBlock{Type: contracts.ContentText}},
		{Type: "content_block_delta", Index: 0, Delta: map[string]any{"type": "text_delta", "text": "hel"}},
		{Type: "content_block_delta", Index: 0, Delta: map[string]any{"type": "text_delta", "text": "lo"}},
		{Type: "content_block_start", Index: 1, ContentBlock: &contracts.ContentBlock{Type: contracts.ContentToolUse, ID: "toolu_1", Name: "Echo"}},
		{Type: "content_block_delta", Index: 1, Delta: map[string]any{"type": "input_json_delta", "partial_json": `{"text"`}},
		{Type: "content_block_delta", Index: 1, Delta: map[string]any{"type": "input_json_delta", "partial_json": `:"hi"}`}},
		{Type: "content_block_stop", Index: 1},
		{Type: "message_delta", Delta: map[string]any{"stop_reason": "tool_use"}, Usage: &contracts.Usage{OutputTokens: 5}},
	}
	for _, event := range events {
		if err := acc.Add(event); err != nil {
			t.Fatal(err)
		}
	}
	resp := acc.Finish()
	if resp.Content[0].Text != "hello" || string(resp.Content[1].Input) != `{"text":"hi"}` || resp.StopReason != "tool_use" || resp.Usage.OutputTokens != 5 {
		t.Fatalf("resp = %#v", resp)
	}
}

func TestUsageUpdateAndAccumulate(t *testing.T) {
	current := contracts.Usage{InputTokens: 10, CacheReadInputTokens: 4, OutputTokens: 1}
	updated := UpdateUsage(current, contracts.Usage{InputTokens: 0, CacheReadInputTokens: 0, OutputTokens: 7})
	if updated.InputTokens != 10 || updated.CacheReadInputTokens != 4 || updated.OutputTokens != 7 {
		t.Fatalf("updated = %#v", updated)
	}
	total := AccumulateUsage(updated, contracts.Usage{
		InputTokens:   2,
		OutputTokens:  3,
		ServerToolUse: contracts.ToolUseUsage{WebSearchRequests: 1},
		CacheCreation: contracts.CacheCreationUsage{Ephemeral1hInputTokens: 5},
		ServiceTier:   "standard",
	})
	if total.InputTokens != 12 || total.OutputTokens != 10 || total.ServerToolUse.WebSearchRequests != 1 || total.CacheCreation.Ephemeral1hInputTokens != 5 || total.ServiceTier != "standard" {
		t.Fatalf("total = %#v", total)
	}
}

func TestCalculateUSDCost(t *testing.T) {
	usage := contracts.Usage{
		InputTokens:              1_000_000,
		OutputTokens:             2_000_000,
		CacheReadInputTokens:     3_000_000,
		CacheCreationInputTokens: 4_000_000,
		ServerToolUse:            contracts.ToolUseUsage{WebSearchRequests: 5},
	}
	got := CalculateUSDCost("claude-sonnet-4-6", usage)
	want := 3.0 + 30.0 + 0.9 + 15.0 + 0.05
	if math.Abs(got-want) > 0.0000001 {
		t.Fatalf("cost = %f, want %f", got, want)
	}
	fast := UsageWithCost("claude-opus-4-6", contracts.Usage{InputTokens: 1_000_000, OutputTokens: 1_000_000, Speed: "fast"})
	if fast.CostUSD != 180 {
		t.Fatalf("fast opus cost = %f", fast.CostUSD)
	}
	if pricing := FormatModelPricing(CostHaiku35); pricing != "$0.80/$4 per Mtok" {
		t.Fatalf("pricing = %q", pricing)
	}
	if _, ok := CostsForModel("unknown-model", contracts.Usage{}); ok {
		t.Fatal("unknown model should report ok=false")
	}
}

func TestCreateMessageAddsCostToUsage(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(`{"id":"msg_1","type":"message","role":"assistant","model":"claude-haiku-4-5-20251001","content":[],"usage":{"input_tokens":1000000,"output_tokens":1000000}}`))
	}))
	defer server.Close()

	client := NewClient(WithBaseURL(server.URL))
	resp, err := client.CreateMessage(context.Background(), Request{
		Model:     "haiku",
		MaxTokens: 32,
		Messages:  []contracts.APIMessage{{Role: "user", Content: []contracts.ContentBlock{contracts.NewTextBlock("hello")}}},
	})
	if err != nil {
		t.Fatal(err)
	}
	if resp.Usage.CostUSD != 6 {
		t.Fatalf("cost = %f", resp.Usage.CostUSD)
	}
}

func TestAdjustRequestForNonStreamingCapsThinkingBudget(t *testing.T) {
	req := Request{
		Model:     "sonnet",
		MaxTokens: 100000,
		Messages:  []contracts.APIMessage{{Role: "user", Content: []contracts.ContentBlock{contracts.NewTextBlock("hello")}}},
		Thinking:  map[string]any{"type": "enabled", "budget_tokens": 90000},
	}
	adjusted := AdjustRequestForNonStreaming(req, MaxNonStreamingTokens)
	if adjusted.MaxTokens != MaxNonStreamingTokens {
		t.Fatalf("max tokens = %d", adjusted.MaxTokens)
	}
	if adjusted.Thinking["budget_tokens"] != MaxNonStreamingTokens-1 {
		t.Fatalf("thinking = %#v", adjusted.Thinking)
	}
}

func TestCreateMessageRetriesRetryableErrors(t *testing.T) {
	attempts := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		attempts++
		if attempts == 1 {
			w.Header().Set("retry-after", "2")
			w.WriteHeader(http.StatusTooManyRequests)
			_, _ = w.Write([]byte(`{"error":{"type":"rate_limit_error","message":"slow down"}}`))
			return
		}
		_, _ = w.Write([]byte(`{"id":"msg_1","type":"message","role":"assistant","model":"sonnet","content":[{"type":"text","text":"ok"}]}`))
	}))
	defer server.Close()

	var slept []int64
	client := NewClient(
		WithBaseURL(server.URL),
		WithRetryConfig(RetryConfig{
			MaxRetries:     1,
			BaseDelay:      1,
			MaxDelay:       1,
			JitterFraction: 0,
			Sleep: func(ctx context.Context, d time.Duration) error {
				slept = append(slept, int64(d/time.Second))
				return nil
			},
		}),
	)
	resp, err := client.CreateMessage(context.Background(), Request{
		Model:     "sonnet",
		MaxTokens: 32,
		Messages:  []contracts.APIMessage{{Role: "user", Content: []contracts.ContentBlock{contracts.NewTextBlock("hello")}}},
	})
	if err != nil {
		t.Fatal(err)
	}
	if resp.Content[0].Text != "ok" || attempts != 2 || len(slept) != 1 || slept[0] != 2 {
		t.Fatalf("resp=%#v attempts=%d slept=%v", resp, attempts, slept)
	}
}

func TestCreateMessageObeysShouldRetryFalse(t *testing.T) {
	attempts := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		attempts++
		w.Header().Set("x-should-retry", "false")
		w.WriteHeader(http.StatusInternalServerError)
		_, _ = w.Write([]byte(`{"error":{"type":"api_error","message":"no retry"}}`))
	}))
	defer server.Close()

	client := NewClient(WithBaseURL(server.URL), WithRetryConfig(RetryConfig{MaxRetries: 3, BaseDelay: 1, MaxDelay: 1, Sleep: func(context.Context, time.Duration) error { return nil }}))
	_, err := client.CreateMessage(context.Background(), Request{
		Model:     "sonnet",
		MaxTokens: 32,
		Messages:  []contracts.APIMessage{{Role: "user", Content: []contracts.ContentBlock{contracts.NewTextBlock("hello")}}},
	})
	if err == nil {
		t.Fatal("expected error")
	}
	if attempts != 1 {
		t.Fatalf("attempts = %d", attempts)
	}
}

func TestContextOverflowAdjustsMaxTokensForRetry(t *testing.T) {
	var maxTokens []int
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var req Request
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			t.Fatal(err)
		}
		maxTokens = append(maxTokens, req.MaxTokens)
		if len(maxTokens) == 1 {
			w.WriteHeader(http.StatusBadRequest)
			_, _ = w.Write([]byte(`{"error":{"type":"invalid_request_error","message":"input length and ` + "`max_tokens`" + ` exceed context limit: 188059 + 20000 > 200000"}}`))
			return
		}
		_, _ = w.Write([]byte(`{"id":"msg_1","type":"message","role":"assistant","model":"sonnet","content":[{"type":"text","text":"ok"}]}`))
	}))
	defer server.Close()

	client := NewClient(WithBaseURL(server.URL), WithRetryConfig(RetryConfig{MaxRetries: 1, BaseDelay: 1, MaxDelay: 1, Sleep: func(context.Context, time.Duration) error { return nil }}))
	_, err := client.CreateMessage(context.Background(), Request{
		Model:     "sonnet",
		MaxTokens: 20000,
		Messages:  []contracts.APIMessage{{Role: "user", Content: []contracts.ContentBlock{contracts.NewTextBlock("hello")}}},
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(maxTokens) != 2 || maxTokens[0] != 20000 || maxTokens[1] != 10941 {
		t.Fatalf("maxTokens = %#v", maxTokens)
	}
}

func TestBetaHeadersDedupedAndCustomHeadersApplied(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if got := r.Header.Get("anthropic-beta"); got != "one,two" {
			t.Fatalf("anthropic-beta = %q", got)
		}
		if got := r.Header.Get("x-organization-uuid"); got != "org" {
			t.Fatalf("x-organization-uuid = %q", got)
		}
		_, _ = w.Write([]byte(`{"id":"msg_1","type":"message","role":"assistant","model":"sonnet","content":[]}`))
	}))
	defer server.Close()

	client := NewClient(WithBaseURL(server.URL), WithBeta("one", "two", "one", ""), WithHeader("x-organization-uuid", "org"))
	_, err := client.CreateMessage(context.Background(), Request{
		Model:     "sonnet",
		MaxTokens: 32,
		Messages:  []contracts.APIMessage{{Role: "user", Content: []contracts.ContentBlock{contracts.NewTextBlock("hello")}}},
	})
	if err != nil {
		t.Fatal(err)
	}
}

func TestCreateMessageAddsDynamicCacheBetaHeaders(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if got := r.Header.Get("anthropic-beta"); got != "one,prompt-caching-scope-2024-07-31,cache-editing-2025-01-24" {
			t.Fatalf("anthropic-beta = %q", got)
		}
		w.Header().Set("content-type", "application/json")
		_, _ = w.Write([]byte(`{"id":"msg_1","type":"message","role":"assistant","model":"sonnet","content":[]}`))
	}))
	defer server.Close()

	client := NewClient(WithBaseURL(server.URL), WithBeta("one", PromptCachingScopeBetaHeader))
	_, err := client.CreateMessage(context.Background(), Request{
		Model:     "sonnet",
		MaxTokens: 32,
		Messages: []contracts.APIMessage{{
			Role: "user",
			Content: []contracts.ContentBlock{
				{
					Type:         contracts.ContentText,
					Text:         "hello",
					CacheControl: &contracts.CacheControl{Type: "ephemeral", Scope: "global"},
				},
				{
					Type: contracts.ContentCacheEdits,
					Edits: []contracts.CacheEdit{{
						Type:           "delete",
						CacheReference: "toolu_old",
					}},
				},
			},
		}},
	})
	if err != nil {
		t.Fatal(err)
	}
}

func TestCreateMessageAddsDynamicStrictAndContextBetaHeaders(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if got := r.Header.Get("anthropic-beta"); got != "structured-outputs-2025-11-13,context-1m-2025-08-07" {
			t.Fatalf("anthropic-beta = %q", got)
		}
		w.Header().Set("content-type", "application/json")
		_, _ = w.Write([]byte(`{"id":"msg_1","type":"message","role":"assistant","model":"sonnet","content":[]}`))
	}))
	defer server.Close()

	client := NewClient(WithBaseURL(server.URL), WithBeta(StructuredOutputsBetaHeader))
	_, err := client.CreateMessage(context.Background(), Request{
		Model:     "claude-sonnet-4-6[1m]",
		MaxTokens: 32,
		Messages:  []contracts.APIMessage{{Role: "user", Content: []contracts.ContentBlock{contracts.NewTextBlock("hello")}}},
		Tools: []ToolDefinition{{
			Name:        "Answer",
			Description: "Return a structured answer",
			InputSchema: contracts.JSONSchema{
				"type": "object",
				"properties": map[string]any{
					"answer": map[string]any{"type": "string"},
				},
				"required":             []any{"answer"},
				"additionalProperties": false,
			},
			Strict: true,
		}},
	})
	if err != nil {
		t.Fatal(err)
	}
}

func TestAddCacheBreakpoints(t *testing.T) {
	messages := []contracts.APIMessage{
		{Role: "user", Content: []contracts.ContentBlock{{Type: contracts.ContentToolResult, ToolUseID: "toolu_1"}}},
		{Role: "assistant", Content: []contracts.ContentBlock{{Type: contracts.ContentText, Text: "ok"}}},
		{Role: "user", Content: []contracts.ContentBlock{{Type: contracts.ContentText, Text: "next"}}},
	}
	out := AddCacheBreakpoints(messages, true, CacheBreakpointOptions{
		CacheControl: contracts.CacheControl{Type: "ephemeral", Scope: "global", TTL: "1h"},
		NewCacheEdits: []contracts.CacheEdit{
			{Type: "delete", CacheReference: "old"},
			{Type: "delete", CacheReference: "old"},
		},
	})
	if out[2].Content[0].CacheControl == nil || out[2].Content[0].CacheControl.Scope != "global" {
		t.Fatalf("cache control missing: %#v", out[2].Content)
	}
	if out[0].Content[0].CacheReference != "toolu_1" {
		t.Fatalf("cache reference missing: %#v", out[0].Content[0])
	}
	if len(out[2].Content) != 2 || out[2].Content[1].Type != contracts.ContentCacheEdits || len(out[2].Content[1].Edits) != 1 {
		t.Fatalf("cache edits not inserted/deduped: %#v", out[2].Content)
	}
	if messages[0].Content[0].CacheReference != "" {
		t.Fatalf("input messages were mutated: %#v", messages)
	}
}
