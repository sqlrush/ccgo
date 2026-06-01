package compact

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

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

func TestMicroCompactSummarizesAndCaches(t *testing.T) {
	cache := NewMicroCache()
	history := []contracts.Message{
		msgs.UserText("first message"),
		msgs.AssistantText("second message", "sonnet", nil),
		msgs.UserText("keep me"),
	}
	result := MicroCompact(history, MicroOptions{KeepLast: 1, MaxChars: 200, Cache: cache})
	if result.Cached || result.MessagesSummarized != 2 || result.MessagesKept != 1 {
		t.Fatalf("result = %#v", result)
	}
	if !strings.Contains(result.Summary, "first message") || strings.Contains(result.Summary, "keep me") {
		t.Fatalf("summary = %q", result.Summary)
	}
	cached := MicroCompact(history, MicroOptions{KeepLast: 1, MaxChars: 200, Cache: cache})
	if !cached.Cached || cached.Digest != result.Digest || cached.Summary != result.Summary {
		t.Fatalf("cached = %#v result = %#v", cached, result)
	}
}

func TestDigestMessagesIncludesMetadataAndRichContent(t *testing.T) {
	parent := contracts.ID("parent_1")
	base := contracts.Message{
		ID:         "msg_1",
		Type:       contracts.MessageAssistant,
		UUID:       "uuid_1",
		ParentUUID: &parent,
		SessionID:  "session_1",
		Model:      "sonnet",
		Content: []contracts.ContentBlock{{
			Type:    contracts.ContentToolResult,
			ID:      "result_1",
			Content: map[string]any{"stdout": "one"},
		}},
	}
	same := base
	if DigestMessages([]contracts.Message{base}) != DigestMessages([]contracts.Message{same}) {
		t.Fatal("identical messages should have identical digest")
	}
	differentParent := base
	otherParent := contracts.ID("parent_2")
	differentParent.ParentUUID = &otherParent
	if DigestMessages([]contracts.Message{base}) == DigestMessages([]contracts.Message{differentParent}) {
		t.Fatal("parent uuid should affect digest")
	}
	differentModel := base
	differentModel.Model = "opus"
	if DigestMessages([]contracts.Message{base}) == DigestMessages([]contracts.Message{differentModel}) {
		t.Fatal("model should affect digest")
	}
	differentContent := base
	differentContent.Content = []contracts.ContentBlock{{
		Type:    contracts.ContentToolResult,
		ID:      "result_1",
		Content: map[string]any{"stdout": "two"},
	}}
	if DigestMessages([]contracts.Message{base}) == DigestMessages([]contracts.Message{differentContent}) {
		t.Fatal("rich content should affect digest")
	}
	differentCacheReference := base
	differentCacheReference.Content = []contracts.ContentBlock{base.Content[0]}
	differentCacheReference.Content[0].CacheReference = "cache_ref_1"
	if DigestMessages([]contracts.Message{base}) == DigestMessages([]contracts.Message{differentCacheReference}) {
		t.Fatal("cache metadata should affect digest")
	}
}

func TestMicroCompactPersistsDiskCache(t *testing.T) {
	cacheDir := filepath.Join(t.TempDir(), "micro")
	now := time.Unix(100, 0).UTC()
	history := []contracts.Message{
		msgs.UserText("first message"),
		msgs.AssistantText("second message", "sonnet", nil),
		msgs.UserText("keep me"),
	}
	result, err := MicroCompactStored(history, MicroOptions{KeepLast: 1, MaxChars: 200, CacheDir: cacheDir, CacheTTL: time.Hour, Now: now})
	if err != nil {
		t.Fatal(err)
	}
	if result.Cached {
		t.Fatalf("first result should not be cached: %#v", result)
	}
	if result.Version != DefaultMicroCacheVersion || !result.CreatedAt.Equal(now) || !result.ExpiresAt.Equal(now.Add(time.Hour)) {
		t.Fatalf("metadata = %#v", result)
	}
	loaded, ok, err := LoadMicroResult(cacheDir, result.Digest)
	if err != nil {
		t.Fatal(err)
	}
	if !ok || loaded.Summary != result.Summary {
		t.Fatalf("loaded=%#v ok=%v result=%#v", loaded, ok, result)
	}
	cached, err := MicroCompactStored(history, MicroOptions{KeepLast: 1, MaxChars: 20, CacheDir: cacheDir, Now: now.Add(time.Minute)})
	if err != nil {
		t.Fatal(err)
	}
	if !cached.Cached || cached.Summary != result.Summary || cached.MessagesKept != 1 {
		t.Fatalf("cached = %#v result = %#v", cached, result)
	}
	if err := os.WriteFile(microResultPath(cacheDir, result.Digest), []byte("{"), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := MicroCompactStored(history, MicroOptions{KeepLast: 1, MaxChars: 20, CacheDir: cacheDir, Now: now.Add(2 * time.Minute), FailOnCacheError: true}); err == nil {
		t.Fatal("strict cache load should fail on corrupt cache")
	}
	recovered, err := MicroCompactStored(history, MicroOptions{KeepLast: 1, MaxChars: 30, CacheDir: cacheDir, CacheTTL: time.Hour, Now: now.Add(3 * time.Minute)})
	if err != nil {
		t.Fatal(err)
	}
	if recovered.Cached {
		t.Fatalf("corrupt cache should be recomputed by default: %#v", recovered)
	}
	expired, err := MicroCompactStored(history, MicroOptions{KeepLast: 1, MaxChars: 20, CacheDir: cacheDir, Now: now.Add(2 * time.Hour)})
	if err != nil {
		t.Fatal(err)
	}
	if expired.Cached || expired.Summary == result.Summary {
		t.Fatalf("expired cache should be recomputed with new max chars: expired=%#v result=%#v", expired, result)
	}
}

func TestMicroCompactWriteThroughPersistsMemoryCache(t *testing.T) {
	cache := NewMicroCache()
	cacheDir := filepath.Join(t.TempDir(), "micro")
	now := time.Unix(100, 0).UTC()
	history := []contracts.Message{
		msgs.UserText("first message"),
		msgs.AssistantText("second message", "sonnet", nil),
		msgs.UserText("keep me"),
	}
	memoryOnly, err := MicroCompactStored(history, MicroOptions{KeepLast: 1, MaxChars: 200, Cache: cache, CacheTTL: time.Hour, Now: now})
	if err != nil {
		t.Fatal(err)
	}
	if memoryOnly.Cached {
		t.Fatalf("first result should not be cached: %#v", memoryOnly)
	}
	cached, err := MicroCompactStored(history, MicroOptions{KeepLast: 1, MaxChars: 20, Cache: cache, CacheDir: cacheDir, Now: now.Add(time.Minute)})
	if err != nil {
		t.Fatal(err)
	}
	if !cached.Cached || cached.Summary != memoryOnly.Summary {
		t.Fatalf("cached = %#v memoryOnly = %#v", cached, memoryOnly)
	}
	loaded, ok, err := LoadMicroResult(cacheDir, memoryOnly.Digest)
	if err != nil {
		t.Fatal(err)
	}
	if !ok || loaded.Cached || loaded.Summary != memoryOnly.Summary || loaded.Digest != memoryOnly.Digest {
		t.Fatalf("loaded=%#v ok=%v memoryOnly=%#v", loaded, ok, memoryOnly)
	}
}

func TestPruneMicroCacheDeletesExpiredVersionedAndInvalidEntries(t *testing.T) {
	cacheDir := filepath.Join(t.TempDir(), "micro")
	now := time.Unix(100, 0).UTC()
	if err := SaveMicroResult(cacheDir, MicroResult{Digest: "expired", Summary: "old", Version: DefaultMicroCacheVersion, CreatedAt: now.Add(-2 * time.Hour), ExpiresAt: now.Add(-time.Hour)}); err != nil {
		t.Fatal(err)
	}
	if err := SaveMicroResult(cacheDir, MicroResult{Digest: "wrongversion", Summary: "old", Version: "other", CreatedAt: now}); err != nil {
		t.Fatal(err)
	}
	if err := SaveMicroResult(cacheDir, MicroResult{Digest: "fresh", Summary: "new", Version: DefaultMicroCacheVersion, CreatedAt: now, ExpiresAt: now.Add(time.Hour)}); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(cacheDir, "mismatch.json"), []byte(`{"Digest":"other","Summary":"bad","Version":"microcompact.v1"}`), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(cacheDir, "bad.json"), []byte("{"), 0o600); err != nil {
		t.Fatal(err)
	}
	pruned, err := PruneMicroCache(cacheDir, MicroPruneOptions{Now: now, DeleteInvalid: true})
	if err != nil {
		t.Fatal(err)
	}
	if pruned != 4 {
		t.Fatalf("pruned = %d", pruned)
	}
	if _, ok, err := LoadMicroResult(cacheDir, "fresh"); err != nil || !ok {
		t.Fatalf("fresh ok=%v err=%v", ok, err)
	}
}

func TestInMemoryMicroCachePrunesExpiredVersionedAndInvalidEntries(t *testing.T) {
	cache := NewMicroCache()
	now := time.Unix(100, 0).UTC()
	cache.Set(MicroResult{Digest: "expired", Summary: "old", Version: DefaultMicroCacheVersion, CreatedAt: now.Add(-2 * time.Hour), ExpiresAt: now.Add(-time.Hour)})
	cache.Set(MicroResult{Digest: "wrongversion", Summary: "old", Version: "other", CreatedAt: now})
	cache.Set(MicroResult{Digest: "fresh", Summary: "new", Version: DefaultMicroCacheVersion, CreatedAt: now, ExpiresAt: now.Add(time.Hour)})
	cache.mu.Lock()
	cache.entries["mismatch"] = MicroResult{Digest: "other", Summary: "bad", Version: DefaultMicroCacheVersion}
	cache.mu.Unlock()

	pruned := cache.Prune(MicroPruneOptions{Now: now, DeleteInvalid: true})
	if pruned != 3 {
		t.Fatalf("pruned = %d", pruned)
	}
	if cached, ok := cache.Get("fresh"); !ok || cached.Summary != "new" || !cached.Cached {
		t.Fatalf("fresh cached=%#v ok=%v", cached, ok)
	}
	for _, digest := range []string{"expired", "wrongversion", "mismatch"} {
		if cached, ok := cache.Get(digest); ok {
			t.Fatalf("%s should be pruned: %#v", digest, cached)
		}
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
