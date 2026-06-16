package conversation

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"path/filepath"
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
		status := session.SidechainStatusFailed
		if errors.Is(err, context.Canceled) {
			status = session.SidechainStatusCancelled
			result.StructuredContent["cancelled"] = true
		}
		result.IsError = true
		result.Content = fmt.Sprintf("Task subagent failed: %v", err)
		result.StructuredContent["status"] = status
		result.StructuredContent["running"] = false
		result.StructuredContent["error"] = err.Error()
		r.copyTaskSubagentWorktreeCleanup(sidechainID, result.StructuredContent)
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
	if mode := strings.TrimSpace(state.Metadata.AgentPermissionMode); mode != "" {
		subRunner.Permissions = taskSubagentPermissionMode(subRunner.Permissions, mode)
	}
	if len(state.Metadata.AgentAllowedTools) > 0 {
		executor, err := taskSubagentAllowedToolExecutor(subRunner.Tools, state.Metadata.AgentAllowedTools)
		if err != nil {
			return taskSubagentOutcome{}, err
		}
		subRunner.Tools = executor
		subRunner.Permissions = taskSubagentAllowedToolPermissions(subRunner.Permissions, state.Metadata.AgentAllowedTools)
	}
	if state.Metadata.AgentModel != "" {
		subRunner.Model = state.Metadata.AgentModel
	}
	history := append([]contracts.Message(nil), conversation.Messages...)
	for round := 0; ; round++ {
		if round >= subRunner.maxToolRounds() {
			err := fmt.Errorf("task %s exceeded maximum subagent tool rounds: %d", state.ID, subRunner.maxToolRounds())
			return taskSubagentOutcome{}, r.finishTaskSubagentError(ctx, manager, state, err)
		}
		_, _, response, _, err := subRunner.send(ctx, history, nil)
		if err != nil {
			return taskSubagentOutcome{}, r.finishTaskSubagentError(ctx, manager, state, err)
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
			return taskSubagentOutcome{}, r.finishTaskSubagentError(ctx, manager, state, err)
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
		if err := ctx.Err(); err != nil {
			return taskSubagentOutcome{}, r.finishTaskSubagentError(ctx, manager, state, err)
		}
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
				return taskSubagentOutcome{}, r.finishTaskSubagentError(ctx, manager, state, err)
			}
		}
	}
}

func (r Runner) finishTaskSubagentError(ctx context.Context, manager session.SidechainManager, state session.SidechainState, err error) error {
	status := session.SidechainStatusFailed
	reason := err.Error()
	if errors.Is(err, context.Canceled) {
		status = session.SidechainStatusCancelled
		if strings.TrimSpace(reason) == "" {
			reason = "subagent cancelled"
		}
	}
	if status == session.SidechainStatusCancelled {
		_, _ = manager.Cancel(state.ID, reason, time.Now().UTC())
	} else {
		_, _ = manager.Fail(state.ID, reason, time.Now().UTC())
	}
	cleanup, cleanupErr := tasktools.CleanupOwnedWorktree(tool.Context{
		Context:          ctx,
		WorkingDirectory: r.WorkingDirectory,
		SessionID:        r.SessionID,
		Metadata:         r.toolMetadata(),
	}, manager, state, "subagent "+status)
	if cleanupErr != nil {
		return fmt.Errorf("%w; worktree cleanup failed: %v", err, cleanupErr)
	}
	if cleanup.Attempted && cleanup.Status == "failed" {
		return fmt.Errorf("%w; worktree cleanup failed: %s", err, cleanup.Reason)
	}
	return err
}

func (r Runner) copyTaskSubagentWorktreeCleanup(sidechainID string, structured map[string]any) {
	if structured == nil || r.SessionPath == "" || r.SessionID == "" {
		return
	}
	state, err := session.FindSidechainState(r.SessionPath, r.SessionID, sidechainID)
	if err != nil {
		return
	}
	if state.Metadata.WorktreeCleanupStatus == "" {
		return
	}
	structured["worktree_cleanup_attempted"] = true
	structured["worktree_cleanup_status"] = state.Metadata.WorktreeCleanupStatus
	if state.Metadata.WorktreeCleanupReason != "" {
		structured["worktree_cleanup_reason"] = state.Metadata.WorktreeCleanupReason
	}
	if state.Metadata.WorktreeCleanupAt != "" {
		structured["worktree_cleanup_at"] = state.Metadata.WorktreeCleanupAt
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

func taskSubagentAllowedToolPermissions(base tool.PermissionDecider, allowedTools []string) tool.PermissionDecider {
	rules := taskSubagentAllowedToolPermissionRules(allowedTools)
	if len(rules) == 0 {
		return base
	}
	next := taskPermissionDeciderWithRules(base, rules)
	return taskSubagentScopedPermissionDecider{Base: next, Rules: rules}
}

func taskSubagentPermissionMode(base tool.PermissionDecider, rawMode string) tool.PermissionDecider {
	mode, ok := parsePermissionMode(rawMode)
	if !ok {
		return base
	}
	switch decider := base.(type) {
	case tool.EnginePermissionDecider:
		context := decider.Engine.Context()
		context.Mode = mode
		return tool.NewEnginePermissionDecider(permissions.NewEngine(context, decider.Engine.Rules()...))
	case *tool.EnginePermissionDecider:
		if decider == nil {
			break
		}
		context := decider.Engine.Context()
		context.Mode = mode
		return tool.NewEnginePermissionDecider(permissions.NewEngine(context, decider.Engine.Rules()...))
	}
	if base == nil {
		return tool.NewEnginePermissionDecider(permissions.NewEngine(contracts.PermissionContext{Mode: mode}))
	}
	return base
}

func taskSubagentAllowedToolPermissionRules(allowedTools []string) []permissions.Rule {
	var rules []permissions.Rule
	for _, raw := range allowedTools {
		raw = strings.TrimSpace(raw)
		if raw == "" || raw == "*" {
			continue
		}
		rule, err := permissions.ParseRule(contracts.PermissionSourceSession, contracts.PermissionAllow, raw)
		if err != nil {
			continue
		}
		rules = append(rules, rule)
	}
	return rules
}

func taskPermissionDeciderWithRules(base tool.PermissionDecider, rules []permissions.Rule) tool.PermissionDecider {
	if len(rules) == 0 {
		return base
	}
	switch decider := base.(type) {
	case tool.EnginePermissionDecider:
		baseRules := decider.Engine.Rules()
		baseRules = append(baseRules, rules...)
		return tool.NewEnginePermissionDecider(permissions.NewEngine(decider.Engine.Context(), baseRules...))
	case *tool.EnginePermissionDecider:
		if decider == nil {
			break
		}
		baseRules := decider.Engine.Rules()
		baseRules = append(baseRules, rules...)
		return tool.NewEnginePermissionDecider(permissions.NewEngine(decider.Engine.Context(), baseRules...))
	}
	return base
}

type taskSubagentScopedPermissionDecider struct {
	Base  tool.PermissionDecider
	Rules []permissions.Rule
}

func (d taskSubagentScopedPermissionDecider) DecideTool(t tool.Tool, raw json.RawMessage, ctx tool.Context) (contracts.PermissionDecision, error) {
	req := taskPermissionRequest(t, raw, ctx)
	if taskPermissionToolHasScopedRules(d.Rules, req.ToolName) && !taskPermissionAnyRuleMatches(d.Rules, req) {
		return contracts.PermissionDecision{
			Behavior:       contracts.PermissionDeny,
			Message:        fmt.Sprintf("%s is not allowed by this agent's tool allowlist", req.ToolName),
			DecisionReason: "agent allowed-tools pattern did not match",
		}, nil
	}
	if d.Base != nil {
		return d.Base.DecideTool(t, raw, ctx)
	}
	return contracts.PermissionDecision{Behavior: contracts.PermissionAllow, DecisionReason: "agent tool allowlist matched"}, nil
}

func taskPermissionRequest(t tool.Tool, raw json.RawMessage, ctx tool.Context) permissions.Request {
	return permissions.Request{
		ToolName:                  t.Name(),
		Input:                     raw,
		Command:                   taskFirstInputString(raw, "command", "cmd"),
		Path:                      taskFirstInputString(raw, "file_path", "notebook_path", "path"),
		WorkingDirectory:          ctx.WorkingDirectory,
		ReadOnly:                  t.IsReadOnly(raw),
		WritesFiles:               !t.IsReadOnly(raw) && !t.IsDestructive(raw),
		Destructive:               t.IsDestructive(raw),
		DangerouslyDisableSandbox: taskFirstInputBool(raw, "dangerouslyDisableSandbox", "dangerously_disable_sandbox"),
		InternalPaths:             tool.InternalPathContextFromMetadata(ctx.Metadata),
	}
}

func taskPermissionToolHasScopedRules(rules []permissions.Rule, toolName string) bool {
	scoped := false
	for _, rule := range rules {
		if !taskPermissionRuleMatchesTool(rule, toolName) {
			continue
		}
		if rule.Pattern == "" || rule.Pattern == "*" {
			return false
		}
		scoped = true
	}
	return scoped
}

func taskPermissionAnyRuleMatches(rules []permissions.Rule, req permissions.Request) bool {
	for _, rule := range rules {
		if rule.Matches(req) {
			return true
		}
	}
	return false
}

func taskPermissionRuleMatchesTool(rule permissions.Rule, toolName string) bool {
	pattern := strings.TrimSpace(rule.ToolName)
	toolName = strings.TrimSpace(toolName)
	if pattern == "*" {
		return true
	}
	if strings.EqualFold(pattern, toolName) {
		return true
	}
	ok, err := filepath.Match(pattern, toolName)
	return err == nil && ok
}

func taskFirstInputString(raw json.RawMessage, keys ...string) string {
	var obj map[string]any
	if err := json.Unmarshal(raw, &obj); err != nil {
		return ""
	}
	for _, key := range keys {
		if value, ok := obj[key].(string); ok && strings.TrimSpace(value) != "" {
			return strings.TrimSpace(value)
		}
	}
	return ""
}

func taskFirstInputBool(raw json.RawMessage, keys ...string) bool {
	var obj map[string]any
	if err := json.Unmarshal(raw, &obj); err != nil {
		return false
	}
	for _, key := range keys {
		switch value := obj[key].(type) {
		case bool:
			return value
		case string:
			return strings.EqualFold(strings.TrimSpace(value), "true")
		}
	}
	return false
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
