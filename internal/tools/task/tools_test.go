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

func taskContextWithAgents(t *testing.T, agents []tool.AgentInfo) (tool.Context, string) {
	t.Helper()
	ctx, transcriptPath := taskContext(t)
	ctx.Metadata[tool.MetadataAvailableAgentsKey] = agents
	return ctx, transcriptPath
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

func TestTaskToolUsesAvailableAgentsInPromptSchemaAndValidation(t *testing.T) {
	ctx, transcriptPath := taskContextWithAgents(t, []tool.AgentInfo{{
		Name:        "demo:reviewer",
		Description: "Review changes",
		Path:        "/tmp/reviewer.md",
		Prompt:      "Review with plugin instructions.",
	}})
	task := NewTaskTool()

	prompt, err := task.Prompt(tool.PromptContext{Metadata: ctx.Metadata})
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(prompt, "general-purpose") || !strings.Contains(prompt, "demo:reviewer: Review changes") {
		t.Fatalf("prompt = %q", prompt)
	}
	schema := task.InputSchema(tool.PromptContext{Metadata: ctx.Metadata})
	properties := schema["properties"].(map[string]any)
	subagent := properties["subagent_type"].(map[string]any)
	enumValues, ok := subagent["enum"].([]any)
	if !ok || !containsEnum(enumValues, "general-purpose") || !containsEnum(enumValues, "demo:reviewer") {
		t.Fatalf("schema enum = %#v", subagent["enum"])
	}

	executor := taskExecutor(t)
	result, err := executor.Execute(ctx, contracts.ToolUse{
		ID:    "toolu_task_plugin_agent",
		Name:  "Task",
		Input: json.RawMessage(`{"description":"Review API","prompt":"Inspect API changes","subagent_type":"demo:reviewer"}`),
	}, nil)
	if err != nil {
		t.Fatal(err)
	}
	if result.StructuredContent["subagent_type"] != "demo:reviewer" {
		t.Fatalf("structured content = %#v", result.StructuredContent)
	}
	if result.StructuredContent["agent_path"] != "/tmp/reviewer.md" || result.StructuredContent["agent_prompt_chars"] != len("Review with plugin instructions.") {
		t.Fatalf("structured agent metadata = %#v", result.StructuredContent)
	}
	states, err := session.ListSidechainStates(transcriptPath, ctx.SessionID)
	if err != nil {
		t.Fatal(err)
	}
	if len(states) != 1 || states[0].MessageCount != 3 {
		t.Fatalf("states = %#v", states)
	}
	if states[0].Metadata.AgentPath != "/tmp/reviewer.md" || states[0].Metadata.AgentPrompt != "Review with plugin instructions." {
		t.Fatalf("metadata = %#v", states[0].Metadata)
	}
	transcript, err := session.LoadTranscript(states[0].Path)
	if err != nil {
		t.Fatal(err)
	}
	var foundAgentPrompt bool
	for _, id := range transcript.Order {
		entry := transcript.Messages[id]
		if entry == nil || entry.Subtype != "agent_prompt" || entry.Message == nil {
			continue
		}
		if msgs.TextContent(*entry.Message) == "Review with plugin instructions." {
			foundAgentPrompt = true
			break
		}
	}
	if !foundAgentPrompt {
		t.Fatalf("sidechain transcript missing agent prompt: %#v", transcript.Order)
	}

	_, err = executor.Execute(ctx, contracts.ToolUse{
		ID:    "toolu_task_unknown_agent",
		Name:  "Task",
		Input: json.RawMessage(`{"description":"Review API","prompt":"Inspect API changes","subagent_type":"missing:agent"}`),
	}, nil)
	if err == nil || !strings.Contains(err.Error(), `subagent_type "missing:agent" is not available`) {
		t.Fatalf("err = %v", err)
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

func containsEnum(values []any, want string) bool {
	for _, value := range values {
		if value == want {
			return true
		}
	}
	return false
}
