package mcp

import (
	"bytes"
	"context"
	"encoding/json"
	"strings"
	"testing"

	"ccgo/internal/contracts"
	"ccgo/internal/tool"
)

func TestBuiltinServerHandlesInitializeListAndCall(t *testing.T) {
	registry, err := tool.NewRegistry(testMCPServerEchoTool(true))
	if err != nil {
		t.Fatal(err)
	}
	server, err := NewBuiltinServer(BuiltinServerOptions{
		Registry:         registry,
		Executor:         tool.NewExecutor(registry),
		WorkingDirectory: "/tmp",
	})
	if err != nil {
		t.Fatal(err)
	}

	input := strings.Join([]string{
		`{"jsonrpc":"2.0","id":"1","method":"initialize","params":{}}`,
		`{"jsonrpc":"2.0","method":"notifications/initialized"}`,
		`{"jsonrpc":"2.0","id":"2","method":"tools/list","params":{}}`,
		`{"jsonrpc":"2.0","id":"3","method":"tools/call","params":{"name":"Echo","arguments":{"text":"hello"}}}`,
		`{"jsonrpc":"2.0","id":"4","method":"ping","params":{}}`,
		`{"jsonrpc":"2.0","id":"5","method":"resources/list","params":{}}`,
		`{"jsonrpc":"2.0","id":"6","method":"prompts/list","params":{}}`,
		"",
	}, "\n")
	var out bytes.Buffer
	if err := server.Run(context.Background(), strings.NewReader(input), &out); err != nil {
		t.Fatal(err)
	}
	responses := decodeServerResponses(t, out.String())
	if len(responses) != 6 {
		t.Fatalf("responses = %#v output=%s", responses, out.String())
	}
	if !strings.Contains(string(mustMarshal(t, responses[0].Result)), `"protocolVersion":"2025-06-18"`) {
		t.Fatalf("initialize = %#v", responses[0])
	}
	if !strings.Contains(string(mustMarshal(t, responses[1].Result)), `"name":"Echo"`) {
		t.Fatalf("tools/list = %#v", responses[1])
	}
	callResult := string(mustMarshal(t, responses[2].Result))
	if !strings.Contains(callResult, `"type":"text"`) || !strings.Contains(callResult, `"text":"hello"`) {
		t.Fatalf("tools/call = %s", callResult)
	}
	if string(mustMarshal(t, responses[3].Result)) != "{}" {
		t.Fatalf("ping = %#v", responses[3].Result)
	}
	if !strings.Contains(string(mustMarshal(t, responses[4].Result)), `"resources":[]`) {
		t.Fatalf("resources/list = %#v", responses[4].Result)
	}
	if !strings.Contains(string(mustMarshal(t, responses[5].Result)), `"prompts":[]`) {
		t.Fatalf("prompts/list = %#v", responses[5].Result)
	}
}

func TestBuiltinServerDeniesMutatingToolsByDefault(t *testing.T) {
	registry, err := tool.NewRegistry(testMCPServerEchoTool(false))
	if err != nil {
		t.Fatal(err)
	}
	server, err := NewBuiltinServer(BuiltinServerOptions{
		Registry: registry,
		Executor: tool.NewExecutor(registry),
	})
	if err != nil {
		t.Fatal(err)
	}

	var out bytes.Buffer
	err = server.Run(context.Background(), strings.NewReader(`{"jsonrpc":"2.0","id":"1","method":"tools/call","params":{"name":"Echo","arguments":{"text":"hello"}}}`+"\n"), &out)
	if err != nil {
		t.Fatal(err)
	}
	responses := decodeServerResponses(t, out.String())
	if len(responses) != 1 {
		t.Fatalf("responses = %#v", responses)
	}
	result := string(mustMarshal(t, responses[0].Result))
	if !strings.Contains(result, `"isError":true`) || !strings.Contains(result, "mutating tools are disabled") {
		t.Fatalf("result = %s", result)
	}
}

func TestBuiltinServerAllowsMutatingToolsWhenConfigured(t *testing.T) {
	registry, err := tool.NewRegistry(testMCPServerEchoTool(false))
	if err != nil {
		t.Fatal(err)
	}
	server, err := NewBuiltinServer(BuiltinServerOptions{
		Registry:           registry,
		Executor:           tool.NewExecutor(registry),
		AllowMutatingTools: true,
	})
	if err != nil {
		t.Fatal(err)
	}

	var out bytes.Buffer
	err = server.Run(context.Background(), strings.NewReader(`{"jsonrpc":"2.0","id":"1","method":"tools/call","params":{"name":"Echo","arguments":{"text":"hello"}}}`+"\n"), &out)
	if err != nil {
		t.Fatal(err)
	}
	responses := decodeServerResponses(t, out.String())
	result := string(mustMarshal(t, responses[0].Result))
	if strings.Contains(result, `"isError":true`) || !strings.Contains(result, `"text":"hello"`) {
		t.Fatalf("result = %s", result)
	}
}

func TestBuiltinServerPreservesJSONRPCID(t *testing.T) {
	registry, err := tool.NewRegistry(testMCPServerEchoTool(true))
	if err != nil {
		t.Fatal(err)
	}
	server, err := NewBuiltinServer(BuiltinServerOptions{
		Registry: registry,
		Executor: tool.NewExecutor(registry),
	})
	if err != nil {
		t.Fatal(err)
	}
	input := strings.Join([]string{
		`{"jsonrpc":"2.0","id":7,"method":"ping","params":{}}`,
		`{"jsonrpc":"2.0","id":null,"method":"ping","params":{}}`,
		"",
	}, "\n")
	var out bytes.Buffer
	if err := server.Run(context.Background(), strings.NewReader(input), &out); err != nil {
		t.Fatal(err)
	}
	lines := strings.Split(strings.TrimSpace(out.String()), "\n")
	if len(lines) != 2 {
		t.Fatalf("output = %s", out.String())
	}
	if !strings.Contains(lines[0], `"id":7`) {
		t.Fatalf("numeric id response = %s", lines[0])
	}
	if !strings.Contains(lines[1], `"id":null`) {
		t.Fatalf("null id response = %s", lines[1])
	}
}

func TestBuiltinServerReportsEmptyResourcesAndPrompts(t *testing.T) {
	registry, err := tool.NewRegistry(testMCPServerEchoTool(true))
	if err != nil {
		t.Fatal(err)
	}
	server, err := NewBuiltinServer(BuiltinServerOptions{
		Registry: registry,
		Executor: tool.NewExecutor(registry),
	})
	if err != nil {
		t.Fatal(err)
	}
	input := strings.Join([]string{
		`{"jsonrpc":"2.0","id":"1","method":"resources/read","params":{"uri":"file:///missing"}}`,
		`{"jsonrpc":"2.0","id":"2","method":"prompts/get","params":{"name":"missing"}}`,
		"",
	}, "\n")
	var out bytes.Buffer
	if err := server.Run(context.Background(), strings.NewReader(input), &out); err != nil {
		t.Fatal(err)
	}
	responses := decodeServerResponses(t, out.String())
	if len(responses) != 2 {
		t.Fatalf("responses = %#v", responses)
	}
	if responses[0].Error == nil || !strings.Contains(responses[0].Error.Message, "resource not found") {
		t.Fatalf("resource error = %#v", responses[0].Error)
	}
	if responses[1].Error == nil || !strings.Contains(responses[1].Error.Message, "prompt not found") {
		t.Fatalf("prompt error = %#v", responses[1].Error)
	}
}

func testMCPServerEchoTool(readOnly bool) tool.Tool {
	return tool.FuncTool{
		DefinitionValue: contracts.ToolDefinition{
			Name:        "Echo",
			Description: "Echo text.",
			InputSchema: contracts.JSONSchema{
				"type":     "object",
				"required": []any{"text"},
				"properties": map[string]any{
					"text": map[string]any{"type": "string"},
				},
			},
			ReadOnly:        readOnly,
			ConcurrencySafe: readOnly,
		},
		CallFunc: func(_ tool.Context, raw json.RawMessage, _ tool.ProgressSink) (contracts.ToolResult, error) {
			var input struct {
				Text string `json:"text"`
			}
			if err := json.Unmarshal(raw, &input); err != nil {
				return contracts.ToolResult{}, err
			}
			return contracts.ToolResult{Content: input.Text}, nil
		},
	}
}

func decodeServerResponses(t *testing.T, output string) []serverRPCResponse {
	t.Helper()
	lines := strings.Split(strings.TrimSpace(output), "\n")
	responses := make([]serverRPCResponse, 0, len(lines))
	for _, line := range lines {
		if strings.TrimSpace(line) == "" {
			continue
		}
		var response serverRPCResponse
		if err := json.Unmarshal([]byte(line), &response); err != nil {
			t.Fatalf("decode response %q: %v", line, err)
		}
		responses = append(responses, response)
	}
	return responses
}

func mustMarshal(t *testing.T, value any) []byte {
	t.Helper()
	data, err := json.Marshal(value)
	if err != nil {
		t.Fatal(err)
	}
	return data
}
