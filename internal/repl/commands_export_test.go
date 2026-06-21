package repl

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"ccgo/internal/contracts"
)

func TestExportHandlerWritesFile(t *testing.T) {
	cwd := t.TempDir()
	history := []contracts.Message{
		{Type: contracts.MessageUser, Content: []contracts.ContentBlock{contracts.NewTextBlock("hello")}},
		{Type: contracts.MessageAssistant, Content: []contracts.ContentBlock{contracts.NewTextBlock("hi there")}},
	}
	h := exportHandler(cwd)
	out, err := h(context.Background(), CommandContext{Args: "convo", CWD: cwd, History: history})
	if err != nil || !out.Handled {
		t.Fatalf("export: %v %+v", err, out)
	}
	path := filepath.Join(cwd, "convo.txt")
	raw, statErr := os.ReadFile(path)
	if statErr != nil {
		t.Fatalf("export file missing: %v", statErr)
	}
	body := string(raw)
	if !strings.Contains(body, "hello") || !strings.Contains(body, "hi there") {
		t.Fatalf("export body incomplete: %q", body)
	}
}

func TestExportHandlerDefaultFilename(t *testing.T) {
	cwd := t.TempDir()
	h := exportHandler(cwd)
	out, err := h(context.Background(), CommandContext{Args: "", CWD: cwd, History: nil})
	if err != nil || !out.Handled {
		t.Fatalf("export default: %v %+v", err, out)
	}
	if !strings.Contains(out.Status, ".txt") {
		t.Fatalf("expected a filename in status: %q", out.Status)
	}
}

func TestExportHandlerTraversalRejected(t *testing.T) {
	cwd := t.TempDir()
	h := exportHandler(cwd)
	for _, bad := range []string{"../escape", "../../etc/passwd", "/absolute"} {
		out, err := h(context.Background(), CommandContext{Args: bad, CWD: cwd})
		if err != nil {
			t.Fatalf("traversal %q: unexpected error: %v", bad, err)
		}
		if !out.Handled {
			t.Fatalf("traversal %q: should be handled (rejected)", bad)
		}
		// Should not write any file outside cwd; status must signal rejection
		if !strings.Contains(out.Status, "invalid") && !strings.Contains(out.Status, "Invalid") {
			t.Fatalf("traversal %q: expected rejection status, got %q", bad, out.Status)
		}
	}
}

func TestExportHandlerFileContainsRoleLabels(t *testing.T) {
	cwd := t.TempDir()
	history := []contracts.Message{
		{Type: contracts.MessageUser, Content: []contracts.ContentBlock{contracts.NewTextBlock("question")}},
		{Type: contracts.MessageAssistant, Content: []contracts.ContentBlock{contracts.NewTextBlock("answer")}},
	}
	h := exportHandler(cwd)
	_, err := h(context.Background(), CommandContext{Args: "chat", CWD: cwd, History: history})
	if err != nil {
		t.Fatalf("export: %v", err)
	}
	raw, _ := os.ReadFile(filepath.Join(cwd, "chat.txt"))
	body := string(raw)
	if !strings.Contains(body, "User:") || !strings.Contains(body, "Assistant:") {
		t.Fatalf("export body missing role labels: %q", body)
	}
}
