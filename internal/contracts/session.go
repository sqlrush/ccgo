package contracts

import "encoding/json"

type SessionEntry struct {
	Type       MessageType      `json:"type"`
	UUID       ID               `json:"uuid,omitempty"`
	ParentUUID *ID              `json:"parentUuid,omitempty"`
	SessionID  ID               `json:"sessionId,omitempty"`
	Timestamp  string           `json:"timestamp,omitempty"`
	Message    *Message         `json:"message,omitempty"`
	Payload    *json.RawMessage `json:"payload,omitempty"`
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
		EventIDSnake     ID `json:"event_id"`
		EventIDCamel     ID `json:"eventId"`
		SessionUUID      ID `json:"sessionUuid"`
		SessionUUIDUpper ID `json:"sessionUUID"`
		SessionUUIDSnake ID `json:"session_uuid"`
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
		e.SessionID = aux.SessionUUID
	}
	if e.SessionID == "" {
		e.SessionID = aux.SessionUUIDUpper
	}
	if e.SessionID == "" {
		e.SessionID = aux.SessionUUIDSnake
	}
	return nil
}
