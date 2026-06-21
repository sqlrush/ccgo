package todotools

import (
	"encoding/json"
	"fmt"
	"strings"

	"ccgo/internal/contracts"
	"ccgo/internal/tool"
)

const todoWriteSuccess = "Todos have been modified successfully. Ensure that you continue to use the todo list to track your progress. Please proceed with the current tasks if applicable."

type todoWriteInput struct {
	Todos []Todo `json:"todos"`
}

func NewTodoWriteTool() tool.Tool {
	return tool.FuncTool{
		DefinitionValue: contracts.ToolDefinition{
			Name:            "TodoWrite",
			Description:     "Create and update the current task list for the session.",
			SearchHint:      "update todo list",
			ReadOnly:        true,
			ConcurrencySafe: false,
			Strict:          true,
			InputSchema: contracts.JSONSchema{
				"type":     "object",
				"required": []any{"todos"},
				"properties": map[string]any{
					"todos": map[string]any{
						"type": "array",
						"items": map[string]any{
							"type":     "object",
							"required": []any{"content", "status", "activeForm"},
							"properties": map[string]any{
								"content":    map[string]any{"type": "string"},
								"status":     map[string]any{"type": "string", "enum": []any{"pending", "in_progress", "completed"}},
								"activeForm": map[string]any{"type": "string"},
							},
						},
					},
				},
			},
		},
		PromptFunc: func(tool.PromptContext) (string, error) {
			return "Updates the session todo list. Provide the complete todos array with content (imperative phrase describing the task), status (pending, in_progress, or completed), and activeForm (present-continuous phrase shown while in_progress, e.g. \"Writing the parser\"). Keep at most one todo in_progress.", nil
		},
		ValidateFunc:    validateTodoWrite,
		CallFunc:        callTodoWrite,
		ReadOnlyFunc:    func(json.RawMessage) bool { return true },
		ConcurrencyFunc: func(json.RawMessage) bool { return false },
	}
}

func validateTodoWrite(_ tool.Context, raw json.RawMessage) error {
	input, err := decodeTodoWrite(raw)
	if err != nil {
		return err
	}
	return validateTodos(input.Todos)
}

func validateTodos(todos []Todo) error {
	inProgress := 0
	for i, todo := range todos {
		prefix := fmt.Sprintf("todos[%d]", i)
		if strings.TrimSpace(todo.Content) == "" {
			return fmt.Errorf("%s.content is required", prefix)
		}
		if strings.TrimSpace(todo.ActiveForm) == "" {
			return fmt.Errorf("%s.activeForm is required", prefix)
		}
		if !validTodoStatus(todo.Status) {
			return fmt.Errorf("%s.status must be one of pending, in_progress, or completed", prefix)
		}
		if todo.Status == "in_progress" {
			inProgress++
		}
	}
	if inProgress > 1 {
		return fmt.Errorf("only one todo can be in_progress at a time")
	}
	return nil
}

func callTodoWrite(ctx tool.Context, raw json.RawMessage, _ tool.ProgressSink) (contracts.ToolResult, error) {
	input, err := decodeTodoWrite(raw)
	if err != nil {
		return contracts.ToolResult{}, err
	}
	state, err := LoadState(ctx)
	if err != nil {
		return contracts.ToolResult{}, err
	}
	if state != nil {
		state.Set(input.Todos)
		if err := state.Save(); err != nil {
			return contracts.ToolResult{}, err
		}
	}
	storePath := ""
	persisted := false
	if state != nil {
		storePath = state.StorePath()
		persisted = storePath != ""
	}
	return contracts.ToolResult{
		Content: todoWriteSuccess,
		StructuredContent: map[string]any{
			"type":      "todo_list",
			"todos":     structuredTodos(input.Todos),
			"persisted": persisted,
			"storePath": storePath,
		},
	}, nil
}

func decodeTodoWrite(raw json.RawMessage) (todoWriteInput, error) {
	var obj map[string]json.RawMessage
	if err := json.Unmarshal(raw, &obj); err != nil {
		return todoWriteInput{}, err
	}
	for key := range obj {
		if key != "todos" {
			return todoWriteInput{}, fmt.Errorf("input.%s is not allowed", key)
		}
	}
	rawTodos, ok := obj["todos"]
	if !ok {
		return todoWriteInput{}, fmt.Errorf("input.todos is required")
	}
	var rawItems []json.RawMessage
	if err := json.Unmarshal(rawTodos, &rawItems); err != nil {
		return todoWriteInput{}, fmt.Errorf("input.todos must be array")
	}
	todos := make([]Todo, 0, len(rawItems))
	for i, rawItem := range rawItems {
		if err := validateTodoKeys(i, rawItem); err != nil {
			return todoWriteInput{}, err
		}
		var todo Todo
		if err := json.Unmarshal(rawItem, &todo); err != nil {
			return todoWriteInput{}, err
		}
		todos = append(todos, todo)
	}
	return todoWriteInput{Todos: todos}, nil
}

func validateTodoKeys(index int, raw json.RawMessage) error {
	var obj map[string]json.RawMessage
	if err := json.Unmarshal(raw, &obj); err != nil {
		return fmt.Errorf("todos[%d] must be object", index)
	}
	allowed := map[string]struct{}{
		"content":    {},
		"status":     {},
		"activeForm": {},
	}
	for key := range obj {
		if _, ok := allowed[key]; !ok {
			return fmt.Errorf("todos[%d].%s is not allowed", index, key)
		}
	}
	for _, key := range []string{"content", "status", "activeForm"} {
		if _, ok := obj[key]; !ok {
			return fmt.Errorf("todos[%d].%s is required", index, key)
		}
	}
	return nil
}

func validTodoStatus(status string) bool {
	switch status {
	case "pending", "in_progress", "completed":
		return true
	default:
		return false
	}
}

func structuredTodos(todos []Todo) []map[string]any {
	out := make([]map[string]any, 0, len(todos))
	for _, todo := range todos {
		out = append(out, map[string]any{
			"content":    todo.Content,
			"status":     todo.Status,
			"activeForm": todo.ActiveForm,
		})
	}
	return out
}
