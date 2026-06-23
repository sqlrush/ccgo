package filetools

import (
	"sync"

	"ccgo/internal/tool"
)

const MetadataReadStateKey = "ccgo.tools.file.read_state"

type ReadFileState struct {
	Content     string
	Timestamp   int64
	Offset      *int
	Limit       *int
	PartialView bool
}

type ReadState struct {
	mu    sync.RWMutex
	files map[string]ReadFileState
}

func NewReadState() *ReadState {
	return &ReadState{files: map[string]ReadFileState{}}
}

func (s *ReadState) Get(path string) (ReadFileState, bool) {
	if s == nil {
		return ReadFileState{}, false
	}
	s.mu.RLock()
	defer s.mu.RUnlock()
	state, ok := s.files[path]
	return state, ok
}

func (s *ReadState) Set(path string, state ReadFileState) {
	if s == nil {
		return
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.files == nil {
		s.files = map[string]ReadFileState{}
	}
	s.files[path] = state
}

// ReadStateEntry is a snapshot of a single file's tracked state, suitable for
// conversion to compact.ReadFileEntry.
type ReadStateEntry struct {
	Path      string
	Content   string
	Timestamp int64
}

// Paths returns the sorted list of all tracked file paths.
func (s *ReadState) Paths() []string {
	if s == nil {
		return nil
	}
	s.mu.RLock()
	defer s.mu.RUnlock()
	paths := make([]string, 0, len(s.files))
	for p := range s.files {
		paths = append(paths, p)
	}
	return paths
}

// Entries returns a snapshot of all tracked files as ReadStateEntry values.
// The slice is safe to hold without locking after the call returns.
func (s *ReadState) Entries() []ReadStateEntry {
	if s == nil {
		return nil
	}
	s.mu.RLock()
	defer s.mu.RUnlock()
	entries := make([]ReadStateEntry, 0, len(s.files))
	for path, state := range s.files {
		entries = append(entries, ReadStateEntry{
			Path:      path,
			Content:   state.Content,
			Timestamp: state.Timestamp,
		})
	}
	return entries
}

func WithReadState(ctx tool.Context, state *ReadState) tool.Context {
	if ctx.Metadata == nil {
		ctx.Metadata = map[string]any{}
	}
	if state == nil {
		state = NewReadState()
	}
	ctx.Metadata[MetadataReadStateKey] = state
	return ctx
}

func EnsureReadState(ctx tool.Context) *ReadState {
	if ctx.Metadata == nil {
		return nil
	}
	if state, ok := ctx.Metadata[MetadataReadStateKey].(*ReadState); ok && state != nil {
		return state
	}
	state := NewReadState()
	ctx.Metadata[MetadataReadStateKey] = state
	return state
}
