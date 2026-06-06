package contracts

import (
	"bytes"
	"encoding/json"
	"strconv"
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
	if entryType := firstSessionEntryType(string(e.Type), aux.EntryType, aux.EntryTypeSnake, aux.MessageType, aux.MessageTypeSnake, aux.Role); entryType != "" {
		e.Type = entryType
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
		switch messageType := CanonicalMessageType(value); messageType {
		case MessageUser, MessageAssistant, MessageSystem, MessageAttachment, MessageProgress, MessageTombstone:
			return messageType
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
		EventTypeSnake         string          `json:"event_type"`
		EventTypeCamel         string          `json:"eventType"`
		Event                  string          `json:"event"`
		Name                   string          `json:"name"`
		Kind                   string          `json:"kind"`
		Role                   string          `json:"role"`
		MessageType            string          `json:"messageType"`
		MessageTypeSnake       string          `json:"message_type"`
		EventIDSnake           ID              `json:"event_id"`
		EventIDCamel           ID              `json:"eventId"`
		EventIDUpper           ID              `json:"eventID"`
		MessageID              ID              `json:"messageId"`
		MessageIDUpper         ID              `json:"messageID"`
		MessageIDSnake         ID              `json:"message_id"`
		MessageUUID            ID              `json:"messageUuid"`
		MessageUUIDUpper       ID              `json:"messageUUID"`
		MessageUUIDSnake       ID              `json:"message_uuid"`
		SessionIDUpper         ID              `json:"sessionID"`
		SessionUUID            ID              `json:"sessionUuid"`
		SessionUUIDUpper       ID              `json:"sessionUUID"`
		SessionUUIDSnake       ID              `json:"session_uuid"`
		ParentUUIDUpper        *ID             `json:"parentUUID"`
		ParentID               *ID             `json:"parentId"`
		ParentIDUpper          *ID             `json:"parentID"`
		ParentIDSnake          *ID             `json:"parent_id"`
		ParentMessageID        *ID             `json:"parentMessageId"`
		ParentMessageIDUpper   *ID             `json:"parentMessageID"`
		ParentMessageIDSnake   *ID             `json:"parent_message_id"`
		CreatedAt              string          `json:"createdAt"`
		CreatedAtSnake         string          `json:"created_at"`
		Created                string          `json:"created"`
		CreatedTime            string          `json:"createdTime"`
		CreatedTimeSnake       string          `json:"created_time"`
		Time                   string          `json:"time"`
		Datetime               string          `json:"datetime"`
		DateTime               string          `json:"dateTime"`
		DateTimeSnake          string          `json:"date_time"`
		EventTime              string          `json:"eventTime"`
		EventTimeSnake         string          `json:"event_time"`
		OccurredAt             string          `json:"occurredAt"`
		OccurredAtSnake        string          `json:"occurred_at"`
		MessagePayload         json.RawMessage `json:"message_payload"`
		MessagePayloadCamel    json.RawMessage `json:"messagePayload"`
		SerializedMessage      json.RawMessage `json:"serialized_message"`
		SerializedMessageCamel json.RawMessage `json:"serializedMessage"`
		Payload                json.RawMessage `json:"payload"`
		Data                   json.RawMessage `json:"data"`
		Body                   json.RawMessage `json:"body"`
		Record                 json.RawMessage `json:"record"`
		Entry                  json.RawMessage `json:"entry"`
		Item                   json.RawMessage `json:"item"`
	}
	base := SDKEventJSON{}
	aux.SDKEventJSON = &base
	normalizedData := normalizeSDKEventJSON(data)
	if err := json.Unmarshal(normalizedData, &aux); err != nil {
		return err
	}
	*e = SDKEvent(base)
	if eventType := firstSDKEventType(string(e.Type), aux.EventTypeSnake, aux.EventTypeCamel, aux.Event, aux.Name, aux.Kind, aux.MessageType, aux.MessageTypeSnake, aux.Role); eventType != "" {
		e.Type = eventType
	}
	if e.ID == "" {
		e.ID = firstSDKEventID(
			aux.EventIDSnake,
			aux.EventIDCamel,
			aux.EventIDUpper,
			aux.MessageUUID,
			aux.MessageUUIDUpper,
			aux.MessageUUIDSnake,
			aux.MessageID,
			aux.MessageIDUpper,
			aux.MessageIDSnake,
			e.UUID,
		)
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
	if e.Timestamp == "" {
		e.Timestamp = firstSDKEventString(
			aux.CreatedAt,
			aux.CreatedAtSnake,
			aux.Created,
			aux.CreatedTime,
			aux.CreatedTimeSnake,
			aux.Time,
			aux.Datetime,
			aux.DateTime,
			aux.DateTimeSnake,
			aux.EventTime,
			aux.EventTimeSnake,
			aux.OccurredAt,
			aux.OccurredAtSnake,
		)
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
	if e.Message == nil {
		e.Message = firstSDKEventMessage(e.Type,
			aux.MessagePayload,
			aux.MessagePayloadCamel,
			aux.SerializedMessage,
			aux.SerializedMessageCamel,
			aux.Payload,
			aux.Data,
			aux.Body,
			aux.Record,
			aux.Entry,
			aux.Item,
		)
	}
	if e.Message == nil {
		fields := map[string]json.RawMessage{}
		if err := json.Unmarshal(data, &fields); err == nil {
			e.Message = firstSDKEventSidecarMessage(e.Type,
				fields["metadata"],
				fields["meta"],
				fields["attributes"],
				fields["properties"],
				fields["attrs"],
			)
		}
	}
	if e.Message == nil {
		e.Message = sdkEventTopLevelMessage(data, e.Type)
	}
	if e.Message != nil && e.Message.Type == "" && e.Type != "" {
		e.Message.Type = MessageType(e.Type)
	}
	return nil
}

func firstSDKEventType(values ...string) SDKEventType {
	for _, value := range values {
		if eventType := CanonicalSDKEventType(value); eventType != "" {
			return eventType
		}
	}
	return ""
}

func normalizeSDKEventJSON(data []byte) []byte {
	trimmed := bytes.TrimSpace(data)
	if len(trimmed) == 0 || trimmed[0] != '{' {
		return data
	}
	fields := map[string]json.RawMessage{}
	if err := json.Unmarshal(data, &fields); err != nil {
		return data
	}
	changed := false
	for _, name := range []string{
		"timestamp",
		"createdAt",
		"created_at",
		"created",
		"createdTime",
		"created_time",
		"time",
		"datetime",
		"dateTime",
		"date_time",
		"eventTime",
		"event_time",
		"occurredAt",
		"occurred_at",
	} {
		raw := bytes.TrimSpace(fields[name])
		if len(raw) == 0 || !sdkEventTimestampRawLooksNumeric(raw) {
			continue
		}
		fields[name] = []byte(strconv.Quote(string(raw)))
		changed = true
	}
	if raw := bytes.TrimSpace(fields["message"]); len(raw) > 0 && !bytes.Equal(raw, []byte("null")) && raw[0] != '{' {
		wrapped, err := json.Marshal(map[string]json.RawMessage{"content": raw})
		if err == nil {
			fields["message"] = wrapped
			changed = true
		}
	}
	if !changed {
		return data
	}
	encoded, err := json.Marshal(fields)
	if err != nil {
		return data
	}
	return encoded
}

func sdkEventTimestampRawLooksNumeric(raw []byte) bool {
	if len(raw) == 0 {
		return false
	}
	return raw[0] == '-' || (raw[0] >= '0' && raw[0] <= '9')
}

func CanonicalSDKEventType(value string) SDKEventType {
	normalized := strings.ToLower(strings.TrimSpace(value))
	if normalized == "" {
		return ""
	}
	compact := strings.NewReplacer("_", "", "-", "", " ", "", ".", "").Replace(normalized)
	switch compact {
	case "system", "systemmessage", "messagesystem", "systemevent", "eventsystem":
		return SDKEventSystem
	case "assistant", "assistantmessage", "messageassistant", "assistantevent", "eventassistant", "assistantdelta", "assistantmessagedelta", "messageassistantdelta", "messagedelta":
		return SDKEventAssistant
	case "user", "usermessage", "messageuser", "userevent", "eventuser", "humanmessage", "messagehuman", "humaninput", "userinput", "inputuser":
		return SDKEventUser
	case "result", "resultevent", "eventresult", "finalresult", "resultfinal", "completionevent", "eventcompletion", "completionresult", "resultcompletion", "responsecomplete", "responsecompleted":
		return SDKEventResult
	case "error", "errorevent", "eventerror", "failureevent", "eventfailure", "failedevent", "eventfailed", "exceptionevent", "eventexception", "errorupdate", "updateerror":
		return SDKEventError
	case "status", "statusevent", "eventstatus", "statusupdate", "updatestatus", "statusmessage", "messagestatus", "progress", "progressevent", "eventprogress", "progressupdate", "updateprogress", "progressmessage", "messageprogress":
		return SDKEventStatus
	default:
		return ""
	}
}

func firstSDKEventString(values ...string) string {
	for _, value := range values {
		if value != "" {
			return value
		}
	}
	return ""
}

func firstSDKEventID(values ...ID) ID {
	for _, value := range values {
		if value != "" {
			return value
		}
	}
	return ""
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

func firstSDKEventMessage(eventType SDKEventType, values ...json.RawMessage) *Message {
	for _, value := range values {
		if message := sdkEventMessageFromRaw(value, eventType); message != nil {
			return message
		}
	}
	return nil
}

func sdkEventMessageFromRaw(raw json.RawMessage, eventType SDKEventType) *Message {
	raw = bytes.TrimSpace(raw)
	if len(raw) == 0 || bytes.Equal(raw, []byte("null")) {
		return nil
	}
	var message Message
	if err := json.Unmarshal(raw, &message); err == nil && sdkEventMessageHasData(message) {
		if message.Type == "" {
			message.Type = MessageType(eventType)
		}
		return &message
	}
	var fields map[string]json.RawMessage
	if err := json.Unmarshal(raw, &fields); err != nil {
		return nil
	}
	for _, name := range []string{
		"message",
		"message_payload",
		"messagePayload",
		"serialized_message",
		"serializedMessage",
		"payload",
		"data",
		"body",
		"record",
		"entry",
		"item",
		"event",
		"result",
		"response",
		"output",
	} {
		if nested := sdkEventMessageFromRaw(fields[name], eventType); nested != nil {
			return nested
		}
	}
	for _, name := range []string{
		"metadata",
		"meta",
		"attributes",
		"properties",
		"attrs",
	} {
		if nested := sdkEventMessageFromRaw(fields[name], eventType); nested != nil && sdkEventTopLevelMessageHasData(*nested) {
			return nested
		}
	}
	return nil
}

func firstSDKEventSidecarMessage(eventType SDKEventType, values ...json.RawMessage) *Message {
	for _, value := range values {
		if message := sdkEventMessageFromRaw(value, eventType); message != nil && sdkEventTopLevelMessageHasData(*message) {
			return message
		}
	}
	return nil
}

func sdkEventTopLevelMessage(data []byte, eventType SDKEventType) *Message {
	var message Message
	if err := json.Unmarshal(data, &message); err != nil || !sdkEventTopLevelMessageHasData(message) {
		return nil
	}
	if message.Type == "" {
		message.Type = MessageType(eventType)
	}
	return &message
}

func sdkEventMessageHasData(message Message) bool {
	return message.Type != "" ||
		message.ID != "" ||
		message.UUID != "" ||
		message.SessionID != "" ||
		message.ParentUUID != nil ||
		message.Timestamp != "" ||
		len(message.Content) > 0 ||
		message.Subtype != "" ||
		message.Model != "" ||
		message.Usage != nil
}

func sdkEventTopLevelMessageHasData(message Message) bool {
	return len(message.Content) > 0 ||
		message.Subtype != "" ||
		message.Model != "" ||
		message.Usage != nil
}
