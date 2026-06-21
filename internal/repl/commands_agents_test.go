package repl

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestAgentsHandlerCreateAndList(t *testing.T) {
	cwd := t.TempDir()
	h := agentsHandler(cwd)

	out, err := h(context.Background(), CommandContext{Args: "create reviewer", CWD: cwd})
	if err != nil || !out.Handled {
		t.Fatalf("create: %v %+v", err, out)
	}
	if _, statErr := os.Stat(filepath.Join(cwd, ".claude", "agents", "reviewer.md")); statErr != nil {
		t.Fatalf("agent file not created: %v", statErr)
	}
	out, err = h(context.Background(), CommandContext{Args: "", CWD: cwd})
	if err != nil {
		t.Fatalf("list err: %v", err)
	}
	if !strings.Contains(out.Status, "reviewer") {
		t.Fatalf("list missing reviewer: %q", out.Status)
	}
}

func TestAgentsHandlerShow(t *testing.T) {
	cwd := t.TempDir()
	h := agentsHandler(cwd)

	_, err := h(context.Background(), CommandContext{Args: "create myagent", CWD: cwd})
	if err != nil {
		t.Fatalf("create: %v", err)
	}

	out, err := h(context.Background(), CommandContext{Args: "show myagent", CWD: cwd})
	if err != nil {
		t.Fatalf("show err: %v", err)
	}
	if !out.Handled {
		t.Fatal("show: expected Handled=true")
	}
	if !strings.Contains(out.Status, "myagent") {
		t.Fatalf("show missing myagent in output: %q", out.Status)
	}
}

func TestAgentsHandlerDelete(t *testing.T) {
	cwd := t.TempDir()
	h := agentsHandler(cwd)

	_, err := h(context.Background(), CommandContext{Args: "create todel", CWD: cwd})
	if err != nil {
		t.Fatalf("create: %v", err)
	}

	out, err := h(context.Background(), CommandContext{Args: "delete todel", CWD: cwd})
	if err != nil {
		t.Fatalf("delete err: %v", err)
	}
	if !out.Handled {
		t.Fatal("delete: expected Handled=true")
	}

	if _, statErr := os.Stat(filepath.Join(cwd, ".claude", "agents", "todel.md")); !os.IsNotExist(statErr) {
		t.Fatalf("agent file should have been deleted: stat err=%v", statErr)
	}
}

func TestAgentsHandlerListEmpty(t *testing.T) {
	cwd := t.TempDir()
	h := agentsHandler(cwd)

	out, err := h(context.Background(), CommandContext{Args: "", CWD: cwd})
	if err != nil {
		t.Fatalf("list err: %v", err)
	}
	if !out.Handled {
		t.Fatal("list: expected Handled=true")
	}
	// Should contain some output, even if empty listing
	if out.Status == "" {
		t.Fatal("list: expected non-empty status")
	}
}
