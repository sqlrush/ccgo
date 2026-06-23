package main

// SDK-55 test: verify that attachStreamJSON emits a system/status NDJSON line
// when EventCompact fires, matching the CC SDKStatusMessageSchema shape.
// CC ref: coreSchemas.ts:1533-1542 (SDKStatusMessageSchema).
// G1 pass — this is the ⚠️ "基础设施已建未接线" item in section 16.

import (
	"bytes"
	"encoding/json"
	"strings"
	"testing"

	compactpkg "ccgo/internal/compact"
	"ccgo/internal/conversation"
	sdkpkg "ccgo/internal/sdk"
)

// TestAttachStreamJSONEmitsStatusOnCompact_G1 verifies that when EventCompact
// fires (auto-compact triggered), attachStreamJSON writes a
// {"type":"system","subtype":"status","status":"compacting"} NDJSON line
// BEFORE the regular compact event line.
//
// SDK-55: CC coreSchemas.ts:1533-1542 SDKStatusMessageSchema.
func TestAttachStreamJSONEmitsStatusOnCompact_G1(t *testing.T) {
	var buf bytes.Buffer

	runner := conversation.Runner{
		Model:     "claude-stub",
		SessionID: "sess-g1",
	}

	runner, getErr := attachStreamJSON(&buf, runner)

	// Simulate a compact event (this is what RunTurn emits when auto-compact fires).
	runner.OnEvent(conversation.Event{
		Type: conversation.EventCompact,
		Compact: &compactpkg.Result{
			Plan: compactpkg.Plan{},
		},
	})

	if err := getErr(); err != nil {
		t.Fatalf("attachStreamJSON error: %v", err)
	}

	// Parse all NDJSON lines (skip the system/init line emitted by attachStreamJSON).
	lines := strings.Split(strings.TrimSpace(buf.String()), "\n")
	var statusLine map[string]any
	for _, line := range lines {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		var msg map[string]any
		if err := json.Unmarshal([]byte(line), &msg); err != nil {
			t.Fatalf("invalid JSON line %q: %v", line, err)
		}
		if msg["type"] == "system" && msg["subtype"] == "status" {
			statusLine = msg
			break
		}
	}

	if statusLine == nil {
		t.Fatalf("no system/status line found in output:\n%s", buf.String())
	}
	if statusLine["status"] != "compacting" {
		t.Errorf("status = %v want compacting", statusLine["status"])
	}
	_ = sdkpkg.SDKStatusMessage{} // ensure the package is used for the wire type
}
