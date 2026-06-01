package session

import (
	"bufio"
	"bytes"
	"encoding/json"
	"errors"
	"os"
	"slices"
	"sort"
	"time"

	"ccgo/internal/contracts"
)

const MaxTombstoneRewriteBytes = 50 * 1024 * 1024

type Transcript struct {
	Messages                map[contracts.ID]*TranscriptMessage
	Order                   []contracts.ID
	Summaries               map[contracts.ID]string
	CustomTitles            map[contracts.ID]string
	Tags                    map[contracts.ID]string
	ContentReplacements     map[contracts.ID][]ContentReplacementRecord
	ContextCollapseCommits  []ContextCollapseCommitEntry
	ContextCollapseSnapshot *ContextCollapseSnapshotEntry
	LeafUUIDs               map[contracts.ID]struct{}
}

type TranscriptMessage struct {
	Type              string             `json:"type"`
	UUID              contracts.ID       `json:"uuid"`
	ParentUUID        *contracts.ID      `json:"parentUuid"`
	LogicalParentUUID *contracts.ID      `json:"logicalParentUuid,omitempty"`
	SessionID         contracts.ID       `json:"sessionId,omitempty"`
	Timestamp         string             `json:"timestamp,omitempty"`
	Subtype           string             `json:"subtype,omitempty"`
	Content           any                `json:"content,omitempty"`
	Message           *contracts.Message `json:"message,omitempty"`
	IsSidechain       bool               `json:"isSidechain,omitempty"`
	CompactMetadata   *CompactMetadata   `json:"compactMetadata,omitempty"`
	SnipMetadata      *SnipMetadata      `json:"snipMetadata,omitempty"`
	Raw               json.RawMessage    `json:"-"`
}

type CompactMetadata struct {
	Trigger            string            `json:"trigger,omitempty"`
	PreTokens          int               `json:"preTokens,omitempty"`
	UserContext        string            `json:"userContext,omitempty"`
	MessagesSummarized int               `json:"messagesSummarized,omitempty"`
	PreservedSegment   *PreservedSegment `json:"preservedSegment,omitempty"`
}

type PreservedSegment struct {
	HeadUUID   contracts.ID `json:"headUuid"`
	TailUUID   contracts.ID `json:"tailUuid"`
	AnchorUUID contracts.ID `json:"anchorUuid"`
}

type SnipMetadata struct {
	RemovedUUIDs []contracts.ID `json:"removedUuids,omitempty"`
}

type ContentReplacementRecord struct {
	Kind         string `json:"kind,omitempty"`
	ToolUseID    string `json:"toolUseId,omitempty"`
	BlockID      string `json:"blockId,omitempty"`
	Replacement  string `json:"replacement,omitempty"`
	OriginalHash string `json:"originalHash,omitempty"`
}

type ContentReplacementEntry struct {
	Type         string                     `json:"type"`
	SessionID    contracts.ID               `json:"sessionId"`
	AgentID      string                     `json:"agentId,omitempty"`
	Replacements []ContentReplacementRecord `json:"replacements"`
}

type ContextCollapseCommitEntry struct {
	Type              string       `json:"type"`
	SessionID         contracts.ID `json:"sessionId"`
	CollapseID        string       `json:"collapseId"`
	SummaryUUID       string       `json:"summaryUuid"`
	SummaryContent    string       `json:"summaryContent"`
	Summary           string       `json:"summary"`
	FirstArchivedUUID string       `json:"firstArchivedUuid"`
	LastArchivedUUID  string       `json:"lastArchivedUuid"`
}

type ContextCollapseSnapshotEntry struct {
	Type            string       `json:"type"`
	SessionID       contracts.ID `json:"sessionId"`
	Staged          []any        `json:"staged,omitempty"`
	Armed           bool         `json:"armed,omitempty"`
	LastSpawnTokens int          `json:"lastSpawnTokens,omitempty"`
}

type transcriptEnvelope struct {
	Type       string        `json:"type"`
	UUID       contracts.ID  `json:"uuid"`
	ParentUUID *contracts.ID `json:"parentUuid"`
}

func LoadTranscript(path string) (Transcript, error) {
	f, err := os.Open(path)
	if err != nil {
		if os.IsNotExist(err) {
			return newTranscript(), nil
		}
		return Transcript{}, err
	}
	defer f.Close()

	transcript := newTranscript()
	progressBridge := map[contracts.ID]*contracts.ID{}

	scanner := bufio.NewScanner(f)
	buf := make([]byte, 0, 1024*1024)
	scanner.Buffer(buf, 50*1024*1024)
	for scanner.Scan() {
		line := bytes.TrimSpace(scanner.Bytes())
		if len(line) == 0 {
			continue
		}
		var envelope transcriptEnvelope
		if err := json.Unmarshal(line, &envelope); err != nil {
			continue
		}
		if envelope.Type == "progress" && envelope.UUID != "" {
			progressBridge[envelope.UUID] = resolveProgressParent(progressBridge, envelope.ParentUUID)
			continue
		}
		switch {
		case isTranscriptType(envelope.Type):
			var msg TranscriptMessage
			if err := json.Unmarshal(line, &msg); err != nil || msg.UUID == "" {
				continue
			}
			if msg.ParentUUID != nil {
				if bridged, ok := progressBridge[*msg.ParentUUID]; ok {
					msg.ParentUUID = cloneIDPtr(bridged)
				}
			}
			msg.Raw = append(json.RawMessage(nil), line...)
			transcript.addMessage(&msg)
			if msg.IsCompactBoundary() {
				transcript.ContextCollapseCommits = nil
				transcript.ContextCollapseSnapshot = nil
			}
		case envelope.Type == "summary":
			var entry struct {
				LeafUUID contracts.ID `json:"leafUuid"`
				Summary  string       `json:"summary"`
			}
			if err := json.Unmarshal(line, &entry); err == nil && entry.LeafUUID != "" {
				transcript.Summaries[entry.LeafUUID] = entry.Summary
			}
		case envelope.Type == "custom-title":
			var entry struct {
				SessionID   contracts.ID `json:"sessionId"`
				CustomTitle string       `json:"customTitle"`
			}
			if err := json.Unmarshal(line, &entry); err == nil && entry.SessionID != "" {
				transcript.CustomTitles[entry.SessionID] = entry.CustomTitle
			}
		case envelope.Type == "tag":
			var entry struct {
				SessionID contracts.ID `json:"sessionId"`
				Tag       string       `json:"tag"`
			}
			if err := json.Unmarshal(line, &entry); err == nil && entry.SessionID != "" {
				transcript.Tags[entry.SessionID] = entry.Tag
			}
		case envelope.Type == "content-replacement":
			var entry ContentReplacementEntry
			if err := json.Unmarshal(line, &entry); err == nil && entry.SessionID != "" {
				transcript.ContentReplacements[entry.SessionID] = append(transcript.ContentReplacements[entry.SessionID], entry.Replacements...)
			}
		case envelope.Type == "marble-origami-commit":
			var entry ContextCollapseCommitEntry
			if err := json.Unmarshal(line, &entry); err == nil {
				transcript.ContextCollapseCommits = append(transcript.ContextCollapseCommits, entry)
			}
		case envelope.Type == "marble-origami-snapshot":
			var entry ContextCollapseSnapshotEntry
			if err := json.Unmarshal(line, &entry); err == nil {
				transcript.ContextCollapseSnapshot = &entry
			}
		}
	}
	if err := scanner.Err(); err != nil {
		return Transcript{}, err
	}

	transcript.applyPreservedSegmentRelinks()
	transcript.applySnipRemovals()
	transcript.computeLeafUUIDs()
	return transcript, nil
}

func (m TranscriptMessage) IsCompactBoundary() bool {
	if m.Type != "system" {
		return false
	}
	if m.Subtype == "compact_boundary" {
		return true
	}
	return m.Message != nil && m.Message.Subtype == "compact_boundary"
}

func (t Transcript) BuildConversationChain(leaf contracts.ID) []TranscriptMessage {
	msg := t.Messages[leaf]
	if msg == nil {
		return nil
	}
	var chain []TranscriptMessage
	seen := map[contracts.ID]struct{}{}
	for msg != nil {
		if _, ok := seen[msg.UUID]; ok {
			break
		}
		seen[msg.UUID] = struct{}{}
		chain = append(chain, *msg)
		if msg.ParentUUID == nil {
			break
		}
		msg = t.Messages[*msg.ParentUUID]
	}
	slices.Reverse(chain)
	chain = t.recoverOrphanedParallelToolResults(chain, seen)
	return chain
}

func (t Transcript) recoverOrphanedParallelToolResults(chain []TranscriptMessage, seen map[contracts.ID]struct{}) []TranscriptMessage {
	var chainAssistants []TranscriptMessage
	for _, msg := range chain {
		if msg.Type == "assistant" && assistantMessageID(&msg) != "" {
			chainAssistants = append(chainAssistants, msg)
		}
	}
	if len(chainAssistants) == 0 {
		return chain
	}

	anchorByMsgID := map[string]contracts.ID{}
	for _, msg := range chainAssistants {
		anchorByMsgID[assistantMessageID(&msg)] = msg.UUID
	}

	siblingsByMsgID := map[string][]TranscriptMessage{}
	toolResultsByAsst := map[contracts.ID][]TranscriptMessage{}
	for _, id := range t.Order {
		msg := t.Messages[id]
		if msg == nil {
			continue
		}
		if msg.Type == "assistant" {
			if msgID := assistantMessageID(msg); msgID != "" {
				siblingsByMsgID[msgID] = append(siblingsByMsgID[msgID], *msg)
			}
			continue
		}
		if msg.Type == "user" && msg.ParentUUID != nil && hasToolResultContent(msg) {
			toolResultsByAsst[*msg.ParentUUID] = append(toolResultsByAsst[*msg.ParentUUID], *msg)
		}
	}

	processedGroups := map[string]struct{}{}
	inserts := map[contracts.ID][]TranscriptMessage{}
	recoveredCount := 0
	for _, asst := range chainAssistants {
		msgID := assistantMessageID(&asst)
		if _, ok := processedGroups[msgID]; ok {
			continue
		}
		processedGroups[msgID] = struct{}{}

		group := siblingsByMsgID[msgID]
		if len(group) == 0 {
			group = []TranscriptMessage{asst}
		}
		var orphanedSiblings []TranscriptMessage
		for _, sibling := range group {
			if _, ok := seen[sibling.UUID]; !ok {
				orphanedSiblings = append(orphanedSiblings, sibling)
			}
		}
		var orphanedTRs []TranscriptMessage
		for _, member := range group {
			for _, tr := range toolResultsByAsst[member.UUID] {
				if _, ok := seen[tr.UUID]; !ok {
					orphanedTRs = append(orphanedTRs, tr)
				}
			}
		}
		if len(orphanedSiblings) == 0 && len(orphanedTRs) == 0 {
			continue
		}

		sort.SliceStable(orphanedSiblings, func(i, j int) bool {
			return orphanedSiblings[i].Timestamp < orphanedSiblings[j].Timestamp
		})
		sort.SliceStable(orphanedTRs, func(i, j int) bool {
			return orphanedTRs[i].Timestamp < orphanedTRs[j].Timestamp
		})

		anchor, ok := anchorByMsgID[msgID]
		if !ok {
			continue
		}
		recovered := append(orphanedSiblings, orphanedTRs...)
		for _, msg := range recovered {
			seen[msg.UUID] = struct{}{}
		}
		recoveredCount += len(recovered)
		inserts[anchor] = recovered
	}
	if recoveredCount == 0 {
		return chain
	}

	result := make([]TranscriptMessage, 0, len(chain)+recoveredCount)
	for _, msg := range chain {
		result = append(result, msg)
		if recovered := inserts[msg.UUID]; len(recovered) > 0 {
			result = append(result, recovered...)
		}
	}
	return result
}

func assistantMessageID(msg *TranscriptMessage) string {
	if msg == nil {
		return ""
	}
	if msg.Message != nil && msg.Message.ID != "" {
		return msg.Message.ID
	}
	var raw struct {
		ID      string `json:"id"`
		Message *struct {
			ID string `json:"id"`
		} `json:"message"`
	}
	if len(msg.Raw) == 0 || json.Unmarshal(msg.Raw, &raw) != nil {
		return ""
	}
	if raw.Message != nil && raw.Message.ID != "" {
		return raw.Message.ID
	}
	return raw.ID
}

func hasToolResultContent(msg *TranscriptMessage) bool {
	for _, block := range transcriptContentBlocks(msg) {
		if block.Type == contracts.ContentToolResult {
			return true
		}
	}
	return false
}

func transcriptContentBlocks(msg *TranscriptMessage) []contracts.ContentBlock {
	if msg == nil {
		return nil
	}
	if msg.Message != nil && len(msg.Message.Content) > 0 {
		return msg.Message.Content
	}
	if msg.Content != nil {
		data, err := json.Marshal(msg.Content)
		if err == nil {
			var blocks []contracts.ContentBlock
			if err := json.Unmarshal(data, &blocks); err == nil {
				return blocks
			}
		}
	}
	if len(msg.Raw) == 0 {
		return nil
	}
	var raw struct {
		Content []contracts.ContentBlock `json:"content"`
		Message *struct {
			Content []contracts.ContentBlock `json:"content"`
		} `json:"message"`
	}
	if err := json.Unmarshal(msg.Raw, &raw); err != nil {
		return nil
	}
	if raw.Message != nil && len(raw.Message.Content) > 0 {
		return raw.Message.Content
	}
	return raw.Content
}

func RemoveTranscriptMessageByUUID(path string, target contracts.ID) (bool, error) {
	return RemoveTranscriptMessageByUUIDWithLimit(path, target, MaxTombstoneRewriteBytes)
}

func RemoveTranscriptMessageByUUIDWithLimit(path string, target contracts.ID, maxRewriteBytes int64) (bool, error) {
	if target == "" {
		return false, errors.New("target uuid is empty")
	}
	info, err := os.Stat(path)
	if err != nil {
		if os.IsNotExist(err) {
			return false, nil
		}
		return false, err
	}
	if maxRewriteBytes > 0 && info.Size() > maxRewriteBytes {
		return false, nil
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return false, err
	}
	lines := bytes.SplitAfter(data, []byte("\n"))
	var out []byte
	removed := false
	for _, line := range lines {
		if len(line) == 0 {
			continue
		}
		trimmed := bytes.TrimSpace(line)
		if len(trimmed) == 0 {
			out = append(out, line...)
			continue
		}
		var envelope transcriptEnvelope
		if err := json.Unmarshal(trimmed, &envelope); err == nil && envelope.UUID == target {
			removed = true
			continue
		}
		out = append(out, line...)
	}
	if !removed {
		return false, nil
	}
	return true, os.WriteFile(path, out, info.Mode())
}

func newTranscript() Transcript {
	return Transcript{
		Messages:            map[contracts.ID]*TranscriptMessage{},
		Summaries:           map[contracts.ID]string{},
		CustomTitles:        map[contracts.ID]string{},
		Tags:                map[contracts.ID]string{},
		ContentReplacements: map[contracts.ID][]ContentReplacementRecord{},
		LeafUUIDs:           map[contracts.ID]struct{}{},
	}
}

func (t *Transcript) addMessage(msg *TranscriptMessage) {
	if _, exists := t.Messages[msg.UUID]; !exists {
		t.Order = append(t.Order, msg.UUID)
	}
	t.Messages[msg.UUID] = msg
}

func (t *Transcript) deleteMessage(id contracts.ID) {
	delete(t.Messages, id)
	for i, item := range t.Order {
		if item == id {
			t.Order = append(t.Order[:i], t.Order[i+1:]...)
			return
		}
	}
}

func (t *Transcript) applyPreservedSegmentRelinks() {
	var lastSeg *PreservedSegment
	lastSegBoundaryIdx := -1
	absoluteLastBoundaryIdx := -1
	entryIndex := map[contracts.ID]int{}
	for i, id := range t.Order {
		entryIndex[id] = i
		msg := t.Messages[id]
		if msg == nil || !msg.IsCompactBoundary() {
			continue
		}
		absoluteLastBoundaryIdx = i
		if msg.CompactMetadata != nil && msg.CompactMetadata.PreservedSegment != nil {
			lastSeg = msg.CompactMetadata.PreservedSegment
			lastSegBoundaryIdx = i
		}
	}
	if absoluteLastBoundaryIdx < 0 {
		return
	}
	preserved := map[contracts.ID]struct{}{}
	segIsLive := lastSeg != nil && lastSegBoundaryIdx == absoluteLastBoundaryIdx
	if segIsLive {
		cur := t.Messages[lastSeg.TailUUID]
		seen := map[contracts.ID]struct{}{}
		reachedHead := false
		for cur != nil {
			if _, ok := seen[cur.UUID]; ok {
				break
			}
			seen[cur.UUID] = struct{}{}
			preserved[cur.UUID] = struct{}{}
			if cur.UUID == lastSeg.HeadUUID {
				reachedHead = true
				break
			}
			if cur.ParentUUID == nil {
				break
			}
			cur = t.Messages[*cur.ParentUUID]
		}
		if !reachedHead {
			return
		}
		if head := t.Messages[lastSeg.HeadUUID]; head != nil {
			head.ParentUUID = &lastSeg.AnchorUUID
		}
		for id, msg := range t.Messages {
			if msg.ParentUUID != nil && *msg.ParentUUID == lastSeg.AnchorUUID && id != lastSeg.HeadUUID {
				msg.ParentUUID = &lastSeg.TailUUID
			}
		}
	}
	var toDelete []contracts.ID
	for id, idx := range entryIndex {
		if idx < absoluteLastBoundaryIdx {
			if _, ok := preserved[id]; !ok {
				toDelete = append(toDelete, id)
			}
		}
	}
	for _, id := range toDelete {
		t.deleteMessage(id)
	}
}

func (t *Transcript) applySnipRemovals() {
	toDelete := map[contracts.ID]struct{}{}
	for _, msg := range t.Messages {
		if msg.SnipMetadata == nil {
			continue
		}
		for _, id := range msg.SnipMetadata.RemovedUUIDs {
			toDelete[id] = struct{}{}
		}
	}
	if len(toDelete) == 0 {
		return
	}
	deletedParent := map[contracts.ID]*contracts.ID{}
	for id := range toDelete {
		if msg := t.Messages[id]; msg != nil {
			deletedParent[id] = cloneIDPtr(msg.ParentUUID)
		}
		t.deleteMessage(id)
	}
	resolve := func(start contracts.ID) *contracts.ID {
		var path []contracts.ID
		cur := &start
		for cur != nil {
			if _, ok := toDelete[*cur]; !ok {
				break
			}
			path = append(path, *cur)
			next, ok := deletedParent[*cur]
			if !ok {
				cur = nil
				break
			}
			cur = next
		}
		for _, id := range path {
			deletedParent[id] = cloneIDPtr(cur)
		}
		return cloneIDPtr(cur)
	}
	for _, msg := range t.Messages {
		if msg.ParentUUID == nil {
			continue
		}
		if _, ok := toDelete[*msg.ParentUUID]; ok {
			msg.ParentUUID = resolve(*msg.ParentUUID)
		}
	}
}

func (t *Transcript) computeLeafUUIDs() {
	t.LeafUUIDs = map[contracts.ID]struct{}{}
	parentUUIDs := map[contracts.ID]struct{}{}
	for _, msg := range t.Messages {
		if msg.ParentUUID != nil {
			parentUUIDs[*msg.ParentUUID] = struct{}{}
		}
	}
	for _, id := range t.Order {
		msg := t.Messages[id]
		if msg == nil {
			continue
		}
		if _, hasChild := parentUUIDs[msg.UUID]; hasChild {
			continue
		}
		seen := map[contracts.ID]struct{}{}
		cur := msg
		for cur != nil {
			if _, ok := seen[cur.UUID]; ok {
				break
			}
			seen[cur.UUID] = struct{}{}
			if cur.Type == "user" || cur.Type == "assistant" {
				t.LeafUUIDs[cur.UUID] = struct{}{}
				break
			}
			if cur.ParentUUID == nil {
				break
			}
			cur = t.Messages[*cur.ParentUUID]
		}
	}
}

func isTranscriptType(entryType string) bool {
	switch entryType {
	case "user", "assistant", "attachment", "system":
		return true
	default:
		return false
	}
}

func resolveProgressParent(bridge map[contracts.ID]*contracts.ID, parent *contracts.ID) *contracts.ID {
	if parent == nil {
		return nil
	}
	if resolved, ok := bridge[*parent]; ok {
		return cloneIDPtr(resolved)
	}
	return cloneIDPtr(parent)
}

func cloneIDPtr(in *contracts.ID) *contracts.ID {
	if in == nil {
		return nil
	}
	out := *in
	return &out
}

func NewTranscriptMessage(entryType string, id contracts.ID, parent *contracts.ID) TranscriptMessage {
	return TranscriptMessage{
		Type:       entryType,
		UUID:       id,
		ParentUUID: cloneIDPtr(parent),
		Timestamp:  time.Now().UTC().Format(time.RFC3339Nano),
	}
}
