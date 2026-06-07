package compact

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"math"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

	"ccgo/internal/contracts"
	msgs "ccgo/internal/messages"
)

const DefaultMicroMaxChars = 4_000
const DefaultMicroCacheVersion = "microcompact.v1"

type MicroOptions struct {
	KeepLast         int
	MaxChars         int
	Cache            *MicroCache
	CacheDir         string
	CacheVersion     string
	CacheTTL         time.Duration
	Now              time.Time
	FailOnCacheError bool
}

type MicroResult struct {
	Summary            string
	Digest             string
	Cached             bool
	MessagesSummarized int
	MessagesKept       int
	Version            string
	CreatedAt          time.Time
	ExpiresAt          time.Time
}

var microSummaryFieldAliases = []string{
	"Summary", "summary", "summaryText", "summary_text", "summaryContent", "summary_content",
	"resultSummary", "result_summary", "finalSummary", "final_summary",
	"summaryMarkdown", "summary_markdown", "markdown", "body",
	"plainText", "plain_text", "displayText", "display_text", "visibleText", "visible_text",
	"answer", "answerText", "answer_text", "refusal", "refusalText", "refusal_text",
	"description", "details", "detail",
	"compressed", "compressedText", "compressed_text", "content", "parts", "text", "value",
	"output", "outputText", "output_text", "resultText", "result_text",
	"completionText", "completion_text", "responseText", "response_text", "messageText", "message_text",
}

var microProviderSummaryFieldAliases = []string{
	"summary", "summaryText", "summary_text", "summaryContent", "summary_content",
	"resultSummary", "result_summary", "finalSummary", "final_summary",
	"summaryMarkdown", "summary_markdown", "markdown", "body",
	"plainText", "plain_text", "displayText", "display_text", "visibleText", "visible_text",
	"answer", "answerText", "answer_text", "refusal", "refusalText", "refusal_text",
	"description", "details", "detail",
	"parts", "segments", "message", "delta", "content", "output", "outputText", "output_text",
	"resultText", "result_text", "completionText", "completion_text",
	"responseText", "response_text", "messageText", "message_text",
}

var microProviderWrapperFieldAliases = []string{
	"value", "values", "payload", "data", "item", "items",
}

func (r *MicroResult) UnmarshalJSON(data []byte) error {
	trimmed := bytes.TrimSpace(data)
	if len(trimmed) > 0 && trimmed[0] == '[' {
		if nested, ok := microResultArrayWrappedJSON(trimmed); ok {
			return json.Unmarshal(nested, r)
		}
	}
	var fields map[string]json.RawMessage
	if err := json.Unmarshal(data, &fields); err != nil {
		return err
	}
	if nested, ok := microResultWrappedJSON(fields); ok {
		var result MicroResult
		if err := json.Unmarshal(nested, &result); err != nil {
			return err
		}
		if err := microResultApplyFieldAliases(&result, fields, false, false); err != nil {
			return err
		}
		if result.Digest == "" {
			if value, ok, err := microStringJSONField(fields, "id", "cacheId", "cacheID", "cache_id", "resourceId", "resourceID", "resource_id"); err != nil {
				return err
			} else if ok {
				result.Digest = value
			}
		}
		if err := microResultApplyNestedFieldAliases(&result, fields); err != nil {
			return err
		}
		*r = result
		return nil
	}
	var result MicroResult
	if err := microResultApplyFieldAliases(&result, fields, true, true); err != nil {
		return err
	}
	if err := microResultApplyNestedFieldAliases(&result, fields); err != nil {
		return err
	}
	*r = result
	return nil
}

func microResultApplyFieldAliases(result *MicroResult, fields map[string]json.RawMessage, overwrite bool, includeSummary bool) error {
	if includeSummary {
		if value, ok, err := microSummaryJSONField(fields, microSummaryFieldAliases...); err != nil {
			return err
		} else if ok && (overwrite || result.Summary == "") {
			result.Summary = value
		}
	}
	if value, ok, err := microStringJSONField(fields, "Digest", "digest", "cacheKey", "cache_key", "cacheDigest", "cache_digest", "digestHash", "digest_hash", "key", "hash", "fingerprint"); err != nil {
		return err
	} else if ok && (overwrite || result.Digest == "") {
		result.Digest = value
	}
	if value, ok, err := microBoolJSONField(fields, "Cached", "cached", "isCached", "is_cached", "fromCache", "from_cache", "cacheHit", "cache_hit"); err != nil {
		return err
	} else if ok && (overwrite || !result.Cached) {
		result.Cached = value
	}
	if value, ok, err := microIntJSONField(fields, "MessagesSummarized", "messagesSummarized", "messages_summarized", "summarized", "summarizedCount", "summarized_count", "summarizedMessages", "summarized_messages", "summaryCount", "summary_count", "messageCount", "message_count", "inputMessages", "input_messages", "totalMessages", "total_messages"); err != nil {
		return err
	} else if ok && (overwrite || result.MessagesSummarized == 0) {
		result.MessagesSummarized = value
	}
	if value, ok, err := microIntJSONField(fields, "MessagesKept", "messagesKept", "messages_kept", "kept", "keptCount", "kept_count", "keptMessages", "kept_messages", "retained", "retainedCount", "retained_count", "retainedMessages", "retained_messages"); err != nil {
		return err
	} else if ok && (overwrite || result.MessagesKept == 0) {
		result.MessagesKept = value
	}
	if value, ok, err := microStringJSONField(fields, "Version", "version", "cacheVersion", "cache_version", "schemaVersion", "schema_version", "formatVersion", "format_version"); err != nil {
		return err
	} else if ok && (overwrite || result.Version == "") {
		result.Version = value
	}
	if value, ok, err := microTimeJSONField(fields,
		"CreatedAt", "createdAt", "created_at", "created",
		"cachedAt", "cached_at", "cacheCreatedAt", "cache_created_at",
		"storedAt", "stored_at", "generatedAt", "generated_at", "updatedAt", "updated_at", "timestamp",
		"createdMs", "created_ms", "createdMillis", "created_millis", "createdAtMs", "created_at_ms", "createdAtMillis", "created_at_millis",
		"cachedAtMs", "cached_at_ms", "cacheCreatedAtMs", "cache_created_at_ms", "storedAtMs", "stored_at_ms", "generatedAtMs", "generated_at_ms", "updatedAtMs", "updated_at_ms", "timestampMs", "timestamp_ms",
		"createdAtUnix", "created_at_unix", "createdAtUnixMs", "created_at_unix_ms",
	); err != nil {
		return err
	} else if ok && (overwrite || result.CreatedAt.IsZero()) {
		result.CreatedAt = value
	}
	if value, ok, err := microTimeJSONField(fields,
		"ExpiresAt", "expiresAt", "expires_at", "expires",
		"expiry", "expiresOn", "expires_on", "expiration", "expirationTime", "expiration_time", "validUntil", "valid_until", "notAfter", "not_after", "cacheExpiresAt", "cache_expires_at",
		"expiresMs", "expires_ms", "expiresMillis", "expires_millis", "expiresAtMs", "expires_at_ms", "expiresAtMillis", "expires_at_millis",
		"expiryMs", "expiry_ms", "expirationMs", "expiration_ms", "expirationTimeMs", "expiration_time_ms", "validUntilMs", "valid_until_ms", "notAfterMs", "not_after_ms", "cacheExpiresAtMs", "cache_expires_at_ms",
		"expiresAtUnix", "expires_at_unix", "expiresAtUnixMs", "expires_at_unix_ms",
	); err != nil {
		return err
	} else if ok && (overwrite || result.ExpiresAt.IsZero()) {
		result.ExpiresAt = value
	}
	if result.ExpiresAt.IsZero() && !result.CreatedAt.IsZero() {
		if value, ok, err := microDurationJSONField(fields,
			"ttl", "ttlSeconds", "ttl_seconds", "ttlSec", "ttl_sec",
			"timeToLive", "time_to_live", "timeToLiveSeconds", "time_to_live_seconds", "ttlInSeconds", "ttl_in_seconds",
			"ttlMs", "ttl_ms", "ttlMillis", "ttl_millis", "ttlMilliseconds", "ttl_milliseconds",
			"timeToLiveMs", "time_to_live_ms", "timeToLiveMillis", "time_to_live_millis", "durationMs", "duration_ms", "durationMillis", "duration_millis",
			"expiresIn", "expires_in", "expiresInSeconds", "expires_in_seconds",
			"expiresInMs", "expires_in_ms", "expiresInMillis", "expires_in_millis", "expiresInMilliseconds", "expires_in_milliseconds",
			"maxAge", "max_age", "maxAgeSeconds", "max_age_seconds", "maxAgeMs", "max_age_ms", "maxAgeMillis", "max_age_millis", "maxAgeMilliseconds", "max_age_milliseconds",
			"validFor", "valid_for", "validForSeconds", "valid_for_seconds", "validForMs", "valid_for_ms", "validForMillis", "valid_for_millis",
			"ttlMinutes", "ttl_minutes", "ttlMins", "ttl_mins", "ttlMin", "ttl_min",
			"timeToLiveMinutes", "time_to_live_minutes", "timeToLiveMins", "time_to_live_mins",
			"expiresInMinutes", "expires_in_minutes", "expiresInMins", "expires_in_mins",
			"maxAgeMinutes", "max_age_minutes", "maxAgeMins", "max_age_mins",
			"validForMinutes", "valid_for_minutes", "validForMins", "valid_for_mins",
			"ttlHours", "ttl_hours", "ttlHrs", "ttl_hrs", "ttlHour", "ttl_hour",
			"timeToLiveHours", "time_to_live_hours", "timeToLiveHrs", "time_to_live_hrs",
			"expiresInHours", "expires_in_hours", "expiresInHrs", "expires_in_hrs",
			"maxAgeHours", "max_age_hours", "maxAgeHrs", "max_age_hrs",
			"validForHours", "valid_for_hours", "validForHrs", "valid_for_hrs",
			"ttlDays", "ttl_days", "ttlDay", "ttl_day",
			"timeToLiveDays", "time_to_live_days",
			"expiresInDays", "expires_in_days",
			"maxAgeDays", "max_age_days",
			"validForDays", "valid_for_days",
		); err != nil {
			return err
		} else if ok && value > 0 {
			result.ExpiresAt = result.CreatedAt.Add(value)
		}
	}
	return nil
}

func microResultApplyNestedFieldAliases(result *MicroResult, fields map[string]json.RawMessage) error {
	for _, field := range microRawJSONFields(fields,
		"metadata", "meta",
		"cacheMetadata", "cache_metadata", "cacheMeta", "cache_meta",
		"microMetadata", "micro_metadata", "microcompactMetadata", "microCompactMetadata", "microcompact_metadata",
		"microResultMetadata", "micro_result_metadata",
		"attributes", "properties", "attrs", "info",
		"cacheInfo", "cache_info", "cacheDetails", "cache_details",
		"cacheEntry", "cache_entry", "entry", "record", "cache", "value",
	) {
		raw := field.raw
		trimmed := bytes.TrimSpace(raw)
		if len(trimmed) == 0 || bytes.Equal(trimmed, []byte("null")) || trimmed[0] != '{' {
			continue
		}
		nested := map[string]json.RawMessage{}
		if err := json.Unmarshal(trimmed, &nested); err != nil {
			return err
		}
		if err := microResultApplyFieldAliases(result, nested, false, false); err != nil {
			return err
		}
	}
	return nil
}

func microResultWrappedJSON(fields map[string]json.RawMessage) (json.RawMessage, bool) {
	if microResultHasDirectPayload(fields) {
		return nil, false
	}
	for _, field := range microRawJSONFields(fields,
		"result", "data", "cache", "cacheEntry", "cache_entry", "entry", "entries", "record", "records", "item", "items", "resource", "resources", "payload", "response", "body",
		"viewer", "edge", "edges", "node", "nodes",
		"choice", "choices", "output", "outputs", "candidate", "candidates", "generation", "generations", "resultList", "result_list", "results", "responseList", "response_list", "responses", "completionChoice", "completion_choice", "completionChoices", "completion_choices", "completions", "alternative", "alternatives", "delta",
		"microcompact", "microCompact", "micro_compact", "micro_result", "microResult", "microcompactResult", "microCompactResult", "micro_compact_result",
		"message", "messages", "assistantMessage", "assistant_message", "resultMessage", "result_message", "outputMessage", "output_message", "completion", "completionMessage", "completion_message",
		"attributes", "properties", "attrs", "value", "values", "included", "collection", "list", "children",
	) {
		raw := field.raw
		trimmed := bytes.TrimSpace(raw)
		if len(trimmed) > 0 && trimmed[0] == '{' {
			return raw, true
		}
		if len(trimmed) > 0 && trimmed[0] == '[' {
			if nested, ok := microResultArrayWrappedJSON(trimmed); ok {
				return nested, true
			}
		}
	}
	return nil, false
}

func microResultArrayWrappedJSON(data json.RawMessage) (json.RawMessage, bool) {
	var items []json.RawMessage
	if err := json.Unmarshal(data, &items); err != nil {
		return nil, false
	}
	for _, item := range items {
		trimmed := bytes.TrimSpace(item)
		if len(trimmed) == 0 || bytes.Equal(trimmed, []byte("null")) || trimmed[0] != '{' {
			continue
		}
		var result MicroResult
		if err := json.Unmarshal(trimmed, &result); err != nil {
			continue
		}
		if result.Summary != "" {
			return item, true
		}
	}
	return nil, false
}

func microResultHasDirectPayload(fields map[string]json.RawMessage) bool {
	for _, name := range microSummaryFieldAliases {
		raw, ok := fields[name]
		if !ok {
			continue
		}
		trimmed := bytes.TrimSpace(raw)
		if len(trimmed) == 0 || bytes.Equal(trimmed, []byte("null")) {
			continue
		}
		if trimmed[0] != '{' && trimmed[0] != '[' {
			return true
		}
		if _, ok, _ := microSummaryFromRaw(raw, name); ok {
			return true
		}
	}
	return false
}

type MicroPruneOptions struct {
	CacheVersion  string
	Now           time.Time
	DeleteInvalid bool
}

type MicroCache struct {
	mu      sync.RWMutex
	entries map[string]MicroResult
}

func NewMicroCache() *MicroCache {
	return &MicroCache{entries: map[string]MicroResult{}}
}

func (c *MicroCache) Get(digest string) (MicroResult, bool) {
	if c == nil {
		return MicroResult{}, false
	}
	c.mu.RLock()
	defer c.mu.RUnlock()
	result, ok := c.entries[digest]
	if ok {
		result.Cached = true
	}
	return result, ok
}

func (c *MicroCache) Set(result MicroResult) {
	if c == nil || result.Digest == "" {
		return
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	result.Cached = false
	c.entries[result.Digest] = result
}

func (c *MicroCache) Prune(options MicroPruneOptions) int {
	if c == nil {
		return 0
	}
	now := options.Now
	if now.IsZero() {
		now = time.Now().UTC()
	}
	version := microCacheVersion(options.CacheVersion)
	c.mu.Lock()
	defer c.mu.Unlock()
	pruned := 0
	for digest, result := range c.entries {
		if result.Digest != digest {
			if options.DeleteInvalid {
				delete(c.entries, digest)
				pruned++
			}
			continue
		}
		if !MicroResultUsable(result, version, now) {
			delete(c.entries, digest)
			pruned++
		}
	}
	return pruned
}

func MicroCompact(history []contracts.Message, options MicroOptions) MicroResult {
	result, _ := MicroCompactStored(history, options)
	return result
}

func MicroCompactStored(history []contracts.Message, options MicroOptions) (MicroResult, error) {
	keepLast := options.KeepLast
	if keepLast < 0 {
		keepLast = 0
	}
	if keepLast > len(history) {
		keepLast = len(history)
	}
	summarized := history[:len(history)-keepLast]
	digest := DigestMessages(summarized)
	now := microNow(options)
	version := microCacheVersion(options.CacheVersion)
	if cached, ok := options.Cache.Get(digest); ok {
		if MicroResultUsable(cached, version, now) {
			cached.MessagesKept = keepLast
			if options.CacheDir != "" {
				if err := SaveMicroResult(options.CacheDir, storedMicroResult(cached)); err != nil {
					return MicroResult{}, err
				}
			}
			return cached, nil
		}
	}
	if options.CacheDir != "" {
		if cached, ok, err := LoadMicroResult(options.CacheDir, digest); err != nil {
			if options.FailOnCacheError {
				return MicroResult{}, err
			}
		} else if ok && MicroResultUsable(cached, version, now) {
			cached.Cached = true
			cached.MessagesKept = keepLast
			options.Cache.Set(cached)
			return cached, nil
		}
	}
	maxChars := options.MaxChars
	if maxChars <= 0 {
		maxChars = DefaultMicroMaxChars
	}
	result := MicroResult{
		Summary:            summarizeMessages(summarized, maxChars),
		Digest:             digest,
		MessagesSummarized: len(summarized),
		MessagesKept:       keepLast,
		Version:            version,
		CreatedAt:          now,
	}
	if options.CacheTTL > 0 {
		result.ExpiresAt = now.Add(options.CacheTTL)
	}
	options.Cache.Set(result)
	if options.CacheDir != "" {
		if err := SaveMicroResult(options.CacheDir, result); err != nil {
			return MicroResult{}, err
		}
	}
	return result, nil
}

func storedMicroResult(result MicroResult) MicroResult {
	result.Cached = false
	return result
}

func MicroResultUsable(result MicroResult, version string, now time.Time) bool {
	if result.Digest == "" {
		return false
	}
	if result.Version != "" && result.Version != microCacheVersion(version) {
		return false
	}
	if !result.ExpiresAt.IsZero() && !now.Before(result.ExpiresAt) {
		return false
	}
	return true
}

func LoadMicroResult(dir string, digest string) (MicroResult, bool, error) {
	if dir == "" || digest == "" {
		return MicroResult{}, false, nil
	}
	data, err := os.ReadFile(microResultPath(dir, digest))
	if err != nil {
		if os.IsNotExist(err) {
			return MicroResult{}, false, nil
		}
		return MicroResult{}, false, err
	}
	var result MicroResult
	if err := json.Unmarshal(data, &result); err != nil {
		return MicroResult{}, false, err
	}
	if result.Digest == "" {
		result.Digest = digest
	}
	if result.Digest != digest {
		return MicroResult{}, false, fmt.Errorf("microcompact cache digest mismatch: got %q want %q", result.Digest, digest)
	}
	return result, true, nil
}

func SaveMicroResult(dir string, result MicroResult) error {
	if dir == "" || result.Digest == "" {
		return nil
	}
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return err
	}
	data, err := json.MarshalIndent(result, "", "  ")
	if err != nil {
		return err
	}
	tmp, err := os.CreateTemp(dir, "."+result.Digest+".*.tmp")
	if err != nil {
		return err
	}
	tmpPath := tmp.Name()
	defer func() { _ = os.Remove(tmpPath) }()
	if _, err := tmp.Write(append(data, '\n')); err != nil {
		_ = tmp.Close()
		return err
	}
	if err := tmp.Chmod(0o600); err != nil {
		_ = tmp.Close()
		return err
	}
	if err := tmp.Close(); err != nil {
		return err
	}
	return os.Rename(tmpPath, microResultPath(dir, result.Digest))
}

func PruneMicroCache(dir string, options MicroPruneOptions) (int, error) {
	if dir == "" {
		return 0, nil
	}
	entries, err := os.ReadDir(dir)
	if err != nil {
		if os.IsNotExist(err) {
			return 0, nil
		}
		return 0, err
	}
	now := options.Now
	if now.IsZero() {
		now = time.Now().UTC()
	}
	version := microCacheVersion(options.CacheVersion)
	pruned := 0
	for _, entry := range entries {
		if entry.IsDir() || filepath.Ext(entry.Name()) != ".json" {
			continue
		}
		path := filepath.Join(dir, entry.Name())
		data, err := os.ReadFile(path)
		if err != nil {
			return pruned, err
		}
		var result MicroResult
		if err := json.Unmarshal(data, &result); err != nil {
			if options.DeleteInvalid {
				if removeErr := os.Remove(path); removeErr != nil {
					return pruned, removeErr
				}
				pruned++
			}
			continue
		}
		digest := strings.TrimSuffix(entry.Name(), filepath.Ext(entry.Name()))
		if result.Digest == "" {
			result.Digest = digest
		}
		if result.Digest != digest {
			if options.DeleteInvalid {
				if err := os.Remove(path); err != nil {
					return pruned, err
				}
				pruned++
			}
			continue
		}
		if !MicroResultUsable(result, version, now) {
			if err := os.Remove(path); err != nil {
				return pruned, err
			}
			pruned++
		}
	}
	return pruned, nil
}

func microStringJSONField(fields map[string]json.RawMessage, names ...string) (string, bool, error) {
	raw, name, ok := microRawJSONField(fields, names...)
	if !ok || bytes.Equal(bytes.TrimSpace(raw), []byte("null")) {
		return "", false, nil
	}
	var value string
	if err := json.Unmarshal(raw, &value); err == nil {
		return value, true, nil
	}
	decoder := json.NewDecoder(bytes.NewReader(raw))
	decoder.UseNumber()
	var number json.Number
	if err := decoder.Decode(&number); err == nil {
		return number.String(), true, nil
	}
	return "", false, fmt.Errorf("invalid string field %q", name)
}

func microSummaryJSONField(fields map[string]json.RawMessage, names ...string) (string, bool, error) {
	raw, name, ok := microRawJSONField(fields, names...)
	if !ok || bytes.Equal(bytes.TrimSpace(raw), []byte("null")) {
		return "", false, nil
	}
	value, ok, err := microSummaryFromRaw(raw, name)
	if err != nil {
		return "", false, err
	}
	if ok {
		return value, true, nil
	}
	return "", false, nil
}

func microSummaryFromRaw(raw json.RawMessage, field string) (string, bool, error) {
	trimmed := bytes.TrimSpace(raw)
	if len(trimmed) == 0 || bytes.Equal(trimmed, []byte("null")) {
		return "", false, nil
	}
	var text string
	if err := json.Unmarshal(trimmed, &text); err == nil {
		if summary, ok := microSummaryFromTextPayload(text); ok {
			return summary, true, nil
		}
		return text, true, nil
	}
	switch trimmed[0] {
	case '{':
		if text, ok, err := microSummaryContentBlockText(trimmed); err != nil {
			return "", false, err
		} else if ok {
			return microSummaryVisibleText(text), true, nil
		}
		if nonText, err := microSummaryNonTextContentBlock(trimmed); err != nil {
			return "", false, err
		} else if nonText {
			return "", false, nil
		}
		if text, ok, err := microSummaryMessageText(trimmed); err != nil {
			return "", false, err
		} else if ok {
			return text, true, nil
		}
		if text, ok, err := microSummaryProviderText(trimmed); err != nil {
			return "", false, err
		} else if ok {
			return text, true, nil
		}
		return "", false, fmt.Errorf("invalid summary field %q: expected text content block or message", field)
	case '[':
		var items []json.RawMessage
		if err := json.Unmarshal(trimmed, &items); err != nil {
			return "", false, err
		}
		parts := make([]string, 0, len(items))
		for _, item := range items {
			part, ok, err := microSummaryArrayItemFromRaw(item, field)
			if err != nil {
				return "", false, err
			}
			if ok {
				parts = append(parts, part)
			}
		}
		if len(parts) == 0 {
			return "", false, fmt.Errorf("invalid summary field %q: expected at least one text item", field)
		}
		return strings.Join(parts, "\n"), true, nil
	default:
		return "", false, fmt.Errorf("invalid summary field %q", field)
	}
}

func microSummaryArrayItemFromRaw(raw json.RawMessage, field string) (string, bool, error) {
	trimmed := bytes.TrimSpace(raw)
	if len(trimmed) == 0 || bytes.Equal(trimmed, []byte("null")) {
		return "", false, nil
	}
	var text string
	if err := json.Unmarshal(trimmed, &text); err == nil {
		if summary, ok := microSummaryFromTextPayload(text); ok {
			return summary, true, nil
		}
		return text, true, nil
	}
	if trimmed[0] != '{' {
		return "", false, fmt.Errorf("invalid summary field %q", field)
	}
	if text, ok, err := microSummaryContentBlockText(trimmed); err != nil {
		return "", false, err
	} else if ok {
		return microSummaryVisibleText(text), true, nil
	}
	if nonText, err := microSummaryNonTextContentBlock(trimmed); err != nil {
		return "", false, err
	} else if nonText {
		return "", false, nil
	}
	if text, ok, err := microSummaryMessageText(trimmed); err != nil {
		return "", false, err
	} else if ok {
		return text, true, nil
	}
	return microSummaryProviderText(trimmed)
}

func microSummaryProviderText(raw json.RawMessage) (string, bool, error) {
	fields := map[string]json.RawMessage{}
	if err := json.Unmarshal(raw, &fields); err != nil {
		return "", false, err
	}
	for _, field := range microRawJSONFields(fields, microProviderSummaryFieldAliases...) {
		text, ok, err := microSummaryFromRaw(field.raw, field.name)
		if err != nil {
			return "", false, err
		}
		if ok {
			if summary, ok := microSummaryFromTextPayload(text); ok {
				return summary, true, nil
			}
			return text, true, nil
		}
	}
	for _, field := range microRawJSONFields(fields, microProviderWrapperFieldAliases...) {
		text, ok, err := microSummaryFromRaw(field.raw, field.name)
		if err != nil {
			continue
		}
		if ok {
			if summary, ok := microSummaryFromTextPayload(text); ok {
				return summary, true, nil
			}
			return text, true, nil
		}
	}
	return "", false, nil
}

func microSummaryFromTextPayload(text string) (string, bool) {
	payload, ok := microSummaryTextJSONPayload(text)
	if !ok {
		return "", false
	}
	var result MicroResult
	if err := json.Unmarshal([]byte(payload), &result); err != nil {
		return "", false
	}
	summary := strings.TrimSpace(result.Summary)
	return summary, summary != ""
}

func microSummaryVisibleText(text string) string {
	if summary, ok := microSummaryFromTextPayload(text); ok {
		return summary
	}
	return text
}

func microSummaryTextJSONPayload(text string) (string, bool) {
	text = strings.TrimSpace(text)
	if text == "" {
		return "", false
	}
	if strings.HasPrefix(text, "{") || strings.HasPrefix(text, "[") {
		return text, true
	}
	start := strings.Index(text, "```")
	if start < 0 {
		return "", false
	}
	afterFence := text[start+3:]
	content := strings.TrimSpace(afterFence)
	end := strings.Index(content, "```")
	if end >= 0 {
		content = content[:end]
	}
	content = strings.TrimSpace(content)
	if strings.HasPrefix(content, "{") || strings.HasPrefix(content, "[") {
		return content, true
	}
	if lineEnd := strings.IndexAny(content, "\r\n"); lineEnd >= 0 {
		candidate := strings.TrimSpace(strings.TrimLeft(content[lineEnd:], "\r\n"))
		if strings.HasPrefix(candidate, "{") || strings.HasPrefix(candidate, "[") {
			return candidate, true
		}
	}
	if fieldEnd := strings.IndexAny(content, " \t"); fieldEnd >= 0 {
		candidate := strings.TrimSpace(content[fieldEnd:])
		if strings.HasPrefix(candidate, "{") || strings.HasPrefix(candidate, "[") {
			return candidate, true
		}
	}
	for _, language := range []string{"json", "jsonc", "javascript", "js"} {
		if len(content) <= len(language) || !strings.EqualFold(content[:len(language)], language) {
			continue
		}
		candidate := strings.TrimSpace(content[len(language):])
		if strings.HasPrefix(candidate, "{") || strings.HasPrefix(candidate, "[") {
			return candidate, true
		}
	}
	return "", false
}

func microSummaryNonTextContentBlock(raw json.RawMessage) (bool, error) {
	var block contracts.ContentBlock
	if err := json.Unmarshal(raw, &block); err != nil {
		return false, err
	}
	switch block.Type {
	case contracts.ContentThinking, contracts.ContentToolUse, contracts.ContentToolResult, contracts.ContentImage, contracts.ContentCacheEdits:
		return true, nil
	default:
		return false, nil
	}
}

func microSummaryContentBlockText(raw json.RawMessage) (string, bool, error) {
	var block contracts.ContentBlock
	if err := json.Unmarshal(raw, &block); err != nil {
		return "", false, err
	}
	if block.Type == contracts.ContentText && block.Text != "" {
		return block.Text, true, nil
	}
	return "", false, nil
}

func microSummaryMessageText(raw json.RawMessage) (string, bool, error) {
	var message contracts.Message
	if err := json.Unmarshal(raw, &message); err != nil {
		return "", false, err
	}
	if text := msgs.TextContent(message); text != "" {
		return text, true, nil
	}
	return "", false, nil
}

func microBoolJSONField(fields map[string]json.RawMessage, names ...string) (bool, bool, error) {
	raw, name, ok := microRawJSONField(fields, names...)
	if !ok || bytes.Equal(bytes.TrimSpace(raw), []byte("null")) {
		return false, false, nil
	}
	value, err := microParseJSONBool(raw, name)
	if err != nil {
		return false, false, err
	}
	return value, true, nil
}

func microParseJSONBool(raw json.RawMessage, field string) (bool, error) {
	var value bool
	if err := json.Unmarshal(raw, &value); err == nil {
		return value, nil
	}
	var text string
	if err := json.Unmarshal(raw, &text); err == nil {
		switch strings.ToLower(strings.TrimSpace(text)) {
		case "true", "1", "yes", "y", "on":
			return true, nil
		case "false", "0", "no", "n", "off":
			return false, nil
		default:
			return false, fmt.Errorf("invalid bool field %q: %q", field, text)
		}
	}
	decoder := json.NewDecoder(strings.NewReader(string(raw)))
	decoder.UseNumber()
	var number json.Number
	if err := decoder.Decode(&number); err == nil {
		value, err := strconv.ParseFloat(number.String(), 64)
		if err != nil {
			return false, err
		}
		switch value {
		case 1:
			return true, nil
		case 0:
			return false, nil
		default:
			return false, fmt.Errorf("invalid bool field %q: %s", field, number.String())
		}
	}
	return false, fmt.Errorf("invalid bool field %q", field)
}

func microIntJSONField(fields map[string]json.RawMessage, names ...string) (int, bool, error) {
	raw, name, ok := microRawJSONField(fields, names...)
	if !ok || bytes.Equal(bytes.TrimSpace(raw), []byte("null")) {
		return 0, false, nil
	}
	value, err := microParseJSONInt(raw, name)
	if err != nil {
		return 0, false, err
	}
	return value, true, nil
}

func microParseJSONInt(raw json.RawMessage, field string) (int, error) {
	var value int
	if err := json.Unmarshal(raw, &value); err == nil {
		return value, nil
	}
	var text string
	if err := json.Unmarshal(raw, &text); err == nil {
		return microParseIntText(text, field)
	}
	decoder := json.NewDecoder(strings.NewReader(string(raw)))
	decoder.UseNumber()
	var number json.Number
	if err := decoder.Decode(&number); err == nil {
		return microParseIntText(number.String(), field)
	}
	return 0, fmt.Errorf("invalid int field %q", field)
}

func microParseIntText(text string, field string) (int, error) {
	text = strings.TrimSpace(text)
	if text == "" {
		return 0, fmt.Errorf("empty int field %q", field)
	}
	parsed, err := strconv.ParseInt(text, 10, 0)
	if err == nil {
		return int(parsed), nil
	}
	value, err := strconv.ParseFloat(text, 64)
	if err != nil {
		return 0, err
	}
	if math.IsInf(value, 0) || math.IsNaN(value) || value != math.Trunc(value) {
		return 0, fmt.Errorf("invalid int field %q: %q", field, text)
	}
	maxInt := int64(^uint(0) >> 1)
	minInt := -maxInt - 1
	if value > float64(maxInt) || value < float64(minInt) {
		return 0, fmt.Errorf("int field %q out of range: %q", field, text)
	}
	return int(value), nil
}

func microTimeJSONField(fields map[string]json.RawMessage, names ...string) (time.Time, bool, error) {
	raw, name, ok := microRawJSONField(fields, names...)
	if !ok || bytes.Equal(bytes.TrimSpace(raw), []byte("null")) {
		return time.Time{}, false, nil
	}
	value, err := microParseJSONTime(raw, name)
	if err != nil {
		return time.Time{}, false, err
	}
	return value, true, nil
}

func microDurationJSONField(fields map[string]json.RawMessage, names ...string) (time.Duration, bool, error) {
	raw, name, ok := microRawJSONField(fields, names...)
	if !ok || bytes.Equal(bytes.TrimSpace(raw), []byte("null")) {
		return 0, false, nil
	}
	value, err := microParseJSONDuration(raw, name)
	if err != nil {
		return 0, false, err
	}
	return value, true, nil
}

func microRawJSONField(fields map[string]json.RawMessage, names ...string) (json.RawMessage, string, bool) {
	matches := microRawJSONFields(fields, names...)
	if len(matches) == 0 {
		return nil, "", false
	}
	return matches[0].raw, matches[0].name, true
}

type microNamedRawJSONField struct {
	name string
	raw  json.RawMessage
}

func microRawJSONFields(fields map[string]json.RawMessage, names ...string) []microNamedRawJSONField {
	var matches []microNamedRawJSONField
	seen := map[string]bool{}
	for _, name := range names {
		if raw, ok := fields[name]; ok {
			matches = append(matches, microNamedRawJSONField{name: name, raw: raw})
			seen[name] = true
		}
	}
	keys := make([]string, 0, len(fields))
	for key := range fields {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	for _, name := range names {
		normalized := microNormalizedJSONFieldName(name)
		for _, key := range keys {
			if seen[key] {
				continue
			}
			if microNormalizedJSONFieldName(key) == normalized {
				matches = append(matches, microNamedRawJSONField{name: key, raw: fields[key]})
				seen[key] = true
			}
		}
	}
	return matches
}

func microNormalizedJSONFieldName(name string) string {
	name = strings.ToLower(strings.TrimSpace(name))
	name = strings.ReplaceAll(name, "_", "")
	name = strings.ReplaceAll(name, "-", "")
	return name
}

func microParseJSONDuration(raw json.RawMessage, field string) (time.Duration, error) {
	var text string
	if err := json.Unmarshal(raw, &text); err == nil {
		text = strings.TrimSpace(text)
		if text == "" {
			return 0, fmt.Errorf("empty duration field %q", field)
		}
		if duration, err := time.ParseDuration(text); err == nil {
			return duration, nil
		}
		if duration, ok := microParseISO8601Duration(text); ok {
			return duration, nil
		}
		number, err := strconv.ParseFloat(text, 64)
		if err != nil {
			return 0, err
		}
		return microDurationFromFloat(number, field), nil
	}
	decoder := json.NewDecoder(strings.NewReader(string(raw)))
	decoder.UseNumber()
	var number json.Number
	if err := decoder.Decode(&number); err == nil {
		value, err := strconv.ParseFloat(number.String(), 64)
		if err != nil {
			return 0, err
		}
		return microDurationFromFloat(value, field), nil
	}
	return 0, fmt.Errorf("invalid duration field %q", field)
}

func microParseISO8601Duration(text string) (time.Duration, bool) {
	value := strings.ToUpper(strings.TrimSpace(text))
	if len(value) < 2 || value[0] != 'P' {
		return 0, false
	}
	value = value[1:]
	inTime := false
	total := 0.0
	for value != "" {
		if value[0] == 'T' {
			if inTime {
				return 0, false
			}
			inTime = true
			value = value[1:]
			continue
		}
		index := strings.IndexFunc(value, func(r rune) bool {
			return !((r >= '0' && r <= '9') || r == '.')
		})
		if index <= 0 {
			return 0, false
		}
		number, err := strconv.ParseFloat(value[:index], 64)
		if err != nil {
			return 0, false
		}
		unit := value[index]
		switch unit {
		case 'W':
			if inTime {
				return 0, false
			}
			total += number * float64(7*24*time.Hour)
		case 'D':
			if inTime {
				return 0, false
			}
			total += number * float64(24*time.Hour)
		case 'H':
			if !inTime {
				return 0, false
			}
			total += number * float64(time.Hour)
		case 'M':
			if !inTime {
				return 0, false
			}
			total += number * float64(time.Minute)
		case 'S':
			if !inTime {
				return 0, false
			}
			total += number * float64(time.Second)
		default:
			return 0, false
		}
		value = value[index+1:]
	}
	if total <= 0 {
		return 0, false
	}
	return time.Duration(total), true
}

func microDurationFromFloat(value float64, field string) time.Duration {
	if microDurationFieldIsMillis(field) {
		return time.Duration(value * float64(time.Millisecond))
	}
	if microDurationFieldIsMinutes(field) {
		return time.Duration(value * float64(time.Minute))
	}
	if microDurationFieldIsHours(field) {
		return time.Duration(value * float64(time.Hour))
	}
	if microDurationFieldIsDays(field) {
		return time.Duration(value * float64(24*time.Hour))
	}
	return time.Duration(value * float64(time.Second))
}

func microDurationFieldIsMillis(field string) bool {
	lower := strings.ToLower(field)
	return strings.Contains(lower, "ms") || strings.Contains(lower, "millis")
}

func microDurationFieldIsMinutes(field string) bool {
	lower := strings.ToLower(field)
	return strings.Contains(lower, "minutes") || strings.Contains(lower, "mins") || strings.HasSuffix(lower, "min") || strings.HasSuffix(lower, "_min")
}

func microDurationFieldIsHours(field string) bool {
	lower := strings.ToLower(field)
	return strings.Contains(lower, "hours") || strings.Contains(lower, "hrs") || strings.HasSuffix(lower, "hour") || strings.HasSuffix(lower, "_hour")
}

func microDurationFieldIsDays(field string) bool {
	lower := strings.ToLower(field)
	return strings.Contains(lower, "days") || strings.HasSuffix(lower, "day") || strings.HasSuffix(lower, "_day")
}

func microParseJSONTime(raw json.RawMessage, field string) (time.Time, error) {
	var text string
	if err := json.Unmarshal(raw, &text); err == nil {
		text = strings.TrimSpace(text)
		if text == "" {
			return time.Time{}, fmt.Errorf("empty time field %q", field)
		}
		if parsed, err := time.Parse(time.RFC3339Nano, text); err == nil {
			return parsed.UTC(), nil
		}
		return microTimeFromNumberString(text, field)
	}
	decoder := json.NewDecoder(strings.NewReader(string(raw)))
	decoder.UseNumber()
	var number json.Number
	if err := decoder.Decode(&number); err == nil {
		return microTimeFromNumber(number, field)
	}
	var value time.Time
	if err := json.Unmarshal(raw, &value); err != nil {
		return time.Time{}, err
	}
	return value.UTC(), nil
}

func microTimeFromNumberString(text string, field string) (time.Time, error) {
	number := json.Number(text)
	if _, err := number.Int64(); err == nil {
		return microTimeFromNumber(number, field)
	}
	if _, err := number.Float64(); err == nil {
		return microTimeFromNumber(number, field)
	}
	return time.Time{}, fmt.Errorf("invalid time field %q: %q", field, text)
}

func microTimeFromNumber(number json.Number, field string) (time.Time, error) {
	if strings.ContainsAny(number.String(), ".eE") {
		value, err := number.Float64()
		if err != nil {
			return time.Time{}, err
		}
		return microTimeFromFloat(value, field), nil
	}
	value, err := number.Int64()
	if err != nil {
		return time.Time{}, err
	}
	return microTimeFromInt(value, field), nil
}

func microTimeFromFloat(value float64, field string) time.Time {
	if microTimeFieldIsMillis(field) || value >= 1e12 || value <= -1e12 {
		millis := int64(value)
		return time.UnixMilli(millis).UTC()
	}
	seconds := int64(value)
	nanos := int64((value - float64(seconds)) * 1_000_000_000)
	return time.Unix(seconds, nanos).UTC()
}

func microTimeFromInt(value int64, field string) time.Time {
	if microTimeFieldIsMillis(field) || value >= 1_000_000_000_000 || value <= -1_000_000_000_000 {
		return time.UnixMilli(value).UTC()
	}
	return time.Unix(value, 0).UTC()
}

func microTimeFieldIsMillis(field string) bool {
	normalized := strings.ToLower(strings.ReplaceAll(field, "_", ""))
	return strings.Contains(normalized, "ms") || strings.Contains(normalized, "millis")
}

func microResultPath(dir string, digest string) string {
	return filepath.Join(dir, digest+".json")
}

func microCacheVersion(version string) string {
	if version != "" {
		return version
	}
	return DefaultMicroCacheVersion
}

func microNow(options MicroOptions) time.Time {
	if !options.Now.IsZero() {
		return options.Now.UTC()
	}
	return time.Now().UTC()
}

func DigestMessages(messages []contracts.Message) string {
	hash := sha256.New()
	for _, message := range messages {
		parentUUID := ""
		if message.ParentUUID != nil {
			parentUUID = string(*message.ParentUUID)
		}
		fmt.Fprintf(hash, "%s\x00%s\x00%s\x00%s\x00%s\x00%t\x00%s\x00", message.ID, message.Type, message.UUID, parentUUID, message.SessionID, message.IsMeta, message.Subtype)
		fmt.Fprintf(hash, "%s\x00", message.Model)
		for _, block := range message.Content {
			fmt.Fprintf(hash, "%s\x00%s\x00%s\x00%s\x00%s\x00%t\x00%s\x00", block.Type, block.ID, block.Name, block.ToolUseID, block.Text, block.IsError, block.CacheReference)
			if block.Input != nil {
				hash.Write(block.Input)
			}
			writeDigestJSON(hash, block.Content)
			writeDigestJSON(hash, block.CacheControl)
			writeDigestJSON(hash, block.Edits)
		}
	}
	return hex.EncodeToString(hash.Sum(nil))
}

func writeDigestJSON(hash interface{ Write([]byte) (int, error) }, value any) {
	if value == nil {
		hash.Write([]byte("\x00"))
		return
	}
	data, err := json.Marshal(value)
	if err != nil {
		hash.Write([]byte(fmt.Sprintf("%T", value)))
		hash.Write([]byte("\x00"))
		return
	}
	hash.Write(data)
	hash.Write([]byte("\x00"))
}

func summarizeMessages(messages []contracts.Message, maxChars int) string {
	if len(messages) == 0 {
		return "No previous messages to microcompact."
	}
	var lines []string
	remaining := maxChars
	for _, message := range messages {
		if remaining <= 0 {
			break
		}
		line := microLine(message)
		if line == "" {
			continue
		}
		if len(line) > remaining {
			line = line[:remaining]
		}
		lines = append(lines, line)
		remaining -= len(line) + 1
	}
	if len(lines) == 0 {
		return "Previous messages contained no text summary."
	}
	return strings.Join(lines, "\n")
}

func microLine(message contracts.Message) string {
	role := string(message.Type)
	if role == "" {
		role = "message"
	}
	text := strings.TrimSpace(msgs.TextContent(message))
	if text != "" {
		text = strings.Join(strings.Fields(text), " ")
		return role + ": " + text
	}
	for _, block := range message.Content {
		switch block.Type {
		case contracts.ContentToolUse:
			return fmt.Sprintf("%s: tool_use %s", role, block.Name)
		case contracts.ContentToolResult:
			return fmt.Sprintf("%s: tool_result %s", role, block.ToolUseID)
		}
	}
	return ""
}
