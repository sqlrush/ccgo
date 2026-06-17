package native

import (
	"os"
	"path/filepath"
	"testing"
)

func TestSessionClipboardPath(t *testing.T) {
	got := SessionClipboardPath(filepath.Join("tmp", "sessions", "session.jsonl"), "sess_clip")
	want := filepath.Join("tmp", "sessions", "sess_clip", clipboardFileName)
	if got != want {
		t.Fatalf("SessionClipboardPath() = %q, want %q", got, want)
	}
	if got := SessionClipboardPath("", "sess_clip"); got != "" {
		t.Fatalf("empty transcript path = %q, want empty", got)
	}
}

func TestClipboardStateWriteReadAndPermissions(t *testing.T) {
	path := filepath.Join(t.TempDir(), "sess_clip", clipboardFileName)
	if err := EnsureClipboardState(path, "sess_clip"); err != nil {
		t.Fatal(err)
	}
	state, err := WriteClipboardText(path, "sess_clip", "", "copy me")
	if err != nil {
		t.Fatal(err)
	}
	if state.SessionID != "sess_clip" || state.UpdatedAt == "" || len(state.Items) != 1 || state.Items[0].Selection != "clipboard" {
		t.Fatalf("state = %#v", state)
	}
	text, ok, err := ReadClipboardText(path, "clipboard")
	if err != nil {
		t.Fatal(err)
	}
	if !ok || text != "copy me" {
		t.Fatalf("clipboard text = %q ok=%v", text, ok)
	}
	if _, _, err := ReadClipboardText(path, "primary"); err != nil {
		t.Fatal(err)
	}
	info, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	if info.Mode().Perm() != 0o600 {
		t.Fatalf("clipboard permissions = %v", info.Mode().Perm())
	}
}
