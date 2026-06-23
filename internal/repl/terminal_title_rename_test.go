package repl

import (
	"context"
	"strings"
	"testing"

	"ccgo/internal/contracts"
)

// TestRenameHandlerSetsTerminalTitleWhenEnabled verifies that renameHandlerWithTitle
// sets the terminal title via the title setter when terminalTitleFromRename=true.
// CC ref: utils/settings/types.ts terminalTitleFromRename — /rename updates terminal tab title.
func TestRenameHandlerSetsTerminalTitleWhenEnabled(t *testing.T) {
	var written []string
	noopWriter := func(_ string, _ contracts.ID, _ string) error { return nil }
	h := renameHandlerWithTitle(noopWriter, "sess1", "/tmp", true, func(title string) {
		written = append(written, title)
	})
	cc := CommandContext{Args: "my-session"}
	out, err := h(context.Background(), cc)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !out.Handled {
		t.Fatal("expected Handled=true")
	}
	if len(written) == 0 {
		t.Fatal("expected title writer to be called")
	}
	if !strings.Contains(written[0], "my-session") {
		t.Errorf("expected title to contain 'my-session'; got %q", written[0])
	}
}

// TestRenameHandlerSkipsTitleWhenDisabled verifies that the title writer is not
// called when terminalTitleFromRename=false.
func TestRenameHandlerSkipsTitleWhenDisabled(t *testing.T) {
	var written []string
	noopWriter := func(_ string, _ contracts.ID, _ string) error { return nil }
	h := renameHandlerWithTitle(noopWriter, "sess1", "/tmp", false, func(title string) {
		written = append(written, title)
	})
	cc := CommandContext{Args: "my-session"}
	_, err := h(context.Background(), cc)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(written) != 0 {
		t.Errorf("expected title writer NOT to be called when disabled; got %v", written)
	}
}
