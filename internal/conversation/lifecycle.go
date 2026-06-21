package conversation

import (
	"context"
	"fmt"
	"strings"

	"ccgo/internal/tool"
)

// SessionStartSource identifies why a session is starting.
type SessionStartSource string

const (
	SessionStartStartup SessionStartSource = "startup"
	SessionStartResume  SessionStartSource = "resume"
	SessionStartClear   SessionStartSource = "clear"
	SessionStartCompact SessionStartSource = "compact"
)

// SessionEndReason identifies why a session is ending.
type SessionEndReason string

const (
	SessionEndClear                     SessionEndReason = "clear"
	SessionEndResume                    SessionEndReason = "resume"
	SessionEndLogout                    SessionEndReason = "logout"
	SessionEndPromptInputExit           SessionEndReason = "prompt_input_exit"
	SessionEndOther                     SessionEndReason = "other"
	SessionEndBypassPermissionsDisabled SessionEndReason = "bypass_permissions_disabled"
)

// RunSessionStartHooks fires SessionStart hooks and returns any injected
// additionalContext text (joined/trimmed). Source becomes the matchQuery so
// hooks with a non-matching matcher are filtered out automatically.
// A blocking hook is treated as a fatal error.
func (r Runner) RunSessionStartHooks(ctx context.Context, source SessionStartSource) (string, error) {
	result, err := r.runConversationHooks(ctx, tool.HookSessionStart, map[string]any{
		"source": string(source),
	})
	if err != nil {
		return "", err
	}
	if result.Block {
		message := result.Message
		if strings.TrimSpace(message) == "" {
			message = "blocked by SessionStart hook"
		}
		return "", fmt.Errorf("%s", message)
	}
	return strings.TrimSpace(result.Message), nil
}

// RunSessionEndHooks fires SessionEnd hooks (best-effort).
// Reason becomes the matchQuery for hook matcher filtering.
func (r Runner) RunSessionEndHooks(ctx context.Context, reason SessionEndReason) error {
	_, err := r.runConversationHooks(ctx, tool.HookSessionEnd, map[string]any{
		"reason": string(reason),
	})
	return err
}

// RunNotificationHooks fires Notification hooks.
// notificationType becomes the matchQuery for hook matcher filtering.
func (r Runner) RunNotificationHooks(ctx context.Context, notificationType, message, title string) error {
	payload := map[string]any{
		"notification_type": notificationType,
		"message":           message,
	}
	if title != "" {
		payload["title"] = title
	}
	_, err := r.runConversationHooks(ctx, tool.HookNotification, payload)
	return err
}
