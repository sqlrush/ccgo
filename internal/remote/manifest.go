package remote

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"time"

	bridgepkg "ccgo/internal/bridge"
	"ccgo/internal/contracts"
	daemonpkg "ccgo/internal/daemon"
	"ccgo/internal/platform"
)

const manifestFileName = "remote-service.json"

type Manifest struct {
	SessionID        contracts.ID `json:"session_id,omitempty"`
	WorkingDirectory string       `json:"working_directory,omitempty"`
	EnvironmentID    string       `json:"environment_id,omitempty"`
	GeneratedAt      string       `json:"generated_at"`
	Services         []Service    `json:"services,omitempty"`
}

type Service struct {
	Name          string   `json:"name"`
	RuntimeState  string   `json:"runtime_state"`
	Endpoint      string   `json:"endpoint,omitempty"`
	WebSocketURL  string   `json:"websocket_url,omitempty"`
	TokenRequired bool     `json:"token_required,omitempty"`
	StatePath     string   `json:"state_path,omitempty"`
	ManifestPath  string   `json:"manifest_path,omitempty"`
	Commands      int      `json:"commands,omitempty"`
	PID           int      `json:"pid,omitempty"`
	Capabilities  []string `json:"capabilities,omitempty"`
}

type BuildInput struct {
	SessionID             contracts.ID
	WorkingDirectory      string
	EnvironmentID         string
	BridgeManifestPath    string
	BridgeDirectStatePath string
	BridgeManifest        bridgepkg.Manifest
	BridgeDirectState     bridgepkg.DirectState
	DaemonStatePath       string
	DaemonState           daemonpkg.State
	Now                   time.Time
}

func SessionManifestPath(sessionPath string, sessionID contracts.ID) string {
	if sessionPath == "" || sessionID == "" {
		return ""
	}
	return filepath.Join(filepath.Dir(sessionPath), string(sessionID), manifestFileName)
}

func BuildManifest(input BuildInput) Manifest {
	now := input.Now
	if now.IsZero() {
		now = time.Now().UTC()
	}
	manifest := Manifest{
		SessionID:        input.SessionID,
		WorkingDirectory: input.WorkingDirectory,
		EnvironmentID:    strings.TrimSpace(input.EnvironmentID),
		GeneratedAt:      now.UTC().Format(time.RFC3339Nano),
	}
	if bridgeService := buildBridgeService(input); bridgeService.Name != "" {
		manifest.Services = append(manifest.Services, bridgeService)
	}
	if daemonService := buildDaemonService(input); daemonService.Name != "" {
		manifest.Services = append(manifest.Services, daemonService)
	}
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

func ServiceCapabilityNames(service Service) string {
	return strings.Join(service.Capabilities, ", ")
}

func buildBridgeService(input BuildInput) Service {
	if input.BridgeManifest.SessionID == "" && input.BridgeDirectState.GeneratedAt == "" {
		return Service{}
	}
	runtimeState := strings.TrimSpace(input.BridgeDirectState.RuntimeState)
	if runtimeState == "" {
		runtimeState = bridgepkg.DirectRuntimeDisabled
	}
	service := Service{
		Name:          "bridge",
		RuntimeState:  runtimeState,
		Endpoint:      strings.TrimSpace(input.BridgeDirectState.URL),
		WebSocketURL:  strings.TrimSpace(input.BridgeDirectState.WebSocketURL),
		TokenRequired: input.BridgeDirectState.TokenRequired,
		StatePath:     strings.TrimSpace(input.BridgeDirectStatePath),
		ManifestPath:  strings.TrimSpace(input.BridgeManifestPath),
		Commands:      input.BridgeDirectState.Commands,
		Capabilities:  bridgeCapabilities(input.BridgeManifest),
	}
	if service.Commands == 0 {
		service.Commands = len(input.BridgeManifest.Commands)
	}
	return service
}

func buildDaemonService(input BuildInput) Service {
	if input.DaemonState.GeneratedAt == "" {
		return Service{}
	}
	runtimeState := daemonpkg.RuntimeStateAt(input.DaemonState, input.Now, 2*time.Minute)
	return Service{
		Name:         "daemon",
		RuntimeState: runtimeState,
		Endpoint:     strings.TrimSpace(input.DaemonState.Endpoint),
		StatePath:    strings.TrimSpace(input.DaemonStatePath),
		PID:          input.DaemonState.PID,
		Capabilities: daemonCapabilities(input.DaemonState),
	}
}

func bridgeCapabilities(manifest bridgepkg.Manifest) []string {
	capabilities := make([]string, 0, len(manifest.Capabilities))
	for _, capability := range manifest.Capabilities {
		if name := strings.TrimSpace(capability.Name); name != "" {
			capabilities = append(capabilities, name)
		}
	}
	return capabilities
}

func daemonCapabilities(state daemonpkg.State) []string {
	if strings.TrimSpace(state.Endpoint) == "" {
		return nil
	}
	return []string{"health", "status", "tick", "stop"}
}
