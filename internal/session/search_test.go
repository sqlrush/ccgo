package session

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"ccgo/internal/contracts"
)

func TestListProjectSessionsSortsAndBuildsTitles(t *testing.T) {
	t.Setenv("CLAUDE_CONFIG_DIR", t.TempDir())
	root := "/repo/project"
	dir := ProjectDir(root)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	first := filepath.Join(dir, "sess_1.jsonl")
	second := filepath.Join(dir, "sess_2.jsonl")
	writeRawSession(t, first, []string{
		`{"type":"user","uuid":"u1","sessionId":"sess_1","message":{"type":"user","content":[{"type":"text","text":"first title from prompt"}]}}`,
	})
	writeRawSession(t, second, []string{
		`{"type":"custom-title","sessionId":"sess_2","customTitle":"Custom Title"}`,
		`{"type":"user","uuid":"u2","sessionId":"sess_2","gitBranch":"feature/session-index","message":{"type":"user","content":[{"type":"text","text":"second prompt"}]}}`,
	})
	if err := os.Chtimes(first, time.Unix(10, 0), time.Unix(10, 0)); err != nil {
		t.Fatal(err)
	}
	if err := os.Chtimes(second, time.Unix(20, 0), time.Unix(20, 0)); err != nil {
		t.Fatal(err)
	}

	sessions, err := ListProjectSessions(root)
	if err != nil {
		t.Fatal(err)
	}
	if len(sessions) != 2 {
		t.Fatalf("sessions = %#v", sessions)
	}
	if sessions[0].ID != "sess_2" || sessions[0].Title != "Custom Title" {
		t.Fatalf("first session = %#v", sessions[0])
	}
	if sessions[0].GitBranch != "feature/session-index" {
		t.Fatalf("first session branch = %#v", sessions[0])
	}
	if sessions[1].Title != "first title from prompt" {
		t.Fatalf("second title = %#v", sessions[1])
	}
	page, err := ListProjectSessionsPage(root, 0, 1)
	if err != nil {
		t.Fatal(err)
	}
	if page.Total != 2 || !page.HasMore || len(page.Sessions) != 1 || page.Sessions[0].ID != "sess_2" {
		t.Fatalf("page = %#v", page)
	}
}

func TestLoadTranscriptIndexSummarizesWithoutFullTranscript(t *testing.T) {
	path := writeTranscript(t, []string{
		`{"type":"summary","leafUuid":"a1","summary":"summary title"}`,
		`{"type":"ai-title","sessionId":"sess_1","aiTitle":"AI Title"}`,
		`{"type":"custom-title","sessionId":"sess_1","customTitle":"Custom Title"}`,
		`{"type":"last-prompt","sessionId":"sess_1","lastPrompt":"last prompt metadata"}`,
		`{"type":"task-summary","sessionId":"sess_1","summary":"running checks","timestamp":"2026-01-01T00:00:03Z"}`,
		`{"type":"tag","sessionId":"sess_1","tag":"ship"}`,
		`{"type":"agent-name","sessionId":"sess_1","agentName":"Builder"}`,
		`{"type":"agent-color","sessionId":"sess_1","agentColor":"green"}`,
		`{"type":"agent-setting","sessionId":"sess_1","agentSetting":"general-purpose"}`,
		`{"type":"pr-link","sessionId":"sess_1","prNumber":7,"prUrl":"https://github.com/o/r/pull/7","prRepository":"o/r","timestamp":"2026-01-01T00:00:04Z"}`,
		`{"type":"mode","sessionId":"sess_1","mode":"coordinator"}`,
		`{"type":"worktree-state","sessionId":"sess_1","worktreeSession":{"worktreePath":"/tmp/wt","sessionId":"sess_1"}}`,
		`{"type":"user","uuid":"u1","sessionId":"sess_1","timestamp":"2026-01-01T00:00:00Z","message":{"type":"user","content":[{"type":"text","text":"first prompt"}]}}`,
		`{"type":"assistant","uuid":"a1","parentUuid":"u1","sessionId":"sess_1","gitBranch":"main","timestamp":"2026-01-01T00:00:01Z","message":{"type":"assistant","content":[{"type":"text","text":"done"}]}}`,
		`{"type":"user","uuid":"u2","parentUuid":"a1","sessionId":"sess_1","gitBranch":"feature/m6-index","timestamp":"2026-01-01T00:00:02Z","message":{"type":"user","content":[{"type":"text","text":"last prompt"}]}}`,
		`{"type":"content-replacement","sessionId":"sess_1","replacements":[{"replacement":"stub"},{"replacement":"stub2"}]}`,
	})
	index, err := LoadTranscriptIndex(path, "sess_1")
	if err != nil {
		t.Fatal(err)
	}
	if index.Title != "Custom Title" || index.MessageCount != 3 || index.UserMessageCount != 2 || index.AssistantMessageCount != 1 {
		t.Fatalf("index = %#v", index)
	}
	if index.FirstUUID != "u1" || index.LastUUID != "u2" || index.FirstUserText != "first prompt" || index.LastUserText != "last prompt" || index.LastAssistantText != "done" {
		t.Fatalf("index messages = %#v", index)
	}
	if index.TextBytes != len("first prompt")+len("done")+len("last prompt") {
		t.Fatalf("text bytes = %d", index.TextBytes)
	}
	if index.SummaryCount != 1 || index.ContentReplacementCount != 2 {
		t.Fatalf("index metadata = %#v", index)
	}
	if index.AITitle != "AI Title" || index.LastPrompt != "last prompt metadata" || index.TaskSummary != "running checks" {
		t.Fatalf("index title/task metadata = %#v", index)
	}
	if index.Tag != "ship" || index.AgentName != "Builder" || index.AgentColor != "green" || index.AgentSetting != "general-purpose" {
		t.Fatalf("index agent metadata = %#v", index)
	}
	if index.PRNumber != 7 || index.PRRepository != "o/r" || index.Mode != "coordinator" || !index.HasWorktreeState {
		t.Fatalf("index session metadata = %#v", index)
	}
	if index.GitBranch != "feature/m6-index" {
		t.Fatalf("index branch = %#v", index)
	}
}

func TestLoadTranscriptIndexAcceptsMetadataAliases(t *testing.T) {
	path := writeTranscript(t, []string{
		`{"type":"summary","leaf_uuid":"a1","summary":"summary alias"}`,
		`{"type":"custom_title","session_id":"sess_1","custom_title":"Custom Alias"}`,
		`{"type":"ai_title","session_id":"sess_1","ai_title":"AI Alias"}`,
		`{"type":"last_prompt","session_id":"sess_1","last_prompt":"last prompt alias"}`,
		`{"type":"task_summary","session_id":"sess_1","summary":"task alias","timestamp":"2026-01-01T00:00:03Z"}`,
		`{"type":"tag","session_id":"sess_1","tag":"alias-tag"}`,
		`{"type":"agent_name","session_id":"sess_1","agent_name":"Alias Builder"}`,
		`{"type":"agent_color","session_id":"sess_1","agent_color":"orange"}`,
		`{"type":"agent_setting","session_id":"sess_1","agent_setting":"reviewer"}`,
		`{"type":"pr_link","session_id":"sess_1","pr_number":8,"pr_url":"https://github.com/o/r/pull/8","pr_repository":"o/r","timestamp":"2026-01-01T00:00:04Z"}`,
		`{"type":"mode","session_id":"sess_1","mode":"worker"}`,
		`{"type":"worktree_state","session_id":"sess_1","worktree_session":{"worktreePath":"/tmp/wt","sessionId":"sess_1"}}`,
		`{"type":"user","message_id":"u_alias","session_id":"sess_1","git_branch":"feature/alias-branch","message":{"type":"user","content":[{"type":"text","text":"alias prompt"}]}}`,
		`{"type":"content_replacement","session_id":"sess_1","replacements":[{"replacement":"stub"}]}`,
	})
	index, err := LoadTranscriptIndex(path, "sess_1")
	if err != nil {
		t.Fatal(err)
	}
	if index.Title != "Custom Alias" || index.SummaryCount != 1 || index.ContentReplacementCount != 1 {
		t.Fatalf("alias index = %#v", index)
	}
	if index.AITitle != "AI Alias" || index.LastPrompt != "last prompt alias" || index.TaskSummary != "task alias" {
		t.Fatalf("alias index title/task metadata = %#v", index)
	}
	if index.Tag != "alias-tag" || index.AgentName != "Alias Builder" || index.AgentColor != "orange" || index.AgentSetting != "reviewer" {
		t.Fatalf("alias index agent metadata = %#v", index)
	}
	if index.PRNumber != 8 || index.PRRepository != "o/r" || index.PRURL == "" || index.Mode != "worker" || !index.HasWorktreeState {
		t.Fatalf("alias index session metadata = %#v", index)
	}
	if index.GitBranch != "feature/alias-branch" {
		t.Fatalf("alias index branch = %#v", index)
	}
}

func TestLoadTranscriptIndexUsesAITitleAndLastPromptFallbacks(t *testing.T) {
	aiTitlePath := writeTranscript(t, []string{
		`{"type":"summary","leafUuid":"a1","summary":"summary title"}`,
		`{"type":"ai-title","sessionId":"sess_1","aiTitle":"Generated Title"}`,
		`{"type":"user","uuid":"u1","sessionId":"sess_1","message":{"type":"user","content":[{"type":"text","text":"first prompt"}]}}`,
	})
	aiTitle, err := LoadTranscriptIndex(aiTitlePath, "sess_1")
	if err != nil {
		t.Fatal(err)
	}
	if aiTitle.Title != "Generated Title" {
		t.Fatalf("ai title index = %#v", aiTitle)
	}

	lastPromptPath := writeTranscript(t, []string{
		`{"type":"summary","leafUuid":"a1","summary":"summary title"}`,
		`{"type":"last-prompt","sessionId":"sess_2","lastPrompt":"resume from prompt"}`,
	})
	lastPrompt, err := LoadTranscriptIndex(lastPromptPath, "sess_2")
	if err != nil {
		t.Fatal(err)
	}
	if lastPrompt.Title != "resume from prompt" {
		t.Fatalf("last prompt index = %#v", lastPrompt)
	}
}

func TestSearchTranscriptFileStreamsMatches(t *testing.T) {
	path := writeTranscript(t, []string{
		`{malformed`,
		`{"type":"summary","leafUuid":"a1","summary":"compact summary"}`,
		`{"type":"user","uuid":"u1","sessionId":"sess_1","message":{"type":"user","content":[{"type":"text","text":"alpha compact memory support"}]}}`,
		`{"type":"assistant","uuid":"a1","parentUuid":"u1","sessionId":"sess_1","message":{"type":"assistant","content":[{"type":"text","text":"compact done"}]}}`,
		`{"type":"user","uuid":"u2","parentUuid":"a1","sessionId":"sess_1","message":{"type":"user","content":[{"type":"text","text":"compact followup"}]}}`,
	})
	matches, err := SearchTranscriptFile(path, "compact", 2)
	if err != nil {
		t.Fatal(err)
	}
	if len(matches) != 2 || !strings.Contains(matches[0], "alpha compact") || !strings.Contains(matches[1], "compact done") {
		t.Fatalf("matches = %#v", matches)
	}
}

func TestSearchProjectSessionsFindsTranscriptText(t *testing.T) {
	t.Setenv("CLAUDE_CONFIG_DIR", t.TempDir())
	root := "/repo/project"
	dir := ProjectDir(root)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	writeRawSession(t, filepath.Join(dir, "sess_1.jsonl"), []string{
		`{"type":"user","uuid":"u1","sessionId":"sess_1","message":{"type":"user","content":[{"type":"text","text":"implement compact memory support"}]}}`,
		`{"type":"assistant","uuid":"a1","parentUuid":"u1","sessionId":"sess_1","message":{"type":"assistant","content":[{"type":"text","text":"done"}]}}`,
	})
	writeRawSession(t, filepath.Join(dir, "sess_2.jsonl"), []string{
		`{"type":"user","uuid":"u2","sessionId":"sess_2","message":{"type":"user","content":[{"type":"text","text":"unrelated"}]}}`,
	})

	results, err := SearchProjectSessions(root, "compact", 10)
	if err != nil {
		t.Fatal(err)
	}
	if len(results) != 1 || results[0].ID != contracts.ID("sess_1") {
		t.Fatalf("results = %#v", results)
	}
	if len(results[0].Matches) != 1 || !strings.Contains(results[0].Matches[0], "compact memory") {
		t.Fatalf("matches = %#v", results[0].Matches)
	}
}

func TestSearchProjectSessionsFindsGitBranch(t *testing.T) {
	t.Setenv("CLAUDE_CONFIG_DIR", t.TempDir())
	root := "/repo/project"
	dir := ProjectDir(root)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	writeRawSession(t, filepath.Join(dir, "sess_1.jsonl"), []string{
		`{"type":"user","uuid":"u1","sessionId":"sess_1","gitBranch":"feature/resume-branch","message":{"type":"user","content":[{"type":"text","text":"ordinary prompt"}]}}`,
	})
	writeRawSession(t, filepath.Join(dir, "sess_2.jsonl"), []string{
		`{"type":"user","uuid":"u2","sessionId":"sess_2","message":{"type":"user","content":[{"type":"text","text":"unrelated"}]}}`,
	})

	results, err := SearchProjectSessions(root, "resume-branch", 10)
	if err != nil {
		t.Fatal(err)
	}
	if len(results) != 1 || results[0].ID != contracts.ID("sess_1") || results[0].GitBranch != "feature/resume-branch" {
		t.Fatalf("branch results = %#v", results)
	}
	if len(results[0].Matches) != 0 {
		t.Fatalf("branch search should not require text matches: %#v", results[0].Matches)
	}
}

func writeRawSession(t *testing.T, path string, lines []string) {
	t.Helper()
	if err := os.WriteFile(path, []byte(strings.Join(lines, "\n")+"\n"), 0o600); err != nil {
		t.Fatal(err)
	}
}
