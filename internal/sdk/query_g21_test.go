package sdk

// G21 tests: verify new SDK features wired into Query.
//
// Features tested:
//   - SDK-36: seed_read_state wires seedReadState callback → runner.ReadState
//   - SDK-37: hook_callback wires hookCallback callback → success (not "not supported")
//   - SDK-46/61: elicitation returns {action:decline} + emits elicitation_complete sdk_event
//   - SDK-54: rate_limit_event emitted when API client returns 429
//   - SDK-60: post_turn_summary sdk_event emitted when Options.PostTurnSummary=true
//   - SDK-64: files_persisted sdk_event via Options.FilesToUpload
//   - SDK-66: EventLocalCommandOutput constant exists and sdk_event wired

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"
	"time"

	"ccgo/internal/api/anthropic"
	"ccgo/internal/conversation"
)

// ── SDK-54: rate_limit_event ──────────────────────────────────────────────────

// rateLimitClient returns a 429 APIError on every CreateMessage call.
type rateLimitClient struct{}

func (r *rateLimitClient) CreateMessage(_ context.Context, _ anthropic.Request) (*anthropic.Response, error) {
	return nil, anthropic.APIError{StatusCode: http.StatusTooManyRequests, Type: "rate_limit_error", Message: "rate limit hit"}
}

// TestQueryEmitsRateLimitEvent_G21 verifies that when the API client returns a
// 429 rate-limit error, a rate_limit_event sdk_event is written to Out before
// the error is propagated.
// CC ref: coreSchemas.ts:1358-1367 (SDKRateLimitEventSchema) (SDK-54).
func TestQueryEmitsRateLimitEvent_G21(t *testing.T) {
	var out bytes.Buffer
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	err := Query(ctx, Options{
		Prompt: "trigger rate limit",
		In:     strings.NewReader(""),
		Out:    &out,
		RunnerFactory: func() (*conversation.Runner, error) {
			return minimalRunner(&rateLimitClient{}), nil
		},
	})
	// The turn should fail (rate limit error propagated).
	if err == nil {
		t.Fatal("Query should return an error on 429")
	}

	// Verify rate_limit_event sdk_event was emitted.
	foundRateLimit := false
	for _, line := range strings.Split(strings.TrimSpace(out.String()), "\n") {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		var msg map[string]any
		if jsonErr := json.Unmarshal([]byte(line), &msg); jsonErr != nil {
			continue
		}
		if msg["type"] != "sdk_event" {
			continue
		}
		req, _ := msg["request"].(map[string]any)
		if req == nil {
			continue
		}
		if req["type"] == "rate_limit_event" {
			foundRateLimit = true
			break
		}
	}
	if !foundRateLimit {
		t.Errorf("rate_limit_event sdk_event not found in output:\n%s", out.String())
	}
}

// ── SDK-36: seed_read_state ───────────────────────────────────────────────────

func TestQueryWiresSeedReadState_G21(t *testing.T) {
	inPW, buf, outDone, ready, done, runner, cancel := g1Setup(t, nil)
	defer cancel()

	g1WaitReady(t, ready, done)

	// seed_read_state with a path and mtime.
	g1SendRequest(inPW, "seed_read_state", map[string]any{
		"path":  "/tmp/foo.go",
		"mtime": float64(1234567890),
	})
	time.Sleep(200 * time.Millisecond)
	g1Finish(t, inPW, outDone, done)

	// Verify runner.ReadState has the entry.
	if runner.ReadState == nil {
		t.Fatal("runner.ReadState should be non-nil after seed_read_state")
	}
	state, ok := runner.ReadState.Get("/tmp/foo.go")
	if !ok {
		t.Fatal("runner.ReadState should have entry for /tmp/foo.go")
	}
	if state.Timestamp != 1234567890 {
		t.Errorf("ReadState timestamp = %d want 1234567890", state.Timestamp)
	}

	// Verify control_response was success.
	resp := g1FindControlResponse(t, buf.String())
	response, _ := resp["response"].(map[string]any)
	if response["subtype"] != "success" {
		t.Errorf("seed_read_state response subtype = %v want success", response["subtype"])
	}
}

// ── SDK-37: hook_callback ────────────────────────────────────────────────────

func TestQueryWiresHookCallback_G21(t *testing.T) {
	inPW, buf, outDone, ready, done, _, cancel := g1Setup(t, nil)
	defer cancel()

	g1WaitReady(t, ready, done)

	// hook_callback should succeed (not return "not supported" error).
	g1SendRequest(inPW, "hook_callback", map[string]any{
		"callback_id": "cb-42",
		"input":       map[string]any{"result": "ok"},
	})
	time.Sleep(200 * time.Millisecond)
	g1Finish(t, inPW, outDone, done)

	resp := g1FindControlResponse(t, buf.String())
	response, _ := resp["response"].(map[string]any)
	if response["subtype"] != "success" {
		t.Errorf("hook_callback response subtype = %v want success (got: %v)", response["subtype"], response)
	}
}

// ── SDK-46/61: elicitation + elicitation_complete ────────────────────────────

func TestQueryWiresElicitation_G21(t *testing.T) {
	inPW, buf, outDone, ready, done, _, cancel := g1Setup(t, nil)
	defer cancel()

	g1WaitReady(t, ready, done)

	g1SendRequest(inPW, "elicitation", map[string]any{
		"mcp_server_name": "my-server",
		"message":         "Please enter your name",
		"elicitation_id":  "elic-99",
	})
	time.Sleep(200 * time.Millisecond)
	g1Finish(t, inPW, outDone, done)

	out := buf.String()

	// Verify control_response returns action:decline.
	resp := g1FindControlResponse(t, out)
	response, _ := resp["response"].(map[string]any)
	if response["subtype"] != "success" {
		t.Errorf("elicitation response subtype = %v want success", response["subtype"])
	}
	inner, _ := response["response"].(map[string]any)
	if inner["action"] != "decline" {
		t.Errorf("elicitation action = %v want decline", inner["action"])
	}

	// Verify elicitation_complete sdk_event was emitted.
	foundComplete := false
	for _, line := range strings.Split(strings.TrimSpace(out), "\n") {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		var msg map[string]any
		if err := json.Unmarshal([]byte(line), &msg); err != nil {
			continue
		}
		if msg["type"] != "sdk_event" {
			continue
		}
		req, _ := msg["request"].(map[string]any)
		if req == nil {
			continue
		}
		if req["type"] == "system" && req["subtype"] == "elicitation_complete" {
			foundComplete = true
			break
		}
	}
	if !foundComplete {
		t.Errorf("elicitation_complete sdk_event not found in output:\n%s", out)
	}
}

// ── SDK-60: post_turn_summary ────────────────────────────────────────────────

func TestQueryEmitsPostTurnSummary_G21(t *testing.T) {
	var out bytes.Buffer
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	err := Query(ctx, Options{
		Prompt:          "hello",
		In:              strings.NewReader(""),
		Out:             &out,
		PostTurnSummary: true,
		RunnerFactory: func() (*conversation.Runner, error) {
			return minimalRunner(&stubMessageClient{reply: "hi"}), nil
		},
	})
	if err != nil {
		t.Fatalf("Query returned error: %v", err)
	}

	// Verify post_turn_summary sdk_event was emitted.
	foundSummary := false
	for _, line := range strings.Split(strings.TrimSpace(out.String()), "\n") {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		var msg map[string]any
		if err := json.Unmarshal([]byte(line), &msg); err != nil {
			continue
		}
		if msg["type"] != "sdk_event" {
			continue
		}
		req, _ := msg["request"].(map[string]any)
		if req == nil {
			continue
		}
		if req["type"] == "system" && req["subtype"] == "post_turn_summary" {
			foundSummary = true
			break
		}
	}
	if !foundSummary {
		t.Errorf("post_turn_summary sdk_event not found in output:\n%s", out.String())
	}
}

// ── SDK-64: files_persisted ──────────────────────────────────────────────────

// TestQueryEmitsFilesPersisted_G21 verifies that when Options.FilesToUpload is
// non-empty, a files_persisted sdk_event is emitted on Out.
func TestQueryEmitsFilesPersisted_G21(t *testing.T) {
	// Start a fake Files API server that accepts uploads.
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		_ = json.NewEncoder(w).Encode(map[string]any{
			"file_id":  "file-abc123",
			"filename": "test.txt",
		})
	}))
	defer srv.Close()

	// Create a temp file to upload.
	tmpFile := t.TempDir() + "/test.txt"
	if err := os.WriteFile(tmpFile, []byte("hello world"), 0o600); err != nil {
		t.Fatalf("create temp file: %v", err)
	}

	var out bytes.Buffer
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	err := Query(ctx, Options{
		Prompt:          "hello",
		In:              strings.NewReader(""),
		Out:             &out,
		FilesToUpload:   []string{tmpFile},
		FilesAPIBaseURL: srv.URL,
		FilesAPIKey:     "test-key",
		RunnerFactory: func() (*conversation.Runner, error) {
			return minimalRunner(&stubMessageClient{reply: "hi"}), nil
		},
	})
	if err != nil {
		t.Fatalf("Query returned error: %v", err)
	}

	// Verify files_persisted sdk_event was emitted.
	foundPersisted := false
	for _, line := range strings.Split(strings.TrimSpace(out.String()), "\n") {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		var msg map[string]any
		if err := json.Unmarshal([]byte(line), &msg); err != nil {
			continue
		}
		if msg["type"] != "sdk_event" {
			continue
		}
		req, _ := msg["request"].(map[string]any)
		if req == nil {
			continue
		}
		if req["type"] == "system" && req["subtype"] == "files_persisted" {
			foundPersisted = true
			break
		}
	}
	if !foundPersisted {
		t.Errorf("files_persisted sdk_event not found in output:\n%s", out.String())
	}
}

// ── SDK-66: local_command_output ─────────────────────────────────────────────

// TestLocalCommandOutputEvent_G21 verifies that EventLocalCommandOutput is wired
// so that when a runner emits this event, a local_command_output sdk_event is
// written to Out.
func TestLocalCommandOutputEvent_G21(t *testing.T) {
	// Verify the constant exists (compile-time check).
	const wantType = conversation.EventLocalCommandOutput
	_ = wantType

	pr, pw := io.Pipe()
	defer pw.Close() //nolint:errcheck
	outPR, outPW := io.Pipe()

	var out bytes.Buffer
	outDoneC := make(chan struct{})
	go func() {
		defer close(outDoneC)
		io.Copy(&out, outPR) //nolint:errcheck
	}()

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	readyC := make(chan struct{})
	blockClient := &blockingClient{ready: readyC}

	// localEventEmitted is set to true once the runner fires an EventLocalCommandOutput.
	localEventCh := make(chan struct{}, 1)

	doneC := make(chan error, 1)
	go func() {
		err := Query(ctx, Options{
			Prompt: "local command test",
			In:     pr,
			Out:    outPW,
			RunnerFactory: func() (*conversation.Runner, error) {
				r := minimalRunner(blockClient)
				// Wrap the OnEvent to fire EventLocalCommandOutput once blockingClient signals.
				// We do this by spawning a goroutine that waits for ready, then emits the event.
				go func() {
					select {
					case <-readyC:
					case <-ctx.Done():
						return
					}
					if r.OnEvent != nil {
						r.OnEvent(conversation.Event{
							Type: conversation.EventLocalCommandOutput,
							LocalCommandOutput: &conversation.LocalCommandOutput{
								Content: "Total cost: $0.01",
							},
						})
					}
					close(localEventCh)
				}()
				return r, nil
			},
		})
		_ = outPW.Close()
		doneC <- err
	}()

	// Wait for the local event to be emitted.
	select {
	case <-localEventCh:
	case <-time.After(3 * time.Second):
		t.Fatal("localEventCh never closed")
	}

	// Send interrupt to end the turn.
	interruptLine := `{"type":"control_request","request_id":"lco-1","request":{"subtype":"interrupt"}}` + "\n"
	_, _ = pw.Write([]byte(interruptLine))
	_ = pw.Close()

	select {
	case <-doneC:
	case <-time.After(5 * time.Second):
		t.Fatal("Query did not return")
	}
	<-outDoneC

	// Verify local_command_output sdk_event in output.
	foundLocal := false
	for _, line := range strings.Split(strings.TrimSpace(out.String()), "\n") {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		var msg map[string]any
		if err := json.Unmarshal([]byte(line), &msg); err != nil {
			continue
		}
		if msg["type"] != "sdk_event" {
			continue
		}
		req, _ := msg["request"].(map[string]any)
		if req == nil {
			continue
		}
		if req["type"] == string(conversation.EventLocalCommandOutput) {
			foundLocal = true
			break
		}
	}
	if !foundLocal {
		t.Errorf("local_command_output sdk_event not found in output:\n%s", out.String())
	}
}
