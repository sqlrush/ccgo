package anthropic

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"time"

	"ccgo/internal/platform"
)

const DefaultPromptDumpCacheSize = 5

type PromptDumpCacheEntry struct {
	Timestamp string          `json:"timestamp"`
	Request   json.RawMessage `json:"request"`
}

type PromptDumper struct {
	Path                string
	Now                 func() time.Time
	MaxCached           int
	mu                  sync.Mutex
	initialized         bool
	messageCountSeen    int
	lastInitDataHash    string
	lastInitFingerprint string
	cached              []PromptDumpCacheEntry
}

func NewPromptDumper(path string) *PromptDumper {
	return &PromptDumper{Path: path, MaxCached: DefaultPromptDumpCacheSize}
}

func (d *PromptDumper) DumpRequest(body []byte) string {
	if d == nil || d.Path == "" || len(body) == 0 {
		return ""
	}
	timestamp := d.now().Format(time.RFC3339Nano)
	var req map[string]json.RawMessage
	if err := json.Unmarshal(body, &req); err != nil {
		return timestamp
	}

	d.mu.Lock()
	defer d.mu.Unlock()
	d.addCachedLocked(timestamp, body)

	var entries []json.RawMessage
	fingerprint := initFingerprint(req)
	if !d.initialized || fingerprint != d.lastInitFingerprint {
		initData := map[string]json.RawMessage{}
		for key, value := range req {
			if key != "messages" {
				initData[key] = value
			}
		}
		initDataBytes, err := json.Marshal(initData)
		if err == nil {
			initHash := hashBytes(initDataBytes)
			d.lastInitFingerprint = fingerprint
			if !d.initialized {
				d.initialized = true
				d.lastInitDataHash = initHash
				entries = append(entries, dumpEntry("init", timestamp, initDataBytes))
			} else if initHash != d.lastInitDataHash {
				d.lastInitDataHash = initHash
				entries = append(entries, dumpEntry("system_update", timestamp, initDataBytes))
			}
		}
	}

	var messages []json.RawMessage
	if rawMessages := req["messages"]; len(rawMessages) > 0 {
		_ = json.Unmarshal(rawMessages, &messages)
	}
	start := d.messageCountSeen
	if start > len(messages) {
		start = 0
	}
	for _, raw := range messages[start:] {
		var msg struct {
			Role string `json:"role"`
		}
		if json.Unmarshal(raw, &msg) == nil && msg.Role == "user" {
			entries = append(entries, dumpEntry("message", timestamp, raw))
		}
	}
	d.messageCountSeen = len(messages)
	_ = appendDumpEntries(d.Path, entries)
	return timestamp
}

func (d *PromptDumper) DumpResponse(timestamp string, data json.RawMessage) {
	if d == nil || d.Path == "" || timestamp == "" || len(data) == 0 {
		return
	}
	_ = appendDumpEntries(d.Path, []json.RawMessage{dumpEntry("response", timestamp, data)})
}

func (d *PromptDumper) DumpStreamResponse(timestamp string, chunks []json.RawMessage) {
	if d == nil || d.Path == "" || timestamp == "" {
		return
	}
	data, err := json.Marshal(struct {
		Stream bool              `json:"stream"`
		Chunks []json.RawMessage `json:"chunks"`
	}{Stream: true, Chunks: chunks})
	if err != nil {
		return
	}
	d.DumpResponse(timestamp, data)
}

func (d *PromptDumper) CachedRequests() []PromptDumpCacheEntry {
	if d == nil {
		return nil
	}
	d.mu.Lock()
	defer d.mu.Unlock()
	out := make([]PromptDumpCacheEntry, len(d.cached))
	copy(out, d.cached)
	return out
}

func (d *PromptDumper) Clear() {
	if d == nil {
		return
	}
	d.mu.Lock()
	defer d.mu.Unlock()
	d.initialized = false
	d.messageCountSeen = 0
	d.lastInitDataHash = ""
	d.lastInitFingerprint = ""
	d.cached = nil
}

func (d *PromptDumper) now() time.Time {
	if d.Now != nil {
		return d.Now()
	}
	return time.Now().UTC()
}

func (d *PromptDumper) addCachedLocked(timestamp string, body []byte) {
	limit := d.MaxCached
	if limit <= 0 {
		limit = DefaultPromptDumpCacheSize
	}
	raw := append(json.RawMessage(nil), body...)
	d.cached = append(d.cached, PromptDumpCacheEntry{Timestamp: timestamp, Request: raw})
	if len(d.cached) > limit {
		d.cached = append([]PromptDumpCacheEntry(nil), d.cached[len(d.cached)-limit:]...)
	}
}

func appendDumpEntries(path string, entries []json.RawMessage) error {
	if len(entries) == 0 {
		return nil
	}
	if err := platform.EnsureDir(filepath.Dir(path)); err != nil {
		return err
	}
	f, err := os.OpenFile(path, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o600)
	if err != nil {
		return err
	}
	defer f.Close()
	for _, entry := range entries {
		if len(entry) == 0 {
			continue
		}
		if _, err := f.Write(append(entry, '\n')); err != nil {
			return err
		}
	}
	return nil
}

func dumpEntry(entryType string, timestamp string, data json.RawMessage) json.RawMessage {
	entry := struct {
		Type      string          `json:"type"`
		Timestamp string          `json:"timestamp"`
		Data      json.RawMessage `json:"data"`
	}{Type: entryType, Timestamp: timestamp, Data: data}
	encoded, err := json.Marshal(entry)
	if err != nil {
		return nil
	}
	return encoded
}

func initFingerprint(req map[string]json.RawMessage) string {
	var model string
	_ = json.Unmarshal(req["model"], &model)
	var tools []struct {
		Name string `json:"name"`
	}
	_ = json.Unmarshal(req["tools"], &tools)
	toolNames := make([]string, 0, len(tools))
	for _, tool := range tools {
		toolNames = append(toolNames, tool.Name)
	}
	return model + "|" + strings.Join(toolNames, ",") + "|" + systemLength(req["system"])
}

func systemLength(raw json.RawMessage) string {
	if len(raw) == 0 {
		return "0"
	}
	var text string
	if json.Unmarshal(raw, &text) == nil {
		return strconv.Itoa(len(text))
	}
	var blocks []struct {
		Text string `json:"text"`
	}
	if json.Unmarshal(raw, &blocks) == nil {
		total := 0
		for _, block := range blocks {
			total += len(block.Text)
		}
		return strconv.Itoa(total)
	}
	return strconv.Itoa(len(raw))
}

func hashBytes(data []byte) string {
	sum := sha256.Sum256(data)
	return hex.EncodeToString(sum[:])
}
