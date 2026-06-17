package native

import (
	"context"
	"errors"
	"path/filepath"
	"testing"
)

func TestWriteClipboardTextWithAdaptersUpdatesStateAndRunsSystemCommand(t *testing.T) {
	path := filepath.Join(t.TempDir(), "sess_clip", clipboardFileName)
	var gotCommand []string
	var gotStdin string
	runner := func(ctx context.Context, command []string, stdin string) (string, error) {
		gotCommand = append([]string(nil), command...)
		gotStdin = stdin
		return "", nil
	}
	state, result, err := WriteClipboardTextWithAdapters(context.Background(), path, "sess_clip", "clipboard", "copy me", []ClipboardAdapter{{
		Name:         "pbcopy",
		Kind:         ClipboardAdapterKindSystem,
		Available:    true,
		WriteCommand: []string{"/usr/bin/pbcopy"},
		ReadCommand:  []string{"/usr/bin/pbpaste"},
	}}, runner)
	if err != nil {
		t.Fatal(err)
	}
	if state.SessionID != "sess_clip" || len(state.Items) != 1 || state.Items[0].Text != "copy me" {
		t.Fatalf("state = %#v", state)
	}
	if !result.External || result.AdapterName != "pbcopy" || result.Skipped {
		t.Fatalf("result = %#v", result)
	}
	if len(gotCommand) != 1 || gotCommand[0] != "/usr/bin/pbcopy" || gotStdin != "copy me" {
		t.Fatalf("command = %#v stdin=%q", gotCommand, gotStdin)
	}
}

func TestWriteClipboardTextWithAdaptersFallsBackToSessionState(t *testing.T) {
	path := filepath.Join(t.TempDir(), "sess_clip", clipboardFileName)
	state, result, err := WriteClipboardTextWithAdapters(context.Background(), path, "sess_clip", "", "copy me", []ClipboardAdapter{{
		Name:      "osc52",
		Kind:      ClipboardAdapterKindTerminal,
		Available: true,
	}}, nil)
	if err != nil {
		t.Fatal(err)
	}
	if len(state.Items) != 1 || state.Items[0].Text != "copy me" {
		t.Fatalf("state = %#v", state)
	}
	if !result.Skipped || result.External {
		t.Fatalf("result = %#v", result)
	}
}

func TestReadClipboardTextWithAdaptersUsesReadableCommand(t *testing.T) {
	path := filepath.Join(t.TempDir(), "sess_clip", clipboardFileName)
	runner := func(ctx context.Context, command []string, stdin string) (string, error) {
		if len(command) != 1 || command[0] != "/usr/bin/pbpaste" || stdin != "" {
			t.Fatalf("command = %#v stdin=%q", command, stdin)
		}
		return "from system", nil
	}
	text, ok, result, err := ReadClipboardTextWithAdapters(context.Background(), path, "clipboard", []ClipboardAdapter{{
		Name:         "pbcopy",
		Kind:         ClipboardAdapterKindSystem,
		Available:    true,
		WriteCommand: []string{"/usr/bin/pbcopy"},
		ReadCommand:  []string{"/usr/bin/pbpaste"},
	}}, runner)
	if err != nil {
		t.Fatal(err)
	}
	if !ok || text != "from system" {
		t.Fatalf("text = %q ok=%v", text, ok)
	}
	if !result.External || result.AdapterName != "pbcopy" {
		t.Fatalf("result = %#v", result)
	}
}

func TestReadClipboardTextWithAdaptersFallsBackToSessionState(t *testing.T) {
	path := filepath.Join(t.TempDir(), "sess_clip", clipboardFileName)
	if _, err := WriteClipboardText(path, "sess_clip", "", "local"); err != nil {
		t.Fatal(err)
	}
	text, ok, result, err := ReadClipboardTextWithAdapters(context.Background(), path, "", nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	if !ok || text != "local" {
		t.Fatalf("text = %q ok=%v", text, ok)
	}
	if !result.Skipped || result.External {
		t.Fatalf("result = %#v", result)
	}
}

func TestClipboardCommandErrorsAreReturned(t *testing.T) {
	path := filepath.Join(t.TempDir(), "sess_clip", clipboardFileName)
	wantErr := errors.New("command failed")
	_, result, err := WriteClipboardTextWithAdapters(context.Background(), path, "sess_clip", "", "copy", []ClipboardAdapter{{
		Name:         "tmux",
		Kind:         ClipboardAdapterKindMultiplexer,
		Available:    true,
		WriteCommand: []string{"/usr/bin/tmux", "load-buffer", "-"},
	}}, func(context.Context, []string, string) (string, error) {
		return "", wantErr
	})
	if !errors.Is(err, wantErr) {
		t.Fatalf("err = %v, want %v", err, wantErr)
	}
	if !result.External || result.Detail == "" {
		t.Fatalf("result = %#v", result)
	}
}
