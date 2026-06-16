package session

import (
	"strings"

	"ccgo/internal/contracts"
)

type SidechainResumeContext struct {
	State        SidechainState
	Run          SidechainRun
	Metadata     SidechainMetadata
	CanResume    bool
	Tail         []TranscriptMessage
	Messages     []contracts.Message
	Summary      string
	Truncated    bool
	MessageLimit int
}

func BuildSidechainResumeContext(sessionPath string, sessionID contracts.ID, sidechainID string, limit int) (SidechainResumeContext, error) {
	state, err := FindSidechainState(sessionPath, sessionID, sidechainID)
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
	messages := TranscriptMessagesToContractMessages(tail)
	messages = ensureAgentPromptResumeMessage(state, tail, messages)
	run, canResume := ResumeSidechainRunFromState(state)
	return SidechainResumeContext{
		State:        state,
		Run:          run,
		Metadata:     state.Metadata,
		CanResume:    canResume,
		Tail:         append([]TranscriptMessage(nil), tail...),
		Messages:     messages,
		Summary:      state.Summary,
		Truncated:    state.MessageCount > len(tail),
		MessageLimit: limit,
	}, nil
}

func (m SidechainManager) ResumeContext(sidechainID string, limit int) (SidechainResumeContext, error) {
	return BuildSidechainResumeContext(m.Runtime.SessionPath, m.Runtime.SessionID, sidechainID, limit)
}

func ensureAgentPromptResumeMessage(state SidechainState, tail []TranscriptMessage, messages []contracts.Message) []contracts.Message {
	prompt := strings.TrimSpace(state.Metadata.AgentPrompt)
	if prompt == "" || tailContainsAgentPrompt(tail) {
		return messages
	}
	id := contracts.ID("agent_prompt_" + state.ID)
	message := contracts.Message{
		Type:      contracts.MessageSystem,
		UUID:      id,
		SessionID: state.SessionID,
		IsMeta:    true,
		Subtype:   "agent_prompt",
		Content:   []contracts.ContentBlock{contracts.NewTextBlock(prompt)},
	}
	return append([]contracts.Message{message}, messages...)
}

func tailContainsAgentPrompt(tail []TranscriptMessage) bool {
	for _, message := range tail {
		if message.Subtype == "agent_prompt" {
			return true
		}
		if message.Message != nil && message.Message.Subtype == "agent_prompt" {
			return true
		}
	}
	return false
}
