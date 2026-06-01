package compact

import (
	"context"
	"strings"
	"testing"

	"ccgo/internal/api/anthropic"
	"ccgo/internal/contracts"
	msgs "ccgo/internal/messages"
)

func TestCalculateWarningStateAndAutoCompactThreshold(t *testing.T) {
	config := WindowConfig{ContextWindow: 200_000, MaxOutputTokens: 20_000, AutoCompactEnabled: true}
	if got := EffectiveContextWindow(config); got != 180_000 {
		t.Fatalf("effective = %d", got)
	}
	if got := AutoCompactThreshold(config); got != 167_000 {
		t.Fatalf("threshold = %d", got)
	}
	state := CalculateWarningState(168_000, config)
	if !state.IsAboveAutoCompactThreshold || state.PercentLeft != 0 {
		t.Fatalf("warning state = %#v", state)
	}
}

func TestSummaryPromptIncludesNoToolsAndExtraInstructions(t *testing.T) {
	prompt := SummaryPrompt(PromptPartial, "Focus on tests.")
	if !strings.Contains(prompt, "Do NOT call any tools") || !strings.Contains(prompt, "recent messages only") || !strings.Contains(prompt, "Focus on tests.") {
		t.Fatalf("prompt = %q", prompt)
	}
}

func TestBuildPlanCreatesBoundarySummaryAndPreservesRecentMessages(t *testing.T) {
	history := []contracts.Message{
		msgs.UserText("one"),
		msgs.AssistantText("two", "sonnet", nil),
		msgs.UserText("three"),
	}
	plan := BuildPlan(history, PlanOptions{
		Trigger:        TriggerManual,
		PreTokens:      123,
		KeepLast:       1,
		Summary:        "summary",
		BoundaryUUID:   "boundary",
		SummaryUUID:    "summary",
		PreserveRecent: true,
	})
	if len(plan.Summarized) != 2 || len(plan.Kept) != 1 || len(plan.Output) != 3 {
		t.Fatalf("plan = %#v", plan)
	}
	if plan.Boundary.Type != contracts.MessageSystem || plan.Boundary.Subtype != "compact_boundary" {
		t.Fatalf("boundary = %#v", plan.Boundary)
	}
	if text := msgs.TextContent(plan.Summary); !strings.Contains(text, "summary") {
		t.Fatalf("summary text = %q", text)
	}
	if plan.Output[2].ParentUUID == nil || *plan.Output[2].ParentUUID != "summary" {
		t.Fatalf("preserved parent = %#v", plan.Output[2].ParentUUID)
	}
	transcriptBoundary := BoundaryTranscriptMessage(plan.Boundary, plan.Metadata)
	if transcriptBoundary.CompactMetadata == nil || transcriptBoundary.CompactMetadata.MessagesSummarized != 2 {
		t.Fatalf("transcript boundary = %#v", transcriptBoundary)
	}
}

func TestEstimateTokensAndShouldRun(t *testing.T) {
	history := []contracts.Message{msgs.UserText(strings.Repeat("x", 400))}
	if got := EstimateTokens(history); got < 90 || got > 110 {
		t.Fatalf("estimate = %d", got)
	}
	if !ShouldRun(history, AutoConfig{Enabled: true, Force: true}) {
		t.Fatal("forced autocompact should run")
	}
	if ShouldRun(history, AutoConfig{Enabled: false, Force: true}) {
		t.Fatal("disabled autocompact should not run")
	}
}

func TestAutoConfigFailureCircuitBreaker(t *testing.T) {
	history := []contracts.Message{msgs.UserText(strings.Repeat("x", 400))}
	config := AutoConfig{
		Enabled:             true,
		TokenUsage:          10_000,
		ConsecutiveFailures: DefaultMaxConsecutiveFailures,
		Window: WindowConfig{
			ContextWindow:      12_000,
			MaxOutputTokens:    1_000,
			AutoCompactEnabled: true,
		},
	}
	if ShouldRun(history, config) {
		t.Fatal("autocompact should stop after failure limit")
	}
	if !ShouldRun(history, AutoConfig{Enabled: true, Force: true, ConsecutiveFailures: DefaultMaxConsecutiveFailures}) {
		t.Fatal("forced autocompact should bypass failure limit")
	}
	RecordFailure(&config)
	if config.ConsecutiveFailures != DefaultMaxConsecutiveFailures+1 {
		t.Fatalf("failure count = %d", config.ConsecutiveFailures)
	}
	RecordSuccess(&config)
	if config.ConsecutiveFailures != 0 {
		t.Fatalf("failure count after success = %d", config.ConsecutiveFailures)
	}
}

func TestRunnerBuildsNoToolSummaryRequestAndPlan(t *testing.T) {
	client := &fakeCompactClient{response: &anthropic.Response{
		ID:      "msg_summary",
		Type:    "message",
		Role:    "assistant",
		Model:   "sonnet",
		Content: []contracts.ContentBlock{contracts.NewTextBlock("summary text")},
		Usage:   contracts.Usage{InputTokens: 10, OutputTokens: 2},
	}}
	history := []contracts.Message{msgs.UserText("one"), msgs.AssistantText("two", "sonnet", nil), msgs.UserText("three")}
	result, err := (Runner{
		Client:            client,
		Model:             "sonnet",
		MaxTokens:         100,
		KeepLast:          1,
		ExtraInstructions: "Focus on code.",
	}).Compact(context.Background(), history, TriggerAuto, 42, "user context")
	if err != nil {
		t.Fatal(err)
	}
	if len(client.request.Tools) != 0 {
		t.Fatalf("compact request should not include tools: %#v", client.request.Tools)
	}
	last := client.request.Messages[len(client.request.Messages)-1]
	if last.Role != "user" || !strings.Contains(last.Content[0].Text, "Do NOT call any tools") || !strings.Contains(last.Content[0].Text, "Focus on code.") {
		t.Fatalf("compact prompt = %#v", last)
	}
	if result.Plan.Metadata.Trigger != string(TriggerAuto) || result.Plan.Metadata.PreTokens != 42 || result.Plan.Metadata.MessagesSummarized != 2 {
		t.Fatalf("plan metadata = %#v", result.Plan.Metadata)
	}
	if text := msgs.TextContent(result.Plan.Summary); !strings.Contains(text, "summary text") {
		t.Fatalf("summary = %q", text)
	}
}

type fakeCompactClient struct {
	request  anthropic.Request
	response *anthropic.Response
	err      error
}

func (f *fakeCompactClient) CreateMessage(ctx context.Context, req anthropic.Request) (*anthropic.Response, error) {
	f.request = req
	return f.response, f.err
}
