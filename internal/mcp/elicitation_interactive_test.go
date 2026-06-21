package mcp

import (
	"context"
	"testing"
)

func TestInteractiveElicitationHandlerAccept(t *testing.T) {
	prompt := func(ctx context.Context, req ElicitationRequest) (string, map[string]any, error) {
		if req.Message != "Pick one" {
			t.Fatalf("message = %q", req.Message)
		}
		return "accept", map[string]any{"choice": "a"}, nil
	}
	handler := InteractiveElicitationHandler(prompt)
	resp, err := handler(context.Background(), ElicitationRequest{Message: "Pick one"})
	if err != nil {
		t.Fatalf("handler err: %v", err)
	}
	if resp["action"] != "accept" {
		t.Fatalf("action = %v want accept", resp["action"])
	}
	content, _ := resp["content"].(map[string]any)
	if content["choice"] != "a" {
		t.Fatalf("content = %v", resp["content"])
	}
}

func TestInteractiveElicitationHandlerNilPromptDeclines(t *testing.T) {
	handler := InteractiveElicitationHandler(nil)
	resp, err := handler(context.Background(), ElicitationRequest{Message: "x"})
	if err != nil {
		t.Fatalf("handler err: %v", err)
	}
	if resp["action"] != "cancel" {
		t.Fatalf("nil prompt should cancel, got %v", resp["action"])
	}
}

func TestInteractiveElicitationHandlerErrorCancels(t *testing.T) {
	handler := InteractiveElicitationHandler(func(context.Context, ElicitationRequest) (string, map[string]any, error) {
		return "", nil, context.Canceled
	})
	resp, err := handler(context.Background(), ElicitationRequest{})
	if err != nil {
		t.Fatalf("handler should not surface prompt error: %v", err)
	}
	if resp["action"] != "cancel" {
		t.Fatalf("error should cancel, got %v", resp["action"])
	}
}
