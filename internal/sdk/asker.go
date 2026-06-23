package sdk

import (
	"context"
	"fmt"
	"sync"

	"ccgo/internal/contracts"
	"ccgo/internal/tool"
)

// controlAsker implements tool.PermissionAsker by emitting a can_use_tool
// control_request over the SDK wire and blocking until the SDK client sends
// back the matching control_response. Concurrency-safe: multiple goroutines
// may call Ask simultaneously; each is correlated by request_id.
//
// CC reference: structuredIO.ts:533-659 (createCanUseTool),
// controlSchemas.ts:106-122 (can_use_tool payload/response shape).
type controlAsker struct {
	send      func(ControlRequest) error
	nextReqID func() string

	mu      sync.Mutex
	waiting map[string]chan contracts.PermissionDecision
}

// newControlAsker returns a controlAsker ready to use.
// send emits a ControlRequest on the SDK wire (e.g. enc.WriteRequest).
// nextReqID generates unique request IDs.
func newControlAsker(send func(ControlRequest) error, nextReqID func() string) *controlAsker {
	return &controlAsker{
		send:      send,
		nextReqID: nextReqID,
		waiting:   make(map[string]chan contracts.PermissionDecision),
	}
}

// Ask implements tool.PermissionAsker. It sends a can_use_tool control_request
// and blocks until the SDK client delivers the matching decision via Resolve, or
// ctx is cancelled. Goroutine-safe; ctx cancellation removes the pending entry.
func (a *controlAsker) Ask(ctx context.Context, req tool.PermissionAskRequest) (contracts.PermissionDecision, error) {
	id := a.nextReqID()
	reply := make(chan contracts.PermissionDecision, 1)

	a.mu.Lock()
	a.waiting[id] = reply
	a.mu.Unlock()

	defer func() {
		a.mu.Lock()
		delete(a.waiting, id)
		a.mu.Unlock()
	}()

	payload := map[string]any{
		"subtype":      "can_use_tool",
		"tool_name":    req.ToolName,
		"tool_use_id":  string(req.ToolUseID),
		"blocked_path": req.Path,
		"description":  req.Description,
		// CC-required fields from controlSchemas.ts:106-122.
		"input": req.Input,
	}
	if req.DisplayName != "" {
		payload["display_name"] = req.DisplayName
	}
	if req.AgentID != "" {
		payload["agent_id"] = req.AgentID
	}
	if req.Title != "" {
		payload["title"] = req.Title
	}
	if len(req.PermissionSuggestions) > 0 {
		payload["permission_suggestions"] = req.PermissionSuggestions
	}
	ctrl := ControlRequest{
		Type:      "control_request",
		RequestID: id,
		Request:   payload,
	}
	if err := a.send(ctrl); err != nil {
		return contracts.PermissionDecision{}, fmt.Errorf("sdk: send can_use_tool: %w", err)
	}

	select {
	case d := <-reply:
		return d, nil
	case <-ctx.Done():
		return contracts.PermissionDecision{}, ctx.Err()
	}
}

// Resolve delivers a decision for a pending can_use_tool request. Called by the
// SDK read-loop when a control_response with matching request_id arrives.
// No-op if requestID is not pending (e.g. ctx was already cancelled or already resolved).
// Uses non-blocking send to guard against duplicate delivery.
func (a *controlAsker) Resolve(requestID string, decision contracts.PermissionDecision) {
	a.mu.Lock()
	ch := a.waiting[requestID]
	delete(a.waiting, requestID)
	a.mu.Unlock()
	if ch != nil {
		select {
		case ch <- decision:
		default:
		}
	}
}
