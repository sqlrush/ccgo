package sdk

import (
	"bytes"
	"encoding/json"
	"errors"
	"io"
	"strings"
	"sync"
	"testing"
)

func TestDecodeControlRequest(t *testing.T) {
	in := `{"type":"control_request","request_id":"r1","request":{"subtype":"interrupt"}}` + "\n"
	dec := NewDecoder(strings.NewReader(in))
	req, err := dec.Next()
	if err != nil {
		t.Fatalf("Next err: %v", err)
	}
	if req.Type != "control_request" || req.RequestID != "r1" {
		t.Fatalf("req = %+v", req)
	}
	if req.Subtype() != "interrupt" {
		t.Fatalf("subtype = %q want interrupt", req.Subtype())
	}
	if _, err := dec.Next(); !errors.Is(err, io.EOF) {
		t.Fatalf("expected EOF, got %v", err)
	}
}

func TestEncodeSuccessResponse(t *testing.T) {
	var buf bytes.Buffer
	enc := NewEncoder(&buf)
	if err := enc.WriteResponse(SuccessResponse("r1", map[string]any{"model": "opus"})); err != nil {
		t.Fatalf("WriteResponse err: %v", err)
	}
	out := buf.String()
	for _, want := range []string{`"type":"control_response"`, `"subtype":"success"`, `"request_id":"r1"`, `"model":"opus"`} {
		if !strings.Contains(out, want) {
			t.Fatalf("output %q missing %q", out, want)
		}
	}
	if !strings.HasSuffix(out, "\n") {
		t.Fatal("NDJSON responses must be newline-terminated")
	}
}

func TestEncodeErrorResponse(t *testing.T) {
	var buf bytes.Buffer
	enc := NewEncoder(&buf)
	if err := enc.WriteResponse(ErrorResponse("r2", "denied")); err != nil {
		t.Fatal(err)
	}
	out := buf.String()
	if !strings.Contains(out, `"subtype":"error"`) || !strings.Contains(out, `"error":"denied"`) {
		t.Fatalf("error response shape wrong: %q", out)
	}
}

func TestRoundTrip(t *testing.T) {
	orig := ControlRequest{
		Type:      "control_request",
		RequestID: "rt1",
		Request:   map[string]any{"subtype": "set_model", "model": "sonnet"},
	}
	var buf bytes.Buffer
	enc := NewEncoder(&buf)
	if err := enc.WriteRequest(orig); err != nil {
		t.Fatalf("WriteRequest err: %v", err)
	}
	dec := NewDecoder(&buf)
	got, err := dec.Next()
	if err != nil {
		t.Fatalf("Next err: %v", err)
	}
	if got.Type != orig.Type || got.RequestID != orig.RequestID {
		t.Fatalf("round-trip mismatch: got %+v", got)
	}
	if got.Subtype() != "set_model" {
		t.Fatalf("subtype mismatch: got %q", got.Subtype())
	}
	if _, err := dec.Next(); !errors.Is(err, io.EOF) {
		t.Fatalf("expected EOF after round-trip, got %v", err)
	}
}

func TestDecoderMultipleLines(t *testing.T) {
	lines := `{"type":"control_request","request_id":"a","request":{"subtype":"interrupt"}}` + "\n" +
		`{"type":"control_request","request_id":"b","request":{"subtype":"set_model"}}` + "\n"
	dec := NewDecoder(strings.NewReader(lines))

	r1, err := dec.Next()
	if err != nil || r1.RequestID != "a" {
		t.Fatalf("first Next: err=%v req=%+v", err, r1)
	}
	r2, err := dec.Next()
	if err != nil || r2.RequestID != "b" {
		t.Fatalf("second Next: err=%v req=%+v", err, r2)
	}
	if _, err := dec.Next(); !errors.Is(err, io.EOF) {
		t.Fatalf("expected EOF, got %v", err)
	}
}

func TestDecoderMalformedLine(t *testing.T) {
	in := "not-json\n"
	dec := NewDecoder(strings.NewReader(in))
	_, err := dec.Next()
	if err == nil {
		t.Fatal("expected error for malformed JSON, got nil")
	}
}

// TestConcurrentEncoderWriteRace verifies that concurrent WriteRequest and
// WriteResponse calls do not race or corrupt NDJSON output.
// Run with: go test -race ./internal/sdk/
func TestConcurrentEncoderWriteRace(t *testing.T) {
	var buf bytes.Buffer
	enc := NewEncoder(&buf)

	// Spawn multiple goroutines, each issuing many writes concurrently.
	// With the mutex in place, this should be race-clean and produce
	// valid, non-interleaved NDJSON.
	const numGoroutines = 4
	const writesPerGoroutine = 50
	var wg sync.WaitGroup
	wg.Add(numGoroutines)

	for i := 0; i < numGoroutines; i++ {
		go func(id int) {
			defer wg.Done()
			for j := 0; j < writesPerGoroutine; j++ {
				// Alternate between WriteRequest and WriteResponse to maximize contention.
				if (id+j)%2 == 0 {
					_ = enc.WriteRequest(ControlRequest{
						Type:      "control_request",
						RequestID: "req-id",
						Request:   map[string]any{"subtype": "test"},
					})
				} else {
					_ = enc.WriteResponse(SuccessResponse("resp-id", map[string]any{"status": "ok"}))
				}
			}
		}(i)
	}
	wg.Wait()

	// Parse the output as NDJSON: split by newlines and validate each line is
	// a complete, parseable JSON object. Interleaved writes would produce
	// malformed lines or partial JSON.
	output := buf.String()
	lines := strings.Split(strings.TrimSuffix(output, "\n"), "\n")
	if len(lines) != numGoroutines*writesPerGoroutine {
		t.Fatalf("expected %d lines, got %d", numGoroutines*writesPerGoroutine, len(lines))
	}

	var lineCount int
	for _, line := range lines {
		if line == "" {
			continue
		}
		lineCount++

		// Each line must be valid JSON and complete (not truncated).
		var obj map[string]any
		if err := json.Unmarshal([]byte(line), &obj); err != nil {
			t.Fatalf("line %d not valid JSON: %q (err: %v)", lineCount, line, err)
		}

		// Verify the line has expected structure (either control_request or control_response).
		msgType, ok := obj["type"].(string)
		if !ok || (msgType != "control_request" && msgType != "control_response") {
			t.Fatalf("line %d has invalid type: %+v", lineCount, obj)
		}
	}

	if lineCount != numGoroutines*writesPerGoroutine {
		t.Fatalf("expected %d valid JSON lines, parsed %d", numGoroutines*writesPerGoroutine, lineCount)
	}
}
