package native

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"runtime"
	"sort"
	"strings"
	"time"

	"ccgo/internal/contracts"
	"ccgo/internal/platform"
)

const manifestFileName = "native-manifest.json"

type Manifest struct {
	SessionID        contracts.ID `json:"session_id,omitempty"`
	WorkingDirectory string       `json:"working_directory,omitempty"`
	GeneratedAt      string       `json:"generated_at"`
	GOOS             string       `json:"goos"`
	GOARCH           string       `json:"goarch"`
	Terminal         string       `json:"terminal,omitempty"`
	ColorTerminal    string       `json:"color_terminal,omitempty"`
	Capabilities     []Capability `json:"capabilities,omitempty"`
}

type Capability struct {
	Name      string `json:"name"`
	Available bool   `json:"available"`
	Detail    string `json:"detail,omitempty"`
}

func SessionManifestPath(sessionPath string, sessionID contracts.ID) string {
	if sessionPath == "" || sessionID == "" {
		return ""
	}
	return filepath.Join(filepath.Dir(sessionPath), string(sessionID), manifestFileName)
}

func BuildManifest(sessionID contracts.ID, cwd string) Manifest {
	manifest := Manifest{
		SessionID:        sessionID,
		WorkingDirectory: cwd,
		GeneratedAt:      time.Now().UTC().Format(time.RFC3339Nano),
		GOOS:             runtime.GOOS,
		GOARCH:           runtime.GOARCH,
		Terminal:         strings.TrimSpace(os.Getenv("TERM")),
		ColorTerminal:    strings.TrimSpace(os.Getenv("COLORTERM")),
		Capabilities: []Capability{
			{Name: "terminal_title", Available: true, Detail: "OSC title sequence generation"},
			{Name: "terminal_hyperlink", Available: true, Detail: "OSC 8 hyperlink sequence generation"},
			{Name: "terminal_progress", Available: true, Detail: "OSC progress sequence generation"},
			{Name: "osc52_clipboard", Available: true, Detail: "OSC 52 clipboard sequence generation"},
			{Name: "native_clipboard", Available: true, Detail: "session-scoped native clipboard runtime"},
			{Name: "native_file_index", Available: true, Detail: "session-scoped native file index runtime"},
			{Name: "native_color_diff", Available: true, Detail: "ANSI color diff rendering runtime"},
		},
	}
	sort.SliceStable(manifest.Capabilities, func(i, j int) bool {
		return manifest.Capabilities[i].Name < manifest.Capabilities[j].Name
	})
	return manifest
}

func WriteManifest(path string, manifest Manifest) error {
	if path == "" {
		return os.ErrInvalid
	}
	if manifest.GeneratedAt == "" {
		manifest.GeneratedAt = time.Now().UTC().Format(time.RFC3339Nano)
	}
	data, err := json.MarshalIndent(manifest, "", "  ")
	if err != nil {
		return err
	}
	data = append(data, '\n')
	return platform.AtomicWriteFile(path, data, 0o644)
}

func LoadManifest(path string) (Manifest, error) {
	if path == "" {
		return Manifest{}, os.ErrInvalid
	}
	data, err := os.ReadFile(path)
	if errors.Is(err, os.ErrNotExist) {
		return Manifest{}, nil
	}
	if err != nil {
		return Manifest{}, err
	}
	var manifest Manifest
	if err := json.Unmarshal(data, &manifest); err != nil {
		return Manifest{}, err
	}
	return manifest, nil
}

func CountAvailable(capabilities []Capability) int {
	count := 0
	for _, capability := range capabilities {
		if capability.Available {
			count++
		}
	}
	return count
}
