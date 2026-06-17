package integrations

import (
	"encoding/binary"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"runtime"
	"strings"

	"ccgo/internal/contracts"
	"ccgo/internal/platform"
)

const (
	ChromeNativeHostName     = "com.anthropic.claude_code_go"
	chromeHostFileName       = "chrome-native-host.json"
	chromeHostWrapperName    = "chrome-native-host.sh"
	chromeHostWrapperCMDName = "chrome-native-host.cmd"
)

type ChromeNativeHostInstallOptions struct {
	Browser           string
	GOOS              string
	HomeDir           string
	InstallDir        string
	WrapperSourcePath string
}

type ChromeNativeHostInstallResult struct {
	Browser     string `json:"browser"`
	SourcePath  string `json:"source_path,omitempty"`
	TargetPath  string `json:"target_path,omitempty"`
	WrapperPath string `json:"wrapper_path,omitempty"`
	Skipped     bool   `json:"skipped,omitempty"`
	Detail      string `json:"detail,omitempty"`
}

type ChromeNativeHostManifest struct {
	Name           string   `json:"name"`
	Description    string   `json:"description"`
	Path           string   `json:"path"`
	Type           string   `json:"type"`
	AllowedOrigins []string `json:"allowed_origins"`
}

func ChromeNativeHostManifestPath(sessionPath string, sessionID contracts.ID) string {
	if sessionPath == "" || sessionID == "" {
		return ""
	}
	return filepath.Join(filepath.Dir(sessionPath), string(sessionID), chromeHostFileName)
}

func ChromeNativeHostWrapperPath(sessionPath string, sessionID contracts.ID) string {
	if sessionPath == "" || sessionID == "" {
		return ""
	}
	name := chromeHostWrapperName
	if runtime.GOOS == "windows" {
		name = chromeHostWrapperCMDName
	}
	return filepath.Join(filepath.Dir(sessionPath), string(sessionID), name)
}

func BuildChromeNativeHostManifest(hostPath string, allowedOrigins []string) ChromeNativeHostManifest {
	return ChromeNativeHostManifest{
		Name:           ChromeNativeHostName,
		Description:    "Claude Code Go native messaging host",
		Path:           strings.TrimSpace(hostPath),
		Type:           "stdio",
		AllowedOrigins: normalizeChromeAllowedOrigins(allowedOrigins),
	}
}

func WriteChromeNativeHostManifest(path string, manifest ChromeNativeHostManifest) error {
	if path == "" {
		return os.ErrInvalid
	}
	if strings.TrimSpace(manifest.Name) == "" {
		manifest.Name = ChromeNativeHostName
	}
	if strings.TrimSpace(manifest.Description) == "" {
		manifest.Description = "Claude Code Go native messaging host"
	}
	if strings.TrimSpace(manifest.Type) == "" {
		manifest.Type = "stdio"
	}
	manifest.AllowedOrigins = normalizeChromeAllowedOrigins(manifest.AllowedOrigins)
	data, err := json.MarshalIndent(manifest, "", "  ")
	if err != nil {
		return err
	}
	data = append(data, '\n')
	return platform.AtomicWriteFile(path, data, 0o644)
}

func WriteChromeNativeHostWrapper(path string, executablePath string) error {
	if path == "" || strings.TrimSpace(executablePath) == "" {
		return os.ErrInvalid
	}
	var data []byte
	if strings.HasSuffix(strings.ToLower(path), ".cmd") {
		data = []byte("@echo off\r\n" + cmdQuote(executablePath) + " --chrome-native-host %*\r\n")
	} else {
		data = []byte("#!/bin/sh\nexec " + shellQuote(executablePath) + " --chrome-native-host \"$@\"\n")
	}
	return platform.AtomicWriteFile(path, data, 0o755)
}

func LoadChromeNativeHostManifest(path string) (ChromeNativeHostManifest, error) {
	if path == "" {
		return ChromeNativeHostManifest{}, os.ErrInvalid
	}
	data, err := os.ReadFile(path)
	if errors.Is(err, os.ErrNotExist) {
		return ChromeNativeHostManifest{}, nil
	}
	if err != nil {
		return ChromeNativeHostManifest{}, err
	}
	var manifest ChromeNativeHostManifest
	if err := json.Unmarshal(data, &manifest); err != nil {
		return ChromeNativeHostManifest{}, err
	}
	manifest.AllowedOrigins = normalizeChromeAllowedOrigins(manifest.AllowedOrigins)
	return manifest, nil
}

func InstallChromeNativeHostManifest(sourcePath string, options ChromeNativeHostInstallOptions) (ChromeNativeHostInstallResult, error) {
	result := ChromeNativeHostInstallResult{
		Browser:    normalizeChromeBrowser(options.Browser),
		SourcePath: strings.TrimSpace(sourcePath),
	}
	if result.SourcePath == "" {
		result.Skipped = true
		result.Detail = "source manifest path is not configured"
		return result, os.ErrInvalid
	}
	manifest, err := LoadChromeNativeHostManifest(result.SourcePath)
	if err != nil {
		result.Detail = err.Error()
		return result, err
	}
	if strings.TrimSpace(manifest.Name) == "" {
		manifest.Name = ChromeNativeHostName
	}
	targetPath, err := ChromeNativeHostInstallPath(manifest.Name, options)
	result.TargetPath = targetPath
	if err != nil {
		result.Skipped = true
		result.Detail = err.Error()
		return result, err
	}
	if strings.TrimSpace(options.WrapperSourcePath) != "" {
		wrapperPath, err := installChromeNativeHostWrapper(options.WrapperSourcePath, filepath.Dir(targetPath))
		result.WrapperPath = wrapperPath
		if err != nil {
			result.Detail = err.Error()
			return result, err
		}
		manifest.Path = wrapperPath
	}
	if err := WriteChromeNativeHostManifest(targetPath, manifest); err != nil {
		result.Detail = err.Error()
		return result, err
	}
	return result, nil
}

func ChromeNativeHostInstallPath(hostName string, options ChromeNativeHostInstallOptions) (string, error) {
	hostName = strings.TrimSpace(hostName)
	if hostName == "" {
		hostName = ChromeNativeHostName
	}
	browser := normalizeChromeBrowser(options.Browser)
	if strings.TrimSpace(options.InstallDir) != "" {
		return filepath.Join(options.InstallDir, hostName+".json"), nil
	}
	goos := strings.TrimSpace(options.GOOS)
	if goos == "" {
		goos = runtime.GOOS
	}
	if goos == "windows" {
		return "", fmt.Errorf("Windows Chrome native messaging host install requires HKCU/HKLM registry registration")
	}
	home := strings.TrimSpace(options.HomeDir)
	if home == "" {
		var err error
		home, err = os.UserHomeDir()
		if err != nil {
			return "", err
		}
	}
	if strings.TrimSpace(home) == "" {
		return "", fmt.Errorf("home directory is not available")
	}
	switch goos {
	case "darwin":
		switch browser {
		case "chromium":
			return filepath.Join(home, "Library", "Application Support", "Chromium", "NativeMessagingHosts", hostName+".json"), nil
		case "edge":
			return filepath.Join(home, "Library", "Application Support", "Microsoft Edge", "NativeMessagingHosts", hostName+".json"), nil
		default:
			return filepath.Join(home, "Library", "Application Support", "Google", "Chrome", "NativeMessagingHosts", hostName+".json"), nil
		}
	default:
		switch browser {
		case "chromium":
			return filepath.Join(home, ".config", "chromium", "NativeMessagingHosts", hostName+".json"), nil
		case "edge":
			return filepath.Join(home, ".config", "microsoft-edge", "NativeMessagingHosts", hostName+".json"), nil
		default:
			return filepath.Join(home, ".config", "google-chrome", "NativeMessagingHosts", hostName+".json"), nil
		}
	}
}

func installChromeNativeHostWrapper(sourcePath string, targetDir string) (string, error) {
	sourcePath = strings.TrimSpace(sourcePath)
	targetDir = strings.TrimSpace(targetDir)
	if sourcePath == "" || targetDir == "" {
		return "", os.ErrInvalid
	}
	data, err := os.ReadFile(sourcePath)
	if err != nil {
		return "", err
	}
	targetPath := filepath.Join(targetDir, filepath.Base(sourcePath))
	if err := platform.AtomicWriteFile(targetPath, data, 0o755); err != nil {
		return targetPath, err
	}
	return targetPath, nil
}

func ChromeAllowedOriginsFromEnv(env func(string) string) []string {
	if env == nil {
		env = os.Getenv
	}
	var origins []string
	for _, field := range strings.FieldsFunc(env("CLAUDE_CHROME_ALLOWED_ORIGINS"), func(r rune) bool {
		return r == ',' || r == ';' || r == '\n' || r == '\t' || r == ' '
	}) {
		if strings.TrimSpace(field) != "" {
			origins = append(origins, field)
		}
	}
	if id := strings.TrimSpace(env("CLAUDE_CHROME_EXTENSION_ID")); id != "" {
		origins = append(origins, "chrome-extension://"+strings.TrimSuffix(strings.TrimPrefix(id, "chrome-extension://"), "/")+"/")
	}
	return normalizeChromeAllowedOrigins(origins)
}

func ReadChromeNativeMessage(r io.Reader, maxBytes uint32) (json.RawMessage, error) {
	if r == nil {
		return nil, os.ErrInvalid
	}
	if maxBytes == 0 {
		maxBytes = 1 << 20
	}
	var header [4]byte
	if _, err := io.ReadFull(r, header[:]); err != nil {
		return nil, err
	}
	size := binary.LittleEndian.Uint32(header[:])
	if size > maxBytes {
		return nil, fmt.Errorf("chrome native message too large: %d > %d", size, maxBytes)
	}
	payload := make([]byte, size)
	if _, err := io.ReadFull(r, payload); err != nil {
		return nil, err
	}
	if !json.Valid(payload) {
		return nil, fmt.Errorf("chrome native message is not valid JSON")
	}
	return json.RawMessage(payload), nil
}

func WriteChromeNativeMessage(w io.Writer, payload any) error {
	if w == nil {
		return os.ErrInvalid
	}
	data, err := json.Marshal(payload)
	if err != nil {
		return err
	}
	if uint64(len(data)) > uint64(^uint32(0)) {
		return fmt.Errorf("chrome native message too large: %d", len(data))
	}
	var header [4]byte
	binary.LittleEndian.PutUint32(header[:], uint32(len(data)))
	if n, err := w.Write(header[:]); err != nil {
		return err
	} else if n != len(header) {
		return io.ErrShortWrite
	}
	if n, err := w.Write(data); err != nil {
		return err
	} else if n != len(data) {
		return io.ErrShortWrite
	}
	return nil
}

func normalizeChromeAllowedOrigins(origins []string) []string {
	seen := map[string]struct{}{}
	var normalized []string
	for _, origin := range origins {
		origin = strings.TrimSpace(origin)
		if origin == "" {
			continue
		}
		if strings.HasPrefix(origin, "chrome-extension://") && !strings.HasSuffix(origin, "/") {
			origin += "/"
		}
		if _, ok := seen[origin]; ok {
			continue
		}
		seen[origin] = struct{}{}
		normalized = append(normalized, origin)
	}
	return normalized
}

func normalizeChromeBrowser(browser string) string {
	switch strings.ToLower(strings.TrimSpace(browser)) {
	case "chromium", "chromium-browser":
		return "chromium"
	case "edge", "microsoft-edge", "msedge":
		return "edge"
	default:
		return "chrome"
	}
}

func shellQuote(value string) string {
	return "'" + strings.ReplaceAll(value, "'", "'\"'\"'") + "'"
}

func cmdQuote(value string) string {
	return `"` + strings.ReplaceAll(value, `"`, `""`) + `"`
}
