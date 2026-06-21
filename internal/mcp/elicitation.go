package mcp

import (
	"context"
	"encoding/json"
	"strings"
)

type ElicitationRequest struct {
	ID              string         `json:"id,omitempty"`
	Method          string         `json:"method"`
	Message         string         `json:"message,omitempty"`
	RequestedSchema map[string]any `json:"requested_schema,omitempty"`
	Params          map[string]any `json:"params,omitempty"`
}

type ElicitationHandler func(context.Context, ElicitationRequest) (map[string]any, error)

func ElicitationRequestHandler(handler ElicitationHandler, fallback RPCRequestHandler) RPCRequestHandler {
	return func(ctx context.Context, request RPCInboundRequest) (any, *RPCError) {
		parsed, ok := ParseElicitationRequest(request)
		if !ok {
			if fallback != nil {
				return fallback(ctx, request)
			}
			return DefaultRPCRequestHandler(ctx, request)
		}
		if handler == nil {
			return CancelElicitationResponse(), nil
		}
		response, err := handler(ctx, parsed)
		if err != nil {
			return nil, &RPCError{Code: -32603, Message: "failed to handle elicitation request", Data: err.Error()}
		}
		return NormalizeElicitationResponse(response), nil
	}
}

func ParseElicitationRequest(request RPCInboundRequest) (ElicitationRequest, bool) {
	if !IsElicitationCreateMethod(request.Method) {
		return ElicitationRequest{}, false
	}
	params := rawParamMap(request.Params)
	schema, _ := firstMapValue(params, "requestedSchema", "requested_schema", "schema", "inputSchema", "input_schema")
	return ElicitationRequest{
		ID:              strings.TrimSpace(request.ID),
		Method:          strings.TrimSpace(request.Method),
		Message:         stringValue(firstNonEmpty(params["message"], params["prompt"], params["text"], params["description"])),
		RequestedSchema: schema,
		Params:          params,
	}, true
}

func IsElicitationCreateMethod(method string) bool {
	normalized := strings.TrimSpace(method)
	normalized = strings.ReplaceAll(normalized, ".", "/")
	normalized = strings.ReplaceAll(normalized, "-", "_")
	normalized = strings.ReplaceAll(normalized, " ", "_")
	normalized = strings.Trim(normalized, "/")
	normalized = strings.ToLower(normalized)
	switch normalized {
	case "elicitation/create", "elicitation_create", "elicitationcreate":
		return true
	default:
		return false
	}
}

func ElicitationResponse(action string, content map[string]any) map[string]any {
	response := map[string]any{"action": normalizeElicitationAction(action)}
	if len(content) > 0 && response["action"] == "accept" {
		response["content"] = content
	}
	return response
}

func NormalizeElicitationResponse(response map[string]any) map[string]any {
	content, _ := firstMapValue(response, "content", "values", "value")
	return ElicitationResponse(stringValue(firstNonEmpty(response["action"], response["type"], response["status"])), content)
}

func CancelElicitationResponse() map[string]any {
	return ElicitationResponse("cancel", nil)
}

func normalizeElicitationAction(action string) string {
	normalized := strings.ToLower(strings.NewReplacer("-", "_", " ", "_").Replace(strings.TrimSpace(action)))
	switch normalized {
	case "accept", "accepted", "submit", "submitted", "confirm", "confirmed", "ok":
		return "accept"
	case "decline", "declined", "reject", "rejected", "deny", "denied":
		return "decline"
	case "cancel", "cancelled", "canceled", "dismiss", "dismissed", "abort", "aborted":
		return "cancel"
	default:
		return "cancel"
	}
}

func elicitationResponseFromJSON(raw json.RawMessage) map[string]any {
	params := rawParamMap(raw)
	return NormalizeElicitationResponse(params)
}

// ElicitationPrompt is the UI seam an interactive front-end implements to
// resolve an elicitation/create request. Returning a non-nil error (or a nil
// prompt) is treated as a cancel, never propagated as a protocol error.
type ElicitationPrompt func(ctx context.Context, req ElicitationRequest) (action string, content map[string]any, err error)

// InteractiveElicitationHandler adapts an ElicitationPrompt into an
// ElicitationHandler. A nil prompt or a prompt error cancels the elicitation.
func InteractiveElicitationHandler(prompt ElicitationPrompt) ElicitationHandler {
	return func(ctx context.Context, req ElicitationRequest) (map[string]any, error) {
		if prompt == nil {
			return CancelElicitationResponse(), nil
		}
		action, content, err := prompt(ctx, req)
		if err != nil {
			return CancelElicitationResponse(), nil
		}
		return ElicitationResponse(action, content), nil
	}
}
