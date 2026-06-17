package native

import (
	"path/filepath"
	"runtime"
	"testing"
)

func TestSessionManifestPath(t *testing.T) {
	got := SessionManifestPath(filepath.Join("tmp", "sessions", "session.jsonl"), "sess_1")
	want := filepath.Join("tmp", "sessions", "sess_1", manifestFileName)
	if got != want {
		t.Fatalf("SessionManifestPath() = %q, want %q", got, want)
	}
	if got := SessionManifestPath("", "sess_1"); got != "" {
		t.Fatalf("empty transcript path = %q, want empty", got)
	}
	if got := SessionManifestPath("session.jsonl", ""); got != "" {
		t.Fatalf("empty session id = %q, want empty", got)
	}
}

func TestBuildManifest(t *testing.T) {
	t.Setenv("TERM", "xterm-256color")
	t.Setenv("COLORTERM", "truecolor")
	manifest := BuildManifest("sess_native", "/work")
	if manifest.SessionID != "sess_native" || manifest.WorkingDirectory != "/work" || manifest.GeneratedAt == "" {
		t.Fatalf("manifest metadata = %#v", manifest)
	}
	if manifest.GOOS != runtime.GOOS || manifest.GOARCH != runtime.GOARCH || manifest.Terminal != "xterm-256color" || manifest.ColorTerminal != "truecolor" {
		t.Fatalf("manifest platform = %#v", manifest)
	}
	if CountAvailable(manifest.Capabilities) == 0 {
		t.Fatalf("manifest capabilities = %#v", manifest.Capabilities)
	}
	if !hasCapability(manifest.Capabilities, "osc52_clipboard", true) ||
		!hasCapability(manifest.Capabilities, "native_file_index", true) ||
		!hasCapability(manifest.Capabilities, "native_clipboard", false) {
		t.Fatalf("manifest capabilities = %#v", manifest.Capabilities)
	}
}

func TestWriteAndLoadManifest(t *testing.T) {
	path := filepath.Join(t.TempDir(), "sess_native", manifestFileName)
	input := Manifest{
		SessionID:    "sess_native",
		GOOS:         "testos",
		GOARCH:       "testarch",
		Capabilities: []Capability{{Name: "native_clipboard", Available: false}},
	}
	if err := WriteManifest(path, input); err != nil {
		t.Fatal(err)
	}
	loaded, err := LoadManifest(path)
	if err != nil {
		t.Fatal(err)
	}
	if loaded.SessionID != "sess_native" || loaded.GeneratedAt == "" || loaded.GOOS != "testos" || len(loaded.Capabilities) != 1 {
		t.Fatalf("loaded manifest = %#v", loaded)
	}
}

func hasCapability(capabilities []Capability, name string, available bool) bool {
	for _, capability := range capabilities {
		if capability.Name == name && capability.Available == available {
			return true
		}
	}
	return false
}
