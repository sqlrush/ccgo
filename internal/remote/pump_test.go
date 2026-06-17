package remote

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"testing"
)

func TestFetchPollEventsUsesCursorAndAuth(t *testing.T) {
	var gotCursor string
	var gotAuth string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotCursor = r.URL.Query().Get("cursor")
		gotAuth = r.Header.Get("Authorization")
		w.Header().Set("content-type", "application/json")
		_, _ = w.Write([]byte(`{
			"nextCursor":"cursor-2",
			"events":[
				{"deliveryId":"evt-1","team":"remote/team","recipient":"coordinator","remote":"github","eventType":"workflow_failed","payload":{"message":"Fix CI."}},
				{"id":"evt-2","team_id":"remote/team","message":"Ship docs."}
			]
		}`))
	}))
	defer server.Close()

	result := FetchPollEvents(context.Background(), PollOptions{
		PollURL:   server.URL + "/poll?token=secret",
		Cursor:    "cursor-1",
		AuthToken: "poll-token",
	})
	if result.Error != "" || result.StatusCode != http.StatusOK || result.NextCursor != "cursor-2" || len(result.Events) != 2 {
		t.Fatalf("poll result = %#v", result)
	}
	if gotCursor != "cursor-1" || gotAuth != "Bearer poll-token" {
		t.Fatalf("cursor/auth = %q/%q", gotCursor, gotAuth)
	}
	if result.Events[0].EventID != "evt-1" || result.Events[0].TeamID != "remote/team" || result.Events[0].Target != "coordinator" || result.Events[0].Source != "github" || result.Events[0].Event != "workflow_failed" || result.Events[0].Message != "Fix CI." {
		t.Fatalf("event aliases = %#v", result.Events[0])
	}
}

func TestDecodePollEventsAcceptsNestedDataAndArray(t *testing.T) {
	events, cursor, err := DecodePollEvents([]byte(`{"data":{"cursor":"c2","items":[{"event_id":"evt","team_id":"team","text":"hello"}]}}`))
	if err != nil {
		t.Fatal(err)
	}
	if cursor != "c2" || len(events) != 1 || events[0].Message != "hello" {
		t.Fatalf("nested events=%#v cursor=%q", events, cursor)
	}
	events, cursor, err = DecodePollEvents([]byte(`[{"id":"evt-array","team":"team","payload":{"summary":"array payload"}}]`))
	if err != nil {
		t.Fatal(err)
	}
	if cursor != "" || len(events) != 1 || events[0].EventID != "evt-array" || events[0].Message != "array payload" {
		t.Fatalf("array events=%#v cursor=%q", events, cursor)
	}
	events, cursor, err = DecodePollEvents([]byte(`{"id":"evt-single","team":"team","payload":{"message":"single payload"}}`))
	if err != nil {
		t.Fatal(err)
	}
	if cursor != "" || len(events) != 1 || events[0].EventID != "evt-single" || events[0].Message != "single payload" {
		t.Fatalf("single events=%#v cursor=%q", events, cursor)
	}
	events, cursor, err = DecodePollEvents([]byte(`{"cursor":"c3","message":"ok"}`))
	if err != nil {
		t.Fatal(err)
	}
	if cursor != "c3" || len(events) != 0 {
		t.Fatalf("status object events=%#v cursor=%q", events, cursor)
	}
}

func TestFetchPollEventsReportsFailedHTTPAndRedactsInvalidURL(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, "bad", http.StatusBadGateway)
	}))
	defer server.Close()
	result := FetchPollEvents(context.Background(), PollOptions{PollURL: server.URL + "/poll"})
	if result.StatusCode != http.StatusBadGateway || result.Error == "" {
		t.Fatalf("http failure = %#v", result)
	}
	invalid := FetchPollEvents(context.Background(), PollOptions{PollURL: "ftp://user:pass@example.invalid/poll?token=secret"})
	if invalid.Error == "" || DisplayEndpoint("https://user:pass@example.com/poll?token=secret#frag") != "https://example.com/poll" {
		t.Fatalf("invalid=%#v display=%q", invalid, DisplayEndpoint("https://user:pass@example.com/poll?token=secret#frag"))
	}
}

func TestWriteAndLoadPumpState(t *testing.T) {
	path := filepath.Join(t.TempDir(), "sess_remote", pumpFileName)
	state := PumpState{
		SessionID:      "sess_remote",
		RuntimeState:   PumpRunning,
		Transport:      "websocket",
		PollURL:        "https://remote/poll",
		WebSocketURL:   "wss://remote/ws",
		LastCursor:     "cursor-1",
		EventCount:     2,
		DeliveredCount: 1,
	}
	if err := WritePumpState(path, state); err != nil {
		t.Fatal(err)
	}
	loaded, err := LoadPumpState(path)
	if err != nil {
		t.Fatal(err)
	}
	if loaded.SessionID != "sess_remote" || loaded.RuntimeState != PumpRunning || loaded.Transport != "websocket" || loaded.WebSocketURL != "wss://remote/ws" || loaded.LastCursor != "cursor-1" || loaded.LastPollAt == "" {
		t.Fatalf("loaded = %#v", loaded)
	}
	data, err := json.Marshal(loaded)
	if err != nil || len(data) == 0 {
		t.Fatalf("marshal = %s err=%v", data, err)
	}
}
