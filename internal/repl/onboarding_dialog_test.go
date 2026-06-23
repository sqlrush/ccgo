package repl

import (
	"strings"
	"testing"

	"ccgo/internal/tui"
)

func TestOnboardingDialogThemeSelects(t *testing.T) {
	d := NewOnboardingDialog([]string{"dark", "light"})
	// cursor 0 = "dark" — Enter should emit onboard:theme:dark and advance
	res, handled := d.ApplyKey(tui.Key{Type: tui.KeyEnter})
	if !handled {
		t.Fatal("Enter should be handled on theme step")
	}
	if res.Submit != "onboard:theme:dark" {
		t.Fatalf("theme step Enter = %q want onboard:theme:dark", res.Submit)
	}
	// After theme selection, step should be safety.
	if d.step != onboardingStepSafety {
		t.Fatalf("step after theme = %v want safety", d.step)
	}
}

func TestOnboardingDialogThemeNavigate(t *testing.T) {
	d := NewOnboardingDialog([]string{"dark", "light"})
	d.ApplyKey(tui.Key{Type: tui.KeyDown}) // cursor 0→1
	res, _ := d.ApplyKey(tui.Key{Type: tui.KeyEnter})
	if res.Submit != "onboard:theme:light" {
		t.Fatalf("navigate+Enter = %q want onboard:theme:light", res.Submit)
	}
}

func TestOnboardingDialogEscSkips(t *testing.T) {
	d := NewOnboardingDialog([]string{"dark"})
	res, handled := d.ApplyKey(tui.Key{Type: tui.KeyEsc})
	if !handled {
		t.Fatal("Esc should be handled")
	}
	if res.Submit != "onboard:skip" {
		t.Fatalf("Esc = %q want onboard:skip", res.Submit)
	}
}

func TestOnboardingDialogSafetyStep(t *testing.T) {
	d := NewOnboardingDialog([]string{"dark"})
	d.step = onboardingStepSafety
	res, handled := d.ApplyKey(tui.Key{Type: tui.KeyEnter})
	if !handled {
		t.Fatal("Enter should be handled on safety step")
	}
	if res.Submit != "onboard:safety:ack" {
		t.Fatalf("safety Enter = %q want onboard:safety:ack", res.Submit)
	}
	if d.step != onboardingStepLogin {
		t.Fatalf("step after safety = %v want login", d.step)
	}
}

func TestOnboardingDialogLoginBrowser(t *testing.T) {
	d := NewOnboardingDialog(nil)
	d.step = onboardingStepLogin
	// cursor 0 = browser
	res, _ := d.ApplyKey(tui.Key{Type: tui.KeyEnter})
	if res.Submit != "onboard:login:browser" {
		t.Fatalf("login browser = %q want onboard:login:browser", res.Submit)
	}
}

func TestOnboardingDialogLoginAPIKey(t *testing.T) {
	d := NewOnboardingDialog(nil)
	d.step = onboardingStepLogin
	d.cursor = 1 // API key
	res, _ := d.ApplyKey(tui.Key{Type: tui.KeyEnter})
	if res.Submit != "onboard:login:apikey" {
		t.Fatalf("login apikey = %q want onboard:login:apikey", res.Submit)
	}
}

func TestOnboardingDialogRenderThemeStep(t *testing.T) {
	d := NewOnboardingDialog([]string{"dark", "light"})
	out := strings.Join(d.Render(80, 24), "\n")
	if !strings.Contains(out, "dark") || !strings.Contains(out, "light") {
		t.Fatalf("Render theme step missing theme names: %q", out)
	}
	if !strings.Contains(out, "Step 1") {
		t.Fatalf("Render theme step missing step indicator: %q", out)
	}
}

func TestOnboardingDialogRenderSafetyStep(t *testing.T) {
	d := NewOnboardingDialog(nil)
	d.step = onboardingStepSafety
	out := strings.Join(d.Render(80, 24), "\n")
	if !strings.Contains(out, "Step 2") {
		t.Fatalf("Render safety step missing step indicator: %q", out)
	}
	if !strings.Contains(strings.ToLower(out), "safety") && !strings.Contains(strings.ToLower(out), "trust") && !strings.Contains(strings.ToLower(out), "approve") {
		t.Fatalf("Render safety step missing safety text: %q", out)
	}
}

func TestOnboardingDialogNoThemesSkipsThemeStep(t *testing.T) {
	d := NewOnboardingDialog(nil)
	// No themes: Enter advances to safety without emitting a theme submit.
	res, _ := d.ApplyKey(tui.Key{Type: tui.KeyEnter})
	if res.Submit != "" {
		// With no themes, the step advances silently; Submit should be empty.
		t.Fatalf("no-theme Enter should advance silently, got Submit=%q", res.Submit)
	}
	if d.step != onboardingStepSafety {
		t.Fatalf("step after no-theme Enter = %v want safety", d.step)
	}
}

func TestOnboardingDialogDefensiveCopy(t *testing.T) {
	themes := []string{"dark", "light"}
	d := NewOnboardingDialog(themes)
	themes[0] = "mutated"
	out := strings.Join(d.Render(80, 24), "\n")
	if strings.Contains(out, "mutated") {
		t.Fatal("onboarding dialog should not share backing slice with caller")
	}
}
