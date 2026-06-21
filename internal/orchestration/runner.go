package orchestration

import (
	"context"
	"fmt"

	"ccgo/internal/contracts"
	"ccgo/internal/messages"
)

// TurnResult is the subset of conversation.Result that TeamRunner needs.
// It uses only contracts types so orchestration stays free of the
// conversation→tools/task→orchestration import cycle.
type TurnResult struct {
	Messages   []contracts.Message
	Assistant  contracts.Message
	StopReason string
}

// RunTurnFunc matches the signature of (*conversation.Runner).RunTurn.
// Callers wire this by passing r.RunTurn directly, keeping the orchestration
// package free of a direct conversation import.
type RunTurnFunc func(ctx context.Context, history []contracts.Message, user contracts.Message) (TurnResult, error)

// RunnerFactory builds a RunTurnFunc for a teammate of the given type and
// (optional) model override. Implementations wrap (*conversation.Runner).RunTurn
// after adapting the return type. Tests pass a closure that returns canned data.
type RunnerFactory func(agentType, model string) (RunTurnFunc, error)

// Teammate identifies one team member backed by a sidechain transcript.
type Teammate struct {
	SidechainID string
	AgentType   string
	Model       string
}

// TeamRunner executes real teammate turns. This replaces the append-only Team
// stubs: a teammate now runs an actual model loop via conversation.RunTurn
// (wrapped as RunTurnFunc). The Factory builds a fresh runner per teammate so
// turns are isolated.
type TeamRunner struct {
	Factory RunnerFactory
	// Persist writes the turn's messages back to the teammate's sidechain.
	Persist func(sidechainID string, msgs []contracts.Message) error
}

// RunTeammate runs one prompt against the teammate's runner and persists the
// resulting messages. Returns a summary Outcome. Input history is not mutated.
func (tr TeamRunner) RunTeammate(ctx context.Context, tm Teammate, history []contracts.Message, prompt string) (Outcome, error) {
	if tr.Factory == nil {
		return Outcome{}, fmt.Errorf("team runner: no factory configured")
	}
	runTurn, err := tr.Factory(tm.AgentType, tm.Model)
	if err != nil {
		return Outcome{}, fmt.Errorf("team runner: build runner for %q: %w", tm.AgentType, err)
	}

	// Pass a copy of history so we never mutate the caller's slice.
	historyCopy := make([]contracts.Message, len(history))
	copy(historyCopy, history)

	user := messages.UserText(prompt)
	result, err := runTurn(ctx, historyCopy, user)
	if err != nil {
		return Outcome{Err: err}, fmt.Errorf("team runner: run turn for %q: %w", tm.AgentType, err)
	}

	if tr.Persist != nil {
		if perr := tr.Persist(tm.SidechainID, result.Messages); perr != nil {
			return Outcome{}, fmt.Errorf("team runner: persist %q: %w", tm.SidechainID, perr)
		}
	}

	return Outcome{Summary: summarizeTurnResult(result)}, nil
}

// summarizeTurnResult extracts readable text from the assistant message,
// falling back to StopReason when no text is present.
func summarizeTurnResult(result TurnResult) string {
	if text := messages.TextContent(result.Assistant); text != "" {
		return text
	}
	if result.StopReason != "" {
		return result.StopReason
	}
	// Last resort: scan all messages for any assistant text.
	for _, m := range result.Messages {
		if m.Type == contracts.MessageAssistant {
			if text := messages.TextContent(m); text != "" {
				return text
			}
		}
	}
	return ""
}

