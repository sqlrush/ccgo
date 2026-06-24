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

// TestDefaultIDEDetectCursorEnvVar verifies defaultIDEDetect detects Cursor via
// CURSOR_TRACE_ID environment variable (CMD-IDE-01 / G30).
func TestDefaultIDEDetectCursorEnvVar(t *testing.T) {
	t.Setenv("CURSOR_TRACE_ID", "trace-abc")
	t.Setenv("VSCODE_PID", "")
	t.Setenv("VSCODE_IPC_HOOK_CLI", "")
	t.Setenv("TERM_PROGRAM", "")
	t.Setenv("WINDSURF_TRACE_ID", "")
	t.Setenv("TERMINAL_EMULATOR", "")
	got := defaultIDEDetect()
	if !sliceContains(got, "Cursor") {
		t.Errorf("CURSOR_TRACE_ID set but 'Cursor' not in results: %v", got)
	}
}

// TestDefaultIDEDetectVSCodePID verifies VS Code detection via VSCODE_PID.
func TestDefaultIDEDetectVSCodePID(t *testing.T) {
	t.Setenv("CURSOR_TRACE_ID", "")
	t.Setenv("VSCODE_PID", "12345")
	t.Setenv("VSCODE_IPC_HOOK_CLI", "")
	t.Setenv("TERM_PROGRAM", "")
	t.Setenv("WINDSURF_TRACE_ID", "")
	t.Setenv("TERMINAL_EMULATOR", "")
	got := defaultIDEDetect()
	if !sliceContains(got, "VS Code") {
		t.Errorf("VSCODE_PID set but 'VS Code' not in results: %v", got)
	}
}

// TestDefaultIDEDetectVSCodeTermProgram verifies VS Code detection via TERM_PROGRAM=vscode.
func TestDefaultIDEDetectVSCodeTermProgram(t *testing.T) {
	t.Setenv("CURSOR_TRACE_ID", "")
	t.Setenv("VSCODE_PID", "")
	t.Setenv("VSCODE_IPC_HOOK_CLI", "")
	t.Setenv("TERM_PROGRAM", "vscode")
	t.Setenv("WINDSURF_TRACE_ID", "")
	t.Setenv("TERMINAL_EMULATOR", "")
	got := defaultIDEDetect()
	if !sliceContains(got, "VS Code") {
		t.Errorf("TERM_PROGRAM=vscode set but 'VS Code' not in results: %v", got)
	}
}

// TestDefaultIDEDetectJetBrains verifies JetBrains detection via TERMINAL_EMULATOR.
func TestDefaultIDEDetectJetBrains(t *testing.T) {
	t.Setenv("CURSOR_TRACE_ID", "")
	t.Setenv("VSCODE_PID", "")
	t.Setenv("VSCODE_IPC_HOOK_CLI", "")
	t.Setenv("TERM_PROGRAM", "")
	t.Setenv("WINDSURF_TRACE_ID", "")
	t.Setenv("TERMINAL_EMULATOR", "JetBrains-JediTerm")
	got := defaultIDEDetect()
	if !sliceContains(got, "JetBrains") {
		t.Errorf("TERMINAL_EMULATOR=JetBrains-JediTerm set but 'JetBrains' not in results: %v", got)
	}
}

// TestDefaultIDEDetectNoneDetected verifies defaultIDEDetect returns empty when
// no IDE env vars are set.
func TestDefaultIDEDetectNoneDetected(t *testing.T) {
	t.Setenv("CURSOR_TRACE_ID", "")
	t.Setenv("VSCODE_PID", "")
	t.Setenv("VSCODE_IPC_HOOK_CLI", "")
	t.Setenv("TERM_PROGRAM", "")
	t.Setenv("WINDSURF_TRACE_ID", "")
	t.Setenv("TERMINAL_EMULATOR", "")
	got := defaultIDEDetect()
	if len(got) != 0 {
		t.Errorf("no IDE env vars set but got %v", got)
	}
}

// TestDefaultIDEDetectCursorNotDuplicatedAsVSCode verifies that when Cursor is
// detected via CURSOR_TRACE_ID AND TERM_PROGRAM=vscode, only Cursor is reported.
func TestDefaultIDEDetectCursorNotDuplicatedAsVSCode(t *testing.T) {
	t.Setenv("CURSOR_TRACE_ID", "trace-xyz")
	t.Setenv("VSCODE_PID", "")
	t.Setenv("VSCODE_IPC_HOOK_CLI", "")
	t.Setenv("TERM_PROGRAM", "vscode")
	t.Setenv("WINDSURF_TRACE_ID", "")
	t.Setenv("TERMINAL_EMULATOR", "")
	got := defaultIDEDetect()
	if sliceContains(got, "VS Code") {
		t.Errorf("Cursor terminal should not also report 'VS Code': %v", got)
	}
	if !sliceContains(got, "Cursor") {
		t.Errorf("expected 'Cursor' in results: %v", got)
	}
}
