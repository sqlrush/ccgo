package conversation

import (
	"context"
	"encoding/json"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"

	"ccgo/internal/api/anthropic"
	compactpkg "ccgo/internal/compact"
	"ccgo/internal/contracts"
	"ccgo/internal/memory"
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

func TestRunnerAppendsToolNewMessagesAfterToolResult(t *testing.T) {
	registry, err := tool.NewRegistry(tool.FuncTool{
		DefinitionValue: contracts.ToolDefinition{Name: "Meta", ReadOnly: true},
		CallFunc: func(ctx tool.Context, raw json.RawMessage, sink tool.ProgressSink) (contracts.ToolResult, error) {
			return contracts.ToolResult{
				Content: "launched",
				NewMessages: []contracts.Message{{
					Type:    contracts.MessageUser,
					IsMeta:  true,
					Content: []contracts.ContentBlock{contracts.NewTextBlock("meta skill content")},
				}},
			}, nil
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
				ID:    "toolu_meta",
				Name:  "Meta",
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
		Client:      client,
		Tools:       tool.NewExecutor(registry),
		Model:       "sonnet",
		MaxTokens:   128,
		SessionID:   "sess_meta",
		SessionPath: transcriptPath,
	}

	result, err := runner.RunTurn(context.Background(), nil, messages.UserText("run meta"))
	if err != nil {
		t.Fatal(err)
	}
	if len(result.ToolResults) != 1 || result.ToolResults[0].Content != "launched" {
		t.Fatalf("tool results = %#v", result.ToolResults)
	}
	if len(client.requests) != 2 {
		t.Fatalf("requests = %d, want 2", len(client.requests))
	}
	second := client.requests[1].Messages
	if len(second) < 2 {
		t.Fatalf("second request messages = %#v", second)
	}
	toolResult := second[len(second)-2]
	meta := second[len(second)-1]
	if toolResult.Role != "user" || toolResult.Content[0].Type != contracts.ContentToolResult || toolResult.Content[0].ToolUseID != "toolu_meta" {
		t.Fatalf("tool result api message = %#v", toolResult)
	}
	if meta.Role != "user" || meta.Content[0].Text != "meta skill content" {
		t.Fatalf("meta api message = %#v", meta)
	}
	entries, err := session.Load(transcriptPath)
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 5 {
		t.Fatalf("transcript entries = %d, want 5", len(entries))
	}
	if !entries[3].Message.IsMeta || entries[3].Message.SessionID != "sess_meta" {
		t.Fatalf("meta transcript entry = %#v", entries[3].Message)
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

func TestRunnerAutoCompactsBeforeMainRequest(t *testing.T) {
	client := &fakeClient{calls: []fakeCall{
		{response: &anthropic.Response{
			ID:         "msg_summary",
			Type:       "message",
			Role:       "assistant",
			Model:      "sonnet",
			StopReason: "end_turn",
			Content:    []contracts.ContentBlock{contracts.NewTextBlock("summary text")},
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
	var events []EventType
	transcriptPath := filepath.Join(t.TempDir(), "session.jsonl")
	runner := Runner{
		Client:      client,
		Model:       "sonnet",
		MaxTokens:   128,
		SessionID:   "sess_compact",
		SessionPath: transcriptPath,
		AutoCompact: &compactpkg.AutoConfig{
			Enabled:  true,
			Force:    true,
			KeepLast: 1,
		},
		OnEvent: func(event Event) {
			events = append(events, event.Type)
		},
	}
	history := []contracts.Message{
		messages.UserText("old one"),
		messages.AssistantText("old two", "sonnet", nil),
	}
	result, err := runner.RunTurn(context.Background(), history, messages.UserText("new request"))
	if err != nil {
		t.Fatal(err)
	}
	if !result.Compacted || result.Compact == nil {
		t.Fatalf("result did not compact: %#v", result)
	}
	if len(client.requests) != 2 {
		t.Fatalf("requests = %d, want 2", len(client.requests))
	}
	compactPrompt := client.requests[0].Messages[len(client.requests[0].Messages)-1]
	if !strings.Contains(compactPrompt.Content[0].Text, "Do NOT call any tools") {
		t.Fatalf("compact prompt = %#v", compactPrompt)
	}
	mainReq := client.requests[1]
	if len(mainReq.Messages) != 2 {
		t.Fatalf("main request messages = %#v", mainReq.Messages)
	}
	if got := mainReq.Messages[0].Content[0].Text; !strings.Contains(got, "summary text") {
		t.Fatalf("main summary = %q", got)
	}
	if got := mainReq.Messages[1].Content[0].Text; got != "new request" {
		t.Fatalf("kept recent message = %q", got)
	}
	if !containsEvent(events, EventCompact) {
		t.Fatalf("events = %#v", events)
	}
	transcript, err := session.LoadTranscript(transcriptPath)
	if err != nil {
		t.Fatal(err)
	}
	foundBoundary := false
	for _, id := range transcript.Order {
		msg := transcript.Messages[id]
		if msg != nil && msg.IsCompactBoundary() && msg.CompactMetadata != nil && msg.CompactMetadata.MessagesSummarized == 2 {
			foundBoundary = true
			break
		}
	}
	if !foundBoundary {
		t.Fatalf("transcript missing compact boundary: %#v", transcript.Order)
	}
	summary, err := memory.LoadSessionSummary(filepath.Join(filepath.Dir(transcriptPath), "session-memory", "sess_compact", memory.SessionSummaryFilename))
	if err != nil {
		t.Fatal(err)
	}
	if summary.SessionID != "sess_compact" || !strings.Contains(summary.Summary, "summary text") || summary.Metadata.MessagesSummarized != 2 {
		t.Fatalf("session memory summary = %#v", summary)
	}
}

func TestRunnerEmitsTokenWarningBeforeMainRequest(t *testing.T) {
	client := &fakeClient{calls: []fakeCall{
		{response: &anthropic.Response{
			ID:         "msg_done",
			Type:       "message",
			Role:       "assistant",
			Model:      "sonnet",
			StopReason: "end_turn",
			Content:    []contracts.ContentBlock{contracts.NewTextBlock("done")},
		}},
	}}
	var events []EventType
	var warnings []TokenWarning
	runner := Runner{
		Client:    client,
		Model:     "sonnet",
		MaxTokens: 128,
		AutoCompact: &compactpkg.AutoConfig{
			Enabled:    true,
			TokenUsage: 160_000,
			Window: compactpkg.WindowConfig{
				ContextWindow:   200_000,
				MaxOutputTokens: 20_000,
			},
		},
		OnEvent: func(event Event) {
			events = append(events, event.Type)
			if event.Type == EventTokenWarning {
				if event.TokenWarning == nil {
					t.Fatal("token warning event missing payload")
				}
				warnings = append(warnings, *event.TokenWarning)
			}
		},
	}

	result, err := runner.RunTurn(context.Background(), []contracts.Message{messages.UserText("old")}, messages.UserText("new"))
	if err != nil {
		t.Fatal(err)
	}
	if result.Compacted {
		t.Fatalf("warning-only turn should not compact: %#v", result)
	}
	if len(client.requests) != 1 {
		t.Fatalf("requests = %d, want only main request", len(client.requests))
	}
	if len(warnings) != 1 {
		t.Fatalf("warnings = %#v events = %#v", warnings, events)
	}
	warning := warnings[0]
	if warning.TokenUsage != 160_000 || warning.State.PercentLeft != 4 {
		t.Fatalf("warning payload = %#v", warning)
	}
	if !warning.State.IsAboveWarningThreshold || warning.State.IsAboveAutoCompactThreshold || !warning.Window.AutoCompactEnabled {
		t.Fatalf("warning state = %#v window = %#v", warning.State, warning.Window)
	}
	if len(events) < 2 || events[0] != EventUserMessage || events[1] != EventTokenWarning {
		t.Fatalf("events = %#v", events)
	}
}

func TestRunnerAutoCompactFailureDoesNotBlockMainRequest(t *testing.T) {
	client := &fakeClient{calls: []fakeCall{
		{err: anthropic.APIError{StatusCode: http.StatusInternalServerError, Type: "overloaded_error", Message: "compact failed"}},
		{response: &anthropic.Response{
			ID:         "msg_done",
			Type:       "message",
			Role:       "assistant",
			Model:      "sonnet",
			StopReason: "end_turn",
			Content:    []contracts.ContentBlock{contracts.NewTextBlock("done")},
		}},
	}}
	var compactErrors int
	config := &compactpkg.AutoConfig{Enabled: true, Force: true}
	runner := Runner{
		Client:      client,
		Model:       "sonnet",
		MaxTokens:   128,
		AutoCompact: config,
		OnEvent: func(event Event) {
			if event.Type == EventCompact && event.Error != nil {
				compactErrors++
			}
		},
	}

	result, err := runner.RunTurn(context.Background(), []contracts.Message{messages.UserText("old")}, messages.UserText("new"))
	if err != nil {
		t.Fatal(err)
	}
	if result.Compacted || result.Assistant.Content[0].Text != "done" {
		t.Fatalf("result = %#v", result)
	}
	if config.ConsecutiveFailures != 1 || compactErrors != 1 {
		t.Fatalf("failures=%d compactErrors=%d", config.ConsecutiveFailures, compactErrors)
	}
	if len(client.requests) != 2 {
		t.Fatalf("requests = %d, want compact attempt and main request", len(client.requests))
	}
}

func TestRunnerAutoCompactSkipsAfterFailureLimit(t *testing.T) {
	client := &fakeClient{calls: []fakeCall{
		{response: &anthropic.Response{
			ID:         "msg_done",
			Type:       "message",
			Role:       "assistant",
			Model:      "sonnet",
			StopReason: "end_turn",
			Content:    []contracts.ContentBlock{contracts.NewTextBlock("done")},
		}},
	}}
	config := &compactpkg.AutoConfig{
		Enabled:             true,
		TokenUsage:          10_000,
		ConsecutiveFailures: compactpkg.DefaultMaxConsecutiveFailures,
		Window: compactpkg.WindowConfig{
			ContextWindow:      12_000,
			MaxOutputTokens:    1_000,
			AutoCompactEnabled: true,
		},
	}
	runner := Runner{
		Client:      client,
		Model:       "sonnet",
		MaxTokens:   128,
		AutoCompact: config,
	}

	if _, err := runner.RunTurn(context.Background(), []contracts.Message{messages.UserText("old")}, messages.UserText("new")); err != nil {
		t.Fatal(err)
	}
	if len(client.requests) != 1 {
		t.Fatalf("requests = %d, want only main request", len(client.requests))
	}
	if config.ConsecutiveFailures != compactpkg.DefaultMaxConsecutiveFailures {
		t.Fatalf("failure count changed = %d", config.ConsecutiveFailures)
	}
}

func TestRunnerInjectsSessionMemoryRecallIntoRequest(t *testing.T) {
	root := filepath.Join(t.TempDir(), "session-memory")
	if _, err := memory.WriteSessionSummary(memory.SessionSummaryOptions{
		Root:      root,
		SessionID: "prior",
		Summary:   "database permissions and migration notes",
	}); err != nil {
		t.Fatal(err)
	}
	if _, err := memory.WriteSessionSummary(memory.SessionSummaryOptions{
		Root:      root,
		SessionID: "current",
		Summary:   "database current session should be excluded",
	}); err != nil {
		t.Fatal(err)
	}
	client := &fakeClient{calls: []fakeCall{
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
		Client:                    client,
		Model:                     "sonnet",
		MaxTokens:                 128,
		SessionID:                 "current",
		EnableSessionMemoryRecall: true,
		SessionMemoryRecallRoot:   root,
		SessionMemoryRecallLimit:  2,
	}
	if _, err := runner.RunTurn(context.Background(), nil, messages.UserText("database permissions")); err != nil {
		t.Fatal(err)
	}
	if len(client.requests) != 1 {
		t.Fatalf("requests = %d", len(client.requests))
	}
	apiMessages := client.requests[0].Messages
	if len(apiMessages) != 2 {
		t.Fatalf("api messages = %#v", apiMessages)
	}
	recall := apiMessages[0].Content[0].Text
	if !strings.Contains(recall, "Relevant session memory") || !strings.Contains(recall, "[prior]") || strings.Contains(recall, "[current]") {
		t.Fatalf("recall = %q", recall)
	}
	if got := apiMessages[1].Content[0].Text; got != "database permissions" {
		t.Fatalf("user message = %q", got)
	}
}

func TestRunnerExpandsRelevantMemoryAttachmentsIntoRequest(t *testing.T) {
	now := time.Date(2026, 6, 3, 12, 0, 0, 0, time.UTC)
	mem := memory.NewRelevantMemory("/repo/.claude/memory/db.md", "database memory", now, now)
	runner := Runner{Model: "sonnet", MaxTokens: 128}

	request, err := runner.BuildRequest([]contracts.Message{
		memory.RelevantMemoriesAttachmentMessage([]memory.RelevantMemory{mem}),
		messages.UserText("continue database work"),
	}, "sonnet")
	if err != nil {
		t.Fatal(err)
	}
	if len(request.Messages) != 2 {
		t.Fatalf("messages = %#v", request.Messages)
	}
	first := request.Messages[0]
	if first.Role != "user" || len(first.Content) != 1 || !strings.HasPrefix(first.Content[0].Text, "<system-reminder>\nMemory (saved today): /repo/.claude/memory/db.md:") || !strings.Contains(first.Content[0].Text, "database memory\n</system-reminder>") {
		t.Fatalf("first message = %#v", first)
	}
	if got := request.Messages[1].Content[0].Text; got != "continue database work" {
		t.Fatalf("user text = %q", got)
	}
}

func TestRunnerInjectsRelevantMemoryFromConfiguredDirIntoRequest(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "db.md")
	if err := os.WriteFile(path, []byte("---\ndescription: database permissions migration\n---\nremember database permission rules\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	now := time.Now()
	if err := os.Chtimes(path, now, now); err != nil {
		t.Fatal(err)
	}
	runner := Runner{Model: "sonnet", MaxTokens: 128, RelevantMemoryDir: dir}

	request, err := runner.BuildRequest([]contracts.Message{messages.UserText("database permissions")}, "sonnet")
	if err != nil {
		t.Fatal(err)
	}
	if len(request.Messages) != 2 {
		t.Fatalf("messages = %#v", request.Messages)
	}
	if got := request.Messages[0].Content[0].Text; got != "database permissions" {
		t.Fatalf("user message = %q", got)
	}
	memoryText := request.Messages[1].Content[0].Text
	if !strings.HasPrefix(memoryText, "<system-reminder>\nMemory (saved today): ") || !strings.Contains(memoryText, "/db.md:") || !strings.Contains(memoryText, "remember database permission rules\n\n</system-reminder>") {
		t.Fatalf("memory text = %q", memoryText)
	}

	request, err = runner.BuildRequest([]contracts.Message{messages.UserText("database")}, "sonnet")
	if err != nil {
		t.Fatal(err)
	}
	if len(request.Messages) != 1 {
		t.Fatalf("single-word messages = %#v", request.Messages)
	}
}

func TestRunnerPrefetchesRelevantMemoryIntoFirstRequest(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "db.md")
	if err := os.WriteFile(path, []byte("---\ndescription: database permissions migration\n---\nremember database permission rules\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	now := time.Now()
	if err := os.Chtimes(path, now, now); err != nil {
		t.Fatal(err)
	}
	client := &fakeClient{calls: []fakeCall{
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
		Client:            client,
		Model:             "sonnet",
		MaxTokens:         128,
		RelevantMemoryDir: dir,
	}

	if _, err := runner.RunTurn(context.Background(), nil, messages.UserText("database permissions")); err != nil {
		t.Fatal(err)
	}
	if len(client.requests) != 1 {
		t.Fatalf("requests = %d", len(client.requests))
	}
	request := client.requests[0]
	if len(request.Messages) != 2 {
		t.Fatalf("messages = %#v", request.Messages)
	}
	if got := request.Messages[0].Content[0].Text; got != "database permissions" {
		t.Fatalf("user message = %q", got)
	}
	if memoryText := request.Messages[1].Content[0].Text; !strings.Contains(memoryText, "/db.md:") || !strings.Contains(memoryText, "remember database permission rules") {
		t.Fatalf("memory text = %q", memoryText)
	}
}

func TestRunnerRelevantMemoryPrefetchUsesMemoryAgentSelector(t *testing.T) {
	dir := t.TempDir()
	dbPath := filepath.Join(dir, "db.md")
	modelPath := filepath.Join(dir, "model.md")
	if err := os.WriteFile(dbPath, []byte("---\ndescription: database permissions migration\n---\ndeterministic memory\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(modelPath, []byte("---\ndescription: model selected memory\n---\nmodel selected memory\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	mainClient := &fakeClient{calls: []fakeCall{
		{response: &anthropic.Response{
			ID:         "msg_done",
			Type:       "message",
			Role:       "assistant",
			Model:      "sonnet",
			StopReason: "end_turn",
			Content:    []contracts.ContentBlock{contracts.NewTextBlock("done")},
		}},
	}}
	memoryClient := &fakeClient{calls: []fakeCall{
		{response: &anthropic.Response{
			ID:      "msg_memory_select",
			Type:    "message",
			Role:    "assistant",
			Model:   "sonnet",
			Content: []contracts.ContentBlock{contracts.NewTextBlock(`{"memory_paths":["model.md"]}`)},
		}},
	}}
	runner := Runner{
		Client:            mainClient,
		Model:             "sonnet",
		MaxTokens:         128,
		RelevantMemoryDir: dir,
		MemoryAgentClient: memoryClient,
	}

	if _, err := runner.RunTurn(context.Background(), nil, messages.UserText("database permissions")); err != nil {
		t.Fatal(err)
	}
	if len(memoryClient.requests) != 1 || !strings.Contains(memoryClient.requests[0].Messages[0].Content[0].Text, "Candidate memory files") {
		t.Fatalf("memory selector requests = %#v", memoryClient.requests)
	}
	if len(mainClient.requests) != 1 || len(mainClient.requests[0].Messages) != 2 {
		t.Fatalf("main request = %#v", mainClient.requests)
	}
	memoryText := mainClient.requests[0].Messages[1].Content[0].Text
	if !strings.Contains(memoryText, "/model.md:") || !strings.Contains(memoryText, "model selected memory") || strings.Contains(memoryText, "deterministic memory") {
		t.Fatalf("memory text = %q", memoryText)
	}
}

func TestRunnerRelevantMemoryPrefetchFailsOpen(t *testing.T) {
	client := &fakeClient{calls: []fakeCall{
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
		Client:            client,
		Model:             "sonnet",
		MaxTokens:         128,
		RelevantMemoryDir: "\x00",
	}

	if _, err := runner.RunTurn(context.Background(), nil, messages.UserText("database permissions")); err != nil {
		t.Fatal(err)
	}
	if len(client.requests) != 1 {
		t.Fatalf("requests = %d", len(client.requests))
	}
	if len(client.requests[0].Messages) != 1 || client.requests[0].Messages[0].Content[0].Text != "database permissions" {
		t.Fatalf("messages = %#v", client.requests[0].Messages)
	}
}

func TestRunnerPassesRelevantMemoryDirToFileTools(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "old.md")
	if err := os.WriteFile(path, []byte("---\ndescription: stale memory\n---\nold memory fact\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	mtime := time.Now().Add(-3 * 24 * time.Hour)
	if err := os.Chtimes(path, mtime, mtime); err != nil {
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
				ID:    "toolu_read_memory",
				Name:  "Read",
				Input: json.RawMessage(`{"file_path":` + strconv.Quote(path) + `}`),
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
		Client:            client,
		Tools:             tool.NewExecutor(registry),
		Model:             "sonnet",
		MaxTokens:         128,
		RelevantMemoryDir: dir,
	}
	result, err := runner.RunTurn(context.Background(), nil, messages.UserText("read stale memory"))
	if err != nil {
		t.Fatal(err)
	}
	if len(result.ToolResults) != 1 {
		t.Fatalf("tool results = %#v", result.ToolResults)
	}
	content := result.ToolResults[0].Content.(string)
	if !strings.HasPrefix(content, "<system-reminder>This memory is 3 days old.") || !strings.Contains(content, "old memory fact") {
		t.Fatalf("content = %q", content)
	}
}

func TestRunnerPassesSkillDirsToToolMetadata(t *testing.T) {
	memoryDir := filepath.Join(t.TempDir(), "memory")
	skillDir := filepath.Join(t.TempDir(), "bundled-skill")
	runner := Runner{
		RelevantMemoryDir: memoryDir,
		SkillDirs:         []string{skillDir},
	}

	internal := tool.InternalPathContextFromMetadata(runner.toolMetadata())
	if internal.AutoMemoryDir != memoryDir {
		t.Fatalf("auto memory dir = %q, want %q", internal.AutoMemoryDir, memoryDir)
	}
	if len(internal.SkillDirs) != 1 || internal.SkillDirs[0] != skillDir {
		t.Fatalf("skill dirs = %#v", internal.SkillDirs)
	}
	internal.SkillDirs[0] = "mutated"
	again := tool.InternalPathContextFromMetadata(runner.toolMetadata())
	if again.SkillDirs[0] != skillDir {
		t.Fatalf("skill dirs should be copied from runner: %#v", again.SkillDirs)
	}
}

func TestRunnerDiscoversProjectSkillDirsForToolMetadata(t *testing.T) {
	repo := filepath.Join(t.TempDir(), "repo")
	cwd := filepath.Join(repo, "pkg")
	rootSkill := filepath.Join(repo, ".claude", "skills", "root")
	nestedSkill := filepath.Join(cwd, ".claude", "skills", "nested")
	for _, dir := range []string{rootSkill, nestedSkill} {
		if err := os.MkdirAll(dir, 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(dir, "SKILL.md"), []byte("---\ndescription: test\n---\n"), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	if err := os.MkdirAll(filepath.Join(repo, ".git"), 0o755); err != nil {
		t.Fatal(err)
	}

	runner := Runner{WorkingDirectory: cwd}
	internal := tool.InternalPathContextFromMetadata(runner.toolMetadata())
	want := []string{nestedSkill, rootSkill}
	if len(internal.SkillDirs) != len(want) {
		t.Fatalf("skill dirs = %#v, want %#v", internal.SkillDirs, want)
	}
	for i := range want {
		if internal.SkillDirs[i] != want[i] {
			t.Fatalf("skill dirs = %#v, want %#v", internal.SkillDirs, want)
		}
	}
}

func TestRunnerExtractsSessionMemoryAfterTurn(t *testing.T) {
	root := filepath.Join(t.TempDir(), "session-memory")
	client := &fakeClient{calls: []fakeCall{
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
		Client:                 client,
		Model:                  "sonnet",
		MaxTokens:              128,
		SessionID:              "extract_session",
		SessionMemoryRoot:      root,
		EnableMemoryExtraction: true,
		MemoryExtractLimit:     4,
	}
	if _, err := runner.RunTurn(context.Background(), nil, messages.UserText("Remember use brief summaries")); err != nil {
		t.Fatal(err)
	}
	summary, err := memory.LoadSessionSummary(filepath.Join(root, "extract_session", memory.SessionSummaryFilename))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(summary.Summary, "Extracted session memory") || !strings.Contains(summary.Summary, "use brief summaries") {
		t.Fatalf("summary = %#v", summary)
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

func containsEvent(events []EventType, target EventType) bool {
	for _, event := range events {
		if event == target {
			return true
		}
	}
	return false
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
