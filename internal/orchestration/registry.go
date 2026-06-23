package orchestration

import (
	"context"
	"sync"
)

// AgentState is the lifecycle state of a tracked background agent.
type AgentState string

const (
	AgentRunning AgentState = "running"
	AgentDone    AgentState = "done"
	AgentFailed  AgentState = "failed"
)

// Outcome is the result of a background-agent run.
type Outcome struct {
	Summary string
	Err     error
}

// AgentStatus is an immutable snapshot of one tracked agent.
// All fields are value types so a copy is a true independent copy.
type AgentStatus struct {
	ID    string
	State AgentState
}

type agentEntry struct {
	status   AgentStatus
	outcome  Outcome
	finished bool
}

// AgentRegistry tracks in-process background agents.
// All methods are safe for concurrent use. Snapshots return copies.
type AgentRegistry struct {
	mu     sync.Mutex
	agents map[string]*agentEntry
}

// NewAgentRegistry creates an empty AgentRegistry.
func NewAgentRegistry() *AgentRegistry {
	return &AgentRegistry{agents: make(map[string]*agentEntry)}
}

// StartBackground registers id and launches run in a new goroutine.
// The goroutine's result is stored and retrievable via Harvest.
func (r *AgentRegistry) StartBackground(id string, run func(context.Context) Outcome) {
	r.mu.Lock()
	r.agents[id] = &agentEntry{status: AgentStatus{ID: id, State: AgentRunning}}
	r.mu.Unlock()

	go func() {
		out := run(context.Background())
		r.mu.Lock()
		defer r.mu.Unlock()
		entry := r.agents[id]
		if entry == nil {
			return
		}
		entry.outcome = out
		entry.finished = true
		if out.Err != nil {
			entry.status.State = AgentFailed
		} else {
			entry.status.State = AgentDone
		}
	}()
}

// Snapshot returns a copy of every tracked agent's status.
// Mutating the returned slice or its elements has no effect on the registry.
func (r *AgentRegistry) Snapshot() []AgentStatus {
	r.mu.Lock()
	defer r.mu.Unlock()
	out := make([]AgentStatus, 0, len(r.agents))
	for _, entry := range r.agents {
		out = append(out, entry.status) // AgentStatus is a value type — copy
	}
	return out
}

// Harvest returns the Outcome of a finished agent and removes it from the
// registry. Returns (zero, false) if the agent is unknown or still running.
func (r *AgentRegistry) Harvest(id string) (Outcome, bool) {
	r.mu.Lock()
	defer r.mu.Unlock()
	entry := r.agents[id]
	if entry == nil || !entry.finished {
		return Outcome{}, false
	}
	delete(r.agents, id)
	return entry.outcome, true
}

// Cancel marks the agent with the given id as stopped (AgentFailed) and
// removes it from the registry. Returns true if the agent was found and
// cancelled (regardless of whether it was running or already finished).
// The underlying goroutine is not forcefully interrupted — it is left to
// exit naturally. This matches CC's stop_task semantics (SDK-43):
// the task is removed from the queue; in-flight work may still complete.
// CC ref: controlSchemas.ts:455-462.
func (r *AgentRegistry) Cancel(id string) bool {
	r.mu.Lock()
	defer r.mu.Unlock()
	_, ok := r.agents[id]
	if !ok {
		return false
	}
	delete(r.agents, id)
	return true
}
