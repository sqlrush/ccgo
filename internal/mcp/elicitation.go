package mcp

import (
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
	content, _ := firstMapValue(params, "content", "values", "value")
	return ElicitationResponse(stringValue(firstNonEmpty(params["action"], params["type"], params["status"])), content)
}
