package tasktools

import (
	"context"
	"encoding/json"
	"path/filepath"
	"strings"
	"testing"

	"ccgo/internal/contracts"
	msgs "ccgo/internal/messages"
	"ccgo/internal/session"
	"ccgo/internal/tool"
)

func taskExecutor(t *testing.T) tool.Executor {
	t.Helper()
	registry, err := tool.NewRegistry(NewTaskTool())
	if err != nil {
		t.Fatal(err)
	}
	return tool.NewExecutor(registry)
}

func taskContext(t *testing.T) (tool.Context, string) {
	t.Helper()
	dir := t.TempDir()
	transcriptPath := filepath.Join(dir, "session.jsonl")
	return tool.Context{
		Context:          context.Background(),
		WorkingDirectory: filepath.Join(dir, "worktree"),
		SessionID:        "sess_task",
		Metadata: map[string]any{
			tool.MetadataSessionPathKey: transcriptPath,
		},
	}, transcriptPath
}

func TestTaskToolStartsSidechainAndStoresPrompt(t *testing.T) {
	ctx, transcriptPath := taskContext(t)
	executor := taskExecutor(t)

	result, err := executor.Execute(ctx, contracts.ToolUse{
		ID:    "toolu_task",
		Name:  "Task",
		Input: json.RawMessage(`{"taskId":"agent/one","description":"Review API","prompt":"Inspect API changes","subagentType":"general-purpose"}`),
	}, nil)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(result.Content.(string), "Task started: Review API") {
		t.Fatalf("content = %#v", result.Content)
	}
	if result.StructuredContent["status"] != session.SidechainStatusRunning ||
		result.StructuredContent["sidechain_id"] != "agent_one" ||
		result.StructuredContent["subagent_type"] != "general-purpose" {
		t.Fatalf("structured content = %#v", result.StructuredContent)
	}

	states, err := session.ListSidechainStates(transcriptPath, ctx.SessionID)
	if err != nil {
		t.Fatal(err)
	}
	if len(states) != 1 {
		t.Fatalf("states = %#v", states)
	}
	state := states[0]
	if state.ID != "agent_one" || state.Status != session.SidechainStatusRunning || state.MessageCount != 2 {
		t.Fatalf("state = %#v", state)
	}
	if state.Metadata.AgentType != "general-purpose" || state.Metadata.Description != "Review API" || state.Metadata.WorktreePath != ctx.WorkingDirectory {
		t.Fatalf("metadata = %#v", state.Metadata)
	}

	transcript, err := session.LoadTranscript(state.Path)
	if err != nil {
		t.Fatal(err)
	}
	var foundPrompt bool
	for _, id := range transcript.Order {
		entry := transcript.Messages[id]
		if entry == nil || entry.Message == nil {
			continue
		}
		if msgs.TextContent(*entry.Message) == "Inspect API changes" && entry.IsSidechain && entry.AgentID == "agent_one" {
			foundPrompt = true
			break
		}
	}
	if !foundPrompt {
		t.Fatalf("sidechain transcript missing prompt: %#v", transcript.Order)
	}
}

func TestTaskToolValidatesRuntimeContext(t *testing.T) {
	executor := taskExecutor(t)
	ctx := tool.Context{Context: context.Background(), SessionID: "sess_task"}
	_, err := executor.Execute(ctx, contracts.ToolUse{
		ID:    "toolu_task_invalid",
		Name:  "Task",
		Input: json.RawMessage(`{"description":"Review API","prompt":"Inspect API changes","subagent_type":"general-purpose"}`),
	}, nil)
	if err == nil || !strings.Contains(err.Error(), "session path is required") {
		t.Fatalf("err = %v", err)
	}

	ctx, _ = taskContext(t)
	_, err = executor.Execute(ctx, contracts.ToolUse{
		ID:    "toolu_task_missing_prompt",
		Name:  "Task",
		Input: json.RawMessage(`{"description":"Review API","subagent_type":"general-purpose"}`),
	}, nil)
	if err == nil || !strings.Contains(err.Error(), "prompt is required") {
		t.Fatalf("err = %v", err)
	}
}

func TestTaskToolDefinitionIsPermissionSafeButOrdered(t *testing.T) {
	task := NewTaskTool()
	if !task.IsReadOnly(nil) {
		t.Fatalf("Task should be read-only for permission decisions")
	}
	if task.IsConcurrencySafe(nil) {
		t.Fatalf("Task should preserve ordered sidechain lifecycle updates")
	}
	if task.IsDestructive(nil) {
		t.Fatalf("Task should not be destructive")
	}
}
