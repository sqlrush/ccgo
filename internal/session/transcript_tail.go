package session

import (
	"encoding/json"
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
