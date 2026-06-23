package main

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// TestCompletionOutputFlag verifies SUBCMD-COMPLETION-06: --output <file> writes the
// completion script to a file instead of stdout.
func TestCompletionOutputFlag(t *testing.T) {
	outFile := filepath.Join(t.TempDir(), "claude.bash")

	var stdout, stderr bytes.Buffer
	code := runCompletionCLI([]string{"bash", "--output", outFile}, &stdout, &stderr)
	if code != 0 {
		t.Fatalf("expected exit 0, got %d; stderr=%s", code, stderr.String())
	}

	// stdout should be empty when --output is used.
	if stdout.Len() > 0 {
		t.Fatalf("stdout should be empty when --output is given; got %q", stdout.String())
	}

	// The file should contain the bash completion script.
	data, err := os.ReadFile(outFile)
	if err != nil {
		t.Fatalf("output file not created: %v", err)
	}
	if !strings.Contains(string(data), "complete -F _claude_completions claude") {
		t.Fatalf("output file should contain bash completion; got %q", string(data))
	}
}

// TestCompletionOutputFlagZsh verifies --output works for zsh too.
func TestCompletionOutputFlagZsh(t *testing.T) {
	outFile := filepath.Join(t.TempDir(), "claude.zsh")

	var stdout, stderr bytes.Buffer
	code := runCompletionCLI([]string{"zsh", "--output", outFile}, &stdout, &stderr)
	if code != 0 {
		t.Fatalf("expected exit 0, got %d; stderr=%s", code, stderr.String())
	}

	if stdout.Len() > 0 {
		t.Fatalf("stdout should be empty with --output; got %q", stdout.String())
	}

	data, err := os.ReadFile(outFile)
	if err != nil {
		t.Fatalf("output file not created: %v", err)
	}
	if !strings.Contains(string(data), "#compdef claude") {
		t.Fatalf("output file should contain zsh completion; got %q", string(data))
	}
}

// TestCompletionOutputFlagBadPath verifies --output with an unwritable path returns 1.
func TestCompletionOutputFlagBadPath(t *testing.T) {
	var stdout, stderr bytes.Buffer
	code := runCompletionCLI([]string{"bash", "--output", "/nonexistent/dir/claude.bash"}, &stdout, &stderr)
	if code == 0 {
		t.Fatal("expected non-zero exit for bad output path")
	}
}
