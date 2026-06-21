package hooks

import (
	"testing"

	"ccgo/internal/tool"
)

func TestMatchQuery(t *testing.T) {
	cases := []struct {
		name      string
		phase     string
		payload   map[string]any
		wantQuery string
		wantHonor bool
	}{
		{"pretooluse", tool.HookPreToolUse, map[string]any{"tool_name": "Bash"}, "Bash", true},
		{"sessionstart", tool.HookSessionStart, map[string]any{"source": "startup"}, "startup", true},
		{"sessionend", tool.HookSessionEnd, map[string]any{"reason": "logout"}, "logout", true},
		{"precompact", tool.HookPreCompact, map[string]any{"trigger": "auto"}, "auto", true},
		{"postcompact", tool.HookPostCompact, map[string]any{"trigger": "manual"}, "manual", true},
		{"notification", tool.HookNotification, map[string]any{"notification_type": "permission"}, "permission", true},
		{"subagentstart", tool.HookSubagentStart, map[string]any{"agent_type": "code-reviewer"}, "code-reviewer", true},
		{"subagentstop", tool.HookSubagentStop, map[string]any{"agent_type": "code-reviewer"}, "code-reviewer", true},
		{"stopfailure", tool.HookStopFailure, map[string]any{"error": "boom"}, "boom", true},
		{"stop-no-matcher", tool.HookStop, map[string]any{"stop_reason": "end_turn"}, "", false},
		{"userprompt-no-matcher", tool.HookUserPromptSubmit, map[string]any{"prompt": "hi"}, "", false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			q, honored := MatchQuery(tc.phase, tc.payload)
			if q != tc.wantQuery || honored != tc.wantHonor {
				t.Fatalf("MatchQuery(%s) = %q,%v want %q,%v", tc.phase, q, honored, tc.wantQuery, tc.wantHonor)
			}
		})
	}
}

func TestIsLifecyclePhase(t *testing.T) {
	if !IsLifecyclePhase(tool.HookSessionStart) || !IsLifecyclePhase(tool.HookStop) {
		t.Fatal("expected lifecycle phases")
	}
	if IsLifecyclePhase(tool.HookPreToolUse) || IsLifecyclePhase(tool.HookPermissionRequest) {
		t.Fatal("tool/permission phases are not lifecycle")
	}
}
