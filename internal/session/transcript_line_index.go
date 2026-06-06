package session

import (
	"bufio"
	"bytes"
	"encoding/json"
	"io"
	"os"

	"ccgo/internal/contracts"
)

type TranscriptLineRef struct {
	UUID       contracts.ID
	Type       string
	SessionID  contracts.ID
	Timestamp  string
	ParentUUID *contracts.ID
	Offset     int64
	Length     int
}

type TranscriptLineIndex struct {
	Path    string
	Entries []TranscriptLineRef
	ByUUID  map[contracts.ID]int
}

type TranscriptIndexedTail struct {
	Messages   []TranscriptMessage
	StartIndex int
	BytesRead  int64
	HasBefore  bool
}

type TranscriptIndexedChain struct {
	Messages        []TranscriptMessage
	Leaf            contracts.ID
	Found           bool
	BytesRead       int64
	HasBefore       bool
	MissingParent   *contracts.ID
	TruncatedParent *contracts.ID
}

func BuildTranscriptLineIndex(path string) (TranscriptLineIndex, error) {
	f, err := os.Open(path)
	if err != nil {
		if os.IsNotExist(err) {
			return TranscriptLineIndex{Path: path, ByUUID: map[contracts.ID]int{}}, nil
		}
		return TranscriptLineIndex{}, err
	}
	defer f.Close()

	index := TranscriptLineIndex{Path: path, ByUUID: map[contracts.ID]int{}}
	progressBridge := map[contracts.ID]*contracts.ID{}
	reader := bufio.NewReader(f)
	offset := int64(0)
	for {
		line, err := reader.ReadBytes('\n')
		if len(line) > 0 {
			ref, ok := transcriptLineRef(line, offset, progressBridge)
			if ok {
				index.ByUUID[ref.UUID] = len(index.Entries)
				index.Entries = append(index.Entries, ref)
			}
			offset += int64(len(line))
		}
		if err == nil {
			continue
		}
		if err == io.EOF {
			break
		}
		return TranscriptLineIndex{}, err
	}
	return index, nil
}

func LatestTranscriptIndexedLeaf(index TranscriptLineIndex) contracts.ID {
	if len(index.Entries) == 0 {
		return ""
	}
	parentUUIDs := map[contracts.ID]struct{}{}
	for _, ref := range index.Entries {
		if ref.ParentUUID != nil {
			parentUUIDs[*ref.ParentUUID] = struct{}{}
		}
	}
	for i := len(index.Entries) - 1; i >= 0; i-- {
		ref := index.Entries[i]
		if ref.Type != "user" && ref.Type != "assistant" {
			continue
		}
		if _, hasChild := parentUUIDs[ref.UUID]; !hasChild {
			return ref.UUID
		}
	}
	for i := len(index.Entries) - 1; i >= 0; i-- {
		ref := index.Entries[i]
		if ref.Type == "user" || ref.Type == "assistant" {
			return ref.UUID
		}
	}
	return ""
}

func LoadTranscriptIndexedWindow(path string, index TranscriptLineIndex, target contracts.ID, before int, after int) (TranscriptWindow, error) {
	if target == "" {
		return TranscriptWindow{}, nil
	}
	if before < 0 {
		before = 0
	}
	if after < 0 {
		after = 0
	}
	ensureTranscriptLineIndexByUUID(&index)
	targetIndex, ok := index.ByUUID[target]
	if !ok {
		return TranscriptWindow{TargetUUID: target, TargetIndex: -1}, nil
	}
	start := targetIndex - before
	if start < 0 {
		start = 0
	}
	end := targetIndex + after + 1
	if end > len(index.Entries) {
		end = len(index.Entries)
	}
	f, err := os.Open(path)
	if err != nil {
		if os.IsNotExist(err) {
			return TranscriptWindow{TargetUUID: target, TargetIndex: -1}, nil
		}
		return TranscriptWindow{}, err
	}
	defer f.Close()

	window := TranscriptWindow{
		TargetUUID:  target,
		TargetIndex: targetIndex - start,
		Found:       true,
		HasBefore:   start > 0,
		HasAfter:    end < len(index.Entries),
		Messages:    make([]TranscriptMessage, 0, end-start),
	}
	for _, ref := range index.Entries[start:end] {
		window.BytesRead += int64(ref.Length)
		msg, ok, err := readTranscriptMessageRef(f, ref)
		if err != nil {
			return TranscriptWindow{}, err
		}
		if ok {
			window.Messages = append(window.Messages, msg)
		}
	}
	return window, nil
}

func LoadTranscriptIndexedWindowBytes(path string, index TranscriptLineIndex, target contracts.ID, maxBytes int64) (TranscriptWindow, error) {
	if target == "" || maxBytes <= 0 {
		return TranscriptWindow{}, nil
	}
	ensureTranscriptLineIndexByUUID(&index)
	targetIndex, ok := index.ByUUID[target]
	if !ok {
		return TranscriptWindow{TargetUUID: target, TargetIndex: -1}, nil
	}
	start := targetIndex
	end := targetIndex + 1
	bytesRead := int64(index.Entries[targetIndex].Length)
	for {
		grew := false
		if start > 0 {
			refBytes := int64(index.Entries[start-1].Length)
			if bytesRead+refBytes <= maxBytes {
				bytesRead += refBytes
				start--
				grew = true
			}
		}
		if end < len(index.Entries) {
			refBytes := int64(index.Entries[end].Length)
			if bytesRead+refBytes <= maxBytes {
				bytesRead += refBytes
				end++
				grew = true
			}
		}
		if !grew {
			break
		}
	}
	f, err := os.Open(path)
	if err != nil {
		if os.IsNotExist(err) {
			return TranscriptWindow{TargetUUID: target, TargetIndex: -1}, nil
		}
		return TranscriptWindow{}, err
	}
	defer f.Close()

	window := TranscriptWindow{
		TargetUUID:  target,
		TargetIndex: targetIndex - start,
		Found:       true,
		HasBefore:   start > 0,
		HasAfter:    end < len(index.Entries),
		BytesRead:   bytesRead,
		Messages:    make([]TranscriptMessage, 0, end-start),
	}
	for _, ref := range index.Entries[start:end] {
		msg, ok, err := readTranscriptMessageRef(f, ref)
		if err != nil {
			return TranscriptWindow{}, err
		}
		if ok {
			window.Messages = append(window.Messages, msg)
		}
	}
	return window, nil
}

func LoadTranscriptIndexedChain(path string, index TranscriptLineIndex, leaf contracts.ID, maxBytes int64) (TranscriptIndexedChain, error) {
	ensureTranscriptLineIndexByUUID(&index)
	if leaf == "" {
		leaf = LatestTranscriptIndexedLeaf(index)
	}
	if leaf == "" {
		return TranscriptIndexedChain{}, nil
	}
	entryIndex, ok := index.ByUUID[leaf]
	if !ok {
		return TranscriptIndexedChain{Leaf: leaf}, nil
	}
	f, err := os.Open(path)
	if err != nil {
		if os.IsNotExist(err) {
			return TranscriptIndexedChain{Leaf: leaf}, nil
		}
		return TranscriptIndexedChain{}, err
	}
	defer f.Close()

	var refs []TranscriptLineRef
	seen := map[contracts.ID]struct{}{}
	bytesRead := int64(0)
	hasBefore := false
	var missingParent *contracts.ID
	var truncatedParent *contracts.ID
	for {
		ref := index.Entries[entryIndex]
		if _, ok := seen[ref.UUID]; ok {
			hasBefore = true
			break
		}
		refBytes := int64(ref.Length)
		if maxBytes > 0 && len(refs) > 0 && bytesRead+refBytes > maxBytes {
			hasBefore = true
			truncatedParent = cloneIDPtr(&ref.UUID)
			break
		}
		seen[ref.UUID] = struct{}{}
		refs = append(refs, ref)
		bytesRead += refBytes
		if ref.ParentUUID == nil {
			break
		}
		parent := *ref.ParentUUID
		nextIndex, ok := index.ByUUID[parent]
		if !ok {
			hasBefore = true
			missingParent = cloneIDPtr(ref.ParentUUID)
			break
		}
		if _, ok := seen[parent]; ok {
			hasBefore = true
			break
		}
		entryIndex = nextIndex
	}

	messages := make([]TranscriptMessage, 0, len(refs))
	for i := len(refs) - 1; i >= 0; i-- {
		msg, ok, err := readTranscriptMessageRef(f, refs[i])
		if err != nil {
			return TranscriptIndexedChain{}, err
		}
		if ok {
			messages = append(messages, msg)
		}
	}
	return TranscriptIndexedChain{
		Messages:        messages,
		Leaf:            leaf,
		Found:           len(messages) > 0,
		BytesRead:       bytesRead,
		HasBefore:       hasBefore,
		MissingParent:   missingParent,
		TruncatedParent: truncatedParent,
	}, nil
}

func LoadTranscriptIndexedTail(path string, index TranscriptLineIndex, limit int) (TranscriptIndexedTail, error) {
	if limit <= 0 {
		return TranscriptIndexedTail{}, nil
	}
	start := len(index.Entries) - limit
	if start < 0 {
		start = 0
	}
	f, err := os.Open(path)
	if err != nil {
		if os.IsNotExist(err) {
			return TranscriptIndexedTail{}, nil
		}
		return TranscriptIndexedTail{}, err
	}
	defer f.Close()

	tail := TranscriptIndexedTail{
		StartIndex: start,
		HasBefore:  start > 0,
		Messages:   make([]TranscriptMessage, 0, len(index.Entries)-start),
	}
	for _, ref := range index.Entries[start:] {
		tail.BytesRead += int64(ref.Length)
		msg, ok, err := readTranscriptMessageRef(f, ref)
		if err != nil {
			return TranscriptIndexedTail{}, err
		}
		if ok {
			tail.Messages = append(tail.Messages, msg)
		}
	}
	return tail, nil
}

func LoadTranscriptIndexedTailBytes(path string, index TranscriptLineIndex, maxBytes int64) (TranscriptIndexedTail, error) {
	if maxBytes <= 0 {
		return TranscriptIndexedTail{}, nil
	}
	start := len(index.Entries)
	bytesRead := int64(0)
	for start > 0 {
		refBytes := int64(index.Entries[start-1].Length)
		if refBytes <= 0 || bytesRead+refBytes > maxBytes {
			break
		}
		bytesRead += refBytes
		start--
	}
	if start == len(index.Entries) {
		return TranscriptIndexedTail{StartIndex: start, HasBefore: start > 0}, nil
	}
	f, err := os.Open(path)
	if err != nil {
		if os.IsNotExist(err) {
			return TranscriptIndexedTail{}, nil
		}
		return TranscriptIndexedTail{}, err
	}
	defer f.Close()

	tail := TranscriptIndexedTail{
		StartIndex: start,
		BytesRead:  bytesRead,
		HasBefore:  start > 0,
		Messages:   make([]TranscriptMessage, 0, len(index.Entries)-start),
	}
	for _, ref := range index.Entries[start:] {
		msg, ok, err := readTranscriptMessageRef(f, ref)
		if err != nil {
			return TranscriptIndexedTail{}, err
		}
		if ok {
			tail.Messages = append(tail.Messages, msg)
		}
	}
	return tail, nil
}

func ensureTranscriptLineIndexByUUID(index *TranscriptLineIndex) {
	if index.ByUUID != nil {
		return
	}
	index.ByUUID = map[contracts.ID]int{}
	for i, ref := range index.Entries {
		index.ByUUID[ref.UUID] = i
	}
}

func transcriptLineRef(line []byte, offset int64, progressBridge map[contracts.ID]*contracts.ID) (TranscriptLineRef, bool) {
	trimmed := bytes.TrimSpace(line)
	if len(trimmed) == 0 {
		return TranscriptLineRef{}, false
	}
	var envelope transcriptEnvelope
	if err := json.Unmarshal(trimmed, &envelope); err != nil {
		return TranscriptLineRef{}, false
	}
	if contracts.CanonicalMessageType(envelope.Type) == contracts.MessageProgress && envelope.UUID != "" {
		progressBridge[envelope.UUID] = resolveProgressParent(progressBridge, envelope.ParentUUID)
		return TranscriptLineRef{}, false
	}
	if !isTranscriptType(envelope.Type) {
		return TranscriptLineRef{}, false
	}
	var msg TranscriptMessage
	if err := json.Unmarshal(trimmed, &msg); err != nil || msg.UUID == "" {
		return TranscriptLineRef{}, false
	}
	parent := cloneIDPtr(msg.ParentUUID)
	if msg.ParentUUID != nil {
		if bridged, ok := progressBridge[*msg.ParentUUID]; ok {
			parent = cloneIDPtr(bridged)
		}
	}
	return TranscriptLineRef{
		UUID:       msg.UUID,
		Type:       msg.Type,
		SessionID:  msg.SessionID,
		Timestamp:  msg.Timestamp,
		ParentUUID: parent,
		Offset:     offset,
		Length:     len(line),
	}, true
}

func readTranscriptMessageRef(f *os.File, ref TranscriptLineRef) (TranscriptMessage, bool, error) {
	if ref.Length <= 0 {
		return TranscriptMessage{}, false, nil
	}
	line := make([]byte, ref.Length)
	n, err := f.ReadAt(line, ref.Offset)
	if err != nil && err != io.EOF {
		return TranscriptMessage{}, false, err
	}
	line = line[:n]
	var msg TranscriptMessage
	if err := unmarshalTranscriptLine(line, &msg); err != nil || msg.UUID == "" {
		return TranscriptMessage{}, false, nil
	}
	msg.ParentUUID = cloneIDPtr(ref.ParentUUID)
	msg.Raw = append(json.RawMessage(nil), bytes.TrimSpace(line)...)
	return msg, true, nil
}
