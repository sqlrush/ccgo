package tool

import "testing"

func TestLifecycleHookPhaseConstants(t *testing.T) {
	cases := map[string]string{
		HookSessionStart:  "SessionStart",
		HookSessionEnd:    "SessionEnd",
		HookNotification:  "Notification",
		HookSubagentStart: "SubagentStart",
		HookPostCompact:   "PostCompact",
		HookStopFailure:   "StopFailure",
	}
	for got, want := range cases {
		if got != want {
			t.Fatalf("hook phase constant = %q want %q", got, want)
		}
	}
	// Sanity: pre-existing constants are unchanged.
	if HookPreToolUse != "PreToolUse" || HookPreCompact != "PreCompact" {
		t.Fatalf("existing constants changed: %q %q", HookPreToolUse, HookPreCompact)
	}
}
