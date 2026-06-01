package conversation

import (
	"context"
	"encoding/json"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"ccgo/internal/api/anthropic"
	"ccgo/internal/contracts"
	"ccgo/internal/messages"
	"ccgo/internal/session"
	"ccgo/internal/tool"
	filetools "ccgo/internal/tools/file"
)

type fakeCall struct {
	response *anthropic.Response
	err      error
}

type fakeClient struct {
	calls    []fakeCall
	requests []anthropic.Request
	streams  [][]anthropic.StreamEvent
}

func (f *fakeClient) CreateMessage(ctx context.Context, req anthropic.Request) (*anthropic.Response, error) {
	f.requests = append(f.requests, req)
	if len(f.calls) == 0 {
		return nil, anthropic.APIError{StatusCode: http.StatusInternalServerError, Type: "test_error", Message: "no fake call configured"}
	}
	call := f.calls[0]
	f.calls = f.calls[1:]
	return call.response, call.err
}

func (f *fakeClient) StreamMessages(ctx context.Context, req anthropic.Request, handle func(anthropic.StreamEvent) error) error {
	f.requests = append(f.requests, req)
	if len(f.streams) == 0 {
		return anthropic.APIError{StatusCode: http.StatusInternalServerError, Type: "test_error", Message: "no fake stream configured"}
	}
	events := f.streams[0]
	f.streams = f.streams[1:]
	for _, event := range events {
		if err := handle(event); err != nil {
			return err
		}
	}
	return nil
}

func TestRunnerExecutesToolUseAndContinuesConversation(t *testing.T) {
	registry, err := tool.NewRegistry(tool.FuncTool{
		DefinitionValue: contracts.ToolDefinition{
			Name:        "Echo",
			Description: "echoes text",
			ReadOnly:    true,
			InputSchema: contracts.JSONSchema{
				"type":     "object",
				"required": []any{"text"},
				"properties": map[string]any{
					"text": map[string]any{"type": "string"},
				},
			},
		},
		CallFunc: func(ctx tool.Context, raw json.RawMessage, sink tool.ProgressSink) (contracts.ToolResult, error) {
			var input struct {
				Text string `json:"text"`
			}
			if err := json.Unmarshal(raw, &input); err != nil {
				return contracts.ToolResult{}, err
			}
			return contracts.ToolResult{Content: "echo:" + input.Text}, nil
		},
	})
	if err != nil {
		t.Fatal(err)
	}

	client := &fakeClient{calls: []fakeCall{
		{response: &anthropic.Response{
			ID:         "msg_tool",
			Type:       "message",
			Role:       "assistant",
			Model:      "sonnet",
			StopReason: "tool_use",
			Content: []contracts.ContentBlock{{
				Type:  contracts.ContentToolUse,
				ID:    "toolu_1",
				Name:  "Echo",
				Input: json.RawMessage(`{"text":"hello"}`),
			}},
		}},
		{response: &anthropic.Response{
			ID:         "msg_done",
			Type:       "message",
			Role:       "assistant",
			Model:      "sonnet",
			StopReason: "end_turn",
			Content:    []contracts.ContentBlock{contracts.NewTextBlock("done")},
			Usage:      contracts.Usage{InputTokens: 5, OutputTokens: 2},
		}},
	}}
	transcriptPath := filepath.Join(t.TempDir(), "session.jsonl")
	runner := Runner{
		Client:      client,
		Tools:       tool.NewExecutor(registry),
		Model:       "sonnet",
		MaxTokens:   128,
		SessionID:   "sess_1",
		SessionPath: transcriptPath,
	}

	result, err := runner.RunTurn(context.Background(), nil, messages.UserText("run echo"))
	if err != nil {
		t.Fatal(err)
	}
	if result.Assistant.Content[0].Text != "done" {
		t.Fatalf("assistant = %#v", result.Assistant)
	}
	if len(result.ToolResults) != 1 || result.ToolResults[0].Content != "echo:hello" {
		t.Fatalf("tool results = %#v", result.ToolResults)
	}
	if len(client.requests) != 2 {
		t.Fatalf("requests = %d, want 2", len(client.requests))
	}
	last := client.requests[1].Messages[len(client.requests[1].Messages)-1]
	if last.Role != "user" || last.Content[0].Type != contracts.ContentToolResult || last.Content[0].ToolUseID != "toolu_1" {
		t.Fatalf("last api message = %#v", last)
	}
	entries, err := session.Load(transcriptPath)
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 4 {
		t.Fatalf("transcript entries = %d, want 4", len(entries))
	}
	if entries[1].Message.ParentUUID == nil {
		t.Fatalf("assistant transcript entry missing parent")
	}
}

func TestRunnerAppliesToolResultBudgetBeforeNextRequest(t *testing.T) {
	registry, err := tool.NewRegistry(tool.FuncTool{
		DefinitionValue: contracts.ToolDefinition{Name: "Big", ReadOnly: true},
		CallFunc: func(ctx tool.Context, raw json.RawMessage, sink tool.ProgressSink) (contracts.ToolResult, error) {
			return contracts.ToolResult{Content: "0123456789abcdef"}, nil
		},
	})
	if err != nil {
		t.Fatal(err)
	}

	client := &fakeClient{calls: []fakeCall{
		{response: &anthropic.Response{
			ID:         "msg_tool",
			Type:       "message",
			Role:       "assistant",
			Model:      "sonnet",
			StopReason: "tool_use",
			Content: []contracts.ContentBlock{{
				Type:  contracts.ContentToolUse,
				ID:    "toolu_big",
				Name:  "Big",
				Input: json.RawMessage(`{}`),
			}},
		}},
		{response: &anthropic.Response{
			ID:         "msg_done",
			Type:       "message",
			Role:       "assistant",
			Model:      "sonnet",
			StopReason: "end_turn",
			Content:    []contracts.ContentBlock{contracts.NewTextBlock("done")},
		}},
	}}
	transcriptPath := filepath.Join(t.TempDir(), "session.jsonl")
	runner := Runner{
		Client:           client,
		Tools:            tool.NewExecutor(registry),
		Model:            "sonnet",
		MaxTokens:        128,
		SessionID:        "sess_budget",
		SessionPath:      transcriptPath,
		ContentBudget:    session.NewContentReplacementState(),
		ContentBudgetMax: 8,
	}

	if _, err := runner.RunTurn(context.Background(), nil, messages.UserText("run big")); err != nil {
		t.Fatal(err)
	}
	if len(client.requests) != 2 {
		t.Fatalf("requests = %d, want 2", len(client.requests))
	}
	last := client.requests[1].Messages[len(client.requests[1].Messages)-1]
	got, _ := last.Content[0].Content.(string)
	if !strings.HasPrefix(got, session.PersistedOutputTag) {
		t.Fatalf("tool result was not replaced in request: %#v", got)
	}
	transcript, err := session.LoadTranscript(transcriptPath)
	if err != nil {
		t.Fatal(err)
	}
	records := transcript.ContentReplacements["sess_budget"]
	if len(records) != 1 || records[0].ToolUseID != "toolu_big" {
		t.Fatalf("content replacement records = %#v", records)
	}
	persistedPath := filepath.Join(filepath.Dir(transcriptPath), "sess_budget", "tool-results", "toolu_big.txt")
	data, err := os.ReadFile(persistedPath)
	if err != nil {
		t.Fatal(err)
	}
	if string(data) != "0123456789abcdef" {
		t.Fatalf("persisted content = %q", string(data))
	}
}

func TestRunnerPreservesToolMetadataAcrossRounds(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "note.txt")
	if err := os.WriteFile(path, []byte("old\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	registry, err := tool.NewRegistry(filetools.BuiltinTools()...)
	if err != nil {
		t.Fatal(err)
	}
	client := &fakeClient{calls: []fakeCall{
		{response: &anthropic.Response{
			ID:         "msg_read",
			Type:       "message",
			Role:       "assistant",
			Model:      "sonnet",
			StopReason: "tool_use",
			Content: []contracts.ContentBlock{{
				Type:  contracts.ContentToolUse,
				ID:    "toolu_read",
				Name:  "Read",
				Input: json.RawMessage(`{"file_path":"note.txt"}`),
			}},
		}},
		{response: &anthropic.Response{
			ID:         "msg_edit",
			Type:       "message",
			Role:       "assistant",
			Model:      "sonnet",
			StopReason: "tool_use",
			Content: []contracts.ContentBlock{{
				Type:  contracts.ContentToolUse,
				ID:    "toolu_edit",
				Name:  "Edit",
				Input: json.RawMessage(`{"file_path":"note.txt","old_string":"old","new_string":"new"}`),
			}},
		}},
		{response: &anthropic.Response{
			ID:         "msg_done",
			Type:       "message",
			Role:       "assistant",
			Model:      "sonnet",
			StopReason: "end_turn",
			Content:    []contracts.ContentBlock{contracts.NewTextBlock("done")},
		}},
	}}
	runner := Runner{
		Client:           client,
		Tools:            tool.NewExecutor(registry),
		Model:            "sonnet",
		MaxTokens:        128,
		WorkingDirectory: dir,
	}
	if _, err := runner.RunTurn(context.Background(), nil, messages.UserText("read then edit")); err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if string(data) != "new\n" {
		t.Fatalf("edited content = %q", data)
	}
}

func TestRunnerFallsBackOnRetryableAPIError(t *testing.T) {
	client := &fakeClient{calls: []fakeCall{
		{err: anthropic.APIError{StatusCode: http.StatusInternalServerError, Type: "overloaded_error", Message: "try later"}},
		{response: &anthropic.Response{
			ID:         "msg_1",
			Type:       "message",
			Role:       "assistant",
			Model:      "haiku",
			StopReason: "end_turn",
			Content:    []contracts.ContentBlock{contracts.NewTextBlock("fallback ok")},
		}},
	}}
	runner := Runner{
		Client:         client,
		Model:          "sonnet",
		FallbackModels: []string{"haiku"},
		MaxTokens:      64,
	}
	result, err := runner.RunTurn(context.Background(), nil, messages.UserText("hello"))
	if err != nil {
		t.Fatal(err)
	}
	if result.Assistant.Model != "haiku" {
		t.Fatalf("assistant model = %q", result.Assistant.Model)
	}
	if len(client.requests) != 2 || client.requests[0].Model != "sonnet" || client.requests[1].Model != "haiku" {
		t.Fatalf("requests = %#v", client.requests)
	}
}

func TestBuildRequestIncludesToolDefinitions(t *testing.T) {
	registry, err := tool.NewRegistry(tool.FuncTool{
		DefinitionValue: contracts.ToolDefinition{
			Name:        "Read",
			Description: "read a file",
			ReadOnly:    true,
			InputSchema: contracts.JSONSchema{"type": "object"},
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	runner := Runner{
		Tools:     tool.NewExecutor(registry),
		Model:     "sonnet",
		MaxTokens: 100,
	}
	req, err := runner.BuildRequest([]contracts.Message{messages.UserText("hi")}, "sonnet")
	if err != nil {
		t.Fatal(err)
	}
	if len(req.Tools) != 1 || req.Tools[0].Name != "Read" || req.Tools[0].InputSchema["type"] != "object" {
		t.Fatalf("tools = %#v", req.Tools)
	}
}

func TestRunnerCanUseStreamingClient(t *testing.T) {
	client := &fakeClient{streams: [][]anthropic.StreamEvent{{
		{Type: "message_start", Message: &anthropic.Response{ID: "msg_1", Type: "message", Role: "assistant", Model: "sonnet"}},
		{Type: "content_block_start", Index: 0, ContentBlock: &contracts.ContentBlock{Type: contracts.ContentText}},
		{Type: "content_block_delta", Index: 0, Delta: map[string]any{"type": "text_delta", "text": "streamed"}},
		{Type: "message_delta", Delta: map[string]any{"stop_reason": "end_turn"}, Usage: &contracts.Usage{OutputTokens: 1}},
	}}}
	runner := Runner{
		Client:       client,
		Model:        "sonnet",
		MaxTokens:    64,
		UseStreaming: true,
	}
	result, err := runner.RunTurn(context.Background(), nil, messages.UserText("hello"))
	if err != nil {
		t.Fatal(err)
	}
	if result.Assistant.Content[0].Text != "streamed" || !client.requests[0].Stream {
		t.Fatalf("result = %#v request = %#v", result, client.requests[0])
	}
}
