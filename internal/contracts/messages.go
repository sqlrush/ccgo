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

type CacheControl struct {
	Type  string `json:"type"`
	Scope string `json:"scope,omitempty"`
	TTL   string `json:"ttl,omitempty"`
}

type CacheEdit struct {
	Type           string `json:"type"`
	CacheReference string `json:"cache_reference"`
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
		ParentUUIDSnake *ID   `json:"parent_uuid"`
		SessionIDSnake  ID    `json:"session_id"`
		IsMetaSnake     *bool `json:"is_meta"`
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
		m.SessionID = aux.SessionIDSnake
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
