// Package updater implements the claude update / install binary download and
// atomic-replace flow. The release base URL is injectable so tests can point
// at an httptest server instead of the real GCS bucket.
//
// Release server API (mirrors CC GCS layout):
//
//	GET {baseURL}/{channel}                   → version string (e.g. "1.2.3")
//	GET {baseURL}/{version}/manifest.json     → {"platforms":{"os-arch":{"checksum":"hex"}}}
//	GET {baseURL}/{version}/{platform}/{name} → binary bytes
package updater

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"time"
)

// DefaultReleaseBaseURL is the real CC GCS bucket URL used in production.
const DefaultReleaseBaseURL = "https://storage.googleapis.com/claude-code-dist-86c565f3-f756-42ad-8dfa-d59b1c096819/claude-code-releases"

// EnvReleaseBaseURL is the environment variable that overrides DefaultReleaseBaseURL.
// Set it to an httptest server URL in tests.
const EnvReleaseBaseURL = "CLAUDE_RELEASE_BASE_URL"

// ResolveBaseURL returns the release base URL: env override > default.
func ResolveBaseURL() string {
	if v := os.Getenv(EnvReleaseBaseURL); v != "" {
		return strings.TrimRight(v, "/")
	}
	return DefaultReleaseBaseURL
}

// CurrentPlatform returns the OS-arch string that matches CC's GCS path layout,
// e.g. "darwin-arm64", "linux-x64", "win32-x64".
func CurrentPlatform() string {
	goos := runtime.GOOS
	arch := runtime.GOARCH

	// Normalise to CC naming conventions.
	osStr := goos
	if goos == "windows" {
		osStr = "win32"
	}

	archStr := arch
	switch arch {
	case "amd64":
		archStr = "x64"
	case "arm64":
		archStr = "arm64"
	}

	return osStr + "-" + archStr
}

// BinaryName returns the executable file name for the current platform.
func BinaryName() string {
	if runtime.GOOS == "windows" {
		return "claude.exe"
	}
	return "claude"
}

// manifest is the JSON structure served at {baseURL}/{version}/manifest.json.
type manifest struct {
	Platforms map[string]platformInfo `json:"platforms"`
}

type platformInfo struct {
	Checksum string `json:"checksum"`
}

// httpClient is the package-level HTTP client with a conservative timeout.
// Tests rely on httptest which is local, so a short timeout is fine.
var httpClient = &http.Client{Timeout: 30 * time.Second}

// CheckLatestVersion queries {baseURL}/{channel} and returns the version string.
// Returns an error if the server is unreachable or returns a non-200 status.
func CheckLatestVersion(channel, baseURL string) (string, error) {
	url := strings.TrimRight(baseURL, "/") + "/" + channel
	resp, err := httpClient.Get(url) //nolint:noctx
	if err != nil {
		return "", fmt.Errorf("version check GET %s: %w", url, err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("version check %s: server returned %d", url, resp.StatusCode)
	}

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return "", fmt.Errorf("version check read body: %w", err)
	}

	version := strings.TrimSpace(string(body))
	if version == "" {
		return "", fmt.Errorf("version check: empty response from %s", url)
	}
	return version, nil
}

// DownloadAndInstall downloads {version} from {baseURL}, verifies its sha256
// checksum against the manifest, and atomically replaces {targetPath}.
//
// Atomicity: the binary is first written to a sibling temp file, then
// os.Rename'd into place. Rename is atomic on POSIX; on Windows it may fail
// if targetPath is already open — callers should handle that case.
func DownloadAndInstall(version, baseURL, targetPath string) error {
	base := strings.TrimRight(baseURL, "/")
	platform := CurrentPlatform()
	binaryName := BinaryName()

	// 1. Fetch manifest to get the expected checksum.
	manifestURL := fmt.Sprintf("%s/%s/manifest.json", base, version)
	m, err := fetchManifest(manifestURL)
	if err != nil {
		return fmt.Errorf("fetch manifest: %w", err)
	}

	pInfo, ok := m.Platforms[platform]
	if !ok {
		return fmt.Errorf("platform %q not found in manifest for version %s", platform, version)
	}
	expectedChecksum := pInfo.Checksum

	// 2. Download binary bytes.
	binaryURL := fmt.Sprintf("%s/%s/%s/%s", base, version, platform, binaryName)
	data, err := fetchBinary(binaryURL)
	if err != nil {
		return fmt.Errorf("download binary: %w", err)
	}

	// 3. Verify checksum.
	h := sha256.Sum256(data)
	actualChecksum := hex.EncodeToString(h[:])
	if actualChecksum != expectedChecksum {
		return fmt.Errorf("checksum mismatch: expected %s, got %s", expectedChecksum, actualChecksum)
	}

	// 4. Atomic replace: write to temp file, chmod, rename.
	dir := filepath.Dir(targetPath)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return fmt.Errorf("create parent dir %s: %w", dir, err)
	}

	tmpFile, err := os.CreateTemp(dir, ".claude-update-*")
	if err != nil {
		return fmt.Errorf("create temp file: %w", err)
	}
	tmpPath := tmpFile.Name()
	defer func() {
		// Clean up temp file on any error path; ignore close/remove errors.
		_ = tmpFile.Close()
		_ = os.Remove(tmpPath)
	}()

	if _, err := tmpFile.Write(data); err != nil {
		return fmt.Errorf("write temp binary: %w", err)
	}
	if err := tmpFile.Chmod(0o755); err != nil {
		return fmt.Errorf("chmod temp binary: %w", err)
	}
	if err := tmpFile.Close(); err != nil {
		return fmt.Errorf("close temp binary: %w", err)
	}

	// Rename is atomic on POSIX; on Windows it overwrites if possible.
	if err := os.Rename(tmpPath, targetPath); err != nil {
		return fmt.Errorf("atomic rename %s → %s: %w", tmpPath, targetPath, err)
	}
	return nil
}

func fetchManifest(url string) (*manifest, error) {
	resp, err := httpClient.Get(url) //nolint:noctx
	if err != nil {
		return nil, fmt.Errorf("GET %s: %w", url, err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("GET %s: server returned %d", url, resp.StatusCode)
	}

	var m manifest
	if err := json.NewDecoder(resp.Body).Decode(&m); err != nil {
		return nil, fmt.Errorf("decode manifest JSON: %w", err)
	}
	if m.Platforms == nil {
		return nil, fmt.Errorf("manifest missing 'platforms' field")
	}
	return &m, nil
}

func fetchBinary(url string) ([]byte, error) {
	// Use a longer timeout for potentially large binaries.
	client := &http.Client{Timeout: 5 * time.Minute}
	resp, err := client.Get(url) //nolint:noctx
	if err != nil {
		return nil, fmt.Errorf("GET %s: %w", url, err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("GET %s: server returned %d", url, resp.StatusCode)
	}

	data, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("read binary body: %w", err)
	}
	return data, nil
}
