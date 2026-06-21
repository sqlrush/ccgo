package orchestration

import (
	"context"
	"fmt"
	"sync"
	"testing"
	"time"
)

func TestAgentRegistryBackgroundLifecycle(t *testing.T) {
	reg := NewAgentRegistry()
	done := make(chan struct{})
	reg.StartBackground("a1", func(ctx context.Context) Outcome {
		<-done
		return Outcome{Summary: "finished"}
	})

	snap := reg.Snapshot()
	if len(snap) != 1 || snap[0].ID != "a1" || snap[0].State != AgentRunning {
		t.Fatalf("snapshot = %+v", snap)
	}
	// Mutating the snapshot must not affect the registry (immutability).
	snap[0].State = AgentDone

	close(done)
	deadline := time.After(2 * time.Second)
	for {
		out, ok := reg.Harvest("a1")
		if ok {
			if out.Summary != "finished" {
				t.Fatalf("harvested outcome = %+v", out)
			}
			return
		}
		select {
		case <-deadline:
			t.Fatal("background agent never completed")
		case <-time.After(10 * time.Millisecond):
		}
	}
}

func TestAgentRegistrySnapshotImmutability(t *testing.T) {
	reg := NewAgentRegistry()
	done := make(chan struct{})
	reg.StartBackground("b1", func(ctx context.Context) Outcome {
		<-done
		return Outcome{Summary: "b1done"}
	})

	// Capture snapshot while agent is running.
	snap1 := reg.Snapshot()
	if len(snap1) != 1 || snap1[0].State != AgentRunning {
		t.Fatalf("expected running agent, got %+v", snap1)
	}
	// Mutate the copy.
	snap1[0].State = AgentDone

	// Registry must still report running.
	snap2 := reg.Snapshot()
	if len(snap2) != 1 || snap2[0].State != AgentRunning {
		t.Fatalf("mutation of snapshot affected registry: %+v", snap2)
	}

	close(done)
}

func TestAgentRegistryConcurrent(t *testing.T) {
	reg := NewAgentRegistry()
	const n = 20
	var wg sync.WaitGroup

	// Fan-out: start N background agents concurrently.
	for i := 0; i < n; i++ {
		id := fmt.Sprintf("c%d", i)
		wg.Add(1)
		go func(id string) {
			defer wg.Done()
			reg.StartBackground(id, func(ctx context.Context) Outcome {
				return Outcome{Summary: id}
			})
		}(id)
	}

	// Concurrently call Snapshot while agents start and finish.
	for i := 0; i < 10; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			_ = reg.Snapshot()
		}()
	}

	wg.Wait()

	// Harvest all agents (may need a short spin to let goroutines finish).
	deadline := time.After(3 * time.Second)
	harvested := 0
	seen := make(map[string]bool)
	for harvested < n {
		snap := reg.Snapshot()
		for _, s := range snap {
			if s.State == AgentDone && !seen[s.ID] {
				out, ok := reg.Harvest(s.ID)
				if ok {
					seen[s.ID] = true
					harvested++
					_ = out
				}
			}
		}
		select {
		case <-deadline:
			t.Fatalf("only harvested %d/%d agents", harvested, n)
		default:
			time.Sleep(10 * time.Millisecond)
		}
	}
}
