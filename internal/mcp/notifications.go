package mcp

import (
	"encoding/json"
	"strconv"
	"strings"
)

type NotificationEvent struct {
	ServerName    string         `json:"server_name,omitempty"`
	Method        string         `json:"method"`
	Type          string         `json:"type"`
	Channel       string         `json:"channel,omitempty"`
	Operation     string         `json:"operation,omitempty"`
	Level         string         `json:"level,omitempty"`
	Logger        string         `json:"logger,omitempty"`
	Message       string         `json:"message,omitempty"`
	ProgressToken string         `json:"progress_token,omitempty"`
	Progress      *float64       `json:"progress,omitempty"`
	Total         *float64       `json:"total,omitempty"`
	URI           string         `json:"uri,omitempty"`
	RequestID     string         `json:"request_id,omitempty"`
	Reason        string         `json:"reason,omitempty"`
	Params        map[string]any `json:"params,omitempty"`
}

func NormalizeNotification(serverName string, notification RPCNotification) NotificationEvent {
	params := notificationParamMap(notification.Params)
	channel, operation, eventType := canonicalNotificationMethod(notification.Method)
	event := NotificationEvent{
		ServerName: strings.TrimSpace(serverName),
		Method:     strings.TrimSpace(notification.Method),
		Type:       eventType,
		Channel:    channel,
		Operation:  operation,
		Params:     params,
	}
	switch eventType {
	case "progress":
		event.ProgressToken = stringValue(firstNonEmpty(params["progressToken"], params["progress_token"], params["token"]))
		event.Progress = numberPointer(firstNonEmpty(params["progress"], params["current"], params["value"]))
		event.Total = numberPointer(firstNonEmpty(params["total"], params["max"], params["maximum"]))
		event.Message = notificationMessage(params)
	case "message":
		event.Level = stringValue(firstNonEmpty(params["level"], params["severity"], params["logLevel"], params["log_level"]))
		event.Logger = stringValue(firstNonEmpty(params["logger"], params["loggerName"], params["logger_name"]))
		event.Message = notificationMessage(params)
	case "resources_updated":
		event.URI = notificationURI(params)
	case "cancelled":
		event.RequestID = stringValue(firstNonEmpty(params["requestId"], params["request_id"], params["id"]))
		event.Reason = stringValue(firstNonEmpty(params["reason"], params["message"], params["detail"]))
	}
	return event
}

func (c *ProtocolClient) NotificationEvents(serverName string) []NotificationEvent {
	if c == nil {
		return nil
	}
	c.notificationMu.Lock()
	notifications := append([]RPCNotification(nil), c.notifications...)
	c.notificationMu.Unlock()
	events := make([]NotificationEvent, 0, len(notifications))
	for _, notification := range notifications {
		events = append(events, NormalizeNotification(serverName, notification))
	}
	return events
}

func canonicalNotificationMethod(method string) (string, string, string) {
	normalized := normalizeNotificationMethod(method)
	switch normalized {
	case "progress":
		return "progress", "progress", "progress"
	case "message", "logging/message", "logging_message", "loggingmessage":
		return "logging", "message", "message"
	case "resources/list_changed", "resource/list_changed", "resources_changed", "resource_changed", "resources/listchanged", "resource/listchanged":
		return "resources", "list_changed", "resources_list_changed"
	case "resources/updated", "resource/updated", "resources_updated", "resource_updated", "resourcesupdated", "resourceupdated":
		return "resources", "updated", "resources_updated"
	case "tools/list_changed", "tool/list_changed", "tools_changed", "tool_changed", "tools/listchanged", "tool/listchanged":
		return "tools", "list_changed", "tools_list_changed"
	case "prompts/list_changed", "prompt/list_changed", "prompts_changed", "prompt_changed", "prompts/listchanged", "prompt/listchanged":
		return "prompts", "list_changed", "prompts_list_changed"
	case "roots/list_changed", "root/list_changed", "roots_changed", "root_changed", "roots/listchanged", "root/listchanged":
		return "roots", "list_changed", "roots_list_changed"
	case "cancelled", "canceled", "cancel_request", "cancelrequest":
		return "cancellation", "cancelled", "cancelled"
	}
	if normalized == "" {
		return "", "", "unknown"
	}
	parts := strings.Split(normalized, "/")
	channel := parts[0]
	operation := strings.Join(parts[1:], "_")
	if operation == "" {
		operation = channel
	}
	return channel, operation, strings.ReplaceAll(normalized, "/", "_")
}

func normalizeNotificationMethod(method string) string {
	normalized := strings.TrimSpace(method)
	normalized = strings.TrimPrefix(normalized, "$/")
	normalized = strings.ReplaceAll(normalized, ".", "/")
	normalized = strings.ReplaceAll(normalized, "-", "_")
	normalized = strings.ReplaceAll(normalized, " ", "_")
	normalized = strings.Trim(normalized, "/")
	normalized = strings.ToLower(normalized)
	normalized = strings.TrimPrefix(normalized, "notifications/")
	normalized = strings.ReplaceAll(normalized, "listchanged", "list_changed")
	return normalized
}

func notificationParamMap(raw json.RawMessage) map[string]any {
	if len(strings.TrimSpace(string(raw))) == 0 {
		return nil
	}
	var obj map[string]any
	if err := json.Unmarshal(raw, &obj); err == nil && obj != nil {
		return obj
	}
	var value any
	if err := json.Unmarshal(raw, &value); err == nil {
		return map[string]any{"value": value}
	}
	return map[string]any{"raw": string(raw)}
}

func notificationMessage(params map[string]any) string {
	if params == nil {
		return ""
	}
	if message := stringValue(firstNonEmpty(params["message"], params["text"], params["content"], params["detail"])); message != "" {
		return message
	}
	if data, _ := firstMapValue(params, "data", "payload"); data != nil {
		if message := stringValue(firstNonEmpty(data["message"], data["text"], data["content"], data["detail"])); message != "" {
			return message
		}
	}
	return stringValue(params["data"])
}

func notificationURI(params map[string]any) string {
	if params == nil {
		return ""
	}
	if uri := stringValue(firstNonEmpty(params["uri"], params["resourceURI"], params["resource_uri"])); uri != "" {
		return uri
	}
	resource, _ := firstMapValue(params, "resource", "item")
	if resource == nil {
		return ""
	}
	return stringValue(firstNonEmpty(resource["uri"], resource["resourceURI"], resource["resource_uri"]))
}

func numberPointer(value any) *float64 {
	switch typed := value.(type) {
	case nil:
		return nil
	case float64:
		return &typed
	case float32:
		out := float64(typed)
		return &out
	case int:
		out := float64(typed)
		return &out
	case int64:
		out := float64(typed)
		return &out
	case int32:
		out := float64(typed)
		return &out
	case json.Number:
		out, err := typed.Float64()
		if err == nil {
			return &out
		}
	case string:
		out, err := strconv.ParseFloat(strings.TrimSpace(typed), 64)
		if err == nil {
			return &out
		}
	}
	return nil
}
