package session

import (
	"os"
	"path/filepath"
	"testing"
	"time"

	"ccgo/internal/contracts"
)

func TestAppendAndListSidechainTranscript(t *testing.T) {
	sessionPath := filepath.Join(t.TempDir(), "session.jsonl")
	sessionID := contracts.ID("sess_1")
	if err := AppendSidechainMessage(sessionPath, sessionID, "agent/one", TranscriptMessage{
		Type: "assistant",
		UUID: "msg_1",
	}); err != nil {
		t.Fatal(err)
	}
	infos, err := ListSidechainTranscripts(sessionPath, sessionID)
	if err != nil {
		t.Fatal(err)
	}
	if len(infos) != 1 || infos[0].ID != "agent_one" {
		t.Fatalf("infos = %#v", infos)
	}
	if filepath.Base(infos[0].Path) != "agent-agent_one.jsonl" || filepath.Base(filepath.Dir(infos[0].Path)) != "subagents" {
		t.Fatalf("sidechain path = %s", infos[0].Path)
	}
	transcript, err := LoadTranscript(infos[0].Path)
	if err != nil {
		t.Fatal(err)
	}
	msg := transcript.Messages["msg_1"]
	if msg == nil || !msg.IsSidechain || msg.AgentID != "agent_one" || msg.SessionID != sessionID {
		t.Fatalf("message = %#v", msg)
	}
}

func TestSidechainRuntimeStartAppendFinish(t *testing.T) {
	sessionPath := filepath.Join(t.TempDir(), "session.jsonl")
	sessionID := contracts.ID("sess_1")
	parent := contracts.ID("parent_1")
	runtime := SidechainRuntime{SessionPath: sessionPath, SessionID: sessionID}
	startedAt := time.Unix(100, 0).UTC()
	run, err := runtime.Start(SidechainOptions{ID: "agent/one", ParentUUID: &parent, StartedAt: startedAt})
	if err != nil {
		t.Fatal(err)
	}
	if run.ID != "agent_one" || run.Status != SidechainStatusRunning || run.Path == "" || run.StartedAt != startedAt.Format(time.RFC3339Nano) {
		t.Fatalf("run = %#v", run)
	}
	if err := runtime.Append(run, TranscriptMessage{Type: "assistant", UUID: "agent_msg"}); err != nil {
		t.Fatal(err)
	}
	states, err := ListSidechainStates(sessionPath, sessionID)
	if err != nil {
		t.Fatal(err)
	}
	if len(states) != 1 || states[0].ID != "agent_one" || states[0].Status != SidechainStatusRunning || states[0].MessageCount != 2 || states[0].LastUUID != "agent_msg" {
		t.Fatalf("running state = %#v", states)
	}
	resumed, ok := ResumeSidechainRunFromState(states[0])
	if !ok || resumed.ID != run.ID || resumed.Path != run.Path || resumed.ParentUUID == nil || *resumed.ParentUUID != parent {
		t.Fatalf("resumed from state = %#v ok=%v", resumed, ok)
	}
	resumed, ok, err = ResumeSidechainRun(sessionPath, sessionID, "agent/one")
	if err != nil {
		t.Fatal(err)
	}
	if !ok || resumed.ID != run.ID || resumed.Status != SidechainStatusRunning {
		t.Fatalf("resumed = %#v ok=%v", resumed, ok)
	}
	summary, err := runtime.Finish(run, SidechainStatusCompleted, "agent finished", time.Unix(110, 0).UTC())
	if err != nil {
		t.Fatal(err)
	}
	if summary.Subtype != "sidechain_summary" || !summary.IsSidechain {
		t.Fatalf("summary = %#v", summary)
	}
	sidechainTranscript, err := LoadTranscript(run.Path)
	if err != nil {
		t.Fatal(err)
	}
	if len(sidechainTranscript.Order) != 3 {
		t.Fatalf("sidechain transcript = %#v", sidechainTranscript.Order)
	}
	startUUID := sidechainTranscript.Order[0]
	if sidechainTranscript.Messages["agent_msg"].ParentUUID == nil || *sidechainTranscript.Messages["agent_msg"].ParentUUID != startUUID {
		t.Fatalf("agent message parent = %#v start=%s", sidechainTranscript.Messages["agent_msg"].ParentUUID, startUUID)
	}
	if sidechainTranscript.Messages[summary.UUID].ParentUUID == nil || *sidechainTranscript.Messages[summary.UUID].ParentUUID != "agent_msg" {
		t.Fatalf("summary parent = %#v", sidechainTranscript.Messages[summary.UUID].ParentUUID)
	}
	mainTranscript, err := LoadTranscript(sessionPath)
	if err != nil {
		t.Fatal(err)
	}
	if len(mainTranscript.Order) != 1 || mainTranscript.Messages[summary.UUID] == nil {
		t.Fatalf("main transcript = %#v", mainTranscript.Order)
	}
	states, err = ListSidechainStates(sessionPath, sessionID)
	if err != nil {
		t.Fatal(err)
	}
	if len(states) != 1 || states[0].Status != SidechainStatusCompleted || states[0].Summary != "agent finished" || states[0].MessageCount != 3 {
		t.Fatalf("finished state = %#v", states)
	}
	if resumed, ok := ResumeSidechainRunFromState(states[0]); ok {
		t.Fatalf("completed sidechain should not resume: %#v", resumed)
	}
	if resumed, ok, err := ResumeSidechainRun(sessionPath, sessionID, "agent/one"); err != nil || ok {
		t.Fatalf("completed resume = %#v ok=%v err=%v", resumed, ok, err)
	}
}

func TestSidechainRuntimeStartPersistsMetadataInLifecyclePayload(t *testing.T) {
	sessionPath := filepath.Join(t.TempDir(), "session.jsonl")
	sessionID := contracts.ID("sess_1")
	runtime := SidechainRuntime{SessionPath: sessionPath, SessionID: sessionID}
	run, err := runtime.Start(SidechainOptions{
		ID:                  "agent/meta",
		StartedAt:           time.Unix(100, 0).UTC(),
		AgentType:           "researcher",
		WorktreePath:        "/tmp/research-worktree",
		Description:         "research the migration",
		AgentPath:           "/tmp/agents/researcher.md",
		AgentPrompt:         "Research carefully.",
		AgentModel:          "opus",
		AgentPermissionMode: string(contracts.PermissionBypassPermissions),
		AgentAllowedTools:   []string{"Read", "Edit"},
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := os.Remove(run.MetadataPath); err != nil {
		t.Fatal(err)
	}

	state, err := FindSidechainState(sessionPath, sessionID, "agent/meta")
	if err != nil {
		t.Fatal(err)
	}
	if state.Status != SidechainStatusRunning {
		t.Fatalf("state = %#v", state)
	}
	if state.Metadata.AgentType != "researcher" || state.Metadata.WorktreePath != "/tmp/research-worktree" || state.Metadata.Description != "research the migration" || state.Metadata.AgentPath != "/tmp/agents/researcher.md" || state.Metadata.AgentPrompt != "Research carefully." || state.Metadata.AgentModel != "opus" || state.Metadata.AgentPermissionMode != string(contracts.PermissionBypassPermissions) || len(state.Metadata.AgentAllowedTools) != 2 || state.Metadata.AgentAllowedTools[0] != "Read" || state.Metadata.AgentAllowedTools[1] != "Edit" {
		t.Fatalf("metadata recovered from lifecycle payload = %#v", state.Metadata)
	}
	resumed, ok := ResumeSidechainRunFromState(state)
	if !ok || resumed.Metadata.AgentType != "researcher" || resumed.Metadata.WorktreePath != "/tmp/research-worktree" || resumed.Metadata.Description != "research the migration" || resumed.Metadata.AgentPath != "/tmp/agents/researcher.md" || resumed.Metadata.AgentPrompt != "Research carefully." || resumed.Metadata.AgentModel != "opus" || resumed.Metadata.AgentPermissionMode != string(contracts.PermissionBypassPermissions) || len(resumed.Metadata.AgentAllowedTools) != 2 {
		t.Fatalf("resumed = %#v ok=%v", resumed, ok)
	}
}

func TestLoadSidechainStateMarksOrphanTranscriptUnknown(t *testing.T) {
	sessionPath := filepath.Join(t.TempDir(), "session.jsonl")
	sessionID := contracts.ID("sess_1")
	if err := AppendSidechainMessage(sessionPath, sessionID, "orphan", TranscriptMessage{
		Type: "assistant",
		UUID: "msg_1",
	}); err != nil {
		t.Fatal(err)
	}
	states, err := ListSidechainStates(sessionPath, sessionID)
	if err != nil {
		t.Fatal(err)
	}
	if len(states) != 1 || states[0].Status != SidechainStatusUnknown || states[0].MessageCount != 1 {
		t.Fatalf("states = %#v", states)
	}
	if run, ok := ResumeSidechainRunFromState(states[0]); ok {
		t.Fatalf("unknown sidechain should not resume: %#v", run)
	}
}

func TestLoadSidechainStateAcceptsContentFieldAliases(t *testing.T) {
	sessionPath := filepath.Join(t.TempDir(), "session.jsonl")
	sessionID := contracts.ID("sess_1")
	if err := AppendSidechainMessage(sessionPath, sessionID, "legacy", TranscriptMessage{
		Type:      "system",
		UUID:      "start_1",
		Timestamp: "2026-01-01T00:00:01Z",
		Subtype:   "sidechain_start",
		Content: map[string]any{
			"sidechain_id": "agent_snake",
			"agent_type":   "reviewer",
			"state":        SidechainStatusRunning,
		},
	}); err != nil {
		t.Fatal(err)
	}
	if err := AppendSidechainMessage(sessionPath, sessionID, "legacy", TranscriptMessage{
		Type:      "system",
		UUID:      "summary_1",
		Timestamp: "2026-01-01T00:00:02Z",
		Subtype:   "sidechain_summary",
		Content: map[string]any{
			"state":        SidechainStatusFailed,
			"summary_text": "tool failed",
		},
	}); err != nil {
		t.Fatal(err)
	}

	states, err := ListSidechainStates(sessionPath, sessionID)
	if err != nil {
		t.Fatal(err)
	}
	if len(states) != 1 {
		t.Fatalf("states = %#v", states)
	}
	state := states[0]
	if state.ID != "agent_snake" || state.Status != SidechainStatusFailed || state.Summary != "tool failed" || state.Metadata.AgentType != "reviewer" {
		t.Fatalf("state = %#v", state)
	}
}

func TestLoadSidechainStateAcceptsSubtypeAndStatusAliases(t *testing.T) {
	sessionPath := filepath.Join(t.TempDir(), "session.jsonl")
	sessionID := contracts.ID("sess_1")
	if err := AppendSidechainMessage(sessionPath, sessionID, "legacy", TranscriptMessage{
		Type:      "system",
		UUID:      "start_1",
		Timestamp: "2026-01-01T00:00:01Z",
		Subtype:   "subagent_start",
		Content: map[string]any{
			"subagent_id":    "subagent_42",
			"subagentType":   "explorer",
			"lifecycleState": "inProgress",
		},
	}); err != nil {
		t.Fatal(err)
	}
	if err := AppendSidechainMessage(sessionPath, sessionID, "legacy", TranscriptMessage{
		Type:      "system",
		UUID:      "summary_1",
		Timestamp: "2026-01-01T00:00:02Z",
		Subtype:   "agent_finish",
		Content: map[string]any{
			"agentID":      "subagent_42",
			"outcome":      "completedSuccessfully",
			"finalSummary": "found the issue",
		},
	}); err != nil {
		t.Fatal(err)
	}

	states, err := ListSidechainStates(sessionPath, sessionID)
	if err != nil {
		t.Fatal(err)
	}
	if len(states) != 1 {
		t.Fatalf("states = %#v", states)
	}
	state := states[0]
	if state.ID != "subagent_42" || state.Status != SidechainStatusCompleted || state.Summary != "found the issue" || state.Metadata.AgentType != "explorer" {
		t.Fatalf("state = %#v", state)
	}
}

func TestLoadSidechainStateAcceptsLifecycleTimeAliases(t *testing.T) {
	sessionPath := filepath.Join(t.TempDir(), "session.jsonl")
	sessionID := contracts.ID("sess_1")
	startedAt := "2026-01-01T00:00:10Z"
	endedAt := "2026-01-01T00:01:30Z"
	if err := AppendSidechainMessage(sessionPath, sessionID, "timed", TranscriptMessage{
		Type:      "system",
		UUID:      "start_1",
		Timestamp: "2026-01-01T00:00:01Z",
		Subtype:   "subagent_started",
		Content: map[string]any{
			"payload": map[string]any{
				"subagentId": "timed_worker",
				"startedAt":  startedAt,
				"status":     "running",
			},
		},
	}); err != nil {
		t.Fatal(err)
	}
	if err := AppendSidechainMessage(sessionPath, sessionID, "timed", TranscriptMessage{
		Type:      "system",
		UUID:      "summary_1",
		Timestamp: "2026-01-01T00:00:02Z",
		Subtype:   "task_done",
		Content: map[string]any{
			"result": map[string]any{
				"agentID":     "timed_worker",
				"completedAt": endedAt,
				"final":       "timed work done",
			},
		},
	}); err != nil {
		t.Fatal(err)
	}

	state, err := FindSidechainState(sessionPath, sessionID, "timed")
	if err != nil {
		t.Fatal(err)
	}
	if state.ID != "timed_worker" || state.Status != SidechainStatusCompleted || state.Summary != "timed work done" {
		t.Fatalf("state = %#v", state)
	}
	if state.StartedAt != startedAt || state.EndedAt != endedAt {
		t.Fatalf("times = started %q ended %q", state.StartedAt, state.EndedAt)
	}
}

func TestNormalizeSidechainStatusAcceptsCompactAliases(t *testing.T) {
	cases := map[string]string{
		"inProgress":            SidechainStatusRunning,
		"in-progress":           SidechainStatusRunning,
		"completedSuccessfully": SidechainStatusCompleted,
		"successful":            SidechainStatusCompleted,
		"cancelledByUser":       SidechainStatusCancelled,
		"canceledByUser":        SidechainStatusCancelled,
		"failedError":           SidechainStatusFailed,
		"failed_with_error":     SidechainStatusFailed,
		"timedOut":              SidechainStatusFailed,
		"custom_state":          "custom_state",
	}
	for input, want := range cases {
		if got := normalizeSidechainStatus(input); got != want {
			t.Fatalf("normalizeSidechainStatus(%q) = %q, want %q", input, got, want)
		}
	}
}

func TestLoadSidechainStateAcceptsFailureAndCancelSummaryAliases(t *testing.T) {
	sessionPath := filepath.Join(t.TempDir(), "session.jsonl")
	sessionID := contracts.ID("sess_1")
	if err := AppendSidechainMessage(sessionPath, sessionID, "failed-alias", TranscriptMessage{
		Type:      "system",
		UUID:      "failed_start",
		Timestamp: "2026-01-01T00:00:01Z",
		Subtype:   "subagent_start",
		Content: map[string]any{
			"subagentId": "failed_alias",
			"status":     "running",
		},
	}); err != nil {
		t.Fatal(err)
	}
	if err := AppendSidechainMessage(sessionPath, sessionID, "failed-alias", TranscriptMessage{
		Type:      "system",
		UUID:      "failed_summary",
		Timestamp: "2026-01-01T00:00:02Z",
		Subtype:   "agent_failed",
		Content: map[string]any{
			"agentId":      "failed_alias",
			"errorMessage": "model call failed",
		},
	}); err != nil {
		t.Fatal(err)
	}
	if err := AppendSidechainMessage(sessionPath, sessionID, "cancelled-alias", TranscriptMessage{
		Type:      "system",
		UUID:      "cancel_start",
		Timestamp: "2026-01-01T00:00:03Z",
		Subtype:   "sidechain_start",
		Content: map[string]any{
			"sidechainId": "cancelled_alias",
			"status":      "running",
		},
	}); err != nil {
		t.Fatal(err)
	}
	if err := AppendSidechainMessage(sessionPath, sessionID, "cancelled-alias", TranscriptMessage{
		Type:      "system",
		UUID:      "cancel_summary",
		Timestamp: "2026-01-01T00:00:04Z",
		Subtype:   "sidechain_cancelled",
		Content: map[string]any{
			"sidechainId":  "cancelled_alias",
			"cancelReason": "user stopped the run",
		},
	}); err != nil {
		t.Fatal(err)
	}

	states, err := ListSidechainStates(sessionPath, sessionID)
	if err != nil {
		t.Fatal(err)
	}
	byID := map[string]SidechainState{}
	for _, state := range states {
		byID[state.ID] = state
	}
	failed := byID["failed_alias"]
	if failed.Status != SidechainStatusFailed || failed.Summary != "model call failed" {
		t.Fatalf("failed state = %#v", failed)
	}
	cancelled := byID["cancelled_alias"]
	if cancelled.Status != SidechainStatusCancelled || cancelled.Summary != "user stopped the run" {
		t.Fatalf("cancelled state = %#v", cancelled)
	}
}

func TestLoadSidechainStateAcceptsAdjacentLifecycleSubtypeAliases(t *testing.T) {
	sessionPath := filepath.Join(t.TempDir(), "session.jsonl")
	sessionID := contracts.ID("sess_1")
	if err := AppendSidechainMessage(sessionPath, sessionID, "worker-failed", TranscriptMessage{
		Type:      "system",
		UUID:      "failed_start",
		Timestamp: "2026-01-01T00:00:01Z",
		Subtype:   "subagent_started",
		Content: map[string]any{
			"taskID":    "worker_9",
			"agentName": "researcher",
			"workspace": "/tmp/research-worktree",
			"task":      "trace lifecycle",
		},
	}); err != nil {
		t.Fatal(err)
	}
	if err := AppendSidechainMessage(sessionPath, sessionID, "worker-failed", TranscriptMessage{
		Type:      "system",
		UUID:      "failed_summary",
		Timestamp: "2026-01-01T00:00:02Z",
		Subtype:   "task_failed",
		Content: map[string]any{
			"workerId":   "worker_9",
			"resultText": "agent crashed",
		},
	}); err != nil {
		t.Fatal(err)
	}
	if err := AppendSidechainMessage(sessionPath, sessionID, "worker-completed", TranscriptMessage{
		Type:      "system",
		UUID:      "completed_start",
		Timestamp: "2026-01-01T00:00:03Z",
		Subtype:   "agentStarted",
		Content: map[string]any{
			"runID": "worker_10",
			"kind":  "reviewer",
			"cwd":   "/tmp/review-worktree",
			"input": "review the diff",
		},
	}); err != nil {
		t.Fatal(err)
	}
	if err := AppendSidechainMessage(sessionPath, sessionID, "worker-completed", TranscriptMessage{
		Type:      "system",
		UUID:      "completed_summary",
		Timestamp: "2026-01-01T00:00:04Z",
		Subtype:   "sidechainCompleted",
		Content: map[string]any{
			"runId":        "worker_10",
			"finalMessage": "review complete",
		},
	}); err != nil {
		t.Fatal(err)
	}

	states, err := ListSidechainStates(sessionPath, sessionID)
	if err != nil {
		t.Fatal(err)
	}
	if len(states) != 2 {
		t.Fatalf("states = %#v", states)
	}
	byID := map[string]SidechainState{}
	for _, state := range states {
		byID[state.ID] = state
	}
	failed := byID["worker_9"]
	completed := byID["worker_10"]
	if failed.ID != "worker_9" || failed.Status != SidechainStatusFailed || failed.Summary != "agent crashed" {
		t.Fatalf("failed state = %#v", failed)
	}
	if failed.Metadata.AgentType != "researcher" || failed.Metadata.WorktreePath != "/tmp/research-worktree" || failed.Metadata.Description != "trace lifecycle" {
		t.Fatalf("failed metadata = %#v", failed.Metadata)
	}
	if completed.ID != "worker_10" || completed.Status != SidechainStatusCompleted || completed.Summary != "review complete" {
		t.Fatalf("completed state = %#v", completed)
	}
	if completed.Metadata.AgentType != "reviewer" || completed.Metadata.WorktreePath != "/tmp/review-worktree" || completed.Metadata.Description != "review the diff" {
		t.Fatalf("completed metadata = %#v", completed.Metadata)
	}
}

func TestLoadSidechainStateAcceptsTimestampAliases(t *testing.T) {
	sessionPath := filepath.Join(t.TempDir(), "session.jsonl")
	sessionID := contracts.ID("sess_1")
	if err := AppendSidechainMessage(sessionPath, sessionID, "timestamp-aliases", TranscriptMessage{
		Type:      "system",
		UUID:      "start_1",
		Timestamp: "2026-01-01T00:00:01Z",
		Subtype:   "subagent_started",
		Content: map[string]any{
			"subagentId":       "timestamp_worker",
			"startTimestampMs": int64(1710000000123),
			"status":           "running",
		},
	}); err != nil {
		t.Fatal(err)
	}
	if err := AppendSidechainMessage(sessionPath, sessionID, "timestamp-aliases", TranscriptMessage{
		Type:      "system",
		UUID:      "summary_1",
		Timestamp: "2026-01-01T00:00:02Z",
		Subtype:   "agent_finish",
		Content: map[string]any{
			"agentID":              "timestamp_worker",
			"completedTimestamp":   1710000100,
			"completedTimestampMs": int64(1710000100456),
			"resultText":           "timestamp aliases complete",
		},
	}); err != nil {
		t.Fatal(err)
	}

	state, err := FindSidechainState(sessionPath, sessionID, "timestamp-aliases")
	if err != nil {
		t.Fatal(err)
	}
	if state.ID != "timestamp_worker" || state.Status != SidechainStatusCompleted || state.Summary != "timestamp aliases complete" {
		t.Fatalf("state = %#v", state)
	}
	if state.StartedAt != "1710000000123" {
		t.Fatalf("started at = %q", state.StartedAt)
	}
	if state.EndedAt != "1710000100" {
		t.Fatalf("ended at = %q", state.EndedAt)
	}
}

func TestLoadSidechainStateAcceptsWrappedLifecycleContent(t *testing.T) {
	sessionPath := filepath.Join(t.TempDir(), "session.jsonl")
	sessionID := contracts.ID("sess_1")
	if err := AppendSidechainMessage(sessionPath, sessionID, "wrapped", TranscriptMessage{
		Type:      "system",
		UUID:      "start_1",
		Timestamp: "2026-01-01T00:00:01Z",
		Subtype:   "agent_start",
		Content: map[string]any{
			"payload": map[string]any{
				"subagentID":      "subagent_nested",
				"agentKind":       "planner",
				"workspacePath":   "/tmp/planner-worktree",
				"lifecycle":       "in_progress",
				"taskDescription": "plan the rollout",
			},
		},
	}); err != nil {
		t.Fatal(err)
	}
	if err := AppendSidechainMessage(sessionPath, sessionID, "wrapped", TranscriptMessage{
		Type:      "system",
		UUID:      "summary_1",
		Timestamp: "2026-01-01T00:00:02Z",
		Subtype:   "task_summary",
		Content: map[string]any{
			"result": map[string]any{
				"outcome":       "failed_error",
				"resultSummary": "nested task failed",
			},
		},
	}); err != nil {
		t.Fatal(err)
	}

	states, err := ListSidechainStates(sessionPath, sessionID)
	if err != nil {
		t.Fatal(err)
	}
	if len(states) != 1 {
		t.Fatalf("states = %#v", states)
	}
	state := states[0]
	if state.ID != "subagent_nested" || state.Status != SidechainStatusFailed || state.Summary != "nested task failed" || state.Metadata.AgentType != "planner" {
		t.Fatalf("state = %#v", state)
	}
	if state.Metadata.WorktreePath != "/tmp/planner-worktree" || state.Metadata.Description != "plan the rollout" {
		t.Fatalf("metadata = %#v", state.Metadata)
	}
}

func TestLoadSidechainStateAcceptsVisibleTextLifecyclePayloads(t *testing.T) {
	sessionPath := filepath.Join(t.TempDir(), "session.jsonl")
	sessionID := contracts.ID("sess_1")
	if err := AppendSidechainMessage(sessionPath, sessionID, "text-payload", TranscriptMessage{
		Type:      "system",
		UUID:      "start_1",
		Timestamp: "2026-01-01T00:00:01Z",
		Subtype:   "subagent_started",
		Content: map[string]any{
			"payload": map[string]any{
				"subagentId":    "text_worker",
				"agentName":     "writer",
				"workspacePath": "/tmp/text-worktree",
				"taskDescription": map[string]any{
					"type": "text",
					"text": "write visible payload support",
				},
				"status": "running",
			},
		},
	}); err != nil {
		t.Fatal(err)
	}
	if err := AppendSidechainMessage(sessionPath, sessionID, "text-payload", TranscriptMessage{
		Type:      "system",
		UUID:      "summary_1",
		Timestamp: "2026-01-01T00:00:02Z",
		Subtype:   "agent_finish",
		Content: map[string]any{
			"response": map[string]any{
				"agentID": "text_worker",
				"outcome": "completedSuccessfully",
				"message": map[string]any{
					"role": "assistant",
					"content": []any{
						map[string]any{"type": "text", "text": "visible provider summary"},
						map[string]any{"type": "thinking", "text": "hidden chain of thought"},
					},
				},
			},
		},
	}); err != nil {
		t.Fatal(err)
	}

	state, err := FindSidechainState(sessionPath, sessionID, "text-payload")
	if err != nil {
		t.Fatal(err)
	}
	if state.ID != "text_worker" || state.Status != SidechainStatusCompleted || state.Summary != "visible provider summary" {
		t.Fatalf("state = %#v", state)
	}
	if state.Metadata.AgentType != "writer" || state.Metadata.WorktreePath != "/tmp/text-worktree" || state.Metadata.Description != "write visible payload support" {
		t.Fatalf("metadata = %#v", state.Metadata)
	}
}

func TestLoadSidechainStateAcceptsResourceLifecycleAttributes(t *testing.T) {
	sessionPath := filepath.Join(t.TempDir(), "session.jsonl")
	sessionID := contracts.ID("sess_1")
	if err := AppendSidechainMessage(sessionPath, sessionID, "resource-fallback", TranscriptMessage{
		Type:      "system",
		UUID:      "start_1",
		Timestamp: "2026-01-01T00:00:01Z",
		Subtype:   "sidechain_start",
		Content: map[string]any{
			"id":   "resource_agent",
			"type": "sidechain-lifecycle",
			"attributes": map[string]any{
				"agentName":       "architect",
				"workspacePath":   "/tmp/architect-worktree",
				"taskDescription": "design the refactor",
				"lifecycleState":  "active",
			},
		},
	}); err != nil {
		t.Fatal(err)
	}
	if err := AppendSidechainMessage(sessionPath, sessionID, "resource-fallback", TranscriptMessage{
		Type:      "system",
		UUID:      "summary_1",
		Timestamp: "2026-01-01T00:00:02Z",
		Subtype:   "sidechainCompleted",
		Content: map[string]any{
			"properties": map[string]any{
				"outcome":      "success",
				"finalMessage": "resource lifecycle complete",
			},
		},
	}); err != nil {
		t.Fatal(err)
	}

	states, err := ListSidechainStates(sessionPath, sessionID)
	if err != nil {
		t.Fatal(err)
	}
	if len(states) != 1 {
		t.Fatalf("states = %#v", states)
	}
	state := states[0]
	if state.ID != "resource_agent" || state.Status != SidechainStatusCompleted || state.Summary != "resource lifecycle complete" {
		t.Fatalf("state = %#v", state)
	}
	if state.Metadata.AgentType != "architect" || state.Metadata.WorktreePath != "/tmp/architect-worktree" || state.Metadata.Description != "design the refactor" {
		t.Fatalf("metadata = %#v", state.Metadata)
	}
}

func TestLoadSidechainStateAcceptsGraphQLResourceLifecycleWrappers(t *testing.T) {
	sessionPath := filepath.Join(t.TempDir(), "session.jsonl")
	sessionID := contracts.ID("sess_1")
	if err := AppendSidechainMessage(sessionPath, sessionID, "graphql-resource", TranscriptMessage{
		Type:      "system",
		UUID:      "start_1",
		Timestamp: "2026-01-01T00:00:01Z",
		Subtype:   "subagent_started",
		Content: map[string]any{
			"edge": map[string]any{
				"node": map[string]any{
					"attrs": map[string]any{
						"subagentId":      "graph_worker",
						"subagentType":    "scanner",
						"workspacePath":   "/tmp/graph-worktree",
						"taskDescription": "scan wrappers",
						"lifecycleState":  "inProgress",
					},
				},
			},
		},
	}); err != nil {
		t.Fatal(err)
	}
	if err := AppendSidechainMessage(sessionPath, sessionID, "graphql-resource", TranscriptMessage{
		Type:      "system",
		UUID:      "summary_1",
		Timestamp: "2026-01-01T00:00:02Z",
		Subtype:   "agent_finish",
		Content: map[string]any{
			"edge": map[string]any{
				"node": map[string]any{
					"properties": map[string]any{
						"agentID":      "graph_worker",
						"outcome":      "completedSuccessfully",
						"finalSummary": "graph done",
					},
				},
			},
		},
	}); err != nil {
		t.Fatal(err)
	}

	states, err := ListSidechainStates(sessionPath, sessionID)
	if err != nil {
		t.Fatal(err)
	}
	if len(states) != 1 {
		t.Fatalf("states = %#v", states)
	}
	state := states[0]
	if state.ID != "graph_worker" || state.Status != SidechainStatusCompleted || state.Summary != "graph done" {
		t.Fatalf("state = %#v", state)
	}
	if state.Metadata.AgentType != "scanner" || state.Metadata.WorktreePath != "/tmp/graph-worktree" || state.Metadata.Description != "scan wrappers" {
		t.Fatalf("metadata = %#v", state.Metadata)
	}
}

func TestLoadSidechainStateAcceptsLifecycleCollectionWrappers(t *testing.T) {
	sessionPath := filepath.Join(t.TempDir(), "session.jsonl")
	sessionID := contracts.ID("sess_1")
	if err := AppendSidechainMessage(sessionPath, sessionID, "collection-resource", TranscriptMessage{
		Type:      "system",
		UUID:      "start_1",
		Timestamp: "2026-01-01T00:00:01Z",
		Subtype:   "agent_start",
		Content: map[string]any{
			"edges": []any{
				map[string]any{
					"node": map[string]any{
						"attrs": map[string]any{
							"subagentId":      "collection_worker",
							"agentKind":       "collector",
							"workspacePath":   "/tmp/collection-worktree",
							"taskDescription": "collect wrapped lifecycle",
							"lifecycleState":  "inProgress",
						},
					},
				},
			},
		},
	}); err != nil {
		t.Fatal(err)
	}
	if err := AppendSidechainMessage(sessionPath, sessionID, "collection-resource", TranscriptMessage{
		Type:      "system",
		UUID:      "summary_1",
		Timestamp: "2026-01-01T00:00:02Z",
		Subtype:   "sidechainCompleted",
		Content: map[string]any{
			"included": []any{
				map[string]any{
					"resource": map[string]any{
						"attributes": map[string]any{
							"agentID":      "collection_worker",
							"outcome":      "completedSuccessfully",
							"finalMessage": "collection lifecycle complete",
						},
					},
				},
			},
		},
	}); err != nil {
		t.Fatal(err)
	}

	states, err := ListSidechainStates(sessionPath, sessionID)
	if err != nil {
		t.Fatal(err)
	}
	if len(states) != 1 {
		t.Fatalf("states = %#v", states)
	}
	state := states[0]
	if state.ID != "collection_worker" || state.Status != SidechainStatusCompleted || state.Summary != "collection lifecycle complete" {
		t.Fatalf("state = %#v", state)
	}
	if state.Metadata.AgentType != "collector" || state.Metadata.WorktreePath != "/tmp/collection-worktree" || state.Metadata.Description != "collect wrapped lifecycle" {
		t.Fatalf("metadata = %#v", state.Metadata)
	}
}

func TestLoadSidechainStateAcceptsNumericLifecycleIDs(t *testing.T) {
	sessionPath := filepath.Join(t.TempDir(), "session.jsonl")
	sessionID := contracts.ID("sess_1")
	if err := AppendSidechainMessage(sessionPath, sessionID, "numeric-fallback", TranscriptMessage{
		Type:      "system",
		UUID:      "start_1",
		Timestamp: "2026-01-01T00:00:01Z",
		Subtype:   "subagent_start",
		Content: map[string]any{
			"payload": map[string]any{
				"subagentID":    42,
				"subagentType":  "coder",
				"workspacePath": "/tmp/coder-worktree",
				"status":        "started",
			},
		},
	}); err != nil {
		t.Fatal(err)
	}

	states, err := ListSidechainStates(sessionPath, sessionID)
	if err != nil {
		t.Fatal(err)
	}
	if len(states) != 1 {
		t.Fatalf("states = %#v", states)
	}
	state := states[0]
	if state.ID != "42" || state.Status != SidechainStatusRunning || state.Metadata.AgentType != "coder" || state.Metadata.WorktreePath != "/tmp/coder-worktree" {
		t.Fatalf("state = %#v", state)
	}
	run, ok, err := ResumeSidechainRun(sessionPath, sessionID, "42")
	if err != nil {
		t.Fatal(err)
	}
	if !ok || run.ID != "42" || run.Path != state.Path || run.Status != SidechainStatusRunning {
		t.Fatalf("run = %#v ok=%v state=%#v", run, ok, state)
	}
}

func TestLoadSidechainStateAcceptsRuntimeLifecycleFieldAliases(t *testing.T) {
	sessionPath := filepath.Join(t.TempDir(), "session.jsonl")
	sessionID := contracts.ID("sess_1")
	if err := AppendSidechainMessage(sessionPath, sessionID, "runtime-fallback", TranscriptMessage{
		Type:    "system",
		UUID:    "runtime_start",
		Subtype: "task_started",
		Content: map[string]any{
			"runtime": map[string]any{
				"jobID":         "job_77",
				"workerType":    "investigator",
				"workspaceRoot": "/tmp/runtime-worktree",
				"operationName": "inspect runtime aliases",
				"startedAtMs":   1700000000000,
				"jobStatus":     "queued",
			},
		},
	}); err != nil {
		t.Fatal(err)
	}
	if err := AppendSidechainMessage(sessionPath, sessionID, "runtime-fallback", TranscriptMessage{
		Type:    "system",
		UUID:    "runtime_finish",
		Subtype: "task_succeeded",
		Content: map[string]any{
			"output": map[string]any{
				"requestID":    "job_77",
				"resultState":  "ok",
				"outputText":   "runtime aliases complete",
				"finishedAtMs": 1700000010000,
			},
		},
	}); err != nil {
		t.Fatal(err)
	}

	states, err := ListSidechainStates(sessionPath, sessionID)
	if err != nil {
		t.Fatal(err)
	}
	if len(states) != 1 {
		t.Fatalf("states = %#v", states)
	}
	state := states[0]
	if state.ID != "job_77" || state.Status != SidechainStatusCompleted || state.Summary != "runtime aliases complete" {
		t.Fatalf("runtime state = %#v", state)
	}
	if state.StartedAt != "1700000000000" || state.EndedAt != "1700000010000" {
		t.Fatalf("runtime times = started %q ended %q", state.StartedAt, state.EndedAt)
	}
	if state.Metadata.AgentType != "investigator" || state.Metadata.WorktreePath != "/tmp/runtime-worktree" || state.Metadata.Description != "inspect runtime aliases" {
		t.Fatalf("runtime metadata = %#v", state.Metadata)
	}
}

func TestLoadSidechainStateAcceptsMetadataFieldAliases(t *testing.T) {
	sessionPath := filepath.Join(t.TempDir(), "session.jsonl")
	sessionID := contracts.ID("sess_1")
	if err := AppendSidechainMessage(sessionPath, sessionID, "legacy", TranscriptMessage{
		Type: "assistant",
		UUID: "msg_1",
	}); err != nil {
		t.Fatal(err)
	}
	metadataPath := SidechainMetadataPath(sessionPath, sessionID, "legacy")
	if err := os.WriteFile(metadataPath, []byte(`{"agent_type":"reviewer","working_directory":"/tmp/agent-worktree","desc":"legacy sidechain metadata"}`), 0o644); err != nil {
		t.Fatal(err)
	}

	state, err := FindSidechainState(sessionPath, sessionID, "legacy")
	if err != nil {
		t.Fatal(err)
	}
	if state.Metadata.AgentType != "reviewer" || state.Metadata.WorktreePath != "/tmp/agent-worktree" || state.Metadata.Description != "legacy sidechain metadata" {
		t.Fatalf("metadata = %#v", state.Metadata)
	}
}

func TestLoadSidechainStateAcceptsExtendedMetadataAliases(t *testing.T) {
	sessionPath := filepath.Join(t.TempDir(), "session.jsonl")
	sessionID := contracts.ID("sess_1")
	if err := AppendSidechainMessage(sessionPath, sessionID, "subagent_7", TranscriptMessage{
		Type: "assistant",
		UUID: "msg_1",
	}); err != nil {
		t.Fatal(err)
	}
	metadataPath := SidechainMetadataPath(sessionPath, sessionID, "subagent_7")
	if err := os.WriteFile(metadataPath, []byte(`{"agentName":"planner","workspaceRoot":"/tmp/planner-worktree","commandName":"plan the migration"}`), 0o644); err != nil {
		t.Fatal(err)
	}

	state, err := FindSidechainState(sessionPath, sessionID, "subagent_7")
	if err != nil {
		t.Fatal(err)
	}
	if state.Metadata.AgentType != "planner" || state.Metadata.WorktreePath != "/tmp/planner-worktree" || state.Metadata.Description != "plan the migration" {
		t.Fatalf("metadata = %#v", state.Metadata)
	}
}

func TestLoadSidechainStateAcceptsWrappedMetadataSidecarAliases(t *testing.T) {
	sessionPath := filepath.Join(t.TempDir(), "session.jsonl")
	sessionID := contracts.ID("sess_1")
	if err := AppendSidechainMessage(sessionPath, sessionID, "wrapped_meta", TranscriptMessage{
		Type: "assistant",
		UUID: "msg_1",
	}); err != nil {
		t.Fatal(err)
	}
	metadataPath := SidechainMetadataPath(sessionPath, sessionID, "wrapped_meta")
	if err := os.WriteFile(metadataPath, []byte(`{"resource":{"id":"meta_1","type":"sidechain-metadata","attributes":{"agentName":"researcher","workspaceRoot":"/tmp/research-worktree","displayTitle":"research wrapped sidecar"}}}`), 0o644); err != nil {
		t.Fatal(err)
	}

	state, err := FindSidechainState(sessionPath, sessionID, "wrapped_meta")
	if err != nil {
		t.Fatal(err)
	}
	if state.Metadata.AgentType != "researcher" || state.Metadata.WorktreePath != "/tmp/research-worktree" || state.Metadata.Description != "research wrapped sidecar" {
		t.Fatalf("metadata = %#v", state.Metadata)
	}
}

func TestLoadSidechainStateAcceptsVisibleTextMetadataSidecarAliases(t *testing.T) {
	sessionPath := filepath.Join(t.TempDir(), "session.jsonl")
	sessionID := contracts.ID("sess_1")
	if err := AppendSidechainMessage(sessionPath, sessionID, "text_meta", TranscriptMessage{
		Type: "assistant",
		UUID: "msg_1",
	}); err != nil {
		t.Fatal(err)
	}
	metadataPath := SidechainMetadataPath(sessionPath, sessionID, "text_meta")
	if err := os.WriteFile(metadataPath, []byte(`{
		"resource": {
			"id": "meta_2",
			"type": "sidechain-metadata",
			"attributes": {
				"agentName": "summarizer",
				"workspaceRoot": "/tmp/summary-worktree",
				"description": {
					"role": "assistant",
					"content": [
						{"type": "text", "text": "summarize wrapped text metadata"},
						{"type": "thinking", "text": "hidden"}
					]
				}
			}
		}
	}`), 0o644); err != nil {
		t.Fatal(err)
	}

	state, err := FindSidechainState(sessionPath, sessionID, "text_meta")
	if err != nil {
		t.Fatal(err)
	}
	if state.Metadata.AgentType != "summarizer" || state.Metadata.WorktreePath != "/tmp/summary-worktree" || state.Metadata.Description != "summarize wrapped text metadata" {
		t.Fatalf("metadata = %#v", state.Metadata)
	}
}

func TestSidechainManagerOrchestratesRunningSidechains(t *testing.T) {
	sessionPath := filepath.Join(t.TempDir(), "session.jsonl")
	sessionID := contracts.ID("sess_1")
	parent := contracts.ID("parent_1")
	manager := NewSidechainManager(sessionPath, sessionID)
	run, err := manager.Start(SidechainOptions{
		ID:          "agent/one",
		ParentUUID:  &parent,
		StartedAt:   time.Unix(200, 0).UTC(),
		AgentPath:   "/tmp/agents/reviewer.md",
		AgentPrompt: "Review carefully.",
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := manager.Append("agent/one", TranscriptMessage{Type: "assistant", UUID: "agent_msg_1"}); err != nil {
		t.Fatal(err)
	}
	if err := manager.Append("agent/one", TranscriptMessage{Type: "assistant", UUID: "agent_msg_2"}); err != nil {
		t.Fatal(err)
	}
	states, err := manager.List()
	if err != nil {
		t.Fatal(err)
	}
	if len(states) != 1 || states[0].LastUUID != "agent_msg_2" || states[0].MessageCount != 3 {
		t.Fatalf("states = %#v", states)
	}
	running, err := manager.ResumeRunning()
	if err != nil {
		t.Fatal(err)
	}
	if len(running) != 1 || running[0].ID != run.ID {
		t.Fatalf("running = %#v", running)
	}
	summary, err := manager.Finish("agent/one", SidechainStatusCompleted, "done", time.Unix(210, 0).UTC())
	if err != nil {
		t.Fatal(err)
	}
	if summary.ParentUUID == nil || *summary.ParentUUID != "agent_msg_2" {
		t.Fatalf("summary parent = %#v", summary.ParentUUID)
	}
	sidechainTranscript, err := LoadTranscript(run.Path)
	if err != nil {
		t.Fatal(err)
	}
	if sidechainTranscript.Messages["agent_msg_1"].ParentUUID == nil || *sidechainTranscript.Messages["agent_msg_1"].ParentUUID != sidechainTranscript.Order[0] {
		t.Fatalf("first append parent = %#v order=%#v", sidechainTranscript.Messages["agent_msg_1"].ParentUUID, sidechainTranscript.Order)
	}
	if sidechainTranscript.Messages["agent_msg_2"].ParentUUID == nil || *sidechainTranscript.Messages["agent_msg_2"].ParentUUID != "agent_msg_1" {
		t.Fatalf("second append parent = %#v", sidechainTranscript.Messages["agent_msg_2"].ParentUUID)
	}
	if _, ok, err := manager.Resume("agent/one"); err != nil || ok {
		t.Fatalf("completed resume ok=%v err=%v", ok, err)
	}
	if err := manager.Append("agent/one", TranscriptMessage{Type: "assistant", UUID: "late"}); err == nil {
		t.Fatal("expected append to completed sidechain to fail")
	}
}

func TestSidechainRuntimeRejectsDuplicateRunningStartAndAllowsRestart(t *testing.T) {
	sessionPath := filepath.Join(t.TempDir(), "session.jsonl")
	sessionID := contracts.ID("sess_1")
	runtime := SidechainRuntime{SessionPath: sessionPath, SessionID: sessionID}
	first, err := runtime.Start(SidechainOptions{ID: "agent/one", StartedAt: time.Unix(100, 0).UTC()})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := runtime.Start(SidechainOptions{ID: "agent/one", StartedAt: time.Unix(105, 0).UTC()}); err == nil {
		t.Fatal("expected duplicate running sidechain start to fail")
	}
	state, err := FindSidechainState(sessionPath, sessionID, "agent/one")
	if err != nil {
		t.Fatal(err)
	}
	if state.MessageCount != 1 || state.Status != SidechainStatusRunning {
		t.Fatalf("duplicate start should not append lifecycle state = %#v", state)
	}
	if _, err := runtime.Finish(first, SidechainStatusCompleted, "first run done", time.Unix(110, 0).UTC()); err != nil {
		t.Fatal(err)
	}
	second, err := runtime.Start(SidechainOptions{ID: "agent/one", StartedAt: time.Unix(120, 0).UTC()})
	if err != nil {
		t.Fatal(err)
	}
	if second.ID != first.ID {
		t.Fatalf("restart id = %q, want %q", second.ID, first.ID)
	}
	state, err = FindSidechainState(sessionPath, sessionID, "agent/one")
	if err != nil {
		t.Fatal(err)
	}
	if state.Status != SidechainStatusRunning || state.Summary != "" || state.EndedAt != "" || state.StartedAt != time.Unix(120, 0).UTC().Format(time.RFC3339Nano) {
		t.Fatalf("restart state = %#v", state)
	}
}

func TestSidechainManagerCancelAndFailLifecycle(t *testing.T) {
	sessionPath := filepath.Join(t.TempDir(), "session.jsonl")
	sessionID := contracts.ID("sess_1")
	manager := NewSidechainManager(sessionPath, sessionID)
	if _, err := manager.Start(SidechainOptions{ID: "cancel-me", StartedAt: time.Unix(100, 0).UTC()}); err != nil {
		t.Fatal(err)
	}
	cancelled, err := manager.Cancel("cancel-me", "user stopped agent", time.Unix(110, 0).UTC())
	if err != nil {
		t.Fatal(err)
	}
	if cancelled.Subtype != "sidechain_summary" || stringField(cancelled.Content, "status") != SidechainStatusCancelled {
		t.Fatalf("cancelled summary = %#v", cancelled)
	}
	if _, ok, err := manager.Resume("cancel-me"); err != nil || ok {
		t.Fatalf("cancelled sidechain should not resume ok=%v err=%v", ok, err)
	}
	if err := manager.Append("cancel-me", TranscriptMessage{Type: "assistant", UUID: "late_cancel"}); err == nil {
		t.Fatal("expected append to cancelled sidechain to fail")
	}

	if _, err := manager.Start(SidechainOptions{ID: "fail-me", StartedAt: time.Unix(120, 0).UTC()}); err != nil {
		t.Fatal(err)
	}
	failed, err := manager.Fail("fail-me", "tool error", time.Unix(130, 0).UTC())
	if err != nil {
		t.Fatal(err)
	}
	if stringField(failed.Content, "status") != SidechainStatusFailed {
		t.Fatalf("failed summary = %#v", failed)
	}
	manifest, err := manager.Manifest()
	if err != nil {
		t.Fatal(err)
	}
	if manifest.Cancelled != 1 || manifest.Failed != 1 || manifest.Running != 0 || len(manifest.Summaries) != 2 {
		t.Fatalf("manifest = %#v", manifest)
	}
}

func TestSidechainFinishNormalizesStatusAliases(t *testing.T) {
	sessionPath := filepath.Join(t.TempDir(), "session.jsonl")
	sessionID := contracts.ID("sess_1")
	manager := NewSidechainManager(sessionPath, sessionID)
	if _, err := manager.Start(SidechainOptions{ID: "alias-success", StartedAt: time.Unix(100, 0).UTC()}); err != nil {
		t.Fatal(err)
	}
	summary, err := manager.Finish("alias-success", "completedSuccessfully", "done", time.Unix(110, 0).UTC())
	if err != nil {
		t.Fatal(err)
	}
	if stringField(summary.Content, "status") != SidechainStatusCompleted {
		t.Fatalf("summary status = %#v", summary.Content)
	}
	state, err := FindSidechainState(sessionPath, sessionID, "alias-success")
	if err != nil {
		t.Fatal(err)
	}
	if state.Status != SidechainStatusCompleted || state.Summary != "done" {
		t.Fatalf("state = %#v", state)
	}

	if _, err := manager.Start(SidechainOptions{ID: "alias-error", StartedAt: time.Unix(120, 0).UTC()}); err != nil {
		t.Fatal(err)
	}
	failed, err := manager.Finish("alias-error", "failedError", "boom", time.Unix(130, 0).UTC())
	if err != nil {
		t.Fatal(err)
	}
	if stringField(failed.Content, "status") != SidechainStatusFailed {
		t.Fatalf("failed status = %#v", failed.Content)
	}
}

func TestBuildSidechainResumeContext(t *testing.T) {
	sessionPath := filepath.Join(t.TempDir(), "session.jsonl")
	sessionID := contracts.ID("sess_1")
	parent := contracts.ID("parent_1")
	manager := NewSidechainManager(sessionPath, sessionID)
	run, err := manager.Start(SidechainOptions{
		ID:          "agent/one",
		ParentUUID:  &parent,
		StartedAt:   time.Unix(200, 0).UTC(),
		AgentPath:   "/tmp/agents/reviewer.md",
		AgentPrompt: "Review carefully.",
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := manager.Append("agent/one", TranscriptMessage{
		Type: "user",
		UUID: "agent_user",
		Message: &contracts.Message{
			Type:    contracts.MessageUser,
			UUID:    "agent_user",
			Content: []contracts.ContentBlock{contracts.NewTextBlock("inspect")},
		},
	}); err != nil {
		t.Fatal(err)
	}
	if err := manager.Append("agent/one", TranscriptMessage{
		Type: "assistant",
		UUID: "agent_msg",
		Message: &contracts.Message{
			Type:    contracts.MessageAssistant,
			UUID:    "agent_msg",
			Content: []contracts.ContentBlock{contracts.NewTextBlock("done")},
		},
	}); err != nil {
		t.Fatal(err)
	}
	context, err := manager.ResumeContext("agent/one", 2)
	if err != nil {
		t.Fatal(err)
	}
	if !context.CanResume || context.Run.ID != run.ID || context.State.LastUUID != "agent_msg" || !context.Truncated {
		t.Fatalf("context = %#v", context)
	}
	if len(context.Tail) != 2 || context.Tail[0].UUID != "agent_user" || len(context.Messages) != 3 || context.Messages[0].Subtype != "agent_prompt" || context.Messages[0].Content[0].Text != "Review carefully." || context.Messages[2].Content[0].Text != "done" {
		t.Fatalf("context tail/messages = %#v %#v", context.Tail, context.Messages)
	}
	if _, err := manager.Finish("agent/one", SidechainStatusCompleted, "finished", time.Unix(210, 0).UTC()); err != nil {
		t.Fatal(err)
	}
	context, err = BuildSidechainResumeContext(sessionPath, sessionID, "agent/one", 10)
	if err != nil {
		t.Fatal(err)
	}
	if context.CanResume || context.Summary != "finished" || context.Truncated {
		t.Fatalf("finished context = %#v", context)
	}
}

func TestSidechainSubagentLayoutMetadataAndConversation(t *testing.T) {
	sessionPath := filepath.Join(t.TempDir(), "session.jsonl")
	sessionID := contracts.ID("sess_1")
	parent := contracts.ID("parent_1")
	manager := NewSidechainManager(sessionPath, sessionID)
	run, err := manager.Start(SidechainOptions{
		ID:           "agent/one",
		Subdir:       "../workflows/run:1",
		ParentUUID:   &parent,
		StartedAt:    time.Unix(300, 0).UTC(),
		AgentType:    "reviewer",
		WorktreePath: "/tmp/ccgo-agent",
		Description:  "review the diff",
	})
	if err != nil {
		t.Fatal(err)
	}
	if filepath.Base(run.Path) != "agent-agent_one.jsonl" || filepath.Base(run.MetadataPath) != "agent-agent_one.meta.json" {
		t.Fatalf("run paths = %#v", run)
	}
	if run.Subdir != filepath.Join("workflows", "run_1") {
		t.Fatalf("subdir = %q", run.Subdir)
	}
	if err := manager.Append("agent/one", TranscriptMessage{
		Type: "user",
		UUID: "agent_user",
		Message: &contracts.Message{
			Type:    contracts.MessageUser,
			UUID:    "agent_user",
			Content: []contracts.ContentBlock{contracts.NewTextBlock("inspect")},
		},
	}); err != nil {
		t.Fatal(err)
	}
	if err := manager.Append("agent/one", TranscriptMessage{
		Type: "assistant",
		UUID: "agent_msg",
		Message: &contracts.Message{
			Type:    contracts.MessageAssistant,
			UUID:    "agent_msg",
			Content: []contracts.ContentBlock{contracts.NewTextBlock("done")},
		},
	}); err != nil {
		t.Fatal(err)
	}
	if err := AppendAgentContentReplacements(run.Path, sessionID, run.ID, []ContentReplacementRecord{{
		Kind:        "tool_result",
		ToolUseID:   "tool_1",
		BlockID:     "block_1",
		Replacement: "[content replaced]",
	}}); err != nil {
		t.Fatal(err)
	}
	states, err := manager.List()
	if err != nil {
		t.Fatal(err)
	}
	if len(states) != 1 || states[0].Metadata.AgentType != "reviewer" || states[0].Metadata.WorktreePath != "/tmp/ccgo-agent" || states[0].Subdir != filepath.Join("workflows", "run_1") {
		t.Fatalf("states = %#v", states)
	}
	context, err := manager.ResumeContext("agent/one", 10)
	if err != nil {
		t.Fatal(err)
	}
	if !context.CanResume || context.Metadata.AgentType != "reviewer" || context.Run.Subdir != filepath.Join("workflows", "run_1") {
		t.Fatalf("resume context = %#v", context)
	}
	conversation, err := manager.Conversation("agent/one")
	if err != nil {
		t.Fatal(err)
	}
	if !conversation.Found || conversation.Leaf != "agent_msg" || len(conversation.Messages) != 3 || conversation.Messages[2].Content[0].Text != "done" {
		t.Fatalf("conversation = %#v", conversation)
	}
	if len(conversation.ContentReplacements) != 1 || conversation.ContentReplacements[0].Replacement != "[content replaced]" {
		t.Fatalf("content replacements = %#v", conversation.ContentReplacements)
	}
}

func TestSidechainManifestSummarizesStates(t *testing.T) {
	sessionPath := filepath.Join(t.TempDir(), "session.jsonl")
	sessionID := contracts.ID("sess_1")
	manager := NewSidechainManager(sessionPath, sessionID)
	if _, err := manager.Start(SidechainOptions{ID: "running", StartedAt: time.Unix(300, 0).UTC()}); err != nil {
		t.Fatal(err)
	}
	if _, err := manager.Start(SidechainOptions{ID: "done", StartedAt: time.Unix(100, 0).UTC()}); err != nil {
		t.Fatal(err)
	}
	if _, err := manager.Finish("done", SidechainStatusCompleted, "finished work", time.Unix(200, 0).UTC()); err != nil {
		t.Fatal(err)
	}
	if _, err := manager.Start(SidechainOptions{ID: "failed", StartedAt: time.Unix(120, 0).UTC()}); err != nil {
		t.Fatal(err)
	}
	if _, err := manager.Finish("failed", SidechainStatusFailed, "boom", time.Unix(240, 0).UTC()); err != nil {
		t.Fatal(err)
	}
	manifest, err := manager.Manifest()
	if err != nil {
		t.Fatal(err)
	}
	if manifest.SessionID != sessionID || manifest.Total != 3 || manifest.Running != 1 || manifest.Completed != 1 || manifest.Failed != 1 {
		t.Fatalf("manifest counts = %#v", manifest)
	}
	if manifest.LatestStartedAt != time.Unix(300, 0).UTC().Format(time.RFC3339Nano) || manifest.LatestEndedAt != time.Unix(240, 0).UTC().Format(time.RFC3339Nano) {
		t.Fatalf("manifest times = %#v", manifest)
	}
	if len(manifest.Summaries) != 2 || manifest.Summaries[0].ID != "done" || manifest.Summaries[0].Summary != "finished work" || manifest.Summaries[1].Status != SidechainStatusFailed {
		t.Fatalf("manifest summaries = %#v", manifest.Summaries)
	}
}
