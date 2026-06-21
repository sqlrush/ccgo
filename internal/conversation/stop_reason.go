package conversation

import (
	"ccgo/internal/contracts"
	msgs "ccgo/internal/messages"
)

type stopAction int

const (
	stopActionContinue stopAction = iota
	stopActionRecoverMaxTokens
	stopActionResumePauseTurn
	stopActionRefusal
	stopActionContextWindowExceeded
)

// maxOutputTokensRecoveryLimit mirrors CC's MAX_OUTPUT_TOKENS_RECOVERY_LIMIT (query.ts:164).
const maxOutputTokensRecoveryLimit = 3

// maxPauseTurnResumes bounds the number of times pause_turn can be resumed within a single turn,
// preventing infinite loops if the server continuously returns pause_turn.
const maxPauseTurnResumes = 10

// classifyStopReason maps an Anthropic stop_reason string to the agent loop's control action.
func classifyStopReason(reason string) stopAction {
	switch reason {
	case "max_tokens":
		return stopActionRecoverMaxTokens
	case "pause_turn":
		return stopActionResumePauseTurn
	case "refusal":
		return stopActionRefusal
	case "model_context_window_exceeded":
		return stopActionContextWindowExceeded
	default:
		// "", "end_turn", "tool_use", "stop_sequence" and any unknown reasons are treated as continue.
		return stopActionContinue
	}
}

const refusalMessageText = "The model declined to respond because the request was flagged by Anthropic's Usage Policy. Try rephrasing your request, or switch models with /model."

const maxTokensRecoveryText = "[The previous response was truncated because it reached the max output tokens limit. Continue from where you left off.]"

const contextWindowExceededText = "The conversation reached the model's context window limit. Older messages must be compacted (/compact) before continuing."

const pauseTurnLimitText = "Turn paused too many times; stopping."

// refusalMessage builds the surfaced assistant message for a usage-policy refusal.
func (r Runner) refusalMessage() contracts.Message {
	msg := msgs.AssistantText(refusalMessageText, "", nil)
	if r.SessionID != "" {
		msg.SessionID = r.SessionID
	}
	return msg
}

// maxTokensContinuationMessage builds the user nudge that drives max_tokens recovery.
func (r Runner) maxTokensContinuationMessage() contracts.Message {
	msg := msgs.UserText(maxTokensRecoveryText)
	if r.SessionID != "" {
		msg.SessionID = r.SessionID
	}
	return msg
}

// contextWindowExceededMessage builds the surfaced message for a context-window overflow.
// Full compaction recovery is implemented in Task 6; here we only surface the message and stop.
func (r Runner) contextWindowExceededMessage() contracts.Message {
	msg := msgs.AssistantText(contextWindowExceededText, "", nil)
	if r.SessionID != "" {
		msg.SessionID = r.SessionID
	}
	return msg
}

// pauseTurnLimitMessage builds the surfaced message when pause_turn resume limit is reached.
func (r Runner) pauseTurnLimitMessage() contracts.Message {
	msg := msgs.AssistantText(pauseTurnLimitText, "", nil)
	if r.SessionID != "" {
		msg.SessionID = r.SessionID
	}
	return msg
}
