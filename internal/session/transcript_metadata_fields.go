package session

import (
	"bytes"
	"encoding/json"
	"strconv"
	"strings"

	"ccgo/internal/contracts"
)

type transcriptMetadataFields map[string]json.RawMessage

var transcriptMetadataFieldWrapperKeys = []string{
	"payload",
	"data",
	"body",
	"content",
	"result",
	"response",
	"record",
	"entry",
	"item",
	"event",
	"message",
	"resource",
	"node",
	"edge",
	"attributes",
	"properties",
	"attrs",
	"metadata",
	"meta",
	"value",
	"object",
	"included",
	"resources",
	"records",
	"entries",
	"items",
	"nodes",
	"edges",
	"collection",
	"list",
	"children",
	"values",
	"results",
	"events",
	"messages",
}

func parseTranscriptMetadataFields(line []byte) (transcriptMetadataFields, error) {
	var fields transcriptMetadataFields
	if err := json.Unmarshal(line, &fields); err != nil {
		return nil, err
	}
	return fields, nil
}

func (f transcriptMetadataFields) stringValue(keys ...string) string {
	if value, ok := transcriptMetadataStringValue(f, keys, 0); ok {
		return value
	}
	return ""
}

func (f transcriptMetadataFields) idValue(keys ...string) contracts.ID {
	if value, ok := transcriptMetadataIDValue(f, keys, 0); ok {
		return value
	}
	return ""
}

func (f transcriptMetadataFields) sessionIDValue() contracts.ID {
	return f.idValue("sessionId", "sessionID", "session_id", "session", "sessionUuid", "sessionUUID", "session_uuid")
}

func (f transcriptMetadataFields) intValue(keys ...string) int {
	if value, ok := transcriptMetadataIntValue(f, keys, 0); ok {
		return value
	}
	return 0
}

func (f transcriptMetadataFields) boolValue(keys ...string) (bool, bool) {
	return transcriptMetadataBoolValue(f, keys, 0)
}

func (f transcriptMetadataFields) rawValue(keys ...string) json.RawMessage {
	if raw, ok := transcriptMetadataRawValue(f, keys, 0); ok {
		return raw
	}
	return nil
}

func transcriptMetadataStringValue(fields transcriptMetadataFields, keys []string, depth int) (string, bool) {
	if depth > 6 {
		return "", false
	}
	for _, key := range keys {
		raw, ok := transcriptMetadataFieldRaw(fields, key)
		if !ok || isNullJSON(raw) {
			continue
		}
		var value string
		if err := json.Unmarshal(raw, &value); err == nil {
			return value, true
		}
		if nested, ok := transcriptMetadataNestedFields(raw); ok {
			if value, ok := transcriptMetadataStringValue(nested, keys, depth+1); ok {
				return value, true
			}
		}
	}
	for _, nested := range transcriptMetadataWrapperFields(fields) {
		if value, ok := transcriptMetadataStringValue(nested, keys, depth+1); ok {
			return value, true
		}
	}
	return "", false
}

func transcriptMetadataIDValue(fields transcriptMetadataFields, keys []string, depth int) (contracts.ID, bool) {
	if depth > 6 {
		return "", false
	}
	for _, key := range keys {
		raw, ok := transcriptMetadataFieldRaw(fields, key)
		if !ok || isNullJSON(raw) {
			continue
		}
		var value contracts.ID
		if err := json.Unmarshal(raw, &value); err == nil {
			return value, true
		}
		if nested, ok := transcriptMetadataNestedFields(raw); ok {
			if value, ok := transcriptMetadataIDValue(nested, keys, depth+1); ok {
				return value, true
			}
		}
	}
	for _, nested := range transcriptMetadataWrapperFields(fields) {
		if value, ok := transcriptMetadataIDValue(nested, keys, depth+1); ok {
			return value, true
		}
	}
	return "", false
}

func transcriptMetadataIntValue(fields transcriptMetadataFields, keys []string, depth int) (int, bool) {
	if depth > 6 {
		return 0, false
	}
	for _, key := range keys {
		raw, ok := transcriptMetadataFieldRaw(fields, key)
		if !ok || isNullJSON(raw) {
			continue
		}
		var value int
		if err := json.Unmarshal(raw, &value); err == nil {
			return value, true
		}
		var text string
		if err := json.Unmarshal(raw, &text); err == nil {
			value, err := strconv.Atoi(strings.TrimSpace(text))
			if err == nil {
				return value, true
			}
		}
		if nested, ok := transcriptMetadataNestedFields(raw); ok {
			if value, ok := transcriptMetadataIntValue(nested, keys, depth+1); ok {
				return value, true
			}
		}
	}
	for _, nested := range transcriptMetadataWrapperFields(fields) {
		if value, ok := transcriptMetadataIntValue(nested, keys, depth+1); ok {
			return value, true
		}
	}
	return 0, false
}

func transcriptMetadataBoolValue(fields transcriptMetadataFields, keys []string, depth int) (bool, bool) {
	if depth > 6 {
		return false, false
	}
	for _, key := range keys {
		raw, ok := transcriptMetadataFieldRaw(fields, key)
		if !ok || isNullJSON(raw) {
			continue
		}
		var value bool
		if err := json.Unmarshal(raw, &value); err == nil {
			return value, true
		}
		var text string
		if err := json.Unmarshal(raw, &text); err == nil {
			switch strings.ToLower(strings.TrimSpace(text)) {
			case "1", "t", "true", "yes", "y", "on":
				return true, true
			case "0", "f", "false", "no", "n", "off":
				return false, true
			}
		}
		var number int
		if err := json.Unmarshal(raw, &number); err == nil {
			return number != 0, true
		}
		if nested, ok := transcriptMetadataNestedFields(raw); ok {
			if value, ok := transcriptMetadataBoolValue(nested, keys, depth+1); ok {
				return value, true
			}
		}
	}
	for _, nested := range transcriptMetadataWrapperFields(fields) {
		if value, ok := transcriptMetadataBoolValue(nested, keys, depth+1); ok {
			return value, true
		}
	}
	return false, false
}

func transcriptMetadataRawValue(fields transcriptMetadataFields, keys []string, depth int) (json.RawMessage, bool) {
	if depth > 6 {
		return nil, false
	}
	for _, key := range keys {
		raw, ok := transcriptMetadataFieldRaw(fields, key)
		if !ok || isNullJSON(raw) {
			continue
		}
		return append(json.RawMessage(nil), raw...), true
	}
	for _, nested := range transcriptMetadataWrapperFields(fields) {
		if raw, ok := transcriptMetadataRawValue(nested, keys, depth+1); ok {
			return raw, true
		}
	}
	return nil, false
}

func transcriptMetadataFieldRaw(fields transcriptMetadataFields, key string) (json.RawMessage, bool) {
	if raw, ok := fields[key]; ok {
		return raw, true
	}
	normalizedKey := transcriptMetadataNormalizedFieldName(key)
	if normalizedKey == "" {
		return nil, false
	}
	for field, raw := range fields {
		if transcriptMetadataNormalizedFieldName(field) == normalizedKey {
			return raw, true
		}
	}
	return nil, false
}

func transcriptMetadataNormalizedFieldName(name string) string {
	name = strings.ToLower(strings.TrimSpace(name))
	name = strings.ReplaceAll(name, "_", "")
	name = strings.ReplaceAll(name, "-", "")
	name = strings.ReplaceAll(name, " ", "")
	return name
}

func transcriptMetadataNestedFields(raw json.RawMessage) (transcriptMetadataFields, bool) {
	raw = bytes.TrimSpace(raw)
	if len(raw) == 0 || raw[0] != '{' {
		return nil, false
	}
	var fields transcriptMetadataFields
	if err := json.Unmarshal(raw, &fields); err != nil {
		return nil, false
	}
	return fields, true
}

func transcriptMetadataWrapperFields(fields transcriptMetadataFields) []transcriptMetadataFields {
	nested := make([]transcriptMetadataFields, 0, 2)
	for _, key := range transcriptMetadataFieldWrapperKeys {
		raw, ok := transcriptMetadataFieldRaw(fields, key)
		if !ok || isNullJSON(raw) {
			continue
		}
		raw = bytes.TrimSpace(raw)
		if len(raw) == 0 {
			continue
		}
		switch raw[0] {
		case '{':
			if fields, ok := transcriptMetadataNestedFields(raw); ok {
				nested = append(nested, fields)
			}
		case '[':
			var items []json.RawMessage
			if err := json.Unmarshal(raw, &items); err != nil {
				continue
			}
			for _, item := range items {
				if fields, ok := transcriptMetadataNestedFields(item); ok {
					nested = append(nested, fields)
				}
			}
		}
	}
	return nested
}

func (f transcriptMetadataFields) arrayValue(keys ...string) []any {
	raw := f.rawValue(keys...)
	if len(raw) == 0 {
		return nil
	}
	var values []any
	if err := json.Unmarshal(raw, &values); err == nil {
		return values
	}
	var value any
	if err := json.Unmarshal(raw, &value); err != nil || value == nil {
		return nil
	}
	return []any{value}
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
	fields, err := parseTranscriptMetadataFields(line)
	if err != nil {
		return ContentReplacementEntry{}, false
	}
	var aux struct {
		Type         string          `json:"type"`
		SessionID    contracts.ID    `json:"sessionId"`
		Replacements json.RawMessage `json:"replacements"`
	}
	if err := json.Unmarshal(line, &aux); err != nil {
		return ContentReplacementEntry{}, false
	}
	entry := ContentReplacementEntry{
		Type:         aux.Type,
		SessionID:    aux.SessionID,
		Replacements: parseContentReplacementRecords(aux.Replacements),
	}
	if entry.SessionID == "" {
		entry.SessionID = fields.sessionIDValue()
	}
	if entry.AgentID == "" {
		entry.AgentID = string(fields.idValue(
			"agentId", "agentID", "agent_id", "agent",
			"sidechainId", "sidechainID", "sidechain_id",
			"subagentId", "subagentID", "subagent_id",
		))
	}
	if len(entry.Replacements) == 0 {
		entry.Replacements = parseContentReplacementRecords(fields.rawValue(
			"replacements",
			"replacementRecords", "replacement_records",
			"records",
			"contentReplacements", "content_replacements",
			"items",
		))
	}
	return entry, true
}

func parseContentReplacementRecords(raw json.RawMessage) []ContentReplacementRecord {
	if len(raw) == 0 {
		return nil
	}
	var records []ContentReplacementRecord
	if err := json.Unmarshal(raw, &records); err == nil {
		return records
	}
	var record ContentReplacementRecord
	if err := json.Unmarshal(raw, &record); err != nil || record.isEmpty() {
		return nil
	}
	return []ContentReplacementRecord{record}
}

func (r ContentReplacementRecord) isEmpty() bool {
	return r.Kind == "" && r.ToolUseID == "" && r.BlockID == "" && r.Replacement == "" && r.OriginalHash == ""
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
	_ = json.Unmarshal(line, &entry)
	fields, err := parseTranscriptMetadataFields(line)
	if err != nil {
		return ContextCollapseCommitEntry{}, false
	}
	if entry.SessionID == "" {
		entry.SessionID = fields.sessionIDValue()
	}
	if entry.CollapseID == "" {
		entry.CollapseID = string(fields.idValue("collapseId", "collapseID", "collapse_id", "id"))
	}
	if entry.SummaryUUID == "" {
		entry.SummaryUUID = string(fields.idValue("summaryUuid", "summaryUUID", "summary_uuid", "summaryId", "summaryID", "summary_id"))
	}
	if entry.SummaryContent == "" {
		entry.SummaryContent = fields.stringValue("summaryContent", "summary_content", "content")
	}
	if entry.FirstArchivedUUID == "" {
		entry.FirstArchivedUUID = string(fields.idValue("firstArchivedUuid", "firstArchivedUUID", "first_archived_uuid", "firstArchivedId", "firstArchivedID", "first_archived_id"))
	}
	if entry.LastArchivedUUID == "" {
		entry.LastArchivedUUID = string(fields.idValue("lastArchivedUuid", "lastArchivedUUID", "last_archived_uuid", "lastArchivedId", "lastArchivedID", "last_archived_id"))
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
		entry.LastSpawnTokens = fields.intValue("lastSpawnTokens", "last_spawn_tokens", "spawnTokens", "spawn_tokens", "tokenCount", "token_count")
	}
	if !entry.Armed {
		if armed, ok := fields.boolValue("armed", "isArmed", "is_armed", "enabled", "ready"); ok {
			entry.Armed = armed
		}
	}
	if len(entry.Staged) == 0 {
		entry.Staged = fields.arrayValue("staged", "stagedMessages", "staged_messages", "pending", "entries", "items")
	}
	return entry, true
}
