package sdk

// G1 tests: verify that Query wires real runner subsystems into the Controller
// callbacks, so control subtypes act on live state rather than returning "not
// supported" errors.
//
// Pattern for each test:
//  1. Wire a blocking turn (blockingClient) so control_requests can be injected
//     concurrently during the turn.
//  2. Start a background goroutine draining outPR into a buffer immediately —
//     this prevents pipe writes from blocking.
//  3. Send a control_request via inPW after the turn signals ready.
//  4. Wait 200ms (read-loop processes + writes response), then send interrupt.
//  5. After Query returns, parse the accumulated buffer for control_response.
//
// CC refs: docs/cc-parity/sections/16-sdk.md G1 pass.

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"strings"
	"sync"
	"testing"
	"time"

	"ccgo/internal/contracts"
	"ccgo/internal/conversation"
	"ccgo/internal/rewind"
)

// g1SendRequest writes a control_request NDJSON line to pw.
func g1SendRequest(pw *io.PipeWriter, subtype string, extra map[string]any) {
	req := map[string]any{"subtype": subtype}
	for k, v := range extra {
		req[k] = v
	}
	msg := map[string]any{
		"type":       "control_request",
		"request_id": "g1-" + subtype,
		"request":    req,
	}
	data, _ := json.Marshal(msg)
	data = append(data, '\n')
	_, _ = pw.Write(data)
}

// g1FindControlResponse scans NDJSON output for the first control_response line.
func g1FindControlResponse(t testing.TB, out string) map[string]any {
	t.Helper()
	for _, line := range strings.Split(strings.TrimSpace(out), "\n") {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		var msg map[string]any
		if err := json.Unmarshal([]byte(line), &msg); err != nil {
			continue
		}
		if msg["type"] == "control_response" {
			return msg
		}
	}
	t.Fatal("no control_response found in output")
	return nil
}

// g1Setup creates the pipes and starts a blocking Query. It returns:
//   - inPW: write end of the control input pipe (send control_requests here)
//   - buf: accumulates all NDJSON output from Query
//   - outDone: closed when the output drain goroutine finishes
//   - ready: closed when blockingClient is blocking in CreateMessage
//   - done: carries the error returned by Query
//   - runner: the *conversation.Runner used by Query (for asserting side-effects)
//   - cancel: cancel the context
//
// runnerSetup, when non-nil, is called inside RunnerFactory (before RunTurn) to
// configure runner fields without a race with the Query goroutine.
func g1Setup(t *testing.T, runnerSetup func(*conversation.Runner), extraOpts ...func(*Options)) (
	inPW *io.PipeWriter,
	buf *bytes.Buffer,
	outDone <-chan struct{},
	ready <-chan struct{},
	done <-chan error,
	runner *conversation.Runner,
	cancel context.CancelFunc,
) {
	t.Helper()
	inPR, pw := io.Pipe()
	inPW = pw

	outPR, outPW := io.Pipe()

	readyC := make(chan struct{})
	blockClient := &blockingClient{ready: readyC}
	ready = readyC

	r := minimalRunner(blockClient)
	runner = r

	ctx, cancelFn := context.WithTimeout(context.Background(), 5*time.Second)
	cancel = cancelFn

	opts := Options{
		Prompt: "g1 test",
		In:     inPR,
		Out:    outPW,
		RunnerFactory: func() (*conversation.Runner, error) {
			if runnerSetup != nil {
				runnerSetup(r) // run inside factory — no race with test goroutine
			}
			return r, nil
		},
	}
	for _, fn := range extraOpts {
		fn(&opts)
	}

	bufOut := &bytes.Buffer{}
	buf = bufOut
	outDoneC := make(chan struct{})
	outDone = outDoneC
	go func() {
		defer close(outDoneC)
		io.Copy(bufOut, outPR) //nolint:errcheck
	}()

	doneC := make(chan error, 1)
	done = doneC
	go func() {
		err := Query(ctx, opts)
		_ = outPW.Close() // signal drain goroutine to finish
		doneC <- err
	}()

	return inPW, buf, outDone, ready, done, runner, cancel
}

// g1WaitReady waits for the blocking client to signal readiness.
func g1WaitReady(t *testing.T, ready <-chan struct{}, done <-chan error) {
	t.Helper()
	select {
	case <-ready:
	case err := <-done:
		t.Fatalf("Query returned before ready: %v", err)
	case <-time.After(3 * time.Second):
		t.Fatal("blockingClient never ready")
	}
}

// g1Finish sends an interrupt to end the turn, then waits for Query to return.
func g1Finish(t *testing.T, inPW *io.PipeWriter, outDone <-chan struct{}, done <-chan error) {
	t.Helper()
	g1SendRequest(inPW, "interrupt", nil)
	_ = inPW.Close()
	select {
	case <-done:
	case <-time.After(3 * time.Second):
		t.Fatal("Query did not return after interrupt")
	}
	<-outDone
}

// ── G1 tests ─────────────────────────────────────────────────────────────────

// TestQueryWiresSetPermissionMode_G1 verifies that set_permission_mode updates
// runner.PermissionMode during a blocking turn (SDK-30 live backend).
func TestQueryWiresSetPermissionMode_G1(t *testing.T) {
	inPW, buf, outDone, ready, done, runner, cancel := g1Setup(t,
		func(r *conversation.Runner) {
			r.PermissionMode = contracts.PermissionDefault
		},
	)
	defer cancel()

	g1WaitReady(t, ready, done)

	g1SendRequest(inPW, "set_permission_mode", map[string]any{"mode": "acceptEdits"})
	time.Sleep(200 * time.Millisecond)
	g1Finish(t, inPW, outDone, done)

	if runner.PermissionMode != contracts.PermissionAcceptEdits {
		t.Errorf("PermissionMode = %q want acceptEdits", runner.PermissionMode)
	}
	resp := g1FindControlResponse(t, buf.String())
	response, _ := resp["response"].(map[string]any)
	if response["subtype"] != "success" {
		t.Errorf("set_permission_mode response subtype = %v want success", response["subtype"])
	}
}

// TestQueryWiresGetContextUsage_G1 verifies that get_context_usage returns a
// valid structural response during a blocking turn (SDK-33 live backend).
func TestQueryWiresGetContextUsage_G1(t *testing.T) {
	inPW, buf, outDone, ready, done, _, cancel := g1Setup(t, nil)
	defer cancel()

	g1WaitReady(t, ready, done)
	g1SendRequest(inPW, "get_context_usage", nil)
	time.Sleep(200 * time.Millisecond)
	g1Finish(t, inPW, outDone, done)

	resp := g1FindControlResponse(t, buf.String())
	response, _ := resp["response"].(map[string]any)
	if response["subtype"] != "success" {
		t.Errorf("get_context_usage response subtype = %v want success", response["subtype"])
	}
	inner, _ := response["response"].(map[string]any)
	for _, key := range []string{"categories", "totalTokens", "maxTokens", "percentage"} {
		if _, ok := inner[key]; !ok {
			t.Errorf("get_context_usage response missing key %q; got %v", key, inner)
		}
	}
}

// TestQueryWiresMCPStatus_G1 verifies that mcp_status returns mcpServers list
// from the runner's MCP config (SDK-32 live backend).
func TestQueryWiresMCPStatus_G1(t *testing.T) {
	inPW, buf, outDone, ready, done, _, cancel := g1Setup(t, nil)
	defer cancel()

	g1WaitReady(t, ready, done)
	g1SendRequest(inPW, "mcp_status", nil)
	time.Sleep(200 * time.Millisecond)
	g1Finish(t, inPW, outDone, done)

	resp := g1FindControlResponse(t, buf.String())
	response, _ := resp["response"].(map[string]any)
	if response["subtype"] != "success" {
		t.Errorf("mcp_status response subtype = %v want success", response["subtype"])
	}
	inner, _ := response["response"].(map[string]any)
	if _, ok := inner["mcpServers"]; !ok {
		t.Errorf("mcp_status response missing mcpServers key; got %v", inner)
	}
}

// TestQueryWiresGetSettings_G1 verifies that get_settings returns effective +
// sources (SDK-45 live backend).
func TestQueryWiresGetSettings_G1(t *testing.T) {
	inPW, buf, outDone, ready, done, _, cancel := g1Setup(t, nil)
	defer cancel()

	g1WaitReady(t, ready, done)
	g1SendRequest(inPW, "get_settings", nil)
	time.Sleep(200 * time.Millisecond)
	g1Finish(t, inPW, outDone, done)

	resp := g1FindControlResponse(t, buf.String())
	response, _ := resp["response"].(map[string]any)
	if response["subtype"] != "success" {
		t.Errorf("get_settings response subtype = %v want success", response["subtype"])
	}
	inner, _ := response["response"].(map[string]any)
	for _, key := range []string{"effective", "sources"} {
		if _, ok := inner[key]; !ok {
			t.Errorf("get_settings response missing key %q; got %v", key, inner)
		}
	}
}

// TestQueryWiresRewindFilesNoStore_G1 verifies that rewind_files returns
// canRewind:false (not an error) when no RewindFiles callback is provided.
func TestQueryWiresRewindFilesNoStore_G1(t *testing.T) {
	inPW, buf, outDone, ready, done, _, cancel := g1Setup(t, nil)
	defer cancel()

	g1WaitReady(t, ready, done)
	g1SendRequest(inPW, "rewind_files", map[string]any{
		"user_message_id": "msg-nonexistent",
		"dry_run":         true,
	})
	time.Sleep(200 * time.Millisecond)
	g1Finish(t, inPW, outDone, done)

	resp := g1FindControlResponse(t, buf.String())
	response, _ := resp["response"].(map[string]any)
	if response["subtype"] != "success" {
		t.Errorf("rewind_files (no callback) response subtype = %v want success", response["subtype"])
	}
	inner, _ := response["response"].(map[string]any)
	if inner["canRewind"] != false {
		t.Errorf("rewind_files canRewind = %v want false (no callback)", inner["canRewind"])
	}
}

// TestQueryWiresRewindFilesCallback_G1 verifies that rewind_files invokes the
// RewindFiles callback when one is provided (SDK-34 live backend).
func TestQueryWiresRewindFilesCallback_G1(t *testing.T) {
	var mu sync.Mutex
	var capturedMsgID string
	var capturedDryRun bool

	store := rewind.Store{Dir: t.TempDir()}
	tmpDir := t.TempDir()

	inPW, buf, outDone, ready, done, _, cancel := g1Setup(t,
		// runnerSetup: set rewind fields before RunTurn starts (race-free).
		func(r *conversation.Runner) {
			r.RewindStore = &store
			r.SessionPath = tmpDir + "/session.jsonl"
			r.WorkingDirectory = tmpDir
		},
		// extraOpts: wire the RewindFiles callback on Options.
		func(o *Options) {
			o.RewindFiles = func(msgID string, dryRun bool) (*RewindFilesResult, error) {
				mu.Lock()
				capturedMsgID = msgID
				capturedDryRun = dryRun
				mu.Unlock()
				return &RewindFilesResult{CanRewind: true, FilesChanged: []string{"/fake/file.go"}}, nil
			}
		},
	)
	defer cancel()

	g1WaitReady(t, ready, done)
	g1SendRequest(inPW, "rewind_files", map[string]any{
		"user_message_id": "msg-abc",
		"dry_run":         true,
	})
	time.Sleep(200 * time.Millisecond)
	g1Finish(t, inPW, outDone, done)

	mu.Lock()
	defer mu.Unlock()

	if capturedMsgID != "msg-abc" {
		t.Errorf("rewindFiles msgID = %q want msg-abc", capturedMsgID)
	}
	if !capturedDryRun {
		t.Error("rewindFiles dryRun = false want true")
	}

	resp := g1FindControlResponse(t, buf.String())
	response, _ := resp["response"].(map[string]any)
	if response["subtype"] != "success" {
		t.Errorf("rewind_files response subtype = %v want success", response["subtype"])
	}
	inner, _ := response["response"].(map[string]any)
	if inner["canRewind"] != true {
		t.Errorf("rewind_files canRewind = %v want true", inner["canRewind"])
	}
}
