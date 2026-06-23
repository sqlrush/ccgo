package sdk

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"strings"
	"testing"
	"time"

	"ccgo/internal/api/anthropic"
	"ccgo/internal/contracts"
	"ccgo/internal/conversation"
	"ccgo/internal/tool"
)

// stubMessageClient returns a canned assistant text response — no real API call.
type stubMessageClient struct {
	reply string
	// errOnce, if set, is returned once then cleared (for interrupt tests).
	errOnce error
}

func (s *stubMessageClient) CreateMessage(_ context.Context, _ anthropic.Request) (*anthropic.Response, error) {
	if s.errOnce != nil {
		err := s.errOnce
		s.errOnce = nil
		return nil, err
	}
	return &anthropic.Response{
		ID:         "msg-stub",
		Type:       "message",
		Role:       "assistant",
		Model:      "claude-stub",
		StopReason: "end_turn",
		Content: []contracts.ContentBlock{
			{Type: "text", Text: s.reply},
		},
	}, nil
}

// minimalRunner builds the smallest valid Runner that can complete one turn
// using the given client.
func minimalRunner(client conversation.MessageClient) *conversation.Runner {
	return &conversation.Runner{
		Client:    client,
		Model:     "claude-stub",
		MaxTokens: 256,
		// No Tools, MCP, etc — safe to leave zero for a hermetic turn.
	}
}

// TestQueryRequiresPromptOrFactory verifies validation before any runner is built.
func TestQueryRequiresPromptOrFactory(t *testing.T) {
	// Missing Prompt.
	err := Query(context.Background(), Options{Out: &bytes.Buffer{}})
	if err == nil {
		t.Fatal("Query must return error when Prompt is empty")
	}
	// Missing RunnerFactory.
	err = Query(context.Background(), Options{Prompt: "hi", Out: &bytes.Buffer{}})
	if err == nil {
		t.Fatal("Query must return error when RunnerFactory is nil")
	}
	// Missing Out.
	err = Query(context.Background(), Options{
		Prompt: "hi",
		RunnerFactory: func() (*conversation.Runner, error) {
			return minimalRunner(&stubMessageClient{reply: "yo"}), nil
		},
	})
	if err == nil {
		t.Fatal("Query must return error when Out is nil")
	}
}

// TestQueryRunsOneTurnAndEmitsAssistantEvent verifies that a turn is run and
// the assistant message event is written to Out as NDJSON.
func TestQueryRunsOneTurnAndEmitsAssistantEvent(t *testing.T) {
	var out bytes.Buffer
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	err := Query(ctx, Options{
		Prompt: "hello",
		In:     strings.NewReader(""), // no control requests — clean EOF
		Out:    &out,
		RunnerFactory: func() (*conversation.Runner, error) {
			return minimalRunner(&stubMessageClient{reply: "hi there"}), nil
		},
	})
	if err != nil {
		t.Fatalf("Query returned error: %v", err)
	}
	if out.Len() == 0 {
		t.Fatal("Query produced no output")
	}

	// Verify at least one NDJSON line exists and is valid JSON.
	lines := strings.Split(strings.TrimSpace(out.String()), "\n")
	foundAssistant := false
	for _, line := range lines {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		var msg map[string]any
		if err := json.Unmarshal([]byte(line), &msg); err != nil {
			t.Fatalf("output line is not valid JSON: %q", line)
		}
		if evType, _ := msg["type"].(string); evType == "sdk_event" {
			if payload, ok := msg["request"].(map[string]any); ok {
				if payload["type"] == string(conversation.EventAssistantMessage) {
					foundAssistant = true
				}
			}
		}
	}
	if !foundAssistant {
		t.Fatalf("no assistant_message sdk_event found in output:\n%s", out.String())
	}
}

// TestQueryInterruptCancelsTurn sends an interrupt control_request while the
// turn is running. The turn context must be cancelled and Query must return.
func TestQueryInterruptCancelsTurn(t *testing.T) {
	// Use a pipe so we can write the interrupt after the turn has started.
	pr, pw := io.Pipe()
	defer pw.Close() //nolint:errcheck

	var out bytes.Buffer

	// Block CreateMessage until the interrupt arrives, then return ctx.Err.
	ready := make(chan struct{})
	blockClient := &blockingClient{ready: ready}

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	done := make(chan error, 1)
	go func() {
		done <- Query(ctx, Options{
			Prompt: "interrupt me",
			In:     pr,
			Out:    &out,
			RunnerFactory: func() (*conversation.Runner, error) {
				return minimalRunner(blockClient), nil
			},
		})
	}()

	// Wait until CreateMessage is blocking.
	select {
	case <-ready:
	case <-time.After(3 * time.Second):
		t.Fatal("blockingClient never signalled ready")
	}

	// Send an interrupt control_request.
	interruptLine := `{"type":"control_request","request_id":"i1","request":{"subtype":"interrupt"}}` + "\n"
	if _, err := pw.Write([]byte(interruptLine)); err != nil {
		t.Fatalf("write interrupt: %v", err)
	}
	pw.Close() //nolint:errcheck

	select {
	case err := <-done:
		// Query may return context.Canceled, context.DeadlineExceeded, or an
		// error from the runner when the turn ctx is cancelled.
		_ = err // interrupted turn may or may not error; what matters is it stopped.
	case <-time.After(5 * time.Second):
		t.Fatal("Query did not stop after interrupt")
	}
}

// blockingClient blocks CreateMessage until its ctx is cancelled, so we can
// inject an interrupt while the turn is running.
type blockingClient struct {
	ready chan struct{}
}

func (b *blockingClient) CreateMessage(ctx context.Context, _ anthropic.Request) (*anthropic.Response, error) {
	// Signal that we are blocking.
	select {
	case b.ready <- struct{}{}:
	default:
	}
	// Block until ctx is done.
	<-ctx.Done()
	return nil, ctx.Err()
}

// TestQueryCleanShutdownOnEOF ensures that Query returns cleanly when the
// turn completes and In reaches EOF (no goroutine leak).
func TestQueryCleanShutdownOnEOF(t *testing.T) {
	var out bytes.Buffer
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	err := Query(ctx, Options{
		Prompt: "ping",
		In:     strings.NewReader(""), // immediate EOF
		Out:    &out,
		RunnerFactory: func() (*conversation.Runner, error) {
			return minimalRunner(&stubMessageClient{reply: "pong"}), nil
		},
	})
	if err != nil {
		t.Fatalf("clean shutdown: unexpected error: %v", err)
	}
}

// TestQueryCtxCancelStopsQuery verifies that cancelling the parent ctx causes
// Query to return (even if the turn is blocking).
func TestQueryCtxCancelStopsQuery(t *testing.T) {
	pr, pw := io.Pipe()
	defer pw.Close() //nolint:errcheck
	defer pr.Close() //nolint:errcheck

	var out bytes.Buffer
	ready := make(chan struct{})
	blockClient := &blockingClient{ready: ready}

	ctx, cancel := context.WithCancel(context.Background())

	done := make(chan error, 1)
	go func() {
		done <- Query(ctx, Options{
			Prompt: "cancel me",
			In:     pr,
			Out:    &out,
			RunnerFactory: func() (*conversation.Runner, error) {
				return minimalRunner(blockClient), nil
			},
		})
	}()

	select {
	case <-ready:
	case <-time.After(3 * time.Second):
		t.Fatal("blockingClient never signalled ready")
	}

	cancel()

	select {
	case err := <-done:
		if !errors.Is(err, context.Canceled) {
			t.Logf("Query returned %v (may be wrapped runner error, acceptable)", err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("Query did not stop after ctx cancel")
	}
}

// ── F1-C06: keep_alive / control_cancel_request / update_environment_variables ─

// TestDispatchLineKeepAliveIsIgnored verifies that a keep_alive message does
// not produce a response and does not crash.
// CC ref: controlSchemas.ts:621-627.
func TestDispatchLineKeepAliveIsIgnored(t *testing.T) {
	var out bytes.Buffer
	enc := NewEncoder(&out)
	ctrl := NewController(nil, nil)
	asker := newControlAsker(enc.WriteRequest, func() string { return "x" })

	dispatchLine(`{"type":"keep_alive"}`, ctrl, asker, enc, nil)
	// No output should be written.
	if out.Len() != 0 {
		t.Fatalf("keep_alive must produce no output, got: %q", out.String())
	}
}

// TestDispatchLineUpdateEnvironmentVariablesIsIgnored verifies that an
// update_environment_variables message is silently accepted.
// CC ref: controlSchemas.ts:629-636.
func TestDispatchLineUpdateEnvironmentVariablesIsIgnored(t *testing.T) {
	var out bytes.Buffer
	enc := NewEncoder(&out)
	ctrl := NewController(nil, nil)
	asker := newControlAsker(enc.WriteRequest, func() string { return "x" })

	dispatchLine(`{"type":"update_environment_variables","variables":{"FOO":"bar"}}`, ctrl, asker, enc, nil)
	if out.Len() != 0 {
		t.Fatalf("update_environment_variables must produce no output, got: %q", out.String())
	}
}

// TestDispatchLineControlCancelRequestCancelsAsker verifies that a
// control_cancel_request with a pending request_id delivers a deny decision
// to the waiting Ask() call.
// CC ref: controlSchemas.ts:612-619.
func TestDispatchLineControlCancelRequestCancelsAsker(t *testing.T) {
	var out bytes.Buffer
	enc := NewEncoder(&out)
	ctrl := NewController(nil, nil)

	idGen := func() string { return "req-77" }
	asker := newControlAsker(enc.WriteRequest, idGen)

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	// Start an Ask in a goroutine (will block waiting for a response).
	decisionCh := make(chan contracts.PermissionDecision, 1)
	errCh := make(chan error, 1)
	go func() {
		d, err := asker.Ask(ctx, tool.PermissionAskRequest{ToolName: "Bash", ToolUseID: "u-77"})
		if err != nil {
			errCh <- err
			return
		}
		decisionCh <- d
	}()

	// Drain the can_use_tool request emitted on out so the asker can proceed.
	outDec := NewDecoder(&out)
	// Wait briefly for the request to appear.
	time.Sleep(50 * time.Millisecond)
	_ = outDec // request was emitted; we don't need to read it for this test.

	// Send a control_cancel_request for the pending request_id.
	dispatchLine(`{"type":"control_cancel_request","request_id":"req-77"}`, ctrl, asker, enc, nil)

	// The Ask should unblock with a deny.
	select {
	case d := <-decisionCh:
		if d.Behavior != contracts.PermissionDeny {
			t.Fatalf("expected deny after cancel, got %v", d.Behavior)
		}
	case err := <-errCh:
		t.Fatalf("Ask returned error: %v", err)
	case <-time.After(3 * time.Second):
		t.Fatal("Ask did not unblock after control_cancel_request")
	}
}

// TestSDKStatusMessageShape verifies the SDKStatusMessage struct serialises to
// the correct CC-wire shape (type=="system", subtype=="status").
// CC ref: coreSchemas.ts:1533-1542 (SDKStatusMessageSchema).
func TestSDKStatusMessageShape(t *testing.T) {
	msg := SDKStatusMessage{
		Type:      "system",
		Subtype:   "status",
		Status:    "compacting",
		SessionID: "sess-1",
	}
	data, err := json.Marshal(msg)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	var got map[string]any
	if err := json.Unmarshal(data, &got); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if got["type"] != "system" {
		t.Errorf("type = %v want system", got["type"])
	}
	if got["subtype"] != "status" {
		t.Errorf("subtype = %v want status", got["subtype"])
	}
	if got["status"] != "compacting" {
		t.Errorf("status = %v want compacting", got["status"])
	}
	if got["session_id"] != "sess-1" {
		t.Errorf("session_id = %v want sess-1", got["session_id"])
	}
}

// TestEncoderWriteEventEmitsNDJSON verifies that WriteEvent serialises any value
// as a single NDJSON line ending with \n.
func TestEncoderWriteEventEmitsNDJSON(t *testing.T) {
	var buf bytes.Buffer
	enc := NewEncoder(&buf)
	msg := SDKStatusMessage{Type: "system", Subtype: "status", Status: "idle"}
	if err := enc.WriteEvent(msg); err != nil {
		t.Fatalf("WriteEvent: %v", err)
	}
	line := buf.String()
	if !strings.HasSuffix(line, "\n") {
		t.Errorf("WriteEvent output must end with newline, got %q", line)
	}
	var got map[string]any
	if err := json.Unmarshal([]byte(strings.TrimSpace(line)), &got); err != nil {
		t.Errorf("WriteEvent output is not valid JSON: %q", line)
	}
}

// TestQueryCanUseTool_ResponseDeliveredToAsker verifies the can_use_tool
// round-trip: when a tool triggers Ask, the asker emits a control_request and
// the read-loop delivers the matching control_response to asker.Resolve.
//
// This test exercises the composition directly (bypassing RunTurn) to keep it
// hermetic and race-free.
func TestQueryCanUseTool_ResponseDeliveredToAsker(t *testing.T) {
	// Wire In/Out via pipes.
	// SDK client → Out: receives the can_use_tool request emitted by the asker.
	// SDK client → In: we write the matching control_response.
	outPR, outPW := io.Pipe()
	inPR, inPW := io.Pipe()

	enc := NewEncoder(outPW)

	var reqCounter int64
	nextID := func() string {
		reqCounter++
		return "ct-1"
	}
	asker := newControlAsker(enc.WriteRequest, nextID)

	ctrl := NewController(func() {}, func(string) error { return nil })

	// Start the read-loop.
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	go readControlLoop(ctx, inPR, ctrl, asker, enc, nil)

	// Trigger Ask in a goroutine (simulates a tool calling PermissionAsker.Ask).
	decisionCh := make(chan contracts.PermissionDecision, 1)
	errCh := make(chan error, 1)
	go func() {
		d, err := asker.Ask(ctx, tool.PermissionAskRequest{
			ToolName:  "Bash",
			ToolUseID: "u-1",
		})
		if err != nil {
			errCh <- err
			return
		}
		decisionCh <- d
	}()

	// Read the control_request emitted on Out (the can_use_tool request).
	outDec := NewDecoder(outPR)
	req, err := outDec.Next()
	if err != nil {
		t.Fatalf("read can_use_tool request: %v", err)
	}
	if req.Subtype() != "can_use_tool" {
		t.Fatalf("subtype = %q want can_use_tool", req.Subtype())
	}
	requestID := req.RequestID
	if requestID == "" {
		t.Fatal("request_id must not be empty")
	}

	// Write the matching control_response back on In.
	responseJSON, _ := json.Marshal(ControlResponse{
		Type: "control_response",
		Response: ControlResponseBody{
			Subtype:   "success",
			RequestID: requestID,
			Response: map[string]any{
				"behavior": string(contracts.PermissionAllow),
			},
		},
	})
	if _, err := inPW.Write(append(responseJSON, '\n')); err != nil {
		t.Fatalf("write control_response: %v", err)
	}
	inPW.Close() //nolint:errcheck

	// The asker.Ask must unblock with an allow decision.
	select {
	case d := <-decisionCh:
		if d.Behavior != contracts.PermissionAllow {
			t.Fatalf("behavior = %v want allow", d.Behavior)
		}
	case err := <-errCh:
		t.Fatalf("Ask returned error: %v", err)
	case <-time.After(5 * time.Second):
		t.Fatal("Ask did not unblock after control_response")
	}
}

