package session

import (
	"bytes"
	"encoding/json"
	"strconv"
	"strings"

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
	return f.idValue("sessionId", "sessionID", "session_id", "session", "sessionUuid", "sessionUUID", "session_uuid")
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
		var text string
		if err := json.Unmarshal(raw, &text); err == nil {
			value, err := strconv.Atoi(strings.TrimSpace(text))
			if err == nil {
				return value
			}
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
	leafUUID := fields.idValue(
		"leafUuid", "leafUUID", "leaf_uuid",
		"leafId", "leafID", "leaf_id",
		"messageUuid", "messageUUID", "message_uuid",
		"messageId", "messageID", "message_id",
		"uuid", "id",
	)
	return leafUUID, fields.stringValue("summary", "summaryContent", "summary_content", "content", "text", "body", "message"), true
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
	if entry.Summary == "" {
		entry.Summary = fields.stringValue("taskSummary", "task_summary", "content", "text", "body", "message")
	}
	if entry.Timestamp == "" {
		entry.Timestamp = fields.stringValue("timestamp", "createdAt", "created_at", "time")
	}
	return entry, true
}

func parsePRLinkMetadata(line []byte) (PRLinkEntry, bool) {
	fields, err := parseTranscriptMetadataFields(line)
	if err != nil {
		return PRLinkEntry{}, false
	}
	var entry PRLinkEntry
	_ = json.Unmarshal(line, &entry)
	if entry.SessionID == "" {
		entry.SessionID = fields.sessionIDValue()
	}
	if entry.PRNumber == 0 {
		entry.PRNumber = fields.intValue("prNumber", "pr_number", "pullRequestNumber", "pullRequestID", "pull_request_number", "pull_request_id", "number")
	}
	if entry.PRURL == "" {
		entry.PRURL = fields.stringValue("prUrl", "prURL", "pr_url", "pullRequestUrl", "pullRequestURL", "pull_request_url", "url", "href")
	}
	if entry.PRRepository == "" {
		entry.PRRepository = fields.stringValue("prRepository", "pr_repository", "repositoryFullName", "repository_full_name", "repoFullName", "repo_full_name", "repository", "repo")
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
		entry.WorktreeSession = fields.rawValue("worktree_session", "worktreeState", "worktree_state", "worktree", "workspace")
	}
	return entry, true
}

func parseSnapshotMessageID(line []byte) contracts.ID {
	fields, err := parseTranscriptMetadataFields(line)
	if err != nil {
		return ""
	}
	return fields.idValue("messageId", "messageID", "message_id", "messageUuid", "messageUUID", "message_uuid", "uuid", "id")
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
		entry.AgentID = fields.stringValue(
			"agentID", "agent_id", "agent",
			"sidechainId", "sidechainID", "sidechain_id",
			"subagentId", "subagentID", "subagent_id",
		)
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
		entry.TargetUUID = fields.idValue(
			"targetUuid", "targetUUID", "target_uuid",
			"targetId", "targetID", "target_id",
			"deletedUuid", "deletedUUID", "deleted_uuid",
			"deletedId", "deletedID", "deleted_id",
			"messageUuid", "messageUUID", "message_uuid",
			"messageId", "messageID", "message_id",
		)
	}
	if entry.TargetUUID == "" {
		entry.TargetUUID = entry.UUID
	}
	if entry.ParentUUID == nil {
		if parent := fields.idValue(
			"parentUuid", "parentUUID", "parent_uuid",
			"parentId", "parentID", "parent_id",
			"parentMessageUuid", "parentMessageUUID", "parent_message_uuid",
			"parentMessageId", "parentMessageID", "parent_message_id",
		); parent != "" {
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
	fields, err := parseTranscriptMetadataFields(line)
	if err != nil {
		return SpeculationAcceptEntry{}, false
	}
	var entry SpeculationAcceptEntry
	_ = json.Unmarshal(line, &entry)
	if entry.TimeSavedMS == 0 {
		entry.TimeSavedMS = fields.intValue("timeSavedMs", "timeSavedMS", "time_saved_ms", "timeSaved", "time_saved")
	}
	if entry.Timestamp == "" {
		entry.Timestamp = fields.stringValue("timestamp", "createdAt", "created_at", "time")
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
		entry.CollapseID = fields.stringValue("collapseId", "collapseID", "collapse_id", "id")
	}
	if entry.SummaryUUID == "" {
		entry.SummaryUUID = fields.stringValue("summaryUuid", "summaryUUID", "summary_uuid", "summaryId", "summaryID", "summary_id")
	}
	if entry.SummaryContent == "" {
		entry.SummaryContent = fields.stringValue("summaryContent", "summary_content", "content")
	}
	if entry.FirstArchivedUUID == "" {
		entry.FirstArchivedUUID = fields.stringValue("firstArchivedUuid", "firstArchivedUUID", "first_archived_uuid", "firstArchivedId", "firstArchivedID", "first_archived_id")
	}
	if entry.LastArchivedUUID == "" {
		entry.LastArchivedUUID = fields.stringValue("lastArchivedUuid", "lastArchivedUUID", "last_archived_uuid", "lastArchivedId", "lastArchivedID", "last_archived_id")
	}
	return entry, true
}

func parseContextCollapseSnapshotMetadata(line []byte) (ContextCollapseSnapshotEntry, bool) {
	fields, err := parseTranscriptMetadataFields(line)
	if err != nil {
		return ContextCollapseSnapshotEntry{}, false
	}
	var entry ContextCollapseSnapshotEntry
	_ = json.Unmarshal(line, &entry)
	if entry.SessionID == "" {
		entry.SessionID = fields.sessionIDValue()
	}
	if entry.LastSpawnTokens == 0 {
		entry.LastSpawnTokens = fields.intValue("lastSpawnTokens", "last_spawn_tokens")
	}
	return entry, true
}
