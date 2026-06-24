package main

import (
	"testing"

	"ccgo/internal/contracts"
)

// TestApplyForceLoginMethod_Console verifies that forceLoginMethod="console"
// forces loginWithClaudeAI to false.
// CFG-47: CC ref: utils/settings/types.ts forceLoginMethod.
func TestApplyForceLoginMethod_Console(t *testing.T) {
	loginWithClaudeAI := true
	s := contracts.Settings{ForceLoginMethod: "console"}
	applyForceLoginMethod(s, &loginWithClaudeAI)
	if loginWithClaudeAI {
		t.Error("expected loginWithClaudeAI=false after forceLoginMethod=console, got true")
	}
}

// TestApplyForceLoginMethod_NoOp verifies that an empty ForceLoginMethod is a no-op.
func TestApplyForceLoginMethod_NoOp(t *testing.T) {
	loginWithClaudeAI := true
	s := contracts.Settings{}
	applyForceLoginMethod(s, &loginWithClaudeAI)
	if !loginWithClaudeAI {
		t.Error("expected loginWithClaudeAI=true (unchanged) when ForceLoginMethod is empty")
	}
}

// TestApplyForceLoginMethod_UnknownValue verifies that an unrecognised value is a no-op.
func TestApplyForceLoginMethod_UnknownValue(t *testing.T) {
	loginWithClaudeAI := true
	s := contracts.Settings{ForceLoginMethod: "oauth"}
	applyForceLoginMethod(s, &loginWithClaudeAI)
	if !loginWithClaudeAI {
		t.Error("expected loginWithClaudeAI=true (unchanged) for unrecognised ForceLoginMethod")
	}
}

// TestApplyForceLoginMethod_NilPointer verifies that a nil pointer is handled safely.
func TestApplyForceLoginMethod_NilPointer(t *testing.T) {
	s := contracts.Settings{ForceLoginMethod: "console"}
	// Must not panic.
	applyForceLoginMethod(s, nil)
}
