package sdk

import (
	"bytes"
	"errors"
	"io"
	"strings"
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
