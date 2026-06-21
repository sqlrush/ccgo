package hooks

import (
	"encoding/json"
	"fmt"
	"strconv"
	"strings"
	"sync"

	"ccgo/internal/contracts"
	"ccgo/internal/tool"
)

// Resolution is the folded outcome of running all matched hooks for one event.
type Resolution struct {
	Block              bool
	Message            string
	AdditionalContext  []string
	PermissionDecision *contracts.PermissionDecision
	UpdatedInput       json.RawMessage
	Metadata           map[string]any
}

type hookOutcome struct {
	result tool.HookResult
	err    error
}

// Resolve runs every hook concurrently and folds the results with permission
// precedence deny > ask > allow (CC utils/hooks.ts:2820-2847), concatenated
// context, sticky Block, and deterministic (config-order) UpdatedInput/Metadata.
// It never mutates the input slice. The first hook error (by index) aborts with that error.
func Resolve(ctx tool.Context, hooks []tool.Hook, event tool.HookEvent) (Resolution, error) {
	if len(hooks) == 0 {
		return Resolution{}, nil
	}

	outcomes := make([]hookOutcome, len(hooks))
	var wg sync.WaitGroup
	wg.Add(len(hooks))
	for i := range hooks {
		go func(i int) {
			defer wg.Done()
			defer func() {
				if r := recover(); r != nil {
					outcomes[i] = hookOutcome{err: fmt.Errorf("hook %d panicked: %v", i, r)}
				}
			}()
			result, err := hooks[i].RunToolHook(ctx, event)
			outcomes[i] = hookOutcome{result: result, err: err}
		}(i)
	}
	wg.Wait()

	var res Resolution
	var behavior contracts.PermissionBehavior // "" until a hook sets one
	var decisionMessage string

	for i, oc := range outcomes {
		if oc.err != nil {
			return Resolution{}, oc.err
		}
		hr := oc.result

		if msg := strings.TrimSpace(hr.Message); msg != "" {
			res.AdditionalContext = append(res.AdditionalContext, msg)
		}
		if hr.Block {
			res.Block = true
		}
		if len(hr.UpdatedInput) > 0 && len(res.UpdatedInput) == 0 {
			res.UpdatedInput = hr.UpdatedInput
		}
		if len(hr.Metadata) > 0 {
			if res.Metadata == nil {
				res.Metadata = map[string]any{}
			}
			res.Metadata["hook_"+strconv.Itoa(i)] = hr.Metadata
		}
		if hr.PermissionDecision != nil {
			next := hr.PermissionDecision.Behavior
			behavior = foldBehavior(behavior, next)
			// First deny message wins (first-by-index), consistent with UpdatedInput.
			if next == contracts.PermissionDeny && strings.TrimSpace(hr.PermissionDecision.Message) != "" && decisionMessage == "" {
				decisionMessage = hr.PermissionDecision.Message
			} else if decisionMessage == "" && next != contracts.PermissionDeny {
				// If no deny yet, accept first non-empty message from other behaviors.
				if msg := strings.TrimSpace(hr.PermissionDecision.Message); msg != "" {
					decisionMessage = msg
				}
			}
		}
	}

	res.Message = strings.Join(res.AdditionalContext, "\n")
	if behavior != "" {
		res.PermissionDecision = &contracts.PermissionDecision{Behavior: behavior, Message: decisionMessage}
		if behavior == contracts.PermissionDeny {
			res.Block = true
		}
	}
	return res, nil
}

// Matches reports whether the hook's matcher accepts the given query string.
// A hook with no matcher (or "*") matches everything. Delegates to the same
// matchesPattern predicate used by CommandHook/HTTPHook at run time.
func Matches(hook tool.Hook, query string) bool {
	return matchesPattern(query, matcherOf(hook))
}

func matcherOf(hook tool.Hook) string {
	switch h := hook.(type) {
	case CommandHook:
		return h.Matcher
	case HTTPHook:
		return h.Matcher
	default:
		return ""
	}
}

// foldBehavior applies deny > ask > allow precedence (passthrough is a no-op).
// Matches CC utils/hooks.ts:2820-2847.
func foldBehavior(current, next contracts.PermissionBehavior) contracts.PermissionBehavior {
	switch next {
	case contracts.PermissionDeny:
		return contracts.PermissionDeny // deny always wins
	case contracts.PermissionAsk:
		if current != contracts.PermissionDeny {
			return contracts.PermissionAsk
		}
		return current
	case contracts.PermissionAllow:
		if current == "" {
			return contracts.PermissionAllow // only fills an empty slot
		}
		return current
	default:
		return current // passthrough / unknown: no change
	}
}
