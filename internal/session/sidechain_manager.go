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
	return AppendSidechainMessage(m.Runtime.SessionPath, m.Runtime.SessionID, state.ID, message)
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

func (m SidechainManager) loadState(sidechainID string) (SidechainState, error) {
	id := sanitizeSidechainID(sidechainID)
	info := SidechainInfo{ID: id, Path: SidechainTranscriptPath(m.Runtime.SessionPath, m.Runtime.SessionID, id)}
	state, err := LoadSidechainState(info)
	if err != nil {
		return SidechainState{}, err
	}
	if state.SessionID == "" {
		state.SessionID = m.Runtime.SessionID
	}
	return state, nil
}
