package telemetry

import (
	"encoding/json"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
)

func TestSessionPath(t *testing.T) {
	got := SessionPath(filepath.Join("tmp", "sessions", "session.jsonl"), "sess_1")
	want := filepath.Join("tmp", "sessions", "sess_1", "telemetry.jsonl")
	if got != want {
		t.Fatalf("SessionPath() = %q, want %q", got, want)
	}
	if got := SessionPath("", "sess_1"); got != "" {
		t.Fatalf("empty transcript path = %q, want empty", got)
	}
	if got := SessionPath("session.jsonl", ""); got != "" {
		t.Fatalf("empty session id = %q, want empty", got)
	}
}

func TestAppendWritesJSONLine(t *testing.T) {
	path := filepath.Join(t.TempDir(), "sess_1", "telemetry.jsonl")
	err := Append(path, Event{
		SessionID:    "sess_1",
		Type:         "tool_progress",
		ToolUseID:    "toolu_1",
		ProgressType: "task_started",
		ProgressKeys: SortedMapKeys(map[string]any{
			"status":  "running",
			"task_id": "task_1",
		}),
	})
	if err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Count(string(data), "\n") != 1 {
		t.Fatalf("telemetry JSONL = %q", string(data))
	}
	var got Event
	if err := json.Unmarshal([]byte(strings.TrimSpace(string(data))), &got); err != nil {
		t.Fatal(err)
	}
	if got.Timestamp == "" {
		t.Fatalf("timestamp was not populated: %#v", got)
	}
	if got.SessionID != "sess_1" || got.Type != "tool_progress" || got.ToolUseID != "toolu_1" || got.ProgressType != "task_started" {
		t.Fatalf("event = %#v", got)
	}
	if !reflect.DeepEqual(got.ProgressKeys, []string{"status", "task_id"}) {
		t.Fatalf("progress keys = %#v", got.ProgressKeys)
	}
}
