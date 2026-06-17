package integrations

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"sort"
	"time"

	"ccgo/internal/contracts"
	"ccgo/internal/platform"
)

const manifestFileName = "integrations-manifest.json"

const (
	RuntimeStateDisabled = "disabled"
	RuntimeStateNotWired = "not_wired"
	RuntimeStateReady    = "ready"
)

type Manifest struct {
	SessionID        contracts.ID  `json:"session_id,omitempty"`
	WorkingDirectory string        `json:"working_directory,omitempty"`
	GeneratedAt      string        `json:"generated_at"`
	Integrations     []Integration `json:"integrations,omitempty"`
}

type Integration struct {
	Name         string `json:"name"`
	Enabled      bool   `json:"enabled"`
	RuntimeState string `json:"runtime_state"`
	Detail       string `json:"detail,omitempty"`
}

func SessionManifestPath(sessionPath string, sessionID contracts.ID) string {
	if sessionPath == "" || sessionID == "" {
		return ""
	}
	return filepath.Join(filepath.Dir(sessionPath), string(sessionID), manifestFileName)
}

func BuildManifest(sessionID contracts.ID, cwd string, advanced *contracts.AdvancedSetting) Manifest {
	manifest := Manifest{
		SessionID:        sessionID,
		WorkingDirectory: cwd,
		GeneratedAt:      time.Now().UTC().Format(time.RFC3339Nano),
		Integrations: []Integration{
			buildIntegration("chrome", enabled(advancedChrome(advanced)), "session-scoped Chrome runtime state is ready"),
			buildIntegration("computer_use", enabled(advancedComputerUse(advanced)), "session-scoped computer-use runtime state is ready"),
			buildIntegration("voice", enabled(advancedVoice(advanced)), "session-scoped voice runtime state is ready"),
		},
	}
	sort.SliceStable(manifest.Integrations, func(i, j int) bool {
		return manifest.Integrations[i].Name < manifest.Integrations[j].Name
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

func AnyEnabled(advanced *contracts.AdvancedSetting) bool {
	return enabled(advancedChrome(advanced)) || enabled(advancedComputerUse(advanced)) || enabled(advancedVoice(advanced))
}

func CountEnabled(integrations []Integration) int {
	count := 0
	for _, integration := range integrations {
		if integration.Enabled {
			count++
		}
	}
	return count
}

func CountByRuntimeState(integrations []Integration) map[string]int {
	counts := make(map[string]int)
	for _, integration := range integrations {
		state := integration.RuntimeState
		if state == "" {
			state = RuntimeStateDisabled
		}
		counts[state]++
	}
	return counts
}

func buildIntegration(name string, isEnabled bool, readyDetail string) Integration {
	if isEnabled {
		return Integration{
			Name:         name,
			Enabled:      true,
			RuntimeState: RuntimeStateReady,
			Detail:       readyDetail,
		}
	}
	return Integration{
		Name:         name,
		RuntimeState: RuntimeStateDisabled,
		Detail:       "advanced integration is disabled or unset",
	}
}

func enabled(value *bool) bool {
	return value != nil && *value
}

func advancedChrome(advanced *contracts.AdvancedSetting) *bool {
	if advanced == nil {
		return nil
	}
	return advanced.Chrome
}

func advancedComputerUse(advanced *contracts.AdvancedSetting) *bool {
	if advanced == nil {
		return nil
	}
	return advanced.ComputerUse
}

func advancedVoice(advanced *contracts.AdvancedSetting) *bool {
	if advanced == nil {
		return nil
	}
	return advanced.Voice
}
