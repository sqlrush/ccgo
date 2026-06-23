package sdk

import (
	"fmt"
	"testing"

	"ccgo/internal/contracts"
)

// ── baseline (F1-C00) ────────────────────────────────────────────────────────

func TestControllerInterrupt(t *testing.T) {
	var interrupted bool
	c := &Controller{interrupt: func() { interrupted = true }}
	resp := c.Handle(ControlRequest{Type: "control_request", RequestID: "r1",
		Request: map[string]any{"subtype": "interrupt"}})
	if !interrupted {
		t.Fatal("interrupt callback not invoked")
	}
	if resp.Response.Subtype != "success" || resp.Response.RequestID != "r1" {
		t.Fatalf("resp = %+v", resp)
	}
}

func TestControllerSetModel(t *testing.T) {
	var got string
	c := &Controller{setModel: func(m string) error { got = m; return nil }}
	resp := c.Handle(ControlRequest{RequestID: "r2",
		Request: map[string]any{"subtype": "set_model", "model": "opus"}})
	if got != "opus" {
		t.Fatalf("set_model = %q want opus", got)
	}
	if resp.Response.Subtype != "success" {
		t.Fatalf("resp = %+v", resp)
	}
}

func TestControllerSetModelError(t *testing.T) {
	c := &Controller{setModel: func(m string) error { return fmt.Errorf("bad model %q", m) }}
	resp := c.Handle(ControlRequest{RequestID: "r3",
		Request: map[string]any{"subtype": "set_model", "model": "unknown"}})
	if resp.Response.Subtype != "error" || resp.Response.Error == "" {
		t.Fatalf("set_model error not propagated: %+v", resp)
	}
}

func TestControllerInitialize(t *testing.T) {
	c := &Controller{}
	resp := c.Handle(ControlRequest{RequestID: "r4",
		Request: map[string]any{"subtype": "initialize"}})
	if resp.Response.Subtype != "success" {
		t.Fatalf("initialize must succeed: %+v", resp)
	}
}

// TestControllerInitializeResponseFields verifies the initialize response contains
// all CC-required fields: commands, models, account, output_style,
// available_output_styles, pid. CC ref: bridgeMessaging.ts:286-303;
// controlSchemas.ts:77-95 (SDKControlInitializeResponseSchema).
func TestControllerInitializeResponseFields(t *testing.T) {
	c := &Controller{}
	resp := c.Handle(ControlRequest{RequestID: "r4i",
		Request: map[string]any{"subtype": "initialize"}})
	if resp.Response.Subtype != "success" {
		t.Fatalf("initialize must succeed: %+v", resp)
	}
	body := resp.Response.Response
	if body == nil {
		t.Fatalf("initialize response.response should not be nil")
	}
	for _, field := range []string{"commands", "models", "account", "output_style", "available_output_styles", "pid"} {
		if _, present := body[field]; !present {
			t.Errorf("initialize response missing field %q; body = %v", field, body)
		}
	}
	if body["output_style"] != "normal" {
		t.Errorf("output_style = %q want normal", body["output_style"])
	}
	styles, ok := body["available_output_styles"].([]string)
	if !ok || len(styles) == 0 {
		t.Errorf("available_output_styles should be non-empty []string, got %T(%v)", body["available_output_styles"], body["available_output_styles"])
	}
}

func TestControllerUnknownSubtypeErrors(t *testing.T) {
	c := &Controller{}
	resp := c.Handle(ControlRequest{RequestID: "r5",
		Request: map[string]any{"subtype": "frobnicate"}})
	if resp.Response.Subtype != "error" || resp.Response.Error == "" {
		t.Fatalf("unknown subtype must error: %+v", resp)
	}
}

// ── F1-C02: set_permission_mode ──────────────────────────────────────────────

// TestControllerSetPermissionModeCallsCallback verifies that set_permission_mode
// invokes the registered callback with the correct mode string.
// CC ref: bridgeMessaging.ts:328-358; controlSchemas.ts:124-135.
func TestControllerSetPermissionModeCallsCallback(t *testing.T) {
	var got contracts.PermissionMode
	c := &Controller{setPermissionMode: func(m contracts.PermissionMode) error {
		got = m
		return nil
	}}
	resp := c.Handle(ControlRequest{RequestID: "pm1",
		Request: map[string]any{"subtype": "set_permission_mode", "mode": "acceptEdits"}})
	if resp.Response.Subtype != "success" {
		t.Fatalf("set_permission_mode must succeed: %+v", resp)
	}
	if got != contracts.PermissionAcceptEdits {
		t.Fatalf("mode = %q want acceptEdits", got)
	}
}

func TestControllerSetPermissionModeCallbackError(t *testing.T) {
	c := &Controller{setPermissionMode: func(m contracts.PermissionMode) error {
		return fmt.Errorf("not allowed")
	}}
	resp := c.Handle(ControlRequest{RequestID: "pm2",
		Request: map[string]any{"subtype": "set_permission_mode", "mode": "bypassPermissions"}})
	if resp.Response.Subtype != "error" || resp.Response.Error == "" {
		t.Fatalf("expected error response, got %+v", resp)
	}
}

func TestControllerSetPermissionModeNoCallbackReturnsError(t *testing.T) {
	c := &Controller{} // no setPermissionMode
	resp := c.Handle(ControlRequest{RequestID: "pm3",
		Request: map[string]any{"subtype": "set_permission_mode", "mode": "default"}})
	// Should return error (not supported), not panic.
	if resp.Response.Subtype != "error" {
		t.Fatalf("missing callback should return error, got %+v", resp)
	}
}

// ── F1-C02: set_max_thinking_tokens ──────────────────────────────────────────

// TestControllerSetMaxThinkingTokensCallsCallback verifies the callback is
// invoked with the parsed integer value.
// CC ref: bridgeMessaging.ts:317-326; controlSchemas.ts:146-155.
func TestControllerSetMaxThinkingTokensCallsCallback(t *testing.T) {
	var got int
	c := &Controller{setMaxThinkingTokens: func(n int) error { got = n; return nil }}
	resp := c.Handle(ControlRequest{RequestID: "mt1",
		Request: map[string]any{"subtype": "set_max_thinking_tokens", "max_thinking_tokens": float64(8000)}})
	if resp.Response.Subtype != "success" {
		t.Fatalf("expected success, got %+v", resp)
	}
	if got != 8000 {
		t.Fatalf("tokens = %d want 8000", got)
	}
}

func TestControllerSetMaxThinkingTokensNullSetsZero(t *testing.T) {
	var got int = -1
	c := &Controller{setMaxThinkingTokens: func(n int) error { got = n; return nil }}
	resp := c.Handle(ControlRequest{RequestID: "mt2",
		Request: map[string]any{"subtype": "set_max_thinking_tokens", "max_thinking_tokens": nil}})
	if resp.Response.Subtype != "success" {
		t.Fatalf("expected success, got %+v", resp)
	}
	if got != 0 {
		t.Fatalf("null max_thinking_tokens should give 0, got %d", got)
	}
}

func TestControllerSetMaxThinkingTokensNoCallbackReturnsError(t *testing.T) {
	c := &Controller{}
	resp := c.Handle(ControlRequest{RequestID: "mt3",
		Request: map[string]any{"subtype": "set_max_thinking_tokens", "max_thinking_tokens": float64(100)}})
	if resp.Response.Subtype != "error" {
		t.Fatalf("missing callback should return error, got %+v", resp)
	}
}

// ── F1-C02: mcp_status ───────────────────────────────────────────────────────

// TestControllerMcpStatusCallsCallback verifies mcp_status invokes the callback
// and returns the CC-wire mcpServers array.
// CC ref: controlSchemas.ts:157-173; coreSchemas.ts:167-220.
func TestControllerMcpStatusCallsCallback(t *testing.T) {
	c := &Controller{mcpStatus: func() ([]MCPServerStatus, error) {
		return []MCPServerStatus{
			{Name: "filesys", Status: "connected"},
		}, nil
	}}
	resp := c.Handle(ControlRequest{RequestID: "ms1",
		Request: map[string]any{"subtype": "mcp_status"}})
	if resp.Response.Subtype != "success" {
		t.Fatalf("mcp_status must succeed: %+v", resp)
	}
	servers, ok := resp.Response.Response["mcpServers"].([]any)
	if !ok || len(servers) != 1 {
		t.Fatalf("mcpServers should be []any of length 1, got %T(%v)", resp.Response.Response["mcpServers"], resp.Response.Response["mcpServers"])
	}
	entry, _ := servers[0].(map[string]any)
	if entry["name"] != "filesys" {
		t.Fatalf("server name = %q want filesys", entry["name"])
	}
}

func TestControllerMcpStatusNoCallbackReturnsEmptyList(t *testing.T) {
	c := &Controller{}
	resp := c.Handle(ControlRequest{RequestID: "ms2",
		Request: map[string]any{"subtype": "mcp_status"}})
	if resp.Response.Subtype != "success" {
		t.Fatalf("mcp_status without callback must still succeed: %+v", resp)
	}
	servers := resp.Response.Response["mcpServers"].([]any)
	if len(servers) != 0 {
		t.Fatalf("expected empty list, got %v", servers)
	}
}

// ── F1-C02: get_context_usage ────────────────────────────────────────────────

// TestControllerGetContextUsageCallsCallback verifies the callback is invoked
// and the response contains all required CC fields.
// CC ref: controlSchemas.ts:175-306.
func TestControllerGetContextUsageCallsCallback(t *testing.T) {
	c := &Controller{getContextUsage: func() (*ContextUsage, error) {
		return &ContextUsage{
			TotalTokens: 1234,
			MaxTokens:   200000,
			Percentage:  0.617,
			Model:       "claude-sonnet-4-6",
			Categories: []ContextCategory{
				{Name: "messages", Tokens: 1234, Color: "#blue"},
			},
		}, nil
	}}
	resp := c.Handle(ControlRequest{RequestID: "cu1",
		Request: map[string]any{"subtype": "get_context_usage"}})
	if resp.Response.Subtype != "success" {
		t.Fatalf("get_context_usage must succeed: %+v", resp)
	}
	body := resp.Response.Response
	for _, field := range []string{"categories", "totalTokens", "maxTokens", "percentage", "model", "isAutoCompactEnabled"} {
		if _, ok := body[field]; !ok {
			t.Errorf("missing field %q in get_context_usage response", field)
		}
	}
	if body["totalTokens"] != 1234 {
		t.Fatalf("totalTokens = %v want 1234", body["totalTokens"])
	}
}

func TestControllerGetContextUsageNoCallbackReturnsMinimal(t *testing.T) {
	c := &Controller{}
	resp := c.Handle(ControlRequest{RequestID: "cu2",
		Request: map[string]any{"subtype": "get_context_usage"}})
	if resp.Response.Subtype != "success" {
		t.Fatalf("expected success: %+v", resp)
	}
	// Must have the mandatory fields at minimum.
	for _, field := range []string{"categories", "totalTokens", "maxTokens", "percentage", "model", "isAutoCompactEnabled"} {
		if _, ok := resp.Response.Response[field]; !ok {
			t.Errorf("minimal response missing field %q", field)
		}
	}
}

// ── F1-C03: rewind_files ─────────────────────────────────────────────────────

// TestControllerRewindFilesCallsCallback verifies the callback is invoked with
// the correct user_message_id and dry_run.
// CC ref: controlSchemas.ts:308-328.
func TestControllerRewindFilesCallsCallback(t *testing.T) {
	var gotID string
	var gotDry bool
	c := &Controller{rewindFiles: func(id string, dry bool) (*RewindFilesResult, error) {
		gotID = id
		gotDry = dry
		return &RewindFilesResult{CanRewind: true, FilesChanged: []string{"/foo.go"}}, nil
	}}
	resp := c.Handle(ControlRequest{RequestID: "rw1",
		Request: map[string]any{"subtype": "rewind_files", "user_message_id": "msg-42", "dry_run": true}})
	if resp.Response.Subtype != "success" {
		t.Fatalf("expected success: %+v", resp)
	}
	if gotID != "msg-42" || !gotDry {
		t.Fatalf("callback args: id=%q dryRun=%v", gotID, gotDry)
	}
	if resp.Response.Response["canRewind"] != true {
		t.Fatalf("canRewind should be true")
	}
}

func TestControllerRewindFilesNoCallbackReturnsFalse(t *testing.T) {
	c := &Controller{}
	resp := c.Handle(ControlRequest{RequestID: "rw2",
		Request: map[string]any{"subtype": "rewind_files", "user_message_id": "msg-1"}})
	if resp.Response.Subtype != "success" {
		t.Fatalf("expected success: %+v", resp)
	}
	if resp.Response.Response["canRewind"] != false {
		t.Fatalf("expected canRewind=false")
	}
}

// ── F1-C03: cancel_async_message ─────────────────────────────────────────────

// TestControllerCancelAsyncMessageCallsCallback verifies the callback is invoked
// with the correct uuid.
// CC ref: controlSchemas.ts:330-349.
func TestControllerCancelAsyncMessageCallsCallback(t *testing.T) {
	var gotUUID string
	c := &Controller{cancelAsyncMessage: func(uuid string) (bool, error) {
		gotUUID = uuid
		return true, nil
	}}
	resp := c.Handle(ControlRequest{RequestID: "ca1",
		Request: map[string]any{"subtype": "cancel_async_message", "message_uuid": "uuid-99"}})
	if resp.Response.Subtype != "success" {
		t.Fatalf("expected success: %+v", resp)
	}
	if gotUUID != "uuid-99" {
		t.Fatalf("uuid = %q want uuid-99", gotUUID)
	}
	if resp.Response.Response["cancelled"] != true {
		t.Fatalf("cancelled should be true")
	}
}

func TestControllerCancelAsyncMessageNoCallbackReturnsFalse(t *testing.T) {
	c := &Controller{}
	resp := c.Handle(ControlRequest{RequestID: "ca2",
		Request: map[string]any{"subtype": "cancel_async_message", "message_uuid": "x"}})
	if resp.Response.Subtype != "success" {
		t.Fatalf("expected success: %+v", resp)
	}
	if resp.Response.Response["cancelled"] != false {
		t.Fatalf("expected cancelled=false")
	}
}

// ── F1-C03: seed_read_state ──────────────────────────────────────────────────

// TestControllerSeedReadStateCallsCallback verifies path and mtime are passed.
// CC ref: controlSchemas.ts:351-362.
func TestControllerSeedReadStateCallsCallback(t *testing.T) {
	var gotPath string
	var gotMtime int64
	c := &Controller{seedReadState: func(p string, m int64) error {
		gotPath = p
		gotMtime = m
		return nil
	}}
	resp := c.Handle(ControlRequest{RequestID: "sr1",
		Request: map[string]any{"subtype": "seed_read_state", "path": "/tmp/foo.go", "mtime": float64(1234567890)}})
	if resp.Response.Subtype != "success" {
		t.Fatalf("expected success: %+v", resp)
	}
	if gotPath != "/tmp/foo.go" || gotMtime != 1234567890 {
		t.Fatalf("path=%q mtime=%d", gotPath, gotMtime)
	}
}

func TestControllerSeedReadStateNoCallbackReturnsError(t *testing.T) {
	c := &Controller{}
	resp := c.Handle(ControlRequest{RequestID: "sr2",
		Request: map[string]any{"subtype": "seed_read_state", "path": "/x", "mtime": float64(1)}})
	if resp.Response.Subtype != "error" {
		t.Fatalf("missing callback should error: %+v", resp)
	}
}

// ── F1-C03: hook_callback ────────────────────────────────────────────────────

// TestControllerHookCallbackCallsCallback verifies callback_id and input forwarding.
// CC ref: controlSchemas.ts:363-372.
func TestControllerHookCallbackCallsCallback(t *testing.T) {
	var gotID string
	var gotInput map[string]any
	c := &Controller{hookCallback: func(id string, input map[string]any) error {
		gotID = id
		gotInput = input
		return nil
	}}
	resp := c.Handle(ControlRequest{RequestID: "hc1",
		Request: map[string]any{
			"subtype":     "hook_callback",
			"callback_id": "cb-7",
			"input":       map[string]any{"tool": "bash"},
		}})
	if resp.Response.Subtype != "success" {
		t.Fatalf("expected success: %+v", resp)
	}
	if gotID != "cb-7" {
		t.Fatalf("callback_id = %q want cb-7", gotID)
	}
	if gotInput["tool"] != "bash" {
		t.Fatalf("input = %v", gotInput)
	}
}

// ── F1-C04: mcp_message ──────────────────────────────────────────────────────

// TestControllerMcpMessageCallsCallback verifies server_name and message forwarding.
// CC ref: controlSchemas.ts:374-383.
func TestControllerMcpMessageCallsCallback(t *testing.T) {
	var gotServer string
	var gotMsg map[string]any
	c := &Controller{mcpMessage: func(s string, m map[string]any) error {
		gotServer = s
		gotMsg = m
		return nil
	}}
	resp := c.Handle(ControlRequest{RequestID: "mm1",
		Request: map[string]any{
			"subtype":     "mcp_message",
			"server_name": "filesys",
			"message":     map[string]any{"method": "ping"},
		}})
	if resp.Response.Subtype != "success" {
		t.Fatalf("expected success: %+v", resp)
	}
	if gotServer != "filesys" {
		t.Fatalf("server_name = %q want filesys", gotServer)
	}
	if gotMsg["method"] != "ping" {
		t.Fatalf("message = %v", gotMsg)
	}
}

func TestControllerMcpMessageNoCallbackReturnsError(t *testing.T) {
	c := &Controller{}
	resp := c.Handle(ControlRequest{RequestID: "mm2",
		Request: map[string]any{"subtype": "mcp_message", "server_name": "x", "message": map[string]any{}}})
	if resp.Response.Subtype != "error" {
		t.Fatalf("missing callback should error: %+v", resp)
	}
}

// ── F1-C04: mcp_set_servers ──────────────────────────────────────────────────

// TestControllerMcpSetServersCallsCallback verifies servers map forwarding and
// returns the CC-wire {added, removed, errors} shape.
// CC ref: controlSchemas.ts:384-403.
func TestControllerMcpSetServersCallsCallback(t *testing.T) {
	c := &Controller{mcpSetServers: func(s map[string]any) (*MCPSetServersResult, error) {
		return &MCPSetServersResult{
			Added:   []string{"new-server"},
			Removed: []string{},
			Errors:  map[string]string{},
		}, nil
	}}
	resp := c.Handle(ControlRequest{RequestID: "mss1",
		Request: map[string]any{
			"subtype": "mcp_set_servers",
			"servers": map[string]any{"new-server": map[string]any{"command": "node"}},
		}})
	if resp.Response.Subtype != "success" {
		t.Fatalf("expected success: %+v", resp)
	}
	added, _ := resp.Response.Response["added"].([]string)
	if len(added) != 1 || added[0] != "new-server" {
		t.Fatalf("added = %v", added)
	}
}

func TestControllerMcpSetServersNoCallbackReturnsEmpty(t *testing.T) {
	c := &Controller{}
	resp := c.Handle(ControlRequest{RequestID: "mss2",
		Request: map[string]any{"subtype": "mcp_set_servers", "servers": map[string]any{}}})
	if resp.Response.Subtype != "success" {
		t.Fatalf("expected success: %+v", resp)
	}
}

// ── F1-C04: mcp_reconnect ────────────────────────────────────────────────────

// TestControllerMcpReconnectCallsCallback verifies serverName forwarding.
// CC ref: controlSchemas.ts:435-441.
func TestControllerMcpReconnectCallsCallback(t *testing.T) {
	var gotName string
	c := &Controller{mcpReconnect: func(n string) error {
		gotName = n
		return nil
	}}
	resp := c.Handle(ControlRequest{RequestID: "mr1",
		Request: map[string]any{"subtype": "mcp_reconnect", "serverName": "filesys"}})
	if resp.Response.Subtype != "success" {
		t.Fatalf("expected success: %+v", resp)
	}
	if gotName != "filesys" {
		t.Fatalf("serverName = %q want filesys", gotName)
	}
}

// ── F1-C04: mcp_toggle ───────────────────────────────────────────────────────

// TestControllerMcpToggleCallsCallback verifies serverName and enabled forwarding.
// CC ref: controlSchemas.ts:443-451.
func TestControllerMcpToggleCallsCallback(t *testing.T) {
	var gotName string
	var gotEnabled bool
	c := &Controller{mcpToggle: func(n string, e bool) error {
		gotName = n
		gotEnabled = e
		return nil
	}}
	resp := c.Handle(ControlRequest{RequestID: "mt1",
		Request: map[string]any{"subtype": "mcp_toggle", "serverName": "filesys", "enabled": false}})
	if resp.Response.Subtype != "success" {
		t.Fatalf("expected success: %+v", resp)
	}
	if gotName != "filesys" || gotEnabled {
		t.Fatalf("serverName=%q enabled=%v", gotName, gotEnabled)
	}
}

// ── F1-C05: reload_plugins ───────────────────────────────────────────────────

// TestControllerReloadPluginsCallsCallback verifies the response contains all
// required CC fields.
// CC ref: controlSchemas.ts:405-433.
func TestControllerReloadPluginsCallsCallback(t *testing.T) {
	called := false
	c := &Controller{reloadPlugins: func() (*ReloadPluginsResult, error) {
		called = true
		return &ReloadPluginsResult{
			Commands:   []any{},
			Agents:     []any{},
			Plugins:    []any{},
			MCPServers: []any{},
			ErrorCount: 0,
		}, nil
	}}
	resp := c.Handle(ControlRequest{RequestID: "rp1",
		Request: map[string]any{"subtype": "reload_plugins"}})
	if resp.Response.Subtype != "success" {
		t.Fatalf("expected success: %+v", resp)
	}
	if !called {
		t.Fatal("reloadPlugins callback not invoked")
	}
	for _, field := range []string{"commands", "agents", "plugins", "mcpServers", "error_count"} {
		if _, ok := resp.Response.Response[field]; !ok {
			t.Errorf("missing field %q", field)
		}
	}
}

func TestControllerReloadPluginsNoCallbackReturnsEmptySuccess(t *testing.T) {
	c := &Controller{}
	resp := c.Handle(ControlRequest{RequestID: "rp2",
		Request: map[string]any{"subtype": "reload_plugins"}})
	if resp.Response.Subtype != "success" {
		t.Fatalf("expected success: %+v", resp)
	}
}

// ── F1-C05: get_settings ─────────────────────────────────────────────────────

// TestControllerGetSettingsCallsCallback verifies the response contains effective
// and sources fields.
// CC ref: controlSchemas.ts:475-519.
func TestControllerGetSettingsCallsCallback(t *testing.T) {
	c := &Controller{getSettings: func() (*SettingsResult, error) {
		return &SettingsResult{
			Effective: map[string]any{"model": "claude-3-5-sonnet"},
			Sources: []SettingsSource{
				{Source: "userSettings", Settings: map[string]any{"model": "claude-3-5-sonnet"}},
			},
		}, nil
	}}
	resp := c.Handle(ControlRequest{RequestID: "gs1",
		Request: map[string]any{"subtype": "get_settings"}})
	if resp.Response.Subtype != "success" {
		t.Fatalf("expected success: %+v", resp)
	}
	if _, ok := resp.Response.Response["effective"]; !ok {
		t.Error("missing effective field")
	}
	if _, ok := resp.Response.Response["sources"]; !ok {
		t.Error("missing sources field")
	}
}

func TestControllerGetSettingsNoCallbackReturnsEmpty(t *testing.T) {
	c := &Controller{}
	resp := c.Handle(ControlRequest{RequestID: "gs2",
		Request: map[string]any{"subtype": "get_settings"}})
	if resp.Response.Subtype != "success" {
		t.Fatalf("expected success: %+v", resp)
	}
}

// ── F1-C05: apply_flag_settings ──────────────────────────────────────────────

// TestControllerApplyFlagSettingsCallsCallback verifies settings map forwarding.
// CC ref: controlSchemas.ts:464-473.
func TestControllerApplyFlagSettingsCallsCallback(t *testing.T) {
	var gotSettings map[string]any
	c := &Controller{applyFlagSettings: func(s map[string]any) error {
		gotSettings = s
		return nil
	}}
	resp := c.Handle(ControlRequest{RequestID: "afs1",
		Request: map[string]any{
			"subtype":  "apply_flag_settings",
			"settings": map[string]any{"verbose": true},
		}})
	if resp.Response.Subtype != "success" {
		t.Fatalf("expected success: %+v", resp)
	}
	if gotSettings["verbose"] != true {
		t.Fatalf("settings = %v", gotSettings)
	}
}

func TestControllerApplyFlagSettingsNoCallbackReturnsError(t *testing.T) {
	c := &Controller{}
	resp := c.Handle(ControlRequest{RequestID: "afs2",
		Request: map[string]any{"subtype": "apply_flag_settings", "settings": map[string]any{}}})
	if resp.Response.Subtype != "error" {
		t.Fatalf("missing callback should error: %+v", resp)
	}
}

// ── F1-C05: stop_task ────────────────────────────────────────────────────────

// TestControllerStopTaskCallsCallback verifies task_id forwarding.
// CC ref: controlSchemas.ts:455-462.
func TestControllerStopTaskCallsCallback(t *testing.T) {
	var gotID string
	c := &Controller{stopTask: func(id string) error {
		gotID = id
		return nil
	}}
	resp := c.Handle(ControlRequest{RequestID: "st1",
		Request: map[string]any{"subtype": "stop_task", "task_id": "task-42"}})
	if resp.Response.Subtype != "success" {
		t.Fatalf("expected success: %+v", resp)
	}
	if gotID != "task-42" {
		t.Fatalf("task_id = %q want task-42", gotID)
	}
}

func TestControllerStopTaskNoCallbackReturnsError(t *testing.T) {
	c := &Controller{}
	resp := c.Handle(ControlRequest{RequestID: "st2",
		Request: map[string]any{"subtype": "stop_task", "task_id": "x"}})
	if resp.Response.Subtype != "error" {
		t.Fatalf("missing callback should error: %+v", resp)
	}
}

// ── F1-C05: elicitation ──────────────────────────────────────────────────────

// TestControllerElicitationCallsCallback verifies server_name and message forwarding.
// CC ref: controlSchemas.ts:522-545.
func TestControllerElicitationCallsCallback(t *testing.T) {
	var gotServer, gotMsg string
	c := &Controller{elicitation: func(s, m string, _ map[string]any) (*ElicitationResult, error) {
		gotServer = s
		gotMsg = m
		return &ElicitationResult{Action: "accept"}, nil
	}}
	resp := c.Handle(ControlRequest{RequestID: "el1",
		Request: map[string]any{
			"subtype":         "elicitation",
			"mcp_server_name": "filesys",
			"message":         "please confirm",
		}})
	if resp.Response.Subtype != "success" {
		t.Fatalf("expected success: %+v", resp)
	}
	if gotServer != "filesys" || gotMsg != "please confirm" {
		t.Fatalf("server=%q msg=%q", gotServer, gotMsg)
	}
	if resp.Response.Response["action"] != "accept" {
		t.Fatalf("action = %v", resp.Response.Response["action"])
	}
}

func TestControllerElicitationNoCallbackReturnsError(t *testing.T) {
	c := &Controller{}
	resp := c.Handle(ControlRequest{RequestID: "el2",
		Request: map[string]any{"subtype": "elicitation", "mcp_server_name": "x", "message": "y"}})
	if resp.Response.Subtype != "error" {
		t.Fatalf("missing callback should error: %+v", resp)
	}
}
