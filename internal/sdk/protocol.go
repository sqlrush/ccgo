// Package sdk implements the SDK control protocol framing.
// Wire format matches CC controlSchemas.ts:578-610 and structuredIO.ts:465-466.
// All JSON field names are snake_case to match the CC SDK wire contract exactly.
package sdk

import (
	"bufio"
	"encoding/json"
	"fmt"
	"io"
	"strings"
)

// ControlRequest is an inbound SDK control message (controlSchemas.ts:578-584).
// Wire: {"type":"control_request","request_id":"...","request":{...}}
type ControlRequest struct {
	Type      string         `json:"type"`
	RequestID string         `json:"request_id"`
	Request   map[string]any `json:"request"`
}

// Subtype returns the request subtype field (e.g. "interrupt", "set_model", "can_use_tool").
// Returns "" if Request is nil or the subtype key is absent/non-string.
func (r ControlRequest) Subtype() string {
	if r.Request == nil {
		return ""
	}
	s, _ := r.Request["subtype"].(string)
	return s
}

// ControlResponseBody is the inner body of a control_response (controlSchemas.ts:586-603).
// Wire (success): {"subtype":"success","request_id":"...","response":{...}}
// Wire (error):   {"subtype":"error","request_id":"...","error":"..."}
type ControlResponseBody struct {
	Subtype   string         `json:"subtype"`
	RequestID string         `json:"request_id"`
	Response  map[string]any `json:"response,omitempty"`
	Error     string         `json:"error,omitempty"`
}

// ControlResponse is an outbound control_response envelope (controlSchemas.ts:605-610).
// Wire: {"type":"control_response","response":{...}}
type ControlResponse struct {
	Type     string              `json:"type"`
	Response ControlResponseBody `json:"response"`
}

// SuccessResponse constructs a control_response with subtype "success".
// payload is placed in the nested response field; nil payload omits the field.
func SuccessResponse(requestID string, payload map[string]any) ControlResponse {
	return ControlResponse{
		Type: "control_response",
		Response: ControlResponseBody{
			Subtype:   "success",
			RequestID: requestID,
			Response:  payload,
		},
	}
}

// ErrorResponse constructs a control_response with subtype "error".
func ErrorResponse(requestID, msg string) ControlResponse {
	return ControlResponse{
		Type: "control_response",
		Response: ControlResponseBody{
			Subtype:   "error",
			RequestID: requestID,
			Error:     msg,
		},
	}
}

// Decoder reads NDJSON control_request messages from a stream.
// Blank lines are silently skipped; malformed JSON is returned as an error.
type Decoder struct {
	r *bufio.Reader
}

// NewDecoder wraps r in a buffered NDJSON decoder.
func NewDecoder(r io.Reader) *Decoder {
	return &Decoder{r: bufio.NewReader(r)}
}

// Next returns the next ControlRequest from the stream.
// Returns io.EOF when the stream is exhausted (no more lines).
// Returns a wrapped error for malformed JSON lines.
func (d *Decoder) Next() (ControlRequest, error) {
	for {
		line, err := d.r.ReadString('\n')
		trimmed := strings.TrimSpace(line)
		if trimmed != "" {
			var req ControlRequest
			if jerr := json.Unmarshal([]byte(trimmed), &req); jerr != nil {
				return ControlRequest{}, fmt.Errorf("sdk: malformed control_request: %w", jerr)
			}
			return req, nil
		}
		if err != nil {
			// Propagate io.EOF directly so callers can use errors.Is(err, io.EOF).
			return ControlRequest{}, err
		}
	}
}

// Encoder writes NDJSON control messages to a stream.
// Each message is serialised as a single JSON object followed by a newline,
// matching CC's ndjsonSafeStringify(message) + '\n' (structuredIO.ts:466).
type Encoder struct {
	enc *json.Encoder
}

// NewEncoder wraps w in an NDJSON encoder. json.Encoder appends '\n' after each Encode call.
func NewEncoder(w io.Writer) *Encoder {
	return &Encoder{enc: json.NewEncoder(w)}
}

// WriteResponse serialises resp as a single NDJSON line.
func (e *Encoder) WriteResponse(resp ControlResponse) error {
	if err := e.enc.Encode(resp); err != nil {
		return fmt.Errorf("sdk: write control_response: %w", err)
	}
	return nil
}

// WriteRequest serialises req as a single NDJSON line.
func (e *Encoder) WriteRequest(req ControlRequest) error {
	if err := e.enc.Encode(req); err != nil {
		return fmt.Errorf("sdk: write control_request: %w", err)
	}
	return nil
}
