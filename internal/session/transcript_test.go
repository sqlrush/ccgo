package session

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestLoadTranscriptBridgesLegacyProgressEntries(t *testing.T) {
	path := writeTranscript(t, []string{
		`{"type":"user","uuid":"u1","parentUuid":null,"timestamp":"2026-01-01T00:00:00Z"}`,
		`{"type":"progress","uuid":"p1","parentUuid":"u1"}`,
		`{"type":"progress","uuid":"p2","parentUuid":"p1"}`,
		`{"type":"assistant","uuid":"a1","parentUuid":"p2","timestamp":"2026-01-01T00:00:01Z"}`,
	})

	transcript, err := LoadTranscript(path)
	if err != nil {
		t.Fatal(err)
	}
	if got := transcript.Messages["a1"].ParentUUID; got == nil || *got != "u1" {
		t.Fatalf("assistant parent = %#v", got)
	}
	chain := transcript.BuildConversationChain("a1")
	if got := chainIDs(chain); strings.Join(got, ",") != "u1,a1" {
		t.Fatalf("chain = %#v", got)
	}
	if _, ok := transcript.LeafUUIDs["a1"]; !ok {
		t.Fatalf("leaf UUIDs = %#v", transcript.LeafUUIDs)
	}
}

func TestLoadTranscriptAcceptsMessageFieldAliases(t *testing.T) {
	path := writeTranscript(t, []string{
		`{"type":"user","uuid":"u1","session_id":"sess_1","timestamp":"2026-01-01T00:00:00Z","message":{"type":"user","session_id":"sess_1","content":[{"type":"text","text":"hi"}]}}`,
		`{"type":"progress","uuid":"p1","parent_uuid":"u1"}`,
		`{"type":"assistant","uuid":"a1","parent_uuid":"p1","logical_parent_uuid":"u1","session_id":"sess_1","is_sidechain":true,"agent_id":"agent_alias","timestamp":"2026-01-01T00:00:01Z","message":{"id":"msg_1","type":"assistant","parentMessageID":"u1","session_id":"sess_1","is_meta":true,"content":[{"type":"text","text":"done"}]}}`,
	})

	transcript, err := LoadTranscript(path)
	if err != nil {
		t.Fatal(err)
	}
	msg := transcript.Messages["a1"]
	if msg == nil || msg.ParentUUID == nil || *msg.ParentUUID != "u1" || msg.LogicalParentUUID == nil || *msg.LogicalParentUUID != "u1" {
		t.Fatalf("aliased parents = %#v", msg)
	}
	if msg.SessionID != "sess_1" || !msg.IsSidechain || msg.AgentID != "agent_alias" {
		t.Fatalf("aliased transcript message fields = %#v", msg)
	}
	if msg.Message == nil || msg.Message.ParentUUID == nil || *msg.Message.ParentUUID != "u1" || msg.Message.SessionID != "sess_1" || !msg.Message.IsMeta {
		t.Fatalf("aliased nested message = %#v", msg.Message)
	}
	chain := transcript.BuildConversationChain("a1")
	if got := chainIDs(chain); strings.Join(got, ",") != "u1,a1" {
		t.Fatalf("aliased chain = %#v", got)
	}

	index, err := BuildTranscriptLineIndex(path)
	if err != nil {
		t.Fatal(err)
	}
	if ref := index.Entries[index.ByUUID["a1"]]; ref.SessionID != "sess_1" || ref.ParentUUID == nil || *ref.ParentUUID != "u1" {
		t.Fatalf("aliased line ref = %#v", ref)
	}
	resume, err := BuildIndexedResumeConversation(path, "a1", 0)
	if err != nil {
		t.Fatal(err)
	}
	if !resume.Found || strings.Join(chainIDs(resume.Chain), ",") != "u1,a1" || resume.Messages[1].SessionID != "sess_1" {
		t.Fatalf("aliased indexed resume = %#v chain=%#v", resume, chainIDs(resume.Chain))
	}
}

func TestLoadTranscriptAcceptsSessionIDUpperAlias(t *testing.T) {
	path := writeTranscript(t, []string{
		`{"type":"user","uuid":"u1","sessionID":"sess_upper","timestamp":"2026-01-01T00:00:00Z","message":{"type":"user","sessionID":"sess_upper","content":[{"type":"text","text":"hi"}]}}`,
		`{"type":"assistant","uuid":"a1","parentUuid":"u1","sessionID":"sess_upper","timestamp":"2026-01-01T00:00:01Z","message":{"type":"assistant","sessionID":"sess_upper","content":[{"type":"text","text":"done"}]}}`,
	})

	transcript, err := LoadTranscript(path)
	if err != nil {
		t.Fatal(err)
	}
	user := transcript.Messages["u1"]
	if user == nil {
		t.Fatal("missing user message")
	}
	if user.SessionID != "sess_upper" || user.Message == nil || user.Message.SessionID != "sess_upper" {
		t.Fatalf("user sessionID alias = %#v", user)
	}
	assistant := transcript.Messages["a1"]
	if assistant == nil {
		t.Fatal("missing assistant message")
	}
	if assistant.SessionID != "sess_upper" || assistant.Message == nil || assistant.Message.SessionID != "sess_upper" {
		t.Fatalf("assistant sessionID alias = %#v", assistant)
	}
	index, err := LoadTranscriptIndex(path, "")
	if err != nil {
		t.Fatal(err)
	}
	if index.SessionID != "sess_upper" || index.MessageCount != 2 || index.FirstUUID != "u1" || index.LastUUID != "a1" {
		t.Fatalf("index sessionID alias = %#v", index)
	}
	resume, err := BuildIndexedResumeConversation(path, "a1", 0)
	if err != nil {
		t.Fatal(err)
	}
	if !resume.Found || len(resume.Messages) != 2 || resume.Messages[0].SessionID != "sess_upper" || resume.Messages[1].SessionID != "sess_upper" {
		t.Fatalf("resume sessionID alias = %#v", resume)
	}
}

func TestContractMessageFromTranscriptPreservesAttachmentRawPayload(t *testing.T) {
	path := writeTranscript(t, []string{
		`{"type":"attachment","uuid":"att1","subtype":"relevant_memories","attachment":{"type":"relevant_memories","memories":[{"path":"/repo/memory.md","content":"memory body","mtimeMs":123}]}}`,
	})
	transcript, err := LoadTranscript(path)
	if err != nil {
		t.Fatal(err)
	}
	converted, ok := ContractMessageFromTranscript(*transcript.Messages["att1"])
	if !ok {
		t.Fatal("attachment did not convert")
	}
	attachment, ok := converted.Raw["attachment"].(map[string]any)
	if !ok || attachment["type"] != "relevant_memories" {
		t.Fatalf("raw attachment = %#v", converted.Raw)
	}
	memories, ok := attachment["memories"].([]any)
	if !ok || len(memories) != 1 {
		t.Fatalf("memories = %#v", attachment["memories"])
	}
}

func TestLoadTranscriptAcceptsSerializedMessageMetadata(t *testing.T) {
	path := writeTranscript(t, []string{
		`{"type":"user","uuid":"u1","sessionId":"sess_1","userType":"external","entrypoint":"cli","version":"1.2.3","slug":"plan-alpha","message":{"type":"user","content":[{"type":"text","text":"hi"}]}}`,
		`{"type":"assistant","uuid":"a1","parentUuid":"u1","session_id":"sess_1","user_kind":"member","entry_point":"sdk-ts","claude_code_version":"1.2.4","plan_slug":"plan-beta","message":{"type":"assistant","content":[{"type":"text","text":"done"}]}}`,
	})

	transcript, err := LoadTranscript(path)
	if err != nil {
		t.Fatal(err)
	}
	user := transcript.Messages["u1"]
	if user == nil || user.UserType != "external" || user.Entrypoint != "cli" || user.Version != "1.2.3" || user.Slug != "plan-alpha" {
		t.Fatalf("serialized user metadata = %#v", user)
	}
	assistant := transcript.Messages["a1"]
	if assistant == nil || assistant.UserType != "member" || assistant.Entrypoint != "sdk-ts" || assistant.Version != "1.2.4" || assistant.Slug != "plan-beta" {
		t.Fatalf("serialized assistant metadata = %#v", assistant)
	}
}

func TestLoadTranscriptAcceptsMessageUUIDFieldAliases(t *testing.T) {
	path := writeTranscript(t, []string{
		`{"type":"user","messageUuid":"u1","session_id":"sess_1","timestamp":"2026-01-01T00:00:00Z","message":{"type":"user","messageId":"nested_u1","session_id":"sess_1","content":[{"type":"text","text":"hi"}]}}`,
		`{"type":"progress","messageId":"p1","parentMessageUuid":"u1"}`,
		`{"type":"assistant","message_id":"a1","parentMessageID":"p1","session_id":"sess_1","timestamp":"2026-01-01T00:00:01Z","message":{"type":"assistant","messageUUID":"nested_a1","session_id":"sess_1","content":[{"type":"text","text":"done"}]}}`,
	})

	transcript, err := LoadTranscript(path)
	if err != nil {
		t.Fatal(err)
	}
	assistant := transcript.Messages["a1"]
	if assistant == nil || assistant.ParentUUID == nil || *assistant.ParentUUID != "u1" {
		t.Fatalf("aliased assistant parent = %#v", assistant)
	}
	if user := transcript.Messages["u1"]; user == nil || user.Message == nil || user.Message.ID != "nested_u1" || user.Message.UUID != "nested_u1" {
		t.Fatalf("aliased nested user id = %#v", user)
	}
	if assistant.Message == nil || assistant.Message.UUID != "nested_a1" {
		t.Fatalf("aliased nested assistant id = %#v", assistant.Message)
	}
	chain := transcript.BuildConversationChain("a1")
	if got := chainIDs(chain); strings.Join(got, ",") != "u1,a1" {
		t.Fatalf("aliased uuid chain = %#v", got)
	}

	index, err := BuildTranscriptLineIndex(path)
	if err != nil {
		t.Fatal(err)
	}
	if ref := index.Entries[index.ByUUID["a1"]]; ref.SessionID != "sess_1" || ref.ParentUUID == nil || *ref.ParentUUID != "u1" {
		t.Fatalf("aliased uuid line ref = %#v", ref)
	}
	resume, err := BuildIndexedResumeConversation(path, "a1", 0)
	if err != nil {
		t.Fatal(err)
	}
	if !resume.Found || strings.Join(chainIDs(resume.Chain), ",") != "u1,a1" || resume.Messages[0].UUID != "nested_u1" || resume.Messages[1].UUID != "nested_a1" {
		t.Fatalf("aliased uuid indexed resume = %#v chain=%#v", resume, chainIDs(resume.Chain))
	}
}

func TestLoadTranscriptAcceptsTypeAndTimestampAliases(t *testing.T) {
	path := writeTranscript(t, []string{
		`{"role":"user","id":"u1","session_id":"sess_1","created_at":"2026-01-01T00:00:00Z","message":{"type":"user","session_id":"sess_1","content":[{"type":"text","text":"hi"}]}}`,
		`{"entry_type":"assistant","id":"a1","parent_id":"u1","session_id":"sess_1","createdAt":"2026-01-01T00:00:01Z","message":{"type":"assistant","session_id":"sess_1","content":[{"type":"text","text":"done"}]}}`,
	})

	transcript, err := LoadTranscript(path)
	if err != nil {
		t.Fatal(err)
	}
	assistant := transcript.Messages["a1"]
	if assistant == nil || assistant.Type != "assistant" || assistant.Timestamp != "2026-01-01T00:00:01Z" || assistant.ParentUUID == nil || *assistant.ParentUUID != "u1" {
		t.Fatalf("type/timestamp alias assistant = %#v", assistant)
	}
	if user := transcript.Messages["u1"]; user == nil || user.Type != "user" || user.Timestamp != "2026-01-01T00:00:00Z" {
		t.Fatalf("type/timestamp alias user = %#v", user)
	}

	index, err := BuildTranscriptLineIndex(path)
	if err != nil {
		t.Fatal(err)
	}
	ref := index.Entries[index.ByUUID["a1"]]
	if ref.Type != "assistant" || ref.Timestamp != "2026-01-01T00:00:01Z" || ref.ParentUUID == nil || *ref.ParentUUID != "u1" {
		t.Fatalf("type/timestamp alias line ref = %#v", ref)
	}
	if leaf := LatestTranscriptIndexedLeaf(index); leaf != "a1" {
		t.Fatalf("type alias indexed leaf = %q", leaf)
	}
}

func TestLoadTranscriptAcceptsSessionUUIDAliases(t *testing.T) {
	path := writeTranscript(t, []string{
		`{"type":"custom-title","session_uuid":"sess_uuid","customTitle":"UUID Title"}`,
		`{"type":"ai-title","sessionUuid":"sess_uuid","aiTitle":"UUID AI Title"}`,
		`{"type":"last-prompt","sessionUUID":"sess_uuid","lastPrompt":"uuid last prompt"}`,
		`{"type":"task-summary","session_uuid":"sess_uuid","summary":"uuid task","timestamp":"2026-01-01T00:00:03Z"}`,
		`{"type":"tag","sessionUuid":"sess_uuid","tag":"uuid-tag"}`,
		`{"type":"agent-name","sessionUUID":"sess_uuid","agentName":"UUID Agent"}`,
		`{"type":"agent-color","session_uuid":"sess_uuid","agentColor":"blue"}`,
		`{"type":"agent-setting","sessionUuid":"sess_uuid","agentSetting":"builder"}`,
		`{"type":"pr-link","sessionUUID":"sess_uuid","prNumber":9,"prUrl":"https://github.com/o/r/pull/9","prRepository":"o/r","timestamp":"2026-01-01T00:00:04Z"}`,
		`{"type":"mode","session_uuid":"sess_uuid","mode":"worker"}`,
		`{"type":"worktree-state","sessionUuid":"sess_uuid","worktreeSession":{"worktreePath":"/tmp/wt","sessionId":"sess_uuid"}}`,
		`{"type":"content-replacement","sessionUUID":"sess_uuid","replacements":[{"replacement":"uuid stub"}]}`,
		`{"type":"marble-origami-commit","session_uuid":"sess_uuid","collapseId":"collapse_1","summaryUuid":"sum_1","summary":"short","firstArchivedUuid":"u0","lastArchivedUuid":"a0"}`,
		`{"type":"marble-origami-snapshot","sessionUUID":"sess_uuid","armed":true,"lastSpawnTokens":32}`,
		`{"type":"user","uuid":"u1","sessionUuid":"sess_uuid","message":{"type":"user","sessionUUID":"sess_uuid","content":[{"type":"text","text":"uuid prompt text"}]}}`,
	})

	transcript, err := LoadTranscript(path)
	if err != nil {
		t.Fatal(err)
	}
	msg := transcript.Messages["u1"]
	if msg == nil {
		t.Fatal("session uuid message missing")
	}
	if msg.SessionID != "sess_uuid" || msg.Message == nil || msg.Message.SessionID != "sess_uuid" {
		t.Fatalf("session uuid message aliases = %#v nested=%#v", msg, msg.Message)
	}
	if transcript.CustomTitles["sess_uuid"] != "UUID Title" || transcript.AITitles["sess_uuid"] != "UUID AI Title" || transcript.LastPrompts["sess_uuid"] != "uuid last prompt" {
		t.Fatalf("session uuid title metadata = %#v %#v %#v", transcript.CustomTitles, transcript.AITitles, transcript.LastPrompts)
	}
	if transcript.TaskSummaries["sess_uuid"].Summary != "uuid task" || transcript.Tags["sess_uuid"] != "uuid-tag" {
		t.Fatalf("session uuid task/tag metadata = %#v %#v", transcript.TaskSummaries, transcript.Tags)
	}
	if transcript.AgentNames["sess_uuid"] != "UUID Agent" || transcript.AgentColors["sess_uuid"] != "blue" || transcript.AgentSettings["sess_uuid"] != "builder" {
		t.Fatalf("session uuid agent metadata = %#v %#v %#v", transcript.AgentNames, transcript.AgentColors, transcript.AgentSettings)
	}
	if transcript.PRLinks["sess_uuid"].PRNumber != 9 || transcript.Modes["sess_uuid"] != "worker" || len(transcript.WorktreeStates["sess_uuid"].WorktreeSession) == 0 {
		t.Fatalf("session uuid pr/mode/worktree metadata = %#v %#v %#v", transcript.PRLinks, transcript.Modes, transcript.WorktreeStates)
	}
	if len(transcript.ContentReplacements["sess_uuid"]) != 1 || transcript.ContentReplacements["sess_uuid"][0].Replacement != "uuid stub" {
		t.Fatalf("session uuid content replacements = %#v", transcript.ContentReplacements)
	}
	if len(transcript.ContextCollapseCommits) != 1 || transcript.ContextCollapseCommits[0].SessionID != "sess_uuid" || transcript.ContextCollapseSnapshot == nil || transcript.ContextCollapseSnapshot.SessionID != "sess_uuid" {
		t.Fatalf("session uuid collapse metadata = %#v snapshot=%#v", transcript.ContextCollapseCommits, transcript.ContextCollapseSnapshot)
	}

	index, err := LoadTranscriptIndex(path, "sess_uuid")
	if err != nil {
		t.Fatal(err)
	}
	if index.Title != "UUID Title" || index.AITitle != "UUID AI Title" || index.LastPrompt != "uuid last prompt" || index.TaskSummary != "uuid task" {
		t.Fatalf("session uuid index title metadata = %#v", index)
	}
	if index.Tag != "uuid-tag" || index.AgentName != "UUID Agent" || index.AgentColor != "blue" || index.AgentSetting != "builder" {
		t.Fatalf("session uuid index agent metadata = %#v", index)
	}
	if index.PRNumber != 9 || index.Mode != "worker" || !index.HasWorktreeState || index.ContentReplacementCount != 1 || index.FirstUserText != "uuid prompt text" {
		t.Fatalf("session uuid index = %#v", index)
	}
}

func TestLoadTranscriptPrunesBeforeCompactBoundary(t *testing.T) {
	path := writeTranscript(t, []string{
		`{"type":"user","uuid":"u1","parentUuid":null}`,
		`{"type":"assistant","uuid":"a1","parentUuid":"u1"}`,
		`{"type":"marble-origami-commit","sessionId":"s1","collapseId":"0000000000000001","summaryUuid":"sum1","summary":"old","firstArchivedUuid":"u1","lastArchivedUuid":"a1"}`,
		`{"type":"system","subtype":"compact_boundary","uuid":"cb1","parentUuid":null,"compactMetadata":{"trigger":"manual","preTokens":100}}`,
		`{"type":"user","uuid":"u2","parentUuid":"cb1"}`,
		`{"type":"assistant","uuid":"a2","parentUuid":"u2"}`,
	})

	transcript, err := LoadTranscript(path)
	if err != nil {
		t.Fatal(err)
	}
	if _, ok := transcript.Messages["u1"]; ok {
		t.Fatalf("pre-boundary message u1 was not pruned")
	}
	if _, ok := transcript.Messages["a1"]; ok {
		t.Fatalf("pre-boundary message a1 was not pruned")
	}
	if len(transcript.ContextCollapseCommits) != 0 {
		t.Fatalf("stale collapse commits = %#v", transcript.ContextCollapseCommits)
	}
	chain := transcript.BuildConversationChain("a2")
	if got := chainIDs(chain); strings.Join(got, ",") != "cb1,u2,a2" {
		t.Fatalf("chain = %#v", got)
	}
}

func TestLoadTranscriptMetadataDropsCollapseStateAtCompactBoundary(t *testing.T) {
	path := writeTranscript(t, []string{
		`{"type":"marble-origami-commit","sessionId":"s1","collapseId":"old","summaryUuid":"old_sum","summary":"old","firstArchivedUuid":"u1","lastArchivedUuid":"a1"}`,
		`{"type":"marble-origami-snapshot","sessionId":"s1","armed":true,"lastSpawnTokens":12}`,
		`{"type":"system","subtype":"compact_boundary","uuid":"cb1","parentUuid":null,"compactMetadata":{"trigger":"manual","preTokens":100}}`,
		`{"type":"marble-origami-commit","sessionId":"s1","collapseId":"new","summaryUuid":"new_sum","summary":"new","firstArchivedUuid":"u2","lastArchivedUuid":"a2"}`,
		`{"type":"marble-origami-snapshot","sessionId":"s1","armed":false,"lastSpawnTokens":24}`,
	})
	metadata, err := LoadTranscriptMetadata(path)
	if err != nil {
		t.Fatal(err)
	}
	if len(metadata.ContextCollapseCommits) != 1 || metadata.ContextCollapseCommits[0].CollapseID != "new" {
		t.Fatalf("collapse commits = %#v", metadata.ContextCollapseCommits)
	}
	if metadata.ContextCollapseSnapshot == nil || metadata.ContextCollapseSnapshot.LastSpawnTokens != 24 {
		t.Fatalf("collapse snapshot = %#v", metadata.ContextCollapseSnapshot)
	}
}

func TestLoadTranscriptAcceptsCompactAndSnipMetadataAliases(t *testing.T) {
	path := writeTranscript(t, []string{
		`{"type":"user","uuid":"u1","parentUuid":null}`,
		`{"type":"assistant","uuid":"a1","parentUuid":"u1"}`,
		`{"type":"user","uuid":"u2","parentUuid":"a1"}`,
		`{"type":"system","uuid":"s1","parent_uuid":"u2","compact_metadata":{"trigger":"manual","pre_tokens":100,"user_context":"ctx","messages_summarized":2,"preserved_segment":{"head_uuid":"u1","tail_uuid":"a1","anchor_uuid":"root"}},"snip_metadata":{"removed_uuids":["a1"]}}`,
	})

	transcript, err := LoadTranscript(path)
	if err != nil {
		t.Fatal(err)
	}
	system := transcript.Messages["s1"]
	if system == nil || system.ParentUUID == nil || *system.ParentUUID != "u2" {
		t.Fatalf("system parent = %#v", system)
	}
	if system.CompactMetadata == nil || system.CompactMetadata.PreTokens != 100 || system.CompactMetadata.UserContext != "ctx" || system.CompactMetadata.MessagesSummarized != 2 {
		t.Fatalf("compact metadata = %#v", system.CompactMetadata)
	}
	if seg := system.CompactMetadata.PreservedSegment; seg == nil || seg.HeadUUID != "u1" || seg.TailUUID != "a1" || seg.AnchorUUID != "root" {
		t.Fatalf("preserved segment = %#v", system.CompactMetadata.PreservedSegment)
	}
	if _, ok := transcript.Messages["a1"]; ok {
		t.Fatalf("snipped message still present")
	}
	if transcript.Messages["u2"].ParentUUID == nil || *transcript.Messages["u2"].ParentUUID != "u1" {
		t.Fatalf("snip relink parent = %#v", transcript.Messages["u2"].ParentUUID)
	}
}

func TestLoadTranscriptAppliesSnipRemovalAndRelinks(t *testing.T) {
	path := writeTranscript(t, []string{
		`{"type":"user","uuid":"u1","parentUuid":null}`,
		`{"type":"assistant","uuid":"a1","parentUuid":"u1"}`,
		`{"type":"user","uuid":"u2","parentUuid":"a1"}`,
		`{"type":"system","uuid":"s1","parentUuid":"u2","snipMetadata":{"removedUuids":["a1"]}}`,
	})

	transcript, err := LoadTranscript(path)
	if err != nil {
		t.Fatal(err)
	}
	if _, ok := transcript.Messages["a1"]; ok {
		t.Fatalf("snipped message still present")
	}
	if got := transcript.Messages["u2"].ParentUUID; got == nil || *got != "u1" {
		t.Fatalf("u2 parent = %#v", got)
	}
	chain := transcript.BuildConversationChain("u2")
	if got := chainIDs(chain); strings.Join(got, ",") != "u1,u2" {
		t.Fatalf("chain = %#v", got)
	}
}

func TestLoadTranscriptAppliesTombstonesAndRelinks(t *testing.T) {
	path := writeTranscript(t, []string{
		`{"type":"user","uuid":"u1","parentUuid":null,"timestamp":"2026-01-01T00:00:00Z"}`,
		`{"type":"assistant","uuid":"a1","parentUuid":"u1","timestamp":"2026-01-01T00:00:01Z"}`,
		`{"type":"user","uuid":"u2","parentUuid":"a1","timestamp":"2026-01-01T00:00:02Z"}`,
		`{"type":"tombstone","targetUuid":"a1","parentUuid":"u1","sessionId":"s1","reason":"deleted","timestamp":"2026-01-01T00:00:03Z"}`,
		`{"type":"tombstone","deleted_uuid":"ghost","parent_uuid":"u2","session_uuid":"s2","deleted_reason":"alias deleted"}`,
		`{"type":"tombstone","uuid":"legacy","parent_uuid":"u2","sessionId":"s3","reason":"legacy fallback"}`,
		`{"type":"tombstone","targetID":"old","parentMessageID":"u1","sessionID":"s4","deletedReason":"id alias deleted"}`,
	})

	transcript, err := LoadTranscript(path)
	if err != nil {
		t.Fatal(err)
	}
	if _, ok := transcript.Messages["a1"]; ok {
		t.Fatalf("tombstoned message still present")
	}
	if got := transcript.Messages["u2"].ParentUUID; got == nil || *got != "u1" {
		t.Fatalf("u2 parent = %#v", got)
	}
	chain := transcript.BuildConversationChain("u2")
	if got := chainIDs(chain); strings.Join(got, ",") != "u1,u2" {
		t.Fatalf("chain = %#v", got)
	}
	if entry := transcript.Tombstones["a1"]; entry.SessionID != "s1" || entry.Reason != "deleted" || entry.ParentUUID == nil || *entry.ParentUUID != "u1" {
		t.Fatalf("tombstone entry = %#v", entry)
	}
	if entry := transcript.Tombstones["ghost"]; entry.SessionID != "s2" || entry.Reason != "alias deleted" || entry.ParentUUID == nil || *entry.ParentUUID != "u2" {
		t.Fatalf("aliased tombstone entry = %#v", entry)
	}
	if entry := transcript.Tombstones["legacy"]; entry.SessionID != "s3" || entry.Reason != "legacy fallback" || entry.TargetUUID != "legacy" {
		t.Fatalf("legacy tombstone entry = %#v", entry)
	}
	if entry := transcript.Tombstones["old"]; entry.SessionID != "s4" || entry.Reason != "id alias deleted" || entry.ParentUUID == nil || *entry.ParentUUID != "u1" {
		t.Fatalf("id aliased tombstone entry = %#v", entry)
	}

	metadata, err := LoadTranscriptMetadata(path)
	if err != nil {
		t.Fatal(err)
	}
	if metadata.Tombstones["a1"].Reason != "deleted" || metadata.Tombstones["ghost"].SessionID != "s2" || metadata.Tombstones["legacy"].TargetUUID != "legacy" || metadata.Tombstones["old"].ParentUUID == nil || *metadata.Tombstones["old"].ParentUUID != "u1" {
		t.Fatalf("metadata tombstones = %#v", metadata.Tombstones)
	}
}

func TestLoadTranscriptTombstoneRelinkHandlesDeletedParentCycle(t *testing.T) {
	path := writeTranscript(t, []string{
		`{"type":"assistant","uuid":"a1","parentUuid":"a2"}`,
		`{"type":"assistant","uuid":"a2","parentUuid":"a1"}`,
		`{"type":"user","uuid":"u1","parentUuid":"a1"}`,
		`{"type":"tombstone","targetUuid":"a1"}`,
		`{"type":"tombstone","targetUuid":"a2"}`,
	})

	transcript, err := LoadTranscript(path)
	if err != nil {
		t.Fatal(err)
	}
	if transcript.Messages["u1"].ParentUUID != nil {
		t.Fatalf("cyclic deleted parent should relink to nil, got %#v", transcript.Messages["u1"].ParentUUID)
	}
	chain := transcript.BuildConversationChain("u1")
	if got := chainIDs(chain); strings.Join(got, ",") != "u1" {
		t.Fatalf("chain = %#v", got)
	}
}

func TestBuildConversationChainRecoversOrphanedParallelToolResults(t *testing.T) {
	path := writeTranscript(t, []string{
		`{"type":"user","uuid":"u1","parentUuid":null,"timestamp":"2026-01-01T00:00:00Z","message":{"type":"user","content":[{"type":"text","text":"run both"}]}}`,
		`{"type":"assistant","uuid":"a1","parentUuid":"u1","timestamp":"2026-01-01T00:00:01Z","message":{"id":"msg_parallel","type":"assistant","content":[{"type":"tool_use","id":"toolu_1","name":"Read"}]}}`,
		`{"type":"assistant","uuid":"a2","parentUuid":"a1","timestamp":"2026-01-01T00:00:02Z","message":{"id":"msg_parallel","type":"assistant","content":[{"type":"tool_use","id":"toolu_2","name":"Grep"}]}}`,
		`{"type":"user","uuid":"tr1","parentUuid":"a1","timestamp":"2026-01-01T00:00:03Z","message":{"type":"user","content":[{"type":"tool_result","tool_use_id":"toolu_1","content":"read ok"}]}}`,
		`{"type":"user","uuid":"tr2","parentUuid":"a2","timestamp":"2026-01-01T00:00:04Z","message":{"type":"user","content":[{"type":"tool_result","tool_use_id":"toolu_2","content":"grep ok"}]}}`,
		`{"type":"user","uuid":"u2","parentUuid":"tr1","timestamp":"2026-01-01T00:00:05Z","message":{"type":"user","content":[{"type":"text","text":"next"}]}}`,
	})

	transcript, err := LoadTranscript(path)
	if err != nil {
		t.Fatal(err)
	}
	chain := transcript.BuildConversationChain("u2")
	if got := chainIDs(chain); strings.Join(got, ",") != "u1,a1,a2,tr2,tr1,u2" {
		t.Fatalf("chain = %#v", got)
	}
}

func TestBuildResumeConversationConvertsTranscriptChain(t *testing.T) {
	path := writeTranscript(t, []string{
		`{"type":"user","uuid":"u1","sessionId":"s1","parentUuid":null,"timestamp":"2026-01-01T00:00:00Z","message":{"type":"user","uuid":"u1","sessionId":"s1","content":[{"type":"text","text":"hello"}]}}`,
		`{"type":"assistant","uuid":"a1","sessionId":"s1","parentUuid":"u1","timestamp":"2026-01-01T00:00:01Z","message":{"id":"msg_1","type":"assistant","content":[{"type":"text","text":"hi"}]}}`,
		`{"type":"user","uuid":"u2","sessionId":"s1","parentUuid":"a1","timestamp":"2026-01-01T00:00:02Z","content":[{"type":"text","text":"continue"}]}`,
	})
	resume, err := BuildResumeConversation(path, "u2")
	if err != nil {
		t.Fatal(err)
	}
	if !resume.Found || resume.Leaf != "u2" || strings.Join(chainIDs(resume.Chain), ",") != "u1,a1,u2" {
		t.Fatalf("resume = %#v", resume)
	}
	if len(resume.Messages) != 3 {
		t.Fatalf("messages = %#v", resume.Messages)
	}
	if resume.Messages[0].Type != "user" || resume.Messages[0].UUID != "u1" || resume.Messages[0].Content[0].Text != "hello" {
		t.Fatalf("first message = %#v", resume.Messages[0])
	}
	if resume.Messages[1].Type != "assistant" || resume.Messages[1].ID != "msg_1" || resume.Messages[1].ParentUUID == nil || *resume.Messages[1].ParentUUID != "u1" {
		t.Fatalf("assistant = %#v", resume.Messages[1])
	}
	if resume.Messages[2].Type != "user" || resume.Messages[2].Content[0].Text != "continue" || resume.Messages[2].ParentUUID == nil || *resume.Messages[2].ParentUUID != "a1" {
		t.Fatalf("last message = %#v", resume.Messages[2])
	}
}

func TestBuildResumeConversationAcceptsContentBlockFieldAliases(t *testing.T) {
	path := writeTranscript(t, []string{
		`{"type":"assistant","uuid":"a1","sessionId":"s1","timestamp":"2026-01-01T00:00:01Z","message":{"type":"assistant","content":[{"type":"tool_use","id":"toolu_1","name":"Read","cacheControl":{"type":"ephemeral","ttl":"5m"}},{"type":"cache_edits","cacheReference":"cache_ref_1","edits":[{"type":"clear","cacheReference":"cache_ref_0"}]}]}}`,
		`{"type":"user","uuid":"tr1","sessionId":"s1","parentUuid":"a1","timestamp":"2026-01-01T00:00:02Z","content":[{"type":"tool_result","toolUseId":"toolu_1","isError":true,"content":"denied"}]}`,
	})
	resume, err := BuildResumeConversation(path, "tr1")
	if err != nil {
		t.Fatal(err)
	}
	if !resume.Found || strings.Join(chainIDs(resume.Chain), ",") != "a1,tr1" {
		t.Fatalf("resume = %#v chain=%#v", resume, chainIDs(resume.Chain))
	}
	if len(resume.Messages) != 2 {
		t.Fatalf("messages = %#v", resume.Messages)
	}
	assistant := resume.Messages[0]
	if len(assistant.Content) != 2 {
		t.Fatalf("assistant content = %#v", assistant.Content)
	}
	if assistant.Content[0].CacheControl == nil || assistant.Content[0].CacheControl.Type != "ephemeral" || assistant.Content[0].CacheControl.TTL != "5m" {
		t.Fatalf("cacheControl alias = %#v", assistant.Content[0].CacheControl)
	}
	if assistant.Content[1].CacheReference != "cache_ref_1" || len(assistant.Content[1].Edits) != 1 || assistant.Content[1].Edits[0].CacheReference != "cache_ref_0" {
		t.Fatalf("cacheReference aliases = %#v", assistant.Content[1])
	}
	user := resume.Messages[1]
	if len(user.Content) != 1 || user.Content[0].ToolUseID != "toolu_1" || !user.Content[0].IsError {
		t.Fatalf("tool result aliases = %#v", user.Content)
	}
}

func TestBuildResumeConversationUsesLatestLeaf(t *testing.T) {
	path := writeTranscript(t, []string{
		`{"type":"user","uuid":"u1","parentUuid":null,"message":{"type":"user","content":[{"type":"text","text":"first"}]}}`,
		`{"type":"assistant","uuid":"a1","parentUuid":"u1","message":{"type":"assistant","content":[{"type":"text","text":"ok"}]}}`,
		`{"type":"user","uuid":"u2","parentUuid":"a1","message":{"type":"user","content":[{"type":"text","text":"latest"}]}}`,
	})
	resume, err := BuildResumeConversation(path, "")
	if err != nil {
		t.Fatal(err)
	}
	if !resume.Found || resume.Leaf != "u2" || len(resume.Messages) != 3 {
		t.Fatalf("resume = %#v", resume)
	}
	if resume.Messages[2].Content[0].Text != "latest" {
		t.Fatalf("latest message = %#v", resume.Messages[2])
	}
}

func TestBuildIndexedResumeConversationUsesLineIndex(t *testing.T) {
	lines := []string{
		`{"type":"user","uuid":"u1","sessionId":"s1","parentUuid":null,"timestamp":"2026-01-01T00:00:00Z","message":{"type":"user","content":[{"type":"text","text":"hello"}]}}`,
		`{"type":"progress","uuid":"p1","parentUuid":"u1"}`,
		`{"type":"assistant","uuid":"a1","sessionId":"s1","parentUuid":"p1","timestamp":"2026-01-01T00:00:01Z","message":{"id":"msg_1","type":"assistant","content":[{"type":"text","text":"hi"}]}}`,
		`{"type":"user","uuid":"u2","sessionId":"s1","parentUuid":"a1","timestamp":"2026-01-01T00:00:02Z","message":{"type":"user","content":[{"type":"text","text":"latest"}]}}`,
	}
	path := writeTranscript(t, lines)
	resume, err := BuildIndexedResumeConversation(path, "", 0)
	if err != nil {
		t.Fatal(err)
	}
	if !resume.Found || resume.Leaf != "u2" || strings.Join(chainIDs(resume.Chain), ",") != "u1,a1,u2" {
		t.Fatalf("indexed resume = %#v chain=%#v", resume, chainIDs(resume.Chain))
	}
	if len(resume.Messages) != 3 || resume.Messages[2].Content[0].Text != "latest" {
		t.Fatalf("indexed resume messages = %#v", resume.Messages)
	}
	if resume.Chain[1].ParentUUID == nil || *resume.Chain[1].ParentUUID != "u1" {
		t.Fatalf("indexed resume should bridge progress parent: %#v", resume.Chain[1].ParentUUID)
	}

	budget := int64(len(lines[2]) + 1 + len(lines[3]) + 1)
	resume, err = BuildIndexedResumeConversation(path, "u2", budget)
	if err != nil {
		t.Fatal(err)
	}
	if !resume.Found || !resume.HasBefore || resume.BytesRead > budget || strings.Join(chainIDs(resume.Chain), ",") != "a1,u2" {
		t.Fatalf("budgeted indexed resume = %#v chain=%#v budget=%d", resume, chainIDs(resume.Chain), budget)
	}
}

func TestLoadTranscriptCollectsMetadataEntries(t *testing.T) {
	path := writeTranscript(t, []string{
		`{"type":"summary","leafUuid":"a1","summary":"short"}`,
		`{"type":"summary","leaf_uuid":"a2","summary":"snake short"}`,
		`{"type":"summary","leafID":"a3","summary":"id short"}`,
		`{"type":"custom-title","sessionId":"s1","customTitle":"Title"}`,
		`{"type":"custom_title","session_id":"s3","custom_title":"Snake Title"}`,
		`{"type":"ai-title","sessionId":"s1","aiTitle":"AI Title"}`,
		`{"type":"ai_title","sessionId":"s2","aiTitle":"Alias AI Title"}`,
		`{"type":"ai-title","session_id":"s3","ai_title":"Snake AI Title"}`,
		`{"type":"last-prompt","sessionId":"s1","lastPrompt":"last prompt text"}`,
		`{"type":"last_prompt","session_id":"s3","last_prompt":"snake prompt"}`,
		`{"type":"task-summary","sessionId":"s1","summary":"running tests","timestamp":"2026-01-01T00:00:03Z"}`,
		`{"type":"task_summary","session_id":"s3","summary":"snake task","timestamp":"2026-01-01T00:00:03Z"}`,
		`{"type":"tag","sessionId":"s1","tag":"tagged"}`,
		`{"type":"tag","session_id":"s3","tag":"snake-tag"}`,
		`{"type":"agent-name","sessionId":"s1","agentName":"Builder"}`,
		`{"type":"agent_name","session_id":"s3","agent_name":"Snake Builder"}`,
		`{"type":"agent-color","sessionId":"s1","agentColor":"blue"}`,
		`{"type":"agent_color","session_id":"s3","agent_color":"violet"}`,
		`{"type":"agent-setting","sessionId":"s1","agentSetting":"reviewer"}`,
		`{"type":"agent_setting","session_id":"s3","agent_setting":"planner"}`,
		`{"type":"pr-link","sessionId":"s1","prNumber":42,"prUrl":"https://github.com/o/r/pull/42","prRepository":"o/r","timestamp":"2026-01-01T00:00:04Z"}`,
		`{"type":"pr_link","session_id":"s3","pr_number":43,"pr_url":"https://github.com/o/r/pull/43","pr_repository":"o/r","timestamp":"2026-01-01T00:00:04Z"}`,
		`{"type":"mode","sessionId":"s1","mode":"coordinator"}`,
		`{"type":"mode","session_id":"s3","mode":"worker"}`,
		`{"type":"worktree-state","sessionId":"s1","worktreeSession":{"worktreePath":"/tmp/wt","sessionId":"s1"}}`,
		`{"type":"worktree_state","session_id":"s3","worktree_session":{"worktreePath":"/tmp/wt2","sessionId":"s3"}}`,
		`{"type":"worktree-state","sessionID":"s4","worktreeState":{"worktreePath":"/tmp/wt4","sessionId":"s4"}}`,
		`{"type":"file-history-snapshot","messageId":"a1","snapshot":{"files":[]},"isSnapshotUpdate":false}`,
		`{"type":"file_history_snapshot","message_id":"a2","snapshot":{"files":["alias"]},"is_snapshot_update":true}`,
		`{"type":"attribution-snapshot","messageId":"a1","surface":"cli","fileStates":{}}`,
		`{"type":"attribution_snapshot","messageUUID":"a2","surface":"ide","fileStates":{}}`,
		`{"type":"speculation-accept","timestamp":"2026-01-01T00:00:05Z","timeSavedMs":1200}`,
		`{"type":"speculation_accept","timestamp":"2026-01-01T00:00:05Z","time_saved_ms":3400}`,
		`{"type":"content-replacement","sessionId":"s1","replacements":[{"toolUseId":"toolu_1","replacement":"stub"}]}`,
		`{"type":"content_replacement","sessionId":"s2","replacements":[{"toolUseId":"toolu_3","replacement":"alias stub"}]}`,
		`{"type":"content-replacement","sessionId":"s1","agentId":"agent_1","replacements":[{"toolUseId":"toolu_2","replacement":"agent stub"}]}`,
		`{"type":"content_replacement","session_id":"s3","agent_id":"agent_2","replacements":[{"tool_use_id":"toolu_4","block_id":"block_4","original_hash":"hash_4","replacement":"snake agent stub"}]}`,
		`{"type":"content-replacement","sessionId":"s1","agentID":"agent_3","replacements":[{"toolUseID":"toolu_5","blockID":"block_5","originalHash":"hash_5","replacement":"upper agent stub"}]}`,
		`{"type":"marble_origami_commit","session_id":"s3","collapse_id":"c1","summary_uuid":"sum1","summary_content":"collapsed","summary":"short","first_archived_uuid":"u1","last_archived_uuid":"a1"}`,
		`{"type":"marble-origami-commit","sessionID":"s4","collapseID":"c2","summaryID":"sum2","content":"upper collapsed","firstArchivedID":"u2","lastArchivedID":"a2"}`,
		`{"type":"marble-origami-snapshot","sessionId":"s1","armed":true,"lastSpawnTokens":42}`,
		`{"type":"marble_origami_snapshot","session_id":"s3","armed":true,"last_spawn_tokens":64}`,
	})
	transcript, err := LoadTranscript(path)
	if err != nil {
		t.Fatal(err)
	}
	if transcript.Summaries["a1"] != "short" || transcript.CustomTitles["s1"] != "Title" || transcript.Tags["s1"] != "tagged" {
		t.Fatalf("metadata = %#v %#v %#v", transcript.Summaries, transcript.CustomTitles, transcript.Tags)
	}
	if transcript.AITitles["s1"] != "AI Title" || transcript.LastPrompts["s1"] != "last prompt text" || transcript.TaskSummaries["s1"].Summary != "running tests" {
		t.Fatalf("title/prompt/task metadata = %#v %#v %#v", transcript.AITitles, transcript.LastPrompts, transcript.TaskSummaries)
	}
	if transcript.AITitles["s2"] != "Alias AI Title" {
		t.Fatalf("alias ai title = %#v", transcript.AITitles)
	}
	if transcript.Summaries["a2"] != "snake short" || transcript.Summaries["a3"] != "id short" || transcript.CustomTitles["s3"] != "Snake Title" || transcript.AITitles["s3"] != "Snake AI Title" || transcript.LastPrompts["s3"] != "snake prompt" || transcript.TaskSummaries["s3"].Summary != "snake task" {
		t.Fatalf("snake metadata = %#v %#v %#v %#v %#v", transcript.Summaries, transcript.CustomTitles, transcript.AITitles, transcript.LastPrompts, transcript.TaskSummaries)
	}
	if transcript.AgentNames["s1"] != "Builder" || transcript.AgentColors["s1"] != "blue" || transcript.AgentSettings["s1"] != "reviewer" {
		t.Fatalf("agent metadata = %#v %#v %#v", transcript.AgentNames, transcript.AgentColors, transcript.AgentSettings)
	}
	if transcript.Tags["s3"] != "snake-tag" || transcript.AgentNames["s3"] != "Snake Builder" || transcript.AgentColors["s3"] != "violet" || transcript.AgentSettings["s3"] != "planner" {
		t.Fatalf("snake agent metadata = %#v %#v %#v %#v", transcript.Tags, transcript.AgentNames, transcript.AgentColors, transcript.AgentSettings)
	}
	if transcript.PRLinks["s1"].PRNumber != 42 || transcript.Modes["s1"] != "coordinator" || len(transcript.WorktreeStates["s1"].WorktreeSession) == 0 {
		t.Fatalf("session metadata = %#v %#v %#v", transcript.PRLinks, transcript.Modes, transcript.WorktreeStates)
	}
	if transcript.PRLinks["s3"].PRNumber != 43 || transcript.Modes["s3"] != "worker" || len(transcript.WorktreeStates["s3"].WorktreeSession) == 0 {
		t.Fatalf("snake session metadata = %#v %#v %#v", transcript.PRLinks, transcript.Modes, transcript.WorktreeStates)
	}
	if !strings.Contains(string(transcript.WorktreeStates["s4"].WorktreeSession), "/tmp/wt4") {
		t.Fatalf("worktree state alias = %#v", transcript.WorktreeStates["s4"])
	}
	if len(transcript.FileHistorySnapshots) != 2 || len(transcript.AttributionSnapshots) != 2 || len(transcript.SpeculationAccepts) != 2 || transcript.SpeculationAccepts[1].TimeSavedMS != 3400 {
		t.Fatalf("raw metadata counts = %d %d %d", len(transcript.FileHistorySnapshots), len(transcript.AttributionSnapshots), len(transcript.SpeculationAccepts))
	}
	if len(transcript.FileHistoryByMessageID["a1"]) == 0 || len(transcript.FileHistoryByMessageID["a2"]) == 0 || len(transcript.AttributionByMessageID["a1"]) == 0 || len(transcript.AttributionByMessageID["a2"]) == 0 {
		t.Fatalf("snapshot maps = %#v %#v", transcript.FileHistoryByMessageID, transcript.AttributionByMessageID)
	}
	if got := transcript.ContentReplacements["s1"]; len(got) != 1 || got[0].Replacement != "stub" {
		t.Fatalf("content replacements = %#v", got)
	}
	if got := transcript.ContentReplacements["s2"]; len(got) != 1 || got[0].Replacement != "alias stub" {
		t.Fatalf("alias content replacements = %#v", got)
	}
	if got := transcript.ContentReplacements["agent_1"]; len(got) != 1 || got[0].Replacement != "agent stub" {
		t.Fatalf("agent content replacements = %#v", got)
	}
	if got := transcript.ContentReplacements["agent_2"]; len(got) != 1 || got[0].ToolUseID != "toolu_4" || got[0].BlockID != "block_4" || got[0].OriginalHash != "hash_4" || got[0].Replacement != "snake agent stub" {
		t.Fatalf("snake agent content replacements = %#v", got)
	}
	if got := transcript.ContentReplacements["agent_3"]; len(got) != 1 || got[0].ToolUseID != "toolu_5" || got[0].BlockID != "block_5" || got[0].Replacement != "upper agent stub" {
		t.Fatalf("upper agent content replacements = %#v", got)
	}
	if len(transcript.ContextCollapseCommits) != 2 || transcript.ContextCollapseCommits[0].CollapseID != "c1" || transcript.ContextCollapseCommits[0].SummaryUUID != "sum1" || transcript.ContextCollapseCommits[1].CollapseID != "c2" || transcript.ContextCollapseCommits[1].SummaryUUID != "sum2" || transcript.ContextCollapseCommits[1].FirstArchivedUUID != "u2" {
		t.Fatalf("collapse commit = %#v", transcript.ContextCollapseCommits)
	}
	if transcript.ContextCollapseSnapshot == nil || transcript.ContextCollapseSnapshot.SessionID != "s3" || transcript.ContextCollapseSnapshot.LastSpawnTokens != 64 {
		t.Fatalf("snapshot = %#v", transcript.ContextCollapseSnapshot)
	}
	metadata, err := LoadTranscriptMetadata(path)
	if err != nil {
		t.Fatal(err)
	}
	if metadata.Summaries["a1"] != "short" || metadata.CustomTitles["s1"] != "Title" || metadata.Tags["s1"] != "tagged" {
		t.Fatalf("metadata loader = %#v %#v %#v", metadata.Summaries, metadata.CustomTitles, metadata.Tags)
	}
	if metadata.AITitles["s1"] != "AI Title" || metadata.LastPrompts["s1"] != "last prompt text" || metadata.TaskSummaries["s1"].Summary != "running tests" {
		t.Fatalf("metadata title/prompt/task = %#v %#v %#v", metadata.AITitles, metadata.LastPrompts, metadata.TaskSummaries)
	}
	if metadata.AITitles["s2"] != "Alias AI Title" {
		t.Fatalf("metadata alias ai title = %#v", metadata.AITitles)
	}
	if metadata.Summaries["a2"] != "snake short" || metadata.Summaries["a3"] != "id short" || metadata.CustomTitles["s3"] != "Snake Title" || metadata.AITitles["s3"] != "Snake AI Title" || metadata.LastPrompts["s3"] != "snake prompt" || metadata.TaskSummaries["s3"].Summary != "snake task" {
		t.Fatalf("metadata snake fields = %#v %#v %#v %#v %#v", metadata.Summaries, metadata.CustomTitles, metadata.AITitles, metadata.LastPrompts, metadata.TaskSummaries)
	}
	if metadata.AgentNames["s1"] != "Builder" || metadata.AgentColors["s1"] != "blue" || metadata.AgentSettings["s1"] != "reviewer" {
		t.Fatalf("metadata agent = %#v %#v %#v", metadata.AgentNames, metadata.AgentColors, metadata.AgentSettings)
	}
	if metadata.Tags["s3"] != "snake-tag" || metadata.AgentNames["s3"] != "Snake Builder" || metadata.AgentColors["s3"] != "violet" || metadata.AgentSettings["s3"] != "planner" {
		t.Fatalf("metadata snake agent = %#v %#v %#v %#v", metadata.Tags, metadata.AgentNames, metadata.AgentColors, metadata.AgentSettings)
	}
	if metadata.PRLinks["s1"].PRRepository != "o/r" || metadata.Modes["s1"] != "coordinator" || len(metadata.WorktreeStates["s1"].WorktreeSession) == 0 {
		t.Fatalf("metadata session = %#v %#v %#v", metadata.PRLinks, metadata.Modes, metadata.WorktreeStates)
	}
	if metadata.PRLinks["s3"].PRNumber != 43 || metadata.Modes["s3"] != "worker" || len(metadata.WorktreeStates["s3"].WorktreeSession) == 0 {
		t.Fatalf("metadata snake session = %#v %#v %#v", metadata.PRLinks, metadata.Modes, metadata.WorktreeStates)
	}
	if !strings.Contains(string(metadata.WorktreeStates["s4"].WorktreeSession), "/tmp/wt4") {
		t.Fatalf("metadata worktree state alias = %#v", metadata.WorktreeStates["s4"])
	}
	if len(metadata.FileHistorySnapshots) != 2 || len(metadata.AttributionSnapshots) != 2 || len(metadata.SpeculationAccepts) != 2 || metadata.SpeculationAccepts[1].TimeSavedMS != 3400 {
		t.Fatalf("metadata raw counts = %d %d %d", len(metadata.FileHistorySnapshots), len(metadata.AttributionSnapshots), len(metadata.SpeculationAccepts))
	}
	if len(metadata.FileHistoryByMessageID["a1"]) == 0 || len(metadata.FileHistoryByMessageID["a2"]) == 0 || len(metadata.AttributionByMessageID["a1"]) == 0 || len(metadata.AttributionByMessageID["a2"]) == 0 {
		t.Fatalf("metadata snapshot maps = %#v %#v", metadata.FileHistoryByMessageID, metadata.AttributionByMessageID)
	}
	if got := metadata.ContentReplacements["s1"]; len(got) != 1 || got[0].Replacement != "stub" {
		t.Fatalf("metadata replacements = %#v", got)
	}
	if got := metadata.ContentReplacements["s2"]; len(got) != 1 || got[0].Replacement != "alias stub" {
		t.Fatalf("metadata alias replacements = %#v", got)
	}
	if got := metadata.ContentReplacements["agent_1"]; len(got) != 1 || got[0].Replacement != "agent stub" {
		t.Fatalf("metadata agent replacements = %#v", got)
	}
	if got := metadata.ContentReplacements["agent_2"]; len(got) != 1 || got[0].ToolUseID != "toolu_4" || got[0].BlockID != "block_4" || got[0].OriginalHash != "hash_4" || got[0].Replacement != "snake agent stub" {
		t.Fatalf("metadata snake agent replacements = %#v", got)
	}
	if got := metadata.ContentReplacements["agent_3"]; len(got) != 1 || got[0].ToolUseID != "toolu_5" || got[0].BlockID != "block_5" || got[0].Replacement != "upper agent stub" {
		t.Fatalf("metadata upper agent replacements = %#v", got)
	}
	if len(metadata.ContextCollapseCommits) != 2 || metadata.ContextCollapseCommits[0].CollapseID != "c1" || metadata.ContextCollapseCommits[0].SummaryUUID != "sum1" || metadata.ContextCollapseCommits[1].CollapseID != "c2" || metadata.ContextCollapseCommits[1].SummaryUUID != "sum2" || metadata.ContextCollapseCommits[1].FirstArchivedUUID != "u2" {
		t.Fatalf("metadata collapse commit = %#v", metadata.ContextCollapseCommits)
	}
	if metadata.ContextCollapseSnapshot == nil || metadata.ContextCollapseSnapshot.SessionID != "s3" || metadata.ContextCollapseSnapshot.LastSpawnTokens != 64 {
		t.Fatalf("metadata snapshot = %#v", metadata.ContextCollapseSnapshot)
	}
}

func TestLoadTranscriptMetadataAcceptsSessionIDAndNumericStrings(t *testing.T) {
	path := writeTranscript(t, []string{
		`{"type":"pr-link","sessionID":"s4","prNumber":"44","prUrl":"https://github.com/o/r/pull/44","prRepository":"o/r"}`,
		`{"type":"speculation-accept","createdAt":"2026-01-01T00:00:05Z","timeSavedMs":"5600"}`,
		`{"type":"marble-origami-snapshot","sessionID":"s4","armed":true,"lastSpawnTokens":"96"}`,
	})
	transcript, err := LoadTranscript(path)
	if err != nil {
		t.Fatal(err)
	}
	if transcript.PRLinks["s4"].PRNumber != 44 || transcript.PRLinks["s4"].PRRepository != "o/r" {
		t.Fatalf("transcript pr link = %#v", transcript.PRLinks)
	}
	if len(transcript.SpeculationAccepts) != 1 || transcript.SpeculationAccepts[0].TimeSavedMS != 5600 || transcript.SpeculationAccepts[0].Timestamp != "2026-01-01T00:00:05Z" {
		t.Fatalf("transcript speculation = %#v", transcript.SpeculationAccepts)
	}
	if transcript.ContextCollapseSnapshot == nil || transcript.ContextCollapseSnapshot.SessionID != "s4" || transcript.ContextCollapseSnapshot.LastSpawnTokens != 96 {
		t.Fatalf("transcript snapshot = %#v", transcript.ContextCollapseSnapshot)
	}

	metadata, err := LoadTranscriptMetadata(path)
	if err != nil {
		t.Fatal(err)
	}
	if metadata.PRLinks["s4"].PRNumber != 44 || metadata.ContextCollapseSnapshot == nil || metadata.ContextCollapseSnapshot.LastSpawnTokens != 96 {
		t.Fatalf("metadata = %#v snapshot=%#v", metadata.PRLinks, metadata.ContextCollapseSnapshot)
	}
	if len(metadata.SpeculationAccepts) != 1 || metadata.SpeculationAccepts[0].TimeSavedMS != 5600 {
		t.Fatalf("metadata speculation = %#v", metadata.SpeculationAccepts)
	}
}

func TestReappendSessionMetadataWritesSessionScopedTailEntries(t *testing.T) {
	path := writeTranscript(t, []string{
		`{"type":"custom-title","sessionId":"s1","customTitle":"Title"}`,
		`{"type":"ai-title","sessionId":"s1","aiTitle":"AI Title"}`,
		`{"type":"last-prompt","sessionId":"s1","lastPrompt":"last prompt text"}`,
		`{"type":"task-summary","sessionId":"s1","summary":"running tests","timestamp":"2026-01-01T00:00:03Z"}`,
		`{"type":"tag","sessionId":"s1","tag":"tagged"}`,
		`{"type":"agent-name","sessionId":"s1","agentName":"Builder"}`,
		`{"type":"agent-color","sessionId":"s1","agentColor":"blue"}`,
		`{"type":"agent-setting","sessionId":"s1","agentSetting":"reviewer"}`,
		`{"type":"mode","sessionId":"s1","mode":"coordinator"}`,
		`{"type":"worktree-state","sessionId":"s1","worktreeSession":{"worktreePath":"/tmp/wt","sessionId":"s1"}}`,
		`{"type":"pr-link","sessionId":"s1","prNumber":42,"prUrl":"https://github.com/o/r/pull/42","prRepository":"o/r","timestamp":"2026-01-01T00:00:04Z"}`,
		`{"type":"custom-title","sessionId":"other","customTitle":"Other"}`,
	})
	result, err := ReappendSessionMetadata(path, "s1")
	if err != nil {
		t.Fatal(err)
	}
	if result.Written != 11 {
		t.Fatalf("result = %#v", result)
	}
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	lines := strings.Split(strings.TrimSpace(string(raw)), "\n")
	tail := strings.Join(lines[len(lines)-result.Written:], "\n")
	for _, want := range []string{
		`"type":"custom-title"`,
		`"customTitle":"Title"`,
		`"type":"ai-title"`,
		`"aiTitle":"AI Title"`,
		`"type":"last-prompt"`,
		`"lastPrompt":"last prompt text"`,
		`"type":"task-summary"`,
		`"summary":"running tests"`,
		`"type":"tag"`,
		`"type":"agent-name"`,
		`"type":"agent-color"`,
		`"type":"agent-setting"`,
		`"type":"mode"`,
		`"type":"worktree-state"`,
		`"type":"pr-link"`,
	} {
		if !strings.Contains(tail, want) {
			t.Fatalf("tail missing %q in %s", want, tail)
		}
	}
	for _, notWant := range []string{
		`"customTitle":"Other"`,
	} {
		if strings.Contains(tail, notWant) {
			t.Fatalf("tail unexpectedly contains %q in %s", notWant, tail)
		}
	}
}

func TestLoadTranscriptTailKeepsOnlyRecentMessagesAndBridgesProgress(t *testing.T) {
	path := writeTranscript(t, []string{
		`{"type":"user","uuid":"u1","parentUuid":null}`,
		`{"type":"progress","uuid":"p1","parentUuid":"u1"}`,
		`{"type":"assistant","uuid":"a1","parentUuid":"p1"}`,
		`{"type":"summary","leafUuid":"a1","summary":"short"}`,
		`{"type":"user","uuid":"u2","parentUuid":"a1"}`,
		`{"type":"assistant","uuid":"a2","parentUuid":"u2"}`,
	})
	tail, err := LoadTranscriptTail(path, 3)
	if err != nil {
		t.Fatal(err)
	}
	if got := tailIDs(tail); strings.Join(got, ",") != "a1,u2,a2" {
		t.Fatalf("tail = %#v", got)
	}
	if tail[0].ParentUUID == nil || *tail[0].ParentUUID != "u1" {
		t.Fatalf("bridged parent = %#v", tail[0].ParentUUID)
	}
	if len(tail[0].Raw) == 0 {
		t.Fatalf("tail raw message should be retained")
	}
}

func TestLoadTranscriptTailBytesReadsCompleteRecords(t *testing.T) {
	lines := []string{
		`{"type":"user","uuid":"u1","parentUuid":null}`,
		`{"type":"assistant","uuid":"a1","parentUuid":"u1"}`,
		`{"type":"progress","uuid":"p1","parentUuid":"a1"}`,
		`{"type":"user","uuid":"u2","parentUuid":"p1"}`,
		`{"type":"assistant","uuid":"a2","parentUuid":"u2"}`,
	}
	path := writeTranscript(t, lines)
	budget := int64(len(strings.Join(lines[2:], "\n")) + 1)
	tail, err := LoadTranscriptTailBytes(path, budget)
	if err != nil {
		t.Fatal(err)
	}
	if !tail.HasBefore || tail.StartOffset <= 0 || tail.BytesRead > budget {
		t.Fatalf("tail metadata = %#v budget=%d", tail, budget)
	}
	if got := tailIDs(tail.Messages); strings.Join(got, ",") != "u2,a2" {
		t.Fatalf("tail messages = %#v", got)
	}
	if tail.Messages[0].ParentUUID == nil || *tail.Messages[0].ParentUUID != "a1" {
		t.Fatalf("bridged byte-tail parent = %#v", tail.Messages[0].ParentUUID)
	}
	if len(tail.Messages[0].Raw) == 0 {
		t.Fatalf("byte-tail raw message should be retained")
	}

	partial, err := LoadTranscriptTailBytes(path, int64(len(lines[len(lines)-1])/2))
	if err != nil {
		t.Fatal(err)
	}
	if len(partial.Messages) != 0 || !partial.HasBefore {
		t.Fatalf("partial tail = %#v", partial)
	}
}

func TestLoadTranscriptWindowAroundUUID(t *testing.T) {
	path := writeTranscript(t, []string{
		`{"type":"user","uuid":"u1","parentUuid":null}`,
		`{"type":"assistant","uuid":"a1","parentUuid":"u1"}`,
		`{"type":"progress","uuid":"p1","parentUuid":"a1"}`,
		`{"type":"user","uuid":"u2","parentUuid":"p1"}`,
		`{"type":"assistant","uuid":"a2","parentUuid":"u2"}`,
		`{"type":"user","uuid":"u3","parentUuid":"a2"}`,
	})
	window, err := LoadTranscriptWindow(path, "u2", 1, 1)
	if err != nil {
		t.Fatal(err)
	}
	if !window.Found || window.TargetIndex != 1 || !window.HasBefore || !window.HasAfter {
		t.Fatalf("window metadata = %#v", window)
	}
	if got := tailIDs(window.Messages); strings.Join(got, ",") != "a1,u2,a2" {
		t.Fatalf("window messages = %#v", got)
	}
	if window.Messages[1].ParentUUID == nil || *window.Messages[1].ParentUUID != "a1" {
		t.Fatalf("bridged parent = %#v", window.Messages[1].ParentUUID)
	}

	largeWindow, err := LoadTranscriptWindow(path, "a2", 2, 5)
	if err != nil {
		t.Fatal(err)
	}
	if !largeWindow.Found || largeWindow.TargetIndex != 2 || !largeWindow.HasBefore || largeWindow.HasAfter {
		t.Fatalf("large window = %#v", largeWindow)
	}
	if got := tailIDs(largeWindow.Messages); strings.Join(got, ",") != "a1,u2,a2,u3" {
		t.Fatalf("large window messages = %#v", got)
	}
}

func TestTranscriptLineIndexLoadsWindowWithoutFullTranscript(t *testing.T) {
	path := writeTranscript(t, []string{
		`{malformed`,
		`{"type":"user","uuid":"u1","parentUuid":null,"timestamp":"2026-01-01T00:00:00Z"}`,
		`{"type":"progress","uuid":"p1","parentUuid":"u1"}`,
		`{"type":"assistant","uuid":"a1","parentUuid":"p1","timestamp":"2026-01-01T00:00:01Z"}`,
		`{"type":"summary","leafUuid":"a1","summary":"short"}`,
		`{"type":"user","uuid":"u2","parentUuid":"a1","timestamp":"2026-01-01T00:00:02Z"}`,
		`{"type":"assistant","uuid":"a2","parentUuid":"u2","timestamp":"2026-01-01T00:00:03Z"}`,
		`{"type":"user","uuid":"u3","parentUuid":"a2","timestamp":"2026-01-01T00:00:04Z"}`,
	})
	index, err := BuildTranscriptLineIndex(path)
	if err != nil {
		t.Fatal(err)
	}
	if len(index.Entries) != 5 || index.ByUUID["u2"] != 2 {
		t.Fatalf("index = %#v", index)
	}
	if ref := index.Entries[index.ByUUID["a1"]]; ref.ParentUUID == nil || *ref.ParentUUID != "u1" || ref.Offset <= 0 || ref.Length <= 0 {
		t.Fatalf("bridged ref = %#v", ref)
	}
	window, err := LoadTranscriptIndexedWindow(path, index, "u2", 1, 1)
	if err != nil {
		t.Fatal(err)
	}
	if !window.Found || window.TargetIndex != 1 || !window.HasBefore || !window.HasAfter {
		t.Fatalf("indexed window metadata = %#v", window)
	}
	if got := tailIDs(window.Messages); strings.Join(got, ",") != "a1,u2,a2" {
		t.Fatalf("indexed window messages = %#v", got)
	}
	if window.Messages[0].ParentUUID == nil || *window.Messages[0].ParentUUID != "u1" || len(window.Messages[0].Raw) == 0 {
		t.Fatalf("indexed message = %#v", window.Messages[0])
	}
	missing, err := LoadTranscriptIndexedWindow(path, index, "missing", 1, 1)
	if err != nil {
		t.Fatal(err)
	}
	if missing.Found || missing.TargetIndex != -1 {
		t.Fatalf("missing indexed window = %#v", missing)
	}
}

func TestTranscriptLineIndexLoadsByteBudgetWindow(t *testing.T) {
	lines := []string{
		`{"type":"user","uuid":"u1","parentUuid":null}`,
		`{"type":"progress","uuid":"p1","parentUuid":"u1"}`,
		`{"type":"assistant","uuid":"a1","parentUuid":"p1"}`,
		`{"type":"summary","leafUuid":"a1","summary":"short"}`,
		`{"type":"user","uuid":"u2","parentUuid":"a1"}`,
		`{"type":"assistant","uuid":"a2","parentUuid":"u2"}`,
		`{"type":"user","uuid":"u3","parentUuid":"a2"}`,
	}
	path := writeTranscript(t, lines)
	index, err := BuildTranscriptLineIndex(path)
	if err != nil {
		t.Fatal(err)
	}
	budget := int64(len(lines[2]) + 1 + len(lines[4]) + 1 + len(lines[5]) + 1)
	window, err := LoadTranscriptIndexedWindowBytes(path, index, "u2", budget)
	if err != nil {
		t.Fatal(err)
	}
	if !window.Found || window.TargetIndex != 1 || !window.HasBefore || !window.HasAfter || window.BytesRead > budget {
		t.Fatalf("byte window metadata = %#v budget=%d", window, budget)
	}
	if got := tailIDs(window.Messages); strings.Join(got, ",") != "a1,u2,a2" {
		t.Fatalf("byte window messages = %#v", got)
	}
	if window.Messages[0].ParentUUID == nil || *window.Messages[0].ParentUUID != "u1" {
		t.Fatalf("byte window bridged parent = %#v", window.Messages[0].ParentUUID)
	}

	targetOnly, err := LoadTranscriptIndexedWindowBytes(path, index, "u2", int64(len(lines[4])+1))
	if err != nil {
		t.Fatal(err)
	}
	if !targetOnly.Found || targetOnly.TargetIndex != 0 || strings.Join(tailIDs(targetOnly.Messages), ",") != "u2" {
		t.Fatalf("target-only byte window = %#v ids=%#v", targetOnly, tailIDs(targetOnly.Messages))
	}
}

func TestTranscriptLineIndexLoadsTailWithoutFullTranscript(t *testing.T) {
	lines := []string{
		`{"type":"user","uuid":"u1","parentUuid":null}`,
		`{"type":"progress","uuid":"p1","parentUuid":"u1"}`,
		`{"type":"assistant","uuid":"a1","parentUuid":"p1"}`,
		`{"type":"summary","leafUuid":"a1","summary":"short"}`,
		`{"type":"user","uuid":"u2","parentUuid":"a1"}`,
		`{"type":"assistant","uuid":"a2","parentUuid":"u2"}`,
		`{"type":"user","uuid":"u3","parentUuid":"a2"}`,
	}
	path := writeTranscript(t, lines)
	index, err := BuildTranscriptLineIndex(path)
	if err != nil {
		t.Fatal(err)
	}
	tail, err := LoadTranscriptIndexedTail(path, index, 3)
	if err != nil {
		t.Fatal(err)
	}
	if !tail.HasBefore || tail.StartIndex != 2 {
		t.Fatalf("indexed tail metadata = %#v", tail)
	}
	if got := tailIDs(tail.Messages); strings.Join(got, ",") != "u2,a2,u3" {
		t.Fatalf("indexed tail messages = %#v", got)
	}
	if tail.Messages[0].ParentUUID == nil || *tail.Messages[0].ParentUUID != "a1" || len(tail.Messages[0].Raw) == 0 {
		t.Fatalf("indexed tail message = %#v", tail.Messages[0])
	}

	all, err := LoadTranscriptIndexedTail(path, index, 50)
	if err != nil {
		t.Fatal(err)
	}
	if all.HasBefore || all.StartIndex != 0 || strings.Join(tailIDs(all.Messages), ",") != "u1,a1,u2,a2,u3" {
		t.Fatalf("all indexed tail = %#v ids=%#v", all, tailIDs(all.Messages))
	}

	empty, err := LoadTranscriptIndexedTail(path, index, 0)
	if err != nil {
		t.Fatal(err)
	}
	if len(empty.Messages) != 0 || empty.HasBefore {
		t.Fatalf("empty indexed tail = %#v", empty)
	}

	budget := int64(len(lines[5]) + 1 + len(lines[6]) + 1)
	byteTail, err := LoadTranscriptIndexedTailBytes(path, index, budget)
	if err != nil {
		t.Fatal(err)
	}
	if !byteTail.HasBefore || byteTail.StartIndex != 3 || byteTail.BytesRead > budget {
		t.Fatalf("indexed byte tail metadata = %#v budget=%d", byteTail, budget)
	}
	if got := tailIDs(byteTail.Messages); strings.Join(got, ",") != "a2,u3" {
		t.Fatalf("indexed byte tail messages = %#v", got)
	}
	if byteTail.Messages[0].ParentUUID == nil || *byteTail.Messages[0].ParentUUID != "u2" {
		t.Fatalf("indexed byte tail parent = %#v", byteTail.Messages[0].ParentUUID)
	}

	tooSmall, err := LoadTranscriptIndexedTailBytes(path, index, int64(len(lines[6])/2))
	if err != nil {
		t.Fatal(err)
	}
	if len(tooSmall.Messages) != 0 || tooSmall.BytesRead != 0 || !tooSmall.HasBefore {
		t.Fatalf("too-small indexed byte tail = %#v", tooSmall)
	}
}

func TestRemoveTranscriptMessageByUUID(t *testing.T) {
	path := writeTranscript(t, []string{
		`{"type":"user","uuid":"u1","parentUuid":null}`,
		`{"type":"assistant","uuid":"a1","parentUuid":"u1"}`,
		`{"type":"user","uuid":"u2","parentUuid":"a1"}`,
		`{malformed`,
	})
	removed, err := RemoveTranscriptMessageByUUID(path, "a1")
	if err != nil {
		t.Fatal(err)
	}
	if !removed {
		t.Fatal("message was not removed")
	}
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	text := string(data)
	if strings.Contains(text, `"uuid":"a1"`) {
		t.Fatalf("target line still present: %s", text)
	}
	if !strings.Contains(text, `"parentUuid":"a1"`) {
		t.Fatalf("child line should be preserved: %s", text)
	}
	if !strings.Contains(text, `{malformed`) {
		t.Fatalf("malformed line should be preserved: %s", text)
	}
}

func TestRemoveTranscriptMessageByUUIDHonorsRewriteLimit(t *testing.T) {
	path := writeTranscript(t, []string{`{"type":"user","uuid":"u1","parentUuid":null}`})
	removed, err := RemoveTranscriptMessageByUUIDWithLimit(path, "u1", 1)
	if err != nil {
		t.Fatal(err)
	}
	if removed {
		t.Fatal("message should not be removed when file exceeds rewrite limit")
	}
}

func writeTranscript(t *testing.T, lines []string) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "session.jsonl")
	if err := os.WriteFile(path, []byte(strings.Join(lines, "\n")+"\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	return path
}

func chainIDs(chain []TranscriptMessage) []string {
	out := make([]string, 0, len(chain))
	for _, msg := range chain {
		out = append(out, string(msg.UUID))
	}
	return out
}

func tailIDs(messages []TranscriptMessage) []string {
	out := make([]string, 0, len(messages))
	for _, msg := range messages {
		out = append(out, string(msg.UUID))
	}
	return out
}
