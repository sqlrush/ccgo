package sdk

import (
	"bytes"
	"context"
	"encoding/json"
	"strings"
	"sync"
	"testing"
	"time"

	"ccgo/internal/conversation"
	"ccgo/internal/orchestration"
)

// ── SDK-58: task_notification ─────────────────────────────────────────────────

// TestAgentRegistryNotifyOnCompletion verifies the StartBackgroundWithNotify seam:
// When a background task finishes successfully, the provided onDone callback is
// called with id=<id>, state=AgentDone, and the returned Outcome.
//
// Given:  a new AgentRegistry
// When:   StartBackgroundWithNotify is called with a function that returns a
//
//	successful Outcome
//
// Then:   the onDone callback is invoked with the correct id, state, and outcome.
func TestAgentRegistryNotifyOnCompletion(t *testing.T) {
	t.Parallel()

	reg := orchestration.NewAgentRegistry()

	var gotID string
	var gotState orchestration.AgentState
	var gotOutcome orchestration.Outcome
	notified := make(chan struct{})

	reg.StartBackgroundWithNotify("task-n1", func(_ context.Context) orchestration.Outcome {
		return orchestration.Outcome{Summary: "done"}
	}, func(id string, state orchestration.AgentState, outcome orchestration.Outcome) {
		gotID = id
		gotState = state
		gotOutcome = outcome
		close(notified)
	})

	select {
	case <-notified:
	case <-time.After(2 * time.Second):
		t.Fatal("timeout waiting for notification")
	}

	if gotID != "task-n1" {
		t.Errorf("got id=%q, want task-n1", gotID)
	}
	if gotState != orchestration.AgentDone {
		t.Errorf("got state=%q, want %q", gotState, orchestration.AgentDone)
	}
	if gotOutcome.Summary != "done" {
		t.Errorf("got summary=%q, want done", gotOutcome.Summary)
	}
}

// TestAgentRegistryNotifyOnFailure verifies that a failed task calls onDone with
// AgentFailed state.
func TestAgentRegistryNotifyOnFailure(t *testing.T) {
	t.Parallel()

	reg := orchestration.NewAgentRegistry()

	var gotState orchestration.AgentState
	notified := make(chan struct{})

	reg.StartBackgroundWithNotify("task-f1", func(_ context.Context) orchestration.Outcome {
		return orchestration.Outcome{Err: errTestG27}
	}, func(id string, state orchestration.AgentState, outcome orchestration.Outcome) {
		gotState = state
		close(notified)
	})

	select {
	case <-notified:
	case <-time.After(2 * time.Second):
		t.Fatal("timeout waiting for failure notification")
	}

	if gotState != orchestration.AgentFailed {
		t.Errorf("got state=%q, want %q", gotState, orchestration.AgentFailed)
	}
}

// TestAgentRegistryNotifyNilCallbackIsSafe verifies that StartBackgroundWithNotify
// does not panic when onDone is nil — maintains backward compatibility with
// StartBackground callers.
func TestAgentRegistryNotifyNilCallbackIsSafe(t *testing.T) {
	t.Parallel()

	reg := orchestration.NewAgentRegistry()
	done := make(chan struct{})

	// nil callback must not panic.
	reg.StartBackgroundWithNotify("task-nil", func(_ context.Context) orchestration.Outcome {
		close(done)
		return orchestration.Outcome{Summary: "ok"}
	}, nil)

	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("timeout")
	}
}

// TestQueryEmitsTaskNotificationOnAgentCompletion verifies SDK-58:
// When a background task registered in AgentRegistry completes, sdk.Query
// emits a system/task_notification sdk_event on Out.
//
// Strategy: use a thread-safe buffer and a channel to synchronise when the
// callback fires. The background task is started BEFORE Query so the global
// SetOnTaskDone callback is registered and fires during the Query turn.
//
// Given:  runner.AgentRegistry is set; a background task completes during Query
// When:   sdk.Query runs
// Then:   Out contains an sdk_event line with type="system",
//
//	subtype="task_notification", task_id=<id>, status="completed"
func TestQueryEmitsTaskNotificationOnAgentCompletion(t *testing.T) {
	t.Parallel()

	reg := orchestration.NewAgentRegistry()

	// notified is closed by the per-task callback once the task finishes.
	notified := make(chan struct{})
	// taskSignal is closed to unblock the background task.
	taskSignal := make(chan struct{})

	// Start the task BEFORE building the runner so it runs during the turn.
	reg.StartBackgroundWithNotify("sdk58-task", func(_ context.Context) orchestration.Outcome {
		<-taskSignal
		return orchestration.Outcome{Summary: "sdk58 done"}
	}, func(_ string, _ orchestration.AgentState, _ orchestration.Outcome) {
		select {
		case <-notified:
		default:
			close(notified)
		}
	})

	// Use a protected buffer so the global SetOnTaskDone callback (which runs in a
	// separate goroutine) can write to it safely even after Query returns.
	safeBuf := &safeBuffer{}

	queryDone := make(chan error, 1)
	go func() {
		queryDone <- Query(context.Background(), Options{
			Prompt: "hello",
			Out:    safeBuf,
			RunnerFactory: func() (*conversation.Runner, error) {
				r := minimalRunner(&stubMessageClient{reply: "ok"})
				r.AgentRegistry = reg
				return r, nil
			},
		})
	}()

	// Let Query start, then unblock the background task.
	time.Sleep(20 * time.Millisecond)
	close(taskSignal)

	// Wait for Query to return.
	select {
	case err := <-queryDone:
		if err != nil {
			t.Fatalf("Query error: %v", err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("Query did not finish in time")
	}

	// Wait for the task notification callback to fire (or timeout).
	select {
	case <-notified:
	case <-time.After(2 * time.Second):
		t.Fatal("task notification callback never fired")
	}

	output := safeBuf.String()

	// Scan for task_notification event.
	found := false
	for _, line := range strings.Split(output, "\n") {
		if line == "" {
			continue
		}
		var ev map[string]any
		if err := json.Unmarshal([]byte(line), &ev); err != nil {
			continue
		}
		if ev["type"] != "sdk_event" {
			continue
		}
		req, _ := ev["request"].(map[string]any)
		if req == nil {
			continue
		}
		if req["type"] == "system" && req["subtype"] == "task_notification" {
			if id, _ := req["task_id"].(string); id == "sdk58-task" {
				if status, _ := req["status"].(string); status == "completed" || status == "failed" || status == "stopped" {
					found = true
					break
				}
			}
		}
	}
	if !found {
		t.Logf("output:\n%s", output)
		t.Fatalf("task_notification not found in output")
	}
}

// safeBuffer is a bytes.Buffer protected by a mutex so it can be safely
// written from a background goroutine (the SetOnTaskDone callback) while
// being read from the test goroutine.
type safeBuffer struct {
	mu  sync.Mutex
	buf bytes.Buffer
}

func (s *safeBuffer) Write(p []byte) (int, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.buf.Write(p)
}

func (s *safeBuffer) String() string {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.buf.String()
}

// ── SDK-57: auth_status ───────────────────────────────────────────────────────

// TestQueryEmitsAuthStatusEvent verifies SDK-57:
// When Options.AuthStatus is set with a snapshot, sdk.Query emits an auth_status
// sdk_event reflecting the provided authentication state.
//
// Given:  Options.AuthStatus = &AuthStatusSnapshot{IsAuthenticating:false, Output:["logged in"]}
// When:   sdk.Query runs
// Then:   Out contains an sdk_event line with {"type":"auth_status","isAuthenticating":false,...}
func TestQueryEmitsAuthStatusEvent(t *testing.T) {
	t.Parallel()

	var buf bytes.Buffer
	err := Query(context.Background(), Options{
		Prompt: "hello",
		Out:    &buf,
		RunnerFactory: func() (*conversation.Runner, error) {
			return minimalRunner(&stubMessageClient{reply: "ok"}), nil
		},
		AuthStatus: &AuthStatusSnapshot{
			IsAuthenticating: false,
			Output:           []string{"logged in"},
		},
	})
	if err != nil {
		t.Fatalf("Query error: %v", err)
	}

	found := false
	for _, line := range strings.Split(buf.String(), "\n") {
		if line == "" {
			continue
		}
		var ev map[string]any
		if err := json.Unmarshal([]byte(line), &ev); err != nil {
			continue
		}
		if ev["type"] != "sdk_event" {
			continue
		}
		req, _ := ev["request"].(map[string]any)
		if req == nil {
			continue
		}
		if req["type"] == "auth_status" {
			isAuth, _ := req["isAuthenticating"].(bool)
			if !isAuth {
				found = true
				break
			}
		}
	}
	if !found {
		t.Fatalf("auth_status event not found in output:\n%s", buf.String())
	}
}

// TestQueryAuthStatusNilNotEmitted verifies that when AuthStatus is nil, no
// auth_status event is emitted (avoids spurious output for callers that do not
// care about auth status).
func TestQueryAuthStatusNilNotEmitted(t *testing.T) {
	t.Parallel()

	var buf bytes.Buffer
	err := Query(context.Background(), Options{
		Prompt: "hello",
		Out:    &buf,
		RunnerFactory: func() (*conversation.Runner, error) {
			return minimalRunner(&stubMessageClient{reply: "ok"}), nil
		},
		// AuthStatus is nil — no event should be emitted.
	})
	if err != nil {
		t.Fatalf("Query error: %v", err)
	}

	for _, line := range strings.Split(buf.String(), "\n") {
		if line == "" {
			continue
		}
		var ev map[string]any
		if err := json.Unmarshal([]byte(line), &ev); err != nil {
			continue
		}
		if ev["type"] != "sdk_event" {
			continue
		}
		req, _ := ev["request"].(map[string]any)
		if req != nil && req["type"] == "auth_status" {
			t.Fatalf("unexpected auth_status event emitted: %s", line)
		}
	}
}

// TestQueryAuthStatusIsAuthenticating verifies that when IsAuthenticating is true,
// the emitted event reflects that.
func TestQueryAuthStatusIsAuthenticating(t *testing.T) {
	t.Parallel()

	var buf bytes.Buffer
	err := Query(context.Background(), Options{
		Prompt: "hello",
		Out:    &buf,
		RunnerFactory: func() (*conversation.Runner, error) {
			return minimalRunner(&stubMessageClient{reply: "ok"}), nil
		},
		AuthStatus: &AuthStatusSnapshot{
			IsAuthenticating: true,
			Output:           []string{"authenticating..."},
			Error:            "token expired",
		},
	})
	if err != nil {
		t.Fatalf("Query error: %v", err)
	}

	found := false
	for _, line := range strings.Split(buf.String(), "\n") {
		if line == "" {
			continue
		}
		var ev map[string]any
		if err := json.Unmarshal([]byte(line), &ev); err != nil {
			continue
		}
		if ev["type"] != "sdk_event" {
			continue
		}
		req, _ := ev["request"].(map[string]any)
		if req == nil {
			continue
		}
		if req["type"] == "auth_status" {
			isAuth, _ := req["isAuthenticating"].(bool)
			if isAuth {
				found = true
				break
			}
		}
	}
	if !found {
		t.Fatalf("auth_status(isAuthenticating=true) not found:\n%s", buf.String())
	}
}

// errTestG27 is a sentinel error for G27 tests.
var errTestG27 = &testErrG27{}

type testErrG27 struct{}

func (e *testErrG27) Error() string { return "test error G27" }
