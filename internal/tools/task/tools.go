package tasktools

import (
	"encoding/json"
	"fmt"
	"sort"
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
