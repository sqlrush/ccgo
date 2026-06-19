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

func TestToolFromContractDescriptionFallback(t *testing.T) {
	cases := []struct {
		name string
		def  contracts.ToolDefinition
		want string
	}{
		{
			name: "description",
			def: contracts.ToolDefinition{
				Name:        "Primary",
				Description: "Primary description",
				Prompt:      "Prompt description",
				SearchHint:  "search hint",
			},
			want: "Primary description",
		},
		{
			name: "prompt",
			def: contracts.ToolDefinition{
				Name:       "PromptOnly",
				Prompt:     "Prompt description",
				SearchHint: "search hint",
			},
			want: "Prompt description",
		},
		{
			name: "search hint",
			def: contracts.ToolDefinition{
				Name:       "HintOnly",
				SearchHint: "search hint",
			},
			want: "search hint",
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := ToolFromContract(tc.def)
			if got.Description != tc.want {
				t.Fatalf("description = %q, want %q", got.Description, tc.want)
			}
		})
	}
}
