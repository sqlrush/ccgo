package session

import (
	"bytes"
	"encoding/json"

	"ccgo/internal/contracts"
)

type transcriptMetadataFields map[string]json.RawMessage

func parseTranscriptMetadataFields(line []byte) (transcriptMetadataFields, error) {
	var fields transcriptMetadataFields
	if err := json.Unmarshal(line, &fields); err != nil {
		return nil, err
	}
	return fields, nil
}

func (f transcriptMetadataFields) stringValue(keys ...string) string {
	for _, key := range keys {
		raw, ok := f[key]
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

func (f transcriptMetadataFields) idValue(keys ...string) contracts.ID {
	return contracts.ID(f.stringValue(keys...))
}

func (f transcriptMetadataFields) sessionIDValue() contracts.ID {
	return f.idValue("sessionId", "session_id", "sessionUuid", "sessionUUID", "session_uuid")
}

func (f transcriptMetadataFields) intValue(keys ...string) int {
	for _, key := range keys {
		raw, ok := f[key]
		if !ok || isNullJSON(raw) {
			continue
		}
		var value int
		if err := json.Unmarshal(raw, &value); err == nil {
			return value
		}
	}
	return 0
}

func (f transcriptMetadataFields) rawValue(keys ...string) json.RawMessage {
	for _, key := range keys {
		raw, ok := f[key]
		if !ok || isNullJSON(raw) {
			continue
		}
		return append(json.RawMessage(nil), raw...)
	}
	return nil
}

func isNullJSON(raw json.RawMessage) bool {
	return len(bytes.TrimSpace(raw)) == 0 || bytes.Equal(bytes.TrimSpace(raw), []byte("null"))
}

func parseSummaryMetadata(line []byte) (contracts.ID, string, bool) {
	fields, err := parseTranscriptMetadataFields(line)
	if err != nil {
		return "", "", false
	}
	leafUUID := fields.idValue("leafUuid", "leaf_uuid")
	return leafUUID, fields.stringValue("summary"), true
}

func parseSessionStringMetadata(line []byte, valueKeys ...string) (contracts.ID, string, bool) {
	fields, err := parseTranscriptMetadataFields(line)
	if err != nil {
		return "", "", false
	}
	sessionID := fields.sessionIDValue()
	return sessionID, fields.stringValue(valueKeys...), true
}

func parseTaskSummaryMetadata(line []byte) (TaskSummaryEntry, bool) {
	var entry TaskSummaryEntry
	if err := json.Unmarshal(line, &entry); err != nil {
		return TaskSummaryEntry{}, false
	}
	fields, err := parseTranscriptMetadataFields(line)
	if err != nil {
		return TaskSummaryEntry{}, false
	}
	if entry.SessionID == "" {
		entry.SessionID = fields.sessionIDValue()
	}
	return entry, true
}

func parsePRLinkMetadata(line []byte) (PRLinkEntry, bool) {
	var entry PRLinkEntry
	if err := json.Unmarshal(line, &entry); err != nil {
		return PRLinkEntry{}, false
	}
	fields, err := parseTranscriptMetadataFields(line)
	if err != nil {
		return PRLinkEntry{}, false
	}
	if entry.SessionID == "" {
		entry.SessionID = fields.sessionIDValue()
	}
	if entry.PRNumber == 0 {
		entry.PRNumber = fields.intValue("pr_number")
	}
	if entry.PRURL == "" {
		entry.PRURL = fields.stringValue("pr_url")
	}
	if entry.PRRepository == "" {
		entry.PRRepository = fields.stringValue("pr_repository")
	}
	return entry, true
}

func parseWorktreeStateMetadata(line []byte) (WorktreeStateEntry, bool) {
	var entry WorktreeStateEntry
	if err := json.Unmarshal(line, &entry); err != nil {
		return WorktreeStateEntry{}, false
	}
	fields, err := parseTranscriptMetadataFields(line)
	if err != nil {
		return WorktreeStateEntry{}, false
	}
	if entry.SessionID == "" {
		entry.SessionID = fields.sessionIDValue()
	}
	if len(entry.WorktreeSession) == 0 {
		entry.WorktreeSession = fields.rawValue("worktree_session")
	}
	return entry, true
}

func parseContentReplacementMetadata(line []byte) (ContentReplacementEntry, bool) {
	var entry ContentReplacementEntry
	if err := json.Unmarshal(line, &entry); err != nil {
		return ContentReplacementEntry{}, false
	}
	fields, err := parseTranscriptMetadataFields(line)
	if err != nil {
		return ContentReplacementEntry{}, false
	}
	if entry.SessionID == "" {
		entry.SessionID = fields.sessionIDValue()
	}
	if entry.AgentID == "" {
		entry.AgentID = fields.stringValue("agent_id")
	}
	return entry, true
}

func parseTombstoneMetadata(line []byte) (TombstoneEntry, bool) {
	var entry TombstoneEntry
	if err := json.Unmarshal(line, &entry); err != nil {
		return TombstoneEntry{}, false
	}
	fields, err := parseTranscriptMetadataFields(line)
	if err != nil {
		return TombstoneEntry{}, false
	}
	if entry.TargetUUID == "" {
		entry.TargetUUID = fields.idValue("targetUuid", "targetUUID", "target_uuid", "deletedUuid", "deletedUUID", "deleted_uuid", "messageUuid", "messageUUID", "message_uuid")
	}
	if entry.TargetUUID == "" {
		entry.TargetUUID = entry.UUID
	}
	if entry.ParentUUID == nil {
		if parent := fields.idValue("parentUuid", "parentUUID", "parent_uuid"); parent != "" {
			entry.ParentUUID = &parent
		}
	}
	if entry.SessionID == "" {
		entry.SessionID = fields.sessionIDValue()
	}
	if entry.Reason == "" {
		entry.Reason = fields.stringValue("deletedReason", "deleted_reason")
	}
	return entry, true
}

func parseSpeculationAcceptMetadata(line []byte) (SpeculationAcceptEntry, bool) {
	var entry SpeculationAcceptEntry
	if err := json.Unmarshal(line, &entry); err != nil {
		return SpeculationAcceptEntry{}, false
	}
	fields, err := parseTranscriptMetadataFields(line)
	if err != nil {
		return SpeculationAcceptEntry{}, false
	}
	if entry.TimeSavedMS == 0 {
		entry.TimeSavedMS = fields.intValue("time_saved_ms")
	}
	return entry, true
}

func parseContextCollapseCommitMetadata(line []byte) (ContextCollapseCommitEntry, bool) {
	var entry ContextCollapseCommitEntry
	if err := json.Unmarshal(line, &entry); err != nil {
		return ContextCollapseCommitEntry{}, false
	}
	fields, err := parseTranscriptMetadataFields(line)
	if err != nil {
		return ContextCollapseCommitEntry{}, false
	}
	if entry.SessionID == "" {
		entry.SessionID = fields.sessionIDValue()
	}
	if entry.CollapseID == "" {
		entry.CollapseID = fields.stringValue("collapse_id")
	}
	if entry.SummaryUUID == "" {
		entry.SummaryUUID = fields.stringValue("summary_uuid")
	}
	if entry.SummaryContent == "" {
		entry.SummaryContent = fields.stringValue("summary_content")
	}
	if entry.FirstArchivedUUID == "" {
		entry.FirstArchivedUUID = fields.stringValue("first_archived_uuid")
	}
	if entry.LastArchivedUUID == "" {
		entry.LastArchivedUUID = fields.stringValue("last_archived_uuid")
	}
	return entry, true
}

func parseContextCollapseSnapshotMetadata(line []byte) (ContextCollapseSnapshotEntry, bool) {
	var entry ContextCollapseSnapshotEntry
	if err := json.Unmarshal(line, &entry); err != nil {
		return ContextCollapseSnapshotEntry{}, false
	}
	fields, err := parseTranscriptMetadataFields(line)
	if err != nil {
		return ContextCollapseSnapshotEntry{}, false
	}
	if entry.SessionID == "" {
		entry.SessionID = fields.sessionIDValue()
	}
	if entry.LastSpawnTokens == 0 {
		entry.LastSpawnTokens = fields.intValue("last_spawn_tokens")
	}
	return entry, true
}
