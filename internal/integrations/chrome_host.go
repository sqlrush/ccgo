package integrations

import (
	"encoding/binary"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"

	"ccgo/internal/contracts"
	"ccgo/internal/platform"
)

const (
	ChromeNativeHostName = "com.anthropic.claude_code_go"
	chromeHostFileName   = "chrome-native-host.json"
)

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
