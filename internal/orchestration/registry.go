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
	mu      sync.Mutex
	agents  map[string]*agentEntry
	onDone  func(string, AgentState, Outcome) // global notify; protected by mu
}

// NewAgentRegistry creates an empty AgentRegistry.
func NewAgentRegistry() *AgentRegistry {
	return &AgentRegistry{agents: make(map[string]*agentEntry)}
}

// SetOnTaskDone registers a global callback that is called every time any
// background task reaches a terminal state (AgentDone or AgentFailed).
// The callback replaces any previously registered one; pass nil to clear.
// This is the production seam used by sdk.Query to emit system/task_notification
// events (SDK-58). The callback is invoked outside the registry lock.
// CC ref: coreSchemas.ts:1694-1713 (SDKTaskNotificationMessageSchema).
func (r *AgentRegistry) SetOnTaskDone(fn func(string, AgentState, Outcome)) {
	r.mu.Lock()
	r.onDone = fn
	r.mu.Unlock()
}

// StartBackground registers id and launches run in a new goroutine.
// The goroutine's result is stored and retrievable via Harvest.
// Equivalent to StartBackgroundWithNotify(id, run, nil).
func (r *AgentRegistry) StartBackground(id string, run func(context.Context) Outcome) {
	r.StartBackgroundWithNotify(id, run, nil)
}

// StartBackgroundWithNotify registers id and launches run in a new goroutine.
// When the goroutine finishes, onDone (if non-nil) is called with the agent id,
// terminal state (AgentDone or AgentFailed), and the returned Outcome.
// The callback is invoked from the goroutine after the registry is updated;
// callers must not hold any lock that would deadlock with the callback.
//
// CC ref: coreSchemas.ts:1694-1713 (SDKTaskNotificationMessageSchema) — the SDK
// task_notification event is emitted by sdk.Query subscribing to this callback.
func (r *AgentRegistry) StartBackgroundWithNotify(id string, run func(context.Context) Outcome, onDone func(string, AgentState, Outcome)) {
	r.mu.Lock()
	r.agents[id] = &agentEntry{status: AgentStatus{ID: id, State: AgentRunning}}
	r.mu.Unlock()

	go func() {
		out := run(context.Background())

		r.mu.Lock()
		entry := r.agents[id]
		if entry == nil {
			r.mu.Unlock()
			return
		}
		entry.outcome = out
		entry.finished = true
		state := AgentDone
		if out.Err != nil {
			state = AgentFailed
		}
		entry.status.State = state
		global := r.onDone
		r.mu.Unlock()

		// Call global callback first (e.g. SDK event emission), then per-task.
		// This ordering ensures that external observers (like SDK callers that set
		// SetOnTaskDone) complete their work before per-task callbacks unblock callers.
		if global != nil {
			global(id, state, out)
		}
		if onDone != nil {
			onDone(id, state, out)
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
