package compact

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
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

func (r *MicroResult) UnmarshalJSON(data []byte) error {
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
		if value, ok, err := microSummaryJSONField(fields, "Summary", "summary", "summaryText", "summary_text", "resultSummary", "result_summary", "summaryMarkdown", "summary_markdown", "compressed", "compressedText", "compressed_text", "content", "text", "value", "output"); err != nil {
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
		); err != nil {
			return err
		} else if ok && value > 0 {
			result.ExpiresAt = result.CreatedAt.Add(value)
		}
	}
	return nil
}

func microResultApplyNestedFieldAliases(result *MicroResult, fields map[string]json.RawMessage) error {
	for _, name := range []string{
		"metadata", "meta",
		"cacheMetadata", "cache_metadata", "cacheMeta", "cache_meta",
		"microMetadata", "micro_metadata", "microcompactMetadata", "microCompactMetadata", "microcompact_metadata",
		"microResultMetadata", "micro_result_metadata",
		"attributes", "properties", "attrs", "info",
		"cacheInfo", "cache_info", "cacheDetails", "cache_details",
		"cacheEntry", "cache_entry", "entry", "record", "cache",
	} {
		raw, ok := fields[name]
		if !ok {
			continue
		}
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
	for _, name := range []string{
		"result", "data", "cache", "cacheEntry", "cache_entry", "entry", "record", "item", "resource", "payload", "response", "body",
		"microcompact", "microCompact", "micro_compact", "micro_result", "microResult", "microcompactResult", "microCompactResult", "micro_compact_result",
		"message", "assistantMessage", "assistant_message", "resultMessage", "result_message", "outputMessage", "output_message", "completion", "completionMessage", "completion_message",
		"attributes", "properties", "value",
	} {
		raw, ok := fields[name]
		if !ok {
			continue
		}
		trimmed := bytes.TrimSpace(raw)
		if len(trimmed) > 0 && trimmed[0] == '{' {
			return raw, true
		}
	}
	return nil, false
}

func microResultHasDirectPayload(fields map[string]json.RawMessage) bool {
	for _, name := range []string{
		"Summary", "summary", "summaryText", "summary_text", "resultSummary", "result_summary", "summaryMarkdown", "summary_markdown", "compressed", "compressedText", "compressed_text", "content", "text", "value", "output",
	} {
		raw, ok := fields[name]
		if !ok {
			continue
		}
		trimmed := bytes.TrimSpace(raw)
		if len(trimmed) == 0 || bytes.Equal(trimmed, []byte("null")) {
			continue
		}
		if trimmed[0] != '{' {
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
	for _, name := range names {
		raw, ok := fields[name]
		if !ok || string(raw) == "null" {
			continue
		}
		var value string
		if err := json.Unmarshal(raw, &value); err != nil {
			return "", false, err
		}
		return value, true, nil
	}
	return "", false, nil
}

func microSummaryJSONField(fields map[string]json.RawMessage, names ...string) (string, bool, error) {
	for _, name := range names {
		raw, ok := fields[name]
		if !ok || string(raw) == "null" {
			continue
		}
		value, ok, err := microSummaryFromRaw(raw, name)
		if err != nil {
			return "", false, err
		}
		if ok {
			return value, true, nil
		}
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
		return text, true, nil
	}
	switch trimmed[0] {
	case '{':
		if text, ok, err := microSummaryContentBlockText(trimmed); err != nil {
			return "", false, err
		} else if ok {
			return text, true, nil
		}
		if text, ok, err := microSummaryMessageText(trimmed); err != nil {
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
		return text, true, nil
	}
	if trimmed[0] != '{' {
		return "", false, fmt.Errorf("invalid summary field %q", field)
	}
	if text, ok, err := microSummaryContentBlockText(trimmed); err != nil {
		return "", false, err
	} else if ok {
		return text, true, nil
	}
	return microSummaryMessageText(trimmed)
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
	for _, name := range names {
		raw, ok := fields[name]
		if !ok || string(raw) == "null" {
			continue
		}
		value, err := microParseJSONBool(raw, name)
		if err != nil {
			return false, false, err
		}
		return value, true, nil
	}
	return false, false, nil
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
	for _, name := range names {
		raw, ok := fields[name]
		if !ok || string(raw) == "null" {
			continue
		}
		var value int
		if err := json.Unmarshal(raw, &value); err == nil {
			return value, true, nil
		}
		var text string
		if err := json.Unmarshal(raw, &text); err != nil {
			return 0, false, err
		}
		parsed, err := strconv.Atoi(text)
		if err != nil {
			return 0, false, err
		}
		return parsed, true, nil
	}
	return 0, false, nil
}

func microTimeJSONField(fields map[string]json.RawMessage, names ...string) (time.Time, bool, error) {
	for _, name := range names {
		raw, ok := fields[name]
		if !ok || string(raw) == "null" {
			continue
		}
		value, err := microParseJSONTime(raw, name)
		if err != nil {
			return time.Time{}, false, err
		}
		return value, true, nil
	}
	return time.Time{}, false, nil
}

func microDurationJSONField(fields map[string]json.RawMessage, names ...string) (time.Duration, bool, error) {
	for _, name := range names {
		raw, ok := fields[name]
		if !ok || string(raw) == "null" {
			continue
		}
		value, err := microParseJSONDuration(raw, name)
		if err != nil {
			return 0, false, err
		}
		return value, true, nil
	}
	return 0, false, nil
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

func microDurationFromFloat(value float64, field string) time.Duration {
	if microDurationFieldIsMillis(field) {
		return time.Duration(value * float64(time.Millisecond))
	}
	return time.Duration(value * float64(time.Second))
}

func microDurationFieldIsMillis(field string) bool {
	lower := strings.ToLower(field)
	return strings.Contains(lower, "ms") || strings.Contains(lower, "millis")
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
