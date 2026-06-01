package session

import "ccgo/internal/contracts"

type SidechainConversation struct {
	AgentID             string
	Path                string
	Metadata            SidechainMetadata
	Found               bool
	Leaf                contracts.ID
	Chain               []TranscriptMessage
	Messages            []contracts.Message
	ContentReplacements []ContentReplacementRecord
}

func BuildSidechainConversation(sessionPath string, sessionID contracts.ID, sidechainID string) (SidechainConversation, error) {
	state, err := FindSidechainState(sessionPath, sessionID, sidechainID)
	if err != nil {
		return SidechainConversation{}, err
	}
	transcript, err := LoadTranscript(state.Path)
	if err != nil {
		return SidechainConversation{}, err
	}
	agentID := state.ID
	if agentID == "" {
		agentID = sanitizeSidechainID(sidechainID)
	}
	messages := sidechainTranscriptMessages(transcript, agentID)
	if len(messages) == 0 {
		return SidechainConversation{AgentID: agentID, Path: state.Path, Metadata: state.Metadata}, nil
	}
	leaf := latestSidechainLeaf(messages)
	if leaf == "" {
		return SidechainConversation{AgentID: agentID, Path: state.Path, Metadata: state.Metadata}, nil
	}
	chain := transcript.BuildConversationChain(leaf)
	chain = filterSidechainChain(chain, agentID)
	return SidechainConversation{
		AgentID:             agentID,
		Path:                state.Path,
		Metadata:            state.Metadata,
		Found:               len(chain) > 0,
		Leaf:                leaf,
		Chain:               append([]TranscriptMessage(nil), chain...),
		Messages:            TranscriptMessagesToContractMessages(chain),
		ContentReplacements: append([]ContentReplacementRecord(nil), transcript.ContentReplacements[contracts.ID(agentID)]...),
	}, nil
}

func (m SidechainManager) Conversation(sidechainID string) (SidechainConversation, error) {
	return BuildSidechainConversation(m.Runtime.SessionPath, m.Runtime.SessionID, sidechainID)
}

func sidechainTranscriptMessages(transcript Transcript, agentID string) []*TranscriptMessage {
	var out []*TranscriptMessage
	for _, id := range transcript.Order {
		message := transcript.Messages[id]
		if message == nil {
			continue
		}
		if message.AgentID == agentID && message.IsSidechain {
			out = append(out, message)
		}
	}
	if len(out) > 0 {
		return out
	}
	for _, id := range transcript.Order {
		message := transcript.Messages[id]
		if message != nil && message.IsSidechain {
			out = append(out, message)
		}
	}
	return out
}

func latestSidechainLeaf(messages []*TranscriptMessage) contracts.ID {
	parents := map[contracts.ID]struct{}{}
	for _, message := range messages {
		if message.ParentUUID != nil {
			parents[*message.ParentUUID] = struct{}{}
		}
	}
	for i := len(messages) - 1; i >= 0; i-- {
		if _, ok := parents[messages[i].UUID]; !ok {
			return messages[i].UUID
		}
	}
	return ""
}

func filterSidechainChain(chain []TranscriptMessage, agentID string) []TranscriptMessage {
	if agentID == "" {
		return chain
	}
	hasAgentIDs := false
	for _, message := range chain {
		if message.AgentID != "" {
			hasAgentIDs = true
			break
		}
	}
	if !hasAgentIDs {
		return chain
	}
	out := make([]TranscriptMessage, 0, len(chain))
	for _, message := range chain {
		if message.AgentID == agentID {
			out = append(out, message)
		}
	}
	return out
}
