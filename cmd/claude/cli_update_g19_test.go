package main

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"ccgo/internal/updater"
)

// newFakeReleaseServerG19 sets up a fake release server for G19 update/install
// tests with the given channel, version, and binary bytes.
func newFakeReleaseServerG19(t *testing.T, channel, version string, binaryContent []byte) *httptest.Server {
	t.Helper()

	platform := updater.CurrentPlatform()
	binaryName := updater.BinaryName()

	h := sha256.Sum256(binaryContent)
	checksum := hex.EncodeToString(h[:])

	mux := http.NewServeMux()

	// Register channel endpoint(s); avoid duplicate registration panic.
	channelHandler := func(w http.ResponseWriter, r *http.Request) {
		fmt.Fprint(w, version)
	}
	mux.HandleFunc("/"+channel, channelHandler)
	// Register the other standard channel only if different from channel.
	otherChannel := "latest"
	if channel == "latest" {
		otherChannel = "stable"
	}
	mux.HandleFunc("/"+otherChannel, channelHandler)

	mux.HandleFunc("/"+version+"/manifest.json", func(w http.ResponseWriter, r *http.Request) {
		manifest := map[string]interface{}{
			"platforms": map[string]interface{}{
				platform: map[string]interface{}{
					"checksum": checksum,
				},
			},
		}
		w.Header().Set("Content-Type", "application/json")
		if err := json.NewEncoder(w).Encode(manifest); err != nil {
			t.Errorf("encode manifest: %v", err)
		}
	})

	mux.HandleFunc("/"+version+"/"+platform+"/"+binaryName, func(w http.ResponseWriter, r *http.Request) {
		if _, err := w.Write(binaryContent); err != nil {
			t.Errorf("write binary: %v", err)
		}
	})

	srv := httptest.NewServer(mux)
	t.Cleanup(srv.Close)
	return srv
}

// --- SUBCMD-UPDATE-03: version check + download (native install) ---

func TestUpdateNative_NewerVersionDownloadsAndReplaces(t *testing.T) {
	// GIVEN a native install with current version 1.0.0
	// AND the release server has 2.0.0 available
	// WHEN we run update with a temp target path (not this binary!)
	// THEN it downloads 2.0.0, writes it, reports success

	binaryContent := []byte("#!/bin/sh\necho claude 2.0.0")
	srv := newFakeReleaseServerG19(t, "latest", "2.0.0", binaryContent)

	targetDir := t.TempDir()
	targetPath := filepath.Join(targetDir, updater.BinaryName())
	// Pre-create old binary
	if err := os.WriteFile(targetPath, []byte("old binary"), 0o755); err != nil {
		t.Fatalf("write old binary: %v", err)
	}

	var out, errOut bytes.Buffer
	opts := updateOptions{
		Ver:            "1.0.0",
		Channel:        "latest",
		InstallType:    "native",
		ReleaseBaseURL: srv.URL,
		TargetPath:     targetPath, // injected: don't replace THIS binary
	}
	code := runUpdateCLIv2(nil, opts, &out, &errOut)
	if code != 0 {
		t.Fatalf("exit %d stderr=%q stdout=%q", code, errOut.String(), out.String())
	}

	combined := out.String() + errOut.String()
	if !strings.Contains(combined, "2.0.0") {
		t.Fatalf("expected new version 2.0.0 in output: %q", combined)
	}
	if !strings.Contains(strings.ToLower(combined), "updat") && !strings.Contains(strings.ToLower(combined), "success") {
		t.Fatalf("expected success indication in output: %q", combined)
	}

	// The target file should have been replaced
	got, err := os.ReadFile(targetPath)
	if err != nil {
		t.Fatalf("read target: %v", err)
	}
	if string(got) != string(binaryContent) {
		t.Fatalf("binary not replaced: got %q, want %q", got, binaryContent)
	}
}

func TestUpdateNative_AlreadyLatest(t *testing.T) {
	// GIVEN current version == latest version
	// WHEN we run update
	// THEN it reports "up to date" and does NOT download anything

	binaryContent := []byte("binary")
	srv := newFakeReleaseServerG19(t, "latest", "1.0.0", binaryContent)

	targetDir := t.TempDir()
	targetPath := filepath.Join(targetDir, updater.BinaryName())

	var out, errOut bytes.Buffer
	opts := updateOptions{
		Ver:            "1.0.0", // same as server
		Channel:        "latest",
		InstallType:    "native",
		ReleaseBaseURL: srv.URL,
		TargetPath:     targetPath,
	}
	code := runUpdateCLIv2(nil, opts, &out, &errOut)
	if code != 0 {
		t.Fatalf("exit %d stderr=%q stdout=%q", code, errOut.String(), out.String())
	}

	combined := out.String() + errOut.String()
	if !strings.Contains(strings.ToLower(combined), "up to date") {
		t.Fatalf("expected 'up to date' in output: %q", combined)
	}

	// target file should NOT have been created (no download)
	if _, err := os.Stat(targetPath); !os.IsNotExist(err) {
		t.Fatalf("expected no download for up-to-date version, but target file exists")
	}
}

func TestUpdateNative_StableChannelSelection(t *testing.T) {
	// GIVEN channel=stable on the release server
	// WHEN we query the version
	// THEN it reads from /stable not /latest

	binaryContent := []byte("stable binary")
	srv := newFakeReleaseServerG19(t, "stable", "1.9.0", binaryContent)

	targetDir := t.TempDir()
	targetPath := filepath.Join(targetDir, updater.BinaryName())

	var out, errOut bytes.Buffer
	opts := updateOptions{
		Ver:            "1.0.0",
		Channel:        "stable",
		InstallType:    "native",
		ReleaseBaseURL: srv.URL,
		TargetPath:     targetPath,
	}
	code := runUpdateCLIv2(nil, opts, &out, &errOut)
	if code != 0 {
		t.Fatalf("exit %d stderr=%q stdout=%q", code, errOut.String(), out.String())
	}

	combined := out.String() + errOut.String()
	if !strings.Contains(combined, "1.9.0") {
		t.Fatalf("expected version 1.9.0 from stable channel: %q", combined)
	}
}

func TestUpdateNative_VersionCheckFails(t *testing.T) {
	// GIVEN the release server returns 500
	// WHEN we run update
	// THEN it exits 1 with error message

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, "server error", http.StatusInternalServerError)
	}))
	t.Cleanup(srv.Close)

	targetDir := t.TempDir()
	targetPath := filepath.Join(targetDir, updater.BinaryName())

	var out, errOut bytes.Buffer
	opts := updateOptions{
		Ver:            "1.0.0",
		Channel:        "latest",
		InstallType:    "native",
		ReleaseBaseURL: srv.URL,
		TargetPath:     targetPath,
	}
	code := runUpdateCLIv2(nil, opts, &out, &errOut)
	if code == 0 {
		t.Fatalf("expected non-zero exit on version check failure, got 0; stdout=%q", out.String())
	}

	combined := out.String() + errOut.String()
	if !strings.Contains(strings.ToLower(combined), "fail") && !strings.Contains(strings.ToLower(combined), "error") {
		t.Fatalf("expected error indication in output: %q", combined)
	}
}

// --- SUBCMD-INSTALL-01/02/03/04: real install logic ---

func TestInstallDownloadsNativeBinary(t *testing.T) {
	// GIVEN a fake release server with version 2.0.0
	// WHEN we run install with the server URL injected
	// THEN it downloads 2.0.0 to targetPath

	binaryContent := []byte("#!/bin/sh\necho claude 2.0.0")
	srv := newFakeReleaseServerG19(t, "latest", "2.0.0", binaryContent)
	t.Setenv(updater.EnvReleaseBaseURL, srv.URL)

	targetDir := t.TempDir()
	targetPath := filepath.Join(targetDir, updater.BinaryName())

	var out, errOut bytes.Buffer
	code := runInstallCLIv2(nil, installOptions{
		Target:     "latest",
		TargetPath: targetPath,
	}, &out, &errOut)
	if code != 0 {
		t.Fatalf("exit %d stderr=%q stdout=%q", code, errOut.String(), out.String())
	}

	combined := out.String() + errOut.String()
	if !strings.Contains(combined, "2.0.0") {
		t.Fatalf("expected version 2.0.0 in output: %q", combined)
	}

	got, err := os.ReadFile(targetPath)
	if err != nil {
		t.Fatalf("read installed binary: %v", err)
	}
	if string(got) != string(binaryContent) {
		t.Fatalf("binary content mismatch: got %q, want %q", got, binaryContent)
	}
}

func TestInstallForceFlag(t *testing.T) {
	// GIVEN the installed version == latest
	// BUT --force is set
	// THEN it still downloads and replaces

	binaryContent := []byte("same version binary")
	srv := newFakeReleaseServerG19(t, "latest", "1.0.0", binaryContent)
	t.Setenv(updater.EnvReleaseBaseURL, srv.URL)

	targetDir := t.TempDir()
	targetPath := filepath.Join(targetDir, updater.BinaryName())
	if err := os.WriteFile(targetPath, []byte("old content"), 0o755); err != nil {
		t.Fatalf("write old binary: %v", err)
	}

	var out, errOut bytes.Buffer
	code := runInstallCLIv2(nil, installOptions{
		Target:         "latest",
		Force:          true,
		CurrentVersion: "1.0.0",
		TargetPath:     targetPath,
	}, &out, &errOut)
	if code != 0 {
		t.Fatalf("exit %d stderr=%q stdout=%q", code, errOut.String(), out.String())
	}

	got, err := os.ReadFile(targetPath)
	if err != nil {
		t.Fatalf("read installed binary: %v", err)
	}
	if string(got) != string(binaryContent) {
		t.Fatalf("force install binary not replaced: got %q, want %q", got, binaryContent)
	}
}

func TestInstallStableTarget(t *testing.T) {
	// GIVEN target=stable
	// THEN it queries /stable on the release server

	binaryContent := []byte("stable content")
	srv := newFakeReleaseServerG19(t, "stable", "1.9.0", binaryContent)
	t.Setenv(updater.EnvReleaseBaseURL, srv.URL)

	targetDir := t.TempDir()
	targetPath := filepath.Join(targetDir, updater.BinaryName())

	var out, errOut bytes.Buffer
	code := runInstallCLIv2(nil, installOptions{
		Target:     "stable",
		TargetPath: targetPath,
	}, &out, &errOut)
	if code != 0 {
		t.Fatalf("exit %d stderr=%q stdout=%q", code, errOut.String(), out.String())
	}

	combined := out.String() + errOut.String()
	if !strings.Contains(combined, "1.9.0") {
		t.Fatalf("expected 1.9.0 in output for stable target: %q", combined)
	}
}

func TestInstallAlreadyLatestNoForce(t *testing.T) {
	// GIVEN current version == latest AND no --force
	// THEN it reports already up to date without downloading

	binaryContent := []byte("binary")
	srv := newFakeReleaseServerG19(t, "latest", "2.0.0", binaryContent)
	t.Setenv(updater.EnvReleaseBaseURL, srv.URL)

	targetDir := t.TempDir()
	targetPath := filepath.Join(targetDir, updater.BinaryName())

	var out, errOut bytes.Buffer
	code := runInstallCLIv2(nil, installOptions{
		Target:         "latest",
		CurrentVersion: "2.0.0",
		TargetPath:     targetPath,
	}, &out, &errOut)
	if code != 0 {
		t.Fatalf("exit %d stderr=%q stdout=%q", code, errOut.String(), out.String())
	}

	combined := out.String() + errOut.String()
	if !strings.Contains(strings.ToLower(combined), "up to date") {
		t.Fatalf("expected 'up to date' in output: %q", combined)
	}

	// No download should have happened
	if _, err := os.Stat(targetPath); !os.IsNotExist(err) {
		t.Fatalf("expected no download for already-current version, but file exists")
	}
}

func TestInstallNpmMigrateDetection(t *testing.T) {
	// GIVEN the current installation is npm-global
	// WHEN install is called
	// THEN output mentions npm cleanup / migration

	binaryContent := []byte("native binary")
	srv := newFakeReleaseServerG19(t, "latest", "2.0.0", binaryContent)
	t.Setenv(updater.EnvReleaseBaseURL, srv.URL)

	targetDir := t.TempDir()
	targetPath := filepath.Join(targetDir, updater.BinaryName())

	var out, errOut bytes.Buffer
	code := runInstallCLIv2(nil, installOptions{
		Target:          "latest",
		TargetPath:      targetPath,
		CurrentInstType: "npm-global",
	}, &out, &errOut)
	if code != 0 {
		t.Fatalf("exit %d stderr=%q stdout=%q", code, errOut.String(), out.String())
	}

	combined := out.String() + errOut.String()
	if !strings.Contains(strings.ToLower(combined), "npm") {
		t.Fatalf("expected npm migration mention in output: %q", combined)
	}
}

func TestInstallServerError(t *testing.T) {
	// GIVEN the release server is unreachable
	// THEN install exits non-zero with a clear error

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, "gone", http.StatusGone)
	}))
	t.Cleanup(srv.Close)
	t.Setenv(updater.EnvReleaseBaseURL, srv.URL)

	targetDir := t.TempDir()
	targetPath := filepath.Join(targetDir, updater.BinaryName())

	var out, errOut bytes.Buffer
	code := runInstallCLIv2(nil, installOptions{
		Target:     "latest",
		TargetPath: targetPath,
	}, &out, &errOut)
	if code == 0 {
		t.Fatal("expected non-zero exit when server returns error, got 0")
	}
}
