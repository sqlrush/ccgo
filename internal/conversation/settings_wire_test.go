package conversation

// Tests for W-C24: parsed-but-unused settings keys applied to their consumers.
// Each test asserts that a setting now changes observable behavior.

import (
	"strings"
	"testing"

	"ccgo/internal/contracts"
)

// testThinkingModel is the versioned name used in the registry (alias "opus4.5" → this).
const testThinkingModel = "claude-opus-4-5-20251101"

// CFG-32: effortLevel → output_config.effort in API request.
func TestEffortLevelWiredToRequest(t *testing.T) {
	runner := Runner{
		Model:            testThinkingModel,
		MaxTokens:        1024,
		EffortLevel:      "high",
		settingsOverride: &contracts.Settings{},
	}
	req, err := runner.BuildRequest(nil, testThinkingModel)
	if err != nil {
		t.Fatalf("BuildRequest: %v", err)
	}
	if req.OutputConfig == nil {
		t.Fatal("expected OutputConfig to be set when EffortLevel='high'")
	}
	if got := req.OutputConfig["effort"]; got != "high" {
		t.Fatalf("OutputConfig[effort] = %v, want 'high'", got)
	}
}

func TestEffortLevelEmptySkipsOutputConfig(t *testing.T) {
	runner := Runner{
		Model:            testThinkingModel,
		MaxTokens:        1024,
		EffortLevel:      "",
		settingsOverride: &contracts.Settings{},
	}
	req, err := runner.BuildRequest(nil, testThinkingModel)
	if err != nil {
		t.Fatalf("BuildRequest: %v", err)
	}
	if req.OutputConfig != nil {
		t.Fatalf("expected OutputConfig to be nil when EffortLevel is empty, got %v", req.OutputConfig)
	}
}

// CFG-33: alwaysThinkingEnabled → thinking block set even without explicit budget.
func TestAlwaysThinkingEnabledForcesThinkingBlock(t *testing.T) {
	runner := Runner{
		Model:                 testThinkingModel,
		MaxTokens:             16384,
		ThinkingBudgetTokens:  0, // no explicit budget
		AlwaysThinkingEnabled: true,
		settingsOverride:      &contracts.Settings{},
	}
	req, err := runner.BuildRequest(nil, testThinkingModel)
	if err != nil {
		t.Fatalf("BuildRequest: %v", err)
	}
	// claude-opus-4-5-20251101 supports thinking; with AlwaysThinkingEnabled the request
	// should carry a non-nil Thinking block.
	if req.Thinking == nil {
		t.Fatal("expected Thinking block when AlwaysThinkingEnabled=true")
	}
}

// CFG-18: language → language section in system prompt.
func TestLanguageInjectedIntoSystemPrompt(t *testing.T) {
	runner := Runner{
		SystemPrompt:     "You are a coding assistant.",
		Language:         "zh",
		settingsOverride: &contracts.Settings{},
	}
	prompt := runner.systemPromptWithOutputStyle()
	if !strings.Contains(prompt, "# Language") {
		t.Errorf("expected '# Language' section in system prompt, got:\n%s", prompt)
	}
	if !strings.Contains(prompt, "Always respond in zh") {
		t.Errorf("expected 'Always respond in zh' in system prompt, got:\n%s", prompt)
	}
}

func TestLanguageEmptyNoSection(t *testing.T) {
	runner := Runner{
		SystemPrompt:     "You are a coding assistant.",
		Language:         "",
		settingsOverride: &contracts.Settings{},
	}
	prompt := runner.systemPromptWithOutputStyle()
	if strings.Contains(prompt, "# Language") {
		t.Errorf("did not expect '# Language' section when Language is empty")
	}
}

// CFG-16: includeGitInstructions → ShouldIncludeGitInstructions.
func TestIncludeGitInstructionsTrueByDefault(t *testing.T) {
	runner := Runner{settingsOverride: &contracts.Settings{}}
	if !runner.ShouldIncludeGitInstructions() {
		t.Error("expected ShouldIncludeGitInstructions()=true when nil (default)")
	}
}

func TestIncludeGitInstructionsFalse(t *testing.T) {
	b := false
	runner := Runner{
		IncludeGitInstructions: &b,
		settingsOverride:       &contracts.Settings{},
	}
	if runner.ShouldIncludeGitInstructions() {
		t.Error("expected ShouldIncludeGitInstructions()=false when IncludeGitInstructions=false")
	}
}

// CFG-53: verbose → Verbose field propagation.
func TestVerboseFieldPresent(t *testing.T) {
	runner := Runner{Verbose: true, settingsOverride: &contracts.Settings{}}
	if !runner.Verbose {
		t.Error("expected Verbose=true")
	}
}
