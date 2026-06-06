package session

import (
	"encoding/json"
	"fmt"
	"math"
	"sort"
	"strconv"
	"strings"
	"unicode"

	"ccgo/internal/contracts"
	msgs "ccgo/internal/messages"
)

type SidechainState struct {
	ID           string
	Path         string
	MetadataPath string
	Subdir       string
	Legacy       bool
	SessionID    contracts.ID
	ParentUUID   *contracts.ID
	Status       string
	Summary      string
	StartedAt    string
	EndedAt      string
	LastUUID     contracts.ID
	MessageCount int
	Metadata     SidechainMetadata
}

var sidechainLifecycleIDFields = []string{
	"sidechainId", "sidechainID", "sidechain_id",
	"subagentId", "subagentID", "subagent_id",
	"agentId", "agentID", "agent_id",
	"taskId", "taskID", "task_id",
	"workerId", "workerID", "worker_id",
	"runId", "runID", "run_id",
	"jobId", "jobID", "job_id",
	"threadId", "threadID", "thread_id",
	"workflowId", "workflowID", "workflow_id",
	"operationId", "operationID", "operation_id",
	"requestId", "requestID", "request_id",
	"id",
}

var sidechainLifecycleAgentTypeFields = []string{
	"agentType", "agent_type",
	"subagentType", "subagent_type",
	"agentKind", "agent_kind",
	"agentRole", "agent_role",
	"workerType", "worker_type",
	"taskType", "task_type",
	"agentName", "agent_name",
	"kind", "name", "agent",
}

var sidechainLifecycleWorktreeFields = []string{
	"worktreePath", "worktree_path",
	"worktree", "worktreeDir", "worktree_dir",
	"workingDirectory", "working_directory",
	"cwd",
	"workspacePath", "workspace_path", "workspace",
	"workspaceRoot", "workspace_root",
	"projectPath", "project_path",
	"projectDir", "project_dir",
	"repoPath", "repo_path",
	"repositoryPath", "repository_path",
	"root", "path", "directory",
}

var sidechainLifecycleDescriptionFields = []string{
	"description", "description_text", "descriptionText",
	"desc", "summary",
	"task", "taskDescription", "task_description",
	"taskPrompt", "task_prompt",
	"instructions", "instruction",
	"objective", "goal", "request",
	"operationName", "operation_name",
	"commandName", "command_name",
	"displayName", "display_name",
	"displayTitle", "display_title",
	"jobName", "job_name",
	"runName", "run_name",
	"workflowName", "workflow_name",
	"prompt", "input", "command", "title",
}

var sidechainLifecycleStartTimeFields = []string{
	"startedAt", "started_at",
	"startTime", "start_time",
	"startedTime", "started_time",
	"createdAt", "created_at",
	"startedAtMs", "started_at_ms",
	"startTimeMs", "start_time_ms",
	"createdAtMs", "created_at_ms",
	"timestamp", "time",
}

var sidechainLifecycleEndTimeFields = []string{
	"endedAt", "ended_at",
	"endTime", "end_time",
	"completedAt", "completed_at",
	"finishedAt", "finished_at",
	"stoppedAt", "stopped_at",
	"endedAtMs", "ended_at_ms",
	"endTimeMs", "end_time_ms",
	"completedAtMs", "completed_at_ms",
	"finishedAtMs", "finished_at_ms",
	"stoppedAtMs", "stopped_at_ms",
	"timestamp", "time",
}

var sidechainLifecycleStartStatusFields = []string{
	"status", "state", "phase", "lifecycle", "lifecycle_state", "lifecycleState",
	"runStatus", "run_status", "taskStatus", "task_status", "jobStatus", "job_status",
}

var sidechainLifecycleSummaryStatusFields = []string{
	"status", "state", "result", "outcome", "phase", "lifecycle", "lifecycle_state", "lifecycleState",
	"runStatus", "run_status", "taskStatus", "task_status", "jobStatus", "job_status",
	"resultStatus", "result_status", "resultState", "result_state",
}

var sidechainLifecycleSummaryFields = []string{
	"summary", "summary_text", "summaryText",
	"finalSummary", "final_summary",
	"resultSummary", "result_summary",
	"resultText", "result_text",
	"resultMessage", "result_message",
	"finalMessage", "final_message",
	"completion", "completionText", "completion_text",
	"outputText", "output_text",
	"messageText", "message_text",
	"errorMessage", "error_message",
	"errorText", "error_text",
	"failureReason", "failure_reason",
	"failedReason", "failed_reason",
	"cancelReason", "cancel_reason",
	"cancellationReason", "cancellation_reason",
	"message", "text", "body", "output", "value", "final",
	"detail", "details",
	"error", "failure", "reason",
}

func LoadSidechainState(info SidechainInfo) (SidechainState, error) {
	transcript, err := LoadTranscript(info.Path)
	if err != nil {
		return SidechainState{}, err
	}
	state := SidechainState{
		ID:           info.ID,
		Path:         info.Path,
		MetadataPath: info.MetadataPath,
		Subdir:       info.Subdir,
		Legacy:       info.Legacy,
		Status:       SidechainStatusUnknown,
	}
	if state.MetadataPath == "" && state.Path != "" {
		state.MetadataPath = replaceJSONLExt(state.Path, ".meta.json")
	}
	metadata, err := ReadSidechainMetadata(state.MetadataPath)
	if err != nil {
		return SidechainState{}, err
	}
	state.Metadata = metadata
	started := false
	finished := false
	for _, id := range transcript.Order {
		msg := transcript.Messages[id]
		if msg == nil {
			continue
		}
		state.MessageCount++
		state.LastUUID = msg.UUID
		if state.ID == "" && msg.AgentID != "" {
			state.ID = msg.AgentID
		}
		if msg.SessionID != "" {
			state.SessionID = msg.SessionID
		}
		if state.ParentUUID == nil && msg.ParentUUID != nil {
			parent := *msg.ParentUUID
			state.ParentUUID = &parent
		}
		if isSidechainStartSubtype(msg.Subtype) {
			started = true
			finished = false
			state.StartedAt = firstNonEmptyString(firstStringField(msg.Content, sidechainLifecycleStartTimeFields...), msg.Timestamp)
			state.EndedAt = ""
			state.Summary = ""
			if sidechainID := firstStringField(msg.Content, sidechainLifecycleIDFields...); sidechainID != "" {
				state.ID = sidechainID
			}
			if agentType := firstStringField(msg.Content, sidechainLifecycleAgentTypeFields...); agentType != "" && state.Metadata.AgentType == "" {
				state.Metadata.AgentType = agentType
			}
			if worktreePath := firstStringField(msg.Content, sidechainLifecycleWorktreeFields...); worktreePath != "" && state.Metadata.WorktreePath == "" {
				state.Metadata.WorktreePath = worktreePath
			}
			if description := firstTextField(msg.Content, sidechainLifecycleDescriptionFields...); description != "" && state.Metadata.Description == "" {
				state.Metadata.Description = description
			}
			if status := sidechainStatusField(msg.Content, sidechainLifecycleStartStatusFields...); status != "" {
				state.Status = status
			} else {
				state.Status = SidechainStatusRunning
			}
		}
		if isSidechainSummarySubtype(msg.Subtype) {
			finished = true
			state.EndedAt = firstNonEmptyString(firstStringField(msg.Content, sidechainLifecycleEndTimeFields...), msg.Timestamp)
			if sidechainID := firstStringField(msg.Content, sidechainLifecycleIDFields...); sidechainID != "" {
				state.ID = sidechainID
			}
			if status := sidechainStatusField(msg.Content, sidechainLifecycleSummaryStatusFields...); status != "" {
				state.Status = status
			} else if status := defaultSidechainSummaryStatus(msg.Subtype); status != "" {
				state.Status = status
			} else {
				state.Status = SidechainStatusCompleted
			}
			state.Summary = firstTextField(msg.Content, sidechainLifecycleSummaryFields...)
		}
	}
	if state.Status == SidechainStatusUnknown {
		switch {
		case finished:
			state.Status = SidechainStatusCompleted
		case started:
			state.Status = SidechainStatusRunning
		}
	}
	if state.ID == "" {
		return SidechainState{}, fmt.Errorf("sidechain state missing id for %s", info.Path)
	}
	return state, nil
}

func isSidechainStartSubtype(subtype string) bool {
	switch sidechainSubtypeAction(subtype) {
	case "start", "started":
		return true
	default:
		return false
	}
}

func isSidechainSummarySubtype(subtype string) bool {
	switch sidechainSubtypeAction(subtype) {
	case "summary", "summarized", "summarised",
		"end", "ended",
		"finish", "finished",
		"complete", "completed",
		"success", "succeeded", "done",
		"fail", "failed", "failure",
		"error", "errored",
		"cancel", "cancelled", "canceled",
		"abort", "aborted":
		return true
	default:
		return false
	}
}

func defaultSidechainSummaryStatus(subtype string) string {
	switch sidechainSubtypeAction(subtype) {
	case "complete", "completed", "success", "succeeded", "done":
		return SidechainStatusCompleted
	case "fail", "failed", "failure", "error", "errored":
		return SidechainStatusFailed
	case "cancel", "cancelled", "canceled", "abort", "aborted":
		return SidechainStatusCancelled
	default:
		return ""
	}
}

func sidechainSubtypeAction(subtype string) string {
	normalized := normalizeSidechainSubtype(subtype)
	for _, prefix := range []string{"sidechain", "subagent", "agent", "task"} {
		if strings.HasPrefix(normalized, prefix) {
			return strings.TrimPrefix(normalized, prefix)
		}
	}
	return ""
}

func normalizeSidechainSubtype(subtype string) string {
	var builder strings.Builder
	for _, r := range strings.TrimSpace(subtype) {
		if unicode.IsLetter(r) || unicode.IsDigit(r) {
			builder.WriteRune(unicode.ToLower(r))
		}
	}
	return builder.String()
}

func sidechainStatusField(value any, keys ...string) string {
	return normalizeSidechainStatus(firstStringField(value, keys...))
}

func normalizeSidechainStatus(status string) string {
	status = strings.TrimSpace(status)
	switch normalizeSidechainStatusAlias(status) {
	case "":
		return ""
	case SidechainStatusRunning, "started", "active", "inprogress", "pending", "queued":
		return SidechainStatusRunning
	case SidechainStatusCompleted, "complete", "completedsuccessfully", "success", "successful", "succeeded", "done", "ok":
		return SidechainStatusCompleted
	case SidechainStatusCancelled, "canceled", "cancel", "cancelledbyuser", "canceledbyuser", "usercancelled", "usercanceled", "aborted", "stopped":
		return SidechainStatusCancelled
	case SidechainStatusFailed, "failure", "error", "errored", "failederror", "failedwitherror", "timeout", "timedout":
		return SidechainStatusFailed
	default:
		return status
	}
}

func normalizeSidechainStatusAlias(status string) string {
	var builder strings.Builder
	for _, r := range strings.TrimSpace(status) {
		if unicode.IsLetter(r) || unicode.IsDigit(r) {
			builder.WriteRune(unicode.ToLower(r))
		}
	}
	return builder.String()
}

func ListSidechainStates(sessionPath string, sessionID contracts.ID) ([]SidechainState, error) {
	infos, err := ListSidechainTranscripts(sessionPath, sessionID)
	if err != nil {
		return nil, err
	}
	states := make([]SidechainState, 0, len(infos))
	for _, info := range infos {
		state, err := LoadSidechainState(info)
		if err != nil {
			return nil, err
		}
		states = append(states, state)
	}
	sort.SliceStable(states, func(i, j int) bool {
		return states[i].ID < states[j].ID
	})
	return states, nil
}

func ResumeSidechainRun(sessionPath string, sessionID contracts.ID, sidechainID string) (SidechainRun, bool, error) {
	state, err := FindSidechainState(sessionPath, sessionID, sidechainID)
	if err != nil {
		return SidechainRun{}, false, err
	}
	if state.SessionID == "" {
		state.SessionID = sessionID
	}
	run, ok := ResumeSidechainRunFromState(state)
	return run, ok, nil
}

func ResumeSidechainRunFromState(state SidechainState) (SidechainRun, bool) {
	if state.Status != SidechainStatusRunning {
		return SidechainRun{}, false
	}
	return SidechainRun{
		ID:           state.ID,
		SessionID:    state.SessionID,
		Path:         state.Path,
		MetadataPath: state.MetadataPath,
		Subdir:       state.Subdir,
		ParentUUID:   state.ParentUUID,
		Status:       state.Status,
		StartedAt:    state.StartedAt,
		EndedAt:      state.EndedAt,
		Metadata:     state.Metadata,
	}, true
}

func FindSidechainState(sessionPath string, sessionID contracts.ID, sidechainID string) (SidechainState, error) {
	id := sanitizeSidechainID(sidechainID)
	info := SidechainInfo{
		ID:           id,
		Path:         SidechainTranscriptPath(sessionPath, sessionID, id),
		MetadataPath: SidechainMetadataPath(sessionPath, sessionID, id),
	}
	state, err := LoadSidechainState(info)
	if err != nil {
		return SidechainState{}, err
	}
	if state.MessageCount > 0 || !state.Metadata.Empty() {
		return state, nil
	}
	states, err := ListSidechainStates(sessionPath, sessionID)
	if err != nil {
		return SidechainState{}, err
	}
	for _, candidate := range states {
		if candidate.ID == id {
			return candidate, nil
		}
	}
	return state, nil
}

func stringField(value any, key string) string {
	return firstStringField(value, key)
}

func firstStringField(value any, keys ...string) string {
	return firstStringFieldDepth(value, keys, 0)
}

func firstTextField(value any, keys ...string) string {
	return firstTextFieldDepth(value, keys, 0)
}

func firstStringFieldDepth(value any, keys []string, depth int) string {
	if depth > 4 {
		return ""
	}
	switch fields := value.(type) {
	case map[string]any:
		for _, key := range keys {
			if raw, ok := fields[key]; ok {
				if value := scalarStringField(raw); value != "" {
					return value
				}
			}
		}
		for _, key := range []string{"payload", "data", "body", "content", "result", "response", "record", "records", "entry", "entries", "item", "items", "event", "events", "edge", "edges", "node", "nodes", "resource", "resources", "attributes", "properties", "attrs", "metadata", "details", "runtime", "context", "state", "value", "values", "output", "outputs", "included", "collection", "list", "children"} {
			if raw, ok := fields[key]; ok {
				if value := firstStringFieldDepth(raw, keys, depth+1); value != "" {
					return value
				}
			}
		}
	case []any:
		for _, item := range fields {
			if value := firstStringFieldDepth(item, keys, depth+1); value != "" {
				return value
			}
		}
	case map[string]string:
		for _, key := range keys {
			if raw := fields[key]; raw != "" {
				return raw
			}
		}
	default:
	}
	return ""
}

func firstTextFieldDepth(value any, keys []string, depth int) string {
	if depth > 4 {
		return ""
	}
	switch fields := value.(type) {
	case map[string]any:
		for _, key := range keys {
			if raw, ok := fields[key]; ok {
				if value := visibleTextField(raw); value != "" {
					return value
				}
			}
		}
		for _, key := range []string{"payload", "data", "body", "content", "result", "response", "record", "records", "entry", "entries", "item", "items", "event", "events", "edge", "edges", "node", "nodes", "resource", "resources", "attributes", "properties", "attrs", "metadata", "details", "runtime", "context", "state", "value", "values", "output", "outputs", "included", "collection", "list", "children"} {
			if raw, ok := fields[key]; ok {
				if value := firstTextFieldDepth(raw, keys, depth+1); value != "" {
					return value
				}
			}
		}
	case []any:
		for _, item := range fields {
			if value := firstTextFieldDepth(item, keys, depth+1); value != "" {
				return value
			}
		}
	case map[string]string:
		for _, key := range keys {
			if raw := fields[key]; raw != "" {
				return raw
			}
		}
	default:
	}
	return ""
}

func visibleTextField(value any) string {
	if value == nil {
		return ""
	}
	if value := scalarStringField(value); value != "" {
		return value
	}
	if text := visibleContentBlockText(value); text != "" {
		return text
	}
	if nonVisibleContentBlock(value) {
		return ""
	}
	if text := visibleMessageText(value); text != "" {
		return text
	}
	switch raw := value.(type) {
	case map[string]any:
		for _, key := range []string{
			"summary", "summaryText", "summary_text", "finalSummary", "final_summary",
			"resultSummary", "result_summary", "resultText", "result_text",
			"finalMessage", "final_message", "completion", "completionText", "completion_text",
			"outputText", "output_text", "messageText", "message_text",
			"body", "text", "message", "content", "parts", "segments", "output", "value",
			"detail", "details", "description",
		} {
			if nested, ok := raw[key]; ok {
				if text := visibleTextField(nested); text != "" {
					return text
				}
			}
		}
	case []any:
		parts := make([]string, 0, len(raw))
		for _, item := range raw {
			if text := visibleTextField(item); text != "" {
				parts = append(parts, text)
			}
		}
		return strings.Join(parts, "\n")
	case map[string]string:
		for _, key := range []string{"text", "body", "message", "content", "value", "output", "summary", "description", "details"} {
			if text := raw[key]; text != "" {
				return text
			}
		}
	}
	return ""
}

func visibleContentBlockText(value any) string {
	data, err := json.Marshal(value)
	if err != nil {
		return ""
	}
	var block contracts.ContentBlock
	if err := json.Unmarshal(data, &block); err != nil {
		return ""
	}
	if block.Type == contracts.ContentText && block.Text != "" {
		return block.Text
	}
	return ""
}

func nonVisibleContentBlock(value any) bool {
	data, err := json.Marshal(value)
	if err != nil {
		return false
	}
	var block contracts.ContentBlock
	if err := json.Unmarshal(data, &block); err != nil {
		return false
	}
	switch block.Type {
	case contracts.ContentThinking, contracts.ContentToolUse, contracts.ContentToolResult, contracts.ContentImage, contracts.ContentCacheEdits:
		return true
	default:
		return false
	}
}

func visibleMessageText(value any) string {
	data, err := json.Marshal(value)
	if err != nil {
		return ""
	}
	var message contracts.Message
	if err := json.Unmarshal(data, &message); err != nil {
		return ""
	}
	return msgs.TextContent(message)
}

func scalarStringField(value any) string {
	switch raw := value.(type) {
	case string:
		return raw
	case json.Number:
		return raw.String()
	case int:
		return strconv.Itoa(raw)
	case int8:
		return strconv.FormatInt(int64(raw), 10)
	case int16:
		return strconv.FormatInt(int64(raw), 10)
	case int32:
		return strconv.FormatInt(int64(raw), 10)
	case int64:
		return strconv.FormatInt(raw, 10)
	case uint:
		return strconv.FormatUint(uint64(raw), 10)
	case uint8:
		return strconv.FormatUint(uint64(raw), 10)
	case uint16:
		return strconv.FormatUint(uint64(raw), 10)
	case uint32:
		return strconv.FormatUint(uint64(raw), 10)
	case uint64:
		return strconv.FormatUint(raw, 10)
	case float32:
		return formatScalarFloat(float64(raw), 32)
	case float64:
		return formatScalarFloat(raw, 64)
	default:
		return ""
	}
}

func formatScalarFloat(value float64, bitSize int) string {
	if math.IsNaN(value) || math.IsInf(value, 0) {
		return ""
	}
	return strconv.FormatFloat(value, 'f', -1, bitSize)
}

func replaceJSONLExt(path string, ext string) string {
	if path == "" {
		return ""
	}
	return strings.TrimSuffix(path, ".jsonl") + ext
}
