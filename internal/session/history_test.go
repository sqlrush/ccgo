package session

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"ccgo/internal/contracts"
)

func TestPastedTextReferences(t *testing.T) {
	if got := PastedTextRefNumLines("one\r\ntwo\nthree\rfour"); got != 3 {
		t.Fatalf("PastedTextRefNumLines = %d", got)
	}
	if got := FormatPastedTextRef(1, 0); got != "[Pasted text #1]" {
		t.Fatalf("FormatPastedTextRef zero = %q", got)
	}
	if got := FormatPastedTextRef(2, 7); got != "[Pasted text #2 +7 lines]" {
		t.Fatalf("FormatPastedTextRef lines = %q", got)
	}
	if got := FormatImageRef(3); got != "[Image #3]" {
		t.Fatalf("FormatImageRef = %q", got)
	}

	input := "a [Pasted text #1 +1 lines] b [Image #2] c [...Truncated text #3.] d [Pasted text #0]"
	refs := ParseReferences(input)
	if len(refs) != 3 {
		t.Fatalf("len(refs) = %d", len(refs))
	}
	if refs[0].ID != 1 || refs[0].Kind != "Pasted text" || refs[1].ID != 2 || refs[2].ID != 3 {
		t.Fatalf("refs = %#v", refs)
	}

	expanded := ExpandPastedTextRefs("x [Pasted text #1] y [Image #2] z [Pasted text #3]", map[int]PastedContent{
		1: {ID: 1, Type: PastedContentText, Content: "TEXT"},
		2: {ID: 2, Type: PastedContentImage, Content: "IMAGE"},
		3: {ID: 3, Type: PastedContentText, Content: "[Pasted text #1]"},
	})
	if expanded != "x TEXT y [Image #2] z [Pasted text #1]" {
		t.Fatalf("expanded = %q", expanded)
	}
}

func TestPrepareStoredPastedContents(t *testing.T) {
	large := strings.Repeat("x", MaxPastedContentLength+1)
	stored := PrepareStoredPastedContents(map[int]PastedContent{
		1: {ID: 1, Type: PastedContentText, Content: "small", MediaType: "text/plain"},
		2: {ID: 2, Type: PastedContentText, Content: large},
		3: {ID: 3, Type: PastedContentImage, Content: "base64", MediaType: "image/png", Filename: "chart.png"},
	})
	if stored[1].Content != "small" || stored[1].ContentHash != "" {
		t.Fatalf("small stored = %#v", stored[1])
	}
	if stored[2].Content != "" || stored[2].ContentHash != HashPastedText(large) {
		t.Fatalf("large stored = %#v", stored[2])
	}
	if _, ok := stored[3]; ok {
		t.Fatalf("image metadata should not be stored in prompt history: %#v", stored[3])
	}

	entry := LogEntryToHistoryEntry(LogEntry{
		Display: "cmd",
		PastedContents: map[int]StoredPastedContent{
			1: stored[1],
			2: stored[2],
			3: {ID: 3, Type: PastedContentImage, MediaType: "image/png", Filename: "chart.png"},
		},
	}, func(hash string) (string, bool) {
		if hash != HashPastedText(large) {
			return "", false
		}
		return large, true
	})
	if entry.PastedContents[1].Content != "small" || entry.PastedContents[2].Content != large {
		t.Fatalf("resolved entry = %#v", entry)
	}
	if entry.PastedContents[3].Type != PastedContentImage || entry.PastedContents[3].Content != "" || entry.PastedContents[3].Filename != "chart.png" {
		t.Fatalf("resolved image entry = %#v", entry.PastedContents[3])
	}
}

func TestAppendAndLoadPromptHistory(t *testing.T) {
	path := filepath.Join(t.TempDir(), "history.jsonl")
	project := "/repo"
	currentSession := contracts.ID("current")
	otherSession := contracts.ID("other")

	entries := []LogEntry{
		{Display: "other-old", PastedContents: map[int]StoredPastedContent{}, Timestamp: 100, Project: project, SessionID: otherSession},
		{Display: "current-old", PastedContents: map[int]StoredPastedContent{}, Timestamp: 200, Project: project, SessionID: currentSession},
		{Display: "other-new", PastedContents: map[int]StoredPastedContent{}, Timestamp: 300, Project: project, SessionID: otherSession},
		{Display: "wrong-project", PastedContents: map[int]StoredPastedContent{}, Timestamp: 400, Project: "/else", SessionID: currentSession},
	}
	for _, entry := range entries {
		if err := AppendHistory(path, entry); err != nil {
			t.Fatal(err)
		}
	}
	f, err := os.OpenFile(path, os.O_WRONLY|os.O_APPEND, 0o600)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := f.WriteString("{malformed\n"); err != nil {
		t.Fatal(err)
	}
	if err := f.Close(); err != nil {
		t.Fatal(err)
	}

	history, err := LoadHistory(path, project, currentSession, MaxHistoryItems, nil)
	if err != nil {
		t.Fatal(err)
	}
	got := displays(history)
	want := []string{"current-old", "other-new", "other-old"}
	if strings.Join(got, ",") != strings.Join(want, ",") {
		t.Fatalf("history = %#v, want %#v", got, want)
	}
}

func TestLoadPromptHistoryAcceptsFieldAliases(t *testing.T) {
	path := filepath.Join(t.TempDir(), "history.jsonl")
	line := `{"display":"run [Pasted text #1] [Image #2]","pasted_contents":{"1":{"id":1,"type":"text","content_hash":"hash_1","media_type":"text/plain"},"2":{"id":2,"type":"image","media_type":"image/png","filename":"chart.png","source_path":"/tmp/chart.png","dimensions":{"original_width":4000,"original_height":2000,"display_width":1000,"display_height":500}}},"timestamp":100,"project":"/repo","session_id":"session"}`
	if err := os.WriteFile(path, []byte(line+"\n"), 0o600); err != nil {
		t.Fatal(err)
	}

	history, err := LoadHistory(path, "/repo", "session", MaxHistoryItems, func(hash string) (string, bool) {
		if hash != "hash_1" {
			return "", false
		}
		return "expanded paste", true
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(history) != 1 || history[0].Display != "run [Pasted text #1] [Image #2]" {
		t.Fatalf("history = %#v", history)
	}
	if got := history[0].PastedContents[1]; got.Content != "expanded paste" || got.MediaType != "text/plain" {
		t.Fatalf("text paste alias = %#v", got)
	}
	if got := history[0].PastedContents[2]; got.Type != PastedContentImage || got.MediaType != "image/png" || got.Filename != "chart.png" || got.SourcePath != "/tmp/chart.png" || got.Dimensions == nil || got.Dimensions.DisplayWidth != 1000 {
		t.Fatalf("image paste alias = %#v", got)
	}
}

func TestLoadTimestampedHistoryDedupesNewestFirst(t *testing.T) {
	path := filepath.Join(t.TempDir(), "history.jsonl")
	project := "/repo"
	for _, entry := range []LogEntry{
		{Display: "same", PastedContents: map[int]StoredPastedContent{}, Timestamp: 100, Project: project},
		{Display: "other", PastedContents: map[int]StoredPastedContent{}, Timestamp: 200, Project: project},
		{Display: "same", PastedContents: map[int]StoredPastedContent{}, Timestamp: 300, Project: project},
	} {
		if err := AppendHistory(path, entry); err != nil {
			t.Fatal(err)
		}
	}
	history, err := LoadTimestampedHistory(path, project, MaxHistoryItems, nil)
	if err != nil {
		t.Fatal(err)
	}
	if len(history) != 2 || history[0].Display != "same" || history[0].Timestamp != 300 || history[1].Display != "other" {
		t.Fatalf("history = %#v", history)
	}
}

func TestAddToHistoryStoresLargePasteAndHonorsSkipEnv(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("CLAUDE_CONFIG_DIR", dir)
	t.Setenv("CLAUDE_CODE_SKIP_PROMPT_HISTORY", "true")
	path := filepath.Join(dir, "history.jsonl")

	written, err := AddToHistory(path, "/repo", "session", HistoryEntry{Display: "skip", PastedContents: map[int]PastedContent{}})
	if err != nil {
		t.Fatal(err)
	}
	if written {
		t.Fatal("history was written while CLAUDE_CODE_SKIP_PROMPT_HISTORY was truthy")
	}
	if _, err := os.Stat(path); !os.IsNotExist(err) {
		t.Fatalf("history file should not exist, stat err = %v", err)
	}

	t.Setenv("CLAUDE_CODE_SKIP_PROMPT_HISTORY", "false")
	large := strings.Repeat("paste", 300)
	written, err = AddToHistory(path, "/repo", "session", HistoryEntry{
		Display: "cmd",
		PastedContents: map[int]PastedContent{
			1: {ID: 1, Type: PastedContentText, Content: large},
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	if !written {
		t.Fatal("history was not written")
	}
	if got, ok := RetrievePastedText(HashPastedText(large)); !ok || got != large {
		t.Fatalf("retrieved paste ok=%v len=%d", ok, len(got))
	}
	history, err := LoadHistory(path, "/repo", "session", MaxHistoryItems, RetrievePastedText)
	if err != nil {
		t.Fatal(err)
	}
	if len(history) != 1 || history[0].PastedContents[1].Content != large {
		t.Fatalf("history = %#v", history)
	}
}

func TestAddToHistorySkipsImagePastedContent(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("CLAUDE_CONFIG_DIR", dir)
	path := filepath.Join(dir, "history.jsonl")

	written, err := AddToHistory(path, "/repo", "session", HistoryEntry{
		Display: "cmd [Image #1]",
		PastedContents: map[int]PastedContent{
			1: {ID: 1, Type: PastedContentImage, Content: "base64", MediaType: "image/png", Filename: "chart.png"},
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	if !written {
		t.Fatal("history was not written")
	}
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	raw := string(data)
	if strings.Contains(raw, `"type":"image"`) || strings.Contains(raw, "chart.png") || strings.Contains(raw, "base64") {
		t.Fatalf("history should not store image pasted content: %s", raw)
	}
	history, err := LoadHistory(path, "/repo", "session", MaxHistoryItems, RetrievePastedText)
	if err != nil {
		t.Fatal(err)
	}
	if len(history) != 1 || history[0].Display != "cmd [Image #1]" || len(history[0].PastedContents) != 0 {
		t.Fatalf("history = %#v", history)
	}
}

func TestCleanupOldPastesRemovesOnlyOldTextFiles(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("CLAUDE_CONFIG_DIR", dir)
	if removed := CleanupOldPastes(time.Now()); removed != 0 {
		t.Fatalf("missing paste-cache removed = %d", removed)
	}

	if err := StorePastedText("oldpaste", "old"); err != nil {
		t.Fatal(err)
	}
	if err := StorePastedText("newpaste", "new"); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(PasteStoreDir(), "keep.bin"), []byte("keep"), 0o600); err != nil {
		t.Fatal(err)
	}
	oldTime := time.Now().Add(-48 * time.Hour)
	if err := os.Chtimes(PastePath("oldpaste"), oldTime, oldTime); err != nil {
		t.Fatal(err)
	}

	removed := CleanupOldPastes(time.Now().Add(-24 * time.Hour))
	if removed != 1 {
		t.Fatalf("removed = %d", removed)
	}
	if _, ok := RetrievePastedText("oldpaste"); ok {
		t.Fatal("old paste was not removed")
	}
	if got, ok := RetrievePastedText("newpaste"); !ok || got != "new" {
		t.Fatalf("new paste ok=%v got=%q", ok, got)
	}
	if _, err := os.Stat(filepath.Join(PasteStoreDir(), "keep.bin")); err != nil {
		t.Fatalf("non txt paste-cache file should remain: %v", err)
	}
}

func TestAppendHistoryUsesStaleLockAndBufferedFlush(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("CLAUDE_CONFIG_DIR", dir)
	path := filepath.Join(dir, "history.jsonl")
	lockPath := path + ".lock"
	if err := os.WriteFile(lockPath, []byte("stale"), 0o600); err != nil {
		t.Fatal(err)
	}
	old := time.Now().Add(-staleHistoryLockAge - time.Second)
	if err := os.Chtimes(lockPath, old, old); err != nil {
		t.Fatal(err)
	}
	if err := AppendHistory(path, LogEntry{Display: "direct", Project: "/repo", PastedContents: map[int]StoredPastedContent{}}); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(lockPath); !os.IsNotExist(err) {
		t.Fatalf("history lock should be removed, err=%v", err)
	}

	large := strings.Repeat("queued", 300)
	writer := &BufferedHistoryWriter{Path: path, Project: "/repo", Session: "sess"}
	writer.Queue(HistoryEntry{Display: "queued", PastedContents: map[int]PastedContent{
		1: {ID: 1, Type: PastedContentText, Content: large},
	}})
	if writer.Pending() != 1 {
		t.Fatalf("pending = %d", writer.Pending())
	}
	written, err := writer.Flush()
	if err != nil {
		t.Fatal(err)
	}
	if written != 1 || writer.Pending() != 0 {
		t.Fatalf("flush written=%d pending=%d", written, writer.Pending())
	}
	history, err := LoadHistory(path, "/repo", "sess", MaxHistoryItems, RetrievePastedText)
	if err != nil {
		t.Fatal(err)
	}
	if got := displays(history); strings.Join(got, ",") != "queued,direct" {
		t.Fatalf("history = %#v", got)
	}
	if history[0].PastedContents[1].Content != large {
		t.Fatalf("queued paste = %#v", history[0].PastedContents)
	}
}

func TestBufferedHistoryWriterRemovesLastPendingEntry(t *testing.T) {
	path := filepath.Join(t.TempDir(), "history.jsonl")
	writer := &BufferedHistoryWriter{Path: path, Project: "/repo", Session: "sess"}
	if writer.RemoveLastPending() {
		t.Fatal("empty writer removed an entry")
	}
	writer.Queue(HistoryEntry{Display: "keep", PastedContents: map[int]PastedContent{}})
	writer.Queue(HistoryEntry{Display: "drop", PastedContents: map[int]PastedContent{}})
	if !writer.RemoveLastPending() || writer.Pending() != 1 {
		t.Fatalf("remove pending failed, pending=%d", writer.Pending())
	}
	written, err := writer.Flush()
	if err != nil {
		t.Fatal(err)
	}
	if written != 1 {
		t.Fatalf("written = %d", written)
	}
	history, err := LoadHistory(path, "/repo", "sess", MaxHistoryItems, nil)
	if err != nil {
		t.Fatal(err)
	}
	if got := displays(history); strings.Join(got, ",") != "keep" {
		t.Fatalf("history = %#v", got)
	}
}

func TestBufferedHistoryWriterRemoveLastSkipsPendingEntry(t *testing.T) {
	path := filepath.Join(t.TempDir(), "history.jsonl")
	writer := &BufferedHistoryWriter{Path: path, Project: "/repo", Session: "sess"}

	writer.Queue(HistoryEntry{Display: "keep", PastedContents: map[int]PastedContent{}})
	writer.Queue(HistoryEntry{Display: "drop", PastedContents: map[int]PastedContent{}})
	if !writer.RemoveLast() || writer.Pending() != 1 {
		t.Fatalf("remove last pending failed, pending=%d", writer.Pending())
	}
	history, err := writer.LoadHistory(MaxHistoryItems, nil)
	if err != nil {
		t.Fatal(err)
	}
	if got := displays(history); strings.Join(got, ",") != "keep" {
		t.Fatalf("history = %#v", got)
	}
}

func TestBufferedHistoryWriterRemoveLastSkipsFlushedEntry(t *testing.T) {
	path := filepath.Join(t.TempDir(), "history.jsonl")
	writer := &BufferedHistoryWriter{Path: path, Project: "/repo", Session: "sess"}

	writer.Queue(HistoryEntry{Display: "drop", PastedContents: map[int]PastedContent{}})
	written, err := writer.Flush()
	if err != nil {
		t.Fatal(err)
	}
	if written != 1 {
		t.Fatalf("written = %d", written)
	}
	if !writer.RemoveLast() {
		t.Fatal("flushed entry was not marked skipped")
	}
	history, err := writer.LoadHistory(MaxHistoryItems, nil)
	if err != nil {
		t.Fatal(err)
	}
	if len(history) != 0 {
		t.Fatalf("writer history = %#v", displays(history))
	}

	history, err = LoadHistory(path, "/repo", "sess", MaxHistoryItems, nil)
	if err != nil {
		t.Fatal(err)
	}
	if got := displays(history); strings.Join(got, ",") != "drop" {
		t.Fatalf("plain history = %#v", got)
	}
}

func TestBufferedHistoryWriterRemoveLastSkipsTimestampedHistory(t *testing.T) {
	path := filepath.Join(t.TempDir(), "history.jsonl")
	writer := &BufferedHistoryWriter{Path: path, Project: "/repo", Session: "sess"}

	if err := AppendHistory(path, LogEntry{Display: "same", PastedContents: map[int]StoredPastedContent{}, Timestamp: 100, Project: "/repo", SessionID: "sess"}); err != nil {
		t.Fatal(err)
	}
	writer.Queue(HistoryEntry{Display: "same", PastedContents: map[int]PastedContent{}})
	if _, err := writer.Flush(); err != nil {
		t.Fatal(err)
	}
	if !writer.RemoveLast() {
		t.Fatal("flushed entry was not marked skipped")
	}
	history, err := writer.LoadTimestampedHistory(MaxHistoryItems, nil)
	if err != nil {
		t.Fatal(err)
	}
	if len(history) != 1 || history[0].Display != "same" || history[0].Timestamp != 100 {
		t.Fatalf("timestamped history = %#v", history)
	}
}

func TestNewLogEntryUsesUnixMillis(t *testing.T) {
	now := time.Unix(42, 123_000_000)
	entry := NewLogEntry("/repo", "session", HistoryEntry{Display: "cmd", PastedContents: map[int]PastedContent{}}, now)
	if entry.Timestamp != 42123 {
		t.Fatalf("timestamp = %d", entry.Timestamp)
	}
}

func displays(entries []HistoryEntry) []string {
	out := make([]string, 0, len(entries))
	for _, entry := range entries {
		out = append(out, entry.Display)
	}
	return out
}
