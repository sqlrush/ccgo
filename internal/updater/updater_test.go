package updater_test

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"ccgo/internal/updater"
)

// fakeReleaseServer stands up an httptest.Server that mimics the CC GCS release
// bucket layout:
//
//	GET /{channel}               → version string
//	GET /{version}/manifest.json → {"platforms": {platform: {checksum: "..."}}}
//	GET /{version}/{platform}/{binary} → binary bytes
func newFakeReleaseServer(t *testing.T, channel, latestVersion string, binaryContent []byte) *httptest.Server {
	t.Helper()

	platform := updater.CurrentPlatform()
	binaryName := updater.BinaryName()

	// Compute sha256 of binary
	h := sha256.Sum256(binaryContent)
	checksum := hex.EncodeToString(h[:])

	mux := http.NewServeMux()

	// Channel → version endpoint
	mux.HandleFunc("/"+channel, func(w http.ResponseWriter, r *http.Request) {
		fmt.Fprint(w, latestVersion)
	})

	// Manifest endpoint
	mux.HandleFunc("/"+latestVersion+"/manifest.json", func(w http.ResponseWriter, r *http.Request) {
		manifest := map[string]interface{}{
			"platforms": map[string]interface{}{
				platform: map[string]interface{}{
					"checksum": checksum,
				},
			},
		}
		w.Header().Set("Content-Type", "application/json")
		if err := json.NewEncoder(w).Encode(manifest); err != nil {
			t.Errorf("failed to encode manifest: %v", err)
		}
	})

	// Binary endpoint
	mux.HandleFunc("/"+latestVersion+"/"+platform+"/"+binaryName, func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/octet-stream")
		if _, err := w.Write(binaryContent); err != nil {
			t.Errorf("failed to write binary: %v", err)
		}
	})

	srv := httptest.NewServer(mux)
	t.Cleanup(srv.Close)
	return srv
}

func TestCheckLatestVersion_NewerAvailable(t *testing.T) {
	srv := newFakeReleaseServer(t, "latest", "2.0.0", []byte("fake-binary"))

	ver, err := updater.CheckLatestVersion("latest", srv.URL)
	if err != nil {
		t.Fatalf("CheckLatestVersion error: %v", err)
	}
	if ver != "2.0.0" {
		t.Fatalf("expected version 2.0.0, got %q", ver)
	}
}

func TestCheckLatestVersion_StableChannel(t *testing.T) {
	srv := newFakeReleaseServer(t, "stable", "1.9.0", []byte("fake-binary"))

	ver, err := updater.CheckLatestVersion("stable", srv.URL)
	if err != nil {
		t.Fatalf("CheckLatestVersion stable error: %v", err)
	}
	if ver != "1.9.0" {
		t.Fatalf("expected 1.9.0, got %q", ver)
	}
}

func TestCheckLatestVersion_ServerError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, "server error", http.StatusInternalServerError)
	}))
	t.Cleanup(srv.Close)

	_, err := updater.CheckLatestVersion("latest", srv.URL)
	if err == nil {
		t.Fatal("expected error from server 500, got nil")
	}
}

func TestDownloadAndInstall_Success(t *testing.T) {
	binaryContent := []byte("#!/bin/sh\necho hello")
	srv := newFakeReleaseServer(t, "latest", "2.0.0", binaryContent)

	targetDir := t.TempDir()
	targetPath := filepath.Join(targetDir, updater.BinaryName())

	err := updater.DownloadAndInstall("2.0.0", srv.URL, targetPath)
	if err != nil {
		t.Fatalf("DownloadAndInstall error: %v", err)
	}

	// Target file should exist and have executable bit
	info, err := os.Stat(targetPath)
	if err != nil {
		t.Fatalf("target file not found: %v", err)
	}
	if runtime.GOOS != "windows" && info.Mode()&0o111 == 0 {
		t.Fatalf("target file is not executable: %v", info.Mode())
	}

	got, err := os.ReadFile(targetPath)
	if err != nil {
		t.Fatalf("read target: %v", err)
	}
	if string(got) != string(binaryContent) {
		t.Fatalf("content mismatch: got %q, want %q", got, binaryContent)
	}
}

func TestDownloadAndInstall_ChecksumMismatch(t *testing.T) {
	// Server returns a different binary than what's in the manifest
	platform := updater.CurrentPlatform()
	binaryName := updater.BinaryName()

	goodContent := []byte("good binary")
	badContent := []byte("bad binary - tampered!")

	// Good checksum for goodContent
	h := sha256.Sum256(goodContent)
	checksum := hex.EncodeToString(h[:])

	mux := http.NewServeMux()
	mux.HandleFunc("/latest", func(w http.ResponseWriter, r *http.Request) {
		fmt.Fprint(w, "2.0.0")
	})
	mux.HandleFunc("/2.0.0/manifest.json", func(w http.ResponseWriter, r *http.Request) {
		manifest := map[string]interface{}{
			"platforms": map[string]interface{}{
				platform: map[string]interface{}{
					"checksum": checksum, // checksum of goodContent
				},
			},
		}
		if err := json.NewEncoder(w).Encode(manifest); err != nil {
			t.Errorf("encode manifest: %v", err)
		}
	})
	mux.HandleFunc("/2.0.0/"+platform+"/"+binaryName, func(w http.ResponseWriter, r *http.Request) {
		if _, err := w.Write(badContent); err != nil { // but serves badContent
			t.Errorf("write bad content: %v", err)
		}
	})

	srv := httptest.NewServer(mux)
	t.Cleanup(srv.Close)

	targetDir := t.TempDir()
	err := updater.DownloadAndInstall("2.0.0", srv.URL, filepath.Join(targetDir, binaryName))
	if err == nil {
		t.Fatal("expected checksum mismatch error, got nil")
	}
	if !strings.Contains(err.Error(), "checksum") {
		t.Fatalf("expected 'checksum' in error, got %q", err.Error())
	}
}

func TestDownloadAndInstall_PlatformNotInManifest(t *testing.T) {
	mux := http.NewServeMux()
	mux.HandleFunc("/latest", func(w http.ResponseWriter, r *http.Request) {
		fmt.Fprint(w, "2.0.0")
	})
	mux.HandleFunc("/2.0.0/manifest.json", func(w http.ResponseWriter, r *http.Request) {
		// No platform entries at all
		manifest := map[string]interface{}{
			"platforms": map[string]interface{}{},
		}
		if err := json.NewEncoder(w).Encode(manifest); err != nil {
			t.Errorf("encode manifest: %v", err)
		}
	})
	srv := httptest.NewServer(mux)
	t.Cleanup(srv.Close)

	targetDir := t.TempDir()
	binaryName := updater.BinaryName()
	err := updater.DownloadAndInstall("2.0.0", srv.URL, filepath.Join(targetDir, binaryName))
	if err == nil {
		t.Fatal("expected error for missing platform in manifest")
	}
	if !strings.Contains(err.Error(), "platform") {
		t.Fatalf("expected 'platform' in error, got %q", err.Error())
	}
}

func TestDownloadAndInstall_AtomicReplace(t *testing.T) {
	// Target already exists; DownloadAndInstall must atomically replace it
	binaryContent := []byte("new binary content")
	srv := newFakeReleaseServer(t, "latest", "2.0.0", binaryContent)

	targetDir := t.TempDir()
	targetPath := filepath.Join(targetDir, updater.BinaryName())

	// Pre-create the "old" binary
	if err := os.WriteFile(targetPath, []byte("old binary"), 0o755); err != nil {
		t.Fatalf("write old binary: %v", err)
	}

	if err := updater.DownloadAndInstall("2.0.0", srv.URL, targetPath); err != nil {
		t.Fatalf("DownloadAndInstall error: %v", err)
	}

	got, err := os.ReadFile(targetPath)
	if err != nil {
		t.Fatalf("read target: %v", err)
	}
	if string(got) != string(binaryContent) {
		t.Fatalf("content not replaced: got %q, want %q", got, binaryContent)
	}
}

func TestCurrentPlatform(t *testing.T) {
	p := updater.CurrentPlatform()
	if p == "" {
		t.Fatal("CurrentPlatform() returned empty string")
	}
	// Should be in the form os-arch
	if !strings.Contains(p, "-") {
		t.Fatalf("expected os-arch form, got %q", p)
	}
}
