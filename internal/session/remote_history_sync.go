package session

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"net/http"
	"sort"

	"ccgo/internal/contracts"
)

type RemoteHistorySyncResult struct {
	Considered   int
	Appended     int
	Skipped      int
	Duplicates   int
	LastUUID     contracts.ID
	Pages        int
	Complete     bool
	NextBeforeID string
}

func RemoteHistoryTranscriptMessages(events []contracts.SDKEvent) []TranscriptMessage {
	return remoteHistoryTranscriptMessages(events, nil)
}

func remoteHistoryTranscriptMessages(events []contracts.SDKEvent, existing map[contracts.ID]struct{}) []TranscriptMessage {
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
	linkMissingRemoteParents(messages, existing)
	return messages
}

func AppendRemoteHistoryTranscript(path string, events []contracts.SDKEvent) (RemoteHistorySyncResult, error) {
	result := RemoteHistorySyncResult{Considered: len(events)}
	transcript, err := LoadTranscript(path)
	if err != nil {
		return result, err
	}
	seen := map[contracts.ID]struct{}{}
	for _, id := range transcript.Order {
		seen[id] = struct{}{}
	}
	messages := remoteHistoryTranscriptMessages(events, seen)
	result.Skipped = len(events) - len(messages)
	linkRemoteMessagesToExistingTranscript(transcript, messages, seen)
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

func SyncRemoteHistoryTranscript(ctx context.Context, client *http.Client, authCtx RemoteHistoryAuthContext, provider RemoteHistoryTokenProvider, path string, options RemoteHistoryFetchOptions) (RemoteHistorySyncResult, error) {
	var remote *RemoteHistoryEvents
	var err error
	if provider == nil {
		remote, err = FetchRemoteHistory(ctx, client, authCtx, options)
	} else {
		remote, err = FetchRemoteHistoryWithTokenRefresh(ctx, client, authCtx, provider, options)
	}
	if err != nil {
		return RemoteHistorySyncResult{}, err
	}
	if remote == nil {
		return RemoteHistorySyncResult{}, nil
	}
	result, err := AppendRemoteHistoryTranscript(path, remote.Events)
	if err != nil {
		return result, err
	}
	result.Pages = remote.Pages
	result.Complete = remote.Complete
	result.NextBeforeID = remote.NextBeforeID
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
		} else if event.UUID != "" {
			message.UUID = event.UUID
		} else if event.ID != "" {
			message.UUID = event.ID
		} else {
			message.UUID = remoteHistoryEventUUID(event)
		}
	}
	if message.SessionID == "" {
		message.SessionID = remoteEventSessionID(event)
	}
	if message.ParentUUID == nil {
		message.ParentUUID = remoteEventParentUUID(event)
	}
	if message.Timestamp == "" {
		message.Timestamp = event.Timestamp
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

func remoteEventSessionID(event contracts.SDKEvent) contracts.ID {
	if event.SessionID != "" {
		return event.SessionID
	}
	return event.SessionIDCamel
}

func remoteEventParentUUID(event contracts.SDKEvent) *contracts.ID {
	if event.ParentUUID != nil {
		return cloneIDPtr(event.ParentUUID)
	}
	return cloneIDPtr(event.ParentUUIDCamel)
}

func linkMissingRemoteParents(messages []TranscriptMessage, existing map[contracts.ID]struct{}) {
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
		if _, duplicate := existing[messages[i].UUID]; duplicate {
			continue
		}
		lastBySession[sessionID] = messages[i].UUID
	}
}

func linkRemoteMessagesToExistingTranscript(transcript Transcript, messages []TranscriptMessage, existing map[contracts.ID]struct{}) {
	lastBySession := latestTranscriptMessagesBySession(transcript)
	for i := range messages {
		sessionID := messages[i].SessionID
		if _, duplicate := existing[messages[i].UUID]; duplicate {
			continue
		}
		if messages[i].ParentUUID == nil {
			if last, ok := lastBySession[sessionID]; ok && shouldLinkRemoteParent(last.Timestamp, messages[i].Timestamp) {
				messages[i].ParentUUID = cloneIDPtr(&last.UUID)
				if messages[i].Message != nil && messages[i].Message.ParentUUID == nil {
					messages[i].Message.ParentUUID = cloneIDPtr(&last.UUID)
				}
			}
		}
		lastBySession[sessionID] = messages[i]
	}
}

func latestTranscriptMessagesBySession(transcript Transcript) map[contracts.ID]TranscriptMessage {
	out := map[contracts.ID]TranscriptMessage{}
	for _, id := range transcript.Order {
		msg := transcript.Messages[id]
		if msg == nil {
			continue
		}
		out[msg.SessionID] = *msg
	}
	return out
}

func shouldLinkRemoteParent(existingTimestamp string, incomingTimestamp string) bool {
	if existingTimestamp == "" || incomingTimestamp == "" {
		return true
	}
	return existingTimestamp <= incomingTimestamp
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
