package session

import (
	"encoding/base64"
	"encoding/json"
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

	aliasInput := "a [pasted text #4] b [Pasted image #5] c [input-image #6] d [...truncated text #7.] e [input_text #0]"
	aliasRefs := ParseReferences(aliasInput)
	if len(aliasRefs) != 4 || aliasRefs[0].ID != 4 || aliasRefs[1].ID != 5 || aliasRefs[2].ID != 6 || aliasRefs[3].ID != 7 {
		t.Fatalf("alias refs = %#v", aliasRefs)
	}

	expanded := ExpandPastedTextRefs("x [Pasted text #1] y [Image #2] z [Pasted text #3] w [input_text #4]", map[int]PastedContent{
		1: {ID: 1, Type: PastedContentText, Content: "TEXT"},
		2: {ID: 2, Type: PastedContentImage, Content: "IMAGE"},
		3: {ID: 3, Type: PastedContentText, Content: "[Pasted text #1]"},
		4: {ID: 4, Type: PastedContentText, Content: "ALIAS"},
	})
	if expanded != "x TEXT y [Image #2] z [Pasted text #1] w ALIAS" {
		t.Fatalf("expanded = %q", expanded)
	}
}

func TestPrepareStoredPastedContents(t *testing.T) {
	large := strings.Repeat("x", MaxPastedContentLength+1)
	stored := PrepareStoredPastedContents(map[int]PastedContent{
		1: {ID: 1, Type: PastedContentText, Content: "small", MediaType: "text/plain"},
		2: {ID: 2, Type: PastedContentText, Content: large},
		3: {
			ID:         3,
			Type:       PastedContentImage,
			Content:    "base64",
			MediaType:  "image/png",
			Filename:   "chart.png",
			SourcePath: "/tmp/image-cache/session/3.png",
			Dimensions: &ImageDimensions{OriginalWidth: 4000, OriginalHeight: 2000, DisplayWidth: 1000, DisplayHeight: 500},
		},
	})
	if stored[1].Content != "small" || stored[1].ContentHash != "" {
		t.Fatalf("small stored = %#v", stored[1])
	}
	if stored[2].Content != "" || stored[2].ContentHash != HashPastedText(large) {
		t.Fatalf("large stored = %#v", stored[2])
	}
	if stored[3].Content != "" || stored[3].ContentHash != "" || stored[3].Type != PastedContentImage || stored[3].MediaType != "image/png" || stored[3].Filename != "chart.png" || stored[3].SourcePath != "/tmp/image-cache/session/3.png" || stored[3].Dimensions == nil || stored[3].Dimensions.DisplayWidth != 1000 {
		t.Fatalf("image metadata stored = %#v", stored[3])
	}

	entry := LogEntryToHistoryEntry(LogEntry{
		Display: "cmd",
		PastedContents: map[int]StoredPastedContent{
			1: stored[1],
			2: stored[2],
			3: stored[3],
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
	if entry.PastedContents[3].Type != PastedContentImage || entry.PastedContents[3].Content != "" || entry.PastedContents[3].Filename != "chart.png" || entry.PastedContents[3].SourcePath != "/tmp/image-cache/session/3.png" || entry.PastedContents[3].Dimensions == nil || entry.PastedContents[3].Dimensions.DisplayHeight != 500 {
		t.Fatalf("resolved image entry = %#v", entry.PastedContents[3])
	}
}

func TestLogEntryToHistoryEntryRestoresCachedImageContent(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("CLAUDE_CONFIG_DIR", dir)
	ClearStoredImagePaths()
	defer ClearStoredImagePaths()

	sessionID := contracts.ID("session-cache")
	encoded := base64.StdEncoding.EncodeToString([]byte("cached image"))
	storedPath, ok := StoreImage(sessionID, PastedContent{
		ID:        30,
		Type:      PastedContentImage,
		Content:   encoded,
		MediaType: "image/png",
		Filename:  "cached.png",
	})
	if !ok {
		t.Fatal("store image failed")
	}
	unsafePath := filepath.Join(t.TempDir(), "31.png")
	if err := os.WriteFile(unsafePath, []byte("outside cache"), 0o600); err != nil {
		t.Fatal(err)
	}

	entry := LogEntryToHistoryEntry(LogEntry{
		Display:   "look [Image #30] [Image #31]",
		SessionID: sessionID,
		PastedContents: map[int]StoredPastedContent{
			30: {ID: 30, Type: PastedContentImage, MediaType: "image/png", Filename: "cached.png"},
			31: {ID: 31, Type: PastedContentImage, MediaType: "image/png", Filename: "outside.png", SourcePath: unsafePath},
		},
	}, nil)
	cached := entry.PastedContents[30]
	if cached.Content != encoded || cached.Filename != "cached.png" {
		t.Fatalf("cached image entry = %#v", cached)
	}
	requireSameFile(t, cached.SourcePath, storedPath)
	if outside := entry.PastedContents[31]; outside.Content != "" || outside.SourcePath != unsafePath {
		t.Fatalf("outside image entry = %#v", outside)
	}
	messages := PromptMessages(entry.Display, entry.PastedContents)
	if len(messages) == 0 || len(messages[0].Content) != 2 || messages[0].Content[1].Type != contracts.ContentImage {
		t.Fatalf("prompt messages = %#v", messages)
	}
	source, ok := messages[0].Content[1].Source.(contracts.ImageSource)
	if !ok || source.MediaType != "image/png" || source.Data != encoded {
		t.Fatalf("image source = %#v", messages[0].Content[1].Source)
	}
}

func TestLogEntryToHistoryEntryRestoresCachedImageWithoutMediaType(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("CLAUDE_CONFIG_DIR", dir)
	ClearStoredImagePaths()
	defer ClearStoredImagePaths()

	sessionID := contracts.ID("session-cache-missing-type")
	encoded := base64.StdEncoding.EncodeToString([]byte("cached webp"))
	storedPath, ok := StoreImage(sessionID, PastedContent{
		ID:        32,
		Type:      PastedContentImage,
		Content:   encoded,
		MediaType: "image/webp",
		Filename:  "diagram.webp",
	})
	if !ok {
		t.Fatal("store image failed")
	}

	entry := LogEntryToHistoryEntry(LogEntry{
		Display:   "look [Image #32]",
		SessionID: sessionID,
		PastedContents: map[int]StoredPastedContent{
			32: {ID: 32, Type: PastedContentImage, Filename: "diagram.webp"},
		},
	}, nil)
	image := entry.PastedContents[32]
	if image.Content != encoded || image.MediaType != "image/webp" {
		t.Fatalf("cached image without media type = %#v", image)
	}
	requireSameFile(t, image.SourcePath, storedPath)
	messages := PromptMessages(entry.Display, entry.PastedContents)
	if len(messages) == 0 || len(messages[0].Content) != 2 || messages[0].Content[1].Type != contracts.ContentImage {
		t.Fatalf("prompt messages = %#v", messages)
	}
	source, ok := messages[0].Content[1].Source.(contracts.ImageSource)
	if !ok || source.MediaType != "image/webp" || source.Data != encoded {
		t.Fatalf("image source = %#v", messages[0].Content[1].Source)
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
	line := `{"display":"run [Pasted text #1] [Image #2] [Image #3]","pasted_contents":{"1":{"id":1,"type":"text","content_hash":"hash_1","contentType":"text/plain"},"2":{"id":2,"type":"image","media_type":"image/png","filename":"chart.png","source_path":"/tmp/chart.png","dimensions":{"original_width":4000,"original_height":2000,"display_width":1000,"display_height":500}},"3":{"id":3,"type":"image","mime_type":"image/jpeg","fileName":"photo.jpg","file_path":"/tmp/photo.jpg","dimensions":{"width":3000,"height":1500}}},"timestamp":100,"project":"/repo","sessionID":"session"}`
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
	if len(history) != 1 || history[0].Display != "run [Pasted text #1] [Image #2] [Image #3]" {
		t.Fatalf("history = %#v", history)
	}
	if got := history[0].PastedContents[1]; got.Content != "expanded paste" || got.MediaType != "text/plain" {
		t.Fatalf("text paste alias = %#v", got)
	}
	if got := history[0].PastedContents[2]; got.Type != PastedContentImage || got.MediaType != "image/png" || got.Filename != "chart.png" || got.SourcePath != "/tmp/chart.png" || got.Dimensions == nil || got.Dimensions.DisplayWidth != 1000 {
		t.Fatalf("image paste alias = %#v", got)
	}
	if got := history[0].PastedContents[3]; got.Type != PastedContentImage || got.MediaType != "image/jpeg" || got.Filename != "photo.jpg" || got.SourcePath != "/tmp/photo.jpg" || got.Dimensions == nil || got.Dimensions.OriginalWidth != 3000 {
		t.Fatalf("image paste alternate aliases = %#v", got)
	}
}

func TestLoadPromptHistoryAcceptsProjectAndTimestampAliases(t *testing.T) {
	path := filepath.Join(t.TempDir(), "history.jsonl")
	lines := []string{
		`{"display":"project path","pasted_contents":{},"createdAt":"1970-01-01T00:00:01Z","projectPath":"/repo","sessionUUID":"session"}`,
		`{"display":"cwd path","pasted_contents":{},"unixTimestamp":"2000","cwd":"/repo","sessionID":"other"}`,
		`{"display":"wrong project","pasted_contents":{},"timestamp":3000,"workingDirectory":"/else","sessionID":"session"}`,
	}
	if err := os.WriteFile(path, []byte(strings.Join(lines, "\n")+"\n"), 0o600); err != nil {
		t.Fatal(err)
	}

	history, err := LoadHistory(path, "/repo", "session", MaxHistoryItems, nil)
	if err != nil {
		t.Fatal(err)
	}
	if got := displays(history); strings.Join(got, ",") != "project path,cwd path" {
		t.Fatalf("history = %#v", got)
	}

	timestamped, err := LoadTimestampedHistory(path, "/repo", MaxHistoryItems, nil)
	if err != nil {
		t.Fatal(err)
	}
	if len(timestamped) != 2 || timestamped[0].Display != "cwd path" || timestamped[0].Timestamp != 2000 || timestamped[1].Display != "project path" || timestamped[1].Timestamp != 1000 {
		t.Fatalf("timestamped history = %#v", timestamped)
	}
}

func TestHistoryEntryAcceptsDisplayFieldAliases(t *testing.T) {
	var entry HistoryEntry
	if err := json.Unmarshal([]byte(`{"prompt":"run [Pasted text #1]","pastedContents":[{"id":1,"type":"text","content":"memo"}]}`), &entry); err != nil {
		t.Fatal(err)
	}
	if entry.Display != "run [Pasted text #1]" || entry.PastedContents[1].Content != "memo" {
		t.Fatalf("history entry = %#v", entry)
	}
}

func TestLoadPromptHistoryAcceptsDisplayFieldAliases(t *testing.T) {
	path := filepath.Join(t.TempDir(), "history.jsonl")
	lines := []string{
		`{"text":"text alias","pasted_contents":{},"timestamp":100,"project":"/repo","sessionID":"session"}`,
		`{"input":"input alias","pasted_contents":{},"timestamp":200,"project":"/repo","sessionID":"other"}`,
	}
	if err := os.WriteFile(path, []byte(strings.Join(lines, "\n")+"\n"), 0o600); err != nil {
		t.Fatal(err)
	}

	history, err := LoadHistory(path, "/repo", "session", MaxHistoryItems, nil)
	if err != nil {
		t.Fatal(err)
	}
	if got := displays(history); strings.Join(got, ",") != "text alias,input alias" {
		t.Fatalf("history = %#v", got)
	}
}

func TestHistoryEntryAcceptsPastedContentFieldAliases(t *testing.T) {
	var entry HistoryEntry
	err := json.Unmarshal([]byte(`{"display":"restore [Image #1] [Pasted text #2]","pasted_contents":{"1":{"pastedContentId":"1","kind":"inputImage","base64":"AAAA","mimeType":"image/png","name":"chart.png","path":"/tmp/chart.png","dimensions":{"width":4000,"height":2000}},"2":{"attachmentID":"2","pastedType":"pasted-text","value":"memo","contentType":"text/plain"}}}`), &entry)
	if err != nil {
		t.Fatal(err)
	}
	got := entry.PastedContents[1]
	if got.ID != 1 || got.Type != PastedContentImage || got.Content != "AAAA" || got.MediaType != "image/png" || got.Filename != "chart.png" || got.SourcePath != "/tmp/chart.png" || got.Dimensions == nil || got.Dimensions.OriginalHeight != 2000 || got.Dimensions.DisplayHeight != 2000 {
		t.Fatalf("pasted content aliases = %#v", got)
	}
	text := entry.PastedContents[2]
	if text.ID != 2 || text.Type != PastedContentText || text.Content != "memo" || text.MediaType != "text/plain" {
		t.Fatalf("text pasted content aliases = %#v", text)
	}
}

func TestHistoryEntryAcceptsPastedContentBodyAndBase64DataAliases(t *testing.T) {
	var entry HistoryEntry
	data := `{
		"display": "restore [Pasted text #3] [Image #4] [Pasted text #5]",
		"pasted_contents": {
			"3": {"pastedContentId": "3", "kind": "text", "text": "text memo"},
			"4": {"imageID": "4", "type": "image", "base64Data": "BBBB", "mimeType": "image/png"},
			"5": {"attachmentID": "5", "pastedType": "input_text", "body": "body memo"}
		}
	}`
	if err := json.Unmarshal([]byte(data), &entry); err != nil {
		t.Fatal(err)
	}
	if got := entry.PastedContents[3]; got.ID != 3 || got.Type != PastedContentText || got.Content != "text memo" {
		t.Fatalf("text alias = %#v", got)
	}
	if got := entry.PastedContents[4]; got.ID != 4 || got.Type != PastedContentImage || got.Content != "BBBB" || got.MediaType != "image/png" {
		t.Fatalf("base64Data alias = %#v", got)
	}
	if got := entry.PastedContents[5]; got.ID != 5 || got.Type != PastedContentText || got.Content != "body memo" {
		t.Fatalf("body alias = %#v", got)
	}
}

func TestHistoryEntryAcceptsImageSourceObjectAliases(t *testing.T) {
	var entry HistoryEntry
	data := `{
		"display": "restore [Image #21] [Image #22] [Image #24] [Image #25]",
		"pastedContents": [
			{
				"imageID": "21",
				"type": "input_image",
				"fileName": "source.png",
				"source": {"type":"base64","media_type":"image/png","data":"AAAA"}
			},
			{
				"imageID": "22",
				"kind": "pasted-image",
				"imageSource": {"type":"url","url":"file:///tmp/photo.jpg","mimeType":"image/jpeg"}
			},
			{
				"imageID": "24",
				"kind": "pasted-image",
				"dataUrl": "data:image/gif;base64,CCCC"
			},
			{
				"imageID": "25",
				"kind": "pasted-image",
				"source": "data:image/png;base64,DDDD"
			}
		]
	}`
	if err := json.Unmarshal([]byte(data), &entry); err != nil {
		t.Fatal(err)
	}
	if got := entry.PastedContents[21]; got.ID != 21 || got.Type != PastedContentImage || got.Content != "AAAA" || got.MediaType != "image/png" || got.Filename != "source.png" {
		t.Fatalf("source object image = %#v", got)
	}
	if got := entry.PastedContents[22]; got.ID != 22 || got.Type != PastedContentImage || got.Content != "" || got.MediaType != "image/jpeg" || got.SourcePath != "file:///tmp/photo.jpg" {
		t.Fatalf("source URL image = %#v", got)
	}
	if got := entry.PastedContents[24]; got.ID != 24 || got.Type != PastedContentImage || got.Content != "CCCC" || got.MediaType != "image/gif" || got.SourcePath != "" {
		t.Fatalf("data URL image = %#v", got)
	}
	if got := entry.PastedContents[25]; got.ID != 25 || got.Type != PastedContentImage || got.Content != "DDDD" || got.MediaType != "image/png" || got.SourcePath != "" {
		t.Fatalf("source data URL image = %#v", got)
	}
}

func TestHistoryEntryAcceptsPastedContentsArrayAndSingleObject(t *testing.T) {
	var entry HistoryEntry
	if err := json.Unmarshal([]byte(`{"display":"restore","pastedContents":[{"pastedContentId":"4","kind":"text","value":"array memo"},{"imageID":"5","type":"image","base64":"AAAA","mimeType":"image/png"}]}`), &entry); err != nil {
		t.Fatal(err)
	}
	if got := entry.PastedContents[4]; got.ID != 4 || got.Type != PastedContentText || got.Content != "array memo" {
		t.Fatalf("array text = %#v", got)
	}
	if got := entry.PastedContents[5]; got.ID != 5 || got.Type != PastedContentImage || got.Content != "AAAA" || got.MediaType != "image/png" {
		t.Fatalf("array image = %#v", got)
	}

	var single HistoryEntry
	if err := json.Unmarshal([]byte(`{"display":"single","pasted_contents":{"attachmentID":"6","pastedType":"pasted-text","value":"single memo"}}`), &single); err != nil {
		t.Fatal(err)
	}
	if got := single.PastedContents[6]; got.ID != 6 || got.Type != PastedContentText || got.Content != "single memo" {
		t.Fatalf("single pasted content = %#v", got)
	}
}

func TestHistoryEntryAcceptsPastedContentContainerAliases(t *testing.T) {
	var entry HistoryEntry
	if err := json.Unmarshal([]byte(`{"display":"restore","attachments":[{"attachmentID":"9","kind":"text","value":"attached memo"},{"imageID":"10","type":"image","base64":"BBBB","mimeType":"image/png"}]}`), &entry); err != nil {
		t.Fatal(err)
	}
	if got := entry.PastedContents[9]; got.ID != 9 || got.Type != PastedContentText || got.Content != "attached memo" {
		t.Fatalf("attachment text = %#v", got)
	}
	if got := entry.PastedContents[10]; got.ID != 10 || got.Type != PastedContentImage || got.Content != "BBBB" || got.MediaType != "image/png" {
		t.Fatalf("attachment image = %#v", got)
	}
}

func TestHistoryEntryAcceptsWrapperObjects(t *testing.T) {
	var entry HistoryEntry
	data := `{
		"historyEntry": {
			"prompt": "restore [Pasted text #12] [Image #13]",
			"attachments": [
				{"record": {"attachmentID": "12", "kind": "input_text", "value": "wrapped memo"}},
				{"payload": {"imageID": "13", "type": "pasted-image", "data": "CCCC", "mimeType": "image/png", "name": "wrapped.png"}}
			]
		}
	}`
	if err := json.Unmarshal([]byte(data), &entry); err != nil {
		t.Fatal(err)
	}
	if entry.Display != "restore [Pasted text #12] [Image #13]" {
		t.Fatalf("wrapped display = %#v", entry)
	}
	if got := entry.PastedContents[12]; got.ID != 12 || got.Type != PastedContentText || got.Content != "wrapped memo" {
		t.Fatalf("wrapped text = %#v", got)
	}
	if got := entry.PastedContents[13]; got.ID != 13 || got.Type != PastedContentImage || got.Content != "CCCC" || got.MediaType != "image/png" || got.Filename != "wrapped.png" {
		t.Fatalf("wrapped image = %#v", got)
	}
}

func TestHistoryEntryAcceptsGraphQLAndResourceWrappers(t *testing.T) {
	var entry HistoryEntry
	data := `{
		"edge": {
			"node": {
				"resource": {
					"attributes": {
						"prompt": "restore [Pasted text #14] [Image #15]",
						"attachments": [
							{"edge": {"node": {"attributes": {"attachmentID": "14", "kind": "input_text", "value": "edge memo"}}}},
							{"resource": {"properties": {"imageID": "15", "type": "pasted-image", "data": "DDDD", "mimeType": "image/png", "name": "edge.png"}}}
						]
					}
				}
			}
		}
	}`
	if err := json.Unmarshal([]byte(data), &entry); err != nil {
		t.Fatal(err)
	}
	if entry.Display != "restore [Pasted text #14] [Image #15]" {
		t.Fatalf("wrapped display = %#v", entry)
	}
	if got := entry.PastedContents[14]; got.ID != 14 || got.Type != PastedContentText || got.Content != "edge memo" {
		t.Fatalf("edge text = %#v", got)
	}
	if got := entry.PastedContents[15]; got.ID != 15 || got.Type != PastedContentImage || got.Content != "DDDD" || got.MediaType != "image/png" || got.Filename != "edge.png" {
		t.Fatalf("edge image = %#v", got)
	}
}

func TestImageDimensionsWidthHeightDefaultDisplaySize(t *testing.T) {
	var dimensions ImageDimensions
	if err := json.Unmarshal([]byte(`{"width":4000,"height":2000}`), &dimensions); err != nil {
		t.Fatal(err)
	}
	if dimensions.OriginalWidth != 4000 || dimensions.OriginalHeight != 2000 || dimensions.DisplayWidth != 4000 || dimensions.DisplayHeight != 2000 {
		t.Fatalf("dimensions = %#v", dimensions)
	}
	if got := ImageMetadataText(&dimensions, "/tmp/chart.png"); got != "[Image: source: /tmp/chart.png]" {
		t.Fatalf("metadata = %q", got)
	}
}

func TestStoredPastedContentAcceptsTypeAliases(t *testing.T) {
	var entry LogEntry
	if err := json.Unmarshal([]byte(`{"display":"restore","pasted_contents":{"1":{"pastedId":"1","kind":"pasted-image","content_hash":"image_hash","mimeType":"image/png"},"2":{"contentID":"2","pasted_type":"input_text","content_hash":"text_hash","contentType":"text/plain"}}}`), &entry); err != nil {
		t.Fatal(err)
	}
	if got := entry.PastedContents[1]; got.ID != 1 || got.Type != PastedContentImage || got.ContentHash != "image_hash" || got.MediaType != "image/png" {
		t.Fatalf("stored image = %#v", got)
	}
	if got := entry.PastedContents[2]; got.ID != 2 || got.Type != PastedContentText || got.ContentHash != "text_hash" || got.MediaType != "text/plain" {
		t.Fatalf("stored text = %#v", got)
	}
}

func TestStoredPastedContentAcceptsHashAndInlineContentAliases(t *testing.T) {
	var entry LogEntry
	data := `{
		"display": "restore",
		"pasted_contents": {
			"3": {"contentID": "3", "pasted_type": "input_text", "digest": "digest_hash", "contentType": "text/plain"},
			"4": {"contentID": "4", "pasted_type": "input_text", "sha256": "sha_hash", "contentType": "text/plain"},
			"5": {"contentID": "5", "pasted_type": "input_text", "body": "inline memo", "contentType": "text/plain"}
		}
	}`
	if err := json.Unmarshal([]byte(data), &entry); err != nil {
		t.Fatal(err)
	}
	if got := entry.PastedContents[3]; got.ID != 3 || got.Type != PastedContentText || got.ContentHash != "digest_hash" || got.MediaType != "text/plain" {
		t.Fatalf("digest hash = %#v", got)
	}
	if got := entry.PastedContents[4]; got.ID != 4 || got.Type != PastedContentText || got.ContentHash != "sha_hash" || got.MediaType != "text/plain" {
		t.Fatalf("sha256 hash = %#v", got)
	}
	if got := entry.PastedContents[5]; got.ID != 5 || got.Type != PastedContentText || got.Content != "inline memo" || got.MediaType != "text/plain" {
		t.Fatalf("inline body = %#v", got)
	}
}

func TestLoadHistoryAcceptsStoredPastedContentsArray(t *testing.T) {
	path := filepath.Join(t.TempDir(), "history.jsonl")
	line := `{"display":"restore [Pasted text #7] [Image #8]","pasted_contents":[{"contentID":"7","pasted_type":"input_text","content_hash":"text_hash","contentType":"text/plain"},{"imageID":"8","kind":"pasted-image","content_hash":"image_hash","mimeType":"image/png","fileName":"array.png"}],"timestamp":100,"project":"/repo","sessionID":"session"}`
	if err := os.WriteFile(path, []byte(line+"\n"), 0o600); err != nil {
		t.Fatal(err)
	}

	history, err := LoadHistory(path, "/repo", "session", MaxHistoryItems, func(hash string) (string, bool) {
		if hash == "text_hash" {
			return "expanded array memo", true
		}
		return "", false
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(history) != 1 {
		t.Fatalf("history = %#v", history)
	}
	if got := history[0].PastedContents[7]; got.ID != 7 || got.Type != PastedContentText || got.Content != "expanded array memo" || got.MediaType != "text/plain" {
		t.Fatalf("stored array text = %#v", got)
	}
	if got := history[0].PastedContents[8]; got.ID != 8 || got.Type != PastedContentImage || got.Content != "" || got.MediaType != "image/png" || got.Filename != "array.png" {
		t.Fatalf("stored array image = %#v", got)
	}
}

func TestLoadHistoryPreservesStoredImageSourceContent(t *testing.T) {
	path := filepath.Join(t.TempDir(), "history.jsonl")
	line := `{"display":"look [Image #23]","pasted_contents":{"23":{"imageID":"23","kind":"pasted-image","fileName":"diagram.webp","source":{"type":"base64","dataUrl":"data:image/webp;base64,BBBB","sourceUrl":"file:///tmp/diagram.webp"}}},"timestamp":100,"project":"/repo","sessionID":"session"}`
	if err := os.WriteFile(path, []byte(line+"\n"), 0o600); err != nil {
		t.Fatal(err)
	}

	history, err := LoadHistory(path, "/repo", "session", MaxHistoryItems, nil)
	if err != nil {
		t.Fatal(err)
	}
	if len(history) != 1 {
		t.Fatalf("history = %#v", history)
	}
	got := history[0].PastedContents[23]
	if got.ID != 23 || got.Type != PastedContentImage || got.Content != "BBBB" || got.MediaType != "image/webp" || got.Filename != "diagram.webp" || got.SourcePath != "file:///tmp/diagram.webp" {
		t.Fatalf("stored image source = %#v", got)
	}
	messages := PromptMessages(history[0].Display, history[0].PastedContents)
	if len(messages) == 0 || len(messages[0].Content) != 2 || messages[0].Content[1].Type != contracts.ContentImage {
		t.Fatalf("prompt messages = %#v", messages)
	}
	source, ok := messages[0].Content[1].Source.(contracts.ImageSource)
	if !ok || source.MediaType != "image/webp" || source.Data != "BBBB" {
		t.Fatalf("image source = %#v", messages[0].Content[1].Source)
	}
}

func TestLoadHistoryAcceptsGraphQLWrappedLogEntries(t *testing.T) {
	path := filepath.Join(t.TempDir(), "history.jsonl")
	line := `{"edge":{"node":{"properties":{"text":"restore [Pasted text #16]","attachments":[{"edge":{"node":{"properties":{"contentID":"16","pasted_type":"input_text","content_hash":"graph_hash","contentType":"text/plain"}}}}]}}},"timestamp":100,"projectPath":"/repo","sessionID":"session"}`
	if err := os.WriteFile(path, []byte(line+"\n"), 0o600); err != nil {
		t.Fatal(err)
	}

	history, err := LoadHistory(path, "/repo", "session", MaxHistoryItems, func(hash string) (string, bool) {
		if hash == "graph_hash" {
			return "expanded graph memo", true
		}
		return "", false
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(history) != 1 || history[0].Display != "restore [Pasted text #16]" {
		t.Fatalf("history = %#v", history)
	}
	if got := history[0].PastedContents[16]; got.ID != 16 || got.Type != PastedContentText || got.Content != "expanded graph memo" || got.MediaType != "text/plain" {
		t.Fatalf("graph stored text = %#v", got)
	}
}

func TestLoadHistoryAcceptsStoredPastedContentContainerAliases(t *testing.T) {
	path := filepath.Join(t.TempDir(), "history.jsonl")
	line := `{"display":"restore [Pasted text #11]","pasteContent":{"contentID":"11","pasted_type":"input_text","content_hash":"container_hash","contentType":"text/plain"},"timestamp":100,"project":"/repo","sessionID":"session"}`
	if err := os.WriteFile(path, []byte(line+"\n"), 0o600); err != nil {
		t.Fatal(err)
	}

	history, err := LoadHistory(path, "/repo", "session", MaxHistoryItems, func(hash string) (string, bool) {
		if hash == "container_hash" {
			return "expanded container memo", true
		}
		return "", false
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(history) != 1 {
		t.Fatalf("history = %#v", history)
	}
	if got := history[0].PastedContents[11]; got.ID != 11 || got.Type != PastedContentText || got.Content != "expanded container memo" || got.MediaType != "text/plain" {
		t.Fatalf("stored container text = %#v", got)
	}
}

func TestLoadHistoryAcceptsStoredPastedContentHashAliases(t *testing.T) {
	path := filepath.Join(t.TempDir(), "history.jsonl")
	line := `{"display":"restore [Pasted text #16] [Pasted text #17]","pasted_contents":{"16":{"contentID":"16","pasted_type":"input_text","digest":"digest_hash","contentType":"text/plain"},"17":{"contentID":"17","pasted_type":"input_text","checksum":"checksum_hash","contentType":"text/plain"}},"timestamp":100,"project":"/repo","sessionID":"session"}`
	if err := os.WriteFile(path, []byte(line+"\n"), 0o600); err != nil {
		t.Fatal(err)
	}

	history, err := LoadHistory(path, "/repo", "session", MaxHistoryItems, func(hash string) (string, bool) {
		switch hash {
		case "digest_hash":
			return "expanded digest memo", true
		case "checksum_hash":
			return "expanded checksum memo", true
		default:
			return "", false
		}
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(history) != 1 {
		t.Fatalf("history = %#v", history)
	}
	if got := history[0].PastedContents[16]; got.ID != 16 || got.Type != PastedContentText || got.Content != "expanded digest memo" || got.MediaType != "text/plain" {
		t.Fatalf("digest alias = %#v", got)
	}
	if got := history[0].PastedContents[17]; got.ID != 17 || got.Type != PastedContentText || got.Content != "expanded checksum memo" || got.MediaType != "text/plain" {
		t.Fatalf("checksum alias = %#v", got)
	}
}

func TestLoadHistoryAcceptsWrappedEntriesAndPastedContentItems(t *testing.T) {
	path := filepath.Join(t.TempDir(), "history.jsonl")
	line := `{"payload":{"input":"restore [Pasted text #14] [Image #15]","attachments":[{"entry":{"contentID":"14","pasted_type":"input_text","contentDigest":"wrapped_hash","contentType":"text/plain"}},{"item":{"imageID":"15","kind":"input_image","hash":"image_hash","mimeType":"image/png","fileName":"wrapped-history.png"}}]},"createdAt":"1970-01-01T00:00:01Z","workspacePath":"/repo","session":"session"}`
	if err := os.WriteFile(path, []byte(line+"\n"), 0o600); err != nil {
		t.Fatal(err)
	}

	history, err := LoadHistory(path, "/repo", "session", MaxHistoryItems, func(hash string) (string, bool) {
		if hash == "wrapped_hash" {
			return "expanded wrapped memo", true
		}
		return "", false
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(history) != 1 || history[0].Display != "restore [Pasted text #14] [Image #15]" {
		t.Fatalf("history = %#v", history)
	}
	if got := history[0].PastedContents[14]; got.ID != 14 || got.Type != PastedContentText || got.Content != "expanded wrapped memo" || got.MediaType != "text/plain" {
		t.Fatalf("wrapped stored text = %#v", got)
	}
	if got := history[0].PastedContents[15]; got.ID != 15 || got.Type != PastedContentImage || got.MediaType != "image/png" || got.Filename != "wrapped-history.png" {
		t.Fatalf("wrapped stored image = %#v", got)
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

func TestAddToHistoryStoresImagePastedContentMetadata(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("CLAUDE_CONFIG_DIR", dir)
	path := filepath.Join(dir, "history.jsonl")

	written, err := AddToHistory(path, "/repo", "session", HistoryEntry{
		Display: "cmd [Image #1]",
		PastedContents: map[int]PastedContent{
			1: {
				ID:         1,
				Type:       PastedContentImage,
				Content:    "base64",
				MediaType:  "image/png",
				Filename:   "chart.png",
				SourcePath: filepath.Join(dir, "image-cache", "session", "1.png"),
				Dimensions: &ImageDimensions{OriginalWidth: 4000, OriginalHeight: 2000, DisplayWidth: 1000, DisplayHeight: 500},
			},
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
	if !strings.Contains(raw, `"type":"image"`) || !strings.Contains(raw, "chart.png") || !strings.Contains(raw, "image-cache") {
		t.Fatalf("history should store image metadata: %s", raw)
	}
	if strings.Contains(raw, "base64") || strings.Contains(raw, `"contentHash"`) {
		t.Fatalf("history should not store image bytes or text-paste hash for images: %s", raw)
	}
	history, err := LoadHistory(path, "/repo", "session", MaxHistoryItems, RetrievePastedText)
	if err != nil {
		t.Fatal(err)
	}
	if len(history) != 1 || history[0].Display != "cmd [Image #1]" {
		t.Fatalf("history = %#v", history)
	}
	image := history[0].PastedContents[1]
	if image.Type != PastedContentImage || image.Content != "" || image.MediaType != "image/png" || image.Filename != "chart.png" || image.SourcePath == "" || image.Dimensions == nil || image.Dimensions.OriginalWidth != 4000 {
		t.Fatalf("image history = %#v", image)
	}
}

func TestLoadHistoryRestoresExistingImageCacheSourcePath(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("CLAUDE_CONFIG_DIR", dir)
	ClearStoredImagePaths()
	defer ClearStoredImagePaths()

	historyPath := filepath.Join(dir, "history.jsonl")
	imagePath := filepath.Join(dir, "image-cache", "session", "4.webp")
	if err := os.MkdirAll(filepath.Dir(imagePath), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(imagePath, []byte("webp"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := AppendHistory(historyPath, LogEntry{
		Display:   "cmd [Image #4]",
		Timestamp: 100,
		Project:   "/repo",
		SessionID: "session",
		PastedContents: map[int]StoredPastedContent{
			4: {ID: 4, Type: PastedContentImage, MediaType: "image/webp", Filename: "diagram.webp"},
		},
	}); err != nil {
		t.Fatal(err)
	}

	history, err := LoadHistory(historyPath, "/repo", "session", MaxHistoryItems, RetrievePastedText)
	if err != nil {
		t.Fatal(err)
	}
	if len(history) != 1 {
		t.Fatalf("history = %#v", history)
	}
	image := history[0].PastedContents[4]
	if image.SourcePath != imagePath || image.MediaType != "image/webp" || image.Filename != "diagram.webp" {
		t.Fatalf("image = %#v, want source path %q", image, imagePath)
	}
	if cached, ok := GetStoredImagePath(4); !ok || cached != imagePath {
		t.Fatalf("cached image path = %q ok=%v, want %q", cached, ok, imagePath)
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

func requireSameFile(t *testing.T, got string, want string) {
	t.Helper()
	gotInfo, err := os.Stat(got)
	if err != nil {
		t.Fatalf("stat got file %q: %v", got, err)
	}
	wantInfo, err := os.Stat(want)
	if err != nil {
		t.Fatalf("stat want file %q: %v", want, err)
	}
	if !os.SameFile(gotInfo, wantInfo) {
		t.Fatalf("files differ: got %q want %q", got, want)
	}
}
