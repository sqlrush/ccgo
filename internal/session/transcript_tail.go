package session

import (
	"bufio"
	"bytes"
	"encoding/json"
	"io"
	"os"

	"ccgo/internal/contracts"
)

type TranscriptWindow struct {
	Messages    []TranscriptMessage
	TargetUUID  contracts.ID
	TargetIndex int
	Found       bool
	HasBefore   bool
	HasAfter    bool
}

type TranscriptByteTail struct {
	Messages    []TranscriptMessage
	StartOffset int64
	BytesRead   int64
	HasBefore   bool
}

func LoadTranscriptTail(path string, limit int) ([]TranscriptMessage, error) {
	if limit <= 0 {
		return nil, nil
	}
	f, err := os.Open(path)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, err
	}
	defer f.Close()

	progressBridge := map[contracts.ID]*contracts.ID{}
	ring := make([]TranscriptMessage, 0, limit)
	seen := 0

	scanner := newTranscriptScanner(f)
	for scanner.Scan() {
		var envelope transcriptEnvelope
		if err := unmarshalTranscriptLine(scanner.Bytes(), &envelope); err != nil {
			continue
		}
		if envelope.Type == "progress" && envelope.UUID != "" {
			progressBridge[envelope.UUID] = resolveProgressParent(progressBridge, envelope.ParentUUID)
			continue
		}
		if !isTranscriptType(envelope.Type) {
			continue
		}
		var msg TranscriptMessage
		if err := unmarshalTranscriptLine(scanner.Bytes(), &msg); err != nil || msg.UUID == "" {
			continue
		}
		if msg.ParentUUID != nil {
			if bridged, ok := progressBridge[*msg.ParentUUID]; ok {
				msg.ParentUUID = cloneIDPtr(bridged)
			}
		}
		msg.Raw = append(json.RawMessage(nil), scanner.Bytes()...)
		if len(ring) < limit {
			ring = append(ring, msg)
		} else {
			ring[seen%limit] = msg
		}
		seen++
	}
	if err := scanner.Err(); err != nil {
		return nil, err
	}
	if seen <= limit {
		return ring, nil
	}
	start := seen % limit
	out := make([]TranscriptMessage, 0, limit)
	out = append(out, ring[start:]...)
	out = append(out, ring[:start]...)
	return out, nil
}

func LoadTranscriptTailBytes(path string, maxBytes int64) (TranscriptByteTail, error) {
	if maxBytes <= 0 {
		return TranscriptByteTail{}, nil
	}
	f, err := os.Open(path)
	if err != nil {
		if os.IsNotExist(err) {
			return TranscriptByteTail{}, nil
		}
		return TranscriptByteTail{}, err
	}
	defer f.Close()

	info, err := f.Stat()
	if err != nil {
		return TranscriptByteTail{}, err
	}
	size := info.Size()
	start := int64(0)
	if size > maxBytes {
		start = size - maxBytes
	}
	actualStart := start
	hasBefore := start > 0
	if _, err := f.Seek(start, io.SeekStart); err != nil {
		return TranscriptByteTail{}, err
	}
	reader := bufio.NewReader(f)
	if start > 0 && !offsetStartsLine(f, start) {
		discarded, err := reader.ReadBytes('\n')
		actualStart += int64(len(discarded))
		if err != nil {
			if err == io.EOF {
				return TranscriptByteTail{StartOffset: size, HasBefore: true}, nil
			}
			return TranscriptByteTail{}, err
		}
	}

	progressBridge := map[contracts.ID]*contracts.ID{}
	scanner := bufio.NewScanner(reader)
	buf := make([]byte, 0, 1024*1024)
	scanner.Buffer(buf, 50*1024*1024)
	var messages []TranscriptMessage
	for scanner.Scan() {
		line := bytes.TrimSpace(scanner.Bytes())
		if len(line) == 0 {
			continue
		}
		msg, ok := transcriptMessageFromLine(line, progressBridge)
		if ok {
			messages = append(messages, msg)
		}
	}
	if err := scanner.Err(); err != nil {
		return TranscriptByteTail{}, err
	}
	if actualStart > size {
		actualStart = size
	}
	return TranscriptByteTail{
		Messages:    messages,
		StartOffset: actualStart,
		BytesRead:   size - actualStart,
		HasBefore:   hasBefore,
	}, nil
}

func LoadTranscriptWindow(path string, target contracts.ID, before int, after int) (TranscriptWindow, error) {
	if target == "" {
		return TranscriptWindow{}, nil
	}
	if before < 0 {
		before = 0
	}
	if after < 0 {
		after = 0
	}
	f, err := os.Open(path)
	if err != nil {
		if os.IsNotExist(err) {
			return TranscriptWindow{TargetUUID: target}, nil
		}
		return TranscriptWindow{}, err
	}
	defer f.Close()

	progressBridge := map[contracts.ID]*contracts.ID{}
	prior := make([]TranscriptMessage, 0, before)
	seenBefore := 0
	window := TranscriptWindow{TargetUUID: target, TargetIndex: -1}
	remainingAfter := 0

	scanner := newTranscriptScanner(f)
	for scanner.Scan() {
		msg, ok := transcriptMessageFromLine(scanner.Bytes(), progressBridge)
		if !ok {
			continue
		}
		if window.Found {
			if remainingAfter > 0 {
				window.Messages = append(window.Messages, msg)
				remainingAfter--
				continue
			}
			window.HasAfter = true
			break
		}
		if msg.UUID == target {
			window.Found = true
			window.HasBefore = seenBefore > len(prior)
			window.Messages = append(window.Messages, prior...)
			window.TargetIndex = len(window.Messages)
			window.Messages = append(window.Messages, msg)
			remainingAfter = after
			continue
		}
		seenBefore++
		if before == 0 {
			continue
		}
		if len(prior) < before {
			prior = append(prior, msg)
		} else {
			copy(prior, prior[1:])
			prior[len(prior)-1] = msg
		}
	}
	if err := scanner.Err(); err != nil {
		return TranscriptWindow{}, err
	}
	return window, nil
}

func offsetStartsLine(f *os.File, offset int64) bool {
	if offset <= 0 {
		return true
	}
	var previous [1]byte
	n, err := f.ReadAt(previous[:], offset-1)
	return err == nil && n == 1 && previous[0] == '\n'
}

func transcriptMessageFromLine(line []byte, progressBridge map[contracts.ID]*contracts.ID) (TranscriptMessage, bool) {
	var envelope transcriptEnvelope
	if err := unmarshalTranscriptLine(line, &envelope); err != nil {
		return TranscriptMessage{}, false
	}
	if envelope.Type == "progress" && envelope.UUID != "" {
		progressBridge[envelope.UUID] = resolveProgressParent(progressBridge, envelope.ParentUUID)
		return TranscriptMessage{}, false
	}
	if !isTranscriptType(envelope.Type) {
		return TranscriptMessage{}, false
	}
	var msg TranscriptMessage
	if err := unmarshalTranscriptLine(line, &msg); err != nil || msg.UUID == "" {
		return TranscriptMessage{}, false
	}
	if msg.ParentUUID != nil {
		if bridged, ok := progressBridge[*msg.ParentUUID]; ok {
			msg.ParentUUID = cloneIDPtr(bridged)
		}
	}
	msg.Raw = append(json.RawMessage(nil), line...)
	return msg, true
}
