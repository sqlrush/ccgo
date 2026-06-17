package daemon

import (
	"errors"
	"path/filepath"
	"testing"
	"time"
)

func TestWriteAndLoadState(t *testing.T) {
	now := time.Date(2026, 6, 17, 10, 0, 0, 0, time.UTC)
	path := filepath.Join(t.TempDir(), "sess_daemon", stateFileName)
	state := BuildState("sess_daemon", "/work", RuntimeRunning, 1234, "http://127.0.0.1:5555", now, nil)
	if err := WriteState(path, state); err != nil {
		t.Fatal(err)
	}
	loaded, err := LoadState(path)
	if err != nil {
		t.Fatal(err)
	}
	if loaded.SessionID != "sess_daemon" || loaded.RuntimeState != RuntimeRunning || loaded.PID != 1234 || loaded.Endpoint != "http://127.0.0.1:5555" || loaded.HeartbeatAt == "" {
		t.Fatalf("loaded = %#v", loaded)
	}
}

func TestRuntimeStateAtMarksStaleRunningDaemon(t *testing.T) {
	now := time.Date(2026, 6, 17, 10, 0, 0, 0, time.UTC)
	state := BuildState("sess_daemon", "/work", RuntimeRunning, 1234, "", now.Add(-5*time.Minute), nil)
	if got := RuntimeStateAt(state, now, time.Minute); got != RuntimeStale {
		t.Fatalf("runtime state = %q, want stale", got)
	}
	if got := RuntimeStateAt(state, now, 10*time.Minute); got != RuntimeRunning {
		t.Fatalf("runtime state = %q, want running", got)
	}
}

func TestBuildStateRecordsError(t *testing.T) {
	state := BuildState("sess_daemon", "/work", RuntimeDisabled, 0, "", time.Time{}, errors.New("bind failed"))
	if state.RuntimeState != RuntimeFailed || state.Error != "bind failed" || state.GeneratedAt == "" {
		t.Fatalf("state = %#v", state)
	}
}
