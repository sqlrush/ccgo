package todotools

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"ccgo/internal/contracts"
	"ccgo/internal/tool"
)

func todoExecutor(t *testing.T) tool.Executor {
	t.Helper()
	registry, err := tool.NewRegistry(NewTodoWriteTool())
	if err != nil {
		t.Fatal(err)
	}
	return tool.NewExecutor(registry)
}

func todoContext() tool.Context {
	return WithState(tool.Context{
		Context:  context.Background(),
		Metadata: map[string]any{},
	}, NewState())
}

func persistentTodoContext(dir string, sessionID contracts.ID) tool.Context {
	return tool.Context{
		Context:          context.Background(),
		WorkingDirectory: dir,
		SessionID:        sessionID,
	}
}

func TestTodoWriteStoresStateAndStructuredContent(t *testing.T) {
	ctx := todoContext()
	executor := todoExecutor(t)

	result, err := executor.Execute(ctx, contracts.ToolUse{
		ID:   "toolu_todo",
		Name: "TodoWrite",
		Input: json.RawMessage(`{"todos":[
			{"content":"Implement TodoWrite","status":"in_progress","activeForm":"Implementing TodoWrite"},
			{"content":"Run tests","status":"pending","activeForm":"Running tests"}
		]}`),
	}, nil)
	if err != nil {
		t.Fatal(err)
	}
	if result.Content != todoWriteSuccess {
		t.Fatalf("content = %#v", result.Content)
	}
	state := EnsureState(ctx)
	todos := state.Snapshot()
	if len(todos) != 2 || todos[0].Content != "Implement TodoWrite" || todos[0].Status != "in_progress" || todos[1].ActiveForm != "Running tests" {
		t.Fatalf("todos = %#v", todos)
	}
	if result.StructuredContent["type"] != "todo_list" {
		t.Fatalf("structured type = %#v", result.StructuredContent)
	}
	structured, ok := result.StructuredContent["todos"].([]map[string]any)
	if !ok || len(structured) != 2 || structured[0]["content"] != "Implement TodoWrite" {
		t.Fatalf("structured todos = %#v", result.StructuredContent["todos"])
	}

	_, err = executor.Execute(ctx, contracts.ToolUse{
		ID:   "toolu_todo_update",
		Name: "TodoWrite",
		Input: json.RawMessage(`{"todos":[
			{"content":"Implement TodoWrite","status":"completed","activeForm":"Implementing TodoWrite"}
		]}`),
	}, nil)
	if err != nil {
		t.Fatal(err)
	}
	todos = state.Snapshot()
	if len(todos) != 1 || todos[0].Status != "completed" {
		t.Fatalf("updated todos = %#v", todos)
	}
}

func TestTodoWritePersistsAndRestoresSessionState(t *testing.T) {
	dir := t.TempDir()
	ctx := persistentTodoContext(dir, "session/one")
	executor := todoExecutor(t)

	result, err := executor.Execute(ctx, contracts.ToolUse{
		ID:   "toolu_todo_persist",
		Name: "TodoWrite",
		Input: json.RawMessage(`{"todos":[
			{"content":"Persist todos","status":"in_progress","activeForm":"Persisting todos"},
			{"content":"Restore todos","status":"pending","activeForm":"Restoring todos"}
		]}`),
	}, nil)
	if err != nil {
		t.Fatal(err)
	}
	storePath := filepath.Join(dir, ".claude", "todos", "session_one.json")
	if result.StructuredContent["persisted"] != true || result.StructuredContent["storePath"] != storePath {
		t.Fatalf("persistence structured content = %#v", result.StructuredContent)
	}
	data, err := os.ReadFile(storePath)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(data), `"Persist todos"`) {
		t.Fatalf("store file = %s", data)
	}

	restoredCtx := persistentTodoContext(dir, "session/one")
	state, err := LoadState(restoredCtx)
	if err != nil {
		t.Fatal(err)
	}
	todos := state.Snapshot()
	if len(todos) != 2 || todos[0].Content != "Persist todos" || todos[0].Status != "in_progress" || todos[1].Content != "Restore todos" {
		t.Fatalf("restored todos = %#v", todos)
	}
}

func TestTodoWriteValidatesInput(t *testing.T) {
	ctx := todoContext()
	executor := todoExecutor(t)
	tests := []struct {
		name  string
		input string
		want  string
	}{
		{
			name:  "missing todos",
			input: `{}`,
			want:  "input.todos is required",
		},
		{
			name:  "unknown top level",
			input: `{"todos":[],"extra":true}`,
			want:  "input.extra is not allowed",
		},
		{
			name:  "unknown todo field",
			input: `{"todos":[{"content":"x","status":"pending","activeForm":"Doing x","extra":true}]}`,
			want:  "todos[0].extra is not allowed",
		},
		{
			name:  "invalid status",
			input: `{"todos":[{"content":"x","status":"blocked","activeForm":"Doing x"}]}`,
			want:  "input.todos[0].status must be one of pending, in_progress, completed",
		},
		{
			name:  "missing activeForm",
			input: `{"todos":[{"content":"x","status":"pending"}]}`,
			want:  "todos[0].activeForm is required",
		},
		{
			name:  "multiple in progress",
			input: `{"todos":[{"content":"x","status":"in_progress","activeForm":"Doing x"},{"content":"y","status":"in_progress","activeForm":"Doing y"}]}`,
			want:  "only one todo can be in_progress",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := executor.Execute(ctx, contracts.ToolUse{
				ID:    "toolu_invalid",
				Name:  "TodoWrite",
				Input: json.RawMessage(tt.input),
			}, nil)
			if err == nil || !strings.Contains(err.Error(), tt.want) {
				t.Fatalf("err = %v, want %q", err, tt.want)
			}
		})
	}
}

func TestTodoWriteDefinitionIsPermissionSafeButOrdered(t *testing.T) {
	todo := NewTodoWriteTool()
	if !todo.IsReadOnly(nil) {
		t.Fatalf("TodoWrite should be read-only for permission decisions")
	}
	if todo.IsConcurrencySafe(nil) {
		t.Fatalf("TodoWrite should preserve ordered state updates")
	}
	if todo.IsDestructive(nil) {
		t.Fatalf("TodoWrite should not be destructive")
	}
}
