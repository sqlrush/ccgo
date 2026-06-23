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
	closeOnce sync.Once
}

// closeEntry closes the done channel exactly once, safe to call from any goroutine.
func (e *asyncEntry) closeDone() {
	e.closeOnce.Do(func() { close(e.done) })
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
		defer entry.closeDone()
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

// Cancel cancels a registered async hook by id, signalling the done channel
// immediately (so any Wait caller no longer blocks on this entry). The
// underlying goroutine may still run to completion — we simply detach the
// entry from the registry and unblock any waiters.
// Returns true if the id was found and cancelled, false if unknown.
// CC ref: controlSchemas.ts:330-349 cancel_async_message (SDK-35).
func (r *AsyncHookRegistry) Cancel(id string) bool {
	r.mu.Lock()
	entry, ok := r.entries[id]
	if ok {
		delete(r.entries, id)
	}
	r.mu.Unlock()
	if ok {
		entry.closeDone()
		return true
	}
	return false
}

// Len returns the number of registered async hooks (including completed ones).
func (r *AsyncHookRegistry) Len() int {
	r.mu.Lock()
	defer r.mu.Unlock()
	return len(r.entries)
}
