package integrations

import (
	"bytes"
	"context"
	"encoding/binary"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
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

func TestChromeNativeHostWrapperWrite(t *testing.T) {
	path := filepath.Join(t.TempDir(), "sess_chrome", chromeHostWrapperName)
	if err := WriteChromeNativeHostWrapper(path, "/tmp/Claude Code/claude"); err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(data), "exec '/tmp/Claude Code/claude' --chrome-native-host") {
		t.Fatalf("wrapper = %q", string(data))
	}
	info, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	if info.Mode().Perm() != 0o755 {
		t.Fatalf("wrapper mode = %v", info.Mode().Perm())
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

func TestInstallChromeNativeHostManifestWritesTarget(t *testing.T) {
	dir := t.TempDir()
	sourcePath := filepath.Join(dir, "session", chromeHostFileName)
	wrapperPath := filepath.Join(dir, "session", chromeHostWrapperName)
	if err := WriteChromeNativeHostWrapper(wrapperPath, "/usr/local/bin/claude"); err != nil {
		t.Fatal(err)
	}
	manifest := BuildChromeNativeHostManifest("/usr/local/bin/claude", []string{"chrome-extension://abc"})
	if err := WriteChromeNativeHostManifest(sourcePath, manifest); err != nil {
		t.Fatal(err)
	}
	installDir := filepath.Join(dir, "NativeMessagingHosts")
	result, err := InstallChromeNativeHostManifest(context.Background(), sourcePath, ChromeNativeHostInstallOptions{InstallDir: installDir, WrapperSourcePath: wrapperPath})
	if err != nil {
		t.Fatal(err)
	}
	wantTarget := filepath.Join(installDir, ChromeNativeHostName+".json")
	if result.TargetPath != wantTarget || result.Skipped {
		t.Fatalf("install result = %#v, want target %q", result, wantTarget)
	}
	installed, err := LoadChromeNativeHostManifest(wantTarget)
	if err != nil {
		t.Fatal(err)
	}
	wantWrapper := filepath.Join(installDir, chromeHostWrapperName)
	if result.WrapperPath != wantWrapper {
		t.Fatalf("wrapper path = %q, want %q", result.WrapperPath, wantWrapper)
	}
	if installed.Name != ChromeNativeHostName || installed.Path != wantWrapper {
		t.Fatalf("installed manifest = %#v", installed)
	}
}

func TestChromeNativeHostInstallPathDefaults(t *testing.T) {
	linuxPath, err := ChromeNativeHostInstallPath(ChromeNativeHostName, ChromeNativeHostInstallOptions{
		GOOS:    "linux",
		HomeDir: "/home/alice",
		Browser: "chromium",
	})
	if err != nil {
		t.Fatal(err)
	}
	if linuxPath != filepath.Join("/home/alice", ".config", "chromium", "NativeMessagingHosts", ChromeNativeHostName+".json") {
		t.Fatalf("linux path = %q", linuxPath)
	}
	darwinPath, err := ChromeNativeHostInstallPath(ChromeNativeHostName, ChromeNativeHostInstallOptions{
		GOOS:    "darwin",
		HomeDir: "/Users/alice",
		Browser: "edge",
	})
	if err != nil {
		t.Fatal(err)
	}
	if darwinPath != filepath.Join("/Users/alice", "Library", "Application Support", "Microsoft Edge", "NativeMessagingHosts", ChromeNativeHostName+".json") {
		t.Fatalf("darwin path = %q", darwinPath)
	}
}

func TestChromeNativeHostInstallPathDefaultsForWindows(t *testing.T) {
	path, err := ChromeNativeHostInstallPath(ChromeNativeHostName, ChromeNativeHostInstallOptions{GOOS: "windows", HomeDir: `C:\Users\alice`})
	if err != nil {
		t.Fatal(err)
	}
	want := filepath.Join(`C:\Users\alice`, "AppData", "Local", "ClaudeCodeGo", "NativeMessagingHosts", ChromeNativeHostName+".json")
	if path != want {
		t.Fatalf("windows path = %q, want %q", path, want)
	}
}

func TestInstallChromeNativeHostManifestRegistersWindowsHKCU(t *testing.T) {
	dir := t.TempDir()
	sourcePath := filepath.Join(dir, "session", chromeHostFileName)
	wrapperPath := filepath.Join(dir, "session", chromeHostWrapperCMDName)
	if err := WriteChromeNativeHostWrapper(wrapperPath, `C:\Program Files\ccgo\ccgo.exe`); err != nil {
		t.Fatal(err)
	}
	manifest := BuildChromeNativeHostManifest(wrapperPath, []string{"chrome-extension://abc"})
	if err := WriteChromeNativeHostManifest(sourcePath, manifest); err != nil {
		t.Fatal(err)
	}
	var gotCommand []string
	result, err := InstallChromeNativeHostManifest(context.Background(), sourcePath, ChromeNativeHostInstallOptions{
		GOOS:              "windows",
		HomeDir:           `C:\Users\alice`,
		Browser:           "edge",
		InstallDir:        filepath.Join(dir, "NativeMessagingHosts"),
		WrapperSourcePath: wrapperPath,
		RegistryRunner: func(ctx context.Context, command []string) error {
			gotCommand = append([]string(nil), command...)
			return nil
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	wantKey := `HKCU\Software\Microsoft\Edge\NativeMessagingHosts\` + ChromeNativeHostName
	if result.RegistryKey != wantKey {
		t.Fatalf("registry key = %q, want %q", result.RegistryKey, wantKey)
	}
	wantCommand := BuildChromeNativeHostRegistryInstallCommand(wantKey, result.TargetPath)
	if !sameChromeHostStrings(gotCommand, wantCommand) {
		t.Fatalf("command = %#v, want %#v", gotCommand, wantCommand)
	}
	installed, err := LoadChromeNativeHostManifest(result.TargetPath)
	if err != nil {
		t.Fatal(err)
	}
	if installed.Path != result.WrapperPath || result.WrapperPath == "" {
		t.Fatalf("installed manifest = %#v result=%#v", installed, result)
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

func sameChromeHostStrings(a []string, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
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
