package repl

import (
	"testing"
)

// TestSetFileSuggestionCmd verifies the setter stores the command on the Loop.
// CFG-40: CC ref: utils/settings/types.ts fileSuggestion:{type:"command",command:string}.
func TestSetFileSuggestionCmd(t *testing.T) {
	term := NewFakeTerminal("", 80, 24)
	l := NewLoop(term, nil)
	if l.fileSuggestionCmd != "" {
		t.Fatalf("expected empty fileSuggestionCmd, got %q", l.fileSuggestionCmd)
	}
	l.SetFileSuggestionCmd("echo file1.go")
	if l.fileSuggestionCmd != "echo file1.go" {
		t.Errorf("expected fileSuggestionCmd=%q, got %q", "echo file1.go", l.fileSuggestionCmd)
	}
}

// TestFileSuggestionFiles_Echo verifies that the command output is parsed into paths.
func TestFileSuggestionFiles_Echo(t *testing.T) {
	term := NewFakeTerminal("", 80, 24)
	l := NewLoop(term, nil)
	// printf is available on macOS and Linux.
	l.SetFileSuggestionCmd("printf 'file1.go\\nfile2.go\\n'")

	paths := l.fileSuggestionFiles()
	if len(paths) != 2 {
		t.Fatalf("expected 2 paths, got %d: %v", len(paths), paths)
	}
	if paths[0] != "file1.go" {
		t.Errorf("expected paths[0]=file1.go, got %q", paths[0])
	}
	if paths[1] != "file2.go" {
		t.Errorf("expected paths[1]=file2.go, got %q", paths[1])
	}
}

// TestFileSuggestionFiles_Empty verifies that an empty command returns nil.
func TestFileSuggestionFiles_Empty(t *testing.T) {
	term := NewFakeTerminal("", 80, 24)
	l := NewLoop(term, nil)
	// No command set — should return nil.
	if paths := l.fileSuggestionFiles(); paths != nil {
		t.Errorf("expected nil paths for empty command, got %v", paths)
	}
}

// TestFileSuggestionFiles_FailFallback verifies that a failing command returns nil
// so the caller falls back to filesystem walking.
func TestFileSuggestionFiles_FailFallback(t *testing.T) {
	term := NewFakeTerminal("", 80, 24)
	l := NewLoop(term, nil)
	l.SetFileSuggestionCmd("exit 1") // always fails
	if paths := l.fileSuggestionFiles(); paths != nil {
		t.Errorf("expected nil on command failure, got %v", paths)
	}
}
