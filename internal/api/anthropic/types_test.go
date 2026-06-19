package anthropic

import (
	"testing"

	"ccgo/internal/contracts"
)

func TestToolFromContractPreservesStrictAndDeferLoading(t *testing.T) {
	got := ToolFromContract(contracts.ToolDefinition{
		Name:        "Task",
		Description: "Start a task",
		InputSchema: contracts.JSONSchema{
			"type": "object",
		},
		Strict:      true,
		ShouldDefer: true,
	})
	if got.Name != "Task" || got.Description != "Start a task" || got.InputSchema["type"] != "object" {
		t.Fatalf("tool = %#v", got)
	}
	if !got.Strict {
		t.Fatalf("strict = false, want true")
	}
	if !got.DeferLoading {
		t.Fatalf("defer loading = false, want true")
	}
}

func TestToolFromContractAlwaysLoadOverridesShouldDefer(t *testing.T) {
	got := ToolFromContract(contracts.ToolDefinition{
		Name:        "Task",
		InputSchema: contracts.JSONSchema{"type": "object"},
		ShouldDefer: true,
		AlwaysLoad:  true,
	})
	if got.DeferLoading {
		t.Fatalf("defer loading = true, want false when always_load is set")
	}
}
