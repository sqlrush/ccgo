package repl

import "ccgo/internal/orchestration"

// agentRegistrySnapshotter is the read-only interface the REPL uses to inspect
// background agents. *orchestration.AgentRegistry satisfies it. Tests may use
// a simpler stub.
type agentRegistrySnapshotter interface {
	Snapshot() []orchestration.AgentStatus
}

// agentRegistryHarvester extends agentRegistrySnapshotter with the ability to
// harvest completed agents. The loop uses it to collect finished outcomes.
type agentRegistryHarvester interface {
	agentRegistrySnapshotter
	Harvest(id string) (orchestration.Outcome, bool)
}
