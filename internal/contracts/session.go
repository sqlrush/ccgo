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
	Type      SDKEventType   `json:"type"`
	SessionID ID             `json:"session_id,omitempty"`
	Message   *Message       `json:"message,omitempty"`
	Status    string         `json:"status,omitempty"`
	Result    any            `json:"result,omitempty"`
	Error     string         `json:"error,omitempty"`
	Meta      map[string]any `json:"meta,omitempty"`
}
