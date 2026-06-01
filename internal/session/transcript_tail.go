package session

import (
	"bufio"
	"bytes"
	"encoding/json"
	"os"

	"ccgo/internal/contracts"
)

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
		if !isTranscriptType(envelope.Type) {
			continue
		}
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
