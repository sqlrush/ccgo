package conversation

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	compactpkg "ccgo/internal/compact"
	"ccgo/internal/config"
	"ccgo/internal/contracts"
	hookpkg "ccgo/internal/hooks"
	msgs "ccgo/internal/messages"
	"ccgo/internal/tool"
)

func (r Runner) configuredHooks(settings contracts.Settings) []tool.Hook {
	hookSettings := settings
	if r.MCP != nil && config.IsRestrictedToPluginOnly(r.MCP.PolicySettings, config.CustomizationSurfaceHooks) {
		hookSettings = r.MCP.PolicySettings
	}
	hooks := hookpkg.FromSettings(hookSettings)
	hooks = append(hooks, r.pluginToolHooks(settings)...)
	return hooks
}

func (r Runner) applyUserPromptSubmitHooks(ctx context.Context, messages []contracts.Message) ([]contracts.Message, bool, string, error) {
	prompt := userPromptText(messages)
	if strings.TrimSpace(prompt) == "" {
		return messages, false, "", nil
	}
	result, err := r.runConversationHooks(ctx, tool.HookUserPromptSubmit, map[string]any{"prompt": prompt})
	if err != nil {
		return messages, false, "", err
	}
	if result.Block {
		message := result.Message
		if strings.TrimSpace(message) == "" {
			message = "blocked by UserPromptSubmit hook"
		}
		return messages, true, message, nil
	}
	if strings.TrimSpace(result.Message) != "" {
		messages = appendUserPromptContext(messages, result.Message)
	}
	return messages, false, "", nil
}

func (r Runner) runStopHooks(ctx context.Context, responseModel string, stopReason string, stopSequence string, assistant contracts.Message) error {
	payload := map[string]any{
		"model":          responseModel,
		"stop_reason":    stopReason,
		"stop_sequence":  stopSequence,
		"assistant_text": msgs.TextContent(assistant),
		"message_id":     assistant.ID,
		"message_uuid":   string(assistant.UUID),
	}
	result, err := r.runConversationHooks(ctx, tool.HookStop, payload)
	if err != nil {
		return err
	}
	if result.Block {
		message := result.Message
		if strings.TrimSpace(message) == "" {
			message = "blocked by Stop hook"
		}
		return fmt.Errorf("%s", message)
	}
	return nil
}

func (r Runner) runSubagentStopHooks(ctx context.Context, payload map[string]any) error {
	result, err := r.runConversationHooks(ctx, tool.HookSubagentStop, payload)
	if err != nil {
		return err
	}
	if result.Block {
		message := result.Message
		if strings.TrimSpace(message) == "" {
			message = "blocked by SubagentStop hook"
		}
		return fmt.Errorf("%s", message)
	}
	return nil
}

func (r Runner) runSubagentStartHooks(ctx context.Context, payload map[string]any) error {
	result, err := r.runConversationHooks(ctx, tool.HookSubagentStart, payload)
	if err != nil {
		return err
	}
	if result.Block {
		message := result.Message
		if strings.TrimSpace(message) == "" {
			message = "blocked by SubagentStart hook"
		}
		return fmt.Errorf("%s", message)
	}
	return nil
}

func (r Runner) runPostCompactHooks(ctx context.Context, trigger compactpkg.Trigger, summary string) error {
	_, err := r.runConversationHooks(ctx, tool.HookPostCompact, map[string]any{
		"trigger":         string(trigger),
		"compact_summary": summary,
	})
	return err
}

func (r Runner) runPreCompactHooks(ctx context.Context, trigger compactpkg.Trigger, tokenUsage int, messageCount int, userContext string, extraInstructions string) (string, bool, error) {
	payload := map[string]any{
		"trigger":             string(trigger),
		"token_usage":         tokenUsage,
		"message_count":       messageCount,
		"user_context":        userContext,
		"extra_instructions":  extraInstructions,
		"custom_instructions": userContext,
	}
	result, err := r.runConversationHooks(ctx, tool.HookPreCompact, payload)
	if err != nil {
		return "", false, err
	}
	return result.Message, result.Block, nil
}

func (r Runner) runConversationHooks(ctx context.Context, phase string, payload map[string]any) (tool.HookResult, error) {
	settings := r.mergedSettings()
	candidates := conversationHooksForPhase(r.configuredHooks(settings), phase)
	matched := filterByMatcher(phase, candidates, payload)
	if len(matched) == 0 {
		return tool.HookResult{}, nil
	}
	input, err := json.Marshal(payload)
	if err != nil {
		return tool.HookResult{}, err
	}
	toolCtx := tool.Context{
		Context:          ctx,
		WorkingDirectory: r.WorkingDirectory,
		SessionID:        r.SessionID,
		Metadata:         r.toolMetadata(),
	}
	for idx := range matched {
		r.emitConversationHookProgress(phase, idx, "hook_started", nil)
	}
	resolution, err := hookpkg.Resolve(toolCtx, matched, tool.HookEvent{Phase: phase, Input: input, Payload: payload})
	if err != nil {
		r.emitConversationHookProgress(phase, 0, "hook_failed", map[string]any{"error": err.Error()})
		return tool.HookResult{}, err
	}
	if resolution.Block {
		r.emitConversationHookProgress(phase, 0, "hook_blocked", map[string]any{"message": resolution.Message})
	} else {
		r.emitConversationHookProgress(phase, 0, "hook_completed", map[string]any{"message": resolution.Message})
	}
	return tool.HookResult{
		Block:              resolution.Block,
		Message:            resolution.Message,
		UpdatedInput:       resolution.UpdatedInput,
		PermissionDecision: resolution.PermissionDecision,
		Metadata:           resolution.Metadata,
	}, nil
}

func filterByMatcher(phase string, candidates []tool.Hook, payload map[string]any) []tool.Hook {
	query, honored := hookpkg.MatchQuery(phase, payload)
	if !honored {
		return candidates
	}
	out := make([]tool.Hook, 0, len(candidates))
	for _, hook := range candidates {
		if hookpkg.Matches(hook, query) {
			out = append(out, hook)
		}
	}
	return out
}

func (r Runner) emitConversationHookProgress(phase string, index int, progressType string, data map[string]any) {
	if data == nil {
		data = map[string]any{}
	}
	data["phase"] = phase
	data["hook_index"] = index
	data["scope"] = "conversation"
	r.emit(Event{Type: EventToolProgress, ToolProgress: &contracts.ToolProgress{
		ToolUseID: contracts.ID("hook_" + phase),
		Type:      progressType,
		Data:      data,
	}})
}

func conversationHooksForPhase(hooks []tool.Hook, phase string) []tool.Hook {
	out := make([]tool.Hook, 0, len(hooks))
	for _, hook := range hooks {
		if hook == nil {
			continue
		}
		phaseHook, ok := hook.(tool.PhaseHook)
		if !ok {
			out = append(out, hook)
			continue
		}
		for _, candidate := range phaseHook.HookPhases() {
			if candidate == phase {
				out = append(out, hook)
				break
			}
		}
	}
	return out
}

func userPromptText(messages []contracts.Message) string {
	for i := len(messages) - 1; i >= 0; i-- {
		if messages[i].Type == contracts.MessageUser {
			text := msgs.TextContent(messages[i])
			if strings.TrimSpace(text) != "" {
				return text
			}
		}
	}
	return ""
}

func appendUserPromptContext(messages []contracts.Message, contextText string) []contracts.Message {
	contextText = strings.TrimSpace(contextText)
	if contextText == "" {
		return messages
	}
	out := append([]contracts.Message(nil), messages...)
	for i := len(out) - 1; i >= 0; i-- {
		if out[i].Type != contracts.MessageUser {
			continue
		}
		out[i].Content = append([]contracts.ContentBlock(nil), out[i].Content...)
		for j := range out[i].Content {
			if out[i].Content[j].Type == contracts.ContentText {
				out[i].Content[j].Text = strings.TrimRight(out[i].Content[j].Text, "\n") + "\n\n" + contextText
				return out
			}
		}
		out[i].Content = append(out[i].Content, contracts.NewTextBlock(contextText))
		return out
	}
	return out
}

func appendHookInstructions(base string, extra string) string {
	base = strings.TrimSpace(base)
	extra = strings.TrimSpace(extra)
	if base == "" {
		return extra
	}
	if extra == "" {
		return base
	}
	return base + "\n\n" + extra
}
