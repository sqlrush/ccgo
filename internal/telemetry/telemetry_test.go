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
	if got.TraceID == "" || got.SpanID == "" {
		t.Fatalf("trace/span was not populated: %#v", got)
	}
	if got.TraceID != TraceID("sess_1") {
		t.Fatalf("trace id = %q, want %q", got.TraceID, TraceID("sess_1"))
	}
	if got.SessionID != "sess_1" || got.Type != "tool_progress" || got.ToolUseID != "toolu_1" || got.ProgressType != "task_started" {
		t.Fatalf("event = %#v", got)
	}
	if !reflect.DeepEqual(got.ProgressKeys, []string{"status", "task_id"}) {
		t.Fatalf("progress keys = %#v", got.ProgressKeys)
	}
}

func TestLoadFilterAndSummarize(t *testing.T) {
	path := filepath.Join(t.TempDir(), "sess_1", "telemetry.jsonl")
	for _, event := range []Event{
		{SessionID: "sess_1", Type: "user_message"},
		{SessionID: "sess_1", Type: "assistant_message", Model: "sonnet"},
		{SessionID: "sess_1", Type: "tool_result", ToolUseID: "toolu_1", ToolResultErr: true, Error: "failed"},
		{SessionID: "sess_1", Type: "compact", CompactTrigger: "manual"},
		{SessionID: "sess_1", Type: "token_warning", TokenState: "warning"},
	} {
		if err := Append(path, event); err != nil {
			t.Fatal(err)
		}
	}
	events, err := Load(path)
	if err != nil {
		t.Fatal(err)
	}
	if len(events) != 5 {
		t.Fatalf("events = %#v", events)
	}
	filtered := FilterEvents(events, Filter{Type: "assistant_message", Model: "sonnet", Limit: 1})
	if len(filtered) != 1 || filtered[0].Type != "assistant_message" {
		t.Fatalf("filtered = %#v", filtered)
	}
	summary := Summarize(events)
	if summary.Total != 5 ||
		summary.ByType["tool_result"] != 1 ||
		summary.ByModel["sonnet"] != 1 ||
		summary.Traces != 1 ||
		summary.Spans != 5 ||
		summary.ToolEvents != 1 ||
		summary.ToolErrors != 1 ||
		summary.ErrorEvents != 1 ||
		summary.Compactions != 1 ||
		summary.TokenWarnings != 1 {
		t.Fatalf("summary = %#v", summary)
	}
}

func TestPrepareEventPreservesProvidedTraceMetadata(t *testing.T) {
	event := PrepareEvent(Event{
		SessionID:    "sess_1",
		TraceID:      "trace_custom",
		SpanID:       "span_custom",
		ParentSpanID: "span_parent",
		Type:         "assistant_message",
	})
	if event.Timestamp == "" || event.TraceID != "trace_custom" || event.SpanID != "span_custom" || event.ParentSpanID != "span_parent" {
		t.Fatalf("event = %#v", event)
	}
}

func TestExportSummaryWritesFilteredJSON(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "sess_1", "telemetry.jsonl")
	for _, event := range []Event{
		{SessionID: "sess_1", Type: "user_message"},
		{SessionID: "sess_1", Type: "assistant_message", Model: "sonnet"},
		{SessionID: "sess_1", Type: "assistant_message", Model: "opus"},
	} {
		if err := Append(path, event); err != nil {
			t.Fatal(err)
		}
	}
	outPath := filepath.Join(dir, "telemetry-export.json")
	export, err := ExportSummary(path, outPath, Filter{Type: "assistant_message", Model: "sonnet"})
	if err != nil {
		t.Fatal(err)
	}
	if export.SourcePath != path || export.EventCount != 1 || export.Summary.Total != 1 || len(export.Events) != 1 {
		t.Fatalf("export = %#v", export)
	}
	if export.Events[0].TraceID == "" || export.Events[0].SpanID == "" || export.Events[0].Model != "sonnet" {
		t.Fatalf("export events = %#v", export.Events)
	}
	data, err := os.ReadFile(outPath)
	if err != nil {
		t.Fatal(err)
	}
	var loaded Export
	if err := json.Unmarshal(data, &loaded); err != nil {
		t.Fatal(err)
	}
	if loaded.EventCount != 1 || loaded.Summary.ByType["assistant_message"] != 1 {
		t.Fatalf("loaded export = %#v", loaded)
	}
}

func TestLoadMissingFile(t *testing.T) {
	events, err := Load(filepath.Join(t.TempDir(), "missing.jsonl"))
	if err != nil {
		t.Fatal(err)
	}
	if events != nil {
		t.Fatalf("events = %#v", events)
	}
}
