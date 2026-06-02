package session

import (
	"fmt"
	"sort"
	"strings"

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
		if msg.Subtype == "sidechain_start" {
			started = true
			if state.StartedAt == "" {
				state.StartedAt = msg.Timestamp
			}
			if sidechainID := firstStringField(msg.Content, "sidechainId", "sidechain_id"); sidechainID != "" {
				state.ID = sidechainID
			} else if agentID := firstStringField(msg.Content, "agentId", "agent_id"); agentID != "" {
				state.ID = agentID
			}
			if agentType := firstStringField(msg.Content, "agentType", "agent_type"); agentType != "" && state.Metadata.AgentType == "" {
				state.Metadata.AgentType = agentType
			}
			if status := firstStringField(msg.Content, "status", "state"); status != "" {
				state.Status = status
			} else {
				state.Status = SidechainStatusRunning
			}
		}
		if msg.Subtype == "sidechain_summary" {
			finished = true
			state.EndedAt = msg.Timestamp
			if status := firstStringField(msg.Content, "status", "state"); status != "" {
				state.Status = status
			} else {
				state.Status = SidechainStatusCompleted
			}
			state.Summary = firstStringField(msg.Content, "summary", "summary_text", "summaryText")
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
	switch fields := value.(type) {
	case map[string]any:
		for _, key := range keys {
			raw, _ := fields[key].(string)
			if raw != "" {
				return raw
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

func replaceJSONLExt(path string, ext string) string {
	if path == "" {
		return ""
	}
	return strings.TrimSuffix(path, ".jsonl") + ext
}
