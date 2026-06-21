package webtools

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"ccgo/internal/tool"
)

type fakeSummarizer struct {
	gotContent string
	gotPrompt  string
	reply      string
}

func (f *fakeSummarizer) Summarize(_ context.Context, req SummarizeRequest) (string, error) {
	f.gotContent = req.Content
	f.gotPrompt = req.Prompt
	return f.reply, nil
}

func TestWebFetchSummarizesWithSecondaryModel(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "text/html")
		_, _ = w.Write([]byte("<html><body><p>The release ships on Tuesday.</p></body></html>"))
	}))
	defer server.Close()

	sum := &fakeSummarizer{reply: "Ships Tuesday."}
	toolImpl := NewWebFetchTool()
	raw, _ := json.Marshal(map[string]any{"url": server.URL, "prompt": "When does it ship?"})
	ctx := tool.Context{
		Context: context.Background(),
		Metadata: map[string]any{
			MetadataWebFetchSkipPreflightKey: true,
			MetadataSecondaryModelClientKey:  sum,
		},
	}
	res, err := toolImpl.Call(ctx, raw, tool.NopProgressSink())
	if err != nil {
		t.Fatalf("Call err: %v", err)
	}
	if !strings.Contains(sum.gotPrompt, "When does it ship?") {
		t.Fatalf("summarizer prompt missing original question: %q", sum.gotPrompt)
	}
	if !strings.Contains(sum.gotPrompt, "Web page content:") {
		t.Fatalf("summarizer prompt missing template framing: %q", sum.gotPrompt)
	}
	if !strings.Contains(sum.gotContent, "ships on Tuesday") {
		t.Fatalf("summarizer did not receive rendered content: %q", sum.gotContent)
	}
	content, _ := res.Content.(string)
	if !strings.Contains(content, "Ships Tuesday.") {
		t.Fatalf("result missing model summary: %q", content)
	}
}

func TestWebFetchFallbackWithoutSecondaryModel(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "text/plain")
		_, _ = w.Write([]byte("raw content here"))
	}))
	defer server.Close()

	toolImpl := NewWebFetchTool()
	raw, _ := json.Marshal(map[string]any{"url": server.URL, "prompt": "what does it say?"})
	ctx := tool.Context{
		Context: context.Background(),
		Metadata: map[string]any{
			MetadataWebFetchSkipPreflightKey: true,
			// no MetadataSecondaryModelClientKey → raw behavior
		},
	}
	res, err := toolImpl.Call(ctx, raw, tool.NopProgressSink())
	if err != nil {
		t.Fatalf("Call err: %v", err)
	}
	content, _ := res.Content.(string)
	if !strings.Contains(content, "raw content here") {
		t.Fatalf("fallback result missing raw body: %q", content)
	}
}

func TestMakeSecondaryModelPromptStructure(t *testing.T) {
	got := makeSecondaryModelPrompt("BODY", "QUESTION")
	if !strings.Contains(got, "Web page content:") || !strings.Contains(got, "BODY") || !strings.Contains(got, "QUESTION") {
		t.Fatalf("prompt structure wrong: %q", got)
	}
	if !strings.Contains(got, "---") {
		t.Fatalf("prompt missing delimiter: %q", got)
	}
	if !strings.Contains(got, "125") {
		t.Fatalf("prompt missing quote guideline: %q", got)
	}
}

func TestTruncateMarkdownCap(t *testing.T) {
	long := strings.Repeat("a", maxSummarizeMarkdown+100)
	got := truncateMarkdown(long)
	if len([]rune(got)) != maxSummarizeMarkdown {
		t.Fatalf("expected %d runes, got %d", maxSummarizeMarkdown, len([]rune(got)))
	}
}
