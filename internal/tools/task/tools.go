package tasktools

import (
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"ccgo/internal/contracts"
	msgs "ccgo/internal/messages"
	"ccgo/internal/session"
	"ccgo/internal/tool"
)

type taskInput struct {
	ID           string `json:"id,omitempty"`
	Description  string `json:"description"`
	Prompt       string `json:"prompt"`
	SubagentType string `json:"subagent_type"`
}

func NewTaskTool() tool.Tool {
	return tool.FuncTool{
		DefinitionValue: contracts.ToolDefinition{
			Name:            "Task",
			Description:     "Start a subagent task.",
			SearchHint:      "launch subagent task",
			ReadOnly:        true,
			ConcurrencySafe: false,
			ShouldDefer:     true,
			Strict:          true,
			InputSchema: contracts.JSONSchema{
				"type":     "object",
				"required": []any{"description", "prompt", "subagent_type"},
				"properties": map[string]any{
					"description": map[string]any{
						"type":        "string",
						"description": "A short description of the task.",
					},
					"prompt": map[string]any{
						"type":        "string",
						"description": "The full instructions for the subagent.",
					},
					"subagent_type": map[string]any{
						"type":        "string",
						"description": "The subagent type to run.",
					},
					"id": map[string]any{
						"type":        "string",
						"description": "Optional stable task id.",
					},
				},
			},
		},
		NormalizeFunc:   normalizeTaskInput,
		PromptFunc:      taskPrompt,
		ValidateFunc:    validateTask,
		PermissionFunc:  allowTask,
		CallFunc:        callTask,
		ReadOnlyFunc:    func(json.RawMessage) bool { return true },
		ConcurrencyFunc: func(json.RawMessage) bool { return false },
	}
}

func taskPrompt(tool.PromptContext) (string, error) {
	return "Starts a subagent task with a short description, full prompt, and subagent_type. The task is recorded as a sidechain so it can be listed or resumed by the runtime.", nil
}

func normalizeTaskInput(raw json.RawMessage) (json.RawMessage, error) {
	var obj map[string]any
	if err := json.Unmarshal(raw, &obj); err != nil {
		return nil, err
	}
	input := taskInput{}
	if value, ok := firstString(obj, "id", "task_id", "taskId", "sidechain_id", "sidechainId"); ok {
		input.ID = value
	}
	if value, ok := firstString(obj, "description", "desc", "summary", "title", "task_description", "taskDescription"); ok {
		input.Description = value
	}
	if value, ok := firstString(obj, "prompt", "instructions", "instruction", "input", "task", "request"); ok {
		input.Prompt = value
	}
	if value, ok := firstString(obj, "subagent_type", "subagentType", "agent_type", "agentType", "agent", "type"); ok {
		input.SubagentType = value
	}
	data, err := json.Marshal(input)
	if err != nil {
		return nil, err
	}
	return data, nil
}

func validateTask(ctx tool.Context, raw json.RawMessage) error {
	input, err := decodeTaskInput(raw)
	if err != nil {
		return err
	}
	if input.Description == "" {
		return fmt.Errorf("description is required")
	}
	if input.Prompt == "" {
		return fmt.Errorf("prompt is required")
	}
	if input.SubagentType == "" {
		return fmt.Errorf("subagent_type is required")
	}
	if ctx.SessionID == "" {
		return fmt.Errorf("session id is required")
	}
	if sessionPathFromMetadata(ctx.Metadata) == "" {
		return fmt.Errorf("session path is required")
	}
	return nil
}

func allowTask(tool.Context, json.RawMessage) (contracts.PermissionDecision, error) {
	return contracts.PermissionDecision{
		Behavior:       contracts.PermissionAllow,
		DecisionReason: "starting a subagent task records runtime metadata; subagent tool calls are permissioned separately",
	}, nil
}

func callTask(ctx tool.Context, raw json.RawMessage, _ tool.ProgressSink) (contracts.ToolResult, error) {
	input, err := decodeTaskInput(raw)
	if err != nil {
		return contracts.ToolResult{}, err
	}
	sessionPath := sessionPathFromMetadata(ctx.Metadata)
	runtime := session.SidechainRuntime{SessionPath: sessionPath, SessionID: ctx.SessionID}
	run, err := runtime.Start(session.SidechainOptions{
		ID:           input.ID,
		AgentType:    input.SubagentType,
		WorktreePath: ctx.WorkingDirectory,
		Description:  input.Description,
	})
	if err != nil {
		return contracts.ToolResult{}, err
	}
	taskMessage := msgs.UserText(input.Prompt)
	taskMessage.SessionID = ctx.SessionID
	if err := runtime.Append(run, session.TranscriptMessage{
		Type:        string(contracts.MessageUser),
		UUID:        taskMessage.UUID,
		SessionID:   ctx.SessionID,
		Timestamp:   time.Now().UTC().Format(time.RFC3339Nano),
		IsSidechain: true,
		AgentID:     run.ID,
		Message:     &taskMessage,
	}); err != nil {
		return contracts.ToolResult{}, err
	}
	structured := map[string]any{
		"success":       true,
		"status":        session.SidechainStatusRunning,
		"sidechain_id":  run.ID,
		"subagent_type": input.SubagentType,
		"description":   input.Description,
		"path":          run.Path,
	}
	return contracts.ToolResult{
		Content:           fmt.Sprintf("Task started: %s\nSubagent type: %s\nSidechain ID: %s", input.Description, input.SubagentType, run.ID),
		StructuredContent: structured,
	}, nil
}

func decodeTaskInput(raw json.RawMessage) (taskInput, error) {
	var input taskInput
	if err := json.Unmarshal(raw, &input); err != nil {
		return taskInput{}, err
	}
	input.ID = strings.TrimSpace(input.ID)
	input.Description = strings.TrimSpace(input.Description)
	input.Prompt = strings.TrimSpace(input.Prompt)
	input.SubagentType = strings.TrimSpace(input.SubagentType)
	return input, nil
}

func sessionPathFromMetadata(metadata map[string]any) string {
	if metadata == nil {
		return ""
	}
	if value, ok := metadata[tool.MetadataSessionPathKey].(string); ok {
		return strings.TrimSpace(value)
	}
	return ""
}

func firstString(obj map[string]any, keys ...string) (string, bool) {
	for _, key := range keys {
		if value, ok := obj[key].(string); ok {
			return value, true
		}
	}
	return "", false
}
