package conversation

import (
	compactpkg "ccgo/internal/compact"
	"ccgo/internal/contracts"
)

// maybeMicroCompact runs deterministic micro-compaction over history before the
// model turn (mirrors CC: microcompact runs before autocompact every turn).
// It is LLM-free and never mutates the input slice.
//
// Contract: MicroCompact returns a MicroResult{Summary, MessagesSummarized,
// MessagesKept} where Summary is a plain-text digest of the summarized prefix.
// When MessagesSummarized > 0 we feed that summary into BuildPlan, which
// produces Plan.Output — the new history as [boundary, summary, ...kept].
// This matches how auto-compact applies its result (run.go:189 uses
// compactResult.Plan.Boundary / compactResult.Plan.Summary / Plan.Output).
//
// Returns (history, nil, false) when disabled or nothing to compact.
func (r Runner) maybeMicroCompact(history []contracts.Message) ([]contracts.Message, *compactpkg.MicroResult, bool) {
	if !r.EnableMicroCompact || len(history) == 0 {
		return history, nil, false
	}

	options := compactpkg.MicroOptions{
		KeepLast: r.MicroCompactKeepLast,
		MaxChars: r.MicroCompactMaxChars,
		CacheDir: r.MicroCompactDir,
	}
	result := compactpkg.MicroCompact(history, options)
	if result.MessagesSummarized == 0 {
		return history, nil, false
	}

	// Apply the micro summary via BuildPlan so the new history has the same
	// boundary+summary+kept shape that auto-compact produces.
	plan := compactpkg.BuildPlan(history, compactpkg.PlanOptions{
		Trigger:        compactpkg.TriggerSnip,
		Summary:        result.Summary,
		KeepLast:       result.MessagesKept,
		PreserveRecent: true,
	})

	return plan.Output, &result, true
}
