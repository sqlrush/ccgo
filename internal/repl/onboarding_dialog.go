package repl

import (
	"ccgo/internal/tui"
)

// onboardingStep is an enumeration of multi-step onboarding pages (OVL-16).
type onboardingStep int

const (
	onboardingStepTheme   onboardingStep = iota // page 1: pick a theme
	onboardingStepSafety                        // page 2: safety / folder trust notice
	onboardingStepLogin                         // page 3: API key / login prompt
	onboardingStepDone                          // sentinel
)

// OnboardingDialog is the first-run multi-step wizard shown when no API key is
// configured and no prior session exists (OVL-16).
//
// CC flow (Onboarding.tsx):
//   1. Theme selection
//   2. Safety notice (brief)
//   3. API-key / OAuth login
//
// Submit protocol:
//   "onboard:theme:<name>"  — theme selected; step advances to safety
//   "onboard:safety:ack"    — safety notice dismissed; step advances to login
//   "onboard:login:apikey"  — user chose API-key entry path
//   "onboard:login:browser" — user chose OAuth/browser path
//   "onboard:skip"          — Esc at any step cancels onboarding
type OnboardingDialog struct {
	step   onboardingStep
	themes []string
	cursor int
}

// NewOnboardingDialog constructs the first-run wizard. themes is the list of
// available theme names; if empty, the theme step is skipped.
func NewOnboardingDialog(themes []string) *OnboardingDialog {
	copied := make([]string, len(themes))
	copy(copied, themes)
	return &OnboardingDialog{themes: copied}
}

func (d *OnboardingDialog) ApplyKey(key tui.Key) (OverlayResult, bool) {
	switch d.step {
	case onboardingStepTheme:
		return d.applyThemeKey(key)
	case onboardingStepSafety:
		return d.applySafetyKey(key)
	case onboardingStepLogin:
		return d.applyLoginKey(key)
	}
	return OverlayResult{Dismissed: true}, true
}

func (d *OnboardingDialog) applyThemeKey(key tui.Key) (OverlayResult, bool) {
	n := len(d.themes)
	switch key.Type {
	case tui.KeyEsc:
		return OverlayResult{Submit: "onboard:skip"}, true
	case tui.KeyUp:
		if n > 0 && d.cursor > 0 {
			d.cursor--
		}
		return OverlayResult{}, true
	case tui.KeyDown:
		if n > 0 && d.cursor < n-1 {
			d.cursor++
		}
		return OverlayResult{}, true
	case tui.KeyEnter:
		if n == 0 {
			// No themes to pick — advance silently.
			d.step = onboardingStepSafety
			d.cursor = 0
			return OverlayResult{}, true
		}
		name := d.themes[d.cursor]
		d.step = onboardingStepSafety
		d.cursor = 0
		return OverlayResult{Submit: "onboard:theme:" + name}, true
	default:
		return OverlayResult{}, false
	}
}

func (d *OnboardingDialog) applySafetyKey(key tui.Key) (OverlayResult, bool) {
	switch key.Type {
	case tui.KeyEsc:
		return OverlayResult{Submit: "onboard:skip"}, true
	case tui.KeyEnter:
		d.step = onboardingStepLogin
		d.cursor = 0
		return OverlayResult{Submit: "onboard:safety:ack"}, true
	default:
		return OverlayResult{}, false
	}
}

func (d *OnboardingDialog) applyLoginKey(key tui.Key) (OverlayResult, bool) {
	switch key.Type {
	case tui.KeyEsc:
		return OverlayResult{Submit: "onboard:skip"}, true
	case tui.KeyUp:
		if d.cursor > 0 {
			d.cursor--
		}
		return OverlayResult{}, true
	case tui.KeyDown:
		if d.cursor < 1 {
			d.cursor++
		}
		return OverlayResult{}, true
	case tui.KeyEnter:
		if d.cursor == 0 {
			return OverlayResult{Submit: "onboard:login:browser"}, true
		}
		return OverlayResult{Submit: "onboard:login:apikey"}, true
	default:
		return OverlayResult{}, false
	}
}

func (d *OnboardingDialog) Render(width, height int) []string {
	switch d.step {
	case onboardingStepTheme:
		return d.renderTheme(width, height)
	case onboardingStepSafety:
		return d.renderSafety(width)
	case onboardingStepLogin:
		return d.renderLogin(width)
	}
	return []string{"Onboarding complete."}
}

func (d *OnboardingDialog) renderTheme(width, height int) []string {
	lines := []string{
		"Welcome to Claude Code!",
		"",
		"Step 1 of 3 — Choose a theme:",
		"",
	}
	max := height - len(lines) - 2
	if max < 1 {
		max = 1
	}
	for i, t := range d.themes {
		if i >= max {
			break
		}
		marker := "  "
		if i == d.cursor {
			marker = "> "
		}
		lines = append(lines, truncateToWidth(marker+t, width))
	}
	if len(d.themes) == 0 {
		lines = append(lines, "  (no themes available — press Enter to continue)")
	}
	return lines
}

func (d *OnboardingDialog) renderSafety(width int) []string {
	msg := "Claude Code can run commands, read and write files, and access external services. " +
		"Only run Claude Code in directories you trust. " +
		"You will be asked to approve sensitive operations."
	lines := []string{
		"Welcome to Claude Code!",
		"",
		"Step 2 of 3 — Safety notice:",
		"",
	}
	lines = append(lines, wordWrap(msg, width)...)
	lines = append(lines, "", "[Press Enter to continue]")
	return lines
}

func (d *OnboardingDialog) renderLogin(width int) []string {
	loginOpts := []string{
		"Login with browser (OAuth)",
		"Enter API key manually",
	}
	lines := []string{
		"Welcome to Claude Code!",
		"",
		"Step 3 of 3 — Sign in:",
		"",
	}
	for i, opt := range loginOpts {
		marker := "  "
		if i == d.cursor {
			marker = "> "
		}
		lines = append(lines, marker+opt)
	}
	return lines
}
