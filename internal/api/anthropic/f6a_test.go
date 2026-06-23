package anthropic

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"
	"time"

	"ccgo/internal/contracts"
)

// ---------------------------------------------------------------------------
// LOOP-07: interleaved-thinking-2025-05-14 beta auto-added when thinking set
// ---------------------------------------------------------------------------

func TestDynamicBetaHeadersIncludesInterleavedThinkingWhenThinkingSet(t *testing.T) {
	req := Request{
		Model:     "claude-opus-4-5-20251101",
		MaxTokens: 16384,
		Messages: []contracts.APIMessage{{
			Role:    "user",
			Content: []contracts.ContentBlock{contracts.NewTextBlock("think")},
		}},
		Thinking: map[string]any{"type": "enabled", "budget_tokens": 8000},
	}
	betas := DynamicBetaHeaders(req)
	found := false
	for _, b := range betas {
		if b == InterleavedThinkingBetaHeader {
			found = true
		}
	}
	if !found {
		t.Fatalf("expected %q in dynamic betas when Thinking is set, got %v",
			InterleavedThinkingBetaHeader, betas)
	}
}

func TestDynamicBetaHeadersOmitsInterleavedThinkingWhenNoThinking(t *testing.T) {
	req := Request{
		Model:     "claude-opus-4-5-20251101",
		MaxTokens: 16384,
		Messages: []contracts.APIMessage{{
			Role:    "user",
			Content: []contracts.ContentBlock{contracts.NewTextBlock("hi")},
		}},
	}
	betas := DynamicBetaHeaders(req)
	for _, b := range betas {
		if b == InterleavedThinkingBetaHeader {
			t.Fatalf("interleaved-thinking beta should be absent when Thinking is nil, got %v", betas)
		}
	}
}

// ---------------------------------------------------------------------------
// LOOP-08: redacted_thinking block — constant, MarshalJSON, NormalizeForAPI
// ---------------------------------------------------------------------------

func TestRedactedThinkingConstantValue(t *testing.T) {
	const want = "redacted_thinking"
	if string(contracts.ContentRedactedThinking) != want {
		t.Fatalf("ContentRedactedThinking = %q want %q", contracts.ContentRedactedThinking, want)
	}
}

func TestRedactedThinkingBlockMarshalRoundTrip(t *testing.T) {
	block := contracts.ContentBlock{
		Type: contracts.ContentRedactedThinking,
		Data: "opaque-redacted-data",
	}
	data, err := json.Marshal(block)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	var decoded contracts.ContentBlock
	if err := json.Unmarshal(data, &decoded); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if decoded.Type != contracts.ContentRedactedThinking {
		t.Fatalf("type = %q want %q", decoded.Type, contracts.ContentRedactedThinking)
	}
	if decoded.Data != "opaque-redacted-data" {
		t.Fatalf("data = %q want %q", decoded.Data, "opaque-redacted-data")
	}
}

func TestRedactedThinkingPreservedInRequest(t *testing.T) {
	// Verify that a redacted_thinking block is serialised into the request body
	// so it can be round-tripped back to the API without triggering a 400.
	var captured map[string]any
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if err := json.NewDecoder(r.Body).Decode(&captured); err != nil {
			w.WriteHeader(http.StatusBadRequest)
			return
		}
		w.Header().Set("content-type", "application/json")
		json.NewEncoder(w).Encode(Response{
			ID:         "msg_test",
			Type:       "message",
			Role:       "assistant",
			StopReason: "end_turn",
			Content:    []contracts.ContentBlock{contracts.NewTextBlock("ok")},
		})
	}))
	defer srv.Close()

	c := NewClient(WithBaseURL(srv.URL), WithAPIKey("test"))
	req := Request{
		Model:     "claude-opus-4-5-20251101",
		MaxTokens: 1024,
		Messages: []contracts.APIMessage{
			{Role: "user", Content: []contracts.ContentBlock{contracts.NewTextBlock("hi")}},
			{Role: "assistant", Content: []contracts.ContentBlock{
				{Type: contracts.ContentRedactedThinking, Data: "secret"},
				contracts.NewTextBlock("ok"),
			}},
			{Role: "user", Content: []contracts.ContentBlock{contracts.NewTextBlock("continue")}},
		},
	}
	if _, err := c.CreateMessage(context.Background(), req); err != nil {
		t.Fatalf("CreateMessage: %v", err)
	}
	// Verify the redacted_thinking block was sent to the server.
	msgs, ok := captured["messages"].([]any)
	if !ok || len(msgs) < 2 {
		t.Fatalf("unexpected messages in request: %v", captured["messages"])
	}
	assistantMsg, ok := msgs[1].(map[string]any)
	if !ok {
		t.Fatalf("assistant message not a map: %v", msgs[1])
	}
	content, ok := assistantMsg["content"].([]any)
	if !ok || len(content) == 0 {
		t.Fatalf("content missing: %v", assistantMsg)
	}
	firstBlock, ok := content[0].(map[string]any)
	if !ok {
		t.Fatalf("first block not map: %v", content[0])
	}
	if firstBlock["type"] != "redacted_thinking" {
		t.Fatalf("type = %v want redacted_thinking", firstBlock["type"])
	}
	if firstBlock["data"] != "secret" {
		t.Fatalf("data = %v want secret", firstBlock["data"])
	}
}

// ---------------------------------------------------------------------------
// LOOP-35: context_management request field + context-management-2025-06-27 beta
// ---------------------------------------------------------------------------

func TestContextManagementBetaHeaderConstant(t *testing.T) {
	const want = "context-management-2025-06-27"
	if ContextManagementBetaHeader != want {
		t.Fatalf("ContextManagementBetaHeader = %q want %q", ContextManagementBetaHeader, want)
	}
}

func TestRequestContextManagementFieldSerialises(t *testing.T) {
	req := Request{
		Model:     "claude-opus-4-5-20251101",
		MaxTokens: 1024,
		Messages: []contracts.APIMessage{{
			Role:    "user",
			Content: []contracts.ContentBlock{contracts.NewTextBlock("hi")},
		}},
		ContextManagement: map[string]any{
			"edits": []any{
				map[string]any{"type": "clear_thinking_20251015", "keep": "all"},
			},
		},
	}
	data, err := json.Marshal(req)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	var decoded map[string]any
	if err := json.Unmarshal(data, &decoded); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if _, ok := decoded["context_management"]; !ok {
		t.Fatalf("context_management missing from serialised request: %s", data)
	}
}

func TestDynamicBetaHeadersIncludesContextManagementWhenSet(t *testing.T) {
	req := Request{
		Model:     "claude-opus-4-5-20251101",
		MaxTokens: 1024,
		Messages: []contracts.APIMessage{{
			Role:    "user",
			Content: []contracts.ContentBlock{contracts.NewTextBlock("hi")},
		}},
		ContextManagement: map[string]any{"edits": []any{}},
	}
	betas := DynamicBetaHeaders(req)
	found := false
	for _, b := range betas {
		if b == ContextManagementBetaHeader {
			found = true
		}
	}
	if !found {
		t.Fatalf("expected %q in betas when ContextManagement set, got %v",
			ContextManagementBetaHeader, betas)
	}
}

func TestDynamicBetaHeadersOmitsContextManagementWhenNotSet(t *testing.T) {
	req := Request{
		Model:     "claude-opus-4-5-20251101",
		MaxTokens: 1024,
		Messages: []contracts.APIMessage{{
			Role:    "user",
			Content: []contracts.ContentBlock{contracts.NewTextBlock("hi")},
		}},
	}
	betas := DynamicBetaHeaders(req)
	for _, b := range betas {
		if b == ContextManagementBetaHeader {
			t.Fatalf("context-management beta should be absent when ContextManagement is nil, got %v", betas)
		}
	}
}

// ---------------------------------------------------------------------------
// LOOP-36: task_budget (output_config.task_budget) + task-budgets-2026-03-13 beta
// ---------------------------------------------------------------------------

func TestTaskBudgetsBetaHeaderConstant(t *testing.T) {
	const want = "task-budgets-2026-03-13"
	if TaskBudgetsBetaHeader != want {
		t.Fatalf("TaskBudgetsBetaHeader = %q want %q", TaskBudgetsBetaHeader, want)
	}
}

func TestRequestOutputConfigTaskBudgetSerialises(t *testing.T) {
	req := Request{
		Model:     "claude-opus-4-5-20251101",
		MaxTokens: 1024,
		Messages: []contracts.APIMessage{{
			Role:    "user",
			Content: []contracts.ContentBlock{contracts.NewTextBlock("hi")},
		}},
		OutputConfig: map[string]any{
			"task_budget": map[string]any{
				"type":      "tokens",
				"total":     50000,
				"remaining": 45000,
			},
		},
	}
	data, err := json.Marshal(req)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	var decoded map[string]any
	if err := json.Unmarshal(data, &decoded); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	oc, ok := decoded["output_config"].(map[string]any)
	if !ok {
		t.Fatalf("output_config missing: %s", data)
	}
	if _, ok := oc["task_budget"]; !ok {
		t.Fatalf("task_budget missing from output_config: %v", oc)
	}
}

func TestDynamicBetaHeadersIncludesTaskBudgetsWhenSet(t *testing.T) {
	req := Request{
		Model:     "claude-opus-4-5-20251101",
		MaxTokens: 1024,
		Messages: []contracts.APIMessage{{
			Role:    "user",
			Content: []contracts.ContentBlock{contracts.NewTextBlock("hi")},
		}},
		OutputConfig: map[string]any{
			"task_budget": map[string]any{"type": "tokens", "total": 50000},
		},
	}
	betas := DynamicBetaHeaders(req)
	found := false
	for _, b := range betas {
		if b == TaskBudgetsBetaHeader {
			found = true
		}
	}
	if !found {
		t.Fatalf("expected %q in betas when task_budget set, got %v", TaskBudgetsBetaHeader, betas)
	}
}

func TestDynamicBetaHeadersOmitsTaskBudgetsWhenNotSet(t *testing.T) {
	req := Request{
		Model:     "claude-opus-4-5-20251101",
		MaxTokens: 1024,
		Messages: []contracts.APIMessage{{
			Role:    "user",
			Content: []contracts.ContentBlock{contracts.NewTextBlock("hi")},
		}},
	}
	betas := DynamicBetaHeaders(req)
	for _, b := range betas {
		if b == TaskBudgetsBetaHeader {
			t.Fatalf("task-budgets beta should be absent when OutputConfig has no task_budget, got %v", betas)
		}
	}
}

// ---------------------------------------------------------------------------
// LOOP-48: CLAUDE_CODE_EXTRA_BODY env var merging into request body
// ---------------------------------------------------------------------------

func TestExtraBodyMergesIntoRequest(t *testing.T) {
	t.Setenv("CLAUDE_CODE_EXTRA_BODY", `{"custom_key":"custom_value","another":42}`)

	var captured map[string]any
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		json.NewDecoder(r.Body).Decode(&captured)
		w.Header().Set("content-type", "application/json")
		json.NewEncoder(w).Encode(Response{
			ID:         "msg_test",
			Type:       "message",
			Role:       "assistant",
			StopReason: "end_turn",
			Content:    []contracts.ContentBlock{contracts.NewTextBlock("ok")},
		})
	}))
	defer srv.Close()

	c := NewClient(WithBaseURL(srv.URL), WithAPIKey("test"))
	_, err := c.CreateMessage(context.Background(), Request{
		Model:     "claude-opus-4-5-20251101",
		MaxTokens: 1024,
		Messages: []contracts.APIMessage{{
			Role:    "user",
			Content: []contracts.ContentBlock{contracts.NewTextBlock("hi")},
		}},
	})
	if err != nil {
		t.Fatalf("CreateMessage: %v", err)
	}
	if captured["custom_key"] != "custom_value" {
		t.Fatalf("custom_key missing from request body: %v", captured)
	}
	if v, ok := captured["another"].(float64); !ok || v != 42 {
		t.Fatalf("another = %v want 42", captured["another"])
	}
}

func TestExtraBodyIgnoresNonObjectJSON(t *testing.T) {
	t.Setenv("CLAUDE_CODE_EXTRA_BODY", `["not","an","object"]`)

	var captured map[string]any
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		json.NewDecoder(r.Body).Decode(&captured)
		w.Header().Set("content-type", "application/json")
		json.NewEncoder(w).Encode(Response{
			ID:         "msg_test",
			Type:       "message",
			Role:       "assistant",
			StopReason: "end_turn",
			Content:    []contracts.ContentBlock{contracts.NewTextBlock("ok")},
		})
	}))
	defer srv.Close()

	c := NewClient(WithBaseURL(srv.URL), WithAPIKey("test"))
	if _, err := c.CreateMessage(context.Background(), Request{
		Model:     "claude-opus-4-5-20251101",
		MaxTokens: 1024,
		Messages: []contracts.APIMessage{{
			Role:    "user",
			Content: []contracts.ContentBlock{contracts.NewTextBlock("hi")},
		}},
	}); err != nil {
		t.Fatalf("CreateMessage: %v", err)
	}
	// Non-object JSON should be ignored — model key must still be present but
	// no unexpected extra keys from the bad env value.
	if _, ok := captured["model"]; !ok {
		t.Fatalf("model missing from request body: %v", captured)
	}
}

func TestExtraBodyIgnoresInvalidJSON(t *testing.T) {
	t.Setenv("CLAUDE_CODE_EXTRA_BODY", `{invalid json`)

	var captured map[string]any
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		json.NewDecoder(r.Body).Decode(&captured)
		w.Header().Set("content-type", "application/json")
		json.NewEncoder(w).Encode(Response{
			ID:         "msg_test",
			Type:       "message",
			Role:       "assistant",
			StopReason: "end_turn",
			Content:    []contracts.ContentBlock{contracts.NewTextBlock("ok")},
		})
	}))
	defer srv.Close()

	c := NewClient(WithBaseURL(srv.URL), WithAPIKey("test"))
	if _, err := c.CreateMessage(context.Background(), Request{
		Model:     "claude-opus-4-5-20251101",
		MaxTokens: 1024,
		Messages: []contracts.APIMessage{{
			Role:    "user",
			Content: []contracts.ContentBlock{contracts.NewTextBlock("hi")},
		}},
	}); err != nil {
		t.Fatalf("CreateMessage: %v", err)
	}
	if _, ok := captured["model"]; !ok {
		t.Fatalf("model missing from request body: %v", captured)
	}
}

func TestExtraBodyEmptyEnvHasNoEffect(t *testing.T) {
	os.Unsetenv("CLAUDE_CODE_EXTRA_BODY")
	extra := getExtraBody()
	if len(extra) != 0 {
		t.Fatalf("expected empty extra body when env not set, got %v", extra)
	}
}

// ---------------------------------------------------------------------------
// LOOP-49: media truncation — StripExcessMediaItems
// ---------------------------------------------------------------------------

func TestStripExcessMediaItemsWithinLimit(t *testing.T) {
	msgs := makeMediaMessages(3)
	out := StripExcessMediaItems(msgs, 100)
	if len(out) != len(msgs) {
		t.Fatalf("expected no change when within limit")
	}
	totalMedia := countMediaInMessages(out)
	if totalMedia != 3 {
		t.Fatalf("media count = %d want 3", totalMedia)
	}
}

func TestStripExcessMediaItemsExceedsLimit(t *testing.T) {
	msgs := makeMediaMessages(101)
	out := StripExcessMediaItems(msgs, 100)
	total := countMediaInMessages(out)
	if total > 100 {
		t.Fatalf("media count = %d, want <= 100", total)
	}
}

func TestStripExcessMediaItemsZeroMedia(t *testing.T) {
	msgs := []contracts.APIMessage{
		{Role: "user", Content: []contracts.ContentBlock{contracts.NewTextBlock("hi")}},
	}
	out := StripExcessMediaItems(msgs, 100)
	if len(out) != 1 {
		t.Fatalf("expected 1 message, got %d", len(out))
	}
}

func TestStripExcessMediaItemsNestedInToolResult(t *testing.T) {
	// Image nested inside a tool_result content block.
	img := contracts.ContentBlock{
		Type: contracts.ContentImage,
		Source: contracts.ImageSource{Type: "base64", MediaType: "image/png", Data: "abc"},
	}
	msgs := []contracts.APIMessage{
		{
			Role: "user",
			Content: []contracts.ContentBlock{
				{
					Type:      contracts.ContentToolResult,
					ToolUseID: "id1",
					Content: []contracts.ContentBlock{
						img, img, img,
					},
				},
			},
		},
	}
	// Count nested media
	total := countMediaInMessages(msgs)
	if total != 3 {
		t.Fatalf("pre-strip media = %d want 3", total)
	}

	// Limit to 1 — 2 oldest should be stripped.
	out := StripExcessMediaItems(msgs, 1)
	after := countMediaInMessages(out)
	if after > 1 {
		t.Fatalf("post-strip media = %d, want <= 1", after)
	}
}

func TestAPIMaxMediaPerRequestConstant(t *testing.T) {
	const want = 100
	if APIMaxMediaPerRequest != want {
		t.Fatalf("APIMaxMediaPerRequest = %d want %d", APIMaxMediaPerRequest, want)
	}
}

// ---------------------------------------------------------------------------
// LOOP-50: non-streaming fallback timeout
// ---------------------------------------------------------------------------

func TestNonstreamingFallbackTimeoutRemote(t *testing.T) {
	t.Setenv("CLAUDE_CODE_REMOTE", "1")
	got := NonstreamingFallbackTimeout()
	want := 120 * time.Second
	if got != want {
		t.Fatalf("remote timeout = %v want %v", got, want)
	}
}

func TestNonstreamingFallbackTimeoutLocal(t *testing.T) {
	os.Unsetenv("CLAUDE_CODE_REMOTE")
	got := NonstreamingFallbackTimeout()
	want := 300 * time.Second
	if got != want {
		t.Fatalf("local timeout = %v want %v", got, want)
	}
}

func TestNonstreamingFallbackTimeoutEnvOverride(t *testing.T) {
	t.Setenv("API_TIMEOUT_MS", "5000")
	os.Unsetenv("CLAUDE_CODE_REMOTE")
	got := NonstreamingFallbackTimeout()
	want := 5 * time.Second
	if got != want {
		t.Fatalf("env override timeout = %v want %v", got, want)
	}
}

// ---------------------------------------------------------------------------
// Helpers
// ---------------------------------------------------------------------------

func makeMediaMessages(count int) []contracts.APIMessage {
	msgs := make([]contracts.APIMessage, 0, count)
	for i := 0; i < count; i++ {
		msgs = append(msgs, contracts.APIMessage{
			Role: "user",
			Content: []contracts.ContentBlock{
				contracts.NewBase64ImageBlock("image/png", strings.Repeat("A", 10)),
			},
		})
	}
	return msgs
}

func countMediaInMessages(msgs []contracts.APIMessage) int {
	count := 0
	for _, msg := range msgs {
		for _, block := range msg.Content {
			if block.Type == contracts.ContentImage {
				count++
			}
			if block.Type == contracts.ContentToolResult {
				if blocks, ok := block.Content.([]contracts.ContentBlock); ok {
					for _, b := range blocks {
						if b.Type == contracts.ContentImage {
							count++
						}
					}
				}
			}
		}
	}
	return count
}
