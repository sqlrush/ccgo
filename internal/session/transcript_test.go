package session

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestLoadTranscriptBridgesLegacyProgressEntries(t *testing.T) {
	path := writeTranscript(t, []string{
		`{"type":"user","uuid":"u1","parentUuid":null,"timestamp":"2026-01-01T00:00:00Z"}`,
		`{"type":"progress","uuid":"p1","parentUuid":"u1"}`,
		`{"type":"progress","uuid":"p2","parentUuid":"p1"}`,
		`{"type":"assistant","uuid":"a1","parentUuid":"p2","timestamp":"2026-01-01T00:00:01Z"}`,
	})

	transcript, err := LoadTranscript(path)
	if err != nil {
		t.Fatal(err)
	}
	if got := transcript.Messages["a1"].ParentUUID; got == nil || *got != "u1" {
		t.Fatalf("assistant parent = %#v", got)
	}
	chain := transcript.BuildConversationChain("a1")
	if got := chainIDs(chain); strings.Join(got, ",") != "u1,a1" {
		t.Fatalf("chain = %#v", got)
	}
	if _, ok := transcript.LeafUUIDs["a1"]; !ok {
		t.Fatalf("leaf UUIDs = %#v", transcript.LeafUUIDs)
	}
}

func TestLoadTranscriptPrunesBeforeCompactBoundary(t *testing.T) {
	path := writeTranscript(t, []string{
		`{"type":"user","uuid":"u1","parentUuid":null}`,
		`{"type":"assistant","uuid":"a1","parentUuid":"u1"}`,
		`{"type":"marble-origami-commit","sessionId":"s1","collapseId":"0000000000000001","summaryUuid":"sum1","summary":"old","firstArchivedUuid":"u1","lastArchivedUuid":"a1"}`,
		`{"type":"system","subtype":"compact_boundary","uuid":"cb1","parentUuid":null,"compactMetadata":{"trigger":"manual","preTokens":100}}`,
		`{"type":"user","uuid":"u2","parentUuid":"cb1"}`,
		`{"type":"assistant","uuid":"a2","parentUuid":"u2"}`,
	})

	transcript, err := LoadTranscript(path)
	if err != nil {
		t.Fatal(err)
	}
	if _, ok := transcript.Messages["u1"]; ok {
		t.Fatalf("pre-boundary message u1 was not pruned")
	}
	if _, ok := transcript.Messages["a1"]; ok {
		t.Fatalf("pre-boundary message a1 was not pruned")
	}
	if len(transcript.ContextCollapseCommits) != 0 {
		t.Fatalf("stale collapse commits = %#v", transcript.ContextCollapseCommits)
	}
	chain := transcript.BuildConversationChain("a2")
	if got := chainIDs(chain); strings.Join(got, ",") != "cb1,u2,a2" {
		t.Fatalf("chain = %#v", got)
	}
}

func TestLoadTranscriptAppliesSnipRemovalAndRelinks(t *testing.T) {
	path := writeTranscript(t, []string{
		`{"type":"user","uuid":"u1","parentUuid":null}`,
		`{"type":"assistant","uuid":"a1","parentUuid":"u1"}`,
		`{"type":"user","uuid":"u2","parentUuid":"a1"}`,
		`{"type":"system","uuid":"s1","parentUuid":"u2","snipMetadata":{"removedUuids":["a1"]}}`,
	})

	transcript, err := LoadTranscript(path)
	if err != nil {
		t.Fatal(err)
	}
	if _, ok := transcript.Messages["a1"]; ok {
		t.Fatalf("snipped message still present")
	}
	if got := transcript.Messages["u2"].ParentUUID; got == nil || *got != "u1" {
		t.Fatalf("u2 parent = %#v", got)
	}
	chain := transcript.BuildConversationChain("u2")
	if got := chainIDs(chain); strings.Join(got, ",") != "u1,u2" {
		t.Fatalf("chain = %#v", got)
	}
}

func TestBuildConversationChainRecoversOrphanedParallelToolResults(t *testing.T) {
	path := writeTranscript(t, []string{
		`{"type":"user","uuid":"u1","parentUuid":null,"timestamp":"2026-01-01T00:00:00Z","message":{"type":"user","content":[{"type":"text","text":"run both"}]}}`,
		`{"type":"assistant","uuid":"a1","parentUuid":"u1","timestamp":"2026-01-01T00:00:01Z","message":{"id":"msg_parallel","type":"assistant","content":[{"type":"tool_use","id":"toolu_1","name":"Read"}]}}`,
		`{"type":"assistant","uuid":"a2","parentUuid":"a1","timestamp":"2026-01-01T00:00:02Z","message":{"id":"msg_parallel","type":"assistant","content":[{"type":"tool_use","id":"toolu_2","name":"Grep"}]}}`,
		`{"type":"user","uuid":"tr1","parentUuid":"a1","timestamp":"2026-01-01T00:00:03Z","message":{"type":"user","content":[{"type":"tool_result","tool_use_id":"toolu_1","content":"read ok"}]}}`,
		`{"type":"user","uuid":"tr2","parentUuid":"a2","timestamp":"2026-01-01T00:00:04Z","message":{"type":"user","content":[{"type":"tool_result","tool_use_id":"toolu_2","content":"grep ok"}]}}`,
		`{"type":"user","uuid":"u2","parentUuid":"tr1","timestamp":"2026-01-01T00:00:05Z","message":{"type":"user","content":[{"type":"text","text":"next"}]}}`,
	})

	transcript, err := LoadTranscript(path)
	if err != nil {
		t.Fatal(err)
	}
	chain := transcript.BuildConversationChain("u2")
	if got := chainIDs(chain); strings.Join(got, ",") != "u1,a1,a2,tr2,tr1,u2" {
		t.Fatalf("chain = %#v", got)
	}
}

func TestBuildResumeConversationConvertsTranscriptChain(t *testing.T) {
	path := writeTranscript(t, []string{
		`{"type":"user","uuid":"u1","sessionId":"s1","parentUuid":null,"timestamp":"2026-01-01T00:00:00Z","message":{"type":"user","uuid":"u1","sessionId":"s1","content":[{"type":"text","text":"hello"}]}}`,
		`{"type":"assistant","uuid":"a1","sessionId":"s1","parentUuid":"u1","timestamp":"2026-01-01T00:00:01Z","message":{"id":"msg_1","type":"assistant","content":[{"type":"text","text":"hi"}]}}`,
		`{"type":"user","uuid":"u2","sessionId":"s1","parentUuid":"a1","timestamp":"2026-01-01T00:00:02Z","content":[{"type":"text","text":"continue"}]}`,
	})
	resume, err := BuildResumeConversation(path, "u2")
	if err != nil {
		t.Fatal(err)
	}
	if !resume.Found || resume.Leaf != "u2" || strings.Join(chainIDs(resume.Chain), ",") != "u1,a1,u2" {
		t.Fatalf("resume = %#v", resume)
	}
	if len(resume.Messages) != 3 {
		t.Fatalf("messages = %#v", resume.Messages)
	}
	if resume.Messages[0].Type != "user" || resume.Messages[0].UUID != "u1" || resume.Messages[0].Content[0].Text != "hello" {
		t.Fatalf("first message = %#v", resume.Messages[0])
	}
	if resume.Messages[1].Type != "assistant" || resume.Messages[1].ID != "msg_1" || resume.Messages[1].ParentUUID == nil || *resume.Messages[1].ParentUUID != "u1" {
		t.Fatalf("assistant = %#v", resume.Messages[1])
	}
	if resume.Messages[2].Type != "user" || resume.Messages[2].Content[0].Text != "continue" || resume.Messages[2].ParentUUID == nil || *resume.Messages[2].ParentUUID != "a1" {
		t.Fatalf("last message = %#v", resume.Messages[2])
	}
}

func TestBuildResumeConversationUsesLatestLeaf(t *testing.T) {
	path := writeTranscript(t, []string{
		`{"type":"user","uuid":"u1","parentUuid":null,"message":{"type":"user","content":[{"type":"text","text":"first"}]}}`,
		`{"type":"assistant","uuid":"a1","parentUuid":"u1","message":{"type":"assistant","content":[{"type":"text","text":"ok"}]}}`,
		`{"type":"user","uuid":"u2","parentUuid":"a1","message":{"type":"user","content":[{"type":"text","text":"latest"}]}}`,
	})
	resume, err := BuildResumeConversation(path, "")
	if err != nil {
		t.Fatal(err)
	}
	if !resume.Found || resume.Leaf != "u2" || len(resume.Messages) != 3 {
		t.Fatalf("resume = %#v", resume)
	}
	if resume.Messages[2].Content[0].Text != "latest" {
		t.Fatalf("latest message = %#v", resume.Messages[2])
	}
}

func TestBuildIndexedResumeConversationUsesLineIndex(t *testing.T) {
	lines := []string{
		`{"type":"user","uuid":"u1","sessionId":"s1","parentUuid":null,"timestamp":"2026-01-01T00:00:00Z","message":{"type":"user","content":[{"type":"text","text":"hello"}]}}`,
		`{"type":"progress","uuid":"p1","parentUuid":"u1"}`,
		`{"type":"assistant","uuid":"a1","sessionId":"s1","parentUuid":"p1","timestamp":"2026-01-01T00:00:01Z","message":{"id":"msg_1","type":"assistant","content":[{"type":"text","text":"hi"}]}}`,
		`{"type":"user","uuid":"u2","sessionId":"s1","parentUuid":"a1","timestamp":"2026-01-01T00:00:02Z","message":{"type":"user","content":[{"type":"text","text":"latest"}]}}`,
	}
	path := writeTranscript(t, lines)
	resume, err := BuildIndexedResumeConversation(path, "", 0)
	if err != nil {
		t.Fatal(err)
	}
	if !resume.Found || resume.Leaf != "u2" || strings.Join(chainIDs(resume.Chain), ",") != "u1,a1,u2" {
		t.Fatalf("indexed resume = %#v chain=%#v", resume, chainIDs(resume.Chain))
	}
	if len(resume.Messages) != 3 || resume.Messages[2].Content[0].Text != "latest" {
		t.Fatalf("indexed resume messages = %#v", resume.Messages)
	}
	if resume.Chain[1].ParentUUID == nil || *resume.Chain[1].ParentUUID != "u1" {
		t.Fatalf("indexed resume should bridge progress parent: %#v", resume.Chain[1].ParentUUID)
	}

	budget := int64(len(lines[2]) + 1 + len(lines[3]) + 1)
	resume, err = BuildIndexedResumeConversation(path, "u2", budget)
	if err != nil {
		t.Fatal(err)
	}
	if !resume.Found || !resume.HasBefore || resume.BytesRead > budget || strings.Join(chainIDs(resume.Chain), ",") != "a1,u2" {
		t.Fatalf("budgeted indexed resume = %#v chain=%#v budget=%d", resume, chainIDs(resume.Chain), budget)
	}
}

func TestLoadTranscriptCollectsMetadataEntries(t *testing.T) {
	path := writeTranscript(t, []string{
		`{"type":"summary","leafUuid":"a1","summary":"short"}`,
		`{"type":"summary","leaf_uuid":"a2","summary":"snake short"}`,
		`{"type":"custom-title","sessionId":"s1","customTitle":"Title"}`,
		`{"type":"custom_title","session_id":"s3","custom_title":"Snake Title"}`,
		`{"type":"ai-title","sessionId":"s1","aiTitle":"AI Title"}`,
		`{"type":"ai_title","sessionId":"s2","aiTitle":"Alias AI Title"}`,
		`{"type":"ai-title","session_id":"s3","ai_title":"Snake AI Title"}`,
		`{"type":"last-prompt","sessionId":"s1","lastPrompt":"last prompt text"}`,
		`{"type":"last_prompt","session_id":"s3","last_prompt":"snake prompt"}`,
		`{"type":"task-summary","sessionId":"s1","summary":"running tests","timestamp":"2026-01-01T00:00:03Z"}`,
		`{"type":"task_summary","session_id":"s3","summary":"snake task","timestamp":"2026-01-01T00:00:03Z"}`,
		`{"type":"tag","sessionId":"s1","tag":"tagged"}`,
		`{"type":"tag","session_id":"s3","tag":"snake-tag"}`,
		`{"type":"agent-name","sessionId":"s1","agentName":"Builder"}`,
		`{"type":"agent_name","session_id":"s3","agent_name":"Snake Builder"}`,
		`{"type":"agent-color","sessionId":"s1","agentColor":"blue"}`,
		`{"type":"agent_color","session_id":"s3","agent_color":"violet"}`,
		`{"type":"agent-setting","sessionId":"s1","agentSetting":"reviewer"}`,
		`{"type":"agent_setting","session_id":"s3","agent_setting":"planner"}`,
		`{"type":"pr-link","sessionId":"s1","prNumber":42,"prUrl":"https://github.com/o/r/pull/42","prRepository":"o/r","timestamp":"2026-01-01T00:00:04Z"}`,
		`{"type":"pr_link","session_id":"s3","pr_number":43,"pr_url":"https://github.com/o/r/pull/43","pr_repository":"o/r","timestamp":"2026-01-01T00:00:04Z"}`,
		`{"type":"mode","sessionId":"s1","mode":"coordinator"}`,
		`{"type":"mode","session_id":"s3","mode":"worker"}`,
		`{"type":"worktree-state","sessionId":"s1","worktreeSession":{"worktreePath":"/tmp/wt","sessionId":"s1"}}`,
		`{"type":"worktree_state","session_id":"s3","worktree_session":{"worktreePath":"/tmp/wt2","sessionId":"s3"}}`,
		`{"type":"file-history-snapshot","messageId":"a1","snapshot":{"files":[]},"isSnapshotUpdate":false}`,
		`{"type":"attribution-snapshot","messageId":"a1","surface":"cli","fileStates":{}}`,
		`{"type":"speculation-accept","timestamp":"2026-01-01T00:00:05Z","timeSavedMs":1200}`,
		`{"type":"speculation_accept","timestamp":"2026-01-01T00:00:05Z","time_saved_ms":3400}`,
		`{"type":"content-replacement","sessionId":"s1","replacements":[{"toolUseId":"toolu_1","replacement":"stub"}]}`,
		`{"type":"content_replacement","sessionId":"s2","replacements":[{"toolUseId":"toolu_3","replacement":"alias stub"}]}`,
		`{"type":"content-replacement","sessionId":"s1","agentId":"agent_1","replacements":[{"toolUseId":"toolu_2","replacement":"agent stub"}]}`,
		`{"type":"content_replacement","session_id":"s3","agent_id":"agent_2","replacements":[{"toolUseId":"toolu_4","replacement":"snake agent stub"}]}`,
		`{"type":"marble_origami_commit","session_id":"s3","collapse_id":"c1","summary_uuid":"sum1","summary_content":"collapsed","summary":"short","first_archived_uuid":"u1","last_archived_uuid":"a1"}`,
		`{"type":"marble-origami-snapshot","sessionId":"s1","armed":true,"lastSpawnTokens":42}`,
		`{"type":"marble_origami_snapshot","session_id":"s3","armed":true,"last_spawn_tokens":64}`,
	})
	transcript, err := LoadTranscript(path)
	if err != nil {
		t.Fatal(err)
	}
	if transcript.Summaries["a1"] != "short" || transcript.CustomTitles["s1"] != "Title" || transcript.Tags["s1"] != "tagged" {
		t.Fatalf("metadata = %#v %#v %#v", transcript.Summaries, transcript.CustomTitles, transcript.Tags)
	}
	if transcript.AITitles["s1"] != "AI Title" || transcript.LastPrompts["s1"] != "last prompt text" || transcript.TaskSummaries["s1"].Summary != "running tests" {
		t.Fatalf("title/prompt/task metadata = %#v %#v %#v", transcript.AITitles, transcript.LastPrompts, transcript.TaskSummaries)
	}
	if transcript.AITitles["s2"] != "Alias AI Title" {
		t.Fatalf("alias ai title = %#v", transcript.AITitles)
	}
	if transcript.Summaries["a2"] != "snake short" || transcript.CustomTitles["s3"] != "Snake Title" || transcript.AITitles["s3"] != "Snake AI Title" || transcript.LastPrompts["s3"] != "snake prompt" || transcript.TaskSummaries["s3"].Summary != "snake task" {
		t.Fatalf("snake metadata = %#v %#v %#v %#v %#v", transcript.Summaries, transcript.CustomTitles, transcript.AITitles, transcript.LastPrompts, transcript.TaskSummaries)
	}
	if transcript.AgentNames["s1"] != "Builder" || transcript.AgentColors["s1"] != "blue" || transcript.AgentSettings["s1"] != "reviewer" {
		t.Fatalf("agent metadata = %#v %#v %#v", transcript.AgentNames, transcript.AgentColors, transcript.AgentSettings)
	}
	if transcript.Tags["s3"] != "snake-tag" || transcript.AgentNames["s3"] != "Snake Builder" || transcript.AgentColors["s3"] != "violet" || transcript.AgentSettings["s3"] != "planner" {
		t.Fatalf("snake agent metadata = %#v %#v %#v %#v", transcript.Tags, transcript.AgentNames, transcript.AgentColors, transcript.AgentSettings)
	}
	if transcript.PRLinks["s1"].PRNumber != 42 || transcript.Modes["s1"] != "coordinator" || len(transcript.WorktreeStates["s1"].WorktreeSession) == 0 {
		t.Fatalf("session metadata = %#v %#v %#v", transcript.PRLinks, transcript.Modes, transcript.WorktreeStates)
	}
	if transcript.PRLinks["s3"].PRNumber != 43 || transcript.Modes["s3"] != "worker" || len(transcript.WorktreeStates["s3"].WorktreeSession) == 0 {
		t.Fatalf("snake session metadata = %#v %#v %#v", transcript.PRLinks, transcript.Modes, transcript.WorktreeStates)
	}
	if len(transcript.FileHistorySnapshots) != 1 || len(transcript.AttributionSnapshots) != 1 || len(transcript.SpeculationAccepts) != 2 || transcript.SpeculationAccepts[1].TimeSavedMS != 3400 {
		t.Fatalf("raw metadata counts = %d %d %d", len(transcript.FileHistorySnapshots), len(transcript.AttributionSnapshots), len(transcript.SpeculationAccepts))
	}
	if got := transcript.ContentReplacements["s1"]; len(got) != 1 || got[0].Replacement != "stub" {
		t.Fatalf("content replacements = %#v", got)
	}
	if got := transcript.ContentReplacements["s2"]; len(got) != 1 || got[0].Replacement != "alias stub" {
		t.Fatalf("alias content replacements = %#v", got)
	}
	if got := transcript.ContentReplacements["agent_1"]; len(got) != 1 || got[0].Replacement != "agent stub" {
		t.Fatalf("agent content replacements = %#v", got)
	}
	if got := transcript.ContentReplacements["agent_2"]; len(got) != 1 || got[0].Replacement != "snake agent stub" {
		t.Fatalf("snake agent content replacements = %#v", got)
	}
	if len(transcript.ContextCollapseCommits) != 1 || transcript.ContextCollapseCommits[0].CollapseID != "c1" || transcript.ContextCollapseCommits[0].SummaryUUID != "sum1" {
		t.Fatalf("collapse commit = %#v", transcript.ContextCollapseCommits)
	}
	if transcript.ContextCollapseSnapshot == nil || transcript.ContextCollapseSnapshot.SessionID != "s3" || transcript.ContextCollapseSnapshot.LastSpawnTokens != 64 {
		t.Fatalf("snapshot = %#v", transcript.ContextCollapseSnapshot)
	}
	metadata, err := LoadTranscriptMetadata(path)
	if err != nil {
		t.Fatal(err)
	}
	if metadata.Summaries["a1"] != "short" || metadata.CustomTitles["s1"] != "Title" || metadata.Tags["s1"] != "tagged" {
		t.Fatalf("metadata loader = %#v %#v %#v", metadata.Summaries, metadata.CustomTitles, metadata.Tags)
	}
	if metadata.AITitles["s1"] != "AI Title" || metadata.LastPrompts["s1"] != "last prompt text" || metadata.TaskSummaries["s1"].Summary != "running tests" {
		t.Fatalf("metadata title/prompt/task = %#v %#v %#v", metadata.AITitles, metadata.LastPrompts, metadata.TaskSummaries)
	}
	if metadata.AITitles["s2"] != "Alias AI Title" {
		t.Fatalf("metadata alias ai title = %#v", metadata.AITitles)
	}
	if metadata.Summaries["a2"] != "snake short" || metadata.CustomTitles["s3"] != "Snake Title" || metadata.AITitles["s3"] != "Snake AI Title" || metadata.LastPrompts["s3"] != "snake prompt" || metadata.TaskSummaries["s3"].Summary != "snake task" {
		t.Fatalf("metadata snake fields = %#v %#v %#v %#v %#v", metadata.Summaries, metadata.CustomTitles, metadata.AITitles, metadata.LastPrompts, metadata.TaskSummaries)
	}
	if metadata.AgentNames["s1"] != "Builder" || metadata.AgentColors["s1"] != "blue" || metadata.AgentSettings["s1"] != "reviewer" {
		t.Fatalf("metadata agent = %#v %#v %#v", metadata.AgentNames, metadata.AgentColors, metadata.AgentSettings)
	}
	if metadata.Tags["s3"] != "snake-tag" || metadata.AgentNames["s3"] != "Snake Builder" || metadata.AgentColors["s3"] != "violet" || metadata.AgentSettings["s3"] != "planner" {
		t.Fatalf("metadata snake agent = %#v %#v %#v %#v", metadata.Tags, metadata.AgentNames, metadata.AgentColors, metadata.AgentSettings)
	}
	if metadata.PRLinks["s1"].PRRepository != "o/r" || metadata.Modes["s1"] != "coordinator" || len(metadata.WorktreeStates["s1"].WorktreeSession) == 0 {
		t.Fatalf("metadata session = %#v %#v %#v", metadata.PRLinks, metadata.Modes, metadata.WorktreeStates)
	}
	if metadata.PRLinks["s3"].PRNumber != 43 || metadata.Modes["s3"] != "worker" || len(metadata.WorktreeStates["s3"].WorktreeSession) == 0 {
		t.Fatalf("metadata snake session = %#v %#v %#v", metadata.PRLinks, metadata.Modes, metadata.WorktreeStates)
	}
	if len(metadata.FileHistorySnapshots) != 1 || len(metadata.AttributionSnapshots) != 1 || len(metadata.SpeculationAccepts) != 2 || metadata.SpeculationAccepts[1].TimeSavedMS != 3400 {
		t.Fatalf("metadata raw counts = %d %d %d", len(metadata.FileHistorySnapshots), len(metadata.AttributionSnapshots), len(metadata.SpeculationAccepts))
	}
	if got := metadata.ContentReplacements["s1"]; len(got) != 1 || got[0].Replacement != "stub" {
		t.Fatalf("metadata replacements = %#v", got)
	}
	if got := metadata.ContentReplacements["s2"]; len(got) != 1 || got[0].Replacement != "alias stub" {
		t.Fatalf("metadata alias replacements = %#v", got)
	}
	if got := metadata.ContentReplacements["agent_1"]; len(got) != 1 || got[0].Replacement != "agent stub" {
		t.Fatalf("metadata agent replacements = %#v", got)
	}
	if got := metadata.ContentReplacements["agent_2"]; len(got) != 1 || got[0].Replacement != "snake agent stub" {
		t.Fatalf("metadata snake agent replacements = %#v", got)
	}
	if len(metadata.ContextCollapseCommits) != 1 || metadata.ContextCollapseCommits[0].CollapseID != "c1" || metadata.ContextCollapseCommits[0].SummaryUUID != "sum1" {
		t.Fatalf("metadata collapse commit = %#v", metadata.ContextCollapseCommits)
	}
	if metadata.ContextCollapseSnapshot == nil || metadata.ContextCollapseSnapshot.SessionID != "s3" || metadata.ContextCollapseSnapshot.LastSpawnTokens != 64 {
		t.Fatalf("metadata snapshot = %#v", metadata.ContextCollapseSnapshot)
	}
}

func TestReappendSessionMetadataWritesSessionScopedTailEntries(t *testing.T) {
	path := writeTranscript(t, []string{
		`{"type":"custom-title","sessionId":"s1","customTitle":"Title"}`,
		`{"type":"ai-title","sessionId":"s1","aiTitle":"AI Title"}`,
		`{"type":"last-prompt","sessionId":"s1","lastPrompt":"last prompt text"}`,
		`{"type":"task-summary","sessionId":"s1","summary":"running tests","timestamp":"2026-01-01T00:00:03Z"}`,
		`{"type":"tag","sessionId":"s1","tag":"tagged"}`,
		`{"type":"agent-name","sessionId":"s1","agentName":"Builder"}`,
		`{"type":"agent-color","sessionId":"s1","agentColor":"blue"}`,
		`{"type":"agent-setting","sessionId":"s1","agentSetting":"reviewer"}`,
		`{"type":"mode","sessionId":"s1","mode":"coordinator"}`,
		`{"type":"worktree-state","sessionId":"s1","worktreeSession":{"worktreePath":"/tmp/wt","sessionId":"s1"}}`,
		`{"type":"pr-link","sessionId":"s1","prNumber":42,"prUrl":"https://github.com/o/r/pull/42","prRepository":"o/r","timestamp":"2026-01-01T00:00:04Z"}`,
		`{"type":"custom-title","sessionId":"other","customTitle":"Other"}`,
	})
	result, err := ReappendSessionMetadata(path, "s1")
	if err != nil {
		t.Fatal(err)
	}
	if result.Written != 11 {
		t.Fatalf("result = %#v", result)
	}
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	lines := strings.Split(strings.TrimSpace(string(raw)), "\n")
	tail := strings.Join(lines[len(lines)-result.Written:], "\n")
	for _, want := range []string{
		`"type":"custom-title"`,
		`"customTitle":"Title"`,
		`"type":"ai-title"`,
		`"aiTitle":"AI Title"`,
		`"type":"last-prompt"`,
		`"lastPrompt":"last prompt text"`,
		`"type":"task-summary"`,
		`"summary":"running tests"`,
		`"type":"tag"`,
		`"type":"agent-name"`,
		`"type":"agent-color"`,
		`"type":"agent-setting"`,
		`"type":"mode"`,
		`"type":"worktree-state"`,
		`"type":"pr-link"`,
	} {
		if !strings.Contains(tail, want) {
			t.Fatalf("tail missing %q in %s", want, tail)
		}
	}
	for _, notWant := range []string{
		`"customTitle":"Other"`,
	} {
		if strings.Contains(tail, notWant) {
			t.Fatalf("tail unexpectedly contains %q in %s", notWant, tail)
		}
	}
}

func TestLoadTranscriptTailKeepsOnlyRecentMessagesAndBridgesProgress(t *testing.T) {
	path := writeTranscript(t, []string{
		`{"type":"user","uuid":"u1","parentUuid":null}`,
		`{"type":"progress","uuid":"p1","parentUuid":"u1"}`,
		`{"type":"assistant","uuid":"a1","parentUuid":"p1"}`,
		`{"type":"summary","leafUuid":"a1","summary":"short"}`,
		`{"type":"user","uuid":"u2","parentUuid":"a1"}`,
		`{"type":"assistant","uuid":"a2","parentUuid":"u2"}`,
	})
	tail, err := LoadTranscriptTail(path, 3)
	if err != nil {
		t.Fatal(err)
	}
	if got := tailIDs(tail); strings.Join(got, ",") != "a1,u2,a2" {
		t.Fatalf("tail = %#v", got)
	}
	if tail[0].ParentUUID == nil || *tail[0].ParentUUID != "u1" {
		t.Fatalf("bridged parent = %#v", tail[0].ParentUUID)
	}
	if len(tail[0].Raw) == 0 {
		t.Fatalf("tail raw message should be retained")
	}
}

func TestLoadTranscriptTailBytesReadsCompleteRecords(t *testing.T) {
	lines := []string{
		`{"type":"user","uuid":"u1","parentUuid":null}`,
		`{"type":"assistant","uuid":"a1","parentUuid":"u1"}`,
		`{"type":"progress","uuid":"p1","parentUuid":"a1"}`,
		`{"type":"user","uuid":"u2","parentUuid":"p1"}`,
		`{"type":"assistant","uuid":"a2","parentUuid":"u2"}`,
	}
	path := writeTranscript(t, lines)
	budget := int64(len(strings.Join(lines[2:], "\n")) + 1)
	tail, err := LoadTranscriptTailBytes(path, budget)
	if err != nil {
		t.Fatal(err)
	}
	if !tail.HasBefore || tail.StartOffset <= 0 || tail.BytesRead > budget {
		t.Fatalf("tail metadata = %#v budget=%d", tail, budget)
	}
	if got := tailIDs(tail.Messages); strings.Join(got, ",") != "u2,a2" {
		t.Fatalf("tail messages = %#v", got)
	}
	if tail.Messages[0].ParentUUID == nil || *tail.Messages[0].ParentUUID != "a1" {
		t.Fatalf("bridged byte-tail parent = %#v", tail.Messages[0].ParentUUID)
	}
	if len(tail.Messages[0].Raw) == 0 {
		t.Fatalf("byte-tail raw message should be retained")
	}

	partial, err := LoadTranscriptTailBytes(path, int64(len(lines[len(lines)-1])/2))
	if err != nil {
		t.Fatal(err)
	}
	if len(partial.Messages) != 0 || !partial.HasBefore {
		t.Fatalf("partial tail = %#v", partial)
	}
}

func TestLoadTranscriptWindowAroundUUID(t *testing.T) {
	path := writeTranscript(t, []string{
		`{"type":"user","uuid":"u1","parentUuid":null}`,
		`{"type":"assistant","uuid":"a1","parentUuid":"u1"}`,
		`{"type":"progress","uuid":"p1","parentUuid":"a1"}`,
		`{"type":"user","uuid":"u2","parentUuid":"p1"}`,
		`{"type":"assistant","uuid":"a2","parentUuid":"u2"}`,
		`{"type":"user","uuid":"u3","parentUuid":"a2"}`,
	})
	window, err := LoadTranscriptWindow(path, "u2", 1, 1)
	if err != nil {
		t.Fatal(err)
	}
	if !window.Found || window.TargetIndex != 1 || !window.HasBefore || !window.HasAfter {
		t.Fatalf("window metadata = %#v", window)
	}
	if got := tailIDs(window.Messages); strings.Join(got, ",") != "a1,u2,a2" {
		t.Fatalf("window messages = %#v", got)
	}
	if window.Messages[1].ParentUUID == nil || *window.Messages[1].ParentUUID != "a1" {
		t.Fatalf("bridged parent = %#v", window.Messages[1].ParentUUID)
	}

	largeWindow, err := LoadTranscriptWindow(path, "a2", 2, 5)
	if err != nil {
		t.Fatal(err)
	}
	if !largeWindow.Found || largeWindow.TargetIndex != 2 || !largeWindow.HasBefore || largeWindow.HasAfter {
		t.Fatalf("large window = %#v", largeWindow)
	}
	if got := tailIDs(largeWindow.Messages); strings.Join(got, ",") != "a1,u2,a2,u3" {
		t.Fatalf("large window messages = %#v", got)
	}
}

func TestTranscriptLineIndexLoadsWindowWithoutFullTranscript(t *testing.T) {
	path := writeTranscript(t, []string{
		`{malformed`,
		`{"type":"user","uuid":"u1","parentUuid":null,"timestamp":"2026-01-01T00:00:00Z"}`,
		`{"type":"progress","uuid":"p1","parentUuid":"u1"}`,
		`{"type":"assistant","uuid":"a1","parentUuid":"p1","timestamp":"2026-01-01T00:00:01Z"}`,
		`{"type":"summary","leafUuid":"a1","summary":"short"}`,
		`{"type":"user","uuid":"u2","parentUuid":"a1","timestamp":"2026-01-01T00:00:02Z"}`,
		`{"type":"assistant","uuid":"a2","parentUuid":"u2","timestamp":"2026-01-01T00:00:03Z"}`,
		`{"type":"user","uuid":"u3","parentUuid":"a2","timestamp":"2026-01-01T00:00:04Z"}`,
	})
	index, err := BuildTranscriptLineIndex(path)
	if err != nil {
		t.Fatal(err)
	}
	if len(index.Entries) != 5 || index.ByUUID["u2"] != 2 {
		t.Fatalf("index = %#v", index)
	}
	if ref := index.Entries[index.ByUUID["a1"]]; ref.ParentUUID == nil || *ref.ParentUUID != "u1" || ref.Offset <= 0 || ref.Length <= 0 {
		t.Fatalf("bridged ref = %#v", ref)
	}
	window, err := LoadTranscriptIndexedWindow(path, index, "u2", 1, 1)
	if err != nil {
		t.Fatal(err)
	}
	if !window.Found || window.TargetIndex != 1 || !window.HasBefore || !window.HasAfter {
		t.Fatalf("indexed window metadata = %#v", window)
	}
	if got := tailIDs(window.Messages); strings.Join(got, ",") != "a1,u2,a2" {
		t.Fatalf("indexed window messages = %#v", got)
	}
	if window.Messages[0].ParentUUID == nil || *window.Messages[0].ParentUUID != "u1" || len(window.Messages[0].Raw) == 0 {
		t.Fatalf("indexed message = %#v", window.Messages[0])
	}
	missing, err := LoadTranscriptIndexedWindow(path, index, "missing", 1, 1)
	if err != nil {
		t.Fatal(err)
	}
	if missing.Found || missing.TargetIndex != -1 {
		t.Fatalf("missing indexed window = %#v", missing)
	}
}

func TestTranscriptLineIndexLoadsByteBudgetWindow(t *testing.T) {
	lines := []string{
		`{"type":"user","uuid":"u1","parentUuid":null}`,
		`{"type":"progress","uuid":"p1","parentUuid":"u1"}`,
		`{"type":"assistant","uuid":"a1","parentUuid":"p1"}`,
		`{"type":"summary","leafUuid":"a1","summary":"short"}`,
		`{"type":"user","uuid":"u2","parentUuid":"a1"}`,
		`{"type":"assistant","uuid":"a2","parentUuid":"u2"}`,
		`{"type":"user","uuid":"u3","parentUuid":"a2"}`,
	}
	path := writeTranscript(t, lines)
	index, err := BuildTranscriptLineIndex(path)
	if err != nil {
		t.Fatal(err)
	}
	budget := int64(len(lines[2]) + 1 + len(lines[4]) + 1 + len(lines[5]) + 1)
	window, err := LoadTranscriptIndexedWindowBytes(path, index, "u2", budget)
	if err != nil {
		t.Fatal(err)
	}
	if !window.Found || window.TargetIndex != 1 || !window.HasBefore || !window.HasAfter || window.BytesRead > budget {
		t.Fatalf("byte window metadata = %#v budget=%d", window, budget)
	}
	if got := tailIDs(window.Messages); strings.Join(got, ",") != "a1,u2,a2" {
		t.Fatalf("byte window messages = %#v", got)
	}
	if window.Messages[0].ParentUUID == nil || *window.Messages[0].ParentUUID != "u1" {
		t.Fatalf("byte window bridged parent = %#v", window.Messages[0].ParentUUID)
	}

	targetOnly, err := LoadTranscriptIndexedWindowBytes(path, index, "u2", int64(len(lines[4])+1))
	if err != nil {
		t.Fatal(err)
	}
	if !targetOnly.Found || targetOnly.TargetIndex != 0 || strings.Join(tailIDs(targetOnly.Messages), ",") != "u2" {
		t.Fatalf("target-only byte window = %#v ids=%#v", targetOnly, tailIDs(targetOnly.Messages))
	}
}

func TestTranscriptLineIndexLoadsTailWithoutFullTranscript(t *testing.T) {
	lines := []string{
		`{"type":"user","uuid":"u1","parentUuid":null}`,
		`{"type":"progress","uuid":"p1","parentUuid":"u1"}`,
		`{"type":"assistant","uuid":"a1","parentUuid":"p1"}`,
		`{"type":"summary","leafUuid":"a1","summary":"short"}`,
		`{"type":"user","uuid":"u2","parentUuid":"a1"}`,
		`{"type":"assistant","uuid":"a2","parentUuid":"u2"}`,
		`{"type":"user","uuid":"u3","parentUuid":"a2"}`,
	}
	path := writeTranscript(t, lines)
	index, err := BuildTranscriptLineIndex(path)
	if err != nil {
		t.Fatal(err)
	}
	tail, err := LoadTranscriptIndexedTail(path, index, 3)
	if err != nil {
		t.Fatal(err)
	}
	if !tail.HasBefore || tail.StartIndex != 2 {
		t.Fatalf("indexed tail metadata = %#v", tail)
	}
	if got := tailIDs(tail.Messages); strings.Join(got, ",") != "u2,a2,u3" {
		t.Fatalf("indexed tail messages = %#v", got)
	}
	if tail.Messages[0].ParentUUID == nil || *tail.Messages[0].ParentUUID != "a1" || len(tail.Messages[0].Raw) == 0 {
		t.Fatalf("indexed tail message = %#v", tail.Messages[0])
	}

	all, err := LoadTranscriptIndexedTail(path, index, 50)
	if err != nil {
		t.Fatal(err)
	}
	if all.HasBefore || all.StartIndex != 0 || strings.Join(tailIDs(all.Messages), ",") != "u1,a1,u2,a2,u3" {
		t.Fatalf("all indexed tail = %#v ids=%#v", all, tailIDs(all.Messages))
	}

	empty, err := LoadTranscriptIndexedTail(path, index, 0)
	if err != nil {
		t.Fatal(err)
	}
	if len(empty.Messages) != 0 || empty.HasBefore {
		t.Fatalf("empty indexed tail = %#v", empty)
	}

	budget := int64(len(lines[5]) + 1 + len(lines[6]) + 1)
	byteTail, err := LoadTranscriptIndexedTailBytes(path, index, budget)
	if err != nil {
		t.Fatal(err)
	}
	if !byteTail.HasBefore || byteTail.StartIndex != 3 || byteTail.BytesRead > budget {
		t.Fatalf("indexed byte tail metadata = %#v budget=%d", byteTail, budget)
	}
	if got := tailIDs(byteTail.Messages); strings.Join(got, ",") != "a2,u3" {
		t.Fatalf("indexed byte tail messages = %#v", got)
	}
	if byteTail.Messages[0].ParentUUID == nil || *byteTail.Messages[0].ParentUUID != "u2" {
		t.Fatalf("indexed byte tail parent = %#v", byteTail.Messages[0].ParentUUID)
	}

	tooSmall, err := LoadTranscriptIndexedTailBytes(path, index, int64(len(lines[6])/2))
	if err != nil {
		t.Fatal(err)
	}
	if len(tooSmall.Messages) != 0 || tooSmall.BytesRead != 0 || !tooSmall.HasBefore {
		t.Fatalf("too-small indexed byte tail = %#v", tooSmall)
	}
}

func TestRemoveTranscriptMessageByUUID(t *testing.T) {
	path := writeTranscript(t, []string{
		`{"type":"user","uuid":"u1","parentUuid":null}`,
		`{"type":"assistant","uuid":"a1","parentUuid":"u1"}`,
		`{"type":"user","uuid":"u2","parentUuid":"a1"}`,
		`{malformed`,
	})
	removed, err := RemoveTranscriptMessageByUUID(path, "a1")
	if err != nil {
		t.Fatal(err)
	}
	if !removed {
		t.Fatal("message was not removed")
	}
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	text := string(data)
	if strings.Contains(text, `"uuid":"a1"`) {
		t.Fatalf("target line still present: %s", text)
	}
	if !strings.Contains(text, `"parentUuid":"a1"`) {
		t.Fatalf("child line should be preserved: %s", text)
	}
	if !strings.Contains(text, `{malformed`) {
		t.Fatalf("malformed line should be preserved: %s", text)
	}
}

func TestRemoveTranscriptMessageByUUIDHonorsRewriteLimit(t *testing.T) {
	path := writeTranscript(t, []string{`{"type":"user","uuid":"u1","parentUuid":null}`})
	removed, err := RemoveTranscriptMessageByUUIDWithLimit(path, "u1", 1)
	if err != nil {
		t.Fatal(err)
	}
	if removed {
		t.Fatal("message should not be removed when file exceeds rewrite limit")
	}
}

func writeTranscript(t *testing.T, lines []string) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "session.jsonl")
	if err := os.WriteFile(path, []byte(strings.Join(lines, "\n")+"\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	return path
}

func chainIDs(chain []TranscriptMessage) []string {
	out := make([]string, 0, len(chain))
	for _, msg := range chain {
		out = append(out, string(msg.UUID))
	}
	return out
}

func tailIDs(messages []TranscriptMessage) []string {
	out := make([]string, 0, len(messages))
	for _, msg := range messages {
		out = append(out, string(msg.UUID))
	}
	return out
}
