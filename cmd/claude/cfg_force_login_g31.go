package main

import "ccgo/internal/contracts"

// applyForceLoginMethod applies the forceLoginMethod setting to the login method flags.
// When settings.ForceLoginMethod == "console", loginWithClaudeAI is forced to false.
// CFG-47: CC ref: utils/settings/types.ts forceLoginMethod.
func applyForceLoginMethod(settings contracts.Settings, loginWithClaudeAI *bool) {
	if loginWithClaudeAI == nil {
		return
	}
	if settings.ForceLoginMethod == "console" {
		*loginWithClaudeAI = false
	}
}
