package conversation

import (
	"context"
	"encoding/json"
	"testing"

	"ccgo/internal/api/anthropic"
	"ccgo/internal/contracts"
	"ccgo/internal/messages"
	"ccgo/internal/tool"
)

func toolUseAssistant(ids ...string) contracts.Message {
	blocks := make([]contracts.ContentBlock, 0, len(ids))
	for _, id := range ids {
		blocks = append(blocks, contracts.ContentBlock{Type: contracts.ContentToolUse, ID: id, Name: "Bash"})
	}
	return contracts.Message{Type: contracts.MessageAssistant, Content: blocks}
}

func toolResultMsg(id string) contracts.Message {
	return contracts.Message{
		Type:    contracts.MessageUser,
		Content: []contracts.ContentBlock{{Type: contracts.ContentToolResult, ToolUseID: id}},
	}
}

func TestSynthesizeOrphanedToolResults(t *testing.T) {
	assistant := toolUseAssistant("a", "b", "c")
	produced := []contracts.Message{toolResultMsg("a")} // only "a" got a result
	orphans := synthesizeOrphanedToolResults("s1", assistant, produced, "Interrupted by user")
	if len(orphans) != 2 {
		t.Fatalf("orphans = %d want 2 (for b and c)", len(orphans))
	}
	got := map[string]bool{}
	for _, m := range orphans {
		for _, blk := range m.Content {
			if blk.Type != contracts.ContentToolResult {
				t.Fatalf("orphan block type = %q want tool_result", blk.Type)
			}
			if !blk.IsError {
				t.Fatalf("orphan tool_result must be is_error")
			}
			got[blk.ToolUseID] = true
		}
	}
	if !got["b"] || !got["c"] || got["a"] {
		t.Fatalf("orphan tool_use_ids = %v want {b,c}", got)
	}
}

func TestSynthesizeOrphanedToolResultsNoneWhenComplete(t *testing.T) {
	assistant := toolUseAssistant("a", "b")
	produced := []contracts.Message{toolResultMsg("a"), toolResultMsg("b")}
	if orphans := synthesizeOrphanedToolResults("s1", assistant, produced, "x"); len(orphans) != 0 {
		t.Fatalf("expected no orphans, got %d", len(orphans))
	}
}

// TestRunTurnOrphanResultsOnCtxCancel verifies that when ctx is cancelled
// mid-tool-execution, the returned result.Messages contains an is_error
// tool_result for every tool_use that didn't finish, so no orphaned tool_use
// blocks remain in the conversation history.
func TestRunTurnOrphanResultsOnCtxCancel(t *testing.T) {
	// cancelTool blocks until ctx is cancelled, then returns an error.
	// This lets us control when cancellation happens mid-tool-execution.
	cancelCh := make(chan struct{})
	registry, err := tool.NewRegistry(tool.FuncTool{
		DefinitionValue: contracts.ToolDefinition{
			Name:        "BlockTool",
			Description: "blocks until cancelled",
			ReadOnly:    true,
			InputSchema: contracts.JSONSchema{
				"type":       "object",
				"properties": map[string]any{},
			},
		},
		CallFunc: func(ctx tool.Context, raw json.RawMessage, sink tool.ProgressSink) (contracts.ToolResult, error) {
			close(cancelCh) // signal we're inside the tool
			<-ctx.Context.Done()
			return contracts.ToolResult{}, ctx.Context.Err()
		},
	})
	if err != nil {
		t.Fatal(err)
	}

	// The fakeClient returns a tool_use on the first call; the second call
	// is never reached because ctx is cancelled.
	toolUseID := "toolu_cancel_1"
	client := &fakeClient{calls: []fakeCall{
		{response: &anthropic.Response{
			ID:         "msg_tool",
			Type:       "message",
			Role:       "assistant",
			Model:      "sonnet",
			StopReason: "tool_use",
			Content: []contracts.ContentBlock{{
				Type:  contracts.ContentToolUse,
				ID:    toolUseID,
				Name:  "BlockTool",
				Input: json.RawMessage(`{}`),
			}},
		}},
	}}

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	runner := Runner{
		Client:    client,
		Tools:     tool.NewExecutor(registry),
		Model:     "sonnet",
		MaxTokens: 128,
		SessionID: "sess_cancel",
	}

	// Cancel the context once the tool has started executing.
	go func() {
		<-cancelCh
		cancel()
	}()

	result, err := runner.RunTurn(ctx, nil, messages.UserText("trigger cancel"))
	// err should be context.Canceled (or nil if already bailed earlier) — either way
	// we must have a tool_result for every tool_use.
	_ = err

	// Collect tool_use IDs and tool_result IDs from result.Messages.
	toolUseIDs := map[string]bool{}
	toolResultIDs := map[string]bool{}
	for _, m := range result.Messages {
		for _, blk := range m.Content {
			switch blk.Type {
			case contracts.ContentToolUse:
				toolUseIDs[blk.ID] = true
			case contracts.ContentToolResult:
				toolResultIDs[blk.ToolUseID] = true
			}
		}
	}

	for id := range toolUseIDs {
		if !toolResultIDs[id] {
			t.Errorf("tool_use %q has no matching tool_result in result.Messages (orphaned)", id)
		}
	}
}
