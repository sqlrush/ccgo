package session

import "ccgo/internal/contracts"

type SidechainSummary struct {
	ID      string
	Status  string
	Summary string
	EndedAt string
}

type SidechainManifest struct {
	SessionID       contracts.ID
	Total           int
	Running         int
	Completed       int
	Failed          int
	Cancelled       int
	Unknown         int
	LatestStartedAt string
	LatestEndedAt   string
	States          []SidechainState
	Summaries       []SidechainSummary
}

func BuildSidechainManifest(sessionPath string, sessionID contracts.ID) (SidechainManifest, error) {
	states, err := ListSidechainStates(sessionPath, sessionID)
	if err != nil {
		return SidechainManifest{}, err
	}
	manifest := SidechainManifest{
		SessionID: sessionID,
		Total:     len(states),
		States:    append([]SidechainState(nil), states...),
	}
	for _, state := range states {
		if state.StartedAt > manifest.LatestStartedAt {
			manifest.LatestStartedAt = state.StartedAt
		}
		if state.EndedAt > manifest.LatestEndedAt {
			manifest.LatestEndedAt = state.EndedAt
		}
		switch state.Status {
		case SidechainStatusRunning:
			manifest.Running++
		case SidechainStatusCompleted:
			manifest.Completed++
		case SidechainStatusFailed:
			manifest.Failed++
		case SidechainStatusCancelled:
			manifest.Cancelled++
		default:
			manifest.Unknown++
		}
		if state.Summary != "" || state.Status != SidechainStatusRunning {
			manifest.Summaries = append(manifest.Summaries, SidechainSummary{
				ID:      state.ID,
				Status:  state.Status,
				Summary: state.Summary,
				EndedAt: state.EndedAt,
			})
		}
	}
	return manifest, nil
}
