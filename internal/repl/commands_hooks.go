package repl

import (
	"context"
	"fmt"
	"sort"
	"strings"

	"ccgo/internal/contracts"
	"ccgo/internal/hooks"
	"ccgo/internal/tool"
)

// hooksHandler returns a CommandHandler for /hooks.
// It is VIEW-ONLY: reads contracts.Settings.Hooks and summarises configured
// hooks per event (phase). No writes are performed.
//
// Usage: /hooks
func hooksHandler(settings func() contracts.Settings) CommandHandler {
	return func(ctx context.Context, cc CommandContext) (CommandOutcome, error) {
		s := settings()
		return CommandOutcome{
			Handled: true,
			Status:  formatHooksSummary(s),
		}, nil
	}
}

// formatHooksSummary renders a human-readable summary of the hooks configured
// in the merged settings. Uses hooks.FromRaw to parse and normalise the raw
// hooks map so the summary reflects what will actually run.
func formatHooksSummary(s contracts.Settings) string {
	if s.DisableAllHooks != nil && *s.DisableAllHooks {
		return "Hooks are disabled (disableAllHooks=true)."
	}
	if len(s.Hooks) == 0 {
		return "No hooks configured."
	}

	parsed := hooks.FromRaw(s.Hooks, hooks.Options{
		AllowedHTTPHookURLs:    s.AllowedHTTPHookURLs,
		HTTPHookAllowedEnvVars: s.HTTPHookAllowedEnvVars,
	})
	if len(parsed) == 0 {
		return "No hooks configured."
	}

	// Group hooks by phase (event).
	type entry struct {
		kind    string
		matcher string
		detail  string
	}
	byPhase := map[string][]entry{}
	for _, h := range parsed {
		// Only PhaseHook implementations expose phases; skip others.
		ph, ok := h.(tool.PhaseHook)
		if !ok {
			continue
		}
		phases := ph.HookPhases()
		for _, phase := range phases {
			switch typed := h.(type) {
			case hooks.CommandHook:
				byPhase[phase] = append(byPhase[phase], entry{
					kind:    "command",
					matcher: typed.Matcher,
					detail:  typed.Command,
				})
			case hooks.HTTPHook:
				byPhase[phase] = append(byPhase[phase], entry{
					kind:    "http",
					matcher: typed.Matcher,
					detail:  typed.URL,
				})
			default:
				byPhase[phase] = append(byPhase[phase], entry{
					kind:    "unknown",
					matcher: "",
					detail:  fmt.Sprintf("%T", h),
				})
			}
		}
	}

	if len(byPhase) == 0 {
		return "No hooks configured."
	}

	phases := make([]string, 0, len(byPhase))
	for phase := range byPhase {
		phases = append(phases, phase)
	}
	sort.Strings(phases)

	var sb strings.Builder
	sb.WriteString("Configured hooks:\n")
	for _, phase := range phases {
		entries := byPhase[phase]
		sb.WriteString(fmt.Sprintf("\n%s:\n", phase))
		for _, e := range entries {
			matcher := e.matcher
			if matcher == "" {
				matcher = "*"
			}
			sb.WriteString(fmt.Sprintf("  [%s] matcher=%s  %s\n", e.kind, matcher, e.detail))
		}
	}
	return strings.TrimRight(sb.String(), "\n")
}
