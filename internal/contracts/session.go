package contracts

import (
	"encoding/json"
	"strings"
)

type SessionEntry struct {
	Type       MessageType      `json:"type"`
	UUID       ID               `json:"uuid,omitempty"`
	ParentUUID *ID              `json:"parentUuid,omitempty"`
	SessionID  ID               `json:"sessionId,omitempty"`
	Timestamp  string           `json:"timestamp,omitempty"`
	Message    *Message         `json:"message,omitempty"`
	Payload    *json.RawMessage `json:"payload,omitempty"`
}

func (e *SessionEntry) UnmarshalJSON(data []byte) error {
	type SessionEntryJSON SessionEntry
	var aux struct {
		*SessionEntryJSON
		EntryType              string `json:"entryType"`
		EntryTypeSnake         string `json:"entry_type"`
		MessageType            string `json:"messageType"`
		MessageTypeSnake       string `json:"message_type"`
		Role                   string `json:"role"`
		ID                     ID     `json:"id"`
		MessageID              ID     `json:"messageId"`
		MessageIDUpper         ID     `json:"messageID"`
		MessageIDSnake         ID     `json:"message_id"`
		MessageUUID            ID     `json:"messageUuid"`
		MessageUUIDUpper       ID     `json:"messageUUID"`
		MessageUUIDSnake       ID     `json:"message_uuid"`
		ParentUUIDUpper        *ID    `json:"parentUUID"`
		ParentUUIDSnake        *ID    `json:"parent_uuid"`
		ParentID               *ID    `json:"parentId"`
		ParentIDUpper          *ID    `json:"parentID"`
		ParentIDSnake          *ID    `json:"parent_id"`
		ParentMessageID        *ID    `json:"parentMessageId"`
		ParentMessageIDUpper   *ID    `json:"parentMessageID"`
		ParentMessageIDSnake   *ID    `json:"parent_message_id"`
		ParentMessageUUID      *ID    `json:"parentMessageUuid"`
		ParentMessageUUIDUpper *ID    `json:"parentMessageUUID"`
		ParentMessageUUIDSnake *ID    `json:"parent_message_uuid"`
		SessionIDUpper         ID     `json:"sessionID"`
		SessionIDSnake         ID     `json:"session_id"`
		Session                ID     `json:"session"`
		SessionUUID            ID     `json:"sessionUuid"`
		SessionUUIDUpper       ID     `json:"sessionUUID"`
		SessionUUIDSnake       ID     `json:"session_uuid"`
	}
	base := SessionEntryJSON{}
	aux.SessionEntryJSON = &base
	if err := json.Unmarshal(data, &aux); err != nil {
		return err
	}
	*e = SessionEntry(base)
	if e.Type == "" {
		e.Type = firstSessionEntryType(aux.EntryType, aux.EntryTypeSnake, aux.MessageType, aux.MessageTypeSnake, aux.Role)
	}
	if e.UUID == "" {
		e.UUID = firstSessionEntryID(aux.MessageUUID, aux.MessageUUIDUpper, aux.MessageUUIDSnake, aux.MessageID, aux.MessageIDUpper, aux.MessageIDSnake, aux.ID)
	}
	if e.ParentUUID == nil {
		e.ParentUUID = firstSessionEntryIDPtr(
			aux.ParentUUIDUpper,
			aux.ParentUUIDSnake,
			aux.ParentID,
			aux.ParentIDUpper,
			aux.ParentIDSnake,
			aux.ParentMessageID,
			aux.ParentMessageIDUpper,
			aux.ParentMessageIDSnake,
			aux.ParentMessageUUID,
			aux.ParentMessageUUIDUpper,
			aux.ParentMessageUUIDSnake,
		)
	}
	if e.SessionID == "" {
		e.SessionID = firstSessionEntryID(aux.SessionIDUpper, aux.SessionIDSnake, aux.Session, aux.SessionUUID, aux.SessionUUIDUpper, aux.SessionUUIDSnake)
	}
	return nil
}

func firstSessionEntryType(values ...string) MessageType {
	for _, value := range values {
		switch strings.ToLower(strings.TrimSpace(value)) {
		case string(MessageUser), string(MessageAssistant), string(MessageSystem), string(MessageAttachment):
			return MessageType(strings.ToLower(strings.TrimSpace(value)))
		}
	}
	return ""
}

func firstSessionEntryID(values ...ID) ID {
	for _, value := range values {
		if value != "" {
			return value
		}
	}
	return ""
}

func firstSessionEntryIDPtr(values ...*ID) *ID {
	for _, value := range values {
		if value != nil && *value != "" {
			cloned := *value
			return &cloned
		}
	}
	return nil
}

type SDKEventType string

const (
	SDKEventSystem    SDKEventType = "system"
	SDKEventAssistant SDKEventType = "assistant"
	SDKEventUser      SDKEventType = "user"
	SDKEventResult    SDKEventType = "result"
	SDKEventError     SDKEventType = "error"
	SDKEventStatus    SDKEventType = "status"
)

type SDKEvent struct {
	ID              ID             `json:"id,omitempty"`
	UUID            ID             `json:"uuid,omitempty"`
	Type            SDKEventType   `json:"type"`
	SessionID       ID             `json:"session_id,omitempty"`
	SessionIDCamel  ID             `json:"sessionId,omitempty"`
	ParentUUID      *ID            `json:"parent_uuid,omitempty"`
	ParentUUIDCamel *ID            `json:"parentUuid,omitempty"`
	Timestamp       string         `json:"timestamp,omitempty"`
	Message         *Message       `json:"message,omitempty"`
	Status          string         `json:"status,omitempty"`
	Result          any            `json:"result,omitempty"`
	Error           string         `json:"error,omitempty"`
	Meta            map[string]any `json:"meta,omitempty"`
}

func (e *SDKEvent) UnmarshalJSON(data []byte) error {
	type SDKEventJSON SDKEvent
	var aux struct {
		*SDKEventJSON
		EventIDSnake         ID  `json:"event_id"`
		EventIDCamel         ID  `json:"eventId"`
		SessionIDUpper       ID  `json:"sessionID"`
		SessionUUID          ID  `json:"sessionUuid"`
		SessionUUIDUpper     ID  `json:"sessionUUID"`
		SessionUUIDSnake     ID  `json:"session_uuid"`
		ParentUUIDUpper      *ID `json:"parentUUID"`
		ParentID             *ID `json:"parentId"`
		ParentIDUpper        *ID `json:"parentID"`
		ParentIDSnake        *ID `json:"parent_id"`
		ParentMessageID      *ID `json:"parentMessageId"`
		ParentMessageIDUpper *ID `json:"parentMessageID"`
		ParentMessageIDSnake *ID `json:"parent_message_id"`
	}
	base := SDKEventJSON{}
	aux.SDKEventJSON = &base
	if err := json.Unmarshal(data, &aux); err != nil {
		return err
	}
	*e = SDKEvent(base)
	if e.ID == "" {
		e.ID = aux.EventIDSnake
	}
	if e.ID == "" {
		e.ID = aux.EventIDCamel
	}
	if e.SessionID == "" {
		e.SessionID = aux.SessionIDUpper
	}
	if e.SessionID == "" {
		e.SessionID = aux.SessionUUID
	}
	if e.SessionID == "" {
		e.SessionID = aux.SessionUUIDUpper
	}
	if e.SessionID == "" {
		e.SessionID = aux.SessionUUIDSnake
	}
	if e.ParentUUID == nil {
		e.ParentUUID = firstSDKEventIDPtr(
			aux.ParentUUIDUpper,
			aux.ParentID,
			aux.ParentIDUpper,
			aux.ParentIDSnake,
			aux.ParentMessageID,
			aux.ParentMessageIDUpper,
			aux.ParentMessageIDSnake,
		)
	}
	return nil
}

func firstSDKEventIDPtr(values ...*ID) *ID {
	for _, value := range values {
		if value != nil && *value != "" {
			cloned := *value
			return &cloned
		}
	}
	return nil
}
