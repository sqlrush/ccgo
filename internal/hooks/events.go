package hooks

import (
	"strings"

	"ccgo/internal/tool"
)

// MatchQuery returns the value the matcher pattern is tested against for the
// given phase, and whether matching is honored at all. When honored is false
// (Stop, UserPromptSubmit) every configured hook for the phase runs regardless
// of its matcher. Mirrors CC utils/hooks.ts:1615-1670.
func MatchQuery(phase string, payload map[string]any) (string, bool) {
	switch phase {
	case tool.HookPreToolUse, tool.HookPostToolUse, tool.HookPostToolUseFailure,
		tool.HookPermissionRequest, tool.HookPermissionDenied:
		return payloadString(payload, "tool_name"), true
	case tool.HookSessionStart:
		return payloadString(payload, "source"), true
	case tool.HookSessionEnd:
		return payloadString(payload, "reason"), true
	case tool.HookPreCompact, tool.HookPostCompact:
		return payloadString(payload, "trigger"), true
	case tool.HookNotification:
		return payloadString(payload, "notification_type"), true
	case tool.HookSubagentStart, tool.HookSubagentStop:
		return payloadString(payload, "agent_type"), true
	case tool.HookStopFailure:
		return payloadString(payload, "error"), true
	case tool.HookStop, tool.HookUserPromptSubmit:
		return "", false
	default:
		return "", false
	}
}

// IsLifecyclePhase reports whether the phase is a conversation/session
// lifecycle event (not a per-tool-call or permission event).
func IsLifecyclePhase(phase string) bool {
	switch phase {
	case tool.HookPreToolUse, tool.HookPostToolUse, tool.HookPostToolUseFailure,
		tool.HookPermissionRequest, tool.HookPermissionDenied:
		return false
	default:
		return true
	}
}

func payloadString(payload map[string]any, key string) string {
	if payload == nil {
		return ""
	}
	value, _ := payload[key].(string)
	return strings.TrimSpace(value)
}
