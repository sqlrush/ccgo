package compact

import (
	"context"
	"fmt"
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

func TestShouldRunAppliesWindowEnvOverride(t *testing.T) {
	t.Setenv("CLAUDE_AUTOCOMPACT_PCT_OVERRIDE", "50")
	config := AutoConfig{
		Enabled:    true,
		TokenUsage: 95_000,
		Window: WindowConfig{
			ContextWindow:   200_000,
			MaxOutputTokens: 20_000,
		},
	}
	if !ShouldRun(nil, config) {
		t.Fatal("autocompact should use environment threshold override")
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

func TestLoadMicroResultAcceptsFieldAliases(t *testing.T) {
	cacheDir := filepath.Join(t.TempDir(), "micro")
	if err := os.MkdirAll(cacheDir, 0o755); err != nil {
		t.Fatal(err)
	}
	digest := "alias"
	data := `{
		"summary": "cached summary",
		"digest": "alias",
		"cached": true,
		"messagesSummarized": "7",
		"messages_kept": 2,
		"version": "microcompact.v1",
		"createdAt": "1970-01-01T00:01:40Z",
		"expires_at": "1970-01-01T01:01:40Z"
	}`
	if err := os.WriteFile(microResultPath(cacheDir, digest), []byte(data), 0o600); err != nil {
		t.Fatal(err)
	}
	result, ok, err := LoadMicroResult(cacheDir, digest)
	if err != nil {
		t.Fatal(err)
	}
	if !ok {
		t.Fatal("expected aliased cache result")
	}
	if result.Summary != "cached summary" || result.Digest != digest || !result.Cached || result.MessagesSummarized != 7 || result.MessagesKept != 2 || result.Version != DefaultMicroCacheVersion {
		t.Fatalf("result = %#v", result)
	}
	if !result.CreatedAt.Equal(time.Unix(100, 0).UTC()) || !result.ExpiresAt.Equal(time.Unix(3700, 0).UTC()) {
		t.Fatalf("result times = %#v", result)
	}
}

func TestLoadMicroResultAcceptsAdjacentCacheFieldAliases(t *testing.T) {
	cacheDir := filepath.Join(t.TempDir(), "micro")
	if err := os.MkdirAll(cacheDir, 0o755); err != nil {
		t.Fatal(err)
	}
	digest := "adjacent"
	data := `{
		"content": "adjacent summary",
		"cacheKey": "adjacent",
		"cacheHit": true,
		"summarizedMessages": "5",
		"retained_messages": 1,
		"cacheVersion": "microcompact.v1",
		"created": 100,
		"expiresMs": "3700000"
	}`
	if err := os.WriteFile(microResultPath(cacheDir, digest), []byte(data), 0o600); err != nil {
		t.Fatal(err)
	}
	result, ok, err := LoadMicroResult(cacheDir, digest)
	if err != nil {
		t.Fatal(err)
	}
	if !ok {
		t.Fatal("expected adjacent cache result")
	}
	if result.Summary != "adjacent summary" || result.Digest != digest || !result.Cached || result.MessagesSummarized != 5 || result.MessagesKept != 1 || result.Version != DefaultMicroCacheVersion {
		t.Fatalf("result = %#v", result)
	}
	if !result.CreatedAt.Equal(time.Unix(100, 0).UTC()) || !result.ExpiresAt.Equal(time.Unix(3700, 0).UTC()) {
		t.Fatalf("result times = %#v", result)
	}
}

func TestLoadMicroResultAcceptsAdjacentBoolCacheAliases(t *testing.T) {
	cacheDir := filepath.Join(t.TempDir(), "micro")
	if err := os.MkdirAll(cacheDir, 0o755); err != nil {
		t.Fatal(err)
	}
	for _, tc := range []struct {
		digest string
		field  string
		want   bool
	}{
		{digest: "cache-hit-string-yes", field: `"cacheHit":"yes"`, want: true},
		{digest: "from-cache-number-one", field: `"fromCache":1`, want: true},
		{digest: "is-cached-string-no", field: `"isCached":"no"`, want: false},
	} {
		payload := fmt.Sprintf(`{
			"summary": %q,
			"digest": %q,
			%s,
			"version": "microcompact.v1",
			"createdAt": 100
		}`, tc.digest+" summary", tc.digest, tc.field)
		if err := os.WriteFile(microResultPath(cacheDir, tc.digest), []byte(payload), 0o600); err != nil {
			t.Fatal(err)
		}
		result, ok, err := LoadMicroResult(cacheDir, tc.digest)
		if err != nil {
			t.Fatalf("%s load error: %v", tc.digest, err)
		}
		if !ok {
			t.Fatalf("%s was not loaded", tc.digest)
		}
		if result.Cached != tc.want || result.Summary != tc.digest+" summary" {
			t.Fatalf("%s result = %#v", tc.digest, result)
		}
	}
}

func TestLoadMicroResultAcceptsAdjacentCacheEntryAliases(t *testing.T) {
	cacheDir := filepath.Join(t.TempDir(), "micro")
	if err := os.MkdirAll(cacheDir, 0o755); err != nil {
		t.Fatal(err)
	}
	digest := "entry-alias"
	data := `{
		"cacheEntry": {
			"summaryMarkdown": "entry summary",
			"cacheDigest": "entry-alias",
			"summarizedCount": "6",
			"retainedCount": "2",
			"formatVersion": "microcompact.v1",
			"createdMillis": 100000,
			"ttlMilliseconds": "3600000"
		}
	}`
	if err := os.WriteFile(microResultPath(cacheDir, digest), []byte(data), 0o600); err != nil {
		t.Fatal(err)
	}
	result, ok, err := LoadMicroResult(cacheDir, digest)
	if err != nil {
		t.Fatal(err)
	}
	if !ok {
		t.Fatal("expected cache entry alias result")
	}
	if result.Summary != "entry summary" || result.Digest != digest || result.MessagesSummarized != 6 || result.MessagesKept != 2 || result.Version != DefaultMicroCacheVersion {
		t.Fatalf("result = %#v", result)
	}
	if !result.CreatedAt.Equal(time.Unix(100, 0).UTC()) || !result.ExpiresAt.Equal(time.Unix(3700, 0).UTC()) {
		t.Fatalf("result times = %#v", result)
	}
}

func TestLoadMicroResultAcceptsWrappedCacheObjects(t *testing.T) {
	cacheDir := filepath.Join(t.TempDir(), "micro")
	if err := os.MkdirAll(cacheDir, 0o755); err != nil {
		t.Fatal(err)
	}
	for _, tc := range []struct {
		digest      string
		body        string
		want        string
		wantExpires bool
	}{
		{
			digest: "wrapped-result",
			body:   `"result":{"summary":"result summary","digest":"wrapped-result","version":"microcompact.v1","createdAt":100}`,
			want:   "result summary",
		},
		{
			digest:      "wrapped-data",
			body:        `"data":{"content":"data summary","cacheKey":"wrapped-data","cacheVersion":"microcompact.v1","created":100,"ttlSeconds":3600}`,
			want:        "data summary",
			wantExpires: true,
		},
		{
			digest: "wrapped-value",
			body:   `"value":{"text":"value summary","key":"wrapped-value","schemaVersion":"microcompact.v1","createdAt":100}`,
			want:   "value summary",
		},
		{
			digest:      "resource-attributes",
			body:        `"id":"resource-attributes","type":"microcompact-cache","attributes":{"summaryMarkdown":"attributes summary","version":"microcompact.v1","createdAt":100,"ttlSeconds":3600}`,
			want:        "attributes summary",
			wantExpires: true,
		},
		{
			digest:      "resource-properties",
			body:        `"resource":{"id":"resource-properties","type":"microcompact-cache","properties":{"compressedText":"properties summary","cacheVersion":"microcompact.v1","cachedAt":100,"ttlMs":"3600000"}}`,
			want:        "properties summary",
			wantExpires: true,
		},
		{
			digest:      "wrapped-envelope",
			body:        `"data":{"summary":"envelope summary"},"digest":"wrapped-envelope","version":"microcompact.v1","createdAt":100,"ttlSeconds":3600`,
			want:        "envelope summary",
			wantExpires: true,
		},
		{
			digest: "direct-value",
			body:   `"value":"direct value summary","digest":"direct-value","version":"microcompact.v1","createdAt":100`,
			want:   "direct value summary",
		},
	} {
		if err := os.WriteFile(microResultPath(cacheDir, tc.digest), []byte("{"+tc.body+"}"), 0o600); err != nil {
			t.Fatal(err)
		}
		result, ok, err := LoadMicroResult(cacheDir, tc.digest)
		if err != nil {
			t.Fatalf("%s load error: %v", tc.digest, err)
		}
		if !ok {
			t.Fatalf("%s was not loaded", tc.digest)
		}
		if result.Summary != tc.want || result.Digest != tc.digest || result.Version != DefaultMicroCacheVersion {
			t.Fatalf("%s result = %#v", tc.digest, result)
		}
		if tc.wantExpires && !result.ExpiresAt.Equal(time.Unix(3700, 0).UTC()) {
			t.Fatalf("%s expires_at = %#v", tc.digest, result.ExpiresAt)
		}
	}
}

func TestLoadMicroResultAcceptsContentBlockSummaryPayloads(t *testing.T) {
	cacheDir := filepath.Join(t.TempDir(), "micro")
	if err := os.MkdirAll(cacheDir, 0o755); err != nil {
		t.Fatal(err)
	}
	for _, tc := range []struct {
		digest  string
		payload string
		want    string
	}{
		{
			digest: "summary-block",
			payload: `{
				"summary": {"type": "text", "text": "block summary"},
				"digest": "summary-block",
				"version": "microcompact.v1",
				"createdAt": 100
			}`,
			want: "block summary",
		},
		{
			digest: "summary-array",
			payload: `{
				"summary": [
					{"type": "text", "text": "first summary"},
					{"type": "image", "source": {"type": "base64", "media_type": "image/png", "data": "AA=="}},
					{"type": "text", "text": "second summary"}
				],
				"cacheKey": "summary-array",
				"cacheVersion": "microcompact.v1",
				"cachedAt": 100
			}`,
			want: "first summary\nsecond summary",
		},
		{
			digest: "response-content-blocks",
			payload: `{
				"response": {
					"content": [
						{"type": "text", "text": "response summary"},
						"tail line"
					],
					"cacheDigest": "response-content-blocks",
					"formatVersion": "microcompact.v1",
					"createdAt": 100
				}
			}`,
			want: "response summary\ntail line",
		},
	} {
		if err := os.WriteFile(microResultPath(cacheDir, tc.digest), []byte(tc.payload), 0o600); err != nil {
			t.Fatal(err)
		}
		result, ok, err := LoadMicroResult(cacheDir, tc.digest)
		if err != nil {
			t.Fatalf("%s load error: %v", tc.digest, err)
		}
		if !ok {
			t.Fatalf("%s was not loaded", tc.digest)
		}
		if result.Summary != tc.want || result.Digest != tc.digest || result.Version != DefaultMicroCacheVersion {
			t.Fatalf("%s result = %#v", tc.digest, result)
		}
	}
}

func TestLoadMicroResultAcceptsMetadataCacheAliases(t *testing.T) {
	cacheDir := filepath.Join(t.TempDir(), "micro")
	if err := os.MkdirAll(cacheDir, 0o755); err != nil {
		t.Fatal(err)
	}
	for _, tc := range []struct {
		digest string
		body   string
		want   string
	}{
		{
			digest: "metadata-cache",
			body: `"result":{"summary":"metadata summary","summarizedCount":"4","retainedCount":1},"metadata":{
				"cacheKey":"metadata-cache",
				"cacheVersion":"microcompact.v1",
				"cacheHit":"yes",
				"cachedAt":100,
				"ttlMs":"3600000"
			}`,
			want: "metadata summary",
		},
		{
			digest: "cache-info",
			body: `"summary":"cache info summary","cacheInfo":{
				"digest":"cache-info",
				"version":"microcompact.v1",
				"fromCache":1,
				"createdAt":"1970-01-01T00:01:40Z",
				"expiresIn":3600
			}`,
			want: "cache info summary",
		},
	} {
		if err := os.WriteFile(microResultPath(cacheDir, tc.digest), []byte("{"+tc.body+"}"), 0o600); err != nil {
			t.Fatal(err)
		}
		result, ok, err := LoadMicroResult(cacheDir, tc.digest)
		if err != nil {
			t.Fatalf("%s load error: %v", tc.digest, err)
		}
		if !ok {
			t.Fatalf("%s was not loaded", tc.digest)
		}
		if result.Summary != tc.want || result.Digest != tc.digest || result.Version != DefaultMicroCacheVersion || !result.Cached {
			t.Fatalf("%s result = %#v", tc.digest, result)
		}
		if !result.CreatedAt.Equal(time.Unix(100, 0).UTC()) || !result.ExpiresAt.Equal(time.Unix(3700, 0).UTC()) {
			t.Fatalf("%s times = %#v", tc.digest, result)
		}
	}
}

func TestLoadMicroResultUsesFilenameDigestFallback(t *testing.T) {
	cacheDir := filepath.Join(t.TempDir(), "micro")
	if err := os.MkdirAll(cacheDir, 0o755); err != nil {
		t.Fatal(err)
	}
	digest := "filename-digest"
	payload := `{
		"summary": "filename keyed cache",
		"version": "microcompact.v1",
		"createdAt": 100,
		"ttlSeconds": 3600
	}`
	if err := os.WriteFile(microResultPath(cacheDir, digest), []byte(payload), 0o600); err != nil {
		t.Fatal(err)
	}
	result, ok, err := LoadMicroResult(cacheDir, digest)
	if err != nil {
		t.Fatal(err)
	}
	if !ok {
		t.Fatal("expected filename-keyed cache result")
	}
	if result.Digest != digest || result.Summary != "filename keyed cache" || result.Version != DefaultMicroCacheVersion {
		t.Fatalf("result = %#v", result)
	}
	if !result.ExpiresAt.Equal(time.Unix(3700, 0).UTC()) {
		t.Fatalf("expires_at = %#v", result.ExpiresAt)
	}
	pruned, err := PruneMicroCache(cacheDir, MicroPruneOptions{Now: time.Unix(200, 0).UTC(), DeleteInvalid: true})
	if err != nil {
		t.Fatal(err)
	}
	if pruned != 0 {
		t.Fatalf("filename-keyed cache should remain fresh; pruned=%d", pruned)
	}
	loaded, ok, err := LoadMicroResult(cacheDir, digest)
	if err != nil || !ok || loaded.Digest != digest {
		t.Fatalf("post-prune loaded=%#v ok=%v err=%v", loaded, ok, err)
	}
}

func TestLoadMicroResultAcceptsNumericTimeAliases(t *testing.T) {
	cacheDir := filepath.Join(t.TempDir(), "micro")
	if err := os.MkdirAll(cacheDir, 0o755); err != nil {
		t.Fatal(err)
	}
	for _, tc := range []struct {
		digest  string
		payload string
	}{
		{
			digest: "numeric-seconds",
			payload: `{
				"summary": "seconds cache",
				"digest": "numeric-seconds",
				"version": "microcompact.v1",
				"createdAt": 100,
				"expiresAt": 3700
			}`,
		},
		{
			digest: "numeric-millis",
			payload: `{
				"summary": "millis cache",
				"digest": "numeric-millis",
				"version": "microcompact.v1",
				"createdAtMs": 100000,
				"expires_at_ms": "3700000"
			}`,
		},
	} {
		if err := os.WriteFile(microResultPath(cacheDir, tc.digest), []byte(tc.payload), 0o600); err != nil {
			t.Fatal(err)
		}
		result, ok, err := LoadMicroResult(cacheDir, tc.digest)
		if err != nil {
			t.Fatalf("%s load error: %v", tc.digest, err)
		}
		if !ok {
			t.Fatalf("%s was not loaded", tc.digest)
		}
		if !result.CreatedAt.Equal(time.Unix(100, 0).UTC()) || !result.ExpiresAt.Equal(time.Unix(3700, 0).UTC()) {
			t.Fatalf("%s times = %#v", tc.digest, result)
		}
	}
}

func TestLoadMicroResultAcceptsAdjacentTimeFieldAliases(t *testing.T) {
	cacheDir := filepath.Join(t.TempDir(), "micro")
	if err := os.MkdirAll(cacheDir, 0o755); err != nil {
		t.Fatal(err)
	}
	for _, tc := range []struct {
		digest  string
		payload string
	}{
		{
			digest: "cached-at-valid-until",
			payload: `{
				"summary": "valid until cache",
				"digest": "cached-at-valid-until",
				"version": "microcompact.v1",
				"cachedAt": "1970-01-01T00:01:40Z",
				"validUntil": "1970-01-01T01:01:40Z"
			}`,
		},
		{
			digest: "timestamp-expiration-ms",
			payload: `{
				"summary": "expiration cache",
				"digest": "timestamp-expiration-ms",
				"version": "microcompact.v1",
				"timestampMs": "100000",
				"expirationTimeMs": 3700000
			}`,
		},
		{
			digest: "updated-not-after",
			payload: `{
				"summary": "not after cache",
				"digest": "updated-not-after",
				"version": "microcompact.v1",
				"updatedAt": 100,
				"notAfter": 3700
			}`,
		},
	} {
		if err := os.WriteFile(microResultPath(cacheDir, tc.digest), []byte(tc.payload), 0o600); err != nil {
			t.Fatal(err)
		}
		result, ok, err := LoadMicroResult(cacheDir, tc.digest)
		if err != nil {
			t.Fatalf("%s load error: %v", tc.digest, err)
		}
		if !ok {
			t.Fatalf("%s was not loaded", tc.digest)
		}
		if !result.CreatedAt.Equal(time.Unix(100, 0).UTC()) || !result.ExpiresAt.Equal(time.Unix(3700, 0).UTC()) {
			t.Fatalf("%s times = %#v", tc.digest, result)
		}
	}
}

func TestLoadMicroResultDerivesExpiryFromTTLFields(t *testing.T) {
	cacheDir := filepath.Join(t.TempDir(), "micro")
	if err := os.MkdirAll(cacheDir, 0o755); err != nil {
		t.Fatal(err)
	}
	for _, tc := range []struct {
		digest  string
		payload string
		want    time.Time
	}{
		{
			digest: "ttl-seconds",
			payload: `{
				"summary": "seconds ttl cache",
				"digest": "ttl-seconds",
				"version": "microcompact.v1",
				"createdAt": 100,
				"ttlSeconds": "3600"
			}`,
			want: time.Unix(3700, 0).UTC(),
		},
		{
			digest: "ttl-millis",
			payload: `{
				"summary": "millis ttl cache",
				"digest": "ttl-millis",
				"version": "microcompact.v1",
				"created": 100,
				"expiresInMs": 3700000
			}`,
			want: time.Unix(3800, 0).UTC(),
		},
		{
			digest: "ttl-duration",
			payload: `{
				"summary": "duration ttl cache",
				"cacheKey": "ttl-duration",
				"cacheVersion": "microcompact.v1",
				"createdAt": 100,
				"maxAge": "1h30m"
			}`,
			want: time.Unix(5500, 0).UTC(),
		},
		{
			digest: "ttl-time-to-live",
			payload: `{
				"summary": "time-to-live cache",
				"digest": "ttl-time-to-live",
				"version": "microcompact.v1",
				"cachedAt": 100,
				"timeToLiveSeconds": "3600"
			}`,
			want: time.Unix(3700, 0).UTC(),
		},
		{
			digest: "ttl-valid-for-ms",
			payload: `{
				"summary": "valid-for cache",
				"digest": "ttl-valid-for-ms",
				"version": "microcompact.v1",
				"createdAt": 100,
				"validForMs": "3600000"
			}`,
			want: time.Unix(3700, 0).UTC(),
		},
		{
			digest: "ttl-absolute-wins",
			payload: `{
				"summary": "absolute expiry cache",
				"digest": "ttl-absolute-wins",
				"version": "microcompact.v1",
				"createdAt": 100,
				"expiresAt": 3700,
				"ttlSeconds": 7200
			}`,
			want: time.Unix(3700, 0).UTC(),
		},
	} {
		if err := os.WriteFile(microResultPath(cacheDir, tc.digest), []byte(tc.payload), 0o600); err != nil {
			t.Fatal(err)
		}
		result, ok, err := LoadMicroResult(cacheDir, tc.digest)
		if err != nil {
			t.Fatalf("%s load error: %v", tc.digest, err)
		}
		if !ok {
			t.Fatalf("%s was not loaded", tc.digest)
		}
		if !result.ExpiresAt.Equal(tc.want) {
			t.Fatalf("%s expiry = %#v, want %s", tc.digest, result, tc.want)
		}
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
