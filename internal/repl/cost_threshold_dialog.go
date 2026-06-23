package repl

import (
	"fmt"

	"ccgo/internal/tui"
)

// costThresholdUSD is the spend level (matching CC's $5 threshold) at which the
// CostThresholdDialog is displayed. Callers compare against SessionStats.CostUSD.
const costThresholdUSD = 5.0

// CostThresholdDialog is shown once when the session's cumulative API spend
// crosses costThresholdUSD (OVL-33). CC shows a single "Got it, thanks!" option.
// Submit value: "cost:ack" — the loop handles it via handleOverlaySubmit.
type CostThresholdDialog struct {
	spentUSD float64
	dismissed bool
}

// NewCostThresholdDialog constructs the cost-threshold overlay. spentUSD is
// included in the dialog text so the user sees the current spend.
func NewCostThresholdDialog(spentUSD float64) *CostThresholdDialog {
	return &CostThresholdDialog{spentUSD: spentUSD}
}

func (d *CostThresholdDialog) ApplyKey(key tui.Key) (OverlayResult, bool) {
	switch key.Type {
	case tui.KeyEsc, tui.KeyEnter:
		return OverlayResult{Submit: "cost:ack"}, true
	default:
		return OverlayResult{}, false
	}
}

func (d *CostThresholdDialog) Render(width, _ int) []string {
	return []string{
		fmt.Sprintf("You've spent $%.2f on the Anthropic API this session.", d.spentUSD),
		"",
		"Learn more about how to monitor your spending:",
		"  https://code.claude.com/docs/en/costs",
		"",
		"[Got it, thanks!]",
	}
}

// TokenWarningThreshold is the fraction of context window at which
// TokenWarningOverlay should be displayed (matching CC's 200k token check).
// At ≥ tokenWarningFraction the status bar or overlay shows a warning.
const tokenWarningFraction = 0.80 // warn at 80% of context limit

// TokenWarningOverlay is an informational banner shown (as a single-line status
// suffix or a dismissible overlay) when the conversation is approaching the
// context window limit (OVL-38). It does not block user input.
//
// Design note: CC renders this inline in the status bar / scroll region; ccgo
// surfaces it as a dismissible overlay so it is visible without a TUI render
// seam. Submit value: "tokenwarn:ack".
type TokenWarningOverlay struct {
	usedTokens int
	maxTokens  int
}

// NewTokenWarningOverlay constructs the token-warning overlay.
func NewTokenWarningOverlay(usedTokens, maxTokens int) *TokenWarningOverlay {
	return &TokenWarningOverlay{usedTokens: usedTokens, maxTokens: maxTokens}
}

func (o *TokenWarningOverlay) ApplyKey(key tui.Key) (OverlayResult, bool) {
	switch key.Type {
	case tui.KeyEsc, tui.KeyEnter:
		return OverlayResult{Submit: "tokenwarn:ack"}, true
	default:
		return OverlayResult{}, false
	}
}

func (o *TokenWarningOverlay) Render(width, _ int) []string {
	pct := 0
	if o.maxTokens > 0 {
		pct = o.usedTokens * 100 / o.maxTokens
	}
	return []string{
		fmt.Sprintf("Context window is %d%% full (%d / %d tokens).", pct, o.usedTokens, o.maxTokens),
		"When the context window is full, Claude Code will automatically compact",
		"the conversation. You can also run /compact now to compress the context.",
		"",
		"[OK]",
	}
}

// AutoCompactWarningOverlay is shown (OVL-39) when context compaction is
// imminent — the conversation is very close to the limit and the automatic
// compaction countdown has begun. Submit value: "compact:ack".
type AutoCompactWarningOverlay struct {
	usedTokens int
	maxTokens  int
}

// NewAutoCompactWarningOverlay constructs the auto-compaction warning overlay.
func NewAutoCompactWarningOverlay(usedTokens, maxTokens int) *AutoCompactWarningOverlay {
	return &AutoCompactWarningOverlay{usedTokens: usedTokens, maxTokens: maxTokens}
}

func (o *AutoCompactWarningOverlay) ApplyKey(key tui.Key) (OverlayResult, bool) {
	switch key.Type {
	case tui.KeyEsc, tui.KeyEnter:
		return OverlayResult{Submit: "compact:ack"}, true
	default:
		return OverlayResult{}, false
	}
}

func (o *AutoCompactWarningOverlay) Render(width, _ int) []string {
	pct := 0
	if o.maxTokens > 0 {
		pct = o.usedTokens * 100 / o.maxTokens
	}
	return []string{
		fmt.Sprintf("Context window is %d%% full — auto-compaction will begin soon.", pct),
		"Conversation history will be summarised to free up space.",
		"Run /compact now to control when this happens.",
		"",
		"[OK]",
	}
}
