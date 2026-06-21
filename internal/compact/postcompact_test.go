package compact

import (
	"strings"
	"testing"

	"ccgo/internal/messages"
)

func TestBuildPostCompactAttachmentsRecentFirstAndSkipPreserved(t *testing.T) {
	files := []ReadFileEntry{
		{Path: "/a.go", Content: "AAA", Timestamp: 100},
		{Path: "/b.go", Content: "BBB", Timestamp: 300}, // most recent
		{Path: "/c.go", Content: "CCC", Timestamp: 200},
		{Path: "/preserved.go", Content: "PPP", Timestamp: 999},
	}
	opts := PostCompactOptions{
		MaxFiles:       2,
		PreservedPaths: map[string]bool{"/preserved.go": true},
	}
	msgs := BuildPostCompactAttachments(files, opts)
	if len(msgs) != 2 {
		t.Fatalf("got %d attachments want 2", len(msgs))
	}
	// preserved.go must be skipped even though it is the newest.
	body := messages.TextContent(msgs[0]) + messages.TextContent(msgs[1])
	if strings.Contains(body, "PPP") {
		t.Fatal("preserved file must not be re-attached")
	}
	// most recent non-preserved first: b.go then c.go.
	if !strings.Contains(messages.TextContent(msgs[0]), "BBB") {
		t.Fatalf("first attachment should be the newest (b.go); got %q", messages.TextContent(msgs[0]))
	}
}

func TestBuildPostCompactAttachmentsTokenBudget(t *testing.T) {
	big := make([]byte, 0, 40000)
	for i := 0; i < 40000; i++ {
		big = append(big, 'x')
	}
	files := []ReadFileEntry{
		{Path: "/1", Content: string(big), Timestamp: 3},
		{Path: "/2", Content: string(big), Timestamp: 2},
		{Path: "/3", Content: string(big), Timestamp: 1},
	}
	opts := PostCompactOptions{MaxFiles: 5, TokenBudget: 50000, MaxTokensPerFile: 50000, ApproxTokensPerChar: 1}
	msgs := BuildPostCompactAttachments(files, opts)
	// 40000 * 1.0 = 40000 tokens per file; 40000+40000 = 80000 > 50000 budget => at most 1 fits.
	if len(msgs) > 1 {
		t.Fatalf("token budget exceeded: %d attachments", len(msgs))
	}
}

func TestBuildPostCompactAttachmentsFileCapAt5(t *testing.T) {
	files := make([]ReadFileEntry, 10)
	for i := range files {
		files[i] = ReadFileEntry{
			Path:      "/f" + string(rune('0'+i)),
			Content:   "X",
			Timestamp: int64(i),
		}
	}
	opts := PostCompactOptions{} // defaults: MaxFiles=5
	msgs := BuildPostCompactAttachments(files, opts)
	if len(msgs) > 5 {
		t.Fatalf("expected at most 5 attachments (default cap), got %d", len(msgs))
	}
}

func TestBuildPostCompactAttachmentsEmpty(t *testing.T) {
	msgs := BuildPostCompactAttachments(nil, PostCompactOptions{})
	if len(msgs) != 0 {
		t.Fatalf("expected 0 attachments for nil input, got %d", len(msgs))
	}
	msgs = BuildPostCompactAttachments([]ReadFileEntry{}, PostCompactOptions{})
	if len(msgs) != 0 {
		t.Fatalf("expected 0 attachments for empty input, got %d", len(msgs))
	}
}

func TestBuildPostCompactAttachmentsPerFileLimit(t *testing.T) {
	// A file exceeding per-file token limit should be skipped.
	big := make([]byte, 10000)
	for i := range big {
		big[i] = 'y'
	}
	files := []ReadFileEntry{
		{Path: "/big.go", Content: string(big), Timestamp: 10},
		{Path: "/small.go", Content: "tiny", Timestamp: 5},
	}
	opts := PostCompactOptions{
		MaxFiles:            5,
		TokenBudget:         50000,
		MaxTokensPerFile:    100, // 10000 chars * 0.25 = 2500 tokens >> 100 limit
		ApproxTokensPerChar: 0.25,
	}
	msgs := BuildPostCompactAttachments(files, opts)
	for _, m := range msgs {
		if strings.Contains(messages.TextContent(m), "big.go") {
			t.Fatal("oversized file should be skipped by per-file token limit")
		}
	}
	// small.go (4 chars * 0.25 = 1 token) must be included.
	found := false
	for _, m := range msgs {
		if strings.Contains(messages.TextContent(m), "small.go") {
			found = true
		}
	}
	if !found {
		t.Fatal("small file within per-file budget should be attached")
	}
}

func TestBuildPostCompactAttachmentsOutputContainsPathAndContent(t *testing.T) {
	files := []ReadFileEntry{
		{Path: "/myfile.go", Content: "package main", Timestamp: 1},
	}
	msgs := BuildPostCompactAttachments(files, PostCompactOptions{})
	if len(msgs) != 1 {
		t.Fatalf("expected 1 message, got %d", len(msgs))
	}
	body := messages.TextContent(msgs[0])
	if !strings.Contains(body, "/myfile.go") {
		t.Errorf("output should contain the file path; got %q", body)
	}
	if !strings.Contains(body, "package main") {
		t.Errorf("output should contain the file content; got %q", body)
	}
}
