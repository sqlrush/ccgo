package repl

import (
	"context"
	"strings"
	"testing"
)

func TestIDEHandlerNoIDEDetected(t *testing.T) {
	detect := func() []string { return nil }
	h := ideHandler(detect)
	out, err := h(context.Background(), CommandContext{Args: ""})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !out.Handled {
		t.Fatal("must be handled")
	}
	if !strings.Contains(out.Status, "No IDE detected") {
		t.Fatalf("expected 'No IDE detected' in status, got: %q", out.Status)
	}
}

func TestIDEHandlerListDetected(t *testing.T) {
	detect := func() []string { return []string{"VS Code", "Cursor"} }
	h := ideHandler(detect)
	out, err := h(context.Background(), CommandContext{Args: ""})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !out.Handled {
		t.Fatal("must be handled")
	}
	if !strings.Contains(out.Status, "VS Code") || !strings.Contains(out.Status, "Cursor") {
		t.Fatalf("expected IDE names in status, got: %q", out.Status)
	}
}

func TestIDEHandlerListSubcommand(t *testing.T) {
	detect := func() []string { return []string{"JetBrains"} }
	h := ideHandler(detect)
	out, err := h(context.Background(), CommandContext{Args: "list"})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !out.Handled {
		t.Fatal("must be handled")
	}
	if !strings.Contains(out.Status, "JetBrains") {
		t.Fatalf("expected 'JetBrains' in status, got: %q", out.Status)
	}
}

func TestIDEHandlerOpenNoIDE(t *testing.T) {
	detect := func() []string { return nil }
	h := ideHandler(detect)
	out, err := h(context.Background(), CommandContext{Args: "open"})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !out.Handled {
		t.Fatal("must be handled")
	}
	if !strings.Contains(out.Status, "No IDE detected") {
		t.Fatalf("expected 'No IDE detected' in open status, got: %q", out.Status)
	}
}

func TestIDEHandlerOpenWithIDE(t *testing.T) {
	detect := func() []string { return []string{"VS Code"} }
	h := ideHandler(detect)
	out, err := h(context.Background(), CommandContext{Args: "open"})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !out.Handled {
		t.Fatal("must be handled")
	}
	// Should mention the IDE and explain the extension is out of scope.
	if !strings.Contains(out.Status, "VS Code") {
		t.Fatalf("expected 'VS Code' in open status, got: %q", out.Status)
	}
}

func TestIDEHandlerNilDetectUsesDefault(t *testing.T) {
	h := ideHandler(nil)
	out, err := h(context.Background(), CommandContext{})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !out.Handled {
		t.Fatal("must be handled")
	}
	// Default detect returns nothing; should report no IDE detected.
	if !strings.Contains(out.Status, "No IDE detected") {
		t.Fatalf("expected 'No IDE detected' with nil detect, got: %q", out.Status)
	}
}
