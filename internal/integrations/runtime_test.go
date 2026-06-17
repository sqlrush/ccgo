package integrations

import (
	"path/filepath"
	"testing"
)

func TestSessionRuntimeStatePath(t *testing.T) {
	got := SessionRuntimeStatePath(filepath.Join("tmp", "sessions", "session.jsonl"), "sess_int", "computer-use")
	want := filepath.Join("tmp", "sessions", "sess_int", "integration-computer_use.json")
	if got != want {
		t.Fatalf("SessionRuntimeStatePath() = %q, want %q", got, want)
	}
	if got := SessionRuntimeStatePath("", "sess_int", "chrome"); got != "" {
		t.Fatalf("empty transcript path = %q, want empty", got)
	}
	if got := SessionRuntimeStatePath("session.jsonl", "sess_int", "unknown"); got != "" {
		t.Fatalf("unknown runtime path = %q, want empty", got)
	}
}

func TestWriteAndLoadRuntimeState(t *testing.T) {
	path := filepath.Join(t.TempDir(), "sess_int", "integration-chrome.json")
	state := RuntimeState{
		SessionID:    "sess_int",
		Name:         "chrome",
		Enabled:      true,
		RuntimeState: RuntimeStateReady,
		Artifacts:    map[string]string{"state": path},
	}
	if err := WriteRuntimeState(path, state); err != nil {
		t.Fatal(err)
	}
	loaded, err := LoadRuntimeState(path)
	if err != nil {
		t.Fatal(err)
	}
	if loaded.SessionID != "sess_int" || loaded.GeneratedAt == "" || loaded.Name != "chrome" || loaded.RuntimeState != RuntimeStateReady || loaded.Artifacts["state"] != path {
		t.Fatalf("loaded = %#v", loaded)
	}
}

func TestBuildRuntimeState(t *testing.T) {
	state := BuildRuntimeState(filepath.Join("tmp", "sessions", "session.jsonl"), "sess_int", "/work", Integration{
		Name:         "voice",
		Enabled:      true,
		RuntimeState: RuntimeStateReady,
		Detail:       "ready",
		Adapters:     []Adapter{{Name: "pw-record", Kind: AdapterKindAudioCapture, Available: true}},
	})
	if state.SessionID != "sess_int" || state.WorkingDirectory != "/work" || state.GeneratedAt == "" || state.Name != "voice" || !state.Enabled || state.RuntimeState != RuntimeStateReady || state.Artifacts["state"] == "" || len(state.Adapters) != 1 {
		t.Fatalf("state = %#v", state)
	}
}
