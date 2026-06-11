package session

import (
	"bytes"
	"encoding/json"
	"strings"
)

const maxTranscriptRecordUnwrapDepth = 8

var transcriptRecordWrapperKeys = []string{
	"data",
	"payload",
	"body",
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
}

var transcriptRecordCollectionKeys = []string{
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

func transcriptRecordLines(line []byte) [][]byte {
	line = bytes.TrimSpace(line)
	if len(line) == 0 {
		return nil
	}
	records := unwrapTranscriptRecordLine(line, 0)
	if len(records) == 0 {
		return [][]byte{append([]byte(nil), line...)}
	}

	out := make([][]byte, 0, len(records))
	seen := map[string]struct{}{}
	for _, record := range records {
		record = bytes.TrimSpace(record)
		if len(record) == 0 {
			continue
		}
		key := string(record)
		if _, ok := seen[key]; ok {
			continue
		}
		seen[key] = struct{}{}
		out = append(out, append([]byte(nil), record...))
	}
	if len(out) == 0 {
		return [][]byte{append([]byte(nil), line...)}
	}
	return out
}

func unwrapTranscriptRecordLine(raw json.RawMessage, depth int) []json.RawMessage {
	raw = bytes.TrimSpace(raw)
	if len(raw) == 0 {
		return nil
	}
	if depth >= maxTranscriptRecordUnwrapDepth {
		return []json.RawMessage{append(json.RawMessage(nil), raw...)}
	}

	var values []json.RawMessage
	if err := json.Unmarshal(raw, &values); err == nil {
		var out []json.RawMessage
		for _, value := range values {
			out = append(out, unwrapTranscriptRecordLine(value, depth+1)...)
		}
		return out
	}

	object, ok := transcriptRecordObject(raw)
	if !ok {
		return []json.RawMessage{append(json.RawMessage(nil), raw...)}
	}

	if transcriptRecordObjectType(object) != "" {
		if merged, ok := mergeTranscriptResourceRecord(object); ok {
			return []json.RawMessage{merged}
		}
		return []json.RawMessage{append(json.RawMessage(nil), raw...)}
	}

	var out []json.RawMessage
	for _, key := range transcriptRecordWrapperKeys {
		child, ok := object[key]
		if !ok || !isTranscriptRecordWrapperPayload(child) {
			continue
		}
		out = append(out, unwrapTranscriptRecordLine(child, depth+1)...)
	}
	for _, key := range transcriptRecordCollectionKeys {
		child, ok := object[key]
		if !ok || !isTranscriptRecordWrapperPayload(child) {
			continue
		}
		out = append(out, unwrapTranscriptRecordLine(child, depth+1)...)
	}
	if len(out) > 0 {
		return out
	}
	return []json.RawMessage{append(json.RawMessage(nil), raw...)}
}

func mergeTranscriptResourceRecord(object map[string]json.RawMessage) (json.RawMessage, bool) {
	attributes, ok := transcriptRecordAttributes(object)
	if !ok {
		return nil, false
	}
	merged := make(map[string]json.RawMessage, len(attributes)+len(object))
	for key, value := range attributes {
		merged[key] = append(json.RawMessage(nil), value...)
	}
	for key, value := range object {
		if isTranscriptRecordStructuralKey(key) {
			continue
		}
		if _, exists := merged[key]; exists {
			continue
		}
		merged[key] = append(json.RawMessage(nil), value...)
	}
	if transcriptRecordObjectType(merged) == "" {
		return nil, false
	}
	data, err := json.Marshal(merged)
	if err != nil {
		return nil, false
	}
	return data, true
}

func transcriptRecordAttributes(object map[string]json.RawMessage) (map[string]json.RawMessage, bool) {
	for _, key := range []string{"attributes", "properties", "attrs"} {
		child, ok := object[key]
		if !ok {
			continue
		}
		if attributes, ok := transcriptRecordObject(child); ok {
			return attributes, true
		}
	}
	return nil, false
}

func transcriptRecordObject(raw json.RawMessage) (map[string]json.RawMessage, bool) {
	var object map[string]json.RawMessage
	if err := json.Unmarshal(bytes.TrimSpace(raw), &object); err != nil {
		return nil, false
	}
	return object, true
}

func transcriptRecordObjectType(object map[string]json.RawMessage) string {
	fields := transcriptMetadataFields(object)
	for _, key := range []string{"type", "entryType", "entry_type", "messageType", "message_type", "role"} {
		raw, ok := transcriptMetadataFieldRaw(fields, key)
		if !ok {
			continue
		}
		var value string
		if err := json.Unmarshal(raw, &value); err == nil && strings.TrimSpace(value) != "" {
			return strings.TrimSpace(value)
		}
	}
	return ""
}

func isTranscriptRecordWrapperPayload(raw json.RawMessage) bool {
	raw = bytes.TrimSpace(raw)
	return len(raw) > 0 && (raw[0] == '{' || raw[0] == '[')
}

func isTranscriptRecordStructuralKey(key string) bool {
	switch key {
	case "attributes", "properties", "attrs", "relationships", "links":
		return true
	}
	for _, structuralKey := range transcriptRecordWrapperKeys {
		if key == structuralKey {
			return true
		}
	}
	for _, structuralKey := range transcriptRecordCollectionKeys {
		if key == structuralKey {
			return true
		}
	}
	return false
}
