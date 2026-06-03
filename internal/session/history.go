package session

import (
	"bufio"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"sync"
	"time"

	"ccgo/internal/contracts"
	"ccgo/internal/platform"
)

const MaxHistoryItems = 100
const MaxPastedContentLength = 1024
const defaultHistoryLockTimeout = 2 * time.Second
const staleHistoryLockAge = 30 * time.Second

const (
	PastedContentText  = "text"
	PastedContentImage = "image"
)

type PastedContent struct {
	ID        int    `json:"id"`
	Type      string `json:"type"`
	Content   string `json:"content,omitempty"`
	MediaType string `json:"mediaType,omitempty"`
	Filename  string `json:"filename,omitempty"`
}

type StoredPastedContent struct {
	ID          int    `json:"id"`
	Type        string `json:"type"`
	Content     string `json:"content,omitempty"`
	ContentHash string `json:"contentHash,omitempty"`
	MediaType   string `json:"mediaType,omitempty"`
	Filename    string `json:"filename,omitempty"`
}

type HistoryEntry struct {
	Display        string                `json:"display"`
	PastedContents map[int]PastedContent `json:"pastedContents"`
}

type LogEntry struct {
	Display        string                      `json:"display"`
	PastedContents map[int]StoredPastedContent `json:"pastedContents"`
	Timestamp      int64                       `json:"timestamp"`
	Project        string                      `json:"project"`
	SessionID      contracts.ID                `json:"sessionId,omitempty"`
}

type TimestampedHistoryEntry struct {
	Display   string
	Timestamp int64
	Entry     HistoryEntry
}

type BufferedHistoryWriter struct {
	mu      sync.Mutex
	Path    string
	Project string
	Session contracts.ID
	entries []HistoryEntry
}

type Reference struct {
	ID    int
	Kind  string
	Match string
	Index int
}

type PasteResolver func(hash string) (content string, ok bool)

var referencePattern = regexp.MustCompile(`\[(Pasted text|Image|\.\.\.Truncated text) #(\d+)(?: \+\d+ lines)?(\.)*\]`)

func HistoryPath() string {
	return filepath.Join(platform.ClaudeHomeDir(), "history.jsonl")
}

func PasteStoreDir() string {
	return filepath.Join(platform.ClaudeHomeDir(), "paste-cache")
}

func PastePath(hash string) string {
	return filepath.Join(PasteStoreDir(), hash+".txt")
}

func PastedTextRefNumLines(text string) int {
	return strings.Count(text, "\n") + strings.Count(text, "\r") - strings.Count(text, "\r\n")
}

func FormatPastedTextRef(id int, numLines int) string {
	if numLines == 0 {
		return "[Pasted text #" + strconv.Itoa(id) + "]"
	}
	return "[Pasted text #" + strconv.Itoa(id) + " +" + strconv.Itoa(numLines) + " lines]"
}

func FormatImageRef(id int) string {
	return "[Image #" + strconv.Itoa(id) + "]"
}

func ParseReferences(input string) []Reference {
	matches := referencePattern.FindAllStringSubmatchIndex(input, -1)
	refs := make([]Reference, 0, len(matches))
	for _, m := range matches {
		if len(m) < 6 || m[4] < 0 || m[5] < 0 {
			continue
		}
		id, err := strconv.Atoi(input[m[4]:m[5]])
		if err != nil || id <= 0 {
			continue
		}
		refs = append(refs, Reference{
			ID:    id,
			Kind:  input[m[2]:m[3]],
			Match: input[m[0]:m[1]],
			Index: m[0],
		})
	}
	return refs
}

func ExpandPastedTextRefs(input string, pastedContents map[int]PastedContent) string {
	refs := ParseReferences(input)
	expanded := input
	for i := len(refs) - 1; i >= 0; i-- {
		ref := refs[i]
		content, ok := pastedContents[ref.ID]
		if !ok || content.Type != PastedContentText {
			continue
		}
		expanded = expanded[:ref.Index] + content.Content + expanded[ref.Index+len(ref.Match):]
	}
	return expanded
}

func HashPastedText(content string) string {
	sum := sha256.Sum256([]byte(content))
	return hex.EncodeToString(sum[:])[:16]
}

func StorePastedText(hash string, content string) error {
	if err := platform.EnsureDir(PasteStoreDir()); err != nil {
		return err
	}
	return os.WriteFile(PastePath(hash), []byte(content), 0o600)
}

func RetrievePastedText(hash string) (string, bool) {
	data, err := os.ReadFile(PastePath(hash))
	if err != nil {
		return "", false
	}
	return string(data), true
}

func PrepareStoredPastedContents(pastedContents map[int]PastedContent) map[int]StoredPastedContent {
	stored := map[int]StoredPastedContent{}
	for id, content := range pastedContents {
		if content.Type == PastedContentImage {
			continue
		}
		if len(content.Content) <= MaxPastedContentLength {
			stored[id] = StoredPastedContent{
				ID:        content.ID,
				Type:      content.Type,
				Content:   content.Content,
				MediaType: content.MediaType,
				Filename:  content.Filename,
			}
			continue
		}
		stored[id] = StoredPastedContent{
			ID:          content.ID,
			Type:        content.Type,
			ContentHash: HashPastedText(content.Content),
			MediaType:   content.MediaType,
			Filename:    content.Filename,
		}
	}
	return stored
}

func NewLogEntry(project string, sessionID contracts.ID, entry HistoryEntry, now time.Time) LogEntry {
	if now.IsZero() {
		now = time.Now()
	}
	return LogEntry{
		Display:        entry.Display,
		PastedContents: PrepareStoredPastedContents(entry.PastedContents),
		Timestamp:      now.UnixMilli(),
		Project:        project,
		SessionID:      sessionID,
	}
}

func LogEntryToHistoryEntry(entry LogEntry, resolver PasteResolver) HistoryEntry {
	pastedContents := map[int]PastedContent{}
	for id, stored := range entry.PastedContents {
		if stored.Type == PastedContentImage {
			pastedContents[id] = PastedContent{
				ID:        stored.ID,
				Type:      stored.Type,
				MediaType: stored.MediaType,
				Filename:  stored.Filename,
			}
			continue
		}
		if stored.Content != "" {
			pastedContents[id] = PastedContent{
				ID:        stored.ID,
				Type:      stored.Type,
				Content:   stored.Content,
				MediaType: stored.MediaType,
				Filename:  stored.Filename,
			}
			continue
		}
		if stored.ContentHash == "" || resolver == nil {
			continue
		}
		content, ok := resolver(stored.ContentHash)
		if !ok {
			continue
		}
		pastedContents[id] = PastedContent{
			ID:        stored.ID,
			Type:      stored.Type,
			Content:   content,
			MediaType: stored.MediaType,
			Filename:  stored.Filename,
		}
	}
	return HistoryEntry{Display: entry.Display, PastedContents: pastedContents}
}

func AppendHistory(path string, entry LogEntry) error {
	if entry.Timestamp == 0 {
		entry.Timestamp = time.Now().UnixMilli()
	}
	if entry.PastedContents == nil {
		entry.PastedContents = map[int]StoredPastedContent{}
	}
	if err := platform.EnsureDir(filepath.Dir(path)); err != nil {
		return err
	}
	return withHistoryLock(path, defaultHistoryLockTimeout, func() error {
		return appendHistoryUnlocked(path, entry)
	})
}

func (w *BufferedHistoryWriter) Queue(entry HistoryEntry) {
	if w == nil {
		return
	}
	w.mu.Lock()
	defer w.mu.Unlock()
	w.entries = append(w.entries, entry)
}

func (w *BufferedHistoryWriter) Pending() int {
	if w == nil {
		return 0
	}
	w.mu.Lock()
	defer w.mu.Unlock()
	return len(w.entries)
}

func (w *BufferedHistoryWriter) Flush() (int, error) {
	if w == nil || w.Path == "" {
		return 0, nil
	}
	if err := platform.EnsureDir(filepath.Dir(w.Path)); err != nil {
		return 0, err
	}
	w.mu.Lock()
	pending := append([]HistoryEntry(nil), w.entries...)
	w.entries = nil
	w.mu.Unlock()
	if len(pending) == 0 {
		return 0, nil
	}
	written := 0
	err := withHistoryLock(w.Path, defaultHistoryLockTimeout, func() error {
		for _, entry := range pending {
			logEntry := NewLogEntry(w.Project, w.Session, entry, time.Now())
			if err := storePastedTextFromHistory(entry, logEntry); err != nil {
				return err
			}
			if err := appendHistoryUnlocked(w.Path, logEntry); err != nil {
				return err
			}
			written++
		}
		return nil
	})
	if err != nil {
		w.mu.Lock()
		w.entries = append(pending[written:], w.entries...)
		w.mu.Unlock()
		return written, err
	}
	return written, nil
}

func appendHistoryUnlocked(path string, entry LogEntry) error {
	f, err := os.OpenFile(path, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o600)
	if err != nil {
		return err
	}
	defer f.Close()
	encoded, err := json.Marshal(entry)
	if err != nil {
		return err
	}
	if _, err := f.Write(append(encoded, '\n')); err != nil {
		return err
	}
	return nil
}

func withHistoryLock(path string, timeout time.Duration, fn func() error) error {
	lockPath := path + ".lock"
	if timeout <= 0 {
		timeout = defaultHistoryLockTimeout
	}
	deadline := time.Now().Add(timeout)
	for {
		lock, err := os.OpenFile(lockPath, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0o600)
		if err == nil {
			_, _ = lock.WriteString(strconv.FormatInt(time.Now().UnixNano(), 10))
			_ = lock.Close()
			defer os.Remove(lockPath)
			return fn()
		}
		if !errors.Is(err, os.ErrExist) {
			return err
		}
		removeStaleHistoryLock(lockPath, time.Now())
		if time.Now().After(deadline) {
			return err
		}
		time.Sleep(10 * time.Millisecond)
	}
}

func removeStaleHistoryLock(lockPath string, now time.Time) {
	info, err := os.Stat(lockPath)
	if err != nil {
		return
	}
	if now.Sub(info.ModTime()) > staleHistoryLockAge {
		_ = os.Remove(lockPath)
	}
}

func AddToHistory(path string, project string, sessionID contracts.ID, entry HistoryEntry) (bool, error) {
	if IsEnvTruthy(os.Getenv("CLAUDE_CODE_SKIP_PROMPT_HISTORY")) {
		return false, nil
	}
	logEntry := NewLogEntry(project, sessionID, entry, time.Now())
	if err := storePastedTextFromHistory(entry, logEntry); err != nil {
		return false, err
	}
	return true, AppendHistory(path, logEntry)
}

func storePastedTextFromHistory(entry HistoryEntry, logEntry LogEntry) error {
	for id, stored := range logEntry.PastedContents {
		if stored.ContentHash == "" {
			continue
		}
		if content, ok := entry.PastedContents[id]; ok {
			if err := StorePastedText(stored.ContentHash, content.Content); err != nil {
				return err
			}
		}
	}
	return nil
}

func LoadHistory(path string, project string, currentSession contracts.ID, limit int, resolver PasteResolver) ([]HistoryEntry, error) {
	if limit <= 0 {
		limit = MaxHistoryItems
	}
	entries, err := loadLogEntriesNewestFirst(path)
	if err != nil {
		return nil, err
	}
	current := make([]HistoryEntry, 0, limit)
	other := make([]HistoryEntry, 0, limit)
	for _, entry := range entries {
		if entry.Project != project {
			continue
		}
		if entry.SessionID == currentSession {
			current = append(current, LogEntryToHistoryEntry(entry, resolver))
		} else {
			other = append(other, LogEntryToHistoryEntry(entry, resolver))
		}
		if len(current)+len(other) >= limit {
			break
		}
	}
	out := append([]HistoryEntry{}, current...)
	for _, entry := range other {
		if len(out) >= limit {
			break
		}
		out = append(out, entry)
	}
	return out, nil
}

func LoadTimestampedHistory(path string, project string, limit int, resolver PasteResolver) ([]TimestampedHistoryEntry, error) {
	if limit <= 0 {
		limit = MaxHistoryItems
	}
	entries, err := loadLogEntriesNewestFirst(path)
	if err != nil {
		return nil, err
	}
	seen := map[string]struct{}{}
	out := make([]TimestampedHistoryEntry, 0, limit)
	for _, entry := range entries {
		if entry.Project != project {
			continue
		}
		if _, ok := seen[entry.Display]; ok {
			continue
		}
		seen[entry.Display] = struct{}{}
		out = append(out, TimestampedHistoryEntry{
			Display:   entry.Display,
			Timestamp: entry.Timestamp,
			Entry:     LogEntryToHistoryEntry(entry, resolver),
		})
		if len(out) >= limit {
			break
		}
	}
	return out, nil
}

func WithoutTimestamp(entries []LogEntry, timestamp int64) []LogEntry {
	out := entries[:0]
	for _, entry := range entries {
		if entry.Timestamp == timestamp {
			continue
		}
		out = append(out, entry)
	}
	return out
}

func IsEnvTruthy(value string) bool {
	if value == "" {
		return false
	}
	switch strings.ToLower(strings.TrimSpace(value)) {
	case "1", "true", "yes", "on":
		return true
	default:
		return false
	}
}

func loadLogEntriesNewestFirst(path string) ([]LogEntry, error) {
	f, err := os.Open(path)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, err
	}
	defer f.Close()

	var entries []LogEntry
	scanner := bufio.NewScanner(f)
	buf := make([]byte, 0, 1024*1024)
	scanner.Buffer(buf, 50*1024*1024)
	for scanner.Scan() {
		line := scanner.Bytes()
		if len(line) == 0 {
			continue
		}
		var entry LogEntry
		if err := json.Unmarshal(line, &entry); err != nil {
			continue
		}
		if entry.Project == "" {
			continue
		}
		entries = append(entries, entry)
	}
	if err := scanner.Err(); err != nil {
		return nil, err
	}
	for i, j := 0, len(entries)-1; i < j; i, j = i+1, j-1 {
		entries[i], entries[j] = entries[j], entries[i]
	}
	return entries, nil
}
