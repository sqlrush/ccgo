package conversation

import (
	"context"
	"fmt"
	"strings"
	"time"

	"ccgo/internal/contracts"
	msgs "ccgo/internal/messages"
	"ccgo/internal/permissions"
	"ccgo/internal/session"
	"ccgo/internal/tool"
	tasktools "ccgo/internal/tools/task"
)

func (r Runner) maybeRunTaskSubagent(ctx context.Context, use contracts.ToolUse, result *contracts.ToolResult) {
	if result == nil || result.IsError || !taskToolRunRequested(result.StructuredContent) {
		return
	}
	sidechainID := taskToolResultString(result.StructuredContent, "sidechain_id", "task_id")
	if sidechainID == "" {
		result.IsError = true
		result.Content = "task result requested subagent run but did not include sidechain_id"
		return
	}
	r.emit(Event{Type: EventToolProgress, ToolProgress: &contracts.ToolProgress{
		ToolUseID: use.ID,
		Type:      "task_agent_started",
		Data: map[string]any{
			"task_id":      sidechainID,
			"sidechain_id": sidechainID,
		},
	}})
	outcome, err := r.runTaskSubagentOnce(ctx, sidechainID)
	if err != nil {
		result.IsError = true
		result.Content = fmt.Sprintf("Task subagent failed: %v", err)
		result.StructuredContent["status"] = session.SidechainStatusFailed
		result.StructuredContent["running"] = false
		result.StructuredContent["error"] = err.Error()
		r.emit(Event{Type: EventToolProgress, ToolProgress: &contracts.ToolProgress{
			ToolUseID: use.ID,
			Type:      "task_agent_failed",
			Data: map[string]any{
				"task_id":      sidechainID,
				"sidechain_id": sidechainID,
				"error":        err.Error(),
			},
		}})
		return
	}
	result.Content = fmt.Sprintf("Task completed: %s\nSummary: %s", sidechainID, outcome.Summary)
	result.StructuredContent["status"] = session.SidechainStatusCompleted
	result.StructuredContent["running"] = false
	result.StructuredContent["summary"] = outcome.Summary
	result.StructuredContent["agent_model"] = outcome.Model
	result.StructuredContent["agent_stop_reason"] = outcome.StopReason
	if outcome.WorktreeCleanupAttempted {
		result.StructuredContent["worktree_cleanup_attempted"] = true
		result.StructuredContent["worktree_cleanup_status"] = outcome.WorktreeCleanupStatus
		if outcome.WorktreeCleanupReason != "" {
			result.StructuredContent["worktree_cleanup_reason"] = outcome.WorktreeCleanupReason
		}
	}
	r.emit(Event{Type: EventToolProgress, ToolProgress: &contracts.ToolProgress{
		ToolUseID: use.ID,
		Type:      "task_agent_completed",
		Data: map[string]any{
			"task_id":      sidechainID,
			"sidechain_id": sidechainID,
			"status":       session.SidechainStatusCompleted,
			"summary":      outcome.Summary,
		},
	}})
	if outcome.WorktreeCleanupAttempted {
		r.emit(Event{Type: EventToolProgress, ToolProgress: &contracts.ToolProgress{
			ToolUseID: use.ID,
			Type:      "task_worktree_cleanup",
			Data: map[string]any{
				"task_id":        sidechainID,
				"sidechain_id":   sidechainID,
				"cleanup_status": outcome.WorktreeCleanupStatus,
				"cleanup_reason": outcome.WorktreeCleanupReason,
			},
		}})
	}
}

type taskSubagentOutcome struct {
	Summary                  string
	Model                    string
	StopReason               string
	WorktreeCleanupAttempted bool
	WorktreeCleanupStatus    string
	WorktreeCleanupReason    string
}

func (r Runner) runTaskSubagentOnce(ctx context.Context, sidechainID string) (taskSubagentOutcome, error) {
	if r.SessionPath == "" || r.SessionID == "" {
		return taskSubagentOutcome{}, fmt.Errorf("session path and id are required")
	}
	manager := session.NewSidechainManager(r.SessionPath, r.SessionID)
	state, err := session.FindSidechainState(r.SessionPath, r.SessionID, sidechainID)
	if err != nil {
		return taskSubagentOutcome{}, err
	}
	if state.Status != session.SidechainStatusRunning {
		return taskSubagentOutcome{}, fmt.Errorf("task %s is not running", state.ID)
	}
	conversation, err := manager.Conversation(sidechainID)
	if err != nil {
		return taskSubagentOutcome{}, err
	}
	if len(conversation.Messages) == 0 {
		return taskSubagentOutcome{}, fmt.Errorf("task %s has no prompt messages", state.ID)
	}
	subRunner := r
	subRunner.MCP = nil
	if worktreePath := strings.TrimSpace(state.Metadata.WorktreePath); worktreePath != "" {
		subRunner.WorkingDirectory = worktreePath
	}
	subRunner.SystemPrompt = taskSubagentSystemPrompt(r.SystemPrompt, state.Metadata.AgentPrompt)
	if len(state.Metadata.AgentAllowedTools) > 0 {
		executor, err := taskSubagentAllowedToolExecutor(subRunner.Tools, state.Metadata.AgentAllowedTools)
		if err != nil {
			return taskSubagentOutcome{}, err
		}
		subRunner.Tools = executor
	}
	if state.Metadata.AgentModel != "" {
		subRunner.Model = state.Metadata.AgentModel
	}
	history := append([]contracts.Message(nil), conversation.Messages...)
	for round := 0; ; round++ {
		if round >= subRunner.maxToolRounds() {
			err := fmt.Errorf("task %s exceeded maximum subagent tool rounds: %d", state.ID, subRunner.maxToolRounds())
			_, _ = manager.Fail(state.ID, err.Error(), time.Now().UTC())
			return taskSubagentOutcome{}, err
		}
		_, _, response, _, err := subRunner.send(ctx, history, nil)
		if err != nil {
			_, _ = manager.Fail(state.ID, err.Error(), time.Now().UTC())
			return taskSubagentOutcome{}, err
		}
		assistant := messageFromResponse(r.SessionID, response)
		if err := manager.Append(state.ID, session.TranscriptMessage{
			Type:        string(contracts.MessageAssistant),
			UUID:        assistant.UUID,
			SessionID:   r.SessionID,
			Timestamp:   time.Now().UTC().Format(time.RFC3339Nano),
			IsSidechain: true,
			AgentID:     state.ID,
			Message:     &assistant,
		}); err != nil {
			return taskSubagentOutcome{}, err
		}
		history, assistant = appendMessage(history, assistant)
		uses := ToolUses(assistant)
		if len(uses) == 0 {
			summary := strings.TrimSpace(msgs.TextContent(assistant))
			if summary == "" {
				summary = strings.TrimSpace(response.StopReason)
			}
			if summary == "" {
				summary = "subagent completed"
			}
			if _, err := manager.Finish(state.ID, session.SidechainStatusCompleted, summary, time.Now().UTC()); err != nil {
				return taskSubagentOutcome{}, err
			}
			outcome := taskSubagentOutcome{Summary: summary, Model: response.Model, StopReason: response.StopReason}
			cleanup, err := tasktools.CleanupOwnedWorktree(tool.Context{
				Context:          ctx,
				WorkingDirectory: r.WorkingDirectory,
				SessionID:        r.SessionID,
				Metadata:         r.toolMetadata(),
			}, manager, state, "subagent completed")
			if err != nil {
				return taskSubagentOutcome{}, err
			}
			if cleanup.Attempted {
				outcome.WorktreeCleanupAttempted = true
				outcome.WorktreeCleanupStatus = cleanup.Status
				outcome.WorktreeCleanupReason = cleanup.Reason
			}
			return outcome, nil
		}
		toolMessages, _ := subRunner.executeToolUses(ctx, uses, subRunner.toolMetadata(), history)
		for i := range toolMessages {
			history, toolMessages[i] = appendMessage(history, toolMessages[i])
			if err := manager.Append(state.ID, session.TranscriptMessage{
				Type:        string(toolMessages[i].Type),
				UUID:        toolMessages[i].UUID,
				ParentUUID:  toolMessages[i].ParentUUID,
				SessionID:   r.SessionID,
				Timestamp:   time.Now().UTC().Format(time.RFC3339Nano),
				IsSidechain: true,
				AgentID:     state.ID,
				Message:     &toolMessages[i],
			}); err != nil {
				return taskSubagentOutcome{}, err
			}
		}
	}
}

func taskSubagentSystemPrompt(base string, agentPrompt string) string {
	base = strings.TrimSpace(base)
	agentPrompt = strings.TrimSpace(agentPrompt)
	switch {
	case base == "":
		return agentPrompt
	case agentPrompt == "":
		return base
	default:
		return base + "\n\n" + agentPrompt
	}
}

func taskToolRunRequested(structured map[string]any) bool {
	if structured == nil {
		return false
	}
	if kind, _ := structured["type"].(string); kind != "task" {
		return false
	}
	value, _ := structured["run"].(bool)
	return value
}

func taskToolResultString(structured map[string]any, keys ...string) string {
	for _, key := range keys {
		if value, ok := structured[key].(string); ok && strings.TrimSpace(value) != "" {
			return strings.TrimSpace(value)
		}
	}
	return ""
}

func taskSubagentAllowedToolExecutor(executor tool.Executor, allowedTools []string) (tool.Executor, error) {
	if executor.Registry == nil || len(allowedTools) == 0 {
		return executor, nil
	}
	var tools []tool.Tool
	seen := map[string]struct{}{}
	for _, raw := range allowedTools {
		raw = strings.TrimSpace(raw)
		if raw == "" {
			continue
		}
		if raw == "*" {
			return executor, nil
		}
		candidates := taskAllowedToolNameCandidates(raw)
		for _, name := range candidates {
			t, ok := executor.Registry.Lookup(name)
			if !ok {
				continue
			}
			key := strings.ToLower(strings.TrimSpace(t.Name()))
			if key == "" {
				continue
			}
			if _, ok := seen[key]; ok {
				continue
			}
			seen[key] = struct{}{}
			tools = append(tools, t)
			break
		}
	}
	registry, err := tool.NewRegistry(tools...)
	if err != nil {
		return tool.Executor{}, err
	}
	executor.Registry = registry
	return executor, nil
}

func taskAllowedToolNameCandidates(raw string) []string {
	value := permissions.PermissionRuleValueFromString(raw)
	var candidates []string
	if value.ToolName != "" {
		candidates = append(candidates, value.ToolName)
	}
	if open := strings.Index(raw, "("); open > 0 {
		candidates = append(candidates, strings.TrimSpace(raw[:open]))
	}
	candidates = append(candidates, raw)
	return candidates
}
