package remote

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
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
				{"deliveryId":"evt-1","team":"remote/team","recipient":"coordinator","remote":"github","eventType":"workflow_failed","ack":{"url":"https://remote/ack/evt-1"},"lease":{"id":"lease-1","expiresAt":"2026-06-17T11:30:00Z"},"payload":{"message":"Fix CI."}},
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
	if result.Events[0].EventID != "evt-1" || result.Events[0].TeamID != "remote/team" || result.Events[0].Target != "coordinator" || result.Events[0].Source != "github" || result.Events[0].Event != "workflow_failed" || result.Events[0].Message != "Fix CI." || result.Events[0].AckURL != "https://remote/ack/evt-1" || result.Events[0].LeaseID != "lease-1" || result.Events[0].LeaseExpiresAt != "2026-06-17T11:30:00Z" {
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
	events, cursor, err = DecodePollEvents([]byte(`{"cursor":"c4","data":{"id":"evt-data","team":"team","payload":{"message":"data payload"}}}`))
	if err != nil {
		t.Fatal(err)
	}
	if cursor != "c4" || len(events) != 1 || events[0].EventID != "evt-data" || events[0].Message != "data payload" {
		t.Fatalf("data wrapper events=%#v cursor=%q", events, cursor)
	}
	events, cursor, err = DecodePollEvents([]byte(`{"type":"remote_trigger","event":{"delivery_id":"evt-event","team_id":"team","target":"coordinator","message":"event wrapper","ack_url":"https://remote/ack/evt-event","lease_id":"lease-event","lease_expires_at":"2026-06-17T11:45:00Z"}}`))
	if err != nil {
		t.Fatal(err)
	}
	if cursor != "" || len(events) != 1 || events[0].EventID != "evt-event" || events[0].Target != "coordinator" || events[0].Message != "event wrapper" || events[0].AckURL != "https://remote/ack/evt-event" || events[0].LeaseID != "lease-event" || events[0].LeaseExpiresAt != "2026-06-17T11:45:00Z" {
		t.Fatalf("event wrapper events=%#v cursor=%q", events, cursor)
	}
	events, cursor, err = DecodePollEvents([]byte(`{"kind":"delivery","payload":{"id":"evt-payload","team_id":"team","event_type":"deploy","message":"payload wrapper"}}`))
	if err != nil {
		t.Fatal(err)
	}
	if cursor != "" || len(events) != 1 || events[0].EventID != "evt-payload" || events[0].Event != "deploy" || events[0].Message != "payload wrapper" {
		t.Fatalf("payload wrapper events=%#v cursor=%q", events, cursor)
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

func TestSendAckPostsSameOriginPayloadAndAuth(t *testing.T) {
	var gotAuth string
	var got map[string]any
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotAuth = r.Header.Get("Authorization")
		if r.Method != http.MethodPost {
			t.Fatalf("method = %s", r.Method)
		}
		if err := json.NewDecoder(r.Body).Decode(&got); err != nil {
			t.Fatal(err)
		}
		w.WriteHeader(http.StatusAccepted)
	}))
	defer server.Close()

	result := SendAck(context.Background(), AckOptions{
		AckURL:         server.URL + "/ack?token=secret",
		AuthToken:      "ack-token",
		EventID:        "evt-ack",
		Status:         "delivered",
		SentCount:      2,
		AllowedOrigins: []string{server.URL + "/poll"},
	})
	if result.Error != "" || result.StatusCode != http.StatusAccepted {
		t.Fatalf("ack result = %#v", result)
	}
	if gotAuth != "Bearer ack-token" || got["event_id"] != "evt-ack" || got["status"] != "delivered" || got["sent_count"] != float64(2) || got["duplicate"] != false {
		t.Fatalf("auth=%q payload=%#v", gotAuth, got)
	}
}

func TestSendAckRejectsDisallowedOriginAndRedactsURL(t *testing.T) {
	result := SendAck(context.Background(), AckOptions{
		AckURL:         "https://user:pass@example.invalid/ack?token=secret",
		EventID:        "evt",
		Status:         "delivered",
		AllowedOrigins: []string{"https://remote.example/poll"},
	})
	if result.Error == "" || !strings.Contains(result.Error, "not allowed") || strings.Contains(result.Error, "token=secret") || strings.Contains(result.Error, "user:pass") {
		t.Fatalf("ack result = %#v", result)
	}
}

func TestWriteAndLoadPumpState(t *testing.T) {
	path := filepath.Join(t.TempDir(), "sess_remote", pumpFileName)
	state := PumpState{
		SessionID:         "sess_remote",
		RuntimeState:      PumpRunning,
		Transport:         "websocket",
		PollURL:           "https://remote/poll",
		WebSocketURL:      "wss://remote/ws",
		LastCursor:        "cursor-1",
		StreamStartedAt:   "2026-06-17T10:00:00Z",
		StreamEndedAt:     "2026-06-17T10:05:00Z",
		StreamStopReason:  "max_frames",
		CloseCode:         1000,
		FrameCount:        2,
		ConnectCount:      1,
		ReconnectCount:    1,
		AckEventCount:     1,
		AckSentCount:      1,
		AckErrorCount:     1,
		LeaseEventCount:   1,
		LeaseExpiredCount: 1,
		EventCount:        2,
		DeliveredCount:    1,
	}
	if err := WritePumpState(path, state); err != nil {
		t.Fatal(err)
	}
	loaded, err := LoadPumpState(path)
	if err != nil {
		t.Fatal(err)
	}
	if loaded.SessionID != "sess_remote" || loaded.RuntimeState != PumpRunning || loaded.Transport != "websocket" || loaded.WebSocketURL != "wss://remote/ws" || loaded.LastCursor != "cursor-1" || loaded.StreamStartedAt != "2026-06-17T10:00:00Z" || loaded.StreamEndedAt != "2026-06-17T10:05:00Z" || loaded.StreamStopReason != "max_frames" || loaded.CloseCode != 1000 || loaded.FrameCount != 2 || loaded.ConnectCount != 1 || loaded.ReconnectCount != 1 || loaded.AckEventCount != 1 || loaded.AckSentCount != 1 || loaded.AckErrorCount != 1 || loaded.LeaseEventCount != 1 || loaded.LeaseExpiredCount != 1 || loaded.LastPollAt == "" {
		t.Fatalf("loaded = %#v", loaded)
	}
	data, err := json.Marshal(loaded)
	if err != nil || len(data) == 0 {
		t.Fatalf("marshal = %s err=%v", data, err)
	}
}
