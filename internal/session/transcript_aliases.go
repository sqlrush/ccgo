package session

import (
	"encoding/json"

	"ccgo/internal/contracts"
)

func (m *TranscriptMessage) UnmarshalJSON(data []byte) error {
	type TranscriptMessageJSON TranscriptMessage
	var aux struct {
		*TranscriptMessageJSON
		ParentUUIDSnake        *contracts.ID    `json:"parent_uuid"`
		LogicalParentUUIDSnake *contracts.ID    `json:"logical_parent_uuid"`
		SessionIDSnake         contracts.ID     `json:"session_id"`
		IsSidechainSnake       *bool            `json:"is_sidechain"`
		AgentIDSnake           string           `json:"agent_id"`
		CompactMetadataSnake   *CompactMetadata `json:"compact_metadata"`
		SnipMetadataSnake      *SnipMetadata    `json:"snip_metadata"`
	}
	base := TranscriptMessageJSON{}
	aux.TranscriptMessageJSON = &base
	if err := json.Unmarshal(data, &aux); err != nil {
		return err
	}
	*m = TranscriptMessage(base)
	if m.ParentUUID == nil {
		m.ParentUUID = cloneIDPtr(aux.ParentUUIDSnake)
	}
	if m.LogicalParentUUID == nil {
		m.LogicalParentUUID = cloneIDPtr(aux.LogicalParentUUIDSnake)
	}
	if m.SessionID == "" {
		m.SessionID = aux.SessionIDSnake
	}
	if aux.IsSidechainSnake != nil {
		m.IsSidechain = *aux.IsSidechainSnake
	}
	if m.AgentID == "" {
		m.AgentID = aux.AgentIDSnake
	}
	if m.CompactMetadata == nil {
		m.CompactMetadata = aux.CompactMetadataSnake
	}
	if m.SnipMetadata == nil {
		m.SnipMetadata = aux.SnipMetadataSnake
	}
	return nil
}

func (e *transcriptEnvelope) UnmarshalJSON(data []byte) error {
	type TranscriptEnvelopeJSON transcriptEnvelope
	var aux struct {
		*TranscriptEnvelopeJSON
		ParentUUIDSnake *contracts.ID `json:"parent_uuid"`
	}
	base := TranscriptEnvelopeJSON{}
	aux.TranscriptEnvelopeJSON = &base
	if err := json.Unmarshal(data, &aux); err != nil {
		return err
	}
	*e = transcriptEnvelope(base)
	if e.ParentUUID == nil {
		e.ParentUUID = cloneIDPtr(aux.ParentUUIDSnake)
	}
	return nil
}

func (m *CompactMetadata) UnmarshalJSON(data []byte) error {
	type CompactMetadataJSON CompactMetadata
	var aux struct {
		*CompactMetadataJSON
		PreTokensSnake          int               `json:"pre_tokens"`
		UserContextSnake        string            `json:"user_context"`
		MessagesSummarizedSnake int               `json:"messages_summarized"`
		PreservedSegmentSnake   *PreservedSegment `json:"preserved_segment"`
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
