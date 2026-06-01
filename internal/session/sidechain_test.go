package session

import (
	"path/filepath"
	"testing"
	"time"

	"ccgo/internal/contracts"
)

func TestAppendAndListSidechainTranscript(t *testing.T) {
	sessionPath := filepath.Join(t.TempDir(), "session.jsonl")
	sessionID := contracts.ID("sess_1")
	if err := AppendSidechainMessage(sessionPath, sessionID, "agent/one", TranscriptMessage{
		Type: "assistant",
		UUID: "msg_1",
	}); err != nil {
		t.Fatal(err)
	}
	infos, err := ListSidechainTranscripts(sessionPath, sessionID)
	if err != nil {
		t.Fatal(err)
	}
	if len(infos) != 1 || infos[0].ID != "agent_one" {
		t.Fatalf("infos = %#v", infos)
	}
	transcript, err := LoadTranscript(infos[0].Path)
	if err != nil {
		t.Fatal(err)
	}
	msg := transcript.Messages["msg_1"]
	if msg == nil || !msg.IsSidechain || msg.SessionID != sessionID {
		t.Fatalf("message = %#v", msg)
	}
}

func TestSidechainRuntimeStartAppendFinish(t *testing.T) {
	sessionPath := filepath.Join(t.TempDir(), "session.jsonl")
	sessionID := contracts.ID("sess_1")
	parent := contracts.ID("parent_1")
	runtime := SidechainRuntime{SessionPath: sessionPath, SessionID: sessionID}
	startedAt := time.Unix(100, 0).UTC()
	run, err := runtime.Start(SidechainOptions{ID: "agent/one", ParentUUID: &parent, StartedAt: startedAt})
	if err != nil {
		t.Fatal(err)
	}
	if run.ID != "agent_one" || run.Status != SidechainStatusRunning || run.Path == "" || run.StartedAt != startedAt.Format(time.RFC3339Nano) {
		t.Fatalf("run = %#v", run)
	}
	if err := runtime.Append(run, TranscriptMessage{Type: "assistant", UUID: "agent_msg"}); err != nil {
		t.Fatal(err)
	}
	summary, err := runtime.Finish(run, SidechainStatusCompleted, "agent finished", time.Unix(110, 0).UTC())
	if err != nil {
		t.Fatal(err)
	}
	if summary.Subtype != "sidechain_summary" || !summary.IsSidechain {
		t.Fatalf("summary = %#v", summary)
	}
	sidechainTranscript, err := LoadTranscript(run.Path)
	if err != nil {
		t.Fatal(err)
	}
	if len(sidechainTranscript.Order) != 3 || sidechainTranscript.Messages["agent_msg"].ParentUUID == nil || *sidechainTranscript.Messages["agent_msg"].ParentUUID != parent {
		t.Fatalf("sidechain transcript = %#v", sidechainTranscript.Order)
	}
	mainTranscript, err := LoadTranscript(sessionPath)
	if err != nil {
		t.Fatal(err)
	}
	if len(mainTranscript.Order) != 1 || mainTranscript.Messages[summary.UUID] == nil {
		t.Fatalf("main transcript = %#v", mainTranscript.Order)
	}
}
