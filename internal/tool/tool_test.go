package tool

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"ccgo/internal/contracts"
	"ccgo/internal/permissions"
)

func TestRegistryAliasLookup(t *testing.T) {
	read := FuncTool{DefinitionValue: contracts.ToolDefinition{Name: "Read", Aliases: []string{"View"}}}
	registry, err := NewRegistry(read)
	if err != nil {
		t.Fatal(err)
	}
	got, ok := registry.Lookup("view")
	if !ok {
		t.Fatalf("alias lookup failed")
	}
	if got.Name() != "Read" {
		t.Fatalf("tool = %q", got.Name())
	}
}

func TestValidateSchema(t *testing.T) {
	schema := contracts.JSONSchema{
		"type":     "object",
		"required": []string{"path"},
		"properties": map[string]any{
			"path":     map[string]any{"type": "string", "minLength": 2},
			"mode":     map[string]any{"type": "string", "enum": []any{"read", "write"}},
			"count":    map[string]any{"type": "integer", "enum": []any{1, 2}},
			"limit":    map[string]any{"type": "integer", "minimum": 1, "maximum": 5},
			"metadata": map[string]any{"type": "object", "additionalProperties": map[string]any{"type": "string"}},
			"tags":     map[string]any{"type": "array", "items": map[string]any{"type": "string"}},
		},
	}
	if err := ValidateSchema(schema, json.RawMessage(`{}`)); err == nil || !strings.Contains(err.Error(), "input.path is required") {
		t.Fatalf("err = %v", err)
	}
	if err := ValidateSchema(schema, json.RawMessage(`{"path":3}`)); err == nil {
		t.Fatalf("expected schema validation error")
	}
	if err := ValidateSchema(schema, json.RawMessage(`{"path":"x"}`)); err == nil || !strings.Contains(err.Error(), "input.path must be at least 2 characters") {
		t.Fatalf("err = %v", err)
	}
	if err := ValidateSchema(schema, json.RawMessage(`{"path":"README.md","tags":[3]}`)); err == nil || !strings.Contains(err.Error(), "input.tags[0] must be string") {
		t.Fatalf("err = %v", err)
	}
	if err := ValidateSchema(schema, json.RawMessage(`{"path":"README.md","mode":"delete"}`)); err == nil || !strings.Contains(err.Error(), "input.mode must be one of read, write") {
		t.Fatalf("err = %v", err)
	}
	if err := ValidateSchema(schema, json.RawMessage(`{"path":"README.md","count":3}`)); err == nil || !strings.Contains(err.Error(), "input.count must be one of 1, 2") {
		t.Fatalf("err = %v", err)
	}
	if err := ValidateSchema(schema, json.RawMessage(`{"path":"README.md","limit":0}`)); err == nil || !strings.Contains(err.Error(), "input.limit must be at least 1") {
		t.Fatalf("err = %v", err)
	}
	if err := ValidateSchema(schema, json.RawMessage(`{"path":"README.md","limit":6}`)); err == nil || !strings.Contains(err.Error(), "input.limit must be at most 5") {
		t.Fatalf("err = %v", err)
	}
	if err := ValidateSchema(schema, json.RawMessage(`{"path":"README.md","metadata":{"env":3}}`)); err == nil || !strings.Contains(err.Error(), "input.metadata.env must be string") {
		t.Fatalf("err = %v", err)
	}
	if err := ValidateSchema(schema, json.RawMessage(`{"path":"README.md"}`)); err != nil {
		t.Fatal(err)
	}
	if err := ValidateSchema(schema, json.RawMessage(`{"path":"README.md","mode":"read","count":2,"limit":5,"metadata":{"env":"prod"}}`)); err != nil {
		t.Fatal(err)
	}
}

func TestValidateSchemaAdvancedConstraints(t *testing.T) {
	schema := contracts.JSONSchema{
		"type":          "object",
		"minProperties": 2,
		"maxProperties": 8,
		"properties": map[string]any{
			"kind": map[string]any{"const": "task"},
			"name": map[string]any{
				"type":      "string",
				"maxLength": 8,
				"pattern":   "^[a-z][a-z0-9-]*$",
			},
			"ratio": map[string]any{
				"type":             "number",
				"exclusiveMinimum": 0,
				"exclusiveMaximum": 1,
			},
			"legacy_ratio": map[string]any{
				"type":             "number",
				"minimum":          0,
				"exclusiveMinimum": true,
			},
			"tags": map[string]any{
				"type":     "array",
				"minItems": 1,
				"maxItems": 2,
				"items":    map[string]any{"type": "string"},
			},
			"choice": map[string]any{
				"anyOf": []contracts.JSONSchema{
					{"type": "string", "enum": []any{"fast", "safe"}},
					{"type": "integer", "minimum": 1, "maximum": 3},
				},
			},
			"single": map[string]any{
				"oneOf": []map[string]any{
					map[string]any{"type": "string"},
					map[string]any{"enum": []any{"same"}},
				},
			},
			"combined": map[string]any{
				"allOf": []any{
					map[string]any{"type": "string"},
					map[string]any{"minLength": 3},
				},
			},
		},
	}
	cases := []struct {
		name  string
		input string
		want  string
	}{
		{"min-properties", `{"kind":"task"}`, "input must contain at least 2 properties"},
		{"max-properties", `{"kind":"task","name":"alpha","ratio":0.5,"tags":["x"],"choice":"fast","single":"x","combined":"abc","extra":1,"extra2":2}`, "input must contain at most 8 properties"},
		{"const", `{"kind":"job","name":"alpha"}`, "input.kind must be task"},
		{"pattern", `{"kind":"task","name":"Alpha"}`, "input.name must match pattern ^[a-z][a-z0-9-]*$"},
		{"max-length", `{"kind":"task","name":"alpha-beta"}`, "input.name must be at most 8 characters"},
		{"exclusive-minimum", `{"kind":"task","ratio":0}`, "input.ratio must be greater than 0"},
		{"exclusive-maximum", `{"kind":"task","ratio":1}`, "input.ratio must be less than 1"},
		{"legacy-exclusive-minimum", `{"kind":"task","legacy_ratio":0}`, "input.legacy_ratio must be greater than 0"},
		{"min-items", `{"kind":"task","tags":[]}`, "input.tags must contain at least 1 items"},
		{"max-items", `{"kind":"task","tags":["a","b","c"]}`, "input.tags must contain at most 2 items"},
		{"any-of", `{"kind":"task","choice":9}`, "input.choice must match at least one allowed schema"},
		{"one-of", `{"kind":"task","single":"same"}`, "input.single must match exactly one allowed schema"},
		{"all-of", `{"kind":"task","combined":"ab"}`, "input.combined must be at least 3 characters"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			err := ValidateSchema(schema, json.RawMessage(tc.input))
			if err == nil || !strings.Contains(err.Error(), tc.want) {
				t.Fatalf("err = %v, want %q", err, tc.want)
			}
		})
	}
	if err := ValidateSchema(schema, json.RawMessage(`{"kind":"task","name":"alpha","ratio":0.5,"legacy_ratio":0.5,"tags":["a","b"],"choice":2,"single":"x","combined":"abc"}`)); err != nil {
		t.Fatal(err)
	}
}

func TestFuncToolValidateUsesDynamicInputSchema(t *testing.T) {
	dynamic := FuncTool{
		DefinitionValue: contracts.ToolDefinition{
			Name: "Dynamic",
			InputSchema: contracts.JSONSchema{
				"type": "object",
				"properties": map[string]any{
					"mode": map[string]any{"type": "string", "enum": []any{"static"}},
				},
			},
		},
		InputSchemaFunc: func(ctx PromptContext) contracts.JSONSchema {
			allowed, _ := ctx.Metadata["allowed_mode"].(string)
			if allowed == "" {
				allowed = "dynamic"
			}
			return contracts.JSONSchema{
				"type": "object",
				"properties": map[string]any{
					"mode": map[string]any{"type": "string", "enum": []any{allowed}},
				},
			}
		},
	}
	ctx := Context{Metadata: map[string]any{"allowed_mode": "dynamic"}}
	if err := dynamic.Validate(ctx, json.RawMessage(`{"mode":"dynamic"}`)); err != nil {
		t.Fatal(err)
	}
	if err := dynamic.Validate(ctx, json.RawMessage(`{"mode":"static"}`)); err == nil || !strings.Contains(err.Error(), "input.mode must be one of dynamic") {
		t.Fatalf("err = %v", err)
	}
}

func TestExecutorRunsAllowedTool(t *testing.T) {
	engine := permissions.NewEngine(contracts.PermissionContext{Mode: contracts.PermissionDefault})
	registry, err := NewRegistry(FuncTool{
		DefinitionValue: contracts.ToolDefinition{Name: "Read", ReadOnly: true},
		CallFunc: func(ctx Context, raw json.RawMessage, sink ProgressSink) (contracts.ToolResult, error) {
			return contracts.ToolResult{Content: "ok"}, nil
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	executor := NewExecutor(registry)
	result, err := executor.Execute(Context{
		Context:     context.Background(),
		Permissions: NewEnginePermissionDecider(engine),
	}, contracts.ToolUse{ID: "toolu_1", Name: "Read", Input: json.RawMessage(`{}`)}, nil)
	if err != nil {
		t.Fatal(err)
	}
	if result.ToolUseID != "toolu_1" || result.Content != "ok" || result.IsError {
		t.Fatalf("result = %#v", result)
	}
}

func TestExecutorHooksCanUpdateInputAndEmitProgress(t *testing.T) {
	var seenInput string
	var progress []contracts.ToolProgress
	registry, err := NewRegistry(FuncTool{
		DefinitionValue: contracts.ToolDefinition{
			Name:     "Echo",
			ReadOnly: true,
			InputSchema: contracts.JSONSchema{"type": "object", "required": []any{"text"}, "properties": map[string]any{
				"text": map[string]any{"type": "string"},
			}},
		},
		CallFunc: func(ctx Context, raw json.RawMessage, sink ProgressSink) (contracts.ToolResult, error) {
			var input struct {
				Text string `json:"text"`
			}
			if err := json.Unmarshal(raw, &input); err != nil {
				t.Fatal(err)
			}
			seenInput = input.Text
			_ = SendProgress(sink, "", "custom", map[string]any{"step": "call"})
			return contracts.ToolResult{Content: input.Text}, nil
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	executor := Executor{
		Registry: registry,
		Hooks: []Hook{
			HookFunc(func(ctx Context, event HookEvent) (HookResult, error) {
				if event.Phase != HookPreToolUse {
					return HookResult{}, nil
				}
				return HookResult{UpdatedInput: json.RawMessage(`{"text":"from-hook"}`)}, nil
			}),
			HookFunc(func(ctx Context, event HookEvent) (HookResult, error) {
				if event.Phase == HookPostToolUse {
					return HookResult{Metadata: map[string]any{"ok": true}}, nil
				}
				return HookResult{}, nil
			}),
		},
	}
	result, err := executor.Execute(
		Context{Context: context.Background()},
		contracts.ToolUse{ID: "toolu_hook", Name: "Echo", Input: json.RawMessage(`{"text":"original"}`)},
		ProgressFunc(func(p contracts.ToolProgress) error {
			progress = append(progress, p)
			return nil
		}),
	)
	if err != nil {
		t.Fatal(err)
	}
	if seenInput != "from-hook" || result.Content != "from-hook" {
		t.Fatalf("seenInput=%q result=%#v", seenInput, result)
	}
	if result.Meta["post_tool_use_hook"].(map[string]any)["ok"] != true {
		t.Fatalf("meta = %#v", result.Meta)
	}
	if got := progressTypes(progress); strings.Join(got, ",") != "started,custom,completed" {
		t.Fatalf("progress = %#v", got)
	}
	for _, item := range progress {
		if item.ToolUseID != "toolu_hook" {
			t.Fatalf("progress tool use id = %#v", progress)
		}
	}
}

func TestExecutorPreHookCanBlock(t *testing.T) {
	registry, err := NewRegistry(FuncTool{
		DefinitionValue: contracts.ToolDefinition{Name: "Read", ReadOnly: true},
		CallFunc: func(ctx Context, raw json.RawMessage, sink ProgressSink) (contracts.ToolResult, error) {
			t.Fatalf("call should not run")
			return contracts.ToolResult{}, nil
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	_, err = (Executor{
		Registry: registry,
		Hooks: []Hook{HookFunc(func(ctx Context, event HookEvent) (HookResult, error) {
			return HookResult{Block: true, Message: "blocked"}, nil
		})},
	}).Execute(Context{Context: context.Background()}, contracts.ToolUse{ID: "toolu_block", Name: "Read"}, nil)
	var blocked HookBlockedError
	if !errors.As(err, &blocked) || blocked.Phase != HookPreToolUse {
		t.Fatalf("error = %#v", err)
	}
}

func TestExecutorReturnsPermissionError(t *testing.T) {
	engine := permissions.NewEngine(contracts.PermissionContext{Mode: contracts.PermissionDontAsk})
	registry, err := NewRegistry(FuncTool{
		DefinitionValue: contracts.ToolDefinition{Name: "Bash", Destructive: true},
		CallFunc: func(ctx Context, raw json.RawMessage, sink ProgressSink) (contracts.ToolResult, error) {
			t.Fatalf("call should not run")
			return contracts.ToolResult{}, nil
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	_, err = NewExecutor(registry).Execute(Context{
		Context:     context.Background(),
		Permissions: NewEnginePermissionDecider(engine),
	}, contracts.ToolUse{ID: "toolu_2", Name: "Bash"}, nil)
	var permissionErr PermissionError
	if !errors.As(err, &permissionErr) {
		t.Fatalf("error = %v, want PermissionError", err)
	}
	if permissionErr.Decision.Behavior != contracts.PermissionDeny {
		t.Fatalf("behavior = %q", permissionErr.Decision.Behavior)
	}
}

func TestEnginePermissionDeciderUsesInternalPathsFromMetadata(t *testing.T) {
	dir := t.TempDir()
	autoMemoryDir := filepath.Join(dir, "memory")
	if err := os.MkdirAll(autoMemoryDir, 0o755); err != nil {
		t.Fatal(err)
	}
	engine := permissions.NewEngine(contracts.PermissionContext{Mode: contracts.PermissionDontAsk})
	decider := NewEnginePermissionDecider(engine)
	writeTool := FuncTool{DefinitionValue: contracts.ToolDefinition{Name: "Write"}}
	metadata := map[string]any{
		MetadataInternalPathContextKey: permissions.InternalPathContext{AutoMemoryDir: autoMemoryDir},
	}
	internal := InternalPathContextFromMetadata(metadata)
	if internal.AutoMemoryDir != autoMemoryDir {
		t.Fatalf("internal paths = %#v", internal)
	}
	check := permissions.CheckEditableInternalPath(filepath.Join(autoMemoryDir, "fact.md"), internal)
	if !check.Allowed || !strings.Contains(check.Reason, "auto memory") {
		t.Fatalf("internal path check = %#v", check)
	}
	direct := engine.Decide(permissions.Request{
		ToolName:         "Write",
		Path:             filepath.Join(autoMemoryDir, "fact.md"),
		WorkingDirectory: dir,
		WritesFiles:      true,
		InternalPaths:    internal,
	})
	if direct.Behavior != contracts.PermissionAllow || !strings.Contains(direct.Message, "auto memory") {
		t.Fatalf("direct decision = %#v", direct)
	}

	decision, err := decider.DecideTool(writeTool, json.RawMessage(fmt.Sprintf(`{"file_path":%q}`, filepath.Join(autoMemoryDir, "fact.md"))), Context{
		WorkingDirectory: dir,
		Metadata:         metadata,
	})
	if err != nil {
		t.Fatal(err)
	}
	if decision.Behavior != contracts.PermissionAllow || !strings.Contains(decision.Message, "auto memory") {
		t.Fatalf("decision = %#v", decision)
	}

	notebookDecision, err := decider.DecideTool(FuncTool{DefinitionValue: contracts.ToolDefinition{Name: "NotebookEdit"}}, json.RawMessage(fmt.Sprintf(`{"notebook_path":%q}`, filepath.Join(autoMemoryDir, "analysis.ipynb"))), Context{
		WorkingDirectory: dir,
		Metadata:         metadata,
	})
	if err != nil {
		t.Fatal(err)
	}
	if notebookDecision.Behavior != contracts.PermissionAllow || !strings.Contains(notebookDecision.Message, "auto memory") {
		t.Fatalf("notebook decision = %#v", notebookDecision)
	}
}

func TestEnginePermissionDeciderSurfacesSandboxOverride(t *testing.T) {
	engine := permissions.NewEngine(contracts.PermissionContext{Mode: contracts.PermissionDefault})
	decider := NewEnginePermissionDecider(engine)
	bashTool := FuncTool{
		DefinitionValue: contracts.ToolDefinition{Name: "Bash", ReadOnly: true},
		NormalizeFunc: func(raw json.RawMessage) (json.RawMessage, error) {
			return json.RawMessage(`{"command":"git status --short","dangerouslyDisableSandbox":true}`), nil
		},
	}
	decision, err := bashTool.CheckPermissions(Context{Permissions: decider}, json.RawMessage(`{"command":"git status --short","dangerouslyDisableSandbox":"true"}`))
	if err != nil {
		t.Fatal(err)
	}
	if decision.Behavior != contracts.PermissionAsk || !strings.Contains(decision.Message, "sandbox override") {
		t.Fatalf("decision = %#v", decision)
	}
}

func TestExecutorRunsPermissionDeniedHook(t *testing.T) {
	engine := permissions.NewEngine(contracts.PermissionContext{Mode: contracts.PermissionDontAsk})
	hookCalled := false
	registry, err := NewRegistry(FuncTool{
		DefinitionValue: contracts.ToolDefinition{Name: "Bash", Destructive: true},
		CallFunc: func(ctx Context, raw json.RawMessage, sink ProgressSink) (contracts.ToolResult, error) {
			t.Fatalf("call should not run")
			return contracts.ToolResult{}, nil
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	result, err := (Executor{
		Registry: registry,
		Hooks: []Hook{HookFunc(func(ctx Context, event HookEvent) (HookResult, error) {
			if event.Phase == HookPermissionDenied && event.Decision != nil {
				hookCalled = true
				return HookResult{Message: "logged", Metadata: map[string]any{"behavior": string(event.Decision.Behavior)}}, nil
			}
			return HookResult{}, nil
		})},
	}).Execute(Context{
		Context:     context.Background(),
		Permissions: NewEnginePermissionDecider(engine),
	}, contracts.ToolUse{ID: "toolu_denied", Name: "Bash"}, nil)
	var permissionErr PermissionError
	if !errors.As(err, &permissionErr) {
		t.Fatalf("error = %v, want PermissionError", err)
	}
	if !hookCalled || result.Meta["permission_denied_hook_message"] != "logged" {
		t.Fatalf("hookCalled=%v result=%#v", hookCalled, result)
	}
}

func TestExecutorHonorsCancelledContextBeforeCall(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	called := false
	registry, err := NewRegistry(FuncTool{
		DefinitionValue: contracts.ToolDefinition{Name: "Read", ReadOnly: true},
		CallFunc: func(ctx Context, raw json.RawMessage, sink ProgressSink) (contracts.ToolResult, error) {
			called = true
			return contracts.ToolResult{Content: "unexpected"}, nil
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	_, err = NewExecutor(registry).Execute(Context{Context: ctx}, contracts.ToolUse{ID: "toolu_cancel", Name: "Read"}, nil)
	if !errors.Is(err, context.Canceled) || called {
		t.Fatalf("err=%v called=%v", err, called)
	}
}

func TestRunToolsPartitionsConcurrencySafeTools(t *testing.T) {
	var mu sync.Mutex
	var running int
	var maxRunning int
	makeTool := func(name string, safe bool) FuncTool {
		return FuncTool{
			DefinitionValue: contracts.ToolDefinition{Name: name, ConcurrencySafe: safe, ReadOnly: true},
			CallFunc: func(ctx Context, raw json.RawMessage, sink ProgressSink) (contracts.ToolResult, error) {
				mu.Lock()
				running++
				if running > maxRunning {
					maxRunning = running
				}
				mu.Unlock()
				time.Sleep(20 * time.Millisecond)
				mu.Lock()
				running--
				mu.Unlock()
				return contracts.ToolResult{Content: name}, nil
			},
		}
	}
	registry, err := NewRegistry(makeTool("ReadA", true), makeTool("ReadB", true), makeTool("Edit", false))
	if err != nil {
		t.Fatal(err)
	}
	updates := RunTools(Context{Context: context.Background()}, NewExecutor(registry), []contracts.ToolUse{
		{ID: "a", Name: "ReadA"},
		{ID: "b", Name: "ReadB"},
		{ID: "c", Name: "Edit"},
	}, nil, RunOptions{MaxConcurrency: 2})
	count := 0
	for update := range updates {
		if update.Err != nil {
			t.Fatal(update.Err)
		}
		count++
	}
	if count != 3 {
		t.Fatalf("updates = %d", count)
	}
	if maxRunning < 2 {
		t.Fatalf("safe tools did not run concurrently, maxRunning=%d", maxRunning)
	}
}

func TestExecutorTruncatesAndStoresLargeResult(t *testing.T) {
	registry, err := NewRegistry(FuncTool{
		DefinitionValue: contracts.ToolDefinition{Name: "Big", ReadOnly: true, MaxResultSizeChars: 5},
		CallFunc: func(ctx Context, raw json.RawMessage, sink ProgressSink) (contracts.ToolResult, error) {
			return contracts.ToolResult{Content: "0123456789"}, nil
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	result, err := (Executor{Registry: registry, ResultStoreDir: t.TempDir()}).Execute(Context{Context: context.Background()}, contracts.ToolUse{ID: "toolu_big", Name: "Big"}, nil)
	if err != nil {
		t.Fatal(err)
	}
	if result.Meta["truncated"] != true {
		t.Fatalf("meta = %#v", result.Meta)
	}
	path, _ := result.Meta["full_output_path"].(string)
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if string(data) != "0123456789" {
		t.Fatalf("stored = %q", string(data))
	}
}

func progressTypes(progress []contracts.ToolProgress) []string {
	out := make([]string, 0, len(progress))
	for _, item := range progress {
		out = append(out, item.Type)
	}
	return out
}
