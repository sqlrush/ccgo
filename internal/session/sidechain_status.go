package session

import (
	"fmt"
	"sort"

	"ccgo/internal/contracts"
)

type SidechainState struct {
	ID           string
	Path         string
	SessionID    contracts.ID
	ParentUUID   *contracts.ID
	Status       string
	Summary      string
	StartedAt    string
	EndedAt      string
	LastUUID     contracts.ID
	MessageCount int
}

func LoadSidechainState(info SidechainInfo) (SidechainState, error) {
	transcript, err := LoadTranscript(info.Path)
	if err != nil {
		return SidechainState{}, err
	}
	state := SidechainState{
		ID:     info.ID,
		Path:   info.Path,
		Status: SidechainStatusUnknown,
	}
	started := false
	finished := false
	for _, id := range transcript.Order {
		msg := transcript.Messages[id]
		if msg == nil {
			continue
		}
		state.MessageCount++
		state.LastUUID = msg.UUID
		if msg.SessionID != "" {
			state.SessionID = msg.SessionID
		}
		if state.ParentUUID == nil && msg.ParentUUID != nil {
			parent := *msg.ParentUUID
			state.ParentUUID = &parent
		}
		if msg.Subtype == "sidechain_start" {
			started = true
			if state.StartedAt == "" {
				state.StartedAt = msg.Timestamp
			}
			if sidechainID := stringField(msg.Content, "sidechainId"); sidechainID != "" {
				state.ID = sidechainID
			}
			if status := stringField(msg.Content, "status"); status != "" {
				state.Status = status
			} else {
				state.Status = SidechainStatusRunning
			}
		}
		if msg.Subtype == "sidechain_summary" {
			finished = true
			state.EndedAt = msg.Timestamp
			if status := stringField(msg.Content, "status"); status != "" {
				state.Status = status
			} else {
				state.Status = SidechainStatusCompleted
			}
			state.Summary = stringField(msg.Content, "summary")
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
	id := sanitizeSidechainID(sidechainID)
	info := SidechainInfo{ID: id, Path: SidechainTranscriptPath(sessionPath, sessionID, id)}
	state, err := LoadSidechainState(info)
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
		ID:         state.ID,
		SessionID:  state.SessionID,
		Path:       state.Path,
		ParentUUID: state.ParentUUID,
		Status:     state.Status,
		StartedAt:  state.StartedAt,
		EndedAt:    state.EndedAt,
	}, true
}

func stringField(value any, key string) string {
	switch fields := value.(type) {
	case map[string]any:
		raw, _ := fields[key].(string)
		return raw
	case map[string]string:
		return fields[key]
	default:
		return ""
	}
}
