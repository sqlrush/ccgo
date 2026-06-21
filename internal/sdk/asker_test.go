package sdk

import (
	"context"
	"fmt"
	"sync"
	"testing"
	"time"

	"ccgo/internal/contracts"
	"ccgo/internal/tool"
)

func TestControlAskerForwardsAndResolves(t *testing.T) {
	out := make(chan ControlRequest, 1)
	asker := newControlAsker(
		func(req ControlRequest) error { out <- req; return nil },
		func() string { return "req-1" },
	)

	decisionCh := make(chan contracts.PermissionDecision, 1)
	go func() {
		d, err := asker.Ask(context.Background(), tool.PermissionAskRequest{
			ToolUseID: "u1", ToolName: "Bash", Description: "run ls",
		})
		if err == nil {
			decisionCh <- d
		}
	}()

	// The asker must emit a can_use_tool control_request.
	select {
	case req := <-out:
		if req.Subtype() != "can_use_tool" {
			t.Fatalf("subtype = %q want can_use_tool", req.Subtype())
		}
		// Validate request_id and payload fields.
		if req.RequestID == "" {
			t.Fatal("request_id must not be empty")
		}
		toolName, _ := req.Request["tool_name"].(string)
		if toolName != "Bash" {
			t.Fatalf("tool_name = %q want Bash", toolName)
		}
		toolUseID, _ := req.Request["tool_use_id"].(string)
		if toolUseID != "u1" {
			t.Fatalf("tool_use_id = %q want u1", toolUseID)
		}
		// Simulate the SDK client allowing the tool.
		asker.Resolve(req.RequestID, contracts.PermissionDecision{Behavior: contracts.PermissionAllow})
	case <-time.After(2 * time.Second):
		t.Fatal("no can_use_tool request emitted")
	}

	select {
	case d := <-decisionCh:
		if d.Behavior != contracts.PermissionAllow {
			t.Fatalf("decision = %v want allow", d.Behavior)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("asker never resolved")
	}
}

func TestControlAskerDeny(t *testing.T) {
	out := make(chan ControlRequest, 1)
	asker := newControlAsker(
		func(req ControlRequest) error { out <- req; return nil },
		func() string { return "req-deny" },
	)

	decisionCh := make(chan contracts.PermissionDecision, 1)
	go func() {
		d, err := asker.Ask(context.Background(), tool.PermissionAskRequest{
			ToolUseID: "u2", ToolName: "Write", Description: "write file",
		})
		if err == nil {
			decisionCh <- d
		}
	}()

	select {
	case req := <-out:
		asker.Resolve(req.RequestID, contracts.PermissionDecision{
			Behavior: contracts.PermissionDeny,
			Message:  "not allowed",
		})
	case <-time.After(2 * time.Second):
		t.Fatal("no request emitted")
	}

	select {
	case d := <-decisionCh:
		if d.Behavior != contracts.PermissionDeny {
			t.Fatalf("decision = %v want deny", d.Behavior)
		}
		if d.Message != "not allowed" {
			t.Fatalf("message = %q want 'not allowed'", d.Message)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("asker never resolved")
	}
}

func TestControlAskerCtxCancelUnblocks(t *testing.T) {
	// send function blocks; ctx cancel must unblock Ask.
	sent := make(chan ControlRequest, 1)
	asker := newControlAsker(
		func(req ControlRequest) error { sent <- req; return nil },
		func() string { return "req-ctx" },
	)

	ctx, cancel := context.WithCancel(context.Background())

	errCh := make(chan error, 1)
	go func() {
		_, err := asker.Ask(ctx, tool.PermissionAskRequest{
			ToolUseID: "u3", ToolName: "Bash", Description: "run",
		})
		errCh <- err
	}()

	// Wait for the request to be sent.
	select {
	case <-sent:
	case <-time.After(2 * time.Second):
		t.Fatal("no request emitted before cancel")
	}

	cancel()

	select {
	case err := <-errCh:
		if err == nil {
			t.Fatal("expected non-nil error after ctx cancel")
		}
	case <-time.After(2 * time.Second):
		t.Fatal("Ask did not unblock after ctx cancel")
	}
}

func TestControlAskerConcurrentRequestsCorrelate(t *testing.T) {
	var mu sync.Mutex
	requestsByID := make(map[string]int) // maps request_id -> original goroutine index
	reqCh := make(chan ControlRequest, 10)
	counter := 0

	asker := newControlAsker(
		func(req ControlRequest) error {
			reqCh <- req
			return nil
		},
		func() string {
			mu.Lock()
			defer mu.Unlock()
			counter++
			return fmt.Sprintf("req-%d", counter)
		},
	)

	const n = 5
	results := make([]chan contracts.PermissionDecision, n)
	for i := 0; i < n; i++ {
		results[i] = make(chan contracts.PermissionDecision, 1)
		idx := i
		toolName := fmt.Sprintf("Tool%d", idx)
		go func() {
			d, err := asker.Ask(context.Background(), tool.PermissionAskRequest{
				ToolUseID:   contracts.ID(fmt.Sprintf("uid-%d", idx)),
				ToolName:    toolName,
				Description: "test",
			})
			if err == nil {
				results[idx] <- d
			}
		}()
	}

	// Collect all n requests and map each by its request_id to the original goroutine index.
	collected := make([]ControlRequest, 0, n)
	timeout := time.After(5 * time.Second)
	for len(collected) < n {
		select {
		case req := <-reqCh:
			collected = append(collected, req)
			// Extract original index from uid-<idx>.
			toolUseID, _ := req.Request["tool_use_id"].(string)
			var idx int
			fmt.Sscanf(toolUseID, "uid-%d", &idx)
			mu.Lock()
			requestsByID[req.RequestID] = idx
			mu.Unlock()
		case <-timeout:
			t.Fatalf("only got %d/%d requests", len(collected), n)
		}
	}

	// Resolve each request with a decision whose identity is the original goroutine index.
	for _, req := range collected {
		mu.Lock()
		idx := requestsByID[req.RequestID]
		mu.Unlock()
		behavior := contracts.PermissionAllow
		if idx%2 == 1 {
			behavior = contracts.PermissionDeny
		}
		asker.Resolve(req.RequestID, contracts.PermissionDecision{
			Behavior: behavior,
			Message:  fmt.Sprintf("decision-%d", idx),
		})
	}

	// Verify all goroutines received exactly one response with the correct identity.
	timeout2 := time.After(5 * time.Second)
	for i := 0; i < n; i++ {
		select {
		case d := <-results[i]:
			// Assert the decision matches what was resolved for this goroutine's request.
			expectedBehavior := contracts.PermissionAllow
			if i%2 == 1 {
				expectedBehavior = contracts.PermissionDeny
			}
			if d.Behavior != expectedBehavior {
				t.Errorf("goroutine %d: behavior = %v want %v", i, d.Behavior, expectedBehavior)
			}
			expectedMsg := fmt.Sprintf("decision-%d", i)
			if d.Message != expectedMsg {
				t.Errorf("goroutine %d: message = %q want %q (indicates crossed wires)", i, d.Message, expectedMsg)
			}
		case <-timeout2:
			t.Fatalf("goroutine %d never received decision", i)
		}
	}
}
