package contracts

import "encoding/json"

type MessageType string

const (
	MessageUser       MessageType = "user"
	MessageAssistant  MessageType = "assistant"
	MessageSystem     MessageType = "system"
	MessageAttachment MessageType = "attachment"
	MessageProgress   MessageType = "progress"
	MessageTombstone  MessageType = "tombstone"
)

type ContentBlockType string

const (
	ContentText       ContentBlockType = "text"
	ContentToolUse    ContentBlockType = "tool_use"
	ContentToolResult ContentBlockType = "tool_result"
	ContentImage      ContentBlockType = "image"
	ContentThinking   ContentBlockType = "thinking"
	ContentCacheEdits ContentBlockType = "cache_edits"
)

type ContentBlock struct {
	Type           ContentBlockType `json:"type"`
	Text           string           `json:"text,omitempty"`
	Source         any              `json:"source,omitempty"`
	ID             string           `json:"id,omitempty"`
	Name           string           `json:"name,omitempty"`
	Input          json.RawMessage  `json:"input,omitempty"`
	Content        any              `json:"content,omitempty"`
	IsError        bool             `json:"is_error,omitempty"`
	ToolUseID      string           `json:"tool_use_id,omitempty"`
	CacheControl   *CacheControl    `json:"cache_control,omitempty"`
	CacheReference string           `json:"cache_reference,omitempty"`
	Edits          []CacheEdit      `json:"edits,omitempty"`
}

func (b *ContentBlock) UnmarshalJSON(data []byte) error {
	type contentBlockJSON ContentBlock
	var base contentBlockJSON
	if err := json.Unmarshal(data, &base); err != nil {
		return err
	}
	*b = ContentBlock(base)

	fields := map[string]json.RawMessage{}
	if err := json.Unmarshal(data, &fields); err != nil {
		return err
	}
	if b.ToolUseID == "" {
		b.ToolUseID = stringJSONField(fields, "toolUseId", "toolUseID")
	}
	if _, hasCanonical := fields["is_error"]; !hasCanonical {
		if value, ok := boolJSONField(fields, "isError"); ok {
			b.IsError = value
		}
	}
	if b.CacheControl == nil {
		b.CacheControl = cacheControlJSONField(fields, "cacheControl")
	}
	if b.CacheReference == "" {
		b.CacheReference = stringJSONField(fields, "cacheReference")
	}
	return nil
}

type CacheControl struct {
	Type  string `json:"type"`
	Scope string `json:"scope,omitempty"`
	TTL   string `json:"ttl,omitempty"`
}

type CacheEdit struct {
	Type           string `json:"type"`
	CacheReference string `json:"cache_reference"`
}

type ImageSource struct {
	Type      string `json:"type"`
	MediaType string `json:"media_type,omitempty"`
	Data      string `json:"data,omitempty"`
}

func (e *CacheEdit) UnmarshalJSON(data []byte) error {
	type cacheEditJSON CacheEdit
	var base cacheEditJSON
	if err := json.Unmarshal(data, &base); err != nil {
		return err
	}
	*e = CacheEdit(base)

	fields := map[string]json.RawMessage{}
	if err := json.Unmarshal(data, &fields); err != nil {
		return err
	}
	if e.CacheReference == "" {
		e.CacheReference = stringJSONField(fields, "cacheReference")
	}
	return nil
}

func stringJSONField(fields map[string]json.RawMessage, names ...string) string {
	for _, name := range names {
		raw, ok := fields[name]
		if !ok {
			continue
		}
		var value string
		if err := json.Unmarshal(raw, &value); err == nil {
			return value
		}
	}
	return ""
}

func boolJSONField(fields map[string]json.RawMessage, names ...string) (bool, bool) {
	for _, name := range names {
		raw, ok := fields[name]
		if !ok {
			continue
		}
		var value bool
		if err := json.Unmarshal(raw, &value); err == nil {
			return value, true
		}
	}
	return false, false
}

func cacheControlJSONField(fields map[string]json.RawMessage, names ...string) *CacheControl {
	for _, name := range names {
		raw, ok := fields[name]
		if !ok {
			continue
		}
		var value *CacheControl
		if err := json.Unmarshal(raw, &value); err == nil {
			return value
		}
	}
	return nil
}

type Message struct {
	ID         string         `json:"id,omitempty"`
	Type       MessageType    `json:"type"`
	UUID       ID             `json:"uuid,omitempty"`
	ParentUUID *ID            `json:"parentUuid,omitempty"`
	SessionID  ID             `json:"sessionId,omitempty"`
	IsMeta     bool           `json:"isMeta,omitempty"`
	Timestamp  string         `json:"timestamp,omitempty"`
	Content    []ContentBlock `json:"content,omitempty"`
	Subtype    string         `json:"subtype,omitempty"`
	Model      string         `json:"model,omitempty"`
	Usage      *Usage         `json:"usage,omitempty"`
	Raw        map[string]any `json:"raw,omitempty"`
}

func (m *Message) UnmarshalJSON(data []byte) error {
	type MessageJSON Message
	var aux struct {
		*MessageJSON
		ParentUUIDSnake  *ID   `json:"parent_uuid"`
		SessionIDUpper   ID    `json:"sessionID"`
		SessionIDSnake   ID    `json:"session_id"`
		SessionUUID      ID    `json:"sessionUuid"`
		SessionUUIDUpper ID    `json:"sessionUUID"`
		SessionUUIDSnake ID    `json:"session_uuid"`
		IsMetaSnake      *bool `json:"is_meta"`
	}
	base := MessageJSON{}
	aux.MessageJSON = &base
	if err := json.Unmarshal(data, &aux); err != nil {
		return err
	}
	*m = Message(base)
	if m.ParentUUID == nil {
		m.ParentUUID = aux.ParentUUIDSnake
	}
	if m.SessionID == "" {
		m.SessionID = aux.SessionIDUpper
	}
	if m.SessionID == "" {
		m.SessionID = aux.SessionIDSnake
	}
	if m.SessionID == "" {
		m.SessionID = aux.SessionUUID
	}
	if m.SessionID == "" {
		m.SessionID = aux.SessionUUIDUpper
	}
	if m.SessionID == "" {
		m.SessionID = aux.SessionUUIDSnake
	}
	if aux.IsMetaSnake != nil {
		m.IsMeta = *aux.IsMetaSnake
	}
	return nil
}

type APIMessage struct {
	Role    string         `json:"role"`
	Content []ContentBlock `json:"content"`
}

type Usage struct {
	InputTokens              int                `json:"input_tokens,omitempty"`
	OutputTokens             int                `json:"output_tokens,omitempty"`
	CacheCreationInputTokens int                `json:"cache_creation_input_tokens,omitempty"`
	CacheReadInputTokens     int                `json:"cache_read_input_tokens,omitempty"`
	CacheDeletedInputTokens  int                `json:"cache_deleted_input_tokens,omitempty"`
	ServerToolUse            ToolUseUsage       `json:"server_tool_use,omitempty"`
	ServiceTier              string             `json:"service_tier,omitempty"`
	CacheCreation            CacheCreationUsage `json:"cache_creation,omitempty"`
	InferenceGeo             string             `json:"inference_geo,omitempty"`
	Iterations               int                `json:"iterations,omitempty"`
	Speed                    string             `json:"speed,omitempty"`
	CostUSD                  float64            `json:"cost_usd,omitempty"`
}

type ToolUseUsage struct {
	WebSearchRequests int `json:"web_search_requests,omitempty"`
	WebFetchRequests  int `json:"web_fetch_requests,omitempty"`
}

type CacheCreationUsage struct {
	Ephemeral1hInputTokens int `json:"ephemeral_1h_input_tokens,omitempty"`
	Ephemeral5mInputTokens int `json:"ephemeral_5m_input_tokens,omitempty"`
}

func NewTextBlock(text string) ContentBlock {
	return ContentBlock{Type: ContentText, Text: text}
}

func NewBase64ImageBlock(mediaType string, data string) ContentBlock {
	if mediaType == "" {
		mediaType = "image/png"
	}
	return ContentBlock{
		Type: ContentImage,
		Source: ImageSource{
			Type:      "base64",
			MediaType: mediaType,
			Data:      data,
		},
	}
}
