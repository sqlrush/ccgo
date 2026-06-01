package tool

import (
	"context"
	"encoding/json"
	"sync"

	"ccgo/internal/contracts"
)

type MessageUpdate struct {
	ToolUse contracts.ToolUse
	Result  contracts.ToolResult
	Err     error
	Done    bool
}

type RunOptions struct {
	MaxConcurrency int
}

func RunTools(ctx Context, executor Executor, uses []contracts.ToolUse, sink ProgressSink, options RunOptions) <-chan MessageUpdate {
	out := make(chan MessageUpdate)
	go func() {
		defer close(out)
		for _, batch := range partitionToolCalls(executor.Registry, uses) {
			if ctx.Context != nil {
				select {
				case <-ctx.Context.Done():
					return
				default:
				}
			}
			if !batch.concurrencySafe || len(batch.uses) == 1 {
				for _, use := range batch.uses {
					if ctx.Context != nil {
						select {
						case <-ctx.Context.Done():
							return
						default:
						}
					}
					result, err := executor.Execute(ctx, use, sink)
					sendUpdate(ctx.Context, out, MessageUpdate{ToolUse: use, Result: result, Err: err, Done: true})
				}
				continue
			}
			runConcurrentBatch(ctx, executor, batch.uses, sink, options, out)
		}
	}()
	return out
}

type toolBatch struct {
	concurrencySafe bool
	uses            []contracts.ToolUse
}

func partitionToolCalls(registry *Registry, uses []contracts.ToolUse) []toolBatch {
	var batches []toolBatch
	for _, use := range uses {
		safe := false
		if registry != nil {
			if t, ok := registry.Lookup(use.Name); ok {
				safe = t.IsConcurrencySafe(normalizeRawInput(use.Input))
			}
		}
		if len(batches) == 0 || batches[len(batches)-1].concurrencySafe != safe || !safe {
			batches = append(batches, toolBatch{concurrencySafe: safe})
		}
		batches[len(batches)-1].uses = append(batches[len(batches)-1].uses, use)
	}
	return batches
}

func runConcurrentBatch(ctx Context, executor Executor, uses []contracts.ToolUse, sink ProgressSink, options RunOptions, out chan<- MessageUpdate) {
	limit := options.MaxConcurrency
	if limit <= 0 {
		limit = len(uses)
	}
	sem := make(chan struct{}, limit)
	var wg sync.WaitGroup
	results := make([]MessageUpdate, len(uses))
	for i, use := range uses {
		i := i
		use := use
		if ctx.Context != nil {
			select {
			case <-ctx.Context.Done():
				return
			default:
			}
		}
		sem <- struct{}{}
		wg.Add(1)
		go func() {
			defer wg.Done()
			defer func() { <-sem }()
			result, err := executor.Execute(ctx, use, sink)
			results[i] = MessageUpdate{ToolUse: use, Result: result, Err: err, Done: true}
		}()
	}
	wg.Wait()
	for _, update := range results {
		sendUpdate(ctx.Context, out, update)
	}
}

func sendUpdate(ctx context.Context, out chan<- MessageUpdate, update MessageUpdate) {
	if ctx == nil {
		out <- update
		return
	}
	select {
	case <-ctx.Done():
	case out <- update:
	}
}

func ToolUseFromBlock(block contracts.ContentBlock) contracts.ToolUse {
	id := contracts.ID(block.ID)
	if id == "" {
		id = contracts.NewID()
	}
	input := block.Input
	if len(input) == 0 {
		input = json.RawMessage(`{}`)
	}
	return contracts.ToolUse{ID: id, Name: block.Name, Input: input}
}
