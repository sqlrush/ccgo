package repl

import (
	"context"
	"strings"
	"testing"

	"ccgo/internal/contracts"
	"ccgo/internal/tui"
)

func TestHelpHandlerOpensHelpScreenOverlay(t *testing.T) {
	cmds := []contracts.Command{
		{Name: "clear", Description: "Clear the conversation", Hidden: false},
		{Name: "vim", Description: "Toggle vim mode", Hidden: false},
		{Name: "internal", Description: "Internal command", Hidden: true},
	}
	h := helpHandlerWith(cmds)
	screen := tui.NewREPLScreen(80, 24, nil)
	out, err := h(context.Background(), CommandContext{Screen: &screen})
	if err != nil {
		t.Fatalf("handler error: %v", err)
	}
	if !out.Handled {
		t.Fatal("Handled must be true")
	}
	if out.Overlay == nil {
		t.Fatal("/help must open HelpScreen overlay, got nil Overlay")
	}
	// Hidden commands must be excluded: render and check "internal" is absent.
	lines := out.Overlay.Render(80, 30)
	rendered := strings.Join(lines, "\n")
	if strings.Contains(rendered, "internal") {
		t.Fatalf("HelpScreen must not show hidden commands; rendered: %q", rendered)
	}
	if !strings.Contains(rendered, "clear") {
		t.Fatalf("HelpScreen must show 'clear'; rendered: %q", rendered)
	}
}

func TestDoctorHandlerReturnsStatusText(t *testing.T) {
	called := false
	h := doctorHandlerWith(func() string {
		called = true
		return "Claude Code Doctor\n✓ API key: present"
	})
	screen := tui.NewREPLScreen(80, 24, nil)
	out, err := h(context.Background(), CommandContext{Screen: &screen})
	if err != nil {
		t.Fatalf("handler error: %v", err)
	}
	if !out.Handled {
		t.Fatal("Handled must be true")
	}
	if !called {
		t.Fatal("doctorRunner must be called")
	}
	if !strings.Contains(out.Status, "Doctor") {
		t.Fatalf("Status must contain doctor output; got: %q", out.Status)
	}
}
