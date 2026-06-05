package memory

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
	"unicode/utf8"

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

func TestMemoryFreshnessHelpersMatchOfficialAgeText(t *testing.T) {
	now := time.Date(2026, 6, 3, 12, 0, 0, 0, time.UTC)
	if got := MemoryAge(now.Add(-time.Hour), now); got != "today" {
		t.Fatalf("today age = %q", got)
	}
	if got := MemoryAge(now.Add(-25*time.Hour), now); got != "yesterday" {
		t.Fatalf("yesterday age = %q", got)
	}
	if got := MemoryAge(now.Add(-3*24*time.Hour), now); got != "3 days ago" {
		t.Fatalf("old age = %q", got)
	}
	if got := MemoryAge(now.Add(time.Hour), now); got != "today" {
		t.Fatalf("future age = %q", got)
	}
	if got := MemoryFreshnessText(now.Add(-25*time.Hour), now); got != "" {
		t.Fatalf("fresh text = %q", got)
	}
	note := MemoryFreshnessNote(now.Add(-3*24*time.Hour), now)
	if !strings.HasPrefix(note, "<system-reminder>This memory is 3 days old.") || !strings.HasSuffix(note, "</system-reminder>\n") {
		t.Fatalf("note = %q", note)
	}
}

func TestReadDocumentsCanPrefixMemoryFreshnessNote(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "stale.md")
	writeFile(t, path, "---\ndescription: Stale\n---\nbody\n")
	mtime := time.Date(2026, 5, 30, 0, 0, 0, 0, time.UTC)
	if err := os.Chtimes(path, mtime, mtime); err != nil {
		t.Fatal(err)
	}
	headers, err := ScanDirectory(dir, ScanOptions{})
	if err != nil {
		t.Fatal(err)
	}

	plain, err := ReadDocuments(headers, 0)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(plain[0].Content, "system-reminder") {
		t.Fatalf("plain content = %q", plain[0].Content)
	}

	docs, err := ReadDocumentsWithOptions(headers, ReadOptions{
		IncludeFreshnessNote: true,
		Now:                  time.Date(2026, 6, 3, 12, 0, 0, 0, time.UTC),
	})
	if err != nil {
		t.Fatal(err)
	}
	if !strings.HasPrefix(docs[0].Content, "<system-reminder>This memory is 4 days old.") || !strings.HasSuffix(docs[0].Content, "body\n") {
		t.Fatalf("content = %q", docs[0].Content)
	}
}

func TestRelevantMemoryAttachmentHeaderRenderAndScan(t *testing.T) {
	now := time.Date(2026, 6, 3, 12, 0, 0, 0, time.UTC)
	stale := NewRelevantMemory("/repo/.claude/memory/old.md", "old fact", now.Add(-3*24*time.Hour), now)
	fresh := NewRelevantMemory("/repo/.claude/memory/today.md", "today fact", now, now)
	if !strings.HasPrefix(stale.Header, "This memory is 3 days old.") || !strings.Contains(stale.Header, "\n\nMemory: /repo/.claude/memory/old.md:") {
		t.Fatalf("stale header = %q", stale.Header)
	}
	if fresh.Header != "Memory (saved today): /repo/.claude/memory/today.md:" {
		t.Fatalf("fresh header = %q", fresh.Header)
	}

	attachment := RelevantMemoriesAttachmentMessage([]RelevantMemory{stale, fresh})
	if attachment.Type != contracts.MessageAttachment || attachment.Subtype != RelevantMemoriesSubtype {
		t.Fatalf("attachment = %#v", attachment)
	}
	rendered := RenderRelevantMemoriesAttachment(attachment, now)
	if len(rendered) != 2 {
		t.Fatalf("rendered = %#v", rendered)
	}
	text := rendered[0].Content[0].Text
	if !rendered[0].IsMeta || rendered[0].Type != contracts.MessageUser || !strings.HasPrefix(text, "<system-reminder>\nThis memory is 3 days old.") || !strings.Contains(text, "\n\nold fact\n</system-reminder>") {
		t.Fatalf("rendered stale message = %#v", rendered[0])
	}

	surfaced := CollectSurfacedMemories([]contracts.Message{attachment, msgs.UserText("ignore")})
	if len(surfaced.Paths) != 2 || surfaced.TotalBytes != len("old fact")+len("today fact") {
		t.Fatalf("surfaced = %#v", surfaced)
	}
	if _, ok := surfaced.Paths[stale.Path]; !ok {
		t.Fatalf("missing stale path: %#v", surfaced.Paths)
	}
}

func TestRelevantMemoryAttachmentRenderFallsBackToHeaderFromMtime(t *testing.T) {
	now := time.Date(2026, 6, 3, 12, 0, 0, 0, time.UTC)
	message := contracts.Message{
		Type: contracts.MessageAttachment,
		Raw: map[string]any{
			"attachment": map[string]any{
				"type": RelevantMemoriesAttachmentType,
				"memories": []map[string]any{{
					"path":     "/repo/.claude/memory/legacy.md",
					"content":  "legacy fact",
					"mtime_ms": now.Add(-2 * 24 * time.Hour).UnixMilli(),
				}},
			},
		},
	}
	rendered := RenderRelevantMemoriesAttachment(message, now)
	if len(rendered) != 1 || !strings.Contains(rendered[0].Content[0].Text, "This memory is 2 days old.") || !strings.Contains(rendered[0].Content[0].Text, "Memory: /repo/.claude/memory/legacy.md:") {
		t.Fatalf("rendered = %#v", rendered)
	}
}

func TestReadMemoriesForSurfacingTruncatesAndSkipsUnreadableFiles(t *testing.T) {
	dir := t.TempDir()
	now := time.Date(2026, 6, 3, 12, 0, 0, 0, time.UTC)
	linePath := filepath.Join(dir, "lines.md")
	var lines strings.Builder
	for i := 1; i <= 205; i++ {
		lines.WriteString("line\n")
	}
	writeFile(t, linePath, lines.String())
	bytePath := filepath.Join(dir, "bytes.md")
	writeFile(t, bytePath, strings.Repeat("a", MaxRelevantMemoryBytes+10))
	mtime := now.Add(-3 * 24 * time.Hour)
	if err := os.Chtimes(linePath, mtime, mtime); err != nil {
		t.Fatal(err)
	}
	if err := os.Chtimes(bytePath, mtime, mtime); err != nil {
		t.Fatal(err)
	}

	memories := ReadMemoriesForSurfacing([]RelevantMemorySelection{
		{Path: linePath, MtimeMs: mtime.UnixMilli()},
		{Path: filepath.Join(dir, "missing.md"), MtimeMs: mtime.UnixMilli()},
		{Path: bytePath, MtimeMs: mtime.UnixMilli()},
	}, RelevantMemorySurfaceOptions{Now: now})
	if len(memories) != 2 {
		t.Fatalf("memories = %#v", memories)
	}
	if memories[0].Limit == nil || *memories[0].Limit != MaxRelevantMemoryLines || !strings.Contains(memories[0].Content, "first 200 lines") {
		t.Fatalf("line-truncated memory = %#v", memories[0])
	}
	if strings.Count(memories[0].Content, "line\n") != MaxRelevantMemoryLines {
		t.Fatalf("line content = %q", memories[0].Content)
	}
	if memories[1].Limit == nil || !strings.Contains(memories[1].Content, "4096 byte limit") || !strings.Contains(memories[1].Header, "This memory is 3 days old.") {
		t.Fatalf("byte-truncated memory = %#v", memories[1])
	}
}

func TestFilterDuplicateRelevantMemoryAttachmentsMarksSurvivors(t *testing.T) {
	now := time.Date(2026, 6, 3, 12, 0, 0, 0, time.UTC)
	first := NewRelevantMemory("/repo/mem/a.md", "alpha", now, now)
	second := NewRelevantMemory("/repo/mem/b.md", "beta", now, now)
	attachment := RelevantMemoriesAttachmentMessage([]RelevantMemory{first, second})
	state := map[string]RelevantMemoryReadState{
		first.Path: {Content: first.Content, MtimeMs: first.MtimeMs},
	}

	filtered := FilterDuplicateRelevantMemoryAttachments([]contracts.Message{msgs.UserText("keep"), attachment}, state)
	if len(filtered) != 2 || filtered[0].Type != contracts.MessageUser {
		t.Fatalf("filtered = %#v", filtered)
	}
	memories := RelevantMemoriesFromAttachmentMessage(filtered[1])
	if len(memories) != 1 || memories[0].Path != second.Path {
		t.Fatalf("memories = %#v", memories)
	}
	if got, ok := state[second.Path]; !ok || got.Content != "beta" || got.MtimeMs != second.MtimeMs {
		t.Fatalf("state = %#v", state)
	}

	filteredAgain := FilterDuplicateRelevantMemoryAttachments([]contracts.Message{filtered[1]}, state)
	if len(filteredAgain) != 0 {
		t.Fatalf("filtered again = %#v", filteredAgain)
	}
}

func TestRelevantMemoryPrefetchPlanUsesLastNonMetaUserAndSessionCap(t *testing.T) {
	now := time.Date(2026, 6, 3, 12, 0, 0, 0, time.UTC)
	memory := NewRelevantMemory("/repo/mem/a.md", "alpha", now, now)
	meta := msgs.UserText("ignore meta prompt")
	meta.IsMeta = true
	plan, ok := RelevantMemoryPrefetchPlanForMessages([]contracts.Message{
		msgs.UserText("singleword"),
		RelevantMemoriesAttachmentMessage([]RelevantMemory{memory}),
		meta,
		msgs.UserText("find database memory"),
	}, 0)
	if !ok || plan.Input != "find database memory" || plan.Surfaced.TotalBytes != len("alpha") {
		t.Fatalf("plan=%#v ok=%v", plan, ok)
	}
	if _, ok := plan.Surfaced.Paths[memory.Path]; !ok {
		t.Fatalf("surfaced paths = %#v", plan.Surfaced.Paths)
	}

	if _, ok := RelevantMemoryPrefetchPlanForMessages([]contracts.Message{msgs.UserText("singleword")}, 0); ok {
		t.Fatal("single-word prompt should not prefetch")
	}
	large := NewRelevantMemory("/repo/mem/large.md", strings.Repeat("x", MaxRelevantMemorySessionBytes), now, now)
	if _, ok := RelevantMemoryPrefetchPlanForMessages([]contracts.Message{
		RelevantMemoriesAttachmentMessage([]RelevantMemory{large}),
		msgs.UserText("find database memory"),
	}, 0); ok {
		t.Fatal("session byte cap should stop prefetch")
	}
}

func TestSelectRelevantMemoryCandidatesFiltersAndCaps(t *testing.T) {
	results := [][]RelevantMemorySelection{{
		{Path: "/repo/mem/read.md"},
		{Path: "/repo/mem/surfaced.md"},
		{Path: "/repo/mem/one.md"},
	}, {
		{Path: "/repo/mem/two.md"},
		{Path: "/repo/mem/three.md"},
		{Path: "/repo/mem/four.md"},
		{Path: "/repo/mem/five.md"},
		{Path: "/repo/mem/six.md"},
	}}
	selected := SelectRelevantMemoryCandidates(results, map[string]RelevantMemoryReadState{
		"/repo/mem/read.md": {},
	}, map[string]struct{}{
		"/repo/mem/surfaced.md": {},
	}, 0)
	if len(selected) != MaxRelevantMemoryAttachments {
		t.Fatalf("selected = %#v", selected)
	}
	got := make([]string, 0, len(selected))
	for _, item := range selected {
		got = append(got, item.Path)
	}
	if strings.Join(got, ",") != "/repo/mem/one.md,/repo/mem/two.md,/repo/mem/three.md,/repo/mem/four.md,/repo/mem/five.md" {
		t.Fatalf("paths = %#v", got)
	}
}

func TestFindRelevantMemorySelectionsScoresAndSuppressesRecentToolReferences(t *testing.T) {
	dir := t.TempDir()
	writeFile(t, filepath.Join(dir, "db.md"), "---\ndescription: database permissions migration\n---\nbody\n")
	writeFile(t, filepath.Join(dir, "old-db.md"), "---\ndescription: database migration notes\n---\nbody\n")
	writeFile(t, filepath.Join(dir, "bash-reference.md"), "---\ndescription: Bash API usage reference\n---\nbody\n")
	writeFile(t, filepath.Join(dir, "bash-warning.md"), "---\ndescription: Bash warning about permission errors\n---\nbody\n")
	writeFile(t, filepath.Join(dir, "MEMORY.md"), "---\ndescription: database permissions root manifest\n---\nbody\n")
	if err := os.Chtimes(filepath.Join(dir, "db.md"), time.Unix(30, 0), time.Unix(30, 0)); err != nil {
		t.Fatal(err)
	}
	if err := os.Chtimes(filepath.Join(dir, "old-db.md"), time.Unix(20, 0), time.Unix(20, 0)); err != nil {
		t.Fatal(err)
	}
	if err := os.Chtimes(filepath.Join(dir, "bash-reference.md"), time.Unix(40, 0), time.Unix(40, 0)); err != nil {
		t.Fatal(err)
	}
	if err := os.Chtimes(filepath.Join(dir, "bash-warning.md"), time.Unix(10, 0), time.Unix(10, 0)); err != nil {
		t.Fatal(err)
	}

	selected, err := FindRelevantMemorySelections(dir, "database permissions", nil, map[string]struct{}{
		filepath.Join(dir, "old-db.md"): {},
	}, 0)
	if err != nil {
		t.Fatal(err)
	}
	if len(selected) != 1 || selected[0].Path != filepath.Join(dir, "db.md") {
		t.Fatalf("selected = %#v", selected)
	}

	selected, err = FindRelevantMemorySelections(dir, "bash api warning", []string{"Bash"}, nil, 0)
	if err != nil {
		t.Fatal(err)
	}
	if len(selected) != 1 || selected[0].Path != filepath.Join(dir, "bash-warning.md") {
		t.Fatalf("recent-tool selected = %#v", selected)
	}
}

func TestPrefetchRelevantMemoriesSelectsAndReads(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "db.md")
	writeFile(t, path, "---\ndescription: database permissions migration\n---\nremember database permissions\n")
	mtime := time.Date(2026, 6, 2, 12, 0, 0, 0, time.UTC)
	if err := os.Chtimes(path, mtime, mtime); err != nil {
		t.Fatal(err)
	}

	result, err := PrefetchRelevantMemories(context.Background(), []contracts.Message{
		msgs.UserText("find database permissions"),
	}, RelevantMemoryPrefetchOptions{
		Root:  dir,
		Limit: 1,
		Now:   time.Date(2026, 6, 3, 12, 0, 0, 0, time.UTC),
	})
	if err != nil {
		t.Fatal(err)
	}
	if result.Plan.Input != "find database permissions" {
		t.Fatalf("plan = %#v", result.Plan)
	}
	if len(result.Selected) != 1 || result.Selected[0].Path != path {
		t.Fatalf("selected = %#v", result.Selected)
	}
	if len(result.Memories) != 1 || result.Memories[0].Path != path || !strings.Contains(result.Memories[0].Content, "remember database permissions") {
		t.Fatalf("memories = %#v", result.Memories)
	}
}

func TestPrefetchRelevantMemoriesHonorsCanceledContext(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	_, err := PrefetchRelevantMemories(ctx, []contracts.Message{
		msgs.UserText("find database permissions"),
	}, RelevantMemoryPrefetchOptions{Root: t.TempDir()})
	if err != context.Canceled {
		t.Fatalf("err = %v", err)
	}
}

func TestMemoryAgentSelectRelevantMemoriesUsesModelPaths(t *testing.T) {
	dir := t.TempDir()
	dbPath := filepath.Join(dir, "db.md")
	opsPath := filepath.Join(dir, "ops.md")
	writeFile(t, dbPath, "---\ndescription: database permissions migration\n---\ndb rules\n")
	writeFile(t, opsPath, "---\ndescription: deployment runbook\n---\nops rules\n")
	if err := os.Chtimes(dbPath, time.Unix(100, 0), time.Unix(100, 0)); err != nil {
		t.Fatal(err)
	}
	if err := os.Chtimes(opsPath, time.Unix(200, 0), time.Unix(200, 0)); err != nil {
		t.Fatal(err)
	}
	client := &fakeMemoryClient{response: &anthropic.Response{
		ID:      "msg_memory_select",
		Type:    "message",
		Role:    "assistant",
		Model:   "sonnet",
		Content: []contracts.ContentBlock{contracts.NewTextBlock(`{"query":"database access","memory_paths":["ops","db.md"]}`)},
	}}

	result, err := (Agent{Client: client}).SelectRelevantMemories(context.Background(), dir, "database permissions", RelevantMemorySelectorOptions{
		Limit:       2,
		RecentTools: []string{"Read", "Bash"},
		Surfaced:    map[string]struct{}{"/repo/.claude/memory/old.md": {}},
	})
	if err != nil {
		t.Fatal(err)
	}
	if result.Fallback || result.Query != "database access" || strings.Join(result.SelectedIDs, ",") != "ops,db.md" {
		t.Fatalf("result = %#v", result)
	}
	if len(result.Selected) != 2 || result.Selected[0].Path != opsPath || result.Selected[1].Path != dbPath {
		t.Fatalf("selected = %#v", result.Selected)
	}
	if len(client.requests) != 1 || !strings.Contains(client.requests[0].Messages[0].Content[0].Text, "Candidate memory files") || !strings.Contains(client.requests[0].Messages[0].Content[0].Text, "id: db.md") {
		t.Fatalf("request = %#v", client.requests)
	}
	requestText := client.requests[0].Messages[0].Content[0].Text
	if !strings.Contains(requestText, "Recent successful tools in this turn") || !strings.Contains(requestText, "- Read") || !strings.Contains(requestText, "- Bash") {
		t.Fatalf("request missing recent tools = %q", requestText)
	}
	if !strings.Contains(requestText, "Already surfaced memory paths") || !strings.Contains(requestText, "/repo/.claude/memory/old.md") {
		t.Fatalf("request missing surfaced paths = %q", requestText)
	}
}

func TestMemoryAgentSelectRelevantMemoriesParsesNestedAliasesAndFallsBack(t *testing.T) {
	dir := t.TempDir()
	dbPath := filepath.Join(dir, "db.md")
	otherPath := filepath.Join(dir, "other.md")
	writeFile(t, dbPath, "---\ndescription: database permissions migration\n---\ndb rules\n")
	writeFile(t, otherPath, "---\ndescription: unrelated notes\n---\nother rules\n")
	client := &fakeMemoryClient{response: &anthropic.Response{
		ID:      "msg_memory_nested",
		Type:    "message",
		Role:    "assistant",
		Model:   "sonnet",
		Content: []contracts.ContentBlock{contracts.NewTextBlock("Selected memory:\n```json\n{\"expandedQuery\":\"database access\",\"files\":[{\"filePath\":\"other.md\"}]}\n```")},
	}}

	result, err := (Agent{Client: client}).SelectRelevantMemories(context.Background(), dir, "database permissions", RelevantMemorySelectorOptions{Limit: 1})
	if err != nil {
		t.Fatal(err)
	}
	if result.Fallback || result.Query != "database access" || strings.Join(result.SelectedIDs, ",") != "other.md" {
		t.Fatalf("nested result = %#v", result)
	}
	if len(result.Selected) != 1 || result.Selected[0].Path != otherPath {
		t.Fatalf("nested selected = %#v", result.Selected)
	}

	client.response.Content = []contracts.ContentBlock{contracts.NewTextBlock(`{"query":"database permissions","memory_paths":["missing.md"]}`)}
	result, err = (Agent{Client: client}).SelectRelevantMemories(context.Background(), dir, "database permissions", RelevantMemorySelectorOptions{Limit: 1})
	if err != nil {
		t.Fatal(err)
	}
	if !result.Fallback || strings.Join(result.SelectedIDs, ",") != "missing.md" {
		t.Fatalf("fallback result = %#v", result)
	}
	if len(result.Selected) != 1 || result.Selected[0].Path != dbPath {
		t.Fatalf("fallback selected = %#v", result.Selected)
	}
}

func TestMemoryAgentSelectRelevantMemoriesParsesGraphQLResourceSelections(t *testing.T) {
	dir := t.TempDir()
	dbPath := filepath.Join(dir, "db.md")
	opsPath := filepath.Join(dir, "ops.md")
	writeFile(t, dbPath, "---\ndescription: database permissions migration\n---\ndb rules\n")
	writeFile(t, opsPath, "---\ndescription: deployment runbook\n---\nops rules\n")
	client := &fakeMemoryClient{response: &anthropic.Response{
		ID:    "msg_memory_graphql",
		Type:  "message",
		Role:  "assistant",
		Model: "sonnet",
		Content: []contracts.ContentBlock{contracts.NewTextBlock(`{
			"data":{
				"viewer":{
					"memorySelection":{
						"query":"database access",
						"edges":[
							{"node":{"attrs":{"filePath":"ops.md"}}},
							{"edge":{"node":{"properties":{"memoryPath":"db.md"}}}}
						]
					}
				}
			}
		}`)},
	}}

	result, err := (Agent{Client: client}).SelectRelevantMemories(context.Background(), dir, "database permissions", RelevantMemorySelectorOptions{Limit: 2})
	if err != nil {
		t.Fatal(err)
	}
	if result.Fallback || result.Query != "database access" || strings.Join(result.SelectedIDs, ",") != "ops.md,db.md" {
		t.Fatalf("result = %#v", result)
	}
	if len(result.Selected) != 2 || result.Selected[0].Path != opsPath || result.Selected[1].Path != dbPath {
		t.Fatalf("selected = %#v", result.Selected)
	}
}

func TestMemoryAgentSelectRelevantMemoriesParsesIncludedCollections(t *testing.T) {
	dir := t.TempDir()
	dbPath := filepath.Join(dir, "db.md")
	opsPath := filepath.Join(dir, "ops.md")
	writeFile(t, dbPath, "---\ndescription: database permissions migration\n---\ndb rules\n")
	writeFile(t, opsPath, "---\ndescription: deployment runbook\n---\nops rules\n")
	client := &fakeMemoryClient{response: &anthropic.Response{
		ID:    "msg_memory_included",
		Type:  "message",
		Role:  "assistant",
		Model: "sonnet",
		Content: []contracts.ContentBlock{contracts.NewTextBlock(`{
			"data":{
				"collection":{
					"query":"database access",
					"included":[
						{"type":"tool","id":"tool_1","attributes":{"name":"Read"}},
						{"type":"memory-selection","id":"ops.md"},
						{"resource":{"type":"memory-selection","properties":{"filePath":"db.md"}}}
					]
				}
			}
		}`)},
	}}

	result, err := (Agent{Client: client}).SelectRelevantMemories(context.Background(), dir, "database permissions", RelevantMemorySelectorOptions{Limit: 2})
	if err != nil {
		t.Fatal(err)
	}
	if result.Fallback || result.Query != "database access" || strings.Join(result.SelectedIDs, ",") != "ops.md,db.md" {
		t.Fatalf("result = %#v", result)
	}
	if len(result.Selected) != 2 || result.Selected[0].Path != opsPath || result.Selected[1].Path != dbPath {
		t.Fatalf("selected = %#v", result.Selected)
	}
}

func TestPrefetchRelevantMemoriesCanUseMemoryAgentSelector(t *testing.T) {
	dir := t.TempDir()
	dbPath := filepath.Join(dir, "db.md")
	modelPath := filepath.Join(dir, "model.md")
	writeFile(t, dbPath, "---\ndescription: database permissions migration\n---\ndb rules\n")
	writeFile(t, modelPath, "---\ndescription: model selected memory\n---\nmodel rules\n")
	client := &fakeMemoryClient{response: &anthropic.Response{
		ID:      "msg_prefetch_select",
		Type:    "message",
		Role:    "assistant",
		Model:   "sonnet",
		Content: []contracts.ContentBlock{contracts.NewTextBlock(`{"memoryPaths":["model.md"]}`)},
	}}
	agent := Agent{Client: client}

	result, err := PrefetchRelevantMemories(context.Background(), []contracts.Message{
		msgs.UserText("database permissions"),
	}, RelevantMemoryPrefetchOptions{Root: dir, Agent: &agent})
	if err != nil {
		t.Fatal(err)
	}
	if result.Agent == nil || result.Agent.Fallback || len(result.Memories) != 1 || result.Memories[0].Path != modelPath || !strings.Contains(result.Memories[0].Content, "model rules") {
		t.Fatalf("result = %#v", result)
	}
	if len(result.Selected) != 1 || result.Selected[0].Path != modelPath || result.Selected[0].Path == dbPath {
		t.Fatalf("selected = %#v", result.Selected)
	}
}

func TestCollectRecentSuccessfulToolsUsesCurrentHumanTurnWindow(t *testing.T) {
	assistantTools := func(blocks ...contracts.ContentBlock) contracts.Message {
		return contracts.Message{Type: contracts.MessageAssistant, Content: blocks}
	}
	tools := CollectRecentSuccessfulTools([]contracts.Message{
		msgs.UserText("previous request"),
		assistantTools(contracts.ContentBlock{Type: contracts.ContentToolUse, ID: "old", Name: "OldTool"}),
		msgs.ToolResult("old", "ok", false),
		assistantTools(
			contracts.ContentBlock{Type: contracts.ContentToolUse, ID: "read_ok", Name: "Read"},
			contracts.ContentBlock{Type: contracts.ContentToolUse, ID: "bash_fail", Name: "Bash"},
		),
		msgs.ToolResult("read_ok", "ok", false),
		msgs.ToolResult("bash_fail", "nope", true),
		assistantTools(
			contracts.ContentBlock{Type: contracts.ContentToolUse, ID: "edit_ok", Name: "Edit"},
			contracts.ContentBlock{Type: contracts.ContentToolUse, ID: "read_fail", Name: "Read"},
			contracts.ContentBlock{Type: contracts.ContentToolUse, ID: "pending", Name: "Pending"},
		),
		msgs.ToolResult("edit_ok", "ok", false),
		msgs.ToolResult("read_fail", "nope", true),
		msgs.UserText("find database memory"),
	})
	if strings.Join(tools, ",") != "Edit,OldTool" {
		t.Fatalf("tools = %#v", tools)
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

func TestLoadSessionSummaryAcceptsFrontmatterFieldAliases(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, SessionSummaryFilename)
	if err := os.WriteFile(path, []byte(`---
type: session
sessionId: sess_alias
updatedAt: 2026-01-02T03:04:05Z
lastMessageUuid: msg_alias
compactTrigger: auto
messagesSummarized: 7
preTokens: 456
userContext: resume work
---
legacy summary text
`), 0o644); err != nil {
		t.Fatal(err)
	}

	loaded, err := LoadSessionSummary(path)
	if err != nil {
		t.Fatal(err)
	}
	if loaded.SessionID != "sess_alias" || loaded.LastMessageUUID != "msg_alias" || loaded.Summary != "legacy summary text" {
		t.Fatalf("loaded = %#v", loaded)
	}
	if !loaded.UpdatedAt.Equal(time.Date(2026, 1, 2, 3, 4, 5, 0, time.UTC)) {
		t.Fatalf("updated at = %s", loaded.UpdatedAt.Format(time.RFC3339))
	}
	if loaded.Metadata.Trigger != "auto" || loaded.Metadata.MessagesSummarized != 7 || loaded.Metadata.PreTokens != 456 || loaded.Metadata.UserContext != "resume work" {
		t.Fatalf("metadata = %#v", loaded.Metadata)
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

func TestCompactSessionMemoryRollsUpOlderSummaries(t *testing.T) {
	root := filepath.Join(t.TempDir(), "session-memory")
	for _, item := range []struct {
		id      contracts.ID
		summary string
		updated int64
	}{
		{id: "oldest", summary: "oldest postgres notes", updated: 100},
		{id: "middle", summary: "middle permissions notes", updated: 200},
		{id: "latest", summary: "latest active notes", updated: 300},
	} {
		if _, err := WriteSessionSummary(SessionSummaryOptions{
			Root:      root,
			SessionID: item.id,
			Summary:   item.summary,
			UpdatedAt: time.Unix(item.updated, 0).UTC(),
		}); err != nil {
			t.Fatal(err)
		}
	}
	result, err := CompactSessionMemory(root, SessionMemoryCompactionOptions{
		KeepLatest: 1,
		UpdatedAt:  time.Unix(400, 0).UTC(),
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(result.Kept) != 1 || result.Kept[0].SessionID != "latest" {
		t.Fatalf("kept = %#v", result.Kept)
	}
	if len(result.Compacted) != 2 || result.Archive == nil || result.Archive.SessionID != SessionMemoryRollupID {
		t.Fatalf("result = %#v", result)
	}
	if _, err := os.Stat(filepath.Join(root, "middle", SessionSummaryFilename)); !os.IsNotExist(err) {
		t.Fatalf("middle summary should be pruned, err=%v", err)
	}
	matches, err := RecallSessionSummaries(root, "postgres permissions", RecallOptions{Limit: 2})
	if err != nil {
		t.Fatal(err)
	}
	if len(matches) != 1 || matches[0].Summary.SessionID != SessionMemoryRollupID || !strings.Contains(matches[0].Summary.Summary, "[oldest") {
		t.Fatalf("matches = %#v", matches)
	}
}

func TestCompactSessionMemorySkipsExistingRollupSummaries(t *testing.T) {
	root := filepath.Join(t.TempDir(), "session-memory")
	for _, item := range []struct {
		id      contracts.ID
		summary string
		updated int64
		trigger string
	}{
		{
			id:      SessionMemoryRollupID,
			summary: "Session memory rollup:\n[archived] default archive notes",
			updated: 250,
			trigger: SessionMemoryRollupTrigger,
		},
		{
			id:      "legacy-rollup",
			summary: "legacy archive notes",
			updated: 200,
			trigger: SessionMemoryRollupTrigger,
		},
		{id: "old", summary: "old normal notes", updated: 100},
		{id: "new", summary: "new active notes", updated: 300},
	} {
		metadata := session.CompactMetadata{}
		if item.trigger != "" {
			metadata.Trigger = item.trigger
		}
		if _, err := WriteSessionSummary(SessionSummaryOptions{
			Root:      root,
			SessionID: item.id,
			Summary:   item.summary,
			UpdatedAt: time.Unix(item.updated, 0).UTC(),
			Metadata:  metadata,
		}); err != nil {
			t.Fatal(err)
		}
	}
	result, err := CompactSessionMemory(root, SessionMemoryCompactionOptions{
		KeepLatest: 1,
		ArchiveID:  "custom-rollup",
		UpdatedAt:  time.Unix(400, 0).UTC(),
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(result.Kept) != 1 || result.Kept[0].SessionID != "new" {
		t.Fatalf("kept = %#v", result.Kept)
	}
	if len(result.Compacted) != 1 || result.Compacted[0].SessionID != "old" {
		t.Fatalf("compacted = %#v", result.Compacted)
	}
	if result.Archive == nil || result.Archive.SessionID != "custom-rollup" {
		t.Fatalf("archive = %#v", result.Archive)
	}
	body := result.Archive.Summary
	if strings.Count(body, "Session memory rollup:") != 1 {
		t.Fatalf("rollup title should not be nested: %q", body)
	}
	for _, want := range []string{"[archived] default archive notes", "legacy archive notes", "[old |"} {
		if !strings.Contains(body, want) {
			t.Fatalf("rollup body missing %q: %q", want, body)
		}
	}
	for _, notWant := range []string{"[session-memory-rollup |", "[legacy-rollup |"} {
		if strings.Contains(body, notWant) {
			t.Fatalf("rollup archive was compacted as a normal summary: %q", body)
		}
	}
	for _, id := range []contracts.ID{SessionMemoryRollupID, "legacy-rollup"} {
		if _, err := os.Stat(filepath.Join(root, string(id), SessionSummaryFilename)); err != nil {
			t.Fatalf("archive summary %s should remain: %v", id, err)
		}
	}
}

func TestBuildSessionMemoryRollupTruncatesAtRuneBoundary(t *testing.T) {
	body := BuildSessionMemoryRollup(nil, []SessionSummary{{
		SessionID: "unicode",
		Summary:   "权限权限权限 compact memory",
		UpdatedAt: time.Unix(100, 0).UTC(),
	}}, 61)
	if !utf8.ValidString(body) {
		t.Fatalf("rollup should remain valid UTF-8: %q", body)
	}
	if len([]rune(body)) > 61 {
		t.Fatalf("rollup length = %d, want <= 61: %q", len([]rune(body)), body)
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

func TestMemoryAgentExtractsFencedFactsFromModelProse(t *testing.T) {
	client := &fakeMemoryClient{response: &anthropic.Response{
		ID:    "msg_memory_fenced",
		Type:  "message",
		Role:  "assistant",
		Model: "sonnet",
		Content: []contracts.ContentBlock{contracts.NewTextBlock(strings.Join([]string{
			"Facts:",
			"```json",
			`{"facts":[{"kind":"decision","text":"keep summaries short","source_uuid":"assistant_1"}]}`,
			"```",
		}, "\n"))},
	}}
	result, err := (Agent{Client: client}).Extract(context.Background(), []contracts.Message{msgs.UserText("Use compact summaries")}, ExtractOptions{})
	if err != nil {
		t.Fatal(err)
	}
	if result.Fallback || len(result.Facts) != 1 || result.Facts[0].Kind != FactDecision || result.Facts[0].Text != "keep summaries short" || result.Facts[0].SourceUUID != "assistant_1" {
		t.Fatalf("result = %#v", result)
	}
}

func TestMemoryAgentExtractsAlternateFactFieldNames(t *testing.T) {
	client := &fakeMemoryClient{response: &anthropic.Response{
		ID:    "msg_memory_alt_fields",
		Type:  "message",
		Role:  "assistant",
		Model: "sonnet",
		Content: []contracts.ContentBlock{contracts.NewTextBlock(`{"memory":[
			{"type":"preference","content":"prefer compact diffs","sourceUuid":"user_1"},
			{"fact_type":"request","summary":"revisit M7 input parity","source_id":"user_2"},
			{"kind":"tool","text":"Used tool Read","uuid":"assistant_1"}
		]}`)},
	}}
	result, err := (Agent{Client: client}).Extract(context.Background(), []contracts.Message{msgs.UserText("Remember prefer compact diffs")}, ExtractOptions{})
	if err != nil {
		t.Fatal(err)
	}
	if result.Fallback || len(result.Facts) != 3 {
		t.Fatalf("result = %#v", result)
	}
	if result.Facts[0].Kind != FactPreference || result.Facts[0].Text != "prefer compact diffs" || result.Facts[0].SourceUUID != "user_1" {
		t.Fatalf("first fact = %#v", result.Facts[0])
	}
	if result.Facts[1].Kind != FactRequest || result.Facts[1].Text != "revisit M7 input parity" || result.Facts[1].SourceUUID != "user_2" {
		t.Fatalf("second fact = %#v", result.Facts[1])
	}
	if result.Facts[2].Kind != FactTool || result.Facts[2].Text != "Used tool Read" || result.Facts[2].SourceUUID != "assistant_1" {
		t.Fatalf("third fact = %#v", result.Facts[2])
	}
}

func TestMemoryAgentExtractsFactKindAliases(t *testing.T) {
	client := &fakeMemoryClient{response: &anthropic.Response{
		ID:    "msg_memory_kind_aliases",
		Type:  "message",
		Role:  "assistant",
		Model: "sonnet",
		Content: []contracts.ContentBlock{contracts.NewTextBlock(`{"facts":[
			{"kind":"Preference","text":"prefer concise updates","source_uuid":"user_1"},
			{"type":"user_request","content":"continue M6 memory parity","source_id":"user_2"},
			{"category":"tool-use","summary":"Used tool Bash","uuid":"assistant_1"},
			{"label":"resolution","detail":"keep recall parser permissive","messageUuid":"assistant_2"}
		]}`)},
	}}
	result, err := (Agent{Client: client}).Extract(context.Background(), []contracts.Message{msgs.UserText("Remember concise updates")}, ExtractOptions{})
	if err != nil {
		t.Fatal(err)
	}
	if result.Fallback || len(result.Facts) != 4 {
		t.Fatalf("result = %#v", result)
	}
	if !hasMemoryFact(result.Facts, FactPreference, "prefer concise updates", "user_1") {
		t.Fatalf("preference alias missing = %#v", result.Facts)
	}
	if !hasMemoryFact(result.Facts, FactRequest, "continue M6 memory parity", "user_2") {
		t.Fatalf("request alias missing = %#v", result.Facts)
	}
	if !hasMemoryFact(result.Facts, FactTool, "Used tool Bash", "assistant_1") {
		t.Fatalf("tool alias missing = %#v", result.Facts)
	}
	if !hasMemoryFact(result.Facts, FactDecision, "keep recall parser permissive", "assistant_2") {
		t.Fatalf("decision alias missing = %#v", result.Facts)
	}
}

func TestMemoryAgentExtractsNestedFactResponseShapes(t *testing.T) {
	client := &fakeMemoryClient{response: &anthropic.Response{
		ID:    "msg_memory_nested_fields",
		Type:  "message",
		Role:  "assistant",
		Model: "sonnet",
		Content: []contracts.ContentBlock{contracts.NewTextBlock(`{
			"extracted_memory":{"facts":[
				{"category":"preference","value":"prefer line-local edits","sourceMessageId":"user_1"}
			]},
			"results":[
				{"label":"decision","detail":"render multiline prompt footer","source":"assistant_1"}
			],
			"memories":[
				{"type":"request","content":"continue M6 parsing","messageUuid":"user_2"}
			]
		}`)},
	}}
	result, err := (Agent{Client: client}).Extract(context.Background(), []contracts.Message{msgs.UserText("Remember line-local edits")}, ExtractOptions{})
	if err != nil {
		t.Fatal(err)
	}
	if result.Fallback || len(result.Facts) != 3 {
		t.Fatalf("result = %#v", result)
	}
	if !hasMemoryFact(result.Facts, FactPreference, "prefer line-local edits", "user_1") {
		t.Fatalf("preference fact missing = %#v", result.Facts)
	}
	if !hasMemoryFact(result.Facts, FactDecision, "render multiline prompt footer", "assistant_1") {
		t.Fatalf("decision fact missing = %#v", result.Facts)
	}
	if !hasMemoryFact(result.Facts, FactRequest, "continue M6 parsing", "user_2") {
		t.Fatalf("request fact missing = %#v", result.Facts)
	}
}

func TestMemoryAgentExtractsNestedFactSourceObjects(t *testing.T) {
	client := &fakeMemoryClient{response: &anthropic.Response{
		ID:    "msg_memory_nested_sources",
		Type:  "message",
		Role:  "assistant",
		Model: "sonnet",
		Content: []contracts.ContentBlock{contracts.NewTextBlock(`{"facts":[
			{"kind":"preference","text":"prefer nested source ids","source":{"uuid":"user_1"}},
			{"type":"decision","content":"keep nested message ids","message":{"id":"assistant_1"}},
			{"factType":"request","summary":"support camel source ids","sourceId":"user_2"},
			{"category":"tool","detail":"read source message object","source_message":{"messageUuid":"assistant_2"}}
		]}`)},
	}}
	result, err := (Agent{Client: client}).Extract(context.Background(), []contracts.Message{msgs.UserText("Remember nested sources")}, ExtractOptions{})
	if err != nil {
		t.Fatal(err)
	}
	if result.Fallback || len(result.Facts) != 4 {
		t.Fatalf("result = %#v", result)
	}
	if !hasMemoryFact(result.Facts, FactPreference, "prefer nested source ids", "user_1") {
		t.Fatalf("nested source fact missing = %#v", result.Facts)
	}
	if !hasMemoryFact(result.Facts, FactDecision, "keep nested message ids", "assistant_1") {
		t.Fatalf("nested message fact missing = %#v", result.Facts)
	}
	if !hasMemoryFact(result.Facts, FactRequest, "support camel source ids", "user_2") {
		t.Fatalf("camel source id fact missing = %#v", result.Facts)
	}
	if !hasMemoryFact(result.Facts, FactTool, "read source message object", "assistant_2") {
		t.Fatalf("nested source message fact missing = %#v", result.Facts)
	}
}

func TestMemoryAgentExtractsStructuredFactText(t *testing.T) {
	client := &fakeMemoryClient{response: &anthropic.Response{
		ID:    "msg_memory_structured_text",
		Type:  "message",
		Role:  "assistant",
		Model: "sonnet",
		Content: []contracts.ContentBlock{contracts.NewTextBlock(`{"facts":[
			{"kind":"preference","content":[{"type":"text","text":"prefer structured memory text"},{"type":"text","text":"when models return content blocks"}],"source_uuid":"user_1"},
			{"type":"decision","text":{"value":"keep nested text object parsing"},"messageUuid":"assistant_1"},
			{"category":"tool","detail":{"content":"Used tool Search"},"sourceId":"assistant_2"}
		]}`)},
	}}
	result, err := (Agent{Client: client}).Extract(context.Background(), []contracts.Message{msgs.UserText("Remember structured text")}, ExtractOptions{})
	if err != nil {
		t.Fatal(err)
	}
	if result.Fallback || len(result.Facts) != 3 {
		t.Fatalf("result = %#v", result)
	}
	if !hasMemoryFact(result.Facts, FactPreference, "prefer structured memory text\nwhen models return content blocks", "user_1") {
		t.Fatalf("structured content fact missing = %#v", result.Facts)
	}
	if !hasMemoryFact(result.Facts, FactDecision, "keep nested text object parsing", "assistant_1") {
		t.Fatalf("nested text fact missing = %#v", result.Facts)
	}
	if !hasMemoryFact(result.Facts, FactTool, "Used tool Search", "assistant_2") {
		t.Fatalf("nested detail fact missing = %#v", result.Facts)
	}
}

func hasMemoryFact(facts []MemoryFact, kind FactKind, text string, source contracts.ID) bool {
	for _, fact := range facts {
		if fact.Kind == kind && fact.Text == text && fact.SourceUUID == source {
			return true
		}
	}
	return false
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

func TestMemoryAgentRecallCanUseModelSelectedSessionIDs(t *testing.T) {
	root := filepath.Join(t.TempDir(), "session-memory")
	if _, err := WriteSessionSummary(SessionSummaryOptions{
		Root:      root,
		SessionID: "older",
		Summary:   "postgres permissions and migration plan",
		UpdatedAt: time.Unix(100, 0).UTC(),
	}); err != nil {
		t.Fatal(err)
	}
	if _, err := WriteSessionSummary(SessionSummaryOptions{
		Root:      root,
		SessionID: "recent",
		Summary:   "database access policy and credential notes",
		UpdatedAt: time.Unix(200, 0).UTC(),
	}); err != nil {
		t.Fatal(err)
	}
	client := &fakeMemoryClient{response: &anthropic.Response{
		ID:      "msg_recall_rank",
		Type:    "message",
		Role:    "assistant",
		Model:   "sonnet",
		Content: []contracts.ContentBlock{contracts.NewTextBlock(`{"query":"database access","session_ids":["older","recent"]}`)},
	}}
	result, err := (Agent{Client: client}).Recall(context.Background(), root, "what did we decide about db access?", RecallOptions{Limit: 2})
	if err != nil {
		t.Fatal(err)
	}
	if result.Fallback || result.Query != "database access" || len(result.SelectedIDs) != 2 {
		t.Fatalf("result = %#v", result)
	}
	if len(result.Matches) != 2 || result.Matches[0].Summary.SessionID != "older" || result.Matches[1].Summary.SessionID != "recent" {
		t.Fatalf("matches = %#v", result.Matches)
	}
	if len(client.requests) != 1 || !strings.Contains(client.requests[0].Messages[0].Content[0].Text, "Candidate session summaries") || !strings.Contains(client.requests[0].Messages[0].Content[0].Text, "older") {
		t.Fatalf("request = %#v", client.requests)
	}
	if !strings.Contains(client.requests[0].Messages[0].Content[0].Text, "Return at most 2 session_ids.") {
		t.Fatalf("request missing recall limit = %q", client.requests[0].Messages[0].Content[0].Text)
	}
}

func TestMemoryAgentRecallParsesAlternateModelResponseKeys(t *testing.T) {
	root := filepath.Join(t.TempDir(), "session-memory")
	if _, err := WriteSessionSummary(SessionSummaryOptions{
		Root:      root,
		SessionID: "prior",
		Summary:   "database access policy notes",
		UpdatedAt: time.Unix(200, 0).UTC(),
	}); err != nil {
		t.Fatal(err)
	}
	if _, err := WriteSessionSummary(SessionSummaryOptions{
		Root:      root,
		SessionID: "other",
		Summary:   "credential rotation notes",
		UpdatedAt: time.Unix(100, 0).UTC(),
	}); err != nil {
		t.Fatal(err)
	}
	client := &fakeMemoryClient{response: &anthropic.Response{
		ID:      "msg_recall_camel",
		Type:    "message",
		Role:    "assistant",
		Model:   "sonnet",
		Content: []contracts.ContentBlock{contracts.NewTextBlock(`{"searchQuery":"database access","selectedIds":["prior"]}`)},
	}}
	result, err := (Agent{Client: client}).Recall(context.Background(), root, "what did we decide about db access?", RecallOptions{Limit: 1})
	if err != nil {
		t.Fatal(err)
	}
	if result.Fallback || result.Query != "database access" || strings.Join(contractIDStrings(result.SelectedIDs), ",") != "prior" {
		t.Fatalf("result = %#v", result)
	}
	if len(result.Matches) != 1 || result.Matches[0].Summary.SessionID != "prior" {
		t.Fatalf("matches = %#v", result.Matches)
	}

	client.response.Content = []contracts.ContentBlock{contracts.NewTextBlock(`{"expandedQuery":"credential rotation","memoryIds":["other"]}`)}
	result, err = (Agent{Client: client}).Recall(context.Background(), root, "what did we decide about credential rotation?", RecallOptions{Limit: 1})
	if err != nil {
		t.Fatal(err)
	}
	if result.Fallback || result.Query != "credential rotation" || strings.Join(contractIDStrings(result.SelectedIDs), ",") != "other" {
		t.Fatalf("memory id alias result = %#v", result)
	}
	if len(result.Matches) != 1 || result.Matches[0].Summary.SessionID != "other" {
		t.Fatalf("memory id alias matches = %#v", result.Matches)
	}
}

func TestMemoryAgentRecallParsesNestedModelSelections(t *testing.T) {
	root := filepath.Join(t.TempDir(), "session-memory")
	for _, item := range []struct {
		id      contracts.ID
		summary string
		updated int64
	}{
		{id: "prior", summary: "database access policy notes", updated: 200},
		{id: "other", summary: "credential rotation notes", updated: 100},
	} {
		if _, err := WriteSessionSummary(SessionSummaryOptions{
			Root:      root,
			SessionID: item.id,
			Summary:   item.summary,
			UpdatedAt: time.Unix(item.updated, 0).UTC(),
		}); err != nil {
			t.Fatal(err)
		}
	}
	client := &fakeMemoryClient{response: &anthropic.Response{
		ID:    "msg_recall_nested",
		Type:  "message",
		Role:  "assistant",
		Model: "sonnet",
		Content: []contracts.ContentBlock{contracts.NewTextBlock(`{
			"query":"database access",
			"matches":[{"session_id":"prior"},{"sessionId":"other"}]
		}`)},
	}}
	result, err := (Agent{Client: client}).Recall(context.Background(), root, "what did we decide about db access?", RecallOptions{Limit: 2})
	if err != nil {
		t.Fatal(err)
	}
	if result.Fallback || result.Query != "database access" || strings.Join(contractIDStrings(result.SelectedIDs), ",") != "prior,other" {
		t.Fatalf("result = %#v", result)
	}
	if len(result.Matches) != 2 || result.Matches[0].Summary.SessionID != "prior" || result.Matches[1].Summary.SessionID != "other" {
		t.Fatalf("matches = %#v", result.Matches)
	}

	client.response.Content = []contracts.ContentBlock{contracts.NewTextBlock(`[{"id":"other"},{"session_id":"prior"}]`)}
	result, err = (Agent{Client: client}).Recall(context.Background(), root, "what did we decide about db access?", RecallOptions{Limit: 2})
	if err != nil {
		t.Fatal(err)
	}
	if result.Fallback || strings.Join(contractIDStrings(result.SelectedIDs), ",") != "other,prior" {
		t.Fatalf("top-level result = %#v", result)
	}
	if len(result.Matches) != 2 || result.Matches[0].Summary.SessionID != "other" || result.Matches[1].Summary.SessionID != "prior" {
		t.Fatalf("top-level matches = %#v", result.Matches)
	}

	client.response.Content = []contracts.ContentBlock{contracts.NewTextBlock(`{
		"rewritten_query":"credential rotation",
		"candidateMemories":[
			{"selectedSession":{"sessionUuid":"other"}},
			{"candidate":{"summaryId":"prior"}}
		]
	}`)}
	result, err = (Agent{Client: client}).Recall(context.Background(), root, "what did we decide about credential rotation?", RecallOptions{Limit: 2})
	if err != nil {
		t.Fatal(err)
	}
	if result.Fallback || result.Query != "credential rotation" || strings.Join(contractIDStrings(result.SelectedIDs), ",") != "other,prior" {
		t.Fatalf("candidate alias result = %#v", result)
	}
	if len(result.Matches) != 2 || result.Matches[0].Summary.SessionID != "other" || result.Matches[1].Summary.SessionID != "prior" {
		t.Fatalf("candidate alias matches = %#v", result.Matches)
	}
}

func TestMemoryAgentRecallParsesWrappedSelectionsAndScalarID(t *testing.T) {
	root := filepath.Join(t.TempDir(), "session-memory")
	for _, item := range []struct {
		id      contracts.ID
		summary string
		updated int64
	}{
		{id: "prior", summary: "database access policy notes", updated: 200},
		{id: "other", summary: "credential rotation notes", updated: 100},
	} {
		if _, err := WriteSessionSummary(SessionSummaryOptions{
			Root:      root,
			SessionID: item.id,
			Summary:   item.summary,
			UpdatedAt: time.Unix(item.updated, 0).UTC(),
		}); err != nil {
			t.Fatal(err)
		}
	}
	client := &fakeMemoryClient{response: &anthropic.Response{
		ID:    "msg_recall_wrapped",
		Type:  "message",
		Role:  "assistant",
		Model: "sonnet",
		Content: []contracts.ContentBlock{contracts.NewTextBlock(`{
			"selection":{
				"search_query":"database access",
				"selected_memories":[
					{"session":{"id":"prior"}},
					{"memory":{"sessionId":"other"}}
				]
			}
		}`)},
	}}
	result, err := (Agent{Client: client}).Recall(context.Background(), root, "what did we decide about db access?", RecallOptions{Limit: 2})
	if err != nil {
		t.Fatal(err)
	}
	if result.Fallback || result.Query != "database access" || strings.Join(contractIDStrings(result.SelectedIDs), ",") != "prior,other" {
		t.Fatalf("wrapped result = %#v", result)
	}
	if len(result.Matches) != 2 || result.Matches[0].Summary.SessionID != "prior" || result.Matches[1].Summary.SessionID != "other" {
		t.Fatalf("wrapped matches = %#v", result.Matches)
	}

	client.response.Content = []contracts.ContentBlock{contracts.NewTextBlock(`"other"`)}
	result, err = (Agent{Client: client}).Recall(context.Background(), root, "what did we decide about credential rotation?", RecallOptions{Limit: 1})
	if err != nil {
		t.Fatal(err)
	}
	if result.Fallback || strings.Join(contractIDStrings(result.SelectedIDs), ",") != "other" {
		t.Fatalf("scalar result = %#v", result)
	}
	if len(result.Matches) != 1 || result.Matches[0].Summary.SessionID != "other" {
		t.Fatalf("scalar matches = %#v", result.Matches)
	}
}

func TestMemoryAgentRecallParsesGraphQLResourceSelections(t *testing.T) {
	root := filepath.Join(t.TempDir(), "session-memory")
	for _, item := range []struct {
		id      contracts.ID
		summary string
		updated int64
	}{
		{id: "prior", summary: "database access policy notes", updated: 200},
		{id: "other", summary: "credential rotation notes", updated: 100},
	} {
		if _, err := WriteSessionSummary(SessionSummaryOptions{
			Root:      root,
			SessionID: item.id,
			Summary:   item.summary,
			UpdatedAt: time.Unix(item.updated, 0).UTC(),
		}); err != nil {
			t.Fatal(err)
		}
	}
	client := &fakeMemoryClient{response: &anthropic.Response{
		ID:    "msg_recall_graphql",
		Type:  "message",
		Role:  "assistant",
		Model: "sonnet",
		Content: []contracts.ContentBlock{contracts.NewTextBlock(`{
			"data":{
				"viewer":{
					"memoryRecall":{
						"searchQuery":"database access",
						"edges":[
							{"node":{"attributes":{"sessionId":"prior"}}},
							{"edge":{"node":{"properties":{"summaryId":"other"}}}}
						]
					}
				}
			}
		}`)},
	}}
	result, err := (Agent{Client: client}).Recall(context.Background(), root, "what did we decide about db access?", RecallOptions{Limit: 2})
	if err != nil {
		t.Fatal(err)
	}
	if result.Fallback || result.Query != "database access" || strings.Join(contractIDStrings(result.SelectedIDs), ",") != "prior,other" {
		t.Fatalf("result = %#v", result)
	}
	if len(result.Matches) != 2 || result.Matches[0].Summary.SessionID != "prior" || result.Matches[1].Summary.SessionID != "other" {
		t.Fatalf("matches = %#v", result.Matches)
	}
}

func TestMemoryAgentRecallParsesIncludedCollections(t *testing.T) {
	root := filepath.Join(t.TempDir(), "session-memory")
	for _, item := range []struct {
		id      contracts.ID
		summary string
		updated int64
	}{
		{id: "prior", summary: "database access policy notes", updated: 200},
		{id: "other", summary: "credential rotation notes", updated: 100},
	} {
		if _, err := WriteSessionSummary(SessionSummaryOptions{
			Root:      root,
			SessionID: item.id,
			Summary:   item.summary,
			UpdatedAt: time.Unix(item.updated, 0).UTC(),
		}); err != nil {
			t.Fatal(err)
		}
	}
	client := &fakeMemoryClient{response: &anthropic.Response{
		ID:    "msg_recall_included",
		Type:  "message",
		Role:  "assistant",
		Model: "sonnet",
		Content: []contracts.ContentBlock{contracts.NewTextBlock(`{
			"data":{
				"collection":{
					"query":"database access",
					"included":[
						{"type":"tool","id":"tool_1","attributes":{"name":"Read"}},
						{"type":"session-memory","id":"prior"},
						{"resource":{"type":"session-memory","properties":{"summaryId":"other"}}}
					]
				}
			}
		}`)},
	}}
	result, err := (Agent{Client: client}).Recall(context.Background(), root, "what did we decide about db access?", RecallOptions{Limit: 2})
	if err != nil {
		t.Fatal(err)
	}
	if result.Fallback || result.Query != "database access" || strings.Join(contractIDStrings(result.SelectedIDs), ",") != "prior,other" {
		t.Fatalf("result = %#v", result)
	}
	if len(result.Matches) != 2 || result.Matches[0].Summary.SessionID != "prior" || result.Matches[1].Summary.SessionID != "other" {
		t.Fatalf("matches = %#v", result.Matches)
	}
}

func TestMemoryAgentRecallExtractsFencedJSONFromModelProse(t *testing.T) {
	root := filepath.Join(t.TempDir(), "session-memory")
	if _, err := WriteSessionSummary(SessionSummaryOptions{
		Root:      root,
		SessionID: "prior",
		Summary:   "database access policy notes",
		UpdatedAt: time.Unix(200, 0).UTC(),
	}); err != nil {
		t.Fatal(err)
	}
	client := &fakeMemoryClient{response: &anthropic.Response{
		ID:    "msg_recall_fenced",
		Type:  "message",
		Role:  "assistant",
		Model: "sonnet",
		Content: []contracts.ContentBlock{contracts.NewTextBlock(strings.Join([]string{
			"Selected memory:",
			"```json",
			`{"search_query":"database access","selected_session_id":"prior"}`,
			"```",
		}, "\n"))},
	}}
	result, err := (Agent{Client: client}).Recall(context.Background(), root, "what did we decide about db access?", RecallOptions{Limit: 1})
	if err != nil {
		t.Fatal(err)
	}
	if result.Fallback || result.Query != "database access" || strings.Join(contractIDStrings(result.SelectedIDs), ",") != "prior" {
		t.Fatalf("result = %#v", result)
	}
	if len(result.Matches) != 1 || result.Matches[0].Summary.SessionID != "prior" {
		t.Fatalf("matches = %#v", result.Matches)
	}
}

func TestMemoryAgentRecallFallsBackWhenModelSelectsNoValidSessions(t *testing.T) {
	root := filepath.Join(t.TempDir(), "session-memory")
	if _, err := WriteSessionSummary(SessionSummaryOptions{
		Root:      root,
		SessionID: "current",
		Summary:   "current database notes",
		UpdatedAt: time.Unix(300, 0).UTC(),
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
	client := &fakeMemoryClient{response: &anthropic.Response{
		ID:      "msg_recall_empty",
		Type:    "message",
		Role:    "assistant",
		Model:   "sonnet",
		Content: []contracts.ContentBlock{contracts.NewTextBlock(`{"query":"postgres permissions","session_ids":["missing","current"]}`)},
	}}
	result, err := (Agent{Client: client}).Recall(context.Background(), root, "what did we decide about db access?", RecallOptions{Limit: 1, ExcludeSessionID: "current"})
	if err != nil {
		t.Fatal(err)
	}
	if !result.Fallback || result.Query != "postgres permissions" || len(result.SelectedIDs) != 2 {
		t.Fatalf("result = %#v", result)
	}
	if len(result.Matches) != 1 || result.Matches[0].Summary.SessionID != "prior" {
		t.Fatalf("matches = %#v", result.Matches)
	}
	if len(client.requests) != 1 || !strings.Contains(client.requests[0].Messages[0].Content[0].Text, "Do not select excluded current session id: current") {
		t.Fatalf("request missing exclude session = %#v", client.requests)
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
	contextMessages := context.ContextMessages()
	if len(contextMessages) != 2 || contextMessages[0].Subtype != CurrentSessionContextSubtype || contextMessages[1].Subtype != RecallContextSubtype {
		t.Fatalf("context messages = %#v", contextMessages)
	}
	if !contextMessages[0].IsMeta || !strings.Contains(contextMessages[0].Content[0].Text, "current session summary") {
		t.Fatalf("current context message = %#v", contextMessages[0])
	}
	withContext := context.MessagesWithContext()
	if len(withContext) != 4 || withContext[0].Subtype != CurrentSessionContextSubtype || withContext[2].UUID != "u1" {
		t.Fatalf("messages with context = %#v", withContext)
	}
}

func TestBuildResumeContextCanUseRecallAgent(t *testing.T) {
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
		Summary:   "database access policy notes",
		UpdatedAt: time.Unix(200, 0).UTC(),
	}); err != nil {
		t.Fatal(err)
	}
	client := &fakeMemoryClient{response: &anthropic.Response{
		ID:      "msg_resume_recall",
		Type:    "message",
		Role:    "assistant",
		Model:   "sonnet",
		Content: []contracts.ContentBlock{contracts.NewTextBlock(`{"query":"database access","session_ids":["prior"]}`)},
	}}
	agent := Agent{Client: client}
	resumeContext, err := BuildResumeContext(ResumeContextOptions{
		SessionPath: sessionPath,
		SessionID:   "current",
		MemoryRoot:  root,
		RecallLimit: 2,
		RecallAgent: &agent,
		Context:     context.Background(),
	})
	if err != nil {
		t.Fatal(err)
	}
	if resumeContext.RecallFallback || resumeContext.RecallQuery != "database access" || strings.Join(contractIDStrings(resumeContext.RecallSelectedIDs), ",") != "prior" {
		t.Fatalf("recall metadata = %#v", resumeContext)
	}
	if len(resumeContext.Recalled) != 1 || resumeContext.Recalled[0].Summary.SessionID != "prior" {
		t.Fatalf("recalled = %#v", resumeContext.Recalled)
	}
	if len(client.requests) != 1 || !strings.Contains(client.requests[0].Messages[0].Content[0].Text, "Candidate session summaries") {
		t.Fatalf("request = %#v", client.requests)
	}
}

func sessionCompactMetadata(trigger string, preTokens int, summarized int) session.CompactMetadata {
	return session.CompactMetadata{Trigger: trigger, PreTokens: preTokens, MessagesSummarized: summarized}
}

func contractIDStrings(ids []contracts.ID) []string {
	out := make([]string, 0, len(ids))
	for _, id := range ids {
		out = append(out, string(id))
	}
	return out
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
