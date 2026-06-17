package conversation

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	"ccgo/internal/contracts"
	hookpkg "ccgo/internal/hooks"
	msgs "ccgo/internal/messages"
	"ccgo/internal/tool"
)

func (r Runner) configuredHooks(settings contracts.Settings) []tool.Hook {
	hooks := hookpkg.FromSettings(settings)
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

func (r Runner) runConversationHooks(ctx context.Context, phase string, payload map[string]any) (tool.HookResult, error) {
	settings := r.mergedSettings()
	hooks := conversationHooksForPhase(r.configuredHooks(settings), phase)
	if len(hooks) == 0 {
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
	var combined tool.HookResult
	var messages []string
	for idx, hook := range hooks {
		r.emitConversationHookProgress(phase, idx, "hook_started", nil)
		result, err := hook.RunToolHook(toolCtx, tool.HookEvent{Phase: phase, Input: input, Payload: payload})
		if err != nil {
			r.emitConversationHookProgress(phase, idx, "hook_failed", map[string]any{"error": err.Error()})
			return combined, err
		}
		if len(result.Metadata) > 0 {
			if combined.Metadata == nil {
				combined.Metadata = map[string]any{}
			}
			combined.Metadata[fmt.Sprintf("hook_%d", idx)] = result.Metadata
		}
		if strings.TrimSpace(result.Message) != "" {
			messages = append(messages, strings.TrimSpace(result.Message))
		}
		if result.Block {
			combined.Block = true
			combined.Message = strings.Join(messages, "\n")
			r.emitConversationHookProgress(phase, idx, "hook_blocked", map[string]any{"message": combined.Message})
			return combined, nil
		}
		data := map[string]any{}
		if result.Message != "" {
			data["message"] = result.Message
		}
		r.emitConversationHookProgress(phase, idx, "hook_completed", data)
	}
	combined.Message = strings.Join(messages, "\n")
	return combined, nil
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
