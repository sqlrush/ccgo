package sdk

// G13 tests: verify that sdk.Query wires reload_plugins and apply_flag_settings
// callbacks to live runner state (SDK-40 and SDK-44).
//
// TDD: tests written first; implementation in query.go wiring functions.
// CC refs: docs/cc-parity/sections/16-sdk.md SDK-40/SDK-44.

import (
	"encoding/json"
	"testing"
	"time"

	"ccgo/internal/conversation"
)

// ── helpers ───────────────────────────────────────────────────────────────────

// g13Pause sleeps briefly to allow the read-loop to process a queued message
// before the interrupt is sent. This avoids a race between request dispatch
// and interrupt delivery. The output buffer must NOT be read while the drain
// goroutine is running — only check it after g1Finish returns.
func g13Pause() {
	time.Sleep(50 * time.Millisecond)
}

// ── SDK-40: reload_plugins ────────────────────────────────────────────────────

// TestQueryWiresReloadPlugins_G13 verifies that reload_plugins control subtype
// is wired to a real callback that calls the plugin loader. The loader will
// return empty lists (no plugins on disk in tmp dir), but must not error.
// CC ref: controlSchemas.ts:405-433 (SDK-40).
func TestQueryWiresReloadPlugins_G13(t *testing.T) {
	inPW, buf, outDone, ready, done, _, cancel := g1Setup(t,
		func(r *conversation.Runner) {
			r.WorkingDirectory = t.TempDir()
		},
	)
	defer cancel()

	g1WaitReady(t, ready, done)

	// Send reload_plugins control_request; give the read-loop time to process
	// before sending interrupt. Do NOT read buf while drain goroutine is running.
	g1SendRequest(inPW, "reload_plugins", nil)
	g13Pause()

	g1Finish(t, inPW, outDone, done)

	// Parse the output for a control_response to reload_plugins.
	resp := g1FindControlResponse(t, buf.String())
	response, _ := resp["response"].(map[string]any)
	if response == nil {
		t.Fatalf("expected response object in control_response, got: %v", resp)
	}
	if response["subtype"] != "success" {
		t.Fatalf("expected success response for reload_plugins, got: %v", response)
	}
	// Verify the response body has the required CC-wire fields.
	var body map[string]any
	if respRaw, ok := response["response"]; ok {
		switch v := respRaw.(type) {
		case map[string]any:
			body = v
		default:
			data, _ := json.Marshal(respRaw)
			_ = json.Unmarshal(data, &body)
		}
	}
	for _, field := range []string{"commands", "agents", "plugins", "mcpServers"} {
		if _, ok := body[field]; !ok {
			t.Errorf("reload_plugins response missing field %q; body: %v", field, body)
		}
	}
}

// TestQueryReloadPluginsNoWorkingDir_G13 verifies that reload_plugins
// returns success with empty lists when WorkingDirectory is empty (safe).
func TestQueryReloadPluginsNoWorkingDir_G13(t *testing.T) {
	inPW, buf, outDone, ready, done, _, cancel := g1Setup(t,
		func(r *conversation.Runner) {
			r.WorkingDirectory = "" // no cwd
		},
	)
	defer cancel()

	g1WaitReady(t, ready, done)

	g1SendRequest(inPW, "reload_plugins", nil)
	g13Pause()

	g1Finish(t, inPW, outDone, done)

	resp := g1FindControlResponse(t, buf.String())
	response, _ := resp["response"].(map[string]any)
	if response == nil {
		t.Fatalf("expected response object, got: %v", resp)
	}
	if response["subtype"] != "success" {
		t.Fatalf("expected success, got: %v", response)
	}
}

// ── SDK-44: apply_flag_settings ──────────────────────────────────────────────

// TestQueryApplyFlagSettingsUpdatesRunnerModel_G13 verifies that
// apply_flag_settings with {"model": "..."} updates runner.Model.
// CC ref: controlSchemas.ts:464-473 (SDK-44).
func TestQueryApplyFlagSettingsUpdatesRunnerModel_G13(t *testing.T) {
	inPW, buf, outDone, ready, done, runner, cancel := g1Setup(t,
		func(r *conversation.Runner) {
			r.Model = "original-model"
		},
	)
	defer cancel()

	g1WaitReady(t, ready, done)

	g1SendRequest(inPW, "apply_flag_settings", map[string]any{
		"settings": map[string]any{
			"model": "claude-opus-4-5",
		},
	})
	g13Pause()

	g1Finish(t, inPW, outDone, done)

	resp := g1FindControlResponse(t, buf.String())
	response, _ := resp["response"].(map[string]any)
	if response == nil {
		t.Fatalf("expected response object, got: %v", resp)
	}
	if response["subtype"] != "success" {
		t.Fatalf("expected success, got: %v", response)
	}
	// Verify runner.Model was updated.
	if runner.Model != "claude-opus-4-5" {
		t.Fatalf("expected runner.Model = claude-opus-4-5, got %q", runner.Model)
	}
}

// TestQueryApplyFlagSettingsNoMCP_G13 verifies graceful degradation when
// runner.MCP is nil — model field is still applied to runner.Model.
func TestQueryApplyFlagSettingsNoMCP_G13(t *testing.T) {
	inPW, buf, outDone, ready, done, runner, cancel := g1Setup(t,
		func(r *conversation.Runner) {
			r.MCP = nil
			r.Model = "before"
		},
	)
	defer cancel()

	g1WaitReady(t, ready, done)

	g1SendRequest(inPW, "apply_flag_settings", map[string]any{
		"settings": map[string]any{
			"model": "after-model",
		},
	})
	g13Pause()

	g1Finish(t, inPW, outDone, done)

	resp := g1FindControlResponse(t, buf.String())
	response, _ := resp["response"].(map[string]any)
	if response == nil {
		t.Fatalf("expected response object, got: %v", resp)
	}
	if response["subtype"] != "success" {
		t.Fatalf("expected success, got: %v", response)
	}
	if runner.Model != "after-model" {
		t.Fatalf("expected runner.Model = after-model, got %q", runner.Model)
	}
}

// ── SDK-44: Controller unit test ─────────────────────────────────────────────

// TestApplyFlagSettingsCallbackReceivesMap_G13 is a pure Controller unit test:
// verify the callback receives the full settings map.
func TestApplyFlagSettingsCallbackReceivesMap_G13(t *testing.T) {
	var received map[string]any
	ctrl := &Controller{
		applyFlagSettings: func(settings map[string]any) error {
			received = settings
			return nil
		},
	}

	resp := ctrl.Handle(ControlRequest{
		RequestID: "r-apply",
		Request: map[string]any{
			"subtype": "apply_flag_settings",
			"settings": map[string]any{
				"model":          "claude-haiku-test",
				"maxOutputTokens": float64(1024),
			},
		},
	})

	if resp.Response.Subtype != "success" {
		t.Fatalf("expected success, got %+v", resp)
	}
	if received == nil {
		t.Fatal("callback was not called")
	}
	if received["model"] != "claude-haiku-test" {
		t.Fatalf("model = %v, want claude-haiku-test", received["model"])
	}
}

// ── SDK-40: Controller unit test ─────────────────────────────────────────────

// TestReloadPluginsCallbackG13 verifies that reload_plugins callback is called
// and returns the right CC-wire shape.
func TestReloadPluginsCallbackG13(t *testing.T) {
	ctrl := &Controller{
		reloadPlugins: func() (*ReloadPluginsResult, error) {
			return &ReloadPluginsResult{
				Commands:   []any{map[string]any{"name": "foo"}},
				Agents:     []any{},
				Plugins:    []any{},
				MCPServers: []any{},
				ErrorCount: 0,
			}, nil
		},
	}

	resp := ctrl.Handle(ControlRequest{
		RequestID: "r-reload",
		Request:   map[string]any{"subtype": "reload_plugins"},
	})

	if resp.Response.Subtype != "success" {
		t.Fatalf("expected success, got %+v", resp)
	}
	body := resp.Response.Response
	cmds, _ := body["commands"].([]any)
	if len(cmds) != 1 {
		t.Fatalf("expected 1 command, got %v", cmds)
	}
}
