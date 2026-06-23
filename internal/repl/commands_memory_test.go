package repl

import (
	"context"
	"fmt"
	"strings"
	"testing"

	"ccgo/internal/tui"
)

func TestMemoryHandlerNoArgOpensOverlay(t *testing.T) {
	files := []string{"/home/user/.claude/CLAUDE.md", "/project/CLAUDE.md"}
	h := memoryHandlerWith(func() ([]string, error) { return files, nil })
	screen := tui.NewREPLScreen(80, 24, nil)
	out, err := h(context.Background(), CommandContext{Args: "", Screen: &screen})
	if err != nil {
		t.Fatalf("handler error: %v", err)
	}
	if !out.Handled {
		t.Fatal("Handled must be true")
	}
	if out.Overlay == nil {
		t.Fatal("/memory must open MemorySelector overlay, got nil")
	}
}

func TestMemoryHandlerDiscoveryError(t *testing.T) {
	h := memoryHandlerWith(func() ([]string, error) { return nil, fmt.Errorf("disk error") })
	screen := tui.NewREPLScreen(80, 24, nil)
	out, err := h(context.Background(), CommandContext{Screen: &screen})
	if err != nil {
		t.Fatalf("handler must not propagate discover error: %v", err)
	}
	if !out.Handled {
		t.Fatal("Handled must be true on error")
	}
	if !strings.Contains(out.Status, "disk error") {
		t.Fatalf("Status must contain error, got: %q", out.Status)
	}
}

func TestMemoryHandlerNoFiles(t *testing.T) {
	h := memoryHandlerWith(func() ([]string, error) { return nil, nil })
	screen := tui.NewREPLScreen(80, 24, nil)
	out, err := h(context.Background(), CommandContext{Screen: &screen})
	if err != nil {
		t.Fatalf("handler error: %v", err)
	}
	if !out.Handled {
		t.Fatal("Handled must be true when no files found")
	}
	if out.Overlay != nil {
		t.Fatal("no overlay should be opened when there are no memory files")
	}
	if out.Status == "" {
		t.Fatal("Status must explain that no memory files were found")
	}
}
