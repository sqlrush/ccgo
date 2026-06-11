package powershelltools

import (
	"bytes"
	"sync"
	"time"

	"ccgo/internal/tool"
)

const MetadataBackgroundStateKey = "ccgo.tools.powershell.background_state"

type BackgroundState struct {
	mu    sync.RWMutex
	tasks map[string]*BackgroundTask
}

type BackgroundTask struct {
	ID          string
	Command     string
	Description string
	StartedAt   time.Time
	EndedAt     time.Time
	TimeoutMS   int
	Executable  string

	stdout lockedBuffer
	stderr lockedBuffer

	mu         sync.RWMutex
	Running    bool
	ExitCode   int
	TimedOut   bool
	Cancelled  bool
	DurationMS int64
	Error      string
	cancel     func()
}

type BackgroundTaskSnapshot struct {
	ID          string
	Command     string
	Description string
	StartedAt   time.Time
	EndedAt     time.Time
	TimeoutMS   int
	Executable  string
	Stdout      string
	Stderr      string
	Running     bool
	ExitCode    int
	TimedOut    bool
	Cancelled   bool
	DurationMS  int64
	Error       string
}

type lockedBuffer struct {
	mu  sync.RWMutex
	buf bytes.Buffer
}

func (b *lockedBuffer) Write(p []byte) (int, error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.buf.Write(p)
}

func (b *lockedBuffer) String() string {
	b.mu.RLock()
	defer b.mu.RUnlock()
	return b.buf.String()
}

func NewBackgroundState() *BackgroundState {
	return &BackgroundState{tasks: map[string]*BackgroundTask{}}
}

func (s *BackgroundState) Add(task *BackgroundTask) {
	if s == nil || task == nil {
		return
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.tasks == nil {
		s.tasks = map[string]*BackgroundTask{}
	}
	s.tasks[task.ID] = task
}

func (s *BackgroundState) Get(id string) (*BackgroundTask, bool) {
	if s == nil {
		return nil, false
	}
	s.mu.RLock()
	defer s.mu.RUnlock()
	task, ok := s.tasks[id]
	return task, ok
}

func (t *BackgroundTask) SetCancel(cancel func()) {
	if t == nil {
		return
	}
	t.mu.Lock()
	defer t.mu.Unlock()
	t.cancel = cancel
}

func (t *BackgroundTask) Cancel() bool {
	if t == nil {
		return false
	}
	t.mu.Lock()
	if !t.Running {
		t.mu.Unlock()
		return false
	}
	t.Cancelled = true
	cancel := t.cancel
	t.mu.Unlock()
	if cancel != nil {
		cancel()
	}
	return true
}

func (t *BackgroundTask) IsCancelled() bool {
	if t == nil {
		return false
	}
	t.mu.RLock()
	defer t.mu.RUnlock()
	return t.Cancelled
}

func (t *BackgroundTask) Finish(exitCode int, timedOut bool, durationMS int64, errText string, endedAt time.Time) {
	if t == nil {
		return
	}
	t.mu.Lock()
	defer t.mu.Unlock()
	t.Running = false
	t.ExitCode = exitCode
	t.TimedOut = timedOut
	t.DurationMS = durationMS
	t.Error = errText
	t.EndedAt = endedAt
	t.cancel = nil
}

func (t *BackgroundTask) Snapshot() BackgroundTaskSnapshot {
	if t == nil {
		return BackgroundTaskSnapshot{}
	}
	t.mu.RLock()
	defer t.mu.RUnlock()
	return BackgroundTaskSnapshot{
		ID:          t.ID,
		Command:     t.Command,
		Description: t.Description,
		StartedAt:   t.StartedAt,
		EndedAt:     t.EndedAt,
		TimeoutMS:   t.TimeoutMS,
		Executable:  t.Executable,
		Stdout:      t.stdout.String(),
		Stderr:      t.stderr.String(),
		Running:     t.Running,
		ExitCode:    t.ExitCode,
		TimedOut:    t.TimedOut,
		Cancelled:   t.Cancelled,
		DurationMS:  t.DurationMS,
		Error:       t.Error,
	}
}

func WithBackgroundState(ctx tool.Context, state *BackgroundState) tool.Context {
	if ctx.Metadata == nil {
		ctx.Metadata = map[string]any{}
	}
	if state == nil {
		state = NewBackgroundState()
	}
	ctx.Metadata[MetadataBackgroundStateKey] = state
	return ctx
}

func EnsureBackgroundState(ctx tool.Context) *BackgroundState {
	if ctx.Metadata == nil {
		return nil
	}
	if state, ok := ctx.Metadata[MetadataBackgroundStateKey].(*BackgroundState); ok && state != nil {
		return state
	}
	state := NewBackgroundState()
	ctx.Metadata[MetadataBackgroundStateKey] = state
	return state
}
