package session

import "ccgo/internal/contracts"

type SidechainResumeContext struct {
	State        SidechainState
	Run          SidechainRun
	CanResume    bool
	Tail         []TranscriptMessage
	Messages     []contracts.Message
	Summary      string
	Truncated    bool
	MessageLimit int
}

func BuildSidechainResumeContext(sessionPath string, sessionID contracts.ID, sidechainID string, limit int) (SidechainResumeContext, error) {
	id := sanitizeSidechainID(sidechainID)
	info := SidechainInfo{ID: id, Path: SidechainTranscriptPath(sessionPath, sessionID, id)}
	state, err := LoadSidechainState(info)
	if err != nil {
		return SidechainResumeContext{}, err
	}
	if state.SessionID == "" {
		state.SessionID = sessionID
	}
	if limit <= 0 {
		limit = state.MessageCount
	}
	tail, err := LoadTranscriptTail(state.Path, limit)
	if err != nil {
		return SidechainResumeContext{}, err
	}
	run, canResume := ResumeSidechainRunFromState(state)
	return SidechainResumeContext{
		State:        state,
		Run:          run,
		CanResume:    canResume,
		Tail:         append([]TranscriptMessage(nil), tail...),
		Messages:     TranscriptMessagesToContractMessages(tail),
		Summary:      state.Summary,
		Truncated:    state.MessageCount > len(tail),
		MessageLimit: limit,
	}, nil
}

func (m SidechainManager) ResumeContext(sidechainID string, limit int) (SidechainResumeContext, error) {
	return BuildSidechainResumeContext(m.Runtime.SessionPath, m.Runtime.SessionID, sidechainID, limit)
}
