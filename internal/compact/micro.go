package compact

import (
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
	var result MicroResult
	if value, ok, err := microStringJSONField(fields, "Summary", "summary"); err != nil {
		return err
	} else if ok {
		result.Summary = value
	}
	if value, ok, err := microStringJSONField(fields, "Digest", "digest"); err != nil {
		return err
	} else if ok {
		result.Digest = value
	}
	if value, ok, err := microBoolJSONField(fields, "Cached", "cached"); err != nil {
		return err
	} else if ok {
		result.Cached = value
	}
	if value, ok, err := microIntJSONField(fields, "MessagesSummarized", "messagesSummarized", "messages_summarized"); err != nil {
		return err
	} else if ok {
		result.MessagesSummarized = value
	}
	if value, ok, err := microIntJSONField(fields, "MessagesKept", "messagesKept", "messages_kept"); err != nil {
		return err
	} else if ok {
		result.MessagesKept = value
	}
	if value, ok, err := microStringJSONField(fields, "Version", "version"); err != nil {
		return err
	} else if ok {
		result.Version = value
	}
	if value, ok, err := microTimeJSONField(fields, "CreatedAt", "createdAt", "created_at", "createdAtMs", "created_at_ms", "createdAtMillis", "created_at_millis", "createdAtUnix", "created_at_unix", "createdAtUnixMs", "created_at_unix_ms"); err != nil {
		return err
	} else if ok {
		result.CreatedAt = value
	}
	if value, ok, err := microTimeJSONField(fields, "ExpiresAt", "expiresAt", "expires_at", "expiresAtMs", "expires_at_ms", "expiresAtMillis", "expires_at_millis", "expiresAtUnix", "expires_at_unix", "expiresAtUnixMs", "expires_at_unix_ms"); err != nil {
		return err
	} else if ok {
		result.ExpiresAt = value
	}
	*r = result
	return nil
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

func microBoolJSONField(fields map[string]json.RawMessage, names ...string) (bool, bool, error) {
	for _, name := range names {
		raw, ok := fields[name]
		if !ok || string(raw) == "null" {
			continue
		}
		var value bool
		if err := json.Unmarshal(raw, &value); err != nil {
			return false, false, err
		}
		return value, true, nil
	}
	return false, false, nil
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
