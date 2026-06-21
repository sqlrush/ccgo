package todotools

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"

	"ccgo/internal/platform"
	"ccgo/internal/tool"
)

const MetadataTodoStateKey = "ccgo.tools.todo.state"
const MetadataTodoStorePathKey = "ccgo.tools.todo.store_path"

type Todo struct {
	Content    string `json:"content"`
	Status     string `json:"status"`
	ActiveForm string `json:"activeForm"`
}

type State struct {
	mu        sync.RWMutex
	todos     []Todo
	storePath string
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

func (s *State) StorePath() string {
	if s == nil {
		return ""
	}
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.storePath
}

func (s *State) setStorePath(path string) {
	if s == nil {
		return
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	s.storePath = path
}

func (s *State) Save() error {
	if s == nil {
		return nil
	}
	s.mu.RLock()
	path := s.storePath
	todos := append([]Todo(nil), s.todos...)
	s.mu.RUnlock()
	if strings.TrimSpace(path) == "" {
		return nil
	}
	data, err := json.MarshalIndent(todoStoreFile{Todos: todos}, "", "  ")
	if err != nil {
		return err
	}
	data = append(data, '\n')
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return err
	}
	return platform.AtomicWriteFile(path, data, 0o600)
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

func WithStorePath(ctx tool.Context, path string) tool.Context {
	if ctx.Metadata == nil {
		ctx.Metadata = map[string]any{}
	}
	ctx.Metadata[MetadataTodoStorePathKey] = path
	if state, ok := ctx.Metadata[MetadataTodoStateKey].(*State); ok && state != nil {
		state.setStorePath(path)
	}
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

func LoadState(ctx tool.Context) (*State, error) {
	state := (*State)(nil)
	if ctx.Metadata != nil {
		state = EnsureState(ctx)
	}
	if state == nil {
		state = NewState()
	}
	path := TodoStorePath(ctx)
	if path == "" {
		return state, nil
	}
	state.setStorePath(path)
	todos, err := LoadTodos(path)
	if err != nil {
		return nil, err
	}
	if todos != nil {
		state.Set(todos)
	}
	return state, nil
}

func TodoStorePath(ctx tool.Context) string {
	if ctx.Metadata != nil {
		if path, ok := ctx.Metadata[MetadataTodoStorePathKey].(string); ok && strings.TrimSpace(path) != "" {
			return filepath.Clean(path)
		}
	}
	if strings.TrimSpace(ctx.WorkingDirectory) == "" || ctx.SessionID == "" {
		return ""
	}
	return filepath.Join(ctx.WorkingDirectory, ".claude", "todos", sanitizeTodoStoreName(string(ctx.SessionID))+".json")
}

func sanitizeTodoStoreName(name string) string {
	name = strings.TrimSpace(name)
	if name == "" {
		return "session"
	}
	return strings.Map(func(r rune) rune {
		switch r {
		case '/', '\\', ':', 0:
			return '_'
		default:
			return r
		}
	}, name)
}

type todoStoreFile struct {
	Todos []Todo `json:"todos"`
}

func LoadTodos(path string) ([]Todo, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return nil, nil
		}
		return nil, err
	}
	var store todoStoreFile
	if err := json.Unmarshal(data, &store); err != nil {
		return nil, fmt.Errorf("load todo state: %w", err)
	}
	if err := validateTodos(store.Todos); err != nil {
		return nil, fmt.Errorf("load todo state: %w", err)
	}
	return append([]Todo(nil), store.Todos...), nil
}
