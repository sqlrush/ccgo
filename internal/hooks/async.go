package hooks

import (
	"context"
	"fmt"
	"sync"
	"sync/atomic"
)

// AsyncHookRegistry tracks hooks that were detached via {"async":true} output.
// It is goroutine-safe and -race clean.
// CC ref: src/utils/hooks.ts:184-264 (executeInBackground / registerPendingAsyncHook).
type AsyncHookRegistry struct {
	mu      sync.Mutex
	entries map[string]*asyncEntry
	counter atomic.Uint64
}

type asyncEntry struct {
	phase    string
	hookName string
	done     chan struct{}
}

// NewAsyncHookRegistry returns a new empty AsyncHookRegistry.
func NewAsyncHookRegistry() *AsyncHookRegistry {
	return &AsyncHookRegistry{entries: make(map[string]*asyncEntry)}
}

// Register spawns fn in a goroutine and records it in the registry.
// Returns a unique ID for the registered entry.
func (r *AsyncHookRegistry) Register(phase, hookName string, fn func()) string {
	id := fmt.Sprintf("async_%s_%d", phase, r.counter.Add(1))
	entry := &asyncEntry{
		phase:    phase,
		hookName: hookName,
		done:     make(chan struct{}),
	}
	r.mu.Lock()
	r.entries[id] = entry
	r.mu.Unlock()
	go func() {
		defer close(entry.done)
		fn()
	}()
	return id
}

// Wait blocks until all registered goroutines complete or the context is done.
func (r *AsyncHookRegistry) Wait(ctx context.Context) {
	r.mu.Lock()
	channels := make([]chan struct{}, 0, len(r.entries))
	for _, e := range r.entries {
		channels = append(channels, e.done)
	}
	r.mu.Unlock()
	for _, ch := range channels {
		select {
		case <-ch:
		case <-ctx.Done():
			return
		}
	}
}

// Len returns the number of registered async hooks (including completed ones).
func (r *AsyncHookRegistry) Len() int {
	r.mu.Lock()
	defer r.mu.Unlock()
	return len(r.entries)
}
