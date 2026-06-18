package skilltools

import (
	"encoding/json"
	"fmt"
	"strings"

	"ccgo/internal/commands"
	"ccgo/internal/contracts"
	"ccgo/internal/tool"
)

type skillInput struct {
	Skill string `json:"skill"`
	Args  string `json:"args,omitempty"`
}

func NewSkillTool(registries ...commands.Registry) tool.Tool {
	var registry *commands.Registry
	if len(registries) > 0 {
		registry = &registries[0]
	}
	return tool.FuncTool{
		DefinitionValue: contracts.ToolDefinition{
			Name:            "Skill",
			Description:     "Invoke an available prompt skill by name.",
			ReadOnly:        true,
			ConcurrencySafe: false,
			Strict:          true,
			InputSchema: contracts.JSONSchema{
				"type":     "object",
				"required": []any{"skill"},
				"properties": map[string]any{
					"skill": map[string]any{
						"type":        "string",
						"description": "The skill name. For example: commit, review-pr, or pdf.",
					},
					"args": map[string]any{
						"type":        "string",
						"description": "Optional arguments for the skill.",
					},
				},
			},
		},
		NormalizeFunc:   normalizeSkillInput,
		PromptFunc:      skillPrompt(registry),
		ValidateFunc:    validateSkill(registry),
		PermissionFunc:  allowSkill,
		CallFunc:        callSkill(registry),
		ReadOnlyFunc:    func(json.RawMessage) bool { return true },
		ConcurrencyFunc: func(json.RawMessage) bool { return false },
	}
}

func skillPrompt(registry *commands.Registry) func(tool.PromptContext) (string, error) {
	return func(ctx tool.PromptContext) (string, error) {
		r := registryForPromptContext(ctx, registry)
		skills := r.SkillToolCommands()
		if len(skills) == 0 {
			return "Invokes an available prompt skill by name with optional arguments. No prompt skills are currently discoverable.", nil
		}
		names := make([]string, 0, len(skills))
		for _, cmd := range skills {
			names = append(names, commands.UserFacingName(cmd))
		}
		return "Invokes an available prompt skill by name with optional arguments. Available skills: " + strings.Join(names, ", "), nil
	}
}

func normalizeSkillInput(raw json.RawMessage) (json.RawMessage, error) {
	var obj map[string]any
	if err := json.Unmarshal(raw, &obj); err != nil {
		return nil, err
	}
	input := skillInput{}
	if value, ok := firstString(obj, "skill", "commandName", "command_name", "name"); ok {
		input.Skill = value
	}
	if value, ok := firstString(obj, "args", "arguments", "argument"); ok {
		input.Args = value
	}
	normalized, err := json.Marshal(input)
	if err != nil {
		return nil, err
	}
	return normalized, nil
}

func validateSkill(registry *commands.Registry) tool.ValidateFunc {
	return func(ctx tool.Context, raw json.RawMessage) error {
		input, err := decodeSkillInput(raw)
		if err != nil {
			return err
		}
		r := registryForContext(ctx, registry)
		cmd, ok := r.Find(input.Skill)
		if !ok {
			return fmt.Errorf("skill %q not found", input.Skill)
		}
		if cmd.Type != contracts.CommandPrompt {
			return fmt.Errorf("command %q is %q, not prompt", input.Skill, cmd.Type)
		}
		if cmd.DisableModelInvocation {
			return fmt.Errorf("skill %q cannot be used with Skill tool due to disable-model-invocation", input.Skill)
		}
		if _, ok := r.PromptTemplate(input.Skill); !ok {
			return fmt.Errorf("skill %q has no prompt template", input.Skill)
		}
		return nil
	}
}

func allowSkill(tool.Context, json.RawMessage) (contracts.PermissionDecision, error) {
	return contracts.PermissionDecision{
		Behavior:       contracts.PermissionAllow,
		DecisionReason: "prompt skill expansion is read-only",
	}, nil
}

func callSkill(registry *commands.Registry) tool.CallFunc {
	return func(ctx tool.Context, raw json.RawMessage, _ tool.ProgressSink) (contracts.ToolResult, error) {
		input, err := decodeSkillInput(raw)
		if err != nil {
			return contracts.ToolResult{}, err
		}
		r := registryForContext(ctx, registry)
		expanded, err := r.ExpandPrompt(input.Skill, input.Args, ctx.SessionID)
		if err != nil {
			return contracts.ToolResult{}, err
		}
		allowedTools := commands.ParseToolList(expanded.Command.AllowedTools)
		structured := map[string]any{
			"success":     true,
			"commandName": expanded.Command.Name,
			"status":      "inline",
		}
		if len(allowedTools) > 0 {
			structured["allowedTools"] = append([]string(nil), allowedTools...)
		}
		if expanded.Command.Model != "" {
			structured["model"] = expanded.Command.Model
		}
		newMessages := []contracts.Message{expanded.Message}
		if attachment := commands.CommandPermissionsAttachment(allowedTools, expanded.Command.Model, ctx.SessionID); attachment.Type != "" {
			newMessages = append(newMessages, attachment)
		}
		return contracts.ToolResult{
			Content:           "Launching skill: " + expanded.Command.Name,
			NewMessages:       newMessages,
			StructuredContent: structured,
		}, nil
	}
}

func decodeSkillInput(raw json.RawMessage) (skillInput, error) {
	var input skillInput
	if err := json.Unmarshal(raw, &input); err != nil {
		return skillInput{}, err
	}
	input.Skill = strings.TrimSpace(strings.TrimPrefix(input.Skill, "/"))
	if input.Skill == "" {
		return skillInput{}, fmt.Errorf("input.skill is required")
	}
	return input, nil
}

func registryForContext(ctx tool.Context, registry *commands.Registry) commands.Registry {
	if registry != nil {
		return *registry
	}
	return commands.Load(commands.Options{CWD: ctx.WorkingDirectory, Settings: settingsFromMetadata(ctx.Metadata), PolicySettings: policySettingsFromMetadata(ctx.Metadata)})
}

func registryForPromptContext(ctx tool.PromptContext, registry *commands.Registry) commands.Registry {
	if registry != nil {
		return *registry
	}
	return commands.Load(commands.Options{CWD: ctx.WorkingDirectory, Settings: settingsFromMetadata(ctx.Metadata), PolicySettings: policySettingsFromMetadata(ctx.Metadata)})
}

func settingsFromMetadata(metadata map[string]any) contracts.Settings {
	if settings, ok := metadata[tool.MetadataSettingsKey].(contracts.Settings); ok {
		return settings
	}
	return contracts.Settings{}
}

func policySettingsFromMetadata(metadata map[string]any) contracts.Settings {
	if settings, ok := metadata[tool.MetadataPolicySettingsKey].(contracts.Settings); ok {
		return settings
	}
	return contracts.Settings{}
}

func firstString(obj map[string]any, keys ...string) (string, bool) {
	for _, key := range keys {
		if value, ok := obj[key].(string); ok {
			return value, true
		}
	}
	return "", false
}
