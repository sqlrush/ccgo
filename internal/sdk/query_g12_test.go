package sdk

// G12 tests: verify async hook queue runtime wiring and SDK-35/43/49
// async/cancel subtypes.
//
// CC refs: docs/cc-parity/sections/16-sdk.md SDK-35/43/49.
//          docs/cc-parity/sections/10-hooks.md HOOK-12 runtime.

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"ccgo/internal/conversation"
	hookpkg "ccgo/internal/hooks"
	"ccgo/internal/orchestration"
)

// ── SDK-35: cancel_async_message ─────────────────────────────────────────────

// fakeAsyncRegistry implements Options.AsyncHookRegistry for tests.
type fakeAsyncRegistry struct {
	cancelled []string
}

func (f *fakeAsyncRegistry) Cancel(id string) bool {
	f.cancelled = append(f.cancelled, id)
	return true
}

// TestQueryWiresCancelAsyncMessageToRegistry verifies that the Controller's
// cancel_async_message subtype is wired to the AsyncHookRegistry.Cancel callback
// when Options.AsyncHookRegistry is set.
// CC ref: SDK-35 (G12).
func TestQueryWiresCancelAsyncMessageToRegistry(t *testing.T) {
	reg := &fakeAsyncRegistry{}
	ctrl := &Controller{
		cancelAsyncMessage: func(messageUUID string) (bool, error) {
			return reg.Cancel(messageUUID), nil
		},
	}

	resp := ctrl.Handle(ControlRequest{
		RequestID: "r-cancel",
		Request: map[string]any{
			"subtype":      "cancel_async_message",
			"message_uuid": "uuid-123",
		},
	})

	if resp.Response.Subtype != "success" {
		t.Fatalf("expected success, got %+v", resp)
	}
	body := resp.Response.Response
	if cancelled, _ := body["cancelled"].(bool); !cancelled {
		t.Fatalf("expected cancelled:true, got %v", body)
	}
	if len(reg.cancelled) != 1 || reg.cancelled[0] != "uuid-123" {
		t.Fatalf("expected Cancel called with uuid-123, got %v", reg.cancelled)
	}
}

// TestCancelAsyncMessageNilCallbackReturnsFalse verifies that when no callback
// is wired, cancel_async_message returns cancelled:false (safe default).
// CC ref: SDK-35.
func TestCancelAsyncMessageNilCallbackReturnsFalse(t *testing.T) {
	ctrl := &Controller{} // no cancelAsyncMessage callback
	resp := ctrl.Handle(ControlRequest{
		RequestID: "r-cancel-nil",
		Request: map[string]any{
			"subtype":      "cancel_async_message",
			"message_uuid": "uuid-456",
		},
	})
	if resp.Response.Subtype != "success" {
		t.Fatalf("expected success, got %+v", resp)
	}
	if cancelled, _ := resp.Response.Response["cancelled"].(bool); cancelled {
		t.Fatal("expected cancelled:false when no callback")
	}
}

// ── SDK-43: stop_task ────────────────────────────────────────────────────────

// fakeAgentReg implements a minimal agent registry interface for stop_task tests.
type fakeAgentReg struct {
	cancelCalled []string
}

func (f *fakeAgentReg) Cancel(id string) bool {
	f.cancelCalled = append(f.cancelCalled, id)
	return true
}

// TestQueryWiresStopTaskToAgentRegistry verifies that the Controller's
// stop_task subtype is wired to the AgentRegistry.Cancel callback.
// CC ref: SDK-43 (G12).
func TestQueryWiresStopTaskToAgentRegistry(t *testing.T) {
	agentReg := &fakeAgentReg{}
	ctrl := &Controller{
		stopTask: func(taskID string) error {
			if !agentReg.Cancel(taskID) {
				return nil // already handled
			}
			return nil
		},
	}

	resp := ctrl.Handle(ControlRequest{
		RequestID: "r-stop",
		Request: map[string]any{
			"subtype": "stop_task",
			"task_id": "task-abc",
		},
	})

	if resp.Response.Subtype != "success" {
		t.Fatalf("expected success, got %+v", resp)
	}
	if len(agentReg.cancelCalled) != 1 || agentReg.cancelCalled[0] != "task-abc" {
		t.Fatalf("expected Cancel called with task-abc, got %v", agentReg.cancelCalled)
	}
}

// TestStopTaskNilCallbackReturnsError verifies that when no stopTask callback
// is registered, stop_task returns an error (unregistered).
// CC ref: SDK-43.
func TestStopTaskNilCallbackReturnsError(t *testing.T) {
	ctrl := &Controller{} // no stopTask callback
	resp := ctrl.Handle(ControlRequest{
		RequestID: "r-stop-nil",
		Request: map[string]any{
			"subtype": "stop_task",
			"task_id": "task-xyz",
		},
	})
	if resp.Response.Subtype != "error" {
		t.Fatalf("expected error when no stopTask callback, got %+v", resp)
	}
}

// ── SDK-49: update_environment_variables ─────────────────────────────────────

// TestDispatchLineUpdateEnvCallsCallback verifies that when onEnvMutation is
// set, update_environment_variables calls it with the variables map.
// CC ref: SDK-49 (G12).
func TestDispatchLineUpdateEnvCallsCallback(t *testing.T) {
	var received map[string]string
	var callCount atomic.Int32

	var out bytes.Buffer
	enc := NewEncoder(&out)
	ctrl := NewController(nil, nil)
	asker := newControlAsker(enc.WriteRequest, func() string { return "x" })

	onEnv := func(vars map[string]string) {
		received = vars
		callCount.Add(1)
	}

	dispatchLine(
		`{"type":"update_environment_variables","variables":{"MY_VAR":"hello","OTHER":"world"}}`,
		ctrl, asker, enc, onEnv,
	)

	if callCount.Load() != 1 {
		t.Fatalf("expected 1 callback call, got %d", callCount.Load())
	}
	if received["MY_VAR"] != "hello" {
		t.Fatalf("MY_VAR = %q, want hello", received["MY_VAR"])
	}
	if received["OTHER"] != "world" {
		t.Fatalf("OTHER = %q, want world", received["OTHER"])
	}
	// No output should be written.
	if out.Len() != 0 {
		t.Fatalf("update_environment_variables must produce no response output, got: %q", out.String())
	}
}

// TestDispatchLineUpdateEnvNilCallbackStillSilent verifies that with nil
// onEnvMutation the message is silently accepted (no output, no crash).
// CC ref: SDK-49.
func TestDispatchLineUpdateEnvNilCallbackStillSilent(t *testing.T) {
	var out bytes.Buffer
	enc := NewEncoder(&out)
	ctrl := NewController(nil, nil)
	asker := newControlAsker(enc.WriteRequest, func() string { return "x" })

	dispatchLine(
		`{"type":"update_environment_variables","variables":{"X":"1"}}`,
		ctrl, asker, enc, nil, // nil callback
	)
	if out.Len() != 0 {
		t.Fatalf("must produce no output with nil callback, got: %q", out.String())
	}
}

// TestQueryAsyncRegistryFromRunnerOptions verifies that Options.AsyncHookRegistry
// is wired to controller.cancelAsyncMessage when set.
// Uses Controller directly (no full Query) to keep the test fast.
// CC ref: SDK-35 (G12).
func TestQueryAsyncRegistryFromRunnerOptions(t *testing.T) {
	cancelled := make([]string, 0)
	ctrl := &Controller{
		cancelAsyncMessage: func(id string) (bool, error) {
			cancelled = append(cancelled, id)
			return true, nil
		},
	}

	// Encode and dispatch as if from readControlLoop.
	req := map[string]any{
		"type":       "control_request",
		"request_id": "q12-1",
		"request": map[string]any{
			"subtype":      "cancel_async_message",
			"message_uuid": "async-msg-uuid",
		},
	}
	data, _ := json.Marshal(req)

	var out bytes.Buffer
	enc := NewEncoder(&out)
	asker := newControlAsker(enc.WriteRequest, func() string { return "t" })
	dispatchLine(string(data), ctrl, asker, enc, nil)

	if len(cancelled) != 1 || cancelled[0] != "async-msg-uuid" {
		t.Fatalf("expected 1 cancel with async-msg-uuid, got %v", cancelled)
	}
	// Verify success response was written.
	if !strings.Contains(out.String(), `"success"`) {
		t.Fatalf("expected success response in output, got: %q", out.String())
	}
}

// ── Integration tests: Query wires runner.AsyncHookRegistry ──────────────────

// g12SendAndInterrupt writes a JSON message followed by an interrupt to the pipe writer.
func g12SendAndInterrupt(pw *io.PipeWriter, msgJSON []byte) {
	_, _ = pw.Write(append(msgJSON, '\n'))
	interrupt, _ := json.Marshal(map[string]any{
		"type": "control_request", "request_id": "g12-int",
		"request": map[string]any{"subtype": "interrupt"},
	})
	_, _ = pw.Write(append(interrupt, '\n'))
	_ = pw.Close()
}

// g12QueryWithBlockingClient starts a Query with a blockingClient and returns:
// the input pipe writer, output buffer, and a channel for the Query error.
func g12QueryWithBlockingClient(t *testing.T, opts Options) (*io.PipeWriter, *bytes.Buffer, <-chan error) {
	t.Helper()
	inPR, inPW := io.Pipe()
	outPR, outPW := io.Pipe()

	bufOut := &bytes.Buffer{}
	outDoneC := make(chan struct{})
	go func() {
		defer close(outDoneC)
		io.Copy(bufOut, outPR) //nolint:errcheck
	}()

	opts.In = inPR
	opts.Out = outPW

	ready := make(chan struct{}, 1)
	bc := &blockingClient{ready: ready}
	innerFactory := opts.RunnerFactory
	opts.RunnerFactory = func() (*conversation.Runner, error) {
		r, err := innerFactory()
		if err != nil {
			return nil, err
		}
		r.Client = bc // use blocking client so turn waits
		return r, nil
	}

	doneC := make(chan error, 1)
	go func() {
		err := Query(context.Background(), opts)
		_ = outPW.Close()
		<-outDoneC
		doneC <- err
	}()

	// Wait for blocking client to be ready.
	select {
	case <-ready:
	case <-time.After(3 * time.Second):
		t.Fatal("blockingClient never ready in g12QueryWithBlockingClient")
	}

	return inPW, bufOut, doneC
}

// TestQueryWiresCancelAsyncFromRunnerRegistry verifies that Query extracts the
// runner's AsyncHookRegistry and wires it to cancelAsyncMessage so that a
// cancel_async_message control_request actually cancels a registered async hook.
// CC ref: SDK-35 (G12) + HOOK-12 runtime.
func TestQueryWiresCancelAsyncFromRunnerRegistry(t *testing.T) {
	// Build a real AsyncHookRegistry with one registered async entry.
	reg := hookpkg.NewAsyncHookRegistry()
	// Register a slow async hook that will never finish within test lifetime.
	asyncID := reg.Register("PreToolUse", "slow-test-hook", func() {
		time.Sleep(10 * time.Second)
	})
	if reg.Len() != 1 {
		t.Fatalf("expected 1 entry, got %d", reg.Len())
	}

	inPW, bufOut, doneC := g12QueryWithBlockingClient(t, Options{
		Prompt:            "g12 cancel async test",
		AsyncHookRegistry: reg,
		RunnerFactory: func() (*conversation.Runner, error) {
			return &conversation.Runner{
				Model:     "stub",
				MaxTokens: 256,
			}, nil
		},
	})

	// Send cancel_async_message for the registered async hook ID.
	cancelMsg, _ := json.Marshal(map[string]any{
		"type":       "control_request",
		"request_id": "g12-cancel",
		"request": map[string]any{
			"subtype":      "cancel_async_message",
			"message_uuid": asyncID,
		},
	})
	g12SendAndInterrupt(inPW, cancelMsg)

	select {
	case <-doneC:
	case <-time.After(5 * time.Second):
		t.Fatal("Query did not return in time")
	}

	// The async hook should have been cancelled.
	if reg.Len() != 0 {
		t.Fatalf("expected 0 entries after cancel, got %d; cancel did not work", reg.Len())
	}
	// Success response should be in output.
	if !strings.Contains(bufOut.String(), `"success"`) {
		t.Fatalf("expected success response for cancel_async_message in output, got: %q", bufOut.String())
	}
}

// TestQueryWiresStopTaskFromAgentRegistry verifies that Query wires the runner's
// AgentRegistry to the stop_task control subtype so that a running background
// agent can be cancelled via the SDK control protocol.
// CC ref: SDK-43 (G12).
func TestQueryWiresStopTaskFromAgentRegistry(t *testing.T) {
	agentReg := orchestration.NewAgentRegistry()
	agentReg.StartBackground("bg-task-1", func(_ context.Context) orchestration.Outcome {
		time.Sleep(10 * time.Second)
		return orchestration.Outcome{Summary: "never"}
	})
	if snap := agentReg.Snapshot(); len(snap) != 1 {
		t.Fatalf("expected 1 agent, got %+v", snap)
	}

	inPW, bufOut, doneC := g12QueryWithBlockingClient(t, Options{
		Prompt: "g12 stop_task test",
		RunnerFactory: func() (*conversation.Runner, error) {
			return &conversation.Runner{
				Model:         "stub",
				MaxTokens:     256,
				AgentRegistry: agentReg,
			}, nil
		},
	})

	// Send stop_task.
	stopMsg, _ := json.Marshal(map[string]any{
		"type":       "control_request",
		"request_id": "g12-stop",
		"request": map[string]any{
			"subtype": "stop_task",
			"task_id": "bg-task-1",
		},
	})
	g12SendAndInterrupt(inPW, stopMsg)

	select {
	case <-doneC:
	case <-time.After(5 * time.Second):
		t.Fatal("Query did not return in time")
	}

	// Agent should have been removed from registry.
	if snap := agentReg.Snapshot(); len(snap) != 0 {
		t.Fatalf("expected 0 agents after stop_task, got %+v", snap)
	}
	if !strings.Contains(bufOut.String(), `"success"`) {
		t.Fatalf("expected success response for stop_task in output, got: %q", bufOut.String())
	}
}

// TestQueryUpdateEnvCallsCallbackViaOptions verifies that update_environment_variables
// messages received during a Query invoke Options.OnEnvMutation.
// CC ref: SDK-49 (G12).
func TestQueryUpdateEnvCallsCallbackViaOptions(t *testing.T) {
	var received atomic.Value // stores map[string]string

	inPW, _, doneC := g12QueryWithBlockingClient(t, Options{
		Prompt: "g12 env test",
		OnEnvMutation: func(vars map[string]string) {
			received.Store(vars)
		},
		RunnerFactory: func() (*conversation.Runner, error) {
			return &conversation.Runner{
				Model:     "stub",
				MaxTokens: 256,
			}, nil
		},
	})

	envMsg, _ := json.Marshal(map[string]any{
		"type":      "update_environment_variables",
		"variables": map[string]string{"CCGO_TEST": "g12"},
	})
	// Give dispatch loop 30ms to process the env message before interrupting.
	_, _ = inPW.Write(append(envMsg, '\n'))
	time.Sleep(30 * time.Millisecond)

	interrupt, _ := json.Marshal(map[string]any{
		"type": "control_request", "request_id": "int",
		"request": map[string]any{"subtype": "interrupt"},
	})
	_, _ = inPW.Write(append(interrupt, '\n'))
	_ = inPW.Close()

	select {
	case <-doneC:
	case <-time.After(5 * time.Second):
		t.Fatal("Query did not return in time")
	}

	got, _ := received.Load().(map[string]string)
	if got == nil {
		t.Fatal("OnEnvMutation callback was never called")
	}
	if got["CCGO_TEST"] != "g12" {
		t.Fatalf("CCGO_TEST = %q, want g12", got["CCGO_TEST"])
	}
}
