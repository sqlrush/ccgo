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

func TestControllerUnknownSubtypeErrors(t *testing.T) {
	c := &Controller{}
	resp := c.Handle(ControlRequest{RequestID: "r5",
		Request: map[string]any{"subtype": "frobnicate"}})
	if resp.Response.Subtype != "error" || resp.Response.Error == "" {
		t.Fatalf("unknown subtype must error: %+v", resp)
	}
}
