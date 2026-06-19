package session

import (
	"encoding/json"

	"ccgo/internal/contracts"
)

func (m *TranscriptMessage) UnmarshalJSON(data []byte) error {
	type TranscriptMessageJSON TranscriptMessage
	var aux struct {
		*TranscriptMessageJSON
		EntryType              string           `json:"entryType"`
		EntryTypeSnake         string           `json:"entry_type"`
		MessageType            string           `json:"messageType"`
		MessageTypeSnake       string           `json:"message_type"`
		Role                   string           `json:"role"`
		ID                     contracts.ID     `json:"id"`
		MessageID              contracts.ID     `json:"messageId"`
		MessageIDSnake         contracts.ID     `json:"message_id"`
		MessageUUID            contracts.ID     `json:"messageUuid"`
		MessageUUIDUpper       contracts.ID     `json:"messageUUID"`
		MessageUUIDSnake       contracts.ID     `json:"message_uuid"`
		ParentUUIDSnake        *contracts.ID    `json:"parent_uuid"`
		ParentID               *contracts.ID    `json:"parentId"`
		ParentIDSnake          *contracts.ID    `json:"parent_id"`
		ParentMessageID        *contracts.ID    `json:"parentMessageId"`
		ParentMessageIDUpper   *contracts.ID    `json:"parentMessageID"`
		ParentMessageIDSnake   *contracts.ID    `json:"parent_message_id"`
		ParentMessageUUID      *contracts.ID    `json:"parentMessageUuid"`
		ParentMessageUUIDUpper *contracts.ID    `json:"parentMessageUUID"`
		ParentMessageUUIDSnake *contracts.ID    `json:"parent_message_uuid"`
		LogicalParentUUIDSnake *contracts.ID    `json:"logical_parent_uuid"`
		SessionIDUpper         contracts.ID     `json:"sessionID"`
		SessionIDSnake         contracts.ID     `json:"session_id"`
		SessionUUID            contracts.ID     `json:"sessionUuid"`
		SessionUUIDUpper       contracts.ID     `json:"sessionUUID"`
		SessionUUIDSnake       contracts.ID     `json:"session_uuid"`
		CreatedAt              string           `json:"createdAt"`
		CreatedAtSnake         string           `json:"created_at"`
		Time                   string           `json:"time"`
		IsSidechainSnake       *bool            `json:"is_sidechain"`
		AgentIDSnake           string           `json:"agent_id"`
		CWDSnake               string           `json:"cwd_path"`
		WorkingDirectory       string           `json:"workingDirectory"`
		WorkingDirectorySnake  string           `json:"working_directory"`
		ProjectPath            string           `json:"projectPath"`
		ProjectPathSnake       string           `json:"project_path"`
		UserTypeSnake          string           `json:"user_type"`
		UserKind               string           `json:"userKind"`
		UserKindSnake          string           `json:"user_kind"`
		EntryPoint             string           `json:"entryPoint"`
		EntryPointSnake        string           `json:"entry_point"`
		Client                 string           `json:"client"`
		Source                 string           `json:"source"`
		AppVersion             string           `json:"appVersion"`
		AppVersionSnake        string           `json:"app_version"`
		ClaudeCodeVersion      string           `json:"claudeCodeVersion"`
		ClaudeCodeVersionSnake string           `json:"claude_code_version"`
		SessionSlug            string           `json:"sessionSlug"`
		SessionSlugSnake       string           `json:"session_slug"`
		PlanSlug               string           `json:"planSlug"`
		PlanSlugSnake          string           `json:"plan_slug"`
		GitBranchSnake         string           `json:"git_branch"`
		Branch                 string           `json:"branch"`
		CompactMetadataSnake   *CompactMetadata `json:"compact_metadata"`
		SnipMetadataSnake      *SnipMetadata    `json:"snip_metadata"`
	}
	base := TranscriptMessageJSON{}
	aux.TranscriptMessageJSON = &base
	if err := json.Unmarshal(data, &aux); err != nil {
		return err
	}
	fields := transcriptAliasFields(data)
	*m = TranscriptMessage(base)
	if messageType := firstTranscriptType(m.Type, aux.EntryType, aux.EntryTypeSnake, aux.MessageType, aux.MessageTypeSnake, aux.Role, transcriptAliasString(fields, "type", "entryType", "entry_type", "messageType", "message_type", "role")); messageType != "" {
		m.Type = messageType
	}
	if m.UUID == "" {
		m.UUID = firstTranscriptID(aux.MessageUUID, aux.MessageUUIDUpper, aux.MessageUUIDSnake, aux.MessageID, aux.MessageIDSnake, aux.ID, transcriptAliasID(fields, "uuid", "messageUuid", "messageUUID", "message_uuid", "messageId", "messageID", "message_id", "id"))
	}
	if m.ParentUUID == nil {
		m.ParentUUID = firstTranscriptIDPtr(
			transcriptAliasIDPtr(fields, "parentUuid", "parentUUID", "parent_uuid", "parentId", "parentID", "parent_id", "parentMessageUuid", "parentMessageUUID", "parent_message_uuid", "parentMessageId", "parentMessageID", "parent_message_id"),
			aux.ParentUUIDSnake,
			aux.ParentMessageUUID,
			aux.ParentMessageUUIDUpper,
			aux.ParentMessageUUIDSnake,
			aux.ParentMessageID,
			aux.ParentMessageIDUpper,
			aux.ParentMessageIDSnake,
			aux.ParentID,
			aux.ParentIDSnake,
		)
	}
	if m.LogicalParentUUID == nil {
		m.LogicalParentUUID = firstTranscriptIDPtr(transcriptAliasIDPtr(fields, "logicalParentUuid", "logicalParentUUID", "logical_parent_uuid", "logicalParentId", "logicalParentID", "logical_parent_id"), aux.LogicalParentUUIDSnake)
	}
	if m.SessionID == "" {
		m.SessionID = firstTranscriptID(aux.SessionIDUpper, aux.SessionIDSnake, aux.SessionUUID, aux.SessionUUIDUpper, aux.SessionUUIDSnake, transcriptAliasID(fields, "sessionId", "sessionID", "session_id", "session", "sessionUuid", "sessionUUID", "session_uuid"))
	}
	if m.Timestamp == "" {
		m.Timestamp = firstTranscriptString(aux.CreatedAt, aux.CreatedAtSnake, aux.Time, transcriptAliasString(fields, "timestamp", "createdAt", "created_at", "time"))
	}
	if aux.IsSidechainSnake != nil {
		m.IsSidechain = *aux.IsSidechainSnake
	} else if value, ok := transcriptAliasBool(fields, "isSidechain", "is_sidechain"); ok {
		m.IsSidechain = value
	}
	if m.AgentID == "" {
		m.AgentID = firstTranscriptString(aux.AgentIDSnake, transcriptAliasString(fields, "agentId", "agentID", "agent_id"))
	}
	if m.CWD == "" {
		m.CWD = firstTranscriptString(aux.CWDSnake, aux.WorkingDirectory, aux.WorkingDirectorySnake, aux.ProjectPath, aux.ProjectPathSnake, transcriptAliasString(fields, "cwd", "cwdPath", "cwd_path", "workingDirectory", "working_directory", "projectPath", "project_path"))
	}
	if m.UserType == "" {
		m.UserType = firstTranscriptString(aux.UserTypeSnake, aux.UserKind, aux.UserKindSnake, transcriptAliasString(fields, "userType", "user_type", "userKind", "user_kind"))
	}
	if m.Entrypoint == "" {
		m.Entrypoint = firstTranscriptString(aux.EntryPoint, aux.EntryPointSnake, aux.Client, aux.Source, transcriptAliasString(fields, "entrypoint", "entryPoint", "entry_point", "client", "source"))
	}
	if m.Version == "" {
		m.Version = firstTranscriptString(aux.AppVersion, aux.AppVersionSnake, aux.ClaudeCodeVersion, aux.ClaudeCodeVersionSnake, transcriptAliasString(fields, "version", "appVersion", "app_version", "claudeCodeVersion", "claude_code_version"))
	}
	if m.Slug == "" {
		m.Slug = firstTranscriptString(aux.SessionSlug, aux.SessionSlugSnake, aux.PlanSlug, aux.PlanSlugSnake, transcriptAliasString(fields, "slug", "sessionSlug", "session_slug", "planSlug", "plan_slug"))
	}
	if m.GitBranch == "" {
		m.GitBranch = firstTranscriptString(aux.GitBranchSnake, aux.Branch, transcriptAliasString(fields, "gitBranch", "git_branch", "branch"))
	}
	if m.CompactMetadata == nil {
		m.CompactMetadata = aux.CompactMetadataSnake
		if m.CompactMetadata == nil {
			m.CompactMetadata = transcriptAliasObject[CompactMetadata](fields, "compactMetadata", "compact_metadata")
		}
	}
	if m.SnipMetadata == nil {
		m.SnipMetadata = aux.SnipMetadataSnake
		if m.SnipMetadata == nil {
			m.SnipMetadata = transcriptAliasObject[SnipMetadata](fields, "snipMetadata", "snip_metadata")
		}
	}
	return nil
}

func (e *transcriptEnvelope) UnmarshalJSON(data []byte) error {
	type TranscriptEnvelopeJSON transcriptEnvelope
	var aux struct {
		*TranscriptEnvelopeJSON
		EntryType              string        `json:"entryType"`
		EntryTypeSnake         string        `json:"entry_type"`
		MessageType            string        `json:"messageType"`
		MessageTypeSnake       string        `json:"message_type"`
		Role                   string        `json:"role"`
		ID                     contracts.ID  `json:"id"`
		MessageID              contracts.ID  `json:"messageId"`
		MessageIDSnake         contracts.ID  `json:"message_id"`
		MessageUUID            contracts.ID  `json:"messageUuid"`
		MessageUUIDUpper       contracts.ID  `json:"messageUUID"`
		MessageUUIDSnake       contracts.ID  `json:"message_uuid"`
		ParentUUIDSnake        *contracts.ID `json:"parent_uuid"`
		ParentID               *contracts.ID `json:"parentId"`
		ParentIDSnake          *contracts.ID `json:"parent_id"`
		ParentMessageID        *contracts.ID `json:"parentMessageId"`
		ParentMessageIDUpper   *contracts.ID `json:"parentMessageID"`
		ParentMessageIDSnake   *contracts.ID `json:"parent_message_id"`
		ParentMessageUUID      *contracts.ID `json:"parentMessageUuid"`
		ParentMessageUUIDUpper *contracts.ID `json:"parentMessageUUID"`
		ParentMessageUUIDSnake *contracts.ID `json:"parent_message_uuid"`
	}
	base := TranscriptEnvelopeJSON{}
	aux.TranscriptEnvelopeJSON = &base
	if err := json.Unmarshal(data, &aux); err != nil {
		return err
	}
	fields := transcriptAliasFields(data)
	*e = transcriptEnvelope(base)
	if e.Type == "" {
		e.Type = firstTranscriptType(aux.EntryType, aux.EntryTypeSnake, aux.MessageType, aux.MessageTypeSnake, aux.Role, transcriptAliasString(fields, "type", "entryType", "entry_type", "messageType", "message_type", "role"))
	}
	if e.UUID == "" {
		e.UUID = firstTranscriptID(aux.MessageUUID, aux.MessageUUIDUpper, aux.MessageUUIDSnake, aux.MessageID, aux.MessageIDSnake, aux.ID, transcriptAliasID(fields, "uuid", "messageUuid", "messageUUID", "message_uuid", "messageId", "messageID", "message_id", "id"))
	}
	if e.ParentUUID == nil {
		e.ParentUUID = firstTranscriptIDPtr(
			transcriptAliasIDPtr(fields, "parentUuid", "parentUUID", "parent_uuid", "parentId", "parentID", "parent_id", "parentMessageUuid", "parentMessageUUID", "parent_message_uuid", "parentMessageId", "parentMessageID", "parent_message_id"),
			aux.ParentUUIDSnake,
			aux.ParentMessageUUID,
			aux.ParentMessageUUIDUpper,
			aux.ParentMessageUUIDSnake,
			aux.ParentMessageID,
			aux.ParentMessageIDUpper,
			aux.ParentMessageIDSnake,
			aux.ParentID,
			aux.ParentIDSnake,
		)
	}
	return nil
}

func transcriptAliasFields(data []byte) transcriptMetadataFields {
	var fields transcriptMetadataFields
	if err := json.Unmarshal(data, &fields); err != nil {
		return nil
	}
	return fields
}

func transcriptAliasString(fields transcriptMetadataFields, names ...string) string {
	for _, name := range names {
		raw, ok := transcriptMetadataFieldRaw(fields, name)
		if !ok || isNullJSON(raw) {
			continue
		}
		var value string
		if err := json.Unmarshal(raw, &value); err == nil {
			return value
		}
	}
	return ""
}

func transcriptAliasID(fields transcriptMetadataFields, names ...string) contracts.ID {
	for _, name := range names {
		raw, ok := transcriptMetadataFieldRaw(fields, name)
		if !ok || isNullJSON(raw) {
			continue
		}
		var value contracts.ID
		if err := json.Unmarshal(raw, &value); err == nil {
			return value
		}
	}
	return ""
}

func transcriptAliasIDPtr(fields transcriptMetadataFields, names ...string) *contracts.ID {
	if value := transcriptAliasID(fields, names...); value != "" {
		return &value
	}
	return nil
}

func transcriptAliasBool(fields transcriptMetadataFields, names ...string) (bool, bool) {
	for _, name := range names {
		raw, ok := transcriptMetadataFieldRaw(fields, name)
		if !ok || isNullJSON(raw) {
			continue
		}
		var value bool
		if err := json.Unmarshal(raw, &value); err == nil {
			return value, true
		}
	}
	return false, false
}

func transcriptAliasObject[T any](fields transcriptMetadataFields, names ...string) *T {
	for _, name := range names {
		raw, ok := transcriptMetadataFieldRaw(fields, name)
		if !ok || isNullJSON(raw) {
			continue
		}
		var value T
		if err := json.Unmarshal(raw, &value); err == nil {
			return &value
		}
	}
	return nil
}

func firstTranscriptType(values ...string) string {
	for _, value := range values {
		switch contracts.CanonicalMessageType(value) {
		case contracts.MessageUser:
			return string(contracts.MessageUser)
		case contracts.MessageAssistant:
			return string(contracts.MessageAssistant)
		case contracts.MessageAttachment:
			return string(contracts.MessageAttachment)
		case contracts.MessageSystem:
			return string(contracts.MessageSystem)
		case contracts.MessageProgress:
			return string(contracts.MessageProgress)
		case contracts.MessageTombstone:
			return string(contracts.MessageTombstone)
		}
	}
	return ""
}

func firstTranscriptString(values ...string) string {
	for _, value := range values {
		if value != "" {
			return value
		}
	}
	return ""
}

func firstTranscriptID(values ...contracts.ID) contracts.ID {
	for _, value := range values {
		if value != "" {
			return value
		}
	}
	return ""
}

func firstTranscriptIDPtr(values ...*contracts.ID) *contracts.ID {
	for _, value := range values {
		if value != nil && *value != "" {
			return cloneIDPtr(value)
		}
	}
	return nil
}

func (m *CompactMetadata) UnmarshalJSON(data []byte) error {
	type CompactMetadataJSON CompactMetadata
	var aux struct {
		*CompactMetadataJSON
		PreTokensSnake                 int               `json:"pre_tokens"`
		UserContextSnake               string            `json:"user_context"`
		MessagesSummarizedSnake        int               `json:"messages_summarized"`
		PreCompactDiscoveredToolsSnake []string          `json:"pre_compact_discovered_tools"`
		PreservedSegmentSnake          *PreservedSegment `json:"preserved_segment"`
	}
	base := CompactMetadataJSON{}
	aux.CompactMetadataJSON = &base
	if err := json.Unmarshal(data, &aux); err != nil {
		return err
	}
	*m = CompactMetadata(base)
	if m.PreTokens == 0 {
		m.PreTokens = aux.PreTokensSnake
	}
	if m.UserContext == "" {
		m.UserContext = aux.UserContextSnake
	}
	if m.MessagesSummarized == 0 {
		m.MessagesSummarized = aux.MessagesSummarizedSnake
	}
	if len(m.PreCompactDiscoveredTools) == 0 {
		m.PreCompactDiscoveredTools = append([]string(nil), aux.PreCompactDiscoveredToolsSnake...)
	}
	if m.PreservedSegment == nil {
		m.PreservedSegment = aux.PreservedSegmentSnake
	}
	return nil
}

func (s *PreservedSegment) UnmarshalJSON(data []byte) error {
	type PreservedSegmentJSON PreservedSegment
	var aux struct {
		*PreservedSegmentJSON
		HeadUUIDSnake   contracts.ID `json:"head_uuid"`
		TailUUIDSnake   contracts.ID `json:"tail_uuid"`
		AnchorUUIDSnake contracts.ID `json:"anchor_uuid"`
	}
	base := PreservedSegmentJSON{}
	aux.PreservedSegmentJSON = &base
	if err := json.Unmarshal(data, &aux); err != nil {
		return err
	}
	*s = PreservedSegment(base)
	if s.HeadUUID == "" {
		s.HeadUUID = aux.HeadUUIDSnake
	}
	if s.TailUUID == "" {
		s.TailUUID = aux.TailUUIDSnake
	}
	if s.AnchorUUID == "" {
		s.AnchorUUID = aux.AnchorUUIDSnake
	}
	return nil
}

func (m *SnipMetadata) UnmarshalJSON(data []byte) error {
	type SnipMetadataJSON SnipMetadata
	var aux struct {
		*SnipMetadataJSON
		RemovedUUIDsSnake []contracts.ID `json:"removed_uuids"`
	}
	base := SnipMetadataJSON{}
	aux.SnipMetadataJSON = &base
	if err := json.Unmarshal(data, &aux); err != nil {
		return err
	}
	*m = SnipMetadata(base)
	if len(m.RemovedUUIDs) == 0 {
		m.RemovedUUIDs = aux.RemovedUUIDsSnake
	}
	return nil
}

func (r *ContentReplacementRecord) UnmarshalJSON(data []byte) error {
	var aux struct {
		KindType                 string       `json:"type"`
		Kind                     string       `json:"kind"`
		KindReplacementCamel     string       `json:"replacementKind"`
		KindReplacementSnake     string       `json:"replacement_kind"`
		ToolUseID                contracts.ID `json:"toolUseId"`
		ToolUseIDUpper           contracts.ID `json:"toolUseID"`
		ToolUseIDSnake           contracts.ID `json:"tool_use_id"`
		BlockID                  contracts.ID `json:"blockId"`
		BlockIDUpper             contracts.ID `json:"blockID"`
		BlockIDSnake             contracts.ID `json:"block_id"`
		Replacement              string       `json:"replacement"`
		ReplacementContent       string       `json:"content"`
		ReplacementText          string       `json:"text"`
		ReplacementValue         string       `json:"value"`
		ReplacementOutput        string       `json:"output"`
		OriginalHash             string       `json:"originalHash"`
		OriginalHashSnake        string       `json:"original_hash"`
		OriginalHashShort        string       `json:"hash"`
		OriginalContentHashCamel string       `json:"originalContentHash"`
		OriginalContentHashSnake string       `json:"original_content_hash"`
	}
	if err := json.Unmarshal(data, &aux); err != nil {
		return err
	}
	r.Kind = firstNonEmptyString(aux.Kind, aux.KindType, aux.KindReplacementCamel, aux.KindReplacementSnake)
	r.ToolUseID = firstNonEmptyString(string(aux.ToolUseID), string(aux.ToolUseIDUpper), string(aux.ToolUseIDSnake))
	r.BlockID = firstNonEmptyString(string(aux.BlockID), string(aux.BlockIDUpper), string(aux.BlockIDSnake))
	r.Replacement = firstNonEmptyString(aux.Replacement, aux.ReplacementContent, aux.ReplacementText, aux.ReplacementValue, aux.ReplacementOutput)
	r.OriginalHash = firstNonEmptyString(aux.OriginalHash, aux.OriginalHashSnake, aux.OriginalHashShort, aux.OriginalContentHashCamel, aux.OriginalContentHashSnake)
	return nil
}

func firstNonEmptyString(values ...string) string {
	for _, value := range values {
		if value != "" {
			return value
		}
	}
	return ""
}
