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

func TestLoadTranscriptCollectsMetadataEntries(t *testing.T) {
	path := writeTranscript(t, []string{
		`{"type":"summary","leafUuid":"a1","summary":"short"}`,
		`{"type":"custom-title","sessionId":"s1","customTitle":"Title"}`,
		`{"type":"tag","sessionId":"s1","tag":"tagged"}`,
		`{"type":"content-replacement","sessionId":"s1","replacements":[{"toolUseId":"toolu_1","replacement":"stub"}]}`,
		`{"type":"marble-origami-snapshot","sessionId":"s1","armed":true,"lastSpawnTokens":42}`,
	})
	transcript, err := LoadTranscript(path)
	if err != nil {
		t.Fatal(err)
	}
	if transcript.Summaries["a1"] != "short" || transcript.CustomTitles["s1"] != "Title" || transcript.Tags["s1"] != "tagged" {
		t.Fatalf("metadata = %#v %#v %#v", transcript.Summaries, transcript.CustomTitles, transcript.Tags)
	}
	if got := transcript.ContentReplacements["s1"]; len(got) != 1 || got[0].Replacement != "stub" {
		t.Fatalf("content replacements = %#v", got)
	}
	if transcript.ContextCollapseSnapshot == nil || transcript.ContextCollapseSnapshot.LastSpawnTokens != 42 {
		t.Fatalf("snapshot = %#v", transcript.ContextCollapseSnapshot)
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
