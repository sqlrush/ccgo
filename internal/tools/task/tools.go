package tasktools

import (
	"encoding/json"
	"fmt"
	"sort"
	"strconv"
	"strings"
	"time"

	"ccgo/internal/contracts"
	msgs "ccgo/internal/messages"
	"ccgo/internal/session"
	"ccgo/internal/tool"
)

const builtInGeneralPurposeAgent = "general-purpose"

type taskInput struct {
	ID           string `json:"id,omitempty"`
	Description  string `json:"description"`
	Prompt       string `json:"prompt"`
	SubagentType string `json:"subagent_type"`
}

type taskOutputInput struct {
	TaskID    string `json:"task_id,omitempty"`
	TailLines *int   `json:"tail_lines,omitempty"`
}

type taskKillInput struct {
	TaskID string `json:"task_id,omitempty"`
	Reason string `json:"reason,omitempty"`
}

type taskResumeInput struct {
	TaskID string `json:"task_id,omitempty"`
	Limit  *int   `json:"limit,omitempty"`
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
		InputSchemaFunc: taskInputSchema,
		ValidateFunc:    validateTask,
		PermissionFunc:  allowTask,
		CallFunc:        callTask,
		ReadOnlyFunc:    func(json.RawMessage) bool { return true },
		ConcurrencyFunc: func(json.RawMessage) bool { return false },
	}
}

func NewTaskOutputTool() tool.Tool {
	return tool.FuncTool{
		DefinitionValue: contracts.ToolDefinition{
			Name:               "TaskOutput",
			Aliases:            []string{"AgentOutputTool", "TaskOutputTool"},
			Description:        "Read subagent task status and output.",
			SearchHint:         "read subagent task output status progress",
			ReadOnly:           true,
			ConcurrencySafe:    true,
			Strict:             true,
			MaxResultSizeChars: 100_000,
			InputSchema: contracts.JSONSchema{
				"type": "object",
				"properties": map[string]any{
					"task_id":    map[string]any{"type": "string"},
					"tail_lines": map[string]any{"type": "integer"},
				},
			},
		},
		PromptFunc: func(tool.PromptContext) (string, error) {
			return "Reads subagent task status and transcript output. Provide task_id to inspect one task, or omit it to list all known tasks for the current session.", nil
		},
		NormalizeFunc:   normalizeTaskOutputInput,
		ValidateFunc:    validateTaskOutput,
		PermissionFunc:  allowTaskOutput,
		CallFunc:        callTaskOutput,
		ReadOnlyFunc:    func(json.RawMessage) bool { return true },
		ConcurrencyFunc: func(json.RawMessage) bool { return true },
	}
}

func NewKillTaskTool() tool.Tool {
	return tool.FuncTool{
		DefinitionValue: contracts.ToolDefinition{
			Name:            "KillTask",
			Aliases:         []string{"TaskStop"},
			Description:     "Cancel a running subagent task.",
			SearchHint:      "kill cancel stop subagent task",
			ConcurrencySafe: false,
			Strict:          true,
			InputSchema: contracts.JSONSchema{
				"type":     "object",
				"required": []any{"task_id"},
				"properties": map[string]any{
					"task_id": map[string]any{"type": "string"},
					"reason":  map[string]any{"type": "string"},
				},
			},
		},
		PromptFunc: func(tool.PromptContext) (string, error) {
			return "Cancels a running subagent task by task_id. Use TaskOutput afterwards to read the final status and cancellation summary.", nil
		},
		NormalizeFunc: normalizeKillTaskInput,
		ValidateFunc:  validateKillTask,
		CallFunc:      callKillTask,
	}
}

func NewResumeTaskTool() tool.Tool {
	return tool.FuncTool{
		DefinitionValue: contracts.ToolDefinition{
			Name:               "ResumeTask",
			Aliases:            []string{"TaskResume"},
			Description:        "Build resume context for a subagent task.",
			SearchHint:         "resume subagent task context",
			ReadOnly:           true,
			ConcurrencySafe:    true,
			Strict:             true,
			MaxResultSizeChars: 100_000,
			InputSchema: contracts.JSONSchema{
				"type":     "object",
				"required": []any{"task_id"},
				"properties": map[string]any{
					"task_id": map[string]any{"type": "string"},
					"limit":   map[string]any{"type": "integer"},
				},
			},
		},
		PromptFunc: func(tool.PromptContext) (string, error) {
			return "Builds a resume context for a subagent task by task_id, including status, can_resume, metadata, and the tail messages that should seed the resumed agent.", nil
		},
		NormalizeFunc:   normalizeResumeTaskInput,
		ValidateFunc:    validateResumeTask,
		PermissionFunc:  allowTaskOutput,
		CallFunc:        callResumeTask,
		ReadOnlyFunc:    func(json.RawMessage) bool { return true },
		ConcurrencyFunc: func(json.RawMessage) bool { return true },
	}
}

func taskPrompt(ctx tool.PromptContext) (string, error) {
	prompt := "Starts a subagent task with a short description, full prompt, and subagent_type. The task is recorded as a sidechain so it can be listed or resumed by the runtime."
	agents := availableTaskAgents(ctx.Metadata)
	if len(agents) == 0 {
		return prompt, nil
	}
	var lines []string
	for _, agent := range agents {
		if agent.Description != "" {
			lines = append(lines, fmt.Sprintf("- %s: %s", agent.Name, agent.Description))
		} else {
			lines = append(lines, "- "+agent.Name)
		}
	}
	return prompt + "\n\nAvailable subagent types:\n" + strings.Join(lines, "\n"), nil
}

func taskInputSchema(ctx tool.PromptContext) contracts.JSONSchema {
	schema := contracts.JSONSchema{
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
	}
	metadataAgents := taskAgentsFromMetadata(ctx.Metadata)
	if len(metadataAgents) == 0 {
		return schema
	}
	names := taskAgentNames(availableTaskAgents(ctx.Metadata))
	if len(names) > 0 {
		if properties, ok := schema["properties"].(map[string]any); ok {
			if subagent, ok := properties["subagent_type"].(map[string]any); ok {
				enumValues := make([]any, 0, len(names))
				for _, name := range names {
					enumValues = append(enumValues, name)
				}
				subagent["enum"] = enumValues
			}
		}
	}
	return schema
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
	if len(taskAgentsFromMetadata(ctx.Metadata)) > 0 && !taskAgentAllowed(input.SubagentType, availableTaskAgents(ctx.Metadata)) {
		return fmt.Errorf("subagent_type %q is not available (available: %s)", input.SubagentType, strings.Join(taskAgentNames(availableTaskAgents(ctx.Metadata)), ", "))
	}
	return nil
}

func validateTaskOutput(ctx tool.Context, raw json.RawMessage) error {
	input, err := decodeTaskOutputInput(raw)
	if err != nil {
		return err
	}
	if input.TailLines != nil && *input.TailLines <= 0 {
		return fmt.Errorf("tail_lines must be positive")
	}
	if ctx.SessionID == "" {
		return fmt.Errorf("session id is required")
	}
	if sessionPathFromMetadata(ctx.Metadata) == "" {
		return fmt.Errorf("session path is required")
	}
	return nil
}

func validateKillTask(ctx tool.Context, raw json.RawMessage) error {
	input, err := decodeKillTaskInput(raw)
	if err != nil {
		return err
	}
	if input.TaskID == "" {
		return fmt.Errorf("task_id is required")
	}
	if ctx.SessionID == "" {
		return fmt.Errorf("session id is required")
	}
	if sessionPathFromMetadata(ctx.Metadata) == "" {
		return fmt.Errorf("session path is required")
	}
	return nil
}

func validateResumeTask(ctx tool.Context, raw json.RawMessage) error {
	input, err := decodeResumeTaskInput(raw)
	if err != nil {
		return err
	}
	if input.TaskID == "" {
		return fmt.Errorf("task_id is required")
	}
	if input.Limit != nil && *input.Limit <= 0 {
		return fmt.Errorf("limit must be positive")
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

func allowTaskOutput(tool.Context, json.RawMessage) (contracts.PermissionDecision, error) {
	return contracts.PermissionDecision{
		Behavior:       contracts.PermissionAllow,
		DecisionReason: "reading subagent task status is read-only",
	}, nil
}

func callTask(ctx tool.Context, raw json.RawMessage, sink tool.ProgressSink) (contracts.ToolResult, error) {
	input, err := decodeTaskInput(raw)
	if err != nil {
		return contracts.ToolResult{}, err
	}
	sessionPath := sessionPathFromMetadata(ctx.Metadata)
	runtime := session.SidechainRuntime{SessionPath: sessionPath, SessionID: ctx.SessionID}
	agent, hasAgent := taskAgentForType(input.SubagentType, availableTaskAgents(ctx.Metadata))
	run, err := runtime.Start(session.SidechainOptions{
		ID:                  input.ID,
		AgentType:           input.SubagentType,
		WorktreePath:        ctx.WorkingDirectory,
		Description:         input.Description,
		AgentPath:           agent.Path,
		AgentPrompt:         agent.Prompt,
		AgentModel:          agent.Model,
		AgentPermissionMode: string(agent.PermissionMode),
		AgentAllowedTools:   append([]string(nil), agent.AllowedTools...),
	})
	if err != nil {
		return contracts.ToolResult{}, err
	}
	if hasAgent && agent.Prompt != "" {
		agentMessage := msgs.SystemText("agent_prompt", agent.Prompt)
		agentMessage.SessionID = ctx.SessionID
		if err := runtime.Append(run, session.TranscriptMessage{
			Type:        string(contracts.MessageSystem),
			UUID:        agentMessage.UUID,
			SessionID:   ctx.SessionID,
			Timestamp:   time.Now().UTC().Format(time.RFC3339Nano),
			Subtype:     "agent_prompt",
			IsSidechain: true,
			AgentID:     run.ID,
			Message:     &agentMessage,
			Content: map[string]any{
				"agentType":           input.SubagentType,
				"agentPath":           agent.Path,
				"agentPrompt":         agent.Prompt,
				"agentModel":          agent.Model,
				"agentPermissionMode": string(agent.PermissionMode),
				"agentAllowedTools":   append([]string(nil), agent.AllowedTools...),
			},
		}); err != nil {
			return contracts.ToolResult{}, err
		}
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
	if hasAgent && agent.Path != "" {
		structured["agent_path"] = agent.Path
	}
	if hasAgent && agent.Prompt != "" {
		structured["agent_prompt_chars"] = len(agent.Prompt)
	}
	if hasAgent && agent.Model != "" {
		structured["agent_model"] = agent.Model
	}
	if hasAgent && agent.PermissionMode != "" {
		structured["agent_permission_mode"] = string(agent.PermissionMode)
	}
	if hasAgent && len(agent.AllowedTools) > 0 {
		structured["agent_allowed_tools"] = append([]string(nil), agent.AllowedTools...)
	}
	_ = tool.SendProgress(sink, "", "task_started", map[string]any{
		"task_id":       run.ID,
		"sidechain_id":  run.ID,
		"status":        session.SidechainStatusRunning,
		"subagent_type": input.SubagentType,
		"description":   input.Description,
		"path":          run.Path,
	})
	return contracts.ToolResult{
		Content:           fmt.Sprintf("Task started: %s\nSubagent type: %s\nSidechain ID: %s", input.Description, input.SubagentType, run.ID),
		StructuredContent: structured,
	}, nil
}

func callTaskOutput(ctx tool.Context, raw json.RawMessage, sink tool.ProgressSink) (contracts.ToolResult, error) {
	input, err := decodeTaskOutputInput(raw)
	if err != nil {
		return contracts.ToolResult{}, err
	}
	manager := session.NewSidechainManager(sessionPathFromMetadata(ctx.Metadata), ctx.SessionID)
	if input.TaskID == "" {
		states, err := manager.List()
		if err != nil {
			return contracts.ToolResult{}, err
		}
		tasks := make([]map[string]any, 0, len(states))
		for _, state := range states {
			tasks = append(tasks, structuredTaskState(state))
		}
		_ = tool.SendProgress(sink, "", "task_listed", map[string]any{
			"count": len(tasks),
		})
		return contracts.ToolResult{
			Content: formatTaskList(states),
			StructuredContent: map[string]any{
				"type":  "task_output",
				"tasks": tasks,
				"count": len(tasks),
			},
		}, nil
	}
	state, err := findTaskState(manager, input.TaskID)
	if err != nil {
		return contracts.ToolResult{}, err
	}
	output, err := taskTranscriptOutput(state, taskOutputTailLines(input))
	if err != nil {
		return contracts.ToolResult{}, err
	}
	structured := structuredTaskState(state)
	structured["type"] = "task_output"
	structured["output"] = output
	if input.TailLines != nil {
		structured["tail_lines"] = *input.TailLines
	}
	_ = tool.SendProgress(sink, "", "task_output", map[string]any{
		"task_id":       state.ID,
		"sidechain_id":  state.ID,
		"status":        state.Status,
		"running":       state.Status == session.SidechainStatusRunning,
		"message_count": state.MessageCount,
		"tail_lines":    taskOutputTailLines(input),
	})
	return contracts.ToolResult{
		Content:           formatTaskOutput(state, output),
		StructuredContent: structured,
	}, nil
}

func callKillTask(ctx tool.Context, raw json.RawMessage, sink tool.ProgressSink) (contracts.ToolResult, error) {
	input, err := decodeKillTaskInput(raw)
	if err != nil {
		return contracts.ToolResult{}, err
	}
	manager := session.NewSidechainManager(sessionPathFromMetadata(ctx.Metadata), ctx.SessionID)
	state, err := findTaskState(manager, input.TaskID)
	if err != nil {
		return contracts.ToolResult{}, err
	}
	killed := false
	if state.Status == session.SidechainStatusRunning {
		reason := input.Reason
		if reason == "" {
			reason = "cancelled by KillTask"
		}
		if _, err := manager.Cancel(state.ID, reason, time.Now().UTC()); err != nil {
			return contracts.ToolResult{}, err
		}
		killed = true
		state, err = findTaskState(manager, state.ID)
		if err != nil {
			return contracts.ToolResult{}, err
		}
	}
	content := fmt.Sprintf("Task %s is not running.", state.ID)
	if killed {
		content = fmt.Sprintf("Cancel requested for task %s.", state.ID)
	}
	structured := structuredTaskState(state)
	structured["type"] = "kill_task"
	structured["killed"] = killed
	structured["cancelled"] = state.Status == session.SidechainStatusCancelled
	progressType := "task_not_running"
	if killed {
		progressType = "task_cancelled"
	}
	_ = tool.SendProgress(sink, "", progressType, map[string]any{
		"task_id":      state.ID,
		"sidechain_id": state.ID,
		"status":       state.Status,
		"killed":       killed,
		"cancelled":    state.Status == session.SidechainStatusCancelled,
	})
	return contracts.ToolResult{
		Content:           content,
		StructuredContent: structured,
	}, nil
}

func callResumeTask(ctx tool.Context, raw json.RawMessage, sink tool.ProgressSink) (contracts.ToolResult, error) {
	input, err := decodeResumeTaskInput(raw)
	if err != nil {
		return contracts.ToolResult{}, err
	}
	manager := session.NewSidechainManager(sessionPathFromMetadata(ctx.Metadata), ctx.SessionID)
	if _, err := findTaskState(manager, input.TaskID); err != nil {
		return contracts.ToolResult{}, err
	}
	resumeContext, err := manager.ResumeContext(input.TaskID, resumeTaskLimit(input))
	if err != nil {
		return contracts.ToolResult{}, err
	}
	structured := structuredTaskState(resumeContext.State)
	structured["type"] = "task_resume"
	structured["can_resume"] = resumeContext.CanResume
	structured["truncated"] = resumeContext.Truncated
	structured["message_limit"] = resumeContext.MessageLimit
	structured["resume_messages"] = structuredResumeMessages(resumeContext.Messages)
	_ = tool.SendProgress(sink, "", "task_resume_context", map[string]any{
		"task_id":       resumeContext.State.ID,
		"sidechain_id":  resumeContext.State.ID,
		"status":        resumeContext.State.Status,
		"can_resume":    resumeContext.CanResume,
		"truncated":     resumeContext.Truncated,
		"message_limit": resumeContext.MessageLimit,
	})
	return contracts.ToolResult{
		Content:           formatTaskResume(resumeContext),
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

func decodeTaskOutputInput(raw json.RawMessage) (taskOutputInput, error) {
	var input taskOutputInput
	if err := json.Unmarshal(raw, &input); err != nil {
		return taskOutputInput{}, err
	}
	input.TaskID = strings.TrimSpace(input.TaskID)
	return input, nil
}

func decodeKillTaskInput(raw json.RawMessage) (taskKillInput, error) {
	var input taskKillInput
	if err := json.Unmarshal(raw, &input); err != nil {
		return taskKillInput{}, err
	}
	input.TaskID = strings.TrimSpace(input.TaskID)
	input.Reason = strings.TrimSpace(input.Reason)
	return input, nil
}

func decodeResumeTaskInput(raw json.RawMessage) (taskResumeInput, error) {
	var input taskResumeInput
	if err := json.Unmarshal(raw, &input); err != nil {
		return taskResumeInput{}, err
	}
	input.TaskID = strings.TrimSpace(input.TaskID)
	return input, nil
}

func normalizeTaskOutputInput(raw json.RawMessage) (json.RawMessage, error) {
	obj, err := decodeRawTaskObject(raw)
	if err != nil {
		return nil, err
	}
	for key := range obj {
		switch key {
		case "task_id", "taskId", "sidechain_id", "sidechainId", "id", "tail_lines", "tailLines":
		default:
			return nil, fmt.Errorf("input.%s is not allowed", key)
		}
	}
	normalized := map[string]json.RawMessage{}
	if value, ok := firstRawTaskField(obj, "task_id", "taskId", "sidechain_id", "sidechainId", "id"); ok {
		normalized["task_id"] = value
	}
	if value, ok := firstRawTaskField(obj, "tail_lines", "tailLines"); ok {
		normalized["tail_lines"] = value
	}
	coerceTaskSemanticNumberStrings(normalized, "tail_lines")
	return json.Marshal(normalized)
}

func normalizeKillTaskInput(raw json.RawMessage) (json.RawMessage, error) {
	obj, err := decodeRawTaskObject(raw)
	if err != nil {
		return nil, err
	}
	for key := range obj {
		switch key {
		case "task_id", "taskId", "sidechain_id", "sidechainId", "id", "reason", "summary", "message":
		default:
			return nil, fmt.Errorf("input.%s is not allowed", key)
		}
	}
	normalized := map[string]json.RawMessage{}
	if value, ok := firstRawTaskField(obj, "task_id", "taskId", "sidechain_id", "sidechainId", "id"); ok {
		normalized["task_id"] = value
	}
	if value, ok := firstRawTaskField(obj, "reason", "summary", "message"); ok {
		normalized["reason"] = value
	}
	return json.Marshal(normalized)
}

func normalizeResumeTaskInput(raw json.RawMessage) (json.RawMessage, error) {
	obj, err := decodeRawTaskObject(raw)
	if err != nil {
		return nil, err
	}
	for key := range obj {
		switch key {
		case "task_id", "taskId", "sidechain_id", "sidechainId", "id", "limit", "message_limit", "messageLimit":
		default:
			return nil, fmt.Errorf("input.%s is not allowed", key)
		}
	}
	normalized := map[string]json.RawMessage{}
	if value, ok := firstRawTaskField(obj, "task_id", "taskId", "sidechain_id", "sidechainId", "id"); ok {
		normalized["task_id"] = value
	}
	if value, ok := firstRawTaskField(obj, "limit", "message_limit", "messageLimit"); ok {
		normalized["limit"] = value
	}
	coerceTaskSemanticNumberStrings(normalized, "limit")
	return json.Marshal(normalized)
}

func decodeRawTaskObject(raw json.RawMessage) (map[string]json.RawMessage, error) {
	var obj map[string]json.RawMessage
	if err := json.Unmarshal(raw, &obj); err != nil {
		return nil, err
	}
	if obj == nil {
		obj = map[string]json.RawMessage{}
	}
	return obj, nil
}

func firstRawTaskField(obj map[string]json.RawMessage, keys ...string) (json.RawMessage, bool) {
	for _, key := range keys {
		if value, ok := obj[key]; ok {
			return value, true
		}
	}
	return nil, false
}

func coerceTaskSemanticNumberStrings(obj map[string]json.RawMessage, keys ...string) {
	for _, key := range keys {
		raw, ok := obj[key]
		if !ok || len(raw) == 0 || raw[0] != '"' {
			continue
		}
		var text string
		if err := json.Unmarshal(raw, &text); err != nil {
			continue
		}
		if _, err := strconv.Atoi(text); err == nil {
			obj[key] = json.RawMessage(text)
		}
	}
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

func findTaskState(manager session.SidechainManager, taskID string) (session.SidechainState, error) {
	state, err := session.FindSidechainState(manager.Runtime.SessionPath, manager.Runtime.SessionID, taskID)
	if err != nil {
		return session.SidechainState{}, err
	}
	if state.MessageCount == 0 && state.Metadata.Empty() {
		return session.SidechainState{}, fmt.Errorf("task not found: %s", strings.TrimSpace(taskID))
	}
	if state.SessionID == "" {
		state.SessionID = manager.Runtime.SessionID
	}
	return state, nil
}

func structuredTaskState(state session.SidechainState) map[string]any {
	structured := map[string]any{
		"task_id":       state.ID,
		"sidechain_id":  state.ID,
		"status":        state.Status,
		"running":       state.Status == session.SidechainStatusRunning,
		"summary":       state.Summary,
		"started_at":    state.StartedAt,
		"ended_at":      state.EndedAt,
		"message_count": state.MessageCount,
		"path":          state.Path,
	}
	if state.Metadata.AgentType != "" {
		structured["subagent_type"] = state.Metadata.AgentType
	}
	if state.Metadata.Description != "" {
		structured["description"] = state.Metadata.Description
	}
	if state.Metadata.WorktreePath != "" {
		structured["worktree_path"] = state.Metadata.WorktreePath
	}
	if state.Metadata.WorktreeOwned {
		structured["worktree_owned"] = true
	}
	if state.Metadata.WorktreeCleanupStatus != "" {
		structured["worktree_cleanup_status"] = state.Metadata.WorktreeCleanupStatus
	}
	if state.Metadata.WorktreeCleanupReason != "" {
		structured["worktree_cleanup_reason"] = state.Metadata.WorktreeCleanupReason
	}
	if state.Metadata.WorktreeCleanupAt != "" {
		structured["worktree_cleanup_at"] = state.Metadata.WorktreeCleanupAt
	}
	if state.Metadata.AgentPath != "" {
		structured["agent_path"] = state.Metadata.AgentPath
	}
	if state.Metadata.AgentModel != "" {
		structured["agent_model"] = state.Metadata.AgentModel
	}
	if state.Metadata.AgentPermissionMode != "" {
		structured["agent_permission_mode"] = state.Metadata.AgentPermissionMode
	}
	if len(state.Metadata.AgentAllowedTools) > 0 {
		structured["agent_allowed_tools"] = append([]string(nil), state.Metadata.AgentAllowedTools...)
	}
	return structured
}

func taskOutputTailLines(input taskOutputInput) int {
	if input.TailLines == nil {
		return 0
	}
	return *input.TailLines
}

func resumeTaskLimit(input taskResumeInput) int {
	if input.Limit == nil {
		return 0
	}
	return *input.Limit
}

func taskTranscriptOutput(state session.SidechainState, tailLines int) (string, error) {
	transcript, err := session.LoadTranscript(state.Path)
	if err != nil {
		return "", err
	}
	var lines []string
	for _, id := range transcript.Order {
		entry := transcript.Messages[id]
		if entry == nil || skipTaskOutputEntry(entry) {
			continue
		}
		text := taskEntryText(entry)
		if text == "" {
			continue
		}
		label := strings.TrimSpace(entry.Type)
		if entry.Message != nil && entry.Message.Type != "" {
			label = string(entry.Message.Type)
		}
		if entry.Subtype != "" && label == "" {
			label = entry.Subtype
		}
		if label == "" {
			label = "message"
		}
		lines = append(lines, fmt.Sprintf("[%s] %s", label, text))
	}
	output := strings.Join(lines, "\n")
	if tailLines > 0 {
		output = tailTaskText(output, tailLines)
	}
	return output, nil
}

func skipTaskOutputEntry(entry *session.TranscriptMessage) bool {
	switch entry.Subtype {
	case "sidechain_start", "agent_prompt":
		return true
	default:
		return false
	}
}

func taskEntryText(entry *session.TranscriptMessage) string {
	if entry.Message != nil {
		return strings.TrimSpace(msgs.TextContent(*entry.Message))
	}
	return strings.TrimSpace(taskVisibleText(entry.Content))
}

func taskVisibleText(value any) string {
	switch typed := value.(type) {
	case string:
		return typed
	case []any:
		parts := make([]string, 0, len(typed))
		for _, item := range typed {
			if text := strings.TrimSpace(taskVisibleText(item)); text != "" {
				parts = append(parts, text)
			}
		}
		return strings.Join(parts, "\n")
	case map[string]any:
		for _, key := range []string{"summary", "finalSummary", "final_summary", "resultText", "result_text", "finalMessage", "final_message", "outputText", "output_text", "message", "text", "body", "output", "value", "content"} {
			if text := strings.TrimSpace(taskVisibleText(typed[key])); text != "" {
				return text
			}
		}
	case map[string]string:
		for _, key := range []string{"summary", "message", "text", "body", "output", "value", "content"} {
			if text := strings.TrimSpace(typed[key]); text != "" {
				return text
			}
		}
	}
	return ""
}

func tailTaskText(text string, lines int) string {
	if lines <= 0 {
		return text
	}
	parts := strings.Split(strings.TrimRight(text, "\n"), "\n")
	if len(parts) <= lines {
		return strings.Join(parts, "\n")
	}
	return strings.Join(parts[len(parts)-lines:], "\n")
}

func formatTaskList(states []session.SidechainState) string {
	if len(states) == 0 {
		return "No subagent tasks recorded for this session."
	}
	lines := make([]string, 0, len(states)+1)
	lines = append(lines, "Subagent tasks:")
	for _, state := range states {
		description := state.Metadata.Description
		if description == "" {
			description = state.ID
		}
		agentType := state.Metadata.AgentType
		if agentType == "" {
			agentType = "unknown"
		}
		lines = append(lines, fmt.Sprintf("- %s [%s] %s: %s", state.ID, state.Status, agentType, description))
	}
	return strings.Join(lines, "\n")
}

func formatTaskOutput(state session.SidechainState, output string) string {
	status := fmt.Sprintf("Task %s is %s.", state.ID, state.Status)
	var lines []string
	lines = append(lines, status)
	if state.Metadata.AgentType != "" {
		lines = append(lines, "Subagent type: "+state.Metadata.AgentType)
	}
	if state.Metadata.Description != "" {
		lines = append(lines, "Description: "+state.Metadata.Description)
	}
	if state.Summary != "" {
		lines = append(lines, "Summary: "+state.Summary)
	}
	if strings.TrimSpace(output) == "" {
		lines = append(lines, "No task output recorded yet.")
	} else {
		lines = append(lines, "Output:\n"+output)
	}
	return strings.Join(lines, "\n")
}

func structuredResumeMessages(messages []contracts.Message) []map[string]any {
	out := make([]map[string]any, 0, len(messages))
	for _, message := range messages {
		item := map[string]any{
			"uuid":    message.UUID,
			"type":    message.Type,
			"subtype": message.Subtype,
			"is_meta": message.IsMeta,
			"text":    strings.TrimSpace(msgs.TextContent(message)),
		}
		out = append(out, item)
	}
	return out
}

func formatTaskResume(resumeContext session.SidechainResumeContext) string {
	state := resumeContext.State
	status := "cannot be resumed"
	if resumeContext.CanResume {
		status = "can be resumed"
	}
	lines := []string{fmt.Sprintf("Task %s %s.", state.ID, status)}
	lines = append(lines, "Status: "+state.Status)
	if state.Metadata.AgentType != "" {
		lines = append(lines, "Subagent type: "+state.Metadata.AgentType)
	}
	if resumeContext.Summary != "" {
		lines = append(lines, "Summary: "+resumeContext.Summary)
	}
	if resumeContext.Truncated {
		lines = append(lines, fmt.Sprintf("Resume context truncated to %d messages.", resumeContext.MessageLimit))
	}
	if len(resumeContext.Messages) == 0 {
		lines = append(lines, "No resume messages available.")
		return strings.Join(lines, "\n")
	}
	lines = append(lines, "Resume messages:")
	for _, message := range resumeContext.Messages {
		label := string(message.Type)
		if message.Subtype != "" {
			label += ":" + message.Subtype
		}
		text := strings.TrimSpace(msgs.TextContent(message))
		if text == "" {
			text = "(no text content)"
		}
		lines = append(lines, fmt.Sprintf("[%s] %s", label, text))
	}
	return strings.Join(lines, "\n")
}

func firstString(obj map[string]any, keys ...string) (string, bool) {
	for _, key := range keys {
		if value, ok := obj[key].(string); ok {
			return value, true
		}
	}
	return "", false
}

func availableTaskAgents(metadata map[string]any) []tool.AgentInfo {
	agents := []tool.AgentInfo{{
		Name:        builtInGeneralPurposeAgent,
		Description: "General-purpose subagent for researching, searching, and multi-step tasks.",
	}}
	agents = append(agents, taskAgentsFromMetadata(metadata)...)
	return uniqueSortedAgents(agents)
}

func taskAgentsFromMetadata(metadata map[string]any) []tool.AgentInfo {
	if metadata == nil {
		return nil
	}
	switch raw := metadata[tool.MetadataAvailableAgentsKey].(type) {
	case []tool.AgentInfo:
		return cleanTaskAgents(raw)
	case []map[string]string:
		agents := make([]tool.AgentInfo, 0, len(raw))
		for _, item := range raw {
			agents = append(agents, tool.AgentInfo{
				Name:           item["name"],
				Description:    item["description"],
				Path:           item["path"],
				Prompt:         item["prompt"],
				Model:          item["model"],
				PermissionMode: contracts.PermissionMode(item["permission_mode"]),
				AllowedTools:   splitTaskAgentTools(item["allowed_tools"]),
			})
		}
		return cleanTaskAgents(agents)
	case []map[string]any:
		agents := make([]tool.AgentInfo, 0, len(raw))
		for _, item := range raw {
			agents = append(agents, tool.AgentInfo{
				Name:           firstTaskAgentField(item, "name", "id", "agent"),
				Description:    firstTaskAgentField(item, "description", "desc", "summary", "whenToUse", "when_to_use", "when-to-use"),
				Path:           firstTaskAgentField(item, "path", "file", "source"),
				Prompt:         firstTaskAgentField(item, "prompt", "agentPrompt", "agent_prompt", "instructions", "body"),
				Model:          firstTaskAgentField(item, "model", "agentModel", "agent_model"),
				PermissionMode: contracts.PermissionMode(firstTaskAgentField(item, "permissionMode", "permission_mode", "permission-mode", "agentPermissionMode", "agent_permission_mode")),
				AllowedTools:   firstTaskAgentStringList(item, "allowedTools", "allowed_tools", "allowed-tools", "tools", "agentAllowedTools", "agent_allowed_tools"),
			})
		}
		return cleanTaskAgents(agents)
	default:
		return nil
	}
}

func firstTaskAgentField(fields map[string]any, keys ...string) string {
	for _, key := range keys {
		if value, ok := fields[key].(string); ok {
			return strings.TrimSpace(value)
		}
	}
	return ""
}

func cleanTaskAgents(agents []tool.AgentInfo) []tool.AgentInfo {
	out := make([]tool.AgentInfo, 0, len(agents))
	for _, agent := range agents {
		agent.Name = strings.TrimSpace(agent.Name)
		agent.Description = strings.TrimSpace(agent.Description)
		agent.Path = strings.TrimSpace(agent.Path)
		agent.Prompt = strings.TrimSpace(agent.Prompt)
		agent.Model = strings.TrimSpace(agent.Model)
		agent.PermissionMode = contracts.PermissionMode(strings.TrimSpace(string(agent.PermissionMode)))
		agent.AllowedTools = cleanTaskAgentStrings(agent.AllowedTools)
		if agent.Name == "" {
			continue
		}
		out = append(out, agent)
	}
	return out
}

func firstTaskAgentStringList(fields map[string]any, keys ...string) []string {
	for _, key := range keys {
		raw, ok := fields[key]
		if !ok {
			continue
		}
		switch value := raw.(type) {
		case []string:
			return cleanTaskAgentStrings(value)
		case []any:
			values := make([]string, 0, len(value))
			for _, item := range value {
				if text, ok := item.(string); ok {
					values = append(values, text)
				}
			}
			return cleanTaskAgentStrings(values)
		case string:
			return splitTaskAgentTools(value)
		}
	}
	return nil
}

func splitTaskAgentTools(raw string) []string {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return nil
	}
	var parts []string
	if strings.Contains(raw, ",") {
		parts = strings.Split(raw, ",")
	} else {
		parts = strings.Fields(raw)
	}
	return cleanTaskAgentStrings(parts)
}

func cleanTaskAgentStrings(values []string) []string {
	out := make([]string, 0, len(values))
	for _, value := range values {
		value = strings.TrimSpace(value)
		if value == "" {
			continue
		}
		out = append(out, value)
	}
	return out
}

func uniqueSortedAgents(agents []tool.AgentInfo) []tool.AgentInfo {
	agents = cleanTaskAgents(agents)
	seen := map[string]struct{}{}
	out := make([]tool.AgentInfo, 0, len(agents))
	for _, agent := range agents {
		key := agent.Name
		if _, ok := seen[key]; ok {
			continue
		}
		seen[key] = struct{}{}
		out = append(out, agent)
	}
	sort.SliceStable(out, func(i, j int) bool {
		if out[i].Name == builtInGeneralPurposeAgent {
			return true
		}
		if out[j].Name == builtInGeneralPurposeAgent {
			return false
		}
		return out[i].Name < out[j].Name
	})
	return out
}

func taskAgentNames(agents []tool.AgentInfo) []string {
	names := make([]string, 0, len(agents))
	for _, agent := range agents {
		if agent.Name != "" {
			names = append(names, agent.Name)
		}
	}
	return names
}

func taskAgentAllowed(subagentType string, agents []tool.AgentInfo) bool {
	subagentType = strings.TrimSpace(subagentType)
	for _, agent := range agents {
		if subagentType == agent.Name {
			return true
		}
	}
	return false
}

func taskAgentForType(subagentType string, agents []tool.AgentInfo) (tool.AgentInfo, bool) {
	subagentType = strings.TrimSpace(subagentType)
	for _, agent := range agents {
		if subagentType == agent.Name {
			return agent, true
		}
	}
	return tool.AgentInfo{}, false
}
