package bridge

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"ccgo/internal/commands"
	"ccgo/internal/contracts"
)

func TestDirectHandlerHealthAndManifest(t *testing.T) {
	handler := NewDirectHandler(DirectOptions{
		SessionID: "sess_bridge",
		Manifest:  testDirectManifest(t),
		Registry:  testDirectRegistry(),
	})

	health := httptest.NewRecorder()
	handler.ServeHTTP(health, httptest.NewRequest(http.MethodGet, "/v1/health", nil))
	if health.Code != http.StatusOK {
		t.Fatalf("health status = %d body=%s", health.Code, health.Body.String())
	}
	var healthResponse DirectHealthResponse
	if err := json.Unmarshal(health.Body.Bytes(), &healthResponse); err != nil {
		t.Fatal(err)
	}
	if !healthResponse.OK || healthResponse.SessionID != "sess_bridge" || healthResponse.Commands != 2 {
		t.Fatalf("health response = %#v", healthResponse)
	}

	manifest := httptest.NewRecorder()
	handler.ServeHTTP(manifest, httptest.NewRequest(http.MethodGet, "/manifest", nil))
	if manifest.Code != http.StatusOK {
		t.Fatalf("manifest status = %d body=%s", manifest.Code, manifest.Body.String())
	}
	var manifestResponse Manifest
	if err := json.Unmarshal(manifest.Body.Bytes(), &manifestResponse); err != nil {
		t.Fatal(err)
	}
	if manifestResponse.SessionID != "sess_bridge" || len(manifestResponse.Commands) != 2 {
		t.Fatalf("manifest response = %#v", manifestResponse)
	}
}

func TestDirectHandlerResolveAllowsAliasesAndRejectsUnsafeCommands(t *testing.T) {
	handler := NewDirectHandler(DirectOptions{
		SessionID: "sess_bridge",
		Manifest:  testDirectManifest(t),
		Registry:  testDirectRegistry(),
	})

	resolved := bridgePOST(t, handler, "/resolve", `{"command":"/question prod deploy"}`)
	if resolved.Code != http.StatusOK {
		t.Fatalf("resolve status = %d body=%s", resolved.Code, resolved.Body.String())
	}
	var response DirectResolveResponse
	if err := json.Unmarshal(resolved.Body.Bytes(), &response); err != nil {
		t.Fatal(err)
	}
	if !response.Allowed || response.Name != "ask" || response.Args != "prod deploy" || response.Command == nil {
		t.Fatalf("resolve response = %#v", response)
	}

	rejected := bridgePOST(t, handler, "/resolve", `{"command":"/status show tools"}`)
	if rejected.Code != http.StatusOK {
		t.Fatalf("resolve rejected status = %d body=%s", rejected.Code, rejected.Body.String())
	}
	var rejectedResponse DirectResolveResponse
	if err := json.Unmarshal(rejected.Body.Bytes(), &rejectedResponse); err != nil {
		t.Fatal(err)
	}
	if rejectedResponse.Allowed || !strings.Contains(rejectedResponse.Reason, "not bridge-safe") {
		t.Fatalf("rejected response = %#v", rejectedResponse)
	}
}

func TestDirectHandlerExecuteLocalCommand(t *testing.T) {
	handler := NewDirectHandler(DirectOptions{
		SessionID: "sess_bridge",
		Manifest:  testDirectManifest(t),
		Registry:  testDirectRegistry(),
	})

	recorder := bridgePOST(t, handler, "/execute", `{"command":"compact focus on API","uuid":"user_bridge"}`)
	if recorder.Code != http.StatusOK {
		t.Fatalf("execute status = %d body=%s", recorder.Code, recorder.Body.String())
	}
	var response DirectExecuteResponse
	if err := json.Unmarshal(recorder.Body.Bytes(), &response); err != nil {
		t.Fatal(err)
	}
	if !response.Allowed || !response.Handled || response.Name != "compact" || response.Args != "focus on API" || response.ShouldQuery {
		t.Fatalf("execute response = %#v", response)
	}
	if response.LocalResult == nil || response.LocalResult.Type != commands.LocalCommandResultCompact || !response.LocalResult.HasValue {
		t.Fatalf("local result = %#v", response.LocalResult)
	}
	if response.Messages != 1 {
		t.Fatalf("messages = %d, want 1", response.Messages)
	}
}

func TestDirectHandlerExecutePromptCommandWithoutLeakingPromptText(t *testing.T) {
	handler := NewDirectHandler(DirectOptions{
		SessionID: "sess_bridge",
		Manifest:  testDirectManifest(t),
		Registry:  testDirectRegistry(),
	})

	recorder := bridgePOST(t, handler, "/v1/execute", `{"command":"/question prod"}`)
	if recorder.Code != http.StatusOK {
		t.Fatalf("execute status = %d body=%s", recorder.Code, recorder.Body.String())
	}
	var response DirectExecuteResponse
	if err := json.Unmarshal(recorder.Body.Bytes(), &response); err != nil {
		t.Fatal(err)
	}
	if !response.Allowed || !response.Handled || !response.ShouldQuery || response.Name != "ask" || response.Model != "opus" {
		t.Fatalf("execute response = %#v", response)
	}
	if len(response.AllowedTools) != 1 || response.AllowedTools[0] != "Read" {
		t.Fatalf("allowed tools = %#v", response.AllowedTools)
	}
	if response.Messages != 3 {
		t.Fatalf("messages = %d, want 3", response.Messages)
	}
	if strings.Contains(recorder.Body.String(), "Ask prod") || strings.Contains(recorder.Body.String(), "ARGUMENTS") {
		t.Fatalf("response leaked prompt text: %s", recorder.Body.String())
	}
}

func TestDirectHandlerExecuteRejectsUnsafeCommands(t *testing.T) {
	handler := NewDirectHandler(DirectOptions{
		SessionID: "sess_bridge",
		Manifest:  testDirectManifest(t),
		Registry:  testDirectRegistry(),
	})

	recorder := bridgePOST(t, handler, "/execute", `{"command":"/status show tools"}`)
	if recorder.Code != http.StatusForbidden {
		t.Fatalf("execute unsafe status = %d body=%s", recorder.Code, recorder.Body.String())
	}
	var response DirectExecuteResponse
	if err := json.Unmarshal(recorder.Body.Bytes(), &response); err != nil {
		t.Fatal(err)
	}
	if response.Allowed || response.Error == "" {
		t.Fatalf("unsafe response = %#v", response)
	}
}

func TestDirectHandlerRemoteTriggerEndpoint(t *testing.T) {
	var got DirectRemoteTriggerRequest
	handler := NewDirectHandler(DirectOptions{
		SessionID: "sess_bridge",
		Manifest:  testDirectManifest(t),
		Registry:  testDirectRegistry(),
		RemoteTrigger: func(_ context.Context, req DirectRemoteTriggerRequest) (DirectRemoteTriggerResponse, int) {
			got = req
			return DirectRemoteTriggerResponse{
				Accepted:  true,
				TeamID:    req.TeamID,
				Target:    req.Target,
				EventID:   req.EventID,
				Source:    req.Source,
				Event:     req.Event,
				SentCount: 2,
			}, http.StatusAccepted
		},
	})

	recorder := bridgePOST(t, handler, "/v1/remote-trigger", `{"team_id":"ops/team","target":"all","event_id":"evt-1","source":"github","event":"workflow_failed","message":"Investigate CI."}`)
	if recorder.Code != http.StatusAccepted {
		t.Fatalf("remote trigger status = %d body=%s", recorder.Code, recorder.Body.String())
	}
	if got.TeamID != "ops/team" || got.Target != "all" || got.EventID != "evt-1" || got.Source != "github" || got.Event != "workflow_failed" || got.Message != "Investigate CI." {
		t.Fatalf("remote trigger request = %#v", got)
	}
	var response DirectRemoteTriggerResponse
	if err := json.Unmarshal(recorder.Body.Bytes(), &response); err != nil {
		t.Fatal(err)
	}
	if !response.Accepted || response.SentCount != 2 || response.TeamID != "ops/team" || response.EventID != "evt-1" {
		t.Fatalf("remote trigger response = %#v", response)
	}
}

func TestDirectHandlerRemoteTriggerRequiresCallback(t *testing.T) {
	handler := NewDirectHandler(DirectOptions{
		SessionID: "sess_bridge",
		Manifest:  testDirectManifest(t),
		Registry:  testDirectRegistry(),
	})
	recorder := bridgePOST(t, handler, "/remote-trigger", `{"team_id":"ops/team","message":"Investigate CI."}`)
	if recorder.Code != http.StatusNotImplemented {
		t.Fatalf("remote trigger status = %d body=%s", recorder.Code, recorder.Body.String())
	}
}

func TestNewDirectHandlerFromSettingsBuildsRegistryAndManifest(t *testing.T) {
	handler := NewDirectHandlerFromSettings("sess_bridge", t.TempDir(), contracts.Settings{})
	recorder := httptest.NewRecorder()
	handler.ServeHTTP(recorder, httptest.NewRequest(http.MethodGet, "/v1/health", nil))
	if recorder.Code != http.StatusOK {
		t.Fatalf("health status = %d body=%s", recorder.Code, recorder.Body.String())
	}
	var response DirectHealthResponse
	if err := json.Unmarshal(recorder.Body.Bytes(), &response); err != nil {
		t.Fatal(err)
	}
	if !response.OK || response.SessionID != "sess_bridge" || response.Commands == 0 {
		t.Fatalf("health response = %#v", response)
	}
}

func TestNormalizeDirectPathOnlyStripsV1Segment(t *testing.T) {
	if got := normalizeDirectPath("/v1/health"); got != "/health" {
		t.Fatalf("v1 path = %q", got)
	}
	if got := normalizeDirectPath("/v10/health"); got != "/v10/health" {
		t.Fatalf("v10 path = %q", got)
	}
}

func bridgePOST(t *testing.T, handler http.Handler, path string, body string) *httptest.ResponseRecorder {
	t.Helper()
	recorder := httptest.NewRecorder()
	handler.ServeHTTP(recorder, httptest.NewRequest(http.MethodPost, path, bytes.NewBufferString(body)))
	return recorder
}

func testDirectManifest(t *testing.T) Manifest {
	t.Helper()
	return BuildManifest("sess_bridge", "/work", testDirectRegistry())
}

func testDirectRegistry() commands.Registry {
	return commands.FromSources(commands.Sources{
		ProjectSkillPrompts: []commands.PromptTemplate{{
			Command: contracts.Command{
				Name:         "ask",
				Type:         contracts.CommandPrompt,
				Source:       contracts.CommandSourceSkills,
				LoadedFrom:   "skills",
				Aliases:      []string{"question"},
				AllowedTools: []string{"Read"},
				Model:        "opus",
			},
			Content: "Ask $ARGUMENTS in ${CLAUDE_SESSION_ID}.",
		}},
		Builtins: []contracts.Command{
			{Name: "compact", Type: contracts.CommandLocal, Source: contracts.CommandSourceBuiltin, SupportsNonInteractive: true},
			{Name: "status", Type: contracts.CommandLocalJSX, Source: contracts.CommandSourceBuiltin},
		},
	})
}
