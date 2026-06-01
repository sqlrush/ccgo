package memory

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"ccgo/internal/api/anthropic"
	"ccgo/internal/contracts"
	msgs "ccgo/internal/messages"
	"ccgo/internal/session"
)

func TestScanDirectoryParsesFrontmatterAndFormatsManifest(t *testing.T) {
	dir := t.TempDir()
	writeFile(t, filepath.Join(dir, "a.md"), "---\ndescription: Alpha\ntype: project\n---\nbody\n")
	writeFile(t, filepath.Join(dir, "nested", "b.md"), "---\ndescription: Beta\ntype: team\n---\nbody\n")
	writeFile(t, filepath.Join(dir, "MEMORY.md"), "ignored")
	if err := os.Chtimes(filepath.Join(dir, "a.md"), time.Unix(10, 0), time.Unix(10, 0)); err != nil {
		t.Fatal(err)
	}
	if err := os.Chtimes(filepath.Join(dir, "nested", "b.md"), time.Unix(20, 0), time.Unix(20, 0)); err != nil {
		t.Fatal(err)
	}

	headers, err := ScanDirectory(dir, ScanOptions{})
	if err != nil {
		t.Fatal(err)
	}
	if len(headers) != 2 {
		t.Fatalf("headers = %#v", headers)
	}
	if headers[0].Filename != "nested/b.md" || headers[0].Description != "Beta" || headers[0].Type != TypeTeam {
		t.Fatalf("first header = %#v", headers[0])
	}
	manifest := FormatManifest(headers)
	if !strings.Contains(manifest, "- [team] nested/b.md") || !strings.Contains(manifest, ": Beta") {
		t.Fatalf("manifest = %q", manifest)
	}
}

func TestDiscoverClaudeFilesReturnsRootToLeaf(t *testing.T) {
	root := t.TempDir()
	child := filepath.Join(root, "sub", "project")
	if err := os.MkdirAll(child, 0o755); err != nil {
		t.Fatal(err)
	}
	writeFile(t, filepath.Join(root, "CLAUDE.md"), "root")
	writeFile(t, filepath.Join(root, "sub", "CLAUDE.md"), "sub")

	files, err := DiscoverClaudeFiles(child)
	if err != nil {
		t.Fatal(err)
	}
	if len(files) != 2 {
		t.Fatalf("files = %#v", files)
	}
	if files[0].Root != root || files[1].Root != filepath.Join(root, "sub") {
		t.Fatalf("order = %#v", files)
	}
}

func TestGuardTeamMemoryWriteRejectsSecrets(t *testing.T) {
	err := GuardTeamMemoryWrite("/repo/.claude/team-memory/auth.md", "token = ghp_123456789012345678901234567890123456")
	if err == nil {
		t.Fatal("expected secret error")
	}
	if !strings.Contains(err.Error(), "line 1") {
		t.Fatalf("err = %v", err)
	}
	if err := GuardTeamMemoryWrite("/repo/notes.md", "token = ghp_123456789012345678901234567890123456"); err != nil {
		t.Fatalf("non-team memory should not be blocked: %v", err)
	}
}

func TestWriteAndLoadSessionSummary(t *testing.T) {
	root := filepath.Join(t.TempDir(), "session-memory")
	updatedAt := time.Unix(100, 0).UTC()
	written, err := WriteSessionSummary(SessionSummaryOptions{
		Root:            root,
		SessionID:       "sess_1",
		Summary:         "summary text\n",
		UpdatedAt:       updatedAt,
		LastMessageUUID: "msg_summary",
		Metadata: sessionCompactMetadata(
			"auto",
			123,
			4,
		),
	})
	if err != nil {
		t.Fatal(err)
	}
	if written.Path != filepath.Join(root, "sess_1", SessionSummaryFilename) {
		t.Fatalf("path = %q", written.Path)
	}
	loaded, err := LoadSessionSummary(written.Path)
	if err != nil {
		t.Fatal(err)
	}
	if loaded.SessionID != "sess_1" || loaded.Summary != "summary text" || !loaded.UpdatedAt.Equal(updatedAt) {
		t.Fatalf("loaded = %#v", loaded)
	}
	if loaded.Metadata.Trigger != "auto" || loaded.Metadata.PreTokens != 123 || loaded.Metadata.MessagesSummarized != 4 {
		t.Fatalf("metadata = %#v", loaded.Metadata)
	}
	headers, err := ScanDirectory(root, ScanOptions{})
	if err != nil {
		t.Fatal(err)
	}
	if len(headers) != 1 || headers[0].Type != TypeSession {
		t.Fatalf("headers = %#v", headers)
	}
}

func TestRecallSessionSummariesScoresAndLimits(t *testing.T) {
	root := filepath.Join(t.TempDir(), "session-memory")
	if _, err := WriteSessionSummary(SessionSummaryOptions{
		Root:      root,
		SessionID: "older",
		Summary:   "database migration notes",
		UpdatedAt: time.Unix(100, 0).UTC(),
		Metadata:  sessionCompactMetadata("auto", 10, 2),
	}); err != nil {
		t.Fatal(err)
	}
	if _, err := WriteSessionSummary(SessionSummaryOptions{
		Root:      root,
		SessionID: "newer",
		Summary:   "database database permissions",
		UpdatedAt: time.Unix(200, 0).UTC(),
		Metadata:  sessionCompactMetadata("auto", 10, 2),
	}); err != nil {
		t.Fatal(err)
	}
	matches, err := RecallSessionSummaries(root, "database permissions", RecallOptions{Limit: 1})
	if err != nil {
		t.Fatal(err)
	}
	if len(matches) != 1 || matches[0].Summary.SessionID != "newer" || matches[0].Score != 3 {
		t.Fatalf("matches = %#v", matches)
	}
	all, err := RecallSessionSummaries(root, "", RecallOptions{})
	if err != nil {
		t.Fatal(err)
	}
	if len(all) != 2 || all[0].Summary.SessionID != "newer" {
		t.Fatalf("all = %#v", all)
	}
	excluding, err := RecallSessionSummaries(root, "database", RecallOptions{ExcludeSessionID: "newer"})
	if err != nil {
		t.Fatal(err)
	}
	if len(excluding) != 1 || excluding[0].Summary.SessionID != "older" {
		t.Fatalf("excluding = %#v", excluding)
	}
	context := BuildRecallContext(matches)
	if !strings.Contains(context, "Relevant session memory") || !strings.Contains(context, "[newer]") {
		t.Fatalf("context = %q", context)
	}
	message := RecallContextMessage(matches)
	if message.Subtype != RecallContextSubtype || !message.IsMeta || !strings.Contains(message.Content[0].Text, "permissions") {
		t.Fatalf("message = %#v", message)
	}
}

func TestExtractFactsBuildsSessionMemorySummary(t *testing.T) {
	toolInput := json.RawMessage(`{"file_path":"README.md"}`)
	assistant := msgs.AssistantText("", "sonnet", nil)
	assistant.UUID = "assistant_1"
	assistant.Content = []contracts.ContentBlock{{
		Type:  contracts.ContentToolUse,
		ID:    "toolu_1",
		Name:  "Read",
		Input: toolInput,
	}}
	user := msgs.UserText("Remember prefer compact diffs")
	user.UUID = "user_1"
	decision := msgs.AssistantText("Decision: keep the session summary short", "sonnet", nil)
	decision.UUID = "assistant_2"

	facts := ExtractFacts([]contracts.Message{user, assistant, decision, user}, ExtractOptions{Limit: 10})
	if len(facts) != 3 {
		t.Fatalf("facts = %#v", facts)
	}
	if facts[0].Kind != FactPreference || facts[0].Text != "prefer compact diffs" || facts[0].SourceUUID != "user_1" {
		t.Fatalf("preference = %#v", facts[0])
	}
	if facts[1].Kind != FactTool || facts[1].Text != "Used tool Read" {
		t.Fatalf("tool = %#v", facts[1])
	}
	summary := BuildFactsSummary(facts)
	if !strings.Contains(summary, "[preference] prefer compact diffs") || !strings.Contains(summary, "[decision] keep the session summary short") {
		t.Fatalf("summary = %q", summary)
	}
}

func TestMemoryAgentExtractsModelFactsAndFallsBack(t *testing.T) {
	client := &fakeMemoryClient{response: &anthropic.Response{
		ID:    "msg_memory",
		Type:  "message",
		Role:  "assistant",
		Model: "sonnet",
		Content: []contracts.ContentBlock{contracts.NewTextBlock(`[
			{"kind":"preference","text":"use terse summaries","source_uuid":"user_1"},
			{"kind":"unknown","text":"ignore me","source_uuid":"user_2"}
		]`)},
	}}
	result, err := (Agent{Client: client, Model: "sonnet", MaxTokens: 128}).Extract(context.Background(), []contracts.Message{msgs.UserText("Remember use terse summaries")}, ExtractOptions{Limit: 5})
	if err != nil {
		t.Fatal(err)
	}
	if result.Fallback || len(result.Facts) != 1 || result.Facts[0].Text != "use terse summaries" || result.Request.MaxTokens != 128 {
		t.Fatalf("result = %#v", result)
	}
	if len(client.requests) != 1 || !strings.Contains(client.requests[0].Messages[0].Content[0].Text, "Return only JSON") {
		t.Fatalf("request = %#v", client.requests)
	}

	fallbackClient := &fakeMemoryClient{response: &anthropic.Response{Content: []contracts.ContentBlock{contracts.NewTextBlock(`not json`)}}}
	fallback, err := (Agent{Client: fallbackClient}).Extract(context.Background(), []contracts.Message{msgs.UserText("Remember fallback fact")}, ExtractOptions{})
	if err != nil {
		t.Fatal(err)
	}
	if !fallback.Fallback || len(fallback.Facts) != 1 || fallback.Facts[0].Text != "fallback fact" {
		t.Fatalf("fallback = %#v", fallback)
	}
}

func TestMemoryAgentRecallUsesModelQueryThenScoresLocalSummaries(t *testing.T) {
	root := filepath.Join(t.TempDir(), "session-memory")
	if _, err := WriteSessionSummary(SessionSummaryOptions{Root: root, SessionID: "prior", Summary: "postgres permissions migration"}); err != nil {
		t.Fatal(err)
	}
	client := &fakeMemoryClient{response: &anthropic.Response{
		ID:      "msg_recall",
		Type:    "message",
		Role:    "assistant",
		Model:   "sonnet",
		Content: []contracts.ContentBlock{contracts.NewTextBlock("postgres permissions")},
	}}
	result, err := (Agent{Client: client}).Recall(context.Background(), root, "what did we decide about db access?", RecallOptions{Limit: 1})
	if err != nil {
		t.Fatal(err)
	}
	if result.Fallback || result.Query != "postgres permissions" || len(result.Matches) != 1 || result.Matches[0].Summary.SessionID != "prior" {
		t.Fatalf("result = %#v", result)
	}
}

func TestBuildResumeContextLoadsCurrentSummaryAndRecallsRelatedSessions(t *testing.T) {
	dir := t.TempDir()
	sessionPath := filepath.Join(dir, "session.jsonl")
	root := filepath.Join(dir, "session-memory")
	writeFile(t, sessionPath, strings.Join([]string{
		`{"type":"user","uuid":"u1","sessionId":"current","parentUuid":null,"message":{"type":"user","uuid":"u1","sessionId":"current","content":[{"type":"text","text":"continue postgres permissions"}]}}`,
		`{"type":"assistant","uuid":"a1","sessionId":"current","parentUuid":"u1","message":{"type":"assistant","uuid":"a1","sessionId":"current","content":[{"type":"text","text":"ok"}]}}`,
	}, "\n")+"\n")
	if _, err := WriteSessionSummary(SessionSummaryOptions{
		Root:      root,
		SessionID: "current",
		Summary:   "current session summary",
		UpdatedAt: time.Unix(100, 0).UTC(),
	}); err != nil {
		t.Fatal(err)
	}
	if _, err := WriteSessionSummary(SessionSummaryOptions{
		Root:      root,
		SessionID: "prior",
		Summary:   "postgres permissions migration notes",
		UpdatedAt: time.Unix(200, 0).UTC(),
	}); err != nil {
		t.Fatal(err)
	}
	context, err := BuildResumeContext(ResumeContextOptions{
		SessionPath: sessionPath,
		SessionID:   "current",
		MemoryRoot:  root,
		RecallLimit: 2,
	})
	if err != nil {
		t.Fatal(err)
	}
	if !context.Conversation.Found || context.Conversation.Leaf != "a1" || len(context.Conversation.Messages) != 2 {
		t.Fatalf("conversation = %#v", context.Conversation)
	}
	if context.CurrentSummary == nil || context.CurrentSummary.Summary != "current session summary" {
		t.Fatalf("current summary = %#v", context.CurrentSummary)
	}
	if context.RecallQuery != "continue postgres permissions" {
		t.Fatalf("query = %q", context.RecallQuery)
	}
	if len(context.Recalled) != 1 || context.Recalled[0].Summary.SessionID != "prior" {
		t.Fatalf("recalled = %#v", context.Recalled)
	}
}

func sessionCompactMetadata(trigger string, preTokens int, summarized int) session.CompactMetadata {
	return session.CompactMetadata{Trigger: trigger, PreTokens: preTokens, MessagesSummarized: summarized}
}

type fakeMemoryClient struct {
	requests []anthropic.Request
	response *anthropic.Response
	err      error
}

func (f *fakeMemoryClient) CreateMessage(ctx context.Context, request anthropic.Request) (*anthropic.Response, error) {
	f.requests = append(f.requests, request)
	return f.response, f.err
}

func writeFile(t *testing.T, path string, content string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
}
