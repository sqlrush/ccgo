package session

import (
	"encoding/json"

	"ccgo/internal/contracts"
)

type ResumeConversation struct {
	Leaf            contracts.ID
	Found           bool
	Messages        []contracts.Message
	Chain           []TranscriptMessage
	BytesRead       int64
	HasBefore       bool
	MissingParent   *contracts.ID
	TruncatedParent *contracts.ID
}

func BuildResumeConversation(path string, leaf contracts.ID) (ResumeConversation, error) {
	transcript, err := LoadTranscript(path)
	if err != nil {
		return ResumeConversation{}, err
	}
	if leaf == "" {
		leaf = latestConversationLeaf(transcript)
	}
	if leaf == "" {
		return ResumeConversation{}, nil
	}
	chain := transcript.BuildConversationChain(leaf)
	if len(chain) == 0 {
		return ResumeConversation{Leaf: leaf}, nil
	}
	return ResumeConversation{
		Leaf:     leaf,
		Found:    true,
		Messages: TranscriptMessagesToContractMessages(chain),
		Chain:    append([]TranscriptMessage(nil), chain...),
	}, nil
}

func BuildIndexedResumeConversation(path string, leaf contracts.ID, maxBytes int64) (ResumeConversation, error) {
	index, err := BuildTranscriptLineIndex(path)
	if err != nil {
		return ResumeConversation{}, err
	}
	chain, err := LoadTranscriptIndexedChain(path, index, leaf, maxBytes)
	if err != nil {
		return ResumeConversation{}, err
	}
	if !chain.Found {
		return ResumeConversation{Leaf: chain.Leaf}, nil
	}
	return ResumeConversation{
		Leaf:            chain.Leaf,
		Found:           true,
		Messages:        TranscriptMessagesToContractMessages(chain.Messages),
		Chain:           append([]TranscriptMessage(nil), chain.Messages...),
		BytesRead:       chain.BytesRead,
		HasBefore:       chain.HasBefore,
		MissingParent:   cloneIDPtr(chain.MissingParent),
		TruncatedParent: cloneIDPtr(chain.TruncatedParent),
	}, nil
}

func TranscriptMessagesToContractMessages(messages []TranscriptMessage) []contracts.Message {
	out := make([]contracts.Message, 0, len(messages))
	for _, message := range messages {
		converted, ok := ContractMessageFromTranscript(message)
		if !ok {
			continue
		}
		out = append(out, converted)
	}
	return out
}

func ContractMessageFromTranscript(message TranscriptMessage) (contracts.Message, bool) {
	if message.Message != nil {
		clone := cloneContractMessage(message.Message)
		if clone.UUID == "" {
			clone.UUID = message.UUID
		}
		if clone.ParentUUID == nil {
			clone.ParentUUID = cloneIDPtr(message.ParentUUID)
		}
		if clone.SessionID == "" {
			clone.SessionID = message.SessionID
		}
		if clone.Timestamp == "" {
			clone.Timestamp = message.Timestamp
		}
		if clone.Type == "" {
			clone.Type = contracts.MessageType(message.Type)
		}
		return *clone, true
	}
	messageType := contracts.MessageType(message.Type)
	switch messageType {
	case contracts.MessageUser, contracts.MessageAssistant, contracts.MessageSystem, contracts.MessageAttachment:
	default:
		return contracts.Message{}, false
	}
	content := append([]contracts.ContentBlock(nil), transcriptContentBlocks(&message)...)
	if len(content) == 0 {
		if text := textFromTranscriptMessage(&message); text != "" {
			content = []contracts.ContentBlock{contracts.NewTextBlock(text)}
		}
	}
	raw := map[string]any(nil)
	if messageType == contracts.MessageAttachment {
		raw = transcriptRawMap(message.Raw)
	}
	return contracts.Message{
		Type:       messageType,
		UUID:       message.UUID,
		ParentUUID: cloneIDPtr(message.ParentUUID),
		SessionID:  message.SessionID,
		Timestamp:  message.Timestamp,
		Subtype:    message.Subtype,
		Content:    content,
		Raw:        raw,
	}, true
}

func transcriptRawMap(raw json.RawMessage) map[string]any {
	if len(raw) == 0 {
		return nil
	}
	var fields map[string]any
	if err := json.Unmarshal(raw, &fields); err != nil || len(fields) == 0 {
		return nil
	}
	return fields
}

func latestConversationLeaf(transcript Transcript) contracts.ID {
	for i := len(transcript.Order) - 1; i >= 0; i-- {
		msg := transcript.Messages[transcript.Order[i]]
		if msg == nil {
			continue
		}
		if _, ok := transcript.LeafUUIDs[msg.UUID]; ok {
			return msg.UUID
		}
	}
	for i := len(transcript.Order) - 1; i >= 0; i-- {
		msg := transcript.Messages[transcript.Order[i]]
		if msg != nil && (msg.Type == "user" || msg.Type == "assistant") {
			return msg.UUID
		}
	}
	return ""
}
