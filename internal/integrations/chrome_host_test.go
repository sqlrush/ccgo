package integrations

import (
	"bytes"
	"encoding/binary"
	"encoding/json"
	"path/filepath"
	"testing"
)

func TestChromeNativeHostManifestPath(t *testing.T) {
	got := ChromeNativeHostManifestPath(filepath.Join("tmp", "sessions", "session.jsonl"), "sess_chrome")
	want := filepath.Join("tmp", "sessions", "sess_chrome", chromeHostFileName)
	if got != want {
		t.Fatalf("ChromeNativeHostManifestPath() = %q, want %q", got, want)
	}
	if got := ChromeNativeHostManifestPath("", "sess_chrome"); got != "" {
		t.Fatalf("empty transcript path = %q, want empty", got)
	}
}

func TestChromeNativeHostManifestWriteLoad(t *testing.T) {
	path := filepath.Join(t.TempDir(), "sess_chrome", chromeHostFileName)
	manifest := BuildChromeNativeHostManifest("/usr/local/bin/claude", []string{
		"chrome-extension://abc",
		"chrome-extension://abc/",
		"chrome-extension://def/",
	})
	if err := WriteChromeNativeHostManifest(path, manifest); err != nil {
		t.Fatal(err)
	}
	loaded, err := LoadChromeNativeHostManifest(path)
	if err != nil {
		t.Fatal(err)
	}
	if loaded.Name != ChromeNativeHostName || loaded.Type != "stdio" || loaded.Path != "/usr/local/bin/claude" {
		t.Fatalf("loaded manifest = %#v", loaded)
	}
	if len(loaded.AllowedOrigins) != 2 || loaded.AllowedOrigins[0] != "chrome-extension://abc/" || loaded.AllowedOrigins[1] != "chrome-extension://def/" {
		t.Fatalf("allowed origins = %#v", loaded.AllowedOrigins)
	}
}

func TestChromeAllowedOriginsFromEnv(t *testing.T) {
	origins := ChromeAllowedOriginsFromEnv(func(key string) string {
		switch key {
		case "CLAUDE_CHROME_ALLOWED_ORIGINS":
			return "chrome-extension://one/ chrome-extension://two"
		case "CLAUDE_CHROME_EXTENSION_ID":
			return "three"
		default:
			return ""
		}
	})
	want := []string{"chrome-extension://one/", "chrome-extension://two/", "chrome-extension://three/"}
	if len(origins) != len(want) {
		t.Fatalf("origins = %#v", origins)
	}
	for i := range want {
		if origins[i] != want[i] {
			t.Fatalf("origin %d = %q, want %q", i, origins[i], want[i])
		}
	}
}

func TestChromeNativeMessageRoundTrip(t *testing.T) {
	var buf bytes.Buffer
	if err := WriteChromeNativeMessage(&buf, map[string]any{"type": "ping", "ok": true}); err != nil {
		t.Fatal(err)
	}
	if buf.Len() < 5 {
		t.Fatalf("encoded message too short: %d", buf.Len())
	}
	size := binary.LittleEndian.Uint32(buf.Bytes()[:4])
	if int(size) != buf.Len()-4 {
		t.Fatalf("size header = %d, payload = %d", size, buf.Len()-4)
	}
	raw, err := ReadChromeNativeMessage(&buf, 1024)
	if err != nil {
		t.Fatal(err)
	}
	var decoded map[string]any
	if err := json.Unmarshal(raw, &decoded); err != nil {
		t.Fatal(err)
	}
	if decoded["type"] != "ping" || decoded["ok"] != true {
		t.Fatalf("decoded = %#v", decoded)
	}
}

func TestChromeNativeMessageRejectsOversizeAndInvalidJSON(t *testing.T) {
	var oversized bytes.Buffer
	var header [4]byte
	binary.LittleEndian.PutUint32(header[:], 4)
	oversized.Write(header[:])
	oversized.WriteString(`true`)
	if _, err := ReadChromeNativeMessage(&oversized, 3); err == nil {
		t.Fatal("expected oversize error")
	}

	var invalid bytes.Buffer
	binary.LittleEndian.PutUint32(header[:], 3)
	invalid.Write(header[:])
	invalid.WriteString(`bad`)
	if _, err := ReadChromeNativeMessage(&invalid, 1024); err == nil {
		t.Fatal("expected invalid JSON error")
	}
}
