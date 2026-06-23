package repl

import (
	"context"
	"strings"
	"testing"

	"ccgo/internal/orchestration"
)

// TestTasksHandlerEmpty verifies /tasks with an empty registry returns a "no tasks" message.
func TestTasksHandlerEmpty(t *testing.T) {
	reg := orchestration.NewAgentRegistry()
	h := tasksHandlerWithRegistry(reg)
	out, err := h(context.Background(), CommandContext{})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !out.Handled {
		t.Fatal("expected Handled=true")
	}
	if !strings.Contains(out.Status, "no background tasks") {
		t.Fatalf("expected 'no background tasks' in status, got: %q", out.Status)
	}
}

// TestTasksHandlerRunning verifies /tasks with a running agent shows its id+status.
func TestTasksHandlerRunning(t *testing.T) {
	reg := orchestration.NewAgentRegistry()
	block := make(chan struct{})
	reg.StartBackground("task-abc", func(ctx context.Context) orchestration.Outcome {
		<-block
		return orchestration.Outcome{Summary: "done"}
	})
	defer close(block)

	h := tasksHandlerWithRegistry(reg)
	out, err := h(context.Background(), CommandContext{})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !out.Handled {
		t.Fatal("expected Handled=true")
	}
	if !strings.Contains(out.Status, "task-abc") {
		t.Fatalf("expected task id in output, got: %q", out.Status)
	}
	if !strings.Contains(out.Status, "running") {
		t.Fatalf("expected 'running' state in output, got: %q", out.Status)
	}
}

// TestTasksHandlerCompleted verifies /tasks shows a completed (done) agent.
func TestTasksHandlerCompleted(t *testing.T) {
	reg := orchestration.NewAgentRegistry()
	done := make(chan struct{})
	reg.StartBackground("task-xyz", func(ctx context.Context) orchestration.Outcome {
		return orchestration.Outcome{Summary: "all done"}
	})
	close(done)

	// Wait until the agent is done.
	deadline := make(chan struct{})
	go func() {
		for {
			snap := reg.Snapshot()
			for _, s := range snap {
				if s.ID == "task-xyz" && s.State == orchestration.AgentDone {
					close(deadline)
					return
				}
			}
		}
	}()
	select {
	case <-deadline:
	case <-context.Background().Done():
	}

	h := tasksHandlerWithRegistry(reg)
	out, err := h(context.Background(), CommandContext{})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !out.Handled {
		t.Fatal("expected Handled=true")
	}
	if !strings.Contains(out.Status, "task-xyz") {
		t.Fatalf("expected task id in output, got: %q", out.Status)
	}
}

// TestTasksHandlerNilRegistry verifies /tasks with nil registry returns a graceful message.
func TestTasksHandlerNilRegistry(t *testing.T) {
	h := tasksHandlerWithRegistry(nil)
	out, err := h(context.Background(), CommandContext{})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !out.Handled {
		t.Fatal("expected Handled=true")
	}
	// Should explain that background tasks are not available
	if !strings.Contains(out.Status, "no background tasks") && !strings.Contains(out.Status, "not available") {
		t.Fatalf("expected graceful message, got: %q", out.Status)
	}
}
