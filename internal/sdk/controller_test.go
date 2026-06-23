package sdk

import (
	"fmt"
	"testing"
)

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
