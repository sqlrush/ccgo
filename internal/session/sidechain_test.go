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
	states, err := ListSidechainStates(sessionPath, sessionID)
	if err != nil {
		t.Fatal(err)
	}
	if len(states) != 1 || states[0].ID != "agent_one" || states[0].Status != SidechainStatusRunning || states[0].MessageCount != 2 || states[0].LastUUID != "agent_msg" {
		t.Fatalf("running state = %#v", states)
	}
	resumed, ok := ResumeSidechainRunFromState(states[0])
	if !ok || resumed.ID != run.ID || resumed.Path != run.Path || resumed.ParentUUID == nil || *resumed.ParentUUID != parent {
		t.Fatalf("resumed from state = %#v ok=%v", resumed, ok)
	}
	resumed, ok, err = ResumeSidechainRun(sessionPath, sessionID, "agent/one")
	if err != nil {
		t.Fatal(err)
	}
	if !ok || resumed.ID != run.ID || resumed.Status != SidechainStatusRunning {
		t.Fatalf("resumed = %#v ok=%v", resumed, ok)
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
	states, err = ListSidechainStates(sessionPath, sessionID)
	if err != nil {
		t.Fatal(err)
	}
	if len(states) != 1 || states[0].Status != SidechainStatusCompleted || states[0].Summary != "agent finished" || states[0].MessageCount != 3 {
		t.Fatalf("finished state = %#v", states)
	}
	if resumed, ok := ResumeSidechainRunFromState(states[0]); ok {
		t.Fatalf("completed sidechain should not resume: %#v", resumed)
	}
	if resumed, ok, err := ResumeSidechainRun(sessionPath, sessionID, "agent/one"); err != nil || ok {
		t.Fatalf("completed resume = %#v ok=%v err=%v", resumed, ok, err)
	}
}

func TestLoadSidechainStateMarksOrphanTranscriptUnknown(t *testing.T) {
	sessionPath := filepath.Join(t.TempDir(), "session.jsonl")
	sessionID := contracts.ID("sess_1")
	if err := AppendSidechainMessage(sessionPath, sessionID, "orphan", TranscriptMessage{
		Type: "assistant",
		UUID: "msg_1",
	}); err != nil {
		t.Fatal(err)
	}
	states, err := ListSidechainStates(sessionPath, sessionID)
	if err != nil {
		t.Fatal(err)
	}
	if len(states) != 1 || states[0].Status != SidechainStatusUnknown || states[0].MessageCount != 1 {
		t.Fatalf("states = %#v", states)
	}
	if run, ok := ResumeSidechainRunFromState(states[0]); ok {
		t.Fatalf("unknown sidechain should not resume: %#v", run)
	}
}

func TestSidechainManagerOrchestratesRunningSidechains(t *testing.T) {
	sessionPath := filepath.Join(t.TempDir(), "session.jsonl")
	sessionID := contracts.ID("sess_1")
	parent := contracts.ID("parent_1")
	manager := NewSidechainManager(sessionPath, sessionID)
	run, err := manager.Start(SidechainOptions{ID: "agent/one", ParentUUID: &parent, StartedAt: time.Unix(200, 0).UTC()})
	if err != nil {
		t.Fatal(err)
	}
	if err := manager.Append("agent/one", TranscriptMessage{Type: "assistant", UUID: "agent_msg_1"}); err != nil {
		t.Fatal(err)
	}
	if err := manager.Append("agent/one", TranscriptMessage{Type: "assistant", UUID: "agent_msg_2"}); err != nil {
		t.Fatal(err)
	}
	states, err := manager.List()
	if err != nil {
		t.Fatal(err)
	}
	if len(states) != 1 || states[0].LastUUID != "agent_msg_2" || states[0].MessageCount != 3 {
		t.Fatalf("states = %#v", states)
	}
	running, err := manager.ResumeRunning()
	if err != nil {
		t.Fatal(err)
	}
	if len(running) != 1 || running[0].ID != run.ID {
		t.Fatalf("running = %#v", running)
	}
	summary, err := manager.Finish("agent/one", SidechainStatusCompleted, "done", time.Unix(210, 0).UTC())
	if err != nil {
		t.Fatal(err)
	}
	if summary.ParentUUID == nil || *summary.ParentUUID != "agent_msg_2" {
		t.Fatalf("summary parent = %#v", summary.ParentUUID)
	}
	sidechainTranscript, err := LoadTranscript(run.Path)
	if err != nil {
		t.Fatal(err)
	}
	if sidechainTranscript.Messages["agent_msg_1"].ParentUUID == nil || *sidechainTranscript.Messages["agent_msg_1"].ParentUUID != sidechainTranscript.Order[0] {
		t.Fatalf("first append parent = %#v order=%#v", sidechainTranscript.Messages["agent_msg_1"].ParentUUID, sidechainTranscript.Order)
	}
	if sidechainTranscript.Messages["agent_msg_2"].ParentUUID == nil || *sidechainTranscript.Messages["agent_msg_2"].ParentUUID != "agent_msg_1" {
		t.Fatalf("second append parent = %#v", sidechainTranscript.Messages["agent_msg_2"].ParentUUID)
	}
	if _, ok, err := manager.Resume("agent/one"); err != nil || ok {
		t.Fatalf("completed resume ok=%v err=%v", ok, err)
	}
	if err := manager.Append("agent/one", TranscriptMessage{Type: "assistant", UUID: "late"}); err == nil {
		t.Fatal("expected append to completed sidechain to fail")
	}
}

func TestSidechainManifestSummarizesStates(t *testing.T) {
	sessionPath := filepath.Join(t.TempDir(), "session.jsonl")
	sessionID := contracts.ID("sess_1")
	manager := NewSidechainManager(sessionPath, sessionID)
	if _, err := manager.Start(SidechainOptions{ID: "running", StartedAt: time.Unix(300, 0).UTC()}); err != nil {
		t.Fatal(err)
	}
	if _, err := manager.Start(SidechainOptions{ID: "done", StartedAt: time.Unix(100, 0).UTC()}); err != nil {
		t.Fatal(err)
	}
	if _, err := manager.Finish("done", SidechainStatusCompleted, "finished work", time.Unix(200, 0).UTC()); err != nil {
		t.Fatal(err)
	}
	if _, err := manager.Start(SidechainOptions{ID: "failed", StartedAt: time.Unix(120, 0).UTC()}); err != nil {
		t.Fatal(err)
	}
	if _, err := manager.Finish("failed", SidechainStatusFailed, "boom", time.Unix(240, 0).UTC()); err != nil {
		t.Fatal(err)
	}
	manifest, err := manager.Manifest()
	if err != nil {
		t.Fatal(err)
	}
	if manifest.SessionID != sessionID || manifest.Total != 3 || manifest.Running != 1 || manifest.Completed != 1 || manifest.Failed != 1 {
		t.Fatalf("manifest counts = %#v", manifest)
	}
	if manifest.LatestStartedAt != time.Unix(300, 0).UTC().Format(time.RFC3339Nano) || manifest.LatestEndedAt != time.Unix(240, 0).UTC().Format(time.RFC3339Nano) {
		t.Fatalf("manifest times = %#v", manifest)
	}
	if len(manifest.Summaries) != 2 || manifest.Summaries[0].ID != "done" || manifest.Summaries[0].Summary != "finished work" || manifest.Summaries[1].Status != SidechainStatusFailed {
		t.Fatalf("manifest summaries = %#v", manifest.Summaries)
	}
}
