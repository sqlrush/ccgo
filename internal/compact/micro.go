package compact

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
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
		fmt.Fprintf(hash, "%s\x00%s\x00%s\x00", message.Type, message.UUID, message.Subtype)
		for _, block := range message.Content {
			fmt.Fprintf(hash, "%s\x00%s\x00%s\x00%s\x00%s\x00", block.Type, block.ID, block.Name, block.ToolUseID, block.Text)
			if block.Input != nil {
				hash.Write(block.Input)
			}
		}
	}
	return hex.EncodeToString(hash.Sum(nil))
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
