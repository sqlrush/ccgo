package main

import (
	"bytes"
	"context"
	"crypto/sha1"
	"encoding/base64"
	"encoding/binary"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"ccgo/internal/api/anthropic"
	"ccgo/internal/bootstrap"
	compactpkg "ccgo/internal/compact"
	"ccgo/internal/contracts"
	"ccgo/internal/conversation"
	daemonpkg "ccgo/internal/daemon"
	integrationspkg "ccgo/internal/integrations"
	"ccgo/internal/messages"
	remotepkg "ccgo/internal/remote"
	"ccgo/internal/session"
	"ccgo/internal/tool"
	bashtools "ccgo/internal/tools/bash"
	filetools "ccgo/internal/tools/file"
)

func TestRunPrintSendsPromptAndPrintsAssistantText(t *testing.T) {
	var requestBody map[string]any
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/v1/messages" {
			t.Fatalf("path = %s", r.URL.Path)
		}
		if got := r.Header.Get("x-api-key"); got != "test-key" {
			t.Fatalf("x-api-key = %q", got)
		}
		if got := r.Header.Get("user-agent"); got != "ccgo/"+version {
			t.Fatalf("user-agent = %q", got)
		}
		if err := json.NewDecoder(r.Body).Decode(&requestBody); err != nil {
			t.Fatal(err)
		}
		w.Header().Set("content-type", "application/json")
		_, _ = w.Write([]byte(`{
			"id":"msg_1",
			"type":"message",
			"role":"assistant",
			"model":"claude-haiku-4-5-20251001",
			"content":[{"type":"text","text":"hello from api"}],
			"stop_reason":"end_turn",
			"usage":{"input_tokens":1,"output_tokens":2}
		}`))
	}))
	defer server.Close()

	t.Setenv("ANTHROPIC_API_KEY", "test-key")
	t.Setenv("ANTHROPIC_BASE_URL", server.URL)
	t.Setenv("ANTHROPIC_MODEL", "")
	t.Setenv("CLAUDE_MODEL", "")
	t.Setenv("ANTHROPIC_BETA", "")
	t.Setenv("CLAUDE_CONFIG_DIR", t.TempDir())

	var stdout, stderr bytes.Buffer
	code := run([]string{"--print", "--model", "haiku", "--max-tokens", "17", "say hello"}, strings.NewReader(""), &stdout, &stderr)
	if code != 0 {
		t.Fatalf("exit = %d stderr=%s", code, stderr.String())
	}
	if got := stdout.String(); got != "hello from api\n" {
		t.Fatalf("stdout = %q", got)
	}
	if stderr.Len() != 0 {
		t.Fatalf("stderr = %q", stderr.String())
	}
	if requestBody["model"] != "claude-haiku-4-5-20251001" {
		t.Fatalf("model = %#v", requestBody["model"])
	}
	if requestBody["max_tokens"] != float64(17) {
		t.Fatalf("max_tokens = %#v", requestBody["max_tokens"])
	}
	messages, ok := requestBody["messages"].([]any)
	if !ok || len(messages) != 1 {
		t.Fatalf("messages = %#v", requestBody["messages"])
	}
	message, ok := messages[0].(map[string]any)
	if !ok || message["role"] != "user" {
		t.Fatalf("message = %#v", messages[0])
	}
	content, ok := message["content"].([]any)
	if !ok || len(content) != 1 {
		t.Fatalf("content = %#v", message["content"])
	}
	block, ok := content[0].(map[string]any)
	if !ok || block["type"] != "text" || block["text"] != "say hello" {
		t.Fatalf("block = %#v", content[0])
	}
	tools, ok := requestBody["tools"].([]any)
	if !ok || len(tools) == 0 {
		t.Fatalf("missing builtin tools: %#v", requestBody["tools"])
	}
}

func TestRunChromeNativeHostRespondsToMessages(t *testing.T) {
	var stdin, stdout, stderr bytes.Buffer
	if err := integrationspkg.WriteChromeNativeMessage(&stdin, map[string]any{"type": "ping"}); err != nil {
		t.Fatal(err)
	}
	if err := integrationspkg.WriteChromeNativeMessage(&stdin, map[string]any{"type": "status"}); err != nil {
		t.Fatal(err)
	}
	if err := integrationspkg.WriteChromeNativeMessage(&stdin, map[string]any{"type": "hello"}); err != nil {
		t.Fatal(err)
	}
	if err := integrationspkg.WriteChromeNativeMessage(&stdin, map[string]any{"type": "session"}); err != nil {
		t.Fatal(err)
	}
	code := run([]string{"--chrome-native-host"}, &stdin, &stdout, &stderr)
	if code != 0 {
		t.Fatalf("exit = %d stderr=%s", code, stderr.String())
	}
	if stderr.Len() != 0 {
		t.Fatalf("stderr = %q", stderr.String())
	}
	pong := readNativeHostTestMessage(t, &stdout)
	if pong["type"] != "pong" || pong["ok"] != true {
		t.Fatalf("pong = %#v", pong)
	}
	status := readNativeHostTestMessage(t, &stdout)
	if status["type"] != "status" || status["runtime"] != "ccgo" || status["version"] != version {
		t.Fatalf("status = %#v", status)
	}
	if capabilities, ok := status["capabilities"].(map[string]any); !ok || capabilities["ping"] != true || capabilities["session"] != true {
		t.Fatalf("status capabilities = %#v", status["capabilities"])
	}
	hello := readNativeHostTestMessage(t, &stdout)
	if hello["type"] != "capabilities" || hello["protocol_version"] != chromeNativeHostProtocolVersion {
		t.Fatalf("hello = %#v", hello)
	}
	if capabilities, ok := hello["capabilities"].(map[string]any); !ok || capabilities["capabilities"] != true {
		t.Fatalf("hello capabilities = %#v", hello["capabilities"])
	}
	session := readNativeHostTestMessage(t, &stdout)
	if session["type"] != "session" || session["runtime"] != "ccgo" || session["protocol_version"] != chromeNativeHostProtocolVersion {
		t.Fatalf("session = %#v", session)
	}
}

func TestRunChromeNativeHostReportsUnsupportedMessage(t *testing.T) {
	var stdin, stdout, stderr bytes.Buffer
	if err := integrationspkg.WriteChromeNativeMessage(&stdin, map[string]any{"type": "unknown"}); err != nil {
		t.Fatal(err)
	}
	code := run([]string{"--chrome-native-host"}, &stdin, &stdout, &stderr)
	if code != 0 {
		t.Fatalf("exit = %d stderr=%s", code, stderr.String())
	}
	response := readNativeHostTestMessage(t, &stdout)
	if response["type"] != "error" || response["ok"] != false || !strings.Contains(fmt.Sprint(response["error"]), "unsupported message type") {
		t.Fatalf("response = %#v", response)
	}
}

func TestRunDaemonOnceWritesState(t *testing.T) {
	t.Setenv("CLAUDE_CONFIG_DIR", t.TempDir())
	cwd := t.TempDir()
	var stdout, stderr bytes.Buffer
	code := run([]string{"--cwd", cwd, "--daemon-once"}, strings.NewReader(""), &stdout, &stderr)
	if code != 0 {
		t.Fatalf("exit = %d stderr=%s", code, stderr.String())
	}
	if stderr.Len() != 0 {
		t.Fatalf("stderr = %q", stderr.String())
	}
	statePath := daemonStatePathFromOutput(t, stdout.String())
	state, err := daemonpkg.LoadState(statePath)
	if err != nil {
		t.Fatal(err)
	}
	resolvedCWD, err := filepath.EvalSymlinks(cwd)
	if err != nil {
		t.Fatal(err)
	}
	if state.RuntimeState != daemonpkg.RuntimeRunning || state.PID <= 0 || state.WorkingDirectory != resolvedCWD || state.HeartbeatAt == "" {
		t.Fatalf("daemon state = %#v", state)
	}
}

func TestRunDaemonOnceUsesInjectedSessionID(t *testing.T) {
	t.Setenv("CLAUDE_CONFIG_DIR", t.TempDir())
	cwd := t.TempDir()
	var stdout, stderr bytes.Buffer
	code := run([]string{"--cwd", cwd, "--daemon-session", "sess_fixed_daemon", "--daemon-once"}, strings.NewReader(""), &stdout, &stderr)
	if code != 0 {
		t.Fatalf("exit = %d stderr=%s", code, stderr.String())
	}
	statePath := daemonStatePathFromOutput(t, stdout.String())
	if !strings.Contains(statePath, "sess_fixed_daemon") {
		t.Fatalf("state path = %q", statePath)
	}
	state, err := daemonpkg.LoadState(statePath)
	if err != nil {
		t.Fatal(err)
	}
	if state.SessionID != "sess_fixed_daemon" {
		t.Fatalf("daemon state session = %q", state.SessionID)
	}
}

func TestRunDaemonStartLaunchesDetachedDaemon(t *testing.T) {
	t.Setenv("CLAUDE_CONFIG_DIR", t.TempDir())
	cwd := t.TempDir()
	oldStartDaemonProcess := startDaemonProcess
	defer func() { startDaemonProcess = oldStartDaemonProcess }()
	launches := 0
	startDaemonProcess = func(_ context.Context, options daemonProcessStartOptions) (int, error) {
		launches++
		if options.SessionID == "" || options.StatePath == "" || options.WorkingDirectory == "" {
			t.Fatalf("start options = %#v", options)
		}
		state := daemonpkg.BuildState(options.SessionID, options.WorkingDirectory, daemonpkg.RuntimeRunning, 4242, "http://127.0.0.1:4242", time.Now().UTC(), nil)
		if err := daemonpkg.WriteState(options.StatePath, state); err != nil {
			t.Fatal(err)
		}
		return 4242, nil
	}
	var stdout, stderr bytes.Buffer
	code := run([]string{"--cwd", cwd, "--daemon-start", "--daemon-heartbeat", "20ms"}, strings.NewReader(""), &stdout, &stderr)
	if code != 0 {
		t.Fatalf("daemon start exit = %d stderr=%s", code, stderr.String())
	}
	if launches != 1 || !strings.Contains(stdout.String(), "ccgo daemon started") || !strings.Contains(stdout.String(), "endpoint=http://127.0.0.1:4242") {
		t.Fatalf("launches=%d stdout=%q", launches, stdout.String())
	}
	var secondOut, secondErr bytes.Buffer
	code = run([]string{"--cwd", cwd, "--daemon-start"}, strings.NewReader(""), &secondOut, &secondErr)
	if code != 0 {
		t.Fatalf("second daemon start exit = %d stderr=%s", code, secondErr.String())
	}
	if launches != 1 || !strings.Contains(secondOut.String(), "ccgo daemon already running") {
		t.Fatalf("launches=%d second stdout=%q", launches, secondOut.String())
	}
}

func TestRunDaemonRestartStopsRunningDaemonBeforeStart(t *testing.T) {
	t.Setenv("CLAUDE_CONFIG_DIR", t.TempDir())
	cwd := t.TempDir()
	resolvedCWD, err := filepath.EvalSymlinks(cwd)
	if err != nil {
		t.Fatal(err)
	}
	oldSessionID := contracts.ID("sess_restart_old")
	oldStatePath := daemonpkg.SessionStatePath(session.TranscriptPath(resolvedCWD, oldSessionID), oldSessionID)
	var stopped bool
	var server *httptest.Server
	server = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/stop" {
			t.Fatalf("path = %s", r.URL.Path)
		}
		stopped = true
		disabled := daemonpkg.BuildState(oldSessionID, resolvedCWD, daemonpkg.RuntimeDisabled, 1111, server.URL, time.Now().UTC(), nil)
		if err := daemonpkg.WriteState(oldStatePath, disabled); err != nil {
			t.Fatal(err)
		}
		_ = json.NewEncoder(w).Encode(daemonpkg.StopResponse{OK: true, RuntimeState: daemonpkg.RuntimeDisabled})
	}))
	defer server.Close()
	running := daemonpkg.BuildState(oldSessionID, resolvedCWD, daemonpkg.RuntimeRunning, 1111, server.URL, time.Now().UTC(), nil)
	if err := daemonpkg.WriteState(oldStatePath, running); err != nil {
		t.Fatal(err)
	}
	oldStartDaemonProcess := startDaemonProcess
	defer func() { startDaemonProcess = oldStartDaemonProcess }()
	launches := 0
	startDaemonProcess = func(_ context.Context, options daemonProcessStartOptions) (int, error) {
		launches++
		state := daemonpkg.BuildState(options.SessionID, options.WorkingDirectory, daemonpkg.RuntimeRunning, 5151, "http://127.0.0.1:5151", time.Now().UTC(), nil)
		if err := daemonpkg.WriteState(options.StatePath, state); err != nil {
			t.Fatal(err)
		}
		return 5151, nil
	}
	var stdout, stderr bytes.Buffer
	code := run([]string{"--cwd", cwd, "--daemon-restart"}, strings.NewReader(""), &stdout, &stderr)
	if code != 0 {
		t.Fatalf("daemon restart exit = %d stderr=%s", code, stderr.String())
	}
	if !stopped || launches != 1 || !strings.Contains(stdout.String(), "ccgo daemon started") {
		t.Fatalf("stopped=%v launches=%d stdout=%q", stopped, launches, stdout.String())
	}
}

func TestRunDaemonDueSchedulesNoopsWithoutSchedules(t *testing.T) {
	runner := conversation.Runner{
		SessionID:        "sess_daemon_due",
		WorkingDirectory: t.TempDir(),
		SessionPath:      filepath.Join(t.TempDir(), "session.jsonl"),
	}
	if _, err := runDaemonDueSchedules(context.Background(), runner, time.Now().UTC()); err != nil {
		t.Fatal(err)
	}
}

func TestRunDaemonRemotePollInjectsRemoteTriggers(t *testing.T) {
	dir := t.TempDir()
	transcriptPath := filepath.Join(dir, "session.jsonl")
	sessionID := contracts.ID("sess_daemon_remote")
	manager := session.NewSidechainManager(transcriptPath, sessionID)
	if _, err := manager.Start(session.SidechainOptions{ID: "agent/remote-lead", StartedAt: time.Unix(100, 0).UTC()}); err != nil {
		t.Fatal(err)
	}
	if _, err := manager.Start(session.SidechainOptions{ID: "agent/remote-member", StartedAt: time.Unix(101, 0).UTC()}); err != nil {
		t.Fatal(err)
	}
	if _, _, err := manager.CreateTeam(session.TeamOptions{
		ID:                "remote/team",
		CoordinatorTaskID: "agent/remote-lead",
		TaskIDs:           []string{"agent/remote-member"},
		Timestamp:         time.Unix(102, 0).UTC(),
	}); err != nil {
		t.Fatal(err)
	}
	var cursors []string
	var auths []string
	var ackStatuses []string
	var server *httptest.Server
	server = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/ack" {
			var payload map[string]any
			if err := json.NewDecoder(r.Body).Decode(&payload); err != nil {
				t.Fatal(err)
			}
			ackStatuses = append(ackStatuses, fmt.Sprint(payload["status"]))
			if got := r.Header.Get("Authorization"); got != "Bearer poll-token" {
				t.Fatalf("ack auth = %q", got)
			}
			w.WriteHeader(http.StatusAccepted)
			return
		}
		cursors = append(cursors, r.URL.Query().Get("cursor"))
		auths = append(auths, r.Header.Get("Authorization"))
		w.Header().Set("content-type", "application/json")
		_, _ = w.Write([]byte(fmt.Sprintf(`{"next_cursor":"cursor-2","events":[{"id":"delivery-1","team":"remote/team","source":"webhook","event":"deploy","message":"Deploy now.","ack_url":%q,"lease":{"id":"lease-1","expires_at":"2026-06-17T12:00:00Z"}}]}`, server.URL+"/ack?token=secret")))
	}))
	defer server.Close()
	if err := remotepkg.WriteRegistrationState(remotepkg.SessionRegistrationPath(transcriptPath, sessionID), remotepkg.RegistrationState{
		SessionID:       sessionID,
		RuntimeState:    remotepkg.RegistrationRegistered,
		PollURL:         server.URL + "/poll?token=secret",
		RemoteSessionID: "remote-session",
	}); err != nil {
		t.Fatal(err)
	}
	runner := conversation.Runner{
		SessionID:        sessionID,
		SessionPath:      transcriptPath,
		WorkingDirectory: dir,
		MCP: &conversation.MCPConfig{UserSettings: contracts.Settings{
			Remote: &contracts.RemoteSetting{AuthToken: "poll-token"},
		}},
	}
	first := runDaemonRemotePoll(context.Background(), runner, time.Unix(200, 0).UTC())
	if first.StructuredContent["runtime_state"] != remotepkg.PumpRunning || first.StructuredContent["delivered_count"] != 1 || first.StructuredContent["duplicate_count"] != 0 || first.StructuredContent["ack_event_count"] != 1 || first.StructuredContent["ack_sent_count"] != 1 || first.StructuredContent["ack_error_count"] != 0 || first.StructuredContent["lease_event_count"] != 1 || first.StructuredContent["error_count"] != 0 {
		t.Fatalf("first poll = %#v", first.StructuredContent)
	}
	if len(cursors) != 1 || cursors[0] != "" || auths[0] != "Bearer poll-token" {
		t.Fatalf("first cursor/auth = %#v %#v", cursors, auths)
	}
	pump, err := remotepkg.LoadPumpState(remotepkg.SessionPumpPath(transcriptPath, sessionID))
	if err != nil {
		t.Fatal(err)
	}
	if pump.LastCursor != "cursor-2" || pump.PollURL != server.URL+"/poll" || pump.AckEventCount != 1 || pump.AckSentCount != 1 || pump.AckErrorCount != 0 || pump.LeaseEventCount != 1 || pump.DeliveredCount != 1 {
		t.Fatalf("pump = %#v", pump)
	}
	resume, err := manager.ResumeContext("agent/remote-lead", 3)
	if err != nil {
		t.Fatal(err)
	}
	if len(resume.Messages) != 2 || !strings.Contains(messages.TextContent(resume.Messages[1]), "Remote trigger received.") || !strings.Contains(messages.TextContent(resume.Messages[1]), "Deploy now.") {
		t.Fatalf("resume messages = %#v", resume.Messages)
	}
	second := runDaemonRemotePoll(context.Background(), runner, time.Unix(201, 0).UTC())
	if second.StructuredContent["delivered_count"] != 0 || second.StructuredContent["duplicate_count"] != 1 || second.StructuredContent["error_count"] != 0 {
		t.Fatalf("second poll = %#v", second.StructuredContent)
	}
	if len(ackStatuses) != 2 || ackStatuses[0] != "delivered" || ackStatuses[1] != "duplicate" {
		t.Fatalf("ack statuses = %#v", ackStatuses)
	}
	if len(cursors) != 2 || cursors[1] != "cursor-2" {
		t.Fatalf("cursors = %#v", cursors)
	}
	resume, err = manager.ResumeContext("agent/remote-lead", 5)
	if err != nil {
		t.Fatal(err)
	}
	if len(resume.Messages) != 2 {
		t.Fatalf("duplicate should not append, messages = %#v", resume.Messages)
	}
}

func TestRunDaemonRemotePollRetriesTransientPoll(t *testing.T) {
	dir := t.TempDir()
	transcriptPath := filepath.Join(dir, "session.jsonl")
	sessionID := contracts.ID("sess_daemon_remote_poll_retry")
	calls := 0
	var auths []string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls++
		auths = append(auths, r.Header.Get("Authorization"))
		if calls == 1 {
			w.Header().Set("Retry-After", "0")
			http.Error(w, "try again", http.StatusServiceUnavailable)
			return
		}
		w.Header().Set("content-type", "application/json")
		_, _ = w.Write([]byte(`{"next_cursor":"cursor-retry","events":[]}`))
	}))
	defer server.Close()
	if err := remotepkg.WriteRegistrationState(remotepkg.SessionRegistrationPath(transcriptPath, sessionID), remotepkg.RegistrationState{
		SessionID:       sessionID,
		RuntimeState:    remotepkg.RegistrationRegistered,
		PollURL:         server.URL + "/poll",
		RemoteSessionID: "remote-session",
	}); err != nil {
		t.Fatal(err)
	}
	runner := conversation.Runner{
		SessionID:        sessionID,
		SessionPath:      transcriptPath,
		WorkingDirectory: dir,
		MCP: &conversation.MCPConfig{UserSettings: contracts.Settings{
			Remote: &contracts.RemoteSetting{AuthToken: "poll-token"},
		}},
	}

	result := runDaemonRemotePoll(context.Background(), runner, time.Unix(200, 0).UTC())
	if result.StructuredContent["runtime_state"] != remotepkg.PumpRunning || result.StructuredContent["status_code"] != http.StatusOK || result.StructuredContent["attempt_count"] != 2 || result.StructuredContent["last_cursor"] != "cursor-retry" || result.StructuredContent["event_count"] != 0 || result.StructuredContent["error_count"] != 0 {
		t.Fatalf("retry poll = %#v", result.StructuredContent)
	}
	if calls != 2 || len(auths) != 2 || auths[0] != "Bearer poll-token" || auths[1] != "Bearer poll-token" {
		t.Fatalf("calls/auths = %d %#v", calls, auths)
	}
	pump, err := remotepkg.LoadPumpState(remotepkg.SessionPumpPath(transcriptPath, sessionID))
	if err != nil {
		t.Fatal(err)
	}
	if pump.StatusCode != http.StatusOK || pump.AttemptCount != 2 || pump.LastCursor != "cursor-retry" || pump.ErrorCount != 0 {
		t.Fatalf("pump = %#v", pump)
	}
}

func TestRunDaemonRemotePollSkipsExpiredLease(t *testing.T) {
	dir := t.TempDir()
	transcriptPath := filepath.Join(dir, "session.jsonl")
	sessionID := contracts.ID("sess_daemon_remote_expired_lease")
	manager := session.NewSidechainManager(transcriptPath, sessionID)
	if _, err := manager.Start(session.SidechainOptions{ID: "agent/remote-lead", StartedAt: time.Unix(100, 0).UTC()}); err != nil {
		t.Fatal(err)
	}
	if _, _, err := manager.CreateTeam(session.TeamOptions{
		ID:                "remote/team",
		CoordinatorTaskID: "agent/remote-lead",
		Timestamp:         time.Unix(102, 0).UTC(),
	}); err != nil {
		t.Fatal(err)
	}
	var ackStatuses []string
	var server *httptest.Server
	server = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/ack" {
			var payload map[string]any
			if err := json.NewDecoder(r.Body).Decode(&payload); err != nil {
				t.Fatal(err)
			}
			ackStatuses = append(ackStatuses, fmt.Sprint(payload["status"]))
			w.WriteHeader(http.StatusAccepted)
			return
		}
		w.Header().Set("content-type", "application/json")
		_, _ = w.Write([]byte(fmt.Sprintf(`{"events":[{"id":"delivery-expired","team":"remote/team","message":"Expired deploy.","ack_url":%q,"lease_id":"lease-expired","lease_expires_at":"2026-06-17T11:59:00Z"}]}`, server.URL+"/ack")))
	}))
	defer server.Close()
	if err := remotepkg.WriteRegistrationState(remotepkg.SessionRegistrationPath(transcriptPath, sessionID), remotepkg.RegistrationState{
		SessionID:    sessionID,
		RuntimeState: remotepkg.RegistrationRegistered,
		PollURL:      server.URL + "/poll",
	}); err != nil {
		t.Fatal(err)
	}
	runner := conversation.Runner{SessionID: sessionID, SessionPath: transcriptPath, WorkingDirectory: dir}
	result := runDaemonRemotePoll(context.Background(), runner, time.Date(2026, 6, 17, 12, 0, 0, 0, time.UTC))
	if result.StructuredContent["delivered_count"] != 0 || result.StructuredContent["duplicate_count"] != 0 || result.StructuredContent["ack_event_count"] != 1 || result.StructuredContent["ack_sent_count"] != 1 || result.StructuredContent["lease_event_count"] != 1 || result.StructuredContent["lease_expired_count"] != 1 || result.StructuredContent["error_count"] != 1 {
		t.Fatalf("expired poll = %#v", result.StructuredContent)
	}
	if len(ackStatuses) != 1 || ackStatuses[0] != "expired" {
		t.Fatalf("ack statuses = %#v", ackStatuses)
	}
	pump, err := remotepkg.LoadPumpState(remotepkg.SessionPumpPath(transcriptPath, sessionID))
	if err != nil {
		t.Fatal(err)
	}
	if pump.DeliveredCount != 0 || pump.AckSentCount != 1 || pump.LeaseExpiredCount != 1 || pump.ErrorCount != 1 {
		t.Fatalf("pump = %#v", pump)
	}
	resume, err := manager.ResumeContext("agent/remote-lead", 3)
	if err != nil {
		t.Fatal(err)
	}
	if len(resume.Messages) != 1 || strings.Contains(messages.TextContent(resume.Messages[0]), "Expired deploy.") {
		t.Fatalf("resume messages = %#v", resume.Messages)
	}
}

func TestRunDaemonRemotePollRenewsLeaseBeforeDelivery(t *testing.T) {
	dir := t.TempDir()
	transcriptPath := filepath.Join(dir, "session.jsonl")
	sessionID := contracts.ID("sess_daemon_remote_lease_renew")
	manager := session.NewSidechainManager(transcriptPath, sessionID)
	if _, err := manager.Start(session.SidechainOptions{ID: "agent/remote-lead", StartedAt: time.Unix(100, 0).UTC()}); err != nil {
		t.Fatal(err)
	}
	if _, _, err := manager.CreateTeam(session.TeamOptions{
		ID:                "remote/team",
		CoordinatorTaskID: "agent/remote-lead",
		Timestamp:         time.Unix(102, 0).UTC(),
	}); err != nil {
		t.Fatal(err)
	}
	var renewAuth string
	var renewPayload map[string]any
	var ackStatuses []string
	var server *httptest.Server
	server = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/leases/renew":
			renewAuth = r.Header.Get("Authorization")
			if err := json.NewDecoder(r.Body).Decode(&renewPayload); err != nil {
				t.Fatal(err)
			}
			w.Header().Set("content-type", "application/json")
			w.WriteHeader(http.StatusAccepted)
			_, _ = w.Write([]byte(`{"lease_expires_at":"2026-06-17T12:05:00Z"}`))
		case "/ack":
			var payload map[string]any
			if err := json.NewDecoder(r.Body).Decode(&payload); err != nil {
				t.Fatal(err)
			}
			ackStatuses = append(ackStatuses, fmt.Sprint(payload["status"]))
			w.WriteHeader(http.StatusAccepted)
		default:
			w.Header().Set("content-type", "application/json")
			_, _ = w.Write([]byte(fmt.Sprintf(`{"events":[{"id":"delivery-renew","team":"remote/team","message":"Renewed deploy.","ack_url":%q,"lease_id":"lease-renew","lease_expires_at":"2026-06-17T12:00:00Z"}]}`, server.URL+"/ack")))
		}
	}))
	defer server.Close()
	if err := remotepkg.WriteRegistrationState(remotepkg.SessionRegistrationPath(transcriptPath, sessionID), remotepkg.RegistrationState{
		SessionID:       sessionID,
		RuntimeState:    remotepkg.RegistrationRegistered,
		PollURL:         server.URL + "/poll?token=secret",
		LeaseRenewURL:   server.URL + "/leases/renew?token=secret",
		RemoteSessionID: "remote-session",
	}); err != nil {
		t.Fatal(err)
	}
	runner := conversation.Runner{
		SessionID:        sessionID,
		SessionPath:      transcriptPath,
		WorkingDirectory: dir,
		MCP: &conversation.MCPConfig{UserSettings: contracts.Settings{
			Remote: &contracts.RemoteSetting{AuthToken: "renew-token"},
		}},
	}
	result := runDaemonRemotePoll(context.Background(), runner, time.Date(2026, 6, 17, 11, 55, 0, 0, time.UTC))
	if result.StructuredContent["delivered_count"] != 1 || result.StructuredContent["lease_event_count"] != 1 || result.StructuredContent["lease_expired_count"] != 0 || result.StructuredContent["lease_renew_sent_count"] != 1 || result.StructuredContent["lease_renew_error_count"] != 0 || result.StructuredContent["ack_sent_count"] != 1 || result.StructuredContent["error_count"] != 0 {
		t.Fatalf("renew poll = %#v", result.StructuredContent)
	}
	if renewAuth != "Bearer renew-token" || renewPayload["event_id"] != "delivery-renew" || renewPayload["lease_id"] != "lease-renew" {
		t.Fatalf("renew auth=%q payload=%#v", renewAuth, renewPayload)
	}
	if len(ackStatuses) != 1 || ackStatuses[0] != "delivered" {
		t.Fatalf("ack statuses = %#v", ackStatuses)
	}
	pump, err := remotepkg.LoadPumpState(remotepkg.SessionPumpPath(transcriptPath, sessionID))
	if err != nil {
		t.Fatal(err)
	}
	if pump.DeliveredCount != 1 || pump.LeaseRenewSent != 1 || pump.LeaseRenewErrors != 0 || pump.ErrorCount != 0 {
		t.Fatalf("pump = %#v", pump)
	}
	resume, err := manager.ResumeContext("agent/remote-lead", 3)
	if err != nil {
		t.Fatal(err)
	}
	if len(resume.Messages) != 2 || !strings.Contains(messages.TextContent(resume.Messages[1]), "Renewed deploy.") {
		t.Fatalf("resume messages = %#v", resume.Messages)
	}
}

func TestRunDaemonRemotePollPrefersWebSocket(t *testing.T) {
	dir := t.TempDir()
	transcriptPath := filepath.Join(dir, "session.jsonl")
	sessionID := contracts.ID("sess_daemon_remote_ws")
	manager := session.NewSidechainManager(transcriptPath, sessionID)
	if _, err := manager.Start(session.SidechainOptions{ID: "agent/remote-lead", StartedAt: time.Unix(100, 0).UTC()}); err != nil {
		t.Fatal(err)
	}
	if _, _, err := manager.CreateTeam(session.TeamOptions{
		ID:                "remote/team",
		CoordinatorTaskID: "agent/remote-lead",
		Timestamp:         time.Unix(102, 0).UTC(),
	}); err != nil {
		t.Fatal(err)
	}
	var auths []string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		auths = append(auths, r.Header.Get("Authorization"))
		conn := acceptDaemonTestWebSocket(t, w, r)
		defer conn.Close()
		writeDaemonTestWebSocketFrame(t, conn, 0x1, []byte(`{"id":"delivery-ws","team":"remote/team","source":"websocket","event":"deploy","message":"WS deploy."}`))
		writeDaemonTestWebSocketFrame(t, conn, 0x8, []byte{0x03, 0xe8})
	}))
	defer server.Close()
	if err := remotepkg.WriteRegistrationState(remotepkg.SessionRegistrationPath(transcriptPath, sessionID), remotepkg.RegistrationState{
		SessionID:       sessionID,
		RuntimeState:    remotepkg.RegistrationRegistered,
		WebSocketURL:    "ws" + strings.TrimPrefix(server.URL, "http") + "/events?token=secret",
		PollURL:         "https://poll.example.invalid/events?token=secret",
		RemoteSessionID: "remote-session",
	}); err != nil {
		t.Fatal(err)
	}
	runner := conversation.Runner{
		SessionID:        sessionID,
		SessionPath:      transcriptPath,
		WorkingDirectory: dir,
		MCP: &conversation.MCPConfig{UserSettings: contracts.Settings{
			Remote: &contracts.RemoteSetting{AuthToken: "ws-token"},
		}},
	}
	result := runDaemonRemotePoll(context.Background(), runner, time.Unix(200, 0).UTC())
	if result.StructuredContent["runtime_state"] != remotepkg.PumpRunning || result.StructuredContent["transport"] != "websocket" || result.StructuredContent["frame_count"] != 1 || result.StructuredContent["connect_count"] != 1 || result.StructuredContent["close_code"] != 1000 || result.StructuredContent["delivered_count"] != 1 || result.StructuredContent["error_count"] != 0 {
		t.Fatalf("websocket poll = %#v", result.StructuredContent)
	}
	if len(auths) != 1 || auths[0] != "Bearer ws-token" {
		t.Fatalf("auths = %#v", auths)
	}
	pump, err := remotepkg.LoadPumpState(remotepkg.SessionPumpPath(transcriptPath, sessionID))
	if err != nil {
		t.Fatal(err)
	}
	if pump.Transport != "websocket" || pump.FrameCount != 1 || pump.ConnectCount != 1 || pump.CloseCode != 1000 || pump.DeliveredCount != 1 || strings.Contains(pump.WebSocketURL, "token=secret") {
		t.Fatalf("pump = %#v", pump)
	}
	resume, err := manager.ResumeContext("agent/remote-lead", 3)
	if err != nil {
		t.Fatal(err)
	}
	if len(resume.Messages) != 2 || !strings.Contains(messages.TextContent(resume.Messages[1]), "WS deploy.") {
		t.Fatalf("resume messages = %#v", resume.Messages)
	}
}

func TestRunDaemonRemoteStreamInjectsRemoteTriggers(t *testing.T) {
	dir := t.TempDir()
	transcriptPath := filepath.Join(dir, "session.jsonl")
	sessionID := contracts.ID("sess_daemon_remote_stream")
	manager := session.NewSidechainManager(transcriptPath, sessionID)
	if _, err := manager.Start(session.SidechainOptions{ID: "agent/remote-lead", StartedAt: time.Unix(100, 0).UTC()}); err != nil {
		t.Fatal(err)
	}
	if _, _, err := manager.CreateTeam(session.TeamOptions{
		ID:                "remote/team",
		CoordinatorTaskID: "agent/remote-lead",
		Timestamp:         time.Unix(102, 0).UTC(),
	}); err != nil {
		t.Fatal(err)
	}
	var auths []string
	var ackStatuses []string
	var pumpDuringAck remotepkg.PumpState
	var server *httptest.Server
	server = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/ack" {
			var payload map[string]any
			if err := json.NewDecoder(r.Body).Decode(&payload); err != nil {
				t.Fatal(err)
			}
			ackStatuses = append(ackStatuses, fmt.Sprint(payload["status"]))
			if got := r.Header.Get("Authorization"); got != "Bearer stream-token" {
				t.Fatalf("ack auth = %q", got)
			}
			loadedPump, err := remotepkg.LoadPumpState(remotepkg.SessionPumpPath(transcriptPath, sessionID))
			if err != nil {
				t.Fatal(err)
			}
			pumpDuringAck = loadedPump
			w.WriteHeader(http.StatusAccepted)
			return
		}
		auths = append(auths, r.Header.Get("Authorization"))
		conn := acceptDaemonTestWebSocket(t, w, r)
		defer conn.Close()
		writeDaemonTestWebSocketFrame(t, conn, 0x1, []byte(fmt.Sprintf(`{"id":"delivery-stream","team":"remote/team","source":"stream","event":"deploy","message":"Stream deploy.","ackUrl":%q,"lease_id":"lease-stream"}`, server.URL+"/ack")))
	}))
	defer server.Close()
	if err := remotepkg.WriteRegistrationState(remotepkg.SessionRegistrationPath(transcriptPath, sessionID), remotepkg.RegistrationState{
		SessionID:       sessionID,
		RuntimeState:    remotepkg.RegistrationRegistered,
		WebSocketURL:    "ws" + strings.TrimPrefix(server.URL, "http") + "/stream?token=secret",
		RemoteSessionID: "remote-session",
	}); err != nil {
		t.Fatal(err)
	}
	runner := conversation.Runner{
		SessionID:        sessionID,
		SessionPath:      transcriptPath,
		WorkingDirectory: dir,
		MCP: &conversation.MCPConfig{UserSettings: contracts.Settings{
			Remote: &contracts.RemoteSetting{AuthToken: "stream-token"},
		}},
	}
	result := runDaemonRemoteStream(context.Background(), runner, time.Unix(200, 0).UTC(), remotepkg.WebSocketOptions{MaxFrames: 1})
	if result.StructuredContent["runtime_state"] != remotepkg.PumpRunning || result.StructuredContent["transport"] != "websocket_stream" || result.StructuredContent["status_code"] != http.StatusSwitchingProtocols || result.StructuredContent["attempt_count"] != 1 || result.StructuredContent["frame_count"] != 1 || result.StructuredContent["connect_count"] != 1 || result.StructuredContent["ack_event_count"] != 1 || result.StructuredContent["ack_sent_count"] != 1 || result.StructuredContent["ack_error_count"] != 0 || result.StructuredContent["lease_event_count"] != 1 || result.StructuredContent["delivered_count"] != 1 || result.StructuredContent["error_count"] != 0 {
		t.Fatalf("stream result = %#v", result.StructuredContent)
	}
	if result.StructuredContent["stream_started_at"] != "1970-01-01T00:03:20Z" || result.StructuredContent["stream_ended_at"] == "" || result.StructuredContent["stream_stop_reason"] != "max_frames" {
		t.Fatalf("stream lifecycle result = %#v", result.StructuredContent)
	}
	if len(auths) != 1 || auths[0] != "Bearer stream-token" {
		t.Fatalf("auths = %#v", auths)
	}
	if len(ackStatuses) != 1 || ackStatuses[0] != "delivered" {
		t.Fatalf("ack statuses = %#v", ackStatuses)
	}
	if pumpDuringAck.Transport != "websocket_stream" || pumpDuringAck.StatusCode != http.StatusSwitchingProtocols || pumpDuringAck.AttemptCount != 1 || pumpDuringAck.FrameCount != 1 || pumpDuringAck.ConnectCount != 1 || pumpDuringAck.StreamEndedAt != "" {
		t.Fatalf("pump during ack = %#v", pumpDuringAck)
	}
	pump, err := remotepkg.LoadPumpState(remotepkg.SessionPumpPath(transcriptPath, sessionID))
	if err != nil {
		t.Fatal(err)
	}
	if pump.Transport != "websocket_stream" || pump.StatusCode != http.StatusSwitchingProtocols || pump.AttemptCount != 1 || pump.FrameCount != 1 || pump.ConnectCount != 1 || pump.AckEventCount != 1 || pump.AckSentCount != 1 || pump.AckErrorCount != 0 || pump.LeaseEventCount != 1 || pump.DeliveredCount != 1 || pump.StreamStartedAt != "1970-01-01T00:03:20Z" || pump.StreamEndedAt == "" || pump.StreamStopReason != "max_frames" || strings.Contains(pump.WebSocketURL, "token=secret") {
		t.Fatalf("pump = %#v", pump)
	}
	resume, err := manager.ResumeContext("agent/remote-lead", 3)
	if err != nil {
		t.Fatal(err)
	}
	if len(resume.Messages) != 2 || !strings.Contains(messages.TextContent(resume.Messages[1]), "Stream deploy.") {
		t.Fatalf("resume messages = %#v", resume.Messages)
	}
}

func TestRunDaemonTickSkipsRemotePollWhenWebSocketStreamRegistered(t *testing.T) {
	dir := t.TempDir()
	transcriptPath := filepath.Join(dir, "session.jsonl")
	sessionID := contracts.ID("sess_daemon_stream_skip")
	if err := remotepkg.WriteRegistrationState(remotepkg.SessionRegistrationPath(transcriptPath, sessionID), remotepkg.RegistrationState{
		SessionID:    sessionID,
		RuntimeState: remotepkg.RegistrationRegistered,
		WebSocketURL: "ws://127.0.0.1:1/stream?token=secret",
		PollURL:      "https://poll.example.invalid/events?token=secret",
	}); err != nil {
		t.Fatal(err)
	}
	runner := conversation.Runner{
		SessionID:        sessionID,
		SessionPath:      transcriptPath,
		WorkingDirectory: dir,
	}
	result, err := runDaemonTickWithOptions(context.Background(), runner, time.Unix(200, 0).UTC(), daemonTickOptions{SkipRemoteWhenWebSocket: true})
	if err != nil {
		t.Fatal(err)
	}
	remotePoll, ok := result.StructuredContent["remote_poll"].(map[string]any)
	if !ok {
		t.Fatalf("remote poll = %#v", result.StructuredContent["remote_poll"])
	}
	if remotePoll["transport"] != "websocket_stream" || remotePoll["skipped"] != true || remotePoll["error_count"] != 0 {
		t.Fatalf("remote poll = %#v", remotePoll)
	}
	pump, err := remotepkg.LoadPumpState(remotepkg.SessionPumpPath(transcriptPath, sessionID))
	if err != nil {
		t.Fatal(err)
	}
	if pump.RuntimeState != "" {
		t.Fatalf("pump should not be overwritten by skipped tick: %#v", pump)
	}
}

func TestRunDaemonOnceRefreshesRemoteManagedPolicyOnTick(t *testing.T) {
	root := t.TempDir()
	t.Setenv("CLAUDE_CONFIG_DIR", filepath.Join(root, "home"))
	t.Setenv("USER_TYPE", "ant")
	t.Setenv("CLAUDE_CODE_MANAGED_SETTINGS_PATH", filepath.Join(root, "missing-managed"))
	var requests int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		atomic.AddInt32(&requests, 1)
		w.Header().Set("content-type", "application/json")
		_, _ = w.Write([]byte(`{"settings":{"model":"remote-daemon"}}`))
	}))
	defer server.Close()
	t.Setenv("CLAUDE_CODE_REMOTE_MANAGED_SETTINGS_URL", server.URL+"/policy")
	cwd := filepath.Join(root, "project")
	if err := os.MkdirAll(cwd, 0o755); err != nil {
		t.Fatal(err)
	}
	state, err := bootstrap.New()
	if err != nil {
		t.Fatal(err)
	}
	resolvedCWD, err := filepath.EvalSymlinks(cwd)
	if err != nil {
		t.Fatal(err)
	}
	state.SetCWD(resolvedCWD)

	var stdout, stderr bytes.Buffer
	code := runDaemon(context.Background(), state, daemonOptions{Once: true, HeartbeatInterval: time.Millisecond}, &stdout, &stderr)
	if code != 0 {
		t.Fatalf("runDaemon code=%d stdout=%q stderr=%q", code, stdout.String(), stderr.String())
	}
	if got := atomic.LoadInt32(&requests); got < 2 {
		t.Fatalf("remote managed policy requests = %d, want initial load plus daemon tick refresh", got)
	}
	if !strings.Contains(stdout.String(), "ccgo daemon running") {
		t.Fatalf("stdout = %q", stdout.String())
	}
}

func TestRunDaemonServesHealthEndpoint(t *testing.T) {
	t.Setenv("CLAUDE_CONFIG_DIR", t.TempDir())
	cwd := t.TempDir()
	state, err := bootstrap.New()
	if err != nil {
		t.Fatal(err)
	}
	resolvedCWD, err := filepath.EvalSymlinks(cwd)
	if err != nil {
		t.Fatal(err)
	}
	state.SetCWD(resolvedCWD)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	var stdout, stderr bytes.Buffer
	done := make(chan int, 1)
	go func() {
		done <- runDaemon(ctx, state, daemonOptions{HeartbeatInterval: 10 * time.Millisecond}, &stdout, &stderr)
	}()
	statePath := waitForDaemonStatePath(t, &stdout)
	daemonState, err := daemonpkg.LoadState(statePath)
	if err != nil {
		t.Fatal(err)
	}
	if daemonState.Endpoint == "" {
		t.Fatalf("daemon state missing endpoint: %#v", daemonState)
	}
	resp, err := http.Get(daemonState.Endpoint + "/health")
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	var health daemonpkg.HealthResponse
	if err := json.NewDecoder(resp.Body).Decode(&health); err != nil {
		t.Fatal(err)
	}
	if !health.OK || health.SessionID != state.SessionID() || health.RuntimeState != daemonpkg.RuntimeRunning {
		t.Fatalf("health = %#v", health)
	}
	tickResp, err := http.Post(daemonState.Endpoint+"/tick", "application/json", nil)
	if err != nil {
		t.Fatal(err)
	}
	defer tickResp.Body.Close()
	var tick daemonpkg.TickResponse
	if err := json.NewDecoder(tickResp.Body).Decode(&tick); err != nil {
		t.Fatal(err)
	}
	if !tick.OK || tick.ErrorCount != 0 {
		t.Fatalf("tick = %#v", tick)
	}
	var statusOut, statusErr bytes.Buffer
	if code := run([]string{"--cwd", cwd, "--daemon-status"}, strings.NewReader(""), &statusOut, &statusErr); code != 0 {
		t.Fatalf("daemon status exit = %d stderr=%s", code, statusErr.String())
	}
	if !strings.Contains(statusOut.String(), "runtime_state=running") || !strings.Contains(statusOut.String(), "endpoint="+daemonState.Endpoint) {
		t.Fatalf("daemon status stdout = %q", statusOut.String())
	}
	var tickOut, tickErr bytes.Buffer
	if code := run([]string{"--cwd", cwd, "--daemon-tick"}, strings.NewReader(""), &tickOut, &tickErr); code != 0 {
		t.Fatalf("daemon tick exit = %d stderr=%s", code, tickErr.String())
	}
	if !strings.Contains(tickOut.String(), "ccgo daemon tick") || !strings.Contains(tickOut.String(), "error_count=0") {
		t.Fatalf("daemon tick stdout = %q", tickOut.String())
	}
	var stopOut, stopErr bytes.Buffer
	if code := run([]string{"--cwd", cwd, "--daemon-stop"}, strings.NewReader(""), &stopOut, &stopErr); code != 0 {
		t.Fatalf("daemon stop exit = %d stderr=%s", code, stopErr.String())
	}
	if !strings.Contains(stopOut.String(), "ccgo daemon stopped") || !strings.Contains(stopOut.String(), "runtime_state=disabled") {
		t.Fatalf("daemon stop stdout = %q", stopOut.String())
	}
	select {
	case code := <-done:
		if code != 0 {
			t.Fatalf("daemon exit = %d stderr=%s", code, stderr.String())
		}
	case <-time.After(time.Second):
		t.Fatal("daemon did not stop after cancel")
	}
	stoppedState, err := daemonpkg.LoadState(statePath)
	if err != nil {
		t.Fatal(err)
	}
	if stoppedState.RuntimeState != daemonpkg.RuntimeDisabled || stoppedState.Error != "" {
		t.Fatalf("stopped daemon state = %#v", stoppedState)
	}
}

func daemonStatePathFromOutput(t *testing.T, output string) string {
	t.Helper()
	for _, line := range strings.Split(output, "\n") {
		if value, ok := strings.CutPrefix(line, "state_path="); ok {
			return value
		}
	}
	t.Fatalf("daemon output missing state_path: %q", output)
	return ""
}

func waitForDaemonStatePath(t *testing.T, stdout *bytes.Buffer) string {
	t.Helper()
	deadline := time.Now().Add(time.Second)
	for time.Now().Before(deadline) {
		if statePath := daemonStatePathFromOutputIfPresent(stdout.String()); statePath != "" {
			return statePath
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatalf("daemon output missing state_path: %q", stdout.String())
	return ""
}

func daemonStatePathFromOutputIfPresent(output string) string {
	for _, line := range strings.Split(output, "\n") {
		if value, ok := strings.CutPrefix(line, "state_path="); ok {
			return value
		}
	}
	return ""
}

func readNativeHostTestMessage(t *testing.T, r io.Reader) map[string]any {
	t.Helper()
	raw, err := integrationspkg.ReadChromeNativeMessage(r, 1024)
	if err != nil {
		t.Fatal(err)
	}
	var message map[string]any
	if err := json.Unmarshal(raw, &message); err != nil {
		t.Fatal(err)
	}
	return message
}

func TestRunHelpExitsSuccessfully(t *testing.T) {
	t.Setenv("CLAUDE_CONFIG_DIR", t.TempDir())

	var stdout, stderr bytes.Buffer
	code := run([]string{"--help"}, strings.NewReader(""), &stdout, &stderr)
	if code != 0 {
		t.Fatalf("exit = %d stderr=%s", code, stderr.String())
	}
	if stdout.Len() != 0 {
		t.Fatalf("stdout = %q", stdout.String())
	}
	if !strings.Contains(stderr.String(), "Usage of claude:") {
		t.Fatalf("stderr = %q", stderr.String())
	}
}

func TestRunCWDFlagSetsScaffoldWorkingDirectory(t *testing.T) {
	project := t.TempDir()
	resolvedProject, err := filepath.EvalSymlinks(project)
	if err != nil {
		t.Fatal(err)
	}
	t.Setenv("CLAUDE_CONFIG_DIR", t.TempDir())

	var stdout, stderr bytes.Buffer
	code := run([]string{"--cwd", project}, strings.NewReader(""), &stdout, &stderr)
	if code != 0 {
		t.Fatalf("exit = %d stderr=%s", code, stderr.String())
	}
	if !strings.Contains(stdout.String(), "cwd="+resolvedProject) {
		t.Fatalf("stdout = %q", stdout.String())
	}
	if stderr.Len() != 0 {
		t.Fatalf("stderr = %q", stderr.String())
	}
}

func TestRunPrintReadsPromptFromStdinAndSettingsModel(t *testing.T) {
	var requestBody map[string]any
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if err := json.NewDecoder(r.Body).Decode(&requestBody); err != nil {
			t.Fatal(err)
		}
		w.Header().Set("content-type", "application/json")
		_, _ = w.Write([]byte(`{
			"id":"msg_2",
			"type":"message",
			"role":"assistant",
			"model":"claude-sonnet-4-6",
			"content":[{"type":"text","text":"stdin ok"}],
			"stop_reason":"end_turn"
		}`))
	}))
	defer server.Close()

	claudeHome := t.TempDir()
	if err := os.WriteFile(filepath.Join(claudeHome, "settings.json"), []byte(`{"model":"sonnet"}`), 0o600); err != nil {
		t.Fatal(err)
	}
	t.Setenv("ANTHROPIC_API_KEY", "test-key")
	t.Setenv("ANTHROPIC_BASE_URL", server.URL)
	t.Setenv("ANTHROPIC_MODEL", "")
	t.Setenv("CLAUDE_MODEL", "")
	t.Setenv("ANTHROPIC_BETA", "")
	t.Setenv("CLAUDE_CONFIG_DIR", claudeHome)

	var stdout, stderr bytes.Buffer
	code := run([]string{"-p"}, strings.NewReader("from stdin\n"), &stdout, &stderr)
	if code != 0 {
		t.Fatalf("exit = %d stderr=%s", code, stderr.String())
	}
	if got := stdout.String(); got != "stdin ok\n" {
		t.Fatalf("stdout = %q", got)
	}
	if requestBody["model"] != "claude-sonnet-4-6" {
		t.Fatalf("model = %#v", requestBody["model"])
	}
	messages := requestBody["messages"].([]any)
	content := messages[0].(map[string]any)["content"].([]any)
	if got := content[0].(map[string]any)["text"]; got != "from stdin" {
		t.Fatalf("prompt = %#v", got)
	}
}

func TestRunPrintCWDFlagLoadsProjectSettings(t *testing.T) {
	var requestBody map[string]any
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if err := json.NewDecoder(r.Body).Decode(&requestBody); err != nil {
			t.Fatal(err)
		}
		w.Header().Set("content-type", "application/json")
		_, _ = w.Write([]byte(`{
			"id":"msg_cwd",
			"type":"message",
			"role":"assistant",
			"model":"claude-haiku-4-5-20251001",
			"content":[{"type":"text","text":"cwd ok"}],
			"stop_reason":"end_turn"
		}`))
	}))
	defer server.Close()

	project := t.TempDir()
	if err := os.MkdirAll(filepath.Join(project, ".claude"), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(project, ".claude", "settings.json"), []byte(`{"model":"haiku"}`), 0o600); err != nil {
		t.Fatal(err)
	}
	t.Setenv("ANTHROPIC_API_KEY", "test-key")
	t.Setenv("ANTHROPIC_BASE_URL", server.URL)
	t.Setenv("ANTHROPIC_MODEL", "")
	t.Setenv("CLAUDE_MODEL", "")
	t.Setenv("ANTHROPIC_BETA", "")
	t.Setenv("CLAUDE_CONFIG_DIR", t.TempDir())

	var stdout, stderr bytes.Buffer
	code := run([]string{"--cwd", project, "--print", "cwd prompt"}, strings.NewReader(""), &stdout, &stderr)
	if code != 0 {
		t.Fatalf("exit = %d stderr=%s", code, stderr.String())
	}
	if got := stdout.String(); got != "cwd ok\n" {
		t.Fatalf("stdout = %q", got)
	}
	if requestBody["model"] != "claude-haiku-4-5-20251001" {
		t.Fatalf("model = %#v", requestBody["model"])
	}
	requestMessages := requestBody["messages"].([]any)
	if got := messageTextAt(t, requestMessages, 0); got != "cwd prompt" {
		t.Fatalf("prompt = %q", got)
	}
}

func TestHeadlessRunnerLoadsCLIProvidedMCPConfig(t *testing.T) {
	project := t.TempDir()
	if err := os.WriteFile(filepath.Join(project, "mcp.json"), []byte(`{
		"mcpServers": {
			"cli": {"command": "cli-server", "args": ["--stdio"]}
		}
	}`), 0o600); err != nil {
		t.Fatal(err)
	}
	t.Setenv("ANTHROPIC_API_KEY", "test-key")
	t.Setenv("CLAUDE_CONFIG_DIR", t.TempDir())

	state, err := bootstrap.New()
	if err != nil {
		t.Fatal(err)
	}
	state.SetCWD(project)
	runner, err := headlessRunner(context.Background(), state, cliOptions{MCPConfig: "mcp.json"})
	if err != nil {
		t.Fatal(err)
	}
	server := runner.MCP.LocalSettings.MCPServers["cli"]
	if server.Command != "cli-server" || len(server.Args) != 1 || server.Args[0] != "--stdio" {
		t.Fatalf("server = %#v", server)
	}
}

func TestRunPrintReadsJSONInputFormatPrompt(t *testing.T) {
	var requestBody map[string]any
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if err := json.NewDecoder(r.Body).Decode(&requestBody); err != nil {
			t.Fatal(err)
		}
		w.Header().Set("content-type", "application/json")
		_, _ = w.Write([]byte(`{
			"id":"msg_json_input",
			"type":"message",
			"role":"assistant",
			"model":"claude-sonnet-4-6",
			"content":[{"type":"text","text":"json input ok"}],
			"stop_reason":"end_turn"
		}`))
	}))
	defer server.Close()

	t.Setenv("ANTHROPIC_API_KEY", "test-key")
	t.Setenv("ANTHROPIC_BASE_URL", server.URL)
	t.Setenv("ANTHROPIC_MODEL", "")
	t.Setenv("CLAUDE_MODEL", "")
	t.Setenv("ANTHROPIC_BETA", "")
	t.Setenv("CLAUDE_CONFIG_DIR", t.TempDir())

	var stdout, stderr bytes.Buffer
	code := run([]string{"--print", "--input-format", "json"}, strings.NewReader(`{"prompt":"json prompt"}`), &stdout, &stderr)
	if code != 0 {
		t.Fatalf("exit = %d stderr=%s", code, stderr.String())
	}
	if got := stdout.String(); got != "json input ok\n" {
		t.Fatalf("stdout = %q", got)
	}
	requestMessages := requestBody["messages"].([]any)
	if got := messageTextAt(t, requestMessages, 0); got != "json prompt" {
		t.Fatalf("prompt = %q", got)
	}
}

func TestRunPrintReadsJSONInputFormatTextAlias(t *testing.T) {
	var requestBody map[string]any
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if err := json.NewDecoder(r.Body).Decode(&requestBody); err != nil {
			t.Fatal(err)
		}
		w.Header().Set("content-type", "application/json")
		_, _ = w.Write([]byte(`{
			"id":"msg_json_text_input",
			"type":"message",
			"role":"assistant",
			"model":"claude-sonnet-4-6",
			"content":[{"type":"text","text":"json text input ok"}],
			"stop_reason":"end_turn"
		}`))
	}))
	defer server.Close()

	t.Setenv("ANTHROPIC_API_KEY", "test-key")
	t.Setenv("ANTHROPIC_BASE_URL", server.URL)
	t.Setenv("ANTHROPIC_MODEL", "")
	t.Setenv("CLAUDE_MODEL", "")
	t.Setenv("ANTHROPIC_BETA", "")
	t.Setenv("CLAUDE_CONFIG_DIR", t.TempDir())

	var stdout, stderr bytes.Buffer
	code := run([]string{"--print", "--input-format", "json"}, strings.NewReader(`{"text":"text alias prompt"}`), &stdout, &stderr)
	if code != 0 {
		t.Fatalf("exit = %d stderr=%s", code, stderr.String())
	}
	if got := stdout.String(); got != "json text input ok\n" {
		t.Fatalf("stdout = %q", got)
	}
	requestMessages := requestBody["messages"].([]any)
	if got := messageTextAt(t, requestMessages, 0); got != "text alias prompt" {
		t.Fatalf("prompt = %q", got)
	}
}

func TestRunPrintReadsJSONInputFormatMessageWrapper(t *testing.T) {
	var requestBody map[string]any
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if err := json.NewDecoder(r.Body).Decode(&requestBody); err != nil {
			t.Fatal(err)
		}
		w.Header().Set("content-type", "application/json")
		_, _ = w.Write([]byte(`{
			"id":"msg_json_wrapped_input",
			"type":"message",
			"role":"assistant",
			"model":"claude-sonnet-4-6",
			"content":[{"type":"text","text":"json wrapped input ok"}],
			"stop_reason":"end_turn"
		}`))
	}))
	defer server.Close()

	t.Setenv("ANTHROPIC_API_KEY", "test-key")
	t.Setenv("ANTHROPIC_BASE_URL", server.URL)
	t.Setenv("ANTHROPIC_MODEL", "")
	t.Setenv("CLAUDE_MODEL", "")
	t.Setenv("ANTHROPIC_BETA", "")
	t.Setenv("CLAUDE_CONFIG_DIR", t.TempDir())

	input := `{"message":{"role":"user","content":[{"type":"text","text":"wrapped message prompt"}]}}`
	var stdout, stderr bytes.Buffer
	code := run([]string{"--print", "--input-format", "json"}, strings.NewReader(input), &stdout, &stderr)
	if code != 0 {
		t.Fatalf("exit = %d stderr=%s", code, stderr.String())
	}
	if got := stdout.String(); got != "json wrapped input ok\n" {
		t.Fatalf("stdout = %q", got)
	}
	requestMessages := requestBody["messages"].([]any)
	if got := messageTextAt(t, requestMessages, 0); got != "wrapped message prompt" {
		t.Fatalf("prompt = %q", got)
	}
}

func TestRunPrintReadsJSONInputFormatMessagesArray(t *testing.T) {
	var requestBody map[string]any
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if err := json.NewDecoder(r.Body).Decode(&requestBody); err != nil {
			t.Fatal(err)
		}
		w.Header().Set("content-type", "application/json")
		_, _ = w.Write([]byte(`{
			"id":"msg_json_messages_input",
			"type":"message",
			"role":"assistant",
			"model":"claude-sonnet-4-6",
			"content":[{"type":"text","text":"json messages input ok"}],
			"stop_reason":"end_turn"
		}`))
	}))
	defer server.Close()

	t.Setenv("ANTHROPIC_API_KEY", "test-key")
	t.Setenv("ANTHROPIC_BASE_URL", server.URL)
	t.Setenv("ANTHROPIC_MODEL", "")
	t.Setenv("CLAUDE_MODEL", "")
	t.Setenv("ANTHROPIC_BETA", "")
	t.Setenv("CLAUDE_CONFIG_DIR", t.TempDir())

	input := strings.Join([]string{
		`{"messages":[`,
		`{"role":"user","content":[{"type":"text","text":"older prompt"}]},`,
		`{"role":"assistant","content":[{"type":"text","text":"old answer"}]},`,
		`{"role":"user","content":[{"type":"text","text":"latest array prompt"}]}`,
		`]}`,
	}, "")
	var stdout, stderr bytes.Buffer
	code := run([]string{"--print", "--input-format", "json"}, strings.NewReader(input), &stdout, &stderr)
	if code != 0 {
		t.Fatalf("exit = %d stderr=%s", code, stderr.String())
	}
	if got := stdout.String(); got != "json messages input ok\n" {
		t.Fatalf("stdout = %q", got)
	}
	requestMessages := requestBody["messages"].([]any)
	if got := messageTextAt(t, requestMessages, 0); got != "latest array prompt" {
		t.Fatalf("prompt = %q", got)
	}
}

func TestRunPrintReadsStreamJSONInputFormatUserEvent(t *testing.T) {
	var requestBody map[string]any
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if err := json.NewDecoder(r.Body).Decode(&requestBody); err != nil {
			t.Fatal(err)
		}
		w.Header().Set("content-type", "application/json")
		_, _ = w.Write([]byte(`{
			"id":"msg_stream_input",
			"type":"message",
			"role":"assistant",
			"model":"claude-sonnet-4-6",
			"content":[{"type":"text","text":"stream input ok"}],
			"stop_reason":"end_turn"
		}`))
	}))
	defer server.Close()

	t.Setenv("ANTHROPIC_API_KEY", "test-key")
	t.Setenv("ANTHROPIC_BASE_URL", server.URL)
	t.Setenv("ANTHROPIC_MODEL", "")
	t.Setenv("CLAUDE_MODEL", "")
	t.Setenv("ANTHROPIC_BETA", "")
	t.Setenv("CLAUDE_CONFIG_DIR", t.TempDir())

	input := strings.Join([]string{
		`{"type":"system","status":"ready"}`,
		`{"type":"user","message":{"role":"user","content":[{"type":"text","text":"first prompt"}]}}`,
		`{"type":"user","message":{"role":"user","content":[{"type":"text","text":"latest prompt"}]}}`,
	}, "\n")

	var stdout, stderr bytes.Buffer
	code := run([]string{"--print", "--input-format", "stream-json"}, strings.NewReader(input), &stdout, &stderr)
	if code != 0 {
		t.Fatalf("exit = %d stderr=%s", code, stderr.String())
	}
	if got := stdout.String(); got != "stream input ok\n" {
		t.Fatalf("stdout = %q", got)
	}
	requestMessages := requestBody["messages"].([]any)
	if got := messageTextAt(t, requestMessages, 0); got != "latest prompt" {
		t.Fatalf("prompt = %q", got)
	}
}

func TestRunPrintReadsStreamJSONInputFormatUserMessageEventAlias(t *testing.T) {
	var requestBody map[string]any
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if err := json.NewDecoder(r.Body).Decode(&requestBody); err != nil {
			t.Fatal(err)
		}
		w.Header().Set("content-type", "application/json")
		_, _ = w.Write([]byte(`{
			"id":"msg_stream_alias_input",
			"type":"message",
			"role":"assistant",
			"model":"claude-sonnet-4-6",
			"content":[{"type":"text","text":"stream alias input ok"}],
			"stop_reason":"end_turn"
		}`))
	}))
	defer server.Close()

	t.Setenv("ANTHROPIC_API_KEY", "test-key")
	t.Setenv("ANTHROPIC_BASE_URL", server.URL)
	t.Setenv("ANTHROPIC_MODEL", "")
	t.Setenv("CLAUDE_MODEL", "")
	t.Setenv("ANTHROPIC_BETA", "")
	t.Setenv("CLAUDE_CONFIG_DIR", t.TempDir())

	input := strings.Join([]string{
		`{"type":"status","status":"ready"}`,
		`{"type":"user_message","message":{"content":[{"type":"text","text":"alias latest prompt"}]}}`,
	}, "\n")

	var stdout, stderr bytes.Buffer
	code := run([]string{"--print", "--input-format", "stream-json"}, strings.NewReader(input), &stdout, &stderr)
	if code != 0 {
		t.Fatalf("exit = %d stderr=%s", code, stderr.String())
	}
	if got := stdout.String(); got != "stream alias input ok\n" {
		t.Fatalf("stdout = %q", got)
	}
	requestMessages := requestBody["messages"].([]any)
	if got := messageTextAt(t, requestMessages, 0); got != "alias latest prompt" {
		t.Fatalf("prompt = %q", got)
	}
}

func TestRunPrintAcceptsCamelCaseFlagAliases(t *testing.T) {
	var requestBody map[string]any
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if err := json.NewDecoder(r.Body).Decode(&requestBody); err != nil {
			t.Fatal(err)
		}
		w.Header().Set("content-type", "application/json")
		_, _ = w.Write([]byte(`{
			"id":"msg_camel",
			"type":"message",
			"role":"assistant",
			"model":"claude-sonnet-4-6",
			"content":[{"type":"text","text":"camel ok"}],
			"stop_reason":"end_turn"
		}`))
	}))
	defer server.Close()

	t.Setenv("ANTHROPIC_API_KEY", "test-key")
	t.Setenv("ANTHROPIC_BASE_URL", server.URL)
	t.Setenv("ANTHROPIC_MODEL", "")
	t.Setenv("CLAUDE_MODEL", "")
	t.Setenv("ANTHROPIC_BETA", "")
	t.Setenv("CLAUDE_CONFIG_DIR", t.TempDir())

	var stdout, stderr bytes.Buffer
	code := run([]string{
		"--print",
		"--inputFormat", "json",
		"--outputFormat", "json",
		"--maxTokens", "23",
		"--systemPrompt", "Base",
		"--appendSystemPrompt", "Extra",
		`{"prompt":"camel prompt"}`,
	}, strings.NewReader(""), &stdout, &stderr)
	if code != 0 {
		t.Fatalf("exit = %d stderr=%s", code, stderr.String())
	}
	var payload map[string]any
	if err := json.Unmarshal(stdout.Bytes(), &payload); err != nil {
		t.Fatalf("invalid json stdout %q: %v", stdout.String(), err)
	}
	if payload["result"] != "camel ok" {
		t.Fatalf("payload = %#v", payload)
	}
	if requestBody["max_tokens"] != float64(23) {
		t.Fatalf("max_tokens = %#v", requestBody["max_tokens"])
	}
	if requestBody["system"] != "Base\n\nExtra" {
		t.Fatalf("system = %#v", requestBody["system"])
	}
	requestMessages := requestBody["messages"].([]any)
	if got := messageTextAt(t, requestMessages, 0); got != "camel prompt" {
		t.Fatalf("prompt = %q", got)
	}
}

func TestRunPrintSystemPromptFlags(t *testing.T) {
	var requestBody map[string]any
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if err := json.NewDecoder(r.Body).Decode(&requestBody); err != nil {
			t.Fatal(err)
		}
		w.Header().Set("content-type", "application/json")
		_, _ = w.Write([]byte(`{
			"id":"msg_system",
			"type":"message",
			"role":"assistant",
			"model":"claude-sonnet-4-6",
			"content":[{"type":"text","text":"system ok"}],
			"stop_reason":"end_turn"
		}`))
	}))
	defer server.Close()

	t.Setenv("ANTHROPIC_API_KEY", "test-key")
	t.Setenv("ANTHROPIC_BASE_URL", server.URL)
	t.Setenv("ANTHROPIC_MODEL", "")
	t.Setenv("CLAUDE_MODEL", "")
	t.Setenv("ANTHROPIC_BETA", "")
	t.Setenv("CLAUDE_CONFIG_DIR", t.TempDir())

	var stdout, stderr bytes.Buffer
	code := run([]string{"--print", "--system-prompt", "Base system", "--append-system-prompt", "Extra system", "hello"}, strings.NewReader(""), &stdout, &stderr)
	if code != 0 {
		t.Fatalf("exit = %d stderr=%s", code, stderr.String())
	}
	if requestBody["system"] != "Base system\n\nExtra system" {
		t.Fatalf("system = %#v", requestBody["system"])
	}
}

func TestRunPrintMaxTurnsLimitsToolLoop(t *testing.T) {
	var requests int
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requests++
		w.Header().Set("content-type", "application/json")
		_, _ = w.Write([]byte(`{
			"id":"msg_tool_round",
			"type":"message",
			"role":"assistant",
			"model":"claude-sonnet-4-6",
			"content":[{"type":"tool_use","id":"toolu_read","name":"Read","input":{"file_path":"cmd/claude/main.go"}}],
			"stop_reason":"tool_use"
		}`))
	}))
	defer server.Close()

	t.Setenv("ANTHROPIC_API_KEY", "test-key")
	t.Setenv("ANTHROPIC_BASE_URL", server.URL)
	t.Setenv("ANTHROPIC_MODEL", "")
	t.Setenv("CLAUDE_MODEL", "")
	t.Setenv("ANTHROPIC_BETA", "")
	t.Setenv("CLAUDE_CONFIG_DIR", t.TempDir())

	var stdout, stderr bytes.Buffer
	code := run([]string{"--print", "--max-turns", "1", "read once"}, strings.NewReader(""), &stdout, &stderr)
	if code == 0 {
		t.Fatalf("exit = %d", code)
	}
	if requests != 1 {
		t.Fatalf("requests = %d", requests)
	}
	if stdout.Len() != 0 {
		t.Fatalf("stdout = %q", stdout.String())
	}
	if !strings.Contains(stderr.String(), "maximum tool rounds exceeded: 1") {
		t.Fatalf("stderr = %q", stderr.String())
	}
}

func TestRunPrintRejectsNegativeMaxTurns(t *testing.T) {
	t.Setenv("ANTHROPIC_API_KEY", "")
	t.Setenv("CLAUDE_CODE_OAUTH_REFRESH_TOKEN", "")
	t.Setenv("CLAUDE_CONFIG_DIR", t.TempDir())

	var stdout, stderr bytes.Buffer
	code := run([]string{"--print", "--max-turns", "-1", "hello"}, strings.NewReader(""), &stdout, &stderr)
	if code == 0 {
		t.Fatalf("exit = %d", code)
	}
	if stdout.Len() != 0 {
		t.Fatalf("stdout = %q", stdout.String())
	}
	if !strings.Contains(stderr.String(), "invalid --max-turns -1") {
		t.Fatalf("stderr = %q", stderr.String())
	}
}

func TestRunPrintRejectsNegativeMaxTokens(t *testing.T) {
	t.Setenv("ANTHROPIC_API_KEY", "")
	t.Setenv("CLAUDE_CODE_OAUTH_REFRESH_TOKEN", "")
	t.Setenv("CLAUDE_CONFIG_DIR", t.TempDir())

	var stdout, stderr bytes.Buffer
	code := run([]string{"--print", "--max-tokens", "-1", "hello"}, strings.NewReader(""), &stdout, &stderr)
	if code == 0 {
		t.Fatalf("exit = %d", code)
	}
	if stdout.Len() != 0 {
		t.Fatalf("stdout = %q", stdout.String())
	}
	if !strings.Contains(stderr.String(), "invalid --max-tokens -1") {
		t.Fatalf("stderr = %q", stderr.String())
	}
}

func TestEffectivePermissionModeDangerouslySkipsPermissions(t *testing.T) {
	mode, err := effectivePermissionMode("", true)
	if err != nil {
		t.Fatal(err)
	}
	if mode != string(contracts.PermissionBypassPermissions) {
		t.Fatalf("mode = %q", mode)
	}
}

func TestPermissionDeciderFromCLIAllowDenyRules(t *testing.T) {
	allowed := parseToolRules(`Write, Bash(git status *)`)
	denied := parseToolRules(`Bash(rm *)`)
	decider, err := permissionDeciderFromSettings(nil, "dontAsk", allowed, denied, nil)
	if err != nil {
		t.Fatal(err)
	}
	ctx := tool.Context{Permissions: decider, WorkingDirectory: t.TempDir()}
	writeDecision, err := filetools.NewWriteTool().CheckPermissions(ctx, json.RawMessage(`{"file_path":"allowed.txt","content":"hi"}`))
	if err != nil {
		t.Fatal(err)
	}
	if writeDecision.Behavior != contracts.PermissionAllow {
		t.Fatalf("write decision = %#v", writeDecision)
	}
	bashDecision, err := bashtools.NewBashTool().CheckPermissions(ctx, json.RawMessage(`{"command":"rm -rf tmp"}`))
	if err != nil {
		t.Fatal(err)
	}
	if bashDecision.Behavior != contracts.PermissionDeny {
		t.Fatalf("bash decision = %#v", bashDecision)
	}
}

func TestPermissionDeciderHonorsManagedPermissionRulesOnly(t *testing.T) {
	managedOnly := true
	mcpConfig := &conversation.MCPConfig{
		UserSettings: contracts.Settings{Permissions: &contracts.PermissionsSetting{
			Allow: []string{"Write"},
		}},
		PolicySettings: contracts.Settings{
			AllowManagedPermissionRulesOnly: &managedOnly,
			Permissions: &contracts.PermissionsSetting{
				Allow: []string{"Bash(git status *)"},
			},
		},
	}
	decider, err := permissionDeciderFromSettings(mcpConfig, "", nil, nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	ctx := tool.Context{Permissions: decider, WorkingDirectory: t.TempDir()}
	allowed, err := bashtools.NewBashTool().CheckPermissions(ctx, json.RawMessage(`{"command":"git status --short"}`))
	if err != nil {
		t.Fatal(err)
	}
	if allowed.Behavior != contracts.PermissionAllow {
		t.Fatalf("managed decision = %#v", allowed)
	}
	userRule, err := filetools.NewWriteTool().CheckPermissions(ctx, json.RawMessage(`{"file_path":"managed-only.txt","content":"hi"}`))
	if err != nil {
		t.Fatal(err)
	}
	if userRule.Behavior == contracts.PermissionAllow {
		t.Fatalf("user rule should be stripped by managed-only policy: %#v", userRule)
	}
}

func TestParseToolRulesAcceptsRepeatedFlagValues(t *testing.T) {
	got := parseToolRules("Write", "Bash(git status *)", "Read, Edit")
	want := []string{"Write", "Bash(git status *)", "Read", "Edit"}
	if strings.Join(got, "|") != strings.Join(want, "|") {
		t.Fatalf("rules = %#v, want %#v", got, want)
	}
}

func TestRunPrintAccumulatesRepeatedAllowedToolFlags(t *testing.T) {
	var requests int
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requests++
		w.Header().Set("content-type", "application/json")
		if requests == 1 {
			_, _ = w.Write([]byte(`{
				"id":"msg_write_request",
				"type":"message",
				"role":"assistant",
				"model":"claude-sonnet-4-6",
				"content":[{"type":"tool_use","id":"toolu_write","name":"Write","input":{"file_path":"flag-write.txt","content":"written by repeated allow"}}],
				"stop_reason":"tool_use"
			}`))
			return
		}
		_, _ = w.Write([]byte(`{
			"id":"msg_write_done",
			"type":"message",
			"role":"assistant",
			"model":"claude-sonnet-4-6",
			"content":[{"type":"text","text":"rules ok"}],
			"stop_reason":"end_turn"
		}`))
	}))
	defer server.Close()

	project := t.TempDir()
	t.Setenv("ANTHROPIC_API_KEY", "test-key")
	t.Setenv("ANTHROPIC_BASE_URL", server.URL)
	t.Setenv("ANTHROPIC_MODEL", "")
	t.Setenv("CLAUDE_MODEL", "")
	t.Setenv("ANTHROPIC_BETA", "")
	t.Setenv("CLAUDE_CONFIG_DIR", t.TempDir())

	var stdout, stderr bytes.Buffer
	code := run([]string{
		"--cwd", project,
		"--print",
		"--permission-mode", "dontAsk",
		"--allowed-tools", "Write",
		"--allowedTools", "Bash(git status *)",
		"--disallowed-tools", "Bash(rm *)",
		"--disallowedTools", "Edit",
		"write with repeated flags",
	}, strings.NewReader(""), &stdout, &stderr)
	if code != 0 {
		t.Fatalf("exit = %d stderr=%s stdout=%s", code, stderr.String(), stdout.String())
	}
	if stdout.String() != "rules ok\n" {
		t.Fatalf("stdout = %q", stdout.String())
	}
	data, err := os.ReadFile(filepath.Join(project, "flag-write.txt"))
	if err != nil {
		t.Fatal(err)
	}
	if string(data) != "written by repeated allow" {
		t.Fatalf("written file = %q", string(data))
	}
}

func TestPermissionDeciderFromCLIAdditionalDirectories(t *testing.T) {
	base := t.TempDir()
	extra1 := filepath.Join(base, "extra 1")
	extra2 := filepath.Join(base, "extra2")
	decider, err := permissionDeciderFromSettings(nil, "", nil, nil, parsePathList([]string{extra1 + "," + extra2, extra1}))
	if err != nil {
		t.Fatal(err)
	}
	engineDecider, ok := decider.(tool.EnginePermissionDecider)
	if !ok {
		t.Fatalf("decider = %T", decider)
	}
	dirs := engineDecider.Engine.Context().AdditionalWorkingDirectories
	if dirs[extra1] != contracts.PermissionSourceCLIArg {
		t.Fatalf("extra1 source = %q dirs=%#v", dirs[extra1], dirs)
	}
	if dirs[extra2] != contracts.PermissionSourceCLIArg {
		t.Fatalf("extra2 source = %q dirs=%#v", dirs[extra2], dirs)
	}
	if len(dirs) != 2 {
		t.Fatalf("dirs = %#v", dirs)
	}
}

func TestRunPrintJSONOutput(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("content-type", "application/json")
		_, _ = w.Write([]byte(`{
			"id":"msg_json",
			"type":"message",
			"role":"assistant",
			"model":"claude-sonnet-4-6",
			"content":[{"type":"text","text":"json ok"}],
			"stop_reason":"end_turn",
			"usage":{"input_tokens":3,"output_tokens":4}
		}`))
	}))
	defer server.Close()

	t.Setenv("ANTHROPIC_API_KEY", "test-key")
	t.Setenv("ANTHROPIC_BASE_URL", server.URL)
	t.Setenv("ANTHROPIC_MODEL", "")
	t.Setenv("CLAUDE_MODEL", "")
	t.Setenv("ANTHROPIC_BETA", "")
	t.Setenv("CLAUDE_CONFIG_DIR", t.TempDir())

	var stdout, stderr bytes.Buffer
	code := run([]string{"--print", "--output-format", "json", "json prompt"}, strings.NewReader(""), &stdout, &stderr)
	if code != 0 {
		t.Fatalf("exit = %d stderr=%s", code, stderr.String())
	}
	var payload map[string]any
	if err := json.Unmarshal(stdout.Bytes(), &payload); err != nil {
		t.Fatalf("invalid json stdout %q: %v", stdout.String(), err)
	}
	if payload["type"] != "result" || payload["subtype"] != "success" || payload["is_error"] != false || payload["num_turns"] != float64(1) || payload["result"] != "json ok" {
		t.Fatalf("payload = %#v", payload)
	}
	if payload["session_id"] == "" {
		t.Fatalf("missing session_id: %#v", payload)
	}
	if payload["stop_reason"] != "end_turn" || payload["model"] != "claude-sonnet-4-6" {
		t.Fatalf("metadata = %#v", payload)
	}
	if _, ok := payload["duration_ms"].(float64); !ok {
		t.Fatalf("duration_ms = %#v", payload["duration_ms"])
	}
	if _, ok := payload["duration_api_ms"].(float64); !ok {
		t.Fatalf("duration_api_ms = %#v", payload["duration_api_ms"])
	}
	if cost, ok := payload["total_cost_usd"].(float64); !ok || cost <= 0 {
		t.Fatalf("total_cost_usd = %#v", payload["total_cost_usd"])
	}
	usage, ok := payload["usage"].(map[string]any)
	if !ok || usage["input_tokens"] != float64(3) || usage["output_tokens"] != float64(4) {
		t.Fatalf("usage = %#v", payload["usage"])
	}
	message, ok := payload["message"].(map[string]any)
	if !ok || message["type"] != "assistant" {
		t.Fatalf("message = %#v", payload["message"])
	}
}

func TestRunPrintJSONOutputIncludesRuntimeMetadata(t *testing.T) {
	t.Setenv("ANTHROPIC_API_KEY", "test-key")
	t.Setenv("ANTHROPIC_BASE_URL", "")
	t.Setenv("ANTHROPIC_MODEL", "")
	t.Setenv("CLAUDE_MODEL", "")
	t.Setenv("ANTHROPIC_BETA", "beta-one beta-two,beta-one")
	configHome := t.TempDir()
	t.Setenv("CLAUDE_CONFIG_DIR", configHome)
	if err := os.WriteFile(filepath.Join(configHome, "settings.json"), []byte(`{"outputStyle":"Explanatory","fastMode":true,"permissions":{"defaultMode":"plan"}}`), 0o644); err != nil {
		t.Fatal(err)
	}
	project := t.TempDir()
	expectedCWD, err := filepath.EvalSymlinks(project)
	if err != nil {
		t.Fatal(err)
	}

	var stdout, stderr bytes.Buffer
	code := run([]string{"--cwd", project, "--print", "--output-format", "json", "/status"}, strings.NewReader(""), &stdout, &stderr)
	if code != 0 {
		t.Fatalf("exit = %d stderr=%s", code, stderr.String())
	}
	var payload map[string]any
	if err := json.Unmarshal(stdout.Bytes(), &payload); err != nil {
		t.Fatalf("invalid json stdout %q: %v", stdout.String(), err)
	}
	if payload["cwd"] != expectedCWD || payload["permission_mode"] != "plan" || payload["api_key_source"] != "api_key" || payload["fast_mode"] != true {
		t.Fatalf("runtime metadata = %#v", payload)
	}
	if payload["output_style"] != "Explanatory" {
		t.Fatalf("output_style = %#v", payload["output_style"])
	}
	betas, ok := payload["betas"].([]any)
	if !ok || len(betas) != 3 || betas[0] != "beta-one" || betas[1] != "beta-two" || betas[2] != "fast-mode-2025-01-24" {
		t.Fatalf("betas = %#v", payload["betas"])
	}
	outputStyles, ok := payload["available_output_styles"].([]any)
	if !ok || len(outputStyles) < 3 || outputStyles[0] != "default" {
		t.Fatalf("available output styles = %#v", payload["available_output_styles"])
	}
}

func TestRunPrintJSONClearIncludesCleared(t *testing.T) {
	t.Setenv("ANTHROPIC_API_KEY", "test-key")
	t.Setenv("ANTHROPIC_BASE_URL", "")
	t.Setenv("ANTHROPIC_MODEL", "")
	t.Setenv("CLAUDE_MODEL", "")
	t.Setenv("ANTHROPIC_BETA", "")
	t.Setenv("CLAUDE_CONFIG_DIR", t.TempDir())

	var stdout, stderr bytes.Buffer
	code := run([]string{"--print", "--output-format", "json", "/clear"}, strings.NewReader(""), &stdout, &stderr)
	if code != 0 {
		t.Fatalf("exit = %d stderr=%s", code, stderr.String())
	}
	var payload map[string]any
	if err := json.Unmarshal(stdout.Bytes(), &payload); err != nil {
		t.Fatalf("invalid json stdout %q: %v", stdout.String(), err)
	}
	if payload["type"] != "result" || payload["subtype"] != "success" || payload["is_error"] != false || payload["cleared"] != true {
		t.Fatalf("payload = %#v", payload)
	}
	if payload["result"] != "" || payload["num_turns"] != nil {
		t.Fatalf("clear result metadata = %#v", payload)
	}
}

func TestRunPrintJSONLocalTextResult(t *testing.T) {
	t.Setenv("ANTHROPIC_API_KEY", "test-key")
	t.Setenv("ANTHROPIC_BASE_URL", "")
	t.Setenv("ANTHROPIC_MODEL", "")
	t.Setenv("CLAUDE_MODEL", "")
	t.Setenv("ANTHROPIC_BETA", "")
	t.Setenv("CLAUDE_CONFIG_DIR", t.TempDir())

	var stdout, stderr bytes.Buffer
	code := run([]string{"--print", "--output-format", "json", "/status"}, strings.NewReader(""), &stdout, &stderr)
	if code != 0 {
		t.Fatalf("exit = %d stderr=%s", code, stderr.String())
	}
	var payload map[string]any
	if err := json.Unmarshal(stdout.Bytes(), &payload); err != nil {
		t.Fatalf("invalid json stdout %q: %v", stdout.String(), err)
	}
	result, ok := payload["result"].(string)
	if !ok || !strings.Contains(result, "Status") || !strings.Contains(result, "Session ID:") {
		t.Fatalf("payload = %#v", payload)
	}
	if payload["num_turns"] != nil || payload["cleared"] != nil {
		t.Fatalf("local result metadata = %#v", payload)
	}
}

func TestRunPrintTextLocalTextResult(t *testing.T) {
	t.Setenv("ANTHROPIC_API_KEY", "test-key")
	t.Setenv("ANTHROPIC_BASE_URL", "")
	t.Setenv("ANTHROPIC_MODEL", "")
	t.Setenv("CLAUDE_MODEL", "")
	t.Setenv("ANTHROPIC_BETA", "")
	t.Setenv("CLAUDE_CONFIG_DIR", t.TempDir())

	var stdout, stderr bytes.Buffer
	code := run([]string{"--print", "/status"}, strings.NewReader(""), &stdout, &stderr)
	if code != 0 {
		t.Fatalf("exit = %d stderr=%s", code, stderr.String())
	}
	if text := stdout.String(); !strings.Contains(text, "Status") || !strings.Contains(text, "Session ID:") {
		t.Fatalf("stdout = %q", text)
	}
}

func TestRunPrintJSONModelCommandIncludesSelectedModel(t *testing.T) {
	t.Setenv("ANTHROPIC_API_KEY", "test-key")
	t.Setenv("ANTHROPIC_BASE_URL", "")
	t.Setenv("ANTHROPIC_MODEL", "")
	t.Setenv("CLAUDE_MODEL", "")
	t.Setenv("ANTHROPIC_BETA", "")
	t.Setenv("CLAUDE_CONFIG_DIR", t.TempDir())

	var stdout, stderr bytes.Buffer
	code := run([]string{"--print", "--output-format", "json", "/model opus"}, strings.NewReader(""), &stdout, &stderr)
	if code != 0 {
		t.Fatalf("exit = %d stderr=%s", code, stderr.String())
	}
	var payload map[string]any
	if err := json.Unmarshal(stdout.Bytes(), &payload); err != nil {
		t.Fatalf("invalid json stdout %q: %v", stdout.String(), err)
	}
	if payload["model"] != "claude-opus-4-6" || !strings.Contains(fmt.Sprint(payload["result"]), "Selected model: claude-opus-4-6") {
		t.Fatalf("payload = %#v", payload)
	}
}

func TestRunPrintJSONOutputIncludesErrorResult(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("content-type", "application/json")
		_, _ = w.Write([]byte(`{`))
	}))
	defer server.Close()

	t.Setenv("ANTHROPIC_API_KEY", "test-key")
	t.Setenv("ANTHROPIC_BASE_URL", server.URL)
	t.Setenv("ANTHROPIC_MODEL", "")
	t.Setenv("CLAUDE_MODEL", "")
	t.Setenv("ANTHROPIC_BETA", "")
	t.Setenv("CLAUDE_CONFIG_DIR", t.TempDir())

	var stdout, stderr bytes.Buffer
	code := run([]string{"--print", "--output-format", "json", "fail prompt"}, strings.NewReader(""), &stdout, &stderr)
	if code == 0 {
		t.Fatalf("exit = %d", code)
	}
	var payload map[string]any
	if err := json.Unmarshal(stdout.Bytes(), &payload); err != nil {
		t.Fatalf("invalid json stdout %q: %v", stdout.String(), err)
	}
	if payload["type"] != "result" || payload["subtype"] != "error" || payload["is_error"] != true || payload["error"] == "" {
		t.Fatalf("payload = %#v", payload)
	}
	if _, ok := payload["duration_ms"].(float64); !ok {
		t.Fatalf("duration_ms = %#v", payload["duration_ms"])
	}
	if _, ok := payload["duration_api_ms"].(float64); !ok {
		t.Fatalf("duration_api_ms = %#v", payload["duration_api_ms"])
	}
	if !strings.Contains(stderr.String(), "ccgo:") {
		t.Fatalf("stderr = %q", stderr.String())
	}
}

func TestRunPrintStreamJSONOutput(t *testing.T) {
	var betaHeader string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		betaHeader = r.Header.Get("anthropic-beta")
		w.Header().Set("content-type", "application/json")
		_, _ = w.Write([]byte(`{
			"id":"msg_stream",
			"type":"message",
			"role":"assistant",
			"model":"claude-sonnet-4-6",
			"content":[{"type":"text","text":"stream ok"}],
			"stop_reason":"end_turn"
		}`))
	}))
	defer server.Close()

	t.Setenv("ANTHROPIC_API_KEY", "test-key")
	t.Setenv("ANTHROPIC_BASE_URL", server.URL)
	t.Setenv("ANTHROPIC_MODEL", "")
	t.Setenv("CLAUDE_MODEL", "")
	t.Setenv("ANTHROPIC_BETA", "beta-one beta-two,beta-one")
	configHome := t.TempDir()
	t.Setenv("CLAUDE_CONFIG_DIR", configHome)
	if err := os.WriteFile(filepath.Join(configHome, "settings.json"), []byte(`{"outputStyle":"Explanatory","fastMode":true,"permissions":{"defaultMode":"plan"},"mcpServers":{"docs":{"type":"http","url":"https://docs.example/mcp"}}}`), 0o644); err != nil {
		t.Fatal(err)
	}
	project := t.TempDir()
	pluginRoot := filepath.Join(project, ".claude", "plugins", "demo")
	if err := os.MkdirAll(filepath.Join(project, ".git"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Join(pluginRoot, "skills", "review"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Join(pluginRoot, "agents"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(pluginRoot, "plugin.json"), []byte(`{"name":"demo","version":"1.0.0"}`), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(pluginRoot, "skills", "review", "SKILL.md"), []byte("---\ndescription: Review code\n---\nReview ${CLAUDE_SKILL_DIR}."), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(pluginRoot, "agents", "reviewer.md"), []byte("---\nname: reviewer\ndescription: Review changes\n---\nReview."), 0o644); err != nil {
		t.Fatal(err)
	}
	expectedPluginRoot, err := filepath.EvalSymlinks(pluginRoot)
	if err != nil {
		t.Fatal(err)
	}

	var stdout, stderr bytes.Buffer
	code := run([]string{"--cwd", project, "--print", "--output-format", "stream-json", "stream prompt"}, strings.NewReader(""), &stdout, &stderr)
	if code != 0 {
		t.Fatalf("exit = %d stderr=%s", code, stderr.String())
	}
	if betaHeader != "beta-one,beta-two,fast-mode-2025-01-24" {
		t.Fatalf("anthropic-beta = %q", betaHeader)
	}
	lines := strings.Split(strings.TrimSpace(stdout.String()), "\n")
	if len(lines) != 4 {
		t.Fatalf("lines = %#v", lines)
	}
	var events []map[string]any
	for _, line := range lines {
		var event map[string]any
		if err := json.Unmarshal([]byte(line), &event); err != nil {
			t.Fatalf("invalid json line %q: %v", line, err)
		}
		events = append(events, event)
	}
	if events[0]["type"] != "system" || events[0]["subtype"] != "init" {
		t.Fatalf("init event = %#v", events[0])
	}
	if events[0]["session_id"] == "" || events[0]["cwd"] == "" {
		t.Fatalf("init metadata = %#v", events[0])
	}
	if events[0]["output_style"] != "Explanatory" {
		t.Fatalf("init output style = %#v", events[0])
	}
	if events[0]["permission_mode"] != "plan" || events[0]["api_key_source"] != "api_key" || events[0]["fast_mode"] != true {
		t.Fatalf("init runtime metadata = %#v", events[0])
	}
	betas, ok := events[0]["betas"].([]any)
	if !ok || len(betas) != 3 || betas[0] != "beta-one" || betas[1] != "beta-two" || betas[2] != "fast-mode-2025-01-24" {
		t.Fatalf("init betas = %#v", events[0]["betas"])
	}
	outputStyles, ok := events[0]["available_output_styles"].([]any)
	if !ok || len(outputStyles) < 3 || outputStyles[0] != "default" {
		t.Fatalf("init available output styles = %#v", events[0]["available_output_styles"])
	}
	slashCommands, ok := events[0]["slash_commands"].([]any)
	if !ok || !containsAnyString(slashCommands, "help") || !containsAnyString(slashCommands, "output-style") {
		t.Fatalf("init slash commands = %#v", events[0]["slash_commands"])
	}
	skills, ok := events[0]["skills"].([]any)
	if !ok || !containsAnyString(skills, "demo:review") {
		t.Fatalf("init skills = %#v", events[0]["skills"])
	}
	agents, ok := events[0]["agents"].([]any)
	if !ok || !containsAnyString(agents, "demo:reviewer") {
		t.Fatalf("init agents = %#v", events[0]["agents"])
	}
	plugins, ok := events[0]["plugins"].([]any)
	if !ok || !containsPluginSummary(plugins, "demo", expectedPluginRoot, "local") {
		t.Fatalf("init plugins = %#v", events[0]["plugins"])
	}
	mcpServers, ok := events[0]["mcp_servers"].([]any)
	if !ok || !containsMCPServerSummary(mcpServers, "docs", "configured", "http", "user", "user", "") {
		t.Fatalf("init mcp servers = %#v", events[0]["mcp_servers"])
	}
	tools, ok := events[0]["tools"].([]any)
	if !ok || len(tools) == 0 {
		t.Fatalf("init tools = %#v", events[0]["tools"])
	}
	if events[1]["type"] != "user_message" || events[2]["type"] != "assistant_message" || events[3]["type"] != "result" {
		t.Fatalf("events = %#v", events)
	}
	if events[3]["result"] != "stream ok" || events[3]["is_error"] != false || events[3]["num_turns"] != float64(1) {
		t.Fatalf("result event = %#v", events[3])
	}
	if _, ok := events[3]["duration_ms"].(float64); !ok {
		t.Fatalf("result duration_ms = %#v", events[3]["duration_ms"])
	}
	if _, ok := events[3]["duration_api_ms"].(float64); !ok {
		t.Fatalf("result duration_api_ms = %#v", events[3]["duration_api_ms"])
	}
}

func TestRunPluginListJSONAvailable(t *testing.T) {
	configHome := t.TempDir()
	t.Setenv("CLAUDE_CONFIG_DIR", configHome)
	project := t.TempDir()
	if err := os.MkdirAll(filepath.Join(project, ".git"), 0o755); err != nil {
		t.Fatal(err)
	}
	installedDir := filepath.Join(project, ".claude", "plugins", "market-demo")
	marketDir := filepath.Join(t.TempDir(), "market-demo")
	lintDir := filepath.Join(t.TempDir(), "lint-tool")
	for _, dir := range []string{installedDir, marketDir, lintDir} {
		if err := os.MkdirAll(dir, 0o755); err != nil {
			t.Fatal(err)
		}
	}
	if err := os.WriteFile(filepath.Join(installedDir, "plugin.json"), []byte(`{"name":"market demo","version":"1.0.0"}`), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(marketDir, "plugin.json"), []byte(`{"name":"market demo","version":"2.0.0","description":"Deploy marketplace plugin"}`), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(lintDir, "plugin.json"), []byte(`{"name":"lint tool","version":"1.0.0","description":"Static checks"}`), 0o644); err != nil {
		t.Fatal(err)
	}
	settings := fmt.Sprintf(`{
		"enabledPlugins": {"market demo": false},
		"extraKnownMarketplaces": {
			"team": {"source": {"source": "settings", "name": "team", "plugins": [%q, %q]}}
		},
		"strictKnownMarketplaces": ["team"]
	}`, marketDir, lintDir)
	if err := os.WriteFile(filepath.Join(configHome, "settings.json"), []byte(settings), 0o644); err != nil {
		t.Fatal(err)
	}

	var stdout, stderr bytes.Buffer
	code := run([]string{"--cwd", project, "plugin", "list", "--json", "--available"}, strings.NewReader(""), &stdout, &stderr)
	if code != 0 {
		t.Fatalf("exit = %d stderr=%s", code, stderr.String())
	}
	var payload map[string]any
	if err := json.Unmarshal(stdout.Bytes(), &payload); err != nil {
		t.Fatalf("invalid json stdout %q: %v", stdout.String(), err)
	}
	installed, ok := payload["installed"].([]any)
	if !ok || len(installed) != 1 {
		t.Fatalf("installed = %#v", payload["installed"])
	}
	installedPlugin, ok := installed[0].(map[string]any)
	if !ok {
		t.Fatalf("installed plugin = %#v", installed[0])
	}
	expectedInstalledDir, err := filepath.EvalSymlinks(installedDir)
	if err != nil {
		t.Fatal(err)
	}
	if installedPlugin["id"] != "market demo@local" || installedPlugin["version"] != "1.0.0" || installedPlugin["enabled"] != false || installedPlugin["installPath"] != expectedInstalledDir {
		t.Fatalf("installed plugin = %#v", installedPlugin)
	}
	available, ok := payload["available"].([]any)
	if !ok || len(available) != 1 {
		t.Fatalf("available = %#v", payload["available"])
	}
	availablePlugin, ok := available[0].(map[string]any)
	if !ok {
		t.Fatalf("available plugin = %#v", available[0])
	}
	if availablePlugin["pluginId"] != "lint tool@team" || availablePlugin["name"] != "lint tool" || availablePlugin["marketplaceName"] != "team" || availablePlugin["version"] != "1.0.0" || availablePlugin["description"] != "Static checks" {
		t.Fatalf("available plugin = %#v", availablePlugin)
	}

	stdout.Reset()
	stderr.Reset()
	code = run([]string{"--cwd", project, "plugin", "list", "--available"}, strings.NewReader(""), &stdout, &stderr)
	if code != 2 || !strings.Contains(stderr.String(), "--available requires --json") {
		t.Fatalf("available without json exit=%d stdout=%q stderr=%q", code, stdout.String(), stderr.String())
	}
}

func TestRunPluginInstallCLI(t *testing.T) {
	configHome := t.TempDir()
	t.Setenv("CLAUDE_CONFIG_DIR", configHome)
	project := t.TempDir()
	if err := os.MkdirAll(filepath.Join(project, ".git"), 0o755); err != nil {
		t.Fatal(err)
	}
	marketDir := filepath.Join(t.TempDir(), "market-demo")
	if err := os.MkdirAll(filepath.Join(marketDir, "assets"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(marketDir, "plugin.json"), []byte(`{"name":"market demo","version":"1.0.0","description":"Deploy marketplace plugin"}`), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(marketDir, "assets", "README.md"), []byte("asset"), 0o644); err != nil {
		t.Fatal(err)
	}
	settings := fmt.Sprintf(`{
		"extraKnownMarketplaces": {
			"team": {"source": {"source": "settings", "name": "team", "plugins": [%q]}}
		},
		"strictKnownMarketplaces": ["team"]
	}`, marketDir)
	if err := os.WriteFile(filepath.Join(configHome, "settings.json"), []byte(settings), 0o644); err != nil {
		t.Fatal(err)
	}

	var stdout, stderr bytes.Buffer
	code := run([]string{"--cwd", project, "plugin", "install", "--scope", "project", "market demo"}, strings.NewReader(""), &stdout, &stderr)
	if code != 0 {
		t.Fatalf("exit = %d stderr=%s", code, stderr.String())
	}
	resolvedProject, err := filepath.EvalSymlinks(project)
	if err != nil {
		t.Fatal(err)
	}
	installedDir := filepath.Join(resolvedProject, ".claude", "plugins", "market-demo")
	for _, want := range []string{
		"Plugin installed",
		"Name: market demo",
		"Marketplace: team",
		"Installed path: " + installedDir,
		"Status: installed",
	} {
		if !strings.Contains(stdout.String(), want) {
			t.Fatalf("stdout missing %q: %q", want, stdout.String())
		}
	}
	if data, err := os.ReadFile(filepath.Join(installedDir, "assets", "README.md")); err != nil || string(data) != "asset" {
		t.Fatalf("installed asset data=%q err=%v", data, err)
	}

	stdout.Reset()
	stderr.Reset()
	code = run([]string{"--cwd", project, "plugin", "install", "market demo"}, strings.NewReader(""), &stdout, &stderr)
	if code != 0 {
		t.Fatalf("second install exit = %d stderr=%s", code, stderr.String())
	}
	if !strings.Contains(stdout.String(), "Status: already installed") {
		t.Fatalf("second install stdout = %q", stdout.String())
	}

	stdout.Reset()
	stderr.Reset()
	code = run([]string{"--cwd", project, "plugin", "install", "--scope", "user", "market demo"}, strings.NewReader(""), &stdout, &stderr)
	if code != 2 || !strings.Contains(stderr.String(), `scope "user" is not supported yet`) {
		t.Fatalf("user scope exit=%d stdout=%q stderr=%q", code, stdout.String(), stderr.String())
	}
}

func TestRunPluginUpdateCLI(t *testing.T) {
	configHome := t.TempDir()
	t.Setenv("CLAUDE_CONFIG_DIR", configHome)
	project := t.TempDir()
	if err := os.MkdirAll(filepath.Join(project, ".git"), 0o755); err != nil {
		t.Fatal(err)
	}
	marketDir := filepath.Join(t.TempDir(), "market-demo")
	if err := os.MkdirAll(filepath.Join(marketDir, "assets"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(marketDir, "plugin.json"), []byte(`{"name":"market demo","version":"1.0.0","description":"Deploy marketplace plugin"}`), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(marketDir, "assets", "README.md"), []byte("v1"), 0o644); err != nil {
		t.Fatal(err)
	}
	settings := fmt.Sprintf(`{
		"extraKnownMarketplaces": {
			"team": {"source": {"source": "settings", "name": "team", "plugins": [%q]}}
		},
		"strictKnownMarketplaces": ["team"]
	}`, marketDir)
	if err := os.WriteFile(filepath.Join(configHome, "settings.json"), []byte(settings), 0o644); err != nil {
		t.Fatal(err)
	}

	var stdout, stderr bytes.Buffer
	code := run([]string{"--cwd", project, "plugin", "install", "--scope", "project", "market demo"}, strings.NewReader(""), &stdout, &stderr)
	if code != 0 {
		t.Fatalf("install exit = %d stderr=%s", code, stderr.String())
	}
	if err := os.WriteFile(filepath.Join(marketDir, "plugin.json"), []byte(`{"name":"market demo","version":"2.0.0","description":"Deploy marketplace plugin"}`), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(marketDir, "assets", "README.md"), []byte("v2"), 0o644); err != nil {
		t.Fatal(err)
	}
	resolvedProject, err := filepath.EvalSymlinks(project)
	if err != nil {
		t.Fatal(err)
	}
	installedDir := filepath.Join(resolvedProject, ".claude", "plugins", "market-demo")

	stdout.Reset()
	stderr.Reset()
	code = run([]string{"--cwd", project, "plugin", "update", "--scope", "project", "market demo"}, strings.NewReader(""), &stdout, &stderr)
	if code != 0 {
		t.Fatalf("update exit = %d stderr=%s", code, stderr.String())
	}
	for _, want := range []string{
		"Plugin update",
		"Marketplace plugins: 1",
		"Updated plugins: 1",
		"- market demo -> " + installedDir,
	} {
		if !strings.Contains(stdout.String(), want) {
			t.Fatalf("stdout missing %q: %q", want, stdout.String())
		}
	}
	if data, err := os.ReadFile(filepath.Join(installedDir, "plugin.json")); err != nil || !strings.Contains(string(data), `"version":"2.0.0"`) {
		t.Fatalf("updated plugin json=%q err=%v", data, err)
	}
	if data, err := os.ReadFile(filepath.Join(installedDir, "assets", "README.md")); err != nil || string(data) != "v2" {
		t.Fatalf("updated asset data=%q err=%v", data, err)
	}

	stdout.Reset()
	stderr.Reset()
	code = run([]string{"--cwd", project, "plugin", "update", "--scope", "user", "market demo"}, strings.NewReader(""), &stdout, &stderr)
	if code != 2 || !strings.Contains(stderr.String(), `scope "user" is not supported yet`) {
		t.Fatalf("user scope exit=%d stdout=%q stderr=%q", code, stdout.String(), stderr.String())
	}

	stdout.Reset()
	stderr.Reset()
	code = run([]string{"--cwd", project, "plugin", "update", "missing"}, strings.NewReader(""), &stdout, &stderr)
	if code != 1 || !strings.Contains(stderr.String(), "installed plugin missing was not found") {
		t.Fatalf("missing exit=%d stdout=%q stderr=%q", code, stdout.String(), stderr.String())
	}
}

func TestRunPluginEnableDisableCLI(t *testing.T) {
	configHome := t.TempDir()
	t.Setenv("CLAUDE_CONFIG_DIR", configHome)
	project := t.TempDir()
	if err := os.MkdirAll(filepath.Join(project, ".git"), 0o755); err != nil {
		t.Fatal(err)
	}

	var stdout, stderr bytes.Buffer
	code := run([]string{"--cwd", project, "plugin", "enable", "market/plugin"}, strings.NewReader(""), &stdout, &stderr)
	if code != 0 {
		t.Fatalf("enable exit = %d stderr=%s", code, stderr.String())
	}
	if !strings.Contains(stdout.String(), "Plugin market/plugin enabled.") {
		t.Fatalf("enable stdout = %q", stdout.String())
	}
	settings := readTestSettingsJSON(t, filepath.Join(configHome, "settings.json"))
	if enabled := settings["enabledPlugins"].(map[string]any)["market/plugin"]; enabled != true {
		t.Fatalf("enabled plugin state = %#v", settings["enabledPlugins"])
	}

	stdout.Reset()
	stderr.Reset()
	code = run([]string{"--cwd", project, "plugin", "disable", "market/plugin"}, strings.NewReader(""), &stdout, &stderr)
	if code != 0 {
		t.Fatalf("disable exit = %d stderr=%s", code, stderr.String())
	}
	if !strings.Contains(stdout.String(), "Plugin market/plugin disabled.") {
		t.Fatalf("disable stdout = %q", stdout.String())
	}
	settings = readTestSettingsJSON(t, filepath.Join(configHome, "settings.json"))
	if enabled := settings["enabledPlugins"].(map[string]any)["market/plugin"]; enabled != false {
		t.Fatalf("disabled plugin state = %#v", settings["enabledPlugins"])
	}

	demoDir := filepath.Join(project, ".claude", "plugins", "demo")
	offDir := filepath.Join(project, ".claude", "plugins", "already-off")
	for _, dir := range []string{demoDir, offDir} {
		if err := os.MkdirAll(dir, 0o755); err != nil {
			t.Fatal(err)
		}
	}
	if err := os.WriteFile(filepath.Join(demoDir, "plugin.json"), []byte(`{"name":"demo","version":"1.0.0"}`), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(offDir, "plugin.json"), []byte(`{"name":"already-off","version":"1.0.0"}`), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(configHome, "settings.json"), []byte(`{"enabledPlugins":{"already-off":false}}`), 0o644); err != nil {
		t.Fatal(err)
	}

	stdout.Reset()
	stderr.Reset()
	code = run([]string{"--cwd", project, "plugin", "disable", "--all"}, strings.NewReader(""), &stdout, &stderr)
	if code != 0 {
		t.Fatalf("disable all exit = %d stderr=%s", code, stderr.String())
	}
	if !strings.Contains(stdout.String(), "Disabled 1 plugin") {
		t.Fatalf("disable all stdout = %q", stdout.String())
	}
	settings = readTestSettingsJSON(t, filepath.Join(configHome, "settings.json"))
	enabledPlugins := settings["enabledPlugins"].(map[string]any)
	if enabledPlugins["demo"] != false || enabledPlugins["already-off"] != false {
		t.Fatalf("disable all enabledPlugins = %#v", enabledPlugins)
	}

	stdout.Reset()
	stderr.Reset()
	code = run([]string{"--cwd", project, "plugin", "enable", "--scope", "project", "demo"}, strings.NewReader(""), &stdout, &stderr)
	if code != 0 {
		t.Fatalf("project scope enable exit=%d stdout=%q stderr=%q", code, stdout.String(), stderr.String())
	}
	if !strings.Contains(stdout.String(), "Plugin demo enabled.") {
		t.Fatalf("project enable stdout = %q", stdout.String())
	}
	projectSettings := readTestSettingsJSON(t, filepath.Join(project, ".claude", "settings.json"))
	if enabled := projectSettings["enabledPlugins"].(map[string]any)["demo"]; enabled != true {
		t.Fatalf("project enabled plugin state = %#v", projectSettings["enabledPlugins"])
	}

	stdout.Reset()
	stderr.Reset()
	code = run([]string{"--cwd", project, "plugin", "disable", "--scope", "project", "--all"}, strings.NewReader(""), &stdout, &stderr)
	if code != 0 {
		t.Fatalf("project disable all exit=%d stdout=%q stderr=%q", code, stdout.String(), stderr.String())
	}
	if !strings.Contains(stdout.String(), "Disabled 1 plugin") {
		t.Fatalf("project disable all stdout = %q", stdout.String())
	}
	projectSettings = readTestSettingsJSON(t, filepath.Join(project, ".claude", "settings.json"))
	if enabled := projectSettings["enabledPlugins"].(map[string]any)["demo"]; enabled != false {
		t.Fatalf("project disabled plugin state = %#v", projectSettings["enabledPlugins"])
	}

	stdout.Reset()
	stderr.Reset()
	code = run([]string{"--cwd", project, "plugin", "enable", "--scope", "local", "market/plugin"}, strings.NewReader(""), &stdout, &stderr)
	if code != 0 {
		t.Fatalf("local scope enable exit=%d stdout=%q stderr=%q", code, stdout.String(), stderr.String())
	}
	localSettings := readTestSettingsJSON(t, filepath.Join(project, ".claude", "settings.local.json"))
	if enabled := localSettings["enabledPlugins"].(map[string]any)["market/plugin"]; enabled != true {
		t.Fatalf("local enabled plugin state = %#v", localSettings["enabledPlugins"])
	}

	stdout.Reset()
	stderr.Reset()
	code = run([]string{"--cwd", project, "plugin", "disable", "--all", "demo"}, strings.NewReader(""), &stdout, &stderr)
	if code != 2 || !strings.Contains(stderr.String(), "cannot use --all with a specific plugin") {
		t.Fatalf("disable all with plugin exit=%d stdout=%q stderr=%q", code, stdout.String(), stderr.String())
	}
}

func readTestSettingsJSON(t *testing.T, path string) map[string]any {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	var document map[string]any
	if err := json.Unmarshal(data, &document); err != nil {
		t.Fatal(err)
	}
	return document
}

func TestRunPluginMarketplaceListCLI(t *testing.T) {
	configHome := t.TempDir()
	t.Setenv("CLAUDE_CONFIG_DIR", configHome)
	project := t.TempDir()
	if err := os.MkdirAll(filepath.Join(project, ".git"), 0o755); err != nil {
		t.Fatal(err)
	}
	directoryMarket := filepath.Join(t.TempDir(), "team-market")
	fileMarket := filepath.Join(t.TempDir(), "catalog.json")
	settings := fmt.Sprintf(`{
		"extraKnownMarketplaces": {
			"team": {
				"source": {"source": "directory", "path": %q},
				"installLocation": "project"
			},
			"remote": {"source": {"source": "url", "url": "https://example.com/catalog.json"}},
			"github": {"source": {"source": "github", "repo": "owner/repo"}},
			"npm-tools": {"source": {"source": "npm", "package": "@example/tools"}},
			"file": {"source": {"source": "file", "path": %q}}
		}
	}`, directoryMarket, fileMarket)
	if err := os.WriteFile(filepath.Join(configHome, "settings.json"), []byte(settings), 0o644); err != nil {
		t.Fatal(err)
	}

	var stdout, stderr bytes.Buffer
	code := run([]string{"--cwd", project, "plugin", "marketplace", "list", "--json"}, strings.NewReader(""), &stdout, &stderr)
	if code != 0 {
		t.Fatalf("exit = %d stderr=%s", code, stderr.String())
	}
	var payload []map[string]any
	if err := json.Unmarshal(stdout.Bytes(), &payload); err != nil {
		t.Fatalf("invalid json stdout %q: %v", stdout.String(), err)
	}
	if len(payload) != 5 {
		t.Fatalf("marketplaces = %#v", payload)
	}
	byName := map[string]map[string]any{}
	for _, marketplace := range payload {
		name, _ := marketplace["name"].(string)
		byName[name] = marketplace
	}
	if byName["team"]["source"] != "directory" || byName["team"]["path"] != directoryMarket || byName["team"]["installLocation"] != "project" {
		t.Fatalf("team marketplace = %#v", byName["team"])
	}
	if byName["remote"]["source"] != "url" || byName["remote"]["url"] != "https://example.com/catalog.json" {
		t.Fatalf("remote marketplace = %#v", byName["remote"])
	}
	if byName["github"]["source"] != "github" || byName["github"]["repo"] != "owner/repo" {
		t.Fatalf("github marketplace = %#v", byName["github"])
	}
	if byName["npm-tools"]["source"] != "npm" || byName["npm-tools"]["package"] != "@example/tools" {
		t.Fatalf("npm marketplace = %#v", byName["npm-tools"])
	}
	if byName["file"]["source"] != "file" || byName["file"]["path"] != fileMarket {
		t.Fatalf("file marketplace = %#v", byName["file"])
	}

	stdout.Reset()
	stderr.Reset()
	code = run([]string{"--cwd", project, "plugin", "marketplace", "list"}, strings.NewReader(""), &stdout, &stderr)
	if code != 0 {
		t.Fatalf("text exit = %d stderr=%s", code, stderr.String())
	}
	for _, want := range []string{
		"Configured marketplaces:",
		"- team",
		"Source: Directory (" + directoryMarket + ")",
		"Install location: project",
		"- remote",
		"Source: URL (https://example.com/catalog.json)",
		"- github",
		"Source: GitHub (owner/repo)",
		"- npm-tools",
		"Source: NPM (@example/tools)",
	} {
		if !strings.Contains(stdout.String(), want) {
			t.Fatalf("text output missing %q: %q", want, stdout.String())
		}
	}
}

func TestRunPluginMarketplaceUpdateCLI(t *testing.T) {
	configHome := t.TempDir()
	t.Setenv("CLAUDE_CONFIG_DIR", configHome)
	project := t.TempDir()
	if err := os.MkdirAll(filepath.Join(project, ".git"), 0o755); err != nil {
		t.Fatal(err)
	}
	pluginRoot := filepath.Join(t.TempDir(), "remote-tool")
	if err := os.MkdirAll(pluginRoot, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(pluginRoot, "plugin.json"), []byte(`{"name":"remote tool","version":"1.0.0"}`), 0o644); err != nil {
		t.Fatal(err)
	}
	var hitCount atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		hitCount.Add(1)
		w.Header().Set("content-type", "application/json")
		_, _ = fmt.Fprintf(w, `{"plugins":[%q]}`, filepath.ToSlash(pluginRoot))
	}))
	defer server.Close()
	settings := fmt.Sprintf(`{
		"extraKnownMarketplaces": {
			"remote": {"source": {"source": "url", "url": %q}},
			"inline": {"source": {"source": "settings", "name": "inline", "plugins": []}}
		}
	}`, server.URL)
	if err := os.WriteFile(filepath.Join(configHome, "settings.json"), []byte(settings), 0o644); err != nil {
		t.Fatal(err)
	}

	var stdout, stderr bytes.Buffer
	code := run([]string{"--cwd", project, "plugin", "marketplace", "update", "remote"}, strings.NewReader(""), &stdout, &stderr)
	if code != 0 {
		t.Fatalf("exit = %d stderr=%s", code, stderr.String())
	}
	if hitCount.Load() != 1 {
		t.Fatalf("catalog hit count = %d", hitCount.Load())
	}
	for _, want := range []string{
		"Updating marketplace: remote...",
		"Successfully updated marketplace: remote",
	} {
		if !strings.Contains(stdout.String(), want) {
			t.Fatalf("stdout missing %q: %q", want, stdout.String())
		}
	}

	stdout.Reset()
	stderr.Reset()
	code = run([]string{"--cwd", project, "plugin", "marketplace", "update", "missing"}, strings.NewReader(""), &stdout, &stderr)
	if code != 1 || !strings.Contains(stderr.String(), `marketplace "missing" not found`) || !strings.Contains(stderr.String(), "Available marketplaces: inline, remote") {
		t.Fatalf("missing exit=%d stdout=%q stderr=%q", code, stdout.String(), stderr.String())
	}
}

func TestRunPluginMarketplaceAddRemoveCLI(t *testing.T) {
	configHome := t.TempDir()
	t.Setenv("CLAUDE_CONFIG_DIR", configHome)
	project := t.TempDir()
	if err := os.MkdirAll(filepath.Join(project, ".git"), 0o755); err != nil {
		t.Fatal(err)
	}
	marketDir := filepath.Join(t.TempDir(), "team-market")
	if err := os.MkdirAll(marketDir, 0o755); err != nil {
		t.Fatal(err)
	}

	var stdout, stderr bytes.Buffer
	code := run([]string{"--cwd", project, "plugin", "marketplace", "add", "--type", "directory", "--install-location", "project", "team", marketDir}, strings.NewReader(""), &stdout, &stderr)
	if code != 0 {
		t.Fatalf("add exit = %d stderr=%s", code, stderr.String())
	}
	if !strings.Contains(stdout.String(), "Marketplace team added.") {
		t.Fatalf("add stdout = %q", stdout.String())
	}
	settings := readTestSettingsJSON(t, filepath.Join(configHome, "settings.json"))
	extraKnown := settings["extraKnownMarketplaces"].(map[string]any)
	team := extraKnown["team"].(map[string]any)
	source := team["source"].(map[string]any)
	if source["source"] != "directory" || source["path"] != marketDir || team["installLocation"] != "project" {
		t.Fatalf("team marketplace settings = %#v", team)
	}

	stdout.Reset()
	stderr.Reset()
	code = run([]string{"--cwd", project, "plugin", "marketplace", "list"}, strings.NewReader(""), &stdout, &stderr)
	if code != 0 {
		t.Fatalf("list exit = %d stderr=%s", code, stderr.String())
	}
	for _, want := range []string{
		"Configured marketplaces:",
		"- team",
		"Source: Directory (" + marketDir + ")",
		"Install location: project",
	} {
		if !strings.Contains(stdout.String(), want) {
			t.Fatalf("list stdout missing %q: %q", want, stdout.String())
		}
	}

	stdout.Reset()
	stderr.Reset()
	code = run([]string{"--cwd", project, "plugin", "marketplace", "add", "team", "github:owner/repo"}, strings.NewReader(""), &stdout, &stderr)
	if code != 0 {
		t.Fatalf("update add exit = %d stderr=%s", code, stderr.String())
	}
	if !strings.Contains(stdout.String(), "Marketplace team updated.") {
		t.Fatalf("update add stdout = %q", stdout.String())
	}
	settings = readTestSettingsJSON(t, filepath.Join(configHome, "settings.json"))
	team = settings["extraKnownMarketplaces"].(map[string]any)["team"].(map[string]any)
	source = team["source"].(map[string]any)
	if source["source"] != "github" || source["repo"] != "owner/repo" {
		t.Fatalf("updated team marketplace settings = %#v", team)
	}
	if _, ok := team["installLocation"]; ok {
		t.Fatalf("installLocation should be cleared on update: %#v", team)
	}

	stdout.Reset()
	stderr.Reset()
	code = run([]string{"--cwd", project, "plugin", "marketplace", "remove", "team"}, strings.NewReader(""), &stdout, &stderr)
	if code != 0 {
		t.Fatalf("remove exit = %d stderr=%s", code, stderr.String())
	}
	if !strings.Contains(stdout.String(), "Marketplace team removed.") {
		t.Fatalf("remove stdout = %q", stdout.String())
	}
	settings = readTestSettingsJSON(t, filepath.Join(configHome, "settings.json"))
	if _, ok := settings["extraKnownMarketplaces"]; ok {
		t.Fatalf("extraKnownMarketplaces should be removed: %#v", settings)
	}

	stdout.Reset()
	stderr.Reset()
	code = run([]string{"--cwd", project, "plugin", "marketplace", "remove", "team"}, strings.NewReader(""), &stdout, &stderr)
	if code != 1 || !strings.Contains(stderr.String(), `marketplace "team" not found in user settings`) {
		t.Fatalf("missing remove exit=%d stdout=%q stderr=%q", code, stdout.String(), stderr.String())
	}

	stdout.Reset()
	stderr.Reset()
	code = run([]string{"--cwd", project, "plugin", "marketplace", "add", "--scope", "project", "team", "owner/repo"}, strings.NewReader(""), &stdout, &stderr)
	if code != 0 {
		t.Fatalf("project add scope exit=%d stdout=%q stderr=%q", code, stdout.String(), stderr.String())
	}
	projectSettings := readTestSettingsJSON(t, filepath.Join(project, ".claude", "settings.json"))
	team = projectSettings["extraKnownMarketplaces"].(map[string]any)["team"].(map[string]any)
	source = team["source"].(map[string]any)
	if source["source"] != "github" || source["repo"] != "owner/repo" {
		t.Fatalf("project marketplace settings = %#v", team)
	}

	stdout.Reset()
	stderr.Reset()
	code = run([]string{"--cwd", project, "plugin", "marketplace", "add", "--scope", "local", "local-tools", "npm:@example/tools"}, strings.NewReader(""), &stdout, &stderr)
	if code != 0 {
		t.Fatalf("local add scope exit=%d stdout=%q stderr=%q", code, stdout.String(), stderr.String())
	}
	localSettings := readTestSettingsJSON(t, filepath.Join(project, ".claude", "settings.local.json"))
	localTools := localSettings["extraKnownMarketplaces"].(map[string]any)["local-tools"].(map[string]any)
	source = localTools["source"].(map[string]any)
	if source["source"] != "npm" || source["package"] != "@example/tools" {
		t.Fatalf("local marketplace settings = %#v", localTools)
	}

	stdout.Reset()
	stderr.Reset()
	code = run([]string{"--cwd", project, "plugin", "marketplace", "remove", "--scope", "local", "local-tools"}, strings.NewReader(""), &stdout, &stderr)
	if code != 0 {
		t.Fatalf("local remove scope exit=%d stdout=%q stderr=%q", code, stdout.String(), stderr.String())
	}
	localSettings = readTestSettingsJSON(t, filepath.Join(project, ".claude", "settings.local.json"))
	if _, ok := localSettings["extraKnownMarketplaces"]; ok {
		t.Fatalf("local extraKnownMarketplaces should be removed: %#v", localSettings)
	}
}

func TestRunPluginMarketplaceListEmpty(t *testing.T) {
	configHome := t.TempDir()
	t.Setenv("CLAUDE_CONFIG_DIR", configHome)
	project := t.TempDir()
	if err := os.MkdirAll(filepath.Join(project, ".git"), 0o755); err != nil {
		t.Fatal(err)
	}

	var stdout, stderr bytes.Buffer
	code := run([]string{"--cwd", project, "plugin", "marketplace", "list"}, strings.NewReader(""), &stdout, &stderr)
	if code != 0 {
		t.Fatalf("exit = %d stderr=%s", code, stderr.String())
	}
	if got := strings.TrimSpace(stdout.String()); got != "No marketplaces configured" {
		t.Fatalf("stdout = %q", stdout.String())
	}

	stdout.Reset()
	stderr.Reset()
	code = run([]string{"--cwd", project, "plugin", "marketplace", "update"}, strings.NewReader(""), &stdout, &stderr)
	if code != 0 {
		t.Fatalf("update exit = %d stderr=%s", code, stderr.String())
	}
	if got := strings.TrimSpace(stdout.String()); got != "No marketplaces configured" {
		t.Fatalf("update stdout = %q", stdout.String())
	}

	stdout.Reset()
	stderr.Reset()
	code = run([]string{"--cwd", project, "plugin", "marketplace", "refresh"}, strings.NewReader(""), &stdout, &stderr)
	if code != 2 || !strings.Contains(stderr.String(), "unsupported subcommand refresh") {
		t.Fatalf("unsupported exit=%d stdout=%q stderr=%q", code, stdout.String(), stderr.String())
	}
}

func TestRunPrintStreamJSONIncludesToolProgress(t *testing.T) {
	var requestBody map[string]any
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if err := json.NewDecoder(r.Body).Decode(&requestBody); err != nil {
			t.Fatal(err)
		}
		w.Header().Set("content-type", "application/json")
		_, _ = w.Write([]byte(`{
			"id":"msg_stream_hook",
			"type":"message",
			"role":"assistant",
			"model":"claude-sonnet-4-6",
			"content":[{"type":"text","text":"hook stream ok"}],
			"stop_reason":"end_turn"
		}`))
	}))
	defer server.Close()

	t.Setenv("ANTHROPIC_API_KEY", "test-key")
	t.Setenv("ANTHROPIC_BASE_URL", server.URL)
	t.Setenv("ANTHROPIC_MODEL", "")
	t.Setenv("CLAUDE_MODEL", "")
	t.Setenv("ANTHROPIC_BETA", "")
	configHome := t.TempDir()
	t.Setenv("CLAUDE_CONFIG_DIR", configHome)
	if err := os.WriteFile(filepath.Join(configHome, "settings.json"), []byte(`{"hooks":{"UserPromptSubmit":[{"hooks":[{"type":"command","command":"printf '%s\n' '{\"hookSpecificOutput\":{\"hookEventName\":\"UserPromptSubmit\",\"additionalContext\":\"stream hook context\"}}'"}]}]}}`), 0o644); err != nil {
		t.Fatal(err)
	}
	project := t.TempDir()

	var stdout, stderr bytes.Buffer
	code := run([]string{"--cwd", project, "--print", "--output-format", "stream-json", "stream prompt"}, strings.NewReader(""), &stdout, &stderr)
	if code != 0 {
		t.Fatalf("exit = %d stderr=%s stdout=%s", code, stderr.String(), stdout.String())
	}
	messages, ok := requestBody["messages"].([]any)
	if !ok || len(messages) != 1 {
		t.Fatalf("messages = %#v", requestBody["messages"])
	}
	message, ok := messages[0].(map[string]any)
	if !ok {
		t.Fatalf("message = %#v", messages[0])
	}
	content, ok := message["content"].([]any)
	if !ok || len(content) != 1 {
		t.Fatalf("content = %#v", message["content"])
	}
	block, ok := content[0].(map[string]any)
	if !ok {
		t.Fatalf("block = %#v", content[0])
	}
	prompt, _ := block["text"].(string)
	if !strings.Contains(prompt, "stream prompt") || !strings.Contains(prompt, "stream hook context") {
		t.Fatalf("prompt = %q", prompt)
	}

	lines := strings.Split(strings.TrimSpace(stdout.String()), "\n")
	var sawHookStarted bool
	var sawHookCompleted bool
	var sawResult bool
	var hookCompletedIndex = -1
	var userMessageIndex = -1
	for idx, line := range lines {
		var event map[string]any
		if err := json.Unmarshal([]byte(line), &event); err != nil {
			t.Fatalf("invalid json line %q: %v", line, err)
		}
		switch event["type"] {
		case "tool_progress":
			if event["tool_use_id"] != "hook_UserPromptSubmit" {
				continue
			}
			data, ok := event["data"].(map[string]any)
			if !ok {
				t.Fatalf("progress data = %#v", event["data"])
			}
			if data["phase"] != "UserPromptSubmit" || data["scope"] != "conversation" || data["hook_index"] != float64(0) {
				t.Fatalf("progress event = %#v", event)
			}
			switch event["progress_type"] {
			case "hook_started":
				sawHookStarted = true
			case "hook_completed":
				sawHookCompleted = true
				hookCompletedIndex = idx
			}
		case "user_message":
			if userMessageIndex < 0 {
				userMessageIndex = idx
			}
		case "result":
			if event["result"] == "hook stream ok" && event["is_error"] == false {
				sawResult = true
			}
		}
	}
	if !sawHookStarted || !sawHookCompleted {
		t.Fatalf("missing hook progress in %q", stdout.String())
	}
	if hookCompletedIndex < 0 || userMessageIndex < 0 || hookCompletedIndex > userMessageIndex {
		t.Fatalf("event order hook_completed=%d user_message=%d stdout=%q", hookCompletedIndex, userMessageIndex, stdout.String())
	}
	if !sawResult {
		t.Fatalf("missing result event in %q", stdout.String())
	}
}

func TestWritePrintStreamEventTokenWarningUsesSnakeCasePayload(t *testing.T) {
	autoOverride := 50.0
	var stdout bytes.Buffer
	encoder := json.NewEncoder(&stdout)
	err := writePrintStreamEvent(encoder, conversation.Event{
		Type: conversation.EventTokenWarning,
		TokenWarning: &conversation.TokenWarning{
			TokenUsage: 160_000,
			Window: compactpkg.WindowConfig{
				ContextWindow:       200_000,
				MaxOutputTokens:     20_000,
				AutoCompactEnabled:  true,
				AutoCompactOverride: &autoOverride,
				BlockingLimit:       177_000,
			},
			State: compactpkg.WarningState{
				PercentLeft:                 4,
				IsAboveWarningThreshold:     true,
				IsAboveAutoCompactThreshold: true,
			},
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	var event map[string]any
	if err := json.Unmarshal(stdout.Bytes(), &event); err != nil {
		t.Fatalf("invalid json %q: %v", stdout.String(), err)
	}
	if event["type"] != "token_warning" {
		t.Fatalf("event = %#v", event)
	}
	warning, ok := event["token_warning"].(map[string]any)
	if !ok {
		t.Fatalf("token warning = %#v", event["token_warning"])
	}
	if _, exists := warning["TokenUsage"]; exists {
		t.Fatalf("token warning uses Go field names: %#v", warning)
	}
	if warning["token_usage"] != float64(160_000) {
		t.Fatalf("token usage = %#v", warning["token_usage"])
	}
	window, ok := warning["window"].(map[string]any)
	if !ok {
		t.Fatalf("window = %#v", warning["window"])
	}
	if window["context_window"] != float64(200_000) || window["max_output_tokens"] != float64(20_000) || window["auto_compact_enabled"] != true || window["auto_compact_override"] != float64(50) || window["blocking_limit"] != float64(177_000) {
		t.Fatalf("window = %#v", window)
	}
	state, ok := warning["state"].(map[string]any)
	if !ok {
		t.Fatalf("state = %#v", warning["state"])
	}
	if state["percent_left"] != float64(4) || state["is_above_warning_threshold"] != true || state["is_above_error_threshold"] != false || state["is_above_auto_compact_threshold"] != true || state["is_at_blocking_limit"] != false {
		t.Fatalf("state = %#v", state)
	}
}

func TestWritePrintStreamEventCompactUsesLightweightMetadata(t *testing.T) {
	plan := compactpkg.BuildPlan(
		[]contracts.Message{messages.UserText("old"), messages.AssistantText("old answer", "sonnet", nil)},
		compactpkg.PlanOptions{
			Trigger:     compactpkg.TriggerAuto,
			PreTokens:   177_000,
			UserContext: "preserve deployment note",
			Summary:     "summary",
		},
	)
	var stdout bytes.Buffer
	encoder := json.NewEncoder(&stdout)
	err := writePrintStreamEvent(encoder, conversation.Event{
		Type:    conversation.EventCompact,
		Compact: &compactpkg.Result{Plan: plan, Usage: contracts.Usage{InputTokens: 10, OutputTokens: 2}},
	})
	if err != nil {
		t.Fatal(err)
	}
	var event map[string]any
	if err := json.Unmarshal(stdout.Bytes(), &event); err != nil {
		t.Fatalf("invalid json %q: %v", stdout.String(), err)
	}
	if event["type"] != "compact" {
		t.Fatalf("event = %#v", event)
	}
	compactPayload, ok := event["compact"].(map[string]any)
	if !ok {
		t.Fatalf("compact payload = %#v", event["compact"])
	}
	if compactPayload["trigger"] != "auto" || compactPayload["preTokens"] != float64(177_000) || compactPayload["userContext"] != "preserve deployment note" || compactPayload["messagesSummarized"] != float64(2) {
		t.Fatalf("compact payload = %#v", compactPayload)
	}
	for _, key := range []string{"Plan", "plan", "Request", "request", "Response", "response", "Usage", "usage"} {
		if _, exists := compactPayload[key]; exists {
			t.Fatalf("compact payload leaked internal field %q: %#v", key, compactPayload)
		}
	}
}

func TestWritePrintStreamEventRetryIncludesModelBreadcrumb(t *testing.T) {
	var stdout bytes.Buffer
	encoder := json.NewEncoder(&stdout)
	err := writePrintStreamEvent(encoder, conversation.Event{
		Type:  conversation.EventRetry,
		Model: "sonnet",
		Error: fmt.Errorf("try later"),
		Retry: &conversation.RetryInfo{
			Attempt:     1,
			MaxAttempts: 2,
			FailedModel: "sonnet",
			NextModel:   "haiku",
			Fallback:    true,
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	var event map[string]any
	if err := json.Unmarshal(stdout.Bytes(), &event); err != nil {
		t.Fatalf("invalid json %q: %v", stdout.String(), err)
	}
	if event["type"] != "retry" || event["model"] != "sonnet" || event["error"] != "try later" {
		t.Fatalf("event = %#v", event)
	}
	retry, ok := event["retry"].(map[string]any)
	if !ok {
		t.Fatalf("retry = %#v", event["retry"])
	}
	if retry["attempt"] != float64(1) || retry["max_attempts"] != float64(2) || retry["failed_model"] != "sonnet" || retry["next_model"] != "haiku" || retry["fallback"] != true {
		t.Fatalf("retry = %#v", retry)
	}
}

func TestWritePrintStreamEventRetryIncludesAPIErrorMetadata(t *testing.T) {
	var stdout bytes.Buffer
	encoder := json.NewEncoder(&stdout)
	err := writePrintStreamEvent(encoder, conversation.Event{
		Type:  conversation.EventRetry,
		Model: "sonnet",
		Error: fmt.Errorf("request failed: %w", anthropic.APIError{
			StatusCode: http.StatusTooManyRequests,
			RequestID:  "req_retry_1",
			Type:       "rate_limit_error",
			Message:    "try later",
		}),
		Retry: &conversation.RetryInfo{
			Attempt:     1,
			MaxAttempts: 2,
			FailedModel: "sonnet",
			NextModel:   "haiku",
			Fallback:    true,
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	var event map[string]any
	if err := json.Unmarshal(stdout.Bytes(), &event); err != nil {
		t.Fatalf("invalid json %q: %v", stdout.String(), err)
	}
	if event["type"] != "retry" || event["error_type"] != "rate_limit_error" || event["status_code"] != float64(http.StatusTooManyRequests) || event["request_id"] != "req_retry_1" {
		t.Fatalf("event = %#v", event)
	}
}

func TestRunPrintStreamJSONClearIncludesCleared(t *testing.T) {
	t.Setenv("ANTHROPIC_API_KEY", "test-key")
	t.Setenv("ANTHROPIC_BASE_URL", "")
	t.Setenv("ANTHROPIC_MODEL", "")
	t.Setenv("CLAUDE_MODEL", "")
	t.Setenv("ANTHROPIC_BETA", "")
	t.Setenv("CLAUDE_CONFIG_DIR", t.TempDir())

	var stdout, stderr bytes.Buffer
	code := run([]string{"--print", "--output-format", "stream-json", "/clear"}, strings.NewReader(""), &stdout, &stderr)
	if code != 0 {
		t.Fatalf("exit = %d stderr=%s", code, stderr.String())
	}
	lines := strings.Split(strings.TrimSpace(stdout.String()), "\n")
	if len(lines) != 3 {
		t.Fatalf("lines = %#v stdout=%q", lines, stdout.String())
	}
	var final map[string]any
	if err := json.Unmarshal([]byte(lines[2]), &final); err != nil {
		t.Fatalf("invalid final line %q: %v", lines[2], err)
	}
	if final["type"] != "result" || final["subtype"] != "success" || final["cleared"] != true || final["result"] != "" {
		t.Fatalf("final = %#v", final)
	}
}

func TestResultNumTurnsCountsAssistantMessages(t *testing.T) {
	result := conversation.Result{
		Messages: []contracts.Message{
			messages.UserText("one"),
			messages.AssistantText("two", "sonnet", nil),
			messages.UserText("three"),
			messages.AssistantText("four", "sonnet", nil),
		},
	}
	if turns := resultNumTurns(result); turns != 2 {
		t.Fatalf("turns = %d", turns)
	}
}

func TestWritePrintJSONResultIncludesModelsAttempted(t *testing.T) {
	result := conversation.Result{
		Assistant:     messages.AssistantText("fallback ok", "haiku", nil),
		ModelsAttempt: []string{"sonnet", "haiku"},
	}
	var stdout bytes.Buffer
	if err := writePrintJSONResult(&stdout, conversation.Runner{Model: "sonnet"}, result, "fallback ok", 10); err != nil {
		t.Fatal(err)
	}
	var payload map[string]any
	if err := json.Unmarshal(stdout.Bytes(), &payload); err != nil {
		t.Fatalf("invalid json stdout %q: %v", stdout.String(), err)
	}
	attempts, ok := payload["models_attempted"].([]any)
	if !ok || len(attempts) != 2 || attempts[0] != "sonnet" || attempts[1] != "haiku" {
		t.Fatalf("models_attempted = %#v", payload["models_attempted"])
	}
}

func TestWritePrintJSONErrorIncludesModelsAttempted(t *testing.T) {
	var stdout bytes.Buffer
	err := writePrintJSONError(&stdout, conversation.Runner{SessionID: "sess_error"}, fmt.Errorf("fallback failed"), time.Millisecond, 2*time.Millisecond, []string{"sonnet", "haiku"})
	if err != nil {
		t.Fatal(err)
	}
	var payload map[string]any
	if err := json.Unmarshal(stdout.Bytes(), &payload); err != nil {
		t.Fatalf("invalid json stdout %q: %v", stdout.String(), err)
	}
	if payload["type"] != "result" || payload["subtype"] != "error" || payload["is_error"] != true {
		t.Fatalf("payload = %#v", payload)
	}
	attempts, ok := payload["models_attempted"].([]any)
	if !ok || len(attempts) != 2 || attempts[0] != "sonnet" || attempts[1] != "haiku" {
		t.Fatalf("models_attempted = %#v", payload["models_attempted"])
	}
}

func TestWritePrintJSONErrorIncludesAPIErrorMetadata(t *testing.T) {
	var stdout bytes.Buffer
	err := writePrintJSONError(&stdout, conversation.Runner{SessionID: "sess_error"}, anthropic.APIError{
		StatusCode: http.StatusServiceUnavailable,
		RequestID:  "req_json_1",
		Type:       "overloaded_error",
		Message:    "temporarily overloaded",
	}, time.Millisecond, 2*time.Millisecond, []string{"sonnet"})
	if err != nil {
		t.Fatal(err)
	}
	var payload map[string]any
	if err := json.Unmarshal(stdout.Bytes(), &payload); err != nil {
		t.Fatalf("invalid json stdout %q: %v", stdout.String(), err)
	}
	if payload["type"] != "result" || payload["subtype"] != "error" || payload["error_type"] != "overloaded_error" || payload["status_code"] != float64(http.StatusServiceUnavailable) || payload["request_id"] != "req_json_1" {
		t.Fatalf("payload = %#v", payload)
	}
}

func TestWritePrintStreamErrorIncludesModelsAttempted(t *testing.T) {
	var stdout bytes.Buffer
	err := writePrintStreamError(&stdout, conversation.Runner{SessionID: "sess_error"}, fmt.Errorf("fallback failed"), time.Millisecond, 2*time.Millisecond, []string{"sonnet", "haiku"})
	if err != nil {
		t.Fatal(err)
	}
	var event map[string]any
	if err := json.Unmarshal(stdout.Bytes(), &event); err != nil {
		t.Fatalf("invalid json stdout %q: %v", stdout.String(), err)
	}
	if event["type"] != "error" || event["is_error"] != true || event["error"] != "fallback failed" {
		t.Fatalf("event = %#v", event)
	}
	attempts, ok := event["models_attempted"].([]any)
	if !ok || len(attempts) != 2 || attempts[0] != "sonnet" || attempts[1] != "haiku" {
		t.Fatalf("models_attempted = %#v", event["models_attempted"])
	}
}

func TestWritePrintStreamErrorIncludesAPIErrorMetadata(t *testing.T) {
	var stdout bytes.Buffer
	err := writePrintStreamError(&stdout, conversation.Runner{SessionID: "sess_error"}, anthropic.APIError{
		StatusCode: http.StatusUnauthorized,
		RequestID:  "req_stream_1",
		Type:       "authentication_error",
		Message:    "invalid api key",
	}, time.Millisecond, 2*time.Millisecond, []string{"sonnet"})
	if err != nil {
		t.Fatal(err)
	}
	var event map[string]any
	if err := json.Unmarshal(stdout.Bytes(), &event); err != nil {
		t.Fatalf("invalid json stdout %q: %v", stdout.String(), err)
	}
	if event["type"] != "error" || event["is_error"] != true || event["error_type"] != "authentication_error" || event["status_code"] != float64(http.StatusUnauthorized) || event["request_id"] != "req_stream_1" {
		t.Fatalf("event = %#v", event)
	}
}

func TestWritePrintJSONResultIncludesCompactMetadata(t *testing.T) {
	plan := compactpkg.BuildPlan(
		[]contracts.Message{messages.UserText("old")},
		compactpkg.PlanOptions{
			Trigger:     compactpkg.TriggerManual,
			PreTokens:   42,
			UserContext: "keep API details",
			Summary:     "summary",
		},
	)
	result := conversation.Result{
		Messages:  []contracts.Message{plan.Summary},
		Compacted: true,
		Compact:   &compactpkg.Result{Plan: plan},
	}
	var stdout bytes.Buffer
	if err := writePrintJSONResult(&stdout, conversation.Runner{Model: "sonnet"}, result, messages.TextContent(plan.Summary), 10); err != nil {
		t.Fatal(err)
	}
	var payload map[string]any
	if err := json.Unmarshal(stdout.Bytes(), &payload); err != nil {
		t.Fatalf("invalid json stdout %q: %v", stdout.String(), err)
	}
	compactPayload, ok := payload["compact"].(map[string]any)
	if payload["compacted"] != true || !ok {
		t.Fatalf("payload = %#v", payload)
	}
	if compactPayload["trigger"] != "manual" || compactPayload["preTokens"] != float64(42) || compactPayload["userContext"] != "keep API details" {
		t.Fatalf("compact payload = %#v", compactPayload)
	}
}

func TestRunPrintStreamJSONOutputIncludesErrorEvent(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("content-type", "application/json")
		_, _ = w.Write([]byte(`{`))
	}))
	defer server.Close()

	t.Setenv("ANTHROPIC_API_KEY", "test-key")
	t.Setenv("ANTHROPIC_BASE_URL", server.URL)
	t.Setenv("ANTHROPIC_MODEL", "")
	t.Setenv("CLAUDE_MODEL", "")
	t.Setenv("ANTHROPIC_BETA", "")
	t.Setenv("CLAUDE_CONFIG_DIR", t.TempDir())

	var stdout, stderr bytes.Buffer
	code := run([]string{"--print", "--output-format", "stream-json", "fail prompt"}, strings.NewReader(""), &stdout, &stderr)
	if code == 0 {
		t.Fatalf("exit = %d", code)
	}
	lines := strings.Split(strings.TrimSpace(stdout.String()), "\n")
	if len(lines) != 3 {
		t.Fatalf("lines = %#v", lines)
	}
	var final map[string]any
	if err := json.Unmarshal([]byte(lines[2]), &final); err != nil {
		t.Fatalf("invalid final line %q: %v", lines[2], err)
	}
	if final["type"] != "error" || final["is_error"] != true || final["error"] == "" {
		t.Fatalf("final = %#v stdout=%q", final, stdout.String())
	}
	if _, ok := final["duration_ms"].(float64); !ok {
		t.Fatalf("duration_ms = %#v", final["duration_ms"])
	}
	if _, ok := final["duration_api_ms"].(float64); !ok {
		t.Fatalf("duration_api_ms = %#v", final["duration_api_ms"])
	}
	if !strings.Contains(stderr.String(), "ccgo:") {
		t.Fatalf("stderr = %q", stderr.String())
	}
}

func TestRunPrintStreamJSONIncludesRawStreamingEvents(t *testing.T) {
	var requestBody map[string]any
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if err := json.NewDecoder(r.Body).Decode(&requestBody); err != nil {
			t.Fatal(err)
		}
		w.Header().Set("content-type", "text/event-stream")
		_, _ = w.Write([]byte("event: message_start\ndata: {\"type\":\"message_start\",\"message\":{\"id\":\"msg_stream_delta\",\"type\":\"message\",\"role\":\"assistant\",\"model\":\"claude-sonnet-4-6\",\"content\":[]}}\n\n"))
		_, _ = w.Write([]byte("event: content_block_start\ndata: {\"type\":\"content_block_start\",\"index\":0,\"content_block\":{\"type\":\"text\"}}\n\n"))
		_, _ = w.Write([]byte("event: content_block_delta\ndata: {\"type\":\"content_block_delta\",\"index\":0,\"delta\":{\"type\":\"text_delta\",\"text\":\"delta ok\"}}\n\n"))
		_, _ = w.Write([]byte("event: message_delta\ndata: {\"type\":\"message_delta\",\"delta\":{\"stop_reason\":\"end_turn\"},\"usage\":{\"output_tokens\":1}}\n\n"))
	}))
	defer server.Close()

	t.Setenv("ANTHROPIC_API_KEY", "test-key")
	t.Setenv("ANTHROPIC_BASE_URL", server.URL)
	t.Setenv("ANTHROPIC_MODEL", "")
	t.Setenv("CLAUDE_MODEL", "")
	t.Setenv("ANTHROPIC_BETA", "")
	t.Setenv("CLAUDE_CONFIG_DIR", t.TempDir())

	var stdout, stderr bytes.Buffer
	code := run([]string{"--print", "--stream", "--output-format", "stream-json", "stream prompt"}, strings.NewReader(""), &stdout, &stderr)
	if code != 0 {
		t.Fatalf("exit = %d stderr=%s", code, stderr.String())
	}
	if requestBody["stream"] != true {
		t.Fatalf("stream flag = %#v", requestBody["stream"])
	}
	lines := strings.Split(strings.TrimSpace(stdout.String()), "\n")
	var sawDelta bool
	var final map[string]any
	for _, line := range lines {
		var event map[string]any
		if err := json.Unmarshal([]byte(line), &event); err != nil {
			t.Fatalf("invalid json line %q: %v", line, err)
		}
		if event["type"] == "stream_event" {
			streamEvent, ok := event["stream_event"].(map[string]any)
			if !ok {
				t.Fatalf("stream event = %#v", event)
			}
			if streamEvent["type"] == "content_block_delta" {
				delta := streamEvent["delta"].(map[string]any)
				if delta["text"] == "delta ok" {
					sawDelta = true
				}
			}
		}
		if event["type"] == "result" {
			final = event
		}
	}
	if !sawDelta {
		t.Fatalf("missing content_block_delta in %q", stdout.String())
	}
	if final == nil || final["result"] != "delta ok" {
		t.Fatalf("final = %#v stdout=%q", final, stdout.String())
	}
}

func TestRunnerMCPServerSummariesMergesSettingsAndPluginServers(t *testing.T) {
	runner := conversation.Runner{MCP: &conversation.MCPConfig{
		UserSettings: contracts.Settings{MCPServers: map[string]contracts.MCPServer{
			"zeta": {Command: "user"},
		}},
		ProjectSettings: contracts.Settings{MCPServers: map[string]contracts.MCPServer{
			"alpha": {Command: "project"},
		}},
		LocalSettings: contracts.Settings{MCPServers: map[string]contracts.MCPServer{
			"beta":    {Command: "local"},
			"blocked": {Command: "blocked"},
		}, DeniedMCPServers: []contracts.MCPServerPolicyEntry{
			{ServerName: "blocked"},
		}},
		PluginServers: map[string]contracts.MCPServer{
			"plugin:docs": {Type: "http", URL: "https://example.com/mcp", PluginSource: "demo"},
		},
	}}
	got := runnerMCPServerSummaries(runner)
	if len(got) != 5 {
		t.Fatalf("summaries = %#v", got)
	}
	if got[0].Name != "alpha" || got[0].Status != "configured" || got[0].Type != "stdio" || got[0].Scope != "project" || got[0].Source != "project" {
		t.Fatalf("alpha = %#v", got[0])
	}
	if got[1].Name != "beta" || got[1].Scope != "local" || got[1].Source != "local" {
		t.Fatalf("beta = %#v", got[1])
	}
	if got[2].Name != "blocked" || got[2].Status != "blocked" || got[2].Reason != "denied" {
		t.Fatalf("blocked = %#v", got[2])
	}
	if got[3].Name != "plugin:docs" || got[3].Type != "http" || got[3].Source != "plugin" || got[3].PluginSource != "demo" {
		t.Fatalf("plugin = %#v", got[3])
	}
	if got[4].Name != "zeta" || got[4].Scope != "user" || got[4].Source != "user" {
		t.Fatalf("zeta = %#v", got[4])
	}
}

func TestRunPrintResumeLoadsTranscriptHistory(t *testing.T) {
	var requestBody map[string]any
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if err := json.NewDecoder(r.Body).Decode(&requestBody); err != nil {
			t.Fatal(err)
		}
		w.Header().Set("content-type", "application/json")
		_, _ = w.Write([]byte(`{
			"id":"msg_resume",
			"type":"message",
			"role":"assistant",
			"model":"claude-sonnet-4-6",
			"content":[{"type":"text","text":"resume ok"}],
			"stop_reason":"end_turn"
		}`))
	}))
	defer server.Close()

	claudeHome := t.TempDir()
	t.Setenv("ANTHROPIC_API_KEY", "test-key")
	t.Setenv("ANTHROPIC_BASE_URL", server.URL)
	t.Setenv("ANTHROPIC_MODEL", "")
	t.Setenv("CLAUDE_MODEL", "")
	t.Setenv("ANTHROPIC_BETA", "")
	t.Setenv("CLAUDE_CONFIG_DIR", claudeHome)

	cwd, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	sessionID := contracts.ID("resume-session")
	transcriptPath := session.TranscriptPath(cwd, sessionID)
	writeTestTranscript(t, transcriptPath, sessionID, "old question", "old answer")

	var stdout, stderr bytes.Buffer
	code := run([]string{"--print", "--resume", string(sessionID), "new question"}, strings.NewReader(""), &stdout, &stderr)
	if code != 0 {
		t.Fatalf("exit = %d stderr=%s", code, stderr.String())
	}
	if stdout.String() != "resume ok\n" {
		t.Fatalf("stdout = %q", stdout.String())
	}
	requestMessages := requestBody["messages"].([]any)
	if got := messageTextAt(t, requestMessages, 0); got != "old question" {
		t.Fatalf("old user = %q", got)
	}
	if got := messageTextAt(t, requestMessages, 1); got != "old answer" {
		t.Fatalf("old assistant = %q", got)
	}
	if got := messageTextAt(t, requestMessages, 2); got != "new question" {
		t.Fatalf("new user = %q", got)
	}
	entries, err := session.Load(transcriptPath)
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 4 {
		t.Fatalf("entries = %#v", entries)
	}
}

func TestRunPrintContinueUsesMostRecentSession(t *testing.T) {
	var requestBody map[string]any
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if err := json.NewDecoder(r.Body).Decode(&requestBody); err != nil {
			t.Fatal(err)
		}
		w.Header().Set("content-type", "application/json")
		_, _ = w.Write([]byte(`{
			"id":"msg_continue",
			"type":"message",
			"role":"assistant",
			"model":"claude-sonnet-4-6",
			"content":[{"type":"text","text":"continue ok"}],
			"stop_reason":"end_turn"
		}`))
	}))
	defer server.Close()

	claudeHome := t.TempDir()
	t.Setenv("ANTHROPIC_API_KEY", "test-key")
	t.Setenv("ANTHROPIC_BASE_URL", server.URL)
	t.Setenv("ANTHROPIC_MODEL", "")
	t.Setenv("CLAUDE_MODEL", "")
	t.Setenv("ANTHROPIC_BETA", "")
	t.Setenv("CLAUDE_CONFIG_DIR", claudeHome)

	cwd, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	sessionID := contracts.ID("continue-session")
	writeTestTranscript(t, session.TranscriptPath(cwd, sessionID), sessionID, "continue old", "continue answer")

	var stdout, stderr bytes.Buffer
	code := run([]string{"--print", "--continue", "continue new"}, strings.NewReader(""), &stdout, &stderr)
	if code != 0 {
		t.Fatalf("exit = %d stderr=%s", code, stderr.String())
	}
	if stdout.String() != "continue ok\n" {
		t.Fatalf("stdout = %q", stdout.String())
	}
	requestMessages := requestBody["messages"].([]any)
	if got := messageTextAt(t, requestMessages, 0); got != "continue old" {
		t.Fatalf("continued old user = %q", got)
	}
	if got := messageTextAt(t, requestMessages, 2); got != "continue new" {
		t.Fatalf("continued new user = %q", got)
	}
}

func TestRunPrintRequiresCredentials(t *testing.T) {
	t.Setenv("ANTHROPIC_API_KEY", "")
	t.Setenv("CLAUDE_CODE_OAUTH_REFRESH_TOKEN", "")
	t.Setenv("ANTHROPIC_MODEL", "")
	t.Setenv("CLAUDE_MODEL", "")
	t.Setenv("CLAUDE_CONFIG_DIR", t.TempDir())

	var stdout, stderr bytes.Buffer
	code := run([]string{"--print", "hello"}, strings.NewReader(""), &stdout, &stderr)
	if code == 0 {
		t.Fatalf("exit = %d", code)
	}
	if stdout.Len() != 0 {
		t.Fatalf("stdout = %q", stdout.String())
	}
	if !strings.Contains(stderr.String(), "missing Anthropic credentials") {
		t.Fatalf("stderr = %q", stderr.String())
	}
}

func TestRunPrintUsesStoredOAuthCredentials(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if got := r.Header.Get("authorization"); got != "Bearer stored-access" {
			t.Fatalf("authorization = %q", got)
		}
		if got := r.Header.Get("x-api-key"); got != "" {
			t.Fatalf("x-api-key = %q", got)
		}
		w.Header().Set("content-type", "application/json")
		_, _ = w.Write([]byte(`{
			"id":"msg_stored_auth",
			"type":"message",
			"role":"assistant",
			"model":"claude-sonnet-4-6",
			"content":[{"type":"text","text":"stored ok"}],
			"stop_reason":"end_turn"
		}`))
	}))
	defer server.Close()

	configHome := t.TempDir()
	t.Setenv("ANTHROPIC_API_KEY", "")
	t.Setenv("CLAUDE_CODE_OAUTH_REFRESH_TOKEN", "")
	t.Setenv("ANTHROPIC_BASE_URL", server.URL)
	t.Setenv("ANTHROPIC_MODEL", "")
	t.Setenv("CLAUDE_MODEL", "")
	t.Setenv("ANTHROPIC_BETA", "")
	t.Setenv("CLAUDE_CONFIG_DIR", configHome)
	if err := os.WriteFile(filepath.Join(configHome, "credentials.json"), []byte(`{"source":"oauth","access_token":"stored-access","refresh_token":"stored-refresh"}`), 0o600); err != nil {
		t.Fatal(err)
	}

	var stdout, stderr bytes.Buffer
	code := run([]string{"--print", "hello"}, strings.NewReader(""), &stdout, &stderr)
	if code != 0 {
		t.Fatalf("exit = %d stderr=%s", code, stderr.String())
	}
	if stdout.String() != "stored ok\n" {
		t.Fatalf("stdout = %q", stdout.String())
	}
}

func TestAnthropicClientFromEnvConfiguresOAuthRefreshProvider(t *testing.T) {
	configHome := t.TempDir()
	t.Setenv("ANTHROPIC_API_KEY", "")
	t.Setenv("CLAUDE_CODE_OAUTH_REFRESH_TOKEN", "")
	t.Setenv("ANTHROPIC_BASE_URL", "")
	t.Setenv("ANTHROPIC_MODEL", "")
	t.Setenv("CLAUDE_MODEL", "")
	t.Setenv("ANTHROPIC_BETA", "")
	t.Setenv("CLAUDE_CONFIG_DIR", configHome)
	if err := os.WriteFile(filepath.Join(configHome, "credentials.json"), []byte(`{"source":"oauth","access_token":"stored-access","refresh_token":"stored-refresh"}`), 0o600); err != nil {
		t.Fatal(err)
	}

	client, source, err := anthropicClientFromEnv(context.Background(), false)
	if err != nil {
		t.Fatal(err)
	}
	if source != "oauth" || client.AccessToken != "stored-access" || client.AccessTokenProvider == nil {
		t.Fatalf("source=%q token=%q provider=%T", source, client.AccessToken, client.AccessTokenProvider)
	}
}

func TestAnthropicClientFromEnvAppliesCustomHeaders(t *testing.T) {
	var seen bool
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		seen = true
		if got := r.Header.Get("x-gateway-auth"); got != "gateway" {
			t.Fatalf("x-gateway-auth = %q", got)
		}
		if values := r.Header.Values("x-gateway-multi"); len(values) != 2 || values[0] != "one" || values[1] != "two" {
			t.Fatalf("x-gateway-multi = %#v", values)
		}
		if got := r.Header.Get("x-proxy-tenant"); got != "acme" {
			t.Fatalf("x-proxy-tenant = %q", got)
		}
		if got := r.Header.Get("x-proxy-mode"); got != "compat" {
			t.Fatalf("x-proxy-mode = %q", got)
		}
		w.Header().Set("content-type", "application/json")
		_, _ = w.Write([]byte(`{"id":"msg_1","type":"message","role":"assistant","model":"sonnet","content":[]}`))
	}))
	defer server.Close()

	t.Setenv("ANTHROPIC_API_KEY", "test-key")
	t.Setenv("CLAUDE_CODE_OAUTH_REFRESH_TOKEN", "")
	t.Setenv("ANTHROPIC_BASE_URL", server.URL)
	t.Setenv("ANTHROPIC_MODEL", "")
	t.Setenv("CLAUDE_MODEL", "")
	t.Setenv("ANTHROPIC_BETA", "")
	t.Setenv("ANTHROPIC_CUSTOM_HEADERS", `{"X-Gateway-Auth":"gateway","X-Gateway-Multi":["one","two"]}`)
	t.Setenv("CLAUDE_CODE_CUSTOM_HEADERS", "X-Proxy-Tenant: acme\nX-Proxy-Mode=compat")
	t.Setenv("CLAUDE_CONFIG_DIR", t.TempDir())

	client, source, err := anthropicClientFromEnv(context.Background(), false)
	if err != nil {
		t.Fatal(err)
	}
	if source != "api_key" {
		t.Fatalf("source = %q", source)
	}
	if _, err := client.CreateMessage(context.Background(), anthropic.Request{
		Model:     "sonnet",
		MaxTokens: 32,
		Messages:  []contracts.APIMessage{{Role: "user", Content: []contracts.ContentBlock{contracts.NewTextBlock("hello")}}},
	}); err != nil {
		t.Fatal(err)
	}
	if !seen {
		t.Fatal("server did not receive request")
	}
}

func TestAnthropicClientFromEnvRejectsInvalidCustomHeaders(t *testing.T) {
	t.Setenv("ANTHROPIC_API_KEY", "test-key")
	t.Setenv("CLAUDE_CODE_OAUTH_REFRESH_TOKEN", "")
	t.Setenv("ANTHROPIC_BASE_URL", "")
	t.Setenv("ANTHROPIC_MODEL", "")
	t.Setenv("CLAUDE_MODEL", "")
	t.Setenv("ANTHROPIC_BETA", "")
	t.Setenv("ANTHROPIC_CUSTOM_HEADERS", "not-a-header")
	t.Setenv("CLAUDE_CODE_CUSTOM_HEADERS", "")
	t.Setenv("CLAUDE_CONFIG_DIR", t.TempDir())

	_, _, err := anthropicClientFromEnv(context.Background(), false)
	if err == nil || !strings.Contains(err.Error(), "ANTHROPIC_CUSTOM_HEADERS line 1") {
		t.Fatalf("error = %v", err)
	}
}

func TestRunPrintJSONOutputsSetupError(t *testing.T) {
	t.Setenv("ANTHROPIC_API_KEY", "")
	t.Setenv("CLAUDE_CODE_OAUTH_REFRESH_TOKEN", "")
	t.Setenv("ANTHROPIC_MODEL", "")
	t.Setenv("CLAUDE_MODEL", "")
	t.Setenv("CLAUDE_CONFIG_DIR", t.TempDir())
	project := t.TempDir()
	expectedCWD, err := filepath.EvalSymlinks(project)
	if err != nil {
		t.Fatal(err)
	}

	var stdout, stderr bytes.Buffer
	code := run([]string{"--cwd", project, "--print", "--output-format", "json", "hello"}, strings.NewReader(""), &stdout, &stderr)
	if code == 0 {
		t.Fatalf("exit = %d", code)
	}
	var payload map[string]any
	if err := json.Unmarshal(stdout.Bytes(), &payload); err != nil {
		t.Fatalf("invalid json stdout %q: %v", stdout.String(), err)
	}
	if payload["type"] != "result" || payload["subtype"] != "error" || !strings.Contains(fmt.Sprint(payload["error"]), "missing Anthropic credentials") {
		t.Fatalf("payload = %#v", payload)
	}
	if payload["cwd"] != expectedCWD || payload["session_id"] == "" {
		t.Fatalf("setup error metadata = %#v", payload)
	}
	if !strings.Contains(stderr.String(), "missing Anthropic credentials") {
		t.Fatalf("stderr = %q", stderr.String())
	}
}

func TestRunPrintJSONOutputsInputFormatError(t *testing.T) {
	t.Setenv("CLAUDE_CONFIG_DIR", t.TempDir())

	var stdout, stderr bytes.Buffer
	code := run([]string{"--print", "--output-format", "json", "--input-format", "xml", "hello"}, strings.NewReader(""), &stdout, &stderr)
	if code == 0 {
		t.Fatalf("exit = %d", code)
	}
	var payload map[string]any
	if err := json.Unmarshal(stdout.Bytes(), &payload); err != nil {
		t.Fatalf("invalid json stdout %q: %v", stdout.String(), err)
	}
	if payload["type"] != "result" || payload["subtype"] != "error" || !strings.Contains(fmt.Sprint(payload["error"]), "unsupported input format") {
		t.Fatalf("payload = %#v", payload)
	}
	if !strings.Contains(stderr.String(), "unsupported input format") {
		t.Fatalf("stderr = %q", stderr.String())
	}
}

func TestRunPrintJSONOutputsPromptError(t *testing.T) {
	t.Setenv("CLAUDE_CONFIG_DIR", t.TempDir())

	var stdout, stderr bytes.Buffer
	code := run([]string{"--print", "--output-format", "json"}, strings.NewReader(""), &stdout, &stderr)
	if code == 0 {
		t.Fatalf("exit = %d", code)
	}
	var payload map[string]any
	if err := json.Unmarshal(stdout.Bytes(), &payload); err != nil {
		t.Fatalf("invalid json stdout %q: %v", stdout.String(), err)
	}
	if payload["type"] != "result" || payload["subtype"] != "error" || !strings.Contains(fmt.Sprint(payload["error"]), "--print requires a prompt") {
		t.Fatalf("payload = %#v", payload)
	}
	if !strings.Contains(stderr.String(), "--print requires a prompt") {
		t.Fatalf("stderr = %q", stderr.String())
	}
}

func TestRunPrintRejectsPermissionModeSkipConflict(t *testing.T) {
	t.Setenv("CLAUDE_CONFIG_DIR", t.TempDir())

	var stdout, stderr bytes.Buffer
	code := run([]string{
		"--print",
		"--output-format", "json",
		"--permission-mode", "plan",
		"--dangerously-skip-permissions",
		"hello",
	}, strings.NewReader(""), &stdout, &stderr)
	if code == 0 {
		t.Fatalf("exit = %d", code)
	}
	var payload map[string]any
	if err := json.Unmarshal(stdout.Bytes(), &payload); err != nil {
		t.Fatalf("invalid json stdout %q: %v", stdout.String(), err)
	}
	if payload["subtype"] != "error" || !strings.Contains(fmt.Sprint(payload["error"]), "dangerously-skip-permissions") {
		t.Fatalf("payload = %#v", payload)
	}
	if !strings.Contains(stderr.String(), "dangerously-skip-permissions") {
		t.Fatalf("stderr = %q", stderr.String())
	}
}

func TestRunPrintRejectsUnsupportedOutputFormat(t *testing.T) {
	t.Setenv("CLAUDE_CONFIG_DIR", t.TempDir())

	var stdout, stderr bytes.Buffer
	code := run([]string{"--print", "--output-format", "xml", "hello"}, strings.NewReader(""), &stdout, &stderr)
	if code == 0 {
		t.Fatalf("exit = %d", code)
	}
	if stdout.Len() != 0 {
		t.Fatalf("stdout = %q", stdout.String())
	}
	if !strings.Contains(stderr.String(), "unsupported output format") {
		t.Fatalf("stderr = %q", stderr.String())
	}
}

func TestRunPrintRejectsUnsupportedInputFormat(t *testing.T) {
	t.Setenv("CLAUDE_CONFIG_DIR", t.TempDir())

	var stdout, stderr bytes.Buffer
	code := run([]string{"--print", "--input-format", "xml", "hello"}, strings.NewReader(""), &stdout, &stderr)
	if code == 0 {
		t.Fatalf("exit = %d", code)
	}
	if stdout.Len() != 0 {
		t.Fatalf("stdout = %q", stdout.String())
	}
	if !strings.Contains(stderr.String(), "unsupported input format") {
		t.Fatalf("stderr = %q", stderr.String())
	}
}

func TestNormalizeInputFormatAliases(t *testing.T) {
	tests := map[string]string{
		"":            "text",
		" text ":      "text",
		"json":        "json",
		"stream-json": "stream-json",
		"stream_json": "stream-json",
		"streamJson":  "stream-json",
		"stream JSON": "stream-json",
		"STREAM_JSON": "stream-json",
	}
	for raw, want := range tests {
		got, err := normalizeInputFormat(raw)
		if err != nil {
			t.Fatalf("normalizeInputFormat(%q) error: %v", raw, err)
		}
		if got != want {
			t.Fatalf("normalizeInputFormat(%q) = %q, want %q", raw, got, want)
		}
	}
}

func TestNormalizeOutputFormatAliases(t *testing.T) {
	tests := map[string]string{
		"":            "text",
		" text ":      "text",
		"json":        "json",
		"stream-json": "stream-json",
		"stream_json": "stream-json",
		"streamJson":  "stream-json",
		"stream JSON": "stream-json",
		"STREAM_JSON": "stream-json",
	}
	for raw, want := range tests {
		got, err := normalizeOutputFormat(raw)
		if err != nil {
			t.Fatalf("normalizeOutputFormat(%q) error: %v", raw, err)
		}
		if got != want {
			t.Fatalf("normalizeOutputFormat(%q) = %q, want %q", raw, got, want)
		}
	}
}

func TestRunRejectsInvalidCWD(t *testing.T) {
	t.Setenv("CLAUDE_CONFIG_DIR", t.TempDir())

	var stdout, stderr bytes.Buffer
	code := run([]string{"--cwd", filepath.Join(t.TempDir(), "missing")}, strings.NewReader(""), &stdout, &stderr)
	if code == 0 {
		t.Fatalf("exit = %d", code)
	}
	if stdout.Len() != 0 {
		t.Fatalf("stdout = %q", stdout.String())
	}
	if !strings.Contains(stderr.String(), "invalid --cwd") {
		t.Fatalf("stderr = %q", stderr.String())
	}
}

func TestRunPrintRejectsMissingMCPConfig(t *testing.T) {
	project := t.TempDir()
	t.Setenv("CLAUDE_CONFIG_DIR", t.TempDir())

	var stdout, stderr bytes.Buffer
	code := run([]string{"--cwd", project, "--print", "--mcp-config", "missing.json", "hello"}, strings.NewReader(""), &stdout, &stderr)
	if code == 0 {
		t.Fatalf("exit = %d", code)
	}
	if stdout.Len() != 0 {
		t.Fatalf("stdout = %q", stdout.String())
	}
	if !strings.Contains(stderr.String(), "load --mcp-config") {
		t.Fatalf("stderr = %q", stderr.String())
	}
}

func TestRunPrintRejectsEmptyPrompt(t *testing.T) {
	t.Setenv("CLAUDE_CONFIG_DIR", t.TempDir())

	var stdout, stderr bytes.Buffer
	code := run([]string{"--print", "   "}, strings.NewReader(""), &stdout, &stderr)
	if code == 0 {
		t.Fatalf("exit = %d", code)
	}
	if stdout.Len() != 0 {
		t.Fatalf("stdout = %q", stdout.String())
	}
	if !strings.Contains(stderr.String(), "--print requires a prompt") {
		t.Fatalf("stderr = %q", stderr.String())
	}
}

func writeTestTranscript(t *testing.T, path string, sessionID contracts.ID, userText string, assistantText string) {
	t.Helper()
	user := messages.UserText(userText)
	user.SessionID = sessionID
	assistant := messages.AssistantText(assistantText, "sonnet", nil)
	assistant.SessionID = sessionID
	parent := user.UUID
	assistant.ParentUUID = &parent
	if err := session.Append(path, session.EntryFromMessage(sessionID, user)); err != nil {
		t.Fatal(err)
	}
	if err := session.Append(path, session.EntryFromMessage(sessionID, assistant)); err != nil {
		t.Fatal(err)
	}
}

func messageTextAt(t *testing.T, requestMessages []any, index int) string {
	t.Helper()
	if index >= len(requestMessages) {
		t.Fatalf("messages = %#v", requestMessages)
	}
	message := requestMessages[index].(map[string]any)
	content := message["content"].([]any)
	block := content[0].(map[string]any)
	return block["text"].(string)
}

func containsAnyString(values []any, want string) bool {
	for _, value := range values {
		if text, ok := value.(string); ok && text == want {
			return true
		}
	}
	return false
}

func containsPluginSummary(values []any, name string, path string, source string) bool {
	for _, value := range values {
		object, ok := value.(map[string]any)
		if !ok {
			continue
		}
		if object["name"] == name && object["path"] == path && object["source"] == source {
			return true
		}
	}
	return false
}

func containsMCPServerSummary(values []any, name string, status string, typ string, scope string, source string, pluginSource string) bool {
	for _, value := range values {
		object, ok := value.(map[string]any)
		if !ok {
			continue
		}
		if object["name"] != name || object["status"] != status || object["type"] != typ || object["scope"] != scope || object["source"] != source {
			continue
		}
		if pluginSource != "" && object["plugin_source"] != pluginSource {
			continue
		}
		return true
	}
	return false
}

func acceptDaemonTestWebSocket(t *testing.T, w http.ResponseWriter, r *http.Request) net.Conn {
	t.Helper()
	key := r.Header.Get("Sec-WebSocket-Key")
	if key == "" {
		t.Fatalf("missing websocket key")
	}
	hijacker, ok := w.(http.Hijacker)
	if !ok {
		t.Fatalf("response writer cannot hijack")
	}
	conn, bufrw, err := hijacker.Hijack()
	if err != nil {
		t.Fatal(err)
	}
	accept := daemonTestWebSocketAccept(key)
	response := fmt.Sprintf("HTTP/1.1 101 Switching Protocols\r\nUpgrade: websocket\r\nConnection: Upgrade\r\nSec-WebSocket-Accept: %s\r\n\r\n", accept)
	if _, err := bufrw.WriteString(response); err != nil {
		t.Fatal(err)
	}
	if err := bufrw.Flush(); err != nil {
		t.Fatal(err)
	}
	return conn
}

func writeDaemonTestWebSocketFrame(t *testing.T, conn net.Conn, opcode byte, payload []byte) {
	t.Helper()
	header := []byte{0x80 | opcode}
	length := len(payload)
	switch {
	case length <= 125:
		header = append(header, byte(length))
	case length <= 65535:
		header = append(header, 126)
		var buf [2]byte
		binary.BigEndian.PutUint16(buf[:], uint16(length))
		header = append(header, buf[:]...)
	default:
		header = append(header, 127)
		var buf [8]byte
		binary.BigEndian.PutUint64(buf[:], uint64(length))
		header = append(header, buf[:]...)
	}
	if _, err := conn.Write(header); err != nil {
		t.Fatal(err)
	}
	if _, err := conn.Write(payload); err != nil {
		t.Fatal(err)
	}
}

func daemonTestWebSocketAccept(key string) string {
	sum := sha1.Sum([]byte(key + "258EAFA5-E914-47DA-95CA-C5AB0DC85B11"))
	return base64.StdEncoding.EncodeToString(sum[:])
}
