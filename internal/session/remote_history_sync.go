package session

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"sort"

	"ccgo/internal/contracts"
)

type RemoteHistorySyncResult struct {
	Considered int
	Appended   int
	Skipped    int
	Duplicates int
	LastUUID   contracts.ID
}

func RemoteHistoryTranscriptMessages(events []contracts.SDKEvent) []TranscriptMessage {
	messages := make([]TranscriptMessage, 0, len(events))
	for _, event := range events {
		message := remoteEventTranscriptMessage(event)
		if message.UUID == "" {
			continue
		}
		messages = append(messages, message)
	}
	sort.SliceStable(messages, func(i, j int) bool {
		left := messages[i].Timestamp
		right := messages[j].Timestamp
		if left == "" || right == "" {
			return left != ""
		}
		return left < right
	})
	linkMissingRemoteParents(messages)
	return messages
}

func AppendRemoteHistoryTranscript(path string, events []contracts.SDKEvent) (RemoteHistorySyncResult, error) {
	messages := RemoteHistoryTranscriptMessages(events)
	result := RemoteHistorySyncResult{Considered: len(events), Skipped: len(events) - len(messages)}
	transcript, err := LoadTranscript(path)
	if err != nil {
		return result, err
	}
	seen := map[contracts.ID]struct{}{}
	for _, id := range transcript.Order {
		seen[id] = struct{}{}
	}
	for _, message := range messages {
		if _, ok := seen[message.UUID]; ok {
			result.Duplicates++
			continue
		}
		if err := AppendTranscriptMessage(path, message); err != nil {
			return result, err
		}
		seen[message.UUID] = struct{}{}
		result.Appended++
		result.LastUUID = message.UUID
	}
	return result, nil
}

func remoteEventTranscriptMessage(event contracts.SDKEvent) TranscriptMessage {
	if event.Message == nil {
		return TranscriptMessage{}
	}
	message := cloneContractMessage(event.Message)
	entryType := string(message.Type)
	if entryType == "" {
		entryType = string(event.Type)
	}
	if !isTranscriptType(entryType) {
		return TranscriptMessage{}
	}
	if message.UUID == "" {
		if message.ID != "" {
			message.UUID = contracts.ID(message.ID)
		} else {
			message.UUID = remoteHistoryEventUUID(event)
		}
	}
	if message.SessionID == "" {
		message.SessionID = event.SessionID
	}
	return TranscriptMessage{
		Type:       entryType,
		UUID:       message.UUID,
		ParentUUID: cloneIDPtr(message.ParentUUID),
		SessionID:  message.SessionID,
		Timestamp:  message.Timestamp,
		Subtype:    message.Subtype,
		Message:    message,
	}
}

func linkMissingRemoteParents(messages []TranscriptMessage) {
	lastBySession := map[contracts.ID]contracts.ID{}
	for i := range messages {
		sessionID := messages[i].SessionID
		if messages[i].ParentUUID == nil {
			if last, ok := lastBySession[sessionID]; ok {
				messages[i].ParentUUID = cloneIDPtr(&last)
				if messages[i].Message != nil && messages[i].Message.ParentUUID == nil {
					messages[i].Message.ParentUUID = cloneIDPtr(&last)
				}
			}
		}
		lastBySession[sessionID] = messages[i].UUID
	}
}

func remoteHistoryEventUUID(event contracts.SDKEvent) contracts.ID {
	encoded, err := json.Marshal(event)
	if err != nil {
		encoded = []byte(string(event.Type) + ":" + string(event.SessionID) + ":" + event.Status + ":" + event.Error)
	}
	sum := sha256.Sum256(encoded)
	return contracts.ID("remote_" + hex.EncodeToString(sum[:])[:24])
}

func cloneContractMessage(message *contracts.Message) *contracts.Message {
	if message == nil {
		return nil
	}
	clone := *message
	clone.ParentUUID = cloneIDPtr(message.ParentUUID)
	clone.Content = append([]contracts.ContentBlock(nil), message.Content...)
	if message.Usage != nil {
		usage := *message.Usage
		clone.Usage = &usage
	}
	if len(message.Raw) > 0 {
		clone.Raw = map[string]any{}
		for key, value := range message.Raw {
			clone.Raw[key] = value
		}
	}
	return &clone
}
