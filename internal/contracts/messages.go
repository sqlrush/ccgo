package contracts

import (
	"bytes"
	"encoding/json"
	"strings"
)

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
	ContentText                ContentBlockType = "text"
	ContentToolUse             ContentBlockType = "tool_use"
	ContentToolResult          ContentBlockType = "tool_result"
	ContentImage               ContentBlockType = "image"
	ContentThinking            ContentBlockType = "thinking"
	ContentCacheEdits          ContentBlockType = "cache_edits"
	ContentServerToolUse       ContentBlockType = "server_tool_use"
	ContentWebSearchToolResult ContentBlockType = "web_search_tool_result"
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
	Signature      string           `json:"signature,omitempty"`
}

func (b *ContentBlock) UnmarshalJSON(data []byte) error {
	var aux struct {
		Type           ContentBlockType `json:"type"`
		Text           string           `json:"text"`
		Thinking       string           `json:"thinking"`
		Source         any              `json:"source"`
		ID             ID               `json:"id"`
		Name           string           `json:"name"`
		Input          json.RawMessage  `json:"input"`
		Content        any              `json:"content"`
		IsError        bool             `json:"is_error"`
		ToolUseID      ID               `json:"tool_use_id"`
		CacheControl   *CacheControl    `json:"cache_control"`
		CacheReference string           `json:"cache_reference"`
		Edits          []CacheEdit      `json:"edits"`
		Signature      string           `json:"signature"`
	}
	if err := json.Unmarshal(data, &aux); err != nil {
		return err
	}
	canonicalType := canonicalContentBlockType(string(aux.Type))
	// For thinking blocks, the reasoning is stored under the "thinking" JSON key.
	// Prefer aux.Thinking over aux.Text so the canonical wire format is honoured.
	text := aux.Text
	if canonicalType == ContentThinking && aux.Thinking != "" {
		text = aux.Thinking
	}
	*b = ContentBlock{
		Type:           canonicalType,
		Text:           text,
		Source:         aux.Source,
		ID:             string(aux.ID),
		Name:           aux.Name,
		Input:          aux.Input,
		Content:        aux.Content,
		IsError:        aux.IsError,
		ToolUseID:      string(aux.ToolUseID),
		CacheControl:   aux.CacheControl,
		CacheReference: aux.CacheReference,
		Edits:          aux.Edits,
		Signature:      aux.Signature,
	}

	fields := map[string]json.RawMessage{}
	if err := json.Unmarshal(data, &fields); err != nil {
		return err
	}
	if b.ToolUseID == "" {
		b.ToolUseID = idJSONField(fields, "toolUseId", "toolUseID")
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
	if b.Text == "" {
		b.Text = contentBlockTextJSONField(fields, b.Type)
	}
	if b.Type == "" && b.Text != "" {
		b.Type = ContentText
	}
	if b.Type == ContentImage {
		if source := imageSourceJSONField(fields, "source", "imageSource", "image_source"); source != nil {
			b.Source = *source
		}
		if source := imageSourceFromBlockFields(fields); source != nil {
			b.Source = *source
		}
	}
	return nil
}

// MarshalJSON encodes ContentBlock to JSON, emitting thinking blocks with the
// canonical Anthropic wire format: {"type":"thinking","thinking":"<reasoning>","signature":"<sig>"}.
// All other block types are encoded via the default struct-tag marshaling so
// their output is byte-identical to what encoding/json would produce on its own.
func (b ContentBlock) MarshalJSON() ([]byte, error) {
	if b.Type == ContentThinking {
		type thinkingBlock struct {
			Type      ContentBlockType `json:"type"`
			Thinking  string           `json:"thinking,omitempty"`
			Signature string           `json:"signature,omitempty"`
		}
		return json.Marshal(thinkingBlock{
			Type:      b.Type,
			Thinking:  b.Text,
			Signature: b.Signature,
		})
	}
	// Use a type alias to avoid infinite recursion while preserving all struct tags.
	type alias ContentBlock
	return json.Marshal(alias(b))
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

func (s *ImageSource) UnmarshalJSON(data []byte) error {
	type imageSourceJSON ImageSource
	var base imageSourceJSON
	if err := json.Unmarshal(data, &base); err != nil {
		return err
	}
	*s = ImageSource(base)

	fields := map[string]json.RawMessage{}
	if err := json.Unmarshal(data, &fields); err != nil {
		return err
	}
	if s.Type == "" {
		s.Type = stringJSONField(fields, "kind", "sourceType", "source_type", "encoding", "format")
	}
	if s.MediaType == "" {
		s.MediaType = stringJSONField(fields, "mediaType", "mime_type", "mimeType", "content_type", "contentType", "mime", "media")
	}
	if s.Data == "" {
		s.Data = stringJSONField(fields, "base64", "content", "value", "payload", "bytes")
	}
	if s.Type == "" && s.Data != "" {
		s.Type = "base64"
	}
	return nil
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
		raw, ok := rawJSONField(fields, name)
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

func idJSONField(fields map[string]json.RawMessage, names ...string) string {
	for _, name := range names {
		raw, ok := rawJSONField(fields, name)
		if !ok {
			continue
		}
		var value ID
		if err := json.Unmarshal(raw, &value); err == nil {
			return string(value)
		}
	}
	return ""
}

func idJSONFieldPtr(fields map[string]json.RawMessage, names ...string) *ID {
	if value := idJSONField(fields, names...); value != "" {
		id := ID(value)
		return &id
	}
	return nil
}

func boolJSONField(fields map[string]json.RawMessage, names ...string) (bool, bool) {
	for _, name := range names {
		raw, ok := rawJSONField(fields, name)
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
		raw, ok := rawJSONField(fields, name)
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

func rawJSONField(fields map[string]json.RawMessage, name string) (json.RawMessage, bool) {
	if raw, ok := fields[name]; ok {
		return raw, true
	}
	normalizedName := normalizedJSONFieldName(name)
	if normalizedName == "" {
		return nil, false
	}
	for field, raw := range fields {
		if normalizedJSONFieldName(field) == normalizedName {
			return raw, true
		}
	}
	return nil, false
}

func normalizedJSONFieldName(name string) string {
	name = strings.ToLower(strings.TrimSpace(name))
	name = strings.ReplaceAll(name, "_", "")
	name = strings.ReplaceAll(name, "-", "")
	name = strings.ReplaceAll(name, " ", "")
	return name
}

func canonicalContentBlockType(value string) ContentBlockType {
	trimmed := strings.TrimSpace(value)
	normalized := strings.ToLower(trimmed)
	normalized = strings.ReplaceAll(normalized, "-", "_")
	normalized = strings.ReplaceAll(normalized, " ", "_")
	switch normalized {
	case "":
		return ""
	case "text", "input_text", "inputtext", "output_text", "outputtext", "content_text", "contenttext":
		return ContentText
	case "thinking", "reasoning", "thought", "chain_of_thought", "chainofthought":
		return ContentThinking
	case "tool_use", "tooluse", "tool_call", "toolcall", "function_call", "functioncall":
		return ContentToolUse
	case "tool_result", "toolresult", "tool_response", "toolresponse", "tool_output", "tooloutput", "function_result", "functionresult":
		return ContentToolResult
	case "image", "input_image", "inputimage", "image_source", "imagesource":
		return ContentImage
	case "cache_edits", "cacheedits", "cache_edit", "cacheedit":
		return ContentCacheEdits
	default:
		return ContentBlockType(trimmed)
	}
}

func imageSourceJSONField(fields map[string]json.RawMessage, names ...string) *ImageSource {
	for _, name := range names {
		raw, ok := rawJSONField(fields, name)
		if !ok {
			continue
		}
		var source ImageSource
		if err := json.Unmarshal(raw, &source); err == nil && (source.Type != "" || source.MediaType != "" || source.Data != "") {
			return &source
		}
	}
	return nil
}

func imageSourceFromBlockFields(fields map[string]json.RawMessage) *ImageSource {
	source := ImageSource{
		Type: stringJSONField(fields, "sourceType", "source_type", "encoding", "format"),
		MediaType: stringJSONField(fields,
			"media_type",
			"mediaType",
			"mime_type",
			"mimeType",
			"content_type",
			"contentType",
			"mime",
			"media",
		),
		Data: stringJSONField(fields, "data", "base64", "content", "value", "payload", "bytes"),
	}
	if source.Type == "" && source.Data != "" {
		source.Type = "base64"
	}
	if source.Type == "" && source.MediaType == "" && source.Data == "" {
		return nil
	}
	return &source
}

func contentBlockTextJSONField(fields map[string]json.RawMessage, blockType ContentBlockType) string {
	if blockType != "" && blockType != ContentText && blockType != ContentThinking {
		return ""
	}
	if text := stringJSONField(fields, "body", "message", "value", "output", "contentText", "content_text"); text != "" {
		return text
	}
	if blockType == ContentText || blockType == ContentThinking {
		return stringJSONField(fields, "content")
	}
	return ""
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
	var aux struct {
		ID                     ID              `json:"id"`
		Type                   MessageType     `json:"type"`
		TypeCamel              string          `json:"messageType"`
		TypeSnake              string          `json:"message_type"`
		Role                   string          `json:"role"`
		UUID                   ID              `json:"uuid"`
		ParentUUID             *ID             `json:"parentUuid"`
		SessionID              ID              `json:"sessionId"`
		IsMeta                 bool            `json:"isMeta"`
		Timestamp              string          `json:"timestamp"`
		Content                json.RawMessage `json:"content"`
		Text                   json.RawMessage `json:"text"`
		Body                   json.RawMessage `json:"body"`
		MessageText            json.RawMessage `json:"message"`
		Value                  json.RawMessage `json:"value"`
		Output                 json.RawMessage `json:"output"`
		Subtype                string          `json:"subtype"`
		Model                  string          `json:"model"`
		Usage                  *Usage          `json:"usage"`
		Raw                    map[string]any  `json:"raw"`
		MessageID              ID              `json:"messageId"`
		MessageIDUpper         ID              `json:"messageID"`
		MessageIDSnake         ID              `json:"message_id"`
		MessageUUID            ID              `json:"messageUuid"`
		MessageUUIDUpper       ID              `json:"messageUUID"`
		MessageUUIDSnake       ID              `json:"message_uuid"`
		ParentUUIDUpper        *ID             `json:"parentUUID"`
		ParentUUIDSnake        *ID             `json:"parent_uuid"`
		ParentID               *ID             `json:"parentId"`
		ParentIDUpper          *ID             `json:"parentID"`
		ParentIDSnake          *ID             `json:"parent_id"`
		ParentMessageID        *ID             `json:"parentMessageId"`
		ParentMessageIDUpper   *ID             `json:"parentMessageID"`
		ParentMessageIDSnake   *ID             `json:"parent_message_id"`
		ParentMessageUUID      *ID             `json:"parentMessageUuid"`
		ParentMessageUUIDUpper *ID             `json:"parentMessageUUID"`
		ParentMessageUUIDSnake *ID             `json:"parent_message_uuid"`
		SessionIDUpper         ID              `json:"sessionID"`
		SessionIDSnake         ID              `json:"session_id"`
		SessionUUID            ID              `json:"sessionUuid"`
		SessionUUIDUpper       ID              `json:"sessionUUID"`
		SessionUUIDSnake       ID              `json:"session_uuid"`
		IsMetaSnake            *bool           `json:"is_meta"`
	}
	if err := json.Unmarshal(data, &aux); err != nil {
		return err
	}

	fields := map[string]json.RawMessage{}
	if err := json.Unmarshal(data, &fields); err != nil {
		return err
	}

	contentRaw := aux.Content
	if isEmptyJSONRaw(contentRaw) {
		if raw, ok := rawJSONField(fields, "content"); ok {
			contentRaw = raw
		} else if raw, ok := rawJSONField(fields, "messageContent"); ok {
			contentRaw = raw
		}
	}
	content, err := contentBlocksFromRaw(contentRaw)
	if err != nil {
		return err
	}
	if len(content) == 0 && isEmptyJSONRaw(contentRaw) {
		text := rawJSONFieldValue(fields, "text")
		body := rawJSONFieldValue(fields, "body")
		message := rawJSONFieldValue(fields, "message")
		value := rawJSONFieldValue(fields, "value")
		output := rawJSONFieldValue(fields, "output")
		messageText := rawJSONFieldValue(fields, "messageText")
		contentText := rawJSONFieldValue(fields, "contentText")
		content = textContentBlocksFromRaw(aux.Text, aux.Body, aux.MessageText, aux.Value, aux.Output, text, body, message, value, output, messageText, contentText)
	}
	*m = Message{
		ID:         string(aux.ID),
		Type:       aux.Type,
		UUID:       aux.UUID,
		ParentUUID: aux.ParentUUID,
		SessionID:  aux.SessionID,
		IsMeta:     aux.IsMeta,
		Timestamp:  aux.Timestamp,
		Content:    content,
		Subtype:    aux.Subtype,
		Model:      aux.Model,
		Usage:      aux.Usage,
		Raw:        aux.Raw,
	}
	if messageType := firstMessageType(string(m.Type), aux.TypeCamel, aux.TypeSnake, aux.Role); messageType != "" {
		m.Type = messageType
	}
	if messageType := firstMessageType(stringJSONField(fields, "type", "messageType", "message_type", "role")); m.Type == "" && messageType != "" {
		m.Type = messageType
	}
	if m.ID == "" {
		m.ID = string(firstMessageID(aux.MessageID, aux.MessageIDUpper, aux.MessageIDSnake, ID(idJSONField(fields, "id", "messageId", "messageID", "message_id"))))
	}
	if m.UUID == "" {
		m.UUID = firstMessageID(
			aux.MessageUUID,
			aux.MessageUUIDUpper,
			aux.MessageUUIDSnake,
			aux.MessageID,
			aux.MessageIDUpper,
			aux.MessageIDSnake,
			ID(idJSONField(fields, "uuid", "messageUuid", "messageUUID", "message_uuid", "messageId", "messageID", "message_id", "id")),
		)
	}
	if m.ParentUUID == nil {
		m.ParentUUID = firstMessageIDPtr(
			idJSONFieldPtr(fields, "parentUuid", "parentUUID", "parent_uuid", "parentId", "parentID", "parent_id", "parentMessageId", "parentMessageID", "parent_message_id", "parentMessageUuid", "parentMessageUUID", "parent_message_uuid"),
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
	if m.SessionID == "" {
		m.SessionID = firstMessageID(aux.SessionIDUpper, aux.SessionIDSnake, aux.SessionUUID, aux.SessionUUIDUpper, aux.SessionUUIDSnake, ID(idJSONField(fields, "sessionId", "sessionID", "session_id", "session", "sessionUuid", "sessionUUID", "session_uuid")))
	}
	if m.Timestamp == "" {
		m.Timestamp = stringJSONField(fields, "timestamp", "createdAt", "created_at", "time")
	}
	if aux.IsMetaSnake != nil {
		m.IsMeta = *aux.IsMetaSnake
	} else if value, ok := boolJSONField(fields, "isMeta", "is_meta"); ok {
		m.IsMeta = value
	}
	return nil
}

func rawJSONFieldValue(fields map[string]json.RawMessage, name string) json.RawMessage {
	if raw, ok := rawJSONField(fields, name); ok {
		return raw
	}
	return nil
}

func contentBlocksFromRaw(raw json.RawMessage) ([]ContentBlock, error) {
	raw = bytes.TrimSpace(raw)
	if isEmptyJSONRaw(raw) {
		return nil, nil
	}
	switch raw[0] {
	case '"':
		var text string
		if err := json.Unmarshal(raw, &text); err != nil {
			return nil, err
		}
		return []ContentBlock{NewTextBlock(text)}, nil
	case '{':
		var block ContentBlock
		if err := json.Unmarshal(raw, &block); err != nil {
			return nil, err
		}
		return []ContentBlock{block}, nil
	case '[':
		var items []json.RawMessage
		if err := json.Unmarshal(raw, &items); err != nil {
			return nil, err
		}
		blocks := make([]ContentBlock, 0, len(items))
		for _, item := range items {
			next, err := contentBlocksFromRaw(item)
			if err != nil {
				return nil, err
			}
			blocks = append(blocks, next...)
		}
		return blocks, nil
	default:
		var blocks []ContentBlock
		if err := json.Unmarshal(raw, &blocks); err != nil {
			return nil, err
		}
		return blocks, nil
	}
}

func textContentBlocksFromRaw(values ...json.RawMessage) []ContentBlock {
	for _, value := range values {
		value = bytes.TrimSpace(value)
		if isEmptyJSONRaw(value) || value[0] != '"' {
			continue
		}
		var text string
		if err := json.Unmarshal(value, &text); err == nil {
			return []ContentBlock{NewTextBlock(text)}
		}
	}
	return nil
}

func isEmptyJSONRaw(raw json.RawMessage) bool {
	raw = bytes.TrimSpace(raw)
	return len(raw) == 0 || bytes.Equal(raw, []byte("null"))
}

func firstMessageType(values ...string) MessageType {
	for _, value := range values {
		if messageType := CanonicalMessageType(value); messageType != "" {
			return messageType
		}
	}
	return ""
}

func CanonicalMessageType(value string) MessageType {
	normalized := strings.ToLower(strings.TrimSpace(value))
	if normalized == "" {
		return ""
	}
	compact := strings.NewReplacer("_", "", "-", "", " ", "", ".", "").Replace(normalized)
	switch compact {
	case "user", "usermessage", "messageuser", "userevent", "eventuser":
		return MessageUser
	case "assistant", "assistantmessage", "messageassistant", "assistantevent", "eventassistant":
		return MessageAssistant
	case "system", "systemmessage", "messagesystem", "systemevent", "eventsystem":
		return MessageSystem
	case "attachment", "attachmentmessage", "messageattachment", "attachmentevent", "eventattachment":
		return MessageAttachment
	case "progress", "progressmessage", "messageprogress", "progressevent", "eventprogress", "progressupdate", "updateprogress", "status", "statusmessage", "messagestatus", "statusevent", "eventstatus", "statusupdate", "updatestatus":
		return MessageProgress
	case "tombstone", "tombstonemessage", "messagetombstone", "tombstoneevent", "eventtombstone":
		return MessageTombstone
	default:
		return ""
	}
}

func firstMessageID(values ...ID) ID {
	for _, value := range values {
		if value != "" {
			return value
		}
	}
	return ""
}

func firstMessageIDPtr(values ...*ID) *ID {
	for _, value := range values {
		if value != nil && *value != "" {
			cloned := *value
			return &cloned
		}
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
