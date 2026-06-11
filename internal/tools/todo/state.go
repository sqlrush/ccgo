package todotools

import (
	"sync"

	"ccgo/internal/tool"
)

const MetadataTodoStateKey = "ccgo.tools.todo.state"

type Todo struct {
	ID       string `json:"id"`
	Content  string `json:"content"`
	Status   string `json:"status"`
	Priority string `json:"priority"`
}

type State struct {
	mu    sync.RWMutex
	todos []Todo
}

func NewState() *State {
	return &State{}
}

func (s *State) Snapshot() []Todo {
	if s == nil {
		return nil
	}
	s.mu.RLock()
	defer s.mu.RUnlock()
	return append([]Todo(nil), s.todos...)
}

func (s *State) Set(todos []Todo) {
	if s == nil {
		return
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	s.todos = append([]Todo(nil), todos...)
}

func WithState(ctx tool.Context, state *State) tool.Context {
	if ctx.Metadata == nil {
		ctx.Metadata = map[string]any{}
	}
	if state == nil {
		state = NewState()
	}
	ctx.Metadata[MetadataTodoStateKey] = state
	return ctx
}

func EnsureState(ctx tool.Context) *State {
	if ctx.Metadata == nil {
		return nil
	}
	if state, ok := ctx.Metadata[MetadataTodoStateKey].(*State); ok && state != nil {
		return state
	}
	state := NewState()
	ctx.Metadata[MetadataTodoStateKey] = state
	return state
}
