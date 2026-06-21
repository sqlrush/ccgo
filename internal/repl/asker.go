package repl

import (
	"context"

	"ccgo/internal/contracts"
	"ccgo/internal/tool"
)

// loopAsker implements tool.PermissionAsker by forwarding the request to the
// event loop over askCh. The loop renders a dialog; when the user makes a
// choice the decision is sent back on the reply channel.
type loopAsker struct {
	askCh chan askRequest
}

// Compile-time check that loopAsker satisfies tool.PermissionAsker.
var _ tool.PermissionAsker = loopAsker{}

func (a loopAsker) Ask(ctx context.Context, req tool.PermissionAskRequest) (contracts.PermissionDecision, error) {
	reply := make(chan contracts.PermissionDecision, 1)
	select {
	case a.askCh <- askRequest{req: req, reply: reply}:
	case <-ctx.Done():
		return contracts.PermissionDecision{}, ctx.Err()
	}
	select {
	case d := <-reply:
		return d, nil
	case <-ctx.Done():
		return contracts.PermissionDecision{}, ctx.Err()
	}
}

// decisionFromAction maps a dialog action label to a PermissionBehavior.
// "Allow" and "Allow Session" grant access; anything else denies.
func decisionFromAction(action string) contracts.PermissionBehavior {
	switch action {
	case "Allow", "Allow Session":
		return contracts.PermissionAllow
	default:
		return contracts.PermissionDeny
	}
}
