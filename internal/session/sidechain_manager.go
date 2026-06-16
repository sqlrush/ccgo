package session

import (
	"fmt"
	"time"

	"ccgo/internal/contracts"
)

type SidechainManager struct {
	Runtime SidechainRuntime
}

func NewSidechainManager(sessionPath string, sessionID contracts.ID) SidechainManager {
	return SidechainManager{Runtime: SidechainRuntime{SessionPath: sessionPath, SessionID: sessionID}}
}

func (m SidechainManager) Start(options SidechainOptions) (SidechainRun, error) {
	return m.Runtime.Start(options)
}

func (m SidechainManager) List() ([]SidechainState, error) {
	return ListSidechainStates(m.Runtime.SessionPath, m.Runtime.SessionID)
}

func (m SidechainManager) Manifest() (SidechainManifest, error) {
	return BuildSidechainManifest(m.Runtime.SessionPath, m.Runtime.SessionID)
}

func (m SidechainManager) TeamManifest() (TeamManifest, error) {
	return LoadTeamManifest(m.Runtime.SessionPath, m.Runtime.SessionID)
}

func (m SidechainManager) CreateTeam(options TeamOptions) (TeamState, TeamManifest, error) {
	return CreateTeam(m.Runtime.SessionPath, m.Runtime.SessionID, options)
}

func (m SidechainManager) DeleteTeam(teamID string) (TeamState, TeamManifest, error) {
	return DeleteTeam(m.Runtime.SessionPath, m.Runtime.SessionID, teamID)
}

func (m SidechainManager) ScheduleManifest() (ScheduleManifest, error) {
	return LoadScheduleManifest(m.Runtime.SessionPath, m.Runtime.SessionID)
}

func (m SidechainManager) UpsertSchedule(options ScheduleOptions) (ScheduleState, ScheduleManifest, error) {
	return UpsertSchedule(m.Runtime.SessionPath, m.Runtime.SessionID, options)
}

func (m SidechainManager) DeleteSchedule(scheduleID string) (ScheduleState, ScheduleManifest, error) {
	return DeleteSchedule(m.Runtime.SessionPath, m.Runtime.SessionID, scheduleID)
}

func (m SidechainManager) RecordScheduleRun(scheduleID string, options ScheduleRunOptions) (ScheduleState, ScheduleManifest, error) {
	return RecordScheduleRun(m.Runtime.SessionPath, m.Runtime.SessionID, scheduleID, options)
}

func (m SidechainManager) Resume(sidechainID string) (SidechainRun, bool, error) {
	return ResumeSidechainRun(m.Runtime.SessionPath, m.Runtime.SessionID, sidechainID)
}

func (m SidechainManager) ResumeRunning() ([]SidechainRun, error) {
	states, err := m.List()
	if err != nil {
		return nil, err
	}
	var runs []SidechainRun
	for _, state := range states {
		if state.SessionID == "" {
			state.SessionID = m.Runtime.SessionID
		}
		run, ok := ResumeSidechainRunFromState(state)
		if ok {
			runs = append(runs, run)
		}
	}
	return runs, nil
}

func (m SidechainManager) Append(sidechainID string, message TranscriptMessage) error {
	state, err := m.loadState(sidechainID)
	if err != nil {
		return err
	}
	if state.Status != SidechainStatusRunning {
		return fmt.Errorf("sidechain %s is not running", state.ID)
	}
	if message.ParentUUID == nil && state.LastUUID != "" {
		parent := state.LastUUID
		message.ParentUUID = &parent
	}
	return AppendSidechainMessageInSubdir(m.Runtime.SessionPath, m.Runtime.SessionID, state.ID, state.Subdir, message)
}

func (m SidechainManager) Finish(sidechainID string, status string, summary string, endedAt time.Time) (TranscriptMessage, error) {
	state, err := m.loadState(sidechainID)
	if err != nil {
		return TranscriptMessage{}, err
	}
	if state.Status != SidechainStatusRunning {
		return TranscriptMessage{}, fmt.Errorf("sidechain %s is not running", state.ID)
	}
	run, ok := ResumeSidechainRunFromState(state)
	if !ok {
		return TranscriptMessage{}, fmt.Errorf("sidechain %s cannot be resumed", state.ID)
	}
	if state.LastUUID != "" {
		parent := state.LastUUID
		run.ParentUUID = &parent
	}
	return m.Runtime.Finish(run, status, summary, endedAt)
}

func (m SidechainManager) Cancel(sidechainID string, reason string, endedAt time.Time) (TranscriptMessage, error) {
	return m.Finish(sidechainID, SidechainStatusCancelled, reason, endedAt)
}

func (m SidechainManager) Fail(sidechainID string, summary string, endedAt time.Time) (TranscriptMessage, error) {
	return m.Finish(sidechainID, SidechainStatusFailed, summary, endedAt)
}

func (m SidechainManager) MarkWorktreeCleanup(sidechainID string, status string, reason string, timestamp time.Time) (TranscriptMessage, error) {
	state, err := m.loadState(sidechainID)
	if err != nil {
		return TranscriptMessage{}, err
	}
	if timestamp.IsZero() {
		timestamp = time.Now().UTC()
	}
	parent := state.LastUUID
	message := TranscriptMessage{
		Type:        "system",
		UUID:        contracts.NewID(),
		SessionID:   m.Runtime.SessionID,
		Timestamp:   timestamp.UTC().Format(time.RFC3339Nano),
		Subtype:     "worktree_cleanup",
		IsSidechain: true,
		AgentID:     state.ID,
		Content: map[string]any{
			"sidechainId":           state.ID,
			"agentId":               state.ID,
			"worktreePath":          state.Metadata.WorktreePath,
			"worktreeOwned":         state.Metadata.WorktreeOwned,
			"worktreeCleanupStatus": status,
			"worktreeCleanupReason": reason,
			"worktreeCleanupAt":     timestamp.UTC().Format(time.RFC3339Nano),
		},
	}
	if parent != "" {
		message.ParentUUID = &parent
	}
	if err := AppendSidechainMessageInSubdir(m.Runtime.SessionPath, m.Runtime.SessionID, state.ID, state.Subdir, message); err != nil {
		return TranscriptMessage{}, err
	}
	return message, nil
}

func (m SidechainManager) loadState(sidechainID string) (SidechainState, error) {
	state, err := FindSidechainState(m.Runtime.SessionPath, m.Runtime.SessionID, sidechainID)
	if err != nil {
		return SidechainState{}, err
	}
	if state.SessionID == "" {
		state.SessionID = m.Runtime.SessionID
	}
	return state, nil
}
