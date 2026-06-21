package hooks

import (
	"encoding/json"
	"strings"

	"ccgo/internal/tool"
)

// BuildInput renders the JSON payload a hook receives on stdin/HTTP body. It
// produces the CC base fields plus per-event extras carried in event.Payload.
// Mirrors CC utils/hooks.ts:301-328 (createBaseHookInput).
func BuildInput(ctx tool.Context, event tool.HookEvent) (string, error) {
	payload := map[string]any{
		"session_id":      string(ctx.SessionID),
		"transcript_path": metadataString(ctx.Metadata, tool.MetadataSessionPathKey),
		"cwd":             ctx.WorkingDirectory,
		"hook_event_name": event.Phase,
	}
	if mode := metadataString(ctx.Metadata, tool.MetadataPermissionModeKey); mode != "" {
		payload["permission_mode"] = mode
	}
	if event.ToolName != "" {
		payload["tool_name"] = event.ToolName
	}
	if len(event.Input) > 0 {
		payload["tool_input"] = json.RawMessage(event.Input)
	}
	if event.ToolUse.ID != "" {
		payload["tool_use_id"] = string(event.ToolUse.ID)
	}
	if event.Decision != nil {
		payload["permission_decision"] = event.Decision
	}
	if event.Result != nil {
		payload["tool_response"] = event.Result
	}
	if event.Error != "" {
		payload["error"] = event.Error
	}
	for key, value := range event.Payload {
		key = strings.TrimSpace(key)
		if key != "" {
			payload[key] = value
		}
	}
	data, err := json.Marshal(payload)
	if err != nil {
		return "", err
	}
	return string(data), nil
}
