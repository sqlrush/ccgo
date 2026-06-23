package conversation

import (
	"context"
	"testing"
	"time"

	"ccgo/internal/orchestration"
)

// TestTaskToolRunRequestedAndIsBackground verifies the predicate functions that
// route task results to synchronous vs background execution (ORCH-03/TOOL-TASK-02).
func TestTaskToolRunRequestedAndIsBackground(t *testing.T) {
	tests := []struct {
		name           string
		structured     map[string]any
		wantRun        bool
		wantBackground bool
	}{
		{
			name:           "sync run=true",
			structured:     map[string]any{"type": "task", "run": true, "run_in_background": false},
			wantRun:        true,
			wantBackground: false,
		},
		{
			name:           "background only",
			structured:     map[string]any{"type": "task", "run": false, "run_in_background": true},
			wantRun:        true,
			wantBackground: true,
		},
		{
			name:           "both run and background",
			structured:     map[string]any{"type": "task", "run": true, "run_in_background": true},
			wantRun:        true,
			wantBackground: true,
		},
		{
			name:           "neither run nor background",
			structured:     map[string]any{"type": "task", "run": false, "run_in_background": false},
			wantRun:        false,
			wantBackground: false,
		},
		{
			name:    "wrong type — taskToolRunRequested checks type, isBackgroundTask does not",
			structured: map[string]any{"type": "bash", "run": true, "run_in_background": true},
			// taskToolRunRequested must return false (wrong type).
			// isBackgroundTask is a lower-level helper; it checks only the flag itself.
			// In production it is only called when taskToolRunRequested returns true.
			wantRun:        false,
			wantBackground: true, // isBackgroundTask does not re-check type
		},
		{
			name:       "nil structured",
			structured: nil,
			wantRun:    false,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			gotRun := taskToolRunRequested(tt.structured)
			gotBg := isBackgroundTask(tt.structured)
			if gotRun != tt.wantRun {
				t.Errorf("taskToolRunRequested = %v, want %v", gotRun, tt.wantRun)
			}
			if gotBg != tt.wantBackground {
				t.Errorf("isBackgroundTask = %v, want %v", gotBg, tt.wantBackground)
			}
		})
	}
}

// TestAgentRegistryBackgroundRoundtrip verifies that the AgentRegistry used by
// the background dispatch path (ORCH-04) correctly tracks, stores, and harvests
// an outcome (matches registry_test.go but confirms wiring from task_agent.go's POV).
func TestAgentRegistryBackgroundRoundtrip(t *testing.T) {
	reg := orchestration.NewAgentRegistry()

	done := make(chan struct{})
	reg.StartBackground("task-bg-1", func(_ context.Context) orchestration.Outcome {
		defer close(done)
		return orchestration.Outcome{Summary: "agent done"}
	})

	// Registry should show running.
	snap := reg.Snapshot()
	if len(snap) != 1 || snap[0].State != orchestration.AgentRunning {
		t.Fatalf("expected 1 running agent, got %#v", snap)
	}

	// Wait for completion.
	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("background goroutine timed out")
	}

	// Harvest must succeed with the correct summary.
	out, ok := reg.Harvest("task-bg-1")
	if !ok {
		t.Fatal("Harvest returned false")
	}
	if out.Summary != "agent done" {
		t.Fatalf("summary = %q, want %q", out.Summary, "agent done")
	}
	// After harvest the agent should be gone.
	if len(reg.Snapshot()) != 0 {
		t.Fatal("expected empty registry after harvest")
	}
}
