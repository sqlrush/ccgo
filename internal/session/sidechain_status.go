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
	"id",
}

var sidechainLifecycleAgentTypeFields = []string{
	"agentType", "agent_type",
	"subagentType", "subagent_type",
	"agentKind", "agent_kind",
	"agentName", "agent_name",
	"kind", "name", "agent",
}

var sidechainLifecycleWorktreeFields = []string{
	"worktreePath", "worktree_path",
	"worktree", "worktreeDir", "worktree_dir",
	"workingDirectory", "working_directory",
	"cwd",
	"workspacePath", "workspace_path", "workspace",
	"path", "directory",
}

var sidechainLifecycleDescriptionFields = []string{
	"description", "description_text", "descriptionText",
	"desc", "summary",
	"task", "taskDescription", "task_description",
	"prompt", "input", "command", "title",
}

var sidechainLifecycleStartStatusFields = []string{
	"status", "state", "phase", "lifecycle", "lifecycle_state", "lifecycleState",
}

var sidechainLifecycleSummaryStatusFields = []string{
	"status", "state", "result", "outcome", "phase", "lifecycle", "lifecycle_state", "lifecycleState",
}

var sidechainLifecycleSummaryFields = []string{
	"summary", "summary_text", "summaryText",
	"finalSummary", "final_summary",
	"resultSummary", "result_summary",
	"resultText", "result_text",
	"finalMessage", "final_message",
	"message", "text", "output", "value", "final",
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
			state.StartedAt = msg.Timestamp
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
			if description := firstStringField(msg.Content, sidechainLifecycleDescriptionFields...); description != "" && state.Metadata.Description == "" {
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
			state.EndedAt = msg.Timestamp
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
			state.Summary = firstStringField(msg.Content, sidechainLifecycleSummaryFields...)
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
	switch strings.ToLower(strings.TrimSpace(status)) {
	case "":
		return ""
	case SidechainStatusRunning, "started", "active", "in_progress", "in-progress", "pending":
		return SidechainStatusRunning
	case SidechainStatusCompleted, "complete", "completed_successfully", "success", "succeeded", "done", "ok":
		return SidechainStatusCompleted
	case SidechainStatusCancelled, "canceled", "cancel", "cancelled_by_user", "canceled_by_user", "aborted", "stopped":
		return SidechainStatusCancelled
	case SidechainStatusFailed, "failure", "error", "errored", "failed_error":
		return SidechainStatusFailed
	default:
		return strings.TrimSpace(status)
	}
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
		for _, key := range []string{"payload", "data", "body", "content", "result", "response", "record", "entry", "item", "event", "resource", "attributes", "properties", "metadata", "details", "value", "output"} {
			if raw, ok := fields[key]; ok {
				if value := firstStringFieldDepth(raw, keys, depth+1); value != "" {
					return value
				}
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
